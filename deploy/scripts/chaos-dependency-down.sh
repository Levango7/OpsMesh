#!/usr/bin/env bash
# 第一批依赖故障注入（issue #88）：compose 形态 mysql 停依赖→验证→恢复。
# 手工 drill，不接 CI；必须显式给 project，防止误停他线/生产容器。
set -euo pipefail

SERVICE="${1:-mysql}"
if [[ "$SERVICE" != "mysql" ]]; then
  echo "第一批仅支持 mysql；Redis 无 /readyz 依赖判据，不能套用本脚本" >&2
  exit 64
fi

: "${OPSMESH_CHAOS_PROJECT:?必须显式设置 OPSMESH_CHAOS_PROJECT（建议独立 drill 项目，禁止使用生产项目名）}"
if [[ "$OPSMESH_CHAOS_PROJECT" == "prod" || "$OPSMESH_CHAOS_PROJECT" == "production" ]]; then
  echo "拒绝对 production 项目做依赖中断" >&2
  exit 64
fi

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../.." && pwd)
COMPOSE_FILE="${OPSMESH_CHAOS_COMPOSE_FILE:-$REPO_ROOT/docker-compose.yaml}"
BASE_URL="${OPSMESH_CHAOS_BASE_URL:-http://127.0.0.1:8080}"
HTTP_TIMEOUT="${OPSMESH_CHAOS_HTTP_TIMEOUT:-5}"
RECOVERY_TIMEOUT="${OPSMESH_CHAOS_RECOVERY_TIMEOUT:-90}"
DEGRADED_TIMEOUT="${OPSMESH_CHAOS_DEGRADED_TIMEOUT:-20}"
COMPOSE=(docker compose --project-name "$OPSMESH_CHAOS_PROJECT" --file "$COMPOSE_FILE")
STOPPED=0

log() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }
fail() { log "FAIL: $*"; return 1; }

cleanup() {
  rc=$?
  trap - EXIT INT TERM
  if [[ "$STOPPED" -eq 1 ]]; then
    log "恢复（EXIT trap）：启动 $SERVICE"
    "${COMPOSE[@]}" start "$SERVICE" >/dev/null 2>&1 || log "WARN: 自动启动失败，请手工执行 docker compose start $SERVICE"
  fi
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

for cmd in docker curl; do
  command -v "$cmd" >/dev/null 2>&1 || { echo "缺少命令: $cmd" >&2; exit 69; }
done
[[ -f "$COMPOSE_FILE" ]] || { echo "compose 文件不存在: $COMPOSE_FILE" >&2; exit 66; }

container_running() {
  local id state
  id=$("${COMPOSE[@]}" ps -q "$1" 2>/dev/null | head -n 1)
  [[ -n "$id" ]] || return 1
  state=$(docker inspect --format '{{.State.Running}}' "$id" 2>/dev/null || true)
  [[ "$state" == "true" ]]
}

# 只输出 code|seconds；不打印响应体，避免 drill 日志意外记录凭据/敏感内容。
probe() {
  local path="$1" out rc=0
  out=$(curl -sS --connect-timeout 2 --max-time "$HTTP_TIMEOUT" -o /dev/null \
    -w '%{http_code}|%{time_total}' "$BASE_URL$path") || rc=$?
  if [[ "$rc" -ne 0 ]]; then
    printf 'curl-error-%s|>%ss\n' "$rc" "$HTTP_TIMEOUT"
    return "$rc"
  fi
  printf '%s\n' "$out"
}

expect_code() {
  local path="$1" expected="$2" out code
  out=$(probe "$path") || { fail "$path 请求失败/超过 ${HTTP_TIMEOUT}s（$out）"; return 1; }
  code=${out%%|*}
  [[ "$code" == "$expected" ]] || { fail "$path 状态码 $code，期望 $expected（$out）"; return 1; }
  log "PASS: $path = $code（$out）"
}

wait_for_code() {
  local path="$1" expected="$2" budget="$3" out code
  local deadline=$((SECONDS + budget))
  while [[ "$SECONDS" -lt "$deadline" ]]; do
    if out=$(probe "$path"); then
      code=${out%%|*}
      if [[ "$code" == "$expected" ]]; then
        log "PASS: $path 在有界窗口内达到 $expected（$out）"
        return 0
      fi
    fi
    sleep 2
  done
  fail "$path 在 ${budget}s 内未达到 $expected"
}

log "预检：project=$OPSMESH_CHAOS_PROJECT service=$SERVICE base=$BASE_URL"
container_running controlplane || { fail "controlplane 未运行；拒绝在不明栈上施工"; exit 1; }
container_running "$SERVICE" || { fail "$SERVICE 未运行；请先以同一 project 启动依赖"; exit 1; }

expect_code /healthz 200
expect_code /readyz 200

log "注入：停止 $SERVICE"
"${COMPOSE[@]}" stop "$SERVICE"
STOPPED=1

# 依赖故障必须表现为 readiness 摘流量，而不是进程死亡或请求无界悬挂。
expect_code /healthz 200
wait_for_code /readyz 503 "$DEGRADED_TIMEOUT"
if out=$(probe /api/v1/devices); then
  # 认证态不同可返回 200/401/403/5xx；这里只钉有界响应，不把鉴权结果当依赖故障。
  log "PASS: /api/v1/devices 在 ${HTTP_TIMEOUT}s 内有响应（$out）"
else
  fail "/api/v1/devices 在依赖中断后悬挂/无响应（$out）"
  exit 1
fi

log "恢复：启动 $SERVICE"
"${COMPOSE[@]}" start "$SERVICE"
wait_for_code /readyz 200 "$RECOVERY_TIMEOUT"
STOPPED=0
expect_code /healthz 200
log "PASS: $SERVICE 故障注入与恢复完成"
