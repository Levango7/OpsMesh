# OpsMesh 多云 + 云原生一体化运维平台 — 架构与产品设计方案

> 版本：v1.0 | 日期：2026-10-18 | 状态：草案（待执行）

## 1. 设计目标

将 OpsMesh 从单租户运维工具箱演进为**打通云厂商与云原生的一体化运维中台**，核心目标：

| 目标 | 说明 |
|------|------|
| 多云统一接入 | 支持阿里云/AWS/Azure/腾讯云/华为云等多种云厂商账号统一接入、资源发现、标签同步、成本归集 |
| 云原生原生支持 | K8s 多集群代理、CRD 管理、Helm 发布、GitOps（ArgoCD/Flux）、Service Mesh 流量治理 |
| 多租户隔离 | 租户→工作空间→云账号→集群→命名空间 五层授权模型，行级隔离 |
| 任务可靠性 | 优先级、重试策略、幂等性、死信队列、批量/金丝雀持久化 |
| 安全合规 | 高危操作审批、秘密管理、审计不可篡改、零信任网络 |

---

## 2. 架构设计

### 2.1 整体架构图（分层）

```
┌─────────────────────────────────────────────────────────────┐
│                    用户交付层 (Delivery)                      │
│  Vue3 前端 (Portal) | 移动端 App | CLI | Terraform Provider   │
└─────────────────────────────────────────────────────────────┘
                                │
┌─────────────────────────────────────────────────────────────┐
│                    API 网关层 (Gateway)                       │
│  认证鉴权 | 限流熔断 | 请求路由 | 租户注入 (X-Tenant-ID)         │
└─────────────────────────────────────────────────────────────┘
                                │
┌─────────────────────────────────────────────────────────────┐
│                    应用服务层 (App Services)                  │
│  任务中心 | 设备中心 | 云账号中心 | K8s 中心 | 秘密中心           │
│  审批中心 | 审计中心 | 告警中心 | 计费中心 | 仪表盘中心             │
└─────────────────────────────────────────────────────────────┘
                                │
┌─────────────────────────────────────────────────────────────┐
│                    域服务层 (Domain Services)                 │
│  多租户 RBAC | 云厂商适配器 | K8s 控制器 | 秘密加密 | 审计存储     │
└─────────────────────────────────────────────────────────────┘
                                │
┌─────────────────────────────────────────────────────────────┐
│                    基础设施层 (Infrastructure)                │
│  PostgreSQL/MySQL | Redis | S3/OSS 对象存储 | Prometheus      │
│  etcd (K8s) | Cloud Vendor APIs | Agent (边缘节点)               │
└─────────────────────────────────────────────────────────────┘
```

### 2.2 多租户模型（五层授权）

```
租户 (Tenant) → 工作空间 (Workspace) → 云账号 (CloudAccount) → 集群 (K8sCluster) → 命名空间 (Namespace)
```

- **租户**：最高级隔离单元，拥有独立配额（Quota）、计费、审计域
- **工作空间**：租户内的协作空间，成员与角色绑定
- **云账号**：接入的云厂商账号（AK/SK、OIDC、托管身份），关联租户
- **集群**：K8s 集群或云厂商托管集群，关联云账号
- **命名空间**：K8s Namespace 或云厂商资源组，细粒度权限控制

### 2.3 云厂商抽象层 (CloudProvider)

定义统一的 `CloudProvider` 接口，所有云厂商适配器实现：

```go
type CloudProvider interface {
    // 身份与连接
    ValidateCredentials(ctx context.Context, creds Credentials) error
    ListRegions(ctx context.Context) ([]Region, error)
    
    // 资源发现
    DiscoverVPCs(ctx context.Context, region string) ([]VPC, error)
    DiscoverSubnets(ctx context.Context, vpcID string) ([]Subnet, error)
    DiscoverInstances(ctx context.Context, region string) ([]Instance, error)
    DiscoverClusters(ctx context.Context, region string) ([]Cluster, error)
    
    // 标签与成本
    SyncTags(ctx context.Context, resourceID string) error
    FetchBilling(ctx context.Context, start, end time.Time) ([]BillingItem, error)
    
    // 操作
    CreateResource(ctx context.Context, req CreateResourceRequest) (Resource, error)
    DeleteResource(ctx context.Context, resourceID string) error
}
```

适配器模式：`AliyunProvider`, `AWSProvider`, `AzureProvider`, `TencentProvider`, `HuaweiProvider`

### 2.4 云原生深度集成

| 能力 | 技术选型 | 说明 |
|------|---------|------|
| K8s 多集群代理 | controller-runtime + kubeconfig 聚合 | 统一管理多集群，跨集群任务下发 |
| CRD 管理 | Kubebuilder | 自定义资源定义，声明式生命周期管理 |
| Helm 发布 | Helm SDK | Chart 仓库接入、版本管理、回滚 |
| GitOps | ArgoCD / Flux API | 声明式交付、同步状态、漂移检测 |
| Service Mesh | Istio / Linkerd 控制平面 | 流量治理、金丝雀、蓝绿、熔断 |

---

## 3. 数据模型设计

### 3.1 新增核心实体

#### CloudAccount (云账号)
```go
type CloudAccount struct {
    ID              string    `json:"id" gorm:"primaryKey"`
    TenantID        string    `json:"tenantId" gorm:"index"`
    WorkspaceID     string    `json:"workspaceId"`
    Name            string    `json:"name"`
    Provider        string    `json:"provider"` // aliyun/aws/azure/tencent/huawei
    Region          string    `json:"region"`
    AuthType        string    `json:"authType"` // aksk/oidc/managed_identity
    CredentialsRef  string    `json:"credentialsRef"` // 指向 SecretItem 的引用，不存明文
    Status          string    `json:"status"` // active/pending/suspended/disabled
    SyncStatus      string    `json:"syncStatus"` // syncing/synced/sync_failed
    LastSyncAt      time.Time `json:"lastSyncAt"`
    DiscoveredCount int       `json:"discoveredCount"`
    CreatedAt       time.Time `json:"createdAt"`
    UpdatedAt       time.Time `json:"updatedAt"`
    CreatedBy       string    `json:"createdBy"`
}
```

#### CloudResource (云资源)
```go
type CloudResource struct {
    ID             string    `json:"id" gorm:"primaryKey"`
    TenantID       string    `json:"tenantId" gorm:"index"`
    CloudAccountID string    `json:"cloudAccountId" gorm:"index"`
    Provider       string    `json:"provider"`
    Region         string    `json:"region"`
    ResourceType   string    `json:"resourceType"` // vpc/subnet/instance/cluster/oss/rds
    ResourceID     string    `json:"resourceId"` // 云厂商原生 ID
    ResourceName   string    `json:"resourceName"`
    Status         string    `json:"status"` // running/stopped/pending/error
    Tags           string    `json:"tags"` // JSON map
    PrivateIP      string    `json:"privateIp"`
    PublicIP       string    `json:"publicIp"`
    CreatedAt      time.Time `json:"createdAt"`
    UpdatedAt      time.Time `json:"updatedAt"`
}
```

#### K8sCluster (K8s 集群)
```go
type K8sCluster struct {
    ID             string    `json:"id" gorm:"primaryKey"`
    TenantID       string    `json:"tenantId" gorm:"index"`
    CloudAccountID string    `json:"cloudAccountId"`
    Name           string    `json:"name"`
    Endpoint       string    `json:"endpoint"`
    KubeconfigRef  string    `json:"kubeconfigRef"` // 指向 SecretItem
    Version        string    `json:"version"`
    NodeCount      int       `json:"nodeCount"`
    Status         string    `json:"status"` // healthy/unhealthy/unknown
    CreatedAt      time.Time `json:"createdAt"`
}
```

### 3.2 现有模型扩展

- **auth-svc User 表**：新增 `TenantID` 字段（默认 "default"）
- **Task 模型**：新增 `CloudAccountID`、`K8sClusterID` 字段，支持云原生任务下发
- **AuditEvent 模型**：新增 `ResourceType`、`ResourceID`、`CloudAccountID` 字段，增强云操作审计

---

## 4. API 设计

### 4.1 云账号管理 API

```
GET    /api/v1/cloud-accounts              # 列表（租户隔离，分页）
POST   /api/v1/cloud-accounts              # 创建
GET    /api/v1/cloud-accounts/{id}         # 详情
PUT    /api/v1/cloud-accounts/{id}         # 更新
DELETE /api/v1/cloud-accounts/{id}         # 删除
POST   /api/v1/cloud-accounts/{id}/sync    # 同步资源发现
POST   /api/v1/cloud-accounts/{id}/test    # 连接测试
GET    /api/v1/cloud-accounts/{id}/regions # 区域列表
GET    /api/v1/cloud-accounts/{id}/resources # 资源列表
```

### 4.2 云资源 API

```
GET    /api/v1/cloud-resources             # 列表（按 provider/region/类型过滤）
GET    /api/v1/cloud-resources/{id}        # 详情
POST   /api/v1/cloud-resources/{id}/action # 执行操作（start/stop/restart）
GET    /api/v1/cloud-resources/{id}/metrics # 监控指标
```

### 4.3 K8s 集群 API

```
GET    /api/v1/k8s-clusters                # 列表
POST   /api/v1/k8s-clusters                # 创建/接入
GET    /api/v1/k8s-clusters/{id}/namespaces # 命名空间列表
GET    /api/v1/k8s-clusters/{id}/resources  # K8s 资源（pod/deployment/svc）
POST   /api/v1/k8s-clusters/{id}/exec      # Pod 执行命令
GET    /api/v1/k8s-clusters/{id}/logs      # Pod 日志
```

### 4.4 Helm 发布 API

```
GET    /api/v1/helm-charts                 # Chart 列表
POST   /api/v1/helm-releases               # 创建发布
GET    /api/v1/helm-releases               # 发布列表
PUT    /api/v1/helm-releases/{id}          # 升级/回滚
DELETE /api/v1/helm-releases/{id}          # 卸载
```

---

## 5. 任务执行可靠性增强

### 5.1 任务状态机

```
PENDING → CLAIMED → RUNNING → SUCCESS
                          ↘ FAILED → RETRY (max 3次) → DEAD_LETTER
                          ↘ CANCELLED
```

字段：`priority` (0-10), `retry_count`, `max_retries`, `retry_delay`, `idempotency_key`, `dead_letter`, `batch_id`, `canary_phase`

### 5.2 幂等性

- 客户端生成 `idempotency_key`，服务端去重
- 窗口期 24h，超期自动清理

### 5.3 批量/金丝雀持久化

- 将 `batchStore` 从内存迁移到 PostgreSQL
- `BatchTask`、`BatchTaskItem`、`CanaryRelease`、`CanaryPhase` 持久化
- 支持重启后状态恢复

---

## 6. 安全与合规

### 6.1 高危操作审批流程

```
用户申请 → 审批流程（审批人/审批规则） → 审批通过 → 执行 → 审计记录
```

高危操作列表：iptables、userdel、umount、格式化磁盘、修改 root 密码、删除生产集群

### 6.2 秘密管理

- 云厂商 AK/SK、K8s kubeconfig、数据库密码、证书
- 加密存储（AES-256-GCM + 主密钥轮转）
- 访问审计（谁何时访问了哪个秘密）

### 6.3 零信任网络

- 服务间 mTLS
- API 网关认证与签名验证
- 租户间网络隔离

---

## 7. 任务拆分与执行计划

### Wave 1: 多租户基础（P0）
1. auth-svc User 表新增 TenantID 字段 + 迁移脚本
2. 租户上下文传播（X-Tenant-ID header → JWT claim → gRPC metadata）
3. 行级隔离修复（所有 agent-addressing 路径应用 tenantguard）
4. 配额执行开关默认开启 (cfg.QuotaEnabled = true)

### Wave 2: 云厂商抽象层（P0）
1. 定义 CloudProvider 接口 + 基础适配器框架
2. 云账号 CRUD API + 前端页面
3. 阿里云适配器（AK/SK + OIDC）
4. 资源发现与标签同步
5. 成本归集接口

### Wave 3: 云原生深度集成（P1）
1. K8s 多集群代理 + kubeconfig 聚合
2. CRD 管理 API
3. Helm 仓库接入与发布管理
4. ArgoCD/Flux GitOps 集成

### Wave 4: 任务执行可靠性（P1）
1. 任务状态机实现
2. 优先级、重试策略、幂等性
3. 批量/金丝雀状态持久化（PostgreSQL）
4. 死信队列

### Wave 5: 安全与秘密管理（P2）
1. 秘密管理 API（创建/读取/轮转/审计）
2. 高危操作审批工作流
3. 审计日志不可篡改（签名/区块链存根）
4. 零信任网络（mTLS 存根）

### Wave 6: 前端增强（P2）
1. Form、Tabs、Tooltip、Popover、Notification 组件
2. 修复 batch/canary 5 组未接线路由
3. 重构核心页面使用新组件库

### Wave 7: 验证（P0）
1. 后端编译 + 单元测试
2. 前端 lint + build + 测试
3. proto 同步 CI 断言
4. 端到端集成测试

---

## 8. 执行原则

1. **能力划分原则**：执行类（增删改）走 shell/模板，查询类走结构化 API，二进制/大文件传输与高风险需审批操作单独写能力
2. **proto 同步**：canonical proto 为单一事实源，CI 断言强制执行字段数/名/字节一致
3. **行级隔离**：所有资源访问必须通过 tenantguard 验证
4. **向后兼容**：默认租户 "default"，现有单租户部署无感升级
