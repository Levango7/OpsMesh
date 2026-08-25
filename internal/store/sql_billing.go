
// sql_billing.go 实现 SQLStore 的 BillingStore 子接口（Phase 6，桩实现）。
package store

import "time"

// CreateBillingPlan 创建订阅计划（桩实现）。
func (s *SQLStore) CreateBillingPlan(plan *SubscriptionPlan) *SubscriptionPlan {
	if plan == nil {
		return nil
	}
	if plan.ID == "" {
		plan.ID = randBillingID("plan")
	}
	if plan.CreatedAt.IsZero() {
		plan.CreatedAt = time.Now().UTC()
	}
	return plan
}

// GetBillingPlan 按 ID 返回单个订阅计划（桩实现）。
func (s *SQLStore) GetBillingPlan(id string) (*SubscriptionPlan, bool) {
	return nil, false
}

// ListBillingPlans 返回全部订阅计划（桩实现）。
func (s *SQLStore) ListBillingPlans() []*SubscriptionPlan {
	return []*SubscriptionPlan{}
}

// UpdateBillingPlan 更新订阅计划（桩实现）。
func (s *SQLStore) UpdateBillingPlan(plan *SubscriptionPlan) (*SubscriptionPlan, bool) {
	return nil, false
}

// DeleteBillingPlan 按 ID 删除订阅计划（桩实现）。
func (s *SQLStore) DeleteBillingPlan(id string) bool {
	return false
}

// CreateSubscription 创建订阅（桩实现）。
func (s *SQLStore) CreateSubscription(sub *Subscription) *Subscription {
	if sub == nil {
		return nil
	}
	if sub.ID == "" {
		sub.ID = randBillingID("sub")
	}
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = time.Now().UTC()
	}
	if sub.Status == "" {
		sub.Status = "active"
	}
	return sub
}

// GetSubscription 按 ID 返回单个订阅（桩实现）。
func (s *SQLStore) GetSubscription(id string) (*Subscription, bool) {
	return nil, false
}

// ListSubscriptions 返回指定租户的全部订阅（桩实现）。
func (s *SQLStore) ListSubscriptions(tenantID string) []*Subscription {
	return []*Subscription{}
}

// UpdateSubscription 更新订阅（桩实现）。
func (s *SQLStore) UpdateSubscription(sub *Subscription) (*Subscription, bool) {
	return nil, false
}

// DeleteSubscription 按 ID 删除订阅（桩实现）。
func (s *SQLStore) DeleteSubscription(id string) bool {
	return false
}

// CreateInvoice 创建账单（桩实现）。
func (s *SQLStore) CreateInvoice(inv *Invoice) *Invoice {
	if inv == nil {
		return nil
	}
	if inv.ID == "" {
		inv.ID = randBillingID("inv")
	}
	if inv.CreatedAt.IsZero() {
		inv.CreatedAt = time.Now().UTC()
	}
	if inv.Status == "" {
		inv.Status = "pending"
	}
	return inv
}

// GetInvoice 按 ID 返回单个账单（桩实现）。
func (s *SQLStore) GetInvoice(id string) (*Invoice, bool) {
	return nil, false
}

// ListInvoices 返回指定租户的全部账单（桩实现）。
func (s *SQLStore) ListInvoices(tenantID string) []*Invoice {
	return []*Invoice{}
}