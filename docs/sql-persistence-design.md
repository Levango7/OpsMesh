# SQL 持久化设计文档 — P1-P6 共 15 域

> 生成时间：2026-08-27
> 目标：将 15 个 StubNotImplemented 零值桩替换为真实 MySQL CRUD
> 项目：OpsMesh `internal/store/sql_<domain>.go`
> 参考实现：`sql_k8s.go` / `sql_secret.go`（P0.3 已建立的生产就绪模式）

## 设计原则

1. **遵循 P0.3 已建立的模式**（`sql_k8s.go` / `sql_secret.go` 风格）
2. **使用 `s.db.ExecContext/QueryRowContext/QueryContext` + `context.Background()`**
3. **SQL 错误时 `log.Printf` + 返回零值**（接口无 error 返回值；`SaveK8sCluster` 例外上抛 error）
4. **`sql.ErrNoRows` 视为"不存在"**，返回 `nil/false`
5. **时间用 `time.Now().UTC()`**（与 `sql_k8s.go` 一致；memory 实现用 `time.Now()`，SQL 层统一 UTC）
6. **JSON 列用 `encoding/json` 序列化/反序列化**，存 `TEXT`；空值存空串 `""`，读取时空串反序列化为零值
7. **租户隔离：所有查询带 `tenant_id` 条件**（`tenantID != ""` 时校验归属）
8. **文件头部无 BOM**，`package store` 紧跟文件头注释之后

## 通用实现模式

### ID 生成

每个域提供 `rand<Domain>ID()` 函数（`crypto/rand` 16 字节 hex，前缀见各域）。熵源失败回退 `time.Now().UnixNano()`。与 memory 实现共用同名函数（memory 已定义，SQL 层直接调用）。

### 租户归一

`tenantID == ""` 时归一为 `"default"`（与 `sql_k8s.go` SaveK8sCluster 一致）。例外：`tenant` / `plugin` / `billing_plans` 域无租户参数（按 ID 全局唯一）。

### 时间填充

- `CreatedAt.IsZero()` 时填 `now := time.Now().UTC()`
- `UpdatedAt` 始终刷新为 `now`
- `Update*` 保留原 `CreatedAt` / `TenantID` 不可改（防越权改归属）

### scan helper

每个域提供 `scan<Domain>(row rowScanner) *<Domain>`，从一行扫描出对象；JSON 列先扫到 `string` 再 `json.Unmarshal`。`rowScanner` 接口已由 `sql_k8s.go` 建立（`*sql.Row` / `*sql.Rows` 均满足）。

### 错误处理

- `QueryRowContext` 后 `err == sql.ErrNoRows` → 返回 `(nil, false)`
- 其他 `err != nil` → `log.Printf("[store] <Method> 失败 ...: %v", err)` + 返回零值
- `ExecContext` 失败 → `log.Printf` + 返回零值（`RowsAffected` 判断删除是否生效）

### Upsert 策略

- `Create*`（按 ID 幂等）：`INSERT ... ON DUPLICATE KEY UPDATE`，`tenant_id` 仅插入不更新（防 upsert 改写归属），与 `sql_k8s.go` SaveK8sCluster 一致
- `Update*`：先 `SELECT` 校验存在 + 租户归属，再 `UPDATE ... WHERE id=? AND tenant_id=?`；不存在返回 `(nil, false)`
- `SaveReport` / `CreateBackup` 等命名含 Save 的方法走 Upsert

### JSON 列读写

```go
// 写入
b, _ := json.Marshal(obj.SLIs)
slisJSON := string(b)
// 读取
var slisJSON string
row.Scan(..., &slisJSON, ...)
var slis []SLI
if slisJSON != "" { json.Unmarshal([]byte(slisJSON), &slis) }
```

### 排序

- `ListSLOs` / `ListK8sClusters` / `ListBillingPlans` / `ListTenants` / `ListPlugins`：按 `created_at` **升序**（ASC）
- 其余 List：按 `created_at` **降序**（DESC，最新优先）
- `ListScriptExecutions` / `ListWebhookDeliveries`：按业务时间（`started_at` / `delivered_at`）降序
- SQL 层用 `ORDER BY` 子句完成排序，无需内存排序

---

## 域 1: SLO（P1）

### 数据模型

```go
type SLO struct {
    ID          string    `json:"id"`
    TenantID    string    `json:"tenantID"`
    Name        string    `json:"name"`
    Description string    `json:"description"`
    ServiceName string    `json:"serviceName"`
    Target      float64   `json:"target"`       // 如 99.9 表示 99.9%
    Window      string    `json:"window"`       // 如 "30d", "7d"
    SLIs        []SLI     `json:"slis"`         // JSON 列
    CreatedAt   time.Time `json:"createdAt"`
    UpdatedAt   time.Time `json:"updatedAt"`
}

type SLI struct {
    Name     string  `json:"name"`
    Metric   string  `json:"metric"`
    Target   float64 `json:"target"`
    Operator string  `json:"operator"`
}

type SLIStatus struct {
    SLIName       string    `json:"sliName"`
    CurrentValue  float64   `json:"currentValue"`
    TargetValue   float64   `json:"targetValue"`
    Status        string    `json:"status"` // "met" | "breached" | "nodata"
    LastEvaluated time.Time `json:"lastEvaluated"`
}
```

### 接口方法

```go
CreateSLO(tenantID string, slo *SLO) *SLO
GetSLO(tenantID, id string) (*SLO, bool)
UpdateSLO(tenantID string, slo *SLO) (*SLO, bool)
ListSLOs(tenantID string) []*SLO
DeleteSLO(tenantID, id string) bool
SLIStatus(tenantID, id string) []*SLIStatus
```

### 表结构

```sql
CREATE TABLE IF NOT EXISTS slos (
    id           VARCHAR(64)  NOT NULL,
    tenant_id    VARCHAR(64)  NOT NULL DEFAULT 'default',
    name         VARCHAR(255) NOT NULL,
    description  TEXT,
    service_name VARCHAR(255),
    target       DOUBLE       NOT NULL DEFAULT 0,
    window       VARCHAR(32),
    slis         TEXT,                    -- JSON: []SLI
    created_at   DATETIME     NOT NULL,
    updated_at   DATETIME     NOT NULL,
    PRIMARY KEY (id),
    KEY idx_slos_tenant (tenant_id),
    KEY idx_slos_service (tenant_id, service_name)
);
```

### SQL 查询模式

| 方法 | SQL |
|------|-----|
| CreateSLO | `INSERT INTO slos (id,tenant_id,name,description,service_name,target,window,slis,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE name=VALUES(name),description=VALUES(description),service_name=VALUES(service_name),target=VALUES(target),window=VALUES(window),slis=VALUES(slis),updated_at=VALUES(updated_at)` |
| GetSLO | `SELECT id,tenant_id,name,description,service_name,target,window,slis,created_at,updated_at FROM slos WHERE id=? AND tenant_id=?` |
| UpdateSLO | `UPDATE slos SET name=?,description=?,service_name=?,target=?,window=?,slis=?,updated_at=? WHERE id=? AND tenant_id=?` |
| ListSLOs | `SELECT ... FROM slos WHERE tenant_id=? ORDER BY created_at ASC` |
| DeleteSLO | `DELETE FROM slos WHERE id=? AND tenant_id=?` |
| SLIStatus | 复用 GetSLO 取 SLO，对每个 SLI 返回模拟状态（CurrentValue=99.5, Status="met", LastEvaluated=now），不查 DB |

### 实现要点

- **JSON 列 `slis`**：`[]SLI` 序列化为 JSON 数组；空切片存 `""`，读取时空串跳过 Unmarshal
- **ID 生成**：复用 `randSLOID()`（memory_slo.go 已定义，前缀 `slo-`）
- **CreateSLO 默认值**：`TenantID` 空归一 `default`；`CreatedAt` 零值填 now；`UpdatedAt` 始终刷新
- **UpdateSLO**：先 `GetSLO` 校验存在 + 租户归属，不存在返回 `(nil, false)`；保留原 `CreatedAt`/`TenantID`
- **SLIStatus**：MVP 返回模拟状态（与 memory 一致），不接入 Prometheus；SLO 不存在返回 nil
- **ListSLOs 升序**：`ORDER BY created_at ASC`（与 ListK8sClusters 一致）

---

## 域 2: Ticket（P1）

### 数据模型

```go
type Ticket struct {
    ID            string     `json:"id"`
    TenantID      string     `json:"tenantID"`
    Title         string     `json:"title"`
    Description   string     `json:"description"`
    Status        string     `json:"status"`         // open|in_progress|resolved|closed
    Priority      string     `json:"priority"`       // low|medium|high|urgent
    Category      string     `json:"category"`       // incident|change|request|problem
    AssigneeID    string     `json:"assigneeID"`
    CreatorID     string     `json:"creatorID"`
    RelatedDevice string     `json:"relatedDevice"`
    RelatedTask   string     `json:"relatedTask"`
    Tags          []string   `json:"tags"`           // JSON 列
    CreatedAt     time.Time  `json:"createdAt"`
    UpdatedAt     time.Time  `json:"updatedAt"`
    ResolvedAt    *time.Time `json:"resolvedAt,omitempty"` // 可空
}

type TicketFilter struct {
    Status     string
    Priority   string
    Category   string
    AssigneeID string
}
```

### 接口方法

```go
CreateTicket(tenantID string, t *Ticket) *Ticket
GetTicket(tenantID, id string) (*Ticket, bool)
UpdateTicket(tenantID string, t *Ticket) (*Ticket, bool)
ListTickets(tenantID string, filter TicketFilter) []*Ticket
CloseTicket(tenantID, id string) (*Ticket, bool)
```

### 表结构

```sql
CREATE TABLE IF NOT EXISTS tickets (
    id             VARCHAR(64)  NOT NULL,
    tenant_id      VARCHAR(64)  NOT NULL DEFAULT 'default',
    title          VARCHAR(255) NOT NULL,
    description    TEXT,
    status         VARCHAR(32)  NOT NULL DEFAULT 'open',
    priority       VARCHAR(32)  NOT NULL DEFAULT 'medium',
    category       VARCHAR(32)  NOT NULL DEFAULT 'incident',
    assignee_id    VARCHAR(64),
    creator_id     VARCHAR(64),
    related_device VARCHAR(64),
    related_task   VARCHAR(64),
    tags           TEXT,                    -- JSON: []string
    created_at     DATETIME     NOT NULL,
    updated_at     DATETIME     NOT NULL,
    resolved_at    DATETIME,                -- NULL 表示未解决
    PRIMARY KEY (id),
    KEY idx_tickets_tenant (tenant_id),
    KEY idx_tickets_status (tenant_id, status),
    KEY idx_tickets_priority (tenant_id, priority),
    KEY idx_tickets_assignee (tenant_id, assignee_id),
    KEY idx_tickets_created (tenant_id, created_at)
);
```

### SQL 查询模式

| 方法 | SQL |
|------|-----|
| CreateTicket | `INSERT INTO tickets (...) VALUES (...) ON DUPLICATE KEY UPDATE title=VALUES(title),description=VALUES(description),status=VALUES(status),priority=VALUES(priority),category=VALUES(category),assignee_id=VALUES(assignee_id),creator_id=VALUES(creator_id),related_device=VALUES(related_device),related_task=VALUES(related_task),tags=VALUES(tags),updated_at=VALUES(updated_at),resolved_at=VALUES(resolved_at)` |
| GetTicket | `SELECT ... FROM tickets WHERE id=? AND tenant_id=?` |
| UpdateTicket | `UPDATE tickets SET title=?,description=?,status=?,priority=?,category=?,assignee_id=?,creator_id=?,related_device=?,related_task=?,tags=?,updated_at=?,resolved_at=? WHERE id=? AND tenant_id=?` |
| ListTickets | `SELECT ... FROM tickets WHERE tenant_id=? [AND status=?] [AND priority=?] [AND category=?] [AND assignee_id=?] ORDER BY created_at DESC` |
| CloseTicket | `UPDATE tickets SET status='closed', resolved_at=?, updated_at=? WHERE id=? AND tenant_id=?` |

### 实现要点

- **JSON 列 `tags`**：`[]string` 序列化为 JSON 数组
- **可空 `resolved_at`**：写入用 `sql.NullTime` 或指针；读取扫到 `*time.Time`，NULL → nil
- **CreateTicket 默认值**：`Status` 空 → `open`；`Priority` 空 → `medium`；`Category` 空 → `incident`（与 memory 一致）
- **ListTickets 动态过滤**：根据 `filter` 非空字段动态拼接 `WHERE` 子句 + `args`（与 sql_k8s.go ListK8sClusters 动态拼接风格一致）
- **CloseTicket**：置 `status='closed'` + `resolved_at=now` + `updated_at=now`；先 SELECT 校验存在 + 租户归属，不存在返回 `(nil, false)`；返回更新后的 Ticket
- **ID 生成**：复用 `randTicketID()`（前缀 `ticket-`）
- **ListTickets 降序**：`ORDER BY created_at DESC`（最新优先）

---

## 域 3: ArgoCD（P2）

### 数据模型

```go
type ArgoCDApp struct {
    ID             string    `json:"id"`
    TenantID       string    `json:"tenantID"`
    Name           string    `json:"name"`
    Namespace      string    `json:"namespace"`
    RepoURL        string    `json:"repoURL"`
    Path           string    `json:"path"`
    TargetRevision string    `json:"targetRevision"`
    ClusterURL     string    `json:"clusterURL"`
    SyncPolicy     string    `json:"syncPolicy"`     // manual|auto
    Status         string    `json:"status"`         // synced|outofsync|unknown
    HealthStatus   string    `json:"healthStatus"`   // healthy|degraded|missing|unknown
    CreatedAt      time.Time `json:"createdAt"`
    UpdatedAt      time.Time `json:"updatedAt"`
}
```

### 接口方法

```go
CreateApp(tenantID string, a *ArgoCDApp) *ArgoCDApp
GetApp(tenantID, id string) (*ArgoCDApp, bool)
UpdateApp(tenantID string, a *ArgoCDApp) (*ArgoCDApp, bool)
ListApps(tenantID string) []*ArgoCDApp
DeleteApp(tenantID, id string) bool
SyncApp(tenantID, id string) (*ArgoCDApp, bool)
```

### 表结构

```sql
CREATE TABLE IF NOT EXISTS argocd_apps (
    id             VARCHAR(64)  NOT NULL,
    tenant_id      VARCHAR(64)  NOT NULL DEFAULT 'default',
    name           VARCHAR(255) NOT NULL,
    namespace      VARCHAR(255),
    repo_url       TEXT,
    path           VARCHAR(512),
    target_revision VARCHAR(64),
    cluster_url    TEXT,
    sync_policy    VARCHAR(32)  NOT NULL DEFAULT 'manual',
    status         VARCHAR(32)  NOT NULL DEFAULT 'unknown',
    health_status  VARCHAR(32)  NOT NULL DEFAULT 'unknown',
    created_at     DATETIME     NOT NULL,
    updated_at     DATETIME     NOT NULL,
    PRIMARY KEY (id),
    KEY idx_argocd_tenant (tenant_id),
    KEY idx_argocd_name (tenant_id, name)
);
```

### SQL 查询模式

| 方法 | SQL |
|------|-----|
| CreateApp | `INSERT INTO argocd_apps (...) VALUES (...) ON DUPLICATE KEY UPDATE name=VALUES(name),namespace=VALUES(namespace),repo_url=VALUES(repo_url),path=VALUES(path),target_revision=VALUES(target_revision),cluster_url=VALUES(cluster_url),sync_policy=VALUES(sync_policy),status=VALUES(status),health_status=VALUES(health_status),updated_at=VALUES(updated_at)` |
| GetApp | `SELECT ... FROM argocd_apps WHERE id=? AND tenant_id=?` |
| UpdateApp | `UPDATE argocd_apps SET name=?,namespace=?,repo_url=?,path=?,target_revision=?,cluster_url=?,sync_policy=?,status=?,health_status=?,updated_at=? WHERE id=? AND tenant_id=?` |
| ListApps | `SELECT ... FROM argocd_apps WHERE tenant_id=? ORDER BY created_at DESC` |
| DeleteApp | `DELETE FROM argocd_apps WHERE id=? AND tenant_id=?` |
| SyncApp | `UPDATE argocd_apps SET status='synced', health_status='healthy', updated_at=? WHERE id=? AND tenant_id=?`（返回更新后的 App） |

### 实现要点

- **纯标量字段**，无 JSON 列
- **SyncApp**：置 `status='synced'` + `health_status='healthy'` + `updated_at=now`（模拟同步成功）；先 SELECT 校验存在 + 租户归属，不存在返回 `(nil, false)`；返回更新后的 App
- **ID 生成**：复用 memory 已有 `randArgoCDID()`（前缀 `argocd-`，定义在 memory_argocd.go）
- **CreateApp 默认值**：`SyncPolicy` 空 → `manual`；`Status` 空 → `unknown`；`HealthStatus` 空 → `unknown`
- **ListApps 降序**

---

## 域 4: Pipeline（P2）

### 数据模型

```go
type PipelineTemplate struct {
    ID          string          `json:"id"`
    TenantID    string          `json:"tenantID"`
    Name        string          `json:"name"`
    Description string          `json:"description"`
    Type        string          `json:"type"`       // tekton|jenkins
    YAML        string          `json:"yaml"`
    Parameters  []PipelineParam `json:"parameters"` // JSON 列
    CreatedAt   time.Time       `json:"createdAt"`
    UpdatedAt   time.Time       `json:"updatedAt"`
}

type PipelineParam struct {
    Name        string `json:"name"`
    Description string `json:"description"`
    Default     string `json:"default"`
    Required    bool   `json:"required"`
}

type PipelineRun struct {
    ID           string            `json:"id"`
    TenantID     string            `json:"tenantID"`
    TemplateID   string            `json:"templateID"`
    TemplateName string            `json:"templateName"`
    Status       string            `json:"status"`     // pending|running|succeeded|failed|cancelled
    Parameters   map[string]string `json:"parameters"` // JSON 列
    Logs         string            `json:"logs"`
    StartedAt    *time.Time        `json:"startedAt,omitempty"`   // 可空
    FinishedAt   *time.Time        `json:"finishedAt,omitempty"`  // 可空
    CreatedAt    time.Time         `json:"createdAt"`
}
```

### 接口方法

```go
CreateTemplate(tenantID string, t *PipelineTemplate) *PipelineTemplate
GetTemplate(tenantID, id string) (*PipelineTemplate, bool)
ListTemplates(tenantID string) []*PipelineTemplate
DeleteTemplate(tenantID, id string) bool
CreateRun(tenantID string, r *PipelineRun) *PipelineRun
GetRun(tenantID, id string) (*PipelineRun, bool)
ListRuns(tenantID string, templateID string) []*PipelineRun
UpdateRun(tenantID string, r *PipelineRun) (*PipelineRun, bool)
```

### 表结构

```sql
CREATE TABLE IF NOT EXISTS pipeline_templates (
    id          VARCHAR(64)  NOT NULL,
    tenant_id   VARCHAR(64)  NOT NULL DEFAULT 'default',
    name        VARCHAR(255) NOT NULL,
    description TEXT,
    type        VARCHAR(32)  NOT NULL DEFAULT 'tekton',
    yaml        TEXT,
    parameters  TEXT,                    -- JSON: []PipelineParam
    created_at  DATETIME     NOT NULL,
    updated_at  DATETIME     NOT NULL,
    PRIMARY KEY (id),
    KEY idx_pipeline_tpl_tenant (tenant_id)
);

CREATE TABLE IF NOT EXISTS pipeline_runs (
    id            VARCHAR(64)  NOT NULL,
    tenant_id     VARCHAR(64)  NOT NULL DEFAULT 'default',
    template_id   VARCHAR(64)  NOT NULL,
    template_name VARCHAR(255),
    status        VARCHAR(32)  NOT NULL DEFAULT 'pending',
    parameters    TEXT,                    -- JSON: map[string]string
    logs          LONGTEXT,
    started_at    DATETIME,                -- NULL 表示未启动
    finished_at   DATETIME,                -- NULL 表示未结束
    created_at    DATETIME     NOT NULL,
    PRIMARY KEY (id),
    KEY idx_pipeline_run_tenant (tenant_id),
    KEY idx_pipeline_run_template (tenant_id, template_id),
    KEY idx_pipeline_run_status (tenant_id, status)
);
```

### SQL 查询模式

| 方法 | SQL |
|------|-----|
| CreateTemplate | `INSERT INTO pipeline_templates (...) VALUES (...) ON DUPLICATE KEY UPDATE name=VALUES(name),description=VALUES(description),type=VALUES(type),yaml=VALUES(yaml),parameters=VALUES(parameters),updated_at=VALUES(updated_at)` |
| GetTemplate | `SELECT ... FROM pipeline_templates WHERE id=? AND tenant_id=?` |
| ListTemplates | `SELECT ... FROM pipeline_templates WHERE tenant_id=? ORDER BY created_at DESC` |
| DeleteTemplate | `DELETE FROM pipeline_templates WHERE id=? AND tenant_id=?` |
| CreateRun | `INSERT INTO pipeline_runs (...) VALUES (...) ON DUPLICATE KEY UPDATE template_id=VALUES(template_id),template_name=VALUES(template_name),status=VALUES(status),parameters=VALUES(parameters),logs=VALUES(logs),started_at=VALUES(started_at),finished_at=VALUES(finished_at)` |
| GetRun | `SELECT ... FROM pipeline_runs WHERE id=? AND tenant_id=?` |
| ListRuns | `SELECT ... FROM pipeline_runs WHERE tenant_id=? AND template_id=? ORDER BY created_at DESC`（templateID 空时省略该条件，返回全部） |
| UpdateRun | `UPDATE pipeline_runs SET template_id=?,template_name=?,status=?,parameters=?,logs=?,started_at=?,finished_at=? WHERE id=? AND tenant_id=?` |

### 实现要点

- **两张表**：`pipeline_templates`（模板）+ `pipeline_runs`（运行记录）
- **JSON 列**：`pipeline_templates.parameters`（`[]PipelineParam`）；`pipeline_runs.parameters`（`map[string]string`）
- **可空时间**：`pipeline_runs.started_at` / `finished_at` 用 `sql.NullTime` 或指针处理
- **ListRuns**：`templateID != ""` 时加 `AND template_id=?`；空时返回该租户全部 Run
- **ID 生成**：复用 memory 已有 `randPipelineID()`（前缀 `pipeline-`）+ `randRunID()`（前缀 `run-`）
- **CreateRun 默认值**：`Status` 空 → `pending`；`CreatedAt` 零值填 now
- **UpdateRun**：先 SELECT 校验存在 + 租户归属，不存在返回 `(nil, false)`

---

## 域 5: Traffic（P2）

### 数据模型

```go
type TrafficPolicy struct {
    ID            string         `json:"id"`
    TenantID      string         `json:"tenantID"`
    Name          string         `json:"name"`
    ServiceName   string         `json:"serviceName"`
    Type          string         `json:"type"`          // canary|timeout|retry|circuit_breaker|mirror
    CanaryWeights map[string]int `json:"canaryWeights"` // JSON 列
    MirrorPercent int            `json:"mirrorPercent"`
    Timeout       string         `json:"timeout"`
    Retries       int            `json:"retries"`
    RetryTimeout  string         `json:"retryTimeout"`
    MaxConns      int            `json:"maxConns"`
    MaxRequests   int            `json:"maxRequests"`
    Status        string         `json:"status"`        // active|inactive
    CreatedAt     time.Time      `json:"createdAt"`
    UpdatedAt     time.Time      `json:"updatedAt"`
}
```

### 接口方法

```go
CreatePolicy(tenantID string, p *TrafficPolicy) *TrafficPolicy
GetPolicy(tenantID, id string) (*TrafficPolicy, bool)
UpdatePolicy(tenantID string, p *TrafficPolicy) (*TrafficPolicy, bool)
ListPolicies(tenantID string) []*TrafficPolicy
DeletePolicy(tenantID, id string) bool
EnablePolicy(tenantID, id string) (*TrafficPolicy, bool)
DisablePolicy(tenantID, id string) (*TrafficPolicy, bool)
```

### 表结构

```sql
CREATE TABLE IF NOT EXISTS traffic_policies (
    id             VARCHAR(64)  NOT NULL,
    tenant_id      VARCHAR(64)  NOT NULL DEFAULT 'default',
    name           VARCHAR(255) NOT NULL,
    service_name   VARCHAR(255),
    type           VARCHAR(32)  NOT NULL,
    canary_weights TEXT,                    -- JSON: map[string]int
    mirror_percent INT          NOT NULL DEFAULT 0,
    timeout        VARCHAR(32),
    retries        INT          NOT NULL DEFAULT 0,
    retry_timeout  VARCHAR(32),
    max_conns      INT          NOT NULL DEFAULT 0,
    max_requests   INT          NOT NULL DEFAULT 0,
    status         VARCHAR(32)  NOT NULL DEFAULT 'inactive',
    created_at     DATETIME     NOT NULL,
    updated_at     DATETIME     NOT NULL,
    PRIMARY KEY (id),
    KEY idx_traffic_tenant (tenant_id),
    KEY idx_traffic_service (tenant_id, service_name),
    KEY idx_traffic_status (tenant_id, status)
);
```

### SQL 查询模式

| 方法 | SQL |
|------|-----|
| CreatePolicy | `INSERT INTO traffic_policies (...) VALUES (...) ON DUPLICATE KEY UPDATE name=VALUES(name),service_name=VALUES(service_name),type=VALUES(type),canary_weights=VALUES(canary_weights),mirror_percent=VALUES(mirror_percent),timeout=VALUES(timeout),retries=VALUES(retries),retry_timeout=VALUES(retry_timeout),max_conns=VALUES(max_conns),max_requests=VALUES(max_requests),status=VALUES(status),updated_at=VALUES(updated_at)` |
| GetPolicy | `SELECT ... FROM traffic_policies WHERE id=? AND tenant_id=?` |
| UpdatePolicy | `UPDATE traffic_policies SET name=?,service_name=?,type=?,canary_weights=?,mirror_percent=?,timeout=?,retries=?,retry_timeout=?,max_conns=?,max_requests=?,status=?,updated_at=? WHERE id=? AND tenant_id=?` |
| ListPolicies | `SELECT ... FROM traffic_policies WHERE tenant_id=? ORDER BY created_at DESC` |
| DeletePolicy | `DELETE FROM traffic_policies WHERE id=? AND tenant_id=?` |
| EnablePolicy | `UPDATE traffic_policies SET status='active', updated_at=? WHERE id=? AND tenant_id=?` |
| DisablePolicy | `UPDATE traffic_policies SET status='inactive', updated_at=? WHERE id=? AND tenant_id=?` |

### 实现要点

- **JSON 列 `canary_weights`**：`map[string]int` 序列化为 JSON 对象
- **EnablePolicy / DisablePolicy**：置 `status='active'` / `'inactive'` + `updated_at=now`；先 SELECT 校验存在 + 租户归属，不存在返回 `(nil, false)`；返回更新后的 Policy
- **ID 生成**：复用 `randTrafficID()`（前缀 `traffic-`）
- **CreatePolicy 默认值**：`Status` 空 → `inactive`（与 memory 一致）
- **ListPolicies 降序**

---

## 域 6: Backup（P3）

### 数据模型

```go
type BackupRecord struct {
    ID        string    `json:"id"`
    TenantID  string    `json:"tenantID"`
    Type      string    `json:"type"`    // full|config|devices|tasks
    Status    string    `json:"status"`  // creating|completed|failed
    Size      int64     `json:"size"`
    Path      string    `json:"path"`
    CreatedAt time.Time `json:"createdAt"`
}
```

### 接口方法

```go
CreateBackup(tenantID string, b *BackupRecord) *BackupRecord
GetBackup(tenantID, id string) (*BackupRecord, bool)
ListBackups(tenantID string) []*BackupRecord
DeleteBackup(tenantID, id string) bool
```

### 表结构

```sql
CREATE TABLE IF NOT EXISTS backup_records (
    id         VARCHAR(64)  NOT NULL,
    tenant_id  VARCHAR(64)  NOT NULL DEFAULT 'default',
    type       VARCHAR(32)  NOT NULL DEFAULT 'full',
    status     VARCHAR(32)  NOT NULL DEFAULT 'creating',
    size       BIGINT       NOT NULL DEFAULT 0,
    path       TEXT,
    created_at DATETIME     NOT NULL,
    PRIMARY KEY (id),
    KEY idx_backup_tenant (tenant_id),
    KEY idx_backup_created (tenant_id, created_at)
);
```

### SQL 查询模式

| 方法 | SQL |
|------|-----|
| CreateBackup | `INSERT INTO backup_records (...) VALUES (...) ON DUPLICATE KEY UPDATE type=VALUES(type),status=VALUES(status),size=VALUES(size),path=VALUES(path)` |
| GetBackup | `SELECT ... FROM backup_records WHERE id=? AND tenant_id=?` |
| ListBackups | `SELECT ... FROM backup_records WHERE tenant_id=? ORDER BY created_at DESC` |
| DeleteBackup | `DELETE FROM backup_records WHERE id=? AND tenant_id=?` |

### 实现要点

- **纯标量字段**，无 JSON 列；无 `updated_at`（只有 `created_at`）
- **CreateBackup**：`SaveReport` 语义（Upsert）；`ID` 空分配；`CreatedAt` 零值填 now；`Type` 空 → `full`；`Status` 空 → `creating`
- **ID 生成**：复用 memory 已有 `randBackupID()`（前缀 `backup-`）
- **ListBackups 降序**

---

## 域 7: Compliance（P3）

### 数据模型

```go
type ComplianceReport struct {
    ID        string             `json:"id"`
    TenantID  string             `json:"tenantID"`
    DeviceID  string             `json:"deviceID"`
    Results   []ComplianceResult `json:"results"` // JSON 列
    Score     int                `json:"score"`
    CreatedAt time.Time          `json:"createdAt"`
}

type ComplianceResult struct {
    RuleID    string    `json:"ruleId"`
    Passed    bool      `json:"passed"`
    Output    string    `json:"output"`
    CheckedAt time.Time `json:"checkedAt"`
}
```

### 接口方法

```go
SaveReport(tenantID string, r *ComplianceReport) *ComplianceReport
GetReport(tenantID, id string) (*ComplianceReport, bool)
ListReports(tenantID string) []*ComplianceReport
DeleteReport(tenantID, id string) bool
```

### 表结构

```sql
CREATE TABLE IF NOT EXISTS compliance_reports (
    id         VARCHAR(64)  NOT NULL,
    tenant_id  VARCHAR(64)  NOT NULL DEFAULT 'default',
    device_id  VARCHAR(64),
    results    TEXT,                    -- JSON: []ComplianceResult
    score      INT          NOT NULL DEFAULT 0,
    created_at DATETIME     NOT NULL,
    PRIMARY KEY (id),
    KEY idx_compliance_tenant (tenant_id),
    KEY idx_compliance_device (tenant_id, device_id),
    KEY idx_compliance_created (tenant_id, created_at)
);
```

### SQL 查询模式

| 方法 | SQL |
|------|-----|
| SaveReport | `INSERT INTO compliance_reports (...) VALUES (...) ON DUPLICATE KEY UPDATE device_id=VALUES(device_id),results=VALUES(results),score=VALUES(score)` |
| GetReport | `SELECT ... FROM compliance_reports WHERE id=? AND tenant_id=?` |
| ListReports | `SELECT ... FROM compliance_reports WHERE tenant_id=? ORDER BY created_at DESC` |
| DeleteReport | `DELETE FROM compliance_reports WHERE id=? AND tenant_id=?` |

### 实现要点

- **JSON 列 `results`**：`[]ComplianceResult` 序列化为 JSON 数组（含嵌套 `CheckedAt time.Time`）
- **SaveReport**：Upsert 语义；`ID` 空分配；`CreatedAt` 零值填 now
- **ID 生成**：复用 memory 已有 `randComplianceID()`（前缀 `compliance-`）
- **无 `updated_at`**（只有 `created_at`，报告不可改，重新扫描生成新 ID）
- **ListReports 降序**

---

## 域 8: Automation（P4）

### 数据模型

```go
type AutomationRule struct {
    ID            string             `json:"id"`
    TenantID      string             `json:"tenantID"`
    Name          string             `json:"name"`
    Description   string             `json:"description"`
    TriggerType   string             `json:"triggerType"`   // alert|metric_threshold|schedule|event
    TriggerParams map[string]string  `json:"triggerParams"` // JSON 列
    Actions       []AutomationAction `json:"actions"`       // JSON 列
    Enabled       bool               `json:"enabled"`
    CreatedAt     time.Time          `json:"createdAt"`
    UpdatedAt     time.Time          `json:"updatedAt"`
}

type AutomationAction struct {
    Type   string            `json:"type"`   // execute_task|send_notify|scale|restart|isolate
    Params map[string]string `json:"params"`
}

type AutomationExecution struct {
    ID        string     `json:"id"`
    TenantID  string     `json:"tenantID"`
    RuleID    string     `json:"ruleID"`
    RuleName  string     `json:"ruleName"`
    Status    string     `json:"status"`   // pending|running|succeeded|failed|skipped
    Detail    string     `json:"detail"`
    StartedAt time.Time  `json:"startedAt"`
    EndedAt   *time.Time `json:"endedAt,omitempty"` // 可空
}
```

### 接口方法

```go
CreateAutomationRule(tenantID string, r *AutomationRule) *AutomationRule
GetAutomationRule(tenantID, id string) (*AutomationRule, bool)
ListAutomationRules(tenantID string) []*AutomationRule
UpdateAutomationRule(tenantID string, r *AutomationRule) (*AutomationRule, bool)
DeleteAutomationRule(tenantID, id string) bool
EnableAutomationRule(tenantID, id string) (*AutomationRule, bool)
DisableAutomationRule(tenantID, id string) (*AutomationRule, bool)
CreateAutomationExecution(tenantID string, e *AutomationExecution) *AutomationExecution
GetAutomationExecution(tenantID, id string) (*AutomationExecution, bool)
ListAutomationExecutions(tenantID string, limit int) []*AutomationExecution
```

### 表结构

```sql
CREATE TABLE IF NOT EXISTS automation_rules (
    id            VARCHAR(64)  NOT NULL,
    tenant_id     VARCHAR(64)  NOT NULL DEFAULT 'default',
    name          VARCHAR(255) NOT NULL,
    description   TEXT,
    trigger_type  VARCHAR(32)  NOT NULL,
    trigger_params TEXT,                   -- JSON: map[string]string
    actions       TEXT,                    -- JSON: []AutomationAction
    enabled       TINYINT(1)   NOT NULL DEFAULT 0,
    created_at    DATETIME     NOT NULL,
    updated_at    DATETIME     NOT NULL,
    PRIMARY KEY (id),
    KEY idx_auto_rule_tenant (tenant_id),
    KEY idx_auto_rule_enabled (tenant_id, enabled)
);

CREATE TABLE IF NOT EXISTS automation_executions (
    id         VARCHAR(64)  NOT NULL,
    tenant_id  VARCHAR(64)  NOT NULL DEFAULT 'default',
    rule_id    VARCHAR(64)  NOT NULL,
    rule_name  VARCHAR(255),
    status     VARCHAR(32)  NOT NULL DEFAULT 'pending',
    detail     TEXT,
    started_at DATETIME     NOT NULL,
    ended_at   DATETIME,                -- NULL 表示未结束
    PRIMARY KEY (id),
    KEY idx_auto_exec_tenant (tenant_id),
    KEY idx_auto_exec_rule (tenant_id, rule_id),
    KEY idx_auto_exec_started (tenant_id, started_at)
);
```

### SQL 查询模式

| 方法 | SQL |
|------|-----|
| CreateAutomationRule | `INSERT INTO automation_rules (...) VALUES (...) ON DUPLICATE KEY UPDATE name=VALUES(name),description=VALUES(description),trigger_type=VALUES(trigger_type),trigger_params=VALUES(trigger_params),actions=VALUES(actions),enabled=VALUES(enabled),updated_at=VALUES(updated_at)` |
| GetAutomationRule | `SELECT ... FROM automation_rules WHERE id=? AND tenant_id=?` |
| ListAutomationRules | `SELECT ... FROM automation_rules WHERE tenant_id=? ORDER BY created_at DESC` |
| UpdateAutomationRule | `UPDATE automation_rules SET name=?,description=?,trigger_type=?,trigger_params=?,actions=?,enabled=?,updated_at=? WHERE id=? AND tenant_id=?` |
| DeleteAutomationRule | `DELETE FROM automation_rules WHERE id=? AND tenant_id=?` |
| EnableAutomationRule | `UPDATE automation_rules SET enabled=1, updated_at=? WHERE id=? AND tenant_id=?` |
| DisableAutomationRule | `UPDATE automation_rules SET enabled=0, updated_at=? WHERE id=? AND tenant_id=?` |
| CreateAutomationExecution | `INSERT INTO automation_executions (...) VALUES (...) ON DUPLICATE KEY UPDATE rule_id=VALUES(rule_id),rule_name=VALUES(rule_name),status=VALUES(status),detail=VALUES(detail),started_at=VALUES(started_at),ended_at=VALUES(ended_at)` |
| GetAutomationExecution | `SELECT ... FROM automation_executions WHERE id=? AND tenant_id=?` |
| ListAutomationExecutions | `SELECT ... FROM automation_executions WHERE tenant_id=? ORDER BY started_at DESC LIMIT ?`（limit<=0 时省略 LIMIT） |

### 实现要点

- **两张表**：`automation_rules` + `automation_executions`
- **JSON 列**：`automation_rules.trigger_params`（`map[string]string`）+ `automation_rules.actions`（`[]AutomationAction`，嵌套 `Params map[string]string`）
- **bool 列 `enabled`**：`TINYINT(1)`，写入 `0/1`，读取扫到 `*bool` 或 `int` 后转换
- **可空 `ended_at`**：用 `sql.NullTime` 或指针处理
- **Enable/Disable**：置 `enabled=1/0` + `updated_at=now`；先 SELECT 校验，返回更新后的 Rule
- **ListAutomationExecutions**：`limit > 0` 时加 `LIMIT ?`；按 `started_at DESC`
- **ID 生成**：复用 memory 已有 `randAutomationRuleID()`（前缀 `rule-`）+ `randAutomationExecID()`（前缀 `exec-`）
- **CreateAutomationExecution 默认值**：`Status` 空 → `pending`；`StartedAt` 零值填 now

---

## 域 9: Network（P4）

### 数据模型

```go
type NetworkDevice struct {
    ID            string    `json:"id"`
    TenantID      string    `json:"tenantID"`
    Name          string    `json:"name"`
    Type          string    `json:"type"`          // switch|router|firewall|load_balancer
    Vendor        string    `json:"vendor"`
    Model         string    `json:"model"`
    IP            string    `json:"ip"`
    Mask          string    `json:"mask"`
    Mac           string    `json:"mac"`
    Location      string    `json:"location"`
    SnmpCommunity string    `json:"snmpCommunity"`
    Status        string    `json:"status"`        // up|down|unknown|maintain
    Config        string    `json:"config,omitempty"`
    CreatedAt     time.Time `json:"createdAt"`
    UpdatedAt     time.Time `json:"updatedAt"`
}

type NetworkMetrics struct {
    DeviceID    string    `json:"deviceID"`
    TenantID    string    `json:"tenantID"`
    Timestamp   time.Time `json:"timestamp"`
    CPUUsage    float64   `json:"cpuUsage"`
    MemoryUsage float64   `json:"memoryUsage"`
    Temperature float64   `json:"temperature"`
    Uptime      int64     `json:"uptime"`
}
```

### 接口方法

```go
CreateNetworkDevice(tenantID string, d *NetworkDevice) *NetworkDevice
GetNetworkDevice(tenantID, id string) (*NetworkDevice, bool)
ListNetworkDevices(tenantID string) []*NetworkDevice
UpdateNetworkDevice(tenantID string, d *NetworkDevice) (*NetworkDevice, bool)
DeleteNetworkDevice(tenantID, id string) bool
StoreNetworkMetrics(deviceID string, m *NetworkMetrics)
GetNetworkMetrics(deviceID string) *NetworkMetrics
UpdateNetworkConfig(tenantID, id, config string) (*NetworkDevice, bool)
```

### 表结构

```sql
CREATE TABLE IF NOT EXISTS network_devices (
    id            VARCHAR(64)  NOT NULL,
    tenant_id     VARCHAR(64)  NOT NULL DEFAULT 'default',
    name          VARCHAR(255) NOT NULL,
    type          VARCHAR(32),
    vendor        VARCHAR(128),
    model         VARCHAR(128),
    ip            VARCHAR(64),
    mask          VARCHAR(32),
    mac           VARCHAR(64),
    location      VARCHAR(255),
    snmp_community VARCHAR(255),
    status        VARCHAR(32)  NOT NULL DEFAULT 'unknown',
    config        TEXT,
    created_at    DATETIME     NOT NULL,
    updated_at    DATETIME     NOT NULL,
    PRIMARY KEY (id),
    KEY idx_netdev_tenant (tenant_id),
    KEY idx_netdev_ip (tenant_id, ip),
    KEY idx_netdev_status (tenant_id, status)
);

CREATE TABLE IF NOT EXISTS network_metrics (
    id           BIGINT       NOT NULL AUTO_INCREMENT,
    device_id    VARCHAR(64)  NOT NULL,
    tenant_id    VARCHAR(64)  NOT NULL DEFAULT 'default',
    timestamp    DATETIME     NOT NULL,
    cpu_usage    DOUBLE       NOT NULL DEFAULT 0,
    memory_usage DOUBLE       NOT NULL DEFAULT 0,
    temperature  DOUBLE       NOT NULL DEFAULT 0,
    uptime       BIGINT       NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    KEY idx_netmetrics_device (device_id, timestamp)
);
```

### SQL 查询模式

| 方法 | SQL |
|------|-----|
| CreateNetworkDevice | `INSERT INTO network_devices (...) VALUES (...) ON DUPLICATE KEY UPDATE name=VALUES(name),type=VALUES(type),vendor=VALUES(vendor),model=VALUES(model),ip=VALUES(ip),mask=VALUES(mask),mac=VALUES(mac),location=VALUES(location),snmp_community=VALUES(snmp_community),status=VALUES(status),config=VALUES(config),updated_at=VALUES(updated_at)` |
| GetNetworkDevice | `SELECT ... FROM network_devices WHERE id=? AND tenant_id=?` |
| ListNetworkDevices | `SELECT ... FROM network_devices WHERE tenant_id=? ORDER BY created_at DESC` |
| UpdateNetworkDevice | `UPDATE network_devices SET name=?,type=?,vendor=?,model=?,ip=?,mask=?,mac=?,location=?,snmp_community=?,status=?,config=?,updated_at=? WHERE id=? AND tenant_id=?` |
| DeleteNetworkDevice | `DELETE FROM network_devices WHERE id=? AND tenant_id=?` |
| StoreNetworkMetrics | `INSERT INTO network_metrics (device_id,tenant_id,timestamp,cpu_usage,memory_usage,temperature,uptime) VALUES (?,?,?,?,?,?,?)` |
| GetNetworkMetrics | `SELECT device_id,tenant_id,timestamp,cpu_usage,memory_usage,temperature,uptime FROM network_metrics WHERE device_id=? ORDER BY timestamp DESC LIMIT 1` |
| UpdateNetworkConfig | `UPDATE network_devices SET config=?, updated_at=? WHERE id=? AND tenant_id=?` |

### 实现要点

- **两张表**：`network_devices` + `network_metrics`
- **network_metrics 用自增 BIGINT 主键**（时序数据，追加写）；按 `(device_id, timestamp)` 索引查询最近一条
- **StoreNetworkMetrics**：追加写，不保留最近 N 条（DB 容量由 DBA 定期清理；memory 实现保留 100 条，SQL 层不模拟环形缓冲）；`Timestamp` 零值填 now
- **GetNetworkMetrics**：`ORDER BY timestamp DESC LIMIT 1` 取最近一条；不存在返回 nil
- **UpdateNetworkConfig**：仅更新 `config` 字段 + `updated_at`；先 SELECT 校验存在 + 租户归属，返回更新后的 Device
- **ID 生成**：复用 `randNetworkDeviceID()`（前缀 `netdev-`）
- **CreateNetworkDevice 默认值**：`Status` 空 → `unknown`
- **SnmpCommunity 敏感**：API 层负责脱敏后返回前端（与 Kubeconfig 一致）
- **ListNetworkDevices 降序**

---

## 域 10: Script（P5）

### 数据模型

```go
type Script struct {
    ID         string    `json:"id"`
    TenantID   string    `json:"tenantID"`
    Name       string    `json:"name"`
    Language   string    `json:"language"`   // shell|python
    Content    string    `json:"content"`
    Params     string    `json:"params"`
    TimeoutSec int       `json:"timeoutSec"`
    Enabled    bool      `json:"enabled"`
    CreatedAt  time.Time `json:"createdAt"`
    UpdatedAt  time.Time `json:"updatedAt"`
}

type ScriptExecution struct {
    ID         string     `json:"id"`
    TenantID   string     `json:"tenantID"`
    ScriptID   string     `json:"scriptID"`
    DeviceID   string     `json:"deviceID"`
    Status     string     `json:"status"`   // pending|running|succeeded|failed
    Stdout     string     `json:"stdout"`
    Stderr     string     `json:"stderr"`
    StartedAt  time.Time  `json:"startedAt"`
    FinishedAt *time.Time `json:"finishedAt,omitempty"` // 可空
}
```

### 接口方法

```go
CreateScript(tenantID string, sc *Script) *Script
GetScript(tenantID, id string) (*Script, bool)
UpdateScript(tenantID string, sc *Script) (*Script, bool)
ListScripts(tenantID string) []*Script
DeleteScript(tenantID, id string) bool
ListScriptExecutions(tenantID, scriptID string) []*ScriptExecution
RecordScriptExecution(tenantID, scriptID, deviceID, status, stdout, stderr string, startedAt time.Time, finishedAt *time.Time) *ScriptExecution
```

### 表结构

```sql
CREATE TABLE IF NOT EXISTS scripts (
    id          VARCHAR(64)  NOT NULL,
    tenant_id   VARCHAR(64)  NOT NULL DEFAULT 'default',
    name        VARCHAR(255) NOT NULL,
    language    VARCHAR(32)  NOT NULL DEFAULT 'shell',
    content     LONGTEXT,
    params      TEXT,
    timeout_sec INT          NOT NULL DEFAULT 0,
    enabled     TINYINT(1)   NOT NULL DEFAULT 1,
    created_at  DATETIME     NOT NULL,
    updated_at  DATETIME     NOT NULL,
    PRIMARY KEY (id),
    KEY idx_scripts_tenant (tenant_id)
);

CREATE TABLE IF NOT EXISTS script_executions (
    id          VARCHAR(64)  NOT NULL,
    tenant_id   VARCHAR(64)  NOT NULL DEFAULT 'default',
    script_id   VARCHAR(64)  NOT NULL,
    device_id   VARCHAR(64),
    status      VARCHAR(32)  NOT NULL DEFAULT 'pending',
    stdout      LONGTEXT,
    stderr      LONGTEXT,
    started_at  DATETIME     NOT NULL,
    finished_at DATETIME,                -- NULL 表示未结束
    PRIMARY KEY (id),
    KEY idx_script_exec_tenant (tenant_id),
    KEY idx_script_exec_script (tenant_id, script_id),
    KEY idx_script_exec_started (tenant_id, started_at)
);
```

### SQL 查询模式

| 方法 | SQL |
|------|-----|
| CreateScript | `INSERT INTO scripts (...) VALUES (...) ON DUPLICATE KEY UPDATE name=VALUES(name),language=VALUES(language),content=VALUES(content),params=VALUES(params),timeout_sec=VALUES(timeout_sec),enabled=VALUES(enabled),updated_at=VALUES(updated_at)` |
| GetScript | `SELECT ... FROM scripts WHERE id=? AND tenant_id=?` |
| UpdateScript | `UPDATE scripts SET name=?,language=?,content=?,params=?,timeout_sec=?,enabled=?,updated_at=? WHERE id=? AND tenant_id=?` |
| ListScripts | `SELECT ... FROM scripts WHERE tenant_id=? ORDER BY created_at DESC` |
| DeleteScript | `DELETE FROM scripts WHERE id=? AND tenant_id=?` |
| ListScriptExecutions | `SELECT ... FROM script_executions WHERE tenant_id=? AND script_id=? ORDER BY started_at DESC` |
| RecordScriptExecution | `INSERT INTO script_executions (id,tenant_id,script_id,device_id,status,stdout,stderr,started_at,finished_at) VALUES (?,?,?,?,?,?,?,?)` |

### 实现要点

- **两张表**：`scripts` + `script_executions`
- **bool 列 `enabled`**：`TINYINT(1)`，默认 `1`（新建脚本默认启用，与 memory 一致，避免零值 false 导致 execute 全部被拒）
- **可空 `finished_at`**：用 `sql.NullTime` 或指针处理
- **RecordScriptExecution**：`ID` 由 store 分配（`randScriptExecutionID()`，前缀 `script-exec-`）；`StartedAt` 零值填 now；返回插入的 ScriptExecution
- **ListScriptExecutions**：先校验 scriptID 归属（GetScript），不匹配返回空 slice；按 `started_at DESC`
- **ID 生成**：复用 `randScriptID()`（前缀 `script-`）+ `randScriptExecutionID()`
- **CreateScript 默认值**：`Language` 空 → `shell`；`Enabled` 零值 → `true`（与 memory 一致）
- **ListScripts 降序**

---

## 域 11: Webhook（P5）

### 数据模型

```go
type Webhook struct {
    ID               string            `json:"id"`
    TenantID         string            `json:"tenantID"`
    Name             string            `json:"name"`
    URL              string            `json:"url"`
    Events           []string          `json:"events"`           // JSON 列
    Headers          map[string]string `json:"headers"`          // JSON 列
    BodyTemplate     string            `json:"bodyTemplate"`
    Enabled          bool              `json:"enabled"`
    RetryCount       int               `json:"retryCount"`
    RetryIntervalSec int               `json:"retryIntervalSec"`
    CreatedAt        time.Time         `json:"createdAt"`
    UpdatedAt        time.Time         `json:"updatedAt"`
}

type WebhookDelivery struct {
    ID          string    `json:"id"`
    TenantID    string    `json:"tenantID"`
    WebhookID   string    `json:"webhookID"`
    Event       string    `json:"event"`
    Payload     string    `json:"payload"`
    StatusCode  int       `json:"statusCode"`
    Response    string    `json:"response"`
    Error       string    `json:"error"`
    DeliveredAt time.Time `json:"deliveredAt"`
}
```

### 接口方法

```go
CreateWebhook(tenantID string, wh *Webhook) *Webhook
GetWebhook(tenantID, id string) (*Webhook, bool)
UpdateWebhook(tenantID string, wh *Webhook) (*Webhook, bool)
ListWebhooks(tenantID string) []*Webhook
DeleteWebhook(tenantID, id string) bool
ListWebhookDeliveries(tenantID, webhookID string) []*WebhookDelivery
RecordWebhookDelivery(tenantID, webhookID, event, payload string, statusCode int, response, errStr string) *WebhookDelivery
```

### 表结构

```sql
CREATE TABLE IF NOT EXISTS webhooks (
    id                VARCHAR(64)  NOT NULL,
    tenant_id         VARCHAR(64)  NOT NULL DEFAULT 'default',
    name              VARCHAR(255) NOT NULL,
    url               TEXT,
    events            TEXT,                    -- JSON: []string
    headers           TEXT,                    -- JSON: map[string]string
    body_template     TEXT,
    enabled           TINYINT(1)   NOT NULL DEFAULT 1,
    retry_count       INT          NOT NULL DEFAULT 0,
    retry_interval_sec INT         NOT NULL DEFAULT 0,
    created_at        DATETIME     NOT NULL,
    updated_at        DATETIME     NOT NULL,
    PRIMARY KEY (id),
    KEY idx_webhooks_tenant (tenant_id),
    KEY idx_webhooks_enabled (tenant_id, enabled)
);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id          VARCHAR(64)  NOT NULL,
    tenant_id   VARCHAR(64)  NOT NULL DEFAULT 'default',
    webhook_id  VARCHAR(64)  NOT NULL,
    event       VARCHAR(255),
    payload     LONGTEXT,
    status_code INT          NOT NULL DEFAULT 0,
    response    LONGTEXT,
    error       TEXT,
    delivered_at DATETIME    NOT NULL,
    PRIMARY KEY (id),
    KEY idx_webhook_deliv_tenant (tenant_id),
    KEY idx_webhook_deliv_webhook (tenant_id, webhook_id),
    KEY idx_webhook_deliv_delivered (tenant_id, delivered_at)
);
```

### SQL 查询模式

| 方法 | SQL |
|------|-----|
| CreateWebhook | `INSERT INTO webhooks (...) VALUES (...) ON DUPLICATE KEY UPDATE name=VALUES(name),url=VALUES(url),events=VALUES(events),headers=VALUES(headers),body_template=VALUES(body_template),enabled=VALUES(enabled),retry_count=VALUES(retry_count),retry_interval_sec=VALUES(retry_interval_sec),updated_at=VALUES(updated_at)` |
| GetWebhook | `SELECT ... FROM webhooks WHERE id=? AND tenant_id=?` |
| UpdateWebhook | `UPDATE webhooks SET name=?,url=?,events=?,headers=?,body_template=?,enabled=?,retry_count=?,retry_interval_sec=?,updated_at=? WHERE id=? AND tenant_id=?` |
| ListWebhooks | `SELECT ... FROM webhooks WHERE tenant_id=? ORDER BY created_at DESC` |
| DeleteWebhook | `DELETE FROM webhooks WHERE id=? AND tenant_id=?` |
| ListWebhookDeliveries | `SELECT ... FROM webhook_deliveries WHERE tenant_id=? AND webhook_id=? ORDER BY delivered_at DESC` |
| RecordWebhookDelivery | `INSERT INTO webhook_deliveries (id,tenant_id,webhook_id,event,payload,status_code,response,error,delivered_at) VALUES (?,?,?,?,?,?,?,?,?)` |

### 实现要点

- **两张表**：`webhooks` + `webhook_deliveries`
- **JSON 列**：`webhooks.events`（`[]string`）+ `webhooks.headers`（`map[string]string`）
- **bool 列 `enabled`**：`TINYINT(1)`，默认 `1`
- **RecordWebhookDelivery**：`ID` 由 store 分配（新建 `randWebhookDeliveryID()`，前缀 `webhook-deliv-`）；`DeliveredAt` 填 now；返回插入的 WebhookDelivery
- **ListWebhookDeliveries**：按 `delivered_at DESC`
- **ID 生成**：复用 memory 已有 `randWebhookID()`（前缀 `webhook-`）+ `randWebhookDeliveryID()`
- **ListWebhooks 降序**

---

## 域 12: Tenant（P6）

### 数据模型

```go
type Tenant struct {
    ID          string        `json:"id"`
    Name        string        `json:"name"`
    DisplayName string        `json:"displayName"`
    Status      TenantStatus  `json:"status"`      // active|suspended|disabled
    Quota       TenantQuota   `json:"quota"`       // JSON 列
    Usage       ResourceUsage `json:"usage"`       // JSON 列
    CreatedAt   time.Time     `json:"createdAt"`
    UpdatedAt   time.Time     `json:"updatedAt"`
}

type TenantQuota struct {
    MaxDevices     int `json:"maxDevices"`
    MaxTasks       int `json:"maxTasks"`
    MaxActiveTasks int `json:"maxActiveTasks"`
    MaxAlerts      int `json:"maxAlerts"`
    MaxAgents      int `json:"maxAgents"`
    MaxWebhooks    int `json:"maxWebhooks"`
    MaxAPIKeys     int `json:"maxAPIKeys"`
}

type ResourceUsage struct {
    Devices     int `json:"devices"`
    Tasks       int `json:"tasks"`
    ActiveTasks int `json:"activeTasks"`
    Alerts      int `json:"alerts"`
    Agents      int `json:"agents"`
    Webhooks    int `json:"webhooks"`
    APIKeys     int `json:"apiKeys"`
}
```

### 接口方法

```go
CreateTenant(tenant *Tenant) *Tenant
GetTenant(id string) (*Tenant, bool)
UpdateTenant(tenant *Tenant) (*Tenant, bool)
ListTenants() []*Tenant
DeleteTenant(id string) bool
```

### 表结构

```sql
CREATE TABLE IF NOT EXISTS tenants (
    id           VARCHAR(64)  NOT NULL,
    name         VARCHAR(128) NOT NULL,
    display_name VARCHAR(255),
    status       VARCHAR(32)  NOT NULL DEFAULT 'active',
    quota        TEXT,                    -- JSON: TenantQuota
    usage        TEXT,                    -- JSON: ResourceUsage
    created_at   DATETIME     NOT NULL,
    updated_at   DATETIME     NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_tenants_name (name),
    KEY idx_tenants_status (status)
);
```

### SQL 查询模式

| 方法 | SQL |
|------|-----|
| CreateTenant | `INSERT INTO tenants (...) VALUES (...) ON DUPLICATE KEY UPDATE name=VALUES(name),display_name=VALUES(display_name),status=VALUES(status),quota=VALUES(quota),usage=VALUES(usage),updated_at=VALUES(updated_at)` |
| GetTenant | `SELECT ... FROM tenants WHERE id=?` |
| UpdateTenant | `UPDATE tenants SET name=?,display_name=?,status=?,quota=?,usage=?,updated_at=? WHERE id=?` |
| ListTenants | `SELECT ... FROM tenants ORDER BY created_at ASC` |
| DeleteTenant | `DELETE FROM tenants WHERE id=?` |

### 实现要点

- **无租户参数**（按 ID 全局唯一，租户本身就是租户）；`GetTenant` / `DeleteTenant` 不带 `tenant_id` 条件
- **JSON 列**：`quota`（`TenantQuota`）+ `usage`（`ResourceUsage`）
- **`name` 唯一约束**：`UNIQUE KEY uq_tenants_name`（URL-safe 租户标识唯一）
- **ID 生成**：复用 memory 已有 `randTenantID()`（前缀 `tenant-`）
- **CreateTenant 默认值**：`Status` 空 → `active`；`CreatedAt` 零值填 now；`UpdatedAt` 始终刷新
- **UpdateTenant**：保留原 `CreatedAt`；`ID` 不可改
- **ListTenants 升序**（`ORDER BY created_at ASC`）

---

## 域 13: APIKey（P6）

### 数据模型

```go
type APIKey struct {
    ID              string    `json:"id"`
    TenantID        string    `json:"tenantID"`
    Name            string    `json:"name"`
    Key             string    `json:"-"`               // SHA-256 hash；JSON 不输出
    Scopes          []string  `json:"scopes"`          // JSON 列
    RateLimitPerSec int       `json:"rateLimitPerSec"`
    ExpiresAt       time.Time `json:"expiresAt"`
    LastUsedAt      time.Time `json:"lastUsedAt"`
    Enabled         bool      `json:"enabled"`
    CreatedAt       time.Time `json:"createdAt"`
}
```

### 接口方法

```go
CreateAPIKey(tenantID string, key *APIKey) *APIKey
GetAPIKey(tenantID, id string) (*APIKey, bool)
UpdateAPIKey(tenantID string, key *APIKey) (*APIKey, bool)
ListAPIKeys(tenantID string) []*APIKey
DeleteAPIKey(tenantID, id string) bool
```

### 表结构

```sql
CREATE TABLE IF NOT EXISTS api_keys (
    id               VARCHAR(64)  NOT NULL,
    tenant_id        VARCHAR(64)  NOT NULL DEFAULT 'default',
    name             VARCHAR(255) NOT NULL,
    key_hash         VARCHAR(128) NOT NULL,           -- SHA-256 hash
    scopes           TEXT,                            -- JSON: []string
    rate_limit_per_sec INT          NOT NULL DEFAULT 0,
    expires_at       DATETIME,                       -- NULL 表示永不过期
    last_used_at     DATETIME,
    enabled          TINYINT(1)   NOT NULL DEFAULT 1,
    created_at       DATETIME     NOT NULL,
    PRIMARY KEY (id),
    KEY idx_apikeys_tenant (tenant_id),
    KEY idx_apikeys_hash (key_hash),
    KEY idx_apikeys_enabled (tenant_id, enabled)
);
```

### SQL 查询模式

| 方法 | SQL |
|------|-----|
| CreateAPIKey | `INSERT INTO api_keys (...) VALUES (...) ON DUPLICATE KEY UPDATE name=VALUES(name),key_hash=VALUES(key_hash),scopes=VALUES(scopes),rate_limit_per_sec=VALUES(rate_limit_per_sec),expires_at=VALUES(expires_at),enabled=VALUES(enabled)` |
| GetAPIKey | `SELECT ... FROM api_keys WHERE id=? AND tenant_id=?` |
| UpdateAPIKey | `UPDATE api_keys SET name=?,key_hash=?,scopes=?,rate_limit_per_sec=?,expires_at=?,last_used_at=?,enabled=? WHERE id=? AND tenant_id=?` |
| ListAPIKeys | `SELECT ... FROM api_keys WHERE tenant_id=? ORDER BY created_at DESC`（tenantID 空时省略 WHERE，返回全部租户） |
| DeleteAPIKey | `DELETE FROM api_keys WHERE id=? AND tenant_id=?` |

### 实现要点

- **JSON 列 `scopes`**：`[]string` 序列化为 JSON 数组
- **敏感列 `key_hash`**：存 SHA-256 hash（明文仅在创建时返回一次）；列名用 `key_hash` 避免与 SQL 关键字 `key` 冲突
- **bool 列 `enabled`**：`TINYINT(1)`，默认 `1`
- **可空 `expires_at` / `last_used_at`**：零值 `time.Time` 视为"永不/未用"，用 `sql.NullTime` 处理；读取时 NULL → 零值 `time.Time{}`
- **ListAPIKeys**：`tenantID == ""` 时返回全部租户的 API Key（供 `platform.APIKeyManager.ValidateKey` 全租户扫描），省略 `WHERE tenant_id=?` 条件（与 memory 一致）
- **ID 生成**：复用 `randAPIKeyID()`（前缀 `apikey-`）
- **CreateAPIKey**：`CreatedAt` 零值填 now；`Enabled` 零值 → `true`（新建默认启用）
- **UpdateAPIKey**：保留原 `CreatedAt` / `TenantID` 不可改
- **ListAPIKeys 降序**

---

## 域 14: Plugin（P6）

### 数据模型

```go
type Plugin struct {
    ID          string    `json:"id"`
    Name        string    `json:"name"`
    Version     string    `json:"version"`
    Description string    `json:"description"`
    Author      string    `json:"author"`
    Type        string    `json:"type"`          // agent|controlplane|ui
    DownloadURL string    `json:"downloadURL"`
    Checksum    string    `json:"checksum"`      // SHA-256
    Installed   bool      `json:"installed"`
    Enabled     bool      `json:"enabled"`
    CreatedAt   time.Time `json:"createdAt"`
}
```

### 接口方法

```go
CreatePlugin(plugin *Plugin) *Plugin
GetPlugin(id string) (*Plugin, bool)
UpdatePlugin(plugin *Plugin) (*Plugin, bool)
ListPlugins() []*Plugin
DeletePlugin(id string) bool
```

### 表结构

```sql
CREATE TABLE IF NOT EXISTS plugins (
    id           VARCHAR(64)  NOT NULL,
    name         VARCHAR(255) NOT NULL,
    version      VARCHAR(64),
    description  TEXT,
    author       VARCHAR(255),
    type         VARCHAR(32)  NOT NULL DEFAULT 'agent',
    download_url TEXT,
    checksum     VARCHAR(128),
    installed    TINYINT(1)   NOT NULL DEFAULT 0,
    enabled      TINYINT(1)   NOT NULL DEFAULT 0,
    created_at   DATETIME     NOT NULL,
    PRIMARY KEY (id),
    KEY idx_plugins_type (type),
    KEY idx_plugins_name (name)
);
```

### SQL 查询模式

| 方法 | SQL |
|------|-----|
| CreatePlugin | `INSERT INTO plugins (...) VALUES (...) ON DUPLICATE KEY UPDATE name=VALUES(name),version=VALUES(version),description=VALUES(description),author=VALUES(author),type=VALUES(type),download_url=VALUES(download_url),checksum=VALUES(checksum),installed=VALUES(installed),enabled=VALUES(enabled)` |
| GetPlugin | `SELECT ... FROM plugins WHERE id=?` |
| UpdatePlugin | `UPDATE plugins SET name=?,version=?,description=?,author=?,type=?,download_url=?,checksum=?,installed=?,enabled=? WHERE id=?` |
| ListPlugins | `SELECT ... FROM plugins ORDER BY created_at ASC` |
| DeletePlugin | `DELETE FROM plugins WHERE id=?` |

### 实现要点

- **无租户参数**（插件市场全局共享）；`GetPlugin` / `DeletePlugin` 不带 `tenant_id` 条件
- **bool 列 `installed` / `enabled`**：`TINYINT(1)`
- **纯标量字段**，无 JSON 列；无 `updated_at`（只有 `created_at`）
- **ID 生成**：复用 memory 已有 `randPluginID()`（前缀 `plugin-`）
- **CreatePlugin**：`CreatedAt` 零值填 now；`Type` 空 → `agent`
- **UpdatePlugin**：保留原 `CreatedAt`；`ID` 不可改
- **ListPlugins 升序**（`ORDER BY created_at ASC`）

---

## 域 15: Billing（P6）

### 数据模型

```go
type SubscriptionPlan struct {
    ID             string      `json:"id"`
    Name           string      `json:"name"`
    Price          int         `json:"price"`          // 单位：分
    Interval       string      `json:"interval"`       // monthly|yearly
    Features       []string    `json:"features"`       // JSON 列
    ResourceLimits TenantQuota `json:"resourceLimits"` // JSON 列
    CreatedAt      time.Time   `json:"createdAt"`
}

type Subscription struct {
    ID        string    `json:"id"`
    TenantID  string    `json:"tenantID"`
    PlanID    string    `json:"planID"`
    Status    string    `json:"status"`        // active|canceled|expired
    StartedAt time.Time `json:"startedAt"`
    ExpiresAt time.Time `json:"expiresAt"`
    CreatedAt time.Time `json:"createdAt"`
}

type Invoice struct {
    ID             string        `json:"id"`
    TenantID       string        `json:"tenantID"`
    SubscriptionID string        `json:"subscriptionID"`
    Amount         int           `json:"amount"`       // 单位：分
    PeriodStart    time.Time     `json:"periodStart"`
    PeriodEnd      time.Time     `json:"periodEnd"`
    Status         string        `json:"status"`       // pending|paid|overdue
    Items          []InvoiceItem `json:"items"`        // JSON 列
    CreatedAt      time.Time     `json:"createdAt"`
}

type InvoiceItem struct {
    Name      string `json:"name"`
    Quantity  int    `json:"quantity"`
    UnitPrice int    `json:"unitPrice"` // 单位：分
    Amount    int    `json:"amount"`    // 单位：分
}
```

### 接口方法

```go
CreateBillingPlan(plan *SubscriptionPlan) *SubscriptionPlan
GetBillingPlan(id string) (*SubscriptionPlan, bool)
ListBillingPlans() []*SubscriptionPlan
UpdateBillingPlan(plan *SubscriptionPlan) (*SubscriptionPlan, bool)
DeleteBillingPlan(id string) bool
CreateSubscription(sub *Subscription) *Subscription
GetSubscription(id string) (*Subscription, bool)
ListSubscriptions(tenantID string) []*Subscription
UpdateSubscription(sub *Subscription) (*Subscription, bool)
DeleteSubscription(id string) bool
CreateInvoice(inv *Invoice) *Invoice
GetInvoice(id string) (*Invoice, bool)
ListInvoices(tenantID string) []*Invoice
```

### 表结构

```sql
CREATE TABLE IF NOT EXISTS billing_plans (
    id              VARCHAR(64)  NOT NULL,
    name            VARCHAR(255) NOT NULL,
    price           INT          NOT NULL DEFAULT 0,
    interval        VARCHAR(32)  NOT NULL DEFAULT 'monthly',
    features        TEXT,                    -- JSON: []string
    resource_limits TEXT,                    -- JSON: TenantQuota
    created_at      DATETIME     NOT NULL,
    PRIMARY KEY (id),
    KEY idx_billing_plans_interval (interval)
);

CREATE TABLE IF NOT EXISTS subscriptions (
    id         VARCHAR(64)  NOT NULL,
    tenant_id  VARCHAR(64)  NOT NULL DEFAULT 'default',
    plan_id    VARCHAR(64)  NOT NULL,
    status     VARCHAR(32)  NOT NULL DEFAULT 'active',
    started_at DATETIME     NOT NULL,
    expires_at DATETIME     NOT NULL,
    created_at DATETIME     NOT NULL,
    PRIMARY KEY (id),
    KEY idx_subscriptions_tenant (tenant_id),
    KEY idx_subscriptions_plan (tenant_id, plan_id),
    KEY idx_subscriptions_status (tenant_id, status)
);

CREATE TABLE IF NOT EXISTS invoices (
    id              VARCHAR(64)  NOT NULL,
    tenant_id       VARCHAR(64)  NOT NULL DEFAULT 'default',
    subscription_id VARCHAR(64)  NOT NULL,
    amount          INT          NOT NULL DEFAULT 0,
    period_start    DATETIME     NOT NULL,
    period_end      DATETIME     NOT NULL,
    status          VARCHAR(32)  NOT NULL DEFAULT 'pending',
    items           TEXT,                    -- JSON: []InvoiceItem
    created_at      DATETIME     NOT NULL,
    PRIMARY KEY (id),
    KEY idx_invoices_tenant (tenant_id),
    KEY idx_invoices_subscription (tenant_id, subscription_id),
    KEY idx_invoices_status (tenant_id, status),
    KEY idx_invoices_created (tenant_id, created_at)
);
```

### SQL 查询模式

| 方法 | SQL |
|------|-----|
| CreateBillingPlan | `INSERT INTO billing_plans (...) VALUES (...) ON DUPLICATE KEY UPDATE name=VALUES(name),price=VALUES(price),interval=VALUES(interval),features=VALUES(features),resource_limits=VALUES(resource_limits)` |
| GetBillingPlan | `SELECT ... FROM billing_plans WHERE id=?` |
| ListBillingPlans | `SELECT ... FROM billing_plans ORDER BY created_at ASC` |
| UpdateBillingPlan | `UPDATE billing_plans SET name=?,price=?,interval=?,features=?,resource_limits=? WHERE id=?` |
| DeleteBillingPlan | `DELETE FROM billing_plans WHERE id=?` |
| CreateSubscription | `INSERT INTO subscriptions (...) VALUES (...) ON DUPLICATE KEY UPDATE plan_id=VALUES(plan_id),status=VALUES(status),started_at=VALUES(started_at),expires_at=VALUES(expires_at)` |
| GetSubscription | `SELECT ... FROM subscriptions WHERE id=?` |
| ListSubscriptions | `SELECT ... FROM subscriptions WHERE tenant_id=? ORDER BY created_at DESC` |
| UpdateSubscription | `UPDATE subscriptions SET plan_id=?,status=?,started_at=?,expires_at=? WHERE id=?` |
| DeleteSubscription | `DELETE FROM subscriptions WHERE id=?` |
| CreateInvoice | `INSERT INTO invoices (...) VALUES (...) ON DUPLICATE KEY UPDATE subscription_id=VALUES(subscription_id),amount=VALUES(amount),period_start=VALUES(period_start),period_end=VALUES(period_end),status=VALUES(status),items=VALUES(items)` |
| GetInvoice | `SELECT ... FROM invoices WHERE id=?` |
| ListInvoices | `SELECT ... FROM invoices WHERE tenant_id=? ORDER BY created_at DESC` |

### 实现要点

- **三张表**：`billing_plans`（计划）+ `subscriptions`（订阅）+ `invoices`（账单）
- **billing_plans 无租户参数**（计划全局共享）；`GetBillingPlan` / `DeleteBillingPlan` / `UpdateBillingPlan` 不带 `tenant_id`
- **subscriptions / invoices 按 ID 全局唯一**：`GetSubscription` / `DeleteSubscription` / `GetInvoice` 不带 `tenant_id` 条件（与 memory 一致，按 ID 定位）；`ListSubscriptions` / `ListInvoices` 按 `tenant_id` 过滤
- **JSON 列**：`billing_plans.features`（`[]string`）+ `billing_plans.resource_limits`（`TenantQuota`）+ `invoices.items`（`[]InvoiceItem`）
- **金额用 INT（单位：分）**，避免浮点精度
- **ID 生成**：复用 `randBillingID(prefix)`（memory_billing.go 已定义）；Plan 前缀 `plan`，Subscription 前缀 `sub`，Invoice 前缀 `inv`
- **CreateSubscription 默认值**：`Status` 空 → `active`；`CreatedAt` 零值填 now
- **CreateInvoice 默认值**：`Status` 空 → `pending`；`CreatedAt` 零值填 now
- **UpdateBillingPlan / UpdateSubscription**：保留原 `CreatedAt`；`ID` 不可改；`UpdateSubscription` 保留 `TenantID` 不可改
- **ListBillingPlans 升序**；`ListSubscriptions` / `ListInvoices` 降序

---

## 附录 A：表名汇总（共 22 张表）

| 域 | 优先级 | 表名 | 主键 | 租户隔离 |
|----|--------|------|------|----------|
| SLO | P1 | `slos` | `id` | 是 |
| Ticket | P1 | `tickets` | `id` | 是 |
| ArgoCD | P2 | `argocd_apps` | `id` | 是 |
| Pipeline | P2 | `pipeline_templates` | `id` | 是 |
| Pipeline | P2 | `pipeline_runs` | `id` | 是 |
| Traffic | P2 | `traffic_policies` | `id` | 是 |
| Backup | P3 | `backup_records` | `id` | 是 |
| Compliance | P3 | `compliance_reports` | `id` | 是 |
| Automation | P4 | `automation_rules` | `id` | 是 |
| Automation | P4 | `automation_executions` | `id` | 是 |
| Network | P4 | `network_devices` | `id` | 是 |
| Network | P4 | `network_metrics` | `id` (BIGINT AUTO_INCREMENT) | 是 |
| Script | P5 | `scripts` | `id` | 是 |
| Script | P5 | `script_executions` | `id` | 是 |
| Webhook | P5 | `webhooks` | `id` | 是 |
| Webhook | P5 | `webhook_deliveries` | `id` | 是 |
| Tenant | P6 | `tenants` | `id` | 否（本身就是租户） |
| APIKey | P6 | `api_keys` | `id` | 是 |
| Plugin | P6 | `plugins` | `id` | 否（全局共享） |
| Billing | P6 | `billing_plans` | `id` | 否（全局共享） |
| Billing | P6 | `subscriptions` | `id` | 是（List 按 tenant_id） |
| Billing | P6 | `invoices` | `id` | 是（List 按 tenant_id） |

## 附录 B：JSON 列汇总

| 表 | 列 | Go 类型 | 说明 |
|----|------|---------|------|
| `slos` | `slis` | `[]SLI` | SLI 列表 |
| `tickets` | `tags` | `[]string` | 标签数组 |
| `pipeline_templates` | `parameters` | `[]PipelineParam` | 流水线参数 |
| `pipeline_runs` | `parameters` | `map[string]string` | 运行参数 |
| `traffic_policies` | `canary_weights` | `map[string]int` | 金丝雀权重 |
| `compliance_reports` | `results` | `[]ComplianceResult` | 检查结果 |
| `automation_rules` | `trigger_params` | `map[string]string` | 触发器参数 |
| `automation_rules` | `actions` | `[]AutomationAction` | 动作列表（嵌套 map） |
| `webhooks` | `events` | `[]string` | 事件列表 |
| `webhooks` | `headers` | `map[string]string` | 自定义请求头 |
| `tenants` | `quota` | `TenantQuota` | 资源配额 |
| `tenants` | `usage` | `ResourceUsage` | 资源用量 |
| `api_keys` | `scopes` | `[]string` | 权限范围 |
| `billing_plans` | `features` | `[]string` | 功能列表 |
| `billing_plans` | `resource_limits` | `TenantQuota` | 资源限额 |
| `invoices` | `items` | `[]InvoiceItem` | 账单明细 |

## 附录 C：可空时间列汇总

| 表 | 列 | Go 类型 | 语义 |
|----|------|---------|------|
| `tickets` | `resolved_at` | `*time.Time` | NULL 表示未解决 |
| `pipeline_runs` | `started_at` | `*time.Time` | NULL 表示未启动 |
| `pipeline_runs` | `finished_at` | `*time.Time` | NULL 表示未结束 |
| `automation_executions` | `ended_at` | `*time.Time` | NULL 表示未结束 |
| `script_executions` | `finished_at` | `*time.Time` | NULL 表示未结束 |
| `api_keys` | `expires_at` | `time.Time` | NULL/零值表示永不过期 |
| `api_keys` | `last_used_at` | `time.Time` | NULL/零值表示未使用 |

## 附录 D：bool 列汇总（TINYINT(1)）

| 表 | 列 | 默认值 | 说明 |
|----|------|--------|------|
| `automation_rules` | `enabled` | 0 | 规则启用 |
| `scripts` | `enabled` | 1 | 脚本启用（新建默认启用） |
| `webhooks` | `enabled` | 1 | Webhook 启用 |
| `plugins` | `installed` | 0 | 已安装 |
| `plugins` | `enabled` | 0 | 已启用 |
| `api_keys` | `enabled` | 1 | API Key 启用 |

## 附录 E：实现顺序建议

按优先级批次实现，每批完成后 `go build ./internal/store/...` 验证：

1. **Batch 2（P1）**：`sql_slo.go` + `sql_ticket.go`（2 域，2 张表）
2. **Batch 3（P2）**：`sql_argocd.go` + `sql_pipeline.go` + `sql_traffic.go`（3 域，5 张表）
3. **Batch 4（P3）**：`sql_backup.go` + `sql_compliance.go`（2 域，2 张表）
4. **Batch 5（P4）**：`sql_automation.go` + `sql_network.go`（2 域，4 张表）
5. **Batch 6（P5）**：`sql_script.go` + `sql_webhook.go`（2 域，4 张表）
6. **Batch 7（P6）**：`sql_tenant.go` + `sql_apikey.go` + `sql_plugin.go` + `sql_billing.go`（4 域，5 张表）

每域实现后删除对应 `StubNotImplemented` 调用，替换为真实 SQL；`initSchema` 中追加 `CREATE TABLE IF NOT EXISTS`（幂等建表）。

---

## StubDomains 清理（必须）

`stub_guard.go` 的 `StubDomains` 列表（约第 62-66 行）硬编码了 15 个 P1-P6 域名。`WarnStubStoreDomains`（约第 71 行）在 SQL 后端启动时据此输出"以下领域未持久化"警告。实现真实 SQL 后若不移除已实现域，会继续误报警告，误导运维判断。

此外 `internal/config` 的 Validate 错误信息维护了同一名单的字面量副本（`stub_guard.go` 第 65 行注释明确指出"两处新增领域时须同步更新"），需同步清理。

**操作要求**：

1. 每域实现完成后，从 `stub_guard.go` 的 `StubDomains` 列表移除该域字符串
2. 同步更新 `internal/config` 中 Validate 错误信息的字面量副本
3. 全部 15 域实现完成后，`StubDomains` 应为空列表（或移除 `WarnStubStoreDomains` 调用）

**15 个域名**（按实现批次）：
- Batch 2（P1）：`slo`, `ticket`
- Batch 3（P2）：`argocd`, `pipeline`, `traffic`
- Batch 4（P3）：`backup`, `compliance`
- Batch 5（P4）：`automation`, `network`
- Batch 6（P5）：`script`, `webhook`
- Batch 7（P6）：`tenant`, `apikey`, `plugin`, `billing`

---

## 审核意见

> 审核人：OpsMesh 架构审核员
> 审核时间：2026-08-27
> 审核依据：参考实现 `sql_k8s.go` / `sql_secret.go`、memory 实现 `memory_*.go`、`stub_guard.go`、迁移文件 `001-009`

### 总体评价

**通过（已修复）**

> 原审核发现 2 类阻塞性问题，已于 2026-08-27 由 Team Leader 修复：
> 1. ID 生成函数命名/前缀已全部对齐 memory 实现（10 处修正）
> 2. 已补充"StubDomains 清理"章节，明确移除要求

设计文档整体质量高：表结构、SQL 注入防护、租户隔离、索引设计、类型映射、JSON 列处理、可空字段处理等方面均符合生产就绪标准，与参考实现 `sql_k8s.go` / `sql_secret.go` 风格一致。

### 逐项审核结果

| # | 审核项 | 结果 | 说明 |
|---|--------|------|------|
| 1 | 表结构合理性 | ✅ | 主键设计合理（单主键 `id VARCHAR(64)`；`network_metrics` 用 `BIGINT AUTO_INCREMENT` 适配时序追加写）；列类型合理（VARCHAR/TEXT/LONGTEXT/INT/BIGINT/DOUBLE/DATETIME/TINYINT(1) 各得其所）；NOT NULL 与 DEFAULT 与 memory 默认值一致 |
| 2 | SQL 注入防护 | ✅ | 所有查询使用 `?` 占位符 + `args` 参数化；`ListTickets` 动态拼接 WHERE 子句仅拼字段名（status/priority/category/assignee_id），值仍走 `?`，安全；ORDER BY 子句为固定字符串，无注入面 |
| 3 | 租户隔离 | ✅ | 有租户域的 Get/Update/Delete 均 `WHERE id=? AND tenant_id=?`，List 均 `WHERE tenant_id=?`；例外域（tenant/plugin/billing_plans 全局共享，subscriptions/invoices 按 ID 定位、List 按 tenant_id 过滤，api_keys 空 tenantID 返回全部供 ValidateKey 扫描）均与 memory 实现一致 |
| 4 | 索引设计 | ✅ | 有租户表均含 `KEY idx_*_tenant (tenant_id)` 或以 tenant_id 为前缀的复合索引；常用过滤字段（status/priority/service_name/template_id/script_id/webhook_id/rule_id/plan_id/device_id/enabled 等）均有索引；时间列（created_at/started_at/delivered_at）有索引支撑 ORDER BY；索引数量适中无过度。注：`network_metrics` 无 tenant_id 索引，但查询路径按 `(device_id, timestamp)`，设计合理 |
| 5 | 类型映射 | ✅ | Go string→VARCHAR(255)/TEXT/LONGTEXT（长文本用 TEXT/LONGTEXT）；Go int→INT，Go int64→BIGINT；Go time.Time→DATETIME；Go bool→TINYINT(1)；Go map/slice→TEXT(JSON)；Go *time.Time→DATETIME NULL；Go float64→DOUBLE。全部正确 |
| 6 | 与内存实现语义一致性 | ❌ | 租户归一（空→default）、时间填充（CreatedAt 零值→now、UpdatedAt 始终刷新）、Update 保留 CreatedAt/TenantID 不可改、List 排序方向均与 memory 一致。**但 ID 生成函数命名/前缀与 memory 实现大面积不一致**（见问题 P1），违反设计原则第 23 行"与 memory 实现共用同名函数"的声明，会导致编译错误（重复定义）或 ID 格式不一致（破坏跨后端语义一致性） |
| 7 | JSON 列处理 | ✅ | 复杂结构（[]SLI/[]string/map[string]string/map[string]int/[]ComplianceResult/[]AutomationAction/[]InvoiceItem/TenantQuota/ResourceUsage 等）均用 JSON TEXT 列；序列化用 json.Marshal，反序列化用 json.Unmarshal；空值存空串 `""`，读取时空串跳过 Unmarshal 得零值。附录 B 汇总完整 |
| 8 | 可空字段处理 | ✅ | *time.Time→DATETIME NULL（resolved_at/started_at/finished_at/ended_at）；api_keys.expires_at/last_used_at 用 DATETIME NULL 表示"永不/未用"，读取 NULL→零值 time.Time{}；用 sql.NullTime 或指针处理。附录 C 汇总完整 |
| 9 | 迁移文件编号 | ✅ | 已有迁移文件 001-009（连续无冲突）。本设计统一走 `initSchema` 幂等建表模式（与 sql_k8s.go 一致，CREATE TABLE IF NOT EXISTS），不新增迁移文件，无编号冲突风险。注：若团队后续决定改走迁移文件模式，应从 010 开始 |
| 10 | StubDomains 清理 | ❌ | **设计文档未提及需从 `stub_guard.go` 的 `StubDomains` 列表移除已实现域**。该列表当前包含全部 15 个域，实现后若不移除，`WarnStubStoreDomains` 会在 SQL 后端启动时继续误报"以下领域未持久化"警告，且 `internal/config` 的 Validate 错误信息维护了同一名单的字面量副本，需同步更新（见 stub_guard.go 第 60-66 行注释） |

### 问题列表

#### P1（阻塞）：ID 生成函数命名/前缀与 memory 实现不一致

设计原则（第 23 行）声明"与 memory 实现共用同名函数（memory 已定义，SQL 层直接调用）"，但各域"实现要点"中存在多处矛盾，分两类：

**P1a — 函数名/前缀均不一致（会导致 ID 格式与 memory 不同，破坏跨后端一致性）：**

| 域 | memory 已有函数（前缀） | 文档新建函数（前缀） | 影响 |
|----|------------------------|---------------------|------|
| Pipeline 模板 | `randPipelineID()`（`pipeline-`） | `randPipelineTemplateID()`（`pipeline-tpl-`） | ID 前缀从 `pipeline-` 变 `pipeline-tpl-` |
| Pipeline 运行 | `randRunID()`（`run-`） | `randPipelineRunID()`（`pipeline-run-`） | ID 前缀从 `run-` 变 `pipeline-run-` |
| Automation 执行 | `randAutomationExecID()`（`exec-`） | `randAutomationExecutionID()`（`auto-exec-`） | 函数名+前缀均变 |
| Automation 规则 | `randAutomationRuleID()`（`rule-`） | `randAutomationRuleID()`（`auto-rule-`） | 同名函数前缀从 `rule-` 变 `auto-rule-`（若"新建"则重复定义编译错误） |

**P1b — 仅函数名不一致（前缀相同，但"新建"会与 memory 同前缀函数并存或重复定义）：**

| 域 | memory 已有函数 | 文档新建函数 | memory 前缀 | 文档前缀 |
|----|----------------|-------------|------------|---------|
| ArgoCD | `randArgoCDID()` | `randArgocdAppID()` | `argocd-` | `argocd-` |
| Compliance | `randComplianceID()` | `randComplianceReportID()` | `compliance-` | `compliance-` |

**P1c — memory 已有同名函数，文档却写"新建"（会导致重复定义编译错误）：**

| 域 | memory 已有函数 | 文档"新建"函数 |
|----|----------------|---------------|
| Backup | `randBackupID()` | `randBackupID()` |
| Webhook | `randWebhookID()` / `randWebhookDeliveryID()` | `randWebhookID()` / `randWebhookDeliveryID()` |
| Tenant | `randTenantID()` | `randTenantID()` |
| Plugin | `randPluginID()` | `randPluginID()` |

**修复建议**：严格遵守设计原则第 23 行"复用 memory 已定义同名函数"，SQL 层直接调用 memory 已有的 `rand<Domain>ID()`，不新建、不改名、不改前缀。具体：
- ArgoCD：复用 `randArgoCDID()`
- Pipeline：复用 `randPipelineID()` + `randRunID()`
- Compliance：复用 `randComplianceID()`
- Automation：复用 `randAutomationRuleID()` + `randAutomationExecID()`
- Backup/Webhook/Tenant/Plugin：复用 memory 已有同名函数，删除文档中"新建"措辞

#### P2（阻塞）：未提及 StubDomains 清理

`stub_guard.go` 的 `StubDomains` 列表（第 62-66 行）硬编码了 15 个域，`WarnStubStoreDomains`（第 71 行）在 SQL 后端启动时据此输出"未持久化"警告。实现真实 SQL 后若不移除已实现域，会继续误报警告，误导运维判断。此外 `internal/config` 的 Validate 错误信息维护了同一名单的字面量副本（stub_guard.go 第 65 行注释明确指出"两处新增领域时须同步更新"），需同步清理。

**修复建议**：在附录 E（实现顺序建议）或设计原则中补充一节"StubDomains 清理"，明确要求：每域实现完成后，从 `stub_guard.go` 的 `StubDomains` 列表移除该域字符串，并同步更新 `internal/config` 中 Validate 错误信息的字面量副本。

### 非阻塞性观察（建议但非必须）

1. **network_metrics 无 tenant_id 索引**：表含 tenant_id 列但仅按 `(device_id, timestamp)` 索引。当前查询路径（GetNetworkMetrics 按 device_id）不受影响，但若未来新增按 tenant_id 的统计查询需补索引。可在审核意见中注明，不阻塞。
2. **initSchema vs 迁移文件**：本设计统一走 initSchema 幂等建表（与 sql_k8s.go 一致），不走迁移文件。这与 sql_secret.go 走 `007_p03_secrets.sql` 迁移文件的模式不同。两种模式在项目中并存，本设计选择 initSchema 是可接受的，但建议在文档中明确说明这一选择及理由（如"15 张表一次性建表，initSchema 更简洁"）。

### 修改建议汇总

1. **[必须]** 修正全部 10 处 ID 生成函数命名/前缀，统一复用 memory 已有函数（见 P1 修复建议）
2. **[必须]** 补充"StubDomains 清理"章节，说明每域实现后从 `stub_guard.go` 的 `StubDomains` 列表及 `internal/config` Validate 副本移除该域（见 P2 修复建议）
3. **[建议]** 在设计原则或附录中注明走 initSchema 而非迁移文件的选择理由
4. **[建议]** 注明 network_metrics 无 tenant_id 索引的设计考量

---

## 审核结论：不通过

P1（ID 生成函数命名/前缀不一致）和 P2（StubDomains 清理遗漏）两类阻塞性问题已修复，审核通过。其余 8 项（表结构、SQL 注入防护、租户隔离、索引设计、类型映射、JSON 列处理、可空字段处理、迁移文件编号）均通过。