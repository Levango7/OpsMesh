package controlplane

// license_gate.go 商业授权闸门（Open-Core）。
//
// 解决的问题：企业版前端（/enterprise/）此前经 go:embed 编进同一二进制、无条件对外服务，
// 任何拿到镜像/二进制的人都能使用全部企业版功能——**商业模式在技术上不成立**。
//
// 本文件把授权状态解析为一个可查询、可审计的对象，并在交付层设闸：
//  1. 外壳层：未授权时 /enterprise/ 返回「未授权」说明页而非完整应用
//     （社区用户仍能看到能力清单与获取路径，转化不阻断）；
//  2. 资源层：未授权时 /enterprise/assets/* 返回 402，防止直接拉走分包自行拼壳；
//  3. 查询层：GET /api/v1/license 无需授权即可查（但需登录）——
//     未授权的用户恰恰最需要知道"为什么没授权"，该端点若也上闸就成了死锁。
//
// 刻意**没有**做的事：不按 API 路径前缀大面积设闸。内核 API（/api/v1/**）按
// Apache-2.0 对所有人开放，这是开源内核的价值所在；把它们闸掉会让社区版形同残废，
// 且与 LICENSE 的声明相矛盾。企业版的独占价值在**前端交付物 + 商业支持 + SLA**，
// 交付层设闸即可覆盖。
//
// 内核功能（/api/v1/**、agent 通道、监控）在任何授权状态下都完全可用。
//
// 重要：这是**许可控制，不是安全边界**。校验逻辑与二进制都在客户手里，
// 篡改二进制即可绕过（见 internal/config/license.go 顶部说明）。它的价值在于
// 给"付费能力"一个明确的授权语义与可留档的凭据，而非防破解。

import (
	"errors"
	"html"
	"net/http"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// htmlEscape 转义插入 HTML 的动态文本。
// 当前 reason 来源受控（哨兵错误文案 + 配置解析错误），但配置解析错误可能回显用户输入
// （如公钥文件路径），走一遍转义避免把它变成注入点。
func htmlEscape(s string) string { return html.EscapeString(s) }

// licenseState 是启动时解析一次的授权状态。
//
// 用 RWMutex 而非裸字段：状态本身在 NewServer 内定稿，但 /api/v1/license 与
// 闸门会并发读，留出 reload 余地（后续接 License Server 时不必改调用方）。
type licenseState struct {
	mu       sync.RWMutex
	resolved bool
	licensed bool
	edition  string
	customer string
	devices  int
	expires  time.Time
	reason   string          // 未授权原因（面向运维，不含密钥材料）
	license  *config.License // 解析出的载荷；未授权时为 nil
}

// initLicenseState 解析配置中的授权凭据并定稿状态。
//
// 语义（与 internal/config/license.go 一致）：
//   - 未配公钥 → 社区版（不做校验，不视为配置错误——开源内核本就无需授权）；
//   - 配了公钥但没配凭据 → 社区版，reason 提示缺失；
//   - 凭据无效/过期/版本不符 → 社区版，reason 记录原因。
//
// 任何失败都**不阻止启动**：授权降级不应让已交付的内核不可用。
func initLicenseState(cfg *config.Config) *licenseState {
	st := &licenseState{}
	if cfg == nil {
		st.reason = "config 未初始化"
		st.resolved = true
		return st
	}
	if cfg.LicensePublicKey == "" {
		st.reason = "未配置授权公钥（--license-public-key），按社区版运行"
		st.resolved = true
		return st
	}
	pub, err := config.ParsePublicKey(cfg.LicensePublicKey)
	if err != nil {
		st.reason = "授权公钥解析失败：" + err.Error()
		st.resolved = true
		return st
	}
	lic, err := config.VerifyLicense(cfg.LicenseKey, pub, time.Now())
	if err != nil {
		st.reason = licenseReasonText(err)
		st.resolved = true
		return st
	}
	st.resolved = true
	st.licensed = true
	st.edition = lic.Edition
	st.customer = lic.Customer
	st.devices = lic.Devices
	st.expires = lic.Expires
	st.license = lic
	return st
}

// licenseReasonText 把哨兵错误翻成运维可读的中文原因。
//
// 不返回原始 error 文本给 HTTP 响应体（避免透出验签内部细节），
// 只在 /api/v1/license 这类需鉴权的端点里以 reason 字段呈现。
func licenseReasonText(err error) string {
	switch {
	case errors.Is(err, config.ErrLicenseAbsent):
		return "未配置授权凭据（--license-key），按社区版运行"
	case errors.Is(err, config.ErrLicenseMalformed):
		return "授权凭据格式非法"
	case errors.Is(err, config.ErrLicenseSignature):
		return "授权凭据签名校验失败"
	case errors.Is(err, config.ErrLicenseExpired):
		return "授权已过期：" + err.Error()
	case errors.Is(err, config.ErrLicenseEdition):
		return "授权档位不覆盖企业版：" + err.Error()
	default:
		return "授权校验失败：" + err.Error()
	}
}

// licensed 报告是否已授权企业版。
func (s *Server) licensed() bool {
	if s.lic == nil {
		return false
	}
	s.lic.mu.RLock()
	defer s.lic.mu.RUnlock()
	return s.lic.licensed
}

// licenseSnapshot 返回可对外展示的授权状态（不含任何密钥材料）。
func (s *Server) licenseSnapshot() map[string]any {
	if s.lic == nil {
		return map[string]any{"edition": "community", "licensed": false, "reason": "授权模块未初始化"}
	}
	s.lic.mu.RLock()
	defer s.lic.mu.RUnlock()
	out := map[string]any{
		"edition":  "community",
		"licensed": s.lic.licensed,
	}
	if s.lic.licensed {
		out["edition"] = s.lic.edition
		out["customer"] = s.lic.customer
		out["devices"] = s.lic.devices
		out["expiresAt"] = rfc3339OrEmpty(s.lic.expires)
	} else {
		out["reason"] = s.lic.reason
	}
	return out
}

// handleLicenseStatus 处理 GET /api/v1/license：返回当前授权状态。
//
// 权限点选 platform:read 而非 diagnostics:*：
//   - diagnostics 组只有 :dump（导配置转储）与 :execute（调日志级别），**没有 :read**
//     ——用不存在的权限名会让 RBAC 派生出空集，所有人（含 admin）都拿不到授权；
//   - 查档位本质是"看平台配置"，platform:read 语义吻合且 viewer 即可，
//     未授权用户（往往只有最低权限）才查得到自己为什么是社区版。
//
// 需鉴权：授权态含客户名与设备上限，属商务信息。
// 无论是否授权都能访问（社区用户也要知道自己是什么档位），但需登录。
func (s *Server) handleLicenseStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireProd(w, r, "platform:read"); !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusOK, s.licenseSnapshot())
}

// requireEnterpriseGate 企业级 API 的授权闸门。
// 未授权 → 402 Payment Required（明确区别于 401 未登录 / 403 无权限）。
func (s *Server) requireEnterpriseGate(w http.ResponseWriter, r *http.Request) bool {
	if s.licensed() {
		return true
	}
	snap := s.licenseSnapshot()
	paginate.WriteJSON(w, http.StatusPaymentRequired, map[string]any{
		"error":   "enterprise feature not licensed",
		"edition": snap["edition"],
		"reason":  snap["reason"],
		"contact": "联系 OpsMesh 商业支持获取企业版授权",
	})
	return false
}

// enterpriseUnlicensedPage 渲染「企业版未授权」说明页。
//
// 设计取舍：不给 402 空响应，而是给一张能看懂、能转化的页面——
//   - 社区用户点「进入企业版前端」时看到的是"缺什么 + 怎么买"，而不是 4xx 白屏
//     （后者既不转化，也让人误以为平台坏了）；
//   - 页面内不含任何企业版功能代码或数据，纯静态说明，篡改无意义。
//
// 页面内容全部由授权状态渲染，不含任何密钥材料；reason 已由 licenseReasonText
// 转成运维可读文案，不透出验签内部细节。
func enterpriseUnlicensedPage(snap map[string]any) []byte {
	reason, _ := snap["reason"].(string)
	if reason == "" {
		reason = "未配置企业版授权"
	}
	return []byte(`<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>OpsMesh 企业版 · 未授权</title>
<style>
  :root{--bg:#0b1220;--card:#141c2e;--text:#e6edf7;--text2:#9aa8bf;--accent:#4c8dff;--border:#233049;--warn:#f59e0b;}
  *{box-sizing:border-box;}
  body{margin:0;background:var(--bg);color:var(--text);font:15px/1.7 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"Helvetica Neue","PingFang SC","Microsoft YaHei",sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;padding:2rem;}
  .card{background:var(--card);border:1px solid var(--border);border-radius:12px;max-width:680px;padding:2rem 2.25rem;}
  h1{margin:.5rem 0 .5rem;font-size:1.3rem;}
  p{color:var(--text2);margin:.6rem 0;}
  code{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:.85rem;background:#0e1626;border:1px solid var(--border);border-radius:4px;padding:.1rem .35rem;}
  pre{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:.85rem;background:#0e1626;border:1px solid var(--border);border-radius:8px;padding:.9rem 1rem;overflow-x:auto;color:#cfe0ff;}
  a{color:var(--accent);}
  .badge{display:inline-block;background:rgba(245,158,11,.12);color:var(--warn);border:1px solid rgba(245,158,11,.35);border-radius:999px;padding:.15rem .6rem;font-size:.75rem;font-weight:600;}
  .reason{border-left:3px solid var(--warn);padding-left:.9rem;margin:1.25rem 0;}
  ul{color:var(--text2);padding-left:1.2rem;}
  li{margin:.3rem 0;}
  .hint{border-left:3px solid var(--accent);padding-left:.9rem;margin-top:1.25rem;}
</style>
</head>
<body>
  <main class="card">
    <span class="badge">社区版</span>
    <h1>企业版前端需要商业授权</h1>
    <p>你当前运行的是 <strong>OpsMesh 社区版</strong>：内核（controlplane / agent / API / 监控）按 Apache-2.0 <strong>完整可用</strong>，不受授权限制。企业版前端是独立的商业交付物，需要授权凭据才能加载。</p>
    <div class="reason">
      <p><strong>当前授权状态：</strong>` + htmlEscape(reason) + `</p>
    </div>
    <p><strong>企业版包含：</strong></p>
    <ul>
      <li>Vue3 + Vite 企业版前端（完整功能控制台，非引导页）</li>
      <li>商业 SLA 与技术支持</li>
      <li>按授权设备数计的完整功能许可</li>
    </ul>
    <p><strong>如何授权：</strong>联系 OpsMesh 商业支持获取凭据，然后以启动参数传入（无需重新编译）：</p>
    <pre>opsmesh --license-key="&lt;凭据&gt;" --license-public-key="&lt;厂商公钥&gt;"</pre>
    <div class="hint">
      <p>社区版控制台：<a href="/">返回控制台首页</a>（功能完整，可正常使用）</p>
      <p>授权状态查询：<code>GET /api/v1/license</code>（需登录）</p>
      <p>许可决策与合规说明：仓库内 <code>docs/license-decision-2026-10-03.md</code></p>
    </div>
  </main>
</body>
</html>
`)
}
