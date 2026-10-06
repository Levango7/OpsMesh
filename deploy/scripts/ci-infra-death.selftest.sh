#!/usr/bin/env bash
# ci-infra-death.selftest.sh —— 自测 deploy/scripts/ci-infra-death.sh 的判据（TD-80）。
#
# 为什么判据要有一份自己的自测：这类"日志形状匹配"的失效方式几乎都是**静默不命中**——
# 旧判据（报错文本白名单）红了一次就吞掉 11 条下游腿，而没人会去查一条"看起来在跑"的重试逻辑。
# 所以这里同时钉三件事：
#   A. 该重试的形态必须重试（阳性对照）；
#   B. 该判红的形态必须判红（否则重试就在掩盖真实回归）；
#   C. **夹具自身的形态**必须仍然是"真实日志的那两种崩溃头 + 两种跑完证据"，
#      否则夹具会跟着实现一起说谎（本仓踩过：正则只认 const 块内缩进，独立 const 行被漏掉，
#      于是"三向对账"里有一向其实不存在）。
#
# 夹具 1 是 run 37414728836 attempt 1 里 agent_JZ 崩溃段的**逐字摘录**（去掉 GitHub 日志的
# 时间戳前缀），不是编造的形态。
#
# 退出码: 0 = 全部用例符合预期；1 = 判据与预期不符。

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLI="$HERE/ci-infra-death.sh"

if [ ! -r "$CLI" ]; then
    echo "  [FAIL] 找不到被测脚本 $CLI（判据无法自测，判红而不是跳过）"
    exit 1
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# ── 夹具 1：真实崩溃段（裸 SIGSEGV 头，PASS 之后，顶帧属 runtime）⇒ 应重试 ──────
cat > "$WORK/real-after-pass.log" <<'EOF'
=== RUN   TestTaskTimeoutFor_NegativeTimeout
--- PASS: TestTaskTimeoutFor_NegativeTimeout (0.00s)
PASS
SIGSEGV: segmentation violation
PC=0x439c7d m=5 sigcode=1 addr=0x0

goroutine 0 gp=0x6e4378041e0 m=5 mp=0x6e437800008 [idle]:
runtime.(*spanQueue).tryDrain(0x100000400?, 0x5175ffcb68?, 0x75ffcbb8?)
	/opt/hostedtoolcache/go/1.26.6/x64/src/runtime/mgcmark_greenteagc.go:520 +0x5d fp=0x7f1e75ffcb38 sp=0x7f1e75ffcb18 pc=0x439c7d
runtime.(*spanQueue).drain(0x6e43774f278, 0x8)
	/opt/hostedtoolcache/go/1.26.6/x64/src/runtime/mgcmark_greenteagc.go:473 +0x66 fp=0x7f1e75ffcb68 sp=0x7f1e75ffcb38 pc=0x439b66
runtime.systemstack()
FAIL	github.com/Levango7/OpsMesh/internal/agent	20.044s
FAIL
EOF

# ── 夹具 2：同一崩溃段但有用例真失败 ⇒ 必须判红（TD-80 指定的变异用例）──────────
cat > "$WORK/after-pass-with-fail.log" <<'EOF'
=== RUN   TestInvoke_MultiAddrsFailover
--- FAIL: TestInvoke_MultiAddrsFailover (0.01s)
    invoke_test.go:88: 期望 3 次重试，实际 1 次
PASS
SIGSEGV: segmentation violation
PC=0x439c7d m=5 sigcode=1 addr=0x0

goroutine 0 gp=0x6e4378041e0 m=5 [idle]:
runtime.(*spanQueue).tryDrain(0x100000400?)
FAIL	github.com/Levango7/OpsMesh/internal/agent	20.044s
FAIL
EOF

# ── 夹具 3：子测试形态的失败（带缩进）⇒ ③ 不能被缩进绕过 ────────────────────────
cat > "$WORK/subtest-indent-fail.log" <<'EOF'
=== RUN   TestTaskTimeoutFor_TaskLevel
=== RUN   TestTaskTimeoutFor_TaskLevel/timeout=10
    --- FAIL: TestTaskTimeoutFor_TaskLevel/timeout=10 (0.00s)
PASS
SIGSEGV: segmentation violation
PC=0x439c7d m=5 sigcode=1 addr=0x0
runtime.(*spanQueue).tryDrain(0x100000400?)
FAIL	github.com/Levango7/OpsMesh/internal/agent	20.044s
EOF

# ── 夹具 4：Go 运行时 ENOMEM（fatal error 头，顶帧 runtime.throw）⇒ 应重试 ──────
cat > "$WORK/enomem-after-pass.log" <<'EOF'
--- PASS: TestAgentBatch_Large (1.20s)
PASS
fatal error: runtime: out of memory

runtime stack:
runtime.throw({0xabcd?, 0xef01?})
	/usr/local/go/src/runtime/panic.go:1067 +0x45 fp=0x7ffd sp=0x7ff8 pc=0x48fc8e
runtime.sysMap()
FAIL	github.com/Levango7/OpsMesh/internal/agent	30.000s
EOF

# ── 夹具 5：业务侧崩溃（顶帧不属 runtime）⇒ 必须判红 ───────────────────────────
cat > "$WORK/business-crash.log" <<'EOF'
=== RUN   TestCollector_Tick
--- PASS: TestCollector_Tick (0.00s)
PASS
fatal error: unexpected signal during runtime execution

goroutine 21 [running]:
github.com/Levango7/OpsMesh/internal/agent.(*Collector).Tick(0xc0001?)
	/home/runner/work/OpsMesh/OpsMesh/internal/agent/collector.go:118 +0x2f
testing.tRunner(0xc0001, 0x9)
FAIL	github.com/Levango7/OpsMesh/internal/agent	12.000s
EOF

# ── 夹具 6：非 -v 多包腿（无裸 PASS，只有 ok 结果行）⇒ ① 在这类腿上必须可达 ──────
# 这条是"防空转"的关键：实测单包非 -v 与多包腿都不打印裸 PASS，若 ① 只认 PASS，
# -race 各腿的重试就变成永不触发的死代码，而测试仍然是绿的。
{
    echo "ok  	github.com/Levango7/OpsMesh/internal/store/sql	12.345s"
    echo "ok  	github.com/Levango7/OpsMesh/internal/controlplane	45.678s"
    cat <<'INNER'
SIGSEGV: segmentation violation
PC=0x448821 m=3 sigcode=1 addr=0x0

goroutine 0 gp=0x6e4376f94a0 m=3 mp=0x6e437700008 [idle]:
runtime.mallocgc(0x100, 0x0, 0x1)
	/usr/local/go/src/runtime/malloc.go:1003 +0x1c
INNER
    echo "FAIL	github.com/Levango7/OpsMesh/internal/metrics	3.000s"
} > "$WORK/nonverbose-multi-ok.log"

# ── 夹具 7：单包 -v 腿跑完之前就崩 ⇒ ① 不成立，判红（有意保守的边界）─────────────
cat > "$WORK/midrun-crash.log" <<'EOF'
=== RUN   TestJZ_One
--- PASS: TestJZ_One (0.00s)
=== RUN   TestJZ_Two
SIGSEGV: segmentation violation
PC=0x439c7d m=5 sigcode=1 addr=0x0
runtime.(*spanQueue).tryDrain(0x100000400?)
FAIL	github.com/Levango7/OpsMesh/internal/agent	5.000s
EOF

# ── 夹具 8：编译失败（① 成立但没有崩溃头）⇒ 必须判红 ────────────────────────────
# 多包腿里"某包编译失败、其余包已打印 ok"是真实形态；这里刻意带上 ok 行，
# 否则该用例会先被 ① 挡掉，声称覆盖的"无崩溃头"分支其实一次都没走到。
cat > "$WORK/compile-error.log" <<'EOF'
ok  	github.com/Levango7/OpsMesh/internal/config	1.000s
# github.com/Levango7/OpsMesh/internal/agent [github.com/Levango7/OpsMesh/internal/agent.test]
./collector.go:12:2: undefined: newCollector
FAIL	github.com/Levango7/OpsMesh/internal/agent [build failed]
FAIL
EOF

# ── 夹具 9：ThreadSanitizer 分配器 OOM（具名例外）⇒ 应重试 ──────────────────────
cat > "$WORK/tsan-oom.log" <<'EOF'
WARNING: ThreadSanitizer: data race (pid=1234)
ThreadSanitizer: internal allocator is out of memory trying to allocate 0x4c0000 bytes
FAIL	github.com/Levango7/OpsMesh/internal/store	60.000s
EOF

# ── 夹具 10：全绿日志（本不该调用判据，但必须不被判成"可重试"）──────────────────
cat > "$WORK/all-green.log" <<'EOF'
ok  	github.com/Levango7/OpsMesh/internal/config	1.000s
ok  	github.com/Levango7/OpsMesh/internal/metrics	2.000s
EOF

# ── 夹具 11：日志里回显了**含 TSan 字样的源码文本**（真实事故的整步骤日志就是这个形状）
# 不加这条的话，判据会先被具名例外分支吃掉，把一次 GC 里的 SIGSEGV 报成"TSan OOM"——
# 结论方向碰巧没错、依据是假的，而只看退出码的自测抓不到它。
cat > "$WORK/quoted-source-tsan.log" <<'EOF'
=== RUN   TestTaskTimeoutFor_NegativeTimeout
--- PASS: TestTaskTimeoutFor_NegativeTimeout (0.00s)
OOM_PAT='fatal error: runtime: (cannot allocate memory|out of memory)|ThreadSanitizer: internal allocator is out of memory'
PASS
SIGSEGV: segmentation violation
PC=0x439c7d m=5 sigcode=1 addr=0x0

goroutine 0 gp=0x6e4378041e0 m=5 mp=0x6e437800008 [idle]:
runtime.(*spanQueue).tryDrain(0x100000400?, 0x5175ffcb68?, 0x75ffcbb8?)
	/opt/hostedtoolcache/go/1.26.6/x64/src/runtime/mgcmark_greenteagc.go:520 +0x5d
FAIL	github.com/Levango7/OpsMesh/internal/agent	20.044s
EOF

# ── 夹具 12：map 并发写（产品级缺陷，崩溃段顶帧也"像 runtime"）⇒ 必须判红 ─────────
# 本机实测形态（go1.26.6 造了个 8 goroutine 抢同一张 map 的用例）：
#   fatal error: concurrent map writes
#   goroutine 22 [running]:
#   internal/runtime/maps.fatal({0x…?, 0x0?})
#   tmpfailprobe/d.Hammer(...)          ← 业务帧出现在第二段
# 只看顶帧会把产品级并发缺陷判成"可重试"；而且它会不会被 ① 挡住取决于崩溃时机，
# 所以这里刻意带一行 ok（模拟多包腿里别的包已过），强制这条用例走到 ②。
{
    echo "ok  	tmpfailprobe/a	0.512s"
    cat <<'INNER'
fatal error: concurrent map writes

goroutine 22 [running]:
internal/runtime/maps.fatal({0x7ff7f05c0ab1?, 0x0?})
	/usr/local/go/src/runtime/panic.go:1043 +0x18 fp=0xc00004ce40 sp=0xc00004ce10 pc=0x7ff761ca3f43
tmpfailprobe/d.Hammer(...)
	F:/tmp/ci80/failmod/d/d.go:8 +0x2c
tmpfailprobe/d.TestMapRace.func1()
	F:/tmp/ci80/failmod/d/d_test.go:9 +0x23
INNER
    echo "FAIL	tmpfailprobe/d	0.603s"
} > "$WORK/maprace-after-ok.log"

# ── 夹具 13：真实 nil 解引用 panic（逐字摘录本机 go test 输出）⇒ 必须判红 ─────────
# 关键事实：panic 之前 testing 已经打印了 `--- FAIL:`，所以 ③ 能真挡住（实测，非推测）。
cat > "$WORK/real-nil-panic.log" <<'EOF'
=== RUN   TestNilDeref
--- FAIL: TestNilDeref (0.00s)
panic: runtime error: invalid memory address or nil pointer dereference [recovered, repanicked]
[signal 0xc0000005 code=0x0 addr=0x0 pc=0x7ff761ca3f43]

goroutine 19 [running]:
testing.tRunner.func1.2({0x7ff761cd0760, 0x7ff761e44050})
	E:/dev-tools/Go/src/testing/testing.go:1974 +0x239
panic({0x7ff761cd0760?, 0x7ff761e44050?})
	E:/dev-tools/Go/src/runtime/panic.go:860 +0x13a
tmpfailprobe/c.Boom(...)
	F:/tmp/ci80/failmod/c/c.go:5 +0x1c
EOF

# ── 夹具 14：崩溃信息与测试二进制输出**交错**（本机真实产出的形状）⇒ 必须判红 ─────
# 实测（go1.26.6，8 个 goroutine 抢同一张 map、用例先返回）：runtime 的 fatal 输出与
# 二进制的 PASS 写同一个 fd 且不加锁，于是行被劈开成 `fatal error: PASS` / `concurrent map writes`，
# 而 go 自己仍然打印了 `ok  tmpfailprobe/d`（退出码 0）。
# 两个结论：① 单看 `^ok` 不能证明批次成功（所以 ② 必须存在）；
# ② 崩溃段取不到栈帧时必须判红，不能因为"有 fatal 头"就放行。
cat > "$WORK/interleaved-fatal.log" <<'EOF'
=== RUN   TestMapRaceAfterPass
--- PASS: TestMapRaceAfterPass (0.00s)
fatal error: PASS
concurrent map writes
ok  	tmpfailprobe/d	0.437s
EOF

pass=0
fails=0
positives=0
negatives=0

check() {
    local name="$1" expect="$2" file="$3" kind="$4" why="${5:-}"
    local out rc
    if out="$(bash "$CLI" "$file" 2>&1)"; then
        rc=0
    else
        rc=$?
    fi
    if [ "$kind" = "retry" ]; then
        positives=$((positives + 1))
    else
        negatives=$((negatives + 1))
    fi
    if [ "$rc" -ne "$expect" ]; then
        fails=$((fails + 1))
        echo "  [FAIL] ${name} → 期望 rc=${expect} 实际 rc=${rc} | ${out}"
        return
    fi
    # 只核对退出码会放过"结论对、依据错"的判据：同一条 retry 可能来自完全不同的分支。
    # 所以给需要指明分支的用例补一条依据断言（夹具 11 就是为这条而加的）。
    if [ -n "$why" ] && ! grep -qF -- "$why" <<<"$out"; then
        fails=$((fails + 1))
        echo "  [FAIL] ${name} → rc 对但依据不符（应含「${why}」）| ${out}"
        return
    fi
    pass=$((pass + 1))
    echo "  [PASS] ${name} → rc=${rc} | ${out}"
}

echo "=== ci-infra-death.sh 判据自测 ==="
check "真实崩溃段（PASS 后裸 SIGSEGV，崩溃段全属 runtime）" 0 "$WORK/real-after-pass.log"  retry "runtime 内"
check "同形状但含 --- FAIL:（必须判红，不掩盖回归）"        1 "$WORK/after-pass-with-fail.log" red
check "子测试缩进形态的 --- FAIL:（③ 不被缩进绕过）"        1 "$WORK/subtest-indent-fail.log"  red
check "Go 运行时 ENOMEM（fatal error 头）"                  0 "$WORK/enomem-after-pass.log"    retry "runtime 内"
check "业务侧崩溃（崩溃段含包内帧）"                        1 "$WORK/business-crash.log"       red "非 runtime 帧"
check "非 -v 多包腿只有 ok 行（防空转）"                    0 "$WORK/nonverbose-multi-ok.log"  retry
check "跑完之前就崩（① 不成立，有意保守）"                  1 "$WORK/midrun-crash.log"         red
check "编译失败（无崩溃头）"                                1 "$WORK/compile-error.log"        red "崩溃头"
check "ThreadSanitizer 分配器 OOM（具名例外）"              0 "$WORK/tsan-oom.log"             retry "ThreadSanitizer"
check "全绿日志不被当成可重试"                              1 "$WORK/all-green.log"            red "崩溃头"
check "回显源码里含 TSan 字样（必须走栈帧分支而非具名例外）" 0 "$WORK/quoted-source-tsan.log"  retry "runtime 内"
check "map 并发写（顶帧像 runtime，第二段是业务帧）"        1 "$WORK/maprace-after-ok.log"     red "非 runtime 帧"
check "真实 nil 解引用 panic（panic 前已打印 --- FAIL:）"   1 "$WORK/real-nil-panic.log"       red "--- FAIL:"
check "崩溃信息与 PASS 交错（无栈帧可判，必须判红）"        1 "$WORK/interleaved-fatal.log"    red "取不到栈帧"

# ── 用法/失明侧：判据读不到东西时必须是 rc=2，而调用方要把 rc=2 当判红 ────────────
missing_rc=0
bash "$CLI" >/dev/null 2>&1 || missing_rc=$?
if [ "$missing_rc" -eq 2 ]; then
    pass=$((pass + 1))
    echo "  [PASS] 缺参数 → rc=2（用法错误，不是「干净」）"
else
    fails=$((fails + 1))
    echo "  [FAIL] 缺参数 → 期望 rc=2 实际 rc=${missing_rc}"
fi

blind_rc=0
bash "$CLI" "$WORK/does-not-exist.log" >/dev/null 2>&1 || blind_rc=$?
if [ "$blind_rc" -eq 2 ]; then
    pass=$((pass + 1))
    echo "  [PASS] 日志不可读 → rc=2（判据失明必须与「判绿」区分）"
else
    fails=$((fails + 1))
    echo "  [FAIL] 日志不可读 → 期望 rc=2 实际 rc=${blind_rc}"
fi

# ── 夹具自检：如果夹具变形，本节的阳性/阴性覆盖会静默消失 ─────────────────────────
# 真实日志里 ^fatal error: 计数为 0，崩溃头是裸 SIGSEGV —— 这条形态是"两种崩溃头"里
# 唯一在真实事故中出现过的那种，不许被改没了。
fixture_checks=0
fixture_fail=0
if [ "$(grep -cE '^PASS$' "$WORK/real-after-pass.log")" -eq 1 ]; then
    fixture_checks=$((fixture_checks + 1))
else
    fixture_fail=1
    echo "  [FAIL] 夹具 1 失去了裸 PASS 行（① 的 -v 形态覆盖会被静默删掉）"
fi
if [ "$(grep -cE '^fatal error: ' "$WORK/real-after-pass.log")" -eq 0 ]; then
    fixture_checks=$((fixture_checks + 1))
else
    fixture_fail=1
    echo "  [FAIL] 夹具 1 被改成 fatal error 形态（真实事故里那一行计数为 0）"
fi
if grep -qE '^SIG[A-Z]+:' "$WORK/real-after-pass.log"; then
    fixture_checks=$((fixture_checks + 1))
else
    fixture_fail=1
    echo "  [FAIL] 夹具 1 失去了裸信号崩溃头（真实事故用的就是这种形态）"
fi
if grep -qE '^ok[[:space:]]' "$WORK/nonverbose-multi-ok.log" &&
   ! grep -qE '^PASS$' "$WORK/nonverbose-multi-ok.log"; then
    fixture_checks=$((fixture_checks + 1))
else
    fixture_fail=1
    echo "  [FAIL] 夹具 6 不再是「只有 ok 行、没有 PASS」的形态（-race 腿的空转风险回来了）"
fi
if grep -qE '^[[:space:]]+--- FAIL:' "$WORK/subtest-indent-fail.log"; then
    fixture_checks=$((fixture_checks + 1))
else
    fixture_fail=1
    echo "  [FAIL] 夹具 3 失去了缩进的 --- FAIL:（子测试形态覆盖消失）"
fi
if grep -qE '^ok[[:space:]]' "$WORK/compile-error.log" &&
   ! grep -qE '^(fatal error: |SIG[A-Z]+: )' "$WORK/compile-error.log"; then
    fixture_checks=$((fixture_checks + 1))
else
    fixture_fail=1
    echo "  [FAIL] 夹具 8 不再是「有 ok 行但无崩溃头」的形态（无崩溃头分支会被 ① 抢先挡掉，等于没覆盖）"
fi
if grep -qE '^ok[[:space:]]' "$WORK/maprace-after-ok.log" &&
   grep -qE '^fatal error: ' "$WORK/maprace-after-ok.log" &&
   grep -qE '^tmpfailprobe/' "$WORK/maprace-after-ok.log"; then
    fixture_checks=$((fixture_checks + 1))
else
    fixture_fail=1
    echo "  [FAIL] 夹具 12 变形（它必须同时有 ok 行、fatal 头与业务帧，否则 ② 的整段判定不会被走到）"
fi
if grep -qE -- '^--- FAIL:' "$WORK/real-nil-panic.log" &&
   grep -qE '^panic: ' "$WORK/real-nil-panic.log"; then
    fixture_checks=$((fixture_checks + 1))
else
    fixture_fail=1
    echo "  [FAIL] 夹具 13 变形（panic 前必须有 --- FAIL:，这是 ③ 真能挡住业务 panic 的那条实测依据）"
fi
if grep -qE '^fatal error: ' "$WORK/interleaved-fatal.log" &&
   grep -qE '^ok[[:space:]]' "$WORK/interleaved-fatal.log" &&
   ! grep -qE '^[A-Za-z_][A-Za-z0-9_./]*\(' "$WORK/interleaved-fatal.log"; then
    fixture_checks=$((fixture_checks + 1))
else
    fixture_fail=1
    echo "  [FAIL] 夹具 14 变形（它必须「有 fatal 头 + 有 ok 行 + 一帧都没有」，否则取不到栈帧那条分支不会被走到）"
fi

# 阳性/阴性都必须存在：只剩一边的自测等于没测。
if [ "$positives" -ge 4 ] && [ "$negatives" -ge 4 ]; then
    fixture_checks=$((fixture_checks + 1))
else
    fixture_fail=1
    echo "  [FAIL] 阳性 ${positives} 条 / 阴性 ${negatives} 条 —— 少于各 4 条，判据覆盖会单边失效"
fi

echo ""
echo "SUMMARY cases=${pass} passed, ${fails} failed; 夹具/平衡自检 ${fixture_checks}/10 通过; 阳性 ${positives} 阴性 ${negatives}"

if [ "$fails" -gt 0 ] || [ "$fixture_fail" -ne 0 ]; then
    echo "自测判红：重试判据不可信，build-test 不得依赖它"
    exit 1
fi
echo "自测通过"
