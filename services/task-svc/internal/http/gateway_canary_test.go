// gateway_canary_test.go — 灰度发布 HTTP handler 单测。
//
// 覆盖端点：
//   - POST /api/v1/tasks/canary          灰度发布创建
//   - GET  /api/v1/tasks/canary/{id}     灰度发布状态查询
//   - POST /api/v1/tasks/canary/{id}/advance  推进到下一阶段
//
// 辅助函数 newTestGateway / doReq 定义在 gateway_batch_test.go（同包共享）。
package http

import (
	"encoding/json"
	"net/http"
	"testing"
)

// ============ POST /api/v1/tasks/canary ============

// TestCanaryCreate 验证灰度发布正常创建：201 + canaryID + phases。
func TestCanaryCreate(t *testing.T) {
	mux := newTestGateway(t)

	// percentage 策略：2 设备 50% → 两阶段（各 1 设备）。
	rec := doReq(t, mux, http.MethodPost, "/api/v1/tasks/canary",
		`{"deviceIDs":["dev-1","dev-2"],"command":"echo hello","strategy":"percentage","percentage":50}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("canary create: got %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		CanaryID string `json:"canaryID"`
		Phases   []struct {
			Phase     int      `json:"phase"`
			DeviceIDs []string `json:"deviceIDs"`
			Status    string   `json:"status"`
		} `json:"phases"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("canary create 响应解析失败: %v", err)
	}
	if resp.CanaryID == "" {
		t.Fatal("canaryID 不应为空")
	}
	if len(resp.Phases) != 2 {
		t.Fatalf("期望 2 阶段（50%% 策略），实际 %d", len(resp.Phases))
	}
	// 第一阶段应已执行（running），第二阶段 pending。
	if resp.Phases[0].Status != "running" {
		t.Fatalf("第一阶段应 running，实际 %q", resp.Phases[0].Status)
	}
	if resp.Phases[1].Status != "pending" {
		t.Fatalf("第二阶段应 pending，实际 %q", resp.Phases[1].Status)
	}
}

// TestCanaryCreate_BadRequests 验证灰度发布的参数校验。
func TestCanaryCreate_BadRequests(t *testing.T) {
	mux := newTestGateway(t)

	// 方法错误：GET → 405。
	rec := doReq(t, mux, http.MethodGet, "/api/v1/tasks/canary", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET canary: got %d, want 405", rec.Code)
	}

	// 无效 JSON → 400。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/tasks/canary", `{invalid`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid JSON: got %d, want 400", rec.Code)
	}

	// 空 deviceIDs → 400（service 层 ErrTaskInvalid → writeCanaryError 400）。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/tasks/canary",
		`{"command":"echo hi","strategy":"percentage"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty deviceIDs: got %d, want 400; body=%s", rec.Code, rec.Body.String())
	}

	// 空 command → 400。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/tasks/canary",
		`{"deviceIDs":["dev-1"],"strategy":"percentage"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty command: got %d, want 400; body=%s", rec.Code, rec.Body.String())
	}

	// 无效 strategy → 400。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/tasks/canary",
		`{"deviceIDs":["dev-1"],"command":"echo hi","strategy":"bogus"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid strategy: got %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// ============ GET /api/v1/tasks/canary/{id} ============

// TestCanaryStatus_Query 验证灰度发布状态查询：创建后查询 200 + 不存在 404。
func TestCanaryStatus_Query(t *testing.T) {
	mux := newTestGateway(t)

	// 先创建灰度发布。
	rec := doReq(t, mux, http.MethodPost, "/api/v1/tasks/canary",
		`{"deviceIDs":["dev-1"],"command":"echo hello","strategy":"label"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("canary create: got %d, body=%s", rec.Code, rec.Body.String())
	}
	var createResp struct {
		CanaryID string `json:"canaryID"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("canary create 解析失败: %v", err)
	}

	// 查询存在 → 200。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/tasks/canary/"+createResp.CanaryID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("canary status: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var statusResp struct {
		CanaryID  string `json:"canaryID"`
		Strategy  string `json:"strategy"`
		TaskType  string `json:"taskType"`
		Command   string `json:"command"`
		CreatedBy string `json:"createdBy"`
		Phases    []struct {
			Phase   int      `json:"phase"`
			Status  string   `json:"status"`
			TaskIDs []string `json:"deviceIDs"`
		} `json:"phases"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &statusResp); err != nil {
		t.Fatalf("canary status 解析失败: %v", err)
	}
	if statusResp.CanaryID != createResp.CanaryID {
		t.Fatalf("canaryID 不匹配: got %s, want %s", statusResp.CanaryID, createResp.CanaryID)
	}
	if statusResp.Strategy != "label" {
		t.Fatalf("strategy 应为 label，实际 %q", statusResp.Strategy)
	}

	// 查询不存在 → 404。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/tasks/canary/no-such-canary", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing canary: got %d, want 404", rec.Code)
	}

	// 方法错误：PUT → 405。
	rec = doReq(t, mux, http.MethodPut, "/api/v1/tasks/canary/"+createResp.CanaryID, "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT canary status: got %d, want 405", rec.Code)
	}
}

// ============ POST /api/v1/tasks/canary/{id}/advance ============

// TestCanaryAdvance 验证灰度发布推进：推进成功 200 + 无 pending 阶段 400。
func TestCanaryAdvance(t *testing.T) {
	mux := newTestGateway(t)

	// 创建两阶段灰度（percentage 50%，2 设备）。
	rec := doReq(t, mux, http.MethodPost, "/api/v1/tasks/canary",
		`{"deviceIDs":["dev-1","dev-2"],"command":"echo hello","strategy":"percentage","percentage":50}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("canary create: got %d, body=%s", rec.Code, rec.Body.String())
	}
	var createResp struct {
		CanaryID string `json:"canaryID"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("canary create 解析失败: %v", err)
	}

	// 推进到第二阶段 → 200。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/tasks/canary/"+createResp.CanaryID+"/advance", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("canary advance: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var advanceResp struct {
		CanaryID string `json:"canaryID"`
		Phase    int    `json:"phase"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &advanceResp); err != nil {
		t.Fatalf("canary advance 解析失败: %v", err)
	}
	if advanceResp.CanaryID != createResp.CanaryID {
		t.Fatalf("canaryID 不匹配: got %s, want %s", advanceResp.CanaryID, createResp.CanaryID)
	}

	// 再推进：无 pending 阶段 → 400。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/tasks/canary/"+createResp.CanaryID+"/advance", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("advance no pending: got %d, want 400; body=%s", rec.Code, rec.Body.String())
	}

	// 推进不存在的 canary → 404。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/tasks/canary/no-such-canary/advance", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("advance missing canary: got %d, want 404", rec.Code)
	}

	// 方法错误：GET → 405。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/tasks/canary/"+createResp.CanaryID+"/advance", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET advance: got %d, want 405", rec.Code)
	}
}
