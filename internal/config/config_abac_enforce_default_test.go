package config

import (
	"testing"
)

// TestLoad_ABACEnforceDefaultEmpty：ABAC 定向强制默认为空（kill switch / opt-in）。
// 默认不强制任何路由，避免生产环境未经显式授权即阻断请求。
func TestLoad_ABACEnforceDefaultEmpty(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()

	cfg := loadForTest()
	if len(cfg.ABACEnforce) != 0 {
		t.Fatalf("默认 ABACEnforce 应为空，得到 %v", cfg.ABACEnforce)
	}
	if cfg.ABACEnforcePreset != "" {
		t.Fatalf("默认 ABACEnforcePreset 应为空（opt-in），得到 %q", cfg.ABACEnforcePreset)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("默认配置应通过 Validate: %v", err)
	}
}

// TestLoad_ABACEnforceOptInViaPreset：显式 opt-in 才启用高风险预置。
func TestLoad_ABACEnforceOptInViaPreset(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()

	cfg := loadForTest("--abac-enforce-preset=high-risk")
	if cfg.ABACEnforcePreset != ABACEnforcePresetHighRisk {
		t.Fatalf("ABACEnforcePreset = %q，want %q", cfg.ABACEnforcePreset, ABACEnforcePresetHighRisk)
	}
	routes, err := ExpandABACEnforcePreset(cfg.ABACEnforcePreset)
	if err != nil {
		t.Fatalf("预置展开失败: %v", err)
	}
	if len(routes) == 0 {
		t.Fatal("opt-in 后 high-risk 预置应展开为非空路由列表")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("合法预置应通过 Validate: %v", err)
	}
}

// TestLoad_ABACEnforceDefaultEmpty_EnvNotSet：环境变量未设置时仍为空。
func TestLoad_ABACEnforceDefaultEmpty_EnvNotSet(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()

	cfg := loadForTest("--mode=controlplane")
	if cfg.ABACEnforcePreset != "" || len(cfg.ABACEnforce) != 0 {
		t.Fatalf("未配置时 ABACEnforce/Preset 应为空，得到 enforce=%v preset=%q",
			cfg.ABACEnforce, cfg.ABACEnforcePreset)
	}
}