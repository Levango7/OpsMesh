package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/services/portal-svc/internal/cost"
	"github.com/Levango7/OpsMesh/services/portal-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/portal-svc/internal/service"
)

// Handler handles HTTP requests for the portal.
type Handler struct {
	svc   *service.Service
	alloc *cost.Allocator
}

// NewHandler creates a new Handler.
func NewHandler(svc *service.Service) *Handler {
	return &Handler{svc: svc, alloc: cost.NewAllocator()}
}

// RegisterRoutes registers all API routes.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Resource requests
	mux.HandleFunc("/api/v1/requests", h.handleRequests)
	mux.HandleFunc("/api/v1/requests/", h.handleRequestDetail)

	// Approval queue (frontend contract: GET /approvals, POST /approvals/{id}/{approve,reject})
	mux.HandleFunc("/api/v1/approvals", h.handleApprovals)
	mux.HandleFunc("/api/v1/approvals/", h.handleApprovalDetail)

	// Cost overview (frontend aggregate contract)
	mux.HandleFunc("/api/v1/cost", h.handleCostOverview)

	// Cost optimization
	mux.HandleFunc("/api/v1/cost/utilization", h.handleUtilization)
	mux.HandleFunc("/api/v1/cost/recommendations", h.handleRecommendations)
	mux.HandleFunc("/api/v1/cost/savings", h.handleSavings)
	mux.HandleFunc("/api/v1/cost/budget", h.handleBudget)

	// Cost allocation / chargeback
	mux.HandleFunc("/api/v1/cost/allocate", h.handleCostAllocate)
	mux.HandleFunc("/api/v1/cost/allocation-report", h.handleAllocationReport)
	mux.HandleFunc("/api/v1/cost/allocation-rules", h.handleAllocationRules)

	// Quota management
	mux.HandleFunc("/api/v1/quotas", h.handleQuotas)
	mux.HandleFunc("/api/v1/quotas/", h.handleQuotaDetail)

	// Dashboard
	mux.HandleFunc("/api/v1/dashboard/stats", h.handleDashboardStats)
	mux.HandleFunc("/api/v1/dashboard/activity", h.handleDashboardActivity)
}

// ============================================================================
// Resource Requests
// ============================================================================

func (h *Handler) handleRequests(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.createRequest(w, r)
	case http.MethodGet:
		h.listRequests(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleRequestDetail(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/requests/")
	if path == "" {
		writeError(w, http.StatusBadRequest, "request ID required")
		return
	}

	// Handle approve/reject sub-paths
	if strings.HasSuffix(path, "/approve") {
		id := strings.TrimSuffix(path, "/approve")
		if r.Method == http.MethodPost {
			h.approveRequest(w, r, id)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if strings.HasSuffix(path, "/reject") {
		id := strings.TrimSuffix(path, "/reject")
		if r.Method == http.MethodPost {
			h.rejectRequest(w, r, id)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	id := path
	switch r.Method {
	case http.MethodGet:
		h.getRequest(w, r, id)
	case http.MethodPut:
		h.updateRequest(w, r, id)
	case http.MethodDelete:
		h.cancelRequest(w, r, id)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// requestView 是资源请求/审批队列的前端契约形态，字段名与
// web/enterprise/src/api/portal.js 的契约注释逐字对齐（type/resource/requester/
// cost/createdAt）——PortalView 的表格列直接按这些键取值，snake_case 直出会让
// 列全空（TD-60 阶段 1 四域同类"字段断裂"的 portal 域版本）。
type requestView struct {
	ID         string     `json:"id"`
	Type       string     `json:"type"`
	Resource   string     `json:"resource"`
	Status     string     `json:"status"`
	Requester  string     `json:"requester"`
	Reason     string     `json:"reason,omitempty"`
	Cost       float64    `json:"cost"`
	CreatedAt  time.Time  `json:"createdAt"`
	ApprovedAt *time.Time `json:"approvedAt,omitempty"`
}

func toRequestView(r *models.ResourceRequest) requestView {
	v := requestView{
		ID:        r.ID,
		Type:      r.ResourceType,
		Resource:  r.Title,
		Status:    string(r.Status),
		Requester: r.Requester,
		Reason:    r.Description,
		Cost:      r.CostEstimate,
		CreatedAt: r.CreatedAt,
	}
	if r.Status == models.StatusApproved {
		t := r.UpdatedAt
		v.ApprovedAt = &t
	}
	return v
}

// createRequestInput 同时兼容两种请求形态：
//   - 前端契约（web/enterprise/src/api/portal.js）：{type,resource,params,reason}；
//   - 既有 snake_case 形态：{tenant_id,requester,title,description,resource_type,cpu,…}。
type createRequestInput struct {
	Type     string `json:"type"`
	Resource string `json:"resource"`
	Params   string `json:"params"`
	Reason   string `json:"reason"`

	TenantID     string `json:"tenant_id"`
	Requester    string `json:"requester"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	ResourceType string `json:"resource_type"`
	CPU          int    `json:"cpu"`
	MemoryGB     int    `json:"memory_gb"`
	StorageGB    int    `json:"storage_gb"`
}

func (h *Handler) createRequest(w http.ResponseWriter, r *http.Request) {
	var in createRequestInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// 字段合并：前端语义字段与 snake_case 字段互为缺省（既有调用方显式提供时优先）。
	title := firstNonEmpty(in.Title, in.Resource)
	resourceType := firstNonEmpty(in.ResourceType, in.Type)
	description := firstNonEmpty(in.Description, in.Reason)
	tenantID := firstNonEmpty(in.TenantID, tenantFromRequest(r))
	requester := firstNonEmpty(in.Requester, userFromRequest(r))

	// params 为前端 JSON 文本（placeholder「JSON 格式参数」）：显式数值字段缺省时
	// 用其中的 cpu/memory_gb/storage_gb 参与成本估算；原文并入 description 保存
	// （resource_requests 表无 params 列），非法 JSON 视为自由文本忽略。
	cpu, memoryGB, storageGB := in.CPU, in.MemoryGB, in.StorageGB
	if in.Params != "" {
		if cpu == 0 && memoryGB == 0 && storageGB == 0 {
			var p struct {
				CPU       int `json:"cpu"`
				MemoryGB  int `json:"memory_gb"`
				StorageGB int `json:"storage_gb"`
			}
			if err := json.Unmarshal([]byte(in.Params), &p); err == nil {
				cpu, memoryGB, storageGB = p.CPU, p.MemoryGB, p.StorageGB
			}
		}
		if description != "" {
			description += "\n"
		}
		description += "params: " + in.Params
	}

	req, err := h.svc.CreateRequest(tenantID, requester, title, description, resourceType, cpu, memoryGB, storageGB)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toRequestView(req))
}

func (h *Handler) listRequests(w http.ResponseWriter, r *http.Request) {
	tenantID := r.URL.Query().Get("tenant_id")
	status := r.URL.Query().Get("status")
	requests := h.svc.ListRequests(tenantID, status)
	writeJSON(w, http.StatusOK, requestsPayload(requests))
}

// requestsPayload 统一包装为契约形态 {requests:[…]}（前端 store 兼容裸数组，
// 但契约注释声明的是包装形态，按声明输出）。
func requestsPayload(requests []*models.ResourceRequest) map[string][]requestView {
	views := make([]requestView, 0, len(requests))
	for _, req := range requests {
		views = append(views, toRequestView(req))
	}
	return map[string][]requestView{"requests": views}
}

// ============================================================================
// Approval queue (frontend contract)
// ============================================================================

// handleApprovals GET /api/v1/approvals —— 待审批队列（status=pending）。
func (h *Handler) handleApprovals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	tenantID := r.URL.Query().Get("tenant_id")
	pending := h.svc.ListRequests(tenantID, string(models.StatusPending))
	writeJSON(w, http.StatusOK, requestsPayload(pending))
}

// approvalContractInput 审批动作请求体：前端 approve 不带 body（空体按零值处理，
// 不报 400），reject 的原因字段名是 reason。
type approvalContractInput struct {
	Approver string `json:"approver"`
	Reason   string `json:"reason"`
	Note     string `json:"note"`
}

// decodeApprovalContractInput 解析审批动作请求体。
// 请求体可选：空体（io.EOF）按零值继续——前端 approve 动作不带 body；
// 其余解析失败不 4xx 拒绝（body 保持零值），但必须留痕（非静默吞错）。
func decodeApprovalContractInput(r *http.Request) approvalContractInput {
	var in approvalContractInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		log.Printf("[portal-svc] approvals: 可选请求体解析失败（按空体继续）: %v", err)
	}
	return in
}

func (in approvalContractInput) approver(r *http.Request) string {
	return firstNonEmpty(in.Approver, userFromRequest(r))
}

// handleApprovalDetail 覆盖前端契约的审批动作：
//
//	POST /api/v1/approvals/{id}/approve
//	POST /api/v1/approvals/{id}/reject  {reason}
//
// 响应为 {status}（前端据此刷新队列）。
func (h *Handler) handleApprovalDetail(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/approvals/")
	if path == "" {
		writeError(w, http.StatusBadRequest, "request ID required")
		return
	}
	switch {
	case strings.HasSuffix(path, "/approve"):
		id := strings.TrimSuffix(path, "/approve")
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.decideApproval(w, r, id, true)
	case strings.HasSuffix(path, "/reject"):
		id := strings.TrimSuffix(path, "/reject")
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.decideApproval(w, r, id, false)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (h *Handler) decideApproval(w http.ResponseWriter, r *http.Request, id string, approve bool) {
	if id == "" {
		writeError(w, http.StatusBadRequest, "request ID required")
		return
	}
	in := decodeApprovalContractInput(r)
	note := firstNonEmpty(in.Note, in.Reason)

	var (
		req *models.ResourceRequest
		err error
	)
	if approve {
		req, err = h.svc.ApproveRequest(id, in.approver(r), note)
	} else {
		req, err = h.svc.RejectRequest(id, in.approver(r), note)
	}
	if err != nil {
		if err == service.ErrRequestNotFound {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": string(req.Status)})
}

func (h *Handler) getRequest(w http.ResponseWriter, r *http.Request, id string) {
	req, err := h.svc.GetRequest(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, req)
}

type updateRequestInput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	CPU         int    `json:"cpu"`
	MemoryGB    int    `json:"memory_gb"`
	StorageGB   int    `json:"storage_gb"`
}

func (h *Handler) updateRequest(w http.ResponseWriter, r *http.Request, id string) {
	var in updateRequestInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req, err := h.svc.UpdateRequest(id, in.Title, in.Description, in.CPU, in.MemoryGB, in.StorageGB)
	if err != nil {
		if err == service.ErrRequestNotFound {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (h *Handler) cancelRequest(w http.ResponseWriter, r *http.Request, id string) {
	req, err := h.svc.CancelRequest(id)
	if err != nil {
		if err == service.ErrRequestNotFound {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, req)
}

type approvalInput struct {
	Approver string `json:"approver"`
	Note     string `json:"note"`
}

func (h *Handler) approveRequest(w http.ResponseWriter, r *http.Request, id string) {
	var in approvalInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req, err := h.svc.ApproveRequest(id, in.Approver, in.Note)
	if err != nil {
		if err == service.ErrRequestNotFound {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (h *Handler) rejectRequest(w http.ResponseWriter, r *http.Request, id string) {
	var in approvalInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req, err := h.svc.RejectRequest(id, in.Approver, in.Note)
	if err != nil {
		if err == service.ErrRequestNotFound {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, req)
}

// ============================================================================
// Cost overview (frontend contract)
// ============================================================================

// costOverviewView 是前端成本契约的聚合形态
// （web/enterprise/src/api/portal.js：{total,trend:[{date,amount}],byCategory:[{category,amount}]}）。
type costOverviewView struct {
	Total      float64           `json:"total"`
	Trend      []costTrendPoint  `json:"trend"`
	ByCategory []costCategorySum `json:"byCategory"`
}

type costTrendPoint struct {
	Date   string  `json:"date"`
	Amount float64 `json:"amount"`
}

type costCategorySum struct {
	Category string  `json:"category"`
	Amount   float64 `json:"amount"`
}

// costTrendDays 趋势窗口天数（含当日）。
const costTrendDays = 14

// buildCostOverview 由已批准/已交付的请求聚合成本总览：
// total = 成本估算合计；byCategory = 按资源类型分组（金额降序、同额按类别字典序）；
// trend = 最近 days 天按审批日（UpdatedAt，存储层在审批时刷新）分日聚合。
// 只统计 approved/fulfilled——draft/pending/rejected/cancelled 不构成实际成本。
func buildCostOverview(reqs []*models.ResourceRequest, now time.Time, days int) costOverviewView {
	view := costOverviewView{
		Trend:      make([]costTrendPoint, 0, days),
		ByCategory: make([]costCategorySum, 0, 4),
	}
	byCategory := map[string]float64{}
	byDay := map[string]float64{}
	for _, r := range reqs {
		if r.Status != models.StatusApproved && r.Status != models.StatusFulfilled {
			continue
		}
		view.Total += r.CostEstimate
		category := r.ResourceType
		if category == "" {
			category = "unknown"
		}
		byCategory[category] += r.CostEstimate
		byDay[r.UpdatedAt.UTC().Format("2006-01-02")] += r.CostEstimate
	}
	for i := days - 1; i >= 0; i-- {
		date := now.UTC().AddDate(0, 0, -i).Format("2006-01-02")
		view.Trend = append(view.Trend, costTrendPoint{Date: date, Amount: byDay[date]})
	}
	for category, amount := range byCategory {
		view.ByCategory = append(view.ByCategory, costCategorySum{Category: category, Amount: amount})
	}
	sort.Slice(view.ByCategory, func(i, j int) bool {
		if view.ByCategory[i].Amount != view.ByCategory[j].Amount {
			return view.ByCategory[i].Amount > view.ByCategory[j].Amount
		}
		return view.ByCategory[i].Category < view.ByCategory[j].Category
	})
	return view
}

func (h *Handler) handleCostOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	tenantID := r.URL.Query().Get("tenant_id")
	reqs := h.svc.ListRequests(tenantID, "")
	writeJSON(w, http.StatusOK, buildCostOverview(reqs, time.Now(), costTrendDays))
}

// ============================================================================
// Cost Optimization
// ============================================================================

func (h *Handler) handleUtilization(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	tenantID := r.URL.Query().Get("tenant_id")
	u, err := h.svc.GetUtilization(tenantID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (h *Handler) handleRecommendations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	tenantID := r.URL.Query().Get("tenant_id")
	recs := h.svc.GetRecommendations(tenantID)
	writeJSON(w, http.StatusOK, recs)
}

func (h *Handler) handleSavings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	tenantID := r.URL.Query().Get("tenant_id")
	s, err := h.svc.GetSavingsAnalysis(tenantID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (h *Handler) handleBudget(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		tenantID := r.URL.Query().Get("tenant_id")
		b, err := h.svc.GetBudget(tenantID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, b)
	case http.MethodPost:
		var in struct {
			TenantID       string  `json:"tenant_id"`
			MonthlyLimit   float64 `json:"monthly_limit"`
			AlertThreshold float64 `json:"alert_threshold"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		b, err := h.svc.SetBudget(in.TenantID, in.MonthlyLimit, in.AlertThreshold)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, b)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// ============================================================================
// Quota Management
// ============================================================================

func (h *Handler) handleQuotas(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		quotas := h.svc.ListQuotas()
		writeJSON(w, http.StatusOK, quotas)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleQuotaDetail(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimPrefix(r.URL.Path, "/api/v1/quotas/")
	if tenantID == "" {
		writeError(w, http.StatusBadRequest, "tenantID required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		// Check if this is a usage request
		if strings.HasSuffix(tenantID, "/usage") {
			id := strings.TrimSuffix(tenantID, "/usage")
			usage, err := h.svc.GetQuotaUsage(id)
			if err != nil {
				writeError(w, http.StatusNotFound, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, usage)
			return
		}
		q, err := h.svc.GetQuota(tenantID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, q)
	case http.MethodPut:
		var in struct {
			MaxCPU       int `json:"max_cpu"`
			MaxMemoryGB  int `json:"max_memory_gb"`
			MaxStorageGB int `json:"max_storage_gb"`
			MaxRequests  int `json:"max_requests"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		q, err := h.svc.UpdateQuota(tenantID, in.MaxCPU, in.MaxMemoryGB, in.MaxStorageGB, in.MaxRequests)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, q)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// ============================================================================
// Dashboard
// ============================================================================

func (h *Handler) handleDashboardStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	tenantID := r.URL.Query().Get("tenant_id")
	stats := h.svc.GetDashboardStats(tenantID)
	writeJSON(w, http.StatusOK, stats)
}

func (h *Handler) handleDashboardActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	tenantID := r.URL.Query().Get("tenant_id")
	limit := 20
	activity := h.svc.GetRecentActivity(tenantID, limit)
	writeJSON(w, http.StatusOK, activity)
}

// ============================================================================
// Cost Allocation / Chargeback
// ============================================================================

func (h *Handler) handleCostAllocate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var in struct {
		Dimension string           `json:"dimension"`
		TotalCost float64          `json:"total_cost"`
		Entries   []cost.CostEntry `json:"entries"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	report, err := h.alloc.AllocateCosts(cost.Dimension(in.Dimension), in.TotalCost, in.Entries)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *Handler) handleAllocationReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	dimension := r.URL.Query().Get("dimension")
	totalCost := 0.0
	if v := r.URL.Query().Get("total_cost"); v != "" {
		if _, err := fmt.Sscanf(v, "%f", &totalCost); err != nil {
			writeError(w, http.StatusBadRequest, "total_cost must be a number")
			return
		}
	}
	report, err := h.alloc.GetAllocationReport(cost.Dimension(dimension), totalCost, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *Handler) handleAllocationRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rules := h.alloc.GetAllocationRules()
		writeJSON(w, http.StatusOK, rules)
	case http.MethodPost:
		var rules []cost.AllocationRule
		if err := json.NewDecoder(r.Body).Decode(&rules); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		h.alloc.SetAllocationRules(rules)
		writeJSON(w, http.StatusCreated, map[string]string{"status": "rules updated"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// ============================================================================
// Helpers
// ============================================================================

// firstNonEmpty 返回第一个非空（TrimSpace 后）字符串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// tenantFromRequest 兜底解析租户：网关注入头（authctx 约定 X-Tenant-ID）优先，
// 缺失时取 "default"——与 gpu-svc 等域的零租户缺省一致
// （services/gpu-svc/internal/handler/handler.go tenantFromRequest）。
// 显式 body/query 中的 tenant_id 由调用方先行优先（既有调用方语义）。
func tenantFromRequest(r *http.Request) string {
	if tid := strings.TrimSpace(r.Header.Get("X-Tenant-ID")); tid != "" {
		return tid
	}
	return "default"
}

// userFromRequest 取网关注入的用户标识（authctx 约定头 X-User-Id）。
// 聚合层当前未对五域注入该头（internal/controlplane/service_proxy.go），
// 缺失时记 "unknown"——操作人留痕宁可标记未知，也不虚构身份。
func userFromRequest(r *http.Request) string {
	if u := strings.TrimSpace(r.Header.Get("X-User-ID")); u != "" {
		return u
	}
	return "unknown"
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
