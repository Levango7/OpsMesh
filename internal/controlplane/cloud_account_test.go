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

// fakeCloudProvider 是可注入发现结果/失败的云厂商适配器，仅供同步 handler 测试。
type fakeCloudProvider struct {
	*cloudprovider.NotImplementedProvider
	providerType string
	counts       [4]int // instances, vpcs, subnets, clusters
	fail         bool
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
