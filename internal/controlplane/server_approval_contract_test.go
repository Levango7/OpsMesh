package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const approvalFlowLowerBody = `{
  "id":"flow-json",
  "name":"高危审批",
  "description":"双人复核",
  "tenantId":"default",
  "triggerType":"shell",
  "enabled":true,
  "steps":[{"id":"s1","name":"经理审批","order":1,"mode":"sequential","approvers":["alice"]}]
}`

func postApprovalFlow(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/approval/flows", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleApprovalFlows(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	return rec
}

func TestApprovalFlowHTTPJSONContract(t *testing.T) {
	s := newApprovalTestServer()
	rec := postApprovalFlow(t, s, approvalFlowLowerBody)
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	for _, key := range []string{"id", "name", "description", "tenantId", "triggerType", "steps", "enabled", "createdAt", "updatedAt"} {
		if _, ok := created[key]; !ok {
			t.Errorf("created 缺少小写键 %q: %v", key, created)
		}
	}
	for _, key := range []string{"ID", "Name", "Description", "TenantID", "TriggerType", "CreatedAt", "UpdatedAt"} {
		if _, ok := created[key]; ok {
			t.Errorf("created 不应包含 PascalCase 键 %q: %v", key, created)
		}
	}
	if created["description"] != "双人复核" {
		t.Errorf("description=%v", created["description"])
	}
	if created["createdAt"] == "0001-01-01T00:00:00Z" || created["updatedAt"] == "0001-01-01T00:00:00Z" {
		t.Error("创建/更新时间为零值，未返回引擎存储对象")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/approval/flows", nil)
	req.Header.Set("X-Tenant-ID", "default")
	listRec := httptest.NewRecorder()
	s.handleApprovalFlows(listRec, req)
	var listed struct {
		Flows []map[string]any `json:"flows"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if listed.Total != 1 || len(listed.Flows) != 1 {
		t.Fatalf("list total=%d flows=%d", listed.Total, len(listed.Flows))
	}
	if listed.Flows[0]["name"] != "高危审批" || listed.Flows[0]["description"] != "双人复核" {
		t.Errorf("list 字段错位: %+v", listed.Flows[0])
	}
}

func TestApprovalRequestAndHistoryHTTPJSONContract(t *testing.T) {
	s := newApprovalTestServer()
	postApprovalFlow(t, s, approvalFlowLowerBody)

	body := `{"id":"req-json","flowId":"flow-json","tenantId":"default","triggerType":"shell","operator":"alice","target":"dev-1","detail":"restart","risk":"high"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/approval/requests", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleApprovalRequests(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	for _, key := range []string{"id", "flowId", "flowName", "tenantId", "triggerType", "operator", "requester", "target", "detail", "risk", "resourceType", "resourceID", "status", "currentStep", "steps", "createdAt", "updatedAt"} {
		if _, ok := created[key]; !ok {
			t.Errorf("request 缺少键 %q: %v", key, created)
		}
	}
	if created["flowName"] != "高危审批" || created["requester"] != "alice" || created["resourceType"] != "shell" || created["resourceID"] != "dev-1" {
		t.Errorf("UI 兼容字段错误: %+v", created)
	}

	histReq := httptest.NewRequest(http.MethodGet, "/api/v1/approval/requests/req-json/history", nil)
	histReq.Header.Set("X-Tenant-ID", "default")
	histRec := httptest.NewRecorder()
	s.handleApprovalRequestRouting(histRec, histReq)
	if histRec.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%s", histRec.Code, histRec.Body.String())
	}
	var hist struct {
		History []map[string]any `json:"history"`
	}
	if err := json.Unmarshal(histRec.Body.Bytes(), &hist); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(hist.History) == 0 {
		t.Fatal("history 应为数组且非空")
	}
	entry := hist.History[0]
	if entry["step"] == nil || entry["approver"] != "alice" {
		t.Errorf("history UI 兼容字段错误: %+v", entry)
	}
}
