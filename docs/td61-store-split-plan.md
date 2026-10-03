# TD-61 分批拆分方案：internal/store 依赖图与首批清单（2026-10-03）

> 背景：TD-61 登记时 controlplane/store 为 134/96 文件，现已长到 **87 / 109**（生产文件口径）。
> 直接 import internal/store 的文件 **129 个**——一次性搬迁即破 129 处编译，故本方案走「分域渐进（方案 C）」。

## 1. import 方按模块分布

| 模块 | 直接 import store 的文件数 |
|---|---|
| internal controlplane | 129 |

## 2. store 包内文件清单（按行数；"包内依赖"列为粗口径）

> **口径说明**：本表「包内依赖」是文件名共现的粗口径（存在同名误匹配），仅作排序参考，
> **不能作为搬迁依据**。真正动手前需按符号级 `go list -deps` 复核目标文件被外部引用的
> 具体符号集。

| 文件 | 行数 | 包内依赖 |
|---|---|---|
| sql_legacy.go | 36 | 2 |
| memory_agent_logs.go | 56 | 2 |
| sql_agent_logs.go | 56 | 2 |
| memory_bounds.go | 96 | 2 |
| memory_os_template.go | 96 | 2 |
| stub_guard.go | 97 | 2 |
| memory_plugin.go | 101 | 2 |
| memory_apikey.go | 124 | 2 |
| sql_tokens.go | 140 | 2 |
| memory_config.go | 145 | 2 |
| memory_secret.go | 156 | 2 |
| sql_audits.go | 178 | 2 |
| memory_discovery.go | 196 | 2 |
| sql_alerts.go | 266 | 2 |
| memory_billing.go | 293 | 2 |
| redis_session.go | 293 | 2 |
| memory_alertgov.go | 324 | 2 |
| sql_devices.go | 513 | 2 |
| sql_audit_chain.go | 615 | 2 |
| sql_tasks.go | 763 | 2 |
| store.go | 930 | 2 |
| memory.go | 1789 | 2 |
| multi_schema_agent_logs.go | 35 | 3 |
| memory_compliance.go | 110 | 3 |
| memory_k8s.go | 110 | 3 |
| memory_middleware_template.go | 115 | 3 |
| memory_tenant.go | 119 | 3 |
| memory_backup.go | 131 | 3 |
| memory_argocd.go | 155 | 3 |
| sql_secret.go | 167 | 3 |
| memory_traffic.go | 174 | 3 |
| memory_slo.go | 190 | 3 |
| memory_ticket.go | 194 | 3 |
| memory_network.go | 221 | 3 |
| memory_webhook.go | 225 | 3 |
| memory_script.go | 228 | 3 |
| sql_templates.go | 229 | 3 |
| memory_pipeline.go | 244 | 3 |
| sql_discovery.go | 254 | 3 |
| memory_automation.go | 263 | 3 |
| memory_rbac.go | 308 | 3 |
| sql_rbac.go | 616 | 3 |
| models.go | 792 | 3 |
| sql.go | 993 | 3 |
| multi_schema.go | 1530 | 3 |
| memory_refresh.go | 113 | 4 |
| sql_k8s.go | 125 | 4 |
| sql_refresh.go | 158 | 4 |
| multi_schema_p03.go | 210 | 4 |
| session.go | 266 | 4 |
| failures.go | 304 | 4 |
| multi_schema_p6.go | 364 | 4 |
| sql_quota.go | 81 | 5 |
| multi_schema_p3.go | 115 | 5 |
| multi_schema_p1.go | 137 | 5 |
| sql_compliance.go | 142 | 5 |
| sql_backup.go | 147 | 5 |
| multi_schema_p5.go | 167 | 5 |
| multi_schema_p4.go | 210 | 5 |
| sql_config.go | 216 | 5 |
| multi_schema_p2.go | 245 | 5 |
| sql_m2.go | 495 | 5 |
| sql_plugin.go | 166 | 6 |
| sql_argocd.go | 195 | 6 |
| sql_tenant.go | 200 | 6 |
| sql_traffic.go | 234 | 6 |
| sql_apikey.go | 238 | 6 |
| sql_ticket.go | 247 | 6 |
| sql_network.go | 259 | 6 |
| sql_script.go | 282 | 6 |
| sql_slo.go | 289 | 6 |
| sql_webhook.go | 290 | 6 |
| sql_automation.go | 341 | 6 |
| sql_pipeline.go | 342 | 6 |
| sql_billing.go | 530 | 6 |

## 3. 首批建议（各批独立绿，批间可停）

- **批次 1（低风险起步）**：自洽度最低的一组文件先搬（见上表头部），每批控制在 **≤10 个 import 方**；
  搬迁 = 新建子包 + 改 import + 旧包留 type alias 过渡（一个发布周期后再删）。
- **批次 2 起**：按主题域推进（alerts / automation / k8s / devices / audit / compliance …），
  每批顺带补该域的 store 测试——**搬迁与补测合并做**，这是本次拆分真正的收益来源。
- **不做**：一次性全搬（破 129 处）、以及「同包分文件」的零收益改法。

## 4. 前置条件核对

| 前置 | 状态 |
|---|---|
| 依赖图（本文件） | ✅ 已产出 |
| 分批策略 | ✅ 本文第 3 节 |
| 每批独立绿 | ✅ 靠 CI（build-test + services 逐模块 vet/lint/-race） |
| type alias 过渡层 | 首批随做随定 |

> 结论：**现在可开工**，按批次 1 起步；每批独立提交、独立绿。
