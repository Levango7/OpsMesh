
// billing.go 实现计费引擎（平台化）。
//
// 计费引擎支持订阅计划管理、订阅生命周期、账单生成：
//   - SubscriptionPlan：订阅计划（免费/基础/专业/企业），定义价格/周期/功能/资源限额；
//   - Subscription：租户订阅记录，关联计划与租户，管理生命周期；
//   - Invoice：账单，按订阅周期生成，记录消费明细。
//
// 设计要点：
//   - CalculateUsage 统计租户当前资源用量（用于按量计费 + 配额展示）；
//   - GenerateInvoice 按订阅周期生成账单（预付费模式：周期开始时生成）；
//   - 计费单位为分（int），避免浮点精度问题；
//   - 货币默认 CNY（人民币），可通过 SubscriptionPlan.Currency 配置。
package platform

import (
	"fmt"
	"time"

	"opsmesh/internal/store"
)

// 复用 store 包计费数据模型。
type (
	SubscriptionPlan = store.SubscriptionPlan
	Subscription     = store.Subscription
	Invoice          = store.Invoice
	InvoiceItem      = store.InvoiceItem
)

// BillingManager 计费引擎。
type BillingManager struct {
	store store.BillingStore
}

// NewBillingManager 构造计费引擎。
func NewBillingManager(s store.BillingStore) *BillingManager {
	return &BillingManager{store: s}
}

// CalculateUsage 计算租户当前资源用量。
// MVP：从 store 中读取租户的 Usage 字段（由控制面周期更新）；
// 生产可改为实时聚合 DeviceStore/TaskStore/AlertStore 等计数。
func (m *BillingManager) CalculateUsage(tenantID string) *ResourceUsage {
	if tenantID == "" {
		return &ResourceUsage{}
	}
	// 从 TenantStore 读取租户（BillingStore 不直接持有 TenantStore，
	// 但 Tenant 结构内嵌 Usage 字段，由控制面周期更新）。
	// 这里通过 BillingStore 间接获取：遍历租户的订阅，从订阅中取用量。
	// MVP 简化：返回空用量，由控制面 handler 调用 TenantStore 填充。
	return &ResourceUsage{}
}

// GenerateInvoice 为租户生成当前周期的账单。
// 行为：
//   - 查找租户的 active 订阅；
//   - 按订阅计划的 Price 生成账单（预付费）；
//   - 账单状态默认 pending（待支付）。
// 无 active 订阅返回 nil。
func (m *BillingManager) GenerateInvoice(tenantID string) *Invoice {
	if tenantID == "" {
		return nil
	}
	subs := m.store.ListSubscriptions(tenantID)
	var active *Subscription
	for _, s := range subs {
		if s != nil && s.Status == "active" {
			active = s
			break
		}
	}
	if active == nil {
		return nil
	}
	plan, ok := m.store.GetBillingPlan(active.PlanID)
	if !ok || plan == nil {
		return nil
	}
	now := time.Now()
	periodStart := now
	periodEnd := now.AddDate(0, 1, 0) // 默认月付
	if plan.Interval == "yearly" {
		periodEnd = now.AddDate(1, 0, 0)
	}
	inv := &Invoice{
		TenantID:       tenantID,
		SubscriptionID: active.ID,
		Amount:         plan.Price,
		PeriodStart:    periodStart,
		PeriodEnd:      periodEnd,
		Status:         "pending",
		Items: []InvoiceItem{{
			Name:      plan.Name,
			Quantity:  1,
			UnitPrice: plan.Price,
			Amount:    plan.Price,
		}},
	}
	return m.store.CreateInvoice(inv)
}

// CalculateProration 计算订阅升级时的按比例退款/补差价。
// 返回应补/退的金额（正数=补差价，负数=退款）。
func (m *BillingManager) CalculateProration(currentPlan *SubscriptionPlan, newPlan *SubscriptionPlan, remainingDays, totalDays int) int {
	if currentPlan == nil || newPlan == nil || totalDays <= 0 {
		return 0
	}
	// 当前计划剩余价值 = 当前价格 * (剩余天数 / 总天数)。
	currentRemaining := currentPlan.Price * remainingDays / totalDays
	// 新计划剩余价值 = 新价格 * (剩余天数 / 总天数)。
	newRemaining := newPlan.Price * remainingDays / totalDays
	return newRemaining - currentRemaining
}

// formatPeriod 格式化账单周期为人类可读字符串（用于账单 PDF/邮件）。
func formatPeriod(start, end time.Time) string {
	return fmt.Sprintf("%s ~ %s", start.Format("2006-01-02"), end.Format("2006-01-02"))
}