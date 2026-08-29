package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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
	// Authorize 鉴权回调（由 controlplane 注入）：校验租户上下文 + 所需权限。
	// perm 为当前请求所需权限（如 "workflow:read"）；未认证/无权限时须已写入响应并返回 ok=false。
	// nil 时不启用鉴权（向后兼容独立使用/测试场景）。
	Authorize func(w http.ResponseWriter, r *http.Request, perm string) (authctx.Context, bool)
}

// NewHandler 构造编排处理器。
func NewHandler(st WorkflowStore, eng TaskEngine) *Handler {
	return &Handler{store: st, eng: eng}
}

// authorize 把 Authorize 鉴权回调包装到单个路由 handler 上（G1 鉴权修复）。
// 按请求方法映射所需权限：GET/HEAD/OPTIONS 只读 → readPerm，其余（POST/PUT/PATCH/DELETE）→ writePerm。
// 鉴权通过后若回调解析出的租户在请求头缺失，回填 X-Tenant-ID，使 handler 行级隔离与鉴权一致。
func (h *Handler) authorize(fn http.HandlerFunc, readPerm, writePerm string) http.HandlerFunc {
	if h.Authorize == nil {
		return fn
	}
	return func(w http.ResponseWriter, r *http.Request) {
		perm := readPerm
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			perm = writePerm
		}
		actx, ok := h.Authorize(w, r, perm)
		if !ok {
			return
		}
		if actx.TenantID != "" && strings.TrimSpace(r.Header.Get("X-Tenant-ID")) == "" {
			r.Header.Set("X-Tenant-ID", actx.TenantID)
		}
		fn(w, r)
	}
}

// RegisterRoutes 注入 M5 编排路由到 mux。
// G1 鉴权修复：读操作（GET 列表/详情/状态/历史）要求 workflow:read，
// 写操作（POST 创建/运行/调度、PUT/PATCH 更新）要求 workflow:write；未配置 Authorize 时透传。
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/workflows", h.authorize(h.handleWorkflows, "workflow:read", "workflow:write"))
	mux.HandleFunc("/api/v1/workflows/", h.authorize(h.handleWorkflowByID, "workflow:read", "workflow:write"))
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
	case len(parts) >= 2 && parts[1] == "runs":
		h.listRuns(w, r, id, actx.TenantID)
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
		switch err {
		case ErrWFNotFound:
			status = http.StatusNotFound
		case ErrWFTenantMismatch:
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
		switch err {
		case ErrWFNotFound:
			status = http.StatusNotFound
		case ErrWFTenantMismatch:
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
	if _, err := h.store.Get(r.Context(), id, tenantID); err != nil {
		status := http.StatusInternalServerError
		switch err {
		case ErrWFNotFound:
			status = http.StatusNotFound
		case ErrWFTenantMismatch:
			status = http.StatusForbidden
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	if _, err := h.Trigger(r.Context(), id, tenantID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	wf, gerr := h.store.Get(r.Context(), id, tenantID)
	if gerr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": gerr.Error()})
		return
	}
	writeJSON(w, http.StatusOK, wf)
}

// maxExpandDepth 是子工作流递归展开的最大深度（防止循环引用导致栈溢出）。
const maxExpandDepth = 10

// Trigger 展开工作流 DAG 为底层任务（不重复校验，调用方负责权限/存在性）。
// 支持 shell/file/service（直接展开）/ workflow（递归展开子工作流）/ condition（求值条件分支）。
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

	// 收集已存在节点任务状态（用于 condition 求值），key 为节点 ID（去掉 prefix）。
	// 首次 Trigger 时无历史任务，nodeStatuses 为空，condition 表达式引用未完成节点时按 false 处理。
	nodeStatuses := make(map[string]string)
	for _, t := range h.eng.TasksByParent(parent) {
		if strings.HasPrefix(t.TaskID, prefix) {
			nodeStatuses[strings.TrimPrefix(t.TaskID, prefix)] = t.Status
		}
	}

	if err := h.expandNodes(ctx, wf, nodes, prefix, parent, 0, nil, nodeStatuses); err != nil {
		return nil, err
	}

	now := time.Now()
	wf.Status = StatusRunning
	wf.LastRunAt = now
	wf.LastRunStatus = StatusRunning
	if err := h.store.Update(ctx, wf); err != nil {
		return nil, err
	}

	// 创建一条 WorkflowRun 记录本次运行（执行历史与回放）。
	// NodeStates 从已创建任务中收集：TaskID 去掉 prefix 得到 nodeID，value 为节点任务状态。
	nodeStates := make(map[string]string)
	for _, t := range h.eng.TasksByParent(parent) {
		if strings.HasPrefix(t.TaskID, prefix) {
			nodeStates[strings.TrimPrefix(t.TaskID, prefix)] = t.Status
		}
	}
	run := &WorkflowRun{
		WorkflowID: wf.ID,
		TenantID:   wf.TenantID,
		StartedAt:  now,
		Status:     StatusRunning,
		NodeStates: nodeStates,
	}
	_ = h.store.CreateRun(ctx, run) // 历史记录写入失败不影响主流程
	return wf, nil
}

// expandNodes 递归展开工作流节点为底层任务。
//   - shell/file/service：直接创建底层任务（确定性 TaskID = prefix+nodeID，DependsOn 映射到 prefix+dep）；
//   - workflow：通过 store.Get 获取子工作流定义，递归展开（depth+1），子节点 ID 前缀 prefix+nodeID+"-sub-"；
//   - condition：求值 Condition 表达式，决定执行 ThenNodes 或 ElseNodes 分支，未选中分支的节点跳过不创建；
//   - condition 节点本身不创建底层任务（纯逻辑节点）。
//
// inheritedDeps 为从父节点继承的依赖（已带 prefix），用于子工作流入口节点 / condition 分支节点
// 在自身无 DependsOn 时继承父节点的依赖，保证执行顺序。
func (h *Handler) expandNodes(ctx context.Context, wf *WorkflowDef, nodes []WorkflowNode, prefix, parent string, depth int, inheritedDeps []string, nodeStatuses map[string]string) error {
	if depth >= maxExpandDepth {
		return fmt.Errorf("workflow %s: 子工作流递归深度超过上限 %d", wf.Name, maxExpandDepth)
	}

	// 第一遍：求值所有 condition 节点，收集未选中分支的节点 ID（这些节点在主循环中跳过）。
	condSkip := make(map[string]bool)
	for i := range nodes {
		n := nodes[i]
		if n.Type != NodeCondition {
			continue
		}
		result := evalCondition(n.Condition, nodeStatuses)
		var skipped []string
		if result {
			skipped = n.ElseNodes
		} else {
			skipped = n.ThenNodes
		}
		for _, bid := range skipped {
			condSkip[bid] = true
		}
	}

	// 第二遍：按节点类型展开。
	for i := range nodes {
		n := nodes[i]
		switch n.Type {
		case NodeShell, NodeFile, NodeService:
			if condSkip[n.ID] {
				continue // 被 condition 未选中分支引用的节点不创建
			}
			deps := make([]string, 0, len(n.DependsOn))
			for _, d := range n.DependsOn {
				deps = append(deps, prefix+d)
			}
			// 无自身依赖时继承父节点依赖（子工作流入口 / condition 分支节点）。
			if len(deps) == 0 && len(inheritedDeps) > 0 {
				deps = inheritedDeps
			}
			h.eng.CreateTask(&proto.Task{
				TaskID:     prefix + n.ID,
				AgentID:    wf.AgentID,
				TenantID:   wf.TenantID,
				Type:       n.Type,
				Command:    n.Command,
				Path:       n.Path,
				DependsOn:  deps,
				ParentID:   parent,
				Timeout:    n.Timeout,    // 节点级超时（秒，0=不超时，agent 端覆盖全局 taskTimeout）
				MaxRetries: n.RetryCount, // 节点级重试上限（失败后由 store SubmitResult 重试）
				RetryDelay: n.RetryDelay, // 重试间隔（秒，0=立即重试）
			})
		case NodeWorkflow:
			if condSkip[n.ID] {
				continue
			}
			// 获取子工作流定义（租户隔离：子工作流必须属于同一租户）。
			sub, err := h.store.Get(ctx, n.SubWorkflowID, wf.TenantID)
			if err != nil {
				return fmt.Errorf("workflow node %s: 子工作流 %d 获取失败: %w", n.ID, n.SubWorkflowID, err)
			}
			subNodes, err := sub.Nodes()
			if err != nil {
				return fmt.Errorf("workflow node %s: 子工作流 %d DAG 解析失败: %w", n.ID, n.SubWorkflowID, err)
			}
			// 子工作流节点 ID 前缀：prefix+nodeID+"-sub-"，避免与父工作流节点 ID 冲突。
			subPrefix := prefix + n.ID + "-sub-"
			// 父节点 n 的依赖映射为已带 prefix 的 ID，作为子工作流入口节点的继承依赖。
			parentDeps := make([]string, 0, len(n.DependsOn))
			for _, d := range n.DependsOn {
				parentDeps = append(parentDeps, prefix+d)
			}
			if len(parentDeps) == 0 && len(inheritedDeps) > 0 {
				parentDeps = inheritedDeps
			}
			if err := h.expandNodes(ctx, wf, subNodes, subPrefix, parent, depth+1, parentDeps, nodeStatuses); err != nil {
				return err
			}
		case NodeCondition:
			// condition 节点本身不创建底层任务（纯逻辑节点）。
			// 分支节点的创建由主循环处理（通过 condSkip 控制是否跳过）。
		}
	}
	return nil
}

// evalCondition 求值条件表达式，返回 bool。
// 语法：
//   - ${nodeID.status} == "success" — 检查节点状态
//   - ${nodeID.exitCode} == 0 — 检查退出码（从 status 推断：done=0, failed=非0）
//   - 支持 &&（与）和 ||（或）组合（|| 优先级低于 &&，不支持括号嵌套）
//   - 支持 == 和 != 比较操作
//
// nodeStatuses 为节点 ID -> 状态的映射（status: pending/running/done/failed/cancelled）。
// 引用不存在的节点或属性时按空串处理，与字面量比较通常得 false。
func evalCondition(expr string, nodeStatuses map[string]string) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false
	}
	// || 优先级最低，先按 || 分割。
	for _, orPart := range splitLogical(expr, "||") {
		allTrue := true
		for _, andPart := range splitLogical(orPart, "&&") {
			if !evalComparison(andPart, nodeStatuses) {
				allTrue = false
				break
			}
		}
		if allTrue {
			return true
		}
	}
	return false
}

// splitLogical 按 op 分割表达式，忽略双引号内的 op（避免字符串字面量中的 || / && 干扰分割）。
func splitLogical(expr, op string) []string {
	var parts []string
	inQuote := false
	last := 0
	for i := 0; i+len(op) <= len(expr); i++ {
		if expr[i] == '"' {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			continue
		}
		if expr[i:i+len(op)] == op {
			parts = append(parts, strings.TrimSpace(expr[last:i]))
			last = i + len(op)
			i += len(op) - 1
		}
	}
	parts = append(parts, strings.TrimSpace(expr[last:]))
	return parts
}

// evalComparison 求值单个比较表达式：left == right 或 left != right。
// 不含比较操作符时返回 false。
func evalComparison(expr string, nodeStatuses map[string]string) bool {
	expr = strings.TrimSpace(expr)
	// 先检查 !=（避免 == 子串误匹配）。
	var op string
	idx := strings.Index(expr, "!=")
	if idx >= 0 {
		op = "!="
	} else {
		idx = strings.Index(expr, "==")
		if idx >= 0 {
			op = "=="
		} else {
			return false
		}
	}
	left := strings.TrimSpace(expr[:idx])
	right := strings.TrimSpace(expr[idx+len(op):])
	leftVal := resolveValue(left, nodeStatuses)
	rightVal := resolveValue(right, nodeStatuses)
	if op == "==" {
		return leftVal == rightVal
	}
	return leftVal != rightVal
}

// resolveValue 解析值表达式：
//   - ${nodeID.status} — 节点状态（pending/running/done/failed/cancelled）
//   - ${nodeID.exitCode} — 退出码（从 status 推断：done=0, failed=1, 其他=空）
//   - "字面量" — 去引号后的字符串
//   - 裸字面量 — 原样返回（如数字 0）
func resolveValue(s string, nodeStatuses map[string]string) string {
	s = strings.TrimSpace(s)
	// ${...} 变量引用。
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") && len(s) > 3 {
		inner := s[2 : len(s)-1]
		parts := strings.SplitN(inner, ".", 2)
		if len(parts) != 2 {
			return ""
		}
		nodeID, attr := parts[0], parts[1]
		status := nodeStatuses[nodeID]
		switch attr {
		case "status":
			return status
		case "exitCode":
			// 从状态推断退出码：done=0, failed=非0（取 1），其他=空。
			switch status {
			case "done":
				return "0"
			case "failed":
				return "1"
			default:
				return ""
			}
		}
		return ""
	}
	// 带引号字面量：去引号。
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	// 裸字面量（数字等）：原样返回。
	return s
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
		switch err {
		case ErrWFNotFound:
			status = http.StatusNotFound
		case ErrWFTenantMismatch:
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
		var status int
		switch err {
		case ErrWFNotFound:
			status = http.StatusNotFound
		case ErrWFTenantMismatch:
			status = http.StatusForbidden
		default:
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
		switch err {
		case ErrWFNotFound:
			status = http.StatusNotFound
		case ErrWFTenantMismatch:
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

// listRuns GET /api/v1/workflows/{id}/runs：返回工作流执行历史列表（执行历史与回放）。
// 按租户隔离；不存在的工作流返回空列表（不报 404，与 List 语义一致）。
func (h *Handler) listRuns(w http.ResponseWriter, r *http.Request, id int64, tenantID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	runs, err := h.store.ListRuns(r.Context(), id, tenantID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if runs == nil {
		runs = []WorkflowRun{}
	}
	writeJSON(w, http.StatusOK, runs)
}

// Reconcile 依据底层节点任务状态翻工作流最近运行终态：
// 全 done → success；有 failed → failed；否则 running。回到 active 等待下次调度。
// 终态（success/failed）时同步更新最近一条 WorkflowRun 的 Status 与 FinishedAt（执行历史与回放）。
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
	now := time.Now()
	finalStatus := ""
	if anyFailed {
		wf.LastRunStatus = StatusFailed
		wf.Status = StatusActive
		finalStatus = StatusFailed
	} else if allDone {
		wf.LastRunStatus = StatusSuccess
		wf.Status = StatusActive
		finalStatus = StatusSuccess
	} else {
		wf.LastRunStatus = StatusRunning
		wf.Status = StatusRunning
	}
	wf.UpdatedAt = now
	if err := h.store.Update(ctx, wf); err != nil {
		return err
	}
	// 终态时更新最近一条 WorkflowRun 的 Status 与 FinishedAt。
	// 非终态（running）不更新历史记录，保持 Trigger 时写入的初始快照。
	if finalStatus != "" {
		// 历史记录读取失败仅跳过终态回写，不影响工作流主状态。
		runs, runsErr := h.store.ListRuns(ctx, wf.ID, tenantID)
		if runsErr != nil {
			log.Printf("[orchestration] ListRuns 失败 wf=%d: %v", wf.ID, runsErr)
		} else if len(runs) > 0 {
			latest := runs[len(runs)-1]
			latest.Status = finalStatus
			latest.FinishedAt = now
			// 重新收集节点状态快照（终态时反映最终状态）。
			prefix := "wf-" + strconv.FormatInt(wf.ID, 10) + "-"
			nodeStates := make(map[string]string)
			for _, t := range tasks {
				if strings.HasPrefix(t.TaskID, prefix) {
					nodeStates[strings.TrimPrefix(t.TaskID, prefix)] = t.Status
				}
			}
			latest.NodeStates = nodeStates
			if uerr := h.store.UpdateRun(ctx, &latest); uerr != nil {
				log.Printf("[orchestration] UpdateRun 失败 run=%d: %v", latest.ID, uerr)
			}
		}
	}
	return nil
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
