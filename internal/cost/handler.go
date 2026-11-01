// Package cost 提供 AI 成本控制与观测：Token 预算、模型路由、成本追踪、报表生成。
package cost

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// ModelTier 模型层级
type ModelTier string

const (
	TierSmall  ModelTier = "small"  // 分类/提取/简单任务
	TierMedium ModelTier = "medium" // 一般对话/分析
	TierLarge  ModelTier = "large"  // 复杂推理/代码生成
)

// ModelSpec 模型规格
type ModelSpec struct {
	Name         string    `json:"name"`
	Provider     string    `json:"provider"`
	Tier         ModelTier `json:"tier"`
	MaxTokens    int       `json:"maxTokens"`
	CostPer1KIn  float64   `json:"costPer1KIn"`  // $/1K input tokens
	CostPer1KOut float64   `json:"costPer1KOut"` // $/1K output tokens
	Capabilities []string  `json:"capabilities"` // classify/reason/chat/code/embed
}

// TokenBudget Token 预算
type TokenBudget struct {
	TenantID       string    `json:"tenantId"`
	DailyLimit     int       `json:"dailyLimit"`
	MonthlyLimit   int       `json:"monthlyLimit"`
	UsedToday      int       `json:"usedToday"`
	UsedMonth      int       `json:"usedMonth"`
	AlertThreshold float64   `json:"alertThreshold"` // 0.8 = 80%
	LastReset      time.Time `json:"lastReset"`
	// Timezone 是配额窗口的切分时区（IANA 名，空=UTC）。日/月窗口按该时区的
	// 自然日/自然月零点切；按服务器时区切会让异地租户每月凭空损失数小时额度。
	Timezone string `json:"timezone"`
	// monthReset 记录 UsedMonth 的窗口起点（不对外序列化）。
	monthReset time.Time
	// alerted 记录本窗口已发过的告警键（`类型:阈值`）。没有它，越限后每一次写入
	// 都会再追加一条同样的告警，把列表刷成噪音；窗口切换时按对应类型清空。
	alerted map[string]bool
}

// BudgetManager 预算管理器
type BudgetManager struct {
	mu      sync.RWMutex
	budgets map[string]*TokenBudget
	alerts  []BudgetAlert
	// now 是时间注入缝：窗口边界用例不该等真实时钟跨过零点。
	now func() time.Time
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
		now:     time.Now,
	}
}

// location 解析窗口切分时区；非法名（只可能来自手搓结构，SetTimezone 已校验）退回 UTC。
func (b *TokenBudget) location() *time.Location {
	if b.Timezone == "" {
		return time.UTC
	}
	if loc, err := time.LoadLocation(b.Timezone); err == nil {
		return loc
	}
	return time.UTC
}

func dayStart(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

func monthStart(t time.Time, loc *time.Location) time.Time {
	y, m, _ := t.In(loc).Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, loc)
}

// normalizeLocked 把用量对齐到当前窗口（调用方须持写锁）。
// 读写两侧都要调：只在写入时归一的话，跨窗口后读到的仍是上个窗口的用量，
// 要等下一次写入才「追平」——对刚用完额度的租户等于凭空多显示一段历史消耗。
func (b *TokenBudget) normalizeLocked(now time.Time) {
	loc := b.location()
	if day := dayStart(now, loc); b.LastReset.IsZero() || day.After(b.LastReset) {
		b.UsedToday = 0
		b.LastReset = day
		b.clearAlerted("daily:")
	}
	if month := monthStart(now, loc); b.monthReset.IsZero() {
		b.monthReset = month
	} else if month.After(b.monthReset) {
		b.UsedMonth = 0
		b.monthReset = month
		b.clearAlerted("monthly:")
	}
}

// clearAlerted 丢弃该类型在本窗口的「已告警」标记，让新窗口能重新告警一次。
func (b *TokenBudget) clearAlerted(prefix string) {
	for key := range b.alerted {
		if strings.HasPrefix(key, prefix) {
			delete(b.alerted, key)
		}
	}
}

// overLimitLocked 判断是否真正打满额度（硬上限线，不是告警线）。
func (b *TokenBudget) overLimitLocked() bool {
	if b.DailyLimit > 0 && b.UsedToday >= b.DailyLimit {
		return true
	}
	return b.MonthlyLimit > 0 && b.UsedMonth >= b.MonthlyLimit
}

func (bm *BudgetManager) SetBudget(tenantID string, daily, monthly int, threshold float64) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	budget, ok := bm.budgets[tenantID]
	if !ok {
		budget = &TokenBudget{TenantID: tenantID}
		bm.budgets[tenantID] = budget
	}
	// 改额度不清用量：已发生的消耗是事实，否则「提额」顺带抹掉历史，
	// 变成可反复清零的套利项。
	budget.DailyLimit = daily
	budget.MonthlyLimit = monthly
	budget.AlertThreshold = threshold
	budget.normalizeLocked(bm.now())
}

// SetTimezone 设置租户配额窗口的切分时区（空串归一为 UTC）；非法时区名拒绝且不落库。
func (bm *BudgetManager) SetTimezone(tenantID, tz string) error {
	if tz == "" {
		tz = "UTC"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return fmt.Errorf("无效时区 %q: %w", tz, err)
	}
	bm.mu.Lock()
	defer bm.mu.Unlock()
	budget, ok := bm.budgets[tenantID]
	if !ok {
		budget = &TokenBudget{TenantID: tenantID, DailyLimit: 100000, MonthlyLimit: 1000000, AlertThreshold: 0.8}
		bm.budgets[tenantID] = budget
	}
	budget.Timezone = loc.String()
	budget.normalizeLocked(bm.now())
	return nil
}

func (bm *BudgetManager) GetBudget(tenantID string) *TokenBudget {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	if b, ok := bm.budgets[tenantID]; ok {
		b.normalizeLocked(bm.now())
		cp := *b
		return &cp
	}
	return &TokenBudget{TenantID: tenantID, DailyLimit: 100000, MonthlyLimit: 1000000, AlertThreshold: 0.8, Timezone: "UTC"}
}

// RecordUsage 累计用量并返回（是否还能继续, 本窗口新越过的告警）。
// 按软限口径：越过告警阈值只发通知，只有真正打满额度才让 ok=false。
func (bm *BudgetManager) RecordUsage(tenantID string, tokens int) (bool, []BudgetAlert) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	budget, ok := bm.budgets[tenantID]
	if !ok {
		budget = &TokenBudget{TenantID: tenantID, DailyLimit: 100000, MonthlyLimit: 1000000, AlertThreshold: 0.8}
		bm.budgets[tenantID] = budget
	}
	now := bm.now()
	budget.normalizeLocked(now)

	budget.UsedToday += tokens
	budget.UsedMonth += tokens

	alerts := budget.checkAlertLocked(now)
	if len(alerts) > 0 {
		bm.alerts = append(bm.alerts, alerts...)
	}
	return !budget.overLimitLocked(), alerts
}

// checkAlertLocked 返回本次写入**新越过**的阈值告警：日与月各自独立判定
// （旧实现日额度先 return，日+月同时超限时月度告警永不出现），且同一窗口内
// 同一「类型:阈值」只发一次。调用方须持写锁。
func (b *TokenBudget) checkAlertLocked(now time.Time) []BudgetAlert {
	var out []BudgetAlert
	collect := func(alertType string, used, limit int) {
		if limit <= 0 || float64(used)/float64(limit) < b.AlertThreshold {
			return
		}
		key := fmt.Sprintf("%s:%g", alertType, b.AlertThreshold)
		if b.alerted[key] {
			return
		}
		if b.alerted == nil {
			b.alerted = make(map[string]bool)
		}
		b.alerted[key] = true
		out = append(out, BudgetAlert{
			TenantID:  b.TenantID,
			Type:      alertType,
			Threshold: b.AlertThreshold,
			Used:      used,
			Limit:     limit,
			Timestamp: now,
		})
	}
	collect("daily", b.UsedToday, b.DailyLimit)
	collect("monthly", b.UsedMonth, b.MonthlyLimit)
	return out
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
	bm.mu.Lock()
	defer bm.mu.Unlock()
	budget, ok := bm.budgets[tenantID]
	if !ok {
		return false
	}
	budget.normalizeLocked(bm.now())
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
	TaskType  string    `json:"taskType"` // classify/extract/reason/chat/code
	Tier      ModelTier `json:"tier"`     // small/medium/large
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`
	MaxTokens int       `json:"maxTokens"`
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
	TotalCost         float64            `json:"totalCost"`
	TotalInputTokens  int                `json:"totalInputTokens"`
	TotalOutputTokens int                `json:"totalOutputTokens"`
	ByProvider        map[string]float64 `json:"byProvider"`
	ByModel           map[string]float64 `json:"byModel"`
	ByTenant          map[string]float64 `json:"byTenant"`
	ByTaskType        map[string]float64 `json:"byTaskType"`
	RecordCount       int                `json:"recordCount"`
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
	fmt.Fprintf(&sb, "# %s\n\n", title)
	fmt.Fprintf(&sb, "生成时间: %s\n\n", time.Now().Format("2006-01-02 15:04:05"))
	sb.WriteString("---\n\n")

	for _, section := range sections {
		fmt.Fprintf(&sb, "## %s\n\n", section.Title)
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
			Title: "成本概览",
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
	budget   *BudgetManager
	router   *ModelRouter
	tracker  *CostTracker
	reporter *ReportGenerator
	// Authorize 鉴权回调（由 controlplane 注入 subsystemAuthorize）：校验身份并核对权限点。
	// nil 时不启用鉴权（独立使用与单测的向后兼容路径）。口径与 ai/agent、ai/gateway、
	// knowledge 一致。
	Authorize func(w http.ResponseWriter, r *http.Request, perm string) (authctx.Context, bool)
}

// authorize 核对权限点；ok=false 时响应已由回调写完，调用方必须立即返回。
func (h *CostHandler) authorize(w http.ResponseWriter, r *http.Request, perm string) (authctx.Context, bool) {
	if h.Authorize == nil {
		return authctx.Context{}, true
	}
	return h.Authorize(w, r, perm)
}

// scopedTenant 决定这次请求作用在哪个租户：已验签上下文优先，仅在未启用鉴权（回调为
// nil，或开放模式返回空上下文）时才接受客户端自报的 tenantId。口径与 ai/agent 的
// identity() 一致——否则任何拿到 cost:read 的人改一下查询参数就能枚举别家的预算、
// 告警与报表，POST 还能替别家改额度。
// 代价（如实记录）：管理员的跨租户汇总视图因此需要一条显式的平台级权限，本批不做，
// 已在协调板上列为待 owner 定夺。
func scopedTenant(actx authctx.Context, r *http.Request) string {
	if actx.TenantID != "" {
		return actx.TenantID
	}
	return r.URL.Query().Get("tenantId")
}

// scopedTenantFromBody 同上，只是自报值来自请求体（POST /cost/budget）。
func scopedTenantFromBody(actx authctx.Context, bodyTenant string) string {
	if actx.TenantID != "" {
		return actx.TenantID
	}
	return bodyTenant
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
//
// 权限点按动词分支就近声明（不在「先算 perm 再统一 authorize」的辅助函数里）：
// 那样字面量就不在 authorize(...) 调用点行上，TestRolePermissions_CoversAuthorizeCallSites
// 这类源码守护会漏看（aiops 族当初就是这么漏的）。
func (h *CostHandler) HandleBudget(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		actx, ok := h.authorize(w, r, "cost:read")
		if !ok {
			return
		}
		budget := h.budget.GetBudget(scopedTenant(actx, r))
		paginate.WriteJSON(w, http.StatusOK, budget)
	case http.MethodPost:
		actx, ok := h.authorize(w, r, "cost:write")
		if !ok {
			return
		}
		var req struct {
			TenantID  string  `json:"tenantId"`
			Daily     int     `json:"daily"`
			Monthly   int     `json:"monthly"`
			Threshold float64 `json:"threshold"`
			Timezone  string  `json:"timezone"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
		// 额度改在谁身上以已验签上下文为准（自报 tenantId 只在未启用鉴权时生效）。
		tenantID := scopedTenantFromBody(actx, req.TenantID)
		// 时区非法要在改额度之前拒绝，避免出现「额度已改、窗口仍按旧时区切」的半应用。
		if req.Timezone != "" {
			if err := h.budget.SetTimezone(tenantID, req.Timezone); err != nil {
				paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
		}
		h.budget.SetBudget(tenantID, req.Daily, req.Monthly, req.Threshold)
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
	// 告警历史含预算阈值与消耗 ⇒ cost:read，且只看本租户。
	actx, ok := h.authorize(w, r, "cost:read")
	if !ok {
		return
	}
	alerts := h.budget.GetAlerts(scopedTenant(actx, r))
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
	// 报表是按租户聚合的消耗明细 ⇒ cost:read，且只看本租户。
	actx, ok := h.authorize(w, r, "cost:read")
	if !ok {
		return
	}
	tenantID := scopedTenant(actx, r)
	sinceStr := r.URL.Query().Get("since")
	var since time.Time
	if sinceStr != "" {
		// since 非法时明确拒绝（对齐 controlplane/audit_query.go 对时间范围参数的处理），
		// 避免静默忽略过滤条件导致客户端拿到全量报表误判成本口径。
		t, err := time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid 'since' time (use RFC3339): " + err.Error()})
			return
		}
		since = t
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
		if _, ok := h.authorize(w, r, "cost:read"); !ok {
			return
		}
		rules := h.router.GetRules()
		paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"rules": rules,
		})
	case http.MethodPost:
		// 路由规则决定后续每次调用用哪个模型（直接影响成本与数据去向）⇒ cost:write。
		if _, ok := h.authorize(w, r, "cost:write"); !ok {
			return
		}
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

	// 只是查「这个任务类型会路由到哪个模型」，不改规则 ⇒ cost:read（但它是全局规则，不能匿名）。
	if _, ok := h.authorize(w, r, "cost:read"); !ok {
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

	// 纯渲染（把调用方给的 sections 排成 markdown），不碰任何状态 ⇒ cost:read。
	if _, ok := h.authorize(w, r, "cost:read"); !ok {
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

// RegisterCostRoutes 注册成本控制路由。
//
// 权限映射（每个 handler 自查，见 CostHandler.Authorize）：
//   - cost:read  → GET /cost/budget、GET /cost/alerts、GET /cost/report、
//     GET /cost/routes、POST /cost/route、POST /cost/report/generate；
//   - cost:write → POST /cost/budget（改额度/阈值/时区）、POST /cost/routes（改路由规则）。
//
// 修复前这 8 条全部匿名可达，且 tenantId 由查询串/请求体自报——任何人不改参数就能
// 读别家的预算、报表与告警，POST 还能替别家改额度（G1 同族 + 身份自报双重缺口）。
// 现在 tenantId 以已验签上下文为准（scopedTenant）。
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
