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

	"opsmesh/internal/automation"
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
//   - schedule/event：恒命中（引擎对这两类直接返回 true），上下文可为空。
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
	case automation.TriggerTypeSchedule, automation.TriggerTypeEvent:
		// 恒命中：引擎对 schedule/event 类型 Evaluate 直接返回 true。
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

// ensureAutomationEvalInterval 返回 Server 的评估周期（零值兜底 30s，供测试/诊断复用）。
func (s *Server) ensureAutomationEvalInterval() time.Duration {
	if s.automationEvalInterval <= 0 {
		return defaultAutomationEvalInterval
	}
	return s.automationEvalInterval
}
