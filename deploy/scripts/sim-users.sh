#!/usr/bin/env bash
# sim-users.sh —— 「模拟用户」环境台（2026-10-13 重建版；原版随 F:\Nexus\OpsMesh 被清空丢失）。
#
# 干什么：从**已提交**的 ref 导出构建真二进制，起一个控制面 + N 个真 agent，并备好一组
# RBAC 角色账号与接线单（BRIEF.md），供人/agent 以不同岗位的真实用户身份去用这个平台
# （模拟团队演练：值班运维 / 只读审计含越权试探 / 新同事三人类人设）。
#
# 关键设计（都是演练里交过学费的，别改）：
#   ① **从 git archive 导出到独立目录再构建**——绝不就地构建、绝不碰工作树：共享工作区
#      随时有别的线在重构，环境必须是"已提交"的那一份（原版正是靠这点在仓库被清空的那天继续跑完）；
#   ② **每个 agent 实例一个 --data-dir**：共用目录会共用 agent.id（身份/设备数据串味，演练 B2）；
#   ③ agent 的 gRPC 目标必须经 `--control-addrs=host:port` 显式指定（本机 9090 常被别的实例占着，
#      症状是 `error reading server preface: EOF` 后重试 30 次"启动失败"）；
#   ④ 账号口令满足**两侧同严**的策略（≥12 位 + 大小写 + 数字 + 特殊字符）；
#   ⑤ 控制面预置账号 admin/operator/viewer **全员 mustChangePassword**（弱口令安全债）——
#      环境台自动走完 admin 的首登改密；operator/viewer 保留原样，正好给"新同事"人设体验
#      首登流（登录响应里 changePasswordToken/mustChangePassword 是给客户端看的字段，
#      不是给人看的文案——这条本身就是演练 F1 的记录）。
#
# 用法：
#   bash deploy/scripts/sim-users.sh start [--agents N] [--ref <git ref>]  # 默认 3 agent、origin/main
#   bash deploy/scripts/sim-users.sh coords   # 打印接线单（并落 BRIEF.md）
#   bash deploy/scripts/sim-users.sh status   # 健康 + 账号/agent 概览
#   bash deploy/scripts/sim-users.sh stop      # 停进程（按 PID + 按镜像名双保险）
#
# 端口：控制面 HTTP 19680 / gRPC 19681 / metrics 19682（避开本机 dev 栈 8080/9090 与冒烟台 1958x）。
set -u

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SIM="${OPSMESH_SIM_ROOT:-${TMPDIR:-/tmp}/opsmesh-sim-users}"
EXE=""
case "$(uname -s)" in MINGW*|MSYS*|CYGWIN*) EXE=".exe" ;; esac
CP_BIN="$SIM/bin/sim-cp$EXE"
AG_BIN="$SIM/bin/sim-agent$EXE"
SRC="$SIM/src"
STATE="$SIM/state.env"
CP="http://127.0.0.1:19680"
GRPC_PORT=19681
SECRET="sim-shared-hmac-secret-0123456789abcdef"
ADMINPASS='Sim-Admin-2026!a'
ADMINPASS2='Sim-Admin-2026!b'
USERPASS='Sim-User-2026!a'

# 口令策略两侧同严（≥12 位 + 大小写 + 数字 + 特殊字符），见 credentials.ValidateStrongPassword。
jget(){ python -c "import json,sys; d=json.load(sys.stdin); print(d.get('$1',''))" 2>/dev/null; }
kill_pid(){
  if command -v taskkill >/dev/null 2>&1; then
    MSYS_NO_PATHCONV=1 taskkill /PID "$1" /F >/dev/null 2>&1 || true
  else
    kill "$1" >/dev/null 2>&1 || true
  fi
}
# 按**镜像名**杀：MSYS 下 $! 与 Windows PID 不总是同一个、只按 PID 会静默不生效（实测）。
# 只杀本台改名后的两份二进制（sim-cp / sim-agent），绝不按 opsmesh 这个通用名杀——那会误杀
# 别人的 dev 栈或冒烟进程。
purge_pids(){
  for f in "$SIM"/run/*.pid; do
    [ -f "$f" ] || continue
    kill_pid "$(cat "$f")"
    rm -f "$f"
  done
  if command -v taskkill >/dev/null 2>&1; then
    MSYS_NO_PATHCONV=1 taskkill /IM "sim-cp$EXE" /F >/dev/null 2>&1 || true
    MSYS_NO_PATHCONV=1 taskkill /IM "sim-agent$EXE" /F >/dev/null 2>&1 || true
  else
    pkill -f "sim-cp$EXE" >/dev/null 2>&1 || true
    pkill -f "sim-agent$EXE" >/dev/null 2>&1 || true
  fi
}
# 判据是"连得上吗"，不是"进程列表里有没有"。
wait_cp_up(){
  local n=0 code
  while [ "$n" -lt "${1:-20}" ]; do
    code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "$CP/healthz" 2>/dev/null || true)"
    [ "$code" = "200" ] && return 0
    sleep 1
    n=$((n + 1))
  done
  return 1
}

cmd_start(){
  local agents=3 ref="origin/main"
  while [ $# -gt 0 ]; do
    case "$1" in
      --agents) agents="$2"; shift 2 ;;
      --ref)    ref="$2"; shift 2 ;;
      *) echo "未知参数: $1" >&2; exit 2 ;;
    esac
  done
  command -v go >/dev/null 2>&1 || { echo "需要 go 工具链" >&2; exit 2; }
  command -v python >/dev/null 2>&1 || { echo "需要 python（jget 用）" >&2; exit 2; }

  local sha
  sha="$(git -C "$ROOT" rev-parse --verify "$ref^{commit}")" || { echo "解析 $ref 失败" >&2; exit 2; }
  echo "== 从 git archive 导出 $ref（$sha）并构建（绝不碰工作树）=="
  rm -rf "$SRC"
  mkdir -p "$SRC" "$SIM/bin" "$SIM/log" "$SIM/data" "$SIM/run"
  git -C "$ROOT" archive "$sha" | tar -x -C "$SRC"
  ( cd "$SRC" && go build -o "$SIM/bin/opsmesh$EXE" ./cmd/opsmesh ) || { echo "构建失败" >&2; exit 1; }
  cp "$SIM/bin/opsmesh$EXE" "$CP_BIN"
  cp "$SIM/bin/opsmesh$EXE" "$AG_BIN"

  echo "== 清残留（只杀 sim-cp/sim-agent 两个改名副本）=="
  purge_pids
  sleep 1

  echo "== 起控制面（$CP，memory store）=="
  OPSMESH_JWT_SECRET="$SECRET" OPSMESH_ADMIN_PASSWORD="$ADMINPASS" OPSMESH_HTTP_TLS=off \
    "$CP_BIN" --mode=controlplane --store memory --http-port 19680 --grpc-port "$GRPC_PORT" \
    --metrics-port 19682 --public-register=false > "$SIM/log/cp.log" 2>&1 &
  echo $! > "$SIM/run/cp.pid"
  wait_cp_up 20 || { echo "控制面未能在 20s 内应答 /healthz——看 $SIM/log/cp.log" >&2; exit 1; }

  echo "== admin 首登改密（预置弱口令带 mustChangePassword 标记；已达标则跳过）=="
  local J CPT code ADMIN_EFF="$ADMINPASS2"
  J="$(curl -s -c "$SIM/run/admin.jar" -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/login" \
        -d "{\"username\":\"admin\",\"password\":\"$ADMINPASS\"}")"
  CPT="$(printf '%s' "$J" | jget changePasswordToken)"
  if [ -n "$CPT" ]; then
    code="$(curl -s -o /dev/null -w '%{http_code}' -b "$SIM/run/admin.jar" -c "$SIM/run/admin.jar" \
          -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/change-password" \
          -d "{\"oldPassword\":\"$ADMINPASS\",\"newPassword\":\"$ADMINPASS2\",\"changePasswordToken\":\"$CPT\"}")"
    [ "$code" = "200" ] || { echo "admin 首登改密失败（$code）" >&2; exit 1; }
  else
    echo "  （未要求首登改密——沿用启动口令）"
    ADMIN_EFF="$ADMINPASS"
  fi
  J="$(curl -s -c "$SIM/run/admin.jar" -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/login" \
        -d "{\"username\":\"admin\",\"password\":\"$ADMIN_EFF\"}")"
  local AT; AT="$(printf '%s' "$J" | jget token)"
  [ -n "$AT" ] || { echo "admin 改密后登录未拿到 token：$J" >&2; exit 1; }

  echo "== 建 3 个模拟账号（sim-ops 运维 / sim-audit 只读审计 / sim-new 新同事）=="
  local acct role
  for pair in "sim-ops:role-operator" "sim-audit:role-viewer" "sim-new:role-viewer"; do
    acct="${pair%%:*}"; role="${pair##*:}"
    code="$(curl -s -o "$SIM/log/create-$acct.json" -w '%{http_code}' -H "Authorization: Bearer $AT" \
        -H 'Content-Type: application/json' -X POST "$CP/api/v1/users" \
        -d "{\"username\":\"$acct\",\"password\":\"$USERPASS\",\"email\":\"$acct@sim.local\",\"role_ids\":[\"$role\"]}")"
    case "$code" in
      201|409) ;;  # 409=已存在（本台幂等重跑）
      *) echo "建号 $acct 失败（$code）：$(cat "$SIM/log/create-$acct.json")" >&2; exit 1 ;;
    esac
  done

  echo "== 起 $agents 个 agent（各自独立 --data-dir；gRPC 目标显式指 127.0.0.1:$GRPC_PORT）=="
  local i
  for i in $(seq 1 "$agents"); do
    "$AG_BIN" --mode=agent --control-addrs="127.0.0.1:$GRPC_PORT" \
      --data-dir "$SIM/data/agent$i" --addr 127.0.0.1 > "$SIM/log/agent$i.log" 2>&1 &
    echo $! > "$SIM/run/agent$i.pid"
  done
  sleep 3

  {
    echo "SIM_REF='$ref'"
    echo "SIM_SHA='$sha'"
    echo "SIM_AGENTS='$agents'"
    echo "ADMINPASS='$ADMIN_EFF'"
    echo "USERPASS='$USERPASS'"
  } > "$STATE"

  echo "== 就绪 =="
  cmd_coords
}

cmd_coords(){
  local sha ref admin_eff
  sha="$(grep -F "SIM_SHA=" "$STATE" 2>/dev/null | cut -d= -f2 | tr -d '\047\042')"
  ref="$(grep -F "SIM_REF=" "$STATE" 2>/dev/null | cut -d= -f2 | tr -d '\047\042')"
  admin_eff="$(grep -F "ADMINPASS=" "$STATE" 2>/dev/null | cut -d= -f2 | tr -d '\047\042')"
  [ -n "$admin_eff" ] || admin_eff="$ADMINPASS2"
  local sheet="$SIM/BRIEF.md"
  cat > "$sheet" <<EOF
# 模拟用户环境接线单（由 sim-users.sh 生成）

- 控制面：$CP（健康 $CP/healthz）；gRPC：127.0.0.1:$GRPC_PORT
- 二进制出处：$ref @ ${sha:-?}（git archive 导出构建，与工作树无关）

## 账号（口令均满足两侧同严策略）

| 账号 | 口令 | 角色 | 人设建议 |
|---|---|---|---|
| admin | $admin_eff | role-admin | 管理台操作 |
| sim-ops | $USERPASS | role-operator | 值班运维（可操作设备/任务/告警，**无删除权**） |
| sim-audit | $USERPASS | role-viewer | 只读审计（**故意试越权**，看 403 文案与审计留痕） |
| sim-new | $USERPASS | role-viewer | 新同事（先用种子账号 operator/operator123 走一遍首登改密流） |
| operator/viewer | operator123 / viewer123 | 种子账号（**mustChangePassword**） | 首登流素材；登录响应里找 changePasswordToken |

## 端点口径（演练 B5 的三处更正，别再照抄旧接线单）

- **没有** \`/api/v1/overview\`；概览在 \`/api/v1/platform/metrics\`、\`/api/v1/admin/service-routing\` 等处。
- 网关路由真实路径是 \`/api/v1/gateway/routes\`（不是 \`/gw/routes\`）。
- operator **无** \`quota:read\` / \`platform:read\`——"看配额"这类动作请用 admin 账号。

## 典型动作（带 Cookie 的 B/S 形态）

\`\`\`bash
# 登录（Cookie 落盘）
curl -s -c /tmp/sim.jar -H 'Content-Type: application/json' -X POST $CP/api/v1/auth/login \\
  -d '{"username":"sim-ops","password":"$USERPASS"}'
# 带租户头做一次读（照文档做也对——A2 已修，带 X-Tenant-ID 不再丢身份）
curl -s -b /tmp/sim.jar -H 'X-Tenant-ID: default' $CP/api/v1/devices
# 越权试探（应得 403 且**进审计**——A3 已修）
curl -s -b /tmp/sim.jar -X DELETE $CP/api/v1/devices/dev-xxx
\`\`\`

- agent 侧：设备由 agent 经 gRPC 注册（**没有** \`/api/v1/devices/register\` 这个 HTTP 端点，
  \`cmd/device-sim\` 是演示桩，不跑任务拉取循环）；每实例独立 data-dir，见本台 \`data/agentN\`。
EOF
  echo "接线单：$sheet"
  sed -n '1,40p' "$sheet"
}

cmd_status(){
  local code
  code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "$CP/healthz" 2>/dev/null || true)"
  echo "健康：$CP/healthz => ${code:-（连不上）}"
  for f in "$SIM"/run/*.pid; do
    [ -f "$f" ] || continue
    echo "PID $(basename "$f" .pid) = $(cat "$f")"
  done
  [ -f "$STATE" ] && cat "$STATE"
  if [ "$code" = "200" ]; then
    local AT admin_eff
    admin_eff="$(grep -F "ADMINPASS=" "$STATE" 2>/dev/null | cut -d= -f2 | tr -d '\047\042')"
    [ -n "$admin_eff" ] || admin_eff="$ADMINPASS2"
    AT="$(curl -s -b "$SIM/run/admin.jar" -H 'Content-Type: application/json' -X POST "$CP/api/v1/auth/login" \
          -d "{\"username\":\"admin\",\"password\":\"$admin_eff\"}" | jget token)"
    if [ -n "$AT" ]; then
      # /api/v1/agents 回**裸数组**（不是 {"agents":…}）；devices 回按 segment 分组的字典。
      echo "agent 数：$(curl -s -H "Authorization: Bearer $AT" "$CP/api/v1/agents" | python -c 'import json,sys; d=json.load(sys.stdin); a=d.get("agents") if isinstance(d,dict) else d; print(len(a or []))' 2>/dev/null || echo '?')"
      echo "设备数：$(curl -s -H "Authorization: Bearer $AT" "$CP/api/v1/devices" | python -c 'import json,sys; d=json.load(sys.stdin); print(sum(len(v) for v in d.values()) if isinstance(d,dict) else len(d))' 2>/dev/null || echo '?')"
    fi
  fi
}

cmd_stop(){
  purge_pids
  sleep 1
  local code
  code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "$CP/healthz" 2>/dev/null || true)"
  # curl 连不上时 -w 给 "000"（不是空串）——把它也算"已停止"，否则每次 stop 都假报占用。
  case "$code" in
    ""|"000") echo "已停止（端口不再应答）" ;;
    *) echo "停止后控制面仍应答（$code）——可能有非本台进程占 19680" >&2; return 1 ;;
  esac
}

case "${1:-}" in
  start)  shift; cmd_start "$@" ;;
  coords) cmd_coords ;;
  status) cmd_status ;;
  stop)   cmd_stop ;;
  *) sed -n '20,26p' "$0"; exit 2 ;;
esac
