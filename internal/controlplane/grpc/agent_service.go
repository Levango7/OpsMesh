package grpc

import (
	"context"
	"log"

	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
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
//
// preClaim 是 task.preClaim 扩展点钩子（TD-62 ③）。nil 表示未接线，
// ClaimTask 行为与接线前**逐字节一致**——扩展点是可选增强，不是必经路径。
type storeAgentService struct {
	store    store.Store
	preClaim func(ctx context.Context, agentID string) error
}

// NewStoreAgentService 从 store.Store 创建 AgentService 适配器。
// s 为 nil 时返回的适配器方法调用会 panic（与原 g.Store.* 在 Store 为 nil 时行为一致）。
//
// 返回的适配器**未接线任何扩展点**；需要 task.preClaim 时用
// NewStoreAgentServiceWithHooks。
func NewStoreAgentService(s store.Store) AgentService {
	return &storeAgentService{store: s}
}

// NewStoreAgentServiceWithHooks 创建带 task.preClaim 扩展点的适配器。
//
// 为什么单独一个构造函数而不是改 NewStoreAgentService 的签名：
// 扩展点是**可选**能力，把它塞进必选参数会让每个调用方（含只想要纯转发的
// 测试）都被迫传 nil，而"传 nil"在调用点上看不出是"没接线"还是"忘了接线"。
// 两个名字把这两种意图分开，也让门禁能直接搜到接线点。
//
// preClaim 为 nil 时等价 NewStoreAgentService。
func NewStoreAgentServiceWithHooks(s store.Store, preClaim func(ctx context.Context, agentID string) error) AgentService {
	return &storeAgentService{store: s, preClaim: preClaim}
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

// ClaimTask agent 领取下一个待执行任务。
//
// 接线 task.preClaim 扩展点（TD-62 ③）：钩子返回 error 时**不下发任务**。
//
// 阻断时返回 nil 而非错误，是刻意的：store.ClaimTask 在无待领任务时同样返回 nil，
// 因此"被插件策略拒绝"与"当前没有任务"对 agent 而言同形——
// 不向 agent 泄露"你有任务但被策略拦了"这一信息（准入策略本身不应被探测）。
// 拒绝原因只进服务端日志，供运维排查。
func (a *storeAgentService) ClaimTask(agentID string) *proto.Task {
	if a.preClaim != nil {
		if err := a.preClaim(context.Background(), agentID); err != nil {
			log.Printf("[controlplane-grpc] task.preClaim 钩子拒绝 agent %s 领取任务: %v", agentID, err)
			return nil
		}
	}
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
