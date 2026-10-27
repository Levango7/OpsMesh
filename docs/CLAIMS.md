# CLAIMS.md — 领地与任务单一追加日志（v2 起）

> 协议见 ./C[DONE]ORDINATION-BOARD.md。追加式、勿改历史行、一律 LF 行尾、每行一条 [KIND]。
> 2026-10-15 前的旧记录留在 docs/I[DONE]BOX-ZCODE.md 与 docs/INBOX-LEVANGO.md（只读档案，门禁照常解析）。

[ROLE] 2026-10-26T03:00:00Z lead=kilo note=当前用户口径：仅 kilo 线在岗，workbuddy/qoder 备用
[RELEASE] 2026-10-26T03:00:00Z from=ZCode scope=docs/COORDINATION-BOARD.md reason=旧claim过期(main,ttl=72h,2026-10-15起)随v2上线关闭
[DISPATCH] 2026-10-26T03:00:00Z from=kilo to=ZCode task=T-001 scope=docs/ ddl=2026-10-27T00:00:00Z note=协作板v2落地+v0.16.0归版+路线图状态修正
[STATUS] 2026-10-26T04:30:00Z from=ZCode task=T-001 note=板v2(d8fad52)+路线图(8a6167b)+归版v0.16.0(3d54aa2)已推main;等CI绿后打tag
[NOTE] 2026-10-26T04:30:00Z from=ZCode trae/agent-5vTIVD 已删（唯一领先提交 35cdcb96 仅 4 个 zip 垃圾文件,sha 留档可恢复）
[DONE] 2026-10-26T04:30:00Z from=ZCode task=T-001 scope=docs/ commit=3d54aa2 note=tag 视 CI 结果另行完成
[STATUS] 2026-10-26T05:20:00Z from=ZCode task=T-001 note=CI 连修三轮（工具链1.26.10/依赖3CVE/许可清单）后 main 12腿全绿
[DONE] 2026-10-26T07:55:00Z from=ZCode task=T-001 scope=docs/ commit=267a104 note=v0.16.0 tag 已打并推送，release 链跑中（回核自验）
[ROLE] 2026-10-26T09:00:00Z lead=kilo note=用户口径更新：kilo 主力 + qoder 机动加入，workbuddy 备用
[NOTE] 2026-10-26T09:00:00Z from=ZCode 分工面（观察实测）：kilo 在途=internal/controlplane + web/enterprise + internal/store（未提交 300+ 文件，本线与 qoder 不进）；qoder 已被引导做隔离面（docs/tests/scripts/deploy 文档，其工作区为过期树、见 INBOX-QODER 处理步骤）；ZCode=协调/归版/发布/演练面。三线共同纪律见根目录 AGENTS.md。
[NOTE] 2026-10-26T09:00:00Z from=ZCode 根目录新增 AGENTS.md——任何 agent 线开工自动读协议（pull→读板→CLAIM→DONE+push），不依赖口头约定。
[CLAIM] 2026-10-26T14:05:02Z from=qoder ttl=8h scope=docs/third-party-licenses-npm.md
[CLAIM] 2026-10-26T14:05:02Z from=qoder ttl=8h scope=docs/license-compliance-deliverable.md
[CLAIM] 2026-10-26T14:05:02Z from=qoder ttl=8h scope=docs/third-party-licenses.md
[CLAIM] 2026-10-26T14:05:02Z from=qoder ttl=8h scope=deploy/scripts/gen-third-party-licenses.sh
[CLAIM] 2026-10-26T14:05:02Z from=qoder ttl=8h scope=deploy/scripts/collect-os-package-licenses.sh
[NOTE] 2026-10-26T14:05:02Z from=qoder 按AGENTS.md§4领隔离面=④第三方许可交付(npm清单+OS包源码获取途径+交付文档)；不触internal/ web/enterprise/ .github/；过期树已按INBOX-QODER处置(备份分支backup/qoder-stale-2026-10-26,HEAD 1763d5e,66项残骸清单已留档)。
[DONE] 2026-10-26T14:42:39Z from=qoder scope=docs/third-party-licenses-npm.md commit=edee8bb
[DONE] 2026-10-26T14:42:39Z from=qoder scope=docs/license-compliance-deliverable.md commit=edee8bb
[DONE] 2026-10-26T14:42:39Z from=qoder scope=docs/third-party-licenses.md commit=edee8bb
[DONE] 2026-10-26T14:42:39Z from=qoder scope=deploy/scripts/gen-third-party-licenses.sh commit=edee8bb
[DONE] 2026-10-26T14:42:39Z from=qoder scope=deploy/scripts/collect-os-package-licenses.sh commit=edee8bb
[NOTE] 2026-10-26T14:42:39Z from=qoder ④第三方许可交付已落地（commit=edee8bb）：npm 侧 342 包/copyleft=0、alpine 16包9含GPL、debian 88包64含GPL、基础镜像表改为从 Dockerfile 推导；--check 变异 5 例全红、基线双 PASS；shellcheck -S style 0、部署资产门禁 PASS=79、coord-gate rc=0。遗留（需 lead/用户开口）：#74 把发布物回核改成拦得住的门禁要动 release.yml（禁区）；#76 新版存量库演练属协调/发布线。
[CLAIM] Mon, 26 Oct 2026 16:57:46 from=qoder ttl=2h scope=docs/
[CLAIM] Mon, 26 Oct 2026 16:57:46 from=qoder ttl=2h scope=internal/gates/doc_paths_gate_test.go
[DONE] Mon, 26 Oct 2026 16:57:46 from=qoder scope=docs/ commit=969f00b
[DONE] Mon, 26 Oct 2026 16:57:46 from=qoder scope=internal/gates/doc_paths_gate_test.go commit=969f00b
[NOTE] Mon, 26 Oct 2026 16:57:46 from=qoder 文档路径门禁已落地（commit=969f00b）：15 份现状说明/294 条路径基线绿；修正 61 处重构后漂移（internal/store→store/sqlstore、provision→pkg、controlplane grpc|paginate、plugin_remote→pluginhost、templates→helm、deploy.sh 路径）；multi-os-support §4 的 PlatformCapability 实为零实现，已标注"设计稿"；变异 5 条全红含扫描面自证。留待：docs/test-specification.md 12 条测试路径需逐条核实改名（未硬改）；docs/tech-debt.md 7 条旧路径在禁区未动；COORDINATION.md 属历史日志不改写。
[CLAIM] Mon, 26 Oct 2026 17:50:28 from=qoder ttl=2h scope=docs/test-specification.md
[DONE] Mon, 26 Oct 2026 17:50:28 from=qoder scope=docs/test-specification.md commit=2f89556
[DONE] Mon, 26 Oct 2026 17:50:28 from=qoder scope=internal/gates/doc_paths_gate_test.go commit=2f89556
[NOTE] Mon, 26 Oct 2026 17:50:28 from=qoder test-specification 复核完毕（commit=2f89556）：4 处按实证修正、9 处登记为存量债务（SSH/SFTP 已无实现、FakeClock 零命中、bufconn 不在 grpcx、authz/csrf/sqlmock 用例对不上），文档顶部已标"✅是历史断言"；门禁扩到 16 份文档/312 条路径，13 条豁免逐条写原因，摘掉任一条即判红。
[CLAIM] Mon, 26 Oct 2026 19:25:35 from=qoder ttl=4h scope=.github/workflows/
[CLAIM] Mon, 26 Oct 2026 19:25:35 from=qoder ttl=4h scope=deploy/scripts/verify-release-artifacts.sh
[CLAIM] Mon, 26 Oct 2026 19:25:35 from=qoder ttl=4h scope=deploy/scripts/verify-artifacts-ci.sh
[CLAIM] Mon, 26 Oct 2026 19:25:35 from=qoder ttl=4h scope=deploy/scripts/validate-deploy-assets.sh
[NOTE] Mon, 26 Oct 2026 19:25:35 from=qoder 用户 2026-10-26 直接授权做 TD-74（发布物回核前置）：按 AGENTS.md 禁区条款该目录需用户开口，本次即该授权；只改 release.yml 的发布链顺序与回核调用，不动 build/promote/签名逻辑。
[DONE] Mon, 26 Oct 2026 19:26:01 from=qoder scope=.github/workflows/ commit=0dac345
[DONE] Mon, 26 Oct 2026 19:26:01 from=qoder scope=deploy/scripts/verify-release-artifacts.sh commit=0dac345
[DONE] Mon, 26 Oct 2026 19:26:01 from=qoder scope=deploy/scripts/verify-artifacts-ci.sh commit=0dac345
[DONE] Mon, 26 Oct 2026 19:26:01 from=qoder scope=deploy/scripts/validate-deploy-assets.sh commit=0dac345
[NOTE] Mon, 26 Oct 2026 19:26:01 from=qoder TD-74 已闭合（commit=0dac345）：新增 verify-preflight 把 ①②④⑤ 拦在 Release 之前，③ 留发布后；§17 加三条顺序断言（变异 3 条全红）；删掉的自检由 §25 的 7 例兜着。**只在打 v* tag 时真跑**，前闸效果待下次切版实例化验证。
[NOTE] 2026-10-26T20:10:00Z from=ZCode main 红（3 提交连红：build-test errcheck 80 处 internal/ai/ + Frontend E2E strict-mode）——报点全在 kilo 领地，精确清单已投 INBOX-KILO；本线不代修，等 lead 处置。qoder 5 scope 已 DONE、TD-74 前闸已落（用户授权）。
[NOTE] 2026-10-26T21:30:00Z from=ZCode 用户四项裁定（本条为准，全板生效）：
① §3.6 口径=**维持 503 fail-closed 定版**（cutover_outage_test.go 四判据即终态，不再推翻）；
② T[DONE]-89 残余=**挂账观察**，触发条件=一个月内 service-container 判红 ≥3 次 ⇒ 上选项②（GHCR 源）；触发前不动 .github/workflows/；
③ main 红（kilo 领地）=**24h 窗口**：至 2026-10-27T21:30Z 未清红，任何线可按板协议 [TAKEOVER] 接管 internal/ai/ 的 errcheck 修复（只修 lint 报点、不碰功能语义；Frontend E2E 仍归 kilo）；
④ **main 转绿后即执行演练批次 0 重演，执行线=ZCode**（一石三鸟：§3.12 operator 取消权 + v0.16.0 运维能力 + T[DONE]-74 前闸实例化受检）。
[TAKEOVER] 2026-10-26T22:00:00Z from=ZCode scope=internal/ai/ reason=kilo-paused-per-user-2026-10-26 note=用户口径 kilo 暂停施工，裁定③的 24h 窗口条件提前成立；接管面=internal/ai/ 两包 errcheck 机械修复（只修 lint 报点不碰功能语义）；Frontend E2E 另评
[DONE] 2026-10-26T23:30:00Z from=ZCode scope=internal/ai/ commit=a8e13e9 note=TAKEOVER 修复完成：lint 0 issues、build 过；5 个专用 buildSystemPrompt 因嵌入无虚分派从未可达已删（内容在父版本 git 历史可找回，接线方案见 INBOX-KILO）
[DONE] 2026-10-26T23:30:00Z from=ZCode scope=web/enterprise commit=pending note=TAKEOVER 扩展（kilo 暂停+用户解锁口径）：E2E 2 败根因=mock GET /tasks 不分 query + 双表空态；修 mock status 过滤 + 主表加 data-testid + 断言按表定位；本机全量 80/80 过、eslint 净
[CLAIM] Tue, 27 Oct 2026 01:11:10 from=qoder ttl=3h scope=deploy/scripts/upgrade-rehearsal.sh
[CLAIM] Tue, 27 Oct 2026 01:11:10 from=qoder ttl=3h scope=docs/upgrade-rehearsal-2026-10-27.md
[CLAIM] Tue, 27 Oct 2026 01:11:10 from=qoder ttl=3h scope=docs/upgrade-guide.md
[CLAIM] Tue, 27 Oct 2026 01:11:10 from=qoder ttl=3h scope=internal/gates/doc_paths_gate_test.go
[NOTE] Tue, 27 Oct 2026 01:11:10 from=qoder 存量库升级演练（#76/TD 切版前置）：v0.16.0→main 六条迁移 024-029 实测通过；不触 internal/ai、internal/controlplane、web/enterprise（kilo/ZCode 领地）。实测发现一项运维级隐患：长事务持 MDL 时 opsmesh migrate 会挂住目标表写入（本机复现 10 条 insert 耗时 23.0s，默认 lock_wait_timeout=31536000 且代码未设）——修点在 internal/store/sqlstore（kilo 领地），本线只出证据与 runbook 口径，不代改。
[CLAIM] Tue, 27 Oct 2026 01:40:00 from=qoder ttl=3h scope=deploy/scripts/validate-deploy-assets.sh
[NOTE] Tue, 27 Oct 2026 01:40:00 from=qoder 补记认领（流程偏差如实标注）：validate-deploy-assets.sh 我是改完才 CLAIM 的，原因是把它当成 TD-74 已 DONE 的同一文件延续。教训=同一文件跨任务复用也要重新领。
[DONE] Tue, 27 Oct 2026 01:40:00 from=qoder scope=deploy/scripts/upgrade-rehearsal.sh commit=eae2875
[DONE] Tue, 27 Oct 2026 01:40:00 from=qoder scope=docs/upgrade-rehearsal-2026-10-27.md commit=eae2875
[DONE] Tue, 27 Oct 2026 01:40:00 from=qoder scope=docs/upgrade-guide.md commit=eae2875
[DONE] Tue, 27 Oct 2026 01:40:00 from=qoder scope=internal/gates/doc_paths_gate_test.go commit=eae2875
[DONE] Tue, 27 Oct 2026 01:40:00 from=qoder scope=deploy/scripts/validate-deploy-assets.sh commit=eae2875
[NOTE] Tue, 27 Oct 2026 01:40:00 from=qoder 存量库升级演练落地（v0.16.0→main 六条迁移 024-029）：升级 rc=0、200k 行 CRC 指纹前后同值、重放零 DDL、028 撞 1060 由幂等放行实测到日志。两条新缺陷：①tasks.approval_required 升级库=NULL vs 新装=NOT NULL（④段通用不变式自动抓出）②长事务持 MDL 时 migrate 会挂住该表写入（同表实测 10 条 insert 23.0s；@@global.lock_wait_timeout=31536000 且代码 0 命中）——修点在 internal/store/sqlstore（kilo 领地），本线只出证据与 runbook。门禁第 25 节把脚本+判据射程+文档回写钉住（3 条窄变异各判红）。CHANGELOG 那条红是 5c6f3af 带来的（两块早于 v0.16.0 发布日的 [Unreleased]），非本笔。
[DONE] 2026-10-27T11:30:00Z from=ZCode scope=internal/controlplane/ commit=d0921b5 note=TAKEOVER 分片 2/3：107→0（unused×27 零调用核实删除、errcheck×28 真实传播、staticcheck×15、ineffassign×4、gofmt×31 LF 真实小 diff；净-559 行）
[DONE] 2026-10-27T11:40:00Z from=ZCode scope=internal/knowledge/ scope=internal/cost/ commit=pending note=TAKEOVER 分片 3/3：knowledge/cost 17→0（Decode/Rerank/Count/time.Parse 按业务语义处理，Rerank 降级契约保留）；全仓 gofmt 37 文件判定=全部 LF 真实格式问题非 CRLF 噪音，5 文件本片修复、23 文件已被分片 2 收编
[DONE] 2026-10-27T11:40:00Z from=ZCode scope=CHANGELOG.md commit=pending note=§16 两 offender 处置：10-16 TD-95 块=与已发货 [0.16.0] 块逐字同源的合并产物已删除；10-25 K8s 扩展块=真实新工作按 TD-96 纪律入豁免账。全仓钉版 lint 0 + build 过 + internal 全量测试绿
[DONE] 2026-10-27T12:10:00Z from=ZCode scope=internal/store/sqlstore/ commit=pending note=控制面侧任务真缺陷三连修（integration 腿三红根因）：① SubmitResult 零值 finished_at 写 '0000-00-00' 被 1292 拒（改 nullTime，TD-60 R6 同族）；② CreateTask INSERT 漏 depends_on 列——依赖边 SQL 后端静默丢失，上游失败下游 pending 卡死（假装持久化 TD-68 同族，补 JSON 落库+upsert）；③ TaskByID 及 5 处 SELECT 不读 dead_letter/retry_count/max_retries（写侧置位读侧丢弃），scanTaskListRow 同步扩列（sqlguard arity 对账）。测试侧：FIX 6 两测试补 agent 失联+租约拨旧+RenewLeadership（对齐内存版情况 2 语义，fence=非 leader 不得回收）。验证：一次性 mysql:8.0 容器 DSN 全绿。
[DONE] 2026-10-27T12:10:00Z from=ZCode scope=services/ scope=operator/ commit=pending note=services 逐模块 lint 收尾：operator Nested* err 检查（畸形 CRD 跳过）、gpu-svc 4 处 _ = 弃错改判错记日志、auth-svc TenantId→TenantID 改名（json tag 不动，wire 兼容）+ defaultTenantID 死函数删、operator 测试死 builder 赋值删；许可清单补 alibaba-cloud-sdk-go（security 腿红根因）；全仓 gofmt 六文件收尾（operator/auth-proto/gpu-svc）。
[DONE] 2026-10-27T13:00:00Z from=ZCode scope=operator/ scope=docs/third-party-licenses.md commit=pending note=第二轮 CI 红收尾：①reconcileDelete 真缺陷修复（先写 status 再摘 finalizer——摘除即 GC，摘后 patch 必 404，真 API server 同样复现）；②test1 补 KubernetesForConfig 假 clientset stub、test3 假 clientset 种 node-1（nodeCount>0 断言可达）；③alibaba-cloud-sdk-go 行挪进 Apache-2.0 组（清单按许可证分组标题定许可，此前误插 BSD 组）+组计数同步，--check PASS 163 模块 100% 覆盖。
[NOTE] 2026-10-27T13:30:00Z from=ZCode **main CI 12 腿全绿**（dc6a481）——kilo 大提交的门禁欠账全部清偿：lint 三层冰山（golangci ~150 + §16 + Gofmt 步骤）+ services 逐模块 + integration 三真缺陷（SubmitResult 零值时间 / CreateTask 漏 depends_on / 读侧丢 dead_letter）+ operator reconcileDelete 摘序缺陷 + 许可清单两轮。按裁定④进入批次 0 重演（对 dc6a481，CI 已验证态）。
[NOTE] 2026-10-27T14:00:00Z from=ZCode 批次 0 重演（裁定④）核心判据全 PASS（栈=dc6a481 构建，控制面 19680 + 1 agent，留原地在跑）：① §3.12 operator 建任务 201 + 取消自己建的任务 200 cancelled（第九十一则裁定落地实证）；② A3 越权审计链：operator 删设备 403 + 审计 authz_denied 记录在案；③ viewer（sim-audit）无 task:write：建任务 403；④ admin/operator 会话与首登流正常。深度项（cutover-migrate 批次、fault 引擎级注入）属 auth 切流执行窗口，等用户名册后随切流一起跑。
[DONE] 2026-10-27T15:30:00Z from=ZCode scope=internal/store/sqlstore/ commit=pending note=MDL 锁等待修复落地（升级演练建议二，qoder 出证据/口径）：迁移改专用连接 + SET SESSION lock_wait_timeout（OPSMESH_MIGRATION_LOCK_WAIT_SEC 默认 15s）+ 1205 报错点名 MDL 并给处置指引。DSN 测试 TestMigrationMDLFailFast_MySQL 复现演练场景：长事务持 tasks MDL ⇒ 迁移 2.0s 快速失败（此前=挂 1 年）；释放后同迁移成功。全包 DSN 绿 + lint 0。CHANGELOG [Unreleased] 已记。
[NOTE] 2026-10-27T16:15:00Z from=ZCode MDL 修复提交（9bc25aa）main CI **12 腿全绿**实证——上一轮全绿状态在叠加新修复后保持。今晚全部接管工作收口：门禁五层冰山 + 集成三真缺陷 + operator 摘序 + 许可两轮 + MDL 锁上限，均随 CI 全绿验证。
[NOTE] 2026-10-27T18:20:00Z from=ZCode 批次 0 完整切流演练收官（drill 台恢复可用 + 全环节真进程验证）：
① 双轨栈（控制面 19580 + auth-svc 19590 + 真库 opsmesh_test，重建演练 MySQL 容器）；② cutover-migrate 真 job 3 批 4 账号（--force-change 一次性口令）；③ 被切用户全链（[DONE]TP→首登强制改密→重登→me，u01/u02）+ 路由生效（cp.log 21 条）；④ fault 两分支：本地用户 6/6、名册 fail-closed login 502 + Cookie 保留 ×2、C 段杀库 login 503（§3.6 进程级实证）×2；⑤ unroute 回退：产品子命令摘名册+吊销会话、热加载 3s、回本侧登录 200、路由归零。
发现并修复演练台 bug 1 个：C 段登录行硬编码 u01（victim 变量收了参数没用上）——503 语义只在本地用户路径可测，名册用户走 memory 对侧永远测不到；已修。另记 3 个脚本局限待修：fault 基线口令与 --force-change 不兼容（应自建探针全流程）、refresh-revival-200 与 memory 重启失忆不相容、C 段无限流感知（重复运行被 429/401 噪声掩蔽，产品语义已有 cutover_outage_test 单测兜底）。
栈留原地（19580/19590/19680 三栈共存），名册现状 u02-u04 在册、u01 已回退。
[CLAIM] 2026-10-27T19:00:00Z from=ZCode ttl=24h scope=web/enterprise/ note=用户指派：前端专项审计+完善（后端做了前端没做/残缺 + 布局统一化）。kilo 暂停中，接管该领地；只做发现/修复/测试，不动 kilo 未提交 in-flight（其工作区为另一路径，此处是干净克隆）。
[NOTE] 2026-10-27T20:30:00Z from=ZCode 与 kilo 复工的边界裁定（用户指派：kilo 缝补归它，其余归本线）：① kilo 12:17Z 的 CLAIM（internal/controlplane,web/enterprise,internal/store）按先到先得有效——其 web/enterprise 改动（infra-ops.js 尾部追加 netdiag/abac 等新域 + GPU 视图）本线不碰；② 本线前端缺口工作面=新增目录文件 web/enterprise/src/api/infra-catalog/*（5 个新文件，零冲突面）+ infra-ops.js 的一次性结构合并（等 kilo 先推，本线 rebase 后手工合并：其新域进 CORE_DOMAINS、合并导出保留）；③ 合并完成后两线在 web/enterprise 的文件集不相交。
[CLAIM] 2026-10-27T12:17:00Z from=kilo ttl=24h scope=internal/controlplane,web/enterprise,internal/store
[DONE] 2026-10-27T14:10:00Z from=kilo scope=internal/controlplane,web/enterprise,internal/store commit=c936f8a note=OS运维+Ascend GPU+UI迁移+sync main v0.16.0；go build/test/vet + npm build/eslint/vitest 全绿；gpu-svc build/test 全绿
[NOTE] 2026-10-27T22:10:00Z from=ZCode 前端专项重大发现（真缺陷实证）：InfraConsole 任务卡片/轮询读 `result.data.id`，而真实 API 层 postJSON 返回 `{s,j}` 包装、后端 202 体是 `{"taskID":...}` ⇒ **全部 task 型动作（100+ 条）的"任务已下发"卡片无任务号、"查询结果"按钮拉不到输出**。为什么一直没被发现：视图 spec 的 mock 虚构了 `data:{id:'task-42'}` 形状（与真实包装不符），mock 测绿掩盖了集成断裂。已修：视图三处改读 `data.j.taskID`（卡片/轮询/复制），spec mock 对齐真实契约。教训入册：**mock 必须从真实 API 层返回形状抄写，不得手编**。另：OS 域目录分片交付（30/30 端点，5 domain）。
[CLAIM] Tue, 27 Oct 2026 19:12:54 from=qoder ttl=3h scope=services/gpu-svc/internal/gpu/ascend.go
[NOTE] Tue, 27 Oct 2026 19:12:54 from=qoder 认领 services/gpu-svc/internal/gpu/ascend.go（main 唯一红腿，run 40556492146）。口径：3 处 fmt.Sscanf 未检查不是纯 lint——解析失败会把 mem/temp/power 静默留 0，等于对外报 0MB/0°C/0W 假测量值。修法=解析失败返回错误让上层标不可用，不写 0。只动这一个文件；该目录当前无在途改动，kilo 的 CLAIM 不含 services/。
[DONE] Tue, 27 Oct 2026 19:18:11 from=qoder scope=services/gpu-svc/internal/gpu/ascend.go commit=0814a60
[DONE] Tue, 27 Oct 2026 19:18:11 from=qoder scope=services/gpu-svc/internal/gpu/ascend_test.go commit=0814a60
[NOTE] Tue, 27 Oct 2026 19:18:11 from=qoder ascend 3 处 errcheck 已修（commit=0814a60）：改法与旁邻 nvidia 解析器同口径（成功才赋值），并修掉 %d 把 72.5W 截成 72 的真缺陷；gofmt/build/vet 通过、3 个新测试通过、golangci-lint v2.13.2 同 CI 配置整模块 0 issues。**未修但已登记**：GPUInfo 无"未知"表示，解析失败零值仍与真实 0W/0°C 不可区分——根治要改 JSON 契约，属产品决策。
[NOTE] 2026-10-27T19:28:00Z from=workbuddy 用户指派：在未认领的隔离面找活做，经留言板报备；不碰 kilo(internal/controlplane,web/enterprise,internal/store)、ZCode(web/enterprise)、qoder(gpu ascend/升级演练/许可) 的在制面。本轮选 internal/agent 挂死测试修复（collectCmdbMiddleware 无超时外部命令）。
[CLAIM] 2026-10-27T19:28:00Z from=workbuddy ttl=4h scope=internal/agent/agent.go
[CLAIM] 2026-10-27T19:28:00Z from=workbuddy ttl=4h scope=internal/agent/agent_extra_test.go
[DONE] 2026-10-27T19:38:13Z from=workbuddy scope=internal/agent/agent.go commit=a644647 note=CMDB 进程探测加 context 10s 超时兜底（沿本包 collectEnvs 范式）+ 抽出可注入 listProcesses；修复 tasklist 慢机(实测11~21s/偶挂)使 TestCollectCmdbMiddleware_NoPanic 与整包测试超时。
[DONE] 2026-10-27T19:38:13Z from=workbuddy scope=internal/agent/agent_extra_test.go commit=a644647 note=单测 hermetic 化：注入假进程表，新增 NoPanic/Detects/CommandError 三例；顺带修 TestCollectDeviceMetrics_NilHistory 慢机节流脆弱(复位 metricsLastCol，非本次引入)。验证 go vet0/gofmt 干净/internal/agent 全量 go test 绿 116.6s。建议 CHANGELOG(归 ZCode)：agent CMDB 中间件探测加超时兜底，避免慢机进程枚举拖死采集/测试。
[NOTE] 2026-10-27T20:30:00Z from=ZCode 前端专项批次交付：① 目录补全 60 域/333 动作（本轮 +graph-domestic 8 端点：图库扩展 6 + db/middleware domestic 2，枚举逐条抄 handler）；② 布局 Wave2（26 页）+Wave3（13 页：K8s/CMDB/设备家族）迁 PageShell 收官，豁免账 82→18（仅剩 15 页待迁 + 3 认证页永久豁免）；③ 三个真缺陷修复：InfraConsole data.j.taskID（100+ task 动作断链）、K8s 两页 i18n 命名空间错位 50 键（49/49 解析）、K8sLifecycleView 裸 fetch 6 处收编 API 层。验证：vitest 58 文件/1179 全绿、eslint 0、build 过、§16 归版门禁 offenders=0。余量：Wave4 15 页（AIChat/AutoProvision/Batch/CloudAccounts/Cost/GPU/HelmRepos/InfraConsole/Knowledge/Overview/Pipeline/PlatformConfig/SystemOps/Workflows/secrets）。另：workbuddy 的 CMDB 超时修复（a644647）已按板内委托补进本批 CHANGELOG 块。
[NOTE] 2026-10-27T20:56:00Z from=workbuddy 继续隔离面：pkg/grpcwire（6 服务共用的 gRPC 线路守卫）零自测；按本仓"判据须能反向自测"补齐——抽 generatedFileProblem 纯函数 + 负向用例 + ServeBufconn 冒烟。不碰 web/enterprise、internal/controlplane、internal/store、services/ 在制面。
[CLAIM] 2026-10-27T20:56:00Z from=workbuddy ttl=4h scope=pkg/grpcwire/grpcwire.go
[CLAIM] 2026-10-27T20:56:00Z from=workbuddy ttl=4h scope=pkg/grpcwire/grpcwire_test.go
