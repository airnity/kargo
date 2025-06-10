package builtin

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/xeipuuv/gojsonschema"
	"sigs.k8s.io/yaml"

	kargoapi "github.com/akuity/kargo/api/v1alpha1"
	"github.com/akuity/kargo/internal/io"
	"github.com/akuity/kargo/internal/logging"
	"github.com/akuity/kargo/pkg/promotion"
	"github.com/akuity/kargo/pkg/x/promotion/runner/builtin"
)

const (
	airnityContentTypeJSON  = "application/json"
	airnityRequestTimeout   = 30 * time.Second
	airnityMaxResponseBytes = 10 << 20 // 10MB
)

var (
	environments = []string{"sandbox", "prod", "dev", "it"}
)

// airnityRenderer is an implementation of the promotion.StepRunner interface that
// calls an HTTP server to render Kubernetes manifests and writes them to files.
type airnityRenderer struct {
	schemaLoader gojsonschema.JSONLoader
}

// newAirnityRenderer returns an implementation of the promotion.StepRunner
// interface that calls an HTTP server to render Kubernetes manifests.
func newAirnityRenderer() promotion.StepRunner {
	r := &airnityRenderer{}
	r.schemaLoader = getConfigSchemaLoader(r.Name())
	return r
}

// Name implements the promotion.StepRunner interface.
func (a *airnityRenderer) Name() string {
	return "airnity-render"
}

// Run implements the promotion.StepRunner interface.
func (a *airnityRenderer) Run(
	ctx context.Context,
	stepCtx *promotion.StepContext,
) (promotion.StepResult, error) {
	if err := a.validate(stepCtx.Config); err != nil {
		return promotion.StepResult{Status: kargoapi.PromotionStepStatusErrored}, err
	}
	cfg, err := promotion.ConfigToStruct[builtin.AirnityRendererConfig](stepCtx.Config)
	if err != nil {
		return promotion.StepResult{Status: kargoapi.PromotionStepStatusErrored},
			fmt.Errorf("could not convert config into airnity-renderer config: %w", err)
	}
	return a.run(ctx, stepCtx, cfg)
}

// validate validates airnityRenderer configuration against a JSON schema.
func (a *airnityRenderer) validate(cfg promotion.Config) error {
	return validate(a.schemaLoader, gojsonschema.NewGoLoader(cfg), a.Name())
}

// AirnityRequest represents the request payload sent to the airnity server
type AirnityRequest struct {
	GitRef         builtin.GitRef `json:"gitRef"`
	Apps           []builtin.App  `json:"apps"`
	RepositoryName string         `json:"repositoryName"`
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
	ClusterID string               `json:"clusterId"`
	AppName   string               `json:"appName"`
	Resources []KubernetesResource `json:"resources"`
}

type AirnityResponse struct {
	Data []AirnityResponseItem `json:"data"`
}

func (a *airnityRenderer) run(
	ctx context.Context,
	stepCtx *promotion.StepContext,
	cfg builtin.AirnityRendererConfig,
) (promotion.StepResult, error) {
	logger := logging.LoggerFromContext(ctx)

	requestPayload := AirnityRequest{
		GitRef:         cfg.GitRef,
		Apps:           cfg.Apps,
		RepositoryName: cfg.ArgoRepoName,
	}

	for _, env := range environments {
		fmt.Println("Running airnity-renderer for environment:", env)
		// Use the fixed URL to the mock airnity server
		url := fmt.Sprintf("https://argocd-apps-generator.admin.%s.airnity.private/api/v1/generate-manifests", env)

		// Make the HTTP request
		responseData, err := a.makeHTTPRequest(ctx, url, cfg, requestPayload)
		if err != nil {
			return promotion.StepResult{Status: kargoapi.PromotionStepStatusErrored},
				fmt.Errorf("error making HTTP request to airnity server: %w", err)
		}
		if responseData == nil {
			return promotion.StepResult{Status: kargoapi.PromotionStepStatusErrored},
				fmt.Errorf("no data received from airnity server")
		}

		responseItems := responseData.Data

		logger.Debug("received response from airnity server", "items", len(responseItems))

		// Determine output directory
		outDir := stepCtx.WorkDir
		if cfg.OutPath != "" {
			var err error
			outDir, err = securejoin.SecureJoin(stepCtx.WorkDir, cfg.OutPath)
			if err != nil {
				return promotion.StepResult{Status: kargoapi.PromotionStepStatusErrored},
					fmt.Errorf("could not secure join outPath %q: %w", cfg.OutPath, err)
			}
		}

		// Write manifests to files
		if err := a.writeManifests(ctx, outDir, responseItems); err != nil {
			return promotion.StepResult{Status: kargoapi.PromotionStepStatusErrored},
				fmt.Errorf("error writing manifests: %w", err)
		}
	}

	return promotion.StepResult{Status: kargoapi.PromotionStepStatusSucceeded}, nil
}

func (a *airnityRenderer) makeHTTPRequest(
	ctx context.Context,
	url string,
	cfg builtin.AirnityRendererConfig,
	payload AirnityRequest,
) (*AirnityResponse, error) {
	logger := logging.LoggerFromContext(ctx)

	// Serialize the request payload
	requestBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("error marshaling request payload: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("error creating HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", airnityContentTypeJSON)
	req.Header.Set("Accept", airnityContentTypeJSON)

	// Create HTTP client
	client := a.getHTTPClient(cfg)

	logger.Debug("making HTTP request to airnity server", "url", url)

	// Make the request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("airnity server returned status %d", resp.StatusCode)
	}

	// Read and parse the response
	bodyBytes, err := io.LimitRead(resp.Body, airnityMaxResponseBytes)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}

	logger.Trace("received response from airnity server", "body", string(bodyBytes))

	var responseItems AirnityResponse
	if err := json.Unmarshal(bodyBytes, &responseItems); err != nil {
		return nil, fmt.Errorf("error unmarshaling response: %w", err)
	}

	return &responseItems, nil
}

func (a *airnityRenderer) getHTTPClient(cfg builtin.AirnityRendererConfig) *http.Client {
	timeout := airnityRequestTimeout
	if cfg.Timeout != "" {
		if parsedTimeout, err := time.ParseDuration(cfg.Timeout); err == nil {
			timeout = parsedTimeout
		}
	}

	client := &http.Client{
		Timeout: timeout,
	}

	if cfg.SkipTLSVerify {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		}
	}

	return client
}

func (a *airnityRenderer) writeManifests(
	ctx context.Context,
	workDir string,
	responseItems []AirnityResponseItem,
) error {
	logger := logging.LoggerFromContext(ctx)

	// Ensure the base working directory exists
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return fmt.Errorf("error creating base directory %s: %w", workDir, err)
	}

	// Create a temporary directory for atomic operations
	tempDir, err := os.MkdirTemp(workDir, "airnity-temp-*")
	if err != nil {
		return fmt.Errorf("error creating temporary directory: %w", err)
	}

	// Ensure cleanup of temp directory on any failure
	defer func() {
		if removeErr := os.RemoveAll(tempDir); removeErr != nil {
			logger.Error(removeErr, "failed to clean up temporary directory", "dir", tempDir)
		}
	}()

	logger.Debug("writing manifests to temporary directory", "tempDir", tempDir)

	// Write all files to temporary directory first
	for _, item := range responseItems {
		tempClusterDir := filepath.Join(tempDir, item.ClusterID)
		tempAppDir := filepath.Join(tempClusterDir, item.AppName)

		// Create directories in temp location
		if err := os.MkdirAll(tempAppDir, 0755); err != nil {
			return fmt.Errorf("error creating temporary directory %s: %w", tempAppDir, err)
		}

		// Write each resource to a file in temp location
		for i, resource := range item.Resources {
			if err := a.writeResourceToFile(ctx, tempAppDir, resource, i); err != nil {
				return fmt.Errorf("error writing resource %d for app %s in cluster %s to temp location: %w",
					i, item.AppName, item.ClusterID, err)
			}
		}

		logger.Trace("wrote manifests for app to temp location", "cluster", item.ClusterID, "app", item.AppName, "resources", len(item.Resources))
	}

	// Now atomically move app directories from temp directory to final location
	for _, item := range responseItems {
		tempClusterDir := filepath.Join(tempDir, item.ClusterID)
		tempAppDir := filepath.Join(tempClusterDir, item.AppName)

		finalClusterDir := filepath.Join(workDir, item.ClusterID)
		finalAppDir := filepath.Join(finalClusterDir, item.AppName)

		// Ensure final cluster directory exists
		if err := os.MkdirAll(finalClusterDir, 0755); err != nil {
			return fmt.Errorf("error creating final cluster directory %s: %w", finalClusterDir, err)
		}

		// Remove existing app directory content if it exists
		if _, err := os.Stat(finalAppDir); err == nil {
			if err := os.RemoveAll(finalAppDir); err != nil {
				return fmt.Errorf("error removing existing app directory %s: %w", finalAppDir, err)
			}
		}

		// Atomically move the entire app directory
		if err := a.simpleAtomicMove(tempAppDir, finalAppDir); err != nil {
			return fmt.Errorf("error moving app directory for app %s in cluster %s: %w",
				item.AppName, item.ClusterID, err)
		}

		logger.Debug("moved app directory to final location", "cluster", item.ClusterID, "app", item.AppName)
	}

	logger.Debug("successfully wrote all manifests atomically")
	return nil
}

func (a *airnityRenderer) simpleAtomicMove(src, dst string) error {
	if err := os.Rename(src, dst); err != nil {
		if !os.IsExist(err) {
			return fmt.Errorf("failed to move %s to %s: %w", src, dst, err)
		}

		// If the destination already exists, remove it and try again
		if err := os.RemoveAll(dst); err != nil {
			return fmt.Errorf("failed to remove existing destination %s: %w", dst, err)
		}

		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("failed to move %s to %s after removing existing: %w", src, dst, err)
		}
	}
	return nil
}

func (a *airnityRenderer) writeResourceToFile(
	ctx context.Context,
	appDir string,
	resource KubernetesResource,
	index int,
) error {
	logger := logging.LoggerFromContext(ctx)

	// Generate full path from the resource metadata
	var namespace string
	if resource.Namespace != nil {
		namespace = *resource.Namespace
	}
	relativePath := a.generateFilePath(resource.Group, resource.Kind, resource.Name, namespace)
	filePath, err := securejoin.SecureJoin(appDir, relativePath)
	if err != nil {
		return fmt.Errorf("error joining path: %w", err)
	}

	// Create directories if they don't exist
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("error creating directories %s: %w", dir, err)
	}

	// Convert resource manifest to YAML
	yamlBytes, err := yaml.Marshal(resource.Manifest)
	if err != nil {
		return fmt.Errorf("error marshaling resource to YAML: %w", err)
	}

	// Write to file (overwrite if exists)
	if err := os.WriteFile(filePath, yamlBytes, 0644); err != nil {
		return fmt.Errorf("error writing file %s: %w", filePath, err)
	}

	logger.Debug("wrote resource to file", "file", relativePath, "group", resource.Group, "version", resource.Version, "kind", resource.Kind, "name", resource.Name, "filePath", filePath)
	return nil
}

func (a *airnityRenderer) generateFilePath(group, kind, name, namespace string) string {
	// Build filename path: group/kind/namespace/name.yaml
	filenameParts := []string{}

	// Add group (use "core" for empty group)
	if group != "" {
		filenameParts = append(filenameParts, strings.ToLower(group))
	}

	// Add kind
	filenameParts = append(filenameParts, strings.ToLower(kind))

	// Add namespace (use "cluster-scoped" for empty namespace)
	if namespace != "" {
		filenameParts = append(filenameParts, namespace)
	}

	// Add name (use index if no name)
	if name != "" {
		filenameParts = append(filenameParts, name)
	}

	return strings.Join(filenameParts, "_") + ".yaml"
}
