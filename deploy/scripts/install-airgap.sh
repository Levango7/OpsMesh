#!/usr/bin/env bash
# install-airgap.sh —— D1 全离线安装包在断网机的安装入口（P0 准入五项·方案甲并行轨）
#
# 前置：build-airgap-bundle.sh 产出的单一 tar 已搬运到本机（U盘/内网拷贝均可）。
#
# 做四件事，顺序即安全边界：
#   1. 解包到目标目录（目标目录必须不存在或是空目录，防覆盖既有栈）
#   2. sha256sum -c CHECKSUMS.sha256 全量比对（任何一位不符即判红，绝不继续装载）
#   3. 按 manifest.tsv 的 image 行 docker load（顺序与构建一致）
#   4. compose 形态：deploy/docker 树解包后 deploy.sh init + up --no-build
#      （--no-build = 离线机不编译、不拉取，只用刚 load 的镜像；同 deploy.sh:837 口径）
#
# Helm 形态（--mode helm）：chart + values-production.yaml 随包，helm install 从本地
# tgz 装；不经过本脚本的 compose 路径，步骤见 docs/airgap-install-sop.md §4。
#
# 用法: install-airgap.sh <bundle.tar.gz> [--target DIR] [--mode compose|helm] [--skip-verify-runtime]
# 退出码: 0 = 安装完成（compose 形态末尾打印 verify-runtime 运行提示）
#         1 = 校验/装载/部署失败（判红）
#         2 = 用法错（目标目录非空等）
# 未覆盖（如实记账）：断网机上的 mysql 首次初始化耗时与磁盘水位不在本脚本判据内，
#   由 deploy.sh 自带预检（MIN_DISK_GB=10 等）与 verify-runtime.sh 黑盒断言兜底。

set -euo pipefail

BUNDLE_TAR="${1:-}"
TARGET="opsmesh-airgap"
MODE="compose"
SKIP_VERIFY=""
shift || true
while [ "$#" -gt 0 ]; do
    case "$1" in
        --target) TARGET="${2:-}"; shift 2 ;;
        --mode)   MODE="${2:-}"; shift 2 ;;
        --skip-verify-runtime) SKIP_VERIFY=1; shift ;;
        *) echo "red 未知参数：$1" >&2; exit 2 ;;
    esac
done

case "${MODE}" in compose|helm) ;; *) echo "red --mode 只收 compose|helm（实得 ${MODE}）" >&2; exit 2 ;; esac
[ -n "${BUNDLE_TAR}" ] || { echo "red 缺 bundle tar 参数" >&2; exit 2; }
[ -f "${BUNDLE_TAR}" ] || { echo "red  ${BUNDLE_TAR} 不存在" >&2; exit 1; }
command -v sha256sum >/dev/null 2>&1 || { echo "red 缺 sha256sum" >&2; exit 1; }
command -v docker >/dev/null 2>&1 || { echo "red 缺 docker" >&2; exit 1; }

if [ -e "${TARGET}" ]; then
    n="$(find "${TARGET}" -mindepth 1 -maxdepth 1 | wc -l | tr -d ' ')"
    [ "${n}" = 0 ] || { echo "red  目标目录 ${TARGET} 非空（${n} 项），拒绝覆盖" >&2; exit 2; }
fi

echo "=== 1. 解包 ==="
mkdir -p "${TARGET}"
tar xzf "${BUNDLE_TAR}" -C "${TARGET}" --strip-components=1
BUNDLE_DIR="${TARGET}"

echo "=== 2. 校验和比对 ==="
( cd "${BUNDLE_DIR}" && sha256sum -c CHECKSUMS.sha256 )
echo "ok   校验和全绿"

echo "=== 3. docker load（按 manifest.tsv image 行顺序）==="
loaded=0
while IFS=$'\t' read -r type ref path sha; do
    [ "${type}" = "image" ] || continue
    [ -n "${path}" ] || { echo "red  manifest image 行缺 path（ref=${ref}），判红" >&2; exit 1; }
    [ -n "${sha}" ]  || { echo "red  manifest image 行缺 sha256（ref=${ref}），判红" >&2; exit 1; }
    echo "     load ${ref}"
    docker load -i "${BUNDLE_DIR}/${path}"
    loaded=$((loaded+1))
done < "${BUNDLE_DIR}/manifest.tsv"
[ "${loaded}" -gt 0 ] || { echo "red  manifest 无 image 行（bundle 损坏或形态不符，判红）" >&2; exit 1; }
echo "ok   已装载 ${loaded} 个镜像"

if [ "${MODE}" = "helm" ]; then
    echo "=== 4. helm 形态（本地 chart 离线安装）==="
    chart="$(cd "${BUNDLE_DIR}" && find helm -name 'opsmesh-*.tgz' | head -1)"
    [ -n "${chart}" ] || { echo "red  bundle 内无 chart（判红）" >&2; exit 1; }
    echo "ok   chart 就绪：${chart}"
    echo "     执行：helm install opsmesh \"${chart}\" -f \"${BUNDLE_DIR}/helm/values-production.yaml\" --wait"
    echo "     （cluster 上下文与命名空间由你在断网机侧指定；本脚本不代为假设）"
    exit 0
fi

echo "=== 4. compose 形态部署（--no-build，镜像全部来自本 bundle）==="
DEPLOY_DIR="${BUNDLE_DIR}/deploy"
mkdir -p "${DEPLOY_DIR}/docker"
tar xzf "${BUNDLE_DIR}/deploy/docker-tree.tar.gz" -C "${DEPLOY_DIR}/docker"
( cd "${DEPLOY_DIR}/docker" && bash scripts/deploy.sh init )
( cd "${DEPLOY_DIR}/docker" && bash scripts/deploy.sh up --no-build -y )

if [ -n "${SKIP_VERIFY}" ]; then
    echo "ok   安装完成（--skip-verify-runtime：验收未跑）"
else
    echo "ok   安装完成。验收（黑盒断言，只读）："
    echo "     bash \"${BUNDLE_DIR}/deploy/scripts/verify-runtime.sh\""
fi
