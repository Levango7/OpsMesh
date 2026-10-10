// enterprise_ui.go 企业版前端的 Server 端接线（TD-87 批 2 第三批：bundle 判定、静态服务、
// 编码协商、内容类型等 helper 已下沉 internal/controlplane/enterpriseui）。
package controlplane

import (
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/enterpriseui"
)

// bundleAvailable 是 enterpriseui.BundleAvailable 的薄包装（父包 license_gate_test 等处引用）。
func bundleAvailable() bool { return enterpriseui.BundleAvailable() }

func (s *Server) handleEnterpriseUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "405 Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path == "/enterprise" {
		http.Redirect(w, r, "/enterprise/", http.StatusMovedPermanently)
		return
	}
	// 授权闸门在「是否内置」之后：未内置时给的是构建指引（更可操作，且此时授权与否都打不开）。
	if !bundleAvailable() {
		w.Header().Set("X-OpsMesh-Enterprise-Bundle", "placeholder")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		data, err := enterpriseui.ReadFile(enterpriseui.Placeholder)
		if err != nil {
			// 占位页都读不到（构建异常）：明确告诉调用方企业版未内置，而不是伪装成 404。
			http.Error(w, "503 企业版前端未内置于本二进制", http.StatusServiceUnavailable)
			return
		}
		enterpriseui.WriteBody(w, r, data)
		return
	}
	// 已内置但未授权：外壳不交付。handleEnterpriseUI 只处理 HTML 外壳与 SPA 深链
	//（静态资源由更精确的 /enterprise/assets/ 路由分走），故直接返回说明页。
	//
	// 判据是 featureEnabled 而非 licensed：一份 features 不含 "frontend" 的凭据
	// （例如只买了 SSO 适配器的客户）同样不该拿到企业版前端。
	if !s.featureEnabled(FeatureEnterpriseUI) {
		w.Header().Set("X-OpsMesh-License", "community")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		enterpriseui.WriteBody(w, r, enterpriseUnlicensedPage(s.licenseSnapshot()))
		return
	}

	// 映射到 embed 内的相对路径：/enterprise/ 与 /enterprise/xxx → enterprise/xxx。
	// serveRel 记录真正被服务的那份文件（SPA 回退时为 index.html）——
	// 内容类型与缓存策略必须按它判定，否则回退出的 HTML 会被当成 octet-stream 下载。
	rel := "enterprise/" + strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/enterprise/"), "/")
	serveRel := rel
	data, err := enterpriseui.ReadFile(rel)
	if err != nil {
		// SPA 回退：vue-router 使用 history 模式（createWebHistory('/enterprise/')），
		// 直接访问 /enterprise/devices 等前端路由时服务端并无此文件，须回退首页外壳。
		// 资源路径（assets/、*.* 带扩展名）不回退：缺失的前端分包应显式 404，便于排查。
		if strings.HasPrefix(rel, enterpriseui.AssetsPrefix) || enterpriseui.LooksLikeFile(rel) {
			http.NotFound(w, r)
			return
		}
		serveRel = enterpriseui.IndexPath
		data, err = enterpriseui.ReadFile(serveRel)
		if err != nil {
			writeInternalError(r.Context(), w, "enterprise.readIndexAsset", err)
			return
		}
	}
	enterpriseui.ServeStatic(w, r, serveRel, data)
}

// handleEnterpriseAsset 服务企业版前端静态资源（/enterprise/assets/*）。
// 与个人版 handleAsset 同思路：只读 embed.FS，不回落宿主文件系统（杜绝路径穿越）。
//
// 授权闸门：未授权时资源一律 402。外壳（handleEnterpriseUI）已拦住，理论上到不了这里；
// 保留闸门是为了防止有人直接猜 URL 拉走分包自行拼壳——属于纵深防御，不是主要防线。
func (s *Server) handleEnterpriseAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "405 Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	rel := "enterprise/" + strings.TrimPrefix(r.URL.Path, "/enterprise/")
	if strings.Contains(rel, "/../") || !strings.HasPrefix(rel, enterpriseui.AssetsPrefix) {
		http.NotFound(w, r)
		return
	}
	if !bundleAvailable() {
		http.NotFound(w, r)
		return
	}
	data, err := enterpriseui.ReadFile(rel)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// 授权闸门放在"文件确实存在"之后：路径穿越与不存在的资源一律 404（无论授权态），
	// 402 只回答"这个资源存在但你没用企业版"。反过来会让 /enterprise/assets/ 这种
	// 目录请求在社区版下返回 402，把"路径不存在"伪装成"商业问题"，误导排障。
	if !s.featureEnabled(FeatureEnterpriseUI) {
		w.Header().Set("X-OpsMesh-License", "community")
		s.requireEnterpriseGate(w, r)
		return
	}
	enterpriseui.ServeStatic(w, r, rel, data)
}
