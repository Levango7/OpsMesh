package config

import "testing"

// TestLoad_ABACBundleBaseURL_Env：环境变量可配置 ABAC HTTP 基地址。
func TestLoad_ABACBundleBaseURL_Env(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	t.Setenv("OPSMESH_ABAC_BUNDLE_BASE_URL", "http://controlplane:8080")

	cfg := loadForTest("--mode=agent")
	if cfg.ABACBundleBaseURL != "http://controlplane:8080" {
		t.Fatalf("ABACBundleBaseURL = %q，want env 值", cfg.ABACBundleBaseURL)
	}
}

// TestLoad_ABACBundleBaseURL_FlagWinsOverEnv：显式 flag 优先于环境变量。
func TestLoad_ABACBundleBaseURL_FlagWinsOverEnv(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	t.Setenv("OPSMESH_ABAC_BUNDLE_BASE_URL", "http://env:8080")

	cfg := loadForTest("--mode=agent", "--abac-bundle-base-url=http://flag:8080")
	if cfg.ABACBundleBaseURL != "http://flag:8080" {
		t.Fatalf("ABACBundleBaseURL = %q，显式 flag 应优先", cfg.ABACBundleBaseURL)
	}
}

// TestLoad_ABACBundleBaseURL_DefaultEmpty：未配置时为空（由 agent 从 ControlAddr 派生）。
func TestLoad_ABACBundleBaseURL_DefaultEmpty(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()

	cfg := loadForTest("--mode=agent")
	if cfg.ABACBundleBaseURL != "" {
		t.Fatalf("默认 ABACBundleBaseURL 应为空，得到 %q", cfg.ABACBundleBaseURL)
	}
}
