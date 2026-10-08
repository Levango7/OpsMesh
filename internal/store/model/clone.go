// clone.go 领域类型的深拷贝与排序 helper（TD-61，自 internal/store/memory_*.go 上提）。
//
// 为什么在中性层：这些 helper 定义在内存后端的文件里，但 SQL 后端同样在用
// （sql_*.go 读路径统一返回深拷贝，防调用方改动污染内部状态/cache）。
// 一端一份必然漂移，故与领域类型同处 model。
//
// 命名：原名 cloneXxx / sortServiceInstances 导出为 CloneXxx / SortServiceInstances。
package model

// CloneAPIKey 返回 k 的深拷贝（含 Scopes）。
func CloneAPIKey(k *APIKey) *APIKey {
	if k == nil {
		return nil
	}
	cp := *k
	if k.Scopes != nil {
		cp.Scopes = append([]string(nil), k.Scopes...)
	}
	return &cp
}

// CloneBillingPlan 返回 p 的深拷贝（含 Features）。
func CloneBillingPlan(p *SubscriptionPlan) *SubscriptionPlan {
	if p == nil {
		return nil
	}
	cp := *p
	if p.Features != nil {
		cp.Features = append([]string(nil), p.Features...)
	}
	return &cp
}

// CloneInvoice 返回 i 的深拷贝（含 Items）。
func CloneInvoice(i *Invoice) *Invoice {
	if i == nil {
		return nil
	}
	cp := *i
	if i.Items != nil {
		cp.Items = append([]InvoiceItem(nil), i.Items...)
	}
	return &cp
}

// ClonePlugin 返回 p 的深拷贝。
func ClonePlugin(p *Plugin) *Plugin {
	if p == nil {
		return nil
	}
	cp := *p
	return &cp
}

// CloneScript 返回 s 的深拷贝。
func CloneScript(s *Script) *Script {
	if s == nil {
		return nil
	}
	cp := *s
	return &cp
}

// CloneScriptExecution 返回 e 的深拷贝（含 FinishedAt 指针）。
func CloneScriptExecution(e *ScriptExecution) *ScriptExecution {
	if e == nil {
		return nil
	}
	cp := *e
	if e.FinishedAt != nil {
		ft := *e.FinishedAt
		cp.FinishedAt = &ft
	}
	return &cp
}

// CloneServiceInstance 返回 inst 的深拷贝（含 Metadata map）。
// nil 返回 nil。用于读路径返回副本、写路径入 map 前拷贝。
func CloneServiceInstance(inst *ServiceInstance) *ServiceInstance {
	if inst == nil {
		return nil
	}
	cp := *inst
	if inst.Metadata != nil {
		cp.Metadata = make(map[string]string, len(inst.Metadata))
		for k, v := range inst.Metadata {
			cp.Metadata[k] = v
		}
	}
	return &cp
}

// CloneSLO 返回 slo 的深拷贝（含 SLIs 切片）。
// 用于 GetSLO / ListSLOs / CreateSLO / UpdateSLO 返回，
// 避免外部修改破坏内部状态。
func CloneSLO(slo *SLO) *SLO {
	if slo == nil {
		return nil
	}
	cp := *slo
	if slo.SLIs != nil {
		cp.SLIs = append([]SLI(nil), slo.SLIs...)
	}
	return &cp
}

// CloneSubscription 返回 s 的深拷贝。
func CloneSubscription(s *Subscription) *Subscription {
	if s == nil {
		return nil
	}
	cp := *s
	return &cp
}

// CloneTenant 返回 t 的深拷贝（值类型字段浅拷贝即深拷贝）。
func CloneTenant(t *Tenant) *Tenant {
	if t == nil {
		return nil
	}
	cp := *t
	return &cp
}

// CloneTicket 返回 t 的深拷贝（含 Tags / ResolvedAt 指针）。
// 用于 GetTicket / ListTickets / CreateTicket / UpdateTicket / CloseTicket 返回，
// 避免外部修改破坏内部状态。
func CloneTicket(t *Ticket) *Ticket {
	if t == nil {
		return nil
	}
	cp := *t
	if t.Tags != nil {
		cp.Tags = append([]string(nil), t.Tags...)
	}
	if t.ResolvedAt != nil {
		rt := *t.ResolvedAt
		cp.ResolvedAt = &rt
	}
	return &cp
}

// CloneWebhook 返回 wh 的深拷贝（含 Events / Headers）。
func CloneWebhook(wh *Webhook) *Webhook {
	if wh == nil {
		return nil
	}
	cp := *wh
	if wh.Events != nil {
		cp.Events = append([]string(nil), wh.Events...)
	}
	if wh.Headers != nil {
		cp.Headers = make(map[string]string, len(wh.Headers))
		for k, v := range wh.Headers {
			cp.Headers[k] = v
		}
	}
	return &cp
}

// CloneWebhookDelivery 返回 d 的深拷贝。
func CloneWebhookDelivery(d *WebhookDelivery) *WebhookDelivery {
	if d == nil {
		return nil
	}
	cp := *d
	return &cp
}

// SortServiceInstances 按 ServiceID 升序排序（稳定输出，便于测试断言）。
// 小规模数据用插入排序，避免引入 sort 包的额外依赖。
func SortServiceInstances(s []*ServiceInstance) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1].ServiceID > s[j].ServiceID; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
