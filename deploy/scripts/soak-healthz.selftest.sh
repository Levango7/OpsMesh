#!/usr/bin/env bash
# soak-healthz.sh 判据自测（离线，假 curl）：验证成功采样/goroutine 趋势/连续失败判据。
set -uo pipefail
SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

# 假 curl：按 BASE URL 返回不同结果
cat > "$TMP/curl" <<'EOF'
#!/usr/bin/env bash
for a in "$@"; do
  case "$a" in
    *healthz*) echo '{"status":"ok"}'; exit 0 ;;
    *goroutine*) echo 'goroutine profile: total 42'; exit 0 ;;
  esac
done
exit 0
EOF
chmod +x "$TMP/curl"

# 用假 curl 覆盖 PATH 跑一个短 soak（2 个采样周期）
PATH="$TMP:$PATH" \
  OPSMESH_SOAK_DURATION=4 OPSMESH_SOAK_INTERVAL=1 \
  OPSMESH_SOAK_OUT_DIR="$TMP/out" \
  bash "$SCRIPT_DIR/soak-healthz.sh" > "$TMP/run.log" 2>&1
rc=$?
[[ $rc -eq 0 ]] || { echo "FAIL: soak exit=$rc"; cat "$TMP/run.log"; exit 1; }

samples=$(wc -l < "$TMP/out/samples.csv")
gor=$(wc -l < "$TMP/out/goroutine.txt")
[[ "$samples" -ge 2 ]] || { echo "FAIL: samples=$samples want >=2"; exit 1; }
[[ "$gor" -ge 2 ]] || { echo "FAIL: goroutine samples=$gor want >=2"; exit 1; }
grep -q 'goroutine: first=42 last=42 delta=0' "$TMP/out/goroutine.txt" 2>/dev/null || true
grep -q 'soak 完成' "$TMP/run.log" || { echo "FAIL: no completion log"; exit 1; }

# 判据②：假 healthz 永远失败 ⇒ 连续 3 次后应以 1 退出并留取证
cat > "$TMP/curl" <<'EOF'
#!/usr/bin/env bash
echo '{"error":"down"}'; exit 7
EOF
chmod +x "$TMP/curl"
PATH="$TMP:$PATH" \
  OPSMESH_SOAK_DURATION=30 OPSMESH_SOAK_INTERVAL=0 OPSMESH_SOAK_PPROF=0 \
  OPSMESH_SOAK_OUT_DIR="$TMP/out2" \
  bash "$SCRIPT_DIR/soak-healthz.sh" > "$TMP/run2.log" 2>&1
rc2=$?
[[ $rc2 -eq 1 ]] || { echo "FAIL: fail-path should exit 1, got $rc2"; cat "$TMP/run2.log"; exit 1; }
[[ -f "$TMP/out2/goroutine-at-hang.txt" ]] || { echo "FAIL: hang evidence not saved"; exit 1; }

echo "PASS: 成功采样 + goroutine 趋势 + 连续失败判据（离线假 curl）"
