package cutover

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// farSide 是一个**行为对齐 auth-svc 网关**的假对侧：登录（must-change 分支）、用户清单（含 pending）、
// 角色清单、管理端建号（默认 active、409 重名、校验 roleID）、口令策略。
// 不复刻业务逻辑，只复刻 job 依赖的契约面。
type farSide struct {
	mu          sync.Mutex
	users       map[string]farUser // username -> user（含 status）
	password    map[string]string  // username -> 口令（模拟库中哈希）
	loginReject map[string]bool    // 登录必拒的用户名（模拟"建号成功但凭证不可用"）
	roles       map[string]bool
	created     int  // 建号调用计数（dry-run 断言用）
	adminMust   bool // 管理员是否处于首登强制改密态
	srv         *httptest.Server
}

func newFarSide(t *testing.T) *farSide {
	t.Helper()
	f := &farSide{
		users:       map[string]farUser{"admin": {ID: "user-admin", Username: "admin", Status: "active"}},
		password:    map[string]string{"admin": "Far-Admin-2026x!"},
		loginReject: map[string]bool{},
		roles:       map[string]bool{"role-admin": true, "role-viewer": true},
	}
	mux := http.NewServeMux()
	writeErr := func(w http.ResponseWriter, code int, msg string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`{"error":` + jsonString(msg) + `}`))
	}
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		u, ok := f.users[body.Username]
		pass := f.password[body.Username]
		reject := f.loginReject[body.Username]
		adminMust := f.adminMust && body.Username == "admin"
		f.mu.Unlock()
		if reject || !ok || pass != body.Password || u.Status != "active" {
			writeErr(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if adminMust {
			_, _ = w.Write([]byte(`{"mustChangePassword":true,"changePasswordToken":"cpt-x"}`))
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "opsmesh_at", Value: "stub-at"})
		_, _ = w.Write([]byte(`{"user":{"username":"` + body.Username + `"}}`))
	})
	mux.HandleFunc("/api/v1/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/v1/users", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			f.mu.Lock()
			defer f.mu.Unlock()
			out := make([]farUser, 0, len(f.users))
			for _, u := range f.users {
				out = append(out, u)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"users":` + string(mustJSON(out)) + `}`))
		case http.MethodPost:
			var body struct {
				Username string   `json:"username"`
				Password string   `json:"password"`
				Email    string   `json:"email"`
				RoleIDs  []string `json:"roleIDs"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(body.Password) < 12 || !strings.ContainsAny(body.Password, "!@#$%^&*-_=+") {
				writeErr(w, http.StatusBadRequest, "password too weak")
				return
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if _, ok := f.users[body.Username]; ok {
				writeErr(w, http.StatusConflict, "username already exists")
				return
			}
			for _, rid := range body.RoleIDs {
				if !f.roles[rid] {
					writeErr(w, http.StatusBadRequest, "unknown role id: "+rid)
					return
				}
			}
			f.created++
			f.users[body.Username] = farUser{ID: "far-" + body.Username, Username: body.Username, Status: "active"}
			f.password[body.Username] = body.Password
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"status":"created"}`))
		}
	})
	mux.HandleFunc("/api/v1/roles", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		out := make([]string, 0, len(f.roles))
		for id := range f.roles {
			out = append(out, id)
		}
		_, _ = w.Write([]byte(`{"roles":[` + strings.Join(roleObjs(out), ",") + `]}`))
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func roleObjs(ids []string) []string {
	objs := make([]string, 0, len(ids))
	for _, id := range ids {
		objs = append(objs, `{"id":"`+id+`"}`)
	}
	return objs
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// fakeLookup 从 map 适配 LocalLookup（email + 本地角色）。
func fakeLookup(users map[string]LocalUser) LocalLookup {
	return func(name string) (LocalUser, bool) {
		u, ok := users[name]
		return u, ok
	}
}

func TestRunMigrationHappyPath(t *testing.T) {
	f := newFarSide(t)
	dir := t.TempDir()
	roster := filepath.Join(dir, "roster.txt")
	lookup := fakeLookup(map[string]LocalUser{
		"alice": {Email: "a@x.io", RoleIDs: []string{"role-admin"}},
		"bob":   {Email: "b@x.io", RoleIDs: []string{"role-admin", "role-local-only"}},
	})
	res, err := RunMigration(MigrateOptions{
		Users:         []string{"alice", "bob"},
		AuthSvcURL:    f.srv.URL,
		RosterFile:    roster,
		AdminUser:     adminCreds0(),
		AdminPassword: adminCreds1(),
	}, lookup)
	if err != nil {
		t.Fatalf("RunMigration: %v", err)
	}
	if len(res.Created) != 2 || len(res.Failed) != 0 {
		t.Fatalf("want 2 created/0 failed, got %+v / %+v", res.Created, res.Failed)
	}
	if res.Added != 2 {
		t.Fatalf("名册应写入 2 条，got %d", res.Added)
	}
	// 一次性口令：长度与字符类（对侧策略 12+特殊），且两人不同。
	for _, u := range res.Created {
		if len(u.OneTimePassword) != 24 {
			t.Errorf("%s 的一次性口令长度 %d, want 24", u.Username, len(u.OneTimePassword))
		}
	}

	// 角色映射：对侧不存在的 role-local-only 记入 DroppedRoles、不透传（透传会 400）。
	// 断言按用户名查找——seen 是 map，迭代顺序随机，不得依赖 Created 的下标。
	byName := map[string]MigratedUser{}
	for _, u := range res.Created {
		byName[u.Username] = u
	}
	if got := byName["alice"].DroppedRoles; len(got) != 0 {
		t.Errorf("alice 不应有丢弃角色: %v", got)
	}
	if got := byName["bob"].DroppedRoles; len(got) != 1 || got[0] != "role-local-only" {
		t.Errorf("bob 应丢弃 role-local-only: %v", got)
	}
	if byName["alice"].OneTimePassword == byName["bob"].OneTimePassword {
		t.Error("两个用户的一次性口令不得相同")
	}
	// 名册内容：两名 + 来源注释（可 diff/审计 = 决策单 Q1）。
	content := readFile(t, roster)
	if !strings.Contains(content, "alice") || !strings.Contains(content, "bob") {
		t.Errorf("名册缺用户: %q", content)
	}
	if !strings.Contains(content, "# cutover-migrate") {
		t.Errorf("名册缺来源注释: %q", content)
	}
	// 幂等与回退故事：从名册**摘掉 bob**（= 单用户回退，决策单 Q1 的回滚粒度）后复跑——
	// alice 已在名册 ⇒ Skipped；bob 对侧已 active ⇒ Adopted（不建号、无新口令）且只补回这 1 条。
	rolled := strings.Replace(readFile(t, roster), "bob\n", "", 1)
	if err := os.WriteFile(roster, []byte(rolled), 0o600); err != nil {
		t.Fatal(err)
	}
	res2, err := RunMigration(MigrateOptions{
		Users: []string{"alice", "bob"}, AuthSvcURL: f.svr(), RosterFile: roster,
		AdminUser: adminCreds0(), AdminPassword: adminCreds1(),
	}, lookup)
	if err != nil {
		t.Fatalf("复跑: %v", err)
	}
	if len(res2.Skipped) != 1 || res2.Skipped[0] != "alice" {
		t.Fatalf("alice 已在名册应 Skipped: %+v", res2.Skipped)
	}
	if len(res2.Adopted) != 1 || res2.Adopted[0].Username != "bob" || res2.Adopted[0].OneTimePassword != "" {
		t.Fatalf("bob 应被 Adopted（无新口令）: %+v", res2.Adopted)
	}
	if res2.Added != 1 {
		t.Fatalf("复跑只应补回 bob 一条, added=%d", res2.Added)
	}
	content2 := readFile(t, roster)
	if strings.Count(content2, "bob") != 1 || strings.Count(content2, "alice") != 1 {
		t.Errorf("复跑后名册条目数不对: %q", content2)
	}
	// 再跑一次（名册已含两人）⇒ 全 Skipped、零新增、零建号（幂等收敛）。
	before := f.createdCount()
	res3, err := RunMigration(MigrateOptions{
		Users: []string{"alice", "bob"}, AuthSvcURL: f.svr(), RosterFile: roster,
		AdminUser: adminCreds0(), AdminPassword: adminCreds1(),
	}, lookup)
	if err != nil {
		t.Fatalf("三跑: %v", err)
	}
	if len(res3.Skipped) != 2 || res3.Added != 0 || f.createdCount() != before {
		t.Fatalf("幂等三跑应全 Skipped/零新增/零建号: %+v added=%d", res3.Skipped, res3.Added)
	}
}

func TestRunMigrationDryRunCreatesNothing(t *testing.T) {
	f := newFarSide(t)
	dir := t.TempDir()
	roster := filepath.Join(dir, "roster.txt")
	before := f.createdCount()
	res, err := RunMigration(MigrateOptions{
		Users: []string{"alice"}, AuthSvcURL: f.svr(), RosterFile: roster,
		AdminUser: adminCreds0(), AdminPassword: adminCreds1(), DryRun: true,
	}, fakeLookup(map[string]LocalUser{"alice": {Email: "a@x.io"}}))
	if err != nil {
		t.Fatalf("RunMigration: %v", err)
	}
	if f.createdCount() != before {
		t.Fatalf("dry-run 不得在对侧建号（created %d→%d）", before, f.createdCount())
	}
	if _, err := os.Stat(roster); !os.IsNotExist(err) {
		t.Fatalf("dry-run 不得写名册文件: %v", err)
	}
	if len(res.Created) != 1 {
		t.Fatalf("dry-run 应给出计划（1 个将建号）: %+v", res.Created)
	}
}

func TestRunMigrationFailureIsolation(t *testing.T) {
	f := newFarSide(t)
	// 预置对侧一个 pending 用户（不自动激活属人工决策）；broken-bea 不预置、仅令其登录必拒
	// ⇒ 模拟「对侧建号成功、但凭证对该用户不可用」——这是"搬完才写名册"验证半边要拦的形态。
	f.mu.Lock()
	f.users["pending-pete"] = farUser{ID: "far-pending", Username: "pending-pete", Status: "pending"}
	f.loginReject["broken-bea"] = true
	f.mu.Unlock()
	dir := t.TempDir()
	roster := filepath.Join(dir, "roster.txt")
	// ghost-gary 故意**不在** lookup map 里 = 本侧不存在该用户。
	lookup := fakeLookup(map[string]LocalUser{
		"good-gina":    {Email: "g@x.io"},
		"pending-pete": {Email: "p@x.io"},
		"broken-bea":   {Email: "b@x.io"},
	})
	res, err := RunMigration(MigrateOptions{
		Users:      []string{"good-gina", "ghost-gary", "pending-pete", "broken-bea"},
		AuthSvcURL: f.svr(), RosterFile: roster,
		AdminUser: adminCreds0(), AdminPassword: adminCreds1(),
	}, lookup)
	if err != nil {
		t.Fatalf("RunMigration: %v", err)
	}
	if len(res.Created) != 1 || res.Created[0].Username != "good-gina" {
		t.Fatalf("只有 good-gina 应建号成功: %+v", res.Created)
	}
	fails := map[string]string{}
	for _, x := range res.Failed {
		fails[x.Username] = x.Reason
	}
	for _, name := range []string{"ghost-gary", "pending-pete", "broken-bea"} {
		if _, ok := fails[name]; !ok {
			t.Errorf("%s 应判失败，got fails=%v", name, fails)
		}
	}
	if !strings.Contains(fails["pending-pete"], "不自动激活") {
		t.Errorf("pending 用户的原因应说明不自动激活: %q", fails["pending-pete"])
	}
	if !strings.Contains(fails["broken-bea"], "登录验证失败") {
		t.Errorf("登录验证失败的形态必须点名（这是搬完才写名册的验证半边）: %q", fails["broken-bea"])
	}
	// 名册只含成功者：失败用户留在本地登录面。
	content := readFile(t, roster)
	for _, bad := range []string{"ghost-gary", "pending-pete", "broken-bea"} {
		if strings.Contains(content, bad) {
			t.Errorf("失败用户 %s 不得进名册: %q", bad, content)
		}
	}
	if !strings.Contains(content, "good-gina") {
		t.Errorf("成功用户应进名册: %q", content)
	}
}

func TestRunMigrationAdminMustChangeFailsFast(t *testing.T) {
	f := newFarSide(t)
	f.mu.Lock()
	f.adminMust = true
	f.mu.Unlock()
	_, err := RunMigration(MigrateOptions{
		Users: []string{"alice"}, AuthSvcURL: f.svr(), RosterFile: filepath.Join(t.TempDir(), "r.txt"),
		AdminUser: adminCreds0(), AdminPassword: adminCreds1(),
	}, fakeLookup(map[string]LocalUser{"alice": {}}))
	if err == nil || !strings.Contains(err.Error(), "首登强制改密") {
		t.Fatalf("管理员 must-change 应整体 fail-fast（不能替人发明新口令）: %v", err)
	}
}

func TestRunMigrationValidatesInput(t *testing.T) {
	f := newFarSide(t)
	base := MigrateOptions{AuthSvcURL: f.svr(), AdminUser: adminCreds0(), AdminPassword: adminCreds1()}
	if _, err := RunMigration(MigrateOptions{AuthSvcURL: f.svr()}, fakeLookup(nil)); err == nil || !strings.Contains(err.Error(), "--users") {
		t.Errorf("空清单应拒: %v", err)
	}
	if _, err := RunMigration(MigrateOptions{Users: []string{"admin"}}, fakeLookup(nil)); err == nil || !strings.Contains(err.Error(), "保留账号") {
		t.Errorf("保留账号应拒: %v", err)
	}
	bad := base
	bad.Users = []string{"ab"}
	if _, err := RunMigration(bad, fakeLookup(nil)); err == nil || !strings.Contains(err.Error(), "合法用户名") {
		t.Errorf("过短用户名应拒: %v", err)
	}
	noURL := base
	noURL.Users = []string{"alice"}
	noURL.AuthSvcURL = ""
	if _, err := RunMigration(noURL, fakeLookup(nil)); err == nil || !strings.Contains(err.Error(), "AUTH_SVC_URL") {
		t.Errorf("缺对侧地址应拒: %v", err)
	}
	noRoster := base
	noRoster.Users = []string{"alice"}
	if _, err := RunMigration(noRoster, fakeLookup(nil)); err == nil || !strings.Contains(err.Error(), "ROSTER_FILE") {
		t.Errorf("缺名册文件应拒（job 只写文件名册）: %v", err)
	}
}

func TestGenerateOneTimePassword(t *testing.T) {
	for i := 0; i < 50; i++ {
		p, err := generateOneTimePassword()
		if err != nil {
			t.Fatal(err)
		}
		if len(p) != 24 {
			t.Fatalf("长度 = %d, want 24", len(p))
		}
		var hasU, hasL, hasD, hasS bool
		for _, c := range p {
			switch {
			case c >= 'A' && c <= 'Z':
				hasU = true
			case c >= 'a' && c <= 'z':
				hasL = true
			case c >= '0' && c <= '9':
				hasD = true
			default:
				hasS = true
			}
		}
		if !hasU || !hasL || !hasD || !hasS {
			t.Fatalf("四类字符须齐备: %q", p)
		}
	}
}

// --- 测试小工具 ---

func (f *farSide) svr() string { return f.srv.URL }

func (f *farSide) createdCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.created
}

func adminCreds0() string { return "admin" }
func adminCreds1() string { return "Far-Admin-2026x!" }

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
