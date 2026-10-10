#!/usr/bin/env bash
# smoke-cutover.sh — TD-60 方案 C（按用户群分批切换）的**真进程双轨冒烟**。
#
# 起两个真实二进制（控制面 + auth-svc，同一 HMAC 密钥、memory 后端），验证名册路由在真链路上
# 按设计分流。判别信号有两条，都不依赖任何 mock：
#   ① 控制面的路由日志（JSON: msg="切流路由：转 auth-svc"，含 endpoint/user/归属判定）；
#   ② 两侧登录失败文案不同：本地 "invalid username or password" / auth-svc "invalid credentials"
#      —— 文案出自哪侧即证明请求落在哪侧。
#
# 覆盖的判据（31 项，见输出逐条 PASS/FAIL）：
#   非名册用户走本地且不触发路由 · 名册用户被转发（本地正确口令不被接受）· 本地闸（IP 令牌桶）
#   先于路由（限流请求不转发）· 模拟迁移（对侧注册+审批）后名册用户登录 200 且会话由对侧签发 ·
#   refresh 按归属侧转发（local_owns=false）· 本地用户 refresh 仍本地 · 名册用户 /me 按 JWT 转发 ·
#   启动自检（名册装载 + 本侧存在性兜底）·
#
# 用法：bash deploy/scripts/smoke-cutover.sh     （约 1 分钟；需 go 工具链与 curl）
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
cleanup(){
  [ -n "${CP_PID:-}" ] && kill_pid "$CP_PID"
  [ -n "${AS_PID:-}" ] && kill_pid "$AS_PID"
  if command -v taskkill >/dev/null 2>&1; then
    MSYS_NO_PATHCONV=1 taskkill /IM "cutover-smoke-cp$EXE" /F >/dev/null 2>&1 || true
    MSYS_NO_PATHCONV=1 taskkill /IM "cutover-smoke-as$EXE" /F >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

echo "== 构建（控制面 + auth-svc）=="
(cd "$ROOT" && go build -o "$CPBIN" ./cmd/opsmesh) || { echo "build cp failed"; exit 1; }
(cd "$ROOT/services/auth-svc" && go build -o "$ASBIN" ./cmd/auth-svc) || { echo "build auth-svc failed"; exit 1; }

echo "== 起 auth-svc（$AS）=="
AUTH_SVC_HTTP_ENABLED=true AUTH_SVC_HTTP_PORT=19590 AUTH_SVC_GRPC_PORT=19591 AUTH_SVC_JWT_SECRET="$SECRET" \
AUTH_SVC_STORE_TYPE=memory AUTH_SVC_PUBLIC_REGISTER=true AUTH_SVC_ADMIN_PASSWORD="$ADMINPASS" \
AUTH_SVC_DEVICE_FP_ENABLED=false "$ASBIN" > "$OUT/authsvc.log" 2>&1 &
AS_PID=$!
sleep 2

echo "== 起控制面（$CP）=="
OPSMESH_JWT_SECRET="$SECRET" OPSMESH_ADMIN_PASSWORD="$ADMINPASS" OPSMESH_HTTP_TLS=off \
AUTH_SVC_URL="$AS" AUTH_SVC_PROXY_ENABLED=true AUTH_CUTOVER_ROSTER=smoke-roster \
"$CPBIN" --http-port 19580 --grpc-port 19581 --metrics-port 19582 \
  --store memory --public-register=false > "$OUT/cp.log" 2>&1 &
CP_PID=$!
sleep 4

jget(){ python -c "import json,sys; d=json.load(sys.stdin); print(d.get('$1',''))" 2>/dev/null; }
routes(){ grep -c 'msg":"切流路由' "$OUT/cp.log" 2>/dev/null || true; }

echo "== 0. 双进程在听 + 启动自检 =="
c=$(curl -s -o /dev/null -w '%{http_code}' "$AS/api/v1/auth/me")
[ "$c" = "401" ] && ok "auth-svc 网关在听（401）" || { no "auth-svc 网关异常（$c）"; tail -5 "$OUT/authsvc.log"; exit 2; }
c=$(curl -s -o /dev/null -w '%{http_code}' "$CP/api/v1/auth/me")
[ "$c" = "401" ] && ok "控制面在听（401）" || { no "控制面异常（$c）"; tail -8 "$OUT/cp.log"; exit 2; }
grep -q "切流名册已装载" "$OUT/cp.log" && ok "启动自检打印名册装载日志" || no "名册装载日志缺失"
grep -q "名册条目在本侧不存在" "$OUT/cp.log" && ok "自检提示名册条目本侧不存在（拼写兜底生效）" || no "自检未提示"

echo "== 1. 控制面 admin 首登改密（拿管理 token）=="
J=$(curl -s -c "$OUT/admin.jar" -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/login" \
  -d "{\"username\":\"admin\",\"password\":\"$ADMINPASS\"}")
CPT=$(printf '%s' "$J" | jget changePasswordToken)
[ -n "$CPT" ] && ok "首登返回一次性改密令牌" || no "首登未返回改密令牌：$J"
J=$(curl -s -b "$OUT/admin.jar" -c "$OUT/admin.jar" -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/change-password" \
  -d "{\"oldPassword\":\"$ADMINPASS\",\"newPassword\":\"$ADMINPASS2\",\"changePasswordToken\":\"$CPT\"}")
AT=$(printf '%s' "$J" | jget token)
[ -n "$AT" ] && ok "改密后签发 at" || no "改密未签发 at：$J"

mkuser(){
  local name=$1 code
  code=$(curl -s -o "$OUT/mk-$name.json" -w '%{http_code}' -H "Authorization: Bearer $AT" -H 'Content-Type: application/json' \
    -H 'X-Tenant-ID: default' -X POST "$CP/api/v1/users" \
    -d "{\"username\":\"$name\",\"password\":\"$USERPASS\",\"email\":\"$name@smoke.io\"}")
  [ "$code" = "201" ] && ok "本地建号 $name（201）" || no "本地建号 $name 失败（$code）：$(cat "$OUT/mk-$name.json")"
}
echo "== 2. 建两个本地用户（一个进名册、一个不进）=="
mkuser smoke-roster
mkuser smoke-local

echo "== 3. 非名册用户：本地登录 200，且无路由日志 =="
BEFORE=$(routes)
J=$(curl -s -b "$OUT/local.jar" -c "$OUT/local.jar" -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/login" \
  -d "{\"username\":\"smoke-local\",\"password\":\"$USERPASS\"}")
T=$(printf '%s' "$J" | jget token)
[ -n "$T" ] && ok "smoke-local 本地登录成功" || no "smoke-local 登录失败：$J"
AFTER=$(routes); [ "$BEFORE" = "$AFTER" ] && ok "非名册用户未触发路由" || no "非名册用户被误路由（$BEFORE→$AFTER）"

echo "== 4. 名册用户（对侧尚无此账号）：必须被转发 ⇒ 错误文案出自 auth-svc =="
BEFORE=$AFTER
J=$(curl -s -b "$OUT/roster.jar" -c "$OUT/roster.jar" -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/login" \
  -d "{\"username\":\"smoke-roster\",\"password\":\"$USERPASS\"}")
printf '%s' "$J" | grep -q "invalid credentials" && ok "错误文案出自 auth-svc（转发已发生）" || no "文案非 auth-svc 形态：$J"
printf '%s' "$J" | grep -q "invalid username or password" && no "被本地处理了（文案是本地的）" || ok "未被本地处理（本地正确口令未被接受）"
AFTER=$(routes); [ "$AFTER" -gt "$BEFORE" ] && ok "路由日志已记录（login）" || no "路由日志缺失（$BEFORE→$AFTER）"
grep '"endpoint":"login"' "$OUT/cp.log" | grep -q '"user":"smoke-roster"' && ok "路由日志点名 smoke-roster" || no "路由日志未点名用户"

# 4b. 本地闸先于路由（确定性演示）：IP 令牌桶容量 5、每 6s 补 1 ⇒ 连打 5 发，
#     期间至少 1 发为本地 429 且**不转发**（限流发生在路由判定之前）。
ROUTED_BEFORE=$(routes)
THROTTLED=0
for _ in 1 2 3 4 5; do
  J=$(curl -s -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/login" \
    -d "{\"username\":\"smoke-roster\",\"password\":\"$USERPASS\"}")
  printf '%s' "$J" | grep -q "too many requests" && THROTTLED=$((THROTTLED+1))
done
ROUTED_AFTER=$(routes)
[ "$THROTTLED" -ge 1 ] && ok "连打阶段出现本地 429（令牌桶生效）" || no "连打 5 发未触发限流（桶参数变化？）"
[ "$((ROUTED_AFTER-ROUTED_BEFORE))" -le 4 ] && ok "被限流的请求未被转发（本地闸先于路由）" || no "限流请求被转发（本段路由增量应 ≤4，实为 $((ROUTED_AFTER-ROUTED_BEFORE))）"
echo "  [info] 等待令牌回填（每 6s 1 枚）后再继续"
sleep 14

echo "== 5. 在 auth-svc 侧建号并审批（模拟迁移：注册→管理员审批）=="
J=$(curl -s -H 'Content-Type: application/json' -X POST "$AS/api/v1/auth/register" \
  -d "{\"username\":\"smoke-roster\",\"password\":\"$USERPASS\",\"email\":\"smoke-roster@smoke.io\"}")
printf '%s' "$J" | grep -q "pending" && ok "auth-svc 侧注册已受理（pending；其响应体不含 userId）" || no "auth-svc 注册失败：$J"
J=$(curl -s -c "$OUT/asadmin.jar" -H 'Content-Type: application/json' -X POST "$AS/api/v1/auth/login" \
  -d "{\"username\":\"admin\",\"password\":\"$ADMINPASS\"}")
ACPT=$(printf '%s' "$J" | jget changePasswordToken)
J=$(curl -s -b "$OUT/asadmin.jar" -c "$OUT/asadmin.jar" -H 'Content-Type: application/json' -X POST "$AS/api/v1/auth/change-password" \
  -d "{\"oldPassword\":\"$ADMINPASS\",\"newPassword\":\"$ADMINPASS2\",\"changePasswordToken\":\"$ACPT\"}")
printf '%s' "$J" | grep -q "password changed" && ok "auth-svc 侧首登改密完成" || no "auth-svc 改密失败：$J"
grep -q "opsmesh_at" "$OUT/asadmin.jar" && ok "auth-svc 会话 Cookie 入罐（其改密只经 Set-Cookie 下发）" || no "auth-svc 未下发会话 Cookie"
ASUID=$(curl -s -b "$OUT/asadmin.jar" "$AS/api/v1/users" | python -c "
import json,sys
d=json.load(sys.stdin)
us=d.get('users') or d.get('items') or []
hit=[u for u in us if (u.get('username') or u.get('Username'))=='smoke-roster']
print((hit[0].get('id') or hit[0].get('ID')) if hit else '')
" 2>/dev/null)
[ -n "$ASUID" ] && ok "从对侧用户列表取到 id（$ASUID）" || no "未能取到对侧用户 id"
code=$(curl -s -o "$OUT/approve.json" -w '%{http_code}' -b "$OUT/asadmin.jar" -X POST "$AS/api/v1/users/$ASUID/approve")
[ "$code" = "200" ] && ok "审批通过（200）" || no "审批失败（$code）：$(cat "$OUT/approve.json")"

echo "== 6. 名册用户（对侧已有账号）：经控制面登录应 200（由 auth-svc 应答）=="
BEFORE=$(routes)
code=$(curl -s -o "$OUT/rosterlogin.json" -w '%{http_code}' -b "$OUT/roster.jar" -c "$OUT/roster.jar" \
  -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/login" \
  -d "{\"username\":\"smoke-roster\",\"password\":\"$USERPASS\"}")
[ "$code" = "200" ] && ok "名册用户登录 200（跨进程转发成功）" || no "名册用户登录失败（$code）：$(cat "$OUT/rosterlogin.json")"
# auth-svc 的登录成功只经 Set-Cookie 下发会话（响应体无 token 字段，与本地形态不同）。
grep -q "opsmesh_at" "$OUT/roster.jar" && ok "auth-svc 签发的会话 Cookie 入罐" || no "未见 auth-svc 会话 Cookie"
AFTER=$(routes); [ "$AFTER" -gt "$BEFORE" ] && ok "路由日志已记录（login 第二次）" || no "路由日志缺失"

echo "== 7. 会话归属证明：上一步的 rt 由 auth-svc 签发 ⇒ 刷新按归属侧转发 =="
BEFORE=$AFTER
code=$(curl -s -o "$OUT/refresh.json" -w '%{http_code}' -b "$OUT/roster.jar" -c "$OUT/roster.jar" -X POST "$CP/api/v1/auth/refresh")
[ "$code" = "200" ] && ok "刷新成功（由 auth-svc 旋转）" || no "刷新失败（$code）：$(cat "$OUT/refresh.json" 2>/dev/null)"
AFTER=$(routes); [ "$AFTER" -gt "$BEFORE" ] && ok "路由日志已记录（refresh）" || no "刷新未记录路由"
grep '"endpoint":"refresh"' "$OUT/cp.log" | grep -q '"local_owns":false' && ok "归属判定为对侧（local_owns=false）" || no "归属判定异常"

echo "== 8. 对照：本地用户刷新仍在本地（无路由日志）=="
BEFORE=$AFTER
code=$(curl -s -o "$OUT/refresh2.json" -w '%{http_code}' -b "$OUT/local.jar" -c "$OUT/local.jar" -X POST "$CP/api/v1/auth/refresh")
[ "$code" = "200" ] && ok "本地用户刷新成功" || no "本地刷新失败（$code）"
AFTER=$(routes); [ "$BEFORE" = "$AFTER" ] && ok "本地用户刷新未触发路由" || no "本地用户刷新被误路由"

echo "== 9. 名册用户 /me 也应按 JWT（sub）转发 =="
BEFORE=$AFTER
code=$(curl -s -o "$OUT/me.json" -w '%{http_code}' -b "$OUT/roster.jar" "$CP/api/v1/auth/me")
[ "$code" = "200" ] && ok "名册用户 /me 200" || no "名册用户 /me 失败（$code）：$(cat "$OUT/me.json" 2>/dev/null)"
AFTER=$(routes); [ "$AFTER" -gt "$BEFORE" ] && ok "路由日志已记录（me）" || no "/me 未记录路由"

echo
echo "===== 冒烟结果：PASS=$PASS FAIL=$FAIL ====="
if [ "$FAIL" != "0" ]; then echo "--- cp.log 路由日志 ---"; grep '切流路由' "$OUT/cp.log" | tail -8; echo "--- cp.log 尾部 ---"; tail -10 "$OUT/cp.log"; fi
[ "$FAIL" = "0" ]
