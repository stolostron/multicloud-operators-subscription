# v0.6.0

## What's Changed
* do not set ownerref if resource is deployed into different namespace by @rokej in https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/38
* Fix Helm subscription resource list population when charts contains CR(s) that the Hub cluster doesn't have CRD  by @mikeshng in https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/39
* Update securitymd to refer to the  OCM community security md file by @mikeshng in https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/40
* fix skip ssl by @rokej in https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/41
* Update go version to 1.17 by @mikeshng in https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/42
* PlacementRule controller generate PlacementDecisions  by @mikeshng in https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/44
* Fix Helm subscription error not being reported in status by @mikeshng in https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/47
* Use a better semver library to handle version parsing to deal with version that starts with a character v by @mikeshng in https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/49
* fix SSH host key scan by @rokej in https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/50
* Add a warning message when helm subscription fails to find a matching helm chart to deploy by @mikeshng in https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/51
* Fix subscription spec changes not triggering Helm appsub updates by @mikeshng in https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/54
* Use PlacementDecision to determine app placement instead of PlacementRule by @mikeshng in https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/55
* Adopt FOSSA scan. by @mikeshng in https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/56
* Don't watch placement decision api until it is installed; skip updati… by @xiangjingli in https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/58
* fix git annotation change by @rokej in https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/59
* add Git mTLS connection support by @rokej in https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/62
* Fix fossa scan by @mikeshng in https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/61
* Remove legacy auto gen file that is no longer in use by @dhaiducek in https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/60
* Update PROW test Git repo URL. by @mikeshng in https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/64
* Update the Policy Generator GitHub URLs to use the stolostron org by @mprahl in https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/65
* Fix bindata for addon deployment by @mikeshng in https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/66
* cve fix by @xiangjingli in https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/67
* Add new e2e framework for kubectl related testing. Add placement API tests by @mikeshng in https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/68
* Update build related files by @mikeshng in https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/69

## New Contributors
* @dhaiducek made their first contribution in https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/60
* @mprahl made their first contribution in https://github.com/open-cluster-management-io/multicloud-operators-subscription/pull/65

**Full Changelog**: https://github.com/open-cluster-management-io/multicloud-operators-subscription/compare/v0.5.0...v0.6.0
