# Simplified Local Testing (Without WASM)

Due to WASM compatibility issues between proxy-wasm-go-sdk v0.24.0 and Envoy v1.28, we can test the core architecture without the WASM filters. The services handle JWT operations directly.

## Quick Test

Test the services directly (without Envoy proxies):

```bash
# Test infrastructure services
curl http://localhost:8081/health  # JWT vending
curl http://localhost:8082/health  # PDP

# Test JWT generation
curl -X POST http://localhost:8081/token/valid \
  -H "Content-Type: application/json" \
  -d '{"service_id":"service-a"}' | jq

# Test PDP evaluation
curl -X POST http://localhost:8082/access/v2/evaluations \
  -H "Content-Type: application/json" \
  -d '{
    "principal": {"id": "service-a"},
    "queries": [{"assetId": "asset-x", "action": "call"}]
  }' | jq

# Check PDP policies
curl http://localhost:8082/policies | jq
```

## Testing Service-to-Service (Without WASM)

The services can be tested directly by calling them and manually providing JWTs:

```bash
# Get a valid JWT
TOKEN=$(curl -s -X POST http://localhost:8081/token/valid \
  -H "Content-Type: application/json" \
  -d '{"service_id":"service-a"}' | jq -r '.token')

echo "Token: $TOKEN"

# Call Service B directly with the JWT
curl -X GET "http://localhost:8083/process?asset=asset-x" \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Service-ID: service-a" | jq
```

## WASM Compatibility Issues

The WASM modules built with proxy-wasm-go-sdk v0.24.0 are not compatible with Envoy v1.28 due to:
- `restricted_callback` error during WASM initialization
- SDK/Envoy ABI version mismatch

### Solutions for WASM Testing

1. **Use older Envoy version** - Try `envoyproxy/envoy:v1.25-latest` or `v1.26-latest`
2. **Use different WASM SDK version** - Try proxy-wasm-go-sdk v0.22.0 or v0.23.0
3. **Build with specific ABI version** - Ensure WASM ABI version matches Envoy
4. **Deploy to GKE with Consul** - Consul's Envoy version may be more compatible

## What Works

✅ All Go services running correctly
✅ JWT vending service generates valid/invalid tokens
✅ PDP evaluates policies correctly (allow/deny)
✅ Services can call each other with manual JWT passing
✅ Docker networking between services

## What Doesn't Work

❌ Envoy WASM filters don't load due to compatibility
❌ Automatic JWT injection by client filter
❌ Automatic JWT validation by server filter
❌ Automatic PDP calls from server filter

## Next Steps

For full WASM testing, deploy to GKE where Consul provides a tested Envoy version with WASM support.