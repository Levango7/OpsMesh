
// Package plugin 提供 OpsMesh 控制面的插件框架。
//
// 设计目标：在不修改控制面核心代码的前提下，允许通过插件扩展控制面行为
// （如审计增强、自定义通知渠道、配置变更钩子、准入校验等）。
//
// 核心抽象：
//   - Plugin：插件实例接口（Name/Version/Init/Close）。
//   - Hook：扩展点标识（字符串枚举，由控制面在各扩展点定义）。
//   - HookHandler：钩子处理函数签名（接收 Event，可修改 Payload 并返回 error 阻断流程）。
//   - Manager：插件管理器（注册/反注册/钩子触发/生命周期/并发安全）。
//
// 使用方式：
//
//	mgr := plugin.NewManager()
//	mgr.Register(&MyPlugin{})
//	mgr.RegisterHook(plugin.Hook("config.preSet"), handler)
//	mgr.FireHook(ctx, plugin.Hook("config.preSet"), event)
//	mgr.Close()
//
// 并发安全：Manager 内部用 sync.RWMutex 保护 plugins 和 hooks 映射；
// FireHook 在持锁拷贝 handler 切片后释放锁再逐个调用，避免长锁 + 死锁。
package plugin

import (
	"context"
	"fmt"
	"log"
	"sync"
)

// Hook 扩展点标识。由控制面在各扩展点定义并触发。
// 命名约定：领域.动作.时机，如 "config.preSet" / "config.postSet" / "secret.preRotate"。
type Hook string

// Event 钩子事件：传递给 HookHandler 的上下文与负载。
// Payload 为可变负载（handler 可修改）；Result 用于 handler 回写结果（可选）。
type Event struct {
	// Hook 触发此事件的扩展点标识。
	Hook Hook
	// Name 事件名（通常等于 Hook 字符串，或更细粒度的子事件）。
	Name string
	// Payload 事件负载（handler 可修改以影响后续流程）。
	Payload any
	// Result handler 回写结果（可选；多个 handler 的 Result 会被后一个覆盖）。
	Result any
	// Ctx 上下文（取消/超时/trace 透传）。
	Ctx context.Context
}

// HookHandler 钩子处理函数签名。
// 返回 error 会阻断当前扩展点流程（控制面据此决定是否回滚/拒绝）；
// 返回 nil 表示放行，后续 handler 继续执行。
type HookHandler func(ev Event) error

// Plugin 插件实例接口。
// 生命周期：Register → Init → （运行期 HookHandler 被触发）→ Close。
type Plugin interface {
	// Name 插件唯一标识（注册时按此去重）。
	Name() string
	// Version 插件版本（语义化版本字符串，如 "1.0.0"）。
	Version() string
	// Init 插件初始化（注册时由 Manager 调用一次；返回 error 会拒绝注册）。
	// cfg 为插件配置（来自控制面配置文件，结构由插件自定义）。
	Init(cfg any) error
	// Close 插件清理（Manager.Close 时逐个调用；须幂等）。
	Close() error
}

// Manager 插件管理器：注册/反注册/钩子触发/生命周期管理。
// 并发安全：plugins 和 hooks 映射由 mu 保护；FireHook 拷贝 handler 切片后释放锁再调用。
type Manager struct {
	mu      sync.RWMutex
	plugins map[string]Plugin         // name -> plugin
	configs map[string]any            // name -> 配置（注册时传入，Init 时消费）
	hooks   map[Hook][]HookHandler    // hook -> handlers（注册顺序）
	closed  bool                      // 是否已 Close（防止重复关闭）
}

// NewManager 创建插件管理器。
func NewManager() *Manager {
	return &Manager{
		plugins: make(map[string]Plugin),
		configs: make(map[string]any),
		hooks:   make(map[Hook][]HookHandler),
	}
}

// Register 注册插件。若同名插件已存在则返回 error（不覆盖）。
// 注册成功后立即调用 Plugin.Init(cfg)；Init 返回 error 会回滚注册（不残留半注册状态）。
func (m *Manager) Register(p Plugin, cfg any) error {
	if p == nil {
		return fmt.Errorf("plugin: 注册失败：插件为 nil")
	}
	name := p.Name()
	if name == "" {
		return fmt.Errorf("plugin: 注册失败：插件名为空")
	}
	m.mu.Lock()
	if _, exists := m.plugins[name]; exists {
		m.mu.Unlock()
		return fmt.Errorf("plugin: 注册失败：同名插件已存在 %q", name)
	}
	// 先占坑（Init 可能依赖 Manager.GetPlugin 查询其他已注册插件）。
	m.plugins[name] = p
	m.configs[name] = cfg
	m.mu.Unlock()

	// Init 在锁外调用（避免长锁 + Init 内若回调 Manager 可能死锁）。
	// Init 失败则回滚。
	if err := p.Init(cfg); err != nil {
		m.mu.Lock()
		delete(m.plugins, name)
		delete(m.configs, name)
		m.mu.Unlock()
		return fmt.Errorf("plugin: %q Init 失败: %w", name, err)
	}
	log.Printf("[plugin] 注册成功: %s@%s", name, p.Version())
	return nil
}

// Unregister 反注册插件：先调用 Plugin.Close，再从映射移除。
// 不存在返回 error；Close 失败仍会移除（避免残留不可关闭的插件）。
func (m *Manager) Unregister(name string) error {
	m.mu.Lock()
	p, ok := m.plugins[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("plugin: 反注册失败：插件不存在 %q", name)
	}
	delete(m.plugins, name)
	delete(m.configs, name)
	m.mu.Unlock()

	// Close 在锁外调用。
	if err := p.Close(); err != nil {
		log.Printf("[plugin] %q Close 失败（已移除）: %v", name, err)
		return fmt.Errorf("plugin: %q Close 失败: %w", name, err)
	}
	log.Printf("[plugin] 反注册成功: %s", name)
	return nil
}

// RegisterHook 注册钩子处理函数。同一 Hook 可注册多个 handler，按注册顺序执行。
// handler 为 nil 时直接返回 error（避免静默丢弃）。
func (m *Manager) RegisterHook(h Hook, handler HookHandler) error {
	if handler == nil {
		return fmt.Errorf("plugin: 注册钩子失败：handler 为 nil (hook=%q)", h)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hooks[h] = append(m.hooks[h], handler)
	return nil
}

// FireHook 触发钩子：按注册顺序逐个调用 handler。
// 任一 handler 返回 error 立即停止后续 handler 并返回该 error（短路语义）。
// 并发安全：先持读锁拷贝 handler 切片，释放锁后逐个调用，避免长锁 + 死锁。
// Event.Ctx 为空时注入 context.Background()。
func (m *Manager) FireHook(_ context.Context, h Hook, ev Event) error {
	// 持读锁拷贝 handler 切片。
	m.mu.RLock()
	handlers := make([]HookHandler, len(m.hooks[h]))
	copy(handlers, m.hooks[h])
	m.mu.RUnlock()

	if len(handlers) == 0 {
		return nil // 无 handler，放行。
	}

	// 注入事件元信息。
	if ev.Hook == "" {
		ev.Hook = h
	}
	if ev.Ctx == nil {
		ev.Ctx = context.Background()
	}

	// 逐个调用（锁外，避免长锁 + handler 内回调 Manager 死锁）。
	for _, handler := range handlers {
		if err := handler(ev); err != nil {
			return fmt.Errorf("plugin: 钩子 %q 处理失败: %w", h, err)
		}
	}
	return nil
}

// GetPlugin 按名返回插件实例；不存在返回 nil。
func (m *Manager) GetPlugin(name string) Plugin {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.plugins[name]
}

// AllPlugins 返回全部已注册插件（快照副本，按名升序稳定输出）。
// 调用方可安全遍历而不持锁。
func (m *Manager) AllPlugins() []Plugin {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Plugin, 0, len(m.plugins))
	for _, p := range m.plugins {
		out = append(out, p)
	}
	// 按名升序（稳定输出，便于测试断言）。
	// 小规模用插入排序，避免引入 sort 包。
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].Name() > out[j].Name(); j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// Close 关闭管理器：逐个调用已注册插件的 Close（幂等；已关闭则直接返回）。
// 关闭顺序与注册顺序无关（map 遍历无序）；任一 Close 失败仅记录日志不中断（尽力关闭）。
func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	// 拷贝插件列表后释放锁，避免长锁 + Close 内回调 Manager 死锁。
	plugins := make([]Plugin, 0, len(m.plugins))
	for _, p := range m.plugins {
		plugins = append(plugins, p)
	}
	m.mu.Unlock()

	var firstErr error
	for _, p := range plugins {
		if err := p.Close(); err != nil {
			log.Printf("[plugin] %q Close 失败: %v", p.Name(), err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}