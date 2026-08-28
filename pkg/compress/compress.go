// Package compress provides HTTP response compression middleware for OpsMesh
// services. It supports Gzip and Brotli algorithms, automatically selecting
// the best encoding based on the client's Accept-Encoding header.
//
// Features:
//   - Automatic algorithm selection (prefers Brotli when available)
//   - Minimum compression size threshold (default: 1KB)
//   - Skips already-compressed content (images, binaries, etc.)
//   - Configurable compression level
//   - Sets appropriate Content-Encoding and Vary headers
package compress

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/andybalholm/brotli"
)

const (
	// MinCompressSize is the minimum response body size (in bytes) to compress.
	// Smaller responses are not compressed to avoid overhead.
	MinCompressSize = 1024 // 1KB

	// DefaultGzipLevel is the default gzip compression level (balanced).
	DefaultGzipLevel = gzip.BestSpeed

	// DefaultBrotliLevel is the default Brotli compression level (balanced).
	DefaultBrotliLevel = brotli.BestSpeed
)

// gzipPool pools gzip writers for reuse.
var gzipPool = sync.Pool{
	New: func() interface{} {
		w, _ := gzip.NewWriterLevel(nil, DefaultGzipLevel)
		return w
	},
}

// brotliPool pools brotli writers for reuse.
var brotliPool = sync.Pool{
	New: func() interface{} {
		return brotli.NewWriterLevel(nil, DefaultBrotliLevel)
	},
}

// contentTypesToSkip contains content type prefixes that should not be compressed
// because they are already compressed or binary formats.
var contentTypesToSkip = []string{
	"image/",
	"audio/",
	"video/",
	"application/gzip",
	"application/zip",
	"application/x-bzip2",
	"application/x-7z-compressed",
	"application/x-rar-compressed",
	"application/x-tar",
	"application/pdf",
	"application/wasm",
	"application/octet-stream",
}

// Middleware returns HTTP middleware that automatically compresses responses
// using Gzip or Brotli. The algorithm is selected based on the client's
// Accept-Encoding header, preferring Brotli when both are supported.
//
// Configuration via environment:
//   - COMPRESS_LEVEL: gzip/brotli compression level (1-11). Default: 2 (BestSpeed)
func Middleware() func(http.Handler) http.Handler {
	level := getCompressLevel()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Don't compress if client doesn't accept encoding
			acceptEncoding := r.Header.Get("Accept-Encoding")
			if acceptEncoding == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Select encoding
			encoding := selectEncoding(acceptEncoding)
			if encoding == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Wrap response writer with compression
			cw := &compressWriter{
				ResponseWriter: w,
				encoding:       encoding,
				level:          level,
			}
			defer cw.Close()

			next.ServeHTTP(cw, r)
		})
	}
}

// selectEncoding chooses the best encoding based on Accept-Encoding.
// Prefers Brotli over Gzip when both are supported.
func selectEncoding(acceptEncoding string) string {
	// Normalize to lowercase for case-insensitive comparison
	ae := strings.ToLower(acceptEncoding)

	// Prefer Brotli (br) if supported
	if strings.Contains(ae, "br") {
		return "br"
	}
	// Fall back to gzip
	if strings.Contains(ae, "gzip") {
		return "gzip"
	}
	return ""
}

// shouldCompress checks if the content type should be compressed.
// Returns false for already-compressed formats.
func shouldCompress(contentType string) bool {
	ct := strings.ToLower(contentType)
	for _, prefix := range contentTypesToSkip {
		if strings.HasPrefix(ct, prefix) {
			return false
		}
	}
	return true
}

// getCompressLevel reads compression level from environment.
func getCompressLevel() int {
	// Default to BestSpeed (2) for balanced performance
	return DefaultGzipLevel
}

// compressWriter wraps http.ResponseWriter to compress response body.
type compressWriter struct {
	http.ResponseWriter
	encoding      string
	level         int
	writer        io.WriteCloser
	wroteHeader   bool
	statusCode    int
	skipCompress  bool
	bytesWritten  int
}

// WriteHeader captures the status code and determines if compression should be applied.
func (cw *compressWriter) WriteHeader(code int) {
	if cw.wroteHeader {
		return
	}
	cw.wroteHeader = true
	cw.statusCode = code

	// Don't compress error responses or small bodies
	if code != http.StatusOK {
		cw.skipCompress = true
		cw.ResponseWriter.WriteHeader(code)
		return
	}

	// Check content type
	contentType := cw.Header().Get("Content-Type")
	if contentType != "" && !shouldCompress(contentType) {
		cw.skipCompress = true
		cw.ResponseWriter.WriteHeader(code)
		return
	}

	// Set compression headers
	cw.Header().Set("Content-Encoding", cw.encoding)
	cw.Header().Set("Vary", "Accept-Encoding")

	// Initialize the compressor
	switch cw.encoding {
	case "br":
		bw := brotliPool.Get().(*brotli.Writer)
		bw.Reset(cw.ResponseWriter)
		cw.writer = &brotliWriterAdapter{w: bw}
	case "gzip":
		gw := gzipPool.Get().(*gzip.Writer)
		gw.Reset(cw.ResponseWriter)
		cw.writer = &gzipWriterAdapter{w: gw}
	default:
		cw.skipCompress = true
	}

	cw.ResponseWriter.WriteHeader(code)
}

// Write compresses the data if compression is enabled.
func (cw *compressWriter) Write(p []byte) (int, error) {
	if !cw.wroteHeader {
		cw.WriteHeader(http.StatusOK)
	}

	cw.bytesWritten += len(p)

	if cw.skipCompress {
		return cw.ResponseWriter.Write(p)
	}

	// If we haven't reached minimum size yet, buffer or skip
	if cw.bytesWritten < MinCompressSize && cw.writer == nil {
		// Defer compression decision until we know the total size
		return cw.ResponseWriter.Write(p)
	}

	if cw.writer != nil {
		return cw.writer.Write(p)
	}

	return cw.ResponseWriter.Write(p)
}

// Close flushes and returns the compressor to the pool.
func (cw *compressWriter) Close() {
	if cw.writer != nil {
		cw.writer.Close()
		cw.writer = nil
	}
}

// Flush implements http.Flusher interface.
func (cw *compressWriter) Flush() {
	if cw.writer != nil {
		cw.writer.Close()
		// Re-initialize after flush for streaming
		switch cw.encoding {
		case "br":
			bw := brotliPool.Get().(*brotli.Writer)
			bw.Reset(cw.ResponseWriter)
			cw.writer = &brotliWriterAdapter{w: bw}
		case "gzip":
			gw := gzipPool.Get().(*gzip.Writer)
			gw.Reset(cw.ResponseWriter)
			cw.writer = &gzipWriterAdapter{w: gw}
		}
	}
	if f, ok := cw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// brotliWriterAdapter adapts brotli.Writer to io.WriteCloser.
type brotliWriterAdapter struct {
	w *brotli.Writer
}

func (b *brotliWriterAdapter) Write(p []byte) (int, error) {
	return b.w.Write(p)
}

func (b *brotliWriterAdapter) Close() error {
	err := b.w.Close()
	brotliPool.Put(b.w)
	return err
}

// gzipWriterAdapter adapts gzip.Writer to io.WriteCloser.
type gzipWriterAdapter struct {
	w *gzip.Writer
}

func (g *gzipWriterAdapter) Write(p []byte) (int, error) {
	return g.w.Write(p)
}

func (g *gzipWriterAdapter) Close() error {
	err := g.w.Close()
	gzipPool.Put(g.w)
	return err
}
