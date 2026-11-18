// abac_decision_reporter.go 实现 agent 侧 ABAC 决策审计回传（M3）。
//
// 契约（服务端见 internal/controlplane/abac_decisions_endpoint.go）：
//   - POST /api/v1/agent/abac-decisions
//   - 认证头：X-OpsMesh-Agent-ID / X-OpsMesh-Agent-Timestamp（unix 秒）/
//     X-OpsMesh-Agent-Signature = hex(HMAC-SHA256(secret, "abac-decisions\n"+agentID+"\n"+timestamp+"\n"+hex(sha256(rawBody))))；
//     密钥复用 gRPC 注册通道下发的 per-agent secret（与 M1 策略包同一份凭证）。
//   - body：{"decisions":[{requestId,bundleId,policyId,allowed,reason,mode,occurredAt}]}
//   - 202 {"accepted":N}：只摘除已被接受的批次前缀；413：批次减半即时重试一次；
//     401/403：保留缓冲区等待下一轮（30s 退避，不做热循环）；5xx/网络：最多重试 2 次（1s/2s）。
//
// 缓冲语义：单生产者/多调用方并发安全；容量 500 的环形队列，满时丢最旧并累计
// dropped 计数（Stats 可观测）。Record 只做内存追加，绝不因网络阻塞调用方；
// 唯一的 HTTP 路径是 Start 的周期 Flush（以及测试/关闭路径的显式 Flush）。
package agent

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/pkg/abacbundle"
)

const (
	// abacDecisionRoute 与控制面注册的路由一致（server_lifecycle.go）。
	abacDecisionRoute = "/api/v1/agent/abac-decisions"
	// abacDecisionSignaturePrefix 签名载荷域分隔前缀（与服务端一致）。
	abacDecisionSignaturePrefix = "abac-decisions"
	// abacDecisionCapability 心跳能力声明值（版本化）。
	abacDecisionCapability = "abac_decisions=v1"
	// abacDecisionBufferCap 环形缓冲区容量（超出丢最旧）。
	abacDecisionBufferCap = 500
	// abacDecisionFlushInterval 周期回传间隔。
	abacDecisionFlushInterval = 30 * time.Second
	// abacDecisionRequestTimeout 单次 POST 的硬超时（不影响下一轮 ticker）。
	abacDecisionRequestTimeout = 10 * time.Second
	// abacDecisionMaxRetries 5xx/网络失败的最多重试次数（总尝试 = 1 + N）。
	abacDecisionMaxRetries = 2

	// 决策模式取值（与 contract mode 字段一致）。
	abacDecisionModeDevice        = "device"
	abacDecisionModeDeviceOffline = "device-offline"
)

// ABACDecisionRecord 是一条待回传的本地 ABAC 决策审计记录。
type ABACDecisionRecord struct {
	RequestID  string
	BundleID   string
	PolicyID   string
	Allowed    bool
	Reason     string
	Mode       string
	OccurredAt time.Time
}

// ABACDecisionReporterStats 是缓冲区与回传结果的只读快照。
type ABACDecisionReporterStats struct {
	Buffered int   // 当前待回传条数
	Dropped  int64 // 因容量上限丢弃的最旧记录累计数
	Accepted int64 // 服务端 202 接受并摘除的累计条数
	Failed   int64 // Flush 最终失败（含认证/超时/非预期状态）的轮次
}

// abacDecisionWire 是回传 body 的线上格式（occurredAt 固定 RFC3339，不带纳秒）。
type abacDecisionWire struct {
	RequestID  string `json:"requestId"`
	BundleID   string `json:"bundleId"`
	PolicyID   string `json:"policyId"`
	Allowed    bool   `json:"allowed"`
	Reason     string `json:"reason"`
	Mode       string `json:"mode"`
	OccurredAt string `json:"occurredAt"`
}

// abacDecisionBatch 是回传 body 顶层结构。
type abacDecisionBatch struct {
	Decisions []abacDecisionWire `json:"decisions"`
}

// abacDecisionAccepted 是 202 响应体。
type abacDecisionAccepted struct {
	Accepted int `json:"accepted"`
}

// abacDecisionEntry 是环形队列条目：seq 单调递增，用于 202 精确摘除
// 「已发送批次」而不误删飞行期间新追加的记录（含环形溢出后的队首漂移）。
type abacDecisionEntry struct {
	rec ABACDecisionRecord
	seq uint64
}

// ABACDecisionReporter 负责决策记录的本地缓冲与周期批量回传。
//
// 并发模型：任意 goroutine 可 Record；Start 是唯一的常驻回传 goroutine；
// Flush 用 flushMu 串行化（Start 与显式调用互斥），缓冲区用 mu 保护。
type ABACDecisionReporter struct {
	enabled  bool
	agentID  string
	secret   string
	baseURL  string
	log      ABACBundleLogger
	http     *http.Client
	now      func() time.Time
	interval time.Duration
	// retryBackoffs 5xx/网络失败的重试等待序列（长度即最大重试次数）。
	retryBackoffs []time.Duration

	// flushMu 保证同一时刻只有一个 HTTP 批次在途，避免重复摘除。
	flushMu sync.Mutex

	mu       sync.Mutex
	ring     [abacDecisionBufferCap]abacDecisionEntry
	head     int
	count    int
	nextSeq  uint64
	dropped  int64
	accepted int64
	failed   int64
	// bundleIDProvider 提供当前 LKG 包的 bundleId（RecordEvaluation 用）。
	bundleIDProvider func() string
}

// NewABACDecisionReporter 构造决策回传器。
//
// cfg 提供启用开关（ABACBundleEnabled；ABACDecisionReport 由 Agent.Run 单独判定）；
// agentID/secret 为注册后的身份与 per-agent HMAC 密钥；controlURL 为控制面 HTTP 基地址；
// logger 为 nil 时使用 logx。
func NewABACDecisionReporter(cfg *config.Config, agentID, secret, controlURL string, logger ABACBundleLogger) *ABACDecisionReporter {
	if logger == nil {
		logger = logxABACBundleLogger{}
	}
	r := &ABACDecisionReporter{
		enabled:       cfg != nil && cfg.ABACBundleEnabled,
		agentID:       strings.TrimSpace(agentID),
		secret:        secret,
		baseURL:       normalizeABACBundleBaseURL(controlURL),
		log:           logger,
		http:          &http.Client{},
		now:           time.Now,
		interval:      abacDecisionFlushInterval,
		retryBackoffs: []time.Duration{time.Second, 2 * time.Second},
	}
	return r
}

// SetBundleIDProvider 注入当前策略包 ID 的来源（通常为 ABACBundleClient.Current）。
// 允许在 Start 之前调用；内部加锁，运行期更换也安全。
func (r *ABACDecisionReporter) SetBundleIDProvider(fn func() string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.bundleIDProvider = fn
	r.mu.Unlock()
}

// CurrentBundleID 返回当前策略包 ID（未配置 provider 时为空）。
// 供 DeviceGuard 直接 Record 本地决策时填充 bundleId，保持 M3 审计字段完整。
func (r *ABACDecisionReporter) CurrentBundleID() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	provider := r.bundleIDProvider
	r.mu.Unlock()
	if provider == nil {
		return ""
	}
	return provider()
}

// Record 非阻塞追加一条记录；容量满时丢最旧并累加 dropped。
func (r *ABACDecisionReporter) Record(d ABACDecisionRecord) {
	if r == nil || !r.enabled {
		return
	}
	if d.OccurredAt.IsZero() {
		d.OccurredAt = r.nowUTC()
	}
	if d.Mode == "" {
		// 契约只允许 device / device-offline；未显式标注的本地决策一律视为在线设备。
		d.Mode = abacDecisionModeDevice
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.count == len(r.ring) {
		r.ring[r.head] = abacDecisionEntry{}
		r.head = (r.head + 1) % len(r.ring)
		r.count--
		r.dropped++
	}
	idx := (r.head + r.count) % len(r.ring)
	r.nextSeq++
	r.ring[idx] = abacDecisionEntry{rec: d, seq: r.nextSeq}
	r.count++
}

// RecordEvaluation 用本地评估结果构造并追加一条决策记录。
// bundleId 取当前 LKG 包（未配置 provider 时为空）；mode 由 BundleExpired 决定。
func (r *ABACDecisionReporter) RecordEvaluation(d abacbundle.Decision, requestID string) {
	if r == nil || !r.enabled {
		return
	}
	r.mu.Lock()
	provider := r.bundleIDProvider
	r.mu.Unlock()

	bundleID := ""
	if provider != nil {
		bundleID = provider()
	}
	r.Record(ABACDecisionRecord{
		RequestID:  requestID,
		BundleID:   bundleID,
		PolicyID:   d.MatchedPolicyID,
		Allowed:    d.Allowed,
		Reason:     d.Reason,
		Mode:       ModeFor(d),
		OccurredAt: r.nowUTC(),
	})
}

// ModeFor 把本地决策映射为回传 mode：过期 LKG 上的决策标记 device-offline。
func ModeFor(decision abacbundle.Decision) string {
	if decision.BundleExpired {
		return abacDecisionModeDeviceOffline
	}
	return abacDecisionModeDevice
}

// Capabilities 返回心跳能力声明（启用时声明 abac_decisions=v1）。
func (r *ABACDecisionReporter) Capabilities() []string {
	if r == nil || !r.enabled {
		return nil
	}
	return []string{abacDecisionCapability}
}

// Stats 返回缓冲/丢弃/接受/失败计数快照。
func (r *ABACDecisionReporter) Stats() ABACDecisionReporterStats {
	if r == nil {
		return ABACDecisionReporterStats{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return ABACDecisionReporterStats{
		Buffered: r.count,
		Dropped:  r.dropped,
		Accepted: r.accepted,
		Failed:   r.failed,
	}
}

// Start 启动周期回传循环：每 interval 检查一次，非空才 Flush；ctx 取消即退出。
// 单 goroutine；缺少控制面地址/密钥时只告警一次并等待 ctx（不让 safeGo 反复重启空循环）。
func (r *ABACDecisionReporter) Start(ctx context.Context) {
	if r == nil || !r.enabled {
		return
	}
	if r.endpointURL() == "" {
		r.log.Warn(ctx, "ABAC 决策回传未配置控制面地址，回传不启动（本地缓冲仍可写入）", "agentID", r.agentID)
		<-ctx.Done()
		return
	}
	if r.secret == "" {
		r.log.Warn(ctx, "ABAC 决策回传缺少 agent 签名密钥，回传不启动（本地缓冲仍可写入）", "agentID", r.agentID)
		<-ctx.Done()
		return
	}

	interval := r.interval
	if interval <= 0 {
		interval = abacDecisionFlushInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if r.Stats().Buffered == 0 {
				continue
			}
			// Flush 内部已按状态码记录告警/重试；这里不再重复刷日志。
			_ = r.Flush(ctx)
		}
	}
}

// Flush 将当前缓冲区快照（最多 500 条）签名回传。
//
// 成功（202）时只摘除被服务端接受的前缀：飞行期间新 Record 的记录位于队尾，
// 不会被误删；部分接受（accepted < 批次长度）时未接受部分留在缓冲区。
// 失败（401/403/413 二次仍超限/5xx 重试耗尽/网络/非预期状态）一律保留缓冲区。
func (r *ABACDecisionReporter) Flush(ctx context.Context) error {
	if r == nil || !r.enabled {
		return nil
	}
	r.flushMu.Lock()
	defer r.flushMu.Unlock()

	r.mu.Lock()
	snapshot := r.snapshotLocked(abacDecisionBufferCap)
	r.mu.Unlock()
	if len(snapshot) == 0 {
		return nil
	}
	batch := make([]ABACDecisionRecord, len(snapshot))
	for i, entry := range snapshot {
		batch[i] = entry.rec
	}

	halved := false
	retries := 0
	for {
		if err := ctx.Err(); err != nil {
			r.markFailed()
			return err
		}
		raw, err := marshalABACDecisionBatch(batch)
		if err != nil {
			r.markFailed()
			return fmt.Errorf("agent: encode ABAC decisions: %w", err)
		}
		status, respBody, err := r.post(ctx, raw)

		switch {
		case err == nil && status == http.StatusAccepted:
			accepted := len(batch)
			if len(respBody) > 0 {
				var body abacDecisionAccepted
				if derr := json.Unmarshal(respBody, &body); derr == nil {
					accepted = body.Accepted
				}
			}
			if accepted < 0 {
				accepted = 0
			}
			if accepted > len(batch) {
				accepted = len(batch)
			}
			var acceptedLastSeq uint64
			if accepted > 0 {
				acceptedLastSeq = snapshot[accepted-1].seq
			}
			r.commitAccepted(accepted, acceptedLastSeq)
			return nil

		case err == nil && status == http.StatusRequestEntityTooLarge:
			if halved || len(batch) <= 1 {
				r.markFailed()
				r.log.Warn(ctx, "ABAC 决策回传批次仍超限（413），保留缓冲区", "agentID", r.agentID, "batch", len(batch))
				return fmt.Errorf("agent: ABAC decision batch rejected as oversize (413)")
			}
			half := len(batch) / 2
			if half < 1 {
				half = 1
			}
			batch = batch[:half]
			halved = true
			r.log.Warn(ctx, "ABAC 决策回传批次超限（413），减半后重试", "agentID", r.agentID, "batch", len(batch))

		case err == nil && (status == http.StatusUnauthorized || status == http.StatusForbidden):
			r.markFailed()
			r.log.Warn(ctx, "ABAC 决策回传被拒绝（401/403），保留缓冲区等待下一轮", "agentID", r.agentID, "status", status)
			return fmt.Errorf("agent: ABAC decision upload unauthorized (status %d)", status)

		case err == nil && status >= http.StatusInternalServerError:
			retry, wait := r.nextRetry(&retries)
			if !retry {
				r.markFailed()
				r.log.Warn(ctx, "ABAC 决策回传持续失败（5xx），保留缓冲区", "agentID", r.agentID, "status", status)
				return fmt.Errorf("agent: ABAC decision upload failed (status %d)", status)
			}
			r.log.Debug(ctx, "ABAC 决策回传 5xx，稍后重试", "agentID", r.agentID, "status", status, "wait", wait.String())
			if serr := sleepWithContext(ctx, wait); serr != nil {
				r.markFailed()
				return serr
			}

		case err != nil:
			if ctx.Err() != nil {
				r.markFailed()
				return ctx.Err()
			}
			retry, wait := r.nextRetry(&retries)
			if !retry {
				r.markFailed()
				r.log.Warn(ctx, "ABAC 决策回传网络失败，保留缓冲区", "agentID", r.agentID, "err", err.Error())
				return err
			}
			r.log.Debug(ctx, "ABAC 决策回传网络失败，稍后重试", "agentID", r.agentID, "wait", wait.String(), "err", err.Error())
			if serr := sleepWithContext(ctx, wait); serr != nil {
				r.markFailed()
				return serr
			}

		default:
			r.markFailed()
			r.log.Warn(ctx, "ABAC 决策回传返回非预期状态，保留缓冲区", "agentID", r.agentID, "status", status)
			return fmt.Errorf("agent: ABAC decision upload unexpected status %d", status)
		}
	}
}

// nextRetry 返回是否还能重试及本次等待时长（1s/2s，可被测试覆盖）。
func (r *ABACDecisionReporter) nextRetry(retries *int) (bool, time.Duration) {
	if *retries >= abacDecisionMaxRetries || *retries >= len(r.retryBackoffs) {
		return false, 0
	}
	wait := r.retryBackoffs[*retries]
	*retries = *retries + 1
	return true, wait
}

// post 执行一次签名 POST，返回状态码与响应体（上限 4KiB）。
func (r *ABACDecisionReporter) post(ctx context.Context, body []byte) (int, []byte, error) {
	endpoint := r.endpointURL()
	if endpoint == "" {
		return 0, nil, fmt.Errorf("控制面地址为空")
	}
	reqCtx, cancel := context.WithTimeout(ctx, abacDecisionRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	ts := strconv.FormatInt(r.nowUTC().Unix(), 10)
	req.Header.Set("X-OpsMesh-Agent-ID", r.agentID)
	req.Header.Set("X-OpsMesh-Agent-Timestamp", ts)
	req.Header.Set("X-OpsMesh-Agent-Signature", r.sign(ts, body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := r.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, respBody, nil
}

// sign 计算 HMAC-SHA256 签名（hex），载荷为
// "abac-decisions\n<agentID>\n<timestamp>\n<hex(sha256(body))>"。
func (r *ABACDecisionReporter) sign(timestamp string, body []byte) string {
	digest := sha256.Sum256(body)
	canonical := abacDecisionSignaturePrefix + "\n" + r.agentID + "\n" + timestamp + "\n" + hex.EncodeToString(digest[:])
	mac := hmac.New(sha256.New, []byte(r.secret))
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

// endpointURL 返回决策回传端点完整 URL。
func (r *ABACDecisionReporter) endpointURL() string {
	if r.baseURL == "" {
		return ""
	}
	if strings.HasSuffix(r.baseURL, abacDecisionRoute) {
		return r.baseURL
	}
	return r.baseURL + abacDecisionRoute
}

// nowUTC 返回当前 UTC 时间（可注入时钟，测试用）。
func (r *ABACDecisionReporter) nowUTC() time.Time {
	if r.now == nil {
		return time.Now().UTC()
	}
	return r.now().UTC()
}

// snapshotLocked 从队首取最多 limit 条（调用方持 r.mu）。
func (r *ABACDecisionReporter) snapshotLocked(limit int) []abacDecisionEntry {
	n := r.count
	if limit > 0 && n > limit {
		n = limit
	}
	out := make([]abacDecisionEntry, n)
	for i := 0; i < n; i++ {
		out[i] = r.ring[(r.head+i)%len(r.ring)]
	}
	return out
}

// commitAccepted 摘除序号不超过 lastSeq 的队首条目并累计 accepted（调用方不持锁）。
// 只按 seq 前缀摘除：飞行期间追加的记录 seq 更大，即使环形溢出导致队首漂移也不会误删。
func (r *ABACDecisionReporter) commitAccepted(n int, lastSeq uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	removed := 0
	for r.count > 0 && removed < n && r.ring[r.head].seq <= lastSeq {
		r.ring[r.head] = abacDecisionEntry{}
		r.head = (r.head + 1) % len(r.ring)
		r.count--
		removed++
	}
	r.accepted += int64(n)
}

// markFailed 累计一次最终失败的 Flush。
func (r *ABACDecisionReporter) markFailed() {
	r.mu.Lock()
	r.failed++
	r.mu.Unlock()
}

// marshalABACDecisionBatch 把记录序列化为线上 JSON（occurredAt 固定 RFC3339）。
func marshalABACDecisionBatch(records []ABACDecisionRecord) ([]byte, error) {
	batch := abacDecisionBatch{Decisions: make([]abacDecisionWire, len(records))}
	for i, rec := range records {
		batch.Decisions[i] = abacDecisionWire{
			RequestID:  rec.RequestID,
			BundleID:   rec.BundleID,
			PolicyID:   rec.PolicyID,
			Allowed:    rec.Allowed,
			Reason:     rec.Reason,
			Mode:       rec.Mode,
			OccurredAt: rec.OccurredAt.UTC().Format(time.RFC3339),
		}
	}
	return json.Marshal(batch)
}

// sleepWithContext 等待 d 或 ctx 取消。
func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// mergeCapabilities 合并多组能力声明并去重（保持首次出现顺序）。
// 心跳把 abac_bundle=v1 与 abac_decisions=v1 合并上报，重复值只保留一份。
func mergeCapabilities(groups ...[]string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, group := range groups {
		for _, value := range group {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}
