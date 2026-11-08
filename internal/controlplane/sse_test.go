package controlplane

import (
	"bufio"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/otelx"
	"github.com/Levango7/OpsMesh/internal/store"
)

// 本文件为 SSE 实时推送的单元测试。
//
// 覆盖范围：
//   - handleEventsStream 响应头（Content-Type / Cache-Control / Connection / X-Accel-Buffering）
//   - hello 握手帧
//   - 事件推送格式（event: <type>\ndata: {...}\n\n）
//   - 多订阅者广播
//   - 连接关闭时清理订阅（防泄漏）
//   - POST 方法拒绝（405）
//   - publishEvent 无订阅者时快速返回
//   - 鉴权矩阵：伪造租户头无凭证 401 / 头与 JWT 租户不一致 403 / 合法路径不回归
//
// 测试模式：用 httptest.Server 启动真实 HTTP server（支持 Flusher + 流式 Body），
// http.Get 获取流式 resp.Body，bufio.Reader 逐帧读取（帧以空行 \n 结束）。

// newSSETestServer 构造最小测试控制面（仅 SSE 相关字段初始化）。
// Demo=true：空租户连接由 requireTenantContext 自动填充 "default"，
// 使底层 SSE 机制测试（响应头/握手/广播等）聚焦功能而非鉴权；
// 空租户拒绝路径由 TestSSE_EmptyTenantRejected 单独覆盖。
func newSSETestServer() *Server {
	return &Server{
		store:     store.NewMemoryStore(),
		cfg:       &config.Config{Demo: true},
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

// TestSSE_TenantIsolation 验证 租户隔离：A 租户订阅者不会收到 B 租户的事件。
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

// TestSSE_EmptyTenantRejected G1 鉴权修复：非 demo 且无租户时拒绝订阅（400），
// 不允许空租户全量订阅所有租户事件（防跨租户信息泄露）。
func TestSSE_EmptyTenantRejected(t *testing.T) {
	s := newSSETestServer()
	s.cfg = &config.Config{Demo: false} // 非 demo
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events/stream", nil)
	rec := httptest.NewRecorder()
	s.handleEventsStream(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty tenant non-demo = %d, want 400", rec.Code)
	}
}

// ============================================================================
// 分布式可观测性：SSE 事件携带 trace_id
// ============================================================================

// TestSSE_EventCarriesTraceID 验证 publishEvent 从 ctx 提取 trace_id 注入 SSEEvent.TraceID。
// 这是 的核心保证：SSE 事件与后端链路追踪关联。
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

// sseGateWriter 是可控的 ResponseWriter：**在写 hello 握手帧处卡住**，
// 让测试可以在「服务端已写 hello、尚未进入事件循环」的窗口内精确发布事件。
// 用于确定性地守住「订阅注册必须早于握手帧」这条顺序约束。
type sseGateWriter struct {
	mu      sync.Mutex
	header  http.Header
	code    int
	body    bytes.Buffer
	gate    chan struct{} // 放行闸
	gateHit chan struct{} // 已到达握手帧写入点
	once    sync.Once
}

func (w *sseGateWriter) Header() http.Header { return w.header }

func (w *sseGateWriter) WriteHeader(code int) { w.code = code }

func (w *sseGateWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("event: hello")) {
		w.once.Do(func() {
			close(w.gateHit)
			<-w.gate
		})
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.Write(p)
}

// Flush 实现 http.Flusher（handler 需要它才走流式分支）。
func (w *sseGateWriter) Flush() {}

func (w *sseGateWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.String()
}

// TestSSE_SubscribeBeforeHello_NoFirstEventLoss 守住「订阅注册早于握手帧」这条顺序约束。
//
// 为什么需要这个用例：handleEventsStream 若**先写 hello 再注册订阅**，客户端收到
// hello 后立刻 publish 的事件会落在「尚未入册」的窗口里，被 publishEvent 的
// 「无订阅者快速返回」静默丢弃 ⇒ 客户可见行为是「刚连上的客户端丢掉第一条事件」。
// 该缺陷在整包测试下必现（TestSSE_TenantIsolation 整包跑红、隔离跑不一定复现），
// 靠时序窗口的常规断言抓不住 ⇒ 用可控 ResponseWriter 把窗口**确定性**撑开：
// 服务端写 hello 时卡住，测试在卡住期间发布事件。
//   - 订阅先于握手（正确）：事件进有缓冲 chan ⇒ 随后必被写出；
//   - 握手先于订阅（缺陷）：此刻无订阅者 ⇒ publishEvent 直接丢弃 ⇒ 事件永不到达。
func TestSSE_SubscribeBeforeHello_NoFirstEventLoss(t *testing.T) {
	s := newSSETestServer()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events/stream", nil).WithContext(ctx)
	req.Header.Set("X-Tenant-ID", "tenantA")

	w := &sseGateWriter{
		header:  make(http.Header),
		gate:    make(chan struct{}),
		gateHit: make(chan struct{}),
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleEventsStream(w, req)
	}()

	select {
	case <-w.gateHit:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("服务端未到达握手帧写入点")
	}

	// 此刻服务端已写 hello、尚未写出后续帧；订阅是否已入册完全由顺序决定。
	s.publishEvent(ctx, "task_status", "tenantA", map[string]string{
		"taskID": "task-a",
		"status": "running",
	})
	close(w.gate)

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(w.String(), `"taskID":"task-a"`) {
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("首连事件被丢弃：握手后发布的事件未出现在流里（已写出内容=%q）", w.String())
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler 未在取消后退出")
	}
}

// ============================================================================
// SSE 鉴权收口：与 requireTenantContext 同一口径（2026-11-07）
// ============================================================================
//
// 缺陷形态：handler 此前自行提取身份，**X-Tenant-ID 头非空即无条件采信**，
// 只在头为空时才回退 Bearer/Cookie。于是 requireAuth=true 的生产形态下，
// 匿名者只发 `X-Tenant-ID: 受害租户` 就能订阅该租户的任务/告警/设备/审批事件流。
// 边缘 nginx 的 SSE location 不剥离该头（deploy/docker/proxy/nginx.conf.template），
// 仓内两处 Header.Del 都在出向代理，不覆盖本入向路径 ⇒ 缺口从边缘可达。
// requireTenantContext 对同一形态早已判 401/403（CI E2E-sec 实测捕获的那条越权），
// 本组用例把 SSE 钉到同一口径，并守住三条不能被收紧紧掉的合法路径。

// sseAuthzFixture 构造生产形态控制面（requireAuth=true、非 demo）+ 一枚 t9 租户有效令牌。
func sseAuthzFixture(t *testing.T) (*Server, string) {
	t.Helper()
	secret := []byte("test-jwt-secret-for-sse-authz-32bytes!")
	s := newSSETestServer()
	s.cfg = &config.Config{Demo: false}
	s.requireAuth = true
	s.jwtSecret = secret
	tok, err := authctx.SignJWT(authctx.JWTClaims{
		UserID: "u-sse", Username: "sse-user", Roles: []string{"role-operator"},
		TenantID: "t9", ExpiresAt: time.Now().Add(time.Hour),
	}, secret)
	if err != nil {
		t.Fatalf("签 token: %v", err)
	}
	return s, tok
}

// callSSEAuthz 用**已取消**的请求上下文调用 handler：拒绝路径立即返回；
// 若被放行则写完握手帧后随即由 ctx.Done 退出流循环（不挂测试），响应码仍可断言。
func callSSEAuthz(s *Server, decorate func(*http.Request)) *httptest.ResponseRecorder {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events/stream", nil).WithContext(ctx)
	if decorate != nil {
		decorate(req)
	}
	rec := httptest.NewRecorder()
	s.handleEventsStream(rec, req)
	return rec
}

// TestSSE_ForgedTenantHeaderWithoutCredentialRejected 伪造租户头且无任何凭证 ⇒ 401。
func TestSSE_ForgedTenantHeaderWithoutCredentialRejected(t *testing.T) {
	s, _ := sseAuthzFixture(t)
	rec := callSSEAuthz(s, func(r *http.Request) { r.Header.Set("X-Tenant-ID", "victim") })
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("伪造 X-Tenant-ID 且无凭证 = %d, want 401（body: %s）", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "event: hello") {
		t.Fatal("拒绝路径不得写出 SSE 握手帧")
	}
}

// TestSSE_TenantHeaderMismatchJWTRejected 头与令牌租户不一致 ⇒ 403（防越租户订阅）。
func TestSSE_TenantHeaderMismatchJWTRejected(t *testing.T) {
	s, tok := sseAuthzFixture(t)
	rec := callSSEAuthz(s, func(r *http.Request) {
		r.Header.Set("X-Tenant-ID", "victim")
		r.Header.Set("Authorization", "Bearer "+tok)
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("X-Tenant-ID 与 JWT 租户不一致 = %d, want 403（body: %s）", rec.Code, rec.Body.String())
	}
}

// TestSSE_HeaderAndMatchingJWTAllowed 头与令牌同租户（网关注入 + 凭证并存）⇒ 放行。
func TestSSE_HeaderAndMatchingJWTAllowed(t *testing.T) {
	s, tok := sseAuthzFixture(t)
	rec := callSSEAuthz(s, func(r *http.Request) {
		r.Header.Set("X-Tenant-ID", "t9")
		r.Header.Set("Authorization", "Bearer "+tok)
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("头与令牌同租户 = %d, want 200（body: %s）", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "event: hello") {
		t.Fatalf("放行路径应写出握手帧，实得 %q", rec.Body.String())
	}
}

// TestSSE_TrustGatewayHeadersAllowsHeaderOnly 显式 --trust-gateway-headers（README IAM 路径 B：
// 可信网关认证后剥离凭证只留头转发）⇒ 头单独放行。收紧不得把这条部署形态一并判死。
func TestSSE_TrustGatewayHeadersAllowsHeaderOnly(t *testing.T) {
	s, _ := sseAuthzFixture(t)
	s.cfg.TrustGatewayHeaders = true
	rec := callSSEAuthz(s, func(r *http.Request) { r.Header.Set("X-Tenant-ID", "t9") })
	if rec.Code != http.StatusOK {
		t.Fatalf("trust-gateway-headers 下仅带租户头 = %d, want 200（body: %s）", rec.Code, rec.Body.String())
	}
}

// TestSSE_CookieJWTSubscriberGetsOwnTenantEvents 浏览器 EventSource 路径（无法设自定义头，
// 只带 HttpOnly Cookie）⇒ 租户取自令牌，且只收本租户事件。收口不得把主线前端打断。
func TestSSE_CookieJWTSubscriberGetsOwnTenantEvents(t *testing.T) {
	s, tok := sseAuthzFixture(t)
	ts := httptest.NewServer(http.HandlerFunc(s.handleEventsStream))
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: accessTokenCookieName, Value: tok})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET SSE（Cookie JWT）: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Cookie JWT 订阅 = %d, want 200", resp.StatusCode)
	}

	br := bufio.NewReader(resp.Body)
	if _, err := readSSEFrame(br); err != nil {
		t.Fatalf("读 hello 帧: %v", err)
	}
	// 订阅已入册（hello 之前注册），两条事件都进缓冲 chan，非本租户那条被过滤。
	s.publishEvent(context.Background(), "task_status", "t9", map[string]string{"taskID": "task-own"})
	s.publishEvent(context.Background(), "task_status", "other", map[string]string{"taskID": "task-other"})
	frame, err := readSSEFrame(br)
	if err != nil {
		t.Fatalf("读事件帧: %v", err)
	}
	if !strings.Contains(frame, `"taskID":"task-own"`) {
		t.Fatalf("Cookie 订阅者未收到本租户（t9）事件，实得 %q", frame)
	}
	if strings.Contains(frame, `"taskID":"task-other"`) {
		t.Fatalf("Cookie 订阅者收到他租户事件（跨租户泄露）：%q", frame)
	}
}
