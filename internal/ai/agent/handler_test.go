package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// denyAuthorize 模拟「已登录但权限不足」：写出 403 并返回 false。
func denyAuthorize(perm string) func(http.ResponseWriter, *http.Request, string) (authctx.Context, bool) {
	return func(w http.ResponseWriter, r *http.Request, got string) (authctx.Context, bool) {
		if got != perm {
			paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "权限点应为 " + perm + "，实为 " + got})
			return authctx.Context{}, false
		}
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "permission denied: " + got})
		return authctx.Context{}, false
	}
}

// handlerWithAuth 在既有 mem 上造一个「鉴权已通过、身份为 actx」的 handler。
func handlerWithAuth(mem *MemoryManager, actx authctx.Context) *AgentHandler {
	h := NewAgentHandler(nil, nil, mem)
	h.Authorize = func(w http.ResponseWriter, r *http.Request, perm string) (authctx.Context, bool) {
		return actx, true
	}
	return h
}

// 六条路由各自的权限点：只读四类走 ai:read，驱动工具与销毁记忆走 ai:write。
func TestRoutePermissionMapping(t *testing.T) {
	cases := []struct {
		name, method, target, perm string
	}{
		{"execute", http.MethodPost, "/api/v1/ai/agent/execute", "ai:write"},
		{"classify", http.MethodPost, "/api/v1/ai/agent/classify", "ai:read"},
		{"decompose", http.MethodPost, "/api/v1/ai/agent/decompose", "ai:read"},
		{"memory 读", http.MethodGet, "/api/v1/ai/agent/memory?sessionId=s1", "ai:read"},
		{"memory 删", http.MethodDelete, "/api/v1/ai/agent/memory?sessionId=s1", "ai:write"},
		{"tools", http.MethodGet, "/api/v1/ai/agent/tools", "ai:read"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mem := NewMemoryManager()
			mem.AddShortTerm("t1", "s1", "q", "a")
			h := NewAgentHandler(nil, nil, mem)
			h.Authorize = denyAuthorize(tc.perm) // 权限点不符时回调内部即报错

			req := httptest.NewRequest(tc.method, tc.target, nil)
			w := httptest.NewRecorder()
			switch tc.name {
			case "execute":
				h.HandleExecute(w, req)
			case "classify":
				h.HandleClassify(w, req)
			case "decompose":
				h.HandleDecompose(w, req)
			case "memory 读":
				h.HandleMemory(w, req)
			case "memory 删":
				h.HandleClearMemory(w, req)
			case "tools":
				h.HandleTools(w, req)
			}
			if w.Code != http.StatusForbidden {
				t.Fatalf("%s 应走权限点 %q（不符时 403），got %d body=%s", tc.name, tc.perm, w.Code, w.Body.String())
			}
		})
	}
}

// 未注入回调（独立使用/单测）保持透传，不改变既有行为。
func TestAuthorizeNilPassesThrough(t *testing.T) {
	mem := NewMemoryManager()
	mem.AddShortTerm("t1", "s1", "q", "a")
	h := NewAgentHandler(nil, nil, mem) // Authorize 为 nil

	w := httptest.NewRecorder()
	h.HandleMemory(w, httptest.NewRequest(http.MethodGet, "/api/v1/ai/agent/memory?sessionId=s1", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("未启用鉴权时应放行，got %d", w.Code)
	}
}

// 跨租户不得读到、也删不掉别人的会话记忆：sessionId 由客户端自报，
// 仅凭它做键等于任意租户猜到号即可读取/销毁他人上下文。
func TestMemoryIsTenantScoped(t *testing.T) {
	mem := NewMemoryManager()
	mem.AddShortTerm("t1", "s1", "t1 的问题", "t1 的答")

	if got := mem.GetShortTerm("t2", "s1"); len(got) != 0 {
		t.Fatalf("t2 读到 t1 的会话记忆: %+v", got)
	}
	if got := mem.GetShortTerm("t1", "s1"); len(got) != 1 {
		t.Fatalf("t1 应能读自己的会话记忆: %+v", got)
	}

	h := handlerWithAuth(mem, authctx.Context{TenantID: "t2"})
	w := httptest.NewRecorder()
	h.HandleMemory(w, httptest.NewRequest(http.MethodGet, "/api/v1/ai/agent/memory?sessionId=s1", nil))
	var resp struct {
		Entries []MemoryEntry `json:"entries"`
		Count   int           `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解码: %v", err)
	}
	if resp.Count != 0 {
		t.Fatalf("t2 通过接口读到 %d 条 t1 的记忆", resp.Count)
	}

	// 删除同理：t2 删不掉 t1 的会话。
	w = httptest.NewRecorder()
	h.HandleClearMemory(w, httptest.NewRequest(http.MethodDelete, "/api/v1/ai/agent/memory?sessionId=s1", nil))
	if len(mem.GetShortTerm("t1", "s1")) != 1 {
		t.Fatal("跨租户 ClearSession 生效了")
	}

	h1 := handlerWithAuth(mem, authctx.Context{TenantID: "t1"})
	w = httptest.NewRecorder()
	h1.HandleClearMemory(w, httptest.NewRequest(http.MethodDelete, "/api/v1/ai/agent/memory?sessionId=s1", nil))
	if len(mem.GetShortTerm("t1", "s1")) != 0 {
		t.Fatal("本租户应能清空自己的会话记忆")
	}
}

// 已验签上下文必须覆盖请求自报的 userId/tenantId（否则可冒充任意身份）。
func TestIdentityPrefersVerifiedContext(t *testing.T) {
	actx := authctx.Context{TenantID: "verified", UserID: "u-real"}
	tenant, user := identity(actx, "body-tenant", "u-fake")
	if tenant != "verified" || user != "u-real" {
		t.Fatalf("identity = (%s,%s), want (verified,u-real)", tenant, user)
	}

	// 未启用鉴权（actx 为空）时沿用自报值，独立部署不受影响。
	tenant, user = identity(authctx.Context{}, "body-tenant", "u-body")
	if tenant != "body-tenant" || user != "u-body" {
		t.Fatalf("无验签时 identity = (%s,%s)，want 原值", tenant, user)
	}
}

// 记忆端点用的租户也遵循同一优先级：验签上下文优先，其次才是查询参数。
func TestMemoryTenantResolution(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/agent/memory?sessionId=s1&tenantId=claimed", nil)
	if got := memoryTenant(authctx.Context{TenantID: "verified"}, req); got != "verified" {
		t.Fatalf("memoryTenant = %s, want verified（不得被查询参数覆盖）", got)
	}
	if got := memoryTenant(authctx.Context{}, req); got != "claimed" {
		t.Fatalf("未启用鉴权时 memoryTenant = %s, want claimed", got)
	}
}

func TestShortTermWindow(t *testing.T) {
	mem := NewMemoryManager()
	for i := 0; i < 45; i++ {
		mem.AddShortTerm("t1", "s1", "in", "out")
	}
	got := mem.GetShortTerm("t1", "s1")
	if len(got) > 20 {
		t.Fatalf("短期记忆窗口 %d 条，超过 maxShort=20", len(got))
	}
	if len(got) == 0 {
		t.Fatal("短期记忆被清空了")
	}
}

func TestSummarizeSessionTruncatesSafely(t *testing.T) {
	mem := NewMemoryManager()
	mem.AddShortTerm("t1", "s1", strings.Repeat("很长的中文问题", 40), strings.Repeat("很长的中文回答", 40))
	summary := mem.SummarizeSession("t1", "s1")
	if !strings.HasPrefix(summary, "会话摘要:") {
		t.Fatalf("摘要格式不符: %q", summary)
	}
	if !utf8.ValidString(summary) {
		t.Fatalf("摘要截断产生非法 UTF-8（按字节硬切落在多字节字符中间）: %q", summary)
	}
	// 直接钉截断原语：截到中文词中间时必须在字符边界回退。
	if got := truncate(strings.Repeat("测", 50), 10); !utf8.ValidString(got) {
		t.Fatalf("truncate 产生非法 UTF-8: %q", got)
	}
}

func TestParseAction(t *testing.T) {
	cases := []struct {
		name, thought, wantAction, wantInput string
	}{
		{"标准两段式", "Thought: t\nAction: query_device\nAction Input: {\"device_id\":\"d1\"}", "query_device", `{"device_id":"d1"}`},
		{"多余空白", "Action:   query_metrics  \nAction Input:   {}  ", "query_metrics", "{}"},
		{"无 Action", "Thought: 只有思考", "", ""},
		{"Final Answer 优先", "Action: query_device\nFinal Answer: 已完成", "final_answer", "已完成"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			action, input := parseAction(tc.thought)
			if action != tc.wantAction || input != tc.wantInput {
				t.Fatalf("parseAction(%q) = (%q,%q), want (%q,%q)", tc.thought, action, input, tc.wantAction, tc.wantInput)
			}
		})
	}
}

func TestToolRegistry(t *testing.T) {
	tr := NewToolRegistry()
	if _, ok := tr.Get("nope"); ok {
		t.Fatal("未注册的工具不应命中")
	}
	tr.Register(&QueryDeviceTool{})
	if len(tr.List()) != 1 {
		t.Fatalf("List = %d 个工具", len(tr.List()))
	}
	// 同名注册应覆盖而不是叠加。
	tr.Register(&QueryDeviceTool{})
	if len(tr.List()) != 1 {
		t.Fatalf("同名工具注册两次后 List = %d", len(tr.List()))
	}

	gwTools := tr.ToGatewayTools()
	if len(gwTools) != 1 {
		t.Fatalf("ToGatewayTools = %d 个", len(gwTools))
	}
	if gwTools[0].Type != "function" || gwTools[0].Function.Name != "query_device" ||
		gwTools[0].Function.Description == "" || gwTools[0].Function.Parameters == nil {
		t.Fatalf("工具元信息未透传到网关结构: %+v", gwTools[0])
	}
}
