# product-design.md 深度复核清单（2026-11-08）

> 性质：**只读证据审计的产出**，供 owner 逐条拍板（立项书 `docs/projects-2026-11-07.md` §4 / §8-Q4）。
> 本清单落盘时**未改动 `docs/product-design.md` 任何 ✅/⚠️/❌ 判定**。
> 测量条件：树 `4737466e`（origin/main，2026-11-08 17:08Z fetch），全部为静态源码核对（grep/读文件），
> 未起服务做运行期验证；运行期证据由各包测试与 CI 承担。
> 口径：✅ 行须「证据存在 **且** 生产装配接线」（函数正确≠被调用）。

## 一、待降级候选（改对外宣称，须 owner 逐条认可）

### D1 「执行历史回放」——宣称存在，能力不存在

- 位置：`docs/product-design.md:112`（§3.2 作业编排 ✅ 行）与 `:388`（§8.1 已实现表）。
- 实测：全仓 `func.*[Rr]eplay`（Go 源码）仅命中 `internal/store/sqlstore/migration_test.go:537`
  （迁移重放测试，与作业回放无关）；`internal/orchestration/` 只有 `ListRuns`/`CreateRun`/`UpdateRun`
  （`sql.go:240`、`store.go:135`），注释里的「执行历史与回放」（`handler.go:289/620/640`）无对应实现。
- 对照：`README.md:241` 已明确「执行历史**回放** ❌ 未实现（2026-10-05 复核）」——两份文档口径矛盾。
- 建议：两处删「回放」字样或改为「执行历史（仅回看，无重放）」，与 README 对齐。**降级幅度最小：只去掉一个词。**

### D2 「灰度指标自适应推进」——实现存在，生产未接线（API 恒 501）

- 位置：`docs/product-design.md:113`（§3.2 部署编排 ✅ 行）；`README.md:232` 同样宣称（跨文件，见 §三）。
- 实测：`internal/deploy/auto_advance.go` 的实现与测试齐全，但全仓
  `NewAutoAdvanceManager` / `SetAutoAdvance(` 的调用方**只有测试文件**
  （`auto_advance_test.go:55`、`deploy_coverage_test.go:54-55` 等）。
  生产装配 `internal/controlplane/factory/server_factory.go:56/67` 只调 `deploy.NewHandler(store, disp)`，
  从不注入 ⇒ `h.autoAdvance == nil` ⇒ `internal/deploy/handler.go:221-226` 对
  `/auto-advance` 一律返回 **501 "auto-advance not enabled on this handler"**。
- 建议（二选一，owner 拍）：
  - a. 文档降级：该行去掉「灰度指标自适应推进」或标注「实现存在、出厂未启用（501）」；
  - b. 代码接线：在生产装配注入 AutoAdvanceManager——那是**代码变更另批**（含配置开关/测试/门禁），不在本批复核范围。
- 注意：同层的「发布门禁 + 自动回滚」是**真接线**（`internal/deploy/handler.go:440/486`），不受影响，不降。

## 二、措辞修正候选（不动 ✅ 判定，仅修过期/不确表述；同样先报 owner）

| # | 位置 | 现状表述 | 实测 | 建议 |
|---|---|---|---|---|
| W1 | §5.2 审计合规行（`:230`） | 「✅ + 6 月归档 + 导出 + 等保三级**报告**」 | 归档/导出为真（`--audit-retention-days` 默认 180，`config.go:533`；`audit_log_archive` 搬迁 `server_tasks.go:402`；`GET /api/v1/audit/export` `audit_query.go:117`）；但「等保三级报告」实体是 `README.md:1037` 的**合规对照表**，不是测评报告 | 「报告」→「对照表」 |
| W2 | §5.2 商业支持行（`:233`） | 「SLA 保障 + 工单系统 + 专属技术支持」 | 仓内无任何交付物证据；立项书 P5 明确「不承诺 SLA、不定价」 | 标注「商务洽谈项，未交付」（属 P5 范围，owner 定） |
| W3 | §3.4 脚注¹（`:138`） | 「部署配置（Dockerfile×5+compose+helm，默认 disabled）齐备」 | 实体已改为**共享** `Dockerfile.service`（仓库根，`--build-arg SERVICE` 注入）；`release.yml` 构建矩阵含全部 5 域；compose 5 条目在位（`docker-compose.prod.yml:895/932/1015/1055` 等）；helm `values.yaml` 五域条目在位（`:560/653` 等）。「齐备」实质成立，「×5 个文件」字面过期 | 「Dockerfile×5」→「共享 Dockerfile.service × 5 域」 |
| W4 | §6 场景表电信资源池行（`:257`） | 「单网段 200-2000 台自动纳管」 | 规模宣称无压测证据（P3 压测脚本未启动） | 加注「设计目标，压测证据待 P3」 |
| W5 | §3.3 审计行（`:127`） | 「6 月留存**规划**」 | 实际已实现且强于宣称（180 天默认 + 归档表 + 导出端点，见 W1） | 「规划」→「已实现（默认 180 天，可调）」；可选 |

## 三、跨文件牵连（本批只登记，不越界改）

- `README.md:232` 与 D2 同款宣称「灰度自适应推进 ✅」——若 owner 裁 D2-a，README 需同步一批
  （README 也在 capability gate 扫描面内，改动须同跑门禁）。
- `README.md:1044` 等保对照表「审计≥6 月」行仍写「运维侧定期导出（DELETE 前先备份）」，
  落后于自动归档实现（W1 证据）——登记为 README 侧过期项，不在本批动。

## 四、复核通过、保留 ✅ 的抽查面（证据存在且接线）

- §3.1 六行：设备纳管/任务执行/配置下发/服务管理/文件分发/状态监控——证据路径全部存在（capability gate 36 条 0 缺失）；告警异常检测 Z-Score+EWMA 实现在 `internal/alertengine/anomaly.go`（含测试）。
- §3.2：CMDB、部署编排（除 D2 短语外：门禁自动回滚 `handler.go:440/486`、联邦协调 `internal/deploy/federation.go`）、K8s 管理（workload scale/restart/rollback 真实现 `k8s_deployments.go:60/104/142`；集群级 lifecycle upgrade/rollback/cert-rotate 已按裁决改**显式 501**（`internal/k8slifecycle/handler.go:462-470`），product-design 未把这三项宣称为 ✅，无矛盾）、OS 优化（模板实数 27：presets 8+19，强于 README「14+」）、中间件部署。
- §3.3：多租户（billing 7 端点 `server_lifecycle.go:406-412` + quotas `:446-447`）、RBAC、联邦（±5min 防重放 `federation.go:43`）、密钥管理、日志检索（TF-IDF `internal/fulltext/fulltext.go`）、审计、HA（PDB `deploy/helm/opsmesh/templates/controlplane-pdb.yaml:3/10`）。
- §3.4 六域「已接线就绪」：`service_proxy.go`/`bot_bridge.go` 在位、前端六路由（router `:123-159`）、compose/helm/release 矩阵齐备——该行宣称的是**接线**而非各域功能完整，与脚注口径自洽，保留。
- §7 默认值/安全表逐条对 flag：worker-concurrency=4、task-timeout=120s、task-lease-sec=300、leader-ttl-sec=15、leader-tick-sec=5、max-procs=256、max-files=4096、shutdown-timeout=15s（`config.go:495-531`）；心跳 10s / 取消轮询 2s（`internal/agent/agent.go:696/809`）；loginGuard 5 次锁 15min（`auth_login.go:191` 注释与实现）；cookie-secure 生产默认 true（`config.go:609`）；其余 flag 全部存在。
- §5.2 已如实降级项（OIDC/SAML/LDAP ❌、密钥轮转未实现、插件市场无执行层）与代码现状一致，保留原样。

## 五、复算命令（任何人可复核）

```sh
# D1：回放函数零命中（除无关迁移测试）
grep -rn 'func.*[Rr]eplay' --include='*.go' internal/ pkg/ services/
# D2：生产装配无注入（输出只有定义与测试）
grep -rn 'NewAutoAdvanceManager\|SetAutoAdvance' --include='*.go' .
sed -n '50,70p' internal/controlplane/factory/server_factory.go
# 门禁（改动 product-design/README 后必跑）
go test ./internal/gates/ -run 'TestCapability|TestDocPaths'
```