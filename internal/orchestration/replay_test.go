package orchestration

// replay_test.go — D1「执行历史回放」最小闭环（2026-11-09 深度复核批立项，owner 裁决：实现而非降级）。
// 覆盖：重放创建新 run + originRunID 血缘、首次运行无血缘、血缘不可变（UpdateRun 不回写）、
// 源 run 不存在/跨租户/非法 ID 的错误路径、HTTP 契约（201/404/400/405）。
//
// 变异验证：删 trigger() 里 run.OriginRunID = originRunID 一行 → TestReplayRun_CreatesLineage 红。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// TestReplayRun_CreatesLineage 验证重放：以历史 run 为源重新执行，新 run 记录 originRunID 血缘，
// 源 run 与首次运行一样保持 OriginRunID=0，ListRuns 能看到两代。
func TestReplayRun_CreatesLineage(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	wfs := NewMemory()
	h := NewHandler(wfs, st)

	wf := &WorkflowDef{Name: "wf", AgentID: a.AgentID, TenantID: "t1",
		DAG: `[{"id":"n1","type":"shell","command":"s1"}]`}
	if err := wfs.Create(context.Background(), wf); err != nil {
		t.Fatal(err)
	}

	// 首次运行：无血缘。
	if _, err := h.Trigger(context.Background(), wf.ID, "t1"); err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	runs, _ := wfs.ListRuns(context.Background(), wf.ID, "t1")
	if len(runs) != 1 {
		t.Fatalf("after trigger len=%d, want 1", len(runs))
	}
	origin := runs[0]
	if origin.OriginRunID != 0 {
		t.Fatalf("首次运行 OriginRunID=%d, want 0", origin.OriginRunID)
	}

	// 重放：新 run 血缘指向源 run。
	replayed, err := h.ReplayRun(context.Background(), wf.ID, origin.ID, "t1")
	if err != nil {
		t.Fatalf("ReplayRun: %v", err)
	}
	if replayed.ID == origin.ID {
		t.Fatalf("重放不应复用源 run ID：%d", replayed.ID)
	}
	if replayed.OriginRunID != origin.ID {
		t.Fatalf("重放 run OriginRunID=%d, want %d", replayed.OriginRunID, origin.ID)
	}
	if replayed.Status != StatusRunning {
		t.Fatalf("重放 run status=%q, want running", replayed.Status)
	}

	runs, _ = wfs.ListRuns(context.Background(), wf.ID, "t1")
	if len(runs) != 2 {
		t.Fatalf("after replay len=%d, want 2", len(runs))
	}
	if runs[1].OriginRunID != origin.ID {
		t.Fatalf("ListRuns 第二条 OriginRunID=%d, want %d", runs[1].OriginRunID, origin.ID)
	}
	if runs[0].OriginRunID != 0 {
		t.Fatalf("源 run OriginRunID 被改写为 %d, want 0", runs[0].OriginRunID)
	}
}

// TestReplayRun_LineageImmutable 验证血缘写库后不可变：UpdateRun（Reconcile 终态回写路径）
// 不回写 origin_run_id，源 run 与重放 run 的血缘值都保持原样。
func TestReplayRun_LineageImmutable(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	wfs := NewMemory()
	h := NewHandler(wfs, st)

	wf := &WorkflowDef{Name: "wf", AgentID: a.AgentID, TenantID: "t1",
		DAG: `[{"id":"n1","type":"shell","command":"s1"}]`}
	if err := wfs.Create(context.Background(), wf); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Trigger(context.Background(), wf.ID, "t1"); err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	origin, _ := wfs.ListRuns(context.Background(), wf.ID, "t1")
	replayed, err := h.ReplayRun(context.Background(), wf.ID, origin[0].ID, "t1")
	if err != nil {
		t.Fatalf("ReplayRun: %v", err)
	}

	// 模拟 Reconcile 终态回写：改状态 + FinishedAt + NodeStates 后 UpdateRun。
	replayed.Status = StatusSuccess
	replayed.NodeStates = map[string]string{"n1": "done"}
	if err := wfs.UpdateRun(context.Background(), replayed); err != nil {
		t.Fatalf("UpdateRun replayed: %v", err)
	}
	origin[0].Status = StatusFailed
	if err := wfs.UpdateRun(context.Background(), &origin[0]); err != nil {
		t.Fatalf("UpdateRun origin: %v", err)
	}

	runs, _ := wfs.ListRuns(context.Background(), wf.ID, "t1")
	for _, r := range runs {
		want := int64(0)
		if r.ID == replayed.ID {
			want = origin[0].ID
		}
		if r.OriginRunID != want {
			t.Fatalf("run %d UpdateRun 后 OriginRunID=%d, want %d（血缘不可变）", r.ID, r.OriginRunID, want)
		}
	}
}

// TestReplayRun_OriginNotFound 验证源 run 不存在 / 属于其他工作流 / 跨租户时均报 ErrWFNotFound
// （跨租户不泄露存在性，与 listRuns 语义一致）。
func TestReplayRun_OriginNotFound(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	wfs := NewMemory()
	h := NewHandler(wfs, st)

	wf := &WorkflowDef{Name: "wf", AgentID: a.AgentID, TenantID: "t1",
		DAG: `[{"id":"n1","type":"shell","command":"s1"}]`}
	if err := wfs.Create(context.Background(), wf); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Trigger(context.Background(), wf.ID, "t1"); err != nil {
		t.Fatalf("Trigger: %v", err)
	}

	// 不存在的 run ID。
	if _, err := h.ReplayRun(context.Background(), wf.ID, 99999, "t1"); err != ErrWFNotFound {
		t.Fatalf("missing run err=%v, want ErrWFNotFound", err)
	}
	// 他租户的同 ID run（t2 视角下源 run 不存在）。
	if _, err := h.ReplayRun(context.Background(), wf.ID, 1, "t2"); err != ErrWFNotFound {
		t.Fatalf("cross-tenant err=%v, want ErrWFNotFound", err)
	}
	// 工作流不存在。
	if _, err := h.ReplayRun(context.Background(), 99999, 1, "t1"); err != ErrWFNotFound {
		t.Fatalf("missing workflow err=%v, want ErrWFNotFound", err)
	}
}

// TestReplayRun_API 验证 HTTP 契约：POST /api/v1/workflows/{id}/runs/{run_id}/replay
// 201 + body 带 originRunID；源不存在 404；非法 run id 400；GET 405。
func TestReplayRun_API(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	wfs := NewMemory()
	h := NewHandler(wfs, st)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	wf := &WorkflowDef{Name: "wf", AgentID: a.AgentID, TenantID: "t1",
		DAG: `[{"id":"n1","type":"shell","command":"s1"}]`}
	if err := wfs.Create(context.Background(), wf); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Trigger(context.Background(), wf.ID, "t1"); err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	origin, _ := wfs.ListRuns(context.Background(), wf.ID, "t1")
	base := "/api/v1/workflows/" + strconv.FormatInt(wf.ID, 10) + "/runs/" +
		strconv.FormatInt(origin[0].ID, 10) + "/replay"

	// 正常重放：201 + body originRunID。
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, newWFRequest(http.MethodPost, base, "", "t1"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("replay status=%d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var created WorkflowRun
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("replay body unmarshal: %v; body=%s", err, rec.Body.String())
	}
	if created.OriginRunID != origin[0].ID {
		t.Fatalf("replay body originRunID=%d, want %d", created.OriginRunID, origin[0].ID)
	}
	if created.ID == origin[0].ID {
		t.Fatalf("replay body id=%d 与源 run 相同", created.ID)
	}

	// 源 run 不存在：404。
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, newWFRequest(http.MethodPost,
		"/api/v1/workflows/"+strconv.FormatInt(wf.ID, 10)+"/runs/99999/replay", "", "t1"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing origin status=%d, want 404; body=%s", rec.Code, rec.Body.String())
	}

	// 非法 run id：400。
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, newWFRequest(http.MethodPost,
		"/api/v1/workflows/"+strconv.FormatInt(wf.ID, 10)+"/runs/abc/replay", "", "t1"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad run id status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}

	// GET 方法：405。
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, newWFRequest(http.MethodGet, base, "", "t1"))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET replay status=%d, want 405; body=%s", rec.Code, rec.Body.String())
	}

	// 跨租户重放：404（不泄露存在性）。
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, newWFRequest(http.MethodPost, base, "", "t2"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant replay status=%d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}
