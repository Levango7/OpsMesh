# API 稳定性与版本策略（v1 默认形态冻结）

> **状态：生效中（2026-11-18 拍板）**
> **适用范围**：OpsMesh 控制面与经网关暴露的 `/api/v1` HTTP JSON API。
> **不适用**：agent gRPC 通道（由 `buf breaking` 守护，见 `proto/buf.yaml`）、
> 内部管理端点、`/metrics`、静态资产。
>
> **一句话原则：v1 的默认响应形态不 breaking；需要改形状就开 v2。**
> `docs/api-pagination.md` 的 opt-in 分页信封是唯一被批准的向前路径，不是过渡期的临时方案。

---

## 1. 决策摘要（保守路径）

| # | 决策 | 说明 |
|---|---|---|
| D1 | **默认形态冻结** | 客户端不携带新参数时看到的 v1 响应（字段名、类型、顶层容器、状态码、错误体）保持不变。 |
| D2 | **opt-in 信封是唯一向前路径** | 既有列表端点通过 `page` / `pageSize` 显式选择分页形态（多数为统一信封；`users`/`tenants` 为旧容器键+元数据例外）；新列表端点直接返回统一信封（`docs/api-pagination.md:54-61`）。 |
| D3 | **新增字段允许，改/删/重命名不允许** | 资源对象可新增字段；顶层容器/键集合不改；删除、重命名、改类型、改语义一律 v2。 |
| D4 | **不强制迁移** | 现有前端、脚本、外部集成方继续按默认形态工作；不要求他们改代码。 |
| D5 | **v2 才做统一** | BB-01 / BB-02 / BB-04 的形状统一、参数词表统一、信封统一，全部留到 v2 决策，不在 v1 内做破坏性收敛。 |
| D6 | **弃用有窗口** | 公告 + `Deprecation` / `Sunset` 响应头；最短 2 个 minor release 或 90 天（取长）后才可移除。 |

---

## 2. 规则

### 2.1 v1 内允许（不升版本）

1. 新增端点（新端点必须遵守 §2.2 的列表信封规则）。
2. 在**资源对象**内新增字段（客户端应忽略未知字段；不改变已有字段的含义）。
3. 新增可选请求参数，且缺省时行为与旧版逐字一致。
4. 放宽校验（如字符串长度上限变大）。
5. 修正错误文案（不改变状态码与 `{"error": ...}` 形状）。
6. 增加响应头（如 `X-Total-Count`、`X-Truncated`、`Deprecation`、`Sunset`），前提是不改变响应体。

### 2.2 新列表端点

1. **必须始终返回统一信封**，不再提供裸数组/自定义容器形态：

   ```json
   {"data": [], "total": 0, "page": 1, "pageSize": 20, "hasMore": false}
   ```

2. `data` 空集必须是 `[]`，不是 `null`。
3. 只接受 `page` / `pageSize`；非法值返回 `400 {"error":"invalid page/pageSize"}`；
   `pageSize` 默认 20、上限 200（`docs/api-pagination.md:35-50`）。
4. 不得模仿 `users` / `tenants` 的「旧容器键 + 分页元数据」历史例外（`docs/api-pagination.md:77-87`）。

### 2.3 v1 内禁止（必须 v2）

| 变更 | 例子 | 结论 |
|---|---|---|
| 删除字段 | 从 `DeviceInfo` 移除 `taskState` | 需 v2 |
| 重命名字段 | `deviceID` → `id` | 需 v2 |
| 改变字段类型 | `total` 从 int 变 string | 需 v2 |
| 改变字段语义 | `status` 枚举值含义变化 | 需 v2 |
| 改变顶层容器 | `GET /devices` 从 segment map 改为 `{data:[...]}` | 需 v2 |
| 改变状态码 | 200 → 204；404 → 410（安全策略收紧 401/403/429 除外，见 §2.5） | 需 v2 |
| 改变错误体 | `{"error":"..."}` → `{"error":{...}}` | 需 v2 |
| 收紧默认行为 | 原本可选的分页参数变为必填 | 需 v2 |
| 改变分页触发条件 | 原本 `page` 才触发信封，改为 `pageSize` 也触发 | 需 v2 |

**v2 标识方式**（二选一，优先路径）：

- 路径版本：`/api/v2/...`（与 `docs/api-specification.md:947-955` 的 URL 路径版本一致）；
- 媒体类型：`Accept: application/vnd.opsmesh.v2+json`，响应 `Content-Type` 对应，
  且必须带 `Vary: Accept`。该方式只用于无法新增路径前缀的聚合端点；**默认请求仍返回 v1 形态**。

v1 与 v2 可同时注册；v2 上线不改变 v1 的默认响应。

### 2.4 默认形态的边界

「默认形态」指**不携带任何新参数**时的响应。对当前列表端点：

- 不传 `page` / `pageSize`（按端点定义）→ 默认形态；
- 传了任一触发参数 → opt-in 信封，属于显式选择，不算 breaking。

**顶层键集合冻结**：不得在默认形态的顶层新增 `data`、`total`、`count`、`items` 等控制字段。
新增元数据只能放在资源对象内部、响应头，或 opt-in 信封中。此规则专门防止 BB-01
「同一端点两种形状、客户端靠探测 `data` 键判断」的歧义回归。

### 2.5 与既有兼容清单的边界

`docs/api-specification.md:959-966` 已列出的 v1 兼容变更继续有效：

- 放宽校验（如长度上限变大）；
- 收紧 401/403/429 等安全策略（更严格）；
- 新增可选请求字段、新增响应字段、新增端点、新增枚举值。

本策略只额外冻结四类**客户端可见的默认形态**：

1. 成功响应的顶层容器与键集合；
2. 成功状态码（2xx 的选择）与正常业务流程的 4xx/5xx 语义；
3. 分页触发条件（哪个参数触发信封）；
4. 错误体形状（`{"error":"<string>"}`）。

安全策略收紧若改变 401/403/429 的触发频率但**不改变响应体形状**，按既有兼容清单执行，
并在 CHANGELOG 公告；若要把错误体改成对象或改变成功响应形态，则必须 v2。

---

## 3. 弃用流程

### 3.1 公告

1. 在 `CHANGELOG.md` 与相关文档标注：弃用对象、替代端点/版本、弃用日期、计划移除日期。
2. 在受影响响应中加：

   ```http
   Deprecation: @1794960000
   Sunset: Fri, 30 Apr 2027 00:00:00 GMT
   Link: <https://<docs-host>/docs/api-versioning-policy.md>; rel="deprecation"; type="text/markdown"
   ```

   - `Deprecation` 按 **RFC 9745** 使用结构化字段时间戳（`@<unix-seconds>`，示例为 2026-11-18）。
     旧写法 `Deprecation: true`（`docs/api-specification.md:986`）不再用于新弃用；
     监控若只判断「头存在」，两种写法都可识别。
   - `Sunset` 按 **RFC 8594** 使用 HTTP-date。
   - `Link` 为可选，用于指向迁移说明。

### 3.2 最短窗口

| 条件 | 要求 |
|---|---|
| 最短公告期 | **2 个 minor release 或 90 天，取较长者** |
| minor release 定义 | `CHANGELOG` 中 `x.Y.0` 的 `Y` 递增（0.x 阶段同样适用） |
| 移除前提 | 替代端点/版本已上线；迁移文档已发布；owner 书面确认 |
| 移除方式 | **端点**：窗口结束后可返回 `410 Gone`；**字段/参数**：不得在 v1 内删除或静默改形，必须在 v2 中移除，v1 原样保留 |
| 例外 | 安全漏洞/数据泄露等紧急情况可缩短窗口，但必须有 owner + 安全负责人签字并记入 CHANGELOG |

> 与既有文档的关系：`docs/api-specification.md:953` 的「v(N) 发布后 v(N-1) 维护 12 个月」
> 适用于**整版本**下线；本节的最短窗口适用于**单个端点/字段/参数**的弃用。
> 两者适用对象不同，不互相替代。

### 3.3 弃用检查清单

- [ ] CHANGELOG 条目（弃用日期 + Sunset 日期 + 替代方案）
- [ ] 文档更新（端点表、迁移示例、错误语义）
- [ ] 响应头 `Deprecation` + `Sunset`（+ 可选 `Link`）
- [ ] 调用量看板（按租户/客户端统计；窗口结束仍有流量则延长）
- [ ] 契约测试：弃用期默认形态仍冻结；端点移除后返回 410；字段/参数移除只随 v2
- [ ] owner 签收

---

## 4. 当前端点与默认形态（契约测试必须钉住）

> 来源：`docs/api-pagination.md:64-87`、`docs/api-list-envelope.md:11-20`、
> `internal/controlplane/server_devices.go:42-51,112-116`、`internal/controlplane/server_tasks.go:307-333`、
> `internal/controlplane/auth_users.go:61-79`。下表是 **v1 冻结清单**；新增列表端点按 §2.2 追加。

| # | 端点 | 默认形态（无分页参数） | 触发 opt-in 的参数 | opt-in 形态 | 现有契约门禁 | 缺口 |
|---:|---|---|---|---|---|---|
| 1 | `GET /api/v1/devices` | 按 segment 分组的对象：`{"seg-a":[DeviceInfo,...],"seg-b":[...]}` | `page` 或 `pageSize` | `{data,total,page,pageSize,hasMore}` | `internal/controlplane/pagination_optin_test.go`（`TestPaginationOptInDevices` 含 `legacy_no_params`） | 无 |
| 2 | `GET /api/v1/tasks` | 裸数组 `[...]`；未分页硬上限 2000，截断时 `X-Truncated: true` + `X-Total-Count` | `page`（`pageSize` 单独存在不触发） | `{data,total,page,pageSize,hasMore}` | `internal/controlplane/list_envelope_contract_test.go`（仅 opt-in + 空集） | **默认裸数组未钉住**，需补 `TestTasksLegacyShape` |
| 3 | `GET /api/v1/alerts` | 裸数组 `[...]`（空列表 `[]`） | `page`（`pageSize` 单独存在不触发） | `{data,total,page,pageSize,hasMore}` | `internal/controlplane/server_alerts_contract_test.go`；`internal/controlplane/coverage_extra_test.go:1146` | 无 |
| 4 | `GET /api/v1/users` | `{"users":[...]}` | `page`（`pageSize` 单独存在不触发） | `{"users":[...],"total":...,"page":...,"pageSize":...,"hasMore":...}`（**无 `data`**） | `internal/controlplane/acceptance_hardening_test.go:291`（分页边界） | 默认 `{users}` 顶层键未单独钉住，建议补精确键集合断言 |
| 5 | `GET /api/v1/tenants` | `{"tenants":[...]}` | `page`（`pageSize` 单独存在不触发） | `{"tenants":[...],"total":...,"page":...,"pageSize":...,"hasMore":...}`（**无 `data`**） | `internal/controlplane/acceptance_hardening_test.go:356` | 同上 |
| 6 | `GET /api/v1/audits` | 裸数组 `[...]`；legacy `limit` 默认 100、上限 1000 | `page` 或 `pageSize` | `{data,total,page,pageSize,hasMore}` | `internal/controlplane/pagination_optin_test.go`（`TestPaginationOptInAudits`） | 无 |
| 7 | `GET /api/v1/webhooks` | `{"webhooks":[...]}` | `page` 或 `pageSize` | `{data,total,page,pageSize,hasMore}` | `internal/controlplane/pagination_optin_test.go`（`TestPaginationOptInWebhooks`） | 无 |
| 8 | `GET /api/v1/scripts` | `{"scripts":[...]}` | `page` 或 `pageSize` | `{data,total,page,pageSize,hasMore}` | `internal/controlplane/pagination_optin_test.go`（`TestPaginationOptInScripts`） | 无 |
| 9 | `GET /api/v1/agents` | 裸数组；元素含 `agentID/hostname/segment/status/lastSeen/live` | `page` | `{data,total,page,pageSize,hasMore}` | `internal/controlplane/server_devices.go:112-116`（实现）；`internal/controlplane/handler_m4_test.go:267`（happy path） | **无默认/信封形状契约测试**，需补 |

### 4.1 契约测试必须断言的项

1. **默认形态的顶层键集合**完全等于上表（多余键、缺失键都判红）。
2. 默认形态**不包含** `data` / `total` / `count` 等信封或控制键（可复用
   `pagination_optin_test.go` 的 `assertLegacyNoEnvelope`）。
3. opt-in 形态的五个键齐全，`data` 为空时为 `[]` 而非 `null`。
4. 非法分页参数返回 `400 {"error":"invalid page/pageSize"}`。
5. 租户隔离：他租户数据不出现在 `data` 或默认列表中。
6. 状态词表：`batch` 等状态的取值集合按各域契约钉住（TD-60 的 T-4 必须由契约测试覆盖；当前尚未闭合）。

### 4.2 新增/修改端点的门禁

- 新增列表端点：必须同时提交 §4.1 的契约测试；未加测试不合并。
- 修改默认形态：PR 必须引用本策略的 v2 条款；否则 CI/评审直接拒绝。
- `make td60-consistency` 只比对 controlplane 与 `services/{task,device,auth}-svc` 的
  路由/信封/状态词表（`scripts/td60-consistency.py:1-35`），**不能替代**字段级契约测试；
  两者都要跑。

---

## 5. 客户端指引

### 5.1 如何选择形态

| 客户端需求 | 做法 |
|---|---|
| 继续用现有默认形态 | 不传 `page` / `pageSize`；按本文 §4 的端点形状解析。**不要**通过探测 `data` 键判断版本。 |
| 需要分页/总数 | 传 `page`（按需带 `pageSize`）。`devices` / `audits` / `webhooks` / `scripts` 任一参数即可触发；`tasks` / `alerts` / `users` / `tenants` 只认 `page`（`docs/api-pagination.md:64-75`）。 |
| 需要任务全量但超过 2000 条 | 默认形态会截断并返回 `X-Truncated: true` + `X-Total-Count`；改用 `?page=1&pageSize=200` 逐页拉取。 |
| 需要 v2 | 请求 `/api/v2/...`，或（仅聚合端点）发送 `Accept: application/vnd.opsmesh.v2+json`。 |

### 5.2 如何检测 v2

1. 先看路径前缀：`/api/v2/` → v2。
2. 否则看请求/响应的媒体类型：`application/vnd.opsmesh.v2+json` → v2。
3. **不要**用「响应里有没有新字段」「有没有 `data` 键」推断版本——这正是 BB-01 的歧义来源。
4. 未知字段一律忽略；未知枚举值按未知处理，不报错（`docs/api-specification.md:959-966`）。

### 5.3 错误与弃用

- 错误体固定为 `{"error":"<string>"}`（BB-08 实测口径；`docs/api-specification.md` 的旧对象 schema 已按实测更正）。
- 收到 `Deprecation` / `Sunset` 头时，按 §3 的窗口安排迁移；不要等到 410。
- 429 无 `Retry-After` 的既有行为不在本策略内变更；客户端应指数退避。

---

## 6. 与现有文档的关系

| 文档 | 关系 |
|---|---|
| `docs/api-pagination.md` | 分页参数、信封字段、opt-in 规则、端点形态表的**权威来源**；本策略引用而不复制其细节。 |
| `docs/api-list-envelope.md` | issue #96 的核验结论；确认 `devices/alerts/tasks/tenants` 的现状与 `tenants` 键例外；本策略把该例外冻结为 v1 契约。 |
| `docs/api-specification.md` §5 | 路径版本（`/api/v1`、`/api/v2`）、兼容变更清单、`buf breaking`、弃用头。本策略与其一致；§3.2 的 90 天窗口是**单端点/字段**的补充，不替代整版本 12 个月维护期。 |
| `docs/qa-sweep-2026-11-06.md` §5.4 | BB-01 / BB-02 / BB-04 / BB-08 的审计证据；本策略是这四项的 owner 裁决。 |
| `docs/td60-consistency-2026-11-17.md` | TD-60 双轨差异；切流期间不得借「统一形状」名义做 v1 breaking 变更。 |

---

## 7. BB-01 / BB-02 / BB-04 裁决记录

| 编号 | 审计原文（摘要） | v1 裁决 | 理由 | 何时重审 |
|---|---|---|---|---|
| **BB-01** | 同一端点按是否传 `page` 返回两种形状（裸数组/分组对象 vs `{data,total,...}`），devices 非翻页是 segment map、翻页变扁平数组（`docs/qa-sweep-2026-11-06.md:217`） | **保留双形态**：默认形态冻结，opt-in 信封冻结；不做「一个端点一种形状」的 v1 改造；不强制客户端迁移；逐端点文档化（§4） | 改成单形态会破坏现有前端/脚本；当前双形态是显式 opt-in，可共存 | 仅在 v2 决策时重审（路径或媒体类型） |
| **BB-02** | 五套分页词汇（`page/pageSize`、`offset/limit`、`limit(100/1000)`、`limit(1000/10000)`、`top`）、`sort/order` 零实现、传错参数名静默忽略（`docs/qa-sweep-2026-11-06.md:218`） | **存量冻结**：既有端点维持现有词汇与静默忽略行为，但在文档中逐条标注；**新端点只用 `page`/`pageSize`**，未知分页参数显式 400 | 统一词汇会改变既有客户端可见行为；新端点可以从一开始就正确 | v2 统一参数词表；或 owner 单独批准兼容窗口 |
| **BB-04** | 列表成功信封四套并存（裸数组 / 领域名键 / `{items}` / `{data,total,...}`），`count` 与 `total` 并存（`docs/qa-sweep-2026-11-06.md:220`） | **存量冻结**：四套家族在 v1 内保留；新端点只用 `{data,total,page,pageSize,hasMore}`；`users`/`tenants` 的旧容器键例外冻结；不强制迁移 | 统一信封需要前端同步 + 破坏性变更；新端点可增量收敛 | v2 决策 |
| **BB-08**（关联） | 文档声称的错误体对象 schema 零实现，实测为 `{"error":"<string>"}`（`docs/qa-sweep-2026-11-06.md:224`） | **冻结字符串错误体**；不引入 `error` 对象/trace_id 对象，除非 v2 | 文档已按实测更正；改错误体会破坏所有错误处理分支 | v2 |

> **明确结论**：BB-01 / BB-02 / BB-04 在 v1 **不做强制迁移**。现有默认形状、参数词表、
> 信封家族全部保留；只通过「新端点用新规则」和「显式 opt-in」向前走。
> 任何要求「统一形状」的改动都必须先有 v2 决策（ADR + owner 签字），不得在 v1 内直接做。

---

## 8. 变更流程（PR 检查清单）

- [ ] 是否改变任何 v1 默认形态？是 → 必须 v2；否 → 继续。
- [ ] 新增列表端点？→ 是否始终返回统一信封、`data:[]` 非 null、非法参数 400。
- [ ] 新增字段？→ 只加在资源对象内；未改变顶层键集合。
- [ ] 弃用？→ CHANGELOG + 文档 + `Deprecation`/`Sunset` 头 + 最短窗口。
- [ ] 契约测试：默认形态精确键集合、opt-in 五键、空集、租户隔离、状态词表。
- [ ] 运行 `go test ./internal/controlplane/ -run 'Pagination|Envelope'` 与
      `make td60-consistency`（涉及 TD-60 三域时）。
- [ ] 文档：`docs/api-pagination.md` 端点表、`docs/api-reference.md`、本策略 §4 同步。

---

## 9. 例外与升级

1. **安全例外**：紧急修复可缩短弃用窗口，但必须由 owner + 安全负责人签字，
   在 CHANGELOG 记录原因与影响面。
2. **v2 立项**：需要单独 ADR（`docs/adr/`），明确范围、迁移期、双版本共存方案与回滚；
   本策略不预设 v2 的时间表。
3. **未覆盖的端点**：其他列表端点按同一原则处理（默认形态冻结、新端点用统一信封）；
   §4 的清单随审计补充，不因未列入而视为可 breaking。

---

## 10. 参考

- `docs/api-pagination.md` —— 分页契约与端点形态表
- `docs/api-list-envelope.md` —— issue #96 信封核验结论
- `docs/api-specification.md` §5 —— 版本管理、兼容变更、弃用
- `docs/qa-sweep-2026-11-06.md` §5.4 —— BB-01/02/04/08 审计证据
- `docs/td60-consistency-2026-11-17.md` —— TD-60 双轨差异与字段级 P0
- RFC 8594（Sunset）、RFC 9745（Deprecation）
