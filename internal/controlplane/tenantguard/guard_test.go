package tenantguard

import (
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// 租户 ID 会流入多租户 schema 名与 SQL 参数，字符集/长度是唯一的写入入口校验点。
func TestValidateTenantID(t *testing.T) {
	cases := []struct {
		name    string
		tenant  string
		wantErr bool
	}{
		{"普通租户", "tenant-01", false},
		{"允许的字符集", "A_z.0-9", false},
		{"恰好 64 字符", strings.Repeat("a", 64), false},
		{"空值由调用方先归一，本身不合法", "", true},
		{"超长 65 字符", strings.Repeat("a", 65), true},
		{"空格", "tenant 1", true},
		{"路径分隔符", "tenant/1", true},
		{"反引号（schema 注入面）", "tenant`1", true},
		{"非 ASCII", "租户1", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateTenantID(tc.tenant)
			if tc.wantErr && err == nil {
				t.Fatalf("ValidateTenantID(%q) = nil, want error", tc.tenant)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("ValidateTenantID(%q) = %v, want nil", tc.tenant, err)
			}
		})
	}
}

func TestTenantOrDefault(t *testing.T) {
	if got := TenantOrDefault(""); got != "default" {
		t.Fatalf("TenantOrDefault(空) = %q, want default", got)
	}
	if got := TenantOrDefault("tenant-a"); got != "tenant-a" {
		t.Fatalf("TenantOrDefault(显式租户) = %q, want 原值", got)
	}
}

func TestTenantAgentIn(t *testing.T) {
	st := store.NewMemoryStore()
	if a := st.Register(&proto.AgentInfo{AgentID: "agent-x", TenantID: "team-a", Segment: "seg"}); a == nil {
		t.Fatal("Register 返回 nil")
	}

	if _, err := TenantAgentIn(st, "", "team-a"); err == nil {
		t.Fatal("TenantAgentIn(空 agentID) = nil, want error")
	}
	if _, err := TenantAgentIn(st, "agent-missing", "team-a"); err == nil {
		t.Fatal("TenantAgentIn(不存在的 agent) = nil, want error")
	}

	// 跨租户拒绝，且不回显目标 agent 的真实租户（避免跨租户探测）。
	_, err := TenantAgentIn(st, "agent-x", "team-b")
	if err == nil {
		t.Fatal("TenantAgentIn(跨租户) = nil, want error")
	}
	if strings.Contains(err.Error(), "team-a") {
		t.Fatalf("错误信息回显了目标 agent 的真实租户: %v", err)
	}

	got, err := TenantAgentIn(st, "agent-x", "team-a")
	if err != nil {
		t.Fatalf("TenantAgentIn(同租户): %v", err)
	}
	if got == nil || got.AgentID != "agent-x" {
		t.Fatalf("TenantAgentIn(同租户) = %+v, want agent-x", got)
	}

	// 开放模式：调用方无租户上下文（空租户）时只校验 agent 存在。
	if _, err := TenantAgentIn(st, "agent-x", ""); err != nil {
		t.Fatalf("TenantAgentIn(开放模式): %v", err)
	}
}
