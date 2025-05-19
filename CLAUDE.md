# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Kargo is a GitOps and continuous delivery orchestration platform that complements Argo CD. It focuses on promoting application versions through environments (e.g., dev to staging to production) with verification, approval workflows, and automated gates.

## Common Development Commands

### Development Environment Setup

```bash
# Create a local Kubernetes cluster with k3d
make k3d-create

# Install Kargo in development mode
make dev-install

# Start Tilt for live development (in a separate terminal)
tilt up
```

### Building

```bash
# Build all binaries
make build

# Build Docker images
make images

# Build specific component
make build-cli  # CLI binary
make build-controlplane  # Control plane binary
```

### Testing

```bash
# Run all tests
make test

# Run tests with coverage
make test-coverage

# Run unit tests for a specific package
go test -v ./internal/controller/promotions

# Run a specific test
go test -v ./internal/controller/promotions -run TestPromotionReconciler_Reconcile
```

### Linting

```bash
# Run linting
make lint
```

### Code Generation

```bash
# Generate code (proto, deepcopy methods, etc.)
make codegen

# Run all code generation/verification checks
make verify
```

### UI Development

```bash
# Run UI development server
cd ui && pnpm install && pnpm dev
```

### Documentation

```bash
# Run local docs development server
cd docs && pnpm install && pnpm start
```

## Architecture Overview

Kargo consists of several components:

1. **Kubernetes Custom Resources** - Core abstractions like:
   - `Project` - Top-level grouping of resources
   - `Warehouse` - Source of artifacts (Git repo, image registry, Helm repo)
   - `Stage` - Deployment environment
   - `Freight` - Bundle of artifacts to be promoted 
   - `Promotion` - Movement of Freight between Stages

2. **Control Plane** - Main controller running several controllers:
   - `ProjectController` - Manages Projects and related resources
   - `WarehouseController` - Discovers artifacts from Warehouses
   - `StageController` - Reconciles Stages and handles verification
   - `PromotionController` - Handles promotion execution
   - `ManagementController` - Manages namespace isolation, RBAC, etc.

3. **API Server** - gRPC/Connect-based API with HTTP/JSON support that serves:
   - CLI requests
   - UI requests
   - Webhook requests

4. **CLI (kargo)** - Command-line tool for interacting with Kargo

5. **UI** - Web dashboard for visualizing and managing Kargo resources

## Key Workflows

### Promotion Workflow

The core Kargo workflow:

1. Warehouses monitor artifact sources (Git, image registries, etc.)
2. Freight is created representing a bundle of artifacts
3. Freight is promoted to Stages via Promotions
4. Verifications (optional) run to validate the deployment
5. Automated Promotions can be triggered based on conditions
6. User approvals can be required before promotion

### Development Workflow

For working on the Kargo codebase:

1. Make code changes
2. Use Tilt for live reloading during development
3. Tests should be written or updated for new functionality
4. Documentation should be updated for user-facing changes
5. Follow semantic versioning for contributions

## Development Setup Tips

### Local Cluster

Kargo development uses either k3d or kind for local Kubernetes clusters:

```bash
# k3d cluster
make k3d-create

# kind cluster
make kind-create
```

### Using Tilt

Tilt provides hot-reloading of components during development:

```bash
# Start Tilt
tilt up
```

### Accessing Services

After starting Tilt:
- Kargo UI: http://localhost:9000
- Argo CD Dashboard: http://localhost:8080
- CLI: `export KARGO_SERVER=localhost:9001`

### Troubleshooting

If you encounter issues with the development environment:

```bash
# Restart Tilt
tilt down
tilt up

# Rebuild the development environment
make dev-uninstall
make dev-install
```