# OpsMesh 发布说明

本文件记录 OpsMesh 各版本的发布说明，按版本号倒序排列。版本号遵循 [Semantic Versioning](https://semver.org/)，格式参考 [Keep a Changelog](https://keepachangelog.com/)。

---

## v0.9.2 — 2026-09-27 商用就绪收口 + 发版链路加固

本次发布为**商用就绪评估收口批次**：P0/P1 全量修复、可支撑性能力补齐、发布链路从「首次真跑」推进到「带签名与 SBOM 的全链产物」，并推进 TD-60 阶段 2 三域生产路径。重切 tag `9347554` 后四条验收全部达成。

### 商用就绪收口（P0 / P1 全量）

- **P0-1 / P0-2 / P0-4 / P0-7**：预置弱口令可被公开接管（初始口令显式交付 + 首登强制改密）、生产模式 Web/REST 明文 HTTP（`--http-tls`）、主要部署资产开箱即坏（compose 端口丢失 / 监控假告警）、微服务持久化静默降级（DSN 幂等归一 + fail-fast）——真机 `verify-runtime.sh` **PASS=38 / FAIL=0**（详见 `docs/commercial-readiness-review-2026-09-25.md` §9）
- **P0-3 / P0-5 / P0-6 / P1-8**：企业版前端交付路径、迁移安全、多租户隔离、子存储失败静默降级改 fail-fast（`206247c`）
- **P1-1 / P1-3 / P1-4 / P1-5**：白名单绕过、审计哈希链防篡改（180 天）、无界内存上界、指标熔断限流（`4fd6411`）
- **P1-2 agent 身份绑定**：per-agent 密钥 + 载荷签名 + 环境隔离（`01475e6`）

### 可支撑性（P1-6）

- **新增端点**：版本 / 日志级别 / 配置转储（脱敏）/ 诊断包 / pprof，并修掉四处静默失效（`5c7a052`）
- **运行期控制**：日志级别运行期开关（匿名 POST `/admin/loglevel` 实测 401）、metrics 计数 TTL 缓存、17 服务日志接入统一管道（`205388b` `c78532e`）

### 发布链路与 CI 加固

- **镜像发布名与消费方引用收敛**：CI 发布名 ↔ chart / 文档 / operator CRD 同时对齐 + 标签策略（sha / latest / X.Y.Z，feature 分支刻意不打 latest）+ 三道防分叉门禁（含 `tr` 区间陷阱与 `helm template` 四组合实测）（`29614da`）
- **两轮发版尝试失败的根因修复**：`Dockerfile.service` COPY 不存在的 `go.work.sum`；发布矩阵混入非服务模块（tf-provider）——修复后 19/19 镜像全链发布（`c6f0a2e`）
- **镜像 job 私有 / GHCR 双路径**消除「空转绿」+ keyless 签名（cosign v2.2.4）+ SBOM（syft v1.51.1）；签名自验证改有界退避重试（GHCR 写后读窗口，判据不放宽）——最终 tag 头提交（`a939d61`）
- **CI 首跑暴露 3 处失败 + 被掩盖的第 4 处夹具缺陷**；`go mod download` 5 次退避重试；build-test 内存型 flaky 仅 OOM 重试一次；actionlint v1.7.7 入 CI（12 条 shellcheck 真修）（`68ff539` `51b0333` `184ec29` `21bae16`）
- **解除 golangci-lint 阻塞**（8 项告警，下游 7 个 job 自 2026-09-20 起全被 skip）（`b67fa59`）
- **Trivy**：agent 镜像 56 条 HIGH/CRITICAL 均无上游修复 → `ignore-unfixed` 登记留档（`0dc4ece`）

### TD-60 阶段 2（三域生产路径）

- A-2 三域生产路径补齐（`4a85fefe`）+ 切流前缺失端点（`125828962`）；task-svc M5 增强 batch-exec / canary / approval 与 schedules pause/resume 对齐（`9fba2955` `00c5310c`）
- 双轨阻塞修复：task-svc 存储错误贯穿 + auth-svc 鉴权对齐；ShadowMode 真正只读 + 裸数组响应（`d449718e` `e05c2fb`）

### 监控与交付资产

- **出厂告警对账**：告警与面板引用的每个序列经核在抓取面真实存在（`e292be4`）
- **交付脚本首次静态检查**：13 个 bash 入口 shellcheck——修掉哑按钮、口令字符集声明不实、「权限 0600」不实陈述等（`a9e4b4f`）

### 升级演练（0.9.0 → 0.9.2）

- 用真的 0.9.0 二进制 + 隔离 MySQL 建老库（17 迁移），换 0.9.2 二进制升级至 **19/19**：`019_audit_chain` 幂等放行、老用户回填 `tenant_id=default`、链校验如实标注遗留行、客户改过的口令哈希不被 seed 覆盖
- **实抓缺陷**：预置账号的强制改密标记随每次重启复活（客户可见）——改为仅当哈希仍等于预置口令时才补标记，回归测试 + 变异检验证明会红；修复含入最终 tag（`375f7549`）

### 已知问题

- Prometheus `/metrics` 暴露面此时仍限于 alert / device / task 三个服务（缺口已列入后续批次）
- agent 镜像 56 条 HIGH/CRITICAL 为无上游修复（`ignore-unfixed` 登记在案）

### 验证

- 重切后四条验收全部达成（tag `9347554`，run `36327166836`，19/19 job success）：① Release assets 5 个；② 核心镜像 `:0.9.2` 可解析且 `.sig` 200；③ 微服务镜像 17/17 发布；④ 17/17 微服务镜像带 `.sig`（加核心共 19/19）+ 17 份 SBOM 产物
- 静态门禁 `validate-deploy-assets.sh` → PASS=30 / FAIL=0 / SKIP=0（CI step 级证据）
- 同 tag 的 `ci` run 有一处 actionlint style 级红（`release.yml:163` SC2004，不影响产物链），已事后修复

---

## v0.9.1 — 2026-09-17 全面评估修复（35 项发现全量落地）

基于 6 维度全面评估（架构 / 质量债务 / 测试 CI / 安全 / 文档契约 / 运维部署），35 项发现全部修复。

### 高风险（H1–H8，全部修复）

- **H1**：controlplane 5 个超长文件拆分为 22 个 ≤500 行（`a2546b6`）
- **H3**：gRPC AgentService 适配层解耦（`ec835b7`）；**H2** 注释修正（`7c7d100`）；**H4** 文档包数统一 + CI 校验（`446782f`）
- **H5 / H6 / H8**（`cab1c7d`）：Operator Privileged → capabilities 白名单；MySQL 明文密码 → SecretKeySelector；Operator 补齐 metrics / probe / resources / securityContext
- **H7**：微服务端口统一 9091 + ServiceMonitor（`7c7d100`）

### 中风险（27 项，全部修复）

- **M1–M15**：文档对齐 / 供应链加固 / 代码安全加固；**M7** KMS provider 实现（HTTP API 解密）；**M14** 根模块路径合法化
- **S1–S12**：API Key 熵 / mTLS / 签名校验 / ChainProvider 降级等

### TD-60 阶段 2 双轨批次（并入本 tag）

- 阶段 2 第一批：task-svc 双轨对照补齐（`a4d819d`）；D1 device-svc HTTP 网关接入 + Shadow 模式（`7aeb388`）；D2 Discovery 真实化（`1e1d0aa`）
- A1+A2 auth-svc 方案 B 用户中心后端（`3aae39b` `1761793`）；双轨观察 GH Actions 落地 + 双 NULL 扫描 bug 清剿（`07447da` → `9506f8c`）
- 第十二轮：技术债 TD-60~64 全量复核 + 留档小项清零（`be272e8`）

### 产物说明

- **本版本没有镜像与二进制产物**：Release 为空壳（`assets=0`）——经核由打 tag 时创建，两条发布路径当时均判 skip，流水线自身门禁正确（详见 `CHANGELOG.md` 2026-09-26 条目）。发布链问题在 v0.9.2 期间修复，重切标签后完成首度全链产物交付

### 验证

- `go build ./...` ✅ / `go vet ./...` ✅ / `golangci-lint run ./...` 0 issues ✅ / `go test ./...` 57 包全过 ✅

---

## v0.9.0 — 2026-09-05 UI 覆盖面清零 + 六域微服务接线 + 安全清零

### 版本亮点（相对 v0.8.0）

- **UI 覆盖面清零**：20 个后端域补齐管理页面（19 view + 19 api 封装 + 46 路由 + i18n 中英对齐）——此前只能 curl 操作的域全部可视化（计费 / API Key / 网关路由 / 审计事件 / 通知渠道 / 定时任务 / 自动化 / Webhook / 脚本 / 工单 / SLO / 流量策略 / 流水线 / ArgoCD / 合规 / HA / 备份 / 配额 / 租户）
- **前端 P0-P3 功能补齐**：企业版多子域（Helm 应用商店等）+ 个人版功能域 + 幽灵 API 修复
- **六域微服务接线闭环**：controlplane 聚合层（`service_proxy` 五域反向代理 + `bot_bridge` ChatOps Web 命令台）+ RBAC 12 权限点 + 部署配置（5 服务 Dockerfile + compose + helm values，默认 disabled 零行为变化）
- **测试规模翻倍**：前端 631 → **1121 用例**全绿；**pkg/ 12/12 包全覆盖**（+130 用例），测试驱动实抓 3 个真 bug——migrate `Rollback` 记账反向（真实 MySQL 回滚必炸）、tenant `RequireTenant` 403 不可达、tenant `EnforceQuota` 数据竞争（CI `-race` 实测）
- **安全清零**：CVE-2026-84304（grpc HIGH）10 模块升 v1.83.1；openssl / musl CVE 随 alpine 3.23 + `apk upgrade` 修复；GO-2026-5932（openpgp）经符号级不可达确认后豁免留档；36 文件 BOM 污染剥离；Trivy 扫描报告 artifact 取证通道
- **其他**：`database-design.md` 补档 55 表（007-017 迁移全量入档）

### 验证

- 发布链第二次全链真跑：镜像 8/8 + goreleaser 13/13 ✅
- 前端 vitest 1121/1121 ✅

---

## v0.8.0 — 2026-09-01 CI 全绿攻坚 + 首次全链发布

五 / 六 / 七轮合并发布：从「CI 从未跑通」到 11/11 job 全绿，并完成首次真实全链发布。

### 安全

- **修复生产越权**：裸 `X-Tenant-ID` 头可冒充任意租户（E2E-sec 实测 200 穿透）——`requireAuth` 下默认 401，显式 `--trust-gateway-headers` 才放行
- **修复生产静默丢数据**：MultiSchemaStore 从未建 schema，首租户写入即失败且静默——先建库再连
- **备份 DATA RACE**：CI `-race` 实证的指针竞争——goroutine 持独立副本
- **依赖 CVE 清零**：x/crypto CRITICAL + x/net HIGH 等，19 个模块全部升级

### CI/CD

- **11/11 job 全绿**（23 连红 → 全绿，19 个修复提交）：lint 112 项、E2E-sec 78 用例、agent OOM 三层根因、MySQL 8.0 保留字迁移链修复
- **release 全链首次真跑打通**：6 微服务镜像（GHCR + Trivy 零 HIGH/CRITICAL）+ 二进制产物
- runtime 基础镜像 alpine:3.19（EOL）→ 3.23 升级

### 告警正确性

- notifyLoop 水位线跨租户漏推 → **AlertID 指纹去重**（对乱序 / 时钟偏差免疫，推送失败撤销下轮重试）

### 部署

- Helm 微服务部署通道（18 服务 range 模板，默认 enabled=false 零行为变化）

### 验证

- Release 5 个资产：linux/amd64 + arm64 tar.gz + SBOM×2 + checksums.txt（amd64 实测 SHA256 与 checksums.txt 逐字节一致）
- GHCR 6 微服务镜像带 `0.8.0` / `latest` / 每 SHA 标签，Trivy 扫描零 HIGH/CRITICAL

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