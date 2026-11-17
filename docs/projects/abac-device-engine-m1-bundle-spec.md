# ABAC 设备侧决策引擎 M1 规格：签名策略包（bundle v1）

> **状态：M1 进行中（规格 v1 定稿，可实施）**
> **上位文档**：[ABAC 设备侧决策引擎 项目章程](./abac-device-engine-charter.md)（§5.1 / §10）
> **日期**：2026-11-17
> **代码基准**：`main@88ff295c`（本文只读核对，未改代码、未提交）
> **依赖约束**：**不引入任何新的外部依赖**。签名/哈希/编码只用 Go 标准库（`crypto/ed25519`、`crypto/sha256`、`encoding/json`、`encoding/base64`）与仓库已有依赖（`github.com/google/uuid`，`go.mod` 已直接依赖 v1.6.0）。

---

## 1. 范围

### 1.1 M1 交付

1. **bundle v1 wire 格式**（本文第 3 节）——字段、语义、限额一次冻结。
2. **规范化序列化 + SHA-256 摘要 + Ed25519 签名/验签**（第 4 节）。
3. **密钥管理契约**（第 5 节）：控制面私钥配置、agent 公钥配置、双公钥轮换、吊销。
4. **分发端点契约**（第 6 节）：`GET /api/v1/agent/abac-bundle`，含 agent 认证、ETag/`If-None-Match`、错误码。
5. **离线缓存、过期模式、回滚拒绝、能力协商、安全限额**（第 7–10 节）。
6. **参考验签库** `pkg/abacbundle`（第 12 节）：类型、规范化、签名、验签、摘要；**不**含设备端评估器。
7. **契约测试与共享 fixture**（第 13 节）。

### 1.2 M1 明确不做

- **不改 agent 代码**：不读 bundle、不评估、不上报决策、不在 heartbeat 广告能力（`abac_bundle=v1` 的实际上报随 M2 落地）。
- **不接强制链**：设备侧判定在 M2 之前不存在；M2 之后仍按 D2 保持 advisory。
- **不改变 501 守卫**：`buildABACScript` 的 `evaluate` 分支继续显式 501（禁止脚本路径伪造判定）。
- **不做审计回传**（M3）、**不做灰度/回滚指令**（M4）。
- **不引入 JSON Schema 校验库、JOSE/JWS 库、UUID 之外的新依赖**。

### 1.3 不变式

| # | 不变式 |
|---|---|
| I1 | 服务端 ABAC 评估器（`POST /api/v1/abac`）是**权威默认路径**；bundle 是它的只读快照，不是第二套语义。 |
| I2 | 私钥只存在于控制面；agent 只持有验签公钥。 |
| I3 | bundle 只含策略数据，**不含任何秘密**（密钥、token、密码、连接串）。 |
| I4 | 任一校验失败（签名/结构/限额/租户/单调性）⇒ **整包拒绝**，绝不部分加载。 |
| I5 | agent 是只读消费者，不能在设备端编辑策略。 |
| I6 | bundle 是**单租户**快照（`tenantScope`），跨租户策略不得出现在同一包内。 |

---

## 2. 术语

| 术语 | 含义 |
|---|---|
| bundle | 一份签名的、租户级的 ABAC 策略快照（JSON） |
| payload | bundle 去掉 `signature` 字段后的对象 |
| canonical payload | payload 的确定性 JSON 序列化（第 4.1 节） |
| signing bytes | `"opsmesh-abac-bundle-v1\n"` + canonical payload（第 4.2 节） |
| digest | `SHA-256(signing bytes)`，小写 hex |
| LKG | last-known-good，本机最后一次验签通过的 bundle |
| capability | agent 在 heartbeat 中声明的协议能力，M1 定义 `abac_bundle=v1` |

---

## 3. Wire 格式（bundle v1）

### 3.1 顶层对象

| 字段 | 类型 | 必填 | 约束与语义 |
|---|---|---|---|
| `schemaVersion` | string | 是 | 固定 `"1"`；未知版本整包拒绝（前向不兼容） |
| `bundleID` | string | 是 | UUID 规范小写形式（8-4-4-4-12）；建议 UUIDv7（时间有序），接受 v4 |
| `keyId` | string | 是 | 签名密钥标识，`[A-Za-z0-9._-]{1,64}`；**在签名覆盖范围内** |
| `tenantScope` | string | 是 | 目标租户 ID，1–128 字节；agent 必须与自身注册租户逐字节相等 |
| `issuedAt` | string | 是 | RFC3339 UTC、秒精度、必须以 `Z` 结尾；签发时刻 |
| `expiresAt` | string | 是 | 同上；必须严格大于 `issuedAt`，且有效期 ≤ 90 天 |
| `policies` | array | 是 | 0–1000 条；空数组合法（表示该租户无策略 ⇒ 处处 default-deny） |
| `signature` | string | 是 | `base64url`（**无 padding**，`base64.RawURLEncoding`）编码的 64 字节 Ed25519 签名 |

> **`keyId` 的双重位置**：JSON 字段（被签名）+ HTTP 响应头 `X-OpsMesh-Bundle-Key-Id`（便利副本）。两者不一致 ⇒ 拒绝（`key_id_header_mismatch`）。以签名字段为准，绝不按未签名的头选密钥。

### 3.2 Policy 对象

| 字段 | 类型 | 必填 | 约束与语义 |
|---|---|---|---|
| `id` | string | 是 | 1–128 字节；包内唯一，重复 ⇒ 整包拒绝 |
| `subject` | string | 是 | 1–512 字节；匹配语义与服务端一致（精确 / `*` / `prefix*`） |
| `resource` | string | 是 | 1–512 字节；同上 |
| `action` | string | 是 | 1–512 字节；同上 |
| `effect` | string | 是 | 仅 `"allow"` 或 `"deny"`（小写）；其他值整包拒绝 |
| `conditions` | object<string,string> | 否 | ≤32 键；键 1–128 字节、值 0–1024 字节；空对象等价于缺省。逐键等值匹配，坏 context ⇒ 不匹配（安全侧） |

**语义冻结**（与 `internal/controlplane/abac_engine.go` 逐条对齐）：

- 匹配：`abacPatternMatch` 的三形态——`""`/`"*"` 全通配、后缀 `*` 前缀匹配、其余精确等值（`abac_engine.go:37-46`）。
- 条件：`abacConditionsMatch` 的逐键等值；context 非法 JSON 视为不匹配（`abac_engine.go:49-79`）。
- 决策：**deny-overrides + default-deny**；一旦命中 deny 立即返回（`abac_engine.go:83-112`）。
- 租户：bundle 内**不重复携带** `tenantID`；`tenantScope` 即租户。生成器按服务端规则取 `p.TenantID == tenantScope || p.TenantID == ""`（全局策略），**绝不**把其他租户策略打进包（`abac_engine.go:92`）。
- 字段名差异：服务端结构体是 `condition`（`abac_engine.go:33`），bundle v1 的 wire 字段是 `conditions`。这是**有意的 wire 名冻结**，转换只发生在打包时，语义不映射（1:1 复制 map）。

### 3.3 示例

```json
{
  "schemaVersion": "1",
  "bundleID": "018f3c1e-6b0a-7c2d-9f11-2a4b6c8d0e1f",
  "keyId": "abac-2026-11",
  "tenantScope": "tenant-acme",
  "issuedAt": "2026-11-17T12:00:00Z",
  "expiresAt": "2026-11-24T12:00:00Z",
  "policies": [
    {
      "id": "p-allow-device-read",
      "subject": "user:*",
      "resource": "/api/v1/devices*",
      "action": "GET",
      "effect": "allow",
      "conditions": {"segment": "prod"}
    },
    {
      "id": "p-deny-iptables",
      "subject": "*",
      "resource": "shell:iptables*",
      "action": "*",
      "effect": "deny"
    }
  ],
  "signature": "5Kc3...64字节签名的base64url..."
}
```

> 上面的缩进仅为可读性；wire 上的 canonical payload 必须是第 4.1 节定义的紧凑形式。**签名不覆盖这段格式化文本**。

---

## 4. 规范化与签名

### 4.1 Canonical JSON（v1 冻结规则）

实现不得依赖 `map[string]any` 的默认序列化或第三方规范化库。规则：

1. **编码**：UTF-8、无 BOM；输入必须是合法 UTF-8（非法字节 ⇒ `schema_invalid`）。
2. **空白**：无任何空格、换行、制表符。
3. **对象键顺序**：按键的 UTF-8 字节序**升序**排列。顶层顺序固定为
   `bundleID, expiresAt, issuedAt, keyId, policies, schemaVersion, tenantScope`；
   policy 内固定为 `action, conditions, effect, id, resource, subject`。
4. **数组**：保持原始顺序（policies 的语义与顺序无关，但规范化不排序，避免同内容多摘要）。生成器 SHOULD 按 `id` 升序输出，以获得跨副本稳定摘要。
5. **字符串转义**：**最小转义集**——`"` → `\"`，`\` → `\\`，U+0000–U+001F → `\u00xx`（小写 hex）。**不**转义 `<`、`>`、`&`、`/`、非 ASCII；不输出 `\u` 形式的非控制字符。此规则刻意绕开 `encoding/json` 默认的 HTML 转义（`\u003c` 等），确保跨语言可复现。
6. **值类型**：v1 只允许 string、array、object<string,string>、整数；**禁止** float、null、bool、嵌套对象。`conditions` 为空对象时**省略该键**（唯一合法的键省略）。
7. **时间**：`issuedAt`/`expiresAt` 原样输出为 `YYYY-MM-DDTHH:MM:SSZ`（不做时区换算/补零变体；解析时拒绝 `+08:00` 与小数秒）。
8. **过期字段**：`signature` 永不参与 canonical payload。

> **兼容性门**：canonicalization 的任何改动都必须升 `schemaVersion`。`pkg/abacbundle/testdata/vectors/` 的 golden 字节是唯一裁决依据。

### 4.2 签名与摘要

```
signingBytes = "opsmesh-abac-bundle-v1\n" || canonicalPayload
digest       = SHA-256(signingBytes)                     // 小写 hex，32 字节
signature    = Ed25519-Sign(privateKey, signingBytes)    // base64url(RawURL) 存入 bundle.signature
```

- **域分隔前缀**是硬要求：许可证 token 的 Ed25519 先例（`internal/config/license.go:71`、`:109`）签名的是裸 JSON；若不复用前缀，bundle 签名与 license 签名存在跨协议混淆面。前缀字节**计入** digest。
- Ed25519 是确定性签名，同一 payload + 同一私钥 ⇒ 同一签名；这是 golden vector 可以逐字节断言的前提。
- `digest` 同时用作 HTTP `ETag`（第 6.3 节）和本地缓存的自检锚点；它**不**写进 bundle（避免"摘要覆盖自身"的歧义），由响应头与缓存元数据携带。

### 4.3 验签算法（顺序固定，短路返回原因码）

| 步 | 检查 | 失败原因码 |
|---|---|---|
| 1 | 原始字节 ≤ 1 MiB；否则不解析 | `size_exceeded` |
| 2 | 严格解码：`json.Decoder.DisallowUnknownFields()`，拒绝重复键 | `schema_invalid` |
| 3 | `schemaVersion == "1"` | `unsupported_schema` |
| 4 | 字段级校验（3.1/3.2 的格式、长度、effect、时间、唯一 id） | `schema_invalid` |
| 5 | 限额（条数 ≤1000 等，见第 10 节） | `policy_count_exceeded` / `schema_invalid` |
| 6 | 重新 canonicalize，计算 digest；若响应头带 `X-OpsMesh-Bundle-Digest` 则必须相等 | `digest_mismatch` |
| 7 | `keyId` 在允许公钥集合内（否则不尝试其他密钥） | `unknown_key_id` |
| 8 | `ed25519.Verify(pub, signingBytes, signature)` | `signature_invalid` |
| 9 | `tenantScope == 本机注册租户` | `tenant_mismatch` |
| 10 | 单调性/防降级（第 8 节）；同一 `bundleID` 不允许不同 digest | `downgrade_refused` / `equivocation_detected` |
| 11 | 过期分类（第 7.3 节）；过期本身不是"拒绝加载"，按模式降级 | `expired`（状态，非丢弃） |

**实现纪律**：

- 第 2 步的"拒绝重复键"不能用 `encoding/json` 的默认行为实现（标准库对重复键取最后一个）；必须先用 `json.Decoder.Token()` 做一遍 token 级预扫描，发现同一对象内键重复即失败。
- 第 8 步之前**不得**信任任何字段（包括 `keyId` 指向的公钥之外的元数据、时间、租户）。
- 失败时不得返回策略内容、不得打印 payload 片段；审计只记原因码、`bundleID`（若可解析）、`keyId`（若可解析）、计数。
- 第 7 步"未知 keyId 不尝试其他密钥"是防降级攻击：否则攻击者可换 `keyId` 指向弱/旧密钥。

---

## 5. 密钥管理

### 5.1 控制面（签发端）

| 配置 | flag | env | 说明 |
|---|---|---|---|
| 私钥路径 | `--abac-bundle-key` | `OPSMESH_ABAC_BUNDLE_KEY` | PKCS#8 PEM 的 Ed25519 私钥文件路径；权限必须 ≤0600（>0600 ⇒ 启动告警，`--abac-bundle-key-strict` 时拒绝启动） |
| 密钥 ID | `--abac-bundle-key-id` | `OPSMESH_ABAC_BUNDLE_KEY_ID` | 必填且与私钥同时配置；写入每个 bundle 的 `keyId` |

- 配置沿用 `internal/config/config.go:865-866` 的 `val(flag, value, env)` 模式（与 `--license-public-key` 同形）。
- 启动自检：用配置私钥签一份空策略 bundle 并自验；失败 ⇒ 拒绝启动（避免"服务起来了但所有 bundle 都签错"）。
- 私钥只允许出现在签发进程内存；**禁止**写日志、禁止进审计、禁止进 `/api/v1/admin/config` 转储（该端点已有的秘密脱敏规则必须覆盖新字段）。
- 生成本机测试密钥：`openssl genpkey -algorithm ed25519 -out abac-bundle.key`（不引入 Go 依赖）。

### 5.2 Agent（验签端）

| 配置 | 格式 | 说明 |
|---|---|---|
| `--abac-bundle-pubkeys` / `OPSMESH_ABAC_BUNDLE_PUBKEYS` | `keyId=path[,keyId=path]` | 最多 **2** 个条目；`path` 指向 PEM(SPKI) 或裸 32 字节（base64/hex）。解析复用 `config.ParsePublicKey` 的形态（`internal/config/license.go:135`），但**不复用授权语义** |
| `--abac-bundle-tenant` | string | agent 注册租户；缺省取注册响应中的租户，不允许由 bundle 反向覆盖 |

- agent 只持有公钥；启动时加载全部条目到 `map[keyId]ed25519.PublicKey`，条目 >2、`keyId` 重复、公钥长度非 32 字节 ⇒ 拒绝启动（`key_config_invalid`）。
- 未配置任何公钥 ⇒ 设备侧评估禁用（`bundle_disabled`），行为回退服务端评估。

### 5.3 轮换（双公钥并存）

**目标：任意时刻最多 2 个公钥，切换窗口内新旧 bundle 均可验证。**

1. **准备**：控制面生成新密钥对，`keyId=abac-2026-12`；新公钥随 agent 配置/发布通道下发（M1 为人工/配置管理，M2 起随 agent 版本包）。
2. **并存**：agent 同时配置 `abac-2026-11`（旧）与 `abac-2026-12`（新）。此时两个 keyId 都能验签。
3. **切换签发**：控制面把 `--abac-bundle-key` 与 `--abac-bundle-key-id` 切到新密钥；新 bundle 由新 keyId 签名。
4. **观察**：至少等待 `max(旧 bundle.expiresAt, 2×轮询周期)`，确认所有在线 agent 已接受新 keyId 的 bundle（指标：`opsmesh_abac_bundle_key_id{key_id}`）。
5. **摘除旧钥**：从 agent 配置删除旧公钥；控制面销毁旧私钥。此后旧 keyId 的 bundle 验签失败（`unknown_key_id`）。

**硬约束**：

- 不允许"先摘旧钥、后切新钥"——会产生无包可验的空窗。
- 同一 `tenantScope` 不得同时存在两个不同 `keyId` 的有效 bundle 被不同副本签发（多副本必须共享同一签发密钥）；M1 由部署配置保证，M2 起可在启动自检里比对 keyId。
- 吊销 = 从 agent 允许集合移除 keyId；**没有** CRL/OCSP，也不新增网络依赖。

---

## 6. 分发契约

### 6.1 端点

```
GET /api/v1/agent/abac-bundle
```

- 路由注册位置：`internal/controlplane/server_lifecycle.go` 的 `Start()`（与 `/api/v1/agents` 同层），handler 建议 `internal/controlplane/abac_bundle.go` 的 `handleAgentABACBundle`。
- 仅 `GET`；其他方法 `405`（`paginate.WriteJSON` 形态，与 `abac_ops.go` 一致）。
- 响应 `Content-Type: application/json`；body 为 bundle JSON（可以是 canonical 形式，ETag 只认 canonical digest）。
- 不接受任何 query 参数：租户由**已认证 agent 身份**决定，绝不接受 `?tenant=`。

### 6.2 Agent 认证（HTTP 通道）

gRPC 通道已有 per-agent HMAC 密钥（注册响应 `grpcx.RegisterResp.Secret`，`internal/grpcx/service.go:24`；agent 落盘 0600，`internal/agent/agent.go:223-252`）。HTTP 端点复用同一秘密，避免新增凭证体系：

| Header | 值 |
|---|---|
| `X-OpsMesh-Agent-ID` | 注册时分配的 agentID |
| `X-OpsMesh-Agent-Timestamp` | Unix 秒（十进制） |
| `X-OpsMesh-Agent-Signature` | `hex(HMAC-SHA256(secret, "abac-bundle\n" + agentID + "\n" + timestamp))` |

校验：

1. 三个头缺一 ⇒ `401`。
2. `|now - timestamp| > 300s` ⇒ `401`（防重放窗口）。
3. 按 `agentID` 取秘密；未知 agent ⇒ `401`（与签名错**同形**，不泄露 agent 是否存在）。
4. 常量时间比较签名（`hmac.Equal`）。
5. agent 记录被退役/禁用 ⇒ `403`；租户为空 ⇒ `401`（无租户的 agent 不可下发策略）。

> M1 只交付**服务端校验 + 契约测试**；agent 侧发送这些头属于 M2 的 agent 改动。M1 期间端点默认关闭（`--abac-bundle-endpoint=false`），打开即要求完整认证，不提供匿名回退。

### 6.3 响应与缓存头

| 情况 | 状态 | Body | Headers |
|---|---|---|---|
| 有可用 bundle | `200` | bundle JSON | `ETag: "sha256:<digest>"`、`X-OpsMesh-Bundle-Key-Id: <keyId>`、`X-OpsMesh-Bundle-Digest: <digest>`、`Cache-Control: no-cache` |
| `If-None-Match` 命中当前 ETag | `304` | 空 | 同 ETag；agent 继续用 LKG |
| agent 未声明 `abac_bundle=v1` | `409` | `{"error":"agent not capable of abac_bundle=v1","code":"agent_not_capable"}` | — |
| 未配置签发密钥 | `503` | `{"error":"abac bundle signing not configured","code":"signing_not_configured"}` | `Retry-After: 30` |
| 尚未生成过 bundle | `503` | `{"error":"abac bundle not ready","code":"bundle_not_ready"}` | `Retry-After: 30` |
| 限流 | `429` | `{"error":"too many requests","code":"rate_limited"}` | `Retry-After: <sec>` |
| 认证失败 | `401` | `{"error":"unauthorized"}` | — |
| 内部签名/序列化失败 | `500` | `{"error":"internal error"}` | — |

- **ETag 是强验证器**：同一 `tenantScope` + 同一策略快照 ⇒ 同一 digest ⇒ 同一 ETag。ETag 基于 canonical signing bytes，因此与 JSON 空白/字段顺序无关。
- `304` 的前提是 agent 本地 LKG 仍通过验签；如果本地文件被篡改，必须忽略 304 并重取（`If-None-Match` 只在本地 bundle 验签通过时发送）。
- 响应**不得**包含 `Cache-Control: public`；策略属租户敏感数据。

### 6.4 轮询与触发

| 触发 | 行为 |
|---|---|
| agent 启动 | 异步立即拉取一次（不阻塞注册/心跳） |
| 周期轮询 | 默认 **5 分钟**，±20% 抖动；`If-None-Match` 带 LKG 的 ETag |
| 失败退避 | 网络/`5xx`：1m → 2m → 4m → … → 上限 30m；`401/403`：停止轮询并告警（配置错误，重试无意义） |
| `409 agent_not_capable` | 停止轮询；回退服务端评估（混合版本舰队的正常路径） |
| 按需触发 | M1 预留接口 `bundleSyncNow()`；M3/M4 在"高风险任务下发前"调用（本里程碑不接线） |

- 不做长轮询/SSE；M4 如需"立即下发"另立契约。
- 轮询是**拉**模型：控制面不需要知道 agent 地址，天然适配 NAT/离线设备。

---

## 7. 本地缓存与离线行为

### 7.1 文件布局与原子写

| 路径（相对 agent 数据目录） | 内容 | 权限 |
|---|---|---|
| `abac/bundle-v1.json` | LKG（**原始响应字节**，不重新序列化） | `0600` |
| `abac/bundle-v1.prev.json` | 上一代 LKG（仅保留 1 份） | `0600` |
| `abac/bundle-v1.meta.json` | `{etag, keyId, digest, bundleID, issuedAt, expiresAt, receivedAt, serverDate}` | `0600` |
| `abac/` | 目录 | `0700` |

- **保存原始字节**：验签针对 canonical bytes，重序列化会引入实现差异；LKG 必须是"可再次验签的字节"。读取时先解析 → canonicalize → 验签，再使用。
- 原子切换：写 `*.tmp.<rand>`（0600）→ `fsync` → `rename` → `fsync(dir)`；失败则保持旧文件不变。
- 启动加载顺序：`bundle-v1.json` → 验签/租户/限额 → 失败则回退 `bundle-v1.prev.json` → 再失败则 `bundle_absent`。
- 缓存目录不跟随符号链接；解析后的真实路径必须仍在数据目录内（防路径逃逸）。

### 7.2 `evaluatedBy` 标记

| 值 | 含义 | 来源 |
|---|---|---|
| `server` | 服务端评估（无设备上下文） | 既有（`abac_ops.go`） |
| `server+device-context` | 服务端评估 + 设备属性注入 | 既有（批次45） |
| `device` | 设备本地评估，且最近一次同步成功 | M2 新增 |
| `device-offline` | 设备本地评估，使用 LKG，控制面不可达/最近同步失败 | M2 新增 |

**规则**：只要本次决策期间控制面拉取失败（包括 `5xx`、超时、`304` 但连接已断），标记必须是 `device-offline`；`device` 只用于"本周期同步确认过且包未过期"的路径。标记进审计（M3）与结构化日志，**不**改变 RBAC 强制结果。

### 7.3 过期语义与模式

| 模式 | 变更类动作（mutating） | 只读动作（read-only） | 建议 |
|---|---|---|---|
| `split`（**默认，推荐**） | **拒绝**（fail-closed） | 允许用过期 LKG，标记 `policyState=expired` | 推荐生产默认：写操作宁可停，读操作保可用 |
| `closed` | 拒绝 | 拒绝 | 高安全租户 / 合规要求场景 |
| `open` | 允许，标记 `policyState=expired` | 允许，同上 | **不建议**；需 owner 书面批准才能启用 |

- 变更类动作定义：`POST`/`PUT`/`PATCH`/`DELETE`；以及 shell/K8s/自动化等"执行类"动作（M2 的动作分类表落地）。只读：`GET`/`HEAD`。
- 过期判定：`now >= expiresAt`。允许 `--abac-bundle-expiry-skew`（默认 `5m`）容忍设备时钟偏差；超过 skew 仍过期。
- 时钟锚：拉取成功时记录 HTTP `Date` 响应头与本地单调时钟；若 `|local - serverDate| > 10m` 记 `clock_skew_detected` 审计，并以 `receivedAt + 单调流逝` 作为过期判定基准（设备时钟不可信，见章程 §8）。
- **从未同步成功**（`bundle_absent`）：设备端不产生任何 allow 判定；ABAC 输出"无意见"，决策回退服务端/RBAC。离线且 mutating ⇒ 记 `bundle_absent`，按 `split` 的失败侧处理（不放行）。
- 过期 bundle **不删除**：保留用于审计与 `open` 模式的显式启用；下一次成功的包会覆盖 LKG 并把旧包挪到 `prev`。

---

## 8. 单调性与回滚拒绝

### 8.1 排序键

```
order(bundle) = (parseRFC3339(issuedAt), bundleID)   // 元组比较，严格递增
```

- `issuedAt` 相同 ⇒ 按 `bundleID` 的字典序比较（UUIDv7 时间有序，天然近似单调）。
- 新包必须 **严格大于** 当前 LKG 的排序键；否则 `downgrade_refused`，保留 LKG。
- 同一 `bundleID` + 不同 digest ⇒ `equivocation_detected`（签发方行为异常或重放攻击），整包拒绝。
- 同一 `bundleID` + 相同 digest ⇒ 幂等重放，直接 `304`/忽略。

### 8.2 允许/拒绝矩阵

| 场景 | 结果 |
|---|---|
| 新 `issuedAt` 更大，签名有效 | 接受，旧 LKG 移入 `prev` |
| 新 `issuedAt` 更小（即便签名有效） | 拒绝（`downgrade_refused`） |
| `issuedAt` 相同、`bundleID` 字典序更大 | 接受 |
| `issuedAt` 相同、`bundleID` 字典序更小/相等 | 拒绝（`downgrade_refused`） |
| `bundleID` 相同、digest 不同 | 拒绝（`equivocation_detected`） |

- M1 **不实现**显式回滚指令。章程 §6 要求的"带签名的显式回滚"属于 M4；在 M4 落地前，任何降级（包括运维想回滚）都只能通过签发一个**新 `issuedAt` 的新包**实现——这样审计链不会倒退。
- `prev` 槽只用于"新包验签/自检失败时回退"，不用于绕过单调性。

---

## 9. 能力协商（混合版本舰队）

### 9.1 协议

- 心跳结构 `grpcx.HeartbeatReq`（`internal/grpcx/service.go:28`）**新增可选字段**：

  ```go
  Capabilities []string `json:"capabilities,omitempty"` // 例：["abac_bundle=v1"]
  ```

  该字段是**追加式**的：旧控制面忽略未知字段，旧 agent 不发送即视为"无能力"，双向兼容。
- 能力值：`abac_bundle=v1`（版本化，未来 v2 并列上报，例如 `["abac_bundle=v1","abac_bundle=v2"]`）。
- 控制面按 agent 记录能力集，TTL = `max(3 × heartbeatInterval, 60s)`；过期未刷新 ⇒ 视为不具备能力（防止 agent 降级/失联后仍被当作可下发对象）。

### 9.2 路由规则

| agent 能力 | 控制面行为 |
|---|---|
| 声明 `abac_bundle=v1` | 允许 `GET /api/v1/agent/abac-bundle`，返回本租户 bundle |
| 未声明 | `409 agent_not_capable`；该 agent 的决策由服务端评估器承担（`evaluatedBy=server`） |
| 声明但版本未知（如 `v2` only） | 同"未声明"；不猜测兼容性 |

- **混合版本舰队**：控制面永远保留服务端评估路径，bundle 只是可选加速/离线能力；任何 agent 不具备能力都不影响其被管理。
- 灰度控制（M4）在此之上再加标签选择；M1 只做能力门。

### 9.3 M1 边界

M1 **只实现服务端**的字段解析、能力存储与门控（可先以内存能力表 + 单元测试验证）；**agent 不发送该字段**，因此 M1 上线后所有 agent 都走"未声明 ⇒ 服务端评估"，行为与今天逐字一致。

---

## 10. 限额与安全约束

### 10.1 硬限额

| 项 | 上限 | 超限行为 |
|---|---|---|
| 响应/请求 body | **1 MiB** | `size_exceeded`；服务端用 `http.MaxBytesReader` 在解析前截断 |
| policies 条数 | **1000** | `policy_count_exceeded` |
| policy.id | 128 字节 / 全包唯一 | `schema_invalid` |
| subject/resource/action | 各 512 字节 | `schema_invalid` |
| conditions 条目 | 32 键；键 128 字节；值 1024 字节 | `schema_invalid` |
| 有效期 | ≤ 90 天 | `schema_invalid` |
| 单租户包数 | 每个 `tenantScope` 仅 1 个"当前包" | 生成器保证；多副本共享密钥 |
| 公钥条目 | 2 | agent 拒绝启动（`key_config_invalid`） |

### 10.2 禁止内容

- bundle 内**不得**出现秘密：私钥、token、密码、连接串、Cookie、Authorization。生成器只允许 3.2 节字段；严格解码拒绝未知字段，天然阻断"顺手塞配置"。
- 禁止在审计/日志中输出 payload 原文、条件值、subject 全量；只输出 `bundleID`、`keyId`、条数、digest 前缀（8 hex）。

### 10.3 攻击面与处理

| 攻击 | 处理 |
|---|---|
| 篡改 payload / 签名 / keyId / tenantScope | 验签失败 ⇒ 丢弃 + `bundle_rejected` 审计；LKG 不变 |
| 重放到旧包（降级） | §8 单调性拒绝 |
| 跨租户包注入 | `tenant_mismatch` 丢弃；agent 不按包内容改租户 |
| 超大包/超多策略（DoS） | §10.1 限额，解析前截断；拒绝加载而非"截断加载" |
| 未知字段注入 | `DisallowUnknownFields` ⇒ `schema_invalid` |
| 重复 JSON 键（`effect` 覆盖） | 解码层拒绝重复键 ⇒ `schema_invalid` |
| 私钥泄漏 | 双公钥轮换 + 摘除旧 keyId；审计 `key_removed` |
| 端点被匿名扫描 | 强制 HMAC；`401` 统一文案；限流 |

### 10.4 审计事件（M1 定义 schema，M3 落地上传）

| 事件 | 字段 |
|---|---|
| `bundle_accepted` | `bundleID, keyId, digest, policyCount, issuedAt, expiresAt, receivedAt, transport=online` |
| `bundle_rejected` | `reasonCode`（附录 A）、`bundleID?`、`keyId?`、`digestPrefix?`、`at` |
| `bundle_expired_used` | `bundleID, policyState=expired, mode, evaluatedBy=device-offline` |
| `downgrade_refused` / `equivocation_detected` | `currentBundleID, incomingBundleID, currentIssuedAt, incomingIssuedAt` |
| `clock_skew_detected` | `localTime, serverDate, deltaSeconds` |

---

## 11. 与服务端语义映射（生成器契约）

bundle 生成器（控制面）必须：

1. 从**已持久化**的策略存储读取该租户的策略（迁移 035，`model.ABACPolicyStore`）；不得从进程缓存直接打包（缓存可能落后于 store）。
2. 过滤：`p.TenantID == tenantScope || p.TenantID == ""`；`id/subject/resource/action/effect/condition` 1:1 映射到 bundle 字段；`effect` 只允许 allow/deny。
3. 稳定排序：按 `policy.id` 升序输出。
4. 计算 `bundleID`（UUIDv7 优先）、`issuedAt=now(UTC, 秒)`、`expiresAt=now+TTL`（默认 7 天，上限 90 天）。
5. 规范化 → 摘要 → 签名 → 返回**原始字节**并缓存（同一策略快照在 TTL 内复用同一 bundle，避免每次请求重新签名）。
6. 策略变更（create/delete）后使缓存失效，下次拉取生成新 `issuedAt` 的包。

> **摘要稳定性**：多副本控制面必须对同一策略快照生成**逐字节相同**的 canonical payload，否则 ETag 抖动会导致全舰队反复下载。保证手段：排序固定 + 时间字段来自同一份"快照元数据"（不是各副本各自 `now`）；实践上建议 bundle 由单一签发副本生成并共享（M1 可用共享密钥 + 同一快照时间；多副本一致性在 M4 复核）。

---

## 12. 参考验签库 `pkg/abacbundle`

放置于 `pkg/`（可被 agent 与外部工具复用，不依赖 `internal/`）。M1 只交付**格式与验签**，不含评估。

```go
package abacbundle

type Bundle struct {
    SchemaVersion string   `json:"schemaVersion"`
    BundleID      string   `json:"bundleID"`
    KeyID         string   `json:"keyId"`
    TenantScope   string   `json:"tenantScope"`
    IssuedAt      string   `json:"issuedAt"`
    ExpiresAt     string   `json:"expiresAt"`
    Policies      []Policy `json:"policies"`
    Signature     string   `json:"signature"`
}

type Policy struct {
    ID         string            `json:"id"`
    Subject    string            `json:"subject"`
    Resource   string            `json:"resource"`
    Action     string            `json:"action"`
    Effect     string            `json:"effect"`
    Conditions map[string]string `json:"conditions,omitempty"`
}

// Limits 为零值时使用文档默认值（1 MiB / 1000 条）。
type Limits struct{ MaxBytes int; MaxPolicies int }

type VerifyOptions struct {
    TenantScope string
    Now         time.Time
    Keys        map[string]ed25519.PublicKey
    Limits      Limits
}

type Reason string // 附录 A 的原因码

func Canonical(b *Bundle) ([]byte, error)                 // payload（不含 signature）
func SigningBytes(b *Bundle) ([]byte, error)              // 域前缀 + Canonical
func Digest(b *Bundle) ([32]byte, error)                  // SHA-256(SigningBytes)
func Sign(b *Bundle, priv ed25519.PrivateKey) error       // 填充 b.Signature
func Verify(raw []byte, opts VerifyOptions) (*Bundle, Reason, error)
```

- `Verify` 返回 `(bundle, reason, err)`：`reason==""` 表示结构与签名通过；过期通过 `bundle.Expired(now, skew)` 单独查询（过期不是"拒绝"）。
- 库内**禁止** `init()` 读环境/文件；配置由调用方注入，便于测试与 agent 复用。
- 错误信息不得包含 payload 内容。

---

## 13. 契约测试与共享 fixture

### 13.1 目录约定

| 路径 | 内容 | 使用方 |
|---|---|---|
| `pkg/abacbundle/testdata/vectors/*.json` | golden vectors：`{name, payload, canonical, digest, keyId, publicKeyPEM, signature}` | `pkg/abacbundle` 单测 |
| `pkg/abacbundle/testdata/fixtures/eval-cases.json` | 评估用例：`{name, policies[], request{tenant,subject,resource,action,context}, wantDecision, wantPolicyID}` | `internal/controlplane` 与 M2 设备评估器 |
| `pkg/abacbundle/testdata/fixtures/bundles/*.json` | 用于 M2/M3 的完整签名包样例（含过期、降级、坏签名） | M2/M3 |

> fixture 放在 `pkg/` 是为了让 `internal/controlplane` 能引用（go 的 `internal` 规则禁止反向依赖）。**服务端与设备端共用同一份 fixture**，不得各写一套。

### 13.2 必测用例

| # | 用例 | 断言 |
|---|---|---|
| T1 | canonical 逐字节 | 两次运行/两个进程输出完全一致；无空白；键排序正确 |
| T2 | 签名可复现 | 同一 payload+私钥 ⇒ 同一签名（Ed25519 确定性） |
| T3 | 篡改检测 | payload 任一字节、签名任一字节、`keyId`、`tenantScope` 改动 ⇒ 对应原因码 |
| T4 | 轮换 | 双公钥下新旧 keyId 均通过；摘除旧钥后旧包 `unknown_key_id` |
| T5 | 过期模式 | `split`/`closed`/`open` 三模式 × 读写两类的 6 组行为符合 7.3 表 |
| T6 | 降级拒绝 | §8.2 五种场景逐条断言；LKG 不被覆盖 |
| T7 | 限额 | 1 MiB+1 字节、1001 条策略、重复 id、未知字段 ⇒ 拒绝 |
| T8 | ETag | 同内容同 ETag；`If-None-Match` ⇒ 304；内容变 ⇒ ETag 变 |
| T9 | 端点认证 | 无头/过期时间戳/错签名/未知 agent ⇒ 统一 401；能力缺失 ⇒ 409 |
| T10 | 语义一致 | `eval-cases.json` 在服务端 `evaluateABAC` 与参考评估器上逐条相同（0 差异） |

---

## 14. M1 DoD（退出判据）

1. `pkg/abacbundle` 实现 `Canonical` / `Sign` / `Verify` / `Digest`，`go vet ./pkg/abacbundle` 与 `go test -race ./pkg/abacbundle` 通过。
2. golden vectors 字节稳定（T1/T2）；canonical 输出无空白、键有序。
3. 篡改包被拒（T3）：修改 payload/签名/`keyId`/`tenantScope` 任一字节 ⇒ 设备侧拒绝加载、保留 LKG、产生 `bundle_rejected`。
4. 轮换契约（T4）：双公钥并存期新旧包均可验；摘除旧钥后旧包被拒。
5. 过期策略按模式生效（T5）：`split` 对 mutating 拒绝、对 read-only 放行并带 `policyState=expired`；`closed` 全拒；`open` 仅经显式配置启用。
6. 降级拒绝（T6）：低于当前排序键的包（即使签名有效）被拒。
7. **与服务端共享 fixture 的契约测试**：`eval-cases.json` 至少覆盖 default-deny、allow、deny-overrides、条件、通配/前缀、租户隔离、坏 JSON context；服务端评估器与参考实现 0 差异；打包/解包往返不丢策略字段（`condition` ⇄ `conditions`）。
8. 端点契约（T7–T9）：`200/304/401/409/429/503` 全覆盖；body 上限生效；错误体不含策略内容。
9. **无新依赖**：`go.mod` 的 `require` 集合不变（`go mod tidy` 无 diff）。
10. **无 agent 改动**：M1 的 `git diff --stat -- internal/agent` 为空；M1 上线后生产行为与今天逐字一致（全部 agent 未声明能力 ⇒ 服务端评估）。
11. 文档：章程 §5.1/§10 与本规格一致；`docs/projects/README.md` ABAC 行状态为“已立项（M1 进行中）”并链接本文件。
12. 收工：`python scripts/coord-gate.py` 通过；`[DONE]` 回执按 `AGENTS.md` 追加（不含修订历史重写）。

---

## 15. 交付物清单（文件落点）

| 文件 | 内容 | M1 必交 |
|---|---|---|
| `pkg/abacbundle/bundle.go` | 类型、限额、字段校验 | 是 |
| `pkg/abacbundle/canonical.go` | 确定性 JSON 编码器 | 是 |
| `pkg/abacbundle/sign.go` | 签名、摘要、域前缀 | 是 |
| `pkg/abacbundle/verify.go` | 验签顺序、原因码、单调性辅助 | 是 |
| `pkg/abacbundle/{canonical,verify,rotation,limits}_test.go` | T1–T7 | 是 |
| `pkg/abacbundle/testdata/vectors/*.json` | golden vectors | 是 |
| `pkg/abacbundle/testdata/fixtures/eval-cases.json` | 与服务端共享的评估用例 | 是 |
| `internal/controlplane/abac_bundle.go` | 生成器 + `handleAgentABACBundle` + ETag | 是 |
| `internal/controlplane/abac_bundle_test.go` | T8–T9 + 生成器稳定性 | 是 |
| `internal/config/config.go` | `--abac-bundle-*` flag/env（`val()` 模式） | 是 |
| `internal/controlplane/server_lifecycle.go` | 路由注册（默认关闭开关） | 是 |
| `internal/grpcx/service.go` | `HeartbeatReq.Capabilities` 追加字段 | 是（仅字段，不改 agent） |
| `cmd/abacbundle/` | `keygen` / `sign` / `verify` 调试 CLI | 否（可选） |

---

## 16. 未决项（不阻塞 M1 格式冻结）

| # | 问题 | 归属 |
|---|---|---|
| Q1 | 生产私钥托管位置、轮换周期、销毁流程（D3） | owner 决策 |
| Q2 | 多副本控制面的 bundle 生成是否收敛到单副本/共享快照 | M4 复核（M1 以部署约束规避） |
| Q3 | 设备时钟不可信时的过期基准是否全面改用 `receivedAt + 单调时钟` | M2 实测后定 |
| Q4 | `open` 过期模式是否允许在生产启用 | owner 决策（默认禁止） |
| Q5 | M4 显式回滚指令的签名格式与审批链 | M4 设计 |
| Q6 | bundle 中是否需要 `policyCount` 冗余字段（用于快速审计） | M3 审计 schema 时定（v1 不加，避免重复真值） |

---

## 附录 A：原因码

| 原因码 | 触发 | 处理 |
|---|---|---|
| `size_exceeded` | body > 1 MiB | 丢弃，不解析 |
| `schema_invalid` | JSON 结构/字段/时间/枚举/重复键/未知字段非法 | 丢弃 |
| `unsupported_schema` | `schemaVersion != "1"` | 丢弃 |
| `policy_count_exceeded` | policies > 1000 | 丢弃 |
| `digest_mismatch` | 重算摘要与响应头不一致 | 丢弃 |
| `unknown_key_id` | `keyId` 不在允许集合 | 丢弃 |
| `signature_invalid` | Ed25519 验签失败 | 丢弃 |
| `tenant_mismatch` | `tenantScope != 本机租户` | 丢弃 + 告警（疑似跨租户投放） |
| `key_id_header_mismatch` | 响应头 keyId 与签名字段不一致 | 丢弃 |
| `downgrade_refused` | 排序键不递增 | 丢弃，保留 LKG |
| `equivocation_detected` | 同 `bundleID` 不同 digest | 丢弃 + 告警 |
| `bundle_absent` | 无任何可用 LKG | 回退服务端评估 |
| `bundle_disabled` | agent 未配置公钥 | 回退服务端评估 |
| `key_config_invalid` | 公钥条目 >2/重复/长度非法 | agent 拒绝启动 |
| `signing_not_configured` | 控制面无签发密钥（HTTP 503） | agent 退避重试 |
| `bundle_not_ready` | 尚无包可发（HTTP 503） | agent 退避重试 |
| `agent_not_capable` | 未声明 `abac_bundle=v1`（HTTP 409） | 停止轮询，服务端评估 |
| `expired` | 包已过期（状态，非丢弃） | 按 7.3 模式降级 |
| `clock_skew_detected` | 本地时钟与 `Date` 偏差 >10m | 审计，改用服务端锚点 |

## 附录 B：签名/验签伪代码

```text
# 签发（控制面）
payload   = bundle - signature
canonical = CanonicalJSON(payload)                  # 4.1 规则
bytes     = "opsmesh-abac-bundle-v1\n" + canonical
digest    = SHA256(bytes)
signature = base64url_raw(Ed25519Sign(priv, bytes))
bundle.signature = signature
etag      = "sha256:" + hex(digest)

# 验签（agent / 参考库）
raw -> strict JSON decode -> bundle
canonical = CanonicalJSON(bundle - signature)
bytes     = "opsmesh-abac-bundle-v1\n" + canonical
digest    = SHA256(bytes)
assert headerDigest == "" || headerDigest == hex(digest)
pub       = keys[bundle.keyId]        # 未命中 => unknown_key_id，不尝试其他键
assert Ed25519Verify(pub, bytes, base64url_raw_decode(bundle.signature))
assert bundle.tenantScope == localTenant
assert order(bundle) > order(current) # 除非 bundle 与 current 完全等价
```

---

*规格起草：2026-11-17。只读核对基准 `main@88ff295c`；本文件为设计文档，未改代码、未提交。*
