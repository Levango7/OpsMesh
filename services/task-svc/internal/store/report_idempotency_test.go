package store

import (
	"testing"

	"github.com/Levango7/OpsMesh/services/task-svc/internal/models"
)

func newRetryTask(id string, maxRetries int) *models.Task {
	return &models.Task{
		TaskID: id, AgentID: "agent-1", TenantID: "default",
		Type: models.TaskTypeShell, Command: "false", MaxRetries: maxRetries,
	}
}

// TestReportResultDuplicateFailureDoesNotDoubleCountRetry 钉幂等缺口：
// 同一 claim_epoch 的重复失败上报（at-least-once 重试上报）不得重复计重试、不得提前死信。
func TestReportResultDuplicateFailureDoesNotDoubleCountRetry(t *testing.T) {
	m := NewMemoryStore()
	if _, err := m.CreateTask(newRetryTask("t-dup", 2)); err != nil {
		t.Fatal(err)
	}
	claimed := m.ClaimTask("agent-1")
	if claimed == nil {
		t.Fatal("应能领到任务")
	}
	epoch := claimed.ClaimEpoch
	// 同一 claim 的重复失败上报。
	for i := 0; i < 2; i++ {
		if err := m.ReportResult(&models.TaskResult{TaskID: "t-dup", ExitCode: 1, ClaimEpoch: epoch}); err != nil {
			t.Fatal(err)
		}
	}
	got := m.GetTask("t-dup")
	if got.RetryCount != 1 {
		t.Fatalf("同一 claim 的重复失败上报应只计一次重试，got retryCount=%d", got.RetryCount)
	}
	if got.DeadLetter {
		t.Fatal("一次真失败 + 一次重复上报不应进入死信")
	}
}

// TestReportResultDistinctClaimsIncrementRetry 防过度去重：
// 不同 claim（不同 epoch）的失败应各自计一次，达到 maxRetries 进死信。
func TestReportResultDistinctClaimsIncrementRetry(t *testing.T) {
	m := NewMemoryStore()
	if _, err := m.CreateTask(newRetryTask("t-two", 2)); err != nil {
		t.Fatal(err)
	}
	c1 := m.ClaimTask("agent-1")
	if c1 == nil {
		t.Fatal("第一次应领到")
	}
	if err := m.ReportResult(&models.TaskResult{TaskID: "t-two", ExitCode: 1, ClaimEpoch: c1.ClaimEpoch}); err != nil {
		t.Fatal(err)
	}
	c2 := m.ClaimTask("agent-1")
	if c2 == nil {
		t.Fatal("失败后应能重新领到")
	}
	if err := m.ReportResult(&models.TaskResult{TaskID: "t-two", ExitCode: 1, ClaimEpoch: c2.ClaimEpoch}); err != nil {
		t.Fatal(err)
	}
	got := m.GetTask("t-two")
	if got.RetryCount != 2 {
		t.Fatalf("两次不同 claim 的失败应各计一次，got retryCount=%d", got.RetryCount)
	}
	if !got.DeadLetter {
		t.Fatal("达到 maxRetries 应进入死信")
	}
}
