package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"opsmesh/internal/authctx"
	"opsmesh/internal/cron"
	"opsmesh/internal/dag"
	"opsmesh/internal/proto"
)

// TaskEngine 防腐接口：M5 展开 DAG 时经此把节点下发给底层任务引擎
// （store.CreateTask + store.TasksByParent），避免 orchestration 反向依赖 controlplane/Registry。
// store.Store 已同时具备这两个方法，controlplane 直接以 store 适配。
type TaskEngine interface {
	CreateTask(t *proto.Task) *proto.Task
	TasksByParent(parentID string) []*proto.Task
}

// Handler 是 M5 作业编排中心的 HTTP 处理器。
type Handler struct {
	store WorkflowStore
	eng   TaskEngine
}

// NewHandler 构造编排处理器。
func NewHandler(st WorkflowStore, eng TaskEngine) *Handler {
	return &Handler{store: st, eng: eng}
}

// RegisterRoutes 注入 M5 编排路由到 mux。
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/workflows", h.handleWorkflows)
	mux.HandleFunc("/api/v1/workflows/", h.handleWorkflowByID)
}

func (h *Handler) handleWorkflows(w http.ResponseWriter, r *http.Request) {
	actx := authctx.FromHTTPHeader(r.Header)
	switch r.Method {
	case http.MethodGet:
		list, err := h.store.List(r.Context(), actx.TenantID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if list == nil {
			list = []WorkflowDef{}
		}
		writeJSON(w, http.StatusOK, list)
	case http.MethodPost:
		var wf WorkflowDef
		if err := json.NewDecoder(r.Body).Decode(&wf); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		wf.TenantID = actx.TenantID
		if !wf.Valid() {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and agentID required"})
			return
		}
		if err := h.validateDAG(wf.DAG); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if wf.Status == "" {
			wf.Status = StatusDraft
		}
		if err := h.store.Create(r.Context(), &wf); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, wf)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleWorkflowByID 处理 /api/v1/workflows/{id} 及其子操作 run / schedule / status。
func (h *Handler) handleWorkflowByID(w http.ResponseWriter, r *http.Request) {
	actx := authctx.FromHTTPHeader(r.Header)
	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/workflows/")
	parts := strings.SplitN(idStr, "/", 2)
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid workflow id"})
		return
	}
	switch {
	case len(parts) >= 2 && parts[1] == "run":
		h.runWorkflow(w, r, id, actx.TenantID)
	case len(parts) >= 2 && parts[1] == "schedule":
		h.scheduleWorkflow(w, r, id, actx.TenantID)
	case len(parts) >= 2 && parts[1] == "status":
		h.statusWorkflow(w, r, id, actx.TenantID)
	case r.Method == http.MethodPut || r.Method == http.MethodPatch:
		h.updateWorkflow(w, r, id, actx.TenantID)
	default:
		h.getWorkflow(w, r, id, actx.TenantID)
	}
}

func (h *Handler) getWorkflow(w http.ResponseWriter, r *http.Request, id int64, tenantID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	wf, err := h.store.Get(r.Context(), id, tenantID)
	if err != nil {
		status := http.StatusInternalServerError
		if err == ErrWFNotFound {
			status = http.StatusNotFound
		} else if err == ErrWFTenantMismatch {
			status = http.StatusForbidden
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, wf)
}

// updateWorkflow PUT/PATCH /api/v1/workflows/{id}：更新名称 / DAG / cron（部分更新）。
// DAG 与 cron 各自独立校验；draft 工作流设合法 cron 后自动转 active。租户隔离沿用 Get 的权限语义。
func (h *Handler) updateWorkflow(w http.ResponseWriter, r *http.Request, id int64, tenantID string) {
	if r.Method != http.MethodPut && r.Method != http.MethodPatch {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	wf, err := h.store.Get(r.Context(), id, tenantID)
	if err != nil {
		status := http.StatusInternalServerError
		if err == ErrWFNotFound {
			status = http.StatusNotFound
		} else if err == ErrWFTenantMismatch {
			status = http.StatusForbidden
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	var body struct {
		Name string `json:"name"`
		DAG  string `json:"dag"`
		Cron string `json:"cron"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if body.Name != "" {
		wf.Name = body.Name
	}
	if body.DAG != "" {
		if err := h.validateDAG(body.DAG); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		wf.DAG = body.DAG
	}
	if body.Cron != "" {
		if _, err := cron.Match(body.Cron, time.Now()); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cron: " + err.Error()})
			return
		}
		wf.Cron = body.Cron
		if wf.Status == StatusDraft {
			wf.Status = StatusActive
		}
	}
	if err := h.store.Update(r.Context(), wf); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, wf)
}

// runWorkflow POST /api/v1/workflows/{id}/run：校验 DAG 后展开为底层任务（按 ParentID 归组），
// 复用 proto.Task.DependsOn + per-agent releaseDeps 引擎驱动依赖就绪。
func (h *Handler) runWorkflow(w http.ResponseWriter, r *http.Request, id int64, tenantID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	wf, err := h.store.Get(r.Context(), id, tenantID)
	if err != nil {
		status := http.StatusInternalServerError
		if err == ErrWFNotFound {
			status = http.StatusNotFound
		} else if err == ErrWFTenantMismatch {
			status = http.StatusForbidden
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	if _, err := h.Trigger(r.Context(), id, tenantID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	wf, _ = h.store.Get(r.Context(), id, tenantID)
	writeJSON(w, http.StatusOK, wf)
}

// Trigger 展开工作流 DAG 为底层任务（不重复校验，调用方负责权限/存在性）。
func (h *Handler) Trigger(ctx context.Context, id int64, tenantID string) (*WorkflowDef, error) {
	wf, err := h.store.Get(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	nodes, err := wf.Nodes()
	if err != nil {
		return nil, err
	}
	if err := dag.Validate(toProtoTasks(nodes)); err != nil {
		return nil, err
	}
	parent := "wf:" + strconv.FormatInt(wf.ID, 10)
	prefix := "wf-" + strconv.FormatInt(wf.ID, 10) + "-"
	for i := range nodes {
		n := nodes[i]
		// 用确定性 TaskID（wf-<id>-<nodeID>），并把节点 DependsOn 映射到这些 ID，
		// 否则 CreateTask 分配随机 TaskID 后，依赖引用对不上，per-agent releaseDeps 永不释放。
		deps := make([]string, 0, len(n.DependsOn))
		for _, d := range n.DependsOn {
			deps = append(deps, prefix+d)
		}
		h.eng.CreateTask(&proto.Task{
			TaskID:    prefix + n.ID,
			AgentID:   wf.AgentID,
			TenantID:  wf.TenantID,
			Type:      n.Type,
			Command:   n.Command,
			Path:      n.Path,
			DependsOn: deps,
			ParentID:  parent,
		})
	}
	now := time.Now()
	wf.Status = StatusRunning
	wf.LastRunAt = now
	wf.LastRunStatus = StatusRunning
	if err := h.store.Update(ctx, wf); err != nil {
		return nil, err
	}
	return wf, nil
}

// SetCron 设置/清除工作流 cron 表达式（空串=取消定时）；非法表达式报错；
// draft 工作流设 cron 后自动转为 active（可调度）。供 HTTP /schedule 与单测复用。
func (h *Handler) SetCron(ctx context.Context, id int64, tenantID, expr string) error {
	wf, err := h.store.Get(ctx, id, tenantID)
	if err != nil {
		return err
	}
	if expr != "" {
		if _, err := cron.Match(expr, time.Now()); err != nil {
			return fmt.Errorf("invalid cron: %w", err)
		}
	}
	wf.Cron = expr
	if wf.Cron != "" && wf.Status == StatusDraft {
		wf.Status = StatusActive
	}
	return h.store.Update(ctx, wf)
}

// scheduleWorkflow POST /api/v1/workflows/{id}/schedule：设置/清除 5 字段 cron 表达式。
func (h *Handler) scheduleWorkflow(w http.ResponseWriter, r *http.Request, id int64, tenantID string) {
	wf, err := h.store.Get(r.Context(), id, tenantID)
	if err != nil {
		status := http.StatusInternalServerError
		if err == ErrWFNotFound {
			status = http.StatusNotFound
		} else if err == ErrWFTenantMismatch {
			status = http.StatusForbidden
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, wf)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Cron string `json:"cron"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if err := h.SetCron(r.Context(), wf.ID, tenantID, body.Cron); err != nil {
		status := http.StatusInternalServerError
		if err == ErrWFNotFound {
			status = http.StatusNotFound
		} else if err == ErrWFTenantMismatch {
			status = http.StatusForbidden
		} else {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, wf)
}

// statusWorkflow GET /api/v1/workflows/{id}/status：返回工作流定义 + 各节点任务状态。
func (h *Handler) statusWorkflow(w http.ResponseWriter, r *http.Request, id int64, tenantID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	wf, err := h.store.Get(r.Context(), id, tenantID)
	if err != nil {
		status := http.StatusInternalServerError
		if err == ErrWFNotFound {
			status = http.StatusNotFound
		} else if err == ErrWFTenantMismatch {
			status = http.StatusForbidden
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	tasks := h.eng.TasksByParent("wf:" + strconv.FormatInt(wf.ID, 10))
	nodeTasks := make(map[string]string, len(tasks))
	for _, t := range tasks {
		nodeTasks[t.TaskID] = t.Status
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"workflow":  wf,
		"nodeTasks": nodeTasks,
	})
}

// Reconcile 依据底层节点任务状态翻工作流最近运行终态：
// 全 done → success；有 failed → failed；否则 running。回到 active 等待下次调度。
func (h *Handler) Reconcile(ctx context.Context, id int64, tenantID string) error {
	wf, err := h.store.Get(ctx, id, tenantID)
	if err != nil {
		return err
	}
	tasks := h.eng.TasksByParent("wf:" + strconv.FormatInt(wf.ID, 10))
	if len(tasks) == 0 {
		return nil
	}
	allDone, anyFailed := true, false
	for _, t := range tasks {
		switch t.Status {
		case "done":
			// ok
		case "failed":
			anyFailed = true
			allDone = false
		default:
			allDone = false
		}
	}
	if anyFailed {
		wf.LastRunStatus = StatusFailed
		wf.Status = StatusActive
	} else if allDone {
		wf.LastRunStatus = StatusSuccess
		wf.Status = StatusActive
	} else {
		wf.LastRunStatus = StatusRunning
		wf.Status = StatusRunning
	}
	wf.UpdatedAt = time.Now()
	return h.store.Update(ctx, wf)
}

// ListActive 返回全部 active 工作流（供控制面 scheduleLoop 周期派发，不按租户过滤）。
func (h *Handler) ListActive(ctx context.Context) ([]WorkflowDef, error) {
	all, err := h.store.List(ctx, "")
	if err != nil {
		return nil, err
	}
	out := make([]WorkflowDef, 0, len(all))
	for _, wf := range all {
		if wf.Status == StatusActive {
			out = append(out, wf)
		}
	}
	return out, nil
}

// validateDAG 解析并校验 DAG（环/自依赖/缺失依赖）。
func (h *Handler) validateDAG(dagJSON string) error {
	ns, err := parseNodes(dagJSON)
	if err != nil {
		return err
	}
	return dag.Validate(toProtoTasks(ns))
}

func parseNodes(dagJSON string) ([]WorkflowNode, error) {
	if dagJSON == "" {
		return nil, nil
	}
	var ns []WorkflowNode
	if err := json.Unmarshal([]byte(dagJSON), &ns); err != nil {
		return nil, fmt.Errorf("workflow dag JSON invalid: %w", err)
	}
	return ns, nil
}

func toProtoTasks(ns []WorkflowNode) []*proto.Task {
	out := make([]*proto.Task, 0, len(ns))
	for i := range ns {
		n := ns[i]
		out = append(out, &proto.Task{
			TaskID:    n.ID,
			Type:      n.Type,
			Command:   n.Command,
			DependsOn: n.DependsOn,
		})
	}
	return out
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
