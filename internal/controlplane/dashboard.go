package controlplane

import (
	"fmt"
	"net/http"
	"strings"

	"opsmesh/internal/authctx"
)

// handleDashboard 结构化 HTML 仪表盘（GET /）。
// 静态资源已抽离至 web/index.html（embed.FS，E2 前端独立化），此处仅做租户隔离校验后吐文件。
// 展示已纳管网段 / 设备（含失败高亮，B2）/ 任务 + 告警面板（M7）+ 任务下发表单 + 设备详情抽屉。
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	// 仪表盘同样按网关注入的租户执行行级隔离：租户为空（无网关/开发模式）才看全局视图。
	actx := authctx.FromHTTPHeader(r.Header)
	if s.requireAuth && actx.TenantID == "" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "401 Unauthorized: 缺少网关注入的身份上下文（X-Tenant-ID）")
		return
	}

	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, "dashboard asset missing: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

// handleAsset 服务前端静态资源（E2 前端独立化：web/assets/* 经 embed.FS 打包）。
// 仅从嵌入的 webFS 读取 web/assets/ 下文件，不回退到宿主文件系统，杜绝路径穿越（../）。
func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	// r.URL.Path 形如 /assets/app.css /assets/app.js；embed.FS 以 web/ 为根，补前缀。
	rel := "web/" + strings.TrimPrefix(r.URL.Path, "/")
	// 仅放行 web/assets/ 下文件，任何 ../ 穿越一律 404（不回退宿主文件系统）。
	if strings.Contains(rel, "/../") || !strings.HasPrefix(rel, "web/assets/") {
		http.NotFound(w, r)
		return
	}
	data, err := webFS.ReadFile(rel)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch {
	case strings.HasSuffix(rel, ".css"):
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case strings.HasSuffix(rel, ".js"):
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case strings.HasSuffix(rel, ".html"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case strings.HasSuffix(rel, ".svg"):
		w.Header().Set("Content-Type", "image/svg+xml")
	case strings.HasSuffix(rel, ".json"):
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Write(data)
}
