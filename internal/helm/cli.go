// Package helm 实现 Helm 应用商店核心能力，作为自包含包提供 chart 仓库管理、
// 模板渲染、release 管理与预置应用商店。
//
// Helm 应用商店核心包：
//   - cli.go：封装 helm 命令行调用，支持 --kubeconfig、超时、JSON 输出解析；
//   - repo.go：Chart 仓库管理（add/remove/list/search/get）；
//   - release.go：Release 管理（install/upgrade/rollback/uninstall/list/history/get）；
//   - render.go：Chart 模板渲染（helm template 封装 + YAML 解析）；
//   - catalog.go：预置应用商店（20+ 常用应用，按分类组织）。
//
// 设计要点：
//   - 不依赖 helm Go SDK（太重），通过 exec.Command 调用 helm 命令行；
//   - 所有 helm 命令使用 -o json 获取结构化输出，便于解析；
//   - 所有命令默认 300s 超时，可通过 WithTimeout 覆盖；
//   - 自包含包，不修改 server.go 等外部文件。
package helm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// 默认配置常量。
const (
	// defaultHelmPath 默认 helm 可执行文件名（依赖 PATH 查找）。
	defaultHelmPath = "helm"
	// defaultTimeout 默认命令超时（5 分钟），与 Helm CLI 默认值一致。
	defaultTimeout = 300 * time.Second
)

// CLI 封装 helm 命令行调用，提供超时、kubeconfig 注入与输出捕获能力。
//
// 字段说明：
//   - helmPath：helm 可执行文件路径，默认 "helm"（依赖 PATH）；
//   - kubeconfig：--kubeconfig 参数值，空则不注入（使用 KUBECONFIG 环境变量或默认路径）；
//   - timeout：命令默认超时，零值表示使用 defaultTimeout；
//   - env：附加环境变量（KEY=VALUE 形式），用于注入 KUBECONFIG 等；
//   - mu：保护并发执行（exec.Command 自身并发安全，mu 用于未来扩展）。
type CLI struct {
	helmPath   string
	kubeconfig string
	timeout    time.Duration
	env        []string
	mu         sync.Mutex
}

// Option 是 CLI 的配置选项函数（函数选项模式）。
type Option func(*CLI)

// WithHelmPath 设置 helm 可执行文件路径。
func WithHelmPath(path string) Option {
	return func(c *CLI) { c.helmPath = path }
}

// WithTimeout 设置命令默认超时。
func WithTimeout(d time.Duration) Option {
	return func(c *CLI) { c.timeout = d }
}

// WithEnv 附加环境变量（KEY=VALUE 形式）。
func WithEnv(env ...string) Option {
	return func(c *CLI) { c.env = append(c.env, env...) }
}

// NewCLI 创建 CLI 实例。
//
// kubeconfig 为 --kubeconfig 参数值，空字符串表示不注入该参数
// （此时 helm 将使用 KUBECONFIG 环境变量或 ~/.kube/config 默认路径）。
func NewCLI(kubeconfig string, opts ...Option) *CLI {
	c := &CLI{
		helmPath:   defaultHelmPath,
		kubeconfig: kubeconfig,
		timeout:    defaultTimeout,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// HelmCLI 是 helm 命令行封装的接口，便于测试注入 mock。
//
// *CLI 实现该接口。RepoManager 和 ReleaseManager 持有此接口，
// 测试时可注入返回预设 JSON 的 mock 实现。
type HelmCLI interface {
	Run(args ...string) (string, error)
	RunWithTimeout(timeout time.Duration, args ...string) (string, error)
	RepoAdd(name, url string, extraArgs ...string) (string, error)
	RepoRemove(name string) (string, error)
	RepoList() (string, error)
	RepoUpdate() (string, error)
	SearchCharts(keyword string, allVersions bool) (string, error)
	ShowChart(chart string) (string, error)
	Install(namespace, name, chart string, valuesFiles, setPairs []string) (string, error)
	Upgrade(namespace, name, chart string, valuesFiles, setPairs []string) (string, error)
	Rollback(namespace, name string, revision int) (string, error)
	Uninstall(namespace, name string) (string, error)
	List(namespace string) (string, error)
	ListAll() (string, error)
	History(namespace, name string) (string, error)
	Get(namespace, name string) (string, error)
	GetValues(namespace, name string) (string, error)
	GetManifest(namespace, name string) (string, error)
	Status(namespace, name string) (string, error)
	Template(namespace, name, chart string, valuesFiles, setPairs []string) (string, error)
}

// effectiveTimeout 返回生效的超时 duration。
func (c *CLI) effectiveTimeout(custom time.Duration) time.Duration {
	if custom > 0 {
		return custom
	}
	if c.timeout > 0 {
		return c.timeout
	}
	return defaultTimeout
}

// buildArgs 构造 helm 命令参数，注入 --kubeconfig（若配置）。
func (c *CLI) buildArgs(args ...string) []string {
	out := make([]string, 0, len(args)+2)
	if c.kubeconfig != "" {
		out = append(out, "--kubeconfig", c.kubeconfig)
	}
	out = append(out, args...)
	return out
}

// Run 执行 helm 命令，返回 stdout 内容。
//
// 若命令退出码非 0，返回 *exec.ExitError 包装的错误（含 stderr）。
// 命令受 defaultTimeout 或 c.timeout 约束，超时返回 context.DeadlineExceeded。
func (c *CLI) Run(args ...string) (string, error) {
	return c.RunWithTimeout(0, args...)
}

// RunWithTimeout 执行 helm 命令，使用指定超时（<=0 使用 c.timeout）。
func (c *CLI) RunWithTimeout(timeout time.Duration, args ...string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	fullArgs := c.buildArgs(args...)
	ctx, cancel := context.WithTimeout(context.Background(), c.effectiveTimeout(timeout))
	defer cancel()

	cmd := exec.CommandContext(ctx, c.helmPath, fullArgs...)
	if len(c.env) > 0 {
		cmd.Env = append(cmd.Env, c.env...)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// 优先识别超时（ctx.Err() 已置位）。
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("helm: 命令 %q 执行超时（%s）: %w",
				strings.Join(fullArgs, " "), c.effectiveTimeout(timeout), ctx.Err())
		}
		// 退出码非 0：附加 stderr 帮助排障。
		stderrStr := strings.TrimSpace(stderr.String())
		if stderrStr != "" {
			return "", fmt.Errorf("helm: 命令 %q 失败: %w; stderr: %s",
				strings.Join(fullArgs, " "), err, stderrStr)
		}
		return "", fmt.Errorf("helm: 命令 %q 失败: %w",
			strings.Join(fullArgs, " "), err)
	}
	return stdout.String(), nil
}

// RunQuiet 执行 helm 命令，仅返回错误，不保留 stdout（用于 install/uninstall 等）。
func (c *CLI) RunQuiet(args ...string) error {
	_, err := c.Run(args...)
	return err
}

// =============================================================================
// helm 命令子命令封装（返回原始 stdout，由上层 Manager 解析）
// =============================================================================

// RepoAdd 执行 `helm repo add <name> <url>`。
func (c *CLI) RepoAdd(name, url string, extraArgs ...string) (string, error) {
	args := append([]string{"repo", "add", name, url}, extraArgs...)
	return c.Run(args...)
}

// RepoRemove 执行 `helm repo remove <name>`。
func (c *CLI) RepoRemove(name string) (string, error) {
	return c.Run("repo", "remove", name)
}

// RepoList 执行 `helm repo list -o json`，返回 JSON 字符串。
func (c *CLI) RepoList() (string, error) {
	return c.Run("repo", "list", "-o", "json")
}

// RepoUpdate 执行 `helm repo update`（更新所有仓库索引）。
func (c *CLI) RepoUpdate() (string, error) {
	return c.Run("repo", "update")
}

// SearchCharts 执行 `helm search repo <keyword> -o json`。
//
// allVersions 为 true 时附加 --versions 返回所有版本（默认仅返回最新版本）。
func (c *CLI) SearchCharts(keyword string, allVersions bool) (string, error) {
	args := []string{"search", "repo", keyword, "-o", "json"}
	if allVersions {
		args = append(args, "--versions")
	}
	return c.Run(args...)
}

// ShowChart 执行 `helm show chart <chart> -o json`，返回 Chart.yaml 的 JSON。
func (c *CLI) ShowChart(chart string) (string, error) {
	return c.Run("show", "chart", chart, "-o", "json")
}

// ShowAll 执行 `helm show all <chart>`，返回合并的 chart/values/readme。
func (c *CLI) ShowAll(chart string) (string, error) {
	return c.Run("show", "all", chart)
}

// Pull 执行 `helm pull <chart> --version <version> --untar --destination <dir>`。
//
// version 空字符串表示不指定版本（拉取最新）。
func (c *CLI) Pull(chart, version, destDir string) (string, error) {
	args := []string{"pull", chart, "--untar", "--destination", destDir}
	if version != "" {
		args = append(args, "--version", version)
	}
	return c.Run(args...)
}

// Install 执行 `helm install <name> <chart> -n <ns> -f <values...> --create-namespace`。
//
// valuesFiles 为 -f 参数列表；setPairs 为 --set 参数列表（KEY=VALUE）。
func (c *CLI) Install(namespace, name, chart string, valuesFiles, setPairs []string) (string, error) {
	args := []string{"install", name, chart, "-n", namespace, "--create-namespace"}
	for _, f := range valuesFiles {
		args = append(args, "-f", f)
	}
	for _, kv := range setPairs {
		args = append(args, "--set", kv)
	}
	return c.Run(args...)
}

// Upgrade 执行 `helm upgrade <name> <chart> -n <ns> --install ...`。
func (c *CLI) Upgrade(namespace, name, chart string, valuesFiles, setPairs []string) (string, error) {
	args := []string{"upgrade", name, chart, "-n", namespace, "--install", "--create-namespace"}
	for _, f := range valuesFiles {
		args = append(args, "-f", f)
	}
	for _, kv := range setPairs {
		args = append(args, "--set", kv)
	}
	return c.Run(args...)
}

// Rollback 执行 `helm rollback <name> <revision> -n <ns>`。
func (c *CLI) Rollback(namespace, name string, revision int) (string, error) {
	return c.Run("rollback", name, fmt.Sprintf("%d", revision), "-n", namespace)
}

// Uninstall 执行 `helm uninstall <name> -n <ns>`。
func (c *CLI) Uninstall(namespace, name string) (string, error) {
	return c.Run("uninstall", name, "-n", namespace)
}

// List 执行 `helm list -n <ns> -o json`，返回 release 列表 JSON。
func (c *CLI) List(namespace string) (string, error) {
	return c.Run("list", "-n", namespace, "-o", "json")
}

// ListAll 执行 `helm list -A -o json`，返回所有命名空间的 release 列表 JSON。
func (c *CLI) ListAll() (string, error) {
	return c.Run("list", "-A", "-o", "json")
}

// History 执行 `helm history <name> -n <ns> -o json`。
func (c *CLI) History(namespace, name string) (string, error) {
	return c.Run("history", name, "-n", namespace, "-o", "json")
}

// Get 执行 `helm get all <name> -n <ns>`（合并 manifest/hooks/values/notes）。
func (c *CLI) Get(namespace, name string) (string, error) {
	return c.Run("get", "all", name, "-n", namespace)
}

// GetValues 执行 `helm get values <name> -n <ns> -o json`。
func (c *CLI) GetValues(namespace, name string) (string, error) {
	return c.Run("get", "values", name, "-n", namespace, "-o", "json")
}

// GetManifest 执行 `helm get manifest <name> -n <ns>`。
func (c *CLI) GetManifest(namespace, name string) (string, error) {
	return c.Run("get", "manifest", name, "-n", namespace)
}

// Status 执行 `helm status <name> -n <ns> -o json`。
func (c *CLI) Status(namespace, name string) (string, error) {
	return c.Run("status", name, "-n", namespace, "-o", "json")
}

// Template 执行 `helm template <name> <chart> -n <ns>`。
//
// valuesFiles 为 -f 参数列表；setPairs 为 --set 参数列表。
func (c *CLI) Template(namespace, name, chart string, valuesFiles, setPairs []string) (string, error) {
	args := []string{"template", name, chart, "-n", namespace}
	for _, f := range valuesFiles {
		args = append(args, "-f", f)
	}
	for _, kv := range setPairs {
		args = append(args, "--set", kv)
	}
	return c.Run(args...)
}

// =============================================================================
// 命令存在性检查
// =============================================================================

// ErrHelmNotFound 表示 helm 命令未找到。
var ErrHelmNotFound = errors.New("helm: 命令未找到，请确认 helm 已安装并在 PATH 中")

// CheckAvailable 检查 helm 命令是否可用（执行 `helm version` 验证）。
func (c *CLI) CheckAvailable() error {
	_, err := c.Run("version", "--short")
	if err != nil {
		// 区分 "未找到" 与 "执行失败"。
		var ee *exec.Error
		if errors.As(err, &ee) {
			return ErrHelmNotFound
		}
		return err
	}
	return nil
}
