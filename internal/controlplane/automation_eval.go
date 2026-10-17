package controlplane

// automation_eval.go 实现自动化引擎评估循环：周期扫描 enabled 规则、按触发上下文评估、
// 命中时执行动作并落库执行记录。
//
// 由 server_lifecycle.go Start 中 `go s.startAutomationEvalLoop(ctx, s.automationEvalInterval)`
// 启动（该调用已就位，本文件补齐实现）。与 alertEngineLoop（M2 告警规则）解耦——
// 本循环评估 Phase 4 automation 规则（alert/metric_threshold/schedule/event 触发器），
// 动作经 automation.Executor（server.go NewServer 注入 automationExecutor）真实执行。

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/Levango7/OpsMesh/internal/aiops"
	"github.com/Levango7/OpsMesh/internal/automation"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// defaultAutomationEvalInterval 评估周期兜底值（interval<=0 时使用）。
const defaultAutomationEvalInterval = 30 * time.Second

// startAutomationEvalLoop 启动自动化规则评估循环。
//
// 退出机制：goroutine 通过 select 监听 ctx.Done() 与 ticker.C，ctx 取消时优雅退出
// 并 Stop ticker，避免 goroutine 泄漏（与 startRefreshSweep 同范式）。
func (s *Server) startAutomationEvalLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = defaultAutomationEvalInterval
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.processAutomationRules()
			}
		}
	}()
}

// processAutomationRules 扫描全部 enabled 自动化规则并评估/执行。
//
// 规则快照：s.store.ListAutomationRules("") 返回深拷贝（Memory/SQL 后端均保证），
// 遍历期间即使有 handler 并发修改规则也不影响本次评估的一致性。
// 兜底：整体 recover 防止单条规则评估/执行 panic 拖垮循环（后续 tick 继续）。
func (s *Server) processAutomationRules() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("controlplane: automation eval loop panic recovered: %v", r)
		}
	}()
	if s.store == nil {
		return
	}
	rules := s.store.ListAutomationRules("")
	for _, rule := range rules {
		if rule == nil || !rule.Enabled {
			continue
		}
		domain := automationRuleToDomain(rule)
		if domain == nil {
			continue
		}
		ctx := s.buildAutomationEvalContext(domain)
		if !automationEngine.Evaluate(domain, ctx) {
			continue
		}

		// AIOps 增强：alert 类型规则命中时，先用 AIOps 做根因分析 + 智能修复方案生成，
		// 将生成的修复命令下发为任务，形成"告警→根因→修复→自动执行"闭环。
		if s.aiopsEngine != nil && s.aiopsEngine.IsEnabled() &&
			domain.Trigger.Type == automation.TriggerTypeAlert {
			if alertID := ctx["alert"]; alertID != "" {
				if alert := s.findAlertByID(rule.TenantID, alertID); alert != nil {
					if _, plan, err := s.aiopsEngine.AnalyzeAndRemediate(
						context.Background(),
						&aiops.AlertContext{
							Alert:   alert,
							Logs:    nil,
							Metrics: s.buildDeviceContext(alert.DeviceID),
						},
						aiops.RemediationConstraints{
							MaxSteps:        5,
							RiskTolerance:   "low",
							RequireApproval: false,
							TimeoutSec:      30,
							AutoExecute:     true,
						},
					); err == nil && plan != nil {
						s.executeRemediationPlan(rule.TenantID, plan)
					}
				}
			}
		}

		exec := automationEngine.Execute(domain)
		if exec == nil {
			continue
		}
		storeExec := automationExecToStore(exec, rule.TenantID)
		if storeExec != nil {
			s.store.CreateAutomationExecution(rule.TenantID, storeExec)
		}
	}
}

// buildAutomationEvalContext 构造规则评估上下文（供 automation.Engine.Evaluate 使用）。
//
// 按触发器类型填充：
//   - alert：租户存在活跃告警时填充 ctx["alert"]（引擎据此触发）；
//   - metric_threshold：从设备最新指标解析 trigger.Params["metric"] 对应值填充 ctx["value"]，
//     引擎按 ctx["value"] >= trigger.Params["threshold"] 判定；
//   - schedule：不需要上下文——引擎直接按 trigger.Params["schedule"] 的 cron 表达式
//     对当前时刻求值（并做 1 分钟去重）。这是修复点：此前引擎对 schedule/event
//     无条件返回 true，而本循环每 30s 遍历全部 enabled 规则，导致定时规则
//     每 30 秒执行一次动作；
//   - event：不填充。本循环是周期扫描而非事件源，不注入 ctx["event"]，
//     该类型在此不会触发（须由事件侧显式调用）。
func (s *Server) buildAutomationEvalContext(rule *automation.Rule) map[string]string {
	if rule == nil || s.store == nil {
		return map[string]string{}
	}
	ctx := make(map[string]string)
	tenantID := rule.TenantID
	if tenantID == "" {
		tenantID = "default"
	}
	switch rule.Trigger.Type {
	case automation.TriggerTypeAlert:
		alerts := s.store.Alerts(tenantID)
		if len(alerts) > 0 {
			ctx["alert"] = alerts[0].AlertID
		}
	case automation.TriggerTypeMetricThreshold:
		if v, ok := s.deviceMetricValue(tenantID, rule.Trigger.Params["metric"]); ok {
			ctx["value"] = strconv.FormatFloat(v, 'f', 2, 64)
		}
	case automation.TriggerTypeSchedule:
		// 无需上下文：引擎自取当前时刻做 cron 求值。
	case automation.TriggerTypeEvent:
		// 本循环非事件源，不注入 ctx["event"]，该类型在此不触发。
	default:
		// 未知触发器类型：留空上下文（引擎 Evaluate 返回 false，不触发）。
	}
	return ctx
}

// deviceMetricValue 从租户设备最新指标中解析指定指标名对应数值。
//
// 支持的指标名映射（与 alert_rules/automation metric_threshold 惯例对齐）：
//   - cpu_usage → CPU.Usage（0-100）
//   - mem_usage / memory_usage → Memory.Usage（0-100）
//   - 其他：返回 (0, false)（无对应映射，规则不触发）。
//
// 多设备时返回首个可解析设备的指标值（MVP 语义：任一设备越线即触发）。
func (s *Server) deviceMetricValue(tenantID, metric string) (float64, bool) {
	if metric == "" {
		return 0, false
	}
	devices := s.store.Snapshot(tenantID)
	for _, list := range devices {
		for _, d := range list {
			m := s.store.DeviceMetrics(d.DeviceID)
			if m == nil {
				continue
			}
			switch metric {
			case "cpu_usage", "cpu", "cpu.usage":
				return m.CPU.Usage, true
			case "mem_usage", "memory_usage", "memory.usage", "mem.usage":
				return m.Memory.Usage, true
			default:
				return 0, false
			}
		}
	}
	return 0, false
}

// findAlertByID 按租户 + AlertID 从活跃告警中查找告警对象。
func (s *Server) findAlertByID(tenantID, alertID string) *proto.Alert {
	if s.store == nil || alertID == "" {
		return nil
	}
	for _, a := range s.store.Alerts(tenantID) {
		if a != nil && a.AlertID == alertID {
			return a
		}
	}
	return nil
}

// buildDeviceContext 从设备最新指标构建 aiops.DeviceContext。
func (s *Server) buildDeviceContext(deviceID string) *aiops.DeviceContext {
	if s.store == nil || deviceID == "" {
		return nil
	}
	m := s.store.DeviceMetrics(deviceID)
	if m == nil {
		return nil
	}
	return &aiops.DeviceContext{
		DeviceInfo: &proto.DeviceInfo{
			DeviceID: m.DeviceID,
			Hostname: m.Hostname,
			OS:       m.OS,
			Arch:     m.Arch,
			State:    "online",
		},
		Metrics: &proto.DeviceMetrics{
			CPU:          m.CPU,
			Memory:       m.Memory,
			Disks:        m.Disks,
			Network:      m.Network,
			ProcessCount: m.ProcessCount,
			Uptime:       m.Uptime,
		},
		Uptime: m.Uptime,
	}
}

// executeRemediationPlan 将 AIOps 修复方案中的 commands 下发为任务。
func (s *Server) executeRemediationPlan(tenantID string, plan *aiops.RemediationPlan) {
	if s.store == nil || plan == nil {
		return
	}
	for _, cmd := range plan.Commands {
		if cmd.AgentID == "" || cmd.Command == "" {
			continue
		}
		task := &proto.Task{
			AgentID:    cmd.AgentID,
			TenantID:   tenantID,
			Type:       cmd.Type,
			Command:    cmd.Command,
			Status:     "pending",
			Timeout:    cmd.Timeout,
			MaxRetries: 3,
			CreatedAt:  time.Now(),
		}
		if task.Type == "" {
			task.Type = proto.TaskTypeShell
		}
		if task.MaxRetries <= 0 {
			task.MaxRetries = 3
		}
		if _, err := s.store.CreateTask(task); err != nil {
			log.Printf("controlplane: aiops remediation dispatch failed for agent %s: %v", cmd.AgentID, err)
		}
	}
}
