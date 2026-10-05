#!/usr/bin/env bash
# verify-release-artifacts.sh —— 「这个版本真的发布出去了」的五点验收（只读，不改任何东西）
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
# 五项判据（任一缺位即非零退出，绝不"跳过当通过"）：
#   ① 14 个镜像仓库（controlplane 的 opsmesh-binary + agent + 12 个微服务）都有 :<版本> tag；
#   ② 每个镜像都有 .sig 与 .att（cosign 签名 + provenance/SBOM 证据链）；
#   ③ GitHub Release 存在且 assets 非空；
#   ④ chart 默认渲染与生产 values 渲染出来的 image 引用，其 **仓库 + tag** 必须在 ① 的集合里
#      （这一条专治"清单指向一个谁都不发布的产品名/版本号"）。
#   ⑤ :<版本> 与 :<发布提交> 必须指向**同一个 manifest digest**——发布链是"先推不可变 sha、
#      门禁全绿后在 registry 侧改标提权"，所以提权不产生新镜像。digest 不一致就意味着有人
#      把版本 tag 重新构建了一遍：那份镜像从没被 Trivy/签名看过，而 ①②③④ 照样全绿。
#
# 用法：
#   bash deploy/scripts/verify-release-artifacts.sh 0.12.0        # 版本不带 v
#   GH=/path/to/gh bash deploy/scripts/verify-release-artifacts.sh 0.11.0
#   GHCR_ATTEMPTS=5 bash deploy/scripts/verify-release-artifacts.sh 0.12.0   # 网络差时加大重试
#   RELEASE_SHA=<40位提交> bash deploy/scripts/verify-release-artifacts.sh 0.12.0
#       （第 ⑤ 项要拿"发布提交"与版本 tag 比 digest。CI 里由 release.yml 直接给 github.sha；
#        本地不给就退到 `git rev-list -n1 v<版本>`，仓库里没有那个 tag 时如实报未核对。）
# 依赖：curl（匿名打 GHCR token）、gh（读 Release；缺失时第 ③ 项判红而不是跳过）。
#
# 三种结论，各有不同处置（这是本脚本能否被信任的关键）：
#   [PASS]        核对到了，符合预期；
#   [FAIL]        核对到了，**不符合**预期——真的没发布 / 引用指向不存在的镜像，属发布事故；
#   [UNVERIFIED]  **没能核对上**（GHCR 传输失败，重试耗尽）。既不是通过也不是缺陷。
# FAIL 或 UNVERIFIED 任一非零都退出非零（不把未知当通过），但计数分开、文案分开：
# 前者要查发布，后者只要重跑。把它们混成一条红，实测代价就是本会话里那次误报——
# 同一脚本连跑两遍给出不同 FAIL 集，第一条红还指着"客户会 ErrImagePull"这种严重后果，
# 而 release run 37221272686 的 12 个 build-and-push 全是 success、直查 tag 三轮全 200。
set -uo pipefail

VER="${1:-}"
REPO="${OPSMESH_GITHUB_REPO:-Levango7/OpsMesh}"
NS="${OPSMESH_GHCR_NAMESPACE:-levango7}"          # GHCR 路径必须全小写（大写会被 buildx 拒）
if [ -z "$VER" ]; then
    echo "用法: $0 <版本号，如 0.12.0（不带 v）>" >&2
    exit 2
fi
case "$REPO" in */*) ;; *) echo "仓库需为 owner/name 形式（实际：$REPO）" >&2; exit 2 ;; esac

PASS=0; FAIL=0; UNVERIFIED=0
ok()  { printf '  [PASS] %s\n' "$*"; PASS=$((PASS+1)); }
bad() { printf '  [FAIL] %s\n' "$*"; FAIL=$((FAIL+1)); }
# unver() 记「没能核对」而不是「核对失败」：仍然非零退出（不把没验的东西当通过），
# 但输出上与真缺陷分开——这两种红的处置动作完全相反（前者重跑/换网络，后者是发布事故）。
unver() { printf '  [UNVERIFIED] %s\n' "$*"; UNVERIFIED=$((UNVERIFIED+1)); }

# GHCR_ATTEMPTS：单次 HTTP 请求的重试次数（可用环境变量覆盖，方便排障时调大）。
GHCR_ATTEMPTS="${GHCR_ATTEMPTS:-3}"

# ghcr_get <放响应体的变量名> <放HTTP码的变量名> <curl 参数...>
#   返回 0 = 拿到**确定性**响应（HTTP 码写进第二个变量，含 401/404）；
#   返回 1 = 重试耗尽（curl 自身失败，或 429/5xx）。
#
# 为什么必须有这一层：GHCR 是公网服务，本机偶发连接失败是常态——实测复刻脚本的请求形态
# 连打 30 轮，约 5% 是传输层失败（URLError / 拿不到 token），而**一次 429 都没有**。
# 原脚本把「取不到 token / 空响应」一律写成「仓库不存在或 GHCR 不可达」并判红，
# 于是一次抖动就能产出一份"看起来像发布缺陷"的红；同一份脚本连跑两遍给出不同 FAIL 集
# （第一遍报 portal-svc 缺 .att + autoscaler-svc 引用不存在，第二遍这两条都变绿，
# 却换成 5 个仓库取不到 token），即为证据。这类假红的代价不是噪音，
# 是让人开始不信这条门禁——而它守的是客户能不能装起来。
ghcr_get() {
    # 内部变量一律用 __gh_ 前缀：调用方传进来的**输出变量名**（如 body/code）会被
    # printf -v 直接写入，而被调函数里同名的 local 会在动态作用域下遮蔽它——
    # 表现是"输出永远是空 + set -u 报未绑定 + 调用方按失败处理"，整条门禁静默失去牙齿。
    # （本函数第一版就是这样：28 项 UNVERIFIED 全由这个遮蔽造成，而不是网络。）
    local __out_var="$1" __code_var="$2" __gh_i __gh_out __gh_code
    shift 2
    __gh_out=""
    __gh_code=""
    for ((__gh_i = 1; __gh_i <= GHCR_ATTEMPTS; __gh_i++)); do
        if __gh_out="$(curl -s --max-time 25 -w $'\n%{http_code}' "$@" 2>/dev/null)"; then
            __gh_code="${__gh_out##*$'\n'}"
            __gh_out="${__gh_out%$'\n'*}"
            case "$__gh_code" in
                429|500|502|503|504)
                    sleep $((__gh_i * 2))
                    continue
                    ;;
                *)
                    printf -v "$__out_var" '%s' "$__gh_out"
                    printf -v "$__code_var" '%s' "$__gh_code"
                    return 0
                    ;;
            esac
        fi
        sleep $((__gh_i * 2))
    done
    printf -v "$__out_var" '%s' "$__gh_out"
    printf -v "$__code_var" '%s' "retry-exhausted"
    return 1
}

# 14 个仓库叶子名：两个核心镜像 + release.yml 矩阵里的 12 个常驻微服务。
# 这里刻意写死而不是去 parse release.yml：本脚色的职责是"核对产物"，
# 矩阵漂移由 validate-deploy-assets.sh 的第 2/10 节负责（两处职责不重叠，避免自我印证）。
LEAVES="opsmesh-binary opsmesh-agent
auth-svc device-svc alert-svc task-svc config-svc log-svc
aio-svc autoscaler-svc gpu-svc incident-svc portal-svc runbook-svc"

# token <仓库叶子名>：成功时把匿名 pull token 打到 stdout 并返回 0。
#   返回 1 = 传输层重试耗尽（够不着 GHCR——这**不能**推出"仓库不存在"）；
#   返回 2 = GHCR 给出确定性拒绝（非 200，或响应里没有 token，等价于该公开仓库不存在/不可拉）。
# 区分这两种是本次改动的核心：原脚本把它们并成一类判红，于是抖动会伪装成发布缺陷。
token() {
    local body code tk
    if ! ghcr_get body code "https://ghcr.io/token?scope=repository:${NS}/$1:pull"; then
        return 1
    fi
    if [ "$code" != "200" ]; then
        return 2
    fi
    tk="$(printf '%s\n' "$body" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')"
    if [ -z "$tk" ]; then
        return 2
    fi
    printf '%s' "$tk"
}

# 注意 Accept 必须含 OCI index，否则 buildx 推的 OCI manifest 会被**误报成 404**
# （本仓 §19 取证时踩过一次，差点把"存在"写成"不存在"）。
ACCEPT='Accept: application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json'
NLEAVES=$(printf '%s\n' $LEAVES | grep -c .)
tags_of() {
    local leaf="$1" tk body code rc
    tk="$(token "$leaf")"; rc=$?
    [ "$rc" -ne 0 ] && return "$rc"      # 1=GHCR 不可达（重试耗尽）；2=确定性拒绝
    if ! ghcr_get body code -H "Authorization: Bearer $tk" \
        "https://ghcr.io/v2/${NS}/${leaf}/tags/list?n=4000"; then
        return 1
    fi
    if [ "$code" != "200" ]; then
        return 2
    fi
    printf '%s' "$body"
}

declare -a PUBLISHED=()
declare -A DIG_VER=()   # leaf → :<版本> 的 manifest digest，供第 ⑤ 项复用（不留第二个来源）
echo "== ① + ② 镜像 tag 与签名/证据链（${NS}/*:${VER}）=="
MISSING_TAG=(); MISSING_SIG=(); MISSING_ATT=()
for leaf in $LEAVES; do
    tk="$(token "$leaf")"; tkrc=$?
    if [ "$tkrc" -ne 0 ]; then
        if [ "$tkrc" -eq 1 ]; then
            unver "${leaf}: 取匿名 pull token 时 GHCR 不可达（已重试 ${GHCR_ATTEMPTS} 次）——这是**没核对上**，不是说它没发布"
        else
            bad "${leaf}: GHCR 确定性拒绝匿名 pull token（仓库不存在或不可匿名拉取）"
            MISSING_TAG+=("$leaf")
        fi
        continue
    fi
    body=""; code=""
    if ! ghcr_get body code -H "Authorization: Bearer $tk" \
        "https://ghcr.io/v2/${NS}/${leaf}/tags/list?n=4000"; then
        unver "${leaf}: 取 tag 列表重试耗尽（最后 HTTP=${code}）——未核对，不计入缺失"
        continue
    fi
    if [ "$code" != "200" ] || [ -z "$body" ]; then
        bad "${leaf}: tag 列表返回 HTTP=${code}$([ -z "$body" ] && printf '（且响应体为空）')"
        MISSING_TAG+=("$leaf"); continue
    fi
    if grep -qE "\"${VER}\"" <<<"$body"; then
        PUBLISHED+=("${leaf}:${VER}")
    else
        bad "${leaf}: 没有 :${VER} tag（release job 未跑成功，或该仓库没进矩阵）"
        MISSING_TAG+=("$leaf")
    fi
    # .sig/.att 在 GHCR 里以**本版本 manifest 的 digest**命名（sha256-<digest>.sig），
    # 所以必须先把 :VER 解析成 digest 再找，否则"仓库里有个 .sig"会被当成"这个版本签过名"。
    hdrs=""; hcode=""
    if ! ghcr_get hdrs hcode -I -H "Authorization: Bearer $tk" -H "$ACCEPT" \
        "https://ghcr.io/v2/${NS}/${leaf}/manifests/${VER}"; then
        unver "${leaf}:${VER} 解析 manifest 重试耗尽（最后 HTTP=${hcode}）——签名/证据链**未核对**"
        continue
    fi
    dig="$(tr -d '\r' <<<"$hdrs" \
        | sed -n 's/^ *[Dd]ocker-[Cc]ontent-[Dd]igest: *sha256:\([0-9a-f]\{64\}\).*/\1/p' | head -1)"
    if [ -z "$dig" ]; then
        if [ "$hcode" = "404" ]; then
            if grep -qE "\"${VER}\"" <<<"$body"; then
                bad "${leaf}:${VER} 的 tag 列表里有它，但 manifest 返回 404（tag 与 manifest 不一致，判红）"
            else
                bad "${leaf}:${VER} 的 manifest 404——与上面「没有该 tag」同源，签名/证据链无从核对"
            fi
        else
            bad "${leaf}:${VER} 解析不到 manifest digest（HTTP=${hcode}；无法核对签名，判红）"
        fi
        MISSING_SIG+=("$leaf"); MISSING_ATT+=("$leaf"); continue
    fi
    DIG_VER["$leaf"]="$dig"
    grep -q "sha256-${dig}\.sig" <<<"$body" \
        || { bad "${leaf}:${VER} 没有对应的 .sig（cosign 签名缺失）"; MISSING_SIG+=("$leaf"); }
    grep -q "sha256-${dig}\.att" <<<"$body" \
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
        rbody="$(gh release view "v${VER}" --repo "$REPO" --json body --jq '.body' 2>/dev/null)"
        if grep -q "能力降级清单" <<<"$rbody"; then
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
    CHART="${ROOT}/deploy/helm/opsmesh"
    # 第三次渲染刻意把 12 个微服务全部 --set enabled=true（仍用生产 values）：
    # 默认/生产两次渲染合起来只有 4 个引用（服务默认禁用），单靠它们会漏掉
    # "启用服务后拿到浮动镜像"这一整类——实测改前 `-f values-production.yaml
    # --set services.task_svc.enabled=true` 得到的是 task-svc:latest。
    SVC_SETS=()
    for k in auth_svc device_svc alert_svc task_svc config_svc log_svc \
             aio_svc autoscaler_svc gpu_svc incident_svc portal_svc runbook_svc; do
        SVC_SETS+=(--set "services.${k}.enabled=true")
    done
    prod_rendered="$(
        { helm template x "${CHART}" -f "${CHART}/values-production.yaml" 2>/dev/null
          helm template x "${CHART}" -f "${CHART}/values-production.yaml" "${SVC_SETS[@]}" 2>/dev/null
        } | sed -n 's/^[[:space:]]*image:[[:space:]]*//p' | tr -d '"' | sort -u
    )"
    rendered="$(
        { helm template x "${CHART}" 2>/dev/null
          printf '%s\n' "${prod_rendered}"
        } | sort -u
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
        # 这里必须分「没核对上」与「核对到不存在」：run 1 那次报
        # "chart 引用了 GHCR 上不存在的镜像 autoscaler-svc:0.12.0 ⇒ 客户会 ErrImagePull"，
        # 而 release run 37221272686 的 12 个 build-and-push 全 success、
        # 直查该 tag 三轮都是 200——假红来自这一行原来的 tags_of 无重试。
        # 假红在这条门禁上尤其贵：它给出的正是让人不敢重跑、只能人工去核的那句话。
        body="$(tags_of "$leaf")"; src=$?
        case "$src" in
            1) unver "④ ${leaf}:${tag} 未能核对（GHCR 不可达，已重试 ${GHCR_ATTEMPTS} 次）——不计入缺失"
               continue ;;
            2) badrefs="${badrefs} ${leaf}:${tag}(GHCR 拒绝匿名拉取)"
               continue ;;
        esac
        if [ -z "$body" ] || ! grep -qE "\"${tag}\"" <<<"$body"; then
            badrefs="${badrefs} ${leaf}:${tag}"
        fi
    done <<< "$rendered"
    if [ "$n" -eq 0 ]; then
        bad "chart 里一个 ghcr.io/${NS}/* 的 image 都没渲染出来（扫描面塌了，判红）"
    elif [ -z "$badrefs" ]; then
        ok "chart 渲染的 ${n} 个 ghcr 镜像引用（含生产全服务启用态）全部存在于 GHCR"
    else
        bad "chart 引用了 GHCR 上不存在的镜像:${badrefs# }"
        echo "         ⇒ 客户 helm install 会 ErrImagePull（§32.9 的成因正是这一条）"
    fi

    # 生产态禁止浮动 tag：values-production.yaml 自己的注释写着「生产禁止 latest」，
    # 但这句话此前没有任何门禁守住——本断言把它变成可判定的。
    # 判定走 herestring 而不是 `producer | grep -q`（后者在本脚本的 pipefail 下是假阴性写法）。
    if [ -z "${prod_rendered}" ]; then
        bad "生产 values 一次镜像引用都没渲染出来（本节在空转，判红）"
    else
        floating=""
        while IFS= read -r ref; do
            [ -z "$ref" ] && continue
            case "$ref" in
                ghcr.io/"${NS}"/*:latest) floating="${floating} ${ref}" ;;
            esac
        done <<< "${prod_rendered}"
        if [ -z "${floating}" ]; then
            ok "生产 values（含 12 服务全启用态）渲染出的引用没有一个 :latest"
        else
            bad "生产 values 渲染出浮动镜像:${floating}"
            echo "         ⇒ 按生产清单装的客户会拉到任意新推送的镜像：版本不可追溯，"
            echo "           升级与回滚都不受控（应钉 tag，或钉 image.digest）"
        fi
    fi
fi

echo "== ⑤ 提权未重建（:${VER} 与 :<发布提交> 必须是同一个 manifest digest）=="
# 为什么单独核这一条：①②③④ 核的都是"存在与挂接"，没有一个能发现"版本 tag 被重新构建过"。
# 发布链现在是"先推不可变 :<sha> → 门禁 → registry 侧改标提权"，提权不产生新镜像，
# 所以 :<版本> 与 :<sha> 必须同 digest；一旦不同，客户拉到的那份就没被 Trivy 与签名看过，
# 而前面四项照样全绿。这条断言核对的是**提权这个动作**，不是它的产物存在与否。
# 发布提交有两个来源，都拿不到就如实报未核对，不猜：
#   · CI：release.yml 的 verify-artifacts job 直接把 github.sha 传进 RELEASE_SHA；
#   · 本地：退到 `git rev-list -n1 v<版本>`。
SHA="${RELEASE_SHA:-}"
if [ -z "$SHA" ]; then
    SHA="$(git rev-list -n 1 "v${VER}" 2>/dev/null || true)"
fi
if ! printf '%s' "$SHA" | grep -qE '^[0-9a-fA-F]{40}$'; then
    unver "拿不到 v${VER} 的 40 位发布提交（既没给 RELEASE_SHA，本地也解析不出该 tag）——第 ⑤ 项未核对"
else
    SHA="$(printf '%s' "$SHA" | tr 'A-F' 'a-f')"
    echo "   发布提交=${SHA}"
    MISMATCH=(); NO_SHA_TAG=()
    for leaf in $LEAVES; do
        want="${DIG_VER[$leaf]-}"
        if [ -z "$want" ]; then
            unver "${leaf}: 第 ② 项没取到 :${VER} 的 digest，第 ⑤ 项无从比对"
            continue
        fi
        tk="$(token "$leaf")"; tkrc=$?
        if [ "$tkrc" -ne 0 ]; then
            if [ "$tkrc" -eq 1 ]; then
                unver "${leaf}: 取匿名 pull token 重试耗尽——第 ⑤ 项未核对"
            else
                bad "${leaf}: GHCR 确定性拒绝匿名 pull token"
                NO_SHA_TAG+=("$leaf")
            fi
            continue
        fi
        h2=""; c2=""
        if ! ghcr_get h2 c2 -I -H "Authorization: Bearer $tk" -H "$ACCEPT" \
            "https://ghcr.io/v2/${NS}/${leaf}/manifests/${SHA}"; then
            unver "${leaf}: 解析 :${SHA} 的 manifest 重试耗尽（最后 HTTP=${c2}）——未核对"
            continue
        fi
        d2="$(tr -d '\r' <<<"$h2" \
            | sed -n 's/^ *[Dd]ocker-[Cc]ontent-[Dd]igest: *sha256:\([0-9a-f]\{64\}\).*/\1/p' | head -1)"
        if [ -z "$d2" ]; then
            if [ "$c2" = "404" ]; then
                bad "${leaf}: GHCR 上没有 :${SHA}（不可变锚点缺失）⇒ 无法证明 :${VER} 就是被门禁看过的那份"
                NO_SHA_TAG+=("$leaf")
            else
                unver "${leaf}:${SHA} 解析不到 digest（HTTP=${c2}）"
            fi
            continue
        fi
        if [ "$d2" != "$want" ]; then
            bad "${leaf}: :${VER} 与 :${SHA} 的 digest 不一致（${want:0:12}… vs ${d2:0:12}…）——版本 tag 被重建过，它从没被 Trivy/签名看过"
            MISMATCH+=("$leaf")
        fi
    done
    if [ "${#MISMATCH[@]}" -eq 0 ] && [ "${#NO_SHA_TAG[@]}" -eq 0 ]; then
        ok "提权是改标不是重建：${NLEAVES} 个镜像的 :${VER} 与 :${SHA} 指向同一 manifest digest"
    else
        [ "${#MISMATCH[@]}" -gt 0 ] && echo "         digest 不一致: ${MISMATCH[*]-}"
        [ "${#NO_SHA_TAG[@]}" -gt 0 ] && echo "         缺 :<发布提交> tag: ${NO_SHA_TAG[*]-}"
    fi
fi

echo
echo "==================================================="
sum="  发布物验收 ${VER}：PASS=${PASS} FAIL=${FAIL}"
[ "$UNVERIFIED" -gt 0 ] && sum="${sum} UNVERIFIED=${UNVERIFIED}"
echo "$sum"
echo "==================================================="
if [ "$UNVERIFIED" -gt 0 ]; then
    echo "  有 ${UNVERIFIED} 项**没能核对上**（GHCR 传输失败，已重试 ${GHCR_ATTEMPTS} 次）。" >&2
    echo "  它既不代表发布有缺陷、也不代表发布没问题——是「未知」。处置动作是重跑本脚本" >&2
    echo "  （或调大 GHCR_ATTEMPTS / 换网络），而不是去查镜像有没有推。" >&2
fi
# 「未知」同样非零退出（绝不把没核对的东西当通过），但与「核对到缺失」分开计数——
# 这两种红的处置动作相反，混在一起就会让人开始不信这条门禁。
[ "$FAIL" -eq 0 ] && [ "$UNVERIFIED" -eq 0 ]
