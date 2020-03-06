FROM registry.access.redhat.com/ubi8/ubi-minimal:8.1-398

ENV OPERATOR=/usr/local/bin/multicloud-operators-subscription \
    USER_UID=1001 \
    USER_NAME=multicloud-operators-subscription

# install operator binary
COPY build/_output/bin/multicloud-operators-subscription ${OPERATOR}

COPY build/bin /usr/local/bin
RUN  /usr/local/bin/user_setup

ENTRYPOINT ["/usr/local/bin/entrypoint"]

USER ${USER_UID}
