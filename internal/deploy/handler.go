package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

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
	store       DeployStore
	disp        Dispatcher
	autoAdvance *AutoAdvanceManager    // 灰度自动推进管理器（可选，nil=未启用）
	fed         *FederationCoordinator // 多集群联邦发布协调器（task 280，默认内存后端开箱即用）
}

// NewHandler 构造部署处理器。
//
// task 280：默认启用联邦发布协调器（内存后端 + 自身作为 DeployExecutor），开箱即用。
// controlplane 可后续调用 SetFederationStore 替换为 SQL 后端（持久化）。
func NewHandler(store DeployStore, disp Dispatcher) *Handler {
	h := &Handler{store: store, disp: disp}
	h.fed = NewFederationCoordinator(NewMemoryFederationStore(), h)
	return h
}

// SetFederationStore 替换联邦发布存储后端（controlplane 在选择 SQL 后端时调用）。
// 协调器的 DeployExecutor 仍为 h 自身，仅替换存储。
func (h *Handler) SetFederationStore(s FederationStore) {
	if s == nil {
		return
	}
	h.fed = NewFederationCoordinator(s, h)
}

// Federation 暴露联邦发布协调器（供 controlplane 后台 ReconcileAll 调用）。
func (h *Handler) Federation() *FederationCoordinator { return h.fed }

// SetAutoAdvance 注入灰度自动推进管理器（可选，启用后 /auto-advance API 可用）。
func (h *Handler) SetAutoAdvance(m *AutoAdvanceManager) {
	h.autoAdvance = m
}

// Store 暴露底层存储（供 controlplane 后台 reconcile 调用）。
func (h *Handler) Store() DeployStore { return h.store }

// RegisterRoutes 注入 M3 部署路由到 mux（对齐 系统设计 3.2.M3 接口清单）。
//
// task 280：同时注册多集群联邦发布路由（/api/v1/deploys/federation*），复用同一 mux。
// 联邦路由以 /api/v1/deploys/federation 前缀注册，ServeMux 最长前缀匹配优先于
// /api/v1/deploys/，故 controlplane 无需改动即可获得联邦 API。
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/deploys", h.handleDeploys)
	mux.HandleFunc("/api/v1/deploys/", h.handleDeployByID)
	mux.HandleFunc("/api/v1/deploys/federation", h.handleFederationDeploys)
	mux.HandleFunc("/api/v1/deploys/federation/", h.handleFederationDeployByID)
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

	// 子操作：execute / rollback / promote（灰度晋级）。
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
		case "promote":
			// 灰度晋级：canary/gated -> 全量派发剩余目标。
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if err := h.Promote(r.Context(), id, actx.TenantID); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			dt, _ := h.store.Get(r.Context(), id, actx.TenantID)
			writeJSON(w, http.StatusOK, dt)
			return
		case "auto-advance", "auto-advance/status":
			// 灰度自动推进：POST 启动 / GET status 查询 / DELETE 停止。
			if h.autoAdvance == nil {
				writeJSON(w, http.StatusNotImplemented, map[string]string{
					"error": "auto-advance not enabled on this handler",
				})
				return
			}
			h.autoAdvance.ServeHTTP(w, r, id, actx.TenantID, parts[1])
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

// Execute 执行部署：按策略派发底层任务到目标 agent。
//
// 策略分流：
//   - rolling：全量目标一次性派发，状态 created -> running（向后兼容）。
//   - canary：按 CanaryWeight 比例选取部分目标派发，状态 created -> canary，
//     记录 CanaryTargets；剩余目标在 Promote 阶段派发。
//   - bluegreen：将 TargetIDs 视为新（inactive）一组，全量派发并记入 CanaryTargets，
//     旧版本目标记入 StableTargets（由调用方在 TargetIDs 之外显式提供，MVP 不自动推断），
//     状态 created -> canary；Promote 阶段切换流量（标记 success），Rollback 下线新组。
//
// 无可用目标则 failed。
func (h *Handler) Execute(ctx context.Context, id int64, tenantID string) error {
	dt, err := h.store.Get(ctx, id, tenantID)
	if err != nil {
		return err
	}
	if dt.Status != StatusCreated {
		return fmt.Errorf("deploy %d not executable in status %s", id, dt.Status)
	}
	allTargets := SplitIDs(dt.TargetIDs)
	strategy := dt.EffectiveStrategy()

	var targets []string
	switch strategy {
	case StrategyCanary:
		// 金丝雀：按比例选取部分目标。
		targets = selectCanaryTargets(allTargets, dt.EffectiveCanaryWeight())
		if len(targets) == 0 {
			// 比例过低导致无目标：至少取 1 个，保证灰度阶段有流量。
			targets = allTargets[:1]
		}
		dt.CanaryTargets = strings.Join(targets, ",")
	case StrategyBlueGreen:
		// 蓝绿：新组全量派发（TargetIDs 即新版本目标），旧组由 StableTargets 标记。
		targets = allTargets
		dt.CanaryTargets = strings.Join(targets, ",")
	default:
		// rolling：全量派发。
		targets = allTargets
	}

	taskIDs := make([]string, 0, len(targets))
	for _, tid := range targets {
		dev := h.disp.Device(tid)
		if dev == nil || dev.AgentID == "" {
			continue // 目标无 agent（未纳管），跳过
		}
		t := &proto.Task{
			AgentID:  dev.AgentID,
			TenantID: dt.TenantID,
			Type:     deployTypeToTaskType(dt.Type),
			Command:  dt.RepoURL,
			Content:  dt.Content,
			Path:     dt.Path,
			Status:   "pending",
		}
		created := h.disp.CreateTask(t)
		if created != nil && created.TaskID != "" {
			taskIDs = append(taskIDs, created.TaskID)
		}
	}
	if len(taskIDs) == 0 {
		dt.Status = StatusFailed
	} else {
		dt.TaskIDs = strings.Join(taskIDs, ",")
		switch strategy {
		case StrategyCanary, StrategyBlueGreen:
			dt.Status = StatusCanary // 灰度阶段，等待门禁评估
		default:
			dt.Status = StatusRunning
		}
	}
	return h.store.Update(ctx, dt)
}

// selectCanaryTargets 按 weight 比例从 all 中选取前 k 个目标作为金丝雀流量。
// weight=0 返回空（调用方应回退至少 1 个）；weight>=100 返回全部。
// 选取策略：按列表顺序取前 k 个（确定性，便于 reconcile 与测试复现）。
func selectCanaryTargets(all []string, weight int) []string {
	if len(all) == 0 || weight <= 0 {
		return nil
	}
	if weight >= canaryWeightMax {
		return all
	}
	k := len(all) * weight / canaryWeightMax
	if k < 1 {
		k = 1
	}
	if k > len(all) {
		k = len(all)
	}
	return all[:k]
}

// Promote 灰度晋级：canary/gated -> promoting，派发剩余目标，全量完成后 -> success。
//
// 对 canary：剩余目标 = TargetIDs - CanaryTargets，派发后状态 -> promoting。
// 对 bluegreen：新组已全量派发，promote 直接切流量（标记 success），旧组下线由外部同步。
// 对 rolling/无灰度：返回错误（不可晋级）。
func (h *Handler) Promote(ctx context.Context, id int64, tenantID string) error {
	dt, err := h.store.Get(ctx, id, tenantID)
	if err != nil {
		return err
	}
	if dt.Status != StatusCanary && dt.Status != StatusGated {
		return fmt.Errorf("deploy %d cannot promote from status %s", id, dt.Status)
	}
	strategy := dt.EffectiveStrategy()
	if strategy == StrategyRolling {
		return fmt.Errorf("deploy %d strategy=rolling has no canary stage to promote", id)
	}

	// bluegreen：promote 即切流量完成，直接 success。
	if strategy == StrategyBlueGreen {
		dt.Status = StatusSuccess
		return h.store.Update(ctx, dt)
	}

	// canary：派发剩余目标。
	all := SplitIDs(dt.TargetIDs)
	done := SplitIDs(dt.CanaryTargets)
	doneSet := make(map[string]bool, len(done))
	for _, d := range done {
		doneSet[d] = true
	}
	var remaining []string
	for _, t := range all {
		if !doneSet[t] {
			remaining = append(remaining, t)
		}
	}
	taskIDs := SplitIDs(dt.TaskIDs) // 已有 canary 阶段任务
	for _, tid := range remaining {
		dev := h.disp.Device(tid)
		if dev == nil || dev.AgentID == "" {
			continue
		}
		t := &proto.Task{
			AgentID:  dev.AgentID,
			TenantID: dt.TenantID,
			Type:     deployTypeToTaskType(dt.Type),
			Command:  dt.RepoURL,
			Content:  dt.Content,
			Path:     dt.Path,
			Status:   "pending",
		}
		created := h.disp.CreateTask(t)
		if created != nil && created.TaskID != "" {
			taskIDs = append(taskIDs, created.TaskID)
		}
	}
	dt.TaskIDs = strings.Join(taskIDs, ",")
	dt.CanaryTargets = dt.TargetIDs // 全量已派发
	dt.Status = StatusPromoting
	return h.store.Update(ctx, dt)
}

// Rollback 回滚：running/canary/gated/promoting/success -> rolledback。
//
// 灰度阶段（canary/gated/promoting）回滚即下线新版本、恢复稳定版本；
// 全量阶段（running/success）回滚为状态回退（MVP，真实 Argo CD 回滚由外部同步）。
func (h *Handler) Rollback(ctx context.Context, id int64, tenantID string) error {
	dt, err := h.store.Get(ctx, id, tenantID)
	if err != nil {
		return err
	}
	switch dt.Status {
	case StatusRunning, StatusCanary, StatusGated, StatusPromoting, StatusSuccess:
		// 允许回滚的状态。
	default:
		return fmt.Errorf("deploy %d cannot rollback from status %s", id, dt.Status)
	}
	dt.Status = StatusRolledBack
	return h.store.Update(ctx, dt)
}

// Reconcile 对单条进行中部署做状态对账：
//
//   - running/promoting（全量阶段）：底层任务全 done -> success；任一 failed -> failed。
//   - canary（灰度阶段）：底层任务终态评估发布门禁——
//     门禁通过 -> gated（可调 Promote 晋级）；
//     门禁不通过 -> failed，若 AutoRollback=true 则自动回滚（状态 -> rolledback）。
//
// 非进行中状态（created/success/failed/rolledback/gated）不对账。
func (h *Handler) Reconcile(ctx context.Context, id int64, tenantID string) error {
	dt, err := h.store.Get(ctx, id, tenantID)
	if err != nil {
		return err
	}
	switch dt.Status {
	case StatusRunning, StatusPromoting, StatusCanary:
		// 进行中状态，继续对账。
	default:
		return nil
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
	total := len(states)

	// 灰度阶段：评估发布门禁。
	if dt.Status == StatusCanary {
		// 仍有任务未终态：等待。
		if done+failed < total {
			return nil
		}
		gate := dt.ResolvedGate()
		if evaluateGate(gate, done, failed, total) {
			dt.Status = StatusGated
			return h.store.Update(ctx, dt)
		}
		// 门禁不通过。
		dt.Status = StatusFailed
		if err := h.store.Update(ctx, dt); err != nil {
			return err
		}
		if dt.AutoRollback {
			return h.autoRollback(ctx, dt)
		}
		return nil
	}

	// 全量阶段（running/promoting）。
	if failed > 0 {
		dt.Status = StatusFailed
		if err := h.store.Update(ctx, dt); err != nil {
			return err
		}
		// 全量阶段失败也支持自动回滚（如蓝绿切流量后健康检查失败）。
		if dt.AutoRollback {
			return h.autoRollback(ctx, dt)
		}
		return nil
	}
	if done == total {
		dt.Status = StatusSuccess
		return h.store.Update(ctx, dt)
	}
	return nil // 仍在进行中
}

// evaluateGate 评估发布门禁是否通过。
//
// 判定规则（任一不满足即不通过）：
//   - SuccessRate > 0：done/total * 100 >= SuccessRate
//   - MaxFailRate > 0：failed/total * 100 <= MaxFailRate
//   - MinSuccessCount > 0：done >= MinSuccessCount
//   - HealthCheckURL：由调用方在外部评估（此处仅按任务终态判定），URL 留待扩展。
//
// gate 全零值时（已由 ResolvedGate 回退默认）要求 100% 成功。
// 仅设 MaxFailRate 未设 SuccessRate 时，默认要求 SuccessRate>=1（至少 1% 成功率），
// 避免 MaxFailRate=100 时 0% 成功率也被放行的边界漏洞。
func evaluateGate(gate GateConfig, done, failed, total int) bool {
	if total == 0 {
		return false
	}
	// 仅设 MaxFailRate 未设 SuccessRate 时，补默认 SuccessRate=1，避免 0% 成功率被放行。
	if gate.SuccessRate == 0 && gate.MaxFailRate > 0 {
		gate.SuccessRate = 1
	}
	successRate := float64(done) / float64(total) * 100.0
	failRate := float64(failed) / float64(total) * 100.0
	if gate.SuccessRate > 0 && successRate < gate.SuccessRate {
		return false
	}
	if gate.MaxFailRate > 0 && failRate > gate.MaxFailRate {
		return false
	}
	if gate.MinSuccessCount > 0 && done < gate.MinSuccessCount {
		return false
	}
	// 默认门禁（SuccessRate=100, MaxFailRate=0）：任一失败即不通过。
	if gate.SuccessRate == 0 && gate.MaxFailRate == 0 && gate.MinSuccessCount == 0 {
		if failed > 0 {
			return false
		}
	}
	return true
}

// autoRollback 自动回滚：将 failed 部署转为 rolledback，记录回滚时间。
//
// MVP：仅置状态（与手动 Rollback 同语义），真实回滚动作（如 Argo CD rollback、
// K8s rollout undo）由外部控制器监听 status=rolledback 事件同步执行。
func (h *Handler) autoRollback(ctx context.Context, dt *DeployTask) error {
	dt.Status = StatusRolledBack
	dt.UpdatedAt = time.Now()
	return h.store.Update(ctx, dt)
}

// AdvanceCanary 扩大金丝雀灰度比例：更新 CanaryWeight 并派发新增目标。
//
// 由 AutoAdvanceManager 通过 AdvanceFunc 回调注入，实现"达标自动扩大灰度"。
// newWeight 为目标灰度比例（[1,100]），需大于当前 CanaryWeight 才有效。
// 仅 canary/gated 状态可推进；派发新增目标后状态保持 canary（仍在灰度阶段）。
func (h *Handler) AdvanceCanary(ctx context.Context, deployID int64, tenantID string, newWeight int) error {
	dt, err := h.store.Get(ctx, deployID, tenantID)
	if err != nil {
		return err
	}
	if dt.EffectiveStrategy() != StrategyCanary {
		return fmt.Errorf("deploy %d strategy %s not canary, cannot advance", deployID, dt.EffectiveStrategy())
	}
	if dt.Status != StatusCanary && dt.Status != StatusGated {
		return fmt.Errorf("deploy %d cannot advance from status %s", deployID, dt.Status)
	}
	if newWeight <= 0 || newWeight > canaryWeightMax {
		return fmt.Errorf("new_weight %d out of range [1,100]", newWeight)
	}
	if newWeight <= dt.EffectiveCanaryWeight() {
		return fmt.Errorf("new_weight %d <= current %d, no advance", newWeight, dt.EffectiveCanaryWeight())
	}

	all := SplitIDs(dt.TargetIDs)
	done := SplitIDs(dt.CanaryTargets)
	doneSet := make(map[string]bool, len(done))
	for _, d := range done {
		doneSet[d] = true
	}
	// 按新比例选取目标（确定性，与 Execute 一致）。
	newTargets := selectCanaryTargets(all, newWeight)
	if len(newTargets) == 0 {
		return fmt.Errorf("no targets selected for new_weight %d", newWeight)
	}
	// 找出新增目标（不在已派发集合中的）。
	var added []string
	for _, t := range newTargets {
		if !doneSet[t] {
			added = append(added, t)
		}
	}
	// 派发新增目标。
	taskIDs := SplitIDs(dt.TaskIDs)
	for _, tid := range added {
		dev := h.disp.Device(tid)
		if dev == nil || dev.AgentID == "" {
			continue
		}
		t := &proto.Task{
			AgentID:  dev.AgentID,
			TenantID: dt.TenantID,
			Type:     deployTypeToTaskType(dt.Type),
			Command:  dt.RepoURL,
			Content:  dt.Content,
			Path:     dt.Path,
			Status:   "pending",
		}
		created := h.disp.CreateTask(t)
		if created != nil && created.TaskID != "" {
			taskIDs = append(taskIDs, created.TaskID)
		}
	}
	dt.TaskIDs = strings.Join(taskIDs, ",")
	dt.CanaryWeight = newWeight
	dt.CanaryTargets = strings.Join(newTargets, ",")
	dt.Status = StatusCanary // 仍在灰度阶段，等待下一轮评估
	return h.store.Update(ctx, dt)
}

// ReconcileAll 对账所有进行中部署（controlplane 后台周期调用）。
//
// 覆盖状态：running（全量）、canary（灰度阶段，评估门禁）、promoting（灰度晋级中）。
// 多状态分别 List 后合并对账，避免单次 List 跨状态过滤遗漏。
func (h *Handler) ReconcileAll(ctx context.Context, tenantID string) int {
	n := 0
	for _, status := range []string{StatusRunning, StatusCanary, StatusPromoting} {
		list, err := h.store.List(ctx, tenantID, status)
		if err != nil {
			continue
		}
		for i := range list {
			if h.Reconcile(ctx, list[i].ID, tenantID) == nil {
				n++
			}
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

// =============================================================================
// 多集群联邦发布 HTTP API（task 280）
// =============================================================================
//
// 路由（由 RegisterRoutes 注册，复用 deployMux，controlplane 无需改动）：
//   - POST   /api/v1/deploys/federation          创建联邦发布计划
//   - GET    /api/v1/deploys/federation          列出联邦发布计划（?status= 过滤）
//   - GET    /api/v1/deploys/federation/{id}     查询联邦发布详情
//   - POST   /api/v1/deploys/federation/{id}/execute  启动联邦发布（派发首批成员）
//   - POST   /api/v1/deploys/federation/{id}/promote  推进到下一批成员
//   - POST   /api/v1/deploys/federation/{id}/rollback 回滚全部已派发成员
//   - GET    /api/v1/deploys/federation/{id}/status   联邦级状态聚合视图

// handleFederationDeploys POST 创建 / GET 列表。
func (h *Handler) handleFederationDeploys(w http.ResponseWriter, r *http.Request) {
	if h.fed == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "federation not enabled"})
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	switch r.Method {
	case http.MethodPost:
		var f FederationDeploy
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 请求体限 1MiB
		if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid JSON: %v", err)})
			return
		}
		f.TenantID = actx.TenantID
		if f.TenantID == "" {
			f.TenantID = "default"
		}
		f.CreatedBy = actx.UserID
		if f.CreatedBy == "" {
			f.CreatedBy = "local"
		}
		created, err := h.fed.Store().Create(r.Context(), &f)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, created)
	case http.MethodGet:
		status := r.URL.Query().Get("status")
		list, err := h.fed.Store().List(r.Context(), actx.TenantID, status)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if list == nil {
			list = []FederationDeploy{}
		}
		writeJSON(w, http.StatusOK, list)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleFederationDeployByID GET 详情 / {id}/execute|promote|rollback|status。
func (h *Handler) handleFederationDeployByID(w http.ResponseWriter, r *http.Request) {
	if h.fed == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "federation not enabled"})
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	// 路径：/api/v1/deploys/federation/{id} 或 /api/v1/deploys/federation/{id}/{action}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/deploys/federation/")
	parts := strings.SplitN(rest, "/", 2)
	idStr := parts[0]
	if idStr == "" || strings.Contains(idStr, "/") {
		http.Error(w, "invalid federation id", http.StatusBadRequest)
		return
	}
	var id int64
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
		http.Error(w, "invalid federation id", http.StatusBadRequest)
		return
	}

	// 子操作。
	if len(parts) == 2 {
		switch parts[1] {
		case "execute":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if err := h.fed.Start(r.Context(), id, actx.TenantID); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			f, _ := h.fed.Store().Get(r.Context(), id, actx.TenantID)
			writeJSON(w, http.StatusOK, f)
			return
		case "promote":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if err := h.fed.Promote(r.Context(), id, actx.TenantID); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			f, _ := h.fed.Store().Get(r.Context(), id, actx.TenantID)
			writeJSON(w, http.StatusOK, f)
			return
		case "rollback":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if err := h.fed.Rollback(r.Context(), id, actx.TenantID); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			f, _ := h.fed.Store().Get(r.Context(), id, actx.TenantID)
			writeJSON(w, http.StatusOK, f)
			return
		case "status":
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			st, err := h.fed.Status(r.Context(), id, actx.TenantID)
			if err != nil {
				if err == ErrFedNotFound {
					writeJSON(w, http.StatusNotFound, map[string]string{"error": "federation not found"})
					return
				}
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, st)
			return
		default:
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
	}

	// 默认：GET 详情。
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	f, err := h.fed.Store().Get(r.Context(), id, actx.TenantID)
	if err != nil {
		if err == ErrFedNotFound {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "federation not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, f)
}

// =============================================================================
// DeployExecutor 实现（task 280）：Handler 自身作为联邦协调器的成员子部署派发器
// =============================================================================

// CreateAndExecute 创建子部署（克隆 template + targetIDs）并立即执行，返回子部署 ID。
// 联邦协调器为每个 FederationMember 调用此方法派发子 DeployTask。
func (h *Handler) CreateAndExecute(ctx context.Context, template *DeployTask, targetIDs, tenantID string) (int64, error) {
	if template == nil {
		return 0, errInvalid("nil template")
	}
	dt := *template // 浅拷贝足够（字段均为值类型或可共享指针，子部署独立）
	dt.ID = 0
	dt.TargetIDs = targetIDs
	dt.TenantID = tenantID
	dt.Status = "" // 让 store.Create 置为 created
	dt.TaskIDs = ""
	dt.CanaryTargets = ""
	dt.StableTargets = ""
	created, err := h.store.Create(ctx, &dt)
	if err != nil {
		return 0, err
	}
	if err := h.Execute(ctx, created.ID, tenantID); err != nil {
		return 0, err
	}
	return created.ID, nil
}

// MemberStatus 查询子部署状态（返回 Status* 常量）。
func (h *Handler) MemberStatus(ctx context.Context, deployID int64, tenantID string) (string, error) {
	dt, err := h.store.Get(ctx, deployID, tenantID)
	if err != nil {
		return "", err
	}
	return dt.Status, nil
}

// PromoteMember 晋级成员子部署（canary/gated -> promoting/success）。
func (h *Handler) PromoteMember(ctx context.Context, deployID int64, tenantID string) error {
	return h.Promote(ctx, deployID, tenantID)
}

// RollbackMember 回滚成员子部署。
func (h *Handler) RollbackMember(ctx context.Context, deployID int64, tenantID string) error {
	return h.Rollback(ctx, deployID, tenantID)
}

// 编译期断言：Handler 实现 DeployExecutor 接口。
var _ DeployExecutor = (*Handler)(nil)
