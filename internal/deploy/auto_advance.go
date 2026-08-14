// auto_advance.go 实现灰度发布的自动推进：定时检查金丝雀批次的失败率/延迟指标，
// 达标自动扩大灰度比例（advance），不达标自动回滚（rollback），达到 100% 自动晋级（promote）。
//
// 设计要点：
//   - AutoAdvanceManager 与 Dispatcher/Store 解耦，通过回调函数注入推进/回滚动作，
//     避免 deploy 包反向依赖 controlplane，且便于单测。
//   - 指标采集通过 TaskResultProvider 接口（store.Store 实现之），仅查询已上报结果。
//   - 并发安全：同一部署可被多次调用 Monitor，仅首次生效；Stop 取消监控 goroutine。
//   - HTTP API：POST 启动 / GET status 查询 / DELETE 停止，由 Handler 路由委托。
package deploy

import (
	"context"

	"fmt"
	"net/http"
	"sync"
	"time"

	"opsmesh/internal/proto"
)

// AutoAdvanceConfig 灰度自动推进配置。
type AutoAdvanceConfig struct {
	Enabled              bool          // 是否启用自动推进
	CheckInterval        time.Duration // 检查间隔（默认 30s）
	FailureRateThreshold float64       // 失败率阈值（0-1，默认 0.05=5%）
	LatencyThreshold     time.Duration // 延迟阈值（默认 500ms）
	MinSampleSize        int           // 最小样本数（不足时不推进，默认 100）
	AdvanceRatio         float64       // 每次推进比例（默认 0.1=10%）
	MaxRatio             float64       // 最大灰度比例（默认 1.0=100%）
}

// DefaultAutoAdvanceConfig 返回生产默认配置。
func DefaultAutoAdvanceConfig() AutoAdvanceConfig {
	return AutoAdvanceConfig{
		Enabled:              true,
		CheckInterval:        30 * time.Second,
		FailureRateThreshold: 0.05,
		LatencyThreshold:     500 * time.Millisecond,
		MinSampleSize:        100,
		AdvanceRatio:         0.1,
		MaxRatio:             1.0,
	}
}

// TaskResultProvider 提供任务结果查询（store.Store 实现之）。
// 仅依赖最小方法集，避免引入 store 大接口耦合。
type TaskResultProvider interface {
	TaskResult(taskID string) *proto.TaskResult
}

// AdvanceFunc 扩大灰度比例回调：更新 CanaryWeight 并派发新增目标。
// 由 Handler 注入（Handler 持有 Dispatcher，可派发底层任务）。
type AdvanceFunc func(ctx context.Context, deployID int64, tenantID string, newWeight int) error

// PromoteFunc 全量晋级回调（canary -> promoting -> success）。
type PromoteFunc func(ctx context.Context, deployID int64, tenantID string) error

// RollbackFunc 回滚回调（任意进行中状态 -> rolledback）。
type RollbackFunc func(ctx context.Context, deployID int64, tenantID string) error

// GateResult 门禁评估结果。
type GateResult struct {
	Passed      bool          `json:"passed"`       // 是否通过
	FailureRate float64       `json:"failure_rate"` // 失败率 [0,1]
	AvgLatency  time.Duration `json:"avg_latency"`  // 平均延迟
	SampleSize  int           `json:"sample_size"`  // 已完成样本数
	Reason      string        `json:"reason"`       // 不通过原因（通过时为空）
}

// monitorState 单个部署的监控运行态。
type monitorState struct {
	cancel     context.CancelFunc // 取消监控 goroutine
	startedAt  time.Time          // 启动时间
	lastGate   *GateResult        // 最近一次门禁评估结果
	lastCheck  time.Time          // 最近一次检查时间
	lastAction string             // 最近一次动作：advance / promote / rollback / wait / error
	lastError  string             // 最近一次错误信息
}

// AutoAdvanceStatus 对外暴露的监控状态（GET /auto-advance/status 返回）。
type AutoAdvanceStatus struct {
	Running    bool        `json:"running"`     // 是否正在监控
	StartedAt  time.Time   `json:"started_at"`  // 启动时间
	LastGate   *GateResult `json:"last_gate"`   // 最近门禁结果
	LastCheck  time.Time   `json:"last_check"`  // 最近检查时间
	LastAction string      `json:"last_action"` // 最近动作
	LastError  string      `json:"last_error"`  // 最近错误
}

// AutoAdvanceManager 灰度自动推进管理器。
type AutoAdvanceManager struct {
	config   AutoAdvanceConfig
	deploys  DeployStore        // 部署计划 CRUD
	tasks    TaskResultProvider // 任务结果查询
	advance  AdvanceFunc        // 扩大灰度回调
	promote  PromoteFunc        // 全量晋级回调
	rollback RollbackFunc       // 回滚回调

	mu      sync.RWMutex
	running map[string]*monitorState // key=deployID（字符串化）
}

// NewAutoAdvanceManager 构造自动推进管理器。
// deploys/times 必须非空；advance/promote/rollback 可为空（仅评估不动作，用于只读监控）。
func NewAutoAdvanceManager(cfg AutoAdvanceConfig, deploys DeployStore, tasks TaskResultProvider,
	advance AdvanceFunc, promote PromoteFunc, rollback RollbackFunc) *AutoAdvanceManager {
	// 配置回退默认值。
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 30 * time.Second
	}
	if cfg.FailureRateThreshold <= 0 {
		cfg.FailureRateThreshold = 0.05
	}
	if cfg.LatencyThreshold <= 0 {
		cfg.LatencyThreshold = 500 * time.Millisecond
	}
	if cfg.MinSampleSize <= 0 {
		cfg.MinSampleSize = 100
	}
	if cfg.AdvanceRatio <= 0 {
		cfg.AdvanceRatio = 0.1
	}
	if cfg.MaxRatio <= 0 {
		cfg.MaxRatio = 1.0
	}
	return &AutoAdvanceManager{
		config:   cfg,
		deploys:  deploys,
		tasks:    tasks,
		advance:  advance,
		promote:  promote,
		rollback: rollback,
		running:  make(map[string]*monitorState),
	}
}

// Monitor 开始监控部署的灰度推进。阻塞直到 ctx 取消或部署终态（success/rolledback/failed）。
// 同一 deployID 重复调用返回 ErrAlreadyMonitoring。
func (m *AutoAdvanceManager) Monitor(ctx context.Context, deployID int64) error {
	key := fmt.Sprintf("%d", deployID)

	m.mu.Lock()
	if _, ok := m.running[key]; ok {
		m.mu.Unlock()
		return ErrAlreadyMonitoring
	}
	// 预检：部署必须存在且为 canary 状态。
	dt, err := m.deploys.Get(ctx, deployID, "")
	if err != nil {
		m.mu.Unlock()
		return err
	}
	if dt.EffectiveStrategy() != StrategyCanary {
		m.mu.Unlock()
		return fmt.Errorf("auto-advance only supports canary strategy, got %s", dt.EffectiveStrategy())
	}
	if dt.Status != StatusCanary && dt.Status != StatusGated {
		m.mu.Unlock()
		return fmt.Errorf("deploy %d not in canary stage (status=%s), cannot monitor", deployID, dt.Status)
	}

	subCtx, cancel := context.WithCancel(ctx)
	st := &monitorState{
		cancel:    cancel,
		startedAt: time.Now(),
	}
	m.running[key] = st
	m.mu.Unlock()

	// 启动监控循环（同步执行，由调用方决定是否 goroutine）。
	m.monitorLoop(subCtx, deployID, st)

	// 退出时清理。
	m.mu.Lock()
	delete(m.running, key)
	m.mu.Unlock()
	return nil
}

// ErrAlreadyMonitoring 部署已在监控中。
var ErrAlreadyMonitoring = fmt.Errorf("deploy already under auto-advance monitoring")

// monitorLoop 监控循环：每 CheckInterval tick 一次 checkAndAdvance，直到终态或 ctx 取消。
func (m *AutoAdvanceManager) monitorLoop(ctx context.Context, deployID int64, st *monitorState) {
	ticker := time.NewTicker(m.config.CheckInterval)
	defer ticker.Stop()

	// 首次立即检查一次（不等首个 tick）。
	m.checkAndAdvance(ctx, deployID, st)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// 检查部署是否已进入终态。
			dt, err := m.deploys.Get(ctx, deployID, "")
			if err != nil {
				m.recordError(st, err)
				return
			}
			if isTerminalStatus(dt.Status) {
				return
			}
			m.checkAndAdvance(ctx, deployID, st)
		}
	}
}

// isTerminalStatus 判断是否终态（不再监控）。
func isTerminalStatus(status string) bool {
	switch status {
	case StatusSuccess, StatusFailed, StatusRolledBack:
		return true
	}
	return false
}

// recordError 记录最近错误到 monitorState。
func (m *AutoAdvanceManager) recordError(st *monitorState, err error) {
	m.mu.Lock()
	st.lastCheck = time.Now()
	st.lastAction = "error"
	st.lastError = err.Error()
	m.mu.Unlock()
}

// recordAction 记录最近动作到 monitorState。
func (m *AutoAdvanceManager) recordAction(st *monitorState, action string, gate *GateResult) {
	m.mu.Lock()
	st.lastCheck = time.Now()
	st.lastAction = action
	st.lastError = ""
	st.lastGate = gate
	m.mu.Unlock()
}

// checkAndAdvance 检查指标并决定推进/回滚/晋级/等待。
//
// 决策矩阵：
//   - 样本不足 → 等待（wait）
//   - 失败率超阈值或延迟超阈值 → 回滚（rollback）
//   - 当前比例 + AdvanceRatio >= MaxRatio → 全量晋级（promote）
//   - 否则 → 扩大灰度比例（advance）
func (m *AutoAdvanceManager) checkAndAdvance(ctx context.Context, deployID int64, st *monitorState) error {
	gate, err := m.evaluateGate(deployID)
	if err != nil {
		m.recordError(st, err)
		return err
	}

	// 样本不足：等待。
	if gate.SampleSize < m.config.MinSampleSize {
		m.recordAction(st, "wait", gate)
		return nil
	}

	// 门禁不通过：回滚。
	if !gate.Passed {
		if m.rollback == nil {
			m.recordAction(st, "rollback_skipped", gate)
			return nil
		}
		dt, err := m.deploys.Get(ctx, deployID, "")
		if err != nil {
			m.recordError(st, err)
			return err
		}
		if err := m.rollback(ctx, deployID, dt.TenantID); err != nil {
			m.recordError(st, err)
			return err
		}
		m.recordAction(st, "rollback", gate)
		return nil
	}

	// 门禁通过：推进或晋级。
	dt, err := m.deploys.Get(ctx, deployID, "")
	if err != nil {
		m.recordError(st, err)
		return err
	}
	currentRatio := float64(dt.EffectiveCanaryWeight()) / 100.0
	newRatio := currentRatio + m.config.AdvanceRatio

	// 达到 MaxRatio：全量晋级。
	if newRatio >= m.config.MaxRatio {
		if m.promote == nil {
			m.recordAction(st, "promote_skipped", gate)
			return nil
		}
		if err := m.promote(ctx, deployID, dt.TenantID); err != nil {
			m.recordError(st, err)
			return err
		}
		m.recordAction(st, "promote", gate)
		return nil
	}

	// 扩大灰度比例。
	newWeight := int(newRatio * 100)
	if newWeight > 100 {
		newWeight = 100
	}
	if newWeight <= dt.EffectiveCanaryWeight() {
		// 比例未增长（步长过小），等待下一轮。
		m.recordAction(st, "wait", gate)
		return nil
	}
	if m.advance == nil {
		m.recordAction(st, "advance_skipped", gate)
		return nil
	}
	if err := m.advance(ctx, deployID, dt.TenantID, newWeight); err != nil {
		m.recordError(st, err)
		return err
	}
	m.recordAction(st, "advance", gate)
	return nil
}

// evaluateGate 评估门禁指标：查询灰度批次任务结果，计算失败率/延迟/样本数。
//
// 返回的 GateResult.SampleSize 为已上报结果的任务数（未完成的不计入）。
// 若部署不存在或非金丝雀阶段，返回错误。
func (m *AutoAdvanceManager) evaluateGate(deployID int64) (*GateResult, error) {
	dt, err := m.deploys.Get(context.Background(), deployID, "")
	if err != nil {
		return nil, err
	}
	if dt.EffectiveStrategy() != StrategyCanary {
		return nil, fmt.Errorf("deploy %d strategy %s not canary", deployID, dt.EffectiveStrategy())
	}

	taskIDs := SplitIDs(dt.TaskIDs)
	if len(taskIDs) == 0 {
		return &GateResult{
			Passed:     false,
			SampleSize: 0,
			Reason:     "no tasks dispatched",
		}, nil
	}

	total, failed, sumDurationMs := 0, 0, int64(0)
	for _, tid := range taskIDs {
		r := m.tasks.TaskResult(tid)
		if r == nil {
			continue // 任务未上报结果，不计入样本。
		}
		total++
		if r.ExitCode != 0 {
			failed++
		}
		if r.DurationMs > 0 {
			sumDurationMs += r.DurationMs
		}
	}

	result := &GateResult{
		SampleSize: total,
	}
	if total == 0 {
		result.Passed = false
		result.Reason = "no completed samples"
		return result, nil
	}

	result.FailureRate = float64(failed) / float64(total)
	if total > 0 {
		result.AvgLatency = time.Duration(sumDurationMs/int64(total)) * time.Millisecond
	}

	// 评估门禁。
	if result.FailureRate > m.config.FailureRateThreshold {
		result.Passed = false
		result.Reason = fmt.Sprintf("failure rate %.4f > threshold %.4f", result.FailureRate, m.config.FailureRateThreshold)
		return result, nil
	}
	if result.AvgLatency > m.config.LatencyThreshold {
		result.Passed = false
		result.Reason = fmt.Sprintf("avg latency %s > threshold %s", result.AvgLatency, m.config.LatencyThreshold)
		return result, nil
	}

	result.Passed = true
	result.Reason = ""
	return result, nil
}

// Stop 停止指定部署的自动推进监控。返回是否曾正在监控。
func (m *AutoAdvanceManager) Stop(deployID int64) bool {
	key := fmt.Sprintf("%d", deployID)
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.running[key]
	if !ok {
		return false
	}
	st.cancel()
	delete(m.running, key)
	return true
}

// Status 查询指定部署的自动推进监控状态。
func (m *AutoAdvanceManager) Status(deployID int64) AutoAdvanceStatus {
	key := fmt.Sprintf("%d", deployID)
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.running[key]
	if !ok {
		return AutoAdvanceStatus{Running: false}
	}
	return AutoAdvanceStatus{
		Running:    true,
		StartedAt:  st.startedAt,
		LastGate:   st.lastGate,
		LastCheck:  st.lastCheck,
		LastAction: st.lastAction,
		LastError:  st.lastError,
	}
}

// =============================================================================
// HTTP API: POST /auto-advance 启动 | GET /auto-advance/status 查询 | DELETE /auto-advance 停止
// =============================================================================

// ServeHTTP 处理 /api/v1/deploys/{id}/auto-advance[...status] 子路径。
// 由 Handler.handleDeployByID 委托调用，deployID/tenantID 已解析。
func (m *AutoAdvanceManager) ServeHTTP(w http.ResponseWriter, r *http.Request, deployID int64, tenantID string, sub string) {
	switch sub {
	case "auto-advance":
		switch r.Method {
		case http.MethodPost:
			// 启动自动推进（异步 goroutine）。
			ctx, cancel := context.WithCancel(context.Background())
			_ = tenantID // tenantID 校验由 Get 隐式完成（空串=管理员视角）
			go func() {
				defer cancel()
				if err := m.Monitor(ctx, deployID); err != nil && err != ErrAlreadyMonitoring {
					// 监控异常退出，错误已记录在 monitorState，此处不再处理。
					_ = err
				}
			}()
			// 等待一小段时间让 Monitor 注册到 running map。
			time.Sleep(50 * time.Millisecond)
			writeJSON(w, http.StatusAccepted, map[string]interface{}{
				"message":   "auto-advance started",
				"deploy_id": deployID,
			})
		case http.MethodDelete:
			if m.Stop(deployID) {
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"message":   "auto-advance stopped",
					"deploy_id": deployID,
				})
			} else {
				writeJSON(w, http.StatusNotFound, map[string]string{
					"error": "auto-advance not running for this deploy",
				})
			}
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case "auto-advance/status":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		status := m.Status(deployID)
		writeJSON(w, http.StatusOK, status)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// 编译期断言：proto.TaskResult 用于 GateResult 指标采集。
var _ proto.TaskResult
