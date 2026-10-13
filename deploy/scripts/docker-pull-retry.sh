#!/usr/bin/env bash
# docker-pull-retry.sh —— docker pull 的有界重试包装（TD-94，2026-10-13；母项 TD-89）。
#
# 要解决的真问题：Docker Hub 对**匿名**拉取按 IP 限流。2026-10-10 实测（run
# 37979767826，attempt 1/2 两次同形态）：`registry-1.docker.io ... 429 Too Many
# Requests`、`auth.docker.io/token: 500`——失败都发生在**解析基础镜像元数据**阶段、
# 未编译任何代码，同 run 的代码门禁全绿 ⇒ 判红与提交内容无关。母项缓解（TD-89）已做
# 两步：CI 侧 docker/login-action 接线（用户决定不配 secrets ⇒ 维持匿名）、本脚本（选项③，
# 台账原话"触发即做，不再问"，本笔 proactively 落地）。
#
# 为什么在构建前**预热**而不是"重跑 job"：429 发生在 docker/build-push-action 内部
# 解析 FROM 阶段，action 无法包一层重试；而预先 `docker pull` 同一批 ref 既把 429
# 暴露到**可重试的 shell 步骤**上，又把镜像写入本地 store（buildx docker driver 的
# FROM 解析优先命中本地镜像）⇒ 构建步骤随之不再碰注册中心元数据。
#
# 判据（保守，照 ci-infra-death.sh 的文化：默认红、只放行白名单形态）：
#   只对**限流与传输层抖动**重试（模式表见 RETRY_PATTERNS，每条都有出处注释）；
#   其余一律立即失败——尤其"manifest unknown / not found / unauthorized /
#   denied"这类真实错误，重试只会把真失败拖成三倍时长。
#   有界：每镜像独立预算，attempts=max 时耗尽即败；总时长 ≈ Σ( sleeps + pull )，
#    sleeps 默认 "5 10"（最坏 +15s/镜像），不随重试次数线性放大。
#
# 未覆盖（如实记账，别假装全覆盖）：
#   - GH-Actions `services:` 容器（integration/services 两job 的 mysql:8）由 runner
#     在 steps 之前拉取，任何步骤级包装都够不着——彻底解是母项选项②（基础镜像改
#     GHCR 镜像源，属镜像决策，未拍板，见台账 TD-89/TD-94）。
#   - digest pinned 的 ref 不走 tag 元数据解析（Renovate 计划），与本脚本正交。
#
# 用法: docker-pull-retry.sh <image>...
# 环境: PULL_RETRY_MAX=3       每镜像最大尝试次数（含首次）
#       PULL_RETRY_SLEEPS="5 10"  重试前睡眠秒数表（空格分隔；元素个数 < max-1 时
#                                后续用最后一个值兜底；单元素 "0" 供自测快跑）
# 退出码: 0 = 全部拉取成功（含重试后成功）
#         1 = 有镜像拉取失败（真实错误或重试耗尽；stderr 保留最后一次原始输出）
#         2 = 用法/环境错误 —— 调用方必须当判红：包装失明不等于镜像拉到了
# stdout: 每镜像一行的结果摘要（CI 日志留痕用）。

set -euo pipefail

if [ "$#" -eq 0 ]; then
    echo "usage: $0 <image>..." >&2
    exit 2
fi

MAX="${PULL_RETRY_MAX:-3}"
SLEEPS_RAW="${PULL_RETRY_SLEEPS:-5 10}"
# shellcheck disable=SC2206  # 故意按空格拆表；元素个数不足时下标越界由下方兜底
SLEEPS=(${SLEEPS_RAW})

case "${MAX}" in
    ''|*[!0-9]*|0)
        echo "red: PULL_RETRY_MAX 必须是正整数（实得 '${MAX}'）——参数错误按判红处理" >&2
        exit 2
        ;;
esac

sleep_at() { # sleep_at <attempt>：第 attempt 次失败后的睡眠秒数
    local i=$(( $1 - 1 ))
    local n=${#SLEEPS[@]}
    if [ "$n" -eq 0 ]; then
        echo 0
        return
    fi
    if [ "$i" -ge "$n" ]; then
        i=$(( n - 1 ))
    fi
    echo "${SLEEPS[$i]}"
}

# RETRY_PATTERNS：只放行"限流/传输层抖动"。每条附出处（TD-89 实测证据或
# registry 客户端标准文案）。新增模式必须同格式注明出处，否则不收。
#   - 429/toomanyrequests：TD-89 两次实测主形态（registry-1.docker.io manifests）
#   - auth.docker.io/token 500：同次实测（token 端点 5xx 同样是限流表现）
#   - 502/503/504：上游网关/注册中心的瞬时不可用
#   - i/o timeout / TLS handshake timeout / connection reset / EOF /
#     temporary failure in name resolution：传输层抖动（runner 网络）
RETRY_PATTERN='429|Too Many Requests|toomanyrequests|auth\.docker\.io/token.*(500|502|503|504)|HTTP code: (500|502|503|504)|error pulling image configuration|i/o timeout|TLS handshake timeout|connection reset by peer|connection refused|EOF|temporary failure in name resolution|no such host|unavailable'

is_retryable() { # is_retryable <输出文本>：命中模式表返回 0
    printf '%s\n' "$1" | grep -Eqi -- "${RETRY_PATTERN}"
}

pulled=0
failed=0
for img in "$@"; do
    attempt=1
    last_out=""
    last_rc=0
    while :; do
        if last_out="$(docker pull "$img" 2>&1)"; then
            if [ "$attempt" -eq 1 ]; then
                echo "ok   ${img}（首次成功）"
            else
                echo "ok   ${img}（第 ${attempt}/${MAX} 次成功，之前为限流/抖动）"
            fi
            pulled=$(( pulled + 1 ))
            break
        fi
        last_rc=$?
        if [ "$attempt" -lt "$MAX" ] && is_retryable "$last_out"; then
            s="$(sleep_at "$attempt")"
            echo "retry ${img}：第 ${attempt}/${MAX} 次失败命中限流/抖动模式，${s}s 后重试" >&2
            sleep "$s"
            attempt=$(( attempt + 1 ))
            continue
        fi
        # 失败出口：不可重试（真实错误，首次即败）或可重试但预算耗尽。
        if [ "$attempt" -ge "$MAX" ]; then
            echo "red  ${img}：重试 ${attempt} 次仍失败（限流/抖动未恢复）" >&2
        else
            echo "red  ${img}：失败不命中限流/抖动模式（真实错误，不重试）" >&2
        fi
        printf '%s\n' "$last_out" >&2
        echo "red  最后一次 docker pull 退出码=${last_rc}" >&2
        failed=$(( failed + 1 ))
        break
    done
done

if [ "$failed" -gt 0 ]; then
    echo "red  共 ${failed} 个镜像拉取失败（ok=${pulled}）" >&2
    exit 1
fi
echo "ok   全部 ${pulled} 个镜像就绪"
exit 0
