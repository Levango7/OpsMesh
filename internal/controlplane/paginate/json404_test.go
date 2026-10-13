package paginate

// json404_test.go — B3 回归守卫（2026-10-12 模拟用户演练）。
//
// 症状：客户端看到**两种 404 形态**——未注册路径回 JSON 信封，而"命中 catch-all 的缺页"
// （SPA/静态处理器注册在 "/" 上，http.NotFound/FileServer 写纯文本）回 `404 page not found`。
// 新人据此判断"是不是另一个服务在答"，运维也难区分"拼错路径"与"服务没部署"。
//
// 两条判据：
//  1. 命中 catch-all 的**纯文本** 404 必须被改写为 JSON 信封（含实际路径，便于自查拼写）；
//  2. handler **有意**写的 JSON 404 必须原样放行（修复不得篡改语义）。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSONErrorMuxRewritesPlainText404FromCatchAll(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r) // Go 默认：text/plain "404 page not found"
	})
	h := &JSONErrorMux{Inner: mux}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/device?x=1", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态应保持 404，实得 %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type 应被改写为 JSON，实得 %q（body=%q）", ct, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "404 page not found") {
		t.Errorf("纯文本 404 未被改写：%q", rec.Body.String())
	}
	var env map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body 不是 JSON 信封: %v / %q", err, rec.Body.String())
	}
	if env["error"] == "" || env["path"] != "/api/v1/device" {
		t.Errorf("信封应含 error 与实际路径（便于自查拼写）：%v", env)
	}
}

func TestJSONErrorMuxKeepsHandlerJSON404(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tasks/", func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusNotFound, map[string]string{"error": "task not found", "taskID": "t-9"})
	})
	h := &JSONErrorMux{Inner: mux}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/tasks/t-9", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态应 404，实得 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "task not found") || !strings.Contains(rec.Body.String(), "t-9") {
		t.Errorf("handler 有意写的 JSON 404 必须原样放行（不得被包装器覆盖）：%q", rec.Body.String())
	}
}

// TestJSONErrorMuxHeaderOnly404StillJson 只写 404 头、不写 body 的形态也要收口成 JSON。
func TestJSONErrorMuxHeaderOnly404StillJson(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound) // 无 body
	})
	h := &JSONErrorMux{Inner: mux}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态应 404，实得 %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Errorf("裸 404 头也应收口为 JSON：Content-Type=%q body=%q", rec.Header().Get("Content-Type"), rec.Body.String())
	}
}
