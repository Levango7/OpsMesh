#!/usr/bin/env bash
# OpsMesh Graceful Shutdown Script
# Usage: ./scripts/stop.sh [--remove-volumes]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && PROJECT_DIR="$(dirname "$SCRIPT_DIR")" && echo "$PROJECT_DIR")"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
COMPOSE_FILE="${PROJECT_DIR}/docker-compose.prod.yml"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'
BOLD='\033[1m'

log_info()  { echo -e "${BLUE}[INFO]${NC}  $(date '+%H:%M:%S') $*"; }
log_ok()    { echo -e "${GREEN}[OK]${NC}    $(date '+%H:%M:%S') $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $(date '+%H:%M:%S') $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $(date '+%H:%M:%S') $*"; }

REMOVE_VOLUMES=false

for arg in "$@"; do
    case "$arg" in
        --remove-volumes|-v) REMOVE_VOLUMES=true ;;
        --help|-h)
            echo "Usage: $0 [--remove-volumes]"
            echo "  --remove-volumes, -v  Also remove data volumes (DESTRUCTIVE)"
            exit 0
            ;;
    esac
done

echo -e "${BOLD}${YELLOW}Stopping OpsMesh...${NC}\n"

cd "$PROJECT_DIR"

# Stop application services first (reverse order)
log_info "Stopping application services..."
docker compose -f "$COMPOSE_FILE" stop -t 30 \
    portal-svc aio-svc gpu-svc log-svc config-svc alert-svc task-svc device-svc auth-svc controlplane \
    2>/dev/null || true
log_ok "Application services stopped"

# Stop observability
log_info "Stopping observability stack..."
docker compose -f "$COMPOSE_FILE" stop -t 15 \
    grafana loki otel-collector prometheus \
    2>/dev/null || true
log_ok "Observability stack stopped"

# Stop infrastructure
log_info "Stopping infrastructure..."
docker compose -f "$COMPOSE_FILE" stop -t 30 redis mysql 2>/dev/null || true
log_ok "Infrastructure stopped"

# Remove containers
log_info "Removing containers..."
if [ "$REMOVE_VOLUMES" = true ]; then
    docker compose -f "$COMPOSE_FILE" down -v --remove-orphans 2>/dev/null || true
    log_warn "All containers and volumes removed"
else
    docker compose -f "$COMPOSE_FILE" down --remove-orphans 2>/dev/null || true
    log_ok "All containers removed (volumes preserved)"
fi

echo ""
log_ok "OpsMesh stopped successfully"
echo -e "  ${BLUE}Data volumes preserved. Use --remove-volumes to delete them.${NC}"
