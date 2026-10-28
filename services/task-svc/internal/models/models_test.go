package models

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"
)

// TestStatusConstants 钉住任务/批次状态与类型的字面值。
// 这些字符串同时落在 MySQL 行、gRPC/HTTP 响应和影子模式对照里，
// 改字面值会让存量数据"看起来是未知状态"，故按值断言而不是只比常量本身。
func TestStatusConstants(t *testing.T) {
	taskStatuses := map[string]string{
		"TaskStatusPending":         TaskStatusPending,
		"TaskStatusClaimed":         TaskStatusClaimed,
		"TaskStatusRunning":         TaskStatusRunning,
		"TaskStatusDone":            TaskStatusDone,
		"TaskStatusFailed":          TaskStatusFailed,
		"TaskStatusCancelled":       TaskStatusCancelled,
		"TaskStatusPendingApproval": TaskStatusPendingApproval,
		"TaskStatusRejected":        TaskStatusRejected,
	}
	wantTask := map[string]string{
		"TaskStatusPending":         "pending",
		"TaskStatusClaimed":         "claimed",
		"TaskStatusRunning":         "running",
		"TaskStatusDone":            "done",
		"TaskStatusFailed":          "failed",
		"TaskStatusCancelled":       "cancelled",
		"TaskStatusPendingApproval": "pending_approval",
		"TaskStatusRejected":        "rejected",
	}
	for name, got := range taskStatuses {
		if got != wantTask[name] {
			t.Errorf("%s = %q，期望 %q", name, got, wantTask[name])
		}
	}

	taskTypes := map[string]string{
		"TaskTypeShell":   TaskTypeShell,
		"TaskTypeService": TaskTypeService,
		"TaskTypeFile":    TaskTypeFile,
	}
	wantTypes := map[string]string{
		"TaskTypeShell":   "shell",
		"TaskTypeService": "service",
		"TaskTypeFile":    "file",
	}
	for name, got := range taskTypes {
		if got != wantTypes[name] {
			t.Errorf("%s = %q，期望 %q", name, got, wantTypes[name])
		}
	}

	batchStatuses := map[string]string{
		"BatchStatusPending": BatchStatusPending,
		"BatchStatusRunning": BatchStatusRunning,
		"BatchStatusDone":    BatchStatusDone,
		"BatchStatusFailed":  BatchStatusFailed,
	}
	wantBatch := map[string]string{
		"BatchStatusPending": "pending",
		"BatchStatusRunning": "running",
		"BatchStatusDone":    "done",
		"BatchStatusFailed":  "failed",
	}
	for name, got := range batchStatuses {
		if got != wantBatch[name] {
			t.Errorf("%s = %q，期望 %q", name, got, wantBatch[name])
		}
	}
}

const canaryJSON = `{"canaryID":"c-1","tenantID":"t1","taskType":"shell","command":"uptime",` +
	`"strategy":"percentage","percentage":20,"groups":["g1","g2"],"labels":{"env":"prod"},` +
	`"createdAt":"2026-01-02T03:04:05Z","createdBy":"alice","phases":[` +
	`{"phase":1,"deviceIDs":["d1"],"status":"done","tasks":[{"deviceID":"d1","taskID":"tk-1","status":"done"}],` +
	`"startedAt":"2026-01-02T03:05:00Z","finishedAt":"2026-01-02T03:06:00Z"},` +
	`{"phase":2,"deviceIDs":["d2"],"status":"pending","tasks":[{"deviceID":"d2","taskID":"tk-2","status":"failed","error":"timeout"}],` +
	`"startedAt":"0001-01-01T00:00:00Z","finishedAt":"0001-01-01T00:00:00Z"}]}`

func sampleCanary() *CanaryRelease {
	return &CanaryRelease{
		CanaryID:   "c-1",
		TenantID:   "t1",
		TaskType:   TaskTypeShell,
		Command:    "uptime",
		Strategy:   "percentage",
		Percentage: 20,
		Groups:     []string{"g1", "g2"},
		Labels:     map[string]string{"env": "prod"},
		CreatedAt:  time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		CreatedBy:  "alice",
		Phases: []CanaryPhase{
			{
				Phase: 1, DeviceIDs: []string{"d1"}, Status: "done",
				Tasks:      []BatchTaskItem{{DeviceID: "d1", TaskID: "tk-1", Status: "done"}},
				StartedAt:  time.Date(2026, 1, 2, 3, 5, 0, 0, time.UTC),
				FinishedAt: time.Date(2026, 1, 2, 3, 6, 0, 0, time.UTC),
			},
			{
				Phase: 2, DeviceIDs: []string{"d2"}, Status: "pending",
				Tasks: []BatchTaskItem{{DeviceID: "d2", TaskID: "tk-2", Status: "failed", Error: "timeout"}},
			},
		},
	}
}

// TestCanaryRelease_JSONPayloadContract 用固定载荷逐字节钉住持久化格式：
// 注释声明这些 tag 是 canary_releases.payload 的字段契约，任何改名/去字段
// 都会让存量行解析成"记录不可用"，因此这里按 golden 串断言（结构体无 map 值，格式确定）。
func TestCanaryRelease_JSONPayloadContract(t *testing.T) {
	got, err := json.Marshal(sampleCanary())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(got) != canaryJSON {
		t.Errorf("payload 格式漂移：\n got=%s\nwant=%s", got, canaryJSON)
	}
}

func TestCanaryRelease_JSONRoundTrip(t *testing.T) {
	orig := sampleCanary()
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var back CanaryRelease
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(&back, orig) {
		t.Errorf("往返后不一致：\n got=%+v\nwant=%+v", back, *orig)
	}

	// 时间字段必须保持 RFC3339 且时区不丢。
	if !back.CreatedAt.Equal(orig.CreatedAt) || back.CreatedAt.Location() != time.UTC {
		t.Errorf("CreatedAt 往返失真：%v", back.CreatedAt)
	}
	if len(back.Phases) != 2 || !back.Phases[0].StartedAt.Equal(orig.Phases[0].StartedAt) {
		t.Errorf("嵌套阶段/时间往返失真：%+v", back.Phases)
	}
	if back.Phases[1].Tasks[0].Error != "timeout" {
		t.Errorf("任务级 error 未保留：%+v", back.Phases[1].Tasks[0])
	}
}

// TestCanaryRelease_OmitEmpty 钉住省略规则：Groups/Labels 为空时不写键，
// 避免"空数组 vs 缺字段"两种载荷并存（下游按缺键判空）。
func TestCanaryRelease_OmitEmpty(t *testing.T) {
	c := &CanaryRelease{CanaryID: "c-2"}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		t.Fatalf("Unmarshal keys: %v", err)
	}
	for _, absent := range []string{"groups", "labels"} {
		if _, ok := keys[absent]; ok {
			t.Errorf("空值字段 %q 不应出现在载荷里：%s", absent, data)
		}
	}
	for _, present := range []string{"canaryID", "tenantID", "taskType", "command", "strategy", "percentage", "createdAt", "createdBy", "phases"} {
		if _, ok := keys[present]; !ok {
			t.Errorf("载荷缺少必需键 %q：%s", present, data)
		}
	}
}

func TestCanaryRelease_TopLevelKeys(t *testing.T) {
	data, err := json.Marshal(sampleCanary())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		t.Fatalf("Unmarshal keys: %v", err)
	}

	var got []string
	for k := range keys {
		got = append(got, k)
	}
	sort.Strings(got)
	want := []string{
		"canaryID", "command", "createdAt", "createdBy", "groups", "labels",
		"percentage", "phases", "strategy", "taskType", "tenantID",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("顶层键集合 = %v，期望 %v", got, want)
	}
}

// TestCanaryRelease_LegacyPayload 钉住向前兼容：旧行只有部分字段时不得报错，
// 缺失字段取零值（新增字段不能变成"必需"）。
func TestCanaryRelease_LegacyPayload(t *testing.T) {
	legacy := `{"canaryID":"c-old","tenantID":"t1","strategy":"group","groups":["g"],"phases":[]}`
	var c CanaryRelease
	if err := json.Unmarshal([]byte(legacy), &c); err != nil {
		t.Fatalf("旧载荷应可解析：%v", err)
	}
	if c.CanaryID != "c-old" || c.Strategy != "group" || len(c.Groups) != 1 {
		t.Errorf("旧载荷字段丢失：%+v", c)
	}
	if c.Percentage != 0 || c.Command != "" || c.CreatedBy != "" || len(c.Phases) != 0 {
		t.Errorf("缺失字段应为零值：%+v", c)
	}
}

func TestBatchTaskItem_ErrorOmitEmpty(t *testing.T) {
	without, err := json.Marshal(BatchTaskItem{DeviceID: "d1", TaskID: "t1", Status: "done"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(without) != `{"deviceID":"d1","taskID":"t1","status":"done"}` {
		t.Errorf("无 error 时不应写该键：%s", without)
	}

	with, err := json.Marshal(BatchTaskItem{DeviceID: "d1", TaskID: "t1", Status: "failed", Error: "x"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(with) != `{"deviceID":"d1","taskID":"t1","status":"failed","error":"x"}` {
		t.Errorf("error 字段格式不对：%s", with)
	}
}

func TestCanaryPhase_Keys(t *testing.T) {
	data, err := json.Marshal(CanaryPhase{Phase: 1})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		t.Fatalf("Unmarshal keys: %v", err)
	}
	for _, want := range []string{"phase", "deviceIDs", "status", "tasks", "startedAt", "finishedAt"} {
		if _, ok := keys[want]; !ok {
			t.Errorf("阶段载荷缺少键 %q：%s", want, data)
		}
	}
}
