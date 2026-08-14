package domain

import (
	"errors"
	"testing"
	"time"
)

// =============================================================================
// M2-1C DDD 实质化：领域实体业务行为状态机测试
//
// 覆盖 Task/Device/Alert 三类实体的全部业务方法：
//   - Task:   Cancel / CanRetry / IsLeaseExpired / MarkDead
//   - Device: CanRetire / TransitionToProvisioning / IsOrphan
//   - Alert:  Acknowledge / Silence / IsExpired
//
// 测试核心：状态机转换的合法/非法路径、幂等性、边界值（空串兼容旧数据、零值防御）。
// =============================================================================

// ---------------------------------------------------------------------------
// Task.Cancel
// ---------------------------------------------------------------------------

func TestTask_Cancel(t *testing.T) {
	cases := []struct {
		name    string
		status  string
		wantErr error  // nil 表示期望成功；非 nil 表示期望该 sentinel error
		want    string // 期望转换后的状态（wantErr==nil 时校验）
	}{
		{"pending→cancelled", TaskStatusPending, nil, TaskStatusCancelled},
		{"running→cancelled", TaskStatusRunning, nil, TaskStatusCancelled},
		{"空串→cancelled（兼容旧数据）", "", nil, TaskStatusCancelled},
		{"done→error", TaskStatusDone, ErrTaskAlreadyDone, ""},
		{"failed→error", TaskStatusFailed, ErrTaskAlreadyFailed, ""},
		{"cancelled→error(幂等拒绝)", TaskStatusCancelled, ErrTaskAlreadyCancelled, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tk := &Task{Status: c.status}
			err := tk.Cancel()
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("Cancel() err = %v, want nil", err)
				}
				if tk.Status != c.want {
					t.Fatalf("status = %q, want %q", tk.Status, c.want)
				}
			} else {
				if !errors.Is(err, c.wantErr) {
					t.Fatalf("Cancel() err = %v, want %v", err, c.wantErr)
				}
			}
		})
	}
}

func TestTask_Cancel_UnknownStatus(t *testing.T) {
	tk := &Task{Status: "weird"}
	if err := tk.Cancel(); err == nil {
		t.Fatal("unknown status should error")
	}
}

// ---------------------------------------------------------------------------
// Task.CanRetry
// ---------------------------------------------------------------------------

func TestTask_CanRetry(t *testing.T) {
	cases := []struct {
		name       string
		status     string
		retryCount int
		maxRetries int
		want       bool
	}{
		{"failed 且未达上限", TaskStatusFailed, 1, 3, true},
		{"failed 且刚好达上限", TaskStatusFailed, 3, 3, false},
		{"failed 且超过上限", TaskStatusFailed, 5, 3, false},
		{"pending 不可重试", TaskStatusPending, 0, 3, false},
		{"running 不可重试", TaskStatusRunning, 0, 3, false},
		{"done 不可重试", TaskStatusDone, 0, 3, false},
		{"maxRetries<=0 不允许重试", TaskStatusFailed, 0, 0, false},
		{"maxRetries<0 不允许重试", TaskStatusFailed, 0, -1, false},
		{"retryCount=0 且 maxRetries=1 可重试", TaskStatusFailed, 0, 1, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tk := &Task{Status: c.status, RetryCount: c.retryCount}
			if got := tk.CanRetry(c.maxRetries); got != c.want {
				t.Fatalf("CanRetry(%d) = %v, want %v", c.maxRetries, got, c.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Task.IsLeaseExpired
// ---------------------------------------------------------------------------

func TestTask_IsLeaseExpired(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name    string
		status  string
		claimed time.Time
		maxAge  time.Duration
		want    bool
	}{
		{"running 且超时", TaskStatusRunning, now.Add(-2 * time.Minute), time.Minute, true},
		{"running 且未超时", TaskStatusRunning, now.Add(-30 * time.Second), time.Minute, false},
		{"running 且刚好超时（边界）", TaskStatusRunning, now.Add(-time.Minute - time.Millisecond), time.Minute, true},
		{"pending 无租约", TaskStatusPending, now, time.Minute, false},
		{"done 无租约", TaskStatusDone, now, time.Minute, false},
		{"running 但 ClaimedAt 零值", TaskStatusRunning, time.Time{}, time.Minute, false},
		{"maxAge<=0 永不超时", TaskStatusRunning, now.Add(-time.Hour), 0, false},
		{"maxAge<0 永不超时", TaskStatusRunning, now.Add(-time.Hour), -time.Minute, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tk := &Task{Status: c.status, ClaimedAt: c.claimed}
			if got := tk.IsLeaseExpired(c.maxAge); got != c.want {
				t.Fatalf("IsLeaseExpired(%v) = %v, want %v", c.maxAge, got, c.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Task.MarkDead
// ---------------------------------------------------------------------------

func TestTask_MarkDead(t *testing.T) {
	tk := &Task{Status: TaskStatusFailed, RetryCount: 3}
	tk.MarkDead()
	if !tk.DeadLetter {
		t.Fatal("DeadLetter should be true after MarkDead")
	}
	if tk.Status != TaskStatusFailed {
		t.Fatalf("status = %q, want failed", tk.Status)
	}
	// 幂等：重复调用无副作用
	tk.MarkDead()
	if !tk.DeadLetter || tk.Status != TaskStatusFailed {
		t.Fatal("MarkDead should be idempotent")
	}
	// 从 running 标记死信也应翻为 failed
	tk2 := &Task{Status: TaskStatusRunning}
	tk2.MarkDead()
	if !tk2.DeadLetter || tk2.Status != TaskStatusFailed {
		t.Fatalf("MarkDead from running: DeadLetter=%v status=%q, want true/failed", tk2.DeadLetter, tk2.Status)
	}
}

// ---------------------------------------------------------------------------
// Device.CanRetire
// ---------------------------------------------------------------------------

func TestDevice_CanRetire(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name       string
		state      string
		retired    bool
		lastResult time.Time
		maxAge     time.Duration
		want       bool
	}{
		{"未退役+离线", DeviceStateOffline, false, time.Time{}, 0, true},
		{"未退役+在线+未超龄", DeviceStateOnline, false, now, time.Hour, false},
		{"未退役+在线+超龄", DeviceStateOnline, false, now.Add(-2 * time.Hour), time.Hour, true},
		{"已退役→false(幂等拒绝)", DeviceStateOffline, true, time.Time{}, 0, false},
		{"已退役+在线→false", DeviceStateOnline, true, now, time.Hour, false},
		{"未退役+在线+LastResultAt零→false", DeviceStateOnline, false, time.Time{}, time.Hour, false},
		{"未退役+在线+maxAge<=0→false(仅离线可退役)", DeviceStateOnline, false, now, 0, false},
		{"未退役+discovered+超龄→true", DeviceStateDiscovered, false, now.Add(-2 * time.Hour), time.Hour, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := &Device{State: c.state, Retired: c.retired, LastResultAt: c.lastResult}
			if got := d.CanRetire(c.maxAge); got != c.want {
				t.Fatalf("CanRetire(%v) = %v, want %v", c.maxAge, got, c.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Device.TransitionToProvisioning
// ---------------------------------------------------------------------------

func TestDevice_TransitionToProvisioning(t *testing.T) {
	cases := []struct {
		name    string
		state   string
		wantErr error
		want    string
	}{
		{"discovered→provisioning", DeviceStateDiscovered, nil, DeviceStateProvisioning},
		{"空串→provisioning（兼容旧数据）", "", nil, DeviceStateProvisioning},
		{"provisioning→error(幂等拒绝)", DeviceStateProvisioning, ErrDeviceAlreadyProvisioning, ""},
		{"online→error(已纳管)", DeviceStateOnline, nil, ""}, // wantErr 用 nil 占位，下面单独判
		{"offline→error(已纳管)", DeviceStateOffline, nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := &Device{State: c.state}
			err := d.TransitionToProvisioning()
			switch c.state {
			case DeviceStateDiscovered, "":
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				if d.State != DeviceStateProvisioning {
					t.Fatalf("state = %q, want provisioning", d.State)
				}
			case DeviceStateProvisioning:
				if !errors.Is(err, ErrDeviceAlreadyProvisioning) {
					t.Fatalf("err = %v, want %v", err, ErrDeviceAlreadyProvisioning)
				}
			case DeviceStateOnline, DeviceStateOffline:
				if err == nil {
					t.Fatalf("managed device should error, got nil")
				}
			}
			_ = c.wantErr
			_ = c.want
		})
	}
}

func TestDevice_TransitionToProvisioning_UnknownState(t *testing.T) {
	d := &Device{State: "weird"}
	if err := d.TransitionToProvisioning(); err == nil {
		t.Fatal("unknown state should error")
	}
}

// ---------------------------------------------------------------------------
// Device.IsOrphan
// ---------------------------------------------------------------------------

func TestDevice_IsOrphan(t *testing.T) {
	cases := []struct {
		name    string
		managed bool
		agentID string
		want    bool
	}{
		{"未纳管+无agent=孤儿", false, "", true},
		{"未纳管+有agent=非孤儿", false, "a1", false},
		{"已纳管+无agent=非孤儿", true, "", false},
		{"已纳管+有agent=非孤儿", true, "a1", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := &Device{Managed: c.managed, AgentID: c.agentID}
			if got := d.IsOrphan(); got != c.want {
				t.Fatalf("IsOrphan() = %v, want %v", got, c.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Alert.Acknowledge
// ---------------------------------------------------------------------------

func TestAlert_Acknowledge(t *testing.T) {
	cases := []struct {
		name    string
		status  string
		wantErr error
	}{
		{"firing→acknowledged", AlertStatusFiring, nil},
		{"空串→acknowledged（兼容旧数据）", "", nil},
		{"acknowledged→error(幂等拒绝)", AlertStatusAcknowledged, ErrAlertAlreadyAcknowledged},
		{"silenced→error", AlertStatusSilenced, ErrAlertSilenced},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := &Alert{Status: c.status}
			err := a.Acknowledge("u1")
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				if a.Status != AlertStatusAcknowledged {
					t.Fatalf("status = %q, want acknowledged", a.Status)
				}
				if a.AcknowledgedBy != "u1" {
					t.Fatalf("acknowledgedBy = %q, want u1", a.AcknowledgedBy)
				}
				if a.UpdatedAt.IsZero() {
					t.Fatal("updatedAt should be set")
				}
			} else {
				if !errors.Is(err, c.wantErr) {
					t.Fatalf("err = %v, want %v", err, c.wantErr)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Alert.Silence
// ---------------------------------------------------------------------------

func TestAlert_Silence(t *testing.T) {
	until := time.Now().Add(time.Hour)
	cases := []struct {
		name    string
		status  string
		wantErr error
	}{
		{"firing→silenced", AlertStatusFiring, nil},
		{"空串→silenced（兼容旧数据）", "", nil},
		{"acknowledged→silenced", AlertStatusAcknowledged, nil},
		{"silenced→error(幂等拒绝)", AlertStatusSilenced, ErrAlertAlreadySilenced},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := &Alert{Status: c.status}
			err := a.Silence(until, "investigating")
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				if a.Status != AlertStatusSilenced {
					t.Fatalf("status = %q, want silenced", a.Status)
				}
				if !a.SilencedUntil.Equal(until) {
					t.Fatalf("silencedUntil = %v, want %v", a.SilencedUntil, until)
				}
				if a.Comment != "investigating" {
					t.Fatalf("comment = %q, want investigating", a.Comment)
				}
				if a.UpdatedAt.IsZero() {
					t.Fatal("updatedAt should be set")
				}
			} else {
				if !errors.Is(err, c.wantErr) {
					t.Fatalf("err = %v, want %v", err, c.wantErr)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Alert.IsExpired
// ---------------------------------------------------------------------------

func TestAlert_IsExpired(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name          string
		status        string
		silencedUntil time.Time
		want          bool
	}{
		{"silenced+已过期", AlertStatusSilenced, now.Add(-time.Minute), true},
		{"silenced+未过期", AlertStatusSilenced, now.Add(time.Hour), false},
		{"silenced+刚好过期(边界)", AlertStatusSilenced, now.Add(-time.Millisecond), true},
		{"非silenced→false", AlertStatusFiring, now.Add(-time.Hour), false},
		{"acknowledged→false", AlertStatusAcknowledged, now.Add(-time.Hour), false},
		{"silenced+SilencedUntil零→false", AlertStatusSilenced, time.Time{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := &Alert{Status: c.status, SilencedUntil: c.silencedUntil}
			if got := a.IsExpired(); got != c.want {
				t.Fatalf("IsExpired() = %v, want %v", got, c.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 状态机组合场景：Acknowledge 后 Silence 应成功（acknowledged 可静默）
// ---------------------------------------------------------------------------

func TestAlert_AckThenSilence(t *testing.T) {
	a := &Alert{Status: AlertStatusFiring}
	if err := a.Acknowledge("u1"); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	if err := a.Silence(time.Now().Add(time.Hour), "ack then silence"); err != nil {
		t.Fatalf("Silence after Ack: %v", err)
	}
	if a.Status != AlertStatusSilenced {
		t.Fatalf("status = %q, want silenced", a.Status)
	}
	// 已 silenced 不可再 Ack
	if err := a.Acknowledge("u2"); !errors.Is(err, ErrAlertSilenced) {
		t.Fatalf("Ack after Silence err = %v, want ErrAlertSilenced", err)
	}
}
