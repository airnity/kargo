#!/bin/bash

set -e

echo "🧪 Testing airnity-render runner with mock server..."

# Check if we're in a Kubernetes context
if ! kubectl cluster-info &> /dev/null; then
  echo "❌ No active Kubernetes context found. Please run 'tilt up' first."
  exit 1
fi

# Check if mock server is running
echo "🔍 Checking if mock airnity server is running..."
if ! kubectl get pod -n kargo -l app=mock-airnity-server | grep -q Running; then
  echo "❌ Mock airnity server is not running. Please run 'tilt up' first."
  exit 1
fi

echo "✅ Mock airnity server is running"

# Test the mock server directly first
echo "🧪 Testing mock server endpoint..."
kubectl port-forward -n kargo svc/mock-airnity-server 8080:80 &
PORT_FORWARD_PID=$!

# Wait for port forward to be ready
sleep 2

# Test the server
response=$(curl -s -X POST http://localhost:8080 \
  -H "Content-Type: application/json" \
  -d '{
    "repoURL": "https://github.com/example/test-repo.git",
    "commit": "test123",
    "deployments": [
      {
        "clusterId": "test-cluster",
        "appName": "test-app"
      }
    ]
  }')

# Kill port forward
kill $PORT_FORWARD_PID 2>/dev/null || true

if echo "$response" | jq -e 'type == "array"' > /dev/null; then
  echo "✅ Mock server is responding correctly"
else
  echo "❌ Mock server is not responding correctly"
  echo "Response: $response"
  exit 1
fi

# Create test project and resources
echo "🏗️  Creating test project..."
kubectl apply -f - <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: airnity-runner-test
---
apiVersion: kargo.akuity.io/v1alpha1
kind: Project
metadata:
  name: airnity-runner-test
spec:
  promotionPolicies: []
---
apiVersion: kargo.akuity.io/v1alpha1
kind: Warehouse
metadata:
  name: test-warehouse
  namespace: airnity-runner-test
spec:
  subscriptions:
  - git:
      repoURL: https://github.com/argoproj/argocd-example-apps.git
      branch: master
---
apiVersion: kargo.akuity.io/v1alpha1
kind: Stage
metadata:
  name: dev
  namespace: airnity-runner-test
spec:
  requestedFreight:
  - origin:
      kind: Warehouse
      name: test-warehouse
    sources:
      direct: true
  promotionTemplate:
    spec:
      steps:
      - uses: airnity-render
        config:
          environment: "http://app-generator.kargo.svc.cluster.local"
          repoURL: https://github.com/argoproj/argocd-example-apps.git
          commit: "latest"
          deployments:
          - clusterId: dev-east
            appName: frontend
          - clusterId: dev-west
            appName: backend
          outPath: "rendered-manifests"
          timeout: 60s
EOF

echo "⏳ Waiting for warehouse to discover freight..."
sleep 5

# Get the first available freight
FREIGHT_NAME=$(kubectl get freight -n airnity-runner-test -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")

if [[ -z "$FREIGHT_NAME" ]]; then
  echo "❌ No freight found. Creating manual freight for testing..."
  
  # Create a manual freight for testing
  kubectl apply -f - <<EOF
apiVersion: kargo.akuity.io/v1alpha1
kind: Freight
metadata:
  name: test-freight
  namespace: airnity-runner-test
spec:
  origin:
    kind: Warehouse
    name: test-warehouse
  commits:
  - repoURL: https://github.com/argoproj/argocd-example-apps.git
    id: abc123def456789
    branch: master
EOF
  
  FREIGHT_NAME="test-freight"
fi

echo "🚀 Creating promotion with freight: $FREIGHT_NAME"

# Create a promotion
PROMOTION_NAME="test-promotion-$(date +%s)"
kubectl apply -f - <<EOF
apiVersion: kargo.akuity.io/v1alpha1
kind: Promotion
metadata:
  name: $PROMOTION_NAME
  namespace: airnity-runner-test
spec:
  stage: dev
  freight: $FREIGHT_NAME
EOF

echo "⏳ Waiting for promotion to complete..."

# Wait for promotion to complete (with timeout)
timeout=60
elapsed=0
while [[ $elapsed -lt $timeout ]]; do
  status=$(kubectl get promotion -n airnity-runner-test $PROMOTION_NAME -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
  
  case "$status" in
    "Succeeded")
      echo "✅ Promotion completed successfully!"
      break
      ;;
    "Failed")
      echo "❌ Promotion failed!"
      kubectl describe promotion -n airnity-runner-test $PROMOTION_NAME
      exit 1
      ;;
    "")
      echo "⏳ Promotion status not available yet..."
      ;;
    *)
      echo "⏳ Promotion status: $status"
      ;;
  esac
  
  sleep 2
  elapsed=$((elapsed + 2))
done

if [[ $elapsed -ge $timeout ]]; then
  echo "❌ Promotion timed out after ${timeout} seconds"
  kubectl describe promotion -n airnity-runner-test $PROMOTION_NAME
  exit 1
fi

echo "🎉 Test completed successfully!"
echo "📋 Promotion details:"
kubectl get promotion -n airnity-runner-test $PROMOTION_NAME -o yaml

echo -e "\n🧹 Cleaning up test resources..."
kubectl delete namespace airnity-runner-test --ignore-not-found=true

echo "✅ All tests passed! The airnity-render runner works correctly with the mock server."