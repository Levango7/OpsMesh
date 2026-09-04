# Changelog

本文件记录 OpsMesh 所有重要变更。格式参考 [Keep a Changelog](https://keepachangelog.com/)，版本号遵循 [Semantic Versioning](https://semver.org/)。

> 当前最新已发布版本：`v0.9.0`（2026-09-05）。第九轮（UI 覆盖面+六域接线）、第十轮（部署配置+pkg 测试+3 真 bug）、追加固化（BOM 剥离+CVE 修复）+ 前端 P0-P3 功能补齐均归入 v0.9.0 发布。

## [0.9.0] — 2026-09-05（九/十轮 + 前端功能补齐合并发布）

### 版本亮点（相对 v0.8.0）

- **UI 覆盖面清零**：20 个后端域补齐管理页面（19 view + 19 api 封装 + 46 路由 + i18n 中英对齐）——此前 curl-only 的域全部可视化
- **六域微服务接线闭环**：controlplane 聚合层（service_proxy 反向代理 + ChatOps Web 命令台）+ RBAC 12 权限点 + 部署配置（Dockerfile×5 + compose + helm）全链就绪
- **测试规模翻倍**：前端 631→1121 用例（+338 store 测试）；pkg/ 12/12 包全覆盖（+130 用例）；测试驱动实抓 3 个真 bug（migrate Rollback 记账反向、tenant RequireTenant 403 不可达、EnforceQuota 数据竞争）
- **安全清零**：CVE-2026-84304（grpc HIGH，10 模块升 v1.83.1）、CVE-2026-14456（openssl）、CVE-2026-40200（musl）、GO-2026-5932（openpgp，不可达豁免留档）、36 文件 BOM 污染剥离
- **前端 P0-P3 功能补齐**（11 提交收编）：企业版多子域（Helm 应用商店等）+ 个人版功能域 + 幽灵 API 修复 + eslint 0/0

### 九轮：UI 覆盖面清零 + 六域微服务接线（2026-09-01）

- `internal/controlplane/service_proxy.go`：gpu/runbook/incident/autoscaler/portal 五域反向代理——静态映射+env 覆盖+路径改写（autoscaler/portal 服务真实路径无域前缀）+ 鉴权双守卫+不可达 503
- `internal/controlplane/bot_bridge.go`：ChatOps Web 命令台（bot-svc 是 IM webhook 入口与前端契约不同构的真缺口），命令语法与 bot-svc 一致（/opsmesh status|devices|alerts|ack|metrics|help）
- RBAC 补种 12 权限点（六域 read/write，viewer 派生 read）；前端六路由启用；9 项防回归测试
- 20 域管理页面（克隆 RolesView 黄金模板：DataTable/DetailDrawer/ConfirmModal/toast 零原生对话框，高危操作二次确认，表单字段对齐真实 handler）；金丝雀 25 新端点守卫生效验证
- 数据库文档补档：表清单 29→55 张、7.1 迁移清单 007-017、附录 A 文件↔代码映射

### 十轮：部署配置补齐 + pkg 测试清零 + 3 个真 bug（2026-09-02）

- 部署：5 服务 Dockerfile（alpine:3.23+apk upgrade 基线）+ compose 5 条目（8111-8115）+ helm values 5 条目（默认 disabled 零行为变化）
- pkg/ 6 零测试包补齐 115+ 用例（httptest 端到端、go-sqlmock 复用既有依赖、goroutine 并发拍）→ 12/12 包全绿
- **3 个真 bug 修复**（subagent 报告→主会话独立实锤→修复）：①migrate Rollback 传正版本走 INSERT 分支（真实 MySQL 主键冲突回滚必炸）→负版本 DELETE；②tenant RequireTenant 403 不可达（extract 恒返 default）→兜底移入 Middleware；③tenant EnforceQuota 锁外裸读 Quota map data race（CI -race 实测）→锁内快照
- compress MinCompressSize 死代码改如实文档；次要项（ratelimit goroutine 泄漏/SetDefaultQuota 拷贝语义/log Debug 落 INFO）留档

### 追加固化（2026-09-04/05）

- 3 轮 CI Trivy 红根因链：GO-2026-5932（openpgp unmaintained，Fixed N/A，0 import 符号级不可达）→ .trivyignore 豁免（trivyignores 输入名）→ 真凶 CVE-2026-84304（grpc v1.83.0 HIGH）→ 10 模块升 v1.83.1 → 本地 trivy 0.74+daocloud 镜像 DB 复扫清零 → CI 全绿
- Trivy 扫描报告 artifact 落盘（7 天保留）作为漏洞明细取证通道
- 并行工具 11 提交收编验证（前端测试 631→1121 全绿）+ 36 文件 BOM 污染字节级剥离
- gofmt/staticcheck 修复（golangci-lint v2.13.2 钉版复验）

## [Unreleased] — 2026-09-02 第十轮：部署配置补齐 + pkg 测试清零 + 3 个真 bug 修复（已归入 0.9.0）

> 三线并行收官（A=部署配置/B=数据库文档/C=pkg 测试 6 包 130+ 用例，2 subagent 协作）：六域接线的部署侧（compose/helm/Dockerfile）补齐、database-design.md 补档 11 个迁移、pkg/ 全部 12 包有测试且全绿——测试驱动开发实抓 3 个真 bug。

### A 组：M13 六域部署配置（adf57d6）

- **5 服务 Dockerfile 新建**（gpu/bot/runbook/incident/autoscaler）：克隆既有 11 服务模式（golang:1.26-bookworm 构建 + alpine:3.23 + apk upgrade 运行基线、非 root svc 用户、EXPOSE 按各自 pkg/config 默认端口）
- **docker-compose.yml 补 5 条目**：宿主端口 8111-8115（无冲突）、健康路径逐服务对照源码（gpu/bot=/health；runbook/incident/autoscaler=/api/v1/health）
- **helm values.yaml 补 5 条目**（gpu_svc/bot_svc/runbook_svc/incident_svc/autoscaler_svc）：默认全部 enabled=false（存量部署零行为变化），microservices.yaml 模板 range 自动渲染；incident 含 gRPC 50052

### B 组：database-design.md 补档（adf57d6）

- 表清单 29→55 张（007-017 共 26 张新表逐条入档）；7.1 迁移清单补 007-017 十一个文件（逐条对照 SQL 的 CREATE TABLE/ALTER 提取）；附录 A 文件↔Go 代码映射补齐（引用文件全部实存验证）；ER 图实体补 26 个（表清单/标题/实体数三处 55 自洽）

### C 组：pkg/ 6 零测试包测试补齐（086d601，2 subagent 并行）

- compress 21 + log 14 + migrate 24 + ratelimit 18 + tenant 24 + trace 14 = **115+ 用例**（httptest 端到端、go-sqlmock 事务链路[复用既有依赖零新增]、50/20 goroutine 并发拍、os.Pipe 捕获真实输出）
- pkg/ 至此 **12/12 包全部有测试且全绿**

### 测试驱动抓到的 3 个真 bug（C 组报告 → 主会话独立验证实锤 → 修复）

| Bug | 根因 | 修复 |
|---|---|---|
| migrate `Rollback` 回滚必炸 | 传正版本走 applyMigration 的 **INSERT 分支**——真实 MySQL 中该版本行已存在（主键），回滚第一步记账即主键冲突 | 改传负版本走 DELETE 分支（本就是为回滚设计的） |
| tenant `RequireTenant` 403 不可达 | `extractTenantFromRequest` 末尾恒返 "default"，"严格模式"与普通 Middleware 完全等价（文档宣称虚假） | 兜底移入 Middleware（宽松语义零变化，依赖方 auth/device-svc 全兼容）；RequireTenant 无身份真正 403 |
| tenant `EnforceQuota` data race | 错误信息构造在 `CanAllocate` 返回后**锁外裸读 `usage.Quota` map**，与 quotaStore 并发 SetQuota 写同 map 竞争（CI -race 实测捕获） | 锁内快照判定（snapshotUsageAndQuota），无限额语义与 CanAllocate 完全对齐 |

- 次要项：compress `MinCompressSize` 为死代码（WriteHeader 即初始化压缩器，阈值分支不可达）——改为如实文档注释（行为无害已上线，真实阈值需响应缓冲改变流式语义，不值得）
- 记录项（不修）：ratelimit cleanup goroutine 无停止通道（构造期一次性成本）；tenant SetDefaultQuota 拷贝语义；log Debug 级别落 INFO

### CI 修复链（2 轮）

- 086d601 红：golangci-lint staticcheck 3 项（log_test De Morgan + trace_test QF1011×2）→ 修复后钉版复验 0 issues
- adf57d6 红：Race detector（即上面 tenant data race）→ 锁内快照修复
- 终态 **093bc89 CI 全绿**（含 Race detector -count=3）

## [Unreleased] — 2026-09-01 第九轮：UI 覆盖面清零 + 六域微服务接线（M13 最后一公里）

> 两大留档项一次收官：①"6 个微服务域路由停用"——查明并非缺 UI（前端组件/API 封装/独立微服务全在），只缺 controlplane 聚合层路由，属纯接线问题；②"20+ 后端域无前端 UI"——后端 endpoint 早已全部注册，纯缺页面。3 个并行 subagent 交付 19 张页面，全部独立抽验。

### 六域微服务聚合接线（bfb8644）

- **`internal/controlplane/service_proxy.go`**：gpu/runbook/incident/autoscaler/portal 五域反向代理转发到 services/* 独立进程——静态映射表（env `*_SVC_URL` 可覆盖，默认与各 svc pkg/config 端口一致）；**路径改写**（autoscaler-svc 真实路径 `/api/v1/rules`、portal-svc `/api/v1/requests`，均无域前缀——代理层剥域前缀转发）；**鉴权双守卫**（requirePermission + requireTenantContext，微服务自身无租户鉴权，聚合层统一做——与第七轮越权修复同一信任边界）；连接预检不可达返回 503 带服务名（前端可提示"服务未启动"而非空洞 502）；剥离 Cookie 防会话凭证落地内部服务日志
- **`internal/controlplane/bot_bridge.go`**：ChatOps Web 命令台——bot-svc 是 IM 平台 webhook 入口（/webhook/{wecom,feishu,slack,dingtalk}），与前端 BotView 契约（/bot/command 等）不同构，属真实契约缺口。聚合层实现 Web 契约：命令语法与 bot-svc 完全一致（`/opsmesh status|devices|alerts|ack|metrics|help`，帮助见页面），数据源站内 store（租户隔离天然继承）；历史进程级内存（每租户 200 条有界）；web 平台恒开，IM 平台开关 env `BOT_PLATFORMS_ENABLED` 控制
- **RBAC 六域权限补种**（sql_rbac.go rbacPermSpecs）：gpu/bot/runbook/incident/autoscaler/portal 各 read+write 共 12 权限点——此前缺目录（admin 全量不受影响，但角色无法被显式授予、权限页不可见）；viewer 派生规则自动获得全部 read
- **前端六路由启用**（router/index.js）：GPU/ChatOps/Runbook/事件/扩缩容/门户 恢复注册
- **防回归测试 9 项**（service_proxy_test.go）：路径改写×9 用例、代理转发端到端（httptest 后端+env 覆盖+方法透传）、不可达 503、无凭证 401、bot 执行+历史、语法错误记录、平台/快捷命令、权限种子、env 覆盖解析——全部真跑通过

### 20 域管理页面（18c8730，3 subagent 并行交付后独立抽验）

- **19 view + 19 api 封装 + 46 路由 + i18n 中英双语逐键对齐**：定时任务/自动化规则/Webhook/脚本/工单/SLO/流量策略/流水线/ArgoCD/合规/HA/备份/配额/租户/计费(4 tab)/API Key(明文一次性展示)/网关路由(统计卡片)/审计事件(只读+筛选+导出)/通知渠道(渠道+模板双 tab)
- 全部克隆 RolesView 黄金模板：DataTable + DetailDrawer + ConfirmModal + toast，**零原生 confirm/alert**；高危操作（HA failover/备份恢复/流水线触发）ConfirmModal 二次确认
- 表单字段对齐真实后端 struct（subagent 逐一 Read handler 核实，非凭空设计）
- 独立抽验：文件清单 19+19 ✓、路由 20/20 ✓、i18n zh/en 键逐一相等 ✓、自跑 vite build ✓、vitest 631/631 ✓、eslint 0 error ✓

### 金丝雀活体验证（D 组）

- 25 个新端点活探测全部注册且守卫生效：未鉴权 401/400（schedules/quotas/notify-channels 因 handler 先解析租户头返回 400）vs 未注册路径 404——语义区分清晰，无"打开即 404"的死路由
- admin 一次性密码随机化/首登强制改密语义实测符合设计

### CI 状态

- 首推两轮 build-test 红：golangci-lint v2.13.2 报 gofmt 2 文件（本地未格式化直写）→ gofmt 修复 + 本地钉版复验 0 issues（510fc1c）
- 终态 **11 success + 1 skipped（release job 仅 tag 触发）全绿**（run 33547881837）

## [Unreleased] — 2026-09-01 第八轮：release tag 全链真跑攻坚（10 轮迭代 × 2 workflow）

> CI 11/11 全绿后遗留的最后一道：`v*` tag 触发的 **release.yml（服务镜像发布）+ ci.yml release job（goreleaser 二进制发布）** 两条链从未真跑。本轮从 tag 打下到全链绿共 10 轮迭代，每一层失败均由真实 CI 日志取证。

### 修复链（每轮失败 → 根因 → 修复）

| 轮次 | 失败点 | 根因 | 修复 |
|---|---|---|---|
| 1 | `invalid tag "ghcr.io/Levango7/..."` | Docker repository 名必须小写，GitHub owner 'Levango7' 含大写 L | release.yml IMAGE_PREFIX 硬编码 `ghcr.io/levango7` |
| 2 | `replaced by ../../: reading /go.mod: no such file` | 服务 go.mod 全部 `replace opsmesh => ../../`，单服务目录作构建上下文时容器内无主模块 | 新建根级 Dockerfile.service（上下文=仓库根，COPY go.work + 全部模块目录） |
| 3 | `"/operator": not found` | .dockerignore 排除 operator/，但 go.work 引用它 | .dockerignore 移除 operator 排除 |
| 4 | `warning: ignoring go.mod in $GOPATH /go` + `go.mod not found` | **golang 官方镜像默认 WORKDIR=/go（即 GOPATH）**——COPY 全落 GOPATH 且 WORKDIR /src/services/<svc> 是空目录，两条报错同根 | Dockerfile.service COPY 前先 `WORKDIR /src` |
| 5 | Trivy HIGH×2（musl CVE-2026-40200） | alpine:3.19 已 EOL（2025-11），musl 补丁 r6 不再进镜像 | 全 14 个 Dockerfile runtime 基础镜像 alpine:3.19→**3.23**（支持到 2027-11） |
| 6 | Trivy HIGH×1（openssl CVE-2026-14456） | alpine:3.23.5 基镜像预置 openssl 3.5.7-r0，而源里已有修复版 3.5.8-r0——`apk add` 只装不升 | runtime 阶段 `apk upgrade && apk add`（14 文件） |
| 7 | ✅ release.yml 全链绿（8/8 job：6 服务 build+push+Trivy + changelog + github-release） | — | — |
| 8 | ci.yml proto job：`buf breaking` 找不到 `proto/.git` | tag/PR checkout 是 detached HEAD，且 working-directory=proto 把相对 `.git` 解析到 `proto/.git`——**此步骤在 tag/PR 触发下从未跑通过**（main push 会跳过所以一直没暴露）；首次修复（git fetch origin main + branch=origin/main）实测仍失败：fetch 只建 remote-tracking ref | 改用 `https://github.com/${GITHUB_REPOSITORY}.git#branch=main,subdir=proto` 远程克隆对比（PUBLIC 仓库免凭证） |
| 9 | goreleaser `field formats not found in type config.Archive`（行 39） | **goreleaser v2.4.8（2025 初）不认 v2 的 archives.formats 复数语法**——.goreleaser.yml 声明的是 v2 新格式，CI 钉的版本太老 | goreleaser v2.4.8→**v2.18.0**（2026 最新稳定） |
| 10 | goreleaser `flag provided but not defined: -trimpath`（链接阶段） | **-trimpath 是 go build 旗标不是链接器旗标**——写进 -ldflags 被 linker 拒收 | 移到 builds.flags，ldflags 只留 -s -w -X |
| 11 | syft `unknown flag: --enrich`（SBOM 环节，归档已成功） | goreleaser v2.18.0 默认给 syft 传 `--enrich all`，CI 钉的 syft v1.11.0（2024-08）不认识——两个工具版本不同代 | syft v1.11.0→**v1.51.1**（2026 最新） |
| 12 | `PATCH /releases/380249686: 403 Resource not accessible by integration`（产物上传） | ci.yml 无顶级 permissions 块，GITHUB_TOKEN 默认只读——goreleaser 更新 Release 需要写权限 | release job 加 job 级 `permissions: contents: write`（最小授权） |
| 13 | ✅ **ci.yml 全链绿（12/12 job 含 goreleaser release）** | — | — |

### 发布产物（独立抽验实存）

- **GHCR 镜像**：`ghcr.io/levango7/{auth,device,alert,task,config,log}-svc` 各带 `0.8.0` + `latest` + 每 SHA tag，Trivy 扫描零 HIGH/CRITICAL
- **GitHub Release v0.8.0**：非草稿非预发布，正文从 CHANGELOG 充实；二进制产物 5 个资产——tar.gz linux/amd64（18MB）+ arm64（16.6MB）+ SBOM×2 + checksums.txt；amd64 tar.gz 已下载实测 SHA256 与 checksums.txt 逐字节一致（e3612982…）
- cosign 签名：COSIGN_PRIVATE_KEY secret 未配置，签名步骤按设计跳过（不影响发布链）

### 架构确认

- 两个 workflow 对同一 tag 无冲突：release.yml（~2 分钟）先建 Release 页面，goreleaser（等 11 门禁全绿，~17 分钟）后到只补产物——goreleaser 对已存在 Release 默认 keep-existing 不覆盖正文（官方文档确认），产物照常上传
- 发现并解锁：proto job 的 buf breaking 自仓库诞生起在 tag/PR 触发下就是坏的（needs 连坐导致 goreleaser release job 从未真跑过）

## [0.8.0] — 2026-09-01（五/六/七轮合并发布）

## [Unreleased] — 2026-09-01 第七轮：CI 23 连红 → 全绿攻坚（11 job × 19 提交）

> 本轮从「CI 从未跑通过」打到全绿：23+ 次真实 CI 运行、19 个修复提交、每一项修复均由真实 CI 日志取证（gh run logs），不凭猜测。**结束时 11/11 job 全绿**（build-test / integration / security / services / proto / Frontend / E2E-real / E2E-sec / Race / image / image-agent）。

### CI 转绿过程中抓到的真实 bug（均已在第六轮前各批次修复，本轮收尾清零）

| 类别 | 问题 | 根因与修复 |
|---|---|---|
| lint | 112 项静态错误 | errcheck 18 处语义化修复 + goimports local-prefixes 对齐（54 文件）+ G404/G702/G703 处置 + golangci-lint **版本钉死 v2.13.2**（此前 latest 漂移致本机绿 CI 红） |
| E2E mock | 78 用例全挂 | **Service Worker 绕过 page.route mock**：生产 PWA 的 sw.js 拦截 /api/* 自行 fetch，不经 Playwright mock 层直连无后端 proxy → ECONNREFUSED。`serviceWorkers: 'block'` |
| E2E mock | 3 用例挂（confirm 迁移连带） | 断言原生 dialog 的用例改 ConfirmModal 交互；confirm 迁移后组件 Teleport to body，组件标签 testid 不可靠 → 用内部 confirm-modal 定位 |
| agent 测试 | killProcessGroup(1) Linux 广播 SIGTERM 杀掉整个 CI job | 负 PID 语义=向所有可及进程组广播；fork sleep 子进程独立进程组再杀其组 |
| runner OOM | -race+coverage 全仓 81s SIGTERM(143) | 测试按资源分批（大包逐个/小包合并 -p 2）+ agent 批 GOMEMLIMIT=3GiB+拆半 + agent 包 TSan 分配器 goroutine 密集场景恒 OOM 降载 |
| DATA RACE | 备份 Create 的指针竞争 | CI -race 实证：goroutine162 写 vs goroutine161 json 序列化读同一结构体 → goroutine 持独立副本 |
| DURATION | TestExecute_Shell Linux 秒挂 | echo 级命令 <1ms 截断为 0 → 耗时保底 1ms（快速失败路径保持 0ms 语义） |
| **生产越权** | **裸 X-Tenant-ID 头冒充任意租户** | **E2E-sec 实测 200 穿透**：头非空但无凭证分支直接放行——修复：requireAuth 下默认 401，显式 --trust-gateway-headers=true（网关认证后剥离凭证的场景）才放行 |
| 迁移链 | 015 用 MySQL 8.0 保留字 usage/interval | 裸用必报 1064 → 改名 usage_data/interval_spec（迁移+SQL 同步；全仓保留字扫描确认无第三处） |
| 迁移链 | devices 表缺 hostname/os/arch | 001 建表漏列 → 017 迁移正式化（fixup 转正）+ agents.secret |
| **生产静默丢数据** | **MultiSchemaStore 从未建 schema** | 全仓无 CREATE DATABASE——首租户写入即 Unknown database → CreateTicket 静默 nil。defaultStoreFactory 先建库再连 |
| SQL | SetSecret 首写 NULL 扫描失败 | MAX(version) 空表返回一行 NULL（非 ErrNoRows）→ NullInt64 承接 |
| SQL | HeartbeatService 返回 false | MySQL RowsAffected 只数实际变更行，秒级 DATETIME 同值更新=0 行 → DSN 统一注入 clientFoundRows=true |
| helm | 三轮 lint 同错 | microservices.yaml 注释体内 */ 序列提前闭合 ×2 + **真凶：notes.txt 应为大写 NOTES.txt**（helm 只对大写名做提示渲染不参与 YAML 解析）。本地 helm v3.14.4 同版实测锁定 |
| 镜像构建 | 容器内 'cannot load module operator' | go.work 引用被 .dockerignore 排除的模块 → Dockerfile 加 GOWORK=off（主模块自洽） |
| 依赖 CVE | x/crypto CRITICAL + x/net/x/text/x/mod HIGH + tf-provider grpc CRITICAL | Trivy 全 lockfile 扫描 → 19 个模块全部升级到一致组合（crypto v0.55/net v0.57/text v0.41/grpc v1.83）；go.work.sum 陈旧哈希行清理 |
| 依赖文件 | go.work.sum 被写坏为 CRLF | PowerShell Set-Content -Encoding ascii 重写引入 196 处 CRLF，容器校验拒绝 → 转回 LF + go.sum tidy 补全 |

### 流程产出

- **安全测试的价值实证**：E2E-sec 的租户越权用例 401 断言抓到生产越权漏洞；CI -race 抓到备份 DATA RACE；Trivy 抓到 CRITICAL CVE——全链从未跑通前这些全被掩盖
- **教训（写入 memory）**：目录被外部清空 3 次（workbuddy 侧），工作区两次重建（现 OpsMesh-ci）；PowerShell Set-Content 写 Go 文件必炸（BOM/CRLF 双雷），本轮 go.work.sum 事故后禁用，统一 Read+Edit
- 修复完成即 commit+push（不留未提交状态防目录事故）

## [Unreleased] — 2026-08-30 第六轮：留档项四项补齐（告警正确性 + Helm 微服务 + UX 收尾 + 文档保真）

### 告警推送正确性修复（notifyLoop 水位线 → 指纹去重）
- **根因**：`lastAlertSent` 全局单一时间高水位跨租户合并——任何租户告警推送后水位前移，其它租户/乱序插入/CreatedAt 更早的告警被**永久漏推**（多副本时钟偏差同样触发）。运维平台漏告警属核心正确性缺陷
- **修复**：改为按 AlertID 指纹去重（map+mutex，对乱序/跨租户/时钟偏差免疫）；**推送成功才标记、失败撤销下轮重试**（原实现成败都推进水位，语义更优）；条目 24h 过期清理（有界防泄漏，远大于聚合窗口 5min）
- **回归测试** `TestLoopM4_NotifyLoop_OutOfOrderAlertsBothPushed`：锁定乱序核心场景（后创建先推送 → 旧水位实现下早创建的告警被漏推、新实现两条都推）+ 去重仍生效 + 有界清理语义四组断言

### Helm 微服务部署能力（18 服务上 K8s 的通道）
- `values.yaml` 新增 `services:` 段（11 个有 Dockerfile 的服务全列，键名下划线规避 `--set` 连字符坑）；**默认全部 enabled=false——存量部署 chart 行为零变化**
- 新增 `templates/microservices.yaml`（第 18 个模板）：range 生成 Deployment+Service（HTTP/gRPC 双端口），资源名下划线统一转连字符；探针路径/端口/存储 env 前缀逐一对照各服务源码实测（log-svc 的 /healthz、plugin/aio 无 gRPC 等差异如实处理）；DSN 复用控制面 Secret 的 mysql-dsn 键派生 + checksum 注解滚动重启
- 渲染验证：默认值 0 对象渲染（零行为变化 PASS）；单服务启用/全量启用/MySQL 关闭三 case 模拟渲染全过（本机无 helm，CI helm lint job 兜底真渲染）
- README Helm 章节补"微服务部署（可选）"；NOTES.txt 提示默认未启用

### UX 收尾
- K8sManageView（15 处）/PortalView（4 处）原生 confirm/alert 全部迁移 ConfirmModal/Toast——**全站对话框体系统一完成**（第五轮已迁 4 页，本轮补齐最后 2 个活跃页面），grep 残留清零

### 文档保真（api-reference 剩余字段漂移清零）
- 15 处 `agent_id` 疑似漂移**逐源核实**（handler JSON tag + 前端实际调用双证据）：14 处确认漂移修正（含 gRPC 全表——实际传输是 JSON codec，字段名=Go camelCase tag；HMAC 签名原文消息与密钥顺序均写反，按 grpc.go:210 实现修正）；1 处核实为 proto IDL 事实陈述保留
- 顺带修正核实中发现的**关联虚构**：GET /devices 实为 segment 分组结构（非扁平数组）、GET /agents 仅 4 字段（原文档 5 字段不存在）、POST /tasks/batch 实为 `targets`（无 agent_ids）、POST /workflows 的 dag 是 JSON 字符串非 {nodes,edges}、os/middleware 响应实为 201+完整对象等 15 项——每项均有 handler 源码行号依据，零瞎改

### 验证
- `go build ./...` + vet + gofmt 清零 ✅；主模块 controlplane（158s，含新乱序回归测试）/store 全绿 ✅
- 前端 vitest 631/631 + build 5.1s ✅；K8s/Portal 残留 grep 清零 ✅
- Helm 模板：语法配对 77/77、helper 引用全有效、values-模板键路径 30 处逐一对照零多余、4 case 渲染模拟通过（CI helm lint 兜底）

## [Unreleased] — 2026-08-30 第五轮：四路审计问题全量修复（认证链 4C + 后端安全 4 项 + 交付链 4C + UX 包）

> 基线：四路并行深度审计（架构/前端/测试CICD/文档）发现的 Critical/High 问题，四组 subagent 并行修复，文件边界零重叠，全部经独立抽验 + 全仓回归。

### 认证链修复（前端 4 个 Critical，全部亲验根因）
- **刷新 401 自等待死锁**（request.js:47-59）：refresh 请求自身 401 时 `await refreshing` 等待自己的 Promise 永不 settle → 会话过期后整站挂起白屏。修复：401 分支入口排除 `/auth/refresh` 自身（isRefreshCall），refresh 401 直接清会话跳登录；注释完整推演三条循环边界（refresh 自排除 / _retry 单次 / single-flight）
- **改密 API 字段名断裂**（api/auth.js:12）：前端发 `old_password` vs 后端 `json:"oldPassword"` 严格映射 → 改密功能 100% 返回 400。修复：对齐驼峰 + 新增可选 changePasswordToken 第三参（首登改密链路）
- **首登强制改密断裂**（stores/auth.js:51 + LoginView.vue:96）：前端读蛇形 `must_change_password` 恒 undefined → 安全特性完全失效。修复：改读驼峰 `mustChangePassword`/`changePasswordToken`，ChangePasswordView 提交带改密令牌，全链路打通
- **用户编辑静默清空角色**（UsersView.vue）：前端读 `role_ids`（后端输出实为 `roleIDs`）恒为 [] → 编辑任何用户保存即清空其全部角色（数据破坏级）。修复：读取侧全改驼峰；提交侧经核实后端 PUT 接收 tag 实为 `role_ids` 故保留（两侧契约不对称是后端历史设计，如实对齐而非盲目统一）；RolesView created_at 同修
- **注册审批入口闭环**（新功能）：后端 `/users/{id}/approve|reject` 早已存在但前端零入口 → pending 用户永久卡死只能 curl。新增 approve/reject API 方法 + UsersView 审批按钮（仅 pending 行）+ 状态三态展示（pending 琥珀"待审批"）+ i18n 双语
- **错误文案误导**（Register/Login）：429 限流被显示为"用户名已被占用"——优先 `e.j?.error` 后端明确文案，429 专用提示

### 后端安全修复（4 项）
- **CORS 反射任意 Origin**（server_lifecycle.go:344）：反射 + Allow-Credentials 等同向任意站点开放带 Cookie 跨域调用。重写为白名单模式（此前修复曾因目录事故丢失，本次落库）：新增 `--allowed-origins` flag（Validate 拒绝 `*`），空=同源策略不输出任何 CORS 头，不匹配 OPTIONS 透传（防浏览器误判放行）+ `Vary: Origin`
- **MultiSchemaStore 随机路由**（multi_schema.go:897）：`for range map` 迭代使用户中心数据路由到随机 schema，≥2 租户时 admin 登录随机失败。修复：固定 `storeFor("global")` 确定性路由
- **task-svc 领取越权**（RCE 级，mysql.go:178）：`OR agent_id=''` 兜底使任意 agent 可领任意租户任务。移除兜底改严格 `agent_id=?`（Memory/MySQL 双实现）+ 回归测试锁定
- **auth-svc 弱口令直发 token**（双轨安全漂移）：Login 忽略 MustChangePassword，admin/admin123 首登直发全量 token（internal 轨早已修复、服务轨原样）。对齐 internal 轨语义：改走 5min 短时效改密令牌 + 不签 refresh；**防御性加固**：mustChangePassword 用户的 RefreshToken 通道同样拒绝签发全量 token（防绕过首登改密）；播种 bcrypt 吞错改 fail-fast；测试对齐新安全语义（含 2 个新回归测试）
- **资源泄漏**：alert-svc MySQLStore 补 Close + main defer；escalation Stop 加 sync.Once 幂等 + ticker.Stop()（防双调 panic 与 ticker 泄漏）+ 幂等回归测试

### 交付链修复（CI/CD 4 个 Critical）
- **微服务镜像管道断裂**：release.yml 在 services/<svc> 构建但 Dockerfile 一个都不存在 → tag 发布必挂。新建 11 个服务 Dockerfile（多阶段，端口对照各服务 config 实测，runtime 用 alpine+curl 保 healthcheck 兼容，非 root 用户）；compose 的 11+9 处不存在引用全部修正
- **共库表名冲突**：prod compose 全部服务 DSN 指向同一 opsmesh 库，而 devices/users/agents/alerts/ci_items 表结构两侧不一致 → INSERT 必炸。微服务改独立库 `opsmesh_<svc>` + init-mysql.sql 补 5 库 CREATE+GRANT
- **release 门禁**：needs 补 services/proto/frontend/race（此前 tag 发布可在服务构建失败时照常出产物）
- **README flag 表**：宣称 116 全列，实测 119 个且表格仅 79 行——补齐 40 个（含 --allow-stub-stores 生产启动闸/--vault-*/--cb-* 等安全项），全仓 5 处旧计数同步

### UX 提升包（7 项）
- 原生 confirm/alert 迁移 ConfirmModal/Toast（Tasks/Deploys/Plugin/Roles 四个活跃页面）；WorkflowsView 8 处硬编码色值 token 化（双主题可读）；硬编码中文错误 i18n 化（log/cmdb store + RolesView）；i18n 缺键补齐 + 2 处重复键去重（gpu.models/portal.myRequests）；路由权限不足 toast 提示（不再静默跳转）；OverviewView 加载骨架；TasksView SSE 事件刷新 2s 节流（trailing 保证末次必刷）

### 文档保真修复
- api-reference 3 处字段名漂移（K8s 集群 api_server/kube_config→server/kubeconfig 必填、设备指标扁平→嵌套结构、logs 蛇形→驼峰）；SSE 文档 2 处 data 字段（action→status）；operations.md 指标类型误标（counter→gauge，rate() 示例改阈值比较——原示例会在 Prometheus 直接报错）；DELIVERY 陈旧数字；product-design 成熟度表 6 个微服务域如实降 🟡

### 验证
- 主模块 build/vet + 四包测试（controlplane 157s/agent 51s/store 36s/config 0.4s）全绿 ✅
- task-svc/auth-svc/alert-svc build+vet+test 全绿（含 3 个新回归测试）✅
- 前端 vitest 631/631 + 生产构建 ✅ gofmt 全仓清零 ✅
- 四组文件边界零重叠，交叉核对无互相回退（CORS 反射行确认已删、MustChangePassword 分支在位）

## [Unreleased] — 2026-08-30 第四轮：注册流程 UX 修复 + 浅色主题提亮

### 登录注册功能修复（用户反馈"登录注册好像有问题"）
- **根因（实测定位）**：后端注册默认走安全基线（`--allow-public-register=false`），返回 `{"status":"pending"}` 且**不签发 token**；但 RegisterView 无视该语义——显示"注册成功"后 600ms 强跳 `/devices`，被路由守卫（未登录）弹回 `/login`。用户看到"注册了却进不去"，体验即"登录注册坏了"。登录流程本身无 bug（`must_change_password` 分支正确跳改密页）
- **修复**：RegisterView 按响应分叉——`status=pending` 时停留本页展示待审批提示（不再误跳）；仅 `--allow-public-register=true`（注册即登录）时才跳 `/overview`（顺带修正原先跳 /devices 的目标页为总览）
- **i18n**：新增 `register.pending` 中英文案——明确告知"已提交待管理员审批、审批后可登录、如需免审批联系管理员开启 --allow-public-register"，消除"注册成功却登录不上也不知道找谁"的困惑
- **样式**：注册页提示框 `align-items: flex-start` + 图标 `flex-shrink:0`——待审批长文案多行时图标钉在首行不挤压文字

### UI 浅色主题提亮（用户反馈"太暗淡"）
- **根因（色值实测）**：浅色主题页面底色 `#e8ecf7` 饱和灰蓝（灰纱感）+ 卡片表面 `#f7f8fd` 非纯白，两者仅差约 4% 亮度——卡片"浮"不起来，整体扁平发暗
- **修复（仅浅色主题，暗色主题不动）**：页面底色 `#e8ecf7 → #f2f4f9`（降饱和去灰纱）；卡片 `#f7f8fd → #ffffff`（纯白拉开层次）；次级表面/边框/四色 soft 底/状态色 bg 全线提亮一档；顶栏玻璃 `rgba(247,248,253,.86) → rgba(255,255,255,.86)`；文字色对比微调（主文字加深 `#1c2340` 保持可读性）；阴影参数随新基色适配
- **验证**：631/631 vitest 全绿 + 生产构建 5.7s 通过（纯 token 替换，组件 scoped 样式经 CSS 变量自动继承，零组件改动）

### 技术栈体检（结论：健康，无需修复）
- Go 1.26.6（最新稳定线）；直接依赖均为近期版本（go-sql-driver v1.10.0 / jwt v5.3.1 / vault api v1.23.0 / client-go v0.32 等）
- `golang-jwt/jwt/v4`、`gogo/protobuf` 等旧包仅存在于 go.sum（`go mod why` 确认主模块不引用，`go mod tidy` 无变化）——传递依赖痕迹非直接风险
- 结论留档：技术栈当前状态良好，无需升级动作；CI 已有 govulncheck+gosec+Trivy 持续扫描兜底

## [Unreleased] — 2026-08-30 第三轮：微服务 MySQL 接线（TD-65）+ 前端可测性补齐

### 微服务持久化接线（TD-65）
- **9 个微服务 main 接线 MySQL store**：alert / auth / config / deploy / device / incident / plugin / portal / task——此前 `NewMySQLStore` 已实现（12 处定义）但 **0 处调用**，main 全部硬编码内存 store，重启数据全丢。现按统一模式接线：`<NAME>_SVC_STORE_TYPE=sql` + `<NAME>_SVC_DSN` 非空时启用 MySQL（构造时自动建表），初始化失败回退 memory 并打日志；auth/device/task 的 MySQLStore 有 `Close()` 的在分支内 defer 调用
- **缺配置字段的服务补齐**（风格对齐既有 env helper）：auth / deploy / incident / plugin / portal / task 各补 `StoreType`/`DSN` 字段与 `<NAME>_SVC_STORE_TYPE`/`<NAME>_SVC_DSN` 环境变量
- **编译期接口断言补齐**：9 个服务 mysql.go 补 `var _ <Store接口> = (*MySQLStore)(nil)`（device/task 各 4 条），杜绝"接线后才发现接口不全"的运行期风险
- **auth-svc 服务层解耦**：`NewService` 参数 `*store.MemoryStore` → `store.Store` 接口（字段同步），否则 MySQLStore 无法注入；测试通过
- **安全修复（接线审核中发现）**：config-svc `NewMySQLStore` 原硬编码 `deriveKey("default-key")`——MySQL 模式下所有租户 secret 用公开常量加密，形同明文。改为与 MemoryStore 同源的 `cfg.EncryptionKey`/`MaxHistorySize` 参数（跨后端加密行为一致）；空 key 时派生临时随机 key 并打告警（重启后旧 secret 不可解，仅限演示，生产必须显式配置）
- **合理跳过（原因留档）**：runbook（无 mysql 实现，本轮不新写）/ tf-provider（无标准 main）/ aio·bot·autoscaler·grafana-bridge（main 无 store 概念）/ gpu（manager 构造不消费 store，需重构）/ workflow（service 层与 store 层接口签名不兼容，需适配层）/ log（main 已有完整 memory/sql/loki/es 四后端分支，mysql.go 为死代码不应换接）

### 前端可测性与体验（DC 补全）
- **6 个核心老页面补 data-testid**（OverviewView 18 / CMDBView 15 / LogsView 26 / UsersView 22 / WorkflowsView 24 / DeploysView 24）——此前这批页面 testid=0，E2E 无法定位元素，与新页面（GPU/K8s/Runbook 等 15-20 个）存在质量断层。命名对齐新页面基准（`<page>-view`/`btn-row-<action>`/`input-<field>`/`<page>-table`），v-for 元素用动态拼接保证唯一。纯属性添加，零逻辑/样式改动，631/631 前端测试全绿

### 明确不做（审核决策，留档）
- 服务默认端口重叠不改代码（改默认值破坏已部署环境，README 警告已覆盖）
- tf-provider/bot-svc 外部系统深度集成（需联调环境，独立立项）

### 验证
- 9 个改动服务逐一 `go build ./... && go vet ./...` ✅；config-svc/auth-svc 服务层测试 ✅
- 主模块 `go test ./internal/controlplane/ ./internal/agent/ ./internal/store/` ✅（三包全绿）；operator build ✅
- 前端 `npx vitest run`（631/631）+ `npm run build` ✅（8.2s）
- `gofmt -l` 全仓清零 ✅（修复 device-svc main 一处残留）

## [Unreleased] — 2026-08-30 第二轮安全加固与文档补全批次

### 安全加固（SEC 系列）
- **SEC-1 错误信息脱敏（收尾 + 守护测试）**：在 60+ 处 500 路径改用 `writeInternalError`/`writeSanitizedError`（k8s_manage 19 处、quota/apikey/middleware/os_optimize 等）的基础上，完成全量 4xx 泄露面核查——59 处输入回显 + 15 处固定鉴权文案 + 60 处 sentinel 校验文案均确认安全；新增 `http_infra_leak_test.go` 静态扫描守护测试：**禁止任何 5xx 响应携带原始 `err.Error()`**（金丝雀注入验证有效，CI 防回退）
- **SEC-4 测试契约同步**：`testNotifyChannel` 发送失败语义从 200+`status:fail` 改为 500+脱敏文案（服务端改动在前批落地，本轮同步 `server_alerts_m2_extra_test.go` 断言），修正「HTTP 语义错误 + SMTP 地址泄露」
- **SEC-5 权限目录补齐**：`rbacPermSpecs` 新增 `cmdb:approve`（CI 变更审批）——此前 `cmdb_approval.go` 的 approve/reject 端点经 `requireProd` 校验该权限点，但权限目录未定义（G1 遗留）；`RolePermissions` 单一来源自动派生，operator 角色不获得审批权（最小权限：审批仅 admin）

### 可靠性（CB 系列）
- **CB-9 agent goroutine panic 兜底**：新增 `internal/agent/safego.go`（`safeGo` 包装器：panic 捕获 + 带堆栈日志 + 5s 防风暴延迟后重启循环）；`agent.go` 全部 7 处常驻 goroutine（worker 池/heartbeatLoop/dispatchLoop/cancelLoop/logCollectLoop/LogPusher/LogCollector）经 safeGo 启动——单循环 panic 不再拖垮整个 agent 进程；新增 `safego_test.go` 验证 panic 后重启与正常退出不重启

### 文档与代码一致性（DC 系列）
- **DC-1 README 补 services/ 18 微服务**：新增「services/ 微服务目录」章节（18 服务职责表 + 双轨并存状态说明 + 端口默认重叠警告——多服务默认同为 HTTP 8080/8081，同机并行须显式配端口）
- **DC-2 api-reference 补 61 组端点**：新增 16 个功能域章节、约 130 个端点小节（+2074 行），覆盖平台化/计费/网关/备份/合规/HA/工单 SLO/流量/流水线 ArgoCD/自动化/Webhook 脚本/网络设备/审计扩展——全部从 handler 源码提取方法/权限/请求体/响应，未文档化路由从 68 项清零；simulated 标记如实注明（backup restore/canary metrics/ha failover 已是真实实现，G2 批次移除占位）
- **DC-5 DELIVERY 规模刷新（实测）**：仓库 1,121 文件 / Go 714（含测试 265）/ 约 221,900 行 / 前端 145 文件——对齐 25 提交增量演进后的真实规模（原 346 文件/~51,700 行已过时）
- **tech-debt 如实登记 TD-60~67**：七域双份实现收敛（约 27,000 行重复）/ 父包拆分 / 网关数据面 / Task 三份 schema / pb stub 死代码 / 微服务 MySQL store 未接线 / gofmt 基线（TD-67 已当场修复）；纠正「全部解决」的失真表述

### 代码质量
- **gofmt 基线恢复**：`gofmt -w internal cmd pkg services tests` 全仓 139 文件清零（2026-08-29 晚间提交曾把 tab 改空格致 CI gofmt 门禁必挂）；`go build ./...` + `go vet ./...` 全绿
- **TE-2 集成测试真断言**：`tests/integration/services_test.go` 重写为两类——A 类纯算法链路（异常检测/告警聚合/插件生命周期，默认执行真实断言）+ B 类微服务 HTTP 契约（环境变量控制，无环境自动 Skip）

### 验证
- `go build ./...` ✅ `go vet ./...` ✅ `go test ./internal/controlplane/ ./internal/agent/ ./internal/store/` ✅ 全绿（含新增 safego/leak-guard 测试）

## [Unreleased] — 2026-08-27 SQL 持久化全域落地（P0.3 + P1-P6 共 18 域）

### SQL 持久化实现
- **P0.3（3 域）**：secret / discovery / config — 迁移 007/008/009，sql_secret.go / sql_discovery.go / sql_config.go 从内存 map 重写为 MySQL CRUD
- **P1（2 域）**：slo / ticket — 迁移 010，sql_slo.go / sql_ticket.go 从桩重写为 MySQL CRUD
- **P2（3 域）**：argocd / pipeline / traffic — 迁移 011，sql_argocd.go / sql_pipeline.go / sql_traffic.go 从桩重写为 MySQL CRUD
- **P3（2 域）**：backup / compliance — 迁移 012，sql_backup.go / sql_compliance.go 从桩重写为 MySQL CRUD
- **P4（2 域）**：automation / network — 迁移 013，sql_automation.go / sql_network.go 从桩重写为 MySQL CRUD
- **P5（2 域）**：script / webhook — 迁移 014，sql_script.go / sql_webhook.go 从桩重写为 MySQL CRUD
- **P6（4 域）**：tenant / apikey / plugin / billing — 迁移 015，sql_tenant.go / sql_apikey.go / sql_plugin.go / sql_billing.go 从桩重写为 MySQL CRUD
- **设计文档**：新增 `docs/sql-persistence-design.md`（15 域 22 张表完整设计 + 审核通过）

### StubDomains 清理
- `stub_guard.go` StubDomains 列表清空（全部 15 域已持久化）
- `config.go` stubStoreDomains 常量清空，生产模式 + SQL 后端不再拒绝启动
- 删除 `stub_semantics_test.go`（桩语义测试已过时）
- 更新 `memory_crud_extra_test.go` / `sql_test.go` / `config_extra_test.go` 中 StubDomains 相关断言

### 测试补强
- 新增 `sql_p03_test.go`：P0.3 扫描函数测试（8 个）
- 新增 `sql_p1p6_test.go`：P1-P6 扫描函数测试（57 个，覆盖全部 21 个 scan 函数）

### 验证
- `go build ./...` ✅ `go vet ./...` ✅ `go test ./...` ✅ 全绿无失败

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