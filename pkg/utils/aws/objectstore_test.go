// Copyright 2019 The Kubernetes Authors.
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

package aws

import (
	"net/http/httptest"
	"testing"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
	"github.com/onsi/gomega"
)

func TestObjectstore(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Set up a fake S3 server
	backend := s3mem.New()
	faker := gofakes3.New(backend)
	ts := httptest.NewServer(faker.Server())

	defer ts.Close()

	awshandler := &Handler{}

	err := awshandler.InitObjectStoreConnection(ts.URL, "randomid", "randomkey")
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Invalid bucket name
	err = awshandler.Create("test$^@&^#@&^#T/")
	g.Expect(err).To(gomega.HaveOccurred())

	// Valid bucket name
	err = awshandler.Create("test")
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Valid bucket name
	err = awshandler.Create("test2")
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Test bucket exists
	err = awshandler.Exists("test")
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Test bucket does not exist
	err = awshandler.Exists("notest")
	g.Expect(err).To(gomega.HaveOccurred())

	// List items in the bucket
	buckets, err := awshandler.List("test")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(buckets)).To(gomega.Equal(0))

	_, err = awshandler.List("notest")
	g.Expect(err).To(gomega.HaveOccurred())

	// Put items into the bucket
	testObj := &DeployableObject{}

	err = awshandler.Put("test", *testObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	testObj.Name = "testObj"
	testObj.Version = "1.1.1"
	testObj.GenerateName = "generateTestObj"

	err = awshandler.Put("test", *testObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = awshandler.Put("notest", *testObj)
	g.Expect(err).To(gomega.HaveOccurred())

	// Get items into the bucket
	obj, err := awshandler.Get("test", "testObj")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(obj.Name).To(gomega.Equal("testObj"))

	_, err = awshandler.Get("test", "testObjDummy")
	g.Expect(err).To(gomega.HaveOccurred())

	err = awshandler.Delete("test", "testObjDummy")
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Check if the item is still there
	obj, err = awshandler.Get("test", "testObj")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(obj.Name).To(gomega.Equal("testObj"))

	err = awshandler.Delete("test", "testObj")
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Check if the item is deleted now
	_, err = awshandler.Get("test", "testObj")
	g.Expect(err).To(gomega.HaveOccurred())
}
