# Mock Airnity Server

This is a mock server that simulates the airnity app generator service for testing the `airnity-render` promotion runner.

## What it does

The mock server:

1. **Receives HTTP POST requests** with the expected airnity payload:
   ```json
   {
     "repoURL": "https://github.com/example/repo.git",
     "commit": "abc123def456",
     "deployments": [
       {
         "clusterId": "prod-east",
         "appName": "frontend"
       }
     ]
   }
   ```

2. **Generates mock Kubernetes manifests** for each deployment, including:
   - Deployment with appropriate labels and environment variables
   - Service to expose the application
   - ConfigMap with app configuration
   - Ingress (for production clusters)

3. **Returns the expected response format**:
   ```json
   [
     {
       "appName": "frontend",
       "clusterId": "prod-east",
       "resources": [
         {
           "apiVersion": "apps/v1",
           "kind": "Deployment",
           "metadata": { ... },
           "spec": { ... }
         },
         ...
       ]
     }
   ]
   ```

## Deployment with Tilt

The mock server is automatically deployed when you run `tilt up` and is available at:

- **Inside the cluster**: `http://app-generator.kargo.svc.cluster.local`
- **Port forward**: `http://localhost:30084` (for direct testing)

### Service Discovery

The server is deployed with the following service configuration:

- **Namespace**: `kargo`
- **Service Name**: `app-generator`
- **URL**: `http://app-generator.kargo.svc.cluster.local`

The simplified configuration eliminates the need for multiple namespaces and environment-based routing.

## Testing the Runner

### 1. Deploy the Example

```bash
# Apply the example stage configuration
kubectl apply -f hack/examples/airnity-test-stage.yaml
```

### 2. Trigger a Promotion

```bash
# Create a promotion to test the runner
kubectl create -n airnity-test promotion test-promotion --from-literal=stage=dev --from-literal=freight=<freight-name>
```

### 3. Check the Results

The promotion should:
1. Call the mock airnity server
2. Generate manifest files in the expected directory structure:
   ```
   <workdir>/
   ├── dev-east/
   │   └── frontend/
   │       ├── apps.deployment-frontend-default.yaml
   │       ├── service-frontend-service-default.yaml
   │       └── configmap-frontend-config-default.yaml
   └── dev-west/
       └── backend/
           ├── apps.deployment-backend-default.yaml
           ├── service-backend-service-default.yaml
           └── configmap-backend-config-default.yaml
   ```

## Manual Testing

You can test the mock server directly:

```bash
# Test via port forward
curl -X POST http://localhost:30084 \
  -H "Content-Type: application/json" \
  -d '{
    "repoURL": "https://github.com/example/test-repo.git",
    "commit": "abc123def456789",
    "deployments": [
      {
        "clusterId": "test-cluster",
        "appName": "test-app"
      }
    ]
  }'

# Health check
curl http://localhost:30084/health
```

## Mock Data Variations

The mock server generates different resources based on:

- **Cluster ID**: Production clusters (`prod-east`) get additional Ingress resources
- **App Name**: Used in resource names and labels
- **Commit**: Included in image tags and labels
- **Repository**: Used in metadata and labels

## Logs

View the mock server logs to see requests:

```bash
kubectl logs -n kargo deployment/mock-airnity-server -f
```

The logs will show:
- Received requests with repo, commit, and deployment details
- Generated manifest information for each app/cluster combination
- Any errors in request processing