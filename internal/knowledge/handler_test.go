package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gateway "github.com/Levango7/OpsMesh/internal/ai/gateway"
)

// newTestHandler 用内存存储 + nil gateway 构造 handler：
// 本文件只走不触达 embedding/LLM 的分支（校验失败、纯存储读写、注入同步替身）。
func newTestHandler() (*KnowledgeHandler, *InMemoryKnowledgeStore) {
	store := NewInMemoryKnowledgeStore()
	_ = store.Upsert(context.Background(), []KnowledgeDoc{
		{ID: "d1", Source: SourceCMDB, Title: "web-01", Content: "server", Embedding: []float64{1, 0}},
		{ID: "d2", Source: SourceAlert, Title: "cpu high", Content: "alert", Embedding: []float64{0, 1}},
	})
	h := NewKnowledgeHandler(store, nil)
	// 默认同步函数会调 gateway（nil ⇒ panic），改用替身。
	for _, src := range []KnowledgeSource{SourceCMDB, SourceAlert, SourceRunbook, SourceGit, SourceConfluence} {
		h.syncEng.RegisterSyncFunc(src, func(context.Context, KnowledgeStore, *gateway.Gateway) (int, error) {
			return 2, nil
		})
	}
	return h, store
}

func doRequest(t *testing.T, h http.HandlerFunc, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是 JSON：%v（body=%q）", err, rec.Body.String())
	}
	return out
}

func TestHandleIngest(t *testing.T) {
	h, store := newTestHandler()

	cases := []struct {
		name     string
		method   string
		body     string
		wantCode int
	}{
		{"方法不允许", http.MethodGet, "", http.StatusMethodNotAllowed},
		{"非法 JSON", http.MethodPost, "{", http.StatusBadRequest},
		{"空文档", http.MethodPost, `{"documents":[]}`, http.StatusBadRequest},
		{"正常摄入（已带向量，不调 gateway）", http.MethodPost,
			`{"documents":[{"id":"n1","source":"manual","title":"t","content":"c","embedding":[0.1,0.2]}]}`,
			http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doRequest(t, h.HandleIngest, tc.method, "/api/v1/knowledge/ingest", tc.body)
			if rec.Code != tc.wantCode {
				t.Fatalf("状态码 = %d，期望 %d（body=%s）", rec.Code, tc.wantCode, rec.Body.String())
			}
		})
	}

	if n, _ := store.Count(context.Background()); n != 3 {
		t.Errorf("摄入后文档数 = %d，期望 3", n)
	}
	got, _ := store.Search(context.Background(), "", 0, map[string]interface{}{})
	var ingested *KnowledgeDoc
	for i := range got {
		if got[i].ID == "n1" {
			ingested = &got[i]
		}
	}
	if ingested == nil {
		t.Fatal("摄入的文档未落库")
	}
	if ingested.CreatedAt.IsZero() || ingested.UpdatedAt.IsZero() {
		t.Errorf("摄入应补时间戳：%+v", ingested)
	}
}

func TestHandleList(t *testing.T) {
	h, _ := newTestHandler()

	rec := doRequest(t, h.HandleList, http.MethodGet, "/api/v1/knowledge/docs", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}
	if body := decodeBody(t, rec); body["count"] != float64(2) {
		t.Errorf("全量 count = %v，期望 2", body["count"])
	}

	rec = doRequest(t, h.HandleList, http.MethodGet, "/api/v1/knowledge/docs?source=cmdb", "")
	if body := decodeBody(t, rec); body["count"] != float64(1) {
		t.Errorf("按源过滤 count = %v，期望 1", body["count"])
	}

	if rec := doRequest(t, h.HandleList, http.MethodPost, "/api/v1/knowledge/docs", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("错误方法状态码 = %d，期望 405", rec.Code)
	}
}

func TestHandleDelete(t *testing.T) {
	h, store := newTestHandler()

	rec := doRequest(t, func(w http.ResponseWriter, r *http.Request) {
		h.HandleDelete(w, r, "d1")
	}, http.MethodDelete, "/api/v1/knowledge/docs/d1", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("状态码 = %d，期望 204", rec.Code)
	}
	if n, _ := store.Count(context.Background()); n != 1 {
		t.Errorf("删除后文档数 = %d，期望 1", n)
	}

	rec = doRequest(t, func(w http.ResponseWriter, r *http.Request) {
		h.HandleDelete(w, r, "d2")
	}, http.MethodGet, "/api/v1/knowledge/docs/d2", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("错误方法状态码 = %d，期望 405", rec.Code)
	}
}

func TestHandleStatsAndSyncConfigs(t *testing.T) {
	h, _ := newTestHandler()

	rec := doRequest(t, h.HandleStats, http.MethodGet, "/api/v1/knowledge/stats", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("stats 状态码 = %d", rec.Code)
	}
	body := decodeBody(t, rec)
	if body["totalDocs"] != float64(2) {
		t.Errorf("totalDocs = %v，期望 2", body["totalDocs"])
	}
	cfgs, ok := body["syncConfigs"].([]interface{})
	if !ok || len(cfgs) != 5 {
		t.Errorf("syncConfigs = %v，期望 5 条", body["syncConfigs"])
	}

	rec = doRequest(t, h.HandleSyncConfigs, http.MethodGet, "/api/v1/knowledge/sync-configs", "")
	if rec.Code != http.StatusOK {
		t.Errorf("sync-configs 状态码 = %d", rec.Code)
	}
}

func TestHandleUpdateSyncConfig(t *testing.T) {
	h, _ := newTestHandler()

	rec := doRequest(t, func(w http.ResponseWriter, r *http.Request) {
		h.HandleUpdateSyncConfig(w, r, string(SourceCMDB))
	}, http.MethodPut, "/api/v1/knowledge/sync-configs/cmdb", `{"enabled":false,"interval":"5m"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200（body=%s）", rec.Code, rec.Body.String())
	}
	cfg := findConfig(t, h.syncEng, SourceCMDB)
	if cfg.Enabled || cfg.Interval != 5*time.Minute {
		t.Errorf("配置未更新：%+v", cfg)
	}

	// 非法 interval 必须拒绝，且不能留下"看似生效"的开关改动。
	rec = doRequest(t, func(w http.ResponseWriter, r *http.Request) {
		h.HandleUpdateSyncConfig(w, r, string(SourceAlert))
	}, http.MethodPut, "/api/v1/knowledge/sync-configs/alert", `{"enabled":false,"interval":"bogus"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 interval 状态码 = %d，期望 400", rec.Code)
	}
	if cfg := findConfig(t, h.syncEng, SourceAlert); !cfg.Enabled {
		t.Error("非法 interval 的请求不应改动 enabled")
	}

	if rec := doRequest(t, func(w http.ResponseWriter, r *http.Request) {
		h.HandleUpdateSyncConfig(w, r, string(SourceCMDB))
	}, http.MethodGet, "/api/v1/knowledge/sync-configs/cmdb", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("错误方法状态码 = %d，期望 405", rec.Code)
	}
}

func TestHandleSync(t *testing.T) {
	h, _ := newTestHandler()

	rec := doRequest(t, h.HandleSync, http.MethodPost, "/api/v1/knowledge/sync", `{"source":"cmdb"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("单源同步状态码 = %d（body=%s）", rec.Code, rec.Body.String())
	}
	if body := decodeBody(t, rec); body["count"] != float64(2) {
		t.Errorf("单源同步 count = %v，期望 2", body["count"])
	}

	rec = doRequest(t, h.HandleSync, http.MethodPost, "/api/v1/knowledge/sync", `{"source":"nope"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("未知源状态码 = %d，期望 400", rec.Code)
	}

	rec = doRequest(t, h.HandleSync, http.MethodPost, "/api/v1/knowledge/sync", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("全量同步状态码 = %d", rec.Code)
	}
	if _, ok := decodeBody(t, rec)["results"]; !ok {
		t.Error("全量同步响应应包含 results")
	}

	if rec := doRequest(t, h.HandleSync, http.MethodGet, "/api/v1/knowledge/sync", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("错误方法状态码 = %d，期望 405", rec.Code)
	}
}

func TestHandleSearch_Validation(t *testing.T) {
	h, _ := newTestHandler()

	if rec := doRequest(t, h.HandleSearch, http.MethodGet, "/api/v1/knowledge/search", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("错误方法状态码 = %d，期望 405", rec.Code)
	}
	if rec := doRequest(t, h.HandleSearch, http.MethodPost, "/api/v1/knowledge/search", "{"); rec.Code != http.StatusBadRequest {
		t.Errorf("非法 JSON 状态码 = %d，期望 400", rec.Code)
	}
	if rec := doRequest(t, h.HandleSearch, http.MethodPost, "/api/v1/knowledge/search", `{"query":""}`); rec.Code != http.StatusBadRequest {
		t.Errorf("空 query 状态码 = %d，期望 400", rec.Code)
	}
}

// TestRegisterKnowledgeRoutes 通过真实 mux 走一遍路径切片逻辑
// （DELETE /docs/{id} 与 PUT /sync-configs/{source} 都在闭包里手工裁前缀）。
func TestRegisterKnowledgeRoutes(t *testing.T) {
	h, store := newTestHandler()
	mux := http.NewServeMux()
	RegisterKnowledgeRoutes(mux, h)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/knowledge/stats")
	if err != nil {
		t.Fatalf("GET stats: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET stats 状态码 = %d", resp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/knowledge/sync-configs/git", strings.NewReader(`{"enabled":false}`))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT sync-configs: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("PUT sync-configs 状态码 = %d", resp.StatusCode)
	}
	if cfg := findConfig(t, h.syncEng, SourceGit); cfg.Enabled {
		t.Error("路由未把 source 正确裁给 handler（git 仍启用）")
	}

	req, _ = http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/knowledge/docs/d1", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE docs: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("DELETE docs 状态码 = %d", resp.StatusCode)
	}
	if n, _ := store.Count(context.Background()); n != 1 {
		t.Errorf("路由删除后文档数 = %d，期望 1", n)
	}
}
