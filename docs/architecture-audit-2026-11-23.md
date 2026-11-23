# OpsMesh 架构与交付力审计（2026-11-23）

> **性质**：只读取证 + 结论，不改代码、不承诺排期。
> **基线**：`origin/main` @ `74ea0a92`（2026-11-23），工作区 clean。
> **方法**：`git ls-files` 全量统计 + 源码逐点核对；所有数字可由 §8 命令复跑。
> **口径**：除注明「全仓」外，计数均为**生产代码**（排除 `_test.go`）；「行」为物理行。
> **边界**：未做真机压测、未做竞品实测；市场判断 = 仓库自述 + 公开认知，已在文中标注。
> **修订**：本版对同日口头评估中 6 处失实做了更正——
> ① 控制面**已有** `/metrics`（自建注册表，30+ 指标），不是「不可观测」；
> ② controlplane 已按 TD-87 抽出 11 个子包，父包生产代码为 245 文件 / 78,888 行（不是「531 文件全是上帝包」）；
> ③ `device-svc` 的 `StartDiscovery` 已于 2026-09 真实化（`pkg/discover.Sweep`），不是写死 3/254 的 stub；
> ④ `task-svc` 已有 schedule/reclaim/leader 三循环与 K8s Lease 选主，不是「缺四大机制」；
> ⑤ SSE 已有 Redis 跨副本广播（ADR-2026-11-15 §ADR-2），不是单实例语义；
> ⑥ `cmd/device-sim` 的「假成功」已在注释中记录并修复（现非 2xx 如实计错），不是现存缺陷。

---

## 0. 结论先行

1. **工程量真实**：25.5 万行 Go 生产码 + 19.3 万行测试（43.0%）+ 6,840 个测试函数。不是脚手架。
2. **架构级缺陷存在，且是根因级**：所谓「运维能力」= **服务端拼 shell 脚本 + 下发执行**。618 处派发任务后返 202，126 个 `build*Script`，635 处 `Fprintf` 拼脚本。平台**没有目标系统的状态模型**，因此无法幂等、无法漂移检测、无法收敛。
3. **前端是全项目最强的一层**（4/5）：i18n 有守卫、权限门控系统化、91 个测试文件 + 10 个 e2e spec、TODO/FIXME 为 0。
4. **产品方向对，证据不足**：信创私有化运维是真需求，但 5 条准入门槛（国产 DB / 多架构 / 国密 / 离线包 / 压测）**没有一条已经跨过**；其中 3 条已有工具或半成品，缺的是「流水线产物 + 验证报告」。
5. **当前最该做的不是再加功能**：广度扩张（每批 +30~485 个 action）边际收益已接近零，只会放大维护面与攻击面。

---

## 1. 量化基线（实测）

| 指标 | 实测值 |
|---|---|
| 仓库文件 / Go 文件 | 2,508 / 1,556 |
| Go 生产代码 | **255,131 行** |
| Go 测试代码 | **192,847 行（43.0%）** |
| 测试函数 | 6,840 |
| `internal/controlplane` | 531 文件 / **154,672 行**（占生产码 60.6%） |
| ├ 父包直属 | **472 文件 / 139,619 行**（生产 245 文件 / 78,888 行；测试 227 文件） |
| └ 已拆子包 | 18 个 / 59 文件 / 15,053 行（TD-87 抽出其中 11 个） |
| `internal/store` | 193 文件 / 48,519 行 |
| `services/` 12 个微服务 + tf-provider | **106,146 行**（tf-provider 1,753） |
| HTTP 路由 | 888 个注册点 / **803 条唯一路由** |
| 文档 | `docs/` 128 个 md / 50,950 行；全仓 149 个 md / 75,971 行 |
| 前端 | 370 文件 / 78,494 行；125 个 `.vue`（90 视图 + 34 组件 + 1 App）；91 个测试文件；10 个 e2e spec |

---

## 2. 架构级缺陷

### A. 内核是「拼 shell」，不是「运维模型」（最严重）

| 模式 | 生产代码计数 |
|---|---|
| 派发任务后返 `202 + taskID` | **618 处** |
| `fmt.Fprintf(&b, ...)` 拼脚本 | **635 处** |
| `func build...Script` | **126 个 / 48 个文件** |
| `shellQuote(` | 496 处 |
| `exec.Command` | 36 处 |

典型链路（`internal/controlplane/db_pg_repl_ops.go:156`）：

```go
script := buildDBPGReplScript(req.Action, db, ...)
task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "db_pg_repl_"+req.Action)
```

`ansible_ops.go` 同理：把 playbook 用 heredoc 写进 `/etc/ansible/playbooks/`，再 `ansible-playbook`。

**后果（按严重度）**：

1. **无状态模型**：平台不知道「这个 PG 现在有几个 slot、是否健康」，只知道「执行过命令、stdout 是这些字」。→ 不可幂等、不可漂移检测、不可收敛。
2. **目标机必须预装 CLI**（psql/mysql/mc/hdfs/ansible/kubectl…），缺一个即整链失败。
3. **结果不可结构化**：前端只能展示日志，做不出真实拓扑/健康度视图。
4. **永久攻击面**：496 处 `shellQuote` + agent 白名单，是在「生成任意 shell」前提下的加固，防线永远在补。

**为什么广度增长这么快**：`eb8714e4` 一次提交 485 个 action / 23 文件；批次 67~74 每批 30~485 个。**这个架构让广度极便宜、深度极昂贵**，于是团队持续做广度。

### B. 父包仍然过大

- 父包生产代码 **245 文件 / 78,888 行**（含测试 472 文件 / 139,619 行），全仓 896 个 `mux.HandleFunc` 平铺、803 条唯一路由，没有 handler/service/repository 分层。
- TD-87 已抽出 11 个子包（presets / pluginhost / credentials / metricscache / loginguard / svcproxy / ratelimit / tenantguard / auditsafe / cmdbcollector / enterpriseui），这是真实进展；但剩余父包仍是所有 CI 抖动与合并冲突的物理根源。
- 直接成本已经发生：2026-11 中旬 main 连红、#138→#146 六轮合并撞车，物理根源就是这个包太大 + 高频直推。

### C. 数据层方言只做了半截

- `sqlstore` 生产文件里 **95 处 `ON DUPLICATE KEY`**，分布在 **34 个文件**；生产代码合计 128 处。
- `dialect.go` 仅 325 行，注释自认：「只接本批有消费者的方法；无消费者的能力（LIMIT-OFFSET 分页、通用 Upsert、DDL 自增翻译）不入口，列 A3-2/A3-3」。
- `pgx` 生产代码仅 6 处引用；`go.mod` 只有 `go-sql-driver/mysql` + `pgx/v5`，**没有达梦/金仓/openGauss 专用驱动**。
- `internal/controlplane/domestic_ops.go` 里的 kingbase/达梦 是「对国产 DB 的运维动作脚本」（依赖目标机 CLI），不是存储层方言支持。

**结论：PG 支持是「能启动」，不是「能承载」。** 而信创准入（达梦/金仓/openGauss）几乎全走 PG 协议——这条半截路直接卡住产品定位。

### D. 可观测性：整体强，但有明确空洞

已确认存在且覆盖良好（`internal/metrics` 生产 8 文件 / 1,142 行 + `pkg/metrics` 生产 1 文件 / 586 行，手写 Prometheus 文本格式，无 `prometheus/client_golang` 依赖）：

- `opsmesh_agents_total`、`opsmesh_tasks_total{status}`、`opsmesh_task_queue_depth`、任务时长直方图
- `opsmesh_http_requests_total` / 时长直方图（路径归一化 + 序列基数上限）
- 审计链自检（`opsmesh_audit_chain_*`）、agent 验签、插件扩展点、ABAC 影子/强制、网关路由级
- 设备在线/离线、活跃告警、未关闭工单、**store 静默失败累计**（这条设计得很好）
- 8080 与 9091 **同一渲染器**（`app_gauges.go` 记录了此前两端口集合不一致导致告警永不触发的真实事故）

**缺口**：指标名清单中**没有** `leader` / `lease` / `reclaim` / `deadletter` / `schedule` 相关指标。这意味着「选主失败」「任务卡死被回收」「死信堆积」「定时派生停摆」在监控面上不可见。对一个宣称 HA 的平台，这是应该补的。

### E. 双轨仍在，但已不是「27,000 行纯负债」

TD-60 的进展比口头评估时掌握的高：

- 轻四域（gpu/runbook/incident/autoscaler）已纯 `service_proxy` 代理化，controlplane 侧零本地实现。
- 2026-09-29 已删除 5 个从未进部署清单的服务（deploy/plugin/bot/workflow/grafana-bridge）。
- 2026-10-11 实测：原约 27,000 行重复残余 **5,980 行（缩减 78%）**。
- 2026-11-16：阶段 2（task/device/auth）进度 **60%**；task-svc 已有 schedule/reclaim/leader 三循环 + K8s Lease 选主，device-svc 发现扫描已真实化，auth-svc 已补 Redis SessionStore/loginGuard/DeviceFP。

**仍真实的负债**：核心三域尚未完成切流。task 域堵点是 **agent gRPC 适配层（约 300 行新开发）+ leader 身份切换**；device 域 netsec/federation 需先下沉 `pkg` 再切。按仓库自己的稳妥序（auth → task leader → HTTP 切 → agent 适配 → device 下沉），收尾可删约 5,000 行、新开发约 500 行。这不是「推倒重来」，但也不是「已经完成」。

---

## 3. 前端评估（4/5，全项目最强）

**做得好的（有证据）**：

- **i18n 是真做了**：26 个 locale JSON、11,116 行，且有 `naked-keys.test.js` + `ROUTE_DOMAIN_MAP` 守卫，漏映射直接 CI 红。
- **权限门控系统化**：`requirePerm` 122 处，`router-perms.test.js` 守着。
- **测试密度高**：91 个 `.test/.spec` 文件 / 334 个源文件（27%），覆盖 stores / API 契约 / 组件 / 路由 / i18n；10 个 e2e spec（含 `e2e-real/` 真后端）+ 4 个 fixture。
- **纪律好**：`.js/.vue/.ts` 中 `TODO/FIXME` **0**，`console.log` **1**。
- **设计令牌存在**：`design-tokens.css` 1,117 行 + `tokens.css` 198 行。
- **可访问性在补**：211 处 `aria-`，`role="dialog"` 8 处；#140 刚修了抽屉/弹窗键盘可达。

**真实短板**：

- **组件复用不足**：90 视图 vs 34 组件（2.6:1）。成熟 Vue 中台通常 1.5~2:1。最大的 `K8sResourcesView.vue` 883 行、`DeviceDetailView.vue` 831 行；组件侧 `Sidebar.vue` 955 行、`Table.vue` 943 行——页面级/组件级单体都存在。
- **a11y 仍早期**：211 处 `aria-` 但只有 8 处 `role="dialog"`，复杂弹层/抽屉的语义还靠零散属性。
- **天花板由后端决定**：90 个视图基本是「表单 + 表格 + 日志输出」的 1:1 映射，因为后端给不出结构化状态。前端再强也做不出真实拓扑图/健康度。

**评级：4/5（相对同类产品）。** 这是可以拿去演示、不会被挑出「像学生作业」的一层。

---

## 4. 产品定位与市场

**定位**：私有化单中心 B/S 自动化部署与运维平台；差异化 = 网段自动纳管 + 并行任务 + 完整任务生命周期；目标 = 信创私有化运维投标。

**方向判断：对。** 信创私有化是真需求，大厂（蓝鲸）覆盖不到中小项目，价格带是空的。

**但仓库自己的文档已经把问题说透**（`docs/market-gap-analysis-2026-11-09.md:13`）：

> 「我们最大的风险不是『能力少』，是『能力与标书错位』。21 条要求里，5 条是准入阻断级（不补就投不了/过不了）：国产数据库、国密、离线安装包、多架构镜像与 ARM64 纳管、规模化压测证据。这 5 条全部是『真缺能力』，没有一条能靠写文档蒙过去。」

对照本次代码实测：

| 准入门槛 | 仓库现有证据 | 判定 |
|---|---|---|
| 国产数据库（达梦/金仓/openGauss） | `go.mod` 仅 mysql + pgx；sqlstore 95 处 MySQL 专有 UPSERT；`domestic_ops.go` 是运维脚本不是驱动 | ❌ 未跨过 |
| 多架构 / ARM64 | `.goreleaser.yml` 明确 `linux/amd64 + linux/arm64`（agent 二进制）；但 `release.yml` 的 buildx **无 `--platform`**，`Dockerfile.agent` 无 `TARGETARCH`；无 ARM64 真机纳管报告 | ◐ 二进制有，镜像/验证无 |
| 国密 SM2/3/4 | 全仓 0 处实现 | ❌ 未跨过 |
| 离线安装包 | `build-airgap-bundle.sh`（163 行）+ `install-airgap.sh` + selftest + `docs/airgap-install-sop.md` 齐全；但 `release.yml` 0 处 airgap，无已发布产物/CI 验证 | ◐ 工具在，产物/证据无 |
| 规模化压测 | `scripts/loadtest/` + `BASELINE.md`：50 VU / demo memory / 250 RPS / p99 5.86ms / 0% 错误 PASS；无 100/500/1000 agent 规模 | ◐ 有 50 VU 基线，无规模证据 |

另有一条 P0 但不在「5 条准入」清单里：**2FA**。市场缺口文档自己标了 `❌ 仅 needMFA 标志且仍下发 token（services/auth-svc/internal/http/gateway.go:218）；无 TOTP/短信/邮箱码`，全仓 `TOTP|2FA` 0 处实现。

**竞争现实**（公开认知，非实测）：
- 蓝鲸：CMDB/Job/SOPS/监控 全套 + 免费 + 生态成熟。OpsMesh 的「网段纳管 + 并行任务」在蓝鲸里是 Job 平台的一个功能，不是品类。
- Spug / JumpServer：轻量段位已被占满，且都免费。
- Ansible AWX / Semaphore：自动化编排的默认答案；OpsMesh 的 Ansible 集成只是它的壳。
- 商业 AIOps 厂商：卖的是算法+行业模型；OpsMesh 的 `internal/ai`（5,981 行）+ aio-svc + eval loader 是真东西，但没被当成主打。

**结论：定位 2.5/5。** 差异化真实但窄（本质是「更好的任务执行器 + 自动发现」），而准入门槛没有一条已经完整跨过。**现在去投标，技术质询环节很难自圆其说**——仓库文档自己也写了「没有一条能靠写文档蒙过去」。

---

## 5. 后端增补 / 修缮清单

### P0 — 不补就投不了标

| # | 项 | 说明 |
|---|---|---|
| 1 | **PG 方言补完 + 国产 DB 真驱动** | 消灭 95 处 `ON DUPLICATE KEY`；补分页/Upsert/DDL 自增/`information_schema` 差异；接 openGauss → 达梦/金仓。**唯一同时卡住信创与自身 store 的项。** |
| 2 | **发布证据链** | 容器镜像 `--platform linux/amd64,linux/arm64`；airgap bundle 进 release 并产出可下载产物；SBOM/签名已有基础，补「安装验收报告」。**工具大多已在，缺的是流水线产物。** |
| 3 | **规模化压测** | 100/500/1000 agent 三档，p50/p99 + 资源曲线 + 版本 SHA；现有 50 VU 基线只能证明「单机 demo 不慢」。 |
| 4 | **2FA** | 等保 B1，成本低、收益直接；当前 `needMFA` 只下发 token，不校验第二因子。 |
| 5 | **国密** | 这是商业决策不是技术决策。若确定不做，就把所有文档口径统一降级——**不能一边写 ✅ 一边没有**。 |

### P1 — 架构修缮（决定能不能规模化交付）

| # | 项 | 说明 |
|---|---|---|
| 6 | **「拼 shell」→「能力适配器」** | 定义 `OpsAction`（validate → plan → execute → verify → 结构化 result）。不必一次全改，先挑 2~3 个域（PG / K8s / 中间件）做样板，让返回体从 stdout 变成对象。**这是从「命令执行器」变成「运维平台」的唯一路径。** |
| 7 | **继续拆父包** | 245 文件 / 78,888 行仍是所有 CI 抖动与合并冲突的物理根源；至少把路由注册与 handler 分组抽成显式模块边界。TD-87 的 11 个子包证明这条路走得通。 |
| 8 | **TD-60 收尾** | 按仓库自己的稳妥序：auth（零代码）→ task leader 切换 → HTTP 切 → agent 适配（约 300 行）→ device 下沉。收尾可删约 5,000 行、新开发约 500 行。现在 60% 的进度最怕「停在这里」。 |

### P2 — 补短板

| # | 项 | 说明 |
|---|---|---|
| 9 | **补齐指标空洞** | leader/租约、任务回收、死信、定时派生。当前这些关键机制在监控面上完全不可见。 |
| 10 | **真实集成面** | Prometheus/Zabbix/ELK 告警入站；K8s exec/CRD；云 API（阿里云 SDK 已在，可复用）。 |
| 11 | **前端组件化** | 把 883/831 行的巨型视图拆成组合式组件，视图:组件比拉到 2:1 以内；同时给 `Sidebar.vue`（955 行）/ `Table.vue`（943 行）减重。 |

---

## 6. 多线协作与流程

**现状**：单一 lead = `kilo`；机动线 `qoder` / `workbuddy` / `ZCode`；所有提交同一 GitHub 账号；靠 `CLAIMS.md`（append-only 领地日志）+ `coord-gate.py` 防撞车。

**卫生债**：75 个 worktree（其中 34 个 prunable）；155 个本地分支 / 90 个远程分支。

**制度漏洞（已实际发生）**：`CLAIMS` 防的是「同文件认领」，防不了「两条线各自修同一个 main 红点再先后合并」。#138→#139→#142→#143→#145→#146 六轮即此。

**建议三条机械规则**：

1. 设「main 红点唯一 owner」角色，同一时刻只允许一条线修 main。
2. 合并前强制 rebase 到最新 base（branch protection: require up-to-date）。
3. 开 PR 前先查 open PR 是否同 scope（`gh pr list` + files 比对）。

---

## 7. 总评

| 维度 | 评级 | 一句话 |
|---|---|---|
| 工程量真实性 | ★★★★☆ | 25.5 万行生产码 + 43% 测试 + 6,840 测试函数 |
| 工程纪律 | ★★★★☆ | CI 门禁、lint ratchet、CLAIMS、128 篇文档，中上 |
| 架构 | ★★☆☆☆ | 拼 shell 内核 + 245 文件父包 + 核心三域双轨，根因级 |
| 前端 | ★★★★☆ | 全项目最强；i18n/权限/测试到位，组件复用偏弱 |
| 产品市场匹配 | ★★☆☆☆ | 方向对，5 条准入未跨，差异化窄 |
| 可交付性 | ★★★☆☆ | 代码能跑，但「可投标的证据」仍不完整 |

**行业坐标**：二线厂商中级水平。比外包交付件强一个档次（有测试、有门禁、有 i18n、有审计链），离蓝鲸这类平台型产品差一个架构代差，离「能中标」差的是**证据**而不是功能。

**如果只做三件事**：

1. 把 PG 方言补完（一条打通信创 + 自身 store）；
2. 把发布链路做出可验证产物（多架构镜像 + 离线包 + 100/500/1000 agent 压测报告）；
3. 挑一个域把「拼 shell」改成结构化适配器，作为架构转型样板。

其余广度扩张（再加 500 个 action）**边际收益已经接近零**，只会继续放大维护面和攻击面。

---

## 8. 复跑命令

```bash
# 规模
git ls-files '*.go' | wc -l
git ls-files 'internal/controlplane/*.go' | xargs wc -l | tail -1
git ls-files 'internal/store/*.go' | xargs wc -l | tail -1

# 拼 shell 内核
git grep -c 'StatusAccepted, map\[string\]interface{}{"taskID"' -- '*.go'
git grep -c 'fmt.Fprintf(&b,' -- '*.go'
git grep -c 'func build' -- 'internal/controlplane/*.go'
git grep -c 'shellQuote(' -- '*.go'

# 方言
git grep -c 'ON DUPLICATE KEY' -- 'internal/store/sqlstore/*.go'

# 指标空洞
git grep -n 'opsmesh_.*\(leader\|lease\|reclaim\|deadletter\|schedule\)' -- 'internal/metrics/*.go'

# 前端
git ls-files 'web/enterprise/src/**/*.vue' | wc -l
git ls-files 'web/enterprise/src/**/*.test.js' | wc -l
```

---

*本文只读取证，不构成排期承诺。P0/P1 的归属与顺序需 owner 裁决。*
