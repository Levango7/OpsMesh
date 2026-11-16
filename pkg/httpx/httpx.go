// Package httpx 提供 HTTP 响应的共享写出原语。
//
// 为什么单独成包：`pkg/` 下的中间件（auth / tenant / security / ratelimit）在**业务 handler
// 之前**短路，它们写什么形状的错误体，客户端就先拿到什么。本仓对外契约是
// `{"error":"<msg>"}`（docs/api-specification.md），而 `http.Error` 写的是 text/plain
// 裸文本 —— axios 不会解析它，前端 `request.js` 的 `j?.error || err.message` 会退化成
// 通用的「Request failed with status code 401」，用户看不到真实原因。
//
// 收编前这 20 处正是如此（门禁 internal/gates/json_error_body_gate_test.go 只扫
// internal/ 与 services/，pkg/ 在扫描面之外）。本包让收编后的写法统一，
// 并把 pkg/ 纳入那道门禁。
package httpx

import (
	"encoding/json"
	"net/http"
)

// WriteError 写统一形状的错误体：`{"error":"<msg>"}` + 指定状态码。
// msg 由调用方给出（本仓中间件的 msg 都是静态串或已校验的错误串，不含客户端可控拼接）。
func WriteError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
