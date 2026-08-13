package alertengine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"opsmesh/internal/proto"
)

// ============================================================================
// 测试辅助
// ============================================================================

// newTestAlert 构造测试用告警。
func newTestAlert(id, metric, deviceID, severity, status string) *proto.Alert {
	return &proto.Alert{
		AlertID:  id,
		Metric:   metric,
		DeviceID: deviceID,
		Severity: severity,
		Status:   status,
	}
}

// hostDownRule 主机宕机抑制服务告警的规则（测试复用）。
//
// SourceMatch: metric=host_status & severity=critical
// TargetMatch: metric=service_status
// Equal:       device_id（同一主机）
func hostDownRule() InhibitRule {
	return InhibitRule{
		Name:        "host-down-suppress-service-down",
		SourceMatch: map[string]string{"metric": "host_status", "severity": "critical"},
		TargetMatch: map[string]string{"metric": "service_status"},
		Equal:       []string{"device_id"},
	}
}

// fakeClock 可注入时钟（测试用）。
type fakeClock struct {
	t time.Time
}

func (f *fakeClock) now() time.Time          { return f.t }
func (f *fakeClock) advance(d time.Duration) { f.t = f.t.Add(d) }

// ============================================================================
// 核心测试用例
// ============================================================================

// TestInhibitor_HostDownSuppressServiceDown 验证主机宕机告警活跃时抑制同主机的服务告警。
func TestInhibitor_HostDownSuppressServiceDown(t *testing.T) {
	in := NewAlertInhibitor([]InhibitRule{hostDownRule()})

	// 父告警：dev-1 主机宕机
	parent := newTestAlert("alert-1", "host_status", "dev-1", "critical", "firing")
	in.TrackActive(parent)

	// 子告警：dev-1 服务不可用 → 应被抑制
	child := newTestAlert("alert-2", "service_status", "dev-1", "warning", "firing")
	if !in.IsInhibited(child) {
		t.Fatal("service alert on dev-1 should be inhibited by host-down alert")
	}
}

// TestInhibitor_NoSuppressionWhenNoParent 验证无活跃父告警时不抑制。
func TestInhibitor_NoSuppressionWhenNoParent(t *testing.T) {
	in := NewAlertInhibitor([]InhibitRule{hostDownRule()})

	// 无任何活跃告警
	child := newTestAlert("alert-2", "service_status", "dev-1", "warning", "firing")
	if in.IsInhibited(child) {
		t.Fatal("service alert should not be inhibited when no parent active")
	}
}

// TestInhibitor_NoSuppressionWhenEqualMismatch 验证 Equal 标签不匹配时不抑制。
// 父告警在 dev-1，子告警在 dev-2，device_id 不同不应抑制。
func TestInhibitor_NoSuppressionWhenEqualMismatch(t *testing.T) {
	in := NewAlertInhibitor([]InhibitRule{hostDownRule()})

	parent := newTestAlert("alert-1", "host_status", "dev-1", "critical", "firing")
	in.TrackActive(parent)

	// 不同设备的服务告警不应被抑制
	child := newTestAlert("alert-2", "service_status", "dev-2", "warning", "firing")
	if in.IsInhibited(child) {
		t.Fatal("service alert on dev-2 should not be inhibited by host-down on dev-1")
	}
}

// TestInhibitor_RemoveActive 验证父告警恢复后不再抑制。
func TestInhibitor_RemoveActive(t *testing.T) {
	in := NewAlertInhibitor([]InhibitRule{hostDownRule()})

	parent := newTestAlert("alert-1", "host_status", "dev-1", "critical", "firing")
	in.TrackActive(parent)

	child := newTestAlert("alert-2", "service_status", "dev-1", "warning", "firing")
	if !in.IsInhibited(child) {
		t.Fatal("service alert should be inhibited while parent active")
	}

	// 父告警恢复 → 移除
	in.RemoveActive("alert-1")
	if in.IsInhibited(child) {
		t.Fatal("service alert should not be inhibited after parent removed")
	}
}

// TestInhibitor_MultipleRules 验证多条规则组合。
func TestInhibitor_MultipleRules(t *testing.T) {
	rules := []InhibitRule{
		hostDownRule(),
		{
			Name:        "db-down-suppress-query-slow",
			SourceMatch: map[string]string{"metric": "db_status", "severity": "critical"},
			TargetMatch: map[string]string{"metric": "query_latency"},
			Equal:       []string{"device_id"},
		},
	}
	in := NewAlertInhibitor(rules)

	// DB 宕机告警活跃
	dbDown := newTestAlert("alert-db", "db_status", "db-1", "critical", "firing")
	in.TrackActive(dbDown)

	// 同库查询延迟告警应被抑制
	querySlow := newTestAlert("alert-q", "query_latency", "db-1", "warning", "firing")
	if !in.IsInhibited(querySlow) {
		t.Fatal("query latency alert on db-1 should be inhibited by db-down")
	}

	// 不同库查询延迟不应被抑制
	querySlowOther := newTestAlert("alert-q2", "query_latency", "db-2", "warning", "firing")
	if in.IsInhibited(querySlowOther) {
		t.Fatal("query latency on db-2 should not be inhibited by db-down on db-1")
	}

	// 主机宕机告警也活跃
	hostDown := newTestAlert("alert-host", "host_status", "dev-1", "critical", "firing")
	in.TrackActive(hostDown)

	// 同主机服务告警应被主机宕机抑制
	serviceDown := newTestAlert("alert-svc", "service_status", "dev-1", "warning", "firing")
	if !in.IsInhibited(serviceDown) {
		t.Fatal("service alert on dev-1 should be inhibited by host-down")
	}
}

// TestInhibitor_NoRules 验证无规则时所有告警都不抑制。
func TestInhibitor_NoRules(t *testing.T) {
	in := NewAlertInhibitor(nil)

	parent := newTestAlert("alert-1", "host_status", "dev-1", "critical", "firing")
	in.TrackActive(parent)

	child := newTestAlert("alert-2", "service_status", "dev-1", "warning", "firing")
	if in.IsInhibited(child) {
		t.Fatal("no rules: nothing should be inhibited")
	}
}

// ============================================================================
// 边界与防御式测试
// ============================================================================

// TestInhibitor_NilAlert 验证 nil alert 不抑制（防御式）。
func TestInhibitor_NilAlert(t *testing.T) {
	in := NewAlertInhibitor([]InhibitRule{hostDownRule()})
	if in.IsInhibited(nil) {
		t.Fatal("nil alert should not be inhibited")
	}
}

// TestInhibitor_SourceMatchMismatch 验证父告警不匹配 SourceMatch 时不抑制。
// 父告警 severity=warning，规则要求 severity=critical。
func TestInhibitor_SourceMatchMismatch(t *testing.T) {
	in := NewAlertInhibitor([]InhibitRule{hostDownRule()})

	// 父告警 severity=warning，不匹配 SourceMatch（要求 critical）
	parent := newTestAlert("alert-1", "host_status", "dev-1", "warning", "firing")
	in.TrackActive(parent)

	child := newTestAlert("alert-2", "service_status", "dev-1", "warning", "firing")
	if in.IsInhibited(child) {
		t.Fatal("service alert should not be inhibited by non-critical host alert")
	}
}

// TestInhibitor_TargetMatchMismatch 验证子告警不匹配 TargetMatch 时不抑制。
// 子告警 metric=cpu_usage，规则 TargetMatch 要求 metric=service_status。
func TestInhibitor_TargetMatchMismatch(t *testing.T) {
	in := NewAlertInhibitor([]InhibitRule{hostDownRule()})

	parent := newTestAlert("alert-1", "host_status", "dev-1", "critical", "firing")
	in.TrackActive(parent)

	// 子告警 metric=cpu_usage，不匹配 TargetMatch（要求 service_status）
	child := newTestAlert("alert-2", "cpu_usage", "dev-1", "warning", "firing")
	if in.IsInhibited(child) {
		t.Fatal("cpu_usage alert should not be inhibited (target mismatch)")
	}
}

// TestInhibitor_RemoveActiveNonExistent 验证移除不存在的告警幂等（不 panic）。
func TestInhibitor_RemoveActiveNonExistent(t *testing.T) {
	in := NewAlertInhibitor([]InhibitRule{hostDownRule()})
	in.RemoveActive("nonexistent") // 不应 panic
	if in.ActiveCount() != 0 {
		t.Fatalf("ActiveCount = %d, want 0", in.ActiveCount())
	}
}

// TestInhibitor_TrackActiveNilOrEmptyID 验证 nil 或空 AlertID 的告警不被跟踪。
func TestInhibitor_TrackActiveNilOrEmptyID(t *testing.T) {
	in := NewAlertInhibitor([]InhibitRule{hostDownRule()})

	in.TrackActive(nil)
	if in.ActiveCount() != 0 {
		t.Fatalf("ActiveCount after nil = %d, want 0", in.ActiveCount())
	}

	emptyID := newTestAlert("", "host_status", "dev-1", "critical", "firing")
	in.TrackActive(emptyID)
	if in.ActiveCount() != 0 {
		t.Fatalf("ActiveCount after empty ID = %d, want 0", in.ActiveCount())
	}
}

// TestInhibitor_EmptyEqual 验证 Equal 为空时不约束设备，所有匹配 TargetMatch 的子告警都被抑制。
func TestInhibitor_EmptyEqual(t *testing.T) {
	rule := InhibitRule{
		Name:        "global-suppress",
		SourceMatch: map[string]string{"metric": "host_status", "severity": "critical"},
		TargetMatch: map[string]string{"metric": "service_status"},
		Equal:       nil, // 无 Equal 约束
	}
	in := NewAlertInhibitor([]InhibitRule{rule})

	// 父告警在 dev-1
	parent := newTestAlert("alert-1", "host_status", "dev-1", "critical", "firing")
	in.TrackActive(parent)

	// dev-2 的服务告警也应被抑制（无 Equal 约束）
	child := newTestAlert("alert-2", "service_status", "dev-2", "warning", "firing")
	if !in.IsInhibited(child) {
		t.Fatal("service alert on dev-2 should be inhibited (no Equal constraint)")
	}
}

// ============================================================================
// Cleanup / TTL 测试
// ============================================================================

// TestInhibitor_Cleanup 验证清理过期活跃告警。
func TestInhibitor_Cleanup(t *testing.T) {
	now := time.Now()
	clock := &fakeClock{t: now}
	in := newAlertInhibitor([]InhibitRule{hostDownRule()}, 50*time.Millisecond, clock.now)

	parent := newTestAlert("alert-1", "host_status", "dev-1", "critical", "firing")
	in.TrackActive(parent)
	if in.ActiveCount() != 1 {
		t.Fatalf("ActiveCount = %d, want 1", in.ActiveCount())
	}

	// 子告警此时应被抑制
	child := newTestAlert("alert-2", "service_status", "dev-1", "warning", "firing")
	if !in.IsInhibited(child) {
		t.Fatal("service alert should be inhibited before parent expires")
	}

	// 推进时钟超过 TTL
	clock.advance(60 * time.Millisecond)
	removed := in.Cleanup()
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if in.ActiveCount() != 0 {
		t.Fatalf("ActiveCount after cleanup = %d, want 0", in.ActiveCount())
	}

	// 子告警不再被抑制
	if in.IsInhibited(child) {
		t.Fatal("service alert should not be inhibited after parent expired")
	}
}

// TestInhibitor_CleanupPartial 验证部分过期清理（仅清理超 TTL 的）。
func TestInhibitor_CleanupPartial(t *testing.T) {
	now := time.Now()
	clock := &fakeClock{t: now}
	in := newAlertInhibitor([]InhibitRule{hostDownRule()}, 50*time.Millisecond, clock.now)

	// 跟踪 alert-1
	a1 := newTestAlert("alert-1", "host_status", "dev-1", "critical", "firing")
	in.TrackActive(a1)

	// 推进 30ms（未过期）
	clock.advance(30 * time.Millisecond)

	// 跟踪 alert-2
	a2 := newTestAlert("alert-2", "host_status", "dev-2", "critical", "firing")
	in.TrackActive(a2)

	// 再推进 30ms：alert-1 已跟踪 60ms（>50ms 过期），alert-2 仅 30ms（未过期）
	clock.advance(30 * time.Millisecond)
	removed := in.Cleanup()
	if removed != 1 {
		t.Fatalf("removed = %d, want 1 (only alert-1 expired)", removed)
	}
	if in.ActiveCount() != 1 {
		t.Fatalf("ActiveCount = %d, want 1 (alert-2 still valid)", in.ActiveCount())
	}
}

// TestInhibitor_TrackActiveRefreshesTTL 验证重复 TrackActive 刷新存活时间（滑动窗口）。
func TestInhibitor_TrackActiveRefreshesTTL(t *testing.T) {
	now := time.Now()
	clock := &fakeClock{t: now}
	in := newAlertInhibitor([]InhibitRule{hostDownRule()}, 50*time.Millisecond, clock.now)

	parent := newTestAlert("alert-1", "host_status", "dev-1", "critical", "firing")
	in.TrackActive(parent)

	// 推进 40ms（未过期）
	clock.advance(40 * time.Millisecond)
	// 刷新 TTL（重新 TrackActive）
	in.TrackActive(parent)

	// 再推进 40ms：若未刷新，已跟踪 80ms > 50ms 过期；刷新后仅 40ms < 50ms 未过期
	clock.advance(40 * time.Millisecond)
	removed := in.Cleanup()
	if removed != 0 {
		t.Fatalf("removed = %d, want 0 (TTL refreshed)", removed)
	}
	if in.ActiveCount() != 1 {
		t.Fatalf("ActiveCount = %d, want 1 (TTL refreshed)", in.ActiveCount())
	}
}

// ============================================================================
// 隔离性测试
// ============================================================================

// TestInhibitor_RulesIsolated 验证构造时深拷贝规则，外部修改不影响抑制器内部。
func TestInhibitor_RulesIsolated(t *testing.T) {
	rule := hostDownRule()
	in := NewAlertInhibitor([]InhibitRule{rule})

	// 外部修改原规则
	rule.SourceMatch["severity"] = "warning"

	parent := newTestAlert("alert-1", "host_status", "dev-1", "critical", "firing")
	in.TrackActive(parent)
	child := newTestAlert("alert-2", "service_status", "dev-1", "warning", "firing")

	// 内部规则仍要求 severity=critical，父告警匹配，应抑制
	if !in.IsInhibited(child) {
		t.Fatal("external rule modification should not affect inhibitor (deep copy)")
	}
}

// TestInhibitor_TrackActiveIsolated 验证 TrackActive 深拷贝告警，外部修改不影响内部。
func TestInhibitor_TrackActiveIsolated(t *testing.T) {
	in := NewAlertInhibitor([]InhibitRule{hostDownRule()})

	parent := newTestAlert("alert-1", "host_status", "dev-1", "critical", "firing")
	in.TrackActive(parent)

	// 外部修改父告警
	parent.Severity = "warning"

	child := newTestAlert("alert-2", "service_status", "dev-1", "warning", "firing")
	// 内部父告警仍为 critical，应抑制
	if !in.IsInhibited(child) {
		t.Fatal("external alert modification should not affect inhibitor (deep copy)")
	}
}

// ============================================================================
// LoadInhibitRules 测试
// ============================================================================

// writeTempJSON 写入临时 JSON 文件并返回路径（测试用，t.Cleanup 自动清理）。
func writeTempJSON(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("写入临时文件失败: %v", err)
	}
	return path
}

// TestLoadInhibitRules 验证从 JSON 文件加载抑制规则。
//
// JSON 使用 snake_case 字段名（source_match/target_match/equal），
// 加载后应正确映射到 InhibitRule 的 Go 字段名（SourceMatch/TargetMatch/Equal）。
func TestLoadInhibitRules(t *testing.T) {
	jsonContent := `[
  {
    "name": "host-down-suppress-service-down",
    "source_match": {"metric": "host_status", "severity": "critical"},
    "target_match": {"metric": "service_status"},
    "equal": ["device_id"]
  },
  {
    "name": "db-down-suppress-query-slow",
    "source_match": {"metric": "db_status", "severity": "critical"},
    "target_match": {"metric": "query_latency"},
    "equal": ["device_id"]
  }
]`
	path := writeTempJSON(t, "inhibit_rules.json", jsonContent)

	rules, err := LoadInhibitRules(path)
	if err != nil {
		t.Fatalf("LoadInhibitRules 失败: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("len(rules) = %d, want 2", len(rules))
	}

	// 校验第一条规则
	r1 := rules[0]
	if r1.Name != "host-down-suppress-service-down" {
		t.Errorf("rules[0].Name = %q, want %q", r1.Name, "host-down-suppress-service-down")
	}
	if r1.SourceMatch["metric"] != "host_status" || r1.SourceMatch["severity"] != "critical" {
		t.Errorf("rules[0].SourceMatch = %v, want {metric:host_status, severity:critical}", r1.SourceMatch)
	}
	if r1.TargetMatch["metric"] != "service_status" {
		t.Errorf("rules[0].TargetMatch = %v, want {metric:service_status}", r1.TargetMatch)
	}
	if len(r1.Equal) != 1 || r1.Equal[0] != "device_id" {
		t.Errorf("rules[0].Equal = %v, want [device_id]", r1.Equal)
	}

	// 校验第二条规则
	r2 := rules[1]
	if r2.Name != "db-down-suppress-query-slow" {
		t.Errorf("rules[1].Name = %q, want %q", r2.Name, "db-down-suppress-query-slow")
	}
	if r2.SourceMatch["metric"] != "db_status" {
		t.Errorf("rules[1].SourceMatch = %v, want {metric:db_status, ...}", r2.SourceMatch)
	}

	// 验证加载的规则可构造抑制器并正常工作
	in := NewAlertInhibitor(rules)
	parent := newTestAlert("p1", "host_status", "dev-1", "critical", "firing")
	in.TrackActive(parent)
	child := newTestAlert("c1", "service_status", "dev-1", "warning", "firing")
	if !in.IsInhibited(child) {
		t.Fatal("加载的规则应抑制同主机服务告警")
	}
}

// TestLoadInhibitRules_EmptyFile 验证空数组返回空规则切片 + nil error。
func TestLoadInhibitRules_EmptyFile(t *testing.T) {
	path := writeTempJSON(t, "empty.json", "[]")

	rules, err := LoadInhibitRules(path)
	if err != nil {
		t.Fatalf("LoadInhibitRules 失败: %v", err)
	}
	if len(rules) != 0 {
		t.Fatalf("len(rules) = %d, want 0", len(rules))
	}
}

// TestLoadInhibitRules_InvalidJSON 验证无效 JSON 返回 error。
func TestLoadInhibitRules_InvalidJSON(t *testing.T) {
	path := writeTempJSON(t, "invalid.json", `{not valid json`)

	rules, err := LoadInhibitRules(path)
	if err == nil {
		t.Fatal("LoadInhibitRules 应返回 error（无效 JSON）")
	}
	if rules != nil {
		t.Fatalf("rules 应为 nil，实际 %v", rules)
	}
}

// TestLoadInhibitRules_NonExistentFile 验证文件不存在返回 error。
func TestLoadInhibitRules_NonExistentFile(t *testing.T) {
	_, err := LoadInhibitRules(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err == nil {
		t.Fatal("LoadInhibitRules 应返回 error（文件不存在）")
	}
}

// TestLoadInhibitRules_EmptyPath 验证空路径返回 nil, nil（不视为错误）。
func TestLoadInhibitRules_EmptyPath(t *testing.T) {
	rules, err := LoadInhibitRules("")
	if err != nil {
		t.Fatalf("LoadInhibitRules(\"\") err = %v, want nil", err)
	}
	if rules != nil {
		t.Fatalf("LoadInhibitRules(\"\") rules = %v, want nil", rules)
	}
}

// TestLoadInhibitRules_SingleRule 验证单条规则加载。
func TestLoadInhibitRules_SingleRule(t *testing.T) {
	jsonContent := `[
  {
    "name": "single-rule",
    "source_match": {"severity": "critical"},
    "target_match": {"severity": "warning"},
    "equal": []
  }
]`
	path := writeTempJSON(t, "single.json", jsonContent)

	rules, err := LoadInhibitRules(path)
	if err != nil {
		t.Fatalf("LoadInhibitRules 失败: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("len(rules) = %d, want 1", len(rules))
	}
	if rules[0].Name != "single-rule" {
		t.Errorf("rules[0].Name = %q, want %q", rules[0].Name, "single-rule")
	}
	if len(rules[0].Equal) != 0 {
		t.Errorf("rules[0].Equal = %v, want empty", rules[0].Equal)
	}
}

// TestLoadInhibitRules_RulesIsolated 验证返回的规则切片与内部解析独立，
// 调用方修改不影响后续 LoadInhibitRules 调用（每次加载独立解析）。
func TestLoadInhibitRules_RulesIsolated(t *testing.T) {
	jsonContent := `[
  {"name": "r1", "source_match": {"k": "v"}, "target_match": {}, "equal": ["a"]}
]`
	path := writeTempJSON(t, "isolated.json", jsonContent)

	rules, err := LoadInhibitRules(path)
	if err != nil {
		t.Fatalf("LoadInhibitRules 失败: %v", err)
	}

	// 修改返回的规则
	rules[0].Name = "modified"
	rules[0].SourceMatch["k"] = "modified"

	// 重新加载，应得到原始值
	rules2, err := LoadInhibitRules(path)
	if err != nil {
		t.Fatalf("第二次 LoadInhibitRules 失败: %v", err)
	}
	if rules2[0].Name != "r1" {
		t.Errorf("第二次加载 rules[0].Name = %q, want %q（应独立解析）", rules2[0].Name, "r1")
	}
	if rules2[0].SourceMatch["k"] != "v" {
		t.Errorf("第二次加载 rules[0].SourceMatch[k] = %q, want %q（应独立解析）", rules2[0].SourceMatch["k"], "v")
	}
}
