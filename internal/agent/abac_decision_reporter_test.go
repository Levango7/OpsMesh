package agent

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/pkg/abacbundle"
)

// newTestABACDecisionReporter 构造启用状态的决策回传器（控制面指向 baseURL）。
func newTestABACDecisionReporter(baseURL string) *ABACDecisionReporter {
	cfg := &config.Config{ABACBundleEnabled: true}
	return NewABACDecisionReporter(cfg, "agent-1", "test-secret", baseURL, nopABACBundleLogger{})
}

// verifyABACDecisionSignature 按服务端契约复算 HMAC，返回解析后的决策条数。
// 签名不符时直接 t.Errorf（handler 与测试同 goroutine 生命周期内）。
func verifyABACDecisionSignature(t *testing.T, r *http.Request, body []byte, agentID, secret string) []abacDecisionWire {
	t.Helper()
	if got := r.Header.Get("X-OpsMesh-Agent-ID"); got != agentID {
		t.Errorf("agent id header = %q, want %q", got, agentID)
	}
	ts := r.Header.Get("X-OpsMesh-Agent-Timestamp")
	if _, err := time.ParseDuration(ts + "s"); err != nil {
		t.Errorf("timestamp header 非 unix 秒: %q", ts)
	}
	digest := sha256.Sum256(body)
	canonical := "abac-decisions\n" + agentID + "\n" + ts + "\n" + hex.EncodeToString(digest[:])
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	want := hex.EncodeToString(mac.Sum(nil))
	if got := r.Header.Get("X-OpsMesh-Agent-Signature"); got != want {
		t.Errorf("签名不匹配: got %s want %s canonical=%q", got, want, canonical)
	}
	if ct := r.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q", ct)
	}

	var batch abacDecisionBatch
	if err := json.Unmarshal(body, &batch); err != nil {
		t.Fatalf("body 解码失败: %v (body=%s)", err, body)
	}
	for _, d := range batch.Decisions {
		if _, err := time.Parse(time.RFC3339, d.OccurredAt); err != nil {
			t.Errorf("occurredAt 非 RFC3339: %q (%v)", d.OccurredAt, err)
		}
	}
	return batch.Decisions
}

func TestABACDecisionBufferDropsOldestAndStats(t *testing.T) {
	r := newTestABACDecisionReporter("http://127.0.0.1:1")
	for i := 0; i < abacDecisionBufferCap+5; i++ {
		r.Record(ABACDecisionRecord{RequestID: fmt.Sprintf("req-%d", i)})
	}
	stats := r.Stats()
	if stats.Buffered != abacDecisionBufferCap {
		t.Fatalf("buffered = %d, want %d", stats.Buffered, abacDecisionBufferCap)
	}
	if stats.Dropped != 5 {
		t.Fatalf("dropped = %d, want 5（满后每追加一条丢最旧）", stats.Dropped)
	}

	r.mu.Lock()
	first := r.ring[r.head].rec.RequestID
	last := r.ring[(r.head+r.count-1)%len(r.ring)].rec.RequestID
	r.mu.Unlock()
	if first != "req-5" {
		t.Fatalf("队首 = %q, want req-5（最旧 5 条被丢弃）", first)
	}
	if last != "req-504" {
		t.Fatalf("队尾 = %q, want req-504", last)
	}
}

func TestABACDecisionFlushHMACAndClearsFlushed(t *testing.T) {
	var mu sync.Mutex
	var received int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != abacDecisionRoute {
			t.Errorf("path = %q, want %q", req.URL.Path, abacDecisionRoute)
		}
		if req.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", req.Method)
		}
		body, _ := io.ReadAll(req.Body)
		decisions := verifyABACDecisionSignature(t, req, body, "agent-1", "test-secret")
		mu.Lock()
		received = len(decisions)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprintf(w, `{"accepted":%d}`, len(decisions))
	}))
	defer srv.Close()

	r := newTestABACDecisionReporter(srv.URL)
	fixed := time.Date(2026, 11, 18, 1, 2, 3, 0, time.UTC)
	r.now = func() time.Time { return fixed }
	for i := 0; i < 3; i++ {
		r.Record(ABACDecisionRecord{
			RequestID:  fmt.Sprintf("req-%d", i),
			BundleID:   "b-1",
			PolicyID:   "p-1",
			Allowed:    i%2 == 0,
			Reason:     "allowed by policy p-1",
			Mode:       abacDecisionModeDevice,
			OccurredAt: fixed,
		})
	}
	if err := r.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	mu.Lock()
	got := received
	mu.Unlock()
	if got != 3 {
		t.Fatalf("server received %d decisions, want 3", got)
	}
	stats := r.Stats()
	if stats.Buffered != 0 || stats.Accepted != 3 {
		t.Fatalf("stats after flush = %+v, want buffered=0 accepted=3", stats)
	}
}

func TestABACDecisionFlushPartialAcceptKeepsRemaining(t *testing.T) {
	var bodies [][]abacDecisionWire
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		decisions := verifyABACDecisionSignature(t, req, body, "agent-1", "test-secret")
		mu.Lock()
		bodies = append(bodies, decisions)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
		// 部分接受：只收前 2 条。
		_, _ = w.Write([]byte(`{"accepted":2}`))
	}))
	defer srv.Close()

	r := newTestABACDecisionReporter(srv.URL)
	for i := 0; i < 4; i++ {
		r.Record(ABACDecisionRecord{RequestID: fmt.Sprintf("req-%d", i)})
	}
	if err := r.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	mu.Lock()
	first := bodies[0]
	mu.Unlock()
	if len(first) != 4 {
		t.Fatalf("首批发送 %d 条, want 4", len(first))
	}

	r.mu.Lock()
	remaining := []string{r.ring[r.head].rec.RequestID, r.ring[(r.head+1)%len(r.ring)].rec.RequestID}
	r.mu.Unlock()
	if strings.Join(remaining, ",") != "req-2,req-3" {
		t.Fatalf("部分接受后保留 = %v, want [req-2 req-3]", remaining)
	}
	if stats := r.Stats(); stats.Buffered != 2 || stats.Accepted != 2 {
		t.Fatalf("stats = %+v, want buffered=2 accepted=2", stats)
	}

	// 第二次 flush 只发剩余两条，且服务端仍只接受 2 条中的前 2 条。
	if err := r.Flush(context.Background()); err != nil {
		t.Fatalf("second Flush: %v", err)
	}
	mu.Lock()
	second := bodies[1]
	mu.Unlock()
	if len(second) != 2 || second[0].RequestID != "req-2" || second[1].RequestID != "req-3" {
		t.Fatalf("第二批 = %+v, want req-2,req-3", second)
	}
	if stats := r.Stats(); stats.Buffered != 0 || stats.Accepted != 4 {
		t.Fatalf("final stats = %+v, want buffered=0 accepted=4", stats)
	}
}

// TestABACDecisionFlushKeepsRecordsAddedDuringFlight：202 只摘除已发送批次，
// 飞行期间新 Record 的记录必须留在缓冲区。
func TestABACDecisionFlushKeepsRecordsAddedDuringFlight(t *testing.T) {
	gotRequest := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		decisions := verifyABACDecisionSignature(t, req, body, "agent-1", "test-secret")
		close(gotRequest)
		<-release
		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprintf(w, `{"accepted":%d}`, len(decisions))
	}))
	defer srv.Close()

	r := newTestABACDecisionReporter(srv.URL)
	r.Record(ABACDecisionRecord{RequestID: "req-0"})
	r.Record(ABACDecisionRecord{RequestID: "req-1"})

	flushErr := make(chan error, 1)
	go func() { flushErr <- r.Flush(context.Background()) }()

	select {
	case <-gotRequest:
	case <-time.After(3 * time.Second):
		t.Fatal("Flush 未发出请求")
	}
	r.Record(ABACDecisionRecord{RequestID: "req-inflight"})
	close(release)

	select {
	case err := <-flushErr:
		if err != nil {
			t.Fatalf("Flush: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Flush 未返回")
	}
	stats := r.Stats()
	if stats.Buffered != 1 || stats.Accepted != 2 {
		t.Fatalf("stats = %+v, want buffered=1 accepted=2", stats)
	}
	r.mu.Lock()
	remaining := r.ring[r.head].rec.RequestID
	r.mu.Unlock()
	if remaining != "req-inflight" {
		t.Fatalf("remaining = %q, want req-inflight（飞行期间新增不得被摘除）", remaining)
	}
}

func TestABACDecisionFlushRetryOn500ThenSuccess(t *testing.T) {
	var attempts int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		decisions := verifyABACDecisionSignature(t, req, body, "agent-1", "test-secret")
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprintf(w, `{"accepted":%d}`, len(decisions))
	}))
	defer srv.Close()

	r := newTestABACDecisionReporter(srv.URL)
	r.retryBackoffs = []time.Duration{time.Millisecond, time.Millisecond}
	r.Record(ABACDecisionRecord{RequestID: "req-1"})
	r.Record(ABACDecisionRecord{RequestID: "req-2"})
	if err := r.Flush(context.Background()); err != nil {
		t.Fatalf("Flush 应在重试后成功: %v", err)
	}
	mu.Lock()
	got := attempts
	mu.Unlock()
	if got != 2 {
		t.Fatalf("attempts = %d, want 2（1 次 500 + 1 次成功）", got)
	}
	if stats := r.Stats(); stats.Buffered != 0 || stats.Accepted != 2 {
		t.Fatalf("stats = %+v, want buffered=0 accepted=2", stats)
	}
}

func TestABACDecisionFlush401KeepsBuffer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	r := newTestABACDecisionReporter(srv.URL)
	r.retryBackoffs = []time.Duration{time.Millisecond, time.Millisecond}
	r.Record(ABACDecisionRecord{RequestID: "req-1"})
	if err := r.Flush(context.Background()); err == nil {
		t.Fatal("401 时 Flush 必须返回错误（不回传成功）")
	}
	stats := r.Stats()
	if stats.Buffered != 1 || stats.Accepted != 0 || stats.Failed != 1 {
		t.Fatalf("401 后 stats = %+v, want buffered=1 accepted=0 failed=1", stats)
	}
}

func TestABACDecisionFlush413HalvesBatch(t *testing.T) {
	var sizes []int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		decisions := verifyABACDecisionSignature(t, req, body, "agent-1", "test-secret")
		mu.Lock()
		sizes = append(sizes, len(decisions))
		mu.Unlock()
		if len(decisions) > 2 {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprintf(w, `{"accepted":%d}`, len(decisions))
	}))
	defer srv.Close()

	r := newTestABACDecisionReporter(srv.URL)
	for i := 0; i < 4; i++ {
		r.Record(ABACDecisionRecord{RequestID: fmt.Sprintf("req-%d", i)})
	}
	if err := r.Flush(context.Background()); err != nil {
		t.Fatalf("413 减半后应成功: %v", err)
	}
	mu.Lock()
	got := append([]int(nil), sizes...)
	mu.Unlock()
	if len(got) != 2 || got[0] != 4 || got[1] != 2 {
		t.Fatalf("批次大小序列 = %v, want [4 2]（413 后立即减半重试一次）", got)
	}
	r.mu.Lock()
	remaining := []string{r.ring[r.head].rec.RequestID, r.ring[(r.head+1)%len(r.ring)].rec.RequestID}
	r.mu.Unlock()
	if strings.Join(remaining, ",") != "req-2,req-3" {
		t.Fatalf("413 后保留 = %v, want [req-2 req-3]", remaining)
	}
	if stats := r.Stats(); stats.Accepted != 2 {
		t.Fatalf("accepted = %d, want 2", stats.Accepted)
	}
}

func TestABACDecisionCapabilityMerge(t *testing.T) {
	merged := mergeCapabilities(
		[]string{abacBundleCapability},
		[]string{abacDecisionCapability},
		[]string{abacBundleCapability, abacDecisionCapability},
	)
	if len(merged) != 2 {
		t.Fatalf("merged = %v, want 2 条去重能力", merged)
	}
	if merged[0] != abacBundleCapability || merged[1] != abacDecisionCapability {
		t.Fatalf("merged = %v, want [%s %s]", merged, abacBundleCapability, abacDecisionCapability)
	}

	agentID := "agent-1"
	r := newTestABACDecisionReporter("http://127.0.0.1:1")
	bundle := &ABACBundleClient{enabled: true, keyCount: 1, agentID: agentID}
	a := &Agent{cfg: &config.Config{ABACBundleEnabled: true}, agentID: agentID, abacBundleClient: bundle, abacDecisionReporter: r}
	raw, err := json.Marshal(a.heartbeatRequest())
	if err != nil {
		t.Fatalf("marshal heartbeat: %v", err)
	}
	for _, cap := range []string{abacBundleCapability, abacDecisionCapability} {
		if got := strings.Count(string(raw), `"`+cap+`"`); got != 1 {
			t.Fatalf("能力 %q 在心跳中出现 %d 次, want 1: %s", cap, got, raw)
		}
	}
}

func TestABACDecisionStartStopsOnContextCancel(t *testing.T) {
	flushed := make(chan int, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		decisions := verifyABACDecisionSignature(t, req, body, "agent-1", "test-secret")
		select {
		case flushed <- len(decisions):
		default:
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprintf(w, `{"accepted":%d}`, len(decisions))
	}))
	defer srv.Close()

	r := newTestABACDecisionReporter(srv.URL)
	r.interval = 10 * time.Millisecond
	r.Record(ABACDecisionRecord{RequestID: "req-1"})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Start(ctx)
	}()

	select {
	case n := <-flushed:
		if n != 1 {
			t.Fatalf("flushed = %d, want 1", n)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("周期回传未在 3s 内发生")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ctx 取消后 Start 未退出（goroutine 泄漏）")
	}
}

func TestABACDecisionModeFor(t *testing.T) {
	if got := ModeFor(abacbundle.Decision{BundleExpired: true}); got != "device-offline" {
		t.Fatalf("expired mode = %q, want device-offline", got)
	}
	if got := ModeFor(abacbundle.Decision{BundleExpired: false}); got != "device" {
		t.Fatalf("fresh mode = %q, want device", got)
	}
}

func TestABACDecisionRecordEvaluation(t *testing.T) {
	r := newTestABACDecisionReporter("http://127.0.0.1:1")
	r.SetBundleIDProvider(func() string { return "bundle-42" })
	fixed := time.Date(2026, 11, 18, 4, 5, 6, 0, time.UTC)
	r.now = func() time.Time { return fixed }

	r.RecordEvaluation(abacbundle.Decision{
		Allowed:         false,
		MatchedPolicyID: "pol-7",
		Reason:          "denied by policy pol-7",
		BundleExpired:   true,
	}, "req-9")

	r.mu.Lock()
	rec := r.ring[r.head].rec
	r.mu.Unlock()
	if rec.RequestID != "req-9" || rec.BundleID != "bundle-42" || rec.PolicyID != "pol-7" {
		t.Fatalf("record = %+v, want req-9/bundle-42/pol-7", rec)
	}
	if rec.Allowed || rec.Mode != "device-offline" {
		t.Fatalf("record = %+v, want denied/device-offline", rec)
	}
	if !rec.OccurredAt.Equal(fixed) {
		t.Fatalf("occurredAt = %s, want %s", rec.OccurredAt, fixed)
	}
}

// Stats 必须导出最近一次失败的时间与原因，并在下一次成功后清空 LastError：
// soak 门禁据此区分"从未回传"与"回传失败中"，并读取 dropped 计算丢弃率。
func TestABACDecisionReporterStats_LastFlushAtAndLastError(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		decisions := verifyABACDecisionSignature(t, req, body, "agent-1", "test-secret")
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprintf(w, `{"accepted":%d}`, len(decisions))
	}))
	defer srv.Close()

	r := newTestABACDecisionReporter(srv.URL)
	r.retryBackoffs = []time.Duration{time.Millisecond, time.Millisecond}
	r.Record(ABACDecisionRecord{RequestID: "req-1"})

	before := time.Now().UTC()
	if err := r.Flush(context.Background()); err == nil {
		t.Fatal("500 持续失败时 Flush 必须返回错误")
	}
	stats := r.Stats()
	if stats.Failed != 1 {
		t.Fatalf("Failed = %d, want 1", stats.Failed)
	}
	if stats.Buffered != 1 || stats.Dropped != 0 {
		t.Fatalf("失败后 Buffered/Dropped = %d/%d, want 1/0", stats.Buffered, stats.Dropped)
	}
	if stats.LastFlushAt.IsZero() || stats.LastFlushAt.Before(before) {
		t.Fatalf("LastFlushAt 应为本次失败时间（>= %v），得到 %v", before, stats.LastFlushAt)
	}
	if !strings.Contains(stats.LastError, "500") {
		t.Fatalf("LastError 应包含状态码 500，得到 %q", stats.LastError)
	}

	fail.Store(false)
	if err := r.Flush(context.Background()); err != nil {
		t.Fatalf("恢复后 Flush 应成功: %v", err)
	}
	stats = r.Stats()
	if stats.Accepted != 1 || stats.Buffered != 0 {
		t.Fatalf("成功后 Accepted/Buffered = %d/%d, want 1/0", stats.Accepted, stats.Buffered)
	}
	if stats.LastError != "" {
		t.Fatalf("成功后 LastError 应清空，得到 %q", stats.LastError)
	}
	if !stats.LastFlushAt.After(before) {
		t.Fatalf("LastFlushAt 应随成功回传推进，得到 %v", stats.LastFlushAt)
	}
}
