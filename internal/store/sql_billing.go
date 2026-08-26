// sql_billing.go 实现 SQLStore 的 BillingStore 子接口（Phase 6 计费：计划/订阅/账单）。
//
// 【未实现的桩】本文件全部方法未接入 MySQL 持久化，经 stub_guard.StubNotImplemented
// 显式告警（每 (domain,method) 首次必打、60s 限频），并按统一约定返回零值：
//   - Create 类（Plan/Subscription/Invoice）一律返回 nil
//     （不返回填充后的假对象——杜绝「201 假成功 → GET 404 → 审计已记成功」链路）；
//   - Get/Update/Delete 返回 (nil, false) / false；List 类返回非 nil 空切片。
package store

// CreateBillingPlan 创建订阅计划（未实现的桩）。
func (s *SQLStore) CreateBillingPlan(plan *SubscriptionPlan) *SubscriptionPlan {
	StubNotImplemented("billing", "CreateBillingPlan")
	return nil
}

// GetBillingPlan 按 ID 返回单个订阅计划（未实现的桩）。
func (s *SQLStore) GetBillingPlan(id string) (*SubscriptionPlan, bool) {
	StubNotImplemented("billing", "GetBillingPlan")
	return nil, false
}

// ListBillingPlans 返回全部订阅计划（未实现的桩；返回非 nil 空切片防上层 range panic）。
func (s *SQLStore) ListBillingPlans() []*SubscriptionPlan {
	StubNotImplemented("billing", "ListBillingPlans")
	return []*SubscriptionPlan{}
}

// UpdateBillingPlan 更新订阅计划（未实现的桩）。
func (s *SQLStore) UpdateBillingPlan(plan *SubscriptionPlan) (*SubscriptionPlan, bool) {
	StubNotImplemented("billing", "UpdateBillingPlan")
	return nil, false
}

// DeleteBillingPlan 按 ID 删除订阅计划（未实现的桩）。
func (s *SQLStore) DeleteBillingPlan(id string) bool {
	StubNotImplemented("billing", "DeleteBillingPlan")
	return false
}

// CreateSubscription 创建订阅（未实现的桩）。
func (s *SQLStore) CreateSubscription(sub *Subscription) *Subscription {
	StubNotImplemented("billing", "CreateSubscription")
	return nil
}

// GetSubscription 按 ID 返回单个订阅（未实现的桩）。
func (s *SQLStore) GetSubscription(id string) (*Subscription, bool) {
	StubNotImplemented("billing", "GetSubscription")
	return nil, false
}

// ListSubscriptions 返回指定租户的全部订阅（未实现的桩；返回非 nil 空切片防上层 range panic）。
func (s *SQLStore) ListSubscriptions(tenantID string) []*Subscription {
	StubNotImplemented("billing", "ListSubscriptions")
	return []*Subscription{}
}

// UpdateSubscription 更新订阅（未实现的桩）。
func (s *SQLStore) UpdateSubscription(sub *Subscription) (*Subscription, bool) {
	StubNotImplemented("billing", "UpdateSubscription")
	return nil, false
}

// DeleteSubscription 按 ID 删除订阅（未实现的桩）。
func (s *SQLStore) DeleteSubscription(id string) bool {
	StubNotImplemented("billing", "DeleteSubscription")
	return false
}

// CreateInvoice 创建账单（未实现的桩）。
func (s *SQLStore) CreateInvoice(inv *Invoice) *Invoice {
	StubNotImplemented("billing", "CreateInvoice")
	return nil
}

// GetInvoice 按 ID 返回单个账单（未实现的桩）。
func (s *SQLStore) GetInvoice(id string) (*Invoice, bool) {
	StubNotImplemented("billing", "GetInvoice")
	return nil, false
}

// ListInvoices 返回指定租户的全部账单（未实现的桩；返回非 nil 空切片防上层 range panic）。
func (s *SQLStore) ListInvoices(tenantID string) []*Invoice {
	StubNotImplemented("billing", "ListInvoices")
	return []*Invoice{}
}
