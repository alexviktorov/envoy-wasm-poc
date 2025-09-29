# Service Mesh Policy Enforcement Point (PEP) Demo

## Overview

This project demonstrates a complete Policy Enforcement Point (PEP) implementation using Envoy WASM modules within a Consul service mesh on Google Kubernetes Engine (GKE). The demo showcases JWT-based authorization between microservices, orthogonal to the underlying mTLS/x509 identity provided by the service mesh.

## Architecture

```
┌─────────────┐      ┌──────────────┐      ┌─────────────┐
│  Service A  │─────▶│ JWT Vending  │      │  Mock SGNL  │
│             │      │   Service    │      │     PDP     │
└──────┬──────┘      └──────────────┘      └──────▲──────┘
       │                                           │
       │ 1. Get JWT                                │
       │ 2. Call Service B with JWT                │
       │                                           │ 3. Validate
       ▼                                           │    with PDP
┌─────────────┐                                   │
│  Service B  │───────────────────────────────────┘
│             │
└─────────────┘
```

### Components

1. **Service A** - Client service that calls Service B
2. **Service B** - Server service that receives calls and validates authorization
3. **JWT Vending Service** - Issues valid and invalid JWTs on demand
4. **Mock SGNL PDP** - Policy Decision Point that evaluates authorization requests
5. **Client WASM Module** - Envoy filter that fetches JWTs and injects them into outbound requests
6. **Server WASM Module** - Envoy filter that extracts JWTs and validates them with the PDP
7. **Consul Service Mesh** - Provides mTLS identity and Envoy sidecar injection

## Technology Stack

- **Language**: Go (services), Rust (WASM modules)
- **Service Mesh**: HashiCorp Consul with Envoy sidecars
- **Policy Enforcement**: Envoy WASM filters (proxy-wasm-rust-sdk v0.2)
- **Envoy Version**: v1.35+ (for WASM support)
- **Infrastructure**: GKE (Google Kubernetes Engine) or Docker Compose (local)
- **IaC**: Terraform

## Prerequisites

### For Local Testing (Docker Compose)

- Docker Desktop or Docker Engine
- `docker compose` v2+
- `rust` and `cargo` (for building WASM modules)
- `go` >= 1.21 (for Go services)
- `curl` and `jq` (for testing)

### For GKE Deployment

- `gcloud` CLI configured with appropriate project and credentials
- `terraform` >= 1.0
- `kubectl`
- `helm` >= 3.0
- `docker`
- `rust` and `cargo` (for building WASM modules)
- `go` >= 1.21 (for Go services)
- `consul` CLI (optional, for debugging)

## Project Structure

```
.
├── README.md
├── terraform/                  # GKE infrastructure
│   ├── main.tf
│   ├── variables.tf
│   └── outputs.tf
├── services/
│   ├── jwt-vending/           # JWT token vending service
│   │   ├── main.go
│   │   ├── Dockerfile
│   │   └── go.mod
│   ├── sgnl-pdp/              # Mock SGNL PDP service
│   │   ├── main.go
│   │   ├── Dockerfile
│   │   └── go.mod
│   ├── service-a/             # Client service
│   │   ├── main.go
│   │   ├── Dockerfile
│   │   └── go.mod
│   └── service-b/             # Server service
│       ├── main.go
│       ├── Dockerfile
│       └── go.mod
├── wasm/
│   ├── client-filter-rust/    # Rust WASM module for Service A (JWT injection)
│   │   ├── src/lib.rs
│   │   ├── Cargo.toml
│   │   └── target/wasm32-wasip1/release/client_filter_rust.wasm
│   └── server-filter-rust/    # Rust WASM module for Service B (JWT validation)
│       ├── src/lib.rs
│       ├── Cargo.toml
│       └── target/wasm32-wasip1/release/server_filter_rust.wasm
├── k8s/
│   ├── consul-values.yaml     # Consul Helm chart values
│   ├── jwt-vending.yaml       # JWT service deployment
│   ├── sgnl-pdp.yaml          # PDP service deployment
│   ├── service-a.yaml         # Service A deployment
│   └── service-b.yaml         # Service B deployment
└── scripts/
    ├── build.sh               # Build all services and WASM modules
    ├── deploy.sh              # Deploy to GKE
    └── test.sh                # Run end-to-end tests
```

## WASM Implementation: Why Rust?

This project uses **Rust** with **proxy-wasm-rust-sdk v0.2** for the WASM filters instead of Go. Here's why:

### Rust Advantages ✅

- **Production-ready**: proxy-wasm-rust-sdk is mature and battle-tested
- **Full Envoy compatibility**: Works with Envoy v1.28+ without issues
- **Proper lifecycle management**: No initialization conflicts with Envoy's sandbox
- **Smaller binary size**: ~270KB vs 2MB+ for Go
- **Better performance**: Lower memory overhead and faster execution

### Go Limitations ❌

The proxy-wasm-go-sdk has significant issues:

- **Restricted callback errors**: Calls host APIs during module initialization (`_start`/`init()`) which Envoy's sandbox restricts
- **WASI compatibility**: Older Envoy versions don't support all WASI functions Go requires
- **Alpha status**: SDK is marked as alpha and not production-ready
- **Larger binaries**: Native Go WASM produces 2MB+ files vs Rust's ~270KB

**Note**: Go WASM filters are included in `wasm/test-go-native/` and `wasm/test-minimal/` for reference, but they fail to load in Envoy. The working Rust implementation is in `wasm/client-filter-rust/` and `wasm/server-filter-rust/`.

## Quick Start

### Option 1: Local Testing (Recommended for Development)

**See [LOCAL_TESTING.md](LOCAL_TESTING.md) for detailed instructions.**

```bash
# 1. Build WASM modules
./scripts/build.sh

# 2. Start all services with Docker Compose
docker compose up --build -d

# 3. Run tests
./scripts/test-local.sh --all

# 4. View logs
docker compose logs -f

# 5. Cleanup
docker compose down
```

This approach:
- ✅ No GCP account needed
- ✅ Runs entirely locally
- ✅ Fast iteration cycle
- ✅ Uses Envoy proxies with WASM filters
- ⚠️  No Consul service mesh (standalone Envoy instead)
- ⚠️  No mTLS between services

### Option 2: Full GKE Deployment

### 1. Set Up Infrastructure

```bash
# Configure your GCP project
export GCP_PROJECT="your-project-id"
export GCP_REGION="us-central1"

# Create GKE cluster with Terraform
cd terraform
terraform init
terraform apply -var="project_id=${GCP_PROJECT}" -var="region=${GCP_REGION}"

# Get cluster credentials
gcloud container clusters get-credentials wasm-pep-demo --region=${GCP_REGION}
```

### 2. Build Services and WASM Modules

```bash
# Build all Docker images and WASM modules
./scripts/build.sh

# Push images to GCR
./scripts/build.sh --push
```

### 3. Deploy Consul Service Mesh

```bash
# Add HashiCorp Helm repository
helm repo add hashicorp https://helm.releases.hashicorp.com
helm repo update

# Install Consul
helm install consul hashicorp/consul -f k8s/consul-values.yaml --create-namespace --namespace consul
```

### 4. Deploy Services

```bash
./scripts/deploy.sh
```

### 5. Run Tests

```bash
# Test positive flow (Service A → Service B with valid JWT on allowed asset)
./scripts/test.sh --positive

# Test negative flow (Service A → Service B with invalid JWT)
./scripts/test.sh --negative

# Test negative flow (Service A → Service B with valid JWT on denied asset)
./scripts/test.sh --denied
```

## Detailed Testing

### Positive Flow: Authorized Access to Asset X

This flow demonstrates successful authorization:

1. Service A requests a valid JWT from the JWT vending service
2. The client WASM module intercepts the outbound request and injects the JWT
3. Service B's server WASM module extracts the JWT
4. The WASM module calls the SGNL PDP to validate the request (Service A → Service B, asset X)
5. PDP returns `allowed: true`
6. Service B processes the request and returns a response with decoded JWT info

```bash
# Port-forward to Service A
kubectl port-forward -n default svc/service-a 8080:8080

# Make a request to Service B through Service A for asset X (allowed)
curl -X POST http://localhost:8080/call-service-b \
  -H "Content-Type: application/json" \
  -d '{"asset": "asset-x", "use_valid_token": true}'

# Expected response:
# {
#   "success": true,
#   "response_from_b": {
#     "message": "Call received from service-a",
#     "jwt_claims": {
#       "sub": "service-a",
#       "iss": "jwt-vending-service",
#       "aud": "service-mesh",
#       "exp": 1234567890,
#       "iat": 1234567890
#     },
#     "authorized": true,
#     "asset": "asset-x"
#   }
# }
```

### Negative Flow 1: Invalid JWT

This flow demonstrates rejection due to invalid JWT:

```bash
curl -X POST http://localhost:8080/call-service-b \
  -H "Content-Type: application/json" \
  -d '{"asset": "asset-x", "use_valid_token": false}'

# Expected response: 403 Forbidden
# {
#   "error": "Invalid JWT signature"
# }
```

### Negative Flow 2: Unauthorized Asset Access

This flow demonstrates rejection due to policy denial:

```bash
curl -X POST http://localhost:8080/call-service-b \
  -H "Content-Type: application/json" \
  -d '{"asset": "asset-y", "use_valid_token": true}'

# Expected response: 403 Forbidden
# {
#   "error": "Access denied by policy",
#   "pdp_response": {
#     "decision": "Deny",
#     "reason": "Service service-a is not allowed to access asset-y"
#   }
# }
```

## Component Details

### JWT Vending Service

- **Endpoint**: `POST /token/valid` - Issues a valid JWT
- **Endpoint**: `POST /token/invalid` - Issues an invalid JWT (wrong signature)
- **Port**: 8081

The JWT contains:
- `sub`: Service identity (e.g., "service-a")
- `iss`: "jwt-vending-service"
- `aud`: "service-mesh"
- `exp`: Expiration timestamp
- `iat`: Issued at timestamp

### Mock SGNL PDP Service

- **Endpoint**: `POST /access/v2/evaluations`
- **Port**: 8082

Request format:
```json
{
  "principal": {
    "id": "service-a"
  },
  "queries": [
    {
      "assetId": "asset-x",
      "action": "call"
    }
  ]
}
```

Response format:
```json
{
  "decisions": [
    {
      "decision": "Allow",
      "reason": "Service service-a is allowed to access asset-x"
    }
  ]
}
```

Policy rules:
- Service A → Service B on asset-x: **Allow**
- Service A → Service B on asset-y: **Deny**

### Client WASM Module (Service A)

This Envoy WASM filter runs on Service A's sidecar and:
1. Intercepts outbound HTTP requests to Service B
2. Calls the JWT vending service to get a token
3. Injects the JWT into the `Authorization: Bearer <token>` header
4. Forwards the request

### Server WASM Module (Service B)

This Envoy WASM filter runs on Service B's sidecar and:
1. Intercepts inbound HTTP requests
2. Extracts the JWT from the `Authorization` header
3. Validates the JWT signature
4. Calls the SGNL PDP with the JWT claims and request context
5. Allows or denies the request based on the PDP decision

## Consul Service Mesh Integration

The demo uses Consul Connect to:
- Automatically inject Envoy sidecar proxies for each service
- Establish mTLS connections between services
- Load WASM filters into the Envoy sidecars via Consul service configuration

Service intentions in Consul allow service-to-service communication at the mesh level, while the WASM filters provide fine-grained, JWT-based authorization.

## Troubleshooting

### Check Pod Status
```bash
kubectl get pods -A
```

### View Service Logs
```bash
kubectl logs -f <pod-name> -c service-a
kubectl logs -f <pod-name> -c consul-connect-envoy-sidecar
```

### Check Consul Service Registration
```bash
kubectl exec -it consul-server-0 -n consul -- consul catalog services
```

### Debug WASM Module
```bash
# Check Envoy configuration
kubectl exec -it <pod-name> -c consul-connect-envoy-sidecar -- curl localhost:19000/config_dump
```

### Common Issues

1. **WASM module not loading**: Check that the WASM file is accessible via ConfigMap and the path is correct in the Envoy filter configuration
2. **JWT validation fails**: Ensure the JWT vending service and PDP are reachable from the Envoy sidecars
3. **Service mesh connectivity issues**: Verify Consul service intentions allow the traffic

## Cleanup

```bash
# Delete Kubernetes resources
kubectl delete -f k8s/

# Uninstall Consul
helm uninstall consul -n consul

# Destroy GKE cluster
cd terraform
terraform destroy
```

## Future Enhancements

- JWT caching in client WASM module
- Token refresh logic
- Metrics and observability (Prometheus/Grafana)
- More sophisticated PDP policies
- Integration with actual SGNL service
- Multi-cluster service mesh
- Rate limiting and circuit breaking

## License

MIT