// abac_device_guard.go 设备侧 ABAC 决策闸门（M4 机制，默认关闭）。
//
// 定位：在 agent 执行被领取任务之前做一次本地 ABAC 决策。默认 mode=off，
// 此时 Agent 不构造 DeviceGuard，worker 只多一次 nil 判断（零评估开销）。
//
// 模式语义（DecisionInfo）：
//   - off / kill switch / 设备豁免 / 选择器不命中 ⇒ bypass（放行、不评估、不留审计）；
//   - shadow ⇒ 本地评估但始终放行（Marker=shadow），would-be 决策仍写入 M3 审计；
//   - enforce ⇒ 按本地决策放行/拒绝（Marker=device）；
//     bundle 缺失/复验失败 ⇒ mutating 动作 fail-closed，read 动作 fail-open；
//     bundle 过期 ⇒ 同上（Marker=device-offline-fail-closed / device-offline-fail-open）。
//
// 评估输入映射（与 M2 EvaluateLocal 对齐）：
//   - subject = resource["agent_id"]（缺省回退 deviceID）
//   - resource = resource["deviceID"]
//   - action   = actionAttr（worker 固定传 "task:execute"）
//   - context  = resource map 的 JSON 文本（segment/os/arch/hostname/taskType 等
//     平铺键可被策略 Conditions 等值匹配；不引入 DeviceInfo 之外的标签体系）。
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/pkg/abacbundle"
)

// 设备侧决策标记（DecisionInfo.Marker）。用于日志/审计区分「没有评估」与
// 「评估后的放行/拒绝」，以及区分 fail-open / fail-closed。
const (
	DeviceMarkerBypass     = "bypass"
	DeviceMarkerShadow     = "shadow"
	DeviceMarkerDevice     = "device"
	DeviceMarkerFailClosed = "device-offline-fail-closed"
	DeviceMarkerFailOpen   = "device-offline-fail-open"
)

// DecisionInfo 是一次设备侧 ABAC 决策的结果快照。
type DecisionInfo struct {
	Allowed  bool   // 最终是否允许执行
	Enforced bool   // 是否实际强制（shadow/bypass/fail-open=false；enforce 决策与 fail-closed=true）
	Marker   string // bypass / shadow / device / device-offline-fail-closed / device-offline-fail-open
	Reason   string // 人类可读原因（策略 reason 或离线原因）
	PolicyID string // 命中的策略 ID（无命中/离线为空）
}

// DeviceGuardDevice 是选择器/豁免匹配所需的设备属性快照。
// 字段与 proto.DeviceInfo 对齐；AgentID 独立于 DeviceID（多设备 agent 场景）。
type DeviceGuardDevice struct {
	DeviceID string
	AgentID  string
	Segment  string
	OS       string
	Arch     string
	Hostname string
}

// DeviceGuard 在 agent 侧执行 ABAC 本地决策。并发安全：所有字段构造后只读，
// 评估经由 ABACBundleClient（内部有锁）与 ABACDecisionReporter（内部有锁）。
type DeviceGuard struct {
	mode       string
	killSwitch bool
	selector   map[string]string
	disabled   map[string]struct{}
	client     *ABACBundleClient
	reporter   *ABACDecisionReporter
	deviceInfo func() DeviceGuardDevice
	logger     ABACBundleLogger
}

// NewDeviceGuard 从配置构造设备侧决策闸门。
//
// mode 必须是 off|shadow|enforce（空视为 off）；selector 的未知字段会返回 error
// （Validate 已在启动期拒绝，这里兜底程序化构造的 Config）。
func NewDeviceGuard(
	cfg *config.Config,
	client *ABACBundleClient,
	reporter *ABACDecisionReporter,
	provider func() DeviceGuardDevice,
	logger ABACBundleLogger,
) (*DeviceGuard, error) {
	if logger == nil {
		logger = logxABACBundleLogger{}
	}
	g := &DeviceGuard{
		mode:       config.ABACDeviceModeOff,
		client:     client,
		reporter:   reporter,
		deviceInfo: provider,
		logger:     logger,
	}
	if cfg == nil {
		return g, nil
	}
	mode, err := config.NormalizeABACDeviceMode(cfg.ABACDeviceMode)
	if err != nil {
		return nil, err
	}
	selector, err := config.ParseABACDeviceSelector(cfg.ABACDeviceSelector)
	if err != nil {
		return nil, fmt.Errorf("设备侧 ABAC 选择器非法: %w", err)
	}
	g.mode = mode
	g.killSwitch = cfg.ABACDeviceKillSwitch
	g.selector = selector
	g.disabled = config.ParseABACDeviceDisabledIDs(cfg.ABACDeviceDisabledIDs)
	return g, nil
}

// Decide 对一次动作做设备侧 ABAC 决策。
//
// actionAttr="read" 视为只读动作（离线时 fail-open）；其他值视为变更动作
// （离线时 fail-closed）。resource 至少包含 deviceID；worker 还会传
// segment/os/arch/hostname/taskType/taskID/agent_id。
func (g *DeviceGuard) Decide(actionAttr string, resource map[string]any) DecisionInfo {
	if g == nil {
		return DecisionInfo{Allowed: true, Marker: DeviceMarkerBypass, Reason: "device guard not configured"}
	}
	if g.mode == config.ABACDeviceModeOff {
		return DecisionInfo{Allowed: true, Marker: DeviceMarkerBypass, Reason: "device ABAC mode=off"}
	}
	if g.killSwitch {
		return DecisionInfo{Allowed: true, Marker: DeviceMarkerBypass, Reason: "device ABAC kill switch enabled"}
	}

	info := g.currentDevice()
	if _, ok := g.disabled[info.DeviceID]; ok {
		return DecisionInfo{Allowed: true, Marker: DeviceMarkerBypass, Reason: "device disabled by abac-device-disabled-ids"}
	}
	if !g.selectorMatches(info) {
		return DecisionInfo{Allowed: true, Marker: DeviceMarkerBypass, Reason: "device selector mismatch"}
	}

	actionAttr = strings.TrimSpace(actionAttr)
	mutating := actionAttr != "read"
	subject, resourceAttr, contextJSON := evaluationInputs(resource)
	requestID := resourceString(resource, "taskID")
	if requestID == "" {
		requestID = resourceString(resource, "requestID")
	}

	decision, evalErr := g.evaluate(subject, resourceAttr, actionAttr, contextJSON)
	if evalErr != nil {
		if g.mode == config.ABACDeviceModeShadow {
			// shadow 只观察：无论评估是否可用都放行；记录一条 would-be 审计
			// （离线原因写入 reason，便于灰度期发现 bundle 供应链问题）。
			reason := "evaluation unavailable: " + evalErr.Error()
			g.record(requestID, "", false, DeviceMarkerShadow, reason, true)
			return DecisionInfo{Allowed: true, Marker: DeviceMarkerShadow, Reason: reason}
		}
		if mutating {
			reason := "no verified policy bundle (fail-closed): " + evalErr.Error()
			g.record(requestID, "", false, DeviceMarkerFailClosed, reason, true)
			return DecisionInfo{Allowed: false, Enforced: true, Marker: DeviceMarkerFailClosed, Reason: reason}
		}
		reason := "no verified policy bundle (fail-open for read): " + evalErr.Error()
		g.record(requestID, "", true, DeviceMarkerFailOpen, reason, true)
		return DecisionInfo{Allowed: true, Marker: DeviceMarkerFailOpen, Reason: reason}
	}

	if g.mode == config.ABACDeviceModeShadow {
		// would-be 决策写审计；实际动作始终放行。
		g.record(requestID, decision.MatchedPolicyID, decision.Allowed, DeviceMarkerShadow, decision.Reason, decision.BundleExpired)
		return DecisionInfo{
			Allowed:  true,
			Marker:   DeviceMarkerShadow,
			Reason:   decision.Reason,
			PolicyID: decision.MatchedPolicyID,
		}
	}

	if decision.BundleExpired {
		if mutating {
			reason := "policy bundle expired (fail-closed): " + decision.Reason
			g.record(requestID, decision.MatchedPolicyID, false, DeviceMarkerFailClosed, reason, true)
			return DecisionInfo{
				Allowed:  false,
				Enforced: true,
				Marker:   DeviceMarkerFailClosed,
				Reason:   reason,
				PolicyID: decision.MatchedPolicyID,
			}
		}
		reason := "policy bundle expired (fail-open for read): " + decision.Reason
		g.record(requestID, decision.MatchedPolicyID, true, DeviceMarkerFailOpen, reason, true)
		return DecisionInfo{
			Allowed:  true,
			Marker:   DeviceMarkerFailOpen,
			Reason:   reason,
			PolicyID: decision.MatchedPolicyID,
		}
	}

	g.record(requestID, decision.MatchedPolicyID, decision.Allowed, DeviceMarkerDevice, decision.Reason, false)
	return DecisionInfo{
		Allowed:  decision.Allowed,
		Enforced: true,
		Marker:   DeviceMarkerDevice,
		Reason:   decision.Reason,
		PolicyID: decision.MatchedPolicyID,
	}
}

// evaluate 调用 M2 本地评估器（内部每次重新验签 LKG）。
func (g *DeviceGuard) evaluate(subject, resource, action, contextJSON string) (abacbundle.Decision, error) {
	if g.client == nil {
		return abacbundle.Decision{}, ErrNoBundle
	}
	return g.client.EvaluateLocal(subject, resource, action, contextJSON)
}

// record 写入 M3 决策审计。marker 拼入 reason（服务端 mode 仅接受
// device/device-offline，故 marker 不占用 mode 字段）。
func (g *DeviceGuard) record(requestID, policyID string, allowed bool, marker, reason string, offline bool) {
	if g.reporter == nil {
		return
	}
	mode := abacDecisionModeDevice
	if offline {
		mode = abacDecisionModeDeviceOffline
	}
	g.reporter.Record(ABACDecisionRecord{
		RequestID: requestID,
		BundleID:  g.reporter.CurrentBundleID(),
		PolicyID:  policyID,
		Allowed:   allowed,
		Reason:    marker + ": " + reason,
		Mode:      mode,
	})
	if !allowed {
		g.logger.Warn(context.Background(), "设备侧 ABAC 拒绝任务执行",
			"requestID", requestID, "marker", marker, "policyID", policyID)
	}
}

// currentDevice 返回设备属性快照（provider 为 nil 时返回空值）。
func (g *DeviceGuard) currentDevice() DeviceGuardDevice {
	if g.deviceInfo == nil {
		return DeviceGuardDevice{}
	}
	return g.deviceInfo()
}

// selectorMatches 对支持的 DeviceInfo 字段做逐键精确匹配（全部命中才通过）。
func (g *DeviceGuard) selectorMatches(info DeviceGuardDevice) bool {
	if len(g.selector) == 0 {
		return true
	}
	for field, want := range g.selector {
		var got string
		switch field {
		case "segment":
			got = info.Segment
		case "os":
			got = info.OS
		case "arch":
			got = info.Arch
		case "hostname":
			got = info.Hostname
		case "agent_id":
			got = info.AgentID
		default:
			// 构造期已拒绝未知字段；运行期兜底为不命中，避免误放行。
			return false
		}
		if got != want {
			return false
		}
	}
	return true
}

// evaluationInputs 把 resource map 映射为 M2 评估参数。
func evaluationInputs(resource map[string]any) (subject, resourceAttr, contextJSON string) {
	deviceID := resourceString(resource, "deviceID")
	subject = resourceString(resource, "agent_id")
	if subject == "" {
		subject = deviceID
	}
	resourceAttr = deviceID
	if len(resource) > 0 {
		if raw, err := json.Marshal(resource); err == nil {
			contextJSON = string(raw)
		}
	}
	return subject, resourceAttr, contextJSON
}

// resourceString 读取 resource map 中的字符串值。
func resourceString(resource map[string]any, key string) string {
	if resource == nil {
		return ""
	}
	if v, ok := resource[key].(string); ok {
		return v
	}
	return ""
}
