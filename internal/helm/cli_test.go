package helm

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// mockCLI：测试用的 HelmCLI mock 实现
// =============================================================================

// mockCLI 是 HelmCLI 接口的 mock 实现，用于测试 RepoManager 和 ReleaseManager。
//
// runFunc 是核心：所有方法构造 args 后调用 runFunc，runFunc 根据 args 返回预设输出或错误。
// calls 记录所有调用的 args 列表，便于断言。
type mockCLI struct {
	runFunc func(args ...string) (string, error)
	calls   [][]string
}

func (m *mockCLI) record(args ...string) {
	m.calls = append(m.calls, append([]string(nil), args...))
}

func (m *mockCLI) Run(args ...string) (string, error) {
	m.record(args...)
	return m.runFunc(args...)
}

func (m *mockCLI) RunWithTimeout(_ time.Duration, args ...string) (string, error) {
	m.record(args...)
	return m.runFunc(args...)
}

func (m *mockCLI) RepoAdd(name, url string, extraArgs ...string) (string, error) {
	args := append([]string{"repo", "add", name, url}, extraArgs...)
	return m.Run(args...)
}

func (m *mockCLI) RepoRemove(name string) (string, error) {
	return m.Run("repo", "remove", name)
}

func (m *mockCLI) RepoList() (string, error) {
	return m.Run("repo", "list", "-o", "json")
}

func (m *mockCLI) RepoUpdate() (string, error) {
	return m.Run("repo", "update")
}

func (m *mockCLI) SearchCharts(keyword string, allVersions bool) (string, error) {
	args := []string{"search", "repo", keyword, "-o", "json"}
	if allVersions {
		args = append(args, "--versions")
	}
	return m.Run(args...)
}

func (m *mockCLI) ShowChart(chart string) (string, error) {
	return m.Run("show", "chart", chart, "-o", "json")
}

func (m *mockCLI) Install(ns, name, chart string, vf, sp []string) (string, error) {
	args := []string{"install", name, chart, "-n", ns, "--create-namespace"}
	for _, f := range vf {
		args = append(args, "-f", f)
	}
	for _, kv := range sp {
		args = append(args, "--set", kv)
	}
	return m.Run(args...)
}

func (m *mockCLI) Upgrade(ns, name, chart string, vf, sp []string) (string, error) {
	args := []string{"upgrade", name, chart, "-n", ns, "--install", "--create-namespace"}
	for _, f := range vf {
		args = append(args, "-f", f)
	}
	for _, kv := range sp {
		args = append(args, "--set", kv)
	}
	return m.Run(args...)
}

func (m *mockCLI) Rollback(ns, name string, rev int) (string, error) {
	return m.Run("rollback", name, itoa(rev), "-n", ns)
}

func (m *mockCLI) Uninstall(ns, name string) (string, error) {
	return m.Run("uninstall", name, "-n", ns)
}

func (m *mockCLI) List(ns string) (string, error) {
	return m.Run("list", "-n", ns, "-o", "json")
}

func (m *mockCLI) ListAll() (string, error) {
	return m.Run("list", "-A", "-o", "json")
}

func (m *mockCLI) History(ns, name string) (string, error) {
	return m.Run("history", name, "-n", ns, "-o", "json")
}

func (m *mockCLI) Get(ns, name string) (string, error) {
	return m.Run("get", "all", name, "-n", ns)
}

func (m *mockCLI) GetValues(ns, name string) (string, error) {
	return m.Run("get", "values", name, "-n", ns, "-o", "json")
}

func (m *mockCLI) GetManifest(ns, name string) (string, error) {
	return m.Run("get", "manifest", name, "-n", ns)
}

func (m *mockCLI) Status(ns, name string) (string, error) {
	return m.Run("status", name, "-n", ns, "-o", "json")
}

func (m *mockCLI) Template(ns, name, chart string, vf, sp []string) (string, error) {
	args := []string{"template", name, chart, "-n", ns}
	for _, f := range vf {
		args = append(args, "-f", f)
	}
	for _, kv := range sp {
		args = append(args, "--set", kv)
	}
	return m.Run(args...)
}

// itoa 简易 int -> string（避免引入 strconv 仅为此用）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// newMockReturn 创建一个按命令前缀返回预设输出的 mockCLI。
//
// rules 是 "命令前缀" -> "预设输出" 映射；若 args 拼接后以某前缀开头则返回对应输出。
// 未匹配时返回 ("", nil)。
func newMockReturn(rules map[string]string) *mockCLI {
	return &mockCLI{
		runFunc: func(args ...string) (string, error) {
			joined := strings.Join(args, " ")
			for prefix, out := range rules {
				if strings.HasPrefix(joined, prefix) {
					return out, nil
				}
			}
			return "", nil
		},
	}
}

// newMockError 创建一个总是返回指定错误的 mockCLI。
func newMockError(err error) *mockCLI {
	return &mockCLI{
		runFunc: func(args ...string) (string, error) {
			return "", err
		},
	}
}

// =============================================================================
// CLI 配置与参数构造测试
// =============================================================================

func TestNewCLI_Defaults(t *testing.T) {
	c := NewCLI("")
	if c.helmPath != defaultHelmPath {
		t.Errorf("helmPath = %q, want %q", c.helmPath, defaultHelmPath)
	}
	if c.timeout != defaultTimeout {
		t.Errorf("timeout = %v, want %v", c.timeout, defaultTimeout)
	}
	if c.kubeconfig != "" {
		t.Errorf("kubeconfig = %q, want empty", c.kubeconfig)
	}
}

func TestNewCLI_WithKubeconfig(t *testing.T) {
	c := NewCLI("/path/to/kubeconfig")
	if c.kubeconfig != "/path/to/kubeconfig" {
		t.Errorf("kubeconfig = %q, want /path/to/kubeconfig", c.kubeconfig)
	}
}

func TestWithHelmPath(t *testing.T) {
	c := NewCLI("", WithHelmPath("/usr/local/bin/helm"))
	if c.helmPath != "/usr/local/bin/helm" {
		t.Errorf("helmPath = %q, want /usr/local/bin/helm", c.helmPath)
	}
}

func TestWithTimeout(t *testing.T) {
	c := NewCLI("", WithTimeout(60*time.Second))
	if c.timeout != 60*time.Second {
		t.Errorf("timeout = %v, want 60s", c.timeout)
	}
}

func TestWithEnv(t *testing.T) {
	c := NewCLI("", WithEnv("KUBECONFIG=/tmp/kc", "HELM_DRIVER=secret"))
	if len(c.env) != 2 {
		t.Fatalf("env len = %d, want 2", len(c.env))
	}
	if c.env[0] != "KUBECONFIG=/tmp/kc" || c.env[1] != "HELM_DRIVER=secret" {
		t.Errorf("env = %v, want [KUBECONFIG=/tmp/kc, HELM_DRIVER=secret]", c.env)
	}
}

func TestCLI_EffectiveTimeout(t *testing.T) {
	tests := []struct {
		name    string
		cli     *CLI
		custom  time.Duration
		want    time.Duration
	}{
		{"custom overrides default", NewCLI(""), 60 * time.Second, 60 * time.Second},
		{"use cli timeout", NewCLI("", WithTimeout(120*time.Second)), 0, 120 * time.Second},
		{"fallback to default", NewCLI(""), 0, defaultTimeout},
		{"zero cli timeout fallback", &CLI{helmPath: "helm"}, 0, defaultTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cli.effectiveTimeout(tt.custom)
			if got != tt.want {
				t.Errorf("effectiveTimeout(%v) = %v, want %v", tt.custom, got, tt.want)
			}
		})
	}
}

func TestCLI_BuildArgs(t *testing.T) {
	// 有 kubeconfig：应注入 --kubeconfig 前缀。
	c := NewCLI("/tmp/kc")
	args := c.buildArgs("list", "-n", "default")
	want := []string{"--kubeconfig", "/tmp/kc", "list", "-n", "default"}
	if !equalStringSlices(args, want) {
		t.Errorf("buildArgs with kubeconfig = %v, want %v", args, want)
	}

	// 无 kubeconfig：不注入前缀。
	c2 := NewCLI("")
	args2 := c2.buildArgs("list", "-n", "default")
	want2 := []string{"list", "-n", "default"}
	if !equalStringSlices(args2, want2) {
		t.Errorf("buildArgs without kubeconfig = %v, want %v", args2, want2)
	}
}

func TestCLI_ImplementsHelmCLI(t *testing.T) {
	// 编译期接口适配性检查：*CLI 必须实现 HelmCLI。
	var _ HelmCLI = (*CLI)(nil)
	var _ HelmCLI = &mockCLI{}
}

func TestErrHelmNotFound(t *testing.T) {
	if ErrHelmNotFound == nil {
		t.Fatal("ErrHelmNotFound is nil")
	}
	if !errors.Is(ErrHelmNotFound, ErrHelmNotFound) {
		t.Fatal("ErrHelmNotFound not self-identical")
	}
}

// equalStringSlices 比较两个字符串切片相等性。
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}