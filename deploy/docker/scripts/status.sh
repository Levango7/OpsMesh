#!/usr/bin/env bash
# OpsMesh Health Status Dashboard
# Usage: ./scripts/status.sh [--watch]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
COMPOSE_FILE="${PROJECT_DIR}/docker-compose.prod.yml"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
GRAY='\033[0;90m'
NC='\033[0;0m'
BOLD='\033[1m'
DIM='\033[2m'

WATCH_MODE=false
REFRESH_INTERVAL=5

for arg in "$@"; do
    case "$arg" in
        --watch|-w) WATCH_MODE=true ;;
        --help|-h)
            echo "Usage: $0 [--watch]"
            echo "  --watch, -w  Refresh every ${REFRESH_INTERVAL} seconds"
            exit 0
            ;;
    esac
done

cd "$PROJECT_DIR"

print_status() {
    clear 2>/dev/null || true

    echo -e "${BOLD}"
    echo "  ██████╗ ██████╗ ███████╗███╗   ███╗███████╗███████╗██╗  ██╗"
    echo " ██╔═══██╗██╔══██╗██╔════╝████╗ ████║██╔════╝██╔════╝██║  ██║"
    echo " ██║   ██║██████╔╝█████╗  ██╔████╔██║███████╗███████╗███████║"
    echo " ██║   ██║██╔═══╝ ██╔══╝  ██║╚██╔╝██║╚════██║██╔══╝██╔══██║"
    echo " ╚██████╔╝██║     ███████╗██║ ╚═╝ ██║███████║███████╗██║  ██║"
    echo "  ╚═════╝ ╚═╝     ╚══════╝╚═╝     ╚═╝╚══════╝╚══════╝╚═╝  ╚═╝"
    echo -e "${NC}"
    echo -e "  ${CYAN}Health Dashboard${NC}  ${GRAY}$(date '+%Y-%m-%d %H:%M:%S')${NC}\n"

    # Service Health Table
    echo -e "${BOLD}┌──────────────────────┬────────────┬─────────────────┬────────────┐${NC}"
    echo -e "${BOLD}│ Service              │ Status     │ Port            │ Uptime     │${NC}"
    echo -e "${BOLD}├──────────────────────┼────────────┼─────────────────┼────────────┤${NC}"

    local services=(
        "opsmesh-mysql:MySQL:3306"
        "opsmesh-redis:Redis:6379"
        "opsmesh-controlplane:Controlplane:8080"
        "opsmesh-auth-svc:Auth Svc:8100"
        "opsmesh-device-svc:Device Svc:8101"
        "opsmesh-task-svc:Task Svc:8102"
        "opsmesh-alert-svc:Alert Svc:8103"
        "opsmesh-config-svc:Config Svc:8106"
        "opsmesh-log-svc:Log Svc:8105"
        "opsmesh-gpu-svc:GPU Svc:8107"
        "opsmesh-portal-svc:Portal Svc:8109"
        "opsmesh-aio-svc:AIO Svc:8108"
        "opsmesh-prometheus:Prometheus:9092"
        "opsmesh-grafana:Grafana:3000"
        "opsmesh-loki:Loki:3100"
        "opsmesh-otel:OTel Collector:4317"
    )

    local running=0
    local unhealthy=0
    local stopped=0

    for svc_entry in "${services[@]}"; do
        IFS=':' read -r name label port <<< "$svc_entry"

        local status
        status=$(docker inspect --format='{{.State.Status}}' "$name" 2>/dev/null || echo "missing")
        local health="─"
        health=$(docker inspect --format='{{if .State.Health}}{{.State.Health.Status}}{{else}}─{{end}}' "$name" 2>/dev/null || echo "─")
        local uptime="─"
        uptime=$(docker inspect --format='{{.State.StartedAt}}' "$name" 2>/dev/null || echo "")

        local status_color
        local health_icon

        case "$status" in
            running)
                ((running++))
                status_color="${GREEN}"
                case "$health" in
                    healthy)   health_icon="${GREEN}♥ healthy${NC}" ;;
                    starting)  health_icon="${YELLOW}◌ starting${NC}" ;;
                    unhealthy) health_icon="${RED}✗ unhealthy${NC}"; ((unhealthy++)) ;;
                    *)         health_icon="${GREEN}● running${NC}" ;;
                esac
                ;;
            exited|dead)
                status_color="${RED}"
                health_icon="${RED}✗ ${status}${NC}"
                ((stopped++))
                ;;
            *)
                status_color="${GRAY}"
                health_icon="${GRAY}○ not found${NC}"
                ((stopped++))
                ;;
        esac

        # Calculate uptime string
        if [ -n "$uptime" ] && [ "$uptime" != "─" ] && [ "$status" = "running" ]; then
            local started_epoch
            started_epoch=$(date -d "$uptime" +%s 2>/dev/null || python3 -c "from datetime import datetime; print(int(datetime.fromisoformat('${uptime}'.replace('Z','+00:00')).timestamp()))" 2>/dev/null || echo "0")
            local now_epoch
            now_epoch=$(date +%s)
            local diff=$((now_epoch - started_epoch))
            if [ $diff -ge 86400 ]; then
                uptime="$((diff / 86400))d $(((diff % 86400) / 3600))h"
            elif [ $diff -ge 3600 ]; then
                uptime="$((diff / 3600))h $(((diff % 3600) / 60))m"
            elif [ $diff -ge 60 ]; then
                uptime="$((diff / 60))m"
            else
                uptime="${diff}s"
            fi
        fi

        printf "│ ${BOLD}%-20s${NC} │ ${status_color}%-10s${NC} │ %-15s │ %-10s │\n" "$label" "$status" "$port" "$uptime"
        echo -e "│                      │ ${health_icon}  │                 │            │"
    done

    echo -e "${BOLD}└──────────────────────┴────────────┴─────────────────┴────────────┘${NC}"

    # Summary
    echo ""
    echo -e "  ${GREEN}● Running: ${running}${NC}  ${RED}● Stopped: ${stopped}${NC}  ${YELLOW}● Unhealthy: ${unhealthy}${NC}"

    # Resource Usage
    echo -e "\n${BOLD}Resource Usage:${NC}"
    echo -e "${DIM}  $(docker stats --no-stream --format "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}\t{{.NetIO}}" 2>/dev/null | head -1)${NC}"
    docker stats --no-stream --format "  {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}\t{{.NetIO}}" 2>/dev/null || echo -e "  ${GRAY}(docker stats unavailable)${NC}"

    # Disk usage for volumes
    echo -e "\n${BOLD}Volume Usage:${NC}"
    docker system df --format "  {{.Type}}: {{.Size}} ({{.Reclaimable}} reclaimable)" 2>/dev/null | head -5 || echo -e "  ${GRAY}(volume info unavailable)${NC}"

    # Recent errors (last 5 errors across all containers)
    echo -e "\n${BOLD}Recent Errors (last 10 min):${NC}"
    local recent_errors
    recent_errors=$(docker events --since 10m --until 0s --filter event=die --filter event=oom --filter event=health_status 2>/dev/null | head -5)
    if [ -n "$recent_errors" ]; then
        echo "$recent_errors" | while IFS= read -r line; do
            echo -e "  ${RED}${line}${NC}"
        done
    else
        echo -e "  ${GREEN}No recent errors${NC}"
    fi

    echo ""
    if [ "$WATCH_MODE" = true ]; then
        echo -e "${DIM}Refreshing every ${REFRESH_INTERVAL}s (Ctrl+C to exit)${NC}"
    fi
}

if [ "$WATCH_MODE" = true ]; then
    while true; do
        print_status
        sleep "$REFRESH_INTERVAL"
    done
else
    print_status
fi
