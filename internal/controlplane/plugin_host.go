// plugin_host.go 把 internal/plugin 框架接到控制面上（TD-62 决策点 ③）。
//
// # 这个文件为什么存在
//
// internal/plugin 提供了完整的 Manager（注册/钩子/生命周期/并发安全），
// 但**控制面里没有任何一处触发它**——实测 FireHook/RegisterHook 在
// internal/controlplane 下零调用点，唯一调用者是示例 plugins/hello 自己 fire 自己。
//
// 后果是：README 与 "可插拔扩展" 的宣传口径对应的能力并不存在，
// 客户接入任何扩展都得先自己补宿主接线。本文件就是那层接线。
//
// # 设计选择：钩子是"可选增强"，不是"必经路径"
//
// 无插件注册时 FireHook 立即返回 nil（handlers 为空切片），
// 因此所有接线点在默认部署下**零行为变化**——这不是"改了核心流程"，
// 而是"在核心流程上开了可观测的口子"。
//
// # 失败语义（与 internal/plugin/hooks.go 一致，勿在此处分叉）
//
//	pre*  : 可阻断 —— error ⇒ 拒绝操作（4xx），不落库
//	post* : 不可阻断 —— 已落库的事实不回滚，error 只进日志与审计
//
// # 未决（决策点 ①，本文件不拍板）
//
// 插件的**运行时模型**（Go plugin / WASM / 独立进程 + RPC）仍然开放。
// 无论最终选哪种，插件都要通过本文件的 firePluginHook 被触发——
// 也就是说本文件是三种方案的公共前置，不替产品做运行时选型。
package controlplane

import (
	"context"
	"log"

	"github.com/Levango7/OpsMesh/internal/plugin"
)

// pluginMgr 是控制面的插件管理器（进程级唯一）。
//
// nil 是合法状态：表示本次进程没有插件宿主（例如测试里未注入）。
// firePluginHook 对 nil 管理器返回 nil，保证接线点无需到处做 nil 检查。
var pluginMgr *plugin.Manager

// SetPluginManager 注入插件管理器（启动期调用一次，之后只读）。
//
// 传 nil 表示不启用插件宿主。测试可用它注入自带钩子的管理器，
// 从而在不改业务代码的前提下验证扩展点确实被触发。
func SetPluginManager(m *plugin.Manager) { pluginMgr = m }

// PluginManager 返回当前插件管理器（nil 表示未启用）。
// 对外暴露是为了让 grpc 子包等其它接线点复用同一个管理器。
func PluginManager() *plugin.Manager { return pluginMgr }

// firePluginHook 触发一个扩展点钩子。
//
// 语义：
//   - 管理器为 nil 或该钩子无 handler ⇒ 返回 nil（放行，零副作用）；
//   - handler 返回 error ⇒ 原样透传，由**调用方**按 pre/post 语义决定
//     是拒绝操作还是仅记日志。这里不做决策，因为同一个钩子在不同
//     调用点的阻断语义可能不同（见 hooks.go 的约定）。
//
// 刻意不吞掉 error：静默吞掉会让"插件说不行"变成"系统说行"，
// 这正是准入类扩展失效后最难排查的形态。
func (s *Server) firePluginHook(ctx context.Context, h plugin.Hook, ev plugin.Event) error {
	m := pluginMgr
	if m == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ev.Ctx == nil {
		ev.Ctx = ctx
	}
	if err := m.FireHook(ctx, h, ev); err != nil {
		// 只做统一日志前缀，错误本身交给调用方处置。
		log.Printf("[controlplane] 插件钩子 %q 返回错误: %v", h, err)
		return err
	}
	return nil
}
