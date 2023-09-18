# Patching ACM hub and managed clusters with another subscription container images

## Patching hub cluster and managed clusters together (ACM >= 2.8)

To patch the subscription image, here are the steps:

`quay.io/xiangjingli/multicloud-operators-subscription@sha256:51f12144c277e33b34c18295468a7f375a2261eafc124b1f427253d3924c4867`

- On the hub, Get the namespace and name of the MCH resource
```
% oc get mch -A
NAMESPACE                 NAME              STATUS    AGE
open-cluster-management   multiclusterhub   Running   16h
```

- Create a ConfigMap to reference the images provided in the hotfix
```
$ oc apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: redhat-acm-hotfix-mintls12
  namespace: open-cluster-management      # this is the MCH namespace
  labels:
    operator.multicluster.openshift.io/hotfix: redhat-acm-hotfix-mintls12
data:
  manifest.json: |-
    [
      {
        "image-remote": "quay.io/xiangjingli",
        "image-key":    "multicluster_operators_subscription",
        "image-name":   "multicloud-operators-subscription",
        "image-digest": "sha256:51f12144c277e33b34c18295468a7f375a2261eafc124b1f427253d3924c4867"
      }
    ]
EOF
```

- Activate the hotfix by applying an annotation to the MCH resource for overriding the images specified in the configmap
```
$ oc -n open-cluster-management annotate mch multiclusterhub --overwrite mch-imageOverridesCM=redhat-acm-hotfix-mintls12
```

- The following hub subscription pods are expected to be restarted and running with the new hot fix image
```
% oc get pods -n open-cluster-management |grep subscription
multicluster-operators-hub-subscription-5cfdf4bb84-xcc9z          1/1     Running   0             50m
multicluster-operators-standalone-subscription-5467dcdbcc-2w8l2   1/1     Running   0             50m
multicluster-operators-subscription-report-57b776ccf9-ktvph       1/1     Running   0             50m
```

- (optional) Restart the RHACM operator on the hub
if hub subscription pods are not restarted after a while, restart the RHACM operator pods to ensure that the operator picks up the hotfix configuration
```
$ oc -n open-cluster-management scale deployment multiclusterhub-operator --replicas=0
$ oc -n open-cluster-management scale deployment multiclusterhub-operator --replicas=1
```

- Go to all managed clusters, make sure the following application-manager pod is restarted and running with the new hot fix image. This may take a while
```
% oc get pods -n open-cluster-management-agent-addon |grep application-manager
application-manager-bd4f7c5db-zvsvx            1/1     Running   0          50m
```

## Patching hub cluster (ACM <= 2.4)

In `open-cluster-management` namespace on ACM hub cluster, edit the advanced-cluster-management.v2.3.0 csv. (or 2.3.2 CSV)

```
oc edit csv advanced-cluster-management.v2.3.0 -n open-cluster-management
```

Look for containers **multicluster-operators-standalone-subscription** and **multicluster-operators-hub-subscription** and update their images to `quay.io/open-cluster-management/multicluster-operators-subscription:TAG` (it is recommended you note the current **SHA** tag if you want to revert the change). Replace `TAG` with the actual image tag (use `latest` to get the latest upstream version). This will recreate `multicluster-operators-standalone-subscription-xxxxxxx` and `multicluster-operators-hub-subscription-xxxxxxx` pods in `open-cluster-management` namespace. Check that the new pods are running with the new container image.

## Patching managed clusters (ACM <= 2.4)

If you are patching `local-cluster` managed cluster, which is the ACM hub cluster itself, run this command.

```
oc annotate -n open-cluster-management `oc get mch -oname -n open-cluster-management | head -n1` mch-pause=true --overwrite=true
```

Then update the images on managed-clusters. On the ACM hub cluster, run this command while replacing CLUSTER_NAME with the actual managed-cluster name.

```
oc annotate klusterletaddonconfig -n CLUSTER_NAME CLUSTER_NAME klusterletaddonconfig-pause=true --overwrite=true
```

Then run:

```
oc edit manifestwork -n CLUSTER_NAME CLUSTER_NAME-klusterlet-addon-appmgr 
```

Replace **CLUSTER_NAME** with the actual managed cluster name. Look for spec.global.imageOverrides.multicluster_operators_subscription. Set the value to `quay.io/open-cluster-management/multicluster-operators-subscription:TAG` (it is recommended you note the current **SHA** tag if you want to revert the change). Replace `TAG` with the actual image tag (use `latest` to get the latest upstream version).  This will recreate the `klusterlet-addon-appmgr-xxxxxx` pod in `open-cluster-management-agent-addon` namespace on the managed cluster. Check that the new pod is running with the new docker image.
