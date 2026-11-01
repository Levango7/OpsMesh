package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Levango7/OpsMesh/internal/authctx"
)

// 六个内置工具都不得再返回硬编码样例数据：它们不查 device-svc、不建任务，
// 却曾给出 web-01 / 192.168.1.10 / task-12345 / 75.3% 这类「看起来像真的」的值。
func TestBuiltinToolsAreNotWired(t *testing.T) {
	tools := DefaultToolRegistry().List()
	if len(tools) != 6 {
		t.Fatalf("内置工具数量 = %d, want 6", len(tools))
	}
	for _, tool := range tools {
		if IsToolWired(tool) {
			t.Errorf("%s 应自报未接入后端", tool.Name())
		}
		out, err := tool.Execute(context.Background(), map[string]interface{}{"device_id": "d1"}, AgentContext{TenantID: "t1"})
		if !errors.Is(err, ErrToolNotWired) {
			t.Errorf("%s Execute err = %v, want ErrToolNotWired", tool.Name(), err)
		}
		if out != nil {
			t.Errorf("%s 未接线却返回了数据: %+v", tool.Name(), out)
		}
		if !errors.Is(err, ErrToolNotWired) || err.Error() == ErrToolNotWired.Error() {
			t.Errorf("%s 错误信息应带上工具名，便于定位是哪个能力没接: %v", tool.Name(), err)
		}
	}
}

// /tools 必须把未接线能力标出来，否则前端仍会摆出一列可点的按钮。
func TestHandleToolsMarksImplementedFalse(t *testing.T) {
	h := handlerWithAuth(NewMemoryManager(), authctx.Context{TenantID: "t1"})
	w := httptest.NewRecorder()
	h.HandleTools(w, httptest.NewRequest(http.MethodGet, "/api/v1/ai/agent/tools", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	var resp struct {
		Tools []struct {
			Name                 string `json:"name"`
			Implemented          bool   `json:"implemented"`
			NotImplementedReason string `json:"notImplementedReason"`
		} `json:"tools"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解码: %v body=%s", err, w.Body.String())
	}
	if resp.Count != 6 || len(resp.Tools) != 6 {
		t.Fatalf("tools 数量 = %d/%d, want 6", resp.Count, len(resp.Tools))
	}
	for _, tc := range resp.Tools {
		if tc.Implemented {
			t.Errorf("%s 被标成已实现", tc.Name)
		}
		if tc.NotImplementedReason == "" {
			t.Errorf("%s 缺少未实现原因字段（前端置灰文案依据）", tc.Name)
		}
	}
}

// executeTool 的观测里只能出现「失败」，不能出现假数据。
func TestExecuteToolSurfacesNotWired(t *testing.T) {
	a := NewBaseAgent(AgentConfig{Type: AgentTypeRootCause, Model: "m1", MaxSteps: 3}, nil, DefaultToolRegistry(), NewMemoryManager())

	out, rec, err := a.executeTool(context.Background(), "query_device", `{"device_id":"d1"}`, AgentContext{TenantID: "t1"})
	if !errors.Is(err, ErrToolNotWired) {
		t.Fatalf("err = %v, want ErrToolNotWired", err)
	}
	if out != "Error: query_device: "+ErrToolNotWired.Error() {
		t.Fatalf("观测文本 = %q", out)
	}
	if rec == nil || rec.Error == "" || rec.Output != nil {
		t.Fatalf("工具调用记录未反映失败: %+v", rec)
	}
}

// Run 命中未接线工具时要记下工具名，HTTP 侧据此把 200 改成 501。
func TestNotWiredOf(t *testing.T) {
	single := &AgentResult{NotWired: []string{"query_device"}}
	if got := notWiredOf(single); len(got) != 1 || got[0] != "query_device" {
		t.Fatalf("AgentResult = %v", got)
	}

	dag := &DAGResult{Results: map[string]*AgentResult{
		"a": {NotWired: []string{"web_search", "query_device"}},
		"b": {NotWired: []string{"web_search"}},
	}}
	got := notWiredOf(dag)
	if len(got) != 2 || got[0] != "query_device" || got[1] != "web_search" {
		t.Fatalf("DAGResult 去重排序 = %v", got)
	}

	if notWiredOf(&AgentResult{}) != nil || notWiredOf(nil) != nil || notWiredOf((*AgentResult)(nil)) != nil {
		t.Fatal("空结果应返回 nil 而不是 panic 或空切片")
	}
	if notWiredOf("别的类型") != nil {
		t.Fatal("未知结果类型应返回 nil")
	}
}

// 高危工具（远程 shell）的权限闸此前恒 true。现在缺字段即拒。
func TestExecuteCommandToolPermissionFailClosed(t *testing.T) {
	tool := &ExecuteCommandTool{}
	cases := []struct {
		name     string
		metadata map[string]interface{}
		want     bool
	}{
		{"缺 permissions 字段", nil, false},
		{"空集合", map[string]interface{}{"permissions": []string{}}, false},
		{"有 task:write", map[string]interface{}{"permissions": []string{"task:write"}}, true},
		{"只有只读点", map[string]interface{}{"permissions": []string{"task:read", "device:read"}}, false},
		{"JSON 反序列化形态", map[string]interface{}{"permissions": []interface{}{"task:write"}}, true},
		{"类型不认识", map[string]interface{}{"permissions": "task:write"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := AgentContext{TenantID: "t1", UserID: "u1", Metadata: tc.metadata}
			if got := tool.CheckPermission(ctx); got != tc.want {
				t.Fatalf("CheckPermission(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestPermissionsForRoles(t *testing.T) {
	if got := permissionsForRoles(nil); len(got) != 0 {
		t.Fatalf("空角色应得空权限集: %v", got)
	}
	if got := permissionsForRoles([]string{"不存在的角色"}); len(got) != 0 {
		t.Fatalf("未知角色应得空权限集: %v", got)
	}
	viewer := permissionsForRoles([]string{"viewer"})
	if hasPermission(AgentContext{Metadata: map[string]interface{}{"permissions": viewer}}, "task:write") {
		t.Fatal("viewer 不应持有 task:write")
	}
	if !hasPermission(AgentContext{Metadata: map[string]interface{}{"permissions": viewer}}, "task:read") {
		t.Fatal("viewer 应持有 task:read（派生自 *:read）")
	}
	admin := permissionsForRoles([]string{"admin"})
	if !hasPermission(AgentContext{Metadata: map[string]interface{}{"permissions": admin}}, "task:write") {
		t.Fatal("admin 应持有 task:write")
	}
	// 多角色并集且稳定排序（供断言与日志可复现）。
	multi := permissionsForRoles([]string{"viewer", "operator"})
	if !hasPermission(AgentContext{Metadata: map[string]interface{}{"permissions": multi}}, "task:write") {
		t.Fatal("operator 角色应带入 task:write")
	}
	for i := 1; i < len(multi); i++ {
		if multi[i-1] > multi[i] {
			t.Fatalf("权限集合未排序，多角色并集将不可复现: %v", multi)
		}
	}
}
