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

@test "kubectl can call TokenReview via kubeconfig" {
    local token
    token=$(get_token)

    # Generate kubeconfig and run kubectl inside the pod
    local result
    result=$(kexec sh -c "
        KUBECONFIG=\$(mktemp)
        cat > \$KUBECONFIG <<KUBEEOF
apiVersion: v1
kind: Config
clusters:
- cluster:
    server: ${SERVICE_URL}
  name: federated-auth
contexts:
- context:
    cluster: federated-auth
    user: test
  name: test
current-context: test
users:
- name: test
  user:
    token: ${token}
KUBEEOF
        kubectl --kubeconfig \$KUBECONFIG create --raw /apis/authentication.k8s.io/v1/tokenreviews -f - <<JSONEOF
{\"apiVersion\":\"authentication.k8s.io/v1\",\"kind\":\"TokenReview\",\"spec\":{\"token\":\"${token}\"}}
JSONEOF
        rm -f \$KUBECONFIG
    ")

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
