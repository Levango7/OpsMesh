# OpsMesh 二进制内核（U-05）：同一份代码构建控制面与 agent 两种角色。
# 此 Dockerfile 用于 controlplane（控制面）：runtime 用 distroless/static-debian12，
# 无 shell、无包管理器，攻击面最小，适合常驻控制面服务。
# agent 角色需 sh 执行 shell/service 任务，请用 Dockerfile.agent（base-debian12 含 sh）。
# 多阶段构建：build 阶段拉取依赖并编译静态二进制，runtime 阶段用 distroless 精简镜像。
# syntax=docker/dockerfile:1.6
# P2-2 供应链安全：base image digest 由 Renovate 自动钉死（见 .github/renovate.json）。
# Renovate 会检测 FROM 行并自动创建 PR 添加 @sha256:<digest>，合入后即钉死。
# CI 中有 digest 校验步骤（ci.yml security job）检查钉死状态。
# 手动钉死：crane digest golang:1.26-bookworm → FROM golang:1.26-bookworm@sha256:<digest> AS build

# ── 企业版前端构建阶段（P0-3）──
# 企业版前端（Vue3 + Vite，web/enterprise/）是控制面的交付物之一：产物经 go:embed
# 打进二进制，由 /enterprise/ 提供。没有这个阶段，客户拿到的镜像里只有占位页
# （P0-3：「企业版前端无任何可交付路径」）。发布镜像必须走本 Dockerfile，故在此构建。
# 受限网络可换源：docker build --build-arg NPM_REGISTRY=https://registry.npmmirror.com .
ARG NPM_REGISTRY=https://registry.npmjs.org
FROM node:22-alpine AS web
ARG NPM_REGISTRY
WORKDIR /src/web/enterprise
# 先只拷清单：依赖层可复用，改源码不触发重新 npm ci。
COPY web/enterprise/package.json web/enterprise/package-lock.json ./
RUN npm config set registry "$NPM_REGISTRY" && npm ci --no-audit --no-fund
COPY web/enterprise/ ./
# 构建失败即整个镜像构建失败（不静默降级为占位页）：交付物必须确定包含企业版前端。
RUN npm run build && test -f dist/index.html

FROM golang:1.26-bookworm AS build
# 国内网络环境 proxy.golang.org 不可达，走 goproxy.cn 公共代理（CI 同样可用）。
ENV GOPROXY=https://goproxy.cn,direct
# GOWORK=off：仓库根 go.work 声明 operator/ 与 services/* 模块，但 .dockerignore
# 刻意排除它们（构建上下文瘦身）——容器内 go.work 引用不存在的模块会直接报错
# （CI 实测：cannot load module operator listed in go.work）。主模块 go.mod
# 自洽（operator/services 均不被内核 import），关掉 workspace 按单模块构建。
ENV GOWORK=off
WORKDIR /src
COPY go.mod go.sum ./
# 构建期校验模块完整性（防供应链投毒 / go.sum 漂移，task 安全 P2-5）。
# M11：先 download 再 verify，校验已下载模块内容与 go.sum 哈希一致。
RUN go mod download && go mod verify
COPY . .
# 企业版前端产物（见上方 web 阶段）覆盖 embed 目录内的占位页 → go:embed 打进二进制。
# 缺此行则客户打开 /enterprise/ 只能看到「未内置」说明页（P0-3 缺陷）。
COPY --from=web /src/web/enterprise/dist/ ./internal/controlplane/embed/enterprise/
# 构建元信息注入（P1-6 /version 端点据此报告版本；缺省值 dev/unknown 表示本地构建）。
# -X 的包路径必须是模块路径，写成 opsmesh/... 会被链接器静默忽略（2026-09-26 实测）。
ARG VERSION=dev
ARG COMMIT=dev
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath     -ldflags="-s -w -X github.com/Levango7/OpsMesh/internal/version.Version=${VERSION} -X github.com/Levango7/OpsMesh/internal/version.Commit=${COMMIT} -X github.com/Levango7/OpsMesh/internal/version.Date=${BUILD_DATE}"     -o /opsmesh ./cmd/opsmesh

# P2-2 供应链安全：distroless 镜像 digest 同样由 Renovate 自动钉死。
# 手动钉死：crane digest gcr.io/distroless/static-debian12 → FROM gcr.io/distroless/static-debian12@sha256:<digest> AS runtime
FROM gcr.io/distroless/static-debian12 AS runtime
# distroless static-debian12 内置 nonroot 用户（UID/GID 65532），以非 root 运行（H16）。
USER nonroot:nonroot
COPY --from=build /opsmesh /usr/local/bin/opsmesh
# distroless 无 curl/wget，HEALTHCHECK 复用二进制 --health 子命令探活 localhost:8080/healthz。
# --health 在 config.Load 之前短路，纯 Go 标准库实现（无需 curl/sh），HTTP 200 → 退出 0，否则 → 退出 1。
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/usr/local/bin/opsmesh", "--health"]
# 默认起控制面；agent 模式通过 deployment args 覆盖：["--mode=agent","--control-addr=..."]
ENTRYPOINT ["/usr/local/bin/opsmesh"]
CMD ["--mode=controlplane"]
