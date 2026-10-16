package aiops

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Engine AIOps 引擎：协调 LLM 客户端、分析器、修复生成器与自然语言解析器。
//
// 使用方式：
//   cfg := aiops.DefaultConfig()
//   cfg.Enabled = true
//   cfg.APIKey = "your-key"
//   eng := aiops.NewEngine(cfg)
//   eng.Start(ctx)
//
//   // 故障根因分析
//   rc, _ := eng.Analyze(ctx, alertCtx)
//
//   // 生成修复方案
//   plan, _ := eng.GenerateRemediation(ctx, &RemediationRequest{RootCause: rc, ...})
//
//   // 自然语言任务
//   plan, _ := eng.NaturalLanguage(ctx, "重启设备 abc 的 nginx 服务", devices)

// Engine AIOps 引擎。
type Engine struct {
	cfg Config
	llm LLM
	an  *Analyzer
	gen *RemediationGenerator
	nl  *NLParser

	mu      sync.RWMutex
	started bool
}

// NewEngine 构造 AIOps 引擎。
func NewEngine(cfg Config) *Engine {
	e := &Engine{cfg: cfg}
	if e.cfg.MaxTokens <= 0 {
		e.cfg.MaxTokens = 4096
	}
	if e.cfg.Temperature <= 0 {
		e.cfg.Temperature = 0.3
	}
	if e.cfg.Provider == "" {
		e.cfg.Provider = ProviderKilo
	}
	return e
}

// Start 初始化 LLM 客户端（懒加载）。
func (e *Engine) Start(ctx context.Context) error {
	if !e.cfg.Enabled {
		return nil
	}
	if err := e.cfg.Validate(); err != nil {
		return err
	}

	e.llm = NewHTTPClient(e.cfg)
	e.an = NewAnalyzer(e.llm, e.cfg)
	e.gen = NewRemediationGenerator(e.llm, e.cfg)
	e.nl = NewNLParser(e.llm, e.cfg)

	e.mu.Lock()
	e.started = true
	e.mu.Unlock()
	return nil
}

// IsEnabled 返回 AIOps 是否启用。
func (e *Engine) IsEnabled() bool {
	return e.cfg.Enabled && e.cfg.Model != ""
}

// Analyze 执行故障根因分析。
func (e *Engine) Analyze(ctx context.Context, alertCtx *AlertContext) (*RootCause, error) {
	if !e.IsEnabled() {
		return nil, ErrDisabled
	}
	return e.an.Analyze(ctx, alertCtx)
}

// GenerateRemediation 生成修复方案。
func (e *Engine) GenerateRemediation(ctx context.Context, req *RemediationRequest) (*RemediationPlan, error) {
	if !e.IsEnabled() {
		return nil, ErrDisabled
	}
	return e.gen.Generate(ctx, req)
}

// NaturalLanguage 自然语言转结构化任务。
func (e *Engine) NaturalLanguage(ctx context.Context, input string, devices []string) (*RemediationPlan, error) {
	if !e.IsEnabled() {
		return nil, ErrDisabled
	}
	return e.nl.Parse(ctx, input, devices)
}

// AnalyzeAndRemediate 一体化：分析 + 生成修复。
func (e *Engine) AnalyzeAndRemediate(ctx context.Context, alertCtx *AlertContext, constraints RemediationConstraints) (*RootCause, *RemediationPlan, error) {
	if !e.IsEnabled() {
		return nil, nil, ErrDisabled
	}

	rc, err := e.Analyze(ctx, alertCtx)
	if err != nil {
		return nil, nil, fmt.Errorf("aiops: root cause analysis failed: %w", err)
	}

	req := &RemediationRequest{
		RootCause:     rc,
		DeviceContext: alertCtx.Metrics,
		AlertContext:  alertCtx,
		Constraints:   constraints,
	}

	plan, err := e.GenerateRemediation(ctx, req)
	if err != nil {
		return rc, nil, fmt.Errorf("aiops: remediation generation failed: %w", err)
	}

	return rc, plan, nil
}

// ErrDisabled AIOps 未启用。
var ErrDisabled = fmt.Errorf("aiops: engine not enabled")

// EngineStats AIOps 引擎统计。
type EngineStats struct {
	Enabled       bool      `json:"enabled"`
	Provider      string    `json:"provider"`
	Model         string    `json:"model"`
	StartedAt     time.Time `json:"startedAt,omitempty"`
	AnalyzesTotal int64     `json:"analyzesTotal"`
	Remediations  int64     `json:"remediations"`
	NLParses      int64     `json:"nlParses"`
	LastErr       string    `json:"lastErr,omitempty"`
}

// Stats 返回引擎统计信息。
func (e *Engine) Stats() EngineStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	s := EngineStats{
		Enabled:  e.cfg.Enabled,
		Provider: string(e.cfg.Provider),
		Model:    e.cfg.Model,
	}
	if e.started {
		s.StartedAt = time.Now()
	}
	return s
}
