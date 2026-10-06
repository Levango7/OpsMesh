#!/usr/bin/env bash
# ci-infra-death.sh —— 判断「go test 批次非零退出」属于**基础设施型死亡**（可重试一次），
# 还是真实失败（必须判红）。被 .github/workflows/ci.yml 的 build-test/run_batch 调用。
#
# 为什么要换掉原来的判据（TD-80，2026-10-06）：旧判据是两条**历史报错字符串**的白名单
# （`fatal error: runtime: (cannot allocate memory|out of memory)` 与 ThreadSanitizer 那句）。
# run 37414728836 attempt 1 里 `-race` 批 agent_JZ 在**全部用例通过、测试二进制已打印 PASS**
# 之后才 SIGSEGV 于 runtime.(*spanQueue).tryDrain（mgcmark_greenteagc.go:520），
# 文本不命中 ⇒ 被判成确定性失败 ⇒ 连带 11 个下游 job skip；同一 sha 的 attempt 2 十二个 job 全绿
# ⇒ 非确定性。峰值 RSS 260MB、MemAvailable=14952MB ⇒ 与宿主内存无关。
#
# 换成按**现象**判定，三条同时成立才重试：
#   ① 用例跑完了。可见形态按实测分两种：单包 `-v` 腿有测试二进制自己打印的裸 `PASS` 行；
#      单包非 `-v` 与多包腿**没有**那行（go 把子进程输出收走，只打印结果行）⇒ 认 `^ok` 行。
#      本机实测（go1.26.6）：单包 -v 裸 PASS 命中 1、单包非 -v 命中 0、多包非 -v 命中 0。
#      只认 PASS 会让 -race 各腿的重试变成永不触发的空转判据。
#      ⚠️ ① 只是必要条件，不是充分条件：runtime 的崩溃输出与二进制的输出写同一个 fd 且不加锁，
#      实测出现过 `fatal error: PASS` / `concurrent map writes` 这种被劈开的形态，
#      而 go 仍然打印了 `ok  <pkg>`（退出码还是 0）——真正把"业务缺陷当基础设施抖动"挡住的是 ②。
#   ② 崩在 runtime 里。崩溃头有两种形态：`fatal error: …`，以及**裸信号行** `SIGSEGV: …`
#      （真实那次就是后者，整份日志里 `^fatal error: ` 计数为 0 —— 只认 fatal error 等于把
#      这条门禁写成死代码）。崩溃段（头之后第一段落）的**每一个栈帧**都必须属 runtime 包；
#      只看顶帧不够——map 并发写这类产品缺陷的顶帧也是 internal/runtime/*，第二段才是业务帧。
#   ③ 没有任何测试级失败：日志里没有 `--- FAIL:`（含子测试的缩进形态）。
#      断言失败与 panic 前都会打印它，所以这一条把"重试掩盖真实回归"挡在门外。
#      实测：真实的 nil 解引用 panic 会先打印 `--- FAIL: TestNilDeref` 再打 panic 栈，
#      所以 ③ 是真能挡住的（不是推测）。
#
# 未覆盖的形态（有意保守，不是遗漏）：单包 `-v` 腿在跑完**之前**就 ENOMEM ⇒ ① 不成立 ⇒ 判红。
# 这类随机红仍会吞掉下游；如果它再次出现，应当补的是"跑到过多少用例"这一维的证据，
# 而不是把 ③ 放宽。
#
# 用法: ci-infra-death.sh <批次日志文件>
# 退出码: 0 = 基础设施型死亡（调用方可重试一次，且必须留痕）
#         1 = 真实失败，判红
#         2 = 用法/读取错误 —— 调用方必须当作判红：判据失明不等于干净
# stdout: 一行判定与依据，供 CI 日志与 step summary 留痕。

set -euo pipefail

LOG="${1:-}"
if [ -z "$LOG" ]; then
    echo "usage: $0 <批次日志文件>" >&2
    exit 2
fi
if [ ! -r "$LOG" ]; then
    echo "red: 读不到日志文件 ${LOG}（判红而不是放行——判据失明不等于干净）"
    exit 2
fi

# ── ③ 测试级失败一律判红 ─────────────────────────────────────────────
if grep -qE -- '^[[:space:]]*--- FAIL:' "$LOG"; then
    echo "red: 日志含 --- FAIL:（测试级失败，重试只会掩盖真实回归）"
    exit 1
fi

# ── 具名例外：ThreadSanitizer 自己的分配器 OOM ────────────────────────
# -race 腿里 TSan 报 OOM 时没有 Go 运行时的崩溃头与栈，只能按具名文本识别；
# 它是机器级失败（③ 已挡住"伴随真实失败"的情形）。
# ⚠️ 必须**行首锚定**（可带 TSan 的 `==PID==` / `ERROR: ` 前缀）：不锚定时，
# 日志里被回显的**源码文本**（例如旧判据那行 OOM_PAT='…|ThreadSanitizer: internal
# allocator is out of memory'）会先命中，于是把一次 GC 里的 SIGSEGV 报成"TSan OOM"——
# 结论方向碰巧没错，依据是假的。这正是"扫描器命中自己"的同族，实测于 run 37414728836 的整步骤日志。
if grep -qE '^[[:space:]]*(==[0-9]+==)?(ERROR: )?ThreadSanitizer: internal allocator is out of memory' "$LOG"; then
    echo "retry: ThreadSanitizer 分配器 OOM（机器级，非断言失败）"
    exit 0
fi

# ── ① 用例跑完的证据 ────────────────────────────────────────────────
if ! grep -qE '^PASS$' "$LOG" && ! grep -qE '^ok[[:space:]]' "$LOG"; then
    echo "red: 没有「用例跑完」的证据（既无裸 PASS 行也无 ok 结果行）——不在半途崩溃的批次上重试"
    exit 1
fi

# ── ② 崩溃头 + 崩溃栈归属 ────────────────────────────────────────────
HEAD_RE='^(fatal error: |SIG[A-Z]+: )'
if ! grep -qE "$HEAD_RE" "$LOG"; then
    echo "red: 非零退出但没有运行时崩溃头（既非 'fatal error:' 也非裸 'SIGxxx:'）——按真实失败处理"
    exit 1
fi

# 取"崩溃栈那一段"的全部栈帧并逐帧判归属，而不是只看顶帧：
# 真基础设施型死亡的崩溃段全是 runtime（实测 run 37414728836 的 goroutine 0 [idle] 段：
# tryDrain/drain/put/tryDeferToSpanScan/scanObjectsSmall/systemstack 全属 runtime）；
# 而 map 并发写这类**产品级**缺陷的崩溃段是 internal/runtime/maps.fatal 紧跟业务帧
# （本机实测：internal/runtime/maps.fatal → tmpfailprobe/d.Hammer）。
# 只认顶帧会把后者判成"可重试"——那正是"重试掩盖真实回归"的形态，
# 且它是否被 ① 挡住取决于崩溃时机，不该把安全性押在时序上。
STATS="$(awk -v re="$HEAD_RE" '
    $0 ~ re              { seen = 1; next }
    !seen                { next }
    n > 0 && /^[[:space:]]*$/  { exit }
    n > 0 && /^goroutine /     { exit }
    n > 0 && /^created by /    { next }
    /^[A-Za-z_][A-Za-z0-9_./]*\(/ {
        n++
        if ($0 !~ /^(runtime\.|runtime\/|internal\/runtime)/) {
            bad++
            if (firstbad == "") firstbad = $0
        }
    }
    END { printf "%d %d %s\n", n, bad + 0, firstbad }
' "$LOG")"

FRAMES="$(cut -d' ' -f1 <<<"$STATS")"
BADFRAMES="$(cut -d' ' -f2 <<<"$STATS")"
FIRSTBAD="$(cut -d' ' -f3- <<<"$STATS")"
FIRSTBAD="${FIRSTBAD%%(*}"
FIRSTBAD="${FIRSTBAD%.}"

if [ "$FRAMES" -eq 0 ]; then
    echo "red: 有崩溃头但崩溃段取不到栈帧——判据无法定位故障归属，按真实失败处理"
    exit 1
fi

if [ "$BADFRAMES" -gt 0 ]; then
    echo "red: 崩溃段含非 runtime 帧（首个：${FIRSTBAD:-未知}）——业务侧崩溃必须判红"
    exit 1
fi

echo "retry: 用例跑完后整段崩溃栈都在 runtime 内（帧数 ${FRAMES}）——基础设施型死亡"
exit 0
