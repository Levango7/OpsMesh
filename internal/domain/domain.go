// Package domain 定义 OpsMesh 的纯领域模型（DDD 分层中的 domain 层）。
// 它与 proto（gRPC/HTTP 传输层）解耦：proto 负责线上格式，domain 负责业务语义。
// 防腐层（ACL）由 mapper.go 提供，在 gRPC/HTTP 边界做 proto <-> domain 转换，
// 避免传输结构泄漏进业务逻辑（回应二次审计⑩「DDD/ACL 代码层不存在」）。
package domain

import (
	"errors"
	"fmt"
	"time"
)

// M2-1C 领域 sentinel error：状态机非法转换的精确错误。
// 调用方（handler）可经 errors.Is 精确区分，映射到不同 HTTP 状态码（404 vs 409）。
var (
	ErrTaskAlreadyDone           = errors.New("task already done, cannot cancel")
	ErrTaskAlreadyFailed         = errors.New("task already failed, cannot cancel")
	ErrTaskAlreadyCancelled      = errors.New("task already cancelled")
	ErrDeviceAlreadyProvisioning = errors.New("device already provisioning")
	ErrAlertAlreadyAcknowledged  = errors.New("alert already acknowledged")
	ErrAlertAlreadySilenced      = errors.New("alert already silenced")
	ErrAlertSilenced             = errors.New("alert silenced, acknowledge not allowed")
)

// Tenant 租户（U-04 行级隔离键）。
type Tenant struct {
	ID string
}

// Agent 已注册 agent 的领域实体。
type Agent struct {
	AgentID     string
	Hostname    string
	Segment     string
	TenantID    string
	Addr        string
	GRPCPort    int
	MetricsPort int
	Status      string
	Load        int
	LastSeen    time.Time
	// B1 自动纳管闭环：经 install token 校验后回填的候选设备 ID（不依赖 agent 自报）。
	// 非空时控制面 Register 把该「已发现候选设备」翻转 onboarded（Managed=true）。
	OnboardDeviceID string
	// 目标机基础元信息（agent 注册时上报）。
	OS   string // 操作系统：windows / linux / darwin
	Arch string // CPU 架构：amd64 / arm64
}

// Device 被纳管的网段内设备（U-02：服务部署后整段网络打通，设备自动纳管）。
// Device 是控制面对外暴露的设备模型（经防腐层从 proto 映射）。
// 显式 json tag 与外部 API 契约一致（镜像 proto 的小写键），避免默认导出大写字段名导致前端取不到值。
type Device struct {
	DeviceID     string    `json:"deviceID"`
	Segment      string    `json:"segment"`
	TenantID     string    `json:"tenantID"`
	IP           string    `json:"ip"`
	AgentID      string    `json:"agentID"`
	State        string    `json:"state"` // online / offline / discovered（B1 候选）/ provisioning（B1 推送中）
	TaskState    string    `json:"taskState"`
	Managed      bool      `json:"managed"`    // true=agent 已注册纳管；false=网段发现候选（待装 agent，B1）
	LastResult   string    `json:"lastResult"` // success / failed（B2 失败回写看板）
	LastResultAt time.Time `json:"lastResultAt"`
	Retired      bool      `json:"retired"`  // F5 设备退役
	Hostname     string    `json:"hostname"` // 主机名
	OS           string    `json:"os"`       // 操作系统：windows / linux / darwin
	Arch         string    `json:"arch"`     // CPU 架构：amd64 / arm64
}

// Task 下发给 agent 的自动化任务。
// Task 对外暴露的任务模型（显式 json tag 与外部 API 契约一致）。
type Task struct {
	TaskID    string    `json:"taskID"`
	AgentID   string    `json:"agentID"`
	TenantID  string    `json:"tenantID"`
	Type      string    `json:"type"`    // shell / service / file（见 proto.TaskType* 常量）
	Command   string    `json:"command"` // shell: 命令; service: start|stop|restart|status
	Content   string    `json:"content"` // file 类型：写入文件的内容
	Path      string    `json:"path"`    // file 类型：目标路径; service 类型：服务名（可选）
	Status    string    `json:"status"`  // pending / running / done / failed / cancelled
	ClaimedBy string    `json:"claimedBy"`
	ClaimedAt time.Time `json:"claimedAt"`
	// ClaimEpoch 任务所有权令牌（A-1 防双跑）：每次 ClaimTask 时 +1，
	// SubmitResult 校验持有者是否仍为当前 epoch，拒绝旧持有者上报防双跑。
	ClaimEpoch  int64     `json:"claimEpoch"`
	CreatedAt   time.Time `json:"createdAt"`
	RetryCount  int       `json:"retryCount"` // F2 重试累计
	MaxRetries  int       `json:"maxRetries"` // F2 重试上限
	DeadLetter  bool      `json:"deadLetter"` // F2 死信标记
	Schedule    string    `json:"schedule"`   // F4 cron 表达式（空=不调度）
	ParentID    string    `json:"parentID"`   // F4 派生实例的模板 ID
	LastFiredAt time.Time `json:"lastFiredAt"`
	DependsOn   []string  `json:"dependsOn"` // M5 作业编排占位（前置任务 ID）
}

// TaskResult agent 上报的任务执行结果（显式 json tag）。
type TaskResult struct {
	TaskID     string    `json:"taskID"`
	AgentID    string    `json:"agentID"`
	ExitCode   int       `json:"exitCode"`
	Stdout     string    `json:"stdout"`
	Stderr     string    `json:"stderr"`
	DurationMs int64     `json:"durationMs"`
	FinishedAt time.Time `json:"finishedAt"`
	// ClaimEpoch 任务所有权令牌（A-1 防双跑）：上报时携带，store 校验持有者是否仍为当前 epoch。
	ClaimEpoch int64 `json:"claimEpoch"`
}

// AuditEvent 内核产出的审计事件（U-04 等保三级：操作 100% 留痕，显式 json tag）。
type AuditEvent struct {
	TenantID  string    `json:"tenantID"`
	UserID    string    `json:"userID"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	Detail    string    `json:"detail"`
	CreatedAt time.Time `json:"createdAt"`
}

// Alert 内核产出的告警事件（M7 监控告警，业务闭环最小数据源，显式 json tag）。
//
// M2-1C DDD 实质化：补 Status/AcknowledgedBy/SilencedUntil/Comment/UpdatedAt 状态字段，
// 使 Alert 成为富领域实体，承载 Acknowledge/Silence/IsExpired 状态机行为（见 behaviour.go）。
type Alert struct {
	AlertID   string    `json:"alertID"`
	TenantID  string    `json:"tenantID"`
	DeviceID  string    `json:"deviceID"`
	AgentID   string    `json:"agentID"`
	Severity  string    `json:"severity"` // warning / critical
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
	// 处理状态（M7 ack/silence）：未处理=firing；确认=acknowledged；静默=silenced。
	Status         string    `json:"status"`
	AcknowledgedBy string    `json:"acknowledgedBy"` // 确认人
	SilencedUntil  time.Time `json:"silencedUntil"`  // 静默截止时间
	Comment        string    `json:"comment"`        // 处理备注
	UpdatedAt      time.Time `json:"updatedAt"`      // 最近一次状态变更时间
}

// =============================================================================
// M2-1C DDD 实质化：领域实体业务行为（状态机 / 重试判定 / 纳管翻转 / 规则匹配）
//
// 此前 domain 仅是数据结构 + mapper，业务逻辑散落在 handler 和 store。现把不变的业务规则
// 下沉到领域实体，handler 退化为薄编排层（解析请求 → 调领域方法 → 写响应）。
// 状态机方法返回 error 而非 bool，使非法状态转换在调用方可精确区分（而非笼统的 false）。
// =============================================================================

// --- Task 状态常量 ---
const (
	TaskStatusPending   = "pending"
	TaskStatusRunning   = "running"
	TaskStatusDone      = "done"
	TaskStatusFailed    = "failed"
	TaskStatusCancelled = "cancelled"
)

// --- Device 状态常量 ---
const (
	DeviceStateOnline       = "online"
	DeviceStateOffline      = "offline"
	DeviceStateDiscovered   = "discovered"
	DeviceStateProvisioning = "provisioning"
)

// --- Alert 状态常量（与 proto.AlertStatus* 对齐） ---
const (
	AlertStatusFiring       = "firing"
	AlertStatusAcknowledged = "acknowledged"
	AlertStatusSilenced     = "silenced"
)

// ---------------------------------------------------------------------------
// Task 业务行为
// ---------------------------------------------------------------------------

// Cancel 任务取消状态机：pending/running → cancelled。
// 终态（done/failed/cancelled）返回 error，调用方据此返回 404/409。
// 幂等：已 cancelled 重复调用返回 error（而非静默成功），便于上层精确报错。
func (t *Task) Cancel() error {
	switch t.Status {
	case TaskStatusPending, TaskStatusRunning, "": // 空串按 pending 处理（兼容旧数据）
		t.Status = TaskStatusCancelled
		return nil
	case TaskStatusDone:
		return ErrTaskAlreadyDone
	case TaskStatusFailed:
		return ErrTaskAlreadyFailed
	case TaskStatusCancelled:
		return ErrTaskAlreadyCancelled
	default:
		return fmt.Errorf("unknown task status %q, cannot cancel", t.Status)
	}
}

// CanRetry 重试判定：仅 failed 且 RetryCount < maxRetries 时可重试。
// maxRetries <= 0 表示不允许重试（一次失败即死信）。
func (t *Task) CanRetry(maxRetries int) bool {
	if t.Status != TaskStatusFailed {
		return false
	}
	if maxRetries <= 0 {
		return false
	}
	return t.RetryCount < maxRetries
}

// IsLeaseExpired 租约超时判定：running 且距 ClaimedAt 超过 maxAge。
// 非 running 或 ClaimedAt 零值返回 false（未领取的任务无租约概念）。
// maxAge <= 0 永不超时（关闭租约回收）。
func (t *Task) IsLeaseExpired(maxAge time.Duration) bool {
	if t.Status != TaskStatusRunning || t.ClaimedAt.IsZero() {
		return false
	}
	if maxAge <= 0 {
		return false
	}
	return time.Since(t.ClaimedAt) > maxAge
}

// MarkDead 标记死信：重试耗尽后置 DeadLetter=true 并把状态翻为 failed。
// 幂等：已 DeadLetter 的任务重复调用无副作用（状态保持 failed）。
func (t *Task) MarkDead() {
	t.DeadLetter = true
	t.Status = TaskStatusFailed
}

// ---------------------------------------------------------------------------
// Device 业务行为
// ---------------------------------------------------------------------------

// CanRetire 退役资格判定：未退役且（离线 或 最近结果超龄）。
// maxAge <= 0 时不按超龄判定，仅离线设备可退役（手动退役场景）。
// 已 retired 返回 false（幂等拒绝，避免重复归档）。
func (d *Device) CanRetire(maxAge time.Duration) bool {
	if d.Retired {
		return false
	}
	if d.State == DeviceStateOffline {
		return true
	}
	if maxAge > 0 && !d.LastResultAt.IsZero() && time.Since(d.LastResultAt) > maxAge {
		return true
	}
	return false
}

// TransitionToProvisioning 纳管状态翻转：仅 discovered 候选可翻 provisioning（B1 推送中）。
// 已 managed（online/offline）或已 provisioning 返回 error（幂等拒绝）。
// 调用方据此区分"首次推送"与"重复推送"，避免重复签发 install token。
func (d *Device) TransitionToProvisioning() error {
	switch d.State {
	case DeviceStateDiscovered, "": // 空串按 discovered 处理（兼容旧数据）
		d.State = DeviceStateProvisioning
		return nil
	case DeviceStateProvisioning:
		return ErrDeviceAlreadyProvisioning
	case DeviceStateOnline, DeviceStateOffline:
		return fmt.Errorf("device already managed (state=%s)", d.State)
	default:
		return fmt.Errorf("unknown device state %q, cannot transition to provisioning", d.State)
	}
}

// IsOrphan 孤儿设备判定：网段发现候选（!Managed）且无 agent 绑定。
// 这类设备需经 provision 推送 agent 才能真正纳管（B1 自动纳管闭环的输入）。
func (d *Device) IsOrphan() bool {
	return !d.Managed && d.AgentID == ""
}

// ---------------------------------------------------------------------------
// Alert 业务行为
// ---------------------------------------------------------------------------

// Acknowledge 告警确认状态机：仅 firing 可确认。
// acknowledged/silenced 返回 error（幂等拒绝）；空 Status 视为 firing（兼容旧数据）。
// by 为确认人（来自网关注入的 X-User-Id）。
func (a *Alert) Acknowledge(by string) error {
	st := a.Status
	if st == "" {
		st = AlertStatusFiring
	}
	switch st {
	case AlertStatusFiring:
		a.Status = AlertStatusAcknowledged
		a.AcknowledgedBy = by
		a.UpdatedAt = time.Now()
		return nil
	case AlertStatusAcknowledged:
		return ErrAlertAlreadyAcknowledged
	case AlertStatusSilenced:
		return ErrAlertSilenced
	default:
		return fmt.Errorf("unknown alert status %q, cannot acknowledge", st)
	}
}

// Silence 告警静默状态机：firing/acknowledged 可静默；已 silenced 返回 error。
// until 为静默截止时间；comment 为处理备注。空 until 表示立即过期（等价于 ack）。
func (a *Alert) Silence(until time.Time, comment string) error {
	st := a.Status
	if st == "" {
		st = AlertStatusFiring
	}
	switch st {
	case AlertStatusFiring, AlertStatusAcknowledged:
		a.Status = AlertStatusSilenced
		a.SilencedUntil = until
		a.Comment = comment
		a.UpdatedAt = time.Now()
		return nil
	case AlertStatusSilenced:
		return ErrAlertAlreadySilenced
	default:
		return fmt.Errorf("unknown alert status %q, cannot silence", st)
	}
}

// IsExpired 静默过期判定：silenced 且当前时间已过 SilencedUntil。
// 非 silenced 或 SilencedUntil 零值返回 false。
// 用于 notifyLoop 决定是否把已过期静默告警重新纳入推送。
func (a *Alert) IsExpired() bool {
	if a.Status != AlertStatusSilenced || a.SilencedUntil.IsZero() {
		return false
	}
	return time.Now().After(a.SilencedUntil)
}
