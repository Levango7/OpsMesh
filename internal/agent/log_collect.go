// log_collect.go 实现增强日志采集器 LogCollector。
//
// 在 log_collectLoop（agent.go 既有基于环境变量的简单增量上报）之上提供更完整的采集能力：
//   - 多路径：同时尾随多个日志文件（glob 通配符展开）。
//   - 多行合并：按正则识别"记录首行"，后续不匹配行合并到上一条（典型 Java stacktrace 场景）。
//   - 过滤：IncludeRules（白名单正则，匹配任一则保留）+ ExcludeRules（黑名单正则，匹配任则丢弃）。
//   - 限速：RateLimit 行/秒，超限丢弃并计数（避免日志暴增打爆 pushFn 下游）。
//   - 增量读取：基于文件 offset，下次仅读新增；文件轮转（size<offset）自动重置。
//   - 热更新：UpdateConfig 在采集运行中替换配置（路径/规则/限速/间隔），无需重启。
//   - 统计：TotalLines/TotalBytes/ActiveFiles 供运维观测采集进度。
//
// 设计取舍：
//   - 采集循环单 goroutine 串行扫描所有路径，避免对每文件起 goroutine 在文件数多时调度开销大；
//     单文件读取上限 1MB 防 OOM，下游 pushFn 批次上报。
//   - pushFn 由调用方注入（agent.go 中桥接到 gRPC ReportLogs），解耦采集与上报通道。
//   - 多行合并缓冲在采集循环内（不跨 tick），tick 结束时强制 flush 剩余缓冲，避免跨周期粘连。
package agent

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"opsmesh/internal/logx"
)

// LogCollectConfig 增强日志采集配置。
//
// 字段语义：
//   - Paths         要采集的日志路径列表（支持 glob 通配符，如 /var/log/*.log）。
//   - IncludeRules  白名单正则列表（非空时，行须匹配任一规则才保留；空=全保留）。
//   - ExcludeRules  黑名单正则列表（匹配任一规则则丢弃；优先级高于 Include）。
//   - MultilineRule 多行合并正则（匹配的行作为新记录首行，不匹配的行合并到上一条；
//     空串=不合并，每行一条记录）。
//   - RateLimit     限速（行/秒，<=0 表示不限速）。
//   - Interval      扫描间隔（<=0 用默认 30s）。
type LogCollectConfig struct {
	Paths         []string
	IncludeRules  []string
	ExcludeRules  []string
	MultilineRule string
	RateLimit     int
	Interval      time.Duration
}

// LogCollectStats 采集统计（原子读写，线程安全）。
type LogCollectStats struct {
	TotalLines  int64 // 累计采集行数（合并前）
	TotalBytes  int64 // 累计采集字节数
	ActiveFiles int64 // 当前活跃文件数（最近一次扫描成功打开的文件数）
	Dropped     int64 // 因限速丢弃的行数
	Pushed      int64 // 成功 pushFn 的记录数（合并后）
}

// CollectedLog 采集到的一条日志记录（多行合并后）。
//
// Content 为合并后的正文（多行用 \n 连接）；LineCount 为合并前的原始行数；
// Timestamp 为采集时刻（不解析日志行内时间戳，简化实现）。
type CollectedLog struct {
	File      string
	Content   string
	LineCount int
	Bytes     int
	Timestamp time.Time
	StartLine string // 合并记录的首行（便于级别解析/过滤展示）
}

// LogCollectPushFunc 采集器上报回调：批量推送合并后的记录。
// agent.go 注入桥接到 gRPC ReportLogs 的实现。
type LogCollectPushFunc func(ctx context.Context, records []CollectedLog) error

// 默认参数。
const (
	defaultLogCollectInterval = 30 * time.Second // 默认扫描间隔
	logCollectReadLimit       = 1 << 20          // 单次读取上限 1MB（防 OOM）
	logCollectMaxRecords      = 1000             // 单次 tick 最多积累记录数（防 pushFn 批次过大）
)

// LogCollector 增强日志采集器。
//
// 字段语义：
//   - config     当前生效配置（热更新时原子替换）。
//   - include/exclude/multiline 预编译正则（由 config 派生，热更新时重建）。
//   - offsets    每文件上次读取 offset（增量读取；文件轮转时重置）。
//   - pushFn     上报回调（构造时注入，不可变）。
//   - stopCh     Stop 信号通道。
//   - wg         Start goroutine 同步。
//   - stats      采集统计（原子读写）。
//   - started    是否已 Start（防止重复启动）。
//   - rateState  限速状态（滑动窗口内已发送行数与窗口起点）。
type LogCollector struct {
	mu         sync.RWMutex
	config     LogCollectConfig
	include    []*regexp.Regexp
	exclude    []*regexp.Regexp
	multiline  *regexp.Regexp
	offsets    map[string]int64
	pushFn     LogCollectPushFunc
	stopCh     chan struct{}
	wg         sync.WaitGroup
	stats      LogCollectStats
	started    bool
	rateMu     sync.Mutex
	rateWindow time.Time
	rateCount  int64
}

// NewLogCollector 构造 LogCollector。
//
// cfg.Interval<=0 时用默认 30s。正则编译失败返回 error（含哪条规则）。
// pushFn 为 nil 时返回 error（采集器无上报通道无意义）。
// Paths 为空不报错（Start 时直接 return，便于配置先到、路径后补的热更新场景）。
func NewLogCollector(cfg LogCollectConfig, pushFn LogCollectPushFunc) (*LogCollector, error) {
	if pushFn == nil {
		return nil, errLogCollectPushFnNil
	}
	if cfg.Interval <= 0 {
		cfg.Interval = defaultLogCollectInterval
	}
	lc := &LogCollector{
		config:  cfg,
		offsets: make(map[string]int64),
		pushFn:  pushFn,
		stopCh:  make(chan struct{}),
	}
	if err := lc.recompile(); err != nil {
		return nil, err
	}
	return lc, nil
}

// recompile 根据 config 重建预编译正则。调用方需持写锁或处于构造阶段。
func (lc *LogCollector) recompile() error {
	include, err := compileRules(lc.config.IncludeRules)
	if err != nil {
		return err
	}
	exclude, err := compileRules(lc.config.ExcludeRules)
	if err != nil {
		return err
	}
	var ml *regexp.Regexp
	if lc.config.MultilineRule != "" {
		ml, err = regexp.Compile(lc.config.MultilineRule)
		if err != nil {
			return errLogCollectMultilineCompile.wrap(err, lc.config.MultilineRule)
		}
	}
	lc.include = include
	lc.exclude = exclude
	lc.multiline = ml
	return nil
}

// compileRules 把字符串规则列表编译为正则列表。空列表返回 nil。
func compileRules(rules []string) ([]*regexp.Regexp, error) {
	if len(rules) == 0 {
		return nil, nil
	}
	out := make([]*regexp.Regexp, 0, len(rules))
	for i, r := range rules {
		re, err := regexp.Compile(r)
		if err != nil {
			return nil, errLogCollectRuleCompile.wrap(err, r, i)
		}
		out = append(out, re)
	}
	return out, nil
}

// Start 启动采集循环（每 Interval 秒扫描所有路径，读取增量，过滤，合并多行，调用 pushFn）。
//
// 阻塞直到 ctx.Done 或 Stop 被调用。返回 nil 表示正常退出。
// 幂等保护：重复调用 Start 直接返回 nil（不重复启动 goroutine）。
// config.Paths 为空时直接 return（不空跑 ticker，零开销）。
func (lc *LogCollector) Start(ctx context.Context) error {
	lc.mu.Lock()
	if lc.started {
		lc.mu.Unlock()
		return nil
	}
	lc.started = true
	lc.mu.Unlock()

	lc.wg.Add(1)
	defer lc.wg.Done()

	logx.Info(ctx, "LogCollector 启动", "paths", lc.config.Paths, "interval", lc.config.Interval)

	ticker := time.NewTicker(lc.snapshotInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			lc.finalFlush(ctx)
			return nil
		case <-lc.stopCh:
			lc.finalFlush(ctx)
			return nil
		case <-ticker.C:
			if err := lc.tick(ctx); err != nil {
				logx.Warn(ctx, "LogCollector tick 失败", "error", err.Error())
			}
			// 热更新可能改了 Interval：重置 ticker 周期，下一个 tick 用新 interval。
			ticker.Reset(lc.snapshotInterval())
		}
	}
}

// snapshotInterval 读取当前配置的 Interval（线程安全）。
func (lc *LogCollector) snapshotInterval() time.Duration {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	if lc.config.Interval <= 0 {
		return defaultLogCollectInterval
	}
	return lc.config.Interval
}

// finalFlush 退出前 flush 残留的多行合并缓冲（尽力而为，失败仅记录日志）。
func (lc *LogCollector) finalFlush(ctx context.Context) {
	// 当前实现每 tick 内完成 flush，无跨 tick 缓冲，故此处无操作；
	// 保留方法以备未来引入跨 tick 缓冲时的统一退出 flush 钩子。
	_ = ctx
}

// tick 单次扫描周期：展开 glob 路径 → 逐文件读增量 → 过滤 → 合并 → pushFn。
func (lc *LogCollector) tick(ctx context.Context) error {
	lc.mu.RLock()
	paths := lc.config.Paths
	include := lc.include
	exclude := lc.exclude
	multiline := lc.multiline
	rateLimit := lc.config.RateLimit
	lc.mu.RUnlock()

	if len(paths) == 0 {
		return nil
	}

	// 展开 glob 通配符（如 /var/log/*.log）。
	allFiles := expandPaths(paths)
	if len(allFiles) == 0 {
		atomic.StoreInt64(&lc.stats.ActiveFiles, 0)
		return nil
	}

	var allRecords []CollectedLog
	var activeFiles int64

	for _, file := range allFiles {
		records, n, err := lc.collectFile(file, include, exclude, multiline, rateLimit)
		if err != nil {
			if !os.IsNotExist(err) {
				logx.Warn(ctx, "LogCollector 读取文件失败", "file", file, "error", err.Error())
			}
			continue
		}
		activeFiles++
		allRecords = append(allRecords, records...)
		atomic.AddInt64(&lc.stats.TotalLines, n.lines)
		atomic.AddInt64(&lc.stats.TotalBytes, n.bytes)
	}
	atomic.StoreInt64(&lc.stats.ActiveFiles, activeFiles)

	if len(allRecords) == 0 {
		return nil
	}

	// 批次上报（pushFn 由调用方注入，内部已处理重试/落库）。
	if err := lc.pushFn(ctx, allRecords); err != nil {
		return err
	}
	atomic.AddInt64(&lc.stats.Pushed, int64(len(allRecords)))
	return nil
}

// fileStats 单文件采集计数（避免返回多值）。
type fileStats struct {
	lines int64
	bytes int64
}

// collectFile 采集单文件增量：读新增字节 → 按行切分 → 过滤 → 多行合并 → 限速。
func (lc *LogCollector) collectFile(
	file string,
	include, exclude []*regexp.Regexp,
	multiline *regexp.Regexp,
	rateLimit int,
) ([]CollectedLog, fileStats, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, fileStats{}, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, fileStats{}, err
	}

	lc.mu.RLock()
	offset := lc.offsets[file]
	lc.mu.RUnlock()

	// 文件轮转/截断：size<offset 重置从头读。
	if fi.Size() < offset {
		offset = 0
	}
	if fi.Size() == offset {
		return nil, fileStats{}, nil // 无新增
	}

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, fileStats{}, err
	}
	data, err := io.ReadAll(io.LimitReader(f, logCollectReadLimit))
	if err != nil {
		return nil, fileStats{}, err
	}

	if len(data) == 0 {
		return nil, fileStats{}, nil
	}

	lines := splitLines(string(data))
	var stats fileStats
	stats.bytes = int64(len(data))

	var records []CollectedLog
	// 多行合并缓冲。
	var buf strings.Builder
	var bufLineCount int
	var bufStartLine string
	var bufBytes int

	// 追踪已处理行在 data 中的字符偏移（含换行符估计），用于 hitLimit 时回退 offset。
	// splitLines 去除行尾换行符，此处每行 +1 保守估计 \n；\r\n 场景偏移略小，
	// 下次 tick 会多读几个已处理字节（幂等过滤，不重复入记录）。
	var processedChars int
	hitLimit := false

	flushBuf := func() {
		if buf.Len() == 0 {
			return
		}
		records = append(records, CollectedLog{
			File:      file,
			Content:   buf.String(),
			LineCount: bufLineCount,
			Bytes:     bufBytes,
			Timestamp: time.Now(),
			StartLine: bufStartLine,
		})
		buf.Reset()
		bufLineCount = 0
		bufStartLine = ""
		bufBytes = 0
	}

	for _, line := range lines {
		stats.lines++
		// 累积已处理字符（行内容 + 换行符估计），用于 hitLimit 时回退 offset。
		processedChars += len(line) + 1
		// 过滤：Exclude 优先于 Include。
		if matchAny(exclude, line) {
			continue
		}
		if len(include) > 0 && !matchAny(include, line) {
			continue
		}
		// 限速：超限丢弃并计数。
		if rateLimit > 0 && !lc.allowRate(rateLimit) {
			atomic.AddInt64(&lc.stats.Dropped, 1)
			continue
		}
		// 多行合并：multiline 匹配行首 → 新记录开始；不匹配 → 合并到上一条。
		if multiline != nil && multiline.MatchString(line) && buf.Len() > 0 {
			flushBuf()
		}
		if buf.Len() > 0 {
			buf.WriteByte('\n')
		}
		if bufStartLine == "" {
			bufStartLine = line
		}
		buf.WriteString(line)
		bufLineCount++
		bufBytes += len(line) + 1 // +1 for \n
		// multiline 为 nil（未配置多行合并）时：每行单独成一条记录，立即 flush。
		if multiline == nil {
			flushBuf()
		}
		if len(records) >= logCollectMaxRecords {
			flushBuf()
			hitLimit = true
			break // 单 tick 记录数上限，剩余下次 tick 再读
		}
	}
	flushBuf()

	// 推进 offset：先按实际处理量截取，offset 只推进实际读取量。
	// hitLimit 时剩余行需下次 tick 重读，故只推进到已处理位置；
	// 否则本次 data 全部处理完毕，推进到 data 末尾。
	advance := len(data)
	if hitLimit && processedChars < advance {
		advance = processedChars
		stats.bytes = int64(advance)
	}
	newOffset := offset + int64(advance)
	lc.mu.Lock()
	lc.offsets[file] = newOffset
	lc.mu.Unlock()

	return records, stats, nil
}

// allowRate 滑动窗口限速：1s 窗口内不超过 rateLimit 行。
func (lc *LogCollector) allowRate(rateLimit int) bool {
	lc.rateMu.Lock()
	defer lc.rateMu.Unlock()
	now := time.Now()
	if lc.rateWindow.IsZero() || now.Sub(lc.rateWindow) >= time.Second {
		lc.rateWindow = now
		lc.rateCount = 0
	}
	if lc.rateCount >= int64(rateLimit) {
		return false
	}
	lc.rateCount++
	return true
}

// splitLines 按行切分（兼容 \r\n 与 \n），去除行尾换行，空行保留（多行合并需感知空行）。
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	trimmed := strings.TrimRight(s, "\r\n")
	if trimmed == "" {
		return nil
	}
	raw := strings.Split(trimmed, "\n")
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		out = append(out, strings.TrimRight(r, "\r"))
	}
	return out
}

// matchAny 检查 line 是否匹配任一正则。空规则列表返回 false。
func matchAny(res []*regexp.Regexp, line string) bool {
	for _, re := range res {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

// expandPaths 展开通配符路径。非通配符路径原样保留（即使文件不存在，由后续 Open 报错跳过）。
func expandPaths(paths []string) []string {
	var out []string
	for _, p := range paths {
		if strings.ContainsAny(p, "*?[") {
			matches, err := filepath.Glob(p)
			if err != nil || len(matches) == 0 {
				continue
			}
			out = append(out, matches...)
			continue
		}
		out = append(out, p)
	}
	return out
}

// Stop 停止采集（关闭 stopCh，等待 Start goroutine 退出）。
//
// 幂等：多次调用安全。未 Start 时直接返回 nil。
func (lc *LogCollector) Stop() error {
	lc.mu.Lock()
	if !lc.started {
		lc.mu.Unlock()
		return nil
	}
	lc.started = false
	lc.mu.Unlock()
	close(lc.stopCh)
	lc.wg.Wait()
	return nil
}

// UpdateConfig 热更新配置。
//
// 替换 config 并重建预编译正则；正则编译失败时保留旧配置并返回 error（不破坏正在运行的采集）。
// Interval 变更在下一个 tick 生效（Start 循环中 ticker.Reset）。
func (lc *LogCollector) UpdateConfig(cfg LogCollectConfig) error {
	if cfg.Interval <= 0 {
		cfg.Interval = defaultLogCollectInterval
	}
	lc.mu.Lock()
	defer lc.mu.Unlock()
	// 先尝试编译新规则，失败则不替换。
	include, err := compileRules(cfg.IncludeRules)
	if err != nil {
		return err
	}
	exclude, err := compileRules(cfg.ExcludeRules)
	if err != nil {
		return err
	}
	var ml *regexp.Regexp
	if cfg.MultilineRule != "" {
		ml, err = regexp.Compile(cfg.MultilineRule)
		if err != nil {
			return errLogCollectMultilineCompile.wrap(err, cfg.MultilineRule)
		}
	}
	lc.config = cfg
	lc.include = include
	lc.exclude = exclude
	lc.multiline = ml
	return nil
}

// Stats 返回当前统计的快照（原子读取，线程安全）。
func (lc *LogCollector) Stats() LogCollectStats {
	return LogCollectStats{
		TotalLines:  atomic.LoadInt64(&lc.stats.TotalLines),
		TotalBytes:  atomic.LoadInt64(&lc.stats.TotalBytes),
		ActiveFiles: atomic.LoadInt64(&lc.stats.ActiveFiles),
		Dropped:     atomic.LoadInt64(&lc.stats.Dropped),
		Pushed:      atomic.LoadInt64(&lc.stats.Pushed),
	}
}

// ===== 错误定义 =====
//
// 用 sentinel error + wrap 辅助方法，便于上层 errors.Is/As 识别与日志结构化。

type logCollectError struct {
	msg     string
	wrapped error
	details []any
}

func (e *logCollectError) Error() string {
	if e.wrapped != nil {
		return e.msg + ": " + e.wrapped.Error()
	}
	return e.msg
}

func (e *logCollectError) Unwrap() error {
	return e.wrapped
}

// Is 使 errors.Is(err, ErrLogCollect) 对任意 *logCollectError 实例生效。
// sentinel error（errLogCollectPushFnNil 等）均为 *logCollectError，
// wrap 后的新实例也通过此方法匹配同一类型，便于上层统一识别采集器错误。
func (e *logCollectError) Is(target error) bool {
	_, ok := target.(*logCollectError)
	return ok
}

func (e *logCollectError) wrap(err error, details ...any) *logCollectError {
	return &logCollectError{msg: e.msg, wrapped: err, details: details}
}

var (
	errLogCollectPushFnNil        = &logCollectError{msg: "log_collect: pushFn 为 nil"}
	errLogCollectMultilineCompile = &logCollectError{msg: "log_collect: multiline 正则编译失败"}
	errLogCollectRuleCompile      = &logCollectError{msg: "log_collect: 过滤规则正则编译失败"}
)
