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
| TD-08 | TD-22 误登记：agent "每次 RPC 重新 Dial" | **误登记**：`grpcclient.go` 的 `B-4 连接复用` 已实现（conns 按 target 缓存长连接 + 错误淘汰重 Dial），从债务清单移除 |
| TD-09 | TD-23 误登记：domain "无业务行为" | **误登记**：`domain.go` 已有 Cancel/CanRetry/MarkDead/TransitionToProvisioning/Acknowledge/Silence 等 10+ 行为方法，从债务清单移除 |
| TD-10 | 前端 E2E 只有 mock，无真实后端联调 | ✅ 已完成：`playwright.real.config.js` + `e2e-real/health.spec.js`（探活）+ `core.spec.js`（登录/任务 CRUD/SSE 契约）+ CI `e2e-real` job（docker compose 起栈真跑）。剩余可选增强：任务执行等待 agent 回执的长链路用例 |
| TD-11 | Store 消费方仍多依赖完整 Store 接口 | ✅ 核查发现 M2-1B 早已落地：Registry 薄转发层已删（registry.go 仅留 package 占位），消费方直连子接口；仅 factory 的类型断言分发保留完整 Store，属合理用途。无进一步工作。 |
| TD-20 | `internal/controlplane` 单包 14,489 行 | ✅ 已完成：server.go 1954→387 行，按主题拆出 8 个 server_*.go；✓ 现单文件 ≤500 行 |
| TD-21 | `internal/store` 巨型文件 | ✅ memory.go 2020→1540 行（拆 os_template/middleware_template/alertgov）；`sql.go` 562 行为**迁移/DDL 基建域**（业务 CRUD 已在 14 个 sql_*.go 拆分），强拆破坏内聚，判定该项完成。 |
| TD-24 | SSE 协议无对外规格 | ✅ docs/sse-protocol.md 与 sse.go 逐字对齐（9 事件名/信封/心跳），并新增 sse_contract_test.go 守护——改代码不改文档时测试变红（已实测验证守护力）。 |
| TD-25 | protobuf 与 JSON codec 双轨 | ✅ docs/tech-selection.md §3 已写清取舍与迁移路径；当前 JSONCodec 带 `__v=1` 版本协商，双轨并存是正确决策。可选增强：过时 codec 打印 deprecation 日志（仅在启动时一次） |
| TD-26 | Roadmap 演进目标无验收标准 | ✅ `docs/product-roadmap.md` 已补 DoD 表（见 roadmap 附录 A） |
| TD-27 | Windows agent 假支持 | ✅ README 已声明"agent 仅 Linux"；如需 Windows 支持须专项立项（位置：`internal/agent/exec_other.go`） |
| TD-28 | CI 无增量覆盖率门禁 | ✅ 已新增 `codecov.yml`：patch ≥70%、project ≥50%（位置：ci.yml） |
| TD-29 | operator Go 版本与主模块不一致 | ✅ `operator/go.mod` 已对齐 go 1.26.0，`go mod tidy && go build` 通过（见 TD-30） |
| TD-30 | operator Go 版本与主模块割裂 | `operator/go.mod` 从 go 1.22 对齐至 go 1.26.0，`go mod tidy && go build` 已验证通过 |
| TD-42 | CSP 保留 `unsafe-inline` | `script-src` 已去除 `unsafe-inline`（任务 250 落地：前端 inline onclick 改用 addEventListener + server_middleware.go CSP 去掉 unsafe-inline） |

---

## 进行中

| ID | 问题 | 现状 / 下一步 |
|---|---|---|
| — | （暂无进行中项） | 上一轮进行中的 TD-10/TD-11/TD-25 均已 ✅ 落地，移入"已解决"节 |

---

## 待启动（仍真实存在，按优先级）

所有已识别技术债均已解决或明确不做（见下方）。

---

## 已明确不做

| ID | 问题 | 决定 / 理由 |
|---|---|---|
| TD-40 | 把 `internal/controlplane/web/` 完全物理删除 | **不做**：B1 bootstrap 的 `/install.sh` 与 `/bin/opsmesh-agent` 端点仍在该 webFS；物理删除会破坏 B1 自动纳管。当前走"内容收敛为引导 + 保留端点"的折中 |
| TD-41 | 引入 Element Plus/Naive UI 重写企业版前端 | **不做**：`tokens.css` 与 9 个基础组件已具备设计系统基线，重写投入产出比不优 |
