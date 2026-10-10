package cutover

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseRosterStrictRejectsInvalidAndReserved(t *testing.T) {
	if _, err := parseRosterStrict("alice, bob\ncarol"); err != nil {
		t.Fatalf("合法名册被拒: %v", err)
	}
	if _, err := parseRosterStrict("ali ce"); err == nil {
		t.Error("含空格的条目应被拒（会被拆成两个条目，静默改变语义）")
	}
	if _, err := parseRosterStrict("admin"); err == nil {
		t.Error("保留账号 admin 必须被拒（两侧 seed 同名 ⇒ 跨库凭证混淆面）")
	}
	if _, err := parseRosterStrict("Admin"); err == nil {
		t.Error("保留账号检查应大小写不敏感")
	}
	if _, err := parseRosterStrict("-bad"); err == nil {
		t.Error("首字符为连字符应被拒")
	}
	if _, err := parseRosterStrict("ab"); err == nil {
		t.Error("过短条目应被拒")
	}
	// 注释与空白不算条目。
	set, err := parseRosterStrict("# 迁移工单 WO-2026-1011\nalice\n\n  bob  \n")
	if err != nil {
		t.Fatal(err)
	}
	if len(set) != 2 {
		t.Fatalf("注释/空白应被忽略，得到 %d 条: %v", len(set), set)
	}
}

func TestLoadRosterInlineAndFile(t *testing.T) {
	r, err := LoadRoster("alice,bob", "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Size() != 2 || !r.Match("alice") || r.Match("carol") || r.Match("") {
		t.Fatalf("内联名册裁决错误: size=%d", r.Size())
	}
	if r.Source() != "env:AUTH_CUTOVER_ROSTER" {
		t.Errorf("来源标注 = %q", r.Source())
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "roster.txt")
	if err := os.WriteFile(path, []byte("alice\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rf, err := LoadRoster("", path)
	if err != nil {
		t.Fatal(err)
	}
	if !rf.Match("alice") || rf.Size() != 1 {
		t.Fatalf("文件名册裁决错误: size=%d", rf.Size())
	}

	// 空来源 = 空名册（合法：切流未开始）。
	empty, err := LoadRoster("", "")
	if err != nil {
		t.Fatal(err)
	}
	if empty.Size() != 0 || empty.Match("alice") {
		t.Error("空名册不应命中任何用户")
	}
}

func TestLoadRosterFailFast(t *testing.T) {
	// 来源歧义：同时给 file 与 inline ⇒ 拒绝（与「配置非法整体拒绝」哲学一致）。
	if _, err := LoadRoster("alice", "x.txt"); err == nil {
		t.Error("file 与 inline 同时设置应被拒（歧义配置）")
	}
	// 文件不可读 ⇒ 拒绝启动（静默当空名册会让已迁移用户登录失败）。
	if _, err := LoadRoster("", filepath.Join(t.TempDir(), "nope.txt")); err == nil {
		t.Error("名册文件不可读应 fail-fast")
	}
	// 文件内容非法 ⇒ 拒绝启动。
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.txt")
	if err := os.WriteFile(bad, []byte("admin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRoster("", bad); err == nil {
		t.Error("含保留账号的名册文件应 fail-fast")
	}
}

func TestRosterReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roster.txt")
	if err := os.WriteFile(path, []byte("alice\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := LoadRoster("", path)
	if err != nil {
		t.Fatal(err)
	}
	// 追加 bob + 摘除 alice（一键回滚粒度 = 名册条目）。
	if err := os.WriteFile(path, []byte("bob\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	n, err := r.Reload()
	if err != nil {
		t.Fatalf("重载失败: %v", err)
	}
	if n != 1 || !r.Match("bob") || r.Match("alice") {
		t.Fatalf("重载后裁决未更新: n=%d bob=%v alice=%v", n, r.Match("bob"), r.Match("alice"))
	}
	// 坏内容 ⇒ **保留上一份可用名册**（清空会让已迁移用户登录失败，是用户可见事故）。
	if err := os.WriteFile(path, []byte("admin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reload(); err == nil {
		t.Fatal("坏名册应报错")
	}
	if !r.Match("bob") || r.Size() != 1 {
		t.Fatalf("坏名册重载后应保留旧名册（bob），实际 size=%d bob=%v", r.Size(), r.Match("bob"))
	}
	// 文件被删 ⇒ 同样保留旧名册并报错。
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reload(); err == nil {
		t.Fatal("文件不存在应报错")
	}
	if !r.Match("bob") {
		t.Error("文件消失后仍应保留旧名册")
	}
}

func TestRouterDecisions(t *testing.T) {
	os.Unsetenv("AUTH_SVC_PROXY_ENABLED")
	os.Unsetenv("AUTH_SVC_OWNS_NEW_ACCOUNTS")
	r, err := LoadRoster("alice", "")
	if err != nil {
		t.Fatal(err)
	}
	rt := NewRouter(r)

	// 总闸关闭：一切按本地（与切流前逐字一致），即使名册有内容。
	if rt.RouteLogin("alice") || rt.RouteRegister() || rt.RouteRefresh(false, false) || rt.RouteTokenOwner(false) {
		t.Fatal("AUTH_SVC_PROXY_ENABLED=false 时不得路由任何请求")
	}

	t.Setenv("AUTH_SVC_PROXY_ENABLED", "true")
	if !rt.RouteLogin("alice") {
		t.Error("名册命中应路由登录")
	}
	if rt.RouteLogin("bob") {
		t.Error("名册未命中不应路由登录")
	}
	if !rt.RouteUser("alice") {
		t.Error("me/logout 同按名册裁决")
	}
	if rt.RouteRegister() {
		t.Error("增量开关未开时注册留在本地（默认控制面持有）")
	}
	t.Setenv("AUTH_SVC_OWNS_NEW_ACCOUNTS", "true")
	if !rt.RouteRegister() {
		t.Error("增量开关打开后注册应路由")
	}

	// refresh：归属侧 + 名册双判。
	if rt.RouteRefresh(true, false) {
		t.Error("本地持有且用户未迁移 ⇒ 本地处理")
	}
	if !rt.RouteRefresh(true, true) {
		t.Error("本地持有但用户已迁移 ⇒ 送对侧（该用户重登一次，避免旧会话把用户永远留在本地）")
	}
	if !rt.RouteRefresh(false, false) {
		t.Error("本地无此 rt ⇒ 属对侧")
	}
	// change-password 的改密令牌：消费失败即不属本地 ⇒ 送对侧。
	if rt.RouteTokenOwner(true) {
		t.Error("本地持有改密令牌 ⇒ 本地处理")
	}
	if !rt.RouteTokenOwner(false) {
		t.Error("本地无此改密令牌 ⇒ 属对侧")
	}
}

func TestRouterNilSafety(t *testing.T) {
	var rt *Router
	if rt.Enabled() || rt.RouteLogin("alice") || rt.RouteRefresh(false, false) || rt.RouteRegister() {
		t.Error("nil Router 必须安全（全按本地）")
	}
	if rt.Roster() != nil {
		t.Error("nil Router 的 Roster 应为 nil")
	}
	var r *Roster
	if r.Match("x") || r.Size() != 0 || r.Source() != "empty" {
		t.Error("nil Roster 必须安全（不命中、空、来源标 empty）")
	}
}
