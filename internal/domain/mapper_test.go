package domain

import (
	"testing"
	"time"

	"opsmesh/internal/proto"
)

// TestMapper_AgentRoundTrip 校验 Agent 的 proto<->domain 双向映射不丢字段（ACL 防腐层）。
func TestMapper_AgentRoundTrip(t *testing.T) {
	now := time.Now()
	p := &proto.AgentInfo{
		AgentID: "agent-1", Hostname: "h1", Segment: "seg-a", TenantID: "t1",
		Addr: "10.0.0.1", GRPCPort: 9090, MetricsPort: 9091, Status: "online", Load: 3, LastSeen: now,
	}
	d := AgentFromProto(p)
	if d.AgentID != p.AgentID || d.TenantID != p.TenantID || d.Segment != p.Segment ||
		d.GRPCPort != p.GRPCPort || d.Load != p.Load || !d.LastSeen.Equal(p.LastSeen) {
		t.Fatalf("AgentFromProto 丢字段: %+v", d)
	}
	back := AgentToProto(d)
	if back.AgentID != p.AgentID || back.LastSeen.Unix() != p.LastSeen.Unix() {
		t.Fatalf("AgentToProto 丢字段: %+v", back)
	}
}

// TestMapper_TaskRoundTrip 校验 Task（含新增 Content/Path）映射完整。
func TestMapper_TaskRoundTrip(t *testing.T) {
	p := &proto.Task{
		TaskID: "task-1", AgentID: "agent-1", TenantID: "t1", Type: "file",
		Command: "echo", Content: "hello", Path: "/tmp/x", Status: "pending",
	}
	d := TaskFromProto(p)
	if d.Content != "hello" || d.Path != "/tmp/x" || d.Type != "file" {
		t.Fatalf("TaskFromProto 丢字段: %+v", d)
	}
	back := TaskToProto(d)
	if back.Content != "hello" || back.Path != "/tmp/x" {
		t.Fatalf("TaskToProto 丢字段: %+v", back)
	}
}

// TestMapper_NilSafe 校验 nil 输入不 panic（边界防御）。
func TestMapper_NilSafe(t *testing.T) {
	if AgentFromProto(nil) != nil || AgentToProto(nil) != nil ||
		DeviceFromProto(nil) != nil || TaskFromProto(nil) != nil ||
		TaskResultFromProto(nil) != nil || AuditFromProto(nil) != nil {
		t.Fatal("nil 映射应返回 nil")
	}
}

// TestMapper_AgentFromProto_StripsOnboardDeviceID 安全回归（P0-F1）：
// agent 在线上自报的 OnboardDeviceID 绝不进入 domain（跨租户设备劫持防护），
// 该字段只能由 gRPC Register 经 ConsumeToken 校验后回填。
func TestMapper_AgentFromProto_StripsOnboardDeviceID(t *testing.T) {
	p := &proto.AgentInfo{
		AgentID: "agent-evil", Hostname: "h1", Segment: "seg-a", TenantID: "tA",
		OnboardDeviceID: "victim-device", // 攻击者试图劫持的目标设备
	}
	d := AgentFromProto(p)
	if d.OnboardDeviceID != "" {
		t.Fatalf("AgentFromProto 不应映射 agent 自报的 OnboardDeviceID，got %q", d.OnboardDeviceID)
	}
	// 但服务端回填后，出站映射应保留（供 store 翻转设备用）。
	d.OnboardDeviceID = "legit-device"
	back := AgentToProto(d)
	if back.OnboardDeviceID != "legit-device" {
		t.Fatalf("AgentToProto 应保留服务端回填的 OnboardDeviceID，got %q", back.OnboardDeviceID)
	}
}
