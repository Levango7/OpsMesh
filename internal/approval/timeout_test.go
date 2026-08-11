package approval

import (
	"sync"
	"testing"
	"time"
)

func TestCheckTimeoutStepExpired(t *testing.T) {
	e, advance := newTestEngine()
	f := newFlow("f1", "t1", TriggerShell, StepAnyOf, "alice")
	f.Steps[0].Timeout = 10 * time.Minute
	_ = e.CreateFlow(f)
	_ = e.Submit(newRequest("r1", "f1", "t1", TriggerShell, "ops"))

	// 未超时。
	fired, err := e.CheckTimeout("r1")
	if err != nil {
		t.Fatalf("CheckTimeout: %v", err)
	}
	if fired {
		t.Error("should not fire before timeout")
	}

	// 推进 11 分钟 → 超时。
	advance(11 * time.Minute)
	fired, err = e.CheckTimeout("r1")
	if err != nil {
		t.Fatalf("CheckTimeout: %v", err)
	}
	if !fired {
		t.Error("should fire after timeout")
	}
	got, _ := e.GetRequest("r1")
	if got.Status != StatusRejected {
		t.Errorf("status = %q want rejected (step timeout auto-reject)", got.Status)
	}
	if got.Steps[0].Status != StatusRejected {
		t.Errorf("step status = %q want rejected", got.Steps[0].Status)
	}

	// 已终态再检查：不触发。
	fired, _ = e.CheckTimeout("r1")
	if fired {
		t.Error("should not fire on terminal request")
	}

	// 历史记录超时事件。
	h, _ := e.GetHistory("r1")
	hasTimeout := false
	for _, entry := range h.Timeline {
		if entry.Action == HistoryTimeout {
			hasTimeout = true
			if entry.StepID != "s1" {
				t.Errorf("timeout entry StepID = %q want s1", entry.StepID)
			}
		}
	}
	if !hasTimeout {
		t.Error("history should contain timeout entry")
	}
}

func TestCheckTimeoutNoTimeoutConfig(t *testing.T) {
	e, advance := newTestEngine()
	f := newFlow("f1", "t1", TriggerShell, StepAnyOf, "alice")
	// Timeout = 0 表示不超时。
	_ = e.CreateFlow(f)
	_ = e.Submit(newRequest("r1", "f1", "t1", TriggerShell, "ops"))

	advance(100 * time.Hour)
	fired, err := e.CheckTimeout("r1")
	if err != nil {
		t.Fatalf("CheckTimeout: %v", err)
	}
	if fired {
		t.Error("Timeout=0 should never fire")
	}
}

func TestCheckTimeoutRequestExpired(t *testing.T) {
	e, advance := newTestEngine()
	f := newFlow("f1", "t1", TriggerShell, StepAnyOf, "alice")
	_ = e.CreateFlow(f)

	req := newRequest("r1", "f1", "t1", TriggerShell, "ops")
	req.ExpireAt = e.nowTime().Add(1 * time.Hour)
	_ = e.Submit(req)

	// 未过期。
	if fired, _ := e.CheckTimeout("r1"); fired {
		t.Error("should not fire before request expiry")
	}

	// 过期。
	advance(2 * time.Hour)
	fired, err := e.CheckTimeout("r1")
	if err != nil {
		t.Fatalf("CheckTimeout: %v", err)
	}
	if !fired {
		t.Error("should fire on request expiry")
	}
	got, _ := e.GetRequest("r1")
	if got.Status != StatusTimeout {
		t.Errorf("status = %q want timeout", got.Status)
	}
}

func TestCheckTimeoutMultiStepAdvanceResetsTimer(t *testing.T) {
	e, advance := newTestEngine()
	f := newMultiStepFlow("f1", "t1", TriggerDeploy, StepSequential, []string{"alice"}, StepAnyOf, []string{"bob"})
	f.Steps[0].Timeout = 10 * time.Minute
	f.Steps[1].Timeout = 10 * time.Minute
	_ = e.CreateFlow(f)
	_ = e.Submit(newRequest("r1", "f1", "t1", TriggerDeploy, "ops"))

	// 步骤1 推进 9 分钟未超时。
	advance(9 * time.Minute)
	if fired, _ := e.CheckTimeout("r1"); fired {
		t.Error("step1 should not fire at 9min")
	}
	// alice 通过，推进到步骤2，步骤2 计时从现在起。
	_ = e.Approve("r1", "alice", "")

	// 再推进 9 分钟（步骤2 总计 9 分钟）。
	advance(9 * time.Minute)
	if fired, _ := e.CheckTimeout("r1"); fired {
		t.Error("step2 should not fire at 9min after advance")
	}
	// 推进到 11 分钟 → 步骤2 超时。
	advance(2 * time.Minute)
	fired, err := e.CheckTimeout("r1")
	if err != nil {
		t.Fatalf("CheckTimeout: %v", err)
	}
	if !fired {
		t.Error("step2 should fire at 11min")
	}
	got, _ := e.GetRequest("r1")
	if got.Status != StatusRejected {
		t.Errorf("status = %q want rejected", got.Status)
	}
}

func TestCheckTimeoutMissingRequest(t *testing.T) {
	e, _ := newTestEngine()
	if _, err := e.CheckTimeout("nope"); err != ErrRequestNotFound {
		t.Errorf("CheckTimeout missing: %v want %v", err, ErrRequestNotFound)
	}
}

func TestCheckTimeoutFlowDeleted(t *testing.T) {
	e, _ := newTestEngine()
	f := newFlow("f1", "t1", TriggerShell, StepAnyOf, "alice")
	f.Steps[0].Timeout = 1 * time.Minute
	_ = e.CreateFlow(f)
	_ = e.Submit(newRequest("r1", "f1", "t1", TriggerShell, "ops"))
	_ = e.DeleteFlow("f1")
	if _, err := e.CheckTimeout("r1"); err != ErrFlowNotFound {
		t.Errorf("CheckTimeout with deleted flow: %v want %v", err, ErrFlowNotFound)
	}
}

func TestCheckTimeoutNotifierFires(t *testing.T) {
	e, advance := newTestEngine()
	var firedAction string
	var mu sync.Mutex
	e.SetNotifier(func(req *ApprovalRequest, action string) {
		mu.Lock()
		defer mu.Unlock()
		firedAction = action
	})
	f := newFlow("f1", "t1", TriggerShell, StepAnyOf, "alice")
	f.Steps[0].Timeout = 1 * time.Minute
	_ = e.CreateFlow(f)
	_ = e.Submit(newRequest("r1", "f1", "t1", TriggerShell, "ops"))
	advance(2 * time.Minute)
	_, _ = e.CheckTimeout("r1")

	mu.Lock()
	defer mu.Unlock()
	if firedAction != HistoryTimeout {
		t.Errorf("notifier action = %q want timeout", firedAction)
	}
}