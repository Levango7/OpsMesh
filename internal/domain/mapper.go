package domain

import "opsmesh/internal/proto"

// 本文件是防腐层（ACL）：传输层 proto 与领域 domain 的双向映射。
// 业务/应用层只认 domain；gRPC/HTTP 边界才接触 proto，降级了传输结构变更对内核的冲击。

// AgentFromProto 入站：传输层 AgentInfo -> 领域 Agent。
func AgentFromProto(a *proto.AgentInfo) *Agent {
	if a == nil {
		return nil
	}
	return &Agent{
		AgentID: a.AgentID, Hostname: a.Hostname, Segment: a.Segment,
		TenantID: a.TenantID, Addr: a.Addr, GRPCPort: a.GRPCPort,
		MetricsPort: a.MetricsPort, Status: a.Status, Load: a.Load, LastSeen: a.LastSeen,
		OS: a.OS, Arch: a.Arch,
		// 安全：OnboardDeviceID 为服务端内部字段，绝不从线上 agent 自报拷贝——
		// 仅由 gRPC Register 经 ConsumeToken 校验后回填。入站防腐层必须剔除，防跨租户设备劫持。
	}
}

// AgentToProto 出站：领域 Agent -> 传输层 AgentInfo。
func AgentToProto(a *Agent) *proto.AgentInfo {
	if a == nil {
		return nil
	}
	return &proto.AgentInfo{
		AgentID: a.AgentID, Hostname: a.Hostname, Segment: a.Segment,
		TenantID: a.TenantID, Addr: a.Addr, GRPCPort: a.GRPCPort,
		MetricsPort: a.MetricsPort, Status: a.Status, Load: a.Load, LastSeen: a.LastSeen,
		OnboardDeviceID: a.OnboardDeviceID,
		OS:              a.OS, Arch: a.Arch,
	}
}

// DeviceFromProto 入站：传输层 DeviceInfo -> 领域 Device。
func DeviceFromProto(d *proto.DeviceInfo) *Device {
	if d == nil {
		return nil
	}
	return &Device{
		DeviceID: d.DeviceID, Segment: d.Segment, TenantID: d.TenantID,
		IP: d.IP, AgentID: d.AgentID, State: d.State, TaskState: d.TaskState,
		Managed: d.Managed, LastResult: d.LastResult, LastResultAt: d.LastResultAt, Retired: d.Retired,
		Hostname: d.Hostname, OS: d.OS, Arch: d.Arch,
	}
}

// DeviceToProto 出站：领域 Device -> 传输层 DeviceInfo。
func DeviceToProto(d *Device) *proto.DeviceInfo {
	if d == nil {
		return nil
	}
	return &proto.DeviceInfo{
		DeviceID: d.DeviceID, Segment: d.Segment, TenantID: d.TenantID,
		IP: d.IP, AgentID: d.AgentID, State: d.State, TaskState: d.TaskState,
		Managed: d.Managed, LastResult: d.LastResult, LastResultAt: d.LastResultAt, Retired: d.Retired,
		Hostname: d.Hostname, OS: d.OS, Arch: d.Arch,
	}
}

// TaskFromProto 入站：传输层 Task -> 领域 Task。
func TaskFromProto(t *proto.Task) *Task {
	if t == nil {
		return nil
	}
	return &Task{
		TaskID: t.TaskID, AgentID: t.AgentID, TenantID: t.TenantID,
		Type: t.Type, Command: t.Command, Content: t.Content, Path: t.Path,
		Status: t.Status, ClaimedBy: t.ClaimedBy, ClaimedAt: t.ClaimedAt, ClaimEpoch: t.ClaimEpoch, CreatedAt: t.CreatedAt,
		RetryCount: t.RetryCount, MaxRetries: t.MaxRetries, DeadLetter: t.DeadLetter,
		Schedule: t.Schedule, ParentID: t.ParentID, LastFiredAt: t.LastFiredAt, DependsOn: t.DependsOn,
	}
}

// TaskToProto 出站：领域 Task -> 传输层 Task。
func TaskToProto(t *Task) *proto.Task {
	if t == nil {
		return nil
	}
	return &proto.Task{
		TaskID: t.TaskID, AgentID: t.AgentID, TenantID: t.TenantID,
		Type: t.Type, Command: t.Command, Content: t.Content, Path: t.Path,
		Status: t.Status, ClaimedBy: t.ClaimedBy, ClaimedAt: t.ClaimedAt, ClaimEpoch: t.ClaimEpoch, CreatedAt: t.CreatedAt,
		RetryCount: t.RetryCount, MaxRetries: t.MaxRetries, DeadLetter: t.DeadLetter,
		Schedule: t.Schedule, ParentID: t.ParentID, LastFiredAt: t.LastFiredAt, DependsOn: t.DependsOn,
	}
}

// TaskResultFromProto 入站：传输层 TaskResult -> 领域 TaskResult。
func TaskResultFromProto(r *proto.TaskResult) *TaskResult {
	if r == nil {
		return nil
	}
	return &TaskResult{
		TaskID: r.TaskID, AgentID: r.AgentID, ExitCode: r.ExitCode,
		Stdout: r.Stdout, Stderr: r.Stderr, DurationMs: r.DurationMs, FinishedAt: r.FinishedAt,
		ClaimEpoch: r.ClaimEpoch,
	}
}

// AuditFromProto 入站：传输层 AuditEvent -> 领域 AuditEvent。
func AuditFromProto(e *proto.AuditEvent) *AuditEvent {
	if e == nil {
		return nil
	}
	return &AuditEvent{
		TenantID: e.TenantID, UserID: e.UserID, Action: e.Action,
		Target: e.Target, Detail: e.Detail, CreatedAt: e.CreatedAt,
	}
}

// AlertFromProto 入站：传输层 Alert -> 领域 Alert。
// 补 Status/AcknowledgedBy/SilencedUntil/Comment/UpdatedAt 状态字段映射，
// 使领域 Alert 可承载 Acknowledge/Silence 状态机行为。
func AlertFromProto(a *proto.Alert) *Alert {
	if a == nil {
		return nil
	}
	return &Alert{
		AlertID: a.AlertID, TenantID: a.TenantID, DeviceID: a.DeviceID,
		AgentID: a.AgentID, Severity: a.Severity, Message: a.Message, CreatedAt: a.CreatedAt,
		Status: a.Status, AcknowledgedBy: a.AcknowledgedBy, SilencedUntil: a.SilencedUntil,
		Comment: a.Comment, UpdatedAt: a.UpdatedAt,
	}
}

// AlertToProto 出站：领域 Alert -> 传输层 Alert。
// 补状态字段映射，使 store 层 ack/silence 后的状态可经 HTTP/gRPC 边界完整传出。
func AlertToProto(a *Alert) *proto.Alert {
	if a == nil {
		return nil
	}
	return &proto.Alert{
		AlertID: a.AlertID, TenantID: a.TenantID, DeviceID: a.DeviceID,
		AgentID: a.AgentID, Severity: a.Severity, Message: a.Message, CreatedAt: a.CreatedAt,
		Status: a.Status, AcknowledgedBy: a.AcknowledgedBy, SilencedUntil: a.SilencedUntil,
		Comment: a.Comment, UpdatedAt: a.UpdatedAt,
	}
}
