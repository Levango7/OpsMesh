package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/service"
)

// Handler handles HTTP requests for the GPU service.
type Handler struct {
	svc *service.Service
}

// NewHandler creates a new HTTP handler.
func NewHandler(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes registers all HTTP routes.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// GPU Node Management
	mux.HandleFunc("/api/v1/gpu/nodes", h.handleNodes)
	mux.HandleFunc("/api/v1/gpu/nodes/", h.handleNodeDetail)

	// GPU Resources
	mux.HandleFunc("/api/v1/gpu/resources", h.handleResources)
	mux.HandleFunc("/api/v1/gpu/resources/per-node", h.handlePerNodeResources)

	// GPU Metrics
	// "/api/v1/gpu/metrics"：前端契约（web/enterprise/src/api/gpu.js）GET query 形态；
	// 尾斜杠子树（/api/v1/gpu/metrics/{nodeID}）为旧路径形态，保留兼容。
	mux.HandleFunc("/api/v1/gpu/metrics", h.handleMetrics)
	mux.HandleFunc("/api/v1/gpu/metrics/", h.handleMetrics)

	// Workload Management
	mux.HandleFunc("/api/v1/gpu/workloads", h.handleWorkloads)
	mux.HandleFunc("/api/v1/gpu/workloads/", h.handleWorkloadDetail)

	// Scheduling
	mux.HandleFunc("/api/v1/gpu/schedule", h.handleSchedule)
	mux.HandleFunc("/api/v1/gpu/schedule/policies", h.handleSchedulePolicies)
	mux.HandleFunc("/api/v1/gpu/schedule/queue", h.handleScheduleQueue)

	// Model Management
	mux.HandleFunc("/api/v1/gpu/models", h.handleModels)
	mux.HandleFunc("/api/v1/gpu/models/", h.handleModelDetail)

	// Quota
	mux.HandleFunc("/api/v1/gpu/quotas", h.handleQuotas)
	mux.HandleFunc("/api/v1/gpu/quotas/", h.handleQuotaDetail)
}

func (h *Handler) handleNodes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var node models.GPUNode
		if err := json.NewDecoder(r.Body).Decode(&node); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		registered, err := h.svc.RegisterNode(&node)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, registered)

	case http.MethodGet:
		nodes := h.svc.ListNodes()
		writeJSON(w, http.StatusOK, nodes)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleNodeDetail(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/gpu/nodes/")
	if path == "" || path == "health" {
		writeError(w, http.StatusBadRequest, "node ID required")
		return
	}

	// Handle /api/v1/gpu/nodes/{id}/health
	if strings.HasSuffix(path, "/health") {
		id := strings.TrimSuffix(path, "/health")
		health, err := h.svc.GetNodeHealth(id)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, health)
		return
	}

	id := path
	switch r.Method {
	case http.MethodGet:
		node, err := h.svc.GetNode(id)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, node)

	case http.MethodPut:
		var node models.GPUNode
		if err := json.NewDecoder(r.Body).Decode(&node); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		node.ID = id
		updated, err := h.svc.UpdateNode(&node)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, updated)

	case http.MethodDelete:
		if err := h.svc.UnregisterNode(id); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleResources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	summary := h.svc.GetResourceSummary()
	writeJSON(w, http.StatusOK, summary)
}

func (h *Handler) handlePerNodeResources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	resources := h.svc.GetNodeResources()
	writeJSON(w, http.StatusOK, resources)
}

func (h *Handler) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// 前端契约：nodeId 取 query 参数（web/enterprise/src/api/gpu.js getJSON('/gpu/metrics', {nodeId, range})）。
	query := r.URL.Query()
	nodeID := query.Get("nodeId")
	if nodeID == "" {
		nodeID = query.Get("node_id")
	}
	if nodeID == "" {
		// 旧路径形态 /api/v1/gpu/metrics/{nodeID}
		nodeID = strings.TrimPrefix(r.URL.Path, "/api/v1/gpu/metrics/")
	}
	// range 参数当前后端指标仅为即时采集快照，无时间序列，忽略透传不报错。
	gpuCount := 4
	if gc := query.Get("gpu_count"); gc != "" {
		if n, err := strconv.Atoi(gc); err == nil {
			gpuCount = n
		}
	}
	m, err := h.svc.GetGPUMetrics(nodeID, gpuCount)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 前端消费 {metrics: [...]}（stores/gpu.js fetchMetrics），首元素为节点级聚合。
	writeJSON(w, http.StatusOK, map[string][]*models.GPUMetrics{"metrics": {m}})
}

func (h *Handler) handleWorkloads(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		wl, err := decodeWorkload(body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		// 前端契约不发 tenant 字段：X-Tenant-ID 由前置网关注入（见 web/enterprise/src/api/request.js），
		// 缺失时兜底 "default"，与 pkg/tenant 零租户安全语义一致。
		if wl.TenantID == "" {
			wl.TenantID = tenantFromRequest(r)
		}
		submitted, err := h.svc.SubmitWorkload(wl)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, submitted)

	case http.MethodGet:
		status := r.URL.Query().Get("status")
		workloads := h.svc.ListWorkloads(status)
		writeJSON(w, http.StatusOK, workloads)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleWorkloadDetail(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/gpu/workloads/")

	// Handle /api/v1/gpu/workloads/{id}/scale
	if strings.HasSuffix(path, "/scale") {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		id := strings.TrimSuffix(path, "/scale")
		var req struct {
			Replicas int `json:"replicas"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := h.svc.ScaleWorkload(id, req.Replicas); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	id := path
	switch r.Method {
	case http.MethodGet:
		wl, err := h.svc.GetWorkload(id)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, wl)

	case http.MethodPut:
		var wl models.Workload
		if err := json.NewDecoder(r.Body).Decode(&wl); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		wl.ID = id
		updated, err := h.svc.UpdateWorkload(&wl)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, updated)

	case http.MethodDelete:
		if err := h.svc.CancelWorkload(id); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleSchedule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	results := h.svc.TriggerScheduling()
	writeJSON(w, http.StatusOK, results)
}

func (h *Handler) handleSchedulePolicies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		policies := h.svc.GetSchedulingPolicies()
		writeJSON(w, http.StatusOK, policies)

	case http.MethodPut:
		var policies []models.SchedulingPolicy
		if err := json.NewDecoder(r.Body).Decode(&policies); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := h.svc.SetSchedulingPolicies(policies); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		w.WriteHeader(http.StatusOK)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleScheduleQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	queue := h.svc.GetScheduleQueue()
	writeJSON(w, http.StatusOK, queue)
}

func (h *Handler) handleModels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		// 前端契约：{name,nodeId}（web/enterprise/src/api/gpu.js pullGpuModel），
		// nodeId 需带入返回的模型对象，禁止丢弃。
		var req struct {
			Name   string `json:"name"`
			NodeID string `json:"nodeId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		model, err := h.svc.PullModel(req.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.NodeID != "" {
			model.NodeID = req.NodeID
		}
		writeJSON(w, http.StatusCreated, model)

	case http.MethodGet:
		models := h.svc.ListModels()
		writeJSON(w, http.StatusOK, models)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleModelDetail(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/gpu/models/")

	// Handle /api/v1/gpu/models/{name}/serve
	if strings.HasSuffix(path, "/serve") {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		name := strings.TrimSuffix(path, "/serve")
		var req struct {
			NodeID   string `json:"node_id"`
			Port     int    `json:"port"`
			Replicas int    `json:"replicas"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		model, err := h.svc.ServeModel(name, req.NodeID, req.Port, req.Replicas)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, model)
		return
	}

	// Handle /api/v1/gpu/models/{name}/status
	if strings.HasSuffix(path, "/status") {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		name := strings.TrimSuffix(path, "/status")
		status, err := h.svc.GetModelStatus(name)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, status)
		return
	}

	name := path
	switch r.Method {
	case http.MethodDelete:
		if err := h.svc.RemoveModel(name); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleQuotas(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		quotas := h.svc.ListQuotas()
		writeJSON(w, http.StatusOK, quotas)

	case http.MethodPost:
		var q models.GPUQuota
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := h.svc.SetQuota(&q); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, q)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleQuotaDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	tenantID := strings.TrimPrefix(r.URL.Path, "/api/v1/gpu/quotas/")
	usage, err := h.svc.GetQuotaUsage(tenantID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, usage)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// workloadCreateRequest 是前端创建负载的 camelCase 契约形态
// （web/enterprise/src/api/gpu.js createGpuWorkload 发 {name,type,model,gpuCount,nodeId}）。
type workloadCreateRequest struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Model    string `json:"model"`
	GPUCount int    `json:"gpuCount"`
	NodeID   string `json:"nodeId"`
	TenantID string `json:"tenantId"`
}

// tenantFromRequest extracts the tenant ID injected by the upstream gateway.
func tenantFromRequest(r *http.Request) string {
	if tid := strings.TrimSpace(r.Header.Get("X-Tenant-ID")); tid != "" {
		return tid
	}
	return "default"
}

// decodeWorkload 兼容解析两种负载创建请求体：
// 前端 camelCase（gpuCount/nodeId/model/tenantId）与旧 snake_case（models.Workload JSON 形态）。
// 约束：前端形态的 gpu_request 不在请求体中，必须由顶层 camelCase 字段映射，避免零值负载入库。
func decodeWorkload(body []byte) (*models.Workload, error) {
	wl := &models.Workload{}
	if err := json.Unmarshal(body, wl); err != nil {
		return nil, err
	}

	var cr workloadCreateRequest
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, err
	}

	if cr.GPUCount > 0 {
		wl.GPURequest.Count = cr.GPUCount
	}
	if cr.TenantID != "" {
		wl.TenantID = cr.TenantID
	}
	if cr.Model != "" {
		wl.ModelName = cr.Model
	}
	if cr.NodeID != "" {
		if len(wl.NodeIDs) == 0 {
			wl.NodeIDs = []string{cr.NodeID}
		}
	}
	return wl, nil
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
