// cmd/opsmesh 入口逻辑补充单测。
//
// 补测重点：
//  1. filterSubcmdArgs 纯函数：子命令名移除、特有 flag 过滤（带值/布尔/等号/单横线）、未知 flag 透传；
//  2. runBackup 子命令：--output 必填校验、未知 flag、配置校验失败、成功导出（memory store）、--format=sql；
//  3. runRestore 子命令：--input 必填校验、未知 flag、文件不存在、dry-run、成功导入、--overwrite、配置校验失败；
//  4. runMain backup/restore 分支：runMain 识别子命令名并分派到 runBackup/runRestore。
//
// withCLIArgs 替换 flag.CommandLine + os.Args，使 runBackup/runRestore 内部的 config.Load
// 能解析模拟参数（testing 框架在 TestMain 前已对原 CommandLine 调过 flag.Parse，直接复用会 no-op）。
package main

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
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
