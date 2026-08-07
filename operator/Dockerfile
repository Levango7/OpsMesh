# Build the manager binary.
# syntax=docker/dockerfile:1.6
# P2-5 安全建议：生产环境应钉死 base image digest，防止供应链投毒。
# 格式：FROM golang:1.26-bookworm@sha256:<digest> AS builder
# 获取 digest：docker build --pull=always 或 crane digest golang:1.26-bookworm
# CI 中可用 Renovate/Dependabot 自动更新 digest。
FROM golang:1.26-bookworm AS builder

WORKDIR /workspace
# Cache modules by copying go.mod/go.sum first.
COPY go.mod go.sum ./
RUN go mod download

COPY api/        api/
COPY cmd/        cmd/
COPY internal/   internal/

# Build a static binary suitable for distroless.
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/manager ./cmd

# Runtime image: distroless static for the smallest attack surface.
# P2-5 安全建议：distroless 镜像也应钉死 digest。
# 格式：FROM gcr.io/distroless/static-debian12:nonroot@sha256:<digest>
# 获取 digest：crane digest gcr.io/distroless/static-debian12:nonroot
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=builder /out/manager /manager
USER nonroot:nonroot
ENTRYPOINT ["/manager"]