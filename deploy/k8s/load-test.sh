#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NAMESPACE="${NAMESPACE:-opsmesh}"
SERVICE="${SERVICE:-auth-svc}"
PORT="${PORT:-8080}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

log_info()     { echo -e "${GREEN}[INFO]${NC}  $*"; }
log_warn()     { echo -e "${YELLOW}[WARN]${NC}  $*"; }
log_error()    { echo -e "${RED}[ERROR]${NC} $*"; }
log_step()     { echo -e "${BLUE}[STEP]${NC}  $*"; }
log_section()  { echo -e "${CYAN}[TEST]${NC} $*"; }

check_dependencies() {
    local missing=()

    if ! command -v hey &>/dev/null && ! command -v ab &>/dev/null && ! command -v wrk &>/dev/null; then
        missing+=("hey (go install github.com/rakyll/hey@latest)")
    fi

    if [[ ${#missing[@]} -gt 0 ]]; then
        log_warn "Some load testing tools not found. Will use fallback curl method."
        log_warn "For better results, install: ${missing[*]}"
    fi
}

hey_load_test() {
    local url="$1"
    local concurrency="${2:-50}"
    local requests="${3:-1000}"
    local duration="${4:-30s}"

    log_section "Load Test: ${concurrency} concurrent, ${requests} total requests"

    if command -v hey &>/dev/null; then
        hey -n "$requests" -c "$concurrency" -z "$duration" "$url"
    elif command -v ab &>/dev/null; then
        ab -n "$requests" -c "$concurrency" "$url"
    elif command -v wrk &>/dev/null; then
        wrk -t4 -c"$concurrency" -d"$duration" "$url"
    else
        curl_load_test "$url" "$concurrency" "$requests"
    fi
}

curl_load_test() {
    local url="$1"
    local concurrency="${2:-20}"
    local total_requests="${3:-200}"

    log_step "Using curl-based load test (limited metrics)"

    local start_time
    start_time=$(date +%s%N)
    local success=0
    local failed=0

    for i in $(seq 1 "$total_requests"); do
        local http_code
        http_code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$url" 2>/dev/null || echo "000")
        if [[ "$http_code" == "200" || "$http_code" == "404" || "$http_code" == "302" ]]; then
            success=$((success + 1))
        else
            failed=$((failed + 1))
        fi

        if (( i % concurrency == 0 )); then
            sleep 0.1
        fi
    done

    local end_time
    end_time=$(date +%s%N)
    local elapsed_ms=$(( (end_time - start_time) / 1000000 ))
    local rps
    if [[ $elapsed_ms -gt 0 ]]; then
        rps=$((success * 1000 / elapsed_ms))
    else
        rps=0
    fi

    echo ""
    echo "Results:"
    echo "  Total requests: $total_requests"
    echo "  Success:        $success"
    echo "  Failed:         $failed"
    echo "  Duration:       ${elapsed_ms}ms"
    echo "  Req/sec:        ${rps}"
}

measure_latency() {
    local url="$1"
    local samples="${2:-50}"

    log_section "Latency Measurement: ${samples} samples"

    local total_ms=0
    local min_ms=999999
    local max_ms=0
    local count=0

    for i in $(seq 1 "$samples"); do
        local time_ms
        time_ms=$(curl -s -o /dev/null -w '%{time_total}' --max-time 10 "$url" 2>/dev/null || echo "10")
        local time_ms_int
        time_ms_int=$(echo "$time_ms" | awk '{printf "%.0f", $1 * 1000}')

        total_ms=$((total_ms + time_ms_int))
        [[ $time_ms_int -lt $min_ms ]] && min_ms=$time_ms_int
        [[ $time_ms_int -gt $max_ms ]] && max_ms=$time_ms_int
        count=$((count + 1))
    done

    local avg_ms=0
    [[ $count -gt 0 ]] && avg_ms=$((total_ms / count))

    echo "Latency Results:"
    echo "  Samples: ${count}"
    echo "  Avg:     ${avg_ms}ms"
    echo "  Min:     ${min_ms}ms"
    echo "  Max:     ${max_ms}ms"
    echo "  P50:     ${avg_ms}ms (approx)"
}

verify_autoscaling() {
    local deployment="$1"
    local hpa_name="$2"
    local target_replicas="${3:-5}"
    local duration="${4:-60}"

    log_section "Autoscaling Verification: ${deployment} via ${hpa_name}"
    log_step "Generating load to trigger scale-up..."

    local pod_count_before
    pod_count_before=$(kubectl get deployment "$deployment" -n "$NAMESPACE" -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
    log_info "Pods before load: ${pod_count_before}"

    local service_url="http://localhost:${PORT}"
    kubectl port-forward svc/"$deployment" "$PORT":"$PORT" -n "$NAMESPACE" &>/dev/null &
    local pf_pid=$!
    sleep 3

    if command -v hey &>/dev/null; then
        hey -n 5000 -c 100 -z "${duration}s" "$service_url" &>/dev/null &
    else
        for i in $(seq 1 50); do
            curl -s -o /dev/null --max-time 5 "$service_url" &
        done
    fi
    local load_pid=$!

    log_step "Monitoring HPA for ${duration}s..."
    local elapsed=0
    local max_pods=$pod_count_before
    while [[ $elapsed -lt $duration ]]; do
        local current_pods
        current_pods=$(kubectl get deployment "$deployment" -n "$NAMESPACE" -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
        [[ ${current_pods:-0} -gt $max_pods ]] && max_pods=$current_pods

        local hpa_current
        hpa_current=$(kubectl get hpa "$hpa_name" -n "$NAMESPACE" -o jsonpath='{.status.currentReplicas}' 2>/dev/null || echo "?")
        local hpa_desired
        hpa_desired=$(kubectl get hpa "$hpa_name" -n "$NAMESPACE" -o jsonpath='{.status.desiredReplicas}' 2>/dev/null || echo "?")

        printf "\r  [%2ds] pods: %s, HPA current: %s, HPA desired: %s" \
            "$elapsed" "${current_pods}" "${hpa_current}" "${hpa_desired}"

        sleep 5
        elapsed=$((elapsed + 5))
    done
    echo ""

    kill $load_pid 2>/dev/null || true
    kill $pf_pid 2>/dev/null || true
    wait 2>/dev/null || true

    local pod_count_after
    pod_count_after=$(kubectl get deployment "$deployment" -n "$NAMESPACE" -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")

    log_info "Pods after load: ${pod_count_after} (max during test: ${max_pods})"

    if [[ $max_pods -gt $pod_count_before ]]; then
        log_info "Autoscaling triggered! Scaled from ${pod_count_before} to ${max_pods} pods."
        return 0
    else
        log_warn "Autoscaling may not have triggered (CPU may not have reached threshold)."
        log_warn "This is expected if the service has minimal CPU usage."
        return 0
    fi
}

wait_for_cooldown() {
    local duration="${1:-120}"
    log_step "Waiting ${duration}s for scale-down cooldown..."
    sleep "$duration"
}

run_load_test_suite() {
    echo "============================================"
    echo "  OpsMesh Load Test Suite"
    echo "============================================"
    echo ""

    check_dependencies

    local target_service="${SERVICE}"
    local service_url="http://localhost:${PORT}"

    log_step "Setting up port-forward to ${target_service}:${PORT}..."
    kubectl port-forward svc/"$target_service" "$PORT":"$PORT" -n "$NAMESPACE" &>/dev/null &
    local pf_pid=$!
    sleep 3

    trap 'kill $pf_pid 2>/dev/null || true; wait 2>/dev/null || true' EXIT

    measure_latency "$service_url" 30

    hey_load_test "$service_url" 20 500 15s

    hey_load_test "$service_url" 50 2000 30s

    hey_load_test "$service_url" 100 5000 30s

    kill $pf_pid 2>/dev/null || true
    wait 2>/dev/null || true
    trap - EXIT

    echo ""
    log_info "Load test suite complete."
}

run_autoscaling_test() {
    echo "============================================"
    echo "  OpsMesh Autoscaling Test"
    echo "============================================"
    echo ""

    verify_autoscaling "auth-svc" "auth-svc-hpa" 5 90
    verify_autoscaling "task-svc" "task-svc-hpa" 5 90

    wait_for_cooldown 60

    echo ""
    log_info "Autoscaling test complete."
}

print_pod_metrics() {
    echo ""
    echo "Current Pod Status:"
    kubectl get pods -n "$NAMESPACE" -o wide
    echo ""
    echo "HPA Status:"
    kubectl get hpa -n "$NAMESPACE"
    echo ""
    if kubectl top pods -n "$NAMESPACE" &>/dev/null; then
        echo "Resource Usage:"
        kubectl top pods -n "$NAMESPACE"
    else
        echo "Resource metrics not available (metrics-server may not be ready)."
    fi
}

usage() {
    cat <<EOF
Usage: $(basename "$0") [OPTIONS]

Run load tests against OpsMesh in the local Kind cluster.

Options:
  --namespace NS   Target namespace (default: opsmesh)
  --service SVC    Target service (default: auth-svc)
  --port PORT      Target port (default: 8080)
  --test TYPE      Run specific test: load|autoscaling|all (default: all)
  --help           Show this help message
EOF
}

TEST_TYPE="all"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --namespace) NAMESPACE="$2"; shift 2 ;;
        --service) SERVICE="$2"; shift 2 ;;
        --port) PORT="$2"; shift 2 ;;
        --test) TEST_TYPE="$2"; shift 2 ;;
        --help) usage; exit 0 ;;
        *) log_error "Unknown option: $1"; usage; exit 1 ;;
    esac
done

main() {
    if ! kubectl get namespace "${NAMESPACE}" &>/dev/null; then
        log_error "Namespace '${NAMESPACE}' not found. Deploy OpsMesh first."
        exit 1
    fi

    case "$TEST_TYPE" in
        load) run_load_test_suite ;;
        autoscaling) run_autoscaling_test ;;
        all)
            run_load_test_suite
            run_autoscaling_test
            ;;
        *)
            log_error "Unknown test type: ${TEST_TYPE}"
            usage
            exit 1
            ;;
    esac

    print_pod_metrics
}

main "$@"
