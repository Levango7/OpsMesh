//go:build !linux && !darwin

package agent

import "runtime"

// setRlimits 在非 POSIX 平台（如 Windows）无对应系统调用，跳过。
// 等保纵深防御在控制面 / IAM 侧兜底；agent 进程限额仅作单机加固，非跨平台必需能力。
func setRlimits(a *Agent) {
	_ = a
	// Windows 不支持 syscall.Setrlimit；如需等价能力可借 job object（超出 MVP 范围）。
	_ = runtime.GOOS
}
