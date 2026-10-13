#!/usr/bin/env bash
# wait-tag-ci.sh —— 等「tag 触发的 ci run」收敛到终态（TD-95 方向②：可判定收敛，替代固定秒数猜测）。
#
# 为什么需要它：`opsmesh-binary` / `opsmesh-agent` 两个核心镜像与 Release 的 5 个 assets
# 都由 ci.yml 的 tag 事件产出，与 release.yml 的「发布物回核」job **并行**跑（GitHub Actions
# 的 `needs` 不能跨 workflow）。回核此前靠 `CORE_WAIT` 固定秒数猜"产物该到了"，两次真实事故
# 都出在这个估计上：v0.13.0 首跑（估计不足 ⇒ 报成"没发布"）、v0.14.0（③ 无凭证把等待整段掐掉）。
# 「等待值来自估计」本身就是缺陷——本脚本把等待换成**可判定的收敛条件**：那条 ci run 到终态。
#
# 用法：bash deploy/scripts/wait-tag-ci.sh <版本，不带 v> <发布提交 sha>
#   WAIT_CI_TICKS  轮询次数上限（默认 20）
#   WAIT_CI_SLEEP  每次间隔秒（默认 90）——默认预算 30 分钟；到点不判红，退化到产物轮询
#
# 退出码（调用方据此分诊，见 release.yml 回核 step）：
#   0 = tag 的 ci run 已 completed/success（binary/agent 镜像与 assets 已产出）
#   1 = tag 的 ci run 终态**失败**（failure/cancelled/timed_out/startup_failure）
#       ⇒ 产物不会来，立刻判红并把 run 链接指给值班人；**不要**拿去重跑回核（会白等）
#   2 = 判不出来（run 还没创建 / 仍在跑且超过轮询预算 / gh 查询失败）⇒ 「未知」，不是缺陷
set -uo pipefail

VER="${1:-}"
SHA="${2:-}"
REPO="${OPSMESH_GITHUB_REPO:-Levango7/OpsMesh}"
TICKS="${WAIT_CI_TICKS:-20}"
SLEEP_S="${WAIT_CI_SLEEP:-90}"

if [ -z "$VER" ] || [ -z "$SHA" ]; then
    echo "用法: $0 <版本（不带 v）> <发布提交 sha>" >&2
    exit 2
fi

# 取"tag 触发的 ci run"（最新的那条）的 id/status/conclusion/url。
# 语义要点：用 head_sha 过滤到发布提交，再按 head_branch == v<版本> 收敛到 **tag 那条**
# ——同 sha 若也有一条 main 分支的 ci run，它不产 binary/agent 镜像与 assets（那些 job
# 以 tag 为条件），拿它当判据会把"没产出"误判成"已产出"。
latest_tag_run() {
    gh api --allow-escape-sequences \
        "repos/${REPO}/actions/workflows/ci.yml/runs?head_sha=${SHA}&event=push&per_page=50" \
        --jq "[.workflow_runs[] | select(.head_branch == \"v${VER}\")]
              | if length == 0 then \"NONE\"
                else (sort_by(.created_at) | last | [.id, .status, (.conclusion // \"\"), .html_url] | @tsv)
                end" 2>&1
}

tick=0
while [ "$tick" -lt "$TICKS" ]; do
    tick=$((tick + 1))
    line="$(latest_tag_run)"; rc=$?
    if [ "$rc" -ne 0 ]; then
        # 查询失败（无凭证/网络/限流）⇒ 既不是"产物缺失"也不是"已收敛"，如实报未知。
        echo "  [wait-tag-ci] 第 ${tick}/${TICKS} 次查询失败（rc=${rc}）：${line%%$'\n'*}"
        sleep "$SLEEP_S"; continue
    fi
    case "$line" in
        NONE*)
            echo "  [wait-tag-ci] 第 ${tick}/${TICKS} 次：tag v${VER} 的 ci run 尚未创建（等它出现）"
            sleep "$SLEEP_S"; continue
            ;;
    esac
    run_id="$(printf '%s' "$line" | cut -f1)"
    status="$(printf '%s' "$line" | cut -f2)"
    concl="$(printf '%s' "$line" | cut -f3)"
    url="$(printf '%s' "$line" | cut -f4)"
    echo "  [wait-tag-ci] 第 ${tick}/${TICKS} 次：run ${run_id} status=${status} conclusion=${concl:-（未定）} ${url}"
    if [ "$status" = "completed" ]; then
        case "$concl" in
            success)
                echo "  [wait-tag-ci] tag 的 ci run 已成功收敛（产物应已产出）"
                exit 0
                ;;
            *)
                echo "  [wait-tag-ci] tag 的 ci run 终态为 ${concl}——产物不会产出：${url}" >&2
                exit 1
                ;;
        esac
    fi
    sleep "$SLEEP_S"
done
echo "  [wait-tag-ci] 轮询 ${TICKS} 次（预算 $((TICKS * SLEEP_S))s）后仍未收敛——按「未知」处理（不判红）" >&2
exit 2
