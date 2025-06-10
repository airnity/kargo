# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Kargo is a next-generation continuous delivery and application lifecycle orchestration platform for Kubernetes. It builds upon GitOps principles and integrates with existing technologies like Argo CD to streamline progressive rollout of changes across application lifecycle stages.

## Essential Commands

### Development Workflow
```bash
# Start local development cluster
make hack-kind-up    # or hack-k3d-up for k3d
make hack-install-prereqs  # Installs cert-manager, Argo CD, Argo Rollouts

# Local development with live reload
tilt up  # Uses Tiltfile for hot reloading

# Access points:
# - Kargo API: localhost:30081
# - Kargo UI: localhost:30082  
# - External webhooks: localhost:30083
# - Argo CD: localhost:30080

# CLI login for testing
bin/kargo-<os>-<arch> login http://localhost:30081 --admin --password admin --insecure-skip-tls-verify

# Clean up
make hack-kind-down  # or hack-k3d-down
```

### Build & Test
```bash
# Run all linters (Go, proto, charts, UI)
make lint  # or make hack-lint (containerized)

# Format code
make format  # or make hack-format (containerized)

# Run unit tests
make test-unit  # or make hack-test-unit (containerized)

# Build CLI binary
make build-cli
# or with UI embedded:
make build-cli-with-ui

# Build container image
make hack-build

# Code generation (after API changes)
make codegen  # or make hack-codegen (containerized)
```

### UI Development
```bash
# UI-specific commands (from ui/ directory)
pnpm install
pnpm run dev                    # Development server
pnpm run build                  # Production build
pnpm run lint                   # ESLint
pnpm run typecheck             # TypeScript checking
pnpm run generate:schema       # Generate schemas
```

### Documentation
```bash
# Serve docs locally
make serve-docs  # or make hack-serve-docs (containerized)
# Accessible at localhost:3000
```

## Architecture Overview

### Main Components
1. **CLI** (`cmd/cli/`) - Command-line interface for interacting with Kargo
2. **Control Plane** (`cmd/controlplane/`) - Core server components:
   - API server
   - Controller
   - External webhooks server
   - Kubernetes webhooks server
   - Management controller
   - Garbage collector
3. **UI** (`ui/`) - React-based web dashboard using Vite, TypeScript, and Ant Design
4. **Credential Helper** (`cmd/credential-helper/`) - Handles credential management

### Code Organization
- **`cmd/`** - Main applications (CLI, control plane components)
- **`internal/`** - Private packages organized by domain (api, controller, server, promotion, etc.)
- **`pkg/`** - Public packages for external consumption
- **`api/`** - Kubernetes API types and protobuf definitions
- **`charts/kargo/`** - Helm chart for deployment

### Technology Stack
- **Backend**: Go 1.24.3+ with Kubernetes controller-runtime, gRPC/Connect, protobuf
- **Frontend**: React 19 + TypeScript + Vite + Ant Design + TanStack Query
- **Testing**: Standard Go testing + testify, comprehensive test coverage
- **Development**: Tilt for live reload, kind/k3d for local clusters

### Development Patterns
- Multi-module Go project (main module + api/ + pkg/)
- Container-based development workflow with Dockerfile.dev
- Co-located test files (`*_test.go`)
- Comprehensive linting with golangci-lint
- Code generation for protobuf and Kubernetes types

### Key Integration Points
- **Argo CD**: GitOps workflow integration
- **Argo Rollouts**: Progressive deployment strategies
- **Helm**: Package management and templating
- **Kubernetes**: Native CRDs and controllers
- **Git Providers**: GitHub, GitLab, Bitbucket, etc.

## Commands You Should Run After Making Changes
- `make lint` - Always run linters before committing
- `make test-unit` - Run unit tests
- `make codegen` - After changing API types or protobuf definitions
- `pnpm run typecheck` - For UI changes (from ui/ directory)