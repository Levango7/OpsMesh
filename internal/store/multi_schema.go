// multi_schema.go 实现 多租户 schema 隔离。
//
// 设计目标：每个租户路由到独立的 MySQL schema（database），实现物理级数据隔离。
// 相比行级隔离（tenant_id 列），schema 隔离提供更强的隔离边界：
//   - 单租户故障/误删不影响其他租户；
//   - 单租户可独立备份/迁移/清理；
//   - 跨租户查询必须显式聚合，避免误漏 tenant_id 过滤导致越权。
//
// 路由策略：
//   - 显式 tenantID 参数的方法（如 Snapshot(tenantID)）直接路由；
//   - payload 内含 TenantID 的方法（如 Register(*AgentInfo)）从 payload 提取；
//   - 无 tenant 上下文的方法（如 Heartbeat(agentID,...)）经反查索引
//     （agentTenant/deviceTenant/taskTenant）定位租户；
//   - 跨租户聚合方法（如 PendingDepth()）遍历所有 schema 求和/合并；
//   - Leader 选举在所有 schema 上续租，任一为主即为主（leader 周期任务遍历所有 schema）。
//
// 安全（SQL 注入防护）：schema 名经 SchemaNamer 白名单校验（只允许 [a-zA-Z0-9_]），
// 非法字符直接返回 error，不会拼进 DSN/SQL。
package store

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"opsmesh/internal/events"
	"opsmesh/internal/proto"
)

// SchemaNamer 把租户名映射为 MySQL schema（database）名。
// 返回 error 表示租户名非法（含 SQL 注入字符等），调用方应拒绝。
type SchemaNamer func(tenant string) (string, error)

// errEmptyTenant 表示租户标识为空，无法路由到独立 schema。
var errEmptyTenant = errors.New("multi-schema: 租户标识为空，无法路由")

// DefaultSchemaNamer 返回默认的 schema 命名函数：prefix + tenant。
//
// 安全（SQL 注入防护）：对 tenant 做白名单校验，只允许 [a-zA-Z0-9_]，
// 含任何其他字符（如 ' ; -- 空格 等）直接返回 error，避免拼进 DSN/SQL 造成注入。
// prefix 本身也做同样校验，防止运维配置的 prefix 含非法字符。
func DefaultSchemaNamer(prefix string) SchemaNamer {
	return func(tenant string) (string, error) {
		if tenant == "" {
			return "", errEmptyTenant
		}
		if err := validateIdent(tenant); err != nil {
			return "", fmt.Errorf("multi-schema: tenant %q 非法: %w", tenant, err)
		}
		if err := validateIdent(prefix); err != nil {
			return "", fmt.Errorf("multi-schema: schema-prefix %q 非法: %w", prefix, err)
		}
		return prefix + tenant, nil
	}
}

// validateIdent 校验标识符只含 [a-zA-Z0-9_]，防 SQL 注入。
// 空串视为合法（prefix 允许为空，tenant 由调用方单独校验非空）。
func validateIdent(s string) error {
	for _, r := range s {
		isLower := r >= 'a' && r <= 'z'
		isUpper := r >= 'A' && r <= 'Z'
		isDigit := r >= '0' && r <= '9'
		isUnderscore := r == '_'
		if !isLower && !isUpper && !isDigit && !isUnderscore {
			return fmt.Errorf("含非法字符 %q（只允许字母数字下划线）", r)
		}
	}
	return nil
}

// dsnForSchema 把 baseDSN 中的 database 名替换为 schema 名。
// MySQL DSN 格式：user:pass@tcp(host:port)/dbname?params
// 找到最后一个 '/' 之后、'?' 之前（或末尾）的部分，替换为 schema。
// 若 DSN 不含 '/'（无效格式），原样返回（让 sql.Open 报错）。
func dsnForSchema(baseDSN, schema string) string {
	slashIdx := strings.LastIndex(baseDSN, "/")
	if slashIdx == -1 {
		return baseDSN
	}
	afterSlash := baseDSN[slashIdx+1:]
	qIdx := strings.Index(afterSlash, "?")
	if qIdx == -1 {
		return baseDSN[:slashIdx+1] + schema
	}
	return baseDSN[:slashIdx+1] + schema + afterSlash[qIdx:]
}

// MultiSchemaStore 多租户 schema 隔离存储：每个租户路由到独立的 *SQLStore（独立 schema/database）。
//
// 实现 Store 接口的所有方法，内部按 tenant 路由到对应的 SQLStore。
// 惰性创建：第一次访问某 tenant 时创建对应的 SQLStore（建表）。
//
// 反查索引（agentTenant/deviceTenant/taskTenant）用于无 tenant 参数的方法路由：
//   - Register/UpsertDevice/CreateTask/Provision 时填充索引；
//   - Heartbeat(agentID)/GetTasks(agentID)/Device(id)/Agent(id)/TaskResult(taskID) 等经索引反查租户。
//
// 线程安全：所有字段访问经 m.mu 保护。store 方法本身不回调 MultiSchemaStore，无死锁风险。
type MultiSchemaStore struct {
	mu           sync.RWMutex
	baseDSN      string // 基础 DSN（database 名会被替换为 schema 名）
	redisAddr    string // Redis 地址（所有 schema 共享，仅作缓存）
	namer        SchemaNamer
	storeFactory func(schema string) (Store, error) // 创建新 schema 的 store（生产用 *SQLStore，测试可注入 mock）
	stores       map[string]Store                   // tenantID -> per-tenant store

	// 配置项（创建新 schema 时传播给 *SQLStore）
	demo   bool
	bus    events.Bus
	secret string

	// 反查索引：无 tenant 参数的方法经此定位租户
	agentTenant  map[string]string // agentID  -> tenantID
	deviceTenant map[string]string // deviceID -> tenantID
	taskTenant   map[string]string // taskID   -> tenantID
}

// NewMultiSchemaStore 构造多租户 schema 隔离存储。
// baseDSN 为基础 MySQL DSN（database 名会被替换为各租户的 schema 名）。
// redisAddr 为空则跳过 Redis 缓存。
// namer 为租户→schema 名的映射函数（含 SQL 注入防护）。
func NewMultiSchemaStore(baseDSN, redisAddr string, namer SchemaNamer) (*MultiSchemaStore, error) {
	if namer == nil {
		return nil, errors.New("multi-schema: namer 为 nil")
	}
	m := &MultiSchemaStore{
		baseDSN:      baseDSN,
		redisAddr:    redisAddr,
		namer:        namer,
		stores:       make(map[string]Store),
		agentTenant:  make(map[string]string),
		deviceTenant: make(map[string]string),
		taskTenant:   make(map[string]string),
		secret:       mustRandHex(32),
	}
	m.storeFactory = m.defaultStoreFactory
	return m, nil
}

// newMultiSchemaWithFactory 测试用构造函数：注入自定义 store 工厂，避免依赖真实 MySQL。
// 生产代码应使用 NewMultiSchemaStore。
func newMultiSchemaWithFactory(namer SchemaNamer, factory func(schema string) (Store, error)) *MultiSchemaStore {
	return &MultiSchemaStore{
		namer:        namer,
		storeFactory: factory,
		stores:       make(map[string]Store),
		agentTenant:  make(map[string]string),
		deviceTenant: make(map[string]string),
		taskTenant:   make(map[string]string),
		secret:       mustRandHex(32),
	}
}

// defaultStoreFactory 生产路径工厂：为指定 schema 名创建 *SQLStore（DSN 中 database 替换为 schema），
// 并传播 bus/secret/demo 配置。
func (m *MultiSchemaStore) defaultStoreFactory(schema string) (Store, error) {
	dsn := dsnForSchema(m.baseDSN, schema)
	ss, err := NewSQLStore(dsn, m.redisAddr)
	if err != nil {
		return nil, fmt.Errorf("multi-schema: 创建 schema %q 的 SQLStore 失败: %w", schema, err)
	}
	return ss.WithBus(m.bus).WithSecret(m.secret).WithDemo(m.demo), nil
}

// WithBus 注入事件总线（store 构造后由控制面注入）。
// 影响后续创建的 schema；已创建的 schema 不受影响（其 bus 在创建时已注入）。
// 线程安全：非并发安全，须在 Start/首次并发访问前调用。
func (m *MultiSchemaStore) WithBus(b events.Bus) *MultiSchemaStore {
	m.mu.Lock()
	m.bus = b
	m.mu.Unlock()
	return m
}

// WithSecret 注入 B1 install token 的 HMAC 签名密钥。
// 多副本控制面共享同一 MySQL 时须注入一致密钥。
// 影响后续创建的 schema；已创建的 schema 不受影响。
// 线程安全：非并发安全，须在 Start/首次并发访问前调用。
func (m *MultiSchemaStore) WithSecret(secret string) *MultiSchemaStore {
	if secret != "" {
		m.mu.Lock()
		m.secret = secret
		m.mu.Unlock()
	}
	return m
}

// WithDemo 设置演示模式：传播到所有已创建的 schema 及后续创建的 schema。
func (m *MultiSchemaStore) WithDemo(b bool) Store {
	m.mu.Lock()
	m.demo = b
	for _, s := range m.stores {
		s.WithDemo(b)
	}
	m.mu.Unlock()
	return m
}

// storeFor 获取或惰性创建指定租户的 store。
// tenantID 为空时返回 errEmptyTenant（调用方应拒绝空租户）。
// namer 返回错误时透传（含 SQL 注入防护错误）。
func (m *MultiSchemaStore) storeFor(tenantID string) (Store, error) {
	if tenantID == "" {
		return nil, errEmptyTenant
	}
	// 快路径：读锁查已创建的 store。
	m.mu.RLock()
	s, ok := m.stores[tenantID]
	m.mu.RUnlock()
	if ok {
		return s, nil
	}
	// 慢路径：写锁创建新 store（double-check 防重复创建）。
	schema, err := m.namer(tenantID)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.stores[tenantID]; ok {
		return s, nil
	}
	ns, err := m.storeFactory(schema)
	if err != nil {
		return nil, err
	}
	m.stores[tenantID] = ns
	return ns, nil
}

// allStores 返回所有已创建 store 的快照（slice），避免调用方遍历时持锁。
func (m *MultiSchemaStore) allStores() []Store {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Store, 0, len(m.stores))
	for _, s := range m.stores {
		out = append(out, s)
	}
	return out
}

// lookupAgentTenant 经反查索引定位 agent 所属租户。返回空串表示未注册过。
func (m *MultiSchemaStore) lookupAgentTenant(agentID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.agentTenant[agentID]
}

func (m *MultiSchemaStore) lookupDeviceTenant(deviceID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.deviceTenant[deviceID]
}

func (m *MultiSchemaStore) lookupTaskTenant(taskID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.taskTenant[taskID]
}

// setAgentTenant 更新 agent→租户索引（写锁）。
func (m *MultiSchemaStore) setAgentTenant(agentID, tenantID string) {
	m.mu.Lock()
	m.agentTenant[agentID] = tenantID
	m.mu.Unlock()
}

func (m *MultiSchemaStore) setDeviceTenant(deviceID, tenantID string) {
	m.mu.Lock()
	m.deviceTenant[deviceID] = tenantID
	m.mu.Unlock()
}

func (m *MultiSchemaStore) setTaskTenant(taskID, tenantID string) {
	m.mu.Lock()
	m.taskTenant[taskID] = tenantID
	m.mu.Unlock()
}

// ============================================================================
// DeviceStore 实现（10 方法）
// ============================================================================

// Register 注册 agent：从 a.TenantID 路由到对应 schema，并填充 agent 反查索引。
func (m *MultiSchemaStore) Register(a *proto.AgentInfo) *proto.AgentInfo {
	s, err := m.storeFor(a.TenantID)
	if err != nil {
		log.Printf("[multi-schema] Register 路由失败 tenant=%q: %v", a.TenantID, err)
		return a
	}
	m.setAgentTenant(a.AgentID, a.TenantID)
	return s.Register(a)
}

// Heartbeat 更新 agent 状态：经 agentTenant 反查租户路由。
func (m *MultiSchemaStore) Heartbeat(agentID, status string, load int) bool {
	tenant := m.lookupAgentTenant(agentID)
	if tenant == "" {
		return false
	}
	s, err := m.storeFor(tenant)
	if err != nil {
		return false
	}
	return s.Heartbeat(agentID, status, load)
}

// Device 按 deviceID 返回单台设备：经 deviceTenant 反查租户路由。
func (m *MultiSchemaStore) Device(id string) *proto.DeviceInfo {
	tenant := m.lookupDeviceTenant(id)
	if tenant == "" {
		return nil
	}
	s, err := m.storeFor(tenant)
	if err != nil {
		return nil
	}
	return s.Device(id)
}

// Results 返回某 agent 的上报结果：经 agentTenant 反查租户路由。
func (m *MultiSchemaStore) Results(agentID string) []*proto.TaskResult {
	tenant := m.lookupAgentTenant(agentID)
	if tenant == "" {
		return nil
	}
	s, err := m.storeFor(tenant)
	if err != nil {
		return nil
	}
	return s.Results(agentID)
}

// UpsertDevice 写入/更新设备：从 d.TenantID 路由，并填充 device 反查索引。
func (m *MultiSchemaStore) UpsertDevice(d *proto.DeviceInfo) {
	if d == nil || d.DeviceID == "" {
		return
	}
	s, err := m.storeFor(d.TenantID)
	if err != nil {
		log.Printf("[multi-schema] UpsertDevice 路由失败 tenant=%q: %v", d.TenantID, err)
		return
	}
	m.setDeviceTenant(d.DeviceID, d.TenantID)
	s.UpsertDevice(d)
}

// RetireDevice 退役设备：直接用 tenantID 路由。
func (m *MultiSchemaStore) RetireDevice(id, tenantID string) bool {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return false
	}
	return s.RetireDevice(id, tenantID)
}

// Snapshot 返回设备视图：直接用 tenantID 路由。
func (m *MultiSchemaStore) Snapshot(tenantID string) map[string][]proto.DeviceInfo {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.Snapshot(tenantID)
}

// Agents 返回已注册 agent：直接用 tenantID 路由。
func (m *MultiSchemaStore) Agents(tenantID string) []*proto.AgentInfo {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.Agents(tenantID)
}

// Agent 按 agentID 返回单台 agent：经 agentTenant 反查租户路由。
func (m *MultiSchemaStore) Agent(id string) *proto.AgentInfo {
	tenant := m.lookupAgentTenant(id)
	if tenant == "" {
		return nil
	}
	s, err := m.storeFor(tenant)
	if err != nil {
		return nil
	}
	return s.Agent(id)
}

// AgentSecret 返回该 agent 的 HMAC 签名密钥（gRPC 身份绑定）。
// 经 agentTenant 反查租户路由到对应 schema 的 store，再委托其 AgentSecret。
// agent 不存在或未生成密钥时返回空串。
func (m *MultiSchemaStore) AgentSecret(agentID string) string {
	tenant := m.lookupAgentTenant(agentID)
	if tenant == "" {
		return ""
	}
	s, err := m.storeFor(tenant)
	if err != nil {
		return ""
	}
	return s.AgentSecret(agentID)
}

// RetireStaleDevices F5 离线超龄自动归档：遍历所有 schema 求和（leader 周期执行）。
func (m *MultiSchemaStore) RetireStaleDevices(maxAge time.Duration) int {
	total := 0
	for _, s := range m.allStores() {
		total += s.RetireStaleDevices(maxAge)
	}
	return total
}

// StoreDeviceMetrics 存储设备最新监控指标：经 deviceTenant 反查租户路由。
// deviceID 未知（无租户索引）时尝试用 metrics.TenantID 兜底（agent 首次上报时索引可能未建）。
func (m *MultiSchemaStore) StoreDeviceMetrics(deviceID string, metrics *proto.DeviceMetrics) {
	if deviceID == "" || metrics == nil {
		return
	}
	tenant := m.lookupDeviceTenant(deviceID)
	if tenant == "" {
		return // 未知设备，丢弃（避免误写到 default schema）
	}
	s, err := m.storeFor(tenant)
	if err != nil {
		return
	}
	s.StoreDeviceMetrics(deviceID, metrics)
}

// DeviceMetrics 返回设备最新监控指标：经 deviceTenant 反查租户路由。
func (m *MultiSchemaStore) DeviceMetrics(deviceID string) *proto.DeviceMetrics {
	tenant := m.lookupDeviceTenant(deviceID)
	if tenant == "" {
		return nil
	}
	s, err := m.storeFor(tenant)
	if err != nil {
		return nil
	}
	return s.DeviceMetrics(deviceID)
}

// DeviceMetricsHistory 返回设备监控指标历史时序（环形缓冲查询）：
// 经 deviceTenant 反查租户路由后转发到子 store。
func (m *MultiSchemaStore) DeviceMetricsHistory(deviceID string, since time.Time) []proto.DeviceMetrics {
	tenant := m.lookupDeviceTenant(deviceID)
	if tenant == "" {
		return nil
	}
	s, err := m.storeFor(tenant)
	if err != nil {
		return nil
	}
	return s.DeviceMetricsHistory(deviceID, since)
}

// ============================================================================
// TaskStore 实现（12 方法）
// ============================================================================

// GetTasks 返回指定 agent 的待执行任务：经 agentTenant 反查租户路由。
func (m *MultiSchemaStore) GetTasks(agentID string) []*proto.Task {
	tenant := m.lookupAgentTenant(agentID)
	if tenant == "" {
		return nil
	}
	s, err := m.storeFor(tenant)
	if err != nil {
		return nil
	}
	return s.GetTasks(agentID)
}

// TasksByParent 返回指定 parent_id 的全部任务：经 taskTenant 反查租户路由。
func (m *MultiSchemaStore) TasksByParent(parentID string) []*proto.Task {
	tenant := m.lookupTaskTenant(parentID)
	if tenant == "" {
		return nil
	}
	s, err := m.storeFor(tenant)
	if err != nil {
		return nil
	}
	return s.TasksByParent(parentID)
}

// ClaimTask 原子领取任务：经 agentTenant 反查租户路由，领取成功后填充 task 反查索引。
func (m *MultiSchemaStore) ClaimTask(agentID string) *proto.Task {
	tenant := m.lookupAgentTenant(agentID)
	if tenant == "" {
		return nil
	}
	s, err := m.storeFor(tenant)
	if err != nil {
		return nil
	}
	t := s.ClaimTask(agentID)
	if t != nil {
		m.setTaskTenant(t.TaskID, tenant)
	}
	return t
}

// CreateTask 下发任务：从 t.TenantID 路由，并填充 task 反查索引。
// 注意：setTaskTenant 必须在 s.CreateTask 之后调用，因为 store 会分配 TaskID（t.TaskID 入参可能为空）。
func (m *MultiSchemaStore) CreateTask(t *proto.Task) *proto.Task {
	s, err := m.storeFor(t.TenantID)
	if err != nil {
		log.Printf("[multi-schema] CreateTask 路由失败 tenant=%q: %v", t.TenantID, err)
		return t
	}
	ret := s.CreateTask(t)
	if ret != nil && ret.TaskID != "" {
		m.setTaskTenant(ret.TaskID, t.TenantID)
	}
	return ret
}

// SubmitResult 接收 agent 上报结果：经 taskTenant 反查租户路由（兜底用 agentTenant）。
func (m *MultiSchemaStore) SubmitResult(res *proto.TaskResult) {
	tenant := m.lookupTaskTenant(res.TaskID)
	if tenant == "" {
		tenant = m.lookupAgentTenant(res.AgentID)
	}
	if tenant == "" {
		log.Printf("[multi-schema] SubmitResult 无法定位租户 taskID=%s agentID=%s，丢弃", res.TaskID, res.AgentID)
		return
	}
	s, err := m.storeFor(tenant)
	if err != nil {
		return
	}
	s.SubmitResult(res)
}

// AllTasks 返回全部任务：直接用 tenantID 路由。
func (m *MultiSchemaStore) AllTasks(tenantID string) []*proto.Task {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.AllTasks(tenantID)
}

// TaskByID 按 taskID 返回单条任务：经 taskTenant 反查租户路由后直查。
func (m *MultiSchemaStore) TaskByID(taskID string) *proto.Task {
	tenant := m.lookupTaskTenant(taskID)
	if tenant == "" {
		return nil
	}
	s, err := m.storeFor(tenant)
	if err != nil {
		return nil
	}
	return s.TaskByID(taskID)
}

// TaskResult 按 taskID 返回单条结果：经 taskTenant 反查租户路由。
func (m *MultiSchemaStore) TaskResult(taskID string) *proto.TaskResult {
	tenant := m.lookupTaskTenant(taskID)
	if tenant == "" {
		return nil
	}
	s, err := m.storeFor(tenant)
	if err != nil {
		return nil
	}
	return s.TaskResult(taskID)
}

// CancelTask 取消任务：直接用 tenantID 路由。
func (m *MultiSchemaStore) CancelTask(id, tenantID string) bool {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return false
	}
	return s.CancelTask(id, tenantID)
}

// PendingDepth 返回 pending 任务总数：遍历所有 schema 求和。
func (m *MultiSchemaStore) PendingDepth() int {
	total := 0
	for _, s := range m.allStores() {
		total += s.PendingDepth()
	}
	return total
}

// ReclaimStaleTasks 复位超期 running 任务：遍历所有 schema 求和（leader 周期执行）。
func (m *MultiSchemaStore) ReclaimStaleTasks(maxAge time.Duration) int {
	total := 0
	for _, s := range m.allStores() {
		total += s.ReclaimStaleTasks(maxAge)
	}
	return total
}

// CancelledTaskIDs 返回 cancelled 任务 ID：经 agentTenant 反查租户路由。
func (m *MultiSchemaStore) CancelledTaskIDs(agentID string) []string {
	tenant := m.lookupAgentTenant(agentID)
	if tenant == "" {
		return nil
	}
	s, err := m.storeFor(tenant)
	if err != nil {
		return nil
	}
	return s.CancelledTaskIDs(agentID)
}

// FireDueSchedules 评估定时模板并派生实例：遍历所有 schema 求和（leader 周期执行）。
func (m *MultiSchemaStore) FireDueSchedules(now time.Time) int {
	total := 0
	for _, s := range m.allStores() {
		total += s.FireDueSchedules(now)
	}
	return total
}

// ============================================================================
// AlertStore 实现（5 方法）
// ============================================================================

// Alerts 返回活跃告警：直接用 tenantID 路由。
func (m *MultiSchemaStore) Alerts(tenantID string) []*proto.Alert {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.Alerts(tenantID)
}

// AddAlert 记录告警：从 a.TenantID 路由。
func (m *MultiSchemaStore) AddAlert(a *proto.Alert) {
	if a == nil {
		return
	}
	s, err := m.storeFor(a.TenantID)
	if err != nil {
		log.Printf("[multi-schema] AddAlert 路由失败 tenant=%q: %v", a.TenantID, err)
		return
	}
	s.AddAlert(a)
}

// Alert 按 alertID 返回单条告警：遍历所有 schema 查找，任一找到即返回。
func (m *MultiSchemaStore) Alert(id string) *proto.Alert {
	for _, s := range m.allStores() {
		if a := s.Alert(id); a != nil {
			return a
		}
	}
	return nil
}

// AckAlert 确认告警：直接用 tenantID 路由。
func (m *MultiSchemaStore) AckAlert(id, tenantID, by string) bool {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return false
	}
	return s.AckAlert(id, tenantID, by)
}

// SilenceAlert 静默告警：直接用 tenantID 路由。
func (m *MultiSchemaStore) SilenceAlert(id, tenantID, by string, until time.Time, comment string) bool {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return false
	}
	return s.SilenceAlert(id, tenantID, by, until, comment)
}

// ============================================================================
// QuotaStore 实现（多租户资源配额，2 方法）
// ============================================================================

// GetQuota 返回租户配额配置：直接用 tenantID 路由。
func (m *MultiSchemaStore) GetQuota(tenantID string) (*QuotaConfig, error) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, nil
	}
	return s.GetQuota(tenantID)
}

// SetQuota 设置或更新租户配额：直接用 tenantID 路由。
func (m *MultiSchemaStore) SetQuota(tenantID string, cfg *QuotaConfig) error {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return err
	}
	return s.SetQuota(tenantID, cfg)
}

// ============================================================================
// AuditStore 实现（3 方法）
// ============================================================================

// Audit 记录审计事件：从 e.TenantID 路由。
func (m *MultiSchemaStore) Audit(e *proto.AuditEvent) {
	if e == nil {
		return
	}
	s, err := m.storeFor(e.TenantID)
	if err != nil {
		// 审计留痕不应因路由失败而丢弃，降级到所有 schema 各记一份（确保留痕）。
		log.Printf("[multi-schema] Audit 路由失败 tenant=%q，降级广播: %v", e.TenantID, err)
		for _, s := range m.allStores() {
			s.Audit(e)
		}
		return
	}
	s.Audit(e)
}

// Audits 返回审计事件：遍历所有 schema 合并（按时间倒序排列）。
func (m *MultiSchemaStore) Audits() []*proto.AuditEvent {
	var out []*proto.AuditEvent
	for _, s := range m.allStores() {
		out = append(out, s.Audits()...)
	}
	// 按时间倒序排列（跨 schema 合并后重排）。
	sortAuditsDesc(out)
	return out
}

// QueryAudits 按条件过滤审计事件：tenant 非空时直接路由；为空时遍历所有 schema 合并。
func (m *MultiSchemaStore) QueryAudits(tenant, action string, since, until time.Time, limit int) []*proto.AuditEvent {
	if tenant != "" {
		s, err := m.storeFor(tenant)
		if err != nil {
			return nil
		}
		return s.QueryAudits(tenant, action, since, until, limit)
	}
	// tenant 为空：跨 schema 合并。
	var out []*proto.AuditEvent
	for _, s := range m.allStores() {
		out = append(out, s.QueryAudits("", action, since, until, 0)...)
	}
	sortAuditsDesc(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// sortAuditsDesc 按时间倒序排列审计事件（跨 schema 合并后重排）。
func sortAuditsDesc(out []*proto.AuditEvent) {
	// 简单插入排序（审计量通常不大，避免引入 sort 包的额外依赖；稳定且够用）。
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].CreatedAt.After(out[j-1].CreatedAt) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
}

// ============================================================================
// TokenStore 实现（4 方法）
// ============================================================================

// Provision 签发 install token：直接用 tenantID 路由，成功后填充 device 反查索引。
func (m *MultiSchemaStore) Provision(deviceID, host, tenantID string) (token, bootstrap string, err error) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return "", "", err
	}
	tok, boot, e := s.Provision(deviceID, host, tenantID)
	if e == nil {
		m.setDeviceTenant(deviceID, tenantID)
	}
	return tok, boot, e
}

// IssueToken 生成 install token：直接用 tenantID 路由。
func (m *MultiSchemaStore) IssueToken(deviceID, tenantID string, ttl time.Duration) (token string, err error) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return "", err
	}
	return s.IssueToken(deviceID, tenantID, ttl)
}

// ConsumeToken 校验并消费 token：遍历所有 schema 尝试，任一成功即返回。
// token 是全局唯一的（HMAC 签名含 nonce），只会在一个 schema 上成功。
func (m *MultiSchemaStore) ConsumeToken(token string) (deviceID, tenantID string, ok bool) {
	for _, s := range m.allStores() {
		if d, t, found := s.ConsumeToken(token); found {
			return d, t, true
		}
	}
	return "", "", false
}

// CleanupTokens 清理过期 token：遍历所有 schema 求和（leader 周期执行）。
func (m *MultiSchemaStore) CleanupTokens(batch int) int {
	total := 0
	for _, s := range m.allStores() {
		total += s.CleanupTokens(batch)
	}
	return total
}

// ============================================================================
// LeaderStore 实现（2 方法）
// ============================================================================

// RenewLeadership 在所有 schema 上续租，任一为主即为主。
// leader 周期任务（ReclaimStaleTasks/FireDueSchedules/RetireStaleDevices/CleanupTokens）
// 遍历所有 schema 执行，故任一 schema 为主即可推动所有 schema 的周期任务。
func (m *MultiSchemaStore) RenewLeadership(ttl time.Duration) bool {
	anyLeader := false
	for _, s := range m.allStores() {
		if s.RenewLeadership(ttl) {
			anyLeader = true
		}
	}
	return anyLeader
}

// IsLeader 返回是否任一 schema 持有领导租约。
func (m *MultiSchemaStore) IsLeader() bool {
	for _, s := range m.allStores() {
		if s.IsLeader() {
			return true
		}
	}
	return false
}

// 编译期断言：MultiSchemaStore 实现 Store 接口。
var (
	_ DeviceStore         = (*MultiSchemaStore)(nil)
	_ TaskStore           = (*MultiSchemaStore)(nil)
	_ AlertStore          = (*MultiSchemaStore)(nil)
	_ AuditStore          = (*MultiSchemaStore)(nil)
	_ TokenStore          = (*MultiSchemaStore)(nil)
	_ LeaderStore         = (*MultiSchemaStore)(nil)
	_ UserStore           = (*MultiSchemaStore)(nil)
	_ RoleStore           = (*MultiSchemaStore)(nil)
	_ PermissionStore     = (*MultiSchemaStore)(nil)
	_ K8sClusterStore     = (*MultiSchemaStore)(nil)
	_ TemplateStore       = (*MultiSchemaStore)(nil)
	_ RefreshTokenStore   = (*MultiSchemaStore)(nil)
	_ SilenceStore        = (*MultiSchemaStore)(nil)
	_ NotifyChannelStore  = (*MultiSchemaStore)(nil)
	_ NotifyTemplateStore = (*MultiSchemaStore)(nil)
	_ Store               = (*MultiSchemaStore)(nil)
)

// ============================================================================
// UserStore / RoleStore / PermissionStore 实现
// ============================================================================
//
// 多租户 schema 隔离下用户中心的路由策略：
//   - 用户/角色/权限为全局资源（跨租户共享），不按租户隔离；
//   - 采用"任意 schema 即可读写"策略：选第一个已创建的 schema 转发，
//     若尚无 schema 则回退到内置全局 store（惰性创建一个 "global" schema）。
//   - 这与 device/task/alert 等租户隔离资源不同：用户中心是平台级管理，
//     admin/operator/viewer 等预定义用户需跨租户可见。
//
// 注意：当前实现为简化版——多 schema 部署下用户中心路由到任一 schema，
// 不保证跨 schema 的用户/角色数据一致性（生产多 schema 部署需另行设计全局 schema）。
// 单 schema（memory 或单 mysql）部署下行为完全正确。

// globalStore 返回用于用户中心的全局 store（任一已创建 schema 或惰性创建 "global" schema）。
// 用户/角色/权限为平台级资源，不按租户隔离，故路由到任一 schema 即可。
func (m *MultiSchemaStore) globalStore() (Store, error) {
	m.mu.RLock()
	for _, s := range m.stores {
		m.mu.RUnlock()
		return s, nil
	}
	m.mu.RUnlock()
	// 尚无 schema：惰性创建一个 "global" schema 专用于用户中心。
	return m.storeFor("global")
}

// GetUser 按 ID 返回单用户（路由到全局 store）。
func (m *MultiSchemaStore) GetUser(id string) *User {
	s, err := m.globalStore()
	if err != nil {
		return nil
	}
	return s.GetUser(id)
}

// GetUserByUsername 按用户名返回单用户（路由到全局 store）。
func (m *MultiSchemaStore) GetUserByUsername(username string) *User {
	s, err := m.globalStore()
	if err != nil {
		return nil
	}
	return s.GetUserByUsername(username)
}

// ListUsers 返回全部用户（路由到全局 store）。
func (m *MultiSchemaStore) ListUsers() []*User {
	s, err := m.globalStore()
	if err != nil {
		return nil
	}
	return s.ListUsers()
}

// CreateUser 创建用户（路由到全局 store）。
func (m *MultiSchemaStore) CreateUser(u *User) *User {
	s, err := m.globalStore()
	if err != nil {
		return nil
	}
	return s.CreateUser(u)
}

// UpdateUser 更新用户（路由到全局 store）。
func (m *MultiSchemaStore) UpdateUser(u *User) bool {
	s, err := m.globalStore()
	if err != nil {
		return false
	}
	return s.UpdateUser(u)
}

// DeleteUser 删除用户（路由到全局 store）。
func (m *MultiSchemaStore) DeleteUser(id string) bool {
	s, err := m.globalStore()
	if err != nil {
		return false
	}
	return s.DeleteUser(id)
}

// ChangePassword 改密（安全债，路由到全局 store）。
func (m *MultiSchemaStore) ChangePassword(userID, newPasswordHash string) bool {
	s, err := m.globalStore()
	if err != nil {
		return false
	}
	return s.ChangePassword(userID, newPasswordHash)
}

// GetRole 按 ID 返回单角色（路由到全局 store）。
func (m *MultiSchemaStore) GetRole(id string) *Role {
	s, err := m.globalStore()
	if err != nil {
		return nil
	}
	return s.GetRole(id)
}

// ListRoles 返回全部角色（路由到全局 store）。
func (m *MultiSchemaStore) ListRoles() []*Role {
	s, err := m.globalStore()
	if err != nil {
		return nil
	}
	return s.ListRoles()
}

// CreateRole 创建角色（路由到全局 store）。
func (m *MultiSchemaStore) CreateRole(r *Role) *Role {
	s, err := m.globalStore()
	if err != nil {
		return nil
	}
	return s.CreateRole(r)
}

// UpdateRole 更新角色（路由到全局 store）。
func (m *MultiSchemaStore) UpdateRole(r *Role) bool {
	s, err := m.globalStore()
	if err != nil {
		return false
	}
	return s.UpdateRole(r)
}

// DeleteRole 删除角色（路由到全局 store）。
func (m *MultiSchemaStore) DeleteRole(id string) bool {
	s, err := m.globalStore()
	if err != nil {
		return false
	}
	return s.DeleteRole(id)
}

// ListPermissions 返回全部预定义权限（路由到全局 store）。
func (m *MultiSchemaStore) ListPermissions() []*Permission {
	s, err := m.globalStore()
	if err != nil {
		return nil
	}
	return s.ListPermissions()
}

// ============================================================================
// K8sClusterStore 实现（Phase 3，路由到全局 store）
// ============================================================================

// ListK8sClusters 返回 K8s 集群配置（路由到全局 store；tenantID 过滤）。
func (m *MultiSchemaStore) ListK8sClusters(tenantID string) []*K8sCluster {
	s, err := m.globalStore()
	if err != nil {
		return nil
	}
	return s.ListK8sClusters(tenantID)
}

// GetK8sCluster 按 ID 返回单个集群配置（路由到全局 store）。
func (m *MultiSchemaStore) GetK8sCluster(id string) *K8sCluster {
	s, err := m.globalStore()
	if err != nil {
		return nil
	}
	return s.GetK8sCluster(id)
}

// SaveK8sCluster 创建或更新集群配置（路由到全局 store），返回持久化错误。
func (m *MultiSchemaStore) SaveK8sCluster(c *K8sCluster) error {
	s, err := m.globalStore()
	if err != nil {
		return err
	}
	return s.SaveK8sCluster(c)
}

// DeleteK8sCluster 删除集群配置（路由到全局 store）。
func (m *MultiSchemaStore) DeleteK8sCluster(id string) bool {
	s, err := m.globalStore()
	if err != nil {
		return false
	}
	return s.DeleteK8sCluster(id)
}

// ============================================================================
// 任务审批：ApproveTask / RejectTask（按 tenantID 路由，与 CancelTask 同模式）
// ============================================================================

// ApproveTask 审批通过任务：直接用 tenantID 路由。
func (m *MultiSchemaStore) ApproveTask(id, tenantID, approvedBy string) bool {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return false
	}
	return s.ApproveTask(id, tenantID, approvedBy)
}

// RejectTask 驳回任务：直接用 tenantID 路由。
func (m *MultiSchemaStore) RejectTask(id, tenantID, approvedBy string) bool {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return false
	}
	return s.RejectTask(id, tenantID, approvedBy)
}

// ============================================================================
// 告警规则：CreateAlertRule / ListAlertRules / DeleteAlertRule
// ============================================================================

// CreateAlertRule 创建告警规则：从 r.TenantID 路由（与 AddAlert 同模式）。
func (m *MultiSchemaStore) CreateAlertRule(r *AlertRule) *AlertRule {
	if r == nil {
		return nil
	}
	s, err := m.storeFor(r.TenantID)
	if err != nil {
		log.Printf("[multi-schema] CreateAlertRule 路由失败 tenant=%q: %v", r.TenantID, err)
		return nil
	}
	return s.CreateAlertRule(r)
}

// ListAlertRules 返回告警规则：直接用 tenantID 路由（与 Alerts 同模式）。
func (m *MultiSchemaStore) ListAlertRules(tenantID string) []*AlertRule {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.ListAlertRules(tenantID)
}

// DeleteAlertRule 删除告警规则：遍历所有 schema 尝试删除（与 Alert 单查同模式：规则 ID 跨 schema 唯一性弱）。
func (m *MultiSchemaStore) DeleteAlertRule(id string) bool {
	for _, s := range m.allStores() {
		if s.DeleteAlertRule(id) {
			return true
		}
	}
	return false
}

// GetAlertRule 按 ID 返回单个告警规则（M2 持久化补全）。
// 遍历所有 schema 查找（与 DeleteAlertRule 同模式：规则 ID 跨 schema 唯一性弱）。
func (m *MultiSchemaStore) GetAlertRule(id string) *AlertRule {
	for _, s := range m.allStores() {
		if r := s.GetAlertRule(id); r != nil {
			return r
		}
	}
	return nil
}

// UpdateAlertRule 更新告警规则（M2 持久化补全）。
// 遍历所有 schema 尝试更新（与 DeleteAlertRule 同模式）。
func (m *MultiSchemaStore) UpdateAlertRule(r *AlertRule) bool {
	if r == nil {
		return false
	}
	for _, s := range m.allStores() {
		if s.UpdateAlertRule(r) {
			return true
		}
	}
	return false
}

// ============================================================================
// OS/中间件部署模板：TemplateStore 实现（路由到全局 store，与 K8sClusterStore 同模式）
// ============================================================================

// SaveOSTemplate 创建或更新 OS 安装模板（路由到全局 store）。
func (m *MultiSchemaStore) SaveOSTemplate(t *OSTemplate) error {
	s, err := m.globalStore()
	if err != nil {
		return err
	}
	return s.SaveOSTemplate(t)
}

// ListOSTemplates 返回 OS 安装模板（路由到全局 store；tenantID 过滤）。
func (m *MultiSchemaStore) ListOSTemplates(tenantID string) []*OSTemplate {
	s, err := m.globalStore()
	if err != nil {
		return nil
	}
	return s.ListOSTemplates(tenantID)
}

// GetOSTemplate 按 ID 返回单个 OS 安装模板（路由到全局 store）。
func (m *MultiSchemaStore) GetOSTemplate(id string) *OSTemplate {
	s, err := m.globalStore()
	if err != nil {
		return nil
	}
	return s.GetOSTemplate(id)
}

// DeleteOSTemplate 删除 OS 安装模板（路由到全局 store）。
func (m *MultiSchemaStore) DeleteOSTemplate(id string) bool {
	s, err := m.globalStore()
	if err != nil {
		return false
	}
	return s.DeleteOSTemplate(id)
}

// SaveMiddlewareTemplate 创建或更新中间件部署模板（路由到全局 store）。
func (m *MultiSchemaStore) SaveMiddlewareTemplate(t *MiddlewareTemplate) error {
	s, err := m.globalStore()
	if err != nil {
		return err
	}
	return s.SaveMiddlewareTemplate(t)
}

// ListMiddlewareTemplates 返回中间件部署模板（路由到全局 store；tenantID 过滤）。
func (m *MultiSchemaStore) ListMiddlewareTemplates(tenantID string) []*MiddlewareTemplate {
	s, err := m.globalStore()
	if err != nil {
		return nil
	}
	return s.ListMiddlewareTemplates(tenantID)
}

// GetMiddlewareTemplate 按 ID 返回单个中间件部署模板（路由到全局 store）。
func (m *MultiSchemaStore) GetMiddlewareTemplate(id string) *MiddlewareTemplate {
	s, err := m.globalStore()
	if err != nil {
		return nil
	}
	return s.GetMiddlewareTemplate(id)
}

// DeleteMiddlewareTemplate 删除中间件部署模板（路由到全局 store）。
func (m *MultiSchemaStore) DeleteMiddlewareTemplate(id string) bool {
	s, err := m.globalStore()
	if err != nil {
		return false
	}
	return s.DeleteMiddlewareTemplate(id)
}

// ============================================================================
// 刷新令牌：SaveRefreshToken / GetRefreshToken / DeleteRefreshToken
// 路由到全局 store（token_hash 为全局唯一主键，跨 schema 查询需全局视角，
// 与 K8sCluster/Template 同模式）。
// ============================================================================

// SaveRefreshToken 保存/更新 refresh token（路由到全局 store），返回持久化错误。
func (m *MultiSchemaStore) SaveRefreshToken(rt *RefreshToken) error {
	s, err := m.globalStore()
	if err != nil {
		return err
	}
	return s.SaveRefreshToken(rt)
}

// GetRefreshToken 按 token_hash 返回单个 refresh token（路由到全局 store）。
func (m *MultiSchemaStore) GetRefreshToken(tokenHash string) *RefreshToken {
	s, err := m.globalStore()
	if err != nil {
		return nil
	}
	return s.GetRefreshToken(tokenHash)
}

// DeleteRefreshToken 按 token_hash 删除 refresh token（路由到全局 store）。
func (m *MultiSchemaStore) DeleteRefreshToken(tokenHash string) bool {
	s, err := m.globalStore()
	if err != nil {
		return false
	}
	return s.DeleteRefreshToken(tokenHash)
}

// ConsumeRefreshToken 原子消费 refresh token（路由到全局 store）。
// token_hash 为全局唯一主键，跨 schema 查询需全局视角，与 Save/Get/Delete 同模式。
func (m *MultiSchemaStore) ConsumeRefreshToken(tokenHash string) (*RefreshToken, bool) {
	s, err := m.globalStore()
	if err != nil {
		return nil, false
	}
	return s.ConsumeRefreshToken(tokenHash)
}

// ============================================================================
// M2 集成：SilenceRule / NotifyChannel / NotifyTemplate
// 路由到全局 store（与 RefreshToken 同模式：ID 为全局唯一主键）。
// ============================================================================

// CreateSilence 创建静默规则（路由到全局 store）。
func (m *MultiSchemaStore) CreateSilence(sr *SilenceRule) *SilenceRule {
	s, err := m.globalStore()
	if err != nil {
		return nil
	}
	return s.CreateSilence(sr)
}

// DeleteSilence 删除静默规则（路由到全局 store）。
func (m *MultiSchemaStore) DeleteSilence(id, tenantID string) bool {
	s, err := m.globalStore()
	if err != nil {
		return false
	}
	return s.DeleteSilence(id, tenantID)
}

// ListSilences 返回静默规则（路由到全局 store）。
func (m *MultiSchemaStore) ListSilences(tenantID string) []*SilenceRule {
	s, err := m.globalStore()
	if err != nil {
		return nil
	}
	return s.ListSilences(tenantID)
}

// GetSilence 按 ID 返回单个静默规则（M2 持久化补全；路由到全局 store）。
func (m *MultiSchemaStore) GetSilence(id string) *SilenceRule {
	s, err := m.globalStore()
	if err != nil {
		return nil
	}
	return s.GetSilence(id)
}

// CreateNotifyChannel 创建通知渠道（路由到全局 store）。
func (m *MultiSchemaStore) CreateNotifyChannel(c *NotifyChannel) *NotifyChannel {
	s, err := m.globalStore()
	if err != nil {
		return nil
	}
	return s.CreateNotifyChannel(c)
}

// UpdateNotifyChannel 更新通知渠道（路由到全局 store）。
func (m *MultiSchemaStore) UpdateNotifyChannel(c *NotifyChannel) bool {
	s, err := m.globalStore()
	if err != nil {
		return false
	}
	return s.UpdateNotifyChannel(c)
}

// DeleteNotifyChannel 删除通知渠道（路由到全局 store）。
func (m *MultiSchemaStore) DeleteNotifyChannel(id, tenantID string) bool {
	s, err := m.globalStore()
	if err != nil {
		return false
	}
	return s.DeleteNotifyChannel(id, tenantID)
}

// GetNotifyChannel 按 ID 返回单个通知渠道（路由到全局 store）。
func (m *MultiSchemaStore) GetNotifyChannel(id string) *NotifyChannel {
	s, err := m.globalStore()
	if err != nil {
		return nil
	}
	return s.GetNotifyChannel(id)
}

// ListNotifyChannels 返回通知渠道（路由到全局 store）。
func (m *MultiSchemaStore) ListNotifyChannels(tenantID string) []*NotifyChannel {
	s, err := m.globalStore()
	if err != nil {
		return nil
	}
	return s.ListNotifyChannels(tenantID)
}

// CreateNotifyTemplate 创建通知模板（路由到全局 store）。
func (m *MultiSchemaStore) CreateNotifyTemplate(t *NotifyTemplate) *NotifyTemplate {
	s, err := m.globalStore()
	if err != nil {
		return nil
	}
	return s.CreateNotifyTemplate(t)
}

// UpdateNotifyTemplate 更新通知模板（路由到全局 store）。
func (m *MultiSchemaStore) UpdateNotifyTemplate(t *NotifyTemplate) bool {
	s, err := m.globalStore()
	if err != nil {
		return false
	}
	return s.UpdateNotifyTemplate(t)
}

// DeleteNotifyTemplate 删除通知模板（路由到全局 store）。
func (m *MultiSchemaStore) DeleteNotifyTemplate(id, tenantID string) bool {
	s, err := m.globalStore()
	if err != nil {
		return false
	}
	return s.DeleteNotifyTemplate(id, tenantID)
}

// GetNotifyTemplate 按 ID 返回单个通知模板（路由到全局 store）。
func (m *MultiSchemaStore) GetNotifyTemplate(id string) *NotifyTemplate {
	s, err := m.globalStore()
	if err != nil {
		return nil
	}
	return s.GetNotifyTemplate(id)
}

// ListNotifyTemplates 返回通知模板（路由到全局 store）。
func (m *MultiSchemaStore) ListNotifyTemplates(tenantID string) []*NotifyTemplate {
	s, err := m.globalStore()
	if err != nil {
		return nil
	}
	return s.ListNotifyTemplates(tenantID)
}
