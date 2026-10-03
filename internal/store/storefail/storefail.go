// Package storefail 存储层失败可观测性（自 internal/store 抽出的独立层，TD-61 批次 1）。
//
// 搬迁说明：本包与 store 主包无任何内部依赖（仅标准库），是 god-package 拆分的
// 第一个独立层。父包 internal/store 经 failures_shim.go 以类型别名 + 薄包装回导，
// 保证 327 个 store 内调用点与全部外部引用零改动。
//
// # 为什么需要它
//
// Store 接口的 220 个方法里只有 11 个返回 error——这是当初为桩实现保留的签名
// （见 stub_guard.go 的说明：改签名爆炸半径 >2000 行）。桩早已删除，签名债却留了下来，
// 于是 SQL 后端的写失败长这样：
//
//	func (s *SQLStore) UpsertDevice(d *proto.DeviceInfo) {
//	    if _, err := s.db.ExecContext(...); err != nil {
//	        log.Printf("[store] UpsertDevice 失败 %s: %v", d.DeviceID, err)   // 吞掉
//	    }
//	}
//
// 后果是 HTTP 层无法区分「写成功」与「数据库挂了」：客户端拿到 201 Created，
// 随后的 GET 返回 404，运维侧看到的却是「一切正常」。这是静默数据丢失，不是可见故障。
//
// # 本文件解决什么、不解决什么
//
// 解决：把被吞掉的错误变成**可查询、可告警、可断言**的一等公民——
//   - 累计计数（总量 + 按操作分类）→ 暴露为 Prometheus 指标；
//   - 最近 N 条样本（环形缓冲，内存有界）→ 暴露为管理端点，供定位；
//   - 故障注入测试可直接断言「写失败会被记录」。
//
// 不解决：接口签名本身。把 209 个方法改成返回 error 需要同时改动 3 个实现
// （Memory/SQL/MultiSchema）与全部调用方，是一次独立的、有明确回归面的重构，
// 应当独立评估与排期，不应与可观测性补丁混在同一次改动里。
// 两者是互补关系：本文件让存量吞错点立刻可见，签名重构则从根上消除它们。
package storefail

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// storeLogf 存储层日志出口。
//
// 刻意保持与既有 log.Printf 完全一致的输出格式：本次改动只增加「错误可被记录」这一
// 能力，不改变任何已有日志文本，避免影响依赖日志格式的既有行为与测试。
//
// 已知待办：internal/store 目前全部走标准库 log（362 处），不经 internal/logx，
// 因此这些日志不带 trace_id / tenant_id，无法与请求或 OTel span 关联。
// 迁移到 logx 是一次独立的、可分批的改动，不应与本次安全补丁混做。
func storeLogf(format string, args ...any) {
	log.Printf(format, args...)
}

// FailureRingSize 最近失败样本的保留条数。内存有界，避免长期运行后无上限增长。
const FailureRingSize = 256

// StoreFailure 一条被吞掉的存储层错误样本。
type StoreFailure struct {
	Op       string    `json:"op"`       // 形如 "UpsertDevice"
	TenantID string    `json:"tenantId"` // 仅当 format 里显式写了 tenant=%s 时才有值，否则空
	Context  string    `json:"context"`  // 除末尾错误外的实参渲染结果，如 `tenant=t-1 id=app-3`
	Err      string    `json:"error"`    // 末位实参（错误）的文本
	At       time.Time `json:"at"`
}

// failureSink 进程内的失败记录器。并发安全。
type failureSink struct {
	mu     sync.Mutex
	total  uint64
	byOp   map[string]uint64
	recent [FailureRingSize]StoreFailure
	next   int
	filled bool
}

// defaultFailureSink 全局单例：包级零状态，无初始化顺序问题。
var defaultFailureSink = &failureSink{byOp: make(map[string]uint64)}

// Record 供存储层内部记录一次被吞掉的错误。
//
// 仍会写日志（保留原有可观测行为与 trace 排查线索），额外把错误送进 sink。
// 形参与 log.Printf 完全一致，因此可以机械地把
//
//	log.Printf("[store] ...失败...: %v", args..., err)
//
// 替换为
//
//	Record("[store] ...失败...: %v", args..., err)
//
// 而不改变任何调用点的语义或输出格式。
func Record(format string, args ...any) {
	storeLogf(format, args...)

	// 从 format 里取出操作名（形如 "[store] UpsertDevice 失败 ..."），
	// 使指标可按操作聚合；解析不出就归到 "unknown"，不影响记录本身。
	op := extractOpFromFormat(format)

	f := StoreFailure{Op: op, At: time.Now()}
	n := len(args)

	// 末位实参几乎总是错误本身（这 300 多个调用点的共同形态），先摘出来。
	if n > 0 {
		f.Err = argText(args[n-1])
		args = args[:n-1]
	}

	// 其余实参按 format 里的动词顺序渲染成 context；这比「按位置猜哪个是 tenant、
	// 哪个是实体 ID」可靠——实测调用点里 2 参形态传的是实体、3 参形态才是
	// (tenant, 实体)，位置猜测会在其中一类上稳定取错值，取错的租户比没有租户更糟。
	if len(args) > 0 {
		var b strings.Builder
		for _, a := range args {
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(argText(a))
		}
		f.Context = b.String()
	}

	// 租户只在 format 显式写了 tenant=%s / tenant_id=%s 时才认领：此时动词位置
	// 就是确定的，映射到实参下标不会错。其余格式一律留空而非猜。
	if idx, ok := tenantVerbIndex(format); ok && idx < len(args) {
		f.TenantID = argText(args[idx])
	}

	defaultFailureSink.record(f)
}

// argText 把实参渲染成可读文本：error 取 Error()，其余走 %v。
func argText(a any) string {
	if e, ok := a.(error); ok {
		return e.Error()
	}
	return fmt.Sprintf("%v", a)
}

// verbSpans 返回 format 中每个动词动词的首字节下标，顺序与实参一一对应。
//
// 需要处理标志位/宽度/精度（"%.0fs"、"%03d"）与转义的 "%%"，否则会数错下标，
// 进而把租户号取成别的字段——这正是本函数要避免的那类错误。
func verbSpans(format string) []int {
	var out []int
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		if i+1 < len(format) && format[i+1] == '%' { // 转义 percent，不占实参
			i++
			continue
		}
		j := i + 1
		for j < len(format) && strings.ContainsRune("+-# 0123456789.*", rune(format[j])) {
			j++
		}
		if j >= len(format) { // 畸形 verb，停止，避免越界
			break
		}
		out = append(out, i)
		i = j
	}
	return out
}

// tenantVerbIndex 找出 format 中被显式标注为租户的动词下标（实参下标）。
// 只认 "tenant=" / "tenant_id=" 这类明写标签，不做语义猜测。
func tenantVerbIndex(format string) (int, bool) {
	lower := strings.ToLower(format)
	all := verbSpans(format)
	for _, label := range []string{"tenant_id=", "tenant="} {
		for off := 0; off < len(lower); {
			rel := strings.Index(lower[off:], label)
			if rel < 0 {
				break
			}
			p := off + rel
			off = p + len(label)

			// 标签之后必须紧跟一个动词（中间只允许标志位/宽度/精度），
			// 否则 "tenant=abc" 这类普通文本会被误当成租户实参。
			end := -1
			for _, v := range all {
				if v < off {
					continue
				}
				if strings.IndexFunc(format[off:v], notVerbFlag) < 0 {
					end = v
				}
				break // 只认紧跟其后的第一个动词
			}
			if end >= 0 {
				return len(verbSpans(format[:end])), true
			}
		}
	}
	return 0, false
}

// notVerbFlag 判断字符是否不是 printf 的标志位/宽度/精度字符。
func notVerbFlag(r rune) bool {
	return !strings.ContainsRune("+-# 0123456789.*", r)
}

func (s *failureSink) record(f StoreFailure) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total++
	s.byOp[f.Op]++
	s.recent[s.next] = f
	s.next = (s.next + 1) % FailureRingSize
	if s.next == 0 {
		s.filled = true
	}
}

// extractOpFromFormat 从 "[store] UpsertDevice 失败 xxx" 中取出 "UpsertDevice"。
func extractOpFromFormat(format string) string {
	const prefix = "[store] "
	if len(format) <= len(prefix) || format[:len(prefix)] != prefix {
		return "unknown"
	}
	rest := format[len(prefix):]
	// 停止符：空格、半角冒号，或中文冒号（'：' 占 3 字节，逐字节比较其首字节不可取，
	// 故改为匹配完整子串）。
	for i := 0; i < len(rest); i++ {
		if rest[i] == ' ' || rest[i] == ':' {
			if i == 0 {
				return "unknown"
			}
			return rest[:i]
		}
		if strings.HasPrefix(rest[i:], "：") {
			if i == 0 {
				return "unknown"
			}
			return rest[:i]
		}
	}
	if rest == "" {
		return "unknown"
	}
	return rest
}

// StoreFailureStats 返回累计失败总数与按操作分类的计数（副本，调用方无需加锁）。
func StoreFailureStats() (total uint64, byOp map[string]uint64) {
	defaultFailureSink.mu.Lock()
	defer defaultFailureSink.mu.Unlock()
	out := make(map[string]uint64, len(defaultFailureSink.byOp))
	for k, v := range defaultFailureSink.byOp {
		out[k] = v
	}
	return defaultFailureSink.total, out
}

// RecentStoreFailures 返回最近的失败样本，最新的排在最前。最多 FailureRingSize 条。
func RecentStoreFailures() []StoreFailure {
	defaultFailureSink.mu.Lock()
	defer defaultFailureSink.mu.Unlock()
	n := defaultFailureSink.next
	if defaultFailureSink.filled {
		n = FailureRingSize
	}
	out := make([]StoreFailure, 0, n)
	// 从 next-1 倒着走，即为时间倒序。
	for i := 0; i < n; i++ {
		idx := (defaultFailureSink.next - 1 - i + FailureRingSize*2) % FailureRingSize
		out = append(out, defaultFailureSink.recent[idx])
	}
	return out
}

// TopStoreFailureOps 返回失败次数最多的操作，按次数降序（同次数按名称升序，保证稳定）。
func TopStoreFailureOps(limit int) []StoreFailureOpCount {
	_, byOp := StoreFailureStats()
	out := make([]StoreFailureOpCount, 0, len(byOp))
	for op, n := range byOp {
		out = append(out, StoreFailureOpCount{Op: op, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Op < out[j].Op
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// StoreFailureOpCount 单个操作的失败计数。
type StoreFailureOpCount struct {
	Op    string `json:"op"`
	Count uint64 `json:"count"`
}

// ResetStoreFailures 清空记录（仅供测试使用；生产不应调用）。
func ResetStoreFailures() {
	defaultFailureSink.mu.Lock()
	defer defaultFailureSink.mu.Unlock()
	defaultFailureSink.total = 0
	defaultFailureSink.byOp = make(map[string]uint64)
	defaultFailureSink.next = 0
	defaultFailureSink.filled = false
}
