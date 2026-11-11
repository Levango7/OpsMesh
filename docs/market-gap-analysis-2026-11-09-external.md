# OpsMesh 能力声明（对外版）

> 性质：基于代码实证的对外能力声明基线。由内部《招标要求 → OpsMesh 现状 全景对照表》（`docs/market-gap-analysis-2026-11-09.md`，2026-11-09 版）派生；派生规则：删去内部排期、资源判断与话术边界，仅保留证据与真实边界。
> 取证基线：`origin/main`（2026-11-09，批次 10 合并后 `65e1667`；2026-11-10 文档订正批不改代码，代码侧结论仍然有效）；代码侧结论一律给 `file:line` 或可复跑命令。
> 出处约定：`[招]` = 招标文件/采购公告/选型指南的公开条款；`[标]` = 等保/密评/适配认证的公开规范与测评口径。

## 1 可正面主张的能力（均有代码实证）

| 主张 | 证据 |
|---|---|
| 自研 gRPC 管控通道，无第三方运维底座 | `internal/grpcx`、`proto/opsmesh/v1`；go.mod 无蓝鲸/GSE 依赖 |
| 审计防篡改哈希链 + 180 天自动归档 + 归档边界哈希 | `sql_audit_chain.go:44-101,540-569`；`config.go:533` |
| RBAC 116 权限点 / 52 资源族 / 4 角色 + 最小权限派生 | `model/perm.go:25,215-231` |
| 多租户 schema 级物理隔离 + 连接池上限治理 | `multischema/multi_schema.go`；`sqlstore/sql.go:130` |
| 传输加密三态开关 + 生产强制 TLS + 联邦 mTLS | `config.go:501,1162`；`tlsutil.go` |
| 供应链四门禁：SBOM(SPDX+CycloneDX) + cosign + govulncheck + Trivy | `release.yml`；`_publish-image.yml`；`ci.yml:520/650` |
| 依赖树 0 GPL/AGPL，无 vendor 逃逸 | `docs/third-party-licenses.md`；`docs/license-compliance-deliverable.md:53` |
| 纯 Go 二进制，不依赖 Tomcat/Java 中间件 | `docs/deployment-scenarios.md:1738-1740` |
| 离线 Ed25519 授权验签（无 License Server） | `internal/config/license.go:9-11` |
| 前端无 CDN 外链，Helm chart 无外部依赖 | 0 命中；`Chart.yaml` |

## 2 现状对照（21 条，含未实现项的如实标注）

| 编号 | 要求 | 现状 | 一句话证据 |
|---|---|---|---|
| A1 | 国产 CPU 适配 + 互认证明 | ❌ | 发布仅 `linux/amd64, linux/arm64`（`.goreleaser.yml:27-31`）；loongarch64/mips64 零命中；无任何互认证书 |
| A2 | 国产 OS（麒麟 V10/UOS/openEuler） | ⚠️ | agent 仅读 `/etc/os-release`（`internal/agent/agent.go:287-293`）；平台自身无发行版分支、无真机验证 |
| A3 | 国产数据库（达梦/金仓/openGauss/OceanBase） | ❌ | 16 个模块只有 `go-sql-driver/mysql`（`go.mod:8`）；迁移是 MySQL 专有语法（`migrations/001_initial.sql:76`） |
| A4 | 国产中间件兼容 | ✅/❌ | 平台自身纯 Go、不依赖 Java 中间件（加分）；但国产中间件 deploy 是占位实现（`domestic_ops.go:303/305/365-369`） |
| B1 | 双因素认证（2FA/MFA） | ❌ | 仅 `needMFA` 标志且仍下发 token（`services/auth-svc/internal/http/gateway.go:218`）；无 TOTP/短信/邮箱码 |
| B2 | 审计留存 ≥6 月、防篡改、可检索 | ✅ | 哈希链 + 三重校验（`sql_audit_chain.go:44-101`）+ 归档边界哈希（`:540-569`）+ 默认 180 天（`config.go:533`） |
| B3 | 通信传输保密性（HTTP 也要加密） | ✅ | `--http-tls auto\|on\|off`（`config.go:501`）+ 生产无 TLS 拒绝启动（`:1162`）+ 联邦 mTLS（`tlsutil.go`） |
| B4 | 运维操作审计 / 堡垒机等价 | ❌ | 无终端录像/PTY/命令回放；只有任务 stdout + 审计事件（`README.md:241` 自认「有历史无回放」） |
| B5 | 渗透测试与漏洞闭环 | ⚠️ | CI 有 gosec + govulncheck + Trivy（`ci.yml:520/650`）；无第三方渗透报告 |
| B6 | 身份鉴别（口令策略）+ RBAC | ⚠️ | 复杂度/黑名单/锁定 ✅（`password.go:31-66`、`loginguard/guard.go:24-30`）；**口令有效期 ❌**；RBAC 116 点/52 族 ✅（`model/perm.go:25`） |
| C1 | 国密 SM2/SM3/SM4 + 国密 TLS | ❌ | 代码侧国密关键词 0 命中；实测算法仅 AES-GCM / bcrypt / SHA-256 / HMAC / Ed25519 |
| D1 | 全离线安装包（airgap） | ❌ | 无 `docker save` / `helm package` / 依赖预拉取脚本；`docker-pull-retry.sh` 只是 429 重试 |
| D2 | 自主研发 + 自主知识产权 | ✅ | 自研 gRPC 通道；go.mod/go.sum 无蓝鲸/GSE 依赖；Go/npm 树 0 GPL/AGPL（`docs/third-party-licenses.md`） |
| D3 | 交付材料包（手册/清单/报告） | ⚠️ | DELIVERY/部署文档有；缺定价、EULA、交付 SOP、案例；部分文档与代码漂移 |
| E1 | 设备自动纳管（发现→推送→注册） | ⚠️ | 有 discovery/bootstrap；但 **ARM64 设备拿到的是控制面自身 amd64 二进制**（`server_bootstrap.go:90`） |
| E2 | 批量作业（脚本/服务/文件分发） | ✅ | 失败重试/死信/取消/周期全链路；E2E 覆盖（CI real backend 绿） |
| E3 | 巡检/监控告警/事件联动自愈 | ? 未核 | rule engine 存在，但「告警→动作」闭环未逐条取证 |
| E4 | CMDB / 资产台账 | ✅ | `internal/cmdb` + 部署/配置库图谱；覆盖度未逐项核 |
| E5 | 多租户 / 分级分权 | ✅ | schema 级物理隔离（`multischema/multi_schema.go:1-21`）+ 连接池上限（`sqlstore/sql.go:130-136`）+ RBAC |
| F1 | 万级~十万级设备纳管（容量证据） | ❌ | 仓库内无任何压测结果；`device-sim` 未接入真实端点（`cmd/device-sim/main.go:4-12`）；2s 轮询 ≈ 1000 agent / 500 req/s 未实测 |
| F2 | 控制面 HA + 灾备 | ⚠️ | 无状态可扩 + 审计链 `FOR UPDATE` 串行化；无 HA 实测/灾备演练 |

计数：✅ 6 条 / ⚠️ 6 条 / ❌ 8 条 / ? 1 条。

## 3 边界应答口径（对客户/招标询问的标准应答）

- 国产 OS：「支持纳管运行主流 Linux 发行版（含麒麟/UOS/openEuler）的设备；平台自身部署已在 Debian/Alpine 验证，国产 OS 适配按项目交付阶段完成。」
- 国产 DB：「平台当前以 MySQL 为默认存储，已规划方言抽象层；国产 DB 适配按项目需求交付。」
- 国密：「支持国密算法演进路径；密码能力以密码机/前置网关卸载为主；按项目交付。」
- 2FA：「支持与前置统一身份网关（APISIX/蓝鲸 IAM）对接实现 MFA；内置 TOTP 在路线图中。」

## 4 取证方法（可复跑命令）

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

*本文件由内部版派生；内部版含排期与话术边界，不对外。代码侧引用的行号以 2026-11-09 基线 `65e1667` 为准，其后代码未变更。*
