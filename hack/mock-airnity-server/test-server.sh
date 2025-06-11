#!/bin/bash

set -e

echo "🚀 Testing Mock Airnity Server..."

# Default to localhost if no host provided
HOST="${1:-localhost:30084}"

echo "📡 Testing health endpoint..."
curl -s "http://$HOST/health" | jq .

echo -e "\n🎯 Testing airnity endpoint with sample data..."

# Test with sample data
response=$(curl -s -X POST "http://$HOST" \
  -H "Content-Type: application/json" \
  -d '{
    "repoURL": "https://github.com/example/test-repo.git",
    "commit": "abc123def456789",
    "deployments": [
      {
        "clusterId": "test-cluster",
        "appName": "test-app"
      },
      {
        "clusterId": "prod-east",
        "appName": "frontend"
      }
    ]
  }')

echo "✅ Response received:"
echo "$response" | jq .

# Validate response structure
echo -e "\n🔍 Validating response structure..."

# Check if response is an array
if echo "$response" | jq -e 'type == "array"' > /dev/null; then
  echo "✅ Response is an array"
else
  echo "❌ Response is not an array"
  exit 1
fi

# Check if each item has required fields
item_count=$(echo "$response" | jq 'length')
echo "📊 Found $item_count deployment responses"

for i in $(seq 0 $((item_count-1))); do
  app_name=$(echo "$response" | jq -r ".[$i].appName")
  cluster_id=$(echo "$response" | jq -r ".[$i].clusterId")
  resources_count=$(echo "$response" | jq ".[$i].resources | length")
  
  echo "  📦 $app_name @ $cluster_id: $resources_count resources"
  
  # Validate required fields exist
  if [[ "$app_name" == "null" || "$cluster_id" == "null" ]]; then
    echo "❌ Missing required fields in response item $i"
    exit 1
  fi
  
  # Check if resources array is not empty
  if [[ "$resources_count" == "0" ]]; then
    echo "❌ No resources generated for $app_name @ $cluster_id"
    exit 1
  fi
  
  # Check resource structure
  for j in $(seq 0 $((resources_count-1))); do
    api_version=$(echo "$response" | jq -r ".[$i].resources[$j].apiVersion")
    kind=$(echo "$response" | jq -r ".[$i].resources[$j].kind")
    
    if [[ "$api_version" == "null" || "$kind" == "null" ]]; then
      echo "❌ Invalid resource structure in item $i, resource $j"
      exit 1
    fi
    
    echo "    🔧 $kind ($api_version)"
  done
done

# Test with prod-east to verify Ingress generation
echo -e "\n🏭 Testing production cluster (should include Ingress)..."
prod_response=$(curl -s -X POST "http://$HOST" \
  -H "Content-Type: application/json" \
  -d '{
    "repoURL": "https://github.com/example/prod-app.git",
    "commit": "prod123",
    "deployments": [
      {
        "clusterId": "prod-east",
        "appName": "web-app"
      }
    ]
  }')

# Check if Ingress is included for prod-east
ingress_count=$(echo "$prod_response" | jq '[.[0].resources[] | select(.kind == "Ingress")] | length')
if [[ "$ingress_count" -gt "0" ]]; then
  echo "✅ Ingress resource generated for prod-east cluster"
else
  echo "❌ No Ingress resource found for prod-east cluster"
  exit 1
fi

echo -e "\n🎉 All tests passed! Mock server is working correctly."
echo "💡 You can now test the airnity-render promotion runner."