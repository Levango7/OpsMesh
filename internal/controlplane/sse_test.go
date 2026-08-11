package controlplane

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opsmesh/internal/config"
	"opsmesh/internal/otelx"
	"opsmesh/internal/store"
)

// 本文件为 M3-2B SSE 实时推送的单元测试。
//
// 覆盖范围：
//   - handleEventsStream 响应头（Content-Type / Cache-Control / Connection / X-Accel-Buffering）
//   - hello 握手帧
//   - 事件推送格式（event: <type>\ndata: {...}\n\n）
//   - 多订阅者广播
//   - 连接关闭时清理订阅（防泄漏）
//   - POST 方法拒绝（405）
//   - publishEvent 无订阅者时快速返回
//
// 测试模式：用 httptest.Server 启动真实 HTTP server（支持 Flusher + 流式 Body），
// http.Get 获取流式 resp.Body，bufio.Reader 逐帧读取（帧以空行 \n 结束）。

// newSSETestServer 构造最小测试控制面（仅 SSE 相关字段初始化）。
func newSSETestServer() *Server {
	return &Server{
		store:     store.NewMemoryStore(),
		cfg:       &config.Config{},
		eventSubs: make(map[chan SSEEvent]struct{}),
	}
}

// readSSEFrame 从 bufio.Reader 读取一个 SSE 帧（以空行 \n 结束）。
// 返回帧的完整文本（含所有 event/data 行和结束空行）。
func readSSEFrame(br *bufio.Reader) (string, error) {
	var frame strings.Builder
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return frame.String(), err
		}
		frame.WriteString(line)
		// SSE 帧以空行结束（仅 "\n"）
		if line == "\n" {
			return frame.String(), nil
		}
	}
}

// TestSSE_ContentTypeAndHeaders 验证 SSE handler 返回标准 SSE 响应头。
func TestSSE_ContentTypeAndHeaders(t *testing.T) {
	s := newSSETestServer()
	ts := httptest.NewServer(http.HandlerFunc(s.handleEventsStream))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET SSE: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", cc)
	}
	if conn := resp.Header.Get("Connection"); conn != "keep-alive" {
		t.Fatalf("Connection = %q, want keep-alive", conn)
	}
	if ab := resp.Header.Get("X-Accel-Buffering"); ab != "no" {
		t.Fatalf("X-Accel-Buffering = %q, want no", ab)
	}
}

// TestSSE_HelloHandshake 验证连接建立后首帧为 hello 握手事件。
func TestSSE_HelloHandshake(t *testing.T) {
	s := newSSETestServer()
	ts := httptest.NewServer(http.HandlerFunc(s.handleEventsStream))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET SSE: %v", err)
	}
	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)
	frame, err := readSSEFrame(br)
	if err != nil {
		t.Fatalf("read hello frame: %v", err)
	}
	if !strings.Contains(frame, "event: hello") {
		t.Fatalf("hello frame missing 'event: hello': %q", frame)
	}
	if !strings.Contains(frame, "data: {}") {
		t.Fatalf("hello frame missing 'data: {}': %q", frame)
	}
}

// TestSSE_EventPushFormat 验证 publishEvent 推送的事件帧格式正确。
// 期望格式：event: task_status\ndata: {"type":"task_status","data":{...}}\n\n
func TestSSE_EventPushFormat(t *testing.T) {
	s := newSSETestServer()
	ts := httptest.NewServer(http.HandlerFunc(s.handleEventsStream))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET SSE: %v", err)
	}
	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)
	// 读 hello 握手帧
	if _, err := readSSEFrame(br); err != nil {
		t.Fatalf("read hello: %v", err)
	}

	// 从另一 goroutine 发布事件（模拟服务端状态变更）
	go func() {
		time.Sleep(50 * time.Millisecond)
		s.publishEvent(context.Background(), "task_status", "", map[string]string{
			"taskID":  "task-123",
			"status":  "running",
			"agentID": "agent-456",
		})
	}()

	frame, err := readSSEFrame(br)
	if err != nil {
		t.Fatalf("read event frame: %v", err)
	}
	if !strings.Contains(frame, "event: task_status") {
		t.Fatalf("frame missing 'event: task_status': %q", frame)
	}
	// data 行应包含 SSEEvent 信封的 JSON，含 type 和 data 字段
	if !strings.Contains(frame, `"type":"task_status"`) {
		t.Fatalf("frame missing type field: %q", frame)
	}
	if !strings.Contains(frame, `"taskID":"task-123"`) {
		t.Fatalf("frame missing taskID: %q", frame)
	}
	if !strings.Contains(frame, `"status":"running"`) {
		t.Fatalf("frame missing status: %q", frame)
	}
	if !strings.Contains(frame, `"agentID":"agent-456"`) {
		t.Fatalf("frame missing agentID: %q", frame)
	}
}

// TestSSE_BroadcastToMultiple 验证 publishEvent 广播到所有活跃订阅者。
func TestSSE_BroadcastToMultiple(t *testing.T) {
	s := newSSETestServer()
	ts := httptest.NewServer(http.HandlerFunc(s.handleEventsStream))
	defer ts.Close()

	// 建立两个 SSE 连接
	r1, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET SSE r1: %v", err)
	}
	defer r1.Body.Close()
	r2, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET SSE r2: %v", err)
	}
	defer r2.Body.Close()

	br1 := bufio.NewReader(r1.Body)
	br2 := bufio.NewReader(r2.Body)
	// 各读 hello 帧
	if _, err := readSSEFrame(br1); err != nil {
		t.Fatalf("read r1 hello: %v", err)
	}
	if _, err := readSSEFrame(br2); err != nil {
		t.Fatalf("read r2 hello: %v", err)
	}

	// 发布一条 alert_new 事件，两个订阅者都应收到（tenantID 为空 = 全局事件）
	s.publishEvent(context.Background(), "alert_new", "", map[string]string{"alertID": "alert-789"})

	f1, err := readSSEFrame(br1)
	if err != nil {
		t.Fatalf("read r1 event: %v", err)
	}
	f2, err := readSSEFrame(br2)
	if err != nil {
		t.Fatalf("read r2 event: %v", err)
	}
	if !strings.Contains(f1, "event: alert_new") {
		t.Fatalf("sub1 missing alert_new: %q", f1)
	}
	if !strings.Contains(f2, "event: alert_new") {
		t.Fatalf("sub2 missing alert_new: %q", f2)
	}
	if !strings.Contains(f1, `"alertID":"alert-789"`) {
		t.Fatalf("sub1 missing alertID: %q", f1)
	}
	if !strings.Contains(f2, `"alertID":"alert-789"`) {
		t.Fatalf("sub2 missing alertID: %q", f2)
	}
}

// TestSSE_UnsubscribeOnClose 验证客户端断开连接后订阅被清理（防泄漏）。
func TestSSE_UnsubscribeOnClose(t *testing.T) {
	s := newSSETestServer()
	ts := httptest.NewServer(http.HandlerFunc(s.handleEventsStream))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET SSE: %v", err)
	}

	br := bufio.NewReader(resp.Body)
	// 读 hello 帧（确保 handler 已进入订阅循环）
	if _, err := readSSEFrame(br); err != nil {
		t.Fatalf("read hello: %v", err)
	}

	// 短暂等待确保 subscribeEvents 已完成
	time.Sleep(20 * time.Millisecond)

	s.eventMu.RLock()
	n := len(s.eventSubs)
	s.eventMu.RUnlock()
	if n != 1 {
		t.Fatalf("subscribers after connect = %d, want 1", n)
	}

	// 关闭客户端连接
	resp.Body.Close()

	// 等待 handler 检测到 ctx.Done() 并执行 defer unsubscribeEvents
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.eventMu.RLock()
		n = len(s.eventSubs)
		s.eventMu.RUnlock()
		if n == 0 {
			return // 已清理
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("subscribers after close = %d, want 0 (subscription leaked)", n)
}

// TestSSE_MethodNotAllowed 验证非 GET 方法被拒绝。
func TestSSE_MethodNotAllowed(t *testing.T) {
	s := newSSETestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/events/stream", nil)
	rec := httptest.NewRecorder()
	s.handleEventsStream(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d, want 405", rec.Code)
	}
}

// TestSSE_PublishEvent_NoSubscribers 验证无订阅者时 publishEvent 快速返回不阻塞。
func TestSSE_PublishEvent_NoSubscribers(t *testing.T) {
	s := newSSETestServer()
	// 无订阅者，直接发布（不应 panic / 阻塞）
	done := make(chan struct{})
	go func() {
		s.publishEvent(context.Background(), "task_status", "", map[string]string{"taskID": "x"})
		close(done)
	}()
	select {
	case <-done:
		// ok
	case <-time.After(time.Second):
		t.Fatal("publishEvent with no subscribers blocked for >1s")
	}
}

// TestSSE_TenantIsolation 验证 H6 租户隔离：A 租户订阅者不会收到 B 租户的事件。
// 发布 tenantB 的 task_status 事件，tenantA 订阅者不应收到（应继续等待心跳或阻塞）。
func TestSSE_TenantIsolation(t *testing.T) {
	s := newSSETestServer()
	ts := httptest.NewServer(http.HandlerFunc(s.handleEventsStream))
	defer ts.Close()

	// 建立 tenantA 订阅者（携带 X-Tenant-ID 头）
	reqA, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest A: %v", err)
	}
	reqA.Header.Set("X-Tenant-ID", "tenantA")
	respA, err := http.DefaultClient.Do(reqA)
	if err != nil {
		t.Fatalf("GET SSE A: %v", err)
	}
	defer respA.Body.Close()

	brA := bufio.NewReader(respA.Body)
	// 读 hello 握手帧
	if _, err := readSSEFrame(brA); err != nil {
		t.Fatalf("read A hello: %v", err)
	}

	// 发布 tenantB 的事件 —— tenantA 订阅者不应收到
	s.publishEvent(context.Background(), "task_status", "tenantB", map[string]string{
		"taskID": "task-b",
		"status": "running",
	})
	// 发布 tenantA 的事件 —— tenantA 订阅者应收到
	s.publishEvent(context.Background(), "task_status", "tenantA", map[string]string{
		"taskID": "task-a",
		"status": "running",
	})

	// 读取下一帧：应为 tenantA 的事件，而非 tenantB 的
	frame, err := readSSEFrame(brA)
	if err != nil {
		t.Fatalf("read A event frame: %v", err)
	}
	if !strings.Contains(frame, `"taskID":"task-a"`) {
		t.Fatalf("tenantA subscriber received wrong event (expected task-a, got: %q)", frame)
	}
	if strings.Contains(frame, `"taskID":"task-b"`) {
		t.Fatalf("tenantA subscriber received tenantB event (cross-tenant leak): %q", frame)
	}
}

// TestSSE_RequireAuthRejectsMissingTenant 验证 requireAuth=true 时缺失身份头返回 401。
func TestSSE_RequireAuthRejectsMissingTenant(t *testing.T) {
	s := newSSETestServer()
	s.requireAuth = true
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events/stream", nil)
	rec := httptest.NewRecorder()
	s.handleEventsStream(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing tenant with requireAuth = %d, want 401", rec.Code)
	}
}

// TestSSE_DemoFillsDefaultTenant 验证 demo 模式下缺失身份头填充默认租户（不 401）。
func TestSSE_DemoFillsDefaultTenant(t *testing.T) {
	s := newSSETestServer()
	s.cfg = &config.Config{Demo: true}
	ts := httptest.NewServer(http.HandlerFunc(s.handleEventsStream))
	defer ts.Close()

	// 不携带 X-Tenant-ID 头，demo 模式应放行（填充 "default"）
	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET SSE: %v", err)
	}
	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)
	frame, err := readSSEFrame(br)
	if err != nil {
		t.Fatalf("read hello frame: %v", err)
	}
	if !strings.Contains(frame, "event: hello") {
		t.Fatalf("demo mode should allow connection (got frame: %q)", frame)
	}
}

// ============================================================================
// M1-4 分布式可观测性：SSE 事件携带 trace_id
// ============================================================================

// TestSSE_EventCarriesTraceID 验证 publishEvent 从 ctx 提取 trace_id 注入 SSEEvent.TraceID。
// 这是 M1-4 的核心保证：SSE 事件与后端链路追踪关联。
func TestSSE_EventCarriesTraceID(t *testing.T) {
	s := newSSETestServer()
	ts := httptest.NewServer(http.HandlerFunc(s.handleEventsStream))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET SSE: %v", err)
	}
	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)
	if _, err := readSSEFrame(br); err != nil {
		t.Fatalf("read hello: %v", err)
	}

	// 初始化 OTel 并创建带 span 的 ctx，使 publishEvent 能提取 trace_id。
	shutdown, otelErr := otelx.Init(otelx.Config{Stdout: true})
	if otelErr != nil {
		t.Fatalf("otelx.Init 失败: %v", otelErr)
	}
	defer shutdown(context.Background())

	spanCtx, span := otelx.StartSpan(context.Background(), "sse-test-span")
	defer span.End()
	expectedTraceID := otelx.TraceIDFromContext(spanCtx)
	if expectedTraceID == "" {
		t.Fatal("OTel TraceID 为空")
	}

	// 用带 span 的 ctx 发布事件，SSEEvent 应携带 trace_id。
	go func() {
		time.Sleep(50 * time.Millisecond)
		s.publishEvent(spanCtx, "task_status", "", map[string]string{
			"taskID": "trace-task",
		})
	}()

	frame, err := readSSEFrame(br)
	if err != nil {
		t.Fatalf("read event frame: %v", err)
	}
	// data 行应包含 traceID 字段，且值为期望的 trace_id。
	if !strings.Contains(frame, `"traceID":"`+expectedTraceID+`"`) {
		t.Fatalf("SSE 事件未携带正确的 trace_id: %q, want traceID=%q", frame, expectedTraceID)
	}
}

// TestSSE_EventNoTraceIDWhenNoSpan 验证 ctx 无 span 时 SSEEvent.TraceID 为空（向后兼容）。
// 旧客户端不感知 traceID 字段（omitempty），不破坏现有行为。
func TestSSE_EventNoTraceIDWhenNoSpan(t *testing.T) {
	s := newSSETestServer()
	ts := httptest.NewServer(http.HandlerFunc(s.handleEventsStream))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET SSE: %v", err)
	}
	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)
	if _, err := readSSEFrame(br); err != nil {
		t.Fatalf("read hello: %v", err)
	}

	// 用 context.Background()（无 span）发布事件，SSEEvent.TraceID 应为空。
	go func() {
		time.Sleep(50 * time.Millisecond)
		s.publishEvent(context.Background(), "task_status", "", map[string]string{
			"taskID": "no-trace-task",
		})
	}()

	frame, err := readSSEFrame(br)
	if err != nil {
		t.Fatalf("read event frame: %v", err)
	}
	// 无 span 时 traceID 字段应不存在（omitempty）或为空。
	if strings.Contains(frame, `"traceID":"`) && !strings.Contains(frame, `"traceID":""`) {
		// 包含非空 traceID 时才报错（空串由 omitempty 不输出，或输出空串均可接受）。
		if !strings.Contains(frame, `"traceID":""`) {
			t.Fatalf("无 span 时 SSE 事件不应携带非空 trace_id: %q", frame)
		}
	}
}
