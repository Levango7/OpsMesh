// acceptance_hardening_test.go — 批次30-32 验收硬化：异常输入 + 并发。
//
// 覆盖：
//
//	A1 联邦入站异常头（缺失/畸形时间戳/空或超长 nonce/篡改 body/错误密钥/签名路径不符）
//	   一律 401/403，不出现 5xx/panic；
//	A2 device-op 异常 op / 畸形 JSON / 超大 body(413) / allowlist 外 peerURL 不发起 HTTP；
//	A3 handleListUsers / handleListTenants 边界分页（0/负数/非数字/超大/pageSize 上限）；
//	A4 实例键校验（空白/分隔符/超长/控制字符）与审计 Detail 解析器抗畸形输入；
//	B6 相同签名设备操作并发重放：恰好一个 200，其余 403；
//	B7 多租户分页并发：响应不串租户、total/hasMore 稳定；
//	B8 同一 pipeline 模板并发提取命令：输出确定一致。
package controlplane

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	authpkg "github.com/Levango7/OpsMesh/pkg/auth"

	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// ---------------------------------------------------------------------------
// A1 联邦入站异常头
// ---------------------------------------------------------------------------

// TestFederationInbound_AbnormalHeaders 验证异常联邦头一律 401/403，绝不 500/panic。
// requireAuth=true 时缺失 X-Federation-Forwarded 的请求按普通未认证请求 401，
// 其余带 Forwarded=1 但签名材料异常/不匹配的请求由验签闸返回 403。
func TestFederationInbound_AbnormalHeaders(t *testing.T) {
	const secret = "fed-accept-abnormal-secret"
	s := newFederationDeviceTestServer(t, secret, true)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := []byte(`{"op":"cmdb-discover","payload":null}`)
	path := federationDeviceOpPath
	base := federationTestHeaders(t, secret, http.MethodPost, path, ts, "abnormal-hdr-nonce-1", "t1", "peer-user", "role-operator", body)

	tests := []struct {
		name string
		req  func() *http.Request
	}{
		{
			name: "missing_forwarded_header",
			req: func() *http.Request {
				h := base.Clone()
				h.Del(authpkg.FederationHeaderForwarded)
				return newFederationTestRequest(path, body, h)
			},
		},
		{
			name: "missing_signature_header",
			req: func() *http.Request {
				h := base.Clone()
				h.Del(authpkg.FederationHeaderSignature)
				return newFederationTestRequest(path, body, h)
			},
		},
		{
			name: "missing_timestamp_header",
			req: func() *http.Request {
				h := base.Clone()
				h.Del(authpkg.FederationHeaderTS)
				return newFederationTestRequest(path, body, h)
			},
		},
		{
			name: "missing_nonce_header",
			req: func() *http.Request {
				h := base.Clone()
				h.Del(authpkg.FederationHeaderNonce)
				return newFederationTestRequest(path, body, h)
			},
		},
		{
			name: "non_numeric_timestamp",
			req: func() *http.Request {
				h := federationTestHeaders(t, secret, http.MethodPost, path, "not-a-timestamp", "abnormal-hdr-nonce-2", "t1", "", "", body)
				return newFederationTestRequest(path, body, h)
			},
		},
		{
			name: "rfc3339_garbage_timestamp",
			req: func() *http.Request {
				h := federationTestHeaders(t, secret, http.MethodPost, path, "2026-99-99T99:99:99Z", "abnormal-hdr-nonce-3", "t1", "", "", body)
				return newFederationTestRequest(path, body, h)
			},
		},
		{
			name: "empty_nonce",
			req: func() *http.Request {
				h := federationTestHeaders(t, secret, http.MethodPost, path, ts, "", "t1", "", "", body)
				return newFederationTestRequest(path, body, h)
			},
		},
		{
			name: "oversized_nonce",
			req: func() *http.Request {
				h := federationTestHeaders(t, secret, http.MethodPost, path, ts, strings.Repeat("n", 4096), "t1", "", "", body)
				return newFederationTestRequest(path, body, h)
			},
		},
		{
			name: "nonce_with_space",
			req: func() *http.Request {
				h := federationTestHeaders(t, secret, http.MethodPost, path, ts, "bad nonce", "t1", "", "", body)
				return newFederationTestRequest(path, body, h)
			},
		},
		{
			name: "wrong_secret",
			req: func() *http.Request {
				h := federationTestHeaders(t, "wrong-secret", http.MethodPost, path, ts, "abnormal-hdr-nonce-4", "t1", "", "", body)
				return newFederationTestRequest(path, body, h)
			},
		},
		{
			name: "signature_computed_over_other_path",
			req: func() *http.Request {
				h := federationTestHeaders(t, secret, http.MethodPost, "/api/v1/federation/forward/task", ts, "abnormal-hdr-nonce-5", "t1", "", "", body)
				return newFederationTestRequest(path, body, h)
			},
		},
		{
			name: "truncated_body",
			req: func() *http.Request {
				h := federationTestHeaders(t, secret, http.MethodPost, path, ts, "abnormal-hdr-nonce-6", "t1", "", "", body)
				return newFederationTestRequest(path, body[:len(body)-5], h)
			},
		},
		{
			name: "tampered_body",
			req: func() *http.Request {
				h := federationTestHeaders(t, secret, http.MethodPost, path, ts, "abnormal-hdr-nonce-7", "t1", "", "", body)
				return newFederationTestRequest(path, []byte(`{"op":"retire"}`), h)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("handler panicked on abnormal federation input: %v", r)
					}
				}()
				s.handleFederationForwardDeviceOp(rec, tt.req())
			}()
			if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 401/403; body=%s", rec.Code, rec.Body.String())
			}
			if rec.Code >= 500 {
				t.Fatalf("abnormal input produced 5xx: %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

// newFederationTestRequest 构造带指定头的 device-op 请求。
func newFederationTestRequest(path string, body []byte, header http.Header) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header = header
	return req
}

// ---------------------------------------------------------------------------
// A2 device-op 异常输入
// ---------------------------------------------------------------------------

// TestFederationDeviceOp_AbnormalPayloads 验证 device-op 本地调用入口的异常输入：
// 未知/空 op、畸形 JSON → 400；超过 1 MiB → 413；均不进入转发/执行。
func TestFederationDeviceOp_AbnormalPayloads(t *testing.T) {
	s := newFederationDeviceTestServer(t, "fed-device-op-abnormal", false)
	s.cfg.Demo = true // 本地调用方走 requireProd 的 demo 放行路径

	oversized := `{"op":"metrics","payload":"` + strings.Repeat("A", maxBodyBytes+128) + `"}`

	tests := []struct {
		name string
		body string
		want int
	}{
		{"unknown_op", `{"op":"rm-rf","payload":null}`, http.StatusBadRequest},
		{"empty_op", `{"op":"","payload":null}`, http.StatusBadRequest},
		{"malformed_json", `{"op":"metrics"`, http.StatusBadRequest},
		{"oversized_body", oversized, http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, federationDeviceOpPath, strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("handler panicked: %v", r)
					}
				}()
				s.handleFederationForwardDeviceOp(rec, req)
			}()
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.want, rec.Body.String())
			}
			if rec.Code >= 500 {
				t.Fatalf("abnormal payload produced 5xx: %d", rec.Code)
			}
		})
	}
}

// TestFederationDeviceOp_UnknownPeerNoHTTP 验证 allowlist 外的 peerURL 在执行任何
// HTTP 请求前被拒绝：trip-wire server 必须收到 0 次请求。
func TestFederationDeviceOp_UnknownPeerNoHTTP(t *testing.T) {
	var hits int32
	tripwire := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer tripwire.Close()

	s := newFederationDeviceTestServer(t, "fed-device-op-ssrf", false)
	s.cfg.Demo = true

	body, _ := json.Marshal(map[string]interface{}{
		"peerURL": tripwire.URL,
		"op":      "metrics",
		"payload": []byte(`{"deviceID":"d1"}`),
	})
	req := httptest.NewRequest(http.MethodPost, federationDeviceOpPath, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleFederationForwardDeviceOp(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown peer status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if n := atomic.LoadInt32(&hits); n != 0 {
		t.Fatalf("trip-wire server received %d requests, want 0 (SSRF guard must reject before dialing)", n)
	}
}

// ---------------------------------------------------------------------------
// A3 分页边界
// ---------------------------------------------------------------------------

type paginatedUsersResp struct {
	Users    []json.RawMessage `json:"users"`
	Total    int               `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"pageSize"`
	HasMore  bool              `json:"hasMore"`
}

type paginatedTenantsResp struct {
	Tenants  []json.RawMessage `json:"tenants"`
	Total    int               `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"pageSize"`
	HasMore  bool              `json:"hasMore"`
}

func createAcceptanceUsers(t *testing.T, s *Server, tenant string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		u := &store.User{
			ID:           fmt.Sprintf("acc-user-%s-%d", tenant, i),
			Username:     fmt.Sprintf("acc-user-%s-%d", tenant, i),
			TenantID:     tenant,
			PasswordHash: "unused",
			Status:       "active",
			RoleIDs:      []string{"role-viewer"},
		}
		if s.store.CreateUser(u) == nil {
			t.Fatalf("CreateUser(%s) failed", u.Username)
		}
	}
}

// TestPaginationBoundary_Users 验证 handleListUsers 的边界输入不 panic、切片不越界，
// total/page/pageSize/hasMore 自洽。
func TestPaginationBoundary_Users(t *testing.T) {
	s := newAuthTestServer(t)
	auth := loginAsAdmin(t, s)
	const createdUsers = 45
	createAcceptanceUsers(t, s, store.DefaultTenantID, createdUsers)
	userCount := len(s.store.ListUsers()) // 含 newAuthTestServer 预置的 admin/operator/viewer

	cases := []struct {
		query        string
		wantPage     int
		wantPageSize int
	}{
		{"?page=0&pageSize=10", 1, 10},
		{"?page=-1&pageSize=10", 1, 10},
		{"?page=abc", 1, 20}, // 非数字 page 被当作第 1 页（ParsePagination 的既有语义）
		{"?page=1000000000&pageSize=10", 1000000000, 10},
		{"?page=1&pageSize=0", 1, 20},
		{"?page=1&pageSize=-5", 1, 20},
		{"?page=1&pageSize=100000", 1, 200},
		// page 被 clamp 到 maxInt/pageSize（maxInt=(1<<62)-1，pageSize 先被 clamp 到 200）。
		{"?page=9223372036854775807&pageSize=100000", ((1 << 62) - 1) / 200, 200},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			req := doWithAuth(http.MethodGet, "/api/v1/users"+tc.query, auth, nil)
			rec := httptest.NewRecorder()
			s.handleUsers(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			var resp paginatedUsersResp
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v; body=%s", err, rec.Body.String())
			}
			if tc.wantPage == 0 {
				if len(resp.Users) != userCount {
					t.Fatalf("unpaginated users = %d, want %d", len(resp.Users), userCount)
				}
				return
			}
			if resp.Page != tc.wantPage || resp.PageSize != tc.wantPageSize {
				t.Fatalf("page/pageSize = %d/%d, want %d/%d", resp.Page, resp.PageSize, tc.wantPage, tc.wantPageSize)
			}
			if resp.Total != userCount {
				t.Fatalf("total = %d, want %d", resp.Total, userCount)
			}
			start := (resp.Page - 1) * resp.PageSize
			if start > userCount {
				start = userCount
			}
			end := start + resp.PageSize
			if end > userCount {
				end = userCount
			}
			if len(resp.Users) != end-start {
				t.Fatalf("users len = %d, want %d (start=%d end=%d)", len(resp.Users), end-start, start, end)
			}
			if resp.HasMore != (end < userCount) {
				t.Fatalf("hasMore = %v, want %v (end=%d total=%d)", resp.HasMore, end < userCount, end, userCount)
			}
		})
	}
}

// TestPaginationBoundary_Tenants 验证 handleListTenants 的同一组分页边界。
func TestPaginationBoundary_Tenants(t *testing.T) {
	s := newAuthTestServer(t)
	auth := loginAsAdmin(t, s)
	const createdTenants = 33
	for i := 0; i < createdTenants; i++ {
		if s.store.CreateTenant(&store.Tenant{ID: fmt.Sprintf("acc-tnt-%02d", i), Name: fmt.Sprintf("acc-tnt-%02d", i)}) == nil {
			t.Fatalf("CreateTenant %d failed", i)
		}
	}
	tenantCount := len(s.store.ListTenants())

	cases := []struct {
		query        string
		wantPage     int
		wantPageSize int
	}{
		{"?page=0&pageSize=10", 1, 10},
		{"?page=-1&pageSize=10", 1, 10},
		{"?page=abc", 1, 20},
		{"?page=1000000000&pageSize=10", 1000000000, 10},
		{"?page=1&pageSize=0", 1, 20},
		{"?page=1&pageSize=-5", 1, 20},
		{"?page=1&pageSize=100000", 1, 200},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			req := doWithAuth(http.MethodGet, "/api/v1/tenants"+tc.query, auth, nil)
			rec := httptest.NewRecorder()
			s.handleTenants(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			var resp paginatedTenantsResp
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v; body=%s", err, rec.Body.String())
			}
			if tc.wantPage == 0 {
				if len(resp.Tenants) != tenantCount {
					t.Fatalf("unpaginated tenants = %d, want %d", len(resp.Tenants), tenantCount)
				}
				return
			}
			if resp.Page != tc.wantPage || resp.PageSize != tc.wantPageSize {
				t.Fatalf("page/pageSize = %d/%d, want %d/%d", resp.Page, resp.PageSize, tc.wantPage, tc.wantPageSize)
			}
			if resp.Total != tenantCount {
				t.Fatalf("total = %d, want %d", resp.Total, tenantCount)
			}
			start := (resp.Page - 1) * resp.PageSize
			if start > tenantCount {
				start = tenantCount
			}
			end := start + resp.PageSize
			if end > tenantCount {
				end = tenantCount
			}
			if len(resp.Tenants) != end-start {
				t.Fatalf("tenants len = %d, want %d", len(resp.Tenants), end-start)
			}
			if resp.HasMore != (end < tenantCount) {
				t.Fatalf("hasMore = %v, want %v", resp.HasMore, end < tenantCount)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// A4 实例键与审计 Detail 解析
// ---------------------------------------------------------------------------

// TestMiddlewareInstanceID_RejectsHostileValues 验证 URL 实例键：空白、'='、';'、
// 空、超长(10k)、控制字符 → 400，不进入任务下发。
func TestMiddlewareInstanceID_RejectsHostileValues(t *testing.T) {
	s := newMWTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	body := `{"agentID":"` + a.AgentID + `","templateID":"mysql","deployType":"docker","params":{"password":"pw"}}`

	ids := []string{
		"bad id",
		"a=b",
		"a;b",
		strings.Repeat("a", 10000),
		"a\x00b",
		"a\x01b",
		"a\u2028b",
	}
	for i, id := range ids {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-instances/"+url.PathEscape(id)+"/uninstall", strings.NewReader(body))
			rec := httptest.NewRecorder()
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("routing panicked on instance id: %v", r)
					}
				}()
				s.handleMiddlewareInstanceRouting(rec, req)
			}()
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("instance id %q status = %d, want 400; body=%s", id, rec.Code, rec.Body.String())
			}
		})
	}

	// 路径中空实例键（//uninstall）同样 400。
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-instances//uninstall", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleMiddlewareInstanceRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty instance id status = %d, want 400", rec.Code)
	}
}

// TestParseAuditDetail_HostileInputs 验证审计 Detail 解析器面对不平衡引号、空值、
// 重复键、未知键均不 panic，且忽略未知键、重复键后值覆盖。
func TestParseAuditDetail_HostileInputs(t *testing.T) {
	t.Run("unbalanced_quotes", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("parseAuditDetail panicked: %v", r)
			}
		}()
		got := parseAuditDetail(`template="mysql;agent="ag1`)
		if _, ok := got["evil"]; ok {
			t.Fatal("unknown key leaked from malformed detail")
		}
	})

	t.Run("empty_value", func(t *testing.T) {
		got := parseAuditDetail(`template="";agent="ag1";`)
		if _, ok := got["template"]; ok {
			t.Fatalf("empty value should be ignored, got template=%q", got["template"])
		}
		if got["agent"] != "ag1" {
			t.Fatalf("agent = %q, want ag1", got["agent"])
		}
	})

	t.Run("duplicated_keys_last_wins", func(t *testing.T) {
		got := parseAuditDetail(`template="first";template="second";`)
		if got["template"] != "second" {
			t.Fatalf("template = %q, want second (last wins)", got["template"])
		}
	})

	t.Run("unknown_keys_ignored", func(t *testing.T) {
		got := parseAuditDetail(`evil="x";template="mysql";agent="ag1";junk;`)
		if _, ok := got["evil"]; ok {
			t.Fatal("unknown key evil accepted")
		}
		if _, ok := got["junk"]; ok {
			t.Fatal("key without value accepted")
		}
		if got["template"] != "mysql" || got["agent"] != "ag1" {
			t.Fatalf("known keys lost: %+v", got)
		}
	})

	t.Run("legacy_space_format", func(t *testing.T) {
		got := parseAuditDetail(`template=mysql deployType=docker agent=ag1 version=8.0`)
		want := map[string]string{"template": "mysql", "deployType": "docker", "agent": "ag1", "version": "8.0"}
		for k, v := range want {
			if got[k] != v {
				t.Fatalf("%s = %q, want %q (%+v)", k, got[k], v, got)
			}
		}
	})

	t.Run("format_parse_roundtrip_special_values", func(t *testing.T) {
		detail := formatMiddlewareAuditDetail("template", "my;tpl=1", "agent", `a"b`, "version", "8.0")
		got := parseAuditDetail(detail)
		if got["template"] != "my;tpl=1" || got["agent"] != `a"b` || got["version"] != "8.0" {
			t.Fatalf("roundtrip mismatch: %+v (detail=%q)", got, detail)
		}
	})
}

// ---------------------------------------------------------------------------
// B6 并发相同签名请求
// ---------------------------------------------------------------------------

// seedDeviceMetrics 注册一台 agent 并为其写入一条指标，返回 deviceID。
func seedDeviceMetrics(t *testing.T, s *Server, tenant string) string {
	t.Helper()
	agent := s.store.Register(&proto.AgentInfo{Segment: "acc-seg", TenantID: tenant, Hostname: "acc-host"})
	if agent == nil || agent.AgentID == "" {
		t.Fatal("register agent failed")
	}
	deviceID := ""
	for _, devs := range s.store.Snapshot(tenant) {
		for _, d := range devs {
			if d.AgentID == agent.AgentID {
				deviceID = d.DeviceID
				break
			}
		}
	}
	if deviceID == "" {
		t.Fatal("device for registered agent not found")
	}
	s.store.StoreDeviceMetrics(deviceID, &proto.DeviceMetrics{DeviceID: deviceID})
	return deviceID
}

// TestFederationDeviceOp_ConcurrentReplayExactlyOne 验证同一签名设备操作并发提交：
// 恰好一个 200，其余 403（nonce 重放），不出现 5xx。
func TestFederationDeviceOp_ConcurrentReplayExactlyOne(t *testing.T) {
	const secret = "fed-concurrent-replay-secret"
	s := newFederationDeviceTestServer(t, secret, true)
	deviceID := seedDeviceMetrics(t, s, "t1")

	payload, _ := json.Marshal(map[string]string{"deviceID": deviceID})
	body, _ := json.Marshal(map[string]interface{}{"op": "metrics", "payload": payload})
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	header := federationTestHeaders(t, secret, http.MethodPost, federationDeviceOpPath, ts, "concurrent-replay-"+strconv.FormatInt(time.Now().UnixNano(), 10), "t1", "peer-user", "role-operator", body)

	const workers = 16
	var ok, replay, other int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			req := httptest.NewRequest(http.MethodPost, federationDeviceOpPath, bytes.NewReader(body))
			req.Header = header.Clone()
			rec := httptest.NewRecorder()
			s.handleFederationForwardDeviceOp(rec, req)
			switch rec.Code {
			case http.StatusOK:
				atomic.AddInt64(&ok, 1)
			case http.StatusForbidden:
				atomic.AddInt64(&replay, 1)
			default:
				atomic.AddInt64(&other, 1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := atomic.LoadInt64(&ok); got != 1 {
		t.Fatalf("accepted device-op = %d, want exactly 1", got)
	}
	if got := atomic.LoadInt64(&replay); got != workers-1 {
		t.Fatalf("replay device-op = %d, want %d", got, workers-1)
	}
	if got := atomic.LoadInt64(&other); got != 0 {
		t.Fatalf("unexpected device-op status count = %d, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// B7 多租户分页并发
// ---------------------------------------------------------------------------

// TestPaginationConcurrent_MultiTenantIsolation 验证不同租户/页码并发查询时，
// 响应不跨租户泄漏，total/hasMore 保持一致。
func TestPaginationConcurrent_MultiTenantIsolation(t *testing.T) {
	s := newAuthTestServer(t)
	tenants := []string{"acme", "bravo"}
	tokens := map[string]string{}
	const extraUsers = 12
	for _, tenant := range tenants {
		tokens[tenant] = createTenantAdmin(t, s, tenant)
		createAcceptanceUsers(t, s, tenant, extraUsers)
	}
	wantTotal := extraUsers + 1 // tenant admin 自己也属于该租户

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 32; i++ {
		tenant := tenants[i%len(tenants)]
		page := i%3 + 1
		wg.Add(1)
		go func(tenant, token string, page int) {
			defer wg.Done()
			<-start
			q := fmt.Sprintf("?page=%d&pageSize=5", page)
			req := doWithAuth(http.MethodGet, "/api/v1/users"+q, token, nil)
			rec := httptest.NewRecorder()
			s.handleUsers(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("%s page %d: status = %d body=%s", tenant, page, rec.Code, rec.Body.String())
				return
			}
			var resp paginatedUsersResp
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Errorf("%s page %d: decode: %v", tenant, page, err)
				return
			}
			if resp.Total != wantTotal {
				t.Errorf("%s page %d: total = %d, want %d", tenant, page, resp.Total, wantTotal)
			}
			startIdx := (page - 1) * resp.PageSize
			if startIdx > wantTotal {
				startIdx = wantTotal
			}
			endIdx := startIdx + resp.PageSize
			if endIdx > wantTotal {
				endIdx = wantTotal
			}
			if len(resp.Users) != endIdx-startIdx {
				t.Errorf("%s page %d: len = %d, want %d", tenant, page, len(resp.Users), endIdx-startIdx)
			}
			if resp.HasMore != (endIdx < wantTotal) {
				t.Errorf("%s page %d: hasMore = %v, want %v", tenant, page, resp.HasMore, endIdx < wantTotal)
			}
			for _, raw := range resp.Users {
				var u store.User
				if err := json.Unmarshal(raw, &u); err != nil {
					t.Errorf("%s page %d: decode user: %v", tenant, page, err)
					continue
				}
				if tenantOrDefault(u.TenantID) != tenant {
					t.Errorf("%s page %d: cross-tenant leak user=%s tenant=%s", tenant, page, u.Username, u.TenantID)
				}
			}
		}(tenant, tokens[tenant], page)
	}
	close(start)
	wg.Wait()
}

// ---------------------------------------------------------------------------
// B8 Pipeline 提取并发确定性
// ---------------------------------------------------------------------------

// TestExtractPipelineCommand_ConcurrentDeterministic 验证同一模板并发提取命令
// 输出完全一致（无共享可变状态）。
func TestExtractPipelineCommand_ConcurrentDeterministic(t *testing.T) {
	tpl := &store.PipelineTemplate{
		ID:   "acc-pipeline",
		Name: "acc pipeline",
		YAML: "steps:\n  - name: build\n    script: |\n      echo one\n      echo \"two three\"\n      for i in 1 2 3; do echo $i; done\n",
	}
	want := extractPipelineCommand(tpl)
	if !strings.Contains(want, "echo one") || !strings.Contains(want, "for i in 1 2 3") {
		t.Fatalf("unexpected baseline extraction: %q", want)
	}

	const workers = 64
	var mismatch int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if got := extractPipelineCommand(tpl); got != want {
				atomic.AddInt64(&mismatch, 1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if got := atomic.LoadInt64(&mismatch); got != 0 {
		t.Fatalf("%d/%d concurrent extractions diverged from baseline", got, workers)
	}
}
