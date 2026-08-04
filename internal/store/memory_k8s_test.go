// memory_k8s_test.go 测试 MemoryStore 的 K8sClusterStore 实现（Phase 3 K8s 集群管理 CRUD）。
//
// 覆盖范围：
//   - ListK8sClusters：空列表、单集群、多集群、按创建时间升序
//   - GetK8sCluster：命中、未命中、深拷贝隔离
//   - SaveK8sCluster：新建（ID 自动分配）、按 ID 幂等更新、默认值填充（CreatedAt/Status）
//   - DeleteK8sCluster：命中、未命中返回 false
//
// 测试风格与 memory_test.go 一致：白盒（package store），直接操作 MemoryStore 字段。
package store

import (
	"fmt"
	"testing"
	"time"
)

// TestMemoryStore_K8sClusterCRUD 覆盖 K8s 集群配置的完整生命周期：
// 空列表 → 保存 → 列表 1 个 → Get 命中 → 更新 → Delete → 空列表 → Delete 不存在返回 false。
func TestMemoryStore_K8sClusterCRUD(t *testing.T) {
	s := NewMemoryStore()

	// 1. 初始列表为空
	clusters := s.ListK8sClusters()
	if len(clusters) != 0 {
		t.Fatalf("expected empty list, got %d", len(clusters))
	}

	// 2. 保存集群
	c1 := &K8sCluster{
		ID:         "cluster-1",
		Name:       "test-cluster",
		Server:     "https://1.2.3.4:6443",
		Kubeconfig: "apiVersion: v1",
		Status:     "online",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	s.SaveK8sCluster(c1)

	// 3. 列表有 1 个
	clusters = s.ListK8sClusters()
	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(clusters))
	}

	// 4. GetK8sCluster 命中
	got := s.GetK8sCluster("cluster-1")
	if got == nil || got.Name != "test-cluster" {
		t.Fatalf("GetK8sCluster failed: %+v", got)
	}

	// 5. 更新集群（按 ID 幂等）
	c1.Status = "offline"
	s.SaveK8sCluster(c1)
	got = s.GetK8sCluster("cluster-1")
	if got.Status != "offline" {
		t.Fatalf("update failed: status=%q, want offline", got.Status)
	}

	// 6. 删除集群
	if !s.DeleteK8sCluster("cluster-1") {
		t.Fatalf("delete failed")
	}

	// 7. 删除后列表为空
	clusters = s.ListK8sClusters()
	if len(clusters) != 0 {
		t.Fatalf("expected empty after delete, got %d", len(clusters))
	}

	// 8. 删除不存在的返回 false
	if s.DeleteK8sCluster("nonexistent") {
		t.Fatalf("delete nonexistent should return false")
	}
}

// TestMemoryStore_K8sClusterMultiple 验证多集群保存与列表计数。
func TestMemoryStore_K8sClusterMultiple(t *testing.T) {
	s := NewMemoryStore()

	for i := 0; i < 5; i++ {
		s.SaveK8sCluster(&K8sCluster{
			ID:         fmt.Sprintf("cluster-%d", i),
			Name:       fmt.Sprintf("cluster-%d", i),
			Server:     "https://example.com",
			Kubeconfig: "config",
			Status:     "online",
		})
	}

	clusters := s.ListK8sClusters()
	if len(clusters) != 5 {
		t.Fatalf("expected 5 clusters, got %d", len(clusters))
	}
}

// TestMemoryStore_K8sClusterAutoID 验证 ID 为空时由 store 分配随机 ID。
func TestMemoryStore_K8sClusterAutoID(t *testing.T) {
	s := NewMemoryStore()
	c := &K8sCluster{
		Name:       "auto-id-cluster",
		Server:     "https://example.com",
		Kubeconfig: "config",
	}
	s.SaveK8sCluster(c)
	if c.ID == "" {
		t.Fatal("expected server-assigned ID, got empty")
	}
	got := s.GetK8sCluster(c.ID)
	if got == nil {
		t.Fatalf("GetK8sCluster(%q) = nil, want non-nil", c.ID)
	}
}

// TestMemoryStore_K8sClusterDefaults 验证新建集群的默认值填充：
// CreatedAt 为空时填当前时间；Status 为空时默认 "unknown"。
func TestMemoryStore_K8sClusterDefaults(t *testing.T) {
	s := NewMemoryStore()
	c := &K8sCluster{
		ID:         "cluster-defaults",
		Name:       "defaults",
		Kubeconfig: "config",
		// CreatedAt / Status 留空
	}
	s.SaveK8sCluster(c)

	got := s.GetK8sCluster("cluster-defaults")
	if got == nil {
		t.Fatal("GetK8sCluster = nil")
	}
	if got.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should be filled by SaveK8sCluster")
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt should be filled by SaveK8sCluster")
	}
	if got.Status != "unknown" {
		t.Fatalf("Status = %q, want unknown (default)", got.Status)
	}
}

// TestMemoryStore_K8sClusterNotFound 验证 GetK8sCluster 对不存在的 ID 返回 nil。
func TestMemoryStore_K8sClusterNotFound(t *testing.T) {
	s := NewMemoryStore()
	if got := s.GetK8sCluster("no-such-cluster"); got != nil {
		t.Fatalf("GetK8sCluster(nonexistent) = %+v, want nil", got)
	}
}

// TestMemoryStore_K8sClusterDeepCopy 验证 ListK8sClusters / GetK8sCluster 返回深拷贝：
// 修改返回值不影响 store 内部状态。
func TestMemoryStore_K8sClusterDeepCopy(t *testing.T) {
	s := NewMemoryStore()
	s.SaveK8sCluster(&K8sCluster{
		ID:         "cluster-dc",
		Name:       "original",
		Kubeconfig: "secret-config",
		Status:     "online",
	})

	// GetK8sCluster 深拷贝
	got := s.GetK8sCluster("cluster-dc")
	got.Name = "MUTATED"
	got.Kubeconfig = "LEAKED"
	inner := s.GetK8sCluster("cluster-dc")
	if inner.Name != "original" {
		t.Fatalf("GetK8sCluster not deep-copied: Name=%q, want original", inner.Name)
	}
	if inner.Kubeconfig != "secret-config" {
		t.Fatalf("GetK8sCluster not deep-copied: Kubeconfig=%q, want secret-config", inner.Kubeconfig)
	}

	// ListK8sClusters 深拷贝
	list := s.ListK8sClusters()
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}
	list[0].Status = "MUTATED"
	list2 := s.ListK8sClusters()
	if list2[0].Status != "online" {
		t.Fatalf("ListK8sClusters not deep-copied: Status=%q, want online", list2[0].Status)
	}
}

// TestMemoryStore_K8sClusterSaveNil 验证 SaveK8sCluster(nil) 安全无 panic。
func TestMemoryStore_K8sClusterSaveNil(t *testing.T) {
	s := NewMemoryStore()
	// 不应 panic
	s.SaveK8sCluster(nil)
	if len(s.ListK8sClusters()) != 0 {
		t.Fatal("SaveK8sCluster(nil) should not add any cluster")
	}
}

// TestMemoryStore_K8sClusterListOrder 验证 ListK8sClusters 按创建时间升序返回。
func TestMemoryStore_K8sClusterListOrder(t *testing.T) {
	s := NewMemoryStore()
	base := time.Now()
	// 故意乱序插入：c2 最早，c1 居中，c3 最晚
	s.SaveK8sCluster(&K8sCluster{ID: "c2", Name: "c2", CreatedAt: base.Add(-time.Hour)})
	s.SaveK8sCluster(&K8sCluster{ID: "c1", Name: "c1", CreatedAt: base.Add(-2 * time.Hour)})
	s.SaveK8sCluster(&K8sCluster{ID: "c3", Name: "c3", CreatedAt: base})

	list := s.ListK8sClusters()
	if len(list) != 3 {
		t.Fatalf("list len = %d, want 3", len(list))
	}
	// 升序：c1 < c2 < c3
	if list[0].ID != "c1" || list[1].ID != "c2" || list[2].ID != "c3" {
		t.Fatalf("list order = %v,%v,%v; want c1,c2,c3 (ascending CreatedAt)",
			list[0].ID, list[1].ID, list[2].ID)
	}
}