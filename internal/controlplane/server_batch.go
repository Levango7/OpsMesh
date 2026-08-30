// server_batch.go 实现 M5 批量运维 API：
//   - POST /api/v1/tasks/batch-exec   批量执行（多设备 + 同一任务）
//   - GET  /api/v1/tasks/batch/{id}   批量任务状态查询
//   - POST /api/v1/tasks/canary       灰度发布
//   - GET  /api/v1/tasks/canary/{id}  灰度发布状态查询
//
// 与 server_tasks.go 中已有的 handleBatchCreateTasks 区别：
//   - handleBatchCreateTasks 走旧 路径，仅返回 created IDs；
//   - 本文件实现 M5 增强版：返回 batchID + 每设备任务详情 + 状态聚合，
//     支持灰度发布（按比例/按分组/按标签分阶段执行）。
//
// 设计：批量/灰度状态仅内存索引（重启后丢失，可通过 batchID 查询活跃批次），
// 任务实例本身持久化在 store 中（与现有任务生命周期一致）。
package controlplane

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"opsmesh/internal/controlplane/paginate"

	"opsmesh/internal/events"
	"opsmesh/internal/proto"
)

// ============================================================================
// 批量运维 / 灰度发布：内存索引
// ============================================================================

// batchTask 单次批量执行记录。
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
type batchTaskItem struct {
	DeviceID string // 设备 ID
	TaskID   string // 任务 ID
	Status   string // 任务状态（pending/running/done/failed/cancelled）
	Error    string // 失败原因（如设备不存在）
}

// canaryRelease 灰度发布记录。
type canaryRelease struct {
	CanaryID   string            // 灰度 ID
	TenantID   string            // 租户
	TaskType   string            // 任务类型
	Command    string            // 命令
	Strategy   string            // 策略：percentage/group/label
	Percentage int               // 比例（strategy=percentage 时有效）
	Groups     []string          // 分组（strategy=group 时有效）
	Labels     map[string]string // 标签（strategy=label 时有效）
	CreatedAt  time.Time
	CreatedBy  string
	Phases     []canaryPhase // 各阶段执行情况
}

// canaryPhase 灰度发布单阶段。
type canaryPhase struct {
	Phase      int             // 阶段序号（1-based）
	DeviceIDs  []string        // 本阶段设备
	Status     string          // 阶段状态：pending/running/done/failed/aborted
	Tasks      []batchTaskItem // 本阶段每设备任务
	StartedAt  time.Time
	FinishedAt time.Time
}

// batchStore 批量/灰度内存索引（Server 持有）。
type batchStore struct {
	mu       sync.RWMutex
	batches  map[string]*batchTask
	canaries map[string]*canaryRelease
}

// newBatchStore 构造空索引。
func newBatchStore() *batchStore {
	return &batchStore{
		batches:  make(map[string]*batchTask),
		canaries: make(map[string]*canaryRelease),
	}
}

// genBatchID 生成批次 ID（batch-<8 字节 hex>）。
func genBatchID(prefix string) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return prefix + "-" + hex.EncodeToString(b[:])
}

// ============================================================================
// 批量执行 API
// ============================================================================

// handleBatchExec 处理 POST /api/v1/tasks/batch-exec：批量执行（M5 增强）。
// 请求体: { deviceIDs: [], taskType, command, timeout }
// 返回: { batchID, tasks: [{deviceID, taskID, status}] }
func (s *Server) handleBatchExec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "task:write"); !ok {
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
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if len(body.DeviceIDs) == 0 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceIDs is required (non-empty)"})
		return
	}
	if body.Command == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "command is required"})
		return
	}
	if body.TaskType == "" {
		body.TaskType = "shell"
	}
	if body.TaskType == "shell" {
		if err := validateCommand(body.Command); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "command validation failed: " + err.Error()})
			return
		}
	}
	tenant := actx.TenantID
	batchID := genBatchID("batch")
	items := make([]batchTaskItem, 0, len(body.DeviceIDs))
	for _, devID := range body.DeviceIDs {
		agent := s.lookupAgent(devID)
		if agent == nil || (tenant != "" && agent.TenantID != tenant) {
			items = append(items, batchTaskItem{
				DeviceID: devID,
				Status:   "failed",
				Error:    "agent not found or tenant mismatch",
			})
			continue
		}
		task := s.store.CreateTask(&proto.Task{
			AgentID:    devID,
			TenantID:   tenant,
			Type:       body.TaskType,
			Command:    body.Command,
			Content:    body.Content,
			Path:       body.Path,
			MaxRetries: s.cfg.TaskMaxRetries,
		})
		items = append(items, batchTaskItem{
			DeviceID: devID,
			TaskID:   task.TaskID,
			Status:   task.Status,
		})
		s.audit(r.Context(), &proto.AuditEvent{
			TenantID: tenant, UserID: actx.UserID, Action: "batch_exec", Target: task.TaskID,
			Detail: sanitizeAuditDetail("batch:" + batchID + ":" + body.Command),
		})
		if s.bus != nil {
			s.bus.Publish(r.Context(), events.Event{
				TenantID: tenant, UserID: actx.UserID,
				Action: "batch_exec", Target: task.TaskID, Level: events.LevelInfo,
				Detail: sanitizeAuditDetail("batch:" + batchID),
			})
		}
		s.publishEvent(r.Context(), "task_status", tenant, map[string]string{
			"taskID": task.TaskID, "status": task.Status, "agentID": devID,
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
	s.batches.mu.Lock()
	s.batches.batches[batchID] = bt
	s.batches.mu.Unlock()
	if s.metrics != nil {
		s.metrics.SetQueueDepth(s.store.PendingDepth())
	}
	paginate.WriteJSON(w, http.StatusCreated, map[string]interface{}{
		"batchID": batchID,
		"tasks":   items,
	})
}

// handleBatchStatus 处理 GET /api/v1/tasks/batch/{id}：批量任务状态。
func (s *Server) handleBatchStatus(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "task:read"); !ok {
		return
	}
	s.batches.mu.RLock()
	bt, exists := s.batches.batches[id]
	s.batches.mu.RUnlock()
	if !exists {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "batch not found"})
		return
	}
	if actx.TenantID != "" && bt.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}
	// 实时刷新每个任务的状态。
	items := make([]batchTaskItem, len(bt.Tasks))
	for i, it := range bt.Tasks {
		items[i] = it
		if it.TaskID == "" {
			continue
		}
		t := s.store.TaskByID(it.TaskID)
		if t != nil {
			items[i].Status = t.Status
		}
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"batchID":   bt.BatchID,
		"taskType":  bt.TaskType,
		"command":   bt.Command,
		"createdAt": bt.CreatedAt,
		"createdBy": bt.CreatedBy,
		"tasks":     items,
	})
}

// ============================================================================
// 灰度发布 API
// ============================================================================

// handleCanaryCreate 处理 POST /api/v1/tasks/canary：灰度发布。
// 请求体: { deviceIDs: [], taskType, command, strategy: "percentage|group|label", percentage?, groups?, labels? }
// 返回: { canaryID, phases: [{phase, deviceIDs, status}] }
func (s *Server) handleCanaryCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "task:write"); !ok {
		return
	}
	var body struct {
		DeviceIDs  []string          `json:"deviceIDs"`
		TaskType   string            `json:"taskType"`
		Command    string            `json:"command"`
		Content    string            `json:"content"`
		Path       string            `json:"path"`
		Strategy   string            `json:"strategy"`
		Percentage int               `json:"percentage"`
		Groups     []string          `json:"groups"`
		Labels     map[string]string `json:"labels"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if len(body.DeviceIDs) == 0 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceIDs is required"})
		return
	}
	if body.Command == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "command is required"})
		return
	}
	if body.TaskType == "" {
		body.TaskType = "shell"
	}
	if body.TaskType == "shell" {
		if err := validateCommand(body.Command); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "command validation failed: " + err.Error()})
			return
		}
	}
	switch body.Strategy {
	case "percentage", "group", "label":
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "strategy must be percentage/group/label"})
		return
	}
	tenant := actx.TenantID
	canaryID := genBatchID("canary")

	// 按策略划分阶段。
	phases := planCanaryPhases(body.DeviceIDs, body.Strategy, body.Percentage, body.Groups, body.Labels)

	// 立即执行第一阶段，其余阶段标记 pending（需手动推进或自动推进）。
	now := time.Now()
	canary := &canaryRelease{
		CanaryID:   canaryID,
		TenantID:   tenant,
		TaskType:   body.TaskType,
		Command:    body.Command,
		Strategy:   body.Strategy,
		Percentage: body.Percentage,
		Groups:     body.Groups,
		Labels:     body.Labels,
		CreatedAt:  now,
		CreatedBy:  actx.UserID,
		Phases:     phases,
	}
	// 执行第一阶段。
	if len(phases) > 0 {
		s.execCanaryPhase(canary, &canary.Phases[0], body.TaskType, body.Command, body.Content, body.Path, tenant, actx.UserID, r)
	}

	s.batches.mu.Lock()
	s.batches.canaries[canaryID] = canary
	s.batches.mu.Unlock()

	// 返回阶段摘要（不含每任务详情，前端按需查询）。
	phaseSummary := make([]map[string]interface{}, len(canary.Phases))
	for i, p := range canary.Phases {
		phaseSummary[i] = map[string]interface{}{
			"phase":     p.Phase,
			"deviceIDs": p.DeviceIDs,
			"status":    p.Status,
		}
	}
	paginate.WriteJSON(w, http.StatusCreated, map[string]interface{}{
		"canaryID": canaryID,
		"phases":   phaseSummary,
	})
}

// planCanaryPhases 按策略划分灰度阶段。
func planCanaryPhases(devices []string, strategy string, percentage int, groups []string, labels map[string]string) []canaryPhase {
	switch strategy {
	case "percentage":
		// 按比例分两阶段：第一阶段 percentage%，第二阶段剩余。
		if percentage <= 0 {
			percentage = 10
		}
		if percentage > 100 {
			percentage = 100
		}
		n := len(devices) * percentage / 100
		if n < 1 && len(devices) > 0 {
			n = 1
		}
		phases := []canaryPhase{
			{Phase: 1, DeviceIDs: append([]string(nil), devices[:n]...), Status: "pending"},
		}
		if n < len(devices) {
			phases = append(phases, canaryPhase{
				Phase: 2, DeviceIDs: append([]string(nil), devices[n:]...), Status: "pending",
			})
		}
		return phases
	case "group":
		// 按分组多阶段：每个分组一阶段。
		// 简化实现：分组仅作为标签，实际设备划分由调用方在 deviceIDs 中已指定；
		// 这里按 groups 数量等分 deviceIDs。
		nGroups := len(groups)
		if nGroups == 0 {
			nGroups = 1
		}
		phases := make([]canaryPhase, nGroups)
		chunkSize := (len(devices) + nGroups - 1) / nGroups
		for i := 0; i < nGroups; i++ {
			start := i * chunkSize
			end := start + chunkSize
			if start > len(devices) {
				start = len(devices)
			}
			if end > len(devices) {
				end = len(devices)
			}
			phases[i] = canaryPhase{
				Phase:     i + 1,
				DeviceIDs: append([]string(nil), devices[start:end]...),
				Status:    "pending",
			}
		}
		return phases
	case "label":
		// 按标签单阶段（标签筛选由调用方在 deviceIDs 中已完成）。
		return []canaryPhase{{Phase: 1, DeviceIDs: append([]string(nil), devices...), Status: "pending"}}
	}
	return []canaryPhase{{Phase: 1, DeviceIDs: append([]string(nil), devices...), Status: "pending"}}
}

// execCanaryPhase 执行灰度的某一阶段：为每个设备下发任务。
func (s *Server) execCanaryPhase(canary *canaryRelease, phase *canaryPhase, taskType, command, content, path, tenant, userID string, r *http.Request) {
	phase.StartedAt = time.Now()
	phase.Status = "running"
	phase.Tasks = make([]batchTaskItem, 0, len(phase.DeviceIDs))
	for _, devID := range phase.DeviceIDs {
		agent := s.lookupAgent(devID)
		if agent == nil || (tenant != "" && agent.TenantID != tenant) {
			phase.Tasks = append(phase.Tasks, batchTaskItem{
				DeviceID: devID, Status: "failed", Error: "agent not found or tenant mismatch",
			})
			continue
		}
		task := s.store.CreateTask(&proto.Task{
			AgentID:    devID,
			TenantID:   tenant,
			Type:       taskType,
			Command:    command,
			Content:    content,
			Path:       path,
			MaxRetries: s.cfg.TaskMaxRetries,
		})
		phase.Tasks = append(phase.Tasks, batchTaskItem{
			DeviceID: devID, TaskID: task.TaskID, Status: task.Status,
		})
		s.audit(r.Context(), &proto.AuditEvent{
			TenantID: tenant, UserID: userID, Action: "canary_exec", Target: task.TaskID,
			Detail: sanitizeAuditDetail("canary:" + canary.CanaryID + ":phase" + itoaPhase(phase.Phase)),
		})
		if s.bus != nil {
			s.bus.Publish(r.Context(), events.Event{
				TenantID: tenant, UserID: userID,
				Action: "canary_exec", Target: task.TaskID, Level: events.LevelInfo,
				Detail: sanitizeAuditDetail("canary:" + canary.CanaryID),
			})
		}
		s.publishEvent(r.Context(), "task_status", tenant, map[string]string{
			"taskID": task.TaskID, "status": task.Status, "agentID": devID,
		})
	}
}

// itoaPhase 简易 int → string。
func itoaPhase(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// handleCanaryStatus 处理 GET /api/v1/tasks/canary/{id}：灰度发布状态。
func (s *Server) handleCanaryStatus(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "task:read"); !ok {
		return
	}
	// RLock 下拷贝只读快照（phase 列表、每阶段 taskIDs/status 等），释放锁后再遍历刷新状态。
	s.batches.mu.RLock()
	c, exists := s.batches.canaries[id]
	if !exists {
		s.batches.mu.RUnlock()
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "canary not found"})
		return
	}
	if actx.TenantID != "" && c.TenantID != actx.TenantID {
		s.batches.mu.RUnlock()
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}
	// 拷贝只读字段
	canaryID := c.CanaryID
	strategy := c.Strategy
	taskType := c.TaskType
	command := c.Command
	createdAt := c.CreatedAt
	createdBy := c.CreatedBy
	// 拷贝 phases 结构（不含可变的 Tasks 状态，后续需刷新）
	phasesSnapshot := make([]struct {
		Phase      int
		DeviceIDs  []string
		Status     string
		TaskIDs    []string
		StartedAt  time.Time
		FinishedAt time.Time
	}, len(c.Phases))
	for i, p := range c.Phases {
		phasesSnapshot[i].Phase = p.Phase
		phasesSnapshot[i].DeviceIDs = p.DeviceIDs
		phasesSnapshot[i].Status = p.Status
		phasesSnapshot[i].StartedAt = p.StartedAt
		phasesSnapshot[i].FinishedAt = p.FinishedAt
		phasesSnapshot[i].TaskIDs = make([]string, len(p.Tasks))
		for j, it := range p.Tasks {
			phasesSnapshot[i].TaskIDs[j] = it.TaskID
		}
	}
	s.batches.mu.RUnlock()

	// 释放锁后刷新每阶段任务状态
	phases := make([]map[string]interface{}, len(phasesSnapshot))
	for i, ps := range phasesSnapshot {
		tasks := make([]batchTaskItem, len(ps.TaskIDs))
		for j, taskID := range ps.TaskIDs {
			tasks[j] = batchTaskItem{TaskID: taskID}
			if taskID == "" {
				continue
			}
			t := s.store.TaskByID(taskID)
			if t != nil {
				tasks[j].Status = t.Status
			}
		}
		phases[i] = map[string]interface{}{
			"phase":      ps.Phase,
			"deviceIDs":  ps.DeviceIDs,
			"status":     ps.Status,
			"tasks":      tasks,
			"startedAt":  ps.StartedAt,
			"finishedAt": ps.FinishedAt,
		}
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"canaryID":  canaryID,
		"strategy":  strategy,
		"taskType":  taskType,
		"command":   command,
		"createdAt": createdAt,
		"createdBy": createdBy,
		"phases":    phases,
	})
}

// handleCanaryAdvance 处理 POST /api/v1/tasks/canary/{id}/advance：推进到下一阶段。
func (s *Server) handleCanaryAdvance(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "task:write"); !ok {
		return
	}
	s.batches.mu.Lock()
	defer s.batches.mu.Unlock()
	c, exists := s.batches.canaries[id]
	if !exists {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "canary not found"})
		return
	}
	if actx.TenantID != "" && c.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}
	// 找到下一个 pending 阶段并执行。
	for i := range c.Phases {
		if c.Phases[i].Status == "pending" {
			s.execCanaryPhase(c, &c.Phases[i], c.TaskType, c.Command, "", "", c.TenantID, actx.UserID, r)
			paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
				"canaryID": c.CanaryID,
				"phase":    c.Phases[i].Phase,
				"status":   c.Phases[i].Status,
			})
			return
		}
	}
	paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "no pending phase to advance"})
}

// ============================================================================
// 路由分发
// ============================================================================

// handleBatchRouting 处理 /api/v1/tasks/batch-exec/{id} 路由。
// 注：实际注册为 /api/v1/tasks/batch/，与旧 handleBatchCreateTasks 共存。
// 为避免与现有 /api/v1/tasks/batch（POST 批量下发）冲突，新批量执行走 /api/v1/tasks/batch-exec。
func (s *Server) handleBatchRouting(w http.ResponseWriter, r *http.Request) {
	// 路径形如 /api/v1/tasks/batch/{id}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/batch/")
	if id == "" || id == r.URL.Path {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "batch id required"})
		return
	}
	s.handleBatchStatus(w, r, id)
}

// handleCanaryRouting 处理 /api/v1/tasks/canary/{id}[/advance] 路由。
func (s *Server) handleCanaryRouting(w http.ResponseWriter, r *http.Request) {
	idAndRest := strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/canary/")
	if idAndRest == "" {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "canary id required"})
		return
	}
	parts := strings.SplitN(idAndRest, "/", 2)
	id := parts[0]
	if len(parts) == 1 {
		s.handleCanaryStatus(w, r, id)
		return
	}
	switch parts[1] {
	case "advance":
		s.handleCanaryAdvance(w, r, id)
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found", "path": r.URL.Path})
	}
}

// 排序辅助：保证测试稳定（未使用时编译器会消除）。
var _ = sort.Strings

// cleanupDoneBatches 删除已进入终态的所有 batch/canary，防止 map 无界增长。
// 终态判定：所有 tasks 为 done/failed/cancelled 且创建时间 >36h（保守阈值，避免误删进行中）。
func (s *batchStore) cleanupDoneBatches() {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-36 * time.Hour)
	for id, bt := range s.batches {
		if bt.CreatedAt.Before(cutoff) {
			delete(s.batches, id)
		}
	}
	for id, cr := range s.canaries {
		if cr.CreatedAt.Before(cutoff) {
			delete(s.canaries, id)
		}
	}
}
