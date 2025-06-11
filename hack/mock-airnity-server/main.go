package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
)

// AirnityDeployment represents a deployment target with cluster and app information
type AirnityDeployment struct {
	ClusterID string `json:"clusterId"`
	AppName   string `json:"appName"`
}

// AirnityRequest represents the request payload sent to the airnity server
type AirnityRequest struct {
	RepoURL     string              `json:"repoURL"`
	Commit      string              `json:"commit"`
	Deployments []AirnityDeployment `json:"deployments"`
}

// AirnityResponseItem represents a single item in the response from airnity server
type AirnityResponseItem struct {
	AppName   string                   `json:"appName"`
	ClusterID string                   `json:"clusterId"`
	Resources []map[string]interface{} `json:"resources"`
}

func handleAirnityRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse the request
	var req AirnityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	log.Printf("Received request for repo: %s, commit: %s, deployments: %d", 
		req.RepoURL, req.Commit, len(req.Deployments))

	// Generate mock response based on the request
	var response []AirnityResponseItem
	
	for _, deployment := range req.Deployments {
		log.Printf("Generating manifests for cluster: %s, app: %s", deployment.ClusterID, deployment.AppName)
		
		// Create mock Kubernetes resources
		resources := []map[string]interface{}{
			// Mock Deployment
			{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata": map[string]interface{}{
					"name":      deployment.AppName,
					"namespace": "default",
					"labels": map[string]interface{}{
						"app":     deployment.AppName,
						"cluster": deployment.ClusterID,
						"commit":  req.Commit,
					},
				},
				"spec": map[string]interface{}{
					"replicas": 3,
					"selector": map[string]interface{}{
						"matchLabels": map[string]interface{}{
							"app": deployment.AppName,
						},
					},
					"template": map[string]interface{}{
						"metadata": map[string]interface{}{
							"labels": map[string]interface{}{
								"app": deployment.AppName,
							},
						},
						"spec": map[string]interface{}{
							"containers": []map[string]interface{}{
								{
									"name":  deployment.AppName,
									"image": fmt.Sprintf("myregistry/%s:%s", deployment.AppName, req.Commit[:8]),
									"ports": []map[string]interface{}{
										{
											"containerPort": 8080,
										},
									},
									"env": []map[string]interface{}{
										{
											"name":  "CLUSTER_ID",
											"value": deployment.ClusterID,
										},
										{
											"name":  "GIT_COMMIT",
											"value": req.Commit,
										},
									},
								},
							},
						},
					},
				},
			},
			// Mock Service
			{
				"apiVersion": "v1",
				"kind":       "Service",
				"metadata": map[string]interface{}{
					"name":      fmt.Sprintf("%s-service", deployment.AppName),
					"namespace": "default",
					"labels": map[string]interface{}{
						"app":     deployment.AppName,
						"cluster": deployment.ClusterID,
					},
				},
				"spec": map[string]interface{}{
					"selector": map[string]interface{}{
						"app": deployment.AppName,
					},
					"ports": []map[string]interface{}{
						{
							"name":       "http",
							"port":       80,
							"targetPort": 8080,
						},
					},
					"type": "ClusterIP",
				},
			},
			// Mock ConfigMap
			{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name":      fmt.Sprintf("%s-config", deployment.AppName),
					"namespace": "default",
				},
				"data": map[string]interface{}{
					"config.yaml": fmt.Sprintf(`
app:
  name: %s
  cluster: %s
  commit: %s
  environment: dev
`, deployment.AppName, deployment.ClusterID, req.Commit),
				},
			},
		}

		// Add cluster-specific resources for different clusters
		if deployment.ClusterID == "prod-east" {
			// Add an Ingress for production east
			resources = append(resources, map[string]interface{}{
				"apiVersion": "networking.k8s.io/v1",
				"kind":       "Ingress",
				"metadata": map[string]interface{}{
					"name":      fmt.Sprintf("%s-ingress", deployment.AppName),
					"namespace": "default",
					"annotations": map[string]interface{}{
						"nginx.ingress.kubernetes.io/rewrite-target": "/",
					},
				},
				"spec": map[string]interface{}{
					"rules": []map[string]interface{}{
						{
							"host": fmt.Sprintf("%s.prod-east.example.com", deployment.AppName),
							"http": map[string]interface{}{
								"paths": []map[string]interface{}{
									{
										"path":     "/",
										"pathType": "Prefix",
										"backend": map[string]interface{}{
											"service": map[string]interface{}{
												"name": fmt.Sprintf("%s-service", deployment.AppName),
												"port": map[string]interface{}{
													"number": 80,
												},
											},
										},
									},
								},
							},
						},
					},
				},
			})
		}

		response = append(response, AirnityResponseItem{
			AppName:   deployment.AppName,
			ClusterID: deployment.ClusterID,
			Resources: resources,
		})
	}

	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	
	// Encode and send response
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	log.Printf("Successfully generated %d deployment responses", len(response))
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "healthy",
		"service": "mock-airnity-server",
	})
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting mock airnity server on port %s", port)

	http.HandleFunc("/", handleAirnityRequest)
	http.HandleFunc("/health", healthCheck)

	log.Printf("Mock airnity server ready to receive requests at http://localhost:%s", port)
	log.Printf("Health check available at http://localhost:%s/health", port)
	
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}