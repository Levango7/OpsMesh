// Command opsmesh 是 OpsMesh 网段运维中枢的二进制内核（U-05）。
// 同一份代码、同一份二进制，通过 --mode 切换控制面(controlplane)与网段 agent。
// 仅依赖 Go 标准库 + 少量稳定外部依赖（grpc/mysql/redis），无任何 protobuf 代码生成。
package main

import (
	"fmt"
	"os"

	"opsmesh/internal/agent"
	"opsmesh/internal/config"
	"opsmesh/internal/controlplane"
	"opsmesh/internal/version"
)

func main() {
	// --version 必须在 config.Load 之前短路，否则 flag.Parse 会因未知 flag 退出。
	for _, a := range os.Args[1:] {
		if a == "--version" || a == "-version" {
			fmt.Printf("opsmesh %s (commit=%s date=%s)\n", version.Version, version.Commit, version.Date)
			return
		}
	}

	cfg := config.Load()

	// 启动期配置校验：明显非法配置立即失败，而非运行期诡异出错（P0-3 健壮性）。
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "[config] 校验失败: %v\n", err)
		os.Exit(1)
	}

	switch cfg.Mode {
	case "controlplane":
		// U-05 控制面模式：HTTP(B/S) 8080 + gRPC 9090（真实注册通道） + metrics 9091。
		srv := controlplane.NewServer(cfg)
		if err := srv.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "[controlplane] 启动失败: %v\n", err)
			os.Exit(1)
		}
	case "agent":
		// U-05 agent 模式：向控制面注册并托管本段网络内的自动化任务。
		ag := agent.New(cfg)
		if err := ag.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "[agent] 启动失败: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "未知 --mode=%q（应为 controlplane 或 agent）\n", cfg.Mode)
		os.Exit(1)
	}
}
