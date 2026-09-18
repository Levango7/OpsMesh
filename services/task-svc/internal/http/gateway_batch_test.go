// gateway_batch_test.go — 批量执行 HTTP handler 单测。
//
// 覆盖端点：
//   - POST /api/v1/tasks/batch-exec   批量执行
//   - GET  /api/v1/tasks/batch/{id}   批量状态查询
//
// 测试模式对齐 device-svc gateway_test.go：newTestGateway + doReq 辅助函数。
// 本文件定义共享辅助函数（newTestGateway / doReq），供 canary/approval 测试复用。
package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/services/task-svc/internal/approval"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/store"
)

// newTestGateway 构造带全路由的 task-svc Gateway + mux（batch/canary/approval 均可用）。
//
// 设计：
//   - MemoryStore 同时实现 TaskStore/ScheduleStore/ResultStore/BatchStore/CanaryStore
//   - SetCanaryStore 注入灰度存储（否则 canary API 返回 503）
//   - SetApprovalEngine 注入审批引擎（否则 approval API 返回 503）
//   - jwtSecret="" → 仅租户隔离，不校验 token（actx.UserID 为空串）
//   - 清空包级 batchIdx 单例，保证测试间隔离
func newTestGateway(t *testing.T) *http.ServeMux {
	t.Helper()

	// 清空包级 batchIdx 单例，保证 batch 测试间隔离。
	batchIdx.mu.Lock()
	batchIdx.batches = make(map[string]*batchTask)
	batchIdx.mu.Unlock()

	st := store.NewMemoryStore()
	svc := service.NewService(st, st, st, st)
	svc.SetCanaryStore(st) // 注入灰度存储，使 canary API 可用

	g := NewGateway(svc, "") // jwtSecret 空=仅租户隔离
	g.SetApprovalEngine(approval.New())

	mux := http.NewServeMux()
	g.RegisterRoutes(mux)
	return mux
}

// doReq 发请求并返回 recorder（对齐 device-svc gateway_test.go doReq）。
func doReq(t *testing.T, mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// ============ POST /api/v1/tasks/batch-exec ============

// TestBatchExec_Create 验证批量执行正常创建：201 + batchID + 每设备任务详情。
func TestBatchExec_Create(t *testing.T) {
	mux := newTestGateway(t)

	rec := doReq(t, mux, http.MethodPost, "/api/v1/tasks/batch-exec",
		`{"deviceIDs":["dev-1","dev-2","dev-3"],"command":"echo hello"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("batch exec: got %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		BatchID string `json:"batchID"`
		Tasks   []struct {
			DeviceID string `json:"deviceID"`
			TaskID   string `json:"taskID"`
			Status   string `json:"status"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("batch exec 响应解析失败: %v", err)
	}
	if resp.BatchID == "" {
		t.Fatal("batchID 不应为空")
	}
	if len(resp.Tasks) != 3 {
		t.Fatalf("期望 3 个任务，实际 %d", len(resp.Tasks))
	}
	for _, task := range resp.Tasks {
		if task.TaskID == "" || task.Status == "" {
			t.Fatalf("任务回显异常（taskID/status 不应为空）: %+v", task)
		}
	}
}

// TestBatchExec_BadRequests 验证批量执行的参数校验：方法/JSON/空deviceIDs/空command。
func TestBatchExec_BadRequests(t *testing.T) {
	mux := newTestGateway(t)

	// 方法错误：GET → 405。
	rec := doReq(t, mux, http.MethodGet, "/api/v1/tasks/batch-exec", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET batch-exec: got %d, want 405", rec.Code)
	}

	// 无效 JSON → 400。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/tasks/batch-exec", `{invalid`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid JSON: got %d, want 400", rec.Code)
	}

	// 空 deviceIDs → 400。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/tasks/batch-exec", `{"command":"echo hi"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty deviceIDs: got %d, want 400", rec.Code)
	}

	// 空 command → 400。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/tasks/batch-exec", `{"deviceIDs":["dev-1"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty command: got %d, want 400", rec.Code)
	}
}

// ============ GET /api/v1/tasks/batch/{id} ============

// TestBatchStatus_Query 验证批量状态查询：创建后查询 200 + 不存在 404 + 方法错误 405。
func TestBatchStatus_Query(t *testing.T) {
	mux := newTestGateway(t)

	// 先创建一个批量任务。
	rec := doReq(t, mux, http.MethodPost, "/api/v1/tasks/batch-exec",
		`{"deviceIDs":["dev-1","dev-2"],"command":"echo hello"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("batch exec create: got %d, body=%s", rec.Code, rec.Body.String())
	}
	var createResp struct {
		BatchID string `json:"batchID"`
		Tasks   []struct {
			DeviceID string `json:"deviceID"`
			TaskID   string `json:"taskID"`
			Status   string `json:"status"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("batch exec 解析失败: %v", err)
	}

	// 查询存在的 batch → 200。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/tasks/batch/"+createResp.BatchID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("batch status: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var statusResp struct {
		BatchID   string `json:"batchID"`
		TaskType  string `json:"taskType"`
		Command   string `json:"command"`
		CreatedBy string `json:"createdBy"`
		Tasks     []struct {
			DeviceID string `json:"deviceID"`
			TaskID   string `json:"taskID"`
			Status   string `json:"status"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &statusResp); err != nil {
		t.Fatalf("batch status 解析失败: %v", err)
	}
	if statusResp.BatchID != createResp.BatchID {
		t.Fatalf("batchID 不匹配: got %s, want %s", statusResp.BatchID, createResp.BatchID)
	}
	if len(statusResp.Tasks) != 2 {
		t.Fatalf("期望 2 个任务，实际 %d", len(statusResp.Tasks))
	}

	// 查询不存在的 batch → 404。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/tasks/batch/no-such-batch", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing batch: got %d, want 404", rec.Code)
	}

	// 方法错误：POST → 405。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/tasks/batch/"+createResp.BatchID, "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST batch status: got %d, want 405", rec.Code)
	}
}
