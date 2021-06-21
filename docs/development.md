# Development Guide

- [Development Guide](#development-guide)
    - [Required tools/Binaries](#required-tools/binaries)
    - [Launch Dev mode](#launch-dev-mode)
    - [Build a local image](#build-a-local-image)

## Required tools/Binaries

Multcloud-Operators projects are built with following tools, use following links to get them installed in your env.

Kubernetes:

- Version 1.13+. Use following [link](https://kubernetes.io/docs/setup/#learning-environment) to find an environment or setup one.

Build:

- [go v1.16+](https://golang.org/dl/)

Lint:

- [golangci-lint](https://github.com/golangci/golangci-lint#install)

Test:

- [kubebuilder v2.3.1](https://github.com/kubernetes-sigs/kubebuilder/releases/tag/v2.3.1)

## Launch Dev mode

```shell
make build
./build/_output/bin/multicloud-operators-subscription --help
```

## Build a local image

```shell
make build-images
```
