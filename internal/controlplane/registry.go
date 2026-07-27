package controlplane

import (
	"time"

	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// Registry 控制面注册表薄封装：内部持有可插拔 store.Store（memory / mysql）。
// U-04: 数据本地化；默认 MemoryStore（无需外部依赖即可运行），可按 --store 切换 SQLStore。
//
// 所有公开方法签名与旧版内存实现保持一致，server.go / grpc.go 的调用无需任何改动。
type Registry struct {
	store store.Store
}

// NewRegistry 构造默认（内存）注册表，向后兼容旧调用。
func NewRegistry() *Registry {
	return &Registry{store: store.NewMemoryStore()}
}

// NewRegistryWithStore 用指定后端构造注册表（选 store 时走此路径）。
func NewRegistryWithStore(s store.Store) *Registry {
	return &Registry{store: s}
}

// Register 注册一个 agent。
func (r *Registry) Register(a *proto.AgentInfo) *proto.AgentInfo {
	return r.store.Register(a)
}

// Heartbeat 更新 agent 在线状态/负载。
func (r *Registry) Heartbeat(agentID, status string, load int) bool {
	return r.store.Heartbeat(agentID, status, load)
}

// GetTasks 返回指定 agent 的待执行任务（仅 pending，只读）。
func (r *Registry) GetTasks(agentID string) []*proto.Task {
	return r.store.GetTasks(agentID)
}

// ClaimTask 原子领取该 agent 的下一条 pending 任务（HA 协调，P1-1）。
func (r *Registry) ClaimTask(agentID string) *proto.Task {
	return r.store.ClaimTask(agentID)
}

// CreateTask 下发一个任务给指定 agent（P0-2 内部下发入口）。
func (r *Registry) CreateTask(t *proto.Task) *proto.Task {
	return r.store.CreateTask(t)
}

// UpsertDevice 写入/更新一台纳管设备（真实网段发现 P0-2 用）。
func (r *Registry) UpsertDevice(d *proto.DeviceInfo) {
	r.store.UpsertDevice(d)
}

// PendingDepth 返回当前 pending 任务总数（观测队列深度 P2-1）。
func (r *Registry) PendingDepth() int {
	return r.store.PendingDepth()
}

// SubmitResult 接收 agent 上报的执行结果。
func (r *Registry) SubmitResult(res *proto.TaskResult) {
	r.store.SubmitResult(res)
}

// AllTasks 返回全部任务（tenantID 非空时按租户过滤；供任务列表端点）。
func (r *Registry) AllTasks(tenantID string) []*proto.Task {
	return r.store.AllTasks(tenantID)
}

// Device 按 deviceID 返回单台设备（供设备详情端点）。
func (r *Registry) Device(id string) *proto.DeviceInfo {
	return r.store.Device(id)
}

// Results 返回某 agent 的上报结果（供设备详情端点）。
func (r *Registry) Results(agentID string) []*proto.TaskResult {
	return r.store.Results(agentID)
}

// TaskResult 按 taskID 返回单条执行结果（A5/F7 结果查询 API）。
func (r *Registry) TaskResult(taskID string) *proto.TaskResult {
	return r.store.TaskResult(taskID)
}

// CancelTask 取消任务（F3）。
func (r *Registry) CancelTask(id, tenantID string) bool {
	return r.store.CancelTask(id, tenantID)
}

// RetireDevice 退役/下线设备（F5）。
func (r *Registry) RetireDevice(id, tenantID string) bool {
	return r.store.RetireDevice(id, tenantID)
}

// Provision B1 自动纳管闭环：为已发现候选设备签发一次性 install token 并标记 provisioning。
func (r *Registry) Provision(deviceID, host, tenantID string) (token, bootstrap string, err error) {
	return r.store.Provision(deviceID, host, tenantID)
}

// IssueToken 生成并登记一次性 install token（B1）。
func (r *Registry) IssueToken(deviceID, tenantID string, ttl time.Duration) (token string, err error) {
	return r.store.IssueToken(deviceID, tenantID, ttl)
}

// ConsumeToken 校验并消费 install token（B1）；校验通过返回设备与租户。
func (r *Registry) ConsumeToken(token string) (deviceID, tenantID string, ok bool) {
	return r.store.ConsumeToken(token)
}

// Alerts 返回活跃告警（M7）。
func (r *Registry) Alerts(tenantID string) []*proto.Alert {
	return r.store.Alerts(tenantID)
}

// AddAlert 记录一条告警（M7）。
func (r *Registry) AddAlert(a *proto.Alert) {
	r.store.AddAlert(a)
}

// Alert 按 alertID 返回单条告警（M7；供 ack/silence 定位）。
func (r *Registry) Alert(id string) *proto.Alert {
	return r.store.Alert(id)
}

// AckAlert 确认告警（M7）。
func (r *Registry) AckAlert(id, tenantID, by string) bool {
	return r.store.AckAlert(id, tenantID, by)
}

// SilenceAlert 静默告警（M7）。
func (r *Registry) SilenceAlert(id, tenantID, by string, until time.Time, comment string) bool {
	return r.store.SilenceAlert(id, tenantID, by, until, comment)
}

// Audit 记录一条审计事件（U-04 等保三级留痕）。
func (r *Registry) Audit(e *proto.AuditEvent) {
	r.store.Audit(e)
}

// Agents 返回已注册 agent（tenantID 非空时按租户过滤；空串=全部）。
func (r *Registry) Agents(tenantID string) []*proto.AgentInfo {
	return r.store.Agents(tenantID)
}

// Agent 按 agentID 直接返回单台 agent（P2-17）。
func (r *Registry) Agent(id string) *proto.AgentInfo {
	return r.store.Agent(id)
}

// ReclaimStaleTasks 复位超期 running 任务为 pending（P0-1 任务必达）。
func (r *Registry) ReclaimStaleTasks(maxAge time.Duration) int {
	return r.store.ReclaimStaleTasks(maxAge)
}

// FireDueSchedules 评估所有模板任务并派生到点的 pending 实例（F4 定时/周期调度）。
func (r *Registry) FireDueSchedules(now time.Time) int {
	return r.store.FireDueSchedules(now)
}

// QueryAudits 按租户/动作/时间窗过滤审计事件（P0-4 审计可查）。
func (r *Registry) QueryAudits(tenant, action string, since, until time.Time, limit int) []*proto.AuditEvent {
	return r.store.QueryAudits(tenant, action, since, until, limit)
}

// Snapshot 返回 segment -> 设备列表 的当前视图（tenantID 非空时按租户过滤）。
func (r *Registry) Snapshot(tenantID string) map[string][]proto.DeviceInfo {
	return r.store.Snapshot(tenantID)
}

// RenewLeadership A3 选主：转发到 store（MemoryStore 恒 true；SQLStore 经 leader_lease 抢占/续租）。
func (r *Registry) RenewLeadership(ttl time.Duration) bool {
	return r.store.RenewLeadership(ttl)
}

// IsLeader A3 选主：返回本控制面实例当前是否持有 leader 租约。
func (r *Registry) IsLeader() bool {
	return r.store.IsLeader()
}

// CancelledTaskIDs F3 取消信号下发：返回该 agent 当前 cancelled 状态的任务 ID。
func (r *Registry) CancelledTaskIDs(agentID string) []string {
	return r.store.CancelledTaskIDs(agentID)
}

// RetireStaleDevices F5 离线超龄自动归档：返回归档设备数（仅 leader 调用）。
func (r *Registry) RetireStaleDevices(maxAge time.Duration) int {
	return r.store.RetireStaleDevices(maxAge)
}

// CleanupTokens 清理过期/已消费的 install token（F9 无界增长防护），仅 leader 周期执行。
func (r *Registry) CleanupTokens(batch int) int {
	return r.store.CleanupTokens(batch)
}
