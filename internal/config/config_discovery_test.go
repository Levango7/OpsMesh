// config_discovery_test.go 服务发现配置测试。
package config

import (
	"testing"
)

// TestConfig_DiscoveryFields 验证 服务发现相关字段存在且默认值合理。
func TestConfig_DiscoveryFields(t *testing.T) {
	c := &Config{}
	// 默认值：空字符串（未配置多控制面，回退到 ControlAddr）
	if c.ControlplaneEndpoints != "" {
		t.Fatalf("ControlplaneEndpoints 默认应为空，得到 %q", c.ControlplaneEndpoints)
	}
	if c.LBStrategy != "" {
		t.Fatalf("LBStrategy 默认应为空，得到 %q", c.LBStrategy)
	}
}

// TestConfig_LBStrategyValidate 验证 LBStrategy 字段在 Validate 中不强制校验
// （Load 中已做回退处理，Validate 不重复校验，保持启动友好）。
func TestConfig_LBStrategyValidate(t *testing.T) {
	c := base()
	c.LBStrategy = "round-robin"
	if err := c.Validate(); err != nil {
		t.Fatalf("LBStrategy=round-robin 应通过 Validate: %v", err)
	}
	c.LBStrategy = "failover"
	if err := c.Validate(); err != nil {
		t.Fatalf("LBStrategy=failover 应通过 Validate: %v", err)
	}
}

// TestConfig_ControlplaneEndpointsBackwardCompat 验证 ControlplaneEndpoints 为空时
// 不影响现有 ControlAddr/ControlAddrs 行为（向后兼容）。
func TestConfig_ControlplaneEndpointsBackwardCompat(t *testing.T) {
	c := base()
	c.Mode = "agent"
	c.ControlAddr = "http://127.0.0.1:8080"
	c.ControlplaneEndpoints = "" // 未配置多控制面
	c.ControlAddrs = ""          // 未配置多控制面
	if err := c.Validate(); err != nil {
		t.Fatalf("向后兼容配置应通过 Validate: %v", err)
	}
}
