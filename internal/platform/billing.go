// billing.go 保留计费数据模型类型别名（handler 经 store 引用）。
//
// 历史：原 BillingManager 计费引擎（CalculateUsage/GenerateInvoice/CalculateProration）
// 已作为 H7 平台死代码清理删除——controlplane 计费 handler 直接调用 store.BillingStore
// 接口，不经 platform 层封装。类型别名保留以兼容 handler 现有 import 路径。
//
// 设计要点：
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

// formatPeriod 格式化账单周期为人类可读字符串（用于账单 PDF/邮件）。
func formatPeriod(start, end time.Time) string {
	return fmt.Sprintf("%s ~ %s", start.Format("2006-01-02"), end.Format("2006-01-02"))
}
