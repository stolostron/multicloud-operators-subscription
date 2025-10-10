# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is the multicloud-operators-subscription project - a Kubernetes operator that enables clusters to subscribe to Git repositories, Helm registries, and object storage repositories to deploy applications. It's part of the Open Cluster Management (OCM) ecosystem and supports both standalone and multi-cluster deployments.

## Development Commands

### Building and Testing
- `make build` - Build all binaries (subscription operator, uninstall-crd, appsubsummary, placementrule)
- `make local` - Build binaries for macOS/Darwin
- `make build-images` - Build Docker images
- `make build-images-non-amd64` - Build Linux/amd64 images on non-amd64 hosts (Apple M3)
- `make test` - Run unit tests with coverage
- `make ensure-kubebuilder-tools` - Download kubebuilder tools for testing
- `make lint` or `make lint-all` - Run Go linting

### Deployment
- `make deploy-standalone` - Deploy operator in standalone mode
- `make deploy-hub` - Deploy operator on hub cluster
- `make deploy-addon` - Deploy addon to managed cluster
- `make deploy-ocm` - Install OCM foundation components

### E2E Testing
- `make e2e` - Run end-to-end tests with local kind cluster
- `make build-e2e` - Build e2e test binary
- `make test-e2e` - Run e2e tests with OCM setup
- `make test-e2e-kc` - Run e2e tests with kind cluster

### Code Generation
- `make generate` - Generate Go code using controller-gen
- `make manifests` - Generate CRDs and RBAC manifests
- `make update` - Update bindata for deployment manifests

## Architecture Overview

### Core Components

1. **Subscription Controller** (`pkg/controller/subscription/`) - Main controller for processing subscription resources on managed clusters. Handles Git, Helm, and object storage subscriptions.

2. **MCM Hub Controller** (`pkg/controller/mcmhub/`) - Hub-side controller that propagates subscriptions to managed clusters via ManifestWork resources. Handles placement logic and Git synchronization.

3. **HelmRelease Controller** (`pkg/helmrelease/`) - Manages Helm chart installations and updates. Works with both hub and managed clusters.

4. **Placement Controller** (`pkg/placementrule/`) - Legacy placement rule controller (being superseded by cluster-api placement).

5. **AppSubSummary Controller** (`pkg/controller/appsubsummary/`) - Aggregates subscription status across clusters.

### Key APIs

- **Subscription** (`pkg/apis/apps/v1/`) - Main subscription resource supporting Git, Helm, and object storage channels
- **SubscriptionStatus** (`pkg/apis/apps/v1alpha1/`) - Status reporting for subscriptions
- **HelmRelease** (`pkg/apis/apps/helmrelease/v1/`) - Helm-specific resource for chart management
- **PlacementRule** (`pkg/apis/apps/placementrule/v1/`) - Legacy placement specification

### Deployment Modes

1. **Standalone** - Single cluster deployment with subscriptions applied directly
2. **Hub-Managed** - Multi-cluster with hub orchestrating deployments to managed clusters via addon framework
3. **Addon** - Managed cluster component installed via OCM addon framework

### Channel Types

- **Git Repository** - Kubernetes YAML and Helm charts from Git repos
- **Helm Repository** - Helm charts from Helm registries
- **Object Storage** - Resources from S3-compatible storage

## File Structure

- `cmd/` - Main entry points for different binaries
- `pkg/apis/` - API definitions and generated code
- `pkg/controller/` - Controller implementations
- `pkg/helmrelease/` - Helm-specific functionality
- `addon/` - OCM addon framework integration
- `deploy/` - Kubernetes manifests for different deployment scenarios
- `test/e2e/` - End-to-end test cases

## Development Notes

- Uses controller-runtime framework for Kubernetes controllers
- Supports Kubernetes 1.28.3+ (see go.mod K8S_VERSION)
- Built with Go 1.24.0
- Integrates with OCM (Open Cluster Management) addon framework
- Helm integration uses Helm v3.18.6
- Uses kubebuilder for code generation

## Testing

The project includes comprehensive e2e tests covering:
- Git repository subscriptions with various scenarios
- Helm chart subscriptions and upgrades
- Placement and cluster targeting
- Ansible job hooks
- Metrics collection
- Error handling and status reporting

Test cases are organized in `test/e2e/cases/` with numbered directories for different scenarios.