#!/usr/bin/env bash
# chaos-dependency-down.sh 的离线判据自测：不接触真实 Docker/网络。
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
TARGET="$SCRIPT_DIR/chaos-dependency-down.sh"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/bin"
: > "$TMP/compose.yaml"
export FAKE_LOG="$TMP/docker.log"
export FAKE_STATE="$TMP/state"
export FAKE_READY_WONT_FAIL=0

cat > "$TMP/bin/docker" <<'EOF'
#!/usr/bin/env bash
printf 'docker %s\n' "$*" >> "$FAKE_LOG"
if [[ "$1" == "compose" ]]; then
  case " $* " in
    *" ps -q "*) printf 'fake-container\n' ;;
    *" stop "*) printf 'down\n' > "$FAKE_STATE" ;;
    *" start "*) printf 'up\n' > "$FAKE_STATE" ;;
  esac
  exit 0
fi
if [[ "$1" == "inspect" ]]; then
  printf 'true\n'
  exit 0
fi
exit 64
EOF

cat > "$TMP/bin/curl" <<'EOF'
#!/usr/bin/env bash
url="${!#}"
state=$(cat "$FAKE_STATE" 2>/dev/null || printf 'up')
code=200
if [[ "$url" == *"/readyz" && "$state" == "down" && "$FAKE_READY_WONT_FAIL" != "1" ]]; then
  code=503
fi
printf '%s|0.001\n' "$code"
EOF
chmod +x "$TMP/bin/docker" "$TMP/bin/curl"
export PATH="$TMP/bin:$PATH"
export OPSMESH_CHAOS_PROJECT=chaos-selftest
export OPSMESH_CHAOS_COMPOSE_FILE="$TMP/compose.yaml"
export OPSMESH_CHAOS_BASE_URL=http://example.invalid
export OPSMESH_CHAOS_HTTP_TIMEOUT=1
export OPSMESH_CHAOS_RECOVERY_TIMEOUT=2
export OPSMESH_CHAOS_DEGRADED_TIMEOUT=1

printf 'up\n' > "$FAKE_STATE"
: > "$FAKE_LOG"
bash "$TARGET" mysql > "$TMP/positive.log"
grep -Fq 'stop mysql' "$FAKE_LOG"
grep -Fq 'start mysql' "$FAKE_LOG"
grep -Fq 'PASS: mysql 故障注入与恢复完成' "$TMP/positive.log"

# readiness 在依赖停止后仍假装 200 ⇒ 必须判红，且 EXIT trap 仍要恢复依赖。
printf 'up\n' > "$FAKE_STATE"
: > "$FAKE_LOG"
export FAKE_READY_WONT_FAIL=1
if bash "$TARGET" mysql > "$TMP/negative.log" 2>&1; then
  echo 'FAIL: readiness 假阳性未被判红' >&2
  exit 1
fi
grep -Fq 'FAIL: /readyz 在 1s 内未达到 503' "$TMP/negative.log"
grep -Fq 'start mysql' "$FAKE_LOG"

printf 'PASS: 正向恢复 + readiness 假阳性判红 + 失败自动恢复（离线假 docker/curl）\n'
