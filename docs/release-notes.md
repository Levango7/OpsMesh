# OpsMesh 发布说明

本文件记录 OpsMesh 各版本的发布说明，按版本号倒序排列。版本号遵循 [Semantic Versioning](https://semver.org/)，格式参考 [Keep a Changelog](https://keepachangelog.com/)。

---

---

## v0.13.0 — 2026-10-07 CMDB 检索真实化 + 插件宿主可接线 + 发布链"先过门禁再提权"，并首次量出存量升级阈值

三件事：把能力表里那行 ❌ 的 CMDB 检索换成真的全文索引召回；把"可插拔扩展"从框架变成**能接线的功能**
（默认不启用）；把发布链门禁挪到提权之前，并给客户一条可自查产物的验收命令。证据与过程见
`docs/commercial-readiness-review-2026-09-25.md` §38–§44、`CHANGELOG.md` 的 `[0.13.0]` 各明细块。

### 先说发布本身（客户会直接撞到）

**本版是发布链改造后的第一次真实执行**。`promote`（registry 侧改标、不重建）、`:latest`/版本 tag 的提权时序、
验收脚本第 ⑤ 项 digest 等式，此前只被本地跑与静态断言验过形状，**真实提权执行次数为 0**（§39 实测）。
tag 推上去后请用 `deploy/scripts/verify-release-artifacts.sh 0.13.0` 自查五项判据（镜像 tag 存在 / `.sig`+`.att`
挂在本版本 digest 上 / Release 存在且正文含能力降级清单 / chart 渲染出的引用真实存在 /
`:0.13.0` 与 `:<发布提交>` 同一个 manifest digest）。任一项不符请以门禁结果为准，**不要手工补 tag**。

### 破坏性 / 行为变更（升级前必读）

1. **存量库升级会把 `ci_items` 整表重建两次，且窗口内禁止并发写**。迁移 020（加 STORED 生成列 + 建首个
   FULLTEXT 索引）与 021（先 `DROP` 再按 7 列重建）不是普通加索引：mysqld 自己拒绝
   `ALGORITHM=INSTANT`/`INPLACE`（`ERROR 1845`）与 `LOCK=NONE`（`ERROR 1846 ... requires a lock`）。
   实测阈值（出厂 `mysql:8.0` + 出厂 `mysql.cnf`，mysqld 8.0.46）：1 千行 7.9s ｜ 10 万行 41–48s ｜
   **30 万行 117s ＝ 120s 等待预算的 97%** ｜ 100 万行 375.6s。
   **判据：升级前查 `SELECT COUNT(*), SUM(LENGTH(CAST(attrs AS CHAR))) FROM ci_items`，超过约 20 万行 /
   45 MiB 请预约维护窗口**（按 0.85 s/MiB 外推并留一倍余量）。因为迁移在控制面启动路径上同步执行且**早于
   HTTP 监听**，大表下的症状是 `deploy.sh up` 报"120s 内未就绪"——看着像新版本部署失败，实际是数据量导致的
   迁移耗时。该缺口已记 **TD-83**，本版未修（本版没有"部署前迁移"这一步）。
2. **微服务健康路径与端口环境变量名统一（TD-77）**：规范路径为 `/health` + `/ready`；历史别名
   `/healthz`（log-svc）与 `/api/v1/health`（incident/runbook/autoscaler）**本版仍注册在同一 handler 上**，
   外部探针不会因升级而 404，计划下个版本摘除。净增能力：runbook-svc / autoscaler-svc 此前**没有就绪端点**，
   统一后 `/ready` 可用。废弃端口键不再注入。
3. **MySQL 容器的日志与 redo 容量**：`deploy/monitoring/mysql.cnf` 原写的 `innodb_log_file_size = 256M`
   在 MySQL 8.0.30+ **静默失效**（挂载后实测生效值仍是默认 100M），本版改用 `innodb_redo_log_capacity = 256M`
   ⇒ **redo 空间首次真正按 256M 分配**（磁盘占用与 checkpoint 行为会变）。同时移除 `log_error` 文件重定向，
   mysqld 错误改走 stderr ⇒ **`docker logs opsmesh-mysql` 从"空白"变成有内容**；若你的日志采集假设是
   "读容器内文件"，需要改成采集 stdout/stderr。
4. **交付镜像 tag 的推送时序**：`:latest` 与 `:<版本>` 不再跟构建一起推送，而是在 Trivy/SBOM/cosign 全绿后
   由 `promote` 改标（不重建 ⇒ digest 不变，被扫的那份就是交付的那份）。依赖"每次构建都会刷新 latest"的
   自动化需改为按版本 tag 或 digest 取镜像。
5. **插件宿主（TD-62）默认不启用、零行为变化**：`--plugin-manifest` 默认为空 ⇒ 不构造宿主、不注入管理器，
   `opsmesh_plugin_hook_calls_total` 恒为 0（出厂 Prometheus 规则已按"未配置时不触发"编写）。本版把它做成
   **可接线的功能**，不是"已经在跑的插件系统"——请按前者规划验收。

### 新增能力

- **CMDB 检索走全文索引**（迁移 020/021）：`ci_attrs_text` STORED 生成列 + `ngram` 全文索引覆盖 7 个召回列；
  检索按 token 分流（长度=1 或含下划线的 token 回退 `LIKE`，其余走 `MATCH`）。**分流是必要的而非可选项**：
  实测单字 token 在 MATCH 下一律召回为空，整体换索引会把中文检索变窄（漏召回无补救，多召回可由下游
  `matchCI` 前缀匹配过滤——这个非对称性就是设计约束本身）。
- **版本自证面（TD-76）**：`opsmesh_build_info{service,version,commit}` 进入抓取面，确认镜像里编进去的版本
  不再需要 exec 进容器。
- **发布物验收命令**：`deploy/scripts/verify-release-artifacts.sh <版本>`，五项判据、PASS/FAIL/UNVERIFIED 分立。
- **插件运行时模型**：独立进程 + HTTP 契约（`internal/plugin/remote.go`）；清单格式与示例见
  `plugins/remote-example/README.md`。生产模式下清单解析/校验失败**即终止启动**（fail-fast），非生产模式
  告警后按未启用继续。

### 缺陷修复

- **日志检索整次查询失败**：`internal/logstore` 写侧主动把空值落成 NULL，而读侧用裸 `string` 扫描 ⇒
  任一可空列为 NULL 时整条 Query 报错。修法为可空列经 `sql.Null*` 中转 + 真库往返用例
  （`TestSQLNullRoundTripRealMySQL`，CI 首次执行即 PASS），并把该包单列进 integration job——此前它
  永远以 skip 姿态"通过"。
- **发布物验收把网络抖动判成产物缺失**（含一次真实误报的收回）：取不到 token / 传输失败现记 `UNVERIFIED`
  而非 `FAIL`，只有确定 404 才算缺陷；判据本身不再"看起来全绿但没核对上"。
- **可空列↔Scan 守卫自身的三个假绿**（TD-74）：门禁从"抽样"换成"逐站点账目"，修好函数名取空、
  宽扫描两头不管、mock 从不喂 NULL 三类失明；覆盖率下限以棘轮方式钉住。

### 供应链与门禁

- 企业版前端 2 条 npm HIGH 清零（`1c21985`；Trivy 公告库当日新增，非本仓回归但确实存在于我们的 lockfile）。
- shellcheck 门禁从 `-S warning` 抬到 **`-S info`**：此前隐形的 44 处存量清零，现 18 个交付脚本 0 findings
  （本机与 CI 同为 v0.10.0；`-S style` 经逐条判定**不设卡**，理由记在台账 TD-78）。
- CI 基础设施死亡的**重试判据**从"报错文本白名单"换成现象级三条件（`deploy/scripts/ci-infra-death.sh`：
  0 可重试 / 1 判红 / 2 判据失明，调用方把 2 当判红），并由门禁第 19 节守住"判据真的被调用"。
- 发布链 `concurrency` 串行闸（排队不取消）；门禁第 17 节把"先过门禁再提权"、第 18 节把"入站只绑环回"
  从口头约定变成静态断言；第 16 节防 CHANGELOG 归版失真复发。

### 能力降级清单（本版如实标注；按能力表验收时请跳过这些或先确认前置）

| 能力 | 本版状态 | 前置 / 说明 |
|---|---|---|
| 插件式扩展 | **需显式启用** | `--plugin-manifest` 为空即完全不启用（默认）；示例仅 `plugins/remote-example`，无插件市场或第三方分发 |
| CMDB 相关性窗口 | 缓解但**未消除** | `LIMIT` 仍拼在 token 循环之外，"最相关 CI 落在窗口外"只是被全文召回缓解（TD-79 在册） |
| 单字 / 含下划线 token | 走 `LIKE` | 与 ngram 粒度不一致，MATCH 空召回，故显式回退；这类 token 从索引得到的收益为零 |
| 链路追踪 | 只有"发得出去" | 无查询后端（TD-75）：OTLP 导出可用，平台内查不到 trace |
| 微服务 gRPC 面 | **无鉴权** | 出厂 compose 把宿主端口除白名单外**只绑环回**（门禁第 18 节断言）；跨主机调用需自建 mTLS |
| 外发告警真实送达 | 未端到端复验 | PagerDuty 载荷已按契约修正，真实送达仍未在客户环境复验 |
| 存量大表升级窗口 | **未处理** | 见"破坏性 1"与 TD-83：本版无"部署前迁移"步骤，`deploy.sh` 内不存在迁移入口 |
| 许可与第三方合规 | 法务结论未出 | MPL-2.0 再分发口径、NOTICE/THIRD_PARTY 完备性属商务 + 法务项 |
| `main` 分支保护 | 无 | 免费档限制；推送即入主干，靠 CI 与门禁事后拦 |

### 本版可复核的验证证据（都带观测量，不是静态结论）

- 版本源一致性：门禁第 1 节 [PASS] `Chart.yaml version/appVersion = 0.13.0`、`values-production` **14 处 tag**
  全等、`gitops production-segment` 全等（`internal/version.Version` 已纳入对账）。
- 部署资产门禁整跑：**PASS=63 FAIL=0 SKIP=1**；第 16 节对归版后的 CHANGELOG 判 `offenders=0`。
- 存量迁移演练：1 千→100 万行完整曲线（§44.2）；10 万行跑两遍 41.2 / 48.0s（±16%）；活体写入器直接
  INSERT 被锁表时**最大间隔 19.8s**（021 墙钟 21.7s）。
- `verify-release-artifacts.sh 0.12.0` 正向 **PASS=8 FAIL=0**（含第 ⑤ 项 14 个镜像 digest 等式命中）；
  反向 `RELEASE_SHA=0000…` ⇒ 14 条 `[FAIL]`、退出码 1。
- `go build ./...` rc=0；`shellcheck -S info` 18 脚本 0 findings；CI 各 job（含 Race detector / E2E 真实后端）
  在归版前最后一笔上为绿。

---

## v0.12.0 — 2026-10-04 gRPC 契约真实化 + 规则引擎真实化 + 八处"声明了但不成立"的收口

本版的主线不是新功能，而是把**此前对外宣称存在、实际不成立**的能力逐条变成事实，
并把两条"版本声明领先于产物"的发布链缺陷纠正掉。评估全文见
`docs/commercial-readiness-review-2026-09-25.md` §31–§32。

### 先说发布本身（客户会直接撞到）

`v0.12.0` 的 tag 与镜像在 **2026-10-04** 才真实生成。2026-10-03 那次"归版"把
Chart.yaml `appVersion`、`values-production.yaml` 的镜像 tag、gitops production-segment 的 tag
都写成了 0.12.0，却从未打 tag；实测 GHCR 上 `opsmesh-binary` / `opsmesh-agent` / `auth-svc`
的版本 tag 都停在 0.11.0。后果是**按生产默认值 `helm install` 的客户对三个镜像 `ErrImagePull`**。
同一次归版还把 CHANGELOG 里 54 个"已归入 0.11.0/0.10.0/0.9.x"的历史明细块整批改成了
`## [0.12.0]`，等于宣称半年的东西都在这一版里——已按 `v0.11.0` 原状逐条还原。
防复发：`validate-deploy-assets.sh` 第 1 节的版本源对账现已包含 `internal/version.Version`
（实测它当时比 Chart.yaml 落后一整版而门禁毫无反应）。

### 破坏性 / 行为变更（升级前必读）

1. **alert-svc `alerts` 表新增 `rule_id` 列**，启动期按 `information_schema` 判缺后幂等 `ALTER`；
   存量库自动补列，**不自动回滚**（本仓 `.down.sql` 永不自动执行）。老行该列为 NULL 且读得回来。
2. **OTLP 端点解释改变**：`http://host:4317` 这类带 scheme 的写法以前**拨不通**（12 个微服务的
   span 一条都没到过 collector），现在按 OTel 规范解析；`https://host:4317` 现在**会走 TLS**
   （以前只看端口是否 443）。若 collector 只监听明文而配置写了 `https://`，升级后会连不上。
3. **PagerDuty `acknowledge`/`resolve` 不再发 `payload.severity` 键**：此前发的是空字符串，
   而 `severity` 是枚举（`critical|error|warning|info`），空串属非法值、真实端点会判 400。
4. **告警规则评估真实化**：引擎现在真读调用方传入的读数并输出五态
   （`met`/`not_matched`/`pending`/`no_data`/`invalid`）。以前 `op=">"` 且阈值 <100 的规则
   **每次评估都触发**（假告警风暴），其他写法**永不触发**（假健康）。升级后看到的告警量
   与历史不同是预期——历史那个数是假的。
5. **抓取面新增恒为 0 的序列**：`PAGERDUTY_ENABLED=true` 时
   `alert_external_notify_failures{action="ack"|"resolve"}` 启动即以 0 注册。
   这不是噪声：`increase()` 取窗口内样本之差，序列若到第一次失败才创建，第一个样本就是 1、
   没有 0 可比 ⇒ 增量算成 0 ⇒ 出厂 critical 规则**漏掉第一次失败**（实测：单次失败
   `increase=0`；两次才有 1.197）。
6. **SLO 状态不再恒 99.5/`met`**（返回真实的 `nodata`/`breached`/`met`）；
   **gpu-svc HPA** 从"引用一个从来没被产出的 Pods 指标"改回 `Resource cpu`；
   **autoscaler-svc** 默认 executor 为 `simulated`（决策记录有、集群副作用无）。
7. **6 个微服务的 gRPC pb 换成 protoc 生成物**：此前 `api/proto/v1` 下是**手写 Go struct**，
   不满足 gRPC 默认 codec 对 `proto.Message` 的要求 ⇒ 线上任何一次调用都会
   `failed to marshal, message is *alertv1.CreateRuleRequest, want proto.Message`，
   而所有测试都是进程内直调 Service，永远碰不到编解码路径——CI 全绿、健康检查全绿、
   **API 100% 不可用**。现在 100 个 unary 方法逐个过真编解码验证。新增字段向后兼容。

### 能力降级清单（本版如实标注；按能力表验收时请跳过这些或先确认前置）

| 能力 | 实际状态 | 前置 / 替代路径 |
|---|---|---|
| SLO/SLI 达成度 | 多数指标**没有生产者**，`/api/v1/slo/status` 通常 `nodata` | 先接指标来源；支持指标清单见 `internal/store/slo_eval.go` |
| 自动扩缩容 | 默认 `simulated`，不真改集群副本 | `AUTOSCALER_K8S_EXECUTOR` + kubeconfig（合并补丁只动 `spec.replicas`） |
| AIOps 智能分析 | 端点可能返回模拟数据（响应带 `source`/`simulated`）；`/ready` 现回报数据源，此前写死 `engines:"5/5"` | 配 `PROMETHEUS_URL`；且**没有宿主代理/前端消费者** |
| alert-svc 按指标自动告警 | 读数由调用方推，服务**不拉 Prometheus** | 要自动触发请走出厂 Prometheus + Alertmanager 链路 |
| 插件 / 应用市场"可插拔扩展" | 只有 Manager 框架，**控制面零钩子触发点**；市场条目无加载器 | TD-62（含四个待决策点） |
| 事故回溯到规则 | incident-svc 的记录**没有 `rule_id` 字段**（alert-svc 侧本版已补） | 目前靠 alert ↔ incident 的时间与设备关联 |
| 链路追踪可查询 | collector 的 traces pipeline 只接 `logging` exporter，**无 Jaeger/Tempo**；`tail_sampling` 对非错误、非慢请求只留 10% | TD-75；现在能做的是"看 span 计数"，不是"打开调用链图" |
| 可空列逐列穷举 | 门禁只覆盖 alert-svc 两个 store；控制面 `internal/store` 仍是抽样核对 | TD-74 |
| PagerDuty 真实 SaaS 送达 | 载荷契约由**自建校验端点**判定，未打过真实端点（无集成密钥） | 报告 §32.7 |
| 微服务 gRPC 面鉴权 | alert-svc 的 gRPC 只有 trace + ratelimit 拦截器，**无鉴权、无租户校验**，默认发布在 `127.0.0.1` | 跨机暴露需前置认证或反代限制；默认口径待产品定 |
| 企业版前端 | 社区授权下 `/enterprise/` 返回"企业版 · 未授权"页，SPA 资产链路只在企业授权下验得到 | 企业授权 + `make frontend` 装配 |
| 第三方许可 | MPL-2.0 / npm 依赖的**法务结论未出**（工程侧清单与 CI 门禁已就绪） | P1-7 |

**这张表的基准（2026-10-06 补）**：它记的是**已发货的 v0.12.0**（tag `v0.12.0` → 提交 `9f79dda1`）那一刻的实际交付状态，
不是 main 的当下状态，两种口径在同一条线上会分叉。逐行按 `file:line` 复核后，**有 2 行在 main 上已经不成立、
但对拿到 0.12.0 镜像的客户仍然成立**：

| 行 | 在 main 上的现状 | 对已发货 0.12.0 是否仍成立 | 证据 |
|---|---|---|---|
| 插件 / 应用市场"可插拔扩展"——控制面零钩子触发点 | **不再成立**：`AllHooks()` 冻结的 3 个扩展点全部有宿主触发点（`platform_config.go:117`、`:155`、`server_netsec.go:103` → `plugin_host.go:74`），并由 `plugin_hook_gate_test.go` 要求"新增扩展点必须同时有触发点与测试" | **仍成立**（接线在 tag 之后） | `git diff --name-only v0.12.0..HEAD -- internal/controlplane/plugin_host.go` 有输出；市场条目无加载器这半句在两侧都成立 |
| 可空列逐列穷举——门禁只覆盖 alert-svc，控制面 `internal/store` 仍是抽样核对 | **不再成立**：改为穷举门禁 `internal/store/nullable_scan_guard_test.go`，另有 15 份 `mysql_scan_test.go`（`internal/{cmdb,deploy,logstore,orchestration}` + 11 个服务 store 包）与 `services/alert-svc/internal/store/nullable_scan_guard_test.go` | **仍成立**（收口在 tag 之后） | `find . -name 'mysql_scan_test.go' \| wc -l` = 15；`internal/store/nullable_scan_guard_test.go:1-20` 自述"把抽样换成穷举" |

其余 12 行本轮逐条重测**仍然成立**，判据是"该能力对应的资产在 tag 之后没被动过，或动了但结论未变"：
`otel-config.yaml`（traces 仍只接 `logging`）与 `internal/store/slo_eval.go` 在 `v0.12.0..HEAD` 区间**零改动文件**；
alert-svc 里 `grep -rl prometheus services/alert-svc/internal` 无结果（仍不自行取数）；incident-svc 无 `rule_id`；
`grpc.ChainUnaryInterceptor` 的实参仍只有 `trace` + `ratelimit`（`services/alert-svc/cmd/alert-svc/main.go:97`）；
`values.yaml:827` 与 `docker-compose.prod.yml:1038` 的 `AUTOSCALER_K8S_EXECUTOR` 默认值仍为 `simulated`。
AIOps 那行的"`/ready` 现回报数据源"经核对**在 tag 上就已成立**（`services/aio-svc/cmd/aio-svc/main.go:75,87` 在
`v0.12.0` 里已是自检版本），所以它不属于上面的分叉行。

> 记账一处口径更正：本报告 §33 之前的口头结论说这张表"有 3 行在 main 上不再成立"，逐行取证后是 **2 行**——
> 第三行（AIOps `/ready`）我当时按"TD-76 的 `c99d0d6` 在 tag 之后"推断，没有实际比对 tag 上的源码，推断错了。

### 本版可复核的验证证据（都带观测量，不是静态结论）

- 出厂栈真机 `verify-runtime.sh`：`PASS=123 / FAIL=1`（唯一 FAIL 是本机跑着本地覆盖层镜像
  `0.11.0-nullfix` 与 `.env` 版本不一致，属真陈述的本地态）。改前是"连跑四次、
  每次报的缺失序列各不相同"——根因是门禁自己的 `producer | grep -q` 在 `pipefail` 下
  被 SIGPIPE 打成假阴性，已全量改为 herestring 并加门禁（见 §32.8）。
- `validate-deploy-assets.sh`：`PASS=45 / FAIL=0 / SKIP=1`。
- alert-svc 外发腿活体：`trigger`/`acknowledge`/`resolve` 三段都被按公开契约校验的端点收下（202），
  `dedup_key` 三段同值、`payload.source` 为真实设备名、ack/resolve 无 `severity` 键；
  失败腿（注入 503）本地状态照常落库、counter 0→1、WARN 带错误文本，
  出厂 critical 规则 `OpsMeshAlertExternalNotifyFailed` 真实 firing 并被 Alertmanager 收到。
- gRPC 契约：6 个服务 100 个 unary 方法过真 codec；pb 生成物版本漂移门禁不依赖 protoc。
- CI：本版本各批次在 main 上均为 12 job 全绿（含 `-race`、真实 MySQL 集成、E2E real/security、
  镜像构建+keyless 签名+SBOM），`release` 按设计仅在 tag 上运行。
- 发布后验收（2026-10-04，`deploy/scripts/verify-release-artifacts.sh 0.12.0`）：**PASS=6 / FAIL=0**
  —— 14 个镜像仓库都有 `:0.12.0` 且 `.sig`/`.att` 齐备、GitHub Release 有 5 个 assets（含本节正文）、
  chart 默认渲染出的 4 个镜像引用全部真实存在。另用**已发布镜像**做过存量库升级演练：
  同一测量在 `0.11.0` 上报 `want proto.Message`，在 `0.12.0` 上 `alerts` 表 14→15 列、
  两条 0.11.0 时代的老行（含全 NULL 可空列那条）全部读得回、重启不重复 ALTER；
  12 个官方镜像逐个起并各自命中健康端点。全文见报告 §33。

## v0.11.0 — 2026-10-01 可观测性语义收口 + 镜像级 SBOM 证据链 + 三域转正 + 许可合规工程化

本版主线不是加功能，而是把**已经在线但看不见**的东西变成事实：微服务指标的类型与基数、
只读日志后端的静默丢弃、无界的调度历史、被吞掉的外部通知失败、以及镜像侧的成分证据链。

### 破坏性变更（升级前必读）

1. **微服务指标家族与类型变更**（`pkg/metrics`，12 个服务共用）：
   - 9 个 `business_metrics{name="*_total" | *_failures}` **gauge 序列**迁为 `business_metrics_total{name="*"}` **counter**。
     原实现是 SET 语义、每事件写 1 ⇒ 恒为 1 的 gauge，`rate()/increase()` 与失败率计算全部无意义。
   - **删除 `queue_depth` 序列**（全仓零生产调用方，每次抓取恒输出 0）。
   - HTTP 与直方图的 `path` 标签值一律归一化（`/api/v1/devices/123` → `/api/v1/devices/:id`）；非标准方法
     收敛为 `:other`；超 2000 条时序后折叠，丢弃数暴露在 `http_metrics_series_dropped_total`。
   - 已核实**出厂告警规则与面板均未引用**上述名字 ⇒ 不断既有查询；自建查询的部署需按上表改写。
2. **ES/Loki 日志后端不再假装写入**：`POST /api/v1/logs` 由 `200/201` 改 **`501 Not Implemented`**，
   `log-svc` 的 `AppendLog` 改 gRPC `codes.Unimplemented`。此前 `Append` 是 `return nil` 的 no-op，
   agent 日志与任务输出被逐条丢弃而调用方收到成功。gRPC 侧改为**整轮跳过 + 仅一条 WARN**。
   需要 OpsMesh 写日志请把 `log-backend` 配成 `memory` 或 `sql`。
3. **cron 双实现语义对齐**（`pkg/cron` ← `internal/cron`）：`0 3 * * 7`（周字段 7）此前在 task-svc 侧
   **静默不执行**、在控制面正常执行 ⇒ 升级后这类表达式**会开始执行**；`60 * * * *` 这类单值越界由
   「静默不匹配」改为**报错**。新增 `internal/cron/parity_test.go`，两轨一致性由注释承诺变为 CI 强制。
4. **`deploy/docker/scripts/init-mysql.sql` 不再携带建表语句**（删 43 张表 DDL，只建库+授权）：该脚本自称
   只建库却带着与代码漂移的 DDL（6 张表 / 13 列零兜底），一旦被挂载，控制面 `INSERT INTO agents` 即
   `Unknown column 'agent_id'`。建表职责归代码迁移与服务自建 schema。
5. **`operator` 角色新增 gpu / runbook / incident / k8s 四组只读权限**：修正此前「operator 权限反而低于
   viewer」的层级倒挂。只补 read 不补 write（write 下放属产品未决）。
6. **autoscaler 决策历史上限 500 条**（此前无界增长且每次评估反向扫全量）：绕回后**保留最新**——
   冷却判定依赖尾部语义，裁剪方向是正确性的一部分。

### 新增能力

- **镜像级 SBOM 证据链（首个带 `.att` 的版本）**：12 个微服务 + 2 个核心镜像执行
  `cosign attest --type cyclonedx`，`cosign verify-attestation <image>` 可直接从镜像问到成分，
  不必登录 Actions 下载附件。v0.10.0 时 `.att` 为 0/14。
- **第三方许可合规工程化**：`deploy/scripts/gen-third-party-licenses.sh`（离线，gen / `--check` /
  `--emit-notice` 三模式）+ `docs/third-party-licenses.md`（162 模块、UNKNOWN 0、24 项 MPL-2.0 列入
  「需法务确认」）+ 仓库根 `NOTICE`（7 段上游署名逐字保留，满足 Apache-2.0 §4(d)）。CI `security`
  job 接入 `--check` 门禁（清单集合相等 / 双读判定一致 / 源码覆盖率下限）。
  **MPL-2.0 能否随商用分发属商务 + 法务判定，脚本不替它签字。**
- **三域转正落地**：incident / runbook / autoscaler 进 compose 生产栈（三段服务定义 + 三条 `*_SVC_URL`），
  `init-databases.sql` 补 `opsmesh_runbook` / `opsmesh_incident`；runbook 落地 MySQL 持久化、编辑器 Save
  由 `alert()` 假动作改真实 PUT；incident 的 MTTD 补上「发生时刻」来源 `occurred_at`。
- **业务指标接线第 1 步**：`task_reclaimed` / `task_scheduled_fired`（+ 各自失败数）、`log_memory_dropped`、
  `autoscaler_decision_history_entries`。另有 5 条候选经核查**不做**——要么只能全表扫描换数字，
  要么数据源是占位实现（在其上出指标等于编造数字）。
- 前端侧栏补第六个入口 `bot`（此前 `/bot` 只能手敲 URL 到达）；gpu 利用率图修正字段读取
  （`avg_utilization`）并按 `source` 显示徽标——**只有 `nvidia-smi` 标「真实采集」**，
  缺省/未知/simulated 一律告警呈现，不把未知当可信。

### 缺陷修复

- `incident-svc` 在「服务自建表」部署（K8s / 自备 MySQL）里**每次 `CreateIncident` 都失败**：
  `occurred_at` 只加进了引导脚本而没进服务自己的 `CREATE TABLE`。修法：DDL 补列 +
  `ensureIncidentColumns`（`information_schema` 预检 + 幂等 `ALTER`）+ `schema_drift_test.go` 防复发。
- `alert-svc` 的 ack/resolve 外部通知失败此前被 4 处 `_ =` 吞掉（本地已改、请求返回 OK，而 PagerDuty
  侧没收到）⇒ 现在 WARN + `alert_external_notify_failures{action}` 计数，且刻意不向上抛错
  （远端故障不应阻塞本地告警流转）。
- `aio-svc` 噪声压缩用例的分钟桶边界偶发失败；errcheck 收紧档 150 处逐点勘验（131 修 + 19 有据豁免）。

### 供应链与门禁

- **axios `1.19.0 → 1.20.0`**：Trivy 刷新漏洞库后暴露 7 条 HIGH（全在 axios、均有修复版本），与本次代码
  改动无关，属存量依赖问题 ⇒ 选择升级而非 `.trivyignore` 豁免。lockfile 的 `resolved` 已用官方 registry
  纠正，**不把第三方镜像固化进交付物**。
- `validate-deploy-assets.sh` 新增**第 12 节**（业务指标禁止实体 ID 当标签、counter 命名不重复 `_total`）与
  **第 13 节**（引导脚本不得含 `CREATE TABLE`；compose 每个 `*_SVC_DSN` 指向的库必须有建库来源），
  两节均经故障/变异注入验证。

### 能力降级清单（2026-10-06 复核追加；本节原正文未改动）

上面这一节当时**没有**写降级清单（`release.yml:266` 的"发布正文必须含能力降级清单"是 v0.12.0 才成为硬断言的，
v0.11.0 发布时该断言还不存在，所以 Release 页上客户看不到降级说明）。本表按**v0.11.0 那个提交时刻的装配**逐条取证，
每行都给出可核对的证据；不照抄 v0.12.0 的清单，因为两条线之间修掉的与仍没修掉的不是一回事。

| 能力 | 在 v0.11.0 当时的实际状态 | 前置 / 替代路径 | 证据 |
|---|---|---|---|
| 日志检索（SQL 后端） | **现行缺陷**：`log_entries.device_id/agent_id/task_id` 由写入侧 `nullStr()` **主动写成 NULL**（agent / system 来源的日志本来就没有 task 关联），而读侧用裸 `string` 承接 ⇒ 任一列为 NULL 就 `converting NULL to string is unsupported`，`Query` 向上抛错 ⇒ **整条日志检索失败**，不是少一条。默认装配不触发（compose `LOG_BACKEND:-loki`、helm `LOG_SVC_BACKEND: "memory"`），但 `sql` 是 `values.yaml:550` 明写的可选后端，一开就中 | 先用 loki / es / memory；要用 sql 需含本次修复的版本 | `git show v0.11.0:internal/logstore/sql.go` 实测 nullStr 2 处、`sql.Null*` 0 处；回归 `TestSQLQueryReadsNullColumns` 复现出该驱动错误文本 |
| 告警列表读取（控制面 + alert-svc） | **现行缺陷**：写入成功，但可空列为 NULL 时整行读不出来，列表"凭空变空" | 无——这是缺陷不是配置问题 | 真机实测 `Alerts 扫描失败` 1844 次（2026-10-02，记于 TD-74）；alert-svc 侧修复 `d98cd39` 在 tag **之后**；控制面 `sql.Null*` 收口在 `b0156ed`（2026-10-05） |
| 链路追踪可查询 | 只有"发得出去"，没有"查得到"：collector 的 traces pipeline 仅接 `logging` exporter，无 Jaeger/Tempo；`tail_sampling` 对非错误非慢请求只留 10% | 自建 Jaeger/Tempo 后改 pipeline | TD-75（至今仍 open，未修） |
| 插件 / 应用市场"可插拔扩展" | 只有 Manager 框架，控制面**零钩子触发点**；市场条目无加载器 | 等 TD-62 宿主接线 | 接线提交 `e8ffa68` 在 tag **之后** |
| CMDB 检索 | 7 列 `LIKE '%x%'` 全表扫描 + 按 `updated_at DESC` 截断 1000 条候选 ⇒ 命中多时最相关 CI 可能落在窗口外 | 无（当时无全文索引） | ngram 迁移 020/021 均在 tag **之后**（`e739da2` 起） |
| auth-svc 对外 HTTP 面 | 出厂 compose **刻意不设 `AUTH_SVC_URL`**，helm 亦默认关闭 ⇒ 登录态相关能力在默认装配下不可用 | 需显式开启并配 storeType=sql | `git show v0.11.0:deploy/docker/docker-compose.prod.yml` 第 427 行的注释即当时自述 |
| K8s（helm）路径的微服务 | 默认**不部署**：`enabled: false` 18 处 vs `true` 5 处 ⇒ 只装控制面是预期行为，不是缺漏 | 逐个 `--set services.*_svc.enabled=true` | `git show v0.11.0:.../values.yaml` 计数（实测 18/5） |
| 自动扩缩容真正生效 | 默认 `simulated`，不会真改集群副本数 | `AUTOSCALER_K8S_EXECUTOR` + kubeconfig（合并补丁只动 `spec.replicas`） | 同 v0.12.0 清单该条，v0.11.0 未变 |
| AIOps 智能分析 | 依赖 `PROMETHEUS_URL`；且**无宿主代理、无前端消费者**；另有不实自述：`/ready` 把数据源写死成 `engines:"5/5"` 而非自检 | 配 Prometheus 后仍需自建前端入口 | `/ready` 数据源自检的修复在 `c99d0d6`（tag **之后**）——所以 v0.11.0 的健康接口在这个字段上**不可信** |
| alert-svc 按指标自动告警 | 读数由调用方推入，服务自身**不拉 Prometheus** | 走出厂 Prometheus + Alertmanager 链路 | 同 v0.12.0 清单该条 |
| SLO/SLI 达成度 | 多数指标**没有生产者**，`/api/v1/slo/status` 通常 `nodata` | 先接指标来源；支持清单见 `internal/store/slo_eval.go` | 同 v0.12.0 清单该条 |
| 事故回溯到告警规则 | `incident` 与 `alert` **两侧都没有** `rule_id`（alert-svc 侧是 v0.12.0 才补的） | 靠时间与设备关联近似回溯 | v0.12.0 清单自述"本版已补"⇒ 反推 0.11.0 两侧皆无 |
| PagerDuty 真实 SaaS 送达 | 载荷契约由自建校验端点判定，未打过真实端点（无集成密钥） | 提供密钥后补真集成 | 报告 §32.7（该状态延续到 v0.12.0） |
| 微服务 gRPC 面鉴权 | alert-svc 的 gRPC 只有 trace + ratelimit 拦截器，**无鉴权、无租户校验**，默认发布在 `127.0.0.1` | 跨机暴露需前置认证或反代限制 | 同 v0.12.0 清单该条 |
| 企业版前端 / 第三方许可 | 社区授权下 `/enterprise/` 只给"未授权"页；24 项 MPL-2.0 依赖的**法务结论未出**（工程侧清单与门禁已就绪） | 企业授权 + `make frontend`；P1-7 待法务 | 同 v0.12.0 清单该条 |

**怎么读这张表**：它不是在说 v0.11.0 不好，而是把"代码里有"与"默认装配下能用"分开记账。这仓库已经栽过两次同形状的跟头
（v0.9.1 有 tag 无产物、v0.12.0 归版时把镜像 pin 指到从未发布的 0.12.0），两次都是**清单声明领先于交付物**。
按能力表验收 v0.11.0 时，请先把上表逐条跳过或确认前置，否则会撞上未声明的缺口并把它们当成新缺陷报回来。

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
- **task-svc 选主租约：释放后同毫秒不可接管**（边界缺陷，非回归——同一代码在上一个绿 run 通过，真 MySQL 后端撞上毫秒边界才现形）。`Release` 写 `lease_until = NOW(3)`，而 `Acquire` 的接管条件只判 `lease_until < NOW(3)`，落在同一毫秒时接管落空，「优雅退出后其余副本不必等自然过期即可接管」的语义在释放那一刻不成立。接管条件补 `holder_identity = ''`（无持有者即空闲），并发单赢家语义不变；新增不等 sleep 的 20 轮回归用例把偶发固化为必然判据。**影响**：`TASK_SVC_LEADER_MODE=mysql` 且 `replicas>1` 时，接管可能延迟到一个续租周期之后（生产上是可用性毛刺，不是双主风险）。
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
- **发布链实测（tag `v0.10.0` = commit `c517df5`）**：
  - `release.yml` run `36550147538` **success**——12 个微服务 `build-and-push` 矩阵 + `github-release` 全通过（矩阵 17→12 之后的第一次真发版）。
  - GitHub Release `v0.10.0` 已发布（非 draft），assets **5 个**：`checksums.txt` + `opsmesh-0.10.0-linux-{amd64,arm64}.tar.gz` 及各自 `.sbom.json`。
  - GHCR 逐镜像实测（取 `manifests/0.10.0` 的 `docker-content-digest`，再请求 `<digest>.sig`）：**14/14 镜像 tag 可解析且 cosign 签名在位**（12 微服务 + `opsmesh-binary` + `opsmesh-agent`），无缺失。
  - **一处缺口如实登记**：`<digest>.att`（镜像级 SBOM 证明链）**14/14 全 404**——该链由 tag 之后的 `b092df5` 才引入，v0.10.0 产物不可能含它；Release 页那 2 份 `.sbom.json` 只覆盖核心二进制的 tarball，不覆盖任何镜像。镜像级 SBOM 需下一版 tag 才成为交付物。
- 同 head 的 `ci` run `36545861598`：**12 job success**（`release` skipped 属非 tag 触发预期）。其中 `services` job 在真 MySQL 后端上实跑了 task-svc 选主租约三条用例 ⇒ §5.8 之后那处「释放后同毫秒不可接管」的修复属**已复验**（本机无 Docker/无 `OPSMESH_TEST_MYSQL_DSN`，这些用例在本地只能 SKIP）。

### 已知问题（本版未覆盖，已在册）

- 五域最终裁决：`gpu` / `portal` / `autoscaler` 仍**待真实流量观察期**（取数出口已具备，见新增能力与 §5.8 口径）；`incident-svc` / `runbook-svc` 经静态取证改判为**「已接线但部署不可达、流量恒 0 是结构性的」**，不必等观察期，需按 §5.9 的三条路（转正 / 冻结 / 删除）择一。`auth-svc` 有据暂缓接通（部署侧 `AUTH_SVC_HTTP_ENABLED` 全量未设置，helm `auth_svc.enabled=false` + `storeType: memory`）。
- **三域能力缺陷（§5.9 登记，本版未修）**：gpu 指标来自 `math/rand` 且无样本时回退到模拟值（面板利用率非观测值）+ 节点表 4/6 列无数据源；incident 复盘读不到 `content`、MTTD 恒 0、时间轴 `evt.content` vs `description`；runbook 只有内存 store（重启全丢）+ 编辑器 Save 是 `alert()` 假动作。另：`operator` 角色因权限白名单不含这三域，**连页面都进不去（403）**。
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