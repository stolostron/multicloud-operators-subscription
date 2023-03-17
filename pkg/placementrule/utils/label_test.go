// Copyright 2021 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package utils

import (
	"testing"

	"github.com/onsi/gomega"
)

func TestValidateK8sLabel(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	APIServer := "_-.api.xili-aws-cluster-pool-tg2g4.dev06.red-chesterfield.com_-."
	expectedServerLabel := "api.xili-aws-cluster-pool-tg2g4.dev06.red-chesterfield.com"

	ServerLabel := ValidateK8sLabel(APIServer)

	g.Expect(ServerLabel).Should(gomega.Equal(expectedServerLabel))
}
