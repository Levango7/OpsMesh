package paginate

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, http.StatusCreated, map[string]string{"key": "value"})
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	if !strings.Contains(w.Body.String(), `"key":"value"`) {
		t.Fatalf("body = %q, want contains key:value", w.Body.String())
	}
}

func TestJSONError(t *testing.T) {
	w := httptest.NewRecorder()
	JSONError(w, http.StatusBadRequest, "bad input")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"error":"bad input"`) {
		t.Fatalf("body = %q, want contains error:bad input", w.Body.String())
	}
}

func TestJSONErrorMux_NotFound(t *testing.T) {
	inner := http.NewServeMux()
	mux := &JSONErrorMux{Inner: inner}

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"error":"not found"`) {
		t.Fatalf("body = %q, want contains not found", w.Body.String())
	}
}

func TestJSONErrorMux_KnownRoute(t *testing.T) {
	inner := http.NewServeMux()
	inner.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux := &JSONErrorMux{Inner: inner}

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestParsePagination_Defaults(t *testing.T) {
	q := url.Values{}
	page, pageSize := ParsePagination(q)
	if page != 0 {
		t.Fatalf("Page = %d, want 0 (no pagination)", page)
	}
	if pageSize != 0 {
		t.Fatalf("PageSize = %d, want 0 (no pagination)", pageSize)
	}
}

func TestParsePagination_Custom(t *testing.T) {
	q := url.Values{}
	q.Set("page", "3")
	q.Set("pageSize", "50")
	page, pageSize := ParsePagination(q)
	if page != 3 {
		t.Fatalf("Page = %d, want 3", page)
	}
	if pageSize != 50 {
		t.Fatalf("PageSize = %d, want 50", pageSize)
	}
}

func TestParsePagination_DefaultPageSize(t *testing.T) {
	q := url.Values{}
	q.Set("page", "1")
	page, pageSize := ParsePagination(q)
	if page != 1 {
		t.Fatalf("Page = %d, want 1", page)
	}
	if pageSize != 20 {
		t.Fatalf("PageSize = %d, want 20", pageSize)
	}
}
