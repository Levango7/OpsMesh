package helm

import (
	"strings"
	"testing"
)

// =============================================================================
// DefaultCatalog 基础测试
// =============================================================================

func TestDefaultCatalog_NotEmpty(t *testing.T) {
	if len(DefaultCatalog) < 20 {
		t.Errorf("DefaultCatalog len = %d, want >= 20", len(DefaultCatalog))
	}
}

func TestDefaultCatalog_UniqueIDs(t *testing.T) {
	seen := make(map[string]bool)
	for _, item := range DefaultCatalog {
		if item.ID == "" {
			t.Error("found item with empty ID")
			continue
		}
		if seen[item.ID] {
			t.Errorf("duplicate ID: %q", item.ID)
		}
		seen[item.ID] = true
	}
}

func TestDefaultCatalog_RequiredFields(t *testing.T) {
	for _, item := range DefaultCatalog {
		if item.Name == "" {
			t.Errorf("item %q has empty Name", item.ID)
		}
		if item.Category == "" {
			t.Errorf("item %q has empty Category", item.ID)
		}
		if item.Chart == "" {
			t.Errorf("item %q has empty Chart", item.ID)
		}
		if item.Repo == "" {
			t.Errorf("item %q has empty Repo", item.ID)
		}
		if item.Version == "" {
			t.Errorf("item %q has empty Version", item.ID)
		}
		if item.Description == "" {
			t.Errorf("item %q has empty Description", item.ID)
		}
	}
}

func TestDefaultCatalog_RequiredApps(t *testing.T) {
	// 任务要求的核心应用必须存在。
	required := []string{
		"mysql", "redis", "kafka", "nginx", "prometheus", "grafana",
		"postgresql", "mongodb", "rabbitmq", "elasticsearch",
	}
	for _, id := range required {
		if GetCatalogItem(id) == nil {
			t.Errorf("required app %q not found in catalog", id)
		}
	}
}

func TestDefaultCatalog_Categories(t *testing.T) {
	// 验证所有分类都有条目。
	cats := ListCategories()
	if len(cats) < 8 {
		t.Errorf("categories len = %d, want >= 8", len(cats))
	}

	expectedCats := []CatalogCategory{
		CategoryDatabase, CategoryCache, CategoryMQ, CategoryWeb,
		CategoryMonitor, CategoryStorage, CategoryNetwork, CategorySecurity,
	}
	for _, ec := range expectedCats {
		found := false
		for _, c := range cats {
			if c == ec {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("category %q not found in catalog", ec)
		}
	}
}

// =============================================================================
// GetCatalogItem 测试
// =============================================================================

func TestGetCatalogItem(t *testing.T) {
	mysql := GetCatalogItem("mysql")
	if mysql == nil {
		t.Fatal("mysql not found")
	}
	if mysql.Name != "MySQL" {
		t.Errorf("mysql.Name = %q, want MySQL", mysql.Name)
	}
	if mysql.Category != CategoryDatabase {
		t.Errorf("mysql.Category = %q, want database", mysql.Category)
	}
	if mysql.Repo != "bitnami" {
		t.Errorf("mysql.Repo = %q, want bitnami", mysql.Repo)
	}
}

func TestGetCatalogItem_NotFound(t *testing.T) {
	if got := GetCatalogItem("nonexistent-app"); got != nil {
		t.Errorf("GetCatalogItem(nonexistent) = %v, want nil", got)
	}
}

// =============================================================================
// ListByCategory 测试
// =============================================================================

func TestListByCategory(t *testing.T) {
	databases := ListByCategory(CategoryDatabase)
	if len(databases) < 4 {
		t.Errorf("databases len = %d, want >= 4 (mysql/postgresql/mongodb/mariadb)", len(databases))
	}
	for _, db := range databases {
		if db.Category != CategoryDatabase {
			t.Errorf("db.Category = %q, want database", db.Category)
		}
	}

	monitors := ListByCategory(CategoryMonitor)
	if len(monitors) < 4 {
		t.Errorf("monitors len = %d, want >= 4 (prometheus/grafana/loki/fluent-bit)", len(monitors))
	}
}

func TestListByCategory_Empty(t *testing.T) {
	// 空分类返回全部。
	all := ListByCategory("")
	if len(all) != len(DefaultCatalog) {
		t.Errorf("ListByCategory(empty) len = %d, want %d", len(all), len(DefaultCatalog))
	}
}

func TestListByCategory_NoMatch(t *testing.T) {
	// 不存在的分类返回空切片。
	result := ListByCategory("nonexistent")
	if len(result) != 0 {
		t.Errorf("ListByCategory(nonexistent) len = %d, want 0", len(result))
	}
}

// =============================================================================
// SearchCatalog 测试
// =============================================================================

func TestSearchCatalog(t *testing.T) {
	// 搜索 "mysql"。
	results := SearchCatalog("mysql")
	if len(results) == 0 {
		t.Fatal("SearchCatalog(mysql) returned no results")
	}
	found := false
	for _, r := range results {
		if r.ID == "mysql" {
			found = true
			break
		}
	}
	if !found {
		t.Error("mysql should be in search results")
	}
}

func TestSearchCatalog_EmptyKeyword(t *testing.T) {
	all := SearchCatalog("")
	if len(all) != len(DefaultCatalog) {
		t.Errorf("SearchCatalog(empty) len = %d, want %d", len(all), len(DefaultCatalog))
	}
}

func TestSearchCatalog_CaseInsensitive(t *testing.T) {
	upper := SearchCatalog("MYSQL")
	lower := SearchCatalog("mysql")
	if len(upper) != len(lower) {
		t.Errorf("case insensitive mismatch: upper=%d lower=%d", len(upper), len(lower))
	}
}

func TestSearchCatalog_ByDescription(t *testing.T) {
	// 搜索描述中的中文关键词。
	results := SearchCatalog("数据库")
	if len(results) == 0 {
		t.Fatal("SearchCatalog(数据库) returned no results")
	}
	// MySQL/PostgreSQL/MongoDB/MariaDB 描述都含"数据库"。
	if len(results) < 3 {
		t.Errorf("SearchCatalog(数据库) len = %d, want >= 3", len(results))
	}
}

func TestSearchCatalog_NoMatch(t *testing.T) {
	results := SearchCatalog("zzz-nonexistent-zzz")
	if len(results) != 0 {
		t.Errorf("SearchCatalog(no match) len = %d, want 0", len(results))
	}
}

// =============================================================================
// ListCategories 测试
// =============================================================================

func TestListCategories(t *testing.T) {
	cats := ListCategories()
	if len(cats) == 0 {
		t.Fatal("ListCategories returned empty")
	}
	// 验证去重。
	seen := make(map[CatalogCategory]bool)
	for _, c := range cats {
		if seen[c] {
			t.Errorf("duplicate category: %q", c)
		}
		seen[c] = true
	}
}

// =============================================================================
// CatalogStatistics 测试
// =============================================================================

func TestCatalogStatistics(t *testing.T) {
	st := CatalogStatistics()
	if st.Total != len(DefaultCatalog) {
		t.Errorf("Total = %d, want %d", st.Total, len(DefaultCatalog))
	}
	if len(st.ByCategory) == 0 {
		t.Error("ByCategory is empty")
	}

	// 验证总数 = 各分类之和。
	sum := 0
	for _, n := range st.ByCategory {
		sum += n
	}
	if sum != st.Total {
		t.Errorf("sum of ByCategory = %d, want %d", sum, st.Total)
	}
}

// =============================================================================
// DefaultRepoURLs / EnsureDefaultRepos 测试
// =============================================================================

func TestDefaultRepoURLs(t *testing.T) {
	urls := DefaultRepoURLs()
	if len(urls) == 0 {
		t.Fatal("DefaultRepoURLs is empty")
	}
	// 验证 bitnami 仓库。
	if urls["bitnami"] == "" {
		t.Error("bitnami URL is empty")
	}
	if !strings.HasPrefix(urls["bitnami"], "https://") {
		t.Errorf("bitnami URL = %q, want https://", urls["bitnami"])
	}
	// 验证所有 catalog 引用的仓库都有 URL。
	for _, item := range DefaultCatalog {
		if _, ok := urls[item.Repo]; !ok {
			t.Errorf("repo %q referenced by %q but not in DefaultRepoURLs", item.Repo, item.ID)
		}
	}
}

func TestEnsureDefaultRepos(t *testing.T) {
	mock := newMockReturn(map[string]string{
		"repo add": "",
	})
	m := NewRepoManagerWithCLI(mock)

	added, err := EnsureDefaultRepos(m)
	if err != nil {
		t.Fatalf("EnsureDefaultRepos failed: %v", err)
	}
	if len(added) == 0 {
		t.Error("no repos added")
	}

	// 验证所有仓库已注册。
	urls := DefaultRepoURLs()
	for name := range urls {
		if _, err := m.GetRepo(name); err != nil {
			t.Errorf("repo %q not registered: %v", name, err)
		}
	}

	// 再次调用应跳过已存在的仓库。
	added2, err := EnsureDefaultRepos(m)
	if err != nil {
		t.Fatalf("second EnsureDefaultRepos failed: %v", err)
	}
	if len(added2) != 0 {
		t.Errorf("second call should add 0 repos, got %d", len(added2))
	}
}

func TestEnsureDefaultRepos_NilManager(t *testing.T) {
	_, err := EnsureDefaultRepos(nil)
	if err == nil {
		t.Fatal("EnsureDefaultRepos(nil) should fail")
	}
}

// =============================================================================
// CatalogItem 字段测试
// =============================================================================

func TestCatalogItem_DefaultValues(t *testing.T) {
	mysql := GetCatalogItem("mysql")
	if mysql.DefaultValues == nil {
		t.Fatal("mysql DefaultValues is nil")
	}
	auth, ok := mysql.DefaultValues["auth"].(map[string]interface{})
	if !ok {
		t.Fatalf("mysql DefaultValues[auth] type = %T", mysql.DefaultValues["auth"])
	}
	if auth["database"] != "appdb" {
		t.Errorf("auth.database = %v, want appdb", auth["database"])
	}
}

func TestCatalogItem_Icons(t *testing.T) {
	// 验证关键应用有图标。
	for _, id := range []string{"mysql", "redis", "prometheus", "grafana"} {
		item := GetCatalogItem(id)
		if item.Icon == "" {
			t.Errorf("item %q has empty Icon", id)
		}
		if !strings.HasPrefix(item.Icon, "http") {
			t.Errorf("item %q Icon = %q, want http URL", id, item.Icon)
		}
	}
}

func TestCatalogItem_HomePages(t *testing.T) {
	// 验证关键应用有主页。
	for _, id := range []string{"mysql", "redis", "prometheus"} {
		item := GetCatalogItem(id)
		if item.HomePage == "" {
			t.Errorf("item %q has empty HomePage", id)
		}
	}
}
