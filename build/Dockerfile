FROM registry.access.redhat.com/ubi8/ubi-minimal:latest

RUN  microdnf update -y \ 
        && rpm -e --nodeps tzdata \
        && microdnf install tzdata \
        && microdnf install git \
        && microdnf install openssh-clients \
        && microdnf clean all

ENV OPERATOR=/usr/local/bin/multicloud-operators-subscription \
    USER_UID=1001 \
    USER_NAME=multicloud-operators-subscription \
    ZONEINFO=/usr/share/timezone

# install operator binary
COPY build/_output/bin/multicloud-operators-subscription ${OPERATOR}
COPY build/_output/bin/uninstall-crd /usr/local/bin

COPY build/bin /usr/local/bin
RUN  /usr/local/bin/user_setup
     
ENTRYPOINT ["/usr/local/bin/entrypoint"]

USER ${USER_UID}
