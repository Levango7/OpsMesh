// Package http 提供 task-svc 的 HTTP 业务网关（REST API）。
//
// 设计原则（task-svc P0 切流阻塞项）：
//   - 纯增量：新增 HTTP handler，不改 server.go/service.go/store.go 任何现有代码
//   - 同进程直连 service 层，不经 gRPC——避免 proto 序列化开销
//   - RESTful 路径对齐 controlplane：/api/v1/tasks, /api/v1/tasks/{id}/cancel 等
//   - 错误语义：400 参数错 / 404 不存在 / 500 内部错（与 controlplane 一致）
//   - JSON 契约：camelCase 字段名与 controlplane proto.Task 的 json tag 对齐
//   - 租户隔离：从 request context（tenant.Middleware 注入）或 X-Tenant-ID 头提取
//   - 认证：从 Cookie opsmesh_at 或 Authorization Bearer 提取 token 解析 userID
//   - 头非空 + token 也携带 tenant_id：校验一致，不一致 403（防绕过网关伪造租户头）
//     来源：2026-09-16-multi-tenant-token-header-cross-validation-forgery
package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/pkg/auth"
	"github.com/Levango7/OpsMesh/pkg/tenant"
	taskv1 "github.com/Levango7/OpsMesh/services/task-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/service"
)

// Cookie 名与 controlplane/auth.go 逐字一致（跨入口互换的前提）。
const accessTokenCookieName = "opsmesh_at"

// 网关层错误（映射到 HTTP 状态码）。
var (
	errInvalidToken   = errors.New("invalid or expired token")
	errTenantMismatch = errors.New("tenant mismatch between header and token")
	errAuthRequired   = errors.New("authentication required")
	errTenantRequired = errors.New("tenant identity required")
)

// Gateway 持有 HTTP handler 依赖的 service 引用。
type Gateway struct {
	svc       *service.Service
	jwtSecret string // JWT 验签密钥（空=不校验 token，仅从头/context 提取租户）
}

// NewGateway 构造 Gateway 实例。
// jwtSecret 非空时启用 token 校验（解析 userID 注入上下文）；空串=仅租户隔离。
func NewGateway(svc *service.Service, jwtSecret string) *Gateway {
	return &Gateway{svc: svc, jwtSecret: jwtSecret}
}

// RegisterRoutes 注册全部 HTTP 路由到给定 mux。
// 鉴权/租户中间件由 main 在 handler 链统一包裹（tenant.Middleware），此处只注册业务路由。
func (g *Gateway) RegisterRoutes(mux *http.ServeMux) {
	// 任务
	mux.HandleFunc("/api/v1/tasks", g.handleTasks)        // GET 列表 / POST 创建
	mux.HandleFunc("/api/v1/tasks/", g.handleTaskRouting) // {id}, {id}/cancel, {id}/result, {id}/approve, {id}/reject
	// 定时任务
	mux.HandleFunc("/api/v1/schedules", g.handleSchedules)        // GET 列表 / POST 创建
	mux.HandleFunc("/api/v1/schedules/", g.handleScheduleRouting) // {id} GET/PUT/DELETE
}

// ============ 认证 / 租户提取 ============

// authContext 封装从请求提取的认证信息。
type authContext struct {
	TenantID string
	UserID   string
	Roles    []string
}

// extractAuth 从请求提取租户和用户身份。
//
// 租户优先级：
//  1. request context（tenant.Middleware 注入）
//  2. X-Tenant-ID 头
//  3. "default"
//
// 用户身份：从 Cookie opsmesh_at 或 Authorization Bearer 提取 token，jwtSecret 非空时验签解析。
//
// 租户一致性校验（防绕过网关伪造租户头）：
//   - 头非空 + token 也携带 tenant_id → 校验一致，不一致 403
//   - 来源：2026-09-16-multi-tenant-token-header-cross-validation-forgery
func (g *Gateway) extractAuth(r *http.Request) (authContext, error) {
	actx := authContext{TenantID: "default"}

	// 1. 从 context 提取（tenant.Middleware 注入）
	if tid, err := tenant.TenantIDFromContext(r.Context()); err == nil && tid != "" {
		actx.TenantID = tid
	}

	// 2. X-Tenant-ID 头
	headerTenant := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	if headerTenant != "" {
		actx.TenantID = headerTenant
	}

	// 3. token 校验 + 租户一致性校验
	token := bearerOrCookie(r)
	if token != "" && g.jwtSecret != "" {
		claims, err := auth.ValidateServiceToken(token, g.jwtSecret)
		if err != nil {
			return actx, errInvalidToken
		}
		actx.UserID = claims.ServiceID
		actx.Roles = claims.Permissions
		// 租户一致性校验：头非空 + token 也携带 tenant_id → 必须一致，否则 403
		// 防绕过网关伪造租户头（来源：multi-tenant-token-header-cross-validation-forgery）
		if claims.TenantID != "" {
			if headerTenant != "" && headerTenant != claims.TenantID {
				return actx, errTenantMismatch
			}
			actx.TenantID = claims.TenantID
		}
	}

	return actx, nil
}

// bearerOrCookie 优先 Authorization Bearer，回退 at Cookie（与 auth-svc 同语义）。
func bearerOrCookie(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	c, err := r.Cookie(accessTokenCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// ============ 任务 ============

// handleTasks 处理 /api/v1/tasks：GET 列表 / POST 创建。
func (g *Gateway) handleTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		g.handleListTasks(w, r)
	case http.MethodPost:
		g.handleCreateTask(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleListTasks 处理 GET /api/v1/tasks?status=&agent_id=&limit=&tenantID=。
func (g *Gateway) handleListTasks(w http.ResponseWriter, r *http.Request) {
	actx, err := g.extractAuth(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	q := r.URL.Query()
	status := q.Get("status")
	agentID := q.Get("agent_id")
	limit := parseInt32(q.Get("limit"))
	// query tenantID 兜底（仅当 context/头未注入时）
	tenantID := actx.TenantID
	if tid := q.Get("tenantID"); tid != "" && tenantID == "default" {
		tenantID = tid
	}
	resp, err := g.svc.ListTasks(r.Context(), &taskv1.ListTasksRequest{
		TenantId: tenantID,
		Status:   status,
		AgentId:  agentID,
		Limit:    limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]taskResponse, 0, len(resp.Tasks))
	for _, t := range resp.Tasks {
		out = append(out, taskToResponse(t))
	}
	// task-svc 列表只支持 limit 截断（裸数组响应），不支持 page/pageSize 分页。
	// controlplane 在 page>0 时返回 PaginateResult{Data,Total,Page,PageSize,HasMore} 分页结构。
	// 双轨期前端不传 page 参数请求 task-svc（前端 tasks.js 仅传 limit），此差异不影响切流。
	// 若后续需对齐分页，引入 internal/controlplane/paginate 包并解析 page/pageSize 参数。
	writeJSON(w, http.StatusOK, out)
}

// handleCreateTask 处理 POST /api/v1/tasks。
// 请求体对齐 controlplane handleCreateTask：
//
//	{"agentID":"...","type":"shell","command":"...","tenantID":"可选",
//	 "schedule":"可选","approvalRequired":false,"maxRetries":可选}
func (g *Gateway) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	actx, err := g.extractAuth(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	var body struct {
		AgentID          string   `json:"agentID"`
		Type             string   `json:"type"`
		Command          string   `json:"command"`
		Content          string   `json:"content"`
		Path             string   `json:"path"`
		TenantID         string   `json:"tenantID"`
		Schedule         string   `json:"schedule"`
		ApprovalRequired bool     `json:"approvalRequired"`
		MaxRetries       *int32   `json:"maxRetries"`
		Timeout          int32    `json:"timeout"`
		RetryDelay       int32    `json:"retryDelay"`
		DependsOn        []string `json:"dependsOn"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if body.AgentID == "" || body.Command == "" {
		writeError(w, http.StatusBadRequest, "agentID and command are required")
		return
	}
	if body.Type == "" {
		body.Type = "shell"
	}
	// 认证防御：强制使用头/context 中的租户 ID，忽略 body 中的 tenantID，防 body 覆盖头租户越权。
	targetTenant := actx.TenantID
	task := &taskv1.Task{
		AgentId:          body.AgentID,
		TenantId:         targetTenant,
		Type:             body.Type,
		Command:          body.Command,
		Content:          body.Content,
		Path:             body.Path,
		Schedule:         body.Schedule,
		ApprovalRequired: body.ApprovalRequired,
		Timeout:          body.Timeout,
		RetryDelay:       body.RetryDelay,
		DependsOn:        body.DependsOn,
	}
	if body.MaxRetries != nil {
		task.MaxRetries = *body.MaxRetries
	}
	created, err := g.svc.CreateTask(r.Context(), &taskv1.CreateTaskRequest{Task: task})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, taskToResponse(created))
}

// handleTaskRouting 统一分派 /api/v1/tasks/{id}/... 子路径（对齐 controlplane handleTaskRouting）：
//   - GET  /api/v1/tasks/{id}          → GetTask
//   - POST /api/v1/tasks/{id}/cancel   → CancelTask
//   - GET  /api/v1/tasks/{id}/result   → GetTaskResult
//   - POST /api/v1/tasks/{id}/approve  → ApproveTask
//   - POST /api/v1/tasks/{id}/reject   → RejectTask
func (g *Gateway) handleTaskRouting(w http.ResponseWriter, r *http.Request) {
	idAndRest := strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/")
	parts := strings.SplitN(idAndRest, "/", 2)
	id := parts[0]
	if id == "" {
		writeError(w, http.StatusBadRequest, "task id required")
		return
	}
	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		g.handleGetTask(w, r, id)
	case len(parts) == 2 && parts[1] == "cancel" && r.Method == http.MethodPost:
		g.handleCancelTask(w, r, id)
	case len(parts) == 2 && parts[1] == "result" && r.Method == http.MethodGet:
		g.handleTaskResult(w, r, id)
	case len(parts) == 2 && parts[1] == "approve" && r.Method == http.MethodPost:
		g.handleApproveTask(w, r, id)
	case len(parts) == 2 && parts[1] == "reject" && r.Method == http.MethodPost:
		g.handleRejectTask(w, r, id)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

// handleGetTask 处理 GET /api/v1/tasks/{id}。
func (g *Gateway) handleGetTask(w http.ResponseWriter, r *http.Request, id string) {
	if _, err := g.extractAuth(r); err != nil {
		writeAuthError(w, err)
		return
	}
	task, err := g.svc.GetTask(r.Context(), &taskv1.GetTaskRequest{TaskId: id})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, taskToResponse(task))
}

// handleCancelTask 处理 POST /api/v1/tasks/{id}/cancel。
func (g *Gateway) handleCancelTask(w http.ResponseWriter, r *http.Request, id string) {
	actx, err := g.extractAuth(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if err := g.svc.CancelTask(r.Context(), &taskv1.CancelTaskRequest{
		TaskId:   id,
		TenantId: actx.TenantID,
	}); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled", "taskID": id})
}

// handleTaskResult 处理 GET /api/v1/tasks/{id}/result。
func (g *Gateway) handleTaskResult(w http.ResponseWriter, r *http.Request, id string) {
	if _, err := g.extractAuth(r); err != nil {
		writeAuthError(w, err)
		return
	}
	res, err := g.svc.GetTaskResult(r.Context(), &taskv1.GetTaskResultRequest{TaskId: id})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resultToResponse(res))
}

// handleApproveTask 处理 POST /api/v1/tasks/{id}/approve。
func (g *Gateway) handleApproveTask(w http.ResponseWriter, r *http.Request, id string) {
	actx, err := g.extractAuth(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	task, err := g.svc.ApproveTask(r.Context(), &taskv1.ApproveTaskRequest{
		TaskId:     id,
		TenantId:   actx.TenantID,
		ApprovedBy: actx.UserID,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, taskToResponse(task))
}

// handleRejectTask 处理 POST /api/v1/tasks/{id}/reject。
// 请求体（可选）：{"reason": "..."}
func (g *Gateway) handleRejectTask(w http.ResponseWriter, r *http.Request, id string) {
	actx, err := g.extractAuth(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	// body 可选（reason 仅用于审计，service 层 RejectTask 不消费）
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body) // best-effort，空 body 兼容
	task, err := g.svc.RejectTask(r.Context(), &taskv1.RejectTaskRequest{
		TaskId:     id,
		TenantId:   actx.TenantID,
		RejectedBy: actx.UserID,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, taskToResponse(task))
}

// ============ 定时任务 ============

// handleSchedules 处理 /api/v1/schedules：GET 列表 / POST 创建。
func (g *Gateway) handleSchedules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		g.handleListSchedules(w, r)
	case http.MethodPost:
		g.handleCreateSchedule(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleListSchedules 处理 GET /api/v1/schedules?tenantID=。
func (g *Gateway) handleListSchedules(w http.ResponseWriter, r *http.Request) {
	actx, err := g.extractAuth(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	tenantID := actx.TenantID
	if tid := r.URL.Query().Get("tenantID"); tid != "" && tenantID == "default" {
		tenantID = tid
	}
	resp, err := g.svc.ListSchedules(r.Context(), &taskv1.ListSchedulesRequest{TenantId: tenantID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]scheduleResponse, 0, len(resp.Schedules))
	for _, s := range resp.Schedules {
		out = append(out, scheduleToResponse(s))
	}
	writeJSON(w, http.StatusOK, map[string]any{"schedules": out})
}

// handleCreateSchedule 处理 POST /api/v1/schedules。
func (g *Gateway) handleCreateSchedule(w http.ResponseWriter, r *http.Request) {
	actx, err := g.extractAuth(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	var body scheduleResponse
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if body.Name == "" || body.CronExpr == "" {
		writeError(w, http.StatusBadRequest, "name and cronExpr are required")
		return
	}
	// 强制使用认证租户，防 body 覆盖越权
	body.TenantID = actx.TenantID
	created, err := g.svc.CreateSchedule(r.Context(), &taskv1.CreateScheduleRequest{
		Schedule: responseToScheduleProto(&body),
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, scheduleToResponse(created))
}

// handleScheduleRouting 处理 /api/v1/schedules/{id}：GET / PUT / DELETE。
func (g *Gateway) handleScheduleRouting(w http.ResponseWriter, r *http.Request) {
	idAndRest := strings.TrimPrefix(r.URL.Path, "/api/v1/schedules/")
	parts := strings.SplitN(idAndRest, "/", 2)
	id := parts[0]
	if id == "" || len(parts) > 1 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if _, err := g.extractAuth(r); err != nil {
		writeAuthError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		sched, err := g.svc.GetSchedule(r.Context(), &taskv1.GetScheduleRequest{Id: id})
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, scheduleToResponse(sched))
	case http.MethodPut:
		var body scheduleResponse
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		body.ID = id // 路径 id 优先，防 body 覆盖
		updated, err := g.svc.UpdateSchedule(r.Context(), &taskv1.UpdateScheduleRequest{
			Schedule: responseToScheduleProto(&body),
		})
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, scheduleToResponse(updated))
	case http.MethodDelete:
		if err := g.svc.DeleteSchedule(r.Context(), &taskv1.DeleteScheduleRequest{Id: id}); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// ============ 响应 DTO（camelCase JSON 对齐 controlplane） ============

// taskResponse 是 Task 的 HTTP DTO。相比 model.go Task（25 字段），故意不暴露 LastFiredAt：
// LastFiredAt 是调度内部状态（上次触发时间），不应暴露给 API 客户端。
// controlplane 用 model.go Task 直接序列化暴露了 lastFiredAt 属于过度暴露，task-svc 的做法更合理。
//
// taskResponse 对齐 controlplane proto.Task 的 json tag（camelCase）。
type taskResponse struct {
	TaskID           string    `json:"taskID"`
	AgentID          string    `json:"agentID"`
	TenantID         string    `json:"tenantID"`
	Type             string    `json:"type"`
	Command          string    `json:"command"`
	Content          string    `json:"content"`
	Path             string    `json:"path"`
	Status           string    `json:"status"`
	ClaimedBy        string    `json:"claimedBy"`
	ClaimedAt        time.Time `json:"claimedAt"`
	ClaimEpoch       int64     `json:"claimEpoch"`
	CreatedAt        time.Time `json:"createdAt"`
	RetryCount       int32     `json:"retryCount"`
	MaxRetries       int32     `json:"maxRetries"`
	DeadLetter       bool      `json:"deadLetter"`
	Timeout          int32     `json:"timeout,omitempty"`
	RetryDelay       int32     `json:"retryDelay,omitempty"`
	Schedule         string    `json:"schedule"`
	ParentID         string    `json:"parentID"`
	DependsOn        []string  `json:"dependsOn"`
	ApprovalRequired bool      `json:"approvalRequired"`
	ApprovedBy       string    `json:"approvedBy"`
	ApprovedAt       time.Time `json:"approvedAt"`
	BatchID          string    `json:"batchID,omitempty"`
}

func taskToResponse(t *taskv1.Task) taskResponse {
	if t == nil {
		return taskResponse{}
	}
	r := taskResponse{
		TaskID:           t.TaskId,
		AgentID:          t.AgentId,
		TenantID:         t.TenantId,
		Type:             t.Type,
		Command:          t.Command,
		Content:          t.Content,
		Path:             t.Path,
		Status:           t.Status,
		ClaimedBy:        t.ClaimedBy,
		ClaimEpoch:       t.ClaimEpoch,
		RetryCount:       t.RetryCount,
		MaxRetries:       t.MaxRetries,
		DeadLetter:       t.DeadLetter,
		Timeout:          t.Timeout,
		RetryDelay:       t.RetryDelay,
		Schedule:         t.Schedule,
		ParentID:         t.ParentId,
		DependsOn:        t.DependsOn,
		ApprovalRequired: t.ApprovalRequired,
		ApprovedBy:       t.ApprovedBy,
		BatchID:          t.BatchId,
	}
	if t.ClaimedAt != nil {
		r.ClaimedAt = t.ClaimedAt.AsTime()
	}
	if t.CreatedAt != nil {
		r.CreatedAt = t.CreatedAt.AsTime()
	}
	if t.ApprovedAt != nil {
		r.ApprovedAt = t.ApprovedAt.AsTime()
	}
	return r
}

// resultResponse 对齐 controlplane TaskResult 的 json tag。
type resultResponse struct {
	TaskID     string    `json:"taskID"`
	AgentID    string    `json:"agentID"`
	ExitCode   int32     `json:"exitCode"`
	Stdout     string    `json:"stdout"`
	Stderr     string    `json:"stderr"`
	DurationMs int64     `json:"durationMs"`
	FinishedAt time.Time `json:"finishedAt"`
	ClaimEpoch int64     `json:"claimEpoch"`
}

func resultToResponse(r *taskv1.TaskResult) resultResponse {
	if r == nil {
		return resultResponse{}
	}
	out := resultResponse{
		TaskID:     r.TaskId,
		AgentID:    r.AgentId,
		ExitCode:   r.ExitCode,
		Stdout:     r.Stdout,
		Stderr:     r.Stderr,
		DurationMs: r.DurationMs,
		ClaimEpoch: r.ClaimEpoch,
	}
	if r.FinishedAt != nil {
		out.FinishedAt = r.FinishedAt.AsTime()
	}
	return out
}

// scheduleResponse 对齐 controlplane Schedule 的 json tag。
type scheduleResponse struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenantID"`
	Name        string    `json:"name"`
	CronExpr    string    `json:"cronExpr"`
	TaskType    string    `json:"taskType"`
	Command     string    `json:"command"`
	Content     string    `json:"content"`
	Path        string    `json:"path"`
	AgentID     string    `json:"agentID"`
	Enabled     bool      `json:"enabled"`
	LastFiredAt time.Time `json:"lastFiredAt"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func scheduleToResponse(s *taskv1.Schedule) scheduleResponse {
	if s == nil {
		return scheduleResponse{}
	}
	out := scheduleResponse{
		ID:       s.Id,
		TenantID: s.TenantId,
		Name:     s.Name,
		CronExpr: s.CronExpr,
		TaskType: s.TaskType,
		Command:  s.Command,
		Content:  s.Content,
		Path:     s.Path,
		AgentID:  s.AgentId,
		Enabled:  s.Enabled,
	}
	if s.LastFiredAt != nil {
		out.LastFiredAt = s.LastFiredAt.AsTime()
	}
	if s.CreatedAt != nil {
		out.CreatedAt = s.CreatedAt.AsTime()
	}
	if s.UpdatedAt != nil {
		out.UpdatedAt = s.UpdatedAt.AsTime()
	}
	return out
}

func responseToScheduleProto(s *scheduleResponse) *taskv1.Schedule {
	if s == nil {
		return nil
	}
	return &taskv1.Schedule{
		Id:       s.ID,
		TenantId: s.TenantID,
		Name:     s.Name,
		CronExpr: s.CronExpr,
		TaskType: s.TaskType,
		Command:  s.Command,
		Content:  s.Content,
		Path:     s.Path,
		AgentId:  s.AgentID,
		Enabled:  s.Enabled,
	}
}

// ============ 工具 ============

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeAuthError 将认证错误映射到 HTTP 状态码。
func writeAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errInvalidToken):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, errTenantMismatch):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, errAuthRequired):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, errTenantRequired):
		writeError(w, http.StatusForbidden, err.Error())
	default:
		writeError(w, http.StatusUnauthorized, err.Error())
	}
}

// writeServiceError 将 service 层错误映射到 HTTP 状态码（对齐 gRPC server.go 的映射）。
func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrTaskInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrTaskNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrScheduleNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrResultNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrBatchNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrClaimEpochMismatch):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// parseInt32 解析查询参数为 int32，空/非法返回 0。
func parseInt32(s string) int32 {
	var n int32
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int32(c-'0')
	}
	return n
}
