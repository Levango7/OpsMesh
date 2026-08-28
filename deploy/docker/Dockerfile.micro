# Generic Dockerfile for OpsMesh Go microservices
# Build: docker build -f deploy/docker/Dockerfile.go -t opsmesh-svc ../services/<svc-name>
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o svc ./cmd/*/

FROM alpine:3.19
RUN apk --no-cache add ca-certificates curl
COPY --from=builder /app/svc /usr/local/bin/svc
EXPOSE 8100
ENTRYPOINT ["svc"]
