package controlplane

// ui_channel_delivery_test.go — TD-92 第二批守卫：UI 通知渠道（notify-channels 表）
// 真正参与自动外发。
//
// 锁住六件事：
//  1. enabled 的同租户渠道经 deliverAlert 真实送达（httptest 收件端实证，不再只有
//     测试按钮能消费这张表）；
//  2. 租户隔离：A 租户的渠道收不到 B 租户的告警；
//  3. disabled 渠道被跳过；
//  4. 按规则选渠道：规则显式配 NotifyChannels 时只送列表内；未配不过滤；
//  5. notifyAlertGroup 落库时把选择意图登记到 alertChannelSel（引擎规则→AlertID）；
//  6. 明文存量启动扫描谓词 plaintextNotifyChannelKeys 的边界
//     （明文/引用/坏JSON/disabled 分别该怎么记）。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/alertengine"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// uiChannelDef 定义一个 UI 渠道测试夹具（每个渠道一个假收件端）。
type uiChannelDef struct {
	ID      string
	Tenant  string
	Enabled bool
}

// uiChannelHarness 一套"UI 渠道 + 假收件端"的测试装置。
// egress 策略注入与 NewServer 对齐（allowLoopbackEgress：httptest 恒绑 127.0.0.1，
// 不声明就变成"想验接线、实际被投递策略拒"），策略本身的正反例在 notify 包内覆盖。
type uiChannelHarness struct {
	s    *Server
	hits map[string]*int32
}

func newUIChannelHarness(t *testing.T, defs ...uiChannelDef) *uiChannelHarness {
	t.Helper()
	allowLoopbackEgress(t)
	st := store.NewMemoryStore()
	h := &uiChannelHarness{hits: map[string]*int32{}}
	for _, d := range defs {
		var hits int32
		backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt32(&hits, 1)
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(backend.Close)
		h.hits[d.ID] = &hits
		cfgJSON, _ := json.Marshal(map[string]string{"webhookURL": backend.URL + "/hook"})
		st.CreateNotifyChannel(&store.NotifyChannel{
			ID: d.ID, TenantID: d.Tenant, Name: d.ID, Type: "dingtalk",
			Config: string(cfgJSON), Enabled: d.Enabled,
		})
	}
	h.s = &Server{store: st}
	return h
}

func (h *uiChannelHarness) hitCount(channelID string) int {
	c, ok := h.hits[channelID]
	if !ok {
		return 0
	}
	return int(atomic.LoadInt32(c))
}

// TestDeliverAlert_StoreChannelReceives UI 渠道经 deliverAlert 真实收到告警
// （TD-92 前它只有一个消费方：测试按钮）。
func TestDeliverAlert_StoreChannelReceives(t *testing.T) {
	h := newUIChannelHarness(t, uiChannelDef{ID: "ch-1", Tenant: "default", Enabled: true})
	a := &proto.Alert{AlertID: "al-1", TenantID: "default", DeviceID: "d1", Severity: "critical", Message: "cpu high"}
	if err := h.s.deliverAlert(a); err != nil {
		t.Fatalf("deliverAlert: %v", err)
	}
	if h.hitCount("ch-1") != 1 {
		t.Fatalf("UI 渠道应收到 1 条，实得 %d", h.hitCount("ch-1"))
	}
}

// TestStoreChannelsFor_TenantIsolation 租户隔离：t1 的渠道不收 t2 的告警，
// 反之亦然（多租户下 A 的 webhook 不能收到 B 的告警内容）。
func TestStoreChannelsFor_TenantIsolation(t *testing.T) {
	h := newUIChannelHarness(t,
		uiChannelDef{ID: "ch-t1", Tenant: "t1", Enabled: true},
		uiChannelDef{ID: "ch-default", Tenant: "default", Enabled: true},
	)

	// t2 的告警：两个渠道都不该选中。
	if got := h.s.storeChannelsFor(&proto.Alert{AlertID: "a1", TenantID: "t2"}); len(got) != 0 {
		t.Errorf("t2 告警不应选中任何渠道，实得 %d", len(got))
	}
	// default 的告警：只选中 default 那个（t1 的收件端不应被打到）。
	if got := h.s.storeChannelsFor(&proto.Alert{AlertID: "a2", TenantID: "default"}); len(got) != 1 {
		t.Fatalf("default 告警应选中 1 个渠道，实得 %d", len(got))
	}
	if err := h.s.deliverAlert(&proto.Alert{AlertID: "a2", TenantID: "default", Message: "m"}); err != nil {
		t.Fatalf("deliverAlert: %v", err)
	}
	if h.hitCount("ch-t1") != 0 {
		t.Errorf("t1 的收件端不该收到 default 告警，实得 %d 次", h.hitCount("ch-t1"))
	}
	if h.hitCount("ch-default") != 1 {
		t.Errorf("default 收件端应收到 1 次，实得 %d", h.hitCount("ch-default"))
	}
	// 空租户（存量告警）归一为 default。
	if got := h.s.storeChannelsFor(&proto.Alert{AlertID: "a3", TenantID: ""}); len(got) != 1 {
		t.Errorf("空租户告警应按 default 匹配，实得 %d", len(got))
	}
}

// TestStoreChannelsFor_SkipsDisabled disabled 渠道不参与自动外发。
func TestStoreChannelsFor_SkipsDisabled(t *testing.T) {
	h := newUIChannelHarness(t, uiChannelDef{ID: "ch-off", Tenant: "default", Enabled: false})
	if got := h.s.storeChannelsFor(&proto.Alert{AlertID: "a1", TenantID: "default"}); len(got) != 0 {
		t.Fatalf("disabled 渠道不应被选中，实得 %d", len(got))
	}
}

// TestStoreChannelsFor_PerRuleSelection 按规则选渠道：登记了选择意图就只送列表内；
// 没登记不过滤（设备/任务类告警与未配选择的规则送全部 enabled 渠道）。
func TestStoreChannelsFor_PerRuleSelection(t *testing.T) {
	h := newUIChannelHarness(t,
		uiChannelDef{ID: "ch-1", Tenant: "default", Enabled: true},
		uiChannelDef{ID: "ch-2", Tenant: "default", Enabled: true},
	)

	base := &proto.Alert{AlertID: "al-x", TenantID: "default", Message: "m"}
	if got := h.s.storeChannelsFor(base); len(got) != 2 {
		t.Fatalf("无选择意图应送全部 enabled 渠道，实得 %d", len(got))
	}
	h.s.noteAlertChannelSelection("al-x", []string{"ch-2"})
	if got := h.s.storeChannelsFor(base); len(got) != 1 {
		t.Fatalf("登记后应只送 ch-2，实得 %d", len(got))
	}
	// 送达也只打 ch-2：ch-1 的假收件端 0 次。
	if err := h.s.deliverAlert(base); err != nil {
		t.Fatalf("deliverAlert: %v", err)
	}
	if h.hitCount("ch-1") != 0 {
		t.Errorf("按规则只选 ch-2 时 ch-1 收件端不应收到，实得 %d", h.hitCount("ch-1"))
	}
	if h.hitCount("ch-2") != 1 {
		t.Errorf("ch-2 应收到 1 次，实得 %d", h.hitCount("ch-2"))
	}
}

// TestNotifyAlertGroup_RegistersRuleChannelSelection wiring 判据：notifyAlertGroup
// 落库时把引擎规则的 NotifyChannels 登记到 alertChannelSel（引擎规则→AlertID），
// 未配 NotifyChannels 的规则不登记（= 不过滤）。
func TestNotifyAlertGroup_RegistersRuleChannelSelection(t *testing.T) {
	s := newAlertsTestServer()
	s.alertEngine = alertengine.NewEngine(nil, nil, nil)
	if err := s.alertEngine.AddRule(&alertengine.AlertRule{
		ID: "r-sel", TenantID: "t1", Enabled: true,
		Conditions: []alertengine.Condition{{Metric: "cpu_usage", Operator: ">", Threshold: 80}},
		Logic:      alertengine.LogicAnd, Severity: "critical",
		NotifyChannels: []string{"ch-a"},
	}); err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	g := &alertengine.AlertGroup{
		Key: "deviceID=d1|severity=critical",
		Events: []*alertengine.AlertEvent{
			{RuleID: "r-sel", TenantID: "t1", DeviceID: "d1", Severity: "critical", Message: "m",
				Labels: map[string]string{"ruleID": "r-sel", "deviceID": "d1", "severity": "critical"}},
		},
	}
	s.notifyAlertGroup(context.Background(), g)

	alerts := s.store.Alerts("t1")
	if len(alerts) != 1 {
		t.Fatalf("应落库 1 条告警，实得 %d", len(alerts))
	}
	ids, ok := s.alertChannelSelection(alerts[0].AlertID)
	if !ok || len(ids) != 1 || ids[0] != "ch-a" {
		t.Fatalf("选择意图应登记为 [ch-a]，实得 %v (ok=%v)", ids, ok)
	}
}

// TestPruneAlertChannelSel 意图登记按时间戳清理：24h 内的留着（推送重试窗口内
// 仍需过滤），超期的清掉（防无限增长）。这条钉住"别改成跟着 alertSent 在不在"——
// 推送失败重试期间 alertSent 没有该告警，但意图必须还在。
func TestPruneAlertChannelSel(t *testing.T) {
	s := &Server{}
	s.noteAlertChannelSelection("al-keep", []string{"c1"})
	s.pruneAlertChannelSel(time.Now().Add(-24 * time.Hour)) // 刚登记的，未超期
	if _, ok := s.alertChannelSelection("al-keep"); !ok {
		t.Error("24h 内的意图不应被清掉（重试窗口内还要用）")
	}
	s.pruneAlertChannelSel(time.Now().Add(time.Second)) // 全部超期
	if _, ok := s.alertChannelSelection("al-keep"); ok {
		t.Error("超期意图应被清掉（有界防泄漏）")
	}
}

// fakeNotifyChannelLister 明文扫描谓词的替身（同 secretLister 先例）。
type fakeNotifyChannelLister struct{ channels []*store.NotifyChannel }

func (f fakeNotifyChannelLister) ListNotifyChannels(string) []*store.NotifyChannel { return f.channels }

// TestPlaintextNotifyChannelKeys 启动扫描谓词：只列含明文敏感字段的渠道，
// 坏 JSON 单列、disabled 也扫（存量就是存量，启用与否都要知道）。
func TestPlaintextNotifyChannelKeys(t *testing.T) {
	lister := fakeNotifyChannelLister{channels: []*store.NotifyChannel{
		{ID: "c-plain", TenantID: "default", Type: "dingtalk", Config: `{"webhookURL":"https://oapi.dingtalk.com/robot/send?access_token=xxx"}`},
		{ID: "c-ref", TenantID: "default", Type: "dingtalk", Config: `{"webhookURL":"${vault:dt/webhook}"}`},
		{ID: "c-bad", TenantID: "", Type: "dingtalk", Config: `{not json`},
		{ID: "c-off", TenantID: "t2", Type: "dingtalk", Enabled: false, Config: `{"webhookURL":"https://x.invalid/hook"}`},
	}}
	got := plaintextNotifyChannelKeys(lister)
	want := []string{
		"default/c-bad(config-unparsable)",
		"default/c-plain(webhookURL)",
		"t2/c-off(webhookURL)",
	}
	if len(got) != len(want) {
		t.Fatalf("plaintext keys = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("plaintext keys[%d] = %q, want %q (full=%v)", i, got[i], want[i], got)
		}
	}
}

// TestBuildChannel_NoWarningButConstructs buildChannel 改为纯构造后仍能构造渠道
// （明文告警挪到写入期/启动期，发送路径不再每次刷屏）。
func TestBuildChannel_NoWarningButConstructs(t *testing.T) {
	ch, err := buildChannel(&store.NotifyChannel{
		ID: "c1", TenantID: "default", Type: "dingtalk",
		Config: `{"webhookURL":"https://oapi.dingtalk.com/robot/send?access_token=x"}`,
	}, nil)
	if err != nil || ch == nil {
		t.Fatalf("buildChannel 应能构造 dingtalk 渠道，实得 ch=%v err=%v", ch, err)
	}
}

// TestWarnPlaintextChannel_Smoke 写入期告警不 panic、不改变返回值语义（纯日志）。
func TestWarnPlaintextChannel_Smoke(t *testing.T) {
	warnPlaintextChannel(&store.NotifyChannel{
		ID: "c1", TenantID: "default", Type: "dingtalk",
		Config: `{"webhookURL":"https://oapi.dingtalk.com/robot/send?access_token=x"}`,
	}, nil)
	warnPlaintextChannel(nil, nil)
	warnPlaintextChannel(&store.NotifyChannel{ID: "c2", Type: "dingtalk", Config: "{bad"}, nil)
}
