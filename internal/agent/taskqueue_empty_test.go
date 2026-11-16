package agent

import (
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// taskqueue_empty_test.go —— 「空队列 = 轮询常态，不是错误」的回归钉（2026-11-16）。
//
// 缺陷原型（demo 栈实测）：agent 每 2s ClaimTask 一次，无任务时 task-svc 回
// grpc NotFound；drainTasks 把**任何** err 都按 logx.Error 记 ⇒ 单 agent 即
// 每 2s 一条「领取任务失败 / task not found」，一天 4 万+ 行假错误，
// 真错误被淹没、告警疲劳（见 PR fix/agent-claim-notfound-noise）。
//
// 判据：NotFound（含经包装的 NotFound）= 空队列常态 → true；
// Unavailable / Internal / 普通 error → false（这些必须继续按 Error 记）。

func TestIsTaskQueueEmpty(t *testing.T) {
	notFound := status.Errorf(codes.NotFound, "task not found")

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"NotFound 直接回", notFound, true},
		{"NotFound 经 fmt 包装", fmt.Errorf("领取: %w", notFound), true},
		{"Unavailable（服务不可用，必须报）", status.Errorf(codes.Unavailable, "conn refused"), false},
		{"Internal（服务端故障，必须报）", status.Errorf(codes.Internal, "boom"), false},
		{"Canceled（ctx 取消）", status.Errorf(codes.Canceled, "ctx canceled"), false},
		{"普通 error（非 gRPC 状态）", errors.New("dial tcp: refused"), false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		if got := isTaskQueueEmpty(c.err); got != c.want {
			t.Errorf("%s: isTaskQueueEmpty = %v, want %v (err=%v)", c.name, got, c.want, c.err)
		}
	}
}
