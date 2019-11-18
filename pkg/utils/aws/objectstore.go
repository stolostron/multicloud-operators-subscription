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
	"bytes"
	"context"
	"io/ioutil"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/endpoints"
	"github.com/aws/aws-sdk-go-v2/aws/external"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"k8s.io/klog"
)

// ObjectStore interface
type ObjectStore interface {
	InitObjectStoreConnection(endpoint, accessKeyID, secretAccessKey string) error
	Exists(bucket string) error
	Create(bucket string) error
	List(bucket string) ([]string, error)
	Put(bucket string, dplObj DeployableObject) error
	Delete(bucket, name string) error
	Get(bucket, name string) (DeployableObject, error)
}

var _ ObjectStore = &Handler{}

const (
	// SecretMapKeyAccessKeyID is key of accesskeyid in secret
	SecretMapKeyAccessKeyID = "AccessKeyID"
	// SecretMapKeySecretAccessKey is key of secretaccesskey in secret
	SecretMapKeySecretAccessKey = "SecretAccessKey"
	//metadata key for stroing the deployable generatename name
	DeployableGenerateNameMeta = "x-amz-meta-generatename"
	//Deployable generate name key within the meta map
	DployableMateGenerateNameKey = "Generatename"
	//metadata key for stroing the deployable generatename name
	DeployableVersionMeta = "x-amz-meta-deployableversion"
	//Deployable generate name key within the meta map
	DeployableMetaVersionKey = "Deployableversion"
)

// Handler handles connections to aws
type Handler struct {
	*s3.Client
}

// credentialProvider provides credetials for mcm hub deployable
type credentialProvider struct {
	AccessKeyID     string
	SecretAccessKey string
}

// Retrieve follow the Provider interface
func (p *credentialProvider) Retrieve() (aws.Credentials, error) {
	awscred := aws.Credentials{
		SecretAccessKey: p.SecretAccessKey,
		AccessKeyID:     p.AccessKeyID,
	}

	return awscred, nil
}

type DeployableObject struct {
	Name         string
	GenerateName string
	Version      string
	Content      []byte
}

func (d DeployableObject) isEmpty() bool {
	if d.Name == "" && d.GenerateName == "" && len(d.Content) == 0 {
		return true
	}

	return false
}

// InitObjectStoreConnection connect to object store
func (h *Handler) InitObjectStoreConnection(endpoint, accessKeyID, secretAccessKey string) error {
	klog.Info("Preparing S3 settings")

	cfg, err := external.LoadDefaultAWSConfig()

	if err != nil {
		klog.Error("Failed to load aws config. error: ", err)
		return err
	}
	// aws client report error without minio
	cfg.Region = "minio"

	defaultResolver := endpoints.NewDefaultResolver()
	s3CustResolverFn := func(service, region string) (aws.Endpoint, error) {
		if service == "s3" {
			return aws.Endpoint{
				URL: endpoint,
			}, nil
		}

		return defaultResolver.ResolveEndpoint(service, region)
	}

	cfg.EndpointResolver = aws.EndpointResolverFunc(s3CustResolverFn)
	cfg.Credentials = &credentialProvider{
		AccessKeyID:     accessKeyID,
		SecretAccessKey: secretAccessKey,
	}

	h.Client = s3.New(cfg)
	if h.Client == nil {
		klog.Error("Failed to connect to s3 service")
		return err
	}

	h.Client.ForcePathStyle = true

	klog.V(2).Info("S3 configured ")

	return nil
}

// Create a bucket
func (h *Handler) Create(bucket string) error {
	req := h.Client.CreateBucketRequest(&s3.CreateBucketInput{
		Bucket: &bucket,
	})

	_, err := req.Send(context.TODO())
	if err != nil {
		klog.Error("Failed to create bucket ", bucket, ". error: ", err)
		return err
	}

	return nil
}

// Exists Checks whether a bucket exists and is accessible
func (h *Handler) Exists(bucket string) error {
	req := h.Client.HeadBucketRequest(&s3.HeadBucketInput{
		Bucket: &bucket,
	})

	_, err := req.Send(context.TODO())
	if err != nil {
		klog.Error("Failed to access bucket ", bucket, ". error: ", err)
		return err
	}

	return nil
}

// List all objects in bucket
func (h *Handler) List(bucket string) ([]string, error) {
	klog.V(10).Info("List S3 Objects ", bucket)

	req := h.Client.ListObjectsRequest(&s3.ListObjectsInput{Bucket: &bucket})
	p := s3.NewListObjectsPaginator(req)

	var keys []string

	for p.Next(context.TODO()) {
		page := p.CurrentPage()
		for _, obj := range page.Contents {
			keys = append(keys, *obj.Key)
		}
	}

	if err := p.Err(); err != nil {
		klog.Error("failed to list objects. error: ", err)
		return nil, err
	}

	klog.V(10).Info("List S3 Objects result ", keys)

	return keys, nil
}

// Get get existing object
func (h *Handler) Get(bucket, name string) (DeployableObject, error) {
	dplObj := DeployableObject{}

	req := h.Client.GetObjectRequest(&s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &name,
	})

	resp, err := req.Send(context.Background())
	if err != nil {
		klog.Error("Failed to send Get request. error: ", err)
		return dplObj, err
	}

	generateName := resp.GetObjectOutput.Metadata[DployableMateGenerateNameKey]
	version := resp.GetObjectOutput.Metadata[DeployableMetaVersionKey]
	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		klog.Error("Failed to parse Get request. error: ", err)
		return dplObj, err
	}

	dplObj.Name = name
	dplObj.GenerateName = generateName
	dplObj.Content = body
	dplObj.Version = version

	klog.V(10).Info("Get Success: \n", string(body))

	return dplObj, nil
}

// Put create new object
func (h *Handler) Put(bucket string, dplObj DeployableObject) error {
	if dplObj.isEmpty() {
		klog.V(5).Infof("got an empty deployableObject to put to object store")
		return nil
	}

	req := h.Client.PutObjectRequest(&s3.PutObjectInput{
		Bucket: &bucket,
		Key:    &dplObj.Name,
		Body:   bytes.NewReader(dplObj.Content),
	})

	req.HTTPRequest.Header.Set(DeployableGenerateNameMeta, dplObj.GenerateName)
	req.HTTPRequest.Header.Set(DeployableVersionMeta, dplObj.Version)

	resp, err := req.Send(context.Background())
	if err != nil {
		klog.Error("Failed to send Put request. error: ", err)
		return err
	}

	klog.V(5).Info("Put Success", resp)

	return nil
}

// Delete delete existing object
func (h *Handler) Delete(bucket, name string) error {
	req := h.Client.DeleteObjectRequest(&s3.DeleteObjectInput{
		Bucket: &bucket,
		Key:    &name,
	})

	resp, err := req.Send(context.Background())
	if err != nil {
		klog.Error("Failed to send Delete request. error: ", err)
		return err
	}

	klog.V(10).Info("Delete Success", resp)

	return nil
}
