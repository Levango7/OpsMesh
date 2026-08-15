// release.go 实现 Helm Release 管理：安装/升级/回滚/卸载/列表/历史/详情。
//
// 设计要点：
//   - ReleaseManager 持有 CLI 引用，所有操作通过 helm CLI 完成；
//   - values map 通过临时 JSON 文件传递（helm -f 支持 .json/.yaml）；
//   - List/History/Get 解析 helm -o json 输出为 Release 结构；
//   - Release.Status 取值：deployed/failed/pending-install/pending-upgrade/pending-rollback。

package helm

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// ReleaseStatus 表示 Helm Release 的状态。
type ReleaseStatus string

const (
	// StatusDeployed 已成功部署。
	StatusDeployed ReleaseStatus = "deployed"
	// StatusFailed 部署失败。
	StatusFailed ReleaseStatus = "failed"
	// StatusPendingInstall 安装中。
	StatusPendingInstall ReleaseStatus = "pending-install"
	// StatusPendingUpgrade 升级中。
	StatusPendingUpgrade ReleaseStatus = "pending-upgrade"
	// StatusPendingRollback 回滚中。
	StatusPendingRollback ReleaseStatus = "pending-rollback"
	// StatusSuperseded 已被新版本取代。
	StatusSuperseded ReleaseStatus = "superseded"
	// StatusUninstalled 已卸载（仅历史中可见）。
	StatusUninstalled ReleaseStatus = "uninstalled"
	// StatusUninstalling 卸载中。
	StatusUninstalling ReleaseStatus = "uninstalling"
)

// Release 描述一个 Helm Release 实例。
type Release struct {
	Name       string                 `json:"name"      yaml:"name"`
	Namespace  string                 `json:"namespace" yaml:"namespace"`
	Chart      string                 `json:"chart"     yaml:"chart"`   // chart 名（不含版本）
	Version    string                 `json:"version"   yaml:"version"` // chart 版本
	AppVersion string                 `json:"appVersion,omitempty" yaml:"appVersion,omitempty"`
	Status     string                 `json:"status"    yaml:"status"`
	Revision   int                    `json:"revision"  yaml:"revision"`
	Values     map[string]interface{} `json:"values,omitempty" yaml:"values,omitempty"`
	CreatedAt  time.Time              `json:"createdAt" yaml:"createdAt"`
	UpdatedAt  time.Time              `json:"updatedAt" yaml:"updatedAt"`
	// Description 来自 history 的描述（如 "Install complete"）。
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// ReleaseManager 管理 Helm Release 生命周期。
type ReleaseManager struct {
	cli HelmCLI
}

// NewReleaseManager 创建 ReleaseManager。
//
// kubeconfig 为 --kubeconfig 参数值，空字符串表示使用默认配置。
func NewReleaseManager(kubeconfig string) *ReleaseManager {
	return &ReleaseManager{cli: NewCLI(kubeconfig)}
}

// NewReleaseManagerWithCLI 使用已有 CLI 创建 ReleaseManager（便于测试注入）。
func NewReleaseManagerWithCLI(cli *CLI) *ReleaseManager {
	if cli == nil {
		cli = NewCLI("")
	}
	return &ReleaseManager{cli: cli}
}

// NewReleaseManagerWithHelmCLI 使用 HelmCLI 接口创建 ReleaseManager（便于测试注入 mock）。
func NewReleaseManagerWithHelmCLI(cli HelmCLI) *ReleaseManager {
	if cli == nil {
		cli = NewCLI("")
	}
	return &ReleaseManager{cli: cli}
}

// Install 安装 chart 创建新 release。
//
//   - namespace：目标命名空间（自动创建）；
//   - name：release 名；
//   - chart：chart 引用（如 "bitnami/mysql" 或本地路径）；
//   - values：覆盖值（写入临时 JSON 文件通过 -f 传递，nil 表示不覆盖）。
//
// 返回新创建的 Release（含 status/revision）。
func (m *ReleaseManager) Install(namespace, name, chart string, values map[string]interface{}) (*Release, error) {
	if err := validateReleaseArgs(namespace, name, chart); err != nil {
		return nil, err
	}

	valuesFiles, cleanup, err := writeValuesTemp(values)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	if _, err := m.cli.Install(namespace, name, chart, valuesFiles, nil); err != nil {
		return nil, fmt.Errorf("helm/release: 安装 %q 失败: %w", name, err)
	}
	return m.Get(namespace, name)
}

// Upgrade 升级已存在的 release（--install 模式：不存在则安装）。
func (m *ReleaseManager) Upgrade(namespace, name, chart string, values map[string]interface{}) (*Release, error) {
	if err := validateReleaseArgs(namespace, name, chart); err != nil {
		return nil, err
	}

	valuesFiles, cleanup, err := writeValuesTemp(values)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	if _, err := m.cli.Upgrade(namespace, name, chart, valuesFiles, nil); err != nil {
		return nil, fmt.Errorf("helm/release: 升级 %q 失败: %w", name, err)
	}
	return m.Get(namespace, name)
}

// Rollback 回滚 release 到指定 revision。
//
// revision <= 0 表示回滚到上一个版本。
func (m *ReleaseManager) Rollback(namespace, name string, revision int) (*Release, error) {
	if namespace == "" || name == "" {
		return nil, fmt.Errorf("helm/release: Rollback: namespace 和 name 不能为空")
	}
	if revision <= 0 {
		// 查询当前 revision，回滚到 revision-1。
		history, err := m.History(namespace, name)
		if err != nil {
			return nil, fmt.Errorf("helm/release: 回滚 %q 需查询历史但失败: %w", name, err)
		}
		if len(history) < 2 {
			return nil, fmt.Errorf("helm/release: 回滚 %q 失败: 历史记录不足 2 条，无法回滚", name)
		}
		// history 按 revision 升序，最后一条是当前版本。
		revision = history[len(history)-2].Revision
	}

	if _, err := m.cli.Rollback(namespace, name, revision); err != nil {
		return nil, fmt.Errorf("helm/release: 回滚 %q 到 revision %d 失败: %w", name, revision, err)
	}
	return m.Get(namespace, name)
}

// Uninstall 卸载 release。
func (m *ReleaseManager) Uninstall(namespace, name string) error {
	if namespace == "" || name == "" {
		return fmt.Errorf("helm/release: Uninstall: namespace 和 name 不能为空")
	}
	if _, err := m.cli.Uninstall(namespace, name); err != nil {
		return fmt.Errorf("helm/release: 卸载 %q 失败: %w", name, err)
	}
	return nil
}

// List 列出指定命名空间的所有 release。
func (m *ReleaseManager) List(namespace string) ([]*Release, error) {
	if namespace == "" {
		return nil, fmt.Errorf("helm/release: List: namespace 为空")
	}
	raw, err := m.cli.List(namespace)
	if err != nil {
		return nil, fmt.Errorf("helm/release: 列出 namespace %q 的 release 失败: %w", namespace, err)
	}
	return parseListJSON(raw)
}

// ListAll 列出所有命名空间的所有 release（helm list -A）。
func (m *ReleaseManager) ListAll() ([]*Release, error) {
	raw, err := m.cli.ListAll()
	if err != nil {
		return nil, fmt.Errorf("helm/release: 列出所有 release 失败: %w", err)
	}
	return parseListJSON(raw)
}

// History 获取 release 的历史版本（按 revision 升序）。
func (m *ReleaseManager) History(namespace, name string) ([]*Release, error) {
	if namespace == "" || name == "" {
		return nil, fmt.Errorf("helm/release: History: namespace 和 name 不能为空")
	}
	raw, err := m.cli.History(namespace, name)
	if err != nil {
		return nil, fmt.Errorf("helm/release: 获取 %q 历史失败: %w", name, err)
	}
	return parseHistoryJSON(raw, namespace, name)
}

// Get 获取 release 详情（helm status -o json）。
func (m *ReleaseManager) Get(namespace, name string) (*Release, error) {
	if namespace == "" || name == "" {
		return nil, fmt.Errorf("helm/release: Get: namespace 和 name 不能为空")
	}
	raw, err := m.cli.Status(namespace, name)
	if err != nil {
		return nil, fmt.Errorf("helm/release: 获取 %q 状态失败: %w", name, err)
	}
	return parseStatusJSON(raw)
}

// GetValues 获取 release 当前的 values（helm get values -o json）。
func (m *ReleaseManager) GetValues(namespace, name string) (map[string]interface{}, error) {
	if namespace == "" || name == "" {
		return nil, fmt.Errorf("helm/release: GetValues: namespace 和 name 不能为空")
	}
	raw, err := m.cli.GetValues(namespace, name)
	if err != nil {
		return nil, fmt.Errorf("helm/release: 获取 %q values 失败: %w", name, err)
	}
	var vals map[string]interface{}
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	if err := json.Unmarshal([]byte(raw), &vals); err != nil {
		return nil, fmt.Errorf("helm/release: 解析 %q values JSON 失败: %w", name, err)
	}
	return vals, nil
}

// GetManifest 获取 release 的 Kubernetes manifest（helm get manifest）。
func (m *ReleaseManager) GetManifest(namespace, name string) (string, error) {
	if namespace == "" || name == "" {
		return "", fmt.Errorf("helm/release: GetManifest: namespace 和 name 不能为空")
	}
	raw, err := m.cli.GetManifest(namespace, name)
	if err != nil {
		return "", fmt.Errorf("helm/release: 获取 %q manifest 失败: %w", name, err)
	}
	return raw, nil
}

// =============================================================================
// values 临时文件处理
// =============================================================================

// writeValuesTemp 将 values map 写入临时 JSON 文件，返回文件路径与 cleanup 函数。
//
// values 为 nil 或空时返回空切片与空 cleanup（不传 -f 参数）。
// cleanup 必须用 defer 调用以删除临时文件。
func writeValuesTemp(values map[string]interface{}) ([]string, func(), error) {
	noop := func() {}
	if len(values) == 0 {
		return nil, noop, nil
	}

	data, err := json.Marshal(values)
	if err != nil {
		return nil, noop, fmt.Errorf("helm/release: 序列化 values 失败: %w", err)
	}

	f, err := os.CreateTemp("", "helm-values-*.json")
	if err != nil {
		return nil, noop, fmt.Errorf("helm/release: 创建临时 values 文件失败: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, noop, fmt.Errorf("helm/release: 写入临时 values 文件失败: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return nil, noop, fmt.Errorf("helm/release: 关闭临时 values 文件失败: %w", err)
	}

	path := f.Name()
	cleanup := func() { _ = os.Remove(path) }
	return []string{path}, cleanup, nil
}

// validateReleaseArgs 校验 install/upgrade 公共参数。
func validateReleaseArgs(namespace, name, chart string) error {
	if namespace == "" {
		return fmt.Errorf("helm/release: namespace 为空")
	}
	if name == "" {
		return fmt.Errorf("helm/release: release name 为空")
	}
	if chart == "" {
		return fmt.Errorf("helm/release: chart 为空")
	}
	return nil
}

// =============================================================================
// JSON 解析
// =============================================================================

// listReleaseJSON 是 helm list -o json 的元素结构。
type listReleaseJSON struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	Revision     int    `json:"revision"`
	Updated      string `json:"updated"`
	Status       string `json:"status"`
	Chart        string `json:"chart"` // "mysql-9.10.0" 格式
	ChartVersion string `json:"chart_version"`
	AppVersion   string `json:"app_version"`
}

// historyReleaseJSON 是 helm history -o json 的元素结构。
type historyReleaseJSON struct {
	Revision     int    `json:"revision"`
	Updated      string `json:"updated"`
	Status       string `json:"status"`
	Chart        string `json:"chart"`
	ChartVersion string `json:"chart_version"`
	AppVersion   string `json:"app_version"`
	Description  string `json:"description"`
}

// statusJSON 是 helm status -o json 的结构（仅提取需要的字段）。
type statusJSON struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Info      struct {
		Status        string `json:"status"`
		FirstDeployed string `json:"first_deployed"`
		LastDeployed  string `json:"last_deployed"`
	} `json:"info"`
	Chart struct {
		Metadata struct {
			Name       string `json:"name"`
			Version    string `json:"version"`
			AppVersion string `json:"appVersion"`
		} `json:"metadata"`
	} `json:"chart"`
}

// parseListJSON 解析 helm list -o json 输出。
func parseListJSON(raw string) ([]*Release, error) {
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "[]" {
		return nil, nil
	}
	var items []listReleaseJSON
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("helm/release: 解析 list JSON 失败: %w; raw=%s", err, truncate(raw, 200))
	}
	out := make([]*Release, 0, len(items))
	for _, it := range items {
		chartName, chartVer := splitChartVersion(it.Chart)
		if chartVer == "" {
			chartVer = it.ChartVersion
		}
		out = append(out, &Release{
			Name:       it.Name,
			Namespace:  it.Namespace,
			Chart:      chartName,
			Version:    chartVer,
			AppVersion: it.AppVersion,
			Status:     it.Status,
			Revision:   it.Revision,
			UpdatedAt:  parseHelmTime(it.Updated),
			CreatedAt:  parseHelmTime(it.Updated),
		})
	}
	return out, nil
}

// parseHistoryJSON 解析 helm history -o json 输出。
func parseHistoryJSON(raw, namespace, name string) ([]*Release, error) {
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "[]" {
		return nil, nil
	}
	var items []historyReleaseJSON
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("helm/release: 解析 history JSON 失败: %w; raw=%s", err, truncate(raw, 200))
	}
	out := make([]*Release, 0, len(items))
	for _, it := range items {
		chartName, chartVer := splitChartVersion(it.Chart)
		if chartVer == "" {
			chartVer = it.ChartVersion
		}
		out = append(out, &Release{
			Name:        name,
			Namespace:   namespace,
			Chart:       chartName,
			Version:     chartVer,
			AppVersion:  it.AppVersion,
			Status:      it.Status,
			Revision:    it.Revision,
			Description: it.Description,
			UpdatedAt:   parseHelmTime(it.Updated),
		})
	}
	return out, nil
}

// parseStatusJSON 解析 helm status -o json 输出。
func parseStatusJSON(raw string) (*Release, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("helm/release: status 输出为空")
	}
	var s statusJSON
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return nil, fmt.Errorf("helm/release: 解析 status JSON 失败: %w; raw=%s", err, truncate(raw, 200))
	}
	return &Release{
		Name:       s.Name,
		Namespace:  s.Namespace,
		Chart:      s.Chart.Metadata.Name,
		Version:    s.Chart.Metadata.Version,
		AppVersion: s.Chart.Metadata.AppVersion,
		Status:     s.Info.Status,
		CreatedAt:  parseHelmTime(s.Info.FirstDeployed),
		UpdatedAt:  parseHelmTime(s.Info.LastDeployed),
	}, nil
}

// splitChartVersion 拆分 "mysql-9.10.0" 为 ("mysql", "9.10.0")。
//
// helm list/history 的 chart 字段格式为 "<name>-<version>"，但 name 本身可能含 "-"，
// 因此从右往左找第一个"数字开头"的段作为 version 起始。
func splitChartVersion(s string) (name, version string) {
	// 从右往左找最后一个 "-"，且其后第一个字符是数字（semver 起始）。
	for i := len(s) - 1; i > 0; i-- {
		if s[i] == '-' && i+1 < len(s) && isDigit(s[i+1]) {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}

// isDigit 判断字节是否为 ASCII 数字。
func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// parseHelmTime 解析 helm 时间字符串（支持多种格式）。
//
// helm 输出时间格式不统一，常见：
//   - "2023-01-01T00:00:00Z"（RFC3339）
//   - "2023-01-01 00:00:00 +0000 UTC"（Go 默认 Time.String()）
//   - "Mon Jan  1 00:00:00 2023"（Unix date 格式）
func parseHelmTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	// 尝试 RFC3339（helm -o json 标准格式）。
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	// 尝试 Go Time.String() 格式。
	if t, err := time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", s); err == nil {
		return t
	}
	// 尝试不带纳秒的 Go 格式。
	if t, err := time.Parse("2006-01-02 15:04:05 -0700 MST", s); err == nil {
		return t
	}
	// 尝试 Unix date 格式。
	if t, err := time.Parse("Mon Jan _2 15:04:05 2006", s); err == nil {
		return t
	}
	return time.Time{}
}
