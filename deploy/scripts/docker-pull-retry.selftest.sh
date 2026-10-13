#!/usr/bin/env bash
# docker-pull-retry.selftest.sh —— 自测 deploy/scripts/docker-pull-retry.sh 的判据（TD-94）。
#
# 为什么判据要有一份自己的自测：这类"选择哪种失败重试"的逻辑，失效方式几乎都是
# **静默反着来**——该重试的不重试（限流照旧判红，缓解白做），或不该重试的乱重试
# （真错误被拖成三倍时长，且掩盖 manifest unknown 这类确定性故障）。这里同时钉：
#   A. 限流/传输抖动必须重试（阳性对照，两条真实文案形状）；
#   B. 真实错误（manifest unknown / 参数错误）必须立即失败、一次都不多重试；
#   C. 预算有界：耗尽即败，调用次数可数（不变成无限重试）；
#   D. 夹具自身仍是 docker 的真实输出形状（逐字摘自 TD-89 实测证据与 docker 标准文案），
#      不跟著实现一起说谎。
#
# 实现手法：把假 `docker` 放到 PATH 最前，按队列文件逐条"演出"（ok / 429 / 真错误），
# 并用调用计数文件断言"重试了几次"——判据的可观测行为就是调用次数与退出码。
#
# 退出码: 0 = 全部用例符合预期；1 = 判据与预期不符。

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLI="$HERE/docker-pull-retry.sh"

if [ ! -r "$CLI" ]; then
    echo "  [FAIL] 找不到被测脚本 $CLI（判据无法自测，判红而不是跳过）"
    exit 1
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# ── 假 docker：按 $QUEUE 队列演出，每次调用把参数追写到 $CALLS ──────────────
# 队列元素（每行一条，按序消费）：
#   ok                       —— exit 0
#   429:<文本>               —— exit 1，输出 <文本>（限流文案）
#   err:<文本>               —— exit 1，输出 <文本>（真实错误文案）
mkdir -p "$WORK/bin"
cat > "$WORK/bin/docker" <<'FAKE'
#!/usr/bin/env bash
echo "$*" >> "$CALLS"
line="$(head -n1 "$QUEUE")"
tail -n +2 "$QUEUE" > "$QUEUE.tmp" && mv "$QUEUE.tmp" "$QUEUE"
case "$line" in
    ok)  echo "Status: Downloaded newer image for $2"; exit 0 ;;
    429:*) printf '%s\n' "${line#429:}" >&2; exit 1 ;;
    err:*) printf '%s\n' "${line#err:}" >&2; exit 1 ;;
    *) echo "fake-docker: 队列已空（不应发生：说明重试次数超出队列编排）" >&2; exit 127 ;;
esac
FAKE
chmod +x "$WORK/bin/docker"

# 真实文案形状（TD-89 实测证据 + docker 标准 not found 文案）
TXT_429='error pulling image configuration: download failed after attempts=1: unexpected status from HEAD request to https://registry-1.docker.io/v2/library/golang/manifests/1.26-bookworm: 429 Too Many Requests'
TXT_TOKEN500='Head "https://auth.docker.io/token?scope=repository:library/mysql:pull&service=registry.docker.io": unexpected status: 500 Internal Server Error'
TXT_UNKNOWN='Error response from daemon: manifest for golang:1.26-nope-not-exist not found: manifest unknown: manifest unknown'

PASS=0
FAIL=0
# run_case <名> <期望退出码> <期望调用次数> <镜像参数字符串> <队列内容...>
# 第四参用字符串而非数组：空串 = 不给镜像参数（覆盖 usage 用例）；单词分隔靠无引号展开
#（有意为之，shellcheck 豁免见下）。
run_case() {
    local name="$1" want_rc="$2" want_calls="$3" args_str="$4"; shift 4
    local queue="$WORK/queue" calls="$WORK/calls"
    : > "$queue"
    for item in "$@"; do printf '%s\n' "$item" >> "$queue"; done
    : > "$calls"
    set +e
    # shellcheck disable=SC2086  # 第四参故意无引号展开（空串⇒无参数；多词⇒多个镜像）
    CALLS="$calls" QUEUE="$queue" PATH="$WORK/bin:$PATH" \
        bash "$CLI" $args_str >"$WORK/out" 2>&1
    local rc=$?
    set -e
    local calls_n; calls_n="$(grep -c '' "$calls" || true)"
    if [ "$rc" = "$want_rc" ] && [ "$calls_n" = "$want_calls" ]; then
        echo "  [PASS] ${name} (rc=${rc}, calls=${calls_n})"
        PASS=$(( PASS + 1 ))
    else
        echo "  [FAIL] ${name}：rc=${rc}（want ${want_rc}）, calls=${calls_n}（want ${want_calls}）"
        sed 's/^/         | /' "$WORK/out" | head -5
        FAIL=$(( FAIL + 1 ))
    fi
}

export PULL_RETRY_SLEEPS="0 0"   # 自测不真睡
IMG="golang:1.26-bookworm"

echo "== A. 阳性：限流/抖动必须重试 =="
run_case "首次成功不重试"          0 1 "$IMG"          "ok"
run_case "429→重试→成功"          0 2 "$IMG"          "429:${TXT_429}" "ok"
run_case "429→429→成功（预算内）" 0 3 "$IMG"          "429:${TXT_429}" "429:${TXT_429}" "ok"
run_case "token 500→重试→成功"    0 2 "$IMG"          "429:${TXT_TOKEN500}" "ok"

echo "== B. 阴性：真实错误不重试 =="
run_case "manifest unknown 一次即败" 1 1 "$IMG"        "err:${TXT_UNKNOWN}"

echo "== C. 有界：耗尽即败 =="
run_case "429×3 耗尽判红"         1 3 "$IMG"          "429:${TXT_429}" "429:${TXT_429}" "429:${TXT_429}"

echo "== D. 多镜像独立预算 =="
run_case "第一个抖动后成功、第二个首次成功" 0 3 "golang:1.26-bookworm debian:bookworm-slim" "429:${TXT_429}" "ok" "ok"

echo "== E. 用法/参数错误按判红 =="
run_case "无镜像参数 ⇒ rc=2"       2 0 ""              "ok"
export PULL_RETRY_MAX="abc"
run_case "MAX 非正整数 ⇒ rc=2"      2 0 "$IMG"          "ok"
unset PULL_RETRY_MAX

echo
if [ "$FAIL" -gt 0 ]; then
    echo "FAIL: ${FAIL} 个用例不符预期（PASS=${PASS}）"
    exit 1
fi
echo "PASS: 全部 ${PASS} 个用例符合预期"
exit 0
