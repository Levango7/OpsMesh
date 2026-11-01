# 语义疑点裁决单（2026-10-30）

- **基线**：`91cfcb2`（main，2026-10-30）
- **作者线**：workbuddy；**性质**：只读取证 + 裁决材料，未改任何生产代码
- **状态**：待 owner 裁决；裁决前不投板、不动代码
- **范围**：四处此前只登记未处置的语义疑点
  - ① k8s 生命周期（升级 / 回滚 / 证书轮换）是纯内存占位
  - ② device-svc 软删在 gRPC 面不可见（且可被复活）
  - ③ alert-svc 文档低估 HTTP 写面
  - ④ portal-svc 的 JSON tag `storage_gd`
- **不在本单**：alert-svc 值班返回不确定（`GetOnCallForTenant` 遍历 map）已在 INBOX-KILO 单独登记；本单不重复计。

---

## 0. 结论先行

| # | 疑点 | 事实定性 | 严重度 | 建议动作 | 预估 |
|---|---|---|---|---|---|
| ① | k8s 升级 / 回滚 / 证书轮换 | `executeUpgrade/executeRollback/executeRotation` 无任何集群操作，状态恒推进到 `Completed 100%`；同组 9 条路由无鉴权闸 | 高 | 先补鉴权（B），再按产品意愿「显式 501」（A）或「真实现」（C） | B 0.5–1d；A 0.5d；C 5–10d |
| ② | device-svc 软删 | `Retired` 只在内部模型；proto 无该字段、删除也不改 `status`；`UpdateDevice` 可把退役设备写回 `retired=false` | 中 | 先防复活（C），再扩 proto（A）/ 复用 status（B） | C 0.5d；A 1–2d；B 0.5d |
| ③ | alert-svc 文档 | `docs/operations.md:1196` 称「只有四个 GET」，实际 policies / schedules 接受 POST（201），且 HTTP 链无认证 | 中 | 文档纠正（A）；按需加只读开关（C）/ 鉴权（B） | A 0.1d；C 0.5d；B 0.5–1d |
| ④ | portal-svc tag | `storage_gd` / `used_storage_gd`；`QuotaUsage` 确实被直接序列化到 `/quotas/{id}/usage` | 低-中 | 改回 `*_gb` 并补键集合测试 | 0.1d |

### 0.1 影响面矩阵

| 疑点 | 用户面 | API / 契约 | 运维 / 安全 | 数据 |
|---|---|---|---|---|
| ① | UI 有「升级 / 轮换证书」按钮且无任何提示，结果假成功 | 202 + 状态恒 Completed；无文档条目 | 9 条路由（含 `etcd-restore`、`node-scale` 真副作用）无鉴权闸 | 状态仅在内存，重启即丢 |
| ② | 走 controlplane 代理的是 device-svc **REST** 网关（`server_lifecycle.go:149-153`），该面已正确暴露 `retired` | gRPC 契约无退役信号；`ListDevices` 又过滤掉它 | — | `UpdateDevice` 覆盖写会把 `retired` 改回 false（MySQL `:231`） |
| ③ | 无前端消费（控制面另有 `/api/v1/oncall` 任务语义） | 文档与实现不一致 | 写面依赖回环绑定，跨机暴露即匿名写 | 策略 / 排班在内存（`NewEscalator` 无 store 参数） |
| ④ | 仓内无消费方（portal 域默认自环拦截） | `GET /quotas/{id}/usage` 键名确为 `used_storage_gd` | — | 数据本身正确，仅键名 |

### 0.2 与既有台账 / 发布说明的口径

- `docs/tech-debt.md` 中 **0 处** `k8slifecycle`（`grep -c` 实测）⇒ ① 从未进债务台账；建议裁决后登记为 TD。
- `docs/release-notes.md:23` 的 v0.16.0 能力增量只写「K8s 一键部署」，未点名升级 / 回滚 / 证书轮换；同节 `:27-28` 有免责句「未宣称的功能不要按已交付用」。⇒ 口径缺口在 **UI 与接口注释**，发布说明本身未超卖。
- **对本线旧登记的更正**：`docs/CLAIMS.md:228` 的 NOTE 写「同一文件里 Upgrade/EtcdBackup/NodeScale 是真执行」——其中 **`Upgrade` 不成立**（`executeUpgrade` 的四个子步骤只打日志），`EtcdBackup`/`EtcdRestore`/`NodeScale` 仍为真执行。收回原因：当时只核了 `Upgrade` 外层与 `PlanUpgrade`，没有展开 `handler.go:170-191` 的四个子函数。

---

## 1. k8s 生命周期：升级 / 回滚 / 证书轮换是纯内存占位

### 1.1 现状与证据

**三个动作都会立即 202，后台 goroutine 只推进内存状态：**

| 动作 | 入口 | 执行体 | 实际行为 |
|---|---|---|---|
| 升级 | `internal/k8slifecycle/handler.go:107-160` | `:142-168` `executeUpgrade` | 调 4 个子步骤；子步骤 `:170-191` 各只有一行 `logx.Info` + `return nil` |
| 回滚 | `:239-271` `Rollback` | `:273-289` `executeRollback` | 只 `updateStatus`（50/80/100），注释列出的 5 步未实现 |
| 证书轮换 | `:321-351` `RotateCertificates` | `:353-366` `executeRotation` | 只 `updateStatus`（10/50/80/100） |

- 状态只存在内存 map：`handler.go:58`（`upgrades`）、`handler.go:309`（`status`）；无持久化、无集群回读，重启即丢。
- 同包的对照组是**真执行**：`PlanUpgrade` `:70-96`（SSH `kubeadm upgrade plan`）、`EtcdManager.Backup` `:502-547`、`Restore` `:548-...`、`NodeScaler.AddNodes/RemoveNodes` `:411-472`。

**鉴权（新增事实，此前未登记）：这 9 条路由没有鉴权闸。**

- `handler.go:871-880` 直接 `mux.HandleFunc` 挂 9 条路由；包内 `grep requirePermission|requireTenantContext` 无命中。
- controlplane 只给 cmdb/log/deploy/orch 注入统一鉴权回调：`internal/controlplane/server.go:549-552`；挂载点 `internal/controlplane/server_lifecycle.go:111` 不在其中。
- 全局 HTTP 链（`server_lifecycle.go:485-492`）只有 CORS / metrics / recovery / OTel / 限流 / 安全头 / CSRF；认证是逐 handler 实现的（`internal/controlplane/auth.go:103`）。同仓其它 k8s 端点都自检（`internal/controlplane/k8s_cluster.go:135/160/276`），本包没有。
- 结论限定：以上是**进程内**事实；若企业部署在反代侧统一鉴权，暴露面取决于反代（已列入未核清单 #1）。

### 1.2 复现命令

```bash
# 占位证据：四个子步骤只剩日志
sed -n '142,191p' internal/k8slifecycle/handler.go

# 鉴权证据：第一条期望无输出；第二条只应命中 4 个子包
grep -rn "requirePermission\|requireTenantContext" internal/k8slifecycle/
grep -n "Authorize = s.subsystemAuthorize" internal/controlplane/server.go

# 现场复现（起一个控制面后；不需要 SSH，因为根本没有拨号）
curl -s -X POST localhost:8080/api/v1/k8s/lifecycle/upgrade \
  -d '{"clusterId":"c1","targetVersion":"1.30.0"}'
curl -s 'localhost:8080/api/v1/k8s/lifecycle/upgrade/status?upgradeID=c1-upgrade-<启动时写入的时间戳>'
# 期望：202 → phase=Completed / progress=100 / currentStep=升级完成，集群实际无任何变化
```

### 1.3 方案对比

| 方案 | 改动点 | 爆炸半径 | 测试面 | 备注 |
|---|---|---|---|---|
| **A. 显式未实现** | 三个入口返回 `501`（或 202+`phase=Unimplemented`）；`K8sLifecycleView.vue:32/82` 置灰并加提示；i18n | 小 | 现有 202 断言改 501；UI 断言按钮禁用 | 最快消除假成功 |
| **B. 补鉴权** | 给 9 条路由注入授权（建议 `k8s:read` / `k8s:write`；`etcd-restore` 可单列更高权限）。可复用 cmdb 等包的 `Authorize` 字段模式 | 中（跨包签名） | 补 401/403；现有用例带 token | 与 `k8s_cluster.go` 同范式；前端 `api/k8s.js:257-259` 已带 token |
| **C. 真实现** | `executeUpgrade` 接 SSH `kubeadm upgrade`（复用 `PlanUpgrade` 基础设施）；`executeRollback` 用 etcd 快照；`executeRotation` 用 `kubeadm certs renew`；补状态持久化与失败回滚 | 大 | 需 SSH 替身 + DSN/集群集成用例；状态持久化用例 | 产品化路径，不建议顺手做 |

**建议顺序：B → A → C。**

### 1.4 验收判据（修好后必须钉的测试）

- B：无 token POST → 401；viewer 角色 POST → 403；admin → 2xx；GET status 的读权限同理。
- A：三个入口返回 501（或 `phase=Unimplemented`）；UI 按钮 `disabled` 且文案含「未实现」。
- C：一次真实（或替身）升级后，状态与集群实际版本一致；进程重启后状态可查（持久化用例）。

### 1.5 需要 owner 回答

1. 升级 / 回滚 / 证书轮换是否属于当前版本对外承诺？若否，是否接受「501 + UI 置灰」？
2. 权限点复用 `k8s:write` 是否够？`etcd-restore` 是否需要单独权限（破坏性最大）？
3. 状态是否要求重启后可查（需要持久化）？

---

## 2. device-svc：软删在 gRPC 面不可见，且 `UpdateDevice` 可复活设备

### 2.1 现状与证据

| 面 | 行为 | 证据 |
|---|---|---|
| 软删实现 | 置 `Retired=true`，不删行 | `store/store.go:187-198`；`store/mysql.go:250-260` |
| 列表 | 过滤退役 | `store/store.go:150-152`；`store/mysql.go:178` |
| 单查 | 原样返回（含退役） | `store/store.go:133-142` |
| gRPC 映射 | 无 `retired` 字段 | `service/service.go:483-489`；`service/service.go:841-859` |
| proto | `Device` 无 `retired`（`:79` 的 `status` 注释却写了 `retired` 取值） | `services/device-svc/api/proto/v1/device.proto:71-87` |
| HTTP 网关 | 正确：删除返回 `{"status":"retired"}`，GET 暴露 `retired=true` | `internal/http/gateway.go:210-217`；`gateway_test.go:118-144` |
| 复活路径 | `UpdateDevice` → `protoToDevice`（无 Retired ⇒ 恒 false）→ 整对象覆盖 | `service/service.go:502-513`、`:818-838`；`store/store.go:172-185`；`mysql.go:231` |

**生产消费面**：controlplane 走的是 device-svc 的 REST 网关（`server_lifecycle.go:149-153` 的 `/api/v1/device-svc/*` 双轨前缀），该面行为正确；不一致只在 gRPC 面。gRPC 在本仓无直接调用方（`grep` 仅测试），但属对外服务契约。

### 2.2 复现命令

```bash
grep -n "retired" services/device-svc/api/proto/v1/device.proto     # 期望无输出
sed -n '187,198p' services/device-svc/internal/store/store.go       # 软删只置 Retired
sed -n '502,513p;818,838p' services/device-svc/internal/service/service.go

# 行为复现（bufconn 或直连 gRPC）：
# 1) DeleteDevice → GetDevice：status 仍为 online/offline，响应无退役信号
# 2) UpdateDevice(仅改名) → ListDevices：设备重新出现（MySQL 后端 retired 被写回 false）
```

### 2.3 方案对比

| 方案 | 改动点 | 爆炸半径 | 测试面 |
|---|---|---|---|
| **A. proto 加 `bool retired = 16`** | `device.proto` + 重新生成；`deviceToProto` 填值；`DeleteDevice` 同时置 `status=retired` | 中（proto 兼容；客户端需重生成） | proto 往返、删除后 Get 断言、List 不含 |
| **B. 只改语义** | `DeleteDevice` 置 `status="retired"`，gRPC 面通过既有 status 表达 | 小 | 删除后 Get 的 status 断言 |
| **C. 防复活（止血）** | `store.UpdateDevice` 对 `Retired` 记录拒绝覆盖（或强制保留 `retired=true`） | 小 | 退役后 Update 再 List 仍不出现 |

**建议：C 先做；A 视 proto 版本策略，否则 B。**

### 2.4 验收判据

- C：退役 → `UpdateDevice` → `ListDevices` 仍不含该设备；`Device(id)` 仍 `retired=true`（若选 A）。
- A：`DeleteDevice` 后 `GetDevice` 返回 `retired=true` 且 `status=retired`；`device.proto` 字段号固定为 16。
- B：删除后 `GetDevice.status=="retired"`，且与 HTTP 网关的 `retired` 布尔语义一致（需文档写明双轨）。

### 2.5 需要 owner 回答

1. proto 是否允许加字段（服务 / 客户端兼容策略）？
2. 退役设备是否允许被 `UpdateDevice` 修改（例如改名归档）？若允许，哪些字段可改？
3. `status=retired` 与 `retired=true` 双轨是否接受，还是只保留其一？

---

## 3. alert-svc：文档低估 HTTP 写面（「四个 GET」实为两 GET + 两 POST，且无鉴权）

### 3.1 现状与证据

- 文档：`docs/operations.md:1196-1197` —— 「`ack`/`resolve` 在服务侧没有 REST 路由（**HTTP 面只有 escalation/oncall 四个 GET**）」。
- 实际路由（`services/alert-svc/internal/server/escalation_http.go`）：

| 路径 | 方法 | 行为 | 证据 |
|---|---|---|---|
| `/api/v1/escalation/policies` | GET / **POST** | 列表 / 创建策略，成功 `201 Created` | `:23-27` 注册；`:30-39` 分派；`:41-54` `createPolicy`（201 在 `:54`） |
| `/api/v1/escalation/active` | GET | 活跃升级列表 | `:66-75` |
| `/api/v1/oncall/schedules` | GET / **POST** | 列表 / 创建排班，成功 `201 Created` | `:78-87` 分派；`:89-102` `createSchedule`（201 在 `:102`）；`:105-112` `listSchedules` |
| `/api/v1/oncall/current` | GET | 当前值班 | `:114+` |

- 鉴权：`services/alert-svc/cmd/alert-svc/main.go:166-176` 的链路只有 metrics / 安全头 / 连接与体积限制 / IP+用户限流 / ratelimit / compress / CORS / trace，**无认证**。`ALERT_SVC_GRPC_TOKEN` 只保护 gRPC。
- 绑定：`deploy/docker/docker-compose.prod.yml:752`（HTTP）绑 `127.0.0.1:${ALERT_SVC_HTTP_PORT:-8103}`；`:759-763` 的注释引导「跨机暴露请配 token 或在反代侧限制」，但该 token 只对 gRPC 生效（`:764` 是 gRPC 端口）。
- 仓内调用方：controlplane 无 `/api/v1/escalation/*`、`/api/v1/oncall/*` 的转发规则（`internal/controlplane/oncall_ops.go:11-53` 是控制面自己的任务下发语义，不是这个面）。

### 3.2 复现命令

```bash
sed -n '1194,1197p' docs/operations.md

# 无 Authorization 也能创建（期望 201；本机直连）
curl -si -X POST localhost:8103/api/v1/escalation/policies -d '{"name":"probe"}' | head -3
curl -si -X POST localhost:8103/api/v1/oncall/schedules  -d '{"name":"probe"}' | head -3

# 链路里没有认证中间件
grep -n "metrics.HTTPMiddleware\|Authorization\|Bearer" services/alert-svc/cmd/alert-svc/main.go
```

（若本机未起 alert-svc，前两条可跳过；`:23-27/:30-39/:78-87` 与 `main.go:166-176` 的静态证据已足够定性。）

### 3.3 方案对比

| 方案 | 改动点 | 爆炸半径 | 测试面 |
|---|---|---|---|
| **A. 文档纠正** | `operations.md:1196` 改为「两条列表 GET + 两条创建 POST；写面无认证，依赖回环绑定」 | 0（纯文档） | 无 |
| **B. 写面鉴权** | 新增共享密钥（如 `ALERT_SVC_HTTP_TOKEN`），仅对 POST 生效；或统一 handler 前置认证 | 中 | POST 无 token 401 / 有 token 201 |
| **C. 只读开关** | 默认拒绝 POST，显式 `ALERT_SVC_ALLOW_WRITES=true` 才放开 | 小 | 默认 405/403；开开关后 201 |

**建议：A 立即；C 作为默认安全姿态；B 仅在该面确定跨机暴露时再做。**

### 3.4 验收判据

- A：文档描述与 `escalation_http.go` 的方法矩阵逐条一致。
- C：默认 POST 拒绝；开关打开后 201。
- B：无 token 401、错误 token 401、正确 token 201。

### 3.5 需要 owner 回答

1. 这两个 POST 是否有产品 / 外部调用方？（仓内检索不到）
2. 是否接受默认只读（C），还是维持匿名写面、仅靠回环绑定？
3. 文档口径以「四个 GET」为准还是以实际路由为准（即 POST 是否本就不该存在）？

---

## 4. portal-svc：`ResourceRequest` / `QuotaUsage` 的 JSON tag 写成 `storage_gd`

### 4.1 现状与证据

- `services/portal-svc/internal/models/models.go:38` `StorageGB` → `json:"storage_gd"`；`:61` `UsedStorageGB` → `json:"used_storage_gd"`。
- 同包其余字段都是 `*_gb`：`:37` `memory_gb`、`:52` `max_storage_gb`、`:60` `used_memory_gb`。
- 输入侧与 DB 列都是 `storage_gb`：`internal/handler/handler.go:168`、`:343`；MySQL 列 `storage_gb`。
- **`QuotaUsage` 会被直接序列化**：`handler.go:588-597`，`GET /api/v1/quotas/{tenantID}/usage` → `writeJSON(w, 200, usage)`（`service.go:323` 返回 `*models.QuotaUsage`）。⇒ 响应键确为 `used_storage_gd`。
- 请求列表走 `requestView` DTO（`handler.go:134-171`），`storage_gb` 正确 ⇒ `ResourceRequest` 的错 tag 目前不漏到响应，但任何「直接返回模型」的新端点都会带出去。
- **旧注释需同步纠正**：`models_test.go:74-77` 写「当前模型不直接序列化给前端（handler 有自有 DTO）」——对 `ResourceRequest` 成立，对 `QuotaUsage` 不成立；该注释与「本测试不钉这两个键」的理由都要随修复一起更新。
- 消费面：`web/enterprise/src` 检索不到 `storage_gd` / `storage_gb`；portal 域默认被自环拦截（`internal/controlplane/service_proxy.go:62-63`，测试 `service_proxy_routing_test.go:150` 用 `/api/v1/portal/quotas` 佐证前缀）⇒ 当前无用户面影响。

### 4.2 复现命令

```bash
grep -n "storage_gd" services/portal-svc/internal/models/models.go

# 起 portal-svc（或经控制面 /api/v1/portal 前缀）后：
curl -s localhost:8080/api/v1/portal/quotas/default/usage \
  | python -c "import sys,json;print(sorted(json.load(sys.stdin)))"
# 期望键集合含 'used_storage_gd'（同响应里其它键是 used_memory_gb / used_cpu …）
```

### 4.3 方案对比

| 方案 | 改动点 | 爆炸半径 | 测试面 |
|---|---|---|---|
| **A. 改回 `*_gb`** | `models.go:38/61` 两行；更新 `models_test.go:74-77` 注释并钉键 | 小（若外部客户端已按 `*_gd` 解析会破，仓内无证据） | `TestResourceRequestJSONContract` / QuotaUsage 键集合断言 |
| **B. 保留并文档化** | 在 API 文档写明键名异常 | 0 | 无 |

**建议：A。**

### 4.4 验收判据

- 模型键集合测试钉住 `storage_gb` / `used_storage_gb`；
- `GET /quotas/{id}/usage` 响应用例断言键名（handler 级 httptest）；
- `models_test.go` 的旧注释改写为「ResourceRequest 走 DTO；QuotaUsage 直出，必须钉键」。

### 4.5 需要 owner 回答

1. 是否有外部调用方已依赖 `*_gd`？（若无，直接改）
2. 是否同意补「模型键集合」测试，防止再出现 `_gd/_gb` 混用？

---

## 5. 建议批次与工单拆分

| 批次 | 内容 | 依赖 | 说明 |
|---|---|---|---|
| **P0（止血，可并行）** | ①B 生命周期鉴权；②C 防复活；③A 文档纠正；④A tag 改名 | 无 | 四条互相独立，可拆四个小 PR |
| **P1（诚实化）** | ①A 501 + UI 置灰；②A 或 B 退役语义 | ①B 合入后 | 与产品确认「是否本版承诺」后执行 |
| **P2（产品化）** | ①C 真实现（升级 / 回滚 / 轮换 + 状态持久化） | P1 | 独立立项；先补 SSH 替身测试与失败回滚设计 |

**不建议在当前迭代做 ①C**：同一文件的 `PlanUpgrade`/`EtcdBackup`/`NodeScale` 已有真实 SSH 通道，真实现属于「补齐而不是新造」，但需要异步作业持久化与失败语义设计。

---

## 6. 裁决回执模板（owner 填）

```
① k8s 生命周期：☐ A(501) ☐ B(鉴权) ☐ C(真实现) ☐ 维持现状并文档标注；归属线：____；目标版本：____
② device 退役：  ☐ A(proto) ☐ B(status) ☐ C(防复活) ☐ 组合：____；归属线：____；目标版本：____
③ alert-svc：    ☐ A(文档) ☐ B(写面鉴权) ☐ C(只读开关)；归属线：____；目标版本：____
④ portal tag：   ☐ A(改名) ☐ B(保留)；归属线：____；目标版本：____
其他指示：____
```

---

## 7. 未核清单（本单明确未取证的部分）

1. **部署侧反代**：k8s 生命周期与 alert-svc HTTP 写面在企业部署（helm / gitops）里是否被反代鉴权挡住——只核了进程内链路与 compose 回环绑定。
2. **k8s 生命周期端点的外部调用方**：除仓内 UI 外是否有脚本 / 文档示例调用（`docs/` 无条目，未查外部仓库）。
3. **portal-svc 的接通状态**：只核了 compose 与 `service_proxy.go` 的自环拦截说明，未逐环境核对 helm values 的 `portal` 后端地址。
4. **device 复活是否已发生**：未查任何环境日志 / 审计库，只确认代码路径存在。
5. **alert-svc 排班 / 策略的持久化**：只看到 `NewEscalator()` 无 store 参数（`main.go:99`），未展开 `internal/escalation` 的存储实现（相关但不在本单）。

## 8. 边界

- 本单为只读取证材料，**未修改任何代码、配置或文档**（本文件除外），**未提交时也不代表已投板**。
- 四条均不属本线长期 CLAIM 的范围：`internal/k8slifecycle` 及其挂载点涉及 `internal/controlplane`（kilo 领地边界）；`services/device-svc`、`services/alert-svc`、`services/portal-svc` 当前无人长期 CLAIM。任何改动都需 owner 先裁定归属线。

---

## 9. 现状复核回写（2026-10-30，qoder 线；只加本节，不改上文任何结论）

按「过时卡点要整条重测」逐条 grep/read 现值，四条的状态与本单成文时已不同：

| # | 疑点 | 现在的实测状态 | 证据 |
|---|---|---|---|
| ① | k8s 升级 / 回滚 / 证书轮换占位 | **仍在**：`upgradeControlPlane` / `upgradeRemainingCP` / `upgradeWorkers` / `verifyCluster` 四个子步骤各只有 `logx.Info` + `return nil`；`executeRollback` / `executeRotation` 只推 `updateStatus` | `internal/k8slifecycle/handler.go:171-191`、`:274-284`、`:353-366` |
| ② | device-svc 软删可被复活 | **已修**：`UpdateDevice` 的 UPDATE 带 `AND retired = false`，注释写明「避免更新载荷里的 retired=false 把它复活」⇒ 本单建议动作的 C 项过期，只剩 A（扩 proto）/B（复用 status）待裁 | `services/device-svc/internal/store/mysql.go` |
| ③ | alert-svc 文档低估 HTTP 写面 | **已修**（A 项：文档纠正）：方法矩阵改为 policies/schedules 接受 GET+POST(201)、active/current 仅 GET，并补「两条 POST 无认证、出厂只绑 127.0.0.1」的口径 | `97f13da`（改 `docs/operations.md`） |
| ④ | portal-svc JSON tag 笔误 | **已修**：`storage_gd`/`used_storage_gd` → `*_gb`，并把它当成**对外契约**钉住（`QuotaUsage` 由 `handler.go:591` 的 `writeJSON` 直接序列化到 `GET /quotas/{id}/usage`），反向断言旧键不再出现 | `820598b`；窄变异：改回旧键 ⇒ 两个键集合用例双双判红 |

补一条本单未取证的**新事实**（直接影响 ① 的严重度判断）：**假成功是用户可见的**——`web/enterprise/src/views/K8sLifecycleView.vue:175` 真的在调 `upgradeK8sCluster(...)`（api 绑定在 `web/enterprise/src/api/k8s.js:258`），点了就拿到 202 + 一路推进到 `Completed 100%` 的内存状态。本单 §0.1 的「UI 有按钮且无任何提示」由此从推断变成有调用点的事实。

给 ① 的成本判据（供 owner 裁 A/C 时参考，本线不动手）：C 不是从零造 SSH——同包里 `PlanUpgrade`（`sshPool.Get` + `ssh.Exec` 跑 `kubeadm upgrade plan`）、`EtcdManager.Backup/Restore`、`NodeScaler.AddNodes/RemoveNodes` 都是**已在的真执行件**；缺的是把这些串成升级/回滚/轮换序列 + 护栏（drain、超时、失败即停）+ 真集群验证窗口。所以差异主要在「要不要现在承担改动生产集群的风险」，而不是「有没有底座」。

## 10. 裁决落地回写（2026-10-30，qoder 线；只追加，不改 §9 结论）

owner 裁决：① 选 **A+C 立项**（A 立即交付，C 立项目标排期）。

- **A 已交付**（`a554697`）：三条假成功入口改为显式 501（`code=ERR_NOT_IMPLEMENTED`），
  删除 4 个只打日志的升级子步骤与两个纯内存 `execute*`；前端升级/证书轮换按钮置灰并给出
  说明；`upgrade/plan`、etcd 备份/恢复、节点扩缩容入口不变（后两者本单已证是真执行）。
  证据：`internal/k8slifecycle/handler.go:473/541/558`、`handler_test.go` 的
  `TestUnimplementedActionsReturn501`；窄变异三条（501→202 / code 改名 / 临时登记作业）各自判红。
- **C 已立项**：`docs/design/k8s-lifecycle-execution-charter.md` —— M1 升级、M2 回滚、
  M3 证书轮换 + `lifecycle_jobs` 持久化 + 护栏与真集群验收，8–12 人日；同时登记了
  现状里的三个真风险（`joinCmd` 请求体直传执行、etcd 恢复无确认无快照校验、作业状态不持久化）。
- 遗留：`docs/tech-debt.md` 仍未登记 ①（本单 §0 指出 0 处命中）；TD 编号留给台账 owner，本线不自行占号。
