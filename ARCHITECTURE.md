# Architecture Documentation

## Overview

This document describes the architecture of the WASM PEP (Policy Enforcement Point) demo, which demonstrates JWT-based authorization in a service mesh using Envoy WASM filters.

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         GKE Cluster                                  │
│                                                                       │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │                    Consul Service Mesh                         │  │
│  │  (mTLS, Service Discovery, Envoy Sidecar Injection)           │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                                                                       │
│  ┌─────────────────────┐         ┌─────────────────────┐           │
│  │   Service A Pod     │         │   Service B Pod     │           │
│  │  ┌───────────────┐  │         │  ┌───────────────┐  │           │
│  │  │  Service A    │  │         │  │  Service B    │  │           │
│  │  │  (Go HTTP)    │  │         │  │  (Go HTTP)    │  │           │
│  │  └───────────────┘  │         │  └───────────────┘  │           │
│  │         ↕            │         │         ↑            │           │
│  │  ┌───────────────┐  │         │  ┌───────────────┐  │           │
│  │  │ Envoy Sidecar │  │  HTTP   │  │ Envoy Sidecar │  │           │
│  │  │ + Client WASM │──┼─────────┼─▶│ + Server WASM │  │           │
│  │  └───────────────┘  │  mTLS   │  └───────────────┘  │           │
│  │         │            │         │         │            │           │
│  └─────────┼────────────┘         └─────────┼────────────┘           │
│            │                                 │                        │
│            │ 1. Fetch JWT                    │ 3. Validate JWT        │
│            ↓                                 ↓    & Check Policy     │
│  ┌─────────────────────┐         ┌─────────────────────┐           │
│  │  JWT Vending Svc    │         │   Mock SGNL PDP     │           │
│  │  (RSA Key Gen)      │         │   (Policy Rules)    │           │
│  └─────────────────────┘         └─────────────────────┘           │
│                                                                       │
└─────────────────────────────────────────────────────────────────────┘
```

## Components

### 1. Service A (Client Service)

**Purpose**: Initiates requests to Service B

**Technology**: Go HTTP server

**Key Functions**:
- Receives external requests via REST API
- Calls JWT vending service to obtain tokens
- Makes authenticated calls to Service B
- Returns responses to clients

**Endpoints**:
- `POST /call-service-b` - Orchestrates call to Service B
- `GET /health` - Health check

**Environment Variables**:
- `SERVICE_ID`: Identifier for this service (default: "service-a")
- `JWT_VENDING_URL`: URL of JWT vending service
- `SERVICE_B_URL`: URL of Service B (via Consul Connect)

### 2. Service B (Server Service)

**Purpose**: Receives and processes authenticated requests

**Technology**: Go HTTP server

**Key Functions**:
- Receives requests from Service A
- Parses JWT claims from Authorization header
- Returns response with caller information
- Respects PDP decisions from WASM filter

**Endpoints**:
- `GET /process?asset=<asset-id>` - Process authenticated request
- `GET /health` - Health check

**Headers Expected**:
- `Authorization: Bearer <jwt>` - JWT token
- `X-Service-ID` - Calling service identifier
- `X-PDP-Decision` - PDP decision (added by WASM filter)
- `X-PDP-Reason` - PDP decision reason (added by WASM filter)

### 3. JWT Vending Service

**Purpose**: Issues JWT tokens for service-to-service authentication

**Technology**: Go HTTP server with RSA key generation

**Key Functions**:
- Generates RSA key pairs on startup (valid and invalid)
- Issues valid JWTs signed with valid private key
- Issues invalid JWTs signed with different private key (for testing)
- Provides public key for token verification

**Endpoints**:
- `POST /token/valid` - Issue valid JWT
- `POST /token/invalid` - Issue invalid JWT (for testing)
- `GET /public-key` - Get RSA public key (PEM format)
- `GET /health` - Health check

**JWT Claims**:
```json
{
  "sub": "service-a",           // Subject (service identity)
  "iss": "jwt-vending-service", // Issuer
  "aud": "service-mesh",        // Audience
  "exp": 1234567890,            // Expiration time
  "iat": 1234567890,            // Issued at
  "nbf": 1234567890             // Not before
}
```

### 4. Mock SGNL PDP (Policy Decision Point)

**Purpose**: Evaluates authorization policies

**Technology**: Go HTTP server

**Key Functions**:
- Implements SGNL API format
- Evaluates access based on principal and asset
- Returns Allow/Deny decisions with reasons

**Endpoints**:
- `POST /access/v2/evaluations` - Evaluate authorization
- `GET /policies` - View current policy rules
- `GET /health` - Health check

**Policy Rules** (hardcoded for demo):
```
service-a → asset-x: ALLOW
service-a → asset-y: DENY
service-b → asset-x: ALLOW
service-b → asset-y: ALLOW
```

**Request Format**:
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

**Response Format**:
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

### 5. Client WASM Filter

**Purpose**: Intercepts outbound requests from Service A to inject JWTs

**Technology**: TinyGo (compiled to WASM)

**Execution Context**: Runs in Service A's Envoy sidecar

**Flow**:
1. `OnHttpRequestHeaders()` - Intercepts outbound request
2. Checks if destination is Service B
3. Dispatches HTTP callout to JWT vending service
4. `OnHttpCallResponse()` - Receives JWT from vending service
5. Injects JWT into `Authorization: Bearer <token>` header
6. Resumes the request

**Key Features**:
- Asynchronous HTTP callouts
- Request pause/resume mechanism
- Header manipulation
- Target-specific filtering

### 6. Server WASM Filter

**Purpose**: Validates JWTs and enforces authorization policies on inbound requests

**Technology**: TinyGo (compiled to WASM)

**Execution Context**: Runs in Service B's Envoy sidecar

**Flow**:
1. `OnHttpRequestHeaders()` - Intercepts inbound request
2. Extracts JWT from Authorization header
3. Parses JWT claims (simplified - no signature verification in this version)
4. Extracts principal ID from JWT
5. Extracts asset ID from query parameters
6. Dispatches HTTP callout to SGNL PDP
7. `OnHttpCallResponse()` - Receives PDP decision
8. If Allow: adds headers and resumes request
9. If Deny: sends 403 Forbidden response

**Key Features**:
- JWT extraction and parsing
- PDP integration
- Request blocking on denial
- Custom response generation
- Header injection for downstream services

### 7. Consul Service Mesh

**Purpose**: Provides service mesh infrastructure

**Key Functions**:
- Automatic Envoy sidecar injection
- mTLS between services (x509-based identity)
- Service discovery and DNS
- Load balancing
- Service intentions (mesh-level authorization)

**Configuration Highlights**:
- Connect injection: Enabled via annotations
- Protocol: HTTP (required for WASM filters)
- Extra static clusters: JWT vending service, SGNL PDP
- Service intentions: Allow service-a → service-b

## Request Flow

### Successful Request (Positive Flow)

```
1. External Client
   └─→ HTTP POST /call-service-b
       └─→ Service A Application
           └─→ GET JWT from JWT Vending Service
               └─→ JWT Vending Service returns valid JWT
                   └─→ HTTP GET to Service B (via localhost:8083)
                       └─→ Service A's Envoy Sidecar
                           └─→ Client WASM Filter intercepts
                               └─→ Injects Authorization: Bearer <JWT>
                                   └─→ Consul Connect (mTLS tunnel)
                                       └─→ Service B's Envoy Sidecar
                                           └─→ Server WASM Filter intercepts
                                               └─→ Extracts JWT
                                               └─→ Calls SGNL PDP
                                                   └─→ PDP returns "Allow"
                                                       └─→ Server WASM adds X-PDP-Decision: Allow
                                                           └─→ Service B Application
                                                               └─→ Processes request
                                                               └─→ Returns response with JWT claims
```

### Denied Request (Policy Denial)

```
1. External Client
   └─→ HTTP POST /call-service-b (asset-y)
       └─→ Service A Application
           └─→ GET JWT from JWT Vending Service
               └─→ JWT Vending Service returns valid JWT
                   └─→ HTTP GET to Service B
                       └─→ Service A's Envoy Sidecar
                           └─→ Client WASM Filter injects JWT
                               └─→ Consul Connect (mTLS tunnel)
                                   └─→ Service B's Envoy Sidecar
                                       └─→ Server WASM Filter intercepts
                                           └─→ Extracts JWT
                                           └─→ Calls SGNL PDP
                                               └─→ PDP returns "Deny" (asset-y not allowed)
                                                   └─→ Server WASM returns 403 Forbidden
                                                       └─→ Request blocked, never reaches Service B
```

## Security Architecture

### Multi-Layer Security

1. **Network Layer**: VPC isolation, firewall rules
2. **Transport Layer**: mTLS via Consul Connect (x509 certificates)
3. **Application Layer**: JWT-based authorization via WASM filters
4. **Policy Layer**: Centralized policy decisions via PDP

### Identity Layers

#### Service Mesh Identity (x509)
- Managed by Consul Connect
- Automatic certificate rotation
- Establishes mTLS tunnels
- Coarse-grained (service-level) authorization via intentions

#### Application Identity (JWT)
- Managed by JWT vending service
- Short-lived tokens (5 minutes)
- Fine-grained (asset-level) authorization via PDP
- Orthogonal to x509 identity

### Authorization Model

```
Request Authorization = Mesh Authorization AND Application Authorization

Mesh Authorization:
- Based on: Service identity (x509)
- Evaluated by: Consul service intentions
- Scope: Service-to-service communication
- Example: service-a → service-b: ALLOW

Application Authorization:
- Based on: JWT claims + context (asset, action)
- Evaluated by: SGNL PDP via WASM filter
- Scope: Resource-level access
- Example: service-a → asset-x: ALLOW, service-a → asset-y: DENY
```

Both layers must allow the request for it to succeed.

## WASM Filter Integration

### Why WASM?

1. **Performance**: Near-native execution speed
2. **Isolation**: Sandboxed execution environment
3. **Portability**: Same binary works across platforms
4. **Flexibility**: Can be updated without redeploying Envoy
5. **Language Support**: Write in Go, Rust, C++, etc.

### WASM SDK Used

**Proxy-WASM Go SDK** (`github.com/tetratelabs/proxy-wasm-go-sdk`)
- Provides Go bindings for Envoy WASM ABI
- Context lifecycle management
- HTTP callout support
- Header manipulation

### Compilation

```bash
tinygo build -o filter.wasm -scheduler=none -target=wasi main.go
```

**Flags**:
- `-scheduler=none`: Disable Go scheduler (not needed in WASM)
- `-target=wasi`: WebAssembly System Interface target
- Output: `filter.wasm` (typically 100-500 KB)

### Deployment

WASM modules are deployed via:
1. ConfigMaps containing the WASM binary
2. Consul service mesh configuration referencing the ConfigMap
3. Envoy dynamically loads the WASM module at runtime

## Infrastructure

### GKE Cluster Configuration

- **Kubernetes Version**: Latest (via REGULAR release channel)
- **Node Type**: e2-standard-4 (4 vCPU, 16 GB RAM)
- **Node Count**: 2 (autoscaling 1-5)
- **Networking**: VPC-native with secondary ranges for pods/services
- **Datapath**: Advanced Datapath (eBPF-based)
- **Workload Identity**: Enabled for secure GCP service access

### Network Architecture

```
VPC: 10.0.0.0/16
├── Nodes: 10.0.0.0/16
├── Pods: 10.1.0.0/16 (secondary range)
└── Services: 10.2.0.0/16 (secondary range)
```

### Consul Configuration

- **Server Count**: 1 (use 3-5 for production)
- **Client Mode**: DaemonSet (runs on every node)
- **UI**: Enabled (LoadBalancer)
- **TLS**: Enabled for internal Consul communication
- **Connect**: Enabled with automatic injection

## Scalability Considerations

### Horizontal Scaling

All services can scale independently:
```bash
kubectl scale deployment service-a --replicas=3
kubectl scale deployment service-b --replicas=5
```

### Performance Characteristics

- **WASM Overhead**: ~1-2ms per request (filter execution)
- **JWT Vending**: ~5-10ms per token generation
- **PDP Evaluation**: ~1-5ms per evaluation (in-memory)
- **mTLS Overhead**: ~1-2ms per connection establishment

### Bottlenecks

1. **JWT Vending Service**: Can become bottleneck if not cached
2. **PDP Service**: Should use caching for policy decisions
3. **WASM Execution**: Minimal overhead but scales with filter complexity

## Observability

### Logging

Each component logs to stdout:
- Service logs: Application-level events
- Envoy logs: Proxy-level events, WASM filter output
- Consul logs: Service mesh events

### Metrics

Available metrics:
- Envoy stats: HTTP, upstream, downstream, WASM
- Application metrics: Custom per service
- Consul metrics: Service health, intention checks

### Tracing

Can be integrated with:
- Jaeger
- Zipkin
- Google Cloud Trace

## Production Considerations

### Security Hardening

- [ ] Enable Consul ACLs
- [ ] Implement JWT signature verification in WASM filter
- [ ] Use secrets management (Vault, GKE Secrets)
- [ ] Restrict network policies
- [ ] Enable pod security policies
- [ ] Implement rate limiting

### High Availability

- [ ] Run 3-5 Consul servers
- [ ] Use PodDisruptionBudgets
- [ ] Configure pod anti-affinity
- [ ] Enable horizontal pod autoscaling
- [ ] Use multiple availability zones

### Monitoring

- [ ] Set up Prometheus metrics collection
- [ ] Configure Grafana dashboards
- [ ] Set up alerting (Alertmanager)
- [ ] Enable distributed tracing
- [ ] Log aggregation (ELK, Google Cloud Logging)

### Caching Strategy

- [ ] JWT caching in client WASM filter
- [ ] PDP decision caching in server WASM filter
- [ ] Public key caching for JWT verification

## Future Enhancements

1. **JWT Signature Verification**: Implement full RSA signature verification in server WASM
2. **Token Caching**: Cache valid tokens in client WASM to reduce calls to vending service
3. **Policy Caching**: Cache PDP decisions in server WASM with TTL
4. **Metrics**: Add custom metrics to WASM filters
5. **Dynamic Policies**: Integrate with real SGNL or OPA
6. **Multi-Cluster**: Extend to multi-cluster service mesh
7. **Advanced Policies**: Support ABAC, RBAC, time-based policies
8. **Audit Logging**: Comprehensive audit trail for all authorization decisions