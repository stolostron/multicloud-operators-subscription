FROM registry.ci.openshift.org/stolostron/builder:go1.17-linux AS plugin-builder
ENV POLICY_GENERATOR_TAG=v1.7.0

WORKDIR /policy-generator
RUN curl -L -o "policy-generator-plugin.tar.gz" \
        "https://github.com/stolostron/policy-generator-plugin/archive/refs/tags/${POLICY_GENERATOR_TAG}.tar.gz"
RUN tar -xzvf "policy-generator-plugin.tar.gz"
RUN cd "/policy-generator/policy-generator-plugin-${POLICY_GENERATOR_TAG#*v}" && \
        make build-binary && \
        mv "PolicyGenerator" "/policy-generator/"

FROM registry.access.redhat.com/ubi8/ubi-minimal:latest

RUN  microdnf update -y \ 
        && rpm -e --nodeps tzdata \
        && microdnf install tzdata \
        && microdnf install git-core \
        && microdnf install openssh-clients \
        && microdnf clean all

ENV OPERATOR=/usr/local/bin/multicluster-operators-subscription \
    USER_UID=1001 \
    USER_NAME=multicluster-operators-subscription \
    ZONEINFO=/usr/share/timezone \
    KUSTOMIZE_PLUGIN_HOME=/etc/kustomize/plugin

# install operator binary
COPY build/_output/bin/multicluster-operators-subscription ${OPERATOR}
COPY build/_output/bin/multicluster-operators-placementrule /usr/local/bin
COPY build/_output/bin/uninstall-crd /usr/local/bin
COPY build/_output/bin/appsubsummary /usr/local/bin

# install the policy generator Kustomize plugin
RUN mkdir -p $KUSTOMIZE_PLUGIN_HOME/policy.open-cluster-management.io/v1/policygenerator
COPY --from=plugin-builder /policy-generator/PolicyGenerator $KUSTOMIZE_PLUGIN_HOME/policy.open-cluster-management.io/v1/policygenerator/PolicyGenerator

COPY build/bin /usr/local/bin
RUN  /usr/local/bin/user_setup
     
ENTRYPOINT ["/usr/local/bin/entrypoint"]

USER ${USER_UID}
