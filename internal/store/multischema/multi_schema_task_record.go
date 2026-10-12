// multi_schema_task_record.go MultiSchemaStore 对 TaskRecordStore 的实现（TD-60 task 域 R6）。
//
// 委派模式与 multi_schema_gateway_route.go 一致：记录是**租户级**资源，
// 用记录自带的 TenantID 路由到该租户的后端实现，不引入二次语义。
package multischema

// GetTaskRecord 按租户 + kind + ID 取记录。
func (m *MultiSchemaStore) GetTaskRecord(tenantID, kind, id string) *TaskRecord {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.GetTaskRecord(tenantID, kind, id)
}

// SaveTaskRecord 幂等 upsert（用记录自带的 TenantID 路由）。
func (m *MultiSchemaStore) SaveTaskRecord(rec *TaskRecord) error {
	if rec == nil {
		return nil
	}
	if rec.TenantID == "" {
		rec.TenantID = "default"
	}
	s, err := m.storeFor(rec.TenantID)
	if err != nil {
		return err
	}
	return s.SaveTaskRecord(rec)
}
