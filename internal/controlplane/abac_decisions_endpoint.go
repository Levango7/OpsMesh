// abac_decisions_endpoint.go ABAC 决策审计回传端点（M3）。
//
// 端点：POST /api/v1/agent/abac-decisions
//
// 契约：
//   - 认证：per-agent HMAC（X-OpsMesh-Agent-{ID,Timestamp,Signature}），签名载荷
//     HMAC-SHA256(secret, "abac-decisions\n"+agentID+"\n"+timestamp+"\n"+hex(sha256(rawBody)))；
//   - 请求体：{"decisions":[{requestId,bundleId,policyId,allowed,reason,mode,occurredAt}...]}；
//   - 限制：<=500 条、<=1 MiB；超限 413/400；模式仅 device / device-offline。
//
// 安全边界：
//   - 租户与 agent 身份只来自 store.Agent(agentID)（HMAC 验签通过后），请求体/查询参数
//     中的 tenant 一律忽略；
//   - 字符串按上限截断（requestId/bundleId/policyId<=128，reason<=512），防超长写入放大；
//   - 存储未配置返回 503（绝不静默丢弃决策）。
//
// 保留期：abacDecisionRetentionLoop 每 24h 调 DeleteABACDecisionLogsBefore(now-retention)，
// retention 默认 720h（--abac-decision-retention），以 ReceivedAt 为准。
package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/store/model"
)

const (
	// abacDecisionsRoute 路由路径（与 server_lifecycle.go 注册处保持一致）。
	abacDecisionsRoute = "/api/v1/agent/abac-decisions"

	// abacDecisionSignaturePrefix 签名载荷域分隔前缀。
	abacDecisionSignaturePrefix = "abac-decisions"

	// 请求体上限：1 MiB；单批最多 500 条决策。
	abacDecisionMaxBodyBytes = 1 << 20
	abacDecisionMaxBatch     = 500

	// 字符串长度上限（按 Unicode 字符数截断，与 MySQL VARCHAR 语义一致）。
	abacDecisionMaxIDLen     = 128
	abacDecisionMaxReasonLen = 512

	// 决策模式白名单。
	abacDecisionModeDevice        = "device"
	abacDecisionModeDeviceOffline = "device-offline"

	// 保留期与清理周期。
	abacDecisionDefaultRetention = 720 * time.Hour // 30d
	abacDecisionRetentionPeriod  = 24 * time.Hour
)

// abacDecisionItem 是 agent 上报的单条决策（字段名与 agent 端契约一致）。
type abacDecisionItem struct {
	RequestID  string `json:"requestId"`
	BundleID   string `json:"bundleId"`
	PolicyID   string `json:"policyId"`
	Allowed    bool   `json:"allowed"`
	Reason     string `json:"reason"`
	Mode       string `json:"mode"`
	OccurredAt string `json:"occurredAt"`
}

// abacDecisionsRequest 是回传请求体。Decisions 用指针区分「缺失」与「空数组」：
// 缺失字段按 malformed 拒绝，空数组是合法的空批次（accepted=0）。
type abacDecisionsRequest struct {
	Decisions *[]abacDecisionItem `json:"decisions"`
}

// handleAgentABACDecisions 处理 POST /api/v1/agent/abac-decisions。
func (s *Server) handleAgentABACDecisions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	// 存储闸先于认证：未配置存储属「服务未就绪」，任何请求都是 503。
	if s.store == nil {
		w.Header().Set("Retry-After", "30")
		paginate.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "abac decision store not configured",
			"code":  "store_unavailable",
		})
		logx.Warn(r.Context(), "ABAC 决策审计端点未就绪（store 未配置）")
		return
	}

	body, ok := abacDecisionReadBody(w, r)
	if !ok {
		return
	}

	digest := sha256.Sum256(body)
	tenantID, agentID, ok := s.verifyAgentHMACRequest(w, r, abacDecisionSignaturePrefix, hex.EncodeToString(digest[:]))
	if !ok {
		return
	}

	items, ok := abacDecisionValidateBody(w, r, body)
	if !ok {
		return
	}

	now := time.Now().UTC()
	logs := make([]model.ABACDecisionLog, 0, len(items))
	for _, d := range items {
		occurredAt, _ := time.Parse(time.RFC3339, strings.TrimSpace(d.OccurredAt))
		logs = append(logs, model.ABACDecisionLog{
			TenantID:   tenantID,
			AgentID:    agentID,
			RequestID:  abacDecisionTruncate(d.RequestID, abacDecisionMaxIDLen),
			BundleID:   abacDecisionTruncate(d.BundleID, abacDecisionMaxIDLen),
			PolicyID:   abacDecisionTruncate(d.PolicyID, abacDecisionMaxIDLen),
			Allowed:    d.Allowed,
			Reason:     abacDecisionTruncate(d.Reason, abacDecisionMaxReasonLen),
			Mode:       d.Mode,
			OccurredAt: occurredAt.UTC(),
			ReceivedAt: now,
		})
	}

	accepted := 0
	if len(logs) > 0 {
		n, err := s.store.AppendABACDecisionLogs(logs)
		if err != nil {
			logx.Error(r.Context(), "ABAC 决策审计落库失败", err,
				"tenant", tenantID, "agentID", agentID, "decisions", len(logs))
			paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "internal error",
				"code":  "persist_failed",
			})
			return
		}
		accepted = n
	}

	logx.Info(r.Context(), "ABAC 决策审计已接收",
		"tenant", tenantID, "agentID", agentID, "accepted", accepted)
	paginate.WriteJSON(w, http.StatusAccepted, map[string]int{"accepted": accepted})
}

// abacDecisionReadBody 读取并限制请求体大小（<=1 MiB）。
// 超限返回 413；读取失败返回 400。
func abacDecisionReadBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if r.ContentLength > abacDecisionMaxBodyBytes {
		paginate.WriteJSON(w, http.StatusRequestEntityTooLarge, map[string]string{
			"error": "request body too large (max 1 MiB)",
			"code":  "body_too_large",
		})
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, abacDecisionMaxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			paginate.WriteJSON(w, http.StatusRequestEntityTooLarge, map[string]string{
				"error": "request body too large (max 1 MiB)",
				"code":  "body_too_large",
			})
			return nil, false
		}
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error": "failed to read request body",
			"code":  "bad_request",
		})
		return nil, false
	}
	return body, true
}

// abacDecisionValidateBody 解析并逐条校验请求体；失败时已写 400 响应。
func abacDecisionValidateBody(w http.ResponseWriter, r *http.Request, body []byte) ([]abacDecisionItem, bool) {
	var req abacDecisionsRequest
	if err := json.Unmarshal(body, &req); err != nil {
		abacDecisionReject(w, r, "malformed JSON body")
		return nil, false
	}
	if req.Decisions == nil {
		abacDecisionReject(w, r, "missing decisions field")
		return nil, false
	}
	items := *req.Decisions
	if len(items) > abacDecisionMaxBatch {
		abacDecisionReject(w, r, "too many decisions (max 500)")
		return nil, false
	}
	for i := range items {
		switch items[i].Mode {
		case abacDecisionModeDevice, abacDecisionModeDeviceOffline:
		default:
			abacDecisionReject(w, r, "invalid mode for decision "+strconv.Itoa(i))
			return nil, false
		}
		if _, err := time.Parse(time.RFC3339, strings.TrimSpace(items[i].OccurredAt)); err != nil {
			abacDecisionReject(w, r, "invalid occurredAt for decision "+strconv.Itoa(i))
			return nil, false
		}
	}
	return items, true
}

// abacDecisionReject 写 400 并记录原因（不回显请求内容）。
func abacDecisionReject(w http.ResponseWriter, r *http.Request, reason string) {
	logx.Warn(r.Context(), "ABAC 决策审计请求被拒绝", "reason", reason)
	paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{
		"error": reason,
		"code":  "bad_request",
	})
}

// abacDecisionTruncate 按 Unicode 字符数截断字符串（不切断多字节字符）。
func abacDecisionTruncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max])
}

// abacDecisionRetention 返回决策审计保留期；未配置时回退默认 720h。
func (s *Server) abacDecisionRetention() time.Duration {
	if s.cfg != nil && s.cfg.ABACDecisionRetention > 0 {
		return s.cfg.ABACDecisionRetention
	}
	return abacDecisionDefaultRetention
}

// abacDecisionRetentionLoop 单 goroutine 保留期清理：每 24h 删除 ReceivedAt 早于
// now-retention 的记录。ctx 取消即退出；多副本并发执行是幂等删除，无需选主。
func (s *Server) abacDecisionRetentionLoop(ctx context.Context) {
	if s.store == nil {
		return
	}
	retention := s.abacDecisionRetention()
	ticker := time.NewTicker(abacDecisionRetentionPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().UTC().Add(-retention)
			n, err := s.store.DeleteABACDecisionLogsBefore(cutoff)
			if err != nil {
				logx.Warn(ctx, "ABAC 决策审计保留期清理失败", "err", err, "cutoff", cutoff)
				continue
			}
			if n > 0 {
				logx.Info(ctx, "ABAC 决策审计保留期清理", "deleted", n,
					"cutoff", cutoff, "retention", retention.String())
			}
		}
	}
}
