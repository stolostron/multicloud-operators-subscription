FROM registry.access.redhat.com/ubi8/ubi-minimal:latest

RUN  microdnf update -y \ 
        && rpm -e --nodeps tzdata \
        && microdnf install tzdata \
        && microdnf install git \
        && microdnf install tar \
        && microdnf clean all

ENV OPERATOR=/usr/local/bin/multicluster-operators-subscription \
    USER_UID=1001 \
    USER_NAME=multicluster-operators-subscription \
    ZONEINFO=/usr/share/timezone


# install operator binary
COPY build/_output/bin/multicluster-operators-subscription ${OPERATOR}
COPY build/_output/bin/uninstall-crd /usr/local/bin

COPY build/bin /usr/local/bin
RUN  /usr/local/bin/user_setup

# install kustomize
ENV KUSTOMIZE_VERSION 3.8.1
RUN curl -L https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2Fv${KUSTOMIZE_VERSION}/kustomize_v${KUSTOMIZE_VERSION}_linux_amd64.tar.gz -o /tmp/kustomize.tar.gz \
     && tar zxvf /tmp/kustomize.tar.gz -C /usr/bin \
     && chmod +x /usr/bin/kustomize
     
ENTRYPOINT ["/usr/local/bin/entrypoint"]

USER ${USER_UID}
