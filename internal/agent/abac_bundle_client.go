// abac_bundle_client.go 实现 agent 侧 ABAC 策略包拉取与 last-known-good 缓存（M1）。
//
// 契约（服务端见 internal/controlplane/abac_bundle_endpoint.go）：
//   - GET /api/v1/agent/abac-bundle
//   - 认证头：X-OpsMesh-Agent-ID / X-OpsMesh-Agent-Timestamp（unix 秒）/
//     X-OpsMesh-Agent-Signature = hex(HMAC-SHA256(secret, "abac-bundle\n"+agentID+"\n"+timestamp))；
//     密钥复用 gRPC 注册通道下发的 per-agent secret（Agent.register → resolveAgentKey），不新增凭证。
//   - 200：SignedBundle JSON + 强 ETag；304：未变更；401/403：身份/授权错误；503：签发未配置。
//
// 安全语义（fail-closed）：
//   - 200 必须通过 abacbundle.KeySet.Verify（Ed25519，按 keyId 选钥，支持双钥轮换）；
//   - 解码失败 / 验签失败 / 超限 → 丢弃响应，内存与磁盘 LKG 均不变；
//   - 401/403/503/网络失败只告警，绝不清空已有策略（配置错误不应抹掉可用策略）；
//   - 过期 LKG 保留（Expired()=true），供后续里程碑按模式（split/closed/open）处理。
//
// 缓存格式：服务端 200 响应原始字节（可再次验签的 SignedBundle JSON），
// 原子写（同目录临时文件 → fsync → rename），权限 0600；目录 0700。
package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/pkg/abacbundle"
)

const (
	// abacBundleRoute 与控制面注册的路由一致（server_lifecycle.go）。
	abacBundleRoute = "/api/v1/agent/abac-bundle"
	// abacBundleSignaturePrefix 签名载荷域分隔前缀（与服务端 abacBundleSignaturePrefix 一致）。
	abacBundleSignaturePrefix = "abac-bundle"
	// abacBundleCapability 心跳能力声明值（版本化，未来 v2 并列上报）。
	abacBundleCapability = "abac_bundle=v1"
	// abacBundleDefaultRefreshInterval 轮询默认间隔。
	abacBundleDefaultRefreshInterval = 10 * time.Minute
	// abacBundleFetchTimeout 单次拉取的 HTTP 超时（不影响 ticker 周期）。
	abacBundleFetchTimeout = 20 * time.Second

	// Current() 的 source 取值。
	abacBundleSourceNone    = "none"
	abacBundleSourceCache   = "cache"
	abacBundleSourceNetwork = "network"
)

// ABACBundleLogger 是策略包客户端的最小日志接口。
// 生产使用 logx 适配器（logger 传 nil 即默认）；测试可注入记录器。
type ABACBundleLogger interface {
	Debug(ctx context.Context, msg string, args ...any)
	Info(ctx context.Context, msg string, args ...any)
	Warn(ctx context.Context, msg string, args ...any)
	Error(ctx context.Context, msg string, err error, args ...any)
}

// logxABACBundleLogger 把 ABACBundleLogger 适配到项目统一的结构化日志（logx）。
type logxABACBundleLogger struct{}

func (logxABACBundleLogger) Debug(ctx context.Context, msg string, args ...any) {
	logx.Debug(ctx, msg, args...)
}

func (logxABACBundleLogger) Info(ctx context.Context, msg string, args ...any) {
	logx.Info(ctx, msg, args...)
}

func (logxABACBundleLogger) Warn(ctx context.Context, msg string, args ...any) {
	logx.Warn(ctx, msg, args...)
}

func (logxABACBundleLogger) Error(ctx context.Context, msg string, err error, args ...any) {
	logx.Error(ctx, msg, err, args...)
}

// ABACBundleClient 负责策略包的周期拉取、验签、LKG 缓存与内存快照。
//
// 并发模型：Start 是唯一的常驻 goroutine（立即拉取一轮后按 ticker 轮询）；
// 内存状态由 mu 保护，Current/Expired/Capabilities 只读。
type ABACBundleClient struct {
	enabled   bool
	agentID   string
	secret    string
	baseURL   string
	cachePath string
	interval  time.Duration
	keyCount  int
	log       ABACBundleLogger
	http      *http.Client
	keys      *abacbundle.KeySet
	// now 可注入时钟（测试用）；生产为 time.Now。
	now func() time.Time

	mu        sync.RWMutex
	bundle    *abacbundle.SignedBundle
	etag      string
	source    string
	fetchedAt time.Time
}

// NewABACBundleClient 构造 agent 侧策略包客户端。
//
// cfg 提供启用开关、验签公钥（keyID:base64pubkey，可多把轮换）、缓存路径与轮询间隔；
// agentID/secret 为注册后的身份与 per-agent HMAC 密钥（与 gRPC 签名同源）；
// controlURL 为控制面 HTTP 基地址（如 http://cp:8080）；logger 为 nil 时使用 logx。
func NewABACBundleClient(cfg *config.Config, agentID, secret, controlURL string, logger ABACBundleLogger) *ABACBundleClient {
	if logger == nil {
		logger = logxABACBundleLogger{}
	}
	c := &ABACBundleClient{
		agentID:  strings.TrimSpace(agentID),
		secret:   secret,
		baseURL:  normalizeABACBundleBaseURL(controlURL),
		interval: abacBundleDefaultRefreshInterval,
		log:      logger,
		http:     &http.Client{Timeout: abacBundleFetchTimeout},
		keys:     &abacbundle.KeySet{},
		now:      time.Now,
		source:   abacBundleSourceNone,
	}
	if cfg == nil {
		return c
	}
	c.enabled = cfg.ABACBundleEnabled
	c.cachePath = strings.TrimSpace(cfg.ABACBundleCachePath)
	if c.cachePath == "" && strings.TrimSpace(cfg.DataDir) != "" {
		// 配置层已给默认值；此处兜底程序化构造的 Config。
		c.cachePath = filepath.Join(cfg.DataDir, "abac-bundle.json")
	}
	if cfg.ABACBundleRefreshInterval > 0 {
		c.interval = cfg.ABACBundleRefreshInterval
	}
	for _, entry := range cfg.ABACBundlePublicKeys {
		keyID, pub, err := parseABACBundlePublicKey(entry)
		if err != nil {
			logger.Error(context.Background(), "ABAC 策略包公钥配置非法，已跳过", err, "entryPrefix", entryPrefix(entry))
			continue
		}
		if err := c.keys.Add(keyID, pub); err != nil {
			logger.Error(context.Background(), "ABAC 策略包公钥注册失败，已跳过", err, "keyID", keyID)
			continue
		}
		c.keyCount++
	}
	if c.enabled && c.keyCount == 0 {
		logger.Warn(context.Background(), "ABAC 策略包已启用但未配置有效验签公钥，拉取不会启动（配置 --abac-bundle-public-keys keyID:base64pubkey）")
	}
	return c
}

// parseABACBundlePublicKey 解析单条 `keyID:base64pubkey` 条目。
// base64 使用标准编码（与服务端 Ed25519 密钥编码一致）。
func parseABACBundlePublicKey(entry string) (string, ed25519.PublicKey, error) {
	parts := strings.SplitN(strings.TrimSpace(entry), ":", 2)
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("条目 %q 缺少 keyID: 前缀", entryPrefix(entry))
	}
	keyID := strings.TrimSpace(parts[0])
	if keyID == "" {
		return "", nil, fmt.Errorf("条目 %q 的 keyID 为空", entryPrefix(entry))
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(parts[1]))
	if err != nil {
		return "", nil, fmt.Errorf("keyID %q 公钥不是合法 base64: %w", keyID, err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return "", nil, fmt.Errorf("keyID %q 公钥长度 %d，want %d", keyID, len(raw), ed25519.PublicKeySize)
	}
	return keyID, ed25519.PublicKey(raw), nil
}

// entryPrefix 返回配置条目可用于日志的前缀（避免整段公钥进入日志）。
func entryPrefix(entry string) string {
	entry = strings.TrimSpace(entry)
	if i := strings.Index(entry, ":"); i > 0 {
		return entry[:i]
	}
	if len(entry) > 16 {
		return entry[:16]
	}
	return entry
}

// normalizeABACBundleBaseURL 规范化控制面 HTTP 基地址：
// 去空白、补 http:// scheme、取逗号列表首项、去尾部斜杠。
func normalizeABACBundleBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if i := strings.Index(raw, ","); i >= 0 {
		raw = strings.TrimSpace(raw[:i])
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	return strings.TrimRight(raw, "/")
}

// endpointURL 返回策略包端点完整 URL。
func (c *ABACBundleClient) endpointURL() string {
	if c.baseURL == "" {
		return ""
	}
	if strings.HasSuffix(c.baseURL, abacBundleRoute) {
		return c.baseURL
	}
	return c.baseURL + abacBundleRoute
}

// Start 启动拉取循环：立即拉取一轮，随后按 RefreshInterval 轮询，ctx 取消即退出。
// 单 goroutine；未启用/缺少密钥/缺少公钥时不启动（只记一次告警）。
func (c *ABACBundleClient) Start(ctx context.Context) {
	if c == nil || !c.enabled {
		return
	}
	if c.endpointURL() == "" {
		c.log.Warn(ctx, "ABAC 策略包客户端未配置控制面地址，拉取不启动")
		return
	}
	if c.secret == "" {
		c.log.Warn(ctx, "ABAC 策略包客户端缺少 agent 签名密钥（注册未下发且无 agent.key / --grpc-signature-key），拉取不启动")
		return
	}
	if c.keyCount == 0 {
		c.log.Warn(ctx, "ABAC 策略包客户端缺少验签公钥，拉取不启动")
		return
	}

	c.fetch(ctx)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.fetch(ctx)
		}
	}
}

// fetch 执行一轮拉取：HMAC 签名 → 条件请求（If-None-Match）→ 验签 → 原子落盘 → 内存换代。
// 任何失败都保留现有 LKG（fail-closed：未验签内容绝不进入内存/磁盘）。
func (c *ABACBundleClient) fetch(ctx context.Context) {
	req, err := c.newRequest(ctx)
	if err != nil {
		c.log.Error(ctx, "ABAC 策略包请求构造失败", err, "agentID", c.agentID)
		return
	}

	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return // 进程退出/循环取消，不当作拉取失败
		}
		c.log.Warn(ctx, "ABAC 策略包拉取失败（保留现有缓存）", "agentID", c.agentID, "url", req.URL.String(), "err", err.Error())
		return
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// 继续处理响应体。
	case http.StatusNotModified:
		c.log.Debug(ctx, "ABAC 策略包未变更（304）", "agentID", c.agentID)
		return
	case http.StatusUnauthorized, http.StatusForbidden:
		// 身份/授权问题：重试无意义，但不删除缓存（可能只是密钥轮换期间的暂时错配）。
		c.log.Warn(ctx, "ABAC 策略包请求被拒绝（401/403），保留现有缓存", "agentID", c.agentID, "status", resp.StatusCode)
		return
	case http.StatusServiceUnavailable:
		c.log.Info(ctx, "ABAC 策略包端点尚未就绪（503），保留现有缓存", "agentID", c.agentID)
		return
	default:
		c.log.Warn(ctx, "ABAC 策略包拉取返回非预期状态，保留现有缓存", "agentID", c.agentID, "status", resp.StatusCode)
		return
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, abacbundle.MaxBundleBytes+1))
	if err != nil {
		c.log.Warn(ctx, "ABAC 策略包响应读取失败（保留现有缓存）", "agentID", c.agentID, "err", err.Error())
		return
	}
	if len(raw) > abacbundle.MaxBundleBytes {
		c.log.Error(ctx, "ABAC 策略包超过大小上限，已丢弃", fmt.Errorf("bundle %d > %d bytes", len(raw), abacbundle.MaxBundleBytes), "agentID", c.agentID)
		return
	}

	sb, err := abacbundle.Decode(raw)
	if err != nil {
		c.log.Error(ctx, "ABAC 策略包解码失败，已丢弃", err, "agentID", c.agentID)
		return
	}
	now := c.now().UTC()
	if err := c.keys.Verify(sb, now); err != nil {
		// 签名无效/未知 keyId/过期 = 不可信工件：不落盘、不换代。
		c.log.Error(ctx, "ABAC 策略包验签失败，已丢弃（保留现有缓存）", err,
			"agentID", c.agentID, "bundleID", sb.Bundle.BundleID, "keyID", sb.Bundle.KeyID)
		return
	}

	etag := strings.TrimSpace(resp.Header.Get("ETag"))
	if etag == "" {
		if digest, derr := abacbundle.Digest(sb.Bundle); derr == nil {
			etag = `"` + digest + `"`
		}
	}

	if err := c.persist(raw); err != nil {
		// 落盘失败不丢弃已验证的新包：本次运行仍可用，下次成功拉取会再尝试持久化。
		c.log.Warn(ctx, "ABAC 策略包缓存落盘失败（继续使用内存副本）", "agentID", c.agentID, "path", c.cachePath, "err", err.Error())
	}

	c.mu.Lock()
	c.bundle = &sb
	c.etag = etag
	c.source = abacBundleSourceNetwork
	c.fetchedAt = now
	c.mu.Unlock()

	c.log.Info(ctx, "ABAC 策略包已更新",
		"agentID", c.agentID, "bundleID", sb.Bundle.BundleID, "keyID", sb.Bundle.KeyID,
		"policies", len(sb.Bundle.Policies), "expiresAt", sb.Bundle.ExpiresAt.UTC().Format(time.RFC3339))
}

// newRequest 构造带 per-agent HMAC 签名与条件 ETag 的 GET 请求。
func (c *ABACBundleClient) newRequest(ctx context.Context) (*http.Request, error) {
	endpoint := c.endpointURL()
	if endpoint == "" {
		return nil, fmt.Errorf("控制面地址为空")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	ts := strconv.FormatInt(c.now().UTC().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(c.secret))
	_, _ = mac.Write([]byte(abacBundleSignaturePrefix + "\n" + c.agentID + "\n" + ts))
	req.Header.Set("X-OpsMesh-Agent-ID", c.agentID)
	req.Header.Set("X-OpsMesh-Agent-Timestamp", ts)
	req.Header.Set("X-OpsMesh-Agent-Signature", hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("Accept", "application/json")
	if etag := c.currentETag(); etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	return req, nil
}

// currentETag 返回当前 bundle 的强验证器（无则空串）。
func (c *ABACBundleClient) currentETag() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.etag
}

// persist 原子写入 LKG：同目录临时文件（0600）→ fsync → rename → fsync(dir)。
// 任一环节失败时旧文件保持不变（rename 前不触碰目标路径）。
func (c *ABACBundleClient) persist(raw []byte) error {
	if c.cachePath == "" {
		return fmt.Errorf("缓存路径为空")
	}
	dir := filepath.Dir(c.cachePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("创建缓存目录: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".abac-bundle-*.tmp")
	if err != nil {
		return fmt.Errorf("创建临时文件: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("设置临时文件权限: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		cleanup()
		return fmt.Errorf("写入临时文件: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("fsync 临时文件: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("关闭临时文件: %w", err)
	}
	if err := os.Rename(tmpName, c.cachePath); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("原子替换缓存: %w", err)
	}
	// 目录 fsync 保证 rename 持久（POSIX 有效；Windows 不支持目录 fsync，忽略错误）。
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// LoadFromCache 在首次网络拉取前加载 LKG。
//
// 验签通过（含已过期但签名有效）才载入；签名无效/未知 keyId/结构非法一律忽略。
// 过期包不删除、不标记删除，仅通过 Expired() 暴露（后续里程碑按模式处理）。
// 若内存中已有网络包，则不用缓存覆盖（避免运行期调用把 source 降级）。
func (c *ABACBundleClient) LoadFromCache() {
	if c == nil || !c.enabled || c.cachePath == "" || c.keyCount == 0 {
		return
	}
	raw, err := os.ReadFile(c.cachePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.log.Info(context.Background(), "ABAC 策略包无本地缓存", "agentID", c.agentID, "path", c.cachePath)
		} else {
			c.log.Warn(context.Background(), "ABAC 策略包缓存读取失败", "agentID", c.agentID, "path", c.cachePath, "err", err.Error())
		}
		return
	}
	sb, err := abacbundle.Decode(raw)
	if err != nil {
		c.log.Error(context.Background(), "ABAC 策略包缓存解码失败，已忽略", err, "agentID", c.agentID, "path", c.cachePath)
		return
	}
	now := c.now().UTC()
	verr := c.keys.Verify(sb, now)
	expired := errors.Is(verr, abacbundle.ErrExpired)
	if verr != nil && !expired {
		c.log.Error(context.Background(), "ABAC 策略包缓存验签失败，已忽略", verr,
			"agentID", c.agentID, "bundleID", sb.Bundle.BundleID, "keyID", sb.Bundle.KeyID)
		return
	}

	etag := ""
	if digest, derr := abacbundle.Digest(sb.Bundle); derr == nil {
		etag = `"` + digest + `"`
	}

	c.mu.Lock()
	if c.bundle != nil && c.source == abacBundleSourceNetwork {
		c.mu.Unlock()
		return // 已有网络包，不用磁盘副本覆盖
	}
	c.bundle = &sb
	c.etag = etag
	c.source = abacBundleSourceCache
	c.fetchedAt = now
	c.mu.Unlock()

	if expired {
		c.log.Warn(context.Background(), "ABAC 策略包缓存已过期（保留 LKG，不删除）",
			"agentID", c.agentID, "bundleID", sb.Bundle.BundleID,
			"expiresAt", sb.Bundle.ExpiresAt.UTC().Format(time.RFC3339))
		return
	}
	c.log.Info(context.Background(), "ABAC 策略包缓存已加载",
		"agentID", c.agentID, "bundleID", sb.Bundle.BundleID, "keyID", sb.Bundle.KeyID,
		"policies", len(sb.Bundle.Policies), "expiresAt", sb.Bundle.ExpiresAt.UTC().Format(time.RFC3339))
}

// Current 返回当前内存中的策略包与来源（network / cache / none）。
// 返回的指针指向不可变快照：调用方不得修改（换代时整体替换指针）。
func (c *ABACBundleClient) Current() (*abacbundle.SignedBundle, string) {
	if c == nil {
		return nil, abacBundleSourceNone
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.bundle == nil {
		return nil, abacBundleSourceNone
	}
	return c.bundle, c.source
}

// Expired 报告当前策略包是否已过期（无包时返回 false，由 Current()==nil 表达「无意见」）。
func (c *ABACBundleClient) Expired() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.bundle == nil {
		return false
	}
	return !c.now().UTC().Before(c.bundle.Bundle.ExpiresAt)
}

// Capabilities 返回心跳能力声明（启用且至少一把验签公钥时声明 abac_bundle=v1）。
//
// 为什么不等首包成功后再声明：能力门控（M1 规格 §9.2）是「声明后才允许请求」，
// 若声明依赖首包成功会形成死锁；公钥就绪即代表协议能力就绪，首包失败由缓存/服务端评估兜底。
func (c *ABACBundleClient) Capabilities() []string {
	if c == nil || !c.enabled || c.keyCount == 0 {
		return nil
	}
	return []string{abacBundleCapability}
}
