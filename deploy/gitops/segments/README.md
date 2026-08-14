# 网段配置说明（segments/）

本目录存放各网段的 Helm values overlay 文件，每份文件对应 `applicationset.yaml` 中的一个 list element。
Argo CD 渲染某网段时，按 `values.yaml → segments/<segment>-segment.yaml` 顺序叠加，后者覆盖前者。

---

## 字段含义

每份 overlay 文件由两块组成：**网段元数据**（GitOps 自用，不传给 Helm）与 **Helm values 覆盖**。

### 1. 网段元数据（`segment` 块）

| 字段 | 说明 | 示例 |
|------|------|------|
| `segment.name` | 网段标识，与 ApplicationSet element 的 `segment` 一致 | `example` |
| `segment.cidr` | 网段 CIDR，仅作元数据标注，便于审计与拓扑可视化 | `10.0.1.0/24` |

> `segment` 块不会被 Helm Chart 消费（Chart 无此字段），仅用于 GitOps 侧的可读性与工具巡检。

### 2. Helm values 覆盖

字段名必须与 [`deploy/helm/opsmesh/values.yaml`](../../helm/opsmesh/values.yaml) 严格一致，
未出现的字段自动回退到默认值。常用覆盖项：

| 字段 | 说明 | 默认值 |
|------|------|--------|
| `controlplane.replicaCount` | 控制面副本数；>1 时需 `store=mysql` | `1` |
| `controlplane.store` | 持久化后端：`memory` \| `mysql` | `memory` |
| `controlplane.production` | 生产模式开关（强告警/审计） | `false` |
| `controlplane.requireAuth` | 要求网关注入 `X-Tenant-ID` | `false` |
| `controlplane.tls.enabled` | gRPC TLS/mTLS 开关 | `false` |
| `controlplane.cookieSecure` | Cookie Secure 标志（HTTPS 传输） | `false` |
| `controlplane.resources` | 控制面资源 requests/limits | 见 values.yaml |
| `controlplane.image.tag` | 镜像 tag；生产建议固定 sha | `latest` |
| `agent.enabled` | 是否部署 agent DaemonSet | `true` |
| `agent.segment` | agent 所属网段（由 ApplicationSet 参数透传） | `default` |
| `mysql.enabled` | 是否部署 MySQL | `true` |
| `mysql.persistence.size` | MySQL 持久化卷大小 | `10Gi` |
| `redis.enabled` | 是否部署 Redis | `true` |
| `redis.persistence.size` | Redis 持久化卷大小 | `5Gi` |
| `federation.enabled` | 联邦 peers 开关（跨网段协同） | `false` |

---

## 与 Helm Chart values.yaml 的 overlay 关系

overlay 是 **YAML 字段级覆盖**，遵循 Helm `-f` 多文件叠加语义：

1. **基线**：`deploy/helm/opsmesh/values.yaml` 提供所有字段默认值（开发/体验环境基线）。
2. **覆盖**：`segments/<segment>-segment.yaml` 仅写差异字段，深合并到基线之上。
3. **顺序**：`applicationset.yaml` 中 `valueFiles` 按数组顺序叠加，后覆盖前。

等价命令：

```bash
helm template opsmesh deploy/helm/opsmesh \
  -f deploy/helm/opsmesh/values.yaml \
  -f deploy/gitops/segments/example-segment.yaml \
  -n opsmesh-example
```

**深合并示例**：基线 `controlplane.resources.requests={cpu:100m, memory:128Mi}`，
overlay 写 `controlplane.resources.requests.cpu: 500m`，则最终 `memory` 仍为 `128Mi`（未覆盖字段保留）。

---

## 如何添加新网段

以新增 `staging` 网段（CIDR `10.20.0.0/16`）为例：

### 1. 创建 overlay 文件

新建 `segments/staging-segment.yaml`：

```yaml
segment:
  name: staging
  cidr: 10.20.0.0/16

controlplane:
  replicaCount: 2
  store: mysql
  tls:
    enabled: true
# ... 其余按需覆盖
```

### 2. 在 ApplicationSet 中登记

编辑 `applicationset.yaml`，在 `spec.generators[0].list.elements` 下追加：

```yaml
- segment: staging
  cidr: 10.20.0.0/16
  namespace: opsmesh-staging
  valuesFile: segments/staging-segment.yaml
```

### 3. 提交 PR

提交后由 Argo CD 自动同步，生成 Application `opsmesh-staging`，部署到 namespace `opsmesh-staging`。

> 命名约定：文件名 `<segment>-segment.yaml`，namespace `opsmesh-<segment>`，Application `opsmesh-<segment>`。
> 保持三者一致便于巡检与告警路由。

---

## 注意事项

- **生产网段**必须开启 `controlplane.tls.enabled`、`controlplane.requireAuth`、`controlplane.cookieSecure`，
  并将 `controlplane.image.tag` 固定到不可变 sha，避免 `latest` 漂移。
- **多副本**控制面（`replicaCount > 1`）必须设置 `controlplane.store=mysql`，否则 memory store 多副本数据分裂。
- **凭证类字段**（`jwtSecret`、`provisionSecret`、TLS 证书）不应写入本目录明文，
  建议通过 External Secrets / Sealed Secrets 注入，或经 `helm.parameters` 引用 K8s Secret。