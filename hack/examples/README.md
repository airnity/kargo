# Kargo Examples

This directory contains example configurations and scripts for Kargo.

## Secret Management

### Files

- `secret.yaml.template` - Template for the GitHub App secret
- `rolebinding.yaml` - RoleBinding for the kargo-controller to read secrets
- `manage-secret.sh` - Script to manage the secret in Kubernetes
- `secret.yaml` - **Git-ignored** actual secret file (created by script)

### Usage

#### Retrieve secret from cluster

```bash
./manage-secret.sh get
```

This will retrieve the `kargo-github-app` secret from the `it-central` context, `kargo` namespace and save it to `secret.yaml`.


#### Create template secret

```bash
./manage-secret.sh template
```

This creates a `secret.yaml` file from the template with placeholder values.

#### Validate secret file

```bash
./manage-secret.sh validate
```

This validates that the secret file is properly formatted YAML.

### Environment Variables

You can override the default values using environment variables:

```bash
CONTEXT=my-context NAMESPACE=my-namespace ./manage-secret.sh get
```

Available variables:
- `CONTEXT` - Kubernetes context (default: `it-central`)
- `NAMESPACE` - Kubernetes namespace (default: `kargo`)
- `SECRET_NAME` - Name of the secret (default: `kargo-github-app`)

### Security Notes

- The `secret.yaml` file is git-ignored and should never be committed
- The script is **read-only** and only retrieves secrets from the cluster (no apply/modify functionality)
- The template file contains placeholder values for reference only
- To modify secrets in the cluster, use kubectl directly with appropriate permissions

## Other Files

- `test-airnity-runner.sh` - End-to-end test script for the airnity-render promotion runner