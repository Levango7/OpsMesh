// Package cost 提供 AI 成本控制与观测：Token 预算、模型路由、成本追踪、报表生成。
package cost

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// ModelTier 模型层级
type ModelTier string

const (
	TierSmall   ModelTier = "small"   // 分类/提取/简单任务
	TierMedium  ModelTier = "medium"  // 一般对话/分析
	TierLarge   ModelTier = "large"   // 复杂推理/代码生成
)

// ModelSpec 模型规格
type ModelSpec struct {
	Name        string    `json:"name"`
	Provider    string    `json:"provider"`
	Tier        ModelTier `json:"tier"`
	MaxTokens   int       `json:"maxTokens"`
	CostPer1KIn float64   `json:"costPer1KIn"`  // $/1K input tokens
	CostPer1KOut float64  `json:"costPer1KOut"` // $/1K output tokens
	Capabilities []string `json:"capabilities"` // classify/reason/chat/code/embed
}

// TokenBudget Token 预算
type TokenBudget struct {
	TenantID     string    `json:"tenantId"`
	DailyLimit   int       `json:"dailyLimit"`
	MonthlyLimit int       `json:"monthlyLimit"`
	UsedToday    int       `json:"usedToday"`
	UsedMonth    int       `json:"usedMonth"`
	AlertThreshold float64 `json:"alertThreshold"` // 0.8 = 80%
	LastReset    time.Time `json:"lastReset"`
}

// BudgetManager 预算管理器
type BudgetManager struct {
	mu      sync.RWMutex
	budgets map[string]*TokenBudget
	alerts  []BudgetAlert
}

type BudgetAlert struct {
	TenantID  string    `json:"tenantId"`
	Type      string    `json:"type"` // daily/monthly
	Threshold float64   `json:"threshold"`
	Used      int       `json:"used"`
	Limit     int       `json:"limit"`
	Timestamp time.Time `json:"timestamp"`
}

func NewBudgetManager() *BudgetManager {
	return &BudgetManager{
		budgets: make(map[string]*TokenBudget),
		alerts:  make([]BudgetAlert, 0),
	}
}

func (bm *BudgetManager) SetBudget(tenantID string, daily, monthly int, threshold float64) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.budgets[tenantID] = &TokenBudget{
		TenantID:       tenantID,
		DailyLimit:     daily,
		MonthlyLimit:   monthly,
		AlertThreshold: threshold,
		LastReset:      time.Now(),
	}
}

func (bm *BudgetManager) GetBudget(tenantID string) *TokenBudget {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	if b, ok := bm.budgets[tenantID]; ok {
		cp := *b
		return &cp
	}
	return &TokenBudget{TenantID: tenantID, DailyLimit: 100000, MonthlyLimit: 1000000, AlertThreshold: 0.8}
}

func (bm *BudgetManager) RecordUsage(tenantID string, tokens int) (bool, *BudgetAlert) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	budget, ok := bm.budgets[tenantID]
	if !ok {
		budget = &TokenBudget{TenantID: tenantID, DailyLimit: 100000, MonthlyLimit: 1000000, AlertThreshold: 0.8, LastReset: time.Now()}
		bm.budgets[tenantID] = budget
	}

	// 重置每日计数
	if time.Since(budget.LastReset) > 24*time.Hour {
		budget.UsedToday = 0
		budget.LastReset = time.Now()
	}

	budget.UsedToday += tokens
	budget.UsedMonth += tokens

	// 检查预算
	alert := bm.checkAlert(budget)
	if alert != nil {
		bm.alerts = append(bm.alerts, *alert)
		return false, alert
	}
	return true, nil
}

func (bm *BudgetManager) checkAlert(budget *TokenBudget) *BudgetAlert {
	if budget.DailyLimit > 0 {
		ratio := float64(budget.UsedToday) / float64(budget.DailyLimit)
		if ratio >= budget.AlertThreshold {
			return &BudgetAlert{
				TenantID:  budget.TenantID,
				Type:      "daily",
				Threshold: budget.AlertThreshold,
				Used:      budget.UsedToday,
				Limit:     budget.DailyLimit,
				Timestamp: time.Now(),
			}
		}
	}
	if budget.MonthlyLimit > 0 {
		ratio := float64(budget.UsedMonth) / float64(budget.MonthlyLimit)
		if ratio >= budget.AlertThreshold {
			return &BudgetAlert{
				TenantID:  budget.TenantID,
				Type:      "monthly",
				Threshold: budget.AlertThreshold,
				Used:      budget.UsedMonth,
				Limit:     budget.MonthlyLimit,
				Timestamp: time.Now(),
			}
		}
	}
	return nil
}

func (bm *BudgetManager) GetAlerts(tenantID string) []BudgetAlert {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	out := make([]BudgetAlert, 0)
	for _, a := range bm.alerts {
		if tenantID == "" || a.TenantID == tenantID {
			out = append(out, a)
		}
	}
	return out
}

func (bm *BudgetManager) IsOverBudget(tenantID string) bool {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	budget, ok := bm.budgets[tenantID]
	if !ok {
		return false
	}
	if budget.DailyLimit > 0 && budget.UsedToday >= budget.DailyLimit {
		return true
	}
	if budget.MonthlyLimit > 0 && budget.UsedMonth >= budget.MonthlyLimit {
		return true
	}
	return false
}

// ============================================================================
// Model Router 模型路由器
// ============================================================================

// ModelRouter 任务感知模型路由器
type ModelRouter struct {
	mu     sync.RWMutex
	models []ModelSpec
	rules  []RoutingRule
}

type RoutingRule struct {
	TaskType   string    `json:"taskType"`   // classify/extract/reason/chat/code
	Tier       ModelTier `json:"tier"`       // small/medium/large
	Provider   string    `json:"provider"`
	Model      string    `json:"model"`
	MaxTokens  int       `json:"maxTokens"`
}

func NewModelRouter() *ModelRouter {
	return &ModelRouter{
		models: make([]ModelSpec, 0),
		rules:  defaultRoutingRules(),
	}
}

func defaultRoutingRules() []RoutingRule {
	return []RoutingRule{
		{TaskType: "classify", Tier: TierSmall, Provider: "openai", Model: "gpt-4o-mini", MaxTokens: 256},
		{TaskType: "extract", Tier: TierSmall, Provider: "openai", Model: "gpt-4o-mini", MaxTokens: 512},
		{TaskType: "reason", Tier: TierLarge, Provider: "openai", Model: "gpt-4o", MaxTokens: 4096},
		{TaskType: "chat", Tier: TierMedium, Provider: "openai", Model: "gpt-4o-mini", MaxTokens: 2048},
		{TaskType: "code", Tier: TierLarge, Provider: "openai", Model: "gpt-4o", MaxTokens: 4096},
		{TaskType: "summarize", Tier: TierSmall, Provider: "openai", Model: "gpt-4o-mini", MaxTokens: 1024},
	}
}

func (mr *ModelRouter) RegisterModel(spec ModelSpec) {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	mr.models = append(mr.models, spec)
}

func (mr *ModelRouter) SetRules(rules []RoutingRule) {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	mr.rules = rules
}

func (mr *ModelRouter) GetRules() []RoutingRule {
	mr.mu.RLock()
	defer mr.mu.RUnlock()
	out := make([]RoutingRule, len(mr.rules))
	copy(out, mr.rules)
	return out
}

// Route 根据任务类型选择模型
func (mr *ModelRouter) Route(taskType string) RoutingRule {
	mr.mu.RLock()
	defer mr.mu.RUnlock()

	for _, rule := range mr.rules {
		if rule.TaskType == taskType {
			return rule
		}
	}
	// 默认：medium tier
	return RoutingRule{TaskType: taskType, Tier: TierMedium, Provider: "openai", Model: "gpt-4o-mini", MaxTokens: 2048}
}

// GetModelsByTier 获取指定层级的模型列表
func (mr *ModelRouter) GetModelsByTier(tier ModelTier) []ModelSpec {
	mr.mu.RLock()
	defer mr.mu.RUnlock()
	out := make([]ModelSpec, 0)
	for _, m := range mr.models {
		if m.Tier == tier {
			out = append(out, m)
		}
	}
	return out
}

// ============================================================================
// Cost Tracker 成本追踪
// ============================================================================

// CostRecord 成本记录
type CostRecord struct {
	TenantID     string    `json:"tenantId"`
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	InputTokens  int       `json:"inputTokens"`
	OutputTokens int       `json:"outputTokens"`
	Cost         float64   `json:"cost"`
	Timestamp    time.Time `json:"timestamp"`
	TaskType     string    `json:"taskType,omitempty"`
}

// CostTracker 成本追踪器
type CostTracker struct {
	mu      sync.RWMutex
	records []CostRecord
	pricing map[string]ModelSpec // model -> spec
}

func NewCostTracker() *CostTracker {
	return &CostTracker{
		records: make([]CostRecord, 0),
		pricing: make(map[string]ModelSpec),
	}
}

func (ct *CostTracker) RegisterPricing(spec ModelSpec) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.pricing[spec.Name] = spec
}

func (ct *CostTracker) Record(tenantID, provider, model string, inputTokens, outputTokens int, taskType string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	cost := 0.0
	if spec, ok := ct.pricing[model]; ok {
		cost = float64(inputTokens)/1000.0*spec.CostPer1KIn + float64(outputTokens)/1000.0*spec.CostPer1KOut
	}

	ct.records = append(ct.records, CostRecord{
		TenantID:     tenantID,
		Provider:     provider,
		Model:        model,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		Cost:         cost,
		Timestamp:    time.Now(),
		TaskType:     taskType,
	})
}

// CostReport 成本报告
type CostReport struct {
	TotalCost       float64            `json:"totalCost"`
	TotalInputTokens int                `json:"totalInputTokens"`
	TotalOutputTokens int              `json:"totalOutputTokens"`
	ByProvider      map[string]float64 `json:"byProvider"`
	ByModel         map[string]float64 `json:"byModel"`
	ByTenant        map[string]float64 `json:"byTenant"`
	ByTaskType      map[string]float64 `json:"byTaskType"`
	RecordCount     int                `json:"recordCount"`
}

func (ct *CostTracker) Report(tenantID string, since time.Time) *CostReport {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	report := &CostReport{
		ByProvider: make(map[string]float64),
		ByModel:    make(map[string]float64),
		ByTenant:   make(map[string]float64),
		ByTaskType: make(map[string]float64),
	}

	for _, r := range ct.records {
		if tenantID != "" && r.TenantID != tenantID {
			continue
		}
		if !since.IsZero() && r.Timestamp.Before(since) {
			continue
		}
		report.TotalCost += r.Cost
		report.TotalInputTokens += r.InputTokens
		report.TotalOutputTokens += r.OutputTokens
		report.ByProvider[r.Provider] += r.Cost
		report.ByModel[r.Model] += r.Cost
		report.ByTenant[r.TenantID] += r.Cost
		if r.TaskType != "" {
			report.ByTaskType[r.TaskType] += r.Cost
		}
		report.RecordCount++
	}

	return report
}

// ============================================================================
// Report Generator 报表生成器
// ============================================================================

// ReportGenerator 报表生成器
type ReportGenerator struct{}

func NewReportGenerator() *ReportGenerator {
	return &ReportGenerator{}
}

// GenerateMarkdown 生成 Markdown 报告
func (rg *ReportGenerator) GenerateMarkdown(title string, sections []ReportSection) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", title))
	sb.WriteString(fmt.Sprintf("生成时间: %s\n\n", time.Now().Format("2006-01-02 15:04:05")))
	sb.WriteString("---\n\n")

	for _, section := range sections {
		sb.WriteString(fmt.Sprintf("## %s\n\n", section.Title))
		if section.Content != "" {
			sb.WriteString(section.Content + "\n\n")
		}
		if len(section.Table) > 0 {
			// 表头
			if len(section.Table) > 0 {
				header := section.Table[0]
				sb.WriteString("| " + strings.Join(header, " | ") + " |\n")
				sb.WriteString("|" + strings.Repeat("---|", len(header)) + "\n")
				for i := 1; i < len(section.Table); i++ {
					sb.WriteString("| " + strings.Join(section.Table[i], " | ") + " |\n")
				}
				sb.WriteString("\n")
			}
		}
		if section.Footer != "" {
			sb.WriteString(section.Footer + "\n\n")
		}
	}

	return sb.String()
}

// ReportSection 报表章节
type ReportSection struct {
	Title   string     `json:"title"`
	Content string     `json:"content"`
	Table   [][]string `json:"table,omitempty"`
	Footer  string     `json:"footer,omitempty"`
}

// GenerateCostReport 生成成本报告 (Markdown)
func (rg *ReportGenerator) GenerateCostReport(report *CostReport, title string) string {
	sections := []ReportSection{
		{
			Title:   "成本概览",
			Content: fmt.Sprintf("总成本: $%.4f\n总输入 Token: %d\n总输出 Token: %d\n记录数: %d",
				report.TotalCost, report.TotalInputTokens, report.TotalOutputTokens, report.RecordCount),
		},
	}

	// 按提供商
	if len(report.ByProvider) > 0 {
		table := [][]string{{"提供商", "成本 ($)"}}
		for k, v := range report.ByProvider {
			table = append(table, []string{k, fmt.Sprintf("%.4f", v)})
		}
		sections = append(sections, ReportSection{Title: "按提供商", Table: table})
	}

	// 按模型
	if len(report.ByModel) > 0 {
		table := [][]string{{"模型", "成本 ($)"}}
		for k, v := range report.ByModel {
			table = append(table, []string{k, fmt.Sprintf("%.4f", v)})
		}
		sections = append(sections, ReportSection{Title: "按模型", Table: table})
	}

	// 按租户
	if len(report.ByTenant) > 0 {
		table := [][]string{{"租户", "成本 ($)"}}
		for k, v := range report.ByTenant {
			table = append(table, []string{k, fmt.Sprintf("%.4f", v)})
		}
		sections = append(sections, ReportSection{Title: "按租户", Table: table})
	}

	// 按任务类型
	if len(report.ByTaskType) > 0 {
		table := [][]string{{"任务类型", "成本 ($)"}}
		for k, v := range report.ByTaskType {
			table = append(table, []string{k, fmt.Sprintf("%.4f", v)})
		}
		sections = append(sections, ReportSection{Title: "按任务类型", Table: table})
	}

	return rg.GenerateMarkdown(title, sections)
}

// ============================================================================
// Cost Handler HTTP 接口
// ============================================================================

// CostHandler 成本控制 HTTP handler
type CostHandler struct {
	budget  *BudgetManager
	router  *ModelRouter
	tracker *CostTracker
	reporter *ReportGenerator
}

func NewCostHandler(bm *BudgetManager, mr *ModelRouter, ct *CostTracker) *CostHandler {
	return &CostHandler{
		budget:   bm,
		router:   mr,
		tracker:  ct,
		reporter: NewReportGenerator(),
	}
}

// HandleBudget 处理 GET/POST /api/v1/cost/budget
func (h *CostHandler) HandleBudget(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		tenantID := r.URL.Query().Get("tenantId")
		budget := h.budget.GetBudget(tenantID)
		paginate.WriteJSON(w, http.StatusOK, budget)
	case http.MethodPost:
		var req struct {
			TenantID  string  `json:"tenantId"`
			Daily     int     `json:"daily"`
			Monthly   int     `json:"monthly"`
			Threshold float64 `json:"threshold"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
		h.budget.SetBudget(req.TenantID, req.Daily, req.Monthly, req.Threshold)
		paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "updated"})
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// HandleBudgetAlerts 处理 GET /api/v1/cost/alerts
func (h *CostHandler) HandleBudgetAlerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	tenantID := r.URL.Query().Get("tenantId")
	alerts := h.budget.GetAlerts(tenantID)
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"alerts": alerts,
		"count":  len(alerts),
	})
}

// HandleCostReport 处理 GET /api/v1/cost/report
func (h *CostHandler) HandleCostReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	tenantID := r.URL.Query().Get("tenantId")
	sinceStr := r.URL.Query().Get("since")
	var since time.Time
	if sinceStr != "" {
		since, _ = time.Parse(time.RFC3339, sinceStr)
	}

	report := h.tracker.Report(tenantID, since)
	format := r.URL.Query().Get("format")

	if format == "markdown" {
		md := h.reporter.GenerateCostReport(report, "AI Cost Report")
		w.Header().Set("Content-Type", "text/markdown")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(md))
		return
	}

	paginate.WriteJSON(w, http.StatusOK, report)
}

// HandleModelRoute 处理 GET/POST /api/v1/cost/routes
func (h *CostHandler) HandleModelRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rules := h.router.GetRules()
		paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"rules": rules,
		})
	case http.MethodPost:
		var req struct {
			Rules []RoutingRule `json:"rules"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
		h.router.SetRules(req.Rules)
		paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "updated"})
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// HandleRoute 处理 POST /api/v1/cost/route (单个任务路由查询)
func (h *CostHandler) HandleRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		TaskType string `json:"taskType"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	rule := h.router.Route(req.TaskType)
	paginate.WriteJSON(w, http.StatusOK, rule)
}

// HandleGenerateReport 处理 POST /api/v1/cost/report/generate
func (h *CostHandler) HandleGenerateReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		Title    string          `json:"title"`
		Sections []ReportSection `json:"sections"`
		Format   string          `json:"format"` // markdown
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if req.Title == "" {
		req.Title = "OpsMesh Report"
	}

	rg := NewReportGenerator()
	md := rg.GenerateMarkdown(req.Title, req.Sections)

	if req.Format == "markdown" || req.Format == "" {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(md))
		return
	}

	paginate.WriteJSON(w, http.StatusOK, map[string]string{"markdown": md})
}

// RegisterCostRoutes 注册成本控制路由
func RegisterCostRoutes(mux *http.ServeMux, handler *CostHandler) {
	mux.HandleFunc("GET /api/v1/cost/budget", handler.HandleBudget)
	mux.HandleFunc("POST /api/v1/cost/budget", handler.HandleBudget)
	mux.HandleFunc("GET /api/v1/cost/alerts", handler.HandleBudgetAlerts)
	mux.HandleFunc("GET /api/v1/cost/report", handler.HandleCostReport)
	mux.HandleFunc("GET /api/v1/cost/routes", handler.HandleModelRoute)
	mux.HandleFunc("POST /api/v1/cost/routes", handler.HandleModelRoute)
	mux.HandleFunc("POST /api/v1/cost/route", handler.HandleRoute)
	mux.HandleFunc("POST /api/v1/cost/report/generate", handler.HandleGenerateReport)
}
