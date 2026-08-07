// Command opsmesh 是 OpsMesh 网段运维中枢的二进制内核（U-05）。
// 同一份代码、同一份二进制，通过 --mode 切换控制面(controlplane)与网段 agent。
// 仅依赖 Go 标准库 + 少量稳定外部依赖（grpc/mysql/redis），无任何 protobuf 代码生成。
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

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

	// --health 子命令：探活 localhost:{httpPort}/healthz，供 docker-compose / K8s 健康检查使用。
	// 必须在 config.Load 之前短路：distroless 镜像无 curl/sh，且 flag.Parse 会因未知 flag 退出。
	// 退出码：HTTP 200 → 0（健康），否则 → 1（不健康/不可达）。
	for _, a := range os.Args[1:] {
		if a == "--health" || a == "-health" {
			os.Exit(runHealth())
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

// runHealth 实现 --health 子命令：对 localhost:{httpPort}/healthz 发 GET，
// 200 → 返回 0（健康），否则 → 返回 1（不健康/不可达）。
//
// 设计要点：
//   - 用独立 FlagSet 解析 --http-port，避免污染 config.Load 的全局 flag 表；
//   - 3s 超时与 docker-compose healthcheck.timeout 对齐，避免阻塞容器编排；
//   - 仅依赖 Go 标准库，distroless 镜像无需 curl/wget/sh 即可探活；
//   - 错误诊断输出到 stderr，stdout 保持空，便于编排系统解析退出码。
func runHealth() int {
	fs := flag.NewFlagSet("health", flag.ExitOnError)
	httpPort := fs.Int("http-port", 8080, "控制面 HTTP 端口（探活目标）")
	if err := fs.Parse(os.Args[2:]); err != nil {
		// flag.ExitOnError 已处理（打印用法 + exit 2），此处不可达。
		return 1
	}

	url := fmt.Sprintf("http://localhost:%d/healthz", *httpPort)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[health] 连接 %s 失败: %v\n", url, err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "[health] %s 返回非 200: HTTP %d\n", url, resp.StatusCode)
		return 1
	}
	return 0
}
