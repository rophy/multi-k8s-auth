# multi-k8s-auth Design

## Overview

A Go service that validates Kubernetes ServiceAccount JWT tokens across multiple clusters using OIDC discovery.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     multi-k8s-auth                          │
├─────────────────────────────────────────────────────────────┤
│  Config (YAML)                                              │
│  ┌─────────────┬──────────────────────────────────────┐    │
│  │ cluster-a   │ issuer: https://oidc.eks...          │    │
│  │ cluster-b   │ issuer: https://k8s.internal.corp    │    │
│  │             │ ca_cert: /path/to/ca.crt             │    │
│  └─────────────┴──────────────────────────────────────┘    │
├─────────────────────────────────────────────────────────────┤
│  POST /validate                                             │
│  ┌─────────────────┐        ┌────────────────────────┐     │
│  │ Request:        │        │ Response:              │     │
│  │   cluster: "a"  │  ───►  │   cluster: "cluster-a" │     │
│  │   token: "eyJ.."│        │   iss: "https://..."   │     │
│  └─────────────────┘        │   sub: "system:sa:..." │     │
│                             │   aud: [...]           │     │
│          │                  │   kubernetes.io: {...} │     │
│          ▼                  └────────────────────────┘     │
│  ┌─────────────────┐                                       │
│  │ OIDC Verifiers  │ ◄── Fetches JWKS from issuers        │
│  │ (per cluster)   │                                       │
│  └─────────────────┘                                       │
└─────────────────────────────────────────────────────────────┘
```

## API Endpoints

### POST /validate

Validate a Kubernetes ServiceAccount token.

**Request:**
```json
{
  "cluster": "cluster-a",
  "token": "eyJhbGciOiJSUzI1NiIsImtpZCI6Ii..."
}
```

**Response (200 OK):**
```json
{
  "cluster": "cluster-a",
  "iss": "https://oidc.eks.us-west-2.amazonaws.com/id/EXAMPLE",
  "sub": "system:serviceaccount:my-namespace:my-sa",
  "aud": ["my-service"],
  "exp": 1702500000,
  "iat": 1702496400,
  "nbf": 1702496400,
  "kubernetes.io": {
    "namespace": "my-namespace",
    "serviceaccount": {
      "name": "my-sa",
      "uid": "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
    },
    "pod": {
      "name": "my-pod-xyz",
      "uid": "e5f6g7h8-i9j0-1234-klmn-opqrstuvwxyz"
    }
  }
}
```

**Error Response (400 Bad Request):**
```json
{
  "error": "cluster_not_found",
  "message": "No configuration found for cluster: unknown-cluster"
}
```

**Error Response (401 Unauthorized):**
```json
{
  "error": "invalid_signature",
  "message": "Token signature verification failed"
}
```

### GET /health

Health check for liveness/readiness probes.

**Response (200 OK):**
```json
{
  "status": "ok"
}
```

### GET /clusters

List configured cluster names.

**Response (200 OK):**
```json
{
  "clusters": ["cluster-a", "cluster-b", "eks-prod"]
}
```

## Configuration

### Format

```yaml
clusters:
  # Public cloud cluster (uses public CA)
  eks-prod:
    issuer: "https://oidc.eks.us-west-2.amazonaws.com/id/EXAMPLE"

  # Self-hosted cluster with private CA
  internal:
    issuer: "https://k8s.internal.corp"
    ca_cert: "/path/to/internal-ca.crt"
```

### Fields

| Field | Required | Description |
|-------|----------|-------------|
| `issuer` | Yes | OIDC issuer URL (used for discovery and JWKS) |
| `ca_cert` | No | Path to CA certificate file for TLS verification |

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Server listening port |
| `CONFIG_PATH` | `config/clusters.yaml` | Path to cluster config file |

## Error Codes

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `invalid_request` | 400 | Missing or malformed request body |
| `cluster_not_found` | 400 | Cluster name not in configuration |
| `invalid_token` | 401 | Malformed JWT |
| `invalid_signature` | 401 | Signature verification failed |
| `token_expired` | 401 | Token has expired |
| `jwks_fetch_failed` | 500 | Failed to fetch JWKS from issuer |
| `oidc_discovery_failed` | 500 | Failed to fetch OIDC discovery document |

## Project Structure

```
multi-k8s-auth/
├── cmd/
│   └── server/
│       └── main.go              # Entry point, CLI flags
├── internal/
│   ├── config/
│   │   └── config.go            # YAML config loading
│   ├── handler/
│   │   ├── validate.go          # POST /validate
│   │   ├── health.go            # GET /health
│   │   └── clusters.go          # GET /clusters
│   ├── oidc/
│   │   └── verifier.go          # OIDC verifier per cluster
│   └── server/
│       └── server.go            # HTTP server, routing
├── config/
│   └── clusters.example.yaml
├── go.mod
├── Dockerfile
└── README.md
```

## Dependencies

| Package | Version | Purpose |
|---------|---------|---------|
| `github.com/go-chi/chi/v5` | v5.x | HTTP routing and middleware |
| `github.com/coreos/go-oidc/v3` | v3.x | OIDC discovery, JWKS, JWT validation |
| `gopkg.in/yaml.v3` | v3.x | Configuration parsing |

## Authentication Flow

1. Client pod obtains projected ServiceAccount token with custom audience
2. Client sends token to multi-k8s-auth with cluster name
3. Service looks up cluster configuration
4. Service validates token using OIDC discovery and JWKS from issuer
5. Service returns decoded claims with cluster name added
6. Caller uses claims for authorization decisions

## Security Considerations

- Only explicitly configured clusters are trusted
- JWKS fetched over TLS (custom CA supported)
- Tokens validated for signature and expiration
- Audience claim returned for caller to validate
- No secrets stored - relies on public JWKS endpoints

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Response format | Full JWT claims | Preserves all info, caller decides what to use |
| Issuer in response | Original URL | OIDC-compliant, cluster name added separately |
| Audience validation | Caller responsibility | Authentication only, not authorization |
| Config format | YAML with issuer URL | Standard OIDC approach, works with all K8s distributions |
| CA certificate | File path only | Simple, works with mounted secrets |
