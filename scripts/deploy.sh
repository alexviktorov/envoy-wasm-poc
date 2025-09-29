#!/bin/bash
# Deployment script for WASM PEP Demo
# Usage: ./scripts/deploy.sh [--project PROJECT_ID] [--region REGION]

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Default values
PROJECT_ID="${GCP_PROJECT:-}"
REGION="${GCP_REGION:-us-central1}"

# Parse arguments
while [[ $# -gt 0 ]]; do
  case $1 in
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
      echo "Usage: $0 [--project PROJECT_ID] [--region REGION]"
      exit 1
      ;;
  esac
done

# Check if PROJECT_ID is set
if [ -z "$PROJECT_ID" ]; then
  echo -e "${RED}Error: PROJECT_ID must be specified via --project or GCP_PROJECT env var${NC}"
  exit 1
fi

echo -e "${GREEN}=== Deploying WASM PEP Demo ===${NC}"
echo -e "Project: ${PROJECT_ID}"
echo -e "Region: ${REGION}"
echo ""

# Check prerequisites
echo -e "${YELLOW}Checking prerequisites...${NC}"

if ! command -v kubectl &> /dev/null; then
  echo -e "${RED}Error: kubectl is not installed${NC}"
  exit 1
fi

if ! command -v helm &> /dev/null; then
  echo -e "${RED}Error: helm is not installed${NC}"
  exit 1
fi

# Check if kubectl is connected to a cluster
if ! kubectl cluster-info &> /dev/null; then
  echo -e "${RED}Error: kubectl is not connected to a cluster${NC}"
  echo -e "Run: gcloud container clusters get-credentials wasm-pep-demo --region=${REGION}"
  exit 1
fi

echo -e "${GREEN}✓ Prerequisites met${NC}"
echo ""

# Update Kubernetes manifests with PROJECT_ID
echo -e "${YELLOW}Updating Kubernetes manifests with project ID...${NC}"

REGISTRY="${REGION}-docker.pkg.dev/${PROJECT_ID}/wasm-pep-demo"

# Create temporary directory for modified manifests
TMP_DIR=$(mktemp -d)
trap "rm -rf $TMP_DIR" EXIT

for file in k8s/*.yaml; do
  sed "s|gcr.io/PROJECT_ID|${REGISTRY}|g" "$file" > "$TMP_DIR/$(basename $file)"
done

echo -e "${GREEN}✓ Manifests updated${NC}"
echo ""

# Deploy Consul Service Mesh
echo -e "${GREEN}=== Deploying Consul Service Mesh ===${NC}"

# Add HashiCorp Helm repository
echo -e "${YELLOW}Adding HashiCorp Helm repository...${NC}"
helm repo add hashicorp https://helm.releases.hashicorp.com
helm repo update

# Check if Consul is already installed
if helm list -n consul | grep -q consul; then
  echo -e "${YELLOW}Consul is already installed, upgrading...${NC}"
  helm upgrade consul hashicorp/consul -f k8s/consul-values.yaml -n consul
else
  echo -e "${YELLOW}Installing Consul...${NC}"
  kubectl create namespace consul --dry-run=client -o yaml | kubectl apply -f -
  helm install consul hashicorp/consul -f k8s/consul-values.yaml -n consul --create-namespace
fi

echo -e "${YELLOW}Waiting for Consul to be ready...${NC}"
kubectl wait --for=condition=Ready pod -l app=consul -n consul --timeout=300s || true

echo -e "${GREEN}✓ Consul deployed${NC}"
echo ""

# Deploy infrastructure services (JWT vending and PDP)
echo -e "${GREEN}=== Deploying Infrastructure Services ===${NC}"

echo -e "${YELLOW}Deploying JWT Vending Service...${NC}"
kubectl apply -f "$TMP_DIR/jwt-vending.yaml"

echo -e "${YELLOW}Deploying SGNL PDP Service...${NC}"
kubectl apply -f "$TMP_DIR/sgnl-pdp.yaml"

echo -e "${YELLOW}Waiting for infrastructure services to be ready...${NC}"
kubectl wait --for=condition=Ready pod -l app=jwt-vending-service --timeout=120s || true
kubectl wait --for=condition=Ready pod -l app=sgnl-pdp-service --timeout=120s || true

echo -e "${GREEN}✓ Infrastructure services deployed${NC}"
echo ""

# Deploy application services
echo -e "${GREEN}=== Deploying Application Services ===${NC}"

echo -e "${YELLOW}Deploying Service A...${NC}"
kubectl apply -f "$TMP_DIR/service-a.yaml"

echo -e "${YELLOW}Deploying Service B...${NC}"
kubectl apply -f "$TMP_DIR/service-b.yaml"

echo -e "${YELLOW}Waiting for application services to be ready...${NC}"
kubectl wait --for=condition=Ready pod -l app=service-a --timeout=120s || true
kubectl wait --for=condition=Ready pod -l app=service-b --timeout=120s || true

echo -e "${GREEN}✓ Application services deployed${NC}"
echo ""

# Apply Consul service intentions
echo -e "${GREEN}=== Applying Consul Service Intentions ===${NC}"

echo -e "${YELLOW}Applying service intentions...${NC}"
kubectl apply -f k8s/consul-intentions.yaml

echo -e "${GREEN}✓ Service intentions applied${NC}"
echo ""

# Get service endpoints
echo -e "${GREEN}=== Deployment Summary ===${NC}"
echo ""
echo -e "${YELLOW}Consul UI:${NC}"
kubectl get svc -n consul consul-ui -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || echo "  Pending..."
echo ""

echo -e "${YELLOW}Service A (External):${NC}"
SERVICE_A_IP=$(kubectl get svc service-a -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || echo "Pending...")
echo "  IP: $SERVICE_A_IP"
echo ""

echo -e "${YELLOW}All Pods:${NC}"
kubectl get pods -o wide
echo ""

echo -e "${GREEN}=== Deployment Complete! ===${NC}"
echo ""
echo -e "${YELLOW}To test the deployment:${NC}"
if [ "$SERVICE_A_IP" != "Pending..." ] && [ -n "$SERVICE_A_IP" ]; then
  echo "  curl -X POST http://${SERVICE_A_IP}:8080/call-service-b -H 'Content-Type: application/json' -d '{\"asset\":\"asset-x\",\"use_valid_token\":true}'"
else
  echo "  Wait for Service A LoadBalancer IP to be assigned, then run:"
  echo "  ./scripts/test.sh --positive"
fi
echo ""
echo -e "${YELLOW}To access Consul UI:${NC}"
echo "  kubectl port-forward -n consul svc/consul-ui 8500:80"
echo "  Then visit: http://localhost:8500"
echo ""
echo -e "${YELLOW}To view logs:${NC}"
echo "  kubectl logs -l app=service-a -c service-a --tail=50 -f"
echo "  kubectl logs -l app=service-a -c consul-connect-envoy-sidecar --tail=50 -f"