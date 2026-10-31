package enterpriseui

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 源码树里只能有占位页：真实构建产物由 Docker/CI 的 node 阶段覆盖进来。
// 这里若变 true，说明有人把前端产物误提交进仓。
func TestBundlePlaceholderInSourceTree(t *testing.T) {
	if BundleAvailable() {
		t.Fatal("源码树中不应内置企业版产物（index.html 不应存在或不应缺少占位标记）")
	}
	data, err := ReadFile(Placeholder)
	if err != nil {
		t.Fatalf("读占位页: %v", err)
	}
	if !bytes.Contains(data, []byte(enterpriseBundleMarkText)) {
		t.Fatal("占位页缺少占位标记，BundleAvailable 判定会失真")
	}
}

// 嵌入文件读取必须拒绝路径穿越，防止 /enterprise/../ 之类的请求读到仓内其他文件。
func TestReadFileRejectsTraversal(t *testing.T) {
	for _, p := range []string{"../go.mod", "/etc/passwd", "enterprise/../../go.mod", ".."} {
		if _, err := ReadFile(p); err == nil {
			t.Fatalf("ReadFile(%q) 应拒绝", p)
		}
	}
	if _, err := ReadFile(Placeholder); err != nil {
		t.Fatalf("合法路径读取失败: %v", err)
	}
}

func TestNegotiatedEncoding(t *testing.T) {
	mk := func(ae string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/enterprise/", nil)
		if ae != "" {
			r.Header.Set("Accept-Encoding", ae)
		}
		return r
	}
	cases := []struct {
		ae, rel, enc, suffix string
		ok                   bool
	}{
		{"gzip, br", "enterprise/assets/a.js", "br", "br", true},
		{"gzip", "enterprise/assets/a.js", "gzip", "gz", true},
		{"", "enterprise/assets/a.js", "", "", false},
		{"deflate", "enterprise/assets/a.js", "", "", false},
		// 已经是预压缩文件本身，不再套一层。
		{"br", "enterprise/assets/a.js.br", "", "", false},
		{"gzip", "enterprise/assets/a.js.gz", "", "", false},
	}
	for _, tc := range cases {
		enc, suffix, ok := NegotiatedEncoding(mk(tc.ae), tc.rel)
		if enc != tc.enc || suffix != tc.suffix || ok != tc.ok {
			t.Fatalf("NegotiatedEncoding(%q,%q) = (%q,%q,%v), want (%q,%q,%v)",
				tc.ae, tc.rel, enc, suffix, ok, tc.enc, tc.suffix, tc.ok)
		}
	}
}

func TestContentType(t *testing.T) {
	cases := map[string]string{
		"a.js":             "application/javascript; charset=utf-8",
		"a.mjs":            "application/javascript; charset=utf-8",
		"a.css":            "text/css; charset=utf-8",
		"a.html":           "text/html; charset=utf-8",
		"a.json":           "application/json; charset=utf-8",
		"a.svg":            "image/svg+xml",
		"a.png":            "image/png",
		"a.woff2":          "font/woff2",
		"a.txt":            "text/plain; charset=utf-8",
		"A.JS":             "application/javascript; charset=utf-8",
		"a.unknown-suffix": "application/octet-stream",
	}
	for rel, want := range cases {
		if got := ContentType(rel); got != want {
			t.Fatalf("ContentType(%q) = %q, want %q", rel, got, want)
		}
	}
}

func TestLooksLikeFile(t *testing.T) {
	cases := map[string]bool{
		"index.html":          true,
		"enterprise/a.js":     true,
		"enterprise/assets/":  false,
		"":                    false,
		"a.":                  false,
		".hidden":             false,
		"enterprise/sub/path": false,
	}
	for rel, want := range cases {
		if got := LooksLikeFile(rel); got != want {
			t.Fatalf("LooksLikeFile(%q) = %v, want %v", rel, got, want)
		}
	}
}

// HEAD 只发头不发体，且 Content-Length 仍是实体长度（爬虫/预检依赖）。
func TestWriteBodyHead(t *testing.T) {
	body := []byte("hello")

	w := httptest.NewRecorder()
	WriteBody(w, httptest.NewRequest(http.MethodHead, "/enterprise/", nil), body)
	if w.Body.Len() != 0 {
		t.Fatalf("HEAD 响应不应带体: %q", w.Body.String())
	}
	if got := w.Header().Get("Content-Length"); got != "5" {
		t.Fatalf("HEAD Content-Length = %q, want 5", got)
	}

	w = httptest.NewRecorder()
	WriteBody(w, httptest.NewRequest(http.MethodGet, "/enterprise/", nil), body)
	if w.Body.String() != "hello" {
		t.Fatalf("GET 响应体 = %q", w.Body.String())
	}
}

func TestServeStaticCachePolicy(t *testing.T) {
	// 带内容哈希的产物可长缓存。
	w := httptest.NewRecorder()
	ServeStatic(w, httptest.NewRequest(http.MethodGet, "/enterprise/assets/a.js", nil), AssetsPrefix+"a.js", []byte("js"))
	if got := w.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("assets 缓存策略 = %q, want immutable", got)
	}
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/javascript") {
		t.Fatalf("assets Content-Type = %q", got)
	}

	// 入口文件必须每次校验，避免升级后加载旧外壳。
	w = httptest.NewRecorder()
	ServeStatic(w, httptest.NewRequest(http.MethodGet, "/enterprise/", nil), IndexPath, []byte("html"))
	if got := w.Header().Get("Cache-Control"); !strings.Contains(got, "no-cache") {
		t.Fatalf("入口缓存策略 = %q, want no-cache", got)
	}
	if w.Body.String() != "html" {
		t.Fatalf("未命中预压缩时响应体 = %q", w.Body.String())
	}
}

// 未内置企业版时，引导页里的入口整段移除；内置时只去标记留内容。
// 源码树里 BundleAvailable() 恒为 false，故这里钉「移除入口」与原样返回两条路径。
func TestStripCTA(t *testing.T) {
	in := []byte("<div>a" + CTAStart + "<a>enter</a>" + CTAEnd + "b</div>")
	out := string(StripCTA(in))
	if strings.Contains(out, "enter") || strings.Contains(out, CTAStart) || strings.Contains(out, CTAEnd) {
		t.Fatalf("未内置时应整段移除入口与标记: %q", out)
	}
	if out != "<div>ab</div>" {
		t.Fatalf("StripCTA = %q, want <div>ab</div>", out)
	}

	// 标记缺失/未闭合：原样返回，不做破坏性替换。
	for _, in := range [][]byte{[]byte("<div>plain</div>"), []byte("a" + CTAStart + "b")} {
		if got := StripCTA(in); !bytes.Equal(got, in) {
			t.Fatalf("StripCTA(%q) = %q, want 原样", in, got)
		}
	}
}
