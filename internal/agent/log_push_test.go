// log_push_test.go 测试 LogPusher（P2-B4 task 270）。
//
// 覆盖：
//   - NewLogPusher 构造与参数校验。
//   - tailFile 尾随临时文件读取新增行。
//   - 正则过滤。
//   - flush Loki / ES 报文格式（httptest.Server 模拟后端）。
//   - 缓冲区满触发 flush。
//   - Stop 后 flush 剩余缓冲。
package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestLogPusher_New 验证构造与参数校验。
func TestLogPusher_New(t *testing.T) {
	t.Run("valid loki", func(t *testing.T) {
		p, err := NewLogPusher([]string{"/var/log/syslog"}, "", "http://loki:3100/loki/api/v1/push", "loki", "t1", "h1")
		if err != nil {
			t.Fatalf("构造失败: %v", err)
		}
		if p.backend != "loki" || p.batchSize != defaultLogPushBatchSize || p.flushInterval != defaultLogPushFlush {
			t.Fatalf("默认参数不正确: %+v", p)
		}
		if p.pattern != nil {
			t.Fatalf("空 pattern 应为 nil")
		}
	})
	t.Run("valid es with pattern", func(t *testing.T) {
		p, err := NewLogPusher([]string{"/a.log"}, "ERROR.*", "http://es:9200/_bulk", "es", "", "")
		if err != nil {
			t.Fatalf("构造失败: %v", err)
		}
		if p.pattern == nil || !p.pattern.MatchString("ERROR something") {
			t.Fatalf("pattern 未正确编译")
		}
	})
	t.Run("empty files", func(t *testing.T) {
		if _, err := NewLogPusher(nil, "", "http://x", "loki", "", ""); err == nil {
			t.Fatalf("空 files 应报错")
		}
	})
	t.Run("empty endpoint", func(t *testing.T) {
		if _, err := NewLogPusher([]string{"/a"}, "", "", "loki", "", ""); err == nil {
			t.Fatalf("空 endpoint 应报错")
		}
	})
	t.Run("invalid backend", func(t *testing.T) {
		if _, err := NewLogPusher([]string{"/a"}, "", "http://x", "kafka", "", ""); err == nil {
			t.Fatalf("非法 backend 应报错")
		}
	})
	t.Run("invalid regex", func(t *testing.T) {
		if _, err := NewLogPusher([]string{"/a"}, "(unclosed", "http://x", "loki", "", ""); err == nil {
			t.Fatalf("非法正则应报错")
		}
	})
}

// TestLogPusher_TailFile 创建临时文件，写入内容后启动 tail，验证读取新增行。
//
// 由于 tailFile 从文件末尾开始读，测试先创建空文件、启动 tail，再追加内容。
func TestLogPusher_TailFile(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "app.log")
	if err := os.WriteFile(logFile, []byte(""), 0o644); err != nil {
		t.Fatalf("创建文件失败: %v", err)
	}

	// 用 httptest.Server 收集推送的行。
	var mu sync.Mutex
	var gotLines []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Streams []struct {
				Values [][2]string `json:"values"`
			} `json:"streams"`
		}
		_ = json.Unmarshal(body, &req)
		mu.Lock()
		for _, s := range req.Streams {
			for _, v := range s.Values {
				gotLines = append(gotLines, v[1])
			}
		}
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	p, err := NewLogPusher([]string{logFile}, "", srv.URL, "loki", "t1", "h1")
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	p.batchSize = 2 // 小批快速触发 flush

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)
	// 等待 tail goroutine 启动并 seek 到文件末尾，避免追加内容被 seek 跳过。
	time.Sleep(200 * time.Millisecond)

	// 追加 3 行（应触发一次 batch=2 flush + 剩余 1 行）。
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("打开追加失败: %v", err)
	}
	_, _ = f.WriteString("line1\nline2\nline3\n")
	_ = f.Close()

	// 等待推送到达（最多 2s）。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(gotLines)
		mu.Unlock()
		if n >= 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(gotLines) < 3 {
		t.Fatalf("期望收到 >=3 行，实际 %d: %v", len(gotLines), gotLines)
	}
	// 验证内容前 3 行匹配写入。
	want := []string{"line1", "line2", "line3"}
	for i, w := range want {
		if gotLines[i] != w {
			t.Fatalf("第 %d 行期望 %q 实际 %q", i, w, gotLines[i])
		}
	}
}

// TestLogPusher_RegexFilter 验证正则过滤：仅匹配行被推送。
func TestLogPusher_RegexFilter(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "filter.log")
	_ = os.WriteFile(logFile, []byte(""), 0o644)

	var mu sync.Mutex
	var gotLines []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Streams []struct {
				Values [][2]string `json:"values"`
			} `json:"streams"`
		}
		_ = json.Unmarshal(body, &req)
		mu.Lock()
		for _, s := range req.Streams {
			for _, v := range s.Values {
				gotLines = append(gotLines, v[1])
			}
		}
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	// 仅匹配 ERROR 开头的行。
	p, err := NewLogPusher([]string{logFile}, "^ERROR", srv.URL, "loki", "", "")
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	p.batchSize = 10
	p.flushInterval = 100 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)
	time.Sleep(200 * time.Millisecond) // 等待 tail 就绪

	f, _ := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString("INFO hello\nERROR boom\nWARN x\nERROR again\n")
	_ = f.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(gotLines)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(gotLines) != 2 {
		t.Fatalf("期望过滤后 2 行，实际 %d: %v", len(gotLines), gotLines)
	}
	if gotLines[0] != "ERROR boom" || gotLines[1] != "ERROR again" {
		t.Fatalf("过滤结果不匹配: %v", gotLines)
	}
}

// TestLogPusher_FlushLoki 验证 Loki push 报文格式与 header。
func TestLogPusher_FlushLoki(t *testing.T) {
	var gotBody []byte
	var gotTenant string
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotTenant = r.Header.Get("X-Scope-OrgID")
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	p, err := NewLogPusher([]string{"/tmp/x.log"}, "", srv.URL, "loki", "tenant-A", "host-X")
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	// 直接塞两条到缓冲区并 flush。
	p.mu.Lock()
	p.buffer = append(p.buffer,
		LogEntry{Timestamp: time.Unix(0, 1700000000000000000), File: "/tmp/x.log", Line: "hello", Labels: nil},
		LogEntry{Timestamp: time.Unix(0, 1700000001000000000), File: "/tmp/x.log", Line: "world", Labels: nil},
	)
	p.mu.Unlock()

	if err := p.flush(); err != nil {
		t.Fatalf("flush 失败: %v", err)
	}

	if gotContentType != "application/json" {
		t.Fatalf("Content-Type 期望 application/json 实际 %q", gotContentType)
	}
	if gotTenant != "tenant-A" {
		t.Fatalf("X-Scope-OrgID 期望 tenant-A 实际 %q", gotTenant)
	}
	// 解析 JSON 验证结构。
	var req struct {
		Streams []struct {
			Stream map[string]string `json:"stream"`
			Values [][2]string       `json:"values"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("解析 Loki 报文失败: %v\nbody: %s", err, string(gotBody))
	}
	if len(req.Streams) != 1 {
		t.Fatalf("期望 1 个 stream 实际 %d", len(req.Streams))
	}
	if req.Streams[0].Stream["host"] != "host-X" || req.Streams[0].Stream["tenant"] != "tenant-A" {
		t.Fatalf("stream 标签不正确: %v", req.Streams[0].Stream)
	}
	if len(req.Streams[0].Values) != 2 {
		t.Fatalf("期望 2 个 value 实际 %d", len(req.Streams[0].Values))
	}
	if req.Streams[0].Values[0][1] != "hello" || req.Streams[0].Values[1][1] != "world" {
		t.Fatalf("values 不匹配: %v", req.Streams[0].Values)
	}
}

// TestLogPusher_FlushES 验证 ES _bulk NDJSON 报文格式。
func TestLogPusher_FlushES(t *testing.T) {
	var gotBody []byte
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p, err := NewLogPusher([]string{"/var/log/app.log"}, "", srv.URL, "es", "tES", "hES")
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	ts := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	p.mu.Lock()
	p.buffer = append(p.buffer,
		LogEntry{Timestamp: ts, File: "/var/log/app.log", Line: "es-line-1", Labels: nil},
		LogEntry{Timestamp: ts, File: "/var/log/app.log", Line: "es-line-2", Labels: nil},
	)
	p.mu.Unlock()

	if err := p.flush(); err != nil {
		t.Fatalf("flush 失败: %v", err)
	}

	if gotContentType != "application/x-ndjson" {
		t.Fatalf("Content-Type 期望 application/x-ndjson 实际 %q", gotContentType)
	}
	// NDJSON 应有 4 行（2 条 × 2 行）。
	lines := strings.Split(strings.TrimRight(string(gotBody), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("期望 4 行 NDJSON 实际 %d: %s", len(lines), string(gotBody))
	}
	// 第 1、3 行为 action。
	var action1, action2 map[string]map[string]string
	if err := json.Unmarshal([]byte(lines[0]), &action1); err != nil {
		t.Fatalf("解析 action1 失败: %v", err)
	}
	if action1["index"]["_index"] != "opsmesh-logs" {
		t.Fatalf("action1 _index 不正确: %v", action1)
	}
	if err := json.Unmarshal([]byte(lines[2]), &action2); err != nil {
		t.Fatalf("解析 action2 失败: %v", err)
	}
	// 第 2、4 行为 doc。
	var doc1, doc2 map[string]interface{}
	if err := json.Unmarshal([]byte(lines[1]), &doc1); err != nil {
		t.Fatalf("解析 doc1 失败: %v", err)
	}
	if doc1["message"] != "es-line-1" || doc1["host"] != "hES" || doc1["tenant"] != "tES" {
		t.Fatalf("doc1 字段不正确: %v", doc1)
	}
	if err := json.Unmarshal([]byte(lines[3]), &doc2); err != nil {
		t.Fatalf("解析 doc2 失败: %v", err)
	}
	if doc2["message"] != "es-line-2" {
		t.Fatalf("doc2 message 不正确: %v", doc2)
	}
}

// TestLogPusher_BatchSize 验证缓冲区满触发 flush。
func TestLogPusher_BatchSize(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "batch.log")
	_ = os.WriteFile(logFile, []byte(""), 0o644)

	var mu sync.Mutex
	var flushCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		flushCount++
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	p, err := NewLogPusher([]string{logFile}, "", srv.URL, "loki", "", "")
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	p.batchSize = 3
	p.flushInterval = 10 * time.Second // 周期 flush 拉长，确保只测 batch 触发

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)
	time.Sleep(200 * time.Millisecond) // 等待 tail 就绪

	// 写入 6 行，应触发 2 次 batch flush（每 3 行一次）。
	f, _ := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0o644)
	for i := 0; i < 6; i++ {
		_, _ = f.WriteString("b\n")
	}
	_ = f.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := flushCount
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if flushCount < 2 {
		t.Fatalf("期望至少 2 次 batch flush，实际 %d", flushCount)
	}
}

// TestLogPusher_Stop 验证 Stop 后 flush 剩余缓冲并退出。
func TestLogPusher_Stop(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "stop.log")
	_ = os.WriteFile(logFile, []byte(""), 0o644)

	var mu sync.Mutex
	var totalLines int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Streams []struct {
				Values [][2]string `json:"values"`
			} `json:"streams"`
		}
		_ = json.Unmarshal(body, &req)
		mu.Lock()
		for _, s := range req.Streams {
			totalLines += len(s.Values)
		}
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	p, err := NewLogPusher([]string{logFile}, "", srv.URL, "loki", "", "")
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	p.batchSize = 100 // 大批，确保 Stop 前不触发 batch flush
	p.flushInterval = 1 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)
	time.Sleep(200 * time.Millisecond) // 等待 tail 就绪

	// 写 3 行（< batchSize，依赖 Stop 时 flush）。
	f, _ := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString("s1\ns2\ns3\n")
	_ = f.Close()

	// 等待 tail 读取到缓冲区。
	time.Sleep(300 * time.Millisecond)
	if err := p.Stop(); err != nil {
		t.Fatalf("Stop 失败: %v", err)
	}
	// 等 Run 退出 + 最终 flush。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := totalLines
		mu.Unlock()
		if n >= 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if totalLines < 3 {
		t.Fatalf("Stop 后期望 flush >=3 行，实际 %d", totalLines)
	}
	// 验证 Stop 幂等。
	if err := p.Stop(); err != nil {
		t.Fatalf("二次 Stop 应幂等无错，实际: %v", err)
	}
}
