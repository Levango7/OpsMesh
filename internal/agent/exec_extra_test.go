// exec_extra_test.go 补充 exec_other.go / exec_unix.go 中未覆盖的函数单元测试。
//
// 覆盖：
//   - setProcessGroup 不 panic
//   - killProcessGroup 各 pid 边界（pid<=0 直接返回、pid>0 平台路径）
package agent

import (
	"os/exec"
	"testing"
)

// --- setProcessGroup ---

func TestSetProcessGroup_Extra(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("setProcessGroup panic: %v", r)
		}
	}()
	cmd := exec.Command("echo", "hi")
	setProcessGroup(cmd)
	// 不 panic 即可（平台特定实现）
}

// --- killProcessGroup ---

func TestKillProcessGroup_ZeroPid(t *testing.T) {
	if err := killProcessGroup(0); err != nil {
		t.Fatalf("pid=0 应返回 nil，得到 %v", err)
	}
}

func TestKillProcessGroup_NegativePid(t *testing.T) {
	if err := killProcessGroup(-1); err != nil {
		t.Fatalf("pid=-1 应返回 nil，得到 %v", err)
	}
}

func TestKillProcessGroup_InvalidPid(t *testing.T) {
	// pid 为非常大的负数也应返回 nil（pid<=0 分支）
	if err := killProcessGroup(-99999); err != nil {
		t.Fatalf("pid=-99999 应返回 nil，得到 %v", err)
	}
}

func TestKillProcessGroup_ValidPid_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("killProcessGroup panic: %v", r)
		}
	}()
	// CI 自杀修复：此前传 pid=1 —— Linux 上 killProcessGroup(1) = syscall.Kill(-1, SIGTERM)，
	// 负 PID 语义是"向所有有权限的进程组广播 SIGTERM"，在 CI 容器内测试进程有权限
	// 杀整个 job 进程树（runner shell/node 全灭，exit 143 正是 SIGTERM 自杀）。
	// Windows 能过纯属 taskkill 无广播语义掩盖了问题。
	// 正确测法：fork 一个 sleep 子进程（Setpgid 使其自成进程组，PGID=PID），
	// 杀它的组——真正覆盖"合法存在且可杀的进程组"路径，且只影响测试自有子进程。
	cmd := exec.Command("sleep", "30")
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Skipf("无法启动 sleep 子进程（环境无 sleep？），跳过: %v", err)
	}
	defer func() { _ = cmd.Wait() }()
	if err := killProcessGroup(cmd.Process.Pid); err != nil {
		t.Fatalf("killProcessGroup(子进程组) 意外错误: %v", err)
	}
}

func TestKillProcessGroup_SelfPid(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("killProcessGroup panic: %v", r)
		}
	}()
	// 用一个肯定不存在的 pid（大数），不应 panic
	// Windows: taskkill 会失败但返回 nil
	// Linux: syscall.Kill(-pid, SIGTERM) 返回 ESRCH，但不应 panic
	_ = killProcessGroup(999999)
}
