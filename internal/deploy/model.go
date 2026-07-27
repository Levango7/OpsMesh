// Package deploy 实现 M3 部署中心（BD-03）：服务部署任务的生命周期管理
// （创建 → 执行 → 成功/失败/回滚），执行动作经 Dispatcher 防腐接口派发到
// M4 自动化执行引擎（复用底层任务通道）。双后端（Memory / SQL，U-04 数据本地化）。
package deploy

import "time"

// 部署状态常量（对齐 系统设计 3.2.M3 状态机）。
const (
	StatusCreated    = "created"    // 已创建，待执行
	StatusRunning    = "running"    // 执行中（底层任务已派发）
	StatusSuccess    = "success"    // 同步完成
	StatusFailed     = "failed"     // 失败（可重试）
	StatusRolledBack = "rolledback" // 已回滚
)

// 部署类型常量（script/file/k8s，对齐设计接口清单）。
const (
	TypeScript = "script" // 脚本部署：RepoURL 指向脚本/仓库，派发 shell 任务
	TypeFile   = "file"   // 文件部署：Content 写入 Path（派发 file 任务）
	TypeK8s    = "k8s"    // K8s GitOps：RepoURL 指向 manifest，派发 shell apply（MVP 占位）
)

// DeployTask 是 M3 部署中心的部署任务（对齐 系统设计 3.2.M3 结构定义，并补充执行所需字段）。
type DeployTask struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`       // script / file / k8s
	RepoURL   string    `json:"repo_url"`   // Git(E-03) manifest 仓库地址（script/k8s 用）
	Content   string    `json:"content"`    // file 类型写入内容 / k8s manifest 内联（可选）
	Path      string    `json:"path"`       // file 类型目标路径（可选）
	TargetIDs string    `json:"target_ids"` // 目标设备 ID（逗号/空格分隔）
	// TaskIDs 为内部字段：执行时派发的底层任务 ID（逗号分隔），供 reconcile 判定终态。
	TaskIDs  string `json:"task_ids,omitempty"`
	TenantID  string    `json:"tenant_id"`
	CreatedBy string    `json:"created_by"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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
	return nil
}
