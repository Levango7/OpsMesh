#!/usr/bin/env bash
# OpsMesh Centralized Log Viewer
# Usage: ./scripts/logs.sh [service] [--follow] [--tail N]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
COMPOSE_FILE="${PROJECT_DIR}/docker-compose.prod.yml"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'
BOLD='\033[1m'

FOLLOW=false
TAIL_LINES=100
SERVICE=""

for arg in "$@"; do
    case "$arg" in
        --follow|-f) FOLLOW=true ;;
        --tail|-n)
            TAIL_LINES="${2:-100}"
            shift
            ;;
        --help|-h)
            echo "Usage: $0 [service] [--follow] [--tail N]"
            echo ""
            echo "Services:"
            echo "  controlplane, auth-svc, device-svc, task-svc, alert-svc,"
            echo "  config-svc, log-svc, gpu-svc, portal-svc, aio-svc,"
            echo "  mysql, redis, prometheus, grafana, loki, otel-collector"
            echo ""
            echo "Options:"
            echo "  --follow, -f     Follow log output"
            echo "  --tail N         Show last N lines (default: 100)"
            exit 0
            ;;
        *)
            if [ -z "$SERVICE" ]; then
                SERVICE="$arg"
            fi
            ;;
    esac
done

cd "$PROJECT_DIR"

# Map service names to container names
declare -A CONTAINER_MAP=(
    [controlplane]="opsmesh-controlplane"
    [auth-svc]="opsmesh-auth-svc"
    [device-svc]="opsmesh-device-svc"
    [task-svc]="opsmesh-task-svc"
    [alert-svc]="opsmesh-alert-svc"
    [config-svc]="opsmesh-config-svc"
    [log-svc]="opsmesh-log-svc"
    [gpu-svc]="opsmesh-gpu-svc"
    [portal-svc]="opsmesh-portal-svc"
    [aio-svc]="opsmesh-aio-svc"
    [mysql]="opsmesh-mysql"
    [redis]="opsmesh-redis"
    [prometheus]="opsmesh-prometheus"
    [grafana]="opsmesh-grafana"
    [loki]="opsmesh-loki"
    [otel-collector]="opsmesh-otel"
)

if [ -n "$SERVICE" ]; then
    container="${CONTAINER_MAP[$SERVICE]:-}"
    if [ -z "$container" ]; then
        echo -e "${RED}Unknown service: ${SERVICE}${NC}"
        echo "Available services: ${!CONTAINER_MAP[*]}"
        exit 1
    fi

    echo -e "${BOLD}Logs: ${SERVICE} (${container})${NC}\n"

    if [ "$FOLLOW" = true ]; then
        docker logs -f --tail="$TAIL_LINES" "$container" 2>&1
    else
        docker logs --tail="$TAIL_LINES" "$container" 2>&1
    fi
else
    # Show all services
    echo -e "${BOLD}All OpsMesh Service Logs${NC}\n"

    local_args=(-f "$COMPOSE_FILE")
    if [ "$FOLLOW" = true ]; then
        docker compose "${local_args[@]}" logs --tail="$TAIL_LINES" -f
    else
        docker compose "${local_args[@]}" logs --tail="$TAIL_LINES"
    fi
fi
