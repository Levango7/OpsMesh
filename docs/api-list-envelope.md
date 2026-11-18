# 列表端点统一信封契约（issue #96 核验结论）

> 核验日期：2026-11-18。来源：issue #96「四套信封并存」的运行时复核。

## 结论

issue #96 报告的「四套形状」在 earlier history + Kilo 批次42 后**已基本收敛**：
4 个列表端点在传 `page`/`pageSize` 时都返回分页信封；不传时保持原形状（向后兼容，
前端已按各自形状解析，联调实测一致）。

| 端点 | 不传 page（默认，向后兼容） | 传 page/pageSize（opt-in 信封） | 数据键 |
|---|---|---|---|
| `GET /api/v1/devices` | 分段 map `{seg-a:[...]}` | `{data,total,page,pageSize,hasMore}` | `data` |
| `GET /api/v1/alerts` | 裸数组 `[...]` | `{data,total,page,pageSize,hasMore}` | `data` |
| `GET /api/v1/tasks` | 裸数组 `[...]` | `{data,total,page,pageSize,hasMore}` | `data` |
| `GET /api/v1/tenants` | `{tenants:[...]}` | `{tenants,total,page,pageSize,hasMore}` | `tenants` |

> `tenants` 用 `tenants` 键而非 `data`：前端 `web/enterprise/src/api/tenant.js` 明确按
> `tenants` 解析（契约逐字写在文件头）。**不自洽改为 `data`**——那会破坏已联调通过的前端
> 契约，属破坏性变更，需 owner 裁决（见下）。

## 统一信封模板（新端点照抄）

```json
{
  "data": [],
  "total": 100,
  "page": 1,
  "pageSize": 20,
  "hasMore": true
}
```

后端实现：`paginate.ParsePaginationOptIn(q)` 解析参数（非法 → 400），
`paginate.PaginateResult` 作为响应结构。空集必须序列化为 `[]` 而非 `null`。

## 尚未完成（需 owner 裁决，不在本轮破坏性范围内）

1. **默认切换**：不传 page 时也统一信封 —— 会破坏前端解析分支，需前端同步 + owner 排期；
2. **tenants 键名**：`tenants` → `data` 统一 —— 同上，需前端 `tenant.js` 配合；
3. **传错参数名显式 400**（BB-02）：`limit`/`offset` 等旧词汇目前静默忽略，
   统一为 `page`/`pageSize` 并对未知参数报 400 需 owner 确认兼容窗口。

## 门禁

- `pagination_optin_test.go`：devices/audits/webhooks/scripts 的 opt-in 信封
- `server_alerts_contract_test.go`：alerts 空列表契约
- `acceptance_hardening_test.go`：tenants 分页信封
- `list_envelope_contract_test.go`：tasks opt-in 信封 + 空集非 null（本轮补）
