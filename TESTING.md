# Testing Guide

This document provides detailed testing procedures for the WASM PEP Demo.

## Prerequisites

- GKE cluster deployed and configured
- All services deployed via `./scripts/deploy.sh`
- `kubectl` configured to access the cluster
- `curl` and `jq` installed locally

## Overview of Test Scenarios

The demo supports three primary test scenarios:

1. **Positive Flow**: Valid JWT + Allowed Asset → Success
2. **Negative Flow (Invalid JWT)**: Invalid JWT → Rejection
3. **Negative Flow (Policy Denial)**: Valid JWT + Denied Asset → Rejection

## Automated Testing

Use the provided test script to run all scenarios:

```bash
# Run all tests
./scripts/test.sh --all

# Run specific test
./scripts/test.sh --positive
./scripts/test.sh --negative
./scripts/test.sh --denied
```

## Manual Testing

### Setup

Get the Service A endpoint:

```bash
# If using LoadBalancer
export SERVICE_A_IP=$(kubectl get svc service-a -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
export SERVICE_A_URL="http://${SERVICE_A_IP}:8080"

# OR use port-forward
kubectl port-forward svc/service-a 8080:8080 &
export SERVICE_A_URL="http://localhost:8080"
```

### Test 1: Positive Flow (Authorized Access)

**Scenario**: Service A → Service B with valid JWT for asset-x (allowed)

```bash
curl -X POST ${SERVICE_A_URL}/call-service-b \
  -H "Content-Type: application/json" \
  -d '{"asset": "asset-x", "use_valid_token": true}' | jq
```

**Expected Response** (HTTP 200):
```json
{
  "success": true,
  "response_from_b": {
    "message": "Call received from service-a",
    "jwt_claims": {
      "aud": "service-mesh",
      "exp": 1234567890,
      "iat": 1234567890,
      "iss": "jwt-vending-service",
      "sub": "service-a"
    },
    "authorized": true,
    "asset": "asset-x",
    "caller_id": "service-a"
  }
}
```

**What happens**:
1. Service A calls JWT vending service → receives valid JWT
2. Service A's Envoy sidecar (client WASM) intercepts request
3. Client WASM injects JWT into Authorization header
4. Request flows through Consul mesh to Service B
5. Service B's Envoy sidecar (server WASM) extracts JWT
6. Server WASM calls SGNL PDP → receives "Allow" decision
7. Request forwarded to Service B application
8. Service B processes request and returns response

### Test 2: Negative Flow - Invalid JWT

**Scenario**: Service A → Service B with invalid JWT

```bash
curl -X POST ${SERVICE_A_URL}/call-service-b \
  -H "Content-Type: application/json" \
  -d '{"asset": "asset-x", "use_valid_token": false}' | jq
```

**Expected Response** (HTTP 200 with error):
```json
{
  "success": false,
  "response_from": {
    "status": 401,
    "error": "Invalid JWT signature"
  },
  "error": "Failed to call service B: service B returned status 401"
}
```

**What happens**:
1. Service A calls JWT vending service with `/token/invalid` → receives JWT signed with wrong key
2. Client WASM injects invalid JWT
3. Server WASM extracts JWT and attempts validation
4. Signature verification fails
5. Server WASM returns 401 Unauthorized

### Test 3: Negative Flow - Policy Denial

**Scenario**: Service A → Service B with valid JWT for asset-y (denied)

```bash
curl -X POST ${SERVICE_A_URL}/call-service-b \
  -H "Content-Type: application/json" \
  -d '{"asset": "asset-y", "use_valid_token": true}' | jq
```

**Expected Response** (HTTP 200 with error):
```json
{
  "success": false,
  "response_from": {
    "error": "Access denied by policy",
    "pdp_response": {
      "decision": "Deny",
      "reason": "Service service-a is not allowed to access asset-y"
    }
  },
  "error": "Failed to call service B: service B returned status 403"
}
```

**What happens**:
1. Service A gets valid JWT
2. Client WASM injects JWT
3. Server WASM extracts JWT and calls PDP
4. PDP evaluates: service-a + asset-y → DENY
5. Server WASM returns 403 Forbidden
6. Request never reaches Service B application

## Debugging

### View Service Logs

```bash
# Service A application logs
kubectl logs -l app=service-a -c service-a --tail=50 -f

# Service A Envoy sidecar (client WASM)
kubectl logs -l app=service-a -c consul-connect-envoy-sidecar --tail=50 -f

# Service B application logs
kubectl logs -l app=service-b -c service-b --tail=50 -f

# Service B Envoy sidecar (server WASM)
kubectl logs -l app=service-b -c consul-connect-envoy-sidecar --tail=50 -f

# JWT Vending Service
kubectl logs -l app=jwt-vending-service --tail=50 -f

# SGNL PDP Service
kubectl logs -l app=sgnl-pdp-service --tail=50 -f
```

### Inspect Services Directly

Test infrastructure services independently:

```bash
# Test JWT Vending Service
kubectl port-forward svc/jwt-vending-service 8081:8081 &

curl -X POST http://localhost:8081/token/valid \
  -H "Content-Type: application/json" \
  -d '{"service_id":"service-a"}' | jq

curl http://localhost:8081/public-key

# Test SGNL PDP Service
kubectl port-forward svc/sgnl-pdp-service 8082:8082 &

curl http://localhost:8082/policies | jq

curl -X POST http://localhost:8082/access/v2/evaluations \
  -H "Content-Type: application/json" \
  -d '{
    "principal": {"id": "service-a"},
    "queries": [{"assetId": "asset-x", "action": "call"}]
  }' | jq
```

### Check Consul Service Mesh

```bash
# View Consul UI
kubectl port-forward -n consul svc/consul-ui 8500:80 &
open http://localhost:8500

# Check service registration
kubectl exec -it consul-server-0 -n consul -- consul catalog services

# Check service intentions
kubectl get serviceintentions

# View Envoy configuration
kubectl exec -it <service-a-pod> -c consul-connect-envoy-sidecar -- \
  curl localhost:19000/config_dump | jq
```

### Common Issues

#### 1. WASM Module Not Loading

**Symptom**: Requests succeed without JWT validation

**Debug**:
```bash
# Check Envoy config for WASM filter
kubectl exec -it <pod> -c consul-connect-envoy-sidecar -- \
  curl localhost:19000/config_dump | grep -A 20 wasm

# Check Envoy logs for WASM errors
kubectl logs <pod> -c consul-connect-envoy-sidecar | grep -i wasm
```

**Solution**:
- Verify WASM files are built: `ls wasm/*/*.wasm`
- Ensure ConfigMaps are updated with WASM binary
- Check Consul service mesh configuration

#### 2. JWT Validation Always Fails

**Symptom**: All requests return 401

**Debug**:
```bash
# Verify JWT vending service is healthy
kubectl get pods -l app=jwt-vending-service

# Test JWT generation directly
kubectl port-forward svc/jwt-vending-service 8081:8081
curl -X POST http://localhost:8081/token/valid \
  -d '{"service_id":"service-a"}' | jq
```

**Solution**:
- Ensure JWT vending service is running
- Check network connectivity between services
- Verify Envoy clusters are configured correctly

#### 3. PDP Always Denies

**Symptom**: All requests return 403

**Debug**:
```bash
# Check PDP logs
kubectl logs -l app=sgnl-pdp-service

# Verify policy configuration
kubectl port-forward svc/sgnl-pdp-service 8082:8082
curl http://localhost:8082/policies | jq
```

**Solution**:
- Verify PDP service is running
- Check policy rules in `services/sgnl-pdp/main.go`
- Ensure correct principal ID and asset ID in request

#### 4. Service Mesh Connectivity Issues

**Symptom**: Requests timeout or fail to connect

**Debug**:
```bash
# Check Consul service registration
kubectl exec -it consul-server-0 -n consul -- \
  consul catalog services

# Verify service intentions
kubectl get serviceintentions

# Check pod status
kubectl get pods -o wide
```

**Solution**:
- Ensure Consul Connect injection is enabled
- Verify service intentions allow traffic
- Check network policies

## Performance Testing

### Load Testing with Apache Bench

```bash
# Test positive flow under load
ab -n 1000 -c 10 -T 'application/json' -p - \
  ${SERVICE_A_URL}/call-service-b <<EOF
{"asset": "asset-x", "use_valid_token": true}
EOF
```

### Observing Metrics

```bash
# Envoy stats
kubectl exec -it <pod> -c consul-connect-envoy-sidecar -- \
  curl localhost:19000/stats/prometheus

# Filter for WASM-related metrics
kubectl exec -it <pod> -c consul-connect-envoy-sidecar -- \
  curl localhost:19000/stats | grep wasm
```

## End-to-End Validation Checklist

- [ ] Consul server is running and healthy
- [ ] All services have Envoy sidecars injected
- [ ] JWT vending service responds to token requests
- [ ] SGNL PDP service responds to evaluation requests
- [ ] Service A can call JWT vending service
- [ ] Positive flow test passes (asset-x with valid token)
- [ ] Negative flow test passes (invalid token rejected)
- [ ] Policy denial test passes (asset-y denied)
- [ ] Envoy logs show WASM filter activity
- [ ] Service B logs show decoded JWT claims

## Additional Resources

- [Consul Service Mesh Documentation](https://www.consul.io/docs/connect)
- [Envoy WASM Documentation](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/wasm_filter)
- [TinyGo WASM Guide](https://tinygo.org/docs/guides/webassembly/)
- [Proxy-WASM Go SDK](https://github.com/tetratelabs/proxy-wasm-go-sdk)