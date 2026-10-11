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
  echo "== 单用户回退：从名册摘除 $user（热加载 ≤30s）=="
  python - "$ROSTER_FILE" "$user" <<'PYEOF'
import sys
p, u = sys.argv[1], sys.argv[2]
lines = [l for l in open(p, encoding='utf-8').read().splitlines() if l.split('#')[0].strip() != u]
open(p, 'w', encoding='utf-8', newline='\n').write('\n'.join(lines) + '\n')
print('摘除后名册:', [l for l in lines if l.strip() and not l.strip().startswith('#')])
PYEOF
  echo "等待热加载（轮询 cp.log，最多 40s）…"
  local n=0
  while [ "$n" -lt 40 ]; do
    if grep -q '"msg":"切流名册已变更"' "$OUT/cp.log"; then echo "  ✓ 已热加载（${n}s）"; break; fi
    sleep 1; n=$((n+1))
  done
  [ "$n" -lt 40 ] || echo "  ⚠ 40s 内未见热加载日志"
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

case "${1:-}" in
  start)   shift; cmd_start "$@" ;;
  migrate) shift; cmd_migrate "$@" ;;
  unroute) shift; cmd_unroute "$@" ;;
  status)  cmd_status ;;
  coords)  cmd_coords ;;
  stop)    cmd_stop ;;
  *) sed -n '2,20p' "$0"; exit 2 ;;
esac
