package cmdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// newSQLMock 构造 sqlmock + SQLCiStore（跳过 seedTypes 真实建表，手动构造）。
func newSQLMock(t *testing.T) (*SQLCiStore, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &SQLCiStore{db: db}, mock
}

// newSQLMockWithSeed 构造 sqlmock 并模拟 NewSQLCiStore 的 seedTypes 行为。
func newSQLMockWithSeed(t *testing.T) (*SQLCiStore, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// 期望 5 次 INSERT IGNORE（内置类型种子）
	mock.MatchExpectationsInOrder(false)
	mock.ExpectExec("INSERT IGNORE INTO ci_types").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT IGNORE INTO ci_types").WillReturnResult(sqlmock.NewResult(2, 1))
	mock.ExpectExec("INSERT IGNORE INTO ci_types").WillReturnResult(sqlmock.NewResult(3, 1))
	mock.ExpectExec("INSERT IGNORE INTO ci_types").WillReturnResult(sqlmock.NewResult(4, 1))
	mock.ExpectExec("INSERT IGNORE INTO ci_types").WillReturnResult(sqlmock.NewResult(5, 1))
	s := NewSQLCiStore(db)
	return s, mock
}

func TestSQLNewSQLCiStoreSeed(t *testing.T) {
	_, _ = newSQLMockWithSeed(t)
	// 构造成功即可，seedTypes 的 INSERT 已被 mock 接受
}

func TestSQLCiTypes(t *testing.T) {
	store, mock := newSQLMock(t)
	ctx := context.Background()
	now := time.Now()
	mock.ExpectQuery("SELECT id, name, display_name, builtin, created_at FROM ci_types").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "display_name", "builtin", "created_at"}).
			AddRow(1, "machine", "物理机", true, now).
			AddRow(2, "os", "操作系统", true, now))
	types, err := store.CiTypes(ctx, "")
	if err != nil {
		t.Fatalf("CiTypes err: %v", err)
	}
	if len(types) != 2 {
		t.Errorf("expect 2 types, got %d", len(types))
	}
}

func TestSQLCiTypesQueryError(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectQuery("SELECT id, name").WillReturnError(errors.New("db error"))
	_, err := store.CiTypes(context.Background(), "")
	if err == nil {
		t.Error("expect error")
	}
}

func TestSQLCiTypesScanError(t *testing.T) {
	store, mock := newSQLMock(t)
	// 列数不匹配 → scan 错误
	mock.ExpectQuery("SELECT id, name").WillReturnRows(
		sqlmock.NewRows([]string{"id", "name"}).AddRow("not-int", "machine"))
	_, err := store.CiTypes(context.Background(), "")
	if err == nil {
		t.Error("expect scan error")
	}
}

func TestSQLCreateCiType(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("INSERT INTO ci_types").WillReturnResult(sqlmock.NewResult(10, 1))
	tp := &CiType{Name: "database", DisplayName: "数据库"}
	if err := store.CreateCiType(context.Background(), tp); err != nil {
		t.Fatalf("CreateCiType err: %v", err)
	}
	if tp.ID != 10 {
		t.Errorf("expect ID 10, got %d", tp.ID)
	}
}

func TestSQLCreateCiTypeDisplayNameDefault(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("INSERT INTO ci_types").WillReturnResult(sqlmock.NewResult(11, 1))
	tp := &CiType{Name: "custom"}
	if err := store.CreateCiType(context.Background(), tp); err != nil {
		t.Fatalf("err: %v", err)
	}
	if tp.DisplayName != "custom" {
		t.Errorf("DisplayName should default to Name, got %s", tp.DisplayName)
	}
}

func TestSQLCreateCiTypeNameEmpty(t *testing.T) {
	store, _ := newSQLMock(t)
	if err := store.CreateCiType(context.Background(), &CiType{Name: ""}); err == nil {
		t.Error("expect error for empty name")
	}
}

func TestSQLCreateCiTypeExecError(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("INSERT INTO ci_types").WillReturnError(errors.New("dup"))
	if err := store.CreateCiType(context.Background(), &CiType{Name: "x"}); err == nil {
		t.Error("expect error")
	}
}

func TestSQLGetCIs(t *testing.T) {
	store, mock := newSQLMock(t)
	now := time.Now()
	attrsJSON, _ := json.Marshal(map[string]string{"ip": "1.1.1.1"})
	mock.ExpectQuery("SELECT id, ci_type").WillReturnRows(
		sqlmock.NewRows([]string{"id", "ci_type", "tenant_id", "name", "status", "approval_status", "attrs", "source", "agent_id", "device_id", "version", "created_at", "updated_at"}).
			AddRow("ci-1", "machine", "t1", "h1", "active", "approved", string(attrsJSON), "api", "a1", "d1", 1, now, now))
	items, err := store.GetCIs(context.Background(), "machine", "active", "t1")
	if err != nil {
		t.Fatalf("GetCIs err: %v", err)
	}
	if len(items) != 1 || items[0].Attrs["ip"] != "1.1.1.1" {
		t.Errorf("wrong: %+v", items)
	}
}

func TestSQLGetCIsQueryError(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectQuery("SELECT id").WillReturnError(errors.New("db err"))
	_, err := store.GetCIs(context.Background(), "", "", "")
	if err == nil {
		t.Error("expect error")
	}
}

func TestSQLGetCIsScanError(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectQuery("SELECT id").WillReturnRows(
		sqlmock.NewRows([]string{"id", "ci_type", "tenant_id", "name", "status", "approval_status", "attrs", "source", "agent_id", "device_id", "version", "created_at", "updated_at"}).
			AddRow("ci-1", "machine", "t1", "h1", "active", "approved", "{}", "api", "a1", "d1", "not-int", time.Now(), time.Now()))
	_, err := store.GetCIs(context.Background(), "", "", "")
	if err == nil {
		t.Error("expect scan error")
	}
}

func TestSQLGetCI(t *testing.T) {
	store, mock := newSQLMock(t)
	now := time.Now()
	mock.ExpectQuery("SELECT id, ci_type").WillReturnRows(
		sqlmock.NewRows([]string{"id", "ci_type", "tenant_id", "name", "status", "approval_status", "attrs", "source", "agent_id", "device_id", "version", "created_at", "updated_at"}).
			AddRow("ci-1", "machine", "t1", "h1", "active", "approved", `{"ip":"1.1.1.1"}`, "api", "a1", "d1", 1, now, now))
	ci, err := store.GetCI(context.Background(), "ci-1", "t1")
	if err != nil {
		t.Fatalf("GetCI err: %v", err)
	}
	if ci.Attrs["ip"] != "1.1.1.1" {
		t.Errorf("attr lost: %v", ci.Attrs)
	}
}

func TestSQLGetCINotFound(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectQuery("SELECT id").WillReturnError(sql.ErrNoRows)
	_, err := store.GetCI(context.Background(), "ci-missing", "t1")
	if err == nil {
		t.Error("expect not found error")
	}
}

func TestSQLGetCIScanError(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectQuery("SELECT id").WillReturnRows(
		sqlmock.NewRows([]string{"id"}).AddRow("ci-1"))
	_, err := store.GetCI(context.Background(), "ci-1", "")
	if err == nil {
		t.Error("expect scan error")
	}
}

func TestSQLCreateCI(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("INSERT INTO ci_items").WillReturnResult(sqlmock.NewResult(0, 1))
	ci := &CiItem{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "h1", Status: "active", Attrs: map[string]string{"ip": "1.1.1.1"}}
	if err := store.CreateCI(context.Background(), ci); err != nil {
		t.Fatalf("CreateCI err: %v", err)
	}
}

func TestSQLCreateCIApprovalDefault(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("INSERT INTO ci_items").WillReturnResult(sqlmock.NewResult(0, 1))
	ci := &CiItem{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "h1", Status: "active"}
	if err := store.CreateCI(context.Background(), ci); err != nil {
		t.Fatalf("err: %v", err)
	}
	if ci.ApprovalStatus != ApprovalApproved {
		t.Errorf("expect default approved, got %s", ci.ApprovalStatus)
	}
}

func TestSQLCreateCIError(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("INSERT INTO ci_items").WillReturnError(errors.New("dup key"))
	err := store.CreateCI(context.Background(), &CiItem{ID: "ci-1", CiType: "machine"})
	if err == nil {
		t.Error("expect error")
	}
}

func TestSQLUpdateCI(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("UPDATE ci_items").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := store.UpdateCI(context.Background(), &CiItem{ID: "ci-1", CiType: "machine", TenantID: "t1"}); err != nil {
		t.Fatalf("UpdateCI err: %v", err)
	}
}

func TestSQLUpdateCINotFound(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("UPDATE ci_items").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := store.UpdateCI(context.Background(), &CiItem{ID: "ci-missing", CiType: "machine"}); err == nil {
		t.Error("expect not found error")
	}
}

func TestSQLUpdateCIExecError(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("UPDATE ci_items").WillReturnError(errors.New("db err"))
	if err := store.UpdateCI(context.Background(), &CiItem{ID: "ci-1", CiType: "machine"}); err == nil {
		t.Error("expect error")
	}
}

func TestSQLDeleteCI(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("UPDATE ci_items SET status='deleted'").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := store.DeleteCI(context.Background(), "ci-1", "t1"); err != nil {
		t.Fatalf("DeleteCI err: %v", err)
	}
}

func TestSQLDeleteCINotFound(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("UPDATE ci_items SET status='deleted'").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := store.DeleteCI(context.Background(), "ci-missing", "t1"); err == nil {
		t.Error("expect not found error")
	}
}

func TestSQLDeleteCIExecError(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("UPDATE ci_items SET status='deleted'").WillReturnError(errors.New("db err"))
	if err := store.DeleteCI(context.Background(), "ci-1", "t1"); err == nil {
		t.Error("expect error")
	}
}

func TestSQLGetCIsByApproval(t *testing.T) {
	store, mock := newSQLMock(t)
	now := time.Now()
	mock.ExpectQuery("SELECT id, ci_type").WillReturnRows(
		sqlmock.NewRows([]string{"id", "ci_type", "tenant_id", "name", "status", "approval_status", "attrs", "source", "agent_id", "device_id", "version", "created_at", "updated_at"}).
			AddRow("ci-1", "machine", "t1", "h1", "active", "pending", "{}", "api", "", "", 1, now, now))
	items, err := store.GetCIsByApproval(context.Background(), "pending", "t1")
	if err != nil || len(items) != 1 {
		t.Fatalf("err: %v len=%d", err, len(items))
	}
}

func TestSQLGetCIsByApprovalError(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectQuery("SELECT id").WillReturnError(errors.New("db err"))
	_, err := store.GetCIsByApproval(context.Background(), "", "")
	if err == nil {
		t.Error("expect error")
	}
}

func TestSQLSetApproval(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("UPDATE ci_items SET approval_status").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := store.SetApproval(context.Background(), "ci-1", "t1", ApprovalApproved); err != nil {
		t.Fatalf("SetApproval err: %v", err)
	}
}

func TestSQLSetApprovalNotFound(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("UPDATE ci_items SET approval_status").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := store.SetApproval(context.Background(), "ci-missing", "t1", ApprovalApproved); err == nil {
		t.Error("expect not found error")
	}
}

func TestSQLSetApprovalExecError(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("UPDATE ci_items SET approval_status").WillReturnError(errors.New("db err"))
	if err := store.SetApproval(context.Background(), "ci-1", "t1", ApprovalApproved); err == nil {
		t.Error("expect error")
	}
}

func TestSQLGetCIHistory(t *testing.T) {
	store, mock := newSQLMock(t)
	now := time.Now()
	mock.ExpectQuery("SELECT id, ci_type").WillReturnRows(
		sqlmock.NewRows([]string{"id", "ci_type", "tenant_id", "name", "status", "approval_status", "attrs", "source", "agent_id", "device_id", "version", "created_at", "updated_at"}).
			AddRow("ci-1", "machine", "t1", "h1", "active", "approved", "{}", "api", "", "", 1, now, now))
	hist, err := store.GetCIHistory(context.Background(), "ci-1", "t1", 10)
	if err != nil || len(hist) != 1 {
		t.Fatalf("err: %v len=%d", err, len(hist))
	}
}

func TestSQLGetCIHistoryNotFound(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectQuery("SELECT id").WillReturnError(sql.ErrNoRows)
	_, err := store.GetCIHistory(context.Background(), "ci-missing", "t1", 10)
	if err == nil {
		t.Error("expect error")
	}
}

func TestSQLCreateRelation(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("INSERT INTO ci_relations").WillReturnResult(sqlmock.NewResult(5, 1))
	rel := &CiRelation{SourceCIID: "ci-1", TargetCIID: "ci-2", RelationType: "runs_on", TenantID: "t1"}
	if err := store.CreateRelation(context.Background(), rel); err != nil {
		t.Fatalf("CreateRelation err: %v", err)
	}
	if rel.ID != 5 {
		t.Errorf("expect ID 5, got %d", rel.ID)
	}
}

func TestSQLCreateRelationError(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("INSERT INTO ci_relations").WillReturnError(errors.New("db err"))
	if err := store.CreateRelation(context.Background(), &CiRelation{}); err == nil {
		t.Error("expect error")
	}
}

func TestSQLDeleteRelation(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("DELETE FROM ci_relations").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := store.DeleteRelation(context.Background(), 1, "t1"); err != nil {
		t.Fatalf("DeleteRelation err: %v", err)
	}
}

func TestSQLDeleteRelationNotFound(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("DELETE FROM ci_relations").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := store.DeleteRelation(context.Background(), 999, "t1"); err == nil {
		t.Error("expect not found error")
	}
}

func TestSQLDeleteRelationExecError(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("DELETE FROM ci_relations").WillReturnError(errors.New("db err"))
	if err := store.DeleteRelation(context.Background(), 1, "t1"); err == nil {
		t.Error("expect error")
	}
}

func TestSQLGetCIRelations(t *testing.T) {
	store, mock := newSQLMock(t)
	now := time.Now()
	mock.ExpectQuery("SELECT id, source_ci_id").WillReturnRows(
		sqlmock.NewRows([]string{"id", "source_ci_id", "target_ci_id", "relation_type", "tenant_id", "attributes", "created_at"}).
			AddRow(1, "ci-1", "ci-2", "runs_on", "t1", `{"k":"v"}`, now))
	rels, err := store.GetCIRelations(context.Background(), "ci-1", "t1")
	if err != nil || len(rels) != 1 {
		t.Fatalf("err: %v len=%d", err, len(rels))
	}
	if rels[0].Attrs["k"] != "v" {
		t.Errorf("attr lost: %v", rels[0].Attrs)
	}
}

func TestSQLGetCIRelationsError(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectQuery("SELECT id").WillReturnError(errors.New("db err"))
	_, err := store.GetCIRelations(context.Background(), "ci-1", "")
	if err == nil {
		t.Error("expect error")
	}
}

func TestSQLGetCIRelationsScanError(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectQuery("SELECT id").WillReturnRows(
		sqlmock.NewRows([]string{"id", "source_ci_id", "target_ci_id", "relation_type", "tenant_id", "attributes", "created_at"}).
			AddRow("not-int", "ci-1", "ci-2", "runs_on", "t1", "{}", time.Now()))
	_, err := store.GetCIRelations(context.Background(), "ci-1", "")
	if err == nil {
		t.Error("expect scan error")
	}
}

func TestSQLGetCIRelationGraph(t *testing.T) {
	store, mock := newSQLMock(t)
	now := time.Now()
	// GetCI for center
	mock.ExpectQuery("SELECT id, ci_type").WillReturnRows(
		sqlmock.NewRows([]string{"id", "ci_type", "tenant_id", "name", "status", "approval_status", "attrs", "source", "agent_id", "device_id", "version", "created_at", "updated_at"}).
			AddRow("ci-1", "machine", "t1", "h1", "active", "approved", "{}", "api", "", "", 1, now, now))
	// GetCIRelations
	mock.ExpectQuery("SELECT id, source_ci_id").WillReturnRows(
		sqlmock.NewRows([]string{"id", "source_ci_id", "target_ci_id", "relation_type", "tenant_id", "attributes", "created_at"}).
			AddRow(1, "ci-1", "ci-2", "runs_on", "t1", "{}", now))
	// GetCI for source name
	mock.ExpectQuery("SELECT id, ci_type").WillReturnRows(
		sqlmock.NewRows([]string{"id", "ci_type", "tenant_id", "name", "status", "approval_status", "attrs", "source", "agent_id", "device_id", "version", "created_at", "updated_at"}).
			AddRow("ci-1", "machine", "t1", "h1", "active", "approved", "{}", "api", "", "", 1, now, now))
	// GetCI for target name
	mock.ExpectQuery("SELECT id, ci_type").WillReturnRows(
		sqlmock.NewRows([]string{"id", "ci_type", "tenant_id", "name", "status", "approval_status", "attrs", "source", "agent_id", "device_id", "version", "created_at", "updated_at"}).
			AddRow("ci-2", "os", "t1", "linux", "active", "approved", "{}", "api", "", "", 1, now, now))
	g, err := store.GetCIRelationGraph(context.Background(), "ci-1", "t1")
	if err != nil {
		t.Fatalf("GetCIRelationGraph err: %v", err)
	}
	if g.CenterCI == nil || len(g.Relations) != 1 {
		t.Errorf("graph wrong: %+v", g)
	}
	if g.Relations[0].TargetName != "linux" {
		t.Errorf("target name wrong: %s", g.Relations[0].TargetName)
	}
}

func TestSQLGetCIRelationGraphCenterNotFound(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectQuery("SELECT id, ci_type").WillReturnError(sql.ErrNoRows)
	_, err := store.GetCIRelationGraph(context.Background(), "ci-missing", "t1")
	if err == nil {
		t.Error("expect error")
	}
}

func TestSQLGetCIRelationGraphRelationsError(t *testing.T) {
	store, mock := newSQLMock(t)
	now := time.Now()
	mock.ExpectQuery("SELECT id, ci_type").WillReturnRows(
		sqlmock.NewRows([]string{"id", "ci_type", "tenant_id", "name", "status", "approval_status", "attrs", "source", "agent_id", "device_id", "version", "created_at", "updated_at"}).
			AddRow("ci-1", "machine", "t1", "h1", "active", "approved", "{}", "api", "", "", 1, now, now))
	mock.ExpectQuery("SELECT id, source_ci_id").WillReturnError(errors.New("db err"))
	_, err := store.GetCIRelationGraph(context.Background(), "ci-1", "t1")
	if err == nil {
		t.Error("expect error")
	}
}

func TestSQLCreateAttrTemplate(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("INSERT INTO ci_attr_templates").WillReturnResult(sqlmock.NewResult(7, 1))
	tmpl := &CiAttrTemplate{CiType: "machine", AttrKey: "ip", TenantID: "t1"}
	if err := store.CreateAttrTemplate(context.Background(), tmpl); err != nil {
		t.Fatalf("err: %v", err)
	}
	if tmpl.ID != 7 {
		t.Errorf("expect ID 7, got %d", tmpl.ID)
	}
}

func TestSQLCreateAttrTemplateError(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("INSERT INTO ci_attr_templates").WillReturnError(errors.New("db err"))
	if err := store.CreateAttrTemplate(context.Background(), &CiAttrTemplate{}); err == nil {
		t.Error("expect error")
	}
}

func TestSQLGetAttrTemplates(t *testing.T) {
	store, mock := newSQLMock(t)
	now := time.Now()
	mock.ExpectQuery("SELECT id, ci_type").WillReturnRows(
		sqlmock.NewRows([]string{"id", "ci_type", "attr_key", "label", "attr_type", "required", "default_value", "tenant_id", "created_at"}).
			AddRow(1, "machine", "ip", "IP", "string", true, "", "t1", now))
	tmpls, err := store.GetAttrTemplates(context.Background(), "machine", "t1")
	if err != nil || len(tmpls) != 1 {
		t.Fatalf("err: %v len=%d", err, len(tmpls))
	}
}

func TestSQLGetAttrTemplatesError(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectQuery("SELECT id").WillReturnError(errors.New("db err"))
	_, err := store.GetAttrTemplates(context.Background(), "", "")
	if err == nil {
		t.Error("expect error")
	}
}

func TestSQLGetAttrTemplatesScanError(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectQuery("SELECT id").WillReturnRows(
		sqlmock.NewRows([]string{"id", "ci_type", "attr_key", "label", "attr_type", "required", "default_value", "tenant_id", "created_at"}).
			AddRow("not-int", "machine", "ip", "IP", "string", true, "", "t1", time.Now()))
	_, err := store.GetAttrTemplates(context.Background(), "", "")
	if err == nil {
		t.Error("expect scan error")
	}
}

func TestSQLUpdateAttrTemplate(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("UPDATE ci_attr_templates").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := store.UpdateAttrTemplate(context.Background(), &CiAttrTemplate{ID: 1, TenantID: "t1"}); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestSQLUpdateAttrTemplateNotFound(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("UPDATE ci_attr_templates").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := store.UpdateAttrTemplate(context.Background(), &CiAttrTemplate{ID: 999}); err == nil {
		t.Error("expect not found error")
	}
}

func TestSQLUpdateAttrTemplateExecError(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("UPDATE ci_attr_templates").WillReturnError(errors.New("db err"))
	if err := store.UpdateAttrTemplate(context.Background(), &CiAttrTemplate{ID: 1}); err == nil {
		t.Error("expect error")
	}
}

func TestSQLDeleteAttrTemplate(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("DELETE FROM ci_attr_templates").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := store.DeleteAttrTemplate(context.Background(), 1, "t1"); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestSQLDeleteAttrTemplateNotFound(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("DELETE FROM ci_attr_templates").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := store.DeleteAttrTemplate(context.Background(), 999, "t1"); err == nil {
		t.Error("expect not found error")
	}
}

func TestSQLDeleteAttrTemplateExecError(t *testing.T) {
	store, mock := newSQLMock(t)
	mock.ExpectExec("DELETE FROM ci_attr_templates").WillReturnError(errors.New("db err"))
	if err := store.DeleteAttrTemplate(context.Background(), 1, "t1"); err == nil {
		t.Error("expect error")
	}
}

// scanCI 的 approval_status NULL 分支
func TestSQLScanCIApprovalNull(t *testing.T) {
	store, mock := newSQLMock(t)
	now := time.Now()
	mock.ExpectQuery("SELECT id, ci_type").WillReturnRows(
		sqlmock.NewRows([]string{"id", "ci_type", "tenant_id", "name", "status", "approval_status", "attrs", "source", "agent_id", "device_id", "version", "created_at", "updated_at"}).
			AddRow("ci-1", "machine", "t1", "h1", "active", nil, nil, nil, nil, nil, 1, now, now))
	ci, err := store.GetCI(context.Background(), "ci-1", "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ci.ApprovalStatus != ApprovalApproved {
		t.Errorf("expect default approved for NULL, got %s", ci.ApprovalStatus)
	}
}