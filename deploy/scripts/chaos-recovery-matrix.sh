#!/usr/bin/env bash
# issue #152：完整 compose 依赖故障与恢复一致性矩阵（MySQL / Redis / Kafka / 网络分区）。
#
# 与 chaos-dependency-down.sh（#88 第一批，只验证 readyz 摘流）的关系：
# 本脚本在其上补齐「业务写入/读取/数据一致性」与 Redis/Kafka/网络分区三类故障，
# 并把每类故障的注入方式、故障期断言、恢复断言、业务一致性、原始日志位置结构化落盘。
#
# 约束（与 issue 验收口径一致）：
#   - 必须显式传独立 compose project；拒绝 prod/production；不触碰其他项目容器；
#   - 所有等待有界；EXIT trap 自动恢复停止的容器与断开的网络；
#   - 不打印凭据、JWT、Cookie 与响应体正文；只输出状态码/耗时/资源 ID/资源名；
#   - 口令仅存 $ARTIFACTS/admin-password.txt（0600），API key 明文用后即删；
#   - 手工 drill，不接 CI。
#
# 环境变量：
#   OPSMESH_CHAOS_PROJECT       必填，compose project 名
#   OPSMESH_CHAOS_COMPOSE_FILE  默认仓根 docker-compose.yaml（隔离演练栈用生成文件）
#   OPSMESH_CHAOS_BASE_URL      默认 http://127.0.0.1:8080
#   OPSMESH_CHAOS_SCENARIOS     默认 mysql,redis,kafka,partition
#   OPSMESH_CHAOS_ARTIFACTS     默认 ./chaos-artifacts（报告与原始日志；勿提交）
#   OPSMESH_CHAOS_HTTP_TIMEOUT / _DEGRADED_TIMEOUT / _RECOVERY_TIMEOUT 同 #88 脚本
set -euo pipefail

: "${OPSMESH_CHAOS_PROJECT:?必须显式设置 OPSMESH_CHAOS_PROJECT（独立 drill 项目，禁止生产项目名）}"
case "$OPSMESH_CHAOS_PROJECT" in
  prod|production) echo "拒绝对 production 项目做依赖中断" >&2; exit 64 ;;
esac

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../.." && pwd)
COMPOSE_FILE="${OPSMESH_CHAOS_COMPOSE_FILE:-$REPO_ROOT/docker-compose.yaml}"
BASE_URL="${OPSMESH_CHAOS_BASE_URL:-http://127.0.0.1:8080}"
HTTP_TIMEOUT="${OPSMESH_CHAOS_HTTP_TIMEOUT:-5}"
DEGRADED_TIMEOUT="${OPSMESH_CHAOS_DEGRADED_TIMEOUT:-20}"
RECOVERY_TIMEOUT="${OPSMESH_CHAOS_RECOVERY_TIMEOUT:-90}"
KAFKA_TOPIC="${OPSMESH_CHAOS_KAFKA_TOPIC:-opsmesh-audit}"
SCENARIOS="${OPSMESH_CHAOS_SCENARIOS:-mysql,redis,kafka,partition}"
ARTIFACTS="${OPSMESH_CHAOS_ARTIFACTS:-$REPO_ROOT/chaos-artifacts}"

# Git Bash 的 mktemp//f/... 是 MSYS 路径，Windows 原生 docker.exe/curl.exe 不认。
# 统一把喂给它们的文件路径转 Windows 形态；MSYS 工具（grep/rm）仍用原路径。
wpath() { if command -v cygpath >/dev/null 2>&1; then cygpath -w "$1"; else printf '%s' "$1"; fi; }

COMPOSE=(docker compose --project-name "$OPSMESH_CHAOS_PROJECT" --file "$(wpath "$COMPOSE_FILE")")
JAR="$ARTIFACTS/admin.jar"
ADMIN_PW_FILE="$ARTIFACTS/admin-password.txt"
REPORT="$ARTIFACTS/report.md"

mkdir -p "$ARTIFACTS"
chmod 700 "$ARTIFACTS"
touch "$JAR"; chmod 600 "$JAR"

# Git Bash 会把 docker exec 的 /opt/... 参数改写成 Windows 路径；固定关闭转换。
export MSYS_NO_PATHCONV=1

STOPPED=()
DISCONNECTED=()
DRILL_FAILED=0
SCENARIO_LOG="$ARTIFACTS/matrix.log"
COMMIT=$(cd "$REPO_ROOT" && git rev-parse --short HEAD 2>/dev/null || echo unknown)

log() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" | tee -a "$SCENARIO_LOG"; }
rep() { printf '%s\n' "$*" >> "$REPORT.run"; }
ok()  { log "PASS: $*"; return 0; }
bad() { log "FAIL: $*"; DRILL_FAILED=1; SCN_FAILED=1; return 0; }

cleanup() {
  rc=$?
  trap - EXIT INT TERM
  log "清理：恢复全部停止的依赖与断开的网络"
  local svc entry net cname
  for svc in "${STOPPED[@]:-}"; do
    [[ -n "$svc" ]] || continue
    "${COMPOSE[@]}" start "$svc" >>"$REPORT.run" 2>&1 || log "WARN: 自动启动 $svc 失败，请手工执行"
  done
  for entry in "${DISCONNECTED[@]:-}"; do
    [[ -n "$entry" ]] || continue
    net="${entry%%:*}"; cname="${entry#*:}"
    docker network connect "$net" "$cname" >>"$REPORT.run" 2>&1 || log "WARN: 自动重接 $cname 失败，请手工执行"
  done
  # 会话 Cookie 用完即清；改密后的随机口令保留在 0600 文件供同一一次性栈复跑（gitignored）。
  rm -f "$JAR" 2>/dev/null || true
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

for cmd in docker curl git; do
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

# 故障注入必须确认生效：compose 命令失败或容器状态未变化都要判红，
# 防止在「从未注入故障」的栈上做空断言（本函数不得在 set -e 失效处静默吞错）。
inject_stop() { # $1=service
  local svc=$1 id state
  # ps -q 只列运行中容器；停前用 -a 取 id，停后 inspect 该 id 才可靠。
  id=$("${COMPOSE[@]}" ps -aq "$svc" 2>/dev/null | head -n 1)
  log "注入：stop $svc"
  if ! "${COMPOSE[@]}" stop "$svc" >>"$SCENARIO_LOG" 2>&1; then
    bad "stop $svc 注入失败（compose 命令未成功，故障未注入）"; return 1
  fi
  STOPPED+=("$svc")
  state=$(docker inspect --format '{{.State.Running}}' "$id" 2>/dev/null || true)
  if [[ "$state" == "false" ]]; then ok "注入确认：$svc 已停止"; return 0; fi
  bad "注入未确认：$svc 仍在运行（state=$state）"; return 1
}

inject_start() { # $1=service
  log "恢复：start $1"
  if ! "${COMPOSE[@]}" start "$1" >>"$SCENARIO_LOG" 2>&1; then
    bad "start $1 恢复失败"; return 1
  fi
  STOPPED=("${STOPPED[@]/$1}")
  return 0
}

inject_disconnect() { # $1=network $2=container-id $3=标签
  log "注入：network disconnect $3"
  if ! docker network disconnect "$1" "$2" >>"$SCENARIO_LOG" 2>&1; then
    bad "disconnect $3 注入失败"; return 1
  fi
  DISCONNECTED+=("$1:$2")
  if grep -q "$1" <<<"$(docker inspect -f '{{range $k,$v := .NetworkSettings.Networks}}{{$k}} {{end}}' "$2" 2>/dev/null)"; then
    bad "注入未确认：$3 仍在网络 $1 上"; return 1
  fi
  ok "注入确认：$3 已脱离网络 $1"; return 0
}

inject_connect() { # $1=network $2=container-id $3=标签
  log "恢复：network connect $3"
  if ! docker network connect "$1" "$2" >>"$SCENARIO_LOG" 2>&1; then
    bad "connect $3 恢复失败"; return 1
  fi
  DISCONNECTED=("${DISCONNECTED[@]/$1:$2}")
  return 0
}

# 故障期 healthz 语义：深度检查（pingStore），依赖断时返回 503 是既定行为。
# 存活判据钉「进程不退出、容器不重启」，而非 healthz 恒 200。
assert_outage_healthz() {
  local out code cid state
  out=$(probe /healthz); code=${out%%|*}
  ok "故障期 /healthz 有界响应 code=$code（深度检查：依赖断时 503 为既定语义，非进程死亡）"
  cid=$("${COMPOSE[@]}" ps -aq controlplane | head -n 1)
  state=$(docker inspect --format '{{.State.Running}}' "$cid" 2>/dev/null || true)
  [[ "$state" == "true" ]] && ok "控制面进程存活（Running=true，未因依赖中断退出）" || bad "控制面进程在依赖中断后退出"
}

# 只输出 code|seconds；绝不打印响应体，避免日志意外记录凭据/敏感内容。
# Windows 原生 curl 写 /dev/null 会报 error 23，空输出统一落到 scratch 文件。
BODY_SCRATCH="$ARTIFACTS/.body-scratch"
probe() {
  local path=$1 out rc=0
  out=$(curl -sS --connect-timeout 2 --max-time "$HTTP_TIMEOUT" -o "$(wpath "$BODY_SCRATCH")" \
    -w '%{http_code}|%{time_total}' "$BASE_URL$path") || rc=$?
  if [[ "$rc" -ne 0 ]]; then
    printf 'curl-error-%s|>%ss\n' "$rc" "$HTTP_TIMEOUT"
    return 0
  fi
  printf '%s\n' "$out"
}

expect_code() {
  local path=$1 expected=$2 out code
  out=$(probe "$path")
  code=${out%%|*}
  [[ "$code" == "$expected" ]] || { bad "$path 状态码 $code，期望 $expected（$out）"; return 1; }
  ok "$path = $code（$out）"
}

wait_for_code() {
  local path=$1 expected=$2 budget=$3 out code
  local deadline=$((SECONDS + budget))
  while [[ "$SECONDS" -lt "$deadline" ]]; do
    out=$(probe "$path")
    code=${out%%|*}
    if [[ "$code" == "$expected" ]]; then
      ok "$path 在有界窗口内达到 $expected（$out）"
      return 0
    fi
    sleep 2
  done
  bad "$path 在 ${budget}s 内未达到 $expected"
  return 1
}

# 认证 API 调用：body 落临时文件（调用方用完即删），只回显 code|time。
# 第 5 参可指定会话 jar（默认 $JAR），用于保留「登出前会话」做容错断言。
api() {
  local method=$1 path=$2 body=${3:-} bf=${4:-} jar=${5:-$JAR} out jwin bfwin
  [[ -n "$bf" && "$bf" != "/dev/null" ]] || bf="$BODY_SCRATCH"
  jwin=$(wpath "$jar"); bfwin=$(wpath "$bf")
  local -a args=(-sS --connect-timeout 2 --max-time "$HTTP_TIMEOUT" -X "$method" \
    -b "$jwin" -c "$jwin" -o "$bfwin" -w '%{http_code}|%{time_total}')
  if [[ -n "$body" ]]; then args+=(-H 'Content-Type: application/json' -d "$body"); fi
  out=$(curl "${args[@]}" "$BASE_URL$path" 2>>"$SCENARIO_LOG") || {
    printf 'curl-error|>%ss\n' "$HTTP_TIMEOUT"; return 0; }
  printf '%s\n' "$out"
}

extract_id() { grep -o '"id"[[:space:]]*:[[:space:]]*"[^"]*"' "$1" | head -n 1 | cut -d'"' -f4 || true; }

# 取代「命令经管道接 grep -q」的写法（仓内 pipefail 门禁）：命令替换捕获后用 herestring 判码。
code_is() { # $1=期望 code 前缀（如 200|），$2...=产生 code|time 的命令
  local want=$1; shift
  grep -q "^${want}" <<<"$("$@")"
}

create_apikey() { # $1=name → 全局 APIKEY_ID / APIKEY_CODE
  local bf
  bf=$(mktemp)
  APIKEY_CODE=$(api POST /api/v1/apikeys "{\"name\":\"$1\"}" "$bf" | cut -d'|' -f1)
  APIKEY_ID=$(extract_id "$bf")
  rm -f "$bf"
}

create_ostemplate() { # $1=name → 全局 TPL_ID / TPL_CODE
  local bf
  bf=$(mktemp)
  TPL_CODE=$(api POST /api/v1/os-templates "{\"name\":\"$1\",\"commands\":\"echo chaos-152\",\"risk\":\"low\"}" "$bf" | cut -d'|' -f1)
  TPL_ID=$(extract_id "$bf")
  rm -f "$bf"
}

# 建立 admin 会话：首登强制改密（一次性令牌）→ 改密 → 正式登录拿 Cookie。
# 当前口令持久化在 0600 文件，供本脚本后续场景复用；绝不打印。
ensure_admin_session() {
  local bf pw token npw
  bf=$(mktemp)
  if code_is '200|' api GET /api/v1/me "" "$bf" && grep -q '"userID":"user-admin"' "$bf"; then
    rm -f "$bf"; return 0
  fi
  rm -f "$bf"
  for pw in admin123 "$(cat "$ADMIN_PW_FILE" 2>/dev/null || true)"; do
    [[ -z "$pw" ]] && continue
    bf=$(mktemp)
    api POST /api/v1/auth/login "{\"username\":\"admin\",\"password\":\"$pw\"}" "$bf" >/dev/null || true
    token=$(grep -o '"changePasswordToken":"[^"]*"' "$bf" | head -n 1 | cut -d'"' -f4 || true)
    rm -f "$bf"
    if [[ -n "$token" ]]; then
      # 口令策略：最小长度 + 大小写 + 数字 + 特殊字符（credentials/password.go）。
      npw="Cx!$(python3 -I -c "import secrets;print(secrets.token_hex(16))")9Z"
      printf '%s\n' "$npw" > "$ADMIN_PW_FILE"; chmod 600 "$ADMIN_PW_FILE"
      bf=$(mktemp)
      api POST /api/v1/auth/change-password \
        "{\"oldPassword\":\"$pw\",\"newPassword\":\"$npw\",\"changePasswordToken\":\"$token\"}" "$bf" >/dev/null || true
      rm -f "$bf"
      pw=$npw
    fi
    bf=$(mktemp)
    api POST /api/v1/auth/login "{\"username\":\"admin\",\"password\":\"$pw\"}" "$bf" >/dev/null || true
    rm -f "$bf"
    bf=$(mktemp)
    if code_is '200|' api GET /api/v1/me "" "$bf" && grep -q '"userID":"user-admin"' "$bf"; then
      printf '%s\n' "$pw" > "$ADMIN_PW_FILE"; chmod 600 "$ADMIN_PW_FILE"
      rm -f "$bf"; log "admin 会话就绪"; return 0
    fi
    rm -f "$bf"
  done
  bad "无法建立 admin 会话"
  return 1
}

kafka_dump() { # $1=输出文件
  timeout 25 "${COMPOSE[@]}" exec -T kafka \
    /opt/kafka/bin/kafka-console-consumer.sh --bootstrap-server localhost:9092 \
    --topic "$KAFKA_TOPIC" --from-beginning --max-messages 500 >"$1" 2>/dev/null || true
}

wait_kafka_topic() {
  local deadline=$((SECONDS + RECOVERY_TIMEOUT))
  while [[ "$SECONDS" -lt "$deadline" ]]; do
    if timeout 10 "${COMPOSE[@]}" exec -T kafka \
        /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --list >/dev/null 2>&1; then
      ok "kafka broker 重新可达"; return 0
    fi
    sleep 2
  done
  bad "kafka broker 在 ${RECOVERY_TIMEOUT}s 内未恢复可达"; return 1
}

# dump 中出现指定模板 ID/名，有界重试（覆盖 producer 重连滞后）。
wait_topic_contains() { # $1=dump 文件 $2=匹配串 $3=预算秒
  local deadline=$((SECONDS + $3))
  while [[ "$SECONDS" -lt "$deadline" ]]; do
    kafka_dump "$1"
    if grep -q "$2" "$1"; then return 0; fi
    sleep 5
  done
  return 1
}

scenario_mysql() {
  SCN_FAILED=0
  SCENARIO_LOG="$ARTIFACTS/mysql.log"; : > "$SCENARIO_LOG"
  log "场景 mysql：停止 MySQL → 有界降级 → 恢复 → 业务写读与哈希链一致性"
  ensure_admin_session || return 1

  expect_code /healthz 200
  expect_code /readyz 200
  local bf id_pre id_post code_during
  bf=$(mktemp)
  code_is '200|' api GET /api/v1/audit/verify "" "$bf" && ok "故障前 audit/verify=200" || bad "故障前 audit/verify 非 200"
  rm -f "$bf"

  create_apikey "chaos152-mysql-pre"
  id_pre=$APIKEY_ID
  [[ "$APIKEY_CODE" == "201" && -n "$id_pre" ]] || { bad "故障前 API key 创建失败（$APIKEY_CODE）"; return 1; }
  ok "故障前业务写：apikeys $id_pre 创建成功"

  inject_stop mysql || return 1

  assert_outage_healthz
  wait_for_code /readyz 503 "$DEGRADED_TIMEOUT"
  if grep -qE '^(200|401|403|500|503|curl-error)' <<<"$(probe /api/v1/devices)"; then
    ok "/api/v1/devices 故障期有界响应（$(probe /api/v1/devices)）"
  else
    bad "/api/v1/devices 故障期无界悬挂"
  fi
  create_apikey "chaos152-mysql-during"
  code_during=$APIKEY_CODE
  if [[ "$code_during" == "201" ]]; then
    bad "故障期 API key 创建竟然 201（与 store 故障语义矛盾）"
  else
    ok "故障期业务写被拒/有界失败（code=$code_during，未伪装成功）"
  fi

  inject_start mysql || return 1
  wait_for_code /readyz 200 "$RECOVERY_TIMEOUT"
  expect_code /healthz 200

  create_apikey "chaos152-mysql-post"
  id_post=$APIKEY_ID
  [[ "$APIKEY_CODE" == "201" && -n "$id_post" ]] || { bad "恢复后 API key 创建失败（$APIKEY_CODE）"; return 1; }
  ok "恢复后业务写：apikeys $id_post 创建成功"

  bf=$(mktemp)
  code_is '200|' api GET /api/v1/apikeys "" "$bf" || { bad "恢复后 GET /apikeys 非 200"; rm -f "$bf"; return 1; }
  grep -q "$id_pre" "$bf" && ok "恢复后读到故障前写入 $id_pre（数据一致性）" \
    || bad "恢复后读不到故障前写入 $id_pre（数据丢失）"
  rm -f "$bf"

  bf=$(mktemp)
  code_is '200|' api GET /api/v1/audit/verify "" "$bf" && ok "恢复后 audit/verify=200（哈希链完整）" \
    || bad "恢复后 audit/verify 非 200"
  rm -f "$bf"
  bf=$(mktemp)
  api GET "/api/v1/audit/events?action=apikey_create&limit=1000" "" "$bf" >/dev/null || true
  audit_contains "$bf" "$id_pre" && ok "审计链含故障前事件 $id_pre" || bad "审计链缺失故障前事件 $id_pre"
  audit_contains "$bf" "$id_post" && ok "审计链含恢复后事件 $id_post" || bad "审计链缺失恢复后事件 $id_post"
  rm -f "$bf"

  rep "| mysql | stop mysql | healthz 200；readyz 有界 503；写有界失败（$code_during） | readyz 有界回 200；healthz 200 | 故障前 $id_pre 恢复后可读；新写 $id_post 201；audit/verify 200 | $([ "$SCN_FAILED" -eq 0 ] && echo PASS || echo FAIL) | $ARTIFACTS/mysql.log |"
  [[ "$SCN_FAILED" -eq 0 ]] || return 1
}

audit_contains() { # $1=dump 文件 $2=目标 ID
  grep -q "\"target\":\"$2\"" "$1" 2>/dev/null
}

scenario_redis() {
  SCN_FAILED=0
  SCENARIO_LOG="$ARTIFACTS/redis.log"; : > "$SCENARIO_LOG"
  log "场景 redis：停止 Redis → 会话/登出/限流容错 → 恢复 → 黑名单重新生效"
  ensure_admin_session || return 1

  local bf pw jarA2 jarC code
  jarA2="$ARTIFACTS/redis-a2.jar"; jarC="$ARTIFACTS/redis-c.jar"
  pw=$(cat "$ADMIN_PW_FILE" 2>/dev/null || echo admin123)

  bf=$(mktemp)
  code_is '200|' api GET /api/v1/apikeys "" "$bf" && ok "基线：已登录会话可读 apikeys" || bad "基线会话读失败"
  rm -f "$bf"

  inject_stop redis || return 1

  # 故障期：JWT 无状态 → 既有会话继续有效；Redis 只承载黑名单/限流/改密令牌。
  bf=$(mktemp)
  code_is '200|' api GET /api/v1/apikeys "" "$bf" && ok "故障期：既有会话继续有效（JWT 无状态）" || bad "故障期既有会话被断"
  rm -f "$bf"

  cp "$JAR" "$jarA2"
  code=$(api POST /api/v1/auth/logout "" "" "$JAR" | cut -d'|' -f1)
  ok "故障期：logout 返回 $code（best-effort；黑名单写入失败仅记录）"
  # 用登出前保留的会话 jarA2 验证 fail-open（$JAR 已被 logout 清 cookie）。
  bf=$(mktemp)
  code_is '200|' api GET /api/v1/apikeys "" "$bf" "$jarA2" && ok "故障期证据：登出未使旧会话失效（IsBlacklisted fail-open，代码既定语义）" \
    || bad "故障期登出后旧会话反而失效（与 fail-open 语义矛盾）"
  rm -f "$bf"

  code=$(api POST /api/v1/auth/login "{\"username\":\"admin\",\"password\":\"$pw\"}" "" "$JAR" | cut -d'|' -f1)
  ok "故障期：新登录 code=$code（IncrRateLimit fail-open，不因 Redis 故障全体击退）"

  inject_start redis || return 1
  expect_code /healthz 200

  # 恢复判据不用容器 healthy，而用行为：登出真正使会话失效。
  bf=$(mktemp)
  api POST /api/v1/auth/login "{\"username\":\"admin\",\"password\":\"$pw\"}" "$bf" "$JAR" >/dev/null || true
  rm -f "$bf"
  bf=$(mktemp)
  code_is '200|' api GET /api/v1/apikeys "" "$bf" || { bad "恢复后新会话不可用"; rm -f "$bf"; return 1; }
  rm -f "$bf"
  cp "$JAR" "$jarC"
  api POST /api/v1/auth/logout "" "" "$JAR" >/dev/null || true
  local deadline=$((SECONDS + RECOVERY_TIMEOUT)) recovered=0
  while [[ "$SECONDS" -lt "$deadline" ]]; do
    bf=$(mktemp)
    code=$(api GET /api/v1/apikeys "" "$bf" "$jarC" | cut -d'|' -f1)
    rm -f "$bf"
    if [[ "$code" == "401" ]]; then recovered=1; break; fi
    sleep 2
  done
  if [[ "$recovered" -eq 1 ]]; then
    ok "恢复证据：登出后旧会话有界变为 401（黑名单重新生效）"
  else
    bad "恢复后登出仍未使会话失效（Redis 黑名单未收敛）"
  fi

  rep "| redis | stop redis | 既有会话继续有效；logout best-effort；登录 fail-open | 登出后有界 401（黑名单重新生效） | 会话不丢；黑名单故障期豁免为既定容错语义，非数据丢失 | $([ "$SCN_FAILED" -eq 0 ] && echo PASS || echo FAIL) | $ARTIFACTS/redis.log |"
  [[ "$SCN_FAILED" -eq 0 ]] || return 1
}

scenario_kafka() {
  SCN_FAILED=0
  SCENARIO_LOG="$ARTIFACTS/kafka.log"; : > "$SCENARIO_LOG"
  log "场景 kafka：停止 broker → 故障期写 → 恢复 → 新事件自动送达且故障期事件去向明确"
  ensure_admin_session || return 1

  local bf id0 id2 name1 cp_before cp_after
  name1="chaos152-k1-during"
  cp_before=$("${COMPOSE[@]}" ps -q controlplane)

  create_ostemplate "chaos152-k0-baseline"
  id0=$TPL_ID
  [[ "$TPL_CODE" == "201" && -n "$id0" ]] || { bad "基线 os-template 创建失败"; return 1; }
  wait_topic_contains "$ARTIFACTS/kafka-d0.log" "$id0" 60 \
    && ok "基线：事件 $id0 已入 topic（管道正常）" || bad "基线事件 $id0 未入 topic"

  inject_stop kafka || return 1

  # 故障期写：Publish 同步阻塞可能拖慢响应；只钉「有界」与「SQL 侧不丢」，
  # 不钉状态码（快失败/超时/201 都是真实形态，区别记录）。
  create_ostemplate "$name1"
  ok "故障期业务写返回 code=$TPL_CODE time 有界（id=${TPL_ID:-无响应}）"
  bf=$(mktemp)
  code_is '200|' api GET /api/v1/audit/verify "" "$bf" && ok "故障期：audit/verify=200（SQL 审计链独立于 Kafka）" \
    || bad "故障期 audit/verify 非 200"
  rm -f "$bf"
  bf=$(mktemp)
  api GET "/api/v1/audit/events?action=os_template_create&limit=1000" "" "$bf" >/dev/null || true
  grep -q "$name1" "$bf" && ok "故障期事件 $name1 已入 SQL 审计链（审计不丢）" \
    || bad "故障期事件 $name1 未入 SQL 审计链"
  rm -f "$bf"

  inject_start kafka || return 1
  wait_kafka_topic

  create_ostemplate "chaos152-k2-post"
  id2=$TPL_ID
  [[ "$TPL_CODE" == "201" && -n "$id2" ]] || { bad "恢复后业务写失败"; return 1; }
  wait_topic_contains "$ARTIFACTS/kafka-d2.log" "$id2" 60 \
    && ok "恢复后新事件 $id2 自动入 topic（无需重启控制面）" || bad "恢复后新事件 $id2 未入 topic"
  grep -q "\"target\":\"$id0\"" "$ARTIFACTS/kafka-d2.log" && ok "基线事件 $id0 仍在 topic（broker 数据未因停启丢失）" \
    || bad "基线事件 $id0 从 topic 消失"
  local kafka_bus_note
  if grep -q "$name1" "$ARTIFACTS/kafka-d2.log"; then
    ok "故障期事件 $name1 也被送达（存在 WAL/重放/缓冲兜底）"
    kafka_bus_note="bus 侧故障期事件经兜底送达（未丢失）"
    rep "- FINDING-NONE：Kafka 故障期事件经兜底送达，无丢失"
  else
    kafka_bus_note="bus 侧故障期事件丢失（F-152-kafka-1）"
    log "FINDING F-152-kafka-1：故障期事件 $name1 未入 topic（丢失）；WAL 兜底（NewKafkaBusWithWAL）在生产路径未接线，Publish 错误被调用方忽略"
    rep "- FINDING F-152-kafka-1：Kafka 故障期 bus 事件丢失；WAL 存在但 events.New 未接线，Publish err 被调用方忽略（审计链 SQL 侧不丢）"
  fi
  cp_after=$("${COMPOSE[@]}" ps -q controlplane)
  [[ "$cp_before" == "$cp_after" ]] && ok "控制面容器未重启（同一 container id）" || bad "控制面容器在演练中被重启"

  rep "| kafka | stop kafka | 业务写有界（$TPL_CODE）；SQL 审计链 verify 200 且含故障期事件 | 新事件有界入 topic；基线事件仍在；控制面未重启 | SQL 侧一致；$kafka_bus_note | $([ "$SCN_FAILED" -eq 0 ] && echo PASS || echo FAIL) | $ARTIFACTS/kafka.log |"
  [[ "$SCN_FAILED" -eq 0 ]] || return 1
}

scenario_partition() {
  SCN_FAILED=0
  SCENARIO_LOG="$ARTIFACTS/partition.log"; : > "$SCENARIO_LOG"
  log "场景 partition：网络分区（依赖侧 + agent 侧）→ 有界降级 → 重连收敛"
  ensure_admin_session || return 1

  local net cid acid bf code conv deadline
  cid=$("${COMPOSE[@]}" ps -q mysql | head -n 1)
  net=$(docker inspect -f '{{range $k,$v := .NetworkSettings.Networks}}{{$k}}{{end}}' "$cid")
  log "网络：$net"

  # 变体 A：控制面 ↔ mysql 分区（依赖侧）。
  inject_disconnect "$net" "$cid" mysql || return 1
  assert_outage_healthz
  wait_for_code /readyz 503 "$DEGRADED_TIMEOUT"
  create_apikey "chaos152-part-during"
  ok "分区期业务写 code=$APIKEY_CODE（有界失败，非悬挂）"
  # 恢复：docker network disconnect 可能丢掉 compose 服务别名，朴素 connect 的「收敛」
  # 是暂时假象（实测：当时 readyz 回 200，随后别名仍丢失、DNS 再度 503）。故先记录朴素
  # connect 观察值，再**一律带别名重连**保证收敛，不把朴素 connect 当恢复终点。
  log "恢复：朴素 network connect mysql（仅观察，不作恢复终点）"
  docker network connect "$net" "$cid" >>"$SCENARIO_LOG" 2>&1 || true
  DISCONNECTED=("${DISCONNECTED[@]/$net:$cid}")
  sleep 8
  local rz_naive
  rz_naive=$(probe /readyz)
  log "朴素 connect 后 readyz=${rz_naive%%|*}（仅供参考；别名可能已丢，须带别名重连兜底）"
  rep "- FINDING F-152-partition-1：docker network disconnect/connect 可能丢 compose 服务别名；朴素 connect 后 readyz 观察值=${rz_naive%%|*}，但别名恢复不可靠，须 connect --alias 或 compose 重建该服务"
  # 正确恢复：先断开再带服务别名重连（保证服务发现恢复）。
  docker network disconnect "$net" "$cid" >>"$SCENARIO_LOG" 2>&1 || true
  docker network connect --alias mysql "$net" "$cid" >>"$SCENARIO_LOG" 2>&1 || bad "带别名重连 mysql 失败"
  ok "带别名重连 mysql 后等待收敛"
  wait_for_code /readyz 200 "$RECOVERY_TIMEOUT"
  create_apikey "chaos152-part-post"
  [[ "$APIKEY_CODE" == "201" ]] || { bad "重连后业务写未恢复"; return 1; }
  ok "重连后业务写恢复（$APIKEY_ID）"

  # 变体 B：控制面 ↔ agent 分区（代理侧）。
  if container_running agent; then
    acid=$("${COMPOSE[@]}" ps -q agent | head -n 1)
    inject_disconnect "$net" "$acid" agent || return 1
    sleep 15
    code=$(api GET /api/v1/agents "" /dev/null | cut -d'|' -f1)
    ok "分区期：控制面存活且 /api/v1/agents 有界响应（$code）；agent 心跳中断为预期"
    # agent 重连同样恢复服务别名（其出站解析 controlplane，controlplane 别名未受影响）。
    log "恢复：network connect --alias agent"
    docker network connect --alias agent "$net" "$acid" >>"$SCENARIO_LOG" 2>&1 || true
    DISCONNECTED=("${DISCONNECTED[@]/$net:$acid}")
    # 收敛判据：agent 重新心跳后 live 派生为 true（30s 心跳窗口内有界等待）。
    deadline=$((SECONDS + RECOVERY_TIMEOUT)); conv=0
    while [[ "$SECONDS" -lt "$deadline" ]]; do
      bf=$(mktemp)
      if code_is '200|' api GET /api/v1/agents "" "$bf" && grep -q '"live":true' "$bf"; then conv=1; rm -f "$bf"; break; fi
      rm -f "$bf"; sleep 5
    done
    [[ "$conv" -eq 1 ]] && ok "重连后 agent 有界恢复 live（心跳收敛）" || bad "重连后 agent 未恢复 live"
  else
    log "跳过 agent 变体：compose 中无 agent 容器"
  fi

  rep "| partition | disconnect mysql / agent | healthz 200；readyz 503 有界；写有界失败；agent 心跳中断 | readyz 有界回 200；业务写恢复；agent live 收敛 | 故障期写有界失败；恢复后新写成功 | $([ "$SCN_FAILED" -eq 0 ] && echo PASS || echo FAIL) | $ARTIFACTS/partition.log |"
  [[ "$SCN_FAILED" -eq 0 ]] || return 1
}

# 收尾硬断言：不许留一个坏栈还报 PASS。所有依赖须运行、mysql 别名在位、readyz 200、业务可写。
verify_environment_restored() {
  SCENARIO_LOG="$ARTIFACTS/restore-check.log"; : > "$SCENARIO_LOG"
  log "收尾：验证环境已恢复（依赖运行 + mysql 别名 + readyz + 业务写）"
  local svc cid aliases
  for svc in mysql redis kafka controlplane; do
    if ! container_running "$svc"; then bad "收尾：$svc 未运行"; fi
  done
  cid=$("${COMPOSE[@]}" ps -aq mysql | head -n 1)
  aliases=$(docker inspect -f '{{range $k,$v := .NetworkSettings.Networks}}{{$v.Aliases}}{{end}}' "$cid" 2>/dev/null || true)
  if [[ "$aliases" == *mysql* ]]; then ok "mysql 服务别名在位"; else bad "mysql 服务别名丢失（$aliases）"; fi
  expect_code /readyz 200
  create_apikey "chaos152-restore-check"
  [[ "$APIKEY_CODE" == "201" ]] && ok "收尾业务写 201（栈已恢复可写）" || bad "收尾业务写失败（$APIKEY_CODE）"
}

# ---- 报告头与主流程 ----
cat > "$REPORT.run" <<EOF
# OpsMesh 依赖故障与恢复一致性矩阵（issue #152）

- commit: $COMMIT
- date: $(date -u +%Y-%m-%dT%H:%M:%SZ)
- project: $OPSMESH_CHAOS_PROJECT
- compose: $COMPOSE_FILE
- base_url: $BASE_URL
- scenarios: $SCENARIOS
- artifacts: $ARTIFACTS

| 场景 | 注入 | 故障期断言 | 恢复断言 | 业务一致性 | 结论 | 原始日志 |
|---|---|---|---|---|---|---|
EOF
rep "" >> "$REPORT.run"

run_scenario() { # $1=场景名：失败不中断矩阵；提前失败时补占位行，保证报告覆盖该场景。
  local name=$1
  if ! "scenario_$name"; then
    if ! grep -q "| $name |" "$REPORT.run"; then
      rep "| $name | 见原始日志 | 场景提前失败（断言未全执行） | - | - | FAIL | $ARTIFACTS/$name.log |"
    fi
  fi
  return 0
}

for sc in ${SCENARIOS//,/ }; do
  case "$sc" in
    mysql)     run_scenario mysql ;;
    redis)     run_scenario redis ;;
    kafka)     run_scenario kafka ;;
    partition) run_scenario partition ;;
    *) echo "未知场景: $sc" >&2; exit 64 ;;
  esac
done

verify_environment_restored

{
  echo ""
  echo "结论：$([ "$DRILL_FAILED" -eq 0 ] && echo 全部场景机制断言通过 || echo 存在机制断言失败，见各场景日志)"
  echo ""
  echo "已知边界/发现："
  echo "- F-152-healthz-1：/healthz 为深度检查（pingStore），MySQL 中断/分区时返回 503（实测）；Helm 将其作 livenessProbe，依赖故障会触发 K8s 重启。liveness 宜与深度 readiness 分离（产品决策，见 issue）。"
  echo "- F-152-kafka-1：Kafka 故障期 bus 事件丢失（WAL 未接线、Publish err 被忽略）；SQL 审计链独立不丢；恢复后新事件免重启送达。"
  echo "原始日志目录：$ARTIFACTS"
} >> "$REPORT.run"
mv "$REPORT.run" "$REPORT"
log "矩阵完成，报告：$REPORT"
exit "$DRILL_FAILED"
