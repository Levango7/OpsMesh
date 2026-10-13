#!/usr/bin/env bash
# wait-tag-ci.selftest.sh —— 自测 wait-tag-ci.sh 的分诊（TD-95 方向②）。
#
# 为什么要有：这个脚本的失效方式同样是**静默说错话**，两个方向都会骗人：
#   · 把"查询失败 / run 还没出现"当成"run 失败" ⇒ 每次发版判红并指一条好 run（狼来了）；
#   · 把"run 失败"当成"未知" ⇒ 产物不会来却只记 warning，退回盲目等待——正是 TD-95 要治的病。
# 两条各钉一个用例；另钉"成功收敛"与"超预算仍未收敛（未知）"。
#
# 手法：假 `gh` 按 $GH_QUEUE 队列逐行演出；**先 unset -f gh**——bash 的命令解析是
# 函数先于 PATH，继承进来的同名函数会盖过夹具（CI 的 security job 对 helm 就是这么干的，
# 2026-10-13 b32dc8b 实测被它咬过一次，见板第八十则）。
#
# 退出码: 0 = 全部用例符合预期；1 = 判据与预期不符。
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLI="$HERE/wait-tag-ci.sh"
if [ ! -r "$CLI" ]; then
    echo "  [FAIL] 找不到被测脚本 $CLI（判据无法自测，判红而不是跳过）"
    exit 1
fi
unset -f gh 2>/dev/null || true

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin"

# 假 gh：按队列演出 latest_tag_run 的四种输出形状（NONE / in_progress / success / failure / err:）
cat > "$WORK/bin/gh" <<'FAKE'
#!/usr/bin/env bash
line="$(head -n1 "$GH_QUEUE")"
tail -n +2 "$GH_QUEUE" > "$GH_QUEUE.tmp" && mv "$GH_QUEUE.tmp" "$GH_QUEUE"
case "$line" in
    NONE)        echo "NONE" ;;
    in_progress) printf '111\tin_progress\t\thttps://example.invalid/run/111\n' ;;
    success)     printf '111\tcompleted\tsuccess\thttps://example.invalid/run/111\n' ;;
    failure)     printf '111\tcompleted\tfailure\thttps://example.invalid/run/111\n' ;;
    err:*)       printf '%s\n' "${line#err:}" >&2; exit 1 ;;
    *) echo "fake-gh: 队列已空（不应发生：说明轮询次数超出队列编排）" >&2; exit 127 ;;
esac
FAKE
chmod +x "$WORK/bin/gh"

PASS=0
FAIL=0
# run_case <名> <期望 rc> <期望行（grep -F）> <队列行...>
# WAIT_CI_TICKS=5 / SLEEP=0：轮询逻辑不变、不真睡（五个 tick 足够覆盖全部用例形状）
run_case() {
    local name="$1" want_rc="$2" want_line="$3"; shift 3
    local queue="$WORK/ghq" item out rc
    : > "$queue"
    for item in "$@"; do printf '%s\n' "$item" >> "$queue"; done
    set +e
    out="$(GH_QUEUE="$queue" WAIT_CI_TICKS=5 WAIT_CI_SLEEP=0 \
        PATH="$WORK/bin:/usr/bin:/bin" bash "$CLI" 0.14.0 f4968bb01f8793df4b2ca550dde6e2a7b1e104ad 2>&1)"
    rc=$?
    set -e
    if [ "$rc" = "$want_rc" ] && grep -qF -- "$want_line" <<<"$out"; then
        echo "  [PASS] ${name}（rc=${rc}）"
        PASS=$(( PASS + 1 ))
    else
        echo "  [FAIL] ${name}：rc=${rc}（want ${want_rc}）或缺期望行「${want_line}」"
        printf '%s\n' "$out" | sed 's/^/         | /'
        FAIL=$(( FAIL + 1 ))
    fi
}

echo "== A. run 还没出现 → 跑着 → 成功收敛（rc=0）=="
run_case "NONE→in_progress→success" 0 "已成功收敛" NONE in_progress success

echo "== B. 终态失败必须判红（rc=1）且把 run 链接指出来 =="
run_case "completed/failure" 1 "https://example.invalid/run/111" failure

echo "== C. 查询失败 ⇒ 未知（rc=2），不许说成 run 失败 =="
run_case "gh 查询连续失败" 2 "查询失败" \
    "err:HTTP 401: Bad credentials" "err:HTTP 401: Bad credentials" \
    "err:HTTP 401: Bad credentials" "err:HTTP 401: Bad credentials" "err:HTTP 401: Bad credentials"

echo "== D. 预算耗尽仍未收敛 ⇒ 未知（rc=2），不许判红 =="
run_case "超预算未收敛" 2 "按「未知」处理" in_progress in_progress in_progress in_progress in_progress

echo
if [ "$FAIL" -gt 0 ]; then
    echo "FAIL: ${FAIL} 个用例不符预期（PASS=${PASS}）"
    exit 1
fi
echo "PASS: 全部 ${PASS} 个用例符合预期"
exit 0
