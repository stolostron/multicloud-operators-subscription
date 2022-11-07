Prepare:
1. An Open Cluster Management hub cluster with subscription operator running and at least one managed cluster with subscription add-on enable. See https://open-cluster-management.io "Get Started" for more details.
2. An Ansible AWX/Tower environment. See https://github.com/ansible/awx for more details.

Perform:
1. Edit the `examples/ansible-tags/appsub/EDITME-secret.yaml` file.
2. `git clone https://github.com/open-cluster-management-io/multicloud-operators-subscription.git`
3. `cd multicloud-operators-subscription`
4. `kubectl apply -f examples/ansible-tags/appsub`
5. This will create a subscription that watches resources in https://github.com/open-cluster-management-io/multicloud-operators-subscription/tree/main/examples/ansible-pre-workflow/resources
