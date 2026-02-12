#!/usr/bin/env bats

# Tests for kube-auth-proxy in reverse proxy (sidecar) mode.
# The echo-server pod runs kube-auth-proxy as a sidecar with --upstream=http://localhost:8080.
# Requests go through the proxy, which validates tokens and forwards to the echo server.

ECHO_URL="${ECHO_URL:-http://echo-server}"

setup_file() {
    load 'test_helper'
    wait_for_service "${ECHO_URL}/healthz"
}

setup() {
    load 'test_helper'
}

@test "sidecar proxy returns 401 without token" {
    local http_code
    http_code=$(kexec curl -s -o /dev/null -w "%{http_code}" "${ECHO_URL}/")
    [[ "$http_code" == "401" ]]
}

@test "sidecar proxy returns 401 with invalid token" {
    local http_code
    http_code=$(kexec curl -s -o /dev/null -w "%{http_code}" \
        -H "Authorization: Bearer invalid.token.here" \
        "${ECHO_URL}/")
    [[ "$http_code" == "401" ]]
}

@test "sidecar proxy forwards request with valid token" {
    local token
    token=$(get_token)

    local http_code
    http_code=$(kexec curl -s -o /dev/null -w "%{http_code}" \
        -H "Authorization: Bearer ${token}" \
        "${ECHO_URL}/")
    [[ "$http_code" == "200" ]]
}

@test "sidecar proxy sets identity headers on upstream request" {
    local token
    token=$(get_token)

    local body
    body=$(kexec curl -s \
        -H "Authorization: Bearer ${token}" \
        "${ECHO_URL}/")

    echo "# Response body: $body"

    # mendhak/http-https-echo returns request headers in JSON
    # Reverse proxy mode uses X-Forwarded-* headers (like oauth2-proxy)
    echo "$body" | jq -e '.headers["x-forwarded-user"]' > /dev/null
}

@test "sidecar proxy strips Authorization header before forwarding" {
    local token
    token=$(get_token)

    local body
    body=$(kexec curl -s \
        -H "Authorization: Bearer ${token}" \
        "${ECHO_URL}/")

    echo "# Response body: $body"

    local auth_header
    auth_header=$(echo "$body" | jq -r '.headers["authorization"] // empty')
    [[ -z "$auth_header" ]]
}

@test "sidecar proxy sets cluster-name header on upstream request" {
    local token
    token=$(get_token)

    local body
    body=$(kexec curl -s \
        -H "Authorization: Bearer ${token}" \
        "${ECHO_URL}/")

    echo "# Response body: $body"

    echo "$body" | jq -e '.headers["x-forwarded-extra-cluster-name"]' > /dev/null
}
