// abac_persistence.go ABAC 策略缓存的持久化接线（批次47）。
//
// 设计（镜像 gateway_persistence.go 的 TD-66 P1 模式）：
//   - store 是事实源，s.abacPolicies 是评估缓存；
//   - 写路径（policy-create/delete）先落 store、成功再改缓存（见 abac_engine.go）；
//   - 启动时用 ListABACPolicyRecords("") 全量装载（重启不丢，恢复 default-deny 之外的真实策略）；
//   - 多副本：单副本写不到别的副本内存，故由 startABACPolicyRefresh 每 5s 回读本副本
//     见过的租户；按 version 比对，未变即跳过重建（不产生无谓的缓存替换）。
//
// 并发语义：
//   - store I/O 在 abacMu 之外进行（SQL 后端可能慢，不阻塞评估读路径）；
//   - 应用快照前校验 abacRev 未变——期间有本地写则本轮快照可能过期，直接放弃下轮再来；
//   - 评估读路径只拷贝 slice 快照，刷新只替换 slice 内容，race-free。
package controlplane

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/internal/store/model"
)

// abacPolicyRefreshInterval 多副本 ABAC 策略缓存的最长陈旧窗口。
const abacPolicyRefreshInterval = 5 * time.Second

// abacVersionKey 版本索引键：租户 + NUL + 策略 ID。
// 用 NUL 分隔避免租户前缀互相包含（"t" 与 "t2"）时键碰撞。
func abacVersionKey(tenantID, id string) string {
	return tenantID + "\x00" + id
}

// abacPolicyFromRecord 持久化记录 → 评估镜像。store 不保存 condition（见 model.ABACPolicyRecord）。
func abacPolicyFromRecord(r model.ABACPolicyRecord) abacPolicy {
	return abacPolicy{
		ID:       r.ID,
		TenantID: r.TenantID,
		Subject:  r.Subject,
		Resource: r.Resource,
		Action:   r.Action,
		Effect:   r.Effect,
	}
}

// noteABACTenant 登记本副本见过的租户（评估/列表/写路径），刷新循环据此限定回读范围。
func (s *Server) noteABACTenant(tenantID string) {
	if tenantID == "" {
		return
	}
	s.abacMu.RLock()
	_, ok := s.abacTenants[tenantID]
	s.abacMu.RUnlock()
	if ok {
		return
	}
	s.abacMu.Lock()
	s.noteABACTenantLocked(tenantID)
	s.abacMu.Unlock()
}

// noteABACTenantLocked 调用方必须持有 abacMu 写锁。
func (s *Server) noteABACTenantLocked(tenantID string) {
	if tenantID == "" {
		return
	}
	if s.abacTenants == nil {
		s.abacTenants = make(map[string]struct{})
	}
	s.abacTenants[tenantID] = struct{}{}
}

// setABACVersionLocked 记录策略版本（调用方必须持有 abacMu 写锁）。
func (s *Server) setABACVersionLocked(tenantID, id string, version int64) {
	if s.abacVersions == nil {
		s.abacVersions = make(map[string]int64)
	}
	s.abacVersions[abacVersionKey(tenantID, id)] = version
}

// initABACPoliciesFromStore 从 store 全量装载 ABAC 策略镜像（启动时调用）。
//
// 无 store 时不做任何事（保持纯内存语义）；store 读取失败时 store 实现返回 nil，
// 缓存为空并按 default-deny 兜底——安全侧，不会因装载失败而放行。
func (s *Server) initABACPoliciesFromStore() {
	if s.store == nil {
		return
	}
	recs := s.store.ListABACPolicyRecords("")
	sort.Slice(recs, func(i, j int) bool {
		if recs[i].TenantID != recs[j].TenantID {
			return recs[i].TenantID < recs[j].TenantID
		}
		return recs[i].ID < recs[j].ID
	})
	policies := make([]abacPolicy, 0, len(recs))
	versions := make(map[string]int64, len(recs))
	tenants := make(map[string]struct{}, len(recs))
	for _, r := range recs {
		policies = append(policies, abacPolicyFromRecord(r))
		versions[abacVersionKey(r.TenantID, r.ID)] = r.Version
		if r.TenantID != "" {
			tenants[r.TenantID] = struct{}{}
		}
	}
	s.abacMu.Lock()
	s.abacPolicies = policies
	s.abacVersions = versions
	s.abacTenants = tenants
	s.abacRev++
	s.abacMu.Unlock()
}

// startABACPolicyRefresh 启动多副本策略缓存的周期刷新（单 goroutine）。
//
// 生命周期：ctx 由 server_lifecycle.go Start 的 signal.NotifyContext 提供，取消即退出；
// ticker 随 defer 释放。interval<=0 时回退到 5s 默认值（测试可注入短间隔）。
func (s *Server) startABACPolicyRefresh(ctx context.Context, interval time.Duration) {
	if s.store == nil {
		return
	}
	if interval <= 0 {
		interval = abacPolicyRefreshInterval
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.refreshABACPolicies(ctx)
			}
		}
	}()
}

// refreshABACPolicies 回读已见租户的策略记录，版本有变化才替换该租户的缓存。
//
// store 读取在锁外；应用前校验 abacRev（期间本地写会使其变化，本轮放弃）。
func (s *Server) refreshABACPolicies(ctx context.Context) {
	if s.store == nil {
		return
	}
	s.abacMu.RLock()
	tenants := make([]string, 0, len(s.abacTenants))
	for tenantID := range s.abacTenants {
		tenants = append(tenants, tenantID)
	}
	rev := s.abacRev
	s.abacMu.RUnlock()
	sort.Strings(tenants)

	for _, tenantID := range tenants {
		if ctx != nil && ctx.Err() != nil {
			return
		}
		recs := s.store.ListABACPolicyRecords(tenantID)
		s.abacMu.Lock()
		if s.abacRev != rev {
			s.abacMu.Unlock()
			return
		}
		if s.abacTenantVersionsMatchLocked(tenantID, recs) {
			s.abacMu.Unlock()
			continue
		}
		s.replaceTenantPoliciesLocked(tenantID, recs)
		s.abacMu.Unlock()
	}
}

// abacTenantVersionsMatchLocked 判断该租户的 store 记录与缓存版本是否完全一致。
// 数量与逐条 version 都要相等：删除与新增都会让数量或键集合失配。
func (s *Server) abacTenantVersionsMatchLocked(tenantID string, recs []model.ABACPolicyRecord) bool {
	prefix := tenantID + "\x00"
	cached := 0
	for key := range s.abacVersions {
		if strings.HasPrefix(key, prefix) {
			cached++
		}
	}
	if cached != len(recs) {
		return false
	}
	for _, r := range recs {
		version, ok := s.abacVersions[abacVersionKey(tenantID, r.ID)]
		if !ok || version != r.Version {
			return false
		}
	}
	return true
}

// replaceTenantPoliciesLocked 用 store 快照替换单租户缓存（其他租户不动）。
// 调用方必须持有 abacMu 写锁。
func (s *Server) replaceTenantPoliciesLocked(tenantID string, recs []model.ABACPolicyRecord) {
	sort.Slice(recs, func(i, j int) bool { return recs[i].ID < recs[j].ID })
	kept := s.abacPolicies[:0]
	for _, p := range s.abacPolicies {
		if p.TenantID != tenantID {
			kept = append(kept, p)
		}
	}
	for _, r := range recs {
		kept = append(kept, abacPolicyFromRecord(r))
	}
	s.abacPolicies = kept
	prefix := tenantID + "\x00"
	for key := range s.abacVersions {
		if strings.HasPrefix(key, prefix) {
			delete(s.abacVersions, key)
		}
	}
	for _, r := range recs {
		s.setABACVersionLocked(tenantID, r.ID, r.Version)
	}
	s.noteABACTenantLocked(tenantID)
}
