// Package leader 提供 task-svc 的选主机制（TD-60 A-2 阶段）。
//
// 设计目标：
//   - **接口注入模式**：LeaderElector 为接口，scheduler 通过 RenewFunc 闭包包装注入，
//     不破坏 scheduler 现有 API/测试（RenewFunc 签名不变）。
//   - **stub 为默认**：StubLeaderElector 永真（单进程/测试用），与 A-1 阶段行为一致。
//   - **K8s Lease 真实现可选**：K8sLeaseElector 基于 coordination.k8s.io/v1 Lease，
//     多副本生产环境由 main 注入；单机/CI 不依赖 K8s client，零依赖编译。
//
// 用法（main.go）：
//
//	// A-1 单进程：stub 永真
//	elector := leader.NewStub()
//	// A-2 多副本：K8s Lease（需注入 clientset）
//	// elector, err = leader.NewK8sLease(clientset, "task-svc-leader", "task-svc-pod", 15*time.Second)
//	renew := func(ctx context.Context, ttl time.Duration) bool {
//	    return elector.Renew(ctx, ttl)
//	}
//	sched := scheduler.New(ctx, reclaim, fire, renew)
package leader

import (
	"context"
	"log"
	"sync"
	"time"
)

// LeaderElector 选主接口。
//
// IsLeader 返回当前实例是否为 leader（非阻塞，用于 gate 检查）。
// Renew 续租 leader 租约（阻塞至完成或超时），返回当前实例是否仍为 leader。
// Close 释放选主资源（如 K8s Lease 持有者退出时释放租约）。
type LeaderElector interface {
	// IsLeader 返回当前实例是否持有 leader 租约。
	IsLeader() bool

	// Renew 续租 leader 租约，ttl 为租约有效期。
	// 返回 true 表示续租成功（仍为 leader），false 表示失去 leader 身份。
	Renew(ctx context.Context, ttl time.Duration) bool

	// Close 释放选主资源（优雅退出时调用）。
	Close() error
}

// StubLeaderElector stub 实现：永真（A-1 阶段单进程默认，与原 renew 闭包行为一致）。
//
// 语义：IsLeader() 永远返回 true，Renew() 永远返回 true，Close() 无操作。
// 用于单进程/测试/CI 环境，不依赖任何外部存储。
type StubLeaderElector struct{}

// NewStub 构造一个 stub 选主器（永真）。
func NewStub() *StubLeaderElector {
	return &StubLeaderElector{}
}

// IsLeader 永真（stub 语义：单进程始终为 leader）。
func (StubLeaderElector) IsLeader() bool { return true }

// Renew 永真（stub 语义：续租始终成功）。
func (StubLeaderElector) Renew(context.Context, time.Duration) bool { return true }

// Close 无操作（stub 无资源需释放）。
func (StubLeaderElector) Close() error { return nil }

// K8sLeaseElector 基于 K8s coordination.k8s.io/v1 Lease 的选主实现。
//
// 设计（与 controlplane leader_lease SQL 方案对照，但用 K8s 原语）：
//   - 持有者周期性 Renew(ttl)：若 lease.holderIdentity == self 且
//     lease.renewTime + lease.leaseDurationSeconds > now → 续租成功；
//     否则尝试 acquire（create/update lease with self as holder）。
//   - IsLeader() 非阻塞检查本地缓存（最近一次 Renew 结果）。
//   - 多副本竞争时由 K8s API server 的 resourceVersion 乐观锁保证唯一持有者。
//
// 注意：本实现不直接 import k8s.io/client-go（避免 task-svc 默认构建引入重依赖）。
// 生产环境由 main 注入 K8sLeaseOps 接口（封装 clientset 的 Lease CRUD），
// 本包只提供选主状态机逻辑——保持零依赖编译，与 events 包的接口注入模式一致。
type K8sLeaseElector struct {
	mu          sync.RWMutex
	holder      string // 本实例标识（如 pod name）
	leaseName   string // K8s Lease 资源名
	isLeader    bool   // 本地缓存：最近一次 Renew 结果
	lastRenewAt time.Time
	ops         K8sLeaseOps // K8s Lease 操作接口（由 main 注入）
}

// K8sLeaseOps K8s Lease 操作接口（由 main 注入 clientset 实现）。
//
// 接口最小化：只暴露选主需要的 3 个操作，不引入 k8s.io/client-go 直接依赖。
// Acquire 尝试获取租约（create or update with self as holder），成功返回 true。
// Renew 续租（更新 lease.renewTime），持有者匹配时成功返回 true。
// Release 释放租约（清空 holder），优雅退出时调用。
type K8sLeaseOps interface {
	// Acquire 尝试获取 lease。holderID 为本实例标识，ttl 为租约有效期。
	// 若 lease 不存在或已过期 → 创建/更新为 holderID，返回 true。
	// 若 lease 被其他持有者且未过期 → 返回 false。
	Acquire(ctx context.Context, leaseName, holderID string, ttl time.Duration) bool

	// Renew 续租 lease。仅当当前持有者 == holderID 时更新 renewTime，返回 true。
	// 持有者不匹配或 lease 不存在 → 返回 false。
	Renew(ctx context.Context, leaseName, holderID string, ttl time.Duration) bool

	// Release 释放 lease（清空 holderIdentity）。优雅退出时调用。
	Release(ctx context.Context, leaseName, holderID string) error
}

// NewK8sLease 构造一个 K8s Lease 选主器。
//
// ops 为 K8s Lease 操作接口（由 main 注入 clientset 实现）。
// leaseName 为 K8s Lease 资源名（如 "task-svc-leader"）。
// holderID 为本实例标识（如 pod name，需唯一）。
func NewK8sLease(ops K8sLeaseOps, leaseName, holderID string) *K8sLeaseElector {
	return &K8sLeaseElector{
		ops:       ops,
		leaseName: leaseName,
		holder:    holderID,
	}
}

// IsLeader 返回本地缓存的 leader 状态（非阻塞）。
//
// 注意：本地缓存可能过期（最多滞后一个 Renew 周期）。
// 调用方应周期性调 Renew 刷新缓存（scheduler.leaderLoop 已做）。
func (e *K8sLeaseElector) IsLeader() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.isLeader
}

// Renew 续租 leader 租约。
//
// 若当前持有者 == self → 续租（更新 renewTime）。
// 若当前持有者 != self 或 lease 不存在 → 尝试 acquire。
// 返回 true 表示续租/acquire 成功（本实例现为 leader）。
func (e *K8sLeaseElector) Renew(ctx context.Context, ttl time.Duration) bool {
	if e == nil || e.ops == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	var ok bool
	if e.isLeader {
		// 已是 leader：续租
		ok = e.ops.Renew(ctx, e.leaseName, e.holder, ttl)
	} else {
		// 非 leader：尝试 acquire
		ok = e.ops.Acquire(ctx, e.leaseName, e.holder, ttl)
	}

	if ok {
		if !e.isLeader {
			log.Printf("[leader] 晋升为 leader lease=%s holder=%s", e.leaseName, e.holder)
		}
		e.isLeader = true
		e.lastRenewAt = time.Now()
	} else {
		if e.isLeader {
			log.Printf("[leader] 失去 leader 身份 lease=%s holder=%s", e.leaseName, e.holder)
		}
		e.isLeader = false
	}
	return ok
}

// Close 释放 leader 租约（优雅退出时调用）。
func (e *K8sLeaseElector) Close() error {
	if e == nil || e.ops == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.isLeader {
		if err := e.ops.Release(context.Background(), e.leaseName, e.holder); err != nil {
			log.Printf("[leader] 释放租约失败 lease=%s: %v", e.leaseName, err)
			return err
		}
		e.isLeader = false
	}
	return nil
}
