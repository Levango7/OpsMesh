package cutover

// unroute_test.go — `cutover-unroute`（回退 job）的守卫（TD-60 F2 后半）。
//
// 要钉住的四件事（每条对应一个真实坏法）：
//  1. 名册语义：摘的**只有点名的人**，注释与其他条目原样保留，且追加一行来源注释（可 diff/可审计）；
//  2. 顺序：**先摘名册、后吊销**——对侧不可达时名册仍必须摘掉（回退已生效），失败如实记账；
//  3. dry-run 零副作用：名册不变、且**不登录对侧**（不产生会话）；
//  4. 本侧账号缺失给告警但不阻断（摘除后两侧都登不进，这事必须让人看见）。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeRoster(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "roster.txt")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("写名册: %v", err)
	}
	return p
}

func readRoster(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读名册: %v", err)
	}
	return string(b)
}

func TestRemoveFromRosterKeepsCommentsAndOthers(t *testing.T) {
	p := writeRoster(t, "# cutover-migrate 2026-10-01T00:00:00Z added=3\nalice\nbob,carol\n\n# 手工备注：carol 待确认\ndave\n")

	n, err := removeFromRoster(p, []string{"bob", "dave"}, time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("摘除失败: %v", err)
	}
	if n != 2 {
		t.Errorf("应摘除 2 条，实得 %d", n)
	}
	got := readRoster(t, p)
	for _, want := range []string{
		"# cutover-migrate 2026-10-01T00:00:00Z added=3", // 既有来源注释保留
		"# 手工备注：carol 待确认",                               // 手工注释保留
		"alice",                                          // 未被摘
		"carol",                                          // 多名字行里只摘了 bob，carol 留下
		"# cutover-unroute 2026-10-12T12:00:00Z removed=2", // 本次来源注释
	} {
		if !strings.Contains(got, want) {
			t.Errorf("名册应保留/包含 %q，实际名册：\n%s", want, got)
		}
	}
	if strings.Contains(got, "bob") {
		t.Errorf("bob 应被摘除，实际名册：\n%s", got)
	}
	if strings.Contains(got, "\ndave\n") {
		t.Errorf("dave 应被摘除，实际名册：\n%s", got)
	}
}

// fakeAuthSvc 假对侧：记录调用，按需返回错误。
func fakeAuthSvc(t *testing.T, revokeStatus int, users map[string]string) (*httptest.Server, *[]string) {
	t.Helper()
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/api/v1/auth/login":
			_ = json.NewEncoder(w).Encode(map[string]any{"mustChangePassword": false})
		case r.URL.Path == "/api/v1/users":
			list := make([]map[string]string, 0, len(users))
			for name, id := range users {
				list = append(list, map[string]string{"id": id, "username": name, "status": "active"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"users": list})
		case strings.HasSuffix(r.URL.Path, "/revoke-sessions"):
			w.WriteHeader(revokeStatus)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "revoked", "refreshTokensDeleted": 1})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestRunUnrouteHappyPathRevokesFarSide(t *testing.T) {
	p := writeRoster(t, "alice\nbob\ncarol\n")
	srv, calls := fakeAuthSvc(t, http.StatusOK, map[string]string{"bob": "user-bob"})

	res, err := RunUnroute(UnrouteOptions{
		Users: []string{"bob"}, RosterFile: p,
		AuthSvcURL: srv.URL, AdminUser: "admin", AdminPassword: "pw",
	}, time.Now(), nil)
	if err != nil {
		t.Fatalf("回退失败: %v", err)
	}
	if len(res.Removed) != 1 || res.Removed[0] != "bob" {
		t.Errorf("应摘除 bob，实得 %v", res.Removed)
	}
	if len(res.Revoked) != 1 || res.Revoked[0] != "bob" {
		t.Errorf("应吊销 bob 的会话，实得 revoked=%v failed=%v", res.Revoked, res.RevokeFailed)
	}
	// 对侧收到的是**按 id** 的吊销调用（不是用户名）
	found := false
	for _, c := range *calls {
		if c == "POST /api/v1/users/user-bob/revoke-sessions" {
			found = true
		}
	}
	if !found {
		t.Errorf("对侧未收到按 id 的吊销调用，实际调用：%v", *calls)
	}
	if got := readRoster(t, p); strings.Contains(got, "\nbob\n") {
		t.Errorf("名册应已摘除 bob：\n%s", got)
	}
}

// TestRunUnrouteRosterFirstWhenFarSideDown 顺序判据：对侧不可达时，名册**仍必须**被摘掉
// （回退已生效），失败逐条记账——不得因为对侧挂了就让回退不发生。
func TestRunUnrouteRosterFirstWhenFarSideDown(t *testing.T) {
	p := writeRoster(t, "alice\nbob\n")
	res, err := RunUnroute(UnrouteOptions{
		Users: []string{"bob"}, RosterFile: p,
		AuthSvcURL: "http://127.0.0.1:1", AdminUser: "admin", AdminPassword: "pw",
	}, time.Now(), nil)
	if err != nil {
		t.Fatalf("对侧不可达不应让整体失败（名册已摘即回退已生效）: %v", err)
	}
	if len(res.Removed) != 1 {
		t.Errorf("名册应已摘除 bob，实得 %v", res.Removed)
	}
	if got := readRoster(t, p); strings.Contains(got, "\nbob\n") {
		t.Errorf("对侧不可达时名册也必须被摘：\n%s", got)
	}
	if len(res.RevokeFailed) != 1 || res.RevokeFailed[0].Username != "bob" {
		t.Errorf("应如实记账吊销失败，实得 %v", res.RevokeFailed)
	}
}

// TestRunUnrouteDryRunHasNoSideEffects dry-run：名册不变、**不登录对侧**。
func TestRunUnrouteDryRunHasNoSideEffects(t *testing.T) {
	p := writeRoster(t, "alice\nbob\n")
	before := readRoster(t, p)
	srv, calls := fakeAuthSvc(t, http.StatusOK, map[string]string{"bob": "user-bob"})

	res, err := RunUnroute(UnrouteOptions{
		Users: []string{"bob", "ghost"}, RosterFile: p, DryRun: true,
		AuthSvcURL: srv.URL, AdminUser: "admin", AdminPassword: "pw",
	}, time.Now(), nil)
	if err != nil {
		t.Fatalf("dry-run 失败: %v", err)
	}
	if readRoster(t, p) != before {
		t.Error("dry-run 不得改名册")
	}
	if len(*calls) != 0 {
		t.Errorf("dry-run 不得调对侧（连登录都不该发生），实际调用：%v", *calls)
	}
	if len(res.Removed) != 1 || len(res.NotInRoster) != 1 {
		t.Errorf("dry-run 应给出计划：removed=%v notInRoster=%v", res.Removed, res.NotInRoster)
	}
}

// TestRunUnrouteWarnsWhenLocalAccountMissing 本侧账号缺失：告警（MissingLocal）但不阻断。
func TestRunUnrouteWarnsWhenLocalAccountMissing(t *testing.T) {
	p := writeRoster(t, "alice\nbob\n")
	srv, _ := fakeAuthSvc(t, http.StatusOK, map[string]string{"bob": "user-bob"})
	lookup := func(username string) (LocalUser, bool) {
		if username == "alice" { // 只有 alice 在本侧还有账号
			return LocalUser{Email: "alice@x"}, true
		}
		return LocalUser{}, false
	}
	res, err := RunUnroute(UnrouteOptions{
		Users: []string{"alice", "bob"}, RosterFile: p,
		AuthSvcURL: srv.URL, AdminUser: "admin", AdminPassword: "pw",
	}, time.Now(), lookup)
	if err != nil {
		t.Fatalf("回退失败: %v", err)
	}
	if len(res.MissingLocal) != 1 || res.MissingLocal[0] != "bob" {
		t.Errorf("应告警 bob 本侧已无账号（回退后两侧都登不进），实得 %v", res.MissingLocal)
	}
	if len(res.Removed) != 2 {
		t.Errorf("告警不得阻断回退（两人都应摘除），实得 %v", res.Removed)
	}
}
