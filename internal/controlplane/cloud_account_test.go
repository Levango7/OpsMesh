package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider"
	"github.com/Levango7/OpsMesh/internal/secrets"
	"github.com/Levango7/OpsMesh/internal/store"
)

// fakeCloudProvider 是可注入发现结果/失败的云厂商适配器，仅供云账号 handler 测试。
type fakeCloudProvider struct {
	*cloudprovider.NotImplementedProvider
	providerType string
	counts       [4]int // instances, vpcs, subnets, clusters
	fail         bool
	regions      []string
	regionsErr   error
}

func (p *fakeCloudProvider) ProviderType() cloudprovider.ProviderType {
	return cloudprovider.ProviderType(p.providerType)
}

func (p *fakeCloudProvider) discover(n int) ([]cloudprovider.Resource, error) {
	if p.fail {
		return nil, errors.New("provider discovery failed")
	}
	out := make([]cloudprovider.Resource, n)
	for i := range out {
		out[i].ID = fmt.Sprintf("res-%d", i)
	}
	return out, nil
}

func (p *fakeCloudProvider) ListRegions(context.Context, cloudprovider.Credentials) ([]cloudprovider.Region, error) {
	if p.regionsErr != nil {
		return nil, p.regionsErr
	}
	out := make([]cloudprovider.Region, 0, len(p.regions))
	for _, id := range p.regions {
		out = append(out, cloudprovider.Region{ID: id})
	}
	return out, nil
}

func (p *fakeCloudProvider) DiscoverInstances(context.Context, cloudprovider.Credentials, string) ([]cloudprovider.Resource, error) {
	return p.discover(p.counts[0])
}

func (p *fakeCloudProvider) DiscoverVPCs(context.Context, cloudprovider.Credentials, string) ([]cloudprovider.Resource, error) {
	return p.discover(p.counts[1])
}

func (p *fakeCloudProvider) DiscoverSubnets(context.Context, cloudprovider.Credentials, string, string) ([]cloudprovider.Resource, error) {
	return p.discover(p.counts[2])
}

func (p *fakeCloudProvider) DiscoverClusters(context.Context, cloudprovider.Credentials, string) ([]cloudprovider.Resource, error) {
	return p.discover(p.counts[3])
}

func newCloudSyncTestServer(t *testing.T, p cloudprovider.CloudProvider, acct *store.CloudAccount) (*Server, *store.MemoryStore) {
	t.Helper()
	st := store.NewMemoryStore()
	s := &Server{
		store:                st,
		cfg:                  &config.Config{TaskMaxRetries: 3, Demo: true},
		requireAuth:          false,
		secretProvider:       secrets.NewEnvProvider(""),
		cloudProviderFactory: cloudprovider.NewAdapterFactory(),
	}
	if p != nil {
		s.cloudProviderFactory.Register(p)
	}
	if err := st.CreateCloudAccount(context.Background(), acct); err != nil {
		t.Fatalf("create cloud account: %v", err)
	}
	return s, st
}

func newSyncRequest(accountID, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cloud-accounts/"+accountID+"/sync", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// TestSyncCloudAccount_ProviderFailure 验证厂商发现失败时响应 status=failed，
// 且调用方请求体里的 discoveredCount 不得落库。
func TestSyncCloudAccount_ProviderFailure(t *testing.T) {
	acct := &store.CloudAccount{
		ID: "acct-fail", TenantID: "default", Name: "fake", Provider: "fake",
		Region: "r1", CredentialsRef: "AK:SK",
	}
	s, st := newCloudSyncTestServer(t, &fakeCloudProvider{providerType: "fake", fail: true}, acct)

	rec := httptest.NewRecorder()
	s.handleSyncCloudAccount(rec, newSyncRequest("acct-fail", `{"syncStatus":"synced","discoveredCount":4242}`), "acct-fail")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d, want 502; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "failed" {
		t.Errorf("response status=%q, want failed", resp["status"])
	}
	if resp["discoveredCount"] != "0" {
		t.Errorf("response discoveredCount=%q, want 0", resp["discoveredCount"])
	}
	got, err := st.GetCloudAccount(context.Background(), "acct-fail")
	if err != nil || got == nil {
		t.Fatalf("get account: %v", err)
	}
	if got.SyncStatus != "failed" {
		t.Errorf("persisted syncStatus=%q, want failed", got.SyncStatus)
	}
	if got.DiscoveredCount != 0 {
		t.Errorf("persisted discoveredCount=%d, want 0 (body value must not be trusted)", got.DiscoveredCount)
	}
}

// TestSyncCloudAccount_ProviderLookupFailure 验证厂商未注册时按 failed 落库，
// 不采信请求体的 discoveredCount。
func TestSyncCloudAccount_ProviderLookupFailure(t *testing.T) {
	acct := &store.CloudAccount{
		ID: "acct-noprov", TenantID: "default", Name: "unknown", Provider: "not-registered",
		Region: "r1", CredentialsRef: "AK:SK",
	}
	s, st := newCloudSyncTestServer(t, nil, acct)

	rec := httptest.NewRecorder()
	s.handleSyncCloudAccount(rec, newSyncRequest("acct-noprov", `{"syncStatus":"synced","discoveredCount":777}`), "acct-noprov")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d, want 502; body=%s", rec.Code, rec.Body.String())
	}
	got, err := st.GetCloudAccount(context.Background(), "acct-noprov")
	if err != nil || got == nil {
		t.Fatalf("get account: %v", err)
	}
	if got.SyncStatus != "failed" || got.DiscoveredCount != 0 {
		t.Fatalf("persisted (status=%q, count=%d), want (failed, 0)", got.SyncStatus, got.DiscoveredCount)
	}
}

// TestSyncCloudAccount_Success 验证成功发现时响应 status=synced，计数为真实发现总数。
func TestSyncCloudAccount_Success(t *testing.T) {
	acct := &store.CloudAccount{
		ID: "acct-ok", TenantID: "default", Name: "fake", Provider: "fake",
		Region: "r1", CredentialsRef: "AK:SK",
	}
	s, st := newCloudSyncTestServer(t, &fakeCloudProvider{
		providerType: "fake",
		counts:       [4]int{2, 1, 1, 1},
	}, acct)

	rec := httptest.NewRecorder()
	s.handleSyncCloudAccount(rec, newSyncRequest("acct-ok", `{"syncStatus":"failed","discoveredCount":9999}`), "acct-ok")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "synced" {
		t.Errorf("response status=%q, want synced", resp["status"])
	}
	if resp["discoveredCount"] != "5" {
		t.Errorf("response discoveredCount=%q, want 5", resp["discoveredCount"])
	}
	got, err := st.GetCloudAccount(context.Background(), "acct-ok")
	if err != nil || got == nil {
		t.Fatalf("get account: %v", err)
	}
	if got.SyncStatus != "synced" {
		t.Errorf("persisted syncStatus=%q, want synced", got.SyncStatus)
	}
	if got.DiscoveredCount != 5 {
		t.Errorf("persisted discoveredCount=%d, want 5", got.DiscoveredCount)
	}
}

// regionsResponse 是 GET /cloud-accounts/{id}/regions 的响应体投影。
type regionsResponse struct {
	Provider string   `json:"provider"`
	Regions  []string `json:"regions"`
	Source   string   `json:"source"`
	Warning  string   `json:"warning"`
	Error    string   `json:"error"`
}

func getRegions(t *testing.T, s *Server, accountID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cloud-accounts/"+accountID+"/regions", nil)
	rec := httptest.NewRecorder()
	s.handleListCloudAccountRegions(rec, req, accountID)
	return rec
}

func decodeRegions(t *testing.T, rec *httptest.ResponseRecorder) regionsResponse {
	t.Helper()
	var resp regionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode regions response: %v; body=%s", err, rec.Body.String())
	}
	return resp
}

// TestListCloudAccountRegions_ProviderSuccess 验证实时调用成功时 source=provider，
// 区域取自厂商返回值且不出现 warning（调用方不会把实时数据误判为兜底）。
func TestListCloudAccountRegions_ProviderSuccess(t *testing.T) {
	acct := &store.CloudAccount{
		ID: "acct-reg-live", TenantID: "default", Name: "fake", Provider: "fake",
		Region: "r1", CredentialsRef: "AK:SK",
	}
	s, _ := newCloudSyncTestServer(t, &fakeCloudProvider{
		providerType: "fake",
		regions:      []string{"live-1", "live-2"},
	}, acct)

	rec := getRegions(t, s, "acct-reg-live")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeRegions(t, rec)
	if resp.Source != cloudRegionSourceProvider {
		t.Errorf("source=%q, want %q", resp.Source, cloudRegionSourceProvider)
	}
	if resp.Warning != "" {
		t.Errorf("unexpected warning on provider success: %q", resp.Warning)
	}
	if len(resp.Regions) != 2 || resp.Regions[0] != "live-1" || resp.Regions[1] != "live-2" {
		t.Errorf("regions=%v, want [live-1 live-2]", resp.Regions)
	}
}

// TestListCloudAccountRegions_ProviderFailureFallsBackStatic 验证厂商失败时仍返回 200，
// 但显式标记 source=static + warning，且 warning 已脱敏（不含凭据值）。
func TestListCloudAccountRegions_ProviderFailureFallsBackStatic(t *testing.T) {
	acct := &store.CloudAccount{
		ID: "acct-reg-fail", TenantID: "default", Name: "aliyun", Provider: "aliyun",
		Region: "cn-hangzhou", CredentialsRef: "AK:SK",
	}
	s, _ := newCloudSyncTestServer(t, &fakeCloudProvider{
		providerType: "aliyun",
		regionsErr:   errors.New("upstream 503 body={\"error\":\"busy\"} accessKeySecret=SECRETVALUE"),
	}, acct)

	rec := getRegions(t, s, "acct-reg-fail")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200（有静态兜底仍可用）; body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeRegions(t, rec)
	if resp.Source != cloudRegionSourceStatic {
		t.Errorf("source=%q, want %q", resp.Source, cloudRegionSourceStatic)
	}
	if !strings.HasPrefix(resp.Warning, "provider unavailable: ") {
		t.Errorf("warning=%q, want prefix %q", resp.Warning, "provider unavailable: ")
	}
	if strings.Contains(resp.Warning, "SECRETVALUE") {
		t.Errorf("warning leaked credential value: %q", resp.Warning)
	}
	want := cloudProviderRegions("aliyun")
	if len(resp.Regions) != len(want) {
		t.Fatalf("regions=%v, want static fallback %v", resp.Regions, want)
	}
	for i := range want {
		if resp.Regions[i] != want[i] {
			t.Errorf("regions[%d]=%q, want %q", i, resp.Regions[i], want[i])
		}
	}
}

// TestListCloudAccountRegions_NoFallbackReturns502 验证厂商未注册且无静态兜底时
// 返回 502，而不是 200 + 空列表冒充成功。
func TestListCloudAccountRegions_NoFallbackReturns502(t *testing.T) {
	acct := &store.CloudAccount{
		ID: "acct-reg-unknown", TenantID: "default", Name: "unknown", Provider: "not-registered",
		Region: "r1", CredentialsRef: "AK:SK",
	}
	s, _ := newCloudSyncTestServer(t, nil, acct)

	rec := getRegions(t, s, "acct-reg-unknown")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d, want 502; body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeRegions(t, rec)
	if resp.Error != "provider regions unavailable" {
		t.Errorf("error=%q, want %q", resp.Error, "provider regions unavailable")
	}
	if len(resp.Regions) != 0 || resp.Source != "" {
		t.Errorf("regions=%v source=%q, want empty on 502", resp.Regions, resp.Source)
	}
}

// TestCloudAccountRouting_MethodSemantics 表驱动：已识别子资源 + 错误动词 ⇒ 405 + Allow；
// 未知子路径 ⇒ 404（不带 Allow）。
func TestCloudAccountRouting_MethodSemantics(t *testing.T) {
	cases := []struct {
		name      string
		method    string
		path      string
		wantAllow string // 非空：期望 405 + 该 Allow；空：期望 404
	}{
		{"bare-wrong-method", http.MethodPost, "/api/v1/cloud-accounts/acc-1", "GET, PUT, DELETE"},
		{"sync-wrong-method", http.MethodGet, "/api/v1/cloud-accounts/acc-1/sync", "POST"},
		{"test-wrong-method", http.MethodDelete, "/api/v1/cloud-accounts/acc-1/test", "POST"},
		{"regions-wrong-method", http.MethodPost, "/api/v1/cloud-accounts/acc-1/regions", "GET"},
		{"unknown-subpath-get", http.MethodGet, "/api/v1/cloud-accounts/acc-1/nope", ""},
		{"unknown-subpath-post", http.MethodPost, "/api/v1/cloud-accounts/acc-1/nope", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newExtraTestServer()
			req := httptest.NewRequest(c.method, c.path, nil)
			rec := httptest.NewRecorder()
			s.handleCloudAccountRouting(rec, req)

			if c.wantAllow == "" {
				if rec.Code != http.StatusNotFound {
					t.Fatalf("%s %s = %d, want 404（未知子路径）; body=%s", c.method, c.path, rec.Code, rec.Body.String())
				}
				if got := rec.Header().Get("Allow"); got != "" {
					t.Fatalf("404 响应不应带 Allow 头，got %q", got)
				}
				return
			}
			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s %s = %d, want 405（已知子资源 + 错误动词）; body=%s", c.method, c.path, rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Allow"); got != c.wantAllow {
				t.Fatalf("%s %s Allow = %q, want %q", c.method, c.path, got, c.wantAllow)
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("405 响应体非 JSON: %v; body=%s", err, rec.Body.String())
			}
			if body["error"] == "" {
				t.Fatalf("405 响应缺 error 字段: %s", rec.Body.String())
			}
		})
	}
}

// TestCloudAccountRouting_CorrectMethodsUnaffected 正动词仍分派到原 handler：
// GET/PUT/DELETE 裸 {id}、POST sync/test、GET regions 均不被 405 拦截。
func TestCloudAccountRouting_CorrectMethodsUnaffected(t *testing.T) {
	acct := &store.CloudAccount{
		ID: "acct-1", TenantID: "default", Name: "fake", Provider: "fake",
		Region: "r1", CredentialsRef: "AK:SK",
	}
	s, _ := newCloudSyncTestServer(t, &fakeCloudProvider{
		providerType: "fake",
		counts:       [4]int{1, 0, 0, 0},
		regions:      []string{"r1"},
	}, acct)

	do := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.handleCloudAccountRouting(rec, req)
		return rec
	}

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"get-bare", http.MethodGet, "/api/v1/cloud-accounts/acct-1", "", http.StatusOK},
		{"put-bare", http.MethodPut, "/api/v1/cloud-accounts/acct-1", `{"name":"renamed"}`, http.StatusOK},
		{"get-regions", http.MethodGet, "/api/v1/cloud-accounts/acct-1/regions", "", http.StatusOK},
		{"post-sync", http.MethodPost, "/api/v1/cloud-accounts/acct-1/sync", `{}`, http.StatusOK},
		{"post-test", http.MethodPost, "/api/v1/cloud-accounts/acct-1/test", `{}`, http.StatusOK},
		{"delete-bare", http.MethodDelete, "/api/v1/cloud-accounts/acct-1", "", http.StatusNoContent},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := do(c.method, c.path, c.body)
			if rec.Code != c.want {
				t.Fatalf("%s %s = %d, want %d; body=%s", c.method, c.path, rec.Code, c.want, rec.Body.String())
			}
		})
	}
}
