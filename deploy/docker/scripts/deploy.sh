#!/usr/bin/env bash
# OpsMesh Production Deployment Script
# One-command deployment: ./scripts/deploy.sh
set -euo pipefail

# ============================================================
# Configuration
# ============================================================
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
COMPOSE_FILE="${PROJECT_DIR}/docker-compose.prod.yml"
ENV_FILE="${PROJECT_DIR}/.env"
LOG_FILE="${PROJECT_DIR}/deploy.log"
MIN_DISK_GB=10
REQUIRED_PORTS=(8080 9090 9091 3306 6379 9092 3000 3100 4317 4318)

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'
BOLD='\033[1m'

# ============================================================
# Helper Functions
# ============================================================
log_info()  { echo -e "${BLUE}[INFO]${NC}  $(date '+%H:%M:%S') $*"; }
log_ok()    { echo -e "${GREEN}[OK]${NC}    $(date '+%H:%M:%S') $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $(date '+%H:%M:%S') $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $(date '+%H:%M:%S') $*"; }
log_section() { echo -e "\n${BOLD}${CYAN}═══ $* ═══${NC}\n"; }

check_command() {
    if ! command -v "$1" &>/dev/null; then
        log_error "'$1' is required but not installed."
        return 1
    fi
}

port_in_use() {
    local port=$1
    if command -v ss &>/dev/null; then
        ss -tln | grep -q ":${port} "
    elif command -v netstat &>/dev/null; then
        netstat -tln 2>/dev/null | grep -q ":${port} "
    else
        # Fallback: try to bind
        python3 -c "
import socket, sys
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
try:
    s.bind(('0.0.0.0', $port))
    s.close()
    sys.exit(1)
except:
    sys.exit(0)
" 2>/dev/null
    fi
}

wait_for_healthy() {
    local service=$1
    local max_wait=${2:-120}
    local interval=5
    local elapsed=0

    log_info "Waiting for ${service} to become healthy..."
    while [ $elapsed -lt $max_wait ]; do
        if docker compose -f "$COMPOSE_FILE" ps "$service" 2>/dev/null | grep -q "healthy"; then
            log_ok "${service} is healthy"
            return 0
        fi
        sleep $interval
        elapsed=$((elapsed + interval))
        echo -n "."
    done
    echo ""
    log_error "${service} did not become healthy within ${max_wait}s"
    return 1
}

wait_for_services() {
    local services=("$@")
    for svc in "${services[@]}"; do
        wait_for_healthy "$svc" 120 || return 1
    done
}

# ============================================================
# Pre-flight Checks
# ============================================================
preflight_checks() {
    log_section "Pre-flight Checks"

    # Check Docker
    check_command docker || exit 1
    check_command docker compose || exit 1

    # Check Docker is running
    if ! docker info &>/dev/null; then
        log_error "Docker daemon is not running"
        exit 1
    fi
    log_ok "Docker is running ($(docker version --format '{{.Server.Version}}' 2>/dev/null || echo 'unknown'))"

    # Check disk space
    local available_gb
    if command -v df &>/dev/null; then
        available_gb=$(df -BG "$PROJECT_DIR" 2>/dev/null | awk 'NR==2 {gsub(/G/,"",$4); print $4}' || echo "0")
        if [ "$available_gb" -lt "$MIN_DISK_GB" ]; then
            log_error "Insufficient disk space: ${available_gb}GB available, ${MIN_DISK_GB}GB required"
            exit 1
        fi
        log_ok "Disk space: ${available_gb}GB available"
    fi

    # Check ports
    local ports_in_use=()
    for port in "${REQUIRED_PORTS[@]}"; do
        if port_in_use "$port"; then
            ports_in_use+=("$port")
        fi
    done

    if [ ${#ports_in_use[@]} -gt 0 ]; then
        log_warn "Ports already in use: ${ports_in_use[*]}"
        read -rp "Continue anyway? [y/N] " answer
        if [[ ! "$answer" =~ ^[Yy]$ ]]; then
            exit 1
        fi
    else
        log_ok "All required ports available"
    fi

    # Check/create .env
    if [ ! -f "$ENV_FILE" ]; then
        log_info "Creating .env from defaults..."
        generate_env_file
    fi
    log_ok "Environment file ready: ${ENV_FILE}"

    # Verify compose file
    if ! docker compose -f "$COMPOSE_FILE" config --quiet 2>/dev/null; then
        log_error "Invalid docker-compose.prod.yml configuration"
        docker compose -f "$COMPOSE_FILE" config 2>&1 | head -20
        exit 1
    fi
    log_ok "Docker Compose configuration valid"
}

# ============================================================
# Environment Generation
# ============================================================
generate_env_file() {
    local jwt_secret
    jwt_secret=$(openssl rand -hex 32 2>/dev/null || head -c 64 /dev/urandom | xxd -p | head -1)
    local mysql_pass
    mysql_pass="opsmesh_$(openssl rand -hex 8 2>/dev/null || echo 'default_change_me')"
    local redis_pass
    redis_pass="opsmesh_$(openssl rand -hex 8 2>/dev/null || echo 'default_change_me')"

    cat > "$ENV_FILE" <<EOF
# OpsMesh Production Environment
# Generated on $(date -u +"%Y-%m-%dT%H:%M:%SZ")
# IMPORTANT: Change all default passwords before production use!

# === Core Security ===
JWT_SECRET=${jwt_secret}
MYSQL_ROOT_PASSWORD=opsmesh_root_secure_2024
MYSQL_USER=opsmesh
MYSQL_PASSWORD=${mysql_pass}
REDIS_PASSWORD=${redis_pass}

# === Service Ports ===
CONTROLPLANE_HTTP_PORT=8080
CONTROLPLANE_GRPC_PORT=9090
CONTROLPLANE_METRICS_PORT=9091
MYSQL_PORT=3306
REDIS_PORT=6379
AUTH_SVC_HTTP_PORT=8100
DEVICE_SVC_HTTP_PORT=8101
TASK_SVC_HTTP_PORT=8102
ALERT_SVC_HTTP_PORT=8103
CONFIG_SVC_HTTP_PORT=8106
LOG_SVC_HTTP_PORT=8105
LOG_SVC_GRPC_PORT=9095
GPU_SVC_HTTP_PORT=8107
PORTAL_SVC_HTTP_PORT=8109
AIO_SVC_HTTP_PORT=8108

# === Observability Ports ===
PROMETHEUS_PORT=9092
GRAFANA_PORT=3000
LOKI_PORT=3100
OTEL_GRPC_PORT=4317
OTEL_HTTP_PORT=4318
OTEL_METRICS_PORT=8888

# === Grafana ===
GRAFANA_ADMIN_USER=admin
GRAFANA_ADMIN_PASSWORD=opsmesh_grafana_2024

# === Feature Flags ===
PRODUCTION_MODE=true
REQUIRE_AUTH=true
COOKIE_SECURE=false
PUBLIC_REGISTER=false
ALLOW_PUBLIC_REGISTER=false
TRUST_PROXY=false
GRPC_REQUIRE_SIGNATURE=true

# === Store Types ===
DEVICE_STORE_TYPE=memory
TASK_STORE_TYPE=memory
ALERT_STORE_TYPE=memory
CONFIG_STORE_TYPE=memory
LOG_BACKEND=loki

# === Logging ===
LOG_LEVEL=info

# === Timezone ===
TZ=UTC

# === Version ===
OPSMESH_VERSION=latest
EOF
    chmod 600 "$ENV_FILE"
    log_info "Generated .env with random secrets (chmod 600)"
}

# ============================================================
# Build Phase
# ============================================================
build_images() {
    log_section "Building Images"

    export COMPOSE_FILE
    docker compose -f "$COMPOSE_FILE" build --parallel --pull 2>&1 | tee -a "$LOG_FILE" | while IFS= read -r line; do
        echo "  ${line}"
    done

    log_ok "All images built successfully"
}

# ============================================================
# Infrastructure Startup
# ============================================================
start_infrastructure() {
    log_section "Starting Infrastructure"

    log_info "Starting MySQL..."
    docker compose -f "$COMPOSE_FILE" up -d mysql
    wait_for_healthy mysql 180

    log_info "Starting Redis..."
    docker compose -f "$COMPOSE_FILE" up -d redis
    wait_for_healthy redis 60

    log_ok "Infrastructure ready"
}

# ============================================================
# Observability Startup
# ============================================================
start_observability() {
    log_section "Starting Observability Stack"

    log_info "Starting Prometheus..."
    docker compose -f "$COMPOSE_FILE" up -d prometheus

    log_info "Starting Loki..."
    docker compose -f "$COMPOSE_FILE" up -d loki

    log_info "Starting OpenTelemetry Collector..."
    docker compose -f "$COMPOSE_FILE" up -d otel-collector

    log_info "Starting Grafana..."
    docker compose -f "$COMPOSE_FILE" up -d grafana

    wait_for_healthy prometheus 60
    wait_for_healthy otel-collector 60

    log_ok "Observability stack ready"
}

# ============================================================
# Application Services Startup
# ============================================================
start_services() {
    log_section "Starting OpsMesh Services"

    log_info "Starting controlplane..."
    docker compose -f "$COMPOSE_FILE" up -d controlplane
    wait_for_healthy controlplane 120

    log_info "Starting auth-svc..."
    docker compose -f "$COMPOSE_FILE" up -d auth-svc
    wait_for_healthy auth-svc 60

    log_info "Starting device-svc..."
    docker compose -f "$COMPOSE_FILE" up -d device-svc
    wait_for_healthy device-svc 60

    log_info "Starting task-svc..."
    docker compose -f "$COMPOSE_FILE" up -d task-svc
    wait_for_healthy task-svc 60

    log_info "Starting alert-svc..."
    docker compose -f "$COMPOSE_FILE" up -d alert-svc
    wait_for_healthy alert-svc 60

    log_info "Starting config-svc..."
    docker compose -f "$COMPOSE_FILE" up -d config-svc
    wait_for_healthy config-svc 60

    log_info "Starting log-svc..."
    docker compose -f "$COMPOSE_FILE" up -d log-svc
    wait_for_healthy log-svc 60

    log_info "Starting gpu-svc..."
    docker compose -f "$COMPOSE_FILE" up -d gpu-svc
    wait_for_healthy gpu-svc 60

    log_info "Starting aio-svc..."
    docker compose -f "$COMPOSE_FILE" up -d aio-svc
    wait_for_healthy aio-svc 60

    log_info "Starting portal-svc..."
    docker compose -f "$COMPOSE_FILE" up -d portal-svc
    wait_for_healthy portal-svc 60

    log_ok "All services started"
}

# ============================================================
# Smoke Tests
# ============================================================
run_smoke_tests() {
    log_section "Running Smoke Tests"

    local failures=0

    # Test controlplane health
    log_info "Testing controlplane /healthz..."
    if curl -sf http://localhost:8080/healthz &>/dev/null; then
        log_ok "controlplane /healthz OK"
    else
        log_error "controlplane /healthz FAILED"
        ((failures++))
    fi

    # Test controlplane API
    log_info "Testing controlplane /api/v1/health..."
    if curl -sf http://localhost:8080/api/v1/health &>/dev/null; then
        log_ok "controlplane API OK"
    else
        log_warn "controlplane API endpoint not responding (may need auth)"
    fi

    # Test prometheus
    log_info "Testing Prometheus..."
    if curl -sf http://localhost:9092/-/healthy &>/dev/null; then
        log_ok "Prometheus OK"
    else
        log_error "Prometheus FAILED"
        ((failures++))
    fi

    # Test Grafana
    log_info "Testing Grafana..."
    if curl -sf http://localhost:3000/api/health &>/dev/null; then
        log_ok "Grafana OK"
    else
        log_warn "Grafana not yet ready (may take longer)"
    fi

    # Test service health endpoints
    for port in 8100 8101 8102 8103 8106 8105 8107 8109 8108; do
        if curl -sf "http://localhost:${port}/health" &>/dev/null; then
            log_ok "Service on port ${port} healthy"
        else
            log_warn "Service on port ${port} not responding"
        fi
    done

    if [ $failures -gt 0 ]; then
        log_error "${failures} smoke test(s) failed"
        return 1
    fi

    log_ok "All smoke tests passed"
}

# ============================================================
# Print Access URLs
# ============================================================
print_access_info() {
    log_section "Deployment Complete!"

    echo -e "${BOLD}Access URLs:${NC}"
    echo -e "  ${GREEN}Controlplane API:${NC}  http://localhost:8080"
    echo -e "  ${GREEN}Controlplane gRPC:${NC} localhost:9090"
    echo -e "  ${GREEN}Portal UI:${NC}        http://localhost:8109"
    echo -e "  ${GREEN}Grafana:${NC}           http://localhost:3000"
    echo -e "  ${GREEN}Prometheus:${NC}       http://localhost:9092"
    echo -e "  ${GREEN}Loki:${NC}             http://localhost:3100"
    echo ""
    echo -e "${BOLD}Service Ports:${NC}"
    echo -e "  auth-svc:    8100"
    echo -e "  device-svc:  8101"
    echo -e "  task-svc:    8102"
    echo -e "  alert-svc:   8103"
    echo -e "  log-svc:     8105"
    echo -e "  config-svc:  8106"
    echo -e "  gpu-svc:     8107"
    echo -e "  aio-svc:     8108"
    echo -e "  portal-svc:  8109"
    echo ""
    echo -e "${BOLD}Credentials:${NC}"
    echo -e "  Grafana: admin / (from .env)"
    echo -e "  MySQL:   (from .env)"
    echo ""
    echo -e "${YELLOW}Next steps:${NC}"
    echo -e "  1. Check status:  ./scripts/status.sh"
    echo -e "  2. View logs:     ./scripts/logs.sh"
    echo -e "  3. Stop all:      ./scripts/stop.sh"
    echo ""
}

# ============================================================
# Main
# ============================================================
main() {
    echo -e "${BOLD}"
    echo "  ██████╗ ██████╗ ███████╗███╗   ███╗███████╗███████╗██╗  ██╗"
    echo " ██╔═══██╗██╔══██╗██╔════╝████╗ ████║██╔════╝██╔════╝██║  ██║"
    echo " ██║   ██║██████╔╝█████╗  ██╔████╔██║███████╗███████╗███████║"
    echo " ██║   ██║██╔═══╝ ██╔══╝  ██║╚██╔╝██║╚════██║██╔══╝██╔══██║"
    echo " ╚██████╔╝██║     ███████╗██║ ╚═╝ ██║███████║███████╗██║  ██║"
    echo "  ╚═════╝ ╚═╝     ╚══════╝╚═╝     ╚═╝╚══════╝╚══════╝╚═╝  ╚═╝"
    echo -e "${NC}"
    echo -e "  ${CYAN}Production Deployment v1.0${NC}\n"

    cd "$PROJECT_DIR"

    preflight_checks
    build_images
    start_infrastructure
    start_observability
    start_services
    run_smoke_tests
    print_access_info
}

main "$@"
