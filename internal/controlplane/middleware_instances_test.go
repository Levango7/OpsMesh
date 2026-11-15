package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// TestHandleUninstallMiddlewareInstance_InvalidInstanceID 验证 URL 路径实例键
// 含空白或 k=v/条目分隔符时返回 400，不得进入任务下发/审计写入。
func TestHandleUninstallMiddlewareInstance_InvalidInstanceID(t *testing.T) {
	s := newMWTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	body := `{"agentID":"` + a.AgentID + `","templateID":"mysql","deployType":"docker","params":{"password":"pw"}}`
	for _, id := range []string{"bad id", "a=b", "a;b"} {
		t.Run(id, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-instances/"+url.PathEscape(id)+"/uninstall", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			s.handleMiddlewareInstanceRouting(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestMiddlewareInstanceKey_DeployUninstallConsistent 验证 deploy 与 uninstall
// 在未提供 URL 实例键时用同一推导逻辑（含版本解析），写出相同的 instance 键。
func TestMiddlewareInstanceKey_DeployUninstallConsistent(t *testing.T) {
	s := newMWTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	const body = `{"agentID":"AGENT","templateID":"mysql","version":"8.4","params":{"password":"pw"}}`
	agentBody := strings.Replace(body, "AGENT", a.AgentID, 1)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-templates/mysql/deploy", strings.NewReader(agentBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("deploy status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var deployResp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &deployResp); err != nil {
		t.Fatalf("decode deploy: %v", err)
	}
	deployKey, _ := deployResp["instanceID"].(string)
	if deployKey == "" {
		t.Fatal("deploy response missing instanceID")
	}

	// 直接调用 handler 且不传 URL 实例键，模拟请求体推导路径。
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-instances//uninstall", strings.NewReader(agentBody))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	s.handleUninstallMiddlewareInstance(rec2, req2, "")
	if rec2.Code != http.StatusCreated {
		t.Fatalf("uninstall status=%d, body=%s", rec2.Code, rec2.Body.String())
	}
	var uninstallResp map[string]interface{}
	if err := json.Unmarshal(rec2.Body.Bytes(), &uninstallResp); err != nil {
		t.Fatalf("decode uninstall: %v", err)
	}
	uninstallKey, _ := uninstallResp["instanceID"].(string)
	if uninstallKey != deployKey {
		t.Fatalf("instance key diverged: deploy=%q uninstall=%q", deployKey, uninstallKey)
	}

	// 审计 Detail 同样必须携带相同的 instance 键。
	var auditDeployKey, auditUninstallKey string
	for _, ev := range s.store.Audits() {
		switch ev.Action {
		case "deploy_middleware":
			auditDeployKey = parseAuditDetail(ev.Detail)["instance"]
		case "uninstall_middleware":
			auditUninstallKey = parseAuditDetail(ev.Detail)["instance"]
		}
	}
	if auditDeployKey != deployKey || auditUninstallKey != deployKey {
		t.Fatalf("audit instance key mismatch: deploy=%q uninstall=%q want=%q", auditDeployKey, auditUninstallKey, deployKey)
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

// TestParseAuditDetail_RoundTripWithSpaces 验证引号格式能无损承载空格与 k=v/';' 分隔符。
func TestParseAuditDetail_RoundTripWithSpaces(t *testing.T) {
	detail := formatMiddlewareAuditDetail(
		"template", "my sql",
		"deployType", "docker",
		"agent", "agent a=1;b",
		"version", "8.0 rc",
		"instance", "my sql@agent a=1;b:docker:8.0 rc",
	)
	got := parseAuditDetail(detail)
	want := map[string]string{
		"template":   "my sql",
		"deployType": "docker",
		"agent":      "agent a=1;b",
		"version":    "8.0 rc",
		"instance":   "my sql@agent a=1;b:docker:8.0 rc",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("key %q: got %q, want %q (detail=%q)", k, got[k], v, detail)
		}
	}
	if len(got) != len(want) {
		t.Errorf("len mismatch: got %d, want %d (got=%v)", len(got), len(want), got)
	}
}

// TestParseAuditDetail_IgnoresUnknownKeys 验证解析时未知键被忽略，值内 k=v 不能伪造字段。
func TestParseAuditDetail_IgnoresUnknownKeys(t *testing.T) {
	got := parseAuditDetail(`template="mysql";evil="x";agent="a=b";`)
	if len(got) != 2 {
		t.Fatalf("want 2 known keys, got %d (%v)", len(got), got)
	}
	if got["template"] != "mysql" || got["agent"] != "a=b" {
		t.Fatalf("unexpected parse result: %v", got)
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
