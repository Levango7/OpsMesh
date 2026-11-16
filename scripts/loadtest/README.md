# OpsMesh 负载基线（k6）

发布前的**手工门禁**，用于验证控制面在目标负载下满足 SLO。CI 不跑（需要真实栈 + 数据）。

## 判据

| 指标 | 阈值 | 依据 |
|---|---|---|
| `http_req_failed` | `rate < 0.001` | 与灰度预案 §3.2 「5xx ≤ 0.1%」一致 |
| `http_req_duration` | `p(99) < 300ms` | ADR-4 触发阈值（P99>300ms 才考虑只读副本/Redis Stream） |

> `auth_denied_rate` 是自定义指标：受保护端点返回 401/403 的比例。它**不计入**失败率（无 token 的压测会大量 401），但应接近 0——持续偏高说明 token/租户配置错了。

## 运行

```bash
# 冒烟（2 VU / 30s，验证脚本与鉴权可用）
k6 run scripts/loadtest/k6-baseline.js

# 负载基线（1→50 VU / 约 5.5min）
bash scripts/loadtest/run-baseline.sh
# 或 PowerShell
pwsh scripts/loadtest/run-baseline.ps1

# 指定目标与凭证
BASE_URL=https://opsmesh.example.com TOKEN='<jwt>' TENANT=t1 \
  k6 run -e SCENARIO=load scripts/loadtest/k6-baseline.js

# 压力上限探测
k6 run -e SCENARIO=stress -e TOKEN=... scripts/loadtest/k6-baseline.js
```

环境变量：

| 变量 | 默认 | 说明 |
|---|---|---|
| `BASE_URL` | `http://127.0.0.1:8080` | 控制面地址（去掉尾斜杠） |
| `TOKEN` | 空 | Bearer JWT；为空时只压 `/healthz`、`/readyz` |
| `TENANT` | `default` | `X-Tenant-ID` |
| `SCENARIO` | `smoke` | `smoke` / `load` / `stress` |

## 基线记录模板

每次切版或改核心路径（分页/限流/SSE/联邦）后记录一行：

| 日期 | commit | 场景 | VU | RPS | P99 | 错误率 | 备注 |
|---|---|---|---|---|---|---|---|
| 2026-11-16 | `<sha>` | load | 50 | — | — | — | 首次基线待跑 |

**未跑基线不放行 L2 全量灰度**（见 `docs/canary-release-and-rollback-plan.md` §2.3）。

## 端点说明

- `/healthz`、`/readyz`：无鉴权探活，`readyz` 依赖选主/存储状态。
- `/api/v1/devices|tasks|tenants?page=1&pageSize=20`：ADR-3 服务端分页路径；压测同时覆盖分页解析与列表查询。
- 需要更高覆盖时，在 `k6-baseline.js` 的 `default` 里按同一 `get()` 模式追加，并保持 `tags.endpoint` 唯一。

## 结果解读

- `p(99)` 超标但错误率正常：看 `http_req_duration{endpoint:...}` 分组，通常是列表查询或 `readyz` 的选主检查慢。
- 错误率超标：先确认 `auth_denied_rate` 与 401/403 分布；真实 5xx 才按灰度预案回滚判据处理。
- 压测机与目标同机时，k6 自身 CPU 会成为瓶颈：RPS 饱和而延迟平稳时优先怀疑压测机。
