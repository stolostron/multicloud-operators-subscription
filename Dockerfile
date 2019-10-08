FROM scratch

COPY build/_output/bin/multicloud-operators-subscription .
CMD ["./multicloud-operators-subscription"]
