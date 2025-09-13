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

package utils

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"

	awsutils "open-cluster-management.io/multicloud-operators-subscription/pkg/utils/aws"
)

// FakeS3Server is a simple in-memory S3-compatible server for testing
type FakeS3Server struct {
	buckets map[string]map[string][]byte // bucket -> key -> content
	mu      sync.RWMutex
}

// NewFakeS3Server creates a new fake S3 server instance
func NewFakeS3Server() *FakeS3Server {
	return &FakeS3Server{
		buckets: make(map[string]map[string][]byte),
	}
}

// ServeHTTP implements the http.Handler interface for the fake S3 server
func (s *FakeS3Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.SplitN(path, "/", 2)

	if len(parts) == 0 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	bucket := parts[0]

	var key string

	if len(parts) > 1 {
		key = parts[1]
	}

	switch r.Method {
	case "PUT":
		if key == "" {
			// Create bucket
			if s.buckets[bucket] == nil {
				s.buckets[bucket] = make(map[string][]byte)
			}

			w.WriteHeader(http.StatusOK)
		} else {
			// Put object
			if s.buckets[bucket] == nil {
				s.buckets[bucket] = make(map[string][]byte)
			}

			body, err := io.ReadAll(r.Body)

			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			s.buckets[bucket][key] = body

			w.WriteHeader(http.StatusOK)
		}

	case "GET":
		if key == "" {
			// List objects in bucket
			if s.buckets[bucket] == nil {
				http.Error(w, "NoSuchBucket", http.StatusNotFound)
				return
			}

			// Simple XML response for list objects
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusOK)

			fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult>
    <Name>%s</Name>`, bucket)

			prefix := r.URL.Query().Get("prefix")
			for objKey := range s.buckets[bucket] {
				if prefix == "" || strings.HasPrefix(objKey, prefix) {
					fmt.Fprintf(w, `
    <Contents>
        <Key>%s</Key>
        <Size>%d</Size>
    </Contents>`, objKey, len(s.buckets[bucket][objKey]))
				}
			}

			fmt.Fprint(w, "\n</ListBucketResult>")
		} else {
			// Get object
			if s.buckets[bucket] == nil {
				http.Error(w, "NoSuchBucket", http.StatusNotFound)
				return
			}

			content, exists := s.buckets[bucket][key]

			if !exists {
				http.Error(w, "NoSuchKey", http.StatusNotFound)
				return
			}

			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)

			if _, err := w.Write(content); err != nil {
				// Log error but don't return since headers are already written
				// In a real scenario, you might want to log this error
				return
			}
		}

	case "HEAD":
		if key == "" {
			// Check if bucket exists
			if s.buckets[bucket] == nil {
				http.Error(w, "NoSuchBucket", http.StatusNotFound)
				return
			}

			w.WriteHeader(http.StatusOK)
		} else {
			// Check if object exists
			if s.buckets[bucket] == nil {
				http.Error(w, "NoSuchBucket", http.StatusNotFound)
				return
			}

			if _, exists := s.buckets[bucket][key]; !exists {
				http.Error(w, "NoSuchKey", http.StatusNotFound)
				return
			}

			w.WriteHeader(http.StatusOK)
		}

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// SetupFakeS3Server creates and configures a fake S3 server for testing
// Returns the HTTP test server, AWS handler, and endpoint string
func SetupFakeS3Server() (*httptest.Server, *awsutils.Handler, string) {
	fakeS3 := NewFakeS3Server()
	server := httptest.NewServer(fakeS3)

	// Parse the server URL to get endpoint without scheme
	serverURL, _ := url.Parse(server.URL)
	endpoint := serverURL.Host

	// Create AWS handler
	awsHandler := &awsutils.Handler{}
	err := awsHandler.InitObjectStoreConnection(
		server.URL,
		"test-access-key",
		"test-secret-key",
		"us-east-1",
		"true", // insecure
		"",     // no CA cert
	)

	if err != nil {
		server.Close()
		return nil, nil, ""
	}

	return server, awsHandler, endpoint
}
