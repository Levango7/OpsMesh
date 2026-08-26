package store

// sql_pipeline.go 实现 SQLStore 的 PipelineStore 子接口（Phase 2 CI/CD 流水线）。
//
// 【未实现的桩】本文件全部方法未接入 MySQL 持久化，经 stub_guard.StubNotImplemented
// 显式告警（每 (domain,method) 首次必打、60s 限频），并按统一约定返回零值：
//   - Create 类返回 nil（不返回填充后的假对象）；
//   - Get/Update/Delete 返回 (nil, false) / false；List 返回非 nil 空切片。
//
// TODO(p2): 接入 MySQL 持久化（pipeline_templates 表 + pipeline_runs 表）。

// CreateTemplate 创建流水线模板（未实现的桩）。
func (s *SQLStore) CreateTemplate(tenantID string, t *PipelineTemplate) *PipelineTemplate {
	StubNotImplemented("pipeline", "CreateTemplate")
	return nil
}

// GetTemplate 按 (tenantID, id) 返回单个模板（未实现的桩）。
func (s *SQLStore) GetTemplate(tenantID, id string) (*PipelineTemplate, bool) {
	StubNotImplemented("pipeline", "GetTemplate")
	return nil, false
}

// ListTemplates 返回指定租户的全部模板（未实现的桩；返回非 nil 空切片防上层 range panic）。
func (s *SQLStore) ListTemplates(tenantID string) []*PipelineTemplate {
	StubNotImplemented("pipeline", "ListTemplates")
	return []*PipelineTemplate{}
}

// DeleteTemplate 删除模板（未实现的桩）。
func (s *SQLStore) DeleteTemplate(tenantID, id string) bool {
	StubNotImplemented("pipeline", "DeleteTemplate")
	return false
}

// CreateRun 创建运行记录（未实现的桩）。
func (s *SQLStore) CreateRun(tenantID string, r *PipelineRun) *PipelineRun {
	StubNotImplemented("pipeline", "CreateRun")
	return nil
}

// GetRun 按 (tenantID, id) 返回单条运行记录（未实现的桩）。
func (s *SQLStore) GetRun(tenantID, id string) (*PipelineRun, bool) {
	StubNotImplemented("pipeline", "GetRun")
	return nil, false
}

// ListRuns 返回运行记录列表（未实现的桩；返回非 nil 空切片防上层 range panic）。
func (s *SQLStore) ListRuns(tenantID string, templateID string) []*PipelineRun {
	StubNotImplemented("pipeline", "ListRuns")
	return []*PipelineRun{}
}

// UpdateRun 更新运行记录（未实现的桩）。
func (s *SQLStore) UpdateRun(tenantID string, r *PipelineRun) (*PipelineRun, bool) {
	StubNotImplemented("pipeline", "UpdateRun")
	return nil, false
}
