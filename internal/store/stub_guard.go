
// stub_guard.go SQLStore 桩方法的统一告警入口（H2 止血措施）。
//
// 背景：SQLStore 对 Phase 1-6 领域（ticket/slo/traffic/.../billing 共 15 个）
// 的方法是未接入 MySQL 的桩。桩的接口签名绝大多数无 error 返回值
// （改签名爆炸半径 >2000 行，见 docs/design/FIXPLAN-phase1-6.md §2.2.1），
// 因此桩策略收敛为「返回约定零值 + 统一限频告警」，让失败可见而非假装成功：
//   - Create 类一律返回 nil（杜绝「201 假成功 → GET 404 → 审计已记成功」链路）；
//   - Get/Update/Delete/Enable/Disable 类返回 nil,false / false；
//   - List 类返回非 nil 空切片（防上层 range panic）；
//   - 每次进入桩方法先经 StubNotImplemented 打点告警。
//
// 告警语义：每 (domain, method) 组合首次调用必打 WARN；之后同一 key 在
// stubLogInterval 窗口内限频（60s 至多一条），避免高频路径刷屏。
package store

import (
	"context"
	"sync"
	"time"

	"opsmesh/internal/logx"
)

// stubLogInterval 同一 (domain, method) 告警的限频窗口。
const stubLogInterval = 60 * time.Second

// stubLastLog 记录每个 (domain+"/"+method) 上次告警的时间戳（UnixNano，int64）。
// sync.Map 读多写少（key 集合固定、命中路径无锁竞争热点），适配本场景。
var stubLastLog sync.Map

// StubNotImplemented 统一桩入口：限频告警 + 返回约定零值由调用方负责。
//
// domain 如 "ticket"，method 如 "CreateTicket"。每 (domain,method) 首次必打，
// 之后同 key 每 60s 限频一次（sync.Map 记录 lastLog 时间戳，避免刷屏）。
func StubNotImplemented(domain, method string) {
	key := domain + "/" + method
	now := time.Now().UnixNano()
	if v, ok := stubLastLog.Load(key); ok {
		// 已有记录：窗口内静默；窗口外尝试 CAS 推进时间戳，
		// CAS 失败说明并发下另一 goroutine 刚刷新过，本调用静默即可。
		if last, _ := v.(int64); now-last < int64(stubLogInterval) {
			return
		}
		if !stubLastLog.CompareAndSwap(key, v, now) {
			return
		}
	} else {
		// 首次记录：LoadOrStore 保证并发首调只有胜方打日志。
		if _, loaded := stubLastLog.LoadOrStore(key, now); loaded {
			return
		}
	}
	logx.Warn(context.Background(),
		"store: 桩方法未实现，SQL 后端该领域数据不持久化（调用将得到零值而非落库结果）",
		"domain", domain, "method", method,
		"hint", "如需生产使用该领域请等待 MySQL 持久化落地，或显式 --allow-stub-stores 接受桩限制")
}

// StubDomains 未持久化领域清单（P1-P6 共 15 个），与 15 个 sql_*.go 桩文件一一对应。
// 供 SQL/MultiSchema 构造函数启动告警使用；internal/config 的 Validate 错误信息
// 维护了同一名单的字面量副本，两处新增领域时须同步更新。
var StubDomains = []string{
	"ticket", "slo", "traffic", "pipeline", "argocd",
	"compliance", "backup", "network", "automation", "webhook",
	"script", "tenant", "apikey", "plugin", "billing",
}

// WarnStubStoreDomains 构造函数接线：检测到 SQL 后端时输出一条总述性 WARN，
// 声明 P1-P6 领域在该后端未持久化（H3 缓解：让空壳可见，而非运行期静默失效）。
// component 为日志定位前缀（如 "sql" / "multi-schema"）；内存后端不调用本函数。
func WarnStubStoreDomains(component string) {
	logx.Warn(context.Background(),
		component+": 以下领域 P1-P6 在 SQL 后端未持久化（桩实现，写入返回零值）",
		"domains", joinStubDomains(), "count", len(StubDomains),
		"hint", "生产模式默认拒绝启动；开发模式可继续但相关 API 将不可持久化")
}

// joinStubDomains 把领域清单拼成逗号分隔串（仅告警展示用）。
func joinStubDomains() string {
	out := ""
	for i, d := range StubDomains {
		if i > 0 {
			out += ","
		}
		out += d
	}
	return out
}