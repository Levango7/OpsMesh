#!/usr/bin/env bash
# build-airgap-bundle.sh —— D1 全离线安装包构建（P0 准入五项·方案甲并行轨；2026-11-11 用户授权接管无人认领项）
#
# 要解决的问题：出厂 compose 形态（docker-compose.prod.yml + prod-proxy overlay）的全部镜像、
# Helm chart、部署树与验收脚本，在一台**联网**构建机上打成一个 tar，搬到断网机直接起栈；
# 断网机不需要 docker pull / helm repo / 任何外网。
#
# 镜像清单的单一事实源 = compose 文件本身的 image 行（prod + prod-proxy overlay），
# ${OPSMESH_VERSION}（含 :?… 报错变体）由 --version 代入后去重。不从任何手抄清单派生 ⇒
# 未来加服务不漏图；清单与 compose 漂移会在构建时立即暴露（守卫图 opsmesh/controlplane 必须出现）。
#
# opsmesh/* 图必须已在本地镜像库（构建机先跑过 deploy/docker/scripts/deploy.sh up，
# 或自行 docker build）；第三方图本地没有时自动拉（走同目录 docker-pull-retry.sh，
# 限流/抖动有界重试）；AIRGAP_SKIP_PULL=1 时缺图直接判红。缺图一律判红，不静默跳过。
#
# 用法: build-airgap-bundle.sh --version <x.y.z> [--out DIR] [--deploy-dir DIR]
# 环境: AIRGAP_SKIP_PULL=1  缺失镜像不自动拉取（直接判红）
# 退出码: 0 = bundle 产出（路径打印到 stdout 末行）
#         1 = 缺图/工具缺失/校验失败（判红）
#         2 = 用法错
# 未覆盖（如实记账）：
#   - 龙芯/飞腾架构（A1 项，不在本批）；本 bundle 只含构建机架构的镜像层。
#   - 断网机实跑验收为用户侧步骤（见 docs/airgap-install-sop.md）。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
DEPLOY_DIR_DEFAULT="${REPO_ROOT}/deploy/docker"
HELM_CHART="${REPO_ROOT}/deploy/helm/opsmesh"

VERSION=""
OUT_DIR=""
DEPLOY_DIR="${DEPLOY_DIR_DEFAULT}"
SKIP_PULL="${AIRGAP_SKIP_PULL:-0}"

while [ "$#" -gt 0 ]; do
    case "$1" in
        --version) VERSION="${2:-}"; shift 2 ;;
        --out)     OUT_DIR="${2:-}"; shift 2 ;;
        --deploy-dir) DEPLOY_DIR="${2:-}"; shift 2 ;;
        *) echo "red 未知参数：$1" >&2; exit 2 ;;
    esac
done

# latest 单独判 exit 1（同 deploy.sh:239-242 口径：不可追溯）；其它非法值归用法错 exit 2
[ "${VERSION}" = "latest" ] && { echo "red 版本 latest 禁止（不可追溯，同 deploy.sh:240 口径）" >&2; exit 1; }
case "${VERSION}" in
    ''|*[!0-9.]*) echo "red --version 必须是 x.y.z（实得 '${VERSION}'）" >&2; exit 2 ;;
esac

for tool in docker helm sha256sum tar grep sed; do
    command -v "${tool}" >/dev/null 2>&1 || { echo "red 缺工具 ${tool}" >&2; exit 1; }
done
[ -f "${DEPLOY_DIR}/docker-compose.prod.yml" ] || { echo "red 找不到 ${DEPLOY_DIR}/docker-compose.prod.yml" >&2; exit 1; }
[ -f "${HELM_CHART}/Chart.yaml" ] || { echo "red 找不到 chart ${HELM_CHART}" >&2; exit 1; }

OUT_DIR="${OUT_DIR:-opsmesh-airgap-${VERSION}}"
BUNDLE_NAME="opsmesh-airgap-${VERSION}"
STAGE="$(mktemp -d)/${BUNDLE_NAME}"
mkdir -p "${STAGE}/images" "${STAGE}/helm" "${STAGE}/deploy/scripts"

# ---- 1. 镜像清单：compose image 行机械派生 ------------------------------------
IMAGES=()
for f in "${DEPLOY_DIR}/docker-compose.prod.yml" "${DEPLOY_DIR}/docker-compose.prod-proxy.yml"; do
    while IFS= read -r img; do
        img="$(printf '%s' "${img}" | sed -E 's/\$\{OPSMESH_VERSION[^}]*\}/'"${VERSION}"'/g')"
        IMAGES+=("${img}")
    done < <(grep -E '^[[:space:]]*image:' "${f}" | sed -E 's/^[[:space:]]*image:[[:space:]]*//')
done
# 去重（保序）
DEDUP=()
for img in "${IMAGES[@]:-}"; do
    [ -z "${img}" ] && continue
    seen=0
    for d in "${DEDUP[@]:-}"; do [ "${d}" = "${img}" ] && { seen=1; break; }; done
    [ "${seen}" = 0 ] && DEDUP+=("${img}")
done

guard_found=0
for img in "${DEDUP[@]:-}"; do [ "${img}" = "opsmesh/controlplane:${VERSION}" ] && guard_found=1; done
[ "${guard_found}" = 1 ] || { echo "red 守卫图 opsmesh/controlplane:${VERSION} 未在派生清单中（compose 解析漂移，判红）" >&2; exit 1; }
echo "ok   派生镜像 ${#DEDUP[@]} 个（compose prod + prod-proxy overlay）"

# ---- 2. 镜像就位：本地有则用，没有则拉 ----------------------------------------
PULL_SCRIPT="${SCRIPT_DIR}/docker-pull-retry.sh"
for img in "${DEDUP[@]}"; do
    if docker image inspect "${img}" >/dev/null 2>&1; then
        echo "ok   本地已有 ${img}"
        continue
    fi
    if [ "${SKIP_PULL}" = "1" ]; then
        echo "red  ${img} 本地缺失且 AIRGAP_SKIP_PULL=1（判红）" >&2
        exit 1
    fi
    echo "     拉取 ${img} …"
    if [ -x "${PULL_SCRIPT}" ]; then
        bash "${PULL_SCRIPT}" "${img}"
    else
        docker pull "${img}"
    fi
    docker image inspect "${img}" >/dev/null 2>&1 || { echo "red  ${img} 拉取后仍缺失（判红）" >&2; exit 1; }
done

# ---- 3. docker save + 逐图校验和 ----------------------------------------------
MANIFEST="${STAGE}/manifest.tsv"
{
    echo "# OpsMesh airgap bundle manifest"
    echo "# version=${VERSION}"
    echo "# built_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo "# docker_version=$(docker version --format '{{.Server.Version}}' 2>/dev/null || echo unknown)"
    echo "# helm_version=$(helm version --short 2>/dev/null || echo unknown)"
    echo "# columns: type<TAB>ref<TAB>path<TAB>sha256"
    echo -e "type\tref\tpath\tsha256"
} > "${MANIFEST}"

for img in "${DEDUP[@]}"; do
    tar_name="images/$(printf '%s' "${img}" | tr '/:' '__').tar"
    docker save -o "${STAGE}/${tar_name}" "${img}"
    img_id="$(docker image inspect "${img}" --format '{{.Id}}')"
    tar_sha="$(sha256sum "${STAGE}/${tar_name}" | cut -d' ' -f1)"
    printf 'image\t%s\t%s\t%s\n' "${img}" "${tar_name}" "${tar_sha}" >> "${MANIFEST}"
    printf '# image_id %s %s\n' "${img}" "${img_id}" >> "${MANIFEST}"
    echo "ok   saved ${img} (${img_id})"
done

# ---- 4. chart + 生产 values ----------------------------------------------------
helm package "${HELM_CHART}" -d "${STAGE}/helm" >/dev/null
CHART_TGZ="$(find "${STAGE}/helm" -name 'opsmesh-*.tgz' | head -1)"
[ -n "${CHART_TGZ}" ] || { echo "red  helm package 未产出 tgz（判红）" >&2; exit 1; }
cp "${REPO_ROOT}/deploy/helm/opsmesh/values-production.yaml" "${STAGE}/helm/values-production.yaml"
printf 'chart\topsmesh\t%s\t%s\n' "helm/$(basename "${CHART_TGZ}")" "$(sha256sum "${CHART_TGZ}" | cut -d' ' -f1)" >> "${MANIFEST}"
printf 'chart-values\topsmesh-production\t%s\t%s\n' "helm/values-production.yaml" "$(sha256sum "${STAGE}/helm/values-production.yaml" | cut -d' ' -f1)" >> "${MANIFEST}"

# ---- 5. 部署树 + 验收脚本 -------------------------------------------------------
tar czf "${STAGE}/deploy/docker-tree.tar.gz" -C "${DEPLOY_DIR}" .
printf 'deploy-tree\tcompose-prod\t%s\t%s\n' "deploy/docker-tree.tar.gz" "$(sha256sum "${STAGE}/deploy/docker-tree.tar.gz" | cut -d' ' -f1)" >> "${MANIFEST}"
cp "${REPO_ROOT}/deploy/scripts/verify-runtime.sh" "${STAGE}/deploy/scripts/verify-runtime.sh"
printf 'verify-script\truntime\t%s\t%s\n' "deploy/scripts/verify-runtime.sh" "$(sha256sum "${STAGE}/deploy/scripts/verify-runtime.sh" | cut -d' ' -f1)" >> "${MANIFEST}"

# ---- 6. README + 总校验和 --------------------------------------------------------
cat > "${STAGE}/README.txt" <<EOF
OpsMesh airgap bundle ${VERSION}
构建机：$(date -u +%Y-%m-%dT%H:%M:%SZ)
内容：images/（$(printf '%s' "${#DEDUP[@]}") 个镜像 tar）+ helm/（chart + 生产 values）
      + deploy/（compose 部署树 tar + verify-runtime.sh）+ manifest.tsv
离线安装：bash install-airgap.sh <本包 tar> --target <目录>   （见 docs/airgap-install-sop.md）
EOF

# 两遍法：先物化文件列表再算哈希。边遍历边写 CHECKSUMS.sha256 有竞态
# （find 可能把正在写的 CHECKSUMS.sha256 自己列进去 ⇒ 自指哈希，sha256sum -c 必红）；
# 列表放 STAGE 之外（系统临时目录），find 遍历与列表写入无交集，竞态与静态告警双消。
LIST="$(mktemp)"
( cd "${STAGE}" && find . -type f ! -name CHECKSUMS.sha256 -print0 | sort -z > "${LIST}" \
    && xargs -0 sha256sum < "${LIST}" > CHECKSUMS.sha256 )
rm -f "${LIST}"
echo "ok   CHECKSUMS.sha256 覆盖 $(wc -l < "${STAGE}/CHECKSUMS.sha256" | tr -d ' ') 个文件"

# ---- 7. 打成单一 tar -------------------------------------------------------------
tar czf "${OUT_DIR}.tar.gz" -C "$(dirname "${STAGE}")" "${BUNDLE_NAME}"
case "${OUT_DIR}" in /*) FINAL="${OUT_DIR}.tar.gz" ;; *) FINAL="$(pwd)/${OUT_DIR}.tar.gz" ;; esac
[ -f "${FINAL}" ] || { echo "red  产物 ${FINAL} 不存在（判红）" >&2; exit 1; }
echo "ok   bundle: ${FINAL} ($(du -h "${FINAL}" | cut -f1))，镜像 ${#DEDUP[@]} 个"
