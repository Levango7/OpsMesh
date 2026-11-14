package netsec

import "testing"

// TestBuild_AllCapabilitiesAllActions 覆盖六能力 × 全部动作：每个组合都必须有脚本。
func TestBuild_AllCapabilitiesAllActions(t *testing.T) {
	caps := []Capability{CapIPSIDS, CapSIEM, CapHoneypot, CapTrafficShaping, CapVPN, CapZeroTrust}
	for _, c := range caps {
		actions := SupportedActions(c)
		if len(actions) == 0 {
			t.Fatalf("%s: SupportedActions 为空", c)
		}
		for _, a := range actions {
			script, ok := Build(c, a)
			if !ok {
				t.Fatalf("%s/%s: Build 返回 ok=false，但该动作在 SupportedActions 中", c, a)
			}
			if script == "" {
				t.Fatalf("%s/%s: 脚本为空", c, a)
			}
		}
	}
}

// TestBuild_UnknownActionRejected 未知动作必须 ok=false（调用方据此返回 400）。
func TestBuild_UnknownActionRejected(t *testing.T) {
	caps := []Capability{CapIPSIDS, CapSIEM, CapHoneypot, CapTrafficShaping, CapVPN, CapZeroTrust}
	for _, c := range caps {
		if _, ok := Build(c, "no-such-action"); ok {
			t.Fatalf("%s: 未知动作应返回 ok=false", c)
		}
	}
	if _, ok := Build("no-such-cap", "status"); ok {
		t.Fatal("未知能力应返回 ok=false")
	}
}

// TestTaskAction_Format 任务标签必须保持既有格式 <capability>_<action>（controlplane 兼容）。
func TestTaskAction_Format(t *testing.T) {
	got := TaskAction(CapIPSIDS, "deploy")
	if got != "ips_ids_deploy" {
		t.Fatalf("TaskAction = %q, want ips_ids_deploy", got)
	}
	got = TaskAction(CapZeroTrust, "policy")
	if got != "zerotrust_policy" {
		t.Fatalf("TaskAction = %q, want zerotrust_policy", got)
	}
}

// TestSupportedActions_NoDuplicates 动作集合内部不得重复。
func TestSupportedActions_NoDuplicates(t *testing.T) {
	caps := []Capability{CapIPSIDS, CapSIEM, CapHoneypot, CapTrafficShaping, CapVPN, CapZeroTrust}
	for _, c := range caps {
		seen := map[string]bool{}
		for _, a := range SupportedActions(c) {
			if seen[a] {
				t.Fatalf("%s: 动作 %q 重复", c, a)
			}
			seen[a] = true
		}
	}
}
