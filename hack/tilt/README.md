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