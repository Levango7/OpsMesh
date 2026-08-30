// server_middleware.go — HTTP 中间件链：安全头/CSRF/recovery/metrics 记录 + 路径归一化
package controlplane

import (
	cryptoRand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"opsmesh/internal/controlplane/paginate"

	"opsmesh/internal/logx"
	"opsmesh/internal/proto"
)

func (s *Server) securityHeadersMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		// 修复 5：Permissions-Policy 禁用敏感设备权限。
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		// 修复 5：HSTS 仅 HTTPS 部署时注入（tlsCert 非空表示启用了 TLS）。
		if s.tlsCert != "" {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		// 修复 6：CSP nonce-based 收紧。
		// 每请求生成 16 字节随机 nonce（hex 编码 32 字符），注入 CSP 头。

		// 安全加固（CSP 收紧 — 已完成）：
		// 个人版前端已在 v0.6.1 收敛为引导页（internal/controlplane/web/index.html），
		// 业务 JS 已删除，仅剩外部 <script type="module" src="/assets/main.js">，无 inline script。
		// 企业版前端是 Vue3+Vite 编译产物（web/enterprise/dist/），<script> 均为外部 src 引用，
		// Vue 的 @click 编译为 addEventListener（非 inline onclick），无 inline script。
		// → script-src 已移除 'unsafe-inline'，仅保留 'self' + 'nonce-{nonce}'（nonce 作防御纵深）。

		// style-src 仍保留 'unsafe-inline'：企业版 Vue 组件使用 :style 绑定（运行时注入 inline style，
		// 如 ProgressRing/MetricsCard/DataTable 等 7 处），个人版引导页亦有 inline <style> 块。
		// style 的 inline 安全风险显著低于 script（无法执行代码），保留是可接受的安全取舍。
		// 后续若需进一步收紧 style-src，可将 :style 绑定改为 class 切换 + 预定义 CSS 变量。
		nonceBytes := make([]byte, 16)
		if _, err := cryptoRand.Read(nonceBytes); err != nil {
			// 随机数生成失败（极罕见）：回退到固定 nonce（仅影响 CSP 强度，不阻断请求）。
			nonceBytes = []byte("fallback-nonce-v1")
		}
		nonce := hex.EncodeToString(nonceBytes)
		w.Header().Set("Content-Security-Policy",
			fmt.Sprintf("default-src 'self'; script-src 'self' 'nonce-%s'; style-src 'self' 'unsafe-inline' 'nonce-%s'; img-src 'self' data:; connect-src 'self'", nonce, nonce))
		h.ServeHTTP(w, r)
	})
}

// csrfOriginCheck 是 CSRF Origin 校验中间件（安全加固）。
// 对状态变更方法（POST/PUT/DELETE/PATCH）校验 Origin 头，防跨站提交。
//
// 校验规则：
//   - demo 模式（s.cfg.Demo == true）：跳过校验（保持本地体验）。
//   - 非状态变更方法（GET/HEAD/OPTIONS 等）：直接放行。
//   - Origin 头为空：放行（同源请求或非浏览器客户端如 curl/agent，不破坏程序化调用）。
//   - Origin 非空：解析其 host:port，与 s.cfg.AdvertiseAddr 的 host:port 比对；
//     不匹配 → 403 Forbidden（疑似跨站 CSRF）。
//   - AdvertiseAddr 为空：跳过校验（开发模式未配置，回退本机；生产模式应由 config.Validate 强制配置）。
//
// 设计取舍：采用 Origin 头而非 Referer，因 Origin 在跨站 POST 中始终存在且不含路径，
// 比 Referer 更稳定（Referer 可能被 Referrer-Policy=no-referrer 剥离）。
func (s *Server) csrfOriginCheck(h http.Handler) http.Handler {
	// 预解析 AdvertiseAddr 的 host:port，避免每请求重复解析。
	// advertiseHost 为空表示未配置或解析失败，此时跳过校验（向后兼容）。
	advertiseHost := ""
	if s.cfg != nil && s.cfg.AdvertiseAddr != "" {
		if u, err := url.Parse(s.cfg.AdvertiseAddr); err == nil && u.Host != "" {
			advertiseHost = u.Host
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 仅状态变更方法校验；GET/HEAD/OPTIONS 等读方法无 CSRF 风险。
		method := r.Method
		if method != http.MethodPost && method != http.MethodPut &&
			method != http.MethodDelete && method != http.MethodPatch {
			h.ServeHTTP(w, r)
			return
		}
		// demo 模式跳过（保持本地体验）。
		if s.cfg != nil && s.cfg.Demo {
			h.ServeHTTP(w, r)
			return
		}
		// AdvertiseAddr 未配置：跳过校验（开发模式兼容；生产应由 Validate 强制配置）。
		if advertiseHost == "" {
			h.ServeHTTP(w, r)
			return
		}
		origin := r.Header.Get("Origin")
		if origin == "" {
			// Origin 为空：同源请求或非浏览器客户端（curl/agent），放行不破坏程序化调用。
			h.ServeHTTP(w, r)
			return
		}
		// 解析 Origin 头（格式 http(s)://host:port），提取 host:port 比对。
		ou, err := url.Parse(origin)
		if err != nil || ou.Host == "" {
			// Origin 格式非法：保守拒绝（浏览器发的 Origin 应总是合法 URL）。
			paginate.JSONError(w, http.StatusForbidden, "invalid Origin header")
			return
		}
		if ou.Host != advertiseHost {
			// Origin host 与 AdvertiseAddr host 不匹配：疑似跨站 CSRF，拒绝。
			s.audit(r.Context(), &proto.AuditEvent{
				TenantID: "default", UserID: clientIP(r, s.cfg.TrustProxy), Action: "csrf_origin_rejected", Target: r.URL.Path,
				Detail: fmt.Sprintf("origin=%s expected_host=%s remote=%s", origin, advertiseHost, r.RemoteAddr),
			})
			paginate.JSONError(w, http.StatusForbidden, "origin not allowed")
			return
		}
		h.ServeHTTP(w, r)
	})
}

// recoveryMiddleware 兜底盘：捕获任何 handler 内的 panic，避免单请求崩溃拖垮整个 HTTP 服务
// （致命短板——internal/ 生产代码零 recover，某 handler 未预期 panic 会击穿 net/http 默认
// recover 仅打印日志但仍返回 200 空响应，掩盖故障且无 trace）。此处返回 500 + 结构化错误 + traceID，
// 并交由 logx 落结构化日志。
func recoveryMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				ctx := logx.WithTrace(r.Context(), "http-recover")
				logx.Error(ctx, "HTTP handler panic recovered",
					fmt.Errorf("%v", rec), "method", r.Method, "path", r.URL.Path)
				// net/http 在 WriteHeader 已调用后无法覆写状态码；此时仅记录，避免二次 panic。
				if w.Header().Get("Content-Type") == "" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					if err := json.NewEncoder(w).Encode(map[string]string{
						"error":   "internal server error",
						"traceId": logx.Trace(ctx),
					}); err != nil {
						log.Printf("controlplane: panic recover 写错误响应失败: %v", err)
					}
				}
			}
		}()
		h.ServeHTTP(w, r)
	})
}

// ============================================================================
// ：HTTP 指标中间件（请求计数器 + 延迟直方图）
// ============================================================================

// httpMetricsMiddleware 记录 HTTP 请求指标到 s.metrics：
//   - opsmesh_http_requests_total{method,path,status}
//   - opsmesh_http_request_duration_seconds_bucket/sum/count{method,path,status}
//
// 设计要点：
//  1. 包在 recoveryMiddleware 外层，使 panic 被 recovery 转为 500 后仍能被本中间件记录为 status=500。
//  2. 路径归一化（normalizePath）避免高基数：/api/v1/devices/123 -> /api/v1/devices/:id，
//     防止每个设备 ID 产生独立时序，拖垮 metrics 基数与 Prometheus 存储。
//  3. statusRecorder 透传 Flush() 以支持 SSE（sse.go 用 http.Flusher 流式推送）。
//  4. /metrics 端点在独立 server（buildMetrics），不经本中间件，无自递归观测问题。
//  5. /healthz、/readyz 仍被记录（探针流量也需观测，便于发现探针异常与频率漂移）。
func (s *Server) httpMetricsMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		h.ServeHTTP(rec, r)
		elapsed := time.Since(start).Seconds()
		path := normalizePath(r.URL.Path)
		status := strconv.Itoa(rec.status)
		s.metrics.IncHTTPRequest(r.Method, path, status)
		s.metrics.ObserveHTTPRequestDuration(r.Method, path, status, elapsed)
	})
}

// statusRecorder 包装 http.ResponseWriter 捕获最终状态码，供 HTTP 指标中间件读取。
// 透传 Flush() 以支持 SSE 流式响应（sse.go 依赖 http.Flusher）。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Flush 透传到底层 ResponseWriter（若实现 http.Flusher），支持 SSE。
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// normalizePath 归一化 URL 路径，避免 metrics 标签高基数。
// 规则：纯数字路径段替换为 :id（设备/任务/用户等资源 ID），
// 版本段（v1/v2 含字母）不受影响。
// 例：/api/v1/devices/123 -> /api/v1/devices/:id
//
//	/api/v1/tasks/batch    -> /api/v1/tasks/batch（不变）
//	/api/v1/users/u-abc-1  -> /api/v1/users/u-abc-1（不变，含字母）
func normalizePath(p string) string {
	if p == "" || p == "/" {
		return p
	}
	// 快速路径：无数字段直接返回（多数 API 路径不含数字 ID）。
	if !strings.ContainsAny(p, "0123456789") {
		return p
	}
	parts := strings.Split(p, "/")
	changed := false
	for i, part := range parts {
		if part == "" || !isAllDigits(part) {
			continue
		}
		parts[i] = ":id"
		changed = true
	}
	if !changed {
		return p
	}
	return strings.Join(parts, "/")
}

// isAllDigits 判断字符串是否全为数字字符（且非空）。
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
