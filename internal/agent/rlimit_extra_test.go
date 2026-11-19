// rlimit_extra_test.go 补充 rlimit_other.go / rlimit_unix.go 中未覆盖的函数单元测试。
//
// 覆盖：
//   - setRlimits 各配置组合（全 0、非 0、负值）
//   - applyRlimits 端到端
package agent

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
)

// rlimitSubprocessEnv 非 0 限额用例的子进程标记。
const rlimitSubprocessEnv = "OPSMESH_TEST_RLIMIT_SUBPROCESS"

// --- setRlimits ---

func TestSetRlimits_AllZero(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("setRlimits panic: %v", r)
		}
	}()
	a := &Agent{cfg: &config.Config{MaxProcs: 0, MaxFiles: 0, MaxMemoryMB: 0}}
	setRlimits(a)
}

func TestSetRlimits_WithLimits(t *testing.T) {
	// RLIMIT 是**进程级且不可逆**的：setRlimits 把 Cur/Max 一并下调，普通进程
	// 之后无法再抬回去。在本进程内真跑 512MB 会把整个 test 二进制锁进 512MB
	// 地址空间——测试跑完后的覆盖率收尾（EmitTextual 需要新 mmap 4MB 堆区）
	// 会直接 fatal error: runtime: out of memory。2026-11-18 CI 的 agent_OZ
	// 批次连续崩在 coverage teardown 即因此（用例全 PASS，死时 RSS ~267MB）。
	// 故非 0 限额改在子进程执行：父进程不受污染，子进程在限额生效后立即退出
	// （不进入 testing 的覆盖率收尾）。
	if os.Getenv(rlimitSubprocessEnv) == "1" {
		a := &Agent{cfg: &config.Config{MaxProcs: 4096, MaxFiles: 1024, MaxMemoryMB: 512}}
		setRlimits(a)
		fmt.Println("SETRLIMITS_OK")
		os.Exit(0)
	}

	// 显式关掉 -test.paniconexit0：子进程要合法地用 os.Exit(0) 跳过收尾。
	cmd := exec.Command(os.Args[0],
		"-test.run=^TestSetRlimits_WithLimits$",
		"-test.paniconexit0=false",
	)
	cmd.Env = append(os.Environ(), rlimitSubprocessEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("子进程执行 setRlimits 失败: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "SETRLIMITS_OK") {
		t.Fatalf("子进程未报告 setRlimits 完成:\n%s", out)
	}
}

func TestSetRlimits_Negative(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("setRlimits panic: %v", r)
		}
	}()
	// 负值应被跳过（v <= 0 分支）
	a := &Agent{cfg: &config.Config{MaxProcs: -1, MaxFiles: -1, MaxMemoryMB: -1}}
	setRlimits(a)
}

// --- applyRlimits 端到端 ---

func TestApplyRlimits_WithConfig(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("applyRlimits panic: %v", r)
		}
	}()
	a := &Agent{cfg: &config.Config{MaxProcs: 0, MaxFiles: 0, MaxMemoryMB: 0}}
	a.applyRlimits()
}
