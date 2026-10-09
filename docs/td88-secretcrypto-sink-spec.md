# `enc:v1:` 加密原语下沉规格（TD-88 前置，供并行线执行）

> **性质**：可执行规格，非决策请求。本文只做设计与步骤定义；实施需 `services/config-svc` 与 `internal/controlplane` 的负责方（并行线）确认后执行——本线按要求未碰其文件。
> **基准**：`f03fb75`（2026-10-10 main 顶点）。文中所有 `file:line` 均可在该基准复现。

---

## 1. 为什么要下沉

同一职责已经有**两份同形态实现**，且 TD-88 还要引入第三处：

| 实现 | 位置 | 状态 |
|---|---|---|
| config-svc 加密对 | `services/config-svc/internal/store/store.go:98` `encryptSecret` / `:119` `decryptSecret` | TD-65 刚修（内存与 MySQL 两后端共用） |
| controlplane kubeconfig 加密对 | `internal/controlplane/k8s_cluster.go:43` `encryptKubeconfig` / `:74` `decryptKubeconfig` | 早于 TD-65，形态相同但**无版本前缀、无 legacy 语义** |
| TD-88 `secrets` 表 | 尚未动工（见 `docs/td88-secret-at-rest-recon.md`） | 将会是第三份 |

config-svc 的注释自己写明了戒条（`store.go:96-97`）：「TD-61 的教训：同一职责各写一份必然漂移——config-svc 的 MySQL 后端曾把值原样落库」。**在下沉之前新增第三处，等于主动复刻该缺陷。**

---

## 2. 现状实测对照（差异即设计输入）

| 维度 | config-svc | controlplane k8s |
|---|---|---|
| 算法 | AES-256-GCM，随机 nonce 前置，base64 | 同 |
| 版本前缀 | **有** `enc:v1:`（`store.go:91`） | **无** |
| 密钥形状 | **passphrase** → `deriveKey` = SHA-256 → 32 字节（`store.go` `deriveKey`） | **base64 解码得 32 原始字节**（`server.go:466-476`，校验长度 32） |
| 解密返回 | **三元** `(plaintext, legacy, err)`：无前缀⇒legacy 原样返回；有前缀解不开⇒硬失败 | **二元** `(plaintext, err)`：无「legacy/被篡改」区分 |
| 空密钥行为 | 内存后端退化为进程内随机密钥（`mysql.go:49-51` 的 `ephemeral-…`） | 明文透传 + 启动告警（`server.go:477-479`） |
| 存量兼容 | 有（legacy 分支） | 无（无前缀 ⇒ 无法区分密文与明文） |

**三个关键结论**：
1. **密钥派生绝不能被下沉**：两侧密钥形状不同（passphrase-SHA256 vs base64-raw）。下沉层只接受 `[]byte`（32 字节）。
2. **格式必须与 config-svc 逐字节一致**（同前缀、同 nonce 前置、同 base64），否则 config-svc 已落库的密文读不出来。
3. **controlplane 的存量是第三类数据**：「无前缀但确是 base64 密文」。若直接用三元 `decrypt` 读它，会被判为 legacy **明文**并原样返回 ⇒ 把一段 base64 乱码当 kubeconfig 交给 client-go。**必须为此设计迁移路径**（§4.3）。

---

## 3. 下沉方案：新包 `pkg/secretcrypto`

`pkg/security` 是 HTTP 中间件（XSS/限流/头/CORS），不是加密原语的家；本仓 `pkg/` 一包一职（circuit/compress/cron/discover/provision/retry…），故新建。

```go
// Package secretcrypto — 机密「静态加密」单一实现（AES-256-GCM + enc:v1: 版本前缀）。
//
// 只做算法与格式，不做密钥管理：密钥形状（passphrase 派生 or base64 raw）
// 由各消费方自定，本包只接受 32 字节 []byte。
package secretcrypto

// Prefix 标记本包产出的密文并带格式版本号（便于日后换算法时区分存量）。
const Prefix = "enc:v1:"

// Encrypt 加密：AES-256-GCM，随机 nonce 前置，base64，带 Prefix。
// key 必须 32 字节；plaintext 为空串时返回空串（不加密空值）。
func Encrypt(key []byte, plaintext string) (string, error)

// Decrypt 解出机密明文。
//   stored 无 Prefix            ⇒ (stored, true, nil)   // 升级前明文存量：放行并提示轮换
//   stored 有 Prefix 且解不开    ⇒ ("", false, err)      // 密钥不匹配/数据损坏：必须硬失败
//   stored 有 Prefix 且解密成功  ⇒ (plaintext, false, nil)
// 返回 legacy=true 时调用方应记录告警并提示轮换（不得静默）。
func Decrypt(key []byte, stored string) (plaintext string, legacy bool, err error)

// DecryptLegacyUnprefixed 专供「无前缀但确是 base64(nonce||ct)」的存量密文
// （controlplane kubeconfig 在切换本格式前的数据）。成功 ⇒ 调用方应立即
// 用 Encrypt 重写回库（机会式迁移），失败 ⇒ 按明文处理并报错。
func DecryptLegacyUnprefixed(key []byte, stored string) (plaintext string, ok bool)

// HasPrefix 判断 stored 是否为本包格式（便于调用方分流，避免重复字符串比较）。
func HasPrefix(stored string) bool
```

**语义红线（承接 config-svc 现有设计，写在包注释里）**：绝不做「试解密失败就当作明文」——那会把「密钥不匹配/数据损坏」与「历史明文存量」混为一谈，真实事故里的表现是**密文被原样当明文返回给调用方**。

---

## 4. 消费方迁移步骤

### 4.1 config-svc（`services/config-svc/internal/store/store.go`）
1. `encryptSecret`/`decryptSecret`/`secretCipherPrefix`/`deriveKey` **整体删除**，改为转发：
   `store.go:98` → `secretcrypto.Encrypt(s.encryptionKey, plaintext)`；
   `store.go:119` → `secretcrypto.Decrypt(s.encryptionKey, stored)`（三元语义原样透传，调用点已按 legacy 分支处理）。
2. `deriveKey` 保留在 config-svc（密钥形状是该侧策略，不下沉）。
3. **存量兼容**：格式逐字节一致 ⇒ 已落库密文无需迁移。
4. 保留现有测试；`mysql_integration_test.go:110-111/135-140/160` 三段断言（落库带前缀 / 历史明文可读 / 轮换后仍密文）继续有效。

### 4.2 controlplane k8s（`internal/controlplane/k8s_cluster.go`）
1. 写路径：`encryptKubeconfig` 改为 `secretcrypto.Encrypt(s.encryptionKey, plaintext)`；空串透传与空密钥明文透传的行为**保持不变**（`k8s_cluster.go:44-49`，非生产 demo 兼容）。
2. 读路径改为**三段分流**：
   ```go
   if !secretcrypto.HasPrefix(stored) {
       // 第三类存量：无前缀但是 base64 密文（切换前写入的）
       if pt, ok := secretcrypto.DecryptLegacyUnprefixed(s.encryptionKey, stored); ok {
           _ = rewriteAsPrefixed(...)   // 机会式迁移：立即用 Encrypt 重写回库
           return pt, nil
       }
       return stored, nil               // 确系明文存量
   }
   pt, _, err := secretcrypto.Decrypt(s.encryptionKey, stored)  // 有前缀：解不开必须硬失败
   ```
3. **机会式迁移**（重写回库）需要在 store 侧有一个「按 id 更新 kubeconfig」的入口；若当前 store 无此入口，则退化为「只读兼容、不重写」，并在代码注释与台账登记「存量将长期停留在旧格式」。**这是需要并行线确认的一点**（§6）。
4. 修复后 controlplane 才拥有 legacy/被篡改区分能力——这是它目前缺失的（`decryptKubeconfig` 二元返回）。

### 4.3 TD-88 `secrets` 表（未来，不在本规格执行范围）
届时直接 `secretcrypto.Encrypt(s.encryptionKey, ...)`（控制面已有生产强制的 32 字节密钥，见 TD-88 侦察 §8），**不得**再写第四份实现。

---

## 5. 防漂移守卫（必须与原语同批落地）

1. **包级单测**（`pkg/secretcrypto/secretcrypto_test.go`）：
   - 往返一致；输出必带 `Prefix`；空串透传；
   - 无前缀 ⇒ `legacy=true` 且原样返回；
   - **篡改密文一个字节 ⇒ 必须 error**（GCM 验签，承接 `secret_crypto_test.go:76` 的判据）；
   - 错误长度 key（非 32 字节）⇒ error；
   - `DecryptLegacyUnprefixed` 对「真 legacy 明文」返回 `ok=false`（不误吞）。
2. **「不得出现第二份实现」静态守卫**（放在新包测试里，仿本仓 schema-drift 守卫的静态扫描风格）：
   扫描全仓 `*.go`（排除 `_test.go` 与新包自身）中 `aes.NewCipher(` / `cipher.NewGCM(` 的出现次数，**超过新包自身的实现数即判红**，并在错误信息里点名文件与行号。
   这是把「TD-61 教训」从注释变成机器判据——否则下一处还会被静默加出来。

---

## 6. 需要并行线确认的三点

1. **机会式迁移是否有 store 入口**（§4.2-3）：若有，切换后存量自动升级为带前缀格式；若无，登记为「长期兼容旧格式」。
2. **config-svc 的空密钥降级策略是否保留**（`mysql.go:49-51` 的 `ephemeral-<nano>-<dsn>`）：建议保留但**只在非生产**，若并行线愿意借本次一并收紧，需单独一轮（涉及部署行为变更）。
3. **新包放置确认**：`pkg/secretcrypto`（本规格推荐）还是并入某个既有包。若选后者，§5 的静态守卫要同步改路径。

---

## 7. 验证清单（实施后按序执行）

```bash
# 1) 新包单测（含防漂移守卫）
go test ./pkg/secretcrypto/... -v
# 2) 两个消费方模块
go test ./services/config-svc/...            # 现存 enc:v1: 断言必须仍绿
go test ./internal/controlplane/...          # k8s kubeconfig 脱敏/往返用例必须仍绿
# 3) 全仓
go build ./... && go vet ./... && gofmt -l .
# 4) 存量兼容（真库，需 DSN）：DB 里预置一条**旧无前缀** kubeconfig 密文与一条明文，
#    读路径应分别走「legacy 解出」与「明文放行」，且被篡改的带前缀密文必须硬失败。
# 5) 门禁
bash deploy/scripts/validate-deploy-assets.sh
```

**验收判据**：上述 1–5 全绿，且 `git diff` 中 `services/config-svc` 与 `internal/controlplane` 不再出现 `aes.NewCipher`/`cipher.NewGCM` 的实现代码（只剩测试与透传调用）。

---

## 8. 风险与回滚

- **最大风险**是格式不一致导致 config-svc 存量读不出 ⇒ 缓解：新包先按「与 config-svc 逐字节一致」实现，并用「旧实现 vs 新包」对同一明文做**交叉验证**（旧 `encryptSecret` 的输出能被新 `Decrypt` 解开，反之亦然）后才删除旧代码。**这一步不可跳过。**
- 回滚：原语下沉是纯内部重构，对外行为不变（同算法同格式）；任一步失败只需 revert 对应提交，不影响数据。
