// memory_bounds.go — 控制面内存驻留缓冲的硬上限（P1-4）。
//
// 背景：deviceMetrics 与 agentLogs 都是「agent 上报驱动」的内存结构，其增长由 agent
// （以及被攻陷/被伪造的 agent）决定，原实现没有任何总量约束：
//   - deviceMetrics 的 key 取自心跳里的 DeviceID（agent 可控）：每个 key 内部的环形缓冲
//     有长度上限（240 条），但 map 本身无淘汰 → 构造任意 DeviceID 即可让 map 无界膨胀；
//   - agentLogs 每 30s/agent 追加一个批次且从不裁剪 → 长跑必然 OOM（单控制面进程被拖垮）。
//
// 两个结构都由 store 的 mu 统一保护，本文件的 helper 由调用方持锁调用（自身不加锁）。
package store

import (
	"sync/atomic"

	"github.com/Levango7/OpsMesh/internal/proto"
)

const (
	// maxTrackedDeviceMetrics 设备指标驻留设备数上限；超限按「最久未写入」淘汰整条设备条目。
	// 内存量级：单设备 240 条样本 × ~1KB ≈ 240KB，2000 设备 ≈ 480MB 上限。
	// 规模化部署的历史时序应以外部时序库（部署栈内已含 Prometheus/Grafana）为准，
	// 此处缓冲仅作控制面内快速回看。
	maxTrackedDeviceMetrics = 2000
	// maxAgentLogReports agent 日志驻留批次数上限（超出丢弃最旧批次）。
	maxAgentLogReports = 2000
	// maxAgentLogLines agent 日志驻留总行数上限（超出丢弃最旧批次直至回落）。
	maxAgentLogLines = 100000
)

// deviceMetricsSeq 设备指标写入序号（单调递增，进程内唯一）。
//
// 为什么不用墙钟：淘汰顺序必须全序确定。Windows 的 time.Now() 粒度约 15.6ms，
// 高负载下同一刻会落入多条写入，按时刻比较会出现「并列」，此刻淘汰哪个取决于 map
// 迭代顺序（随机）——实测表现为 `TestStoreDeviceMetrics_RecentlyWrittenSurvives`
// 偶发失败。单调序号既保证严格全序，又保留了原始设计意图：排序依据是 store 侧写入
// 顺序，不受 agent 上报时间（可被伪造到未来）影响。
var deviceMetricsSeq atomic.Uint64

// nextDeviceMetricsSeq 返回下一个写入序号（由调用方在持锁下调用）。
func nextDeviceMetricsSeq() uint64 { return deviceMetricsSeq.Add(1) }

// appendAgentLogBounded 追加日志批次并按上限裁剪最旧批次，返回新切片与新的总行数。
// lines 为 logs 当前总行数（调用方在 store 结构体里累加维护，避免每次 O(n) 重算）。
// 恒定保留刚追加的批次（单批次即超总行数上限时也不会被丢弃）。
func appendAgentLogBounded(logs []proto.LogReport, lines int, cp proto.LogReport) ([]proto.LogReport, int) {
	logs = append(logs, cp)
	lines += len(cp.Lines)

	dropped := 0
	for dropped < len(logs)-1 && (len(logs)-dropped > maxAgentLogReports || lines > maxAgentLogLines) {
		lines -= len(logs[dropped].Lines)
		logs[dropped] = proto.LogReport{} // 清空元素，避免底层数组继续持有已丢弃批次的 Lines
		dropped++
	}
	if dropped == 0 {
		return logs, lines
	}
	logs = logs[dropped:]
	// 长期「丢弃最旧 + 追加」会让底层数组的容量只增不减（切片头前移），
	// 容量超过上限两倍时复制到紧凑切片回收内存。
	if cap(logs) > 2*maxAgentLogReports {
		compacted := make([]proto.LogReport, len(logs))
		copy(compacted, logs)
		logs = compacted
	}
	return logs, lines
}

// evictDeviceMetricsIfNeeded 在设备条目数超过上限时淘汰最久未写入的条目，返回淘汰数。
// 超出量通常为 1（每次心跳最多新增 1 个设备），循环代价可忽略。
// 排序依据是 writeSeq（单调递增写入序号）而非墙钟：严格全序，同刻写入亦可确定淘汰对象。
func evictDeviceMetricsIfNeeded(m map[string]*metricsRing) int {
	if len(m) <= maxTrackedDeviceMetrics {
		return 0
	}
	evicted := 0
	for len(m) > maxTrackedDeviceMetrics {
		oldestKey := ""
		var oldestSeq uint64
		for k, r := range m {
			if r == nil {
				oldestKey = k
				break
			}
			if oldestKey == "" || r.writeSeq < oldestSeq {
				oldestKey, oldestSeq = k, r.writeSeq
			}
		}
		if oldestKey == "" {
			return evicted
		}
		delete(m, oldestKey)
		evicted++
	}
	return evicted
}
