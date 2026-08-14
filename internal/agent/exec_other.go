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

// killProcessGroup 在 Windows 上同步执行 taskkill /T /F /PID 杀进程树（task 78 安全加固）。
// /T 表示杀进程树（含子进程），/F 表示强制终止。
// 同步等待 taskkill 返回，确保子进程被终止后再继续：
// Windows 上 cmd.Wait() 会等待 stdout/stderr pipe 关闭，而子进程（如 cmd /C sleep 2 中的 sleep.exe）
// 可能继承了 pipe。若 taskkill 异步执行，pipe 不会立即关闭，cmd.Wait() 会阻塞至子进程自然退出。
// 同步 taskkill 确保子进程被杀、pipe 关闭，cmd.Wait() 立即返回（修复 Windows flaky TestExecute_Timeout）。
// 其他非 POSIX 非 Windows 平台（罕见）无等价命令，仅返回 nil。
func killProcessGroup(pid int) error {
	if pid <= 0 {
		return nil
	}
	if runtime.GOOS == "windows" {
		// 同步执行 taskkill，等待整个进程树被终止。
		c := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
		if err := c.Run(); err != nil {
			log.Printf("agent: taskkill 杀进程树(pid=%d) 失败: %v", pid, err)
		}
		return nil
	}
	// 其他非 POSIX 平台无 taskkill，noop（依赖 exec 默认取消语义）。
	return nil
}