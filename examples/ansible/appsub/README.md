Prepare:
1. A open-cluster-management hub cluster with subscription operator running and at least one managed cluster. See https://open-cluster-management.io "Getting Started" 
2. An Ansible AWX/Tower environment. See https://github.com/ansible/awx for more details.

Perform:
1. Fork the https://github.com/stolostron/multicloud-operators-subscription into your personal repo.
2. Edit the `examples/ansible/appsub/EDITME02-channel.yaml` and `examples/ansible/appsub/EDITME-secret.yaml` files.
3. `git clone https://github.com/_your_github_id_/multicloud-operators-subscription.git`
4. `cd multicloud-operators-subscription`
5. `kubectl apply -f examples/ansible/appsub`
6. This will create a subscription that watches resources in https://github.com/_your_github_id_/multicloud-operators-subscription/examples/ansible/resources
