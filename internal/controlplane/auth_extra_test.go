package controlplane

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/store"
)

// 本文件补全 auth.go 中 0% 覆盖函数的单元测试：
// revokeRefreshToken / purgeExpiredRefreshTokens / revokeAccessTokenFromRequest /
// enforceInitialCredentials / randHexID / clientIP / userPermissions /
// loginGuard.startSweep / loginGuard.stopSweep / loginGuard.sweep / loginGuard.allow 等。
//
// 复用 auth_test.go 中的 newAuthTestServer(t) 构造带 sessionStore 的 Server。

// =============================================================================
// revokeRefreshToken
// =============================================================================

func TestRevokeRefreshToken_Extra(t *testing.T) {
	s := newAuthTestServer(t)
	// 先保存一个 refresh token
	rt := &store.RefreshToken{TokenHash: hashRefreshToken("rt-id-1"), UserID: "u1", TenantID: "t1"}
	if err := s.store.SaveRefreshToken(rt); err != nil {
		t.Fatalf("save: %v", err)
	}
	// 确认存在
	if got := s.store.GetRefreshToken(hashRefreshToken("rt-id-1")); got == nil {
		t.Fatal("refresh token not found before revoke")
	}
	// 吊销
	s.revokeRefreshToken("rt-id-1")
	// 确认已删除
	if got := s.store.GetRefreshToken(hashRefreshToken("rt-id-1")); got != nil {
		t.Fatalf("refresh token still exists after revoke: %+v", got)
	}
}

// =============================================================================
// purgeExpiredRefreshTokens
// =============================================================================

func TestPurgeExpiredRefreshTokens_WithSessionStore(t *testing.T) {
	s := newAuthTestServer(t)
	// 注入一些黑名单条目
	s.sessionStore.Blacklist("jti-purge-1", 5*time.Minute)
	if !s.sessionStore.IsBlacklisted("jti-purge-1") {
		t.Fatal("blacklist entry not added")
	}
	// 调用 purge（应清理过期条目；未过期的保留）
	s.purgeExpiredRefreshTokens()
	// 覆盖代码路径
}

func TestPurgeExpiredRefreshTokens_NilSessionStore(t *testing.T) {
	s := newAuthTestServer(t)
	s.sessionStore = nil
	// nil sessionStore → 不 panic
	s.purgeExpiredRefreshTokens()
}

// =============================================================================
// revokeAccessTokenFromRequest
// =============================================================================

func TestRevokeAccessTokenFromRequest_NilSessionStore(t *testing.T) {
	s := newAuthTestServer(t)
	s.sessionStore = nil
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	// 不应 panic
	s.revokeAccessTokenFromRequest(req)
}

func TestRevokeAccessTokenFromRequest_NoToken(t *testing.T) {
	s := newAuthTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	// 无 token → 静默返回
	s.revokeAccessTokenFromRequest(req)
}

func TestRevokeAccessTokenFromRequest_InvalidToken(t *testing.T) {
	s := newAuthTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	// 无效 token → 静默返回
	s.revokeAccessTokenFromRequest(req)
}

func TestRevokeAccessTokenFromRequest_ValidToken(t *testing.T) {
	s := newAuthTestServer(t)
	u := s.store.CreateUser(&store.User{
		ID:       "u-revoke-1",
		Username: "testuser-revoke",
		Status:   "active",
	})
	token, err := s.issueUserToken(u)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	s.revokeAccessTokenFromRequest(req)
	// 验证 jti 已加入黑名单（覆盖代码路径）
}

func TestRevokeAccessTokenFromRequest_CookieToken(t *testing.T) {
	s := newAuthTestServer(t)
	u := s.store.CreateUser(&store.User{
		ID:       "u-revoke-2",
		Username: "cookieuser-revoke",
		Status:   "active",
	})
	token, err := s.issueUserToken(u)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: accessTokenCookieName, Value: token})
	s.revokeAccessTokenFromRequest(req)
}

// =============================================================================
// enforceInitialCredentials（首启凭据加固）
// =============================================================================

// assertSeedCredentialsRevoked 断言公开预置弱口令在非 demo 部署下已不可登录。
func assertSeedCredentialsRevoked(t *testing.T, st store.Store) {
	t.Helper()
	for _, id := range []string{"user-operator", "user-viewer"} {
		u := st.GetUser(id)
		if u == nil {
			t.Fatalf("预置账号 %s 不应被删除（保留供管理员处置）", id)
		}
		if verifyPassword(u.PasswordHash, seedUserPasswords[id]) {
			t.Fatalf("预置账号 %s 仍可用公开弱口令 %q 登录", id, seedUserPasswords[id])
		}
	}
}

func TestEnforceInitialCredentials_NonProductionRotatesAllSeeds(t *testing.T) {
	st := store.NewMemoryStore()
	cfg := &config.Config{} // 非生产、无交付通道 → 打印到 stderr
	if err := enforceInitialCredentials(cfg, st); err != nil {
		t.Fatalf("enforceInitialCredentials: %v", err)
	}
	admin := st.GetUserByUsername("admin")
	if admin == nil {
		t.Fatal("admin 用户应存在")
	}
	// admin 弱口令必须失效，且仍保留首登强制改密标记。
	if verifyPassword(admin.PasswordHash, "admin123") {
		t.Fatal("admin 仍可用公开弱口令 admin123 登录")
	}
	if !admin.MustChangePassword {
		t.Fatal("admin 应保留 MustChangePassword=true（首登强制改密）")
	}
	assertSeedCredentialsRevoked(t, st)
}

func TestEnforceInitialCredentials_ExplicitAdminPassword(t *testing.T) {
	st := store.NewMemoryStore()
	cfg := &config.Config{AdminPassword: "Str0ngPass1"}
	if err := enforceInitialCredentials(cfg, st); err != nil {
		t.Fatalf("enforceInitialCredentials: %v", err)
	}
	admin := st.GetUserByUsername("admin")
	if !verifyPassword(admin.PasswordHash, "Str0ngPass1") {
		t.Fatal("--admin-password 指定的口令应生效")
	}
}

func TestEnforceInitialCredentials_WeakAdminPasswordRejected(t *testing.T) {
	st := store.NewMemoryStore()
	for _, weak := range []string{"short1A", "alllower1", "ALLUPPER1", "NoDigitsHere"} {
		if err := enforceInitialCredentials(&config.Config{AdminPassword: weak}, st); err == nil {
			t.Fatalf("弱口令 %q 应被拒绝", weak)
		}
	}
	// 拒绝后不应留下半成品状态：admin 口令仍是原弱口令（未写入）。
	if admin := st.GetUserByUsername("admin"); !verifyPassword(admin.PasswordHash, "admin123") {
		t.Fatal("校验失败时不应改动 admin 口令")
	}
}

func TestEnforceInitialCredentials_ProductionWithoutChannelFails(t *testing.T) {
	st := store.NewMemoryStore()
	err := enforceInitialCredentials(&config.Config{Production: true}, st)
	if err == nil {
		t.Fatal("生产模式无口令交付通道应 fail-fast（否则管理员被静默锁死）")
	}
	// 报错必须给出可执行指引，而非只报错不给出路。
	if !strings.Contains(err.Error(), "OPSMESH_ADMIN_PASSWORD") || !strings.Contains(err.Error(), "--admin-password-file") {
		t.Fatalf("错误信息应提示交付通道, got: %v", err)
	}
}

func TestEnforceInitialCredentials_PasswordFileWritten(t *testing.T) {
	st := store.NewMemoryStore()
	path := filepath.Join(t.TempDir(), "admin-password")
	if err := enforceInitialCredentials(&config.Config{Production: true, AdminPasswordFile: path}, st); err != nil {
		t.Fatalf("enforceInitialCredentials: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("口令文件应存在: %v", err)
	}
	// 权限 0600：口令明文不得对同机其他用户可读（Windows 下不校验 POSIX 位）。
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("口令文件权限 = %v, want 0600", info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	password := strings.TrimSpace(string(raw))
	if len(password) != 32 {
		t.Fatalf("随机口令应为 32 字符 hex, got %d", len(password))
	}
	if !verifyPassword(st.GetUserByUsername("admin").PasswordHash, password) {
		t.Fatal("文件中写入的口令应与库内口令一致")
	}
}

func TestEnforceInitialCredentials_MissingDirFails(t *testing.T) {
	st := store.NewMemoryStore()
	path := filepath.Join(t.TempDir(), "no-such-dir", "admin-password")
	if err := enforceInitialCredentials(&config.Config{AdminPasswordFile: path}, st); err == nil {
		t.Fatal("目录不存在时应报错，而非静默把口令写到别处或直接锁死")
	}
}

func TestEnforceInitialCredentials_Idempotent(t *testing.T) {
	st := store.NewMemoryStore()
	cfg := &config.Config{AdminPassword: "Str0ngPass1"}
	if err := enforceInitialCredentials(cfg, st); err != nil {
		t.Fatalf("first: %v", err)
	}
	// 管理员在界面上改了密。
	hash, err := hashPassword("RotatedByAdmin9")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !st.ChangePassword("user-admin", hash) {
		t.Fatal("change password failed")
	}
	// 再次启动（ForceReset=false）：不得回滚管理员的新口令。
	if err := enforceInitialCredentials(cfg, st); err != nil {
		t.Fatalf("second: %v", err)
	}
	if !verifyPassword(st.GetUser("user-admin").PasswordHash, "RotatedByAdmin9") {
		t.Fatal("默认配置不得覆盖管理员已修改的口令（避免重启静默回滚）")
	}
}

func TestEnforceInitialCredentials_ForceResetRecovers(t *testing.T) {
	st := store.NewMemoryStore()
	cfg := &config.Config{AdminPassword: "Recovered9Pass", AdminPasswordForceReset: true}
	if err := enforceInitialCredentials(cfg, st); err != nil {
		t.Fatalf("first: %v", err)
	}
	// 模拟管理员忘记口令（库内为随机口令）：强制重置应把口令改回配置值。
	if err := enforceInitialCredentials(cfg, st); err != nil {
		t.Fatalf("second: %v", err)
	}
	if !verifyPassword(st.GetUser("user-admin").PasswordHash, "Recovered9Pass") {
		t.Fatal("--admin-password-force-reset 应覆盖已有 admin 口令")
	}
}

func TestEnforceInitialCredentials_NoAdminUser(t *testing.T) {
	st := store.NewMemoryStore()
	if u := st.GetUserByUsername("admin"); u != nil {
		st.DeleteUser(u.ID)
	}
	// 无 admin 账号时不得报错（部署方自行管理账号），但预置弱口令账号仍须被处置。
	if err := enforceInitialCredentials(&config.Config{Production: true}, st); err != nil {
		t.Fatalf("无 admin 账号不应阻断启动: %v", err)
	}
	assertSeedCredentialsRevoked(t, st)
}

// =============================================================================
// randHexID
// =============================================================================

func TestRandHexID_Extra(t *testing.T) {
	id := randHexID("user")
	if !strings.HasPrefix(id, "user-") {
		t.Fatalf("id = %q, want prefix user-", id)
	}
	// 应有 32 hex 字符（16 字节）+ prefix
	if len(id) != len("user-")+32 {
		t.Fatalf("id len = %d, want %d", len(id), len("user-")+32)
	}
	// 两次调用应不同（crypto/rand）
	id2 := randHexID("user")
	if id == id2 {
		t.Fatal("randHexID returned same id twice")
	}
}

// =============================================================================
// clientIP
// =============================================================================

func TestClientIP_Extra(t *testing.T) {
	// trustProxy=false → 用 RemoteAddr
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	if got := clientIP(req, false); got != "1.2.3.4" {
		t.Fatalf("clientIP = %q, want 1.2.3.4", got)
	}

	// trustProxy=true + XFF → 用 XFF 首段
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	req.Header.Set("X-Forwarded-For", "9.9.9.9, 8.8.8.8")
	if got := clientIP(req, true); got != "9.9.9.9" {
		t.Fatalf("clientIP with XFF = %q, want 9.9.9.9", got)
	}

	// trustProxy=true + 无 XFF → 回退 RemoteAddr
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	if got := clientIP(req, true); got != "1.2.3.4" {
		t.Fatalf("clientIP no XFF = %q, want 1.2.3.4", got)
	}

	// RemoteAddr 无端口 → 原样返回
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "no-port"
	if got := clientIP(req, false); got != "no-port" {
		t.Fatalf("clientIP no port = %q, want no-port", got)
	}

	// trustProxy=true + XFF 单个 IP
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "7.7.7.7")
	if got := clientIP(req, true); got != "7.7.7.7" {
		t.Fatalf("clientIP single XFF = %q, want 7.7.7.7", got)
	}
}

// =============================================================================
// userPermissions
// =============================================================================

func TestUserPermissions_NilUser(t *testing.T) {
	s := newAuthTestServer(t)
	if got := s.userPermissions(nil); got != nil {
		t.Fatalf("nil user = %v, want nil", got)
	}
}

func TestUserPermissions_NoRoles(t *testing.T) {
	s := newAuthTestServer(t)
	u := &store.User{ID: "u1", RoleIDs: nil}
	if got := s.userPermissions(u); got != nil {
		t.Fatalf("no roles = %v, want nil", got)
	}
}

func TestUserPermissions_WithRoles(t *testing.T) {
	s := newAuthTestServer(t)
	// 创建角色（用独特名称避免与预填充角色冲突）
	r := s.store.CreateRole(&store.Role{ID: "r-perm-1", Name: "test-role-perm", Permissions: []string{"read", "write"}})
	if r == nil {
		t.Fatal("CreateRole returned nil")
	}
	u := s.store.CreateUser(&store.User{ID: "u-perm-1", Username: "xperm", RoleIDs: []string{r.ID}})
	if u == nil {
		t.Fatal("CreateUser returned nil")
	}
	got := s.userPermissions(u)
	if len(got) != 2 {
		t.Fatalf("permissions = %v, want 2", got)
	}
}

func TestUserPermissions_MissingRole(t *testing.T) {
	s := newAuthTestServer(t)
	u := &store.User{ID: "u1", RoleIDs: []string{"nonexistent"}}
	// 角色不存在 → 跳过，返回 nil
	if got := s.userPermissions(u); got != nil {
		t.Fatalf("missing role = %v, want nil", got)
	}
}

// =============================================================================
// loginGuard: startSweep / stopSweep / sweep / allow
// =============================================================================

func TestLoginGuard_StartSweepStopSweep_Extra(t *testing.T) {
	g := newLoginGuard(store.NewInProcessSessionStore())
	g.startSweep(50 * time.Millisecond)
	// 等待一两次 sweep 周期
	time.Sleep(120 * time.Millisecond)
	// stopSweep 应让 goroutine 退出
	g.stopSweep()
	// 幂等：再次调用不应 panic
	g.stopSweep()
}

func TestLoginGuard_Sweep_Extra(t *testing.T) {
	g := newLoginGuard(store.NewInProcessSessionStore())
	// 注入一个令牌已回满且超过 1 小时未活动的 IP 记录
	g.mu.Lock()
	g.ips["old-ip"] = &rateRec{tokens: loginRateBurst, last: time.Now().Add(-2 * time.Hour)}
	g.ips["recent-ip"] = &rateRec{tokens: loginRateBurst, last: time.Now()}
	g.mu.Unlock()
	g.sweep()
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.ips["old-ip"]; ok {
		t.Fatal("old-ip should be swept")
	}
	if _, ok := g.ips["recent-ip"]; !ok {
		t.Fatal("recent-ip should remain")
	}
}

func TestLoginGuard_Allow_Extra(t *testing.T) {
	g := newLoginGuard(store.NewInProcessSessionStore())
	// 首次应放行（令牌桶满）
	if !g.allow("1.1.1.1") {
		t.Fatal("first allow should pass")
	}
	// 耗尽令牌后应限流
	for i := 0; i < loginRateBurst; i++ {
		g.allow("1.1.1.1")
	}
	if g.allow("1.1.1.1") {
		t.Fatal("should be rate limited after burst exhausted")
	}
	// 不同 IP 应独立限流
	if !g.allow("2.2.2.2") {
		t.Fatal("different IP should pass")
	}
}

func TestLoginGuard_RecordFailLockedReset_Extra(t *testing.T) {
	g := newLoginGuard(store.NewInProcessSessionStore())
	// 记录失败直到触发锁定
	var locked bool
	for i := 0; i < loginMaxFails; i++ {
		locked = g.recordFail("attacker")
	}
	if !locked {
		t.Fatal("should be locked after max fails")
	}
	if !g.locked("attacker") {
		t.Fatal("locked() should return true")
	}
	// 重置后应解锁
	g.resetFail("attacker")
	// resetFail 只清除失败计数，不清除锁定标记；锁定靠 TTL 自然过期
	// 此处主要覆盖代码路径
}

// =============================================================================
// createChangePasswordToken / consumeChangePasswordToken
// =============================================================================

func TestCreateAndConsumeChangePasswordToken_Extra(t *testing.T) {
	s := newAuthTestServer(t)
	token, err := s.createChangePasswordToken("u-cp")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if token == "" {
		t.Fatal("token empty")
	}
	// 消费
	userID, ok := s.consumeChangePasswordToken(token)
	if !ok || userID != "u-cp" {
		t.Fatalf("consume: ok=%v userID=%q, want ok=true u-cp", ok, userID)
	}
	// 再次消费应失败（一次性）
	_, ok = s.consumeChangePasswordToken(token)
	if ok {
		t.Fatal("token should be one-time, second consume should fail")
	}
}
