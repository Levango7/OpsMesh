#!/usr/bin/env bash
# drill-batch0.sh — TD-60 auth 收敛「批次 0」演练台（真进程 + 真 job + 模拟账号）。
#
# 与 smoke-cutover.sh 的分工：
#   smoke-cutover.sh   —— 机制面证据（一次性跑完 45 项判据后退出）
#   本脚本             —— **演练台**：把两个进程与名册**留在原地**，供人/agent 以真实
#                          用户身份反复操作（登录、保持会话、被切、被回退），并可分步执行
#                          真实的 cutover-migrate job。批次真正开切前用它把流程走一遍。
#
# 用法：
#   bash deploy/scripts/drill-batch0.sh start [N]     起栈 + 本地建 N 个演练账号（默认 6）
#   bash deploy/scripts/drill-batch0.sh migrate u1,u2  对指定账号跑真 job（一次性口令 → 文件）
#   bash deploy/scripts/drill-batch0.sh unroute u1     单用户回退（从名册摘除，热加载 ≤30s）
#   bash deploy/scripts/drill-batch0.sh fault [user]    引擎级故障注入（击杀对侧 / 锁定副作用 / store 故障步骤）
#   bash deploy/scripts/drill-batch0.sh status         名册/两侧账号/路由日志现状
#   bash deploy/scripts/drill-batch0.sh coords         打印坐标（URL/账号/口令/路径）供外部 actor 使用
#   bash deploy/scripts/drill-batch0.sh stop           停两个进程
#
# 端口：控制面 19580/19581/19582、auth-svc 19590/19591（与 smoke 同段，刻意避开本机 dev 栈）。
# 数据：$TMPDIR/opsmesh-batch0-drill 存日志/名册/凭据；**账号数据在同机 MySQL 容器**
#       $DRILL_MYSQL_CONTAINER（默认 opsmesh-mysql-evidence-v2，库 opsmesh_test，start 时重建）。
#       为什么不用 memory：迁移 job 是独立进程，memory 后端下它看不到控制面建的账号。
set -u

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT="${TMPDIR:-/tmp}/opsmesh-batch0-drill"
EXE=""
case "$(uname -s)" in MINGW*|MSYS*|CYGWIN*) EXE=".exe";; esac
CPBIN="$OUT/drill-cp$EXE"
ASBIN="$OUT/drill-as$EXE"
CP=http://127.0.0.1:19580
AS=http://127.0.0.1:19590
ROSTER_FILE="$OUT/roster.txt"
SECRET="drill-shared-hmac-secret-0123456789abcdef"
ADMINPASS='Drill-Admin-2026x!'
ADMINPASS2='Drill-Admin-2026y!'
USERPASS='Drill-User-2026x!'
# 共享库：job 是独立进程，memory 后端看不到控制面建的本侧账号 ⇒ 演练必须用真库
# （与 runbook 的生产形态一致）。容器名沿用小仓既有的 evidence 库。
MYSQL_C=${DRILL_MYSQL_CONTAINER:-opsmesh-mysql-evidence-v2}
MYSQL_DB=opsmesh_test
MYSQL_DSN="root:rootpass@tcp(127.0.0.1:3306)/${MYSQL_DB}?parseTime=true"

mkdir -p "$OUT"
jget(){ python -c "import json,sys; d=json.load(sys.stdin); print(d.get('$1',''))" 2>/dev/null; }
routes(){ grep -c 'msg":"切流路由' "$OUT/cp.log" 2>/dev/null || echo 0; }

# bootstrap_far_admin 把对侧 admin 从首登改密态引导到正式态（ADMINPASS → ADMINPASS2）。
# 2026-10-14 fault 段实测教训：auth-svc 是 **memory 后端**——重启即失忆（对侧 admin 的
# 改密状态、migrate 建的对侧账号全部丢失）。fault 的"恢复"步骤必须重跑本函数，
# 否则 cutover-migrate 拿 ADMINPASS2 登录会被 401 拒掉（本台实测踩中）。
# 名册用户在重启后也需重新 migrate（对侧账号已丢）——演练报告已把这条写入故障步骤。
bootstrap_far_admin(){
  local J2 CPT2
  J2=$(curl -s -H 'Content-Type: application/json' -X POST "$AS/api/v1/auth/login" \
    -d "{\"username\":\"admin\",\"password\":\"$ADMINPASS\"}")
  CPT2=$(printf '%s' "$J2" | jget changePasswordToken)
  if [ -n "$CPT2" ]; then
    curl -s -o /dev/null -X POST "$AS/api/v1/auth/change-password" -H 'Content-Type: application/json' \
      -d "{\"oldPassword\":\"$ADMINPASS\",\"newPassword\":\"$ADMINPASS2\",\"changePasswordToken\":\"$CPT2\"}"
    echo "  ✓ 对侧 admin 已改密（迁移 job 用 ADMINPASS2 登录对侧）"
  else
    echo "  （对侧 admin 无需改密或已改过）"
  fi
}

kill_pid(){
  if command -v taskkill >/dev/null 2>&1; then
    MSYS_NO_PATHCONV=1 taskkill /PID "$1" /F >/dev/null 2>&1 || true
  else
    kill "$1" >/dev/null 2>&1 || true
  fi
}
purge(){
  for img in "drill-cp$EXE" "drill-as$EXE"; do
    if command -v taskkill >/dev/null 2>&1; then
      MSYS_NO_PATHCONV=1 taskkill /IM "$img" /F >/dev/null 2>&1 || true
    else
      pkill -f "$img" >/dev/null 2>&1 || true
    fi
  done
}
port_free(){ ! curl -s -o /dev/null --max-time 2 "$CP/api/v1/auth/me" >/dev/null 2>&1; }
wait_port_free(){
  local n=0
  while [ "$n" -lt "${1:-20}" ]; do port_free && return 0; sleep 1; n=$((n+1)); done
  return 1
}
wait_cp_up(){
  local n=0 code
  while [ "$n" -lt "${1:-20}" ]; do
    code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 2 "$CP/api/v1/auth/me" 2>/dev/null || true)
    [ "$code" = "401" ] && return 0
    sleep 1; n=$((n+1))
  done
  return 1
}

cmd_start(){
  local n="${1:-6}"
  echo "== 清残 + 建二进制 =="
  purge; kill -0 "${CP_PID:-0}" 2>/dev/null && kill_pid "${CP_PID:-0}"
  sleep 1
  wait_port_free 15 || { echo "❌ 端口仍被占用，先手工清理"; exit 2; }
  (cd "$ROOT" && go build -o "$CPBIN" ./cmd/opsmesh) || exit 1
  (cd "$ROOT/services/auth-svc" && go build -o "$ASBIN" ./cmd/auth-svc) || exit 1
  rm -f "$OUT"/*.log "$OUT"/*.jar "$OUT"/*.json
  : > "$ROSTER_FILE"     # 起始：空名册 = 全部走本地（切流前的常态）

  echo "== 重置演练库（$MYSQL_C / $MYSQL_DB）=="
  docker exec "$MYSQL_C" mysql -uroot -prootpass \
    -e "DROP DATABASE IF EXISTS $MYSQL_DB; CREATE DATABASE $MYSQL_DB;" 2>&1 | tail -2
  # 等库可用（mysql 容器刚 drop/create 后偶发握手未就绪）
  local t=0
  until docker exec "$MYSQL_C" mysql -uroot -prootpass -e "USE $MYSQL_DB;" >/dev/null 2>&1; do
    t=$((t+1)); [ "$t" -gt 20 ] && { echo "❌ 库不可用"; exit 2; }; sleep 1
  done

  echo "== 起 auth-svc =="
  AUTH_SVC_HTTP_ENABLED=true AUTH_SVC_HTTP_PORT=19590 AUTH_SVC_GRPC_PORT=19591 \
  AUTH_SVC_JWT_SECRET="$SECRET" AUTH_SVC_STORE_TYPE=memory AUTH_SVC_PUBLIC_REGISTER=true \
  AUTH_SVC_ADMIN_PASSWORD="$ADMINPASS" AUTH_SVC_DEVICE_FP_ENABLED=false \
    "$ASBIN" > "$OUT/as.log" 2>&1 &
  AS_PID=$!
  sleep 2

  echo "== 起控制面（名册用文件来源）=="
  OPSMESH_JWT_SECRET="$SECRET" OPSMESH_ADMIN_PASSWORD="$ADMINPASS" OPSMESH_HTTP_TLS=off \
  AUTH_SVC_URL="$AS" AUTH_SVC_PROXY_ENABLED=true AUTH_CUTOVER_ROSTER_FILE="$ROSTER_FILE" \
    "$CPBIN" --http-port 19580 --grpc-port 19581 --metrics-port 19582 \
      --store mysql --mysql-dsn "$MYSQL_DSN" --public-register=false > "$OUT/cp.log" 2>&1 &
  CP_PID=$!
  wait_cp_up 25 || { echo "❌ 控制面未就绪"; tail -5 "$OUT/cp.log"; exit 2; }
  echo "controlplane ready ($CP)"

  echo "== 引导 admin（首登强制改密 → 正式会话）=="
  local J CPT AT
  J=$(curl -s -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/login" \
      -d "{\"username\":\"admin\",\"password\":\"$ADMINPASS\"}")
  CPT=$(printf '%s' "$J" | jget changePasswordToken)
  [ -n "$CPT" ] || { echo "❌ admin 首登未拿到改密令牌: $J"; exit 2; }
  curl -s -o /dev/null -X POST "$CP/api/v1/auth/change-password" -H 'Content-Type: application/json' \
    -d "{\"oldPassword\":\"$ADMINPASS\",\"newPassword\":\"$ADMINPASS2\",\"changePasswordToken\":\"$CPT\"}"
  J=$(curl -s -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/login" \
      -d "{\"username\":\"admin\",\"password\":\"$ADMINPASS2\"}")
  AT=$(printf '%s' "$J" | jget token)
  [ -n "$AT" ] || { echo "❌ admin 二次登录失败: $J"; exit 2; }

  echo "== 引导对侧 admin（auth-svc 侧同样有首登强制改密）=="
  # 不引导的话 cutover-migrate 会 fail-closed 并提示「对侧管理员处于首登强制改密态：请先人工登录
  # 完成改密，再跑迁移」——那条提示本身是对的（job 拒绝拿不够权重的会话去建号），
  # 但演练台应当把它自动化，否则每次演练都卡在同一步。
  bootstrap_far_admin

  echo "== 本地建 $n 个演练账号 =="
  local i name
  for i in $(seq 1 "$n"); do
    name=$(printf 'drill-u%02d' "$i")
    local code
    code=$(curl -s -o "$OUT/mk-$name.json" -w '%{http_code}' -H "Authorization: Bearer $AT" \
      -H 'Content-Type: application/json' -H 'X-Tenant-ID: default' -X POST "$CP/api/v1/users" \
      -d "{\"username\":\"$name\",\"password\":\"$USERPASS\",\"email\":\"$name@drill.local\"}")
    if [ "$code" = "201" ]; then echo "  ✓ $name"; else echo "  ✗ $name ($code): $(cat "$OUT/mk-$name.json")"; fi
  done
  echo "$n" > "$OUT/usercount"
  echo
  echo "演练台已就绪。下一步："
  echo "  bash $0 coords            # 看坐标（给外部 actor）"
  echo "  bash $0 migrate drill-u01,drill-u02"
  echo "  bash $0 stop"
}

cmd_migrate(){
  local users="${1:?用法: migrate u1,u2}"
  echo "== 跑真实 cutover-migrate（--force-change：一次性口令 + 首登强制改密）=="
  local creds
  creds="$OUT/creds-$(date +%H%M%S).txt"
  (cd "$ROOT" && AUTH_CUTOVER_ROSTER_FILE="$ROSTER_FILE" AUTH_SVC_URL="$AS" \
    AUTH_SVC_MIGRATE_ADMIN_USER=admin AUTH_SVC_MIGRATE_ADMIN_PASSWORD="$ADMINPASS2" \
    GOSUMDB=sum.golang.org go run ./cmd/opsmesh cutover-migrate \
      --users "$users" --store mysql --mysql-dsn "$MYSQL_DSN" \
      --force-change --credentials-out "$creds" 2>&1) | tail -25
  echo
  echo "== 名册现状（$ROSTER_FILE）=="
  cat "$ROSTER_FILE"
  echo "== 口令文件（一次性）: $creds =="
  cat "$creds" 2>/dev/null || true
}

cmd_unroute(){
  local user="${1:?用法: unroute u1}"
  echo "== 单用户回退：摘名册 + **吊销对侧会话**（走产品子命令 cutover-unroute，与 runbook §3 同路径）=="
  # 为什么必须走产品子命令而不是手改名册（2026-10-13 第 3 波演练实证）：手改只摘名册、**不吊销**，
  # 对侧残留活跃 rt（实测摘除后仍 refresh 200 并拿到新令牌）⇒ "回退完整"的假象；而 runbook 早已
  # 写明"手改文件不会吊销会话 ⇒ 回退须走 cutover-unroute"。演练台此前只做了一半（W3-D1）。
  local before
  # 注意：`grep -c` 无匹配时**也打印 0** 且退出码 1——不能写 `$(grep -c ... || echo 0)`（会得 "0\n0"，
  # 下一行 [ 比较直接报 integer expected；本台首跑实测）。取到空串（文件不存在）才兜底 0。
  before="$(grep -c '"msg":"切流名册已变更"' "$OUT/cp.log" 2>/dev/null)"; before="${before:-0}"
  (cd "$ROOT" && AUTH_CUTOVER_ROSTER_FILE="$ROSTER_FILE" AUTH_SVC_URL="$AS" \
    AUTH_SVC_MIGRATE_ADMIN_USER=admin AUTH_SVC_MIGRATE_ADMIN_PASSWORD="$ADMINPASS2" \
    GOSUMDB=sum.golang.org go run ./cmd/opsmesh cutover-unroute \
      --users "$user" --store mysql --mysql-dsn "$MYSQL_DSN" 2>&1) | tail -20
  echo
  echo "== 名册现状（$ROSTER_FILE）=="
  cat "$ROSTER_FILE"
  echo "== 等待热加载（按**变更事件数自增**判据，最多 40s）=="
  # 旧写法 grep -q 会命中历史日志行 ⇒ 任何一次执行都"秒报已热加载"（W3-D2 假绿，实测真实生效在 12s 后）。
  local n=0 now
  while [ "$n" -lt 40 ]; do
    now="$(grep -c '"msg":"切流名册已变更"' "$OUT/cp.log" 2>/dev/null)"; now="${now:-0}"
    if [ "$now" -gt "$before" ]; then echo "  ✓ 已热加载（${n}s，变更事件 #$now）"; break; fi
    sleep 1; n=$((n+1))
  done
  [ "$n" -lt 40 ] || echo "  ⚠ 40s 内未见新的热加载事件（before=$before）"
}

cmd_status(){
  echo "== 名册文件 =="; cat "$ROSTER_FILE"
  echo "== 控制面装载日志（最后 2 条）=="
  grep '"msg":"切流名册已装载"' "$OUT/cp.log" | tail -2 || true
  echo "== 路由日志计数：$(routes) =="
  grep 'msg":"切流路由' "$OUT/cp.log" | tail -5 || true
  echo "== 两侧账号 =="
  local J AT
  J=$(curl -s -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/login" \
      -d "{\"username\":\"admin\",\"password\":\"$ADMINPASS2\"}")
  AT=$(printf '%s' "$J" | jget token)
  echo "-- 控制面本侧 --"
  curl -s -H "Authorization: Bearer $AT" -H 'X-Tenant-ID: default' "$CP/api/v1/users" | python -c "
import json,sys
d=json.load(sys.stdin)
us=d.get('users') or d.get('items') or []
print(' ', ', '.join(sorted(u.get('username','?') for u in us)))" 2>/dev/null || echo "  (取不到)"
  echo "-- auth-svc 对侧 --"
  curl -s -H 'Content-Type: application/json' -X POST "$AS/api/v1/auth/login" \
    -d "{\"username\":\"admin\",\"password\":\"$ADMINPASS\"}" -c "$OUT/asadmin.jar" >/dev/null
  curl -s -b "$OUT/asadmin.jar" "$AS/api/v1/users" | python -c "
import json,sys
d=json.load(sys.stdin)
us=d.get('users') or d.get('items') or []
print(' ', ', '.join(sorted(u.get('username','?') for u in us)))" 2>/dev/null || echo "  (取不到)"
}

cmd_coords(){
  cat <<EOF
# 演练台坐标（供外部 actor 使用）
控制面基址 : $CP
auth-svc    : $AS
名册文件    : $ROSTER_FILE
日志        : $OUT/cp.log  /  $OUT/as.log
本侧用户口令: $USERPASS      （drill-u01 … drill-uNN 同口令）
admin       : admin / $ADMINPASS2   （本侧）
对侧 admin  : admin / $ADMINPASS    （auth-svc 侧）
# 常用探针
login   : curl -s -X POST $CP/api/v1/auth/login -H 'Content-Type: application/json' -d '{"username":"drill-u01","password":"$USERPASS"}'
refresh : curl -s -X POST $CP/api/v1/auth/refresh -b <jar> -c <jar>
me      : curl -s -b <jar> $CP/api/v1/auth/me
EOF
}

cmd_stop(){
  [ -n "${CP_PID:-}" ] && kill_pid "$CP_PID"
  [ -n "${AS_PID:-}" ] && kill_pid "$AS_PID"
  purge
  echo "已停（残留进程一并清理）"
}

# ============================================================================
# fault — 引擎级故障注入（2026-10-14 新增，演练报告待办 9 的"故障恢复步骤"落地）。
#
# 为什么值得单列一个子命令：此前的演练只覆盖"流程走通"（登录→切→被切→回退），
# 而真实事故里值班先遇到的是**半边系统趴了**。本命令把三件最苛刻、且当前环境
# 完全可执行的故障场景断言化：
#   A. 对侧（auth-svc）被击杀：名册用户的 login/refresh 必须 fail-closed
#      （5xx + 不吐 401），且**活跃会话不得被一次故障摧毁**（refresh 不清 Cookie），
#      对侧恢复后同一会话直接复活（无需重新登录）。
#   B. 账号锁定副作用（可复现、无需破坏任何基础设施）：连续错口令 ⇒ 429；
#      锁定窗口内**正确口令也 429**——这是防爆破的代价，演练报告要求把它
#      写明而不是等值班在事故夜里第一次撞上。
#   C. store 读故障（§3.6/§3.7 的进程级实证）：杀 MySQL ⇒ login 必须 503 而非
#      401、失败不得计入锁、refresh 不得清 Cookie。**注意**：演练库与其它
#      证据栈共用容器（opsmesh-mysql-evidence-v2），stop/pause 会波及他人——
#      本默认流程只打印步骤不执行；传 --with-mysql 且确认无他人使用时才真杀。
# ============================================================================

say(){ printf '%s\n' "$*" >&2; }   # 走 stderr：stdout 重定向到文件时是块缓冲，排障看不到进度
pass_fail(){ # pass_fail <desc> <got> <want...>
  local desc="$1" got="$2"; shift 2
  local w
  for w in "$@"; do [ "$got" = "$w" ] && { say "  ✓ $desc（$got）"; return 0; }; done
  say "  ✗ $desc：实得 '$got'，期望 $*"; return 1
}

cmd_fault(){
  local with_mysql=0 u="${1:-drill-u01}"
  [ "${1:-}" = "--with-mysql" ] && { with_mysql=1; shift; u="${1:-drill-u01}"; }

  local fails=0
  say "== A. 对侧击杀：fail-closed 与会话存活 =="
  # 先建一个活会话（迁移后的用户走对侧；未迁移的走本地——两种都测）
  local jar="$OUT/fault.jar"
  rm -f "$jar"
  curl -s -o /dev/null -c "$jar" -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/login" \
    -d "{\"username\":\"$u\",\"password\":\"$USERPASS\"}"
  local code
  code=$(curl -s -o /dev/null -w '%{http_code}' -b "$jar" "$CP/api/v1/auth/me")
  pass_fail "击杀前基线：login+me 应 200" "$code" 200 || fails=$((fails+1))

  # PID 发现按**镜像名**（不依赖 start 进程的局部变量——fault 是另起一个 bash，
  # AS_PID 取不到；且 Git Bash 无 pgrep，用与 purge 同款的 taskkill 路径）。
  local as_img; as_img="$(basename "$ASBIN")"
  if command -v taskkill >/dev/null 2>&1; then
    MSYS_NO_PATHCONV=1 taskkill /IM "$as_img" /F >/dev/null 2>&1 || true
  else
    pkill -f "$as_img" >/dev/null 2>&1 || true
  fi
  sleep 1
  if curl -s -o /dev/null --max-time 2 "$AS/api/v1/auth/login" >/dev/null 2>&1; then
    say "  ✗ 击杀后对侧仍在响应（击杀未生效）"; fails=$((fails+1))
    return "$fails"
  fi
  say "  已击杀 auth-svc（按镜像名 $as_img）"

  # 该用户是否在名册——决定 login 对侧的依赖形态（名册用户走对侧、本地用户不依赖）。
  local routed=0
    # 名册条目可能逗号分隔：展平后用 <<< 喂给 grep（**不用 `| grep`**——
    # pipefail 下 producer 非零退出会把命中误判成未命中，internal/gates 有专禁）。
    local flat
    flat="$(tr -d '\r' < "$ROSTER_FILE" 2>/dev/null | tr ',' ' ')"
    if [ -f "$ROSTER_FILE" ] && grep -qw -- "$u" <<< "$flat"; then routed=1; fi
  code=$(curl -s -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/login" \
    -d "{\"username\":\"$u\",\"password\":\"$USERPASS\"}")
  if [ "$routed" = "1" ]; then
    # 基础设施故障不得伪装成凭据错误（§3.6）；本仓约定 fail-closed ⇒ 5xx。
    pass_fail "对侧趴窝时（名册用户）login 应 5xx（不得 401 冒充凭据错误）" "$code" 500 502 503 || fails=$((fails+1))
  else
    pass_fail "对侧趴窝时（本地用户）login 应仍 200（本侧路径不依赖对侧）" "$code" 200 || fails=$((fails+1))
  fi
  code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$CP/api/v1/auth/refresh" -b "$jar" -c "$jar")
  if [ "$routed" = "1" ]; then
    pass_fail "对侧趴窝时（名册用户）refresh 应 5xx（不得 401）" "$code" 500 502 503 || fails=$((fails+1))
  else
    pass_fail "对侧趴窝时（本地用户）refresh 应仍 200" "$code" 200 || fails=$((fails+1))
  fi
  # 关键判据：故障不得摧毁会话——refresh 的失败不得带清 Cookie 动作。
  if grep -q 'Set-Cookie.*Max-Age=0' "$OUT/cp.log" 2>/dev/null; then
    say "  ✗ 故障期出现清 Cookie（会话被故障摧毁）"; fails=$((fails+1))
  else
    say "  ✓ 故障期未清 Cookie（会话存活）"
  fi
  # 恢复：重新拉起对侧（与 cmd_start 同参数）并**重跑 admin 引导**——auth-svc 是
  # memory 后端，重启即失忆（改密状态与对侧账号全丢，见 bootstrap_far_admin 注释）。
  AUTH_SVC_HTTP_ENABLED=true AUTH_SVC_HTTP_PORT=19590 AUTH_SVC_GRPC_PORT=19591 \
  AUTH_SVC_JWT_SECRET="$SECRET" AUTH_SVC_STORE_TYPE=memory AUTH_SVC_PUBLIC_REGISTER=true \
  AUTH_SVC_ADMIN_PASSWORD="$ADMINPASS" AUTH_SVC_DEVICE_FP_ENABLED=false \
    "$ASBIN" >> "$OUT/as.log" 2>&1 &
  AS_PID=$!
  sleep 3
  bootstrap_far_admin
  say "  （注意：auth-svc 是 memory 后端，重启即丢失对侧账号；名册用户需先 unroute"
  say "    再 migrate 才能重建对侧账号——名册幂等会跳过「名册已有」的账号）"
  code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$CP/api/v1/auth/refresh" -b "$jar" -c "$jar")
  if [ "$routed" = "1" ]; then
    pass_fail "对侧恢复后同一会话 refresh 应复活（200）" "$code" 200 || fails=$((fails+1))
  else
    pass_fail "对侧恢复后本地会话 refresh 应仍 200" "$code" 200 || fails=$((fails+1))
  fi

  say "== B. 账号锁定副作用（可复现；错口令 5 次 ⇒ 429，窗口内正确口令也 429）=="
  # 用一个**不存在**的账号做探针：loginGuard 按用户名计数（不存在也计），
  # 这样不会把 admin/演练账号真锁住影响后续 migrate/unroute（本台首版试错过）。
  local probe='fault-lock-probe'
  local n hit=0
  for n in 1 2 3 4 5 6; do
    code=$(curl -s -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/login" \
      -d "{\"username\":\"$probe\",\"password\":\"definitely-wrong-$n\"}")
    [ "$code" = "429" ] && hit=1
  done
  if [ "$hit" = "1" ]; then say "  ✓ 连续错口令已触发 429（防爆破生效）"; else say "  ✗ 6 次错口令未见 429"; fails=$((fails+1)); fi
  say "  （探针账号 $probe 被锁——这正是代价：真实账号此时连正确口令也 429，"
  say "    恢复要等锁定窗口；值班须知，别在事故夜第一次撞上）"

  say "== C. store 读故障（§3.6/§3.7 进程级实证；--with-mysql 才真跑）=="
  say "  默认只打印步骤不执行；传 --with-mysql 且确认无他人使用共享库容器 $MYSQL_C 才真跑"
  say "  （本机 2026-10-14 实测前置：docker ps 在跑容器对 opsmesh_test 零依赖）。"
  say "  步骤：1) docker stop $MYSQL_C  2) login 任意账号 ⇒ 应 503 且**不是** 401；"
  say "        3) 连错 5 次口令后正常口令登录 ⇒ 仍 503（锁不因基础设施故障累计），"
  say "           容器恢复后直接 200（无锁残留）；  4) refresh 活会话 ⇒ 503 且 Cookie 不清；"
  say "        5) docker start $MYSQL_C 后一切恢复。"
  if [ "$with_mysql" != "1" ]; then return 0; fi

  # ---- 真跑：对共享库容器做 stop/start（调用方已确认无他人使用）----
  local victim="${1:-drill-u01}"
  local jar2="$OUT/fault-mysql.jar"
  rm -f "$jar2"
  # 先建一个活会话（sha256 摘要登录=本地路径，与对侧无关）。
  curl -s -o /dev/null -c "$jar2" -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/register" \
    -d "{\"username\":\"$victim\",\"password\":\"$USERPASS\",\"email\":\"$victim@drill.local\"}" >/dev/null 2>&1 || true
  # 注册可能需审批或被占用：退回用既有演练账号登录（start 建的 account 是 active）。
  code=$(curl -s -o /dev/null -w '%{http_code}' -c "$jar2" -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/login" \
    -d "{\"username\":\"drill-u01\",\"password\":\"$USERPASS\"}")
  pass_fail "杀库前基线：drill-u01 登录" "$code" 200 || fails=$((fails+1))

  say "  docker stop $MYSQL_C"
  docker stop "$MYSQL_C" >/dev/null 2>&1 || { say "  ✗ 停库失败（docker 权限？）"; fails=$((fails+1)); return "$fails"; }
  sleep 2
  code=$(curl -s -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/login" \
    -d "{\"username\":\"drill-u01\",\"password\":\"$USERPASS\"}")
  pass_fail "杀库后 login 应 503（不得 401 冒充凭据错误）" "$code" 500 502 503 || fails=$((fails+1))
  # 连错 5 次仍不得因"故障"锁账号（lock 会把这些 503 也算失败的话，恢复后正确口令也进不去）。
  for n in 1 2 3 4 5; do
    curl -s -o /dev/null -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/login" \
      -d "{\"username\":\"drill-u01\",\"password\":\"wrong-$n\"}" >/dev/null 2>&1 || true
  done
  code=$(curl -s -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/login" \
    -d "{\"username\":\"drill-u01\",\"password\":\"$USERPASS\"}")
  pass_fail "锁累计判据：连错 5 次 + 杀库期，正确口令应仍 503（锁未累计）" "$code" 500 502 503 || fails=$((fails+1))
  code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$CP/api/v1/auth/refresh" -b "$jar2" -c "$jar2")
  pass_fail "杀库期 refresh 应 503（不得 401）" "$code" 500 502 503 || fails=$((fails+1))
  if grep -q 'Set-Cookie.*Max-Age=0' "$OUT/cp.log" 2>/dev/null; then
    say "  ✗ 杀库期出现清 Cookie（会话被基础设施故障摧毁）"; fails=$((fails+1))
  else
    say "  ✓ 杀库期未清 Cookie（会话存活）"
  fi

  say "  docker start $MYSQL_C"
  docker start "$MYSQL_C" >/dev/null 2>&1 || { say "  ✗ 起库失败"; fails=$((fails+1)); return "$fails"; }
  # 等库就绪（mysql 容器刚起时握手未就绪会偶连失败）。
  local t=0
  until docker exec "$MYSQL_C" mysql -uroot -prootpass -e "SELECT 1;" >/dev/null 2>&1; do
    t=$((t+1)); [ "$t" -gt 30 ] && { say "  ✗ 库起后不就绪"; fails=$((fails+1)); return "$fails"; }; sleep 1
  done
  say "  ✓ 库已就绪（${t}s）"
  code=$(curl -s -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/login" \
    -d "{\"username\":\"drill-u01\",\"password\":\"$USERPASS\"}")
  pass_fail "库恢复后正确口令应立即 200（锁无残留）" "$code" 200 || fails=$((fails+1))

  say
  if [ "$fails" -eq 0 ]; then say "fault: 全部断言通过"; else say "fault: $fails 条断言未过"; fi
  return "$fails"
}

case "${1:-}" in
  start)   shift; cmd_start "$@" ;;
  migrate) shift; cmd_migrate "$@" ;;
  unroute) shift; cmd_unroute "$@" ;;
  fault)   shift; cmd_fault "$@" ;;
  status)  cmd_status ;;
  coords)  cmd_coords ;;
  stop)    cmd_stop ;;
  *) sed -n '2,20p' "$0"; exit 2 ;;
esac
