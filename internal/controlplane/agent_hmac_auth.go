// agent_hmac_auth.go agent HTTP 通道的 per-agent HMAC 认证（M1 bundle / M3 decisions 共用）。
//
// 契约（与 gRPC verifyAgentSignature 同源）：
//   - X-OpsMesh-Agent-ID / X-OpsMesh-Agent-Timestamp（unix 秒）/ X-OpsMesh-Agent-Signature（hex）；
//   - 密钥取 store.AgentSecret(agentID)（注册时签发），不新增凭证体系；
//   - 签名载荷 = domain + "\n" + agentID + "\n" + timestamp
//     [+ "\n" + hex(sha256(rawBody))，仅带请求体的通道]；
//   - 时钟偏移窗口 5 分钟。
//
// 失败语义：缺头/时间戳非法/超窗/未知 agent/密钥缺失/签名不符一律 401（未知 agent 与签名错
// 同形，不泄露存在性）；签名通过后才暴露退役/禁用状态（403）。租户只取自 store.Agent(agentID)，
// 请求体/查询参数中的 tenant 一律不参与。
package controlplane

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/logx"
)

const (
	// Web/REST 通道的 agent 认证头。
	agentHMACAgentIDHeader        = "X-OpsMesh-Agent-ID"
	agentHMACAgentTimestampHeader = "X-OpsMesh-Agent-Timestamp"
	agentHMACAgentSignatureHeader = "X-OpsMesh-Agent-Signature"

	// agentHMACSignatureMaxSkew 允许的 agent 时钟偏移（与 gRPC agentSignatureMaxSkew 同值）。
	agentHMACSignatureMaxSkew = 5 * time.Minute
)

// verifyAgentHMACRequest 校验 per-agent HMAC 三头，返回 (租户, agentID)。
// domain 为签名域分隔前缀（如 "abac-bundle" / "abac-decisions"）；
// bodyDigest 为 hex(sha256(rawBody))，仅带请求体的通道使用（空串表示不含摘要）。
func (s *Server) verifyAgentHMACRequest(w http.ResponseWriter, r *http.Request, domain, bodyDigest string) (tenantID, agentID string, ok bool) {
	unauthorized := func(reason string) (string, string, bool) {
		logx.Warn(r.Context(), "agent HMAC 认证失败", "domain", domain, "reason", reason)
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return "", "", false
	}

	agentID = strings.TrimSpace(r.Header.Get(agentHMACAgentIDHeader))
	tsStr := strings.TrimSpace(r.Header.Get(agentHMACAgentTimestampHeader))
	provided := strings.TrimSpace(r.Header.Get(agentHMACAgentSignatureHeader))
	if agentID == "" || tsStr == "" || provided == "" {
		return unauthorized("missing agent identity headers")
	}

	tsSec, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return unauthorized("agent timestamp is not a valid unix second")
	}
	if skew := time.Since(time.Unix(tsSec, 0)); skew > agentHMACSignatureMaxSkew || skew < -agentHMACSignatureMaxSkew {
		return unauthorized("agent timestamp out of skew")
	}

	if s.store == nil {
		return unauthorized("agent store unavailable")
	}
	agent := s.store.Agent(agentID)
	secret := s.store.AgentSecret(agentID)
	if agent == nil || secret == "" {
		return unauthorized("unknown agent or missing secret")
	}

	parts := []string{domain, agentID, tsStr}
	if bodyDigest != "" {
		parts = append(parts, bodyDigest)
	}
	canonical := strings.Join(parts, "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(provided), []byte(want)) {
		return unauthorized("agent signature mismatch")
	}

	// 签名通过后再暴露状态差异：避免未认证请求探测 agent 状态。
	if strings.EqualFold(agent.Status, "retired") || strings.EqualFold(agent.Status, "disabled") {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		logx.Warn(r.Context(), "agent HMAC 请求来自禁用/退役 agent", "domain", domain, "agentID", agentID, "status", agent.Status)
		return "", "", false
	}
	if strings.TrimSpace(agent.TenantID) == "" {
		return unauthorized("agent has no tenant")
	}
	return agent.TenantID, agentID, true
}
