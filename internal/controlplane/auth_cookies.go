// auth_cookies.go 双 HttpOnly Cookie 令牌方案（同源最简单且安全）：
//   - at（access token）：短期 JWT（15min），仅标识身份，XSS 窃取后利用窗口极小；
//   - rt（refresh token）：长期不透明随机串（7d），服务端可吊销/旋转，用于静默续期。
//
// 两者均为 HttpOnly + SameSite=Lax（防 XSS 读取 / 防 CSRF 跨站携带），同源由浏览器自动携带。
package controlplane

import "net/http"

// setCookie 统一的 HttpOnly Cookie 写入（Path=/、SameSite=Lax；HTTPS 部署才置 Secure）。
// Cookie Secure：优先用 cfg.CookieSecure（HTTPS 反代终止 TLS 时显式开启），
// 回退到 TLSCert 非空（控制面直连 HTTPS 时自动启用），二者任一为 true 即置 Secure。
func (s *Server) setCookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cookieSecure(),
	})
}

// cookieSecure 决定 Cookie 的 Secure 属性。
// 优先级：cfg.CookieSecure 显式 true → true；否则回退到 TLSCert 非空（HTTPS 直连自动启用）。
// 这样既支持 HTTPS 反代终止 TLS（控制面收 HTTP 但对外 HTTPS，须显式 --cookie-secure=true），
// 又保持原有 TLSCert 兜底语义（控制面自身持证直连 HTTPS 时自动 Secure）。
func (s *Server) cookieSecure() bool {
	if s.cfg == nil {
		return false
	}
	return s.cfg.CookieSecure || s.cfg.TLSCert != ""
}

// setAccessCookie 下发短期访问令牌（at）HttpOnly Cookie。
func (s *Server) setAccessCookie(w http.ResponseWriter, token string) {
	s.setCookie(w, accessTokenCookieName, token, int(accessTokenExpiry.Seconds()))
}

// setRefreshCookie 下发刷新令牌（rt）HttpOnly Cookie（长期，服务端可吊销/旋转）。
func (s *Server) setRefreshCookie(w http.ResponseWriter, rt string) {
	s.setCookie(w, refreshTokenCookieName, rt, int(refreshTokenExpiry.Seconds()))
}

// setAuthCookies 同时下发 at + rt（登录/刷新成功时调用）。
func (s *Server) setAuthCookies(w http.ResponseWriter, at, rt string) {
	s.setAccessCookie(w, at)
	s.setRefreshCookie(w, rt)
}

// clearAuthCookies 清除 at + rt（登出时调用）。
func (s *Server) clearAuthCookies(w http.ResponseWriter) {
	s.setCookie(w, accessTokenCookieName, "", -1)
	s.setCookie(w, refreshTokenCookieName, "", -1)
}
