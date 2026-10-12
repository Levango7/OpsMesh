package controlplane

// batch_records_persistence_test.go — TD-60 task 域 R6：批量/灰度记录**重启后仍可查**的守卫。
//
// 为什么需要它：这两份状态原本只存进程内存（batchStore），重启即丢——任务实例还在库里，
// 但用户手里的 batchID 直接变 404。R6 的验收就是这一条：**换一个 Server 实例（= 重启）、
// 共用同一个 store**，批次/灰度仍能按 ID 查回来。
//
// 判据刻意分三层（缺一层就会放过不同形态的坏法）：
//  1. 重启后能查到（正向）；
//  2. 记录里的**内容**没丢（任务项数量与 device/taskID 都在——只存壳子也能"查到"）；
//  3. 没建过的 ID 仍是 404（防止回落路径把"查不到"变成"随便给个空壳"，那是假成功）。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store/memory"
	"github.com/Levango7/OpsMesh/internal/store/model"

	"github.com/Levango7/OpsMesh/internal/controlplane/cutover"
)

// seedAgents 注册 n 个 agent：**agentID 必须等于 batch-exec 传入的 deviceIDs 元素**
// （handleBatchExec 走 store.Agent(devID) 查 agent；查不到就落 "agent not found or tenant mismatch"，
// 任务项拿不到 TaskID——那样"内容保留"的断言会退化成空断言）。
func seedAgents(st *memory.MemoryStore, tenantID string, n int) {
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("dev-bench-%d", i)
		st.Register(&proto.AgentInfo{
			AgentID:  id,
			Hostname: fmt.Sprintf("host-%d", i),
			TenantID: tenantID,
			Status:   "online",
			Segment:  "seg-a",
		})
	}
}

// r6Server 造一个与生产同形的 Server：带 cutoverRouter（空名册）与**共享 store**。
// 第二个参数即"重启"——同一个 store 交给新 Server。
func r6Server(t *testing.T, st *memory.MemoryStore) *Server {
	t.Helper()
	s := &Server{store: st, requireAuth: false, cfg: &config.Config{TaskMaxRetries: 3, Demo: true}}
	s.batches = newBatchStore()
	r, err := cutover.LoadRoster("", "")
	if err != nil {
		t.Fatalf("空名册应合法: %v", err)
	}
	s.cutoverRouter = cutover.NewRouter(r)
	return s
}

func r6Post(t *testing.T, s *Server, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-User-Id", "u-admin")
	rec := httptest.NewRecorder()
	switch path {
	case "/api/v1/tasks/batch-exec":
		s.handleBatchExec(rec, req)
	case "/api/v1/tasks/canary":
		s.handleCanaryCreate(rec, req)
	default:
		t.Fatalf("未知路径 %s", path)
	}
	return rec
}

func r6GetBatchStatus(t *testing.T, s *Server, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/batch/"+id, nil)
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-User-Id", "u-admin")
	rec := httptest.NewRecorder()
	s.handleBatchStatus(rec, req, id)
	return rec
}

func r6GetCanaryStatus(t *testing.T, s *Server, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/canary/"+id, nil)
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-User-Id", "u-admin")
	rec := httptest.NewRecorder()
	s.handleCanaryStatus(rec, req, id)
	return rec
}

// TestBatchRecordSurvivesRestart 批次记录：建 → 换 Server（重启）→ 仍按 batchID 查得到，且内容完整。
func TestBatchRecordSurvivesRestart(t *testing.T) {
	st := memory.NewMemoryStore()
	seedDevices(st, 2)
	seedAgents(st, "t1", 2)
	s1 := r6Server(t, st)

	rec := r6Post(t, s1, "/api/v1/tasks/batch-exec",
		`{"deviceIDs":["dev-bench-0","dev-bench-1"],"type":"shell","command":"echo r6"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("建批次应 201，实得 %d：%s", rec.Code, rec.Body.String())
	}
	var created struct {
		BatchID string `json:"batchID"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.BatchID == "" {
		t.Fatalf("取 batchID 失败: %v / body=%s", err, rec.Body.String())
	}

	// "重启"：同一 store，新 Server（内存索引为空）
	// 重启前的基线响应
	before := r6GetBatchStatus(t, s1, created.BatchID)
	if before.Code != http.StatusOK {
		t.Fatalf("重启前基线查询应 200，实得 %d：%s", before.Code, before.Body.String())
	}
	// 先确认基线里真的建出了任务（否则后面的"内容保留"是空断言）
	var baseline map[string]any
	if err := json.Unmarshal(before.Body.Bytes(), &baseline); err != nil {
		t.Fatalf("基线响应解析失败: %v", err)
	}
	tasksAny, _ := baseline["tasks"].([]any)
	if len(tasksAny) != 2 {
		t.Fatalf("基线任务项应为 2 条，实得 %d：%s", len(tasksAny), before.Body.String())
	}
	if it, ok := tasksAny[0].(map[string]any); ok && fmt.Sprint(it["TaskID"]) == "" {
		t.Fatalf("基线任务项不含 TaskID（设备没建成任务，后续断言会失去意义）：%s", before.Body.String())
	}

	// "重启"：同一 store，新 Server（内存索引为空）
	s2 := r6Server(t, st)
	out := r6GetBatchStatus(t, s2, created.BatchID)
	if out.Code != http.StatusOK {
		t.Fatalf("重启后应按 ID 查到批次，实得 %d：%s", out.Code, out.Body.String())
	}
	// 判据用"逐字相同"而不是逐字段比对：不绑 JSON 键名（键名属契约面、另有对账），
	// 且能一并抓住"少字段/多字段"这类只存壳子的坏法。
	if out.Body.String() != before.Body.String() {
		t.Errorf("重启前后同一 batchID 的响应应逐字相同（记录内容未完整持久化）：重启前=%s 重启后=%s",
			before.Body.String(), out.Body.String())
	}
}

// TestCanaryRecordSurvivesRestart 灰度记录：同上，且阶段结构完整。
func TestCanaryRecordSurvivesRestart(t *testing.T) {
	st := memory.NewMemoryStore()
	seedDevices(st, 2)
	seedAgents(st, "t1", 2)
	s1 := r6Server(t, st)

	rec := r6Post(t, s1, "/api/v1/tasks/canary",
		`{"deviceIDs":["dev-bench-0","dev-bench-1"],"taskType":"shell","command":"echo r6","strategy":"percentage","percentage":50}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("建灰度应 201，实得 %d：%s", rec.Code, rec.Body.String())
	}
	var created struct {
		CanaryID string `json:"canaryID"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.CanaryID == "" {
		t.Fatalf("取 canaryID 失败: %v / body=%s", err, rec.Body.String())
	}

	s2 := r6Server(t, st)
	out := r6GetCanaryStatus(t, s2, created.CanaryID)
	if out.Code != http.StatusOK {
		t.Fatalf("重启后应按 ID 查到灰度，实得 %d：%s", out.Code, out.Body.String())
	}
	var got struct {
		CanaryID string `json:"canaryID"`
		Phases   []struct {
			Phase     int      `json:"phase"`
			DeviceIDs []string `json:"deviceIDs"`
		} `json:"phases"`
	}
	if err := json.Unmarshal(out.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if got.CanaryID != created.CanaryID {
		t.Errorf("灰度 ID 不一致：%s vs %s", got.CanaryID, created.CanaryID)
	}
	if len(got.Phases) == 0 {
		t.Error("阶段结构丢失（重启后 phases 为空）")
	}
	if len(got.Phases) > 0 && len(got.Phases[0].DeviceIDs) == 0 {
		t.Error("首阶段设备列表丢失（内容未完整持久化）")
	}
}

// TestCorruptRecordPayloadIsTreatedAsMissing 坏载荷必须按"查不到"处理，**不得**用空壳顶替。
// 为什么值得一条专门用例：载荷是内部结构的序列化、不承诺跨版本稳定，坏载荷迟早会出现。
// 实测的失败形态（变异验证时跑出来的真实结果，不是推测）：若解不出来还返回，
// 空壳的 TenantID 为空 ⇒ 被后面的租户校验挡成 **403 tenant mismatch**——
// 一个把"记录不可用"说成"租户不符"的误导性答案（用户会去找权限问题，而根因是数据坏了）。
// 故判据只认 404。变异验证：把"解不出按查不到处理"改成"解不出也返回" ⇒ 本用例判红。
func TestCorruptRecordPayloadIsTreatedAsMissing(t *testing.T) {
	st := memory.NewMemoryStore()
	s := r6Server(t, st)
	// 直接往 store 塞一条坏载荷（模拟：写侧版本漂移 / 存储被截断）
	if err := st.SaveTaskRecord(&model.TaskRecord{
		Kind: "batch", ID: "batch-corrupt", TenantID: "t1", Payload: []byte("{not-json"),
	}); err != nil {
		t.Fatalf("种坏载荷失败: %v", err)
	}
	if out := r6GetBatchStatus(t, s, "batch-corrupt"); out.Code != http.StatusNotFound {
		t.Errorf("坏载荷应按查不到处理（404），实得 %d：%s", out.Code, out.Body.String())
	}
}

// TestCanaryAdvanceSurvivesRestart 推进路径的两件事一起验：
//
//	① **重启后仍能推进**（记录不在索引里 ⇒ 必须从持久化记录补入；不补就 404）；
//	② **推进结果写穿**（否则重启后倒回推进前——用户刚推进过的阶段"又变回 pending"）。
//
// 判据用"推进后 + 再重启"两次读取的**逐字相同**，与查看路径同口径。
func TestCanaryAdvanceSurvivesRestart(t *testing.T) {
	st := memory.NewMemoryStore()
	seedDevices(st, 4)
	seedAgents(st, "t1", 4)
	s1 := r6Server(t, st)

	// 4 台设备 + 50% 比例 ⇒ 至少 2 个阶段（首阶段执行、其余 pending）
	rec := r6Post(t, s1, "/api/v1/tasks/canary",
		`{"deviceIDs":["dev-bench-0","dev-bench-1","dev-bench-2","dev-bench-3"],"taskType":"shell","command":"echo r6","strategy":"percentage","percentage":50}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("建灰度应 201，实得 %d：%s", rec.Code, rec.Body.String())
	}
	var created struct {
		CanaryID string `json:"canaryID"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.CanaryID == "" {
		t.Fatalf("取 canaryID 失败: %v / body=%s", err, rec.Body.String())
	}

	// 重启后推进（若补入索引缺失，这里会 404）
	s2 := r6Server(t, st)
	adv := r6PostCanaryAdvance(t, s2, created.CanaryID)
	if adv.Code != http.StatusOK {
		t.Fatalf("重启后推进应 200（记录须从持久化补入索引），实得 %d：%s", adv.Code, adv.Body.String())
	}
	afterAdvance := r6GetCanaryStatus(t, s2, created.CanaryID)
	if afterAdvance.Code != http.StatusOK {
		t.Fatalf("推进后查询应 200，实得 %d", afterAdvance.Code)
	}

	// 再重启：推进结果必须仍在（写穿生效）
	s3 := r6Server(t, st)
	afterRestart := r6GetCanaryStatus(t, s3, created.CanaryID)
	if afterRestart.Code != http.StatusOK {
		t.Fatalf("二次重启后查询应 200，实得 %d", afterRestart.Code)
	}
	if afterRestart.Body.String() != afterAdvance.Body.String() {
		t.Errorf("推进结果未写穿：二次重启后状态与推进后不一致（用户刚推进的阶段会倒回 pending）")
	}
}

// r6PostCanaryAdvance 调推进接口。
func r6PostCanaryAdvance(t *testing.T, s *Server, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/canary/"+id+"/advance", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-User-Id", "u-admin")
	rec := httptest.NewRecorder()
	s.handleCanaryAdvance(rec, req, id)
	return rec
}

// TestUnknownRecordStillNotFound 没建过的 ID 必须 404——回落路径不得把"查不到"变成"给个空壳"
// （空壳返回 200 是假成功：前端会把它当成"批次存在但没任务"，与真实情况相反）。
func TestUnknownRecordStillNotFound(t *testing.T) {
	st := memory.NewMemoryStore()
	s := r6Server(t, st)

	if out := r6GetBatchStatus(t, s, "batch-does-not-exist"); out.Code != http.StatusNotFound {
		t.Errorf("未建过的 batchID 应 404，实得 %d：%s", out.Code, out.Body.String())
	}
	if out := r6GetCanaryStatus(t, s, "canary-does-not-exist"); out.Code != http.StatusNotFound {
		t.Errorf("未建过的 canaryID 应 404，实得 %d：%s", out.Code, out.Body.String())
	}
}

// TestRecordIsTenantIsolated 跨租户取记录必须查不到（tenant_id 进主键的意义所在）。
func TestRecordIsTenantIsolated(t *testing.T) {
	st := memory.NewMemoryStore()
	seedDevices(st, 1)
	seedAgents(st, "t1", 1)
	s1 := r6Server(t, st)
	rec := r6Post(t, s1, "/api/v1/tasks/batch-exec",
		`{"deviceIDs":["dev-bench-0"],"type":"shell","command":"echo r6"}`)
	var created struct {
		BatchID string `json:"batchID"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// 另一个租户上下文取同一 batchID
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/batch/"+created.BatchID, nil)
	req.Header.Set("X-Tenant-ID", "other-tenant")
	req.Header.Set("X-User-Id", "u-other")
	out := httptest.NewRecorder()
	s1.handleBatchStatus(out, req, created.BatchID)
	if out.Code == http.StatusOK {
		t.Errorf("跨租户不应读到他人批次，实得 200：%s", out.Body.String())
	}
}
