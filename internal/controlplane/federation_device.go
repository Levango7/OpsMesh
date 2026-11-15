// federation_device.go 联邦设备域操作转发。
//
// 背景：设备域存在一组必须以"设备所在控制面"为执行点的操作（退役/纳管/指标/系统清单/
// 网络安全/CMDB 发现）。同一套签名协议（pkg/auth + X-Federation-* 头 + nonce 防重放）
// 复用到设备操作转发，避免为每个操作单独开一条联邦通道。
//
// 双角色端点：POST /api/v1/federation/forward/device-op
//   - 本地用户调用：body {peerURL, op, payload}，校验 task:write 后转发到 peer；
//   - peer 调用（携带 X-Federation-Forwarded=1 且验签通过）：body {op, payload}，
//     直接在本控制面执行（peer 已由来源侧完成用户 RBAC，不再二次做用户鉴权），
//     租户边界取自 X-Tenant-ID（requireTenantContext 已校验）。
package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	authpkg "github.com/Levango7/OpsMesh/pkg/auth"
	"github.com/Levango7/OpsMesh/pkg/netsec"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// federationDeviceOpPath 设备操作转发的联邦端点路径（出站与入站共用）。
const federationDeviceOpPath = "/api/v1/federation/forward/device-op"

// federationDeviceOps 设备操作白名单（与 server_devices.go / netsec_ext_ops.go /
// cmdb_collector.go 的真实操作一一对应）：
//   - retire：DELETE /api/v1/devices/{id}（handleRetireDevice）
//   - provision：POST /api/v1/devices/{id}/provision（handleProvision）
//   - metrics：GET /api/v1/devices/{id}/metrics（handleDeviceMetrics）
//   - system：GET /api/v1/devices/{id}/system（handleDeviceSystemInfo）
//   - netsec：POST /api/v1/netsec/{capability}（handleNetSec）
//   - cmdb-discover：POST /api/v1/cmdb/collect（handleCMDBCollect）
var federationDeviceOps = map[string]bool{
	"retire":        true,
	"provision":     true,
	"metrics":       true,
	"system":        true,
	"netsec":        true,
	"cmdb-discover": true,
}

// federationDeviceOpNames 返回白名单的稳定排序文案（错误信息用）。
func federationDeviceOpNames() string {
	return "retire|provision|metrics|system|netsec|cmdb-discover"
}

// isAllowedFederationDeviceOp 判断 op 是否在白名单内。
func isAllowedFederationDeviceOp(op string) bool {
	return federationDeviceOps[op]
}

// federationPeerStatusError peer 返回非 2xx 时携带状态码与原始 body，
// 供 handler 原样透传给调用方（保持 peer 的错误语义，不把上游状态码抹成固定 502）。
type federationPeerStatusError struct {
	StatusCode int
	Body       []byte
}

func (e *federationPeerStatusError) Error() string {
	return fmt.Sprintf("peer rejected request: status=%d", e.StatusCode)
}

// ForwardDeviceOp 把设备操作转发到指定 peer（POST <peer>/api/v1/federation/forward/device-op）。
// 请求体为 {op, payload}；身份头（租户/用户/角色）参与 HMAC 签名，nonce 防重放。
// peerURL 必须精确命中配置的 peers 列表（SSRF 防护），否则不发起任何 HTTP 请求。
// 成功返回 peer 响应 body；非 2xx 返回 *federationPeerStatusError（含状态码与 body）。
func (f *FederationManager) ForwardDeviceOp(ctx context.Context, peerURL, op string, payload []byte, tenantID, userID string, roles []string) ([]byte, error) {
	if !isAllowedFederationDeviceOp(op) {
		return nil, fmt.Errorf("unsupported device op %q (allowed: %s)", op, federationDeviceOpNames())
	}
	if !f.isKnownPeer(peerURL) {
		return nil, fmt.Errorf("peerURL %q not in configured federation peers", peerURL)
	}
	body, err := json.Marshal(struct {
		Op      string `json:"op"`
		Payload []byte `json:"payload"`
	}{Op: op, Payload: payload})
	if err != nil {
		return nil, fmt.Errorf("marshal device op: %w", err)
	}
	endpoint := strings.TrimRight(peerURL, "/") + federationDeviceOpPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if tenantID != "" {
		req.Header.Set("X-Tenant-ID", tenantID)
	}
	if userID != "" {
		req.Header.Set("X-User-Id", userID)
	}
	if len(roles) > 0 {
		req.Header.Set("X-User-Roles", strings.Join(roles, ","))
	}
	if err := f.signFederationRequest(req, body); err != nil {
		return nil, err
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("peer %s unreachable: %w", peerURL, err)
	}
	defer resp.Body.Close()
	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("read peer %s response: %w", peerURL, readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return respBody, &federationPeerStatusError{StatusCode: resp.StatusCode, Body: respBody}
	}
	return respBody, nil
}

// handleFederationForwardDeviceOp 处理 POST /api/v1/federation/forward/device-op。
//
// 请求体：{ "peerURL": "http://peer1:8080", "op": "metrics", "payload": {...} }
//   - 本地调用方：需 task:write（执行类权限的规范徽章）；peer 响应 body/status 原样透传；
//   - 联邦入站（peer 调用）：仅要求 X-Federation-Forwarded=1 且 HMAC 验签通过（租户取 X-Tenant-ID），
//     不再叠加本地用户 RBAC（peer 侧已授权）。
func (s *Server) handleFederationForwardDeviceOp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.fed == nil {
		http.NotFound(w, r)
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	isPeer := r.Header.Get(authpkg.FederationHeaderForwarded) == "1"
	if !isPeer {
		// 本地调用方：转发属执行类操作，规范权限点为 task:write。
		if _, ok := s.requireProd(w, r, "task:write"); !ok {
			return
		}
	}
	var body struct {
		PeerURL string `json:"peerURL"`
		Op      string `json:"op"`
		Payload []byte `json:"payload"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if !isAllowedFederationDeviceOp(body.Op) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error": "unsupported device op (allowed: " + federationDeviceOpNames() + ")",
		})
		return
	}
	if isPeer {
		status, respBody := s.executeFederationDeviceOp(r, body.Op, body.Payload)
		if len(respBody) > 0 {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(status)
		_, _ = w.Write(respBody)
		return
	}
	if body.PeerURL == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "peerURL is required"})
		return
	}
	// SSRF 防护：peerURL 必须在配置的 peers 列表中（精确匹配）。
	if !s.fed.isKnownPeer(body.PeerURL) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "peerURL not in configured federation peers"})
		return
	}
	respBody, err := s.fed.ForwardDeviceOp(r.Context(), body.PeerURL, body.Op, body.Payload, actx.TenantID, actx.UserID, actx.Roles)
	if err != nil {
		var peerErr *federationPeerStatusError
		if errors.As(err, &peerErr) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(peerErr.StatusCode)
			_, _ = w.Write(peerErr.Body)
			return
		}
		logx.Error(r.Context(), "联邦设备操作转发失败", err, "peer", body.PeerURL, "op", body.Op)
		paginate.WriteJSON(w, http.StatusBadGateway, map[string]string{"error": "upstream peer request failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID,
		UserID:   actx.UserID,
		Action:   "federation_forward_device_op",
		Target:   body.Op,
		Detail:   sanitizeAuditDetail("forwarded to " + opsSQLString(body.PeerURL)),
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(respBody)
}

// federationDeviceOpPayload 设备操作参数（payload 为 JSON 字节，按 op 取用相关字段）。
type federationDeviceOpPayload struct {
	DeviceID   string `json:"deviceID"`
	Range      string `json:"range"`      // metrics：15m/1h/2h/6h/24h
	Capability string `json:"capability"` // netsec：ips/siem/honeypot/traffic/vpn/zerotrust
	Action     string `json:"action"`     // netsec：动作名
}

// executeFederationDeviceOp 在本地执行 peer 转发的设备操作。
// 复用现有 HTTP handler（不复制业务逻辑），子请求保留当前请求的已验签 context 标记
// 与 X-Federation-* 头，使 handler 内的 requireTenantContext/requireProd 走联邦分支
// 且不会二次消费 nonce（见 verifyFederationRequest 的幂等标记）。
func (s *Server) executeFederationDeviceOp(r *http.Request, op string, payload []byte) (int, []byte) {
	var p federationDeviceOpPayload
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &p); err != nil {
			return http.StatusBadRequest, jsonErrorBody("invalid payload: " + err.Error())
		}
	}
	rec := &federationBufferedWriter{}
	switch op {
	case "retire":
		if p.DeviceID == "" {
			return http.StatusBadRequest, jsonErrorBody("payload.deviceID is required for retire")
		}
		sub := s.federationDeviceSubRequest(r, http.MethodDelete, "/api/v1/devices/"+url.PathEscape(p.DeviceID), nil)
		s.handleRetireDevice(rec, sub, p.DeviceID)
	case "provision":
		if p.DeviceID == "" {
			return http.StatusBadRequest, jsonErrorBody("payload.deviceID is required for provision")
		}
		sub := s.federationDeviceSubRequest(r, http.MethodPost, "/api/v1/devices/"+url.PathEscape(p.DeviceID)+"/provision", nil)
		s.handleProvision(rec, sub, p.DeviceID)
	case "metrics":
		if p.DeviceID == "" {
			return http.StatusBadRequest, jsonErrorBody("payload.deviceID is required for metrics")
		}
		sub := s.federationDeviceSubRequest(r, http.MethodGet, "/api/v1/devices/"+url.PathEscape(p.DeviceID)+"/metrics", nil)
		if p.Range != "" {
			sub.URL.RawQuery = "range=" + url.QueryEscape(p.Range)
		}
		s.handleDeviceMetrics(rec, sub, p.DeviceID)
	case "system":
		if p.DeviceID == "" {
			return http.StatusBadRequest, jsonErrorBody("payload.deviceID is required for system")
		}
		sub := s.federationDeviceSubRequest(r, http.MethodGet, "/api/v1/devices/"+url.PathEscape(p.DeviceID)+"/system", nil)
		s.handleDeviceSystemInfo(rec, sub, p.DeviceID)
	case "netsec":
		capability, path, ok := federationNetSecCapability(p.Capability)
		if !ok {
			return http.StatusBadRequest, jsonErrorBody("payload.capability must be one of ips|siem|honeypot|traffic|vpn|zerotrust")
		}
		if p.DeviceID == "" || p.Action == "" {
			return http.StatusBadRequest, jsonErrorBody("payload.deviceID and payload.action are required for netsec")
		}
		netsecBody, err := json.Marshal(netsec.ActionRequest{DeviceID: p.DeviceID, Action: p.Action})
		if err != nil {
			return http.StatusInternalServerError, jsonErrorBody("encode netsec request failed")
		}
		sub := s.federationDeviceSubRequest(r, http.MethodPost, "/api/v1/netsec/"+path, netsecBody)
		s.handleNetSec(rec, sub, capability)
	case "cmdb-discover":
		sub := s.federationDeviceSubRequest(r, http.MethodPost, "/api/v1/cmdb/collect", nil)
		s.handleCMDBCollect(rec, sub)
	default:
		return http.StatusBadRequest, jsonErrorBody("unsupported device op")
	}
	return rec.result()
}

// federationNetSecCapability 把 wire 上的 capability 名映射到 pkg/netsec 能力与本地路由段。
func federationNetSecCapability(name string) (netsec.Capability, string, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "ips", "ips_ids", "ids":
		return netsec.CapIPSIDS, "ips", true
	case "siem":
		return netsec.CapSIEM, "siem", true
	case "honeypot":
		return netsec.CapHoneypot, "honeypot", true
	case "traffic", "traffic_shaping":
		return netsec.CapTrafficShaping, "traffic", true
	case "vpn":
		return netsec.CapVPN, "vpn", true
	case "zerotrust", "zero-trust":
		return netsec.CapZeroTrust, "zerotrust", true
	default:
		return "", "", false
	}
}

// federationDeviceSubRequest 构造与当前请求共享 context/身份头的本地子请求。
func (s *Server) federationDeviceSubRequest(r *http.Request, method, path string, body []byte) *http.Request {
	sub := r.Clone(r.Context())
	sub.Method = method
	sub.URL.Path = path
	sub.URL.RawQuery = ""
	if body != nil {
		sub.Body = io.NopCloser(bytes.NewReader(body))
		sub.ContentLength = int64(len(body))
	} else {
		sub.Body = http.NoBody
		sub.ContentLength = 0
	}
	return sub
}

// federationBufferedWriter 捕获本地 handler 的响应（状态码 + body），
// 供联邦转发 handler 以 peer 视角原样返回，避免把子 handler 直接写进真实 ResponseWriter。
type federationBufferedWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *federationBufferedWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *federationBufferedWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
}

func (w *federationBufferedWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(p)
}

// result 返回最终状态码与 body（未显式 WriteHeader 时默认 200）。
func (w *federationBufferedWriter) result() (int, []byte) {
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}
	return status, w.body.Bytes()
}

// jsonErrorBody 生成 {"error": msg} 形态的错误 body。
func jsonErrorBody(msg string) []byte {
	b, err := json.Marshal(map[string]string{"error": msg})
	if err != nil {
		return []byte(`{"error":"internal error"}`)
	}
	return b
}
