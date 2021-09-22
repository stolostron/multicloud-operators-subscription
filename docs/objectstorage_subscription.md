# Object storage channel subscription

You can subscribe to cloud object storage that contain Kubernetes resource YAML files. This document gives examples of connecting to an object storage through a channel and subscribing to Kubernetes resources from the object store.

## Supported cloud object storage

- Amazon S3
- MinIO

## Prerequisite

Ensure that you have a Kubernetes cluster that include a running instance of this subscription operator.

## Subscribing to Kubernetes resources from a cloud object storage

In the following example, you create a channel resource and a subscription resources that will help the subscription operator connects to a cloud object storage and subscribes to it.

1. Run the following command to create a `kuberesources`namespace:

   ```shell
   kubectl create ns kuberesources
   ```

1. Create an object storage channel and secret YAML files and apply them to the `kuberesources` namespace:

   The following YAML content is an example of an object storage channel:

   ```yaml
   apiVersion: apps.open-cluster-management.io/v1
   kind: Channel
   metadata:
     name:  sample-kube-resources-object
     namespace: kuberesources
   spec:
     type: ObjectBucket
     pathname: https://s3.console.aws.amazon.com/s3/buckets/<bucket-name-here> # replace <bucket-name-here> with your bucket name
     secretRef:
       name: secret-dev
   ```

   The following YAML content is an example of a secret that is used to connect to the cloud storage:

   ```yaml
   apiVersion: v1
   kind: Secret
   metadata:
     name: secret-dev
     namespace: kuberesources
   stringData:
     AccessKeyID: <access-key-id> # replace <access-key-id> with your AWS access key ID
     SecretAccessKey: <secret-access-key> # replace <secret-access-key> with your AWS secret access key
     Region: <region> # replace <region> with region
   type: Opaque
   ```

   Run the following command to apply the above YAML files to the `kuberesources` namespace:

   ```shell
   kubectl apply -f sample-kube-resources-object.yaml
   kubectl apply -f secret-dev.yaml
   ```

1. Create a subscription and apply it to the Kubernetes cluster to subscribe to the `sample-kube-resources-object` channel:

   The following YAML content is an example of a subscription that subscribes to the `sample-kube-resources-object` channel:

   ```yaml
   apiVersion: apps.open-cluster-management.io/v1
   kind: Subscription
   metadata:
     name: obj-sub
   spec:
     channel: kuberesources/sample-kube-resources-object
     placement:
       local: true
   ```

   Run the following command to apply the above YAML file to your current context default namspace:

   ```shell
   kubectl apply -f obj-sub.yaml
   ```

1. The subscription will now watch for the YAML files on the `pathname` value of `sample-kube-resources-object` channel and apply them to the Kubernetes cluster.
