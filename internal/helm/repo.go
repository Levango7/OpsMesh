
// repo.go 实现 Chart 仓库管理：添加/删除仓库、列出/搜索 chart。
//
// 设计要点：
//   - RepoManager 持有 CLI 引用与内存中的 repos 索引（并发安全）；
//   - AddRepo/RemoveRepo 同步 helm CLI 与内存状态；
//   - ListCharts/SearchCharts/GetChart 解析 helm JSON 输出为 ChartInfo；
//   - 内存索引使仓库元数据可离线查询，CLI 调用获取 chart 详情。

package helm

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// RepoType 表示 Chart 仓库类型。
type RepoType string

const (
	// RepoTypeHTTP 表示 HTTP/HTTPS Helm 仓库。
	RepoTypeHTTP RepoType = "http"
	// RepoTypeOCI 表示 OCI 注册表（如 registry-1.docker.io/charts）。
	RepoTypeOCI RepoType = "oci"
	// RepoTypeHelm 是 RepoTypeHTTP 的别名，兼容旧配置。
	RepoTypeHelm RepoType = "helm"
)

// ChartRepo 描述一个 Chart 仓库。
type ChartRepo struct {
	Name string   `json:"name" yaml:"name"`
	URL  string   `json:"url"  yaml:"url"`
	Type RepoType `json:"type" yaml:"type"`
}

// ChartInfo 描述一个 Chart 的元数据（来自 helm search/show）。
type ChartInfo struct {
	Name        string   `json:"name"         yaml:"name"`
	Version     string   `json:"version"      yaml:"version"`
	AppVersion  string   `json:"appVersion"   yaml:"appVersion"`
	Description string   `json:"description"  yaml:"description"`
	Keywords    []string `json:"keywords"     yaml:"keywords"`
	Home        string   `json:"home"         yaml:"home"`
	Icon        string   `json:"icon"         yaml:"icon"`
	Maintainers []string `json:"maintainers"  yaml:"maintainers"`
	// Repository 来源仓库名（如 "bitnami"），helm search 输出 "bitnami/mysql"。
	Repository string `json:"repository,omitempty" yaml:"repository,omitempty"`
}

// maintainerJSON 用于解析 helm show chart 的 maintainers 字段。
type maintainerJSON struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// chartInfoJSON 是 helm search repo -o json 的单个元素结构。
// helm search 输出 name 为 "<repo>/<chart>"，需拆分。
type chartInfoJSON struct {
	Name        string           `json:"name"`
	Version     string           `json:"version"`
	AppVersion  string           `json:"appVersion"`
	Description string           `json:"description"`
	Keywords    []string         `json:"keywords"`
	Home        string           `json:"home"`
	Icon        string           `json:"icon"`
	Maintainers []maintainerJSON `json:"maintainers"`
}

// repoListJSON 是 helm repo list -o json 的元素结构。
type repoListJSON struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// RepoManager 管理 Chart 仓库集合，提供并发安全的仓库增删与 chart 查询。
type RepoManager struct {
	cli   HelmCLI
	mu    sync.RWMutex
	repos map[string]*ChartRepo
}

// NewRepoManager 创建 RepoManager。
//
// cli 为 nil 时使用默认 CLI（无 kubeconfig），便于纯内存操作场景。
func NewRepoManager(cli *CLI) *RepoManager {
	if cli == nil {
		cli = NewCLI("")
	}
	return &RepoManager{
		cli:   cli,
		repos: make(map[string]*ChartRepo),
	}
}

// NewRepoManagerWithCLI 使用指定 HelmCLI 接口创建 RepoManager（便于测试注入 mock）。
func NewRepoManagerWithCLI(cli HelmCLI) *RepoManager {
	if cli == nil {
		cli = NewCLI("")
	}
	return &RepoManager{
		cli:   cli,
		repos: make(map[string]*ChartRepo),
	}
}

// AddRepo 添加 Chart 仓库，同步 helm CLI 与内存索引。
//
// repo.Name 必须非空；repo.URL 必须非空；repo.Type 为空时默认 "http"。
// 重复添加同名仓库会返回错误（先调用 RemoveRepo）。
func (m *RepoManager) AddRepo(repo *ChartRepo) error {
	if repo == nil {
		return fmt.Errorf("helm/repo: AddRepo: repo 为 nil")
	}
	if repo.Name == "" {
		return fmt.Errorf("helm/repo: AddRepo: 仓库名为空")
	}
	if repo.URL == "" {
		return fmt.Errorf("helm/repo: AddRepo: 仓库 URL 为空")
	}
	if repo.Type == "" {
		repo.Type = RepoTypeHTTP
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.repos[repo.Name]; exists {
		return fmt.Errorf("helm/repo: 仓库 %q 已存在，请先 RemoveRepo", repo.Name)
	}

	// OCI 仓库不能用 helm repo add（helm 3 不支持），仅记录内存索引。
	if repo.Type != RepoTypeOCI {
		if _, err := m.cli.RepoAdd(repo.Name, repo.URL); err != nil {
			return fmt.Errorf("helm/repo: 添加仓库 %q 失败: %w", repo.Name, err)
		}
	}

	// 拷贝以避免外部修改。
	cp := *repo
	m.repos[repo.Name] = &cp
	return nil
}

// RemoveRepo 删除 Chart 仓库。
//
// 仓库不存在时返回错误；OCI 仓库仅删除内存索引。
func (m *RepoManager) RemoveRepo(name string) error {
	if name == "" {
		return fmt.Errorf("helm/repo: RemoveRepo: 仓库名为空")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	repo, exists := m.repos[name]
	if !exists {
		return fmt.Errorf("helm/repo: 仓库 %q 不存在", name)
	}

	if repo.Type != RepoTypeOCI {
		if _, err := m.cli.RepoRemove(name); err != nil {
			return fmt.Errorf("helm/repo: 删除仓库 %q 失败: %w", name, err)
		}
	}
	delete(m.repos, name)
	return nil
}

// ListRepos 返回所有已注册仓库（内存索引，不调用 helm CLI）。
func (m *RepoManager) ListRepos() []*ChartRepo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]*ChartRepo, 0, len(m.repos))
	for _, r := range m.repos {
		cp := *r
		out = append(out, &cp)
	}
	return out
}

// GetRepo 返回指定仓库的元数据（内存索引）。
func (m *RepoManager) GetRepo(name string) (*ChartRepo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	r, exists := m.repos[name]
	if !exists {
		return nil, fmt.Errorf("helm/repo: 仓库 %q 不存在", name)
	}
	cp := *r
	return &cp, nil
}

// ListCharts 列出指定仓库中的所有 chart（调用 helm search repo <name>/）。
//
// repoName 为空时返回错误（避免误搜全局）。
// OCI 仓库不支持 helm search，返回错误提示。
func (m *RepoManager) ListCharts(repoName string) ([]*ChartInfo, error) {
	if repoName == "" {
		return nil, fmt.Errorf("helm/repo: ListCharts: 仓库名为空")
	}

	m.mu.RLock()
	repo, exists := m.repos[repoName]
	m.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("helm/repo: 仓库 %q 不存在", repoName)
	}
	if repo.Type == RepoTypeOCI {
		return nil, fmt.Errorf("helm/repo: OCI 仓库 %q 不支持 list，请用 helm pull 或 GetChart", repoName)
	}

	// helm search repo <name>/ 仅返回该仓库的 chart。
	raw, err := m.cli.SearchCharts(repoName+"/", false)
	if err != nil {
		return nil, fmt.Errorf("helm/repo: 列出仓库 %q 的 chart 失败: %w", repoName, err)
	}
	return parseSearchJSON(raw)
}

// SearchCharts 跨所有仓库搜索 chart（调用 helm search repo <keyword>）。
//
// keyword 为空时返回错误；匹配 name/keywords/description。
func (m *RepoManager) SearchCharts(keyword string) ([]*ChartInfo, error) {
	if keyword == "" {
		return nil, fmt.Errorf("helm/repo: SearchCharts: 关键字为空")
	}

	raw, err := m.cli.SearchCharts(keyword, false)
	if err != nil {
		return nil, fmt.Errorf("helm/repo: 搜索 %q 失败: %w", keyword, err)
	}
	return parseSearchJSON(raw)
}

// GetChart 获取指定 chart 的详细信息（调用 helm show chart）。
//
// version 为空表示最新版本；repoName/chartName 共同定位 chart。
func (m *RepoManager) GetChart(repoName, chartName, version string) (*ChartInfo, error) {
	if repoName == "" || chartName == "" {
		return nil, fmt.Errorf("helm/repo: GetChart: repoName 和 chartName 不能为空")
	}

	m.mu.RLock()
	repo, exists := m.repos[repoName]
	m.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("helm/repo: 仓库 %q 不存在", repoName)
	}

	// 构造 chart 引用：OCI 用 oci://<url>/<chart>，HTTP 用 <repo>/<chart>。
	var chartRef string
	if repo.Type == RepoTypeOCI {
		chartRef = fmt.Sprintf("oci://%s/%s", strings.TrimSuffix(repo.URL, "/"), chartName)
	} else {
		chartRef = fmt.Sprintf("%s/%s", repoName, chartName)
	}

	args := []string{"show", "chart", chartRef, "-o", "json"}
	if version != "" {
		args = append(args, "--version", version)
	}
	raw, err := m.cli.Run(args...)
	if err != nil {
		return nil, fmt.Errorf("helm/repo: 获取 chart %q 失败: %w", chartRef, err)
	}

	// helm show chart 输出单个 JSON 对象（非数组）。
	var cj chartInfoJSON
	if err := json.Unmarshal([]byte(raw), &cj); err != nil {
		return nil, fmt.Errorf("helm/repo: 解析 chart 元数据 JSON 失败: %w; raw=%s", err, truncate(raw, 200))
	}
	return chartInfoJSONToInfo(&cj, repoName), nil
}

// UpdateRepos 更新所有仓库索引（helm repo update）。
func (m *RepoManager) UpdateRepos() error {
	if _, err := m.cli.RepoUpdate(); err != nil {
		return fmt.Errorf("helm/repo: 更新仓库索引失败: %w", err)
	}
	return nil
}

// LoadFromHelm 从 helm CLI 已配置的仓库同步到内存索引（helm repo list）。
//
// 用于启动时恢复已存在的仓库配置。type 字段无法从 helm repo list 获取，
// 默认设为 "http"（OCI 仓库不出现在 helm repo list 中）。
func (m *RepoManager) LoadFromHelm() error {
	raw, err := m.cli.RepoList()
	if err != nil {
		// helm repo list 在无仓库时返回非 0 退出码 + "no repositories to show"。
		// 这种情况视为空列表，不报错。
		if strings.Contains(err.Error(), "no repositories") {
			return nil
		}
		return fmt.Errorf("helm/repo: 加载仓库列表失败: %w", err)
	}

	var items []repoListJSON
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return fmt.Errorf("helm/repo: 解析仓库列表 JSON 失败: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, it := range items {
		m.repos[it.Name] = &ChartRepo{
			Name: it.Name,
			URL:  it.URL,
			Type: RepoTypeHTTP,
		}
	}
	return nil
}

// =============================================================================
// JSON 解析辅助函数
// =============================================================================

// parseSearchJSON 解析 helm search repo -o json 输出为 ChartInfo 列表。
func parseSearchJSON(raw string) ([]*ChartInfo, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var items []chartInfoJSON
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("helm/repo: 解析 search JSON 失败: %w; raw=%s", err, truncate(raw, 200))
	}

	out := make([]*ChartInfo, 0, len(items))
	for i := range items {
		// helm search 的 name 为 "<repo>/<chart>"，拆分得到 repo 与 chart name。
		repo, chart := splitRepoChart(items[i].Name)
		info := chartInfoJSONToInfo(&items[i], repo)
		info.Name = chart
		out = append(out, info)
	}
	return out, nil
}

// chartInfoJSONToInfo 将内部 JSON 结构转为对外 ChartInfo。
func chartInfoJSONToInfo(cj *chartInfoJSON, repo string) *ChartInfo {
	info := &ChartInfo{
		Name:        cj.Name,
		Version:     cj.Version,
		AppVersion:  cj.AppVersion,
		Description: cj.Description,
		Keywords:    cj.Keywords,
		Home:        cj.Home,
		Icon:        cj.Icon,
		Repository:  repo,
	}
	if len(cj.Maintainers) > 0 {
		info.Maintainers = make([]string, 0, len(cj.Maintainers))
		for _, m := range cj.Maintainers {
			if m.Email != "" {
				info.Maintainers = append(info.Maintainers, fmt.Sprintf("%s <%s>", m.Name, m.Email))
			} else {
				info.Maintainers = append(info.Maintainers, m.Name)
			}
		}
	}
	return info
}

// splitRepoChart 拆分 "bitnami/mysql" 为 ("bitnami", "mysql")。
// 无 "/" 时返回 ("", 全串)。
func splitRepoChart(fullName string) (repo, chart string) {
	idx := strings.Index(fullName, "/")
	if idx < 0 {
		return "", fullName
	}
	return fullName[:idx], fullName[idx+1:]
}

// truncate 截断字符串到 maxLen 并加省略号，用于错误信息。
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}