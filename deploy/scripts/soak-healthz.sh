#!/usr/bin/env bash
# 控制面长跑存活采样（issue #97）：周期性采 /healthz 延迟 + pprof goroutine 数，
# 用于「20min/60min 存活」回归判据与卡死取证。
#
# 手工 drill，不接 CI（需要独占机器 + 长时间窗口）。前置：控制面已启动，
# 且开 --debug-pprof 与 --metrics-allow-cidr 仅放开本机（pprof 能读内存/调用栈，禁止公网）。
#
# 判据：goroutine 数若随时间单调增长（>GROW_THRESHOLD）⇒ 疑似泄漏；
#       healthz 连续超时 ⇒ 疑似卡死，立即保留最后一次正常采样 + 卡死期采样供定性。
set -uo pipefail

BASE_URL="${OPSMESH_SOAK_BASE_URL:-http://127.0.0.1:8080}"
DURATION="${OPSMESH_SOAK_DURATION:-1200}"   # 默认 20min（秒）
INTERVAL="${OPSMESH_SOAK_INTERVAL:-30}"     # 采样间隔（秒）
HTTP_TIMEOUT="${OPSMESH_SOAK_HTTP_TIMEOUT:-5}"
GROW_THRESHOLD="${OPSMESH_SOAK_GROW_THRESHOLD:-20}" # goroutine 增长告警阈值
OUT_DIR="${OPSMESH_SOAK_OUT_DIR:-/tmp/opsmesh-soak}"
PPROF_ENABLED="${OPSMESH_SOAK_PPROF:-1}"    # 1=采 goroutine，0=跳过

mkdir -p "$OUT_DIR"
LOG="$OUT_DIR/samples.csv"
GOROUT="$OUT_DIR/goroutine.txt"
: > "$LOG"
: > "$GOROUT"

log() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }
sample_health() {
  local start end code body
  start=$(date +%s%3N)
  body=$(curl -s -m "$HTTP_TIMEOUT" "$BASE_URL/healthz" 2>/dev/null)
  code=$?
  end=$(date +%s%3N)
  if [[ $code -eq 0 && "$body" == *'"status":"ok"'* ]]; then
    printf '%s,%s,ok\n' "$((end-start))" "$(date +%s)" >> "$LOG"
    return 0
  fi
  printf '%s,%s,FAIL\n' "$((end-start))" "$(date +%s)" >> "$LOG"
  log "WARN: /healthz 失败/超时（code=$code body=${body:0:80}）"
  return 1
}
sample_goroutines() {
  [[ "$PPROF_ENABLED" != "1" ]] && return 0
  local n
  n=$(curl -s -m "$HTTP_TIMEOUT" "$BASE_URL/debug/pprof/goroutine?debug=1" 2>/dev/null \
      | grep -oE 'goroutine profile: total [0-9]+' | grep -oE '[0-9]+' | head -1)
  if [[ -n "$n" ]]; then
    printf '%s,%s\n' "$(date +%s)" "$n" >> "$GOROUT"
  fi
}

log "soak 开始：duration=${DURATION}s interval=${INTERVAL}s base=$BASE_URL out=$OUT_DIR"
start_ts=$(date +%s)
consecutive_fail=0
while (( $(date +%s) - start_ts < DURATION )); do
  if sample_health; then consecutive_fail=0; else consecutive_fail=$((consecutive_fail+1)); fi
  sample_goroutines
  if (( consecutive_fail >= 3 )); then
    log "FAIL: /healthz 连续 3 次失败 ⇒ 疑似卡死，保留取证到 $OUT_DIR，请用 pprof/goroutine dump 定性"
    cp "$GOROUT" "$OUT_DIR/goroutine-at-hang.txt"
    exit 1
  fi
  sleep "$INTERVAL"
done

# 趋势判据
log "soak 完成：samples=$(wc -l < "$LOG")"
if [[ "$PPROF_ENABLED" == "1" && -s "$GOROUT" ]]; then
  first=$(head -1 "$GOROUT" | cut -d, -f2)
  last=$(tail -1 "$GOROUT" | cut -d, -f2)
  if [[ -n "$first" && -n "$last" ]]; then
    delta=$(( last - first ))
    log "goroutine: first=$first last=$last delta=$delta threshold=$GROW_THRESHOLD"
    if (( delta > GROW_THRESHOLD )); then
      log "WARN: goroutine 单调增长超阈值 ⇒ 疑似泄漏，请结合 dump 定性"
    fi
  fi
fi
log "Done"
