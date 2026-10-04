// egress_policy_test.go 钉住告警通知的出网策略（2026-10-04）。
//
// 背景：internal/notify 此前用裸 http.DefaultClient 出网，完全绕过 SSRF 防护，
// 且 --webhook-allow-private 开关对通知层无效。修复后通知层复用 internal/egress，
// 由 controlplane 在启动期按 cfg.WebhookAllowPrivate 注入。
//
// 本文件承担两件事：
//  1. TestMain：为**本包既有测试**注入放行私网的 client——它们用 httptest 起
//     环回（127.0.0.1）服务器，被安全基线按设计拒绝。逐个测试去改既有的
//     推送断言会污染测试意图（它们要验的是"推送内容对不对"），故统一在
//     TestMain 处理。
//  2. 专项用例：显式验证"默认拒绝内网""开关打开才放行"这两条策略语义。
package notify

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/egress"
)

// TestMain 为本包测试统一注入"允许私网"的出网 client。
//
// 为什么这样注入而不是改每个测试：既有测试用 httptest 起环回服务器来验证
// "推送的 JSON 对不对""签名算得对不对"，环回被拒是**正确行为**而非缺陷。
// 逐个改会把与本议题无关的测试也搅进来。
//
// 注意：这只影响测试进程；生产由 controlplane 按配置注入，且默认恒拒私网。
func TestMain(m *testing.M) {
	SetEgressClient(egress.NewClient(10*time.Second, true))
	// 不在 m.Run() 之后复位：那时所有测试已跑完，复位没有意义。
	// 各测试若自行改策略，必须用 t.Cleanup 复位（见 withEgressPolicy）。
	os.Exit(m.Run())
}

// withEgressPolicy 临时替换出网策略并在测试结束后复位。
func withEgressPolicy(t *testing.T, allowPrivate bool) {
	t.Helper()
	prev := EgressClient()
	SetEgressClient(egress.NewClient(10*time.Second, allowPrivate))
	t.Cleanup(func() { SetEgressClient(prev) })
}

// TestWebhook_RejectsLoopbackByDefault 是本轮修复的核心回归：
// 默认策略下，通知推送**不得**能打到内网/环回。
//
// 这条测试守护的是修复前的真实缺陷——http.DefaultClient 无任何 SSRF 校验，
// 通知可被指向 127.0.0.1 或 169.254.169.254。
func TestWebhook_RejectsLoopbackByDefault(t *testing.T) {
	withEgressPolicy(t, false) // 安全基线：恒拒私网

	cases := []struct {
		name string
		url  string
	}{
		{"环回", "http://127.0.0.1:9/hook"},
		{"环回 IPv6", "http://[::1]:9/hook"},
		{"私网 A", "http://10.0.0.1/hook"},
		{"私网 C", "http://192.168.1.1/hook"},
		{"云元数据", "http://169.254.169.254/latest/meta-data/"},
		{"未指定地址", "http://0.0.0.0/hook"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := postJSON(c.url, map[string]string{"probe": "1"})
			if err == nil {
				t.Fatalf("postJSON(%s) 成功返回——默认策略下内网地址必须被拒（修复前是裸 DefaultClient，无任何校验）", c.url)
			}
		})
	}
}

// TestWebhook_AllowPrivateStillRejectsMetadata 钉住"放行内网 ≠ 放行云元数据"。
//
// 这是 --webhook-allow-private=true 的收窄契约：它只放行私网与环回，
// 链路本地/云元数据/0.0.0.0/8 恒拒。若这条被放宽，内网开关等于给
// IMDS 凭证窃取开了后门。
func TestWebhook_AllowPrivateStillRejectsMetadata(t *testing.T) {
	withEgressPolicy(t, true)

	// 恒拒地址：即使 allowPrivate=true 也必须失败。
	for _, u := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://0.0.0.0/hook",
		"http://[fe80::1]/hook",
	} {
		if err := postJSON(u, map[string]string{"probe": "1"}); err == nil {
			t.Errorf("postJSON(%s) 成功返回——allowPrivate=true 也不得放行元数据/链路本地段", u)
		}
	}
	// 私网应放行（会因连接失败而报错，但不是"被 SSRF 策略拒绝"）。
	// 端口 9 (discard) 通常无人监听，故断言错误文案里不含 "egress policy"。
	err := postJSON("http://10.255.255.1:9/hook", map[string]string{"probe": "1"})
	if err != nil && strings.Contains(err.Error(), "egress policy") {
		t.Errorf("allowPrivate=true 时私网地址仍被 SSRF 策略拒绝：%v", err)
	}
}

// TestEgressClientWiring 钉住通知层出网 client 的接线。
//
// 这些是"看着对但实际没生效"的高发点：Timeout=0（无超时）、
// CheckRedirect=nil（默认跟随重定向，每跳都不复检）。
func TestEgressClientWiring(t *testing.T) {
	c := EgressClient()
	if c == nil {
		t.Fatal("EgressClient() 返回 nil：通知层将回落到 nil client 并 panic")
	}
	if c.Timeout == 0 {
		t.Error("Client.Timeout 为 0：连接黑洞时 goroutine 永久挂起，告警风暴会拖死通知通道")
	}
	if c.CheckRedirect == nil {
		t.Error("Client.CheckRedirect 为 nil：默认跟随最多 10 跳重定向，每跳都需复检 SSRF")
	}
}

// TestSetEgressClientNilResetsToSafeDefault 验证传 nil 会复位到"恒拒私网"默认，
// 而非留下一个不受控的 client。
func TestSetEgressClientNilResetsToSafeDefault(t *testing.T) {
	// 必须复位：本测试末尾会把 client 留在"恒拒私网"状态，
	// 而本包其他测试用环回 httptest 依赖"放行私网"。忘了复位就是
	// "一个测试污染后续全部测试"——本轮修复时正是踩了这个坑。
	prev := EgressClient()
	t.Cleanup(func() { SetEgressClient(prev) })

	SetEgressClient(egress.NewClient(time.Second, true))
	SetEgressClient(nil)
	if egress.AllowsPrivate(EgressClient()) {
		t.Error("SetEgressClient(nil) 后仍放行私网：复位默认值必须是安全基线")
	}
}
