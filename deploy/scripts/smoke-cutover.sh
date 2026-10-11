#!/usr/bin/env bash
# smoke-cutover.sh — TD-60 方案 C（按用户群分批切换）的**真进程双轨冒烟**。
#
# 起两个真实二进制（控制面 + auth-svc，同一 HMAC 密钥、memory 后端），验证名册路由在真链路上
# 按设计分流。判别信号有两条，都不依赖任何 mock：
#   ① 控制面的路由日志（JSON: msg="切流路由：转 auth-svc"，含 endpoint/user/归属判定）；
#   ② 两侧登录失败文案不同：本地 "invalid username or password" / auth-svc "invalid credentials"
#      —— 文案出自哪侧即证明请求落在哪侧。
#
# 覆盖的判据（45 项，见输出逐条 PASS/FAIL）：
#   非名册用户走本地且不触发路由 · 名册用户被转发（本地正确口令不被接受）· 本地闸（IP 令牌桶）
#   先于路由（限流请求不转发）· 模拟迁移（对侧注册+审批）后名册用户登录 200 且会话由对侧签发 ·
#   refresh 按归属侧转发（local_owns=false）· 本地用户 refresh 仍本地 · 名册用户 /me 按 JWT 转发 ·
#   启动自检（名册装载 + 本侧存在性兜底）·
#   **回退路径三件（第 10 节）**：清空名册热重载（≤30s，不重启）后回落本侧 · 回退不搁浅已签发会话
#   （对侧 rt 仍按归属侧转发并可刷新）· 硬回退（空名册重启，runbook §5 的文档路径）后全本地。
#
# 用法：bash deploy/scripts/smoke-cutover.sh     （约 2 分钟；需 go 工具链与 curl）
#       耗时含一次 ≤35s 的名册热重载等待（第 10 节 10a；重载周期硬编码 30s，见 cutoverRosterLoop）。
# 端口：控制面 19580/19581/19582，auth-svc 19590/19591（刻意避开本机 dev 栈的 8080/9090/9091/50052）。
# 说明：**未接入 CI**（需两个空闲端口、起两个进程，且含一次 ~14s 的令牌回填等待）；
#       切流批次执行前建议本地复跑一次作为机制面证据。
set -u
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT="${TMPDIR:-/tmp}/opsmesh-cutover-smoke"
EXE=""
case "$(uname -s)" in MINGW*|MSYS*|CYGWIN*) EXE=".exe";; esac
CPBIN="$OUT/cutover-smoke-cp$EXE"
ASBIN="$OUT/cutover-smoke-as$EXE"
mkdir -p "$OUT"; rm -f "$OUT"/*.log "$OUT"/*.jar "$OUT"/*.json 2>/dev/null
SECRET="smoke-shared-hmac-secret-0123456789abcdef"
ADMINPASS='Smoke-Admin-2026x!'
ADMINPASS2='Smoke-Admin-2026y!'
USERPASS='Smoke-User-2026x!'
CP=http://127.0.0.1:19580
AS=http://127.0.0.1:19590
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
# purge_cp 杀控制面：按 PID + **按镜像名**双保险。
# 为什么要双保险（2026-10-12 实测）：MSYS 下 $! 的 PID 与 Windows PID 不总是同一个，
# 只按 PID taskkill 会静默不生效——旧进程继续占着 19581，新进程 bind 失败，
# 而"最后一条装载日志"之类的判据照样绿（假绿）。第 10c 节即栽在这上面。
purge_cp(){
  [ -n "${CP_PID:-}" ] && kill_pid "$CP_PID"
  if command -v taskkill >/dev/null 2>&1; then
    MSYS_NO_PATHCONV=1 taskkill /IM "cutover-smoke-cp$EXE" /F >/dev/null 2>&1 || true
  else
    pkill -f "cutover-smoke-cp$EXE" >/dev/null 2>&1 || true
  fi
}

# wait_port_free 等控制面端口真的不再应答（$1 秒上限）。
# 判据是"连不上"，不是"进程列表里没有"——只有前者能证明后续应答来自新进程。
wait_port_free(){
  local n=0
  while [ "$n" -lt "${1:-20}" ]; do
    if ! curl -s -o /dev/null --max-time 2 "$CP/api/v1/auth/me" >/dev/null 2>&1; then return 0; fi
    sleep 1
    n=$((n + 1))
  done
  return 1
}

# wait_cp_up 等控制面重新应答（401 = 网关在听）。
wait_cp_up(){
  local n=0 code
  while [ "$n" -lt "${1:-20}" ]; do
    code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 2 "$CP/api/v1/auth/me" 2>/dev/null || true)
    if [ "$code" = "401" ]; then return 0; fi
    sleep 1
    n=$((n + 1))
  done
  return 1
}

cleanup(){
  purge_cp
  [ -n "${AS_PID:-}" ] && kill_pid "$AS_PID"
  if command -v taskkill >/dev/null 2>&1; then
    MSYS_NO_PATHCONV=1 taskkill /IM "cutover-smoke-as$EXE" /F >/dev/null 2>&1 || true
  else
    pkill -f "cutover-smoke-as$EXE" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

echo "== 构建（控制面 + auth-svc）=="
(cd "$ROOT" && go build -o "$CPBIN" ./cmd/opsmesh) || { echo "build cp failed"; exit 1; }
(cd "$ROOT/services/auth-svc" && go build -o "$ASBIN" ./cmd/auth-svc) || { echo "build auth-svc failed"; exit 1; }

# 首启前先清残并断言端口空闲：若上一轮有残留进程占着端口，本次的"首启"会失败，
# 而第 0 节的健康检查照样绿（旧进程应答）——这是另一处假绿入口，一并堵掉。
echo "== 0.0 清理残留进程并确认端口空闲 =="
purge_cp
if command -v taskkill >/dev/null 2>&1; then
  MSYS_NO_PATHCONV=1 taskkill /IM "cutover-smoke-as$EXE" /F >/dev/null 2>&1 || true
else
  pkill -f "cutover-smoke-as$EXE" >/dev/null 2>&1 || true
fi
sleep 1
if wait_port_free 15; then ok "控制面端口空闲（无残留进程应答）"; else no "端口仍被占用——本轮判据会假绿，先手工清理再跑"; exit 2; fi

echo "== 起 auth-svc（$AS）=="
AUTH_SVC_HTTP_ENABLED=true AUTH_SVC_HTTP_PORT=19590 AUTH_SVC_GRPC_PORT=19591 AUTH_SVC_JWT_SECRET="$SECRET" \
AUTH_SVC_STORE_TYPE=memory AUTH_SVC_PUBLIC_REGISTER=true AUTH_SVC_ADMIN_PASSWORD="$ADMINPASS" \
AUTH_SVC_DEVICE_FP_ENABLED=false "$ASBIN" > "$OUT/authsvc.log" 2>&1 &
AS_PID=$!
sleep 2

echo "== 起控制面（$CP）=="
# 名册用**文件**来源（AUTH_CUTOVER_ROSTER_FILE）：与 runbook 的生产旋钮同形，
# 且只有文件来源支持热重载——第 10 节的两条回退路径都依赖它。
# 日志一律**追加**（不是覆盖）：名册热重载与硬回退重启都要在既有日志里留下可查记录。
ROSTER_FILE="$OUT/roster.txt"
printf 'smoke-roster\n' > "$ROSTER_FILE"
start_cp(){
  OPSMESH_JWT_SECRET="$SECRET" OPSMESH_ADMIN_PASSWORD="$ADMINPASS" OPSMESH_HTTP_TLS=off \
  AUTH_SVC_URL="$AS" AUTH_SVC_PROXY_ENABLED=true AUTH_CUTOVER_ROSTER_FILE="$ROSTER_FILE" \
  "$CPBIN" --http-port 19580 --grpc-port 19581 --metrics-port 19582 \
    --store memory --public-register=false >> "$OUT/cp.log" 2>&1 &
  CP_PID=$!
}
start_cp
# 等它就绪再进断言（不写死 sleep：2026-10-12 实测启动到就绪 4.6s，写死 4s 会偶发误判成 000）。
wait_cp_up 20 || true

jget(){ python -c "import json,sys; d=json.load(sys.stdin); print(d.get('$1',''))" 2>/dev/null; }
routes(){ grep -c 'msg":"切流路由' "$OUT/cp.log" 2>/dev/null || true; }

echo "== 0. 双进程在听 + 启动自检 =="
c=$(curl -s -o /dev/null -w '%{http_code}' "$AS/api/v1/auth/me")
if [ "$c" = "401" ]; then ok "auth-svc 网关在听（401）"; else no "auth-svc 网关异常（$c）"; tail -5 "$OUT/authsvc.log"; exit 2; fi
c=$(curl -s -o /dev/null -w '%{http_code}' "$CP/api/v1/auth/me")
if [ "$c" = "401" ]; then ok "控制面在听（401）"; else no "控制面异常（$c）"; tail -8 "$OUT/cp.log"; exit 2; fi
if grep -q "切流名册已装载" "$OUT/cp.log"; then ok "启动自检打印名册装载日志"; else no "名册装载日志缺失"; fi
if grep -q "名册条目在本侧不存在" "$OUT/cp.log"; then ok "自检提示名册条目本侧不存在（拼写兜底生效）"; else no "自检未提示"; fi

echo "== 1. 控制面 admin 首登改密（拿管理 token）=="
J=$(curl -s -c "$OUT/admin.jar" -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/login" \
  -d "{\"username\":\"admin\",\"password\":\"$ADMINPASS\"}")
CPT=$(printf '%s' "$J" | jget changePasswordToken)
if [ -n "$CPT" ]; then ok "首登返回一次性改密令牌"; else no "首登未返回改密令牌：$J"; fi
J=$(curl -s -b "$OUT/admin.jar" -c "$OUT/admin.jar" -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/change-password" \
  -d "{\"oldPassword\":\"$ADMINPASS\",\"newPassword\":\"$ADMINPASS2\",\"changePasswordToken\":\"$CPT\"}")
AT=$(printf '%s' "$J" | jget token)
if [ -n "$AT" ]; then ok "改密后签发 at"; else no "改密未签发 at：$J"; fi

mkuser(){
  local name=$1 code
  code=$(curl -s -o "$OUT/mk-$name.json" -w '%{http_code}' -H "Authorization: Bearer $AT" -H 'Content-Type: application/json' \
    -H 'X-Tenant-ID: default' -X POST "$CP/api/v1/users" \
    -d "{\"username\":\"$name\",\"password\":\"$USERPASS\",\"email\":\"$name@smoke.io\"}")
if   [ "$code" = "201" ]; then ok "本地建号 $name（201）"; else no "本地建号 $name 失败（$code）：$(cat "$OUT/mk-$name.json")"; fi
}
echo "== 2. 建两个本地用户（一个进名册、一个不进）=="
mkuser smoke-roster
mkuser smoke-local

echo "== 3. 非名册用户：本地登录 200，且无路由日志 =="
BEFORE=$(routes)
J=$(curl -s -b "$OUT/local.jar" -c "$OUT/local.jar" -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/login" \
  -d "{\"username\":\"smoke-local\",\"password\":\"$USERPASS\"}")
T=$(printf '%s' "$J" | jget token)
if [ -n "$T" ]; then ok "smoke-local 本地登录成功"; else no "smoke-local 登录失败：$J"; fi
if AFTER=$(routes); [ "$BEFORE" = "$AFTER" ]; then ok "非名册用户未触发路由"; else no "非名册用户被误路由（$BEFORE→$AFTER）"; fi

echo "== 4. 名册用户（对侧尚无此账号）：必须被转发 ⇒ 错误文案出自 auth-svc =="
BEFORE=$AFTER
J=$(curl -s -b "$OUT/roster.jar" -c "$OUT/roster.jar" -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/login" \
  -d "{\"username\":\"smoke-roster\",\"password\":\"$USERPASS\"}")
if grep -q "invalid credentials" <<<"$J"; then ok "错误文案出自 auth-svc（转发已发生）"; else no "文案非 auth-svc 形态：$J"; fi
if grep -q "invalid username or password" <<<"$J"; then no "被本地处理了（文案是本地的）"; else ok "未被本地处理（本地正确口令未被接受）"; fi
if AFTER=$(routes); [ "$AFTER" -gt "$BEFORE" ]; then ok "路由日志已记录（login）"; else no "路由日志缺失（$BEFORE→$AFTER）"; fi
if grep -qE '"endpoint":"login".*"user":"smoke-roster"' "$OUT/cp.log"; then ok "路由日志点名 smoke-roster"; else no "路由日志未点名用户"; fi

# 4b. 本地闸先于路由（确定性演示）：IP 令牌桶容量 5、每 6s 补 1 ⇒ 连打 5 发，
#     期间至少 1 发为本地 429 且**不转发**（限流发生在路由判定之前）。
ROUTED_BEFORE=$(routes)
THROTTLED=0
for _ in 1 2 3 4 5; do
  J=$(curl -s -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/login" \
    -d "{\"username\":\"smoke-roster\",\"password\":\"$USERPASS\"}")
  grep -q "too many requests" <<<"$J" && THROTTLED=$((THROTTLED+1))
done
ROUTED_AFTER=$(routes)
if [ "$THROTTLED" -ge 1 ]; then ok "连打阶段出现本地 429（令牌桶生效）"; else no "连打 5 发未触发限流（桶参数变化？）"; fi
if [ "$((ROUTED_AFTER-ROUTED_BEFORE))" -le 4 ]; then ok "被限流的请求未被转发（本地闸先于路由）"; else no "限流请求被转发（本段路由增量应 ≤4，实为 $((ROUTED_AFTER-ROUTED_BEFORE))）"; fi
echo "  [info] 等待令牌回填（每 6s 1 枚）后再继续"
sleep 14

echo "== 5. 在 auth-svc 侧建号并审批（模拟迁移：注册→管理员审批）=="
J=$(curl -s -H 'Content-Type: application/json' -X POST "$AS/api/v1/auth/register" \
  -d "{\"username\":\"smoke-roster\",\"password\":\"$USERPASS\",\"email\":\"smoke-roster@smoke.io\"}")
if grep -q "pending" <<<"$J"; then ok "auth-svc 侧注册已受理（pending；其响应体不含 userId）"; else no "auth-svc 注册失败：$J"; fi
J=$(curl -s -c "$OUT/asadmin.jar" -H 'Content-Type: application/json' -X POST "$AS/api/v1/auth/login" \
  -d "{\"username\":\"admin\",\"password\":\"$ADMINPASS\"}")
ACPT=$(printf '%s' "$J" | jget changePasswordToken)
J=$(curl -s -b "$OUT/asadmin.jar" -c "$OUT/asadmin.jar" -H 'Content-Type: application/json' -X POST "$AS/api/v1/auth/change-password" \
  -d "{\"oldPassword\":\"$ADMINPASS\",\"newPassword\":\"$ADMINPASS2\",\"changePasswordToken\":\"$ACPT\"}")
if grep -q "password changed" <<<"$J"; then ok "auth-svc 侧首登改密完成"; else no "auth-svc 改密失败：$J"; fi
if grep -q "opsmesh_at" "$OUT/asadmin.jar"; then ok "auth-svc 会话 Cookie 入罐（其改密只经 Set-Cookie 下发）"; else no "auth-svc 未下发会话 Cookie"; fi
ASUID=$(curl -s -b "$OUT/asadmin.jar" "$AS/api/v1/users" | python -c "
import json,sys
d=json.load(sys.stdin)
us=d.get('users') or d.get('items') or []
hit=[u for u in us if (u.get('username') or u.get('Username'))=='smoke-roster']
print((hit[0].get('id') or hit[0].get('ID')) if hit else '')
" 2>/dev/null)
if [ -n "$ASUID" ]; then ok "从对侧用户列表取到 id（$ASUID）"; else no "未能取到对侧用户 id"; fi
code=$(curl -s -o "$OUT/approve.json" -w '%{http_code}' -b "$OUT/asadmin.jar" -X POST "$AS/api/v1/users/$ASUID/approve")
if [ "$code" = "200" ]; then ok "审批通过（200）"; else no "审批失败（$code）：$(cat "$OUT/approve.json")"; fi

echo "== 6. 名册用户（对侧已有账号）：经控制面登录应 200（由 auth-svc 应答）=="
BEFORE=$(routes)
code=$(curl -s -o "$OUT/rosterlogin.json" -w '%{http_code}' -b "$OUT/roster.jar" -c "$OUT/roster.jar" \
  -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/login" \
  -d "{\"username\":\"smoke-roster\",\"password\":\"$USERPASS\"}")
if [ "$code" = "200" ]; then ok "名册用户登录 200（跨进程转发成功）"; else no "名册用户登录失败（$code）：$(cat "$OUT/rosterlogin.json")"; fi
# auth-svc 的登录成功只经 Set-Cookie 下发会话（响应体无 token 字段，与本地形态不同）。
if grep -q "opsmesh_at" "$OUT/roster.jar"; then ok "auth-svc 签发的会话 Cookie 入罐"; else no "未见 auth-svc 会话 Cookie"; fi
if AFTER=$(routes); [ "$AFTER" -gt "$BEFORE" ]; then ok "路由日志已记录（login 第二次）"; else no "路由日志缺失"; fi

echo "== 7. 会话归属证明：上一步的 rt 由 auth-svc 签发 ⇒ 刷新按归属侧转发 =="
BEFORE=$AFTER
code=$(curl -s -o "$OUT/refresh.json" -w '%{http_code}' -b "$OUT/roster.jar" -c "$OUT/roster.jar" -X POST "$CP/api/v1/auth/refresh")
if [ "$code" = "200" ]; then ok "刷新成功（由 auth-svc 旋转）"; else no "刷新失败（$code）：$(cat "$OUT/refresh.json" 2>/dev/null)"; fi
if AFTER=$(routes); [ "$AFTER" -gt "$BEFORE" ]; then ok "路由日志已记录（refresh）"; else no "刷新未记录路由"; fi
if grep -qE '"endpoint":"refresh".*"local_owns":false' "$OUT/cp.log"; then ok "归属判定为对侧（local_owns=false）"; else no "归属判定异常"; fi

echo "== 8. 对照：本地用户刷新仍在本地（无路由日志）=="
BEFORE=$AFTER
code=$(curl -s -o "$OUT/refresh2.json" -w '%{http_code}' -b "$OUT/local.jar" -c "$OUT/local.jar" -X POST "$CP/api/v1/auth/refresh")
if [ "$code" = "200" ]; then ok "本地用户刷新成功"; else no "本地刷新失败（$code）"; fi
if AFTER=$(routes); [ "$BEFORE" = "$AFTER" ]; then ok "本地用户刷新未触发路由"; else no "本地用户刷新被误路由"; fi

echo "== 9. 名册用户 /me 也应按 JWT（sub）转发 =="
BEFORE=$AFTER
code=$(curl -s -o "$OUT/me.json" -w '%{http_code}' -b "$OUT/roster.jar" "$CP/api/v1/auth/me")
if [ "$code" = "200" ]; then ok "名册用户 /me 200"; else no "名册用户 /me 失败（$code）：$(cat "$OUT/me.json" 2>/dev/null)"; fi
if AFTER=$(routes); [ "$AFTER" -gt "$BEFORE" ]; then ok "路由日志已记录（me）"; else no "/me 未记录路由"; fi

echo "== 10. 回退路径（runbook §5：名册摘除=单用户回退，清空=全量回退）=="
# 为什么补这段：此前 31 项判据**全在验证"切过去"**，而批次一旦出事，运维唯一依赖的是回退路径
# ——它此前只在单测里覆盖，没在真进程上压过。本段补三件：
#   10a 软回退：清空名册 → 热重载（≤30s，不重启进程）→ 名册用户回落本侧
#   10b 已签发会话不搁浅：回退后，切流期间由 auth-svc 签发的 rt 仍按归属侧转发并可刷新
#   10c 硬回退：runbook 的文档路径——空名册重启控制面（判据=端口先释放再重新应答，
: > "$ROSTER_FILE"
RELOADED=0
WAITED=0
while [ "$WAITED" -lt 40 ]; do
  if grep -q '"msg":"切流名册已变更".*"after":0' "$OUT/cp.log"; then RELOADED=1; break; fi
  sleep 1
  WAITED=$((WAITED + 1))
done
# 把实测等待秒数打进结论：它是运维真正关心的数字（回退要等多久生效，周期硬编码 30s）。
if [ "$RELOADED" = "1" ]; then ok "名册清空后热重载生效（实测等待 ${WAITED}s，进程未重启）"; else no "${WAITED}s 内未见置空热重载（cp.log 无 after:0）"; fi

# 10a. 回落本侧：名册用户用**本侧原口令**应被本侧受理，且不再产生路由日志。
# 判别信号用响应体形态：本侧登录在 body 里签发 token，auth-svc 只走 Set-Cookie（body 无 token）。
BEFORE=$(routes)
code=$(curl -s -o "$OUT/rollback-login.json" -w '%{http_code}' -H 'Content-Type: application/json' \
  -X POST "$CP/api/v1/auth/login" -d "{\"username\":\"smoke-roster\",\"password\":\"$USERPASS\"}")
if [ "$code" = "200" ]; then ok "回退后名册用户由本侧受理（200）"; else no "回退后本地登录失败（$code）：$(cat "$OUT/rollback-login.json")"; fi
if grep -q '"token"' "$OUT/rollback-login.json"; then ok "响应体含 token ⇒ 本侧签发（对侧只走 Set-Cookie）"; else no "响应体无 token——可能仍被判到对侧"; fi
if AFTER=$(routes); [ "$BEFORE" = "$AFTER" ]; then ok "回退后不再路由（本地接管）"; else no "回退后仍被路由（$BEFORE→$AFTER）"; fi

# 10b. 会话不搁浅：roster.jar 里是切流期间 auth-svc 旋转出的 rt。
BEFORE=$(routes)
code=$(curl -s -o "$OUT/rollback-refresh.json" -w '%{http_code}' -b "$OUT/roster.jar" -c "$OUT/roster.jar" -X POST "$CP/api/v1/auth/refresh")
if [ "$code" = "200" ]; then ok "回退后对侧会话仍可刷新（200）"; else no "回退后对侧会话刷新失败（$code）：$(cat "$OUT/rollback-refresh.json" 2>/dev/null)"; fi
if AFTER=$(routes); [ "$AFTER" -gt "$BEFORE" ]; then ok "刷新仍按归属侧转发（回退不搁浅已签发会话）"; else no "回退后刷新未转发——已签发会话被搁浅"; fi

# 10c. 硬回退（runbook §5 的文档路径）：空名册 + 重启控制面。
# 判据刻意不解析日志："最后一条装载日志"曾被证明可以假绿——旧进程没被杀掉时，新进程 bind 失败，
# 而它失败前已经打印过 count:0 的装载行（2026-10-12 实测）。只看两件硬证据：
#   ① 旧进程真的退出（端口不再应答）② 新进程真的起来（端口重新应答 401）。
# ①不成立时后续判据全是假绿，故 ① 失败即整段放弃（不再往下断言）。
purge_cp
if wait_port_free 20; then
  ok "硬回退：旧进程已退出、端口已释放"
  start_cp
  if wait_cp_up 20; then
    ok "硬回退：新进程已起来（/auth/me 应答 401）"
    # 本冒烟用 --store memory：重启会**清空账号数据**，所以直接拿旧账号登录必然 401
    # （2026-10-12 实测：第一版 10c 就是拿这条当"通过"——实际是旧进程还没死，假绿）。
    # 要验证"回退后本地登录面完全可用"，就得在新进程上重建本地账号再登：
    # 顺带把 admin 首启引导路径也再压一遍。生产（SQL 后端）重启不清号，runbook §5
    # 的场景比这里更宽——这里验的是"名册空 + 本地登录链路通"。
    J=$(curl -s -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/login" \
      -d "{\"username\":\"admin\",\"password\":\"$ADMINPASS\"}")
    CPT=$(printf '%s' "$J" | jget changePasswordToken)
    if [ -n "$CPT" ]; then
      code=$(curl -s -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/change-password" \
        -d "{\"oldPassword\":\"$ADMINPASS\",\"newPassword\":\"$ADMINPASS2\",\"changePasswordToken\":\"$CPT\"}")
      if [ "$code" = "200" ]; then ok "硬回退后 admin 首启引导可走通（重建账号的前置）"; else no "硬回退后 admin 改密失败（$code）"; fi
      J=$(curl -s -c "$OUT/at2.jar" -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/login" \
        -d "{\"username\":\"admin\",\"password\":\"$ADMINPASS2\"}")
      AT=$(printf '%s' "$J" | jget token)
      code=$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $AT" -H 'Content-Type: application/json' \
        -H 'X-Tenant-ID: default' -X POST "$CP/api/v1/users" \
        -d "{\"username\":\"smoke-roster\",\"password\":\"$USERPASS\",\"email\":\"smoke-roster@smoke.io\"}")
      if [ "$code" = "201" ]; then ok "硬回退后本侧重建名册用户（201）"; else no "硬回退后重建用户失败（$code）"; fi
    else
      no "硬回退后 admin 首登未返回改密令牌（新进程未按空库首启？）：$J"
    fi
    BEFORE=$(routes)
    code=$(curl -s -o "$OUT/hard-rollback-login.json" -w '%{http_code}' -H 'Content-Type: application/json' \
      -X POST "$CP/api/v1/auth/login" -d "{\"username\":\"smoke-roster\",\"password\":\"$USERPASS\"}")
    if [ "$code" = "200" ]; then ok "硬回退后本地登录可用（200）"; else no "硬回退后本地登录失败（$code）：$(cat "$OUT/hard-rollback-login.json" 2>/dev/null)"; fi
    if grep -q '"token"' "$OUT/hard-rollback-login.json"; then ok "硬回退后响应体含 token ⇒ 本侧签发"; else no "硬回退后响应体无 token"; fi
    if AFTER=$(routes); [ "$BEFORE" = "$AFTER" ]; then ok "硬回退后无路由（名册空即全本地）"; else no "硬回退后仍被路由（$BEFORE→$AFTER）"; fi
  else
    no "硬回退：新进程 20s 内未应答——本轮判据不可信"
  fi
else
  no "硬回退：旧进程 20s 内未退出（端口仍应答）——后续判据不可信，跳过"
fi
echo
echo "===== 冒烟结果：PASS=$PASS FAIL=$FAIL ====="
if [ "$FAIL" != "0" ]; then echo "--- cp.log 路由日志 ---"; grep '切流路由' "$OUT/cp.log" | tail -8; echo "--- cp.log 尾部 ---"; tail -10 "$OUT/cp.log"; fi
[ "$FAIL" = "0" ]
