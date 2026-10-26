#!/usr/bin/env bash
# verify-artifacts-ci.sh —— 发布物回核的 **CI 驱动层**（判据本身在 verify-release-artifacts.sh）。
#
# 为什么单独成脚本：这段逻辑（等 tag 腿收敛 → 轮询产物 → 把 FAIL 分成"竞态内/竞态外" → 三态收尾）
# 原来内联在 release.yml 的 verify-artifacts job 里。TD-74 把回核提前到发布**之前**之后，
# 同一段逻辑要在两个 job 里各跑一次（preflight 与 post），内联就意味着两份会各自漂移——
# 而"两份判据不一样"正是本仓反复记过的失效形态。故抽出，两个 job 共用一份实现。
#
# 用法：bash deploy/scripts/verify-artifacts-ci.sh <版本> [--pre-release]
#   --pre-release：发布前回核（Release 对象尚未创建）⇒ 判据脚本跳过 ③ 并记 SKIP。
# 退出码：0 通过（含"UNVERIFIED 但放宽"的既有语义，见下）；1 判红。
#
# 参数约定：VER 取 $1；未给则退到 GITHUB_REF_NAME（tag 触发时的行为，保持向后兼容）。
set -uo pipefail
# 关掉 errexit 是必须的，不是风格问题：runner 用 `bash -e {0}` 执行本步，
# 而 `out="$(verify…)"` 这种命令替换一非零就当场终止整步——于是下面那套
# 「FAIL 判红 / UNVERIFIED 只 warning 放行 / 无结论判红」的三分支**一行都执行不到**，
# 日志里连脚本输出都没有（2026-10-07 v0.13.0 的 attempt 1/2 就是这个形状：
# 只有 run 块回显 + `Process completed with exit code 1`）。
# 一条负责区分"未知"与"缺陷"的门禁，首先得真的跑到那句区分。
set +e
VER="${1:-${GITHUB_REF_NAME:-}}"
PRE_RELEASE=0
for _a in "$@"; do
  case "$_a" in
    --pre-release) PRE_RELEASE=1 ;;
    -*) echo "未知选项：${_a}" >&2; exit 2 ;;
    *) VER="$_a" ;;
  esac
done
VER="${VER#v}"
if [ -z "$VER" ]; then
  echo "用法: $0 <版本> [--pre-release]（CI 里可由 GITHUB_REF_NAME 推导）" >&2
  exit 2
fi
# TD-95 方向②（2026-10-13，板第八十则）：先把等待从"固定秒数估计"换成**可判定的
# 收敛条件**——按 head_sha 轮询 tag 触发的 ci run 到终态（它产出 binary/agent 镜像
# 与 5 个 assets）。三态分诊：0=已成功（产物应已产出，进下面的产物轮询做 GHCR 传播
# 兜底）；1=那条 run 终态**失败**（产物不会来 ⇒ 立刻判红并把 run 链接指给值班人，
# **别重跑本 job 白等**——这正是 TD-95 要治的"红因文案把人引向错误处置"）；2=未知
# （run 还没出现 / 超预算 / 查询失败）⇒ 退化到产物轮询，不因"慢"判红。
if bash deploy/scripts/wait-tag-ci.sh "${VER}" "${RELEASE_SHA}"; then
  echo "::notice::tag v${VER} 的 ci run 已成功收敛"
else
  wait_rc=$?
  if [ "$wait_rc" -eq 1 ]; then
    echo "::error::tag v${VER} 的 ci run 终态失败 ⇒ opsmesh-binary/opsmesh-agent 镜像与 Release assets 不会产出；先修那条 run，再重跑本 job"
    exit 1
  fi
  echo "::notice::tag 的 ci run 尚未收敛（wait-tag-ci rc=${wait_rc}）——退化到产物轮询（CORE_WAIT）"
fi
# 为什么要等：`opsmesh-binary` / `opsmesh-agent` 两个核心镜像与 GitHub Release 的 assets
# 都由 **ci.yml 的 tag 事件**产出，与本 workflow 并行；GitHub Actions 的 `needs` 不能跨 workflow
# 表达依赖。2026-10-07 v0.13.0 首跑实测：12 个服务镜像与 12 次 promote 全绿，本 job 仍红在
# 「binary/agent 没有 :0.13.0 + assets=0」——那是还没做出来，不是做坏了。
# 等待范围刻意收窄到「失败集只含这几条竞态行」；出现任何其它缺失立刻判红，
# 否则这道门禁会退化成"晚点自然会好"的噪音，而 §32.9 的教训恰恰是它必须拦得住。
# 方向②落地后这层是**兜底**（正常路径已被 wait-tag-ci 判完）：留它接住
# "run 已 success 但 GHCR 传播尚未可见"的分钟级窗口。
CORE_WAIT="${CORE_WAIT_LIMIT_SEC:-2700}"
waited=0
while :; do
  if [ "$PRE_RELEASE" = "1" ]; then
    out="$(bash deploy/scripts/verify-release-artifacts.sh "$VER" --pre-release 2>&1)"; rc=$?
  else
    out="$(bash deploy/scripts/verify-release-artifacts.sh "$VER" 2>&1)"; rc=$?
  fi
  printf '%s\n' "$out"
  summary="$(printf '%s\n' "$out" | grep '发布物验收' | tail -1)"
  fails="$(printf '%s' "$summary" | sed -n 's/.*FAIL=\([0-9][0-9]*\).*/\1/p')"
  unver="$(printf '%s' "$summary" | sed -n 's/.*UNVERIFIED=\([0-9][0-9]*\).*/\1/p')"
  fails="${fails:-0}"; unver="${unver:-0}"
  if [ "$rc" -ne 0 ] && [ -z "$summary" ]; then
    # 脚本没给出任何结论（崩溃、参数错、找不到 gh/helm 之外的意外）：
    # 这不是"未知"而是"没跑成"，必须判红——否则一条从未执行过的门禁会看起来像通过了。
    echo "::error::verify-release-artifacts.sh 未产出验收结论（exit=${rc}），无法判定发布物是否存在"
    exit 1
  fi
  if [ "$fails" -eq 0 ]; then break; fi
  # 与竞态无关的 FAIL 条数（核心两镜像的 tag/manifest/chart 引用行 + assets=0 行都属竞态）
  other="$(printf '%s\n' "$out" | grep '\[FAIL\]' \
    | grep -vcE 'opsmesh-binary:|opsmesh-agent:|assets=0')"
  if [ "$other" -gt 0 ]; then
    echo "::error::${VER} 的发布物核对到缺失（FAIL=${fails}，其中 ${other} 条与并行 workflow 无关）：客户按发布说明安装会拿不到产物（§19 / §32.9 就是这个形状）"
    exit 1
  fi
  if [ "$waited" -ge "$CORE_WAIT" ]; then
    echo "::error::等满 ${CORE_WAIT}s 后 opsmesh-binary / opsmesh-agent 的 :${VER} 或 Release assets 仍缺失——它们由 ci.yml 的 tag 事件（refs/tags/v${VER}）产出，请查那条 run 是否失败或被 runner 饥饿卡住"
    exit 1
  fi
  echo "::notice::${VER}：核心镜像/assets 尚未由 ci.yml 的 tag 腿产出（FAIL=${fails} 全部落在竞态集合内），120s 后重试；已等 ${waited}s / ${CORE_WAIT}s"
  sleep 120
  waited=$((waited + 120))
done
if [ "$rc" -ne 0 ] || [ "$unver" -gt 0 ]; then
  # 注意：这里刻意用 ${GHCR_ATTEMPTS:-5} 而不是 ${GHCR_ATTEMPTS}——本 job 开了 set -u，
  # 未定义的变量会在这里直接把"警告并放行"变成 exit 1（本地跑分支时实测踩过）。
  # 一条负责区分"未知"与"缺陷"的门禁，不能因为自己的引用方式而伪装成缺陷。
  echo "::warning::有 ${unver} 项没能核对上（GHCR 传输失败，已重试 ${GHCR_ATTEMPTS:-5} 次）——既不是缺陷也不是通过，请重跑本 job"
  exit 0
fi
echo "✅ 发布物回核通过（等待竞态 ${waited}s）：${summary}"

