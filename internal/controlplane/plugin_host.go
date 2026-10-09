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
// # 已决（决策点 ①，2026-10-07）
//
// 插件运行时模型选定为**独立进程 + HTTP 契约**：装载与校验在 plugin_remote.go，
// 传输在 internal/plugin/remote.go，本文件仍是三种候选下都成立的公共前置——
// 插件最终都通过本文件的 firePluginHook 被触发。
//
// 本文件另一处未决也已闭合：SetPluginManager 此前只有测试调用（即"能力在源码里成立、
// 在交付物里不存在"），现由 NewServer 在启动路径上调用（server.go 的插件宿主接线段）。
//
// TD-87 批 2：全局持有器与访问器实现已迁入 internal/controlplane/pluginhost（host.go），
// 本文件保留 Server 方法 firePluginHook 与两个薄包装。
package controlplane

import (
	"context"
	"log"

	"github.com/Levango7/OpsMesh/internal/controlplane/pluginhost"
	"github.com/Levango7/OpsMesh/internal/plugin"
)

// SetPluginManager / PluginManager 是 pluginhost 包同名访问器的薄包装（TD-87 批 2）：
// 全局持有器已迁至 internal/controlplane/pluginhost/host.go，父包保留这对门面，
// 46 处调用点（server.go、plugin_hook_gate_test.go、plugin_remote_test.go 等）零改动。
func SetPluginManager(m *plugin.Manager) { pluginhost.SetPluginManager(m) }

// PluginManager 返回当前插件管理器（nil 表示未启用）。
func PluginManager() *plugin.Manager { return pluginhost.PluginManager() }

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
	m := PluginManager()
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
