// multi_schema_cloud_resource.go MultiSchemaStore 对 CloudResourceStore 的委托实现。
//
// 设计要点（与 multi_schema_p6.go 风格一致）：
//   - ListCloudResources 用 tenantID 路由；
//   - GetCloudResource 按 ID 遍历全部租户 store 查找；
//   - DeleteCloudResourcesByAccount 遍历全部租户 store（accountID 非 tenantID，不可按 schema 路由）。
package multischema

import (
	"context"
)

// CreateCloudResource 创建云资源：用 res.TenantID 路由。
func (m *MultiSchemaStore) CreateCloudResource(ctx context.Context, res *CloudResource) error {
	if res == nil {
		return nil
	}
	tenantID := res.TenantID
	if tenantID == "" {
		tenantID = "default"
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.CreateCloudResource(ctx, res)
}

// GetCloudResource 按 ID 返回单个云资源：遍历全部租户 store 查找。
func (m *MultiSchemaStore) GetCloudResource(ctx context.Context, id string) (*CloudResource, error) {
	for _, s := range m.allStores() {
		if res, err := s.GetCloudResource(ctx, id); err == nil && res != nil {
			return res, nil
		}
	}
	return nil, nil
}

// ListCloudResources 返回指定租户的云资源列表：用 tenantID 路由。
func (m *MultiSchemaStore) ListCloudResources(ctx context.Context, tenantID string, accountID string, resourceType string, offset, limit int) ([]*CloudResource, int, error) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, 0, nil
	}
	return s.ListCloudResources(ctx, tenantID, accountID, resourceType, offset, limit)
}

// DeleteCloudResourcesByAccount 删除指定云账号下全部云资源：遍历全部租户 store。
func (m *MultiSchemaStore) DeleteCloudResourcesByAccount(ctx context.Context, accountID string) error {
	if accountID == "" {
		return nil
	}
	for _, s := range m.allStores() {
		_ = s.DeleteCloudResourcesByAccount(ctx, accountID)
	}
	return nil
}
