// sql_billing.go 实现 SQLStore 的 BillingStore 子接口（Phase 6 计费：计划/订阅/账单，生产就绪）。
//
// 表结构：
//   - billing_plans（id PK + name + price + interval + features JSON + resource_limits JSON +
//     created_at）— 计划全局共享，无 tenant_id；
//   - subscriptions（id PK + tenant_id + plan_id + status + started_at + expires_at +
//     created_at）— 按 ID 全局唯一，List 按 tenant_id 过滤；
//   - invoices（id PK + tenant_id + subscription_id + amount + period_start + period_end +
//     status + items JSON + created_at）— 按 ID 全局唯一，List 按 tenant_id 过滤。
//
// 迁移文件 migrations/015_p6_tenant_apikey_plugin_billing.sql 幂等建表。
//
// 设计要点（与 sql_webhook.go / sql_secret.go 风格一致）：
//   - JSON 列：billing_plans.features（[]string）+ billing_plans.resource_limits（TenantQuota）
//     + invoices.items（[]InvoiceItem），用 encoding/json 序列化为 TEXT；空值存空串，
//     读取时空串跳过 Unmarshal 得零值；
//   - 金额用 INT（单位：分），避免浮点精度；
//   - billing_plans 全局共享：Get/Update/Delete/List 不带 tenant_id；
//   - subscriptions / invoices 按 ID 全局唯一：Get/Delete 不带 tenant_id（与 memory 一致），
//     List 按 tenant_id 过滤；
//   - CreateBillingPlan/CreateSubscription/CreateInvoice 按 ID 幂等
//     （INSERT ... ON DUPLICATE KEY UPDATE），不更新 created_at；
//   - ListBillingPlans 升序；ListSubscriptions/ListInvoices 降序（与 memory 一致）；
//   - UpdateBillingPlan/UpdateSubscription 先 SELECT 校验存在，再 UPDATE，
//     保留原 CreatedAt；ID 不可改；UpdateSubscription 保留 TenantID 不可改；
//   - DB 不可用时返回零值（nil/false/空 slice），不 panic；
//   - ID 生成复用 memory_billing.go 的 randBillingID（prefix + "-" + 16 字节 hex）：
//     Plan 前缀 "plan"，Subscription 前缀 "sub"，Invoice 前缀 "inv"。
package store

import (
	"context"
	"encoding/json"
	"log"
	"time"
)

// ============================================================================
// BillingPlan（订阅计划，全局共享）
// ============================================================================

// scanBillingPlan 从一行扫描出 *SubscriptionPlan（features / resource_limits 为 JSON 文本列）。
// 列顺序：id, name, price, interval, features, resource_limits, created_at。
// 无行或扫描失败返回 nil。
func scanBillingPlan(row rowScanner) *SubscriptionPlan {
	var p SubscriptionPlan
	var featuresJSON, resourceLimitsJSON string
	var createdAt time.Time
	if err := row.Scan(&p.ID, &p.Name, &p.Price, &p.Interval, &featuresJSON, &resourceLimitsJSON,
		&createdAt); err != nil {
		return nil
	}
	p.CreatedAt = createdAt
	if featuresJSON != "" {
		if err := json.Unmarshal([]byte(featuresJSON), &p.Features); err != nil {
			log.Printf("[store] scanBillingPlan 解析 features JSON 失败 (plan=%s): %v", p.ID, err)
		}
	}
	if resourceLimitsJSON != "" {
		if err := json.Unmarshal([]byte(resourceLimitsJSON), &p.ResourceLimits); err != nil {
			log.Printf("[store] scanBillingPlan 解析 resource_limits JSON 失败 (plan=%s): %v", p.ID, err)
		}
	}
	return &p
}

// marshalPlanFeatures 将 Features 序列化为 JSON 文本（空切片存空串）。
func marshalPlanFeatures(features []string) string {
	if features == nil {
		return ""
	}
	b, err := json.Marshal(features)
	if err != nil {
		return ""
	}
	return string(b)
}

// marshalPlanResourceLimits 将 ResourceLimits 序列化为 JSON 文本（零值存空串）。
func marshalPlanResourceLimits(q TenantQuota) string {
	b, err := json.Marshal(q)
	if err != nil {
		return ""
	}
	s := string(b)
	if s == `{"maxDevices":0,"maxTasks":0,"maxActiveTasks":0,"maxAlerts":0,"maxAgents":0,"maxWebhooks":0,"maxAPIKeys":0}` {
		return ""
	}
	return s
}

// CreateBillingPlan 创建订阅计划（按 ID 幂等；ID 为空时分配随机 ID）。
//
// 行为：
//   - plan == nil 返回 nil；
//   - ID 为空时分配随机 ID（前缀 plan-）；
//   - CreatedAt 为零值时填当前时间（新建场景）；
//   - INSERT ... ON DUPLICATE KEY UPDATE 实现 upsert（按 id 幂等），
//     created_at 仅插入不更新，防 upsert 改写创建时间；
//   - DB 失败时 log.Printf + 返回 nil。
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
	featuresJSON := marshalPlanFeatures(plan.Features)
	resourceLimitsJSON := marshalPlanResourceLimits(plan.ResourceLimits)
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO billing_plans (id, name, price, interval, features, resource_limits, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE name=VALUES(name), price=VALUES(price), interval=VALUES(interval),
		 features=VALUES(features), resource_limits=VALUES(resource_limits)`,
		plan.ID, plan.Name, plan.Price, plan.Interval, featuresJSON, resourceLimitsJSON,
		plan.CreatedAt); err != nil {
		log.Printf("[store] CreateBillingPlan 插入失败 (plan=%s): %v", plan.ID, err)
		return nil
	}
	return cloneBillingPlan(plan)
}

// GetBillingPlan 按 ID 返回单个订阅计划（深拷贝；不存在返回 (nil, false)）。
func (s *SQLStore) GetBillingPlan(id string) (*SubscriptionPlan, bool) {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT id, name, price, interval, features, resource_limits, created_at
		  FROM billing_plans WHERE id=?`, id)
	p := scanBillingPlan(row)
	if p == nil {
		return nil, false
	}
	return p, true
}

// ListBillingPlans 返回全部订阅计划（按创建时间升序；深拷贝）。
func (s *SQLStore) ListBillingPlans() []*SubscriptionPlan {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT id, name, price, interval, features, resource_limits, created_at
		  FROM billing_plans ORDER BY created_at ASC`)
	if err != nil {
		log.Printf("[store] ListBillingPlans 查询失败: %v", err)
		return []*SubscriptionPlan{}
	}
	defer rows.Close()
	out := make([]*SubscriptionPlan, 0)
	for rows.Next() {
		if p := scanBillingPlan(rows); p != nil {
			out = append(out, p)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListBillingPlans 遍历失败: %v", err)
	}
	return out
}

// UpdateBillingPlan 更新订阅计划（按 plan.ID 定位）。
//
// 行为：
//   - plan == nil 或 ID 为空返回 (nil, false)；
//   - 先 GetBillingPlan 校验存在，不存在返回 (nil, false)；
//   - CreatedAt 不可改（保留原值）；ID 不可改；
//   - 返回更新后的 SubscriptionPlan（深拷贝）。
func (s *SQLStore) UpdateBillingPlan(plan *SubscriptionPlan) (*SubscriptionPlan, bool) {
	if plan == nil || plan.ID == "" {
		return nil, false
	}
	// 先 SELECT 校验存在。
	existing, ok := s.GetBillingPlan(plan.ID)
	if !ok {
		return nil, false
	}
	// 保留不可改字段。
	plan.ID = existing.ID
	plan.CreatedAt = existing.CreatedAt
	featuresJSON := marshalPlanFeatures(plan.Features)
	resourceLimitsJSON := marshalPlanResourceLimits(plan.ResourceLimits)
	if _, err := s.db.ExecContext(context.Background(),
		`UPDATE billing_plans SET name=?, price=?, interval=?, features=?, resource_limits=?
		 WHERE id=?`,
		plan.Name, plan.Price, plan.Interval, featuresJSON, resourceLimitsJSON, plan.ID); err != nil {
		log.Printf("[store] UpdateBillingPlan 更新失败 (plan=%s): %v", plan.ID, err)
		return nil, false
	}
	return cloneBillingPlan(plan), true
}

// DeleteBillingPlan 按 ID 删除订阅计划。不存在返回 false。
func (s *SQLStore) DeleteBillingPlan(id string) bool {
	res, err := s.db.ExecContext(context.Background(),
		`DELETE FROM billing_plans WHERE id=?`, id)
	if err != nil {
		log.Printf("[store] DeleteBillingPlan 失败 (plan=%s): %v", id, err)
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		log.Printf("[store] DeleteBillingPlan RowsAffected 失败 (plan=%s): %v", id, rowsErr)
		return false
	}
	return n > 0
}

// ============================================================================
// Subscription（订阅，按 ID 全局唯一，List 按 tenant_id 过滤）
// ============================================================================

// scanSubscription 从一行扫描出 *Subscription。
// 列顺序：id, tenant_id, plan_id, status, started_at, expires_at, created_at。
// 无行或扫描失败返回 nil。
func scanSubscription(row rowScanner) *Subscription {
	var sub Subscription
	var startedAt, expiresAt, createdAt time.Time
	if err := row.Scan(&sub.ID, &sub.TenantID, &sub.PlanID, &sub.Status,
		&startedAt, &expiresAt, &createdAt); err != nil {
		return nil
	}
	sub.StartedAt = startedAt
	sub.ExpiresAt = expiresAt
	sub.CreatedAt = createdAt
	return &sub
}

// CreateSubscription 创建订阅（按 ID 幂等；ID 为空时分配随机 ID）。
//
// 行为：
//   - sub == nil 返回 nil；
//   - TenantID 为空时归一为 default；
//   - ID 为空时分配随机 ID（前缀 sub-）；
//   - Status 为空时归一为 active；
//   - CreatedAt 为零值时填当前时间（新建场景）；
//   - INSERT ... ON DUPLICATE KEY UPDATE 实现 upsert（按 id 幂等），
//     tenant_id / created_at 仅插入不更新，防 upsert 改写归属/创建时间；
//   - DB 失败时 log.Printf + 返回 nil。
func (s *SQLStore) CreateSubscription(sub *Subscription) *Subscription {
	if sub == nil {
		return nil
	}
	// 租户隔离：空租户归一为 default。
	if sub.TenantID == "" {
		sub.TenantID = "default"
	}
	if sub.ID == "" {
		sub.ID = randBillingID("sub")
	}
	if sub.Status == "" {
		sub.Status = "active"
	}
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = time.Now().UTC()
	}
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO subscriptions (id, tenant_id, plan_id, status, started_at, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE plan_id=VALUES(plan_id), status=VALUES(status),
		 started_at=VALUES(started_at), expires_at=VALUES(expires_at)`,
		sub.ID, sub.TenantID, sub.PlanID, sub.Status, sub.StartedAt, sub.ExpiresAt,
		sub.CreatedAt); err != nil {
		log.Printf("[store] CreateSubscription 插入失败 (sub=%s): %v", sub.ID, err)
		return nil
	}
	return cloneSubscription(sub)
}

// GetSubscription 按 ID 返回单个订阅（深拷贝；不存在返回 (nil, false)）。
// 不带 tenant_id 条件（与 memory 一致，按 ID 全局唯一）。
func (s *SQLStore) GetSubscription(id string) (*Subscription, bool) {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT id, tenant_id, plan_id, status, started_at, expires_at, created_at
		  FROM subscriptions WHERE id=?`, id)
	sub := scanSubscription(row)
	if sub == nil {
		return nil, false
	}
	return sub, true
}

// ListSubscriptions 返回指定租户的全部订阅（按创建时间降序；深拷贝）。
// tenantID 为空串时返回全部租户的订阅（与 memory 一致）。
func (s *SQLStore) ListSubscriptions(tenantID string) []*Subscription {
	var rows interface {
		Next() bool
		Scan(dest ...interface{}) error
		Close() error
		Err() error
	}
	var err error
	if tenantID == "" {
		rows, err = s.db.QueryContext(context.Background(),
			`SELECT id, tenant_id, plan_id, status, started_at, expires_at, created_at
			  FROM subscriptions ORDER BY created_at DESC`)
	} else {
		rows, err = s.db.QueryContext(context.Background(),
			`SELECT id, tenant_id, plan_id, status, started_at, expires_at, created_at
			  FROM subscriptions WHERE tenant_id=? ORDER BY created_at DESC`, tenantID)
	}
	if err != nil {
		log.Printf("[store] ListSubscriptions 查询失败 (tenant=%s): %v", tenantID, err)
		return []*Subscription{}
	}
	defer rows.Close()
	out := make([]*Subscription, 0)
	for rows.Next() {
		if sub := scanSubscription(rows); sub != nil {
			out = append(out, sub)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListSubscriptions 遍历失败: %v", err)
	}
	return out
}

// UpdateSubscription 更新订阅（按 sub.ID 定位）。
//
// 行为：
//   - sub == nil 或 ID 为空返回 (nil, false)；
//   - 先 GetSubscription 校验存在，不存在返回 (nil, false)；
//   - CreatedAt / TenantID 不可改（保留原值，防越权改归属）；ID 不可改；
//   - 返回更新后的 Subscription（深拷贝）。
func (s *SQLStore) UpdateSubscription(sub *Subscription) (*Subscription, bool) {
	if sub == nil || sub.ID == "" {
		return nil, false
	}
	// 先 SELECT 校验存在。
	existing, ok := s.GetSubscription(sub.ID)
	if !ok {
		return nil, false
	}
	// 保留不可改字段。
	sub.ID = existing.ID
	sub.TenantID = existing.TenantID
	sub.CreatedAt = existing.CreatedAt
	if _, err := s.db.ExecContext(context.Background(),
		`UPDATE subscriptions SET plan_id=?, status=?, started_at=?, expires_at=?
		 WHERE id=?`,
		sub.PlanID, sub.Status, sub.StartedAt, sub.ExpiresAt, sub.ID); err != nil {
		log.Printf("[store] UpdateSubscription 更新失败 (sub=%s): %v", sub.ID, err)
		return nil, false
	}
	return cloneSubscription(sub), true
}

// DeleteSubscription 按 ID 删除订阅。不存在返回 false。
// 不带 tenant_id 条件（与 memory 一致，按 ID 全局唯一）。
func (s *SQLStore) DeleteSubscription(id string) bool {
	res, err := s.db.ExecContext(context.Background(),
		`DELETE FROM subscriptions WHERE id=?`, id)
	if err != nil {
		log.Printf("[store] DeleteSubscription 失败 (sub=%s): %v", id, err)
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		log.Printf("[store] DeleteSubscription RowsAffected 失败 (sub=%s): %v", id, rowsErr)
		return false
	}
	return n > 0
}

// ============================================================================
// Invoice（账单，按 ID 全局唯一，List 按 tenant_id 过滤）
// ============================================================================

// scanInvoice 从一行扫描出 *Invoice（items 为 JSON 文本列）。
// 列顺序：id, tenant_id, subscription_id, amount, period_start, period_end, status,
// items, created_at。无行或扫描失败返回 nil。
func scanInvoice(row rowScanner) *Invoice {
	var inv Invoice
	var itemsJSON string
	var periodStart, periodEnd, createdAt time.Time
	if err := row.Scan(&inv.ID, &inv.TenantID, &inv.SubscriptionID, &inv.Amount,
		&periodStart, &periodEnd, &inv.Status, &itemsJSON, &createdAt); err != nil {
		return nil
	}
	inv.PeriodStart = periodStart
	inv.PeriodEnd = periodEnd
	inv.CreatedAt = createdAt
	if itemsJSON != "" {
		if err := json.Unmarshal([]byte(itemsJSON), &inv.Items); err != nil {
			log.Printf("[store] scanInvoice 解析 items JSON 失败 (invoice=%s): %v", inv.ID, err)
		}
	}
	return &inv
}

// marshalInvoiceItems 将 Items 序列化为 JSON 文本（空切片存空串）。
func marshalInvoiceItems(items []InvoiceItem) string {
	if items == nil {
		return ""
	}
	b, err := json.Marshal(items)
	if err != nil {
		return ""
	}
	return string(b)
}

// CreateInvoice 创建账单（按 ID 幂等；ID 为空时分配随机 ID）。
//
// 行为：
//   - inv == nil 返回 nil；
//   - TenantID 为空时归一为 default；
//   - ID 为空时分配随机 ID（前缀 inv-）；
//   - Status 为空时归一为 pending；
//   - CreatedAt 为零值时填当前时间（新建场景）；
//   - INSERT ... ON DUPLICATE KEY UPDATE 实现 upsert（按 id 幂等），
//     tenant_id / created_at 仅插入不更新，防 upsert 改写归属/创建时间；
//   - DB 失败时 log.Printf + 返回 nil。
func (s *SQLStore) CreateInvoice(inv *Invoice) *Invoice {
	if inv == nil {
		return nil
	}
	// 租户隔离：空租户归一为 default。
	if inv.TenantID == "" {
		inv.TenantID = "default"
	}
	if inv.ID == "" {
		inv.ID = randBillingID("inv")
	}
	if inv.Status == "" {
		inv.Status = "pending"
	}
	if inv.CreatedAt.IsZero() {
		inv.CreatedAt = time.Now().UTC()
	}
	itemsJSON := marshalInvoiceItems(inv.Items)
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO invoices (id, tenant_id, subscription_id, amount, period_start, period_end, status, items, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE subscription_id=VALUES(subscription_id), amount=VALUES(amount),
		 period_start=VALUES(period_start), period_end=VALUES(period_end), status=VALUES(status),
		 items=VALUES(items)`,
		inv.ID, inv.TenantID, inv.SubscriptionID, inv.Amount, inv.PeriodStart, inv.PeriodEnd,
		inv.Status, itemsJSON, inv.CreatedAt); err != nil {
		log.Printf("[store] CreateInvoice 插入失败 (invoice=%s): %v", inv.ID, err)
		return nil
	}
	return cloneInvoice(inv)
}

// GetInvoice 按 ID 返回单个账单（深拷贝；不存在返回 (nil, false)）。
// 不带 tenant_id 条件（与 memory 一致，按 ID 全局唯一）。
func (s *SQLStore) GetInvoice(id string) (*Invoice, bool) {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT id, tenant_id, subscription_id, amount, period_start, period_end, status, items, created_at
		  FROM invoices WHERE id=?`, id)
	inv := scanInvoice(row)
	if inv == nil {
		return nil, false
	}
	return inv, true
}

// ListInvoices 返回指定租户的全部账单（按创建时间降序；深拷贝）。
// tenantID 为空串时返回全部租户的账单（与 memory 一致）。
func (s *SQLStore) ListInvoices(tenantID string) []*Invoice {
	var rows interface {
		Next() bool
		Scan(dest ...interface{}) error
		Close() error
		Err() error
	}
	var err error
	if tenantID == "" {
		rows, err = s.db.QueryContext(context.Background(),
			`SELECT id, tenant_id, subscription_id, amount, period_start, period_end, status, items, created_at
			  FROM invoices ORDER BY created_at DESC`)
	} else {
		rows, err = s.db.QueryContext(context.Background(),
			`SELECT id, tenant_id, subscription_id, amount, period_start, period_end, status, items, created_at
			  FROM invoices WHERE tenant_id=? ORDER BY created_at DESC`, tenantID)
	}
	if err != nil {
		log.Printf("[store] ListInvoices 查询失败 (tenant=%s): %v", tenantID, err)
		return []*Invoice{}
	}
	defer rows.Close()
	out := make([]*Invoice, 0)
	for rows.Next() {
		if inv := scanInvoice(rows); inv != nil {
			out = append(out, inv)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListInvoices 遍历失败: %v", err)
	}
	return out
}
