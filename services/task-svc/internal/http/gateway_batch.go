// gateway_batch.go 实现 task-svc 的批量运维 HTTP API（对齐 controlplane M5 batch-exec）：
//   - POST /api/v1/tasks/batch-exec   批量执行（多设备 + 同一任务）
//   - GET  /api/v1/tasks/batch/{id}   批量任务状态查询
//
// 与 controlplane server_batch.go handleBatchExec/handleBatchStatus 的对齐关系：
//   - 请求/响应 JSON 契约一致（camelCase 字段名）
//   - batchID 生成方式一致：crypto/rand → batch-<8 字节 hex>
//   - 批量状态仅内存索引（重启后丢失），任务实例本身持久化在 store 中
//
// 与 controlplane 的差异（task-svc 网关层约束）：
//   - task-svc 不管理 agent/device，省略 lookupAgent 检查，直接为每个 deviceID 创建任务
//   - gateway 层无审计/事件总线/SSE，省略 s.audit / s.bus.Publish / s.publishEvent 调用
//   - 使用 writeJSON/writeError 替换 paginate.WriteJSON/paginate.JSONError
//   - 使用 g.svc.CreateTask（service 层）替换 s.store.CreateTask
//   - service 层 CreateTask 已内置 ValidateCommand，handler 层不再重复校验
//
// 设计：不修改 gateway.go，路由注册由 RegisterBatchRoutes 独立暴露，
// 由 main 或 team leader 统一编排调用。批量内存索引用包级单例（sync.RWMutex 保护）。
package http

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	taskv1 "github.com/Levango7/OpsMesh/services/task-svc/api/proto/v1"
)

// ============================================================================
// 批量执行：内存索引
// ============================================================================

// batchTask 单次批量执行记录（对齐 controlplane batchTask）。
type batchTask struct {
	BatchID   string          // 批次 ID
	TenantID  string          // 租户
	TaskType  string          // 任务类型
	Command   string          // 命令
	Timeout   int             // 超时（秒）
	CreatedAt time.Time       // 创建时间
	CreatedBy string          // 创建人
	Tasks     []batchTaskItem // 每设备任务详情
}

// batchTaskItem 批量中单设备任务状态。
// JSON tag 使用 camelCase（task-svc 网关契约统一 camelCase，见 gateway.go 头注释）。
type batchTaskItem struct {
	DeviceID string `json:"deviceID"`
	TaskID   string `json:"taskID"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
}

// batchIndex 批量执行内存索引（包级单例，重启后丢失）。
// 设计与 controlplane batchStore 一致：任务实例本身持久化在 store 中，
// 此索引仅用于通过 batchID 查询活跃批次的聚合状态。
type batchIndex struct {
	mu      sync.RWMutex
	batches map[string]*batchTask
}

// batchIdx 包级单例，包加载时初始化。
var batchIdx = &batchIndex{batches: make(map[string]*batchTask)}

// genBatchID 生成批次 ID（batch-<8 字节 hex>），对齐 controlplane genBatchID。
// crypto/rand 失败仅见于系统熵源故障的极端环境：占位 ID 保持非空可用，
// 冲突由内存索引覆盖语义兜底，此处留痕即可。
func genBatchID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		log.Printf("[task-svc] genBatchID: crypto/rand 读取失败（使用零值占位 ID）: %v", err)
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}

// ============================================================================
// 批量执行 API
// ============================================================================

// handleBatchExec 处理 POST /api/v1/tasks/batch-exec：批量执行。
// 请求体: { deviceIDs: [], taskType, command, content, path, timeout }
// 返回: { batchID, tasks: [{deviceID, taskID, status}] }
//
// 为每个 deviceID 创建一个任务，返回 batchID + 每设备任务详情。
// 单设备创建失败不中断整批（与 controlplane lookupAgent 失败时 continue 语义一致），
// 记录失败原因到对应 item.Error，批次仍创建并返回。
func (g *Gateway) handleBatchExec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	actx, err := g.extractAuth(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	var body struct {
		DeviceIDs []string `json:"deviceIDs"`
		TaskType  string   `json:"taskType"`
		Command   string   `json:"command"`
		Content   string   `json:"content"`
		Path      string   `json:"path"`
		Timeout   int      `json:"timeout"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if len(body.DeviceIDs) == 0 {
		writeError(w, http.StatusBadRequest, "deviceIDs is required (non-empty)")
		return
	}
	if body.Command == "" {
		writeError(w, http.StatusBadRequest, "command is required")
		return
	}
	if body.TaskType == "" {
		body.TaskType = "shell"
	}
	tenant := actx.TenantID
	batchID := genBatchID("batch")
	items := make([]batchTaskItem, 0, len(body.DeviceIDs))
	for _, devID := range body.DeviceIDs {
		// task-svc 不管理 agent/device，直接为每个 deviceID 创建任务。
		// service 层 CreateTask 内置 ValidateCommand 校验（等价 controlplane 循环外 validateCommand）。
		task := &taskv1.Task{
			AgentId:  devID,
			TenantId: tenant,
			Type:     body.TaskType,
			Command:  body.Command,
			Content:  body.Content,
			Path:     body.Path,
			Timeout:  int32(body.Timeout),
		}
		created, createErr := g.svc.CreateTask(r.Context(), &taskv1.CreateTaskRequest{Task: task})
		if createErr != nil {
			// 单设备创建失败不中断整批，记录失败原因继续后续设备。
			items = append(items, batchTaskItem{
				DeviceID: devID,
				Status:   "failed",
				Error:    createErr.Error(),
			})
			continue
		}
		items = append(items, batchTaskItem{
			DeviceID: devID,
			TaskID:   created.TaskId,
			Status:   created.Status,
		})
	}
	bt := &batchTask{
		BatchID:   batchID,
		TenantID:  tenant,
		TaskType:  body.TaskType,
		Command:   body.Command,
		Timeout:   body.Timeout,
		CreatedAt: time.Now(),
		CreatedBy: actx.UserID,
		Tasks:     items,
	}
	batchIdx.mu.Lock()
	batchIdx.batches[batchID] = bt
	batchIdx.mu.Unlock()
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"batchID": batchID,
		"tasks":   items,
	})
}

// handleBatchStatus 处理 GET /api/v1/tasks/batch/{id}：批量任务状态。
// 实时刷新每个任务的状态（从 service 层 GetTask 拉取最新状态）。
// 返回: { batchID, taskType, command, createdAt, createdBy, tasks: [{deviceID, taskID, status}] }
func (g *Gateway) handleBatchStatus(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	actx, err := g.extractAuth(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	batchIdx.mu.RLock()
	bt, exists := batchIdx.batches[id]
	batchIdx.mu.RUnlock()
	if !exists {
		writeError(w, http.StatusNotFound, "batch not found")
		return
	}
	// 租户隔离：批次租户与请求租户不一致时 403（防跨租户查询）。
	// 双侧空串兜底：actx.TenantID 为空（未注入）或批次 TenantID 为空时放行，
	// 与 controlplane `actx.TenantID != "" && bt.TenantID != actx.TenantID` 语义一致。
	if actx.TenantID != "" && bt.TenantID != "" && bt.TenantID != actx.TenantID {
		writeError(w, http.StatusForbidden, "tenant mismatch")
		return
	}
	// 实时刷新每个任务的状态。
	items := make([]batchTaskItem, len(bt.Tasks))
	for i, it := range bt.Tasks {
		items[i] = it
		if it.TaskID == "" {
			// 创建阶段即失败的设备（无 TaskID），保留原状态不刷新。
			continue
		}
		t, getErr := g.svc.GetTask(r.Context(), &taskv1.GetTaskRequest{TaskId: it.TaskID})
		if getErr == nil && t != nil {
			items[i].Status = t.Status
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"batchID":   bt.BatchID,
		"taskType":  bt.TaskType,
		"command":   bt.Command,
		"createdAt": bt.CreatedAt,
		"createdBy": bt.CreatedBy,
		"tasks":     items,
	})
}

// handleBatchRouting 路由分派 /api/v1/tasks/batch/{id}。
//
// GET → handleBatchStatus
func (g *Gateway) handleBatchRouting(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/batch/")
	if id == "" {
		writeError(w, http.StatusBadRequest, "batch id required")
		return
	}
	// 仅支持单段 id（无子路径），子路径返回 404。
	if strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	g.handleBatchStatus(w, r, id)
}

// RegisterBatchRoutes 注册批量执行相关路由到给定 mux。
//
// 路由与 gateway.go RegisterRoutes 中的 /api/v1/tasks/ 前缀存在重叠，
// 但 http.ServeMux 按最长前缀匹配：/api/v1/tasks/batch-exec 和 /api/v1/tasks/batch/
// 比 /api/v1/tasks/ 更具体，会优先匹配，不会冲突。
//
// 由 main 在 RegisterRoutes 之外单独调用（或由 team leader 统一编排）。
func (g *Gateway) RegisterBatchRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/tasks/batch-exec", g.handleBatchExec)
	mux.HandleFunc("/api/v1/tasks/batch/", g.handleBatchRouting)
}
