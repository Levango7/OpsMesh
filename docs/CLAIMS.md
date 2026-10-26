# CLAIMS.md — 领地与任务单一追加日志（v2 起）

> 协议见 ./COORDINATION-BOARD.md。追加式、勿改历史行、一律 LF 行尾、每行一条 [KIND]。
> 2026-10-15 前的旧记录留在 docs/INBOX-ZCODE.md 与 docs/INBOX-LEVANGO.md（只读档案，门禁照常解析）。

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
② TD-89 残余=**挂账观察**，触发条件=一个月内 service-container 判红 ≥3 次 ⇒ 上选项②（GHCR 源）；触发前不动 .github/workflows/；
③ main 红（kilo 领地）=**24h 窗口**：至 2026-10-27T21:30Z 未清红，任何线可按板协议 [TAKEOVER] 接管 internal/ai/ 的 errcheck 修复（只修 lint 报点、不碰功能语义；Frontend E2E 仍归 kilo）；
④ **main 转绿后即执行演练批次 0 重演，执行线=ZCode**（一石三鸟：§3.12 operator 取消权 + v0.16.0 运维能力 + TD-74 前闸实例化受检）。
