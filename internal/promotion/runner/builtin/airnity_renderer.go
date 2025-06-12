package builtin

import (
	"bytes"
	"context"
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

// KubernetesResource represents a Kubernetes resource with metadata
type KubernetesResource struct {
	Group     string `json:"group"`
	Version   string `json:"version"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace *string `json:"namespace"`
	Manifest  any    `json:"manifest"`
}

// AirnityResponseItem represents a single item in the response from airnity server
type AirnityResponseItem struct {
	ClusterID string                `json:"cluster_id"`
	AppName   string                `json:"app_name"`
	Resources []KubernetesResource  `json:"resources"`
}

func (a *airnityRenderer) run(
	ctx context.Context,
	stepCtx *promotion.StepContext,
	cfg builtin.AirnityRendererConfig,
) (promotion.StepResult, error) {
	logger := logging.LoggerFromContext(ctx)

	// Prepare the request payload
	deployments := make([]AirnityDeployment, len(cfg.Deployments))
	for i, dep := range cfg.Deployments {
		deployments[i] = AirnityDeployment{
			ClusterID: dep.ClusterID,
			AppName:   dep.AppName,
		}
	}

	requestPayload := AirnityRequest{
		RepoURL:     cfg.RepoURL,
		Commit:      cfg.Commit,
		Deployments: deployments,
	}

	// Use the fixed URL to the mock airnity server
	url := "http://app-generator.kargo.svc.cluster.local"

	// Make the HTTP request
	responseItems, err := a.makeHTTPRequest(ctx, url, cfg, requestPayload)
	if err != nil {
		return promotion.StepResult{Status: kargoapi.PromotionStepStatusErrored},
			fmt.Errorf("error making HTTP request to airnity server: %w", err)
	}

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

	return promotion.StepResult{Status: kargoapi.PromotionStepStatusSucceeded}, nil
}

func (a *airnityRenderer) makeHTTPRequest(
	ctx context.Context,
	url string,
	cfg builtin.AirnityRendererConfig,
	payload AirnityRequest,
) ([]AirnityResponseItem, error) {
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

	var responseItems []AirnityResponseItem
	if err := json.Unmarshal(bodyBytes, &responseItems); err != nil {
		return nil, fmt.Errorf("error unmarshaling response: %w", err)
	}

	return responseItems, nil
}

func (a *airnityRenderer) getHTTPClient(cfg builtin.AirnityRendererConfig) *http.Client {
	timeout := airnityRequestTimeout
	if cfg.Timeout != "" {
		if parsedTimeout, err := time.ParseDuration(cfg.Timeout); err == nil {
			timeout = parsedTimeout
		}
	}

	return &http.Client{
		Timeout: timeout,
	}
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

	// Now atomically move files from temp directory to final location
	for _, item := range responseItems {
		tempClusterDir := filepath.Join(tempDir, item.ClusterID)
		tempAppDir := filepath.Join(tempClusterDir, item.AppName)

		finalClusterDir := filepath.Join(workDir, item.ClusterID)
		finalAppDir := filepath.Join(finalClusterDir, item.AppName)

		// Ensure final directory structure exists
		if err := os.MkdirAll(finalAppDir, 0755); err != nil {
			return fmt.Errorf("error creating final directory %s: %w", finalAppDir, err)
		}

		// Move files from temp to final location
		if err := a.moveManifestFiles(ctx, tempAppDir, finalAppDir); err != nil {
			return fmt.Errorf("error moving manifests for app %s in cluster %s: %w",
				item.AppName, item.ClusterID, err)
		}

		logger.Debug("moved manifests to final location", "cluster", item.ClusterID, "app", item.AppName)
	}

	logger.Debug("successfully wrote all manifests atomically")
	return nil
}

func (a *airnityRenderer) moveManifestFiles(ctx context.Context, srcDir, destDir string) error {
	logger := logging.LoggerFromContext(ctx)

	fmt.Println("Moving manifest files from", srcDir, "to", destDir)

	// Read all files from source directory
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("error reading source directory %s: %w", srcDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue // Skip directories, we only care about files
		}

		srcFile := filepath.Join(srcDir, entry.Name())
		destFile := filepath.Join(destDir, entry.Name())

		// Atomic move: rename from temp location to final location
		if err := os.Rename(srcFile, destFile); err != nil {
			// If rename fails (different filesystems), fall back to copy + delete
			if err := a.copyFile(srcFile, destFile); err != nil {
				return fmt.Errorf("error copying file %s to %s: %w", srcFile, destFile, err)
			}
			if err := os.Remove(srcFile); err != nil {
				logger.Error(err, "failed to remove source file after copy", "file", srcFile)
			}
		}

		logger.Trace("moved manifest file", "from", srcFile, "to", destFile)
	}

	return nil
}

func (a *airnityRenderer) copyFile(src, dest string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("error opening source file: %w", err)
	}
	defer srcFile.Close()

	destFile, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("error creating destination file: %w", err)
	}
	defer destFile.Close()

	if _, err := destFile.ReadFrom(srcFile); err != nil {
		return fmt.Errorf("error copying file content: %w", err)
	}

	return destFile.Sync()
}

func (a *airnityRenderer) writeResourceToFile(
	ctx context.Context,
	appDir string,
	resource KubernetesResource,
	index int,
) error {
	logger := logging.LoggerFromContext(ctx)

	// Generate filename from the resource metadata
	var namespace string
	if resource.Namespace != nil {
		namespace = *resource.Namespace
	}
	filename := a.generateFilename(resource.Group, resource.Version, resource.Kind, resource.Name, namespace, index)
	filePath, err := securejoin.SecureJoin(appDir, filename)
	if err != nil {
		return fmt.Errorf("error joining path: %w", err)
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

	logger.Debug("wrote resource to file", "file", filename, "group", resource.Group, "version", resource.Version, "kind", resource.Kind, "name", resource.Name, "filePath", filePath)
	return nil
}

func (a *airnityRenderer) generateFilename(group, version, kind, name, namespace string, index int) string {
	// Start with GVK
	gvkStr := strings.ToLower(kind)
	if group != "" {
		// For core resources (v1), group is empty, so we don't need to handle that specially
		gvkStr = fmt.Sprintf("%s.%s", strings.ToLower(group), gvkStr)
	}

	// Build filename components
	components := []string{gvkStr}

	if name != "" {
		components = append(components, name)
	} else {
		// Fallback to index if no name
		components = append(components, fmt.Sprintf("resource-%d", index))
	}

	if namespace != "" {
		components = append(components, namespace)
	}

	return strings.Join(components, "-") + ".yaml"
}
