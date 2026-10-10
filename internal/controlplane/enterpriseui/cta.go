// cta.go 内置企业版入口标记的剥离（TD-87 批 2 第三批：自父包 dashboard.go 迁入——
// 它与 enterprise_ui.go 同属「企业版前端接线」这一件事，集中到本包）。
package enterpriseui

import "bytes"

// CTAStart / CTAEnd 是 web/index.html 中企业版入口的包裹标记。
// 用标记而非「正则匹配 <a class="btn-enterprise">」是为了让 HTML 作者可自由改样式/文案。
const (
	CTAStart = "<!--OPSMESH_ENTERPRISE_CTA_START-->"
	CTAEnd   = "<!--OPSMESH_ENTERPRISE_CTA_END-->"
)

// StripCTA 在内置企业版前端可用时原样返回（仅去掉包裹标记），
// 否则删除标记之间的整段入口。标记缺失时原样返回（保持向后兼容，不退化为破坏性替换）。
func StripCTA(html []byte) []byte {
	start := bytes.Index(html, []byte(CTAStart))
	if start < 0 {
		return html
	}
	rest := html[start:]
	endRel := bytes.Index(rest, []byte(CTAEnd))
	if endRel < 0 {
		return html
	}
	end := start + endRel + len(CTAEnd)
	if BundleAvailable() {
		// 去标记、留内容：HTML 里不再残留内部注释。
		out := make([]byte, 0, len(html))
		out = append(out, html[:start]...)
		out = append(out, html[start+len(CTAStart):end-len(CTAEnd)]...)
		out = append(out, html[end:]...)
		return out
	}
	out := make([]byte, 0, len(html))
	out = append(out, html[:start]...)
	out = append(out, html[end:]...)
	return out
}

// handleAsset 服务前端静态资源（前端独立化：web/assets/* 经 embed.FS 打包）。
// 仅从嵌入的 webFS 读取 web/assets/ 下文件，不回退到宿主文件系统，杜绝路径穿越（../）。
