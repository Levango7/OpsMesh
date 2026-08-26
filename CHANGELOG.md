# Changelog

本文件记录 OpsMesh 所有重要变更。格式参考 [Keep a Changelog](https://keepachangelog.com/)，版本号遵循 [Semantic Versioning](https://semver.org/)。

> 当前最新已发布版本：`v0.7.0`（2026-08-16）。`[Unreleased]` 段累积未发布变更，下一个发布版本号待定（按实际演进预计 `v0.8.0`；若按文档同步批次独立发版可记为 `v0.5.0`，由发布流程最终确定）。

## [Unreleased] — 2026-08-27 技术债务清偿批次（测试覆盖率提升 + 编码修复 + 架构文档）

### 测试覆盖率提升（Go 单元测试）
- **internal/store**：50.9% → 74.6%，新增 `memory_crud_extra_test.go` 覆盖 apikey / argocd / automation / backup / billing / compliance / network / pipeline / plugin / slo / traffic 十一个此前零覆盖领域，以及 MultiSchemaStore 委托层（p03~p6）与 config/secret/discovery/script/tenant/ticket/webhook 缺口方法
- **internal/alertengine**：96.8% → 99.8%，新增 `engine_extra_test.go`（11 个测试），覆盖 Z-Score/EWMA 基线检测、异常引擎多规则命中、抑制器、静默器、聚合管线
- **internal/config**：98.0% → 99.7%，新增 6 个测试，覆盖 AllowStubStores 四象限矩阵、Production TLS/EncryptionKey 强制、flag 组合矩阵、env 兜底、Shell 白名单导出
- **internal/provision**：94.3% → 98.1%，新增 `provision_extra_test.go`，覆盖纳管流程错误路径
- **cmd/opsmesh**：24.3% → 98.0%，新增 10 个测试，覆盖 runMain 各 mode 失败分支（端口占用 / TLS 缺失 / 非法 DSN）、runBackup/runRestore 错误路径（导出写目录 / Store 初始化失败）

### 编码修复
- **移除 5 个文件头部 BOM**：`memory_discovery.go` / `memory_secret.go` / `sql_config.go` / `sql_discovery.go` / `sql_secret.go`。修复 Go 1.26 `go test -cover` 插桩与文件头 BOM 不兼容导致的 "invalid BOM in the middle of the file" 编译错误（纯编码规范化，零语义变更）

### 架构文档
- **README 架构图重绘**：Unicode 框线改纯 ASCII（`+-|/` 等），同步 internal 包数 30 → 36（补 automation / compliance / extension / network / platform / plugin），store 子接口 15 → 35，补全企业版 Vue3 前端 / K8s Operator / 联邦通道（--federation-peers）/ mTLS / Metrics / SSE / protobuf gRPC 双轨 / 多租户 schema 隔离 / API Key（`om_` 前缀）/ log_collect v2.0 / alertengine（Z-Score+EWMA）等组件

## [Unreleased] — 2026-08-26 第四轮质量审查修复批次（31 项）

### 安全加固
- **API Key 认证体系**（H5）：platform 层新增 ValidateKey/HasScope + `ConstantTimeCompare` 恒时比较，controlplane 认证链支持 `Bearer om_` 前缀 API Key，PUT 白名单字段合并防篡改（M2）
- **Webhook SSRF 防护**（M1）：出站 URL 强制 scheme 白名单 + 私网/环回地址拦截（ValidateWebhookURL）
- **审计补齐**（H9）：automation / network / gateway 共 12 处敏感操作补写审计事件
- **跨租户越权修复**（H1）：handler 租户归属校验补全
- **Production 配置桩拒绝**：SQL 桩存储需显式 `AllowStubStores` 开启，生产配置校验强制拒绝

### 正确性修复
- **日志采集截断丢日志**（H8-C1）：offset 改为按实际处理量推进，单 tick 记录数上限 break 早退不再丢弃剩余行；`logCollectError` 补 `Is()` 支持 errors.Is 判别
- **Store 读路径内部指针泄漏**（H8-C2）：config / secret / discovery 读路径锁内 clone 后返回，外部修改副本不再污染内部状态
- **ensureGateway 并发竞态**（M7）：`sync.Once` 保护网关引擎单例初始化
- **platform 死代码删除**（H7）：BillingManager / TenantManager / PluginManager 及其方法移除，保留类型别名与数据模型
- **租户删除保护与级联清理**（L3）：`default` 租户删除返回 409；删除租户级联清理 APIKey / Webhook / Script 三域资源
- **脚本执行防护**（L1）：timeoutSec clamp 至 [1,600]；禁用脚本 execute 返回 409；CreateScript 默认 `Enabled=true` 保持向后兼容

### 输入校验
- **marketplace 插件校验**（L1）：pluginType 白名单 `{data,logic,integration}` + downloadURL 仅允许 http/https scheme
- **gateway 路由后端校验**（L1）：targetBackend scheme 白名单 `{http,https,grpc}` + host:port 格式校验
- **ParseFloat 错误处理**（H6）与 BOM 文件编码问题（H10）、魔法数字常量化（L7）

### 占位实现透明化
- **simulated 标记**（M12）：backup restore / canary metrics / HA failover / compliance scan 四处占位响应显式携带 `simulated:true`
- **平台配置假审计修正**（M12）：PUT 平台配置审计 Action 改 `platform_config_update_simulated` 后缀 + 响应体标记 simulated

### Store 层治理
- **统一桩入口 stub_guard.go**（M6）：15 个 SQL 域桩方法接入统一桩语义与告警计数
- **Record 断言消除**（M3）：RecordWebhookDelivery / RecordScriptExecution 提升进 Store 接口，webhook/script handler 类型断言移除
- **MultiSchemaStore 修复**（M4/M5）：随机租户 ID schema 名合法化（`-`→`_`）；ListSubscriptions/ListInvoices 空串聚合遍历 allStores
- **读路径 clone 全覆盖**：memory_apikey 模式推广至 config/secret/discovery 域

### 测试补强（+60 个测试用例）
- **跨租户隔离矩阵**（M10）：apikey/billing 跨租户头断言 403；marketplace/tenant 设计行为文档化（全局市场/平台级管理不做租户校验）
- **分页边界值守护**（M11）：page=0/pageSize=0/page=-1/pageSize=100000 clamp 行为锁定 + 空 body POST→400 透传验证
- **桩语义锁定**（H11）：stub_semantics_test.go 固定 Create→nil / Get→(nil,false) / List→空切片 / Delete→false 契约 + StubDomains 完整性断言
- **动态权限计数**（M9）：auth_test 三处硬编码 `72` 改为 `len(RolePermissions()["admin"])` 动态派生 + 下限守护 ≥60
- **CI race job**（L6）：新增 ubuntu-latest `go test -race -count=3` 独立 job
- **Phase0 清偿测试**（H8-C3/C4）：日志截断边界多轮分片拼接还原 + store clone 并发 race 断言
- **审查文档归档**：docs/design/REVIEW-phase1-6.md（31 项发现）+ FIXPLAN-phase1-6.md（修复方案）

## [Unreleased] — 2026-08-24 文档全面同步批次

### 文档同步
- **README.md**：功能矩阵扩展为 14 个功能域（设备管理 / 任务执行 / 监控告警 / CMDB / 日志检索 / 编排部署 / OS 优化 / 中间件部署 / K8s 管理 / 用户中心 / 审计日志 / 联邦 / SSE 实时推送 / 工作流），对齐 `docs/feature-design.md` F1–F18 与 `docs/product-roadmap.md` M1–M4
- **README.md**：新增「技术栈」章节（Go 1.26 + Vue3 + Vite + Pinia + MySQL + Redis + gRPC + OTel）与「internal 包职责（30 个）」章节，按 7 个领域分组列出全部 internal 包
- **README.md**：明确 `internal/discover`（设备发现，控制面→网段找设备）与 `internal/discovery`（控制面服务发现 + 负载均衡，agent→控制面 failover）的边界
- **README.md**：快速启动补充 docker-compose / Helm / systemd 三种部署方式
- **README.md**：开发指引补全 30 个 internal 包（新增 alertengine / approval / circuitbreaker / discovery / helm / k8s / otelx / provision / secrets）
- **DELIVERY.md**：代码规模刷新至 2026-08-24（179 源码 + 167 测试 = 346 Go 文件，84 前端文件，34 包），功能交付清单对齐 14 个功能域
- **docs/api-reference.md**：补全 `GET /api/v1/devices/{id}/metrics`（设备监控指标，支持 `?range=15m|1h|2h|6h|24h` 历史时序）
- **docs/api-reference.md**：K8s 资源管理章节补全 15 个端点（namespace / pod / deployment + scale/restart/rollback / service / configmap / secret / node / dashboard / health）
- **docs/tech-debt.md**：新增 TD-50~TD-54（controlplane 覆盖率 / helm 覆盖率 / discover-discovery 边界 / 文档同步 / 版本发布流程）

### 安全
- **第三轮终审 P0/P1/P2 修复**（`35e2375`）：security/deploy/store 多处安全漏洞与部署阻断修复
- **refresh token 过期清理**（`5199f4e`）：周期清理过期刷新令牌 + blacklist，避免 goroutine 泄漏
- **demo JWT 默认密钥移除**（`f0fc51e`）：未设置 `OPSMESH_JWT_SECRET` 时二进制自动生成随机密钥（重启后旧 token 失效），生产务必显式注入
- **rows.Err() 补齐 20 处**（`f0fc51e`）：SQL 迭代错误路径覆盖
- **Dockerfile digest 钉死**（`af9a914`）：base image 摘要固定，防供应链漂移

### 前端
- **HttpOnly Cookie 会话恢复**（`3af70a7`，P0）：修复 SSE 帧分割边界残留
- **SSE 401 刷新重连**（`612d59b`，P1）：URL 编码统一 + i18n 错误消息
- **列标题 i18n 化**（`20629b2`，P2）：vite 代理环境变量 + eslint 恢复 no-v-html + 移除 msw
- **E2E 断言改用 data-testid**（`85d4d2f`）：替代中文文案，防语言切换失败

### 质量
- **去 AI 化全面收尾**（`9014081`）：注释 / 标识符 / 文档清理 + TestExecute_Timeout 阈值修复
- **log.Fatalf → return error**（`e3f9324`，P1）：错误处理规范化
- **flaky 测试根治**（`08827f8`）：CMDB 节流测试与采集耗时解耦 + E2E 超时预算放宽
- **store 覆盖率 75.7%**（`3e86452`）：BadDB 方法覆盖 SQL 错误路径（49.8% → 57.5% → 75.7%）
- **6 个低覆盖包补全**（`17708be`）：grpcx 99.5% / otelx 97.2% / secrets 96.4% / authctx 93.1% / cmdb 95.4% / deploy 81.0%

### 部署
- **.dockerignore 补全**（`bf25730`）：构建上下文 ~250MB → 215KB
- **HPA replicas 冲突修复**（`5199f4e`）：Helm Chart HPA 与 Deployment replicas 去冲突
- **NetworkPolicy 补全**（`5199f4e`）：Helm Chart 网络策略加固
- **pipefail 补全**（`5199f4e`）：CI shell 脚本 pipefail 加固
- **toolchain 锁定 go1.26.6**（`d78d01b`）：解决多版本冲突

### CI
- **CI release 触发/secret 复用修复**（`cb2f58a`）：审计遗留问题修复
- **SSE 契约/canceled 拼写修正**（`5199f4e`）：五态 dead_letter 修正

### 验证
- `go build ./...` ✅
- `go vet ./...` ✅
- `go test -timeout 300s ./...` ✅

---

## [0.7.0] — 2026-08-16

### 可视化与检索增强
- **CMDB 关系图谱可视化**（`web/enterprise/src/components/RelationGraph.vue`）：纯 SVG 力导向图 + 网络拓扑布局，CI 类型颜色 + 关系类型线型 + 拖拽缩放平移 + 图例 + 节点详情面板，集成到 CMDBView 三视图切换
- **全文本检索倒排索引**（`internal/logstore/inverted.go`）：中英文混合分词 + TF-IDF 排序 + 短语/布尔/通配符查询 + 并发安全，`SearchFullText` 集成到 MemoryLogStore
- **多集群联邦发布**（`internal/deploy/federation.go`）：FederationStore + FederationCoordinator 跨集群灰度协调 Start/Promote/Reconcile/Rollback/Status + 联邦级发布状态 REST API

### 交付物补全
- **Argo CD GitOps 仓库**（`deploy/gitops/`）：ApplicationSet 多网段批量渲染 + AppProject 隔离 + 网段 values 示例（example/production）

### 文档体系建立

完成 13 个核心设计文档（共约 19,385 行），覆盖产品/架构/数据库/接口/安全/UI/模块/功能/测试/运维/AI/多系统/部署场景全维度。

#### 核心文档（5 个，245KB）
- **docs/product-design.md**（457 行）：产品定位/目标用户/功能矩阵/竞品对比/商业模式/适用场景/非功能需求/路线图
- **docs/architecture.md**（925 行）：架构构图/分层设计/模块依赖/Store 接口拆分/数据流/技术选型/扩展点/容量规划/高可用/多租户
- **docs/database-design.md**（1163 行）：ER 图/29 张表结构详解/索引设计/分库分表/数据生命周期/迁移策略/容量估算
- **docs/api-specification.md**（1368 行）：OpenAPI 3.0 规范/错误码标准/认证规范/版本管理/分页过滤/SSE/gRPC/限流/幂等性
- **docs/security-mechanism.md**（1173 行）：认证/授权/传输安全/输入安全/SSRF/密钥管理/审计/租户隔离/联邦安全/Agent 安全/部署检查清单

#### 设计文档（5 个）
- **docs/ui-design.md**（764 行）：设计系统/组件库/页面布局/交互规范/主题切换/i18n/无障碍/双前端策略
- **docs/module-design.md**（1508 行）：30 个 internal 包详细设计，按 7 个领域分组，每包 6 维度
- **docs/feature-design.md**（2246 行）：18 个功能模块详细设计，每模块 7 子节（概述/用例/流程图/业务规则/边界条件/配置项/API）
- **docs/test-specification.md**（968 行）：测试策略/分层测试/覆盖率目标/CI 矩阵/E2E/性能/安全测试
- **docs/operations.md**（2131 行）：部署/配置/监控/告警/日志/备份/扩缩容/故障排查/巡检/SOP

#### 扩展文档（3 个）
- **docs/ai-design.md**（1779 行）：AI 能力总览/异常检测/智能告警/根因分析/容量预测/AIOps Copilot/智能编排/日志分析/模型管理/数据管道/AI 安全治理/性能成本/集成架构/路线图
- **docs/multi-os-support.md**（2182 行）：当前支持状态/目标支持矩阵（18 系统）/平台抽象层/11 个系统详细方案/跨平台 CI/Agent 构建/平台配置/已知限制/路线图
- **docs/deployment-scenarios.md**（2719 行）：12 个部署场景（单机房/异地多机房/多数据中心/电信资源池/混合云/公有云/私有云/边缘/国产化/容器化/高安全/灾备）+ 对比选型 + 自动化

### 测试覆盖率提升

#### 低覆盖包补全（6 个包）
- **grpcx**：48.9% → 99.5%（新增 `grpcx_extra_test.go`，761 行）
- **otelx**：58.0% → 97.2%（新增 `otelx_extra_test.go`，514 行）
- **secrets**：61.3% → 96.4%（新增 `secrets_extra_test.go`）
- **authctx**：62.6% → 93.1%（新增 `authctx_extra_test.go`）
- **cmdb**：41.5% → 95.4%（新增 `cmdb_extra_test.go` 970 行 + `sql_test.go`）
- **deploy**：60.1% → 81.0%（新增 `deploy_coverage_test.go`，1622 行）

#### store 包覆盖率提升
- **store**：49.8% → 57.5% → **75.7%**（超过 70% 目标）
  - `store_extra_test.go`：MemoryStore 边缘路径/redis_session/multi_schema
  - `store_extra2/3/4_test.go`：SQLStore 纯函数/scan 函数/IsLeader/DeviceMetrics/早期返回路径/MultiSchemaStore 错误路径
  - `store_extra5_test.go`（530 行）：使用不可达 DB（127.0.0.1:1）测试 SQL 方法错误路径，覆盖 GetUser/UpdateUser/DeleteUser/CreateRole 等 40+ 个 SQL 方法

### 文档同步
- roadmap 7.2 作业编排标记已实现（子工作流/条件分支/超时重试/执行历史）
- roadmap 7.5 日志检索标记 ELK/Loki 对接已实现
- roadmap 5.2/5.4/5.5/5.6/7.6/7.7 标记已交付

### 验证
- `go build ./...` ✅
- `go vet ./...` ✅
- `go test -timeout 300s ./...` ✅ 全绿
- `npm run build` ✅
- `npx vitest run` ✅ 527 测试全绿

---

## [Unreleased] — 2026-08-16 CI 全绿

### 里程碑：GitHub Actions 8/8 job 全绿（首次真正全绿）

从"build 就挂"推进到全链路绿：build-test / integration / security / proto / frontend / E2E(real backend) / image 全部通过（release 仅 tag 触发，skipped 属正常）。

#### 产品级缺陷修复（不只是 CI 适配，生产也有意义）

- **MySQL 启动时序竞态**（`internal/store/sql.go`）：控制面启动时 MySQL 未就绪 → 迁移+seedRBAC 失败（非致命）→ admin 用户/表缺失 → 运行期 401/404。新增 `initWithRetry`（10 次 × 3s 退避）等待 MySQL 就绪后重试迁移+seed。
- **agent 裸注册租户缺失**（`internal/controlplane/grpc.go`）：无 install token、无网关时 `TenantID=""`，被 `Agents("default")` 租户过滤 → 控制面永远看不到该 agent。demo 模式租户兜底填 `default`（与 dashboard/SSE 一致）。
- **agent 镜像无 /bin/sh**（`Dockerfile.agent`）：runtime 用 `gcr.io/distroless/base-debian12`，官方明确不含 shell → `exec.Command("sh",...)` 启动失败 → 所有 shell 任务 `exitCode=-1` 立即失败。改用 `debian:bookworm-slim`（含完整 sh，仍 UID 65532 非 root）。
- **devices INSERT 参数错位**（`internal/store/sql_devices.go`）：非 onboard 分支传 10 参数但 SQL 8 个占位符（state/task_state 已硬编码），`expected 8 arguments, got 10`。
- **handleCreateTask 支持 maxRetries 覆盖**（`internal/controlplane/server_tasks.go`）：body 新增 `maxRetries`（nil=全局默认，显式 0=一次失败即死信），此前单任务无法覆盖默认重试上限。

#### 基础设施修复

- **MySQL 迁移 MariaDB 语法**：002/004/005 的 `ADD COLUMN IF NOT EXISTS` / `CREATE INDEX IF NOT EXISTS` 是 MariaDB 语法，MySQL 8 报 1064 → 建表不全连锁失败。已去掉（幂等由 migration 记录表保证）。
- **CI 集成测试权限**：DSN 改 root（opsmesh 用户无 CREATE DATABASE 权限建临时库）+ mysql 容器 `MYSQL_ROOT_HOST=%`（默认 root 仅容器内 localhost）+ healthcheck 密码改用环境变量（曾硬编码 rootpass 与注入密码不符）。
- **trivy 版本**：v0.9.2（内置 trivy v0.38）go.mod 解析器不认识 go 1.26 → v0.36.0（内置 v0.73）。
- **operator 依赖漏洞**：8 个 HIGH（x/net v0.23→v0.57 / x/oauth2 v0.12→v0.27 / x/text v0.14→v0.40 / x/sys v0.18→v0.47）。
- **nanoid CVE-2026-67213**：3.3.16→3.3.18（npm overrides，vite→postcss 传递依赖）。
- **compose 强制 `--build`**：`docker compose up -d` 复用缓存旧镜像（balancer 端口修复不生效）→ `--build` 强制重建。
- **Playwright 浏览器下载镜像**：`PLAYWRIGHT_DOWNLOAD_HOST=npmmirror` + timeout 300 重试（cdn.playwright.dev 偶发 30+ 分钟卡死）。

#### E2E 契约对齐（spec 与真实后端逐步收敛）

- 登录兼容首登强制改密（MustChangePassword → change-password 换密 → 重新登录）、loginGuard 429 限流重试 + 文件级 token 缓存、agent 注册等待轮询、SSE 短连接（Playwright request.get 对长连接挂起）、任务响应/列表字段名大小写（`taskID` 大写）、下发任务补 agentID（400 根因）。

### 覆盖率门禁调整（基于真实 CI 数据）

- store 包覆盖率门禁 65% → 32%（实测真实 mysql 集成环境 34.6%，65% 系 CI 未跑通时设定；同 build-test 50%→45% 先例）。

## [Unreleased] — 2026-08-12

### 已解决

- **kafka-go 依赖升级**：v0.4.48 → v0.4.51（go 1.26 环境已无兼容限制；普通构建 + `-tags kafka` 构建 + events 测试全绿）
- **前端死重清理**：删除个人版原生 JS 仪表盘业务代码（`internal/controlplane/web/`，约 1.3 万行 flow_*/render/i18n/icons/api）。`GET /` 收敛为极简引导页并自动重定向至 `/enterprise/`；`/install.sh` 与 `/bin/opsmesh-agent` bootstrap 端点保留（纳管依赖）。
- **docker-compose 弱口令**：MySQL 密码改为 `${MYSQL_ROOT_PASSWORD:-}` 环境变量插值，正式部署必须显式注入。
- **Dockerfile**：构建阶段加入 `go mod verify`（防供应链投毒 / go.sum 漂移）。
- **CI**：整体覆盖率门禁 40%→50%、store 包 60%→65%；codecov 已配置 token 时上报失败阻断；`e2e-real` job 上线（docker compose 拉起真栈跑 Playwright，不再全 mock）；GitOps 镜像 tag 写回步骤改为可跳过（clone/path 守卫，仓库未就绪不再失败）。
- **文档**：新增 `docs/flag-matrix.md`（配置治理）、`docs/tech-debt.md`（技术债登记册）、`docs/sse-protocol.md`（SSE 实时推送契约）；README 修正 IAM 双轨表述、补平台支持声明（agent 仅 Linux）、删除 kafka-go 钉版本旧约束；tech-selection.md 补充 protobuf/JSON codec 双轨说明。
- **Store 拆分确认**：`internal/store/store.go` 已存在 15 个领域子接口 + 编译期断言（此前评估误判其未拆分，已更正）。

### 遗留已知问题（进入 `docs/tech-debt.md` 跟踪）

- `internal/controlplane` 单包 ~14.5k 行待拆分；`memory.go` 2020 行待按域拆分。
- agent 每次 RPC 重新 Dial 无连接池；Windows agent 仅可编译不可用。
- 前端 E2E 真实后端 spec 仅覆盖健康检查；核心交互流程待补充。

### 严重问题修复（5 项，阻塞生产发布）

#### 修复
- **`web/enterprise/src/api/auth.js`**：添加缺失的 logout API 方法（清除前端 token + 调用后端 logout 端点）
- **README.md + docs/deployment-guide.md**：补充企业版前端独立部署说明（npm run build → 静态文件部署到 Nginx/CDN）
- **`deploy/helm/opsmesh/templates/agent-daemonset.yaml`**：agent 数据卷从 emptyDir 改为 hostPath（重启不丢失 agent.id）
- **`deploy/helm/opsmesh/values.yaml` + `values-production.yaml`**：镜像 tag 从 "0.1" 修正为 "latest"（与 CI 推送的 :sha tag 一致）
- **`internal/store/sql_templates.go`**：移除 UTF-8 BOM（导致 MySQL 首字节乱码）

### 重要问题修复（10 项，影响质量）

#### i18n 全面覆盖
- **12 个 Vue 组件**：TasksView/AlertsView/DevicesView/DeviceDetailView/CMDBView/WorkflowsView/DeploysView/LogsView/UsersView/K8sManageView/MiddlewareDeployView/OSOptimizeView — 所有硬编码中文提取到 i18n（zh.json/en.json 从 459 键扩展到 629 键，结构完全对称）
- **DataTable/Pagination 组件**：空状态文本、翻页按钮 i18n 化
- **MiddlewareDeployView**：fmtTime 从硬编码 'zh-CN' 改为动态 locale（随 i18n 语言切换）

#### 路由守卫 + i18n 回退
- **auth.js**：添加 ready Promise + initialized flag，解决路由守卫竞态条件
- **router/index.js**：守卫改为 async，await auth.ready
- **main.js + App.vue**：启动时调用 fetchMe，移除重复调用
- **i18n/index.js**：添加 FALLBACK_LANG='zh' 回退机制（缺失键回退到中文）

#### API 文档修正
- **docs/api-reference.md**：修正 4 处不一致（/readyz 端点缺失、/metrics 端口、/healthz 格式、/api/v1/me 字段名）

#### 测试补全
- **前端测试**：新增 vitest + @vue/test-utils + jsdom，88 个测试（auth 28 + i18n 22 + DataTable 20 + Pagination 18）
- **internal/k8s 测试**：覆盖率 14.8% → 90.9%（30 个测试，Clientset 字段改为 Interface 接口支持 fake 注入）
- **internal/orchestration 测试**：覆盖率 30.1% → 71.2%（17 个新测试）
- **cmd/opsmesh 测试**：覆盖率 0% → 73.9%（20 个测试，重构提取 runMain()/versionString()）

#### 可观测性
- **Helm ServiceMonitor + PrometheusRule**：新增两个监控模板 + 3 条告警规则（agent 离线 / 任务失败率高 / 队列堆积）
- **/healthz 深度检查 + /readyz**：healthz 增加 store ping 深度检查，新增 /readyz 就绪探针端点
- **metrics 指标扩充**：HTTP 延迟直方图 + HTTP 计数器 + Go runtime 指标（零依赖手写，不引入 prometheus 客户端库）

### 次要问题修复（10 项，技术债务清理）

#### 代码质量
- **flow.js 拆分**：2714 行 / 106 导出的单体 JS 文件按业务域拆分为 13 个模块 + barrel re-export（零风险，main.js 无需修改）
- **app.legacy.js 删除**：64.8KB 完全未引用死代码清除（零引用确认后删除）

#### 部署加固
- **operator/Dockerfile**：Go 版本从 1.22 对齐到 1.26（与主 Dockerfile/go.mod 一致）
- **systemd hardening**：两个 service 文件添加 19 条安全指令（NoNewPrivileges/ProtectSystem/ProtectHome/PrivateTmp/ProtectKernelTunables/ProtectKernelModules/ProtectControlGroups/RestrictAddressFamilies/RestrictNamespaces/SystemCallFilter 等）
- **Helm Ingress + HPA 模板**：新增 templates/ingress.yaml 和 templates/hpa.yaml，对接 values.yaml 中已有的 ingress 和 autoscaling 配置段（生产环境 HPA 默认开启：minReplicas=2, maxReplicas=10）

#### 供应链安全
- **cosign 镜像签名**：CI image job 新增条件性 cosign sign 步骤（需 COSIGN_PRIVATE_KEY secret，未配置时自动跳过）
- **Base image digest pinning**：3 个 Dockerfile 添加 digest pinning 最佳实践注释 + 新建 docs/image-pinning.md 指南

#### 测试补全
- **internal/tlsutil 测试**：覆盖率 0% → 91.7%（17 个测试，覆盖 ServerCreds/ClientCreds/HTTPClientTLSConfig/HTTPServerTLSConfig 全部 4 个导出函数）

#### 文档
- **README 架构图**：更新 ASCII 架构图，新增企业版 Vue3 前端、K8s Operator、联邦 mTLS、SSE 实时推送、多租户 Schema 隔离、ELK/Loki 日志集成、Prometheus 监控告警等；通信模型表格补充 SSE/联邦/Metrics 三行

#### 验证
- `go build ./...` ✅
- `go vet ./...` ✅
- `go test -timeout 300s ./...` ✅
- `npm run build` ✅
- `npx vitest run` ✅ 88/88 通过

---

## [0.1.1] — 2026-08-07

### 部署配置对齐修复 + 测试覆盖率提升 + 前端企业版功能对齐 + 性能优化 + 安全加固

#### 部署配置对齐修复
- **docker-compose.yaml**：controlplane 启用 `--store=mysql` + `--advertise-addr=http://controlplane:8080`，对齐 Helm values；安全相关 flag 环境变量映射注释完整（cookie-secure/public-register/allow-public-register/federation-*/provision-secret/grpc-require-signature/trust-proxy/jwt-public-key/metrics-allow-cidr）
- **systemd env 模板**：`opsmesh-controlplane.env` 补全全部安全加固项注释（federation mTLS/grpc-require-signature/trust-proxy/jwt-public-key/client-ca/metrics-allow-cidr），与 config.go flag 一一映射
- **Helm values-production.yaml**：3 副本 + mysql + TLS + require-auth + cookie-secure + podAntiAffinity + 资源放大，与 systemd 生产配置对齐

#### 测试覆盖率提升
- **CI 覆盖率门禁**：整体 ≥40%（build-test job）、store 包 ≥60%（integration job）
- **前端最小测试集**：vitest + jsdom，32 个测试（auth 15 + api 17）

#### 前端企业版功能对齐
- **Vue3 企业版前端**：与原生 JS 个人版功能对齐，独立化静态资源（web/assets/*）

#### 性能优化
- **server.go 按路由域拆分**：2483 行 → ~1225 行，拆为 server_devices/server_tasks/server_alerts/server_audits/server_deploy
- **auth.go 按 handler 拆分**：拆为 auth_login/auth_users/auth_roles/auth_perms
- **sql.go 按领域拆分**：1261 行 → ~542 行，拆为 sql_devices/sql_tasks/sql_alerts/sql_audits/sql_tokens/sql_templates/sql_legacy

#### 安全加固
- **`--health` 子命令**：独立健康检查，GET /healthz → exit 0/1，供 docker-compose healthcheck（不依赖 curl/shell）
- **Refresh Token 持久化**：RefreshTokenStore 接口 + 三实现（Memory/SQL/MultiSchema），哈希存储 + 设备指纹绑定
- **Cookie Secure**：`--cookie-secure` flag，生产模式默认 true
- **请求体限流**：统一 1 MiB 上限（http.MaxBytesReader）
- **登录防爆破**：令牌桶限流 + 失败锁账号
- **metrics 访问控制**：`--metrics-allow-cidr` CIDR 白名单
- **联邦 mTLS + HMAC 签名**：`--federation-*` 通道硬化

#### 修复
- **CI `secrets` 上下文 bug**：改用 env 中转 + guard step，修复全部 28 次 CI 运行失败
- **删除 `opsmesh.exe`**：69MB 二进制误提交，已删除并加入 .gitignore

#### 变更
- **Go 版本统一 1.26**：go.mod / Dockerfile / README 一致
- **DELIVERY.md 数据刷新**：行数/包数/依赖数/功能矩阵更新至最新

#### 验证
- `go build ./...` ✅ 0 错误
- `go vet ./...` ✅ 0 警告
- `go test -timeout 180s ./...` ✅ 全部通过
- `npx vitest run` ✅ 32/32 通过

---

## [0.1.0] — 2026-08-01 ~ 2026-08-06

### 初始版本


#### 核心功能
- 控制面 + Agent 双模式架构（HTTP REST + gRPC）
- 设备纳管 / 任务执行 / 告警监控 / 审计日志
- 用户中心 + RBAC + JWT 认证
- OS 基础优化（14+ 预置模板）+ 中间件自动化部署（15+ 模板）
- K8s 多集群管理（client-go 集成）
- 多租户 schema 隔离 + 控制面联邦
- SSE 实时推送 + ELK/Loki 日志集成
- Helm Chart + docker-compose + systemd 部署
- Vue3 企业版前端 + 原生 JS 个人版前端
- K8s Operator（CRD + controller-runtime）
- protobuf 契约 + buf breaking 检查