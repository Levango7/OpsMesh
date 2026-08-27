// memory_billing.go 实现 MemoryStore 的 BillingStore 子接口（Phase 6 计费）。
package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// randBillingID 生成随机计费 ID（prefix + 16 字节 hex）。
func randBillingID(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(b)
}

// cloneBillingPlan 返回 p 的深拷贝（含 Features）。
func cloneBillingPlan(p *SubscriptionPlan) *SubscriptionPlan {
	if p == nil {
		return nil
	}
	cp := *p
	if p.Features != nil {
		cp.Features = append([]string(nil), p.Features...)
	}
	return &cp
}

// cloneSubscription 返回 s 的深拷贝。
func cloneSubscription(s *Subscription) *Subscription {
	if s == nil {
		return nil
	}
	cp := *s
	return &cp
}

// cloneInvoice 返回 i 的深拷贝（含 Items）。
func cloneInvoice(i *Invoice) *Invoice {
	if i == nil {
		return nil
	}
	cp := *i
	if i.Items != nil {
		cp.Items = append([]InvoiceItem(nil), i.Items...)
	}
	return &cp
}

// CreateBillingPlan 创建订阅计划（按 ID 幂等；ID 为空时分配随机 ID）。
func (m *MemoryStore) CreateBillingPlan(plan *SubscriptionPlan) *SubscriptionPlan {
	if plan == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if plan.ID == "" {
		plan.ID = randBillingID("plan")
	}
	if plan.CreatedAt.IsZero() {
		plan.CreatedAt = time.Now()
	}
	m.billingPlans[plan.ID] = plan
	return cloneBillingPlan(plan)
}

// GetBillingPlan 按 ID 返回单个订阅计划。
func (m *MemoryStore) GetBillingPlan(id string) (*SubscriptionPlan, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.billingPlans[id]
	if !ok {
		return nil, false
	}
	return cloneBillingPlan(p), true
}

// ListBillingPlans 返回全部订阅计划（按创建时间升序）。
func (m *MemoryStore) ListBillingPlans() []*SubscriptionPlan {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*SubscriptionPlan, 0, len(m.billingPlans))
	for _, p := range m.billingPlans {
		out = append(out, cloneBillingPlan(p))
	}
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].CreatedAt.Before(out[j-1].CreatedAt) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

// UpdateBillingPlan 更新订阅计划。
func (m *MemoryStore) UpdateBillingPlan(plan *SubscriptionPlan) (*SubscriptionPlan, bool) {
	if plan == nil || plan.ID == "" {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.billingPlans[plan.ID]
	if !ok {
		return nil, false
	}
	plan.ID = existing.ID
	plan.CreatedAt = existing.CreatedAt
	m.billingPlans[plan.ID] = plan
	return cloneBillingPlan(plan), true
}

// DeleteBillingPlan 按 ID 删除订阅计划。
func (m *MemoryStore) DeleteBillingPlan(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.billingPlans[id]; !ok {
		return false
	}
	delete(m.billingPlans, id)
	return true
}

// CreateSubscription 创建订阅。
func (m *MemoryStore) CreateSubscription(sub *Subscription) *Subscription {
	if sub == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if sub.ID == "" {
		sub.ID = randBillingID("sub")
	}
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = time.Now()
	}
	if sub.Status == "" {
		sub.Status = "active"
	}
	m.subscriptions[sub.ID] = sub
	return cloneSubscription(sub)
}

// GetSubscription 按 ID 返回单个订阅。
func (m *MemoryStore) GetSubscription(id string) (*Subscription, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.subscriptions[id]
	if !ok {
		return nil, false
	}
	return cloneSubscription(s), true
}

// ListSubscriptions 返回指定租户的全部订阅（按创建时间降序）。
func (m *MemoryStore) ListSubscriptions(tenantID string) []*Subscription {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Subscription, 0, len(m.subscriptions))
	for _, s := range m.subscriptions {
		if tenantID != "" && s.TenantID != tenantID {
			continue
		}
		out = append(out, cloneSubscription(s))
	}
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].CreatedAt.After(out[j-1].CreatedAt) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

// UpdateSubscription 更新订阅。
func (m *MemoryStore) UpdateSubscription(sub *Subscription) (*Subscription, bool) {
	if sub == nil || sub.ID == "" {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.subscriptions[sub.ID]
	if !ok {
		return nil, false
	}
	sub.ID = existing.ID
	sub.TenantID = existing.TenantID
	sub.CreatedAt = existing.CreatedAt
	m.subscriptions[sub.ID] = sub
	return cloneSubscription(sub), true
}

// DeleteSubscription 按 ID 删除订阅。
func (m *MemoryStore) DeleteSubscription(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.subscriptions[id]; !ok {
		return false
	}
	delete(m.subscriptions, id)
	return true
}

// CreateInvoice 创建账单。
func (m *MemoryStore) CreateInvoice(inv *Invoice) *Invoice {
	if inv == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if inv.ID == "" {
		inv.ID = randBillingID("inv")
	}
	if inv.CreatedAt.IsZero() {
		inv.CreatedAt = time.Now()
	}
	if inv.Status == "" {
		inv.Status = "pending"
	}
	m.invoices[inv.ID] = inv
	return cloneInvoice(inv)
}

// GetInvoice 按 ID 返回单个账单。
func (m *MemoryStore) GetInvoice(id string) (*Invoice, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	i, ok := m.invoices[id]
	if !ok {
		return nil, false
	}
	return cloneInvoice(i), true
}

// ListInvoices 返回指定租户的全部账单（按创建时间降序）。
func (m *MemoryStore) ListInvoices(tenantID string) []*Invoice {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Invoice, 0, len(m.invoices))
	for _, i := range m.invoices {
		if tenantID != "" && i.TenantID != tenantID {
			continue
		}
		out = append(out, cloneInvoice(i))
	}
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].CreatedAt.After(out[j-1].CreatedAt) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

// CalculateUsage 计算指定租户的资源用量统计。
func (m *MemoryStore) CalculateUsage(tenantID string) (*Usage, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	usage := &Usage{
		TenantID:     tenantID,
		CalculatedAt: time.Now(),
	}

	for _, devices := range m.segments {
		for _, d := range devices {
			if tenantID == "" || d.TenantID == tenantID {
				usage.DeviceCount++
			}
		}
	}

	for _, taskList := range m.tasks {
		for _, t := range taskList {
			if tenantID == "" || t.TenantID == tenantID {
				usage.TaskCount++
			}
		}
	}

	for _, a := range m.alerts {
		if tenantID == "" || a.TenantID == tenantID {
			usage.AlertCount++
		}
	}

	return usage, true
}
