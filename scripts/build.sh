#!/bin/bash
# Build script for all services and WASM modules
# Usage: ./scripts/build.sh [--push] [--project PROJECT_ID] [--region REGION]

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Default values
PUSH=false
PROJECT_ID="${GCP_PROJECT:-}"
REGION="${GCP_REGION:-us-central1}"

# Parse arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --push)
      PUSH=true
      shift
      ;;
    --project)
      PROJECT_ID="$2"
      shift 2
      ;;
    --region)
      REGION="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1"
      echo "Usage: $0 [--push] [--project PROJECT_ID] [--region REGION]"
      exit 1
      ;;
  esac
done

# Check if PROJECT_ID is set when --push is used
if [ "$PUSH" = true ] && [ -z "$PROJECT_ID" ]; then
  echo -e "${RED}Error: --project must be specified when using --push${NC}"
  exit 1
fi

# Determine registry URL
if [ "$PUSH" = true ]; then
  REGISTRY="${REGION}-docker.pkg.dev/${PROJECT_ID}/wasm-pep-demo"
  echo -e "${YELLOW}Will push images to: ${REGISTRY}${NC}"
else
  REGISTRY="gcr.io/PROJECT_ID"
  echo -e "${YELLOW}Building images locally (use --push to push to registry)${NC}"
fi

echo -e "${GREEN}=== Building WASM PEP Demo ===${NC}"
echo ""

# Function to build a Go service
build_service() {
  local service_name=$1
  local service_path=$2

  echo -e "${YELLOW}Building ${service_name}...${NC}"

  cd "$service_path"

  # Build Docker image
  docker build -t "${service_name}:latest" .
  docker tag "${service_name}:latest" "${REGISTRY}/${service_name}:latest"

  if [ "$PUSH" = true ]; then
    echo -e "${YELLOW}Pushing ${service_name} to registry...${NC}"
    docker push "${REGISTRY}/${service_name}:latest"
  fi

  cd - > /dev/null
  echo -e "${GREEN}✓ ${service_name} built successfully${NC}"
  echo ""
}

# Function to build a Rust WASM module
build_wasm() {
  local module_name=$1
  local module_path=$2

  echo -e "${YELLOW}Building Rust WASM module: ${module_name}...${NC}"

  # Check if Rust/Cargo is installed
  if ! command -v cargo &> /dev/null; then
    echo -e "${RED}Error: Rust/Cargo is not installed. Please install from https://rustup.rs/${NC}"
    exit 1
  fi

  # Check if wasm32-wasip1 target is installed
  if ! rustup target list --installed | grep -q wasm32-wasip1; then
    echo -e "${YELLOW}Installing wasm32-wasip1 target...${NC}"
    rustup target add wasm32-wasip1
  fi

  cd "$module_path"

  # Build WASM module with Rust
  cargo build --target wasm32-wasip1 --release

  local wasm_file="target/wasm32-wasip1/release/${module_name//-/_}.wasm"

  if [ ! -f "$wasm_file" ]; then
    echo -e "${RED}Error: Failed to build ${wasm_file}${NC}"
    exit 1
  fi

  echo -e "${GREEN}✓ ${module_name}.wasm built successfully ($(du -h ${wasm_file} | cut -f1))${NC}"

  cd - > /dev/null
  echo ""
}

# Check prerequisites
echo -e "${YELLOW}Checking prerequisites...${NC}"

if ! command -v docker &> /dev/null; then
  echo -e "${RED}Error: Docker is not installed${NC}"
  exit 1
fi

if ! command -v go &> /dev/null; then
  echo -e "${RED}Error: Go is not installed${NC}"
  exit 1
fi

if ! command -v cargo &> /dev/null; then
  echo -e "${RED}Error: Rust/Cargo is not installed. Please install from https://rustup.rs/${NC}"
  exit 1
fi

echo -e "${GREEN}✓ Prerequisites met${NC}"
echo ""

# Build WASM modules (Rust)
echo -e "${GREEN}=== Building Rust WASM Modules ===${NC}"
build_wasm "client-filter-rust" "wasm/client-filter-rust"
build_wasm "server-filter-rust" "wasm/server-filter-rust"

# Build Go services
echo -e "${GREEN}=== Building Services ===${NC}"
build_service "jwt-vending-service" "services/jwt-vending"
build_service "sgnl-pdp-service" "services/sgnl-pdp"
build_service "service-a" "services/service-a"
build_service "service-b" "services/service-b"

# Summary
echo -e "${GREEN}=== Build Summary ===${NC}"
echo -e "Rust WASM modules:"
echo -e "  • client-filter-rust.wasm - $(ls -lh wasm/client-filter-rust/target/wasm32-wasip1/release/client_filter_rust.wasm 2>/dev/null | awk '{print $5}' || echo 'not found')"
echo -e "  • server-filter-rust.wasm - $(ls -lh wasm/server-filter-rust/target/wasm32-wasip1/release/server_filter_rust.wasm 2>/dev/null | awk '{print $5}' || echo 'not found')"
echo ""
echo -e "Docker images:"
docker images | grep -E "(jwt-vending-service|sgnl-pdp-service|service-a|service-b)" | head -4
echo ""

if [ "$PUSH" = true ]; then
  echo -e "${GREEN}✓ All images built and pushed successfully!${NC}"
else
  echo -e "${GREEN}✓ All images built successfully!${NC}"
  echo -e "${YELLOW}Tip: Use --push --project YOUR_PROJECT_ID to push to Artifact Registry${NC}"
fi