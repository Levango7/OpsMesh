// external_notify_failure_test.go — ack/resolve 的外部通知失败必须可见（缺陷 B 回归）。
//
// 覆盖四个组合：ack/resolve × 走熔断器/直调 notifier。每个组合断言三件事：
//  1. 本地状态确实变了、调用仍返回成功（远端失败不回滚 ack/resolve，这是既定意图）；
//  2. 失败被计入 business_metrics_total（counter 家族），值为 1——既证明"记下来了"，
//     也证明没被同时记进 gauge 家族或重复计数；
//  3. 日志里带真实错误文本且没有 !BADKEY。日志走标准库 log（cmd 侧 applog.Init 会把它
//     接管成 JSON），因此这里用 log.SetOutput 捕获——服务层不 import 根的 internal/logx。
package service

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/pkg/circuit"
	"github.com/Levango7/OpsMesh/pkg/metrics"
	alertv1 "github.com/Levango7/OpsMesh/services/alert-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/alert-svc/internal/store"
)

// failingNotifier 是让外部通知始终失败的 stub（模拟 PagerDuty 不可达/被拒绝）。
type failingNotifier struct {
	err      error
	ackCalls int
	resCalls int
}

func (f *failingNotifier) TriggerEvent(_, _, _, _ string, _ map[string]interface{}) error {
	return f.err
}

func (f *failingNotifier) AcknowledgeEvent(_, _, _ string, _ map[string]interface{}) error {
	f.ackCalls++
	return f.err
}

func (f *failingNotifier) ResolveEvent(_, _, _ string, _ map[string]interface{}) error {
	f.resCalls++
	return f.err
}

func (f *failingNotifier) IsEnabled() bool { return true }

// renderMetrics 抓取 /metrics 文本（与 device-svc 指标断言同一口径）。
func renderMetrics(t *testing.T) string {
	t.Helper()
	rec := httptest.NewRecorder()
	metrics.GetHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	return rec.Body.String()
}

// captureWarn 捕获一段代码写出的日志（标准库 log 的输出目标可换，无需管道/goroutine）。
func captureWarn(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer func() { log.SetOutput(os.Stderr) }()
	fn()
	return buf.String()
}

func TestAckResolveExternalNotifyFailureIsVisible(t *testing.T) {
	notifyErr := errors.New("pagerduty: 503 service unavailable")

	cases := []struct {
		name       string
		action     string
		useBreaker bool
		wantStatus string
	}{
		{"ack_with_breaker", "ack", true, "acknowledged"},
		{"ack_without_breaker", "ack", false, "acknowledged"},
		{"resolve_with_breaker", "resolve", true, "resolved"},
		{"resolve_without_breaker", "resolve", false, "resolved"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 每个子用例重建注册表：counter 从 0 起，"1" 才是真实的"只计了一次"。
			metrics.Init("alert-svc-test")
			n := &failingNotifier{err: notifyErr}
			svc := newTestService()
			st := store.NewMemoryStore()
			st.AddAlert(&store.Alert{AlertID: "alert-1", TenantID: "tenant-1", DeviceID: "device-1", Status: "firing"})
			svc.store = st
			svc.SetNotifier(n)
			if tc.useBreaker {
				svc.SetCircuitBreaker(circuit.New("notify-test", 5, time.Minute))
			}

			ctx := context.Background()
			var logs string
			var err error
			if tc.action == "ack" {
				logs = captureWarn(t, func() {
					err = svc.AckAlert(ctx, &alertv1.AckAlertRequest{Id: "alert-1"})
				})
			} else {
				logs = captureWarn(t, func() {
					err = svc.ResolveAlert(ctx, &alertv1.ResolveAlertRequest{Id: "alert-1"})
				})
			}

			// 1. 远端失败不得让本地 ack/resolve 失败，且状态必须已变更。
			if err != nil {
				t.Fatalf("%s 不应因外部通知失败而报错，got: %v", tc.action, err)
			}
			if got := st.Alert("alert-1").Status; got != tc.wantStatus {
				t.Fatalf("本地状态 = %q，期望 %q", got, tc.wantStatus)
			}
			if calls := n.ackCalls + n.resCalls; calls != 1 {
				t.Fatalf("通知调用次数 = %d，期望 1（stub 根本没被走到，断言会是空跑）", calls)
			}

			// 2. 失败计数落在 counter 家族。
			want := `business_metrics_total{name="alert_external_notify_failures",action="` + tc.action + `"} 1`
			body := renderMetrics(t)
			if !strings.Contains(body, want) {
				t.Errorf("缺少 counter %q\n---输出:\n%s", want, body)
			}
			// Add 语义用成 Set 的话会进 gauge 家族（值恒为 1，increase() 无意义）。
			gauge := `business_metrics{name="alert_external_notify_failures",action="` + tc.action + `"}`
			if strings.Contains(body, gauge) {
				t.Errorf("失败数被写进了 gauge 家族 %q", gauge)
			}

			// 3. 日志可见性：级别 + 错误文本 + 定位字段 + 无 !BADKEY。
			// 断言按标准库 log 的 key=value 形态写（cmd 侧 applog.Init 会把它接管成 JSON，
			// 届时同样带这些子串）——服务层不 import 根的 internal/logx，避免服务镜像
			// 跟着控制面的内部包一起变。
			if !strings.Contains(logs, "WARN") {
				t.Errorf("日志里没有 WARN 级记录（运维按级别 grep 会漏掉这条）\n---%s", logs)
			}
			if !strings.Contains(logs, notifyErr.Error()) {
				t.Errorf("日志里没有真实错误文本\n---%s", logs)
			}
			if strings.Contains(logs, "!BADKEY") {
				t.Errorf("日志出现 !BADKEY 这类无键输出\n---%s", logs)
			}
			if !strings.Contains(logs, "alertID=alert-1") || !strings.Contains(logs, "action="+tc.action) {
				t.Errorf("日志缺 alertID/action 字段（无法定位是哪条告警的哪种操作）\n---%s", logs)
			}
		})
	}
}

// TestAckResolveExternalNotifyFailureNotCountedOnSuccess 反向用例：通知成功时
// 失败计数器不得出现（否则"失败率"分母是假的）。
func TestAckResolveExternalNotifyFailureNotCountedOnSuccess(t *testing.T) {
	metrics.Init("alert-svc-test")
	svc := newTestService()
	st := store.NewMemoryStore()
	st.AddAlert(&store.Alert{AlertID: "alert-1", TenantID: "tenant-1", DeviceID: "device-1", Status: "firing"})
	svc.store = st
	svc.SetNotifier(&failingNotifier{err: nil})
	svc.SetCircuitBreaker(circuit.New("notify-test", 5, time.Minute))

	if err := svc.AckAlert(context.Background(), &alertv1.AckAlertRequest{Id: "alert-1"}); err != nil {
		t.Fatalf("AckAlert failed: %v", err)
	}
	if err := svc.ResolveAlert(context.Background(), &alertv1.ResolveAlertRequest{Id: "alert-1"}); err != nil {
		t.Fatalf("ResolveAlert failed: %v", err)
	}

	body := renderMetrics(t)
	if strings.Contains(body, "alert_external_notify_failures") {
		t.Errorf("通知成功却被记成失败\n---输出:\n%s", body)
	}
}

// TestNotifyFailureBaselineIsExportedAtStartup 钉住 #66：两条失败计数序列必须**在第一次失败之前**
// 就存在于 /metrics 上（值为 0）。
//
// 判据为什么是"序列先存在"而不是"值对不对"：Prometheus 的 increase() 取的是窗口内样本之差，
// 计数器若到失败时才惰性创建，第一个样本已经是 1，没有 0 可比 ⇒ increase 恒 0 ⇒
// 出厂规则 OpsMeshAlertExternalNotifyFailed 漏掉第一次失败。2026-10-04 真机实测过这一形态
// （只有一次 resolve 失败时 increase=0，规则不动；ack 到第二次失败才被算出 1.197）。
func TestNotifyFailureBaselineIsExportedAtStartup(t *testing.T) {
	metrics.Init("alert-svc-test")
	InitNotifyFailureBaseline()

	body := renderMetrics(t)
	for _, action := range []string{"ack", "resolve"} {
		want := `business_metrics_total{name="alert_external_notify_failures",action="` + action + `"} 0`
		if !strings.Contains(body, want) {
			t.Errorf("InitNotifyFailureBaseline 之后仍没有 %q\n---输出:\n%s", want, body)
		}
	}

	// 基线不得把"已经失败过"洗掉：Add 是在基线之上累加的。
	svc := newTestService()
	st := store.NewMemoryStore()
	st.AddAlert(&store.Alert{AlertID: "alert-base", TenantID: "tenant-1", DeviceID: "device-1", Status: "firing"})
	svc.store = st
	svc.SetNotifier(&failingNotifier{err: errors.New("pagerduty: 503")})
	svc.SetCircuitBreaker(circuit.New("notify-base", 5, time.Minute))
	if err := svc.AckAlert(context.Background(), &alertv1.AckAlertRequest{Id: "alert-base"}); err != nil {
		t.Fatalf("AckAlert: %v", err)
	}
	if got := `business_metrics_total{name="alert_external_notify_failures",action="ack"} 1`; !strings.Contains(renderMetrics(t), got) {
		t.Errorf("基线之后第一次失败没有累加到 1（找 %q）\n---输出:\n%s", got, renderMetrics(t))
	}
}

// TestMainWiresBaseline 钉住调用点：基线函数存在但没人调用 = 又是一条"配了却没接到线上"的死开关
// （PAGERDUTY_ENABLED 在 #58 之前就是这个形态，同一类缺陷不打算再犯第三次）。
// 这里按块取文本而不是反射：cmd 包不能 import 进 service 包测试，而"调用是否落在启用分支内"
// 恰恰是这段接线的语义所在。
func TestMainWiresBaseline(t *testing.T) {
	path := filepath.Join("..", "..", "cmd", "alert-svc", "main.go")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败（接线门禁没有代码可读时必须判红）: %v", path, err)
	}
	src := string(b)
	i := strings.Index(src, "if cfg.PagerDutyEnabled {")
	if i < 0 {
		t.Fatalf("main.go 里找不到 PagerDuty 启用分支（交付形态已变，本测试失去覆盖面）")
	}
	block := src[i:]
	if j := strings.Index(block, "\n\t}"); j >= 0 {
		block = block[:j]
	}
	if !strings.Contains(block, "InitNotifyFailureBaseline()") {
		t.Errorf("PagerDuty 启用分支内没有调用 InitNotifyFailureBaseline()（第一次失败将不被 increase() 看见）")
	}
}
