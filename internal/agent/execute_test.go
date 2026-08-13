package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opsmesh/internal/config"
	"opsmesh/internal/proto"
)

func newTestAgent(timeout time.Duration) *Agent {
	return &Agent{agentID: "agent-test", taskTimeout: timeout, cfg: &config.Config{}}
}

// TestExecute_Shell 验证 shell 任务捕获 stdout/stderr 与退出码。
func TestExecute_Shell(t *testing.T) {
	a := newTestAgent(5 * time.Second)
	res := a.execute(context.Background(), proto.Task{Type: proto.TaskTypeShell, Command: "echo hello"})
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode=%d, stderr=%s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "hello") {
		t.Fatalf("Stdout=%q, want contains hello", res.Stdout)
	}
	if res.DurationMs <= 0 {
		t.Fatalf("DurationMs 应 > 0，得到 %d", res.DurationMs)
	}
}

// TestExecute_File 验证 file 任务原子写入目标路径。
func TestExecute_File(t *testing.T) {
	a := newTestAgent(5 * time.Second)
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	res := a.execute(context.Background(), proto.Task{Type: proto.TaskTypeFile, Path: path, Content: "config-data"})
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode=%d, stderr=%s", res.ExitCode, res.Stderr)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "config-data" {
		t.Fatalf("文件内容=%q, want config-data", string(got))
	}
}

// TestExecute_Unsupported 验证未知类型返回 ExitCode=-1 而非崩溃。
func TestExecute_Unsupported(t *testing.T) {
	a := newTestAgent(5 * time.Second)
	res := a.execute(context.Background(), proto.Task{Type: "bogus"})
	if res.ExitCode != -1 {
		t.Fatalf("ExitCode=%d, want -1", res.ExitCode)
	}
}

// TestExecute_Timeout 验证超长命令被 context 超时打断（P0-3 健壮性）。
// execute 使用传入的 ctx（worker 已绑定 taskTimeout 与取消信号，F3 取消时 ctx 被取消）。
func TestExecute_Timeout(t *testing.T) {
	a := newTestAgent(50 * time.Millisecond) // 故意短超时
	toCtx, toCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer toCancel()
	res := a.execute(toCtx, proto.Task{Type: proto.TaskTypeShell, Command: "sleep 2"})
	if res.ExitCode == 0 {
		t.Fatalf("超时命令应非零退出，得到 ExitCode=0")
	}
	// 平台差异：Linux 上被信号杀死 ExitCode=-1；Windows 上 killed 进程返回 1。
	// 关键断言是“被超时打断而非跑完”，用耗时上界判定更稳（超时设为 50ms）。
	if res.DurationMs > 1000 {
		t.Fatalf("超时命令应在 ~50ms 内被中断，实际耗时 %dms", res.DurationMs)
	}
}

// TestExecute_Service 验证 service 类型拼装 systemctl（命令不存在时仍应返回非 0，不 panic）。
func TestExecute_Service(t *testing.T) {
	a := newTestAgent(5 * time.Second)
	res := a.execute(context.Background(), proto.Task{TaskID: "task-svc-1", Type: proto.TaskTypeService, Command: "status", Path: "nginx"})
	// 测试环境大多无 nginx/systemctl，仅断言不 panic 且产出结构化结果。
	if res.TaskID == "" {
		t.Fatal("result 应携带 TaskID")
	}
}

// TestTaskTimeoutFor 验证 P2-B2 节点级超时（任务 261）：
//   - 任务 Timeout>0 时覆盖全局 taskTimeout；
//   - Timeout=0 时回退全局 taskTimeout（向后兼容）。
func TestTaskTimeoutFor(t *testing.T) {
	global := 30 * time.Second
	cases := []struct {
		name   string
		taskTO int
		want   time.Duration
	}{
		{"task_timeout_overrides_global", 10, 10 * time.Second},
		{"task_zero_falls_back_global", 0, global},
		{"task_large_timeout", 3600, 3600 * time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tk := proto.Task{Timeout: c.taskTO}
			got := taskTimeoutFor(tk, global)
			if got != c.want {
				t.Errorf("taskTimeoutFor(Timeout=%d, global=%v) = %v, want %v",
					c.taskTO, global, got, c.want)
			}
		})
	}
}
