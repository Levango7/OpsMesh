//go:build linux || darwin

package agent

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// setRlimits 应用 POSIX 进程资源限额。所有选项为 0 时不限制。
// 注意：RLIMIT_AS 设置过低可能导致进程被杀；作为等保/多租户隔离的纵深防御，默认不开启。
//
// ⚠ 本函数修改的是**整个进程**的 rlimit，且 Cur/Max 一并下调（不可逆：普通进程
// 无法再把 Max 抬回去）。在生产里这是有意的（防止运行期自行放宽隔离），但任何
// 测试都不得在本进程内用非 0 值调用它——Go 测试二进制的后续分配/覆盖率收尾需要
// 新 mmap，会被锁死成 fatal OOM。非 0 用例见 rlimit_extra_test.go 的子进程写法。
//
// Go 1.26 兼容：Linux 平台的 RLIMIT_NPROC 已从 syscall 包移除（syscall 仅保留
// RLIMIT_AS/RLIMIT_NOFILE），统一改用 golang.org/x/sys/unix 的常量与类型，
// 避免"本地 Windows 不编译该文件、CI Linux 才报 undefined"的跨平台遗漏。
func setRlimits(a *Agent) {
	r := &unix.Rlimit{}
	set := func(res int, v int64, name string) {
		if v <= 0 {
			return
		}
		r.Cur = uint64(v)
		r.Max = uint64(v)
		if err := unix.Setrlimit(res, r); err != nil {
			fmt.Printf("[agent] 设置 %s 失败: %v\n", name, err)
		}
	}
	set(unix.RLIMIT_NPROC, int64(a.cfg.MaxProcs), "RLIMIT_NPROC")
	set(unix.RLIMIT_NOFILE, int64(a.cfg.MaxFiles), "RLIMIT_NOFILE")
	set(unix.RLIMIT_AS, a.cfg.MaxMemoryMB*1024*1024, "RLIMIT_AS")
}
