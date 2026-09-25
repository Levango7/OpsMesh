package controlplane

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 本文件测试企业版前端交付路径（P0-3）。
//
// 关键约束：这些用例必须在**两种构建状态下都通过**——
//  1. 占位状态（源码构建，embed 目录里只有 placeholder.html）：CI 的 build-test job 即此状态；
//  2. 已内置状态（跑过 deploy/docker/scripts/build-enterprise-web.sh 或镜像构建）：CI 的
//     enterprise-web job 会组装真实产物后再跑本文件。
//
// 因此断言分两类：与状态无关的不变量（路由可达、路径穿越被拒、方法限制、缓存策略）
// 直接断言；与状态相关的行为按 bundleAvailable() 分支断言。

// newEnterpriseServer 构造一个要求鉴权的控制面 Server（企业版外壳必须免鉴权可达）。
func newEnterpriseServer(t *testing.T) *Server {
	t.Helper()
	s := newTestServer()
	s.requireAuth = true
	return s
}

// TestEnterpriseUI_ShellReachableWithoutAuth 企业版外壳必须能在未登录状态加载。
// 理由：浏览器不会带 X-Tenant-ID，且空白外壳要先渲染出登录页；鉴权由 /api/v1/* 承担。
func TestEnterpriseUI_ShellReachableWithoutAuth(t *testing.T) {
	s := newEnterpriseServer(t)
	rec := httptest.NewRecorder()
	s.handleEnterpriseUI(rec, httptest.NewRequest(http.MethodGet, "/enterprise/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /enterprise/ 应 200（未登录也要能加载外壳）；got=%d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("Content-Type 应为 text/html；got=%q", ct)
	}
	if rec.Header().Get("Cache-Control") != "no-cache, no-store, must-revalidate" {
		t.Fatalf("外壳不得被缓存（升级后旧外壳会引用已删除的旧分包）；got=%q", rec.Header().Get("Cache-Control"))
	}
	body := rec.Body.String()
	if bundleAvailable() {
		// 真实产物：外壳应引用带哈希的分包（Vite 产物特征）。
		if !strings.Contains(body, "/enterprise/assets/") {
			t.Fatalf("已内置企业版时外壳应引用 /enterprise/assets/ 下的构建产物；body=%.200q", body)
		}
	} else {
		if rec.Header().Get("X-OpsMesh-Enterprise-Bundle") != "placeholder" {
			t.Fatalf("未内置时须显式声明 placeholder（便于运维定位）；headers=%v", rec.Header())
		}
		if !strings.Contains(body, "未内置") {
			t.Fatalf("未内置时应给出说明页而非空白/404；body=%.200q", body)
		}
	}
}

// TestEnterpriseUI_NoTrailingSlashRedirects /enterprise → /enterprise/。
// 缺此重定向时 Vite 的相对资源路径会挂到站点根上（/assets/... 与个人版冲突）。
func TestEnterpriseUI_NoTrailingSlashRedirects(t *testing.T) {
	s := newEnterpriseServer(t)
	rec := httptest.NewRecorder()
	s.handleEnterpriseUI(rec, httptest.NewRequest(http.MethodGet, "/enterprise", nil))

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("/enterprise 应 301 到 /enterprise/；got=%d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/enterprise/" {
		t.Fatalf("Location 应为 /enterprise/；got=%q", loc)
	}
}

// TestEnterpriseUI_MethodNotAllowed 非 GET/HEAD 返回 405 且带 Allow 头。
func TestEnterpriseUI_MethodNotAllowed(t *testing.T) {
	s := newEnterpriseServer(t)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		s.handleEnterpriseUI(rec, httptest.NewRequest(method, "/enterprise/", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s /enterprise/ 应 405；got=%d", method, rec.Code)
		}
		if !strings.Contains(rec.Header().Get("Allow"), "GET") {
			t.Fatalf("%s 405 应带 Allow 头；got=%q", method, rec.Header().Get("Allow"))
		}
	}
}

// TestEnterpriseAsset_TraversalRejected 静态资源处理器不得越出 embed 树。
func TestEnterpriseAsset_TraversalRejected(t *testing.T) {
	s := newEnterpriseServer(t)
	paths := []string{
		"/enterprise/assets/../../../etc/passwd",
		"/enterprise/assets/..%2f..%2fpasswd",
		"/enterprise/assets/../../web/index.html",
		"/enterprise/assets/",          // 目录本身
		"/enterprise/placeholder.html", // 非 assets 前缀（个人版资源路径不得由本处理器兜底）
		"/enterprise/index.html",       // 同上：外壳走 handleEnterpriseUI
	}
	for _, p := range paths {
		rec := httptest.NewRecorder()
		s.handleEnterpriseAsset(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s 应 404（路径穿越/越界访问）；got=%d body=%.120q", p, rec.Code, rec.Body.String())
		}
	}
}

// TestEnterpriseAsset_UnbuiltReturns404 未内置构建产物时，资源路径应为 404（不是占位 HTML）。
func TestEnterpriseAsset_UnbuiltReturns404(t *testing.T) {
	if bundleAvailable() {
		t.Skip("本仓库已组装企业版产物；占位态断言由未组装环境（CI build-test）覆盖")
	}
	s := newEnterpriseServer(t)
	rec := httptest.NewRecorder()
	s.handleEnterpriseAsset(rec, httptest.NewRequest(http.MethodGet, "/enterprise/assets/js/main.js", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("未内置时资源请求应 404；got=%d", rec.Code)
	}
}

// TestEnterpriseUI_SPAFallback 前端路由（history 模式）回退到 index.html。
// 仅在有真实产物时生效——占位态没有前端路由概念。
func TestEnterpriseUI_SPAFallback(t *testing.T) {
	if !bundleAvailable() {
		t.Skip("未组装企业版产物：SPA 回退仅对真实产物有意义")
	}
	s := newEnterpriseServer(t)
	for _, p := range []string{"/enterprise/devices", "/enterprise/alerts/rules", "/enterprise/settings/profile"} {
		rec := httptest.NewRecorder()
		s.handleEnterpriseUI(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s 应回退 index.html（200）；got=%d", p, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "/enterprise/assets/") {
			t.Fatalf("GET %s 回退内容不是前端外壳", p)
		}
	}
	// 缺失的分包必须显式 404（便于发现构建不完整），不得静默回退成 HTML。
	rec := httptest.NewRecorder()
	s.handleEnterpriseUI(rec, httptest.NewRequest(http.MethodGet, "/enterprise/assets/js/missing-chunk.js", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("缺失分包应 404（不得回退 HTML，否则前端报 MIME 错误难排查）；got=%d", rec.Code)
	}
}

// TestEnterpriseAsset_ContentTypesAndCache 内容类型与缓存策略不变量。
func TestEnterpriseAsset_ContentTypesAndCache(t *testing.T) {
	cases := []struct {
		path     string
		wantType string
	}{
		{"enterprise/assets/js/app.js", "application/javascript"},
		{"enterprise/assets/css/app.css", "text/css"},
		{"enterprise/assets/img/logo.svg", "image/svg+xml"},
		{"enterprise/assets/fonts/x.woff2", "font/woff2"},
		{"enterprise/assets/data.json", "application/json"},
	}
	for _, c := range cases {
		if ct := enterpriseContentType(c.path); !strings.Contains(ct, c.wantType) {
			t.Fatalf("%s 内容类型应为 %q；got=%q", c.path, c.wantType, ct)
		}
	}
	// 带哈希的 assets 可长缓存；入口文件必须每次校验。
	rec := httptest.NewRecorder()
	serveEnterpriseStatic(rec, httptest.NewRequest(http.MethodGet, "/enterprise/assets/js/app-abc123.js", nil),
		"enterprise/assets/js/app-abc123.js", []byte("x"))
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("带哈希的资源应 immutable 长缓存；got=%q", cc)
	}
	rec = httptest.NewRecorder()
	serveEnterpriseStatic(rec, httptest.NewRequest(http.MethodGet, "/enterprise/sw.js", nil),
		"enterprise/sw.js", []byte("x"))
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache, no-store, must-revalidate" {
		t.Fatalf("sw.js 不得被缓存（否则升级后仍走旧缓存策略）；got=%q", cc)
	}
}

// TestEnterpriseAsset_PrecompressedNegotiation 预压缩旁路（.br/.gz）协商：
// 仅当客户端声明支持且旁路文件存在时启用，并且必须带 Vary: Accept-Encoding。
func TestEnterpriseAsset_PrecompressedNegotiation(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/enterprise/sw.js", nil)
	req.Header.Set("Accept-Encoding", "br, gzip")

	// 用确定存在的文件（sw.js 在两种状态下都可能不存在）——直接对 dist 里的代表文件做单元校验：
	// 只要 embed 中存在 sw.js 即验证协商路径，否则跳过（占位态）。
	if _, err := readEnterpriseFile("enterprise/sw.js"); err != nil {
		t.Skip("embed 中无 sw.js（占位态）：预压缩协商需真实产物")
	}
	rec := httptest.NewRecorder()
	data, _ := readEnterpriseFile("enterprise/sw.js")
	serveEnterpriseStatic(rec, req, "enterprise/sw.js", data)
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		// 有 .br 旁路时应为 br；无旁路文件则为空（两者都合法），但不能是 gzip（br 优先）。
		if got != "br" {
			t.Fatalf("Accept-Encoding: br,gzip 时应优先 br 旁路；got=%q", got)
		}
		if !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
			t.Fatalf("压缩协商响应必须带 Vary: Accept-Encoding；got=%q", rec.Header().Get("Vary"))
		}
	}
	// 未声明压缩支持时不得返回压缩体（否则客户端无法解码）。
	plain := httptest.NewRequest(http.MethodGet, "/enterprise/sw.js", nil)
	rec = httptest.NewRecorder()
	serveEnterpriseStatic(rec, plain, "enterprise/sw.js", data)
	if enc := rec.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("未声明 Accept-Encoding 时不得压缩；got Content-Encoding=%q", enc)
	}

	// gzip 协商：头值是 gzip 但旁路文件后缀是 .gz，二者不可混用。
	// 曾因把「头值」直接当后缀拼接去查 .gzip，导致 gzip 客户端静默拿到未压缩原文——
	// 服务端仍 200、体积翻数倍，只有比对 Content-Encoding 与体积才能发现。
	for _, c := range []struct {
		accept   string
		wantEnc  string
		wantSufx string
	}{
		{"gzip", "gzip", "gz"},
		{"br", "br", "br"},
	} {
		enc, sufx, ok := negotiatedEncoding(httptest.NewRequest(http.MethodGet, "/x.js", nil), "enterprise/assets/js/x.js")
		// 无 Accept-Encoding 头：不协商。
		if ok || enc != "" || sufx != "" {
			t.Fatalf("无 Accept-Encoding 头不应协商；got enc=%q sufx=%q ok=%v", enc, sufx, ok)
		}
		req := httptest.NewRequest(http.MethodGet, "/x.js", nil)
		req.Header.Set("Accept-Encoding", c.accept)
		enc, sufx, ok = negotiatedEncoding(req, "enterprise/assets/js/x.js")
		if !ok || enc != c.wantEnc || sufx != c.wantSufx {
			t.Fatalf("Accept-Encoding=%q → (enc=%q sufx=%q ok=%v)，期望 (%q %q true)",
				c.accept, enc, sufx, ok, c.wantEnc, c.wantSufx)
		}
	}
	// 已是 .br/.gz 的路径不再二次协商（否则会去找 .br.br）。
	for _, p := range []string{"enterprise/assets/js/x.js.br", "enterprise/assets/js/x.js.gz"} {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("Accept-Encoding", "br, gzip")
		if _, _, ok := negotiatedEncoding(req, p); ok {
			t.Fatalf("%s 不应再协商压缩", p)
		}
	}

	// 端到端：真实存在 .gz 旁路时，Accept-Encoding: gzip 必须拿到 gz 体（体积小于原文）。
	if gz, err := readEnterpriseFile("enterprise/sw.js.gz"); err == nil {
		req := httptest.NewRequest(http.MethodGet, "/enterprise/sw.js", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		serveEnterpriseStatic(rec, req, "enterprise/sw.js", data)
		if enc := rec.Header().Get("Content-Encoding"); enc != "gzip" {
			t.Fatalf("存在 .gz 旁路时 gzip 客户端应得 Content-Encoding: gzip；got=%q", enc)
		}
		if rec.Body.Len() != len(gz) {
			t.Fatalf("gzip 响应体应为旁路文件内容（%d 字节）；got=%d 字节", len(gz), rec.Body.Len())
		}
		if len(gz) >= len(data) {
			t.Fatalf("旁路文件应小于原文（data 用例失真）；got %d >= %d", len(gz), len(data))
		}
	}
}

// TestEnterpriseCTA_Gating 个人版首页的企业版入口按「是否内置」显示/隐藏。
func TestEnterpriseCTA_Gating(t *testing.T) {
	html := []byte(`<div>` + enterpriseCTAStart + `<a class="btn-enterprise" href="/enterprise/">进入企业版前端 →</a>` + enterpriseCTAEnd + `</div>`)

	out := stripEnterpriseCTA(html)
	if bundleAvailable() {
		if !strings.Contains(string(out), "/enterprise/") {
			t.Fatalf("已内置企业版时入口必须保留；got=%q", out)
		}
		if strings.Contains(string(out), "OPSMESH_ENTERPRISE_CTA") {
			t.Fatalf("包裹标记不应出现在响应中；got=%q", out)
		}
	} else {
		if strings.Contains(string(out), "/enterprise/") {
			t.Fatalf("未内置企业版时必须隐藏入口（避免 404 首屏）；got=%q", out)
		}
	}
	// 标记缺失时原样返回（不破坏老 HTML/自定义页面）。
	noMarker := []byte(`<div><a href="/enterprise/">x</a></div>`)
	if got := string(stripEnterpriseCTA(noMarker)); got != string(noMarker) {
		t.Fatalf("无标记时应原样返回；got=%q", got)
	}
}

// TestEnterpriseUI_DashboardWiring 走真实 HTTP 路由（含 ServeMux 注册）验证接线：
// P0-3 的核心缺陷是「没有路由注册」，故必须有一处断言覆盖 mux 而非仅调 handler。
func TestEnterpriseUI_DashboardWiring(t *testing.T) {
	s := newEnterpriseServer(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleDashboard)
	mux.HandleFunc("/enterprise/", s.handleEnterpriseUI)
	mux.HandleFunc("/enterprise/assets/", s.handleEnterpriseAsset)

	// /enterprise（无尾斜杠）由 ServeMux 自动重定向到 /enterprise/（Go 标准库用 307/308）。
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/enterprise", nil))
	if rec.Code != http.StatusMovedPermanently && rec.Code != http.StatusTemporaryRedirect &&
		rec.Code != http.StatusPermanentRedirect {
		t.Fatalf("GET /enterprise 应重定向到 /enterprise/；got=%d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/enterprise/" {
		t.Fatalf("重定向目标应为 /enterprise/；got=%q", loc)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/enterprise/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /enterprise/ 经 mux 应 200；got=%d", rec.Code)
	}
}
