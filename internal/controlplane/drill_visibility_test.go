package controlplane

// drill_visibility_test.go — §3.9/§3.10/§3.11 守卫（2026-10-14 演练可见性三件）：
//   - §3.9 agent 列表补 lastSeen + 派生 live（此前死进程恒 "online"，运维看不出失联多久）；
//   - §3.10 身份类失败文案统一带"请重新登录"收尾（此前三套并存、零指引）；
//   - §3.11 零角色账号登录成功但有显式指引（此前零提示，新人判"系统坏了"）。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/controlplane/credentials"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// staleAgentStore 把指定 agent 的心跳回拨成"失联"（§3.9 的注入点）。
// 包内直改内存 map 的写法在 memory 包测试里用；本包经 store 接口注入，
// 与 nilCreateStore 同款手法，不给生产代码加测试钩子。
type staleAgentStore struct {
	store.Store
}

func (s *staleAgentStore) Agents(tenant string) []*proto.AgentInfo {
	out := s.Store.Agents(tenant)
	for _, a := range out {
		if a.AgentID == "a-dead" {
			a.LastSeen = time.Now().Add(-2 * time.Minute)
		}
	}
	return out
}

// TestAgents_StaleHeartbeatMarkedNotLive §3.9：心跳超窗的 agent 必须 live=false
// （自报 status 仍原样返回——兼容旧字段），且 lastSeen 在场。
func TestAgents_StaleHeartbeatMarkedNotLive(t *testing.T) {
	s := newM4TestServer()
	s.store = &staleAgentStore{Store: s.store}
	s.store.Register(&proto.AgentInfo{AgentID: "a-live", Segment: "seg", TenantID: "t1", Hostname: "h1", Status: "online"})
	s.store.Register(&proto.AgentInfo{AgentID: "a-dead", Segment: "seg", TenantID: "t1", Hostname: "h2", Status: "online"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleAgents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body.String())
	}

	var list []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byID := map[string]map[string]any{}
	for _, item := range list {
		byID[item["agentID"].(string)] = item
	}
	if live, ok := byID["a-live"]["live"].(bool); !ok || !live {
		t.Errorf("刚注册的 agent 应 live=true，实得 %v", byID["a-live"]["live"])
	}
	if live, ok := byID["a-dead"]["live"].(bool); !ok || live {
		t.Errorf("心跳超 30s 的 agent 应 live=false（§3.9 原状：恒 online），实得 %v", byID["a-dead"]["live"])
	}
	// 自报 status 原样保留（旧字段兼容）。
	if byID["a-dead"]["status"] != "online" {
		t.Errorf("自报 status 应原样保留，实得 %v", byID["a-dead"]["status"])
	}
	// lastSeen 必须在场（RFC3339 形状）。
	if ls, _ := byID["a-dead"]["lastSeen"].(string); !strings.Contains(ls, "T") {
		t.Errorf("lastSeen 应为 RFC3339 时间串，实得 %v", byID["a-dead"]["lastSeen"])
	}
}

// TestIdentityFailuresCarryReloginHint §3.10：身份类失败（missing identity）必须带
// 统一收尾"please log in again"。（requireAuth=true 让空凭证请求走上
// "missing identity context (gateway auth required)" 分支——正是演练里 /admin/* 撞到的
// 那一套。）
func TestIdentityFailuresCarryReloginHint(t *testing.T) {
	s := newM4TestServer()
	s.requireAuth = true

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil) // 无租户口、无凭证
	rec := httptest.NewRecorder()
	s.handleAgents(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("requireAuth 下无身份应 401，实得 %d：%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "please log in again") {
		t.Errorf("身份失败应带重登指引（§3.10），实得 %s", rec.Body.String())
	}
}

// TestLogin_ZeroRoleAccountGetsNotice §3.11：零角色账号登录 200 且响应带 notice；
// 有角色账号看不到 notice（不制造噪音）。
func TestLogin_ZeroRoleAccountGetsNotice(t *testing.T) {
	s := newAuthTestServer(t)
	hash, err := credentials.HashPassword("SomePass123!x")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	s.store.CreateUser(&store.User{
		ID:           "u-norole",
		Username:     "norole",
		Email:        "norole@x.io",
		Status:       "active",
		PasswordHash: hash,
	})

	body := `{"username":"norole","password":"SomePass123!x"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleAuthLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("零角色账号应能登录（200），实得 %d：%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Notice string `json:"notice"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(out.Notice, "no role assigned") {
		t.Errorf("零角色登录应带指引 notice，实得 %q", out.Notice)
	}

	// 有角色账号：notice 缺席（omitempty 保证响应体与之前逐字一致）。
	body = `{"username":"admin","password":"admin123"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	s.handleAuthLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin 登录应 200，实得 %d：%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "notice") {
		t.Errorf("有角色账号的登录响应不该出现 notice，实得 %s", rec.Body.String())
	}
}
