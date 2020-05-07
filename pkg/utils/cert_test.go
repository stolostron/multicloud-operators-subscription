// Copyright 2020 The Kubernetes Authors.
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

package utils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/onsi/gomega"
)

func TestGenerateServerCerts(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	certDir := filepath.Join(os.TempDir(), "certtest")

	err := GenerateServerCerts(certDir)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	tlsKeyFile := filepath.Join(certDir, "tls.key")
	tlsCrtFile := filepath.Join(certDir, "tls.crt")

	_, err = os.Stat(tlsKeyFile)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	_, err = os.Stat(tlsCrtFile)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = os.RemoveAll(certDir)
	g.Expect(err).NotTo(gomega.HaveOccurred())
}
