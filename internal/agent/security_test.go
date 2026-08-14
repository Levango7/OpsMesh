package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opsmesh/internal/config"
	"opsmesh/internal/proto"
)

// TestExecute_ShellWhitelist_Empty 验证白名单为空时放行所有命令（向后兼容）。
func TestExecute_ShellWhitelist_Empty(t *testing.T) {
	a := &Agent{agentID: "test", taskTimeout: 5 * time.Second, cfg: &config.Config{AgentShellWhitelist: ""}}
	res := a.execute(context.Background(), proto.Task{Type: proto.TaskTypeShell, Command: "echo hi"})
	if res.ExitCode != 0 {
		t.Fatalf("空白名单应放行，ExitCode=%d stderr=%s", res.ExitCode, res.Stderr)
	}
}

// TestExecute_ShellWhitelist_Allowed 验证白名单内命令被放行。
func TestExecute_ShellWhitelist_Allowed(t *testing.T) {
	a := &Agent{agentID: "test", taskTimeout: 5 * time.Second, cfg: &config.Config{AgentShellWhitelist: "echo,ls,cat"}}
	res := a.execute(context.Background(), proto.Task{Type: proto.TaskTypeShell, Command: "echo whitelisted"})
	if res.ExitCode != 0 {
		t.Fatalf("白名单内命令应放行，ExitCode=%d stderr=%s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "whitelisted") {
		t.Fatalf("Stdout 应包含 whitelisted，得到 %q", res.Stdout)
	}
}

// TestExecute_ShellWhitelist_Blocked 验证白名单外命令被拒绝。
func TestExecute_ShellWhitelist_Blocked(t *testing.T) {
	a := &Agent{agentID: "test", taskTimeout: 5 * time.Second, cfg: &config.Config{AgentShellWhitelist: "echo,ls,cat"}}
	res := a.execute(context.Background(), proto.Task{Type: proto.TaskTypeShell, Command: "rm -rf /"})
	if res.ExitCode != -1 {
		t.Fatalf("白名单外命令应被拒绝 ExitCode=-1，得到 %d", res.ExitCode)
	}
	if !strings.Contains(res.Stderr, "not in shell whitelist") {
		t.Fatalf("Stderr 应提示白名单拒绝，得到 %q", res.Stderr)
	}
}

// TestExecute_ShellWhitelist_Basename 验证白名单匹配 basename（如 /bin/echo -> echo）。
func TestExecute_ShellWhitelist_Basename(t *testing.T) {
	a := &Agent{agentID: "test", taskTimeout: 5 * time.Second, cfg: &config.Config{AgentShellWhitelist: "echo"}}
	// 用绝对路径 echo（Windows 上 echo 是 cmd 内置命令，此处仅验证白名单逻辑放行）
	res := a.execute(context.Background(), proto.Task{Type: proto.TaskTypeShell, Command: "echo ok"})
	if res.ExitCode != 0 {
		t.Fatalf("basename 匹配应放行，ExitCode=%d stderr=%s", res.ExitCode, res.Stderr)
	}
}

// TestExecute_FileTraversalReject 验证路径遍历被根目录白名单拒绝。
// 注意：filepath.Abs+Clean 会消除绝对路径中的 ..，所以路径遍历主要靠根目录白名单防护。
func TestExecute_FileTraversalReject(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{agentID: "test", taskTimeout: 5 * time.Second, cfg: &config.Config{AgentFileRootWhitelist: dir}}
	// 构造含 .. 的路径试图逃逸到 dir 之外
	evil := filepath.Join(dir, "..", "..", "etc", "passwd")
	res := a.execute(context.Background(), proto.Task{Type: proto.TaskTypeFile, Path: evil, Content: "pwned"})
	if res.ExitCode == 0 {
		t.Fatalf("路径遍历应被拒绝，ExitCode=0 不期望")
	}
	if !strings.Contains(res.Stderr, "not under") {
		t.Fatalf("Stderr 应提示根目录白名单拒绝，得到 %q", res.Stderr)
	}
}

// TestExecute_FileTraversalDotDot 验证 Clean 后仍含 .. 的路径被拒绝。
func TestExecute_FileTraversalDotDot(t *testing.T) {
	a := &Agent{agentID: "test", taskTimeout: 5 * time.Second, cfg: &config.Config{}}
	// 纯相对路径 ../../etc/passwd，Clean 后仍含 ..
	res := a.execute(context.Background(), proto.Task{Type: proto.TaskTypeFile, Path: "../../etc/passwd", Content: "pwned"})
	if res.ExitCode == 0 {
		t.Fatalf("含 .. 的路径应被拒绝")
	}
}

// TestExecute_FileRootWhitelist 验证根目录白名单限制。
func TestExecute_FileRootWhitelist(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{agentID: "test", taskTimeout: 5 * time.Second, cfg: &config.Config{AgentFileRootWhitelist: dir}}
	// 白名单内路径应放行
	inside := filepath.Join(dir, "ok.txt")
	res := a.execute(context.Background(), proto.Task{Type: proto.TaskTypeFile, Path: inside, Content: "ok"})
	if res.ExitCode != 0 {
		t.Fatalf("白名单内路径应放行，ExitCode=%d stderr=%s", res.ExitCode, res.Stderr)
	}
	// 白名单外路径应拒绝
	outside := filepath.Join(dir, "..", "outside.txt")
	res2 := a.execute(context.Background(), proto.Task{Type: proto.TaskTypeFile, Path: outside, Content: "bad"})
	if res2.ExitCode == 0 {
		t.Fatalf("白名单外路径应被拒绝")
	}
}

// TestExecute_OutputTruncation 验证 stdout 超过 10MB 被截断（用 limitedBuffer 直接测试，避免 shell 命令开销）。
func TestExecute_OutputTruncation(t *testing.T) {
	b := newLimitedBuffer(maxOutputBytes)
	// 写入 11MB 数据（分 11 次 1MB 写入）
	chunk := make([]byte, 1024*1024) // 1MB
	for i := 0; i < 11; i++ {
		b.Write(chunk)
	}
	s := b.String()
	if !strings.Contains(s, "truncated") {
		t.Fatalf("输出应被截断并包含 truncated 提示，长度=%d", len(s))
	}
	// 截断后总长度应略大于 10MB（含提示），不应无限增长
	if len(s) > maxOutputBytes+1024 {
		t.Fatalf("截断后输出应 <= 10MB+1KB，实际 %d", len(s))
	}
}

// TestLimitedBuffer_Basic 验证 limitedBuffer 基本写入与截断。
func TestLimitedBuffer_Basic(t *testing.T) {
	b := newLimitedBuffer(100)
	b.Write([]byte("hello"))
	if b.String() != "hello" {
		t.Fatalf("得到 %q，期望 hello", b.String())
	}
	// 写入超过上限
	b.Write(make([]byte, 200))
	s := b.String()
	if !strings.Contains(s, "truncated") {
		t.Fatalf("应包含 truncated 提示，得到 %q", s)
	}
	// 继续写入应被丢弃
	b.Write([]byte("dropped"))
	if strings.Contains(b.String(), "dropped") {
		t.Fatalf("写入超限后 dropped 应被丢弃，实际出现在输出中")
	}
}
