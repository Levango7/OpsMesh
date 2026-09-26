// metrics_cache.go — /metrics 应用级计数的短 TTL 缓存（P1-6 可支撑性/性能）。
//
// 背景：GET /metrics 原先每次请求都要做 4 次全量 store 扫描（8080 的
// Snapshot/AllTasks/Alerts/ListTickets）+ 1 次 Agents 全量（9091 渲染前 SetAgents）。
// Prometheus 默认 15~30s 抓一次，且 8080 与 9091 两端口都会抓——即"每个抓取周期
// 对库做 5 次全表读"，规模上去后这本身就是负载来源。这些计数只用于仪表盘读数，
// 不需要逐秒精确，故加一个短 TTL 缓存把同波抓取合并为一次计算。
//
// 失效方向刻意保守：ttl<=0（含 Server 未被 New() 装配的测试场景）即**完全关闭缓存、
// 每次实算**，行为与加这个缓存之前逐字相同。
package controlplane

import (
	"sync"
	"time"
)

// appCounts 是 /metrics 输出的应用级计数快照。
//
// devicesOnline/devicesOffline 是为 DeviceOffline 告警拆出来的：出厂规则引用带 status 标签的
// 序列，而此前全仓只有一处未分状态的设备总数 ⇒ 该告警永远没有可比序列。
// 注意 online+offline 不一定等于 devices：设备还有 unknown 等状态（实测 demo 种子即有差额），
// 刻意不把未知状态塞进 offline —— 那会凭空造出一条"设备掉线"的假告警。
type appCounts struct {
	devices        int
	devicesOnline  int
	devicesOffline int
	tasks          int
	alerts         int
	ticketsOpen    int
	agents         int
}

// appCountsCache 按 TTL 复用一次计算结果。零值可用（ttl=0 → 不缓存）。
type appCountsCache struct {
	mu   sync.Mutex
	ttl  time.Duration
	at   time.Time
	vals appCounts
}

// resolve 返回计数：TTL 内复用缓存，否则调用 compute 重算。
// compute 在锁外无法执行（需要串行化），因此持锁调用——计算期间并发抓取会排队，
// 这正是期望行为（合并风暴，而不是各算一遍）。
func (c *appCountsCache) resolve(compute func() appCounts) appCounts {
	if c == nil || c.ttl <= 0 {
		return compute()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && time.Since(c.at) < c.ttl {
		return c.vals
	}
	v := compute()
	c.vals, c.at = v, time.Now()
	return v
}

// invalidate 清空缓存（测试与"改配置后立即看到新值"的场景用）。
func (c *appCountsCache) invalidate() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.at = time.Time{}
	c.mu.Unlock()
}
