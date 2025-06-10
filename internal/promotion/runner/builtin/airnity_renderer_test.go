package builtin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"

	kargoapi "github.com/akuity/kargo/api/v1alpha1"
	"github.com/akuity/kargo/pkg/promotion"
	"github.com/akuity/kargo/pkg/x/promotion/runner/builtin"
)

func Test_airnityRenderer_Name(t *testing.T) {
	r := newAirnityRenderer()
	assert.Equal(t, "airnity-renderer", r.Name())
}

func Test_airnityRenderer_validate(t *testing.T) {
	testCases := []struct {
		name             string
		config           promotion.Config
		expectedProblems []string
	}{
		{
			name:   "url not specified",
			config: promotion.Config{},
			expectedProblems: []string{
				"(root): url is required",
			},
		},
		{
			name: "url is empty string",
			config: promotion.Config{
				"url": "",
			},
			expectedProblems: []string{
				"url: String length must be greater than or equal to 1",
			},
		},
		{
			name: "gitRepo not specified",
			config: promotion.Config{
				"url": "https://example.com",
			},
			expectedProblems: []string{
				"(root): gitRepo is required",
			},
		},
		{
			name: "commitSha not specified",
			config: promotion.Config{
				"url":     "https://example.com",
				"gitRepo": "https://github.com/example/repo",
			},
			expectedProblems: []string{
				"(root): commitSha is required",
			},
		},
		{
			name: "deployments not specified",
			config: promotion.Config{
				"url":       "https://example.com",
				"gitRepo":   "https://github.com/example/repo",
				"commitSha": "abc123",
			},
			expectedProblems: []string{
				"(root): deployments is required",
			},
		},
		{
			name: "deployments is empty array",
			config: promotion.Config{
				"url":         "https://example.com",
				"gitRepo":     "https://github.com/example/repo",
				"commitSha":   "abc123",
				"deployments": []any{},
			},
			expectedProblems: []string{
				"deployments: Array must have at least 1 items",
			},
		},
		{
			name: "deployment missing clusterId",
			config: promotion.Config{
				"url":       "https://example.com",
				"gitRepo":   "https://github.com/example/repo",
				"commitSha": "abc123",
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
				"url":       "https://example.com",
				"gitRepo":   "https://github.com/example/repo",
				"commitSha": "abc123",
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
				"url":       "https://example.com",
				"gitRepo":   "https://github.com/example/repo",
				"commitSha": "abc123",
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
				"url":       "https://example.com",
				"gitRepo":   "https://github.com/example/repo",
				"commitSha": "abc123",
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
			name: "valid configuration with headers and timeout",
			config: promotion.Config{
				"url":       "https://example.com",
				"gitRepo":   "https://github.com/example/repo",
				"commitSha": "abc123",
				"deployments": []any{
					map[string]any{
						"clusterId": "test-cluster",
						"appName":   "test-app",
					},
				},
				"headers": []any{
					map[string]any{
						"name":  "Authorization",
						"value": "Bearer token",
					},
				},
				"timeout":                "30s",
				"insecureSkipTLSVerify": true,
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

func Test_airnityRenderer_run(t *testing.T) {
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
				URL:       "", // Will be set by test server
				GitRepo:   "https://github.com/example/repo",
				CommitSHA: "abc123",
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
					Resources: []map[string]interface{}{
						{
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
						{
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
			name: "successful render with multiple apps and clusters",
			config: builtin.AirnityRendererConfig{
				URL:       "", // Will be set by test server
				GitRepo:   "https://github.com/example/repo",
				CommitSHA: "def456",
				Deployments: []builtin.Deployment{
					{
						ClusterID: "prod-east",
						AppName:   "frontend",
					},
					{
						ClusterID: "prod-west",
						AppName:   "backend",
					},
				},
			},
			serverResponse: []AirnityResponseItem{
				{
					ClusterID: "prod-east",
					AppName:   "frontend",
					Resources: []map[string]interface{}{
						{
							"apiVersion": "apps/v1",
							"kind":       "Deployment",
							"metadata": map[string]interface{}{
								"name":      "frontend",
								"namespace": "default",
							},
						},
					},
				},
				{
					ClusterID: "prod-west",
					AppName:   "backend",
					Resources: []map[string]interface{}{
						{
							"apiVersion": "apps/v1",
							"kind":       "Deployment",
							"metadata": map[string]interface{}{
								"name":      "backend",
								"namespace": "production",
							},
						},
					},
				},
			},
			serverStatus: http.StatusOK,
			assertions: func(t *testing.T, workDir string, result promotion.StepResult, err error) {
				assert.NoError(t, err)
				assert.Equal(t, kargoapi.PromotionStepStatusSucceeded, result.Status)

				// Check that files were created in correct directories
				frontendFile := filepath.Join(workDir, "prod-east", "frontend", "apps.deployment-frontend-default.yaml")
				assert.FileExists(t, frontendFile)

				backendFile := filepath.Join(workDir, "prod-west", "backend", "apps.deployment-backend-production.yaml")
				assert.FileExists(t, backendFile)
			},
		},
		{
			name: "resource without name uses index",
			config: builtin.AirnityRendererConfig{
				URL:       "", // Will be set by test server
				GitRepo:   "https://github.com/example/repo",
				CommitSHA: "xyz789",
				Deployments: []builtin.Deployment{
					{
						ClusterID: "test-cluster",
						AppName:   "test-app",
					},
				},
			},
			serverResponse: []AirnityResponseItem{
				{
					ClusterID: "test-cluster",
					AppName:   "test-app",
					Resources: []map[string]interface{}{
						{
							"apiVersion": "v1",
							"kind":       "ConfigMap",
							"metadata":   map[string]interface{}{},
						},
					},
				},
			},
			serverStatus: http.StatusOK,
			assertions: func(t *testing.T, workDir string, result promotion.StepResult, err error) {
				assert.NoError(t, err)
				assert.Equal(t, kargoapi.PromotionStepStatusSucceeded, result.Status)

				// Check that file was created with index-based name
				configMapFile := filepath.Join(workDir, "test-cluster", "test-app", "configmap-resource-0.yaml")
				assert.FileExists(t, configMapFile)
			},
		},
		{
			name: "cluster resource without namespace",
			config: builtin.AirnityRendererConfig{
				URL:       "", // Will be set by test server
				GitRepo:   "https://github.com/example/repo",
				CommitSHA: "xyz789",
				Deployments: []builtin.Deployment{
					{
						ClusterID: "test-cluster",
						AppName:   "test-app",
					},
				},
			},
			serverResponse: []AirnityResponseItem{
				{
					ClusterID: "test-cluster",
					AppName:   "test-app",
					Resources: []map[string]interface{}{
						{
							"apiVersion": "v1",
							"kind":       "Namespace",
							"metadata": map[string]interface{}{
								"name": "test-namespace",
							},
						},
					},
				},
			},
			serverStatus: http.StatusOK,
			assertions: func(t *testing.T, workDir string, result promotion.StepResult, err error) {
				assert.NoError(t, err)
				assert.Equal(t, kargoapi.PromotionStepStatusSucceeded, result.Status)

				// Check that file was created without namespace suffix
				namespaceFile := filepath.Join(workDir, "test-cluster", "test-app", "namespace-test-namespace.yaml")
				assert.FileExists(t, namespaceFile)
			},
		},
		{
			name: "server returns error status",
			config: builtin.AirnityRendererConfig{
				URL:       "", // Will be set by test server
				GitRepo:   "https://github.com/example/repo",
				CommitSHA: "abc123",
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
		{
			name: "server returns invalid JSON",
			config: builtin.AirnityRendererConfig{
				URL:       "", // Will be set by test server
				GitRepo:   "https://github.com/example/repo",
				CommitSHA: "abc123",
				Deployments: []builtin.Deployment{
					{
						ClusterID: "prod-east",
						AppName:   "frontend",
					},
				},
			},
			serverStatus: http.StatusOK,
			assertions: func(t *testing.T, workDir string, result promotion.StepResult, err error) {
				assert.Error(t, err)
				assert.Equal(t, kargoapi.PromotionStepStatusErrored, result.Status)
				assert.Contains(t, err.Error(), "error unmarshaling response")
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

				assert.Equal(t, tt.config.GitRepo, requestPayload.GitRepo)
				assert.Equal(t, tt.config.CommitSHA, requestPayload.CommitSha)
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

			// Update config with server URL
			tt.config.URL = server.URL

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

func Test_airnityRenderer_generateFilename(t *testing.T) {
	tests := []struct {
		name      string
		gvk       string
		group     string
		resName   string
		namespace string
		index     int
		expected  string
	}{
		{
			name:      "deployment with namespace",
			gvk:       "Deployment",
			group:     "apps/v1",
			resName:   "frontend",
			namespace: "default",
			index:     0,
			expected:  "apps.deployment-frontend-default.yaml",
		},
		{
			name:      "service with namespace",
			gvk:       "Service",
			group:     "v1",
			resName:   "frontend-svc",
			namespace: "default",
			index:     0,
			expected:  "service-frontend-svc-default.yaml",
		},
		{
			name:      "namespace without namespace",
			gvk:       "Namespace",
			group:     "v1",
			resName:   "test-namespace",
			namespace: "",
			index:     0,
			expected:  "namespace-test-namespace.yaml",
		},
		{
			name:      "resource without name",
			gvk:       "ConfigMap",
			group:     "v1",
			resName:   "",
			namespace: "default",
			index:     2,
			expected:  "configmap-resource-2-default.yaml",
		},
		{
			name:      "custom resource",
			gvk:       "Application",
			group:     "argoproj.io/v1alpha1",
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
			// Parse group and version
			var group, version string
			if strings.Contains(tt.group, "/") {
				parts := strings.Split(tt.group, "/")
				group = parts[0]
				version = parts[1]
			} else {
				group = ""
				version = tt.group
			}

			gvk := schema.GroupVersionKind{
				Group:   group,
				Version: version,
				Kind:    tt.gvk,
			}

			result := runner.generateFilename(gvk, tt.resName, tt.namespace, tt.index)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func Test_airnityRenderer_Run_ConfigValidation(t *testing.T) {
	r := newAirnityRenderer()

	ctx := context.Background()
	stepCtx := &promotion.StepContext{
		Config: promotion.Config{
			"url": "", // Invalid: empty URL
		},
		WorkDir: t.TempDir(),
	}

	result, err := r.Run(ctx, stepCtx)
	assert.Error(t, err)
	assert.Equal(t, kargoapi.PromotionStepStatusErrored, result.Status)
}

func Test_airnityRenderer_Run_HTTPError(t *testing.T) {
	r := newAirnityRenderer()

	ctx := context.Background()
	stepCtx := &promotion.StepContext{
		Config: promotion.Config{
			"url":       "http://invalid-url-that-does-not-exist.local",
			"gitRepo":   "https://github.com/example/repo",
			"commitSha": "abc123",
			"deployments": []any{
				map[string]any{
					"clusterId": "test-cluster",
					"appName":   "test-app",
				},
			},
		},
		WorkDir: t.TempDir(),
	}

	result, err := r.Run(ctx, stepCtx)
	assert.Error(t, err)
	assert.Equal(t, kargoapi.PromotionStepStatusErrored, result.Status)
	assert.Contains(t, err.Error(), "error making HTTP request to airnity server")
}