#!/usr/bin/env bash
# OpsMesh load baseline wrapper: builds the controlplane, runs it in demo/memory
# mode on a free port, executes scripts/loadtest/loadgen.go, appends one row to
# scripts/loadtest/BASELINE.md, and always stops the server.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
cd "$ROOT_DIR"

VUS="${VUS:-50}"
DURATION="${DURATION:-30s}"
TENANT="${TENANT:-default}"
TIMEOUT="${TIMEOUT:-5s}"
THINK="${THINK:-1s}"
TOKEN="${TOKEN:-}"
BASE_PORT="${PORT:-18080}"

TMP_DIR="$(mktemp -d)"
CP_PID=""
# shellcheck disable=SC2317,SC2329  # cleanup 经 trap EXIT 间接调用，shellcheck 静态分析看不到调用链，误报"命令不可达/函数未调用"
cleanup() {
    if [[ -n "$CP_PID" ]] && kill -0 "$CP_PID" 2>/dev/null; then
        kill "$CP_PID" 2>/dev/null || true
        wait "$CP_PID" 2>/dev/null || true
    fi
    rm -rf "$TMP_DIR"
}
trap cleanup EXIT

port_busy() {
    (exec 3<>"/dev/tcp/127.0.0.1/$1") >/dev/null 2>&1
}

pick_port() {
    local p="$1"
    while port_busy "$p"; do
        p=$((p + 1))
    done
    printf '%s' "$p"
}

HTTP_PORT="$(pick_port "$BASE_PORT")"
GRPC_PORT="$(pick_port $((HTTP_PORT + 1)))"
METRICS_PORT="$(pick_port $((GRPC_PORT + 1)))"

echo "== OpsMesh loadgen baseline =="
echo "repo=$ROOT_DIR vus=$VUS duration=$DURATION http_port=$HTTP_PORT"

echo "== building controlplane =="
go build -o "$TMP_DIR/controlplane.exe" ./cmd/opsmesh

echo "== starting controlplane (--demo --store=memory) =="
"$TMP_DIR/controlplane.exe" \
    --mode=controlplane \
    --demo \
    --store=memory \
    --task-svc-grpc-addr= \
    --http-port="$HTTP_PORT" \
    --grpc-port="$GRPC_PORT" \
    --metrics-port="$METRICS_PORT" \
    >"$TMP_DIR/controlplane.log" 2>&1 &
CP_PID=$!

ready=0
for _ in $(seq 1 60); do
    if ! kill -0 "$CP_PID" 2>/dev/null; then
        echo "controlplane exited before becoming ready" >&2
        tail -n 40 "$TMP_DIR/controlplane.log" >&2 || true
        exit 1
    fi
    if curl -fsS --max-time 1 "http://127.0.0.1:$HTTP_PORT/readyz" >/dev/null 2>&1; then
        ready=1
        break
    fi
    sleep 0.5
done
if [[ "$ready" -ne 1 ]]; then
    echo "controlplane did not become ready within 30s" >&2
    tail -n 40 "$TMP_DIR/controlplane.log" >&2 || true
    exit 1
fi

echo "== running loadgen =="
if [[ -z "$TOKEN" ]]; then
    LOGIN_RESP="$(curl -fsS --max-time 5 -H 'Content-Type: application/json' \
        -d '{"username":"admin","password":"admin123"}' \
        "http://127.0.0.1:$HTTP_PORT/api/v1/auth/login" 2>/dev/null || true)"
    TOKEN="$(printf '%s' "$LOGIN_RESP" | sed -n 's/.*"token"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
    if [[ -z "$TOKEN" ]]; then
        CPT="$(printf '%s' "$LOGIN_RESP" | sed -n 's/.*"changePasswordToken"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
        if [[ -n "$CPT" ]]; then
            CHANGE_RESP="$(curl -fsS --max-time 5 -H 'Content-Type: application/json' \
                -d "{\"oldPassword\":\"admin123\",\"newPassword\":\"Loadgen!Baseline2026\",\"changePasswordToken\":\"$CPT\"}" \
                "http://127.0.0.1:$HTTP_PORT/api/v1/auth/change-password" 2>/dev/null || true)"
            TOKEN="$(printf '%s' "$CHANGE_RESP" | sed -n 's/.*"token"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
        fi
    fi
    if [[ -n "$TOKEN" ]]; then
        echo "demo admin token acquired"
    else
        echo "warning: demo login failed; protected endpoints may return 401" >&2
    fi
fi

ARGS=(
    -base "http://127.0.0.1:$HTTP_PORT"
    -tenant "$TENANT"
    -vus "$VUS"
    -duration "$DURATION"
    -timeout "$TIMEOUT"
    -think "$THINK"
)
if [[ -n "$TOKEN" ]]; then
    ARGS+=(-token "$TOKEN")
fi

OUT_FILE="$TMP_DIR/loadgen.out"
set +e
go run "$SCRIPT_DIR/loadgen.go" "${ARGS[@]}" 2>&1 | tee "$OUT_FILE"
RC=${PIPESTATUS[0]}
set -e

OVERALL_ROW="$(grep -E '^\| overall \|' "$OUT_FILE" | tail -n 1 || true)"
if [[ -z "$OVERALL_ROW" ]]; then
    echo "loadgen produced no overall row (exit $RC)" >&2
    tail -n 40 "$TMP_DIR/controlplane.log" >&2 || true
    exit 1
fi

DATE_UTC="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
SHA="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
if ! git diff --quiet 2>/dev/null || ! git diff --cached --quiet 2>/dev/null; then
    SHA="$SHA-dirty"
fi
if [[ "$RC" -eq 0 ]]; then
    VERDICT="PASS"
else
    VERDICT="FAIL"
fi
NOTE="demo memory :$HTTP_PORT ${DURATION} think=$THINK (bash)"

BASELINE="$SCRIPT_DIR/BASELINE.md"
if [[ ! -f "$BASELINE" ]]; then
    cat >"$BASELINE" <<'EOF'
# OpsMesh 控制面负载基线

> 由 `scripts/loadtest/run-loadgen.sh` / `run-loadgen.ps1` 自动追加。
> 环境：单机 controlplane `--demo --store=memory`，压测进程与目标同机。
> 判据与 k6 基线一致：overall p99 < 300ms 且错误率 < 0.1%（2xx/3xx 视为成功）。

| 日期 (UTC) | commit | 场景 | VUs | RPS | p50 (ms) | p95 (ms) | p99 (ms) | 错误率 | 结论 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
EOF
fi

ROW="$(awk -F'|' -v date="$DATE_UTC" -v sha="$SHA" -v vus="$VUS" -v verdict="$VERDICT" -v note="$NOTE" '
    /^\| overall \|/ {
        for (i = 1; i <= NF; i++) gsub(/^[ \t]+|[ \t]+$/, "", $i)
        printf "| %s | %s | loadgen | %s | %s | %s | %s | %s | %.3f%% | %s | %s |\n",
            date, sha, vus, $4, $5, $6, $7, $8 * 100, verdict, note
    }
' "$OUT_FILE" | tail -n 1)"

if [[ -z "$ROW" ]]; then
    echo "failed to build BASELINE.md row" >&2
    exit 1
fi
printf '%s\n' "$ROW" >>"$BASELINE"
echo
echo "appended: $ROW"

exit "$RC"
