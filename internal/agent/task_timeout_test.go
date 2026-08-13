package agent

import (
	"testing"
	"time"

	"opsmesh/internal/proto"
)

// TestTaskTimeoutFor_TaskLevel 测试任务自带 Timeout>0 时覆盖全局超时。
func TestTaskTimeoutFor_TaskLevel(t *testing.T) {
	global := 30 * time.Second
	for _, tc := range []struct {
		name    string
		timeout int
		want    time.Duration
	}{
		{"timeout=10", 10, 10 * time.Second},
		{"timeout=60", 60, 60 * time.Second},
		{"timeout=1", 1, 1 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := proto.Task{Timeout: tc.timeout}
			got := taskTimeoutFor(task, global)
			if got != tc.want {
				t.Fatalf("taskTimeoutFor(Timeout=%d, global=%v) = %v, want %v", tc.timeout, global, got, tc.want)
			}
		})
	}
}

// TestTaskTimeoutFor_GlobalFallback 测试 Timeout=0 时回退全局超时。
func TestTaskTimeoutFor_GlobalFallback(t *testing.T) {
	global := 45 * time.Second
	task := proto.Task{Timeout: 0}
	got := taskTimeoutFor(task, global)
	if got != global {
		t.Fatalf("taskTimeoutFor(Timeout=0, global=%v) = %v, want %v", global, got, global)
	}
}

// TestTaskTimeoutFor_NegativeTimeout 测试 Timeout<0 时也回退全局超时（防御性）。
func TestTaskTimeoutFor_NegativeTimeout(t *testing.T) {
	global := 30 * time.Second
	task := proto.Task{Timeout: -1}
	got := taskTimeoutFor(task, global)
	if got != global {
		t.Fatalf("taskTimeoutFor(Timeout=-1, global=%v) = %v, want %v", global, got, global)
	}
}
