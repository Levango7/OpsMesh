// process_cpu.go — 进程 CPU 时间指标（`process_cpu_seconds_total`）。
//
// 为什么需要它：出厂的 prometheus-alerts.yml 与 Grafana 总览面板都在用
// `rate(process_cpu_seconds_total[5m])`，但本项目的注册表此前只输出
// process_start_time / resident_memory / virtual_memory / pid 四项——**没有 CPU**。
// 结果是 HighCPUUsage 告警与 CPU 面板恒无数据（Prometheus 里"无数据"合法，不报错），
// 属于"看起来有监控、实际不覆盖"的那一类交付缺陷（实测：抓 9091 命中 0 条）。
//
// 语义与 prometheus 官方 ProcessCollector 对齐：**累计 CPU 秒数（user+system），counter**。
// 只支持 Linux（读 /proc/self/stat）；其它平台返回 ok=false ⇒ 该序列**不输出**，
// 而不是输一个假的 0——假的 0 会让 rate() 得到 0，看起来"CPU 空闲"，比没有更误导。
package metrics

import (
	"os"
	"runtime"
	"strconv"
	"strings"
)

// linuxClockTicksPerSecond Linux 上 /proc 暴露的时钟节拍（USER_HZ）。
// 内核对外固定为 100（与内部 CONFIG_HZ 无关），prometheus 官方 collector 同样按 100 处理。
const linuxClockTicksPerSecond = 100.0

// readProcessCPUTimeSeconds 返回本进程累计 CPU 秒（user+system）。
func readProcessCPUTimeSeconds() (float64, bool) {
	if runtime.GOOS != "linux" {
		return 0, false
	}
	raw, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, false
	}
	return parseProcessCPUTime(raw)
}

// parseProcessCPUTime 从 /proc/<pid>/stat 的内容里取 utime+stime。
// 单独拆出来是为了可测：CI/本机都能用固定样本验证字段错位问题，而不必依赖真实 /proc。
func parseProcessCPUTime(raw []byte) (float64, bool) {
	// /proc/self/stat 的第 2 个字段是 comm，可用含空格与括号，直接按空格切会错位。
	// 内核约定：从最后一个 ')' 之后才是第 3 个字段，故据此定位再切。
	closeParen := strings.LastIndexByte(string(raw), ')')
	if closeParen < 0 {
		return 0, false
	}
	rest := strings.Fields(string(raw)[closeParen+1:])
	// rest[0] 是 state（第 3 字段）。utime 是第 14、stime 第 15 字段 ⇒ 索引 11 / 12。
	if len(rest) < 13 {
		return 0, false
	}
	utime, err1 := strconv.ParseFloat(rest[11], 64)
	stime, err2 := strconv.ParseFloat(rest[12], 64)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return (utime + stime) / linuxClockTicksPerSecond, true
}
