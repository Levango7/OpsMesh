package aigateway

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"sync"
	"time"
)

// CircuitBreaker 熔断器
type CircuitBreaker struct {
	mu           sync.RWMutex
	failures     int
	maxFailures  int
	resetTimeout time.Duration
	lastFailure  time.Time
	state        string // closed/open/half-open
}

func NewCircuitBreaker(maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		maxFailures:  maxFailures,
		resetTimeout: resetTimeout,
		state:        "closed",
	}
}

func (cb *CircuitBreaker) Allow() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if cb.state == "closed" {
		return true
	}
	if cb.state == "open" && time.Since(cb.lastFailure) > cb.resetTimeout {
		return true // half-open
	}
	return false
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
	cb.state = "closed"
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	cb.lastFailure = time.Now()
	if cb.failures >= cb.maxFailures {
		cb.state = "open"
	}
}

// RateLimiter 令牌桶限流器
type RateLimiter struct {
	mu       sync.Mutex
	rpm      int
	tokens   float64
	lastTime time.Time
}

// NewRateLimiter rpm<=0 表示不限流：ProviderConfig.RateLimit 是可选项，
// 未配置时若按 0 处理会让该提供商的每一次请求都被拒（fail-closed 的误伤）。
func NewRateLimiter(rpm int) *RateLimiter {
	return &RateLimiter{
		rpm:      rpm,
		tokens:   float64(rpm),
		lastTime: time.Now(),
	}
}

func (rl *RateLimiter) Wait(ctx interface{}) error {
	if rl.rpm <= 0 {
		return nil
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastTime).Seconds()
	rl.tokens += elapsed * float64(rl.rpm) / 60.0
	if rl.tokens > float64(rl.rpm) {
		rl.tokens = float64(rl.rpm)
	}
	rl.lastTime = now

	if rl.tokens < 1 {
		return fmt.Errorf("rate limit exceeded")
	}
	rl.tokens--
	return nil
}

// TokenUsageTracker Token 用量追踪
type TokenUsageTracker struct {
	mu       sync.RWMutex
	records  []TokenRecord
	totalIn  int
	totalOut int
}

type TokenRecord struct {
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	Tenant       string    `json:"tenant,omitempty"` // TD-97 ③：租户级用量（空=未注入身份的历史记录）
	InputTokens  int       `json:"inputTokens"`
	OutputTokens int       `json:"outputTokens"`
	Timestamp    time.Time `json:"timestamp"`
}

type TokenUsageReport struct {
	TotalInputTokens  int            `json:"totalInputTokens"`
	TotalOutputTokens int            `json:"totalOutputTokens"`
	TotalTokens       int            `json:"totalTokens"`
	ByProvider        map[string]int `json:"byProvider"`
	ByModel           map[string]int `json:"byModel"`
	// Scope 回显本次报表的作用域（own=本租户 / all=平台全量，TD-97 ③④）——
	// 让调用方不必靠猜判断手里这份数字是什么口径。
	Scope string `json:"scope"`
}

func NewTokenUsageTracker() *TokenUsageTracker {
	return &TokenUsageTracker{
		records: make([]TokenRecord, 0),
	}
}

// Record 记一条用量（TD-97 ③：tenant 由调用方从 ctx 解析后传入，
// 空串 = 未注入身份的历史/单测路径）。
func (t *TokenUsageTracker) Record(provider, model, tenant string, input, output int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.records = append(t.records, TokenRecord{
		Provider:     provider,
		Model:        model,
		Tenant:       tenant,
		InputTokens:  input,
		OutputTokens: output,
		Timestamp:    time.Now(),
	})
	t.totalIn += input
	t.totalOut += output
}

// Report 出报表（TD-97 ③）：tenant 为空 = 平台全量口径（scope=all，
// 仅 default 租户持 platform:read 的调用方能拿到，见 handler）；
// 非空 = 该租户的本租户口径（scope=own，/ai/usage 的默认形态）。
// 注意：totalIn/totalOut 是进程级累计，作为 own 口径时按记录逐条过滤，
// 避免「看自己的数字却把别人算进来」。
func (t *TokenUsageTracker) Report(tenant string) *TokenUsageReport {
	t.mu.RLock()
	defer t.mu.RUnlock()

	report := &TokenUsageReport{
		ByProvider: make(map[string]int),
		ByModel:    make(map[string]int),
		Scope:      "all",
	}
	if tenant != "" {
		report.Scope = "own"
	}

	for _, r := range t.records {
		if tenant != "" && r.Tenant != tenant {
			continue
		}
		report.TotalInputTokens += r.InputTokens
		report.TotalOutputTokens += r.OutputTokens
		report.ByProvider[r.Provider] += r.InputTokens + r.OutputTokens
		report.ByModel[r.Model] += r.InputTokens + r.OutputTokens
	}
	report.TotalTokens = report.TotalInputTokens + report.TotalOutputTokens
	return report
}

// SemanticCache 语义缓存 (基于请求哈希)
type SemanticCache struct {
	mu      sync.RWMutex
	cache   map[string]*CompletionResponse
	maxSize int
}

func NewSemanticCache(maxSize int) *SemanticCache {
	return &SemanticCache{
		cache:   make(map[string]*CompletionResponse),
		maxSize: maxSize,
	}
}

func (sc *SemanticCache) Get(req CompletionRequest) *CompletionResponse {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	key := hashRequest(req)
	if resp, ok := sc.cache[key]; ok {
		return resp
	}
	return nil
}

func (sc *SemanticCache) Set(req CompletionRequest, resp *CompletionResponse) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if len(sc.cache) >= sc.maxSize {
		// 简单淘汰：清空一半
		count := 0
		for k := range sc.cache {
			delete(sc.cache, k)
			count++
			if count >= sc.maxSize/2 {
				break
			}
		}
	}

	key := hashRequest(req)
	sc.cache[key] = resp
}

// hashRequest 生成语义缓存键。
// 旧实现把各条 Content 的首字符做 h=h*31+c 连乘、再拼消息条数：实测
// ["a","bc"] 与 ["ab","c"] 等 14 种切分全部撞成同一键（1 条与 2 条消息的
// 总字符数相同也撞），缓存会把别的请求的答案当作命中返回；同时消息角色、
// temperature、maxTokens、tools 都不进键，改了参数仍命中旧结果。
// 这里按「角色+分隔符」逐项写入并混入采样参数。
func hashRequest(req CompletionRequest) string {
	h := fnv.New64a()
	fmt.Fprintf(h, "model=%s|maxTokens=%d|temperature=%g|topP=%g|stream=%v|tools=%d\n",
		req.Model, req.MaxTokens, req.Temperature, req.TopP, req.Stream, len(req.Tools))
	for _, m := range req.Messages {
		fmt.Fprintf(h, "%s\x00%s\x01", m.Role, m.Content)
	}
	return strconv.FormatUint(h.Sum64(), 16)
}
