package cmdb

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"opsmesh/internal/authctx"
	"opsmesh/internal/proto"
)

// Handler 是 CMDB HTTP 路由处理器。
type Handler struct {
	store CiStore
}

// NewHandler 构造 CMDB 处理器。
func NewHandler(store CiStore) *Handler {
	return &Handler{store: store}
}

// Store 返回底层 CiStore，供 CMDBCollector 等内部组件复用 CMDB CRUD 能力。
// 外部消费方不应通过此方法绕过 handler 路由层写 CI（仍走 HTTP API）；
// 仅限控制面内部 collector/ reconciler 等需要直接操作 CI 的场景。
func (h *Handler) Store() CiStore { return h.store }

// RegisterRoutes 注入 CMDB 路由到 mux。
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/cmdb/ci", h.handleCIs)
	mux.HandleFunc("/api/v1/cmdb/types", h.handleCITypes)
	// Phase 3: 导入导出 + 待审列表（比 /ci/ 子树更具体，优先匹配）
	mux.HandleFunc("/api/v1/cmdb/ci/export", h.handleCIExport)
	mux.HandleFunc("/api/v1/cmdb/ci/import", h.handleCIImport)
	mux.HandleFunc("/api/v1/cmdb/ci/pending", h.handleCIPending)
	// Phase 2: 关系拓扑 + CI 子路由
	mux.HandleFunc("/api/v1/cmdb/ci/", h.handleCIByIDPrefix)
	mux.HandleFunc("/api/v1/cmdb/relations", h.handleRelations)
	// Phase 2: 属性模板
	mux.HandleFunc("/api/v1/cmdb/attr-templates", h.handleAttrTemplates)
	mux.HandleFunc("/api/v1/cmdb/attr-templates/", h.handleAttrTemplateByID)
}

// writeJSON 写入 JSON 响应。
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// handleCIs 处理 GET/POST /api/v1/cmdb/ci。
func (h *Handler) handleCIs(w http.ResponseWriter, r *http.Request) {
	actx := authctx.FromHTTPHeader(r.Header)
	switch r.Method {
	case http.MethodGet:
		ciType := r.URL.Query().Get("type")
		status := r.URL.Query().Get("status")
		if status == "" {
			status = "active"
		}
		items, err := h.store.GetCIs(r.Context(), ciType, status, actx.TenantID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if items == nil {
			items = []CiItem{}
		}
		writeJSON(w, http.StatusOK, items)

	case http.MethodPost:
		var ci CiItem
		if err := json.NewDecoder(r.Body).Decode(&ci); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid JSON: %v", err)})
			return
		}
		ci.TenantID = actx.TenantID
		ci.ID = fmt.Sprintf("ci-%d", time.Now().UnixNano()) // MVP 简易 ID
		ci.Status = "active"
		ci.Version = 1
		ci.Source = "api"
		// Phase-3 轻量审批流：手动 API 创建的 CI 进入待审批状态。
		ci.ApprovalStatus = ApprovalPending
		now := time.Now()
		ci.CreatedAt = now
		ci.UpdatedAt = now
		if err := h.store.CreateCI(r.Context(), &ci); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, ci)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCIByIDPrefix 处理 /api/v1/cmdb/ci/ 下所有子路径的请求路由。
func (h *Handler) handleCIByIDPrefix(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	// /api/v1/cmdb/ci/{id}/relations
	// /api/v1/cmdb/ci/{id}/graph
	// /api/v1/cmdb/ci/{id}
	parts := strings.Split(strings.TrimPrefix(path, "/api/v1/cmdb/ci/"), "/")
	if len(parts) >= 2 && parts[1] == "relations" {
		h.handleCIRelations(w, r, parts[0])
		return
	}
	if len(parts) >= 2 && parts[1] == "graph" {
		h.handleCIRelationGraph(w, r, parts[0])
		return
	}
	// Phase-3 审批端点：/api/v1/cmdb/ci/{id}/approve | /reject
	if len(parts) >= 2 && parts[1] == "approve" {
		h.handleCIApprove(w, r, parts[0])
		return
	}
	if len(parts) >= 2 && parts[1] == "reject" {
		h.handleCIReject(w, r, parts[0])
		return
	}
	// 回退到 ID 操作
	h.handleCIByID(w, r)
}

// handleCIByID 处理 GET/PUT/DELETE /api/v1/cmdb/ci/{id}。
func (h *Handler) handleCIByID(w http.ResponseWriter, r *http.Request) {
	actx := authctx.FromHTTPHeader(r.Header)
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/cmdb/ci/")
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "invalid ci id", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		ci, err := h.store.GetCI(r.Context(), id, actx.TenantID)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, ci)

	case http.MethodPut:
		var update CiItem
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid JSON: %v", err)})
			return
		}
		update.ID = id
		update.TenantID = actx.TenantID
		if err := h.store.UpdateCI(r.Context(), &update); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, update)

	case http.MethodDelete:
		if err := h.store.DeleteCI(r.Context(), id, actx.TenantID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCITypes 处理 GET/POST /api/v1/cmdb/types。
// GET 列出全部 CI 类型；POST 创建自定义（非内置）类型。
func (h *Handler) handleCITypes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		actx := authctx.FromHTTPHeader(r.Header)
		types, err := h.store.CiTypes(r.Context(), actx.TenantID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if types == nil {
			types = []CiType{}
		}
		writeJSON(w, http.StatusOK, types)
	case http.MethodPost:
		var t CiType
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid JSON: %v", err)})
			return
		}
		if t.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name required"})
			return
		}
		if err := h.store.CreateCiType(r.Context(), &t); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, t)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// === Phase-3: 导入导出 + 轻量审批流 ===

// handleCIExport 处理 GET /api/v1/cmdb/ci/export。
// 支持 ?format=csv（默认 json），导出当前租户下 active 状态 CI。
func (h *Handler) handleCIExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	items, err := h.store.GetCIs(r.Context(), "", "active", actx.TenantID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if items == nil {
		items = []CiItem{}
	}
	if r.URL.Query().Get("format") == "csv" {
		h.writeCICSV(w, items)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// writeCICSV 将 CI 列表写成 CSV 响应。
func (h *Handler) writeCICSV(w http.ResponseWriter, items []CiItem) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=ci_export.csv")
	w.WriteHeader(http.StatusOK)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "ciType", "name", "status", "approvalStatus", "source", "agentID", "deviceID", "attrs"})
	for _, it := range items {
		_ = cw.Write([]string{it.ID, it.CiType, it.Name, it.Status, it.ApprovalStatus, it.Source, it.AgentID, it.DeviceID, ciAttrsToCSV(it.Attrs)})
	}
	cw.Flush()
}

// handleCIImport 处理 POST /api/v1/cmdb/ci/import。
// 支持 JSON 数组或 CSV（?format=csv），按 ID upsert 到当前租户，返回导入摘要。
func (h *Handler) handleCIImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	var items []CiItem
	var parseErr string
	if r.URL.Query().Get("format") == "csv" {
		items, parseErr = parseCICSV(r)
	} else {
		if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
			parseErr = fmt.Sprintf("invalid JSON: %v", err)
		}
	}
	if parseErr != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": parseErr})
		return
	}
	var created, updated int
	var errs []string
	for i := range items {
		ci := items[i]
		ci.TenantID = actx.TenantID
		if ci.Status == "" {
			ci.Status = "active"
		}
		if ci.Source == "" {
			ci.Source = "import"
		}
		if ci.ApprovalStatus == "" {
			ci.ApprovalStatus = ApprovalPending
		}
		if ci.Attrs == nil {
			ci.Attrs = make(map[string]string)
		}
		// 同租户内已存在则更新；否则创建。若 ID 已被其它租户占用则换一个新 ID。
		if own, ownErr := h.store.GetCI(r.Context(), ci.ID, actx.TenantID); ownErr == nil && own != nil {
			if uerr := h.store.UpdateCI(r.Context(), &ci); uerr != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", ci.ID, uerr))
				continue
			}
			updated++
			continue
		}
		if ci.ID == "" || h.idTakenByOtherTenant(r.Context(), ci.ID, actx.TenantID) {
			ci.ID = fmt.Sprintf("ci-%d", time.Now().UnixNano())
		}
		if cerr := h.store.CreateCI(r.Context(), &ci); cerr != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", ci.ID, cerr))
			continue
		}
		created++
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"created": created,
		"updated": updated,
		"errors":  errs,
	})
}

// handleCIPending 处理 GET /api/v1/cmdb/ci/pending。
// 返回当前租户下待审批（approvalStatus=pending）的 CI 列表。
func (h *Handler) handleCIPending(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	items, err := h.store.GetCIsByApproval(r.Context(), ApprovalPending, actx.TenantID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if items == nil {
		items = []CiItem{}
	}
	writeJSON(w, http.StatusOK, items)
}

// handleCIApprove 处理 POST /api/v1/cmdb/ci/{id}/approve。
func (h *Handler) handleCIApprove(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	if err := h.store.SetApproval(r.Context(), id, actx.TenantID, ApprovalApproved); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "approvalStatus": ApprovalApproved})
}

// handleCIReject 处理 POST /api/v1/cmdb/ci/{id}/reject。
func (h *Handler) handleCIReject(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	if err := h.store.SetApproval(r.Context(), id, actx.TenantID, ApprovalRejected); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "approvalStatus": ApprovalRejected})
}

// ciAttrsToCSV 把属性 map 序列化为 k=v;k2=v2 形式（值内不含 ; 与 =）。
func ciAttrsToCSV(attrs map[string]string) string {
	if len(attrs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(attrs))
	for k, v := range attrs {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, ";")
}

// csvAttrsToMap 解析 k=v;k2=v2 形式的属性串。
func csvAttrsToMap(s string) map[string]string {
	out := make(map[string]string)
	s = strings.TrimSpace(s)
	if s == "" {
		return out
	}
	for _, pair := range strings.Split(s, ";") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		idx := strings.Index(pair, "=")
		if idx < 0 {
			continue
		}
		out[pair[:idx]] = pair[idx+1:]
	}
	return out
}

// parseCICSV 解析 CSV 形式的 CI 导入体，首行为表头。
func parseCICSV(r *http.Request) ([]CiItem, string) {
	reader := csv.NewReader(r.Body)
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Sprintf("invalid CSV: %v", err)
	}
	if len(rows) < 2 {
		return []CiItem{}, ""
	}
	header := rows[0]
	col := make(map[string]int, len(header))
	for i, hd := range header {
		col[strings.TrimSpace(hd)] = i
	}
	get := func(row []string, name string) string {
		if i, ok := col[name]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
	var out []CiItem
	for _, row := range rows[1:] {
		it := CiItem{
			ID:       get(row, "id"),
			CiType:   get(row, "ciType"),
			Name:     get(row, "name"),
			Status:   get(row, "status"),
			Source:   get(row, "source"),
			AgentID:  get(row, "agentID"),
			DeviceID: get(row, "deviceID"),
			Attrs:    csvAttrsToMap(get(row, "attrs")),
		}
		it.ApprovalStatus = get(row, "approvalStatus")
		out = append(out, it)
	}
	return out, ""
}

// idTakenByOtherTenant 探测 ID 是否已被其它租户占用（导入时避免跨租户 ID 冲突）。
func (h *Handler) idTakenByOtherTenant(ctx context.Context, id, tenantID string) bool {
	if id == "" {
		return false
	}
	it, err := h.store.GetCI(ctx, id, "")
	if err != nil || it == nil {
		return false
	}
	return it.TenantID != tenantID
}

// HandleReport 处理 agent 心跳中的 CMDB 增量上报：查找或创建对应 CI 条目并更新属性。
// 供 gRPC Heartbeat handler 调用。幂等（按 agentID 匹配后 upsert）。
func (h *Handler) HandleReport(ctx context.Context, agentID string, report *proto.CmdbReport) {
	if h == nil || report == nil {
		return
	}
	items, err := h.store.GetCIs(ctx, report.CiType, "active", "")
	if err != nil {
		return
	}
	var ci *CiItem
	for i := range items {
		if items[i].AgentID == agentID {
			ci = &items[i]
			break
		}
	}
	if ci == nil {
		ci = &CiItem{
			ID:      fmt.Sprintf("ci-%s-%d", agentID, time.Now().UnixNano()),
			CiType:  report.CiType,
			Name:    agentID,
			Status:  "active",
			Source:  "agent",
			AgentID: agentID,
			Attrs:   make(map[string]string),
		}
	}
	for _, attr := range report.Attrs {
		ci.Attrs[attr.Key] = attr.Value
	}
	if ci.CreatedAt.IsZero() {
		now := time.Now()
		ci.CreatedAt = now
		ci.UpdatedAt = now
		if err := h.store.CreateCI(ctx, ci); err != nil {
			log.Printf("[cmdb] HandleReport CreateCI 失败: %v", err)
			return
		}
	} else {
		if err := h.store.UpdateCI(ctx, ci); err != nil {
			log.Printf("[cmdb] HandleReport UpdateCI 失败: %v", err)
			return
		}
	}
}

// === Phase 2: 关系拓扑 ===

// handleRelations 处理 POST/GET /api/v1/cmdb/relations。
func (h *Handler) handleRelations(w http.ResponseWriter, r *http.Request) {
	actx := authctx.FromHTTPHeader(r.Header)
	switch r.Method {
	case http.MethodGet:
		ciID := r.URL.Query().Get("ciID")
		if ciID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ciID required"})
			return
		}
		rels, err := h.store.GetCIRelations(r.Context(), ciID, actx.TenantID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if rels == nil {
			rels = []CiRelation{}
		}
		writeJSON(w, http.StatusOK, rels)

	case http.MethodPost:
		var rel CiRelation
		if err := json.NewDecoder(r.Body).Decode(&rel); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid JSON: %v", err)})
			return
		}
		rel.TenantID = actx.TenantID
		if err := h.store.CreateRelation(r.Context(), &rel); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, rel)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCIRelations 处理 GET /api/v1/cmdb/ci/{id}/relations。
func (h *Handler) handleCIRelations(w http.ResponseWriter, r *http.Request, ciID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	rels, err := h.store.GetCIRelations(r.Context(), ciID, actx.TenantID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rels == nil {
		rels = []CiRelation{}
	}
	writeJSON(w, http.StatusOK, rels)
}

// handleCIRelationGraph 处理 GET /api/v1/cmdb/ci/{id}/graph。
func (h *Handler) handleCIRelationGraph(w http.ResponseWriter, r *http.Request, ciID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	graph, err := h.store.GetCIRelationGraph(r.Context(), ciID, actx.TenantID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, graph)
}

// === Phase 2: 属性模板 ===

// handleAttrTemplates 处理 GET/POST /api/v1/cmdb/attr-templates。
func (h *Handler) handleAttrTemplates(w http.ResponseWriter, r *http.Request) {
	actx := authctx.FromHTTPHeader(r.Header)
	switch r.Method {
	case http.MethodGet:
		ciType := r.URL.Query().Get("type")
		tmpls, err := h.store.GetAttrTemplates(r.Context(), ciType, actx.TenantID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if tmpls == nil {
			tmpls = []CiAttrTemplate{}
		}
		writeJSON(w, http.StatusOK, tmpls)

	case http.MethodPost:
		var tmpl CiAttrTemplate
		if err := json.NewDecoder(r.Body).Decode(&tmpl); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid JSON: %v", err)})
			return
		}
		tmpl.TenantID = actx.TenantID
		if err := h.store.CreateAttrTemplate(r.Context(), &tmpl); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, tmpl)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAttrTemplateByID 处理 GET/PUT/DELETE /api/v1/cmdb/attr-templates/{id}。
func (h *Handler) handleAttrTemplateByID(w http.ResponseWriter, r *http.Request) {
	actx := authctx.FromHTTPHeader(r.Header)
	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/cmdb/attr-templates/")
	if idStr == "" || strings.Contains(idStr, "/") {
		http.Error(w, "invalid template id", http.StatusBadRequest)
		return
	}
	var tmplID int
	if _, err := fmt.Sscanf(idStr, "%d", &tmplID); err != nil {
		http.Error(w, "invalid template id", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		tmpls, err := h.store.GetAttrTemplates(r.Context(), "", actx.TenantID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		for _, t := range tmpls {
			if t.ID == tmplID {
				writeJSON(w, http.StatusOK, t)
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})

	case http.MethodPut:
		var tmpl CiAttrTemplate
		if err := json.NewDecoder(r.Body).Decode(&tmpl); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid JSON: %v", err)})
			return
		}
		tmpl.ID = tmplID
		tmpl.TenantID = actx.TenantID
		if err := h.store.UpdateAttrTemplate(r.Context(), &tmpl); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, tmpl)

	case http.MethodDelete:
		if err := h.store.DeleteAttrTemplate(r.Context(), tmplID, actx.TenantID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
