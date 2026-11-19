// gateway_json_contract_test.go — auth 双栈预演（2026-11-19）D2/D3/D5 回归钉子。
//
//	D2：登录响应同一对象不得出现 roleIDs + roleIds 两个等价键——严格 JSON 解码器
//	    （PowerShell ConvertFrom-Json 等）会直接拒绝整份 body。
//	D3：GET /users 列表项只保留 controlplane store.User 的字段集，不再叠加
//	    role_ids / created_at（protobuf 对象）/ must_change_password 三组旧拼写。
//	D5：登录响应 user.tenantId 必须与 /auth/me 的 tenantId 同值（空租户归一为 default）。
package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
)

// assertNoDuplicateJSONKeys 用 token 流严格检查重复键（json.Unmarshal 会静默取后者）。
func assertNoDuplicateJSONKeys(t *testing.T, data []byte) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	var walk func() error
	walk = func() error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := make(map[string]bool)
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := kt.(string)
				if !ok {
					return fmt.Errorf("object key is not a string: %v", kt)
				}
				if seen[key] {
					return fmt.Errorf("duplicate key %q", key)
				}
				seen[key] = true
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = dec.Token() // consume '}'
			return err
		case '[':
			for dec.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = dec.Token() // consume ']'
			return err
		}
		return nil
	}
	if err := walk(); err != nil {
		t.Fatalf("响应含重复 JSON 键（严格客户端会拒绝）：%v\nbody=%s", err, data)
	}
	if tok, err := dec.Token(); err != io.EOF {
		t.Fatalf("JSON 之后仍有内容（token=%v err=%v）：%s", tok, err, data)
	}
}

// TestLogin_NoDuplicateRoleKeys D2：登录 user 只输出 roleIDs，roleIds 必须消失。
func TestLogin_NoDuplicateRoleKeys(t *testing.T) {
	_, mux, svc := newTestGateway()
	clearMustChangePassword(t, svc)

	rec := doReq(t, mux, http.MethodPost, "/api/v1/auth/login",
		`{"username":"admin","password":"admin123"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: got %d, body=%s", rec.Code, rec.Body.String())
	}
	assertNoDuplicateJSONKeys(t, rec.Body.Bytes())

	var body struct {
		User map[string]any `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("登录响应不是 JSON：%v", err)
	}
	if _, ok := body.User["roleIds"]; ok {
		t.Fatalf("登录响应 user 仍含重复别名 roleIds（D2）：%s", rec.Body.String())
	}
	if _, ok := body.User["roleIDs"]; !ok {
		t.Fatalf("登录响应 user 缺规范名 roleIDs：%s", rec.Body.String())
	}
}

// TestListUsers_ExactControlplaneShape D3：列表项字段集与 controlplane store.User 一致。
// 规范集：id/username/email/tenantId/status/roleIDs/createdAt/mustChangePassword/permissions。
func TestListUsers_ExactControlplaneShape(t *testing.T) {
	_, mux, svc := newTestGateway()
	cookies := loginAsAdmin(t, mux, svc)
	f6Register(t, mux, "contract-list")

	rec := doReq(t, mux, http.MethodGet, "/api/v1/users", "", cookies)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET users: got %d, body=%s", rec.Code, rec.Body.String())
	}
	assertNoDuplicateJSONKeys(t, rec.Body.Bytes())

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("列表响应不是 JSON 对象：%v", err)
	}
	// 无参数调用保持 legacy {users:[…]} 信封，不得长出 total/page 等键。
	if len(envelope) != 1 {
		t.Fatalf("legacy 信封应只有 users 一个顶层键，实际 %v", keysOf(envelope))
	}
	var users []map[string]any
	if err := json.Unmarshal(envelope["users"], &users); err != nil {
		t.Fatalf("users 不是数组：%v", err)
	}
	if len(users) == 0 {
		t.Fatal("列表为空（夹具失效）")
	}
	want := map[string]bool{
		"id": true, "username": true, "email": true, "tenantId": true, "status": true,
		"roleIDs": true, "createdAt": true, "mustChangePassword": true, "permissions": true,
	}
	item := users[0]
	for k := range item {
		if !want[k] {
			t.Errorf("列表项出现 controlplane 契约之外的键 %q：%v", k, item)
		}
	}
	for k := range want {
		if _, ok := item[k]; !ok {
			t.Errorf("列表项缺 controlplane 契约键 %q：%v", k, item)
		}
	}
	if got, _ := item["tenantId"].(string); got != "default" {
		t.Errorf("列表项 tenantId = %q，want default（D5 同口径）", got)
	}
}

// TestLogin_TenantMatchesMe D5：空租户（seeded admin）在登录与 /me 上必须同为 default。
func TestLogin_TenantMatchesMe(t *testing.T) {
	_, mux, svc := newTestGateway()
	clearMustChangePassword(t, svc)

	login := doReq(t, mux, http.MethodPost, "/api/v1/auth/login",
		`{"username":"admin","password":"admin123"}`, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login: got %d, body=%s", login.Code, login.Body.String())
	}
	var loginBody struct {
		User map[string]any `json:"user"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &loginBody); err != nil {
		t.Fatalf("登录响应不是 JSON：%v", err)
	}
	loginTenant, _ := loginBody.User["tenantId"].(string)
	if loginTenant != "default" {
		t.Fatalf("登录 user.tenantId = %q，want default（D5）", loginTenant)
	}

	at := findCookie(t, login, "opsmesh_at")
	if at == nil {
		t.Fatal("登录未写 opsmesh_at")
	}
	me := doReq(t, mux, http.MethodGet, "/api/v1/auth/me", "",
		map[string]string{"Cookie": "opsmesh_at=" + at.Value})
	if me.Code != http.StatusOK {
		t.Fatalf("/auth/me: got %d, body=%s", me.Code, me.Body.String())
	}
	var meBody map[string]any
	if err := json.Unmarshal(me.Body.Bytes(), &meBody); err != nil {
		t.Fatalf("/me 响应不是 JSON：%v", err)
	}
	if meTenant, _ := meBody["tenantId"].(string); meTenant != loginTenant {
		t.Fatalf("登录 tenantId=%q 与 /me tenantId=%q 不一致（D5）", loginTenant, meTenant)
	}
}
