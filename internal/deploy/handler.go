package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"opsmesh/internal/authctx"
	"opsmesh/internal/proto"
)

// Dispatcher 是 M3 派发底层任务到执行引擎（M4）的防腐接口。
// 由 controlplane 用 Registry 适配实现，避免 deploy 包反向依赖 controlplane（P2-18 贯穿边界）。
type Dispatcher interface {
	// CreateTask 派发一个底层自动化任务（复用 M4 任务引擎）。
	CreateTask(t *proto.Task) *proto.Task
	// Device 查询目标设备（取 AgentID 以定位执行 agent）。
	Device(id string) *proto.DeviceInfo
	// TaskStates 批量查询底层任务状态（taskID -> status），供 reconcile 判定部署终态。
	TaskStates(ids []string, tenantID string) map[string]string
}

// Handler 是 M3 部署中心的 HTTP 处理器。
type Handler struct {
	store DeployStore
	disp  Dispatcher
}

// NewHandler 构造部署处理器。
func NewHandler(store DeployStore, disp Dispatcher) *Handler {
	return &Handler{store: store, disp: disp}
}

// Store 暴露底层存储（供 controlplane 后台 reconcile 调用）。
func (h *Handler) Store() DeployStore { return h.store }

// RegisterRoutes 注入 M3 部署路由到 mux（对齐 系统设计 3.2.M3 接口清单）。
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/deploys", h.handleDeploys)
	mux.HandleFunc("/api/v1/deploys/", h.handleDeployByID)
}

// handleDeploys POST 创建 / GET 列表。
func (h *Handler) handleDeploys(w http.ResponseWriter, r *http.Request) {
	actx := authctx.FromHTTPHeader(r.Header)
	switch r.Method {
	case http.MethodPost:
		var dt DeployTask
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // task 87：请求体限 1MiB，防超大 Content 打爆内存/存储
		if err := json.NewDecoder(r.Body).Decode(&dt); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid JSON: %v", err)})
			return
		}
		dt.TenantID = actx.TenantID // 强制租户隔离
		if dt.TenantID == "" {
			dt.TenantID = "default" // 本地无网关时的隐式租户，与任务/CMDB 等模块一致
		}
		dt.CreatedBy = actx.UserID
		if dt.CreatedBy == "" {
			dt.CreatedBy = "local"
		}
		created, err := h.store.Create(r.Context(), &dt)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, created)
	case http.MethodGet:
		status := r.URL.Query().Get("status")
		list, err := h.store.List(r.Context(), actx.TenantID, status)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if list == nil {
			list = []DeployTask{}
		}
		writeJSON(w, http.StatusOK, list)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleDeployByID GET 详情 / {id}/execute 执行 / {id}/rollback 回滚。
func (h *Handler) handleDeployByID(w http.ResponseWriter, r *http.Request) {
	actx := authctx.FromHTTPHeader(r.Header)
	// 路径：/api/v1/deploys/{id} 或 /api/v1/deploys/{id}/execute|rollback
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/deploys/")
	parts := strings.SplitN(rest, "/", 2)
	idStr := parts[0]
	if idStr == "" || strings.Contains(idStr, "/") {
		http.Error(w, "invalid deploy id", http.StatusBadRequest)
		return
	}
	var id int64
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
		http.Error(w, "invalid deploy id", http.StatusBadRequest)
		return
	}

	// 子操作：execute / rollback。
	if len(parts) == 2 {
		switch parts[1] {
		case "execute":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if err := h.Execute(r.Context(), id, actx.TenantID); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			dt, _ := h.store.Get(r.Context(), id, actx.TenantID)
			writeJSON(w, http.StatusOK, dt)
			return
		case "rollback":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if err := h.Rollback(r.Context(), id, actx.TenantID); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			dt, _ := h.store.Get(r.Context(), id, actx.TenantID)
			writeJSON(w, http.StatusOK, dt)
			return
		default:
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
	}

	// 默认：GET 详情（带 lazy reconcile）。
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	dt, err := h.store.Get(r.Context(), id, actx.TenantID)
	if err != nil {
		if err == ErrNotFound {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "deploy not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, dt)
}

// Execute 执行部署：派发底层任务到目标 agent，状态 created -> running（无可用目标则 failed）。
func (h *Handler) Execute(ctx context.Context, id int64, tenantID string) error {
	dt, err := h.store.Get(ctx, id, tenantID)
	if err != nil {
		return err
	}
	if dt.Status != StatusCreated {
		return fmt.Errorf("deploy %d not executable in status %s", id, dt.Status)
	}
	targets := SplitIDs(dt.TargetIDs)
	taskIDs := make([]string, 0, len(targets))
	for _, tid := range targets {
		dev := h.disp.Device(tid)
		if dev == nil || dev.AgentID == "" {
			continue // 目标无 agent（未纳管），跳过
		}
		t := &proto.Task{
			AgentID:  dev.AgentID,
			TenantID: dt.TenantID,
			Type:      deployTypeToTaskType(dt.Type),
			Command:   dt.RepoURL,
			Content:   dt.Content,
			Path:      dt.Path,
			Status:    "pending",
		}
		created := h.disp.CreateTask(t)
		if created != nil && created.TaskID != "" {
			taskIDs = append(taskIDs, created.TaskID)
		}
	}
	if len(taskIDs) == 0 {
		dt.Status = StatusFailed
	} else {
		dt.Status = StatusRunning
		dt.TaskIDs = strings.Join(taskIDs, ",")
	}
	return h.store.Update(ctx, dt)
}

// Rolback 回滚：running/success -> rolledback（MVP：状态回退；真实 Argo CD 回滚由外部同步）。
func (h *Handler) Rollback(ctx context.Context, id int64, tenantID string) error {
	dt, err := h.store.Get(ctx, id, tenantID)
	if err != nil {
		return err
	}
	if dt.Status != StatusRunning && dt.Status != StatusSuccess {
		return fmt.Errorf("deploy %d cannot rollback from status %s", id, dt.Status)
	}
	dt.Status = StatusRolledBack
	return h.store.Update(ctx, dt)
}

// Reconcile 对单条 running 部署做状态对账：底层任务全 done -> success；任一 failed -> failed。
func (h *Handler) Reconcile(ctx context.Context, id int64, tenantID string) error {
	dt, err := h.store.Get(ctx, id, tenantID)
	if err != nil {
		return err
	}
	if dt.Status != StatusRunning {
		return nil // 非 running 不对账
	}
	ids := SplitIDs(dt.TaskIDs)
	states := h.disp.TaskStates(ids, dt.TenantID)
	if len(states) == 0 {
		return nil
	}
	done, failed := 0, 0
	for _, st := range states {
		switch st {
		case "done":
			done++
		case "failed":
			failed++
		}
	}
	if failed > 0 {
		dt.Status = StatusFailed
	} else if done == len(states) {
		dt.Status = StatusSuccess
	} else {
		return nil // 仍在进行中
	}
	return h.store.Update(ctx, dt)
}

// ReconcileAll 对账所有 running 部署（controlplane 后台周期调用）。
func (h *Handler) ReconcileAll(ctx context.Context, tenantID string) int {
	list, err := h.store.List(ctx, tenantID, StatusRunning)
	if err != nil {
		return 0
	}
	n := 0
	for i := range list {
		if h.Reconcile(ctx, list[i].ID, tenantID) == nil {
			n++
		}
	}
	return n
}

// deployTypeToTaskType 把部署类型映射到底层任务类型。
func deployTypeToTaskType(dt string) string {
	switch dt {
	case TypeScript, TypeK8s:
		return proto.TaskTypeShell // script/k8s -> shell 执行
	case TypeFile:
		return proto.TaskTypeFile // file -> 写文件
	default:
		return proto.TaskTypeShell
	}
}

// writeJSON 写入 JSON 响应。
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
