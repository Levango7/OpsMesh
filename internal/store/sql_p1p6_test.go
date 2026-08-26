// sql_p1p6_test.go 测试 P1-P6 全部 15 域 SQL 实现的扫描函数。
//
// 背景：P1-P6 共 15 个域的 SQL 持久化已全部实现（sql_p01.go ~ sql_p06.go
// 各 sql_*.go 文件真实 MySQL CRUD 落地），StubDomains 清单收敛为空。
// 本文件为全部 21 个扫描函数编写单元测试，验证从 *sql.Rows 扫描出领域对象的
// 字段映射与 JSON 列解析正确性。
//
// 测试分层（与 sql_p03_test.go 风格一致）：
//  1. Happy 路径：预置完整列值，断言字段映射正确；
//  2. EmptyJSON 路径（有 JSON 列者）：空 JSON 串应解析为零值，不 panic；
//  3. ScanError 路径：Scan 出错时返回 nil。
//
// mockRowScanner 已在 sql_test.go 中定义，此处直接复用。
// 无需 MySQL，始终运行；真实 MySQL 集成测试见各域 TestSQLStore_* 函数。
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// ============================================================================
// P1: SLO + Ticket（sql_slo.go / sql_ticket.go）
// ============================================================================

// scanSLO 列顺序：id, tenant_id, name, description, service_name, target, window,
// slis(JSON string), created_at, updated_at。

func TestScan_SLO_Happy(t *testing.T) {
	created := time.Date(2026, 1, 10, 8, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 1, 15, 12, 30, 0, 0, time.UTC)
	slisJSON, _ := json.Marshal([]SLI{{Name: "availability", Metric: "up", Target: 99.9, Operator: ">="}})
	row := &mockRowScanner{vals: []interface{}{
		"slo-1", "t1", "api-avail", "API 可用率 SLO", "api", 99.9, "30d",
		string(slisJSON), created, updated,
	}}
	s := scanSLO(row)
	if s == nil {
		t.Fatal("scanSLO 返回 nil")
	}
	if s.ID != "slo-1" || s.TenantID != "t1" || s.Name != "api-avail" {
		t.Fatalf("基础字段映射错误: %+v", s)
	}
	if s.ServiceName != "api" || s.Target != 99.9 || s.Window != "30d" {
		t.Fatalf("服务/目标/窗口错误: %+v", s)
	}
	if len(s.SLIs) != 1 || s.SLIs[0].Name != "availability" || s.SLIs[0].Target != 99.9 {
		t.Fatalf("SLIs 解析错误: %+v", s.SLIs)
	}
	if !s.CreatedAt.Equal(created) || !s.UpdatedAt.Equal(updated) {
		t.Fatalf("时间戳错误: created=%v updated=%v", s.CreatedAt, s.UpdatedAt)
	}
}

func TestScan_SLO_EmptySLIs(t *testing.T) {
	row := &mockRowScanner{vals: []interface{}{
		"slo-2", "t1", "empty-slo", "", "svc", 0.0, "",
		"", time.Time{}, time.Time{},
	}}
	s := scanSLO(row)
	if s == nil {
		t.Fatal("scanSLO 返回 nil")
	}
	if len(s.SLIs) != 0 {
		t.Fatalf("空 slis JSON 应解析为零值切片；got=%+v", s.SLIs)
	}
}

func TestScan_SLO_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("db connection lost")}
	if s := scanSLO(row); s != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// scanTicket 列顺序：id, tenant_id, title, description, status, priority, category,
// assignee_id, creator_id, related_device, related_task, tags(JSON string),
// created_at, updated_at, resolved_at(sql.NullTime)。

func TestScan_Ticket_Happy(t *testing.T) {
	created := time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 2, 5, 14, 0, 0, 0, time.UTC)
	resolved := time.Date(2026, 2, 5, 14, 0, 0, 0, time.UTC)
	tagsJSON, _ := json.Marshal([]string{"urgent", "network"})
	row := &mockRowScanner{vals: []interface{}{
		"ticket-1", "t1", "CPU 过高", "web-01 CPU 持续 >90%", "open", "high", "alert",
		"user-1", "user-2", "dev-1", "task-1", string(tagsJSON),
		created, updated, sql.NullTime{Time: resolved, Valid: true},
	}}
	tk := scanTicket(row)
	if tk == nil {
		t.Fatal("scanTicket 返回 nil")
	}
	if tk.ID != "ticket-1" || tk.TenantID != "t1" || tk.Title != "CPU 过高" {
		t.Fatalf("基础字段映射错误: %+v", tk)
	}
	if tk.Status != "open" || tk.Priority != "high" || tk.Category != "alert" {
		t.Fatalf("状态/优先级/分类错误: %+v", tk)
	}
	if tk.AssigneeID != "user-1" || tk.CreatorID != "user-2" {
		t.Fatalf("指派人/创建人错误: %+v", tk)
	}
	if tk.RelatedDevice != "dev-1" || tk.RelatedTask != "task-1" {
		t.Fatalf("关联设备/任务错误: %+v", tk)
	}
	if len(tk.Tags) != 2 || tk.Tags[0] != "urgent" || tk.Tags[1] != "network" {
		t.Fatalf("Tags 解析错误: %+v", tk.Tags)
	}
	if !tk.CreatedAt.Equal(created) || !tk.UpdatedAt.Equal(updated) {
		t.Fatalf("时间戳错误: created=%v updated=%v", tk.CreatedAt, tk.UpdatedAt)
	}
	if tk.ResolvedAt == nil || !tk.ResolvedAt.Equal(resolved) {
		t.Fatalf("ResolvedAt 错误: got=%v want=%v", tk.ResolvedAt, resolved)
	}
}

func TestScan_Ticket_NullResolvedAt(t *testing.T) {
	row := &mockRowScanner{vals: []interface{}{
		"ticket-2", "t1", "未解决", "", "open", "low", "",
		"", "", "", "", "", time.Time{}, time.Time{}, sql.NullTime{Valid: false},
	}}
	tk := scanTicket(row)
	if tk == nil {
		t.Fatal("scanTicket 返回 nil")
	}
	if tk.ResolvedAt != nil {
		t.Fatalf("NullTime Invalid 时 ResolvedAt 应为 nil；got=%v", tk.ResolvedAt)
	}
	if len(tk.Tags) != 0 {
		t.Fatalf("空 tags 应解析为零值；got=%+v", tk.Tags)
	}
}

func TestScan_Ticket_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("column mismatch")}
	if tk := scanTicket(row); tk != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// ============================================================================
// P2: ArgoCD + Pipeline + Traffic（sql_argocd.go / sql_pipeline.go / sql_traffic.go）
// ============================================================================

// scanArgoCDApp 列顺序：id, tenant_id, name, namespace, repo_url, path, target_revision,
// cluster_url, sync_policy, status, health_status, created_at, updated_at。

func TestScan_ArgoCDApp_Happy(t *testing.T) {
	created := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 3, 10, 16, 0, 0, 0, time.UTC)
	row := &mockRowScanner{vals: []interface{}{
		"argocd-1", "t1", "guestbook", "default", "https://git.example.com/x.git",
		"manifests", "main", "https://10.0.0.1:6443", "auto", "synced", "healthy",
		created, updated,
	}}
	a := scanArgoCDApp(row)
	if a == nil {
		t.Fatal("scanArgoCDApp 返回 nil")
	}
	if a.ID != "argocd-1" || a.TenantID != "t1" || a.Name != "guestbook" {
		t.Fatalf("基础字段映射错误: %+v", a)
	}
	if a.Namespace != "default" || a.RepoURL != "https://git.example.com/x.git" {
		t.Fatalf("命名空间/仓库错误: %+v", a)
	}
	if a.Path != "manifests" || a.TargetRevision != "main" {
		t.Fatalf("路径/目标版本错误: %+v", a)
	}
	if a.ClusterURL != "https://10.0.0.1:6443" || a.SyncPolicy != "auto" {
		t.Fatalf("集群/同步策略错误: %+v", a)
	}
	if a.Status != "synced" || a.HealthStatus != "healthy" {
		t.Fatalf("状态/健康状态错误: %+v", a)
	}
	if !a.CreatedAt.Equal(created) || !a.UpdatedAt.Equal(updated) {
		t.Fatalf("时间戳错误: created=%v updated=%v", a.CreatedAt, a.UpdatedAt)
	}
}

func TestScan_ArgoCDApp_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("db error")}
	if a := scanArgoCDApp(row); a != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// scanPipelineTemplate 列顺序：id, tenant_id, name, description, type, yaml,
// parameters(JSON []byte), created_at, updated_at。

func TestScan_PipelineTemplate_Happy(t *testing.T) {
	created := time.Date(2026, 3, 5, 8, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 3, 6, 9, 0, 0, 0, time.UTC)
	paramsJSON, _ := json.Marshal([]PipelineParam{{Name: "branch", Default: "main", Required: true}})
	row := &mockRowScanner{vals: []interface{}{
		"pipeline-1", "t1", "build", "构建流水线", "tekton", "apiVersion: v1",
		paramsJSON, created, updated,
	}}
	tpl := scanPipelineTemplate(row)
	if tpl == nil {
		t.Fatal("scanPipelineTemplate 返回 nil")
	}
	if tpl.ID != "pipeline-1" || tpl.TenantID != "t1" || tpl.Name != "build" {
		t.Fatalf("基础字段映射错误: %+v", tpl)
	}
	if tpl.Type != "tekton" || tpl.YAML != "apiVersion: v1" {
		t.Fatalf("类型/YAML 错误: %+v", tpl)
	}
	if len(tpl.Parameters) != 1 || tpl.Parameters[0].Name != "branch" || !tpl.Parameters[0].Required {
		t.Fatalf("Parameters 解析错误: %+v", tpl.Parameters)
	}
	if !tpl.CreatedAt.Equal(created) || !tpl.UpdatedAt.Equal(updated) {
		t.Fatalf("时间戳错误: created=%v updated=%v", tpl.CreatedAt, tpl.UpdatedAt)
	}
}

func TestScan_PipelineTemplate_EmptyParams(t *testing.T) {
	row := &mockRowScanner{vals: []interface{}{
		"pipeline-2", "t1", "empty", "", "jenkins", "",
		[]byte{}, time.Time{}, time.Time{},
	}}
	tpl := scanPipelineTemplate(row)
	if tpl == nil {
		t.Fatal("scanPipelineTemplate 返回 nil")
	}
	if len(tpl.Parameters) != 0 {
		t.Fatalf("空 parameters 应解析为零值；got=%+v", tpl.Parameters)
	}
}

func TestScan_PipelineTemplate_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("db error")}
	if tpl := scanPipelineTemplate(row); tpl != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// scanPipelineRun 列顺序：id, tenant_id, template_id, template_name, status,
// parameters(JSON []byte), logs, started_at(sql.NullTime), finished_at(sql.NullTime),
// created_at。

func TestScan_PipelineRun_Happy(t *testing.T) {
	started := time.Date(2026, 3, 7, 10, 0, 0, 0, time.UTC)
	finished := time.Date(2026, 3, 7, 10, 5, 0, 0, time.UTC)
	created := time.Date(2026, 3, 7, 10, 0, 0, 0, time.UTC)
	paramsJSON, _ := json.Marshal(map[string]string{"branch": "feature/x"})
	row := &mockRowScanner{vals: []interface{}{
		"run-1", "t1", "pipeline-1", "build", "succeeded",
		paramsJSON, "Build completed", sql.NullTime{Time: started, Valid: true},
		sql.NullTime{Time: finished, Valid: true}, created,
	}}
	r := scanPipelineRun(row)
	if r == nil {
		t.Fatal("scanPipelineRun 返回 nil")
	}
	if r.ID != "run-1" || r.TenantID != "t1" || r.TemplateID != "pipeline-1" {
		t.Fatalf("基础字段映射错误: %+v", r)
	}
	if r.TemplateName != "build" || r.Status != "succeeded" {
		t.Fatalf("模板名/状态错误: %+v", r)
	}
	if r.Parameters["branch"] != "feature/x" {
		t.Fatalf("Parameters 解析错误: %+v", r.Parameters)
	}
	if r.Logs != "Build completed" {
		t.Fatalf("Logs 错误: %q", r.Logs)
	}
	if r.StartedAt == nil || !r.StartedAt.Equal(started) {
		t.Fatalf("StartedAt 错误: got=%v want=%v", r.StartedAt, started)
	}
	if r.FinishedAt == nil || !r.FinishedAt.Equal(finished) {
		t.Fatalf("FinishedAt 错误: got=%v want=%v", r.FinishedAt, finished)
	}
	if !r.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt 错误: got=%v want=%v", r.CreatedAt, created)
	}
}

func TestScan_PipelineRun_NullTimes(t *testing.T) {
	row := &mockRowScanner{vals: []interface{}{
		"run-2", "t1", "pipeline-1", "build", "pending",
		[]byte{}, "", sql.NullTime{Valid: false}, sql.NullTime{Valid: false}, time.Time{},
	}}
	r := scanPipelineRun(row)
	if r == nil {
		t.Fatal("scanPipelineRun 返回 nil")
	}
	if r.StartedAt != nil {
		t.Fatalf("NullTime Invalid 时 StartedAt 应为 nil；got=%v", r.StartedAt)
	}
	if r.FinishedAt != nil {
		t.Fatalf("NullTime Invalid 时 FinishedAt 应为 nil；got=%v", r.FinishedAt)
	}
}

func TestScan_PipelineRun_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("db error")}
	if r := scanPipelineRun(row); r != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// scanTrafficPolicy 列顺序：id, tenant_id, name, service_name, type, canary_weights(JSON []byte),
// mirror_percent, timeout, retries, retry_timeout, max_conns, max_requests, status,
// created_at, updated_at。

func TestScan_TrafficPolicy_Happy(t *testing.T) {
	created := time.Date(2026, 3, 8, 8, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	canaryJSON, _ := json.Marshal(map[string]int{"v1": 90, "v2": 10})
	row := &mockRowScanner{vals: []interface{}{
		"traffic-1", "t1", "canary-v2", "web", "canary", canaryJSON,
		0, "5s", 3, "2s", 100, 50, "active", created, updated,
	}}
	p := scanTrafficPolicy(row)
	if p == nil {
		t.Fatal("scanTrafficPolicy 返回 nil")
	}
	if p.ID != "traffic-1" || p.TenantID != "t1" || p.Name != "canary-v2" {
		t.Fatalf("基础字段映射错误: %+v", p)
	}
	if p.ServiceName != "web" || p.Type != "canary" {
		t.Fatalf("服务/类型错误: %+v", p)
	}
	if p.CanaryWeights["v1"] != 90 || p.CanaryWeights["v2"] != 10 {
		t.Fatalf("CanaryWeights 解析错误: %+v", p.CanaryWeights)
	}
	if p.MirrorPercent != 0 || p.Timeout != "5s" || p.Retries != 3 {
		t.Fatalf("镜像/超时/重试错误: %+v", p)
	}
	if p.RetryTimeout != "2s" || p.MaxConns != 100 || p.MaxRequests != 50 {
		t.Fatalf("重试超时/最大连接/最大请求错误: %+v", p)
	}
	if p.Status != "active" {
		t.Fatalf("状态错误: %q", p.Status)
	}
	if !p.CreatedAt.Equal(created) || !p.UpdatedAt.Equal(updated) {
		t.Fatalf("时间戳错误: created=%v updated=%v", p.CreatedAt, p.UpdatedAt)
	}
}

func TestScan_TrafficPolicy_EmptyCanary(t *testing.T) {
	row := &mockRowScanner{vals: []interface{}{
		"traffic-2", "t1", "timeout-policy", "api", "timeout", []byte{},
		0, "10s", 0, "", 0, 0, "inactive", time.Time{}, time.Time{},
	}}
	p := scanTrafficPolicy(row)
	if p == nil {
		t.Fatal("scanTrafficPolicy 返回 nil")
	}
	if len(p.CanaryWeights) != 0 {
		t.Fatalf("空 canary_weights 应解析为零值；got=%+v", p.CanaryWeights)
	}
}

func TestScan_TrafficPolicy_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("db error")}
	if p := scanTrafficPolicy(row); p != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// ============================================================================
// P3: Backup + Compliance（sql_backup.go / sql_compliance.go）
// ============================================================================

// scanBackupRecord 列顺序：id, tenant_id, type, status, size, path, created_at。

func TestScan_BackupRecord_Happy(t *testing.T) {
	created := time.Date(2026, 4, 1, 2, 0, 0, 0, time.UTC)
	row := &mockRowScanner{vals: []interface{}{
		"backup-1", "t1", "full", "completed", int64(1048576), "/backup/full.tar.gz", created,
	}}
	b := scanBackupRecord(row)
	if b == nil {
		t.Fatal("scanBackupRecord 返回 nil")
	}
	if b.ID != "backup-1" || b.TenantID != "t1" || b.Type != "full" {
		t.Fatalf("基础字段映射错误: %+v", b)
	}
	if b.Status != "completed" || b.Size != 1048576 {
		t.Fatalf("状态/大小错误: %+v", b)
	}
	if b.Path != "/backup/full.tar.gz" {
		t.Fatalf("路径错误: %q", b.Path)
	}
	if !b.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt 错误: got=%v want=%v", b.CreatedAt, created)
	}
}

func TestScan_BackupRecord_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("db error")}
	if b := scanBackupRecord(row); b != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// scanComplianceReport 列顺序：id, tenant_id, device_id, results(JSON []byte),
// score, created_at。

func TestScan_ComplianceReport_Happy(t *testing.T) {
	created := time.Date(2026, 4, 5, 14, 0, 0, 0, time.UTC)
	resultsJSON, _ := json.Marshal([]ComplianceResult{
		{RuleID: "cis-1.1", Passed: true, Output: "ok"},
		{RuleID: "cis-1.2", Passed: false, Output: "fail"},
	})
	row := &mockRowScanner{vals: []interface{}{
		"compliance-1", "t1", "dev-1", resultsJSON, 85, created,
	}}
	r := scanComplianceReport(row)
	if r == nil {
		t.Fatal("scanComplianceReport 返回 nil")
	}
	if r.ID != "compliance-1" || r.TenantID != "t1" || r.DeviceID != "dev-1" {
		t.Fatalf("基础字段映射错误: %+v", r)
	}
	if r.Score != 85 {
		t.Fatalf("Score 错误: got=%d want=85", r.Score)
	}
	if len(r.Results) != 2 || r.Results[0].RuleID != "cis-1.1" || !r.Results[0].Passed {
		t.Fatalf("Results 解析错误: %+v", r.Results)
	}
	if r.Results[1].RuleID != "cis-1.2" || r.Results[1].Passed {
		t.Fatalf("Results[1] 解析错误: %+v", r.Results[1])
	}
	if !r.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt 错误: got=%v want=%v", r.CreatedAt, created)
	}
}

func TestScan_ComplianceReport_EmptyResults(t *testing.T) {
	row := &mockRowScanner{vals: []interface{}{
		"compliance-2", "t1", "dev-2", []byte{}, 100, time.Time{},
	}}
	r := scanComplianceReport(row)
	if r == nil {
		t.Fatal("scanComplianceReport 返回 nil")
	}
	if len(r.Results) != 0 {
		t.Fatalf("空 results 应解析为零值；got=%+v", r.Results)
	}
}

func TestScan_ComplianceReport_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("db error")}
	if r := scanComplianceReport(row); r != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// ============================================================================
// P4: Automation + Network（sql_automation.go / sql_network.go）
// ============================================================================

// scanAutomationRule 列顺序：id, tenant_id, name, description, trigger_type,
// trigger_params(JSON []byte), actions(JSON []byte), enabled(int),
// created_at, updated_at。

func TestScan_AutomationRule_Happy(t *testing.T) {
	created := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC)
	triggerJSON, _ := json.Marshal(map[string]string{"metric": "cpu", "op": ">", "threshold": "90"})
	actionsJSON, _ := json.Marshal([]AutomationAction{
		{Type: "restart", Params: map[string]string{"host": "web-01"}},
	})
	row := &mockRowScanner{vals: []interface{}{
		"rule-1", "t1", "restart-on-cpu", "CPU 超阈值重启", "metric_threshold",
		triggerJSON, actionsJSON, 1, created, updated,
	}}
	r := scanAutomationRule(row)
	if r == nil {
		t.Fatal("scanAutomationRule 返回 nil")
	}
	if r.ID != "rule-1" || r.TenantID != "t1" || r.Name != "restart-on-cpu" {
		t.Fatalf("基础字段映射错误: %+v", r)
	}
	if r.TriggerType != "metric_threshold" {
		t.Fatalf("TriggerType 错误: %q", r.TriggerType)
	}
	if r.TriggerParams["metric"] != "cpu" || r.TriggerParams["threshold"] != "90" {
		t.Fatalf("TriggerParams 解析错误: %+v", r.TriggerParams)
	}
	if len(r.Actions) != 1 || r.Actions[0].Type != "restart" {
		t.Fatalf("Actions 解析错误: %+v", r.Actions)
	}
	if r.Actions[0].Params["host"] != "web-01" {
		t.Fatalf("Actions[0].Params 解析错误: %+v", r.Actions[0].Params)
	}
	if !r.Enabled {
		t.Fatal("Enabled 应为 true")
	}
	if !r.CreatedAt.Equal(created) || !r.UpdatedAt.Equal(updated) {
		t.Fatalf("时间戳错误: created=%v updated=%v", r.CreatedAt, r.UpdatedAt)
	}
}

func TestScan_AutomationRule_Disabled(t *testing.T) {
	row := &mockRowScanner{vals: []interface{}{
		"rule-2", "t1", "disabled-rule", "", "schedule",
		[]byte{}, []byte{}, 0, time.Time{}, time.Time{},
	}}
	r := scanAutomationRule(row)
	if r == nil {
		t.Fatal("scanAutomationRule 返回 nil")
	}
	if r.Enabled {
		t.Fatal("Enabled=0 时应为 false")
	}
	if r.TriggerParams != nil {
		t.Fatalf("空 trigger_params 应解析为 nil；got=%+v", r.TriggerParams)
	}
}

func TestScan_AutomationRule_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("db error")}
	if r := scanAutomationRule(row); r != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// scanAutomationExecution 列顺序：id, tenant_id, rule_id, rule_name, status, detail,
// started_at, ended_at(sql.NullTime)。

func TestScan_AutomationExecution_Happy(t *testing.T) {
	started := time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)
	ended := time.Date(2026, 5, 3, 10, 0, 5, 0, time.UTC)
	row := &mockRowScanner{vals: []interface{}{
		"exec-1", "t1", "rule-1", "restart-on-cpu", "succeeded", "restarted web-01",
		started, sql.NullTime{Time: ended, Valid: true},
	}}
	e := scanAutomationExecution(row)
	if e == nil {
		t.Fatal("scanAutomationExecution 返回 nil")
	}
	if e.ID != "exec-1" || e.TenantID != "t1" || e.RuleID != "rule-1" {
		t.Fatalf("基础字段映射错误: %+v", e)
	}
	if e.RuleName != "restart-on-cpu" || e.Status != "succeeded" {
		t.Fatalf("规则名/状态错误: %+v", e)
	}
	if e.Detail != "restarted web-01" {
		t.Fatalf("Detail 错误: %q", e.Detail)
	}
	if !e.StartedAt.Equal(started) {
		t.Fatalf("StartedAt 错误: got=%v want=%v", e.StartedAt, started)
	}
	if e.EndedAt == nil || !e.EndedAt.Equal(ended) {
		t.Fatalf("EndedAt 错误: got=%v want=%v", e.EndedAt, ended)
	}
}

func TestScan_AutomationExecution_NullEndedAt(t *testing.T) {
	row := &mockRowScanner{vals: []interface{}{
		"exec-2", "t1", "rule-1", "restart-on-cpu", "running", "",
		time.Time{}, sql.NullTime{Valid: false},
	}}
	e := scanAutomationExecution(row)
	if e == nil {
		t.Fatal("scanAutomationExecution 返回 nil")
	}
	if e.EndedAt != nil {
		t.Fatalf("NullTime Invalid 时 EndedAt 应为 nil；got=%v", e.EndedAt)
	}
}

func TestScan_AutomationExecution_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("db error")}
	if e := scanAutomationExecution(row); e != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// scanNetworkDevice 列顺序：id, tenant_id, name, type, vendor, model, ip, mask, mac,
// location, snmp_community, status, config, created_at, updated_at。

func TestScan_NetworkDevice_Happy(t *testing.T) {
	created := time.Date(2026, 5, 5, 8, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 5, 6, 9, 0, 0, 0, time.UTC)
	row := &mockRowScanner{vals: []interface{}{
		"netdev-1", "t1", "sw-core-01", "switch", "huawei", "S5700",
		"10.0.0.2", "255.255.255.0", "aa:bb:cc:dd:ee:ff", "机房A",
		"public", "online", "vlan 10; stp enable", created, updated,
	}}
	d := scanNetworkDevice(row)
	if d == nil {
		t.Fatal("scanNetworkDevice 返回 nil")
	}
	if d.ID != "netdev-1" || d.TenantID != "t1" || d.Name != "sw-core-01" {
		t.Fatalf("基础字段映射错误: %+v", d)
	}
	if d.Type != "switch" || d.Vendor != "huawei" || d.Model != "S5700" {
		t.Fatalf("类型/厂商/型号错误: %+v", d)
	}
	if d.IP != "10.0.0.2" || d.Mask != "255.255.255.0" || d.Mac != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("IP/掩码/MAC 错误: %+v", d)
	}
	if d.Location != "机房A" || d.SnmpCommunity != "public" {
		t.Fatalf("位置/SNMP 社区错误: %+v", d)
	}
	if d.Status != "online" || d.Config != "vlan 10; stp enable" {
		t.Fatalf("状态/配置错误: %+v", d)
	}
	if !d.CreatedAt.Equal(created) || !d.UpdatedAt.Equal(updated) {
		t.Fatalf("时间戳错误: created=%v updated=%v", d.CreatedAt, d.UpdatedAt)
	}
}

func TestScan_NetworkDevice_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("db error")}
	if d := scanNetworkDevice(row); d != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// scanNetworkMetrics 列顺序：device_id, tenant_id, timestamp, cpu_usage,
// memory_usage, temperature, uptime。

func TestScan_NetworkMetrics_Happy(t *testing.T) {
	ts := time.Date(2026, 5, 7, 14, 30, 0, 0, time.UTC)
	row := &mockRowScanner{vals: []interface{}{
		"netdev-1", "t1", ts, 45.5, 62.3, 38.0, int64(86400),
	}}
	m := scanNetworkMetrics(row)
	if m == nil {
		t.Fatal("scanNetworkMetrics 返回 nil")
	}
	if m.DeviceID != "netdev-1" || m.TenantID != "t1" {
		t.Fatalf("基础字段映射错误: %+v", m)
	}
	if !m.Timestamp.Equal(ts) {
		t.Fatalf("Timestamp 错误: got=%v want=%v", m.Timestamp, ts)
	}
	if m.CPUUsage != 45.5 || m.MemoryUsage != 62.3 || m.Temperature != 38.0 {
		t.Fatalf("CPU/内存/温度错误: %+v", m)
	}
	if m.Uptime != 86400 {
		t.Fatalf("Uptime 错误: got=%d want=86400", m.Uptime)
	}
}

func TestScan_NetworkMetrics_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("db error")}
	if m := scanNetworkMetrics(row); m != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// ============================================================================
// P5: Script + Webhook（sql_script.go / sql_webhook.go）
// ============================================================================

// scanScript 列顺序：id, tenant_id, name, language, content, params, timeout_sec,
// enabled(int), created_at, updated_at。

func TestScan_Script_Happy(t *testing.T) {
	created := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	row := &mockRowScanner{vals: []interface{}{
		"script-1", "t1", "check-disk", "shell", "#!/bin/sh\ndf -h", "",
		300, 1, created, updated,
	}}
	sc := scanScript(row)
	if sc == nil {
		t.Fatal("scanScript 返回 nil")
	}
	if sc.ID != "script-1" || sc.TenantID != "t1" || sc.Name != "check-disk" {
		t.Fatalf("基础字段映射错误: %+v", sc)
	}
	if sc.Language != "shell" || sc.Content != "#!/bin/sh\ndf -h" {
		t.Fatalf("语言/内容错误: %+v", sc)
	}
	if sc.Params != "" || sc.TimeoutSec != 300 {
		t.Fatalf("参数/超时错误: %+v", sc)
	}
	if !sc.Enabled {
		t.Fatal("Enabled=1 时应为 true")
	}
	if !sc.CreatedAt.Equal(created) || !sc.UpdatedAt.Equal(updated) {
		t.Fatalf("时间戳错误: created=%v updated=%v", sc.CreatedAt, sc.UpdatedAt)
	}
}

func TestScan_Script_Disabled(t *testing.T) {
	row := &mockRowScanner{vals: []interface{}{
		"script-2", "t1", "disabled", "python", "", "",
		0, 0, time.Time{}, time.Time{},
	}}
	sc := scanScript(row)
	if sc == nil {
		t.Fatal("scanScript 返回 nil")
	}
	if sc.Enabled {
		t.Fatal("Enabled=0 时应为 false")
	}
}

func TestScan_Script_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("db error")}
	if sc := scanScript(row); sc != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// scanScriptExecution 列顺序：id, tenant_id, script_id, device_id, status, stdout,
// stderr, started_at, finished_at(sql.NullTime)。

func TestScan_ScriptExecution_Happy(t *testing.T) {
	started := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
	finished := time.Date(2026, 6, 3, 10, 0, 2, 0, time.UTC)
	row := &mockRowScanner{vals: []interface{}{
		"script-exec-1", "t1", "script-1", "dev-1", "succeeded",
		"Filesystem  Size  Used  Avail\n/dev  10G  5G  5G", "",
		started, sql.NullTime{Time: finished, Valid: true},
	}}
	e := scanScriptExecution(row)
	if e == nil {
		t.Fatal("scanScriptExecution 返回 nil")
	}
	if e.ID != "script-exec-1" || e.TenantID != "t1" || e.ScriptID != "script-1" {
		t.Fatalf("基础字段映射错误: %+v", e)
	}
	if e.DeviceID != "dev-1" || e.Status != "succeeded" {
		t.Fatalf("设备/状态错误: %+v", e)
	}
	if e.Stdout == "" || e.Stderr != "" {
		t.Fatalf("Stdout/Stderr 错误: %+v", e)
	}
	if !e.StartedAt.Equal(started) {
		t.Fatalf("StartedAt 错误: got=%v want=%v", e.StartedAt, started)
	}
	if e.FinishedAt == nil || !e.FinishedAt.Equal(finished) {
		t.Fatalf("FinishedAt 错误: got=%v want=%v", e.FinishedAt, finished)
	}
}

func TestScan_ScriptExecution_NullFinishedAt(t *testing.T) {
	row := &mockRowScanner{vals: []interface{}{
		"script-exec-2", "t1", "script-1", "dev-1", "running",
		"", "", time.Time{}, sql.NullTime{Valid: false},
	}}
	e := scanScriptExecution(row)
	if e == nil {
		t.Fatal("scanScriptExecution 返回 nil")
	}
	if e.FinishedAt != nil {
		t.Fatalf("NullTime Invalid 时 FinishedAt 应为 nil；got=%v", e.FinishedAt)
	}
}

func TestScan_ScriptExecution_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("db error")}
	if e := scanScriptExecution(row); e != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// scanWebhook 列顺序：id, tenant_id, name, url, events(JSON string), headers(JSON string),
// body_template, enabled(int), retry_count, retry_interval_sec, created_at, updated_at。

func TestScan_Webhook_Happy(t *testing.T) {
	created := time.Date(2026, 6, 5, 8, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 6, 6, 9, 0, 0, 0, time.UTC)
	eventsJSON, _ := json.Marshal([]string{"alert.created", "ticket.closed"})
	headersJSON, _ := json.Marshal(map[string]string{"Authorization": "Bearer tok-1"})
	row := &mockRowScanner{vals: []interface{}{
		"webhook-1", "t1", "notify-slack", "https://hooks.slack.com/x",
		string(eventsJSON), string(headersJSON), `{"text":"{{.event}}"}`,
		1, 3, 10, created, updated,
	}}
	wh := scanWebhook(row)
	if wh == nil {
		t.Fatal("scanWebhook 返回 nil")
	}
	if wh.ID != "webhook-1" || wh.TenantID != "t1" || wh.Name != "notify-slack" {
		t.Fatalf("基础字段映射错误: %+v", wh)
	}
	if wh.URL != "https://hooks.slack.com/x" {
		t.Fatalf("URL 错误: %q", wh.URL)
	}
	if len(wh.Events) != 2 || wh.Events[0] != "alert.created" || wh.Events[1] != "ticket.closed" {
		t.Fatalf("Events 解析错误: %+v", wh.Events)
	}
	if wh.Headers["Authorization"] != "Bearer tok-1" {
		t.Fatalf("Headers 解析错误: %+v", wh.Headers)
	}
	if wh.BodyTemplate != `{"text":"{{.event}}"}` {
		t.Fatalf("BodyTemplate 错误: %q", wh.BodyTemplate)
	}
	if !wh.Enabled || wh.RetryCount != 3 || wh.RetryIntervalSec != 10 {
		t.Fatalf("启用/重试次数/重试间隔错误: %+v", wh)
	}
	if !wh.CreatedAt.Equal(created) || !wh.UpdatedAt.Equal(updated) {
		t.Fatalf("时间戳错误: created=%v updated=%v", wh.CreatedAt, wh.UpdatedAt)
	}
}

func TestScan_Webhook_EmptyJSON(t *testing.T) {
	row := &mockRowScanner{vals: []interface{}{
		"webhook-2", "t1", "empty", "https://example.com",
		"", "", "", 0, 0, 0, time.Time{}, time.Time{},
	}}
	wh := scanWebhook(row)
	if wh == nil {
		t.Fatal("scanWebhook 返回 nil")
	}
	if len(wh.Events) != 0 {
		t.Fatalf("空 events 应解析为零值；got=%+v", wh.Events)
	}
	if len(wh.Headers) != 0 {
		t.Fatalf("空 headers 应解析为零值；got=%+v", wh.Headers)
	}
}

func TestScan_Webhook_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("db error")}
	if wh := scanWebhook(row); wh != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// scanWebhookDelivery 列顺序：id, tenant_id, webhook_id, event, payload, status_code,
// response, error, delivered_at。

func TestScan_WebhookDelivery_Happy(t *testing.T) {
	delivered := time.Date(2026, 6, 7, 14, 0, 0, 0, time.UTC)
	row := &mockRowScanner{vals: []interface{}{
		"wh-delivery-1", "t1", "webhook-1", "alert.created", `{"alert":"cpu"}`,
		200, `{"ok":true}`, "", delivered,
	}}
	d := scanWebhookDelivery(row)
	if d == nil {
		t.Fatal("scanWebhookDelivery 返回 nil")
	}
	if d.ID != "wh-delivery-1" || d.TenantID != "t1" || d.WebhookID != "webhook-1" {
		t.Fatalf("基础字段映射错误: %+v", d)
	}
	if d.Event != "alert.created" || d.Payload != `{"alert":"cpu"}` {
		t.Fatalf("事件/载荷错误: %+v", d)
	}
	if d.StatusCode != 200 || d.Response != `{"ok":true}` || d.Error != "" {
		t.Fatalf("状态码/响应/错误错误: %+v", d)
	}
	if !d.DeliveredAt.Equal(delivered) {
		t.Fatalf("DeliveredAt 错误: got=%v want=%v", d.DeliveredAt, delivered)
	}
}

func TestScan_WebhookDelivery_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("db error")}
	if d := scanWebhookDelivery(row); d != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// ============================================================================
// P6: Tenant + APIKey + Plugin + Billing（sql_tenant.go / sql_apikey.go /
// sql_plugin.go / sql_billing.go）
// ============================================================================

// scanTenant 列顺序：id, name, display_name, status, quota(JSON string),
// usage(JSON string), created_at, updated_at。

func TestScan_Tenant_Happy(t *testing.T) {
	created := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC)
	quotaJSON, _ := json.Marshal(TenantQuota{MaxDevices: 100, MaxTasks: 500, MaxAlerts: 50})
	usageJSON, _ := json.Marshal(ResourceUsage{Devices: 10, Tasks: 20})
	row := &mockRowScanner{vals: []interface{}{
		"tenant-1", "acme", "ACME Corp", TenantStatus("active"),
		string(quotaJSON), string(usageJSON), created, updated,
	}}
	tt := scanTenant(row)
	if tt == nil {
		t.Fatal("scanTenant 返回 nil")
	}
	if tt.ID != "tenant-1" || tt.Name != "acme" || tt.DisplayName != "ACME Corp" {
		t.Fatalf("基础字段映射错误: %+v", tt)
	}
	if tt.Status != TenantStatus("active") {
		t.Fatalf("Status 错误: got=%q want=active", tt.Status)
	}
	if tt.Quota.MaxDevices != 100 || tt.Quota.MaxTasks != 500 || tt.Quota.MaxAlerts != 50 {
		t.Fatalf("Quota 解析错误: %+v", tt.Quota)
	}
	if tt.Usage.Devices != 10 || tt.Usage.Tasks != 20 {
		t.Fatalf("Usage 解析错误: %+v", tt.Usage)
	}
	if !tt.CreatedAt.Equal(created) || !tt.UpdatedAt.Equal(updated) {
		t.Fatalf("时间戳错误: created=%v updated=%v", tt.CreatedAt, tt.UpdatedAt)
	}
}

func TestScan_Tenant_EmptyQuotaUsage(t *testing.T) {
	row := &mockRowScanner{vals: []interface{}{
		"tenant-2", "beta", "Beta", TenantStatus("suspended"),
		"", "", time.Time{}, time.Time{},
	}}
	tt := scanTenant(row)
	if tt == nil {
		t.Fatal("scanTenant 返回 nil")
	}
	// 空 JSON 串跳过 Unmarshal，Quota/Usage 保持零值。
	if tt.Quota.MaxDevices != 0 {
		t.Fatalf("空 quota 应解析为零值；got=%+v", tt.Quota)
	}
	if tt.Usage.Devices != 0 {
		t.Fatalf("空 usage 应解析为零值；got=%+v", tt.Usage)
	}
}

func TestScan_Tenant_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("db error")}
	if tt := scanTenant(row); tt != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// scanAPIKey 列顺序：id, tenant_id, name, key_hash, scopes(JSON string),
// rate_limit_per_sec, expires_at(sql.NullTime), last_used_at(sql.NullTime),
// enabled(int), created_at。

func TestScan_APIKey_Happy(t *testing.T) {
	created := time.Date(2026, 7, 3, 8, 0, 0, 0, time.UTC)
	expires := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	lastUsed := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	scopesJSON, _ := json.Marshal([]string{"device:read", "task:write"})
	row := &mockRowScanner{vals: []interface{}{
		"apikey-1", "t1", "ci-key", "sha256:abc123", string(scopesJSON),
		100, sql.NullTime{Time: expires, Valid: true},
		sql.NullTime{Time: lastUsed, Valid: true}, 1, created,
	}}
	k := scanAPIKey(row)
	if k == nil {
		t.Fatal("scanAPIKey 返回 nil")
	}
	if k.ID != "apikey-1" || k.TenantID != "t1" || k.Name != "ci-key" {
		t.Fatalf("基础字段映射错误: %+v", k)
	}
	if k.Key != "sha256:abc123" {
		t.Fatalf("Key hash 错误: %q", k.Key)
	}
	if len(k.Scopes) != 2 || k.Scopes[0] != "device:read" || k.Scopes[1] != "task:write" {
		t.Fatalf("Scopes 解析错误: %+v", k.Scopes)
	}
	if k.RateLimitPerSec != 100 {
		t.Fatalf("RateLimitPerSec 错误: got=%d want=100", k.RateLimitPerSec)
	}
	if !k.ExpiresAt.Equal(expires) {
		t.Fatalf("ExpiresAt 错误: got=%v want=%v", k.ExpiresAt, expires)
	}
	if !k.LastUsedAt.Equal(lastUsed) {
		t.Fatalf("LastUsedAt 错误: got=%v want=%v", k.LastUsedAt, lastUsed)
	}
	if !k.Enabled {
		t.Fatal("Enabled=1 时应为 true")
	}
	if !k.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt 错误: got=%v want=%v", k.CreatedAt, created)
	}
}

func TestScan_APIKey_NullTimes(t *testing.T) {
	row := &mockRowScanner{vals: []interface{}{
		"apikey-2", "t1", "no-expiry", "sha256:xyz", "",
		0, sql.NullTime{Valid: false}, sql.NullTime{Valid: false}, 0, time.Time{},
	}}
	k := scanAPIKey(row)
	if k == nil {
		t.Fatal("scanAPIKey 返回 nil")
	}
	if !k.ExpiresAt.IsZero() {
		t.Fatalf("NullTime Invalid 时 ExpiresAt 应为零值；got=%v", k.ExpiresAt)
	}
	if !k.LastUsedAt.IsZero() {
		t.Fatalf("NullTime Invalid 时 LastUsedAt 应为零值；got=%v", k.LastUsedAt)
	}
	if k.Enabled {
		t.Fatal("Enabled=0 时应为 false")
	}
}

func TestScan_APIKey_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("db error")}
	if k := scanAPIKey(row); k != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// scanPlugin 列顺序：id, name, version, description, author, type, download_url,
// checksum, installed(int), enabled(int), created_at。

func TestScan_Plugin_Happy(t *testing.T) {
	created := time.Date(2026, 7, 4, 8, 0, 0, 0, time.UTC)
	row := &mockRowScanner{vals: []interface{}{
		"plugin-1", "nvidia-gpu", "1.0.0", "NVIDIA GPU 监控插件", "opsmesh",
		"agent", "https://opsmesh.io/plugins/nvidia-gpu", "sha256:abc",
		1, 1, created,
	}}
	p := scanPlugin(row)
	if p == nil {
		t.Fatal("scanPlugin 返回 nil")
	}
	if p.ID != "plugin-1" || p.Name != "nvidia-gpu" || p.Version != "1.0.0" {
		t.Fatalf("基础字段映射错误: %+v", p)
	}
	if p.Description != "NVIDIA GPU 监控插件" || p.Author != "opsmesh" {
		t.Fatalf("描述/作者错误: %+v", p)
	}
	if p.Type != "agent" || p.DownloadURL != "https://opsmesh.io/plugins/nvidia-gpu" {
		t.Fatalf("类型/下载URL错误: %+v", p)
	}
	if p.Checksum != "sha256:abc" {
		t.Fatalf("Checksum 错误: %q", p.Checksum)
	}
	if !p.Installed || !p.Enabled {
		t.Fatalf("Installed/Enabled 应为 true: %+v", p)
	}
	if !p.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt 错误: got=%v want=%v", p.CreatedAt, created)
	}
}

func TestScan_Plugin_NotInstalled(t *testing.T) {
	row := &mockRowScanner{vals: []interface{}{
		"plugin-2", "ui-ext", "0.1.0", "", "", "ui", "", "",
		0, 0, time.Time{},
	}}
	p := scanPlugin(row)
	if p == nil {
		t.Fatal("scanPlugin 返回 nil")
	}
	if p.Installed || p.Enabled {
		t.Fatalf("Installed/Enabled=0 时应为 false: %+v", p)
	}
}

func TestScan_Plugin_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("db error")}
	if p := scanPlugin(row); p != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// scanBillingPlan 列顺序：id, name, price, interval, features(JSON string),
// resource_limits(JSON string), created_at。

func TestScan_BillingPlan_Happy(t *testing.T) {
	created := time.Date(2026, 7, 5, 8, 0, 0, 0, time.UTC)
	featuresJSON, _ := json.Marshal([]string{"sso", "audit", "rbac"})
	limitsJSON, _ := json.Marshal(TenantQuota{MaxDevices: 200, MaxTasks: 1000, MaxAlerts: 100})
	row := &mockRowScanner{vals: []interface{}{
		"plan-1", "pro", 9900, "monthly", string(featuresJSON), string(limitsJSON), created,
	}}
	p := scanBillingPlan(row)
	if p == nil {
		t.Fatal("scanBillingPlan 返回 nil")
	}
	if p.ID != "plan-1" || p.Name != "pro" || p.Price != 9900 {
		t.Fatalf("基础字段映射错误: %+v", p)
	}
	if p.Interval != "monthly" {
		t.Fatalf("Interval 错误: %q", p.Interval)
	}
	if len(p.Features) != 3 || p.Features[0] != "sso" || p.Features[2] != "rbac" {
		t.Fatalf("Features 解析错误: %+v", p.Features)
	}
	if p.ResourceLimits.MaxDevices != 200 || p.ResourceLimits.MaxTasks != 1000 {
		t.Fatalf("ResourceLimits 解析错误: %+v", p.ResourceLimits)
	}
	if !p.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt 错误: got=%v want=%v", p.CreatedAt, created)
	}
}

func TestScan_BillingPlan_EmptyJSON(t *testing.T) {
	row := &mockRowScanner{vals: []interface{}{
		"plan-2", "free", 0, "monthly", "", "", time.Time{},
	}}
	p := scanBillingPlan(row)
	if p == nil {
		t.Fatal("scanBillingPlan 返回 nil")
	}
	if len(p.Features) != 0 {
		t.Fatalf("空 features 应解析为零值；got=%+v", p.Features)
	}
	if p.ResourceLimits.MaxDevices != 0 {
		t.Fatalf("空 resource_limits 应解析为零值；got=%+v", p.ResourceLimits)
	}
}

func TestScan_BillingPlan_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("db error")}
	if p := scanBillingPlan(row); p != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// scanSubscription 列顺序：id, tenant_id, plan_id, status, started_at, expires_at,
// created_at。

func TestScan_Subscription_Happy(t *testing.T) {
	started := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	expires := time.Date(2027, 7, 1, 0, 0, 0, 0, time.UTC)
	created := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	row := &mockRowScanner{vals: []interface{}{
		"sub-1", "t1", "plan-1", "active", started, expires, created,
	}}
	sub := scanSubscription(row)
	if sub == nil {
		t.Fatal("scanSubscription 返回 nil")
	}
	if sub.ID != "sub-1" || sub.TenantID != "t1" || sub.PlanID != "plan-1" {
		t.Fatalf("基础字段映射错误: %+v", sub)
	}
	if sub.Status != "active" {
		t.Fatalf("Status 错误: %q", sub.Status)
	}
	if !sub.StartedAt.Equal(started) || !sub.ExpiresAt.Equal(expires) {
		t.Fatalf("StartedAt/ExpiresAt 错误: started=%v expires=%v", sub.StartedAt, sub.ExpiresAt)
	}
	if !sub.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt 错误: got=%v want=%v", sub.CreatedAt, created)
	}
}

func TestScan_Subscription_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("db error")}
	if sub := scanSubscription(row); sub != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// scanInvoice 列顺序：id, tenant_id, subscription_id, amount, period_start, period_end,
// status, items(JSON string), created_at。

func TestScan_Invoice_Happy(t *testing.T) {
	periodStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	created := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	itemsJSON, _ := json.Marshal([]InvoiceItem{
		{Name: "basic-plan", Quantity: 1, UnitPrice: 9900, Amount: 9900},
		{Name: "extra-device", Quantity: 5, UnitPrice: 1000, Amount: 5000},
	})
	row := &mockRowScanner{vals: []interface{}{
		"inv-1", "t1", "sub-1", 14900, periodStart, periodEnd, "paid",
		string(itemsJSON), created,
	}}
	inv := scanInvoice(row)
	if inv == nil {
		t.Fatal("scanInvoice 返回 nil")
	}
	if inv.ID != "inv-1" || inv.TenantID != "t1" || inv.SubscriptionID != "sub-1" {
		t.Fatalf("基础字段映射错误: %+v", inv)
	}
	if inv.Amount != 14900 {
		t.Fatalf("Amount 错误: got=%d want=14900", inv.Amount)
	}
	if !inv.PeriodStart.Equal(periodStart) || !inv.PeriodEnd.Equal(periodEnd) {
		t.Fatalf("PeriodStart/PeriodEnd 错误: start=%v end=%v", inv.PeriodStart, inv.PeriodEnd)
	}
	if inv.Status != "paid" {
		t.Fatalf("Status 错误: %q", inv.Status)
	}
	if len(inv.Items) != 2 || inv.Items[0].Name != "basic-plan" || inv.Items[0].Amount != 9900 {
		t.Fatalf("Items 解析错误: %+v", inv.Items)
	}
	if inv.Items[1].Quantity != 5 || inv.Items[1].Amount != 5000 {
		t.Fatalf("Items[1] 解析错误: %+v", inv.Items[1])
	}
	if !inv.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt 错误: got=%v want=%v", inv.CreatedAt, created)
	}
}

func TestScan_Invoice_EmptyItems(t *testing.T) {
	row := &mockRowScanner{vals: []interface{}{
		"inv-2", "t1", "sub-1", 0, time.Time{}, time.Time{}, "pending",
		"", time.Time{},
	}}
	inv := scanInvoice(row)
	if inv == nil {
		t.Fatal("scanInvoice 返回 nil")
	}
	if len(inv.Items) != 0 {
		t.Fatalf("空 items 应解析为零值；got=%+v", inv.Items)
	}
}

func TestScan_Invoice_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("db error")}
	if inv := scanInvoice(row); inv != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}