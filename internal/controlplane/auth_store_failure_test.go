package controlplane

// auth_store_failure_test.go — §3.6/§3.7 守卫（2026-10-14 演练移交/待办）：
// **store 读故障必须与"用户不存在/未激活"分流**。
//
// 症状（两个人设独立复现）：后端抖动窗口内任意账号登录 ⇒ 401 "invalid username
// or password"，且失败**计入** loginGuard ⇒ 运维 5 次后被锁 15 分钟；恢复后原口令
// 立即可登（口令从来没错，错的是把故障当成了凭据错误）。§3.7 同源：refresh 路径把
// GetUser 的 nil（读失败与查无被压成同一个 nil）当"user not active" ⇒ 清 Cookie +
// 401 ⇒ 故障期一次 refresh 销毁活跃会话。
//
// 判据三条：① 登录遇 store 故障 ⇒ 503（不是 401）且**不记**防爆破失败；② refresh
// 遇 store 故障 ⇒ 503 且**不清** Cookie；③ 未命中故障的用户走原路径逐字不变
// （分流只对故障目标生效，不是"故障模式一亮全线 503"）。

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/store"
)

// failingUserStore 包装真实 store 并把两个用户查询变体染成 store 故障
// （实现 userLookupErr 可选接口——正是 SQL 后端新增的能力；内存后端不实现它）。
type failingUserStore struct {
	store.Store
	failName string // GetUserByUsernameErr 命中此用户名时返回故障
	failID   string // GetUserErr 命中此 ID 时返回故障
}

var errStoreRead = errors.New("simulated store read failure")

func (f *failingUserStore) GetUserByUsernameErr(username string) (*store.User, error) {
	if username == f.failName {
		return nil, errStoreRead
	}
	return f.GetUserByUsername(username), nil
}

func (f *failingUserStore) GetUserErr(id string) (*store.User, error) {
	if id == f.failID {
		return nil, errStoreRead
	}
	return f.GetUser(id), nil
}

func postLogin(t *testing.T, s *Server, username, password string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"username":"`+username+`","password":"`+password+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleAuthLogin(rec, req)
	return rec
}

// TestLogin_StoreReadFailureIsNotCredentialError 判据①：store 故障 ⇒ 503、
// 不记防爆破失败（故障不能把用户锁在门外，也不冒充凭据错误）。
func TestLogin_StoreReadFailureIsNotCredentialError(t *testing.T) {
	s := newAuthTestServer(t)
	s.store = &failingUserStore{Store: s.store, failName: "admin"}

	rec := postLogin(t, s, "admin", "admin123")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("store 故障应 503（而非 401 冒充凭据错误——§3.6 原状），实得 %d：%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "temporarily unavailable") {
		t.Errorf("503 文案应说明「暂时不可用、稍后重试」，实得 %s", rec.Body.String())
	}
	// 关键：故障**不得**计入防爆破——否则一次抖动等于替攻击者铺好锁。
	if s.loginGuard.Locked("admin") {
		t.Error("store 故障不得把账号推进锁定态（演练实测：5 次故障 ⇒ 锁 15 分钟）")
	}
}

// TestLogin_OtherUsersUnaffectedDuringStoreFailure 判据③：分流只命中故障目标——
// 换一个不故障的用户名仍走原逻辑（错误口令 ⇒ 401 + 正常计数），证明没有
// "故障模式一旦打开就全线 503"。
func TestLogin_OtherUsersUnaffectedDuringStoreFailure(t *testing.T) {
	s := newAuthTestServer(t)
	// 只让 "ghost-user" 的查询故障；admin 走真实内存路径。
	s.store = &failingUserStore{Store: s.store, failName: "ghost-user"}

	rec := postLogin(t, s, "admin", "wrong-password")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("非故障用户的错误口令仍应 401，实得 %d：%s", rec.Code, rec.Body.String())
	}
	// 真失败仍照常计数（连续 5 次应锁——证明分流没有顺手关掉真防爆破）。
	for i := 0; i < 5; i++ {
		s.loginGuard.RecordFail("admin")
	}
	if !s.loginGuard.Locked("admin") {
		t.Error("非故障路径的失败计数应照常记录（分流没有顺手关掉真防爆破）")
	}
	s.loginGuard.ResetFail("admin")

	rec = postLogin(t, s, "ghost-user", "whatever")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("故障用户应 503，实得 %d：%s", rec.Code, rec.Body.String())
	}
}

// TestRefresh_StoreReadFailureKeepsSession 判据②：refresh 遇 store 故障 ⇒ 503 且
// **不清 Cookie**（活跃会话不被一次故障摧毁）。
func TestRefresh_StoreReadFailureKeepsSession(t *testing.T) {
	s := newAuthTestServer(t)
	admin := s.store.GetUserByUsername("admin")
	if admin == nil {
		t.Fatal("前置失败：admin 不存在")
	}
	rt, err := s.createRefreshToken(admin.ID, admin.TenantID, "")
	if err != nil {
		t.Fatalf("创建 refresh token: %v", err)
	}
	// 换成故障 store（该用户 ID 的查询染故障）。
	s.store = &failingUserStore{Store: s.store, failID: admin.ID}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: refreshTokenCookieName, Value: rt})
	rec := httptest.NewRecorder()
	s.handleAuthRefresh(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("refresh 遇 store 故障应 503，实得 %d：%s", rec.Code, rec.Body.String())
	}
	for _, sc := range rec.Result().Cookies() {
		if (sc.Name == accessTokenCookieName || sc.Name == refreshTokenCookieName) && sc.MaxAge < 0 {
			t.Errorf("故障期不得清空 %s Cookie（§3.7：一次 refresh 不该摧毁活跃会话）", sc.Name)
		}
	}
}
