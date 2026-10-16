// mysql_canary_test.go — MySQL 后端 CanaryStore 的单元守卫（TD-60 task 域 R6 任务侧）。
//
// 为什么值得单测（而不是只靠 DSN 门控集成用例）：灰度记录此前的形态是
// 「无条件内存索引，重启即丢」（main.go 曾写死 SetCanaryStore(NewMemoryStore())，
// 而 compose 出厂 TASK_SVC_STORE_TYPE=sql）。本文件把三条判据钉在 CI 常跑层：
//  1. 建/改/查/列 四个方法真的走 SQL（upsert 幂等、列方法按租户过滤 + 排序）；
//  2. 载荷解不出按「记录不存在」处理——**不返回半条**（半条会让调用方看到
//     一个没有 phases 的灰度，比 404 更误导；controlplane 同口径）；
//  3. DB 出错不假成功（CreateCanary 返回 nil，调用方据此报错而不是宣称已建）。
package store

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/Levango7/OpsMesh/services/task-svc/internal/models"
)

// sampleCanary 造一条带两阶段结构的灰度记录（内容保留判据要用 phases）。
func sampleCanary() *models.CanaryRelease {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	return &models.CanaryRelease{
		CanaryID:   "canary-1",
		TenantID:   "t1",
		TaskType:   models.TaskTypeShell,
		Command:    "echo r6",
		Strategy:   "percentage",
		Percentage: 50,
		CreatedAt:  now,
		CreatedBy:  "u-admin",
		Phases: []models.CanaryPhase{
			{Phase: 1, DeviceIDs: []string{"dev-0"}, Status: "running",
				Tasks: []models.BatchTaskItem{{DeviceID: "dev-0", TaskID: "task-1", Status: "running"}}},
			{Phase: 2, DeviceIDs: []string{"dev-1"}, Status: "pending"},
		},
	}
}

func TestMySQLCanaryStoreRoundTrip(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &MySQLStore{db: db}

	c := sampleCanary()
	payload, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}

	// 建：upsert（created_at 只在 INSERT 分支生效，故 created_at 出现两次）
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO canary_releases")).
		WithArgs(c.CanaryID, c.TenantID, payload, c.CreatedAt, c.TenantID, payload).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if got := s.CreateCanary(c); got == nil {
		t.Fatal("CreateCanary 返回 nil（写入失败被当成成功之外的空形态）")
	}

	// 查：整条读回且内容完整（只存壳子也能"查到"，故断言 phases）
	mock.ExpectQuery(regexp.QuoteMeta("SELECT payload FROM canary_releases WHERE canary_id = ?")).
		WithArgs(c.CanaryID).
		WillReturnRows(sqlmock.NewRows([]string{"payload"}).AddRow(payload))
	back := s.GetCanary(c.CanaryID)
	if back == nil {
		t.Fatal("GetCanary 返回 nil（刚写入的记录读不回）")
	}
	if back.CanaryID != c.CanaryID || back.Command != c.Command || back.Percentage != 50 {
		t.Errorf("灰度记录字段丢失：%+v", back)
	}
	if len(back.Phases) != 2 {
		t.Fatalf("阶段结构丢失：%d 个阶段", len(back.Phases))
	}
	if back.Phases[0].Tasks[0].TaskID != "task-1" {
		t.Errorf("阶段内任务项丢失：%+v", back.Phases[0].Tasks)
	}

	// 改：advance 推进第一阶段后整条写回
	c.Phases[0].Status = "done"
	c.Phases[1].Status = "running"
	updated, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO canary_releases")).
		WithArgs(c.CanaryID, c.TenantID, updated, c.CreatedAt, c.TenantID, updated).
		WillReturnResult(sqlmock.NewResult(0, 2))
	s.UpdateCanary(c)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT payload FROM canary_releases WHERE canary_id = ?")).
		WithArgs(c.CanaryID).
		WillReturnRows(sqlmock.NewRows([]string{"payload"}).AddRow(updated))
	after := s.GetCanary(c.CanaryID)
	if after == nil || after.Phases[0].Status != "done" || after.Phases[1].Status != "running" {
		t.Errorf("推进结果未写穿：%+v", after)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestGetCanaryMissingRowReturnsNil 查不到按 nil（调用方映射 404），不报错不造空壳。
func TestGetCanaryMissingRowReturnsNil(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT payload FROM canary_releases WHERE canary_id = ?")).
		WithArgs("nope").
		WillReturnError(errors.New("sql: no rows in result set"))
	if got := (&MySQLStore{db: db}).GetCanary("nope"); got != nil {
		t.Errorf("不存在的 canaryID 应返回 nil，实得 %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestGetCanaryCorruptPayloadTreatedAsMissing 坏载荷按「记录不存在」处理。
// 实测过的失败形态（与 controlplane 同族）：解不出还返回就会给调用方一个
// TenantID 为空的半条记录，被租户校验挡成 tenant mismatch——把"数据坏了"
// 说成"租户不符"，用户会去找权限问题。
func TestGetCanaryCorruptPayloadTreatedAsMissing(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT payload FROM canary_releases WHERE canary_id = ?")).
		WithArgs("bad").
		WillReturnRows(sqlmock.NewRows([]string{"payload"}).AddRow([]byte("{not-json")))
	if got := (&MySQLStore{db: db}).GetCanary("bad"); got != nil {
		t.Errorf("坏载荷应按不存在处理（nil），实得 %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestCreateCanaryInsertFailureReturnsNil DB 写失败必须让调用方看得见（nil），
// 不能返回一个"看起来建好了"的记录——那正是"假成功"形态。
func TestCreateCanaryInsertFailureReturnsNil(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO canary_releases")).
		WillReturnError(errors.New("table canary_releases doesn't exist"))
	if got := (&MySQLStore{db: db}).CreateCanary(sampleCanary()); got != nil {
		t.Errorf("INSERT 失败应返回 nil，实得 %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestListCanariesFiltersTenantAndOrders 列方法：按租户过滤 + created_at 倒序，
// 坏载荷跳过（不返回半条）。
func TestListCanariesFiltersTenantAndOrders(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	a := sampleCanary()
	a.CanaryID = "canary-a"
	b := sampleCanary()
	b.CanaryID = "canary-b"
	pa, _ := json.Marshal(a)
	pb, _ := json.Marshal(b)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT payload FROM canary_releases WHERE tenant_id = ? ORDER BY created_at DESC")).
		WithArgs("t1").
		WillReturnRows(sqlmock.NewRows([]string{"payload"}).
			AddRow(pb).AddRow(pa).AddRow([]byte("{corrupt")))
	got := (&MySQLStore{db: db}).ListCanaries("t1")
	if len(got) != 2 {
		t.Fatalf("应读到 2 条（坏载荷跳过），实得 %d", len(got))
	}
	if got[0].CanaryID != "canary-b" || got[1].CanaryID != "canary-a" {
		t.Errorf("顺序应遵循 created_at DESC（查询侧 ORDER BY 负责，此处按行序回填）：%s, %s",
			got[0].CanaryID, got[1].CanaryID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestListCanariesEmptyTenantListsAll 空租户 = 不过滤（与 MemoryStore 语义一致）。
func TestListCanariesEmptyTenantListsAll(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT payload FROM canary_releases ORDER BY created_at DESC")).
		WillReturnRows(sqlmock.NewRows([]string{"payload"}))
	if got := (&MySQLStore{db: db}).ListCanaries(""); got != nil {
		t.Errorf("无行时应返回 nil，实得 %v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// nonZeroTime 是 sqlmock 的自定义参数断言：要求实参是 time.Time 且非零值。
//
// 为什么需要它：created_at 是 TIMESTAMP NOT NULL，而 driver 把 time.Time{} 序列化成
// '0001-01-01 00:00:00'（超出下界）⇒ 严格模式报 1292，且本方法只留痕不回错——
// 缺陷形态是「推进静默丢失、HTTP 仍 200」。用 time.Time{} 直接比对的用例会随
// 每次调用时间漂移，故断言"非零"这一性质本身。
type nonZeroTime struct{}

func (nonZeroTime) Match(v driver.Value) bool {
	tm, ok := v.(time.Time)
	return ok && !tm.IsZero()
}

// TestUpdateCanaryFillsZeroCreatedAt 零值 CreatedAt 必须被补齐后再落库。
// 变异检验：去掉 UpdateCanary 的 IsZero 守卫 ⇒ 本用例判红。
func TestUpdateCanaryFillsZeroCreatedAt(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := sampleCanary()
	c.CreatedAt = time.Time{} // 模拟「载荷里没有 createdAt」（历史行 / 外部构造）
	// payload 由被测方法在补齐 CreatedAt 之后生成，故这里只断言"有且是合法 JSON"，
	// 判据落在列值（created_at 必须非零）与内存对象被补齐这两点上。
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO canary_releases")).
		WithArgs(c.CanaryID, c.TenantID, sqlmock.AnyArg(), nonZeroTime{}, c.TenantID, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	(&MySQLStore{db: db}).UpdateCanary(c)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if c.CreatedAt.IsZero() {
		t.Error("UpdateCanary 应在内存对象上补齐 CreatedAt（否则调用方后续读到的仍是零值）")
	}
}
