//go:build linux || darwin

package agent

import (
	"fmt"
	"syscall"
)

// setRlimits 应用 POSIX 进程资源限额（P0-3）。所有选项为 0 时不限制。
// 注意：RLIMIT_AS 设置过低可能导致进程被杀；作为等保/多租户隔离的纵深防御，默认不开启。
func setRlimits(a *Agent) {
	r := &syscall.Rlimit{}
	set := func(res syscall.RlimitType, v int64, name string) {
		if v <= 0 {
			return
		}
		r.Cur = uint64(v)
		r.Max = uint64(v)
		if err := syscall.Setrlimit(res, r); err != nil {
			fmt.Printf("[agent] 设置 %s 失败: %v\n", name, err)
		}
	}
	set(syscall.RLIMIT_NPROC, int64(a.cfg.MaxProcs), "RLIMIT_NPROC")
	set(syscall.RLIMIT_NOFILE, int64(a.cfg.MaxFiles), "RLIMIT_NOFILE")
	set(syscall.RLIMIT_AS, a.cfg.MaxMemoryMB*1024*1024, "RLIMIT_AS")
}
