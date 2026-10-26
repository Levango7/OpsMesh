package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// TestParseAuditDetail 验证审计 Detail 字符串的解析。
func TestParseAuditDetail(t *testing.T) {
	cases := []struct {
		name   string
		detail string
		want   map[string]string
	}{
		{
			name:   "standard deploy detail",
			detail: "template=mysql deployType=docker agent=ag1 version=8.0 instance=mysql@ag1:docker:8.0",
			want:   map[string]string{"template": "mysql", "deployType": "docker", "agent": "ag1", "version": "8.0", "instance": "mysql@ag1:docker:8.0"},
		},
		{
			name:   "empty detail",
			detail: "",
			want:   map[string]string{},
		},
		{
			name:   "malformed tokens skipped",
			detail: "template=mysql badtoken deployType=docker",
			want:   map[string]string{"template": "mysql", "deployType": "docker"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseAuditDetail(tc.detail)
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch: got %d, want %d (got=%v)", len(got), len(tc.want), got)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("key %q: got %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

// TestInstanceKey 验证复合键格式。
func TestInstanceKey(t *testing.T) {
	key := instanceKey("mysql", "agent-1", "docker", "8.0")
	if key != "mysql@agent-1:docker:8.0" {
		t.Fatalf("got %q, want %q", key, "mysql@agent-1:docker:8.0")
	}
}

// TestDeriveMiddlewareInstances_Deploy 验证部署事件推导出实例。
func TestDeriveMiddlewareInstances_Deploy(t *testing.T) {
	s := newMWTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	// 先部署一个 mysql
	body := `{"agentID":"` + a.AgentID + `","params":{"password":"pw"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-templates/mysql/deploy", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("deploy status=%d, body=%s", rec.Code, rec.Body.String())
	}

	// 查询实例
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/middleware-instances", nil)
	rec2 := httptest.NewRecorder()
	s.handleMiddlewareInstances(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("instances status=%d, body=%s", rec2.Code, rec2.Body.String())
	}
	var list []middlewareInstance
	if err := json.Unmarshal(rec2.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 instance, got %d", len(list))
	}
	if list[0].TemplateID != "mysql" {
		t.Errorf("templateID = %q, want mysql", list[0].TemplateID)
	}
	if list[0].Status != "deployed" {
		t.Errorf("status = %q, want deployed", list[0].Status)
	}
	if list[0].Version == "" {
		t.Error("version should not be empty")
	}
}

// TestDeriveMiddlewareInstances_Uninstall 验证部署+卸载后实例消失。
func TestDeriveMiddlewareInstances_Uninstall(t *testing.T) {
	s := newMWTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	// 部署
	body := `{"agentID":"` + a.AgentID + `","params":{"password":"pw"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-templates/mysql/deploy", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("deploy status=%d", rec.Code)
	}
	var deployResp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &deployResp)
	instID := deployResp["instanceID"].(string)

	// 卸载
	uninstallBody := `{"agentID":"` + a.AgentID + `","templateID":"mysql","deployType":"docker"}`
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-instances/"+instID+"/uninstall", strings.NewReader(uninstallBody))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	s.handleMiddlewareInstanceRouting(rec2, req2)
	if rec2.Code != http.StatusCreated {
		t.Fatalf("uninstall status=%d, body=%s", rec2.Code, rec2.Body.String())
	}

	// 查询实例（默认不显示 uninstalling）
	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/middleware-instances", nil)
	rec3 := httptest.NewRecorder()
	s.handleMiddlewareInstances(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("instances status=%d", rec3.Code)
	}
	var list []middlewareInstance
	json.Unmarshal(rec3.Body.Bytes(), &list)
	if len(list) != 0 {
		t.Fatalf("want 0 instances after uninstall, got %d", len(list))
	}
}

// TestDeriveMiddlewareInstances_TenantIsolation 验证租户隔离。
func TestDeriveMiddlewareInstances_TenantIsolation(t *testing.T) {
	s := newMWTestServer()
	// 默认租户部署
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	body := `{"agentID":"` + a.AgentID + `","params":{"password":"pw"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-templates/mysql/deploy", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("deploy status=%d", rec.Code)
	}

	// 用非 default 租户查询（如果有 tenant 上下文的话）
	// 这里主要验证 QueryAudits 的 tenant 过滤
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/middleware-instances", nil)
	req2.Header.Set("X-Tenant-ID", "other-tenant")
	rec2 := httptest.NewRecorder()
	s.handleMiddlewareInstances(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("instances status=%d", rec2.Code)
	}
	var list []middlewareInstance
	json.Unmarshal(rec2.Body.Bytes(), &list)
	if len(list) != 0 {
		t.Fatalf("want 0 instances for other tenant, got %d", len(list))
	}
}

// TestResolveMiddlewareVersion 验证版本解析。
func TestResolveMiddlewareVersion(t *testing.T) {
	s := newMWTestServer()
	tpl := s.getMiddlewareTemplateByID("mysql")
	if tpl == nil {
		t.Fatal("mysql template not found")
	}
	// 默认版本
	v := resolveMiddlewareVersion(tpl, "")
	if v == nil || v.Version != "8.0" {
		t.Errorf("default version = %v, want 8.0", v)
	}
	// 指定版本
	v2 := resolveMiddlewareVersion(tpl, "8.4")
	if v2 == nil || v2.Version != "8.4" {
		t.Errorf("selected version = %v, want 8.4", v2)
	}
	// 不存在的版本回退到默认
	v3 := resolveMiddlewareVersion(tpl, "99.99")
	if v3 == nil || v3.Version != "8.0" {
		t.Errorf("fallback version = %v, want 8.0", v3)
	}
}

// TestSelectedMiddlewareVersionImage 验证镜像解析。
func TestSelectedMiddlewareVersionImage(t *testing.T) {
	s := newMWTestServer()
	tpl := s.getMiddlewareTemplateByID("mysql")
	if tpl == nil {
		t.Fatal("mysql template not found")
	}
	img := selectedMiddlewareVersionImage(tpl, "8.4")
	if img != "mysql:8.4" {
		t.Errorf("image = %q, want mysql:8.4", img)
	}
	img2 := selectedMiddlewareVersionImage(tpl, "")
	if img2 != "mysql:8.0" {
		t.Errorf("default image = %q, want mysql:8.0", img2)
	}
}

// TestParseMiddlewareScript 验证 {image} 占位符被正确替换。
func TestParseMiddlewareScript(t *testing.T) {
	s := newMWTestServer()
	tpl := s.getMiddlewareTemplateByID("mysql")
	if tpl == nil {
		t.Fatal("mysql template not found")
	}
	script, ok := resolveMiddlewareScript(tpl, "docker", "8.4")
	if !ok {
		t.Fatal("script not found")
	}
	if !strings.Contains(script.Deploy, "mysql:8.4") {
		t.Errorf("deploy script should contain mysql:8.4, got: %s", script.Deploy)
	}
	if strings.Contains(script.Deploy, "{image}") {
		t.Errorf("deploy script should not contain {image} placeholder, got: %s", script.Deploy)
	}
}

// TestMiddlewareTemplateFromStoreVersions 验证旧模板从目录回退版本。
func TestMiddlewareTemplateFromStoreVersions(t *testing.T) {
	st := &store.MiddlewareTemplate{
		ID:      "mysql",
		Name:    "MySQL",
		Type:    "database",
		Version: "8.0",
		Config:  `{"id":"mysql","name":"MySQL","category":"database","version":"8.0","deployTypes":["docker","systemd"],"params":[],"scripts":{},"risk":"medium","tags":["database"]}`,
	}
	tpl := middlewareTemplateFromStore(st)
	if tpl == nil {
		t.Fatal("nil template")
	}
	if len(tpl.Versions) == 0 {
		t.Fatal("versions should be populated from catalog")
	}
	if tpl.Versions[0].Version != "8.0" {
		t.Errorf("first version = %q, want 8.0", tpl.Versions[0].Version)
	}
}
