# OpsMesh 许可口径决策材料

> 生成时间：2026-10-03
> 决策问题：仓库内 `LICENSE` 是 Apache-2.0 全文，但 `README.md` 与 `docs/product-design.md`
> 均声称"内部项目，私有部署"。二者必有一错，且这一项直接决定"能不能收费"。
> 本材料把两条路径各自的法律后果摊开，给出**建议**，最终由商务 + 法务签字。

---

## 0. 事实基线（可复核）

| 事实 | 出处 | 影响 |
|---|---|---|
| `LICENSE` 是 Apache License 2.0 **全文**（11,731 字节） | `LICENSE:1-3` | 代码当前受 Apache-2.0 约束 |
| `NOTICE` 按 Apache-2.0 的 **Section 4(d)** 写合规声明 | `NOTICE:6-11` | NOTICE 是为 Apache-2.0 准备的合规产物 |
| README 称"内部项目，私有部署" | `README.md:1153` | 与上两条矛盾 |
| 产品设计文档竞品表把 OpsMesh 的 License 写为"私有（内部项目）" | `docs/product-design.md:172` | 同上 |
| 同文档称"当前 License 为私有，未开源。若未来开源，建议 Apache 2.0" | `docs/product-design.md:199` | 把 Apache-2.0 描述为**未来**选项 |
| 依赖树中 **GPL / LGPL / AGPL / CDDL / EPL 依赖数 = 0** | `docs/third-party-licenses.md` 汇总段 | **无强 copyleft 传染** |

依赖许可证分布（162 个 module@version）：

| 许可证 | 模块数 | 性质 |
|---|---|---|
| Apache-2.0 | 50 | 宽松 |
| MIT | 47 | 宽松 |
| BSD-3-Clause | 31 | 宽松 |
| **MPL-2.0** | **24** | **文件级 copyleft（本次唯一的法律关注点）** |
| BSD-2-Clause | 9 | 宽松 |
| ISC | 1 | 宽松 |

---

## 1. Apache-2.0 路径：隐忧逐条排查

用户问"Apache 是否有太多隐患"。逐条查证结果如下——**结论是没有法律阻断项，代价在商业模式**。

### 1.1 MPL-2.0（24 个模块）——文件级 copyleft，**非阻断**

- 唯一两个**直接**依赖：`go-sql-driver/mysql` v1.10.0、`hashicorp/vault/api` v1.23.0。
  其余 22 个均为 hashicorp 体系的间接依赖。
- MPL-2.0 的义务边界是**文件级**的：修改某个 MPL 文件才需公开该文件，
  **原样链接进闭源二进制不产生源码提供义务**。
- OpsMesh 未修改这两个库的任何文件（`go.sum` 锁定版本，无 vendor 目录、无 replace 指向它们）。
- 保留义务：`NOTICE` 须逐字保留上游署名（已由 `gen-third-party-licenses.sh --emit-notice` 落实）。
- **结论：非阻断。** 唯一需要长期守住的纪律是**不要 patch 这两个库**——
  一旦本地改动 `go-sql-driver/mysql` 的某个文件，该文件即须开源。

### 1.2 容器基础镜像的 GPL/LGPL —— **可管理，非阻断**

`docs/third-party-licenses.md` 已指出这条风险面。三处需要结论：

| 镜像 | 内含 GPL/LGPL 程序 | 义务 | 现状 |
|---|---|---|---|
| `gcr.io/distroless/static-debian12` | 几乎无（仅 glibc 等运行库） | 极低 | **最省事**，继续保持 |
| `debian:bookworm-slim` | coreutils、bash 等 | 提供源码获取途径 | 指向 Debian 官方 + 随附 source 链接即可 |
| `alpine:3.23` | busybox、musl | 同上 | 同上 |

标准做法是随产品提供"第三方组件及源码获取途径"清单（GPL-2a §3 / LGPL §6 要求）。
`docs/third-party-licenses.md` 已覆盖 Go 依赖，**缺的是 OS 包这一段**。
→ 这是**交付物补齐项**，不是许可路线障碍。

### 1.3 Apache-2.0 自身的代价 —— 这才是真正的"隐患"

| 代价 | 说明 | 能否规避 |
|---|---|---|
| **任何人可再分发** | 拿到源码者可原样打包成竞品销售，Apache-2.0 §4(a) 明确授予 | **不能**。这是采用 Apache-2.0 的固有成本 |
| **专利授权 + 专利诉讼终止条款** | §3 授予专利许可；若发起专利诉讼则许可终止 | 不能规避，但相比 GPL 已算温和 |
| **无 copyleft 保护** | 竞品可闭源衍生，无法强制其公开 | 不能 |
| 商标 | Apache-2.0 **不授予**商标许可 | 可用商标声明独立保护 |

**一句话：Apache-2.0 的问题不是"法律风险"，是"你守不住商业化"。**
代码可以开源，但**收费点必须放在 Apache-2.0 覆盖不到的地方**。

---

## 2. 闭源（商业 EULA）路径的代价

若把 `LICENSE` 换成商业 EULA：

- ✅ 守得住商业化，可以收全功能的钱
- ❌ **必须先终止 Apache-2.0 的授权**。Apache-2.0 §4(b) 允许被许可人永久、不可撤销地终止授权，
  所以技术上随时能切换，但**已按 Apache-2.0 分发出去的副本仍受 Apache-2.0 约束**——
  那些客户有权继续使用、修改、再分发。仓库若无外部分发，可忽略。
- ❌ 所有客户需要签署 EULA，法务成本高（私有化交付常见做法，可接受）
- ❌ 依赖 hashicorp/vault/api 的 MPL-2.0 义务不变（与路线无关）

---

## 3. 建议：Open-Core（内核 Apache-2.0 + 企业版商业授权）

两条纯路都不是最优。**Open-Core 同时拿到 Apache-2.0 的采纳速度与商业化的守成**：

| 层 | 范围 | 许可 |
|---|---|---|
| **内核** | `internal/`（controlplane / agent / store / 各引擎）、`cmd/`、`pkg/`、`operator/` | **Apache-2.0**（即现有 `LICENSE`，无需改动） |
| **企业版** | `web/enterprise/` 前端、企业级 API（多租户 schema、SSO/LDAP/OIDC、计费、白标） | **商业授权**（`LICENSE-ENTERPRISE`） |
| **服务与支持** | 版本升级、故障响应、私有化交付实施 | 商业合同 |

**为什么这是最优解：**

1. `LICENSE` **不需要改动**——现状（Apache-2.0）对内核是准确的，矛盾自然消解。
2. README/product-design 里"私有"的表述**改为分层描述**即可，不再自相矛盾。
3. 商业价值守得住：收费点在企业版前端 + 企业级 API，而这两者需要 `--license-key` 才能启用
   （本轮已实现，见 §5）。**拿到镜像 ≠ 拿到付费功能**。
4. 第三方许可义务完全不变，两条路线都要履行。
5. 契合现状：企业版前端本就通过 `/enterprise/` 路由独立服务，技术上天然可隔离。

**代价（诚实列出）：**
- 社区版会分流一部分只用到基础功能的客户 → 需靠版本分层制造实质差异
- 双许可治理有沟通成本 → 需在 README 首屏讲清楚

---

## 4. 需要补齐的交付物（无论走哪条路）

| # | 事项 | 责任 | 状态 |
|---|---|---|---|
| 1 | OS 包（Debian/Alpine 内 GPL/LGPL）的源码获取途径清单 | 交付 | **待补** |
| 2 | npm 依赖（`web/enterprise/node_modules`）许可清单 | 交付 | **待补** |
| 3 | 24 个 MPL-2.0 的商务确认函 | 法务 | **待补**（P1-7 判定面） |
| 4 | `web/enterprise/` 的独立许可声明文件 | 本轮 | **本轮已做** |
| 5 | 企业版能力表"已实现/规划"两栏化 | 产品 | **待做**（当前 OIDC/SAML/LDAP 标 ✅ 但代码零命中） |

---

## 5. 配套技术闸门：授权校验（已实装）

Open-Core 要成立，技术上必须有"企业版功能不是白给"的闸门。**已实现并通过测试**：

### 5.1 机制

| 项 | 实现 |
|---|---|
| 凭据格式 | `base64url(payloadJSON) + "." + base64url(ed25519Signature)` |
| 载荷字段 | `edition` / `customer` / `devices` / `expires` / `issuedAt` / `features` |
| 公钥输入 | PEM 文本、文件路径、裸 base64、裸 hex（容器里无 openssl 也能用） |
| 校验顺序 | 格式 → 验签（覆盖载荷**原始字节**）→ edition → 有效期 |
| 依赖 | 无。不依赖 License Server，私有化内网离线可用 |
| 传参 | `--license-key` / `--license-public-key`（或 `OPSMESH_LICENSE_*`），**无需重新编译** |

### 5.2 闸门位置（三层，刻意不做大范围 API 拦截）

| 层 | 未授权时的行为 |
|---|---|
| `/enterprise/` 外壳与 SPA 深链 | 200 返回「未授权」说明页（能力清单 + 获取路径 + `X-OpsMesh-License: community` 头） |
| `/enterprise/assets/*` | 402 Payment Required —— 但**仅对确实存在的文件**；路径穿越与不存在资源一律 404 |
| `GET /api/v1/license` | 200 可查（`platform:read` 权限，viewer 即可）。**刻意不上闸**：未授权用户恰恰最需要知道"为什么没授权" |
| `/api/v1/**` 其余、agent 通道、监控 | **完全不受影响**——Apache-2.0 内核对所有人开放 |

**不按 API 路径前缀大面积设闸的理由**：内核 API 闸掉会让社区版形同残废，且与 LICENSE 的声明自相矛盾。企业版的独占价值在**前端交付物 + 商业支持 + SLA**，交付层设闸即可覆盖。

### 5.3 付费边界：功能清单机制（已实装）

原先 `License.Features` 字段**解析了但从不被查询**——"哪些能力要付费"因此只能硬编码成代码里的 `if`，
每加一个付费能力就要改一次代码。已改为可执行机制：

| 情形 | `featureEnabled(name)` |
|---|---|
| 未授权 | `false`（未授权时无任何能力可用） |
| 已授权，`features` 为空 | `true`（该 edition 下全部功能；**向后兼容**早于本机制签发的凭据） |
| 已授权，`features` 非空且含该能力 | `true` |
| 已授权，`features` 非空但不含 | `false` |
| 未在代码中登记的能力名 | `false`（fail-closed 兜底） |

已接入的实际闸门：企业版前端交付（能力标识 `frontend`），见 `enterprise_ui.go`。

**为什么这样设计**：付费边界成为**凭据里的数据**而非代码里的分支。签发时用
`-features frontend,sso` 决定客户买什么，闸门只负责对照。新增付费能力时在闸门加一行
`featureEnabled("xxx")` 即可——签发格式不变，也不需要为每个客户改代码。

功能名在解析时统一归一化（小写 / 去空白 / 去重），避免 `"Frontend"` 与 `"frontend"`
被判为两种能力而把付费客户拦在门外。

**尚未接闸门的能力**（`product-design.md` §5.2 现状栏已如实标注）：
- OIDC/SAML/LDAP：**代码零实现**，无从谈闸门（先补实现）
- 密钥轮转：**未实现**（`internal/secrets/` 有 Vault/KMS provider 但无 rotate）
- 多租户 schema 隔离：已实现但**当前归内核免费**——是否收费是产品决策，不在技术侧预设

### 5.4 设备数上限：暂不强制，并显式声明

`License.Devices` 同样只被解析不被执行。**不擅自补强制逻辑**，因为"超限后降级什么"
（阻断企业前端？拒绝下发任务？只告警？）是产品决策而非技术决策。

未定义前，选择**如实披露**而非沉默：`GET /api/v1/license` 在 `devices > 0` 时返回
`devicesEnforced: false`。理由是"承诺了但没实现"比明确说"暂不强制"更糟——
运维真到超限时才发现拦不住，已经晚了。

### 5.5 已验证的关键行为

| 行为 | 测试 |
|---|---|
| 异私钥签发的凭据被拒 | `TestVerifyLicense_ForgedSignature` |
| 篡改载荷被拒（签名覆盖原始字节） | `TestVerifyLicense_TamperedPayload` |
| 过期被拒；零值 `expires` = 永久授权 | `TestVerifyLicense_Expired` / `_PermanentExpiry` |
| 非 enterprise edition 被拒 | `TestVerifyLicense_WrongEdition` |
| 公钥四种输入形式均可解析 | `TestParsePublicKey_AllForms` |
| **任何校验失败都不阻止启动**（降级为社区版 + `reason`） | `TestInitLicenseState_AllFailureModesDoNotBlockStartup`（6 种坏配置） |
| `s.lic == nil` 时 fail-closed（按未授权处理） | `TestServerLicensed_NilStateFailsClosed` |
| 快照/页面永不泄露凭据原文 | `TestLicenseSnapshot_NeverLeaksCredential` |
| 授权态与社区态下社区控制台均正常 | `TestEnterpriseUI_CommunityCoreUnaffected` |
| 授权端点权限点必须真实存在于 RBAC 规格 | `TestHandleLicenseStatus_PermissionPointExists` |
| 功能清单三态语义（含老凭据兼容、大小写、去重、未知能力 fail-closed） | `TestFeatureEnabled_ThreeStates`（8 子用例）+ `_UnknownFeatureIsDenied` |
| 快照回显 features；无清单返回空数组而非 nil | `TestLicenseSnapshot_ExposesFeatures` |
| 设备上限如实标注 `devicesEnforced: false` | `TestLicenseSnapshot_DevicesEnforcementIsDisclosed` |
| 功能清单**真的**作用在外壳交付上（不是没接线的字段） | `TestEnterpriseUI_FeatureListGatesShell` |
| 签发器产出 → 服务端解析链的格式契约 | `TestIssueToVerifyRoundTrip` |

### 5.6 两个必须知悉的边界

1. **这是许可控制，不是安全边界。** 校验逻辑与二进制都在客户手里，改二进制即可绕过。它的价值是给"付费能力"一个明确的授权语义与可留档凭据，**不是防破解**——防破解需配合服务端校验或硬件信任根，不在当前范围。
2. **多副本必须一致**：`--license-public-key` 各副本不一致时，负载均衡到不同副本会出现"功能时有时无"。已在 `docs/flag-matrix.md` §2.1 记录。

实现见 `internal/config/license.go`、`internal/config/license_pem.go`、`internal/controlplane/license_gate.go`；签发工具见 `deploy/scripts/license-issue/`（厂商侧签发真实授权）与 `deploy/scripts/license-ci/`（CI 用临时密钥，两者职责不可混用）。部署接线见 Helm `controlplane.licensePublicKey` / `licenseKey` 与 `docker-compose.yaml` 的 `OPSMESH_LICENSE_*`。

---

## 6. 仍待决策的一件事

**企业版的免费/付费边界**：具体哪些能力要 key。当前实现只闸了**企业版前端交付**（唯一已实现、且当前技术上"白给"的部分）。若后续把多租户 schema、OIDC/SAML/LDAP、Vault/KMS UI 判定为付费能力，需逐项加 `requireEnterpriseGate`——但注意后几项目前**代码零命中**（见 §4 待办第 5 项），先补实现再谈闸门。

---

## 附：证据索引

| 结论 | 复核命令 |
|---|---|
| 无强 copyleft | `grep -iE "GPL\|AGPL" docs/third-party-licenses.md`（仅命中说明文字，无实际条目） |
| MPL 直接依赖仅 2 个 | `grep "\| 直接 \|" docs/third-party-licenses.md` |
| 许可身份矛盾 | `head -3 LICENSE` vs `sed -n '1153p' README.md` |
| 无授权闸门 | `grep -c "license\|edition" internal/config/config.go`（改造前为 0） |
| 授权闸门已实装 | `grep -c "licensed()" internal/controlplane/enterprise_ui.go`（改造前无该文件） |
| 依赖树无强 copyleft（复核） | `go list -deps ./... \| xargs go list -m -f '{{.Path}} {{.License}}' 2>/dev/null \| grep -iE "gpl\|agpl"` |
