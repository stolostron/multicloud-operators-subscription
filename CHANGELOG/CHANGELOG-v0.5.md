# Changelog since v0.4.0
All notable changes to this project will be documented in this file.

## v0.5.0

### New Features 
For improvement to scaling. ([@xiangjingli](https://github.com/xiangjingli) [@philipwu08](https://github.com/philipwu08)) [#35](https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/35)
- Propagate application subscription to managed clusters using work API.
- Introduce SubscriptionReport CRD to provide the results of individual subscriptions on the hub.
- Introduce SubscriptionStatus CRD to represents the status of all the resources deployed of a subscription on a cluster.
- Add new subscription summary aggregation controller for aggregating all cluster subscriptionReports to app subscriptionReport.
- Rename most references of multicloud to multicluster.
- Separate out placementrule entry into its own binary.


### Added
- Add secondary channel support for subscription. ([@rokej](https://github.com/rokej)) [#25](https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/25)
- Add allowable and deny list support for subscription. ([@rokej](https://github.com/rokej)) [#35](https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/35)

### Changes
- Rename sample for order when applied. ([@vincent-pli](https://github.com/vincent-pli)) [#34](https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/34)
- Misc doc related updates. ([@vincent-pli](https://github.com/vincent-pli) [@wzhanw](https://github.com/wzhanw))

### Bug Fixes
- Remove owner reference from managed cluster subscription. ([@mikeshng](https://github.com/mikeshng)) [#33](https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/33)
- Fix when Git subscription fails on managed cluster, host subscription is not updated. ([@rokej](https://github.com/rokej)) [#22](https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/22)
- Fix AWS S3 listing objects to make sure all objects (> 1000) is able to be fetched. ([@rokej](https://github.com/rokej)) [#21](https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/21)
- Fix after Git application deployment, do not remove application resources when kustomization build fails. ([@rokej](https://github.com/rokej)) [#20](https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/20)

### Removed & Deprecated
- Remove deployable API dependency ([@xiangjingli](https://github.com/xiangjingli) [@philipwu08](https://github.com/philipwu08) [@mikeshng](https://github.com/mikeshng)) [#35](https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/35)
