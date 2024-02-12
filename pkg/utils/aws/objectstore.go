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

package aws

import (
	"bytes"
	"context"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"k8s.io/klog/v2"
)

// ObjectStore interface.
type ObjectStore interface {
	InitObjectStoreConnection(endpoint, accessKeyID, secretAccessKey, region string) error
	Exists(bucket string) error
	Create(bucket string) error
	List(bucket string, folderName *string) ([]string, error)
	Put(bucket string, dplObj DeployableObject) error
	Delete(bucket, name string) error
	Get(bucket, name string) (DeployableObject, error)
}

var _ ObjectStore = &Handler{}

const (
	// SecretMapKeyAccessKeyID is key of accesskeyid in secret.
	SecretMapKeyAccessKeyID = "AccessKeyID"
	// SecretMapKeySecretAccessKey is key of secretaccesskey in secret.
	SecretMapKeySecretAccessKey = "SecretAccessKey"
	// SecretMapKeyRegion is key of region in secret.
	SecretMapKeyRegion = "Region"
	// metadata key for stroing the deployable generatename name.
	DeployableGenerateNameMeta = "x-amz-meta-generatename"
	// Deployable generate name key within the meta map.
	DployableMateGenerateNameKey = "Generatename"
	// metadata key for stroing the deployable generatename name.
	DeployableVersionMeta = "x-amz-meta-deployableversion"
	// Deployable generate name key within the meta map.
	DeployableMetaVersionKey = "Deployableversion"
)

// Handler handles connections to aws.
type Handler struct {
	*s3.Client
}

// credentialProvider provides credetials for mcm hub deployable.
type credentialProvider struct {
	Value aws.Credentials
}

// Retrieve follow the Provider interface.
func (p credentialProvider) Retrieve(ctx context.Context) (aws.Credentials, error) {
	awscred := aws.Credentials{
		SecretAccessKey: p.Value.SecretAccessKey,
		AccessKeyID:     p.Value.AccessKeyID,
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

func isAwsS3ObjectBucket(endpoint string) bool {
	if strings.Contains(strings.ToLower(endpoint), strings.ToLower("s3://")) {
		return true
	}

	if strings.Contains(strings.ToLower(endpoint), strings.ToLower("s3")) &&
		strings.Contains(strings.ToLower(endpoint), strings.ToLower("aws")) {
		return true
	}

	return false
}

// InitObjectStoreConnection connect to object store.
func (h *Handler) InitObjectStoreConnection(endpoint, accessKeyID, secretAccessKey, region string) error {
	klog.Infof("Preparing S3 settings endpoint: %v", endpoint)

	// set the default object store region  as minio
	objectRegion := "minio"

	if isAwsS3ObjectBucket(endpoint) {
		objectRegion = region
	}

	// aws s3 object store doesn't need to specify URL.
	// minio object store needs immutable URL. The aws sdk is not allowed to modify the host name of the minio URL
	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		klog.V(1).Infof("service: %v, region: %v", service, region)
		if region == "minio" {
			return aws.Endpoint{
				URL:               endpoint,
				HostnameImmutable: true,
			}, nil
		}
		return aws.Endpoint{}, &aws.EndpointNotFoundError{}
	})

	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithEndpointResolverWithOptions(customResolver))
	if err != nil {
		klog.Error("Failed to load aws config. error: ", err)

		return err
	}

	objCredential := credentialProvider{
		Value: aws.Credentials{
			AccessKeyID:     accessKeyID,
			SecretAccessKey: secretAccessKey,
		},
	}

	h.Client = s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.Region = objectRegion
		o.Credentials = objCredential
	})

	if h.Client == nil {
		klog.Error("Failed to connect to s3 service")

		return err
	}

	klog.V(1).Info("S3 configured ")

	return nil
}

// Create a bucket.
func (h *Handler) Create(bucket string) error {
	resp, err := h.Client.CreateBucket(context.TODO(), &s3.CreateBucketInput{
		Bucket: &bucket,
	})
	if err != nil {
		klog.Error("Failed to create bucket ", bucket, ". error: ", err)

		return err
	}

	klog.Infof("resp: %#v", resp)

	return nil
}

// Exists Checks whether a bucket exists and is accessible.
func (h *Handler) Exists(bucket string) error {
	_, err := h.Client.HeadBucket(context.TODO(), &s3.HeadBucketInput{
		Bucket: &bucket,
	})

	if err != nil {
		klog.Error("Failed to access bucket ", bucket, ". error: ", err)

		return err
	}

	return nil
}

// List all objects in bucket.
func (h *Handler) List(bucket string, folderName *string) ([]string, error) {
	klog.V(1).Info("List S3 Objects ", bucket)

	if folderName != nil {
		tmpFolderName := *folderName
		if len(tmpFolderName) > 0 && tmpFolderName[len(tmpFolderName)-1:] != "/" {
			tmpFolderName += "/"
		}

		folderName = &tmpFolderName
	}

	params := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: folderName,
	}

	paginator := s3.NewListObjectsV2Paginator(h.Client, params, func(o *s3.ListObjectsV2PaginatorOptions) {
		o.Limit = 1000
	})

	var keys []string

	var objErr error

	pageNum := 0

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(context.TODO())
		if err != nil {
			klog.Infof("Got error retrieving list of objects. err: %v", err)
			objErr = err

			break
		}

		for _, value := range output.Contents {
			key := *value.Key
			if len(key) > 0 && key[len(key)-1:] != "/" {
				keys = append(keys, *value.Key)
			} else {
				klog.V(1).Info("Skipping S3 Object: ", key)
			}
		}
		pageNum++
	}

	klog.Infof("List S3 Objects result, page Num: %v, keys: %v, err: %v ", pageNum, keys, objErr)

	return keys, objErr
}

// Get get existing object.
func (h *Handler) Get(bucket, name string) (DeployableObject, error) {
	dplObj := DeployableObject{}

	resp, err := h.Client.GetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &name,
	})
	if err != nil {
		klog.Error("Failed to send Get request. error: ", err)

		return dplObj, err
	}

	generateName := resp.Metadata[DployableMateGenerateNameKey]
	version := resp.Metadata[DeployableMetaVersionKey]
	body, err := io.ReadAll(resp.Body)

	if err != nil {
		klog.Error("Failed to parse Get request. error: ", err)

		return dplObj, err
	}

	if len(body) == 0 {
		return DeployableObject{}, nil
	}

	dplObj.Name = name
	dplObj.GenerateName = generateName
	dplObj.Content = body
	dplObj.Version = version

	klog.V(1).Info("Get Success: \n", string(body))

	return dplObj, nil
}

// Put create new object.
func (h *Handler) Put(bucket string, dplObj DeployableObject) error {
	if dplObj.isEmpty() {
		klog.V(1).Infof("got an empty deployableObject to put to object store")

		return nil
	}

	resp, err := h.Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: &bucket,
		Key:    &dplObj.Name,
		Body:   bytes.NewReader(dplObj.Content),
	})
	if err != nil {
		klog.Error("Failed to send Put request. error: ", err)

		return err
	}

	klog.V(5).Info("Put Success", resp)

	return nil
}

// Delete delete existing object.
func (h *Handler) Delete(bucket, name string) error {
	resp, err := h.Client.DeleteObject(context.TODO(), &s3.DeleteObjectInput{
		Bucket: &bucket,
		Key:    &name,
	})
	if err != nil {
		klog.Error("Failed to send Delete request. error: ", err)

		return err
	}

	klog.V(1).Info("Delete Success", resp)

	return nil
}
