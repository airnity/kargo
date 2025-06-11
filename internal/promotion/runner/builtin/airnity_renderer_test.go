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
	assert.Equal(t, "airnity-render", r.Name())
}

func Test_airnityRenderer_validate(t *testing.T) {
	testCases := []struct {
		name             string
		config           promotion.Config
		expectedProblems []string
	}{
		{
			name:   "environment not specified",
			config: promotion.Config{},
			expectedProblems: []string{
				"(root): environment is required",
			},
		},
		{
			name: "environment is empty string",
			config: promotion.Config{
				"environment": "",
			},
			expectedProblems: []string{
				"environment: String length must be greater than or equal to 1",
			},
		},
		{
			name: "repoURL not specified",
			config: promotion.Config{
				"environment": "dev",
			},
			expectedProblems: []string{
				"(root): repoURL is required",
			},
		},
		{
			name: "commit not specified",
			config: promotion.Config{
				"environment": "dev",
				"repoURL":     "https://github.com/example/repo",
			},
			expectedProblems: []string{
				"(root): commit is required",
			},
		},
		{
			name: "deployments not specified",
			config: promotion.Config{
				"environment": "dev",
				"repoURL":     "https://github.com/example/repo",
				"commit":      "abc123",
			},
			expectedProblems: []string{
				"(root): deployments is required",
			},
		},
		{
			name: "deployments is empty array",
			config: promotion.Config{
				"environment":   "dev",
				"repoURL":       "https://github.com/example/repo",
				"commit":     "abc123",
				"deployments":   []any{},
			},
			expectedProblems: []string{
				"deployments: Array must have at least 1 items",
			},
		},
		{
			name: "deployment missing clusterId",
			config: promotion.Config{
				"environment": "dev",
				"repoURL":     "https://github.com/example/repo",
				"commit":   "abc123",
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
				"environment": "dev",
				"repoURL":     "https://github.com/example/repo",
				"commit":   "abc123",
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
				"environment": "dev",
				"repoURL":     "https://github.com/example/repo",
				"commit":   "abc123",
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
				"environment": "dev",
				"repoURL":     "https://github.com/example/repo",
				"commit":   "abc123",
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
				"environment": "dev",
				"repoURL":     "https://github.com/example/repo",
				"commit":   "abc123",
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
				"environment": "dev",
				"repoURL":     "https://github.com/example/repo",
				"commit":   "abc123",
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
				Environment: "dev",
				RepoURL:     "https://github.com/example/repo",
				Commit:   "abc123",
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
				Environment: "dev",
				RepoURL:     "https://github.com/example/repo",
				Commit:   "def456",
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
				Environment: "dev",
				RepoURL:   "https://github.com/example/repo",
				Commit: "xyz789",
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
				Environment: "dev",
				RepoURL:   "https://github.com/example/repo",
				Commit: "xyz789",
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
				Environment: "dev",
				RepoURL:   "https://github.com/example/repo",
				Commit: "abc123",
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
				Environment: "dev",
				RepoURL:   "https://github.com/example/repo",
				Commit: "abc123",
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
		{
			name: "successful render with custom outPath",
			config: builtin.AirnityRendererConfig{
				Environment: "dev",
				RepoURL:     "https://github.com/example/repo",
				Commit:      "abc123",
				OutPath:     "manifests",
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
							"apiVersion": "apps/v1",
							"kind":       "Deployment",
							"metadata": map[string]interface{}{
								"name":      "test-app",
								"namespace": "default",
							},
						},
					},
				},
			},
			serverStatus: http.StatusOK,
			assertions: func(t *testing.T, workDir string, result promotion.StepResult, err error) {
				assert.NoError(t, err)
				assert.Equal(t, kargoapi.PromotionStepStatusSucceeded, result.Status)

				// Check that files were created in the custom outPath directory
				deploymentFile := filepath.Join(workDir, "manifests", "test-cluster", "test-app", "apps.deployment-test-app-default.yaml")
				assert.FileExists(t, deploymentFile)

				// Verify the working directory doesn't have files directly
				directFile := filepath.Join(workDir, "test-cluster", "test-app", "apps.deployment-test-app-default.yaml")
				assert.NoFileExists(t, directFile)

				// Verify content
				content, err := os.ReadFile(deploymentFile)
				assert.NoError(t, err)
				assert.Contains(t, string(content), "name: test-app")
			},
		},
		{
			name: "outPath with directory traversal attempt is normalized",
			config: builtin.AirnityRendererConfig{
				Environment: "dev",
				RepoURL:     "https://github.com/example/repo",
				Commit:      "abc123",
				OutPath:     "../../../safe-dir",
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
							"metadata": map[string]interface{}{
								"name":      "test-config",
								"namespace": "default",
							},
						},
					},
				},
			},
			serverStatus: http.StatusOK,
			assertions: func(t *testing.T, workDir string, result promotion.StepResult, err error) {
				assert.NoError(t, err)
				assert.Equal(t, kargoapi.PromotionStepStatusSucceeded, result.Status)

				// Verify the file is created in the normalized path within workDir
				normalizedFile := filepath.Join(workDir, "safe-dir", "test-cluster", "test-app", "configmap-test-config-default.yaml")
				assert.FileExists(t, normalizedFile)

				// Verify it's not created outside the workDir
				outsideFile := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(workDir))), "safe-dir", "test-cluster", "test-app", "configmap-test-config-default.yaml")
				assert.NoFileExists(t, outsideFile)
			},
		},
		{
			name: "atomic operation with simulated failure",
			config: builtin.AirnityRendererConfig{
				Environment: "dev",
				RepoURL:   "https://github.com/example/repo",
				Commit: "abc123",
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
							"metadata": map[string]interface{}{
								"name":      "config1",
								"namespace": "default",
							},
						},
						{
							"apiVersion": "v1",
							"kind":       "Secret",
							"metadata": map[string]interface{}{
								"name":      "secret1",
								"namespace": "default",
							},
						},
					},
				},
			},
			serverStatus: http.StatusOK,
			assertions: func(t *testing.T, workDir string, result promotion.StepResult, err error) {
				assert.NoError(t, err)
				assert.Equal(t, kargoapi.PromotionStepStatusSucceeded, result.Status)

				// Verify both files were created
				configMapFile := filepath.Join(workDir, "test-cluster", "test-app", "configmap-config1-default.yaml")
				assert.FileExists(t, configMapFile)

				secretFile := filepath.Join(workDir, "test-cluster", "test-app", "secret-secret1-default.yaml")
				assert.FileExists(t, secretFile)

				// Verify no temporary directories remain
				entries, err := os.ReadDir(workDir)
				assert.NoError(t, err)
				for _, entry := range entries {
					if entry.IsDir() && strings.HasPrefix(entry.Name(), "airnity-temp-") {
						t.Errorf("Found temporary directory that should have been cleaned up: %s", entry.Name())
					}
				}
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

			// Override environment with test server URL for testing
			tt.config.Environment = server.URL

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
			"environment": "", // Invalid: empty environment
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
			"environment": "http://invalid-url-that-does-not-exist.local",
			"repoURL":     "https://github.com/example/repo",
			"commit":   "abc123",
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

func Test_airnityRenderer_TemporaryDirectoryCleanup(t *testing.T) {
	r := newAirnityRenderer()
	runner, ok := r.(*airnityRenderer)
	require.True(t, ok)

	// Create a temporary work directory
	workDir := t.TempDir()

	// Create a directory structure, then make it read-only to cause move operation to fail
	readOnlyDir := filepath.Join(workDir, "readonly-cluster", "readonly-app")
	err := os.MkdirAll(readOnlyDir, 0755) // Create with normal permissions first
	require.NoError(t, err)

	// Now make it read-only to cause the move to fail
	err = os.Chmod(readOnlyDir, 0555) // Read and execute only, no write permission
	require.NoError(t, err)

	// Restore permissions at end of test
	defer func() {
		_ = os.Chmod(readOnlyDir, 0755)
		_ = os.Chmod(filepath.Dir(readOnlyDir), 0755)
	}()

	// Create some valid response data
	responseItems := []AirnityResponseItem{
		{
			ClusterID: "readonly-cluster",
			AppName:   "readonly-app",
			Resources: []map[string]interface{}{
				{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test-config",
						"namespace": "default",
					},
				},
			},
		},
	}

	// This should fail during the move operation due to permission issues
	err = runner.writeManifests(context.Background(), workDir, responseItems)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error moving manifests")

	// Verify that no temporary directories remain after the failure
	entries, err := os.ReadDir(workDir)
	assert.NoError(t, err)
	
	tempDirFound := false
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "airnity-temp-") {
			tempDirFound = true
			break
		}
	}
	assert.False(t, tempDirFound, "Temporary directory should have been cleaned up after failure")
}