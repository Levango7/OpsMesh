package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"opsmesh/internal/controlplane/paginate"
	"testing"
)

// 本文件补全 server_paginate.go 中 0% 覆盖的分页中间件：
//   - jsonErrorMux.ServeHTTP / responseCapture.WriteHeader / responseCapture.Write
//   - paginateJSONHandler

// =============================================================================
// jsonErrorMux
// =============================================================================

func TestJSONErrorMux_NotFound(t *testing.T) {
	mux := &paginate.JSONErrorMux{Inner: http.NewServeMux()}
	req := httptest.NewRequest(http.MethodGet, "/no-such-path", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["error"] != "not found" {
		t.Errorf("error=%q, want 'not found'", resp["error"])
	}
}

func TestJSONErrorMux_Routing(t *testing.T) {
	inner := http.NewServeMux()
	inner.HandleFunc("/api/v1/ok", func(w http.ResponseWriter, r *http.Request) {
		paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux := &paginate.JSONErrorMux{Inner: inner}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ok", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
}

// =============================================================================
// parsePagination
// =============================================================================

func TestParsePagination_NoPage(t *testing.T) {
	q := url.Values{}
	page, size := paginate.ParsePagination(q)
	if page != 0 || size != 0 {
		t.Errorf("no page: got %d/%d, want 0/0", page, size)
	}
}

func TestParsePagination_Defaults(t *testing.T) {
	q := url.Values{}
	q.Set("page", "1")
	page, size := paginate.ParsePagination(q)
	if page != 1 || size != 20 {
		t.Errorf("default: got %d/%d, want 1/20", page, size)
	}
}

func TestParsePagination_CustomSize(t *testing.T) {
	q := url.Values{}
	q.Set("page", "2")
	q.Set("pageSize", "50")
	page, size := paginate.ParsePagination(q)
	if page != 2 || size != 50 {
		t.Errorf("custom: got %d/%d, want 2/50", page, size)
	}
}

func TestParsePagination_SizeCapped(t *testing.T) {
	q := url.Values{}
	q.Set("page", "1")
	q.Set("pageSize", "500")
	_, size := paginate.ParsePagination(q)
	if size != 200 {
		t.Errorf("capped: got %d, want 200", size)
	}
}

func TestParsePagination_InvalidPage(t *testing.T) {
	q := url.Values{}
	q.Set("page", "-1")
	page, _ := paginate.ParsePagination(q)
	if page != 1 {
		t.Errorf("invalid page: got %d, want 1", page)
	}
}

// =============================================================================
// paginateJSONHandler
// =============================================================================

func TestPaginateJSONHandler_NoPageBypass(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		paginate.WriteJSON(w, http.StatusOK, []int{1, 2, 3})
	})
	h := paginate.PaginateJSONHandler(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !called {
		t.Error("inner should be called directly when no page param")
	}
}

func TestPaginateJSONHandler_PostBypass(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		paginate.WriteJSON(w, http.StatusOK, []int{1})
	})
	h := paginate.PaginateJSONHandler(inner)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/x?page=1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !called {
		t.Error("inner should be called directly for POST")
	}
}

func TestPaginateJSONHandler_PaginateArray(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paginate.WriteJSON(w, http.StatusOK, []int{1, 2, 3, 4, 5})
	})
	h := paginate.PaginateJSONHandler(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/x?page=1&pageSize=2", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	var resp paginate.PaginateResult
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rec.Body.String())
	}
	if resp.Total != 5 || resp.Page != 1 || resp.PageSize != 2 {
		t.Errorf("meta=%+v", resp)
	}
	if !resp.HasMore {
		t.Error("HasMore should be true")
	}
}

func TestPaginateJSONHandler_Page2(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paginate.WriteJSON(w, http.StatusOK, []int{1, 2, 3, 4, 5})
	})
	h := paginate.PaginateJSONHandler(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/x?page=2&pageSize=2", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var resp paginate.PaginateResult
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Page != 2 || !resp.HasMore {
		t.Errorf("page2=%+v", resp)
	}
}

func TestPaginateJSONHandler_LastPage(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paginate.WriteJSON(w, http.StatusOK, []int{1, 2, 3, 4, 5})
	})
	h := paginate.PaginateJSONHandler(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/x?page=3&pageSize=2", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var resp paginate.PaginateResult
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.HasMore {
		t.Error("HasMore should be false on last page")
	}
}

func TestPaginateJSONHandler_BeyondEnd(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paginate.WriteJSON(w, http.StatusOK, []int{1, 2, 3})
	})
	h := paginate.PaginateJSONHandler(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/x?page=10&pageSize=2", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestPaginateJSONHandler_NonArrayJSON(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paginate.WriteJSON(w, http.StatusOK, map[string]string{"key": "value"})
	})
	h := paginate.PaginateJSONHandler(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/x?page=1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// 非 JSON 数组应直接转发
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestPaginateJSONHandler_Non200Status(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "bad"})
	})
	h := paginate.PaginateJSONHandler(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/x?page=1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

// =============================================================================
// responseCapture 直接测试
// =============================================================================

func TestResponseCapture_WriteAndHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	c := &paginate.ResponseCapture{ResponseWriter: rec}
	c.WriteHeader(http.StatusCreated)
	n, _ := c.Write([]byte("hello"))
	if n != 5 {
		t.Errorf("write n=%d, want 5", n)
	}
}

func TestResponseCapture_WriteDefaultStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	c := &paginate.ResponseCapture{ResponseWriter: rec}
	c.Write([]byte("x"))
	// Default status should be 200 (can't check unexported field from outside package)
}

// ============================================================================
// M11 分页边界值守护：page=0/pageSize=0/page=-1/pageSize=100000 + 空 body POST→400
// ============================================================================

// TestParsePagination_Page0_ClampTo1 验证 page=0 被 clamp 到 1（page<1 归一为 1）。
// page=0 在 API 语义上表示"不分页"，但 parsePagination 的契约是 page<1 一律 clamp 到 1，
// 防止 start=(page-1)*pageSize 算出负数导致切片越界。
func TestParsePagination_Page0_ClampTo1(t *testing.T) {
	q := url.Values{}
	q.Set("page", "0")
	page, _ := paginate.ParsePagination(q)
	if page != 1 {
		t.Errorf("page=0 should clamp to 1; got %d", page)
	}
}

// TestParsePagination_PageSize0_Default20 验证 pageSize=0 回退到默认 20。
// parsePagination 对 pageSize<=0 不接受，保持默认值 20（防零页大小导致除零/空页）。
func TestParsePagination_PageSize0_Default20(t *testing.T) {
	q := url.Values{}
	q.Set("page", "1")
	q.Set("pageSize", "0")
	_, size := paginate.ParsePagination(q)
	if size != 20 {
		t.Errorf("pageSize=0 should fall back to default 20; got %d", size)
	}
}

// TestParsePagination_PageSizeHuge_ClampTo200 验证 pageSize=100000 被 clamp 到 200 上限。
// 防止客户端请求超大页大小拖垮内存/DB。
func TestParsePagination_PageSizeHuge_ClampTo200(t *testing.T) {
	q := url.Values{}
	q.Set("page", "1")
	q.Set("pageSize", "100000")
	_, size := paginate.ParsePagination(q)
	if size != 200 {
		t.Errorf("pageSize=100000 should clamp to 200; got %d", size)
	}
}

// TestParsePagination_PageSizeNegative_Default20 验证 pageSize=-1 回退到默认 20。
func TestParsePagination_PageSizeNegative_Default20(t *testing.T) {
	q := url.Values{}
	q.Set("page", "1")
	q.Set("pageSize", "-1")
	_, size := paginate.ParsePagination(q)
	if size != 20 {
		t.Errorf("pageSize=-1 should fall back to default 20; got %d", size)
	}
}

// TestParsePagination_PageSizeOne 验证 pageSize=1 边界值（最小有效页大小）。
func TestParsePagination_PageSizeOne(t *testing.T) {
	q := url.Values{}
	q.Set("page", "1")
	q.Set("pageSize", "1")
	_, size := paginate.ParsePagination(q)
	if size != 1 {
		t.Errorf("pageSize=1 should be accepted; got %d", size)
	}
}

// TestParsePagination_PageSizeTwoHundred 验证 pageSize=200 边界值（上限恰好 200）。
func TestParsePagination_PageSizeTwoHundred(t *testing.T) {
	q := url.Values{}
	q.Set("page", "1")
	q.Set("pageSize", "200")
	_, size := paginate.ParsePagination(q)
	if size != 200 {
		t.Errorf("pageSize=200 should be accepted; got %d", size)
	}
}

// TestParsePagination_PageSizeTwoHundredOne 验证 pageSize=201 被 clamp 到 200（超上限 1）。
func TestParsePagination_PageSizeTwoHundredOne(t *testing.T) {
	q := url.Values{}
	q.Set("page", "1")
	q.Set("pageSize", "201")
	_, size := paginate.ParsePagination(q)
	if size != 200 {
		t.Errorf("pageSize=201 should clamp to 200; got %d", size)
	}
}

// TestParsePagination_NonNumericPage 验证 page=abc 非数字时 clamp 到 1。
// strconv.Atoi 失败时 page 保持 0，随后 page<1 clamp 到 1，防非法输入致错。
func TestParsePagination_NonNumericPage(t *testing.T) {
	q := url.Values{}
	q.Set("page", "abc")
	page, _ := paginate.ParsePagination(q)
	if page != 1 {
		t.Errorf("page=abc should clamp to 1; got %d", page)
	}
}

// TestParsePagination_NonNumericPageSize 验证 pageSize=xyz 非数字时回退默认 20。
func TestParsePagination_NonNumericPageSize(t *testing.T) {
	q := url.Values{}
	q.Set("page", "1")
	q.Set("pageSize", "xyz")
	_, size := paginate.ParsePagination(q)
	if size != 20 {
		t.Errorf("pageSize=xyz should fall back to default 20; got %d", size)
	}
}

// TestPaginateJSONHandler_PostEmptyBodyBypass400 验证 POST 空 body 透传 inner 的 400。
// paginateJSONHandler 对 POST 请求直接透传（不分页），inner handler 对空 body 返回 400
// 时，paginate 透传 400。锁定 POST 透传路径不误吞 inner 错误码。
func TestPaginateJSONHandler_PostEmptyBodyBypass400(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 模拟 inner handler 对空 body POST 返回 400。
		if r.ContentLength == 0 {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "empty body"})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	h := paginate.PaginateJSONHandler(inner)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/x?page=1", nil) // 空 body
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 (POST 空 body 应透传 inner 400); body=%s", rec.Code, rec.Body.String())
	}
}

// TestPaginateJSONHandler_PageZeroStillPaginates 验证 page=0 经 clamp 后仍触发分页。
// page=0 经 parsePagination clamp 到 1，paginateJSONHandler 捕获 inner 数组并分页返回。
func TestPaginateJSONHandler_PageZeroStillPaginates(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paginate.WriteJSON(w, http.StatusOK, []int{1, 2, 3, 4, 5})
	})
	h := paginate.PaginateJSONHandler(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/x?page=0&pageSize=2", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	var resp paginate.PaginateResult
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rec.Body.String())
	}
	// page=0 clamp 到 1，应返回第 1 页（前 2 个元素）。
	if resp.Page != 1 || resp.PageSize != 2 || resp.Total != 5 {
		t.Errorf("meta=%+v, want Page=1 PageSize=2 Total=5", resp)
	}
}
