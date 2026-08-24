# OpsMesh 发布说明

本文件记录 OpsMesh 各版本的发布说明，按版本号倒序排列。版本号遵循 [Semantic Versioning](https://semver.org/)，格式参考 [Keep a Changelog](https://keepachangelog.com/)。

---

## v0.5.0 — 2026-08-24 文档全面同步批次

本次发布为**文档同步批次独立发版**，聚焦 2026-08-24 文档全面同步、第三轮安全终审修复、前端 P0/P1/P2 修复、质量与覆盖率提升以及部署加固。本次版本号独立于内核演进主线（内核已到 0.7.0），用于标记文档与配套修复的发布节点。

### 新功能

- **README 功能矩阵扩展**：功能域从原范围扩展为 14 个（设备管理 / 任务执行 / 监控告警 / CMDB / 日志检索 / 编排部署 / OS 优化 / 中间件部署 / K8s 管理 / 用户中心 / 审计日志 / 联邦 / SSE 实时推送 / 工作流），对齐 `docs/feature-design.md` F1–F18 与 `docs/product-roadmap.md` M1–M4
- **README 技术栈章节**：新增「技术栈」章节（Go 1.26 + Vue3 + Vite + Pinia + MySQL + Redis + gRPC + OTel）与「internal 包职责（30 个）」章节，按 7 个领域分组列出全部 internal 包
- **API 端点补全**：
  - `GET /api/v1/devices/{id}/metrics`（设备监控指标，支持 `?range=15m|1h|2h|6h|24h` 历史时序）
  - K8s 资源管理章节补全 15 个端点（namespace / pod / deployment + scale/restart/rollback / service / configmap / secret / node / dashboard / health）
- **技术债登记**：新增 TD-50~TD-54（controlplane 覆盖率 / helm 覆盖率 / discover-discovery 边界 / 文档同步 / 版本发布流程）

### 改进

- **文档边界澄清**：明确 `internal/discover`（设备发现，控制面→网段找设备）与 `internal/discovery`（控制面服务发现 + 负载均衡，agent→控制面 failover）的边界
- **快速启动补全**：README 快速启动补充 docker-compose / Helm / systemd 三种部署方式
- **开发指引补全**：README 开发指引补全 30 个 internal 包（新增 alertengine / approval / circuitbreaker / discovery / helm / k8s / otelx / provision / secrets）
- **交付清单刷新**：DELIVERY.md 代码规模刷新至 2026-08-24（179 源码 + 167 测试 = 346 Go 文件，84 前端文件，34 包），功能交付清单对齐 14 个功能域
- **构建上下文瘦身**：`.dockerignore` 补全，构建上下文从 ~250MB 缩减至 215KB
- **工具链锁定**：toolchain 锁定 go1.26.6，解决多版本冲突

### 修复

#### 安全（第三轮终审 P0/P1/P2）

- **多处安全漏洞与部署阻断修复**（`35e2375`）：security/deploy/store 多处安全漏洞与部署阻断修复
- **refresh token 过期清理**（`5199f4e`）：周期清理过期刷新令牌 + blacklist，避免 goroutine 泄漏
- **demo JWT 默认密钥移除**（`f0fc51e`）：未设置 `OPSMESH_JWT_SECRET` 时二进制自动生成随机密钥（重启后旧 token 失效），生产务必显式注入
- **rows.Err() 补齐 20 处**（`f0fc51e`）：SQL 迭代错误路径覆盖
- **Dockerfile digest 钉死**（`af9a914`）：base image 摘要固定，防供应链漂移

#### 前端

- **HttpOnly Cookie 会话恢复**（`3af70a7`，P0）：修复 SSE 帧分割边界残留
- **SSE 401 刷新重连**（`612d59b`，P1）：URL 编码统一 + i18n 错误消息
- **列标题 i18n 化**（`20629b2`，P2）：vite 代理环境变量 + eslint 恢复 no-v-html + 移除 msw
- **E2E 断言改用 data-testid**（`85d4d2f`）：替代中文文案，防语言切换失败

#### 质量

- **去 AI 化全面收尾**（`9014081`）：注释 / 标识符 / 文档清理 + TestExecute_Timeout 阈值修复
- **log.Fatalf → return error**（`e3f9324`，P1）：错误处理规范化
- **flaky 测试根治**（`08827f8`）：CMDB 节流测试与采集耗时解耦 + E2E 超时预算放宽

#### 部署

- **HPA replicas 冲突修复**（`5199f4e`）：Helm Chart HPA 与 Deployment replicas 去冲突
- **NetworkPolicy 补全**（`5199f4e`）：Helm Chart 网络策略加固
- **pipefail 补全**（`5199f4e`）：CI shell 脚本 pipefail 加固

#### CI

- **CI release 触发/secret 复用修复**（`cb2f58a`）：审计遗留问题修复
- **SSE 契约/canceled 拼写修正**（`5199f4e`）：五态 dead_letter 修正

### 覆盖率提升

- **store 包覆盖率 75.7%**（`3e86452`）：BadDB 方法覆盖 SQL 错误路径（49.8% → 57.5% → 75.7%）
- **6 个低覆盖包补全**（`17708be`）：
  - grpcx：99.5%
  - otelx：97.2%
  - secrets：96.4%
  - authctx：93.1%
  - cmdb：95.4%
  - deploy：81.0%

### 已知问题

- `internal/controlplane` 单包 ~14.5k 行待拆分；`memory.go` 2020 行待按域拆分（见 `docs/tech-debt.md`）
- agent 每次 RPC 重新 Dial 无连接池；Windows agent 仅可编译不可用
- 前端 E2E 真实后端 spec 仅覆盖健康检查；核心交互流程待补充
- demo 模式下未显式注入 `OPSMESH_JWT_SECRET` 时，重启后旧 token 失效（设计如此，生产务必显式注入）

### 升级指南

1. **备份现有数据**：升级前务必备份 MySQL 数据与配置文件
2. **显式注入 JWT 密钥**：本次移除 demo JWT 默认密钥，生产环境必须显式设置 `OPSMESH_JWT_SECRET` 环境变量，否则重启后旧 token 全部失效
3. **更新工具链**：本地开发环境请对齐 Go 1.26.6（toolchain 已锁定）
4. **检查 HPA 配置**：若启用 Helm Chart HPA，确认 Deployment replicas 与 HPA minReplicas 不冲突（本次已修复 Chart，但自定义 values 需自查）
5. **重建镜像**：Dockerfile base image 已 digest 钉死，建议执行 `docker compose up -d --build` 强制重建镜像
6. **验证**：升级后执行 `opsmesh --version` 确认输出 `0.5.0`，并检查 `/healthz` 与 `/readyz` 端点

### 验证

- `go build ./...` ✅
- `go vet ./...` ✅
- `go test -timeout 300s ./...` ✅

---

## 历史版本

- **v0.7.0**（2026-08-16）：CMDB 关系图谱可视化 + 全文本检索倒排索引 + 多集群联邦发布 + 13 个核心设计文档建立 + 测试覆盖率提升。详见 `CHANGELOG.md`。
- **v0.1.1**（2026-08-07）：部署配置对齐修复 + 测试覆盖率提升 + 前端企业版功能对齐 + 性能优化 + 安全加固。详见 `CHANGELOG.md`。
- **v0.1.0**（2026-08-01 ~ 2026-08-06）：初始版本，控制面 + Agent 双模式架构及核心功能。详见 `CHANGELOG.md`。