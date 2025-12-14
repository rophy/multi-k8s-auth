#!/bin/bash
# Setup multi-cluster authentication
# This script runs AFTER cluster-b is deployed, BEFORE cluster-a is deployed
# It extracts cluster-b credentials and prepares cluster-a configuration
set -e

CLUSTER_A="cluster-a"
CLUSTER_B="cluster-b"
NAMESPACE="multi-k8s-auth"

echo "=========================================="
echo "Configuring Multi-Cluster Authentication"
echo "=========================================="
echo ""

# Get cluster IPs
CLUSTER_A_IP=$(docker inspect ${CLUSTER_A}-control-plane --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}')
CLUSTER_B_IP=$(docker inspect ${CLUSTER_B}-control-plane --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}')
echo "Cluster-A API Server IP: $CLUSTER_A_IP"
echo "Cluster-B API Server IP: $CLUSTER_B_IP"

# Switch to cluster-b and extract credentials
echo ""
echo "Extracting cluster-b credentials..."
kubectl config use-context kind-${CLUSTER_B} > /dev/null

# Wait for namespace to exist
echo "Waiting for namespace ${NAMESPACE} in cluster-b..."
timeout 60s bash -c "until kubectl get namespace ${NAMESPACE} > /dev/null 2>&1; do sleep 2; done" || {
    echo "ERROR: Namespace ${NAMESPACE} not ready in cluster-b"
    exit 1
}

# Wait for ServiceAccount to exist
echo "Waiting for ServiceAccount test-client..."
timeout 60s bash -c "until kubectl get serviceaccount test-client -n ${NAMESPACE} > /dev/null 2>&1; do sleep 2; done" || {
    echo "ERROR: ServiceAccount test-client not ready"
    exit 1
}

# Create a long-lived token for accessing cluster-b OIDC endpoints
TOKEN_FILE="/tmp/cluster-b-token.txt"
kubectl create token test-client -n ${NAMESPACE} --duration=24h > "$TOKEN_FILE"
echo "✅ Token created"

# Extract CA certificate
CA_FILE="/tmp/cluster-b-ca.crt"
kubectl get configmap kube-root-ca.crt -n kube-system -o jsonpath='{.data.ca\.crt}' > "$CA_FILE"
echo "✅ CA certificate extracted"

# Get issuer from token
ISSUER=$(cat "$TOKEN_FILE" | cut -d'.' -f2 | base64 -d 2>/dev/null | jq -r '.iss')
echo "Cluster-B Issuer: $ISSUER"

# Switch to cluster-a and prepare resources
echo ""
echo "Preparing cluster-a resources..."
kubectl config use-context kind-${CLUSTER_A} > /dev/null

# Create namespace if it doesn't exist
kubectl create namespace ${NAMESPACE} 2>/dev/null || true
echo "✅ Namespace ready"

# Create or update secret with cluster-b credentials
kubectl delete secret cluster-certs -n ${NAMESPACE} 2>/dev/null || true
kubectl create secret generic cluster-certs \
  --from-file=cluster-b-ca.crt="$CA_FILE" \
  --from-file=cluster-b-token="$TOKEN_FILE" \
  -n ${NAMESPACE}
echo "✅ Secret created"

# Create ConfigMap with both cluster configurations
kubectl apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: multi-k8s-auth-config
  namespace: ${NAMESPACE}
data:
  clusters.yaml: |
    clusters:
      # Local cluster (cluster-a)
      cluster-a:
        issuer: "https://kubernetes.default.svc.cluster.local"
        ca_cert: "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
        token_path: "/var/run/secrets/kubernetes.io/serviceaccount/token"

      # Remote cluster (cluster-b)
      cluster-b:
        issuer: "${ISSUER}"
        api_server: "https://${CLUSTER_B_IP}:6443"
        ca_cert: "/etc/multi-k8s-auth/certs/cluster-b-ca.crt"
        token_path: "/etc/multi-k8s-auth/certs/cluster-b-token"
EOF
echo "✅ ConfigMap created"

# Update cluster-b test client with correct service URL (for later use)
echo ""
echo "Configuring cluster-b test client..."
kubectl config use-context kind-${CLUSTER_B} > /dev/null
kubectl set env deployment/test-client -n ${NAMESPACE} SERVICE_URL="http://${CLUSTER_A_IP}:30080" 2>/dev/null || true

echo ""
echo "=========================================="
echo "✅ Multi-Cluster Configuration Complete"
echo "=========================================="
echo ""
echo "Summary:"
echo "  Cluster-A API: https://${CLUSTER_A_IP}:6443"
echo "  Cluster-B API: https://${CLUSTER_B_IP}:6443"
echo "  Service URL:   http://${CLUSTER_A_IP}:30080"
echo ""
echo "Created in cluster-a:"
echo "  - Namespace: ${NAMESPACE}"
echo "  - Secret: cluster-certs (cluster-b credentials)"
echo "  - ConfigMap: multi-k8s-auth-config"
echo ""
