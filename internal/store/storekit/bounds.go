// bounds.go — 控制面内存驻留缓冲的硬上限（P1-4）。
//
// 背景：deviceMetrics 与 agentLogs 都是「agent 上报驱动」的内存结构，其增长由 agent
// （以及被攻陷/被伪造的 agent）决定，原实现没有任何总量约束：
//   - deviceMetrics 的 key 取自心跳里的 DeviceID（agent 可控）：每个 key 内部的环形缓冲
//     有长度上限（240 条），但 map 本身无淘汰 → 构造任意 DeviceID 即可让 map 无界膨胀
//     （上限与淘汰见 metricsring.go 的 MaxTrackedDeviceMetrics / EvictDeviceMetricsIfNeeded）；
//   - agentLogs 每 30s/agent 追加一个批次且从不裁剪 → 长跑必然 OOM（单控制面进程被拖垮）。
//
// 两个结构都由持有它的 store 结构体以其 mu 统一保护，本文件的 helper 由调用方
// 持锁调用（自身不加锁）。memory 与 sql 两个后端共用这些上限，保证两后端行为一致。
package storekit

import "github.com/Levango7/OpsMesh/internal/proto"

const (
	// MaxAgentLogReports agent 日志驻留批次数上限（超出丢弃最旧批次）。
	MaxAgentLogReports = 2000
	// MaxAgentLogLines agent 日志驻留总行数上限（超出丢弃最旧批次直至回落）。
	MaxAgentLogLines = 100000
)

// AppendAgentLogBounded 追加日志批次并按上限裁剪最旧批次，返回新切片与新的总行数。
// lines 为 logs 当前总行数（调用方在 store 结构体里累加维护，避免每次 O(n) 重算）。
// 恒定保留刚追加的批次（单批次即超总行数上限时也不会被丢弃）。
func AppendAgentLogBounded(logs []proto.LogReport, lines int, cp proto.LogReport) ([]proto.LogReport, int) {
	logs = append(logs, cp)
	lines += len(cp.Lines)

	dropped := 0
	for dropped < len(logs)-1 && (len(logs)-dropped > MaxAgentLogReports || lines > MaxAgentLogLines) {
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
	if cap(logs) > 2*MaxAgentLogReports {
		compacted := make([]proto.LogReport, len(logs))
		copy(compacted, logs)
		logs = compacted
	}
	return logs, lines
}
