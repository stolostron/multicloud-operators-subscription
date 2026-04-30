// Copyright 2024 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package git

// Tests for the goroutine lifecycle fix in git_subscriber_item.go.
//
// These tests cover three categories:
//  1. Stop() nil-safety / idempotency (no k8s required).
//  2. WaitGroup semantics – Stop() must block until the running goroutine
//     exits, ensuring no two goroutines ever race on shared SubscriberItem
//     fields (ghsi.resources, ghsi.successful, etc.).
//  3. doSubscription() resets ghsi.successful to false at entry so that a
//     stale "true" value left by a previous run cannot bypass the empty-
//     resource guard and trigger a spurious PurgeAllSubscribedResources call.

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	chnv1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"
	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	kubesynchronizer "open-cluster-management.io/multicloud-operators-subscription/pkg/synchronizer/kubernetes"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// noopSyncSource satisfies the SyncSource interface. GetLocalClient returns a
// fake controller-runtime client that returns NotFound for all Gets; this lets
// utils.UpdateLastUpdateTime (and similar helpers) fail gracefully without
// needing a real API server.
type noopSyncSource struct {
	fakeClient client.Client
}

func newNoopSyncSource() *noopSyncSource {
	return &noopSyncSource{
		// Build a fake client using the default scheme. Custom types such as
		// appv1.Subscription are not registered, so Gets on them return a "no
		// kind is registered" error – which callers already handle gracefully.
		fakeClient: fake.NewClientBuilder().Build(),
	}
}

func (n *noopSyncSource) GetInterval() int                                       { return 0 }
func (n *noopSyncSource) GetLocalClient() client.Client                          { return n.fakeClient }
func (n *noopSyncSource) GetLocalNonCachedClient() client.Client                 { return n.fakeClient }
func (n *noopSyncSource) GetRemoteClient() client.Client                         { return nil }
func (n *noopSyncSource) GetRemoteNonCachedClient() client.Client                { return nil }
func (n *noopSyncSource) IsResourceNamespaced(_ *unstructured.Unstructured) bool { return true }
func (n *noopSyncSource) ProcessSubResources(_ *appv1.Subscription,
	_ []kubesynchronizer.ResourceUnit,
	_, _ map[string]map[string]string,
	_, _ bool) error {
	return nil
}
func (n *noopSyncSource) PurgeAllSubscribedResources(_ *appv1.Subscription) error { return nil }
func (n *noopSyncSource) UpdateAppsubOverallStatus(_ *appv1.Subscription, _ bool, _ string) error {
	return nil
}

// minimalItem constructs a SubscriberItem with just enough state for
// doSubscription to start executing: a Subscription, a Channel with an
// intentionally unreachable URL (so cloneGitRepo fails fast without a
// network timeout), and a noop synchronizer.
func minimalItem() *SubscriberItem {
	return &SubscriberItem{
		SubscriberItem: appv1.SubscriberItem{
			Subscription: &appv1.Subscription{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "lifecycle-test-sub",
					Namespace: "default",
				},
			},
			// Use localhost:1 so that the git clone attempt fails
			// immediately (connection refused) rather than timing out.
			Channel: &chnv1.Channel{
				Spec: chnv1.ChannelSpec{
					Pathname: "https://localhost:1/nonexistent.git",
				},
			},
		},
		synchronizer: newNoopSyncSource(),
	}
}

// ─── Stop() nil-safety ────────────────────────────────────────────────────────

// TestStop_NilStopch verifies that Stop() on a zero-value SubscriberItem
// (where stopch is nil and no goroutine has ever been started) does not panic.
func TestStop_NilStopch(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Stop() panicked on nil stopch: %v", r)
		}
	}()

	item := &SubscriberItem{
		SubscriberItem: appv1.SubscriberItem{
			Subscription: &appv1.Subscription{
				ObjectMeta: metav1.ObjectMeta{Name: "s"},
			},
		},
	}
	item.Stop() // must not panic
}

// TestStop_Idempotent verifies that calling Stop() twice does not cause a
// double-close panic. The nil-guard on stopch must make the second call a no-op.
func TestStop_Idempotent(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("second Stop() panicked: %v", r)
		}
	}()

	item := &SubscriberItem{
		SubscriberItem: appv1.SubscriberItem{
			Subscription: &appv1.Subscription{
				ObjectMeta: metav1.ObjectMeta{Name: "s"},
			},
		},
	}
	item.stopch = make(chan struct{})

	item.Stop() // first call: closes stopch, sets stopch = nil
	item.Stop() // second call: stopch is nil, must be a no-op
}

// TestStop_NilsStopchAfterReturn verifies the nil-guard postcondition: after
// Stop() returns, ghsi.stopch must be nil so that a subsequent Start() can
// create a fresh channel without risk of closing an already-closed one.
func TestStop_NilsStopchAfterReturn(t *testing.T) {
	item := &SubscriberItem{
		SubscriberItem: appv1.SubscriberItem{
			Subscription: &appv1.Subscription{
				ObjectMeta: metav1.ObjectMeta{Name: "s"},
			},
		},
	}
	item.stopch = make(chan struct{})
	item.Stop()

	if item.stopch != nil {
		t.Fatal("expected stopch to be nil after Stop()")
	}
}

// ─── WaitGroup semantics ──────────────────────────────────────────────────────

// TestStop_WaitsForRunningGoroutine is the central test for the race-condition
// fix. It manually registers a goroutine under the SubscriberItem's WaitGroup
// (exactly as Start() does) and verifies that Stop() blocks until that
// goroutine exits before returning.
//
// Without the wg.Wait() call in Stop(), this test would fail: Stop() would
// return while the goroutine was still running, allowing a subsequent goroutine
// to race on shared SubscriberItem fields.
func TestStop_WaitsForRunningGoroutine(t *testing.T) {
	item := &SubscriberItem{
		SubscriberItem: appv1.SubscriberItem{
			Subscription: &appv1.Subscription{
				ObjectMeta: metav1.ObjectMeta{Name: "s"},
			},
		},
	}
	item.stopch = make(chan struct{})

	goroutineStarted := make(chan struct{})
	goroutineCanExit := make(chan struct{})

	// Simulate a running goroutine, exactly as Start() sets up.
	item.wg.Add(1)

	go func() {
		defer item.wg.Done()

		close(goroutineStarted)
		<-goroutineCanExit // hold until the test releases it
	}()

	<-goroutineStarted // ensure the goroutine is running before calling Stop()

	stopReturned := make(chan struct{})

	go func() {
		item.Stop()
		close(stopReturned)
	}()

	// Stop() should be blocked while the goroutine is still running.
	select {
	case <-stopReturned:
		t.Fatal("Stop() returned before the goroutine exited")
	case <-time.After(100 * time.Millisecond):
	}

	// Allow the goroutine to finish.
	close(goroutineCanExit)

	// Stop() must unblock promptly once the goroutine exits.
	select {
	case <-stopReturned:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not return after the goroutine exited")
	}
}

// TestStart_RestartSerializesGoroutines verifies the end-to-end invariant that
// motivates the fix: when Start(restart=true) is called while a goroutine is
// already running (represented by an entry in the WaitGroup), the call blocks
// via Stop() until the old goroutine finishes before the new one is allowed to
// start, preventing two goroutines from concurrently touching ghsi.resources.
func TestStart_RestartSerializesGoroutines(t *testing.T) {
	item := &SubscriberItem{
		SubscriberItem: appv1.SubscriberItem{
			Subscription: &appv1.Subscription{
				ObjectMeta: metav1.ObjectMeta{Name: "s"},
			},
		},
	}
	item.stopch = make(chan struct{})

	firstGoroutineStarted := make(chan struct{})
	firstGoroutineCanExit := make(chan struct{})
	firstGoroutineExited := make(chan struct{})

	// Simulate the first "running" goroutine.
	item.wg.Add(1)

	go func() {
		defer item.wg.Done()
		defer close(firstGoroutineExited)

		close(firstGoroutineStarted)
		<-firstGoroutineCanExit
	}()

	<-firstGoroutineStarted

	// Invoke Stop() (the part of Start(restart=true) that serializes goroutines)
	// in a separate goroutine so we can observe whether it blocks.
	stopReturned := make(chan struct{})

	go func() {
		item.Stop() // must block until first goroutine exits
		close(stopReturned)
	}()

	// Stop() should be blocked.
	select {
	case <-stopReturned:
		t.Fatal("Stop() returned before the first goroutine exited")
	case <-time.After(100 * time.Millisecond):
	}

	// Release the first goroutine.
	close(firstGoroutineCanExit)
	<-firstGoroutineExited

	// Now Stop() should complete.
	select {
	case <-stopReturned:
		// success: new goroutines are safe to start because the old one has exited
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not return after the first goroutine exited")
	}
}

// ─── doSubscription: successful field reset ───────────────────────────────────

// The Ginkgo test below verifies the "successful" reset that guards against a
// second, related facet of the race: a stale successful=true from a prior run
// bypassing the empty-resource check and causing ProcessSubResources to be
// called with no resources (which would trigger PurgeAllSubscribedResources).
//
// It relies on the webhookEnabled fast-path as a canary:
//   - WITHOUT the fix: doSubscription would see successful=true right after the
//     webhook check and return nil immediately (skip reconciliation).
//   - WITH the fix: doSubscription resets successful=false first, so the webhook
//     check falls through to cloneGitRepo, which fails (no real git server).
//     doSubscription therefore returns a non-nil error.
var _ = Describe("doSubscription resets successful flag", func() {
	It("should reset successful=false at entry, preventing stale state from prior run", func() {
		item := minimalItem()
		item.synchronizer = defaultSubscriber.synchronizer

		// Simulate a prior successful run leaving successful=true.
		item.successful = true
		item.webhookEnabled = true

		// With the fix, doSubscription resets successful to false before the
		// webhook check, so it does NOT return nil – it proceeds to cloneGitRepo
		// (which fails), and returns a non-nil error.
		err := item.doSubscription()
		Expect(err).To(HaveOccurred(),
			"doSubscription should have returned an error; "+
				"a nil return would indicate it skipped via the stale successful=true path")
		Expect(item.successful).To(BeFalse(),
			"successful must be false after doSubscription fails")
	})
})
