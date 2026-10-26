// multi_schema_cloud_account.go MultiSchemaStore 对 CloudAccountStore 的委托实现。
//
// 设计要点（与 multi_schema_p6.go 风格一致）：
//   - 带 tenantID 的方法（Create/List）用 storeFor(tenantID) 路由；
//   - 仅按 ID 定位的方法（Get/Update/Delete/Sync）遍历全部租户 store 查找，
//     与 GetSubscription/DeleteSubscription 既有模式一致；
//   - 路由失败返回零值（nil/false/nil error），与现有方法风格一致。
package multischema

import (
	"context"
)

// CreateCloudAccount 创建云账号：用 acct.TenantID 路由。
func (m *MultiSchemaStore) CreateCloudAccount(ctx context.Context, acct *CloudAccount) error {
	if acct == nil {
		return nil
	}
	tenantID := acct.TenantID
	if tenantID == "" {
		tenantID = "default"
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.CreateCloudAccount(ctx, acct)
}

// GetCloudAccount 按 ID 返回单个云账号：遍历全部租户 store 查找。
func (m *MultiSchemaStore) GetCloudAccount(ctx context.Context, id string) (*CloudAccount, error) {
	for _, s := range m.allStores() {
		if acct, err := s.GetCloudAccount(ctx, id); err == nil && acct != nil {
			return acct, nil
		}
	}
	return nil, nil
}

// ListCloudAccounts 返回指定租户的云账号列表：用 tenantID 路由。
func (m *MultiSchemaStore) ListCloudAccounts(ctx context.Context, tenantID string, offset, limit int) ([]*CloudAccount, int, error) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, 0, nil
	}
	return s.ListCloudAccounts(ctx, tenantID, offset, limit)
}

// UpdateCloudAccount 按 ID 部分更新云账号：遍历全部租户 store 查找后更新。
func (m *MultiSchemaStore) UpdateCloudAccount(ctx context.Context, id string, updates map[string]interface{}) error {
	for _, s := range m.allStores() {
		if acct, err := s.GetCloudAccount(ctx, id); err == nil && acct != nil {
			return s.UpdateCloudAccount(ctx, id, updates)
		}
	}
	return nil
}

// DeleteCloudAccount 按 ID 删除云账号：遍历全部租户 store 查找后删除。
func (m *MultiSchemaStore) DeleteCloudAccount(ctx context.Context, id string) error {
	for _, s := range m.allStores() {
		if acct, err := s.GetCloudAccount(ctx, id); err == nil && acct != nil {
			return s.DeleteCloudAccount(ctx, id)
		}
	}
	return nil
}

// SyncCloudAccount 回写同步状态：遍历全部租户 store 查找后回写。
func (m *MultiSchemaStore) SyncCloudAccount(ctx context.Context, id string, syncStatus string, discoveredCount int) error {
	for _, s := range m.allStores() {
		if acct, err := s.GetCloudAccount(ctx, id); err == nil && acct != nil {
			return s.SyncCloudAccount(ctx, id, syncStatus, discoveredCount)
		}
	}
	return nil
}
