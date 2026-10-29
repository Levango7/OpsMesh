// multi_schema_cloud_resource.go MultiSchemaStore 对 CloudResourceStore 的委托实现。
//
// 设计要点（与 multi_schema_p6.go 风格一致）：
//   - ListCloudResources 用 tenantID 路由；
//   - GetCloudResource 按 ID 遍历全部租户 store 查找；
//   - DeleteCloudResourcesByAccount 遍历全部租户 store（accountID 非 tenantID，不可按 schema 路由）。
package multischema

import (
	"context"

	"github.com/Levango7/OpsMesh/internal/store/storefail"
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
// 单个租户 store 删除失败不中断扇出（记录 storefail 后继续其余租户），最终仍返回 nil，
// 与本包其余跨租户聚合方法的宽松语义一致。
func (m *MultiSchemaStore) DeleteCloudResourcesByAccount(ctx context.Context, accountID string) error {
	if accountID == "" {
		return nil
	}
	for _, s := range m.allStores() {
		if err := s.DeleteCloudResourcesByAccount(ctx, accountID); err != nil {
			storefail.Record("[multi-schema] DeleteCloudResourcesByAccount 删除失败 (account=%s): %v", accountID, err)
		}
	}
	return nil
}

// UpdateCloudResource 更新云资源（部分字段）：遍历全部租户 store 查找并更新。
func (m *MultiSchemaStore) UpdateCloudResource(ctx context.Context, id string, updates map[string]interface{}) error {
	if id == "" {
		return nil
	}
	for _, s := range m.allStores() {
		if err := s.UpdateCloudResource(ctx, id, updates); err == nil {
			return nil // 找到并更新成功
		}
	}
	return nil // 未找到，不返回 error（宽松语义）
}

// DeleteCloudResource 删除单个云资源：遍历全部租户 store 查找并删除。
func (m *MultiSchemaStore) DeleteCloudResource(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	for _, s := range m.allStores() {
		if err := s.DeleteCloudResource(ctx, id); err == nil {
			return nil // 找到并删除成功
		}
	}
	return nil // 未找到，宽松语义不返回 error
}
