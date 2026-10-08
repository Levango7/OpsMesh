// Package storekit 存储层中性工具（TD-61「后端拆包」的共享内核）。
//
// # 为什么存在
//
// internal/store 原先是「两个中心类型（MemoryStore / SQLStore）的方法集 + 一批被两端
// 共用的自由函数」的混装包：token 签名（HashToken / VerifyTokenMAC）、随机串与 bcrypt
// 包装（RandHex / MustRandHex / BcryptHash / RandAlertRuleID）、设备指标环形缓冲
// （MetricsRing）与内存驻留上限（AppendAgentLogBounded 等）既被 memory_*.go 调用，
// 也被 sql_*.go / multi_schema.go 调用。后端按包拆分（memory/ 与 sqlstore/）后，
// 这些符号必须位于双方都能 import 且互不依赖的中性层，否则子包只能反向 import
// 父包（成环，编译期硬失败）。
//
// # 边界约定
//
// 本包只放「无状态工具 + 一个自持内存结构（MetricsRing）」，不 import
// internal/store 的任何子包，也不感知租户/领域语义。
// 并发约定：MetricsRing 自身无锁，由持有它的 store 结构体统一以其 mu 保护
// （调用方持锁调用，与本包从 memory.go / sql_devices.go 搬入前的语义完全一致）。
package storekit

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// BcryptHash 包装 bcrypt.GenerateFromPassword，避免调用方直接依赖 bcrypt 包
// （集中在此函数便于后续替换哈希算法或调整成本因子）。
// 成本因子默认 bcrypt.DefaultCost（10），兼顾安全与性能。
//
// 测试加速：CI -race 全量跑 400+ 测试时每个测试都 NewMemoryStore → seedRBAC
// → 3 次 bcrypt cost=10，纯哈希开销可达 120s+ 导致超时。
// 测试进程可设 OPSMESH_TEST_BCRYPT_COST=4（仅测试用，生产不设置），
// 将 seed 成本降至 MinCost 附近，大幅提速且不改变哈希格式/校验语义。
func BcryptHash(password string) (string, error) {
	cost := bcrypt.DefaultCost
	if v := os.Getenv("OPSMESH_TEST_BCRYPT_COST"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= bcrypt.MinCost && n <= 31 {
			cost = n
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// RandHex 返回 n 字节的十六进制随机串（crypto/rand，密码学安全）。
// 用于 nonce：熵失败时回退时间戳（降熵但可容忍）并打印告警。
func RandHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		log.Printf("[store] 警告：crypto/rand 熵源不可用（%v），回退时间戳派生 nonce——熵降级", err)
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// MustRandHex 返回 n 字节的十六进制随机串，熵失败时 panic（用于 HMAC secret）。
// 安全（F11）：secret 必须密码学安全，不可静默降级为时间戳。
func MustRandHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// log.Panicf 合理：crypto/rand 不可用意味着系统随机数生成器故障，
		// 无法生成安全的 HMAC 签名密钥，这是不可恢复的系统级错误。
		log.Panicf("[store] crypto/rand 不可用，无法生成安全密钥: %v", err)
	}
	return hex.EncodeToString(b)
}

// HashToken 对完整 token 取 SHA-256 摘要（hex）。
// 安全：库存/内存只存摘要，不存明文 token——DB 只读账号/备份泄露不等于活体 token 泄露。
func HashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// VerifyTokenMAC 校验 token 的 HMAC 签名（F8 真正落地）：从 token 中提取 payload + 签名，
// 用 secret 重算 HMAC-SHA256 并与签名部分比较。防 DB 写权限伪造 token。
func VerifyTokenMAC(secret, token string) bool {
	if secret == "" || token == "" {
		return false
	}
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	sigHex, payload := parts[0], parts[1]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sigHex))
}

// RandAlertRuleID 生成随机告警规则 ID（16 字节十六进制，crypto/rand 密码学安全）。
// 用于 CreateAlertRule 分配 ID（调用方未填 ID 时）。
func RandAlertRuleID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 熵源失败回退时间戳（降级但可容忍，唯一性由 alertRules map key 兜底）。
		return fmt.Sprintf("alert-rule-%d", time.Now().UnixNano())
	}
	return "alert-rule-" + hex.EncodeToString(b)
}
