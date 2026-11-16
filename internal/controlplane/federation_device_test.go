package controlplane

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	authpkg "github.com/Levango7/OpsMesh/pkg/auth"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/store"
)

// TestSignFederationRequest_Nonce 验证出站签名请求携带 X-Federation-Nonce，
// 且两次内容相同的请求使用不同 nonce（nonce 必须随机，不能由请求内容派生）。
func TestSignFederationRequest_Nonce(t *testing.T) {
	const secret = "fed-sign-nonce-secret"
	fed := NewFederationManager([]string{"http://peer.example:8080"}, store.NewMemoryStore(), secret, nil)
	if fed == nil {
		t.Fatal("NewFederationManager returned nil")
	}
	body := []byte(`{"agentID":"a1","command":"echo hi"}`)
	newSigned := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "http://peer.example:8080/api/v1/tasks", bytes.NewReader(body))
		req.Header.Set("X-Tenant-ID", "t1")
		req.Header.Set("X-User-Id", "u1")
		req.Header.Set("X-User-Roles", "role-admin")
		if err := fed.signFederationRequest(req, body); err != nil {
			t.Fatalf("signFederationRequest: %v", err)
		}
		return req
	}
	req1 := newSigned()
	req2 := newSigned()

	nonce1 := req1.Header.Get(authpkg.FederationHeaderNonce)
	nonce2 := req2.Header.Get(authpkg.FederationHeaderNonce)
	if nonce1 == "" {
		t.Fatal("outbound request missing X-Federation-Nonce")
	}
	if len(nonce1) != 32 {
		t.Fatalf("nonce %q length = %d, want 32 hex chars (16 bytes)", nonce1, len(nonce1))
	}
	if _, err := hex.DecodeString(nonce1); err != nil {
		t.Fatalf("nonce %q is not hex: %v", nonce1, err)
	}
	if nonce1 == nonce2 {
		t.Fatalf("identical requests reused nonce %q, want distinct nonces", nonce1)
	}
	if req1.Header.Get(authpkg.FederationHeaderTS) == "" || req1.Header.Get(authpkg.FederationHeaderSignature) == "" {
		t.Fatal("outbound request missing ts/signature headers")
	}
	// pkg/auth 能验证该出站签名（同一 canonical，nil cache = 不检查重放）。
	if err := authpkg.VerifyFederationSignature([]byte(secret), time.Minute, req1.Method, req1.URL.Path, req1.Header, body, nil, time.Now()); err != nil {
		t.Fatalf("outbound signature failed verification: %v", err)
	}
}

// newFederationDeviceTestServer 构造启用联邦的测试 Server（内存 store + 单个 allowlist peer）。
func newFederationDeviceTestServer(t *testing.T, secret string, requireAuth bool) *Server {
	t.Helper()
	st := store.NewMemoryStore()
	return &Server{
		store:       st,
		cfg:         &config.Config{FederationSecret: secret, FederationPeers: []string{"http://peer.example:8080"}},
		fed:         NewFederationManager([]string{"http://peer.example:8080"}, st, secret, nil),
		requireAuth: requireAuth,
	}
}

// TestFederationDeviceOp_InboundReplayRejected 验证同一份带签名请求重放：
// 第一次进入 handler（执行 cmdb-discover，collector 未初始化 → 503），
// 第二次同 nonce 必须被 NonceCache 拒绝 → 403。
func TestFederationDeviceOp_InboundReplayRejected(t *testing.T) {
	const secret = "fed-replay-secret"
	s := newFederationDeviceTestServer(t, secret, false)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body, _ := json.Marshal(map[string]interface{}{"op": "cmdb-discover", "payload": nil})
	nonce := "replay-nonce-" + strconv.FormatInt(time.Now().UnixNano(), 10) // 进程级 NonceCache 跨 -count 复用，nonce 必须逐次唯一

	newReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, federationDeviceOpPath, bytes.NewReader(body))
		req.Header = federationTestHeaders(t, secret, http.MethodPost, federationDeviceOpPath, ts, nonce, "t1", "peer-user", "role-operator", body)
		return req
	}

	first := httptest.NewRecorder()
	s.handleFederationForwardDeviceOp(first, newReq())
	if first.Code == http.StatusForbidden {
		t.Fatalf("first signed request unexpectedly rejected as replay: body=%s", first.Body.String())
	}

	second := httptest.NewRecorder()
	s.handleFederationForwardDeviceOp(second, newReq())
	if second.Code != http.StatusForbidden {
		t.Fatalf("replayed request status = %d, want 403, body=%s", second.Code, second.Body.String())
	}
}

// TestFederationDeviceOp_SkewExceeded 验证时间戳超出配置窗口 → 403。
func TestFederationDeviceOp_SkewExceeded(t *testing.T) {
	const secret = "fed-skew-secret"
	s := newFederationDeviceTestServer(t, secret, false)
	s.cfg.FederationMaxSkewSeconds = 1
	ts := strconv.FormatInt(time.Now().Add(-10*time.Second).Unix(), 10)
	body, _ := json.Marshal(map[string]interface{}{"op": "metrics", "payload": []byte(`{"deviceID":"d1"}`)})
	req := httptest.NewRequest(http.MethodPost, federationDeviceOpPath, bytes.NewReader(body))
	req.Header = federationTestHeaders(t, secret, http.MethodPost, federationDeviceOpPath, ts, "skew-nonce-0001", "t1", "", "", body)
	rec := httptest.NewRecorder()
	s.handleFederationForwardDeviceOp(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("stale signature status = %d, want 403, body=%s", rec.Code, rec.Body.String())
	}

	// 直接验签同样应返回 ErrFederationSkew（便于调用方区分原因）。
	direct := httptest.NewRequest(http.MethodPost, federationDeviceOpPath, bytes.NewReader(body))
	direct.Header = federationTestHeaders(t, secret, http.MethodPost, federationDeviceOpPath, ts, "skew-nonce-0002", "t1", "", "", body)
	if err := s.verifyFederationRequest(direct); !errors.Is(err, authpkg.ErrFederationSkew) {
		t.Fatalf("verifyFederationRequest err = %v, want ErrFederationSkew", err)
	}
}

// TestForwardDeviceOp_HappyPath 验证设备操作转发：路径、签名头（含 nonce）、身份头、
// 请求体 {op,payload} 与 peer 响应 body 均正确透传。
func TestForwardDeviceOp_HappyPath(t *testing.T) {
	const secret = "fed-device-op-secret"
	type captured struct {
		method  string
		path    string
		header  http.Header
		body    []byte
		nonce   string
		op      string
		payload []byte
	}
	gotCh := make(chan captured, 1)
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Op      string `json:"op"`
			Payload []byte `json:"payload"`
		}
		_ = json.Unmarshal(raw, &body)
		gotCh <- captured{
			method:  r.Method,
			path:    r.URL.Path,
			header:  r.Header.Clone(),
			body:    raw,
			nonce:   r.Header.Get(authpkg.FederationHeaderNonce),
			op:      body.Op,
			payload: body.Payload,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"result":"peer-ok"}`))
	}))
	defer peer.Close()

	fed := NewFederationManager([]string{peer.URL}, store.NewMemoryStore(), secret, nil)
	payload := []byte(`{"deviceID":"dev-1","range":"1h"}`)
	respBody, err := fed.ForwardDeviceOp(context.Background(), peer.URL, "metrics", payload, "t1", "u1", []string{"role-admin", "role-operator"})
	if err != nil {
		t.Fatalf("ForwardDeviceOp: %v", err)
	}
	if string(respBody) != `{"result":"peer-ok"}` {
		t.Fatalf("response body = %q, want peer body passthrough", respBody)
	}

	got := <-gotCh
	if got.method != http.MethodPost {
		t.Errorf("method = %s, want POST", got.method)
	}
	if got.path != federationDeviceOpPath {
		t.Errorf("path = %s, want %s", got.path, federationDeviceOpPath)
	}
	if got.header.Get(authpkg.FederationHeaderForwarded) != "1" {
		t.Error("missing X-Federation-Forwarded=1")
	}
	if got.nonce == "" {
		t.Error("missing X-Federation-Nonce")
	}
	if got.header.Get(authpkg.FederationHeaderTS) == "" || got.header.Get(authpkg.FederationHeaderSignature) == "" {
		t.Error("missing federation ts/signature headers")
	}
	if got.header.Get("X-Tenant-ID") != "t1" || got.header.Get("X-User-Id") != "u1" {
		t.Errorf("identity headers = tenant %q user %q", got.header.Get("X-Tenant-ID"), got.header.Get("X-User-Id"))
	}
	if got.header.Get("X-User-Roles") != "role-admin,role-operator" {
		t.Errorf("roles header = %q", got.header.Get("X-User-Roles"))
	}
	if got.op != "metrics" {
		t.Errorf("op = %q, want metrics", got.op)
	}
	if string(got.payload) != string(payload) {
		t.Errorf("payload = %q, want %q", got.payload, payload)
	}
	if err := authpkg.VerifyFederationSignature([]byte(secret), time.Minute, got.method, got.path, got.header, got.body, nil, time.Now()); err != nil {
		t.Fatalf("peer-side signature verification failed: %v", err)
	}
}

// TestForwardDeviceOp_UnknownPeerRejected 验证未配置的 peerURL 在发起任何 HTTP 请求前被拒绝。
func TestForwardDeviceOp_UnknownPeerRejected(t *testing.T) {
	var hits int32
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer peer.Close()

	fed := NewFederationManager([]string{"http://allowed.example:8080"}, store.NewMemoryStore(), "s", nil)
	if _, err := fed.ForwardDeviceOp(context.Background(), peer.URL, "metrics", []byte(`{"deviceID":"d1"}`), "t1", "u1", nil); err == nil {
		t.Fatal("ForwardDeviceOp accepted unknown peer, want error")
	}
	if n := atomic.LoadInt32(&hits); n != 0 {
		t.Fatalf("unknown peer received %d HTTP requests, want 0 (SSRF guard must reject before dialing)", n)
	}
}

// TestForwardDeviceOp_OpWhitelist 验证 op 白名单：未列出的操作被拒绝。
func TestForwardDeviceOp_OpWhitelist(t *testing.T) {
	var hits int32
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer peer.Close()

	fed := NewFederationManager([]string{peer.URL}, store.NewMemoryStore(), "s", nil)
	if _, err := fed.ForwardDeviceOp(context.Background(), peer.URL, "rm-rf", nil, "t1", "u1", nil); err == nil {
		t.Fatal("ForwardDeviceOp accepted non-whitelisted op, want error")
	}
	if n := atomic.LoadInt32(&hits); n != 0 {
		t.Fatalf("peer received %d requests for non-whitelisted op, want 0", n)
	}
}

// TestFederationPeerOps_Restricted 验证 per-peer 设备操作限制：
// peer-a 配置 ;ops=retire 后调用 provision 必须 403；未声明 ops 的 peer-b 行为不变。
func TestFederationPeerOps_Restricted(t *testing.T) {
	entries := []string{
		"peer-a@" + federationPeerAURL + "=secret-a;ops=retire",
		"peer-b@" + federationPeerBURL + "=secret-b",
	}
	st := store.NewMemoryStore()
	s := &Server{
		store: st,
		cfg: &config.Config{
			FederationPeers:       []string{federationPeerAURL, federationPeerBURL},
			FederationPeerSecrets: entries,
		},
		fed: NewFederationManager([]string{federationPeerAURL, federationPeerBURL}, st, "", nil,
			WithFederationPeerSecrets(mustParsePeerSecrets(t, entries))),
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body, _ := json.Marshal(map[string]interface{}{"op": "provision", "payload": []byte(`{"deviceID":"d1"}`)})

	newSigned := func(secret, peerID string) *http.Request {
		nonce := "peer-ops-" + peerID + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
		req := httptest.NewRequest(http.MethodPost, federationDeviceOpPath, bytes.NewReader(body))
		req.Header = federationTestHeaders(t, secret, http.MethodPost, federationDeviceOpPath, ts, nonce, "t1", "peer-user", "role-operator", body)
		req.Header.Set(authpkg.FederationHeaderPeerID, peerID)
		return req
	}

	// peer-a 被限制为 retire：provision 必须 403，且不进入执行链。
	recA := httptest.NewRecorder()
	s.handleFederationForwardDeviceOp(recA, newSigned("secret-a", "peer-a"))
	if recA.Code != http.StatusForbidden {
		t.Fatalf("restricted peer provision status = %d, want 403, body=%s", recA.Code, recA.Body.String())
	}
	if got := recA.Body.String(); !strings.Contains(got, "not allowed for this op") {
		t.Fatalf("restricted peer body = %q", got)
	}

	// peer-b 未声明 ops：保持全局白名单语义，不得被 403 拦截。
	recB := httptest.NewRecorder()
	s.handleFederationForwardDeviceOp(recB, newSigned("secret-b", "peer-b"))
	if recB.Code == http.StatusForbidden {
		t.Fatalf("unrestricted peer provision unexpectedly 403: body=%s", recB.Body.String())
	}

	// 限制列表中允许的 retire 不因 ops 校验被拒（后续执行结果可为 404）。
	retireBody, _ := json.Marshal(map[string]interface{}{"op": "retire", "payload": []byte(`{"deviceID":"d1"}`)})
	reqRetire := httptest.NewRequest(http.MethodPost, federationDeviceOpPath, bytes.NewReader(retireBody))
	reqRetire.Header = federationTestHeaders(t, "secret-a", http.MethodPost, federationDeviceOpPath, ts,
		"peer-ops-retire-"+strconv.FormatInt(time.Now().UnixNano(), 10), "t1", "peer-user", "role-operator", retireBody)
	reqRetire.Header.Set(authpkg.FederationHeaderPeerID, "peer-a")
	recRetire := httptest.NewRecorder()
	s.handleFederationForwardDeviceOp(recRetire, reqRetire)
	if recRetire.Code == http.StatusForbidden {
		t.Fatalf("allowed retire op unexpectedly 403: body=%s", recRetire.Body.String())
	}
}

// mustParsePeerSecrets 解析 per-peer 条目，失败即终止测试。
func mustParsePeerSecrets(t *testing.T, entries []string) []authpkg.FederationPeerSecret {
	t.Helper()
	peers, err := authpkg.ParseFederationPeerSecrets(entries)
	if err != nil {
		t.Fatalf("ParseFederationPeerSecrets: %v", err)
	}
	return peers
}
