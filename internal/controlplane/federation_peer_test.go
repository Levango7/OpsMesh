package controlplane

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	authpkg "github.com/Levango7/OpsMesh/pkg/auth"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/store"
)

const (
	federationPeerAURL = "http://peer-a.example:8080"
	federationPeerBURL = "http://peer-b.example:8080"
)

// newPeerSecretManager 构造启用 per-peer 密钥的 FederationManager（无实际网络调用）。
func newPeerSecretManager(t *testing.T, peers []string, localPeerID string, entries []string, globalSecret string) *FederationManager {
	t.Helper()
	peerSecrets, err := authpkg.ParseFederationPeerSecrets(entries)
	if err != nil {
		t.Fatalf("ParseFederationPeerSecrets: %v", err)
	}
	opts := []FederationManagerOption{WithFederationPeerID(localPeerID)}
	if len(peerSecrets) > 0 {
		opts = append(opts, WithFederationPeerSecrets(peerSecrets))
	}
	fed := NewFederationManager(peers, store.NewMemoryStore(), globalSecret, nil, opts...)
	if fed == nil {
		t.Fatal("NewFederationManager returned nil")
	}
	return fed
}

// signPeerRequest 对目标 peer 的转发出站请求签名（只构造请求，不发送）。
func signPeerRequest(t *testing.T, fed *FederationManager, target string, body []byte) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target+"/api/v1/tasks", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-User-Id", "u1")
	req.Header.Set("X-User-Roles", "role-admin")
	if err := fed.signFederationRequest(req, body); err != nil {
		t.Fatalf("signFederationRequest(%s): %v", target, err)
	}
	return req
}

// TestFederationPeerSecrets_PerPeerIsolation 验证两个 peer 使用不同密钥：
// 发往 peerA 的签名只被 peerA 密钥接受，peerB 密钥拒绝（反之亦然）。
func TestFederationPeerSecrets_PerPeerIsolation(t *testing.T) {
	fed := newPeerSecretManager(t, []string{federationPeerAURL, federationPeerBURL}, "cp-local", []string{
		"peer-a@" + federationPeerAURL + "=secret-a",
		"peer-b@" + federationPeerBURL + "=secret-b",
	}, "")
	body := []byte(`{"agentID":"a1","command":"echo hi"}`)

	reqA := signPeerRequest(t, fed, federationPeerAURL, body)
	if got := reqA.Header.Get(authpkg.FederationHeaderPeerID); got != "cp-local" {
		t.Fatalf("X-Federation-Peer-ID = %q, want cp-local", got)
	}
	if err := authpkg.VerifyFederationSignature([]byte("secret-a"), time.Minute, reqA.Method, reqA.URL.Path, reqA.Header, body, nil, time.Now()); err != nil {
		t.Fatalf("peerA signature rejected by secret-a: %v", err)
	}
	if err := authpkg.VerifyFederationSignature([]byte("secret-b"), time.Minute, reqA.Method, reqA.URL.Path, reqA.Header, body, nil, time.Now()); !errors.Is(err, authpkg.ErrFederationSignature) {
		t.Fatalf("peerA signature accepted by secret-b: err=%v", err)
	}

	reqB := signPeerRequest(t, fed, federationPeerBURL, body)
	if err := authpkg.VerifyFederationSignature([]byte("secret-b"), time.Minute, reqB.Method, reqB.URL.Path, reqB.Header, body, nil, time.Now()); err != nil {
		t.Fatalf("peerB signature rejected by secret-b: %v", err)
	}
	if err := authpkg.VerifyFederationSignature([]byte("secret-a"), time.Minute, reqB.Method, reqB.URL.Path, reqB.Header, body, nil, time.Now()); !errors.Is(err, authpkg.ErrFederationSignature) {
		t.Fatalf("peerB signature accepted by secret-a: err=%v", err)
	}
}

// TestFederationVerification_PerPeerIsolation 验证接收方按 X-Federation-Peer-ID 选密钥：
// peerA 身份只接受 peerA 的 secret，peerB 的 secret（即使全局存在）一律 403 语义。
func TestFederationVerification_PerPeerIsolation(t *testing.T) {
	s := &Server{
		store: store.NewMemoryStore(),
		cfg: &config.Config{FederationPeerSecrets: []string{
			"peer-a@" + federationPeerAURL + "=secret-a",
			"peer-b@" + federationPeerBURL + "=secret-b",
		}},
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := []byte(`{"agentID":"a1","command":"echo hi"}`)
	path := "/api/v1/tasks"

	signed := func(secret, nonce, peerID string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		req.Header = federationTestHeaders(t, secret, http.MethodPost, path, ts, nonce, "t1", "u1", "role-admin", body)
		req.Header.Set(authpkg.FederationHeaderPeerID, peerID)
		return req
	}

	if err := s.verifyFederationRequest(signed("secret-a", "peer-iso-nonce-1", "peer-a")); err != nil {
		t.Fatalf("peer-a signed request rejected: %v", err)
	}
	if err := s.verifyFederationRequest(signed("secret-b", "peer-iso-nonce-2", "peer-a")); !errors.Is(err, authpkg.ErrFederationSignature) {
		t.Fatalf("peer-a header with peer-b secret err = %v, want ErrFederationSignature", err)
	}
	if err := s.verifyFederationRequest(signed("secret-a", "peer-iso-nonce-3", "peer-b")); !errors.Is(err, authpkg.ErrFederationSignature) {
		t.Fatalf("peer-b header with peer-a secret err = %v, want ErrFederationSignature", err)
	}
	if err := s.verifyFederationRequest(signed("secret-b", "peer-iso-nonce-4", "peer-b")); err != nil {
		t.Fatalf("peer-b signed request rejected: %v", err)
	}
}

// TestFederationRotation_SignsWithFirstAcceptsSecond 验证轮换语义：
// 签名固定使用 Secrets[0]，接收方接受该 peer 的任一 secret（含后追加的旧密钥）。
func TestFederationRotation_SignsWithFirstAcceptsSecond(t *testing.T) {
	entries := []string{"peer-a@" + federationPeerAURL + "=s1,s2"}
	fed := newPeerSecretManager(t, []string{federationPeerAURL}, "cp-local", entries, "")
	body := []byte(`{"agentID":"a1"}`)

	req := signPeerRequest(t, fed, federationPeerAURL, body)
	canonical := authpkg.FederationCanonicalString(
		req.Method,
		req.URL.Path,
		req.Header.Get(authpkg.FederationHeaderTS),
		req.Header.Get(authpkg.FederationHeaderNonce),
		"t1", "u1", "role-admin",
		authpkg.FederationBodySHA256(body),
	)
	if got, want := req.Header.Get(authpkg.FederationHeaderSignature), authpkg.SignFederation("s1", canonical); got != want {
		t.Fatalf("signer did not use first secret: got=%q want=%q", got, want)
	}

	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{FederationPeerSecrets: entries}}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	newSigned := func(secret, nonce string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", bytes.NewReader(body))
		r.Header = federationTestHeaders(t, secret, http.MethodPost, "/api/v1/tasks", ts, nonce, "t1", "u1", "role-admin", body)
		r.Header.Set(authpkg.FederationHeaderPeerID, "peer-a")
		return r
	}
	if err := s.verifyFederationRequest(newSigned("s1", "rotation-nonce-1")); err != nil {
		t.Fatalf("first secret rejected: %v", err)
	}
	if err := s.verifyFederationRequest(newSigned("s2", "rotation-nonce-2")); err != nil {
		t.Fatalf("rotated (second) secret rejected: %v", err)
	}
	if err := s.verifyFederationRequest(newSigned("s3", "rotation-nonce-3")); !errors.Is(err, authpkg.ErrFederationSignature) {
		t.Fatalf("unknown secret err = %v, want ErrFederationSignature", err)
	}
}

// TestFederationVerification_FallbackToGlobal 验证向后兼容：
// 缺 X-Federation-Peer-ID 或 peerID 未配置时回退全局共享密钥。
func TestFederationVerification_FallbackToGlobal(t *testing.T) {
	s := &Server{
		store: store.NewMemoryStore(),
		cfg: &config.Config{
			FederationSecret:      "legacy-global",
			FederationPeerSecrets: []string{"peer-a@" + federationPeerAURL + "=secret-a"},
		},
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := []byte(`{"agentID":"a1"}`)
	path := "/api/v1/tasks"

	newSigned := func(secret, nonce, peerID string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		req.Header = federationTestHeaders(t, secret, http.MethodPost, path, ts, nonce, "t1", "u1", "role-admin", body)
		if peerID != "" {
			req.Header.Set(authpkg.FederationHeaderPeerID, peerID)
		}
		return req
	}

	if err := s.verifyFederationRequest(newSigned("legacy-global", "fallback-nonce-1", "")); err != nil {
		t.Fatalf("absent peer ID header did not fall back to global secret: %v", err)
	}
	if err := s.verifyFederationRequest(newSigned("legacy-global", "fallback-nonce-2", "ghost-peer")); err != nil {
		t.Fatalf("unknown peer ID did not fall back to global secret: %v", err)
	}
	if err := s.verifyFederationRequest(newSigned("secret-a", "fallback-nonce-3", "ghost-peer")); !errors.Is(err, authpkg.ErrFederationSignature) {
		t.Fatalf("unknown peer ID accepted a per-peer secret: err=%v, want ErrFederationSignature", err)
	}
}

// TestFederationSigning_NoSecretReturnsError 验证无任何密钥时拒绝发送未签名请求；
// 同时验证默认本地 peer ID 与全局密钥的旧行为。
func TestFederationSigning_NoSecretReturnsError(t *testing.T) {
	body := []byte(`{"agentID":"a1"}`)

	fedNoSecret := NewFederationManager([]string{federationPeerAURL}, store.NewMemoryStore(), "", nil)
	if fedNoSecret == nil {
		t.Fatal("NewFederationManager returned nil")
	}
	req := httptest.NewRequest(http.MethodPost, federationPeerAURL+"/api/v1/tasks", bytes.NewReader(body))
	err := fedNoSecret.signFederationRequest(req, body)
	if !errors.Is(err, authpkg.ErrFederationSecretMissing) {
		t.Fatalf("no-secret sign err = %v, want ErrFederationSecretMissing", err)
	}
	if sig := req.Header.Get(authpkg.FederationHeaderSignature); sig != "" {
		t.Fatalf("unsigned request carries signature header %q", sig)
	}

	fedGlobal := NewFederationManager([]string{federationPeerAURL}, store.NewMemoryStore(), "legacy-global", nil)
	req2 := signPeerRequest(t, fedGlobal, federationPeerAURL, body)
	if got := req2.Header.Get(authpkg.FederationHeaderPeerID); got != "default" {
		t.Fatalf("default peer ID header = %q, want default", got)
	}
	if err := authpkg.VerifyFederationSignature([]byte("legacy-global"), time.Minute, req2.Method, req2.URL.Path, req2.Header, body, nil, time.Now()); err != nil {
		t.Fatalf("global-secret signature rejected: %v", err)
	}

	fedPeerOnly := newPeerSecretManager(t, []string{federationPeerAURL, federationPeerBURL}, "cp-local",
		[]string{"peer-a@" + federationPeerAURL + "=secret-a"}, "")
	req3 := httptest.NewRequest(http.MethodPost, federationPeerBURL+"/api/v1/tasks", bytes.NewReader(body))
	if err := fedPeerOnly.signFederationRequest(req3, body); !errors.Is(err, authpkg.ErrFederationSecretMissing) {
		t.Fatalf("peer without per-peer entry and no global secret err = %v, want ErrFederationSecretMissing", err)
	}
}

// TestFederationManagerFromConfig_Wiring 验证配置到管理器的装配：
// 本地 peer ID 与 per-peer 密钥均生效。
func TestFederationManagerFromConfig_Wiring(t *testing.T) {
	cfg := &config.Config{
		FederationPeers:       []string{federationPeerAURL},
		FederationPeerID:      "cp-1",
		FederationPeerSecrets: []string{"peer-a@" + federationPeerAURL + "=cfg-secret"},
	}
	fed, err := NewFederationManagerFromConfig(cfg, store.NewMemoryStore(), nil)
	if err != nil {
		t.Fatalf("NewFederationManagerFromConfig: %v", err)
	}
	if fed == nil {
		t.Fatal("NewFederationManagerFromConfig returned nil for non-empty peers")
	}
	body := []byte(`{"agentID":"a1"}`)
	req := signPeerRequest(t, fed, federationPeerAURL, body)
	if got := req.Header.Get(authpkg.FederationHeaderPeerID); got != "cp-1" {
		t.Fatalf("X-Federation-Peer-ID = %q, want cp-1", got)
	}
	if err := authpkg.VerifyFederationSignature([]byte("cfg-secret"), time.Minute, req.Method, req.URL.Path, req.Header, body, nil, time.Now()); err != nil {
		t.Fatalf("config-derived signature rejected: %v", err)
	}

	emptyCfg := &config.Config{}
	fedEmpty, err := NewFederationManagerFromConfig(emptyCfg, store.NewMemoryStore(), nil)
	if err != nil || fedEmpty != nil {
		t.Fatalf("empty peers: fed=%v err=%v, want (nil, nil)", fedEmpty, err)
	}
}

// TestFederationServerWiring_UsesPerPeerSecrets 验证 server.go 的实际装配路径（NewServer）：
// 出站签名使用目标 peer 的首个 secret（而非全局密钥），并携带本地 peer 身份头。
// 回归背景：server 曾直接用四参数 NewFederationManager(cfg.FederationPeers, st, cfg.FederationSecret, tls)，
// 导致 per-peer 密钥与 X-Federation-Peer-ID 从未在出站方向生效。
func TestFederationServerWiring_UsesPerPeerSecrets(t *testing.T) {
	cfg := &config.Config{
		Mode:                  "controlplane",
		Store:                 "memory",
		EventBus:              "noop",
		TaskMaxRetries:        3,
		FederationPeers:       []string{federationPeerAURL},
		FederationPeerID:      "cp-wired",
		FederationSecret:      "legacy-global",
		FederationPeerSecrets: []string{"peer-a@" + federationPeerAURL + "=peer-secret-1,peer-secret-2"},
	}
	s, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if s.fed == nil {
		t.Fatal("NewServer did not wire FederationManager for non-empty peers")
	}
	body := []byte(`{"agentID":"a1"}`)
	req := httptest.NewRequest(http.MethodPost, federationPeerAURL+"/api/v1/tasks", bytes.NewReader(body))
	if err := s.fed.signFederationRequest(req, body); err != nil {
		t.Fatalf("signFederationRequest: %v", err)
	}
	if got := req.Header.Get(authpkg.FederationHeaderPeerID); got != "cp-wired" {
		t.Fatalf("X-Federation-Peer-ID = %q, want cp-wired", got)
	}
	if err := authpkg.VerifyFederationSignature([]byte("peer-secret-1"), time.Minute, req.Method, req.URL.Path, req.Header, body, nil, time.Now()); err != nil {
		t.Fatalf("server-wired signature rejected by peer's first secret: %v", err)
	}
	if err := authpkg.VerifyFederationSignature([]byte("legacy-global"), time.Minute, req.Method, req.URL.Path, req.Header, body, nil, time.Now()); !errors.Is(err, authpkg.ErrFederationSignature) {
		t.Fatalf("server-wired signature accepted by global secret: err=%v", err)
	}

	// peers 为空时保持 nil（路由层跳过 /api/v1/federation/* 注册）。
	emptyCfg := &config.Config{Mode: "controlplane", Store: "memory", EventBus: "noop", TaskMaxRetries: 3}
	sEmpty, err := NewServer(emptyCfg)
	if err != nil {
		t.Fatalf("NewServer(empty federation): %v", err)
	}
	if sEmpty.fed != nil {
		t.Fatalf("NewServer wired FederationManager with empty peers: %v", sEmpty.fed)
	}
}

// TestFederationVerification_NoSecretsConfigured 验证接收方无密钥时拒绝联邦请求。
func TestFederationVerification_NoSecretsConfigured(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{}}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body, _ := json.Marshal(map[string]string{"agentID": "a1"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", bytes.NewReader(body))
	req.Header = federationTestHeaders(t, "any", http.MethodPost, "/api/v1/tasks", ts, "no-secret-nonce-1", "t1", "", "", body)
	req.Header.Set(authpkg.FederationHeaderPeerID, "peer-a")
	if err := s.verifyFederationRequest(req); err == nil {
		t.Fatal("federation request accepted with no configured secrets")
	}
}

// TestFederationAllowlist_SignedPeerPaths 验证 R2 入站路径白名单：
// 验签通过但路径不在白名单的联邦请求必须 403，且 handler 不得执行；
// 默认白名单内的联邦/设备/任务路径放行。
func TestFederationAllowlist_SignedPeerPaths(t *testing.T) {
	const secret = "fed-allowlist-secret"
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{FederationSecret: secret}}
	ts := strconv.FormatInt(time.Now().Unix(), 10)

	newSigned := func(path, nonce string) *http.Request {
		body := []byte(`{"agentID":"a1"}`)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		req.Header = federationTestHeaders(t, secret, http.MethodPost, path, ts, nonce, "t1", "peer-user", "role-admin", body)
		return req
	}

	// 1) 已签名但调用用户管理端点：403 + 指定错误 + handler 未执行。
	handlerReached := false
	rec := httptest.NewRecorder()
	req := newSigned("/api/v1/users", "allowlist-denied-"+strconv.FormatInt(time.Now().UnixNano(), 10))
	if _, ok := s.requireProd(rec, req, "user:delete"); ok {
		handlerReached = true
	}
	if handlerReached {
		t.Fatal("handler reached for federation request to /api/v1/users")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("disallowed path status = %d, want 403, body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.Contains(got, `"error":"federation path not allowed"`) {
		t.Fatalf("disallowed path body = %q", got)
	}

	// 2) 默认白名单内的路径全部放行。
	for i, path := range []string{"/api/v1/federation/devices", "/api/v1/devices", "/api/v1/tasks"} {
		rec := httptest.NewRecorder()
		nonce := "allowlist-ok-" + strconv.Itoa(i) + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
		if _, ok := s.requireProd(rec, newSigned(path, nonce), "task:write"); !ok {
			t.Fatalf("allowed path %s rejected: status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

// TestFederationAllowlist_NonFederationUnchanged 验证非联邦请求行为不变：
// 无身份仍按原语义 401，不因新增路径白名单提前放行或改变状态码。
func TestFederationAllowlist_NonFederationUnchanged(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{FederationSecret: "fed-allowlist-secret"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	if _, ok := s.requireProd(rec, req, "user:read"); ok {
		t.Fatal("non-federation request without identity was allowed")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("non-federation status = %d, want 401, body=%s", rec.Code, rec.Body.String())
	}
}

// TestFederationPathAllowed_Traversal 验证白名单前缀匹配前先 path.Clean：
// /api/v1/tasks/../users 不能被 /api/v1/tasks 前缀放行。
func TestFederationPathAllowed_Traversal(t *testing.T) {
	s := &Server{cfg: &config.Config{}}
	if s.federationPathAllowed("/api/v1/tasks/../users") {
		t.Fatal("traversal path normalized into allowed prefix")
	}
	if !s.federationPathAllowed("/api/v1/tasks") || !s.federationPathAllowed("/api/v1/devices/abc") {
		t.Fatal("default allowlist rejected expected prefixes")
	}
}
