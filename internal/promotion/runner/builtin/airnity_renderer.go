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

	"github.com/hashicorp/go-cleanhttp"
	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/xeipuuv/gojsonschema"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"

	kargoapi "github.com/akuity/kargo/api/v1alpha1"
	"github.com/akuity/kargo/internal/io"
	"github.com/akuity/kargo/internal/logging"
	"github.com/akuity/kargo/pkg/promotion"
	"github.com/akuity/kargo/pkg/x/promotion/runner/builtin"
)

const (
	airnityContentTypeJSON   = "application/json"
	airnityRequestTimeout    = 30 * time.Second
	airnityMaxResponseBytes  = 10 << 20 // 10MB
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
	return "airnity-renderer"
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
	GitRepo     string              `json:"gitRepo"`
	CommitSha   string              `json:"commitSha"`
	Deployments []AirnityDeployment `json:"deployments"`
}

// AirnityResponseItem represents a single item in the response from airnity server
type AirnityResponseItem struct {
	AppName   string                    `json:"appName"`
	ClusterID string                    `json:"clusterId"`
	Resources []map[string]interface{} `json:"resources"`
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
		GitRepo:     cfg.GitRepo,
		CommitSha:   cfg.CommitSHA,
		Deployments: deployments,
	}

	// Make the HTTP request
	responseItems, err := a.makeHTTPRequest(ctx, cfg, requestPayload)
	if err != nil {
		return promotion.StepResult{Status: kargoapi.PromotionStepStatusErrored},
			fmt.Errorf("error making HTTP request to airnity server: %w", err)
	}

	logger.Debug("received response from airnity server", "items", len(responseItems))

	// Write manifests to files
	if err := a.writeManifests(ctx, stepCtx.WorkDir, responseItems); err != nil {
		return promotion.StepResult{Status: kargoapi.PromotionStepStatusErrored},
			fmt.Errorf("error writing manifests: %w", err)
	}

	return promotion.StepResult{Status: kargoapi.PromotionStepStatusSucceeded}, nil
}

func (a *airnityRenderer) makeHTTPRequest(
	ctx context.Context,
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("error creating HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", airnityContentTypeJSON)
	req.Header.Set("Accept", airnityContentTypeJSON)
	for _, header := range cfg.Headers {
		req.Header.Set(header.Name, header.Value)
	}

	// Create HTTP client
	client := a.getHTTPClient(cfg)

	logger.Debug("making HTTP request to airnity server", "url", cfg.URL)

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
	httpTransport := cleanhttp.DefaultTransport()
	if cfg.InsecureSkipTLSVerify {
		httpTransport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true, // nolint: gosec
		}
	}

	timeout := airnityRequestTimeout
	if cfg.Timeout != "" {
		if parsedTimeout, err := time.ParseDuration(cfg.Timeout); err == nil {
			timeout = parsedTimeout
		}
	}

	return &http.Client{
		Transport: httpTransport,
		Timeout:   timeout,
	}
}

func (a *airnityRenderer) writeManifests(
	ctx context.Context,
	workDir string,
	responseItems []AirnityResponseItem,
) error {
	logger := logging.LoggerFromContext(ctx)

	for _, item := range responseItems {
		clusterDir := filepath.Join(workDir, item.ClusterID)
		appDir := filepath.Join(clusterDir, item.AppName)

		// Create directories if they don't exist
		if err := os.MkdirAll(appDir, 0755); err != nil {
			return fmt.Errorf("error creating directory %s: %w", appDir, err)
		}

		// Write each resource to a file
		for i, resource := range item.Resources {
			if err := a.writeResourceToFile(ctx, appDir, resource, i); err != nil {
				return fmt.Errorf("error writing resource %d for app %s in cluster %s: %w",
					i, item.AppName, item.ClusterID, err)
			}
		}

		logger.Debug("wrote manifests for app", "cluster", item.ClusterID, "app", item.AppName, "resources", len(item.Resources))
	}

	return nil
}

func (a *airnityRenderer) writeResourceToFile(
	ctx context.Context,
	appDir string,
	resource map[string]interface{},
	index int,
) error {
	logger := logging.LoggerFromContext(ctx)

	// Convert to unstructured to extract metadata
	unstructuredObj := &unstructured.Unstructured{Object: resource}

	// Get resource information
	gvk := unstructuredObj.GroupVersionKind()
	name := unstructuredObj.GetName()
	namespace := unstructuredObj.GetNamespace()

	// Generate filename
	filename := a.generateFilename(gvk, name, namespace, index)
	filePath, err := securejoin.SecureJoin(appDir, filename)
	if err != nil {
		return fmt.Errorf("error joining path: %w", err)
	}

	// Convert resource to YAML
	yamlBytes, err := yaml.Marshal(resource)
	if err != nil {
		return fmt.Errorf("error marshaling resource to YAML: %w", err)
	}

	// Write to file (overwrite if exists)
	if err := os.WriteFile(filePath, yamlBytes, 0644); err != nil {
		return fmt.Errorf("error writing file %s: %w", filePath, err)
	}

	logger.Trace("wrote resource to file", "file", filename, "gvk", gvk.String(), "name", name)
	return nil
}

func (a *airnityRenderer) generateFilename(gvk schema.GroupVersionKind, name, namespace string, index int) string {
	// Start with GVK
	gvkStr := strings.ToLower(gvk.Kind)
	if gvk.Group != "" {
		// For core resources (v1), group is empty, so we don't need to handle that specially
		gvkStr = fmt.Sprintf("%s.%s", strings.ToLower(gvk.Group), gvkStr)
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