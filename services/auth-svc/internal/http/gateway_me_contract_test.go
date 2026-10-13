package http

// gateway_me_contract_test.go — F6 守卫：/auth/me 响应字段集**单一名套**
// （2026-10-13 本线接管裁定后新增）。
//
// 背景（演练批次 0 F6）：auth-svc /auth/me 曾同时回 tenantID+tenantId、
// userID+id、roles+roleIDs 两套拼写（代码注释原话"两种拼写并存，消费者按需
// 取一"）。后果不是"多几个键"而是**切流前后响应形状不同**：名册用户被切到
// auth-svc 后拿到一套键、本地用户（控制面 /auth/me）拿到另一套——与 TD-60
// "同一用户两套认证面同一响应"正相反；且任一侧增删键都会静默改写另一侧的
// 契约面，而文档与客户端无从知道该信哪套。
//
// 裁定（规范集 = 控制面 /auth/me 本地形态 = store.User 的 JSON tag）：
// id / username / email / tenantId / roleIDs + auth-svc 侧既有补充
// mode / status / mustChangePassword / permissions。
// 依据：① 前端唯一功能消费是 permissions（web/enterprise/src/stores/auth.js:31），
// 其余键无人读；② 镜像 store.User tag 即"零差异切流"；③ /api/v1/me（网关注入
// 身份，tenantID/userID/roles 方言）是另一端点另一消费方，不受本裁定影响。
// 对侧同族形状由 internal/controlplane/auth_me_contract_test.go 钉住。

import (
	"encoding/json"
	"net/http"
	"testing"
)

// wantMeKeys 规范字段集（与控制面 /auth/me 的 store.User tag 对齐 + auth-svc 补充）。
// 顺序无关；多一个少一个都判红——"同义双返"回归（再塞回 tenantID/userID/roles）
// 由本表直接抓住。
var wantMeKeys = map[string]bool{
	"id":                 true,
	"username":           true,
	"email":              true,
	"tenantId":           true,
	"roleIDs":            true,
	"mode":               true,
	"status":             true,
	"mustChangePassword": true,
	"permissions":        true,
}

// forbiddenMeKeys 明确不许出现的同义键（F6 的三个案发点）。
var forbiddenMeKeys = []string{"tenantID", "userID", "roles", "roleIds", "userId"}

// TestMe_FieldSetIsCanonical /auth/me 的键集必须**恰好**等于规范集：
// ① 规范键一个不少（缺一个说明响应被改瘦，客户端/文档对不上）；
// ② 同义键一个不来（再来"两种拼写并存"就是 F6 回归）。
func TestMe_FieldSetIsCanonical(t *testing.T) {
	_, mux, svc := newTestGateway()
	cookies := loginAsAdmin(t, mux, svc)

	rec := doReq(t, mux, http.MethodGet, "/api/v1/auth/me", "", cookies)
	if rec.Code != http.StatusOK {
		t.Fatalf("me: got %d, body=%s", rec.Code, rec.Body.String())
	}
	var me map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("me 响应不是合法 JSON: %v", err)
	}

	for k := range me {
		if !wantMeKeys[k] {
			t.Errorf("响应含规范集外字段 %q（F6 回归：同义双返/漂移键）；当前全集=%v", k, sortedKeys(me))
		}
	}
	for k := range wantMeKeys {
		if _, ok := me[k]; !ok {
			t.Errorf("响应缺规范字段 %q（契约被改瘦）；当前全集=%v", k, sortedKeys(me))
		}
	}
	for _, k := range forbiddenMeKeys {
		if _, ok := me[k]; ok {
			t.Errorf("响应含 F6 明案同义键 %q——收单名裁定被回退；当前全集=%v", k, sortedKeys(me))
		}
	}

	// 规范键的取值语义抽查（防"键对了值错了"）：id/roleIDs 必须与库内用户一致
	// （不是占位）；tenantId 来自 at 的 claims（store.User 无租户字段——租户在
	// JWT 里），只断言非空；permissions 非空由同包另一用例细验。
	u := svc.Store().GetUserByUsername("admin")
	if u == nil {
		t.Fatal("前置失败：admin 不存在")
	}
	if me["id"] != u.ID {
		t.Errorf("id 应与库内一致：响应 %v，库内 %v", me["id"], u.ID)
	}
	if tnt, _ := me["tenantId"].(string); tnt == "" {
		t.Error("tenantId 不应为空（at 的 claims 带了租户；空值会让前端按 default 兜底，掩盖切流后租户丢失）")
	}
	roleIDs, _ := me["roleIDs"].([]any)
	if len(roleIDs) != len(u.RoleIDs) {
		t.Errorf("roleIDs 应与库内一致：响应 %v，库内 %v", me["roleIDs"], u.RoleIDs)
	}
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ { // 小集合插入排序，免引 sort 只为 9 个键
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
