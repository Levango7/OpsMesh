# OpsMesh 技术债登记册（tech-debt）

> 目的：从代码内的 TODO / FIXME / task 引用 / `// Deprecated` 等零散线索收敛为一份可追踪登记册。
> 治理规则：任何本表删除必须伴随对应代码或文档真正落地；本表随 `CHANGELOG` 一起走 Review。

状态列：**已解决**（代码已落地）、**进行中**、**待启动**、**已明确不做（见理由）**。

---

## 已解决（本次收敛批次，可删）

| ID | 问题 | 解决方式 |
|---|---|---|
| TD-01 | Store 巨型接口（40+ 方法违反 ISP） | `internal/store/store.go` 已拆成 15 个领域小接口 + 编译期双实现断言 |
| TD-02 | 个人版前端死重（`internal/controlplane/web/`，~1.3 万行） | 删除全部业务 JS（flow_*/render/i18n/icons/api），仅保留引导页 + stub；业务全部由 Vue3 企业版接管 |
| TD-03 | `docker-compose.yaml` 硬编码 MySQL 弱口令 | 改为 `${MYSQL_ROOT_PASSWORD:-rootpass}` 等环境变量插值，CLI 强制注入 |
| TD-04 | Dockerfile 缺 `go mod verify` | 已加入构建阶段（防供应链投毒） |
| TD-05 | CI GitOps 写回步骤必失败 | `.github/workflows/ci.yml` 新增 clone/path 守卫，仓库未就绪时安全跳过（`::warning::`） |
| TD-06 | kafka-go 钉版本说明过期 | README/roadmap 已删除"必须钉 v0.4.48"的旧约束 |
| TD-07 | 双前端描述不一致 | README 删去"Deprecated v0.2→v0.4"表述，改为"已收敛为引导页" |
| TD-08 | TD-22 误登记：agent "每次 RPC 重新 Dial" | **误登记**：`grpcclient.go` 的 `连接复用` 已实现（conns 按 target 缓存长连接 + 错误淘汰重 Dial），从债务清单移除 |
| TD-09 | TD-23 误登记：domain "无业务行为" | **误登记**：`domain.go` 已有 Cancel/CanRetry/MarkDead/TransitionToProvisioning/Acknowledge/Silence 等 10+ 行为方法，从债务清单移除 |
| TD-10 | 前端 E2E 只有 mock，无真实后端联调 | ✅ 已完成：`playwright.real.config.js` + `e2e-real/health.spec.js`（探活）+ `core.spec.js`（登录/任务 CRUD/SSE 契约）+ CI `e2e-real` job（docker compose 起栈真跑）。剩余可选增强：任务执行等待 agent 回执的长链路用例 |
| TD-11 | Store 消费方仍多依赖完整 Store 接口 | ✅ 核查发现 早已落地：Registry 薄转发层已删（registry.go 仅留 package 占位），消费方直连子接口；仅 factory 的类型断言分发保留完整 Store，属合理用途。无进一步工作。 |
| TD-20 | `internal/controlplane` 单包 14,489 行 | ✅ 已完成：server.go 1954→387 行，按主题拆出 8 个 server_*.go；✓ 现单文件 ≤500 行 |
| TD-21 | `internal/store` 巨型文件 | ✅ memory.go 2020→1540 行（拆 os_template/middleware_template/alertgov）；`sql.go` 562 行为**迁移/DDL 基建域**（业务 CRUD 已在 14 个 sql_*.go 拆分），强拆破坏内聚，判定该项完成。 |
| TD-24 | SSE 协议无对外规格 | ✅ docs/sse-protocol.md 与 sse.go 逐字对齐（9 事件名/信封/心跳），并新增 sse_contract_test.go 守护——改代码不改文档时测试变红（已实测验证守护力）。 |
| TD-25 | protobuf 与 JSON codec 双轨 | ✅ docs/tech-selection.md §3 已写清取舍与迁移路径；当前 JSONCodec 带 `__v=1` 版本协商，双轨并存是正确决策。可选增强：过时 codec 打印 deprecation 日志（仅在启动时一次） |
| TD-26 | Roadmap 演进目标无验收标准 | ✅ `docs/product-roadmap.md` 已补 DoD 表（见 roadmap 附录 A） |
| TD-27 | Windows agent 假支持 | ✅ README 已声明"agent 仅 Linux"；如需 Windows 支持须专项立项（位置：`internal/agent/exec_other.go`） |
| TD-28 | CI 无增量覆盖率门禁 | ✅ 已新增 `codecov.yml`：patch ≥70%、project ≥50%（位置：ci.yml） |
| TD-29 | operator Go 版本与主模块不一致 | ✅ `operator/go.mod` 已对齐 go 1.26.0，`go mod tidy && go build` 通过（见 TD-30） |
| TD-30 | operator Go 版本与主模块割裂 | `operator/go.mod` 从 go 1.22 对齐至 go 1.26.0，`go mod tidy && go build` 已验证通过 |
| TD-42 | CSP 保留 `unsafe-inline` | `script-src` 已去除 `unsafe-inline`（落地：前端 inline onclick 改用 addEventListener + server_middleware.go CSP 去掉 unsafe-inline） |
| TD-50 | `internal/controlplane` 测试覆盖率不足 | ✅ 已完成：补充 `handler_extra_test.go` / `handler_m4_test.go` / `integration_m4_test.go` / `integration_m5_test.go` / `observability_m1_4_test.go` / `loop_m4_test.go` / `endpoint_test.go` / `command_validation_test.go` / `shell_safe_test.go` / `integration_inhibit_test.go` 等多个测试文件，controlplane 覆盖率提升至 56.9%（Batch2），按 `server_*.go` 主题拆分后单文件 ≤500 行可测性显著改善 |
| TD-51 | Helm Chart 测试覆盖率不足 | ✅ 已完成：`deploy/helm/opsmesh/` 全套 17 个模板落地（含 ServiceMonitor / PrometheusRule / Ingress / HPA / NetworkPolicy / PodDisruptionBudget），`server_helm_extra_test.go` 补充 handler 测试；CI `image` job 把 chart `global.image.tag` 钉死为本 commit sha（含占位符守卫）；`helm` 包覆盖率经 `internal/helm/` catalog 测试覆盖 |
| TD-52 | `internal/discover` 与 `internal/discovery` 包边界混淆 | ✅ 已完成：两包 `doc.go` 明确职责边界——`discover` = 设备发现（控制面→网段找设备，TCP 存活扫描），`discovery` = 控制面服务发现 + 负载均衡（agent→控制面 failover/round-robin）；README「internal 包职责」章节与「discover vs discovery 边界说明」表格明确区分，分属设备纳管域与 agent 高可用域，无相互依赖 |
| TD-53 | 文档与代码不同步 | ✅ 已完成（2026-08-24 文档同步批次）：README 功能矩阵扩展为 14 个功能域 + 技术栈 + 30 个 internal 包职责 + discover/discovery 边界说明 + 三种部署方式；DELIVERY 代码规模刷新（346 Go 文件 / 84 前端文件 / 34 包）+ 14 功能域交付清单；api-reference 补全设备指标 + K8s 资源管理 15 端点；CHANGELOG 追加 [Unreleased] 段记录最近变更；tech-debt 登记 TD-50~TD-54 |
| TD-54 | 版本发布流程缺失 | ✅ 已完成：CHANGELOG.md 采用 Keep a Changelog 格式 + Semantic Versioning；`internal/version/version.go` 暴露内核版本供 `--version` 与镜像标签；`.goreleaser.yml` 配置 GoReleaser 自动化发布；CI `release` job 由 tag 触发；Helm Chart `Chart.yaml` 版本与主版本同步；DELIVERY.md 记录仓库地址与提交链 |

---

## 进行中

| ID | 问题 | 现状 / 下一步 |
|---|---|---|
| — | （暂无进行中项） | 上一轮进行中的 TD-10/TD-11/TD-25 均已 ✅ 落地，移入"已解决"节 |

---

## 待启动（仍真实存在，按优先级）

> 2026-08-30 全仓审查后如实登记（此前本节声称"全部解决"，与实际不符，已纠正）。
> 以下为 25 提交增量演进（18 微服务拆分 + GPU/AIOps/ChatOps 等新域）引入的结构性债务，
> 均需独立立项，不阻塞当前交付。

| ID | 问题 | 现状 / 影响 / 下一步 |
|---|---|---|
| TD-60 | **七域双份实现收敛**（约 27,000 行重复） | **2026-09-05 产品决策：选项 A——微服务化为正式方向**。同日深度侦察（两路 agent 全仓盘点）后拆为两阶段：**阶段 1（已完成）**：轻四域（gpu/runbook/incident/autoscaler）经侦察确认 controlplane 侧已纯 service_proxy 代理化（零本地实现）——迁移目标状态实际已就位，遗留缺口是前端契约对齐层（4 个必 404+2 个 405+3 处字段断裂），已全部修复并补 22 个 handler 防回归测试（e2e757c+67a1050，CI 全绿）。**阶段 2（已立项待启动，核心三域 task/device/auth 迁移）**：侦察结论=现阶段不可薄客户端化——task-svc 缺四大任务必达机制（DAG 释放/定时派生 scheduleLoop/租约回收 reclaim/leader 选主，缺失会让失联任务永久卡死+定时全停）；device-svc 的 StartDiscovery 是硬编码 stub（写死 3/254）+自动纳管链路（install token 签发/消费、install.sh/agent 分发、SSH 推送、后台循环）全缺；auth-svc 缺 HttpOnly Cookie/防爆破 loginGuard/注册审批，而 controlplane requireProd 被 121 处调用依赖本地 JWT 验签（全量远程化=每请求加一跳 RPC+auth 故障全站 401）。**最大耦合点**：agent gRPC 通道（GrpcServerImpl，PullTasks/ReportResult/Heartbeat/Register 四条直写 store）是 agent 管控数据面——agent 只说 opsmesh.v1.Registration 协议不认微服务协议，该通道保留 controlplane 实现（阶段 2 需给其加"内部转调微服务"的适配层决策）。**阶段 2 前置补齐清单**：task-svc 补 DAG releaseDeps/scheduleLoop+FireDueSchedules/reclaimLoop/leaderLoop/canary/batch 推进/validateCommand 移植；device-svc 接真实 Sweep/install token/bootstrap 资产分发/PushAndExec/autoProvisionLoop/心跳归档/metrics；auth-svc 补 loginGuard/注册审批/validateStrongPassword/DeviceFP/Redis SessionStore + 与 controlplane 统一 JWT claims 契约（共享密钥本地验签模式）。工作量=数周级独立工程，需按域分批立项 |
| TD-61 | **controlplane/store 父包拆分** | `internal/controlplane/` 顶层 **134** 文件、`internal/store/` **96** 文件（2026-09-05 实测）。**下一步**：按主题继续下沉到子包，目标顶层 ≤20 文件。纯机械重构但 diff 巨大（import 全改），建议与 TD-60 收敛决策合并立项（收敛时一并搬家，避免两次全量 import 改动） |
| TD-62 | **API 网关完整数据面** | 当前 `/gw/` 为最小反向代理：PathPrefix 匹配 ✓、路由级限流 ✓（gatewayRouteEntry.RateLimiter，超出 429）、统计 ✓；缺数据面级鉴权/熔断/租户隔离。**2026-09-05 决策：明确声明为控制面预览形态**（路由规则 CRUD/统计是产品能力，数据面转发是 demo 级），完整数据面不在当前路线图——生产流量应走 APISIX/Envoy 等正式网关（README 部署架构已按此口径），本网关定位为轻量路由编排预览。升级为正式数据面需重新立项 |
| TD-63 | **Task 多份 schema 统一** | **2026-09-05 复核修正**：原表述"三份 Task 定义"不准确——proto/opsmesh/v1 实际只有 registration.proto（163 行，agent gRPC 管控通道），**不含 Task**；真实的双份是：services/task-svc/api/proto/v1/task.proto（300 行，task-svc 自身 gRPC API）与 internal/proto/model.go Task struct（JSON codec 主契约）。两份服务于不同通道（独立服务 gRPC vs controlplane JSON），字段演进需人工同步。**下一步**：低成本缓解=在两份定义头部互相加"演进须同步"锚注释+CI 加字段数一致性断言（浅校验）；根治=buf generate 单一来源（需先把 Task 契约搬进 proto/opsmesh/v1，属 TD-60 决策的前置） |
| TD-64 | ~~**pb stub 死代码**~~ | **2026-09-05 复核撤销**：原判定过期——internal/grpcx/pb（registration.pb.go + registration_grpc.pb.go）是 agent gRPC 管控通道的**在用 stub**（internal/grpcx/stub.go 消费，gRPC Register/Heartbeat/TaskReport 通道），非死代码。JSON codec 是 B/S 通道主契约、gRPC 是 agent 管控通道，双轨各司其职。原"删除或补消费方"的下一步作废 |
| TD-65 | ~~**微服务 MySQL store 未接线**~~ | ✅ 2026-08-30 已修（9/12 接线）：alert/auth/config/deploy/device/incident/plugin/portal/task 经 `<NAME>_SVC_STORE_TYPE=sql` + DSN 启用 MySQL（失败回退 memory）；缺字段服务补 config，mysql.go 补编译期断言；**附带安全修复**：config-svc 硬编码 `deriveKey("default-key")` 改为 cfg.EncryptionKey（跨后端加密一致）。跳过项与原因：runbook（无实现）/ gpu（manager 不消费 store，需重构）/ workflow（接口签名不兼容，需适配层）/ log（main 已有 memory/sql/loki/es 四后端分支，不应换接）。留此行供集成测试补齐后删除 |
| TD-67 | ~~gofmt 基线破坏~~ | ✅ 2026-08-30 已修：`gofmt -w internal cmd pkg services tests` 全仓恢复（73+66 文件），`gofmt -l` 清零，`go build ./...` 通过。留此行供 CI 首跑确认后删除 |

---

## 已明确不做

| ID | 问题 | 决定 / 理由 |
|---|---|---|
| TD-40 | 把 `internal/controlplane/web/` 完全物理删除 | **不做**：B1 bootstrap 的 `/install.sh` 与 `/bin/opsmesh-agent` 端点仍在该 webFS；物理删除会破坏 自动纳管。当前走"内容收敛为引导 + 保留端点"的折中 |
| TD-41 | 引入 Element Plus/Naive UI 重写企业版前端 | **不做**：`tokens.css` 与 9 个基础组件已具备设计系统基线，重写投入产出比不优 |
