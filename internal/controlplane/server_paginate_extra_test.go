package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// 本文件补全 server_paginate.go 中 0% 覆盖的分页中间件：
//   - jsonErrorMux.ServeHTTP / responseCapture.WriteHeader / responseCapture.Write
//   - paginateJSONHandler

// =============================================================================
// jsonErrorMux
// =============================================================================

func TestJSONErrorMux_NotFound(t *testing.T) {
	mux := &jsonErrorMux{inner: http.NewServeMux()}
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
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux := &jsonErrorMux{inner: inner}
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
	page, size := parsePagination(q)
	if page != 0 || size != 0 {
		t.Errorf("no page: got %d/%d, want 0/0", page, size)
	}
}

func TestParsePagination_Defaults(t *testing.T) {
	q := url.Values{}
	q.Set("page", "1")
	page, size := parsePagination(q)
	if page != 1 || size != 20 {
		t.Errorf("default: got %d/%d, want 1/20", page, size)
	}
}

func TestParsePagination_CustomSize(t *testing.T) {
	q := url.Values{}
	q.Set("page", "2")
	q.Set("pageSize", "50")
	page, size := parsePagination(q)
	if page != 2 || size != 50 {
		t.Errorf("custom: got %d/%d, want 2/50", page, size)
	}
}

func TestParsePagination_SizeCapped(t *testing.T) {
	q := url.Values{}
	q.Set("page", "1")
	q.Set("pageSize", "500")
	_, size := parsePagination(q)
	if size != 200 {
		t.Errorf("capped: got %d, want 200", size)
	}
}

func TestParsePagination_InvalidPage(t *testing.T) {
	q := url.Values{}
	q.Set("page", "-1")
	page, _ := parsePagination(q)
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
		writeJSON(w, http.StatusOK, []int{1, 2, 3})
	})
	h := paginateJSONHandler(inner)
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
		writeJSON(w, http.StatusOK, []int{1})
	})
	h := paginateJSONHandler(inner)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/x?page=1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !called {
		t.Error("inner should be called directly for POST")
	}
}

func TestPaginateJSONHandler_PaginateArray(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []int{1, 2, 3, 4, 5})
	})
	h := paginateJSONHandler(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/x?page=1&pageSize=2", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	var resp paginateResult
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
		writeJSON(w, http.StatusOK, []int{1, 2, 3, 4, 5})
	})
	h := paginateJSONHandler(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/x?page=2&pageSize=2", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var resp paginateResult
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Page != 2 || !resp.HasMore {
		t.Errorf("page2=%+v", resp)
	}
}

func TestPaginateJSONHandler_LastPage(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []int{1, 2, 3, 4, 5})
	})
	h := paginateJSONHandler(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/x?page=3&pageSize=2", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var resp paginateResult
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.HasMore {
		t.Error("HasMore should be false on last page")
	}
}

func TestPaginateJSONHandler_BeyondEnd(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []int{1, 2, 3})
	})
	h := paginateJSONHandler(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/x?page=10&pageSize=2", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestPaginateJSONHandler_NonArrayJSON(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"key": "value"})
	})
	h := paginateJSONHandler(inner)
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad"})
	})
	h := paginateJSONHandler(inner)
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
	c := &responseCapture{ResponseWriter: rec}
	c.WriteHeader(http.StatusCreated)
	if c.status != http.StatusCreated {
		t.Errorf("status=%d, want 201", c.status)
	}
	n, _ := c.Write([]byte("hello"))
	if n != 5 {
		t.Errorf("write n=%d, want 5", n)
	}
	if c.body.String() != "hello" {
		t.Errorf("body=%q, want 'hello'", c.body.String())
	}
}

func TestResponseCapture_WriteDefaultStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	c := &responseCapture{ResponseWriter: rec}
	c.Write([]byte("x"))
	if c.status != http.StatusOK {
		t.Errorf("default status=%d, want 200", c.status)
	}
}
