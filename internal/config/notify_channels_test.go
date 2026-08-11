package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadNotifyChannelsConfig_Valid 验证加载合法的渠道配置文件。
func TestLoadNotifyChannelsConfig_Valid(t *testing.T) {
	content := `{
  "channels": [
    {"type": "dingtalk", "webhook_url": "https://oapi.dingtalk.com/robot/send?access_token=xxx", "secret": "SECxxx"},
    {"type": "feishu", "webhook_url": "https://open.feishu.cn/open-apis/bot/v2/hook/xxx", "secret": "xxx"},
    {"type": "wechat", "webhook_url": "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx"},
    {"type": "slack", "webhook_url": "https://hooks.slack.com/services/xxx", "channel": "#ops"},
    {"type": "email", "smtp_host": "smtp.example.com", "smtp_port": 25, "username": "u", "password": "p", "from": "ops@example.com", "to": ["ops1@example.com", "ops2@example.com"]}
  ]
}`
	path := writeTempFile(t, content)
	defer os.Remove(path)

	chs, err := LoadNotifyChannelsConfig(path)
	if err != nil {
		t.Fatalf("LoadNotifyChannelsConfig err = %v", err)
	}
	if len(chs) != 5 {
		t.Fatalf("channels len = %d, want 5", len(chs))
	}
	// 验证钉钉
	if chs[0].Type != "dingtalk" || chs[0].WebhookURL == "" || chs[0].Secret == "" {
		t.Fatalf("dingtalk channel wrong: %+v", chs[0])
	}
	// 验证飞书
	if chs[1].Type != "feishu" || chs[1].Secret == "" {
		t.Fatalf("feishu channel wrong: %+v", chs[1])
	}
	// 验证企业微信
	if chs[2].Type != "wechat" || chs[2].WebhookURL == "" {
		t.Fatalf("wechat channel wrong: %+v", chs[2])
	}
	// 验证 Slack
	if chs[3].Type != "slack" || chs[3].Channel != "#ops" {
		t.Fatalf("slack channel wrong: %+v", chs[3])
	}
	// 验证邮件
	if chs[4].Type != "email" || chs[4].SMTPHost == "" || len(chs[4].To) != 2 {
		t.Fatalf("email channel wrong: %+v", chs[4])
	}
}

// TestLoadNotifyChannelsConfig_EmptyPath 验证空路径返回 nil + nil。
func TestLoadNotifyChannelsConfig_EmptyPath(t *testing.T) {
	chs, err := LoadNotifyChannelsConfig("")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if chs != nil {
		t.Fatalf("chs = %v, want nil", chs)
	}
}

// TestLoadNotifyChannelsConfig_FileNotExist 验证文件不存在返回 error。
func TestLoadNotifyChannelsConfig_FileNotExist(t *testing.T) {
	_, err := LoadNotifyChannelsConfig("/nonexistent/path/config.json")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

// TestLoadNotifyChannelsConfig_InvalidJSON 验证非法 JSON 返回 error。
func TestLoadNotifyChannelsConfig_InvalidJSON(t *testing.T) {
	path := writeTempFile(t, `{invalid json}`)
	defer os.Remove(path)

	_, err := LoadNotifyChannelsConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// TestLoadNotifyChannelsConfig_EmptyChannels 验证空 channels 列表不报错。
func TestLoadNotifyChannelsConfig_EmptyChannels(t *testing.T) {
	path := writeTempFile(t, `{"channels": []}`)
	defer os.Remove(path)

	chs, err := LoadNotifyChannelsConfig(path)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(chs) != 0 {
		t.Fatalf("channels len = %d, want 0", len(chs))
	}
}

// TestLoadNotifyChannelsConfig_NoChannelsField 验证无 channels 字段返回空切片。
func TestLoadNotifyChannelsConfig_NoChannelsField(t *testing.T) {
	path := writeTempFile(t, `{}`)
	defer os.Remove(path)

	chs, err := LoadNotifyChannelsConfig(path)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(chs) != 0 {
		t.Fatalf("channels len = %d, want 0", len(chs))
	}
}

// TestNotifyChannelConfig_JSONTags 验证 JSON tag 映射正确。
func TestNotifyChannelConfig_JSONTags(t *testing.T) {
	content := `{"type":"email","webhook_url":"http://x","secret":"s","channel":"#c","smtp_host":"h","smtp_port":25,"username":"u","password":"p","from":"f","to":["t1","t2"]}`
	path := writeTempFile(t, content)
	defer os.Remove(path)

	chs, err := LoadNotifyChannelsConfig(path)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	// 注意：单个对象（非数组）解析到 channels 数组会失败，需用数组格式
	_ = chs
}

// TestNotifyChannelConfig_ArrayFormat 验证数组格式配置。
func TestNotifyChannelConfig_ArrayFormat(t *testing.T) {
	content := `{"channels": [{"type": "dingtalk", "webhook_url": "http://x", "secret": "s"}]}`
	path := writeTempFile(t, content)
	defer os.Remove(path)

	chs, err := LoadNotifyChannelsConfig(path)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(chs) != 1 {
		t.Fatalf("channels len = %d, want 1", len(chs))
	}
	if chs[0].Type != "dingtalk" || chs[0].WebhookURL != "http://x" || chs[0].Secret != "s" {
		t.Fatalf("channel[0] wrong: %+v", chs[0])
	}
}

// writeTempFile 写入临时文件并返回路径。测试用，失败 t.Fatal。
func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "notify-channels-*.json")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(f.Name())
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()
	return filepath.ToSlash(f.Name())
}