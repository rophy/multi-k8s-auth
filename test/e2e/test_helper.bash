#!/bin/bash
# Common test helpers for kube-federated-auth e2e tests

# Service URL (set by pod env or override for local testing)
SERVICE_URL="${SERVICE_URL:-http://kube-federated-auth}"

# Token file path (projected SA token)
TOKEN_PATH="${TOKEN_PATH:-/var/run/secrets/tokens/token}"

# Expected cluster name for the test token
CLUSTER_NAME="${CLUSTER_NAME:-cluster-a}"

# Read the projected ServiceAccount token
get_token() {
    cat "$TOKEN_PATH"
}

# POST a TokenReview request and return the response JSON
token_review() {
    local token="$1"
    curl -s -X POST "${SERVICE_URL}/apis/authentication.k8s.io/v1/tokenreviews" \
        -H "Content-Type: application/json" \
        -d "{\"apiVersion\":\"authentication.k8s.io/v1\",\"kind\":\"TokenReview\",\"spec\":{\"token\":\"${token}\"}}"
}

# Wait for the service to be ready (up to 30 seconds)
wait_for_service() {
    local attempts=0
    while [[ $attempts -lt 30 ]]; do
        if curl -sf "${SERVICE_URL}/health" > /dev/null 2>&1; then
            return 0
        fi
        sleep 1
        attempts=$((attempts + 1))
    done
    echo "ERROR: service not ready at ${SERVICE_URL}" >&2
    return 1
}
