package helm

import (
	"errors"
	"strings"
	"testing"
)

// =============================================================================
// ChartRepo / RepoType 测试
// =============================================================================

func TestRepoType_Constants(t *testing.T) {
	if RepoTypeHTTP != "http" {
		t.Errorf("RepoTypeHTTP = %q, want http", RepoTypeHTTP)
	}
	if RepoTypeOCI != "oci" {
		t.Errorf("RepoTypeOCI = %q, want oci", RepoTypeOCI)
	}
	if RepoTypeHelm != "helm" {
		t.Errorf("RepoTypeHelm = %q, want helm", RepoTypeHelm)
	}
}

// =============================================================================
// RepoManager.AddRepo / RemoveRepo 测试
// =============================================================================

func TestRepoManager_AddRepo_Success(t *testing.T) {
	mock := newMockReturn(map[string]string{
		"repo add": `success`, // helm repo add 输出
	})
	m := NewRepoManagerWithCLI(mock)

	err := m.AddRepo(&ChartRepo{Name: "bitnami", URL: "https://charts.bitnami.com/bitnami"})
	if err != nil {
		t.Fatalf("AddRepo failed: %v", err)
	}

	// 验证内存索引。
	repo, err := m.GetRepo("bitnami")
	if err != nil {
		t.Fatalf("GetRepo failed: %v", err)
	}
	if repo.URL != "https://charts.bitnami.com/bitnami" {
		t.Errorf("URL = %q", repo.URL)
	}
	if repo.Type != RepoTypeHTTP {
		t.Errorf("Type = %q, want http (default)", repo.Type)
	}

	// 验证 CLI 被调用。
	if len(mock.calls) != 1 {
		t.Fatalf("calls len = %d, want 1", len(mock.calls))
	}
	joined := strings.Join(mock.calls[0], " ")
	if !strings.HasPrefix(joined, "repo add bitnami https://charts.bitnami.com/bitnami") {
		t.Errorf("call = %q", joined)
	}
}

func TestRepoManager_AddRepo_Duplicate(t *testing.T) {
	mock := newMockReturn(map[string]string{"repo add": ""})
	m := NewRepoManagerWithCLI(mock)

	_ = m.AddRepo(&ChartRepo{Name: "bitnami", URL: "https://charts.bitnami.com/bitnami"})
	err := m.AddRepo(&ChartRepo{Name: "bitnami", URL: "https://other"})
	if err == nil {
		t.Fatal("AddRepo duplicate should fail")
	}
	if !strings.Contains(err.Error(), "已存在") {
		t.Errorf("error = %v, want contain 已存在", err)
	}
}

func TestRepoManager_AddRepo_Validation(t *testing.T) {
	m := NewRepoManagerWithCLI(newMockReturn(nil))

	tests := []struct {
		name string
		repo *ChartRepo
	}{
		{"nil repo", nil},
		{"empty name", &ChartRepo{Name: "", URL: "https://x"}},
		{"empty url", &ChartRepo{Name: "x", URL: ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := m.AddRepo(tt.repo); err == nil {
				t.Fatal("AddRepo should fail for invalid input")
			}
		})
	}
}

func TestRepoManager_AddRepo_OCI(t *testing.T) {
	mock := newMockReturn(nil)
	m := NewRepoManagerWithCLI(mock)

	// OCI 仓库不应调用 helm repo add。
	err := m.AddRepo(&ChartRepo{Name: "registry", URL: "registry-1.docker.io", Type: RepoTypeOCI})
	if err != nil {
		t.Fatalf("AddRepo OCI failed: %v", err)
	}
	if len(mock.calls) != 0 {
		t.Errorf("OCI repo should not call helm CLI, calls = %d", len(mock.calls))
	}
}

func TestRepoManager_RemoveRepo_Success(t *testing.T) {
	mock := newMockReturn(map[string]string{
		"repo add":    "",
		"repo remove": "",
	})
	m := NewRepoManagerWithCLI(mock)

	_ = m.AddRepo(&ChartRepo{Name: "bitnami", URL: "https://x"})
	err := m.RemoveRepo("bitnami")
	if err != nil {
		t.Fatalf("RemoveRepo failed: %v", err)
	}
	if _, err := m.GetRepo("bitnami"); err == nil {
		t.Fatal("repo should be removed")
	}
}

func TestRepoManager_RemoveRepo_NotExist(t *testing.T) {
	m := NewRepoManagerWithCLI(newMockReturn(nil))
	err := m.RemoveRepo("nonexistent")
	if err == nil {
		t.Fatal("RemoveRepo nonexistent should fail")
	}
}

func TestRepoManager_RemoveRepo_OCI(t *testing.T) {
	mock := newMockReturn(nil)
	m := NewRepoManagerWithCLI(mock)

	_ = m.AddRepo(&ChartRepo{Name: "reg", URL: "oci://x", Type: RepoTypeOCI})
	if err := m.RemoveRepo("reg"); err != nil {
		t.Fatalf("RemoveRepo OCI failed: %v", err)
	}
	if len(mock.calls) != 0 {
		t.Errorf("OCI remove should not call helm CLI, calls = %d", len(mock.calls))
	}
}

func TestRepoManager_ListRepos(t *testing.T) {
	mock := newMockReturn(map[string]string{"repo add": ""})
	m := NewRepoManagerWithCLI(mock)

	_ = m.AddRepo(&ChartRepo{Name: "a", URL: "https://a"})
	_ = m.AddRepo(&ChartRepo{Name: "b", URL: "https://b"})

	repos := m.ListRepos()
	if len(repos) != 2 {
		t.Fatalf("ListRepos len = %d, want 2", len(repos))
	}

	// 验证返回的是副本（修改不影响内部）。
	repos[0].Name = "modified"
	if _, err := m.GetRepo("a"); err != nil {
		t.Fatal("modifying returned slice should not affect internal state")
	}
}

// =============================================================================
// ListCharts / SearchCharts / GetChart 测试
// =============================================================================

const searchBitnamiJSON = `[
  {"name":"bitnami/mysql","version":"9.10.0","appVersion":"8.0.32","description":"MySQL is a fast, reliable, ...","keywords":["mysql","database"],"home":"https://bitnami.com","icon":"https://bitnami.com/icon.png","maintainers":[{"name":"Bitnami","email":"containers@bitnami.com"}]},
  {"name":"bitnami/redis","version":"18.0.0","appVersion":"7.2.0","description":"Redis is an in-memory data store.","keywords":["redis","cache"]}
]`

func TestRepoManager_ListCharts(t *testing.T) {
	mock := newMockReturn(map[string]string{
		"repo add":             "",
		"search repo bitnami/": searchBitnamiJSON,
	})
	m := NewRepoManagerWithCLI(mock)
	_ = m.AddRepo(&ChartRepo{Name: "bitnami", URL: "https://x"})

	charts, err := m.ListCharts("bitnami")
	if err != nil {
		t.Fatalf("ListCharts failed: %v", err)
	}
	if len(charts) != 2 {
		t.Fatalf("charts len = %d, want 2", len(charts))
	}

	// 验证第一个 chart（mysql）。
	mysql := charts[0]
	if mysql.Name != "mysql" {
		t.Errorf("chart[0].Name = %q, want mysql", mysql.Name)
	}
	if mysql.Version != "9.10.0" {
		t.Errorf("chart[0].Version = %q", mysql.Version)
	}
	if mysql.AppVersion != "8.0.32" {
		t.Errorf("chart[0].AppVersion = %q", mysql.AppVersion)
	}
	if mysql.Repository != "bitnami" {
		t.Errorf("chart[0].Repository = %q, want bitnami", mysql.Repository)
	}
	if len(mysql.Maintainers) != 1 || mysql.Maintainers[0] != "Bitnami <containers@bitnami.com>" {
		t.Errorf("chart[0].Maintainers = %v", mysql.Maintainers)
	}
}

func TestRepoManager_ListCharts_OCI_Unsupported(t *testing.T) {
	m := NewRepoManagerWithCLI(newMockReturn(nil))
	_ = m.AddRepo(&ChartRepo{Name: "reg", URL: "oci://x", Type: RepoTypeOCI})

	_, err := m.ListCharts("reg")
	if err == nil {
		t.Fatal("ListCharts on OCI should fail")
	}
	if !strings.Contains(err.Error(), "不支持") {
		t.Errorf("error = %v, want contain 不支持", err)
	}
}

func TestRepoManager_ListCharts_RepoNotExist(t *testing.T) {
	m := NewRepoManagerWithCLI(newMockReturn(nil))
	_, err := m.ListCharts("nonexistent")
	if err == nil {
		t.Fatal("ListCharts on nonexistent repo should fail")
	}
}

func TestRepoManager_SearchCharts(t *testing.T) {
	mock := newMockReturn(map[string]string{
		"search repo mysql": searchBitnamiJSON,
	})
	m := NewRepoManagerWithCLI(mock)

	results, err := m.SearchCharts("mysql")
	if err != nil {
		t.Fatalf("SearchCharts failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2", len(results))
	}
}

func TestRepoManager_SearchCharts_EmptyKeyword(t *testing.T) {
	m := NewRepoManagerWithCLI(newMockReturn(nil))
	_, err := m.SearchCharts("")
	if err == nil {
		t.Fatal("SearchCharts empty keyword should fail")
	}
}

func TestRepoManager_GetChart(t *testing.T) {
	const chartJSON = `{"name":"mysql","version":"9.10.0","appVersion":"8.0.32","description":"MySQL","keywords":["mysql"],"home":"https://x","icon":"https://i","maintainers":[{"name":"Bitnami"}]}`
	mock := newMockReturn(map[string]string{
		"repo add":   "",
		"show chart": chartJSON,
	})
	m := NewRepoManagerWithCLI(mock)
	_ = m.AddRepo(&ChartRepo{Name: "bitnami", URL: "https://x"})

	info, err := m.GetChart("bitnami", "mysql", "9.10.0")
	if err != nil {
		t.Fatalf("GetChart failed: %v", err)
	}
	if info.Name != "mysql" {
		t.Errorf("Name = %q", info.Name)
	}
	if info.Version != "9.10.0" {
		t.Errorf("Version = %q", info.Version)
	}
	if info.AppVersion != "8.0.32" {
		t.Errorf("AppVersion = %q", info.AppVersion)
	}
}

func TestRepoManager_GetChart_OCI(t *testing.T) {
	const chartJSON = `{"name":"mysql","version":"9.10.0","appVersion":"8.0.32"}`
	mock := newMockReturn(map[string]string{
		"show chart oci://": chartJSON,
	})
	m := NewRepoManagerWithCLI(mock)
	_ = m.AddRepo(&ChartRepo{Name: "reg", URL: "registry.example.com/charts", Type: RepoTypeOCI})

	info, err := m.GetChart("reg", "mysql", "")
	if err != nil {
		t.Fatalf("GetChart OCI failed: %v", err)
	}
	if info.Name != "mysql" {
		t.Errorf("Name = %q", info.Name)
	}
	// 验证用了 oci:// 前缀。
	joined := strings.Join(mock.calls[len(mock.calls)-1], " ")
	if !strings.Contains(joined, "oci://registry.example.com/charts/mysql") {
		t.Errorf("OCI chart ref not used, call = %q", joined)
	}
}

func TestRepoManager_LoadFromHelm(t *testing.T) {
	const repoListJSON = `[{"name":"bitnami","url":"https://charts.bitnami.com/bitnami"},{"name":"prometheus-community","url":"https://prometheus-community.github.io/helm-charts"}]`
	mock := newMockReturn(map[string]string{
		"repo list": repoListJSON,
	})
	m := NewRepoManagerWithCLI(mock)

	if err := m.LoadFromHelm(); err != nil {
		t.Fatalf("LoadFromHelm failed: %v", err)
	}
	repos := m.ListRepos()
	if len(repos) != 2 {
		t.Fatalf("repos len = %d, want 2", len(repos))
	}
}

func TestRepoManager_LoadFromHelm_Empty(t *testing.T) {
	// helm repo list 无仓库时返回错误 + "no repositories"。
	mock := newMockError(errors.New("Error: no repositories to show"))
	m := NewRepoManagerWithCLI(mock)

	if err := m.LoadFromHelm(); err != nil {
		t.Fatalf("LoadFromHelm with no repos should not fail: %v", err)
	}
	if len(m.ListRepos()) != 0 {
		t.Fatal("should have 0 repos")
	}
}

// =============================================================================
// JSON 解析辅助函数测试
// =============================================================================

func TestParseSearchJSON(t *testing.T) {
	results, err := parseSearchJSON(searchBitnamiJSON)
	if err != nil {
		t.Fatalf("parseSearchJSON failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2", len(results))
	}
	if results[0].Name != "mysql" {
		t.Errorf("results[0].Name = %q, want mysql", results[0].Name)
	}
	if results[0].Repository != "bitnami" {
		t.Errorf("results[0].Repository = %q, want bitnami", results[0].Repository)
	}
}

func TestParseSearchJSON_Empty(t *testing.T) {
	results, err := parseSearchJSON("")
	if err != nil {
		t.Fatalf("parseSearchJSON empty failed: %v", err)
	}
	if results != nil {
		t.Errorf("results = %v, want nil", results)
	}
}

func TestParseSearchJSON_Invalid(t *testing.T) {
	_, err := parseSearchJSON("not json")
	if err == nil {
		t.Fatal("parseSearchJSON invalid should fail")
	}
}

func TestSplitRepoChart(t *testing.T) {
	tests := []struct {
		input string
		repo  string
		chart string
	}{
		{"bitnami/mysql", "bitnami", "mysql"},
		{"mysql", "", "mysql"},
		{"a/b/c", "a", "b/c"},
		{"", "", ""},
	}
	for _, tt := range tests {
		repo, chart := splitRepoChart(tt.input)
		if repo != tt.repo || chart != tt.chart {
			t.Errorf("splitRepoChart(%q) = (%q, %q), want (%q, %q)",
				tt.input, repo, chart, tt.repo, tt.chart)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("truncate(hello,10) = %q", got)
	}
	if got := truncate("hello world", 5); got != "hello..." {
		t.Errorf("truncate(hello world,5) = %q", got)
	}
}
