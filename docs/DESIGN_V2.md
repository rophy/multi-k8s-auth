# kube-federated-auth V2 Design

## Overview

V2 changes the API from a custom `/validate` endpoint to a **standard Kubernetes TokenReview API**. This enables service providers to integrate using familiar Kubernetes APIs without awareness of cross-cluster complexity.

## Goals

1. **Standard API**: Use Kubernetes TokenReview API specification
2. **Minimal Integration**: Service providers use standard K8s client libraries
3. **Implicit Cross-Cluster**: Cross-cluster support via configuration, not code changes
4. **Backward Compatibility**: V1 `/validate` endpoint remains available during transition

## Design Principles

### Why TokenReview API?

The Kubernetes community recommends TokenReview for ServiceAccount token validation because:
- Real-time revocation when bound objects (Pods, ServiceAccounts) are deleted
- Standard API that service providers may already support
- Part of the `authentication.k8s.io/v1` API group

### Why Configurable Endpoint?

Service providers have legitimate reasons to specify custom K8s API endpoints:
- Running outside the cluster (local dev, CI/CD, external services)
- Using `kubectl proxy` for local development
- API server behind load balancer or HA proxy
- Platform-specific routing (Rancher, OpenShift)
- Service mesh configurations
- Testing against mock servers

This makes cross-cluster support an implicit capability without requiring special justification.

## Architecture

### V1 vs V2 Comparison

| Aspect | V1 | V2 |
|--------|----|----|
| API Endpoint | `POST /validate` | `POST /apis/authentication.k8s.io/v1/tokenreviews` |
| Request Format | `{"cluster": "...", "token": "..."}` | Standard TokenReview |
| Response Format | Custom JWT claims object | Standard TokenReview |
| Cluster Selection | Explicit `cluster` field in body | Hostname-based routing |
| Validation Method | OIDC/JWKS | OIDC/JWKS or TokenReview forwarding (internal choice) |

### Request Flow

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ Service Provider (e.g., MariaDB)                                            │
│                                                                             │
│   Configurable endpoint:                                                    │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │ TOKENREVIEW_ENDPOINT = api.app1.kube-fed.svc.cluster.local          │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
│   POST {ENDPOINT}/apis/authentication.k8s.io/v1/tokenreviews                │
│   Body: Standard TokenReview                                                │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ kube-federated-auth                                                         │
│                                                                             │
│   1. Extract cluster from Host header (api.{cluster}.kube-fed.svc)          │
│   2. Validate token (JWKS or forward to remote TokenReview)                 │
│   3. Return standard TokenReview response                                   │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                    ┌─────────────────┼─────────────────┐
                    ▼                 ▼                 ▼
             ┌───────────┐     ┌───────────┐     ┌───────────┐
             │   app1    │     │   app2    │     │   app3    │
             │  cluster  │     │  cluster  │     │  cluster  │
             └───────────┘     └───────────┘     └───────────┘
```

## API Specification

### Endpoint

```
POST /apis/authentication.k8s.io/v1/tokenreviews
```

### Request

Standard Kubernetes TokenReview:

```json
{
  "apiVersion": "authentication.k8s.io/v1",
  "kind": "TokenReview",
  "spec": {
    "token": "eyJhbGciOiJSUzI1NiIs...",
    "audiences": ["my-service"]
  }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.token` | string | Yes | The ServiceAccount JWT token to validate |
| `spec.audiences` | []string | No | Expected audiences for the token |

### Response

Standard Kubernetes TokenReview:

```json
{
  "apiVersion": "authentication.k8s.io/v1",
  "kind": "TokenReview",
  "status": {
    "authenticated": true,
    "user": {
      "username": "system:serviceaccount:default:my-app",
      "uid": "abc-123",
      "groups": [
        "system:serviceaccounts",
        "system:serviceaccounts:default"
      ],
      "extra": {
        "authentication.kubernetes.io/pod-name": ["my-pod"],
        "authentication.kubernetes.io/pod-uid": ["pod-uid-123"]
      }
    },
    "audiences": ["my-service"]
  }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `status.authenticated` | bool | Whether the token is valid |
| `status.user.username` | string | ServiceAccount identity (`system:serviceaccount:{namespace}:{name}`) |
| `status.user.uid` | string | Unique identifier for the ServiceAccount |
| `status.user.groups` | []string | Groups the ServiceAccount belongs to |
| `status.user.extra` | map | Additional claims (pod binding, etc.) |
| `status.audiences` | []string | Audiences the token is valid for |
| `status.error` | string | Error message if authentication failed |

### Error Response

```json
{
  "apiVersion": "authentication.k8s.io/v1",
  "kind": "TokenReview",
  "status": {
    "authenticated": false,
    "error": "token has expired"
  }
}
```

## Cluster Routing

### Hostname-Based Routing

The target cluster is determined by the `Host` header:

| Hostname | Cluster |
|----------|---------|
| `api.kube-fed.svc.cluster.local` | Local/default cluster |
| `api.{cluster}.kube-fed.svc.cluster.local` | Named cluster |

Examples:
- `api.kube-fed.svc.cluster.local` → validate against local cluster
- `api.app1.kube-fed.svc.cluster.local` → validate against "app1" cluster
- `api.app2.kube-fed.svc.cluster.local` → validate against "app2" cluster

### Extracting Cluster from Host

```go
func extractClusterFromHost(host string) string {
    // api.kube-fed.svc.cluster.local → "" (local)
    // api.app1.kube-fed.svc.cluster.local → "app1"

    parts := strings.Split(host, ".")
    if len(parts) >= 2 && parts[0] == "api" {
        if parts[1] == "kube-fed" {
            return "" // local cluster
        }
        return parts[1] // cluster name
    }
    return ""
}
```

### Kubernetes Service Configuration

One service per cluster:

```yaml
# Local cluster service
apiVersion: v1
kind: Service
metadata:
  name: api.kube-fed
  namespace: kube-federated-auth
spec:
  selector:
    app: kube-federated-auth
  ports:
    - port: 443
      targetPort: 8080
---
# Remote cluster service (one per cluster)
apiVersion: v1
kind: Service
metadata:
  name: api.app1.kube-fed
  namespace: kube-federated-auth
spec:
  selector:
    app: kube-federated-auth
  ports:
    - port: 443
      targetPort: 8080
```

## Internal Implementation Options

V2 API compliance allows flexibility in internal implementation:

### Option A: JWKS Validation (Current V1 Approach)

- Validate tokens locally using cached JWKS
- Format results as TokenReview response
- Pros: Fast, no per-request remote calls
- Cons: No real-time revocation detection

### Option B: TokenReview Forwarding

- Forward TokenReview to remote cluster's API server
- Return response as-is
- Pros: Real-time revocation, simpler code
- Cons: Network dependency, latency

### Recommendation

Start with **Option A** (JWKS) to minimize changes from V1. The internal implementation can be changed later without affecting the API contract.

## Service Provider Integration

### Example: MariaDB Plugin

The plugin code uses standard Kubernetes types:

```c
// Configuration (environment variable)
// TOKENREVIEW_ENDPOINT=https://api.app1.kube-fed.svc.cluster.local

// Validation request
POST {TOKENREVIEW_ENDPOINT}/apis/authentication.k8s.io/v1/tokenreviews
Content-Type: application/json

{
  "apiVersion": "authentication.k8s.io/v1",
  "kind": "TokenReview",
  "spec": {
    "token": "<client-provided-token>",
    "audiences": ["mariadb"]
  }
}
```

### Integration Scenarios

| Scenario | Endpoint Configuration |
|----------|------------------------|
| In-cluster only | `https://kubernetes.default.svc` |
| Cross-cluster via kube-fed | `https://api.{cluster}.kube-fed.svc.cluster.local` |
| Local development | `http://localhost:8001` (kubectl proxy) |

The same plugin code works for all scenarios - only configuration changes.

## Migration Path

### Phase 1: Add V2 Endpoint

- Add `/apis/authentication.k8s.io/v1/tokenreviews` handler
- Keep V1 `/validate` endpoint unchanged
- Both endpoints use same internal validation logic

### Phase 2: Deploy Services

- Create per-cluster Kubernetes Services
- Update documentation with V2 examples

### Phase 3: Deprecate V1

- Mark `/validate` endpoint as deprecated
- Add deprecation warnings to responses

### Phase 4: Remove V1

- Remove `/validate` endpoint
- Remove V1-specific code

## Configuration

### Cluster Configuration (Unchanged from V1)

```yaml
clusters:
  # Local cluster (public OIDC endpoint)
  local:
    issuer: https://container.googleapis.com/v1/projects/xxx/locations/xxx/clusters/xxx

  # Remote clusters
  app1:
    issuer: https://kubernetes.default.svc.cluster.local
    api_server: https://app1-cluster.example.com:6443
    ca_cert: /etc/kube-fed/clusters/app1/ca.crt
    token_path: /etc/kube-fed/clusters/app1/token

  app2:
    issuer: https://kubernetes.default.svc.cluster.local
    api_server: https://app2-cluster.example.com:6443
    ca_cert: /etc/kube-fed/clusters/app2/ca.crt
    token_path: /etc/kube-fed/clusters/app2/token

renewal:
  interval: 1h
  token_duration: 168h
  renew_before: 48h
```

## Code Changes Summary

### New Files

| File | Description |
|------|-------------|
| `internal/handler/tokenreview.go` | TokenReview endpoint handler |

### Modified Files

| File | Changes |
|------|---------|
| `internal/server/server.go` | Add TokenReview route |
| `internal/handler/validate.go` | Extract shared validation logic |

### Unchanged

| Component | Reason |
|-----------|--------|
| `internal/config/` | Cluster configuration unchanged |
| `internal/credentials/` | Credential management unchanged |
| `internal/oidc/` | JWKS validation reused |
| `internal/handler/clusters.go` | Debugging endpoint unchanged |
| `internal/handler/health.go` | Health check unchanged |

## Open Questions

1. **TLS**: Should kube-federated-auth terminate TLS, or rely on service mesh/ingress?
2. **Authentication**: Should the TokenReview endpoint require authentication from callers?
3. **Rate Limiting**: Should per-cluster rate limits be implemented?
4. **Metrics**: What Prometheus metrics should be exposed?

## References

- [Kubernetes TokenReview API](https://kubernetes.io/docs/reference/kubernetes-api/authentication-resources/token-review-v1/)
- [Managing Service Accounts](https://kubernetes.io/docs/reference/access-authn-authz/service-accounts-admin/)
- [Projected Volumes](https://kubernetes.io/docs/concepts/storage/projected-volumes/)
