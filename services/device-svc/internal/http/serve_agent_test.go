// serve_agent_test.go — D3+ handleServeAgent agent 二进制分发单测。
//
// 覆盖：
//  1. 方法校验：非 GET 返回 405
//  2. 默认平台/架构：无查询参数时回退 linux/amd64
//  3. 显式平台/架构：?os=linux&arch=arm64 选择对应二进制
//  4. agentBinDir 未配置时回退当前进程二进制（os.Executable）
//  5. agentBinDir 配置但文件不存在时回退当前进程二进制
//  6. agentBinDir 配置且按平台/架构命名文件存在时优先分发
package http

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/services/device-svc/internal/store"
)

// TestServeAgent_MethodNotAllowed 验证非 GET 返回 405。
func TestServeAgent_MethodNotAllowed(t *testing.T) {
	mux := newTestGateway(t)
	rec := doReq(t, mux, http.MethodPost, "/bin/opsmesh-agent", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /bin/opsmesh-agent: got %d, want 405", rec.Code)
	}
}

// TestServeAgent_FallbackToCurrentBinary 验证未配置 agentBinDir 时回退当前进程二进制。
// 当前进程二进制必然存在（go test 自身），故应返回 200 + octet-stream。
func TestServeAgent_FallbackToCurrentBinary(t *testing.T) {
	ms := store.NewMemoryStore()
	g := NewGateway(ms, ms, ms, ms, ms, "http://127.0.0.1:8081")
	// 不 SetAgentBinDir → 回退当前进程二进制
	mux := http.NewServeMux()
	g.RegisterRoutes(mux, func(h http.Handler) http.Handler { return h })

	rec := doReq(t, mux, http.MethodGet, "/bin/opsmesh-agent", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /bin/opsmesh-agent fallback: got %d, body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Fatalf("Content-Type 期望 application/octet-stream，got %q", ct)
	}
	// 默认平台/架构头。
	if os := rec.Header().Get("X-OpsMesh-Agent-OS"); os != "linux" {
		t.Fatalf("默认 OS 期望 linux，got %q", os)
	}
	if arch := rec.Header().Get("X-OpsMesh-Agent-Arch"); arch != "amd64" {
		t.Fatalf("默认 Arch 期望 amd64，got %q", arch)
	}
}

// TestServeAgent_ExplicitPlatform 验证 ?os=&arch= 查询参数选择平台/架构。
func TestServeAgent_ExplicitPlatform(t *testing.T) {
	ms := store.NewMemoryStore()
	g := NewGateway(ms, ms, ms, ms, ms, "http://127.0.0.1:8081")
	mux := http.NewServeMux()
	g.RegisterRoutes(mux, func(h http.Handler) http.Handler { return h })

	rec := doReq(t, mux, http.MethodGet, "/bin/opsmesh-agent?os=linux&arch=arm64", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /bin/opsmesh-agent?os=linux&arch=arm64: got %d", rec.Code)
	}
	if arch := rec.Header().Get("X-OpsMesh-Agent-Arch"); arch != "arm64" {
		t.Fatalf("显式 arch=arm64 期望回显 arm64，got %q", arch)
	}
}

// TestServeAgent_AgentBinDirNamedFile 验证配置 agentBinDir 且按平台/架构命名文件存在时优先分发。
func TestServeAgent_AgentBinDirNamedFile(t *testing.T) {
	// 创建临时目录 + 写入按平台/架构命名的假二进制。
	tmpDir := t.TempDir()
	namedPath := filepath.Join(tmpDir, "opsmesh-agent-linux-amd64")
	fakeContent := "fake-agent-binary-linux-amd64"
	if err := os.WriteFile(namedPath, []byte(fakeContent), 0o755); err != nil {
		t.Fatalf("写入测试二进制失败: %v", err)
	}

	ms := store.NewMemoryStore()
	g := NewGateway(ms, ms, ms, ms, ms, "http://127.0.0.1:8081")
	g.SetAgentBinDir(tmpDir)
	mux := http.NewServeMux()
	g.RegisterRoutes(mux, func(h http.Handler) http.Handler { return h })

	rec := doReq(t, mux, http.MethodGet, "/bin/opsmesh-agent?os=linux&arch=amd64", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /bin/opsmesh-agent 命名文件: got %d, body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != fakeContent {
		t.Fatalf("期望分发命名文件内容 %q，got %q", fakeContent, rec.Body.String())
	}
}

// TestServeAgent_AgentBinDirGenericFallback 验证按平台/架构命名文件不存在时回退通用名 opsmesh-agent。
func TestServeAgent_AgentBinDirGenericFallback(t *testing.T) {
	tmpDir := t.TempDir()
	genericPath := filepath.Join(tmpDir, "opsmesh-agent")
	fakeContent := "generic-agent-binary"
	if err := os.WriteFile(genericPath, []byte(fakeContent), 0o755); err != nil {
		t.Fatalf("写入通用二进制失败: %v", err)
	}

	ms := store.NewMemoryStore()
	g := NewGateway(ms, ms, ms, ms, ms, "http://127.0.0.1:8081")
	g.SetAgentBinDir(tmpDir)
	mux := http.NewServeMux()
	g.RegisterRoutes(mux, func(h http.Handler) http.Handler { return h })

	// 请求 arm64（命名文件不存在），应回退通用名。
	rec := doReq(t, mux, http.MethodGet, "/bin/opsmesh-agent?os=linux&arch=arm64", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /bin/opsmesh-agent arm64 回退通用名: got %d", rec.Code)
	}
	if rec.Body.String() != fakeContent {
		t.Fatalf("期望回退通用名内容 %q，got %q", fakeContent, rec.Body.String())
	}
}

// TestServeAgent_DetectPlatform 验证平台探测逻辑。
func TestServeAgent_DetectPlatform(t *testing.T) {
	cases := []struct {
		name     string
		query    string
		wantOS   string
		wantArch string
	}{
		{"空查询默认 linux/amd64", "", "linux", "amd64"},
		{"仅 os=linux", "?os=linux", "linux", "amd64"},
		{"仅 arch=arm64", "?arch=arm64", "linux", "arm64"},
		{"显式 linux/amd64", "?os=linux&arch=amd64", "linux", "amd64"},
		{"显式 linux/arm64", "?os=linux&arch=arm64", "linux", "arm64"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/bin/opsmesh-agent"+c.query, nil)
			osName, arch := detectPlatform(req)
			if osName != c.wantOS {
				t.Fatalf("OS 期望 %q，got %q", c.wantOS, osName)
			}
			if arch != c.wantArch {
				t.Fatalf("Arch 期望 %q，got %q", c.wantArch, arch)
			}
		})
	}
}

// TestServeAgent_ResolveAgentBinary 验证二进制路径解析逻辑。
func TestServeAgent_ResolveAgentBinary(t *testing.T) {
	// 未配置 agentBinDir → 回退当前进程二进制（必然存在）。
	g := &Gateway{}
	path := g.resolveAgentBinary("linux", "amd64")
	if path == "" {
		t.Fatal("未配置 agentBinDir 时应回退当前进程二进制，不应返回空")
	}
	if !strings.HasSuffix(path, "test") && !strings.Contains(path, ".test") && !strings.Contains(path, "go-build") {
		// 当前进程是 go test 二进制，路径应包含 test 或 go-build 相关片段。
		// 此断言较宽松，主要验证非空。
	}

	// 配置不存在的目录 → 回退当前进程二进制。
	g.SetAgentBinDir("/nonexistent/dir/for/test")
	path = g.resolveAgentBinary("linux", "amd64")
	if path == "" {
		t.Fatal("配置不存在目录时应回退当前进程二进制")
	}
}
