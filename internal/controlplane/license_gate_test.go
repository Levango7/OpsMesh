package controlplane

// license_gate_test.go 商业授权闸门（Open-Core）的接线测试。
//
// 覆盖三层语义：
//  1. initLicenseState：配置 → 授权状态的映射，含"任何失败都不阻止启动"；
//  2. licensed / licenseSnapshot：并发安全 + 不泄露密钥材料；
//  3. 交付层闸门：/enterprise/ 未授权降级、/enterprise/assets/ 402、/api/v1/license 可查。
//
// 一条贯穿性断言：响应体与快照里**永远不应出现凭据原文或私钥**。
// 授权信息会出现在日志、诊断包与前端页面上，泄露即等于凭据可被复制盗用。

import (
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/controlplane/embed"
)

// newLicenseTestServer 构造一个带指定授权状态的 Server。
func newLicenseTestServer(t *testing.T, st *licenseState) *Server {
	t.Helper()
	s := newTestServer()
	s.lic = st
	return s
}

// communityState 构造"未授权"状态（模拟默认部署：未配公钥）。
func communityState() *licenseState {
	return initLicenseState(&config.Config{})
}

// enterpriseState 构造"已授权"状态：现场生成密钥对 → 签发凭据 → 用公钥校验。
// 用真实的 SignLicense/VerifyLicense 而非手工构造 state，确保测试覆盖的是生产路径。
func enterpriseState(t *testing.T) (*licenseState, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("生成密钥对失败: %v", err)
	}
	tok, err := config.SignLicense(config.License{
		Edition:  config.LicenseEditionEnterprise,
		Customer: "acme-corp",
		Devices:  50,
		Expires:  time.Now().AddDate(1, 0, 0),
		IssuedAt: time.Now(),
	}, priv)
	if err != nil {
		t.Fatalf("签发凭据失败: %v", err)
	}
	pubB64 := base64.RawURLEncoding.EncodeToString(pub)
	st := initLicenseState(&config.Config{
		LicenseKey:       tok,
		LicensePublicKey: pubB64,
	})
	if !st.licensed {
		t.Fatalf("构造的凭据应通过校验，实际未授权：reason=%q", st.reason)
	}
	return st, tok
}

// TestInitLicenseState_CommunityByDefault 未配公钥 → 社区版，且**不是错误**。
// 这是开源内核的默认形态：任何人 clone 下来都能跑，不该有任何"缺授权"的噪音。
func TestInitLicenseState_CommunityByDefault(t *testing.T) {
	st := initLicenseState(&config.Config{})
	if st.licensed {
		t.Fatal("未配公钥时不得视为已授权")
	}
	if !st.resolved {
		t.Fatal("状态必须已定稿（resolved），否则 licensed() 每次都会重算")
	}
	if st.license != nil {
		t.Fatal("未授权时不得保留载荷指针")
	}
	if st.reason == "" {
		t.Fatal("未授权必须给出原因（运维要能据此排查）")
	}
}

// TestInitLicenseState_NilConfig nil 配置不得 panic（fail-closed 降级为社区版）。
func TestInitLicenseState_NilConfig(t *testing.T) {
	st := initLicenseState(nil)
	if st == nil || st.licensed {
		t.Fatalf("nil 配置应降级为社区版；got=%+v", st)
	}
	if !st.resolved {
		t.Fatal("nil 配置也应定稿状态")
	}
}

// TestInitLicenseState_AllFailureModesDoNotBlockStartup 各种坏配置都降级为社区版而非启动失败。
// 逐项：公钥非法、凭据缺失、凭据格式错、签名不符、已过期、edition 不符。
func TestInitLicenseState_AllFailureModesDoNotBlockStartup(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	pubB64 := base64.RawURLEncoding.EncodeToString(pub)
	otherPub, _, _ := ed25519.GenerateKey(nil)

	expired, err := config.SignLicense(config.License{
		Edition: config.LicenseEditionEnterprise, Expires: time.Now().Add(-time.Hour),
	}, priv)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	wrongEdition, err := config.SignLicense(config.License{Edition: "community"}, priv)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	valid, err := config.SignLicense(config.License{
		Edition: config.LicenseEditionEnterprise, Expires: time.Now().AddDate(1, 0, 0),
	}, priv)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}

	cases := []struct {
		name       string
		key, pubIn string
		wantReason string // 必须出现在 reason 中的关键词
	}{
		{"公钥非法", valid, "not-a-key", "解析失败"},
		{"凭据缺失", "", pubB64, "未配置授权凭据"},
		{"凭据格式错", "garbage", pubB64, "格式非法"},
		{"签名不符", valid, base64.RawURLEncoding.EncodeToString(otherPub), "签名校验失败"},
		{"已过期", expired, pubB64, "已过期"},
		{"档位不符", wrongEdition, pubB64, "档位不覆盖"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := initLicenseState(&config.Config{LicenseKey: c.key, LicensePublicKey: c.pubIn})
			if st.licensed {
				t.Fatal("坏配置不得被判定为已授权")
			}
			if !st.resolved {
				t.Fatal("状态必须定稿")
			}
			if !strings.Contains(st.reason, c.wantReason) {
				t.Fatalf("reason 应包含 %q；got=%q", c.wantReason, st.reason)
			}
		})
	}
}

// TestServerLicensed_NilStateFailsClosed lic 未初始化（测试直接构造 Server）时按未授权处理。
// 方向很重要：宁可误降级为社区版，也不能因为漏初始化就默认放行企业版。
func TestServerLicensed_NilStateFailsClosed(t *testing.T) {
	s := newTestServer() // 不设置 s.lic
	if s.licensed() {
		t.Fatal("lic 为 nil 时必须按未授权处理（fail-closed）")
	}
	snap := s.licenseSnapshot()
	if snap["licensed"] != false || snap["edition"] != "community" {
		t.Fatalf("nil 状态快照应表示社区版；got=%v", snap)
	}
}

// TestLicenseSnapshot_Shape 快照字段：授权时含客户/设备/到期；未授权时含原因。
func TestLicenseSnapshot_Shape(t *testing.T) {
	t.Run("授权态", func(t *testing.T) {
		st, _ := enterpriseState(t)
		snap := newLicenseTestServer(t, st).licenseSnapshot()
		if snap["edition"] != config.LicenseEditionEnterprise {
			t.Fatalf("edition 应为 enterprise；got=%v", snap["edition"])
		}
		if snap["customer"] != "acme-corp" {
			t.Fatalf("customer 应回填；got=%v", snap["customer"])
		}
		if snap["devices"] != 50 {
			t.Fatalf("devices 应回填；got=%v", snap["devices"])
		}
		if snap["expiresAt"] == "" || snap["expiresAt"] == nil {
			t.Fatalf("expiresAt 应存在；got=%v", snap["expiresAt"])
		}
		if _, ok := snap["reason"]; ok {
			t.Fatal("授权态不应带 reason 字段（会让前端误判为降级）")
		}
	})
	t.Run("社区态", func(t *testing.T) {
		snap := newLicenseTestServer(t, communityState()).licenseSnapshot()
		if snap["edition"] != "community" || snap["licensed"] != false {
			t.Fatalf("社区态快照不正确；got=%v", snap)
		}
		if snap["reason"] == nil || snap["reason"] == "" {
			t.Fatal("社区态必须带 reason")
		}
		if _, ok := snap["customer"]; ok {
			t.Fatal("社区态不应带 customer 字段")
		}
	})
}

// TestLicenseSnapshot_NeverLeaksCredential 快照与页面都不得出现凭据原文。
func TestLicenseSnapshot_NeverLeaksCredential(t *testing.T) {
	st, tok := enterpriseState(t)
	snap := newLicenseTestServer(t, st).licenseSnapshot()
	for k, v := range snap {
		if s, ok := v.(string); ok && strings.Contains(s, tok) {
			t.Fatalf("快照字段 %q 泄露了凭据原文", k)
		}
		if s, ok := v.(string); ok && strings.Contains(s, ".") && len(s) > 60 {
			t.Fatalf("快照字段 %q 疑似包含凭据：%.40q…", k, s)
		}
	}
	// 未授权页同理。
	page := string(enterpriseUnlicensedPage(newLicenseTestServer(t, communityState()).licenseSnapshot()))
	if strings.Contains(page, tok) {
		t.Fatal("未授权页泄露了凭据原文")
	}
}

// TestLicenseReasonText_EscapesDynamicParts 原因文案里的动态部分（公钥解析错误）会回显用户输入，
// 页面必须转义 —— 否则 --license-public-key 里的内容会变成 HTML 注入点。
func TestLicenseReasonText_EscapesDynamicParts(t *testing.T) {
	st := initLicenseState(&config.Config{
		LicensePublicKey: "not-a-key",
		LicenseKey:       "x",
	})
	reason, _ := newLicenseTestServer(t, st).licenseSnapshot()["reason"].(string)
	if reason == "" {
		t.Fatal("reason 不应为空")
	}
	page := string(enterpriseUnlicensedPage(newLicenseTestServer(t, st).licenseSnapshot()))
	if strings.Contains(page, "<script") {
		t.Fatal("未授权页不得包含可执行脚本")
	}
	// 单独验证转义函数本身。
	if got := htmlEscape(`<img src=x onerror=alert(1)>`); strings.Contains(got, "<img") {
		t.Fatalf("htmlEscape 未转义尖括号；got=%q", got)
	}
}

// TestEnterpriseUI_UnlicensedShowsNoticePage 未授权 + 已内置 → 返回「未授权」说明页（200 HTML）。
//
// 只在已内置真实产物时才有意义：未内置时给的是构建指引页（优先级更高）。
func TestEnterpriseUI_UnlicensedShowsNoticePage(t *testing.T) {
	if !bundleAvailable() {
		t.Skip("未组装企业版产物：未授权降级页仅在已内置时有意义（占位态由既有测试覆盖）")
	}
	s := newEnterpriseServer(t)
	s.lic = communityState()

	rec := httptest.NewRecorder()
	s.handleEnterpriseUI(rec, httptest.NewRequest(http.MethodGet, "/enterprise/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("未授权说明页应 200（不是 4xx 白屏）；got=%d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("Content-Type 应为 text/html；got=%q", ct)
	}
	if h := rec.Header().Get("X-OpsMesh-License"); h != "community" {
		t.Fatalf("应显式声明授权态便于排障；got=%q", h)
	}
	body := rec.Body.String()
	// 关键：不得泄露任何前端分包（否则等于把企业版产物交付出去了）。
	if strings.Contains(body, "/enterprise/assets/") {
		t.Fatalf("未授权时不得引用企业版分包；body=%.300q", body)
	}
	for _, want := range []string{"未授权", "社区版", "Apache-2.0", "--license-key"} {
		if !strings.Contains(body, want) {
			t.Fatalf("说明页应包含 %q（要让用户知道缺什么、怎么买）；body=%.300q", want, body)
		}
	}
}

// TestEnterpriseUI_UnlicensedSPADeepLinksAlsoGated SPA 深链（/enterprise/devices）同样不得泄露外壳。
// 只按 /enterprise/ 拦会漏掉深链——攻击者/用户可直接访问子路由。
func TestEnterpriseUI_UnlicensedSPADeepLinksAlsoGated(t *testing.T) {
	if !bundleAvailable() {
		t.Skip("未组装企业版产物：SPA 深链需真实产物")
	}
	s := newEnterpriseServer(t)
	s.lic = communityState()
	for _, p := range []string{"/enterprise/devices", "/enterprise/settings/profile", "/enterprise/audit"} {
		rec := httptest.NewRecorder()
		s.handleEnterpriseUI(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if strings.Contains(rec.Body.String(), "/enterprise/assets/") {
			t.Fatalf("未授权时深链 %s 泄露了前端分包", p)
		}
		if rec.Header().Get("X-OpsMesh-License") != "community" {
			t.Fatalf("深链 %s 应声明授权态；headers=%v", p, rec.Header())
		}
	}
}

// TestEnterpriseAsset_UnlicensedReturns402 未授权时**存在**的静态资源 402。
// 只对真实存在的文件断言：路径穿越与不存在的资源一律 404（无论授权态），
// 402 专门回答"资源存在但你没用企业版"。
func TestEnterpriseAsset_UnlicensedReturns402(t *testing.T) {
	if !bundleAvailable() {
		t.Skip("未组装企业版产物：资源闸门需真实产物")
	}
	// 从 embed 里找一个确实存在的 assets 文件（真实产物路径随构建哈希变化，不能硬编码）。
	var real string
	entries, err := embed.EnterpriseFS.ReadDir("enterprise/assets")
	if err != nil {
		t.Skipf("无法枚举企业版资源目录: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			real = "/enterprise/assets/" + e.Name()
			break
		}
	}
	if real == "" {
		t.Skip("embed 中无顶层资源文件")
	}
	s := newEnterpriseServer(t)
	s.lic = communityState()
	rec := httptest.NewRecorder()
	s.handleEnterpriseAsset(rec, httptest.NewRequest(http.MethodGet, real, nil))
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("未授权时企业版资源 %s 应 402；got=%d body=%.200q", real, rec.Code, rec.Body.String())
	}
	if rec.Body.Len() == 0 {
		t.Fatal("402 响应应带说明体（告知如何授权），不应是空响应")
	}
}

// TestEnterpriseAsset_MissingIs404Not402 不存在的资源在**任何**授权态下都应 404。
// 若返回 402，会把"URL 打错了"伪装成"需要买企业版"，是最容易浪费一轮排障的误导。
func TestEnterpriseAsset_MissingIs404Not402(t *testing.T) {
	if !bundleAvailable() {
		t.Skip("未组装企业版产物")
	}
	s := newEnterpriseServer(t)
	s.lic = communityState() // 未授权
	for _, p := range []string{
		"/enterprise/assets/",                             // 目录，不是文件
		"/enterprise/assets/definitely-missing-xyz.js",    // 构造上不会命中
		"/enterprise/assets/js/definitely-missing-xyz.js", // 同上（含子目录）
	} {
		rec := httptest.NewRecorder()
		s.handleEnterpriseAsset(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("不存在的资源 %s 应 404（无论授权态）；got=%d body=%.160q", p, rec.Code, rec.Body.String())
		}
	}
}

// TestEnterpriseAsset_PathTraversalBeatsLicenseGate 路径穿越检查在授权闸门**之前**。
// 顺序很重要：若先判授权，穿越探测会得到 402 而不是 404，掩盖真实的安全检查；
// 且未授权时也不该因"碰巧没授权"而掩盖路径穿越这一事实。
func TestEnterpriseAsset_PathTraversalBeatsLicenseGate(t *testing.T) {
	s := newEnterpriseServer(t)
	s.lic = communityState() // 未授权
	for _, p := range []string{
		"/enterprise/assets/../../../etc/passwd",
		"/enterprise/assets/../index.html",
		"/enterprise/assets/../../secret",
	} {
		rec := httptest.NewRecorder()
		s.handleEnterpriseAsset(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("穿越路径 %s 应 404（授权检查不得掩盖安全检查）；got=%d", p, rec.Code)
		}
	}
}

// TestEnterpriseUI_LicensedServesBundle 已授权时行为与授权前完全一致（回归保护）。
func TestEnterpriseUI_LicensedServesBundle(t *testing.T) {
	if !bundleAvailable() {
		t.Skip("未组装企业版产物")
	}
	s := newEnterpriseServer(t)
	s.lic, _ = enterpriseState(t)
	rec := httptest.NewRecorder()
	s.handleEnterpriseUI(rec, httptest.NewRequest(http.MethodGet, "/enterprise/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("已授权时应正常交付外壳；got=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "/enterprise/assets/") {
		t.Fatal("已授权时外壳应引用真实分包")
	}
	if h := rec.Header().Get("X-OpsMesh-License"); h != "" && h != "enterprise" {
		t.Fatalf("X-OpsMesh-License 取值异常：%q", h)
	}
}

// TestEnterpriseUI_CommunityCoreUnaffected 社区版控制台（GET /）在任何授权状态下都不受影响。
// 这是 Open-Core 的底线：闸门只作用于企业版交付物，绝不牵连 Apache-2.0 内核。
func TestEnterpriseUI_CommunityCoreUnaffected(t *testing.T) {
	for _, tc := range []struct {
		name string
		st   *licenseState
	}{
		{"社区版", communityState()},
		{"企业版", nil}, // 下面单独构造
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer()
			if tc.st == nil {
				s.lic, _ = enterpriseState(t)
			} else {
				s.lic = tc.st
			}
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("X-Tenant-ID", "default")
			rec := httptest.NewRecorder()
			s.handleDashboard(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("社区控制台在任何授权状态下都应 200；got=%d", rec.Code)
			}
			if rec.Body.Len() == 0 {
				t.Fatal("社区控制台不应被清空")
			}
		})
	}
}

// TestHandleLicenseStatus_RequiresAuth 授权状态端点必须鉴权（含客户名与设备上限，属商务信息）。
//
// 必须用**非 demo** server：newTestServer() 设了 cfg.Demo=true，而 requireProd 的
// demo 分支会宽松放行（无身份也通过）——那样测的就不是鉴权而是 demo 豁免了。
func TestHandleLicenseStatus_RequiresAuth(t *testing.T) {
	s := newEnterpriseServer(t) // requireAuth = true
	s.cfg.Demo = false          // 关掉 demo 豁免，才真正走 requireProd 的拒绝分支
	s.lic, _ = enterpriseState(t)

	rec := httptest.NewRecorder()
	s.handleLicenseStatus(rec, httptest.NewRequest(http.MethodGet, "/api/v1/license", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录时应 401；got=%d body=%.200q", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "acme-corp") {
		t.Fatalf("未登录响应不得包含客户名；body=%.200q", rec.Body.String())
	}
}

// TestHandleLicenseStatus_PermissionPointExists 授权端点用的权限点必须**真实存在**于 RBAC 规格中。
//
// 这条测试的存在理由：曾把权限点写成 diagnostics:read，而 RBAC 里 diagnostics 组
// 只有 :dump 与 :execute——不存在的权限名会让派生结果为空集，连 admin 都被 403，
// 且症状是"所有人 403"这种极易误判为鉴权配置错误的表现。
// 权限名写错不会有编译错误，只能靠断言兜住。
func TestHandleLicenseStatus_PermissionPointExists(t *testing.T) {
	perms := getRolePermCache()
	const required = "platform:read"
	found := false
	for _, p := range perms["admin"] {
		if p == required {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("权限点 %q 不存在于 RBAC 规格（admin 角色权限表里没有）；换一个真实存在的点", required)
	}
}

// TestHandleLicenseStatus_CommunityReachable 已登录用户（含未授权态）必须能查到"为什么没授权"。
// 若该端点也上闸，未授权态就成了黑盒：用户除了翻日志没有任何自查途径。
// 走网关注入身份路径（requireProd 第 3 步 + TrustGatewayHeaders），避免依赖 demo 豁免。
func TestHandleLicenseStatus_CommunityReachable(t *testing.T) {
	s := newEnterpriseServer(t)
	s.lic = communityState()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/license", nil)
	req.Header.Set("X-User-Roles", "admin")
	req.Header.Set("X-Tenant-ID", "default")
	s.cfg.Demo = false
	s.cfg.TrustGatewayHeaders = true
	s.handleLicenseStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("已登录查询授权状态应 200（这是唯一自查途径）；got=%d body=%.200q", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "community") {
		t.Fatalf("响应应表明当前为社区版；body=%.200q", body)
	}
	if !strings.Contains(body, "reason") {
		t.Fatalf("社区态响应必须带 reason（用户要知道下一步做什么）；body=%.200q", body)
	}
}

// TestHandleLicenseStatus_ViewerCanQuery 最低权限（viewer）也应能查授权档位。
// 用 admin 权限守门会让未授权用户（往往只有 viewer）恰好查不到自己为什么是社区版。
func TestHandleLicenseStatus_ViewerCanQuery(t *testing.T) {
	s := newEnterpriseServer(t)
	s.lic = communityState()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/license", nil)
	req.Header.Set("X-User-Roles", "viewer")
	req.Header.Set("X-Tenant-ID", "default")
	s.cfg.Demo = false
	s.cfg.TrustGatewayHeaders = true
	s.handleLicenseStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer 应能查授权档位（否则最低权限用户无法自查）；got=%d body=%.200q", rec.Code, rec.Body.String())
	}
}

// TestLicenseState_ConcurrentAccessSnapshot 并发读授权状态不得 data race / 死锁。
// 授权状态被 /api/v1/license、/enterprise/ 与闸门并发读，RWMutex 用错会直接崩服务。
func TestLicenseState_ConcurrentAccessSnapshot(t *testing.T) {
	s := newEnterpriseServer(t)
	s.lic, _ = enterpriseState(t)
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 200; j++ {
				_ = s.licensed()
				_ = s.licenseSnapshot()
			}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
}

// TestDashboardCTA_KeptForConversion 未授权时个人版首页**仍保留**企业版入口。
// 藏起来等于让潜在客户连"有企业版"都不知道——那不是转化，是屏蔽。
func TestDashboardCTA_KeptForConversion(t *testing.T) {
	if !bundleAvailable() {
		t.Skip("未组装企业版产物：入口本就被 bundleAvailable 隐藏")
	}
	s := newEnterpriseServer(t)
	s.lic = communityState()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleDashboard(rec, req)
	if !strings.Contains(rec.Body.String(), "/enterprise/") {
		t.Fatal("未授权时应保留企业版入口（转化路径）")
	}
}
