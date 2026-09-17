// devicefp.go — 设备指纹采集与校验（TD-60 安全增强）。
//
// 设备指纹（DeviceFP）由 UA + IP + TLS 指纹合成 SHA-256 hash，用于：
//   - 登录时绑定到 access/refresh token（防 token 跨设备重放）
//   - 未知设备触发二次验证（首次登录设备需 MFA/邮箱确认）
//   - 设备指纹存储在 Redis（已知设备集合），降级为内存（单副本）
//
// 设计原则：
//   - 客户端可通过 X-Device-FP 头显式传入预计算指纹（优先）
//   - 未传入时服务端从请求采集（UA + IP + TLS 指纹合成）
//   - TLS 指纹（JA3/JA4）需代理层注入（X-TLS-Fingerprint 头），MVP 无 TLS 终止时为空
//   - Redis 不可用时降级为进程内已知设备集合（单副本限制，与 loginGuard 同语义）
package http

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/services/auth-svc/internal/cache"
)

// deviceFPManager 管理设备指纹采集、已知设备存储与未知设备检测。
//
// Redis 可用时：已知设备集合存 Redis（多副本共享，key=devicefp:{userID}:{fp}）。
// Redis 不可用时：降级为进程内 map（单副本，与 loginGuard 同已知限制）。
type deviceFPManager struct {
	cache   *cache.Cache // Redis 缓存（可为 nil=纯内存模式）
	enabled bool         // 设备指纹校验是否启用（默认 true，测试可关闭）
	knownMu sync.Mutex
	known   map[string]map[string]time.Time // userID → {fp → firstSeen}（内存降级）
}

// newDeviceFPManager 构造设备指纹管理器。
// c 为 Redis 缓存实例（可为 nil 或 disabled → 纯内存模式）。
// enabled 控制是否启用未知设备二次验证（false 时所有设备放行，测试环境用）。
func newDeviceFPManager(c *cache.Cache, enabled bool) *deviceFPManager {
	return &deviceFPManager{
		cache:   c,
		enabled: enabled,
		known:   make(map[string]map[string]time.Time),
	}
}

// collectDeviceFP 从请求采集设备指纹。
//
// 优先级：
//  1. X-Device-FP 头（客户端预计算，最精确）
//  2. 服务端合成：SHA-256(UA + IP + TLS-FP)
//
// TLS 指纹从 X-TLS-Fingerprint 头读取（由代理层注入，如 APISIX/Envoy 的 JA3 插件）。
// 无 TLS 指纹时仅用 UA + IP（MVP 直连场景）。
func collectDeviceFP(r *http.Request) string {
	// 优先使用客户端显式传入的指纹。
	if fp := strings.TrimSpace(r.Header.Get("X-Device-FP")); fp != "" {
		return fp
	}
	// 服务端合成。
	ua := r.Header.Get("User-Agent")
	ip := clientIP(r)
	tlsFP := strings.TrimSpace(r.Header.Get("X-TLS-Fingerprint"))
	return computeDeviceFP(ua, ip, tlsFP)
}

// computeDeviceFP 由 UA + IP + TLS 指纹合成设备指纹 hash。
// 输入拼接后 SHA-256，hex 编码（64 字符）。
func computeDeviceFP(ua, ip, tlsFP string) string {
	h := sha256.New()
	h.Write([]byte(ua))
	h.Write([]byte{0x1f}) // 分隔符（防 UA+IP 拼接歧义）
	h.Write([]byte(ip))
	h.Write([]byte{0x1f})
	h.Write([]byte(tlsFP))
	return hex.EncodeToString(h.Sum(nil))
}

// isKnownDevice 检查设备指纹是否为该用户的已知设备。
//
// Redis 可用时查 Redis（key=devicefp:{userID}:{fp}）；
// 不可用时查进程内 map。
// 返回 true=已知设备（放行），false=未知设备（触发二次验证）。
func (m *deviceFPManager) isKnownDevice(userID, fp string) bool {
	if fp == "" || userID == "" {
		return true // 空指纹视为已知（向后兼容旧客户端）
	}
	if m.cache != nil && m.cache.Enabled() {
		key := "devicefp:" + userID + ":" + fp
		var v bool
		if m.cache.Get(key, &v) {
			return v
		}
		return false
	}
	// 内存降级。
	m.knownMu.Lock()
	defer m.knownMu.Unlock()
	devices, ok := m.known[userID]
	if !ok {
		return false
	}
	_, known := devices[fp]
	return known
}

// registerDevice 注册设备指纹为该用户的已知设备。
//
// Redis 可用时写 Redis（TTL=90 天，长期未登录设备自动清理）；
// 不可用时写进程内 map。
func (m *deviceFPManager) registerDevice(userID, fp string) {
	if fp == "" || userID == "" {
		return
	}
	if m.cache != nil && m.cache.Enabled() {
		key := "devicefp:" + userID + ":" + fp
		m.cache.SetWithTTL(key, true, 90*24*time.Hour)
		return
	}
	// 内存降级。
	m.knownMu.Lock()
	defer m.knownMu.Unlock()
	if m.known[userID] == nil {
		m.known[userID] = make(map[string]time.Time)
	}
	m.known[userID][fp] = time.Now()
}

// checkAndRegister 检查设备是否已知，未知则注册并返回需要二次验证。
//
// 返回 (known, needMFA):
//   - enabled=false：始终返回 (true, false)（功能关闭，所有设备放行）
//   - 已知设备：(true, false)
//   - 未知设备：注册并返回 (false, true)（触发二次验证）
func (m *deviceFPManager) checkAndRegister(userID, fp string) (known bool, needMFA bool) {
	if !m.enabled || fp == "" {
		return true, false
	}
	if m.isKnownDevice(userID, fp) {
		return true, false
	}
	// 未知设备：注册并标记需二次验证。
	m.registerDevice(userID, fp)
	return false, true
}
