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
FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
# 构建期校验模块完整性（防供应链投毒 / go.sum 漂移，task 安全 P2-5）
RUN go mod verify && go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /opsmesh ./cmd/opsmesh

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
