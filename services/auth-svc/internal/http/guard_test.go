// guard_test.go — A2 防爆破/口令策略测试（与 controlplane 参数对照的防回归锚）。
package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/auth-svc/internal/auth"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/store"
)

// ============ validateStrongPassword（与 controlplane 同规则集） ============

func TestValidateStrongPassword(t *testing.T) {
	bad := map[string]string{
		"short":   "Ab1",      // 太短
		"noUpper": "abcdefg1", // 缺大写
		"noLower": "ABCDEFG1", // 缺小写
		"noDigit": "Abcdefgh", // 缺数字
		"empty":   "",         // 空
	}
	for name, pw := range bad {
		if msg := validateStrongPassword(pw); msg == "" {
			t.Errorf("%s: %q 应被拒绝", name, pw)
		}
	}
	good := []string{"Abcdefg1", "NewPass123x", "Xy9Zabcd"}
	for _, pw := range good {
		if msg := validateStrongPassword(pw); msg != "" {
			t.Errorf("合法口令 %q 被拒: %s", pw, msg)
		}
	}
}

// ============ loginGuard IP 令牌桶 ============

func TestGuard_IPRateLimit(t *testing.T) {
	g := newLoginGuard()
	ip := "10.0.0.1"
	// burst=5：前 5 次放行。
	for i := 0; i < 5; i++ {
		if !g.allowIP(ip) {
			t.Fatalf("第 %d 次应放行（burst=5 内）", i+1)
		}
	}
	// 第 6 次限流。
	if g.allowIP(ip) {
		t.Fatal("超 burst 应限流")
	}
	// 不同 IP 不受影响。
	if !g.allowIP("10.0.0.2") {
		t.Fatal("不同 IP 应独立计数")
	}
}

// ============ loginGuard 账号锁定 ============

func TestGuard_AccountLockAfterFiveFails(t *testing.T) {
	g := newLoginGuard()
	// 5 次失败 → 锁定。
	for i := 0; i < 5; i++ {
		g.recordFail("victim")
	}
	if !g.accountLocked("victim") {
		t.Fatal("5 次失败后账号应锁定")
	}
	if g.accountLocked("other-user") {
		t.Fatal("不应影响其他账号")
	}
	// 成功复位。
	g.recordSuccess("victim")
	if g.accountLocked("victim") {
		t.Fatal("成功后锁定应复位")
	}
}

func TestGuard_FailWindowReset(t *testing.T) {
	g := newLoginGuard()
	// 4 次失败（未达阈值）→ 模拟窗口过期 → 再 4 次仍不锁（窗口重置语义）。
	for i := 0; i < 4; i++ {
		g.recordFail("u1")
	}
	g.mu.Lock()
	rec := g.fails["u1"]
	rec.windowAt = time.Now().Add(-guardFailWindow - time.Minute) // 窗口已过期
	g.mu.Unlock()
	g.recordFail("u1") // 过期后新窗口：计数重置为 1
	g.mu.Lock()
	fails := g.fails["u1"].fails
	g.mu.Unlock()
	if fails != 1 {
		t.Fatalf("过期窗口后计数应重置为 1，实际 %d", fails)
	}
}

// ============ 网关层集成：登录防爆破生效 ============

func TestGateway_LoginLockoutIntegration(t *testing.T) {
	// 两道闸分开验证：IP 令牌桶（burst 5）先于账号锁（5 次）触发——
	// 同 IP 连打 6 次会先炸 IP 闸，账号闸须用"错密 4 次+正确密码"隔离验证。
	g := newLoginGuard()

	// 闸 1（IP 桶）：纯 guard 级验证（网关级同代码路径，avoid 网关测试与 IP 闸耦合）。
	for i := 0; i < 5; i++ {
		if !g.allowIP("192.0.2.1") {
			t.Fatalf("第 %d 次 IP 应放行（burst=5）", i+1)
		}
	}
	if g.allowIP("192.0.2.1") {
		t.Fatal("第 6 次 IP 应限流")
	}

	// 闸 2（账号锁）：5 次错密（分散 IP 避开 IP 闸）触发锁定 → 第 6 次正确密码被拒。
	_, mux, svc := newTestGateway()
	clearMustChangePassword(t, svc)
	for i := 0; i < guardMaxFails; i++ {
		ip := "10.1." + string(rune('0'+i)) + ".1" // 分散 IP：IP 闸各 1 次不超 burst
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
			strings.NewReader(`{"username":"admin","password":"wrong"}`))
		req.RemoteAddr = ip + ":12345"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("第 %d 次错密应 401，实际 %d", i+1, rec.Code)
		}
	}
	// 锁定已触发（guardMaxFails 次错密）：正确密码也被拒 429 locked。
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"username":"admin","password":"admin123"}`))
	req.RemoteAddr = "10.9.9.9:12345"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("锁定期正确密码应被拒 429，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "locked") {
		t.Error("锁定响应应提示 locked")
	}
}

func TestGateway_RegisterRejectsWeakPassword(t *testing.T) {
	_, mux, _ := newTestGateway()
	// 弱口令注册 → 400。
	rec := doReq(t, mux, http.MethodPost, "/api/v1/auth/register",
		`{"username":"weakpw","password":"abc","email":"w@x.io"}`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("弱口令注册应 400，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	// 强口令注册 → 201。
	rec2 := doReq(t, mux, http.MethodPost, "/api/v1/auth/register",
		`{"username":"strongpw","password":"GoodPass123","email":"s@x.io"}`, nil)
	if rec2.Code != http.StatusCreated {
		t.Fatalf("强口令注册应 201，实际 %d body=%s", rec2.Code, rec2.Body.String())
	}
}

// ============ admin 轮换（store 级语义——main 的 rotate 函数是纯 store 操作） ============

func TestAdminRotation_Semantics(t *testing.T) {
	// 验证 store 支持轮换所需的完整操作序列（main.rotateDefaultAdminPassword 依赖）：
	// ChangePassword 落新口令（会清标记——正常用户改密语义）→ UpdateUser 置回标记
	//（轮换非用户主动改密，MustChangePassword 保持 true，controlplane 同语义）。
	eng := auth.NewEngine("t", 15*time.Minute, 7*24*time.Hour)
	st := store.NewMemoryStore()
	_ = service.NewService(eng, st) // 触发 seed（admin/admin123 + MustChangePassword）

	u := st.GetUserByUsername("admin")
	if u == nil || !u.MustChangePassword {
		t.Fatal("seed admin 应带 MustChangePassword=true")
	}
	if !auth.VerifyPassword(u.PasswordHash, "admin123") {
		t.Fatal("seed 口令应为 admin123（轮换触发条件）")
	}
	// 轮换序列：ChangePassword（清标记）→ SetMustChangePassword 置回 true。
	hash, _ := auth.HashPassword("RandomNewPass1")
	if err := st.ChangePassword(u.ID, hash); err != nil {
		t.Fatalf("改密: %v", err)
	}
	u2 := st.GetUserByUsername("admin")
	if u2 == nil {
		t.Fatal("改密后用户应仍存在")
	}
	if auth.VerifyPassword(u2.PasswordHash, "admin123") {
		t.Fatal("改密后弱口令应失效")
	}
	if u2.MustChangePassword {
		t.Fatal("ChangePassword 语义应清标记（改密完成）")
	}
	if err := st.SetMustChangePassword(u.ID, true); err != nil {
		t.Fatalf("置回标记: %v", err)
	}
	u3 := st.GetUserByUsername("admin")
	if !u3.MustChangePassword {
		t.Fatal("SetMustChangePassword(true) 后标记应为 true")
	}
	if !auth.VerifyPassword(u3.PasswordHash, "RandomNewPass1") {
		t.Fatal("置回标记不应影响口令哈希")
	}
}

// 防编译器收报 headers 参数未用（doReq 已用）。
var _ = httptest.NewRecorder
