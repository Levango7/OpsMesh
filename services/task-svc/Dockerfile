# task-svc 微服务镜像（CICD 修复 C-1：补齐 release.yml 矩阵缺失的 Dockerfile）。
# 多阶段构建：build 阶段参照仓库根 Dockerfile（golang:1.26-bookworm 编译静态二进制），
# runtime 阶段 alpine:3.23 + curl（与 deploy/docker/Dockerfile.controlplane 一致，
# 保证 docker-compose.yml / docker-compose.prod.yml 的 healthcheck 可执行）。
# 注意：task-svc 是独立 go.mod 模块，构建必须在本服务目录内进行。
# base image digest 由 Renovate 自动钉死（见 .github/renovate.json）。
# 手动钉死：crane digest golang:1.26-bookworm → FROM golang:1.26-bookworm@sha256:<digest> AS build
# syntax=docker/dockerfile:1.6
# A-2 阶段：补齐 arm64 多架构构建支持（TD-60 阶段 2 恢复完整生产路径）。
# buildx 自动注入 TARGETOS/TARGETARCH，CI 矩阵 linux/amd64+linux/arm64 同一份 Dockerfile。
FROM golang:1.26-bookworm AS build
# 国内网络环境 proxy.golang.org 不可达，走 goproxy.cn 公共代理（CI 同样可用）。
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod verify && go mod download
COPY . .
# 多架构构建：TARGETOS/TARGETARCH 由 buildx 自动注入（单架构 docker build 时回退默认值）。
# CGO_ENABLED=0 保证静态编译，无需 glibc/musl 依赖，单一二进制跨发行版可运行。
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /svc ./cmd/task-svc

# 手动钉死：crane digest alpine:3.23 → FROM alpine:3.23@sha256:<digest> AS runtime
FROM alpine:3.23 AS runtime
# 运行时系统依赖：ca-certificates（HTTPS 拨号）+ curl（healthcheck 探活）。
# 经验库 2026-09-09：所有 RUN apk add 必须在 USER 切换前，否则非 root 无权限安装。
RUN apk --no-cache upgrade && apk --no-cache add ca-certificates curl
# 以非 root 运行（H16，与仓库根 Dockerfile 的 nonroot 基线一致）。
RUN addgroup -S svc && adduser -S svc -G svc
USER svc
COPY --from=build /svc /usr/local/bin/svc
# 监听端口（TASK_SVC_HTTP_PORT/TASK_SVC_GRPC_PORT）：8081；gRPC 默认 50052。
EXPOSE 8081 50052
CMD ["svc"]
