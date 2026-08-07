// Package deploy 实现 M3 部署中心（BD-03）：服务部署任务的生命周期管理
// （创建 → 执行 → 成功/失败/回滚），执行动作经 Dispatcher 防腐接口派发到
// M4 自动化执行引擎（复用底层任务通道）。双后端（Memory / SQL，U-04 数据本地化）。
package deploy

import (
	"strings"
	"time"
)

// 部署状态常量（对齐 系统设计 3.2.M3 状态机，扩展灰度发布状态）。
const (
	StatusCreated    = "created"    // 已创建，待执行
	StatusRunning    = "running"    // 执行中（底层任务已派发，全量目标）
	StatusCanary     = "canary"     // 金丝雀阶段：仅按 CanaryWeight 分流部分目标执行
	StatusPromoting  = "promoting"  // 灰度晋级中：从 canary 推进到全量目标
	StatusGated      = "gated"      // 发布门禁已通过（健康/成功率达标，可晋级）
	StatusSuccess    = "success"    // 同步完成
	StatusFailed     = "failed"     // 失败（可重试；若 AutoRollback=true 则触发自动回滚）
	StatusRolledBack = "rolledback" // 已回滚
)

// 部署类型常量（script/file/k8s，对齐设计接口清单）。
const (
	TypeScript = "script" // 脚本部署：RepoURL 指向脚本/仓库，派发 shell 任务
	TypeFile   = "file"   // 文件部署：Content 写入 Path（派发 file 任务）
	TypeK8s    = "k8s"    // K8s GitOps：RepoURL 指向 manifest，派发 shell apply（MVP 占位）
)

// 部署策略常量（灰度发布，对齐 系统设计 3.2.M3 灰度发布扩展）。
const (
	StrategyRolling   = "rolling"   // 滚动发布（默认）：全量目标一次性派发，向后兼容
	StrategyCanary    = "canary"    // 金丝雀：按 CanaryWeight 比例先派发部分目标，门禁通过后再晋级全量
	StrategyBlueGreen = "bluegreen" // 蓝绿：先派发 inactive 一组（blue/green），切换流量后再下线旧组
)

// 默认发布门禁阈值（未配置 Gate 时采用，保证灰度阶段默认有保护）。
const (
	defaultGateSuccessRate = 100.0 // 默认要求底层任务 100% 成功才放行晋级
	defaultGateMaxFailRate = 0.0   // 默认失败率上限 0%（任一失败即触发门禁不通过）
	defaultCanaryWeight    = 10    // 默认金丝雀流量比例 10%（未设 CanaryWeight 时回退）
)

// canaryWeightBounds CanaryWeight 取值范围 [0, 100]。
const (
	canaryWeightMin = 0
	canaryWeightMax = 100
)

// repoURLUnsafeChars 是禁止出现在 RepoURL 中的 shell 元字符（task 87 命令注入防护）。
// RepoURL 在 Execute 时被原样作为 shell 任务的 Command 下发给 agent（以 sh -c 执行），
// 值中若含以下字符即可拼接/截断命令造成目标机 RCE，故一律拒绝。
const repoURLUnsafeChars = " \t\n\r;&|`$\"'<>(){}[]*?!\\#~"

// DeployTask 是 M3 部署中心的部署任务（对齐 系统设计 3.2.M3 结构定义，并补充执行所需字段）。
type DeployTask struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`       // script / file / k8s
	RepoURL   string `json:"repo_url"`   // Git(E-03) manifest 仓库地址（script/k8s 用）
	Content   string `json:"content"`    // file 类型写入内容 / k8s manifest 内联（可选）
	Path      string `json:"path"`       // file 类型目标路径（可选）
	TargetIDs string `json:"target_ids"` // 目标设备 ID（逗号/空格分隔）
	// TaskIDs 为内部字段：执行时派发的底层任务 ID（逗号分隔），供 reconcile 判定终态。
	TaskIDs   string    `json:"task_ids,omitempty"`
	TenantID  string    `json:"tenant_id"`
	CreatedBy string    `json:"created_by"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// 灰度发布扩展（P3 灰度发布策略：蓝绿/金丝雀 + 发布门禁 + 自动回滚）。
	// Strategy 为空时按 StrategyRolling 处理（向后兼容旧部署）。
	Strategy     string      `json:"strategy,omitempty"`      // rolling / canary / bluegreen
	CanaryWeight int         `json:"canary_weight,omitempty"` // 金丝雀流量比例 [0,100]，0=不分流，100=全量
	AutoRollback bool        `json:"auto_rollback,omitempty"` // 失败时是否自动回滚（canary/bluegreen 推荐 true）
	Gate         *GateConfig `json:"gate,omitempty"`          // 发布门禁（成功率/健康检查阈值），nil=用默认门禁
	// CanaryTargets 为灰度阶段已派发的目标设备 ID（逗号分隔），晋级时派发剩余目标。
	CanaryTargets string `json:"canary_targets,omitempty"`
	// StableTargets 为蓝绿部署中稳定（旧）版本目标设备 ID（逗号分隔），切换流量后下线。
	StableTargets string `json:"stable_targets,omitempty"`
}

// GateConfig 发布门禁配置：灰度阶段底层任务终态须满足以下阈值才放行晋级，
// 否则视为门禁不通过——失败率超阈值且 AutoRollback=true 时触发自动回滚。
type GateConfig struct {
	// SuccessRate 要求底层任务成功率下限（百分比，[0,100]），默认 100。
	SuccessRate float64 `json:"success_rate,omitempty"`
	// MaxFailRate 允许的失败率上限（百分比，[0,100]），默认 0。与 SuccessRate 互补，二选一即可。
	MaxFailRate float64 `json:"max_fail_rate,omitempty"`
	// MinSuccessCount 至少多少个底层任务成功才放行（绝对值，0=不约束）。
	MinSuccessCount int `json:"min_success_count,omitempty"`
	// HealthCheckURL 可选的健康检查 URL（HTTP GET 期望 2xx 才算通过），空=不做健康检查。
	HealthCheckURL string `json:"health_check_url,omitempty"`
}

// ResolvedGate 返回生效的门禁配置（nil 时回退默认值），调用方据此判定灰度阶段是否放行。
func (d *DeployTask) ResolvedGate() GateConfig {
	if d.Gate != nil {
		g := *d.Gate
		if g.SuccessRate == 0 && g.MaxFailRate == 0 && g.MinSuccessCount == 0 {
			// 用户显式给了空 Gate：仍回退默认，避免无门禁裸奔。
			g.SuccessRate = defaultGateSuccessRate
		}
		return g
	}
	return GateConfig{SuccessRate: defaultGateSuccessRate, MaxFailRate: defaultGateMaxFailRate}
}

// EffectiveStrategy 返回生效的部署策略（空串回退 rolling，向后兼容）。
func (d *DeployTask) EffectiveStrategy() string {
	if d.Strategy == "" {
		return StrategyRolling
	}
	return d.Strategy
}

// EffectiveCanaryWeight 返回生效的金丝雀流量比例（0 或越界回退 defaultCanaryWeight）。
func (d *DeployTask) EffectiveCanaryWeight() int {
	if d.CanaryWeight <= 0 || d.CanaryWeight > canaryWeightMax {
		return defaultCanaryWeight
	}
	return d.CanaryWeight
}

// Valid 校验部署任务关键字段（创建时调用）。
func (d *DeployTask) Valid() error {
	if d.Name == "" {
		return errInvalid("name required")
	}
	switch d.Type {
	case TypeScript, TypeFile, TypeK8s:
	default:
		return errInvalid("type must be script/file/k8s")
	}
	if d.TargetIDs == "" {
		return errInvalid("target_ids required")
	}
	// task 87：RepoURL 非空时校验为安全的仓库地址，防命令注入。
	if d.RepoURL != "" {
		if err := validateRepoURL(d.RepoURL); err != nil {
			return err
		}
	}
	// 灰度发布策略校验。
	switch d.Strategy {
	case "", StrategyRolling, StrategyCanary, StrategyBlueGreen:
		// 空串视为 rolling（向后兼容），合法。
	default:
		return errInvalid("strategy must be rolling/canary/bluegreen")
	}
	// CanaryWeight 越界校验（0 合法：表示不分流，等价 rolling；>100 非法）。
	if d.CanaryWeight < canaryWeightMin || d.CanaryWeight > canaryWeightMax {
		return errInvalid("canary_weight must be in [0,100]")
	}
	// Gate 阈值合法性校验（若显式配置）。
	if d.Gate != nil {
		if d.Gate.SuccessRate < 0 || d.Gate.SuccessRate > 100 {
			return errInvalid("gate.success_rate must be in [0,100]")
		}
		if d.Gate.MaxFailRate < 0 || d.Gate.MaxFailRate > 100 {
			return errInvalid("gate.max_fail_rate must be in [0,100]")
		}
	}
	return nil
}

// validateRepoURL 校验 RepoURL 为安全的仓库地址（task 87 命令注入防护）。
// 要求不含 shell 元字符，且以 http(s):// / git:// / ssh:// / git@ / 绝对路径 开头。
func validateRepoURL(u string) error {
	if strings.ContainsAny(u, repoURLUnsafeChars) {
		return errInvalid("repo_url contains shell metacharacters and is rejected for safety")
	}
	if !(strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") ||
		strings.HasPrefix(u, "git://") || strings.HasPrefix(u, "ssh://") ||
		strings.HasPrefix(u, "git@") || strings.HasPrefix(u, "/")) {
		return errInvalid("repo_url must start with http(s)://, git://, ssh://, git@, or /")
	}
	return nil
}
