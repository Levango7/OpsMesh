// abac_bundle_endpoint.go 实现 agent 侧 ABAC 策略包（signed bundle）分发端点（M1）。
//
// 端点：GET /api/v1/agent/abac-bundle
//
// 设计要点：
//   - 租户边界来自**已认证 agent 身份**，查询参数一律不参与（防 ?tenant= 越权）；
//   - 复用 gRPC per-agent HMAC 密钥（store.AgentSecret，注册时签发），HTTP 侧用
//     X-OpsMesh-Agent-{ID,Timestamp,Signature} 三头验签，不新增凭证体系；
//   - 只下发该 agent 所在租户的策略 + TenantID=="" 的平台策略（与 abac_engine.go 的
//     评估可见性一致，绝不跨租户）；
//   - 空策略集合也签发一个合法空 bundle（agent 需要可缓存的有效工件）；
//   - ETag 为 canonical bundle 的强验证器 `"<sha256-hex>"`：按租户缓存已签名 artifact，
//     策略快照未变时 bundleID/IssuedAt 不抖动，If-None-Match 才能真正命中 304。
package controlplane

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/pkg/abacbundle"
)

const (
	// abacBundleRoute 路由路径（与 server_lifecycle.go 注册处保持一致）。
	abacBundleRoute = "/api/v1/agent/abac-bundle"

	// Web/REST 通道的 agent 认证头（契约见 docs/projects/abac-device-engine-m1-bundle-spec.md §6.2）。
	abacBundleAgentIDHeader        = "X-OpsMesh-Agent-ID"
	abacBundleAgentTimestampHeader = "X-OpsMesh-Agent-Timestamp"
	abacBundleAgentSignatureHeader = "X-OpsMesh-Agent-Signature"

	// abacBundleSignatureMaxSkew 允许的 agent 时钟偏移（与 gRPC agentSignatureMaxSkew 同值）。
	abacBundleSignatureMaxSkew = 5 * time.Minute
	// abacBundleSignaturePrefix 签名载荷域分隔前缀：HMAC(secret, prefix+"\n"+agentID+"\n"+ts)。
	abacBundleSignaturePrefix = "abac-bundle"

	// abacBundleDefaultTTL / abacBundleDefaultKeyID 是配置缺省值兜底。
	abacBundleDefaultTTL   = 24 * time.Hour
	abacBundleDefaultKeyID = "k1"
)

// abacBundleArtifact 是单个租户的一份已签名策略包缓存。
//
// 缓存的必要性：bundleID 与 IssuedAt 参与 canonical digest，若每次请求重新生成，
// ETag 每请求都变，If-None-Match 永远无法命中——agent 的轮询就退化成全量拉取。
// snapshot 是「租户 + keyId + 策略集合」的确定性摘要，只有它变化（或过期）才重签。
type abacBundleArtifact struct {
	snapshot    string
	payload     []byte
	digest      string
	etag        string
	bundleID    string
	policyCount int
	expiresAt   time.Time
}

// abacBundleClock 返回签发时间基准（UTC）。测试可注入 s.abacBundleNow 固定时钟。
func (s *Server) abacBundleClock() time.Time {
	if s.abacBundleNow != nil {
		return s.abacBundleNow().UTC()
	}
	return time.Now().UTC()
}

// abacBundleTTL 返回策略包有效期，<=0 时回退默认 24h。
func (s *Server) abacBundleTTL() time.Duration {
	if s.cfg != nil && s.cfg.ABACBundleTTL > 0 {
		return s.cfg.ABACBundleTTL
	}
	return abacBundleDefaultTTL
}

// abacBundleKeyID 返回签名密钥 ID，空值时回退 "k1"（与配置默认一致）。
func (s *Server) abacBundleKeyID() string {
	if s.cfg != nil {
		if id := strings.TrimSpace(s.cfg.ABACBundleKeyID); id != "" {
			return id
		}
	}
	return abacBundleDefaultKeyID
}

// abacBundlePrivateKey 解析配置中的 base64(std) 64 字节 Ed25519 私钥。
// 空配置与非法配置都不阻断启动：端点侧返回 503，便于先部署后配钥。
func (s *Server) abacBundlePrivateKey() (ed25519.PrivateKey, error) {
	if s.cfg == nil || strings.TrimSpace(s.cfg.ABACBundleSigningKey) == "" {
		return nil, fmt.Errorf("abac bundle signing key not configured")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s.cfg.ABACBundleSigningKey))
	if err != nil {
		return nil, fmt.Errorf("abac bundle signing key is not valid base64: %w", err)
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("abac bundle signing key length %d, want %d", len(raw), ed25519.PrivateKeySize)
	}
	return ed25519.PrivateKey(raw), nil
}

// handleAgentABACBundle 处理 GET /api/v1/agent/abac-bundle。
func (s *Server) handleAgentABACBundle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	// 配置闸先于认证：未配密钥时任何请求都是 503（含未认证请求），
	// 语义是「服务尚未就绪」而非「身份无效」。
	priv, err := s.abacBundlePrivateKey()
	if err != nil {
		w.Header().Set("Retry-After", "30")
		paginate.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "abac bundle signing not configured: set --abac-bundle-signing-key or OPSMESH_ABAC_BUNDLE_SIGNING_KEY (" + err.Error() + ")",
			"code":  "signing_not_configured",
		})
		logx.Warn(r.Context(), "ABAC 策略包端点未就绪", "err", err)
		return
	}

	tenantID, agentID, ok := s.authenticateABACBundleAgent(w, r)
	if !ok {
		return
	}

	policies := abacBundlePolicies(s.listABACPolicies(tenantID))
	artifact, err := s.abacBundleArtifactFor(tenantID, policies, priv)
	if err != nil {
		logx.Error(r.Context(), "ABAC 策略包签发失败", err,
			"tenant", tenantID, "agentID", agentID, "policies", len(policies))
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "internal error",
			"code":  "signing_failed",
		})
		return
	}

	// ETag 为强验证器：`"<canonical sha256 hex>"`（不加 W/ 前缀）。
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("ETag", artifact.etag)
	// 便利副本；权威值仍在已签名的 bundle.keyId / digest 中。
	w.Header().Set("X-OpsMesh-Bundle-Key-Id", s.abacBundleKeyID())
	w.Header().Set("X-OpsMesh-Bundle-Digest", artifact.digest)

	if abacBundleETagMatches(r.Header.Get("If-None-Match"), artifact.etag) {
		w.WriteHeader(http.StatusNotModified)
		logx.Info(r.Context(), "ABAC 策略包未变更（304）",
			"tenant", tenantID, "agentID", agentID,
			"policies", artifact.policyCount, "bundleID", artifact.bundleID)
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(artifact.payload); err != nil {
		logx.Warn(r.Context(), "ABAC 策略包响应写入失败", "tenant", tenantID, "agentID", agentID, "err", err)
		return
	}
	logx.Info(r.Context(), "ABAC 策略包已签发",
		"tenant", tenantID, "agentID", agentID,
		"policies", artifact.policyCount, "bundleID", artifact.bundleID)
}

// authenticateABACBundleAgent 校验 HTTP 通道的 per-agent HMAC 签名，返回 (租户, agentID)。
//
// 与 gRPC verifyAgentSignature（internal/controlplane/grpc/grpc.go）同源：
// 密钥取 store.AgentSecret(agentID)（注册时签发、agent 落盘 0600），不新增凭证。
// 失败一律 401（未知 agent 与签名错同形，不泄露存在性）；退役/禁用 agent 403。
func (s *Server) authenticateABACBundleAgent(w http.ResponseWriter, r *http.Request) (tenantID, agentID string, ok bool) {
	unauthorized := func(reason string) (string, string, bool) {
		logx.Warn(r.Context(), "ABAC 策略包认证失败", "reason", reason)
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return "", "", false
	}

	agentID = strings.TrimSpace(r.Header.Get(abacBundleAgentIDHeader))
	tsStr := strings.TrimSpace(r.Header.Get(abacBundleAgentTimestampHeader))
	provided := strings.TrimSpace(r.Header.Get(abacBundleAgentSignatureHeader))
	if agentID == "" || tsStr == "" || provided == "" {
		return unauthorized("missing agent identity headers")
	}

	tsSec, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return unauthorized("agent timestamp is not a valid unix second")
	}
	if skew := time.Since(time.Unix(tsSec, 0)); skew > abacBundleSignatureMaxSkew || skew < -abacBundleSignatureMaxSkew {
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

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(abacBundleSignaturePrefix + "\n" + agentID + "\n" + tsStr))
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(provided), []byte(want)) {
		return unauthorized("agent signature mismatch")
	}

	// 签名通过后再暴露状态差异：避免未认证请求探测 agent 状态。
	if strings.EqualFold(agent.Status, "retired") || strings.EqualFold(agent.Status, "disabled") {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		logx.Warn(r.Context(), "ABAC 策略包请求来自禁用/退役 agent", "agentID", agentID, "status", agent.Status)
		return "", "", false
	}
	if strings.TrimSpace(agent.TenantID) == "" {
		return unauthorized("agent has no tenant")
	}
	return agent.TenantID, agentID, true
}

// abacBundlePolicies 把控制面策略镜像映射为 bundle 条目并做确定性排序。
// 排序是 ETag 稳定性的前提：同一策略集合在不同副本/不同插入顺序下必须得到同一字节序。
func abacBundlePolicies(in []abacPolicy) []abacbundle.Policy {
	out := make([]abacbundle.Policy, 0, len(in))
	for _, p := range in {
		bp := abacbundle.Policy{
			ID:       p.ID,
			Subject:  p.Subject,
			Resource: p.Resource,
			Action:   p.Action,
			Effect:   p.Effect,
		}
		// 持久化层当前不保存 condition；仅在内存镜像确实带条件时下发副本（不共享 map）。
		if len(p.Condition) > 0 {
			cond := make(map[string]string, len(p.Condition))
			for k, v := range p.Condition {
				cond[k] = v
			}
			bp.Conditions = cond
		}
		out = append(out, bp)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch {
		case a.ID != b.ID:
			return a.ID < b.ID
		case a.Subject != b.Subject:
			return a.Subject < b.Subject
		case a.Resource != b.Resource:
			return a.Resource < b.Resource
		case a.Action != b.Action:
			return a.Action < b.Action
		default:
			return a.Effect < b.Effect
		}
	})
	return out
}

// abacBundleSnapshot 计算「租户 + keyId + 策略集合」的确定性摘要（缓存键）。
func abacBundleSnapshot(tenantID, keyID string, policies []abacbundle.Policy) (string, error) {
	raw, err := json.Marshal(policies)
	if err != nil {
		return "", fmt.Errorf("marshal policies for snapshot: %w", err)
	}
	h := sha256.New()
	_, _ = h.Write([]byte("opsmesh-abac-bundle-snapshot-v1"))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(tenantID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(keyID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(raw)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// abacBundleArtifactFor 返回该租户当前策略集合对应的已签名 artifact。
// 快照与缓存一致且未过期时直接复用（ETag/bundleID 稳定）；否则重新签发并替换缓存。
func (s *Server) abacBundleArtifactFor(tenantID string, policies []abacbundle.Policy, priv ed25519.PrivateKey) (abacBundleArtifact, error) {
	keyID := s.abacBundleKeyID()
	snapshot, err := abacBundleSnapshot(tenantID, keyID, policies)
	if err != nil {
		return abacBundleArtifact{}, err
	}
	now := s.abacBundleClock()
	ttl := s.abacBundleTTL()

	s.abacBundleMu.Lock()
	defer s.abacBundleMu.Unlock()
	if s.abacBundleCache == nil {
		s.abacBundleCache = make(map[string]abacBundleArtifact)
	}
	if cached, ok := s.abacBundleCache[tenantID]; ok && cached.snapshot == snapshot && now.Before(cached.expiresAt) {
		return cached, nil
	}

	bundleID, err := abacbundle.NewBundleID()
	if err != nil {
		return abacBundleArtifact{}, err
	}
	bundle := abacbundle.Bundle{
		SchemaVersion: abacbundle.SchemaVersion,
		BundleID:      bundleID,
		TenantScope:   tenantID,
		KeyID:         keyID,
		IssuedAt:      now,
		ExpiresAt:     now.Add(ttl),
		Policies:      policies,
	}
	signed, err := abacbundle.Sign(bundle, priv)
	if err != nil {
		return abacBundleArtifact{}, fmt.Errorf("sign bundle: %w", err)
	}
	digest, err := abacbundle.Digest(bundle)
	if err != nil {
		return abacBundleArtifact{}, fmt.Errorf("digest bundle: %w", err)
	}
	payload, err := abacbundle.Encode(signed)
	if err != nil {
		return abacBundleArtifact{}, fmt.Errorf("encode bundle: %w", err)
	}

	artifact := abacBundleArtifact{
		snapshot:    snapshot,
		payload:     payload,
		digest:      digest,
		etag:        `"` + digest + `"`,
		bundleID:    bundleID,
		policyCount: len(policies),
		expiresAt:   bundle.ExpiresAt,
	}
	s.abacBundleCache[tenantID] = artifact
	return artifact, nil
}

// abacBundleETagMatches 判断 If-None-Match 是否命中当前 ETag。
// 仅做强比较（弱验证器 W/"..." 不匹配），支持逗号分隔列表与 "*"。
func abacBundleETagMatches(ifNoneMatch, etag string) bool {
	for _, part := range strings.Split(ifNoneMatch, ",") {
		candidate := strings.TrimSpace(part)
		if candidate == "" {
			continue
		}
		if candidate == "*" || candidate == etag {
			return true
		}
	}
	return false
}
