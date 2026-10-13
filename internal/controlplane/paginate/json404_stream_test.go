package paginate

// json404_stream_test.go — B3 包装器的**流式响应腿**守卫（2026-10-13 补）。
//
// 为什么必须有这一条腿：B3（56ac092）的三条守卫只覆盖 404 改写语义，全部直连
// ResponseWriter 形态的用例；而 json404Writer 嵌的是 http.ResponseWriter **接口**，
// 不为流式响应转发 Flush() 时，下游 `w.(http.Flusher)` 断言失败——sse.go:124 的
// SSE handler 正是这么判的，于是在 wrapper 下返回 500 "streaming unsupported"。
// 该交互只有"穿过 mux"才暴露（既有 SSE 用例全部直调 handler 或裸 HandlerFunc），
// 由 E2E real-backend 腿首次抓到（run 38348565851：Expected value: 500）。
// 本文件把那条缺失的腿钉在 paginate 包内：包装器下的流式 handler 必须拿到
// Flusher 并原样流出（200 + text/event-stream + 帧），且 404 改写语义不受影响。

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestJSONErrorMux_StreamingHandlerKeepsFlusher 包装器下的 SSE 形态 handler：
// ① 必须能断言 http.Flusher（复现 sse.go:124 的形态，断言失败即本测试失败）；
// ② 200 + text/event-stream 原样透出，帧内容不被改写；
// ③ Flush 真的可调（透传不 panic）。
func TestJSONErrorMux_StreamingHandlerKeepsFlusher(t *testing.T) {
	inner := http.NewServeMux()
	inner.HandleFunc("/api/v1/events/stream", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			// 正是 sse.go:124 断言失败的形态——B3 上线后 SSE 500 的根因。
			t.Errorf("包装器下丢失 http.Flusher：流式 handler 会被降级成 500")
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "event: hello\ndata: {}\n\n")
		flusher.Flush() // 透传调用不得 panic
	})
	mux := &JSONErrorMux{Inner: inner}

	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/events/stream")
	if err != nil {
		t.Fatalf("GET stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("流式响应 status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(strings.ToLower(ct), "text/event-stream") {
		t.Fatalf("Content-Type = %q, 应含 text/event-stream", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "event: hello") {
		t.Fatalf("hello 帧应原样透出，实得 %q", body)
	}
}

// TestJSONErrorMux_FlushAdditionKeeps404Rewrite 反向不回归：加 Flush/Unwrap 不得
// 削弱 B3 的 404 改写（catch-all 纯文本 404 仍须收口成 JSON）。
func TestJSONErrorMux_FlushAdditionKeeps404Rewrite(t *testing.T) {
	inner := http.NewServeMux()
	inner.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r) // 纯文本 404（B3 的改写对象）
	})
	mux := &JSONErrorMux{Inner: inner}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/no-such-page", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("404 状态被改坏：%d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("404 应仍收口为 JSON，实得 %q", ct)
	}
	if !strings.Contains(rec.Body.String(), `"path":"/no-such-page"`) {
		t.Fatalf("404 JSON 信封应带路径，实得 %q", rec.Body.String())
	}
}
