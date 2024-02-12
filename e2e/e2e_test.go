// Copyright 2021 The Kubernetes Authors.
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

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
)

const (
	defaultAddr     = "localhost:8765"
	runEndpoint     = "/run"
	clusterEndpoint = "/clusters"
	Succeed         = "succeed"
)

func IsSeverUp(addr, cluster string) error {
	URL := fmt.Sprintf("http://%s%s", addr, cluster)
	resp, err := http.Get(URL)

	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("e2e server is not up")
	}

	return nil
}

type Runner struct {
	Addr     string
	Endpoint string
}

func NewRunner(url, endpoint string) *Runner {
	return &Runner{
		Addr:     url,
		Endpoint: endpoint,
	}
}

type TResponse struct {
	TestID  string      `json:"test_id"`
	Name    string      `json:"name"`
	Status  string      `json:"run_status"`
	Error   string      `json:"error"`
	Details interface{} `json:"details"`
}

func (tr *TResponse) String() string {
	o, err := json.MarshalIndent(tr, "", "\t")
	if err != nil {
		return ""
	}

	return string(o)
}

func (r *Runner) Run(runID string) error {
	URL := fmt.Sprintf("http://%s%s?id=%s", r.Addr, r.Endpoint, runID)
	resp, err := http.Get(URL)

	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		res := &TResponse{}

		if err := json.Unmarshal(bodyBytes, res); err != nil {
			return err
		}

		if res.Status != Succeed {
			return fmt.Errorf("failed test on %s, with status %s err: %s", res.TestID, res.Status, res.Status)
		}

		return nil
	}

	return fmt.Errorf("incorrect response code %v", resp.StatusCode)
}
func TestE2ESuite(t *testing.T) {
	if err := IsSeverUp(defaultAddr, clusterEndpoint); err != nil {
		t.Fatal(err)
	}

	runner := NewRunner(defaultAddr, runEndpoint)

	testIDs := []string{"sub-002", "sub-003", "sub-004",
		"RHACM4K-2346", "RHACM4K-1701", "RHACM4K-2352", "RHACM4K-2347", "RHACM4K-2570", "RHACM4K-2569"}
	stageTestIDs := []string{"RHACM4K-2348", "RHACM4K-1732", "RHACM4K-2566", "RHACM4K-2568"}

	for _, tID := range testIDs {
		if err := runner.Run(tID); err != nil {
			t.Fatal(err)
		}
	}

	for _, tID := range stageTestIDs {
		if err := runner.Run(tID); err != nil {
			t.Fatal(err)
		}
	}

	t.Logf("The e2e tests %v, stage tests %v passed", testIDs, stageTestIDs)
}
