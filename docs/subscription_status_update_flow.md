# What does the status updates do we need for a subscription
1. update the status of the subscription itself
2. update the status of the subscribe resources to the subscription
3. aggregates the spoke subscription status to hub subscription


# Data structure of the subscription status

As you can see we use a nested map structure to model the hub to many spoke
cluster case.

```go
// SubscriptionUnitStatus defines status of a unit (subscription or package)
type SubscriptionUnitStatus struct {
	// Phase are Propagated if it is in hub or Subscribed if it is in endpoint
	Phase          SubscriptionPhase `json:"phase,omitempty"`
	Message        string            `json:"message,omitempty"`
	Reason         string            `json:"reason,omitempty"`
	LastUpdateTime metav1.Time       `json:"lastUpdateTime"`

	ResourceStatus *runtime.RawExtension `json:"resourceStatus,omitempty"`
}

// SubscriptionPerClusterStatus defines status for subscription in each cluster, key is package name
type SubscriptionPerClusterStatus struct {
	SubscriptionPackageStatus map[string]*SubscriptionUnitStatus `json:"packages,omitempty"`
}

// SubscriptionClusterStatusMap defines per cluster status, key is cluster name
type SubscriptionClusterStatusMap map[string]*SubscriptionPerClusterStatus

// SubscriptionStatus defines the observed state of Subscription
// Examples - status of a subscription on hub
//Status:
// 	phase: Propagated
// 	statuses:
// 	  washdc:
// 		packages:
// 		  nginx:
// 			phase: Subscribed
// 		  mongodb:
// 			phase: Failed
// 			Reason: "not authorized"
// 			Message: "user xxx does not have permission to start pod"
//			resourceStatus: {}
//    toronto:
// 		packages:
// 		  nginx:
// 			phase: Subscribed
// 		  mongodb:
// 			phase: Subscribed
//Status of a subscription on managed cluster will only have 1 cluster in the map.
type SubscriptionStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Phase          SubscriptionPhase `json:"phase,omitempty"`
	Message        string            `json:"message,omitempty"`
	Reason         string            `json:"reason,omitempty"`
	LastUpdateTime metav1.Time       `json:"lastUpdateTime,omitempty"`

	// For endpoint, it is the status of subscription, key is packagename,
	// For hub, it aggregates all status, key is cluster name
	Statuses SubscriptionClusterStatusMap `json:"statuses,omitempty"`
}
```

For the hub perspective, you will have the status of yourself at the
subscriptionStatus level, then for each of the spoke cluster, its status will be
put into the `map[string]*SubscriptionPerClusterStatus`. 

For example, we have a hub cluster `hub-test` and a spoke cluster `spoke-test`.
Then the status will be from hub perspective would be:

```
//Status:
// 	phase: Propagated
// 	statuses:
// 	  spoke-test:
// 		packages:
// 		  nginx:
// 			phase: Subscribed
// 		  mongodb:
// 			phase: Failed
// 			Reason: "not authorized"
// 			Message: "user xxx does not have permission to start pod"
//			resourceStatus: {}
```

At the meantime, the status of the spoke cluster should be:
```
//Status:
// 	phase: Propagated
// 	statuses:
// 	  /:
// 		packages:
// 		  nginx:
// 			phase: Subscribed
// 		  mongodb:
// 			phase: Failed
// 			Reason: "not authorized"
// 			Message: "user xxx does not have permission to start pod"
//			resourceStatus: {}
```

# Update or aggregate logic
1. update the status of the subscription itself 
   reconcile 
   `pkg.controller.Subscription.Reconcile()`

   In addition to the reconcile logic, we are having 
   `pkg.utils.SetInClusterPackageStatus()` for updating the status as well.
   
2. update the status of the subscribe resources to the subscription 
   `pkg.synchronizer.Extension.UpdateHost()`
   
   
3. aggregates the spoke subscription status to hub subscription 
   `pkg.synchronizer.Extension.UpdateHost()`

## we can put the status update into the following time frame 
1. accept subscription, a user submit a subscription
2. valid subscription, such as if the referred channel is valid
3. load deployables, such as download object from object bucket and put it to
   deployable
4. send deployables, send deployables to synchronizer
5. process template, unpack the template from deployable and process logic given
   the deployable annotations.
   
## time frame with status update logic
1. accept subscription and valid subscription, we rely on the reconcile logic to
   update the subscription.status

2. load deployables, we rely on the `pkg.utils.SetInClusterPackageStatus` to
   update the subscription.Status. However, this logic doesn't have a actual
   update action against the cluster. It's mainly update the status at the
   subscribe map. 

3. send deployables, we don't need to update the subscription status.
4. process template, at this point, the synchronizer will invoke
   `pkg.synchronizer.Extension.UpdateHost()` to update the status of a give
   resource status to the subscription.
  
```go
// UpdateHostSubscriptionStatus defines update host status function for deployable
func (se *SubscriptionExtension) UpdateHostStatus(actionerr error, tplunit *unstructured.Unstructured, status interface{}, deletePkg bool) error {
	// parseing host from the syncsource annotation
	host := se.GetHostFromObject(tplunit)
	if host == nil || host.String() == "/" {
		return utils.UpdateDeployableStatus(se.remoteClient, actionerr, tplunit, status)
	}

	return utils.UpdateSubscriptionStatus(se.localClient, actionerr, tplunit, status, deletePkg)
}
``` 

As you can see from the above function, the synchronizer will try to decide if
the template unit belong to host "/" or not. 

If a template unit belongs to "/", meaning the template should be update to hub
cluster, therefor, `UpdateHostStatus` will update the incoming status to the
host deployable via the hub client `se.remoteClient`.

On the other hand, if the host is not "/", then the `UpdateHostStatus` will
update the status to a subscription sitting in the current cluster. 


Here're a list of the caller of `UpdateHostStatus`:
```
func (sync *KubeSynchronizer) checkServerObjects(gvk schema.GroupVersionKind,
res *ResourceMap) error{}

func (sync *KubeSynchronizer) createNewResourceByTemplateUnit(ri
dynamic.ResourceInterface, tplunit *TemplateUnit) error {}

func (sync *KubeSynchronizer) updateResourceByTemplateUnit(ri dynamic.ResourceInterface,
	obj *unstructured.Unstructured, tplunit *TemplateUnit, isService bool) error
	{}

func (sync *KubeSynchronizer) DeRegisterTemplate(host, dpl types.NamespacedName,
source string) error {}
```

The deployable controller at spoke cluster will requeue and sync the
subscription status to the hub's deployable status.
