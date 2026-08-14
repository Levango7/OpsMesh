package logstore

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestESBuildDSL 验证 OpsMesh Query → ES DSL 翻译。
// 校验 must 子句、filter range、from/size、sort。
func TestESBuildDSL(t *testing.T) {
	s := NewESStore("http://es:9200", "opsmesh-logs")

	from := time.Now().Add(-1 * time.Hour)
	to := time.Now()
	q := Query{
		TenantID: "t1",
		AgentID:  "a1",
		Level:    "error",
		Keyword:  "boom",
		From:     from,
		To:       to,
		Limit:    50,
		Offset:   10,
	}
	b := s.buildDSL(q, 50)

	var dsl map[string]interface{}
	if err := json.Unmarshal(b, &dsl); err != nil {
		t.Fatalf("dsl unmarshal: %v", err)
	}

	// from/size。
	if from, _ := dsl["from"].(float64); int(from) != 10 {
		t.Errorf("from want 10, got %v", dsl["from"])
	}
	if size, _ := dsl["size"].(float64); int(size) != 50 {
		t.Errorf("size want 50, got %v", dsl["size"])
	}

	// sort。
	sort, _ := dsl["sort"].([]interface{})
	if len(sort) != 1 {
		t.Fatalf("sort want 1 entry, got %d", len(sort))
	}

	// query.bool.must 应含 4 个子句（tenant/agent/level/keyword）。
	boolQ, _ := dsl["query"].(map[string]interface{})["bool"].(map[string]interface{})
	must, _ := boolQ["must"].([]interface{})
	if len(must) != 4 {
		t.Fatalf("must want 4 clauses, got %d", len(must))
	}

	// filter 应含 1 个 range 子句。
	filter, _ := boolQ["filter"].([]interface{})
	if len(filter) != 1 {
		t.Fatalf("filter want 1 range clause, got %d", len(filter))
	}
}

// TestESBuildDSLMinimal 验证空查询的 DSL 仍合法（bool 可空）。
func TestESBuildDSLMinimal(t *testing.T) {
	s := NewESStore("http://es:9200", "opsmesh-logs")
	b := s.buildDSL(Query{}, 200)

	var dsl map[string]interface{}
	if err := json.Unmarshal(b, &dsl); err != nil {
		t.Fatalf("dsl unmarshal: %v", err)
	}
	// size 默认 200。
	if size, _ := dsl["size"].(float64); int(size) != 200 {
		t.Errorf("size want 200, got %v", dsl["size"])
	}
}

// TestESQueryAndParse 验证 Query 调用 ES _search 并解析 hits。
// 用 httptest.NewServer 模拟 ES API，返回 2 条 hits（最新在前）。
func TestESQueryAndParse(t *testing.T) {
	tsNewer := time.Now()
	tsOlder := time.Now().Add(-1 * time.Minute)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 校验路径与方法。
		if !strings.HasSuffix(r.URL.Path, "/opsmesh-logs/_search") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("want POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type want application/json, got %s", ct)
		}

		// 返回 2 条 hits（最新在前）。
		resp := map[string]interface{}{
			"hits": map[string]interface{}{
				"total": map[string]interface{}{"value": 2},
				"hits": []map[string]interface{}{
					{
						"_id": "abc2",
						"_source": map[string]interface{}{
							"tenant_id": "t1", "agent_id": "a1", "level": "error",
							"source": "task", "message": "boom here",
							"timestamp": tsNewer.Format(time.RFC3339Nano),
						},
					},
					{
						"_id": "abc1",
						"_source": map[string]interface{}{
							"tenant_id": "t1", "agent_id": "a1", "level": "error",
							"source": "task", "message": "earlier boom",
							"timestamp": tsOlder.Format(time.RFC3339Nano),
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := NewESStore(srv.URL, "opsmesh-logs")
	out, err := s.Query(context.Background(), Query{TenantID: "t1", Keyword: "boom"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2, got %d", len(out))
	}
	// 最新在前。
	if out[0].Message != "boom here" {
		t.Fatalf("want newest first, got %q", out[0].Message)
	}
	// 字段解析。
	if out[0].TenantID != "t1" || out[0].AgentID != "a1" || out[0].Level != "error" {
		t.Fatalf("字段解析失败: %#v", out[0])
	}
	// 时间戳解析。
	if !out[0].Timestamp.Equal(tsNewer) {
		t.Fatalf("时间戳解析失败: got=%v want=%v", out[0].Timestamp, tsNewer)
	}
}

// TestESQueryError 验证 ES 返回非 200 时 Query 报错。
func TestESQueryError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"es cluster unavailable"}`))
	}))
	defer srv.Close()

	s := NewESStore(srv.URL, "opsmesh-logs")
	_, err := s.Query(context.Background(), Query{TenantID: "t1"})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "status=503") {
		t.Fatalf("error 应含 status=503: %v", err)
	}
}

// TestESQueryEmptyHits 验证空结果不 panic、返回空切片。
func TestESQueryEmptyHits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"hits": map[string]interface{}{
				"total": map[string]interface{}{"value": 0},
				"hits":  []map[string]interface{}{},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := NewESStore(srv.URL, "opsmesh-logs")
	out, err := s.Query(context.Background(), Query{TenantID: "t1"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("want 0, got %d", len(out))
	}
}

// TestESAppendNoop 验证 Append 为 noop。
func TestESAppendNoop(t *testing.T) {
	s := NewESStore("http://es:9200", "opsmesh-logs")
	if err := s.Append(context.Background(), &Entry{Message: "x"}); err != nil {
		t.Fatalf("append noop should not error: %v", err)
	}
	if err := s.Append(context.Background(), nil); err != nil {
		t.Fatalf("append nil should not error: %v", err)
	}
}

// TestESClose 验证 Close 不报错。
func TestESClose(t *testing.T) {
	s := NewESStore("http://es:9200", "opsmesh-logs")
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// TestESLimitClamped 验证 limit 受 maxQueryLimit 约束。
func TestESLimitClamped(t *testing.T) {
	var capturedSize int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var dsl map[string]interface{}
		json.NewDecoder(r.Body).Decode(&dsl)
		if sz, ok := dsl["size"].(float64); ok {
			capturedSize = int(sz)
		}
		resp := map[string]interface{}{
			"hits": map[string]interface{}{
				"total": map[string]interface{}{"value": 0},
				"hits":  []map[string]interface{}{},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := NewESStore(srv.URL, "opsmesh-logs")
	_, _ = s.Query(context.Background(), Query{TenantID: "t1", Limit: 999999})
	if capturedSize != 1000 {
		t.Fatalf("size 应被截断为 1000, got %d", capturedSize)
	}
}

// TestESOffsetFrom 验证 offset 映射到 ES from 字段（ES 原生分页）。
func TestESOffsetFrom(t *testing.T) {
	var capturedFrom int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var dsl map[string]interface{}
		json.NewDecoder(r.Body).Decode(&dsl)
		if f, ok := dsl["from"].(float64); ok {
			capturedFrom = int(f)
		}
		resp := map[string]interface{}{
			"hits": map[string]interface{}{
				"total": map[string]interface{}{"value": 0},
				"hits":  []map[string]interface{}{},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := NewESStore(srv.URL, "opsmesh-logs")
	_, _ = s.Query(context.Background(), Query{TenantID: "t1", Limit: 20, Offset: 40})
	if capturedFrom != 40 {
		t.Fatalf("from want 40, got %d", capturedFrom)
	}
}
