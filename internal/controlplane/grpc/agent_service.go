package grpc

import (
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// AgentService 封装 gRPC handler 需要的 store 操作，解耦 gRPC 层与 store 层。
//
// 背景：原 GrpcServerImpl 直接持有 store.Store（35 领域组合大接口），20 处
// g.Store.* 调用分布在 11 个方法中、跨 6 个领域（Token/Device/Task/Audit/Log
// 及 AgentSecret）。这阻碍了 TD-60 微服务切流演进——本接口是切换到远程调用的
// 前置基础设施。
//
// 当前唯一实现是 storeAgentService（适配 store.Store，纯薄转发）；
// 未来 TD-60 微服务切流时可替换为远程调用实现（如 rpcAgentService 走 gRPC/HTTP
// 调用下游 agent-service 微服务），GrpcServerImpl 无需改动。
//
// 接口方法签名与 store.Store 对应方法完全一致，确保适配层零语义偏移。
type AgentService interface {
	// Token 领域（TokenStore）
	ConsumeToken(token string) (deviceID, tenantID string, ok bool)

	// Agent/Device 领域（DeviceStore）
	Register(agent *proto.AgentInfo) *proto.AgentInfo
	UpsertDevice(device *proto.DeviceInfo)
	Agents(tenantID string) []*proto.AgentInfo
	Agent(agentID string) *proto.AgentInfo
	AgentSecret(agentID string) string
	Heartbeat(agentID, status string, load int) bool
	StoreDeviceMetrics(deviceID string, metrics *proto.DeviceMetrics)

	// Task 领域（TaskStore）
	ClaimTask(agentID string) *proto.Task
	PendingDepth() int
	SubmitResult(result *proto.TaskResult)
	CancelTask(taskID, tenantID string) bool
	CancelledTaskIDs(agentID string) []string

	// Audit 领域（AuditStore）
	Audit(event *proto.AuditEvent)

	// Log 领域（AgentLogStore）
	SaveLogs(tenantID string, report *proto.LogReport) error
}

// storeAgentService 适配 store.Store 实现 AgentService 接口。
// 纯薄转发，不添加任何业务逻辑；是当前唯一实现。
type storeAgentService struct {
	store store.Store
}

// NewStoreAgentService 从 store.Store 创建 AgentService 适配器。
// s 为 nil 时返回的适配器方法调用会 panic（与原 g.Store.* 在 Store 为 nil 时行为一致）。
func NewStoreAgentService(s store.Store) AgentService {
	return &storeAgentService{store: s}
}

// Token 领域

func (a *storeAgentService) ConsumeToken(token string) (deviceID, tenantID string, ok bool) {
	return a.store.ConsumeToken(token)
}

// Agent/Device 领域

func (a *storeAgentService) Register(agent *proto.AgentInfo) *proto.AgentInfo {
	return a.store.Register(agent)
}

func (a *storeAgentService) UpsertDevice(device *proto.DeviceInfo) {
	a.store.UpsertDevice(device)
}

func (a *storeAgentService) Agents(tenantID string) []*proto.AgentInfo {
	return a.store.Agents(tenantID)
}

func (a *storeAgentService) Agent(agentID string) *proto.AgentInfo {
	return a.store.Agent(agentID)
}

func (a *storeAgentService) AgentSecret(agentID string) string {
	return a.store.AgentSecret(agentID)
}

func (a *storeAgentService) Heartbeat(agentID, status string, load int) bool {
	return a.store.Heartbeat(agentID, status, load)
}

func (a *storeAgentService) StoreDeviceMetrics(deviceID string, metrics *proto.DeviceMetrics) {
	a.store.StoreDeviceMetrics(deviceID, metrics)
}

// Task 领域

func (a *storeAgentService) ClaimTask(agentID string) *proto.Task {
	return a.store.ClaimTask(agentID)
}

func (a *storeAgentService) PendingDepth() int {
	return a.store.PendingDepth()
}

func (a *storeAgentService) SubmitResult(result *proto.TaskResult) {
	a.store.SubmitResult(result)
}

func (a *storeAgentService) CancelTask(taskID, tenantID string) bool {
	return a.store.CancelTask(taskID, tenantID)
}

func (a *storeAgentService) CancelledTaskIDs(agentID string) []string {
	return a.store.CancelledTaskIDs(agentID)
}

// Audit 领域

func (a *storeAgentService) Audit(event *proto.AuditEvent) {
	a.store.Audit(event)
}

// Log 领域

func (a *storeAgentService) SaveLogs(tenantID string, report *proto.LogReport) error {
	return a.store.SaveLogs(tenantID, report)
}
