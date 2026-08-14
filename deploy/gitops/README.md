# OpsMesh GitOps 部署目录

本目录承载 OpsMesh 多网段运维平台在 Argo CD 上的 GitOps 声明式部署清单。
通过 Argo CD ApplicationSet 实现「一份 Helm Chart + N 份网段 overlay」的多集群/多网段自动同步，
所有网段配置以 Git 为唯一事实来源（Single Source of Truth），变更经 PR 审核后由 Argo CD 自动下发。

---

## 架构说明

整体数据流：**GitOps 仓库 → Argo CD → 多网段 K8s 集群**。

```text
┌──────────────────────────────────────────────────────────────────────┐
│  Git 仓库 (github.com/Levango7/OpsMesh)                              │
│  └─ deploy/                                                          │
│     ├─ helm/opsmesh/          # 共享 Helm Chart（控制面/agent/中间件）│
│     └─ gitops/                # 本目录：Argo CD 声明式部署清单        │
│        ├─ applicationset.yaml # ApplicationSet：list generator 驱动   │
│        ├─ projects/opsmesh.yaml # AppProject：源/目标/权限隔离       │
│        └─ segments/           # 各网段 Helm values overlay           │
└───────────────────────────┬──────────────────────────────────────────┘
                            │ git pull / webhook
                            ▼
┌──────────────────────────────────────────────────────────────────────┐
│  Argo CD (namespace: argocd)                                         │
│  ApplicationSet opsmesh-multi-segment                                │
│  ├─ Application opsmesh-example     → namespace opsmesh-example      │
│  └─ Application opsmesh-production  → namespace opsmesh-production   │
└───────────────────────────┬──────────────────────────────────────────┘
                            │ helm template + sync
                            ▼
┌──────────────────────────────────────────────────────────────────────┐
│  目标 Kubernetes 集群 (https://kubernetes.default.svc)               │
│  ├─ namespace opsmesh-example     (CIDR 10.0.1.0/24  开发/演示)       │
│  └─ namespace opsmesh-production  (CIDR 10.10.0.0/16 生产)            │
│     每个命名空间内：controlplane Deployment + agent DaemonSet         │
│                     + MySQL/Redis StatefulSet（按需）                │
└──────────────────────────────────────────────────────────────────────┘
```

要点：

- **单一 Chart，多网段 overlay**：所有网段共用 `deploy/helm/opsmesh/`，差异通过 `segments/<segment>.yaml` 覆盖。
- **list generator**：网段清单显式枚举在 `applicationset.yaml` 中，新增网段即新增一个元素 + 一份 values 文件。
- **自动同步**：`syncPolicy.automated` 开启 `prune` 与 `selfHeal`，Git 即期望状态，漂移自动收敛。
- **命名空间隔离**：每个网段对应独立 namespace（`opsmesh-<segment>`），由 `CreateNamespace=true` 自动创建。

---

## 目录结构说明

```text
deploy/gitops/
├─ README.md                          # 本说明文档
├─ applicationset.yaml                # Argo CD ApplicationSet：多网段应用集合
├─ projects/
│  └─ opsmesh.yaml                    # Argo CD AppProject：源仓库/目标/权限隔离
└─ segments/
   ├─ README.md                       # 网段配置说明
   ├─ example-segment.yaml            # 示例网段 overlay（开发/演示）
   └─ production-segment.yaml         # 生产网段 overlay（生产级配置）
```

| 文件 | 作用 | 是否需修改 |
|------|------|-----------|
| `applicationset.yaml` | 声明所有网段与对应 values 文件的映射 | 新增/删除网段时修改 |
| `projects/opsmesh.yaml` | 限定可同步的源仓库、目标 namespace、RBAC | 仓库迁移/权限调整时修改 |
| `segments/*.yaml` | 各网段的 Helm values overlay | 调整网段配置时修改 |

---

## 使用步骤

### 1. 配置网段 overlay

在 `segments/` 下新增 `<segment>-segment.yaml`，按 Helm Chart `values.yaml` 字段编写 overlay。
字段含义见 [`segments/README.md`](segments/README.md)。

### 2. 在 ApplicationSet 中登记网段

编辑 `applicationset.yaml`，在 `spec.generators[0].list.elements` 下追加一项：

```yaml
- segment: <网段名>
  cidr: <CIDR>
  namespace: opsmesh-<网段名>
  valuesFile: segments/<segment>-segment.yaml
```

### 3. 应用 AppProject 与 ApplicationSet

在已安装 Argo CD 的集群上执行一次（后续由 Git 驱动，无需重复）：

```bash
kubectl apply -f deploy/gitops/projects/opsmesh.yaml
kubectl apply -f deploy/gitops/applicationset.yaml
```

### 4. Argo CD 自动同步

ApplicationSet 控制器会为每个 element 渲染出一个 Application，Argo CD 拉取 Helm Chart + values overlay，
自动同步到对应 namespace。后续任何 overlay 变更只需提交 PR，合并后 Argo CD 自动收敛。

```bash
# 查看生成的 Application
kubectl get applications -n argocd
# 查看 ApplicationSet 状态
kubectl get applicationset opsmesh-multi-segment -n argocd
```

---

## 网段配置说明

每个网段由 `applicationset.yaml` 中的一个 list element 与 `segments/` 下的一份 values 文件共同描述：

- `segment`：网段标识，用于命名 Application（`opsmesh-<segment>`）与 namespace。
- `cidr`：网段 CIDR，仅作元数据标注（agent 实际网段由 `agent.segment` 传入 Helm）。
- `namespace`：目标命名空间，建议保持 `opsmesh-<segment>` 命名约定。
- `valuesFile`：相对仓库根的 overlay 文件路径，被 `helm.valueFiles` 引用。

详见 [`segments/README.md`](segments/README.md)。

---

## 与现有 Helm Chart 的关系

本目录**不重复**任何 Chart 模板，全部复用 `deploy/helm/opsmesh/`：

| 维度 | `deploy/helm/opsmesh/` | `deploy/gitops/segments/` |
|------|------------------------|---------------------------|
| 角色 | 共享 Helm Chart（模板 + 默认 values） | 各网段 values overlay |
| 内容 | `templates/*.yaml`、`values.yaml`、`Chart.yaml` | 仅覆盖差异字段 |
| 变更频率 | 随版本发布低频变更 | 随网段调优高频变更 |
| 渲染顺序 | — | `values.yaml` → `segments/<segment>.yaml`（后者覆盖前者） |

Argo CD 渲染某网段时，等价于：

```bash
helm template opsmesh deploy/helm/opsmesh \
  -f deploy/helm/opsmesh/values.yaml \
  -f deploy/gitops/segments/<segment>-segment.yaml \
  -n opsmesh-<segment>
```

因此 `segments/*.yaml` 中只需写**与默认值不同的字段**，未出现字段自动回退到 `values.yaml` 默认值。
字段名必须与 `deploy/helm/opsmesh/values.yaml` 严格一致（如 `controlplane.replicaCount`、`mysql.persistence.size`），
否则 overlay 不会生效。