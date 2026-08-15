//go:build linux || darwin

package agent

import (
	"os/exec"
	"syscall"
)

// setProcessGroup 在子进程上设置 Setpgid=true，使其成为新进程组的 leader（task 78 安全加固）。
// 取消/超时时可经 killProcessGroup 杀整个进程组（包括子进程 fork 出的后台进程），避免孤儿后台进程继续运行。
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessGroup 杀整个进程组（task 78 安全加固）。
// pid 是子进程的 PID（因 Setpgid=true，PGID=PID）。负 pid 表示杀整个进程组。
// 先发 SIGTERM 优雅终止；与原 exec.CommandContext 的取消语义对齐（ctx 取消即终止）。
func killProcessGroup(pid int) error {
	if pid <= 0 {
		return nil
	}
	return syscall.Kill(-pid, syscall.SIGTERM)
}
