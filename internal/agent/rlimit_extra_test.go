// rlimit_extra_test.go 补充 rlimit_other.go / rlimit_unix.go 中未覆盖的函数单元测试。
//
// 覆盖：
//   - setRlimits 各配置组合（全 0、非 0）
//   - applyRlimits 端到端
package agent

import (
	"testing"

	"opsmesh/internal/config"
)

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
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("setRlimits panic: %v", r)
		}
	}()
	// 设置较小的限额（避免影响测试进程本身）
	a := &Agent{cfg: &config.Config{MaxProcs: 4096, MaxFiles: 1024, MaxMemoryMB: 512}}
	setRlimits(a)
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