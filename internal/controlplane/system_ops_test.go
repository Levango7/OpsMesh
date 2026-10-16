package controlplane

import (
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// TestSafeNameRe 验证用户名/组名/服务名正则的边界。
func TestSafeNameRe(t *testing.T) {
	valid := []string{"deploy", "user_1", "app-2.0", "a", "UPPER", "x.y-z_1"}
	invalid := []string{"", "user name", "user;rm", "user$(x)", "user`x`", "user|cat", "../etc", "user\n", "user&"}

	for _, v := range valid {
		if !safeNameRe.MatchString(v) {
			t.Errorf("safeNameRe should accept %q", v)
		}
	}
	for _, v := range invalid {
		if safeNameRe.MatchString(v) {
			t.Errorf("safeNameRe should reject %q", v)
		}
	}
}

// TestSafePortRe 验证端口/端口范围正则。
func TestSafePortRe(t *testing.T) {
	valid := []string{"80", "8080", "65535", "1", "8080:8090"}
	invalid := []string{"", "0abc", "port", "8080;rm", "70000", "80 90", "-1"}

	for _, v := range valid {
		if !safePortRe.MatchString(v) {
			t.Errorf("safePortRe should accept %q", v)
		}
	}
	for _, v := range invalid {
		if safePortRe.MatchString(v) {
			t.Errorf("safePortRe should reject %q", v)
		}
	}
}

// TestSafeProtoRe 验证协议正则。
func TestSafeProtoRe(t *testing.T) {
	if !safeProtoRe.MatchString("tcp") || !safeProtoRe.MatchString("udp") {
		t.Error("safeProtoRe should accept tcp/udp")
	}
	for _, v := range []string{"icmp", "TCP", "", "tcp;rm"} {
		if safeProtoRe.MatchString(v) {
			t.Errorf("safeProtoRe should reject %q", v)
		}
	}
}

// TestBuildUserAddScript 验证创建用户脚本的构造与幂等。
func TestBuildUserAddScript(t *testing.T) {
	req := systemOpRequest{
		Username: "deploy",
		Group:    "deploygrp",
		Home:     "/home/deploy",
		Shell:    "/bin/bash",
	}
	script := buildUserAddScript(req)
	if !strings.Contains(script, "useradd") {
		t.Errorf("script should contain useradd: %s", script)
	}
	if !strings.Contains(script, `"deploy"`) {
		t.Errorf("script should contain quoted username: %s", script)
	}
	if !strings.Contains(script, "-g") || !strings.Contains(script, `"deploygrp"`) {
		t.Errorf("script should set group: %s", script)
	}
	if !strings.Contains(script, "-d") || !strings.Contains(script, "-m") {
		t.Errorf("script should set home with -m: %s", script)
	}
	if !strings.Contains(script, "-s") {
		t.Errorf("script should set shell: %s", script)
	}
	// 幂等：已存在则跳过。
	if !strings.Contains(script, "already exists") {
		t.Errorf("script should be idempotent: %s", script)
	}
}

// TestBuildUserAddScript_InjectionRejected 验证命令注入防御：非法 group 被忽略。
func TestBuildUserAddScript_InjectionRejected(t *testing.T) {
	req := systemOpRequest{
		Username: "deploy",
		Group:    "deploy; rm -rf /", // 含元字符，应被忽略
	}
	script := buildUserAddScript(req)
	if strings.Contains(script, "rm -rf") {
		t.Errorf("script must not contain injected command: %s", script)
	}
}

// TestBuildFirewallScript 验证防火墙脚本构造（firewalld + iptables 回退）。
func TestBuildFirewallScript(t *testing.T) {
	req := systemOpRequest{Port: "8080", Protocol: "tcp"}
	addScript := buildFirewallScript(req, true)
	if !strings.Contains(addScript, "firewall-cmd") || !strings.Contains(addScript, "--add-port") {
		t.Errorf("add script should use firewalld add-port: %s", addScript)
	}
	if !strings.Contains(addScript, "iptables -A") {
		t.Errorf("add script should fall back to iptables -A: %s", addScript)
	}

	delScript := buildFirewallScript(req, false)
	if !strings.Contains(delScript, "--remove-port") {
		t.Errorf("delete script should use firewalld remove-port: %s", delScript)
	}
	if !strings.Contains(delScript, "iptables -D") {
		t.Errorf("delete script should fall back to iptables -D: %s", delScript)
	}
}

// TestSystemInventoryProto 验证 SystemInventory 结构体字段完整性。
func TestSystemInventoryProto(t *testing.T) {
	inv := proto.SystemInventory{
		Users:           []proto.UserInfo{{Name: "root", UID: 0, System: true}},
		Groups:          []proto.GroupInfo{{Name: "wheel", GID: 10, Members: []string{"admin"}}},
		Firewall:        []proto.FirewallRule{{Chain: "INPUT", Target: "ACCEPT", Protocol: "tcp", Port: "22"}},
		FirewallBackend: "iptables",
	}
	if len(inv.Users) != 1 || inv.Users[0].Name != "root" || !inv.Users[0].System {
		t.Errorf("user mismatch: %+v", inv.Users)
	}
	if len(inv.Groups) != 1 || inv.Groups[0].GID != 10 {
		t.Errorf("group mismatch: %+v", inv.Groups)
	}
	if inv.FirewallBackend != "iptables" {
		t.Errorf("backend mismatch: %s", inv.FirewallBackend)
	}
}
