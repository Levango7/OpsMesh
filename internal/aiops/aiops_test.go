package aiops

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// mockLLM 是 LLM 接口的 mock 实现，用于单元测试。
type mockLLM struct {
	respContent string
	respErr     error
	callCount   int
	lastReq     ChatRequest
}

func (m *mockLLM) Chat(ctx context.Context, req ChatRequest) (*LLMResponse, error) {
	m.callCount++
	m.lastReq = req
	if m.respErr != nil {
		return nil, m.respErr
	}
	return &LLMResponse{Content: m.respContent}, nil
}

func TestConfigValidate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{name: "disabled", cfg: Config{Enabled: false}, wantErr: false},
		{name: "enabled without model", cfg: Config{Enabled: true}, wantErr: true},
		{name: "enabled without base_url", cfg: Config{Enabled: true, Model: "qwen"}, wantErr: true},
		{name: "enabled valid", cfg: Config{Enabled: true, Model: "qwen", BaseURL: "http://x", Timeout: 30}, wantErr: false},
		{name: "enabled timeout<=0", cfg: Config{Enabled: true, Model: "qwen", BaseURL: "http://x", Timeout: 0}, wantErr: false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.cfg.Validate()
			if (err != nil) != c.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	if c.Enabled {
		t.Errorf("DefaultConfig().Enabled should be false, got true")
	}
	if c.Provider != ProviderKilo {
		t.Errorf("DefaultConfig().Provider = %v, want %v", c.Provider, ProviderKilo)
	}
	if c.Model == "" {
		t.Errorf("DefaultConfig().Model should not be empty")
	}
	if c.Temperature <= 0 {
		t.Errorf("DefaultConfig().Temperature should be >0")
	}
	if c.MaxTokens <= 0 {
		t.Errorf("DefaultConfig().MaxTokens should be >0")
	}
}

func TestMockLLMChat(t *testing.T) {
	m := &mockLLM{respContent: `{"category":"软件","component":"nginx","description":"nginx 未启动","confidence":0.9,"evidence":["日志错误:连接被拒绝"]}`}

	resp, err := m.Chat(context.Background(), ChatRequest{Model: "qwen"})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content == "" {
		t.Errorf("Chat() Content should not be empty")
	}
	if m.callCount != 1 {
		t.Errorf("callCount = %d, want 1", m.callCount)
	}
}

func TestParseRootCause(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantCat  string
		wantComp string
		wantDesc string
		wantConf float64
	}{
		{
			name:     "valid json",
			content:  `{"category":"网络","component":"dns","description":"DNS 解析失败","confidence":0.95,"evidence":["ev1","ev2"]}`,
			wantCat:  "网络",
			wantComp: "dns",
			wantDesc: "DNS 解析失败",
			wantConf: 0.95,
		},
		{
			name:     "empty content",
			content:  "",
			wantCat:  "未知",
			wantComp: "未知",
			wantDesc: "",
			wantConf: 0.3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc, err := parseRootCause(tt.content)
			if err != nil {
				t.Fatalf("parseRootCause() error = %v", err)
			}
			if rc.Category != tt.wantCat {
				t.Errorf("Category = %q, want %q", rc.Category, tt.wantCat)
			}
			if rc.Component != tt.wantComp {
				t.Errorf("Component = %q, want %q", rc.Component, tt.wantComp)
			}
			if rc.Description != tt.wantDesc {
				t.Errorf("Description = %q, want %q", rc.Description, tt.wantDesc)
			}
			if rc.Confidence != tt.wantConf {
				t.Errorf("Confidence = %v, want %v", rc.Confidence, tt.wantConf)
			}
		})
	}
}

func TestParseRootCauseWithMarkdownCodeBlock(t *testing.T) {
	content := "```json\n{\"category\":\"软件\",\"component\":\"mysql\",\"description\":\"连接池耗尽\",\"confidence\":0.8}\n```"
	rc, err := parseRootCause(content)
	if err != nil {
		t.Fatalf("parseRootCause() error = %v", err)
	}
	if rc.Category != "软件" {
		t.Errorf("Category = %q, want 软件", rc.Category)
	}
	if rc.Component != "mysql" {
		t.Errorf("Component = %q, want mysql", rc.Component)
	}
	if rc.Description != "连接池耗尽" {
		t.Errorf("Description = %q, want 连接池耗尽", rc.Description)
	}
}

func TestAnalyzerAnalyze(t *testing.T) {
	m := &mockLLM{respContent: `{"category":"硬件","component":"磁盘","description":"磁盘满","confidence":0.9,"evidence":["磁盘使用率95%"]}`}

	analyzer := NewAnalyzer(m, DefaultConfig())

	alertCtx := &AlertContext{
		Alert: &proto.Alert{
			AlertID:   "al-1",
			DeviceID:  "dev-1",
			Severity:  "critical",
			Metric:    "disk_usage",
			Message:   "磁盘使用率超过阈值",
			CreatedAt: time.Now(),
		},
	}

	rc, err := analyzer.Analyze(context.Background(), alertCtx)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if rc.Category != "硬件" {
		t.Errorf("Category = %q, want 硬件", rc.Category)
	}
	if rc.Component != "磁盘" {
		t.Errorf("Component = %q, want 磁盘", rc.Component)
	}
	if m.callCount != 1 {
		t.Errorf("callCount = %d, want 1", m.callCount)
	}
}

func TestAnalyzerAnalyzeNilAlert(t *testing.T) {
	m := &mockLLM{}
	analyzer := NewAnalyzer(m, DefaultConfig())

	_, err := analyzer.Analyze(context.Background(), nil)
	if !errors.Is(err, nil) && err.Error() == "" {
		t.Errorf("expected error for nil alert context")
	}
	if err == nil {
		t.Errorf("expected error for nil alert context, got nil")
	}
	if !strings.Contains(err.Error(), "alert context is nil") {
		t.Errorf("error message should contain 'alert context is nil', got: %v", err)
	}
}

func TestRemediationGeneratorGenerate(t *testing.T) {
	m := &mockLLM{respContent: `{"title":"重启 nginx","steps":[{"order":1,"title":"重启服务","action":"restart","target":"dev-1","command":"systemctl restart nginx","verifyCmd":"systemctl status nginx"}],"commands":[{"agentID":"dev-1","command":"systemctl restart nginx","type":"shell","timeout":30,"priority":5}],"dagNodes":[{"id":"n1","name":"重启 nginx","type":"shell","command":"systemctl restart nginx","dependsOn":[],"retryCount":3,"retryDelay":5}],"safety":{"requiresConfirm":true,"rollbackCmd":"systemctl start nginx","riskLevel":"low","notes":["注意备份配置"]}}`}

	gen := NewRemediationGenerator(m, DefaultConfig())

	req := &RemediationRequest{
		RootCause: &RootCause{
			Category:    "软件",
			Component:   "nginx",
			Description: "nginx 进程异常退出",
			Confidence:  0.9,
			Evidence:    []string{"进程不存在"},
		},
		DeviceContext: &DeviceContext{
			DeviceInfo: &proto.DeviceInfo{
				DeviceID: "dev-1",
				Hostname: "web01",
				IP:       "10.0.0.1",
				OS:       "linux",
				Arch:     "amd64",
				State:    "online",
			},
		},
		Constraints: RemediationConstraints{
			MaxSteps:        5,
			RiskTolerance:   "low",
			RequireApproval: false,
			TimeoutSec:      30,
			AutoExecute:     false,
		},
	}

	plan, err := gen.Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if plan.Title != "重启 nginx" {
		t.Errorf("Title = %q, want 重启 nginx", plan.Title)
	}
	if len(plan.Steps) != 1 {
		t.Errorf("Steps count = %d, want 1", len(plan.Steps))
	}
	if plan.Safety.RequiresConfirm != true {
		t.Errorf("RequiresConfirm = %v, want true", plan.Safety.RequiresConfirm)
	}
	if m.callCount != 1 {
		t.Errorf("callCount = %d, want 1", m.callCount)
	}
}

func TestParseRemediationPlanInvalidJSON(t *testing.T) {
	_, err := parseRemediationPlan("not json")
	if err == nil {
		t.Errorf("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("error message should contain 'invalid JSON', got: %v", err)
	}
}

func TestParseRemediationPlanWithCodeBlock(t *testing.T) {
	content := "```json\n{\"title\":\"方案\",\"steps\":[],\"commands\":[],\"dagNodes\":[],\"safety\":{\"requiresConfirm\":false,\"riskLevel\":\"medium\",\"notes\":[]}}\n```"
	plan, err := parseRemediationPlan(content)
	if err != nil {
		t.Fatalf("parseRemediationPlan() error = %v", err)
	}
	if plan.Title != "方案" {
		t.Errorf("Title = %q, want 方案", plan.Title)
	}
	if plan.Safety.RiskLevel != "medium" {
		t.Errorf("RiskLevel = %q, want medium", plan.Safety.RiskLevel)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		sec  int
		want string
	}{
		{30, "30s"},
		{90, "1m30s"},
		{3661, "1h1m"},
	}

	for _, tt := range tests {
		got := FormatDuration(tt.sec)
		if got != tt.want {
			t.Errorf("FormatDuration(%d) = %q, want %q", tt.sec, got, tt.want)
		}
	}
}

// 确保 proto 类型被引用，避免编译器优化删除
var _ = proto.Task{}
var _ = proto.Alert{}
