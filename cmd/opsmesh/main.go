// Command opsmesh 是 OpsMesh 网段运维中枢的二进制内核（U-05）。
// 同一份代码、同一份二进制，通过 --mode 切换控制面(controlplane)与网段 agent。
// 仅依赖 Go 标准库 + 少量稳定外部依赖（grpc/mysql/redis），无任何 protobuf 代码生成。
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"opsmesh/internal/agent"
	"opsmesh/internal/config"
	"opsmesh/internal/controlplane"
	"opsmesh/internal/version"
)

func main() {
	os.Exit(runMain())
}

// runMain 是 main 的可测试核心：返回进程退出码而非直接 os.Exit，
// 便于单测在主测试进程内安全驱动 --version / --health 等短路分支并断言返回码。
// controlplane/agent 分支会阻塞（启动服务器/agent 主循环），仅由真实二进制入口调用。
func runMain() int {
	// --version 必须在 config.Load 之前短路，否则 flag.Parse 会因未知 flag 退出。
	for _, a := range os.Args[1:] {
		if a == "--version" || a == "-version" {
			fmt.Print(versionString())
			return 0
		}
	}

	// --health 子命令：探活 localhost:{httpPort}/healthz，供 docker-compose / K8s 健康检查使用。
	// 必须在 config.Load 之前短路：distroless 镜像无 curl/sh，且 flag.Parse 会因未知 flag 退出。
	// 退出码：HTTP 200 → 0（健康），否则 → 1（不健康/不可达）。
	for _, a := range os.Args[1:] {
		if a == "--health" || a == "-health" {
			return runHealth()
		}
	}

	// backup/restore 子命令：导出/导入控制面数据，供离线备份/迁移/灾备恢复。
	// 必须在 config.Load 之前短路：特有 flag（--output/--format 等）未注册到全局 flag 表，
	// flag.Parse 会因未知 flag 退出。子命令内部用独立 FlagSet 解析特有参数，
	// 再过滤掉特有 flag 后调用 config.Load 解析 store 相关参数（--store/--mysql-dsn 等）。
	for _, a := range os.Args[1:] {
		switch a {
		case "backup":
			return runBackup()
		case "restore":
			return runRestore()
		}
	}

	cfg := config.Load()

	// 启动期配置校验：明显非法配置立即失败，而非运行期诡异出错（P0-3 健壮性）。
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "[config] 校验失败: %v\n", err)
		return 1
	}

	switch cfg.Mode {
	case "controlplane":
		// U-05 控制面模式：HTTP(B/S) 8080 + gRPC 9090（真实注册通道） + metrics 9091。
		srv := controlplane.NewServer(cfg)
		if err := srv.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "[controlplane] 启动失败: %v\n", err)
			return 1
		}
	case "agent":
		// U-05 agent 模式：向控制面注册并托管本段网络内的自动化任务。
		ag := agent.New(cfg)
		if err := ag.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "[agent] 启动失败: %v\n", err)
			return 1
		}
	default:
		fmt.Fprintf(os.Stderr, "未知 --mode=%q（应为 controlplane 或 agent）\n", cfg.Mode)
		return 1
	}
	return 0
}

// versionString 返回 --version 子命令的标准输出（单行）。
// 提取为独立函数便于单测精确断言输出格式，避免依赖 stdout 捕获。
func versionString() string {
	return fmt.Sprintf("opsmesh %s (commit=%s date=%s)\n", version.Version, version.Commit, version.Date)
}

// runHealth 实现 --health 子命令：对 localhost:{httpPort}/healthz 发 GET，
// 200 → 返回 0（健康），否则 → 返回 1（不健康/不可达）。
//
// 设计要点：
//   - 用独立 FlagSet 解析 --http-port，避免污染 config.Load 的全局 flag 表；
//   - 从 os.Args[1:] 过滤掉 --health/-health 自身再 parse，避免假设 --health 必为首个参数
//     （如 `opsmesh --http-port=8080 --health` 时 os.Args[2:] 含 --health，FlagSet 不认识会 exit 2）；
//   - FlagSet 用 ContinueOnError，parse 失败返回 2（参数错误），不污染主进程 flag 表；
//   - 3s 超时与 docker-compose healthcheck.timeout 对齐，避免阻塞容器编排；
//   - 仅依赖 Go 标准库，distroless 镜像无需 curl/wget/sh 即可探活；
//   - 错误诊断输出到 stderr，stdout 保持空，便于编排系统解析退出码。
func runHealth() int {
	fs := flag.NewFlagSet("health", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	httpPort := fs.Int("http-port", 8080, "控制面 HTTP 端口（探活目标）")
	// 过滤掉 --health/-health 自身，剩余参数交给 FlagSet 解析。
	// 这样无论 --health 出现在命令行哪个位置都能正确解析其他 flag。
	var args []string
	for _, a := range os.Args[1:] {
		if a == "--health" || a == "-health" {
			continue
		}
		args = append(args, a)
	}
	if err := fs.Parse(args); err != nil {
		return 2
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

// backupFlagSpecs 定义 backup 子命令特有 flag 及其是否带值（bool flag 不带值）。
// 用于从 os.Args 中过滤掉这些 flag，使剩余参数可安全交给 config.Load 的全局 flag.Parse。
var backupFlagSpecs = map[string]bool{ // flag 名 -> 是否带值（false=布尔 flag）
	"output":            true,
	"format":            true,
	"include-config":    false,
	"include-audits":    false,
	"task-window-days":  true,
	"alert-window-days": true,
	"audit-window-days": true,
}

// restoreFlagSpecs 定义 restore 子命令特有 flag。
var restoreFlagSpecs = map[string]bool{
	"input":     true,
	"dry-run":   false,
	"overwrite": false,
}

// filterSubcmdArgs 从 args 中移除子命令名及其特有 flag，返回剩余参数（供 config.Load 解析）。
//
// 处理两种 flag 形式：
//   - --flag=value：整 token 移除；
//   - --flag value：当前 token 与下一 token 一起移除（带值 flag）；
//   - --flag：仅当前 token 移除（布尔 flag）。
//
// 子命令名（如 "backup"/"restore"）作为首 token 出现时也被移除。
func filterSubcmdArgs(args []string, subcmd string, specs map[string]bool) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == subcmd {
			continue
		}
		// 解析 --name 或 --name=value
		if strings.HasPrefix(a, "--") || strings.HasPrefix(a, "-") {
			name := strings.TrimLeft(a, "-")
			hasEq := false
			if idx := strings.Index(name, "="); idx >= 0 {
				name = name[:idx]
				hasEq = true
			}
			if takesValue, ok := specs[name]; ok {
				if takesValue && !hasEq {
					// --flag value：跳过当前 token 与下一 token（值）
					i++
				}
				// 否则仅跳过当前 token（布尔 flag 或 --flag=value 形式）
				continue
			}
		}
		out = append(out, a)
	}
	return out
}

// runBackup 实现 backup 子命令：导出控制面数据到文件。
//
// 用法：
//
//	opsmesh backup --output <file> [--format json|sql] [--include-config] [--include-audits]
//	               [--task-window-days N] [--alert-window-days N] [--audit-window-days N]
//	               [--store memory|mysql] [--mysql-dsn ...] ...
//
// 设计要点：
//   - 用独立 FlagSet 解析 backup 特有 flag，避免污染 config.Load 全局 flag 表；
//   - 过滤掉特有 flag 后调用 config.Load 解析 store 相关参数（复用全部持久化配置）；
//   - 不启动 HTTP/gRPC，仅初始化 Store 后导出；
//   - 退出码：0=成功，1=运行错误，2=参数错误。
func runBackup() int {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	output := fs.String("output", "", "备份输出文件路径（必填）")
	format := fs.String("format", "json", "备份格式: json（默认） | sql（MySQL dump）")
	includeConfig := fs.Bool("include-config", false, "是否导出运行配置（含敏感字段，慎用）")
	includeAudits := fs.Bool("include-audits", false, "是否导出审计日志（最近 30 天）")
	taskWindowDays := fs.Int("task-window-days", 7, "任务导出时间窗（天，默认 7）")
	alertWindowDays := fs.Int("alert-window-days", 7, "告警导出时间窗（天，默认 7）")
	auditWindowDays := fs.Int("audit-window-days", 30, "审计导出时间窗（天，默认 30）")

	// 过滤掉 backup 子命令名及其特有 flag，剩余参数交给 FlagSet 解析。
	var parseArgs []string
	for _, a := range os.Args[1:] {
		if a == "backup" {
			continue
		}
		parseArgs = append(parseArgs, a)
	}
	if err := fs.Parse(parseArgs); err != nil {
		return 2
	}
	if *output == "" {
		fmt.Fprintln(os.Stderr, "[backup] --output 必填")
		return 2
	}

	// 过滤掉 backup 特有 flag，剩余参数交给 config.Load 解析 store 配置。
	cfgArgs := filterSubcmdArgs(os.Args[1:], "backup", backupFlagSpecs)
	// 临时替换 os.Args 以复用 config.Load（它解析全局 flag 表）。
	oldArgs := os.Args
	os.Args = append([]string{"opsmesh"}, cfgArgs...)
	cfg := config.Load()
	os.Args = oldArgs

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "[backup] 配置校验失败: %v\n", err)
		return 1
	}

	st, err := controlplane.NewStoreForCLI(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[backup] Store 初始化失败: %v\n", err)
		return 1
	}

	opts := controlplane.ExportOptions{
		Format:          *format,
		IncludeConfig:   *includeConfig,
		IncludeAudits:   *includeAudits,
		TaskWindowDays:  *taskWindowDays,
		AlertWindowDays: *alertWindowDays,
		AuditWindowDays: *auditWindowDays,
	}
	data, err := controlplane.ExportBackupFile(context.Background(), st, cfg, opts, *output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[backup] 导出失败: %v\n", err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "[backup] 导出成功 → %s\n", *output)
	fmt.Fprintf(os.Stderr, "  设备: %d, agent: %d, 任务: %d, 告警: %d\n",
		data.Meta.Counts.Devices, data.Meta.Counts.Agents, data.Meta.Counts.Tasks, data.Meta.Counts.Alerts)
	fmt.Fprintf(os.Stderr, "  告警规则: %d, 用户: %d, 角色: %d, 权限: %d\n",
		data.Meta.Counts.AlertRules, data.Meta.Counts.Users, data.Meta.Counts.Roles, data.Meta.Counts.Permissions)
	if data.Meta.IncludeAudits {
		fmt.Fprintf(os.Stderr, "  审计: %d\n", data.Meta.Counts.Audits)
	}
	return 0
}

// runRestore 实现 restore 子命令：从备份文件导入数据到 Store。
//
// 用法：
//
//	opsmesh restore --input <file> [--dry-run] [--overwrite]
//	                [--store memory|mysql] [--mysql-dsn ...] ...
//
// 仅支持 JSON 格式（SQL dump 需先用 mysql 客户端导入 DB）。
// 退出码：0=成功，1=运行错误，2=参数错误。
func runRestore() int {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	input := fs.String("input", "", "备份输入文件路径（必填，JSON 格式）")
	dryRun := fs.Bool("dry-run", false, "只校验不实际写入")
	overwrite := fs.Bool("overwrite", false, "覆盖已存在的数据（否则跳过）")

	var parseArgs []string
	for _, a := range os.Args[1:] {
		if a == "restore" {
			continue
		}
		parseArgs = append(parseArgs, a)
	}
	if err := fs.Parse(parseArgs); err != nil {
		return 2
	}
	if *input == "" {
		fmt.Fprintln(os.Stderr, "[restore] --input 必填")
		return 2
	}

	cfgArgs := filterSubcmdArgs(os.Args[1:], "restore", restoreFlagSpecs)
	oldArgs := os.Args
	os.Args = append([]string{"opsmesh"}, cfgArgs...)
	cfg := config.Load()
	os.Args = oldArgs

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "[restore] 配置校验失败: %v\n", err)
		return 1
	}

	st, err := controlplane.NewStoreForCLI(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[restore] Store 初始化失败: %v\n", err)
		return 1
	}

	opts := controlplane.ImportOptions{
		DryRun:    *dryRun,
		Overwrite: *overwrite,
	}
	_, res, err := controlplane.ImportBackupFile(context.Background(), st, opts, *input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[restore] 导入失败: %v\n", err)
		return 1
	}

	mode := "导入"
	if *dryRun {
		mode = "校验（dry-run）"
	}
	fmt.Fprintf(os.Stderr, "[restore] %s成功 ← %s\n", mode, *input)
	fmt.Fprintf(os.Stderr, "  设备: %d, agent: %d, 任务: %d, 告警: %d\n",
		res.Devices, res.Agents, res.Tasks, res.Alerts)
	fmt.Fprintf(os.Stderr, "  告警规则: %d, 用户: %d, 角色: %d, 跳过: %d\n",
		res.AlertRules, res.Users, res.Roles, res.Skipped)
	return 0
}
