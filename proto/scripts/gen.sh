#!/usr/bin/env bash
# protobuf Go stub 生成脚本。
# 用 Docker 镜像 bufbuild/buf 生成，无需本机安装 buf/protoc/protoc-gen-go。
#
# 用法：
#   bash proto/scripts/gen.sh          # 生成到 internal/grpcx/pb/
#   BUF_VERSION=v1.47.0 bash proto/scripts/gen.sh  # 钉版本
#
# 前置：本机已装 Docker。无网络时无法拉镜像（CI 有网络）。
# 生成结果提交到仓库（internal/grpcx/pb/*.go），避免无 Docker 的开发者重新生成。
set -euo pipefail

# 脚本位于 proto/scripts/，proto/ 在上一级。
PROTO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BUF_VERSION="${BUF_VERSION:-latest}"

echo "==> buf generate (Docker bufbuild/buf:${BUF_VERSION})"
echo "    proto dir: ${PROTO_DIR}"
echo "    output:    ${PROTO_DIR}/../internal/grpcx/pb/"

# 用 Docker 运行 buf generate，把 proto/ 挂载到 /workspace，
# 输出目录 ../internal/grpcx/pb 通过挂载仓库根可见。
# 注意：buf.gen.yaml 中 out: ../internal/grpcx/pb 是相对 proto/ 的路径，
# 故需把仓库根也挂进容器（这里挂整个 OpsMesh 仓库到 /repo，proto 在 /repo/proto）。
REPO_ROOT="$(cd "${PROTO_DIR}/.." && pwd)"

docker run --rm \
  -v "${REPO_ROOT}:/repo" \
  -w /repo/proto \
  bufbuild/buf:"${BUF_VERSION}" \
  buf generate

echo "==> done. 生成文件："
ls -1 "${REPO_ROOT}/internal/grpcx/pb/"*.pb.go 2>/dev/null || true
ls -1 "${REPO_ROOT}/internal/grpcx/pb/"*_grpc.pb.go 2>/dev/null || true
echo "==> 提醒：生成后请运行 go build ./... && go test ./internal/grpcx/... 验证。"