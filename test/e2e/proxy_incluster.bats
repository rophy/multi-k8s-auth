#!/usr/bin/env bats

# Tests for kube-auth-proxy in-cluster mode (validating tokens directly
# against the Kubernetes API server, without kube-federated-auth).

setup_file() {
    load 'test_helper'
    export PROXY_URL="${PROXY_INCLUSTER_URL:-http://kube-auth-proxy-incluster:4180}"
    local attempts=0
    while [[ $attempts -lt 30 ]]; do
        if curl -sf "${PROXY_URL}/healthz" > /dev/null 2>&1; then
            return 0
        fi
        sleep 1
        attempts=$((attempts + 1))
    done
    echo "ERROR: in-cluster proxy not ready at ${PROXY_URL}" >&2
    return 1
}

setup() {
    load 'test_helper'
    export PROXY_URL="${PROXY_INCLUSTER_URL:-http://kube-auth-proxy-incluster:4180}"
}

@test "in-cluster proxy healthz returns ok" {
    local result
    result=$(curl -s "${PROXY_URL}/healthz")

    echo "# Response: $result"

    local status
    status=$(echo "$result" | jq -r '.status')
    [[ "$status" == "ok" ]]
}

@test "in-cluster proxy /auth returns 401 without token" {
    local http_code
    http_code=$(curl -s -o /dev/null -w "%{http_code}" "${PROXY_URL}/auth")
    [[ "$http_code" == "401" ]]
}

@test "in-cluster proxy /auth returns 200 with valid token" {
    local token
    token=$(get_token)

    local http_code
    http_code=$(curl -s -o /dev/null -w "%{http_code}" \
        -H "Authorization: Bearer ${token}" \
        "${PROXY_URL}/auth")
    [[ "$http_code" == "200" ]]
}

@test "in-cluster proxy /auth sets user header on success" {
    local token
    token=$(get_token)

    local headers
    headers=$(curl -s -D - -o /dev/null \
        -H "Authorization: Bearer ${token}" \
        "${PROXY_URL}/auth")

    echo "# Headers: $headers"

    # User header should be set (no cluster-name extra since this is direct K8s API)
    echo "$headers" | grep -qi "X-Auth-Request-User:"
}

@test "in-cluster proxy /auth returns 401 with invalid token" {
    local http_code
    http_code=$(curl -s -o /dev/null -w "%{http_code}" \
        -H "Authorization: Bearer invalid.token.here" \
        "${PROXY_URL}/auth")
    [[ "$http_code" == "401" ]]
}
