// backup_test.go 单测 backup/restore 子命令的导出/导入逻辑。
//
// 覆盖：
//  1. ExportBackup JSON 格式：验证 Meta/Counts/数据条数；
//  2. ExportBackup SQL 格式：验证输出含 INSERT 语句；
//  3. ImportBackup 正常导入：验证条数统计；
//  4. ImportBackup dry-run：验证只校验不写入；
//  5. ImportBackup overwrite：验证覆盖已存在数据；
//  6. Export→Import 往返：导出后导入到新 Store，验证数据一致。
package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"opsmesh/internal/config"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// seedBackupStore 构造一个有数据的 MemoryStore，供导出测试使用。
func seedBackupStore() *store.MemoryStore {
	st := store.NewMemoryStore()
	// 注册 2 个 agent（不同租户）
	st.Register(&proto.AgentInfo{AgentID: "agent-1", Hostname: "h1", Segment: "seg-a", TenantID: "t1", Status: "online"})
	st.Register(&proto.AgentInfo{AgentID: "agent-2", Hostname: "h2", Segment: "seg-b", TenantID: "t2", Status: "online"})
	// 2 台设备
	st.UpsertDevice(&proto.DeviceInfo{DeviceID: "dev-1", Segment: "seg-a", TenantID: "t1", AgentID: "agent-1", Hostname: "h1", State: "online", Managed: true})
	st.UpsertDevice(&proto.DeviceInfo{DeviceID: "dev-2", Segment: "seg-b", TenantID: "t2", AgentID: "agent-2", Hostname: "h2", State: "online", Managed: true})
	// 2 个任务（最近 1 天内）
	now := time.Now()
	st.CreateTask(&proto.Task{TaskID: "task-1", AgentID: "agent-1", TenantID: "t1", Command: "echo 1", CreatedAt: now})
	st.CreateTask(&proto.Task{TaskID: "task-2", AgentID: "agent-2", TenantID: "t2", Command: "echo 2", CreatedAt: now})
	// 1 条告警
	st.AddAlert(&proto.Alert{AlertID: "alert-1", TenantID: "t1", DeviceID: "dev-1", Severity: "warning", Message: "test", CreatedAt: now})
	// 1 条告警规则
	st.CreateAlertRule(&store.AlertRule{ID: "rule-1", TenantID: "t1", Metric: "cpu_usage", Op: ">", Threshold: 90, Enabled: true, CreatedAt: now})
	// 1 条审计
	st.Audit(&proto.AuditEvent{TenantID: "t1", UserID: "u1", Action: "test", Target: "t1", CreatedAt: now})
	return st
}

// TestExportBackup_JSON 验证 JSON 格式导出：Meta/Counts/数据条数正确。
func TestExportBackup_JSON(t *testing.T) {
	st := seedBackupStore()
	cfg := &config.Config{Mode: "controlplane"}
	var buf bytes.Buffer
	opts := ExportOptions{Format: "json", IncludeAudits: true, IncludeConfig: true}

	data, err := ExportBackup(context.Background(), st, cfg, opts, &buf)
	if err != nil {
		t.Fatalf("ExportBackup 失败: %v", err)
	}
	if data.Meta.Format != "json" {
		t.Fatalf("Format = %q, want json", data.Meta.Format)
	}
	if data.Meta.IncludeAudits != true {
		t.Fatalf("IncludeAudits = false, want true")
	}
	// 验证条数
	if data.Meta.Counts.Agents != 2 {
		t.Fatalf("Agents = %d, want 2", data.Meta.Counts.Agents)
	}
	// MemoryStore.Register 为每个 agent 自动创建 1 台设备（"agent 即设备" MVP 降级），
	// 加上显式 UpsertDevice 的 2 台 = 4 台。
	if data.Meta.Counts.Devices != 4 {
		t.Fatalf("Devices = %d, want 4", data.Meta.Counts.Devices)
	}
	if data.Meta.Counts.Tasks != 2 {
		t.Fatalf("Tasks = %d, want 2", data.Meta.Counts.Tasks)
	}
	if data.Meta.Counts.Alerts != 1 {
		t.Fatalf("Alerts = %d, want 1", data.Meta.Counts.Alerts)
	}
	if data.Meta.Counts.AlertRules != 1 {
		t.Fatalf("AlertRules = %d, want 1", data.Meta.Counts.AlertRules)
	}
	// 审计：seedBackupStore 产 1 条 + Register/CreateTask/AddAlert/CreateAlertRule 内核产出
	if data.Meta.Counts.Audits < 1 {
		t.Fatalf("Audits = %d, want >=1", data.Meta.Counts.Audits)
	}
	// 预定义 RBAC：3 角色 + 3 用户 + 多个权限
	if data.Meta.Counts.Roles < 3 {
		t.Fatalf("Roles = %d, want >=3", data.Meta.Counts.Roles)
	}
	if data.Meta.Counts.Users < 3 {
		t.Fatalf("Users = %d, want >=3", data.Meta.Counts.Users)
	}
	if data.Meta.Counts.Permissions == 0 {
		t.Fatalf("Permissions = 0, want >0")
	}
	// 验证 Config 被导出
	if data.Config == nil {
		t.Fatalf("Config = nil, want non-nil")
	}
	// 验证输出是合法 JSON
	var parsed BackupData
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("JSON 解析失败: %v", err)
	}
}

// TestExportBackup_SQL 验证 SQL dump 格式：输出含 INSERT 语句。
func TestExportBackup_SQL(t *testing.T) {
	st := seedBackupStore()
	cfg := &config.Config{Mode: "controlplane"}
	var buf bytes.Buffer
	opts := ExportOptions{Format: "sql"}

	_, err := ExportBackup(context.Background(), st, cfg, opts, &buf)
	if err != nil {
		t.Fatalf("ExportBackup SQL 失败: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "INSERT INTO") {
		t.Fatalf("SQL dump 缺少 INSERT 语句")
	}
	if !strings.Contains(out, "opsmesh_devices") {
		t.Fatalf("SQL dump 缺少 opsmesh_devices 表")
	}
	if !strings.Contains(out, "opsmesh_agents") {
		t.Fatalf("SQL dump 缺少 opsmesh_agents 表")
	}
	if !strings.Contains(out, "opsmesh_tasks") {
		t.Fatalf("SQL dump 缺少 opsmesh_tasks 表")
	}
}

// TestExportBackup_DefaultOptions 验证默认选项：format=json, 不含审计, 不含 config。
func TestExportBackup_DefaultOptions(t *testing.T) {
	st := seedBackupStore()
	cfg := &config.Config{Mode: "controlplane"}
	var buf bytes.Buffer
	opts := ExportOptions{} // 全默认

	data, err := ExportBackup(context.Background(), st, cfg, opts, &buf)
	if err != nil {
		t.Fatalf("ExportBackup 失败: %v", err)
	}
	if data.Meta.Format != "json" {
		t.Fatalf("默认 Format = %q, want json", data.Meta.Format)
	}
	if data.Meta.IncludeAudits != false {
		t.Fatalf("默认 IncludeAudits = true, want false")
	}
	if data.Meta.IncludeConfig != false {
		t.Fatalf("默认 IncludeConfig = true, want false")
	}
	if data.Meta.TaskWindowDays != 7 {
		t.Fatalf("默认 TaskWindowDays = %d, want 7", data.Meta.TaskWindowDays)
	}
	if len(data.Audits) != 0 {
		t.Fatalf("默认不应导出审计, got %d", len(data.Audits))
	}
	if data.Config != nil {
		t.Fatalf("默认不应导出 config")
	}
}

// TestImportBackup_DryRun 验证 dry-run：只校验不写入。
func TestImportBackup_DryRun(t *testing.T) {
	src := seedBackupStore()
	// 导出到 buffer
	var buf bytes.Buffer
	data, err := ExportBackup(context.Background(), src, &config.Config{}, ExportOptions{IncludeAudits: true}, &buf)
	if err != nil {
		t.Fatalf("ExportBackup 失败: %v", err)
	}
	// dry-run 导入到新 store
	dst := store.NewMemoryStore()
	opts := ImportOptions{DryRun: true}
	_, res, err := ImportBackup(context.Background(), dst, opts, &buf)
	if err != nil {
		t.Fatalf("ImportBackup dry-run 失败: %v", err)
	}
	// 验证统计条数与导出一致
	if res.Devices != data.Meta.Counts.Devices {
		t.Fatalf("dry-run Devices = %d, want %d", res.Devices, data.Meta.Counts.Devices)
	}
	if res.Agents != data.Meta.Counts.Agents {
		t.Fatalf("dry-run Agents = %d, want %d", res.Agents, data.Meta.Counts.Agents)
	}
	if res.Tasks != data.Meta.Counts.Tasks {
		t.Fatalf("dry-run Tasks = %d, want %d", res.Tasks, data.Meta.Counts.Tasks)
	}
	// dry-run 不应写入任何数据
	if len(dst.Agents("")) != 0 {
		t.Fatalf("dry-run 后 dst 不应有 agent, got %d", len(dst.Agents("")))
	}
	if len(dst.AllTasks("")) != 0 {
		t.Fatalf("dry-run 后 dst 不应有 task, got %d", len(dst.AllTasks("")))
	}
}

// TestImportBackup_Normal 验证正常导入：数据写入新 store。
func TestImportBackup_Normal(t *testing.T) {
	src := seedBackupStore()
	var buf bytes.Buffer
	_, err := ExportBackup(context.Background(), src, &config.Config{}, ExportOptions{IncludeAudits: true}, &buf)
	if err != nil {
		t.Fatalf("ExportBackup 失败: %v", err)
	}
	dst := store.NewMemoryStore()
	opts := ImportOptions{}
	_, res, err := ImportBackup(context.Background(), dst, opts, &buf)
	if err != nil {
		t.Fatalf("ImportBackup 失败: %v", err)
	}
	// 验证导入后 dst 有数据
	if res.Agents != 2 {
		t.Fatalf("导入 Agents = %d, want 2", res.Agents)
	}
	// MemoryStore.Register 为每个 agent 自动创建设备，加上显式 UpsertDevice 的 2 台 = 4 台。
	if res.Devices != 4 {
		t.Fatalf("导入 Devices = %d, want 4", res.Devices)
	}
	if res.Tasks != 2 {
		t.Fatalf("导入 Tasks = %d, want 2", res.Tasks)
	}
	if res.Alerts != 1 {
		t.Fatalf("导入 Alerts = %d, want 1", res.Alerts)
	}
	// 验证 dst 实际有数据
	if len(dst.Agents("")) < 2 {
		t.Fatalf("dst.Agents = %d, want >=2", len(dst.Agents("")))
	}
}

// TestImportBackup_Overwrite 验证 overwrite：已存在数据被覆盖。
func TestImportBackup_Overwrite(t *testing.T) {
	src := seedBackupStore()
	var buf bytes.Buffer
	_, err := ExportBackup(context.Background(), src, &config.Config{}, ExportOptions{}, &buf)
	if err != nil {
		t.Fatalf("ExportBackup 失败: %v", err)
	}
	// dst 预先有同 ID 的 agent
	dst := store.NewMemoryStore()
	dst.Register(&proto.AgentInfo{AgentID: "agent-1", Hostname: "old", Segment: "old", TenantID: "old", Status: "offline"})
	// 非 overwrite：agent-1 应跳过
	opts := ImportOptions{Overwrite: false}
	_, res1, err := ImportBackup(context.Background(), dst, opts, bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ImportBackup(overwrite=false) 失败: %v", err)
	}
	if res1.Skipped == 0 {
		t.Fatalf("overwrite=false 应有跳过, got Skipped=0")
	}
	// overwrite：agent-1 应更新
	res2, _, err := ImportBackup(context.Background(), dst, ImportOptions{Overwrite: true}, bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ImportBackup(overwrite=true) 失败: %v", err)
	}
	// 验证 agent-1 状态被更新（Heartbeat 更新 status）
	a := dst.Agent("agent-1")
	if a == nil {
		t.Fatalf("agent-1 不存在")
	}
	if a.Status != "online" {
		t.Fatalf("overwrite 后 agent-1.Status = %q, want online", a.Status)
	}
	_ = res2
}

// TestExportImport_Roundtrip 验证导出→导入往返：导入到新 store 后数据一致。
func TestExportImport_Roundtrip(t *testing.T) {
	src := seedBackupStore()
	var buf bytes.Buffer
	data, err := ExportBackup(context.Background(), src, &config.Config{}, ExportOptions{IncludeAudits: true}, &buf)
	if err != nil {
		t.Fatalf("ExportBackup 失败: %v", err)
	}
	dst := store.NewMemoryStore()
	_, _, err = ImportBackup(context.Background(), dst, ImportOptions{}, &buf)
	if err != nil {
		t.Fatalf("ImportBackup 失败: %v", err)
	}
	// 验证 agent 数量一致
	if got := len(dst.Agents("")); got != data.Meta.Counts.Agents {
		t.Fatalf("roundtrip Agents = %d, want %d", got, data.Meta.Counts.Agents)
	}
	// 验证 task 数量一致
	if got := len(dst.AllTasks("")); got != data.Meta.Counts.Tasks {
		t.Fatalf("roundtrip Tasks = %d, want %d", got, data.Meta.Counts.Tasks)
	}
	// 验证 alert 数量一致
	if got := len(dst.Alerts("")); got != data.Meta.Counts.Alerts {
		t.Fatalf("roundtrip Alerts = %d, want %d", got, data.Meta.Counts.Alerts)
	}
	// 验证 alert rule 数量一致
	if got := len(dst.ListAlertRules("")); got != data.Meta.Counts.AlertRules {
		t.Fatalf("roundtrip AlertRules = %d, want %d", got, data.Meta.Counts.AlertRules)
	}
}

// TestImportBackup_BadJSON 验证非法 JSON 返回错误。
func TestImportBackup_BadJSON(t *testing.T) {
	st := store.NewMemoryStore()
	_, _, err := ImportBackup(context.Background(), st, ImportOptions{}, strings.NewReader("not json"))
	if err == nil {
		t.Fatalf("ImportBackup 非法 JSON 应返回 error")
	}
}

// TestExportBackup_UnsupportedFormat 验证不支持的格式返回错误。
func TestExportBackup_UnsupportedFormat(t *testing.T) {
	st := seedBackupStore()
	var buf bytes.Buffer
	opts := ExportOptions{Format: "xml"}
	_, err := ExportBackup(context.Background(), st, &config.Config{}, opts, &buf)
	if err == nil {
		t.Fatalf("ExportBackup 不支持格式应返回 error")
	}
	if !strings.Contains(err.Error(), "xml") {
		t.Fatalf("错误应提及不支持的格式 xml, got: %v", err)
	}
}