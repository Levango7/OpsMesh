// backup.go 实现 opsmesh backup / restore 子命令的数据导出/导入逻辑。
//
// 设计目标：
//   - backup：把控制面 Store 中的关键业务数据（设备/任务/告警/规则/用户/角色/权限/审计/配置）
//     导出为 JSON 或 SQL dump 文件，供离线备份、迁移、灾备恢复使用；
//   - restore：从备份文件导入数据回 Store，支持 --dry-run（只校验不落库）与 --overwrite（覆盖已存在）；
//   - 与控制面 server 共用同一 Store 抽象，不直接耦合 HTTP/gRPC，可独立 CLI 调用。
//
// 数据范围：
//   - devices / agents：全量（纳管设备与注册 agent）；
//   - tasks / alerts：默认最近 7 天（--task-window-days / --alert-window-days 可调）；
//   - alert-rules / users / roles / permissions：全量（配置类数据，量小且关键）；
//   - audits：默认不导出（量大且敏感），--include-audits 时导出最近 30 天；
//   - config：默认不导出（含密钥/DSN 等敏感字段），--include-config 时导出。
//
// 安全注意：
//   - config 含 MySQLDSN/JWTSecret/ProvisionSecret/EncryptionKey 等敏感字段，
//     导出时明文落盘，备份文件须按密钥管理（加密存储/受限访问）；
//   - users.PasswordHash 为 bcrypt 哈希（json:"-" 不序列化），导出/导入均不携带密码哈希，
//     restore 后用户密码需重置（导入用户记录但密码为空，登录会失败 → 须管理员重置）。
package controlplane

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"opsmesh/internal/config"
	"opsmesh/internal/events"
	"opsmesh/internal/logx"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
	"opsmesh/internal/version"
)

// NewStoreForCLI 为 backup/restore 等 CLI 子命令初始化 Store（复用控制面 selectStore 逻辑）。
//
// 与 NewServer 不同：不启动 HTTP/gRPC/metrics，仅初始化持久化后端后返回，
// 供短生命周期 CLI 命令（backup/restore）直接读写 Store。
//
// 失败时返回 error（不静默回退 memory）：CLI 子命令应显式失败而非静默回退，
// 避免 backup 误读空 memory store 产出空备份。
func NewStoreForCLI(cfg *config.Config) (store.Store, error) {
	bus := events.New(cfg.EventBus, cfg.KafkaBrokers, cfg.KafkaTopic)
	return selectStore(cfg, bus)
}

// 默认时间窗（天）。
const (
	defaultTaskWindowDays  = 7  // 任务默认导出最近 7 天
	defaultAlertWindowDays = 7  // 告警默认导出最近 7 天
	defaultAuditWindowDays = 30 // 审计默认导出最近 30 天
)

// BackupMeta 备份文件元信息（写在 BackupData.Meta，用于 restore 时校验来源/版本/时间窗）。
type BackupMeta struct {
	Version         string       `json:"version"`         // opsmesh 内核版本（version.Version）
	CreatedAt       time.Time    `json:"createdAt"`       // 备份生成时间
	Format          string       `json:"format"`          // json | sql
	IncludeAudits   bool         `json:"includeAudits"`   // 是否包含审计
	IncludeConfig   bool         `json:"includeConfig"`   // 是否包含配置
	TaskWindowDays  int          `json:"taskWindowDays"`  // 任务时间窗（天）
	AlertWindowDays int          `json:"alertWindowDays"` // 告警时间窗（天）
	AuditWindowDays int          `json:"auditWindowDays"` // 审计时间窗（天）
	Counts          BackupCounts `json:"counts"`          // 各类数据条数（供 restore 前预览/校验）
}

// BackupCounts 各类数据条数（备份生成时统计，restore 前可预览规模）。
type BackupCounts struct {
	Devices     int `json:"devices"`
	Agents      int `json:"agents"`
	Tasks       int `json:"tasks"`
	Alerts      int `json:"alerts"`
	AlertRules  int `json:"alertRules"`
	Users       int `json:"users"`
	Roles       int `json:"roles"`
	Permissions int `json:"permissions"`
	Audits      int `json:"audits"`
}

// BackupData 备份数据载体（JSON 序列化结构）。
//
// 字段语义：
//   - Meta：备份元信息（版本/时间/时间窗/条数）；
//   - Devices/Agents/Tasks/Alerts/AlertRules：业务数据；
//   - Users/Roles/Permissions：RBAC 数据（Users 不含密码哈希）；
//   - Audits：审计事件（仅 --include-audits 时填充）；
//   - Config：运行配置（仅 --include-config 时填充，含敏感字段）。
type BackupData struct {
	Meta        BackupMeta          `json:"meta"`
	Devices     []proto.DeviceInfo  `json:"devices"`
	Agents      []*proto.AgentInfo  `json:"agents"`
	Tasks       []*proto.Task       `json:"tasks"`
	Alerts      []*proto.Alert      `json:"alerts"`
	AlertRules  []*store.AlertRule  `json:"alertRules"`
	Users       []*store.User       `json:"users"`
	Roles       []*store.Role       `json:"roles"`
	Permissions []*store.Permission `json:"permissions"`
	Audits      []*proto.AuditEvent `json:"audits,omitempty"`
	Config      *config.Config      `json:"config,omitempty"`
}

// ExportOptions backup 子命令导出选项。
type ExportOptions struct {
	Format          string // json（默认） | sql
	IncludeConfig   bool   // 是否导出 config（含敏感字段）
	IncludeAudits   bool   // 是否导出审计日志
	TaskWindowDays  int    // 任务时间窗（天，默认 7）
	AlertWindowDays int    // 告警时间窗（天，默认 7）
	AuditWindowDays int    // 审计时间窗（天，默认 30）
}

// withDefaults 填充零值为默认值，返回填充后的副本（不修改原 opts）。
func (o ExportOptions) withDefaults() ExportOptions {
	if o.Format == "" {
		o.Format = "json"
	}
	if o.TaskWindowDays <= 0 {
		o.TaskWindowDays = defaultTaskWindowDays
	}
	if o.AlertWindowDays <= 0 {
		o.AlertWindowDays = defaultAlertWindowDays
	}
	if o.AuditWindowDays <= 0 {
		o.AuditWindowDays = defaultAuditWindowDays
	}
	return o
}

// ImportOptions restore 子命令导入选项。
type ImportOptions struct {
	DryRun    bool // 只校验不实际写入
	Overwrite bool // 覆盖已存在的数据（否则跳过已存在的）
}

// ImportResult 导入结果统计（restore 后返回，供 CLI 输出汇总）。
type ImportResult struct {
	Devices    int `json:"devices"`
	Agents     int `json:"agents"`
	Tasks      int `json:"tasks"`
	Alerts     int `json:"alerts"`
	AlertRules int `json:"alertRules"`
	Users      int `json:"users"`
	Roles      int `json:"roles"`
	Skipped    int `json:"skipped"` // 因已存在且 Overwrite=false 跳过的条数
}

// ExportBackup 从 Store 读取数据并写入 w（按 opts.Format 选择 JSON 或 SQL dump）。
//
// 参数：
//   - ctx：上下文（目前仅用于日志，未来可加超时/取消）；
//   - st：Store 读取源（复用控制面 selectStore 初始化结果）；
//   - cfg：运行配置（opts.IncludeConfig=true 时写入 BackupData.Config）；
//   - opts：导出选项；
//   - w：输出目标（文件/stdout）。
//
// 返回 BackupData（供 CLI 输出汇总）与 error。
func ExportBackup(ctx context.Context, st store.Store, cfg *config.Config, opts ExportOptions, w io.Writer) (*BackupData, error) {
	opts = opts.withDefaults()
	now := time.Now()

	data := &BackupData{
		Meta: BackupMeta{
			Version:         version.Version,
			CreatedAt:       now,
			Format:          opts.Format,
			IncludeAudits:   opts.IncludeAudits,
			IncludeConfig:   opts.IncludeConfig,
			TaskWindowDays:  opts.TaskWindowDays,
			AlertWindowDays: opts.AlertWindowDays,
			AuditWindowDays: opts.AuditWindowDays,
		},
	}

	// 1. 设备：全量（Snapshot 返回 segment -> []DeviceInfo，按 segment 排序展平）。
	snap := st.Snapshot("")
	devices := make([]proto.DeviceInfo, 0, len(snap)*2)
	segs := make([]string, 0, len(snap))
	for s := range snap {
		segs = append(segs, s)
	}
	sort.Strings(segs)
	for _, s := range segs {
		for _, d := range snap[s] {
			devices = append(devices, d)
		}
	}
	data.Devices = devices

	// 2. Agents：全量。
	data.Agents = st.Agents("")

	// 3. Tasks：按时间窗过滤（最近 N 天）。
	taskSince := now.AddDate(0, 0, -opts.TaskWindowDays)
	allTasks := st.AllTasks("")
	tasks := make([]*proto.Task, 0, len(allTasks))
	for _, t := range allTasks {
		if t.CreatedAt.IsZero() || !t.CreatedAt.Before(taskSince) {
			tasks = append(tasks, t)
		}
	}
	data.Tasks = tasks

	// 4. Alerts：按时间窗过滤（最近 N 天）。
	alertSince := now.AddDate(0, 0, -opts.AlertWindowDays)
	allAlerts := st.Alerts("")
	alerts := make([]*proto.Alert, 0, len(allAlerts))
	for _, a := range allAlerts {
		if a.CreatedAt.IsZero() || !a.CreatedAt.Before(alertSince) {
			alerts = append(alerts, a)
		}
	}
	data.Alerts = alerts

	// 5. AlertRules：全量。
	data.AlertRules = st.ListAlertRules("")

	// 6. Users / Roles / Permissions：全量。
	data.Users = st.ListUsers()
	data.Roles = st.ListRoles()
	data.Permissions = st.ListPermissions()

	// 7. Audits：仅 --include-audits 时导出（按时间窗过滤）。
	if opts.IncludeAudits {
		auditSince := now.AddDate(0, 0, -opts.AuditWindowDays)
		// QueryAudits(tenant, action, since, until, limit)：空 tenant/action 表示不限；limit<=0 不限。
		data.Audits = st.QueryAudits("", "", auditSince, now, 0)
	}

	// 8. Config：仅 --include-config 时导出（含敏感字段，备份文件须按密钥管理）。
	if opts.IncludeConfig && cfg != nil {
		data.Config = cfg
	}

	// 9. 统计条数写入 Meta.Counts。
	data.Meta.Counts = BackupCounts{
		Devices:     len(data.Devices),
		Agents:      len(data.Agents),
		Tasks:       len(data.Tasks),
		Alerts:      len(data.Alerts),
		AlertRules:  len(data.AlertRules),
		Users:       len(data.Users),
		Roles:       len(data.Roles),
		Permissions: len(data.Permissions),
		Audits:      len(data.Audits),
	}

	// 10. 按格式序列化输出。
	switch strings.ToLower(opts.Format) {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(data); err != nil {
			return nil, fmt.Errorf("JSON 编码失败: %w", err)
		}
	case "sql":
		if err := writeSQLDump(data, w); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("不支持的格式 %q（应为 json 或 sql）", opts.Format)
	}

	logx.Info(ctx, "backup 导出完成",
		"format", opts.Format,
		"devices", data.Meta.Counts.Devices,
		"agents", data.Meta.Counts.Agents,
		"tasks", data.Meta.Counts.Tasks,
		"alerts", data.Meta.Counts.Alerts,
		"audits", data.Meta.Counts.Audits)
	return data, nil
}

// ExportBackupFile 是 ExportBackup 的文件包装：创建/截断 output 文件后导出。
func ExportBackupFile(ctx context.Context, st store.Store, cfg *config.Config, opts ExportOptions, output string) (*BackupData, error) {
	f, err := os.Create(output)
	if err != nil {
		return nil, fmt.Errorf("创建备份文件 %q 失败: %w", output, err)
	}
	defer f.Close()
	// 用 bufio.Writer 缓冲写入，避免大备份逐行 syscall 影响性能。
	bw := bufio.NewWriter(f)
	defer bw.Flush()
	return ExportBackup(ctx, st, cfg, opts, bw)
}

// ImportBackup 从 r 读取 JSON 备份并导入 Store（按 opts 决策 dry-run/overwrite）。
//
// 仅支持 JSON 格式（SQL dump 需先用 mysql 客户端导入 DB，再走 Store 读取）。
// 导入策略：
//   - devices：UpsertDevice（按 deviceID 幂等，overwrite 无影响，已存在即更新）；
//   - agents：Register（按 agentID 幂等，已存在则跳过，overwrite 时更新心跳/状态）；
//   - tasks：CreateTask（保留原 TaskID；已存在且非 overwrite 时跳过）；
//   - alerts：AddAlert（无幂等键校验，直接追加；dry-run 时计数不写入）；
//   - alertRules：CreateAlertRule（保留原 ID；已存在且非 overwrite 时跳过）；
//   - users：CreateUser（按 username 幂等；已存在且非 overwrite 时跳过）；
//   - roles：CreateRole（按 name 幂等；已存在且非 overwrite 时跳过）。
//
// dry-run 模式：只解析 + 校验 + 统计，不调用任何 Store 写方法，返回 ImportResult 预览。
func ImportBackup(ctx context.Context, st store.Store, opts ImportOptions, r io.Reader) (*BackupData, *ImportResult, error) {
	var data BackupData
	dec := json.NewDecoder(r)
	if err := dec.Decode(&data); err != nil {
		return nil, nil, fmt.Errorf("JSON 解码失败: %w", err)
	}

	// 基本校验：Meta.Version 非空（由 ExportBackup 写入，空表示文件损坏/非本工具产出）。
	if data.Meta.Version == "" && data.Meta.CreatedAt.IsZero() {
		return nil, nil, fmt.Errorf("备份文件缺少 Meta 信息（Version/CreatedAt 均为空），可能已损坏或非 opsmesh backup 产出")
	}

	res := &ImportResult{}

	// dry-run：只统计不写入。
	if opts.DryRun {
		res.Devices = len(data.Devices)
		res.Agents = len(data.Agents)
		res.Tasks = len(data.Tasks)
		res.Alerts = len(data.Alerts)
		res.AlertRules = len(data.AlertRules)
		res.Users = len(data.Users)
		res.Roles = len(data.Roles)
		logx.Info(ctx, "backup dry-run 校验通过",
			"devices", res.Devices, "agents", res.Agents, "tasks", res.Tasks,
			"alerts", res.Alerts, "alertRules", res.AlertRules,
			"users", res.Users, "roles", res.Roles)
		return &data, res, nil
	}

	// 1. Devices：UpsertDevice 幂等。
	for _, d := range data.Devices {
		dc := d // 取副本避免取地址遍历变量复用
		st.UpsertDevice(&dc)
		res.Devices++
	}

	// 2. Agents：Register 幂等（已存在则跳过，overwrite 时 Heartbeat 更新状态）。
	for _, a := range data.Agents {
		existing := st.Agent(a.AgentID)
		if existing != nil && !opts.Overwrite {
			res.Skipped++
			continue
		}
		ac := *a
		if existing != nil {
			// overwrite：用 Heartbeat 更新在线状态/负载（保留原 secret，不重置）。
			st.Heartbeat(a.AgentID, a.Status, a.Load)
		} else {
			st.Register(&ac)
		}
		res.Agents++
	}

	// 3. Tasks：CreateTask（保留原 TaskID；已存在且非 overwrite 时跳过）。
	for _, t := range data.Tasks {
		if t.TaskID != "" {
			if existing := st.TaskByID(t.TaskID); existing != nil && !opts.Overwrite {
				res.Skipped++
				continue
			}
		}
		tc := *t
		st.CreateTask(&tc)
		res.Tasks++
	}

	// 4. Alerts：AddAlert（直接追加，无幂等校验——告警历史追加语义，重复导入会重复）。
	for _, a := range data.Alerts {
		ac := *a
		st.AddAlert(&ac)
		res.Alerts++
	}

	// 5. AlertRules：CreateAlertRule（保留原 ID；已存在且非 overwrite 时跳过）。
	for _, r := range data.AlertRules {
		// AlertStore 未提供单查方法，用 ListAlertRules 线性查找（规则量小，可接受）。
		exists := false
		for _, existing := range st.ListAlertRules("") {
			if existing.ID == r.ID {
				exists = true
				break
			}
		}
		if exists && !opts.Overwrite {
			res.Skipped++
			continue
		}
		rc := *r
		st.CreateAlertRule(&rc)
		res.AlertRules++
	}

	// 6. Users：CreateUser（按 username 幂等；已存在且非 overwrite 时跳过）。
	// 注意：User.PasswordHash 为 json:"-" 不序列化，导入用户密码哈希为空，
	// 登录会失败 → 须管理员通过 change-password API 重置密码。
	for _, u := range data.Users {
		if existing := st.GetUserByUsername(u.Username); existing != nil && !opts.Overwrite {
			res.Skipped++
			continue
		}
		uc := *u
		st.CreateUser(&uc)
		res.Users++
	}

	// 7. Roles：CreateRole（按 name 幂等；已存在且非 overwrite 时跳过）。
	for _, r := range data.Roles {
		exists := false
		for _, existing := range st.ListRoles() {
			if existing.Name == r.Name {
				exists = true
				break
			}
		}
		if exists && !opts.Overwrite {
			res.Skipped++
			continue
		}
		rc := *r
		st.CreateRole(&rc)
		res.Roles++
	}

	logx.Info(ctx, "backup 导入完成",
		"devices", res.Devices, "agents", res.Agents, "tasks", res.Tasks,
		"alerts", res.Alerts, "alertRules", res.AlertRules,
		"users", res.Users, "roles", res.Roles, "skipped", res.Skipped)
	return &data, res, nil
}

// ImportBackupFile 是 ImportBackup 的文件包装：打开 input 文件后导入。
func ImportBackupFile(ctx context.Context, st store.Store, opts ImportOptions, input string) (*BackupData, *ImportResult, error) {
	f, err := os.Open(input)
	if err != nil {
		return nil, nil, fmt.Errorf("打开备份文件 %q 失败: %w", input, err)
	}
	defer f.Close()
	return ImportBackup(ctx, st, opts, f)
}

// writeSQLDump 把 BackupData 序列化为 MySQL 兼容的 SQL dump 文本写入 w。
//
// 生成 INSERT 语句（ON DUPLICATE KEY UPDATE 实现幂等），按表分组：
//   - opsmesh_devices / opsmesh_agents / opsmesh_tasks / opsmesh_alerts / opsmesh_alert_rules
//   - opsmesh_users / opsmesh_roles / opsmesh_permissions / opsmesh_audits
//
// 注意：SQL dump 仅作离线备份/迁移用，restore 子命令只支持 JSON 格式导入。
// SQL dump 导入需用 mysql 客户端：mysql -u<user> -p<pwd> <dump.sql。
func writeSQLDump(data *BackupData, w io.Writer) error {
	bw := bufio.NewWriter(w)
	defer bw.Flush()

	fmt.Fprintf(bw, "-- opsmesh backup SQL dump\n")
	fmt.Fprintf(bw, "-- version: %s\n", data.Meta.Version)
	fmt.Fprintf(bw, "-- createdAt: %s\n", data.Meta.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(bw, "-- generated by opsmesh backup --format sql\n\n")
	bw.WriteString("SET NAMES utf8mb4;\nSET FOREIGN_KEY_CHECKS=0;\n\n")

	// devices
	bw.WriteString("-- Table: opsmesh_devices\n")
	for _, d := range data.Devices {
		fmt.Fprintf(bw,
			"INSERT INTO opsmesh_devices (device_id,segment,tenant_id,ip,agent_id,state,task_state,managed,last_result,retired,hostname,os,arch) VALUES (%s,%s,%s,%s,%s,%s,%s,%d,%s,%d,%s,%s,%s) ON DUPLICATE KEY UPDATE state=VALUES(state);\n",
			sqlStr(d.DeviceID), sqlStr(d.Segment), sqlStr(d.TenantID), sqlStr(d.IP),
			sqlStr(d.AgentID), sqlStr(d.State), sqlStr(d.TaskState), b2i(d.Managed),
			sqlStr(d.LastResult), b2i(d.Retired), sqlStr(d.Hostname), sqlStr(d.OS), sqlStr(d.Arch))
	}
	bw.WriteString("\n")

	// agents
	bw.WriteString("-- Table: opsmesh_agents\n")
	for _, a := range data.Agents {
		fmt.Fprintf(bw,
			"INSERT INTO opsmesh_agents (agent_id,hostname,segment,tenant_id,addr,status,load,last_seen,os,arch) VALUES (%s,%s,%s,%s,%s,%s,%d,%s,%s,%s) ON DUPLICATE KEY UPDATE status=VALUES(status);\n",
			sqlStr(a.AgentID), sqlStr(a.Hostname), sqlStr(a.Segment), sqlStr(a.TenantID),
			sqlStr(a.Addr), sqlStr(a.Status), a.Load, sqlTime(a.LastSeen), sqlStr(a.OS), sqlStr(a.Arch))
	}
	bw.WriteString("\n")

	// tasks
	bw.WriteString("-- Table: opsmesh_tasks\n")
	for _, t := range data.Tasks {
		fmt.Fprintf(bw,
			"INSERT INTO opsmesh_tasks (task_id,agent_id,tenant_id,type,command,status,created_at) VALUES (%s,%s,%s,%s,%s,%s,%s) ON DUPLICATE KEY UPDATE status=VALUES(status);\n",
			sqlStr(t.TaskID), sqlStr(t.AgentID), sqlStr(t.TenantID), sqlStr(t.Type),
			sqlStr(t.Command), sqlStr(t.Status), sqlTime(t.CreatedAt))
	}
	bw.WriteString("\n")

	// alerts
	bw.WriteString("-- Table: opsmesh_alerts\n")
	for _, a := range data.Alerts {
		fmt.Fprintf(bw,
			"INSERT INTO opsmesh_alerts (alert_id,tenant_id,device_id,severity,message,status,created_at) VALUES (%s,%s,%s,%s,%s,%s,%s);\n",
			sqlStr(a.AlertID), sqlStr(a.TenantID), sqlStr(a.DeviceID),
			sqlStr(a.Severity), sqlStr(a.Message), sqlStr(a.Status), sqlTime(a.CreatedAt))
	}
	bw.WriteString("\n")

	// alert_rules
	bw.WriteString("-- Table: opsmesh_alert_rules\n")
	for _, r := range data.AlertRules {
		fmt.Fprintf(bw,
			"INSERT INTO opsmesh_alert_rules (id,tenant_id,metric,op,threshold,severity,enabled,created_at) VALUES (%s,%s,%s,%s,%g,%s,%d,%s) ON DUPLICATE KEY UPDATE enabled=VALUES(enabled);\n",
			sqlStr(r.ID), sqlStr(r.TenantID), sqlStr(r.Metric), sqlStr(r.Op),
			r.Threshold, sqlStr(r.Severity), b2i(r.Enabled), sqlTime(r.CreatedAt))
	}
	bw.WriteString("\n")

	// users（不导出 password_hash，敏感字段由管理员重置）
	bw.WriteString("-- Table: opsmesh_users (password_hash 不导出，导入后须重置密码)\n")
	for _, u := range data.Users {
		fmt.Fprintf(bw,
			"INSERT INTO opsmesh_users (id,username,email,status,created_at) VALUES (%s,%s,%s,%s,%s) ON DUPLICATE KEY UPDATE email=VALUES(email);\n",
			sqlStr(u.ID), sqlStr(u.Username), sqlStr(u.Email), sqlStr(u.Status), sqlTime(u.CreatedAt))
	}
	bw.WriteString("\n")

	// roles
	bw.WriteString("-- Table: opsmesh_roles\n")
	for _, r := range data.Roles {
		fmt.Fprintf(bw,
			"INSERT INTO opsmesh_roles (id,name,description,created_at) VALUES (%s,%s,%s,%s) ON DUPLICATE KEY UPDATE description=VALUES(description);\n",
			sqlStr(r.ID), sqlStr(r.Name), sqlStr(r.Description), sqlTime(r.CreatedAt))
	}
	bw.WriteString("\n")

	// permissions
	bw.WriteString("-- Table: opsmesh_permissions\n")
	for _, p := range data.Permissions {
		fmt.Fprintf(bw,
			"INSERT INTO opsmesh_permissions (id,name,description,`group`) VALUES (%s,%s,%s,%s) ON DUPLICATE KEY UPDATE description=VALUES(description);\n",
			sqlStr(p.ID), sqlStr(p.Name), sqlStr(p.Description), sqlStr(p.Group))
	}
	bw.WriteString("\n")

	// audits（仅 --include-audits 时有数据）
	if len(data.Audits) > 0 {
		bw.WriteString("-- Table: opsmesh_audits\n")
		for _, a := range data.Audits {
			fmt.Fprintf(bw,
				"INSERT INTO opsmesh_audits (tenant_id,user_id,action,target,detail,created_at) VALUES (%s,%s,%s,%s,%s,%s);\n",
				sqlStr(a.TenantID), sqlStr(a.UserID), sqlStr(a.Action),
				sqlStr(a.Target), sqlStr(a.Detail), sqlTime(a.CreatedAt))
		}
		bw.WriteString("\n")
	}

	bw.WriteString("SET FOREIGN_KEY_CHECKS=1;\n")
	return bw.Flush()
}

// sqlStr 把字符串转义为 SQL 字面量（单引号包裹，内部单引号转义为 ”）。
func sqlStr(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// sqlTime 把 time.Time 转为 SQL datetime 字面量（零值返回 NULL）。
func sqlTime(t time.Time) string {
	if t.IsZero() {
		return "NULL"
	}
	return "'" + t.Format("2006-01-02 15:04:05") + "'"
}

// b2i 把 bool 转为 0/1。
func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
