package config

import (
	"strings"
	"testing"
)

// TestLoad_ABACDeviceModeDefaultOff：设备侧 ABAC 默认 off（零行为差异）。
func TestLoad_ABACDeviceModeDefaultOff(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()

	cfg := loadForTest("--mode=agent")
	if cfg.ABACDeviceMode != ABACDeviceModeOff {
		t.Fatalf("默认 ABACDeviceMode = %q，want off", cfg.ABACDeviceMode)
	}
	if cfg.ABACDeviceSelector != "" || cfg.ABACDeviceKillSwitch || cfg.ABACDeviceDisabledIDs != "" {
		t.Fatalf("设备侧 ABAC 其他默认值应为空/false，得到 selector=%q kill=%v disabled=%q",
			cfg.ABACDeviceSelector, cfg.ABACDeviceKillSwitch, cfg.ABACDeviceDisabledIDs)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("默认配置应通过 Validate: %v", err)
	}
}

// TestLoad_ABACDeviceMode_EnvAndFlagWins：env 可配置，显式 flag 优先。
func TestLoad_ABACDeviceMode_EnvAndFlagWins(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	t.Setenv("OPSMESH_ABAC_DEVICE_MODE", "SHADOW")

	cfg := loadForTest("--mode=agent")
	if cfg.ABACDeviceMode != ABACDeviceModeShadow {
		t.Fatalf("ABACDeviceMode = %q，want shadow（env 应小写归一）", cfg.ABACDeviceMode)
	}

	cfg = loadForTest("--mode=agent", "--abac-device-mode=enforce")
	if cfg.ABACDeviceMode != ABACDeviceModeEnforce {
		t.Fatalf("显式 flag 应优先，得到 %q", cfg.ABACDeviceMode)
	}
}

// TestLoad_ABACDeviceKnobs_Env：selector / kill switch / disabled ids 的 env 解析。
func TestLoad_ABACDeviceKnobs_Env(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	t.Setenv("OPSMESH_ABAC_DEVICE_SELECTOR", "segment=prod,os=linux")
	t.Setenv("OPSMESH_ABAC_DEVICE_KILL_SWITCH", "true")
	t.Setenv("OPSMESH_ABAC_DEVICE_DISABLED_IDS", "dev-1, dev-2")

	cfg := loadForTest("--mode=agent")
	if cfg.ABACDeviceSelector != "segment=prod,os=linux" {
		t.Fatalf("ABACDeviceSelector = %q", cfg.ABACDeviceSelector)
	}
	if !cfg.ABACDeviceKillSwitch {
		t.Fatal("ABACDeviceKillSwitch 应为 true")
	}
	if cfg.ABACDeviceDisabledIDs != "dev-1, dev-2" {
		t.Fatalf("ABACDeviceDisabledIDs = %q", cfg.ABACDeviceDisabledIDs)
	}
}

// TestValidate_ABACDeviceModeInvalid：非法模式启动期拒绝。
func TestValidate_ABACDeviceModeInvalid(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()

	cfg := loadForTest("--mode=agent", "--abac-device-mode=bogus")
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "abac-device-mode") {
		t.Fatalf("非法 mode 应被 Validate 拒绝，得到 %v", err)
	}
}

// TestValidate_ABACDeviceSelectorUnknownField：未知选择器字段启动期拒绝。
func TestValidate_ABACDeviceSelectorUnknownField(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()

	cfg := loadForTest("--mode=agent", "--abac-device-selector=label=prod")
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "abac-device-selector") {
		t.Fatalf("未知 selector 字段应被 Validate 拒绝，得到 %v", err)
	}
}

// TestValidate_ABACDeviceSelectorValid：全部受支持字段可通过校验。
func TestValidate_ABACDeviceSelectorValid(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()

	cfg := loadForTest("--mode=agent",
		"--abac-device-mode=enforce",
		"--abac-device-selector=segment=prod,os=linux,arch=amd64,hostname=web-1,agent_id=agent-1")
	if err := cfg.Validate(); err != nil {
		t.Fatalf("合法 selector 应通过 Validate: %v", err)
	}
	parsed, err := ParseABACDeviceSelector(cfg.ABACDeviceSelector)
	if err != nil {
		t.Fatalf("ParseABACDeviceSelector: %v", err)
	}
	if len(parsed) != 5 || parsed["agent_id"] != "agent-1" || parsed["segment"] != "prod" {
		t.Fatalf("selector 解析结果不符: %+v", parsed)
	}
}
