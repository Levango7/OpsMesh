// audit_chain_test.go 测试 P1-3 审计哈希链（防篡改）+ 归档保留。
//
// 测试分两层：
//  1. 纯逻辑层（无需 MySQL，始终运行）：auditEntryHash 的字段边界/时间归一、
//     verifyChainRows 的篡改/删除/换序/缺失哈希判定；
//  2. 集成层（需真实 MySQL，默认跳过）：链式写入自洽、篡改与尾部删除可检出、
//     租户窗口隔离、归档搬运 + 边界哈希 + 链头回退。
//     OPSMESH_TEST_MYSQL_DSN="user:pass@tcp(127.0.0.1:3306)/opsmesh?parseTime=true" \
//     go test ./internal/store/ -run TestAuditChain -v
package store

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// ============================================================================
// 纯逻辑层：auditEntryHash
// ============================================================================

// TestAuditEntryHash_FieldBoundaryAmbiguity 验证长度前缀消除了字段拼接歧义：
// 朴素拼接下 ("ab","c") 与 ("a","bc") 得到同一串，长度前缀必须让二者不同。
func TestAuditEntryHash_FieldBoundaryAmbiguity(t *testing.T) {
	base := time.Unix(1700000000, 0).UTC()
	e1 := proto.AuditEvent{TenantID: "ab", UserID: "c", CreatedAt: base}
	e2 := proto.AuditEvent{TenantID: "a", UserID: "bc", CreatedAt: base}
	if auditEntryHash("", &e1) == auditEntryHash("", &e2) {
		t.Fatal("字段边界不同的两条事件得到相同哈希（长度前缀失效）")
	}
}

// TestAuditEntryHash_TimeSecondPrecision 验证时间以秒精度参与哈希：
// MySQL DATETIME 无小数秒，若用纳秒值计算，回读复算必然对不上（自校验恒失败）。
func TestAuditEntryHash_TimeSecondPrecision(t *testing.T) {
	base := time.Unix(1700000000, 0).UTC()
	withNanos := base.Add(987654321 * time.Nanosecond)
	a := proto.AuditEvent{TenantID: "t", Action: "a", CreatedAt: withNanos}
	b := proto.AuditEvent{TenantID: "t", Action: "a", CreatedAt: withNanos.Truncate(time.Second)}
	if auditEntryHash("", &a) != auditEntryHash("", &b) {
		t.Fatal("同秒内不同纳秒值产生了不同哈希（时间未做秒级归一，回读复算必然失败）")
	}
	// 跨秒必须不同（否则秒级归一过度，时间被抹掉）。
	c := proto.AuditEvent{TenantID: "t", Action: "a", CreatedAt: withNanos.Add(time.Second)}
	if auditEntryHash("", &a) == auditEntryHash("", &c) {
		t.Fatal("跨秒的两次事件得到相同哈希（时间归一过度）")
	}
}

// TestAuditEntryHash_PrevChained 验证 prev 参与哈希：前驱不同则哈希不同（否则链无意义）。
func TestAuditEntryHash_PrevChained(t *testing.T) {
	e := proto.AuditEvent{TenantID: "t", Action: "a", CreatedAt: time.Unix(1700000000, 0).UTC()}
	h1 := auditEntryHash("", &e)
	h2 := auditEntryHash("prev-hash", &e)
	if h1 == h2 {
		t.Fatal("prev_hash 未参与哈希计算")
	}
}

// ============================================================================
// 纯逻辑层：verifyChainRows
// ============================================================================

// chainRows 构造 n 行自洽的链式行（id 从 1 开始），供篡改用例做基线。
func chainRows(n int, base time.Time) []auditChainRow {
	rows := make([]auditChainRow, 0, n)
	prev := auditChainGenesis
	for i := 0; i < n; i++ {
		e := proto.AuditEvent{
			TenantID: "t1", UserID: "u1", Action: "act",
			Target: fmt.Sprintf("target-%d", i), Detail: fmt.Sprintf("detail-%d", i),
			CreatedAt: base.Add(time.Duration(i) * time.Second), TraceID: "trace-1",
		}
		h := auditEntryHash(prev, &e)
		rows = append(rows, auditChainRow{ID: int64(i + 1), Event: e, PrevHash: prev, EntryHash: h})
		prev = h
	}
	return rows
}

func TestVerifyChainRows_HappyPath(t *testing.T) {
	rows := chainRows(5, time.Unix(1700000000, 0).UTC())
	bad, reason := verifyChainRows(rows, auditChainGenesis)
	if bad != -1 {
		t.Fatalf("自洽链被判为不一致：bad=%d reason=%s", bad, reason)
	}
}

func TestVerifyChainRows_ContentTampered(t *testing.T) {
	rows := chainRows(5, time.Unix(1700000000, 0).UTC())
	rows[2].Event.Detail = "被改过的内容"
	bad, reason := verifyChainRows(rows, auditChainGenesis)
	if bad != 2 {
		t.Fatalf("内容被改写应定位到第 3 行（idx=2）；got bad=%d reason=%s", bad, reason)
	}
	if !strings.Contains(reason, "entry_hash") {
		t.Fatalf("原因应指出 entry_hash 不一致；got %q", reason)
	}
}

func TestVerifyChainRows_DeletedMiddleRow(t *testing.T) {
	rows := chainRows(5, time.Unix(1700000000, 0).UTC())
	trimmed := append(append([]auditChainRow{}, rows[:2]...), rows[3:]...)
	bad, reason := verifyChainRows(trimmed, auditChainGenesis)
	if bad != 2 {
		t.Fatalf("删除中间行后应在后继行（id=4，idx=2）检出断链；got bad=%d reason=%s", bad, reason)
	}
	if !strings.Contains(reason, "prev_hash") {
		t.Fatalf("原因应指出 prev_hash 不一致；got %q", reason)
	}
}

func TestVerifyChainRows_Reordered(t *testing.T) {
	rows := chainRows(4, time.Unix(1700000000, 0).UTC())
	rows[1], rows[2] = rows[2], rows[1]
	bad, _ := verifyChainRows(rows, auditChainGenesis)
	if bad != 1 {
		t.Fatalf("换序应在首个被换的行（idx=1）检出；got bad=%d", bad)
	}
}

func TestVerifyChainRows_MissingEntryHash(t *testing.T) {
	rows := chainRows(3, time.Unix(1700000000, 0).UTC())
	rows[1].EntryHash = ""
	bad, reason := verifyChainRows(rows, auditChainGenesis)
	if bad != 1 || !strings.Contains(reason, "entry_hash") {
		t.Fatalf("缺 entry_hash 应报错在第 2 行；got bad=%d reason=%q", bad, reason)
	}
}

// TestVerifyChainRows_ExpectedPrevMismatch 验证「窗口首行与前驱（前一行/归档边界/创世）」
// 的链接判定：期望前驱不同即断链。
func TestVerifyChainRows_ExpectedPrevMismatch(t *testing.T) {
	rows := chainRows(3, time.Unix(1700000000, 0).UTC())
	if bad, _ := verifyChainRows(rows, rows[0].PrevHash); bad != -1 {
		t.Fatalf("期望前驱=创世时应通过；got bad=%d", bad)
	}
	bad, reason := verifyChainRows(rows, "不存在的边界哈希")
	if bad != 0 {
		t.Fatalf("期望前驱不匹配应在首行检出；got bad=%d reason=%s", bad, reason)
	}
}

// ============================================================================
// 纯逻辑层：verifyChainRowsScoped（租户视图）
// ============================================================================

// TestVerifyChainRowsScoped_InterleavedTenants 复现真实多租户交错场景：
// 链是一条跨租户的链，同一租户的相邻窗口行之间夹着其他租户的行（id 不相邻），
// 此时不得因「prev_hash 不等于窗口内前一行的哈希」而误报篡改。
func TestVerifyChainRowsScoped_InterleavedTenants(t *testing.T) {
	all := chainRows(4, time.Unix(1700000000, 0).UTC())
	// 取 id=1 与 id=3（隔了 id=2，属于其他租户）作为某租户的窗口。
	win := []auditChainRow{all[0], all[2]}
	if bad, reason := verifyChainRowsScoped(win, auditChainGenesis); bad != -1 {
		t.Fatalf("跨租户交错窗口不应误报：bad=%d reason=%s", bad, reason)
	}
	// 严格校验同样窗口会报错——正是它不能用于租户视图的原因（对照断言）。
	if bad, _ := verifyChainRows(win, auditChainGenesis); bad < 0 {
		t.Fatal("严格校验本应对交错窗口报错（对照组失效，测试前提不成立）")
	}
}

// TestVerifyChainRowsScoped_ContentTampered 租户视图仍须检出内容改写。
func TestVerifyChainRowsScoped_ContentTampered(t *testing.T) {
	all := chainRows(4, time.Unix(1700000000, 0).UTC())
	win := []auditChainRow{all[0], all[2]}
	win[1].Event.Detail = "被改过"
	if bad, reason := verifyChainRowsScoped(win, auditChainGenesis); bad != 1 {
		t.Fatalf("内容改写应在 idx=1 检出；got bad=%d reason=%s", bad, reason)
	}
}

// TestVerifyChainRowsScoped_PrevTampered 验证 prev_hash 被改也能检出（自洽性覆盖 prev）。
func TestVerifyChainRowsScoped_PrevTampered(t *testing.T) {
	all := chainRows(3, time.Unix(1700000000, 0).UTC())
	win := []auditChainRow{all[0], all[2]}
	win[1].PrevHash = "被改过的前驱"
	if bad, _ := verifyChainRowsScoped(win, auditChainGenesis); bad != 1 {
		t.Fatalf("prev_hash 被改应在 idx=1 检出；got bad=%d", bad)
	}
}

// TestVerifyChainRowsScoped_AdjacentLinkChecked 验证 id 相邻的两行仍做链接校验
// （相邻即中间无行，链接必须成立）。
func TestVerifyChainRowsScoped_AdjacentLinkChecked(t *testing.T) {
	all := chainRows(3, time.Unix(1700000000, 0).UTC())
	// 相邻两行但把前一行的 entry_hash 改成别的（模拟换序/改写后自洽但断链）。
	win := []auditChainRow{all[0], all[1]}
	win[0].EntryHash = strings.Repeat("f", 64)
	if bad, reason := verifyChainRowsScoped(win, auditChainGenesis); bad < 0 {
		t.Fatalf("相邻行断链应被检出；got bad=%d reason=%s", bad, reason)
	}
}

// TestVerifyChainRowsScoped_FirstRowBoundary 验证首行边界：首行 prev_hash 必须等于
// 窗口前驱（在线前一行/归档段前一行/创世）。
func TestVerifyChainRowsScoped_FirstRowBoundary(t *testing.T) {
	all := chainRows(3, time.Unix(1700000000, 0).UTC())
	win := []auditChainRow{all[1], all[2]} // 窗口从 id=2 开始，其前驱应为 all[0].EntryHash
	if bad, _ := verifyChainRowsScoped(win, all[0].EntryHash); bad != -1 {
		t.Fatalf("首行与前驱链接正确时应通过；got bad=%d", bad)
	}
	if bad, reason := verifyChainRowsScoped(win, "错误的边界哈希"); bad != 0 {
		t.Fatalf("首行边界不匹配应在 idx=0 检出；got bad=%d reason=%s", bad, reason)
	}
}

// ============================================================================
// 内存后端：不支持链式校验 + 保留策略
// ============================================================================

func TestMemoryStore_VerifyAuditChainUnsupported(t *testing.T) {
	m := NewMemoryStore()
	m.Audit(&proto.AuditEvent{TenantID: "t1", Action: "a"})
	res, err := m.VerifyAuditChain("t1", 100)
	if err != nil {
		t.Fatalf("内存后端校验不应报错: %v", err)
	}
	if res == nil || res.Supported {
		t.Fatalf("内存后端必须如实回报 Supported=false；got %+v", res)
	}
	if res.Note == "" {
		t.Fatal("内存后端应给出说明性 Note（避免被误读为「校验通过」）")
	}
}

func TestMemoryStore_ArchiveAuditLog(t *testing.T) {
	m := NewMemoryStore()
	now := time.Now()
	m.Audit(&proto.AuditEvent{TenantID: "t1", Action: "old1", CreatedAt: now.AddDate(0, 0, -40)})
	m.Audit(&proto.AuditEvent{TenantID: "t1", Action: "old2", CreatedAt: now.AddDate(0, 0, -31)})
	m.Audit(&proto.AuditEvent{TenantID: "t1", Action: "fresh", CreatedAt: now.Add(-time.Minute)})

	// retainDays<=0：永久保留，不做任何事。
	if n, err := m.ArchiveAuditLog(0, 100); err != nil || n != 0 {
		t.Fatalf("retainDays=0 应为空操作；got n=%d err=%v", n, err)
	}
	if got := len(m.Audits()); got != 3 {
		t.Fatalf("retainDays=0 不应删除事件；got %d 条", got)
	}
	// retainDays=30：丢弃 40 天前与 31 天前的事件，保留 1 分钟前的。
	n, err := m.ArchiveAuditLog(30, 100)
	if err != nil {
		t.Fatalf("归档报错: %v", err)
	}
	if n != 2 {
		t.Fatalf("应归档 2 条超龄事件；got %d", n)
	}
	rest := m.Audits()
	if len(rest) != 1 || rest[0].Action != "fresh" {
		t.Fatalf("归档后应仅剩 fresh；got %+v", rest)
	}
}

// ============================================================================
// 多租户 schema 隔离：审计链校验/归档的聚合与路由
// ============================================================================

// TestMultiSchemaStore_VerifyAuditChain_UnsupportedAggregate 验证平台级（tenant 为空）
// 询所有 schema 聚合：内存 schema 如实回报不支持，且不被当成「校验通过」。
func TestMultiSchemaStore_VerifyAuditChain_UnsupportedAggregate(t *testing.T) {
	m, _ := newTestMultiSchema()
	m.Audit(&proto.AuditEvent{TenantID: "tA", Action: "a"})
	m.Audit(&proto.AuditEvent{TenantID: "tB", Action: "b"})

	res, err := m.VerifyAuditChain("", 100)
	if err != nil {
		t.Fatalf("聚合校验不应报错: %v", err)
	}
	if res == nil || res.Supported || res.OK {
		t.Fatalf("全内存 schema 应回报 Supported=false / OK=false；got %+v", res)
	}
	if !strings.Contains(res.Note, "不提供链式校验") {
		t.Fatalf("Note 应说明不支持；got %q", res.Note)
	}
	// 租户路由：tA 的校验路由到 tA 的 schema（内存 → 同样不支持，但 Note 来自该 schema）。
	resA, err := m.VerifyAuditChain("tA", 100)
	if err != nil {
		t.Fatalf("租户校验不应报错: %v", err)
	}
	if resA.Supported {
		t.Fatalf("内存 schema 不应回报支持；got %+v", resA)
	}
}

// TestMultiSchemaStore_ArchiveAuditLog 验证归档逐 schema 转发（内存 schema 只保留在保留期内的事件）。
func TestMultiSchemaStore_ArchiveAuditLog(t *testing.T) {
	m, _ := newTestMultiSchema()
	now := time.Now()
	m.Audit(&proto.AuditEvent{TenantID: "tA", Action: "old", CreatedAt: now.AddDate(0, 0, -40)})
	m.Audit(&proto.AuditEvent{TenantID: "tA", Action: "fresh", CreatedAt: now})
	m.Audit(&proto.AuditEvent{TenantID: "tB", Action: "old", CreatedAt: now.AddDate(0, 0, -40)})

	if n, err := m.ArchiveAuditLog(0, 100); err != nil || n != 0 {
		t.Fatalf("retainDays=0 应为空操作；got n=%d err=%v", n, err)
	}
	n, err := m.ArchiveAuditLog(30, 100)
	if err != nil {
		t.Fatalf("归档报错: %v", err)
	}
	if n != 2 {
		t.Fatalf("两个 schema 各应归档 1 条超龄事件（共 2）；got %d", n)
	}
	if got := m.QueryAudits("tA", "", time.Time{}, time.Time{}, 0); len(got) != 1 || got[0].Action != "fresh" {
		t.Fatalf("tA 归档后应仅剩 fresh；got %+v", got)
	}
	if got := m.QueryAudits("tB", "", time.Time{}, time.Time{}, 0); len(got) != 0 {
		t.Fatalf("tB 的旧事件应被归档；got %+v", got)
	}
}

// ============================================================================
// 集成层：真实 MySQL（OPSMESH_TEST_MYSQL_DSN）
// ============================================================================

// writeChainedAudit 写入 n 条链式审计并返回行 id（自增顺序 = 返回顺序）。
func writeChainedAudit(t *testing.T, s *SQLStore, tenant, action string, n int, base time.Time) []int64 {
	t.Helper()
	ids := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		s.Audit(&proto.AuditEvent{
			TenantID: tenant, UserID: "u1", Action: action,
			Target: fmt.Sprintf("target-%d", i), Detail: fmt.Sprintf("detail-%d", i),
			CreatedAt: base.Add(time.Duration(i) * time.Second), TraceID: "trace-int",
		})
	}
	ctx := context.Background()
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM audit_log WHERE action=? ORDER BY id ASC`, action)
	if err != nil {
		t.Fatalf("回查审计行 id: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("扫描审计行 id: %v", err)
		}
		ids = append(ids, id)
	}
	if len(ids) != n {
		t.Fatalf("应写入 %d 条审计，实际 %d 条（链式写入可能失败并降级）", n, len(ids))
	}
	return ids
}

// TestAuditChainIntegration_WriteVerify 验证链式写入后自校验通过。
// 关键回归点：事件时间带纳秒（time.Now），若哈希未做秒级归一，此处必然失败。
func TestAuditChainIntegration_WriteVerify(t *testing.T) {
	s := auditChainTestStore(t)

	ids := writeChainedAudit(t, s, "t1", "chain_write", 5, time.Now().UTC().Truncate(time.Second).Add(137*time.Nanosecond))
	if len(ids) != 5 {
		t.Fatalf("写入行数错误: %d", len(ids))
	}
	res, err := s.VerifyAuditChain("", 100)
	if err != nil {
		t.Fatalf("校验报错: %v", err)
	}
	if !res.Supported {
		t.Fatalf("迁移 019 应已生效；got %+v", res)
	}
	if !res.OK || res.FirstBadID != 0 {
		t.Fatalf("自洽链应校验通过；got ok=%v firstBad=%d reason=%s", res.OK, res.FirstBadID, res.Reason)
	}
	if res.Checked != 5 || res.FromID != ids[0] || res.ToID != ids[4] {
		t.Fatalf("校验窗口错误: checked=%d from=%d to=%d（期望 5 / %d / %d）",
			res.Checked, res.FromID, res.ToID, ids[0], ids[4])
	}
	if !res.HeadConsistent || !res.TailCovered {
		t.Fatalf("链头应与在线最新行一致: %+v", res)
	}
	// 前驱链接：首行 prev 应为创世（空），且写库内容与哈希一致（回读复算通过）。
	var prev, entry string
	if err := s.db.QueryRowContext(context.Background(),
		`SELECT COALESCE(prev_hash,''), entry_hash FROM audit_log WHERE id=?`, ids[0]).Scan(&prev, &entry); err != nil {
		t.Fatalf("回读首行哈希: %v", err)
	}
	if prev != auditChainGenesis {
		t.Fatalf("首行 prev_hash 应为创世空值；got %q", prev)
	}
	if entry == "" {
		t.Fatal("首行 entry_hash 为空（链式写入未生效）")
	}
}

// TestAuditChainIntegration_TamperDetected 验证改库（DBA 直接改内容）能被检出并定位到行。
func TestAuditChainIntegration_TamperDetected(t *testing.T) {
	s := auditChainTestStore(t)
	ids := writeChainedAudit(t, s, "t1", "chain_tamper", 5, time.Now().UTC().Truncate(time.Second))

	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, `UPDATE audit_log SET detail='被改过的内容' WHERE id=?`, ids[2]); err != nil {
		t.Fatalf("模拟篡改失败: %v", err)
	}
	res, err := s.VerifyAuditChain("", 100)
	if err != nil {
		t.Fatalf("校验报错: %v", err)
	}
	if res.OK {
		t.Fatal("内容被改写但校验通过（防篡改失效）")
	}
	if res.FirstBadID != ids[2] {
		t.Fatalf("应定位到被改行 id=%d；got %d（reason=%s）", ids[2], res.FirstBadID, res.Reason)
	}
}

// TestAuditChainIntegration_DeleteMiddle 验证删除中间行（断链）能被检出并定位到后继行。
func TestAuditChainIntegration_DeleteMiddle(t *testing.T) {
	s := auditChainTestStore(t)
	ids := writeChainedAudit(t, s, "t1", "chain_del_mid", 5, time.Now().UTC().Truncate(time.Second))
	ctx := context.Background()

	if _, err := s.db.ExecContext(ctx, `DELETE FROM audit_log WHERE id=?`, ids[1]); err != nil {
		t.Fatalf("删除中间行失败: %v", err)
	}
	res, err := s.VerifyAuditChain("", 100)
	if err != nil {
		t.Fatalf("校验报错: %v", err)
	}
	if res.OK {
		t.Fatal("删除中间行但校验通过")
	}
	if res.FirstBadID != ids[2] {
		t.Fatalf("应在被删行的后继 id=%d 检出断链；got %d（reason=%s）", ids[2], res.FirstBadID, res.Reason)
	}
}

// TestAuditChainIntegration_DeleteTail 验证删除尾部行：窗口自身仍自洽，但链头指向的行
// 已不在在线链式行中 → 必须报「尾部被删除」（平台级 TailCovered=false 即证据）。
func TestAuditChainIntegration_DeleteTail(t *testing.T) {
	s := auditChainTestStore(t)
	ids := writeChainedAudit(t, s, "t1", "chain_del_tail", 4, time.Now().UTC().Truncate(time.Second))
	ctx := context.Background()

	if _, err := s.db.ExecContext(ctx, `DELETE FROM audit_log WHERE id=?`, ids[3]); err != nil {
		t.Fatalf("删除尾部行失败: %v", err)
	}
	res, err := s.VerifyAuditChain("", 100)
	if err != nil {
		t.Fatalf("校验报错: %v", err)
	}
	if res.OK {
		t.Fatal("删除尾部行但校验通过（链头一致性未生效）")
	}
	if res.TailCovered || res.HeadConsistent {
		t.Fatalf("尾部被删除时 TailCovered/HeadConsistent 应为 false；got tailCovered=%v headConsistent=%v",
			res.TailCovered, res.HeadConsistent)
	}
	if res.FirstBadID != ids[3] {
		t.Fatalf("应指向丢失的链尾行 id=%d；got %d（reason=%s）", ids[3], res.FirstBadID, res.Reason)
	}
	if !strings.Contains(res.Reason, "尾部") {
		t.Fatalf("原因应指出尾部行被删除；got %q", res.Reason)
	}
}

// TestAuditChainIntegration_LegacyRows 验证链前遗留行（迁移前写入、entry_hash 为空）
// 如实计入 LegacyRows，且不被误报为篡改。
func TestAuditChainIntegration_LegacyRows(t *testing.T) {
	s := auditChainTestStore(t)
	ctx := context.Background()
	// 模拟迁移前写入的行：无 prev_hash / entry_hash。
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log (tenant_id, user_id, action, target, detail, created_at)
		 VALUES ('t1','legacy','legacy_action','t','d', ?)`, time.Now().UTC()); err != nil {
		t.Fatalf("写入遗留行失败: %v", err)
	}
	res, err := s.VerifyAuditChain("", 100)
	if err != nil {
		t.Fatalf("校验报错: %v", err)
	}
	if res.LegacyRows != 1 {
		t.Fatalf("应统计到 1 条链前遗留行；got %d", res.LegacyRows)
	}
	if !res.OK {
		t.Fatalf("仅存在遗留行时不应报篡改；got reason=%s", res.Reason)
	}
	// 再写一条链式行：其 prev 为空串（创世），窗口应通过且说明存在链前遗留行。
	writeChainedAudit(t, s, "t1", "chain_after_legacy", 1, time.Now().UTC().Truncate(time.Second))
	res2, err := s.VerifyAuditChain("", 100)
	if err != nil {
		t.Fatalf("校验报错: %v", err)
	}
	if !res2.OK || res2.Checked != 1 {
		t.Fatalf("链式行应自洽；got ok=%v checked=%d reason=%s", res2.OK, res2.Checked, res2.Reason)
	}
}

// TestAuditChainIntegration_TenantScope 验证租户窗口：只校验该租户的行，
// 且当前租户不是链尾时如实标注「链头一致性未判定」（TailCovered=false）。
func TestAuditChainIntegration_TenantScope(t *testing.T) {
	s := auditChainTestStore(t)
	base := time.Now().UTC().Truncate(time.Second)
	// 交错写入：tA / tB 交替，最后一条属于 tB → tA 的窗口不覆盖链尾。
	writeChainedAudit(t, s, "tA", "scope_a1", 1, base)
	writeChainedAudit(t, s, "tB", "scope_b1", 1, base.Add(time.Second))
	writeChainedAudit(t, s, "tA", "scope_a2", 1, base.Add(2*time.Second))
	writeChainedAudit(t, s, "tB", "scope_b2", 1, base.Add(3*time.Second))

	resA, err := s.VerifyAuditChain("tA", 100)
	if err != nil {
		t.Fatalf("校验报错: %v", err)
	}
	if !resA.Supported || !resA.OK || resA.Checked != 2 {
		t.Fatalf("tA 窗口应自洽且仅含 2 行；got %+v", resA)
	}
	if resA.TailCovered {
		t.Fatalf("tA 不是链尾，TailCovered 应为 false；got %+v", resA)
	}
	if !strings.Contains(resA.Note, "未判定") {
		t.Fatalf("非链尾窗口应在 Note 说明链头一致性未判定；got %q", resA.Note)
	}
	// 平台级（tenant 为空）覆盖全部 4 行且覆盖链尾。
	resAll, err := s.VerifyAuditChain("", 100)
	if err != nil {
		t.Fatalf("平台级校验报错: %v", err)
	}
	if !resAll.OK || resAll.Checked != 4 || !resAll.TailCovered || !resAll.HeadConsistent {
		t.Fatalf("平台级校验应覆盖 4 行并覆盖链尾；got %+v", resAll)
	}
}

// TestAuditChainIntegration_Archive 验证归档：超龄行搬入 audit_log_archive、
// 边界哈希落 meta，且「归档段 ↔ 在线段」链接在删除后仍可验证。
func TestAuditChainIntegration_Archive(t *testing.T) {
	s := auditChainTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	// 3 条 40 天前的历史事件（先写 → id 小）+ 2 条当前事件。
	writeChainedAudit(t, s, "t1", "arch_old", 3, now.AddDate(0, 0, -40))
	liveIDs := writeChainedAudit(t, s, "t1", "arch_live", 2, now)

	// retainDays=0 → 空操作。
	if n, err := s.ArchiveAuditLog(0, 100); err != nil || n != 0 {
		t.Fatalf("retainDays=0 应为空操作；got n=%d err=%v", n, err)
	}
	n, err := s.ArchiveAuditLog(30, 100)
	if err != nil {
		t.Fatalf("归档报错: %v", err)
	}
	if n != 3 {
		t.Fatalf("应归档 3 条超龄行；got %d", n)
	}
	// 在线表剩 2 条、归档表 3 条。
	var online, archived int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log`).Scan(&online); err != nil {
		t.Fatalf("统计在线行: %v", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log_archive`).Scan(&archived); err != nil {
		t.Fatalf("统计归档行: %v", err)
	}
	if online != 2 || archived != 3 {
		t.Fatalf("在线/归档行数应为 2/3；got %d/%d", online, archived)
	}
	// 归档边界哈希应为「被归档段最后一条链式行」的 entry_hash。
	var lastArchivedHash string
	if err := s.db.QueryRowContext(ctx,
		`SELECT entry_hash FROM audit_log_archive WHERE entry_hash<>'' ORDER BY id DESC LIMIT 1`).Scan(&lastArchivedHash); err != nil {
		t.Fatalf("查归档段尾哈希: %v", err)
	}
	through, _, boundary, err := s.AuditArchiveMeta()
	if err != nil {
		t.Fatalf("读归档元数据: %v", err)
	}
	if boundary != lastArchivedHash {
		t.Fatalf("归档边界哈希与归档段尾不一致：meta=%s want=%s", boundary, lastArchivedHash)
	}
	if through == 0 {
		t.Fatal("归档水位（archived_through_id）未推进")
	}
	// 删除后验证：在线窗口首行必须与归档边界链接（否则会被误报为断链）。
	res, err := s.VerifyAuditChain("", 100)
	if err != nil {
		t.Fatalf("归档后校验报错: %v", err)
	}
	if !res.OK || res.Checked != 2 {
		t.Fatalf("归档后在线段应自洽；got %+v", res)
	}
	if res.ArchivedBoundaryHash != lastArchivedHash || res.ArchivedThroughID != through {
		t.Fatalf("校验结果应带归档边界信息；got %+v", res)
	}
	if res.FromID != liveIDs[0] || res.ToID != liveIDs[1] {
		t.Fatalf("在线窗口应为 %v；got from=%d to=%d", liveIDs, res.FromID, res.ToID)
	}
}

// TestAuditChainIntegration_ArchiveTailRollback 验证「归档搬走链尾」的链头回退：
// 补写历史事件（CreatedAt 在过去）时会先写在线行、再写超龄行，
// 归档若搬走链尾行而链头不回退，校验端会把归档误报为「尾部被删除」。
func TestAuditChainIntegration_ArchiveTailRollback(t *testing.T) {
	s := auditChainTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	// 先写 2 条当前事件（id 小），再写 3 条补历史事件（id 大但时间在过去）。
	liveIDs := writeChainedAudit(t, s, "t1", "roll_live", 2, now)
	oldIDs := writeChainedAudit(t, s, "t1", "roll_old", 3, now.AddDate(0, 0, -40))

	n, err := s.ArchiveAuditLog(30, 100)
	if err != nil {
		t.Fatalf("归档报错: %v", err)
	}
	if n != 3 {
		t.Fatalf("应归档 3 条补历史行；got %d", n)
	}
	// 链头应回退到在线段最新行（id=liveIDs[1]），而不是仍指向已被归档的 oldIDs[2]。
	var headID int64
	var headHash string
	if err := s.db.QueryRowContext(ctx,
		`SELECT last_id, last_hash FROM audit_chain_head WHERE id=1`).Scan(&headID, &headHash); err != nil {
		t.Fatalf("读链头: %v", err)
	}
	var wantHash string
	if err := s.db.QueryRowContext(ctx,
		`SELECT entry_hash FROM audit_log WHERE id=?`, liveIDs[1]).Scan(&wantHash); err != nil {
		t.Fatalf("读在线尾行哈希: %v", err)
	}
	if headID != liveIDs[1] || headHash != wantHash {
		t.Fatalf("链头应回退到在线段尾行 id=%d；got id=%d hash=%s want=%s", liveIDs[1], headID, headHash, wantHash)
	}
	if oldIDs[2] == liveIDs[1] {
		t.Fatal("测试前提错误：补历史行与在线行 id 应不同")
	}
	// 回退后校验应通过（不把归档误报成尾部被删除）。
	res, err := s.VerifyAuditChain("", 100)
	if err != nil {
		t.Fatalf("校验报错: %v", err)
	}
	if !res.OK || !res.HeadConsistent {
		t.Fatalf("归档链尾后在线段应自洽且链头一致；got %+v", res)
	}
}
