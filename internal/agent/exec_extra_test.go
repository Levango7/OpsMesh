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
	// pid=1 (init 进程，无权限杀，但不应 panic)
	// Windows: 会执行 taskkill /T /F /PID 1，失败但返回 nil
	// Linux: syscall.Kill(-1, SIGTERM) 可能返回 EPERM，但不应 panic
	_ = killProcessGroup(1)
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