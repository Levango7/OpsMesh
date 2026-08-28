#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="${SCRIPT_DIR}/deployments"
HPA_DIR="${SCRIPT_DIR}/hpa"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
CLUSTER_NAME="opsmesh-local"
NAMESPACE="opsmesh"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }
log_step()  { echo -e "${BLUE}[STEP]${NC}  $*"; }

SERVICES=(auth-svc task-svc alert-svc device-svc gpu-svc)

usage() {
    cat <<EOF
Usage: $(basename "$0") [OPTIONS]

Deploy OpsMesh to the local Kind cluster.

Options:
  --load-images    Build and load OpsMesh Docker images into Kind
  --skip-images    Skip image loading (images already in cluster)
  --namespace NS   Target namespace (default: opsmesh)
  --help           Show this help message
EOF
}

LOAD_IMAGES=false
SKIP_IMAGES=false

while [[ $# -gt 0 ]]; do
    case "$1" in
        --load-images) LOAD_IMAGES=true; shift ;;
        --skip-images) SKIP_IMAGES=true; shift ;;
        --namespace) NAMESPACE="$2"; shift 2 ;;
        --help) usage; exit 0 ;;
        *) log_error "Unknown option: $1"; usage; exit 1 ;;
    esac
done

check_cluster() {
    log_step "Checking cluster connectivity..."

    if ! kubectl cluster-info &>/dev/null; then
        log_error "Cannot connect to Kubernetes cluster. Run ./create-cluster.sh first."
        exit 1
    fi

    if ! kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
        log_error "Kind cluster '${CLUSTER_NAME}' not found. Run ./create-cluster.sh first."
        exit 1
    fi

    log_info "Connected to cluster."
}

apply_rbac() {
    log_step "Applying RBAC resources..."

    kubectl apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: opsmesh-sa
  namespace: ${NAMESPACE}
  labels:
    app.kubernetes.io/name: opsmesh
    app.kubernetes.io/component: serviceaccount
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: opsmesh-role
  labels:
    app.kubernetes.io/name: opsmesh
rules:
  - apiGroups: [""]
    resources: ["pods", "services", "endpoints", "configmaps", "secrets", "namespaces"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["apps"]
    resources: ["deployments", "daemonsets", "replicasets", "statefulsets"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["autoscaling"]
    resources: ["horizontalpodautoscalers"]
    verbs: ["get", "list", "watch", "create", "update", "patch"]
  - apiGroups: ["networking.k8s.io"]
    resources: ["ingresses", "networkpolicies"]
    verbs: ["get", "list", "watch", "create", "update", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: opsmesh-rolebinding
  labels:
    app.kubernetes.io/name: opsmesh
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: opsmesh-role
subjects:
  - kind: ServiceAccount
    name: opsmesh-sa
    namespace: ${NAMESPACE}
EOF

    log_info "RBAC resources applied."
}

apply_secrets() {
    log_step "Applying secrets..."

    local jwt_secret
    jwt_secret=$(openssl rand -base64 32 2>/dev/null || head -c 32 /dev/urandom | base64)

    kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: opsmesh-secrets
  namespace: ${NAMESPACE}
  labels:
    app.kubernetes.io/name: opsmesh
    app.kubernetes.io/component: secrets
type: Opaque
stringData:
  jwt-secret: "${jwt_secret}"
  provision-secret: "$(openssl rand -base64 24 2>/dev/null || head -c 24 /dev/urandom | base64)"
  alert-webhook-url: ""
EOF

    log_info "Secrets applied."
}

apply_namespace() {
    log_step "Applying namespace..."
    kubectl apply -f "${DEPLOY_DIR}/namespace.yaml"
    log_info "Namespace '${NAMESPACE}' ready."
}

apply_configmap() {
    log_step "Applying ConfigMap..."
    kubectl apply -f "${DEPLOY_DIR}/configmap.yaml"
    log_info "ConfigMap applied."
}

apply_deployments() {
    log_step "Applying deployments..."

    for svc in "${SERVICES[@]}"; do
        local f="${DEPLOY_DIR}/${svc}-deployment.yaml"
        if [[ -f "$f" ]]; then
            log_info "Applying ${svc} deployment..."
            kubectl apply -f "$f"
        else
            log_warn "Deployment file not found: ${f}. Skipping."
        fi
    done

    log_info "All deployments applied."
}

apply_hpa() {
    log_step "Applying HorizontalPodAutoscalers..."

    for svc in "${SERVICES[@]}"; do
        local f="${HPA_DIR}/${svc}-hpa.yaml"
        if [[ -f "$f" ]]; then
            log_info "Applying ${svc} HPA..."
            kubectl apply -f "$f"
        else
            log_warn "HPA file not found: ${f}. Skipping."
        fi
    done

    log_info "All HPAs applied."
}

apply_ingress() {
    log_step "Applying Ingress..."

    kubectl apply -f - <<EOF
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: opsmesh-ingress
  namespace: ${NAMESPACE}
  labels:
    app.kubernetes.io/name: opsmesh
  annotations:
    nginx.ingress.kubernetes.io/rewrite-target: /
    nginx.ingress.kubernetes.io/ssl-redirect: "false"
    nginx.ingress.kubernetes.io/proxy-body-size: "50m"
    nginx.ingress.kubernetes.io/proxy-read-timeout: "300"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "300"
spec:
  ingressClassName: nginx
  rules:
    - host: opsmesh.local
      http:
        paths:
          - path: /api/auth
            pathType: Prefix
            backend:
              service:
                name: auth-svc
                port:
                  number: 8080
          - path: /api/tasks
            pathType: Prefix
            backend:
              service:
                name: task-svc
                port:
                  number: 8080
          - path: /api/alerts
            pathType: Prefix
            backend:
              service:
                name: alert-svc
                port:
                  number: 8080
          - path: /api/devices
            pathType: Prefix
            backend:
              service:
                name: device-svc
                port:
                  number: 8080
          - path: /api/gpu
            pathType: Prefix
            backend:
              service:
                name: gpu-svc
                port:
                  number: 8080
          - path: /
            pathType: Prefix
            backend:
              service:
                name: auth-svc
                port:
                  number: 8080
EOF

    log_info "Ingress applied."
}

wait_for_rollout() {
    log_step "Waiting for deployments to rollout..."

    for svc in "${SERVICES[@]}"; do
        if kubectl get deployment "${svc}" -n "${NAMESPACE}" &>/dev/null; then
            log_info "Waiting for ${svc} rollout..."
            kubectl rollout status deployment/"${svc}" -n "${NAMESPACE}" --timeout=180s || \
                log_warn "${svc} rollout timed out (may need image loading)."
        fi
    done

    log_info "Rollout checks complete."
}

load_images() {
    log_step "Building and loading OpsMesh images into Kind..."

    if [[ ! -d "${PROJECT_ROOT}/services" ]]; then
        log_error "Services directory not found at ${PROJECT_ROOT}/services"
        exit 1
    fi

    local registry="${REGISTRY:-localhost:5000}"
    local tag="${IMAGE_TAG:-latest}"

    for svc in "${SERVICES[@]}"; do
        local svc_dir="${PROJECT_ROOT}/services/${svc}"
        if [[ -d "$svc_dir" ]]; then
            local image_name="opsmesh/${svc}:${tag}"
            log_info "Building ${image_name}..."

            if [[ -f "${svc_dir}/Dockerfile" ]]; then
                docker build -t "$image_name" "${svc_dir}/"
            elif [[ -f "${PROJECT_ROOT}/deploy/docker/Dockerfile.micro" ]]; then
                docker build -t "$image_name" \
                    -f "${PROJECT_ROOT}/deploy/docker/Dockerfile.micro" \
                    --build-arg SERVICE_NAME="$svc" \
                    "${PROJECT_ROOT}/"
            else
                log_warn "No Dockerfile found for ${svc}. Creating minimal image..."
                create_minimal_image "$svc" "$image_name"
            fi

            log_info "Loading ${image_name} into Kind..."
            kind load docker-image "$image_name" --name "${CLUSTER_NAME}"
        else
            log_warn "Service directory not found: ${svc_dir}. Creating minimal image..."
            create_minimal_image "$svc" "opsmesh/${svc}:${tag}"
            kind load docker-image "opsmesh/${svc}:${tag}" --name "${CLUSTER_NAME}"
        fi
    done

    log_info "All images loaded into Kind cluster."
}

create_minimal_image() {
    local svc_name="$1"
    local image_name="$2"

    local tmpdir
    tmpdir=$(mktemp -d)

    cat > "${tmpdir}/Dockerfile" <<DOCKERFILE
FROM alpine:3.19
RUN apk --no-cache add ca-certificates curl && \
    adduser -D -u 65532 opsmesh
USER opsmesh
EXPOSE 8080 9090 9091
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/healthz || exit 1
ENTRYPOINT ["/bin/sh", "-c", "echo 'OpsMesh ${svc_name} placeholder' && exec sleep infinity"]
DOCKERFILE

    docker build -t "$image_name" "$tmpdir"
    rm -rf "$tmpdir"
}

print_status() {
    echo ""
    log_info "========================================="
    log_info "  OpsMesh Deployment Status"
    log_info "========================================="
    echo ""
    echo "Pods:"
    kubectl get pods -n "${NAMESPACE}" -o wide
    echo ""
    echo "Services:"
    kubectl get svc -n "${NAMESPACE}"
    echo ""
    echo "HPA:"
    kubectl get hpa -n "${NAMESPACE}"
    echo ""
    echo "Ingress:"
    kubectl get ingress -n "${NAMESPACE}"
    echo ""
    echo "Access:"
    echo "  - Add '127.0.0.1 opsmesh.local' to /etc/hosts for ingress"
    echo "  - Port-forward: kubectl port-forward svc/auth-svc 8080:8080 -n ${NAMESPACE}"
    echo ""
}

main() {
    echo "============================================"
    echo "  OpsMesh Local Deployment"
    echo "============================================"
    echo ""

    check_cluster

    if [[ "$LOAD_IMAGES" == true ]]; then
        load_images
    fi

    apply_namespace
    apply_rbac
    apply_secrets
    apply_configmap
    apply_deployments
    apply_hpa
    apply_ingress
    wait_for_rollout
    print_status
}

main "$@"
