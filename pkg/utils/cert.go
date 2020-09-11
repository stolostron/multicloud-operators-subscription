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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"k8s.io/klog"
)

func GenerateServerCerts(dir string) error {
	var err error
	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)

	if err != nil {
		klog.Errorf("Failed to generate private key: %v", err)
		return err
	}

	notBefore := time.Now()
	notAfter := notBefore.AddDate(5, 0, 0)

	ca := x509.Certificate{
		SerialNumber: big.NewInt(2019),
		Subject: pkix.Name{
			Organization: []string{"Red Hat, Inc."},
			Country:      []string{"CA"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	caBytes, err := x509.CreateCertificate(rand.Reader, &ca, &ca, &privateKey.PublicKey, privateKey)
	if err != nil {
		klog.Errorf("Failed to create certificate: %v", err)
		return err
	}

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err = os.MkdirAll(dir, os.ModePerm)
		if err != nil {
			klog.Error(err, "Failed to make directory ", dir)
			return err
		}
	}

	certFilePath := filepath.Join(dir, "tls.crt")
	certOut, err := os.Create(certFilePath)

	if err != nil {
		klog.Errorf("Failed to open tls.crt for writing: %v", err)
		return err
	}

	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: caBytes}); err != nil {
		klog.Errorf("Failed to write data to tls.crt: %v", err)
		return err
	}

	if err := certOut.Close(); err != nil {
		klog.Errorf("Error closing tls.crt: %v", err)
		return err
	}

	klog.Infof("tls.crt file was generated successfully.\n")

	keyFilePath := filepath.Join(dir, "tls.key")
	keyOut, err := os.OpenFile(filepath.Clean(keyFilePath), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)

	if err != nil {
		klog.Errorf("Failed to open tls.key for writing: %v", err)
		return err
	}

	privBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)

	if err != nil {
		klog.Errorf("Unable to marshal private key: %v", err)
		return err
	}

	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: privBytes}); err != nil {
		klog.Errorf("Failed to write data to tls.key: %v", err)
		return err
	}

	if err := keyOut.Close(); err != nil {
		klog.Errorf("Error closing tls.key: %v", err)
		return err
	}

	klog.Infof("tls.key file was generated successfully.\n")

	return nil
}
