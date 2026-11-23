#!/usr/bin/env bash
# 为生产 SW E2E 提供真实控制面；仅监听固定环回端口，不读取外部 URL。
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WORK="${TMPDIR:-/tmp}/opsmesh-sw-e2e"
BASE="http://127.0.0.1:4190"
ACTION="${1:?usage: e2e-service-worker-backend.sh start|stop}"

healthy() {
  curl -fsS --max-time 1 "$BASE/healthz" >/dev/null 2>&1
}

stop_backend() {
  if [ -f "$WORK/controlplane.pid" ]; then
    PID="$(cat "$WORK/controlplane.pid")"
    kill "$PID" 2>/dev/null || true
    wait "$PID" 2>/dev/null || true
  fi
  case "$WORK" in
    */opsmesh-sw-e2e) rm -rf -- "$WORK" ;;
    *) echo "拒绝清理异常路径：$WORK" >&2; return 1 ;;
  esac
}

if [ "$ACTION" = "stop" ]; then
  stop_backend
  exit 0
fi
if [ "$ACTION" != "start" ]; then
  echo "未知操作：$ACTION（仅 start/stop）" >&2
  exit 2
fi
if healthy; then
  exit 0
fi

mkdir -p "$WORK"
cd "$ROOT"
BINARY="$WORK/opsmesh"
if [ "$(go env GOOS)" = "windows" ]; then BINARY="$BINARY.exe"; fi
go build -o "$BINARY" ./cmd/opsmesh
go run ./deploy/scripts/license-ci -out-dir "$WORK/license"
"$BINARY" --mode=controlplane --store=memory --demo \
  --http-port=4190 --grpc-port=4191 --metrics-port=4192 \
  --license-public-key="$(cat "$WORK/license/public.key")" \
  --license-key="$(cat "$WORK/license/license.key")" > "$WORK/controlplane.log" 2>&1 &
PID=$!
printf '%s\n' "$PID" > "$WORK/controlplane.pid"
for _ in $(seq 1 120); do
  if healthy; then exit 0; fi
  if ! kill -0 "$PID" 2>/dev/null; then
    echo "控制面启动失败，日志：$WORK/controlplane.log" >&2
    exit 1
  fi
  sleep 0.5
done
echo "控制面启动超时，日志：$WORK/controlplane.log" >&2
exit 1
