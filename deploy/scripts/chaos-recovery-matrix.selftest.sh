#!/usr/bin/env bash
# chaos-recovery-matrix.sh 的离线判据自测：不接触真实 Docker/网络/服务。
# 用假 docker/curl 覆盖：守卫拒绝、MySQL 场景正反向、清理恢复。
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
TARGET="$SCRIPT_DIR/chaos-recovery-matrix.sh"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/bin" "$TMP/artifacts"
: > "$TMP/compose.yaml"
export FAKE_LOG="$TMP/docker.log"
export FAKE_STATE="$TMP/state"
export FAKE_BODY_DIR="$TMP/bodies"
mkdir -p "$FAKE_BODY_DIR"

cat > "$TMP/bin/docker" <<'EOF'
#!/usr/bin/env bash
printf 'docker %s\n' "$*" >> "$FAKE_LOG"
if [[ "$1" == "compose" ]]; then
  case " $* " in
    *" ps -q "*|*" ps -aq "*)
      # 按服务名给 id，便于 inspect 区分 controlplane（应存活）与被停依赖。
      printf 'cid-%s\n' "${!#}" ;;
    *" stop mysql"*) printf 'degraded\n' > "$FAKE_STATE" ;;
    *" start mysql"*) printf 'up\n' > "$FAKE_STATE" ;;
    *" stop redis"*) printf 'degraded\n' > "$FAKE_STATE" ;;
    *" start redis"*) printf 'up\n' > "$FAKE_STATE" ;;
    *" stop kafka"*) printf 'degraded\n' > "$FAKE_STATE" ;;
    *" start kafka"*) printf 'up\n' > "$FAKE_STATE" ;;
    *" exec -T kafka"*) printf '{"target":"fake-tpl-0"}\n' ;;
  esac
  exit 0
fi
if [[ "$1" == "inspect" ]]; then
  id="${!#}"
  if [[ "$id" == "cid-controlplane" ]]; then printf 'true\n'; exit 0; fi
  st=$(cat "$FAKE_STATE" 2>/dev/null || printf 'up')
  if [[ "$st" == "degraded" ]]; then printf 'false\n'; else printf 'true\n'; fi
  exit 0
fi
if [[ "$1" == "network" ]]; then
  exit 0
fi
exit 64
EOF

cat > "$TMP/bin/curl" <<'EOF'
#!/usr/bin/env bash
# 参数最小解析：吃下 -X/-H/-d/-b/-c/-w/-o/--max-time 等，只关心 URL 与 -o。
url=""; ofile="/dev/null"; method="GET"; body=""
args=("$@")
for ((i=0; i<${#args[@]}; i++)); do
  case "${args[$i]}" in
    -X) method="${args[$((i+1))]}" ;;
    -o) ofile="${args[$((i+1))]}" ;;
    -d) body="${args[$((i+1))]}" ;;
    http*) url="${args[$i]}" ;;
  esac
done
state=$(cat "$FAKE_STATE" 2>/dev/null || printf 'up')
code=200; payload='{}'
case "$url" in
  */healthz) payload='{"ok":true}' ;;
  */readyz)  if [[ "$state" == "degraded" ]]; then code=503; payload='{"error":"mysql down"}'; else payload='{"ok":true}'; fi ;;
  */api/v1/me) payload='{"userID":"user-admin"}' ;;
  */api/v1/audit/verify) payload='{"ok":true}' ;;
  */api/v1/devices) payload='{"devices":[]}' ;;
  */api/v1/apikeys)
    if [[ "$method" == "POST" ]]; then
      if [[ "$state" == "degraded" ]]; then code=500; payload='{"error":"store down"}';
      else code=201; payload='{"apiKey":{"id":"fake-key-1","name":"chaos152-mysql-pre"}}'; fi
    else
      payload='{"apiKeys":[{"id":"fake-key-1","name":"chaos152-mysql-pre"}]}'
    fi ;;
  */api/v1/audit/events*) payload='[{"action":"apikey_create","target":"fake-key-1"}]' ;;
  *) payload='{}' ;;
esac
printf '%s' "$payload" > "$ofile"
printf '%s|0.001\n' "$code"
EOF
chmod +x "$TMP/bin/docker" "$TMP/bin/curl"
export PATH="$TMP/bin:$PATH"
export OPSMESH_CHAOS_PROJECT=chaos-selftest
export OPSMESH_CHAOS_COMPOSE_FILE="$TMP/compose.yaml"
export OPSMESH_CHAOS_BASE_URL=http://example.invalid
export OPSMESH_CHAOS_HTTP_TIMEOUT=1
export OPSMESH_CHAOS_DEGRADED_TIMEOUT=3
export OPSMESH_CHAOS_RECOVERY_TIMEOUT=2
export OPSMESH_CHAOS_ARTIFACTS="$TMP/artifacts"
export OPSMESH_CHAOS_SCENARIOS=mysql

# 1) 守卫：生产项目名直接拒绝。
set +e
OPSMESH_CHAOS_PROJECT=prod bash "$TARGET" >/dev/null 2>&1
rc_prod=$?
set -e
[[ "$rc_prod" -eq 64 ]] || { echo "SELFTEST FAIL: prod 守卫未拒绝（rc=$rc_prod）" >&2; exit 1; }

# 2) 正向：MySQL 停→有界降级→恢复→业务一致性，全程假组件。
printf 'up\n' > "$FAKE_STATE"
: > "$FAKE_LOG"
bash "$TARGET" > "$TMP/positive.log" 2>&1
grep -Fq 'stop mysql' "$FAKE_LOG" || { echo "SELFTEST FAIL: 未调用 stop mysql" >&2; exit 1; }
grep -Fq 'start mysql' "$FAKE_LOG" || { echo "SELFTEST FAIL: 未调用 start mysql" >&2; exit 1; }
grep -Fq '| PASS |' "$TMP/artifacts/report.md" || { echo "SELFTEST FAIL: 报告无 PASS 行" >&2; cat "$TMP/artifacts/report.md" >&2; exit 1; }
grep -Fq 'audit/verify=200' "$TMP/artifacts/mysql.log" || { echo "SELFTEST FAIL: 原始日志缺 audit/verify 证据" >&2; exit 1; }
grep -Fq '数据一致性' "$TMP/artifacts/mysql.log" || { echo "SELFTEST FAIL: 原始日志缺业务一致性证据" >&2; exit 1; }

# 3) 反向：依赖停止后 readiness 不摘流（假阳性）必须判红，且 trap 仍恢复依赖。
printf 'up\n' > "$FAKE_STATE"
: > "$FAKE_LOG"
cat > "$TMP/bin/curl" <<'EOF'
#!/usr/bin/env bash
url=""; ofile="/dev/null"; method="GET"
args=("$@")
for ((i=0; i<${#args[@]}; i++)); do
  case "${args[$i]}" in
    -X) method="${args[$((i+1))]}" ;;
    -o) ofile="${args[$((i+1))]}" ;;
    -d) : ;;
    http*) url="${args[$i]}" ;;
  esac
done
state=$(cat "$FAKE_STATE" 2>/dev/null || printf 'up')
code=200; payload='{"ok":true}'
case "$url" in
  */readyz) [[ "$state" == "degraded" ]] || payload='{"ok":true}' ;;
  */api/v1/me) payload='{"userID":"user-admin"}' ;;
  */api/v1/apikeys)
    if [[ "$method" == "POST" ]]; then code=201; payload='{"apiKey":{"id":"fake-key-1"}}';
    else payload='{"apiKeys":[{"name":"chaos152-mysql-pre"}]}'; fi ;;
  */api/v1/audit/events*) payload='[{"target":"fake-key-1"}]' ;;
esac
printf '%s' "$payload" > "$ofile"
printf '%s|0.001\n' "$code"
EOF
chmod +x "$TMP/bin/curl"
set +e
bash "$TARGET" > "$TMP/negative.log" 2>&1
rc_neg=$?
set -e
[[ "$rc_neg" -ne 0 ]] || { echo "SELFTEST FAIL: readyz 假阳性竟判绿" >&2; exit 1; }
grep -Fq 'start mysql' "$FAKE_LOG" || { echo "SELFTEST FAIL: 判红后 trap 未恢复依赖" >&2; exit 1; }
grep -Fq '| FAIL |' "$TMP/artifacts/report.md" || { echo "SELFTEST FAIL: 报告未记录 FAIL" >&2; exit 1; }

echo "SELFTEST PASS"
