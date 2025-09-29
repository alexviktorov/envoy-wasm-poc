# Service Mesh PEP Demo - Testing Guide

This guide provides comprehensive instructions for testing the Policy Enforcement Point (PEP) demo with Rust WASM filters.

## Architecture Overview

The demo implements JWT-based authorization with Policy Decision Point (PDP) validation:

1. **Service A** makes requests to Service B
2. **Client WASM Filter** (Rust) intercepts outbound requests from Service A and injects JWTs
3. **Server WASM Filter** (Rust) intercepts inbound requests to Service B, validates JWTs, and checks with PDP
4. **SGNL PDP** enforces fine-grained access policies

## Prerequisites

- Docker Desktop or Docker Engine with Docker Compose v2+
- Rust toolchain (`curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh`)
- Go 1.21+ (for Go services)
- `curl` and `jq` (for testing)

## Quick Start

### 1. Build Everything

```bash
# Build Rust WASM modules and Go services
./scripts/build.sh
```

This compiles:
- `client-filter-rust.wasm` (269KB) - JWT injection filter
- `server-filter-rust.wasm` (281KB) - JWT validation + PDP filter
- All Go services (jwt-vending, sgnl-pdp, service-a, service-b)

### 2. Start the Demo

```bash
# Start all services
docker compose up -d

# Check services are running
docker compose ps
```

### 3. Run Automated Tests

```bash
# Run all test scenarios
./scripts/test-local.sh --all

# Or run individual scenarios:
./scripts/test-local.sh --positive   # Valid JWT, allowed resource
./scripts/test-local.sh --denied     # Valid JWT, denied by policy
```

## Manual Testing

### Test 1: Positive Flow (ALLOW)

**Scenario**: Service A calls Service B with valid JWT for `asset-x` (allowed by policy)

```bash
curl -X POST http://localhost:8080/call-service-b \
  -H "Content-Type: application/json" \
  -d '{"asset":"asset-x","use_valid_token":true}' | jq
```

**Expected Response**:
```json
{
  "success": true,
  "response_from_b": {
    "asset": "asset-x",
    "authorized": true,
    "caller_id": "service-a",
    "jwt_claims": {
      "aud": ["service-mesh"],
      "exp": 1759180622,
      "iat": 1759180322,
      "iss": "jwt-vending-service",
      "nbf": 1759180322,
      "sub": "service-a"
    },
    "message": "Call received from service-a"
  }
}
```

### Test 2: Policy Denial (DENY)

**Scenario**: Service A calls Service B with valid JWT for `asset-y` (denied by policy)

```bash
curl -X POST http://localhost:8080/call-service-b \
  -H "Content-Type: application/json" \
  -d '{"asset":"asset-y","use_valid_token":true}' | jq
```

**Expected Response**:
```json
{
  "success": false,
  "response_from_b": {
    "error": "Access denied by policy",
    "pdp_response": {
      "decision": "Deny",
      "reason": "Service service-a is not allowed to access asset-y"
    }
  },
  "error": "Failed to call service B: service B returned status 403"
}
```

## Viewing WASM Filter Logs

### Client Filter (Service A Envoy)

```bash
docker compose logs -f envoy-service-a | grep "Client WASM Rust"
```

Expected output:
```
[Client WASM Rust] VM started
[Client WASM Rust] Intercepted request to service-b:8083, fetching JWT token
[Client WASM Rust] Dispatched HTTP call to JWT vending service (call_id: 1)
[Client WASM Rust] Received JWT response (headers: 7, body: 575)
[Client WASM Rust] Successfully obtained JWT token (length: 542)
[Client WASM Rust] Injected JWT token into Authorization header
```

### Server Filter (Service B Envoy)

```bash
docker compose logs -f envoy-service-b | grep "Server WASM Rust"
```

Expected output:
```
[Server WASM Rust] VM started
[Server WASM Rust] Intercepted inbound request: GET /process?asset=asset-x
[Server WASM Rust] JWT token extracted (length: 542)
[Server WASM Rust] Calling PDP: principal=service-a, asset=asset-x
[Server WASM Rust] Dispatched HTTP call to PDP (call_id: 4)
[Server WASM Rust] Received PDP response (body size: 95)
[Server WASM Rust] PDP decision: Allow (Service service-a is allowed to access asset-x)
[Server WASM Rust] Access granted, resuming request
```

## Debugging

### Check Service Health

```bash
# JWT Vending Service
curl http://localhost:8081/health

# SGNL PDP Service
curl http://localhost:8082/health

# Service A
curl http://localhost:8080/health
```

### Check PDP Policies

```bash
curl http://localhost:8082/policies | jq
```

Expected output:
```json
{
  "policies": {
    "service-a": {
      "asset-x": true,
      "asset-y": false
    }
  }
}
```

### Envoy Admin Interfaces

- **Service A Envoy**: http://localhost:9901
  - Stats: http://localhost:9901/stats
  - Config dump: http://localhost:9901/config_dump

- **Service B Envoy**: http://localhost:9902
  - Stats: http://localhost:9902/stats
  - Config dump: http://localhost:9902/config_dump

### View Service Logs

```bash
# All services
docker compose logs -f

# Specific services
docker compose logs -f service-a
docker compose logs -f service-b
docker compose logs -f jwt-vending-service
docker compose logs -f sgnl-pdp-service
docker compose logs -f envoy-service-a
docker compose logs -f envoy-service-b
```

## Testing Edge Cases

### Test Direct Access (Bypassing Envoy)

Try accessing Service B directly without going through the Envoy proxy:

```bash
# This should fail because there's no JWT
curl http://localhost:8083/health
```

### Test Invalid JWT

The server filter validates JWTs with the PDP. You can test invalid signatures by directly calling the server filter's endpoint with a malformed token.

## Architecture Verification

### Verify WASM Filters are Loaded

```bash
# Check Service A Envoy config
curl -s http://localhost:9901/config_dump | jq '.configs[] | select(.["@type"] | contains("HttpConnectionManager"))'

# Check Service B Envoy config
curl -s http://localhost:9902/config_dump | jq '.configs[] | select(.["@type"] | contains("HttpConnectionManager"))'
```

Look for `envoy.filters.http.wasm` entries with the Rust WASM modules.

## Cleanup

```bash
# Stop all services
docker compose down

# Stop and remove volumes
docker compose down -v

# Remove built images
docker compose down --rmi local
```

## Common Issues

### Issue: Services not starting

**Solution**: Check logs for individual services
```bash
docker compose logs jwt-vending-service
docker compose logs sgnl-pdp-service
```

### Issue: WASM filters not loading

**Solution**: Check Envoy logs for WASM errors
```bash
docker compose logs envoy-service-a | grep -i wasm
docker compose logs envoy-service-b | grep -i wasm
```

### Issue: Connection refused errors

**Solution**: Wait for services to be healthy
```bash
# Wait for all health checks to pass
watch docker compose ps
```

## Performance Testing

### Load Test Service A

```bash
# Install Apache Bench
# macOS: brew install ab
# Linux: sudo apt-get install apache2-utils

# Run load test
ab -n 1000 -c 10 -p payload.json -T application/json \
  http://localhost:8080/call-service-b
```

**payload.json**:
```json
{"asset":"asset-x","use_valid_token":true}
```

### Monitor Resource Usage

```bash
# Container stats
docker stats

# Envoy memory usage
docker compose exec envoy-service-a cat /proc/1/status | grep VmRSS

# WASM module size
ls -lh wasm/client-filter-rust/target/wasm32-wasip1/release/client_filter_rust.wasm
ls -lh wasm/server-filter-rust/target/wasm32-wasip1/release/server_filter_rust.wasm
```

## Next Steps

1. **Deploy to Kubernetes**: See main README for GKE deployment instructions
2. **Add More Policies**: Modify `services/sgnl-pdp/main.go` to add more access rules
3. **Customize WASM Filters**: Edit `wasm/client-filter-rust/src/lib.rs` or `wasm/server-filter-rust/src/lib.rs`
4. **Integrate with Real SGNL**: Replace mock PDP with actual SGNL API

## Success Criteria

✅ **Demo is working correctly if**:
- All 6 containers are running (`docker compose ps`)
- Health checks pass for all services
- Positive test returns `"success": true` with `"authorized": true`
- Policy denial test returns `"success": false` with PDP denial reason
- WASM filter logs show JWT injection and validation
- No "restricted_callback" or other WASM errors in Envoy logs