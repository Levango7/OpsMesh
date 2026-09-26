#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="${SCRIPT_DIR}/deployments"
HPA_DIR="${SCRIPT_DIR}/hpa"
# deploy/k8s → 上两级即仓库根。原写法 ../../.. 会指到仓库的【父目录】，
# 使 --load-images 在检查 ${PROJECT_ROOT}/services 时直接报目录不存在。
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
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

# --skip-images 与 --load-images 共用一个开关（默认即不加载）：此前 SKIP_IMAGES 解析后
# 从不被读，于是这个写进 usage 的开关是个哑按钮——传与不传行为一致，且没人报错。
# 两个都给时以**最后出现者**为准（与常见 CLI 语义一致）。
while [[ $# -gt 0 ]]; do
    case "$1" in
        --load-images) LOAD_IMAGES=true; shift ;;
        --skip-images) LOAD_IMAGES=false; shift ;;
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

apply_secrets() {
    log_step "Applying secrets..."

    local jwt_secret admin_password
    jwt_secret=$(openssl rand -base64 32 2>/dev/null || head -c 32 /dev/urandom | base64)
    # 口令须同时满足控制面与 auth-svc 的强口令规则（auth-svc 更严：≥12 位 + 大小写 + 数字 +
    # 特殊字符）。纯 base64 随机偶有无数字/无大写，故用固定前缀 + 随机 hex 段，保证恒定合规；
    # 特殊字符取 '!'（避开 '@'/'#'/'$'/引号等会破坏 DSN、env 与 YAML 的字符）。
    admin_password="Pw1!$(openssl rand -hex 12 2>/dev/null || head -c 12 /dev/urandom | od -An -tx1 | tr -d ' \n')"

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
  admin-password: "${admin_password}"
EOF

    log_info "Secrets applied."
    log_warn "初始 admin 口令（仅本地样例）：${admin_password}"
    log_warn "首次登录强制改密；生产环境改用随机口令交付通道，勿用可预测口令。"
}

apply_namespace() {
    log_step "Applying namespace..."
    kubectl apply -f "${DEPLOY_DIR}/namespace.yaml"
    log_info "Namespace '${NAMESPACE}' ready."
}

# 说明：本样例【不】创建任何 RBAC。样例只部署 5 个微服务，其运行期不访问 K8s API
# （task-svc 多副本选主默认走进程内 stub，见 cmd/task-svc/main.go: leader.NewStub()）。
# 原脚本给 Pod 绑定了一个含 secrets 写、namespace 删除的 ClusterRole（过权），已删除；
# 控制面集群能力（internal/k8s/client.go）所需的 RBAC 由生产部署（Helm）侧另行提供。

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

    # 路径对照各服务真实注册的路由（2026-09-25 逐服务核实），且【不】做 rewrite：
    # 各服务自身就服务 /api/v1/... 前缀，剥掉前缀会 404。
    #   auth-svc   /api/v1/auth|users|roles|permissions            :8081
    #   task-svc   /api/v1/tasks|schedules|approval                :8081
    #   alert-svc  /api/v1/escalation|oncall                       :8080
    #   device-svc /api/v1/devices|agents|catalog|cmdb|discovery|provision :8081
    #   gpu-svc    /api/v1/gpu                                     :8090
    kubectl apply -f - <<EOF
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: opsmesh-ingress
  namespace: ${NAMESPACE}
  labels:
    app.kubernetes.io/name: opsmesh
  annotations:
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
          - path: /api/v1/auth
            pathType: Prefix
            backend:
              service:
                name: auth-svc
                port:
                  number: 8081
          - path: /api/v1/users
            pathType: Prefix
            backend:
              service:
                name: auth-svc
                port:
                  number: 8081
          - path: /api/v1/roles
            pathType: Prefix
            backend:
              service:
                name: auth-svc
                port:
                  number: 8081
          - path: /api/v1/permissions
            pathType: Prefix
            backend:
              service:
                name: auth-svc
                port:
                  number: 8081
          - path: /api/v1/tasks
            pathType: Prefix
            backend:
              service:
                name: task-svc
                port:
                  number: 8081
          - path: /api/v1/schedules
            pathType: Prefix
            backend:
              service:
                name: task-svc
                port:
                  number: 8081
          - path: /api/v1/approval
            pathType: Prefix
            backend:
              service:
                name: task-svc
                port:
                  number: 8081
          - path: /api/v1/escalation
            pathType: Prefix
            backend:
              service:
                name: alert-svc
                port:
                  number: 8080
          - path: /api/v1/oncall
            pathType: Prefix
            backend:
              service:
                name: alert-svc
                port:
                  number: 8080
          - path: /api/v1/devices
            pathType: Prefix
            backend:
              service:
                name: device-svc
                port:
                  number: 8081
          - path: /api/v1/catalog
            pathType: Prefix
            backend:
              service:
                name: device-svc
                port:
                  number: 8081
          - path: /api/v1/gpu
            pathType: Prefix
            backend:
              service:
                name: gpu-svc
                port:
                  number: 8090
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

    # 标签默认 0.9.2：与 deployments/*.yaml 中的 image 标签、当前发布版本一致。
    local tag="${IMAGE_TAG:-0.9.2}"

    for svc in "${SERVICES[@]}"; do
        local svc_dir="${PROJECT_ROOT}/services/${svc}"
        if [[ ! -d "$svc_dir" ]]; then
            log_error "Service directory not found: ${svc_dir}"
            exit 1
        fi

        if [[ ! -f "${PROJECT_ROOT}/Dockerfile.service" ]]; then
            log_error "缺少根级 Dockerfile.service（微服务统一构建模板），无法构建 ${svc}"
            exit 1
        fi

        local image_name="opsmesh/${svc}:${tag}"
        log_info "Building ${image_name}..."
        # 构建上下文必须是仓库根：services/*/go.mod 均 `replace opsmesh => ../..`，
        # 单服务目录作上下文时容器内无主模块，go mod verify 必失败
        # （实测报 "replaced by ../../: open /go.mod: no such file"）。
        # 与 .github/workflows/release.yml、docker-compose.prod.yml 同一契约。
        docker build -t "$image_name" \
            -f "${PROJECT_ROOT}/Dockerfile.service" \
            --build-arg SERVICE="$svc" \
            --build-arg VERSION="$tag" \
            "${PROJECT_ROOT}/"

        log_info "Loading ${image_name} into Kind..."
        kind load docker-image "$image_name" --name "${CLUSTER_NAME}"
    done

    log_info "All images loaded into Kind cluster."
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
    echo "  - Port-forward: kubectl port-forward svc/auth-svc 8081:8081 -n ${NAMESPACE}"
    echo ""
    log_warn "开发样例边界：本目录仅供本地 Kind 联调，非生产部署基线。"
    log_warn "  - 无 RBAC：Pod 运行期不访问 K8s API（控制面集群能力由 Helm/生产侧提供）"
    log_warn "  - 密码/密钥为脚本随机生成，经 Secret 注入；生产走正式密钥管理"
    log_warn "  - 无 TLS、无 NetworkPolicy、无资源配额策略；生产基线见 deploy/helm/opsmesh"
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
    apply_secrets
    apply_deployments
    apply_hpa
    apply_ingress
    wait_for_rollout
    print_status
}

main "$@"
