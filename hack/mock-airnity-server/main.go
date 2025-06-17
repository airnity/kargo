package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

// AinrityApp represents a deployment target with cluster and app information
type AinrityApp struct {
	ClusterIDs               []string `json:"clusterIds"`
	ApplicationManifestNames []string `json:"applicationManifestNames"`
}

// GitRef represents a Git reference (branch, tag, etc.)
type GitRef struct {
	Type string `json:"type"` // e.g., "branch", "tag"
	Ref  string `json:"ref"`  // e.g., "main", "v1.0.0"
}

// AirnityRequest represents the request payload sent to the airnity server
type AirnityRequest struct {
	GitRef GitRef       `json:"gitRef"`
	Apps   []AinrityApp `json:"apps"`
}

// KubernetesResource represents a Kubernetes resource with metadata
type KubernetesResource struct {
	Group     string  `json:"group"`
	Version   string  `json:"version"`
	Kind      string  `json:"kind"`
	Name      string  `json:"name"`
	Namespace *string `json:"namespace"`
	Manifest  any     `json:"manifest"`
}

// AirnityResponseItem represents a single item in the response from airnity server
type AirnityResponseItem struct {
	ClusterID string               `json:"cluster_id"`
	AppName   string               `json:"app_name"`
	Resources []KubernetesResource `json:"resources"`
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

	log.Printf("Received request for gitRef: %s-%s, deployments: %d",
		req.GitRef.Type, req.GitRef.Ref, len(req.Apps))

	// Generate mock response based on the request
	var response []AirnityResponseItem

	for _, app := range req.Apps {
		for _, applicationManifestName := range app.ApplicationManifestNames {
			log.Printf("Generating manifests for cluster: %s, app: %s", app.ClusterIDs[0], applicationManifestName)

			// Create mock Kubernetes resources
			defaultNamespace := "default"
			resources := []KubernetesResource{
				// Mock Deployment
				{
					Group:     "apps",
					Version:   "v1",
					Kind:      "Deployment",
					Name:      applicationManifestName,
					Namespace: &defaultNamespace,
					Manifest: map[string]any{
						"apiVersion": "apps/v1",
						"kind":       "Deployment",
						"metadata": map[string]any{
							"name":      applicationManifestName,
							"namespace": "default",
							"labels": map[string]any{
								"app":     applicationManifestName,
								"cluster": app.ClusterIDs[0],
							},
						},
						"spec": map[string]any{
							"replicas": 5,
							"selector": map[string]any{
								"matchLabels": map[string]any{
									"app": applicationManifestName,
								},
							},
							"template": map[string]any{
								"metadata": map[string]any{
									"labels": map[string]any{
										"app": applicationManifestName,
									},
								},
								"spec": map[string]any{
									"containers": []map[string]any{
										{
											"name":  applicationManifestName,
											"image": fmt.Sprintf("myregistry/%s:latest", applicationManifestName),
											"ports": []map[string]any{
												{
													"containerPort": 8080,
												},
											},
											"env": []map[string]any{
												{
													"name":  "CLUSTER_ID",
													"value": app.ClusterIDs[0],
												},
												{
													"name":  "GIT_COMMIT",
													"value": req.GitRef.Ref,
												},
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
					Group:     "",
					Version:   "v1",
					Kind:      "Service",
					Name:      fmt.Sprintf("%s-service", applicationManifestName),
					Namespace: &defaultNamespace,
					Manifest: map[string]any{
						"apiVersion": "v1",
						"kind":       "Service",
						"metadata": map[string]any{
							"name":      fmt.Sprintf("%s-service", applicationManifestName),
							"namespace": "default",
							"labels": map[string]any{
								"app":     applicationManifestName,
								"cluster": app.ClusterIDs[0],
							},
						},
						"spec": map[string]any{
							"selector": map[string]any{
								"app": applicationManifestName,
							},
							"ports": []map[string]any{
								{
									"name":       "http",
									"port":       80,
									"targetPort": 8080,
								},
							},
							"type": "ClusterIP",
						},
					},
				},
				// Mock ConfigMap
				{
					Group:     "",
					Version:   "v1",
					Kind:      "ConfigMap",
					Name:      fmt.Sprintf("%s-config", applicationManifestName),
					Namespace: &defaultNamespace,
					Manifest: map[string]any{
						"apiVersion": "v1",
						"kind":       "ConfigMap",
						"metadata": map[string]any{
							"name":      fmt.Sprintf("%s-config", applicationManifestName),
							"namespace": "default",
						},
						"data": map[string]any{
							"config.yaml": fmt.Sprintf(`
	app:
	name: %s
	cluster: %s
	environment: dev
	`, applicationManifestName, app.ClusterIDs[0]),
						},
					},
				},
			}

			// Add cluster-specific resources for different clusters
			if app.ClusterIDs[0] == "prod-east" {
				// Add an Ingress for production east
				resources = append(resources, KubernetesResource{
					Group:     "networking.k8s.io",
					Version:   "v1",
					Kind:      "Ingress",
					Name:      fmt.Sprintf("%s-ingress", applicationManifestName),
					Namespace: &defaultNamespace,
					Manifest: map[string]any{
						"apiVersion": "networking.k8s.io/v1",
						"kind":       "Ingress",
						"metadata": map[string]any{
							"name":      fmt.Sprintf("%s-ingress", applicationManifestName),
							"namespace": "default",
							"annotations": map[string]any{
								"nginx.ingress.kubernetes.io/rewrite-target": "/",
							},
						},
						"spec": map[string]any{
							"rules": []map[string]any{
								{
									"host": fmt.Sprintf("%s.prod-east.example.com", applicationManifestName),
									"http": map[string]any{
										"paths": []map[string]any{
											{
												"path":     "/",
												"pathType": "Prefix",
												"backend": map[string]any{
													"service": map[string]any{
														"name": fmt.Sprintf("%s-service", applicationManifestName),
														"port": map[string]any{
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
					},
				})
			}

			response = append(response, AirnityResponseItem{
				AppName:   applicationManifestName,
				ClusterID: app.ClusterIDs[0],
				Resources: resources,
			})
		}

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

func healthCheck(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"status":  "healthy",
		"service": "mock-airnity-server",
	}); err != nil {
		log.Printf("Error encoding health check response: %v", err)
	}
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

	// Create server with timeouts to address security concerns
	server := &http.Server{
		Addr:         ":" + port,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
