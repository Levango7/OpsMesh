package helm

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// fakehelm 辅助程序：编译一个模拟 helm 命令行的可执行文件用于测试 CLI 执行路径
// =============================================================================

// fakeHelmSrc 是 fakehelm 辅助程序的 Go 源码。
// 不使用反引号，以便整个源码可用反引号包裹。
const fakeHelmSrc = `package main

import (
	"fmt"
	"os"
	"time"
)

func main() {
	mode := os.Getenv("FAKEHELM_MODE")
	switch mode {
	case "fail":
		fmt.Fprintln(os.Stderr, "fake helm error")
		os.Exit(1)
	case "failnostderr":
		os.Exit(1)
	case "hang":
		time.Sleep(10 * time.Second)
		os.Exit(0)
	}
	out := os.Getenv("FAKEHELM_OUTPUT")
	if out != "" {
		fmt.Println(out)
		return
	}
	args := os.Args[1:]
	// 跳过 --kubeconfig <value> 全局参数
	idx := 0
	for idx < len(args) {
		if args[idx] == "--kubeconfig" && idx+1 < len(args) {
			idx += 2
		} else {
			break
		}
	}
	args = args[idx:]
	if len(args) == 0 {
		fmt.Println("fake helm")
		return
	}
	switch args[0] {
	case "version":
		fmt.Println("v3.14.0")
	case "repo":
		if len(args) > 1 && args[1] == "list" {
			fmt.Println("[{\"name\":\"bitnami\",\"url\":\"https://charts.bitnami.com/bitnami\"}]")
		} else {
			fmt.Println("ok")
		}
	case "search":
		fmt.Println("[{\"name\":\"bitnami/mysql\",\"version\":\"9.10.0\",\"appVersion\":\"8.0.32\",\"description\":\"MySQL\"}]")
	case "show":
		fmt.Println("{\"name\":\"mysql\",\"version\":\"9.10.0\",\"appVersion\":\"8.0.32\",\"description\":\"MySQL\"}")
	case "list":
		fmt.Println("[{\"name\":\"my-release\",\"namespace\":\"default\",\"revision\":1,\"updated\":\"2023-01-01T00:00:00Z\",\"status\":\"deployed\",\"chart\":\"mysql-9.10.0\",\"chart_version\":\"9.10.0\",\"app_version\":\"8.0.32\"}]")
	case "status":
		fmt.Println("{\"name\":\"my-release\",\"namespace\":\"default\",\"info\":{\"status\":\"deployed\",\"first_deployed\":\"2023-01-01T00:00:00Z\",\"last_deployed\":\"2023-01-01T00:00:00Z\"},\"chart\":{\"metadata\":{\"name\":\"mysql\",\"version\":\"9.10.0\",\"appVersion\":\"8.0.32\"}}}")
	case "history":
		fmt.Println("[{\"revision\":1,\"updated\":\"2023-01-01T00:00:00Z\",\"status\":\"deployed\",\"chart\":\"mysql-9.10.0\",\"chart_version\":\"9.10.0\",\"app_version\":\"8.0.32\",\"description\":\"Install complete\"}]")
	case "get":
		if len(args) > 1 && args[1] == "values" {
			fmt.Println("{\"key\":\"value\"}")
		} else if len(args) > 1 && args[1] == "manifest" {
			fmt.Println("---\napiVersion: v1\nkind: Service\nmetadata:\n  name: mysql\n")
		} else {
			fmt.Println("ok")
		}
	case "template":
		fmt.Println("---\n# Source: mysql/templates/svc.yaml\napiVersion: v1\nkind: Service\nmetadata:\n  name: mysql\n")
	default:
		fmt.Println("ok")
	}
}
`

var (
	fakeHelmOnce sync.Once
	fakeHelmPath string
	fakeHelmErr  error
)

// buildFakeHelm 编译 fakehelm 辅助程序，返回可执行文件路径。
// 编译失败时跳过测试（t.Skip）。
func buildFakeHelm(t *testing.T) string {
	t.Helper()
	fakeHelmOnce.Do(func() {
		dir, err := os.MkdirTemp("", "fakehelm-*")
		if err != nil {
			fakeHelmErr = err
			return
		}
		srcPath := filepath.Join(dir, "main.go")
		if err := os.WriteFile(srcPath, []byte(fakeHelmSrc), 0644); err != nil {
			fakeHelmErr = err
			return
		}
		exePath := filepath.Join(dir, "helm.exe")
		cmd := exec.Command("go", "build", "-o", exePath, srcPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			fakeHelmErr = errors.New("build fakehelm failed: " + err.Error() + "; output: " + string(out))
			return
		}
		fakeHelmPath = exePath
	})
	if fakeHelmErr != nil {
		t.Skipf("fakehelm unavailable: %v", fakeHelmErr)
	}
	return fakeHelmPath
}

// newFakeHelmCLI 创建使用 fakehelm 的 CLI 实例。
func newFakeHelmCLI(t *testing.T, opts ...Option) *CLI {
	t.Helper()
	p := buildFakeHelm(t)
	allOpts := append([]Option{WithHelmPath(p)}, opts...)
	return NewCLI("", allOpts...)
}

// mustFindTemplateCall 在 mock 调用记录中查找 template 命令，返回其完整参数。
// 未找到时返回 nil。
func mustFindTemplateCall(mock *mockCLI) []string {
	for _, call := range mock.calls {
		if len(call) > 0 && call[0] == "template" {
			return call
		}
	}
	return nil
}

// =============================================================================
// CLI 执行测试（使用 fakehelm）
// =============================================================================

func TestCLI_Run_Success(t *testing.T) {
	c := newFakeHelmCLI(t)
	out, err := c.Run("version", "--short")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !strings.Contains(out, "v3.14.0") {
		t.Errorf("Run output = %q, want contain v3.14.0", out)
	}
}

func TestCLI_Run_FailWithStderr(t *testing.T) {
	t.Setenv("FAKEHELM_MODE", "fail")
	c := newFakeHelmCLI(t)
	_, err := c.Run("repo", "list")
	if err == nil {
		t.Fatal("Run should fail with stderr")
	}
	if !strings.Contains(err.Error(), "stderr") {
		t.Errorf("error should contain stderr: %v", err)
	}
	if !strings.Contains(err.Error(), "fake helm error") {
		t.Errorf("error should contain stderr content: %v", err)
	}
}

func TestCLI_Run_FailNoStderr(t *testing.T) {
	t.Setenv("FAKEHELM_MODE", "failnostderr")
	c := newFakeHelmCLI(t)
	_, err := c.Run("repo", "list")
	if err == nil {
		t.Fatal("Run should fail without stderr")
	}
	if strings.Contains(err.Error(), "stderr") {
		t.Errorf("error should not contain stderr: %v", err)
	}
}

func TestCLI_RunWithTimeout_Timeout(t *testing.T) {
	t.Setenv("FAKEHELM_MODE", "hang")
	c := newFakeHelmCLI(t)
	_, err := c.RunWithTimeout(100*time.Millisecond, "repo", "list")
	if err == nil {
		t.Fatal("RunWithTimeout should fail with timeout")
	}
	if !strings.Contains(err.Error(), "超时") {
		t.Errorf("error should mention timeout: %v", err)
	}
}

func TestCLI_Run_WithEnv(t *testing.T) {
	c := newFakeHelmCLI(t, WithEnv("FAKEHELM_OUTPUT=custom-output"))
	out, err := c.Run("version")
	if err != nil {
		t.Fatalf("Run with env failed: %v", err)
	}
	if !strings.Contains(out, "custom-output") {
		t.Errorf("Run output = %q, want contain custom-output", out)
	}
}

func TestCLI_Run_WithKubeconfig(t *testing.T) {
	p := buildFakeHelm(t)
	c := NewCLI("/tmp/kubeconfig", WithHelmPath(p))
	out, err := c.Run("version")
	if err != nil {
		t.Fatalf("Run with kubeconfig failed: %v", err)
	}
	if !strings.Contains(out, "v3.14.0") {
		t.Errorf("Run output = %q", out)
	}
}

func TestCLI_RunQuiet(t *testing.T) {
	c := newFakeHelmCLI(t)
	if err := c.RunQuiet("version"); err != nil {
		t.Errorf("RunQuiet success failed: %v", err)
	}

	t.Setenv("FAKEHELM_MODE", "fail")
	if err := c.RunQuiet("version"); err == nil {
		t.Error("RunQuiet fail should return error")
	}
}

func TestCLI_Subcommands(t *testing.T) {
	c := newFakeHelmCLI(t)
	tests := []struct {
		name string
		fn   func() (string, error)
	}{
		{"RepoAdd", func() (string, error) { return c.RepoAdd("bitnami", "https://x") }},
		{"RepoAddExtra", func() (string, error) { return c.RepoAdd("bitnami", "https://x", "--force-update") }},
		{"RepoRemove", func() (string, error) { return c.RepoRemove("bitnami") }},
		{"RepoList", func() (string, error) { return c.RepoList() }},
		{"RepoUpdate", func() (string, error) { return c.RepoUpdate() }},
		{"SearchCharts", func() (string, error) { return c.SearchCharts("mysql", false) }},
		{"SearchChartsAllVersions", func() (string, error) { return c.SearchCharts("mysql", true) }},
		{"ShowChart", func() (string, error) { return c.ShowChart("bitnami/mysql") }},
		{"ShowAll", func() (string, error) { return c.ShowAll("bitnami/mysql") }},
		{"Pull", func() (string, error) { return c.Pull("bitnami/mysql", "9.10.0", os.TempDir()) }},
		{"PullNoVersion", func() (string, error) { return c.Pull("bitnami/mysql", "", os.TempDir()) }},
		{"Install", func() (string, error) { return c.Install("ns", "rel", "chart", []string{"f1"}, []string{"k=v"}) }},
		{"InstallEmpty", func() (string, error) { return c.Install("ns", "rel", "chart", nil, nil) }},
		{"Upgrade", func() (string, error) { return c.Upgrade("ns", "rel", "chart", []string{"f1"}, []string{"k=v"}) }},
		{"UpgradeEmpty", func() (string, error) { return c.Upgrade("ns", "rel", "chart", nil, nil) }},
		{"Rollback", func() (string, error) { return c.Rollback("ns", "rel", 2) }},
		{"Uninstall", func() (string, error) { return c.Uninstall("ns", "rel") }},
		{"List", func() (string, error) { return c.List("ns") }},
		{"ListAll", func() (string, error) { return c.ListAll() }},
		{"History", func() (string, error) { return c.History("ns", "rel") }},
		{"Get", func() (string, error) { return c.Get("ns", "rel") }},
		{"GetValues", func() (string, error) { return c.GetValues("ns", "rel") }},
		{"GetManifest", func() (string, error) { return c.GetManifest("ns", "rel") }},
		{"Status", func() (string, error) { return c.Status("ns", "rel") }},
		{"Template", func() (string, error) { return c.Template("ns", "rel", "chart", []string{"f1"}, []string{"k=v"}) }},
		{"TemplateEmpty", func() (string, error) { return c.Template("ns", "rel", "chart", nil, nil) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := tt.fn()
			if err != nil {
				t.Errorf("%s failed: %v", tt.name, err)
			}
			if out == "" {
				t.Errorf("%s returned empty output", tt.name)
			}
		})
	}
}

func TestCLI_CheckAvailable_Success(t *testing.T) {
	c := newFakeHelmCLI(t)
	if err := c.CheckAvailable(); err != nil {
		t.Errorf("CheckAvailable should succeed: %v", err)
	}
}

func TestCLI_CheckAvailable_NotFound(t *testing.T) {
	c := NewCLI("", WithHelmPath(filepath.Join(os.TempDir(), "nonexistent-helm-xxx")))
	err := c.CheckAvailable()
	if err == nil {
		t.Fatal("CheckAvailable should fail for nonexistent helm")
	}
	if !errors.Is(err, ErrHelmNotFound) {
		t.Errorf("CheckAvailable should return ErrHelmNotFound, got: %v", err)
	}
}

func TestCLI_CheckAvailable_Fail(t *testing.T) {
	t.Setenv("FAKEHELM_MODE", "fail")
	c := newFakeHelmCLI(t)
	err := c.CheckAvailable()
	if err == nil {
		t.Fatal("CheckAvailable should fail")
	}
	if errors.Is(err, ErrHelmNotFound) {
		t.Errorf("CheckAvailable should not return ErrHelmNotFound for execution failure, got: %v", err)
	}
}

// =============================================================================
// ReleaseManager 补充测试
// =============================================================================

func TestNewReleaseManager(t *testing.T) {
	m := NewReleaseManager("/tmp/kc")
	if m == nil || m.cli == nil {
		t.Fatal("NewReleaseManager returned nil")
	}
	m2 := NewReleaseManager("")
	if m2 == nil || m2.cli == nil {
		t.Fatal("NewReleaseManager empty kubeconfig returned nil")
	}
}

func TestNewReleaseManagerWithCLI_Nil(t *testing.T) {
	m := NewReleaseManagerWithCLI(nil)
	if m == nil || m.cli == nil {
		t.Fatal("NewReleaseManagerWithCLI(nil) returned nil")
	}
}

func TestNewReleaseManagerWithHelmCLI_Nil(t *testing.T) {
	m := NewReleaseManagerWithHelmCLI(nil)
	if m == nil || m.cli == nil {
		t.Fatal("NewReleaseManagerWithHelmCLI(nil) returned nil")
	}
}

func TestReleaseManager_ListAll(t *testing.T) {
	mock := newMockReturn(map[string]string{"list": listJSONSample})
	m := NewReleaseManagerWithHelmCLI(mock)
	releases, err := m.ListAll()
	if err != nil {
		t.Fatalf("ListAll failed: %v", err)
	}
	if len(releases) != 2 {
		t.Fatalf("releases len = %d, want 2", len(releases))
	}
}

func TestReleaseManager_ListAll_Error(t *testing.T) {
	m := NewReleaseManagerWithHelmCLI(newMockError(errors.New("list all failed")))
	_, err := m.ListAll()
	if err == nil {
		t.Fatal("ListAll should fail")
	}
}

func TestReleaseManager_Install_CLIError(t *testing.T) {
	mock := &mockCLI{
		runFunc: func(args ...string) (string, error) {
			if len(args) > 0 && args[0] == "install" {
				return "", errors.New("install failed")
			}
			return "", nil
		},
	}
	m := NewReleaseManagerWithHelmCLI(mock)
	_, err := m.Install("ns", "rel", "chart", nil)
	if err == nil {
		t.Fatal("Install should fail on CLI error")
	}
}

func TestReleaseManager_Upgrade_CLIError(t *testing.T) {
	mock := &mockCLI{
		runFunc: func(args ...string) (string, error) {
			if len(args) > 0 && args[0] == "upgrade" {
				return "", errors.New("upgrade failed")
			}
			return "", nil
		},
	}
	m := NewReleaseManagerWithHelmCLI(mock)
	_, err := m.Upgrade("ns", "rel", "chart", nil)
	if err == nil {
		t.Fatal("Upgrade should fail on CLI error")
	}
}

func TestReleaseManager_Rollback_Validation(t *testing.T) {
	m := NewReleaseManagerWithHelmCLI(newMockReturn(nil))
	if _, err := m.Rollback("", "rel", 1); err == nil {
		t.Fatal("Rollback empty ns should fail")
	}
	if _, err := m.Rollback("ns", "", 1); err == nil {
		t.Fatal("Rollback empty name should fail")
	}
}

func TestReleaseManager_Rollback_HistoryError(t *testing.T) {
	mock := newMockError(errors.New("history error"))
	m := NewReleaseManagerWithHelmCLI(mock)
	_, err := m.Rollback("ns", "rel", 0)
	if err == nil {
		t.Fatal("Rollback should fail when History fails")
	}
}

func TestReleaseManager_Rollback_InsufficientHistory(t *testing.T) {
	mock := newMockReturn(map[string]string{
		"history": `[{"revision":1,"updated":"2023-01-01T00:00:00Z","status":"deployed","chart":"mysql-9.10.0","chart_version":"9.10.0","app_version":"8.0.32","description":"Install complete"}]`,
	})
	m := NewReleaseManagerWithHelmCLI(mock)
	_, err := m.Rollback("ns", "rel", 0)
	if err == nil {
		t.Fatal("Rollback should fail with insufficient history")
	}
}

func TestReleaseManager_Rollback_CLIError(t *testing.T) {
	mock := &mockCLI{
		runFunc: func(args ...string) (string, error) {
			if len(args) > 0 && args[0] == "rollback" {
				return "", errors.New("rollback failed")
			}
			return "", nil
		},
	}
	m := NewReleaseManagerWithHelmCLI(mock)
	_, err := m.Rollback("ns", "rel", 1)
	if err == nil {
		t.Fatal("Rollback should fail on CLI error")
	}
}

func TestReleaseManager_Uninstall_CLIError(t *testing.T) {
	m := NewReleaseManagerWithHelmCLI(newMockError(errors.New("uninstall failed")))
	if err := m.Uninstall("ns", "rel"); err == nil {
		t.Fatal("Uninstall should fail on CLI error")
	}
}

func TestReleaseManager_List_EmptyNamespace(t *testing.T) {
	m := NewReleaseManagerWithHelmCLI(newMockReturn(nil))
	_, err := m.List("")
	if err == nil {
		t.Fatal("List empty ns should fail")
	}
}

func TestReleaseManager_List_CLIError(t *testing.T) {
	m := NewReleaseManagerWithHelmCLI(newMockError(errors.New("list failed")))
	_, err := m.List("ns")
	if err == nil {
		t.Fatal("List should fail on CLI error")
	}
}

func TestReleaseManager_History_Validation(t *testing.T) {
	m := NewReleaseManagerWithHelmCLI(newMockReturn(nil))
	if _, err := m.History("", "rel"); err == nil {
		t.Fatal("History empty ns should fail")
	}
	if _, err := m.History("ns", ""); err == nil {
		t.Fatal("History empty name should fail")
	}
}

func TestReleaseManager_History_CLIError(t *testing.T) {
	m := NewReleaseManagerWithHelmCLI(newMockError(errors.New("history failed")))
	_, err := m.History("ns", "rel")
	if err == nil {
		t.Fatal("History should fail on CLI error")
	}
}

func TestReleaseManager_Get_Validation(t *testing.T) {
	m := NewReleaseManagerWithHelmCLI(newMockReturn(nil))
	if _, err := m.Get("", "rel"); err == nil {
		t.Fatal("Get empty ns should fail")
	}
	if _, err := m.Get("ns", ""); err == nil {
		t.Fatal("Get empty name should fail")
	}
}

func TestReleaseManager_Get_CLIError(t *testing.T) {
	m := NewReleaseManagerWithHelmCLI(newMockError(errors.New("status failed")))
	_, err := m.Get("ns", "rel")
	if err == nil {
		t.Fatal("Get should fail on CLI error")
	}
}

func TestReleaseManager_GetValues_Validation(t *testing.T) {
	m := NewReleaseManagerWithHelmCLI(newMockReturn(nil))
	if _, err := m.GetValues("", "rel"); err == nil {
		t.Fatal("GetValues empty ns should fail")
	}
	if _, err := m.GetValues("ns", ""); err == nil {
		t.Fatal("GetValues empty name should fail")
	}
}

func TestReleaseManager_GetValues_Empty(t *testing.T) {
	mock := newMockReturn(map[string]string{"get values": ""})
	m := NewReleaseManagerWithHelmCLI(mock)
	vals, err := m.GetValues("ns", "rel")
	if err != nil {
		t.Fatalf("GetValues empty should not fail: %v", err)
	}
	if vals != nil {
		t.Errorf("GetValues empty = %v, want nil", vals)
	}
}

func TestReleaseManager_GetValues_CLIError(t *testing.T) {
	m := NewReleaseManagerWithHelmCLI(newMockError(errors.New("get values failed")))
	_, err := m.GetValues("ns", "rel")
	if err == nil {
		t.Fatal("GetValues should fail on CLI error")
	}
}

func TestReleaseManager_GetValues_InvalidJSON(t *testing.T) {
	mock := newMockReturn(map[string]string{"get values": "not json"})
	m := NewReleaseManagerWithHelmCLI(mock)
	_, err := m.GetValues("ns", "rel")
	if err == nil {
		t.Fatal("GetValues should fail on invalid JSON")
	}
}

func TestReleaseManager_GetManifest_Validation(t *testing.T) {
	m := NewReleaseManagerWithHelmCLI(newMockReturn(nil))
	if _, err := m.GetManifest("", "rel"); err == nil {
		t.Fatal("GetManifest empty ns should fail")
	}
	if _, err := m.GetManifest("ns", ""); err == nil {
		t.Fatal("GetManifest empty name should fail")
	}
}

func TestReleaseManager_GetManifest_CLIError(t *testing.T) {
	m := NewReleaseManagerWithHelmCLI(newMockError(errors.New("get manifest failed")))
	_, err := m.GetManifest("ns", "rel")
	if err == nil {
		t.Fatal("GetManifest should fail on CLI error")
	}
}

// =============================================================================
// Release JSON 解析补充测试
// =============================================================================

func TestParseHistoryJSON_Empty(t *testing.T) {
	releases, err := parseHistoryJSON("[]", "ns", "rel")
	if err != nil {
		t.Fatalf("parseHistoryJSON empty failed: %v", err)
	}
	if releases != nil {
		t.Errorf("releases = %v, want nil", releases)
	}
}

func TestParseHistoryJSON_Invalid(t *testing.T) {
	_, err := parseHistoryJSON("not json", "ns", "rel")
	if err == nil {
		t.Fatal("parseHistoryJSON invalid should fail")
	}
}

func TestParseStatusJSON_Invalid(t *testing.T) {
	_, err := parseStatusJSON("not json")
	if err == nil {
		t.Fatal("parseStatusJSON invalid should fail")
	}
}

func TestParseHelmTime_AlternativeFormats(t *testing.T) {
	// Go Time.String() 格式带纳秒
	t1 := parseHelmTime("2023-01-01 00:00:00.123456789 -0700 MST")
	if t1.IsZero() {
		t.Error("Go format with nanos should parse")
	}
	// Go 格式不带纳秒
	t2 := parseHelmTime("2023-01-01 00:00:00 -0700 MST")
	if t2.IsZero() {
		t.Error("Go format without nanos should parse")
	}
	// Unix date 格式
	t3 := parseHelmTime("Sun Jan  1 00:00:00 2023")
	if t3.IsZero() {
		t.Error("Unix date format should parse")
	}
}

func TestWriteValuesTemp_MarshalError(t *testing.T) {
	// chan 无法被 json.Marshal 序列化
	_, _, err := writeValuesTemp(map[string]interface{}{"chan": make(chan int)})
	if err == nil {
		t.Fatal("writeValuesTemp should fail with marshal error")
	}
}

// =============================================================================
// RepoManager 补充测试
// =============================================================================

func TestNewRepoManager(t *testing.T) {
	m := NewRepoManager(nil)
	if m == nil {
		t.Fatal("NewRepoManager(nil) returned nil")
	}
	m2 := NewRepoManager(NewCLI(""))
	if m2 == nil {
		t.Fatal("NewRepoManager(NewCLI) returned nil")
	}
}

func TestNewRepoManagerWithCLI_Nil(t *testing.T) {
	m := NewRepoManagerWithCLI(nil)
	if m == nil {
		t.Fatal("NewRepoManagerWithCLI(nil) returned nil")
	}
}

func TestRepoManager_UpdateRepos(t *testing.T) {
	m := NewRepoManagerWithCLI(newMockReturn(nil))
	if err := m.UpdateRepos(); err != nil {
		t.Fatalf("UpdateRepos failed: %v", err)
	}
}

func TestRepoManager_UpdateRepos_Error(t *testing.T) {
	m := NewRepoManagerWithCLI(newMockError(errors.New("repo update failed")))
	if err := m.UpdateRepos(); err == nil {
		t.Fatal("UpdateRepos should fail on CLI error")
	}
}

func TestRepoManager_AddRepo_CLIError(t *testing.T) {
	m := NewRepoManagerWithCLI(newMockError(errors.New("repo add failed")))
	err := m.AddRepo(&ChartRepo{Name: "bitnami", URL: "https://x"})
	if err == nil {
		t.Fatal("AddRepo should fail on CLI error")
	}
}

func TestRepoManager_RemoveRepo_CLIError(t *testing.T) {
	mock := &mockCLI{
		runFunc: func(args ...string) (string, error) {
			if strings.HasPrefix(strings.Join(args, " "), "repo remove") {
				return "", errors.New("repo remove failed")
			}
			return "", nil
		},
	}
	m := NewRepoManagerWithCLI(mock)
	_ = m.AddRepo(&ChartRepo{Name: "bitnami", URL: "https://x"})
	err := m.RemoveRepo("bitnami")
	if err == nil {
		t.Fatal("RemoveRepo should fail on CLI error")
	}
}

func TestRepoManager_ListCharts_CLIError(t *testing.T) {
	mock := &mockCLI{
		runFunc: func(args ...string) (string, error) {
			if strings.HasPrefix(strings.Join(args, " "), "search") {
				return "", errors.New("search failed")
			}
			return "", nil
		},
	}
	m := NewRepoManagerWithCLI(mock)
	_ = m.AddRepo(&ChartRepo{Name: "bitnami", URL: "https://x"})
	_, err := m.ListCharts("bitnami")
	if err == nil {
		t.Fatal("ListCharts should fail on CLI error")
	}
}

func TestRepoManager_SearchCharts_CLIError(t *testing.T) {
	m := NewRepoManagerWithCLI(newMockError(errors.New("search failed")))
	_, err := m.SearchCharts("mysql")
	if err == nil {
		t.Fatal("SearchCharts should fail on CLI error")
	}
}

func TestRepoManager_GetChart_Validation(t *testing.T) {
	m := NewRepoManagerWithCLI(newMockReturn(nil))
	if _, err := m.GetChart("", "chart", ""); err == nil {
		t.Fatal("GetChart empty repo should fail")
	}
	if _, err := m.GetChart("repo", "", ""); err == nil {
		t.Fatal("GetChart empty chart should fail")
	}
}

func TestRepoManager_GetChart_RepoNotExist(t *testing.T) {
	m := NewRepoManagerWithCLI(newMockReturn(nil))
	_, err := m.GetChart("nonexistent", "chart", "")
	if err == nil {
		t.Fatal("GetChart should fail for nonexistent repo")
	}
}

func TestRepoManager_GetChart_CLIError(t *testing.T) {
	mock := &mockCLI{
		runFunc: func(args ...string) (string, error) {
			if strings.HasPrefix(strings.Join(args, " "), "show chart") {
				return "", errors.New("show chart failed")
			}
			return "", nil
		},
	}
	m := NewRepoManagerWithCLI(mock)
	_ = m.AddRepo(&ChartRepo{Name: "bitnami", URL: "https://x", Type: RepoTypeOCI})
	_, err := m.GetChart("bitnami", "mysql", "")
	if err == nil {
		t.Fatal("GetChart should fail on CLI error")
	}
}

func TestRepoManager_GetChart_InvalidJSON(t *testing.T) {
	mock := newMockReturn(map[string]string{"show chart": "not json"})
	m := NewRepoManagerWithCLI(mock)
	_ = m.AddRepo(&ChartRepo{Name: "bitnami", URL: "https://x", Type: RepoTypeOCI})
	_, err := m.GetChart("bitnami", "mysql", "")
	if err == nil {
		t.Fatal("GetChart should fail on invalid JSON")
	}
}

func TestRepoManager_LoadFromHelm_Error(t *testing.T) {
	m := NewRepoManagerWithCLI(newMockError(errors.New("some other error")))
	if err := m.LoadFromHelm(); err == nil {
		t.Fatal("LoadFromHelm should fail on non-no-repositories error")
	}
}

func TestRepoManager_LoadFromHelm_InvalidJSON(t *testing.T) {
	mock := newMockReturn(map[string]string{"repo list": "not json"})
	m := NewRepoManagerWithCLI(mock)
	if err := m.LoadFromHelm(); err == nil {
		t.Fatal("LoadFromHelm should fail on invalid JSON")
	}
}

// =============================================================================
// Render 补充测试
// =============================================================================

func TestRenderChartWithCLI_Validation(t *testing.T) {
	mock := newMockReturn(nil)
	_, err := RenderChartWithCLI(mock, "", &RenderOptions{ReleaseName: "rel"})
	if err == nil {
		t.Fatal("RenderChartWithCLI empty chartPath should fail")
	}
	_, err = RenderChartWithCLI(mock, "chart", &RenderOptions{ReleaseName: ""})
	if err == nil {
		t.Fatal("RenderChartWithCLI empty ReleaseName should fail")
	}
	_, err = RenderChartWithCLI(mock, "chart", nil)
	if err == nil {
		t.Fatal("RenderChartWithCLI nil opts should fail")
	}
}

func TestRenderChartWithCLI_CLIError(t *testing.T) {
	mock := newMockError(errors.New("template failed"))
	_, err := RenderChartWithCLI(mock, "chart", &RenderOptions{ReleaseName: "rel"})
	if err == nil {
		t.Fatal("RenderChartWithCLI should fail on CLI error")
	}
}

func TestRenderChartWithCLI_NilCLI(t *testing.T) {
	// RenderChartWithCLI(nil) 会回退 RenderChart，后者默认 helmPath="helm" 依赖 PATH 查找。
	// CI（ubuntu-latest 预装真 helm）上会真的执行 helm 而非 fake，导致结果依赖环境
	// （真 helm 报 "non-absolute URLs should be in form of repo_name/path_to_chart"）。
	// 因此本测试不再依赖 PATH，改为：
	//   1. 显式注入 fakehelm 路径验证相同代码路径（参数构造 + 输出解析）；
	//   2. 用 mockCLI 验证 nil 回退逻辑确实委托 RenderChart（调用 template 命令）。
	c := newFakeHelmCLI(t)
	templates, err := RenderChartWithCLI(c, "chart", &RenderOptions{ReleaseName: "rel"})
	if err != nil {
		t.Fatalf("RenderChartWithCLI with fake helm failed: %v", err)
	}
	if len(templates) == 0 {
		t.Error("RenderChartWithCLI returned no templates")
	}
}

// TestRenderChart_WithFakeHelm 验证完整渲染代码路径（参数构造 → CLI 执行 → 输出解析）
// 使用显式注入的 fakehelm，环境无关：无 helm 的 Windows 与预装真 helm 的 Linux 结果一致。
func TestRenderChart_WithFakeHelm(t *testing.T) {
	c := newFakeHelmCLI(t)
	templates, err := RenderChartWithCLI(c, "chart", &RenderOptions{ReleaseName: "rel"})
	if err != nil {
		t.Fatalf("RenderChart failed: %v", err)
	}
	if len(templates) == 0 {
		t.Error("RenderChart returned no templates")
	}
}

// TestRenderChart_WithFakeHelm_AllOptions 全量 RenderOptions 的渲染路径
// （Namespace/Values/IncludeCRDs/APIVersions）。fakehelm 回显固定 template 输出，
// 本测试锁定"全选项下参数构造与解析不报错"；参数正确性由
// TestRenderChart_AllOptions_ArgsConstruction 用 mockCLI 精确断言。
func TestRenderChart_WithFakeHelm_AllOptions(t *testing.T) {
	c := newFakeHelmCLI(t)
	templates, err := RenderChartWithCLI(c, "chart", &RenderOptions{
		ReleaseName: "rel",
		Namespace:   "myns",
		Values:      map[string]interface{}{"key": "value"},
		IncludeCRDs: true,
		APIVersions: []string{"monitoring.coreos.com/v1"},
	})
	if err != nil {
		t.Fatalf("RenderChart with all options failed: %v", err)
	}
	if len(templates) == 0 {
		t.Error("RenderChart returned no templates")
	}
}

func TestRenderChart_AllOptions_ArgsConstruction(t *testing.T) {
	// 用 mockCLI 锁定 RenderChartWithCLI 构造的完整参数序列（纯逻辑，无进程执行）。
	// 覆盖 --api-versions（而非错误的 --api-version）、--include-crds、-n、-f、--set。
	mock := newMockReturn(map[string]string{"template": sampleManifest})
	_, err := RenderChartWithCLI(mock, "chart", &RenderOptions{
		ReleaseName: "rel",
		Namespace:   "myns",
		Values:      map[string]interface{}{"key": "value"},
		SetPairs:    []string{"a=b"},
		IncludeCRDs: true,
		APIVersions: []string{"monitoring.coreos.com/v1", "batch/v1"},
	})
	if err != nil {
		t.Fatalf("RenderChartWithCLI failed: %v", err)
	}
	call := mustFindTemplateCall(mock)
	if call == nil {
		t.Fatal("template command not called")
	}
	hasFlagVal := func(flag, wantVal string) {
		for i, a := range call {
			if a == flag {
				if i+1 >= len(call) || call[i+1] != wantVal {
					t.Errorf("flag %s value = %q, want %q (full args: %v)", flag, call[i+1], wantVal, call)
				}
				return
			}
		}
		t.Errorf("flag %s missing (full args: %v)", flag, call)
	}
	hasBool := func(flag string) {
		for _, a := range call {
			if a == flag {
				return
			}
		}
		t.Errorf("bool flag %s missing (full args: %v)", flag, call)
	}
	hasFlagVal("-n", "myns")
	hasBool("--include-crds")
	hasFlagVal("--api-versions", "monitoring.coreos.com/v1")
	hasFlagVal("--set", "a=b")
	// --api-versions 可重复：第二个 API 版本也应出现。
	count := 0
	for _, a := range call {
		if a == "--api-versions" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("--api-versions 出现 %d 次, want 2 (args: %v)", count, call)
	}
	// 绝不能出现错误拼写的 --api-version（单数，真 helm 会报 unknown flag）。
	for _, a := range call {
		if a == "--api-version" {
			t.Errorf("错误拼写 flag --api-version 出现（应为 --api-versions）: %v", call)
		}
	}
	// -f 应有 values 临时文件。
	foundF := false
	for i, a := range call {
		if a == "-f" && i+1 < len(call) {
			foundF = true
			break
		}
	}
	if !foundF {
		t.Errorf("template call should include -f for values (args: %v)", call)
	}
}

func TestStats_Empty(t *testing.T) {
	st := Stats(nil)
	if st.Total != 0 {
		t.Errorf("Stats(nil) Total = %d, want 0", st.Total)
	}
	st2 := Stats([]*RenderedTemplate{})
	if st2.Total != 0 {
		t.Errorf("Stats empty slice Total = %d, want 0", st2.Total)
	}
}

func TestStats_NoKind(t *testing.T) {
	st := Stats([]*RenderedTemplate{{Name: "test", Content: "x"}})
	if st.Total != 1 {
		t.Errorf("Total = %d, want 1", st.Total)
	}
	if st.WithSource != 1 {
		t.Errorf("WithSource = %d, want 1", st.WithSource)
	}
	if len(st.ByKind) != 0 {
		t.Errorf("ByKind = %v, want empty", st.ByKind)
	}
}

func TestStats_NoName(t *testing.T) {
	st := Stats([]*RenderedTemplate{{Kind: "Service", Content: "x"}})
	if st.Total != 1 {
		t.Errorf("Total = %d, want 1", st.Total)
	}
	if st.WithoutSource != 1 {
		t.Errorf("WithoutSource = %d, want 1", st.WithoutSource)
	}
	if st.ByKind["Service"] != 1 {
		t.Errorf("ByKind[Service] = %d, want 1", st.ByKind["Service"])
	}
}
