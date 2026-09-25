#!/usr/bin/env bash
# build-enterprise-web.sh — 构建企业版前端（Vue3 + Vite）并组装进 Go embed 目录（P0-3）。
#
# 为什么需要「组装」这一步：
#   go:embed 不能跨目录，企业版前端源码在 web/enterprise/，而产物必须落在
#   internal/controlplane/embed/enterprise/ 才能被 `go build` 打进二进制。
#   本脚本负责 npm 构建 + 拷贝，之后 `go build ./cmd/opsmesh` 即得到内置企业版前端的二进制。
#
# 使用：
#   bash deploy/docker/scripts/build-enterprise-web.sh          # 有 node_modules 则直接 build，否则先 npm ci
#   bash deploy/docker/scripts/build-enterprise-web.sh --clean  # 强制 npm ci（干净依赖树）
#   bash deploy/docker/scripts/build-enterprise-web.sh --no-build  # 跳过 npm，只把已有 dist/ 组装进 embed 目录
#                                                                  （CI 已单独跑过 npm run build 时用）
#
# 不装 Node 也能构建：产物目录里入库了 placeholder.html 占位页，此时
# `go build` 仍成功，但 /enterprise/ 显示「未内置」说明页、个人版入口自动隐藏。
# 官方镜像（deploy/docker/Dockerfile.controlplane）内置 Node 构建阶段，客户无需本地 Node。

set -euo pipefail

CLEAN=0
NO_BUILD=0
for arg in "$@"; do
  case "$arg" in
    --clean) CLEAN=1 ;;
    --no-build) NO_BUILD=1 ;;
    -h|--help) sed -n '2,18p' "$0"; exit 0 ;;
    *) echo "未知参数：$arg（可用：--clean / --no-build / --help）" >&2; exit 2 ;;
  esac
done

# 定位仓库根（脚本位于 deploy/docker/scripts/ 下）。
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
WEB_DIR="$ROOT/web/enterprise"
EMBED_DIR="$ROOT/internal/controlplane/embed/enterprise"
DIST_DIR="$WEB_DIR/dist"

echo "==> 仓库根：$ROOT"

command -v npm >/dev/null 2>&1 || {
  echo "错误：未找到 npm。请安装 Node.js ≥ 18（源码构建方式），或直接使用 Docker/官方镜像。" >&2
  exit 1
}
echo "==> Node $(node -v 2>/dev/null || echo '?') / npm $(npm -v)"

cd "$WEB_DIR"
if [ "$NO_BUILD" = "1" ]; then
  echo "==> --no-build：复用已有 dist/（需已存在 $DIST_DIR/index.html）"
  [ -f "$DIST_DIR/index.html" ] || { echo "错误：--no-build 但缺少 $DIST_DIR/index.html，请先 npm run build" >&2; exit 1; }
else
  if [ "$CLEAN" = "1" ] || [ ! -d node_modules ]; then
    echo "==> npm ci（可复现安装，依据 package-lock.json）"
    npm ci --no-audit --no-fund
  else
    echo "==> 复用已有 node_modules（如需干净依赖树请加 --clean）"
  fi

  echo "==> npm run build（vite build，base=/enterprise/）"
  npm run build
fi

[ -f "$DIST_DIR/index.html" ] || { echo "错误：构建产物缺少 $DIST_DIR/index.html" >&2; exit 1; }

echo "==> 组装到 $EMBED_DIR"
mkdir -p "$EMBED_DIR"
# 清掉上一次的构建产物，但保留 placeholder.html 与 .gitignore（入库文件）。
# placeholder.html 始终留在目录内：它保证「未装 Node 的源码构建」下 go:embed 仍有文件可嵌、
# 且能识别出未内置企业版前端（bundleAvailable() 以 index.html 是否含标记为准）。
find "$EMBED_DIR" -mindepth 1 -maxdepth 1 \
  ! -name 'placeholder.html' ! -name '.gitignore' -exec rm -rf {} +
cp -R "$DIST_DIR"/. "$EMBED_DIR"/
COUNT=$(find "$EMBED_DIR" -type f | wc -l | tr -d ' ')
SIZE=$(du -sh "$EMBED_DIR" | cut -f1)
echo "==> 完成：$COUNT 个文件，$SIZE（含占位页，运行时以外壳 index.html 是否含标记判定）"
echo "    下一步：go build ./cmd/opsmesh   （随后访问 http://<host>:8080/enterprise/）"
