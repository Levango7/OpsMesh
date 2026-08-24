// server_paginate.go — JSON 响应工具与分页中间件
package controlplane

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

// writeJSON 统一写出 JSON 响应。
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ============================================================================
// 修复 4：404/405 统一 JSON 错误格式
// ============================================================================

// jsonError 替换 http.Error，返回 application/json 格式的错误响应。
// 用于所有原 http.Error 调用点，统一错误格式为 {"error": "message"}。
func jsonError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// jsonErrorMux 包装 http.ServeMux，将 404 响应统一为 JSON 格式。
// 当 mux 无匹配路由时（pattern == ""），返回 JSON 404 而非默认的 text/plain。
// 405 由各 handler 内部用 jsonError 处理（显式方法检查）。
type jsonErrorMux struct {
	inner *http.ServeMux
}

func (m *jsonErrorMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h, pattern := m.inner.Handler(r)
	if pattern == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found", "path": r.URL.Path})
		return
	}
	h.ServeHTTP(w, r)
}

// ============================================================================
// 修复 3：列表 API 分页辅助函数
// ============================================================================

// paginateResult 分页响应结构。当客户端传 page 参数时，列表 API 返回此结构；
// 不传 page 时返回原数组（向后兼容）。
type paginateResult struct {
	Data     interface{} `json:"data"`
	Total    int         `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"pageSize"`
	HasMore  bool        `json:"hasMore"`
}

// parsePagination 从 query 参数解析 page/pageSize。
// page 从 1 开始，pageSize 默认 20、上限 200。
// page == 0 表示不分页（返回全量，向后兼容）。
// 为防止 start=(page-1)*pageSize 整数溢出，page 上限 clamp 到 maxInt64/pageSize。
func parsePagination(q url.Values) (page, pageSize int) {
	pageStr := q.Get("page")
	if pageStr == "" {
		return 0, 0 // 不分页
	}
	if n, atoiErr := strconv.Atoi(pageStr); atoiErr == nil {
		page = n
	}
	if page < 1 {
		page = 1
	}
	pageSize = 20
	if v := q.Get("pageSize"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			pageSize = n
		}
	}
	if pageSize > 200 {
		pageSize = 200
	}
	// 防止 (page-1)*pageSize 溢出：page <= MaxInt64/pageSize + 1
	const maxInt = (1 << 62) - 1 // 留足余量避免乘法溢出
	maxPage := maxInt / pageSize
	if maxPage < 1 {
		maxPage = 1
	}
	if page > maxPage {
		page = maxPage
	}
	return page, pageSize
}

// responseCapture 捕获 http.Handler 的响应（状态码 + body），用于分页包装。
// 仅用于 GET 列表请求的分页捕获，非分页请求直接透传。
type responseCapture struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (c *responseCapture) WriteHeader(code int) {
	c.status = code
}

func (c *responseCapture) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	return c.body.Write(b)
}

// paginateJSONHandler 包装一个返回 JSON 数组的 handler，对 GET 请求支持 page/pageSize 分页。
// 不传 page 时直接透传（向后兼容）；传 page 时捕获原 handler 响应，解析 JSON 数组并分页。
// 用于 deploys/workflows 等外部 handler 注册的列表 API。
func paginateJSONHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Query().Get("page") == "" {
			h.ServeHTTP(w, r)
			return
		}
		// 捕获原 handler 响应
		rc := &responseCapture{ResponseWriter: w}
		h.ServeHTTP(rc, r)
		if rc.status != http.StatusOK {
			// 非 200 直接转发原响应
			for k, v := range rc.Header() {
				w.Header()[k] = v
			}
			if rc.status == 0 {
				rc.status = http.StatusOK
			}
			w.WriteHeader(rc.status)
			_, _ = w.Write(rc.body.Bytes())
			return
		}
		// 解析 JSON 数组并分页
		page, pageSize := parsePagination(r.URL.Query())
		var arr []json.RawMessage
		if err := json.Unmarshal(rc.body.Bytes(), &arr); err != nil {
			// 非 JSON 数组（可能是对象），直接转发原响应
			for k, v := range rc.Header() {
				w.Header()[k] = v
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(rc.status)
			_, _ = w.Write(rc.body.Bytes())
			return
		}
		total := len(arr)
		start := (page - 1) * pageSize
		if start >= total {
			start = total
		}
		end := start + pageSize
		if end > total {
			end = total
		}
		writeJSON(w, http.StatusOK, paginateResult{
			Data:     arr[start:end],
			Total:    total,
			Page:     page,
			PageSize: pageSize,
			HasMore:  end < total,
		})
	})
}

// ============================================================================
// 修复 7：SSRF 校验
// ============================================================================

// validateURLSSRF 校验 URL 是否安全（防 SSRF）。
// 拒绝条件：
//   - 协议非 http/https
//   - 主机解析为私网地址（10.x/172.16-31.x/192.168.x/127.x/169.254.x）
//   - IPv6 link-local（fe80::/10）
//   - 元数据地址（169.254.169.254）
//   - 主机无法解析
