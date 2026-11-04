# 决策材料 2026-11-04：trace 端点接门 / pkg/auth 死代码 / TD-89 / ops 路由 RBAC 缺口

> 性质：**只读取证 + 决策材料**，本线不改上述代码。
> 取证者：workbuddy（用户点名「B/C 无他人认领就认领并行做」）。
> 复核：文末每条都给了 file:line 或可重跑命令；所有结论以 `origin/main`（2026-11-04 拉取，tip `fdcf98d`）为准。

---

## 结论先行

1. **`/api/services`、`/api/traces` 建议「先补认证、暂不建权限族」**：四个端点都是 graceful 空结果，给它们单独立 `observability:read` 的收益要等真实 Jaeger 接入才兑现；短期先用 `requireTenantContext` 去掉匿名面，等 owner 对“observability 域是否立族”拍板后再升级为权限门。
2. **`pkg/auth` 的三条中间件建议全部删除**（`AuthMiddleware` / `RequirePermission` / `ServiceAuthMiddleware`）：生产零调用点，`RequirePermission` 与 controlplane 的 `requireProd` 是同一概念的两份定义（漂移风险）。若将来走服务面严格模式，可从 git 历史恢复，成本低于长期养两份。
3. **B4 补账时发现一个更大的问题**：`bd06090`/`1f9e481` 新增的 **17 条运维路由没有任何权限校验**；同形态在现有 **69 个 `*ops*.go`、301 条路由**里普遍存在。生产 `requireAuth: true` 下这不是“匿名”，而是**任意已登录用户（含 viewer）都能执行系统级运维脚本**（部署软件、drop 表、drain 节点等，经 `dispatchKafkaTask` 下发到 agent）。建议单独立项：先加「ops 路由权限覆盖率不回退」棘轮门禁，再按域补 `requireProd`。
4. **TD-89（Docker Hub 429）仍阻塞在 owner 的镜像源/凭据选择**，技术选项见 §4；本线不做无授权的基础设施变更。

---

## 1. `/api/services`、`/api/traces`（C2）

### 事实

- 路由直接挂在 mux 上，无权限/认证检查（`internal/controlplane/server_lifecycle.go:494-497`）：
  ```
  GET /api/services              → handleTraceServices
  GET /api/services/{svc}/operations
  GET /api/traces                → handleListTraces
  GET /api/traces/{traceID}      → handleTraceRouting
  ```
- 四个 handler 全部只做方法判断并返回空结果（`internal/controlplane/trace.go:74-127`），未调用 `requireTenantContext` / `requirePermission`。
- 全局 handler 链（`server_lifecycle.go:509-517`）只有 CORS/指标/recovery/OTel/限流/安全头/CSRF，**没有统一认证中间件**——认证是各 handler 自己调的。
- 前端 `/trace` 路由已无门（`web/enterprise/src/router/index.js:154-156`，ZCode 2026-11-02 按“后端无守卫”的事实去门）。

### 方案与取舍

| 方案 | 改动 | 收益 | 代价/风险 |
|---|---|---|---|
| A. 只补认证（`requireTenantContext`） | 4 个 handler 各加 3 行 | 去掉匿名面；不动权限目录/前端 | 登录用户均可读；仍是“认证≠授权” |
| B. 立 `observability:read` 并接门 + 前端路由门 | PermSpecs 新增族、operatorReadOnlyGroups、viewer 派生、handler + router | 语义正确，为真实 Jaeger 接入做准备 | 新增权限族属产品决策；前端要同步改门 |
| C. 改显式 501 | handler 返回 501 | 最诚实，杜绝“空结果当成功” | 前端要处理错误态；用户已接受空态 |
| D. 维持现状 | 无 | 零成本 | 匿名 API 面保留 |

**建议**：**先 A、后 B**。A 是纯收口，不需要产品裁决；B 与“observability 权限族”一起裁（当前 A3 待决）。真实数据接入前不改 501（前端已按空态实现）。

---

## 2. `pkg/auth` 三条零调用中间件（C3）

### 事实

- 导出面（`pkg/auth/middleware.go:24/45/64`）：`AuthMiddleware`、`RequirePermission`、`ServiceAuthMiddleware`。
- 生产引用只有 `ValidateServiceToken`（`pkg/tenant/auth_bridge.go`、`services/task-svc/internal/http/gateway.go`）；三条中间件**零调用点**，由 `internal/gates/authz_wiring_gate_test.go:62-64` 豁免台账兜着。
- `RequirePermission(permission string)` 的语义与 controlplane 的 `requireProd(w,r,perm)` 重叠，两份定义并存。

### 方案与取舍

| 方案 | 收益 | 代价 |
|---|---|---|
| 删 3 条（保留 jwt/mtls/grpc） | 消除漂移源；门禁台账减 3 条 | 若服务面严格模式要 HTTP 中间件，需从历史恢复 |
| 只删 `RequirePermission`/`ServiceAuthMiddleware`，留 `AuthMiddleware` | 保留严格模式的现成件 | 留一条死代码，门禁继续豁免 |
| 全部接线 | 能力不闲置 | 需要先有“谁在调服务、用哪种凭据”的产品口径；接线本身不改安全语义 |

**建议**：**删 3 条**。`pkg/auth` 的核心价值（JWT 签发/校验、gRPC 拦截器、mTLS 配置）都还在用或可直接启用；“定义了但没人接”的 HTTP 装配件正是本仓两次栽过的形态，留着只会让下一个人误以为防线已存在。

---

## 3. B4 补账附带发现：ops 路由的 RBAC 缺口（严重）

### 事实（现查）

- `bd06090`（2026-11-03，Elasticsearch 扩展运维：node drain / shard relocate / diagnostic bundle，3 条路由）与 `1f9e481`（同日，云托管 DB 6 条 + NoSQL 5 条 + Apache 数据工程 3 条）新增 **17 条** `POST /api/v1/...` 路由。
- 这 17 条路由直接 `mux.HandleFunc` 注册；对应 handler 内 `requireProd` / `requirePermission` / `authorize(` 命中数为 **0**。它们最终走 `dispatchKafkaTask`（`internal/controlplane/kafka_ops.go:29-40`），而后者只调 `requireTenantContext`，**不做角色/权限校验**。
- 同形态不是孤例：**69 个 `internal/controlplane/*ops*.go` 中 0 处权限检查，合计 301 条 `mux.HandleFunc` 注册**（本轮只统计“文件内 0 命中”，未逐条判定每个 handler 是否真的执行命令）。
- `requireTenantContext` 的行为（`internal/controlplane/http_infra.go:129-196`）：`requireAuth=true` 时要求可验证凭证（否则 401）——**只保证“登录”，不保证“有权限”**；`requireAuth=false` 时，带任意 `X-Tenant-ID` 头即可通过（内网直通，注释自陈）。
- 对照：`internal/controlplane/db_ops.go:169/342/470` 等同族 handler 是**有** `requireProd(w,r,"system:write")` 的，说明“该加而漏加”而非“设计如此”。

### 影响

- 生产（`values-production.yaml:35` `requireAuth: true`）：任意已登录用户（viewer 也满足“已登录”）可调用这些路由，生成并下发任意运维脚本到目标设备；脚本内容还包含把请求参数直接拼进 shell 的写法（如 `db_nosql_ext_ops.go` 的 `hbase shell <<< "create '<table>', '<cf>'"`），存在命令注入面。
- 开发/内网（`requireAuth: false`）：只要带一个 `X-Tenant-ID` 头即可，无需凭证。

### 建议（需 owner/dispatch；本线未改代码）

1. **止血**：给上述 17 条新路由按同族惯例补 `requireProd(w,r,"system:write")`（与 `db_ops.go` 一致），并补 1 条表驱动测试（每个新 handler 无权限时 403）。
2. **防扩散**：加一条结构性门禁——扫描 `*ops*.go` 的 `mux.HandleFunc` 注册与同文件/同 handler 的权限检查，**基线 301 条未覆盖只许减不许增**（豁免按文件记账并写原因）。
3. **根治**：按域决定 viewer/operator 的可执行面（`system:write` / `middleware:write` / `db:write`…），再批量补 `requireProd`；这会改变现有角色的可执行范围，属产品语义，需一张裁决单。

---

## 4. TD-89：CI 依赖 Docker Hub 匿名拉取（B3，阻塞）

### 事实

- 失败发生在**解析基础镜像元数据**阶段（`429`/`5xx`），代码门禁全绿；TD-94 的 `deploy/scripts/docker-pull-retry.sh` 只能预热 service containers，包不住 `docker/build-push-action` 内部的 FROM 解析（`docs/tech-debt.md` TD-94 行）。
- 当前基础镜像：`node:22-alpine`、`golang:1.26-bookworm`、`debian:bookworm-slim`、`alpine:3.23`（`Dockerfile`、`Dockerfile.agent`、`Dockerfile.service`、`deploy/docker/*`）。

### 方案与取舍

| 方案 | 收益 | 代价 |
|---|---|---|
| A. 配 Docker Hub 只读 token（环境已支持 `docker/login-action`） | 提高到认证配额，改动最小 | 需要 owner 提供 secret；此前明确“不配 secrets” |
| B. 基础镜像改走公共镜像源（如 `mirror.gcr.io`） | 不需要凭据，直接绕开 Docker Hub 限额 | 引入第三方镜像源信任与可用性依赖；需逐镜像验证 digest 一致 |
| C. 保持现状 + 失败自动重跑 | 零改动 | 随机红继续，发布线仍可能被外部限流挡住 |
| D. 自建/推送到 GHCR 的镜像缓存 | 供应源可控 | 维护成本最高，需定期同步基础镜像 |

**建议**：**A（首选）→ B（备选）→ C（兜底）**。A 只改 CI 凭据注入，不动 Dockerfile 与供应链；若 owner 仍不愿配 secret，再评估 B。B 必须附 digest 对照（同一 tag 在 Docker Hub 与镜像源的 manifest digest 必须相等），否则不换。

---

## 五、需要 owner 回答 / 决策的问题

1. `/api/services`、`/api/traces`：先 A（只认证）还是直接 B（立 observability 族）？
2. `pkg/auth` 三条：确认删除，还是保留 `AuthMiddleware` 等服务面严格模式？
3. ops 路由 RBAC 缺口：是否按 §3 建议立项（止血 17 条 + 棘轮门禁 + 按域补权限）？谁执行？
4. TD-89：A/B/C/D 选哪条？若选 A，secret 由谁注入？

---

## 六、未核清单

- 301 条 ops 路由未逐条判定“是否真的执行命令/是否有其它隐式守卫”；本材料只确认**文件级 0 权限检查**与**17 条新路由 0 检查**。
- `/api/services`、`/api/traces` 在 `requireAuth=false` 部署下的真实调用方未普查（前端 trace.js 是已知调用方）。
- `pkg/auth` 删除对 `pkg/tenant/auth_bridge.go` 无影响（只依赖 `ValidateServiceToken`），但未跑全仓引用扫描之外的动态装配点（如反射/插件）。
- TD-89 的镜像源方案未做连通性与 digest 实测。

---

## 七、复现命令

```bash
# 17 条新路由：0 权限检查
git show 1f9e481:internal/controlplane/db_nosql_ext_ops.go | grep -c "requireProd\|requirePermission\|authorize("
git show bd06090:internal/controlplane/elasticsearch_ops_ext.go | grep -c "requireProd\|requirePermission\|authorize("
# 家族面：69 文件 / 301 路由
grep -c "requireProd\|requirePermission" internal/controlplane/*ops*.go | awk -F: '$2==0' | wc -l
# trace 端点无门
grep -n "api/services\|api/traces" internal/controlplane/server_lifecycle.go
# pkg/auth 零调用
grep -rn "auth.AuthMiddleware\|auth.RequirePermission\|auth.ServiceAuthMiddleware" --include=*.go . | grep -v "pkg/auth/" | grep -v _test
```
