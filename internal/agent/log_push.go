// Package agent 内的 log_push.go 实现 P2-B4 task 270 日志采集 agent 端推送。
//
// LogPusher 尾随（tail -f）一组日志文件，按可选正则过滤后批量推送到后端：
//   - Loki：POST JSON 到 /loki/api/v1/push（流式格式）。
//   - ES  ：POST NDJSON 到 /_bulk（bulk action + doc 双行）。
//
// 设计要点：
//   - 每个文件一个 tailFile goroutine，从文件末尾开始读取新增行（不重读历史）。
//   - 主循环 Run 起动 tail goroutine 并按 flushInterval 周期 flush 缓冲。
//   - flush 失败时缓冲保留，下次重试（不丢失数据）。
//   - Stop 关闭 stopCh 并 flush 剩余缓冲后返回。
package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"opsmesh/internal/logx"
)

// LogPusher 日志采集推送器：尾随日志文件，正则过滤，批量推送到指定 endpoint。
//
// 字段语义：
//   - files          要尾随的日志文件路径列表（tail -f）。
//   - pattern        正则过滤模式（空=不过滤，全部推送）；非空时仅推送匹配行。
//   - endpoint       推送目标完整 URL（Loki /api/v1/push 或 ES /_bulk）。
//   - backend        "loki" | "es"，决定推送报文格式。
//   - batchSize      缓冲区满阈值，达到即触发 flush（默认 100 行）。
//   - flushInterval  周期 flush 间隔（默认 5s），即使缓冲未满也定时推送。
//   - tenantID/hostname 注入到每条日志的标签，便于后端按租户/主机检索。
//   - buffer         待推送日志缓冲区；mu 保护其并发读写。
//   - stopCh         Stop 时关闭以通知 Run 退出。
type LogPusher struct {
	files         []string       // 要尾随的日志文件路径列表
	pattern       *regexp.Regexp // 正则过滤模式（空=不过滤，全部推送）
	endpoint      string         // 推送目标 endpoint（Loki /api/v1/push 或 ES /_bulk）
	backend       string         // "loki" | "es"
	batchSize     int            // 批量大小（默认 100 行）
	flushInterval time.Duration  // 刷新间隔（默认 5s）
	tenantID      string         // 租户标签
	hostname      string         // 主机名标签
	mu            sync.Mutex     // 保护 buffer 并发读写
	buffer        []LogEntry     // 缓冲区
	stopCh        chan struct{}  // Stop 信号
	stopped       bool           // 是否已 Stop（防止重复关闭）
	httpClient    *http.Client   // 推送用 HTTP 客户端（10s 超时）
}

// LogEntry 单条待推送日志。
type LogEntry struct {
	Timestamp time.Time         // 行产生时间（采集时刻）
	File      string            // 来源文件路径
	Line      string            // 日志正文（已去除行尾换行）
	Labels    map[string]string // 额外标签（host/tenant/file 等，推送时合并）
}

// 默认参数。
const (
	defaultLogPushBatchSize = 100                    // 默认批大小
	defaultLogPushFlush     = 5 * time.Second        // 默认 flush 间隔
	logPushTailPoll         = 100 * time.Millisecond // tail 到 EOF 时的轮询间隔
	logPushHTTPTimeout      = 10 * time.Second       // HTTP 推送超时
)

// NewLogPusher 构造 LogPusher。
//
// 参数：
//   - files     要尾随的文件路径列表（空切片返回 error，避免空跑）。
//   - pattern   正则过滤（空串=不过滤）。
//   - endpoint  推送目标 URL（空串返回 error）。
//   - backend   "loki" | "es"（其他值返回 error）。
//   - tenantID  租户标签（可空）。
//   - hostname  主机名标签（可空）。
//
// pattern 非空时编译为 *regexp.Regexp，编译失败返回 error。
func NewLogPusher(files []string, pattern, endpoint, backend, tenantID, hostname string) (*LogPusher, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("log_push: files 列表为空")
	}
	if endpoint == "" {
		return nil, fmt.Errorf("log_push: endpoint 为空")
	}
	switch backend {
	case "loki", "es":
	default:
		return nil, fmt.Errorf("log_push: 非法 backend=%q（应为 loki | es）", backend)
	}
	var re *regexp.Regexp
	if pattern != "" {
		var err error
		re, err = regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("log_push: 正则编译失败 %q: %w", pattern, err)
		}
	}
	return &LogPusher{
		files:         files,
		pattern:       re,
		endpoint:      endpoint,
		backend:       backend,
		batchSize:     defaultLogPushBatchSize,
		flushInterval: defaultLogPushFlush,
		tenantID:      tenantID,
		hostname:      hostname,
		buffer:        make([]LogEntry, 0, defaultLogPushBatchSize),
		stopCh:        make(chan struct{}),
		httpClient:    &http.Client{Timeout: logPushHTTPTimeout},
	}, nil
}

// Run 启动采集循环：对每个文件启动 tail goroutine，定期 flush 到 endpoint。
//
// 阻塞直到 ctx.Done 或 Stop 被调用。返回 nil 表示正常退出。
// 单个 tail goroutine 异常不影响其他文件采集（仅记录日志）。
func (p *LogPusher) Run(ctx context.Context) error {
	logx.Info(ctx, "LogPusher 启动",
		"files", p.files, "backend", p.backend,
		"endpoint", p.endpoint, "batchSize", p.batchSize, "flushInterval", p.flushInterval)

	var wg sync.WaitGroup
	// 为每个文件启动一个 tail goroutine。
	for _, file := range p.files {
		wg.Add(1)
		go func(f string) {
			defer wg.Done()
			p.tailFile(ctx, f)
		}(file)
	}

	// 周期 flush goroutine。
	flushDone := make(chan struct{})
	go func() {
		defer close(flushDone)
		ticker := time.NewTicker(p.flushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-p.stopCh:
				return
			case <-ticker.C:
				if err := p.flush(); err != nil {
					logx.Warn(ctx, "LogPusher 周期 flush 失败", "error", err.Error())
				}
			}
		}
	}()

	// 等待退出信号。
	select {
	case <-ctx.Done():
	case <-p.stopCh:
	}

	// 退出前 flush 剩余缓冲（尽力而为，失败仅记录日志）。
	if err := p.flush(); err != nil {
		logx.Warn(ctx, "LogPusher 退出前 flush 失败", "error", err.Error())
	}
	wg.Wait()
	<-flushDone
	logx.Info(ctx, "LogPusher 已退出")
	return nil
}

// Stop 停止采集（关闭所有 tail goroutine，flush 剩余缓冲）。
//
// 幂等：多次调用安全。关闭 stopCh 后 Run 的 select 会退出并触发最终 flush。
func (p *LogPusher) Stop() error {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return nil
	}
	p.stopped = true
	p.mu.Unlock()
	close(p.stopCh)
	return nil
}

// appendEntry 向缓冲区追加一条日志，缓冲满时触发 flush。
//
// flush 失败时缓冲保留（不丢失数据），下次重试。返回 flush 错误仅用于上层日志。
func (p *LogPusher) appendEntry(entry LogEntry) error {
	needFlush := false
	p.mu.Lock()
	p.buffer = append(p.buffer, entry)
	if len(p.buffer) >= p.batchSize {
		needFlush = true
	}
	p.mu.Unlock()
	if needFlush {
		return p.flush()
	}
	return nil
}

// tailFile 尾随单个文件（类似 tail -f）：从文件末尾开始读取新增行。
//
// 逻辑：
//  1. 打开文件，seek 到末尾（不重读历史内容）。
//  2. bufio.Scanner 逐行读取。
//  3. 读到 EOF 时 sleep 100ms 再试（非阻塞尾随）。
//  4. 每行用正则过滤（若配置了 pattern）；不匹配则跳过。
//  5. 添加到缓冲区，缓冲满时触发 flush。
//  6. ctx.Done 或 stopCh 关闭时退出。
//
// 文件打开失败/读取错误仅记录日志并退出该 goroutine（不影响其他文件）。
func (p *LogPusher) tailFile(ctx context.Context, file string) {
	f, err := os.Open(file)
	if err != nil {
		logx.Warn(ctx, "LogPusher 打开文件失败", "file", file, "error", err.Error())
		return
	}
	defer f.Close()
	// seek 到文件末尾，仅采集启动后新增的行。
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		logx.Warn(ctx, "LogPusher seek 末尾失败", "file", file, "error", err.Error())
		return
	}

	reader := bufio.NewReaderSize(f, 64*1024)

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopCh:
			return
		default:
		}
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			// 去除行尾换行（含 \r\n）。
			line = strings.TrimRight(line, "\r\n")
			if line != "" {
				// 正则过滤：配置了 pattern 时仅推送匹配行。
				if p.pattern == nil || p.pattern.MatchString(line) {
					entry := LogEntry{
						Timestamp: time.Now(),
						File:      file,
						Line:      line,
						Labels: map[string]string{
							"host":   p.hostname,
							"tenant": p.tenantID,
							"file":   file,
						},
					}
					if ferr := p.appendEntry(entry); ferr != nil {
						logx.Warn(ctx, "LogPusher appendEntry 触发 flush 失败", "file", file, "error", ferr.Error())
					}
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				// 到达 EOF：等待 100ms 后重试。
				// bufio.Reader 在 EOF 后会缓存 err 不再读取底层文件，
				// 故重建 Reader（基于同一 *os.File，不重 Open）；底层文件指针位置保持连续，
				// 新增内容会在下次 ReadString 读到。半行内容（无 \n 结尾）会丢失，
				// 对日志采集场景（行通常以 \n 结尾）可接受。
				select {
				case <-ctx.Done():
					return
				case <-p.stopCh:
					return
				case <-time.After(logPushTailPoll):
				}
				reader.Reset(f)
				continue
			}
			logx.Warn(ctx, "LogPusher 读取错误", "file", file, "error", err.Error())
			return
		}
	}
}

// flush 将缓冲区日志批量推送到 endpoint。
//
// 缓冲区为空时直接返回 nil（不发空请求）。
// 取出缓冲区当前内容后立即清空（避免长时间持锁）；推送失败时把取出的内容回填缓冲区，
// 下次 flush 重试（不丢失数据）。
//
// backend 决定报文格式：
//   - loki：POST JSON {"streams":[{"stream":{labels},"values":[[ts, line], ...]}]}
//   - es   ：POST NDJSON _bulk（每条两行：action + doc）
func (p *LogPusher) flush() error {
	p.mu.Lock()
	if len(p.buffer) == 0 {
		p.mu.Unlock()
		return nil
	}
	batch := p.buffer
	p.buffer = make([]LogEntry, 0, p.batchSize)
	p.mu.Unlock()

	var body []byte
	var contentType string
	switch p.backend {
	case "loki":
		body, contentType = p.buildLokiPayload(batch)
	case "es":
		body, contentType = p.buildESBulk(batch)
	default:
		// 不应发生（构造时已校验），防御性处理。
		return fmt.Errorf("log_push flush: 未知 backend=%q", p.backend)
	}

	req, err := http.NewRequest(http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		p.requeue(batch) // 请求构造失败，回填缓冲区
		return fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	if p.tenantID != "" && p.backend == "loki" {
		// Loki 多租户头：X-Scope-OrgID。
		req.Header.Set("X-Scope-OrgID", p.tenantID)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.requeue(batch)
		return fmt.Errorf("推送请求失败: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 非 2xx 视为失败，回填缓冲区下次重试。
		p.requeue(batch)
		return fmt.Errorf("推送返回非 2xx: status=%d body=%s", resp.StatusCode, string(respBody))
	}
	return nil
}

// requeue 把取出的 batch 回填缓冲区（推送失败时调用，避免丢数据）。
func (p *LogPusher) requeue(batch []LogEntry) {
	p.mu.Lock()
	// 把失败批次 prepend 到缓冲区头部（保证顺序）。
	newBuf := make([]LogEntry, 0, len(batch)+len(p.buffer))
	newBuf = append(newBuf, batch...)
	newBuf = append(newBuf, p.buffer...)
	p.buffer = newBuf
	p.mu.Unlock()
}

// buildLokiPayload 构造 Loki push API 报文。
//
// 格式：{"streams":[{"stream":{"host":"...","tenant":"...","file":"..."},"values":[[ts, line], ...]}]}
// ts 为纳秒精度字符串（Loki 要求）。
// 同一文件 + 同一标签集合的行合并到同一 stream；此处简化为按文件分组。
func (p *LogPusher) buildLokiPayload(batch []LogEntry) ([]byte, string) {
	type lokiValue [2]string // [ts, line]
	type lokiStream struct {
		Stream map[string]string `json:"stream"`
		Values []lokiValue       `json:"values"`
	}
	type lokiPushReq struct {
		Streams []lokiStream `json:"streams"`
	}

	// 按文件分组（标签相同），减少 stream 数量。
	groups := make(map[string]*lokiStream)
	order := make([]string, 0) // 保持稳定顺序
	for _, e := range batch {
		g, ok := groups[e.File]
		if !ok {
			g = &lokiStream{
				Stream: map[string]string{
					"host":   p.hostname,
					"tenant": p.tenantID,
					"file":   e.File,
				},
				Values: make([]lokiValue, 0),
			}
			groups[e.File] = g
			order = append(order, e.File)
		}
		g.Values = append(g.Values, lokiValue{
			fmt.Sprintf("%d", e.Timestamp.UnixNano()),
			e.Line,
		})
	}
	req := lokiPushReq{Streams: make([]lokiStream, 0, len(order))}
	for _, f := range order {
		req.Streams = append(req.Streams, *groups[f])
	}
	body, _ := json.Marshal(req)
	return body, "application/json"
}

// buildESBulk 构造 Elasticsearch _bulk NDJSON 报文。
//
// 每条日志两行：
//   {"index": {"_index": "opsmesh-logs"}}
//   {"@timestamp": "...", "host": "...", "tenant": "...", "file": "...", "message": "..."}
//
// 索引名固定 opsmesh-logs（与 config.ESIndex 默认一致；如需自定义可在 endpoint 路径中体现）。
func (p *LogPusher) buildESBulk(batch []LogEntry) ([]byte, string) {
	const esIndex = "opsmesh-logs"
	var buf bytes.Buffer
	for _, e := range batch {
		// action 行。
		fmt.Fprintf(&buf, `{"index": {"_index": %q}}`+"\n", esIndex)
		// doc 行。
		doc := map[string]interface{}{
			"@timestamp": e.Timestamp.UTC().Format(time.RFC3339Nano),
			"host":       p.hostname,
			"tenant":     p.tenantID,
			"file":       e.File,
			"message":    e.Line,
		}
		b, _ := json.Marshal(doc)
		buf.Write(b)
		buf.WriteByte('\n')
	}
	return buf.Bytes(), "application/x-ndjson"
}

// ensureNoTrailingWhitespace 防御性辅助：确保行内无尾随空白（仅用于测试断言辅助，未在生产路径调用）。
//
// 保留以备未来扩展校验；当前未使用，避免 unused 警告用下划线导入占位。
var _ = strings.TrimSpace
