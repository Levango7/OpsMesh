// kernel_shim.go 父包对 storekit 共享内核的兼容层（TD-61 批次 3，过渡件）。
//
// 共享内核（token 签名 / 随机串 / bcrypt / 指标环形缓冲与内存上限）已下沉
// internal/store/storekit：memory 子包直接 import 并调用导出名；
// 而父包内的 sql_*.go / multi_schema*.go / 各测试仍在用旧短名——本文件以
// 「类型别名 + 常量别名 + 薄包装」保持它们零改动。
//
// 生命周期：随 SQL 后端下沉（批次 3-sql）一并删除——sqlstore 子包与 memory 一样
// 直接 import storekit，届时本文件成为无人引用的死代码。
package store

import (
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store/storekit"
)

// 类型/常量别名：指标环形缓冲与内存驻留上限（原 memory_middleware_template.go / memory_bounds.go）。
type metricsRing = storekit.MetricsRing

const (
	metricsRingDefaultCap   = storekit.MetricsRingDefaultCap
	maxTrackedDeviceMetrics = storekit.MaxTrackedDeviceMetrics
	maxAgentLogReports      = storekit.MaxAgentLogReports
	maxAgentLogLines        = storekit.MaxAgentLogLines
)

// 薄包装：函数无法别名，按旧短名转发。

func newMetricsRing(capacity int) *metricsRing { return storekit.NewMetricsRing(capacity) }

func evictDeviceMetricsIfNeeded(m map[string]*metricsRing) int {
	return storekit.EvictDeviceMetricsIfNeeded(m)
}

func appendAgentLogBounded(logs []proto.LogReport, lines int, cp proto.LogReport) ([]proto.LogReport, int) {
	return storekit.AppendAgentLogBounded(logs, lines, cp)
}

func bcryptHash(password string) (string, error) { return storekit.BcryptHash(password) }

func randHex(n int) string { return storekit.RandHex(n) }

func mustRandHex(n int) string { return storekit.MustRandHex(n) }

func hashToken(tok string) string { return storekit.HashToken(tok) }

func verifyTokenMAC(secret, token string) bool { return storekit.VerifyTokenMAC(secret, token) }

func randAlertRuleID() string { return storekit.RandAlertRuleID() }
