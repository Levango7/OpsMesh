package agent

import (
	"context"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
)

// TestResolveABACSecret_PrefersRegistrationSecret：同时存在预共享 key 与注册密钥时，
// ABAC 使用 DB 签发的注册密钥，且该密钥落盘供重启复用；gRPC 密钥优先级保持不变。
func TestResolveABACSecret_PrefersRegistrationSecret(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	a := &Agent{agentID: "a1", dataDir: dir, cfg: &config.Config{GRPCSignatureKey: "pre-shared"}}

	// gRPC 行为不变：--grpc-signature-key 仍优先。
	if key, src := a.resolveAgentKey(ctx, "db-secret"); key != "pre-shared" || src != "pre-shared" {
		t.Fatalf("gRPC 密钥优先级被改变：key=%q src=%q", key, src)
	}

	key, src := a.resolveABACSecret(ctx, "db-secret")
	if key != "db-secret" || src != "register-response" {
		t.Fatalf("ABAC 应使用注册密钥：key=%q src=%q", key, src)
	}
	if got := loadAgentKey(dir); got != "db-secret" {
		t.Fatalf("注册密钥未落盘：%q", got)
	}
}

// TestResolveABACSecret_PrefersPersistedKeyOverConfiguredKey：注册响应未下发时，
// 本机 agent.key（DB 密钥）优先于 --grpc-signature-key。
func TestResolveABACSecret_PrefersPersistedKeyOverConfiguredKey(t *testing.T) {
	dir := t.TempDir()
	if err := saveAgentKey(dir, "db-secret"); err != nil {
		t.Fatalf("saveAgentKey: %v", err)
	}
	a := &Agent{agentID: "a1", dataDir: dir, cfg: &config.Config{GRPCSignatureKey: "pre-shared"}}
	key, src := a.resolveABACSecret(context.Background(), "")
	if key != "db-secret" || src != "agent.key" {
		t.Fatalf("ABAC 应使用本机 DB 密钥：key=%q src=%q", key, src)
	}
}

// TestResolveABACSecret_FallbackToConfiguredKey：注册从未提供 DB 密钥时才回退预共享 key。
func TestResolveABACSecret_FallbackToConfiguredKey(t *testing.T) {
	a := &Agent{agentID: "a1", dataDir: t.TempDir(), cfg: &config.Config{GRPCSignatureKey: "pre-shared"}}
	key, src := a.resolveABACSecret(context.Background(), "")
	if key != "pre-shared" || src != "pre-shared" {
		t.Fatalf("无注册密钥时应回退预共享：key=%q src=%q", key, src)
	}

	// 三者皆无 → 空串。
	a = &Agent{agentID: "a1", dataDir: t.TempDir(), cfg: &config.Config{}}
	if key, src = a.resolveABACSecret(context.Background(), ""); key != "" || src != "" {
		t.Fatalf("无任何密钥时应为空：key=%q src=%q", key, src)
	}
}

// TestABACHTTPBaseURL_OverrideAndDefault：--abac-bundle-base-url 覆盖 ControlAddr；
// 未配置时沿用 ControlAddr（默认派生不变）。
func TestABACHTTPBaseURL_OverrideAndDefault(t *testing.T) {
	a := &Agent{cfg: &config.Config{ControlAddr: "http://controlplane:9090"}}
	if got := a.abacHTTPBaseURL(); got != "http://controlplane:9090" {
		t.Fatalf("默认基地址应回退 ControlAddr：%q", got)
	}
	a.cfg.ABACBundleBaseURL = "http://controlplane:8080"
	if got := a.abacHTTPBaseURL(); got != "http://controlplane:8080" {
		t.Fatalf("显式基地址应优先：%q", got)
	}
}

// TestAgentABACBundleClientWiring：Agent 构造策略包客户端时使用 abacSecret 与
// abac-bundle-base-url（而非 gRPC 的 ControlAddr/agentSecret）。
func TestAgentABACBundleClientWiring(t *testing.T) {
	a := &Agent{
		agentID:    "a1",
		abacSecret: "db-secret",
		cfg: &config.Config{
			ABACBundleEnabled: true,
			ABACBundleBaseURL: "http://controlplane:8080",
			ControlAddr:       "http://controlplane:9090",
			GRPCSignatureKey:  "pre-shared",
		},
	}
	c := a.newABACBundleClient()
	if c.secret != "db-secret" {
		t.Fatalf("ABAC 客户端密钥 = %q，want db-secret（不得用 gRPC 预共享密钥）", c.secret)
	}
	const want = "http://controlplane:8080/api/v1/agent/abac-bundle"
	if got := c.endpointURL(); got != want {
		t.Fatalf("ABAC endpoint = %q，want %q", got, want)
	}
}
