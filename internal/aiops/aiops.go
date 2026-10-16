package aiops

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// Engine AIOps 引擎：协调 LLM 客户端、分析器、修复生成器与自然语言解析器。
//
// 使用方式：
//   cfg := config.Load()
//   cfg.AIOpsEnabled = true
//   cfg.AIOpsModel = "qvac/qwen3.5-0.8b"
//   cfg.AIOpsAPIKey = "your-key"
//   eng := aiops.NewEngine(cfg, s.store)
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
//
//   // 自动执行修复（生成的任务下发到 store，由 agent 拉取执行）
//   results, _ := eng.ExecuteRemediation(ctx, &ExecuteRemediationRequest{Plan: plan, AutoExecute: true})

// ExecutionRecord 单次执行记录（用于审计与追溯）。
type ExecutionRecord struct {
	ID        string    `json:"id"`
	PlanTitle string    `json:"planTitle"`
	Results   []*ExecuteResult `json:"results"`
	Dispatched int      `json:"dispatched"`
	Total     int      `json:"total"`
	CreatedAt time.Time `json:"createdAt"`
}

// Engine AIOps 引擎。
type Engine struct {
	cfg Config
	llm LLM
	an  *Analyzer
	gen *RemediationGenerator
	nl  *NLParser
	st  store.TaskStore // 任务下发：将修复命令创建为 proto.Task，由 agent 拉取执行

	mu           sync.RWMutex
	started      bool
	stats        EngineStats
	execHistory  []*ExecutionRecord // 执行历史记录（最近 100 条，内存留痕）
}

// NewEngine 构造 AIOps 引擎（从控制面 Config 构造，支持 --aiops-* flags / OPSMESH_AIOPS_* env）。
// st 为任务下发接口（可为 nil，此时 ExecuteRemediation 只生成计划不实际执行）。
func NewEngine(cfg *config.Config, st store.TaskStore) *Engine {
	acfg := Config{
		Enabled:     cfg.AIOpsEnabled,
		Provider:    Provider(cfg.AIOpsProvider),
		Model:       cfg.AIOpsModel,
		BaseURL:     cfg.AIOpsBaseURL,
		APIKey:      cfg.AIOpsAPIKey,
		Temperature: cfg.AIOpsTemperature,
		MaxTokens:   cfg.AIOpsMaxTokens,
		Timeout:     cfg.AIOpsTimeout,
	}
	e := &Engine{cfg: acfg, st: st}
	if e.cfg.MaxTokens <= 0 {
		e.cfg.MaxTokens = 4096
	}
	if e.cfg.Temperature <= 0 {
		e.cfg.Temperature = 0.3
	}
	if e.cfg.Timeout <= 0 {
		e.cfg.Timeout = 30
	}
	if e.cfg.Provider == "" {
		e.cfg.Provider = ProviderKilo
	}
	e.stats = EngineStats{
		Enabled:   e.cfg.Enabled,
		Provider:  string(e.cfg.Provider),
		Model:     e.cfg.Model,
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
	e.stats.StartedAt = time.Now()
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
	rc, err := e.an.Analyze(ctx, alertCtx)
	if err != nil {
		e.setLastErr(err.Error())
		return nil, err
	}
	e.incAnalyzes()
	return rc, nil
}

// GenerateRemediation 生成修复方案。
func (e *Engine) GenerateRemediation(ctx context.Context, req *RemediationRequest) (*RemediationPlan, error) {
	if !e.IsEnabled() {
		return nil, ErrDisabled
	}
	plan, err := e.gen.Generate(ctx, req)
	if err != nil {
		e.setLastErr(err.Error())
		return nil, err
	}
	e.incRemediations()
	return plan, nil
}

// NaturalLanguage 自然语言转结构化任务。
func (e *Engine) NaturalLanguage(ctx context.Context, input string, devices []string) (*RemediationPlan, error) {
	if !e.IsEnabled() {
		return nil, ErrDisabled
	}
	plan, err := e.nl.Parse(ctx, input, devices)
	if err != nil {
		e.setLastErr(err.Error())
		return nil, err
	}
	e.incNLParses()
	return plan, nil
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

// ExecuteRemediationRequest 自动执行修复请求。
type ExecuteRemediationRequest struct {
	Plan       *RemediationPlan `json:"plan"`        // 待执行的修复方案
	AutoExecute bool            `json:"autoExecute"` // true=直接下发任务；false=仅校验不下发（预检）
}

// ExecuteResult 单条修复命令的执行结果。
type ExecuteResult struct {
	CommandID string `json:"commandID"`        // 命令索引标识（cmd-0, cmd-1...）
	TaskID    string `json:"taskID"`           // 分配的 TaskID（空=未下发）
	AgentID   string `json:"agentID"`          // 目标 agent
	Status    string `json:"status"`           // dispatched / skipped / failed
	Reason    string `json:"reason,omitempty"` // skipped/failed 时的原因
}

// ExecuteRemediation 自动执行修复方案：将 RemediationPlan 中的 commands 下发为 proto.Task。
//
// 安全规则：
//   - plan.Safety.RequiresConfirm 为 true 时，AutoExecute 必须为 true，否则返回 skipped，
//     原因是 "requires manual confirmation"。
//   - plan.Safety.RiskLevel 为 "high" 时，强制 plan.Safety.RequiresConfirm 为 true，
//     否则全部返回 skipped，原因是 "high risk requires confirmation"。
//   - req.AutoExecute 为 false 时，所有命令均返回 skipped，原因是 "autoExecute disabled"（预检模式）。
//   - st 为 nil 时，所有命令均返回 skipped，原因是 "task store not available"。
func (e *Engine) ExecuteRemediation(ctx context.Context, req *ExecuteRemediationRequest) ([]*ExecuteResult, error) {
	if !e.IsEnabled() {
		return nil, ErrDisabled
	}
	if req == nil || req.Plan == nil {
		return nil, fmt.Errorf("aiops: execute request or plan is nil")
	}

	plan := req.Plan
	results := make([]*ExecuteResult, 0, len(plan.Commands))

	// 高风险操作必须要求确认
	if plan.Safety.RiskLevel == "high" && !plan.Safety.RequiresConfirm {
		for i, cmd := range plan.Commands {
			results = append(results, &ExecuteResult{
				CommandID: fmt.Sprintf("cmd-%d", i),
				AgentID:   cmd.AgentID,
				Status:    "skipped",
				Reason:    "high risk task requires manual confirmation",
			})
		}
		e.recordExecution(plan.Title, results)
		return results, nil
	}

	// 需要人工确认但未开启自动执行
	if plan.Safety.RequiresConfirm && !req.AutoExecute {
		for i, cmd := range plan.Commands {
			results = append(results, &ExecuteResult{
				CommandID: fmt.Sprintf("cmd-%d", i),
				AgentID:   cmd.AgentID,
				Status:    "skipped",
				Reason:    "requires manual confirmation",
			})
		}
		e.recordExecution(plan.Title, results)
		return results, nil
	}

	// 预检模式：不下发
	if !req.AutoExecute {
		for i, cmd := range plan.Commands {
			results = append(results, &ExecuteResult{
				CommandID: fmt.Sprintf("cmd-%d", i),
				AgentID:   cmd.AgentID,
				Status:    "skipped",
				Reason:    "autoExecute disabled (pre-check mode)",
			})
		}
		e.recordExecution(plan.Title, results)
		return results, nil
	}

	// 无任务存储可用
	if e.st == nil {
		for i, cmd := range plan.Commands {
			results = append(results, &ExecuteResult{
				CommandID: fmt.Sprintf("cmd-%d", i),
				AgentID:   cmd.AgentID,
				Status:    "skipped",
				Reason:    "task store not available",
			})
		}
		e.recordExecution(plan.Title, results)
		return results, nil
	}

	// 下发每条命令为 proto.Task
	dispatched := 0
	for i, cmd := range plan.Commands {
		task := &proto.Task{
			AgentID:   cmd.AgentID,
			TenantID:  "", // 由 handler 注入租户
			Type:      cmd.Type,
			Command:   cmd.Command,
			Path:      "",
			Content:   "",
			Status:    "pending",
			Timeout:   cmd.Timeout,
			RetryCount: 0,
			MaxRetries: 3,
			DeadLetter: false,
			RetryDelay: 0,
			CreatedAt:  time.Now(),
		}
		// 修复类型映射：shell/service/file
		if task.Type == "" {
			task.Type = proto.TaskTypeShell
		}
		if task.MaxRetries <= 0 {
			task.MaxRetries = 3
		}

		created, err := e.st.CreateTask(task)
		if err != nil {
			results = append(results, &ExecuteResult{
				CommandID: fmt.Sprintf("cmd-%d", i),
				AgentID:   cmd.AgentID,
				Status:    "failed",
				Reason:    err.Error(),
			})
			continue
		}
		dispatched++
		results = append(results, &ExecuteResult{
			CommandID: fmt.Sprintf("cmd-%d", i),
			TaskID:    created.TaskID,
			AgentID:   cmd.AgentID,
			Status:    "dispatched",
		})
	}

	e.incExecutedRemediations(int64(dispatched))
	e.recordExecution(plan.Title, results)
	return results, nil
}

// recordExecution 记录一次执行到历史记录（最近 100 条）。
func (e *Engine) recordExecution(planTitle string, results []*ExecuteResult) {
	e.mu.Lock()
	defer e.mu.Unlock()

	dispatched := 0
	for _, r := range results {
		if r.Status == "dispatched" {
			dispatched++
		}
	}

	rec := &ExecutionRecord{
		ID:         fmt.Sprintf("exec-%d", time.Now().UnixNano()),
		PlanTitle:  planTitle,
		Results:    results,
		Dispatched: dispatched,
		Total:      len(results),
		CreatedAt:  time.Now(),
	}

	e.execHistory = append([]*ExecutionRecord{rec}, e.execHistory...)
	if len(e.execHistory) > 100 {
		e.execHistory = e.execHistory[:100]
	}
}

// ExecutionHistory 返回最近的执行历史记录。
func (e *Engine) ExecutionHistory(limit int) []*ExecutionRecord {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if limit <= 0 || limit > len(e.execHistory) {
		limit = len(e.execHistory)
	}
	out := make([]*ExecutionRecord, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, e.execHistory[i])
	}
	return out
}

// incAnalyzes 累计分析次数。
func (e *Engine) incAnalyzes() {
	e.mu.Lock()
	e.stats.AnalyzesTotal++
	e.mu.Unlock()
}

// incRemediations 累计修复方案生成次数。
func (e *Engine) incRemediations() {
	e.mu.Lock()
	e.stats.Remediations++
	e.mu.Unlock()
}

// incNLParses 累计自然语言解析次数。
func (e *Engine) incNLParses() {
	e.mu.Lock()
	e.stats.NLParses++
	e.mu.Unlock()
}

// incExecutedRemediations 累计已下发任务数。
func (e *Engine) incExecutedRemediations(n int64) {
	e.mu.Lock()
	e.stats.ExecutedRemediations += n
	e.mu.Unlock()
}

// setLastErr 记录最后一次错误。
func (e *Engine) setLastErr(msg string) {
	e.mu.Lock()
	e.stats.LastErr = msg
	e.mu.Unlock()
}

// ErrDisabled AIOps 未启用。
var ErrDisabled = fmt.Errorf("aiops: engine not enabled")

// EngineStats AIOps 引擎统计。
type EngineStats struct {
	Enabled              bool      `json:"enabled"`
	Provider             string    `json:"provider"`
	Model                string    `json:"model"`
	StartedAt            time.Time `json:"startedAt,omitempty"`
	AnalyzesTotal        int64     `json:"analyzesTotal"`
	Remediations         int64     `json:"remediations"`
	NLParses             int64     `json:"nlParses"`
	ExecutedRemediations int64     `json:"executedRemediations"`
	LastErr              string    `json:"lastErr,omitempty"`
}

// Stats 返回引擎统计信息。
func (e *Engine) Stats() EngineStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	s := EngineStats{
		Enabled:             e.cfg.Enabled,
		Provider:            string(e.cfg.Provider),
		Model:               e.cfg.Model,
		AnalyzesTotal:       e.stats.AnalyzesTotal,
		Remediations:        e.stats.Remediations,
		NLParses:            e.stats.NLParses,
		ExecutedRemediations: e.stats.ExecutedRemediations,
		LastErr:             e.stats.LastErr,
	}
	if e.started {
		s.StartedAt = e.stats.StartedAt
	}
	return s
}
