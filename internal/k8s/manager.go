// manager.go 实现多集群连接管理：ClusterManager 维护 clusterID -> K8sClient 映射，
// 提供 AddCluster / RemoveCluster / GetClient / TestCluster 方法，全部并发安全。
//
// 设计要点：
//   - sync.RWMutex 保护 clients map，读多写少场景用 RLock 优化；
//   - AddCluster 幂等：同 clusterID 重复添加会替换旧连接（先 Close 旧连接再写入新连接）；
//   - RemoveCluster 安全：clusterID 不存在时静默返回；
//   - TestCluster 不持久化连接，仅临时构造客户端测试连通性（避免污染 clients map）。
package k8s

import (
	"fmt"
	"sync"
)

// ClusterManager 管理多个 K8s 集群连接。
//
// clients 按 clusterID 索引 K8sClient；并发访问由 mu 保护。
// 生命周期：控制面 NewServer 时构造，AddCluster 在用户创建/更新集群时调用，
// RemoveCluster 在用户删除集群时调用，GetClient 在用户查询集群资源时调用。
type ClusterManager struct {
	mu      sync.RWMutex
	clients map[string]*K8sClient // clusterID -> client
}

// NewClusterManager 构造空的多集群管理器。
func NewClusterManager() *ClusterManager {
	return &ClusterManager{
		clients: make(map[string]*K8sClient),
	}
}

// AddCluster 添加/更新集群连接。
//
// 幂等：同 clusterID 重复调用会替换旧连接（先 Close 旧连接再写入新连接），
// 避免连接泄漏。kubeconfigData 为 kubeconfig YAML 内容。
func (m *ClusterManager) AddCluster(clusterID, kubeconfigData string) error {
	if clusterID == "" {
		return fmt.Errorf("k8s: clusterID 为空")
	}
	client, err := NewK8sClient(clusterID, kubeconfigData)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// 替换旧连接前先 Close，避免连接泄漏（client-go 当前 Close 为 noop，但保留以兼容后续扩展）。
	if old, ok := m.clients[clusterID]; ok {
		old.Close()
	}
	m.clients[clusterID] = client
	return nil
}

// RemoveCluster 移除集群连接。
//
// 安全：clusterID 不存在时静默返回。移除前 Close 旧连接。
func (m *ClusterManager) RemoveCluster(clusterID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.clients[clusterID]; ok {
		old.Close()
		delete(m.clients, clusterID)
	}
}

// GetClient 获取指定集群的客户端。
//
// 不存在时返回错误（调用方据此返回 404）。
func (m *ClusterManager) GetClient(clusterID string) (*K8sClient, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.clients[clusterID]
	if !ok {
		return nil, fmt.Errorf("k8s: 集群 %q 未连接或已移除", clusterID)
	}
	return c, nil
}

// TestCluster 测试集群连接。
//
// 不持久化连接：仅临时构造客户端测试连通性，避免污染 clients map。
// 适用于用户「测试连接」按钮：先验证再决定是否保存。
func (m *ClusterManager) TestCluster(clusterID, kubeconfigData string) error {
	if clusterID == "" {
		return fmt.Errorf("k8s: clusterID 为空")
	}
	client, err := NewK8sClient(clusterID, kubeconfigData)
	if err != nil {
		return err
	}
	defer client.Close()
	return client.TestConnection()
}

// ListClusters 返回当前已连接的集群 ID 列表（仅供调试/观测）。
func (m *ClusterManager) ListClusters() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.clients))
	for id := range m.clients {
		out = append(out, id)
	}
	return out
}
