// memory_k8s.go 实现 MemoryStore 的 K8sClusterStore 子接口（Phase 3 K8s 集群管理）。
//
// K8s 集群配置内存实现：
//   - k8sClusters 字段在 MemoryStore struct 中定义（map[string]*K8sCluster）；
//   - NewMemoryStore 中初始化为空 map；
//   - 本文件实现 CRUD 方法，全部经 m.mu 互斥保护，并发安全。
//
// 设计要点（与 memory_rbac.go 风格一致）：
//   - ListK8sClusters 返回深拷贝避免外部修改破坏内部状态；
//   - SaveK8sCluster 按 ID 幂等（ID 为空时分配随机 ID）；
//   - Kubeconfig 为敏感内容，store 层不脱敏，由 API 层负责。
package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// randK8sClusterID 生成随机 K8s 集群 ID（16 字节十六进制，crypto/rand 密码学安全）。
// 用于 SaveK8sCluster 分配 ID（调用方未填 ID 时）。
func randK8sClusterID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 熵源失败回退时间戳（降级但可容忍，唯一性由 k8sClusters map key 兜底）。
		return fmt.Sprintf("k8s-cluster-%d", time.Now().UnixNano())
	}
	return "k8s-cluster-" + hex.EncodeToString(b)
}

// ListK8sClusters 返回所有 K8s 集群配置（按创建时间升序；深拷贝）。
func (m *MemoryStore) ListK8sClusters(tenantID string) []*K8sCluster {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*K8sCluster, 0, len(m.k8sClusters))
	for _, c := range m.k8sClusters {
		// task 88 租户隔离：tenantID 非空时仅返回同租户集群（空=不过滤，仅内部调用）。
		if tenantID != "" && c.TenantID != tenantID {
			continue
		}
		// 深拷贝，避免外部修改破坏内部状态。
		cp := *c
		out = append(out, &cp)
	}
	// 按创建时间升序（插入排序，避免引入 sort 包；与 ListUsers 风格一致）。
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].CreatedAt.Before(out[j-1].CreatedAt) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

// GetK8sCluster 按 ID 返回单个集群配置（深拷贝；不存在返回 nil）。
func (m *MemoryStore) GetK8sCluster(id string) *K8sCluster {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.k8sClusters[id]
	if !ok {
		return nil
	}
	cp := *c
	return &cp
}

// SaveK8sCluster 创建或更新集群配置（按 ID 幂等）。
//
// 行为：
//   - ID 为空时分配随机 ID（新建场景）；
//   - CreatedAt 为空时填当前时间（新建场景）；
//   - UpdatedAt 始终刷新为当前时间；
//   - Status 为空时默认 "unknown"。
func (m *MemoryStore) SaveK8sCluster(c *K8sCluster) error {
	if c == nil {
		return nil
	}
	// task 88 租户隔离：空租户归一为 default（与 deploy 模块一致）。
	if c.TenantID == "" {
		c.TenantID = "default"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if c.ID == "" {
		c.ID = randK8sClusterID()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	if c.Status == "" {
		c.Status = "unknown"
	}
	c.UpdatedAt = now
	m.k8sClusters[c.ID] = c
	return nil // memory 存储无持久化失败
}

// DeleteK8sCluster 删除集群配置，返回是否删除成功（不存在返回 false）。
func (m *MemoryStore) DeleteK8sCluster(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.k8sClusters[id]; !ok {
		return false
	}
	delete(m.k8sClusters, id)
	return true
}
