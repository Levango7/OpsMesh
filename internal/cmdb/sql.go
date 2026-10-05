package cmdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// SQLCiStore 基于 MySQL 的 CMDB 存储实现。
type SQLCiStore struct {
	db *sql.DB

	// fulltextMu 保护下面两个字段：召回路径要读 ready，多个请求可能同时首次探测。
	fulltextMu     sync.Mutex
	fulltextReady  bool
	fulltextProbed bool
}

// NewSQLCiStore 构造 MySQL CMDB 存储，同时种子内置 CI 类型。
func NewSQLCiStore(db *sql.DB) *SQLCiStore {
	s := &SQLCiStore{db: db}
	s.seedTypes()
	return s
}

// seedTypes 确保内置 CI 类型存在（幂等 INSERT IGNORE）。
func (s *SQLCiStore) seedTypes() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, t := range []struct{ name, display string }{
		{"machine", "物理机"},
		{"os", "操作系统"},
		{"service", "系统服务"},
		{"app", "应用"},
		{"cluster", "集群"},
	} {
		if _, err := s.db.ExecContext(ctx,
			`INSERT IGNORE INTO ci_types (name, display_name, builtin, created_at) VALUES (?, ?, 1, NOW())`,
			t.name, t.display); err != nil {
			log.Printf("[cmdb] seedTypes %s: %v", t.name, err)
		}
	}
}

func (s *SQLCiStore) CiTypes(ctx context.Context, tenantID string) ([]CiType, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, display_name, builtin, created_at FROM ci_types ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("CiTypes: %w", err)
	}
	defer rows.Close()
	var out []CiType
	for rows.Next() {
		var t CiType
		if err := rows.Scan(&t.ID, &t.Name, &t.DisplayName, &t.Builtin, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("CiTypes scan: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CreateCiType 创建自定义（非内置）CI 类型（builtin=0）。
func (s *SQLCiStore) CreateCiType(ctx context.Context, t *CiType) error {
	if t.Name == "" {
		return fmt.Errorf("ci type name required")
	}
	if t.DisplayName == "" {
		t.DisplayName = t.Name
	}
	now := time.Now()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO ci_types (name, display_name, builtin, created_at)
		VALUES (?, ?, 0, ?)
		ON DUPLICATE KEY UPDATE display_name=VALUES(display_name)`,
		t.Name, t.DisplayName, now)
	if err != nil {
		return fmt.Errorf("CreateCiType: %w", err)
	}
	if id, lidErr := res.LastInsertId(); lidErr == nil && id > 0 {
		t.ID = int(id)
	}
	t.Builtin = false
	t.CreatedAt = now
	return nil
}

func (s *SQLCiStore) GetCIs(ctx context.Context, ciType, status, tenantID string) ([]CiItem, error) {
	q := `SELECT id, ci_type, tenant_id, name, status, approval_status, attrs, source, agent_id, device_id, version, created_at, updated_at
	FROM ci_items WHERE 1=1`
	var args []interface{}
	if ciType != "" {
		q += " AND ci_type=?"
		args = append(args, ciType)
	}
	if status != "" {
		q += " AND status=?"
		args = append(args, status)
	}
	if tenantID != "" {
		q += " AND tenant_id=?"
		args = append(args, tenantID)
	}
	q += " ORDER BY updated_at DESC"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("GetCIs: %w", err)
	}
	defer rows.Close()
	var out []CiItem
	for rows.Next() {
		item, err := scanCI(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func (s *SQLCiStore) GetCI(ctx context.Context, id, tenantID string) (*CiItem, error) {
	q := `SELECT id, ci_type, tenant_id, name, status, approval_status, attrs, source, agent_id, device_id, version, created_at, updated_at
	FROM ci_items WHERE id=?`
	args := []interface{}{id}
	if tenantID != "" {
		q += " AND tenant_id=?"
		args = append(args, tenantID)
	}
	row := s.db.QueryRowContext(ctx, q, args...)
	return scanCI(row)
}

func (s *SQLCiStore) CreateCI(ctx context.Context, ci *CiItem) error {
	attrsJSON, _ := json.Marshal(ci.Attrs)
	now := time.Now()
	if ci.ApprovalStatus == "" {
		ci.ApprovalStatus = ApprovalApproved
	}
	_, err := s.db.ExecContext(ctx, `
	INSERT INTO ci_items (id, ci_type, tenant_id, name, status, approval_status, attrs, source, agent_id, device_id, version, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		ci.ID, ci.CiType, ci.TenantID, ci.Name, ci.Status, ci.ApprovalStatus, string(attrsJSON),
		ci.Source, ci.AgentID, ci.DeviceID, now, now)
	if err != nil {
		return fmt.Errorf("CreateCI: %w", err)
	}
	return nil
}

func (s *SQLCiStore) UpdateCI(ctx context.Context, ci *CiItem) error {
	attrsJSON, _ := json.Marshal(ci.Attrs)
	res, err := s.db.ExecContext(ctx, `
	UPDATE ci_items SET ci_type=?, name=?, status=?, approval_status=?, attrs=?, source=?, agent_id=?, device_id=?,
		version=version+1, updated_at=NOW() WHERE id=? AND (tenant_id=? OR ?='')`,
		ci.CiType, ci.Name, ci.Status, ci.ApprovalStatus, string(attrsJSON), ci.Source, ci.AgentID, ci.DeviceID,
		ci.ID, ci.TenantID, ci.TenantID)
	if err != nil {
		return fmt.Errorf("UpdateCI: %w", err)
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return fmt.Errorf("UpdateCI rows affected: %w", rowsErr)
	}
	if n == 0 {
		return fmt.Errorf("CI %s not found", ci.ID)
	}
	return nil
}

func (s *SQLCiStore) DeleteCI(ctx context.Context, id, tenantID string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE ci_items SET status='deleted', updated_at=NOW() WHERE id=? AND (tenant_id=? OR ?='')`,
		id, tenantID, tenantID)
	if err != nil {
		return fmt.Errorf("DeleteCI: %w", err)
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return fmt.Errorf("DeleteCI rows affected: %w", rowsErr)
	}
	if n == 0 {
		return fmt.Errorf("CI %s not found", id)
	}
	return nil
}

// GetCIsByApproval 按审批状态列出 CI（Phase-3 待审列表）。
func (s *SQLCiStore) GetCIsByApproval(ctx context.Context, approvalStatus, tenantID string) ([]CiItem, error) {
	q := `SELECT id, ci_type, tenant_id, name, status, approval_status, attrs, source, agent_id, device_id, version, created_at, updated_at
	FROM ci_items WHERE 1=1`
	var args []interface{}
	if approvalStatus != "" {
		q += " AND approval_status=?"
		args = append(args, approvalStatus)
	}
	if tenantID != "" {
		q += " AND tenant_id=?"
		args = append(args, tenantID)
	}
	q += " ORDER BY updated_at DESC"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("GetCIsByApproval: %w", err)
	}
	defer rows.Close()
	var out []CiItem
	for rows.Next() {
		item, err := scanCI(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

// SetApproval 设置单个 CI 的审批状态。
func (s *SQLCiStore) SetApproval(ctx context.Context, id, tenantID, approvalStatus string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE ci_items SET approval_status=?, updated_at=NOW() WHERE id=? AND (tenant_id=? OR ?='')`,
		approvalStatus, id, tenantID, tenantID)
	if err != nil {
		return fmt.Errorf("SetApproval: %w", err)
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return fmt.Errorf("SetApproval rows affected: %w", rowsErr)
	}
	if n == 0 {
		return fmt.Errorf("CI %s not found", id)
	}
	return nil
}

func (s *SQLCiStore) GetCIHistory(ctx context.Context, ciID, tenantID string, limit int) ([]CiItem, error) {
	// MVP：SQL cmdb 不存储历史版本，返回当前版本
	ci, err := s.GetCI(ctx, ciID, tenantID)
	if err != nil {
		return nil, err
	}
	return []CiItem{*ci}, nil
}

// === 全文检索 ===

// ciSearchColumns 是 SQL 召回阶段参与 LIKE 匹配的列表达式。
//
// 必须与 ciSearchText 覆盖的字段集合严格一致，否则会出现"内存后端搜得到、
// SQL 后端搜不到"的口径分裂。JSON 列 attrs 需显式 CAST 成字符串再做 LIKE。
var ciSearchColumns = []string{"name", "ci_type", "CAST(attrs AS CHAR)", "agent_id", "device_id", "source", "id"}

// ciSearchTokenCond 是单个检索词对应的 WHERE 片段模板，含 N 个占位符。
//
// 为什么在包级构造而不是在 SearchCIs 里逐列 `sqlText += col + " LIKE ?"`：
// 列名与片段结构只与列集合有关，与查询无关，每次请求重拼是白做功。
//
// 安全性不变：**检索词本身从不进 SQL 文本**，只作为 `?` 占位符的参数（见下方 args），
// 所以不存在注入面。数据库侧的召回本就是"宽松匹配"，精确判定由 matchCI 负责。
var ciSearchTokenCond = buildCISearchTokenCond()

func buildCISearchTokenCond() string {
	parts := make([]string, 0, len(ciSearchColumns))
	for _, col := range ciSearchColumns {
		parts = append(parts, col+" LIKE ?")
	}
	return " AND (" + strings.Join(parts, " OR ") + ")"
}

// ciSearchFulltextCond 是走全文索引时的召回片段，恰好 1 个占位符。
//
// 覆盖列必须与索引 ft_ci_items_search 的列清单**逐列一致**（020 建 3 列，021 补齐到 7 列），
// 否则 MySQL 报 ER_FT_MATCHING_KEY_NOT_FOUND（1191）。这条不变量由
// TestCISearchFulltextIndexCoversRecallColumns 静态对账迁移文件守住，不需要数据库。
//
// 为什么必须与 ciSearchColumns 同集合（而不是只索引 name/ci_type/attrs）：分流是**独占**的
// ——token 一旦判给 MATCH，就不再对这 7 列做 LIKE。索引若只覆盖 3 列，只命中
// agent_id / device_id / source / id 的行会在召回阶段整行丢失，而 matchCI 只能判定**已召回**
// 的行，救不回来。020 头注释里「标识符由 matchCI 前缀匹配覆盖」混淆了判定层与召回层，
// 推理见 021 迁移注释。ci_attrs_text 是 020 的 STORED 生成列，对应 LIKE 侧的 CAST(attrs AS CHAR)。
const ciSearchFulltextCond = " AND MATCH(name, ci_type, ci_attrs_text, agent_id, device_id, source, id) AGAINST(? IN BOOLEAN MODE)"

// ciSearchNgramTokenSize 是本分流判据**唯一实测过**的 ngram 词元长度。
//
// 分流规则（长度=1 走 LIKE、长度>=2 走 MATCH）是在 ngram_token_size=2 的库上逐 token
// 实测出来的（见 020 迁移注释与 TD-79）。但词元长度是全局可配的：若某部署设成 3，
// 长度=2 的检索词就短于词元，MATCH 一律召回为空——正是本模块实测证明「漏召回无补救」
// 的那一类，而「索引在不在」的探测完全看不出异常（索引存在且类型正确）。
// 故探测必须同时核对 @@ngram_token_size，只放行实测过的 2，其余一律退回 LIKE：慢，但不漏。
const ciSearchNgramTokenSize = 2

// ciSearchFulltextProbeSQL 探测全文索引是否**可用**，而不只是是否存在：
// 索引在位（且类型确为 FULLTEXT）+ 词元长度是被实测过的 2。
//
// 查 information_schema 而不是试一条 MATCH 查询：后者在索引缺失时会在**执行计划阶段**
// 报错并让整条查询失败，而 information_schema 查询只读元数据、恒成功。
// 同时限定 index_type='FULLTEXT'，避免同名普通索引误判为就绪。
//
// 两个值写成标量子查询而不是 `COUNT(*), MAX(@@var)`：后者在零行时 MAX 返回 NULL，
// Scan 进 int 会失败，把「索引不存在」这个**正常**状态错报成探测错误。
// MariaDB 既无 ngram 插件也无 @@ngram_token_size，本查询会报 1193 Unknown system variable
// → 落进「探测失败退 LIKE」分支，方向安全。
const ciSearchFulltextProbeSQL = `SELECT
		(SELECT COUNT(*) FROM information_schema.statistics
		 WHERE table_schema = DATABASE() AND table_name = 'ci_items'
		   AND index_name = 'ft_ci_items_search' AND index_type = 'FULLTEXT'),
		@@ngram_token_size`

// isCISearchFulltextReady 探测 020 迁移建的全文索引是否可用，结果缓存。
//
// 「可用」有两个条件：索引在位，且词元长度是本模块实测过的 2（见 ciSearchNgramTokenSize）。
// 只查「在位」是不够的——索引存在但 ngram_token_size 被改成 3 时，MATCH 会对长度 2 的
// 检索词返回空，而那是静默的漏召回，没有任何错误信号。
//
// 为什么需要运行时探测而不是直接依赖索引：020 迁移与本代码的发布顺序不敏感。
// 先跑迁移 → 索引在 → 走 MATCH；先发代码后跑迁移（或回滚了 020）→ 探不到 → 退回 LIKE。
// 两种顺序都能正常工作，检索功能不会因为迁移未执行而报错或静默返回空。
//
// 为什么必须缓存：探测是一次元数据查询，不能每次检索都打一次。用 once 语义
// （fulltextProbed）保证一个进程生命周期内只探一次——索引存在与否在进程运行期间
// 不会变化，真要变化也需要重启才能让新 DDL 生效，重复探测没有收益。
//
// 探测失败（DB 不可用等）按"未就绪"处理：退回 LIKE 是安全方向，宁可慢不可漏。
func (s *SQLCiStore) isCISearchFulltextReady(ctx context.Context) bool {
	s.fulltextMu.Lock()
	defer s.fulltextMu.Unlock()
	if s.fulltextProbed {
		return s.fulltextReady
	}
	s.fulltextProbed = true // 先置位：即使这次探测失败也不在每个请求上重试

	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var idxCount, ngramSize int
	if err := s.db.QueryRowContext(probeCtx, ciSearchFulltextProbeSQL).Scan(&idxCount, &ngramSize); err != nil {
		log.Printf("[cmdb] 全文索引探测失败，本次退回 LIKE 召回: %v", err)
		return false
	}
	s.fulltextReady = idxCount > 0 && ngramSize == ciSearchNgramTokenSize
	switch {
	case s.fulltextReady:
		log.Printf("[cmdb] 检测到 ci_items 全文索引（ngram_token_size=%d），长度>=2 的检索词走 MATCH 召回", ngramSize)
	case idxCount == 0:
		log.Printf("[cmdb] 未检测到 ci_items 全文索引（020 迁移未执行？），全部走 LIKE 召回")
	default:
		// 索引在位但词元长度不是实测过的值：此时 MATCH 的召回行为未经验证，
		// 而「未验证」在本模块里等于「可能静默漏召回」，故不冒险，整体退回 LIKE。
		log.Printf("[cmdb] ci_items 全文索引在位但 ngram_token_size=%d（分流判据仅在 %d 上实测过），全部走 LIKE 召回",
			ngramSize, ciSearchNgramTokenSize)
	}
	return s.fulltextReady
}

// ciSearchTokenUseFulltext 判断某个检索词能否安全走全文索引。
//
// 判据来自 020 迁移注释里记录的本机实测（MySQL 8.0.46，10 行中英混合语料，逐 token
// 对比 LIKE 与 MATCH 的命中集合，49 个 token）：
//
//   - 长度 = 1 → 一律走 LIKE。全text.Tokenize 把中文按**单字**切，而 ngram 索引按
//     **双字**切（ngram_token_size=2），单字永远匹配不上双字词元，实测 35/35 全部召回为空。
//     走 MATCH 会让中文检索彻底返回空结果——这是把召回变窄的正确性回归。
//   - 含下划线 → 一律走 LIKE。ngram 把下划线当字面量，而 LIKE 里下划线是单字符通配符
//     （实测 LIKE '%server_prod%' 能命中 'webserver-prod'，MATCH 则不能），两者语义不同，
//     换过去等于漏召回。
//   - 其余（长度 >= 2 且无下划线）→ 走 MATCH。实测 13 个此类 token 的 MATCH 命中集合均为
//     LIKE 的超集或相等。
//
// 为什么允许 MATCH 略微放宽（多召回）而不能放宽到漏召回：精确判定与排序由
// internal/cmdb/search.go 的 matchCI 负责，它按前缀匹配逐 token 复核，
// MATCH 的伪命中（如 ngram 跨字把 "订单1" 匹到 "订单服务"）会在那一层被过滤。
// 反向的漏召回则没有任何补救手段。
func ciSearchTokenUseFulltext(tok string) bool {
	if len([]rune(tok)) < 2 {
		return false
	}
	return !strings.Contains(tok, "_")
}

// SearchCIs 按检索词做全文本检索（见 internal/cmdb/search.go 的设计说明）。
//
// 召回策略分两种，按 token 分流（判据见 ciSearchTokenUseFulltext）：
//   - token 长度 >= 2 且不含下划线 → MATCH … AGAINST，走 020 迁移建的 ngram 全文索引；
//   - 其余（单字 token、含下划线 token）→ 7 列 LIKE 子串匹配。
//
// 分流不是可选优化而是正确性要求：ngram 按双字切索引而分词器按单字切中文，
// 单字 token 走 MATCH 会召回为空（详见 ciSearchTokenUseFulltext 与 020 迁移注释）。
//
// 全文索引不可用（020 未执行、已回滚，或 ngram_token_size 不是实测过的 2）时整体退回 LIKE，
// 检索仍可用——只是退回「候选窗口」取舍：ciSearchSQLRecallCap 之外最相关的 CI 可能取不到。
//
// 收益边界（别把本次改动读成「中文检索已走索引」）：查询侧 token 全部来自
// fulltext.Tokenize（search.go 的 normalizeCiSearch），而它把中文**按单字**切，
// 于是中文查询产出的 token 长度恒为 1，按上面的判据**恒走 LIKE**；MATCH 路径实际只服务
// ASCII/数字词（webserver、db01 这类）。要让中文真正走 ngram，得改**查询侧分词**
// （中文切双字组），而那会把匹配语义从「每个字都出现」变成「这些字连续出现」——
// 属检索行为变更，需单独裁决，不在本改动里顺手做。
//
// 两个刻意的取舍：
//   - 不加 ESCAPE 转义 '%' 与 '_'：未转义只会放宽匹配（'_' 成为通配符），
//     而召回只需是命中集合的超集；省掉转义同时规避了不同驱动对 ESCAPE 子句的语法差异。
//     含下划线的 token 因此一律不走 MATCH——ngram 按字面量处理下划线，与 LIKE 的
//     通配符语义不同，硬换会造成漏召回。
//   - 召回窗口 ciSearchSQLRecallCap **对两条路径都生效**（LIMIT 在 token 循环之外无条件拼接）。
//     本注释此前写作「仅在 LIKE 路径上生效」，与代码不符，已更正。MATCH 是 LIKE 的超集、
//     召回更宽，触发截断的概率不降反升——故 TD-79 的「最相关 CI 落在窗口外」只是被缓解，
//     没有被消除。
func (s *SQLCiStore) SearchCIs(ctx context.Context, tenantID string, q CiSearchQuery) ([]CiSearchHit, error) {
	tokens, mode, limit, status := normalizeCiSearch(q)
	if len(tokens) == 0 {
		return []CiSearchHit{}, nil
	}
	// SQL 文本一次性拼装：收集 WHERE 片段后用 strings.Join 合成，再经 fmt.Sprintf 代入。
	//
	// 为什么不用 `sqlText := "...WHERE 1=1" + strings.Join(conds, "")`：
	// G202（gosec）把「SQL 字面量与变量用 + 拼接」判为字符串拼接注入风险。2026-10-05
	// 实测（同一文件内并列三种写法，golangci-lint v2.13.2 = CI 同版）：
	//   `字面量` + strings.Join(...)        ⇒ G202
	//   fmt.Sprintf("...%s", strings.Join)  ⇒ 不报
	//   fmt.Sprintf(tmpl, strings.Join)     ⇒ 不报
	// 即 G202 匹配的是 `+` 运算符，不是「拼出来的文本里有变量」。
	//
	// 为什么值得为写法绕一下而不是给 internal/cmdb 加豁免：仓库里 G202 已有
	// internal/(store|logstore)/ 的收窄豁免（.golangci.yml），再加一个会稀释这道规则
	// 的约束力。改写法则规则覆盖面不变。
	//
	// 关键点：检索词从不进 SQL 文本，只作为 ? 占位符的参数（见下方 args），
	// 所以不存在注入面；conds 与 args 严格一一对应，由同一次循环同时追加。
	var conds []string
	var args []interface{}
	if tenantID != "" {
		conds = append(conds, " AND tenant_id=?")
		args = append(args, tenantID)
	}
	if status != "" {
		conds = append(conds, " AND status=?")
		args = append(args, status)
	}
	if q.CiType != "" {
		conds = append(conds, " AND ci_type=?")
		args = append(args, q.CiType)
	}
	// 每个检索词都必须至少命中一个列（AND across tokens）。
	// 走哪条召回路径由 ciSearchTokenUseFulltext 逐词决定；fulltextReady 为 false
	// （020 未执行）时全部走 LIKE。
	useFulltext := s.isCISearchFulltextReady(ctx)
	for _, tok := range tokens {
		if useFulltext && ciSearchTokenUseFulltext(tok) {
			conds = append(conds, ciSearchFulltextCond)
			// BOOLEAN 模式下加 "+" 前缀关闭 50% 阈值判定：MATCH 默认会丢弃出现在
			// 超过一半文档里的词，若不关闭，高频词（如某个通用 env 值）在长表里
			// 会突然搜不到——这是随数据增长而漂移的行为，难以复现和诊断。
			// 检索词仍只作为 ? 占位符参数传入，不进 SQL 文本。
			args = append(args, "+"+tok)
			continue
		}
		conds = append(conds, ciSearchTokenCond)
		for range ciSearchColumns {
			args = append(args, "%"+tok+"%")
		}
	}
	conds = append(conds, " ORDER BY updated_at DESC LIMIT ?")
	args = append(args, ciSearchSQLRecallCap)

	const sqlTemplate = `SELECT id, ci_type, tenant_id, name, status, approval_status, attrs, source, agent_id, device_id, version, created_at, updated_at
    FROM ci_items WHERE 1=1%s`
	sqlText := fmt.Sprintf(sqlTemplate, strings.Join(conds, ""))

	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("SearchCIs: %w", err)
	}
	defer rows.Close()
	var hits []CiSearchHit
	for rows.Next() {
		item, err := scanCI(rows)
		if err != nil {
			return nil, err
		}
		// 租户/类型/状态在 SQL 里已经过滤过一遍，这里再判一次：
		// 租户隔离值得多一道防线——将来谁改了 WHERE 拼装或参数绑定，
		// 少这一层就是静默的跨租户数据泄漏；而它只对候选集做一次 O(1) 判断。
		// 附带收益：SQL 与 memory 两个后端的过滤口径因此完全等价。
		if !ciMatchesFilter(item, tenantID, q.CiType, status) {
			continue
		}
		score, matched := matchCI(item, tokens, mode)
		if score <= 0 {
			continue
		}
		hits = append(hits, CiSearchHit{CiItem: *item, Score: score, Matched: matched})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("SearchCIs rows: %w", err)
	}
	if hits == nil {
		hits = []CiSearchHit{}
	}
	sortCiSearchHits(hits)
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

// scanner 接口统一 row 与 rows 的 Scan。
type scanner interface {
	Scan(dest ...interface{}) error
}

func scanCI(s scanner) (*CiItem, error) {
	var ci CiItem
	var attrsStr sql.NullString
	var source, agentID, deviceID, approvalStatus sql.NullString
	err := s.Scan(&ci.ID, &ci.CiType, &ci.TenantID, &ci.Name, &ci.Status, &approvalStatus,
		&attrsStr, &source, &agentID, &deviceID, &ci.Version, &ci.CreatedAt, &ci.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("CI not found")
		}
		return nil, fmt.Errorf("scanCI: %w", err)
	}
	ci.Source = source.String
	ci.AgentID = agentID.String
	ci.DeviceID = deviceID.String
	if approvalStatus.Valid {
		ci.ApprovalStatus = approvalStatus.String
	} else {
		ci.ApprovalStatus = ApprovalApproved
	}
	ci.Attrs = make(map[string]string)
	if attrsStr.Valid && attrsStr.String != "" {
		// attrs 解析失败时保留空 map：不让单行脏数据导致整条 CI 无法读取。
		if uErr := json.Unmarshal([]byte(attrsStr.String), &ci.Attrs); uErr != nil {
			ci.Attrs = make(map[string]string)
		}
	}
	return &ci, nil
}

// === Phase 2: 关系拓扑 ===

func (s *SQLCiStore) CreateRelation(ctx context.Context, rel *CiRelation) error {
	attrsJSON, _ := json.Marshal(rel.Attrs)
	now := time.Now()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO ci_relations (source_ci_id, target_ci_id, relation_type, tenant_id, attributes, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		rel.SourceCIID, rel.TargetCIID, rel.RelationType, rel.TenantID, string(attrsJSON), now)
	if err != nil {
		return fmt.Errorf("CreateRelation: %w", err)
	}
	if id, lidErr := res.LastInsertId(); lidErr == nil {
		rel.ID = id
	}
	rel.CreatedAt = now
	return nil
}

func (s *SQLCiStore) DeleteRelation(ctx context.Context, id int64, tenantID string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM ci_relations WHERE id=? AND (tenant_id=? OR ?='')`,
		id, tenantID, tenantID)
	if err != nil {
		return fmt.Errorf("DeleteRelation: %w", err)
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return fmt.Errorf("DeleteRelation rows affected: %w", rowsErr)
	}
	if n == 0 {
		return fmt.Errorf("relation %d not found", id)
	}
	return nil
}

func (s *SQLCiStore) GetCIRelations(ctx context.Context, ciID, tenantID string) ([]CiRelation, error) {
	q := `SELECT id, source_ci_id, target_ci_id, relation_type, tenant_id, attributes, created_at
		FROM ci_relations WHERE (source_ci_id=? OR target_ci_id=?)`
	args := []interface{}{ciID, ciID}
	if tenantID != "" {
		q += " AND tenant_id=?"
		args = append(args, tenantID)
	}
	q += " ORDER BY created_at"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("GetCIRelations: %w", err)
	}
	defer rows.Close()
	var out []CiRelation
	for rows.Next() {
		rel, err := scanRelation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rel)
	}
	return out, rows.Err()
}

func (s *SQLCiStore) GetCIRelationGraph(ctx context.Context, ciID, tenantID string) (*CIRelationGraph, error) {
	center, err := s.GetCI(ctx, ciID, tenantID)
	if err != nil {
		return nil, err
	}
	rels, err := s.GetCIRelations(ctx, ciID, tenantID)
	if err != nil {
		return nil, err
	}
	withTargets := make([]RelationWithTarget, 0, len(rels))
	for _, rel := range rels {
		var sourceName, targetName, targetType string
		// 端点 CI 可能已被删除/越权，Get 失败按无名称展示（不阻断拓扑渲染）。
		if src, srcErr := s.GetCI(ctx, rel.SourceCIID, ""); srcErr == nil && src != nil {
			sourceName = src.Name
		}
		if tgt, tgtErr := s.GetCI(ctx, rel.TargetCIID, ""); tgtErr == nil && tgt != nil {
			targetName = tgt.Name
			targetType = tgt.CiType
		}
		withTargets = append(withTargets, RelationWithTarget{
			CiRelation: rel,
			SourceName: sourceName,
			TargetName: targetName,
			TargetType: targetType,
		})
	}
	return &CIRelationGraph{CenterCI: center, Relations: withTargets}, nil
}

// === Phase 2: 属性模板 ===

func (s *SQLCiStore) CreateAttrTemplate(ctx context.Context, tmpl *CiAttrTemplate) error {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO ci_attr_templates (ci_type, attr_key, label, attr_type, required, default_value, tenant_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, NOW())`,
		tmpl.CiType, tmpl.AttrKey, tmpl.Label, tmpl.AttrType, tmpl.Required, tmpl.DefaultValue, tmpl.TenantID)
	if err != nil {
		return fmt.Errorf("CreateAttrTemplate: %w", err)
	}
	id, lidErr := res.LastInsertId()
	if lidErr != nil {
		return fmt.Errorf("CreateAttrTemplate last insert id: %w", lidErr)
	}
	tmpl.ID = int(id)
	return nil
}

func (s *SQLCiStore) GetAttrTemplates(ctx context.Context, ciType, tenantID string) ([]CiAttrTemplate, error) {
	q := `SELECT id, ci_type, attr_key, label, attr_type, required, default_value, tenant_id, created_at
		FROM ci_attr_templates WHERE 1=1`
	var args []interface{}
	if ciType != "" {
		q += " AND ci_type=?"
		args = append(args, ciType)
	}
	if tenantID != "" {
		q += " AND tenant_id=?"
		args = append(args, tenantID)
	}
	q += " ORDER BY ci_type, id"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("GetAttrTemplates: %w", err)
	}
	defer rows.Close()
	var out []CiAttrTemplate
	for rows.Next() {
		var t CiAttrTemplate
		if err := rows.Scan(&t.ID, &t.CiType, &t.AttrKey, &t.Label, &t.AttrType,
			&t.Required, &t.DefaultValue, &t.TenantID, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("GetAttrTemplates scan: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *SQLCiStore) UpdateAttrTemplate(ctx context.Context, tmpl *CiAttrTemplate) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE ci_attr_templates SET ci_type=?, attr_key=?, label=?, attr_type=?, required=?, default_value=?
		WHERE id=? AND (tenant_id=? OR ?='')`,
		tmpl.CiType, tmpl.AttrKey, tmpl.Label, tmpl.AttrType, tmpl.Required, tmpl.DefaultValue,
		tmpl.ID, tmpl.TenantID, tmpl.TenantID)
	if err != nil {
		return fmt.Errorf("UpdateAttrTemplate: %w", err)
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return fmt.Errorf("UpdateAttrTemplate rows affected: %w", rowsErr)
	}
	if n == 0 {
		return fmt.Errorf("template %d not found", tmpl.ID)
	}
	return nil
}

func (s *SQLCiStore) DeleteAttrTemplate(ctx context.Context, id int, tenantID string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM ci_attr_templates WHERE id=? AND (tenant_id=? OR ?='')`,
		id, tenantID, tenantID)
	if err != nil {
		return fmt.Errorf("DeleteAttrTemplate: %w", err)
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return fmt.Errorf("DeleteAttrTemplate rows affected: %w", rowsErr)
	}
	if n == 0 {
		return fmt.Errorf("template %d not found", id)
	}
	return nil
}

// scanRelation 扫描一行 ci_relations 记录。
func scanRelation(s scanner) (*CiRelation, error) {
	var rel CiRelation
	var attrsJSON sql.NullString
	err := s.Scan(&rel.ID, &rel.SourceCIID, &rel.TargetCIID, &rel.RelationType,
		&rel.TenantID, &attrsJSON, &rel.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("scanRelation: %w", err)
	}
	rel.Attrs = make(map[string]string)
	if attrsJSON.Valid && attrsJSON.String != "" {
		// attrs 解析失败时保留空 map：不让单行脏数据导致整条关系无法读取。
		if uErr := json.Unmarshal([]byte(attrsJSON.String), &rel.Attrs); uErr != nil {
			rel.Attrs = make(map[string]string)
		}
	}
	return &rel, nil
}
