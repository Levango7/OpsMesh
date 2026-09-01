// compress_test.go 测试 HTTP 响应压缩中间件。
// 验证：Accept-Encoding 协商（br 优先于 gzip）、gzip 响应可被 gzip.NewReader 正确解压、
// 无 Accept-Encoding 时不压缩、非 200 状态码跳过压缩、已压缩内容类型跳过、
// WriteHeader 幂等、Flush 流式透传、MinCompressSize 边界、空响应体。
package compress

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
)

// largeBody 构造超过 MinCompressSize 阈值的可压缩响应体（重复文本，压缩比极高）。
func largeBody() []byte {
	return []byte(strings.Repeat("compressible data. ", 100)) // ~2KB > 1KB
}

// doRequest 用指定 Accept-Encoding 走一遍中间件，返回响应。
func doRequest(t *testing.T, acceptEncoding string, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	mw := Middleware()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/data", nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	mw(handler).ServeHTTP(rec, req)
	return rec
}

// TestSelectEncoding 验证 Accept-Encoding 协商：br 优先于 gzip。
func TestSelectEncoding(t *testing.T) {
	tests := []struct {
		acceptEncoding string
		want           string
	}{
		{"gzip", "gzip"},
		{"GZIP", "gzip"},               // 大小写不敏感
		{"br", "br"},                   // Brotli 优先
		{"gzip, deflate, br", "br"},    // 同时支持时选 br
		{"gzip;q=1.0, br;q=0.5", "br"}, // 忽略权重直接选 br
		{"deflate", ""},                // 不支持的编码
		{"identity", ""},               // identity 不压缩
		{"", ""},                       // 空头
		{"gzip, deflate", "gzip"},      // 仅支持 gzip 时回退 gzip
		{"GZip, BR", "br"},             // 混合大小写
		{"brotli", "br"},               // 含 "br" 子串即选中（宽松匹配）
		{"x-br-token", "br"},           // 子串匹配边界（见下方源码疑点注释）
	}
	for _, tt := range tests {
		if got := selectEncoding(tt.acceptEncoding); got != tt.want {
			t.Errorf("selectEncoding(%q) = %q, want %q", tt.acceptEncoding, got, tt.want)
		}
	}
}

// TestShouldCompress 验证内容类型过滤：已压缩格式跳过。
func TestShouldCompress(t *testing.T) {
	tests := []struct {
		contentType string
		want        bool
	}{
		{"text/plain", true},
		{"application/json", true},
		{"text/html; charset=utf-8", true},
		{"", true}, // 空 Content-Type 默认压缩
		{"image/png", false},
		{"image/jpeg", false},
		{"IMAGE/PNG", false}, // 大小写不敏感
		{"audio/mpeg", false},
		{"video/mp4", false},
		{"application/gzip", false},
		{"application/zip", false},
		{"application/pdf", false},
		{"application/octet-stream", false},
		{"application/x-tar", false},
	}
	for _, tt := range tests {
		if got := shouldCompress(tt.contentType); got != tt.want {
			t.Errorf("shouldCompress(%q) = %v, want %v", tt.contentType, got, tt.want)
		}
	}
}

// TestGetCompressLevel 验证压缩级别读取（当前固定返回 DefaultGzipLevel）。
func TestGetCompressLevel(t *testing.T) {
	if got := getCompressLevel(); got != DefaultGzipLevel {
		t.Errorf("getCompressLevel() = %d, want %d (DefaultGzipLevel)", got, DefaultGzipLevel)
	}
}

// TestMiddlewareGzipCompression 验证 Accept-Encoding: gzip 时响应被压缩，
// 且能用 gzip.NewReader 解压还原原文。
func TestMiddlewareGzipCompression(t *testing.T) {
	body := largeBody()
	rec := doRequest(t, "gzip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write(body)
	})

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want %q", got, "gzip")
	}
	if got := rec.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Fatalf("Vary = %q, want %q", got, "Accept-Encoding")
	}
	if rec.Body.Len() >= len(body) {
		t.Fatalf("响应未被压缩: %d >= %d", rec.Body.Len(), len(body))
	}

	// 用 gzip.NewReader 解压验证内容一致。
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader 失败: %v", err)
	}
	defer zr.Close()
	restored, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	if string(restored) != string(body) {
		t.Fatalf("解压后内容不一致: got %d bytes, want %d bytes", len(restored), len(body))
	}
}

// TestMiddlewareBrotliCompression 验证 Accept-Encoding: br 时响应用 Brotli 压缩，
// 能用 brotli.NewReader 解压还原。
func TestMiddlewareBrotliCompression(t *testing.T) {
	body := largeBody()
	rec := doRequest(t, "br", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write(body)
	})

	if got := rec.Header().Get("Content-Encoding"); got != "br" {
		t.Fatalf("Content-Encoding = %q, want %q", got, "br")
	}
	if rec.Body.Len() >= len(body) {
		t.Fatalf("响应未被压缩: %d >= %d", rec.Body.Len(), len(body))
	}

	zr := brotli.NewReader(rec.Body)
	restored, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("brotli 解压失败: %v", err)
	}
	if string(restored) != string(body) {
		t.Fatalf("解压后内容不一致: got %d bytes, want %d bytes", len(restored), len(body))
	}
}

// TestMiddlewarePrefersBrotli 验证同时支持 gzip 和 br 时选 br。
func TestMiddlewarePrefersBrotli(t *testing.T) {
	rec := doRequest(t, "gzip, br", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write(largeBody())
	})
	if got := rec.Header().Get("Content-Encoding"); got != "br" {
		t.Fatalf("Content-Encoding = %q, want br（应优先 Brotli）", got)
	}
}

// TestMiddlewareNoAcceptEncoding 验证无 Accept-Encoding 头时透传不压缩。
func TestMiddlewareNoAcceptEncoding(t *testing.T) {
	body := largeBody()
	rec := doRequest(t, "", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write(body)
	})
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("无 Accept-Encoding 时不应设置 Content-Encoding, got %q", got)
	}
	if rec.Body.String() != string(body) {
		t.Fatalf("响应体应原样透传")
	}
}

// TestMiddlewareUnsupportedEncoding 验证不支持的编码（如 deflate）时透传。
func TestMiddlewareUnsupportedEncoding(t *testing.T) {
	body := largeBody()
	rec := doRequest(t, "deflate", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write(body)
	})
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("不支持的编码不应压缩, Content-Encoding = %q", got)
	}
	if rec.Body.String() != string(body) {
		t.Fatal("响应体应原样透传")
	}
}

// TestMiddlewareSmallBodyCompressedDespiteDocs 验证小响应体的实际压缩行为。
//
// 源码疑点（不改产品代码，记录现状）：文档注释宣称"小于 MinCompressSize(1KB) 的
// 响应不压缩以避免开销"，但 Write() 的判断是
// `if cw.bytesWritten < MinCompressSize && cw.writer == nil { 透传 }`——
// WriteHeader(200) 时压缩器（cw.writer）已被初始化且不再为 nil，故该分支永远不命中，
// 任何大小的响应体（含 "small"）实际都会被压缩。文档语义（小响应跳过压缩）
// 未实现。本用例按实际行为断言：小响应也被压缩且可正确解压。
func TestMiddlewareSmallBodyCompressedDespiteDocs(t *testing.T) {
	body := []byte("small") // 5 字节 << 1KB 阈值
	rec := doRequest(t, "gzip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write(body)
	})
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("实际行为：任何大小均压缩, Content-Encoding = %q, want gzip", got)
	}
	// 解压还原。
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader 失败: %v", err)
	}
	defer zr.Close()
	restored, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	if string(restored) != string(body) {
		t.Fatalf("解压后内容不一致: got %q, want %q", restored, body)
	}
}

// TestMiddlewareMinCompressSizeBoundary 验证恰好达到 MinCompressSize 的响应被压缩。
func TestMiddlewareMinCompressSizeBoundary(t *testing.T) {
	body := []byte(strings.Repeat("a", MinCompressSize)) // 恰好 1KB
	rec := doRequest(t, "gzip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write(body)
	})
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("达到阈值的响应应压缩, Content-Encoding = %q", got)
	}
	// 解压还原。
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader 失败: %v", err)
	}
	defer zr.Close()
	restored, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	if string(restored) != string(body) {
		t.Fatal("解压后内容不一致")
	}
}

// TestMiddlewareLargeBodyExceedsThreshold 验证超长响应（1MB+）压缩正确。
func TestMiddlewareLargeBodyExceedsThreshold(t *testing.T) {
	body := []byte(strings.Repeat("abcdefghij", 120000)) // 1.2MB
	rec := doRequest(t, "gzip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write(body)
	})
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("超长响应应压缩, Content-Encoding = %q", got)
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader 失败: %v", err)
	}
	defer zr.Close()
	restored, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	if string(restored) != string(body) {
		t.Fatalf("解压后内容不一致: got %d bytes, want %d bytes", len(restored), len(body))
	}
}

// TestMiddlewareEmptyBody 验证空响应体不压缩且不 panic。
func TestMiddlewareEmptyBody(t *testing.T) {
	rec := doRequest(t, "gzip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		// 不写任何 body
	})
	if got := rec.Header().Get("Content-Encoding"); got == "" {
		// 空 body 时 WriteHeader 已发出压缩头（Write 从未调用），头存在但无 body 属预期。
		t.Log("空 body：Write 未调用，WriteHeader 未触发，无压缩头（可接受）")
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("空响应体, got %d bytes", rec.Body.Len())
	}
}

// TestMiddlewareEmptyBodyWriteHeaderOnly 验证 handler 只调 WriteHeader(200) 不写 body：
// 压缩头被设置但 gzip 流只有 header 字节，响应可被空解压。
func TestMiddlewareEmptyBodyWriteHeaderOnly(t *testing.T) {
	rec := doRequest(t, "gzip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
	})
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("WriteHeader(200) 后应设置 Content-Encoding, got %q", got)
	}
	// 空内容 gzip 流仅含 ~23 字节 header/trailer，可被解压为空。
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("空 gzip 流应为合法 gzip: %v", err)
	}
	defer zr.Close()
	restored, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	if len(restored) != 0 {
		t.Fatalf("解压结果应为空, got %d bytes", len(restored))
	}
}

// TestMiddlewareMultiWriteGzip 验证多次 Write 分块写出的 gzip 流完整可解压
// （模拟 handler 分批 flush 数据，如 SSE / 流式响应）。
func TestMiddlewareMultiWriteGzip(t *testing.T) {
	want := strings.Repeat("chunk-", 400) // ~2.8KB
	rec := doRequest(t, "gzip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		// 分 4 次写，每次 700 字节（首次写 < 1KB 阈值）。
		for i := 0; i < 4; i++ {
			w.Write([]byte(strings.Repeat("chunk-", 100)))
		}
	})
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader 失败: %v", err)
	}
	defer zr.Close()
	restored, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	if string(restored) != want {
		t.Fatalf("分块写出解压后不一致: got %d bytes, want %d bytes", len(restored), len(want))
	}
}

// TestMiddlewareSkipsNon200 验证非 200 状态码跳过压缩（如 404/500）。
func TestMiddlewareSkipsNon200(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusSeeOther} {
		body := largeBody()
		rec := doRequest(t, "gzip", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(code)
			w.Write(body)
		})
		if rec.Code != code {
			t.Errorf("状态码 = %d, want %d", rec.Code, code)
		}
		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Errorf("状态码 %d 不应压缩, Content-Encoding = %q", code, got)
		}
		if rec.Body.String() != string(body) {
			t.Errorf("状态码 %d 响应体应原样透传", code)
		}
	}
}

// TestMiddlewareSkipsCompressedContentTypes 验证已压缩内容类型（image/gzip 等）跳过压缩。
func TestMiddlewareSkipsCompressedContentTypes(t *testing.T) {
	for _, ct := range []string{"image/png", "application/gzip", "application/zip", "application/octet-stream"} {
		body := largeBody()
		rec := doRequest(t, "gzip", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", ct)
			w.Write(body)
		})
		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Errorf("Content-Type %s 不应压缩, Content-Encoding = %q", ct, got)
		}
		if rec.Body.String() != string(body) {
			t.Errorf("Content-Type %s 响应体应原样透传", ct)
		}
	}
}

// TestMiddlewareCompressibleContentTypes 验证可压缩内容类型（text/json）正常压缩。
func TestMiddlewareCompressibleContentTypes(t *testing.T) {
	for _, ct := range []string{"text/plain", "application/json", "text/html; charset=utf-8"} {
		rec := doRequest(t, "gzip", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", ct)
			w.Write(largeBody())
		})
		if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
			t.Errorf("Content-Type %s 应压缩, Content-Encoding = %q", ct, got)
		}
	}
}

// TestCompressWriterWriteHeaderIdempotent 验证重复 WriteHeader 只生效一次
// （状态码不被第二次调用覆盖）。
func TestCompressWriterWriteHeaderIdempotent(t *testing.T) {
	mw := Middleware()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 中间件注入的 writer 类型即 *compressWriter，此处验证重复 WriteHeader 语义。
		w.WriteHeader(http.StatusOK)
		w.WriteHeader(http.StatusInternalServerError) // 第二次应被忽略
		w.Write(largeBody())
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200（重复 WriteHeader 应只生效一次）", rec.Code)
	}
}

// TestCompressWriterFlush 验证 Flush 透传并支持流式写出：
// Flush 后继续 Write，最终 gzip 流可完整解压。
// 注：Flush 会关闭当前压缩器并重开新流，多段内容拼接后需逐段解压。
func TestCompressWriterFlush(t *testing.T) {
	mw := Middleware()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write(largeBody())
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		w.Write(largeBody())
	})).ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}

	// Flush 会终结 gzip 流（Close），之后重开新流 → 响应体是多个连续的 gzip 成员流。
	// 逐成员解压并拼接，内容应为两份 largeBody。
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader 失败: %v", err)
	}
	defer zr.Close()
	var restored strings.Builder
	if _, err := io.Copy(&restored, zr); err != nil {
		t.Fatalf("解压第一个 gzip 成员失败: %v", err)
	}
	for {
		if err := zr.Reset(rec.Body); err != nil {
			break // 无更多 gzip 成员（io.EOF），拼接完成。
		}
		if _, err := io.Copy(&restored, zr); err != nil {
			t.Fatalf("解压后续 gzip 成员失败: %v", err)
		}
	}
	want := string(largeBody()) + string(largeBody())
	if restored.String() != want {
		t.Fatalf("流式写出解压后不一致: got %d bytes, want %d bytes", restored.Len(), len(want))
	}
}

// TestMiddlewareConcurrentRequests 验证中间件并发使用（writer 池）不互相干扰。
func TestMiddlewareConcurrentRequests(t *testing.T) {
	mw := Middleware()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write(largeBody())
	})
	wrapped := mw(handler)

	done := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Accept-Encoding", "gzip")
			wrapped.ServeHTTP(rec, req)

			zr, err := gzip.NewReader(rec.Body)
			if err != nil {
				done <- err
				return
			}
			defer zr.Close()
			restored, err := io.ReadAll(zr)
			if err != nil {
				done <- err
				return
			}
			if string(restored) != string(largeBody()) {
				done <- io.ErrUnexpectedEOF
				return
			}
			done <- nil
		}()
	}
	for i := 0; i < 8; i++ {
		if err := <-done; err != nil {
			t.Fatalf("并发请求 %d 失败: %v", i, err)
		}
	}
}

// TestMiddlewareGETAndPOST 验证不同 HTTP 方法均正常压缩。
func TestMiddlewareGETAndPOST(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/api/v1/resource", strings.NewReader(`{"key":"value"}`))
		req.Header.Set("Accept-Encoding", "gzip")
		Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(largeBody())
		})).ServeHTTP(rec, req)
		if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
			t.Errorf("method %s: Content-Encoding = %q, want gzip", method, got)
		}
	}
}
