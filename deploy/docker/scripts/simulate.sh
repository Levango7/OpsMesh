#!/usr/bin/env bash
# OpsMesh Device Simulator Launcher
# Usage: ./scripts/simulate.sh [N] [interval] [batch]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
OPS_MESH_DIR="$(dirname "$PROJECT_DIR")"
SIMULATOR_BIN="${OPS_MESH_DIR}/cmd/device-sim/device-sim"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'
BOLD='\033[1m'

NUM_DEVICES="${1:-10}"
HEARTBEAT_INTERVAL="${2:-10s}"
BATCH_SIZE="${3:-5}"
CONTROLPLANE_URL="${CONTROLPLANE_URL:-http://localhost:8080}"

for arg in "$@"; do
    case "$arg" in
        --help|-h)
            echo "Usage: $0 [N] [interval] [batch]"
            echo ""
            echo "Arguments:"
            echo "  N         Number of simulated devices (default: 10)"
            echo "  interval  Heartbeat interval (default: 10s)"
            echo "  batch     Batch size for registration (default: 5)"
            echo ""
            echo "Environment:"
            echo "  CONTROLPLANE_URL  API endpoint (default: http://localhost:8080)"
            exit 0
            ;;
    esac
done

echo -e "${BOLD}${CYAN}OpsMesh Device Simulator${NC}\n"
echo -e "  Devices:   ${GREEN}${NUM_DEVICES}${NC}"
echo -e "  Interval:  ${GREEN}${HEARTBEAT_INTERVAL}${NC}"
echo -e "  Batch:     ${GREEN}${BATCH_SIZE}${NC}"
echo -e "  Target:    ${GREEN}${CONTROLPLANE_URL}${NC}\n"

cd "$OPS_MESH_DIR"

# Build if needed
if [ ! -f "$SIMULATOR_BIN" ]; then
    log_info "Building device simulator..."
    mkdir -p "$(dirname "$SIMULATOR_BIN")"
    go build -o "$SIMULATOR_BIN" ./cmd/device-sim/
    log_ok "Simulator built: ${SIMULATOR_BIN}"
fi

# Check if controlplane is reachable
if curl -sf "${CONTROLPLANE_URL}/healthz" &>/dev/null; then
    echo -e "${GREEN}✓${NC} Controlplane is reachable"
else
    echo -e "${YELLOW}!${NC} Controlplane not reachable at ${CONTROLPLANE_URL}"
    echo -e "  ${DIM}Devices will retry connection${NC}"
fi

echo ""
echo -e "${BOLD}Starting simulation (Ctrl+C to stop)...${NC}\n"

export CONTROLPLANE_URL
export NUM_DEVICES
export HEARTBEAT_INTERVAL
export BATCH_SIZE

"$SIMULATOR_BIN" \
    -devices="$NUM_DEVICES" \
    -interval="$HEARTBEAT_INTERVAL" \
    -batch="$BATCH_SIZE" \
    -url="$CONTROLPLANE_URL"
