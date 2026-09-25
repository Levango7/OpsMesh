# deploy/k8s —— 本地 Kind 开发样例（非生产基线）

> **边界声明（2026-09-25 核实）**：本目录是**开发/联调样例**，用于在本地 Kind 集群快速拉起
> 5 个微服务并验证接口连通性。**它不是生产部署方案**，不要直接用于商用交付。

## 生产部署走哪里

| 场景 | 方案 | 位置 |
| --- | --- | --- |
| 生产 / 准生产（K8s） | Helm Chart（含 Secret 加密、PDB、ServiceMonitor、NetworkPolicy 等） | `deploy/helm/opsmesh/` |
| 单机 / 小规模交付 | Compose 生产栈（TLS 反代 + 强口令门禁） | `deploy/docker/docker-compose.prod.yml` |
| GitOps | ArgoCD 分段交付 | `deploy/gitops/` |

## 本样例覆盖什么

- 5 个微服务：`auth-svc`、`device-svc`、`task-svc`、`alert-svc`、`gpu-svc`
- 端口 / 探针路径 / env 变量名逐项对照源码核实（见各 `deployments/*.yaml` 头部注释）
- Ingress 路径按各服务**真实注册**的 `/api/v1/...` 前缀映射，不做 rewrite

## 本样例【不】覆盖什么（与生产的差距）

- **无 RBAC**：样例 Pod 运行期不访问 K8s API——`task-svc` 多副本选主默认走进程内 stub
  （`cmd/task-svc/main.go` → `leader.NewStub()`）。控制面集群能力（`internal/k8s/client.go`）
  所需 RBAC 由 Helm / 生产侧提供。原样例曾给 Pod 绑定含 `secrets` 写、`namespace` 删除的
  过权 ClusterRole，已删除。
- **无 TLS**：仅 Kind 本地网络，生产侧由反代/网关终结 TLS。
- **无 NetworkPolicy / 资源配额策略 / PDB**：见 Helm Chart。
- **密钥为脚本随机生成**（`deploy-opsmesh.sh` 的 `apply_secrets`，执行时会打印初始 admin 口令）：
  生产必须走正式密钥管理通道。
- **不含 `controlplane` / `log-svc` / `config-svc` / `portal-svc` / `aio-svc`**：样例只覆盖微服务面。

## 使用

```bash
# 1. 创建本地 Kind 集群（需 docker + kind + kubectl）
./create-cluster.sh

# 2. 构建镜像并加载进 Kind，然后部署
./deploy-opsmesh.sh --load-images

# 3. 后续仅重新部署（镜像已在集群内）
./deploy-opsmesh.sh --skip-images
```

部署完成后：

```bash
# 查看初始 admin 口令：在 apply_secrets 输出里（形如 [WARN] 初始 admin 口令…）
kubectl get pods -n opsmesh
kubectl port-forward svc/auth-svc 8081:8081 -n opsmesh
curl http://127.0.0.1:8081/health
```

## 镜像构建契约（重要）

微服务的 `go.mod` 均含 `replace opsmesh => ../..`，因此**必须**以**仓库根目录**为构建上下文，
使用根级 `Dockerfile.service`：

```bash
docker build -f Dockerfile.service \
  --build-arg SERVICE=auth-svc \
  --build-arg VERSION=0.9.0 \
  .
```

以单服务目录作上下文会失败：`go mod verify` 报
`replaced by ../../: open /go.mod: no such file`。
`deploy-opsmesh.sh --load-images` 已按此契约实现，与 `.github/workflows/release.yml`、
`deploy/docker/docker-compose.prod.yml` 一致。

> 各服务目录下的独立 `Dockerfile` 与 `deploy/docker/Dockerfile.micro` 为历史遗留、已被
> `Dockerfile.service` 取代，请勿使用。
