package agent

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/pkg/abacbundle"

	taskv1 "github.com/Levango7/OpsMesh/services/task-svc/api/proto/v1"
)

// deviceGuardTestNow 固定测试时钟，使 bundle 有效期判定确定。
var deviceGuardTestNow = time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC)

// deviceGuardTestProvider 返回固定的设备属性快照。
func deviceGuardTestProvider() DeviceGuardDevice {
	return DeviceGuardDevice{
		DeviceID: "dev-1",
		AgentID:  "agent-1",
		Segment:  "seg-a",
		OS:       "linux",
		Arch:     "amd64",
		Hostname: "host-1",
	}
}

// newDeviceGuardFixture 构造一个带已签名 LKG 包的 DeviceGuard。
// policies 为 nil 时表示「无 bundle」（模拟拉取失败/首次启动离线）。
func newDeviceGuardFixture(t *testing.T, mode string, policies []abacbundle.Policy, mutate func(*config.Config)) (*DeviceGuard, *ABACDecisionReporter, *ABACBundleClient) {
	t.Helper()
	pub, priv := newLocalEvalKey(t)
	cfg := &config.Config{
		ABACBundleEnabled:    true,
		ABACBundlePublicKeys: []string{"k1:" + base64.StdEncoding.EncodeToString(pub)},
		ABACBundleCachePath:  filepath.Join(t.TempDir(), "bundle.json"),
		ABACDeviceMode:       mode,
	}
	if mutate != nil {
		mutate(cfg)
	}
	client := NewABACBundleClient(cfg, "agent-1", "secret", "http://127.0.0.1:1", nopABACBundleLogger{})
	client.now = func() time.Time { return deviceGuardTestNow }
	if policies != nil {
		sb := signLocalEvalBundle(t, priv, policies,
			deviceGuardTestNow.Add(-time.Minute), deviceGuardTestNow.Add(time.Hour))
		client.bundle = &sb
		client.source = abacBundleSourceNetwork
	}
	reporter := NewABACDecisionReporter(cfg, "agent-1", "secret", "http://127.0.0.1:1", nopABACBundleLogger{})
	reporter.now = func() time.Time { return deviceGuardTestNow }
	reporter.SetBundleIDProvider(func() string {
		sb, _ := client.Current()
		if sb == nil {
			return ""
		}
		return sb.Bundle.BundleID
	})
	guard, err := NewDeviceGuard(cfg, client, reporter, deviceGuardTestProvider, nopABACBundleLogger{})
	if err != nil {
		t.Fatalf("NewDeviceGuard: %v", err)
	}
	return guard, reporter, client
}

// newExpiredDeviceGuardFixture 构造一个 LKG 已过期的 guard（策略本身仍是 allow）。
func newExpiredDeviceGuardFixture(t *testing.T, mode string) (*DeviceGuard, *ABACDecisionReporter, *ABACBundleClient) {
	t.Helper()
	pub, priv := newLocalEvalKey(t)
	cfg := &config.Config{
		ABACBundleEnabled:    true,
		ABACBundlePublicKeys: []string{"k1:" + base64.StdEncoding.EncodeToString(pub)},
		ABACBundleCachePath:  filepath.Join(t.TempDir(), "bundle.json"),
		ABACDeviceMode:       mode,
	}
	client := NewABACBundleClient(cfg, "agent-1", "secret", "http://127.0.0.1:1", nopABACBundleLogger{})
	client.now = func() time.Time { return deviceGuardTestNow }
	sb := signLocalEvalBundle(t, priv, []abacbundle.Policy{
		{ID: "p-allow", Subject: "*", Resource: "*", Action: "*", Effect: "allow"},
	}, deviceGuardTestNow.Add(-2*time.Hour), deviceGuardTestNow.Add(-time.Hour))
	client.bundle = &sb
	client.source = abacBundleSourceCache
	reporter := NewABACDecisionReporter(cfg, "agent-1", "secret", "http://127.0.0.1:1", nopABACBundleLogger{})
	reporter.now = func() time.Time { return deviceGuardTestNow }
	reporter.SetBundleIDProvider(func() string {
		sb, _ := client.Current()
		if sb == nil {
			return ""
		}
		return sb.Bundle.BundleID
	})
	guard, err := NewDeviceGuard(cfg, client, reporter, deviceGuardTestProvider, nopABACBundleLogger{})
	if err != nil {
		t.Fatalf("NewDeviceGuard: %v", err)
	}
	return guard, reporter, client
}

func allowTaskPolicy() []abacbundle.Policy {
	return []abacbundle.Policy{
		{ID: "p-allow-task", Subject: "agent-1", Resource: "dev-1", Action: "task:execute", Effect: "allow"},
	}
}

func denyTaskPolicy() []abacbundle.Policy {
	return []abacbundle.Policy{
		{ID: "p-deny-task", Subject: "agent-1", Resource: "dev-1", Action: "task:execute", Effect: "deny"},
	}
}

func taskResource() map[string]any {
	info := deviceGuardTestProvider()
	return map[string]any{
		"deviceID": info.DeviceID,
		"segment":  info.Segment,
		"os":       info.OS,
		"arch":     info.Arch,
		"hostname": info.Hostname,
		"taskID":   "task-1",
		"taskType": proto.TaskTypeShell,
		"agent_id": info.AgentID,
	}
}

// TestDeviceGuard_OffBypassesWithoutEvaluation：off 模式必须零评估、零审计。
func TestDeviceGuard_OffBypassesWithoutEvaluation(t *testing.T) {
	guard, reporter, _ := newDeviceGuardFixture(t, config.ABACDeviceModeOff, denyTaskPolicy(), nil)

	got := guard.Decide("task:execute", taskResource())
	if !got.Allowed || got.Enforced || got.Marker != DeviceMarkerBypass {
		t.Fatalf("off 模式应 bypass，得到 %+v", got)
	}
	if n := reporter.Stats().Buffered; n != 0 {
		t.Fatalf("off 模式不得写 M3 审计，buffered=%d", n)
	}
}

// TestDeviceGuard_KillSwitchBypassesEnforce：kill switch 在 enforce 下也必须 bypass。
func TestDeviceGuard_KillSwitchBypassesEnforce(t *testing.T) {
	guard, reporter, _ := newDeviceGuardFixture(t, config.ABACDeviceModeEnforce, denyTaskPolicy(), func(c *config.Config) {
		c.ABACDeviceKillSwitch = true
	})

	got := guard.Decide("task:execute", taskResource())
	if !got.Allowed || got.Enforced || got.Marker != DeviceMarkerBypass {
		t.Fatalf("kill switch 应 bypass，得到 %+v", got)
	}
	if !strings.Contains(got.Reason, "kill switch") {
		t.Fatalf("bypass reason 应说明 kill switch，得到 %q", got.Reason)
	}
	if n := reporter.Stats().Buffered; n != 0 {
		t.Fatalf("bypass 不得写 M3 审计，buffered=%d", n)
	}
}

// TestDeviceGuard_DisabledDeviceBypasses：disabled-ids 命中的设备始终 bypass。
func TestDeviceGuard_DisabledDeviceBypasses(t *testing.T) {
	guard, reporter, _ := newDeviceGuardFixture(t, config.ABACDeviceModeEnforce, denyTaskPolicy(), func(c *config.Config) {
		c.ABACDeviceDisabledIDs = "other-dev, dev-1"
	})

	got := guard.Decide("task:execute", taskResource())
	if !got.Allowed || got.Enforced || got.Marker != DeviceMarkerBypass {
		t.Fatalf("disabled 设备应 bypass，得到 %+v", got)
	}
	if n := reporter.Stats().Buffered; n != 0 {
		t.Fatalf("bypass 不得写 M3 审计，buffered=%d", n)
	}
}

// TestDeviceGuard_SelectorMismatchBypasses：选择器不命中 ⇒ bypass；命中 ⇒ 进入评估。
func TestDeviceGuard_SelectorMismatchBypasses(t *testing.T) {
	guard, reporter, _ := newDeviceGuardFixture(t, config.ABACDeviceModeEnforce, denyTaskPolicy(), func(c *config.Config) {
		c.ABACDeviceSelector = "segment=prod"
	})
	got := guard.Decide("task:execute", taskResource())
	if !got.Allowed || got.Marker != DeviceMarkerBypass {
		t.Fatalf("selector 不命中应 bypass，得到 %+v", got)
	}
	if n := reporter.Stats().Buffered; n != 0 {
		t.Fatalf("selector 不命中不得写 M3 审计，buffered=%d", n)
	}
}

// TestDeviceGuard_SelectorMatchEvaluates：全部字段命中时正常评估。
func TestDeviceGuard_SelectorMatchEvaluates(t *testing.T) {
	guard, reporter, _ := newDeviceGuardFixture(t, config.ABACDeviceModeEnforce, allowTaskPolicy(), func(c *config.Config) {
		c.ABACDeviceSelector = "segment=seg-a, os=linux, arch=amd64, hostname=host-1, agent_id=agent-1"
	})
	got := guard.Decide("task:execute", taskResource())
	if !got.Allowed || !got.Enforced || got.Marker != DeviceMarkerDevice {
		t.Fatalf("selector 命中应评估并按 allow 放行，得到 %+v", got)
	}
	if got.PolicyID != "p-allow-task" {
		t.Fatalf("PolicyID = %q，want p-allow-task", got.PolicyID)
	}
	if n := reporter.Stats().Buffered; n != 1 {
		t.Fatalf("评估后应写 1 条 M3 审计，buffered=%d", n)
	}
}

// TestDeviceGuard_ShadowDenyStillAllows：shadow 下 deny 决策仍放行，并记录 would-be 决策。
func TestDeviceGuard_ShadowDenyStillAllows(t *testing.T) {
	guard, reporter, _ := newDeviceGuardFixture(t, config.ABACDeviceModeShadow, denyTaskPolicy(), nil)

	got := guard.Decide("task:execute", taskResource())
	if !got.Allowed || got.Enforced || got.Marker != DeviceMarkerShadow {
		t.Fatalf("shadow 必须放行且不强制，得到 %+v", got)
	}
	if got.PolicyID != "p-deny-task" {
		t.Fatalf("shadow 应保留 would-be PolicyID，得到 %q", got.PolicyID)
	}
	reporter.mu.Lock()
	entries := reporter.snapshotLocked(10)
	reporter.mu.Unlock()
	if len(entries) != 1 {
		t.Fatalf("shadow 应写 1 条 M3 审计，得到 %d", len(entries))
	}
	rec := entries[0].rec
	if rec.Allowed || rec.RequestID != "task-1" || rec.PolicyID != "p-deny-task" {
		t.Fatalf("shadow 审计应为 would-be deny，得到 %+v", rec)
	}
	if !strings.HasPrefix(rec.Reason, DeviceMarkerShadow+": ") {
		t.Fatalf("shadow 审计 reason 应带 marker 前缀，得到 %q", rec.Reason)
	}
}

// TestDeviceGuard_EnforceDeny：enforce 下 deny 必须阻断。
func TestDeviceGuard_EnforceDeny(t *testing.T) {
	guard, reporter, _ := newDeviceGuardFixture(t, config.ABACDeviceModeEnforce, denyTaskPolicy(), nil)

	got := guard.Decide("task:execute", taskResource())
	if got.Allowed || !got.Enforced || got.Marker != DeviceMarkerDevice {
		t.Fatalf("enforce deny 必须阻断，得到 %+v", got)
	}
	if got.PolicyID != "p-deny-task" || got.Reason != "denied by policy p-deny-task" {
		t.Fatalf("deny reason/policy 不符，得到 %+v", got)
	}
	reporter.mu.Lock()
	entries := reporter.snapshotLocked(10)
	reporter.mu.Unlock()
	if len(entries) != 1 {
		t.Fatalf("enforce deny 应写 1 条 M3 审计，得到 %d", len(entries))
	}
	if rec := entries[0].rec; rec.BundleID != "b-local-eval" || rec.Allowed || rec.RequestID != "task-1" {
		t.Fatalf("M3 审计应带 bundleId/拒绝决策/任务 ID，得到 %+v", rec)
	}
}

// TestDeviceGuard_MissingBundleFailClosedMutatingOpenRead：无 bundle 时
// mutating fail-closed、read fail-open。
func TestDeviceGuard_MissingBundleFailClosedMutatingOpenRead(t *testing.T) {
	guard, reporter, _ := newDeviceGuardFixture(t, config.ABACDeviceModeEnforce, nil, nil)

	mut := guard.Decide("task:execute", taskResource())
	if mut.Allowed || !mut.Enforced || mut.Marker != DeviceMarkerFailClosed {
		t.Fatalf("无 bundle 的 mutating 动作应 fail-closed，得到 %+v", mut)
	}
	read := guard.Decide("read", taskResource())
	if !read.Allowed || read.Enforced || read.Marker != DeviceMarkerFailOpen {
		t.Fatalf("无 bundle 的 read 动作应 fail-open，得到 %+v", read)
	}
	reporter.mu.Lock()
	entries := reporter.snapshotLocked(10)
	reporter.mu.Unlock()
	if len(entries) != 2 {
		t.Fatalf("两条离线决策都应写审计，得到 %d", len(entries))
	}
	for _, e := range entries {
		if e.rec.Mode != abacDecisionModeDeviceOffline {
			t.Fatalf("离线决策 mode 应为 device-offline，得到 %q", e.rec.Mode)
		}
	}
}

// TestDeviceGuard_ExpiredBundleFailClosedMutatingOpenRead：过期 bundle 时
// mutating fail-closed、read fail-open，marker 明确区分。
func TestDeviceGuard_ExpiredBundleFailClosedMutatingOpenRead(t *testing.T) {
	guard, reporter, _ := newExpiredDeviceGuardFixture(t, config.ABACDeviceModeEnforce)

	mut := guard.Decide("task:execute", taskResource())
	if mut.Allowed || !mut.Enforced || mut.Marker != DeviceMarkerFailClosed {
		t.Fatalf("过期 bundle 的 mutating 动作应 fail-closed，得到 %+v", mut)
	}
	if !strings.Contains(mut.Reason, "expired") {
		t.Fatalf("fail-closed reason 应说明过期，得到 %q", mut.Reason)
	}
	read := guard.Decide("read", taskResource())
	if !read.Allowed || read.Enforced || read.Marker != DeviceMarkerFailOpen {
		t.Fatalf("过期 bundle 的 read 动作应 fail-open，得到 %+v", read)
	}
	if n := reporter.Stats().Buffered; n != 2 {
		t.Fatalf("过期决策应写 2 条审计，buffered=%d", n)
	}
}

// TestDeviceGuard_UnknownSelectorFieldRejected：构造期拒绝未知选择器字段。
func TestDeviceGuard_UnknownSelectorFieldRejected(t *testing.T) {
	cfg := &config.Config{ABACDeviceMode: config.ABACDeviceModeShadow, ABACDeviceSelector: "label=prod"}
	if _, err := NewDeviceGuard(cfg, nil, nil, deviceGuardTestProvider, nopABACBundleLogger{}); err == nil {
		t.Fatal("未知 selector 字段必须被 NewDeviceGuard 拒绝")
	}
}

// capturingTaskClient 捕获 ReportResult 请求（验证 allow 执行 / deny 跳过）。
type capturingTaskClient struct {
	*StubTaskServiceClient
	results chan *taskv1.ReportResultRequest
}

func newCapturingTaskClient() *capturingTaskClient {
	return &capturingTaskClient{
		StubTaskServiceClient: NewStubTaskServiceClient(),
		results:               make(chan *taskv1.ReportResultRequest, 4),
	}
}

func (c *capturingTaskClient) ReportResult(ctx context.Context, req *taskv1.ReportResultRequest) (*taskv1.TaskResult, error) {
	c.results <- req
	return nil, nil
}

// newGuardedTestAgent 构造仅含执行闸门依赖的最小 Agent。
func newGuardedTestAgent(t *testing.T, guard *DeviceGuard) (*Agent, *capturingTaskClient) {
	t.Helper()
	client := newCapturingTaskClient()
	return &Agent{
		agentID:           "agent-1",
		hostname:          "host-1",
		cfg:               &config.Config{Segment: "seg-a"},
		taskTimeout:       5 * time.Second,
		workers:           1,
		taskCh:            make(chan proto.Task, 2),
		running:           make(map[string]*runState),
		taskServiceClient: client,
		abacDeviceGuard:   guard,
	}, client
}

// runOneGuardedTask 启动 worker、投递一个任务并等待结果。
func runOneGuardedTask(t *testing.T, a *Agent, client *capturingTaskClient, task proto.Task) *taskv1.ReportResultRequest {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		a.worker(ctx)
		close(done)
	}()
	a.taskCh <- task
	select {
	case req := <-client.results:
		return req
	case <-ctx.Done():
		t.Fatal("等待任务结果超时")
		return nil
	}
}

// TestAgentWorker_ABACDeviceGate：allow 时任务正常执行；deny 时跳过执行并上报失败。
func TestAgentWorker_ABACDeviceGate(t *testing.T) {
	t.Run("allowed executes", func(t *testing.T) {
		guard, _, _ := newDeviceGuardFixture(t, config.ABACDeviceModeEnforce, allowTaskPolicy(), nil)
		a, client := newGuardedTestAgent(t, guard)
		out := filepath.Join(t.TempDir(), "allowed.txt")
		res := runOneGuardedTask(t, a, client, proto.Task{
			TaskID:  "task-allow",
			Type:    proto.TaskTypeShell,
			Command: "echo ok>" + out,
			AgentID: "dev-1",
		})
		if res.GetResult().GetExitCode() != 0 {
			t.Fatalf("allow 任务应执行成功，得到 exit=%d stderr=%q",
				res.GetResult().GetExitCode(), res.GetResult().GetStderr())
		}
		if _, err := os.Stat(out); err != nil {
			t.Fatalf("allow 任务应实际执行（输出文件缺失）: %v", err)
		}
	})

	t.Run("denied skips and fails", func(t *testing.T) {
		guard, _, _ := newDeviceGuardFixture(t, config.ABACDeviceModeEnforce, denyTaskPolicy(), nil)
		a, client := newGuardedTestAgent(t, guard)
		out := filepath.Join(t.TempDir(), "denied.txt")
		res := runOneGuardedTask(t, a, client, proto.Task{
			TaskID:  "task-deny",
			Type:    proto.TaskTypeShell,
			Command: "echo no>" + out,
			AgentID: "dev-1",
		})
		if res.GetResult().GetExitCode() != -1 {
			t.Fatalf("deny 任务应上报失败，得到 exit=%d", res.GetResult().GetExitCode())
		}
		if !strings.HasPrefix(res.GetResult().GetStderr(), "abac denied: ") {
			t.Fatalf("失败原因应以 abac denied: 开头，得到 %q", res.GetResult().GetStderr())
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Fatalf("deny 任务不得执行（输出文件存在或 stat 异常: %v）", err)
		}
	})
}
