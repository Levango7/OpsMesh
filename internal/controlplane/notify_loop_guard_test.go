// notify_loop_guard_test.go 钉住"M7 外发通道的 SSRF 准入必须读 --webhook-allow-private"。
//
// 缺陷形态（2026-10-03 读码 + 实测确认）：notifyLoop 调的是无开关的 validateURLSSRF，
// 而 Webhook CRUD / notify-channels 调的是开关版 ValidateWebhookURL。
// 于是部署者按文档开了"允许内网收件端"，钉钉/飞书内网网关仍然连不上——
// 而且控制面容器照样 healthy，只在日志里留一行 error。
package controlplane

import (
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
)

func TestAlertWebhookGuardHonorsAllowPrivateSwitch(t *testing.T) {
	cases := []struct {
		name         string
		url          string
		allowPrivate bool
		wantErr      bool
	}{
		{"内网服务名+开关关闭仍应拒绝", "http://alert-gateway.internal:9919/alert", false, true},
		{"环回+开关打开必须放行", "http://127.0.0.1:9919/alert", true, false},
		{"私网 IP+开关打开必须放行", "http://192.168.1.30:9919/alert", true, false},
		{"私网 IP+开关关闭仍应拒绝", "http://192.168.1.30:9919/alert", false, true},
		{"云元数据地址任何开关都拒绝", "http://169.254.169.254/latest/meta-data", true, true},
		{"非 http 协议任何开关都拒绝", "file:///etc/passwd", true, true},
	}
	for _, c := range cases {
		s := &Server{cfg: &config.Config{AlertWebhookURL: c.url, WebhookAllowPrivate: c.allowPrivate}}
		err := s.alertWebhookGuard()
		if (err != nil) != c.wantErr {
			t.Errorf("%s：url=%q allowPrivate=%v ⇒ err=%v，期望报错=%v", c.name, c.url, c.allowPrivate, err, c.wantErr)
		}
	}
}
