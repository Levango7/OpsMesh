#!/usr/bin/env bash
# verify-release-artifacts.sh —— 「这个版本真的发布出去了」的四点验收（只读，不改任何东西）
#
# 为什么需要这么一条脚本（两次真实事故，同一形状）：
#   · §19：GitHub Release v0.9.1 存在但 **assets=0**、GHCR 无 0.9.1 镜像矩阵（18 条全灭），
#     而 CHANGELOG 已把它写成"已发布"——客户按说明装就是空手。
#   · §32.9：v0.12.0 的"归版"把 Chart.yaml appVersion / values-production 三处 tag /
#     gitops segment 全钉到 0.12.0，**却从未打 tag**：GHCR 只到 0.11.0 ⇒ 按生产默认值
#     `helm install` 的客户对 controlplane/agent 直接 ErrImagePull。
# 两条的共性是：**CI 全绿不等于产物存在**。release job 只在 tag 上跑，
# 而"打了 tag"这一步没有任何自动检查会回头核对产物。
#
# 四项判据（任一缺位即非零退出，绝不"跳过当通过"）：
#   ① 14 个镜像仓库（controlplane 的 opsmesh-binary + agent + 12 个微服务）都有 :<版本> tag；
#   ② 每个镜像都有 .sig 与 .att（cosign 签名 + provenance/SBOM 证据链）；
#   ③ GitHub Release 存在且 assets 非空；
#   ④ chart 默认渲染与生产 values 渲染出来的 image 引用，其 **仓库 + tag** 必须在 ① 的集合里
#      （这一条专治"清单指向一个谁都不发布的产品名/版本号"）。
#
# 用法：
#   bash deploy/scripts/verify-release-artifacts.sh 0.12.0        # 版本不带 v
#   GH=/path/to/gh bash deploy/scripts/verify-release-artifacts.sh 0.11.0
# 依赖：curl（匿名打 GHCR token）、gh（读 Release；缺失时第 ③ 项判红而不是跳过）。
set -uo pipefail

VER="${1:-}"
REPO="${OPSMESH_GITHUB_REPO:-Levango7/OpsMesh}"
NS="${OPSMESH_GHCR_NAMESPACE:-levango7}"          # GHCR 路径必须全小写（大写会被 buildx 拒）
if [ -z "$VER" ]; then
    echo "用法: $0 <版本号，如 0.12.0（不带 v）>" >&2
    exit 2
fi
case "$REPO" in */*) ;; *) echo "仓库需为 owner/name 形式（实际：$REPO）" >&2; exit 2 ;; esac

PASS=0; FAIL=0
ok()  { printf '  [PASS] %s\n' "$*"; PASS=$((PASS+1)); }
bad() { printf '  [FAIL] %s\n' "$*"; FAIL=$((FAIL+1)); }

# 14 个仓库叶子名：两个核心镜像 + release.yml 矩阵里的 12 个常驻微服务。
# 这里刻意写死而不是去 parse release.yml：本脚色的职责是"核对产物"，
# 矩阵漂移由 validate-deploy-assets.sh 的第 2/10 节负责（两处职责不重叠，避免自我印证）。
LEAVES="opsmesh-binary opsmesh-agent
auth-svc device-svc alert-svc task-svc config-svc log-svc
aio-svc autoscaler-svc gpu-svc incident-svc portal-svc runbook-svc"

token() {
    curl -s --max-time 20 "https://ghcr.io/token?scope=repository:${NS}/$1:pull" \
        | sed -n 's/.*"token":"\([^"]*\)".*/\1/p'
}

# 注意 Accept 必须含 OCI index，否则 buildx 推的 OCI manifest 会被**误报成 404**
# （本仓 §19 取证时踩过一次，差点把"存在"写成"不存在"）。
ACCEPT='Accept: application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json'
NLEAVES=$(printf '%s\n' $LEAVES | grep -c .)
tags_of() {
    local leaf="$1" tk
    tk="$(token "$leaf")"
    [ -z "$tk" ] && return 1
    curl -s --max-time 25 -H "Authorization: Bearer $tk" \
        "https://ghcr.io/v2/${NS}/${leaf}/tags/list?n=4000"
}

declare -a PUBLISHED=()
echo "== ① + ② 镜像 tag 与签名/证据链（${NS}/*:${VER}）=="
MISSING_TAG=(); MISSING_SIG=(); MISSING_ATT=()
for leaf in $LEAVES; do
    tk="$(token "$leaf")"
    if [ -z "$tk" ]; then
        bad "${leaf}: 取不到匿名 pull token（仓库不存在或 GHCR 不可达）"
        MISSING_TAG+=("$leaf"); continue
    fi
    body="$(curl -s --max-time 25 -H "Authorization: Bearer $tk" \
        "https://ghcr.io/v2/${NS}/${leaf}/tags/list?n=4000")"
    if [ -z "$body" ]; then
        bad "${leaf}: tag 列表为空响应"
        MISSING_TAG+=("$leaf"); continue
    fi
    if printf '%s' "$body" | grep -qE "\"${VER}\""; then
        PUBLISHED+=("${leaf}:${VER}")
    else
        bad "${leaf}: 没有 :${VER} tag（release job 未跑成功，或该仓库没进矩阵）"
        MISSING_TAG+=("$leaf")
    fi
    # .sig/.att 在 GHCR 里以**本版本 manifest 的 digest**命名（sha256-<digest>.sig），
    # 所以必须先把 :VER 解析成 digest 再找，否则"仓库里有个 .sig"会被当成"这个版本签过名"。
    dig="$(curl -sI --max-time 25 -H "Authorization: Bearer $tk" -H "$ACCEPT" \
        "https://ghcr.io/v2/${NS}/${leaf}/manifests/${VER}" | tr -d '\r' \
        | sed -n 's/^[Dd]ocker-[Cc]ontent-[Dd]igest: *sha256:\([0-9a-f]\{64\}\).*/\1/p' | head -1)"
    if [ -z "$dig" ]; then
        bad "${leaf}:${VER} 解析不到 manifest digest（无法核对签名，按未验证判红）"
        MISSING_SIG+=("$leaf"); MISSING_ATT+=("$leaf"); continue
    fi
    printf '%s' "$body" | grep -q "sha256-${dig}\.sig" \
        || { bad "${leaf}:${VER} 没有对应的 .sig（cosign 签名缺失）"; MISSING_SIG+=("$leaf"); }
    printf '%s' "$body" | grep -q "sha256-${dig}\.att" \
        || { bad "${leaf}:${VER} 没有对应的 .att（provenance/SBOM attest 缺失）"; MISSING_ATT+=("$leaf"); }
done
[ ${#MISSING_TAG[@]} -eq 0 ] && ok "${NLEAVES} 个镜像仓库都有 :${VER}" || echo "         缺 tag: ${MISSING_TAG[*]-}"
[ ${#MISSING_SIG[@]} -eq 0 ] && ok "${NLEAVES} 个镜像的 :${VER} 都有 cosign .sig" || echo "         缺 .sig: ${MISSING_SIG[*]-}"
[ ${#MISSING_ATT[@]} -eq 0 ] && ok "${NLEAVES} 个镜像的 :${VER} 都有 .att 证据链" || echo "         缺 .att: ${MISSING_ATT[*]-}"

echo "== ③ GitHub Release v${VER} 的 assets =="
if ! command -v gh >/dev/null 2>&1; then
    bad "本机没有 gh 命令，无法核对 Release assets（判红而不是跳过）"
else
    if gh release view "v${VER}" --repo "$REPO" >/dev/null 2>&1; then
        n="$(gh release view "v${VER}" --repo "$REPO" --json assets --jq '.assets | length' 2>/dev/null)"
        body_len="$(gh release view "v${VER}" --repo "$REPO" --json body --jq '.body | length' 2>/dev/null)"
        if [ "${n:-0}" -gt 0 ]; then
            ok "Release v${VER} 存在且有 ${n} 个 assets"
        else
            bad "Release v${VER} 存在但 **assets=0**（§19 那个形态：版本有 tag 无产物）"
        fi
        # 发布正文必须带降级能力清单（release.yml 里是硬断言，这里复核产物）
        if printf '%s' "$(gh release view "v${VER}" --repo "$REPO" --json body --jq '.body' 2>/dev/null)" \
           | grep -q "能力降级清单"; then
            ok "Release 正文含「能力降级清单」（${body_len:-?} 字符）"
        else
            bad "Release 正文没有「能力降级清单」——客户在 Releases 页看不到当前哪些能力是降级的"
        fi
    else
        bad "GitHub Release v${VER} 不存在"
    fi
fi

echo "== ④ chart 渲染出的镜像引用必须在已发布集合里 =="
if ! command -v helm >/dev/null 2>&1; then
    bad "本机没有 helm，无法渲染 chart 核对镜像引用（判红而不是跳过）"
else
    ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
    rendered="$(
        { helm template x "${ROOT}/deploy/helm/opsmesh" 2>/dev/null
          helm template x "${ROOT}/deploy/helm/opsmesh" -f "${ROOT}/deploy/helm/opsmesh/values-production.yaml" 2>/dev/null
        } | sed -n 's/^[[:space:]]*image:[[:space:]]*//p' | tr -d '"' | sort -u
    )"
    n=0; badrefs=""
    while IFS= read -r ref; do
        [ -z "$ref" ] && continue
        case "$ref" in
            ghcr.io/"${NS}"/*) : ;;
            *) continue ;;        # 第三方镜像（mysql/redis…）不归本项目发布
        esac
        leaf="$(basename "${ref%%:*}")"
        tag="${ref##*:}"
        n=$((n + 1))
        # :latest 在生产 values 里是缺陷（不可追溯），在默认 values 里允许由 ① 的 latest tag 兜住
        body="$(tags_of "$leaf")"
        if [ -z "$body" ] || ! printf '%s' "$body" | grep -qE "\"${tag}\""; then
            badrefs="${badrefs} ${leaf}:${tag}"
        fi
    done <<< "$rendered"
    if [ "$n" -eq 0 ]; then
        bad "chart 里一个 ghcr.io/${NS}/* 的 image 都没渲染出来（扫描面塌了，判红）"
    elif [ -z "$badrefs" ]; then
        ok "chart 渲染的 ${n} 个 ghcr 镜像引用（含 :${VER} 与 latest）全部存在于 GHCR"
    else
        bad "chart 引用了 GHCR 上不存在的镜像:${badrefs# }"
        echo "         ⇒ 客户 helm install 会 ErrImagePull（§32.9 的成因正是这一条）"
    fi
fi

echo
echo "==================================================="
echo "  发布物验收 ${VER}：PASS=${PASS} FAIL=${FAIL}"
echo "==================================================="
[ "$FAIL" -eq 0 ]
