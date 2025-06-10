#!/bin/bash

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SECRET_FILE="${SCRIPT_DIR}/secret.yaml"
TEMPLATE_FILE="${SCRIPT_DIR}/secret.yaml.template"
CONTEXT="it-central"
NAMESPACE="kargo"
SECRET_NAME="kargo-github-app"

usage() {
    cat << EOF
Usage: $0 [COMMAND]

Commands:
    get         Retrieve secret from k8s cluster and save to secret.yaml
    update      Update the secret.yaml from k8s cluster (alias for get)
    template    Create a template secret.yaml from template
    validate    Check if secret.yaml exists and is valid
    help        Show this help message

Environment:
    CONTEXT:    Kubernetes context (default: it-central)
    NAMESPACE:  Kubernetes namespace (default: kargo)
    SECRET_NAME: Name of the secret (default: kargo-github-app)

Examples:
    $0 get                    # Retrieve secret from cluster
    CONTEXT=prod $0 get       # Use different context
EOF
}

check_kubectl() {
    if ! command -v kubectl &> /dev/null; then
        echo "❌ kubectl is not installed or not in PATH"
        exit 1
    fi
}

check_context() {
    if ! kubectl config get-contexts "${CONTEXT}" &> /dev/null; then
        echo "❌ Context '${CONTEXT}' not found"
        echo "Available contexts:"
        kubectl config get-contexts -o name
        exit 1
    fi
}

check_namespace() {
    if ! kubectl --context="${CONTEXT}" get namespace "${NAMESPACE}" &> /dev/null; then
        echo "❌ Namespace '${NAMESPACE}' not found in context '${CONTEXT}'"
        exit 1
    fi
}

get_secret() {
    echo "🔍 Retrieving secret '${SECRET_NAME}' from context '${CONTEXT}' namespace '${NAMESPACE}'..."
    
    check_kubectl
    check_context
    check_namespace
    
    if ! kubectl --context="${CONTEXT}" get secret "${SECRET_NAME}" -n "${NAMESPACE}" &> /dev/null; then
        echo "❌ Secret '${SECRET_NAME}' not found in namespace '${NAMESPACE}'"
        exit 1
    fi
    
    kubectl --context="${CONTEXT}" get secret "${SECRET_NAME}" -n "${NAMESPACE}" -o yaml > "${SECRET_FILE}"
    
    # Clean up the output (remove managed fields, resourceVersion, etc.)
    if command -v yq &> /dev/null; then
        yq eval 'del(.metadata.resourceVersion, .metadata.uid, .metadata.creationTimestamp, .metadata.managedFields)' -i "${SECRET_FILE}"
    fi
    
    echo "✅ Secret saved to ${SECRET_FILE}"
}


create_template() {
    echo "📝 Creating template secret file..."
    
    if [[ -f "${SECRET_FILE}" ]]; then
        echo "⚠️  Secret file already exists: ${SECRET_FILE}"
        read -p "Do you want to overwrite it? (y/N): " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            echo "❌ Operation cancelled"
            exit 1
        fi
    fi
    
    cp "${TEMPLATE_FILE}" "${SECRET_FILE}"
    echo "✅ Template secret created: ${SECRET_FILE}"
    echo "💡 Edit the file and add your actual secret values (base64 encoded)"
}

validate_secret() {
    echo "🔍 Validating secret file..."
    
    if [[ ! -f "${SECRET_FILE}" ]]; then
        echo "❌ Secret file not found: ${SECRET_FILE}"
        exit 1
    fi
    
    if ! kubectl --dry-run=client apply -f "${SECRET_FILE}" &> /dev/null; then
        echo "❌ Secret file is not valid YAML"
        exit 1
    fi
    
    echo "✅ Secret file is valid"
    
    # Check if values are still template placeholders
    if grep -q "# Base64 encoded" "${SECRET_FILE}"; then
        echo "⚠️  Secret file contains template placeholders - remember to fill in actual values"
    fi
}

main() {
    case "${1:-help}" in
        get|update)
            get_secret
            ;;
        template)
            create_template
            ;;
        validate)
            validate_secret
            ;;
        help|--help|-h)
            usage
            ;;
        *)
            echo "❌ Unknown command: $1"
            echo
            usage
            exit 1
            ;;
    esac
}

main "$@"