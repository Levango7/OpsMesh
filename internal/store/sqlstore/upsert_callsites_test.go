package sqlstore

import (
	"sort"
	"strings"
	"testing"
)

// expectedUpsert 描述一处期望的 s.dialect.Upsert 调用点：
// 所在函数、目标表、冲突目标（冲突列顺序敏感，须与 DDL 唯一键一致）。
//
// Ordinal 用于同 Func+Table+Conflict 下存在多条调用点的情形
// （Register 对 agents/devices 各 upsert 两次）：按源码位置
// （fset.Position）排序后逐条匹配。Ordinal=0 保持「精确唯一匹配」
// 语义（缺失或重复都报红），Task 6 组不设置 Ordinal。
//
// Cols/Values/UpdateCols 非 nil 时逐列钉死（含常量 VALUES 与
// 更新集差异，如 placeholder 刻意省略 agent_id）；nil 时不校验，
// 保持 Task 6 组断言强度不变。
type expectedUpsert struct {
	Func       string
	Table      string
	Conflict   []string
	Cols       []string
	Values     []string
	UpdateCols []string
	Ordinal    int
}

// lowRiskCRUDUpserts 钉死低风险 CRUD 组 15 处 upsert 调用点
// 的 Func+Table+Conflict 元数据。迁移完成后 collectUpsertCalls
// 必须恰好命中每一处期望：缺失（未迁移）或重复（重复调用点）
// 都报红。
var lowRiskCRUDUpserts = []expectedUpsert{
	{"CreateAlertRule", "alert_rules", []string{"id"}, nil, nil, nil, 0},
	{"CreateAPIKey", "api_keys", []string{"id"}, nil, nil, nil, 0},
	{"CreateApp", "argocd_apps", []string{"id"}, nil, nil, nil, 0},
	{"CreateAutomationRule", "automation_rules", []string{"id"}, nil, nil, nil, 0},
	{"CreateAutomationExecution", "automation_executions", []string{"id"}, nil, nil, nil, 0},
	{"CreateBackup", "backup_records", []string{"id"}, nil, nil, nil, 0},
	{"CreateBillingPlan", "billing_plans", []string{"id"}, nil, nil, nil, 0},
	{"CreateSubscription", "subscriptions", []string{"id"}, nil, nil, nil, 0},
	{"CreateInvoice", "invoices", []string{"id"}, nil, nil, nil, 0},
	{"UpsertBudget", "budgets", []string{"tenant_id"}, nil, nil, nil, 0},
	{"CreateCloudAccount", "cloud_accounts", []string{"id"}, nil, nil, nil, 0},
	{"SaveReport", "compliance_reports", []string{"id"}, nil, nil, nil, 0},
	{"CreateScript", "scripts", []string{"id"}, nil, nil, nil, 0},
	{"SaveOSTemplate", "os_templates", []string{"id"}, nil, nil, nil, 0},
	{"SaveMiddlewareTemplate", "middleware_templates", []string{"id"}, nil, nil, nil, 0},
}

func TestUpsertCallsitesLowRiskCRUD(t *testing.T) {
	assertExpectedUpserts(t, lowRiskCRUDUpserts)
}

// devicesAndOpsUpserts 钉死设备、通知与流水线组 16 处
// upsert 调用点的 Func+Table+Conflict 元数据及列/值/更新集。
// Register 对 agents 与 devices 各产生两条同表调用点，故用
// Ordinal 按源码位置排序后逐条匹配；onboard 与 placeholder
// 的更新集差异（placeholder 刻意省略 agent_id）由
// UpdateCols 逐列钉死。
var devicesAndOpsUpserts = []expectedUpsert{
	// Register agents+secret（首次注册分支，含 secret 列）。
	{Func: "Register", Table: "agents", Conflict: []string{"agent_id"}, Ordinal: 1,
		Cols:       []string{"agent_id", "hostname", "segment", "tenant_id", "addr", "grpc_port", "metrics_port", "status", "load", "last_seen", "secret"},
		Values:     qmarks(11),
		UpdateCols: []string{"hostname", "segment", "tenant_id", "addr", "grpc_port", "metrics_port", "status", "load", "last_seen"}},
	// Register agents-no-secret（老库无 secret 列降级分支）。
	{Func: "Register", Table: "agents", Conflict: []string{"agent_id"}, Ordinal: 2,
		Cols:       []string{"agent_id", "hostname", "segment", "tenant_id", "addr", "grpc_port", "metrics_port", "status", "load", "last_seen"},
		Values:     qmarks(10),
		UpdateCols: []string{"hostname", "segment", "tenant_id", "addr", "grpc_port", "metrics_port", "status", "load", "last_seen"}},
	// Register onboard（翻转已发现候选设备为已纳管）。
	{Func: "Register", Table: "devices", Conflict: []string{"device_id"}, Ordinal: 1,
		Cols:       []string{"device_id", "segment", "tenant_id", "ip", "agent_id", "state", "task_state", "managed", "hostname", "os", "arch"},
		Values:     []string{"?", "?", "?", "?", "?", "'online'", "'idle'", "1", "?", "?", "?"},
		UpdateCols: []string{"segment", "tenant_id", "ip", "agent_id", "state", "task_state", "managed", "hostname", "os", "arch"}},
	// Register placeholder（按 agent 即设备语义新建占位设备；
	// 更新集刻意省略 agent_id，与 onboard 区分）。
	{Func: "Register", Table: "devices", Conflict: []string{"device_id"}, Ordinal: 2,
		Cols:       []string{"device_id", "segment", "tenant_id", "ip", "agent_id", "state", "task_state", "managed", "hostname", "os", "arch"},
		Values:     []string{"?", "?", "?", "?", "?", "'online'", "'idle'", "1", "?", "?", "?"},
		UpdateCols: []string{"segment", "tenant_id", "ip", "state", "task_state", "managed", "hostname", "os", "arch"}},
	// Register demo-task（--demo 预置 uname -a 示例任务）。
	{Func: "Register", Table: "tasks", Conflict: []string{"task_id"}, Ordinal: 1,
		Cols:       []string{"task_id", "agent_id", "tenant_id", "type", "command", "status", "created_at"},
		Values:     []string{"?", "?", "?", "?", "?", "'pending'", "?"},
		UpdateCols: []string{"type", "command", "status"}},
	// UpsertDevice（设备详情/心跳写回）。
	{Func: "UpsertDevice", Table: "devices", Conflict: []string{"device_id"}, Ordinal: 1,
		Cols:       []string{"device_id", "segment", "tenant_id", "ip", "agent_id", "state", "task_state", "managed", "last_result", "last_result_at", "retired"},
		Values:     qmarks(11),
		UpdateCols: []string{"segment", "tenant_id", "ip", "agent_id", "state", "task_state", "managed", "last_result", "last_result_at", "retired"}},
	// RegisterService（服务发现实例注册）。
	{Func: "RegisterService", Table: "services", Conflict: []string{"service_id"}, Ordinal: 1,
		Cols:       []string{"service_id", "tenant_id", "service_name", "address", "port", "metadata", "status", "last_heartbeat", "created_at"},
		Values:     qmarks(9),
		UpdateCols: []string{"tenant_id", "service_name", "address", "port", "metadata", "status", "last_heartbeat"}},
	// CreateSilence（告警静默规则）。
	{Func: "CreateSilence", Table: "alert_silences", Conflict: []string{"id"}, Ordinal: 1,
		Cols:       []string{"id", "tenant_id", "match_labels", "starts_at", "ends_at", "created_by", "reason", "created_at"},
		Values:     qmarks(8),
		UpdateCols: []string{"tenant_id", "match_labels", "starts_at", "ends_at", "created_by", "reason"}},
	// CreateNotifyChannel（通知渠道）。
	{Func: "CreateNotifyChannel", Table: "notify_channels", Conflict: []string{"id"}, Ordinal: 1,
		Cols:       []string{"id", "tenant_id", "name", "type", "config", "enabled", "created_at", "updated_at"},
		Values:     qmarks(8),
		UpdateCols: []string{"name", "type", "config", "enabled", "updated_at"}},
	// CreateNotifyTemplate（通知模板）。
	{Func: "CreateNotifyTemplate", Table: "notify_templates", Conflict: []string{"id"}, Ordinal: 1,
		Cols:       []string{"id", "tenant_id", "name", "type", "title", "body", "format", "created_at", "updated_at"},
		Values:     qmarks(9),
		UpdateCols: []string{"name", "type", "title", "body", "format", "updated_at"}},
	// CreateNetworkDevice（网络设备）。
	{Func: "CreateNetworkDevice", Table: "network_devices", Conflict: []string{"id"}, Ordinal: 1,
		Cols:       []string{"id", "tenant_id", "name", "type", "vendor", "model", "ip", "mask", "mac", "location", "snmp_community", "status", "config", "created_at", "updated_at"},
		Values:     qmarks(15),
		UpdateCols: []string{"name", "type", "vendor", "model", "ip", "mask", "mac", "location", "snmp_community", "status", "config", "updated_at"}},
	// CreateTemplate（流水线模板）。
	{Func: "CreateTemplate", Table: "pipeline_templates", Conflict: []string{"id"}, Ordinal: 1,
		Cols:       []string{"id", "tenant_id", "name", "description", "type", "yaml", "agent_id", "parameters", "created_at", "updated_at"},
		Values:     qmarks(10),
		UpdateCols: []string{"name", "description", "type", "yaml", "agent_id", "parameters", "updated_at"}},
	// CreateRun（流水线运行记录）。
	{Func: "CreateRun", Table: "pipeline_runs", Conflict: []string{"id"}, Ordinal: 1,
		Cols:       []string{"id", "tenant_id", "template_id", "template_name", "status", "parameters", "logs", "started_at", "finished_at", "created_at"},
		Values:     qmarks(10),
		UpdateCols: []string{"template_id", "template_name", "status", "parameters", "logs", "started_at", "finished_at"}},
	// CreatePlugin（插件市场）。
	{Func: "CreatePlugin", Table: "plugins", Conflict: []string{"id"}, Ordinal: 1,
		Cols:       []string{"id", "name", "version", "description", "author", "type", "download_url", "checksum", "installed", "enabled", "created_at"},
		Values:     qmarks(11),
		UpdateCols: []string{"name", "version", "description", "author", "type", "download_url", "checksum", "installed", "enabled"}},
	// SetQuota（租户配额）。
	{Func: "SetQuota", Table: "quota_configs", Conflict: []string{"tenant_id"}, Ordinal: 1,
		Cols:       []string{"tenant_id", "max_devices", "max_tasks", "max_alerts", "updated_at"},
		Values:     qmarks(5),
		UpdateCols: []string{"max_devices", "max_tasks", "max_alerts", "updated_at"}},
	// SaveRefreshToken（刷新令牌）。
	{Func: "SaveRefreshToken", Table: "refresh_tokens", Conflict: []string{"token_hash"}, Ordinal: 1,
		Cols:       []string{"token_hash", "user_id", "tenant_id", "device_fp", "expires_at", "created_at"},
		Values:     qmarks(6),
		UpdateCols: []string{"user_id", "tenant_id", "device_fp", "expires_at", "created_at"}},
}

func TestUpsertCallsitesDevicesAndOperations(t *testing.T) {
	assertExpectedUpserts(t, devicesAndOpsUpserts)
}

// businessCriticalUpserts 钉死业务关键组 14 处
// upsert 调用点的 Func+Table+Conflict 元数据及列/值/更新集。
// 与低风险 CRUD 组不同，本组逐列钉死 Cols/Values/UpdateCols：
// 业务关键写路径的更新集与常量 VALUES（NOW()、0）必须与
// 迁移前 MySQL 文本逐列等价（CreateTask 不得更新
// retry_count/dead_letter/created_at；issueToken 复位
// consumed=0；UpsertControlplaneInstance 刷新 last_heartbeat=NOW()）。
var businessCriticalUpserts = []expectedUpsert{
	// UpsertControlplaneInstance（控制面实例心跳）。
	{Func: "UpsertControlplaneInstance", Table: "controlplane_instances", Conflict: []string{"instance_id"},
		Cols:       []string{"instance_id", "hostname", "http_port", "grpc_port", "role", "version", "started_at", "last_heartbeat"},
		Values:     []string{"?", "?", "?", "?", "?", "?", "?", "NOW()"},
		UpdateCols: []string{"hostname", "http_port", "grpc_port", "role", "version", "last_heartbeat"}},
	// CreateCloudResource（PG 冲突目标为自然键 uk_cloud_resources_account_res）。
	{Func: "CreateCloudResource", Table: "cloud_resources", Conflict: []string{"cloud_account_id", "resource_type", "resource_id"},
		Cols:       []string{"id", "tenant_id", "cloud_account_id", "provider", "region", "resource_type", "resource_id", "resource_name", "status", "tags", "private_ip", "public_ip", "created_at", "updated_at"},
		Values:     qmarks(14),
		UpdateCols: []string{"tenant_id", "cloud_account_id", "provider", "region", "resource_type", "resource_id", "resource_name", "status", "tags", "private_ip", "public_ip", "updated_at"}},
	// SetConfig（配置写穿 config_history 后 UPSERT configs）。
	{Func: "SetConfig", Table: "configs", Conflict: []string{"tenant_id", "key_name"},
		Cols:       []string{"tenant_id", "key_name", "value", "format", "version", "description", "updated_by", "updated_at"},
		Values:     qmarks(8),
		UpdateCols: []string{"value", "format", "version", "description", "updated_by", "updated_at"}},
	// SaveGatewayRoute（路由规则幂等写）。
	{Func: "SaveGatewayRoute", Table: "gateway_routes", Conflict: []string{"tenant_id", "id"},
		Cols:       []string{"id", "tenant_id", "name", "path_prefix", "target_backend", "methods", "rate_limit_per_sec", "timeout_ms", "enabled", "created_at", "updated_at"},
		Values:     qmarks(11),
		UpdateCols: []string{"name", "path_prefix", "target_backend", "methods", "rate_limit_per_sec", "timeout_ms", "enabled", "updated_at"}},
	// SaveK8sCluster（tenant_id 仅插入不更新，防改写集群租户归属）。
	{Func: "SaveK8sCluster", Table: "k8s_clusters", Conflict: []string{"id"},
		Cols:       []string{"id", "tenant_id", "name", "server", "kubeconfig", "status", "created_at", "updated_at"},
		Values:     qmarks(8),
		UpdateCols: []string{"name", "server", "kubeconfig", "status", "updated_at"}},
	// CreateSLO。
	{Func: "CreateSLO", Table: "slos", Conflict: []string{"id"},
		Cols:       []string{"id", "tenant_id", "name", "description", "service_name", "target", "window_spec", "slis", "created_at", "updated_at"},
		Values:     qmarks(10),
		UpdateCols: []string{"name", "description", "service_name", "target", "window_spec", "slis", "updated_at"}},
	// SaveTaskRecord（payload 整段重写，其余列不更新）。
	{Func: "SaveTaskRecord", Table: "task_records", Conflict: []string{"tenant_id", "kind", "id"},
		Cols:       []string{"kind", "id", "tenant_id", "created_at", "payload"},
		Values:     qmarks(5),
		UpdateCols: []string{"payload"}},
	// SubmitResult（任务结果写回）。
	{Func: "SubmitResult", Table: "task_results", Conflict: []string{"task_id"},
		Cols:       []string{"task_id", "agent_id", "exit_code", "stdout", "stderr", "finished_at"},
		Values:     qmarks(6),
		UpdateCols: []string{"exit_code", "stdout", "stderr", "finished_at"}},
	// CreateTask（不得更新 retry_count/dead_letter/created_at）。
	{Func: "CreateTask", Table: "tasks", Conflict: []string{"task_id"},
		Cols:       []string{"task_id", "agent_id", "tenant_id", "type", "command", "content", "path", "status", "retry_count", "max_retries", "dead_letter", "schedule", "parent_id", "created_at", "timeout", "retry_delay", "priority", "idempotency_key", "approval_required", "script_body", "depends_on"},
		Values:     qmarks(21),
		UpdateCols: []string{"type", "command", "content", "path", "status", "schedule", "parent_id", "max_retries", "timeout", "retry_delay", "priority", "idempotency_key", "approval_required", "script_body", "depends_on"}},
	// CreateTenant（PG 冲突目标固定 id）。
	{Func: "CreateTenant", Table: "tenants", Conflict: []string{"id"},
		Cols:       []string{"id", "name", "display_name", "status", "quota", "usage_data", "created_at", "updated_at"},
		Values:     qmarks(8),
		UpdateCols: []string{"name", "display_name", "status", "quota", "usage_data", "updated_at"}},
	// CreateTicket。
	{Func: "CreateTicket", Table: "tickets", Conflict: []string{"id"},
		Cols:       []string{"id", "tenant_id", "title", "description", "status", "priority", "category", "assignee_id", "creator_id", "related_device", "related_task", "tags", "created_at", "updated_at", "resolved_at"},
		Values:     qmarks(15),
		UpdateCols: []string{"title", "description", "status", "priority", "category", "assignee_id", "creator_id", "related_device", "related_task", "tags", "updated_at", "resolved_at"}},
	// issueToken（consumed 恒复位为 0；VALUES 内联常量 0）。
	{Func: "issueToken", Table: "install_tokens", Conflict: []string{"token"},
		Cols:       []string{"token", "device_id", "tenant_id", "expires_at", "consumed"},
		Values:     []string{"?", "?", "?", "?", "0"},
		UpdateCols: []string{"device_id", "tenant_id", "expires_at", "consumed"}},
	// CreatePolicy。
	{Func: "CreatePolicy", Table: "traffic_policies", Conflict: []string{"id"},
		Cols:       []string{"id", "tenant_id", "name", "service_name", "type", "canary_weights", "mirror_percent", "timeout", "retries", "retry_timeout", "max_conns", "max_requests", "status", "created_at", "updated_at"},
		Values:     qmarks(15),
		UpdateCols: []string{"name", "service_name", "type", "canary_weights", "mirror_percent", "timeout", "retries", "retry_timeout", "max_conns", "max_requests", "status", "updated_at"}},
	// CreateWebhook。
	{Func: "CreateWebhook", Table: "webhooks", Conflict: []string{"id"},
		Cols:       []string{"id", "tenant_id", "name", "url", "events", "headers", "body_template", "enabled", "retry_count", "retry_interval_sec", "created_at", "updated_at"},
		Values:     qmarks(12),
		UpdateCols: []string{"name", "url", "events", "headers", "body_template", "enabled", "retry_count", "retry_interval_sec", "updated_at"}},
}

// allExpectedUpserts 是三组共 45 处通用 Upsert 调用点的
// 并集，供 TestUpsertCallsiteCountAndConflictParity 钉死
// 总数与每处冲突目标。
var allExpectedUpserts = append(append(append([]expectedUpsert{},
	lowRiskCRUDUpserts...), devicesAndOpsUpserts...), businessCriticalUpserts...)

func TestUpsertCallsitesBusinessCritical(t *testing.T) {
	assertExpectedUpserts(t, businessCriticalUpserts)
}

// TestUpsertCallsiteCountAndConflictParity 常驻守卫：
// 通用 s.dialect.Upsert 调用点必须恰好 45 处，每处
// Conflict 非空，且冲突目标在 MySQL/PG 双轨 DDL 中都存在
// 相同列序的唯一键（assertUpsertConflictParity）。
// 此外逐点钉死 Func+Table+Conflict 三元组必须与三个分组
// 测试的期望完全一致——防止自然键冲突目标被静默降级为
// 代理主键 id（变异射程：CreateCloudResource 的 conflict
// 改为 []string{"id"} 必须报红）。
func TestUpsertCallsiteCountAndConflictParity(t *testing.T) {
	files := sqlstoreProductionFiles(t)
	calls := collectUpsertCalls(t, files)
	if len(calls) != 45 {
		t.Fatalf("通用 Upsert 调用点数量不符: got %d, want 45", len(calls))
	}
	for _, c := range calls {
		if len(c.Conflict) == 0 {
			t.Errorf("Upsert 调用 Conflict 为空: %s:%d Func=%s Table=%s",
				c.File, c.Pos.Line, c.Func, c.Table)
		}
	}
	assertUpsertConflictParity(t, calls)

	want := map[string]int{}
	for _, w := range allExpectedUpserts {
		key := w.Func + "\x00" + w.Table + "\x00" + strings.Join(w.Conflict, ",")
		want[key]++
	}
	got := map[string]int{}
	for _, c := range calls {
		key := c.Func + "\x00" + c.Table + "\x00" + strings.Join(c.Conflict, ",")
		got[key]++
	}
	for key, n := range want {
		if got[key] != n {
			parts := strings.SplitN(key, "\x00", 3)
			t.Errorf("Upsert 调用点冲突目标不符: Func=%s Table=%s Conflict=%s 期望 %d 处，实际 %d 处",
				parts[0], parts[1], parts[2], n, got[key])
		}
	}
	for key, n := range got {
		if want[key] != n {
			parts := strings.SplitN(key, "\x00", 3)
			t.Errorf("Upsert 调用点冲突目标不符: Func=%s Table=%s Conflict=%s 期望 %d 处，实际 %d 处",
				parts[0], parts[1], parts[2], want[key], n)
		}
	}
}

// qmarks 生成 n 个 "?" 占位符。
func qmarks(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "?"
	}
	return out
}

// assertExpectedUpserts 从 collectUpsertCalls 中按 Func+Table+Conflict
// 精确匹配 want：缺失或重复都失败。
//
// Ordinal>0 的期望进入序数模式：同一 Func+Table+Conflict 组内的调用点
// 按源码位置（Pos.Line）排序后，与同组内按 Ordinal 升序的期望逐条匹配；
// 组内调用点数必须等于该组期望条数（多出或缺少都报红）。
// Cols/Values/UpdateCols 非 nil 时逐列钉死（nil 不校验，Task 6 组语义不变）。
func assertExpectedUpserts(t *testing.T, want []expectedUpsert) {
	t.Helper()
	files := sqlstoreProductionFiles(t)
	calls := collectUpsertCalls(t, files)
	// 顺带守冲突目标：每一处调用点的 Conflict 在 MySQL/PG 双轨
	// 迁移 DDL 中都必须存在相同列序的唯一键。
	assertUpsertConflictParity(t, calls)

	groups := map[string][]upsertCallSpec{}
	for _, c := range calls {
		key := c.Func + "\x00" + c.Table + "\x00" + strings.Join(c.Conflict, ",")
		groups[key] = append(groups[key], c)
	}
	// 组内按源码位置排序（Ordinal 匹配的顺序基准）。
	for key := range groups {
		g := groups[key]
		sort.SliceStable(g, func(i, j int) bool {
			return g[i].Pos.Line < g[j].Pos.Line
		})
		groups[key] = g
	}

	// 每个 Ordinal 组的期望条数（用于组大小校验）。
	wantPerGroup := map[string]int{}
	for _, w := range want {
		if w.Ordinal > 0 {
			key := w.Func + "\x00" + w.Table + "\x00" + strings.Join(w.Conflict, ",")
			wantPerGroup[key]++
		}
	}

	for _, w := range want {
		key := w.Func + "\x00" + w.Table + "\x00" + strings.Join(w.Conflict, ",")
		group := groups[key]
		if w.Ordinal == 0 {
			// 精确唯一匹配语义（Task 6 组）：缺失或重复都报红。
			switch len(group) {
			case 0:
				t.Errorf("缺少 Upsert 调用点: Func=%s Table=%s Conflict=%v",
					w.Func, w.Table, w.Conflict)
			case 1:
				assertSpecFields(t, w, group[0])
			default:
				t.Errorf("Upsert 调用点重复: Func=%s Table=%s Conflict=%v 命中 %d 处",
					w.Func, w.Table, w.Conflict, len(group))
			}
			continue
		}
		// 序数模式：同组按 Pos 排序后逐条匹配。
		if w.Ordinal > len(group) {
			t.Errorf("缺少 Upsert 调用点: Func=%s Table=%s Conflict=%v Ordinal=%d（组内仅 %d 条）",
				w.Func, w.Table, w.Conflict, w.Ordinal, len(group))
			continue
		}
		assertSpecFields(t, w, group[w.Ordinal-1])
	}

	// Ordinal 组大小校验：组内调用点数必须等于该组期望条数。
	for key, n := range wantPerGroup {
		if got := len(groups[key]); got != n {
			parts := strings.SplitN(key, "\x00", 3)
			t.Errorf("Upsert 调用点组大小不符: Func=%s Table=%s Conflict=%s 期望 %d 条，实际 %d 条",
				parts[0], parts[1], parts[2], n, got)
		}
	}
}

// assertSpecFields 在非 nil 时逐列钉死 Cols/Values/UpdateCols。
func assertSpecFields(t *testing.T, w expectedUpsert, got upsertCallSpec) {
	t.Helper()
	if w.Cols != nil && !equalStrings(got.Cols, w.Cols) {
		t.Errorf("Upsert 调用点列不符: Func=%s Table=%s Pos=%s Cols=%v want=%v",
			w.Func, w.Table, got.Pos, got.Cols, w.Cols)
	}
	if w.Values != nil && !equalStrings(got.Values, w.Values) {
		t.Errorf("Upsert 调用点 VALUES 不符: Func=%s Table=%s Pos=%s Values=%v want=%v",
			w.Func, w.Table, got.Pos, got.Values, w.Values)
	}
	if w.UpdateCols != nil && !equalStrings(got.UpdateCols, w.UpdateCols) {
		t.Errorf("Upsert 调用点更新集不符: Func=%s Table=%s Pos=%s UpdateCols=%v want=%v",
			w.Func, w.Table, got.Pos, got.UpdateCols, w.UpdateCols)
	}
}
