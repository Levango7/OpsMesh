// enterprise_ui.go 企业版前端（Vue3 + Vite）的交付路径（P0-3）。
//
// 背景（商用就绪评审 P0-3）：企业版前端此前只有源码（web/enterprise/，223 个 .js/.vue），
// 既没有 go:embed、也没有路由、也没进任何部署资产，而个人版引导页上却有「进入企业版前端 →」
// 的醒目入口——按 README 首次启动的用户点开主按钮就是 404。
//
// 本文件的交付约定：
//   - 构建产物内嵌进二进制（embed.EnterpriseFS），服务前缀 /enterprise/（与 Vite base 一致）；
//   - 源码构建（无 Node）时目录内只有占位页 → bundleAvailable()==false →
//     个人版引导页自动隐藏企业版入口，/enterprise/ 给出「未内置 + 如何构建」的说明页（不是 404）；
//   - 官方镜像（Dockerfile.controlplane 的 node 构建阶段）与 CI 会把真实产物打进来，
//     客户拿到的镜像里企业版前端开箱可用。
//
// 鉴权边界：/enterprise/ 只服务静态外壳（HTML/JS/CSS），**不做租户头校验**——
// 浏览器不会带 X-Tenant-ID，且空白外壳必须先在未登录状态下加载出登录页；
// 真正的鉴权与租户隔离由 /api/v1/* 的 RequireAuth / authctx 承担（外壳内无任何数据）。
package controlplane

import (
	"bytes"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/Levango7/OpsMesh/internal/controlplane/embed"
)

// enterprisePlaceholderPath / enterpriseIndexPath 是 embed.FS 内的相对路径。
const (
	enterpriseIndexPath      = "enterprise/index.html"
	enterprisePlaceholder    = "enterprise/placeholder.html"
	enterpriseBundleMarkText = "OPSMESH_ENTERPRISE_BUNDLE_PLACEHOLDER"
	// enterpriseAssetsPrefix 是 Vite 产物里带内容哈希的静态资源前缀（可长缓存）。
	enterpriseAssetsPrefix = "enterprise/assets/"
	// enterpriseImmutableCache 仅用于带哈希的文件名（内容变化必然换名）。
	enterpriseImmutableCache = "public, max-age=31536000, immutable"
)

// enterpriseBundleOnce 缓存「是否内置了企业版前端」的判定结果。
// go:embed 内容在编译期固定，进程存活期内不会变化，故只需判定一次。
var enterpriseBundleOnce sync.Once

var enterpriseBundleEmbedded bool

// bundleAvailable 报告本二进制是否内置了真实的企业版前端（而非占位页）。
func bundleAvailable() bool {
	enterpriseBundleOnce.Do(func() {
		enterpriseBundleEmbedded = bundleAvailableUncached()
	})
	return enterpriseBundleEmbedded
}

// bundleAvailableUncached 的实现：存在 enterprise/index.html 且不含占位标记。
// 单独拆出便于测试直接验证判定逻辑（避免 once 缓存影响用例）。
func bundleAvailableUncached() bool {
	data, err := readEnterpriseFile(enterpriseIndexPath)
	if err != nil {
		// 未内置：目录里只有 placeholder.html。
		return false
	}
	return !bytes.Contains(data, []byte(enterpriseBundleMarkText))
}

// readEnterpriseFile 读嵌入文件并做路径合法性校验（拒绝穿越）。
// 回退链：请求路径 → 若不存在且有 .br/.gz 旁路文件，由调用方决定是否使用（见 serveEnterpriseAsset）。
func readEnterpriseFile(rel string) ([]byte, error) {
	if strings.Contains(rel, "..") || strings.HasPrefix(rel, "/") {
		return nil, fs.ErrNotExist
	}
	return embed.EnterpriseFS.ReadFile(rel)
}

// handleEnterpriseUI 服务企业版前端外壳（GET /enterprise/ 与 SPA 回退）。
//
//   - /enterprise（无尾斜杠）→ 301 到 /enterprise/（否则 Vite 的相对资源路径会挂到根上）；
//   - 未内置构建产物 → 200 返回占位说明页（X-OpsMesh-Enterprise-Bundle: placeholder）；
//   - 命中嵌入文件 → 原样返回；未命中且非资源路径 → 回退 index.html（vue-router history 模式）。
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
	if !bundleAvailable() {
		w.Header().Set("X-OpsMesh-Enterprise-Bundle", "placeholder")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		data, err := readEnterpriseFile(enterprisePlaceholder)
		if err != nil {
			// 占位页都读不到（构建异常）：明确告诉调用方企业版未内置，而不是伪装成 404。
			http.Error(w, "503 企业版前端未内置于本二进制", http.StatusServiceUnavailable)
			return
		}
		writeEnterpriseBody(w, r, data)
		return
	}

	// 映射到 embed 内的相对路径：/enterprise/ 与 /enterprise/xxx → enterprise/xxx。
	// serveRel 记录真正被服务的那份文件（SPA 回退时为 index.html）——
	// 内容类型与缓存策略必须按它判定，否则回退出的 HTML 会被当成 octet-stream 下载。
	rel := "enterprise/" + strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/enterprise/"), "/")
	serveRel := rel
	data, err := readEnterpriseFile(rel)
	if err != nil {
		// SPA 回退：vue-router 使用 history 模式（createWebHistory('/enterprise/')），
		// 直接访问 /enterprise/devices 等前端路由时服务端并无此文件，须回退首页外壳。
		// 资源路径（assets/、*.* 带扩展名）不回退：缺失的前端分包应显式 404，便于排查。
		if strings.HasPrefix(rel, enterpriseAssetsPrefix) || looksLikeFile(rel) {
			http.NotFound(w, r)
			return
		}
		serveRel = enterpriseIndexPath
		data, err = readEnterpriseFile(serveRel)
		if err != nil {
			writeInternalError(r.Context(), w, "enterprise.readIndexAsset", err)
			return
		}
	}
	serveEnterpriseStatic(w, r, serveRel, data)
}

// handleEnterpriseAsset 服务企业版前端静态资源（/enterprise/assets/*）。
// 与个人版 handleAsset 同思路：只读 embed.FS，不回落宿主文件系统（杜绝路径穿越）。
func (s *Server) handleEnterpriseAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "405 Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	rel := "enterprise/" + strings.TrimPrefix(r.URL.Path, "/enterprise/")
	if strings.Contains(rel, "/../") || !strings.HasPrefix(rel, enterpriseAssetsPrefix) {
		http.NotFound(w, r)
		return
	}
	if !bundleAvailable() {
		http.NotFound(w, r)
		return
	}
	data, err := readEnterpriseFile(rel)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	serveEnterpriseStatic(w, r, rel, data)
}

// serveEnterpriseStatic 输出静态文件：内容类型 + 缓存策略 + 预压缩协商。
func serveEnterpriseStatic(w http.ResponseWriter, r *http.Request, rel string, data []byte) {
	w.Header().Set("Content-Type", enterpriseContentType(rel))
	if strings.HasPrefix(rel, enterpriseAssetsPrefix) {
		// Vite 产物文件名带内容哈希：内容不变则 URL 不变，可放心长缓存。
		w.Header().Set("Cache-Control", enterpriseImmutableCache)
	} else {
		// index.html / sw.js / manifest.json / offline.html 等入口文件：必须每次校验，
		// 否则升级后浏览器仍加载旧外壳，引用已删除的旧分包导致白屏。
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	}
	if enc, suffix, ok := negotiatedEncoding(r, rel); ok {
		// 预压缩旁路文件（vite-plugin-compression 产出 .br/.gz）：命中则带 Content-Encoding 直出，
		// 省掉实时压缩。Vary 必须带上：同一 URL 对不同 Accept-Encoding 有不同响应体。
		if pre, err := readEnterpriseFile(rel + "." + suffix); err == nil {
			w.Header().Set("Content-Encoding", enc)
			w.Header().Add("Vary", "Accept-Encoding")
			writeEnterpriseBody(w, r, pre)
			return
		}
	}
	writeEnterpriseBody(w, r, data)
}

// negotiatedEncoding 按 Accept-Encoding 决定是否可用预压缩旁路（优先 br）。
// 已经是 .br/.gz 的路径、或非文本类资源不做协商。
//
// 返回 Content-Encoding 头值 encoding 与旁路文件后缀 suffix 两个值：二者对 gzip 并不相同
// （头值 gzip / 后缀 .gz），合并成一个会导致查找 ".gzip" 而静默退回未压缩原文。
func negotiatedEncoding(r *http.Request, rel string) (encoding, suffix string, ok bool) {
	if strings.HasSuffix(rel, ".br") || strings.HasSuffix(rel, ".gz") {
		return "", "", false
	}
	ae := r.Header.Get("Accept-Encoding")
	if ae == "" {
		return "", "", false
	}
	if strings.Contains(ae, "br") {
		return "br", "br", true
	}
	if strings.Contains(ae, "gzip") {
		return "gzip", "gz", true
	}
	return "", "", false
}

// writeEnterpriseBody 写响应体并正确对待 HEAD（只发头不发体）。
func writeEnterpriseBody(w http.ResponseWriter, r *http.Request, data []byte) {
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write(data)
}

// looksLikeFile 判断路径末段是否像具体文件（含扩展名）。
func looksLikeFile(rel string) bool {
	idx := strings.LastIndex(rel, "/")
	last := rel[idx+1:]
	dot := strings.LastIndex(last, ".")
	return dot > 0 && dot < len(last)-1
}

// enterpriseContentType 按扩展名给出内容类型（覆盖 Vite 产物可能出现的全部类型）。
func enterpriseContentType(rel string) string {
	lower := strings.ToLower(rel)
	switch {
	case strings.HasSuffix(lower, ".js"), strings.HasSuffix(lower, ".mjs"), strings.HasSuffix(lower, ".br"), strings.HasSuffix(lower, ".gz"):
		// .br/.gz 是压缩旁路文件，其底层类型由 Content-Type 表达（Content-Encoding 另设）。
		return "application/javascript; charset=utf-8"
	case strings.HasSuffix(lower, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(lower, ".html"):
		return "text/html; charset=utf-8"
	case strings.HasSuffix(lower, ".json"), strings.HasSuffix(lower, ".map"), strings.HasSuffix(lower, ".webmanifest"):
		return "application/json; charset=utf-8"
	case strings.HasSuffix(lower, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(lower, ".png"):
		return "image/png"
	case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(lower, ".gif"):
		return "image/gif"
	case strings.HasSuffix(lower, ".webp"):
		return "image/webp"
	case strings.HasSuffix(lower, ".avif"):
		return "image/avif"
	case strings.HasSuffix(lower, ".ico"):
		return "image/x-icon"
	case strings.HasSuffix(lower, ".woff2"):
		return "font/woff2"
	case strings.HasSuffix(lower, ".woff"):
		return "font/woff"
	case strings.HasSuffix(lower, ".ttf"):
		return "font/ttf"
	case strings.HasSuffix(lower, ".txt"):
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}
