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

---

## 进行中

| ID | 问题 | 现状 / 下一步 |
|---|---|---|
| TD-10 | 前端 E2E 只有 mock，无真实后端联调 | 已补 `playwright.real.config.js` + `e2e-real/health.spec.js` + CI `e2e-real` job（docker compose 起栈）；**待**：把登录、创建任务、SSE 推送等核心流程补充真实 spec |
| TD-11 | Store 消费方仍多依赖完整 Store 接口 | 已具备领域接口；**待**：handler/loop 按需要从 `Store` 改用子接口（如 `TokenStore`、`TaskStore`），进一步降低耦合 |

---

## 待启动（仍真实存在，按优先级）

| ID | 问题 | 位置 | 建议 |
|---|---|---|---|
| TD-20 | `internal/controlplane` 单包 14,489 行 | 27 个 `.go` 文件 | 按 `server_*.go` 已有形态继续拆；目标是单文件 ≤ 500 行 |
| TD-21 | `internal/store/memory.go` 2020 行 / `sql.go` 562 行 | store 包 | memory 按域拆 memory_*.go（部分已拆）；sql 同理 |
| TD-22 | agent 每次 RPC 重新 Dial 无连接池 | `internal/agent/grpcclient.go` | 引入 gRPC 连接复用 + 指数退避 |
| TD-23 | `domain` 包只有 struct，无业务行为 | `internal/domain/` | 补充校验/聚合方法；否则与 proto Task 等强耦合 |
| TD-24 | SSE 协议无对外规格 | `internal/controlplane/sse.go` | 新增 `docs/sse-protocol.md`（事件名/字段/重连策略/Last-Event-ID 兼容矩阵） |
| TD-25 | protobuf 与 JSON codec 双轨 | `internal/grpcx/` | 在 `docs/tech-selection.md` 写清取舍与迁移路径；codec 加 deprecation 警告日志 |
| TD-26 | Roadmap 文档的"演进目标"无验收标准 | `docs/product-roadmap.md` | 每条补 DoD：影响哪些包、估计改动量、性能预算、迁移兼容性 |
| TD-27 | Windows agent 假支持 | `internal/agent/exec_other.go` 仅兜底 | 明确 README / `--help` 说明 agent 仅支持 Linux；或补 Windows 实现 |
| TD-28 | CI coverage 门禁刚提到 50%/65%，未配增量门禁 | ci.yml | 后续接 `codecov.yml` 的 patch coverage 门槛 |
| TD-29 | `operator` 模块 Go 版本（1.22）与主模块（1.26）不一致 | `operator/go.mod` | 同步到 1.26，Dockerfile 基础镜像对齐 |

---

## 已明确不做

| ID | 问题 | 决定 / 理由 |
|---|---|---|
| TD-40 | 把 `internal/controlplane/web/` 完全物理删除 | **不做**：B1 bootstrap 的 `/install.sh` 与 `/bin/opsmesh-agent` 端点仍在该 webFS；物理删除会破坏 B1 自动纳管。当前走"内容收敛为引导 + 保留端点"的折中 |
| TD-41 | 引入 Element Plus/Naive UI 重写企业版前端 | **不做**：`tokens.css` 与 9 个基础组件已具备设计系统基线，重写投入产出比不优 |
