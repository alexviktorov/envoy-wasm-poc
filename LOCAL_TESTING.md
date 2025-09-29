# Local Testing Guide

This guide explains how to test the WASM PEP demo locally using Docker Compose, without deploying to GCP/GKE.

## Architecture

The local setup uses:
- **Docker Compose** to orchestrate all services
- **Envoy proxies** as sidecars for Service A and Service B
- **WASM filters** loaded directly into Envoy from local filesystem
- **Docker networking** to simulate service mesh connectivity

```
┌─────────────────────────────────────────────────────────────────┐
│                      Docker Compose Network                      │
│                                                                   │
│  ┌──────────────┐          ┌──────────────┐                    │
│  │ JWT Vending  │          │  SGNL PDP    │                    │
│  │   Service    │          │   Service    │                    │
│  │   :8081      │          │   :8082      │                    │
│  └──────────────┘          └──────────────┘                    │
│         ▲                          ▲                            │
│         │                          │                            │
│  ┌──────┴──────────────────────────┴─────────────────────────┐│
│  │                                                             ││
│  │  ┌─────────────────────────────────────────────────────┐  ││
│  │  │  Envoy Service A (client WASM)                      │  ││
│  │  │  :8080 (external)  :10000 (outbound)               │  ││
│  │  │  ┌──────────────┐                                   │  ││
│  │  │  │  Service A   │                                   │  ││
│  │  │  │  :8080       │                                   │  ││
│  │  │  └──────────────┘                                   │  ││
│  │  └─────────────────────────────────────────────────────┘  ││
│  │                        │                                   ││
│  │                        ▼  HTTP with JWT                   ││
│  │  ┌─────────────────────────────────────────────────────┐  ││
│  │  │  Envoy Service B (server WASM)                      │  ││
│  │  │  :10001 (inbound)  :8083 (external)                │  ││
│  │  │  ┌──────────────┐                                   │  ││
│  │  │  │  Service B   │                                   │  ││
│  │  │  │  :8083       │                                   │  ││
│  │  │  └──────────────┘                                   │  ││
│  │  └─────────────────────────────────────────────────────┘  ││
│  │                                                             ││
│  └─────────────────────────────────────────────────────────────┘│
│                                                                   │
└─────────────────────────────────────────────────────────────────┘
```

## Prerequisites

- Docker Desktop or Docker Engine running
- Docker Compose
- `curl` and `jq` (for testing)

## Quick Start

### 1. Build WASM Modules

```bash
./scripts/build.sh
```

This will:
- Build `client-filter.wasm` (768K) - for Service A
- Build `server-filter.wasm` (816K) - for Service B
- Skip Docker image builds if Docker isn't running (that's fine)

### 2. Start All Services

```bash
docker compose up --build -d
```

This will:
- Build Docker images for all 4 Go services
- Start all containers in detached mode
- Set up networking between containers

### 3. Verify Services Are Running

```bash
docker compose ps
```

Expected output:
```
NAME                        STATUS    PORTS
envoy-service-a            running   0.0.0.0:8080->8080/tcp, ...
envoy-service-b            running   0.0.0.0:8083->8083/tcp, ...
jwt-vending-service        running   0.0.0.0:8081->8081/tcp
service-a                  running
service-b                  running
sgnl-pdp-service           running   0.0.0.0:8082->8082/tcp
```

### 4. Run Tests

```bash
# Run all tests
./scripts/test-local.sh --all

# Or run specific tests
./scripts/test-local.sh --positive
./scripts/test-local.sh --negative
./scripts/test-local.sh --denied
```

## Manual Testing

### Test Infrastructure Services

```bash
# Test JWT Vending Service
curl -X POST http://localhost:8081/token/valid \
  -H "Content-Type: application/json" \
  -d '{"service_id":"service-a"}' | jq

# Test SGNL PDP Service
curl http://localhost:8082/policies | jq

curl -X POST http://localhost:8082/access/v2/evaluations \
  -H "Content-Type: application/json" \
  -d '{
    "principal": {"id": "service-a"},
    "queries": [{"assetId": "asset-x", "action": "call"}]
  }' | jq
```

### Test End-to-End Flows

#### Positive Flow (Allowed)

```bash
curl -X POST http://localhost:8080/call-service-b \
  -H "Content-Type: application/json" \
  -d '{
    "asset": "asset-x",
    "use_valid_token": true
  }' | jq
```

Expected: HTTP 200 with success=true

#### Negative Flow (Invalid JWT)

```bash
curl -X POST http://localhost:8080/call-service-b \
  -H "Content-Type: application/json" \
  -d '{
    "asset": "asset-x",
    "use_valid_token": false
  }' | jq
```

Expected: HTTP 200 with success=false, error about invalid JWT

#### Negative Flow (Policy Denial)

```bash
curl -X POST http://localhost:8080/call-service-b \
  -H "Content-Type: application/json" \
  -d '{
    "asset": "asset-y",
    "use_valid_token": true
  }' | jq
```

Expected: HTTP 200 with success=false, error about policy denial

## Debugging

### View Logs

```bash
# All logs
docker compose logs -f

# Specific service
docker compose logs -f service-a
docker compose logs -f envoy-service-a
docker compose logs -f envoy-service-b
docker compose logs -f jwt-vending-service
docker compose logs -f sgnl-pdp-service
```

### View Envoy Admin Interface

Service A Envoy:
```bash
open http://localhost:9901
```

Service B Envoy:
```bash
open http://localhost:9902
```

Useful endpoints:
- `/config_dump` - Full Envoy configuration including WASM filters
- `/stats` - Metrics and statistics
- `/logging` - Change log levels
- `/clusters` - Upstream cluster health

### Check WASM Filter Status

```bash
# Service A Envoy (client filter)
curl -s http://localhost:9901/config_dump | jq '.configs[] | select(.["@type"] | contains("Bootstrap")) | .bootstrap.static_resources.listeners[] | select(.name == "listener_outbound")'

# Service B Envoy (server filter)
curl -s http://localhost:9902/config_dump | jq '.configs[] | select(.["@type"] | contains("Bootstrap")) | .bootstrap.static_resources.listeners[] | select(.name == "listener_inbound")'
```

### Inspect WASM Module Loading

```bash
# Check if WASM modules are loaded
docker compose logs envoy-service-a | grep -i wasm
docker compose logs envoy-service-b | grep -i wasm
```

Look for messages like:
- `wasm log: [Client WASM] ...`
- `wasm log: [Server WASM] ...`

## Troubleshooting

### Issue: WASM Modules Not Loading

**Symptoms**: No WASM-related logs, requests pass through without JWT validation

**Solutions**:
1. Verify WASM files exist:
   ```bash
   ls -lh wasm/*/*.wasm
   ```

2. Check Envoy can read WASM files:
   ```bash
   docker compose exec envoy-service-a ls -l /etc/envoy/
   docker compose exec envoy-service-b ls -l /etc/envoy/
   ```

3. Check Envoy logs for errors:
   ```bash
   docker compose logs envoy-service-a | grep -i error
   docker compose logs envoy-service-b | grep -i error
   ```

### Issue: Services Not Communicating

**Symptoms**: Connection refused or timeout errors

**Solutions**:
1. Check all containers are running:
   ```bash
   docker compose ps
   ```

2. Verify network connectivity:
   ```bash
   docker compose exec service-a ping -c 3 jwt-vending-service
   docker compose exec envoy-service-a ping -c 3 envoy-service-b
   ```

3. Check service health:
   ```bash
   curl http://localhost:8081/health
   curl http://localhost:8082/health
   curl http://localhost:8080/health
   ```

### Issue: JWT Validation Always Fails

**Symptoms**: All requests return 401

**Solutions**:
1. Test JWT vending service directly:
   ```bash
   curl -X POST http://localhost:8081/token/valid \
     -H "Content-Type: application/json" \
     -d '{"service_id":"service-a"}' | jq .token
   ```

2. Check server WASM filter logs:
   ```bash
   docker compose logs envoy-service-b | grep "Server WASM"
   ```

3. Verify the server filter is receiving JWTs:
   ```bash
   docker compose logs envoy-service-b | grep -i authorization
   ```

### Issue: PDP Always Denies

**Symptoms**: All requests return 403

**Solutions**:
1. Check PDP policies:
   ```bash
   curl http://localhost:8082/policies | jq
   ```

2. Test PDP directly:
   ```bash
   curl -X POST http://localhost:8082/access/v2/evaluations \
     -H "Content-Type: application/json" \
     -d '{
       "principal": {"id": "service-a"},
       "queries": [{"assetId": "asset-x", "action": "call"}]
     }' | jq
   ```

3. Check server WASM filter logs:
   ```bash
   docker compose logs envoy-service-b | grep "PDP"
   ```

## Cleanup

Stop and remove all containers:
```bash
docker compose down
```

Remove volumes and networks:
```bash
docker compose down -v
```

Remove built images:
```bash
docker compose down --rmi all
```

## Differences from GKE Deployment

| Aspect | Local (Docker Compose) | GKE (Production) |
|--------|------------------------|------------------|
| Service Mesh | Standalone Envoy | Consul Connect |
| mTLS | Not configured | Automatic via Consul |
| Service Discovery | Docker DNS | Consul DNS |
| Certificate Management | None | Consul CA |
| Scaling | Manual | Kubernetes HPA |
| Load Balancing | Docker | GCP Load Balancer |
| Secrets Management | Environment vars | GKE Secrets/Vault |
| Monitoring | Docker logs | Prometheus/Grafana |

## Next Steps

After validating locally:

1. **Deploy to GKE**: Use Terraform and scripts in the main README
2. **Add mTLS**: Configure mutual TLS between services
3. **Implement JWT Verification**: Add RSA signature verification in server WASM
4. **Add Caching**: Cache JWTs in client filter, PDP decisions in server filter
5. **Observability**: Add Prometheus metrics and distributed tracing

## Tips

- Use `docker compose logs -f --tail=100` to follow recent logs
- The Envoy admin interfaces (9901, 9902) are invaluable for debugging
- Check WASM filter logs by grepping for "[Client WASM]" and "[Server WASM]"
- Use `jq` to pretty-print JSON responses
- Test infrastructure services independently before testing end-to-end