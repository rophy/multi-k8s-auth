#!/usr/bin/env bats

setup_file() {
    load 'test_helper'
    wait_for_service
}

setup() {
    load 'test_helper'
}

@test "health endpoint returns ok" {
    local result
    result=$(kexec curl -s "${SERVICE_URL}/health")

    echo "# Response: $result"

    local status
    status=$(echo "$result" | jq -r '.status')
    [[ "$status" == "ok" ]]
}

@test "clusters endpoint lists configured clusters" {
    local result
    result=$(kexec curl -s "${SERVICE_URL}/clusters")

    echo "# Response: $result"

    local names
    names=$(echo "$result" | jq -r '.clusters[].name')
    echo "$names" | grep -q "$CLUSTER_NAME"
}

@test "TokenReview authenticates valid token" {
    local token
    token=$(get_token)

    local result
    result=$(token_review "$token")

    echo "# Response: $result"

    # Check authenticated
    local authenticated
    authenticated=$(echo "$result" | jq -r '.status.authenticated')
    [[ "$authenticated" == "true" ]]

    # Check username starts with system:serviceaccount:
    local username
    username=$(echo "$result" | jq -r '.status.user.username')
    [[ "$username" == system:serviceaccount:* ]]

    # Check apiVersion and kind
    local apiVersion kind
    apiVersion=$(echo "$result" | jq -r '.apiVersion')
    kind=$(echo "$result" | jq -r '.kind')
    [[ "$apiVersion" == "authentication.k8s.io/v1" ]]
    [[ "$kind" == "TokenReview" ]]

    # Check cluster-name in extra field
    local clusterExtra
    clusterExtra=$(echo "$result" | jq -r '.status.user.extra["authentication.kubernetes.io/cluster-name"][0]')
    [[ "$clusterExtra" == "$CLUSTER_NAME" ]]
}

@test "TokenReview works with separate caller and review tokens" {
    # Caller authenticates with their default SA token (Authorization header),
    # and reviews a different token (the projected one in request body)
    local caller_token
    caller_token=$(get_caller_token)

    local review_token
    review_token=$(get_token)

    local result
    result=$(kexec curl -s -X POST "${SERVICE_URL}/apis/authentication.k8s.io/v1/tokenreviews" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer ${caller_token}" \
        -d "{\"apiVersion\":\"authentication.k8s.io/v1\",\"kind\":\"TokenReview\",\"spec\":{\"token\":\"${review_token}\"}}")

    echo "# Response: $result"

    local authenticated
    authenticated=$(echo "$result" | jq -r '.status.authenticated')
    [[ "$authenticated" == "true" ]]

    local username
    username=$(echo "$result" | jq -r '.status.user.username')
    [[ "$username" == system:serviceaccount:* ]]
}

@test "TokenReview rejects invalid token" {
    local result
    result=$(token_review "invalid.token.here")

    echo "# Response: $result"

    # authenticated should be false or absent (null)
    local authenticated
    authenticated=$(echo "$result" | jq -r '.status.authenticated')
    [[ "$authenticated" == "false" ]] || [[ "$authenticated" == "null" ]]

    local error
    error=$(echo "$result" | jq -r '.status.error')
    [[ -n "$error" ]]
    [[ "$error" != "null" ]]
}

@test "TokenReview rejects request without Authorization header" {
    # Send a TokenReview without caller auth — should get 401
    local http_code
    http_code=$(kexec curl -s -o /dev/null -w '%{http_code}' -X POST \
        "${SERVICE_URL}/apis/authentication.k8s.io/v1/tokenreviews" \
        -H "Content-Type: application/json" \
        -d '{"apiVersion":"authentication.k8s.io/v1","kind":"TokenReview","spec":{"token":"some-token"}}')

    echo "# HTTP status: $http_code"
    [[ "$http_code" == "401" ]]
}
