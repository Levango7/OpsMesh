// abac_shadow_test.go ABAC 影子模式（批次50）回归。
//
// 钉住的硬约束：
//  1. 默认拒绝策略下请求照常进入 handler（不阻断、状态码/响应体不变）；
//  2. 关闭时零评估（计数器不变）；
//  3. 只观察变更方法；ABAC 管理面、联邦入站、基础端点与 SSE 一律跳过；
//  4. 评估器 panic 被 recover，不影响响应，并记 error 决策；
//  5. 身份缺租户时跳过且不 panic（不把「无身份」误计成 would-deny）。
package controlplane

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/metrics"
)

func newABACShadowTestServer(enabled bool) *Server {
	return &Server{cfg: &config.Config{ABACShadow: enabled}, metrics: metrics.New()}
}

func newABACShadowRequest(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "10.0.0.9:54321"
	req.Header.Set("User-Agent", "opsmesh-shadow-test")
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-User-Id", "alice")
	req.Header.Set("X-User-Roles", "operator")
	return req
}

// shadowResponse 是 next handler 的固定响应；任何 shadow 行为都不得改动它。
const shadowResponse = `{"ok":true}`

func serveABACShadow(t *testing.T, s *Server, req *http.Request) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusAccepted)
		if _, err := w.Write([]byte(shadowResponse)); err != nil {
			t.Fatalf("写响应失败: %v", err)
		}
	})
	rec := httptest.NewRecorder()
	s.abacShadowMiddleware(next).ServeHTTP(rec, req)
	return rec, called
}

func TestABACShadow_DefaultDenyDoesNotBlock(t *testing.T) {
	beforeEval := ABACShadowEvalTotal.Load()
	beforeDeny := ABACShadowWouldDenyTotal.Load()

	var buf bytes.Buffer
	logx.SetOutput(&buf)
	logx.SetLevel(slog.LevelInfo)
	defer func() {
		logx.SetOutput(os.Stderr)
		logx.SetLevel(slog.LevelInfo)
	}()

	// 无任何策略 ⇒ 引擎 default-deny；shadow 必须只记录、不放行/不阻断。
	s := newABACShadowTestServer(true)
	rec, called := serveABACShadow(t, s, newABACShadowRequest(http.MethodPost, "/api/v1/tasks"))

	if !called {
		t.Fatal("shadow 评估不得跳过 handler")
	}
	if rec.Code != http.StatusAccepted {
		t.Fatalf("响应状态被 shadow 改写: got %d, want %d", rec.Code, http.StatusAccepted)
	}
	if rec.Body.String() != shadowResponse {
		t.Fatalf("响应体被 shadow 改写: got %q", rec.Body.String())
	}
	if got := ABACShadowEvalTotal.Load() - beforeEval; got != 1 {
		t.Fatalf("评估计数=%d, want 1", got)
	}
	if got := ABACShadowWouldDenyTotal.Load() - beforeDeny; got != 1 {
		t.Fatalf("would-deny 计数=%d, want 1", got)
	}

	logOut := buf.String()
	for _, want := range []string{
		`"msg":"abac shadow decision"`,
		`"shadow":true`,
		`"decision":"deny"`,
		`"reason":"no matching policy (default deny)"`,
		`"method":"POST"`,
		`"path":"/api/v1/tasks"`,
		`"tenantID":"t1"`,
		`"userID":"alice"`,
		`"durationMs":`,
	} {
		if !strings.Contains(logOut, want) {
			t.Fatalf("shadow 日志缺少 %s；实际输出: %s", want, logOut)
		}
	}
	if !strings.Contains(s.metrics.Render(), `opsmesh_abac_shadow_decisions_total{decision="deny",method="POST"} 1`) {
		t.Fatalf("Prometheus 计数器未记录 deny/POST:\n%s", s.metrics.Render())
	}
}

func TestABACShadow_DisabledSkipsEvaluation(t *testing.T) {
	beforeEval := ABACShadowEvalTotal.Load()
	beforeDeny := ABACShadowWouldDenyTotal.Load()

	s := newABACShadowTestServer(false)
	rec, called := serveABACShadow(t, s, newABACShadowRequest(http.MethodPost, "/api/v1/tasks"))

	if !called || rec.Code != http.StatusAccepted {
		t.Fatalf("关闭时 handler 应照常执行（called=%v code=%d）", called, rec.Code)
	}
	if got := ABACShadowEvalTotal.Load() - beforeEval; got != 0 {
		t.Fatalf("关闭时不得评估，评估计数增量=%d", got)
	}
	if got := ABACShadowWouldDenyTotal.Load() - beforeDeny; got != 0 {
		t.Fatalf("关闭时不得记录 would-deny，增量=%d", got)
	}
	if !strings.Contains(s.metrics.Render(), `opsmesh_abac_shadow_decisions_total{decision="deny",method="POST"} 0`) {
		t.Fatal("关闭时 Prometheus 序列应存在且为 0")
	}
}

func TestABACShadow_SkipsNonMutatingAndExcludedPaths(t *testing.T) {
	beforeEval := ABACShadowEvalTotal.Load()

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/tasks"},
		{http.MethodHead, "/api/v1/tasks"},
		{http.MethodPost, "/api/v1/abac"},
		{http.MethodPost, "/api/v1/abac/evaluate"},
		{http.MethodPost, "/api/v1/federation/sync"},
		{http.MethodPost, "/healthz"},
		{http.MethodPost, "/readyz"},
		{http.MethodPost, "/metrics"},
		{http.MethodPost, "/api/v1/events/stream"},
		{http.MethodPost, "/other"},
	}

	s := newABACShadowTestServer(true)
	for _, tc := range cases {
		rec, called := serveABACShadow(t, s, newABACShadowRequest(tc.method, tc.path))
		if !called || rec.Code != http.StatusAccepted {
			t.Fatalf("%s %s 应照常进入 handler（called=%v code=%d）", tc.method, tc.path, called, rec.Code)
		}
	}
	if got := ABACShadowEvalTotal.Load() - beforeEval; got != 0 {
		t.Fatalf("跳过路径不得评估，评估计数增量=%d", got)
	}
}

func TestABACShadow_EvaluatorPanicDoesNotAffectResponse(t *testing.T) {
	beforeEval := ABACShadowEvalTotal.Load()
	beforePanic := ABACShadowPanicTotal.Load()

	abacShadowEvalHook = func(_ *Server, _, _, _, _, _ string) (bool, string, string) {
		panic("simulated evaluator failure")
	}
	defer func() { abacShadowEvalHook = nil }()

	s := newABACShadowTestServer(true)
	rec, called := serveABACShadow(t, s, newABACShadowRequest(http.MethodPut, "/api/v1/tasks/t1"))

	if !called {
		t.Fatal("评估器 panic 后 handler 仍必须执行")
	}
	if rec.Code != http.StatusAccepted || rec.Body.String() != shadowResponse {
		t.Fatalf("评估器 panic 改写了响应: code=%d body=%q", rec.Code, rec.Body.String())
	}
	if got := ABACShadowEvalTotal.Load() - beforeEval; got != 0 {
		t.Fatalf("panic 不应计入成功评估，增量=%d", got)
	}
	if got := ABACShadowPanicTotal.Load() - beforePanic; got != 1 {
		t.Fatalf("panic 计数=%d, want 1", got)
	}
	if !strings.Contains(s.metrics.Render(), `opsmesh_abac_shadow_decisions_total{decision="error",method="PUT"} 1`) {
		t.Fatalf("panic 应记 error 决策:\n%s", s.metrics.Render())
	}
}

func TestABACShadow_NoTenantSkipsWithoutPanic(t *testing.T) {
	beforeEval := ABACShadowEvalTotal.Load()
	beforeDeny := ABACShadowWouldDenyTotal.Load()

	s := newABACShadowTestServer(true)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", nil)
	rec, called := serveABACShadow(t, s, req)

	if !called || rec.Code != http.StatusAccepted {
		t.Fatalf("缺租户时 handler 应照常执行（called=%v code=%d）", called, rec.Code)
	}
	if got := ABACShadowEvalTotal.Load() - beforeEval; got != 0 {
		t.Fatalf("缺租户应跳过评估，增量=%d", got)
	}
	if got := ABACShadowWouldDenyTotal.Load() - beforeDeny; got != 0 {
		t.Fatalf("缺租户不得计入 would-deny，增量=%d", got)
	}
}
