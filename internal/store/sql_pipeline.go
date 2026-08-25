package store

// sql_pipeline.go 实现 SQLStore 的 PipelineStore 子接口（Phase 2 CI/CD 流水线，桩实现）。
//
// TODO(p2): 接入 MySQL 持久化（pipeline_templates 表 + pipeline_runs 表）。
// MVP 用内存桩实现，保证接口齐全 + go build 通过；DB 不可用时返回零值，
// 不 panic，与 SQLStore 其他桩方法风格一致（参考 sql_slo.go）。

// CreateTemplate 创建流水线模板（桩实现）。
func (s *SQLStore) CreateTemplate(tenantID string, t *PipelineTemplate) *PipelineTemplate {
	return nil
}

// GetTemplate 按 (tenantID, id) 返回单个模板（桩实现）。
func (s *SQLStore) GetTemplate(tenantID, id string) (*PipelineTemplate, bool) {
	return nil, false
}

// ListTemplates 返回指定租户的全部模板（桩实现）。
func (s *SQLStore) ListTemplates(tenantID string) []*PipelineTemplate {
	return []*PipelineTemplate{}
}

// DeleteTemplate 删除模板（桩实现）。
func (s *SQLStore) DeleteTemplate(tenantID, id string) bool {
	return false
}

// CreateRun 创建运行记录（桩实现）。
func (s *SQLStore) CreateRun(tenantID string, r *PipelineRun) *PipelineRun {
	return nil
}

// GetRun 按 (tenantID, id) 返回单条运行记录（桩实现）。
func (s *SQLStore) GetRun(tenantID, id string) (*PipelineRun, bool) {
	return nil, false
}

// ListRuns 返回运行记录列表（桩实现）。
func (s *SQLStore) ListRuns(tenantID string, templateID string) []*PipelineRun {
	return []*PipelineRun{}
}

// UpdateRun 更新运行记录（桩实现）。
func (s *SQLStore) UpdateRun(tenantID string, r *PipelineRun) (*PipelineRun, bool) {
	return nil, false
}
