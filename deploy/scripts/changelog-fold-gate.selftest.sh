#!/usr/bin/env bash
# changelog-fold-gate.selftest.sh —— 自测 deploy/scripts/changelog-fold-gate.py（TD-96）。
#
# 为什么判据要带走一份自测：这一节管的是**发布账**，它的两种失效都不报错——
#   ① 判据失明（该抓的漏归版不抓）⇒ 客户按 Keep-a-Changelog 读到的欠账是假的；
#   ② 豁免账变成后门（条目过期仍放行）⇒ 下一次真漏归版永远绿。
# 所以这里钉四类事：真 offender 必须判红、被豁免的必须放行且报条数、过期豁免必须判红、
# 以及**把判据改坏三个方向各跑一次**（边界比较反向、标题判据放松、变体识别退回旧写法）
# ——证明前面几条不是"无论怎么改都通过"的空跑。第三条是 2026-11-04 随「`[Unreleased-主题]`
# 变体标题对判据不可见」这个真实盲区一起加的：没有它，变体用例的红无法归因到变体识别。
#
# 退出码: 0 = 全部用例符合预期；1 = 有用例不符预期。

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLI="$HERE/changelog-fold-gate.py"

PY=""
for _c in python3 python; do
    if command -v "$_c" >/dev/null 2>&1; then PY="$_c"; break; fi
done
if [ -z "$PY" ]; then
    echo "  [FAIL] 本机/CI 没有 python 解释器，判据无法自测（判红而不是跳过）"
    exit 1
fi
if [ ! -r "$CLI" ]; then
    echo "  [FAIL] 找不到被测判据 $CLI（判红而不是跳过）"
    exit 1
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

PASS=0
FAIL=0

# 夹具：已发布版本 2026-10-13（= v0.14.0），正文由各用例追加。
mk_changelog() {  # $1=输出文件  $2=追加到版本节之后的块（可为空）
    {
        echo "# Changelog"
        echo ""
        echo "## [0.14.0] — 2026-10-13（汇总）"
        echo ""
        echo "- 已发布内容"
        if [ -n "${2:-}" ]; then printf '%s\n' "$2"; fi
    } > "$1"
}

run_case() {  # $1=名  $2=changelog  $3=豁免账  $4=期望退码  $5=SUMMARY 必须包含
    local name="$1" cl="$2" lg="$3" exp_rc="$4" exp_sub="$5"
    local out rc summary
    out="$("$PY" "$CLI" "$cl" "$lg" 2>&1)"
    rc=$?
    summary="$(printf '%s\n' "$out" | head -n1)"
    if [ "$rc" != "$exp_rc" ] || [[ "$summary" != *"$exp_sub"* ]]; then
        FAIL=$((FAIL + 1))
        printf '  [FAIL] %s: rc=%s（期望 %s）summary=%s（期望含 %s）\n' \
            "$name" "$rc" "$exp_rc" "$summary" "$exp_sub"
        printf '%s\n' "$out" | sed 's/^/         /'
    else
        PASS=$((PASS + 1))
        printf '  [PASS] %s: rc=%s | %s\n' "$name" "$rc" "$summary"
    fi
}

TITLE_EARLY='## [Unreleased] — 2026-10-12（模拟用户演练 A3：授权拒绝入审计链）'

# ① 干净：唯一的 [Unreleased] 块日期 = 发布日（合法未发版块，与当日发布并存）
mk_changelog "$WORK/cl_clean.md" '## [Unreleased] — 2026-10-13（当日的未发版块）'
printf '# 空账\n' > "$WORK/lg_empty.tsv"
run_case "干净（无 offender）" "$WORK/cl_clean.md" "$WORK/lg_empty.tsv" 0 \
    "released_max=2026-10-13 released_blocks=1 unreleased=1 exempt=0 offenders=0 stale=0"

# ② 真漏归版：日期早于发布日、账上又没有 ⇒ 必须判红并点名行号
mk_changelog "$WORK/cl_off.md" "$TITLE_EARLY"
run_case "漏归版必须判红" "$WORK/cl_off.md" "$WORK/lg_empty.tsv" 1 "offenders=1 stale=0"

# ③ 被豁免：同 ②，账上有该标题整行 ⇒ 放行，且 SUMMARY 报出豁免条数
mk_changelog "$WORK/cl_ex.md" "$TITLE_EARLY"
printf '# 账\n%s\t0.14.0 之后才落地\t2026-10-13\n' "$TITLE_EARLY" > "$WORK/lg_one.tsv"
run_case "tag 后落地的块被豁免" "$WORK/cl_ex.md" "$WORK/lg_one.tsv" 0 \
    "released_blocks=1 unreleased=1 exempt=1 offenders=0 stale=0"

# ④ 过期豁免（幽灵条目）：账里的标题在当前文件里不存在 ⇒ 判红（豁免不许变成后门）
mk_changelog "$WORK/cl_ghost.md" '## [Unreleased] — 2026-10-13（当日的未发版块）'
printf '# 账\n## [Unreleased] — 2020-01-01（早已归版删掉的块）\t过期\t2026-10-13\n' > "$WORK/lg_ghost.tsv"
run_case "过期豁免条目必须判红" "$WORK/cl_ghost.md" "$WORK/lg_ghost.tsv" 1 "offenders=0 stale=1"

# ⑤ 不必要的豁免：账里那条命中的块日期并不早于发布日 ⇒ 也不该在账上
mk_changelog "$WORK/cl_needless.md" '## [Unreleased] — 2026-10-13（当日的未发版块）'
printf '# 账\n## [Unreleased] — 2026-10-13（当日的未发版块）\t不需要\t2026-10-13\n' > "$WORK/lg_needless.tsv"
run_case "不必要的豁免条目也判红" "$WORK/cl_needless.md" "$WORK/lg_needless.tsv" 1 "stale=1"

# ⑥ [Unreleased] 标题没有日期 ⇒ 判红（无法判它是否该归版）
mk_changelog "$WORK/cl_nodate.md" '## [Unreleased] — 漏写日期的块'
run_case "标题无日期判红" "$WORK/cl_nodate.md" "$WORK/lg_empty.tsv" 1 "offenders=1"

# ⑦ 正文里的散文行（缩进 + 反引号）不得被当成标题：这是历史假阳性的原形状
# shellcheck disable=SC2016  # 夹具要的就是字面反引号（模拟正文散文行），不是命令替换
mk_changelog "$WORK/cl_prose.md" "$(printf '%s\n%s\n' \
    '## [Unreleased] — 2026-10-13（当日的未发版块）' \
    '  · 把 `## [Unreleased] — 2026-09-01（老块）` 一次性折叠进版本节')"
run_case "正文散文行不算标题" "$WORK/cl_prose.md" "$WORK/lg_empty.tsv" 0 \
    "released_blocks=1 unreleased=1 exempt=0 offenders=0 stale=0"

# ⑧ 变体标题（`[Unreleased-主题]`）早于发布日 ⇒ 必须判红，且被算进 unreleased 计数。
#    2026-11-04 的真实形状：main 上有 3 个 `[Unreleased-前端-*] — 日期` 块，旧判据一律看不见。
TITLE_VAR_EARLY='## [Unreleased-前端-裁决批] — 2026-10-12（变体标题，早于发布日）'
mk_changelog "$WORK/cl_var_off.md" "$TITLE_VAR_EARLY"
run_case "变体标题漏归版必须判红" "$WORK/cl_var_off.md" "$WORK/lg_empty.tsv" 1 \
    "unreleased=1 exempt=0 offenders=1 stale=0"

# ⑨ 变体标题与发布日同日 ⇒ 合法未发版块，放行且必须被计数（计数为 0 就说明判据又失明了）
mk_changelog "$WORK/cl_var_ok.md" '## [Unreleased-前端-K8s命名空间] — 2026-10-13（变体标题，同日）'
run_case "变体标题同日块放行且被计数" "$WORK/cl_var_ok.md" "$WORK/lg_empty.tsv" 0 \
    "released_blocks=1 unreleased=1 exempt=0 offenders=0 stale=0"

# ⑩ 变体标题没有日期 ⇒ 同样判红（无从判断它是否该归版）
mk_changelog "$WORK/cl_var_nodate.md" '## [Unreleased-主题名] — 漏写日期的变体块'
run_case "变体标题无日期判红" "$WORK/cl_var_nodate.md" "$WORK/lg_empty.tsv" 1 "offenders=1"

# ⑪ 变体标题也能进豁免账（TD-96 的机制对新形态同样有效，不是只给旧形态开门）
mk_changelog "$WORK/cl_var_ex.md" "$TITLE_VAR_EARLY"
printf '# 账\n%s\ttag 后落地\t2026-10-13\n' "$TITLE_VAR_EARLY" > "$WORK/lg_var.tsv"
run_case "变体标题可被豁免账放行" "$WORK/cl_var_ex.md" "$WORK/lg_var.tsv" 0 \
    "unreleased=1 exempt=1 offenders=0 stale=0"

# ⑫ 顶格散文行 `## [Unreleased]` 一次性改成 …`（`]` 后紧跟反引号）仍不得算标题
# shellcheck disable=SC2016  # 夹具要的就是字面反引号，不是命令替换
mk_changelog "$WORK/cl_prose2.md" "$(printf '%s\n%s\n' \
    '## [Unreleased-前端-系统运维] — 2026-10-13（同日变体块）' \
    '## [Unreleased]` 一次性改成 `## [0.12.0]`')"
run_case "顶格散文行不算标题" "$WORK/cl_prose2.md" "$WORK/lg_empty.tsv" 0 \
    "released_blocks=1 unreleased=1 exempt=0 offenders=0 stale=0"

# ⑬ 一个已发布版本标题都没有 ⇒ 判红（本节失去基准，不许静默放行）
{
    echo "# Changelog"
    echo ""
    echo "## [Unreleased] — 2026-10-12（没有基准时的块）"
} > "$WORK/cl_nobase.md"
run_case "无发布基准判红" "$WORK/cl_nobase.md" "$WORK/lg_empty.tsv" 1 "released_max=NONE"

# ⑭ 变异 A（边界比较反向）：`day >= cut` 改 `day > cut` ⇒ 发布日期当天的未发版块被误判成 offender
MUT_A="$WORK/mut_a.py"
sed 's/day >= cut/day > cut/' "$CLI" > "$MUT_A"
out_a="$("$PY" "$MUT_A" "$WORK/cl_clean.md" "$WORK/lg_empty.tsv" 2>&1)"
rc_a=$?
if [ "$rc_a" = "1" ]; then
    PASS=$((PASS + 1))
    printf '  [PASS] 变异A（day>=cut→day>cut）：干净夹具转红，边界比较是承重的\n'
else
    FAIL=$((FAIL + 1))
    printf '  [FAIL] 变异A：判据改坏后仍 rc=%s ⇒ 边界比较没被用例覆盖\n' "$rc_a"
    printf '%s\n' "$out_a" | sed 's/^/         /'
fi

# ⑮ 变异 B（标题判据放松）：把「按正则判标题」改成子串包含 ⇒ 正文散文行被误判成标题
MUT_B="$WORK/mut_b.py"
sed 's/    return bool(UNRELEASED_HEADING.match(line))/    return "## [Unreleased]" in line/' \
    "$CLI" > "$MUT_B"
if cmp -s "$MUT_B" "$CLI"; then
    FAIL=$((FAIL + 1))
    printf '  [FAIL] 变异B：sed 没改到判据（锚点失配），该变异用例无效\n'
else
    out_b="$("$PY" "$MUT_B" "$WORK/cl_prose.md" "$WORK/lg_empty.tsv" 2>&1)"
    rc_b=$?
    if [ "$rc_b" = "1" ]; then
        PASS=$((PASS + 1))
        printf '  [PASS] 变异B（标题判据放松）：散文行夹具转红，严格标题规则承重\n'
    else
        FAIL=$((FAIL + 1))
        printf '  [FAIL] 变异B：放松标题判据后散文夹具仍 rc=%s ⇒ 严格性没被用例覆盖\n' "$rc_b"
        printf '%s\n' "$out_b" | sed 's/^/         /'
    fi
fi

# ⑯ 变异 C（变体识别退回旧写法）：把 `[^\]]*` 去掉 ⇒ 判据重新对 `[Unreleased-主题]` 失明。
#    这条是本轮改动的**承重证明**：真实判据对「变体标题早于发布日」判红（用例⑧），
#    退回旧写法的变异必须判绿——否则用例⑧的红与变体识别无关，改动等于没射程。
MUT_C="$WORK/mut_c.py"
"$PY" - "$CLI" "$MUT_C" <<'PY'
import io
import sys

src, dst = sys.argv[1], sys.argv[2]
text = io.open(src, encoding="utf-8").read()
old = r'^## \[Unreleased[^\]]*\]( — |$)'
new = r'^## \[Unreleased\]( — |$)'
if old not in text:
    sys.exit(4)
io.open(dst, "w", encoding="utf-8", newline="\n").write(text.replace(old, new))
PY
rc_gen_c=$?
if [ "$rc_gen_c" != "0" ]; then
    FAIL=$((FAIL + 1))
    printf '  [FAIL] 变异C：判据里找不到变体正则锚点（^## \\[Unreleased[^\\]]*\\]），该变异用例无效\n'
else
    out_c="$("$PY" "$MUT_C" "$WORK/cl_var_off.md" "$WORK/lg_empty.tsv" 2>&1)"
    rc_c=$?
    out_real="$("$PY" "$CLI" "$WORK/cl_var_off.md" "$WORK/lg_empty.tsv" 2>&1)"
    rc_real=$?
    if [ "$rc_c" = "0" ] && [ "$rc_real" = "1" ]; then
        PASS=$((PASS + 1))
        printf '  [PASS] 变异C（去掉变体识别）：变体漏归版夹具由红转绿，射程确实落在变体标题上\n'
    else
        FAIL=$((FAIL + 1))
        printf '  [FAIL] 变异C：期望变异 rc=0 且真实判据 rc=1，实测 rc=%s / rc=%s\n' "$rc_c" "$rc_real"
        printf '%s\n' "$out_c" | sed 's/^/         变异: /'
        printf '%s\n' "$out_real" | sed 's/^/         真实: /'
    fi
fi

echo
if [ "$FAIL" -gt 0 ]; then
    echo "FAIL: ${FAIL} 个用例不符预期（PASS=${PASS}）"
    exit 1
fi
echo "PASS: 全部 ${PASS} 个用例符合预期"
exit 0
