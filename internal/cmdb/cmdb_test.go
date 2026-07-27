package cmdb

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opsmesh/internal/authctx"
	"opsmesh/internal/proto"
)

// ===== MemoryCiStore 单元测试 =====

func newTestStore() *MemoryCiStore {
	s := NewMemoryCiStore()
	// 预置两个 CI 用于关系测试
	_ = s.CreateCI(context.Background(), &CiItem{
		ID: "ci-host-1", CiType: "machine", TenantID: "t1", Name: "host-1", Status: "active",
		Attrs: map[string]string{"ip": "10.0.0.1"},
	})
	_ = s.CreateCI(context.Background(), &CiItem{
		ID: "ci-os-1", CiType: "os", TenantID: "t1", Name: "linux", Status: "active",
		Attrs: map[string]string{"kernel": "5.4"},
	})
	return s
}

func TestCiTypesBuiltin(t *testing.T) {
	s := NewMemoryCiStore()
	types, err := s.CiTypes(context.Background(), "")
	if err != nil {
		t.Fatalf("CiTypes err: %v", err)
	}
	if len(types) != 5 {
		t.Fatalf("expect 5 builtin types, got %d", len(types))
	}
	want := map[string]bool{"machine": false, "os": false, "service": false, "app": false, "cluster": false}
	for _, ty := range types {
		if _, ok := want[ty.Name]; !ok {
			t.Errorf("unexpected type %s", ty.Name)
		}
		want[ty.Name] = true
		if !ty.Builtin {
			t.Errorf("type %s should be builtin", ty.Name)
		}
	}
}

func TestCreateAndGetCI(t *testing.T) {
	s := NewMemoryCiStore()
	ci := &CiItem{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "h1", Status: "active", Attrs: map[string]string{"ip": "1.1.1.1"}}
	if err := s.CreateCI(context.Background(), ci); err != nil {
		t.Fatalf("CreateCI err: %v", err)
	}
	got, err := s.GetCI(context.Background(), "ci-1", "t1")
	if err != nil {
		t.Fatalf("GetCI err: %v", err)
	}
	if got.Version != 1 {
		t.Errorf("expect version 1, got %d", got.Version)
	}
	if got.Attrs["ip"] != "1.1.1.1" {
		t.Errorf("attr lost: %v", got.Attrs)
	}
}

func TestCreateCIUnknownType(t *testing.T) {
	s := NewMemoryCiStore()
	err := s.CreateCI(context.Background(), &CiItem{ID: "x", CiType: "ghost", TenantID: "t1", Name: "n", Status: "active"})
	if err == nil {
		t.Fatal("expect error for unknown CI type")
	}
}

func TestUpdateCIVersionBump(t *testing.T) {
	s := NewMemoryCiStore()
	_ = s.CreateCI(context.Background(), &CiItem{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "h1", Status: "active"})
	upd := &CiItem{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "h1-renamed", Status: "active", Attrs: map[string]string{"ip": "2.2.2.2"}}
	if err := s.UpdateCI(context.Background(), upd); err != nil {
		t.Fatalf("UpdateCI err: %v", err)
	}
	got, _ := s.GetCI(context.Background(), "ci-1", "t1")
	if got.Version != 2 {
		t.Errorf("expect version 2, got %d", got.Version)
	}
	if got.Name != "h1-renamed" {
		t.Errorf("name not updated: %s", got.Name)
	}
}

func TestTenantIsolation(t *testing.T) {
	s := NewMemoryCiStore()
	_ = s.CreateCI(context.Background(), &CiItem{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "h1", Status: "active"})
	if _, err := s.GetCI(context.Background(), "ci-1", "t2"); err == nil {
		t.Fatal("tenant isolation broken: t2 can read t1 CI")
	}
}

func TestDeleteCISoft(t *testing.T) {
	s := NewMemoryCiStore()
	_ = s.CreateCI(context.Background(), &CiItem{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "h1", Status: "active"})
	if err := s.DeleteCI(context.Background(), "ci-1", "t1"); err != nil {
		t.Fatalf("DeleteCI err: %v", err)
	}
	// 软删除后仍可查到（status=deleted）
	got, err := s.GetCI(context.Background(), "ci-1", "t1")
	if err != nil {
		t.Fatalf("GetCI after delete err: %v", err)
	}
	if got.Status != "deleted" {
		t.Errorf("expect soft delete status, got %s", got.Status)
	}
	// 列表默认 active 不应再出现
	items, _ := s.GetCIs(context.Background(), "", "active", "t1")
	if len(items) != 0 {
		t.Errorf("deleted CI should not appear in active list, got %d", len(items))
	}
}

func TestRelationCRUD(t *testing.T) {
	s := newTestStore()
	if err := s.CreateRelation(context.Background(), &CiRelation{
		SourceCIID: "ci-host-1", TargetCIID: "ci-os-1", RelationType: "runs_on", TenantID: "t1",
	}); err != nil {
		t.Fatalf("CreateRelation err: %v", err)
	}
	rels, err := s.GetCIRelations(context.Background(), "ci-host-1", "t1")
	if err != nil {
		t.Fatalf("GetCIRelations err: %v", err)
	}
	if len(rels) != 1 {
		t.Fatalf("expect 1 relation, got %d", len(rels))
	}
	// 反向查询（target 侧）
	rels2, _ := s.GetCIRelations(context.Background(), "ci-os-1", "t1")
	if len(rels2) != 1 {
		t.Errorf("expect relation visible from target side, got %d", len(rels2))
	}
	// 图谱
	graph, err := s.GetCIRelationGraph(context.Background(), "ci-host-1", "t1")
	if err != nil {
		t.Fatalf("GetCIRelationGraph err: %v", err)
	}
	if graph.CenterCI == nil || graph.CenterCI.ID != "ci-host-1" {
		t.Fatal("graph center wrong")
	}
	if len(graph.Relations) != 1 {
		t.Fatalf("graph relations wrong: %d", len(graph.Relations))
	}
	if graph.Relations[0].TargetName != "linux" {
		t.Errorf("graph target name wrong: %s", graph.Relations[0].TargetName)
	}
	// 删除
	if err := s.DeleteRelation(context.Background(), rels[0].ID, "t1"); err != nil {
		t.Fatalf("DeleteRelation err: %v", err)
	}
	rels, _ = s.GetCIRelations(context.Background(), "ci-host-1", "t1")
	if len(rels) != 0 {
		t.Errorf("relation not deleted, got %d", len(rels))
	}
}

func TestAttrTemplateCRUD(t *testing.T) {
	s := NewMemoryCiStore()
	tmpl := &CiAttrTemplate{CiType: "machine", AttrKey: "ip", Label: "IP 地址", AttrType: "string", Required: true, TenantID: "t1"}
	if err := s.CreateAttrTemplate(context.Background(), tmpl); err != nil {
		t.Fatalf("CreateAttrTemplate err: %v", err)
	}
	if tmpl.ID == 0 {
		t.Fatal("template ID not assigned")
	}
	got, err := s.GetAttrTemplates(context.Background(), "machine", "t1")
	if err != nil || len(got) != 1 {
		t.Fatalf("GetAttrTemplates err: %v len=%d", err, len(got))
	}
	if got[0].AttrKey != "ip" {
		t.Errorf("wrong attr key: %s", got[0].AttrKey)
	}
	tmpl.Label = "管理 IP"
	if err := s.UpdateAttrTemplate(context.Background(), tmpl); err != nil {
		t.Fatalf("UpdateAttrTemplate err: %v", err)
	}
	got, _ = s.GetAttrTemplates(context.Background(), "machine", "t1")
	if got[0].Label != "管理 IP" {
		t.Errorf("template not updated: %s", got[0].Label)
	}
	if err := s.DeleteAttrTemplate(context.Background(), tmpl.ID, "t1"); err != nil {
		t.Fatalf("DeleteAttrTemplate err: %v", err)
	}
	got, _ = s.GetAttrTemplates(context.Background(), "machine", "t1")
	if len(got) != 0 {
		t.Errorf("template not deleted, got %d", len(got))
	}
}

func TestAttrTemplateTypeFilter(t *testing.T) {
	s := NewMemoryCiStore()
	_ = s.CreateAttrTemplate(context.Background(), &CiAttrTemplate{CiType: "machine", AttrKey: "ip", TenantID: "t1"})
	_ = s.CreateAttrTemplate(context.Background(), &CiAttrTemplate{CiType: "os", AttrKey: "kernel", TenantID: "t1"})
	got, _ := s.GetAttrTemplates(context.Background(), "os", "t1")
	if len(got) != 1 || got[0].AttrKey != "kernel" {
		t.Errorf("type filter wrong: %+v", got)
	}
}

// ===== Handler HTTP 测试 =====

func newTestHandler(store CiStore) (*Handler, *http.ServeMux) {
	h := NewHandler(store)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return h, mux
}

func TestHandlerCreateAndGetCI(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	body, _ := json.Marshal(map[string]interface{}{
		"ciType": "machine", "name": "h-test", "attrs": map[string]string{"ip": "9.9.9.9"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/ci", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create CI expect 201, got %d: %s", w.Code, w.Body.String())
	}
	var created CiItem
	json.Unmarshal(w.Body.Bytes(), &created)
	if created.ID == "" {
		t.Fatal("created CI missing ID")
	}
	// GET
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/ci/"+created.ID, nil)
	req2.Header.Set("X-Tenant-ID", "t1")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("get CI expect 200, got %d", w2.Code)
	}
	var got CiItem
	json.Unmarshal(w2.Body.Bytes(), &got)
	if got.Attrs["ip"] != "9.9.9.9" {
		t.Errorf("round-trip attr lost: %v", got.Attrs)
	}
}

func TestHandlerCIListAndTypeFilter(t *testing.T) {
	_, mux := newTestHandler(newTestStore())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/ci?type=machine", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list CI expect 200, got %d", w.Code)
	}
	var items []CiItem
	json.Unmarshal(w.Body.Bytes(), &items)
	if len(items) != 1 || items[0].CiType != "machine" {
		t.Errorf("type filter wrong: %+v", items)
	}
}

func TestHandlerRelationGraph(t *testing.T) {
	_, mux := newTestHandler(newTestStore())
	// 建关系
	body, _ := json.Marshal(CiRelation{SourceCIID: "ci-host-1", TargetCIID: "ci-os-1", RelationType: "runs_on"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/relations", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create relation expect 201, got %d: %s", w.Code, w.Body.String())
	}
	// 查图谱
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/ci/ci-host-1/graph", nil)
	req2.Header.Set("X-Tenant-ID", "t1")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("graph expect 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var g CIRelationGraph
	json.Unmarshal(w2.Body.Bytes(), &g)
	if len(g.Relations) != 1 || g.Relations[0].TargetName != "linux" {
		t.Errorf("graph content wrong: %+v", g)
	}
}

func TestHandlerAttrTemplateFlow(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	body, _ := json.Marshal(CiAttrTemplate{CiType: "machine", AttrKey: "ip", Label: "IP", AttrType: "string"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/attr-templates", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create tmpl expect 201, got %d: %s", w.Code, w.Body.String())
	}
	var tmpl CiAttrTemplate
	json.Unmarshal(w.Body.Bytes(), &tmpl)
	if tmpl.ID == 0 {
		t.Fatal("tmpl ID not set")
	}
	// list
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/attr-templates", nil)
	req2.Header.Set("X-Tenant-ID", "t1")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("list tmpl expect 200, got %d", w2.Code)
	}
}

func TestHandlerMethodNotAllowed(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/cmdb/types", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expect 405, got %d", w.Code)
	}
}

func TestHandleReportAgentUpsert(t *testing.T) {
	s := NewMemoryCiStore()
	h := NewHandler(s)
	report := &proto.CmdbReport{
		CiType: "machine",
		Attrs:  []proto.CmdbAttr{{Key: "hostname", Value: "agent-host"}, {Key: "os", Value: "linux"}},
	}
	h.HandleReport(context.Background(), "agent-A", report)
	items, _ := s.GetCIs(context.Background(), "machine", "active", "")
	if len(items) != 1 {
		t.Fatalf("expect 1 CI after report, got %d", len(items))
	}
	if items[0].AgentID != "agent-A" || items[0].Source != "agent" {
		t.Errorf("CI metadata wrong: %+v", items[0])
	}
	if items[0].Attrs["hostname"] != "agent-host" {
		t.Errorf("attr not applied: %v", items[0].Attrs)
	}
	// 再次上报同一 agent → 更新而非新建
	h.HandleReport(context.Background(), "agent-A", &proto.CmdbReport{
		CiType: "machine", Attrs: []proto.CmdbAttr{{Key: "os", Value: "linux-updated"}},
	})
	items2, _ := s.GetCIs(context.Background(), "machine", "active", "")
	if len(items2) != 1 {
		t.Fatalf("upsert broke: expect 1 CI, got %d", len(items2))
	}
	if items2[0].Attrs["os"] != "linux-updated" || items2[0].Attrs["hostname"] != "agent-host" {
		t.Errorf("upsert merge wrong: %v", items2[0].Attrs)
	}
}

func TestAuthCtxFromHeader(t *testing.T) {
	// 验证 handler 读取租户头，防止回归
	h, mux := newTestHandler(NewMemoryCiStore())
	body, _ := json.Marshal(map[string]interface{}{"ciType": "machine", "name": "x"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/ci", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "tenant-xyz")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create expect 201 got %d", w.Code)
	}
	// 其他租户不应读到
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/ci", nil)
	req2.Header.Set("X-Tenant-ID", "other")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)
	var items []CiItem
	json.Unmarshal(w2.Body.Bytes(), &items)
	if len(items) != 0 {
		t.Errorf("tenant leak: other tenant sees %d CIs", len(items))
	}
	_ = h
}

func TestGraphEdgesContainRelation(t *testing.T) {
	// 验证 CIRelationGraph 中 RelationType 透传
	s := newTestStore()
	_ = s.CreateRelation(context.Background(), &CiRelation{SourceCIID: "ci-host-1", TargetCIID: "ci-os-1", RelationType: "depends", TenantID: "t1"})
	g, err := s.GetCIRelationGraph(context.Background(), "ci-host-1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if g.Relations[0].RelationType != "depends" {
		t.Errorf("relation type lost: %s", g.Relations[0].RelationType)
	}
}

// ===== Phase-3: 自定义类型 / 导入导出 / 审批流 =====

func TestCreateCustomCiType(t *testing.T) {
	s := NewMemoryCiStore()
	tp := &CiType{Name: "database", DisplayName: "数据库"}
	if err := s.CreateCiType(context.Background(), tp); err != nil {
		t.Fatalf("CreateCiType err: %v", err)
	}
	if tp.Builtin {
		t.Error("custom type must not be builtin")
	}
	if tp.ID == 0 {
		t.Error("custom type ID not assigned")
	}
	// 重名应报错
	if err := s.CreateCiType(context.Background(), &CiType{Name: "database"}); err == nil {
		t.Error("duplicate type should error")
	}
	types, _ := s.CiTypes(context.Background(), "")
	found := false
	for _, ty := range types {
		if ty.Name == "database" && !ty.Builtin {
			found = true
		}
	}
	if !found {
		t.Error("custom type not listed")
	}
	// 未知类型创建 CI 应被拒（保证导入/创建的健壮性）
	if err := s.CreateCI(context.Background(), &CiItem{ID: "x", CiType: "nope", TenantID: "t1", Name: "n", Status: "active"}); err == nil {
		t.Error("CI with unknown type should be rejected")
	}
}

func TestApprovalFlowStore(t *testing.T) {
	s := NewMemoryCiStore()
	ci := &CiItem{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "h1", Status: "active"}
	if err := s.CreateCI(context.Background(), ci); err != nil {
		t.Fatalf("CreateCI err: %v", err)
	}
	// 默认 approved
	got, _ := s.GetCI(context.Background(), "ci-1", "t1")
	if got.ApprovalStatus != ApprovalApproved {
		t.Fatalf("expect default approved, got %s", got.ApprovalStatus)
	}
	// 置为 pending
	if err := s.SetApproval(context.Background(), "ci-1", "t1", ApprovalPending); err != nil {
		t.Fatalf("SetApproval err: %v", err)
	}
	pend, err := s.GetCIsByApproval(context.Background(), ApprovalPending, "t1")
	if err != nil || len(pend) != 1 {
		t.Fatalf("pending list err: %v len=%d", err, len(pend))
	}
	// 审批通过
	if err := s.SetApproval(context.Background(), "ci-1", "t1", ApprovalApproved); err != nil {
		t.Fatalf("approve err: %v", err)
	}
	pend, _ = s.GetCIsByApproval(context.Background(), ApprovalPending, "t1")
	if len(pend) != 0 {
		t.Errorf("pending list should be empty after approve, got %d", len(pend))
	}
}

func TestHandlerCustomTypeAndApproval(t *testing.T) {
	_, mux := newTestHandler(NewMemoryCiStore())
	// 创建自定义类型
	body, _ := json.Marshal(CiType{Name: "database", DisplayName: "数据库"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/types", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create type expect 201, got %d: %s", w.Code, w.Body.String())
	}
	// 用自定义类型创建 CI → 应进入 pending
	body2, _ := json.Marshal(map[string]interface{}{"ciType": "database", "name": "db-1", "attrs": map[string]string{"ip": "1.2.3.4"}})
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/ci", bytes.NewReader(body2))
	req2.Header.Set("X-Tenant-ID", "t1")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)
	if w2.Code != http.StatusCreated {
		t.Fatalf("create CI expect 201, got %d: %s", w2.Code, w2.Body.String())
	}
	var created CiItem
	json.Unmarshal(w2.Body.Bytes(), &created)
	if created.ApprovalStatus != ApprovalPending {
		t.Fatalf("new CI should be pending, got %s", created.ApprovalStatus)
	}
	// 待审列表应包含它
	reqp := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/ci/pending", nil)
	reqp.Header.Set("X-Tenant-ID", "t1")
	wp := httptest.NewRecorder()
	mux.ServeHTTP(wp, reqp)
	if wp.Code != http.StatusOK {
		t.Fatalf("pending list expect 200, got %d", wp.Code)
	}
	var pend []CiItem
	json.Unmarshal(wp.Body.Bytes(), &pend)
	if len(pend) != 1 || pend[0].ID != created.ID {
		t.Errorf("pending list wrong: %+v", pend)
	}
	// 审批通过
	reqa := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/ci/"+created.ID+"/approve", nil)
	reqa.Header.Set("X-Tenant-ID", "t1")
	wa := httptest.NewRecorder()
	mux.ServeHTTP(wa, reqa)
	if wa.Code != http.StatusOK {
		t.Fatalf("approve expect 200, got %d: %s", wa.Code, wa.Body.String())
	}
	// 待审列表应清空
	wp2 := httptest.NewRecorder()
	mux.ServeHTTP(wp2, reqp)
	var pend2 []CiItem
	json.Unmarshal(wp2.Body.Bytes(), &pend2)
	if len(pend2) != 0 {
		t.Errorf("pending list should be empty after approve, got %d", len(pend2))
	}
}

func TestHandlerExportImportJSON(t *testing.T) {
	_, mux := newTestHandler(newTestStore())
	// 导出 JSON
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/ci/export", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("export expect 200, got %d: %s", w.Code, w.Body.String())
	}
	var exported []CiItem
	json.Unmarshal(w.Body.Bytes(), &exported)
	if len(exported) == 0 {
		t.Fatal("export returned empty")
	}
	// 导入 JSON（新租户）
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/ci/import", bytes.NewReader(w.Body.Bytes()))
	req2.Header.Set("X-Tenant-ID", "t2")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("import expect 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var summary map[string]interface{}
	json.Unmarshal(w2.Body.Bytes(), &summary)
	if summary["created"] == nil || int(summary["created"].(float64)) == 0 {
		t.Errorf("import created count wrong: %+v", summary)
	}
}

func TestHandlerExportCSV(t *testing.T) {
	_, mux := newTestHandler(newTestStore())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/ci/export?format=csv", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("export csv expect 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Errorf("expect csv content type, got %s", ct)
	}
	// CSV 至少含表头 + 数据行
	lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("csv too short: %q", w.Body.String())
	}
	if !strings.Contains(lines[0], "id") || !strings.Contains(lines[0], "ciType") {
		t.Errorf("csv header wrong: %s", lines[0])
	}
}

// 防止 authctx 包未使用告警：直接构造验证
func TestAuthCtxHelper(t *testing.T) {
	hdr := http.Header{}
	hdr.Set("X-Tenant-ID", "t-1")
	hdr.Set("X-User-ID", "u-1")
	hdr.Set("X-User-Roles", "admin")
	actx := authctx.FromHTTPHeader(hdr)
	if actx.TenantID != "t-1" || actx.UserID != "u-1" || len(actx.Roles) != 1 || actx.Roles[0] != "admin" {
		t.Errorf("authctx parse wrong: %+v", actx)
	}
}
