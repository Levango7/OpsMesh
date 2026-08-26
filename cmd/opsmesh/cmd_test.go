// cmd/opsmesh 入口逻辑补充单测。
//
// 补测重点：
//  1. filterSubcmdArgs 纯函数：子命令名移除、特有 flag 过滤（带值/布尔/等号/单横线）、未知 flag 透传；
//  2. runBackup 子命令：--output 必填校验、未知 flag、配置校验失败、成功导出（memory store）、--format=sql；
//  3. runRestore 子命令：--input 必填校验、未知 flag、文件不存在、dry-run、成功导入、--overwrite、配置校验失败；
//  4. runMain backup/restore 分支：runMain 识别子命令名并分派到 runBackup/runRestore；
//  5. runMain controlplane/agent 模式分支的快速失败路径（端口占用 / TLS 凭证缺失 / 生产模式 MySQL fail-fast）；
//  6. runBackup/runRestore 的 Store 初始化失败（非法 DSN → sql.Open 立即报错）与备份导出写文件失败。
//
// 不可测部分说明：
//   - runMain 的 default 分支（未知 --mode）：config.Validate 保证 Mode ∈ {controlplane, agent}，
//     且 runMain 内部自行调用 config.Load 无法注入 cfg，该分支为防御性死代码，无法触达；
//   - 信号处理：signal.NotifyContext 位于 internal/controlplane.Start 与 internal/agent.Run 内部，
//     cmd 包层面无可注入的信号处理逻辑，不在本包测试范围。
//
// withCLIArgs 替换 flag.CommandLine + os.Args，使 runBackup/runRestore 内部的 config.Load
// 能解析模拟参数（testing 框架在 TestMain 前已对原 CommandLine 调过 flag.Parse，直接复用会 no-op）。
package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

// withCLIArgs 临时替换 flag.CommandLine 与 os.Args，使 runBackup/runRestore 内部的
// config.Load 能解析模拟参数。返回恢复函数，defer 调用恢复原状。
func withCLIArgs(args []string) func() {
	oldFS := flag.CommandLine
	oldArgs := os.Args
	flag.CommandLine = flag.NewFlagSet("opsmesh", flag.ExitOnError)
	os.Args = args
	return func() {
		flag.CommandLine = oldFS
		os.Args = oldArgs
	}
}

// withEnv 临时设置环境变量，返回恢复函数。用于让 config.Load 的 env 兜底触发 Validate 失败。
func withEnv(key, val string) func() {
	old, had := os.LookupEnv(key)
	os.Setenv(key, val)
	return func() {
		if had {
			os.Setenv(key, old)
		} else {
			os.Unsetenv(key)
		}
	}
}

// writeMinimalBackup 写一个最小有效 JSON 备份文件（空数据 + 合法 Meta）。
// ImportBackup 校验 Meta.Version 非空即放行，空数据导入/dry-run 均成功。
func writeMinimalBackup(t *testing.T, path string) {
	t.Helper()
	const data = `{"meta":{"version":"0.7.0","createdAt":"2024-01-01T00:00:00Z","format":"json"}}`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
}

// ---------- filterSubcmdArgs 纯函数测试 ----------

func TestFilterSubcmdArgs_Empty(t *testing.T) {
	got := filterSubcmdArgs(nil, "backup", backupFlagSpecs)
	if len(got) != 0 {
		t.Fatalf("空输入应返回空, got=%v", got)
	}
}

func TestFilterSubcmdArgs_OnlySubcmd(t *testing.T) {
	got := filterSubcmdArgs([]string{"backup"}, "backup", backupFlagSpecs)
	if len(got) != 0 {
		t.Fatalf("仅子命令名应被移除, got=%v", got)
	}
}

func TestFilterSubcmdArgs_Passthrough(t *testing.T) {
	// 非特有 flag 应原样透传（--store/--http-port 不在 backupFlagSpecs）。
	in := []string{"backup", "--store", "memory", "--http-port=8080"}
	got := filterSubcmdArgs(in, "backup", backupFlagSpecs)
	want := []string{"--store", "memory", "--http-port=8080"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("非特有 flag 应透传, got=%v want=%v", got, want)
	}
}

func TestFilterSubcmdArgs_FlagWithValue(t *testing.T) {
	// --output file：带值 flag 空格形式，当前 token 与下一 token（值）都移除。
	in := []string{"backup", "--output", "file.json"}
	got := filterSubcmdArgs(in, "backup", backupFlagSpecs)
	if len(got) != 0 {
		t.Fatalf("--output file 应被移除, got=%v", got)
	}
}

func TestFilterSubcmdArgs_FlagWithEquals(t *testing.T) {
	// --output=file：等号形式，仅当前 token 移除。
	in := []string{"backup", "--output=file.json"}
	got := filterSubcmdArgs(in, "backup", backupFlagSpecs)
	if len(got) != 0 {
		t.Fatalf("--output=file 应被移除, got=%v", got)
	}
}

func TestFilterSubcmdArgs_BoolFlag(t *testing.T) {
	// --include-config：布尔 flag（takesValue=false），仅当前 token 移除，下一 token 保留。
	in := []string{"backup", "--include-config", "--store", "memory"}
	got := filterSubcmdArgs(in, "backup", backupFlagSpecs)
	want := []string{"--store", "memory"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("布尔 flag 移除后其余透传, got=%v want=%v", got, want)
	}
}

func TestFilterSubcmdArgs_UnknownFlag(t *testing.T) {
	// 未知 flag 不在 specs 中，应保留（含其值）。
	in := []string{"backup", "--unknown-flag", "val"}
	got := filterSubcmdArgs(in, "backup", backupFlagSpecs)
	want := []string{"--unknown-flag", "val"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("未知 flag 应保留, got=%v want=%v", got, want)
	}
}

func TestFilterSubcmdArgs_Mixed(t *testing.T) {
	// 混合：特有 flag（带值空格/布尔/等号）+ 非特有 flag，仅保留非特有 flag。
	in := []string{"backup", "--output", "f.json", "--store", "mysql", "--include-config", "--format=sql", "--http-port=9090"}
	got := filterSubcmdArgs(in, "backup", backupFlagSpecs)
	want := []string{"--store", "mysql", "--http-port=9090"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("混合场景过滤错误, got=%v want=%v", got, want)
	}
}

func TestFilterSubcmdArgs_SingleDashFlag(t *testing.T) {
	// -output file：单横线变体，TrimLeft("-") 后仍匹配 specs 中的 "output"。
	in := []string{"backup", "-output", "f.json"}
	got := filterSubcmdArgs(in, "backup", backupFlagSpecs)
	if len(got) != 0 {
		t.Fatalf("-output file 应被移除, got=%v", got)
	}
}

func TestFilterSubcmdArgs_RestSpecs(t *testing.T) {
	// restore 特有 flag 过滤：--input（带值）、--dry-run/--overwrite（布尔）都移除。
	in := []string{"restore", "--input", "backup.json", "--dry-run", "--overwrite", "--store", "memory"}
	got := filterSubcmdArgs(in, "restore", restoreFlagSpecs)
	want := []string{"--store", "memory"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("restore specs 过滤错误, got=%v want=%v", got, want)
	}
}

func TestFilterSubcmdArgs_NoSubcmdInArgs(t *testing.T) {
	// args 中不含子命令名：仅按 specs 过滤 flag。
	in := []string{"--output", "f.json", "--store", "memory"}
	got := filterSubcmdArgs(in, "backup", backupFlagSpecs)
	want := []string{"--store", "memory"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("无子命令名时过滤错误, got=%v want=%v", got, want)
	}
}

// ---------- runBackup 子命令测试 ----------

func TestRunBackup_NoOutput(t *testing.T) {
	// --output 必填，缺失返回 2。
	defer withCLIArgs([]string{"opsmesh", "backup"})()
	if code := runBackup(); code != 2 {
		t.Fatalf("runBackup 无 --output=%d, want 2", code)
	}
}

func TestRunBackup_BadFlag(t *testing.T) {
	// 未知 flag：backup FlagSet(ContinueOnError).Parse 失败返回 2。
	defer withCLIArgs([]string{"opsmesh", "backup", "--no-such-flag"})()
	if code := runBackup(); code != 2 {
		t.Fatalf("runBackup 未知 flag=%d, want 2", code)
	}
}

func TestRunBackup_Success(t *testing.T) {
	// 默认 memory store，导出空数据到临时文件，应成功返回 0 且文件非空。
	out := filepath.Join(t.TempDir(), "backup.json")
	defer withCLIArgs([]string{"opsmesh", "backup", "--output", out})()
	if code := runBackup(); code != 0 {
		t.Fatalf("runBackup 成功路径=%d, want 0", code)
	}
	if info, err := os.Stat(out); err != nil {
		t.Fatalf("备份文件未生成: %v", err)
	} else if info.Size() == 0 {
		t.Fatalf("备份文件为空")
	}
}

func TestRunBackup_ConfigValidateFail(t *testing.T) {
	// 通过 env OPSMESH_MODE=invalid 让 config.Validate 失败返回 1
	// （不能直接传 --mode=invalid，backup FlagSet 不认识会先返回 2）。
	defer withEnv("OPSMESH_MODE", "invalid")()
	out := filepath.Join(t.TempDir(), "backup.json")
	defer withCLIArgs([]string{"opsmesh", "backup", "--output", out})()
	if code := runBackup(); code != 1 {
		t.Fatalf("runBackup 配置校验失败=%d, want 1", code)
	}
}

func TestRunBackup_FormatSQL(t *testing.T) {
	// --format=sql 等号 flag + --include-audits 布尔 flag，验证 backup FlagSet 解析与 SQL dump 导出。
	out := filepath.Join(t.TempDir(), "backup.sql")
	defer withCLIArgs([]string{"opsmesh", "backup", "--output", out, "--format=sql", "--include-audits"})()
	if code := runBackup(); code != 0 {
		t.Fatalf("runBackup --format=sql --include-audits=%d, want 0", code)
	}
	if info, err := os.Stat(out); err != nil {
		t.Fatalf("备份文件未生成: %v", err)
	} else if info.Size() == 0 {
		t.Fatalf("备份文件为空")
	}
}

func TestRunBackup_WindowDays(t *testing.T) {
	// --task-window-days/--alert-window-days/--audit-window-days 带值 flag，验证解析与导出。
	out := filepath.Join(t.TempDir(), "backup.json")
	defer withCLIArgs([]string{"opsmesh", "backup", "--output", out,
		"--task-window-days=14", "--alert-window-days=3", "--audit-window-days=60"})()
	if code := runBackup(); code != 0 {
		t.Fatalf("runBackup window-days=%d, want 0", code)
	}
}

// ---------- runRestore 子命令测试 ----------

func TestRunRestore_NoInput(t *testing.T) {
	// --input 必填，缺失返回 2。
	defer withCLIArgs([]string{"opsmesh", "restore"})()
	if code := runRestore(); code != 2 {
		t.Fatalf("runRestore 无 --input=%d, want 2", code)
	}
}

func TestRunRestore_BadFlag(t *testing.T) {
	// 未知 flag：restore FlagSet(ContinueOnError).Parse 失败返回 2。
	defer withCLIArgs([]string{"opsmesh", "restore", "--no-such-flag"})()
	if code := runRestore(); code != 2 {
		t.Fatalf("runRestore 未知 flag=%d, want 2", code)
	}
}

func TestRunRestore_NonExistFile(t *testing.T) {
	// --input 指向不存在文件：ImportBackupFile os.Open 失败返回 1。
	in := filepath.Join(t.TempDir(), "nonexist.json")
	defer withCLIArgs([]string{"opsmesh", "restore", "--input", in})()
	if code := runRestore(); code != 1 {
		t.Fatalf("runRestore 不存在文件=%d, want 1", code)
	}
}

func TestRunRestore_DryRun(t *testing.T) {
	// --dry-run：只校验不写入，空备份应成功返回 0。
	in := filepath.Join(t.TempDir(), "backup.json")
	writeMinimalBackup(t, in)
	defer withCLIArgs([]string{"opsmesh", "restore", "--input", in, "--dry-run"})()
	if code := runRestore(); code != 0 {
		t.Fatalf("runRestore --dry-run=%d, want 0", code)
	}
}

func TestRunRestore_Success(t *testing.T) {
	// 实际导入空备份到 memory store，应成功返回 0。
	in := filepath.Join(t.TempDir(), "backup.json")
	writeMinimalBackup(t, in)
	defer withCLIArgs([]string{"opsmesh", "restore", "--input", in})()
	if code := runRestore(); code != 0 {
		t.Fatalf("runRestore 成功路径=%d, want 0", code)
	}
}

func TestRunRestore_Overwrite(t *testing.T) {
	// --overwrite 标志，空备份导入应成功。
	in := filepath.Join(t.TempDir(), "backup.json")
	writeMinimalBackup(t, in)
	defer withCLIArgs([]string{"opsmesh", "restore", "--input", in, "--overwrite"})()
	if code := runRestore(); code != 0 {
		t.Fatalf("runRestore --overwrite=%d, want 0", code)
	}
}

func TestRunRestore_ConfigValidateFail(t *testing.T) {
	// 通过 env OPSMESH_MODE=invalid 让 config.Validate 失败返回 1。
	defer withEnv("OPSMESH_MODE", "invalid")()
	in := filepath.Join(t.TempDir(), "backup.json")
	writeMinimalBackup(t, in)
	defer withCLIArgs([]string{"opsmesh", "restore", "--input", in})()
	if code := runRestore(); code != 1 {
		t.Fatalf("runRestore 配置校验失败=%d, want 1", code)
	}
}

// ---------- runMain backup/restore 分支测试 ----------

func TestRunMainBackup(t *testing.T) {
	// runMain 识别 "backup" 子命令 → runBackup → 成功返回 0。
	out := filepath.Join(t.TempDir(), "backup.json")
	defer withCLIArgs([]string{"opsmesh", "backup", "--output", out})()
	if code := runMain(); code != 0 {
		t.Fatalf("runMain backup=%d, want 0", code)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("备份文件未生成: %v", err)
	}
}

func TestRunMainRestore(t *testing.T) {
	// runMain 识别 "restore" 子命令 → runRestore → dry-run 成功返回 0。
	in := filepath.Join(t.TempDir(), "backup.json")
	writeMinimalBackup(t, in)
	defer withCLIArgs([]string{"opsmesh", "restore", "--input", in, "--dry-run"})()
	if code := runMain(); code != 0 {
		t.Fatalf("runMain restore=%d, want 0", code)
	}
}

// ---------- 端口探测辅助 ----------

// freePort 探测一个当前空闲的 TCP 端口并立即释放。
// 存在极小 TOCTOU 竞态窗口（释放后到被测代码监听前被其他进程抢占），
// 与 main_test.go TestRunHealth_Unreachable 的"启动即关闭"策略一致，测试环境可接受。
func freePort(t *testing.T) int {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("探测空闲端口失败: %v", err)
	}
	port := lis.Addr().(*net.TCPAddr).Port
	if err := lis.Close(); err != nil {
		t.Fatalf("关闭探测 listener 失败: %v", err)
	}
	return port
}

// busyPort 以 wildcard 形式（":port"，与 buildGRPC 的 net.Listen(fmt.Sprintf(":%d", port))
// 完全一致）占用一个 TCP 端口并保持监听，返回 listener 与端口（调用方 defer 关闭释放）。
// 必须用 wildcard 而非 127.0.0.1：Windows 对"具体地址已占用 + wildcard 二次绑定"可能放行，
// 导致冲突注入失效、Start() 进入 select 永久阻塞。
func busyPort(t *testing.T) (*net.TCPListener, int) {
	t.Helper()
	lis, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("占用端口失败: %v", err)
	}
	return lis.(*net.TCPListener), lis.Addr().(*net.TCPAddr).Port
}

// ---------- runMain controlplane 分支测试（快速失败路径） ----------

// TestRunMainControlplaneStartFail 验证 runMain 的 controlplane 分支中 srv.Start() 失败路径：
// 预先占用 gRPC 端口 → NewServer 成功（memory store 无外部依赖）→ Start 内 buildGRPC
// 同步 net.Listen 报 "address already in use" → runMain 打印 "[controlplane] 启动失败" 并返回 1。
// Start 在 buildGRPC 阶段就失败返回，尚未启动任何后台 goroutine，主进程无阻塞/泄漏风险。
func TestRunMainControlplaneStartFail(t *testing.T) {
	lis, grpcPort := busyPort(t)
	defer lis.Close()
	// 预检：确认 wildcard 二次绑定确实被拒绝。若当前环境允许端口重复绑定
	//（如特殊 socket 选项/容器网络），注入失效且 Start 会阻塞，跳过而非卡死测试套件。
	if probe, err := net.Listen("tcp", fmt.Sprintf(":%d", grpcPort)); err == nil {
		probe.Close()
		lis.Close()
		t.Skip("当前环境允许 wildcard 端口重复绑定，无法模拟 gRPC 端口冲突")
	}
	httpPort := freePort(t) // buildGRPC 先于 HTTP 监听执行，这两个端口实际不会被绑定
	metricsPort := freePort(t)

	defer withCLIArgs([]string{"opsmesh",
		"--mode=controlplane",
		"--grpc-port=" + strconv.Itoa(grpcPort),
		"--http-port=" + strconv.Itoa(httpPort),
		"--metrics-port=" + strconv.Itoa(metricsPort),
	})()

	if code := runMain(); code != 1 {
		t.Fatalf("runMain controlplane gRPC 端口冲突=%d, want 1", code)
	}
}

// TestRunMainControlplaneNewServerFail 验证 runMain 的 controlplane 分支中 NewServer 失败路径：
// 生产模式 fail-fast——--production=true + --store=mysql + 非法格式 DSN，
// config.Validate 放行（DSN 非空即过），NewServer → selectStore → store.NewSQLStore →
// sql.Open 对非法 DSN 立即报错 → 生产模式不回退 memory 而是返回 error → runMain 返回 1。
// 前置约束：生产模式下 Validate 要求 TLS 证书、JWT 密钥（≥32 字节）、kubeconfig 加密密钥非空，
// 且 mysql 后端需显式 --allow-stub-stores=true 接受桩实现限制，否则在更早的校验处返回 1（语义不同）。
func TestRunMainControlplaneNewServerFail(t *testing.T) {
	defer withCLIArgs([]string{"opsmesh",
		"--mode=controlplane",
		"--production=true",
		"--tls-cert=nonexistent-cert.pem", // Validate 仅查非空；NewServer 在 selectStore 即失败，不会真正加载
		"--tls-key=nonexistent-key.pem",
		"--jwt-secret=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", // 64 字符 ≥ 32 字节
		"--encryption-key=MTIzNDU2Nzg5MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2",             // base64(32 字节)
		"--allow-stub-stores=true",
		"--store=mysql",
		"--mysql-dsn=!!!invalid dsn without slash!!!", // go-sql-driver ParseDSN 立即报错（缺斜杠），无网络 IO
	})()

	if code := runMain(); code != 1 {
		t.Fatalf("runMain controlplane 生产模式非法 DSN=%d, want 1 (NewServer fail-fast)", code)
	}
}

// ---------- runMain agent 分支测试（快速失败路径） ----------

// TestRunMainAgentRunFail 验证 runMain 的 agent 分支中 ag.Run() 失败路径：
// --tls-cert/--tls-key 指向不存在的文件 → agent.Run 内 NewGRPCClient →
// tls.LoadX509KeyPair 读文件立即报错（纯本地 IO，不发起任何网络连接与注册重试）→
// Run 返回 error → runMain 打印 "[agent] 启动失败" 并返回 1。
// otelx.Init 在此之前因 endpoint 为空且 stdout=false 为 no-op，不会阻塞。
func TestRunMainAgentRunFail(t *testing.T) {
	defer withCLIArgs([]string{"opsmesh",
		"--mode=agent",
		"--tls-cert=" + filepath.Join(t.TempDir(), "no-such-cert.pem"),
		"--tls-key=" + filepath.Join(t.TempDir(), "no-such-key.pem"),
	})()

	if code := runMain(); code != 1 {
		t.Fatalf("runMain agent TLS 凭证缺失=%d, want 1 (Run 快速失败)", code)
	}
}

// ---------- runBackup/runRestore Store 初始化失败分支 ----------

// TestRunBackupStoreInitFail 验证 runBackup 中 NewStoreForCLI 失败路径：
// 经环境变量兜底注入 --store=mysql + 非法格式 DSN（backup FlagSet 不注册 store/mysql-dsn，
// 无法从命令行传入，与生产用法一致走 OPSMESH_* env 兜底）→ selectStore → sql.Open
// 由 go-sql-driver OpenConnector 立即解析报错 → runBackup 打印 "[backup] Store 初始化失败" 返回 1。
// 相比不可达 DSN（会触发 NewSQLStore 内部最长约 30s 的迁移重试且最终仍成功），
// 格式非法是唯一同步快速失败的注入点。
func TestRunBackupStoreInitFail(t *testing.T) {
	out := filepath.Join(t.TempDir(), "backup.json")
	defer withEnv("OPSMESH_STORE", "mysql")()
	defer withEnv("OPSMESH_MYSQL_DSN", "!!!invalid dsn!!!")()
	defer withCLIArgs([]string{"opsmesh", "backup", "--output", out})()

	if code := runBackup(); code != 1 {
		t.Fatalf("runBackup 非法 DSN=%d, want 1 (Store 初始化失败)", code)
	}
}

// TestRunRestoreStoreInitFail 验证 runRestore 中 NewStoreForCLI 失败路径（同上注入手段）。
func TestRunRestoreStoreInitFail(t *testing.T) {
	in := filepath.Join(t.TempDir(), "backup.json")
	writeMinimalBackup(t, in)
	defer withEnv("OPSMESH_STORE", "mysql")()
	defer withEnv("OPSMESH_MYSQL_DSN", "!!!invalid dsn!!!")()
	defer withCLIArgs([]string{"opsmesh", "restore", "--input", in})()

	if code := runRestore(); code != 1 {
		t.Fatalf("runRestore 非法 DSN=%d, want 1 (Store 初始化失败)", code)
	}
}

// ---------- runBackup 导出写文件失败分支 ----------

// TestRunBackupOutputIsDir 验证 runBackup 中 ExportBackupFile 写文件失败路径：
// --output 指向一个已存在的目录 → os.Create(output) 失败 → 导出错误 → runBackup 返回 1。
// （Store 初始化成功走 memory，失败发生在写盘阶段。）
func TestRunBackupOutputIsDir(t *testing.T) {
	outDir := t.TempDir() // 目录本身作为输出目标，os.Create 必然失败
	defer withCLIArgs([]string{"opsmesh", "backup", "--output", outDir})()

	if code := runBackup(); code != 1 {
		t.Fatalf("runBackup --output 为目录=%d, want 1 (导出失败)", code)
	}
}

// ---------- filterSubcmdArgs 边界补充（等号空值形式） ----------

// TestFilterSubcmdArgs_FlagEqualsEmptyValue 验证 --flag= 形式（等号后空值）：
// hasEq=true 时即使 takesValue=true 也仅移除当前 token，不会误吞下一 token。
func TestFilterSubcmdArgs_FlagEqualsEmptyValue(t *testing.T) {
	in := []string{"backup", "--format=", "--store", "memory"}
	got := filterSubcmdArgs(in, "backup", backupFlagSpecs)
	want := []string{"--store", "memory"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("--flag= 空值形式过滤错误, got=%v want=%v", got, want)
	}
}

// TestFilterSubcmdArgs_SubcmdAnywhere 验证子命令名出现在任意位置都会被移除（非仅首 token）。
func TestFilterSubcmdArgs_SubcmdAnywhere(t *testing.T) {
	in := []string{"--store", "memory", "restore", "--dry-run"}
	got := filterSubcmdArgs(in, "restore", restoreFlagSpecs)
	want := []string{"--store", "memory"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("子命令名任意位置移除错误, got=%v want=%v", got, want)
	}
}

