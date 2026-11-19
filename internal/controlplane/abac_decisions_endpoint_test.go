// abac_decisions_endpoint_test.go 验证 ABAC 决策审计回传端点（M3）的契约：
// per-agent HMAC（含 body digest）认证矩阵、请求体限制、字段校验、租户隔离与存储闸。
package controlplane

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// newABACDecisionsTestServer 构造带内存 store 的测试 Server。
func newABACDecisionsTestServer(t *testing.T) *Server {
	t.Helper()
	return &Server{store: store.NewMemoryStore()}
}

// abacDecisionsTestRegisterAgent 注册租户下的 agent 并返回 agentID。
func abacDecisionsTestRegisterAgent(t *testing.T, s *Server, tenantID string) string {
	t.Helper()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-test", TenantID: tenantID})
	if a == nil || a.AgentID == "" {
		t.Fatal("register agent failed")
	}
	return a.AgentID
}

// abacDecisionsTestSign 计算契约规定的签名：HMAC(secret, "abac-decisions\n"+agentID+"\n"+ts+"\n"+hex(sha256(body)))。
func abacDecisionsTestSign(t *testing.T, s *Server, agentID string, ts int64, body []byte) string {
	t.Helper()
	secret := s.store.AgentSecret(agentID)
	if secret == "" {
		t.Fatalf("agent %q has no secret", agentID)
	}
	digest := sha256.Sum256(body)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(abacDecisionSignaturePrefix + "\n" + agentID + "\n" +
		strconv.FormatInt(ts, 10) + "\n" + hex.EncodeToString(digest[:])))
	return hex.EncodeToString(mac.Sum(nil))
}

// abacDecisionsTestRequest 构造带签名的 POST 请求。
func abacDecisionsTestRequest(t *testing.T, s *Server, agentID string, body []byte) *http.Request {
	t.Helper()
	ts := time.Now().Unix()
	req := httptest.NewRequest(http.MethodPost, abacDecisionsRoute, bytes.NewReader(body))
	req.Header.Set(agentHMACAgentIDHeader, agentID)
	req.Header.Set(agentHMACAgentTimestampHeader, strconv.FormatInt(ts, 10))
	req.Header.Set(agentHMACAgentSignatureHeader, abacDecisionsTestSign(t, s, agentID, ts, body))
	return req
}

// abacDecisionsTestBody 返回一批合法决策（1 allow + 1 deny）。
func abacDecisionsTestBody(t *testing.T) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]interface{}{
		"decisions": []map[string]interface{}{
			{
				"requestId":  "req-1",
				"bundleId":   "bundle-1",
				"policyId":   "policy-allow",
				"allowed":    true,
				"reason":     "match allow",
				"mode":       abacDecisionModeDevice,
				"occurredAt": "2026-11-17T12:00:00Z",
			},
			{
				"requestId":  "req-2",
				"bundleId":   "bundle-1",
				"policyId":   "",
				"allowed":    false,
				"reason":     "default deny",
				"mode":       abacDecisionModeDeviceOffline,
				"occurredAt": "2026-11-17T11:59:00Z",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return body
}

// TestABACDecision_AcceptedAndPersisted 覆盖：合法批次 202 + accepted 计数 + 身份回填/截断。
func TestABACDecision_AcceptedAndPersisted(t *testing.T) {
	s := newABACDecisionsTestServer(t)
	agentID := abacDecisionsTestRegisterAgent(t, s, "tenant-a")

	longReason := strings.Repeat("x", abacDecisionMaxReasonLen+100)
	body, err := json.Marshal(map[string]interface{}{
		"decisions": []map[string]interface{}{
			{
				"requestId":  strings.Repeat("r", abacDecisionMaxIDLen+50),
				"bundleId":   "bundle-1",
				"policyId":   "policy-1",
				"allowed":    true,
				"reason":     longReason,
				"mode":       abacDecisionModeDevice,
				"occurredAt": "2026-11-17T12:00:00Z",
				// 伪装的 tenant 必须被忽略（身份只取 store.Agent）。
				"tenantID": "tenant-b",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	rec := httptest.NewRecorder()
	s.handleAgentABACDecisions(rec, abacDecisionsTestRequest(t, s, agentID, body))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s, want 202", rec.Code, rec.Body.String())
	}
	var resp map[string]int
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["accepted"] != 1 {
		t.Fatalf("accepted=%d, want 1", resp["accepted"])
	}

	logs := s.store.ListABACDecisionLogs("tenant-a", 10)
	if len(logs) != 1 {
		t.Fatalf("tenant-a logs=%d, want 1", len(logs))
	}
	got := logs[0]
	if got.TenantID != "tenant-a" || got.AgentID != agentID {
		t.Fatalf("identity tenant=%q agent=%q, want tenant-a/%s", got.TenantID, got.AgentID, agentID)
	}
	if len([]rune(got.RequestID)) != abacDecisionMaxIDLen {
		t.Fatalf("requestId len=%d, want truncated %d", len([]rune(got.RequestID)), abacDecisionMaxIDLen)
	}
	if len([]rune(got.Reason)) != abacDecisionMaxReasonLen {
		t.Fatalf("reason len=%d, want truncated %d", len([]rune(got.Reason)), abacDecisionMaxReasonLen)
	}
	if got.ReceivedAt.IsZero() {
		t.Fatal("receivedAt must be set")
	}
	if other := s.store.ListABACDecisionLogs("tenant-b", 10); len(other) != 0 {
		t.Fatalf("tenant-b logs=%d, want 0 (body tenant must be ignored)", len(other))
	}
}

// TestABACDecision_AcceptedCount 覆盖：多条决策 accepted 与列表倒序。
func TestABACDecision_AcceptedCount(t *testing.T) {
	s := newABACDecisionsTestServer(t)
	agentID := abacDecisionsTestRegisterAgent(t, s, "tenant-a")

	rec := httptest.NewRecorder()
	s.handleAgentABACDecisions(rec, abacDecisionsTestRequest(t, s, agentID, abacDecisionsTestBody(t)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s, want 202", rec.Code, rec.Body.String())
	}
	var resp map[string]int
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["accepted"] != 2 {
		t.Fatalf("accepted=%d, want 2", resp["accepted"])
	}
	logs := s.store.ListABACDecisionLogs("tenant-a", 10)
	if len(logs) != 2 {
		t.Fatalf("logs=%d, want 2", len(logs))
	}
	if logs[0].RequestID != "req-2" || logs[1].RequestID != "req-1" {
		t.Fatalf("order=%q,%q, want newest (req-2) first", logs[0].RequestID, logs[1].RequestID)
	}
}

// TestABACDecision_AuthMatrix 覆盖：缺头/错签/未知 agent/body 被篡改/时钟超窗统一 401。
func TestABACDecision_AuthMatrix(t *testing.T) {
	s := newABACDecisionsTestServer(t)
	agentID := abacDecisionsTestRegisterAgent(t, s, "tenant-a")
	body := abacDecisionsTestBody(t)

	cases := []struct {
		name string
		req  *http.Request
	}{
		{
			name: "no headers",
			req:  httptest.NewRequest(http.MethodPost, abacDecisionsRoute, bytes.NewReader(body)),
		},
		{
			name: "bad signature",
			req: func() *http.Request {
				req := abacDecisionsTestRequest(t, s, agentID, body)
				req.Header.Set(agentHMACAgentSignatureHeader, "deadbeef")
				return req
			}(),
		},
		{
			name: "unknown agent",
			req: func() *http.Request {
				req := abacDecisionsTestRequest(t, s, agentID, body)
				req.Header.Set(agentHMACAgentIDHeader, "ghost")
				return req
			}(),
		},
		{
			name: "body tampered after signing",
			req: func() *http.Request {
				req := abacDecisionsTestRequest(t, s, agentID, body)
				req.Body = httptest.NewRequest(http.MethodPost, abacDecisionsRoute, bytes.NewReader(append(body, ' '))).Body
				return req
			}(),
		},
		{
			name: "timestamp out of skew",
			req: func() *http.Request {
				ts := time.Now().Add(-10 * time.Minute).Unix()
				req := httptest.NewRequest(http.MethodPost, abacDecisionsRoute, bytes.NewReader(body))
				req.Header.Set(agentHMACAgentIDHeader, agentID)
				req.Header.Set(agentHMACAgentTimestampHeader, strconv.FormatInt(ts, 10))
				req.Header.Set(agentHMACAgentSignatureHeader, abacDecisionsTestSign(t, s, agentID, ts, body))
				return req
			}(),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.handleAgentABACDecisions(rec, tc.req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d body=%s, want 401", rec.Code, rec.Body.String())
			}
		})
	}
	if got := s.store.ListABACDecisionLogs("", 10); len(got) != 0 {
		t.Fatalf("unauthorized requests must not persist logs; got %d", len(got))
	}
}

// TestABACDecision_BodyTooLarge 覆盖：>1 MiB 请求体 413。
func TestABACDecision_BodyTooLarge(t *testing.T) {
	s := newABACDecisionsTestServer(t)
	agentID := abacDecisionsTestRegisterAgent(t, s, "tenant-a")
	body := bytes.Repeat([]byte("a"), abacDecisionMaxBodyBytes+1)

	rec := httptest.NewRecorder()
	s.handleAgentABACDecisions(rec, abacDecisionsTestRequest(t, s, agentID, body))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s, want 413", rec.Code, rec.Body.String())
	}
}

// TestABACDecision_TooManyDecisions 覆盖：>500 条 400。
func TestABACDecision_TooManyDecisions(t *testing.T) {
	s := newABACDecisionsTestServer(t)
	agentID := abacDecisionsTestRegisterAgent(t, s, "tenant-a")
	items := make([]map[string]interface{}, 0, abacDecisionMaxBatch+1)
	for i := 0; i < abacDecisionMaxBatch+1; i++ {
		items = append(items, map[string]interface{}{
			"requestId":  fmt.Sprintf("req-%d", i),
			"bundleId":   "bundle-1",
			"allowed":    true,
			"mode":       abacDecisionModeDevice,
			"occurredAt": "2026-11-17T12:00:00Z",
		})
	}
	body, err := json.Marshal(map[string]interface{}{"decisions": items})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	rec := httptest.NewRecorder()
	s.handleAgentABACDecisions(rec, abacDecisionsTestRequest(t, s, agentID, body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
	}
}

// TestABACDecision_Malformed 覆盖：非法 JSON/缺字段/非法 mode/非法 occurredAt 一律 400。
func TestABACDecision_Malformed(t *testing.T) {
	s := newABACDecisionsTestServer(t)
	agentID := abacDecisionsTestRegisterAgent(t, s, "tenant-a")

	cases := []struct {
		name string
		body string
	}{
		{"invalid json", `{"decisions":[`},
		{"missing decisions", `{}`},
		{"decisions null", `{"decisions":null}`},
		{"invalid mode", `{"decisions":[{"mode":"online","occurredAt":"2026-11-17T12:00:00Z"}]}`},
		{"invalid occurredAt", `{"decisions":[{"mode":"device","occurredAt":"not-a-time"}]}`},
		{"missing occurredAt", `{"decisions":[{"mode":"device"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.handleAgentABACDecisions(rec, abacDecisionsTestRequest(t, s, agentID, []byte(tc.body)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestABACDecision_TenantIsolationOnList 覆盖：两个租户各自回传，列表查询互不可见。
func TestABACDecision_TenantIsolationOnList(t *testing.T) {
	s := newABACDecisionsTestServer(t)
	agentA := abacDecisionsTestRegisterAgent(t, s, "tenant-a")
	agentB := abacDecisionsTestRegisterAgent(t, s, "tenant-b")

	for _, agentID := range []string{agentA, agentB} {
		rec := httptest.NewRecorder()
		s.handleAgentABACDecisions(rec, abacDecisionsTestRequest(t, s, agentID, abacDecisionsTestBody(t)))
		if rec.Code != http.StatusAccepted {
			t.Fatalf("agent %s status=%d body=%s", agentID, rec.Code, rec.Body.String())
		}
	}

	a := s.store.ListABACDecisionLogs("tenant-a", 10)
	b := s.store.ListABACDecisionLogs("tenant-b", 10)
	if len(a) != 2 || len(b) != 2 {
		t.Fatalf("tenant-a=%d tenant-b=%d, want 2/2", len(a), len(b))
	}
	for _, l := range a {
		if l.TenantID != "tenant-a" {
			t.Fatalf("tenant-a list leaked %q", l.TenantID)
		}
	}
	for _, l := range b {
		if l.TenantID != "tenant-b" {
			t.Fatalf("tenant-b list leaked %q", l.TenantID)
		}
	}
	if all := s.store.ListABACDecisionLogs("", 10); len(all) != 4 {
		t.Fatalf("platform logs=%d, want 4", len(all))
	}
}

// TestABACDecision_MethodNotAllowed 覆盖：非 POST 405 + Allow。
func TestABACDecision_MethodNotAllowed(t *testing.T) {
	s := newABACDecisionsTestServer(t)
	rec := httptest.NewRecorder()
	s.handleAgentABACDecisions(rec, httptest.NewRequest(http.MethodGet, abacDecisionsRoute, nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != http.MethodPost {
		t.Fatalf("Allow=%q, want POST", got)
	}
}

// TestABACDecision_StoreMissing503 覆盖：store 未配置时 503（不得静默丢弃）。
func TestABACDecision_StoreMissing503(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleAgentABACDecisions(rec, httptest.NewRequest(http.MethodPost, abacDecisionsRoute, bytes.NewReader(abacDecisionsTestBody(t))))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s, want 503", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp["code"] != "store_unavailable" {
		t.Fatalf("code=%q, want store_unavailable", resp["code"])
	}
}

// TestABACDecision_ProductionDefaultDenyReachesHandler 覆盖生产链路：
// 默认拒绝中间件不得用 JWT 规则拦掉 agent HMAC 通道。
func TestABACDecision_ProductionDefaultDenyReachesHandler(t *testing.T) {
	s := newABACDecisionsTestServer(t)
	s.requireAuth = true
	agentID := abacDecisionsTestRegisterAgent(t, s, "tenant-a")

	mux := http.NewServeMux()
	mux.HandleFunc(abacDecisionsRoute, s.handleAgentABACDecisions)
	handler := s.defaultDenyMiddleware(mux)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, abacDecisionsTestRequest(t, s, agentID, abacDecisionsTestBody(t)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("authorized status=%d body=%s, want 202", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, abacDecisionsRoute, bytes.NewReader(abacDecisionsTestBody(t))))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d, want 401", rec.Code)
	}
}
