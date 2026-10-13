#!/usr/bin/env bash
# verify-release-artifacts.selftest.sh —— 自测 verify-release-artifacts.sh 第 ③ 项的分诊
#（「没发布」vs「门禁没凭证」vs「没能核对上」，2026-10-13 v0.14.0 事故）。
#
# 为什么这层分诊需要一份自己的自测：它的失效方式是**静默说错话**，两个方向都不靠别的门禁兜——
#   · 把 gh 无凭证（exit=4）报成「Release 不存在」⇒ 每次发布都有人去找一个不存在的发布事故
#     （v0.14.0 实测原样：③ 自诞生起恒红这一句，重跑也不变）；
#   · 把真 404（release not found）报成 UNVERIFIED ⇒ release.yml 的 wrapper 对 UNVERIFIED
#     是「warning 放行」，**真没发布反而变绿**——这个方向比第一种更危险。
# 所以两个方向各钉一条；另外补 assets=0 与正文缺「能力降级清单」两条既有判据（③ 整段这次
# 被重写过，不能只验新分支）。
#
# 实现手法（同 docker-pull-retry.selftest.sh）：假 gh / 假 curl / 假 helm 放进 PATH 最前，
# 让被测脚本**整个跑完**，断言 ③ 的逐行输出与总体退出码。夹具文案逐字取自本机实测：
#   gh 无凭证（exit=4）："To get started with GitHub CLI, please run:  gh auth login" /
#                        "Alternatively, populate the GH_TOKEN environment variable …"
#   gh 真 404（exit=1）："release not found"
# 假 curl 让 ①②⑤ 全绿（token/tags/manifest 三者自洽，含 sha256-<digest>.sig/.att），
# 假 helm 让 ④ 全绿——于是「全绿用例 rc=0」同时是 ①②④⑤ 整脚本的回归网。
#
# 退出码: 0 = 全部用例符合预期；1 = 判据与预期不符。

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLI="$HERE/verify-release-artifacts.sh"
if [ ! -r "$CLI" ]; then
    echo "  [FAIL] 找不到被测脚本 $CLI（判据无法自测，判红而不是跳过）"
    exit 1
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin"

VER=9.9.9
DIG="$(printf 'a%.0s' {1..64})"   # manifest digest（64 hex），tags 夹具里的 .sig/.att 与它同名
SHA="$(printf 'b%.0s' {1..40})"   # 发布提交（40 hex），第 ⑤ 项直接给 RELEASE_SHA 免得退到 git

# ── 假 gh ─────────────────────────────────────────────────────────────────────
# 分类调用（无 --json）按 $GH_QUEUE 队列逐条演出；--json 两类查询直接给成功形状
# （GH_ASSETS / GH_BODY 两个旋钮供 assets=0 与正文缺清单两条用例改写）。
cat > "$WORK/bin/gh" <<'FAKE'
#!/usr/bin/env bash
args="$*"
case "$args" in
    *"body | length"*) echo 42; exit 0 ;;
    *"--json body"*)   printf '%s\n' "${GH_BODY:-发布说明……能力降级清单……破坏性：无}"; exit 0 ;;
    *"--json assets"*) echo "${GH_ASSETS:-5}"; exit 0 ;;
esac
line="$(head -n1 "$GH_QUEUE")"
tail -n +2 "$GH_QUEUE" > "$GH_QUEUE.tmp" && mv "$GH_QUEUE.tmp" "$GH_QUEUE"
case "$line" in
    ok)   exit 0 ;;
    404:*) printf '%s\n' 'release not found' >&2; exit 1 ;;
    auth:*)
        printf '%s\n' 'To get started with GitHub CLI, please run:  gh auth login' >&2
        printf '%s\n' 'Alternatively, populate the GH_TOKEN environment variable with a GitHub API authentication token.' >&2
        exit 4 ;;
    err:*) printf '%s\n' "${line#err:}" >&2; exit 1 ;;
    *) echo "fake-gh: 队列已空（不应发生：说明被测脚本多调了一次分类查询）" >&2; exit 127 ;;
esac
FAKE

# ── 假 curl ───────────────────────────────────────────────────────────────────
# 伺候三类请求（按 URL 分派），输出遵守 ghcr_get 的 `-w $'\n%{http_code}'` 协议：
# body + 换行 + HTTP 码；manifest 走 -I（HEAD），把 digest 放在响应头里。
cat > "$WORK/bin/curl" <<FAKE
#!/usr/bin/env bash
url=""; head_mode=0
for a in "\$@"; do
    case "\$a" in
        -I) head_mode=1 ;;
        https://*) url="\$a" ;;
    esac
done
case "\$url" in
    *"/token"*)      printf '{"token":"fake"}\n200' ;;
    *"/tags/list"*)  printf '{"tags":["${VER}","sha256-${DIG}.sig","sha256-${DIG}.att"]}\n200' ;;
    *"/manifests/"*)
        if [ "\$head_mode" = 1 ]; then
            printf 'Docker-Content-Digest: sha256:${DIG}\n200'
        else
            printf '{}\n200'
        fi ;;
    *) printf '{"errors":[{"code":"DENIED"}]}\n403' ;;
esac
FAKE

# ── 假 helm ───────────────────────────────────────────────────────────────────
# 第 ④ 项要 `helm template` 渲染出 image 引用再与 ① 的集合比对；这里给两条都在
# 已发布集合里的引用（auth-svc / opsmesh-binary 都是 LEAVES 成员），且不含 :latest。
cat > "$WORK/bin/helm" <<FAKE
#!/usr/bin/env bash
printf '  image: ghcr.io/levango7/auth-svc:${VER}\n'
printf '  image: ghcr.io/levango7/opsmesh-binary:${VER}\n'
FAKE

chmod +x "$WORK/bin/gh" "$WORK/bin/curl" "$WORK/bin/helm"

PASS=0
FAIL=0
# run_case <名> <期望退出码> <期望行（grep -F 字面）> <gh 分类队列行...>
# PATH 刻意收窄到 $WORK/bin:/usr/bin:/bin：假 gh/helm 必须在最前，且本机真实的
# gh 登录态、helm 安装与否都不能渗进来（否则同一份自测在开发机/runner 上判据不同）。
run_case() {
    local name="$1" want_rc="$2" want_line="$3"; shift 3
    local queue="$WORK/ghq" item out rc
    : > "$queue"
    for item in "$@"; do printf '%s\n' "$item" >> "$queue"; done
    set +e
    out="$(GH_QUEUE="$queue" RELEASE_SHA="$SHA" \
        PATH="$WORK/bin:/usr/bin:/bin" bash "$CLI" "$VER" 2>&1)"
    rc=$?
    set -e
    if [ "$rc" = "$want_rc" ] && grep -qF -- "$want_line" <<<"$out"; then
        echo "  [PASS] ${name}（rc=${rc}）"
        PASS=$(( PASS + 1 ))
    else
        echo "  [FAIL] ${name}：rc=${rc}（want ${want_rc}）或缺期望行「${want_line}」"
        printf '%s\n' "$out" | sed 's/^/         | /' | tail -20
        FAIL=$(( FAIL + 1 ))
    fi
}

echo "== A. 全绿（假 curl/helm 让 ①②④⑤ 全绿，rc=0 同时是整脚本的回归网）=="
run_case "Release 正常（5 assets + 正文含清单）" 0 "[PASS] Release v9.9.9 存在且有 5 个 assets" ok

echo "== B. 真 404 必须判红成「不存在」（不许降级成 UNVERIFIED——那会被 wrapper 放行）=="
run_case "gh 确认 404" 1 "[FAIL] GitHub Release v9.9.9 不存在（gh 确认 404）" "404:x"

echo "== C. gh 无凭证（exit=4）判红且**指名病因**（门禁自身配置缺陷，不是发布缺陷）=="
run_case "gh 无凭证" 1 "门禁自身的配置缺陷" "auth:x"

echo "== D. 传输/未知失败 ⇒ UNVERIFIED（重跑本 job），不是发布事故 =="
run_case "gh 5xx 抖动" 1 "[UNVERIFIED] GitHub Release v9.9.9 查询失败" "err:HTTP 503: server error"
run_case "gh 未知失败（无文案）" 1 "[UNVERIFIED] GitHub Release v9.9.9 查询失败" "err:"

echo "== E. 既有判据不许在重写中丢失 =="
# 旋钮必须 export：假 gh 是**子进程**，只做前缀赋值它收不到（曾这样写错过，用例假绿/假红）。
export GH_ASSETS=0
run_case "assets=0（§19 形态）" 1 "存在但 **assets=0**（§19 那个形态" ok
unset GH_ASSETS
export GH_BODY='本版发布说明（故意没有那四个字）'
run_case "正文缺「能力降级清单」" 1 "正文没有「能力降级清单」" ok
unset GH_BODY

echo
if [ "$FAIL" -gt 0 ]; then
    echo "FAIL: ${FAIL} 个用例不符预期（PASS=${PASS}）"
    exit 1
fi
echo "PASS: 全部 ${PASS} 个用例符合预期"
exit 0
