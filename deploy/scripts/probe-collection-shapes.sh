#!/usr/bin/env bash
# probe-collection-shapes.sh —— 集合端点空值形状黑盒巡检
#
# 为什么需要它（2026-10-03 转正运行时验收的产物）：nil 切片直出 JSON 是 null，
# 前端列表 v-for/.map() 直接崩——静态检查（vet/lint/类型）完全看不见这类别名
# 缺陷。本脚本对全部集合类端点断言「200 且响应体是 [] 或 {…:[]}」，把该类缺陷
# 变成一条可复跑的门禁（本轮据此抓到 /api/v1/alerts 与 /api/v1/incidents 两处）。
#
# 用法：
#   probe-collection-shapes.sh                     # 默认 https://127.0.0.1:8080，
#                                                 # 口令读 deploy/docker/.env 的 ADMIN_PASSWORD
#   BASE_URL=http://cp:8080 ADMIN_PASSWORD=xxx probe-collection-shapes.sh
#
# 退出码：0=全部通过；1=发现 null/空体或非 200（打印逐端点明细）。
#
# 尚未并入 verify-runtime.sh 的原因：2026-10-03 时该文件处于另一条工作线的
# 进行中现场，按纪律不碰；合并点已登记在 docs/roadmap-2026-10-03.md 批次 A4。
set -uo pipefail

BASE_URL="${BASE_URL:-https://127.0.0.1:8080}"
ENV_FILE="${ENV_FILE:-$(dirname "$0")/../docker/.env}"

if [ -z "${ADMIN_PASSWORD:-}" ] && [ -f "$ENV_FILE" ]; then
  # CR 剥离：Windows 上写的 .env 是 CRLF，直接 cut 会把 \r 带进 JSON 串 → 400。
  ADMIN_PASSWORD="$(grep -E '^ADMIN_PASSWORD=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || true)"
fi
if [ -z "${ADMIN_PASSWORD:-}" ]; then
  echo "::error::缺少 ADMIN_PASSWORD（设置环境变量或提供 $ENV_FILE）"
  exit 1
fi

JAR="$(mktemp)"
BODY="$(mktemp)"
trap 'rm -f "$JAR" "$BODY"' EXIT

login() {
  printf '{"username":"admin","password":"%s"}' "$ADMIN_PASSWORD" > "$BODY"
  code=$(curl -sk -c "$JAR" -X POST -H 'Content-Type: application/json' \
    --data-binary "@$BODY" -o /dev/null -w '%{http_code}' "$BASE_URL/api/v1/auth/login")
  [ "$code" = "200" ] || { echo "::error::登录失败 HTTP=$code"; exit 1; }
}

# 集合类端点清单（与 2026-10-03 巡检同口径；新增域时同步追加）
ENDPOINTS=(
  /api/v1/runbooks
  /api/v1/incidents
  /api/v1/autoscaler/rules
  /api/v1/autoscaler/decisions
  /api/v1/autoscaler/cooldowns
  /api/v1/portal/requests
  /api/v1/portal/approvals
  /api/v1/gpu/nodes
  /api/v1/gpu/workloads
  /api/v1/gpu/models
  /api/v1/gpu/quotas
  /api/v1/tasks
  /api/v1/alerts
  /api/v1/audits
  /api/v1/schedules
  /api/v1/webhooks
  /api/v1/scripts
  /api/v1/notify-channels
)

login

fails=0
for ep in "${ENDPOINTS[@]}"; do
  code=$(curl -sk -b "$JAR" -o "$BODY" -w '%{http_code}' "$BASE_URL$ep")
  raw=$(tr -d ' \n\r' < "$BODY")
  if [ "$code" != "200" ]; then
    echo "::error::$ep → HTTP $code"
    fails=$((fails + 1))
  elif [ -z "$raw" ] || [ "$raw" = "null" ]; then
    echo "::error::$ep → 空/null 响应体（前端列表会崩）"
    fails=$((fails + 1))
  else
    case "$raw" in
      \[*|'{'*) ;;
      *) echo "::error::$ep → 非集合形状（$raw）"; fails=$((fails + 1));;
    esac
  fi
done

if [ "$fails" -gt 0 ]; then
  echo "❌ 集合端点形状巡检：$fails 个端点不合格"
  exit 1
fi
echo "✅ 集合端点形状巡检：${#ENDPOINTS[@]} 个端点全部 200 且为 []/{…:[]}"