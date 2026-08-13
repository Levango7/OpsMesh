// anomaly.go 实现基于基线偏离的异常检测规则（P2-B4）。
//
// 提供两种检测算法：
//   - BaselineDetector：滑动窗口均值/标准差 + Z-Score，适合稳定基线的偏离检测。
//   - EWMADetector：指数加权移动平均 + 方差，对突变敏感，适合检测突然飙升。
//
// AnomalyEngine 管理多条 AnomalyRule，对指标流评估，异常时产生 AnomalyAlert。
// 线程安全：所有检测器与引擎内部以 sync.RWMutex 保护共享状态。
//
// 设计目标：
//   - 自包含：不依赖外部 store/server，可被控制面或 agent 复用。
//   - 统一接口：detector 接口屏蔽 BaselineDetector/EWMADetector 差异。
//   - 多租户隔离：AnomalyRule.TenantID 用于规则过滤（与 AlertRule 一致语义）。
package alertengine

import (
	"math"
	"sync"
	"time"
)

// ============================================================================
// detector 接口：统一 BaselineDetector 与 EWMADetector
// ============================================================================

// detector 异常检测器统一接口。
//
// 屏蔽 BaselineDetector 与 EWMADetector 的差异，使 AnomalyEngine 可用同一 map 管理。
// 所有实现须线程安全。
type detector interface {
	// Add 添加一个数据点，更新内部统计量。
	Add(value float64)
	// IsAnomaly 判断给定值是否异常（如 Z-Score 超过阈值）。
	IsAnomaly(value float64) bool
	// Stats 返回当前统计量（均值/标准差）。EWMA 检测器返回 (ewma, sqrt(ewmaVar))。
	Stats() (float64, float64)
}

// ============================================================================
// BaselineDetector：滑动窗口 + Z-Score
// ============================================================================

// BaselineDetector 基线检测器：维护滑动窗口，计算均值/标准差，用 Z-Score 检测异常。
//
// 算法：
//  1. Add(value) 将 value 追加到滑动窗口，超 maxSize 时淘汰最旧数据。
//  2. 重新计算 mean / stdDev（O(n) 重算，n=maxSize 上限 100，开销可接受）。
//  3. IsAnomaly(value) = |value - mean| / stdDev > threshold（Z-Score 检验）。
//
// 线程安全：mu 保护 window/mean/stdDev。
// 当窗口数据少于 2 个或 stdDev=0 时 IsAnomaly 恒返回 false（无法计算 Z-Score）。
type BaselineDetector struct {
	mu        sync.RWMutex
	window    []float64 // 滑动窗口数据
	maxSize   int       // 窗口大小（默认 100）
	mean      float64   // 缓存均值
	stdDev    float64   // 缓存标准差
	threshold float64   // Z-Score 阈值（默认 3.0，即 3σ）
}

// NewBaselineDetector 构造基线检测器。
//
//   - windowSize：滑动窗口大小，<=0 时默认 100。
//   - threshold：Z-Score 阈值，<=0 时默认 3.0（3σ，约 99.7% 置信区间）。
func NewBaselineDetector(windowSize int, threshold float64) *BaselineDetector {
	if windowSize <= 0 {
		windowSize = 100
	}
	if threshold <= 0 {
		threshold = 3.0
	}
	return &BaselineDetector{
		window:    make([]float64, 0, windowSize),
		maxSize:   windowSize,
		threshold: threshold,
	}
}

// Add 添加数据点，更新统计量。
//
// 窗口满后淘汰最旧数据（FIFO），保持窗口大小不超过 maxSize。
// 添加后重新计算 mean/stdDev（缓存以避免 IsAnomaly 时重算）。
func (b *BaselineDetector) Add(value float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.window) >= b.maxSize {
		// 淘汰最旧：左移一位后追加
		b.window = b.window[1:]
	}
	b.window = append(b.window, value)
	b.recomputeLocked()
}

// recomputeLocked 重新计算 mean/stdDev（调用方须持写锁）。
func (b *BaselineDetector) recomputeLocked() {
	n := len(b.window)
	if n == 0 {
		b.mean = 0
		b.stdDev = 0
		return
	}
	var sum float64
	for _, v := range b.window {
		sum += v
	}
	mean := sum / float64(n)
	var sqSum float64
	for _, v := range b.window {
		d := v - mean
		sqSum += d * d
	}
	// 总体标准差（n 而非 n-1）：窗口数据视为完整总体而非样本。
	// n=1 时 stdDev=0，IsAnomaly 将返回 false（单点无法判断偏离）。
	variance := sqSum / float64(n)
	b.mean = mean
	b.stdDev = math.Sqrt(variance)
}

// IsAnomaly 判断是否异常（|Z-Score| > threshold）。
//
// 当窗口数据少于 2 个、stdDev=0 时返回 false（无法计算 Z-Score）。
// 使用绝对值（|Z|）判断，即双向偏离均视为异常（高于或低于基线都触发）。
func (b *BaselineDetector) IsAnomaly(value float64) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if len(b.window) < 2 {
		return false
	}
	if b.stdDev == 0 {
		return false
	}
	zscore := math.Abs(value-b.mean) / b.stdDev
	return zscore > b.threshold
}

// Stats 返回当前统计量（均值、标准差、数据点数）。
func (b *BaselineDetector) Stats() (mean, stdDev float64, count int) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.mean, b.stdDev, len(b.window)
}

// 确保 BaselineDetector 实现 detector 接口（编译期检查）。
// Stats 签名 (float64, float64, int) 与 detector.Stats (float64, float64) 不同，
// 故提供适配方法 stats2 并在 NewAnomalyEngine 中包装。
func (b *BaselineDetector) stats2() (float64, float64) {
	mean, stdDev, _ := b.Stats()
	return mean, stdDev
}

// ============================================================================
// EWMADetector：指数加权移动平均 + 方差
// ============================================================================

// EWMADetector EWMA（指数加权移动平均）检测器：对突变敏感，适合检测突然飙升。
//
// 算法：
//  1. 首次 Add(value)：ewma = value, ewmaVar = 0。
//  2. 后续 Add(value)：
//     diff = value - ewma
//     ewma = ewma + alpha * diff
//     ewmaVar = (1 - alpha) * (ewmaVar + alpha * diff * diff)
//  3. IsAnomaly(value) = |value - ewma| / sqrt(ewmaVar) > threshold。
//
// alpha 越大越敏感（新数据权重高），越小越平滑（历史权重高）。
// 线程安全：mu 保护 ewma/ewmaVar/initialized。
type EWMADetector struct {
	mu          sync.RWMutex
	alpha       float64 // 平滑系数（0<alpha<1，默认 0.3，越大越敏感）
	ewma        float64 // 当前 EWMA 值
	ewmaVar     float64 // 当前 EWMA 方差
	threshold   float64 // 异常阈值（默认 3.0）
	initialized bool    // 是否已初始化（首次 Add 后置 true）
}

// NewEWMADetector 构造 EWMA 检测器。
//
//   - alpha：平滑系数，<=0 或 >=1 时默认 0.3。
//   - threshold：异常阈值，<=0 时默认 3.0。
func NewEWMADetector(alpha, threshold float64) *EWMADetector {
	if alpha <= 0 || alpha >= 1 {
		alpha = 0.3
	}
	if threshold <= 0 {
		threshold = 3.0
	}
	return &EWMADetector{
		alpha:     alpha,
		threshold: threshold,
	}
}

// Add 更新 EWMA 和方差。
//
// 首次调用初始化 ewma=value, ewmaVar=0；后续按 EWMA 递推公式更新。
func (e *EWMADetector) Add(value float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.initialized {
		e.ewma = value
		e.ewmaVar = 0
		e.initialized = true
		return
	}
	diff := value - e.ewma
	e.ewma = e.ewma + e.alpha*diff
	e.ewmaVar = (1 - e.alpha) * (e.ewmaVar + e.alpha*diff*diff)
}

// IsAnomaly 判断是否异常（|value - ewma| / sqrt(ewmaVar) > threshold）。
//
// 未初始化或 ewmaVar=0 时返回 false（无法计算）。
// 使用绝对值判断（双向偏离均视为异常）。
func (e *EWMADetector) IsAnomaly(value float64) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if !e.initialized {
		return false
	}
	if e.ewmaVar <= 0 {
		return false
	}
	stdDev := math.Sqrt(e.ewmaVar)
	if stdDev == 0 {
		return false
	}
	zscore := math.Abs(value-e.ewma) / stdDev
	return zscore > e.threshold
}

// Stats 返回当前 EWMA 值与标准差（sqrt(ewmaVar)）。
func (e *EWMADetector) Stats() (ewma, stdDev float64) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if !e.initialized {
		return 0, 0
	}
	return e.ewma, math.Sqrt(e.ewmaVar)
}

// ============================================================================
// AnomalyRule / AnomalyAlert / AnomalyEngine
// ============================================================================

// AnomalyRule 异常检测规则：关联指标名 + 检测器 + 告警级别。
//
// 字段语义：
//   - ID：规则唯一 ID（租户内唯一）。
//   - MetricName：指标名（如 "cpu_usage"、"mem_usage"），与设备指标字段对应。
//   - DeviceID：设备 ID，空表示对所有设备生效（评估时 deviceID 不参与匹配）。
//   - Detector：检测器类型，"baseline" 或 "ewma"。
//   - WindowSize：baseline 窗口大小（Detector="ewma" 时忽略）。
//   - Threshold：异常阈值（Z-Score 或 EWMA Z-Score）。
//   - Severity：告警级别，"critical" 或 "warning"。
//   - TenantID：租户隔离（与 AlertRule.TenantID 一致语义）。
type AnomalyRule struct {
	ID         string  // 规则 ID
	MetricName string  // 指标名（如 "cpu_usage"、"mem_usage"）
	DeviceID   string  // 设备 ID（空=所有设备）
	Detector   string  // "baseline" | "ewma"
	WindowSize int     // baseline 窗口大小
	Threshold  float64 // 异常阈值
	Severity   string  // "critical" | "warning"
	TenantID   string  // 租户隔离
}

// AnomalyAlert 异常告警。
//
// 由 AnomalyEngine.Evaluate 在检测到异常时构造返回。
// ZScore 为实际计算的 Z 值（带符号，正=高于基线，负=低于基线），便于调用方区分方向。
type AnomalyAlert struct {
	RuleID     string    // 触发的规则 ID
	MetricName string    // 指标名
	DeviceID   string    // 设备 ID
	Value      float64   // 触发时的指标值
	Mean       float64   // 检测器均值（EWMA 时为 ewma）
	StdDev     float64   // 检测器标准差
	ZScore     float64   // 实际 Z-Score（带符号）
	Severity   string    // 告警级别
	Timestamp  time.Time // 触发时刻
}

// AnomalyEngine 异常检测引擎：管理多条规则，对指标流评估，异常时产生告警。
//
// 内部为每条规则构造一个 detector 实例（按 rule.Detector 类型选择 Baseline/EWMA），
// 评估时按 (metricName, deviceID) 匹配规则，调用 detector.Add + IsAnomaly。
//
// 线程安全：rules/detectors 经 mu 保护。
// 注意：detector 实例自身线程安全，但同一规则多次 Evaluate 串行调用 Add/IsAnomaly
// 才能保证统计量递推正确（并发调用同一规则会被 detector 内部锁串行化，结果正确）。
type AnomalyEngine struct {
	mu        sync.RWMutex
	rules     map[string]*AnomalyRule // 按 ID 索引
	detectors map[string]detector     // key = ruleID
	now       func() time.Time        // 可注入时钟（便于测试）
}

// NewAnomalyEngine 构造异常检测引擎。
// now 为 nil 时使用 time.Now。
func NewAnomalyEngine() *AnomalyEngine {
	return &AnomalyEngine{
		rules:     make(map[string]*AnomalyRule),
		detectors: make(map[string]detector),
		now:       time.Now,
	}
}

// NewAnomalyEngineWithClock 构造带可注入时钟的引擎（测试用）。
func NewAnomalyEngineWithClock(now func() time.Time) *AnomalyEngine {
	if now == nil {
		now = time.Now
	}
	return &AnomalyEngine{
		rules:     make(map[string]*AnomalyRule),
		detectors: make(map[string]detector),
		now:       now,
	}
}

// baselineAdapter 将 BaselineDetector.Stats (3 返回值) 适配为 detector.Stats (2 返回值)。
type baselineAdapter struct {
	*BaselineDetector
}

func (a *baselineAdapter) Stats() (float64, float64) {
	return a.BaselineDetector.stats2()
}

// ewmaAdapter EWMADetector 已天然满足 detector 接口，包装仅为统一构造路径。
type ewmaAdapter struct {
	*EWMADetector
}

// buildDetector 按 AnomalyRule 配置构造对应检测器。
//
//   - Detector="baseline"：构造 BaselineDetector（WindowSize/Threshold）。
//   - Detector="ewma"：构造 EWMADetector（alpha=0.3 固定，Threshold）。
//   - 未知类型：默认 baseline（防御式）。
func buildDetector(rule *AnomalyRule) detector {
	switch rule.Detector {
	case "ewma":
		return &ewmaAdapter{NewEWMADetector(0.3, rule.Threshold)}
	default: // "baseline" 或未知
		return &baselineAdapter{NewBaselineDetector(rule.WindowSize, rule.Threshold)}
	}
}

// AddRule 添加规则 + 构造对应检测器。
//
// 若规则 ID 已存在则覆盖（与 Engine.AddRule 不同，此处幂等语义便于配置热更新）。
// rule 为 nil 时静默返回。
func (e *AnomalyEngine) AddRule(rule *AnomalyRule) {
	if rule == nil {
		return
	}
	d := buildDetector(rule)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules[rule.ID] = rule
	e.detectors[rule.ID] = d
}

// RemoveRule 删除规则及其检测器。
// ID 不存在时静默返回。
func (e *AnomalyEngine) RemoveRule(ruleID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.rules, ruleID)
	delete(e.detectors, ruleID)
}

// Evaluate 评估指标，异常时返回告警。
//
// 流程：
//  1. 遍历所有规则，匹配 MetricName 与 DeviceID（DeviceID 空匹配所有设备）。
//  2. 调用 detector.IsAnomaly(value) 判断是否异常（基于当前统计量，不含本次值）。
//  3. 调用 detector.Add(value) 更新统计量（无论是否异常都更新，使基线持续适应）。
//  4. 异常时构造 AnomalyAlert（含 ZScore，基于更新前的统计量计算）返回。
//  5. 多条规则匹配时返回首个触发的告警（按规则 ID 升序遍历保证确定性）。
//  6. 无匹配规则或无异常时返回 nil。
//
// 注意：先 IsAnomaly 再 Add 是有意为之的"先判后加"语义：
//   - 异常判定基于历史基线，不被当前值污染，避免 EWMA 检测器因 Add 拉高 ewma/ewmaVar
//     而稀释 Z-Score（EWMA 对当前值权重 alpha=0.3，先 Add 会使 Z-Score 降低约 1/(1+alpha) 倍）。
//   - baseline 模式下窗口较大（100），先 Add 再 IsAnomaly 影响有限，但"先判后加"语义
//     统一两种检测器行为，且更符合"用历史基线判断新值"的直觉。
//   - Add 始终执行（即使异常也更新基线），使持续异常时基线逐渐适应，避免持续告警风暴。
func (e *AnomalyEngine) Evaluate(metricName, deviceID string, value float64) *AnomalyAlert {
	e.mu.RLock()
	// 收集匹配的规则（按 ID 升序，保证多规则触发时返回首个的确定性）。
	type matched struct {
		rule *AnomalyRule
		d    detector
	}
	var matches []matched
	for _, r := range e.rules {
		if r.MetricName != metricName {
			continue
		}
		// DeviceID 空匹配所有设备；非空须精确匹配。
		if r.DeviceID != "" && r.DeviceID != deviceID {
			continue
		}
		d := e.detectors[r.ID]
		if d == nil {
			continue
		}
		matches = append(matches, matched{rule: r, d: d})
	}
	e.mu.RUnlock()

	if len(matches) == 0 {
		return nil
	}

	// 按 ruleID 升序排序保证确定性。
	// 简单插入排序（matches 通常很小，避免引入 sort 包开销）。
	for i := 1; i < len(matches); i++ {
		for j := i; j > 0 && matches[j].rule.ID < matches[j-1].rule.ID; j-- {
			matches[j], matches[j-1] = matches[j-1], matches[j]
		}
	}

	now := e.now()
	for _, m := range matches {
		// 先判后加：基于历史基线判断异常，再更新统计量。
		isAnomaly := m.d.IsAnomaly(value)
		mean, stdDev := m.d.Stats() // 更新前的统计量（用于构造告警）
		m.d.Add(value)              // 更新基线（无论是否异常）
		if !isAnomaly {
			continue
		}
		var zscore float64
		if stdDev > 0 {
			zscore = (value - mean) / stdDev
		}
		return &AnomalyAlert{
			RuleID:     m.rule.ID,
			MetricName: m.rule.MetricName,
			DeviceID:   deviceID,
			Value:      value,
			Mean:       mean,
			StdDev:     stdDev,
			ZScore:     zscore,
			Severity:   m.rule.Severity,
			Timestamp:  now,
		}
	}
	return nil
}

// GetRule 返回规则拷贝（修改返回值不影响引擎内部状态）。
// 不存在时返回 nil。
func (e *AnomalyEngine) GetRule(ruleID string) *AnomalyRule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	r, ok := e.rules[ruleID]
	if !ok {
		return nil
	}
	cp := *r
	return &cp
}

// ListRules 返回所有规则的拷贝（按 ID 升序）。
func (e *AnomalyEngine) ListRules() []*AnomalyRule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]*AnomalyRule, 0, len(e.rules))
	for _, r := range e.rules {
		cp := *r
		out = append(out, &cp)
	}
	// 按 ID 升序排序。
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].ID < out[j-1].ID; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
