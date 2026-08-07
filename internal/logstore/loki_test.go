package logstore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestLokiBuildLogQL 验证 OpsMesh Query → LogQL 翻译。
// 标签顺序固定（app 在前），keyword 翻译为 |= "kw" 字面子串管道（向后兼容，避免正则元字符改变语义）。
func TestLokiBuildLogQL(t *testing.T) {
	s := NewLokiStore("http://loki:3100")

	// 全条件。
	q := Query{TenantID: "t1", DeviceID: "d1", AgentID: "a1", Level: "error", Source: "task", Keyword: "boom"}
	got := s.buildLogQL(q)
	want := `{app="opsmesh", tenant_id="t1", device_id="d1", agent_id="a1", level="error", source="task"} |= "boom"`
	if got != want {
		t.Fatalf("LogQL 翻译失败:\n got=%q\nwant=%q", got, want)
	}

	// 仅租户（无 keyword）。
	got2 := s.buildLogQL(Query{TenantID: "t1"})
	want2 := `{app="opsmesh", tenant_id="t1"}`
	if got2 != want2 {
		t.Fatalf("无 keyword 翻译失败: got=%q want=%q", got2, want2)
	}

	// 空查询（仅 app 标签）。
	got3 := s.buildLogQL(Query{})
	want3 := `{app="opsmesh"}`
	if got3 != want3 {
		t.Fatalf("空查询翻译失败: got=%q want=%q", got3, want3)
	}
}

// TestLokiParseLine 验证 Loki 日志行解析：JSON {"message":"..."} 提取 message，纯文本原样返回。
func TestLokiParseLine(t *testing.T) {
	if got := parseLokiLine(`{"message":"boom here"}`); got != "boom here" {
		t.Fatalf("JSON 行解析失败: got=%q", got)
	}
	if got := parseLokiLine("plain text line"); got != "plain text line" {
		t.Fatalf("纯文本行应原样返回: got=%q", got)
	}
	// 非 message 字段的 JSON：原样返回（不抽取其他字段）。
	raw := `{"level":"info","ts":"2024-01-01"}`
	if got := parseLokiLine(raw); got != raw {
		t.Fatalf("无 message 字段应原样返回: got=%q", got)
	}
}

// TestLokiQueryAndParse 验证 Query 调用 Loki query_range 并解析 stream 结果。
// 用 httptest.NewServer 模拟 Loki API，返回 2 条日志（backward：最新在前）。
func TestLokiQueryAndParse(t *testing.T) {
	tsNewer := time.Now().Add(-1 * time.Minute)
	tsOlder := time.Now().Add(-5 * time.Minute)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 校验路径。
		if !strings.HasPrefix(r.URL.Path, "/loki/api/v1/query_range") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		// 校验 query 参数。
		query := r.URL.Query().Get("query")
		if !strings.Contains(query, `tenant_id="t1"`) {
			t.Errorf("query 缺 tenant_id 标签: %s", query)
		}
		if !strings.Contains(query, `|= "boom"`) {
			t.Errorf("query 缺 keyword 字面子串管道: %s", query)
		}
		if dir := r.URL.Query().Get("direction"); dir != "backward" {
			t.Errorf("direction want backward, got %q", dir)
		}

		// 返回 1 个 stream，2 条日志（backward：最新在前）。
		resp := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"resultType": "streams",
				"result": []map[string]interface{}{
					{
						"stream": map[string]string{
							"app": "opsmesh", "tenant_id": "t1", "agent_id": "a1",
							"level": "error", "source": "task",
						},
						"values": [][]string{
							{strconv.FormatInt(tsNewer.UnixNano(), 10), `{"message":"boom here"}`},
							{strconv.FormatInt(tsOlder.UnixNano(), 10), `{"message":"earlier boom"}`},
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := NewLokiStore(srv.URL)
	out, err := s.Query(context.Background(), Query{TenantID: "t1", Keyword: "boom", Limit: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2 entries, got %d", len(out))
	}
	// 最新在前。
	if out[0].Message != "boom here" {
		t.Fatalf("want newest first, got %q", out[0].Message)
	}
	// 标签解析。
	if out[0].TenantID != "t1" || out[0].AgentID != "a1" || out[0].Level != "error" || out[0].Source != "task" {
		t.Fatalf("标签解析失败: %#v", out[0])
	}
	// 时间戳解析。
	if !out[0].Timestamp.Equal(tsNewer) {
		t.Fatalf("时间戳解析失败: got=%v want=%v", out[0].Timestamp, tsNewer)
	}
}

// TestLokiQueryError 验证 Loki 返回非 200 时 Query 报错。
func TestLokiQueryError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"status":"error","error":"loki storage down"}`))
	}))
	defer srv.Close()

	s := NewLokiStore(srv.URL)
	_, err := s.Query(context.Background(), Query{TenantID: "t1"})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "status=500") {
		t.Fatalf("error 应含 status=500: %v", err)
	}
}

// TestLokiQueryNonSuccessStatus 验证 Loki 返回 status!="success" 时报错。
func TestLokiQueryNonSuccessStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status": "error",
			"error":  "query parse error",
		})
	}))
	defer srv.Close()

	s := NewLokiStore(srv.URL)
	_, err := s.Query(context.Background(), Query{TenantID: "t1"})
	if err == nil {
		t.Fatal("want error, got nil")
	}
}

// TestLokiAppendNoop 验证 Append 为 noop（不报错、不实际写入）。
func TestLokiAppendNoop(t *testing.T) {
	s := NewLokiStore("http://loki:3100")
	if err := s.Append(context.Background(), &Entry{Message: "x"}); err != nil {
		t.Fatalf("append noop should not error: %v", err)
	}
	// nil entry 也应安全。
	if err := s.Append(context.Background(), nil); err != nil {
		t.Fatalf("append nil should not error: %v", err)
	}
}

// TestLokiClose 验证 Close 不报错。
func TestLokiClose(t *testing.T) {
	s := NewLokiStore("http://loki:3100")
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// TestLokiOffsetPagination 验证 offset 分页（客户端切片）。
// 模拟返回 5 条（backward：m5,m4,m3,m2,m1），offset=2 limit=2 → 切片后 m3,m2。
func TestLokiOffsetPagination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Loki limit 参数 = limit+offset = 2+2 = 4。
		if l := r.URL.Query().Get("limit"); l != "4" {
			t.Errorf("limit want 4 (limit+offset), got %s", l)
		}
		// 返回 5 条（模拟 Loki 实际会按 limit=4 截断，但客户端逻辑能处理任意数量）。
		var values [][]string
		for i := 5; i >= 1; i-- { // backward：最新在前
			ts := time.Now().Add(time.Duration(i) * time.Minute)
			values = append(values, []string{
				strconv.FormatInt(ts.UnixNano(), 10),
				fmt.Sprintf(`{"message":"m%d"}`, i),
			})
		}
		resp := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"resultType": "streams",
				"result": []map[string]interface{}{
					{
						"stream": map[string]string{"app": "opsmesh", "tenant_id": "t1"},
						"values": values,
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := NewLokiStore(srv.URL)
	out, err := s.Query(context.Background(), Query{TenantID: "t1", Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2 entries after offset, got %d", len(out))
	}
	// 原始顺序 m5,m4,m3,m2,m1；offset=2 切片后 m3,m2。
	if out[0].Message != "m3" || out[1].Message != "m2" {
		t.Fatalf("offset 切片失败: %#v", out)
	}
}

// TestLokiOffsetOutOfBounds 验证 offset 越界返回空结果不 panic。
func TestLokiOffsetOutOfBounds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"resultType": "streams",
				"result":     []map[string]interface{}{},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := NewLokiStore(srv.URL)
	out, err := s.Query(context.Background(), Query{TenantID: "t1", Limit: 10, Offset: 100})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("offset 越界应返回空, got %d", len(out))
	}
}

// TestLokiLimitClamped 验证 limit 受 maxQueryLimit 约束。
func TestLokiLimitClamped(t *testing.T) {
	var capturedLimit string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedLimit = r.URL.Query().Get("limit")
		resp := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"resultType": "streams",
				"result":     []map[string]interface{}{},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := NewLokiStore(srv.URL)
	_, _ = s.Query(context.Background(), Query{TenantID: "t1", Limit: 999999})
	// limit 999999 应被截断为 maxQueryLimit=1000，offset=0，所以 Loki limit=1000。
	if capturedLimit != "1000" {
		t.Fatalf("limit 应被截断为 1000, got %s", capturedLimit)
	}
}
