# TD-88 工程侧侦察：控制面密钥「明文落库」现状与两方向评估

> **侦察基准**：`ee0f378`（2026-10-09 main 顶点）。
> **性质**：只读侦察 + 设计评估。TD-88 在台账中标为「属产品/架构决策，未动工」；本文**不代替决策**，只把决策所需的事实、代价与兼容面列清。
> **与并行会话关系**：全程只读；`internal/secrets`、`internal/store`、`services/config-svc` 均未改动。

---

## 1. 结论摘要

**台账那句「SecretItem.Value 明文落库是设计现状」属实，但比台账描述的更具体，也更值得先决策**：

| # | 事实 | 证据 |
|---|---|---|
| 1 | `secrets` 表（控制面 `SecretItem`）**实现齐备但零消费方**——控制面对 `SecretItem`/`SecretMeta`/`SecretStore` **零引用**（非测试），无 HTTP handler、无前端界面 | §2.2 |
| 2 | 台账的「方向 B：`${provider:key}` 引用格式」**已经建成并在投产使用**，不是待建项 | `internal/secrets/provider.go:199-238`、`internal/notify/channels.go` 6 处调用 |
| 3 | 台账的「方向 A：库级静态加密」在 **config-svc 已有刚落地的新鲜参照实现**（AES-256-GCM + `enc:v1:` 前缀 + 存量兼容 + 硬失败） | `services/config-svc/internal/store/store.go:91-137` |
| 4 | config-svc 那把加密密钥来自 `.env`，**与库同机同部署**——方向 A 若照搬，DB 脱库+主机失陷即同时失密钥；`internal/secrets/kms.go` 的 `KMSProvider` 已搭好但未接线 | `mysql.go:49-51`、`docker-compose.prod.yml:780`、`kms.go:83` |

**核心判断**：TD-88 的真实问题**不是「选 A 还是选 B」**（B 已存在、A 已有参照），而是**先决定 `secrets` 表的去向**——它是「待接线的产品功能」、「应废弃的死表」、还是「应改造为引用索引」。在此之前对一张无人读写的表加密，是给死数据新增密钥管理负担。

---

## 2. 现状实测：三套并存的东西

### 2.1 已建成并投产：`internal/secrets` provider 链 + `${provider:key}`

- **Provider 实现**：`EnvProvider`（`provider.go:55`）、`FileProvider`（`:100`）、`VaultProvider`（`vault.go`，`NewVaultProvider`）、`KMSProvider`（`kms.go:83`）、`ChainProvider`（`:171`，多 provider 按优先级合并）。
- **引用解析**：`ResolveSecret`（`provider.go:209`）支持 `${provider:key}` 与 `${key}` 两种形态，非引用格式原样返回（向后兼容明文）。
- **⚠ 一个需要写进设计文档的细节**：`provider:key` 里的 provider 名**仅用于诊断**，实际解析一律走调用方传入的 provider（`provider.go:222-232`）——即 `${vault:foo}` **不会强制走 Vault**，而是由传入的 `ChainProvider` 按优先级自行选择。多 provider 环境下这可能与配置者直觉相反。
- **投产调用方（6 处 + 1 处控制面）**：
  - `internal/notify/channels.go:494/498`（webhook url + secret）、`:509/520`（dingtalk/corp 渠道）、`:524/536`（email/sms）、`:561`（SMTP 密码）
  - `internal/controlplane/notify_channels.go:239`（通知渠道密码）
- **对外 API 与前端**：`GET /api/v1/secrets/status`（`server_secrets.go:42`）、`POST /api/v1/secrets/test`（`:78`，含 `validateURLSSRF` 私网拦截）、`GET /api/v1/secrets/keys`（`:190`，仅 key 名 + 来源 provider，不返回值）；前端 `api/secrets.js:13/18/23` + 路由 `router/index.js:62`（`/secrets` 视图）。

### 2.2 孤儿：控制面 `secrets` 表（TD-88 所指的明文落库）

- **模型**：`SecretItem{Key, Value, KeyType}` / `SecretMeta{...无 Value}`（`internal/store/model/model.go:307-324`），注释已自述「API 层对外暴露时须转为 SecretMeta（脱去 Value）」。
- **接口**：`SecretStore`（`internal/store/model/contract.go:439` 起，Get/Set/Delete/List/Rotate/SecretVersions 六个方法）。
- **实现**：`internal/store/memory/memory_secret.go`（内存明文，仅进程内）+ `internal/store/sqlstore/sql_secret.go`（MySQL）+ `internal/store/multischema/multi_schema_p03.go`（委派）。
- **落库形态**：`internal/store/migrations/007_p03_secrets.sql` 中 `value TEXT`；`sql_secret.go:79-85` 直接把 `item.Value` **明文 INSERT**。`sql_secret.go:19` 的注释自己写着「生产环境 value 列须应用层加密（KMS/信封加密），DBA 不可见明文」——**这是一条存在已久的自认 TODO**。
- **消费方实测为空**：控制面非测试代码对 `SecretItem`/`SecretMeta`/`SecretStore`/`store.GetSecret`/`store.SetSecret` **零命中**；前端 `api/secrets.js` 只调 provider 三端点，**没有任何 secrets 表的 CRUD 界面**。grep 命中的 `services/config-svc/...` 是 config-svc **自己的另一套** `SecretEntry`/store，`internal/agent/agent.go:659` 与 `device-svc …SetSecret(cfg.ProvisionSecret)` 是同名不同义的方法。

**⇒ 修正台账口径**：TD-88 行写「`SecretItem.Value` 明文落库是设计现状」，容易读成「生产数据正在明文落地」。实测是：**表与机制俱在，但没有生产写路径**，因此暴露是**潜在的**（latent）而非正在发生的。这个区别直接影响优先级排序。

### 2.3 参照实现：config-svc 的 `enc:v1:`（TD-65 同批新建）

- **原语**：`secretCipherPrefix = "enc:v1:"`（`store.go:91`）；`encryptSecret` = AES-256-GCM + 随机 nonce 前置 + base64（`:93`）；`decryptSecret` 返回 `(plaintext, legacy, err)` 三元组（`:110` 附近）。
- **三态语义（这套设计最值得抄的部分）**：无前缀 ⇒ 历史明文存量，原样返回并提示轮换；有前缀解不开 ⇒ 密钥不匹配/数据损坏，**硬失败**，绝不做「解密失败就原样返回」。
- **一致性**：内存与 MySQL 两后端共用同一对原语（注释点명 TD-61 教训：同一职责各写一份必然漂移——config-svc 曾经「内存加密、MySQL 明文」）。
- **测试**：`mysql_integration_test.go:110-111`（断言落库密文带 `enc:v1:`）、`:135-140`（历史明文存量可读）、`:160`（轮换后仍是密文）；`secret_crypto_test.go:76`（被篡改密文必须解密失败）、`:92`（写读往返）。
- **⚠ 密钥来源弱点**：`cfg.EncryptionKey` ← `CONFIG_SVC_ENCRYPTION_KEY`（`mysql.go:26/32/49`），compose 用 `${CONFIG_ENCRYPTION_KEY:?...}` **强制注入**（`docker-compose.prod.yml:780`）；为空时退化成 `ephemeral-<纳秒>-<dsn>`（`mysql.go:49-51`，仅进程内有效）。**密钥与库处在同一部署的 `.env` 里**：抗「脱库」，不抗「失主机」。

---

## 3. 两方向评估（注意：不是二选一）

### 方向 B：`${provider:key}` 引用格式 —— **已建成**

剩余工作不是「建」，而是「决定 secrets 表与它的关系」：

| 选项 | 做法 | 代价 | 风险/收益 |
|---|---|---|---|
| B1 废弃 | 删除 `SecretStore` 接口 + 三实现 + `secrets` 表（或保留表但不再写入） | 小（零消费方，删除不破坏任何调用） | 收益：消除一整套明文存储的面与维护成本；风险：若产品原规划有「内置密钥库」功能，等于砍功能——**需产品确认** |
| B2 改造为引用索引 | `secrets` 表只存 `${provider:key}` 引用串与元信息，读时经 `ResolveSecret` 解析 | 中（表加引用列 + 读路径接 provider + 前端补 CRUD） | 收益：DB 内不再出现明文；密钥统一由 env/file/vault/KMS 管理，与 notify 渠道同一条治理链 |
| B3 接线但不改语义 | 补 API + 前端，使 secrets 表成为可用的内置密钥库（值仍明文） | 中 | **不建议**：把潜在暴露变成实际暴露 |

**B2 的一个前置修正**：`ResolveSecret` 的 `provider:` 前缀是诊断性的（§2.1），若 B2 要让用户显式指定「这个 key 去 Vault 取」，需要先补齐「按前缀强制路由」的语义，否则 `${vault:x}` 在 chain 下可能落到 env。

### 方向 A：库级静态加密 —— **有现成原语，但密钥源是设计焦点**

| 选项 | 做法 | 评价 |
|---|---|---|
| A1 照搬 config-svc | 控制面 secrets 表加 AES-256-GCM + `enc:v1:` | 实现成本最低（原语可下沉为共享包，避免 TD-61 式重复）；但**必须先回答密钥从哪来**——放进 `.env` 只是把「DBA 可见明文」换成「DBA 可见密文 + 同机可见密钥」 |
| A2 KMS/信封加密 | 用已搭好的 `KMSProvider`（`kms.go:83`）托管数据密钥，库里只存封装后的 DEK | 抗「DB 脱库 + 主机失陷」；代价：要接真实 KMS（Vault transit / 云 KMS）、密钥轮换流程、以及「KMS 不可用时服务是否可启动」的可用性决策 |
| A3 两者叠加 | A2 管 DEK，A1 的格式做版本演进 | 最稳但最重；建议仅在 B2 选定「确实要自持密文」后再考虑 |

**注意**：无论 A1/A2/A3，都只解决「`secrets` 表」这一张表。控制面还有**其他明文落库面**未在 TD-88 行内枚举（见 §4），建议一并盘点后再定边界，否则会重复立项。

---

## 4. 顺带发现的相邻暴露面（未核完，建议纳入同一次盘点）

侦察中撞见、但未逐个核到「是否有脱敏」的相邻面，列出来以免 TD-88 收口时漏掉：

| 位置 | 现象 | 状态 |
|---|---|---|
| `internal/store/model/model.go` 的 `K8sCluster.Kubeconfig` | 注释要求「API 返回时须脱敏为 ***」 | **待核**：控制面 k8s handler 是否真的脱敏（本次未读该 handler） |
| config-svc 的 `ConfigEntry` 非 secret 字段 | 配置值可能含敏感信息 | 待核 |
| notify 渠道密码 | 走 `ResolveSecret`，若配置的是**明文**而非 `${...}` 引用，则明文落库 | 待核：生产 compose 的通知渠道配置是引用还是明文 |

---

## 5. 建议与待办

**给产品/架构的决策请求（阻塞后续）**：
1. `secrets` 表（`SecretStore`）是保留为产品功能、还是废弃？——这决定 B1/B2/B3。
2. 若保留：控制面自持密文的密钥源选 `.env` 静态密钥（A1）还是 KMS（A2）？建议 A2，理由是 §3 的同机弱点；若选 A1，需在文档明示「抗脱库、不抗失主机」。

**工程侧可立即做（不涉上述决策）**：
- 把 config-svc 的 `encryptSecret`/`decryptSecret` 下沉为共享包（或先加一条「不得复制第三份」的门禁注释），避免将来 A 方向落地时出现第四份实现。
- 补 §4 的三个「待核」项，产出完整暴露面清单。
- `sql_secret.go:19` 与 `007_p03_secrets.sql` 头部那两句自认 TODO，建议在表结构注释里补一句「当前无生产写路径」，防止下一个人误读为正在泄露。

**验证方式（若后续动工）**：沿用 config-svc 已立好的三段断言——落库必带 `enc:v1:`（或引用串不含明文）、历史明文存量可读且告警、被篡改密文硬失败；并在 CI integration job 用真库跑「A 写 B 读」。

---

## 6. 局限声明

- 本文为只读侦察，未运行任何代码、未启动双栈。
- 「零消费方」结论基于对 `internal/`、`services/`、`cmd/` 下非测试 Go 源码的 grep；若存在经反射/接口断言的动态调用或外部 SQL 直写 `secrets` 表（如运维脚本），不在此结论内。
- 基准 `ee0f378`；并行会话正在 TD-87 拆包 `internal/controlplane`，`server_secrets.go` 等路径可能迁移，引用以基准提交为准。
