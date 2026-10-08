package config

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestValidateRejectsPagerDutyWithoutToken(t *testing.T) {
	// 这是本笔的核心判据：开了那条"只能经无鉴权 gRPC 触发"的外发腿，就必须给凭据。
	c := &Config{PagerDutyEnabled: true, GRPCAuthToken: ""}
	if err := c.Validate(); !errors.Is(err, ErrNoAuthTokenWithPagerDuty) {
		t.Fatalf("PagerDuty 开 + 无 token 应当拒绝启动，实得 %v", err)
	}
}

func TestValidateKeepsFactoryDefaultsUnchanged(t *testing.T) {
	// 出厂默认（PagerDuty 关 + 无 token）必须照旧通过——"升级不改变现有部署"的断言。
	c := Load()
	if c.PagerDutyEnabled || c.GRPCAuthToken != "" {
		t.Fatalf("测试环境不应带着 opt-in 变量：%+v", c)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("默认配置应放行，实得 %v", err)
	}
}

func TestValidateAllowsPair(t *testing.T) {
	c := &Config{PagerDutyEnabled: true, GRPCAuthToken: "x"}
	if err := c.Validate(); err != nil {
		t.Fatalf("PagerDuty 开 + 有 token 应放行，实得 %v", err)
	}
}

func TestLoadReadsTokenFromEnv(t *testing.T) {
	t.Setenv("ALERT_SVC_GRPC_TOKEN", "tok-from-env")
	if got := Load().GRPCAuthToken; got != "tok-from-env" {
		t.Fatalf("没从 ALERT_SVC_GRPC_TOKEN 读到值，got=%q", got)
	}
}

// 密钥不能出现在任何 JSON 序列化里（诊断端点/日志把 cfg 整体 marshal 是最常见的泄露路径）。
// 这条测试是对 `json:"-"` 的直接取证，不是对注释的信任。
func TestTokenNeverSerialized(t *testing.T) {
	b, err := json.Marshal(&Config{GRPCAuthToken: "super-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "super-secret") {
		t.Fatalf("密钥出现在 JSON 里：%s", b)
	}
}
