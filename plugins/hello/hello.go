// plugins/hello 是 OpsMesh 插件框架的示例插件（main 包，独立可构建）。
//
// 演示插件框架的最小用法：
//  1. 实现 plugin.Plugin 接口（Name/Version/Init/Close）。
//  2. 在 Init 中注册钩子处理函数。
//  3. 钩子触发时打印日志（模拟"配置变更审计增强"场景）。
//
// 构建：go build -o hello.exe ./plugins/hello
// 运行：./hello.exe（仅演示注册 + 触发一次钩子，无副作用）。
package main

import (
	"context"
	"fmt"
	"log"

	"opsmesh/internal/plugin"
)

// helloPlugin 示例插件实例。
type helloPlugin struct {
	mgr *plugin.Manager // 持有 Manager 引用，便于 Init 时注册钩子
}

func (p *helloPlugin) Name() string    { return "hello" }
func (p *helloPlugin) Version() string { return "1.0.0" }

// Init 初始化：注册钩子处理函数。
// cfg 期望为 *plugin.Manager（控制面注册时传入），用于反注册钩子（本示例不演示反注册）。
func (p *helloPlugin) Init(cfg any) error {
	if mgr, ok := cfg.(*plugin.Manager); ok {
		p.mgr = mgr
	}
	// 演示：注册一个配置变更钩子，触发时打印日志。
	// 注意：此处仅演示；真实插件应通过 p.mgr 注册钩子，而非在 Init 里自注册
	// （Init 在 Manager.Register 内部调用，此时 p.mgr 已可用）。
	if p.mgr != nil {
		if err := p.mgr.RegisterHook(plugin.Hook("config.postSet"), p.onConfigSet); err != nil {
			return fmt.Errorf("注册钩子失败: %w", err)
		}
	}
	log.Printf("[hello] 插件已初始化")
	return nil
}

// Close 清理：示例插件无资源需释放。
func (p *helloPlugin) Close() error {
	log.Printf("[hello] 插件已关闭")
	return nil
}

// onConfigSet 配置变更钩子处理函数：打印变更日志（模拟审计增强）。
func (p *helloPlugin) onConfigSet(ev plugin.Event) error {
	log.Printf("[hello] 检测到配置变更: hook=%s name=%s payload=%v", ev.Hook, ev.Name, ev.Payload)
	return nil
}

func main() {
	mgr := plugin.NewManager()
	defer mgr.Close()

	// 注册插件，传入 mgr 作为配置（让插件能反向注册钩子）。
	if err := mgr.Register(&helloPlugin{}, mgr); err != nil {
		log.Fatalf("注册插件失败: %v", err)
	}

	// 模拟控制面触发配置变更钩子。
	if err := mgr.FireHook(context.Background(), plugin.Hook("config.postSet"), plugin.Event{
		Name:    "config.set",
		Payload: map[string]any{"key": "app/db/pool", "version": 3, "updatedBy": "admin"},
	}); err != nil {
		log.Fatalf("触发钩子失败: %v", err)
	}

	fmt.Println("hello 插件演示完成")
}
