#!/usr/bin/env bash
# OpsMesh 负载基线包装器（load 场景）
# 用法：BASE_URL=... TOKEN=... TENANT=... bash scripts/loadtest/run-baseline.sh
set -euo pipefail

if ! command -v k6 >/dev/null 2>&1; then
  echo "ERROR: k6 未安装。安装方式见 https://grafana.com/docs/k6/latest/set-up/install-k6/" >&2
  exit 127
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

: "${BASE_URL:=http://127.0.0.1:8080}"
: "${TENANT:=default}"

echo "== OpsMesh 负载基线 =="
echo "BASE_URL=$BASE_URL  TENANT=$TENANT  SCENARIO=load"
if [ -z "${TOKEN:-}" ]; then
  echo "WARN: TOKEN 为空，仅压 /healthz 与 /readyz（业务读面跳过）"
fi

k6 run \
  -e SCENARIO=load \
  -e BASE_URL="$BASE_URL" \
  -e TENANT="$TENANT" \
  -e TOKEN="${TOKEN:-}" \
  "$SCRIPT_DIR/k6-baseline.js"
