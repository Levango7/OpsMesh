# ABAC 影子模式实测报告（2026-11-17）

> 目标：不等待数周真实流量，先用控制面 demo 环境跑出一批可复现的 shadow 决策数据，
> 量化 `--abac-shadow` 的评估行为、指标口径与默认拒绝（default-deny）的覆盖率缺口，
> 并给出晋级到 enforcement 前的硬性条件。
>
> 结论：75.9s 内完成 **690 次变更请求评估**，allow 300 / would-deny 390 / error 0，
> 整体 would-deny 率 **56.52%**；缺口全部来自未被策略覆盖的 PUT/DELETE/PATCH 路径与
> `POST /api/v1/users`。影子模式本身未阻断任何请求，150 次 GET 均未被计数。

---

## 1. 环境与启动

| 项 | 值 |
|---|---|
| 仓库 | `F:\Agent\kilo\workspace\OpsMesh` |
| HEAD | `c6d17ca2` |
| 二进制 | `cmd/opsmesh`（`go build -o <temp>\opsmesh-cp.exe ./cmd/opsmesh`） |
| Go | `go1.26.6 windows/amd64` |
| 运行模式 | `--mode=controlplane --demo --store=memory --abac-shadow` |
| 端口 | HTTP `18080` / gRPC `19090` / metrics `19091` |
| 外部依赖 | 无。memory store；`--task-svc-grpc-addr=` 置空走存根；无 MySQL/Redis/Kafka |
| 数据目录 | 临时目录 `%TEMP%\kilo\abac-shadow-sim\data`（仓库外） |
| 版本输出 | `opsmesh 0.18.0 (commit=dev date=unknown)` |

实际启动命令：

```text
opsmesh-cp.exe --mode=controlplane --demo --store=memory --abac-shadow \
  --task-svc-grpc-addr= --http-port=18080 --grpc-port=19090 \
  --metrics-port=19091 --data-dir=<temp>\data --log-level=debug
```

启动后 `/readyz` 返回 `{"status":"ready"}`。

### 1.1 身份引导

1. `POST /api/v1/auth/login`，`admin/admin123` → `200`，`mustChangePassword=true`，
   仅返回一次性 `changePasswordToken`（不签发 access token）。
2. `POST /api/v1/auth/change-password`，携带 `changePasswordToken` + 新口令
   （12 位以上、含大小写/数字/特殊字符）→ `200`，返回正式 token。
3. 后续请求使用 `Authorization: Bearer <token>` + `X-Tenant-ID: default`。
4. 身份：`userID=user-admin`，`tenantID=default`，角色 `role-admin`；
   `/api/v1/auth/me` 展开权限含 `abac:write`、`script:write`、`webhook:write` 等。

### 1.2 策略种子

通过 `POST /api/v1/abac` 的 `action=policy-create` 写入 3 条 allow 策略（handler 固定
`effect=allow`，不接受 deny effect）：

| # | Subject 模式 | Resource 模式 | Action |
|---|---|---|---|
| 1 | `{"userID":"user-admin"*` | `{"path":"/api/v1/scripts"*` | `POST` |
| 2 | `{"userID":"user-admin"*` | `{"path":"/api/v1/webhooks"*` | `POST` |
| 3 | `{"userID":"user-admin"*` | `{"path":"/api/v1/alert-rules"*` | `POST` |

关键实现事实：`abac_engine.go` 对 subject/resource/action 做的是**字符串模式匹配**
（精确、`*`、`prefix*`），不是结构化 JSON 字段匹配。因此形如
`{"roles":["admin"]}` 的精确规则**不会**命中影子中间件序列化出的
`{"userID":"…","roles":[…],"tenantID":"…"}`。本次用 `userID` 前缀模式命中 admin。
未覆盖的路径/方法由引擎 default-deny 自然产生 would-deny，无需、也无法通过
`policy-create` 显式创建 deny 规则。

---

## 2. 流量画像

- 时长：**75.9s**（75 轮，每轮目标 1s 节奏）。
- 变更请求：**690 次**（`POST/PUT/DELETE/PATCH`）。
- 读请求：**150 次**（`GET /api/v1/scripts`、`GET /api/v1/users` 各 75 次）。
- 总 HTTP 请求：840 次。

| 请求 | 次数 | 实际 HTTP 状态 | 预期 ABAC 决策 |
|---|---:|---|---|
| `POST /api/v1/scripts`（合法体） | 75 | 201 | allow |
| `POST /api/v1/scripts`（非法体） | 75 | 400 | allow |
| `PUT /api/v1/scripts/{id}`（不存在 id） | 75 | 404 | deny |
| `DELETE /api/v1/scripts/{id}`（不存在 id） | 75 | 404 | deny |
| `POST /api/v1/webhooks`（合法/非法体交替） | 75 | 37×201 + 38×400 | allow |
| `PUT /api/v1/webhooks/{id}`（不存在 id） | 75 | 404 | deny |
| `POST /api/v1/alert-rules`（合法体） | 75 | 201 | allow |
| `DELETE /api/v1/alert-rules-engine/{id}`（不存在 id） | 75 | 404 | deny |
| `POST /api/v1/users`（合法/非法体交替） | 75 | 37×201 + 38×400 | deny（无策略） |
| `PATCH /api/v1/alert-rules/{id}`（每 5 轮 1 次） | 15 | 405 | deny |
| `GET /api/v1/scripts` + `GET /api/v1/users` | 150 | 200 | 不评估 |

说明：

- 400/403/404/405 不影响 shadow 计数：中间件在 handler 之前评估，只看请求方法与路径。
- `POST /api/v1/scripts` 的 400 来自空 `name`，说明业务失败与 ABAC 决策相互独立。
- 写用户时 37 次合法请求实际创建了 memory 用户；不影响结论，仅证明请求到达 handler。
- `POST /api/v1/abac`（策略管理）与 `/api/v1/federation/*` 被中间件显式排除，
  策略种子请求未被计数。

---

## 3. 实测结果

### 3.1 `GET /metrics` 原始序列

```text
opsmesh_abac_shadow_decisions_total{decision="allow",method="POST"} 300
opsmesh_abac_shadow_decisions_total{decision="allow",method="PUT"} 0
opsmesh_abac_shadow_decisions_total{decision="allow",method="DELETE"} 0
opsmesh_abac_shadow_decisions_total{decision="allow",method="PATCH"} 0
opsmesh_abac_shadow_decisions_total{decision="deny",method="POST"} 75
opsmesh_abac_shadow_decisions_total{decision="deny",method="PUT"} 150
opsmesh_abac_shadow_decisions_total{decision="deny",method="DELETE"} 150
opsmesh_abac_shadow_decisions_total{decision="deny",method="PATCH"} 15
opsmesh_abac_shadow_decisions_total{decision="error",method="POST"} 0
opsmesh_abac_shadow_decisions_total{decision="error",method="PUT"} 0
opsmesh_abac_shadow_decisions_total{decision="error",method="DELETE"} 0
opsmesh_abac_shadow_decisions_total{decision="error",method="PATCH"} 0
```

### 3.2 汇总

| 指标 | 数值 |
|---|---:|
| 总评估数 | 690 |
| allow | 300（43.48%） |
| deny（would-deny） | 390（56.52%） |
| error（评估器 panic） | 0（0.00%） |
| GET 被评估数 | 0 / 150 次发送 |
| 结构化决策日志行 | 690（与指标总数一致） |
| 评估耗时（日志 `durationMs`） | 全部 0ms（整数毫秒截断，即 <1ms） |

### 3.3 按方法拆分

| 方法 | 总数 | allow | deny | would-deny 率 |
|---|---:|---:|---:|---:|
| POST | 375 | 300 | 75 | 20.00% |
| PUT | 150 | 0 | 150 | 100.00% |
| DELETE | 150 | 0 | 150 | 100.00% |
| PATCH | 15 | 0 | 15 | 100.00% |

### 3.4 would-deny 路径 Top（日志聚合）

指标刻意不包含 `path` 标签（避免请求方控制的高基数），因此路径维度来自
`abac shadow decision` 结构化日志。把不存在的资源 id 归一为 `{id}` 后：

| 路径 | deny 次数 |
|---|---:|
| `PUT /api/v1/scripts/{id}` | 75 |
| `DELETE /api/v1/scripts/{id}` | 75 |
| `PUT /api/v1/webhooks/{id}` | 75 |
| `DELETE /api/v1/alert-rules-engine/{id}` | 75 |
| `POST /api/v1/users` | 75 |
| `PATCH /api/v1/alert-rules/{id}` | 15 |

allow 侧：`POST /api/v1/scripts` 150 次、`POST /api/v1/webhooks` 75 次、
`POST /api/v1/alert-rules` 75 次。决策原因分布：`allowed by policy …` 300 次，
`no matching policy (default deny)` 390 次。

### 3.5 口径确认

- 运行期计数直接来自 `internal/metrics/abac_shadow.go` 的 Prometheus 序列与
  `internal/controlplane/abac_shadow.go` 的原子计数器，**不依赖任何 `go test`
  专用计数或测试钩子**。`go test ./internal/controlplane -run TestABACShadow -count=1`
  仅用于确认中间件行为（通过，0.328s），未参与本报告数据。
- 690 条日志 = 690 次评估，说明没有“租户缺失跳过”或序列化失败导致的静默丢弃。

---

## 4. 解读

1. **默认拒绝放大了 would-deny 率。** 引擎语义是 deny-overrides + default-deny；
   本次只 seed 了 3 条 POST allow 规则，所有 PUT/DELETE/PATCH 以及
   `POST /api/v1/users` 都没有匹配策略，因此 100% 落到 default-deny。
   56.52% 是“策略覆盖率缺口”指标，不是生产环境拒绝率预测。

2. **覆盖是分方法、分路径的，不是一个总数。** POST 侧 allow 80%，
   因为流量集中打在已 seed 的 scripts/webhooks/alert-rules 上；
   PUT/DELETE/PATCH 侧 0% allow，说明当前策略镜像远未达到可强制执行的覆盖面。

3. **400/404/405 也被评估。** shadow 位于业务 handler 之前，评估不受业务结果影响；
   这正是影子模式的价值：可以在“请求本来就会失败”的情况下继续积累授权决策证据。
   反过来，如果把 4xx 当成“无需授权”，会低估真实覆盖率缺口。

4. **`error=0` 说明评估器健康。** 690 次评估无 panic、无策略数据异常；
   日志中的 `durationMs` 全为 0，说明单次评估开销低于日志整数毫秒分辨率。

5. **GET 不进入观察面。** 150 次 GET 没有产生任何 shadow 决策，符合设计
   （只观察变更方法）；读路径不会因 shadow 增加决策开销。

6. **策略匹配是字符串前缀，不是结构化属性匹配。** 这是晋级前必须对齐的语义：
   若生产策略按 `{"roles":["admin"]}` 这类结构化 JSON 编写，当前引擎不会命中。
   要么在策略编写规范中明确“使用影子序列化串的前缀模式”，要么在 enforcement
   前扩展引擎为结构化匹配。

---

## 5. 晋级到 enforcement 的建议

当前数据只能证明影子链路可用，**不足以直接开启强制**。建议按以下门槛推进：

### 5.1 必须先满足（硬门槛）

1. **基线策略全覆盖**：对生产 RBAC 允许的每一个 `(角色, 路径前缀, 方法)` 组合，
   至少有一条 ABAC allow 策略。优先域：scripts 执行、webhooks、alert-rules、
   users/roles、deploy/k8s、secrets、network/iptables 类高风险操作。
   PUT/DELETE/PATCH 必须单独覆盖，不能假设 POST 规则会继承。
2. **真实流量 shadow 观察 ≥7 天**（覆盖全部租户与全部变更入口），并满足：
   - 关键路径 would-deny **<1%**；
   - 非关键路径 would-deny 有逐条解释，且无“未知原因”拒绝；
   - `error=0`，且无 `abac shadow skipped: no verified tenant` 异常升高。
3. **持久化与多副本收敛验证**：使用 MySQL store 重启后策略不丢；
   多副本在 5s 刷新窗口内收敛；策略删除/更新不会出现副本间长期分叉。
4. **明确 enforcement 作用域**：ABAC 只对指定资源域生效，或由 RBAC 基线自动生成
   allow 策略；否则 default-deny 会连带阻断管理面与尚未迁移的接口。
   `/api/v1/abac`、`/api/v1/federation/*`、健康检查与读路径继续保持排除。
5. **可回退**：提供按租户/按路由的 canary 开关与全局 kill switch；
   强制拒绝必须落审计并接入告警，避免“静默全站 403”。

### 5.2 建议的验收指标

| 指标 | 晋级阈值 |
|---|---|
| 关键路径 would-deny | <1% |
| 全部变更请求 would-deny | <5%，且每条残留 deny 有 owner 解释 |
| `error` 决策 | 0 |
| shadow 评估 p99 开销 | <5ms（当前 <1ms） |
| 策略覆盖率（RBAC allow 三元组） | 100% |
| 真实流量观察时长 | ≥7 天，覆盖全部租户 |

### 5.3 下一步最小动作

1. 把本次 3 条种子策略扩展为“按 RBAC 角色 × 资源域 × 方法”的完整基线。
2. 用相同脚本重跑一次，目标是把 PUT/DELETE/PATCH 与 `POST /users` 的
   would-deny 降到 0，并确认 allow 覆盖与 RBAC 一致。
3. 在预发环境接真实流量跑满 7 天，再按 5.2 的指标评审是否开启 enforcement。

---

## 6. 复现与证据

本次未修改任何 Go/Vue 源码，未提交 commit。临时脚本与原始产物均在仓库外：

```text
%TEMP%\kilo\abac-shadow-sim\
  opsmesh-cp.exe          构建产物
  run-cp.ps1              启动脚本
  bootstrap.ps1           登录/改密/seed 策略
  traffic.ps1             75s 流量生成器
  traffic-summary.json    HTTP 状态计数
  metrics-shadow.txt      shadow 指标原始序列
  cp.err.log              690 条结构化决策日志（JSON Lines）
  shadow-log-analysis.json / shadow-normalized.json  日志聚合结果
```

复现命令顺序：

```powershell
go build -o <temp>\opsmesh-cp.exe ./cmd/opsmesh
powershell -File <temp>\run-cp.ps1          # 18080/19090/19091
powershell -File <temp>\bootstrap.ps1       # admin 改密 + 3 条策略
powershell -File <temp>\traffic.ps1         # 75s / 690 变更请求
(Invoke-WebRequest http://127.0.0.1:18080/metrics).Content |
  Select-String 'opsmesh_abac_shadow_decisions_total'
```

清理：停止控制面进程后，临时目录可整体删除；仓库内仅新增本文档。
