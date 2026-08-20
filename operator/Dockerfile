# Build the manager binary.
# syntax=docker/dockerfile:1.6
# P2-2 供应链安全：base image digest 由 Renovate 自动钉死（见 .github/renovate.json）。
# Renovate 会检测 FROM 行并自动创建 PR 添加 @sha256:<digest>，合入后即钉死。
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
# P2-2 供应链安全：distroless 镜像 digest 同样由 Renovate 自动钉死。
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=builder /out/manager /manager
USER nonroot:nonroot
ENTRYPOINT ["/manager"]