package cmdb

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opsmesh/internal/proto"
)

// ===== Handler.Store() =====

func TestHandlerStore(t *testing.T) {
	s := NewMemoryCiStore()
	h := NewHandler(s)
	if h.Store() != s {
		t.Fatal("Store() should return underlying store")
	}
}

// ===== handleCIByID: PUT / DELETE =====

func TestHandlerUpdateCIViaHTTP(t *testing.T) {
	_, mux := newTestHandler(newTestStore())
	body, _ := json.Marshal(map[string]interface{}{
		"ciType": "machine", "name": "host-1-updated", "attrs": map[string]string{"ip": "10.0.0.100"},
	})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/cmdb/ci/ci-host-1", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update CI expect 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated CiItem
	json.Unmarshal(w.Body.Bytes(), &updated)
	if updated.Name != "host-1-updated" {
		t.Errorf("name not updated: %s", updated.Name)
	}
}

func TestHandlerUpdateCIBadJSON(t *testing.T) {
	_, mux := newTestHandler(newTestStore())
	req := httptest.NewRequest(http.MethodPut, "/api/v1/cmdb/ci/ci-host-1", strings.NewReader("{bad json"))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expect 400, got %d", w.Code)
	}
}

func TestHandlerUpdateCINotFound(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	body, _ := json.Marshal(map[string]interface{}{"ciType": "machine", "name": "x"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/cmdb/ci/ci-missing", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expect 500, got %d", w.Code)
	}
}

func TestHandlerDeleteCIViaHTTP(t *testing.T) {
	_, mux := newTestHandler(newTestStore())
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/cmdb/ci/ci-host-1", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete CI expect 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "deleted" {
		t.Errorf("expect deleted, got %s", resp["status"])
	}
}

func TestHandlerDeleteCINotFound(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/cmdb/ci/ci-missing", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expect 500, got %d", w.Code)
	}
}

func TestHandlerGetCINotFound(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/ci/ci-missing", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expect 404, got %d", w.Code)
	}
}

func TestHandleCIByIDInvalidID(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	// id 含 "/" 走 prefix 路由，但若 trim 后含 / 视为 invalid
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/ci/", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expect 400, got %d", w.Code)
	}
}

func TestHandleCIByIDMethodNotAllowed(t *testing.T) {
	_, mux := newTestHandler(newTestStore())
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/cmdb/ci/ci-host-1", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expect 405, got %d", w.Code)
	}
}

// ===== handleCITypes: POST 错误路径 =====

func TestHandleCITypesPostNameRequired(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	body, _ := json.Marshal(map[string]interface{}{"displayName": "x"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/types", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expect 400, got %d", w.Code)
	}
}

func TestHandleCITypesPostBadJSON(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/types", strings.NewReader("{bad"))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expect 400, got %d", w.Code)
	}
}

func TestHandleCITypesPostDuplicate(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	body, _ := json.Marshal(CiType{Name: "machine"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/types", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expect 500 for duplicate builtin type, got %d", w.Code)
	}
}

func TestHandleCITypesGet(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/types", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expect 200, got %d", w.Code)
	}
	var types []CiType
	json.Unmarshal(w.Body.Bytes(), &types)
	if len(types) != 5 {
		t.Errorf("expect 5 builtin types, got %d", len(types))
	}
}

// ===== handleCIReject =====

func TestHandleCIReject(t *testing.T) {
	_, mux := newTestHandler(newTestStore())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/ci/ci-host-1/reject", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("reject expect 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["approvalStatus"] != ApprovalRejected {
		t.Errorf("expect rejected, got %s", resp["approvalStatus"])
	}
}

func TestHandleCIRejectNotFound(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/ci/ci-missing/reject", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expect 404, got %d", w.Code)
	}
}

func TestHandleCIRejectMethodNotAllowed(t *testing.T) {
	_, mux := newTestHandler(newTestStore())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/ci/ci-host-1/reject", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expect 405, got %d", w.Code)
	}
}

// ===== handleCIApprove: 错误路径 =====

func TestHandleCIApproveNotFound(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/ci/ci-missing/approve", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expect 404, got %d", w.Code)
	}
}

func TestHandleCIApproveMethodNotAllowed(t *testing.T) {
	_, mux := newTestHandler(newTestStore())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/ci/ci-host-1/approve", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expect 405, got %d", w.Code)
	}
}

// ===== handleCIRelations =====

func TestHandleCIRelations(t *testing.T) {
	_, mux := newTestHandler(newTestStore())
	// 先建关系
	body, _ := json.Marshal(CiRelation{SourceCIID: "ci-host-1", TargetCIID: "ci-os-1", RelationType: "runs_on"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/relations", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	mux.ServeHTTP(httptest.NewRecorder(), req)

	// GET 关系
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/ci/ci-host-1/relations", nil)
	req2.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req2)
	if w.Code != http.StatusOK {
		t.Fatalf("expect 200, got %d: %s", w.Code, w.Body.String())
	}
	var rels []CiRelation
	json.Unmarshal(w.Body.Bytes(), &rels)
	if len(rels) != 1 {
		t.Errorf("expect 1 relation, got %d", len(rels))
	}
}

func TestHandleCIRelationsMethodNotAllowed(t *testing.T) {
	_, mux := newTestHandler(newTestStore())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/ci/ci-host-1/relations", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expect 405, got %d", w.Code)
	}
}

// ===== handleCIRelationGraph: 错误路径 =====

func TestHandleCIRelationGraphMethodNotAllowed(t *testing.T) {
	_, mux := newTestHandler(newTestStore())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/ci/ci-host-1/graph", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expect 405, got %d", w.Code)
	}
}

func TestHandleCIRelationGraphNotFound(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/ci/ci-missing/graph", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expect 500, got %d", w.Code)
	}
}

// ===== handleAttrTemplateByID =====

func TestHandleAttrTemplateByIDGet(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	// 先创建模板
	body, _ := json.Marshal(CiAttrTemplate{CiType: "machine", AttrKey: "ip", Label: "IP", AttrType: "string"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/attr-templates", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var tmpl CiAttrTemplate
	json.Unmarshal(w.Body.Bytes(), &tmpl)

	// GET by ID
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/attr-templates/"+itoa(tmpl.ID), nil)
	req2.Header.Set("X-Tenant-ID", "t1")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expect 200, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestHandleAttrTemplateByIDNotFound(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/attr-templates/999", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expect 404, got %d", w.Code)
	}
}

func TestHandleAttrTemplateByIDInvalidID(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/attr-templates/abc", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expect 400, got %d", w.Code)
	}
}

func TestHandleAttrTemplateByIDEmptyID(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/attr-templates/", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expect 400, got %d", w.Code)
	}
}

func TestHandleAttrTemplateByIDPut(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	// 创建
	body, _ := json.Marshal(CiAttrTemplate{CiType: "machine", AttrKey: "ip", Label: "IP", AttrType: "string"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/attr-templates", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var tmpl CiAttrTemplate
	json.Unmarshal(w.Body.Bytes(), &tmpl)

	// 更新
	body2, _ := json.Marshal(CiAttrTemplate{CiType: "machine", AttrKey: "ip", Label: "Updated", AttrType: "string"})
	req2 := httptest.NewRequest(http.MethodPut, "/api/v1/cmdb/attr-templates/"+itoa(tmpl.ID), bytes.NewReader(body2))
	req2.Header.Set("X-Tenant-ID", "t1")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expect 200, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestHandleAttrTemplateByIDPutBadJSON(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	req := httptest.NewRequest(http.MethodPut, "/api/v1/cmdb/attr-templates/1", strings.NewReader("{bad"))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expect 400, got %d", w.Code)
	}
}

func TestHandleAttrTemplateByIDPutNotFound(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	body, _ := json.Marshal(CiAttrTemplate{CiType: "machine", AttrKey: "ip"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/cmdb/attr-templates/999", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expect 500, got %d", w.Code)
	}
}

func TestHandleAttrTemplateByIDDelete(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	// 创建
	body, _ := json.Marshal(CiAttrTemplate{CiType: "machine", AttrKey: "ip", Label: "IP"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/attr-templates", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var tmpl CiAttrTemplate
	json.Unmarshal(w.Body.Bytes(), &tmpl)

	// 删除
	req2 := httptest.NewRequest(http.MethodDelete, "/api/v1/cmdb/attr-templates/"+itoa(tmpl.ID), nil)
	req2.Header.Set("X-Tenant-ID", "t1")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expect 200, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestHandleAttrTemplateByIDDeleteNotFound(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/cmdb/attr-templates/999", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expect 500, got %d", w.Code)
	}
}

func TestHandleAttrTemplateByIDMethodNotAllowed(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/cmdb/attr-templates/1", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expect 405, got %d", w.Code)
	}
}

// ===== handleRelations: 错误路径 =====

func TestHandleRelationsGetNoCIID(t *testing.T) {
	_, mux := newTestHandler(newTestStore())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/relations", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expect 400, got %d", w.Code)
	}
}

func TestHandleRelationsGetSuccess(t *testing.T) {
	_, mux := newTestHandler(newTestStore())
	// 先建关系
	body, _ := json.Marshal(CiRelation{SourceCIID: "ci-host-1", TargetCIID: "ci-os-1", RelationType: "runs_on"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/relations", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	mux.ServeHTTP(httptest.NewRecorder(), req)

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/relations?ciID=ci-host-1", nil)
	req2.Header.Set("X-Tenant-ID", "t1")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expect 200, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestHandleRelationsPostBadJSON(t *testing.T) {
	_, mux := newTestHandler(newTestStore())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/relations", strings.NewReader("{bad"))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expect 400, got %d", w.Code)
	}
}

func TestHandleRelationsMethodNotAllowed(t *testing.T) {
	_, mux := newTestHandler(newTestStore())
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/cmdb/relations", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expect 405, got %d", w.Code)
	}
}

// ===== handleCIPending: 错误路径 =====

func TestHandleCIPendingMethodNotAllowed(t *testing.T) {
	_, mux := newTestHandler(newTestStore())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/ci/pending", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expect 405, got %d", w.Code)
	}
}

func TestHandleCIPendingEmpty(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/ci/pending", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expect 200, got %d", w.Code)
	}
	var items []CiItem
	json.Unmarshal(w.Body.Bytes(), &items)
	if len(items) != 0 {
		t.Errorf("expect empty, got %d", len(items))
	}
}

// ===== handleCIExport: 错误路径 =====

func TestHandleCIExportMethodNotAllowed(t *testing.T) {
	_, mux := newTestHandler(newTestStore())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/ci/export", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expect 405, got %d", w.Code)
	}
}

func TestHandleCIExportEmpty(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/ci/export", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expect 200, got %d", w.Code)
	}
}

// ===== handleCIImport: CSV 路径 =====

func TestHandleCIImportCSV(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	csvBody := "id,ciType,name,status,approvalStatus,source,agentID,deviceID,attrs\nci-csv-1,machine,h1,active,approved,import,a1,d1,ip=1.1.1.1\n"
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/ci/import?format=csv", strings.NewReader(csvBody))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("import csv expect 200, got %d: %s", w.Code, w.Body.String())
	}
	var summary map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &summary)
	if int(summary["created"].(float64)) != 1 {
		t.Errorf("expect 1 created, got %v", summary["created"])
	}
}

func TestHandleCIImportCSVUpdateExisting(t *testing.T) {
	_, mux := newTestHandler(newTestStore())
	// ci-host-1 已存在 → 应触发 update
	csvBody := "id,ciType,name,status,approvalStatus,source,agentID,deviceID,attrs\nci-host-1,machine,h-updated,active,approved,import,a1,d1,ip=2.2.2.2\n"
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/ci/import?format=csv", strings.NewReader(csvBody))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("import csv expect 200, got %d: %s", w.Code, w.Body.String())
	}
	var summary map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &summary)
	if int(summary["updated"].(float64)) != 1 {
		t.Errorf("expect 1 updated, got %v", summary)
	}
}

func TestHandleCIImportCSVBadFormat(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	// CSV 列数不一致但 FieldsPerRecord=-1 容忍；构造一个无法解析的 body
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/ci/import?format=csv", strings.NewReader("not\"valid\"csv"))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expect 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleCIImportCSVOnlyHeader(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	csvBody := "id,ciType,name,status,approvalStatus,source,agentID,deviceID,attrs\n"
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/ci/import?format=csv", strings.NewReader(csvBody))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expect 200, got %d", w.Code)
	}
}

func TestHandleCIImportMethodNotAllowed(t *testing.T) {
	_, mux := newTestHandler(newTestStore())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/ci/import", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expect 405, got %d", w.Code)
	}
}

func TestHandleCIImportBadJSON(t *testing.T) {
	_, mux := newTestHandler(newTestStore())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/ci/import", strings.NewReader("{bad"))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expect 400, got %d", w.Code)
	}
}

func TestHandleCIImportUnknownType(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	body, _ := json.Marshal([]map[string]interface{}{
		{"id": "ci-x", "ciType": "ghost", "name": "x"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/ci/import", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expect 200, got %d", w.Code)
	}
	var summary map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &summary)
	errs, _ := summary["errors"].([]interface{})
	if len(errs) != 1 {
		t.Errorf("expect 1 error, got %v", summary)
	}
}

// ===== handleAttrTemplates: 错误路径 =====

func TestHandleAttrTemplatesPostBadJSON(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/attr-templates", strings.NewReader("{bad"))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expect 400, got %d", w.Code)
	}
}

func TestHandleAttrTemplatesMethodNotAllowed(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/cmdb/attr-templates", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expect 405, got %d", w.Code)
	}
}

// ===== handleCIs: 错误路径 =====

func TestHandleCIsPostBadJSON(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/ci", strings.NewReader("{bad"))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expect 400, got %d", w.Code)
	}
}

func TestHandleCIsPostUnknownType(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	body, _ := json.Marshal(map[string]interface{}{"ciType": "ghost", "name": "x"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/ci", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expect 500, got %d", w.Code)
	}
}

func TestHandleCIsMethodNotAllowed(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/cmdb/ci", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expect 405, got %d", w.Code)
	}
}

// ===== ciAttrsToCSV / csvAttrsToMap / parseCICSV =====

func TestCiAttrsToCSVEempty(t *testing.T) {
	if ciAttrsToCSV(nil) != "" {
		t.Error("nil attrs should return empty")
	}
	if ciAttrsToCSV(map[string]string{}) != "" {
		t.Error("empty attrs should return empty")
	}
}

func TestCiAttrsToCSVWithValues(t *testing.T) {
	out := ciAttrsToCSV(map[string]string{"ip": "1.1.1.1", "host": "h1"})
	// 顺序不确定，检查两个 k=v 都在
	if !strings.Contains(out, "ip=1.1.1.1") || !strings.Contains(out, "host=h1") {
		t.Errorf("attrs missing: %s", out)
	}
}

func TestCsvAttrsToMap(t *testing.T) {
	cases := []struct {
		in   string
		want map[string]string
	}{
		{"", map[string]string{}},
		{"  ", map[string]string{}},
		{"ip=1.1.1.1", map[string]string{"ip": "1.1.1.1"}},
		{"ip=1.1.1.1;host=h1", map[string]string{"ip": "1.1.1.1", "host": "h1"}},
		{"ip=1.1.1.1;", map[string]string{"ip": "1.1.1.1"}},
		{"ip=1.1.1.1; badpair", map[string]string{"ip": "1.1.1.1"}},
		// 整体 TrimSpace 后 key 不再单独 trim，"  ip  =  1.1.1.1  " → key="ip  ", value="  1.1.1.1"
		{"  ip  =  1.1.1.1  ", map[string]string{"ip  ": "  1.1.1.1"}},
	}
	for _, c := range cases {
		got := csvAttrsToMap(c.in)
		if len(got) != len(c.want) {
			t.Errorf("input %q: expect len %d, got %d (%v)", c.in, len(c.want), len(got), got)
			continue
		}
		for k, v := range c.want {
			if got[k] != v {
				t.Errorf("input %q: key %s expect %q, got %q", c.in, k, v, got[k])
			}
		}
	}
}

func TestParseCICSV(t *testing.T) {
	// 完整 CSV
	body := "id,ciType,name,status,approvalStatus,source,agentID,deviceID,attrs\nci-1,machine,h1,active,approved,manual,a1,d1,ip=1.1.1.1\n"
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/ci/import?format=csv", strings.NewReader(body))
	items, parseErr := parseCICSV(req)
	if parseErr != "" {
		t.Fatalf("parseCICSV err: %s", parseErr)
	}
	if len(items) != 1 {
		t.Fatalf("expect 1 item, got %d", len(items))
	}
	if items[0].ID != "ci-1" || items[0].CiType != "machine" || items[0].Attrs["ip"] != "1.1.1.1" {
		t.Errorf("item wrong: %+v", items[0])
	}
}

func TestParseCICSVEmpty(t *testing.T) {
	// 仅表头
	body := "id,ciType,name\n"
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/ci/import?format=csv", strings.NewReader(body))
	items, parseErr := parseCICSV(req)
	if parseErr != "" {
		t.Fatalf("parseCICSV err: %s", parseErr)
	}
	if len(items) != 0 {
		t.Errorf("expect 0 items, got %d", len(items))
	}
}

func TestParseCICSVBadCSV(t *testing.T) {
	// 引号不闭合 → CSV 解析错误
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/ci/import?format=csv", strings.NewReader("a,b\n\"unclosed"))
	_, parseErr := parseCICSV(req)
	if parseErr == "" {
		t.Error("expect parse error for malformed CSV")
	}
}

// ===== idTakenByOtherTenant =====

func TestIdTakenByOtherTenantEmpty(t *testing.T) {
	h := NewHandler(NewMemoryCiStore())
	if h.idTakenByOtherTenant(context.Background(), "", "t1") {
		t.Error("empty id should return false")
	}
}

func TestIdTakenByOtherTenantSameTenant(t *testing.T) {
	s := NewMemoryCiStore()
	_ = s.CreateCI(context.Background(), &CiItem{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "h1", Status: "active"})
	h := NewHandler(s)
	if h.idTakenByOtherTenant(context.Background(), "ci-1", "t1") {
		t.Error("same tenant should return false")
	}
}

func TestIdTakenByOtherTenantOtherTenant(t *testing.T) {
	s := NewMemoryCiStore()
	_ = s.CreateCI(context.Background(), &CiItem{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "h1", Status: "active"})
	h := NewHandler(s)
	if !h.idTakenByOtherTenant(context.Background(), "ci-1", "t2") {
		t.Error("other tenant should return true")
	}
}

func TestIdTakenByOtherTenantNotFound(t *testing.T) {
	h := NewHandler(NewMemoryCiStore())
	if h.idTakenByOtherTenant(context.Background(), "ci-missing", "t1") {
		t.Error("not found should return false")
	}
}

// ===== HandleReport: 错误路径 =====

func TestHandleReportNil(t *testing.T) {
	s := NewMemoryCiStore()
	h := NewHandler(s)
	// nil handler
	var nilH *Handler
	nilH.HandleReport(context.Background(), "a", &proto.CmdbReport{})
	// nil report
	h.HandleReport(context.Background(), "a", nil)
	// 验证不会 panic 即可
}

func TestHandleReportCreateCIError(t *testing.T) {
	s := NewMemoryCiStore()
	h := NewHandler(s)
	// 用未知 CI 类型触发 CreateCI 错误
	report := &proto.CmdbReport{
		CiType: "ghost-type",
		Attrs:  []proto.CmdbAttr{{Key: "k", Value: "v"}},
	}
	h.HandleReport(context.Background(), "agent-X", report)
	// 应该没有 CI 被创建
	items, _ := s.GetCIs(context.Background(), "ghost-type", "active", "")
	if len(items) != 0 {
		t.Errorf("expect 0 CI due to CreateCI error, got %d", len(items))
	}
}

// ===== MemoryCiStore: GetCIHistory =====

func TestMemoryGetCIHistory(t *testing.T) {
	s := NewMemoryCiStore()
	_ = s.CreateCI(context.Background(), &CiItem{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "h1", Status: "active"})
	hist, err := s.GetCIHistory(context.Background(), "ci-1", "t1", 10)
	if err != nil {
		t.Fatalf("GetCIHistory err: %v", err)
	}
	if len(hist) != 1 {
		t.Errorf("expect 1 history, got %d", len(hist))
	}
}

func TestMemoryGetCIHistoryNotFound(t *testing.T) {
	s := NewMemoryCiStore()
	_, err := s.GetCIHistory(context.Background(), "ci-missing", "t1", 10)
	if err == nil {
		t.Error("expect error for missing CI")
	}
}

// ===== MemoryCiStore: 各种 not found / 租户不匹配 =====

func TestMemoryGetCITenantMismatch(t *testing.T) {
	s := NewMemoryCiStore()
	_ = s.CreateCI(context.Background(), &CiItem{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "h1", Status: "active"})
	if _, err := s.GetCI(context.Background(), "ci-1", "t2"); err == nil {
		t.Error("expect error for tenant mismatch")
	}
}

func TestMemorySetApprovalNotFound(t *testing.T) {
	s := NewMemoryCiStore()
	if err := s.SetApproval(context.Background(), "ci-missing", "t1", ApprovalPending); err == nil {
		t.Error("expect error for missing CI")
	}
}

func TestMemorySetApprovalTenantMismatch(t *testing.T) {
	s := NewMemoryCiStore()
	_ = s.CreateCI(context.Background(), &CiItem{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "h1", Status: "active"})
	if err := s.SetApproval(context.Background(), "ci-1", "t2", ApprovalPending); err == nil {
		t.Error("expect error for tenant mismatch")
	}
}

func TestMemoryUpdateCINotFound(t *testing.T) {
	s := NewMemoryCiStore()
	err := s.UpdateCI(context.Background(), &CiItem{ID: "ci-missing", CiType: "machine", TenantID: "t1", Name: "h1", Status: "active"})
	if err == nil {
		t.Error("expect error for missing CI")
	}
}

func TestMemoryUpdateCITenantMismatch(t *testing.T) {
	s := NewMemoryCiStore()
	_ = s.CreateCI(context.Background(), &CiItem{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "h1", Status: "active"})
	err := s.UpdateCI(context.Background(), &CiItem{ID: "ci-1", CiType: "machine", TenantID: "t2", Name: "h1", Status: "active"})
	if err == nil {
		t.Error("expect error for tenant mismatch")
	}
}

func TestMemoryUpdateCIPreserveAttrs(t *testing.T) {
	s := NewMemoryCiStore()
	_ = s.CreateCI(context.Background(), &CiItem{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "h1", Status: "active", Attrs: map[string]string{"ip": "1.1.1.1"}})
	// UpdateCI with nil Attrs → 保留原 attrs
	err := s.UpdateCI(context.Background(), &CiItem{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "h1-new", Status: "active"})
	if err != nil {
		t.Fatalf("UpdateCI err: %v", err)
	}
	got, _ := s.GetCI(context.Background(), "ci-1", "t1")
	if got.Attrs["ip"] != "1.1.1.1" {
		t.Errorf("attrs should be preserved, got %v", got.Attrs)
	}
}

func TestMemoryDeleteCINotFound(t *testing.T) {
	s := NewMemoryCiStore()
	if err := s.DeleteCI(context.Background(), "ci-missing", "t1"); err == nil {
		t.Error("expect error for missing CI")
	}
}

func TestMemoryDeleteCITenantMismatch(t *testing.T) {
	s := NewMemoryCiStore()
	_ = s.CreateCI(context.Background(), &CiItem{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "h1", Status: "active"})
	if err := s.DeleteCI(context.Background(), "ci-1", "t2"); err == nil {
		t.Error("expect error for tenant mismatch")
	}
}

func TestMemoryGetCIRelationsTenantFilter(t *testing.T) {
	s := NewMemoryCiStore()
	_ = s.CreateCI(context.Background(), &CiItem{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "h1", Status: "active"})
	_ = s.CreateCI(context.Background(), &CiItem{ID: "ci-2", CiType: "os", TenantID: "t1", Name: "o1", Status: "active"})
	_ = s.CreateRelation(context.Background(), &CiRelation{SourceCIID: "ci-1", TargetCIID: "ci-2", RelationType: "runs_on", TenantID: "t1"})
	// 不同租户查询 → 应为空
	rels, _ := s.GetCIRelations(context.Background(), "ci-1", "t2")
	if len(rels) != 0 {
		t.Errorf("expect 0 for other tenant, got %d", len(rels))
	}
}

func TestMemoryDeleteRelationNotFound(t *testing.T) {
	s := NewMemoryCiStore()
	if err := s.DeleteRelation(context.Background(), 999, "t1"); err == nil {
		t.Error("expect error for missing relation")
	}
}

func TestMemoryDeleteRelationTenantMismatch(t *testing.T) {
	s := NewMemoryCiStore()
	_ = s.CreateCI(context.Background(), &CiItem{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "h1", Status: "active"})
	_ = s.CreateCI(context.Background(), &CiItem{ID: "ci-2", CiType: "os", TenantID: "t1", Name: "o1", Status: "active"})
	_ = s.CreateRelation(context.Background(), &CiRelation{SourceCIID: "ci-1", TargetCIID: "ci-2", RelationType: "runs_on", TenantID: "t1"})
	rels, _ := s.GetCIRelations(context.Background(), "ci-1", "t1")
	if err := s.DeleteRelation(context.Background(), rels[0].ID, "t2"); err == nil {
		t.Error("expect error for tenant mismatch")
	}
}

func TestMemoryUpdateAttrTemplateNotFound(t *testing.T) {
	s := NewMemoryCiStore()
	if err := s.UpdateAttrTemplate(context.Background(), &CiAttrTemplate{ID: 999, TenantID: "t1"}); err == nil {
		t.Error("expect error for missing template")
	}
}

func TestMemoryUpdateAttrTemplateTenantMismatch(t *testing.T) {
	s := NewMemoryCiStore()
	tmpl := &CiAttrTemplate{CiType: "machine", AttrKey: "ip", TenantID: "t1"}
	_ = s.CreateAttrTemplate(context.Background(), tmpl)
	if err := s.UpdateAttrTemplate(context.Background(), &CiAttrTemplate{ID: tmpl.ID, TenantID: "t2", AttrKey: "ip"}); err == nil {
		t.Error("expect error for tenant mismatch")
	}
}

func TestMemoryDeleteAttrTemplateNotFound(t *testing.T) {
	s := NewMemoryCiStore()
	if err := s.DeleteAttrTemplate(context.Background(), 999, "t1"); err == nil {
		t.Error("expect error for missing template")
	}
}

func TestMemoryDeleteAttrTemplateTenantMismatch(t *testing.T) {
	s := NewMemoryCiStore()
	tmpl := &CiAttrTemplate{CiType: "machine", AttrKey: "ip", TenantID: "t1"}
	_ = s.CreateAttrTemplate(context.Background(), tmpl)
	if err := s.DeleteAttrTemplate(context.Background(), tmpl.ID, "t2"); err == nil {
		t.Error("expect error for tenant mismatch")
	}
}

func TestMemoryCreateCiTypeDisplayNameDefault(t *testing.T) {
	s := NewMemoryCiStore()
	tp := &CiType{Name: "custom1"} // 不传 DisplayName
	if err := s.CreateCiType(context.Background(), tp); err != nil {
		t.Fatalf("CreateCiType err: %v", err)
	}
	if tp.DisplayName != "custom1" {
		t.Errorf("DisplayName should default to Name, got %s", tp.DisplayName)
	}
}

func TestMemoryCreateCiTypeNameEmpty(t *testing.T) {
	s := NewMemoryCiStore()
	if err := s.CreateCiType(context.Background(), &CiType{Name: ""}); err == nil {
		t.Error("expect error for empty name")
	}
}

func TestMemoryGetCIsByApprovalTenantFilter(t *testing.T) {
	s := NewMemoryCiStore()
	_ = s.CreateCI(context.Background(), &CiItem{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "h1", Status: "active", ApprovalStatus: ApprovalPending})
	_ = s.CreateCI(context.Background(), &CiItem{ID: "ci-2", CiType: "machine", TenantID: "t2", Name: "h2", Status: "active", ApprovalStatus: ApprovalPending})
	// t1 只能看到自己租户的 pending
	items, _ := s.GetCIsByApproval(context.Background(), ApprovalPending, "t1")
	if len(items) != 1 || items[0].ID != "ci-1" {
		t.Errorf("tenant filter wrong: %+v", items)
	}
}

func TestMemoryGetAttrTemplatesTenantFilter(t *testing.T) {
	s := NewMemoryCiStore()
	_ = s.CreateAttrTemplate(context.Background(), &CiAttrTemplate{CiType: "machine", AttrKey: "ip", TenantID: "t1"})
	_ = s.CreateAttrTemplate(context.Background(), &CiAttrTemplate{CiType: "machine", AttrKey: "ip", TenantID: "t2"})
	got, _ := s.GetAttrTemplates(context.Background(), "machine", "t1")
	if len(got) != 1 || got[0].TenantID != "t1" {
		t.Errorf("tenant filter wrong: %+v", got)
	}
}

// ===== 辅助函数 =====

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}