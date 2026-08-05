//go:build !linux && !darwin

package agent

import (
	"log"
	"os/exec"
	"runtime"
	"strconv"
)

// setProcessGroup 在非 POSIX 平台（如 Windows）是 noop（task 78 安全加固）。
// Windows 不支持 syscall.SysProcAttr.Setpgid；取消时改用 taskkill /T /F /PID 杀进程树。
func setProcessGroup(cmd *exec.Cmd) {
	// Windows 与其他非 POSIX 平台均无 Setpgid 等价物，noop。
	_ = cmd
}

// killProcessGroup 在 Windows 上异步启动 taskkill /T /F /PID 杀进程树（task 78 安全加固）。
// /T 表示杀进程树（含子进程），/F 表示强制终止。
// 异步启动（不等待 taskkill 返回）避免 taskkill.exe 启动开销（~数百 ms）阻塞取消流程；
// 父进程由 executeShell 中的 cmd.Process.Kill() 同步杀掉，taskkill 仅负责清理子进程树。
// 其他非 POSIX 非 Windows 平台（罕见）无等价命令，仅返回 nil。
func killProcessGroup(pid int) error {
	if pid <= 0 {
		return nil
	}
	if runtime.GOOS == "windows" {
		// 异步启动 taskkill，不等待返回（避免 taskkill.exe 启动开销阻塞取消流程）。
		// 父进程已由 executeShell 的 cmd.Process.Kill() 同步终止，此处仅清理子进程树。
		c := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
		if err := c.Start(); err != nil {
			log.Printf("agent: 启动 taskkill 杀进程树(pid=%d) 失败: %v", pid, err)
		}
		return nil
	}
	// 其他非 POSIX 平台无 taskkill，noop（依赖 exec 默认取消语义）。
	return nil
}