FROM registry.access.redhat.com/ubi8/ubi-minimal:latest

RUN  microdnf update -y \ 
        && rpm -e --nodeps tzdata \
        && microdnf install tzdata \
        && microdnf install git \
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
     
ENTRYPOINT ["/usr/local/bin/entrypoint"]

USER ${USER_UID}
