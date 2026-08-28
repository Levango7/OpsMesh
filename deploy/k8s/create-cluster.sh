#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLUSTER_NAME="opsmesh-local"
KIND_CONFIG="${SCRIPT_DIR}/kind-config.yaml"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }
log_step()  { echo -e "${BLUE}[STEP]${NC}  $*"; }

check_prerequisites() {
    log_step "Checking prerequisites..."

    if ! command -v kind &> /dev/null; then
        log_error "kind is not installed. Install from https://kind.sigs.k8s.io/"
        exit 1
    fi
    log_info "kind found: $(kind version | head -1)"

    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl is not installed. Install from https://kubernetes.io/docs/tasks/tools/"
        exit 1
    fi
    log_info "kubectl found: $(kubectl version --client -o json 2>/dev/null | grep -o '"gitVersion": *"[^"]*"' | head -1)"

    if ! command -v helm &> /dev/null; then
        log_warn "helm not found. Some addons may not install. Install from https://helm.sh/"
    else
        log_info "helm found: $(helm version --short 2>/dev/null || echo 'unknown')"
    fi

    if ! command -v docker &> /dev/null; then
        log_error "docker is not installed. Kind requires Docker."
        exit 1
    fi
    log_info "docker found: $(docker --version)"
}

create_cluster() {
    log_step "Creating Kind cluster '${CLUSTER_NAME}'..."

    if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
        log_warn "Cluster '${CLUSTER_NAME}' already exists. Skipping creation."
    else
        kind create cluster --name "${CLUSTER_NAME}" --config "${KIND_CONFIG}" --wait 120s
        log_info "Cluster '${CLUSTER_NAME}' created successfully."
    fi

    kubectl config use-context "kind-${CLUSTER_NAME}" &>/dev/null || true
    kubectl cluster-info --context "kind-${CLUSTER_NAME}" &>/dev/null || true
    log_info "kubectl context set to kind-${CLUSTER_NAME}"
}

install_metrics_server() {
    log_step "Installing metrics-server..."

    kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml

    kubectl patch deployment metrics-server -n kube-system --type='json' -p='[
      {"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--kubelet-insecure-tls"},
      {"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--kubelet-preferred-address-types=InternalIP,ExternalIP,Hostname"}
    ]' 2>/dev/null || true

    kubectl rollout status deployment/metrics-server -n kube-system --timeout=120s
    log_info "metrics-server installed and ready."
}

install_nginx_ingress() {
    log_step "Installing nginx-ingress controller..."

    kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml

    kubectl rollout status deployment/ingress-nginx-controller -n ingress-nginx --timeout=180s

    cat <<'EOF' | kubectl apply -f -
apiVersion: v1
kind: Service
metadata:
  name: ingress-nginx-nodeport
  namespace: ingress-nginx
  labels:
    app.kubernetes.io/name: ingress-nginx
spec:
  type: NodePort
  ports:
    - name: http
      port: 80
      targetPort: 80
      nodePort: 30080
      protocol: TCP
    - name: https
      port: 443
      targetPort: 443
      nodePort: 30443
      protocol: TCP
  selector:
    app.kubernetes.io/name: ingress-nginx
    app.kubernetes.io/component: controller
EOF

    log_info "nginx-ingress installed and ready."
}

install_cert_manager() {
    log_step "Installing cert-manager..."

    kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.14.4/cert-manager.yaml

    kubectl rollout status deployment/cert-manager -n cert-manager --timeout=120s
    kubectl rollout status deployment/cert-manager-cainjector -n cert-manager --timeout=120s
    kubectl rollout status deployment/cert-manager-webhook -n cert-manager --timeout=120s

    cat <<EOF | kubectl apply -f -
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: selfsigned-issuer
spec:
  selfSigned: {}
EOF

    log_info "cert-manager installed and ready."
}

install_prometheus_operator() {
    log_step "Installing prometheus-operator..."

    helm repo add prometheus-community https://prometheus-community.github.io/helm-charts 2>/dev/null || true
    helm repo update 2>/dev/null || true

    kubectl create namespace monitoring 2>/dev/null || true

    helm upgrade --install prometheus prometheus-community/kube-prometheus-stack \
        --namespace monitoring \
        --set prometheus.prometheusSpec.serviceMonitorSelectorNilUsesHelmValues=false \
        --set prometheus.prometheusSpec.podMonitorSelectorNilUsesHelmValues=false \
        --set grafana.enabled=true \
        --set grafana.service.type=NodePort \
        --set grafana.service.nodePort=30300 \
        --wait \
        --timeout 300s 2>/dev/null

    if [ $? -eq 0 ]; then
        log_info "prometheus-operator installed via helm."
    else
        log_warn "helm install failed, applying raw manifests..."
        kubectl apply -f https://github.com/prometheus-operator/prometheus-operator/releases/download/v0.71.0/bundle.yaml 2>/dev/null || \
            log_warn "prometheus-operator raw manifest failed. Skipping."
    fi
}

print_status() {
    echo ""
    log_info "========================================="
    log_info "  Kind cluster '${CLUSTER_NAME}' ready!"
    log_info "========================================="
    echo ""
    echo "Nodes:"
    kubectl get nodes -o wide
    echo ""
    echo "System pods:"
    kubectl get pods -A
    echo ""
    echo "Access points:"
    echo "  - Kubernetes API:    https://127.0.0.1:$(kubectl get endpoints kubernetes -o jsonpath='{.subsets[0].ports[0].port}' 2>/dev/null || echo '6443')"
    echo "  - Ingress HTTP:      http://localhost:80"
    echo "  - Ingress HTTPS:     https://localhost:443"
    echo "  - Grafana:           http://localhost:30300 (admin/prom-operator)"
    echo ""
    echo "Next steps:"
    echo "  1. Build and load images:  ./deploy-opsmesh.sh --load-images"
    echo "  2. Deploy OpsMesh:         ./deploy-opsmesh.sh"
    echo "  3. Run chaos tests:        ./chaos-test.sh"
    echo "  4. Run load tests:         ./load-test.sh"
    echo ""
}

main() {
    echo "============================================"
    echo "  OpsMesh Local Kind Cluster Setup"
    echo "============================================"
    echo ""

    check_prerequisites
    create_cluster
    install_metrics_server
    install_nginx_ingress
    install_cert_manager
    install_prometheus_operator
    print_status
}

main "$@"
