// 联邦请求签名：控制面与 device-svc 共享的 HMAC 签名/验签与 nonce 防重放核心。
//
// canonical 形式（字段顺序固定，| 分隔）：
//
//	新：method|path|ts|nonce|tenant|user|roles|sha256(body)
//	旧：method|path|ts|tenant|user|roles|sha256(body)
//
// 兼容规则：未启用 NonceCache（nil）时允许旧格式（无 nonce）验签，供滚动升级；
// 启用 NonceCache 时强制要求 nonce + 新格式，旧格式一律拒绝，避免绕过重放保护。
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultFederationMaxSkew 联邦签名时间戳允许的最大时钟偏差（±5min），防重放。
const DefaultFederationMaxSkew = 5 * time.Minute

// 联邦签名 HTTP 头名称（与 controlplane 现有实现逐字一致）。
const (
	FederationHeaderForwarded = "X-Federation-Forwarded"
	FederationHeaderTS        = "X-Federation-Ts"
	FederationHeaderNonce     = "X-Federation-Nonce"
	FederationHeaderSignature = "X-Federation-Sig"
	// FederationHeaderPeerID 签名方本地 peer 身份；接收方据此选择 per-peer 验签密钥。
	// 缺省时按全局共享密钥验签（向后兼容）。
	FederationHeaderPeerID = "X-Federation-Peer-ID"
)

// DefaultNonceCacheMaxEntries NonceCache 默认容量上限（超出后先清理过期项，仍满则拒绝）。
const DefaultNonceCacheMaxEntries = 100000

// nonceCachePruneEvery 每 N 次 CheckAndAdd 触发一次机会式清理（无后台 goroutine）。
const nonceCachePruneEvery = 256

// defaultNonceCacheTTL NonceCache 未指定/非法 TTL 时的默认值。
// 取 2 倍签名时间窗：保证在签名仍有效的整个窗口内 nonce 都不会被清理。
const defaultNonceCacheTTL = 2 * DefaultFederationMaxSkew

// maxFederationNonceLen 入站 nonce 的最大长度。
// 出站 nonce 固定为 32 个 hex 字符（16 字节随机数）；上限规范化到 256 以兼容
// 其他实现并防止超长 nonce 进入缓存/日志（内存与日志放大攻击面）。
// 不设下限：滚动升级期允许对端使用较短 nonce，重放保护由缓存语义独立保证。
const maxFederationNonceLen = 256

var (
	// ErrFederationMalformed 联邦签名头缺失或格式非法。
	ErrFederationMalformed = errors.New("auth: 联邦签名头缺失或格式非法")
	// ErrFederationSecretMissing 联邦共享密钥为空。
	ErrFederationSecretMissing = errors.New("auth: 联邦共享密钥为空")
	// ErrFederationSkew 时间戳超出允许偏差。
	ErrFederationSkew = errors.New("auth: 联邦签名时间戳超出允许偏差")
	// ErrFederationNonceRequired 启用防重放时请求未携带 nonce。
	ErrFederationNonceRequired = errors.New("auth: 联邦请求缺少 nonce（启用防重放时必填）")
	// ErrFederationReplay nonce 已使用过（重放）。
	ErrFederationReplay = errors.New("auth: 联邦请求 nonce 重放")
	// ErrFederationSignature 签名不匹配。
	ErrFederationSignature = errors.New("auth: 联邦签名不匹配")
)

// FederationCanonicalString 构造带 nonce 的签名原文（新格式）。
// 字段顺序：method|path|ts|nonce|tenant|user|roles|sha256(body)。
func FederationCanonicalString(method, path, ts, nonce, tenantID, userID, roles, bodySHA256 string) string {
	return strings.Join([]string{method, path, ts, nonce, tenantID, userID, roles, bodySHA256}, "|")
}

// FederationCanonicalStringLegacy 构造不含 nonce 的旧签名原文，仅用于滚动升级期验签。
// 字段顺序：method|path|ts|tenant|user|roles|sha256(body)。
func FederationCanonicalStringLegacy(method, path, ts, tenantID, userID, roles, bodySHA256 string) string {
	return strings.Join([]string{method, path, ts, tenantID, userID, roles, bodySHA256}, "|")
}

// FederationBodySHA256 返回 body 的 SHA-256 十六进制摘要；nil body 即空体摘要。
func FederationBodySHA256(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// FederationPeerSecret 单个 peer 的 HMAC 密钥集合。
// Secrets[0] 为当前签名密钥，其余为轮换期保留的旧密钥（仅用于验签）。
type FederationPeerSecret struct {
	PeerID  string
	URL     string
	Secrets []string
}

// FederationPeerResolver 按 peer URL / peer ID 查询 per-peer 密钥；构造后只读，可并发使用。
type FederationPeerResolver struct {
	byURL    map[string]FederationPeerSecret
	byPeerID map[string]FederationPeerSecret
}

// ParseFederationPeerSecrets 解析 `<peerID>@<peerURL>=<secret1>[,<secret2>...]` 条目。
// 每个条目至少一个非空 secret；peerID 与 URL 均不可为空，且不允许重复。
func ParseFederationPeerSecrets(entries []string) ([]FederationPeerSecret, error) {
	out := make([]FederationPeerSecret, 0, len(entries))
	seenPeerID := make(map[string]struct{}, len(entries))
	seenURL := make(map[string]struct{}, len(entries))
	for i, raw := range entries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			return nil, fmt.Errorf("auth: 联邦 per-peer 密钥条目[%d]为空", i)
		}
		at := strings.Index(entry, "@")
		if at <= 0 {
			return nil, fmt.Errorf("auth: 联邦 per-peer 密钥条目[%d]=%q 缺少 <peerID>@", i, entry)
		}
		peerID := strings.TrimSpace(entry[:at])
		if peerID == "" {
			return nil, fmt.Errorf("auth: 联邦 per-peer 密钥条目[%d]=%q 的 peerID 为空", i, entry)
		}
		rest := entry[at+1:]
		eq := strings.Index(rest, "=")
		if eq <= 0 {
			return nil, fmt.Errorf("auth: 联邦 per-peer 密钥条目[%d]=%q 缺少 <peerURL>=", i, entry)
		}
		peerURL := strings.TrimSpace(rest[:eq])
		if peerURL == "" {
			return nil, fmt.Errorf("auth: 联邦 per-peer 密钥条目[%d]=%q 的 peerURL 为空", i, entry)
		}
		parts := strings.Split(rest[eq+1:], ",")
		secrets := make([]string, 0, len(parts))
		for _, part := range parts {
			secret := strings.TrimSpace(part)
			if secret == "" {
				return nil, fmt.Errorf("auth: 联邦 per-peer 密钥条目[%d]=%q 含空 secret", i, entry)
			}
			secrets = append(secrets, secret)
		}
		if _, dup := seenPeerID[peerID]; dup {
			return nil, fmt.Errorf("auth: 联邦 per-peer 密钥 peerID %q 重复", peerID)
		}
		if _, dup := seenURL[peerURL]; dup {
			return nil, fmt.Errorf("auth: 联邦 per-peer 密钥 URL %q 重复", peerURL)
		}
		seenPeerID[peerID] = struct{}{}
		seenURL[peerURL] = struct{}{}
		out = append(out, FederationPeerSecret{PeerID: peerID, URL: peerURL, Secrets: secrets})
	}
	return out, nil
}

// NewFederationPeerResolver 用已解析的 per-peer 密钥构造只读查询器。
func NewFederationPeerResolver(peers []FederationPeerSecret) *FederationPeerResolver {
	r := &FederationPeerResolver{
		byURL:    make(map[string]FederationPeerSecret, len(peers)),
		byPeerID: make(map[string]FederationPeerSecret, len(peers)),
	}
	for _, p := range peers {
		r.byURL[p.URL] = p
		r.byPeerID[p.PeerID] = p
	}
	return r
}

// ParseFederationPeerResolver 解析条目并直接构造查询器。
func ParseFederationPeerResolver(entries []string) (*FederationPeerResolver, error) {
	peers, err := ParseFederationPeerSecrets(entries)
	if err != nil {
		return nil, err
	}
	return NewFederationPeerResolver(peers), nil
}

// SecretForURL 返回目标 peer 的 peerID 与当前签名密钥（Secrets[0]）；未配置返回 ok=false。
func (r *FederationPeerResolver) SecretForURL(url string) (peerID, secret string, ok bool) {
	if r == nil {
		return "", "", false
	}
	p, found := r.byURL[url]
	if !found || len(p.Secrets) == 0 {
		return "", "", false
	}
	return p.PeerID, p.Secrets[0], true
}

// SecretsForPeerID 返回该 peer 的全部可验签密钥（含轮换期旧密钥）；未配置返回 nil。
func (r *FederationPeerResolver) SecretsForPeerID(peerID string) []string {
	if r == nil {
		return nil
	}
	p, found := r.byPeerID[peerID]
	if !found || len(p.Secrets) == 0 {
		return nil
	}
	out := make([]string, len(p.Secrets))
	copy(out, p.Secrets)
	return out
}

// SignFederation 对 canonical 原文做 HMAC-SHA256，返回十六进制签名。
func SignFederation(secret, canonical string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

// NonceCache 记录最近见过的 nonce 以拒绝重放。
//
// 无线程池、无后台 goroutine：过期项在 CheckAndAdd 内机会式清理；
// 容量达到上限时先清理再判断，仍满则拒绝新 nonce（返回 false），保证内存有界。
// 并发安全；零值可用（TTL 与容量取默认值）。
type NonceCache struct {
	mu         sync.Mutex
	ttl        time.Duration
	maxEntries int
	entries    map[string]time.Time // nonce -> 过期时刻
	ops        uint64
}

// NewNonceCache 构造指定 TTL 的 NonceCache；ttl <= 0 时使用 2 倍默认签名时间窗。
func NewNonceCache(ttl time.Duration) *NonceCache {
	return &NonceCache{
		ttl:        ttl,
		maxEntries: DefaultNonceCacheMaxEntries,
		entries:    make(map[string]time.Time),
	}
}

// CheckAndAdd 原子检查并登记 nonce：首次出现返回 true，重放返回 false。
//
// 语义：
//   - nil receiver：返回 true（测试或未启用防重放时可传 nil）；
//   - 空 nonce：返回 false（启用防重放后空 nonce 不可用）；
//   - 已登记且未过期：返回 false（重放）；
//   - 已登记但已过期：视为未出现，覆盖登记后返回 true；
//   - 缓存已满：先清理过期项，仍满则返回 false（与重放同语义，保护内存）。
func (c *NonceCache) CheckAndAdd(nonce string, now time.Time) bool {
	if c == nil {
		return true
	}
	if nonce == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.IsZero() {
		now = time.Now()
	}
	if c.entries == nil {
		c.entries = make(map[string]time.Time)
	}
	if exp, ok := c.entries[nonce]; ok {
		if exp.After(now) {
			return false
		}
		delete(c.entries, nonce)
	}
	c.ops++
	if len(c.entries) >= c.maxEntriesValue() || c.ops%nonceCachePruneEvery == 0 {
		c.pruneLocked(now)
	}
	if len(c.entries) >= c.maxEntriesValue() {
		return false
	}
	c.entries[nonce] = now.Add(c.ttlValue())
	return true
}

// ttlValue 返回生效 TTL（调用方需持有锁或仅构造期调用）。
func (c *NonceCache) ttlValue() time.Duration {
	if c.ttl <= 0 {
		return defaultNonceCacheTTL
	}
	return c.ttl
}

// maxEntriesValue 返回生效容量上限。
func (c *NonceCache) maxEntriesValue() int {
	if c.maxEntries <= 0 {
		return DefaultNonceCacheMaxEntries
	}
	return c.maxEntries
}

// pruneLocked 删除已过期 nonce；调用方必须持有 c.mu。
func (c *NonceCache) pruneLocked(now time.Time) {
	for nonce, exp := range c.entries {
		if !exp.After(now) {
			delete(c.entries, nonce)
		}
	}
}

// VerifyFederationSignature 校验入站联邦请求的 HMAC 签名、时间窗与 nonce 重放。
//
// 参数：
//   - secret：联邦共享密钥（空则拒绝）；
//   - maxSkew：允许的 |now-ts| 偏差，<=0 时使用 DefaultFederationMaxSkew；
//   - method/path：参与签名的请求方法与路径；
//   - headers：请求头（X-Federation-* / 身份头）；
//   - body：完整请求体（GET 传 nil）；
//   - nonces：防重放缓存；nil 表示未启用（允许旧格式，无重放保护）；
//   - now：校验基准时间，零值取 time.Now()。
//
// 时间戳接受 unix 秒或 RFC3339；canonical 使用头部原始字符串，签名方须与其一致。
//
// 兼容规则（滚动升级安全边界）：
//   - nonces == nil：无 nonce 头时按 legacy canonical 验签；有 nonce 头时按新 canonical 验签；
//   - nonces != nil：必须携带非空 nonce 且只接受新 canonical；legacy 请求直接拒绝
//     （返回 ErrFederationNonceRequired），否则重放保护可被无 nonce 请求绕过。
//
// 签名不匹配返回 ErrFederationSignature，重放返回 ErrFederationReplay；
// 错误均可用 errors.Is 判断。
func VerifyFederationSignature(secret []byte, maxSkew time.Duration, method, path string, headers http.Header, body []byte, nonces *NonceCache, now time.Time) error {
	return VerifyFederationSignatureAny([][]byte{secret}, maxSkew, method, path, headers, body, nonces, now)
}

// VerifyFederationSignatureAny 与 VerifyFederationSignature 语义一致，但接受多个候选密钥：
// 任一非空候选匹配即通过（per-peer 密钥轮换期新旧密钥并存）。空候选集合一律拒绝；
// nonce 仅在签名匹配成功后消费一次，避免错误候选提前耗尽重放缓存。
func VerifyFederationSignatureAny(secrets [][]byte, maxSkew time.Duration, method, path string, headers http.Header, body []byte, nonces *NonceCache, now time.Time) error {
	candidates := make([][]byte, 0, len(secrets))
	for _, secret := range secrets {
		if len(secret) > 0 {
			candidates = append(candidates, secret)
		}
	}
	if len(candidates) == 0 {
		return ErrFederationSecretMissing
	}
	if headers.Get(FederationHeaderForwarded) != "1" {
		return fmt.Errorf("%w: %s 必须为 1", ErrFederationMalformed, FederationHeaderForwarded)
	}
	if maxSkew <= 0 {
		maxSkew = DefaultFederationMaxSkew
	}
	if now.IsZero() {
		now = time.Now()
	}
	tsRaw := headers.Get(FederationHeaderTS)
	sig := headers.Get(FederationHeaderSignature)
	if tsRaw == "" || sig == "" {
		return fmt.Errorf("%w: 缺少 %s 或 %s", ErrFederationMalformed, FederationHeaderTS, FederationHeaderSignature)
	}
	ts, err := parseFederationTimestamp(tsRaw)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrFederationMalformed, err)
	}
	if skew := now.Sub(ts); skew > maxSkew || skew < -maxSkew {
		return fmt.Errorf("%w: |now-ts|=%s 超过 %s", ErrFederationSkew, skew.Abs(), maxSkew)
	}
	nonce := headers.Get(FederationHeaderNonce)
	if nonces != nil && nonce == "" {
		return ErrFederationNonceRequired
	}
	if err := validateFederationNonce(nonce); err != nil {
		return err
	}
	tenant := headers.Get("X-Tenant-ID")
	user := headers.Get("X-User-Id")
	roles := headers.Get("X-User-Roles")
	bodySHA := FederationBodySHA256(body)
	var canonical string
	if nonce != "" {
		canonical = FederationCanonicalString(method, path, tsRaw, nonce, tenant, user, roles, bodySHA)
	} else {
		canonical = FederationCanonicalStringLegacy(method, path, tsRaw, tenant, user, roles, bodySHA)
	}
	matched := false
	for _, secret := range candidates {
		expected := SignFederation(string(secret), canonical)
		if hmac.Equal([]byte(expected), []byte(sig)) {
			matched = true
			break
		}
	}
	if !matched {
		return ErrFederationSignature
	}
	if nonces != nil && !nonces.CheckAndAdd(nonce, now) {
		return fmt.Errorf("%w: nonce=%q", ErrFederationReplay, nonce)
	}
	return nil
}

// validateFederationNonce 校验 nonce 的形态边界：仅允许可见 ASCII 且不超过上限。
// 空 nonce 由调用方按"未启用防重放/旧格式"或 ErrFederationNonceRequired 处理，此处放行。
func validateFederationNonce(nonce string) error {
	if nonce == "" {
		return nil
	}
	if len(nonce) > maxFederationNonceLen {
		return fmt.Errorf("%w: %s 长度 %d 超过上限 %d", ErrFederationMalformed, FederationHeaderNonce, len(nonce), maxFederationNonceLen)
	}
	for i := 0; i < len(nonce); i++ {
		if c := nonce[i]; c < 0x21 || c > 0x7e {
			return fmt.Errorf("%w: %s 含非法字符（仅允许可见 ASCII）", ErrFederationMalformed, FederationHeaderNonce)
		}
	}
	return nil
}

// parseFederationTimestamp 解析 unix 秒或 RFC3339 时间戳。
func parseFederationTimestamp(raw string) (time.Time, error) {
	if sec, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return time.Unix(sec, 0), nil
	}
	ts, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("时间戳 %q 既非 unix 秒也非 RFC3339", raw)
	}
	return ts, nil
}
