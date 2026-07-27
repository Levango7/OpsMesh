# OpsMesh 二进制内核（U-05）：同一份代码构建控制面与 agent 两种角色。
# 多阶段构建：build 阶段拉取依赖并编译静态二进制，runtime 阶段用 distroless 精简镜像。
FROM golang:1.22 AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /opsmesh ./cmd/opsmesh

FROM gcr.io/distroless/static-debian12 AS runtime
COPY --from=build /opsmesh /usr/local/bin/opsmesh
# 默认起控制面；agent 模式通过 deployment args 覆盖：["--mode=agent","--control-addr=..."]
ENTRYPOINT ["/usr/local/bin/opsmesh"]
CMD ["--mode=controlplane"]
