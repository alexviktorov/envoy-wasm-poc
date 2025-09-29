#!/bin/bash
# Test script for WASM PEP Demo
# Usage: ./scripts/test.sh [--positive|--negative|--denied|--all]

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test type
TEST_TYPE="all"

# Parse arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --positive)
      TEST_TYPE="positive"
      shift
      ;;
    --negative)
      TEST_TYPE="negative"
      shift
      ;;
    --denied)
      TEST_TYPE="denied"
      shift
      ;;
    --all)
      TEST_TYPE="all"
      shift
      ;;
    *)
      echo "Unknown option: $1"
      echo "Usage: $0 [--positive|--negative|--denied|--all]"
      exit 1
      ;;
  esac
done

echo -e "${GREEN}=== WASM PEP Demo Test Suite ===${NC}"
echo ""

# Get Service A endpoint
echo -e "${YELLOW}Getting Service A endpoint...${NC}"

# Try LoadBalancer IP first
SERVICE_A_IP=$(kubectl get svc service-a -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || echo "")

if [ -z "$SERVICE_A_IP" ]; then
  echo -e "${YELLOW}LoadBalancer IP not available, using port-forward...${NC}"

  # Kill any existing port-forward
  pkill -f "port-forward.*service-a" 2>/dev/null || true
  sleep 2

  # Start port-forward in background
  kubectl port-forward svc/service-a 8080:8080 > /dev/null 2>&1 &
  PORT_FORWARD_PID=$!

  # Wait for port-forward to be ready
  sleep 3

  SERVICE_A_URL="http://localhost:8080"

  # Cleanup on exit
  trap "kill $PORT_FORWARD_PID 2>/dev/null || true" EXIT
else
  SERVICE_A_URL="http://${SERVICE_A_IP}:8080"
fi

echo -e "${GREEN}Service A URL: ${SERVICE_A_URL}${NC}"
echo ""

# Function to run a test
run_test() {
  local test_name=$1
  local asset=$2
  local use_valid=$3
  local expected_success=$4
  local description=$5

  echo -e "${BLUE}=== Test: ${test_name} ===${NC}"
  echo -e "${YELLOW}Description: ${description}${NC}"
  echo ""

  # Prepare request
  REQUEST_BODY="{\"asset\":\"${asset}\",\"use_valid_token\":${use_valid}}"
  echo -e "${YELLOW}Request:${NC}"
  echo "  POST ${SERVICE_A_URL}/call-service-b"
  echo "  Body: ${REQUEST_BODY}"
  echo ""

  # Make request
  echo -e "${YELLOW}Response:${NC}"
  RESPONSE=$(curl -s -X POST "${SERVICE_A_URL}/call-service-b" \
    -H "Content-Type: application/json" \
    -d "${REQUEST_BODY}" || echo '{"error":"Connection failed"}')

  echo "$RESPONSE" | jq '.' 2>/dev/null || echo "$RESPONSE"
  echo ""

  # Validate response
  SUCCESS=$(echo "$RESPONSE" | jq -r '.success' 2>/dev/null || echo "false")

  if [ "$SUCCESS" = "$expected_success" ]; then
    echo -e "${GREEN}✓ Test PASSED${NC}"
  else
    echo -e "${RED}✗ Test FAILED${NC}"
    echo -e "  Expected success=${expected_success}, got success=${SUCCESS}"
  fi

  echo ""
  echo "---"
  echo ""
}

# Run tests based on TEST_TYPE

if [ "$TEST_TYPE" = "positive" ] || [ "$TEST_TYPE" = "all" ]; then
  run_test \
    "Positive Flow" \
    "asset-x" \
    "true" \
    "true" \
    "Service A calls Service B with valid JWT for asset-x (allowed by policy)"
fi

if [ "$TEST_TYPE" = "negative" ] || [ "$TEST_TYPE" = "all" ]; then
  run_test \
    "Negative Flow - Invalid JWT" \
    "asset-x" \
    "false" \
    "false" \
    "Service A calls Service B with invalid JWT (should be rejected)"
fi

if [ "$TEST_TYPE" = "denied" ] || [ "$TEST_TYPE" = "all" ]; then
  run_test \
    "Negative Flow - Policy Denial" \
    "asset-y" \
    "true" \
    "false" \
    "Service A calls Service B with valid JWT for asset-y (denied by policy)"
fi

# Summary
echo -e "${GREEN}=== Test Suite Complete ===${NC}"
echo ""
echo -e "${YELLOW}Additional debugging commands:${NC}"
echo ""
echo -e "View Service A logs:"
echo "  kubectl logs -l app=service-a -c service-a --tail=50 -f"
echo ""
echo -e "View Service A Envoy sidecar logs (client WASM):"
echo "  kubectl logs -l app=service-a -c consul-connect-envoy-sidecar --tail=50 -f"
echo ""
echo -e "View Service B logs:"
echo "  kubectl logs -l app=service-b -c service-b --tail=50 -f"
echo ""
echo -e "View Service B Envoy sidecar logs (server WASM):"
echo "  kubectl logs -l app=service-b -c consul-connect-envoy-sidecar --tail=50 -f"
echo ""
echo -e "View JWT Vending Service logs:"
echo "  kubectl logs -l app=jwt-vending-service --tail=50 -f"
echo ""
echo -e "View SGNL PDP Service logs:"
echo "  kubectl logs -l app=sgnl-pdp-service --tail=50 -f"
echo ""
echo -e "Check PDP policies:"
echo "  kubectl port-forward svc/sgnl-pdp-service 8082:8082"
echo "  curl http://localhost:8082/policies | jq"