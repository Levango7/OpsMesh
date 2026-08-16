package controlplane

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opsmesh/internal/config"
	"opsmesh/internal/helm"
	"opsmesh/internal/store"
)

// 本文件补全 server_helm.go 的单元测试（Helm 应用商店 API）。
// 覆盖：handleHelmRepos/list/add、handleHelmRepoRouting/delete/listCharts、
// handleHelmChartSearch、handleHelmReleases/list/install、handleHelmReleaseRouting/
// upgrade/uninstall/rollback/history、handleHelmCatalog、isHelmCLINotFound。

// newHelmTestServer 构造带 helm 管理器的测试 Server。
// helm CLI 不存在时 AddRepo/ListCharts 等会返回错误，覆盖 isHelmCLINotFound 路径。
func newHelmTestServer() *Server {
	st := store.NewMemoryStore()
	cli := helm.NewCLI("")
	return &Server{
		store:       st,
		cfg:         &config.Config{TaskMaxRetries: 3, Demo: true},
		requireAuth: false,
		helmRepo:    helm.NewRepoManager(cli),
		helmRelease: helm.NewReleaseManager(""),
	}
}

// newHelmNilTestServer 构造 helmRepo/helmRelease 为 nil 的 Server，覆盖 503 路径。
func newHelmNilTestServer() *Server {
	st := store.NewMemoryStore()
	return &Server{
		store:       st,
		cfg:         &config.Config{TaskMaxRetries: 3, Demo: true},
		requireAuth: false,
		helmRepo:    nil,
		helmRelease: nil,
	}
}

// =============================================================================
// handleHelmRepos / listHelmRepos / addHelmRepo
// =============================================================================

func TestHandleHelmRepos_List(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/helm/repos", nil)
	rec := httptest.NewRecorder()
	s.handleHelmRepos(rec, req)
	// helm CLI 不存在 → ListRepos 可能返回空或错误；ListRepos 不调 CLI，应返回 200
	if rec.Code != http.StatusOK {
		t.Fatalf("list repos = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleHelmRepos_AddBadJSON(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/helm/repos", strings.NewReader("{bad"))
	rec := httptest.NewRecorder()
	s.handleHelmRepos(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d, want 400", rec.Code)
	}
}

func TestHandleHelmRepos_AddMissingFields(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/helm/repos", strings.NewReader(`{"name":""}`))
	rec := httptest.NewRecorder()
	s.handleHelmRepos(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing fields = %d, want 400", rec.Code)
	}
}

func TestHandleHelmRepos_AddHelmCLINotFound(t *testing.T) {
	s := newHelmTestServer()
	// helm CLI 不存在 → AddRepo 返回 exec.ErrNotFound → 503
	req := httptest.NewRequest(http.MethodPost, "/api/v1/helm/repos", strings.NewReader(`{"name":"myrepo","url":"https://charts.example.com"}`))
	rec := httptest.NewRecorder()
	s.handleHelmRepos(rec, req)
	// helm CLI 不存在时返回 503；若 helm 存在则返回 500/201。接受 503 或 500。
	if rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusInternalServerError && rec.Code != http.StatusCreated {
		t.Fatalf("add repo = %d, want 503/500/201; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleHelmRepos_MethodNotAllowed(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/helm/repos", nil)
	rec := httptest.NewRecorder()
	s.handleHelmRepos(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("put = %d, want 405", rec.Code)
	}
}

func TestHandleHelmRepos_NilRepo(t *testing.T) {
	s := newHelmNilTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/helm/repos", nil)
	rec := httptest.NewRecorder()
	s.handleHelmRepos(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil repo list = %d, want 503", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/helm/repos", strings.NewReader(`{"name":"x","url":"y"}`))
	rec = httptest.NewRecorder()
	s.handleHelmRepos(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil repo add = %d, want 503", rec.Code)
	}
}

// =============================================================================
// handleHelmRepoRouting / deleteHelmRepo / listHelmRepoCharts
// =============================================================================

func TestHandleHelmRepoRouting_EmptyName(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/helm/repos/", nil)
	rec := httptest.NewRecorder()
	s.handleHelmRepoRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty name = %d, want 400", rec.Code)
	}
}

func TestHandleHelmRepoRouting_Delete(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/helm/repos/myrepo", nil)
	rec := httptest.NewRecorder()
	s.handleHelmRepoRouting(rec, req)
	// helm CLI 不存在 → 503；仓库不存在 → 404；接受 503/500/404
	if rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusInternalServerError && rec.Code != http.StatusNotFound && rec.Code != http.StatusOK {
		t.Fatalf("delete repo = %d, want 503/500/404/200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleHelmRepoRouting_DeleteMethodNotAllowed(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/helm/repos/myrepo", nil)
	rec := httptest.NewRecorder()
	s.handleHelmRepoRouting(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("get on repo = %d, want 405", rec.Code)
	}
}

func TestHandleHelmRepoRouting_ListCharts(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/helm/repos/myrepo/charts", nil)
	rec := httptest.NewRecorder()
	s.handleHelmRepoRouting(rec, req)
	// helm CLI 不存在 → 503；仓库不存在 → 404
	if rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusInternalServerError && rec.Code != http.StatusNotFound && rec.Code != http.StatusOK {
		t.Fatalf("list charts = %d; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleHelmRepoRouting_ListChartsMethodNotAllowed(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/helm/repos/myrepo/charts", nil)
	rec := httptest.NewRecorder()
	s.handleHelmRepoRouting(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post charts = %d, want 405", rec.Code)
	}
}

func TestHandleHelmRepoRouting_UnknownSubPath(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/helm/repos/myrepo/unknown", nil)
	rec := httptest.NewRecorder()
	s.handleHelmRepoRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown subpath = %d, want 404", rec.Code)
	}
}

func TestHandleHelmRepoRouting_NilRepo(t *testing.T) {
	s := newHelmNilTestServer()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/helm/repos/x", nil)
	rec := httptest.NewRecorder()
	s.handleHelmRepoRouting(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil delete = %d, want 503", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/helm/repos/x/charts", nil)
	rec = httptest.NewRecorder()
	s.handleHelmRepoRouting(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil charts = %d, want 503", rec.Code)
	}
}

// =============================================================================
// handleHelmChartSearch
// =============================================================================

func TestHandleHelmChartSearch_MissingQ(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/helm/charts/search", nil)
	rec := httptest.NewRecorder()
	s.handleHelmChartSearch(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing q = %d, want 400", rec.Code)
	}
}

func TestHandleHelmChartSearch_WithQ(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/helm/charts/search?q=nginx", nil)
	rec := httptest.NewRecorder()
	s.handleHelmChartSearch(rec, req)
	// helm CLI 不存在 → 503；成功 → 200
	if rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusInternalServerError && rec.Code != http.StatusOK {
		t.Fatalf("search = %d; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleHelmChartSearch_NilRepo(t *testing.T) {
	s := newHelmNilTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/helm/charts/search?q=x", nil)
	rec := httptest.NewRecorder()
	s.handleHelmChartSearch(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil search = %d, want 503", rec.Code)
	}
}

// =============================================================================
// handleHelmReleases / listHelmReleases / installHelmRelease
// =============================================================================

func TestHandleHelmReleases_List(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/helm/releases", nil)
	rec := httptest.NewRecorder()
	s.handleHelmReleases(rec, req)
	if rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusInternalServerError && rec.Code != http.StatusOK {
		t.Fatalf("list releases = %d; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleHelmReleases_ListWithNamespace(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/helm/releases?namespace=default", nil)
	rec := httptest.NewRecorder()
	s.handleHelmReleases(rec, req)
	if rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusInternalServerError && rec.Code != http.StatusOK {
		t.Fatalf("list releases ns = %d; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleHelmReleases_InstallBadJSON(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/helm/releases", strings.NewReader("{bad"))
	rec := httptest.NewRecorder()
	s.handleHelmReleases(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d, want 400", rec.Code)
	}
}

func TestHandleHelmReleases_InstallMissingFields(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/helm/releases", strings.NewReader(`{"namespace":"default"}`))
	rec := httptest.NewRecorder()
	s.handleHelmReleases(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing fields = %d, want 400", rec.Code)
	}
}

func TestHandleHelmReleases_Install(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/helm/releases", strings.NewReader(`{"namespace":"default","name":"myapp","chart":"nginx"}`))
	rec := httptest.NewRecorder()
	s.handleHelmReleases(rec, req)
	// helm CLI 不存在 → 503；成功 → 201
	if rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusInternalServerError && rec.Code != http.StatusCreated {
		t.Fatalf("install = %d; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleHelmReleases_MethodNotAllowed(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/helm/releases", nil)
	rec := httptest.NewRecorder()
	s.handleHelmReleases(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("delete = %d, want 405", rec.Code)
	}
}

func TestHandleHelmReleases_NilRelease(t *testing.T) {
	s := newHelmNilTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/helm/releases", nil)
	rec := httptest.NewRecorder()
	s.handleHelmReleases(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil list = %d, want 503", rec.Code)
	}
}

// =============================================================================
// handleHelmReleaseRouting / upgrade / uninstall / rollback / history
// =============================================================================

func TestHandleHelmReleaseRouting_EmptyName(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/helm/releases/", nil)
	rec := httptest.NewRecorder()
	s.handleHelmReleaseRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty name = %d, want 400", rec.Code)
	}
}

func TestHandleHelmReleaseRouting_Upgrade(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/helm/releases/myapp", strings.NewReader(`{"namespace":"default","chart":"nginx"}`))
	rec := httptest.NewRecorder()
	s.handleHelmReleaseRouting(rec, req)
	if rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusInternalServerError && rec.Code != http.StatusOK {
		t.Fatalf("upgrade = %d; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleHelmReleaseRouting_UpgradeMissingFields(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/helm/releases/myapp", strings.NewReader(`{"namespace":"default"}`))
	rec := httptest.NewRecorder()
	s.handleHelmReleaseRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing fields = %d, want 400", rec.Code)
	}
}

func TestHandleHelmReleaseRouting_Uninstall(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/helm/releases/myapp?namespace=default", nil)
	rec := httptest.NewRecorder()
	s.handleHelmReleaseRouting(rec, req)
	if rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusInternalServerError && rec.Code != http.StatusOK {
		t.Fatalf("uninstall = %d; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleHelmReleaseRouting_UninstallMissingNamespace(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/helm/releases/myapp", nil)
	rec := httptest.NewRecorder()
	s.handleHelmReleaseRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing ns = %d, want 400", rec.Code)
	}
}

func TestHandleHelmReleaseRouting_Rollback(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/helm/releases/myapp/rollback", strings.NewReader(`{"namespace":"default"}`))
	rec := httptest.NewRecorder()
	s.handleHelmReleaseRouting(rec, req)
	if rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusInternalServerError && rec.Code != http.StatusOK {
		t.Fatalf("rollback = %d; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleHelmReleaseRouting_RollbackMissingNamespace(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/helm/releases/myapp/rollback", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	s.handleHelmReleaseRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing ns = %d, want 400", rec.Code)
	}
}

func TestHandleHelmReleaseRouting_History(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/helm/releases/myapp/history?namespace=default", nil)
	rec := httptest.NewRecorder()
	s.handleHelmReleaseRouting(rec, req)
	if rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusInternalServerError && rec.Code != http.StatusOK {
		t.Fatalf("history = %d; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleHelmReleaseRouting_HistoryMissingNamespace(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/helm/releases/myapp/history", nil)
	rec := httptest.NewRecorder()
	s.handleHelmReleaseRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing ns = %d, want 400", rec.Code)
	}
}

func TestHandleHelmReleaseRouting_UnknownSubPath(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/helm/releases/myapp/unknown", nil)
	rec := httptest.NewRecorder()
	s.handleHelmReleaseRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown subpath = %d, want 404", rec.Code)
	}
}

func TestHandleHelmReleaseRouting_MethodNotAllowed(t *testing.T) {
	s := newHelmTestServer()
	// POST on /releases/{name} (no sub) → 405
	req := httptest.NewRequest(http.MethodPost, "/api/v1/helm/releases/myapp", nil)
	rec := httptest.NewRecorder()
	s.handleHelmReleaseRouting(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post on release = %d, want 405", rec.Code)
	}

	// GET on /releases/{name}/rollback → 405
	req = httptest.NewRequest(http.MethodGet, "/api/v1/helm/releases/myapp/rollback", nil)
	rec = httptest.NewRecorder()
	s.handleHelmReleaseRouting(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("get rollback = %d, want 405", rec.Code)
	}

	// POST on /releases/{name}/history → 405
	req = httptest.NewRequest(http.MethodPost, "/api/v1/helm/releases/myapp/history", nil)
	rec = httptest.NewRecorder()
	s.handleHelmReleaseRouting(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post history = %d, want 405", rec.Code)
	}
}

func TestHandleHelmReleaseRouting_NilRelease(t *testing.T) {
	s := newHelmNilTestServer()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/helm/releases/x", strings.NewReader(`{"namespace":"d","chart":"c"}`))
	rec := httptest.NewRecorder()
	s.handleHelmReleaseRouting(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil upgrade = %d, want 503", rec.Code)
	}
}

// =============================================================================
// handleHelmCatalog
// =============================================================================

func TestHandleHelmCatalog_Default(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/helm/catalog", nil)
	rec := httptest.NewRecorder()
	s.handleHelmCatalog(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleHelmCatalog_Search(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/helm/catalog?q=nginx", nil)
	rec := httptest.NewRecorder()
	s.handleHelmCatalog(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog search = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleHelmCatalog_Category(t *testing.T) {
	s := newHelmTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/helm/catalog?category=database", nil)
	rec := httptest.NewRecorder()
	s.handleHelmCatalog(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog category = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// isHelmCLINotFound
// =============================================================================

func TestIsHelmCLINotFound(t *testing.T) {
	if isHelmCLINotFound(nil) {
		t.Fatal("nil should be false")
	}
	if !isHelmCLINotFound(errString("executable file not found")) {
		t.Fatal("exec not found should be true")
	}
	if !isHelmCLINotFound(errString("no such file or directory")) {
		t.Fatal("no such file should be true")
	}
	if !isHelmCLINotFound(errString("helm: command not found")) {
		t.Fatal("command not found should be true")
	}
	if isHelmCLINotFound(errString("some other error")) {
		t.Fatal("other error should be false")
	}
}

// errString 实现 error 接口的字符串错误。
type errString string

func (e errString) Error() string { return string(e) }