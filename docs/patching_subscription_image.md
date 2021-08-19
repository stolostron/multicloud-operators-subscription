# Patching ACM hub and managed clusters with another subscription container images

## Patching hub cluster

In `open-cluster-management` namespace on ACM hub cluster, edit the advanced-cluster-management.v2.1.0 csv. (or 2.1.1 CSV)

```
oc edit csv advanced-cluster-management.v2.1.0 -n open-cluster-management
```

Look for containers **multicluster-operators-standalone-subscription** and **multicluster-operators-hub-subscription** and update their images to `quay.io/open-cluster-management/multicluster-operators-subscription:TAG` (it is recommended you note the current **SHA** tag if you want to revert the change). Replace `TAG` with the actual image tag (use `latest` to get the latest upstream version). This will recreate `multicluster-operators-standalone-subscription-xxxxxxx` and `multicluster-operators-hub-subscription-xxxxxxx` pods in `open-cluster-management` namespace. Check that the new pods are running with the new container image.

## Patching managed clusters

Then update the images on managed-clusters. On the ACM hub cluster, run this command while replacing CLUSTER_NAME with the actual managed-cluster name.

```
oc annotate klusterletaddonconfig -n CLUSTER_NAME CLUSTER_NAME klusterletaddonconfig-pause=true --overwrite=true
```

Then run:

```
oc edit manifestwork -n CLUSTER_NAME CLUSTER_NAME-klusterlet-addon-appmgr 
```

Replace **CLUSTER_NAME** with the actual managed cluster name. Look for spec.global.imageOverrides.multicluster_operators_subscription. Set the value to `quay.io/open-cluster-management/multicluster-operators-subscription:TAG` (it is recommended you note the current **SHA** tag if you want to revert the change). Replace `TAG` with the actual image tag (use `latest` to get the latest upstream version).  This will recreate the `klusterlet-addon-appmgr-xxxxxx` pod in `open-cluster-management-agent-addon` namespace on the managed cluster. Check that the new pod is running with the new docker image.
