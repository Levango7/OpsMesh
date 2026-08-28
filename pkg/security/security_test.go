package security

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSanitizeHTML_StripsScriptTags(t *testing.T) {
	input := `<script>alert("xss")</script>hello`
	result := SanitizeHTML(input)
	if strings.Contains(result, "script") || strings.Contains(result, "alert") {
		t.Errorf("SanitizeHTML 未剥离 script 标签: got %q", result)
	}
}

func TestSanitizeHTML_StripsEventHandlers(t *testing.T) {
	input := `<img src="x" onerror="alert(1)">`
	result := SanitizeHTML(input)
	if strings.Contains(result, "onerror") {
		t.Errorf("SanitizeHTML 未剥离事件处理器: got %q", result)
	}
}

func TestSanitizeHTML_StripsJavaScriptURL(t *testing.T) {
	input := `<a href="javascript:alert(1)">click</a>`
	result := SanitizeHTML(input)
	if strings.Contains(strings.ToLower(result), "javascript:") {
		t.Errorf("SanitizeHTML 未剥离 javascript: URL: got %q", result)
	}
}

func TestSanitizeHTML_PreservesSafeContent(t *testing.T) {
	input := `<p>Hello <strong>world</strong></p>`
	result := SanitizeHTML(input)
	if !strings.Contains(result, "Hello") || !strings.Contains(result, "world") {
		t.Errorf("SanitizeHTML 误删安全内容: got %q", result)
	}
}

func TestSanitizeSQL_EscapeSingleQuote(t *testing.T) {
	input := `O'Brien`
	result := SanitizeSQL(input)
	if result != `O''Brien` {
		t.Errorf("SanitizeSQL 未正确转义单引号: got %q", result)
	}
}

func TestSanitizeSQL_RemoveNullByte(t *testing.T) {
	input := "hello\x00world"
	result := SanitizeSQL(input)
	if strings.Contains(result, "\x00") {
		t.Errorf("SanitizeSQL 未移除 null 字节: got %q", result)
	}
}

func TestValidateInput_ExceedsMaxLength(t *testing.T) {
	err := ValidateInput("toolong", 5)
	if err == nil {
		t.Error("ValidateInput 应拒绝超长输入")
	}
}

func TestValidateInput_RejectsIllegalChars(t *testing.T) {
	err := ValidateInput("hello<script>", 100)
	if err == nil {
		t.Error("ValidateInput 应拒绝含非法字符的输入")
	}
}

func TestValidateInput_AcceptsValidInput(t *testing.T) {
	err := ValidateInput("valid_input-123", 100)
	if err != nil {
		t.Errorf("ValidateInput 不应拒绝合法输入: %v", err)
	}
}

func TestIPRateLimit_AllowsWithinLimit(t *testing.T) {
	middleware := IPRateLimit(3, time.Second)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("请求 %d 应被允许, got %d", i+1, rr.Code)
		}
	}
}

func TestIPRateLimit_BlocksOverLimit(t *testing.T) {
	middleware := IPRateLimit(2, time.Second)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "192.168.1.2:12345"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.2:12345"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("第3次请求应被限流, got %d", rr.Code)
	}
}

func TestUserRateLimit_BlocksOverLimit(t *testing.T) {
	middleware := UserRateLimit(1, time.Second)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-User-ID", "user1")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("首次请求应被允许, got %d", rr.Code)
	}

	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-User-ID", "user1")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("第2次请求应被限流, got %d", rr.Code)
	}
}

func TestGlobalRateLimit_AllowsWithinLimit(t *testing.T) {
	middleware := GlobalRateLimit(100)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("全局限流应允许正常请求, got %d", rr.Code)
	}
}

func TestSecurityHeadersMiddleware_SetsHeaders(t *testing.T) {
	middleware := SecurityHeadersMiddleware()
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	expected := map[string]string{
		"X-Content-Type-Options":     "nosniff",
		"X-Frame-Options":            "DENY",
		"X-XSS-Protection":           "1; mode=block",
		"Content-Security-Policy":    "default-src 'self'",
		"Strict-Transport-Security":  "max-age=31536000; includeSubDomains",
		"Referrer-Policy":            "strict-origin-when-cross-origin",
	}

	for header, want := range expected {
		got := rr.Header().Get(header)
		if got != want {
			t.Errorf("header %s: got %q, want %q", header, got, want)
		}
	}
}

func TestConnectionLimit_BlocksOverLimit(t *testing.T) {
	middleware := ConnectionLimit(1)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))

	var rr1, rr2 *httptest.ResponseRecorder
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		req1 := httptest.NewRequest("GET", "/", nil)
		rr1 = httptest.NewRecorder()
		handler.ServeHTTP(rr1, req1)
	}()

	go func() {
		defer wg.Done()
		req2 := httptest.NewRequest("GET", "/", nil)
		rr2 = httptest.NewRecorder()
		handler.ServeHTTP(rr2, req2)
	}()

	wg.Wait()

	if rr1.Code != http.StatusOK && rr2.Code != http.StatusOK {
		t.Errorf("ConnectionLimit 应允许一个请求, got %d and %d", rr1.Code, rr2.Code)
	}
	if rr1.Code != http.StatusServiceUnavailable && rr2.Code != http.StatusServiceUnavailable {
		t.Errorf("ConnectionLimit 应拒绝一个超限连接, got %d and %d", rr1.Code, rr2.Code)
	}
}

func TestRequestSizeLimit_RejectsOversizedBody(t *testing.T) {
	middleware := RequestSizeLimit(10)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 1024)
		_, err := r.Body.Read(body)
		if err != nil {
			http.Error(w, "请求体过大", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/", strings.NewReader("this is a very long body that exceeds limit"))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("RequestSizeLimit 应拒绝超大请求体, got %d", rr.Code)
	}
}

func TestCORS_Middleware_AllowsAllowedOrigin(t *testing.T) {
	config := CORSConfig{
		AllowedOrigins: []string{"https://example.com"},
		AllowedMethods: []string{"GET", "POST"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
	}
	middleware := config.Middleware()
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://example.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("CORS 应允许合法来源, got %d", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Errorf("CORS 应设置 Access-Control-Allow-Origin, got %q", got)
	}
}

func TestCORS_Middleware_BlocksDisallowedOrigin(t *testing.T) {
	config := CORSConfig{
		AllowedOrigins: []string{"https://example.com"},
		AllowedMethods: []string{"GET", "POST"},
		AllowedHeaders: []string{"Content-Type"},
	}
	middleware := config.Middleware()
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://evil.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("CORS 应拒绝非法来源, got %d", rr.Code)
	}
}

func TestCORS_Middleware_HandlesPreflight(t *testing.T) {
	config := CORSConfig{
		AllowedOrigins: []string{"https://example.com"},
		AllowedMethods: []string{"GET", "POST", "DELETE"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
	}
	middleware := config.Middleware()
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("OPTIONS", "/", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("CORS 预检请求应返回 204, got %d", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST, DELETE" {
		t.Errorf("CORS 预检应返回允许的方法, got %q", got)
	}
}
