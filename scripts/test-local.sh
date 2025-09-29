#!/bin/bash
# Local testing script for Docker Compose deployment
# Usage: ./scripts/test-local.sh [--positive|--negative|--denied|--all]

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

echo -e "${GREEN}=== WASM PEP Demo Local Test Suite ===${NC}"
echo ""

SERVICE_A_URL="http://localhost:8080"

echo -e "${GREEN}Service A URL: ${SERVICE_A_URL}${NC}"
echo ""

# Wait for services to be ready
echo -e "${YELLOW}Waiting for services to be ready...${NC}"
for i in {1..30}; do
  if curl -s http://localhost:8081/health > /dev/null && \
     curl -s http://localhost:8082/health > /dev/null && \
     curl -s http://localhost:8080/health > /dev/null; then
    echo -e "${GREEN}✓ All services are ready${NC}"
    break
  fi
  if [ $i -eq 30 ]; then
    echo -e "${RED}✗ Services not ready after 30 seconds${NC}"
    exit 1
  fi
  echo -n "."
  sleep 1
done
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
echo "  docker compose logs -f service-a"
echo ""
echo -e "View Service A Envoy logs (client WASM):"
echo "  docker compose logs -f envoy-service-a"
echo ""
echo -e "View Service B logs:"
echo "  docker compose logs -f service-b"
echo ""
echo -e "View Service B Envoy logs (server WASM):"
echo "  docker compose logs -f envoy-service-b"
echo ""
echo -e "View JWT Vending Service logs:"
echo "  docker compose logs -f jwt-vending-service"
echo ""
echo -e "View SGNL PDP Service logs:"
echo "  docker compose logs -f sgnl-pdp-service"
echo ""
echo -e "Check Envoy admin interfaces:"
echo "  Service A Envoy: http://localhost:9901"
echo "  Service B Envoy: http://localhost:9902"
echo ""
echo -e "Check PDP policies:"
echo "  curl http://localhost:8082/policies | jq"