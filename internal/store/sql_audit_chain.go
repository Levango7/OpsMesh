// sql_audit_chain.go — 审计日志防篡改哈希链（P1-3）。
//
// 设计要点：
//   - 链式写入：每条 audit 行携带 prev_hash（上一条的 entry_hash）与 entry_hash
//     （sha256(prev_hash || 规范化字段)）。任一行被改写/删除/换序，后续链接即对不上。
//   - 序列化：append 在事务内对 audit_chain_head 单行 SELECT ... FOR UPDATE，
//     多副本并发写入串行化且链不分叉（单行锁是天然的序列化点）。
//   - 时间归一：created_at 以秒精度参与哈希。MySQL DATETIME 无小数秒，
//     若用纳秒值计算，回读时必然对不上（自校验永远失败）——这是本文件的硬约束。
//   - 归档保留：超龄行先搬入 audit_log_archive 再删除，边界哈希记入
//     audit_archive_meta，使「已归档段 ↔ 在线段」的链接在删除后仍可验证。
//   - 诚实边界：持 DB 凭证的攻击者可以整链重算（重写全部行的哈希）——哈希链防的是
//     「改一行不被发现」，不是「有 DB 写权限者完全无法伪造」。要做到后者需要外部
//     WORM/远端签名锚点（见 docs/security-mechanism.md）。
package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// auditChainGenesis 链首行的 prev_hash（无前驱时使用）。
const auditChainGenesis = ""

// auditChainFieldSep 规范化编码的字段分隔符（0x1f，不可能出现在业务文本里；
// 即便如此仍配合长度前缀，双重保证不同字段组合的编码不碰撞）。
const auditChainFieldSep = "\x1f"

// auditHashField 按「长度前缀 + 内容」写入字段，避免朴素拼接歧义：
// ("ab","c") 与 ("a","bc") 的拼接结果相同，带长度前缀后必然不同。
func auditHashField(b *strings.Builder, s string) {
	b.WriteString(strconv.Itoa(len(s)))
	b.WriteByte(':')
	b.WriteString(s)
	b.WriteString(auditChainFieldSep)
}

// auditEntryHash 计算一条审计记录的链式摘要：sha256(prev_hash || 规范化(全部业务字段))。
// created_at 以秒精度（UTC RFC3339）参与：与 MySQL DATETIME 的存储精度对齐，
// 否则回读值必然与写入值不等，自校验恒失败。
func auditEntryHash(prev string, e *proto.AuditEvent) string {
	var b strings.Builder
	auditHashField(&b, prev)
	auditHashField(&b, e.TenantID)
	auditHashField(&b, e.UserID)
	auditHashField(&b, e.Action)
	auditHashField(&b, e.Target)
	auditHashField(&b, e.Detail)
	auditHashField(&b, e.CreatedAt.UTC().Format(time.RFC3339))
	auditHashField(&b, e.TraceID)
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// auditChainRow 校验用的一行（DB 列 → 结构体）。
type auditChainRow struct {
	ID        int64
	Event     proto.AuditEvent
	PrevHash  string
	EntryHash string
}

// AuditChainVerifyResult 审计链校验结果（对外 API 直接序列化）。
type AuditChainVerifyResult struct {
	// Supported 该存储后端是否支持链式校验（SQL 后端且已应用迁移 019 时为 true）。
	Supported bool `json:"supported"`
	// Scope 校验范围：platform=平台级（全链，逐行链接严格校验）；
	// tenant=租户视图（只读本租户行，行自洽 + 边界链接，逐行链接受跨租户交错限制）。
	// 两者结论强度不同，消费方（告警/合规报告）应据此判断。
	Scope string `json:"scope,omitempty"`
	// OK 为 true 表示窗口内所有行自洽、且窗口首行与前驱（在线前一行/归档边界/创世）链接一致。
	OK bool `json:"ok"`
	// Checked 本次实际校验的行数。
	Checked int `json:"checked"`
	// FromID / ToID 本次校验覆盖的行号区间（闭区间）。
	FromID int64 `json:"fromID"`
	ToID   int64 `json:"toID"`
	// FirstBadID / Reason 首个不一致的行号与原因（OK=true 时为空）。
	FirstBadID int64  `json:"firstBadID,omitempty"`
	Reason     string `json:"reason,omitempty"`
	// ChainHeadID / ChainHeadHash 链头记录（最新一次成功追加）。
	ChainHeadID   int64  `json:"chainHeadID"`
	ChainHeadHash string `json:"chainHeadHash,omitempty"`
	// HeadConsistent 链头与在线最新行的 entry_hash 是否一致（false 说明尾部被删/链头过期）。
	HeadConsistent bool `json:"headConsistent"`
	// TailCovered 本次窗口是否覆盖到链尾（租户视图下窗口可能不覆盖链尾，此时 HeadConsistent 不做判定）。
	TailCovered bool `json:"tailCovered"`
	// ArchivedThroughID / ArchivedBoundaryHash 归档边界（0/空表示尚无归档）。
	ArchivedThroughID    int64  `json:"archivedThroughID,omitempty"`
	ArchivedBoundaryHash string `json:"archivedBoundaryHash,omitempty"`
	// LegacyRows 未纳入链的历史行数（本迁移上线前写入的行，entry_hash 为空）。
	LegacyRows int64 `json:"legacyRows"`
	// Note 人类可读的补充说明（如窗口截断、无归档、存在链前遗留行）。
	Note string `json:"note,omitempty"`
}

// verifyChainRows 校验一串按 id 升序的链式行（纯函数，便于单测）——平台级严格校验：
//   - 每行 entry_hash 必须等于 H(该行 prev_hash, 该行字段)（内容未被改写）；
//   - 每行 prev_hash 必须等于前一行的 entry_hash（顺序与完整性未被破坏）；
//   - 首行的 prev_hash 必须等于 expectedPrev（前驱：在线前一行/归档边界/创世）。
//
// 该函数要求全链可读（跨租户），租户视图请用 verifyChainRowsScoped。
// 返回 (首个坏行的下标, 原因)；全部通过时下标为 -1。
func verifyChainRows(rows []auditChainRow, expectedPrev string) (int, string) {
	prev := expectedPrev
	for i, r := range rows {
		if r.EntryHash == "" {
			return i, fmt.Sprintf("id=%d 已纳入链的行缺少 entry_hash", r.ID)
		}
		if r.PrevHash != prev {
			return i, fmt.Sprintf("id=%d 的 prev_hash 与前驱 entry_hash 不一致（行被删除/换序或前驱被改写）", r.ID)
		}
		if want := auditEntryHash(prev, &r.Event); want != r.EntryHash {
			return i, fmt.Sprintf("id=%d 的 entry_hash 与内容重算结果不一致（行内容被改写）", r.ID)
		}
		prev = r.EntryHash
	}
	return -1, ""
}

// isRetryableTxErr 判定 MySQL 事务错误是否值得重试（死锁 1213 / 锁等待超时 1205）。
// 链式写入对所有副本串行化，突发并发下死锁是预期内的瞬时冲突，重试即可。
func isRetryableTxErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Error 1213") || strings.Contains(msg, "Error 1205") ||
		strings.Contains(strings.ToLower(msg), "deadlock")
}

// auditColumnsTTL 列/表能力探测的缓存有效期。迁移在启动时应用、运行期不改表，
// 30s TTL 只为「服务先起来后补迁移」的窗口兜底（探测本身是幂等只读）。
const auditColumnsTTL = 30 * time.Second

// auditColumnFlags 返回审计写入所需的能力：trace_id 列 + 链列 + 链头表。
// hasChain 为 true 才可走链式写入；否则降级为非链式（老库未迁移 019）。
// 带 TTL 缓存：审计写入是高频路径（每 30s/agent 一条 report_logs）。
func (s *SQLStore) auditColumnFlags(ctx context.Context) (hasTrace, hasChain bool) {
	s.auditColsMu.Lock()
	if !s.auditColsAt.IsZero() && time.Since(s.auditColsAt) < auditColumnsTTL {
		hasTrace, hasChain = s.auditHasTrace, s.auditHasChain
		s.auditColsMu.Unlock()
		return
	}
	s.auditColsMu.Unlock()

	hasTrace = s.columnExists(ctx, "audit_log", "trace_id")
	hasChain = s.columnExists(ctx, "audit_log", "prev_hash") &&
		s.columnExists(ctx, "audit_log", "entry_hash") &&
		s.tableExists(ctx, "audit_chain_head")

	s.auditColsMu.Lock()
	s.auditHasTrace, s.auditHasChain, s.auditColsAt = hasTrace, hasChain, time.Now()
	s.auditColsMu.Unlock()
	return hasTrace, hasChain
}

// tableExists 检查当前库中表是否存在（兼容老库未迁移场景）。
func (s *SQLStore) tableExists(ctx context.Context, table string) bool {
	var cnt int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?`,
		table).Scan(&cnt); err != nil {
		return false
	}
	return cnt > 0
}

// insertChainedAudit 在事务内把事件追加到审计哈希链，返回写入的行 id。
// 调用方须已完成列存在性探测（hasTrace 决定是否写入 trace_id 列）。
func (s *SQLStore) insertChainedAudit(ctx context.Context, e *proto.AuditEvent, hasTrace bool) (int64, error) {
	// 时间归一（硬约束）：DATETIME 秒精度，纳秒值会让哈希无法回读复算。
	e.CreatedAt = e.CreatedAt.UTC().Truncate(time.Second)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	// 链表头行不存在时补建（迁移已建；此处兜底防手工删表）。
	if _, err := tx.ExecContext(ctx,
		`INSERT IGNORE INTO audit_chain_head (id, last_id, last_hash, updated_at) VALUES (1, 0, '', ?)`, now); err != nil {
		return 0, fmt.Errorf("初始化链头: %w", err)
	}
	// 单行锁 = 追加序列化点：多副本并发写入不会分叉。
	var lastHash string
	if err := tx.QueryRowContext(ctx,
		`SELECT last_hash FROM audit_chain_head WHERE id=1 FOR UPDATE`).Scan(&lastHash); err != nil {
		return 0, fmt.Errorf("锁定链头: %w", err)
	}

	entryHash := auditEntryHash(lastHash, e)
	var res sql.Result
	if hasTrace {
		res, err = tx.ExecContext(ctx,
			`INSERT INTO audit_log (tenant_id, user_id, action, target, detail, created_at, trace_id, prev_hash, entry_hash)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			e.TenantID, e.UserID, e.Action, e.Target, e.Detail, e.CreatedAt, e.TraceID, lastHash, entryHash)
	} else {
		res, err = tx.ExecContext(ctx,
			`INSERT INTO audit_log (tenant_id, user_id, action, target, detail, created_at, prev_hash, entry_hash)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			e.TenantID, e.UserID, e.Action, e.Target, e.Detail, e.CreatedAt, lastHash, entryHash)
	}
	if err != nil {
		return 0, fmt.Errorf("插入链式审计行: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("读取审计行 id: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE audit_chain_head SET last_id=?, last_hash=?, updated_at=? WHERE id=1`, id, entryHash, now); err != nil {
		return 0, fmt.Errorf("推进链头: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// VerifyAuditChain 校验最近 limit 行审计的链式完整性（limit<=0 时取 1000）。
//
// tenant 非空（租户视图）：
//   - 窗口与遗留行计数按 tenant_id 过滤，输出不含其他租户的行内容；
//   - 逐行链接校验退化为「行自洽 + 首行前驱边界」（同租户相邻行之间可能夹着其他租户的行，
//     而校验哈希必须读行内容，跨租户内容不可读）→ Scope="tenant"，结论强度较弱；
//   - 窗口未覆盖链尾时不做链头一致性判定（TailCovered=false）。
//
// tenant 为空（平台级）：全链窗口逐行严格链接校验（Scope="platform"）。
//
// 校验范围刻意限定为「最近 limit 行 + 与前一行的链接」：全表校验在长跑库上是 O(全表)，
// 不适合放在 HTTP 请求里（需要全量校验时应按窗口分段多次调用，或用归档边界逐段推进）。
func (s *SQLStore) VerifyAuditChain(tenant string, limit int) (*AuditChainVerifyResult, error) {
	if limit <= 0 {
		limit = 1000
	}
	if limit > maxAuditVerifyLimit {
		limit = maxAuditVerifyLimit
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	hasTrace, hasChain := s.auditColumnFlags(ctx)
	if !hasChain {
		// 老库未应用迁移 019：如实回报「不支持」，不伪称校验通过，也不当成错误。
		return &AuditChainVerifyResult{
			Supported: false,
			Note:      "audit_log 缺少哈希链列或链头表（迁移 019 未应用），本库不提供防篡改校验",
		}, nil
	}

	res := &AuditChainVerifyResult{Supported: true, OK: true, Scope: "platform"}
	tenantFilter := ""
	targs := []interface{}{}
	if tenant != "" {
		tenantFilter = " AND tenant_id=?"
		targs = append(targs, tenant)
		res.Scope = "tenant"
	}
	// 链前遗留行（迁移上线前写入，未纳入链）：如实计入，不视为篡改。
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_log WHERE (entry_hash IS NULL OR entry_hash='')`+tenantFilter,
		targs...).Scan(&res.LegacyRows); err != nil {
		return nil, fmt.Errorf("统计链前遗留行: %w", err)
	}
	// 归档边界：老行被归档删除后，在线首行须与边界哈希链接。
	var archivedThrough sql.NullInt64
	var boundary sql.NullString
	if err := s.db.QueryRowContext(ctx,
		`SELECT archived_through_id, boundary_hash FROM audit_archive_meta WHERE id=1`).
		Scan(&archivedThrough, &boundary); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("读取归档边界: %w", err)
	}
	if archivedThrough.Valid {
		res.ArchivedThroughID = archivedThrough.Int64
	}
	if boundary.Valid {
		res.ArchivedBoundaryHash = boundary.String
	}
	// 链头。
	if err := s.db.QueryRowContext(ctx,
		`SELECT last_id, last_hash FROM audit_chain_head WHERE id=1`).
		Scan(&res.ChainHeadID, &res.ChainHeadHash); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("读取链头: %w", err)
	}

	// 取最近 limit 条链式行（倒序取再反转，保证窗口是「最新的一段」）。
	cols := `id, tenant_id, user_id, action, target, detail, created_at, trace_id, prev_hash, entry_hash`
	scan := func(rows *sql.Rows, r *auditChainRow) error {
		return rows.Scan(&r.ID, &r.Event.TenantID, &r.Event.UserID, &r.Event.Action,
			&r.Event.Target, &r.Event.Detail, &r.Event.CreatedAt, &r.Event.TraceID, &r.PrevHash, &r.EntryHash)
	}
	where := `WHERE entry_hash IS NOT NULL AND entry_hash<>''` + tenantFilter
	q := `SELECT ` + cols + ` FROM audit_log ` + where + ` ORDER BY id DESC LIMIT ?`
	if !hasTrace {
		q = `SELECT id, tenant_id, user_id, action, target, detail, created_at, '', prev_hash, entry_hash
		     FROM audit_log ` + where + ` ORDER BY id DESC LIMIT ?`
	}
	qargs := append(append([]interface{}{}, targs...), limit)
	dbRows, err := s.db.QueryContext(ctx, q, qargs...)
	if err != nil {
		return nil, fmt.Errorf("查询链式审计行: %w", err)
	}
	defer dbRows.Close()
	var desc []auditChainRow
	for dbRows.Next() {
		var r auditChainRow
		if err := scan(dbRows, &r); err != nil {
			return nil, fmt.Errorf("扫描链式审计行: %w", err)
		}
		desc = append(desc, r)
	}
	if err := dbRows.Err(); err != nil {
		return nil, fmt.Errorf("遍历链式审计行: %w", err)
	}
	rows := make([]auditChainRow, len(desc))
	for i, r := range desc {
		rows[len(desc)-1-i] = r
	}
	res.Checked = len(rows)
	if len(rows) == 0 {
		res.Note = "尚无已纳入链的审计行"
		return res, nil
	}
	res.FromID = rows[0].ID
	res.ToID = rows[len(rows)-1].ID

	// 窗口首行的期望前驱：在线前一行 → 归档段前一行 → 创世。
	// 归档段必须真的在窗口「之下」（id < FromID）才算前驱：归档也可能搬走链尾
	// （补写历史事件时会先写在线行、再写超龄行），此时归档段在窗口之上，与前驱无关。
	expectedPrev := auditChainGenesis
	var prevHash string
	err = s.db.QueryRowContext(ctx,
		`SELECT entry_hash FROM audit_log WHERE id < ? AND entry_hash IS NOT NULL AND entry_hash<>'' ORDER BY id DESC LIMIT 1`,
		res.FromID).Scan(&prevHash)
	switch {
	case err == nil:
		expectedPrev = prevHash
	case errors.Is(err, sql.ErrNoRows):
		// 在线段前无链式行：前驱可能已被保留策略归档，到归档表查同一条件。
		var archID int64
		errArch := s.db.QueryRowContext(ctx,
			`SELECT id, entry_hash FROM audit_log_archive WHERE id < ? AND entry_hash IS NOT NULL AND entry_hash<>'' ORDER BY id DESC LIMIT 1`,
			res.FromID).Scan(&archID, &prevHash)
		switch {
		case errArch == nil:
			expectedPrev = prevHash
			auditAppendNote(res, fmt.Sprintf("窗口前驱在归档段（id=%d）", archID))
		case errors.Is(errArch, sql.ErrNoRows):
			if res.LegacyRows > 0 {
				auditAppendNote(res, fmt.Sprintf("存在 %d 条链前遗留行（迁移上线前写入，未纳入链）", res.LegacyRows))
			}
		default:
			return nil, fmt.Errorf("查询归档段前驱: %w", errArch)
		}
	default:
		return nil, fmt.Errorf("查询窗口前驱: %w", err)
	}

	// 租户视图下窗口内相邻行可能跨着其他租户的行（链是一条跨租户的链），
	// 故只能用「自洽 + 边界链接」的弱校验；平台级用严格链接校验。
	verifyFn := verifyChainRows
	if tenant != "" {
		verifyFn = verifyChainRowsScoped
	}
	if bad, reason := verifyFn(rows, expectedPrev); bad >= 0 {
		res.OK = false
		res.FirstBadID = rows[bad].ID
		res.Reason = reason
	}
	// 链头一致性：链头指向的行必须就是在线最新链式行，且哈希一致。
	// 平台级：窗口就是「最新的一段」，链头若指向窗口外或哈希不符 ⇒ 尾部行被删除/链头被改。
	// 租户视图：链尾行属于哪个租户不确定，窗口不覆盖链尾时不做判定（如实标注 TailCovered=false）。
	res.TailCovered = res.ChainHeadID == res.ToID
	switch {
	case res.TailCovered:
		res.HeadConsistent = res.ChainHeadHash == rows[len(rows)-1].EntryHash
	case tenant != "":
		res.HeadConsistent = true
		auditAppendNote(res, fmt.Sprintf("窗口未覆盖链尾（该窗口最新 id=%d < 链头 id=%d），链头一致性未判定", res.ToID, res.ChainHeadID))
	default:
		res.HeadConsistent = false
	}
	if res.OK && !res.HeadConsistent {
		res.OK = false
		if res.TailCovered {
			res.Reason = fmt.Sprintf("链头（id=%d）与在线最新链式行（id=%d）不一致：尾部行被删除或链头被改动", res.ChainHeadID, res.ToID)
			res.FirstBadID = res.ToID
		} else {
			res.Reason = fmt.Sprintf("链头（id=%d）指向的行不在在线链式行中（窗口最新 id=%d）：尾部行被删除", res.ChainHeadID, res.ToID)
			res.FirstBadID = res.ChainHeadID
		}
	}
	return res, nil
}

// verifyChainRowsScoped 租户视图的链校验（纯函数）。
//
// 之所以比 verifyChainRows 弱：链是一条跨租户的链，同一租户的相邻两行之间可能夹着
// 其他租户的行，而校验一行的哈希必须读它的内容（跨租户内容不可读）。故租户视图只能做：
//   - 每行自洽：entry_hash == H(该行自己的 prev_hash, 该行字段)（内容/prev 被改即暴露）；
//   - 首行边界：首行 prev_hash 必须等于窗口前驱（在线前一行/归档段前一行/创世）；
//   - 相邻行链接：仅当窗口内两行 id 相邻（中间确实没有别的行）时才判定链接。
//
// 完整的逐行链接校验在平台级（tenant 为空）由 verifyChainRows 执行。
func verifyChainRowsScoped(rows []auditChainRow, expectedPrev string) (int, string) {
	for i, r := range rows {
		if r.EntryHash == "" {
			return i, fmt.Sprintf("id=%d 已纳入链的行缺少 entry_hash", r.ID)
		}
		if want := auditEntryHash(r.PrevHash, &r.Event); want != r.EntryHash {
			return i, fmt.Sprintf("id=%d 的 entry_hash 与内容重算结果不一致（行内容或 prev_hash 被改写）", r.ID)
		}
		if i == 0 {
			if r.PrevHash != expectedPrev {
				return i, fmt.Sprintf("id=%d 的 prev_hash 与窗口前驱不一致（前驱行被删除/改写，或本行 prev_hash 被改）", r.ID)
			}
			continue
		}
		if r.ID == rows[i-1].ID+1 && r.PrevHash != rows[i-1].EntryHash {
			return i, fmt.Sprintf("id=%d 的 prev_hash 与相邻前一行 entry_hash 不一致（行被改写或换序）", r.ID)
		}
	}
	return -1, ""
}

// auditAppendNote 追加人类可读说明（多条用「；」分隔）。
func auditAppendNote(res *AuditChainVerifyResult, s string) {
	if res.Note == "" {
		res.Note = s
		return
	}
	res.Note += "；" + s
}

// maxAuditVerifyLimit 单次校验的最大行数上限（防把校验端点变成全表扫描的放大器）。
const maxAuditVerifyLimit = 10000

// auditArchiveMeta 归档元数据（边界）。archived 为 true 表示确实发生过归档。
type auditArchiveMeta struct {
	ArchivedThroughID int64
	BoundaryHash      string
	ArchivedRows      int64
}

// ArchiveAuditLog 把超过 retainDays 的审计行搬运到 archive 表后从 audit_log 删除。
//
// retainDays<=0 表示永久保留（不做任何事，返回 0）。batch<=0 时取 500。
// 返回本次归档行数。仅 leader 周期调用（多副本重复归档会互相争锁，虽安全但无意义）。
//
// 顺序保证：先 INSERT IGNORE 入归档表（幂等，主键=id），再 DELETE 在线行，
// 最后推进边界元数据。任何一步失败都不会丢数据（最坏情况是同一行下次重跑再搬一次）。
func (s *SQLStore) ArchiveAuditLog(retainDays, batch int) (int, error) {
	if retainDays <= 0 {
		return 0, nil
	}
	if batch <= 0 {
		batch = 500
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, hasChain := s.auditColumnFlags(ctx); !hasChain {
		return 0, nil // 链列缺失（未迁移老库）：不归档，避免语义不明
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -retainDays)
	total := 0
	for {
		ids, lastHash, err := s.auditArchiveBatch(ctx, cutoff, batch)
		if err != nil {
			return total, err
		}
		if len(ids) == 0 {
			return total, nil
		}
		total += len(ids)
		if err := s.auditArchiveCommit(ctx, ids, lastHash); err != nil {
			return total, err
		}
		if len(ids) < batch {
			return total, nil
		}
	}
}

// auditArchiveBatch 取一批待归档行的 id 与「本批最后一条的 entry_hash」（边界哈希）。
func (s *SQLStore) auditArchiveBatch(ctx context.Context, cutoff time.Time, batch int) ([]int64, string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, COALESCE(entry_hash,'') FROM audit_log WHERE created_at < ? ORDER BY id ASC LIMIT ?`,
		cutoff, batch)
	if err != nil {
		return nil, "", fmt.Errorf("查询待归档审计行: %w", err)
	}
	defer rows.Close()
	var ids []int64
	var lastHash string
	for rows.Next() {
		var id int64
		var h string
		if err := rows.Scan(&id, &h); err != nil {
			return nil, "", fmt.Errorf("扫描待归档审计行: %w", err)
		}
		ids = append(ids, id)
		if h != "" {
			lastHash = h // 本批最后一条「已纳入链」的哈希 = 边界哈希
		}
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("遍历待归档审计行: %w", err)
	}
	return ids, lastHash, nil
}

// auditArchiveCommit 把指定行搬入归档表并删除在线行、推进归档边界。
func (s *SQLStore) auditArchiveCommit(ctx context.Context, ids []int64, boundaryHash string) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]interface{}, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	// 先入归档表（幂等：主键冲突忽略，重放不会重复计入）。
	if _, err := s.db.ExecContext(ctx,
		`INSERT IGNORE INTO audit_log_archive
		   (id, tenant_id, user_id, action, target, detail, created_at, trace_id, prev_hash, entry_hash, archived_at)
		 SELECT id, tenant_id, user_id, action, target, detail, created_at, trace_id, prev_hash, entry_hash, ?
		 FROM audit_log WHERE id IN (`+placeholders+`)`, append([]interface{}{time.Now().UTC()}, args...)...); err != nil {
		return fmt.Errorf("写入归档表: %w", err)
	}
	// 再删在线行（仅删除已确认进入归档表的部分）。
	res, err := s.db.ExecContext(ctx, `DELETE FROM audit_log WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return fmt.Errorf("删除在线审计行: %w", err)
	}
	deleted, _ := res.RowsAffected()
	if int(deleted) != len(ids) {
		// 并发追加不会影响这些超龄行；数量不符说明有别的写者在动审计表 → 明确告警。
		log.Printf("[store] 审计归档删除行数不符：期望 %d 实际 %d（可能有外部写者直接操作 audit_log）", len(ids), deleted)
	}
	// 推进边界（archived_through_id 取本批最大 id）。
	maxID := ids[len(ids)-1]
	// 元数据行兜底初始化必须在读旧值之前（手工删表后重启也能恢复）。
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx,
		`INSERT IGNORE INTO audit_archive_meta (id, archived_through_id, boundary_hash, archived_rows, updated_at) VALUES (1, 0, '', 0, ?)`,
		now); err != nil {
		return fmt.Errorf("初始化归档元数据: %w", err)
	}
	// 本批若全为链前遗留行（无 entry_hash），边界哈希只能沿用旧值——
	// 不能用本批的空值把已有边界清掉（否则归档段与在线段的链接断在这里）。
	if boundaryHash == "" {
		if m, err := s.auditArchiveMeta(ctx); err == nil {
			boundaryHash = m.BoundaryHash
		}
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE audit_archive_meta SET archived_through_id=?, boundary_hash=?, archived_rows=archived_rows+?, updated_at=? WHERE id=1`,
		maxID, boundaryHash, len(ids), now); err != nil {
		return fmt.Errorf("推进归档边界: %w", err)
	}
	// 链头回退：正常情况下链尾就是最新行，不会超龄，故归档搬不到它；
	// 但若补写了历史事件（CreatedAt 在过去）或从备份恢复，链尾行可能落在本批里。
	// 此时链头仍指向已被删除的行 → 校验端会把归档误报成「尾部被删除」。
	// 故：仅当链头行确实在本批归档列表中时，把链头回退到「在线段的最新链式行」
	// （在线已无链式行时回退到归档边界），使在线段的校验自洽。
	if _, err := s.db.ExecContext(ctx,
		`UPDATE audit_chain_head
		    SET last_id   = COALESCE((SELECT MAX(id) FROM audit_log WHERE entry_hash IS NOT NULL AND entry_hash<>''), ?),
		        last_hash = COALESCE((SELECT entry_hash FROM audit_log WHERE entry_hash IS NOT NULL AND entry_hash<>'' ORDER BY id DESC LIMIT 1), ?),
		        updated_at = ?
		  WHERE id=1 AND last_id IN (`+placeholders+`)`,
		append([]interface{}{maxID, boundaryHash, now}, args...)...); err != nil {
		return fmt.Errorf("回退链头: %w", err)
	}
	return nil
}

// auditArchiveMeta 读取归档元数据（边界哈希；无记录时返回零值）。
func (s *SQLStore) auditArchiveMeta(ctx context.Context) (auditArchiveMeta, error) {
	var m auditArchiveMeta
	err := s.db.QueryRowContext(ctx,
		`SELECT archived_through_id, boundary_hash, archived_rows FROM audit_archive_meta WHERE id=1`).
		Scan(&m.ArchivedThroughID, &m.BoundaryHash, &m.ArchivedRows)
	if errors.Is(err, sql.ErrNoRows) {
		return m, nil
	}
	return m, err
}

// AuditArchiveMeta 供控制面在日志/健康检查中展示归档水位（无记录时返回零值）。
func (s *SQLStore) AuditArchiveMeta() (throughID, archivedRows int64, boundaryHash string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, err := s.auditArchiveMeta(ctx)
	return m.ArchivedThroughID, m.ArchivedRows, m.BoundaryHash, err
}
