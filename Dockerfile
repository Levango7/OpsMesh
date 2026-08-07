# OpsMesh 二进制内核（U-05）：同一份代码构建控制面与 agent 两种角色。
# 此 Dockerfile 用于 controlplane（控制面）：runtime 用 distroless/static-debian12，
# 无 shell、无包管理器，攻击面最小，适合常驻控制面服务。
# agent 角色需 sh 执行 shell/service 任务，请用 Dockerfile.agent（base-debian12 含 sh）。
# 多阶段构建：build 阶段拉取依赖并编译静态二进制，runtime 阶段用 distroless 精简镜像。
FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /opsmesh ./cmd/opsmesh

FROM gcr.io/distroless/static-debian12 AS runtime
# distroless static-debian12 内置 nonroot 用户（UID/GID 65532），以非 root 运行（H16）。
USER nonroot:nonroot
COPY --from=build /opsmesh /usr/local/bin/opsmesh
# distroless 无 curl/wget，HEALTHCHECK 复用二进制自身做轻量探活（要求 opsmesh 支持 --version 且快速退出）。
# 若二进制不支持该探测语义，可移除本行改由 K8s liveness/readiness probe（TCP/HTTP）兜底。
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s CMD ["/usr/local/bin/opsmesh", "--mode=controlplane", "--version"]
# 默认起控制面；agent 模式通过 deployment args 覆盖：["--mode=agent","--control-addr=..."]
ENTRYPOINT ["/usr/local/bin/opsmesh"]
CMD ["--mode=controlplane"]
