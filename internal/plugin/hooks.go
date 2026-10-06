package plugin

// hooks.go 冻结控制面的扩展点清单（TD-62 决策点 ②）。
//
// # 为什么要把"扩展点"从裸字符串升级为常量并冻结
//
// Hook 此前只是 `type Hook string`——任何代码都能凭空写一个 plugin.Hook("xxx")
// 出来，而没有任何机制保证它在控制面里真的有触发点。这正是 TD-62 的形态：
// 框架齐备、零接线，而"零接线"这件事本身没人能发现，因为字符串不构成契约。
//
// 冻结后：
//   - 扩展点是**有限的枚举**，新增必须改本文件（评审面收敛）；
//   - 每个扩展点必须同时具备「控制面触发点」与「测试」，
//     由 internal/controlplane 的 plugin_hook_gate_test.go 强制（决策点 ④）；
//   - 命名约定 领域.动作.时机，见 Hook 类型注释。
//
// # 失败语义（决策点 ③，与控制面接线处一一对应）
//
//	pre*  钩子：可阻断。返回 error ⇒ 控制面拒绝该次操作（4xx），不落库。
//	post* 钩子：不可阻断。已落库的事实不因插件失败而回滚；
//	            error 只进审计/日志，响应仍为成功。
//
// 这个区分是刻意的：post 阶段里"业务已提交"与"插件失败"同时成立时，
// 回滚会让客户端看到成功却没生效，比让插件失败更糟。
//
// # 已决（决策点 ①，2026-10-07）
//
// 插件运行时模型选定为**独立进程 + HTTP 契约**，实现见本包 remote.go 与
// internal/controlplane/plugin_remote.go（装载）+ plugin_host.go（触发）。
// 本文件冻结的仍是"有哪些扩展点、何时触发、失败怎么办"——这三种问题在
// 任何一种运行时模型下都同样成立，所以选型没有改动本文件的任何约定。
// 新增扩展点的门槛不变：改本文件 + 控制面触发点 + 测试，三件缺一不可。

const (
	// HookConfigPreSet 平台配置写入前触发（可阻断）。
	// 用途：准入校验（如禁止关闭审计、限制允许的告警通道）。
	// 失败语义：返回 error ⇒ 拒绝本次写入，返回 4xx，不落库。
	HookConfigPreSet Hook = "config.preSet"

	// HookConfigPostSet 平台配置写入后触发（不可阻断）。
	// 用途：变更通知、外部系统同步、派生配置重算。
	// 失败语义：error 只进审计与日志，响应仍为成功（配置已落库的事实不回滚）。
	HookConfigPostSet Hook = "config.postSet"

	// HookTaskPreClaim agent 领取任务前触发（可阻断）。
	// 用途：准入策略（如按设备标签/时段限制可执行任务）。
	// 失败语义：返回 error ⇒ 本次领取返回空（等同于当前无任务），不下发任务。
	HookTaskPreClaim Hook = "task.preClaim"
)

// AllHooks 返回全部已冻结的扩展点。
//
// 这是门禁的**权威清单**：新增扩展点必须加到本切片，并同时提供触发点与测试，
// 否则 internal/controlplane 的 plugin_hook_gate_test.go 判红。
// 只改文档不改本切片，或只加本切片不加触发点，都会被门禁拦下。
func AllHooks() []Hook {
	return []Hook{
		HookConfigPreSet,
		HookConfigPostSet,
		HookTaskPreClaim,
	}
}
