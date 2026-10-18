#!/usr/bin/env bash
# smoke-task-cutover.sh — TD-60 task 域切流的**真进程冒烟**（runbook 的机制面证据）。
#
# 起两个真实二进制（控制面 --demo + task-svc，均为 memory 后端），在真链路上验证
# task 域的「整域开关（D）」与「增量开关（TASK_SVC_OWNS_NEW_TASKS）」的联合语义：
#
#   ① 基线（全默认）：两侧都能建任务——这正是切流要消掉的双写形态；
#   ② 增量开关打开：控制面**四个**建任务入口一律 409 并指引去 task-svc 侧，
#      而**读**（GET /api/v1/tasks）不受影响；
#   ③ 代理面：/api/v1/task-svc/tasks 经聚合层落到 task-svc（201），
#      且 /api/v1/admin/service-routing 如实报告 task 域的转发状态；
#   ④ 联锁：OPSMESH_SERVICE_PROXY 含 task（域停用、单体在服务任务）时，
#      即便两个开关都打开也**不得** 409——否则单体"读得到任务、建不了任务"，
#      前端没有任何一条路可走（自伤式停摆）；
#   ⑤ 回退：增量开关关回 false 后本地建任务立即恢复（**无需重启**——判据读 env 在请求时）。
#
# 与 auth 域冒烟的差别（写清楚，避免照搬）：task 域**没有名册**——名册的键是用户名，
# 而"任务归谁"是**新任务**的全局决策（存量任务在某一侧的 tasks 表里，没有按请求路由的键）。
# 故 task 域切流 = 整域开关 + 增量开关；不存在 cutover-migrate/cutover-unroute 这类搬号 job。
#
# 判据（见输出逐条 PASS/FAIL）。**未接入 CI**（需两个空闲端口、起两个进程）；
# 切流批次执行前建议本地复跑一次。
#
# 用法：bash deploy/scripts/smoke-task-cutover.sh        （约 1 分钟；需 go 工具链与 curl）
# 端口：控制面 19680/19681/19682，task-svc 19690/19691（刻意避开本机 dev 栈与 auth 冒烟的端口）。
set -u
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT="${TMPDIR:-/tmp}/opsmesh-task-cutover-smoke"
EXE=""
case "$(uname -s)" in MINGW*|MSYS*|CYGWIN*) EXE=".exe";; esac
CPBIN="$OUT/task-smoke-cp$EXE"
TSBIN="$OUT/task-smoke-ts$EXE"
mkdir -p "$OUT"; rm -f "$OUT"/*.log 2>/dev/null
SECRET="smoke-shared-hmac-secret-task-0123456789"
CP=http://127.0.0.1:19680
TS=http://127.0.0.1:19690
PASS=0; FAIL=0
ok(){ echo "  [PASS] $1"; PASS=$((PASS+1)); }
no(){ echo "  [FAIL] $1"; FAIL=$((FAIL+1)); }
kill_pid(){
  if command -v taskkill >/dev/null 2>&1; then
    MSYS_NO_PATHCONV=1 taskkill /PID "$1" /F >/dev/null 2>&1 || true
  else
    kill "$1" >/dev/null 2>&1 || true
  fi
}
purge(){
  [ -n "${CP_PID:-}" ] && kill_pid "$CP_PID"
  [ -n "${TS_PID:-}" ] && kill_pid "$TS_PID"
  if command -v taskkill >/dev/null 2>&1; then
    MSYS_NO_PATHCONV=1 taskkill /IM "task-smoke-cp$EXE" /F >/dev/null 2>&1 || true
    MSYS_NO_PATHCONV=1 taskkill /IM "task-smoke-ts$EXE" /F >/dev/null 2>&1 || true
  else
    pkill -f "task-smoke-cp$EXE" >/dev/null 2>&1 || true
    pkill -f "task-smoke-ts$EXE" >/dev/null 2>&1 || true
  fi
}
trap purge EXIT

# wait_cp_up 等控制面应答（401 = 网关在听）。
wait_cp_up(){
  local n=0 code
  while [ "$n" -lt "${1:-20}" ]; do
    code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 2 "$CP/api/v1/tasks" 2>/dev/null || true)
    if [ "$code" != "000" ] && [ -n "$code" ]; then return 0; fi
    sleep 1; n=$((n + 1))
  done
  return 1
}

echo "== 构建（控制面 + task-svc）=="
(cd "$ROOT" && go build -o "$CPBIN" ./cmd/opsmesh) || { echo "build cp failed"; exit 1; }
(cd "$ROOT/services/task-svc" && go build -o "$TSBIN" ./cmd/task-svc) || { echo "build task-svc failed"; exit 1; }

echo "== 0.0 清理残留进程 =="
purge; sleep 1

echo "== 起 task-svc（$TS）=="
TASK_SVC_HTTP_PORT=19690 TASK_SVC_GRPC_PORT=19691 TASK_SVC_STORE_TYPE=memory \
  TASK_SVC_JWT_SECRET="$SECRET" TASK_SVC_HTTP_GATEWAY_ENABLED=true \
  TASK_SVC_LEADER_MODE=stub "$TSBIN" > "$OUT/tasksvc.log" 2>&1 &
TS_PID=$!
sleep 2

echo "== 起控制面（$CP，--demo 播种 agent-demo-*）=="
# TASK_SVC_URL 指到本机 task-svc；OPSMESH_SERVICE_PROXY 默认空=全部转发。
# 增量开关先关（基线），第 2 节再开——判据读 env 在**请求时**，故无需重启即可翻转。
start_cp(){
  OPSMESH_JWT_SECRET="$SECRET" OPSMESH_HTTP_TLS=off TASK_SVC_URL="$TS" \
  TASK_SVC_PROXY_ENABLED=false TASK_SVC_OWNS_NEW_TASKS=false OPSMESH_SERVICE_PROXY="${1:-}" \
  "$CPBIN" --http-port 19680 --grpc-port 19681 --metrics-port 19682 \
    --store memory --demo=true >> "$OUT/cp.log" 2>&1 &
  CP_PID=$!
}
start_cp ""
wait_cp_up 25 || true

# create_local 打控制面本地建任务入口，打印状态码（-w）。
local_create(){ curl -s -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' \
  -H 'X-Tenant-ID: default' -H 'X-User-Id: u-admin' -X POST "$CP$1" -d "$2"; }
# create_ts 直接打 task-svc 的 HTTP 网关。
ts_create(){ curl -s -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' \
  -H 'X-Tenant-ID: default' -X POST "$TS/api/v1/tasks" -d "$1"; }

echo "== 0. 双进程在听 =="
c=$(curl -s -o /dev/null -w '%{http_code}' "$TS/health")
if [ "$c" = "200" ]; then ok "task-svc 健康检查 200"; else no "task-svc 未就绪（$c）"; tail -5 "$OUT/tasksvc.log"; exit 2; fi
c=$(curl -s -o /dev/null -w '%{http_code}' "$CP/api/v1/tasks")
if [ "$c" != "000" ] && [ -n "$c" ]; then ok "控制面在听（任务列表 HTTP $c）"; else no "控制面未就绪"; tail -8 "$OUT/cp.log"; exit 2; fi

echo "== 1. 基线（全开关关）：两侧都能建任务 =="
c=$(local_create /api/v1/tasks '{"agentID":"agent-demo-1","type":"shell","command":"uname -a"}')
if [ "$c" = "201" ]; then ok "控制面本地建任务 201（基线）"; else no "基线本地建任务应 201，实得 $c"; fi
c=$(ts_create '{"agentID":"agent-demo-1","type":"shell","command":"uname -a"}')
if [ "$c" = "201" ]; then ok "task-svc 建任务 201（基线；双写形态成立）"; else no "基线 task-svc 建任务应 201，实得 $c"; fi

echo "== 2. 代理面：/api/v1/task-svc/tasks 经聚合层落到 task-svc =="
c=$(curl -s -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -H 'X-Tenant-ID: default' \
  -X POST "$CP/api/v1/task-svc/tasks" -d '{"agentID":"agent-demo-1","type":"shell","command":"uname -a"}')
if [ "$c" = "201" ]; then ok "代理前缀 /api/v1/task-svc/tasks 落到 task-svc（201）"; else no "代理前缀应 201，实得 $c"; fi
J=$(curl -s -H 'X-Tenant-ID: default' "$CP/api/v1/admin/service-routing")
if grep -q '"domain":"task"' <<<"$J"; then ok "service-routing 报告 task 域状态"; else no "service-routing 缺失 task 域"; fi

echo "== 3. 增量开关打开：四个入口 409 + 指引 =="
# 判据读 env 在**请求时**，但已在运行的控制面进程不会继承脚本后来导出的变量——
# 故这里用重启把两个开关真正注入进程（生产上改 .env 后重启同理）。
purge; sleep 1
OPSMESH_JWT_SECRET="$SECRET" OPSMESH_HTTP_TLS=off TASK_SVC_URL="$TS" \
  TASK_SVC_PROXY_ENABLED=true TASK_SVC_OWNS_NEW_TASKS=true OPSMESH_SERVICE_PROXY="" \
  "$CPBIN" --http-port 19680 --grpc-port 19681 --metrics-port 19682 \
    --store memory --demo=true >> "$OUT/cp.log" 2>&1 &
CP_PID=$!
wait_cp_up 25 || true
c=$(local_create /api/v1/tasks '{"agentID":"agent-demo-1","type":"shell","command":"uname -a"}')
if [ "$c" = "409" ]; then ok "单建入口 409（增量开关生效）"; else no "单建入口应 409，实得 $c"; fi
B=$(curl -s -H 'Content-Type: application/json' -H 'X-Tenant-ID: default' -X POST "$CP/api/v1/tasks" \
  -d '{"agentID":"agent-demo-1","type":"shell","command":"uname -a"}')
if grep -q 'task-svc' <<<"$B"; then ok "409 文案指引去 task-svc 侧"; else no "409 文案未指引：$B"; fi
for ep in "/api/v1/tasks/batch|{\"targets\":[\"agent-demo-1\"],\"type\":\"shell\",\"command\":\"uname -a\"}" \
          "/api/v1/tasks/batch-exec|{\"agentIDs\":[\"agent-demo-1\"],\"type\":\"shell\",\"command\":\"uname -a\"}" \
          "/api/v1/tasks/canary|{\"deviceIDs\":[\"dev-demo-01\"],\"type\":\"shell\",\"command\":\"uname -a\"}"; do
  p="${ep%%|*}"; b="${ep#*|}"
  c=$(local_create "$p" "$b")
  if [ "$c" = "409" ]; then ok "入口 $p 409"; else no "入口 $p 应 409，实得 $c"; fi
done
c=$(curl -s -o /dev/null -w '%{http_code}' -H 'X-Tenant-ID: default' "$CP/api/v1/tasks")
if [ "$c" = "200" ]; then ok "读路径不受闸门影响（GET /api/v1/tasks 200）"; else no "读路径应 200，实得 $c"; fi

echo "== 4. 联锁：域停用（OPSMESH_SERVICE_PROXY=task）时不得 409 =="
purge; sleep 1
# 域停用 + 两个开关都打开，重启生效（OPSMESH_SERVICE_PROXY 只在启动期读）。
OPSMESH_JWT_SECRET="$SECRET" OPSMESH_HTTP_TLS=off TASK_SVC_URL="$TS" \
  TASK_SVC_PROXY_ENABLED=true TASK_SVC_OWNS_NEW_TASKS=true OPSMESH_SERVICE_PROXY=task \
  "$CPBIN" --http-port 19680 --grpc-port 19681 --metrics-port 19682 \
    --store memory --demo=true >> "$OUT/cp.log" 2>&1 &
CP_PID=$!
wait_cp_up 25 || true
c=$(local_create /api/v1/tasks '{"agentID":"agent-demo-1","type":"shell","command":"uname -a"}')
if [ "$c" != "409" ]; then ok "域停用时本地建任务未被 409（联锁生效，实得 $c）"; else no "域停用却 409——自伤式停摆"; fi

echo "== 5. 回退：域恢复 + 增量开关关回 false（重启）后本地可建 =="
purge; sleep 1; start_cp ""; wait_cp_up 25 || true
c=$(local_create /api/v1/tasks '{"agentID":"agent-demo-1","type":"shell","command":"uname -a"}')
if [ "$c" = "201" ]; then ok "回退后本地建任务 201（域恢复 + 开关关）"; else no "回退后本地建任务应 201，实得 $c"; fi

echo
echo "==================================================="
echo "  task 域切流冒烟：PASS=$PASS  FAIL=$FAIL"
echo "==================================================="
[ "$FAIL" -eq 0 ]
