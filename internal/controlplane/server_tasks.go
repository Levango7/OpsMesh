
// server_tasks.go 任务相关 HTTP handler 与后台循环。
//
// 从 server.go 拆分而来（task 114：按路由域拆分巨型 server.go）。
// 包含任务下发/列表/取消/审批等端点，以及定时调度/归档/租约回收/选主等后台循环，
// 逻辑未做任何修改。
package controlplane

import (
	"context"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"opsmesh/internal/domain"
	"opsmesh/internal/events"
	"opsmesh/internal/logx"
	"opsmesh/internal/proto"
)

// handleCreateTask 处理 POST /api/v1/tasks：内部下发入口（P0-2）。
// 请求体：{ "agentID": "...", "type": "shell", "command": "...", "tenantID": "可选" }
// 租户隔离：任务只能下发给本租户（网关注入）的 agent；缺失则按 body.tenantID 兜底。
func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// P1-6 联邦入站验签：带转发标记的请求必须验签，防跨不可信网段伪造租户身份。
	if err := s.verifyFederationRequest(r); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
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
		AgentID          string `json:"agentID"`
		Type             string `json:"type"`
		Command          string `json:"command"`
		TenantID         string `json:"tenantID"`
		Schedule         string `json:"schedule"`         // F4 可选：5 字段 cron，设定则成为模板任务（周期派生实例）
		ApprovalRequired bool   `json:"approvalRequired"` // B1 修复 10：高风险任务需审批后才可执行
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.AgentID == "" || body.Command == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "agentID and command are required"})
		return
	}
	if body.Type == "" {
		body.Type = "shell"
	}
	// H6 认证防御：强制使用头中的租户 ID，忽略 body 中的 tenantID，防 body 覆盖头租户越权。
	targetTenant := actx.TenantID
	agent := s.lookupAgent(body.AgentID)
	if agent == nil || (targetTenant != "" && agent.TenantID != targetTenant) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "agent not found or tenant mismatch"})
		return
	}
	task := s.store.CreateTask(&proto.Task{
		AgentID:          body.AgentID,
		TenantID:         targetTenant,
		Type:             body.Type,
		Command:          body.Command,
		Schedule:         body.Schedule,         // F4 模板任务（cron）随创建下传
		MaxRetries:       s.cfg.TaskMaxRetries,  // F2 失败重试上限随任务下发（store 层按策略重入队/死信）
		ApprovalRequired: body.ApprovalRequired, // B1 修复 10：高风险任务需审批
	})
	s.store.Audit(&proto.AuditEvent{
		TenantID: targetTenant,
		UserID:   actx.UserID,
		Action:   "create_task",
		Target:   task.TaskID,
		Detail:   body.Command,
	})
	// 事件总线（P1-5）+ 队列深度观测（P2-1）。
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: targetTenant, UserID: actx.UserID,
			Action: "create_task", Target: task.TaskID, Detail: body.Command, Level: events.LevelInfo,
		})
	}
	if s.metrics != nil {
		s.metrics.SetQueueDepth(s.store.PendingDepth())
	}
	// M3-2B SSE：通知前端新任务已创建（前端任务表追加一行 pending）
	// H6 租户隔离：携带 targetTenant，仅同租户订阅者收到。
	s.publishEvent("task_status", targetTenant, map[string]string{
		"taskID":  task.TaskID,
		"status":  task.Status,
		"agentID": body.AgentID,
	})
	writeJSON(w, http.StatusCreated, task)
}

// handleListTasks 统一处理 /api/v1/tasks：
//   - GET：列出任务（租户隔离 + 可选 ?status= 过滤 + B1 修复 3 分页），经 domain 防腐层对外暴露领域模型。
//   - POST：下发给指定 agent（逻辑复用 handleCreateTask，P0-2 内部下发入口）。
func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleCreateTask(w, r)
		return
	}
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "task:read"); !ok {
		return
	}
	status := r.URL.Query().Get("status")
	tasks := s.store.AllTasks(actx.TenantID)
	out := make([]*domain.Task, 0, len(tasks))
	for _, t := range tasks {
		if status != "" && t.Status != status {
			continue
		}
		out = append(out, domain.TaskFromProto(t))
	}
	// B1 修复 3：分页（向后兼容：不传 page 返回全量）。
	page, pageSize := parsePagination(r.URL.Query())
	if page == 0 {
		writeJSON(w, http.StatusOK, out)
		return
	}
	total := len(out)
	start := (page - 1) * pageSize
	if start >= total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	writeJSON(w, http.StatusOK, paginateResult{
		Data: out[start:end], Total: total, Page: page, PageSize: pageSize, HasMore: end < total,
	})
}

// scheduleLoop 周期性评估模板任务（F4 定时/周期调度）：每 30s 调一次
// reg.FireDueSchedules(now)，对到点（cron 匹配且本分钟未触发）的模板派生 pending 实例。
// ctx 取消即退出。
func (s *Server) scheduleLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !s.store.IsLeader() {
				continue // A3：非 leader 不派生，避免多副本重复派生定时实例
			}
			n := s.store.FireDueSchedules(time.Now())
			if n > 0 {
				logx.Info(ctx, "定时任务派生", "fired", n)
			}
		}
	}
}

// archiveLoop F5 离线超龄自动归档：每 60s 由 leader 扫描最后心跳早于
// ArchiveAgeMin 的 agent 对应设备（或孤儿设备），批量标记 retired。
// 仅 leader 执行（归档属协调任务，避免多副本重复归档）。
func (s *Server) archiveLoop(ctx context.Context) {
	if s.cfg.ArchiveAgeMin <= 0 {
		return // 关闭自动归档（仅手动 DELETE 退役）
	}
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !s.store.IsLeader() {
				continue
			}
			n := s.store.RetireStaleDevices(time.Duration(s.cfg.ArchiveAgeMin) * time.Minute)
			if n > 0 {
				logx.Info(ctx, "离线设备自动归档", "archived", n)
			}
			if tc := s.store.CleanupTokens(1000); tc > 0 {
				logx.Info(ctx, "过期 install token 清理", "cleaned", tc)
			}
		}
	}
}

// reclaimLoop 周期性复位超期 running 任务（P0-1 任务必达）：agent 领取后超过租约租期仍未
// 上报结果，视为失联，复位 pending 重新进入调度队列。ctx 取消即退出。
func (s *Server) reclaimLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !s.store.IsLeader() {
				continue // A3：非 leader 不回收，避免多副本重复复位 running 任务
			}
			n := s.store.ReclaimStaleTasks(time.Duration(s.cfg.TaskLeaseSec) * time.Second)
			if n > 0 {
				logx.Info(ctx, "任务租约回收", "reclaimed", n)
			}
		}
	}
}

// leaderLoop A3 选主循环：每 LeaderTickSec 秒续租一次 leader 租约，
// 并通过日志在晋升/失去 leader 身份时打印一次（避免每 tick 刷屏）。
// 仅 leader 才会由 reclaimLoop/scheduleLoop（及后续 provision/离线归档循环）真正执行周期协调任务。
func (s *Server) leaderLoop(ctx context.Context) {
	tick := time.Duration(s.cfg.LeaderTickSec) * time.Second
	if tick <= 0 {
		tick = 5 * time.Second
	}
	ttl := time.Duration(s.cfg.LeaderTTLSec) * time.Second
	if ttl <= 0 {
		ttl = 15 * time.Second
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	var wasLeader bool
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			isLeader := s.store.RenewLeadership(ttl)
			if isLeader != wasLeader {
				if isLeader {
					logx.Info(ctx, "晋升为 leader，开始执行周期协调任务", "ttl", ttl.String())
				} else {
					logx.Info(ctx, "失去 leader 身份，暂停周期协调任务（其他副本接管）")
				}
				wasLeader = isLeader
			}
		}
	}
}

// handleBatchCreateTasks 处理 POST /api/v1/tasks/batch：向多台 agent 批量下发同一任务模板（P0-3 危点闭环）。
// 请求体：{ "targets":["a1","a2"], "type","command","content","path","tenantID" }
// 逐台 CreateTask（复用租户隔离校验与审计）；返回已创建任务 ID 与逐台失败条目。
func (s *Server) handleBatchCreateTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
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
		Targets          []string `json:"targets"`
		Type             string   `json:"type"`
		Command          string   `json:"command"`
		Content          string   `json:"content"`
		Path             string   `json:"path"`
		TenantID         string   `json:"tenantID"`
		Schedule         string   `json:"schedule"`         // F4 可选：批量下发的任务模板也支持 cron
		ApprovalRequired bool     `json:"approvalRequired"` // B1 修复 10：批量下发也支持审批
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if len(body.Targets) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "targets is required (non-empty)"})
		return
	}
	if body.Command == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "command is required"})
		return
	}
	if body.Type == "" {
		body.Type = "shell"
	}
	// H6 认证防御：强制使用头中的租户 ID，忽略 body 中的 tenantID，防 body 覆盖头租户越权。
	targetTenant := actx.TenantID
	created := make([]string, 0, len(body.Targets))
	type fail struct {
		Target string `json:"target"`
		Error  string `json:"error"`
	}
	fails := make([]fail, 0)
	for _, tid := range body.Targets {
		agent := s.lookupAgent(tid)
		if agent == nil || (targetTenant != "" && agent.TenantID != targetTenant) {
			fails = append(fails, fail{Target: tid, Error: "agent not found or tenant mismatch"})
			continue
		}
		task := s.store.CreateTask(&proto.Task{
			AgentID:          tid,
			TenantID:         targetTenant,
			Type:             body.Type,
			Command:          body.Command,
			Content:          body.Content,
			Path:             body.Path,
			Schedule:         body.Schedule,         // F4 批量模板也支持 cron
			MaxRetries:       s.cfg.TaskMaxRetries,  // F2 批量下发同样带重试上限
			ApprovalRequired: body.ApprovalRequired, // B1 修复 10：批量下发也支持审批
		})
		s.store.Audit(&proto.AuditEvent{
			TenantID: targetTenant,
			UserID:   actx.UserID,
			Action:   "create_task",
			Target:   task.TaskID,
			Detail:   "batch:" + body.Command,
		})
		if s.bus != nil {
			s.bus.Publish(r.Context(), events.Event{
				TenantID: targetTenant, UserID: actx.UserID,
				Action: "create_task", Target: task.TaskID, Detail: "batch:" + body.Command, Level: events.LevelInfo,
			})
		}
		created = append(created, task.TaskID)
		// M3-2B SSE：批量下发也逐条推送 task_status（前端实时追加任务行）
		// H6 租户隔离：携带 targetTenant，仅同租户订阅者收到。
		s.publishEvent("task_status", targetTenant, map[string]string{
			"taskID":  task.TaskID,
			"status":  task.Status,
			"agentID": tid,
		})
	}
	if s.metrics != nil {
		s.metrics.SetQueueDepth(s.store.PendingDepth())
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"count":   len(created),
		"created": created,
		"errors":  fails,
	})
}

// handleTaskRouting 统一分派 /api/v1/tasks/{id}/... 子路径：
//   - POST /api/v1/tasks/{id}/cancel：取消任务（F3）
//   - GET  /api/v1/tasks/{id}/result：查询单条执行结果（A5/F7）
//   - POST /api/v1/tasks/{id}/approve：审批通过任务（B1 修复 10）
//   - POST /api/v1/tasks/{id}/reject：审批拒绝任务（B1 修复 10）
func (s *Server) handleTaskRouting(w http.ResponseWriter, r *http.Request) {
	idAndRest := strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/")
	parts := strings.SplitN(idAndRest, "/", 2)
	id := parts[0]
	if id == "" {
		jsonError(w, http.StatusBadRequest, "task id required")
		return
	}
	switch {
	case len(parts) == 2 && parts[1] == "cancel" && r.Method == http.MethodPost:
		s.handleCancelTask(w, r, id)
	case len(parts) == 2 && parts[1] == "result" && r.Method == http.MethodGet:
		s.handleTaskResult(w, r, id)
	case len(parts) == 2 && parts[1] == "approve" && r.Method == http.MethodPost:
		s.handleApproveTask(w, r, id)
	case len(parts) == 2 && parts[1] == "reject" && r.Method == http.MethodPost:
		s.handleRejectTask(w, r, id)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found", "path": r.URL.Path})
	}
}

// handleCancelTask 处理 POST /api/v1/tasks/{id}/cancel：取消 pending/running 任务（F3）。
// 租户隔离：requireAuth 时强制用网关注入租户，禁止越权取消他租户任务。
func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "task:cancel"); !ok {
		return
	}
	tenant := actx.TenantID
	ok = s.store.CancelTask(id, tenant)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not cancellable (not found / not pending|running / tenant mismatch)"})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: tenant, UserID: actx.UserID, Action: "cancel_task", Target: id, Detail: "cancelled via HTTP",
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: tenant, UserID: actx.UserID, Action: "cancel_task", Target: id, Level: events.LevelInfo,
		})
	}
	// M3-2B SSE：通知前端任务已取消（任务表对应行状态翻 cancelled）
	// H6 租户隔离：携带 tenant，仅同租户订阅者收到。
	s.publishEvent("task_status", tenant, map[string]string{
		"taskID": id,
		"status": "cancelled",
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled", "taskID": id})
}

// handleTaskResult 处理 GET /api/v1/tasks/{id}/result：返回单条执行结果（A5/F7）。
// 租户隔离：requireAuth 时仅返回本租户任务的结果（通过任务的租户归属判定）。
func (s *Server) handleTaskResult(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "task:read"); !ok {
		return
	}
	res := s.store.TaskResult(id)
	if res == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "result not found"})
		return
	}
	// 租户隔离：结果对应的任务须属于当前租户（requireAuth 时强制）。
	// TODO(perf): 此处遍历 AllTasks 为 O(N)，任务量大时需优化为按 (tenantID, taskID) 直查。
	// 当前 store 未暴露按 (tenantID, taskID) 直查方法，暂保留遍历实现，行为不变。
	if actx.TenantID != "" {
		found := false
		for _, t := range s.store.AllTasks(actx.TenantID) {
			if t.TaskID == id {
				found = true
				break
			}
		}
		if !found {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
			return
		}
	}
	writeJSON(w, http.StatusOK, domain.TaskResultFromProto(res))
}

// ============================================================================
// B1 修复 10：作业审批 API
// ============================================================================

// 任务审批状态常量。
const (
	taskStatusPendingApproval = "pending_approval" // 待审批（审批后才可执行）
)

// handleApproveTask 处理 POST /api/v1/tasks/{id}/approve：审批通过任务。
// 将 pending_approval 状态的任务翻为 pending，使其可被 agent 领取执行。
func (s *Server) handleApproveTask(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "task:approve"); !ok {
		return
	}
	if !s.store.ApproveTask(id, actx.TenantID, actx.UserID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found, not pending_approval, or tenant mismatch"})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "approve_task", Target: id,
		Detail: "approved via HTTP",
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: actx.TenantID, UserID: actx.UserID, Action: "approve_task", Target: id, Level: events.LevelInfo,
		})
	}
	// M3-2B SSE：通知前端任务已审批通过。
	s.publishEvent("task_status", actx.TenantID, map[string]string{
		"taskID": id,
		"status": "pending",
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved", "taskID": id})
}

// handleRejectTask 处理 POST /api/v1/tasks/{id}/reject：审批拒绝任务。
// 将 pending_approval 状态的任务翻为 rejected（终态，不可执行）。
// 请求体（可选）：{"reason": "..."}
func (s *Server) handleRejectTask(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "task:approve"); !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil && err != io.EOF {
		log.Printf("controlplane: handleRejectTask 解析请求体失败: %v", err)
	}
	if !s.store.RejectTask(id, actx.TenantID, actx.UserID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found, not pending_approval, or tenant mismatch"})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "reject_task", Target: id,
		Detail: "rejected via HTTP: " + body.Reason,
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: actx.TenantID, UserID: actx.UserID, Action: "reject_task", Target: id, Level: events.LevelInfo,
		})
	}
	// M3-2B SSE：通知前端任务已审批拒绝。
	s.publishEvent("task_status", actx.TenantID, map[string]string{
		"taskID": id,
		"status": "rejected",
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected", "taskID": id})
}