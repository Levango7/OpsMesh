// failures_shim.go —— storefail 子包的父包回导层（TD-61 批次 1）。
//
// 背景：failures.go 是 internal/store 里第一个被抽出的独立层（迁往
// internal/store/storefail）。抽出的前提与代价都写在这里：
//
//   - 前提：该文件与主包零内部依赖（仅标准库 import），抽出不产生循环；
//   - 代价的消解：store 内 327 个调用点写的是未导出的 recordStoreFailure，
//     外部消费方写的是 store.StoreFailureStats / store.RecentStoreFailures。
//     本文件以**类型别名 + 薄包装**把它们原样保留——搬迁因此对调用方不可见，
//     一个调用点都不用改，编译期全程有信号。
//
// 这是形态 A（按后端/按层拆）的标准过渡手法：别名过渡类型，薄包装过渡函数；
// 待一个发布周期后，可把调用方逐批改为直接 import storefail 再删除本层。
package store

import "github.com/Levango7/OpsMesh/internal/store/storefail"

// 类型别名：调用方写 store.StoreFailure 与写 storefail.StoreFailure 完全等同。
type (
	StoreFailure        = storefail.StoreFailure
	StoreFailureOpCount = storefail.StoreFailureOpCount
)

// failureRingSize：常量别名（未导出名保留，父包既有测试零改动）。
const failureRingSize = storefail.FailureRingSize

// recordStoreFailure 保持未导出形态不变——store 内 327 个调用点零改动。
func recordStoreFailure(format string, args ...any) {
	storefail.Record(format, args...)
}

// 以下四个是外部消费方（controlplane 的管理/指标端点）与测试使用的导出 API。
func StoreFailureStats() (total uint64, byOp map[string]uint64) {
	return storefail.StoreFailureStats()
}

func RecentStoreFailures() []StoreFailure {
	return storefail.RecentStoreFailures()
}

func TopStoreFailureOps(limit int) []StoreFailureOpCount {
	return storefail.TopStoreFailureOps(limit)
}

func ResetStoreFailures() {
	storefail.ResetStoreFailures()
}
