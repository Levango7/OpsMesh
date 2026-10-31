package aigateway

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

// --- 测试替身 ---

type fakeProvider struct {
	name    string
	calls   int32
	failErr error
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Complete(_ context.Context, req CompletionRequest) (*CompletionResponse, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.failErr != nil {
		return nil, f.failErr
	}
	return newResponse("echo:"+req.Messages[len(req.Messages)-1].Content, 10, 5), nil
}

func (f *fakeProvider) Embed(_ context.Context, _ string) ([]float64, error) {
	return []float64{1, 2}, nil
}

func (f *fakeProvider) ListModels() []string { return []string{"m1"} }

func (f *fakeProvider) HealthCheck(_ context.Context) error { return f.failErr }

func newResponse(text string, in, out int) *CompletionResponse {
	resp := &CompletionResponse{ID: "resp-1", Model: "m1"}
	choice := struct {
		Index        int        `json:"index"`
		Message      Message    `json:"message"`
		ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
		FinishReason string     `json:"finish_reason"`
	}{Message: Message{Role: RoleAssistant, Content: text}, FinishReason: "stop"}
	resp.Choices = append(resp.Choices, choice)
	resp.Usage.PromptTokens = in
	resp.Usage.CompletionTokens = out
	resp.Usage.TotalTokens = in + out
	return resp
}

func newGatewayWithProvider(t *testing.T, rpm int, failErr error) (*Gateway, *fakeProvider) {
	t.Helper()
	fp := &fakeProvider{name: "p1", failErr: failErr}
	g := NewGateway()
	g.RegisterProvider(ProviderConfig{Name: "p1", Type: "openai", Models: []string{"m1"}, RateLimit: rpm}, fp)
	return g, fp
}

// --- CircuitBreaker ---

func TestCircuitBreakerOpensAndRecovers(t *testing.T) {
	cb := NewCircuitBreaker(2, 30*time.Millisecond)
	if !cb.Allow() {
		t.Fatal("初始状态应允许请求")
	}
	cb.RecordFailure()
	if !cb.Allow() {
		t.Fatal("未达失败阈值不应熔断")
	}
	cb.RecordFailure()
	if cb.Allow() {
		t.Fatal("达到失败阈值后应熔断")
	}
	time.Sleep(45 * time.Millisecond)
	if !cb.Allow() {
		t.Fatal("超过 resetTimeout 应放行半开探测")
	}
	cb.RecordSuccess()
	if cb.failures != 0 || cb.state != "closed" {
		t.Fatalf("成功后应复位: failures=%d state=%s", cb.failures, cb.state)
	}
	if !cb.Allow() {
		t.Fatal("复位后应允许请求")
	}
}

// --- RateLimiter ---

func TestRateLimiterZeroMeansNoLimit(t *testing.T) {
	// RateLimit 是可选项；0 若按「零令牌」处理，该提供商的每次请求都会被拒。
	rl := NewRateLimiter(0)
	for i := 0; i < 3; i++ {
		if err := rl.Wait(context.Background()); err != nil {
			t.Fatalf("rpm=0 第 %d 次被限流: %v", i+1, err)
		}
	}
}

func TestRateLimiterBurstAndRefill(t *testing.T) {
	rl := NewRateLimiter(2)
	if err := rl.Wait(context.Background()); err != nil {
		t.Fatalf("首请求被限流: %v", err)
	}
	if err := rl.Wait(context.Background()); err != nil {
		t.Fatalf("第二请求被限流: %v", err)
	}
	if err := rl.Wait(context.Background()); err == nil {
		t.Fatal("burst 用尽后应限流")
	}

	// 过去 1 分钟 ⇒ 补满到 rpm 上限（而不是无限累积）。
	rl.mu.Lock()
	rl.tokens = 0
	rl.lastTime = time.Now().Add(-2 * time.Minute)
	rl.mu.Unlock()
	for i := 0; i < 2; i++ {
		if err := rl.Wait(context.Background()); err != nil {
			t.Fatalf("回填后第 %d 次应放行: %v", i+1, err)
		}
	}
	if err := rl.Wait(context.Background()); err == nil {
		t.Fatal("回填不应超过 rpm 上限")
	}
}

// --- TokenUsageTracker ---

func TestTokenUsageTrackerReport(t *testing.T) {
	tr := NewTokenUsageTracker()
	empty := tr.Report()
	if empty.ByProvider == nil || empty.ByModel == nil {
		t.Fatal("空统计也应返回非 nil 映射（前端直接遍历）")
	}
	if empty.TotalTokens != 0 {
		t.Fatalf("空统计 TotalTokens = %d", empty.TotalTokens)
	}

	tr.Record("p1", "m1", 100, 40)
	tr.Record("p1", "m2", 50, 10)
	tr.Record("p2", "m1", 7, 3)

	rep := tr.Report()
	if rep.TotalInputTokens != 157 || rep.TotalOutputTokens != 53 || rep.TotalTokens != 210 {
		t.Fatalf("总量统计 = %+v", rep)
	}
	if rep.ByProvider["p1"] != 200 || rep.ByProvider["p2"] != 10 {
		t.Fatalf("按提供商统计 = %+v", rep.ByProvider)
	}
	if rep.ByModel["m1"] != 150 || rep.ByModel["m2"] != 60 {
		t.Fatalf("按模型统计 = %+v", rep.ByModel)
	}
}

// --- SemanticCache ---

func TestSemanticCacheHitAndMiss(t *testing.T) {
	sc := NewSemanticCache(2)
	req := CompletionRequest{Model: "m1", Messages: []Message{{Role: RoleUser, Content: "hi"}}}
	if sc.Get(req) != nil {
		t.Fatal("未写入前应 miss")
	}
	sc.Set(req, newResponse("v1", 1, 1))
	if got := sc.Get(req); got == nil || got.Choices[0].Message.Content != "v1" {
		t.Fatalf("写入后应命中: %+v", got)
	}

	// 换模型/换参数必须视为不同请求。
	other := req
	other.Model = "m2"
	if sc.Get(other) != nil {
		t.Fatal("不同模型不应命中同一缓存")
	}
	other = req
	other.Temperature = 0.9
	if sc.Get(other) != nil {
		t.Fatal("改了 temperature 不应命中旧结果")
	}
	// 角色不同也不能命中。
	other = CompletionRequest{Model: "m1", Messages: []Message{{Role: RoleAssistant, Content: "hi"}}}
	if sc.Get(other) != nil {
		t.Fatal("角色不同不应命中同一缓存")
	}
}

// 旧实现把各条消息内容首字符连乘，14 种切分（"a"+"bc" vs "ab"+"c" 等）全部撞成
// 同一键 —— 语义缓存会把另一个请求的答案当作命中返回。
func TestSemanticCacheNoCrossMessageCollision(t *testing.T) {
	splits := []struct{ first, second string }{
		{"a", "bc"},
		{"ab", "c"},
	}
	sc := NewSemanticCache(10)
	sc.Set(CompletionRequest{Model: "m1", Messages: []Message{{Role: RoleUser, Content: splits[0].first}, {Role: RoleUser, Content: splits[0].second}}}, newResponse("A", 1, 1))
	got := sc.Get(CompletionRequest{Model: "m1", Messages: []Message{{Role: RoleUser, Content: splits[1].first}, {Role: RoleUser, Content: splits[1].second}}})
	if got != nil {
		t.Fatalf("请求 %q+%q 撞进了 %q+%q 的缓存条目: %q",
			splits[1].first, splits[1].second, splits[0].first, splits[0].second, got.Choices[0].Message.Content)
	}
}

func TestSemanticCacheEviction(t *testing.T) {
	sc := NewSemanticCache(2)
	for i := 0; i < 5; i++ {
		sc.Set(CompletionRequest{Model: "m1", Messages: []Message{{Role: RoleUser, Content: string(rune('a' + i))}}}, newResponse("v", 1, 1))
	}
	if len(sc.cache) > 2 {
		t.Fatalf("淘汰后仍占 %d 条, 上限 2", len(sc.cache))
	}
}

// --- Gateway 组合路径 ---

func TestGatewayCompleteRecordsUsageAndCaches(t *testing.T) {
	g, fp := newGatewayWithProvider(t, 0, nil)
	req := CompletionRequest{Model: "m1", Messages: []Message{{Role: RoleUser, Content: "ping"}}}

	resp, err := g.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Choices[0].Message.Content != "echo:ping" {
		t.Fatalf("响应内容 = %q", resp.Choices[0].Message.Content)
	}

	if _, err := g.Complete(context.Background(), req); err != nil {
		t.Fatalf("第二次 Complete: %v", err)
	}
	if got := atomic.LoadInt32(&fp.calls); got != 1 {
		t.Fatalf("第二次应走缓存, provider 调用次数 = %d", got)
	}

	rep := g.GetTokenUsage()
	if rep.TotalInputTokens != 10 || rep.TotalOutputTokens != 5 || rep.ByProvider["p1"] != 15 {
		t.Fatalf("用量统计只应记一次真实调用: %+v", rep)
	}
}

func TestGatewayOpensBreakerAfterFailures(t *testing.T) {
	g, fp := newGatewayWithProvider(t, 0, errors.New("upstream down"))

	var lastErr error
	for i := 0; i < 6; i++ {
		req := CompletionRequest{Model: "m1", Messages: []Message{{Role: RoleUser, Content: "r" + string(rune('0'+i))}}}
		_, lastErr = g.Complete(context.Background(), req)
		if lastErr == nil {
			t.Fatal("上游持续失败时 Complete 应报错")
		}
	}
	if !strings.Contains(lastErr.Error(), "circuit breaker open") {
		t.Fatalf("第 6 次应被熔断器拦下: %v", lastErr)
	}
	if got := atomic.LoadInt32(&fp.calls); got != 5 {
		t.Fatalf("熔断后不应再打下游, provider 调用次数 = %d", got)
	}
}

func TestGatewayRateLimitsWithoutConfig(t *testing.T) {
	g, fp := newGatewayWithProvider(t, 0, nil)
	for i := 0; i < 10; i++ {
		req := CompletionRequest{Model: "m1", Messages: []Message{{Role: RoleUser, Content: "n" + string(rune('0'+i))}}}
		if _, err := g.Complete(context.Background(), req); err != nil {
			t.Fatalf("未配置 RateLimit 时第 %d 次请求失败: %v", i+1, err)
		}
	}
	if got := atomic.LoadInt32(&fp.calls); got != 10 {
		t.Fatalf("应放行 10 次, got %d", got)
	}
}

func TestGatewayWithoutProvider(t *testing.T) {
	if _, err := NewGateway().Complete(context.Background(), CompletionRequest{Model: "m1"}); err == nil {
		t.Fatal("无可用提供商时应报错")
	}
	if _, err := NewGateway().Embed(context.Background(), "ghost", "x"); err == nil ||
		!strings.Contains(err.Error(), "not found") {
		t.Fatalf("Embed 未知提供商: %v", err)
	}
}

func TestGatewayHealthCheckAndListProviders(t *testing.T) {
	g, _ := newGatewayWithProvider(t, 60, nil)
	if ok := g.HealthCheck(context.Background())["p1"]; !ok {
		t.Fatal("健康检查应报 ok")
	}
	cfgs := g.ListProviders()
	if len(cfgs) != 1 || cfgs[0].Name != "p1" || cfgs[0].RateLimit != 60 {
		t.Fatalf("ListProviders = %+v", cfgs)
	}

	broken := &fakeProvider{name: "p2", failErr: errors.New("down")}
	g.RegisterProvider(ProviderConfig{Name: "p2", Models: []string{"m2"}}, broken)
	if ok := g.HealthCheck(context.Background())["p2"]; ok {
		t.Fatal("故障提供商应报不健康")
	}
}

// --- ContextCompressor ---

func TestNewContextCompressorDefault(t *testing.T) {
	if c := NewContextCompressor(0); c.maxTokens != 4096 {
		t.Fatalf("非正预算应回退默认 4096, got %d", c.maxTokens)
	}
}

func TestCompressWithinBudgetPassesThrough(t *testing.T) {
	c := NewContextCompressor(4096)
	msgs := []Message{{Role: RoleSystem, Content: "sys"}, {Role: RoleUser, Content: "hi"}}
	got := c.Compress(msgs)
	if len(got) != 2 || got[1].Content != "hi" {
		t.Fatalf("预算内不应改写消息: %+v", got)
	}
}

func TestCompressKeepsSystemAndRecentAndSummarizesEarly(t *testing.T) {
	c := NewContextCompressor(1000)
	msgs := []Message{{Role: RoleSystem, Content: "系统提示"}}
	for i := 0; i < 20; i++ {
		msgs = append(msgs, Message{Role: RoleUser, Content: strings.Repeat("x", 400)})
	}

	got := c.Compress(msgs)
	if len(got) >= len(msgs) {
		t.Fatalf("超预算应被压缩: %d -> %d", len(msgs), len(got))
	}
	if got[0].Role != RoleSystem || got[0].Content != "系统提示" {
		t.Fatalf("系统消息必须原样保留: %+v", got[0])
	}
	if got[len(got)-1].Content != msgs[len(msgs)-1].Content {
		t.Fatal("最近一条对话必须保留")
	}
	summaryFound := false
	for _, m := range got[1:] {
		if strings.HasPrefix(m.Content, "早期对话摘要: ") {
			summaryFound = true
			break
		}
	}
	if !summaryFound {
		t.Fatal("被丢弃的早期对话必须进摘要")
	}
}

func TestTruncateStr(t *testing.T) {
	if got := truncateStr("abcdef", 10); got != "abcdef" {
		t.Fatalf("未超长不应截断: %q", got)
	}
	if got := truncateStr("abcdefgh", 3); got != "abc..." {
		t.Fatalf("ASCII 截断 = %q", got)
	}
	// 中文字符 3 字节：按字节硬切会在字符中间断开，送进模型的上下文带非法字符。
	got := truncateStr(strings.Repeat("测", 100), 10)
	if !utf8.ValidString(got) {
		t.Fatalf("CJK 截断产生非法 UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("CJK 截断缺少省略号: %q", got)
	}
}

func TestSummarizeCapsLength(t *testing.T) {
	c := NewContextCompressor(100)
	msgs := make([]Message, 0, 30)
	for i := 0; i < 30; i++ {
		msgs = append(msgs, Message{Role: RoleUser, Content: strings.Repeat("讨论", 200)})
	}
	summary := c.summarize(msgs)
	if len(summary) > 504 { // 500 字节上限 + 省略号
		t.Fatalf("摘要未截断: %d 字节", len(summary))
	}
	if !utf8.ValidString(summary) {
		t.Fatal("摘要截断产生非法 UTF-8")
	}
}

// --- route 确定性 ---

// 多个提供商声明同一模型时，旧实现直接 `for name := range g.configs` 取第一个命中，
// map 迭代顺序随机 ⇒ 同一请求会打到不同厂商（输出/计价/合规边界都不同）。
func TestRouteIsDeterministicAcrossProviders(t *testing.T) {
	g := NewGateway()
	g.RegisterProvider(ProviderConfig{Name: "zz-provider", Models: []string{"shared"}}, &fakeProvider{name: "zz"})
	g.RegisterProvider(ProviderConfig{Name: "aa-provider", Models: []string{"shared"}}, &fakeProvider{name: "aa"})

	for i := 0; i < 200; i++ {
		provider, model, err := g.route("shared")
		if err != nil {
			t.Fatalf("route: %v", err)
		}
		if provider != "aa-provider" || model != "shared" {
			t.Fatalf("第 %d 次 route = (%s,%s)，要求稳定命中按名排序的第一个声明方", i, provider, model)
		}
	}
}

// ModelRoute.Priority 此前声明了但从不参与判定。
func TestRouteHonoursRulePriority(t *testing.T) {
	g := NewGateway()
	g.RegisterProvider(ProviderConfig{Name: "p1", Models: []string{"m1"}}, &fakeProvider{name: "p1"})
	g.RegisterProvider(ProviderConfig{Name: "p2", Models: []string{"m2"}}, &fakeProvider{name: "p2"})
	g.SetRoutes([]ModelRoute{
		{TaskType: "chat", Provider: "p2", Model: "rare", Priority: 1},
		{TaskType: "chat", Provider: "p1", Model: "rare", Priority: 5},
	})

	for i := 0; i < 50; i++ {
		if provider, _, err := g.route("rare"); err != nil || provider != "p1" {
			t.Fatalf("应取 Priority 最高的 p1, got (%s,%v)", provider, err)
		}
	}
}

// 规则指向未注册的提供商时必须跳过，不能让请求打到空气上。
func TestRouteSkipsUnregisteredRule(t *testing.T) {
	g := NewGateway()
	g.RegisterProvider(ProviderConfig{Name: "real", Models: []string{"m1"}}, &fakeProvider{name: "real"})
	g.SetRoutes([]ModelRoute{
		{Provider: "ghost", Model: "rare", Priority: 99},
		{Provider: "real", Model: "rare", Priority: 1},
	})
	if provider, _, err := g.route("rare"); err != nil || provider != "real" {
		t.Fatalf("got (%s,%v), want real", provider, err)
	}
}

// 兜底：单提供商部署沿用请求模型名；多提供商且都没声明该模型时明确报错，
// 不再随机挑一个把不支持的模型名发给远端。
func TestRouteFallbackRules(t *testing.T) {
	single := NewGateway()
	single.RegisterProvider(ProviderConfig{Name: "only", Models: []string{"m1"}}, &fakeProvider{name: "only"})
	if provider, model, err := single.route("undeclared"); err != nil || provider != "only" || model != "undeclared" {
		t.Fatalf("单提供商兜底 = (%s,%s,%v)", provider, model, err)
	}

	multi := NewGateway()
	multi.RegisterProvider(ProviderConfig{Name: "a", Models: []string{"m1"}}, &fakeProvider{name: "a"})
	multi.RegisterProvider(ProviderConfig{Name: "b", Models: []string{"m2"}}, &fakeProvider{name: "b"})
	if _, _, err := multi.route("nobody-declares-this"); err == nil {
		t.Fatal("多提供商且无人声明该模型时应报错，而不是随机挑一个")
	}
}

// 预留输出空间要按预算比例，而不是固定 500：固定 500 在 400 token 预算下
// remaining 变负，整段对话只剩一条摘要。
func TestCompressSmallBudgetKeepsMoreThanOneTurn(t *testing.T) {
	c := NewContextCompressor(400)
	msgs := []Message{{Role: RoleSystem, Content: "sys"}}
	for i := 0; i < 10; i++ {
		msgs = append(msgs, Message{Role: RoleUser, Content: strings.Repeat("x", 400)})
	}

	got := c.Compress(msgs)
	keptTurns := 0
	for _, m := range got {
		if m.Role != RoleSystem && !strings.HasPrefix(m.Content, "早期对话摘要: ") {
			keptTurns++
		}
	}
	if keptTurns < 2 {
		t.Fatalf("400 预算下应至少保留最近两轮对话, got %d（结果 %+v）", keptTurns, got)
	}
}

// 系统消息本身吃满预算时（remaining=0），最近一条仍必须原样保留：
// 丢了用户刚问的那句，模型就无从回答。
func TestCompressKeepsLatestTurnWhenBudgetExhausted(t *testing.T) {
	c := NewContextCompressor(100)
	last := "这是用户刚问的最后一句"
	msgs := []Message{{Role: RoleSystem, Content: strings.Repeat("s", 400)}}
	for i := 0; i < 4; i++ {
		msgs = append(msgs, Message{Role: RoleUser, Content: strings.Repeat("x", 400)})
	}
	msgs = append(msgs, Message{Role: RoleUser, Content: last})

	got := c.Compress(msgs)
	found := false
	for _, m := range got {
		if m.Content == last {
			found = true
		}
	}
	if !found {
		t.Fatalf("预算被系统消息吃满时最近一条被吞掉了: %+v", got)
	}
}
