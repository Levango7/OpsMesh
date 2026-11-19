package agent

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/pkg/abacbundle"
)

// nopABACBundleLogger 静默日志（避免后台 goroutine 在测试结束后写 t.Logf）。
type nopABACBundleLogger struct{}

func (nopABACBundleLogger) Debug(context.Context, string, ...any)        {}
func (nopABACBundleLogger) Info(context.Context, string, ...any)         {}
func (nopABACBundleLogger) Warn(context.Context, string, ...any)         {}
func (nopABACBundleLogger) Error(context.Context, string, error, ...any) {}

// abacBundleTestServer 按端点契约实现 GET /api/v1/agent/abac-bundle：
// 校验三个 agent 认证头、返回强 ETag、命中 If-None-Match 时 304，
// 并可在 tamper=true 时返回「签名后篡改」的坏包。
type abacBundleTestServer struct {
	t       *testing.T
	agentID string
	secret  string
	keyID   string
	priv    ed25519.PrivateKey
	bundle  abacbundle.Bundle
	payload []byte
	etag    string

	mu              sync.Mutex
	tamper          bool
	requests        int
	notModified     int
	badAuth         int
	badPath         int
	lastIfNoneMatch string
}

func newABACBundleTestServer(t *testing.T, agentID, keyID string) (*abacBundleTestServer, ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("生成 Ed25519 密钥: %v", err)
	}
	now := time.Now().UTC()
	b := abacbundle.Bundle{
		SchemaVersion: abacbundle.SchemaVersion,
		BundleID:      "bundle-test-0001",
		TenantScope:   "tenant-1",
		KeyID:         keyID,
		IssuedAt:      now,
		ExpiresAt:     now.Add(time.Hour),
		Policies: []abacbundle.Policy{
			{ID: "p1", Subject: "user:1", Resource: "device:*", Action: "read", Effect: "allow"},
		},
	}
	signed, err := abacbundle.Sign(b, priv)
	if err != nil {
		t.Fatalf("签名测试 bundle: %v", err)
	}
	payload, err := abacbundle.Encode(signed)
	if err != nil {
		t.Fatalf("编码测试 bundle: %v", err)
	}
	digest, err := abacbundle.Digest(b)
	if err != nil {
		t.Fatalf("摘要测试 bundle: %v", err)
	}
	return &abacBundleTestServer{
		t:       t,
		agentID: agentID,
		secret:  "test-agent-secret",
		keyID:   keyID,
		priv:    priv,
		bundle:  b,
		payload: payload,
		etag:    `"` + digest + `"`,
	}, pub
}

func (s *abacBundleTestServer) setTamper(v bool) {
	s.mu.Lock()
	s.tamper = v
	s.mu.Unlock()
}

func (s *abacBundleTestServer) counts() (requests, notModified, badAuth, badPath int, lastIfNoneMatch string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests, s.notModified, s.badAuth, s.badPath, s.lastIfNoneMatch
}

func (s *abacBundleTestServer) verifySignature(r *http.Request) bool {
	if r.Header.Get("X-OpsMesh-Agent-ID") != s.agentID {
		return false
	}
	ts := r.Header.Get("X-OpsMesh-Agent-Timestamp")
	tsSec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	if skew := time.Since(time.Unix(tsSec, 0)); skew > 5*time.Minute || skew < -5*time.Minute {
		return false
	}
	mac := hmac.New(sha256.New, []byte(s.secret))
	_, _ = mac.Write([]byte("abac-bundle\n" + s.agentID + "\n" + ts))
	want := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(r.Header.Get("X-OpsMesh-Agent-Signature")))
}

func (s *abacBundleTestServer) handler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests++
	tampered := s.tamper
	ifnm := r.Header.Get("If-None-Match")
	s.lastIfNoneMatch = ifnm
	if r.URL.Path != "/api/v1/agent/abac-bundle" {
		s.badPath++
	}
	s.mu.Unlock()

	if r.URL.Path != "/api/v1/agent/abac-bundle" {
		http.NotFound(w, r)
		return
	}
	if !s.verifySignature(r) {
		s.mu.Lock()
		s.badAuth++
		s.mu.Unlock()
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	// 篡改模式下强制返回 200 坏包（否则 If-None-Match 会把请求挡在 304）。
	if !tampered && ifnm == s.etag {
		s.mu.Lock()
		s.notModified++
		s.mu.Unlock()
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("ETag", s.etag)
	if tampered {
		var sb abacbundle.SignedBundle
		if err := json.Unmarshal(s.payload, &sb); err != nil {
			s.t.Errorf("unmarshal payload: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		sb.Bundle.Policies[0].Effect = "deny" // 签名后篡改 → 验签必然失败
		raw, err := abacbundle.Encode(sb)
		if err != nil {
			s.t.Errorf("encode tampered: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(raw)
		return
	}
	_, _ = w.Write(s.payload)
}

// newTestABACBundleConfig 构造启用状态的 agent 配置（缓存落在 t.TempDir）。
func newTestABACBundleConfig(dir, keyID string, pub ed25519.PublicKey) *config.Config {
	return &config.Config{
		ABACBundleEnabled:         true,
		ABACBundlePublicKeys:      []string{keyID + ":" + base64.StdEncoding.EncodeToString(pub)},
		ABACBundleCachePath:       filepath.Join(dir, "abac-bundle.json"),
		ABACBundleRefreshInterval: time.Hour,
	}
}

func newTestABACBundleHTTP(t *testing.T, s *abacBundleTestServer) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(s.handler))
	t.Cleanup(srv.Close)
	return srv
}

// TestABACBundleClient_FetchHappyPath：200 → 验签 → 换内存（source=network）+ 缓存落盘 0600。
func TestABACBundleClient_FetchHappyPath(t *testing.T) {
	dir := t.TempDir()
	srv, pub := newABACBundleTestServer(t, "agent-1", "k1")
	httpSrv := newTestABACBundleHTTP(t, srv)
	c := NewABACBundleClient(newTestABACBundleConfig(dir, "k1", pub), "agent-1", srv.secret, httpSrv.URL, nopABACBundleLogger{})

	c.fetch(context.Background())

	sb, source := c.Current()
	if sb == nil || source != abacBundleSourceNetwork {
		t.Fatalf("Current() = (%v, %q), want bundle, %q", sb != nil, source, abacBundleSourceNetwork)
	}
	if sb.Bundle.BundleID != srv.bundle.BundleID || sb.Bundle.KeyID != "k1" {
		t.Fatalf("bundle = %q/key=%q, want %q/k1", sb.Bundle.BundleID, sb.Bundle.KeyID, srv.bundle.BundleID)
	}
	if c.Expired() {
		t.Fatal("刚拉取的有效 bundle 不应过期")
	}

	cachePath := newTestABACBundleConfig(dir, "k1", pub).ABACBundleCachePath
	raw, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("缓存文件不存在: %v", err)
	}
	if !bytes.Equal(raw, srv.payload) {
		t.Fatal("缓存必须是服务端原始响应字节（可再次验签）")
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(cachePath)
		if err != nil {
			t.Fatalf("stat 缓存: %v", err)
		}
		if perm := fi.Mode().Perm(); perm != 0o600 {
			t.Fatalf("缓存权限 = %o, want 600", perm)
		}
	}

	if reqs, _, badAuth, badPath, _ := srv.counts(); reqs != 1 || badAuth != 0 || badPath != 0 {
		t.Fatalf("requests=%d badAuth=%d badPath=%d, want 1/0/0（认证头必须正确）", reqs, badAuth, badPath)
	}
}

// TestABACBundleClient_NotModifiedKeepsBundle：304 不清空、不换代，且请求携带 If-None-Match。
func TestABACBundleClient_NotModifiedKeepsBundle(t *testing.T) {
	dir := t.TempDir()
	srv, pub := newABACBundleTestServer(t, "agent-1", "k1")
	httpSrv := newTestABACBundleHTTP(t, srv)
	c := NewABACBundleClient(newTestABACBundleConfig(dir, "k1", pub), "agent-1", srv.secret, httpSrv.URL, nopABACBundleLogger{})

	c.fetch(context.Background())
	first, firstSource := c.Current()
	if first == nil {
		t.Fatal("首次拉取应得到 bundle")
	}
	c.fetch(context.Background())
	second, source := c.Current()
	if second == nil || second.Bundle.BundleID != first.Bundle.BundleID {
		t.Fatal("304 后应保留原 bundle")
	}
	if source != firstSource || source != abacBundleSourceNetwork {
		t.Fatalf("304 后 source = %q, want %q", source, abacBundleSourceNetwork)
	}
	_, notModified, _, _, lastIfNoneMatch := srv.counts()
	if notModified != 1 {
		t.Fatalf("服务端 304 次数 = %d, want 1", notModified)
	}
	if lastIfNoneMatch != srv.etag {
		t.Fatalf("If-None-Match = %q, want %q", lastIfNoneMatch, srv.etag)
	}
}

// TestABACBundleClient_TamperedResponseDiscarded：坏签名 → 内存与磁盘 LKG 均不变。
func TestABACBundleClient_TamperedResponseDiscarded(t *testing.T) {
	dir := t.TempDir()
	srv, pub := newABACBundleTestServer(t, "agent-1", "k1")
	httpSrv := newTestABACBundleHTTP(t, srv)
	cfg := newTestABACBundleConfig(dir, "k1", pub)
	c := NewABACBundleClient(cfg, "agent-1", srv.secret, httpSrv.URL, nopABACBundleLogger{})

	c.fetch(context.Background())
	before, _ := c.Current()
	if before == nil {
		t.Fatal("首次拉取应成功")
	}
	beforeRaw, err := os.ReadFile(cfg.ABACBundleCachePath)
	if err != nil {
		t.Fatalf("读取缓存: %v", err)
	}

	srv.setTamper(true)
	c.fetch(context.Background())

	after, source := c.Current()
	if after == nil || after.Bundle.BundleID != before.Bundle.BundleID {
		t.Fatal("验签失败不得替换内存中的 LKG")
	}
	if source != abacBundleSourceNetwork {
		t.Fatalf("source = %q, want %q", source, abacBundleSourceNetwork)
	}
	afterRaw, err := os.ReadFile(cfg.ABACBundleCachePath)
	if err != nil {
		t.Fatalf("读取缓存: %v", err)
	}
	if !bytes.Equal(beforeRaw, afterRaw) {
		t.Fatal("验签失败的响应不得写入缓存")
	}
}

// TestABACBundleClient_RestartLoadsCache：重启（新客户端同缓存路径）→ LoadFromCache 得 source=cache。
func TestABACBundleClient_RestartLoadsCache(t *testing.T) {
	dir := t.TempDir()
	srv, pub := newABACBundleTestServer(t, "agent-1", "k1")
	httpSrv := newTestABACBundleHTTP(t, srv)
	cfg := newTestABACBundleConfig(dir, "k1", pub)

	first := NewABACBundleClient(cfg, "agent-1", srv.secret, httpSrv.URL, nopABACBundleLogger{})
	first.fetch(context.Background())
	want, _ := first.Current()
	if want == nil {
		t.Fatal("首次拉取应成功")
	}

	restarted := NewABACBundleClient(cfg, "agent-1", srv.secret, httpSrv.URL, nopABACBundleLogger{})
	restarted.LoadFromCache()
	got, source := restarted.Current()
	if got == nil {
		t.Fatal("重启后 LoadFromCache 应载入 LKG")
	}
	if source != abacBundleSourceCache {
		t.Fatalf("source = %q, want %q", source, abacBundleSourceCache)
	}
	if got.Bundle.BundleID != want.Bundle.BundleID {
		t.Fatalf("重启后 bundleID = %q, want %q", got.Bundle.BundleID, want.Bundle.BundleID)
	}
	if restarted.Expired() {
		t.Fatal("有效缓存不应标记过期")
	}
}

// TestABACBundleClient_ExpiredCacheRetained：过期缓存保留可用，Expired()=true。
func TestABACBundleClient_ExpiredCacheRetained(t *testing.T) {
	dir := t.TempDir()
	srv, pub := newABACBundleTestServer(t, "agent-1", "k1")
	cfg := newTestABACBundleConfig(dir, "k1", pub)

	now := time.Now().UTC()
	expired := srv.bundle
	expired.BundleID = "bundle-expired-0001"
	expired.IssuedAt = now.Add(-2 * time.Hour)
	expired.ExpiresAt = now.Add(-time.Hour)
	signed, err := abacbundle.Sign(expired, srv.priv)
	if err != nil {
		t.Fatalf("签名过期 bundle: %v", err)
	}
	raw, err := abacbundle.Encode(signed)
	if err != nil {
		t.Fatalf("编码过期 bundle: %v", err)
	}
	if err := os.WriteFile(cfg.ABACBundleCachePath, raw, 0o600); err != nil {
		t.Fatalf("写入缓存: %v", err)
	}

	c := NewABACBundleClient(cfg, "agent-1", srv.secret, "http://127.0.0.1:1", nopABACBundleLogger{})
	c.LoadFromCache()

	sb, source := c.Current()
	if sb == nil || sb.Bundle.BundleID != "bundle-expired-0001" {
		t.Fatal("过期缓存必须保留（不得删除/置空）")
	}
	if source != abacBundleSourceCache {
		t.Fatalf("source = %q, want %q", source, abacBundleSourceCache)
	}
	if !c.Expired() {
		t.Fatal("过期缓存必须报告 Expired()=true")
	}
	if _, err := os.Stat(cfg.ABACBundleCachePath); err != nil {
		t.Fatalf("过期缓存文件不得删除: %v", err)
	}
}

// TestABACBundleHeartbeatCapability：启用时心跳载荷包含 abac_bundle=v1；未启用不含该字段。
func TestABACBundleHeartbeatCapability(t *testing.T) {
	_, pub := newABACBundleTestServer(t, "agent-1", "k1")
	cfg := newTestABACBundleConfig(t.TempDir(), "k1", pub)

	enabled := NewABACBundleClient(cfg, "agent-1", "secret", "http://127.0.0.1:8080", nopABACBundleLogger{})
	a := &Agent{cfg: cfg, agentID: "agent-1", abacBundleClient: enabled}
	raw, err := json.Marshal(a.heartbeatRequest())
	if err != nil {
		t.Fatalf("marshal heartbeat: %v", err)
	}
	if !strings.Contains(string(raw), `"capabilities":["abac_bundle=v1"]`) {
		t.Fatalf("心跳载荷缺少能力声明: %s", raw)
	}

	disabledCfg := &config.Config{ABACBundleEnabled: false}
	disabled := NewABACBundleClient(disabledCfg, "agent-1", "secret", "http://127.0.0.1:8080", nopABACBundleLogger{})
	da := &Agent{cfg: disabledCfg, agentID: "agent-1", abacBundleClient: disabled}
	draw, err := json.Marshal(da.heartbeatRequest())
	if err != nil {
		t.Fatalf("marshal heartbeat(disabled): %v", err)
	}
	if strings.Contains(string(draw), "capabilities") {
		t.Fatalf("未启用时心跳不应携带 capabilities: %s", draw)
	}
}

// TestABACBundleClient_ContextCancelStopsLoop：ctx 取消后 Start 返回（无 goroutine 泄漏）。
func TestABACBundleClient_ContextCancelStopsLoop(t *testing.T) {
	dir := t.TempDir()
	srv, pub := newABACBundleTestServer(t, "agent-1", "k1")
	httpSrv := newTestABACBundleHTTP(t, srv)
	cfg := newTestABACBundleConfig(dir, "k1", pub)
	cfg.ABACBundleRefreshInterval = 50 * time.Millisecond
	c := NewABACBundleClient(cfg, "agent-1", srv.secret, httpSrv.URL, nopABACBundleLogger{})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Start(ctx)
	}()

	deadline := time.Now().Add(3 * time.Second)
	for {
		if reqs, _, _, _, _ := srv.counts(); reqs >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Start 未立即执行首次拉取")
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("ctx 取消后 Start 未退出（goroutine 泄漏）")
	}

	before, _, _, _, _ := srv.counts()
	time.Sleep(120 * time.Millisecond)
	after, _, _, _, _ := srv.counts()
	if after != before {
		t.Fatalf("取消后仍有拉取请求：before=%d after=%d", before, after)
	}
}
