// Package secretcrypto — 机密「静态加密」的**单一实现**（AES-256-GCM + enc:v1: 版本前缀）。
//
// # 为什么合并
//
// 同一职责曾有三处同形态实现：config-svc 的 encryptSecret/decryptSecret（TD-65 新建）、
// controlplane 的 encryptKubeconfig/decryptKubeconfig（更早、无版本前缀与 legacy 语义），
// 以及 TD-88 的 secrets 表（将会是第三份）。config-svc 的注释自己写着戒条：
// 「TD-61 的教训：同一职责各写一份必然漂移」——在新增第三处之前先把地基定死，
// 避免再次出现「MySQL 后端把机密原样落库」那类只在一份实现里发生的缺陷。
//
// # 边界：只做算法与格式，不做密钥管理
//
// 两侧密钥**形状不同**（config-svc 是 passphrase→SHA-256；controlplane 是 base64→32 原始字节），
// 派生策略是各方自己的部署语义，**不下沉**。本包只接受 32 字节 []byte。
//
// # 语义红线
//
// **绝不做「试解密失败就当作明文」**：那会把「密钥不匹配/数据损坏」与「升级前的明文存量」
// 混为一谈，真实事故里的表现是**密文被原样当明文返回给调用方**（本仓在 SSRF 判定与
// 机密读取上都定过同类判据）。三态返回（plaintext / legacy / err）就是这条红线的形状。
//
// 格式（必须与 config-svc 逐字节一致，否则其存量密文读不出来）：
// Prefix + base64(nonce || GCM(ciphertext+tag))，nonce 随机、前置。
package secretcrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

// Prefix 标记本包产出的密文并带格式版本号（便于日后换算法时区分存量）。
const Prefix = "enc:v1:"

// keyLen AES-256 要求的密钥长度。
const keyLen = 32

// HasPrefix 判断 stored 是否为本包格式（调用方分流用，避免各自重复字符串比较）。
func HasPrefix(stored string) bool { return strings.HasPrefix(stored, Prefix) }

// Encrypt 加密：AES-256-GCM，随机 nonce 前置，base64，带 Prefix。
//
//   - key 必须 32 字节（非 32 ⇒ error，不静默降级）；
//   - plaintext 为空串 ⇒ 返回空串（空值不加密：空机密没有「静态加密」可言，
//     且空串透传让上层无需为「未配置」造特例）。
func Encrypt(key []byte, plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	// Seal 把 nonce 作为 dst 前缀追加：nonce || ciphertext || tag。
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return Prefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt 解出机密明文。
//
//	stored 无 Prefix            ⇒ (stored, true, nil)  // 升级前的明文存量：放行并提示轮换
//	stored 有 Prefix 且解不开    ⇒ ("", false, err)     // 密钥不匹配/数据损坏：必须硬失败
//	stored 有 Prefix 且解密成功  ⇒ (plaintext, false, nil)
//
// 返回 legacy=true 时调用方应记录告警并提示轮换（不得静默）。
func Decrypt(key []byte, stored string) (plaintext string, legacy bool, err error) {
	if stored == "" {
		return "", false, nil
	}
	raw, ok := strings.CutPrefix(stored, Prefix)
	if !ok {
		return stored, true, nil
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", false, err
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", false, fmt.Errorf("密文 base64 解码失败: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", false, fmt.Errorf("密文长度 %d 小于 nonce 长度 %d", len(data), nonceSize)
	}
	out, err := gcm.Open(nil, data[:nonceSize], data[nonceSize:], nil)
	if err != nil {
		return "", false, fmt.Errorf("GCM 解密失败（密钥不匹配或数据被篡改）: %w", err)
	}
	return string(out), false, nil
}

// DecryptLegacyUnprefixed 解「无前缀但确是 base64(nonce||ct)」的历史密文。
//
// 专供带版本前缀之前的存量（如 controlplane 的 kubeconfig：切换本格式前写入的数据）。
// ok=true 时调用方**应立即用 Encrypt 重写回库**（机会式迁移），随后按新格式读；
// ok=false 表示该值不是本密钥能解开的旧密文（大概率本就是明文），调用方按明文处理。
//
// 为什么需要它：这类值既没有前缀（三元 Decrypt 会判定为 legacy 明文原样返回），
// 又确实是密文——直接用三元 Decrypt 会把一段 base64 乱码当明文交给调用方。
func DecryptLegacyUnprefixed(key []byte, stored string) (plaintext string, ok bool) {
	if stored == "" || HasPrefix(stored) {
		return "", false
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", false
	}
	data, err := base64.StdEncoding.DecodeString(stored)
	if err != nil {
		return "", false
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", false
	}
	out, err := gcm.Open(nil, data[:nonceSize], data[nonceSize:], nil)
	if err != nil {
		return "", false
	}
	return string(out), true
}

// newGCM 构造 AES-256-GCM；key 长度不合法即报错（不静默降级为弱算法/明文）。
func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != keyLen {
		return nil, fmt.Errorf("密钥长度 %d，须为 %d 字节（AES-256）", len(key), keyLen)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
