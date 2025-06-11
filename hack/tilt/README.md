# Tilt Development Environment

This directory contains configuration for the Tilt development environment.

## Mock Services

### Mock Airnity Server

A mock server is included to test the `airnity-render` promotion runner:

- **Service**: `mock-airnity-server` (in `kargo` namespace)
- **Port Forward**: `localhost:30084`
- **Internal URL**: `http://app-generator.admin.<env>.airnity.private`

#### Testing the Mock Server

```bash
# Test the mock server directly
./hack/mock-airnity-server/test-server.sh

# Test the airnity-render runner end-to-end
./hack/examples/test-airnity-runner.sh
```

#### Service Discovery

The mock server is deployed in the `kargo` namespace with the service name `app-generator`:

- `app-generator.kargo.svc.cluster.local` - Service endpoint

The airnity-render runner connects directly to this service endpoint, simplifying the configuration and removing the need for multiple namespaces.

## Configuration Files

- `values.dev.yaml` - Helm values for development
- `ui.yaml` - UI deployment configuration  
- `mock-airnity-server.yaml` - Mock airnity server deployment

## Example Resources

Tilt also deploys example Kargo resources from `hack/examples/` for testing:

- **Examples Label Group**: Contains test Project, Warehouse, and Stage resources
- **Namespace**: `airnity-test` - Isolated environment for testing
- **Resources**:
  - `airnity-test` Project with auto-promotion enabled
  - `airnity-test-warehouse` Warehouse tracking a Git repository
  - `dev` Stage with airnity-render promotion steps
  - RoleBinding for secret access (if `secret.yaml` exists)

These resources demonstrate the airnity-render promotion runner in action and can be used to test end-to-end promotion workflows.