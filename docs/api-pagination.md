# API 列表分页契约

> 状态：生效中（2026-11）
> 适用范围：控制面 `internal/controlplane` 下所有 `GET` 列表端点
> 实现依据：`internal/controlplane/paginate`（`ParsePaginationOptIn` / `ParsePagination` / `PaginateSlice`）

本文件是外部集成方读取列表型 API 的唯一分页说明。所有列表端点在**未携带分页参数**时保持历史响应形态（逐字段兼容，前端与既有脚本不受影响）；携带分页参数时返回统一信封。

---

## 1. 统一分页信封

```json
{
  "data": [ ... ],
  "total": 137,
  "page": 2,
  "pageSize": 20,
  "hasMore": true
}
```

| 字段 | 类型 | 语义 |
| --- | --- | --- |
| `data` | array | 当前页数据。越界页为 `[]`（**不是** `null`）。 |
| `total` | int | 切片前、按当前租户（及端点自身过滤条件）过滤后的总行数。 |
| `page` | int | 当前页码，1 起始（归一后的实际值）。 |
| `pageSize` | int | 实际页大小（归一/封顶后的值）。 |
| `hasMore` | bool | `page * pageSize < total`。 |

`data` 中每个元素的字段与该端点旧形态中的元素完全一致，不额外包裹。

---

## 2. 请求参数

| 参数 | 默认 | 归一规则 |
| --- | --- | --- |
| `page` | `1` | `< 1` 一律按 `1` 处理；极大值按 `maxInt/pageSize` 封顶以防整数溢出。 |
| `pageSize` | `20` | `< 1` 回退默认 `20`；`> 200` 封顶 `200`（与 `paginate.ParsePagination` 行为一致）。 |

### 错误语义

| 情况 | 状态码 | 响应体 |
| --- | --- | --- |
| `page` 或 `pageSize` 非整数（如 `abc`、`1.5`、空串以外的非法值） | `400` | `{"error":"invalid page/pageSize"}` |
| 认证 / 权限 / 租户校验失败 | `401` / `403` | 沿用各端点既有错误信封，分页逻辑不改变鉴权顺序 |
| 非 `GET` 方法 | `405` | `{"error":"method not allowed"}` |

数值越界（`page<1`、`pageSize<1`、`pageSize>200`）**不是**错误，按上表归一。

---

## 3. opt-in 规则（必须遵守）

1. **无 `page` 且无 `pageSize`**：响应形态与分页功能上线前**逐字节兼容**（字段名、外层容器不变）。旧脚本、前端 store 无需改动。
2. **`page` 或 `pageSize` 任一存在**：返回第 1 节的统一信封。
3. 新增列表端点**必须始终返回统一信封**，不再提供裸数组/自定义容器形态。
4. 既有端点不得在未带分页参数时悄然切换为信封——需要新形态的集成方显式传参。
5. 租户边界不因分页改变：`total` 与 `data` 始终基于调用方上下文租户（`requireTenantContext`）过滤后的集合。

---

## 4. 当前端点形态表

| 端点 | 无分页参数（legacy） | 触发信封的参数 | 分页时 `data` 内容 | 稳定排序 |
| --- | --- | --- | --- | --- |
| `GET /api/v1/devices` | 按 segment 分组的对象：`{"seg-a":[...],"seg-b":[...]}` | `page` 或 `pageSize` | 跨 segment 展平后的设备列表；每项保留自身字段（含已存在的 `segment` 字段） | `deviceID` 升序（同 ID 以 `segment`、`agentID` 兜底） |
| `GET /api/v1/tasks` | 裸数组 `[...]`（可选 `?status=` 过滤；未分页时硬上限 2000，超出以 `X-Truncated: true` + `X-Total-Count` 告知） | `page`（`pageSize` 单独存在不触发） | 任务列表 | 存储层返回顺序（按创建顺序） |
| `GET /api/v1/alerts` | 裸数组 `[...]`（空列表为 `[]`） | `page`（`pageSize` 单独存在不触发） | 告警列表 | 存储层返回顺序 |
| `GET /api/v1/users` | `{"users":[...]}` | `page`（`pageSize` 单独存在不触发） | 用户列表 | 存储层返回顺序 |
| `GET /api/v1/tenants` | `{"tenants":[...]}` | `page`（`pageSize` 单独存在不触发） | 租户列表 | 存储层返回顺序 |
| `GET /api/v1/audits` | 裸数组 `[...]`（`limit` 默认 100、上限 1000） | `page` 或 `pageSize` | 审计事件列表 | `createdAt` 倒序；同刻按 `traceID`→`action`→`target`→`detail` 兜底 |
| `GET /api/v1/webhooks` | `{"webhooks":[...]}` | `page` 或 `pageSize` | Webhook 列表 | `id` 升序 |
| `GET /api/v1/scripts` | `{"scripts":[...]}` | `page` 或 `pageSize` | 脚本列表 | `id` 升序 |

### 兼容性注意（历史例外）

`users` 与 `tenants` 在分页时**不返回 `data` 字段**，而是沿用旧容器键并在同级追加 `total/page/pageSize/hasMore`：

```json
{"users":[ ... ], "total": 42, "page": 1, "pageSize": 20, "hasMore": true}
```

这是既有契约，未在本轮变更范围内；新端点不得模仿该形态，必须使用 `data` 信封。

`GET /api/v1/audits` 的 `limit` 参数仅作用于 legacy 形态。启用分页后忽略 `limit`，`total` 为过滤后全量行数。

---

## 5. 示例

```http
GET /api/v1/devices?page=2&pageSize=50
X-Tenant-ID: acme
```

```json
{
  "data": [
    {"deviceID":"dev-051","segment":"seg-b","tenantID":"acme","ip":"10.0.3.51","agentID":"agent-b","state":"online","taskState":"idle","managed":true,"lastResult":"","lastResultAt":"0001-01-01T00:00:00Z","retired":false,"hostname":"node-51","os":"linux","arch":"amd64"}
  ],
  "total": 137,
  "page": 2,
  "pageSize": 50,
  "hasMore": true
}
```

```http
GET /api/v1/audits?page=9&pageSize=100
```

```json
{"data":[],"total":137,"page":9,"pageSize":100,"hasMore":false}
```

```http
GET /api/v1/scripts?pageSize=abc
```

```json
{"error":"invalid page/pageSize"}
```

---

## 6. 回归守护

契约由 `internal/controlplane/pagination_optin_test.go` 表驱动覆盖（devices / audits / webhooks / scripts）：

- `legacy_no_params`：无参数时旧形态不变，且响应中不存在 `data`/`total` 字段；
- `first_page` / `page_only_default_size` / `page_size_only`：信封元数据正确；
- `page_below_one_clamped`：`page<1` 归一为 1；
- `page_beyond_last`：越界页 `data:[]`、`total` 不变、`hasMore=false`；
- `page_size_capped`：`pageSize>200` 封顶 200；
- `non_numeric_page` / `non_numeric_page_size`：400 且错误文案为 `invalid page/pageSize`；
- `tenant_isolation`：他租户行不出现在 `data` 中。
