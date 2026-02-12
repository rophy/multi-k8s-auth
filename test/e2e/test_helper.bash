#!/bin/bash
# Common test helpers for kube-federated-auth e2e tests
# Tests run on the HOST and use kubectl exec to reach in-cluster services.

KUBE_CONTEXT="${KUBE_CONTEXT:-kind-cluster-a}"
NAMESPACE="${NAMESPACE:-kube-federated-auth}"
TEST_CLIENT="${TEST_CLIENT:-deployment/test-client}"

SERVICE_URL="${SERVICE_URL:-http://kube-federated-auth}"
CLUSTER_NAME="${CLUSTER_NAME:-cluster-a}"
TOKEN_PATH="${TOKEN_PATH:-/var/run/secrets/tokens/token}"

# Run a command in the test-client pod
kexec() {
    kubectl --context "$KUBE_CONTEXT" exec -n "$NAMESPACE" "$TEST_CLIENT" -- "$@"
}

# Read the projected ServiceAccount token from the test-client pod
get_token() {
    kexec cat "$TOKEN_PATH"
}

# POST a TokenReview request via curl in the test-client pod
token_review() {
    local token="$1"
    kexec curl -s -X POST "${SERVICE_URL}/apis/authentication.k8s.io/v1/tokenreviews" \
        -H "Content-Type: application/json" \
        -d "{\"apiVersion\":\"authentication.k8s.io/v1\",\"kind\":\"TokenReview\",\"spec\":{\"token\":\"${token}\"}}"
}

# Wait for a service to be ready (up to 30 seconds)
wait_for_service() {
    local url="${1:-${SERVICE_URL}/health}"
    local attempts=0
    while [[ $attempts -lt 30 ]]; do
        if kexec curl -sf "$url" > /dev/null 2>&1; then
            return 0
        fi
        sleep 1
        attempts=$((attempts + 1))
    done
    echo "ERROR: service not ready at $url" >&2
    return 1
}
