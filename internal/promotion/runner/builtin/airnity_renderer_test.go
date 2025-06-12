package builtin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kargoapi "github.com/akuity/kargo/api/v1alpha1"
	"github.com/akuity/kargo/pkg/promotion"
	"github.com/akuity/kargo/pkg/x/promotion/runner/builtin"
)

func Test_airnityRenderer_Name_New(t *testing.T) {
	r := newAirnityRenderer()
	assert.Equal(t, "airnity-render", r.Name())
}

func Test_airnityRenderer_validate_New(t *testing.T) {
	testCases := []struct {
		name             string
		config           promotion.Config
		expectedProblems []string
	}{
		{
			name:   "repoURL not specified",
			config: promotion.Config{},
			expectedProblems: []string{
				"(root): repoURL is required",
			},
		},
		{
			name: "commit not specified",
			config: promotion.Config{
				"repoURL": "https://github.com/example/repo",
			},
			expectedProblems: []string{
				"(root): commit is required",
			},
		},
		{
			name: "deployments not specified",
			config: promotion.Config{
				"repoURL": "https://github.com/example/repo",
				"commit":  "abc123",
			},
			expectedProblems: []string{
				"(root): deployments is required",
			},
		},
		{
			name: "deployments is empty array",
			config: promotion.Config{
				"repoURL":     "https://github.com/example/repo",
				"commit":      "abc123",
				"deployments": []any{},
			},
			expectedProblems: []string{
				"deployments: Array must have at least 1 items",
			},
		},
		{
			name: "deployment missing clusterId",
			config: promotion.Config{
				"repoURL": "https://github.com/example/repo",
				"commit":  "abc123",
				"deployments": []any{
					map[string]any{
						"appName": "test-app",
					},
				},
			},
			expectedProblems: []string{
				"deployments.0: clusterId is required",
			},
		},
		{
			name: "deployment missing appName",
			config: promotion.Config{
				"repoURL": "https://github.com/example/repo",
				"commit":  "abc123",
				"deployments": []any{
					map[string]any{
						"clusterId": "test-cluster",
					},
				},
			},
			expectedProblems: []string{
				"deployments.0: appName is required",
			},
		},
		{
			name: "invalid timeout format",
			config: promotion.Config{
				"repoURL": "https://github.com/example/repo",
				"commit":  "abc123",
				"deployments": []any{
					map[string]any{
						"clusterId": "test-cluster",
						"appName":   "test-app",
					},
				},
				"timeout": "invalid",
			},
			expectedProblems: []string{
				"timeout: Does not match pattern",
			},
		},
		{
			name: "valid configuration",
			config: promotion.Config{
				"repoURL": "https://github.com/example/repo",
				"commit":  "abc123",
				"deployments": []any{
					map[string]any{
						"clusterId": "test-cluster",
						"appName":   "test-app",
					},
				},
			},
			expectedProblems: nil,
		},
		{
			name: "valid configuration with timeout",
			config: promotion.Config{
				"repoURL": "https://github.com/example/repo",
				"commit":  "abc123",
				"deployments": []any{
					map[string]any{
						"clusterId": "test-cluster",
						"appName":   "test-app",
					},
				},
				"timeout": "30s",
			},
			expectedProblems: nil,
		},
		{
			name: "valid configuration with outPath",
			config: promotion.Config{
				"repoURL": "https://github.com/example/repo",
				"commit":  "abc123",
				"deployments": []any{
					map[string]any{
						"clusterId": "test-cluster",
						"appName":   "test-app",
					},
				},
				"outPath": "manifests",
			},
			expectedProblems: nil,
		},
	}

	r := newAirnityRenderer()
	runner, ok := r.(*airnityRenderer)
	require.True(t, ok)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := runner.validate(testCase.config)
			if len(testCase.expectedProblems) == 0 {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				for _, expectedProblem := range testCase.expectedProblems {
					assert.Contains(t, err.Error(), expectedProblem)
				}
			}
		})
	}
}

func Test_airnityRenderer_run_New(t *testing.T) {
	tests := []struct {
		name           string
		config         builtin.AirnityRendererConfig
		serverResponse []AirnityResponseItem
		serverStatus   int
		assertions     func(*testing.T, string, promotion.StepResult, error)
	}{
		{
			name: "successful render with single app",
			config: builtin.AirnityRendererConfig{
				RepoURL: "https://github.com/example/repo",
				Commit:  "abc123",
				Deployments: []builtin.Deployment{
					{
						ClusterID: "prod-east",
						AppName:   "frontend",
					},
				},
			},
			serverResponse: []AirnityResponseItem{
				{
					ClusterID: "prod-east",
					AppName:   "frontend",
					Resources: []KubernetesResource{
						{
							Group:     "apps",
							Version:   "v1",
							Kind:      "Deployment",
							Name:      "frontend",
							Namespace: func() *string { s := "default"; return &s }(),
							Manifest: map[string]interface{}{
								"apiVersion": "apps/v1",
								"kind":       "Deployment",
								"metadata": map[string]interface{}{
									"name":      "frontend",
									"namespace": "default",
								},
								"spec": map[string]interface{}{
									"replicas": 3,
								},
							},
						},
						{
							Group:     "",
							Version:   "v1",
							Kind:      "Service",
							Name:      "frontend-svc",
							Namespace: func() *string { s := "default"; return &s }(),
							Manifest: map[string]interface{}{
								"apiVersion": "v1",
								"kind":       "Service",
								"metadata": map[string]interface{}{
									"name":      "frontend-svc",
									"namespace": "default",
								},
								"spec": map[string]interface{}{
									"type": "ClusterIP",
								},
							},
						},
					},
				},
			},
			serverStatus: http.StatusOK,
			assertions: func(t *testing.T, workDir string, result promotion.StepResult, err error) {
				assert.NoError(t, err)
				assert.Equal(t, kargoapi.PromotionStepStatusSucceeded, result.Status)

				// Check that files were created
				deploymentFile := filepath.Join(workDir, "prod-east", "frontend", "apps.deployment-frontend-default.yaml")
				assert.FileExists(t, deploymentFile)

				serviceFile := filepath.Join(workDir, "prod-east", "frontend", "service-frontend-svc-default.yaml")
				assert.FileExists(t, serviceFile)

				// Verify file content
				content, err := os.ReadFile(deploymentFile)
				assert.NoError(t, err)
				assert.Contains(t, string(content), "name: frontend")
				assert.Contains(t, string(content), "replicas: 3")
			},
		},
		{
			name: "server returns error status",
			config: builtin.AirnityRendererConfig{
				RepoURL: "https://github.com/example/repo",
				Commit:  "abc123",
				Deployments: []builtin.Deployment{
					{
						ClusterID: "prod-east",
						AppName:   "frontend",
					},
				},
			},
			serverStatus: http.StatusInternalServerError,
			assertions: func(t *testing.T, workDir string, result promotion.StepResult, err error) {
				assert.Error(t, err)
				assert.Equal(t, kargoapi.PromotionStepStatusErrored, result.Status)
				assert.Contains(t, err.Error(), "airnity server returned status 500")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request method and headers
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.Equal(t, "application/json", r.Header.Get("Accept"))

				// Verify request body
				bodyBytes, err := io.ReadAll(r.Body)
				require.NoError(t, err)

				var requestPayload AirnityRequest
				err = json.Unmarshal(bodyBytes, &requestPayload)
				require.NoError(t, err)

				assert.Equal(t, tt.config.RepoURL, requestPayload.RepoURL)
				assert.Equal(t, tt.config.Commit, requestPayload.Commit)
				assert.Len(t, requestPayload.Deployments, len(tt.config.Deployments))

				// Set response status
				w.WriteHeader(tt.serverStatus)

				// Send response
				if tt.serverStatus == http.StatusOK {
					if tt.name == "server returns invalid JSON" {
						_, _ = w.Write([]byte("invalid json"))
					} else {
						responseBytes, err := json.Marshal(tt.serverResponse)
						require.NoError(t, err)
						_, _ = w.Write(responseBytes)
					}
				}
			}))
			defer server.Close()

			// Create temporary work directory
			workDir := t.TempDir()

			// Create context and step context
			ctx := context.Background()
			stepCtx := &promotion.StepContext{
				WorkDir: workDir,
			}

			// Create runner and execute
			r := newAirnityRenderer()
			runner, ok := r.(*airnityRenderer)
			require.True(t, ok)

			result, err := runner.run(ctx, stepCtx, tt.config)

			// Run assertions
			tt.assertions(t, workDir, result, err)
		})
	}
}

func Test_airnityRenderer_generateFilename_New(t *testing.T) {
	tests := []struct {
		name      string
		group     string
		version   string
		kind      string
		resName   string
		namespace string
		index     int
		expected  string
	}{
		{
			name:      "deployment with namespace",
			group:     "apps",
			version:   "v1", 
			kind:      "Deployment",
			resName:   "frontend",
			namespace: "default",
			index:     0,
			expected:  "apps.deployment-frontend-default.yaml",
		},
		{
			name:      "service with namespace",
			group:     "",
			version:   "v1",
			kind:      "Service",
			resName:   "frontend-svc",
			namespace: "default",
			index:     0,
			expected:  "service-frontend-svc-default.yaml",
		},
		{
			name:      "namespace without namespace",
			group:     "",
			version:   "v1",
			kind:      "Namespace",
			resName:   "test-namespace",
			namespace: "",
			index:     0,
			expected:  "namespace-test-namespace.yaml",
		},
		{
			name:      "resource without name",
			group:     "",
			version:   "v1",
			kind:      "ConfigMap",
			resName:   "",
			namespace: "default",
			index:     2,
			expected:  "configmap-resource-2-default.yaml",
		},
		{
			name:      "custom resource",
			group:     "argoproj.io",
			version:   "v1alpha1",
			kind:      "Application",
			resName:   "my-app",
			namespace: "argocd",
			index:     0,
			expected:  "argoproj.io.application-my-app-argocd.yaml",
		},
	}

	r := newAirnityRenderer()
	runner, ok := r.(*airnityRenderer)
	require.True(t, ok)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runner.generateFilename(tt.group, tt.version, tt.kind, tt.resName, tt.namespace, tt.index)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func Test_airnityRenderer_Run_ConfigValidation_New(t *testing.T) {
	r := newAirnityRenderer()

	ctx := context.Background()
	stepCtx := &promotion.StepContext{
		Config: promotion.Config{
			"repoURL": "", // Invalid: empty repoURL
		},
		WorkDir: t.TempDir(),
	}

	result, err := r.Run(ctx, stepCtx)
	assert.Error(t, err)
	assert.Equal(t, kargoapi.PromotionStepStatusErrored, result.Status)
}
