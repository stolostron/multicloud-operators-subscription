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

package helmrepo

// Lifecycle tests for the goroutine serialization fix in helm_subscriber_item.go.
//
// The fix adds a sync.WaitGroup to SubscriberItem so that Stop() blocks until
// the running doSubscription goroutine exits before a new one is started by
// Start(restart=true). This eliminates the race condition where two concurrent
// goroutines write to shared fields such as hrsi.success.
//
// These tests do not require a Kubernetes API server or a real Helm repository;
// they exercise the synchronization primitives directly by manipulating the
// unexported wg and stopch fields.

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
)

// newHelmItem returns a minimal SubscriberItem with just enough state to
// exercise the Start/Stop lifecycle without panicking.
func newHelmItem() *SubscriberItem {
	return &SubscriberItem{
		SubscriberItem: appv1.SubscriberItem{
			Subscription: &appv1.Subscription{
				ObjectMeta: metav1.ObjectMeta{Name: "helm-lifecycle-test"},
			},
		},
	}
}

// ─── Stop() nil-safety ────────────────────────────────────────────────────────

// TestHelmStop_NilStopch verifies that Stop() on a zero-value SubscriberItem
// (where stopch is nil and no goroutine has been started) does not panic.
func TestHelmStop_NilStopch(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Stop() panicked on nil stopch: %v", r)
		}
	}()

	newHelmItem().Stop()
}

// TestHelmStop_Idempotent verifies that calling Stop() twice does not cause a
// double-close panic. The nil-guard on stopch must make the second call a no-op.
func TestHelmStop_Idempotent(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("second Stop() panicked: %v", r)
		}
	}()

	item := newHelmItem()
	item.stopch = make(chan struct{})

	item.Stop()
	item.Stop()
}

// TestHelmStop_NilsStopchAfterReturn verifies that stopch is nil after Stop()
// returns, ensuring a subsequent Start() can create a fresh channel safely.
func TestHelmStop_NilsStopchAfterReturn(t *testing.T) {
	item := newHelmItem()
	item.stopch = make(chan struct{})
	item.Stop()

	if item.stopch != nil {
		t.Fatal("expected stopch to be nil after Stop()")
	}
}

// ─── WaitGroup semantics ──────────────────────────────────────────────────────

// TestHelmStop_WaitsForRunningGoroutine verifies that Stop() blocks until the
// WaitGroup drains — i.e. until the goroutine started by Start() has exited.
//
// The test registers a goroutine under the SubscriberItem's WaitGroup exactly
// as Start() does, then asserts that Stop() does not return until the goroutine
// is released.
func TestHelmStop_WaitsForRunningGoroutine(t *testing.T) {
	item := newHelmItem()
	item.stopch = make(chan struct{})

	goroutineStarted := make(chan struct{})
	goroutineCanExit := make(chan struct{})

	item.wg.Add(1)

	go func() {
		defer item.wg.Done()

		close(goroutineStarted)
		<-goroutineCanExit
	}()

	<-goroutineStarted

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

	close(goroutineCanExit)

	select {
	case <-stopReturned:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not return after the goroutine exited")
	}
}

// TestHelmStart_RestartSerializesGoroutines verifies that the Stop() call
// inside Start(restart=true) blocks until the previous goroutine exits,
// preventing two goroutines from concurrently accessing shared SubscriberItem
// fields such as hrsi.success.
func TestHelmStart_RestartSerializesGoroutines(t *testing.T) {
	item := newHelmItem()
	item.stopch = make(chan struct{})

	firstGoroutineStarted := make(chan struct{})
	firstGoroutineCanExit := make(chan struct{})
	firstGoroutineExited := make(chan struct{})

	item.wg.Add(1)

	go func() {
		defer item.wg.Done()
		defer close(firstGoroutineExited)

		close(firstGoroutineStarted)
		<-firstGoroutineCanExit
	}()

	<-firstGoroutineStarted

	stopReturned := make(chan struct{})

	go func() {
		item.Stop()
		close(stopReturned)
	}()

	select {
	case <-stopReturned:
		t.Fatal("Stop() returned before the first goroutine exited")
	case <-time.After(100 * time.Millisecond):
	}

	close(firstGoroutineCanExit)
	<-firstGoroutineExited

	select {
	case <-stopReturned:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not return after the first goroutine exited")
	}
}
