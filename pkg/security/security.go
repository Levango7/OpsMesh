// pkg/security — 统一安全中间件库：XSS 防护、速率限制、安全头、DDoS 防护、CORS。
package security

import (
	"container/heap"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ==================== XSS Protection ====================

var (
	xssScriptTag      = regexp.MustCompile(`(?i)<script[^>]*>.*?</script>`)
	xssScriptTagOpen  = regexp.MustCompile(`(?i)<script[^>]*>`)
	xssScriptTagClose = regexp.MustCompile(`(?i)</script>`)
	xssEventHandlers  = regexp.MustCompile(`(?i)\son\w+\s*=`)
	xssJavaScriptURL  = regexp.MustCompile(`(?i)javascript\s*:`)
	xssDataURL        = regexp.MustCompile(`(?i)data\s*:\s*text/html`)
	xssExpression     = regexp.MustCompile(`(?i)expression\s*\(`)
)

// SanitizeHTML 剥离 script 标签、事件处理器、javascript: URL 等 XSS 载荷。
func SanitizeHTML(input string) string {
	input = xssScriptTag.ReplaceAllString(input, "")
	input = xssScriptTagOpen.ReplaceAllString(input, "")
	input = xssScriptTagClose.ReplaceAllString(input, "")
	input = xssEventHandlers.ReplaceAllString(input, " data-disabled=")
	input = xssJavaScriptURL.ReplaceAllString(input, "disabled:")
	input = xssDataURL.ReplaceAllString(input, "disabled:")
	input = xssExpression.ReplaceAllString(input, "disabled(")
	return input
}

// SanitizeSQL 对输入中的 SQL 特殊字符进行转义（纵深防御，不能替代参数化查询）。
func SanitizeSQL(input string) string {
	input = strings.ReplaceAll(input, "'", "''")
	input = strings.ReplaceAll(input, "\\", "\\\\")
	input = strings.ReplaceAll(input, "\x00", "")
	input = strings.ReplaceAll(input, "\n", "\\n")
	input = strings.ReplaceAll(input, "\r", "\\r")
	input = strings.ReplaceAll(input, "\x1a", "\\Z")
	return input
}

// ValidateInput 校验输入长度和字符模式。
func ValidateInput(input string, maxLength int) error {
	if len(input) > maxLength {
		return fmt.Errorf("输入长度 %d 超过最大允许值 %d", len(input), maxLength)
	}
	if strings.ContainsAny(input, "<>\"'%;()&+") {
		return fmt.Errorf("输入包含非法字符")
	}
	return nil
}

// ==================== Rate Limit (Sliding Window) ====================

// windowEntry 表示滑动窗口内的一次请求时间戳。
type windowEntry struct {
	timestamp time.Time
	index     int
}

// windowHeap 是最小堆，用于滑动窗口过期清理。
type windowHeap []time.Time

func (h windowHeap) Len() int            { return len(h) }
func (h windowHeap) Less(i, j int) bool  { return h[i].Before(h[j]) }
func (h windowHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *windowHeap) Push(x interface{}) { *h = append(*h, x.(time.Time)) }
func (h *windowHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// slidingWindow 实现基于最小堆的滑动窗口速率限制器。
type slidingWindow struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	requests windowHeap
}

func newSlidingWindow(limit int, window time.Duration) *slidingWindow {
	h := make(windowHeap, 0, limit)
	heap.Init(&h)
	return &slidingWindow{
		limit:    limit,
		window:   window,
		requests: h,
	}
}

func (sw *slidingWindow) allow() bool {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-sw.window)

	for sw.requests.Len() > 0 && sw.requests[0].Before(cutoff) {
		heap.Pop(&sw.requests)
	}

	if sw.requests.Len() >= sw.limit {
		return false
	}

	heap.Push(&sw.requests, now)
	return true
}

// IPRateLimit 返回每 IP 滑动窗口速率限制中间件。
func IPRateLimit(requests int, window time.Duration) func(http.Handler) http.Handler {
	mu := sync.Mutex{}
	limiters := make(map[string]*slidingWindow)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}

			mu.Lock()
			sw, ok := limiters[ip]
			if !ok {
				sw = newSlidingWindow(requests, window)
				limiters[ip] = sw
			}
			mu.Unlock()

			if !sw.allow() {
				w.Header().Set("Retry-After", fmt.Sprintf("%d", int(window.Seconds())))
				http.Error(w, "请求过于频繁，请稍后重试", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// UserRateLimit 返回每用户滑动窗口速率限制中间件（从 X-User-ID header 提取用户标识）。
func UserRateLimit(requests int, window time.Duration) func(http.Handler) http.Handler {
	mu := sync.Mutex{}
	limiters := make(map[string]*slidingWindow)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := r.Header.Get("X-User-ID")
			if userID == "" {
				userID = "anonymous"
			}

			mu.Lock()
			sw, ok := limiters[userID]
			if !ok {
				sw = newSlidingWindow(requests, window)
				limiters[userID] = sw
			}
			mu.Unlock()

			if !sw.allow() {
				w.Header().Set("Retry-After", fmt.Sprintf("%d", int(window.Seconds())))
				http.Error(w, "请求过于频繁，请稍后重试", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// GlobalRateLimit 返回全局令牌桶式速率限制中间件。
func GlobalRateLimit(rps int) func(http.Handler) http.Handler {
	interval := time.Second / time.Duration(rps)
	ticker := time.NewTicker(interval)
	tokens := make(chan struct{}, rps)

	for i := 0; i < rps; i++ {
		tokens <- struct{}{}
	}

	go func() {
		for range ticker.C {
			select {
			case tokens <- struct{}{}:
			default:
			}
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-tokens:
				next.ServeHTTP(w, r)
			default:
				w.Header().Set("Retry-After", "1")
				http.Error(w, "服务繁忙，请稍后重试", http.StatusTooManyRequests)
			}
		})
	}
}

// ==================== Security Headers ====================

// Middleware 注入安全响应头。
func SecurityHeadersMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("X-XSS-Protection", "1; mode=block")
			w.Header().Set("Content-Security-Policy", "default-src 'self'")
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			next.ServeHTTP(w, r)
		})
	}
}

// ==================== DDoS Protection ====================

// ConnectionLimit 限制最大并发连接数。
func ConnectionLimit(maxConcurrent int) func(http.Handler) http.Handler {
	semaphore := make(chan struct{}, maxConcurrent)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
				next.ServeHTTP(w, r)
			default:
				http.Error(w, "服务连接数已满，请稍后重试", http.StatusServiceUnavailable)
			}
		})
	}
}

// RequestSizeLimit 限制请求体最大字节数。
func RequestSizeLimit(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// SlowLorisProtection 设置读超时防 Slowloris 攻击。
func SlowLorisProtection(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			done := make(chan struct{})
			go func() {
				next.ServeHTTP(w, r)
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(timeout):
				http.Error(w, "请求超时", http.StatusGatewayTimeout)
			}
		})
	}
}

// ==================== CORS ====================

// CORSConfig 严格 CORS 配置。
type CORSConfig struct {
	AllowedOrigins []string
	AllowedMethods []string
	AllowedHeaders []string
}

// Middleware 返回严格 CORS 中间件。
func (c CORSConfig) Middleware() func(http.Handler) http.Handler {
	originSet := make(map[string]bool, len(c.AllowedOrigins))
	for _, o := range c.AllowedOrigins {
		originSet[o] = true
	}

	methods := strings.Join(c.AllowedMethods, ", ")
	headers := strings.Join(c.AllowedHeaders, ", ")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if origin != "" {
				if !originSet[origin] {
					http.Error(w, "CORS: 来源不被允许", http.StatusForbidden)
					return
				}
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
			}

			if methods != "" {
				w.Header().Set("Access-Control-Allow-Methods", methods)
			}
			if headers != "" {
				w.Header().Set("Access-Control-Allow-Headers", headers)
			}
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Max-Age", "600")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
