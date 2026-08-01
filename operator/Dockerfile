# Build the manager binary.
# syntax=docker/dockerfile:1.6
FROM golang:1.22 AS builder

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
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=builder /out/manager /manager
USER nonroot:nonroot
ENTRYPOINT ["/manager"]