// server_helm.go M3 集成：Helm 应用商店 API（仓库/Chart/Release/Catalog）。
//
// 与 internal/helm/ 包的关系：
//   - helm.RepoManager 提供 AddRepo/RemoveRepo/ListRepos/ListCharts/SearchCharts/GetChart；
//   - helm.ReleaseManager 提供 Install/Upgrade/Rollback/Uninstall/List/History/Get；
//   - helm.DefaultCatalog 提供 ListByCategory/ListCategories/SearchCatalog 静态目录；
//   - 本文件仅做 HTTP 层适配（参数解析、错误转换、JSON 响应），不实现业务逻辑。
//
// API 路由（在 server.go 注册）：
//   - GET/POST        /api/v1/helm/repos                  — 列表/添加仓库
//   - DELETE          /api/v1/helm/repos/{name}           — 删除仓库
//   - GET             /api/v1/helm/charts/search?q=xxx    — 搜索 chart
//   - GET             /api/v1/helm/repos/{name}/charts    — 列表仓库 chart
//   - GET/POST        /api/v1/helm/releases               — 列表/安装 release
//   - PUT/DELETE      /api/v1/helm/releases/{name}        — 升级/卸载 release
//   - POST            /api/v1/helm/releases/{name}/rollback — 回滚 release
//   - GET             /api/v1/helm/releases/{name}/history — release 历史
//   - GET             /api/v1/helm/catalog                — 预置应用目录
//
// 设计要点：
//   - helmRepo/helmRelease 由 NewServer 构造（kubeconfig 默认空，使用 KUBECONFIG 环境变量）；
//   - 所有写操作需 helm:write 权限，读操作需 helm:read 权限；
//   - helm CLI 不存在时返回 503 + 友好错误（前端提示用户安装 helm）；
//   - 路径参数 {name} 经 URL 解码后传入 helm 包，不再做额外校验（helm 包内校验）。
package controlplane

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"opsmesh/internal/helm"
)

// ============================================================================
// Helm 仓库管理 API
// ============================================================================

// handleHelmRepos 处理 /api/v1/helm/repos：GET 列表 / POST 添加。
func (s *Server) handleHelmRepos(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listHelmRepos(w, r)
	case http.MethodPost:
		s.addHelmRepo(w, r)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// listHelmRepos 返回所有已注册的 Helm 仓库。
func (s *Server) listHelmRepos(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireProd(w, r, "helm:read"); !ok {
		return
	}
	if s.helmRepo == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "helm not initialized"})
		return
	}
	repos := s.helmRepo.ListRepos()
	writeJSON(w, http.StatusOK, map[string]interface{}{"repos": repos})
}

// addHelmRepo 添加 Helm 仓库。body: {name, url, type?}。
func (s *Server) addHelmRepo(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireProd(w, r, "helm:write"); !ok {
		return
	}
	if s.helmRepo == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "helm not initialized"})
		return
	}
	var req struct {
		Name string `json:"name"`
		URL  string `json:"url"`
		Type string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if req.Name == "" || req.URL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and url required"})
		return
	}
	repo := &helm.ChartRepo{Name: req.Name, URL: req.URL, Type: helm.RepoType(req.Type)}
	if err := s.helmRepo.AddRepo(repo); err != nil {
		// helm CLI 不存在或命令失败时返回 503，便于前端区分。
		status := http.StatusInternalServerError
		if isHelmCLINotFound(err) {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, repo)
}

// handleHelmRepoRouting 分派 /api/v1/helm/repos/{name} 子路径：
//   - DELETE /api/v1/helm/repos/{name}             — 删除仓库
//   - GET    /api/v1/helm/repos/{name}/charts      — 列表仓库 chart
func (s *Server) handleHelmRepoRouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/helm/repos/")
	if rest == "" {
		jsonError(w, http.StatusBadRequest, "repo name required")
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	name := parts[0]
	sub := ""
	if len(parts) == 2 {
		sub = parts[1]
	}
	switch sub {
	case "":
		// /api/v1/helm/repos/{name}
		if r.Method != http.MethodDelete {
			jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.deleteHelmRepo(w, r, name)
	case "charts":
		// /api/v1/helm/repos/{name}/charts
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.listHelmRepoCharts(w, r, name)
	default:
		jsonError(w, http.StatusNotFound, "unknown sub-path: "+sub)
	}
}

// deleteHelmRepo 删除 Helm 仓库。
func (s *Server) deleteHelmRepo(w http.ResponseWriter, r *http.Request, name string) {
	if _, ok := s.requireProd(w, r, "helm:write"); !ok {
		return
	}
	if s.helmRepo == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "helm not initialized"})
		return
	}
	if err := s.helmRepo.RemoveRepo(name); err != nil {
		status := http.StatusInternalServerError
		if isHelmCLINotFound(err) {
			status = http.StatusServiceUnavailable
		}
		// 仓库不存在返回 404。
		if strings.Contains(err.Error(), "不存在") {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "name": name})
}

// listHelmRepoCharts 列出指定仓库的所有 chart。
func (s *Server) listHelmRepoCharts(w http.ResponseWriter, r *http.Request, name string) {
	if _, ok := s.requireProd(w, r, "helm:read"); !ok {
		return
	}
	if s.helmRepo == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "helm not initialized"})
		return
	}
	charts, err := s.helmRepo.ListCharts(name)
	if err != nil {
		status := http.StatusInternalServerError
		if isHelmCLINotFound(err) {
			status = http.StatusServiceUnavailable
		}
		if strings.Contains(err.Error(), "不存在") {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"charts": charts})
}

// ============================================================================
// Helm Chart 搜索 API
// ============================================================================

// handleHelmChartSearch 处理 GET /api/v1/helm/charts/search?q=xxx：搜索 chart。
func (s *Server) handleHelmChartSearch(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireProd(w, r, "helm:read"); !ok {
		return
	}
	if s.helmRepo == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "helm not initialized"})
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "q parameter required"})
		return
	}
	charts, err := s.helmRepo.SearchCharts(q)
	if err != nil {
		status := http.StatusInternalServerError
		if isHelmCLINotFound(err) {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"charts": charts, "query": q})
}

// ============================================================================
// Helm Release 管理 API
// ============================================================================

// handleHelmReleases 处理 /api/v1/helm/releases：GET 列表 / POST 安装。
func (s *Server) handleHelmReleases(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listHelmReleases(w, r)
	case http.MethodPost:
		s.installHelmRelease(w, r)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// listHelmReleases 列出 release。query: ?namespace=xxx（空则列所有 namespace）。
func (s *Server) listHelmReleases(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireProd(w, r, "helm:read"); !ok {
		return
	}
	if s.helmRelease == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "helm not initialized"})
		return
	}
	ns := r.URL.Query().Get("namespace")
	var (
		releases []*helm.Release
		err      error
	)
	if ns == "" {
		releases, err = s.helmRelease.ListAll()
	} else {
		releases, err = s.helmRelease.List(ns)
	}
	if err != nil {
		status := http.StatusInternalServerError
		if isHelmCLINotFound(err) {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"releases": releases})
}

// installHelmRelease 安装 release。body: {namespace, name, chart, values?}。
func (s *Server) installHelmRelease(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireProd(w, r, "helm:write"); !ok {
		return
	}
	if s.helmRelease == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "helm not initialized"})
		return
	}
	var req struct {
		Namespace string                 `json:"namespace"`
		Name      string                 `json:"name"`
		Chart     string                 `json:"chart"`
		Values    map[string]interface{} `json:"values"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if req.Namespace == "" || req.Name == "" || req.Chart == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "namespace, name, chart required"})
		return
	}
	rel, err := s.helmRelease.Install(req.Namespace, req.Name, req.Chart, req.Values)
	if err != nil {
		status := http.StatusInternalServerError
		if isHelmCLINotFound(err) {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, rel)
}

// handleHelmReleaseRouting 分派 /api/v1/helm/releases/{name} 子路径：
//   - PUT    /api/v1/helm/releases/{name}             — 升级 release
//   - DELETE /api/v1/helm/releases/{name}             — 卸载 release
//   - POST   /api/v1/helm/releases/{name}/rollback    — 回滚 release
//   - GET    /api/v1/helm/releases/{name}/history     — release 历史
func (s *Server) handleHelmReleaseRouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/helm/releases/")
	if rest == "" {
		jsonError(w, http.StatusBadRequest, "release name required")
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	name := parts[0]
	sub := ""
	if len(parts) == 2 {
		sub = parts[1]
	}
	switch sub {
	case "":
		// /api/v1/helm/releases/{name}
		switch r.Method {
		case http.MethodPut:
			s.upgradeHelmRelease(w, r, name)
		case http.MethodDelete:
			s.uninstallHelmRelease(w, r, name)
		default:
			jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case "rollback":
		if r.Method != http.MethodPost {
			jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.rollbackHelmRelease(w, r, name)
	case "history":
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.helmReleaseHistory(w, r, name)
	default:
		jsonError(w, http.StatusNotFound, "unknown sub-path: "+sub)
	}
}

// upgradeHelmRelease 升级 release。body: {namespace, chart, values?}。
func (s *Server) upgradeHelmRelease(w http.ResponseWriter, r *http.Request, name string) {
	if _, ok := s.requireProd(w, r, "helm:write"); !ok {
		return
	}
	if s.helmRelease == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "helm not initialized"})
		return
	}
	var req struct {
		Namespace string                 `json:"namespace"`
		Chart     string                 `json:"chart"`
		Values    map[string]interface{} `json:"values"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if req.Namespace == "" || req.Chart == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "namespace and chart required"})
		return
	}
	rel, err := s.helmRelease.Upgrade(req.Namespace, name, req.Chart, req.Values)
	if err != nil {
		status := http.StatusInternalServerError
		if isHelmCLINotFound(err) {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, rel)
}

// uninstallHelmRelease 卸载 release。query: ?namespace=xxx。
func (s *Server) uninstallHelmRelease(w http.ResponseWriter, r *http.Request, name string) {
	if _, ok := s.requireProd(w, r, "helm:write"); !ok {
		return
	}
	if s.helmRelease == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "helm not initialized"})
		return
	}
	ns := r.URL.Query().Get("namespace")
	if ns == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "namespace query parameter required"})
		return
	}
	if err := s.helmRelease.Uninstall(ns, name); err != nil {
		status := http.StatusInternalServerError
		if isHelmCLINotFound(err) {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "uninstalled", "name": name})
}

// rollbackHelmRelease 回滚 release。body: {namespace, revision?}（revision 省略则回滚到上一版本）。
func (s *Server) rollbackHelmRelease(w http.ResponseWriter, r *http.Request, name string) {
	if _, ok := s.requireProd(w, r, "helm:write"); !ok {
		return
	}
	if s.helmRelease == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "helm not initialized"})
		return
	}
	var req struct {
		Namespace string `json:"namespace"`
		Revision  int    `json:"revision"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if req.Namespace == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "namespace required"})
		return
	}
	rel, err := s.helmRelease.Rollback(req.Namespace, name, req.Revision)
	if err != nil {
		status := http.StatusInternalServerError
		if isHelmCLINotFound(err) {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, rel)
}

// helmReleaseHistory 获取 release 历史。query: ?namespace=xxx。
func (s *Server) helmReleaseHistory(w http.ResponseWriter, r *http.Request, name string) {
	if _, ok := s.requireProd(w, r, "helm:read"); !ok {
		return
	}
	if s.helmRelease == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "helm not initialized"})
		return
	}
	ns := r.URL.Query().Get("namespace")
	if ns == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "namespace query parameter required"})
		return
	}
	history, err := s.helmRelease.History(ns, name)
	if err != nil {
		status := http.StatusInternalServerError
		if isHelmCLINotFound(err) {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"history": history, "name": name})
}

// ============================================================================
// Helm 应用商店目录 API
// ============================================================================

// handleHelmCatalog 处理 GET /api/v1/helm/catalog：预置应用目录。
// query: ?category=xxx 按分类过滤；?q=xxx 搜索。
func (s *Server) handleHelmCatalog(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireProd(w, r, "helm:read"); !ok {
		return
	}
	q := r.URL.Query().Get("q")
	category := r.URL.Query().Get("category")

	if q != "" {
		// 搜索模式。
		items := helm.SearchCatalog(q)
		writeJSON(w, http.StatusOK, map[string]interface{}{"items": items, "query": q})
		return
	}

	if category != "" {
		// 按分类过滤。
		items := helm.ListByCategory(helm.CatalogCategory(category))
		writeJSON(w, http.StatusOK, map[string]interface{}{"items": items, "category": category})
		return
	}

	// 默认返回全部条目 + 分类列表 + 统计信息。
	items := helm.ListByCategory("")
	categories := helm.ListCategories()
	stats := helm.CatalogStatistics()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items":      items,
		"categories": categories,
		"stats":      stats,
	})
}

// ============================================================================
// 辅助函数
// ============================================================================

// isHelmCLINotFound 判断错误是否因 helm CLI 不存在（exec: "helm": executable file not found）。
// 用于区分 helm 未安装（503）与 helm 命令失败（500）。
func isHelmCLINotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "executable file not found") ||
		strings.Contains(msg, "no such file or directory") ||
		strings.Contains(msg, "helm: command not found")
}
