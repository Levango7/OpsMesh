// federation.go 实现多集群联邦发布协调器：跨集群灰度协调 + 联邦级发布状态聚合。
//
// 设计要点：
//   - FederationCoordinator 与具体派发实现解耦，通过 DeployExecutor 接口注入派发/晋级/回滚/状态查询
//     动作（由 Handler 实现），避免反向依赖，且便于单测注入桩。
//   - FederationStore 为联邦发布计划 CRUD 接口（内存实现默认，可扩展 SQL）。
//   - 协调器不持有 goroutine：Start/Promote/Reconcile/Rollback 均同步执行，由调用方（HTTP handler
//     或 controlplane 后台循环）决定并发。ReconcileFedAll 供后台周期对账所有进行中联邦发布。
//   - 两层灰度正交：联邦层按 Mode（sequential/parallel）+ Member.Order/Weight 在集群间协调；
//     成员内部仍可独立配置 Strategy（canary/bluegreen），由 DeployExecutor 派发的子 DeployTask 承载。
package deploy

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ErrFedNotFound 联邦发布计划不存在。
var ErrFedNotFound = errors.New("federation deploy not found")

// ErrFedTenantMismatch 联邦发布租户越权访问。
var ErrFedTenantMismatch = errors.New("federation tenant mismatch")

// FederationStore 联邦发布计划存储接口（双后端 Memory / SQL，与 DeployStore 同模式）。
// 行级租户隔离：所有读/写均按 TenantID 过滤（空串=开发模式放行全部）。
type FederationStore interface {
	// Create 创建联邦发布计划（自动补 ID / 状态=fed_pending / 时间戳）。
	Create(ctx context.Context, f *FederationDeploy) (*FederationDeploy, error)
	// Get 按 ID 查询（tenantID 非空时校验归属）。
	Get(ctx context.Context, id int64, tenantID string) (*FederationDeploy, error)
	// Update 更新联邦发布计划（自动刷 UpdatedAt；禁止越权改租户）。
	Update(ctx context.Context, f *FederationDeploy) error
	// List 按租户/状态列出（二者皆可空）。
	List(ctx context.Context, tenantID, status string) ([]FederationDeploy, error)
	// Delete 删除联邦发布计划（tenantID 非空时校验归属）。
	Delete(ctx context.Context, id int64, tenantID string) error
}

// MemoryFederationStore 内存实现（默认后端，无外部依赖）。
type MemoryFederationStore struct {
	mu  sync.RWMutex
	m   map[int64]*FederationDeploy
	seq int64
}

// NewMemoryFederationStore 构造内存联邦发布存储。
func NewMemoryFederationStore() *MemoryFederationStore {
	return &MemoryFederationStore{m: make(map[int64]*FederationDeploy)}
}

// Create 创建联邦发布计划。
func (s *MemoryFederationStore) Create(ctx context.Context, f *FederationDeploy) (*FederationDeploy, error) {
	if f == nil {
		return nil, errInvalid("nil")
	}
	if f.TenantID == "" {
		return nil, errInvalid("tenant_id required")
	}
	if err := f.Valid(); err != nil {
		return nil, err
	}
	if f.Status == "" {
		f.Status = FedStatusPending
	}
	now := time.Now()
	if f.CreatedAt.IsZero() {
		f.CreatedAt = now
	}
	f.UpdatedAt = now
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	f.ID = s.seq
	cp := *f
	// 深拷贝 Members 切片，避免外部修改污染存储。
	cp.Members = append([]FederationMember(nil), f.Members...)
	s.m[f.ID] = &cp
	return &cp, nil
}

// Get 按 ID 查询（租户校验）。
func (s *MemoryFederationStore) Get(ctx context.Context, id int64, tenantID string) (*FederationDeploy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, ok := s.m[id]
	if !ok {
		return nil, ErrFedNotFound
	}
	if tenantID != "" && f.TenantID != tenantID {
		return nil, ErrFedTenantMismatch
	}
	cp := *f
	cp.Members = append([]FederationMember(nil), f.Members...)
	return &cp, nil
}

// Update 更新联邦发布计划。
func (s *MemoryFederationStore) Update(ctx context.Context, f *FederationDeploy) error {
	if f == nil {
		return errInvalid("nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.m[f.ID]
	if !ok {
		return ErrFedNotFound
	}
	if f.TenantID != "" && old.TenantID != f.TenantID {
		return ErrFedTenantMismatch
	}
	f.UpdatedAt = time.Now()
	cp := *f
	cp.Members = append([]FederationMember(nil), f.Members...)
	s.m[f.ID] = &cp
	return nil
}

// List 按租户/状态列出。
func (s *MemoryFederationStore) List(ctx context.Context, tenantID, status string) ([]FederationDeploy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]FederationDeploy, 0)
	for _, f := range s.m {
		if tenantID != "" && f.TenantID != tenantID {
			continue
		}
		if status != "" && f.Status != status {
			continue
		}
		cp := *f
		cp.Members = append([]FederationMember(nil), f.Members...)
		out = append(out, cp)
	}
	// 稳定排序便于测试与展示。
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Delete 删除联邦发布计划（租户校验）。
func (s *MemoryFederationStore) Delete(ctx context.Context, id int64, tenantID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.m[id]
	if !ok {
		return ErrFedNotFound
	}
	if tenantID != "" && f.TenantID != tenantID {
		return ErrFedTenantMismatch
	}
	delete(s.m, id)
	return nil
}

// DeployExecutor 派发成员子部署的防腐接口（由 Handler 实现，避免协调器反向依赖 Handler）。
//
// 协调器为每个 FederationMember 调用 CreateAndExecute 创建子 DeployTask（克隆联邦 Template +
// Member.TargetIDs）并立即执行；后续通过 MemberStatus 查询子部署终态，PromoteMember/RollbackMember
// 推进/回滚成员子部署。
type DeployExecutor interface {
	// CreateAndExecute 创建子部署（基于 template + targetIDs）并执行，返回子部署 ID。
	CreateAndExecute(ctx context.Context, template *DeployTask, targetIDs, tenantID string) (int64, error)
	// MemberStatus 查询子部署状态（返回 Status* 常量）。
	MemberStatus(ctx context.Context, deployID int64, tenantID string) (string, error)
	// PromoteMember 晋级成员子部署（canary/gated -> promoting/success）。
	PromoteMember(ctx context.Context, deployID int64, tenantID string) error
	// RollbackMember 回滚成员子部署。
	RollbackMember(ctx context.Context, deployID int64, tenantID string) error
}

// FederationCoordinator 跨集群灰度协调器。
type FederationCoordinator struct {
	store FederationStore
	exec  DeployExecutor
}

// NewFederationCoordinator 构造协调器。store 必须非空；exec 为空时仅支持创建/查询，不支持派发
// （Start/Promote 返回错误，便于只读监控场景）。
func NewFederationCoordinator(store FederationStore, exec DeployExecutor) *FederationCoordinator {
	return &FederationCoordinator{store: store, exec: exec}
}

// Store 暴露底层存储（供 handler/controlplane 调用）。
func (c *FederationCoordinator) Store() FederationStore { return c.store }

// Start 启动联邦发布：按 Mode + Order/Weight 派发首批成员子部署。
//
// 状态流转：fed_pending -> fed_canary（sequential 首批 / parallel 灰度阶段）
// 或 fed_running（parallel 且首批即全量）。无可用成员则 fed_failed。
func (c *FederationCoordinator) Start(ctx context.Context, id int64, tenantID string) error {
	f, err := c.store.Get(ctx, id, tenantID)
	if err != nil {
		return err
	}
	if f.Status != FedStatusPending {
		return fmt.Errorf("federation %d not startable in status %s", id, f.Status)
	}
	if c.exec == nil {
		return fmt.Errorf("deploy executor not configured")
	}
	batch := f.firstBatchMembers()
	if len(batch) == 0 {
		f.Status = FedStatusFailed
		return c.store.Update(ctx, f)
	}
	// 派发首批成员。
	for i := range batch {
		m := &batch[i]
		deployID, err := c.exec.CreateAndExecute(ctx, &f.Template, m.TargetIDs, f.TenantID)
		if err != nil {
			// 单成员派发失败：记录错误，标记该成员 failed，继续派发其余（容错）。
			c.setMemberError(f, m.ClusterID, err)
			continue
		}
		c.setMemberDispatched(f, m.ClusterID, deployID)
	}
	// 聚合首批结果决定联邦状态。
	c.recomputeStatus(f)
	return c.store.Update(ctx, f)
}

// Promote 推进联邦发布：将下一批成员派发（sequential）或全量晋级（parallel）。
//
// 仅 fed_canary/fed_gated 可推进。推进前评估联邦门禁（已派发成员子部署终态须达标）。
func (c *FederationCoordinator) Promote(ctx context.Context, id int64, tenantID string) error {
	f, err := c.store.Get(ctx, id, tenantID)
	if err != nil {
		return err
	}
	if f.Status != FedStatusCanary && f.Status != FedStatusGated {
		return fmt.Errorf("federation %d cannot promote from status %s", id, f.Status)
	}
	if c.exec == nil {
		return fmt.Errorf("deploy executor not configured")
	}

	// 评估联邦门禁：已派发成员子部署须全部成功（按 ResolvedFedGate 的 SuccessRate 阈值）。
	if pass, reason := c.evaluateFedGate(ctx, f); !pass {
		return fmt.Errorf("federation gate not passed: %s", reason)
	}

	// parallel 模式：promote 即全量晋级，已派发成员子部署晋级，联邦状态 -> fed_promoting。
	if f.EffectiveMode() == FedModeParallel {
		for i := range f.Members {
			m := &f.Members[i]
			if m.DeployID > 0 && (m.Status == StatusCanary || m.Status == StatusGated) {
				if err := c.exec.PromoteMember(ctx, m.DeployID, f.TenantID); err != nil {
					c.setMemberError(f, m.ClusterID, err)
					continue
				}
				m.Status = StatusPromoting
			}
		}
		f.Status = FedStatusPromoting
		return c.store.Update(ctx, f)
	}

	// sequential 模式：派发下一批成员。
	doneMaxOrder := f.maxDispatchedOrder()
	next := f.nextBatchMembers(doneMaxOrder)
	if len(next) == 0 {
		// 无后续成员：首批即全部，promote 即全量晋级 -> fed_success（成员已成功）。
		f.Status = FedStatusSuccess
		return c.store.Update(ctx, f)
	}
	for i := range next {
		m := &next[i]
		deployID, err := c.exec.CreateAndExecute(ctx, &f.Template, m.TargetIDs, f.TenantID)
		if err != nil {
			c.setMemberError(f, m.ClusterID, err)
			continue
		}
		c.setMemberDispatched(f, m.ClusterID, deployID)
	}
	f.Status = FedStatusPromoting
	c.recomputeStatus(f)
	return c.store.Update(ctx, f)
}

// Reconcile 对账单个联邦发布：查询各成员子部署状态，聚合联邦级状态。
//
// 聚合规则：
//   - 任一成员 failed -> 联邦 failed，若 AutoRollback 则回滚已成功成员。
//   - 全部成员 success -> 联邦 success。
//   - 已派发成员全成功且有未派发成员 -> fed_gated（可 Promote 推进）。
//   - 否则保持当前进行中状态。
func (c *FederationCoordinator) Reconcile(ctx context.Context, id int64, tenantID string) error {
	f, err := c.store.Get(ctx, id, tenantID)
	if err != nil {
		return err
	}
	switch f.Status {
	case FedStatusPending, FedStatusSuccess, FedStatusFailed, FedStatusRolledBack:
		return nil // 终态/未启动不对账
	}
	// 查询各成员子部署最新状态。
	changed := false
	for i := range f.Members {
		m := &f.Members[i]
		if m.DeployID == 0 {
			continue
		}
		st, err := c.exec.MemberStatus(ctx, m.DeployID, f.TenantID)
		if err != nil {
			continue
		}
		if st != m.Status {
			m.Status = st
			changed = true
		}
	}
	c.recomputeStatus(f)

	// 失败 + 自动回滚。
	if f.Status == FedStatusFailed && f.AutoRollback {
		if err := c.rollbackDispatched(ctx, f); err != nil {
			return err
		}
		f.Status = FedStatusRolledBack
		changed = true
	}
	if !changed && f.Status == FedStatusCanary {
		// 状态未变且仍为 canary，避免无谓写回。
		return nil
	}
	return c.store.Update(ctx, f)
}

// Rollback 回滚联邦发布：已派发成员子部署全部回滚，联邦状态 -> fed_rolledback。
func (c *FederationCoordinator) Rollback(ctx context.Context, id int64, tenantID string) error {
	f, err := c.store.Get(ctx, id, tenantID)
	if err != nil {
		return err
	}
	switch f.Status {
	case FedStatusRunning, FedStatusCanary, FedStatusGated, FedStatusPromoting, FedStatusSuccess:
	default:
		return fmt.Errorf("federation %d cannot rollback from status %s", id, f.Status)
	}
	if err := c.rollbackDispatched(ctx, f); err != nil {
		return err
	}
	f.Status = FedStatusRolledBack
	return c.store.Update(ctx, f)
}

// rollbackDispatched 回滚所有已派发成员子部署（忽略单成员回滚错误，尽力回滚）。
func (c *FederationCoordinator) rollbackDispatched(ctx context.Context, f *FederationDeploy) error {
	if c.exec == nil {
		return nil
	}
	for i := range f.Members {
		m := &f.Members[i]
		if m.DeployID == 0 {
			continue
		}
		if err := c.exec.RollbackMember(ctx, m.DeployID, f.TenantID); err != nil {
			c.setMemberError(f, m.ClusterID, err)
			continue
		}
		m.Status = StatusRolledBack
	}
	return nil
}

// Status 聚合联邦级发布状态视图。
func (c *FederationCoordinator) Status(ctx context.Context, id int64, tenantID string) (*FederationStatus, error) {
	f, err := c.store.Get(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	return computeFederationStatus(f), nil
}

// ReconcileAll 对账所有进行中联邦发布（controlplane 后台周期调用），返回成功对账数。
//
// 去重：按状态分轮 List 时，前一轮对账可能使联邦转入下一状态（如 canary -> gated），
// 后续轮次会再次 List 到同一联邦。用 seen 集合保证每个联邦每轮只对账一次。
func (c *FederationCoordinator) ReconcileAll(ctx context.Context, tenantID string) int {
	n := 0
	seen := make(map[int64]bool)
	for _, status := range []string{FedStatusRunning, FedStatusCanary, FedStatusPromoting, FedStatusGated} {
		list, err := c.store.List(ctx, tenantID, status)
		if err != nil {
			continue
		}
		for i := range list {
			id := list[i].ID
			if seen[id] {
				continue
			}
			seen[id] = true
			if c.Reconcile(ctx, id, tenantID) == nil {
				n++
			}
		}
	}
	return n
}

// =============================================================================
// 内部辅助
// =============================================================================

// setMemberDispatched 标记成员已派发（回填 DeployID + 状态=running）。
func (c *FederationCoordinator) setMemberDispatched(f *FederationDeploy, clusterID string, deployID int64) {
	for i := range f.Members {
		if f.Members[i].ClusterID == clusterID {
			f.Members[i].DeployID = deployID
			f.Members[i].Status = StatusRunning
			f.Members[i].Error = ""
		}
	}
}

// setMemberError 标记成员派发失败。
func (c *FederationCoordinator) setMemberError(f *FederationDeploy, clusterID string, err error) {
	for i := range f.Members {
		if f.Members[i].ClusterID == clusterID {
			f.Members[i].Status = StatusFailed
			f.Members[i].Error = err.Error()
		}
	}
}

// evaluateFedGate 评估联邦门禁：已派发成员子部署须全部终态且按门禁阈值达标。
//
// 判定：统计已派发成员中 success/failed 数，套用 ResolvedFedGate（与单部署 evaluateGate 同语义）。
// 任一成员未终态（running/canary/promoting）视为未完成，返回不通过（等待）。
func (c *FederationCoordinator) evaluateFedGate(ctx context.Context, f *FederationDeploy) (bool, string) {
	dispatched, done, failed := 0, 0, 0
	for i := range f.Members {
		m := &f.Members[i]
		if m.DeployID == 0 {
			continue
		}
		dispatched++
		switch m.Status {
		case StatusSuccess, StatusGated:
			// success=成员已完成；gated=成员内部门禁通过可晋级。两者均视为联邦门禁达标。
			done++
		case StatusFailed:
			failed++
		default:
			// 成员未终态（running/canary/promoting），门禁不通过（等待成员完成）。
			return false, fmt.Sprintf("member %s not terminal (status=%s)", m.ClusterID, m.Status)
		}
	}
	if dispatched == 0 {
		return false, "no dispatched members"
	}
	gate := f.ResolvedFedGate()
	if evaluateGate(gate, done, failed, dispatched) {
		return true, ""
	}
	return false, fmt.Sprintf("gate failed: %d done, %d failed, %d total", done, failed, dispatched)
}

// recomputeStatus 根据成员子部署状态重算联邦级状态（不落库，仅改内存）。
//
// 规则：
//   - 任一成员 failed -> fed_failed
//   - 全部成员 success -> fed_success
//   - 已派发成员全 success 且有未派发成员 -> fed_gated
//   - 否则若当前为 pending -> fed_canary（首批派发后灰度阶段），否则保持
func (c *FederationCoordinator) recomputeStatus(f *FederationDeploy) {
	dispatched, done, failed := 0, 0, 0
	for i := range f.Members {
		m := &f.Members[i]
		if m.DeployID == 0 {
			continue
		}
		dispatched++
		switch m.Status {
		case StatusSuccess:
			done++
		case StatusFailed:
			failed++
		}
	}
	total := len(f.Members)
	if failed > 0 {
		f.Status = FedStatusFailed
		return
	}
	if dispatched == total && done == total {
		f.Status = FedStatusSuccess
		return
	}
	// 已派发成员全成功但有未派发成员 -> 门禁通过可推进。
	if dispatched > 0 && done == dispatched && dispatched < total {
		f.Status = FedStatusGated
		return
	}
	// 首批派发后进入灰度阶段（成员仍在 running/canary）。
	if f.Status == FedStatusPending {
		f.Status = FedStatusCanary
	}
}

// computeFederationStatus 构造联邦级状态聚合视图（纯函数，不依赖协调器）。
func computeFederationStatus(f *FederationDeploy) *FederationStatus {
	done, failed, pending := 0, 0, 0
	for i := range f.Members {
		switch f.Members[i].Status {
		case StatusSuccess:
			done++
		case StatusFailed:
			failed++
		case "", StatusCreated:
			pending++
		}
	}
	members := append([]FederationMember(nil), f.Members...)
	return &FederationStatus{
		ID:             f.ID,
		OverallStatus:  f.Status,
		TotalMembers:   len(f.Members),
		DoneMembers:    done,
		FailedMembers:  failed,
		PendingMembers: pending,
		Members:        members,
	}
}
