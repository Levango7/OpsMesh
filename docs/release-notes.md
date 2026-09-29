# OpsMesh 发布说明

本文件记录 OpsMesh 各版本的发布说明，按版本号倒序排列。版本号遵循 [Semantic Versioning](https://semver.org/)，格式参考 [Keep a Changelog](https://keepachangelog.com/)。

---

## v0.10.0 — 2026-09-29 TD-60 阶段 2 收口：五服务删除 + 域完善与身份头治理 + 流量取数出口

本次发布把 TD-60「先接通、再裁决」推进到**可裁决**状态：五域接线与权限逐条镜像本地、五个从未进部署清单的服务删除、两个真 bug 修复（自动伸缩指标读取恒失败、RBAC 权限目录缺失导致含 admin 一律 403）、代理身份头统一治理（多租户落错桶 + 同租户内审计伪造），并补上裁决所缺的真实流量取数出口。因含三类破坏性变更，按 0.x 惯例走 minor。

### 破坏性变更（升级前必读）

1. **五个服务模块删除**：`deploy-svc` / `plugin-svc` / `bot-svc` / `workflow-svc` / `grafana-bridge`。判据 = 从未进任何部署清单 + 无消费方 + 能力已被替代（`internal/deploy`+`internal/helm`、`internal/plugin`+迁移 015、聚合层 `bot_bridge.go`、`internal/orchestration`、Prometheus 直抓 `/metrics`）。**影响面**：`services/` 18→13（12 服务 + tf-provider 工具链）、`release.yml` 镜像矩阵 17→12、Helm chart 删 4 个服务段、`init-mysql.sql` 删 workflow/plugin 建库段、`prometheus.yml` / `verify-runtime.sh` / 抓取豁免清单同步、集成测试端口表 12→9。自建镜像流水线或直接引用上述镜像 tag 的客户需清理引用。
2. **代理域写方法权限收紧**：gpu/runbook/incident/autoscaler/portal 五域与 device/task 的写方法此前统一只校验 `*:read`——持有只读凭证可经代理完成创建 GPU 负载、执行 Runbook、审批门户请求甚至删除设备。现按 `permRules` 逐条镜像本地：写方法→`*:write`、`DELETE /device-svc/devices/{id}`→`device:delete`、provision→`provision:execute`。**影响**：以只读角色经代理前缀做写操作的集成会开始 403（前端页面守卫不变，仍是 `*:read` 准入；动作级校验本就该在服务端）。
3. **代理身份头一律剥离重注入 + X-User-Id 交叉校验**：客户端自带的 `X-Tenant-ID` / `X-User-Id` / `X-User-Roles` 在 director 里全部剥离，改以聚合层已校验身份重注入（`X-User-Roles` 仅剥离，下游无消费方）；令牌用户与 `X-User-Id` 头不一致直接 403 `user mismatch`。**影响**：依赖「自发身份头直连控制面」的部署（README IAM 路径 B）必须显式声明 `--trust-gateway-headers=true`（生产模式强制 false，该模式下网关头为权威声明）。**收益**：gpu-svc / portal-svc 多租户数据不再兜底落 `default` 桶；同租户内伪造审计主体的链路关闭。

### 新增能力

- **task-svc 接通为第二代理域**：双轨前缀 `/api/v1/task-svc/*` → task-svc HTTP 网关（容器内 8102），`TASK_SVC_URL` 进 prod / dual-track compose；新机制 `permRules`（方法+路径→权限点，首条命中）使代理与本地对同一操作的权限要求逐一对齐。**约定**：规则路径按上游（改写后）形态书写，匹配前先 `rewriteProxyPath`。
- **`GET /api/v1/admin/service-traffic`**：逐域真实流量聚合（域→前缀派生自转发路由表，与 `/admin/service-routing` 同源）。TD-60 §5.3 把五域裁决定为「待真实流量」却无取数出口，本端点补上；零流量域显式返回 `requests=0`。窗口是进程内计数（重启归零），跨重启/多副本用 PromQL `increase(opsmesh_http_requests_total{path=~"/api/v1/<前缀>(/.*)?"}[Nd])`。裁决阈值口径与两条判读陷阱见 `docs/td60-decision-2026-09-26.md` §5.8。
- **portal-svc 前端契约补齐**：`GET /api/v1/approvals`、`POST /api/v1/approvals/{id}/approve|reject`、`GET /api/v1/cost`（14 天窗口，仅 approved/fulfilled 计入分摊）三组端点；`requestView` camelCase 视图层 + 列表 `{requests:[...]}` 包裹；`createRequest` 双解析兼容前端体与旧 snake_case 体；租户/请求人从身份头回退。
- **RBAC 权限目录 87→102**：15 个被 handler 的 `requireProd` 引用却从未进 `rbacPermSpecs` 的权限串（`schedule:*` / `approval:*` / `task:approve` / `helm:*` / `quota:*` / `secrets:*` / `alert·middleware·os:write`）⇒ **任何角色含 admin 恒 403**（`git log -S` 印证从未存在，全站性缺陷）。修复含老库预置角色并集回填自愈 + `sql_rbac_catalog_test.go` 守护（含派生效应断言）。

### 缺陷修复

- **autoscaler-svc 指标读取恒失败**：`ReadMetric` 请求 `/api/v1/query`（JSON API）却按 exposition 文本逐行解析，真实 Prometheus 下恒报 `no metric found`、评估器恒 `no_action`——自动伸缩链路第一步即断。改按 JSON 契约解析（`status`/`errorType` 报错、空 `result` 保留原语义、`value[1]` 非数字显式报错）。
- **portal 前端数据面全不可用**：三端点 404、`GET /api/v1/requests` 返回 snake_case 裸数组致前端列表全空、`POST /api/v1/requests` 因缺 title 400。
- **aio-svc 噪声压缩测试偶发失败**：去重按分钟桶（`FiredAt.Unix()/60`）而样本用 `time.Now()`+10s，跨分钟边界不合并（CI run 36453413667 实测）→ 测试改用固定桶内时间戳。
- **本仓 self-inflicted**：批次① 在 portal-svc 引入的 `_ = json.NewDecoder(...).Decode(...)` 被 errcheck `check-blank`（TD-71 收紧档）拦下，令 `services` job 红；改用仓库既有 best-effort 惯例（`io.EOF` 按零值继续、其余落日志留痕）。

### 工程与门禁

- **errcheck 收紧档全量收口（TD-71 收官）**：`check-blank` + `check-type-assertions` 开启后 +150 处清零（131 修 + 19 有据豁免），19 模块严格档零报点；`.golangci.services.yml` YAML 坏损修复（此前 HEAD 加载必失败，CI 实测红）。
- **`-race` 打通**：本机补 PATH（msys64 gcc 16.1.0）后开启竞态检测，全量跑根模块 + 全部服务模块，抓到两处普通测试视野外的竞态并修复。

### 升级须知

- **版本源已同步至 0.10.0**：`Chart.yaml`（version/appVersion）、`values-production.yaml` 两处 tag、`gitops/segments/production-segment.yaml` tag、`internal/version.Version`、`deploy/k8s/deployments/*.yaml` 五份镜像 tag、`deploy-opsmesh.sh` 默认 `IMAGE_TAG`。`release.yml` 的「标签==appVersion」硬门禁与 `validate-deploy-assets.sh` 第 1 节共同兜住版本一致性。
- **数据卷升级走 P0-5 迁移链路**（勿直接改 `.env` 的 `OPSMESH_VERSION` 后原地起服务）。
- **逐域开关判读**：被 `OPSMESH_SERVICE_PROXY` 停用的纯代理域，同前缀请求会以 **404** 计入该域流量桶（路由未注册）——读 `service-traffic` 时先看 `byStatus`。

### 验证

- `deploy/scripts/validate-deploy-assets.sh` → **PASS=36 / FAIL=0**；根模块 + 12 个存量服务模块 build / vet / 测试全绿。
- 两套 lint 配置均 `0 issues`（golangci-lint 2.13.2，与 CI 同版）：根 `.golangci.yml`、服务 `.golangci.services.yml`。
- 新增/收口测试：身份头治理 5 例（含非 default 租户传播直接证据）、流量聚合 10 例、RBAC 目录守护、portal/autoscaler 防回归 17 例；controlplane 全包 `-race` 由 CI `Race detector` job 复验。
- tag 触发的真发版验收（Release assets、12 镜像 + `.sig`、SBOM）以该 tag 的 run 结果为准，发布后回填本小节。

### 已知问题（本版未覆盖，已在册）

- 五域最终裁决仍**待真实流量观察期**（取数出口已具备，见新增能力与 §5.8 口径）；`auth-svc` 有据暂缓接通（部署侧 `AUTH_SVC_HTTP_ENABLED` 全量未设置，helm `auth_svc.enabled=false` + `storeType: memory`）。
- 微服务存储层测试覆盖极低（config-svc 0.7% / alert-svc 1.7% / gpu-svc 1.6% / auth-svc 2.4%）却在 prod 以 `*_STORE_TYPE=sql` 跑真 DSN——单体侧 P0-4 的修复不覆盖它们。
- TD-61 父包下沉（controlplane 顶层 134 文件 / store 96 文件）、TD-63 `buf generate` 根治、P1-7 许可与第三方合规（NOTICE / MPL-2.0 再分发）。

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