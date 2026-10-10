// ui.go 企业版前端（Vue3 + Vite）的交付路径（P0-3）。
//
// 背景（商用就绪评审 P0-3）：企业版前端此前只有源码（web/enterprise/，223 个 .js/.vue），
// 既没有 go:embed、也没有路由、也没进任何部署资产，而个人版引导页上却有「进入企业版前端 →」
// 的醒目入口——按 README 首次启动的用户点开主按钮就是 404。
//
// 本文件的交付约定：
//   - 构建产物内嵌进二进制（embed.EnterpriseFS），服务前缀 /enterprise/（与 Vite base 一致）；
//   - 源码构建（无 Node）时目录内只有占位页 → BundleAvailable()==false →
//     个人版引导页自动隐藏企业版入口，/enterprise/ 给出「未内置 + 如何构建」的说明页（不是 404）；
//   - 官方镜像（Dockerfile.controlplane 的 node 构建阶段）与 CI 会把真实产物打进来，
//     客户拿到的镜像里企业版前端开箱可用。
//
// 鉴权边界：/enterprise/ 只服务静态外壳（HTML/JS/CSS），**不做租户头校验**——
// 浏览器不会带 X-Tenant-ID，且空白外壳必须先在未登录状态下加载出登录页；
// 真正的鉴权与租户隔离由 /api/v1/* 的 RequireAuth / authctx 承担（外壳内无任何数据）。
//
// 授权边界（Open-Core，见 license_gate.go）：外壳是**商业交付物**，未授权时不得完整交付。
// 未授权时按调用方形态分流：
//   - 浏览器导航（Accept 含 text/html）→ 200 返回「未授权」说明页（能力清单 + 联系方式，
//     转化路径不阻断；用户能看到产品长什么样、差什么才能用）；
//   - XHR / JSON / 资源请求 → 402 Payment Required，与 401（未登录）、403（无权限）语义区分。
//
// 社区控制台（GET /）是 Apache-2.0 内核，**任何授权状态下都完整可用**。
package enterpriseui

import (
	"bytes"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/Levango7/OpsMesh/internal/controlplane/embed"
)

// PlaceholderPath / IndexPath 是 embed.FS 内的相对路径。
const (
	IndexPath                = "enterprise/index.html"
	Placeholder              = "enterprise/placeholder.html"
	enterpriseBundleMarkText = "OPSMESH_ENTERPRISE_BUNDLE_PLACEHOLDER"
	// AssetsPrefix 是 Vite 产物里带内容哈希的静态资源前缀（可长缓存）。
	AssetsPrefix = "enterprise/assets/"
	// enterpriseImmutableCache 仅用于带哈希的文件名（内容变化必然换名）。
	enterpriseImmutableCache = "public, max-age=31536000, immutable"
)

// enterpriseBundleOnce 缓存「是否内置了企业版前端」的判定结果。
// 前端产物经 go:embed 在编译期固定，进程存活期内不会变化，故只需判定一次。
var enterpriseBundleOnce sync.Once

var enterpriseBundleEmbedded bool

// bundleAvailable 报告本二进制是否内置了真实的企业版前端（而非占位页）。
func BundleAvailable() bool {
	enterpriseBundleOnce.Do(func() {
		enterpriseBundleEmbedded = bundleAvailableUncached()
	})
	return enterpriseBundleEmbedded
}

// bundleAvailableUncached 的实现：存在 enterprise/index.html 且不含占位标记。
// 单独拆出便于测试直接验证判定逻辑（避免 once 缓存影响用例）。
func bundleAvailableUncached() bool {
	data, err := ReadFile(IndexPath)
	if err != nil {
		// 未内置：目录里只有 placeholder.html。
		return false
	}
	return !bytes.Contains(data, []byte(enterpriseBundleMarkText))
}

// readEnterpriseFile 读嵌入文件并做路径合法性校验（拒绝穿越）。
// 回退链：请求路径 → 若不存在且有 .br/.gz 旁路文件，由调用方决定是否使用（见 serveEnterpriseAsset）。
func ReadFile(rel string) ([]byte, error) {
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

// negotiatedEncoding 按 Accept-Encoding 决定是否可用预压缩旁路（优先 br）。
// 已经是 .br/.gz 的路径、或非文本类资源不做协商。
//
// 返回 Content-Encoding 头值 encoding 与旁路文件后缀 suffix 两个值：二者对 gzip 并不相同
// （头值 gzip / 后缀 .gz），合并成一个会导致查找 ".gzip" 而静默退回未压缩原文。
func NegotiatedEncoding(r *http.Request, rel string) (encoding, suffix string, ok bool) {
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
func WriteBody(w http.ResponseWriter, r *http.Request, data []byte) {
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write(data)
}

// looksLikeFile 判断路径末段是否像具体文件（含扩展名）。
func LooksLikeFile(rel string) bool {
	idx := strings.LastIndex(rel, "/")
	last := rel[idx+1:]
	dot := strings.LastIndex(last, ".")
	return dot > 0 && dot < len(last)-1
}

// enterpriseContentType 按扩展名给出内容类型（覆盖 Vite 产物可能出现的全部类型）。
func ContentType(rel string) string {
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

// ServeStatic 输出静态文件：内容类型 + 缓存策略 + 预压缩协商。
func ServeStatic(w http.ResponseWriter, r *http.Request, rel string, data []byte) {
	w.Header().Set("Content-Type", ContentType(rel))
	if strings.HasPrefix(rel, AssetsPrefix) {
		// Vite 产物文件名带内容哈希：内容不变则 URL 不变，可放心长缓存。
		w.Header().Set("Cache-Control", enterpriseImmutableCache)
	} else {
		// index.html / sw.js / manifest.json / offline.html 等入口文件：必须每次校验，
		// 否则升级后浏览器仍加载旧外壳，引用已删除的旧分包导致白屏。
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	}
	if enc, suffix, ok := NegotiatedEncoding(r, rel); ok {
		// 预压缩旁路文件（vite-plugin-compression 产出 .br/.gz）：命中则带 Content-Encoding 直出，
		// 省掉实时压缩。Vary 必须带上：同一 URL 对不同 Accept-Encoding 有不同响应体。
		if pre, err := ReadFile(rel + "." + suffix); err == nil {
			w.Header().Set("Content-Encoding", enc)
			w.Header().Add("Vary", "Accept-Encoding")
			WriteBody(w, r, pre)
			return
		}
	}
	WriteBody(w, r, data)
}
