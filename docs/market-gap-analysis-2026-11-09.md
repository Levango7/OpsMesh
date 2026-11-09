# 招标要求 → OpsMesh 现状 全景对照表

> 性质：**只读取证 + 对照**，本表不改代码、不承诺排期。
> 用途：**内部方向对齐材料，不对外发布**（对外版本需删去「话术边界」与内部判断）。
> 目的：把「市场的准入门槛」与「我们的工程投入」放在同一张表上，避免几条 agent 线各拉各的方向。
> 取证基线：`origin/main`（2026-11-09，批次 10 合并后 `65e1667`）；代码侧结论一律给 `file:line` 或可复跑命令。
> 出处约定：`[招]` = 招标文件/采购公告/选型指南的公开条款；`[标]` = 等保/密评/适配认证的公开规范与测评口径。

---

## 0. 结论先行（先看这五条）

1. **我们最大的风险不是「能力少」，是「能力与标书错位」。** 21 条要求里，**5 条是准入阻断级（不补就投不了/过不了）**：国产数据库、国密、离线安装包、多架构镜像与 ARM64 纳管、规模化压测证据。这 5 条**全部是「真缺能力」**，没有一条能靠写文档蒙过去。
2. **当前主力投入（shell 注入面收口）属于「等保 B5 漏洞闭环」的支撑项，不是投标准入项。** 它有价值（安全评审会查），但它**不能把任何一条 ❌ 变成 ✅**。这是「几个人力气不在一个方向」的具体表现。
3. **有一批「已经做到但文档没写对」的加分项，被我们自己低估了**：审计哈希链 + 180 天自动归档（比 README 写的强）、RBAC 116 权限点/52 资源族（README 写 24 条）、供应链四门禁、纯 Go 不依赖 Java 中间件、依赖树 0 GPL/AGPL。**这些改文档就能变成投标弹药，成本最低、见效最快。**
4. **另有一批「文档按完成态写、代码查无实据」的高危项**：国密、多 DB、国产中间件运维、容量指标、ARM64 tag 命名。**投标现场一质询就穿帮**，比「老实说没有」更伤。这类必须**先降级文档口径**，再谈补能力。
5. **「先做到能投标」的最小闭环 = 5 个 P0 工程项 + 1 次文档真实性纠偏。** 不是把 21 条全做完，而是把「一票否决项」补到**可演示、可举证、可复跑**，其余用真实边界应答。

---

## 1. 全景总表（21 条）

| 编号 | 要求 | 现状 | 一句话证据 | 行动 |
|---|---|---|---|---|
| A1 | 国产 CPU 适配 + 互认证明 | ❌ | 发布仅 `linux/amd64, linux/arm64`（`.goreleaser.yml:27-31`）；loongarch64/mips64 零命中；无任何互认证书 | **P0** |
| A2 | 国产 OS（麒麟 V10/UOS/openEuler） | ⚠️ | agent 仅读 `/etc/os-release`（`internal/agent/agent.go:287-293`）；平台自身无发行版分支、无真机验证 | **P0** |
| A3 | 国产数据库（达梦/金仓/openGauss/OceanBase） | ❌ | 16 个模块只有 `go-sql-driver/mysql`（`go.mod:8`）；迁移是 MySQL 专有语法（`migrations/001_initial.sql:76`） | **P0** |
| A4 | 国产中间件兼容 | ✅/❌ | 平台自身纯 Go、不依赖 Java 中间件（**加分**）；但国产中间件 deploy 是 `echo` 桩（`domestic_ops.go:303/305/365-369`） | P1（拆分应答） |
| B1 | 双因素认证（2FA/MFA） | ❌ | 仅 `needMFA` 标志且仍下发 token（`services/auth-svc/internal/http/gateway.go:218`）；无 TOTP/短信/邮箱码 | **P0** |
| B2 | 审计留存 ≥6 月、防篡改、可检索 | ✅ | 哈希链 + 三重校验（`sql_audit_chain.go:44-101`）+ 归档边界哈希（`:540-569`）+ 默认 180 天（`config.go:533`） | P2（改 README） |
| B3 | 通信传输保密性（HTTP 也要加密） | ✅ | `--http-tls auto\|on\|off`（`config.go:501`）+ 生产无 TLS 拒绝启动（`:1162`）+ 联邦 mTLS（`tlsutil.go`） | P2（改 README） |
| B4 | 运维操作审计 / 堡垒机等价 | ❌ | 无终端录像/PTY/命令回放；只有任务 stdout + 审计事件（`README.md:241` 自认「有历史无回放」） | P1 |
| B5 | 渗透测试与漏洞闭环 | ⚠️ | CI 有 gosec + govulncheck + Trivy（`ci.yml:520/650`）；无第三方渗透报告 | P1 |
| B6 | 身份鉴别（口令策略）+ RBAC | ⚠️ | 复杂度/黑名单/锁定 ✅（`password.go:31-66`、`loginguard/guard.go:24-30`）；**口令有效期 ❌**；RBAC 116 点/52 族 ✅（`model/perm.go:25`） | P1 |
| C1 | 国密 SM2/SM3/SM4 + 国密 TLS | ❌ | 代码侧国密关键词 **0 命中**；实测算法仅 AES-GCM / bcrypt / SHA-256 / HMAC / Ed25519 | **P0** |
| D1 | 全离线安装包（airgap） | ❌ | 无 `docker save` / `helm package` / 依赖预拉取脚本；`docker-pull-retry.sh` 只是 429 重试 | **P0** |
| D2 | 自主研发 + 自主知识产权 | ✅ | 自研 gRPC 通道；go.mod/go.sum 无蓝鲸/GSE 依赖；Go/npm 树 0 GPL/AGPL（`docs/third-party-licenses.md`） | P2（可直接主张） |
| D3 | 交付材料包（手册/清单/报告） | ⚠️ | DELIVERY/部署文档有；缺定价、EULA、交付 SOP、案例；部分文档与代码漂移 | P1 |
| E1 | 设备自动纳管（发现→推送→注册） | ⚠️ | 有 discovery/bootstrap；但 **ARM64 设备拿到的是控制面自身 amd64 二进制**（`server_bootstrap.go:90`） | **P0**（并入 A1） |
| E2 | 批量作业（脚本/服务/文件分发） | ✅ | 失败重试/死信/取消/周期全链路；E2E 覆盖（本批 CI 的 E2E real backend 绿） | P2（差异化主打） |
| E3 | 巡检/监控告警/事件联动自愈 | ? 未核 | rule engine 存在，但「告警→动作」闭环未逐条取证 | 待核 |
| E4 | CMDB / 资产台账 | ✅ | `internal/cmdb` + 部署/配置库图谱；覆盖度未逐项核 | 待核（低优先） |
| E5 | 多租户 / 分级分权 | ✅ | schema 级物理隔离（`multischema/multi_schema.go:1-21`）+ 连接池上限（`sqlstore/sql.go:130-136`）+ RBAC | P2 |
| F1 | 万级~十万级设备纳管（容量证据） | ❌ | 仓库内**无任何压测结果**；`device-sim` 打的端点不存在（`cmd/device-sim/main.go:4-12` 自认假成功）；2s 轮询 ≈ 1000 agent / 500 req/s 未实测 | **P0** |
| F2 | 控制面 HA + 灾备 | ⚠️ | 无状态可扩 + 审计链 `FOR UPDATE` 串行化；无 HA 实测/灾备演练 | P1 |

**计数：✅ 6 条 / ⚠️ 6 条 / ❌ 8 条 / ? 1 条。P0 准入阻断 6 条（A1+A3+B1+C1+D1+F1，E1 并入 A1）。**

---

## 2. 分域详表

### A. 信创全栈适配（国产替代类项目的**准入门槛**，不是加分项）

#### A1 国产 CPU 适配 + 互认证明 — ❌ 不具备

- **典型条款**：「所投产品应满足信创造配要求，包括：服务器、操作系统、数据库、终端设备等」；部分标书直接要求「提供与具体芯片型号对应的**兼容性互认证书**」。
- **我方可验收判据**：鲲鹏 920 / 飞腾 FT2000+ / 海光 7000 / 兆芯 KX-7000 / 龙芯 3A5000 至少各一份互认证书或 CMA/CNAS 适配报告。
- **现状证据**：
  - `.goreleaser.yml:27-31` → `goos: [linux]`、`goarch: [amd64, arm64]`；**无 loongarch64 / mips64**。
  - CI 无 `GOARCH` 矩阵（`ci.yml` 唯一命中是注释行 `:820`）。
  - Docker 构建 **无 `--platform`**（`_publish-image.yml:167-179`、`release.yml:140-146`）⇒ 镜像单架构。
  - 全仓无任何互认证书/适配报告。
- **差距 / 行动**：**P0**。① 镜像改多架构 manifest（`linux/amd64,linux/arm64`）；② agent 改为按 `uname -m` 分发对应架构产物；③ 龙芯/飞腾适配单独立项（工作量大，先做「可应答的范围声明」）。
- **⚠️ 坑点**：行业口径明确「支持国产芯片」≠「与华为鲲鹏实现技术互认」——互认意味着双方实验室做过联合验证，且招标文件会点名要证书编号。

#### A2 国产 OS 适配 — ⚠️ 部分具备（不足以应答）

- **典型条款**：适配范围含操作系统；选型指南把「兼容麒麟 V10、欧拉 OS、UOS」列为必选。
- **我方可验收判据**：在目标 OS 上完成**安装 + 纳管 + 任务下发 + 回执**四段闭环的验证记录（非仅编译通过）。
- **现状证据**：
  - agent 侧只通用采集 `/etc/os-release`（`internal/agent/agent.go:287-293`），**无发行版分支**。
  - 国产 OS/CPU 支持是**下发给被管设备的 shell 探测**（`domestic_ops.go:1-5,34`），不是平台自身的 OS 适配层。
  - 安装脚本无 arch/发行版探测；`pkg/provision/install.go:10` 注释称「探测 OS/arch」，模板实际没有（`:27-76`）。
  - bootstrap 生成的 unit `User=root`（`install.go:61`），与 systemd 加固版 `User=opsmesh`（`deploy/systemd/opsmesh-agent.service:13`）双轨不一致。
- **差距 / 行动**：**P0**（与 A1 合并）。先做「一个国产 OS 真机闭环」拿到证据，再谈互认；同时修 bootstrap 的 arch 探测 + 用户/路径双轨。

#### A3 国产数据库适配 — ❌ 不具备

- **典型条款**：适配范围含数据库；选型指南列「数据库适配达梦、神通、openGauss」。
- **我方可验收判据**：指定国产 DB 上建表/迁移、事务写入、审计哈希链、归档任务全部可用（不是只连得上）。
- **现状证据**：
  - `go.mod:8` 仅 `github.com/go-sql-driver/mysql`；16 个模块同上；`go.sum` 中 pgx/dm8/kingbase/opengauss **0 命中**。
  - `internal/store/sqlstore/sql.go:126` 硬编码 `sql.Open("mysql", ...)`；`--store` 只有 `memory|mysql`（`config.go:490`）。
  - 迁移目录单目录 001~030，**MySQL 专有语法**：`AUTO_INCREMENT`（`001_initial.sql:76`）、`MEDIUMTEXT`（`:52`）、`ON DUPLICATE KEY UPDATE`（`sql.go:1006`）、`information_schema`/`DATABASE()`（`sql.go:758-760`）。
  - 无方言抽象层（`Dialect/Rebind/Placeholders` 0 命中）。
  - **文档自相矛盾（投标高危）**：同一份 `docs/deployment-scenarios.md` 在 `:1677-1681` 把「鲲鹏+openEuler+GaussDB」「鲲鹏+UOS+达梦」「飞腾+麒麟+金仓」「海光+麒麟+达梦」四组标成 **✅ 已验证**，而 `:1726` 又自注「国密与多 DB 驱动未实现，勿配」。代码侧既无达梦/金仓驱动，也无 ARM64 镜像 ⇒ 这张表**不能对外**。
- **差距 / 行动**：**P0，工作量最大**。建议路径：先抽方言层（最小接口：占位符、UPSERT、分页、DDL 差异）→ 先支持 PostgreSQL 协议（openGauss/GaussDB 可复用）→ 再做达梦/金仓专用驱动。**不要承诺「全栈国产 DB」再动手**。
- **⚠️ 坑点**：GaussDB 可经 MySQL 兼容协议旁路接入，但仓库自认「SQL 方言未审计」（`docs/deployment-scenarios.md:1725-1727`）——这条路能演示，不能当适配证明。

#### A4 国产中间件兼容 — ✅（平台自身）+ ❌（运维能力）

- **典型条款**：全栈适配含中间件（东方通 TongWeb / 中创 InforSuite / 宝兰德 BES）。
- **现状证据**：
  - **优势**：控制面全部 Go 二进制，不依赖 Tomcat/Java（`docs/deployment-scenarios.md:1738-1740` 确认；`Dockerfile.service:74` 等）。
  - **缺口**：国产中间件 deploy 是 `echo` 桩（`domestic_ops.go:303/305/365-369`）；status/start/stop 是调远端脚本。
- **差距 / 行动**：**P1**。应答时拆两句话说：①「平台自身无需 Java 中间件，天然不受中间件替换影响」；②「对被管业务系统的国产中间件，提供纳管与生命周期脚本框架，具体版本适配按项目交付」。**不要把 echo 桩说成「支持部署」。**

### B. 等保 2.0 三级（多数政企项目的**必测项**，一票否决风险）

#### B1 双因素认证 — ❌ 不具备

- **典型条款**：「未配置双因素认证，在等保三级测评中被一票否决」；2023 年测评数据 90% 不合规项集中在**双因素、日志留存、渗透测试**。
- **现状证据**：
  - `services/auth-svc/internal/http/devicefp.go:136-146` 返回 `needMFA=true`；`gateway.go:218` 明写「needMFA=true 时**仍下发 token**（用户已通过密码验证）」。
  - 全仓无 TOTP/短信/邮箱验证码实现；前端 `mfa|2fa|totp` 0 命中。
  - `internal/authctx/authctx.go:5` 把 MFA 外置给前置网关（APISIX/蓝鲸 IAM）——**这是「可豁免」的技术理由，但标书通常不接受**。
- **差距 / 行动**：**P0**。最小可投实现：TOTP（RFC 6238）+ 恢复码 + 管理员强制开启开关 + 登录审计。工作量约 1~2 周，是**性价比最高的准入项**。

#### B2 审计留存 ≥6 月 — ✅ 已实现（且强于文档）

- **现状证据**：
  - 哈希链：`sql_audit_chain.go:44-66`（sha256(prev_hash‖长度前缀规范化字段)）；`:84-101` 三重校验；`:151` 事务内 `FOR UPDATE` 串行化防分叉；`:540-569` 归档边界哈希。
  - 留存：`config.go:533` 默认 180 天；`server_tasks.go:402-406` leader 周期归档。
  - 诚实边界：`sql_audit_chain.go:12-15` 自认「持 DB 凭证者可整链重算」⇒ 无外部 WORM/签名锚点。
- **差距 / 行动**：**P2 文档**。README `:237` 写「≥6 月导出」低估了实际能力，应改为「180 天自动归档 + 哈希链 + 归档边界哈希」。若客户要求 WORM，再单独立项（外部时间戳/对象锁）。

#### B3 通信传输保密性 — ✅ 已实现

- **现状证据**：`config.go:501` `--http-tls auto|on|off`；`:1162` 生产模式无 TLS 拒绝启动；`tlsutil.go` 提供 gRPC/mTLS/联邦凭证；`internal/config/http_tls_test.go:3-4` 记录了「原本只作用于 gRPC，Web/REST 始终明文」的修复。
- **差距 / 行动**：**P2 文档**。README 等保对照表 `:1047` 已写对，但 `commercial-readiness-review` 的旧结论（「HTTP 全程明文」）仍在流传，需订正。

#### B4 运维操作审计 / 堡垒机等价 — ❌ 不具备

- **典型条款**：通过堡垒机对运维人员操作详细记录和审计；等保三级安全审计覆盖**运维操作行为**。
- **现状证据**：无终端录像/PTY 劫持/命令回放；现有的是任务 stdout/stderr（`internal/store/model/task_record.go`）+ 审计事件；`README.md:241` 自认「有历史无回放」。
- **差距 / 行动**：**P1**。若标书硬性要求「堡垒机」，两条路：① 集成成熟堡垒机（JumpServer 等）并做审计对接；② 自研命令级留痕（记录每条下发命令 + 回执 + 操作者，不做终端录像）。**先做②，成本低且能覆盖 80% 条款措辞。**

#### B5 渗透测试与漏洞闭环 — ⚠️ 部分具备

- **现状证据**：CI 有 gosec（`ci.yml` security job）、govulncheck `:520-523`、Trivy `:650-672`；**无第三方渗透报告**。
- **差距 / 行动**：**P1**。投标前做一次第三方渗透测试并归档报告；CI 侧已有门禁，可作为「持续漏洞闭环」证据。当前注入面收口批次正是这条的支撑材料，**但它是加分项，不是准入项**。

#### B6 身份鉴别 + 访问控制 — ⚠️ 部分具备（缺口令有效期）

- **现状证据**：
  - ✅ 口令复杂度：`services/auth-svc/internal/http/password.go:31-40`（默认 12 位 + 四类必含）；黑名单 `:44-66`。
  - ✅ 锁定：`loginguard/guard.go:24-30`（5 次失败锁 15 分钟 + IP 令牌桶）。
  - ❌ 口令有效期：全仓无「过期天数/定期改密」；`model/contract.go:227` 只有一次性 `must_change_password`。
  - ✅ RBAC：`model/perm.go:25` 起 **116 权限点 / 52 资源族**；`RolePermissions()` 派生 admin/viewer/operator，`operatorGroups` 显式排除 k8s 写/删与 `diagnostics:dump`。
- **差距 / 行动**：**P1**。补「口令有效期 + 到期强制改密」即可闭环；顺手把 README 的「24 条默认权限」改成 116 条/52 族。

### C. 密码应用（密评，与等保是**两套体系**，不能互相替代）

#### C1 国密算法 — ❌ 不具备

- **典型条款**：政务/金融项目普遍把「数据库原生支持国密算法」作为选型加分甚至门槛；涉密项目要求密码合规性说明。
- **现状证据**：
  - 代码侧 `SM2|SM3|SM4|gmsm|tjfec|gmssl|国密|商用密码|密码机|GM/T` **0 命中**（仅 go.sum base64 误匹配 + docs 文字）。
  - 实测算法：AES-256-GCM（`pkg/secretcrypto/secretcrypto.go:133-141`）、bcrypt（`storekit/crypto.go:46`）、HMAC-SHA256（`authctx.go:330`）、SHA-256（`sql_audit_chain.go:59`）、Ed25519（`internal/config/license.go`）。
  - TLS 用 Go 标准库（`tlsutil.go:44` `tls.LoadX509KeyPair`），无 GM/T 0024 套件、无 `gm` 构建标签。
  - 文档已自认「国密与多 DB 驱动未实现，勿配」（`docs/deployment-scenarios.md:1726`）。
- **差距 / 行动**：**P0**（若目标市场涉密/政务）。最小路径：引入 `tjfoc/gmsm`，用构建标签 `gm` 提供 SM2 证书 + SM3 摘要 + SM4 加密 + 国密 TLS 套件；同时支持国密 SSL 卸载（由前置网关/密码机承担）。**不要在没有密码机/CA 的情况下承诺「密评通过」。**

### D. 私有化交付（无外网、自主研发、材料包）

#### D1 全离线安装包 — ❌ 不具备

- **典型条款**：私有化项目前提：无外网、无公网镜像源、内网离线安装包。
- **现状证据**：
  - 无 `docker save` / `helm package` / airgap 脚本（全仓 0 命中）。
  - 部署需拉 10+ 上游镜像（`deploy/docker/docker-compose.prod.yml:70,126,169,218,262,288,329,358,398,431`）。
  - 运行时 Helm 目录仍指向 `charts.bitnami.com`（`internal/helm/catalog.go:50`）。
  - 被管设备装中间件需公网（`apache_ops.go:130`、`apache_ext_ops.go:45/101/155/263/317/371` 等）。
  - **好消息**：前端无 CDN 外链；Helm chart 无外部依赖（`Chart.yaml` 无 `dependencies`）；License 离线验签（`internal/config/license.go:9-11`）。
- **差距 / 行动**：**P0**。产出 `airgap-bundle`：`docker save` 全量镜像 tar + `helm package` + 依赖清单 + 离线安装脚本 + 校验和；在**断网**机器上跑通一次安装。这是纯工程活，**没有技术不确定性，只有工作量**。

#### D2 自主研发 + 自主知识产权 — ✅ 已实现

- **现状证据**：自研 gRPC（`internal/grpcx`、`proto/opsmesh/v1`）；go.mod/go.sum 无蓝鲸/GSE 依赖；Go 依赖树无 GPL/AGPL，24 项 copyleft 全为 MPL-2.0；npm copyleft=0（`docs/license-compliance-deliverable.md:53`）。
- **差距 / 行动**：**P2**。投标直接主张「全栈自研 + 依赖可审计 + 无 GPL/AGPL 传染」。**注意**：MPL-2.0 × 24 尚未获法务结论（`:77` 自认），大标需先出法务意见。

#### D3 交付材料包 — ⚠️ 部分具备

- **现状证据**：DELIVERY/部署/运维/兼容矩阵文档存在；缺定价、EULA、交付 SOP、案例（`docs/projects-2026-11-07.md` P5）。
- **差距 / 行动**：**P1**。先做「一页纸交付清单 + 版本化」，再补商务材料。

### E. 产品功能广度（业务标书的技术条款）

#### E1 设备自动纳管 — ⚠️ 部分具备（有硬伤）

- **现状证据**：有网段发现 + bootstrap + 注册；**但** `server_bootstrap.go:90` 用 `os.Executable()` 把**控制面自身二进制**当 agent 下发 ⇒ amd64 控制面给 ARM64 设备 → 无法执行。
- **差距 / 行动**：**P0**（并入 A1）。按 `uname -m` 分发对应架构产物；bootstrap 脚本补 arch 探测（与 `install.go:10` 注释对齐）。

#### E2 批量作业 — ✅ 已实现（**强项**）

- **现状证据**：任务生命周期完整（重试/死信/取消/周期/告警）；E2E real backend 在 CI 常态运行（本批 PR #19 该腿绿）。
- **差距 / 行动**：**P2**。应答时作差异化主打：「整段网络设备并行执行 + 完整生命周期」是竞品常缺的。

#### E3 巡检/告警/自愈 — ? 未核

- **说明**：rule engine 代码存在，但「告警→规则→动作（自愈脚本）」的闭环未逐条取证。
- **行动**：投标前补一次端到端演示；若无闭环，标 P1。

#### E4 CMDB — ✅ 已实现（覆盖度待核）

- **现状证据**：`internal/cmdb` + 资产模型 + 图谱能力；README 能力矩阵标 ✅。
- **行动**：P2，低优先。

#### E5 多租户 — ✅ 已实现

- **现状证据**：schema 级物理隔离（`multischema/multi_schema.go:1-21`）；连接池上限（`sqlstore/sql.go:130-136`）；RBAC 116 点。
- **行动**：P2，可直接主张。

### F. 规模与性能（大标常见指标）

#### F1 万级设备纳管 — ❌ 无证据

- **典型条款**：电信资源池「分层控制（省→市→机房→设备），支持万级到十万级设备纳管」。
- **现状证据**：
  - 仓库内**无任何压测结果**（`docs/commercial-readiness-review-2026-09-25.md:694` 自认；`docs/projects-2026-11-07.md:116` 列为待办）。
  - `cmd/device-sim/main.go:4-12` 自认走**不存在的端点**、此前是假成功。
  - `internal/agent/agent.go:809` 2s 轮询 ⇒ 1000 agent ≈ 500 req/s 固定开销，**未实测**。
  - `docs/architecture.md:597/602` 以完成态写「1,000 tasks/s / 1,000+ SSE」，无实测支撑。
- **差距 / 行动**：**P0**。把 `device-sim` 改成复用同一二进制 `--mode=agent` 多实例；跑 100/500/1000 agent，产出 p50/p99、CPU/内存、固定轮询开销报告；把架构文档改为「设计目标（未实测）」。

#### F2 HA + 灾备 — ⚠️ 部分具备

- **现状证据**：控制面无状态可水平扩；审计链 `FOR UPDATE` 串行化保证副本一致；无 HA 实测/灾备演练。
- **行动**：**P1**。补一次双副本 + MySQL 主从切换演练记录。

---

## 3. 投标话术边界（**不能越线**）

### 3.1 可以正面主张（有代码实证）

| 主张 | 证据 |
|---|---|
| 自研 gRPC 管控通道，无第三方运维底座 | `internal/grpcx`、`proto/opsmesh/v1`；go.mod 无蓝鲸/GSE |
| 审计防篡改哈希链 + 180 天自动归档 + 归档边界哈希 | `sql_audit_chain.go:44-101,540-569`；`config.go:533` |
| RBAC 116 权限点 / 52 资源族 / 4 角色 + 最小权限派生 | `model/perm.go:25,215-231` |
| 多租户 schema 级物理隔离 + 连接池上限治理 | `multischema/multi_schema.go`；`sqlstore/sql.go:130` |
| 传输加密三态开关 + 生产强制 TLS + 联邦 mTLS | `config.go:501,1162`；`tlsutil.go` |
| 供应链四门禁：SBOM(SPDX+CycloneDX) + cosign + govulncheck + Trivy | `release.yml`；`_publish-image.yml`；`ci.yml:520/650` |
| 依赖树 0 GPL/AGPL，无 vendor 逃逸 | `docs/third-party-licenses.md`；`docs/license-compliance-deliverable.md:53` |
| 纯 Go 二进制，不依赖 Tomcat/Java 中间件 | `docs/deployment-scenarios.md:1738-1740` |
| 离线 Ed25519 授权验签（无 License Server） | `internal/config/license.go:9-11` |
| 前端无 CDN 外链，Helm chart 无外部依赖 | 0 命中；`Chart.yaml` |

### 3.2 **不能**说（现场会被打穿）

| 不能说 | 实际 |
|---|---|
| 支持达梦/人大金仓/openGauss | 只有 MySQL 驱动 |
| 支持国密 SM2/SM3/SM4 | 0 行代码 |
| 支持 ARM64/龙芯自动纳管 | 镜像单架构 + bootstrap 下发控制面自身二进制 |
| 有离线安装包 | 无 `docker save`/`helm package`/airgap 脚本 |
| 支持双因素认证 | 只有 `needMFA` 标志且仍发 token |
| 有堡垒机/会话录像/命令回放 | 只有任务 stdout + 审计事件 |
| 已做千级设备压测 | 无任何压测结果；模拟器不可用 |
| 国产中间件可部署 | deploy 是 `echo` 桩 |
| 国产 OS 已完成兼容性互认 | 无真机验证、无证书 |
| K8s 集群升级/回滚/证书轮换可用 | 三条动作后端 501（`k8slifecycle/handler.go:490/553/570`） |
| SSE 事件契约 9 种 | 实际 11 种（`api-specification.md:1108`、`sse.js:24-36`） |

### 3.3 可用的**策略性表述**（真话，但不自曝短板）

- 国产 OS：「支持纳管运行主流 Linux 发行版（含麒麟/UOS/openEuler）的设备；平台自身部署已在 Debian/Alpine 验证，国产 OS 适配按项目交付阶段完成。」——**不说「已互认」**。
- 国产 DB：「平台当前以 MySQL 为默认存储，已规划方言抽象层；国产 DB 适配按项目需求交付。」——**不说「已支持」**。
- 国密：「支持通过前置网关/密码机卸载实现国密 TLS；产品内置国密算法按项目版本提供。」——**不说「已内置」**。
- 2FA：「支持与前置统一身份网关（APISIX/蓝鲸 IAM）对接实现 MFA；内置 TOTP 在路线图中。」——**不说「已支持」**。

---

## 4. 「先做到能投标」的最小闭环（建议排序）

> 原则：**先补一票否决项，再补加分项；先让文档说真话，再谈扩能力。**

### 第 0 步（1 周，纯文档，零代码风险）

0. **优先处理「文档自己打自己」的 5 处**（来自 2026-11-08 深度复核，现场最容易被要求出示报告）：
   - `docs/deployment-scenarios.md:1677-1683` 国产化兼容矩阵把 4 组标「✅ 已验证」⇒ 改「待验证/未适配」（代码无国产 DB 驱动、无 ARM64 镜像）。
   - `docs/deployment-scenarios.md:813/824` 的 `opsmesh batch create` **命令不存在**（`cmd/opsmesh/main.go` 无 `batch` 子命令）⇒ 删除或改真实接口。
   - `docs/deployment-scenarios.md:917`「≥50000 台设备纳管」⇒ 改「设计目标，无压测证据」。
   - `README.md:239`「9 事件名」⇒ 11 种（`docs/api-specification.md:1108` 与前端 `sse.js:24-36` 均为 11）。
   - `README.md:235` / `docs/product-design.md:114` 的 K8s 生命周期整行 ✅ ⇒ 标注 `upgrade` / `rollback` / `cert-rotate` **后端 501**（`internal/k8slifecycle/handler.go:490/553/570`）。

1. 订正 `docs/deployment-scenarios.md` 中国密/多 DB/ARM64 tag 的完成态口吻 ⇒ 统一标「规划中/按项目交付」。
2. 订正 `docs/multi-os-support.md`：明确「设计稿，非现状基线」。
3. 订正 README：审计能力（180 天+哈希链）、RBAC（116 点/52 族）、基础镜像（debian-slim）。
4. 订正 `docs/architecture.md` 容量指标 ⇒ 「设计目标（未实测）」。
5. 产出本表的「可对外版本」（删掉内部吐槽，保留证据与边界）。

**产出**：一份**不会被打穿**的能力声明基线。这是所有投标材料的地基。

### 第 1 步（2~4 周，P0 工程）

| 顺序 | 项 | 最小可投标准 | 估算 |
|---|---|---|---|
| 1 | 多架构镜像 + agent 分发 | `linux/amd64,linux/arm64` manifest；bootstrap 按 `uname -m` 分发；ARM64 真机纳管闭环 | 1 周 |
| 2 | 离线安装包 | `airgap-bundle`（镜像 tar + chart + 脚本 + 校验和），断网机器安装通过 | 1 周 |
| 3 | 双因素认证 | TOTP + 恢复码 + 强制开关 + 审计 | 1~2 周 |
| 4 | 国产 DB 方言层（第一阶段） | 方言接口 + PostgreSQL/openGauss 跑通建表/迁移/审计链 | 2~3 周 |
| 5 | 规模压测 | device-sim 改造 + 100/500/1000 agent 报告 | 1~2 周 |
| 6 | 国密（若目标市场涉密） | `gm` 构建标签 + SM2/SM3/SM4 + 国密 TLS 卸载说明 | 2~3 周 |

### 第 2 步（并行，P1）

- 口令有效期 + 到期强制改密（1~3 天）。
- 命令级操作留痕（不做终端录像）。
- 第三方渗透测试报告。
- HA/灾备演练记录。
- 国产中间件运维脚本从 `echo` 桩改为真实实现（按项目）。

### 第 3 步（一次演练，同时产出两份材料）

**一台干净机器 / 一个网段（20–50 台设备），一次进场同时产出「能力深度证据」与「交付 SOP」。**

1. 对 10 个高风险域（DB 备份/恢复、DB 复制、中间件部署 Kafka/Redis/ActiveMQ、网络、安全加固、K8s 集群部署、文件共享、SSH 互信、DNS/NTP）各跑 **1 次成功 + 1 次可归因失败**；每条带命令、目标环境、版本 SHA。
2. 没有证据的域一律标「未验证」，**不得标 ✅**。
3. 同一次演练产出：交付 SOP + 验收清单 + 已知限制清单（直接引用 v0.17.0/v0.18.0 降级清单，不新造口径）。

**为什么这一件事杠杆最大**：它同时消掉「广度宣传无深度证据」（D3）与「无交付包」（D9）；也是商业评估自己指定的下一步（1 个网段 / 20–50 台真实试用）。信创、容量、批量 CLI 这些宣称，只有真机才能判真伪。

---

## 5. 与各 agent 线的方向对齐（避免继续拉锯）

| 线 | 当前在做 | 与市场主线的关系 | 建议 |
|---|---|---|---|
| 本线（opencode） | shell 注入面收口批次 7–10 | B5 漏洞闭环的**支撑项**，非准入项 | 按用户裁决**做完剩余队列**（判据 C 159 / D 217），但**不再扩新批次**；完成后转 P0 |
| kilo（lead） | 主干敏感区 | 需拍板：P0 五项的优先级与归属 | 用本表做一次方向裁决 |
| qoder | 机动（docs/dead-env） | 可承接「文档真实性纠偏」 | 第 0 步优先派给它 |
| workbuddy | 取证/决策材料 | 已产出权限/审计取证 | 可承接「国产 DB 方言层调研」 |
| ZCode | 版本/发布 | 发版前需文档口径一致 | 第 0 步完成后再切版 |

**一句话**：**注入面收口是「把船板钉牢」，但船现在开的方向是「能力广度」；市场要的是「准入深度」。两者不冲突，但资源必须分开算，不能把钉船板当成在开船。**

---

## 6. 取证方法与可复跑命令

```bash
# 1) 数据库驱动
rg -n 'sql-driver|pgx|lib/pq|sqlite|dm8|kingbase|opengauss' --glob go.mod .

# 2) 国密
rg -n -i -e 'SM2|SM3|SM4|gmsm|tjfec|gmssl|国密|商用密码|密码机|GM/T' --glob '!docs/**' --glob '!web/**/node_modules/**' .

# 3) 多架构
rg -n -i 'platforms|--platform|loongarch|mips64|GOARCH' .github/workflows/ .goreleaser.yml Dockerfile*

# 4) 离线包
rg -n -i 'docker save|helm package|airgap|离线包' --glob '!docs/**' --glob '!CHANGELOG.md' .

# 5) 2FA
rg -n -i 'totp|mfa|2fa' --glob '!docs/**' --glob '!web/**/node_modules/**' .

# 6) 压测产物
rg --files -g '*bench*' -g '*load*' -g '*perf*' -g '*stress*'

# 7) 审计链
sed -n '44,110p' internal/store/sqlstore/sql_audit_chain.go

# 8) RBAC 规模
rg -n 'PermSpecs|len\(' internal/store/model/perm.go
```

---

## 7. 附：与 21 条要求对应的「文档订正清单」

| 文档 | 现说法 | 应改为 | 依据 |
|---|---|---|---|
| `docs/deployment-scenarios.md:912/1015/1693/2079/2646` | 商密 SM2/SM3/SM4 完成态 | 「规划中，按项目交付」 | 国密 0 行代码 |
| `docs/deployment-scenarios.md:1726` | 已写「国密与多 DB 未实现，勿配」 | 保留，并作为全篇唯一口径 | 该处是诚实口径 |
| `docs/deployment-scenarios.md:1716` | `v1.0.0-linux-arm64` tag | 改为 goreleaser 实际模板 `{{.ProjectName}}-{{.Version}}-{{.Os}}-{{.Arch}}` | `.goreleaser.yml` |
| `docs/multi-os-support.md:4` | 状态「设计稿」但正文按基线写 | 全文加「设计稿，非现状」水印 | 无 PAL 实现 |
| `README.md:237` | 「≥6 月导出」 | 「180 天自动归档 + 哈希链 + 归档边界哈希」 | `config.go:533`、`sql_audit_chain.go` |
| `README.md:1057` | 「24 条默认权限」 | 「116 权限点 / 52 资源族」 | `model/perm.go:25` |
| `docs/architecture.md:597/602` | 1,000 tasks/s / 1,000+ SSE 完成态 | 「设计目标（未实测）」 | 无压测产物 |
| `docs/license-compliance-deliverable.md:50` | 控制面 distroless | `debian:bookworm-slim` | `Dockerfile:66` |
| `pkg/provision/install.go:10` | 注释称「探测 OS/arch」 | 实现补上探测，或改注释 | 模板无 `uname -m` |
| `docs/deployment-scenarios.md:1677-1683` | 国产化兼容矩阵 4 组「✅ 已验证」 | 「待验证 / 未适配」 | 无国产 DB 驱动、无 ARM64 镜像 |
| `docs/deployment-scenarios.md:813/824` | `opsmesh batch create` | 删除或改真实 API | `cmd/opsmesh/main.go` 无 `batch` 子命令 |
| `docs/deployment-scenarios.md:917` | 「≥50000 台设备纳管」 | 「设计目标，无压测证据」 | 仓库无压测产物 |
| `README.md:239` | 「9 事件名」 | 「11 种」 | `api-specification.md:1108`、`sse.js:24-36` |
| `README.md:235` / `docs/product-design.md:114` | K8s 生命周期整行 ✅ | 标注 3 条动作 501 | `k8slifecycle/handler.go:490/553/570` |
| `docs/product-design.md:142` / `docs/projects-2026-11-07.md:153` | 能力矩阵「34 ✅ / 1 ⚠️ / 1 ❌」 | 按 README 实测改「32 / 2 / 1」（或补齐矩阵） | README.md:223-264 逐行计数 |
