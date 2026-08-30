# log-svc 微服务镜像（CICD 修复 C-1：补齐 release.yml 矩阵缺失的 Dockerfile）。
# 多阶段构建：build 阶段参照仓库根 Dockerfile（golang:1.26-bookworm 编译静态二进制），
# runtime 阶段 alpine:3.19 + curl（与 deploy/docker/Dockerfile.controlplane 一致，
# 保证 docker-compose.yml / docker-compose.prod.yml 的 healthcheck 可执行）。
# 注意：log-svc 是独立 go.mod 模块，构建必须在本服务目录内进行。
# base image digest 由 Renovate 自动钉死（见 .github/renovate.json）。
# 手动钉死：crane digest golang:1.26-bookworm → FROM golang:1.26-bookworm@sha256:<digest> AS build
FROM golang:1.26-bookworm AS build
# 国内网络环境 proxy.golang.org 不可达，走 goproxy.cn 公共代理（CI 同样可用）。
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod verify && go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /svc ./cmd/log-svc

# 手动钉死：crane digest alpine:3.19 → FROM alpine:3.19@sha256:<digest> AS runtime
FROM alpine:3.19 AS runtime
RUN apk --no-cache add ca-certificates curl
# 以非 root 运行（H16，与仓库根 Dockerfile 的 nonroot 基线一致）。
RUN addgroup -S svc && adduser -S svc -G svc
USER svc
COPY --from=build /svc /usr/local/bin/svc
# 监听端口（LOG_SVC_HEALTH_ADDR/LOG_SVC_GRPC_ADDR）：健康检查 HTTP 默认 8080；gRPC 默认 9090。
EXPOSE 8080 9090
CMD ["svc"]
