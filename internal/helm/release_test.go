package helm

import (
	"os"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Release / ReleaseStatus 测试
// =============================================================================

func TestReleaseStatus_Constants(t *testing.T) {
	tests := []struct {
		name   string
		status ReleaseStatus
		want   string
	}{
		{"deployed", StatusDeployed, "deployed"},
		{"failed", StatusFailed, "failed"},
		{"pending-install", StatusPendingInstall, "pending-install"},
		{"pending-upgrade", StatusPendingUpgrade, "pending-upgrade"},
		{"pending-rollback", StatusPendingRollback, "pending-rollback"},
		{"superseded", StatusSuperseded, "superseded"},
		{"uninstalled", StatusUninstalled, "uninstalled"},
		{"uninstalling", StatusUninstalling, "uninstalling"},
	}
	for _, tt := range tests {
		if string(tt.status) != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.status, tt.want)
		}
	}
}

// =============================================================================
// ReleaseManager.Install / Upgrade / Rollback / Uninstall 测试
// =============================================================================

const statusJSONSample = `{
  "name": "my-release",
  "namespace": "default",
  "info": {
    "status": "deployed",
    "first_deployed": "2023-01-01T00:00:00Z",
    "last_deployed": "2023-01-01T00:01:00Z"
  },
  "chart": {
    "metadata": {
      "name": "mysql",
      "version": "9.10.0",
      "appVersion": "8.0.32"
    }
  }
}`

const listJSONSample = `[
  {"name":"my-release","namespace":"default","revision":1,"updated":"2023-01-01T00:00:00Z","status":"deployed","chart":"mysql-9.10.0","chart_version":"9.10.0","app_version":"8.0.32"},
  {"name":"other","namespace":"default","revision":3,"updated":"2023-01-02T00:00:00Z","status":"deployed","chart":"redis-18.0.0","chart_version":"18.0.0","app_version":"7.2.0"}
]`

const historyJSONSample = `[
  {"revision":1,"updated":"2023-01-01T00:00:00Z","status":"deployed","chart":"mysql-9.10.0","chart_version":"9.10.0","app_version":"8.0.32","description":"Install complete"},
  {"revision":2,"updated":"2023-01-01T01:00:00Z","status":"superseded","chart":"mysql-9.10.1","chart_version":"9.10.1","app_version":"8.0.33","description":"Upgrade complete"},
  {"revision":3,"updated":"2023-01-01T02:00:00Z","status":"deployed","chart":"mysql-9.10.2","chart_version":"9.10.2","app_version":"8.0.34","description":"Upgrade complete"}
]`

func TestReleaseManager_Install(t *testing.T) {
	mock := newMockReturn(map[string]string{
		"install": "Release installed",
		"status":  statusJSONSample,
	})
	m := NewReleaseManagerWithHelmCLI(mock)

	rel, err := m.Install("default", "my-release", "bitnami/mysql", map[string]interface{}{
		"auth": map[string]interface{}{"rootPassword": "secret"},
	})
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}
	if rel.Name != "my-release" {
		t.Errorf("Name = %q", rel.Name)
	}
	if rel.Status != "deployed" {
		t.Errorf("Status = %q", rel.Status)
	}
	if rel.Chart != "mysql" {
		t.Errorf("Chart = %q, want mysql", rel.Chart)
	}
	if rel.Version != "9.10.0" {
		t.Errorf("Version = %q", rel.Version)
	}

	// 验证 install 命令被调用（含 -f 临时文件）。
	foundInstall := false
	for _, call := range mock.calls {
		if len(call) > 0 && call[0] == "install" {
			foundInstall = true
			// 应包含 -f 参数。
			hasF := false
			for i, a := range call {
				if a == "-f" && i+1 < len(call) {
					hasF = true
					break
				}
			}
			if !hasF {
				t.Error("install call should include -f for values")
			}
			break
		}
	}
	if !foundInstall {
		t.Error("install command not called")
	}
}

func TestReleaseManager_Install_NoValues(t *testing.T) {
	mock := newMockReturn(map[string]string{
		"install": "ok",
		"status":  statusJSONSample,
	})
	m := NewReleaseManagerWithHelmCLI(mock)

	_, err := m.Install("default", "rel", "chart", nil)
	if err != nil {
		t.Fatalf("Install with nil values failed: %v", err)
	}
}

func TestReleaseManager_Install_Validation(t *testing.T) {
	m := NewReleaseManagerWithHelmCLI(newMockReturn(nil))

	tests := []struct {
		name, ns, rel, chart string
	}{
		{"empty ns", "", "rel", "chart"},
		{"empty name", "ns", "", "chart"},
		{"empty chart", "ns", "rel", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := m.Install(tt.ns, tt.rel, tt.chart, nil)
			if err == nil {
				t.Fatal("Install should fail for invalid input")
			}
		})
	}
}

func TestReleaseManager_Upgrade(t *testing.T) {
	mock := newMockReturn(map[string]string{
		"upgrade": "Release upgraded",
		"status":  statusJSONSample,
	})
	m := NewReleaseManagerWithHelmCLI(mock)

	rel, err := m.Upgrade("default", "my-release", "bitnami/mysql", nil)
	if err != nil {
		t.Fatalf("Upgrade failed: %v", err)
	}
	if rel.Name != "my-release" {
		t.Errorf("Name = %q", rel.Name)
	}
}

func TestReleaseManager_Rollback_Explicit(t *testing.T) {
	mock := newMockReturn(map[string]string{
		"rollback": "Rollback to 1",
		"status":   statusJSONSample,
	})
	m := NewReleaseManagerWithHelmCLI(mock)

	rel, err := m.Rollback("default", "my-release", 1)
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}
	if rel.Status != "deployed" {
		t.Errorf("Status = %q", rel.Status)
	}
	// 验证 rollback 命令参数。
	foundRollback := false
	for _, call := range mock.calls {
		if len(call) > 0 && call[0] == "rollback" {
			foundRollback = true
			if call[1] != "my-release" || call[2] != "1" {
				t.Errorf("rollback args = %v", call)
			}
			break
		}
	}
	if !foundRollback {
		t.Error("rollback command not called")
	}
}

func TestReleaseManager_Rollback_AutoPrev(t *testing.T) {
	mock := newMockReturn(map[string]string{
		"history": historyJSONSample,
		"rollback": "ok",
		"status":   statusJSONSample,
	})
	m := NewReleaseManagerWithHelmCLI(mock)

	// revision=0 表示回滚到上一个版本（revision 2，因为当前是 3）。
	rel, err := m.Rollback("default", "my-release", 0)
	if err != nil {
		t.Fatalf("Rollback auto-prev failed: %v", err)
	}
	if rel.Name != "my-release" {
		t.Errorf("Name = %q", rel.Name)
	}
}

func TestReleaseManager_Uninstall(t *testing.T) {
	mock := newMockReturn(map[string]string{
		"uninstall": "Release uninstalled",
	})
	m := NewReleaseManagerWithHelmCLI(mock)

	if err := m.Uninstall("default", "my-release"); err != nil {
		t.Fatalf("Uninstall failed: %v", err)
	}
}

func TestReleaseManager_Uninstall_Validation(t *testing.T) {
	m := NewReleaseManagerWithHelmCLI(newMockReturn(nil))
	if err := m.Uninstall("", "x"); err == nil {
		t.Fatal("Uninstall empty ns should fail")
	}
	if err := m.Uninstall("x", ""); err == nil {
		t.Fatal("Uninstall empty name should fail")
	}
}

// =============================================================================
// List / History / Get 测试
// =============================================================================

func TestReleaseManager_List(t *testing.T) {
	mock := newMockReturn(map[string]string{"list": listJSONSample})
	m := NewReleaseManagerWithHelmCLI(mock)

	releases, err := m.List("default")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(releases) != 2 {
		t.Fatalf("releases len = %d, want 2", len(releases))
	}
	if releases[0].Name != "my-release" {
		t.Errorf("releases[0].Name = %q", releases[0].Name)
	}
	if releases[0].Chart != "mysql" {
		t.Errorf("releases[0].Chart = %q, want mysql", releases[0].Chart)
	}
	if releases[0].Version != "9.10.0" {
		t.Errorf("releases[0].Version = %q", releases[0].Version)
	}
	if releases[0].Revision != 1 {
		t.Errorf("releases[0].Revision = %d", releases[0].Revision)
	}
	if releases[1].Chart != "redis" {
		t.Errorf("releases[1].Chart = %q, want redis", releases[1].Chart)
	}
}

func TestReleaseManager_List_Empty(t *testing.T) {
	mock := newMockReturn(map[string]string{"list": "[]"})
	m := NewReleaseManagerWithHelmCLI(mock)

	releases, err := m.List("default")
	if err != nil {
		t.Fatalf("List empty failed: %v", err)
	}
	if len(releases) != 0 {
		t.Errorf("releases len = %d, want 0", len(releases))
	}
}

func TestReleaseManager_History(t *testing.T) {
	mock := newMockReturn(map[string]string{"history": historyJSONSample})
	m := NewReleaseManagerWithHelmCLI(mock)

	history, err := m.History("default", "my-release")
	if err != nil {
		t.Fatalf("History failed: %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("history len = %d, want 3", len(history))
	}
	if history[0].Revision != 1 {
		t.Errorf("history[0].Revision = %d", history[0].Revision)
	}
	if history[2].Status != "deployed" {
		t.Errorf("history[2].Status = %q", history[2].Status)
	}
	if history[0].Description != "Install complete" {
		t.Errorf("history[0].Description = %q", history[0].Description)
	}
}

func TestReleaseManager_Get(t *testing.T) {
	mock := newMockReturn(map[string]string{"status": statusJSONSample})
	m := NewReleaseManagerWithHelmCLI(mock)

	rel, err := m.Get("default", "my-release")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if rel.Name != "my-release" {
		t.Errorf("Name = %q", rel.Name)
	}
	if rel.Namespace != "default" {
		t.Errorf("Namespace = %q", rel.Namespace)
	}
	if rel.Chart != "mysql" {
		t.Errorf("Chart = %q", rel.Chart)
	}
	if rel.AppVersion != "8.0.32" {
		t.Errorf("AppVersion = %q", rel.AppVersion)
	}
	if rel.Status != "deployed" {
		t.Errorf("Status = %q", rel.Status)
	}
	if rel.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if rel.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero")
	}
}

func TestReleaseManager_GetValues(t *testing.T) {
	const valuesJSON = `{"auth":{"rootPassword":"secret"},"primary":{"persistence":{"size":"8Gi"}}}`
	mock := newMockReturn(map[string]string{"get values": valuesJSON})
	m := NewReleaseManagerWithHelmCLI(mock)

	vals, err := m.GetValues("default", "my-release")
	if err != nil {
		t.Fatalf("GetValues failed: %v", err)
	}
	auth, ok := vals["auth"].(map[string]interface{})
	if !ok {
		t.Fatalf("vals[auth] type = %T", vals["auth"])
	}
	if auth["rootPassword"] != "secret" {
		t.Errorf("rootPassword = %v", auth["rootPassword"])
	}
}

func TestReleaseManager_GetManifest(t *testing.T) {
	const manifest = "---\napiVersion: v1\nkind: Service\nmetadata:\n  name: mysql\n"
	mock := newMockReturn(map[string]string{"get manifest": manifest})
	m := NewReleaseManagerWithHelmCLI(mock)

	got, err := m.GetManifest("default", "my-release")
	if err != nil {
		t.Fatalf("GetManifest failed: %v", err)
	}
	if got != manifest {
		t.Errorf("GetManifest mismatch")
	}
}

// =============================================================================
// JSON 解析函数测试
// =============================================================================

func TestParseListJSON(t *testing.T) {
	releases, err := parseListJSON(listJSONSample)
	if err != nil {
		t.Fatalf("parseListJSON failed: %v", err)
	}
	if len(releases) != 2 {
		t.Fatalf("len = %d, want 2", len(releases))
	}
}

func TestParseListJSON_Empty(t *testing.T) {
	releases, err := parseListJSON("[]")
	if err != nil {
		t.Fatalf("parseListJSON empty failed: %v", err)
	}
	if releases != nil {
		t.Errorf("releases = %v, want nil", releases)
	}
}

func TestParseListJSON_Invalid(t *testing.T) {
	_, err := parseListJSON("not json")
	if err == nil {
		t.Fatal("parseListJSON invalid should fail")
	}
}

func TestParseHistoryJSON(t *testing.T) {
	releases, err := parseHistoryJSON(historyJSONSample, "default", "my-release")
	if err != nil {
		t.Fatalf("parseHistoryJSON failed: %v", err)
	}
	if len(releases) != 3 {
		t.Fatalf("len = %d, want 3", len(releases))
	}
	if releases[0].Name != "my-release" {
		t.Errorf("releases[0].Name = %q", releases[0].Name)
	}
}

func TestParseStatusJSON(t *testing.T) {
	rel, err := parseStatusJSON(statusJSONSample)
	if err != nil {
		t.Fatalf("parseStatusJSON failed: %v", err)
	}
	if rel.Name != "my-release" {
		t.Errorf("Name = %q", rel.Name)
	}
}

func TestParseStatusJSON_Empty(t *testing.T) {
	_, err := parseStatusJSON("")
	if err == nil {
		t.Fatal("parseStatusJSON empty should fail")
	}
}

// =============================================================================
// 辅助函数测试
// =============================================================================

func TestSplitChartVersion(t *testing.T) {
	tests := []struct {
		input   string
		name    string
		version string
	}{
		{"mysql-9.10.0", "mysql", "9.10.0"},
		{"redis-18.0.0", "redis", "18.0.0"},
		{"my-chart-1.2.3", "my-chart", "1.2.3"},
		{"plain", "plain", ""},
		{"", "", ""},
	}
	for _, tt := range tests {
		name, version := splitChartVersion(tt.input)
		if name != tt.name || version != tt.version {
			t.Errorf("splitChartVersion(%q) = (%q, %q), want (%q, %q)",
				tt.input, name, version, tt.name, tt.version)
		}
	}
}

func TestParseHelmTime(t *testing.T) {
	tests := []struct {
		input string
		zero  bool
	}{
		{"2023-01-01T00:00:00Z", false},
		{"2023-01-01T00:00:00+08:00", false},
		{"", true},
		{"invalid", true},
	}
	for _, tt := range tests {
		got := parseHelmTime(tt.input)
		if tt.zero {
			if !got.IsZero() {
				t.Errorf("parseHelmTime(%q) = %v, want zero", tt.input, got)
			}
		} else {
			if got.IsZero() {
				t.Errorf("parseHelmTime(%q) = zero, want non-zero", tt.input)
			}
		}
	}

	// 验证具体值。
	got := parseHelmTime("2023-01-01T00:00:00Z")
	want := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("parseHelmTime RFC3339 = %v, want %v", got, want)
	}
}

func TestValidateReleaseArgs(t *testing.T) {
	tests := []struct {
		ns, name, chart string
		wantErr         bool
	}{
		{"default", "rel", "chart", false},
		{"", "rel", "chart", true},
		{"default", "", "chart", true},
		{"default", "rel", "", true},
	}
	for _, tt := range tests {
		err := validateReleaseArgs(tt.ns, tt.name, tt.chart)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateReleaseArgs(%q,%q,%q) err = %v, wantErr = %v",
				tt.ns, tt.name, tt.chart, err, tt.wantErr)
		}
	}
}

func TestWriteValuesTemp(t *testing.T) {
	// nil values：不创建临时文件。
	files, cleanup, err := writeValuesTemp(nil)
	if err != nil {
		t.Fatalf("writeValuesTemp nil failed: %v", err)
	}
	if files != nil {
		t.Errorf("files = %v, want nil", files)
	}
	cleanup()

	// 有 values：创建临时文件。
	files, cleanup, err = writeValuesTemp(map[string]interface{}{"key": "value"})
	if err != nil {
		t.Fatalf("writeValuesTemp failed: %v", err)
	}
	defer cleanup()
	if len(files) != 1 {
		t.Fatalf("files len = %d, want 1", len(files))
	}
	// 验证文件存在（cleanup 未调用）。
	if _, err := os.Stat(files[0]); err != nil {
		t.Errorf("temp file should exist: %v", err)
	}
	// 验证文件内容。
	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read temp file failed: %v", err)
	}
	if !strings.Contains(string(content), "key") {
		t.Errorf("temp file content = %s, want contain key", content)
	}
}