// notify_channels_plaintext_test.go 通知渠道「明文凭据识别」的判据用例（TD-88 方向裁定：引用格式优先）。
//
// 为什么单测这条谓词：告警文案与「哪些字段算敏感」会随渠道演进漂移（比如新增渠道忘了登记敏感字段
// ⇒ 明文静默通过），用表驱动钉住「计/不计」两侧，比看日志可靠。
package controlplane

import (
	"reflect"
	"testing"
)

func TestPlaintextSensitiveFields(t *testing.T) {
	cases := []struct {
		name    string
		typ     string
		cfg     map[string]string
		want    []string
		comment string
	}{
		{
			name: "dingtalk 明文 webhook+secret",
			typ:  "dingtalk",
			cfg:  map[string]string{"webhookURL": "https://oapi.dingtalk.com/robot/send?access_token=abc", "secret": "SECabc"},
			want: []string{"secret", "webhookURL"},
		},
		{
			name: "dingtalk 全部走引用 ⇒ 无明文",
			typ:  "dingtalk",
			cfg:  map[string]string{"webhookURL": "${vault:notify/dingtalk#url}", "secret": "${notify/dingtalk#secret}"},
			want: nil,
		},
		{
			name: "wecom 只有 webhookURL（无 secret 字段）",
			typ:  "wecom",
			cfg:  map[string]string{"webhookURL": "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=k"},
			want: []string{"webhookURL"},
		},
		{
			name: "feishu 明文 secret + 引用 url ⇒ 只报 secret",
			typ:  "feishu",
			cfg:  map[string]string{"webhookURL": "${feishu/url}", "secret": "plain"},
			want: []string{"secret"},
		},
		{
			name: "email 只把 pass 视为敏感（host/user/to 不算）",
			typ:  "email",
			cfg:  map[string]string{"host": "smtp.example.com", "user": "ops@example.com", "pass": "plainpw", "to": "a@b.c"},
			want: []string{"pass"},
		},
		{
			name:    "slack 的 channel 是频道名，不算凭据",
			typ:     "slack",
			cfg:     map[string]string{"webhookURL": "${slack/url}", "channel": "#alerts"},
			want:    nil,
			comment: "channel 计为敏感会制造假告警（这正是抽成纯函数要钉住的边界）",
		},
		{
			name: "空值不算明文",
			typ:  "dingtalk",
			cfg:  map[string]string{"webhookURL": "", "secret": ""},
			want: nil,
		},
		{
			name: "未知渠道类型不报（保守：不猜它的字段语义）",
			typ:  "some-future-channel",
			cfg:  map[string]string{"token": "plain"},
			want: nil,
		},
	}
	for _, c := range cases {
		got := PlaintextSensitiveFields(c.typ, c.cfg)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: PlaintextSensitiveFields(%s) = %v, want %v %s", c.name, c.typ, got, c.want, c.comment)
		}
	}
}
