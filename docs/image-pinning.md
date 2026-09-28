# 镜像 Digest Pinning 指南

## 为什么需要 Digest Pinning

Docker 镜像标签（如 `golang:1.26-bookworm`）是可变的——同一标签可能指向不同内容。
生产环境使用 digest（`@sha256:...`）引用确保镜像内容不可变，防止供应链投毒。

## 如何获取 Digest

```bash
# 方法 1：docker pull + inspect
docker pull golang:1.26-bookworm
docker inspect --format='{{index .RepoDigests 0}}' golang:1.26-bookworm

# 方法 2：crane（推荐，无需拉取完整镜像）
crane digest golang:1.26-bookworm

# 方法 3：skopeo
skopeo inspect docker://golang:1.26-bookworm --format '{{.Digest}}'
```

## 如何应用

将 Dockerfile 中的：
```dockerfile
FROM golang:1.26-bookworm AS build
```
改为：
```dockerfile
FROM golang:1.26-bookworm@sha256:abc123... AS build
```

## 自动化

- **Renovate**：配置 `regexManagers` 自动检测并更新 digest
- **Dependabot**：GitHub 原生支持 Dockerfile 依赖更新
- **CI**：镜像构建流水线已内置签名与自验证——`ci.yml` 的 `image` / `image-agent`（核心镜像）与
  `release.yml` 的 `build-and-push`（12 个微服务镜像）在推送后 `cosign sign`，随即
  `cosign verify --certificate-identity-regexp '…/<workflow>\.yml@' --certificate-oidc-issuer https://token.actions.githubusercontent.com`
  自验一遍；**验不过即失败**（避免签名步骤空转却 job 绿）。消费方验证命令同上。

## OpsMesh 涉及的 Base Image

| Dockerfile | Build Image | Runtime Image |
|---|---|---|
| `Dockerfile` | `golang:1.26-bookworm` | `gcr.io/distroless/static-debian12` |
| `Dockerfile.agent` | `golang:1.26-bookworm` | `gcr.io/distroless/base-debian12` |
| `operator/Dockerfile` | `golang:1.26-bookworm` | `gcr.io/distroless/static-debian12:nonroot` |