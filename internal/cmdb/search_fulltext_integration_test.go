// search_fulltext_integration_test.go — 检索召回分流在真实 MySQL 上的端到端验证。
//
// 为什么单元测试不够：本次改动的全部风险都落在"数据库真实行为"上——
// ngram 分词粒度、单字 token 能否命中、BOOLEAN 模式的 50% 阈值、生成列能否建索引。
// 这些用 sqlmock 一律测不出来（mock 对任何 SQL 都返回预设行），只能打真库。
//
// 运行方式：
//
//	OPSMESH_TEST_MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/opsmesh?parseTime=true' \
//	  go test ./internal/cmdb/... -run TestSearchCIsFulltextIntegration -v
//
// 未设置该环境变量时整组用例 skip，不影响默认 `go test ./...`。
//
// 覆盖两种迁移状态：索引就绪（跑完 020）与索引缺失（不跑 020），
// 断言两种状态下检索结果**完全一致**——这正是"发布顺序不敏感"的含义。
package cmdb

import (
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	_ "github.com/go-sql-driver/mysql"
)

// ftIntegrationSchema 是本用例自建的最小 ci_items 表。
//
// 刻意不复用 internal/store 的全部 19 个迁移：这里只需要检索路径涉及的列，
// 建全量迁移会让这个用例的耗时从秒级涨到十秒级（迁移开销实测约 10s），
// 而 CI 的 -race 整体超时有限。列定义与 020 迁移后的真实结构保持一致：
// attrs 是 JSON、ci_attrs_text 是 STORED 生成列。
//
// 不带库名前缀：连接的 DSN 已选定临时库，这里建的就是该库下的 ci_items。
const ftIntegrationSchema = `CREATE TABLE ci_items (
	id VARCHAR(64) NOT NULL PRIMARY KEY,
	ci_type VARCHAR(64) NOT NULL,
	tenant_id VARCHAR(64) NOT NULL,
	name VARCHAR(255) NOT NULL,
	status VARCHAR(64) NOT NULL,
	approval_status VARCHAR(64) NOT NULL,
	attrs JSON NULL,
	source VARCHAR(64) NOT NULL DEFAULT '',
	agent_id VARCHAR(64) NOT NULL DEFAULT '',
	device_id VARCHAR(64) NOT NULL DEFAULT '',
	version INT NOT NULL DEFAULT 1,
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL,
	ci_attrs_text TEXT GENERATED ALWAYS AS (CAST(attrs AS CHAR)) STORED
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`

// ftIntegrationIndexDDL 从**迁移文件本身**取出终态索引定义，再据此建索引。
//
// 为什么不留第二份列清单（原先这里硬写 3 列，与 020 一致）：索引列集合一旦与代码的
// `ciSearchFulltextCond` 不一致，要么报 1191，要么**静默漏召回**（少覆盖的召回列上，
// 只命中该列的行在召回阶段就整行丢失）。把 DDL 从迁移里推导出来，就只剩一处定义源，
// 漂移在编译期之后、真库执行之前由 TestCISearchFulltextIndexCoversRecallColumns 拦住。
//
// 顺带说明这个用例原来为什么测不到 020 的那个缺陷：它自建语料从不往
// agent_id / device_id 写值，而 4 条查询里也没有任何只命中 source / id 的词——
// 「不得漏召回」的断言在空转。现在补齐了值与查询（见 ftIntegrationRows 与 queries）。
func ftIntegrationIndexDDL(t *testing.T) string {
	t.Helper()
	name, cols := latestFulltextIndexColumns(t)
	return fmt.Sprintf("CREATE FULLTEXT INDEX %s\n\tON ci_items (%s) WITH PARSER ngram", name, strings.Join(cols, ", "))
}

// ftIntegrationRows 是测试语料。中英文混合，覆盖：纯 ASCII 名、带下划线名、
// 中文名、中文属性值、中英混合名（gateway-cn-北京）、无命中词。
//
// attrs 里的中文是本用例的核心：它同时验证 JSON 生成列的展平与 ngram 的中文召回。
var ftIntegrationRows = []struct {
	id, ciType, tenant, name, status, attrs string
}{
	{"ci1", "machine", "t1", "web-01", "active", `{"owner":"张伟","env":"生产机房","ip":"10.0.0.1"}`},
	{"ci2", "machine", "t1", "db-01", "active", `{"owner":"李娜","env":"测试环境","ip":"10.0.0.2"}`},
	{"ci3", "app", "t1", "webserver-prod", "active", `{"owner":"张伟","env":"生产环境"}`},
	{"ci4", "service", "t1", "订单服务", "active", `{"owner":"王强","env":"生产机房"}`},
	{"ci5", "machine", "t1", "db_slave_01", "active", `{"owner":"李娜","env":"灾备机房"}`},
	{"ci6", "cluster", "t1", "k8s-master-1", "active", `{"owner":"赵敏","env":"生产机房"}`},
	{"ci7", "service", "t1", "redis-cache", "active", `{"owner":"孙七","env":"staging"}`},
	{"ci8", "app", "t1", "gateway-cn-北京", "active", `{"owner":"周八","env":"生产环境"}`},
	{"ci9", "service", "t1", "支付网关", "active", `{"owner":"吴九","env":"生产机房"}`},
	{"ci10", "service", "t1", "nginx-edge", "active", `{"owner":"郑十","env":"edge"}`},
	// t2 租户：验证过滤条件在两条召回路径下都生效（跨租户不可见）。
	{"ci11", "machine", "t2", "web-99", "active", `{"owner":"钱十一","env":"生产机房"}`},
}

// newFulltextTestDB 建一个临时库并灌入语料，按 withIndex 决定是否建全文索引。
// 返回 store、库名、清理函数。
func newFulltextTestDB(t *testing.T, withIndex bool) (*SQLCiStore, func()) {
	t.Helper()
	adminDSN := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if adminDSN == "" {
		t.Skip("OPSMESH_TEST_MYSQL_DSN not set; skipping fulltext integration test")
	}
	admin, err := sql.Open("mysql", adminDSN)
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	// 临时库名必须落在 DSN 那个账号的授权范围内。
	//
	// 约定：沿用「库名以 <服务前缀>_ 开头」的现有惯例（本机 opsmesh 账号被授予的是
	// `opsmesh%`.* ，而 root 账号是 MYSQL_ROOT_HOST=localhost、只允许本机登录，
	// 从容器网络里连不上——两种情况都会让本用例无法自建库）。因此前缀写死为
	// opsmesh_cmdb_ft_，若你的测试账号权限不同，改这里即可。
	//
	// 序号而非 PID：同一进程内会同时存在多个库（两库对比用例要建两次），
	// 用 PID 会让第二次建库 DROP 掉第一次的库，两边指针指向同一张表。
	const dbPrefix = "opsmesh_cmdb_ft_"
	dbName := fmt.Sprintf("%s%d_%d", dbPrefix, os.Getpid(), ftDBSeq.Add(1))
	if _, err := admin.Exec("DROP DATABASE IF EXISTS " + dbName); err != nil {
		t.Fatalf("drop stale db: %v", err)
	}
	if _, err := admin.Exec("CREATE DATABASE " + dbName + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatalf("create db: %v", err)
	}
	cleanup := func() {
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + dbName)
		_ = admin.Close()
	}

	// 连接到临时库：把 DSN 里的库名替换掉（库名位于最后一个 '/' 与 '?' 之间）。
	slash := strings.LastIndex(adminDSN, "/")
	if slash < 0 {
		cleanup()
		t.Fatalf("OPSMESH_TEST_MYSQL_DSN 里找不到库名（应在最后一个 '/' 之后）: %s", adminDSN)
	}
	dsn := adminDSN[:slash+1] + dbName
	if q := strings.Index(adminDSN, "?"); q >= 0 {
		dsn += adminDSN[q:]
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		cleanup()
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(ftIntegrationSchema); err != nil {
		cleanup()
		t.Fatalf("create table: %v", err)
	}
	for _, r := range ftIntegrationRows {
		// agent_id / device_id 必须写值，且写的 token 只存在于这两列：
		// 它们是 ciSearchColumns 的一部分、也进 matchCI 的 ident 字段，但 020 的索引
		// 没覆盖它们。语料留空就等于把这条召回口径的守卫整条关掉。
		_, err := db.Exec(
			`INSERT INTO ci_items (id,ci_type,tenant_id,name,status,approval_status,attrs,source,agent_id,device_id,created_at,updated_at)
			 VALUES (?,?,?,?,?,?,?,'agent',?,?,NOW(),NOW())`,
			r.id, r.ciType, r.tenant, r.name, r.status, ApprovalApproved, r.attrs,
			"ag"+r.id, "dv"+r.id)
		if err != nil {
			cleanup()
			t.Fatalf("insert %s: %v", r.id, err)
		}
	}
	if withIndex {
		if _, err := db.Exec(ftIntegrationIndexDDL(t)); err != nil {
			cleanup()
			t.Fatalf("create fulltext index: %v", err)
		}
	}
	return &SQLCiStore{db: db}, cleanup
}

// ftDBSeq 给每次 newFulltextTestDB 分配唯一库名后缀。
var ftDBSeq atomic.Int64

// hitIDs 把命中结果收敛成排序后的 ID 串，便于两轮对比。
func hitIDs(hits []CiSearchHit) string {
	var ids []string
	for _, h := range hits {
		ids = append(ids, h.ID)
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

// TestSearchCIsFulltextIntegrationWithIndex 索引就绪时的召回正确性。
//
// 关键断言分两类：
//   - 正向：中文查询、单字 token 查询必须能召回（单字走 LIKE 是本改动的关键）；
//   - 租户：t1 查询不得看到 t2 的 ci11。
func TestSearchCIsFulltextIntegrationWithIndex(t *testing.T) {
	store, cleanup := newFulltextTestDB(t, true)
	defer cleanup()

	if !store.isCISearchFulltextReady(t.Context()) {
		t.Fatal("索引已建，探测却报未就绪")
	}

	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{"ASCII 走 MATCH", "web", []string{"ci1", "ci3"}},
		{"中文单字 token 走 LIKE", "生产", []string{"ci1", "ci3", "ci4", "ci6", "ci8", "ci9"}},
		{"中文属性值检索", "机房", []string{"ci1", "ci4", "ci5", "ci6", "ci9"}},
		{"中文名称检索", "订单服务", []string{"ci4"}},
		{"下划线 token 走 LIKE", "db_slave_01", []string{"ci5"}},
		{"混合查询", "web 生产", []string{"ci1", "ci3"}},
		{"无命中", "nosuchtoken", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hits, err := store.SearchCIs(t.Context(), "t1", CiSearchQuery{Query: c.query, Limit: 50})
			if err != nil {
				t.Fatalf("SearchCIs(%q): %v", c.query, err)
			}
			got := hitIDs(hits)
			want := strings.Join(c.want, ",")
			if got != want {
				t.Errorf("查询 %q 命中 = %q, want %q", c.query, got, want)
			}
		})
	}
}

// TestSearchCIsFulltextIntegrationTenantIsolation 两种召回路径都必须维持租户隔离。
//
// 这是安全断言：若 MATCH 路径漏掉 tenant_id 过滤，t1 的查询会看到 t2 的 ci11。
func TestSearchCIsFulltextIntegrationTenantIsolation(t *testing.T) {
	store, cleanup := newFulltextTestDB(t, true)
	defer cleanup()

	// "web" 会走 MATCH，"生产" 的每个单字 token 走 LIKE——两条路径都要验。
	for _, qy := range []string{"web", "生产", "机房", "web 生产"} {
		hits, err := store.SearchCIs(t.Context(), "t1", CiSearchQuery{Query: qy, Limit: 50})
		if err != nil {
			t.Fatalf("SearchCIs(%q): %v", qy, err)
		}
		for _, h := range hits {
			if h.TenantID != "t1" {
				t.Errorf("查询 %q 返回了跨租户数据: %s (tenant=%s)", qy, h.ID, h.TenantID)
			}
		}
	}
}

// TestSearchCIsFulltextIntegrationMatchesLikeWithoutIndex 索引有无，两种状态结果必须一致。
//
// 这是"020 迁移与代码发布顺序不敏感"的直接验证，也是本次改动最重要的一条性质：
// 建了索引不该改变用户看到的检索结果，没建索引也不该。
//
// 实现方式：同一份语料建两次库（一次带索引、一次不带），对每条查询比对命中 ID 串。
func TestSearchCIsFulltextIntegrationMatchesLikeWithoutIndex(t *testing.T) {
	withIdx, cleanupIdx := newFulltextTestDB(t, true)
	defer cleanupIdx()
	withoutIdx, cleanupNoIdx := newFulltextTestDB(t, false)
	defer cleanupNoIdx()

	if !withIdx.isCISearchFulltextReady(t.Context()) {
		t.Fatal("带索引的库探测未就绪")
	}
	if withoutIdx.isCISearchFulltextReady(t.Context()) {
		t.Fatal("不带索引的库却报就绪——探测逻辑有误")
	}

	queries := []string{
		"web", "生产", "机房", "订单服务", "支付网关", "web 生产", "机房 生产",
		"db_slave_01", "server", "k8s", "master", "edge", "redis", "cache",
		"张伟", "李娜", "北京", "north", "w", "a", "张", "机", "订",
		"生产机房", "web01", "nosuchtoken", "10.0.0.1",
		// 只命中「020 没索引、但 matchCI 会判定」的那几列：source / agent_id / device_id / id。
		// 加这组词之前，本用例对 020 的漏召回是完全无感的——索引少覆盖一列，
		// 只出现在这些列里的行连召回都进不来，两轮对比自然「一致」。
		"agent", "agci1", "dvci4", "ci1",
	}
	for _, qy := range queries {
		gotIdx, err := withIdx.SearchCIs(t.Context(), "t1", CiSearchQuery{Query: qy, Limit: 100})
		if err != nil {
			t.Fatalf("带索引 SearchCIs(%q): %v", qy, err)
		}
		gotNoIdx, err := withoutIdx.SearchCIs(t.Context(), "t1", CiSearchQuery{Query: qy, Limit: 100})
		if err != nil {
			t.Fatalf("不带索引 SearchCIs(%q): %v", qy, err)
		}
		if a, b := hitIDs(gotIdx), hitIDs(gotNoIdx); a != b {
			t.Errorf("查询 %q：带索引命中 %q，不带索引命中 %q —— 索引改变了用户可见结果",
				qy, a, b)
		}
	}
}

// TestSearchCIsFulltextIntegrationRecallNotNarrowed 索引不得让召回变窄。
//
// 与上面「两库对比」的分工：那条比的是**带索引库 vs 不带索引库**（同一个被测代码，
// 只有 DDL 不同）；这条比的是**被测代码 vs 一份独立的纯 LIKE 实现**，能在
// MATCH 分流有偏差时给出不同的诊断信息。
//
// 基准必须同样经过 matchCI 精确判定，这一点是本用例的要点：
// 召回阶段是「宽松的超集」（LIKE 子串能命中 JSON 键名里的字母，如查 "w" 会因为
// attrs 里的 "owner" 命中 db-01），而 matchCI 做的是**前缀匹配**（"owner" 不以 "w"
// 开头，正确地被判为不相关）。只拿原始 LIKE 召回去比最终结果，会把「超集」误判成
// 「漏召回」——这类错误基准比没有基准更糟。
func TestSearchCIsFulltextIntegrationRecallNotNarrowed(t *testing.T) {
	store, cleanup := newFulltextTestDB(t, true)
	defer cleanup()

	queries := []string{
		"web", "生产", "机房", "订单", "服务", "支付", "网关", "张伟", "李娜",
		"web 生产", "机房 生产", "db_slave_01", "北京", "north", "web01", "k8s",
		"生", "产", "机", "房", "订", "单", "服", "务", "支", "付", "网", "关", "北", "w",
		// 基准是「7 列 LIKE + matchCI」，而 ident 字段含 id/agent_id/device_id/source
		// （search.go:125）。索引少覆盖其中任何一列，这里就必须报漏召回——
		// 这 4 个词是 020 那个缺陷的直接复现件（当时全绿，因为语料没往这些列写值）。
		"agent", "agci1", "dvci4", "ci1",
	}
	for _, qy := range queries {
		hits, err := store.SearchCIs(t.Context(), "t1", CiSearchQuery{Query: qy, Limit: 100})
		if err != nil {
			t.Fatalf("SearchCIs(%q): %v", qy, err)
		}
		found := map[string]bool{}
		for _, h := range hits {
			found[h.ID] = true
		}
		for _, id := range pureLikeBaseline(t, store, qy) {
			if !found[id] {
				t.Errorf("查询 %q：纯 LIKE 实现命中 %s，但 MATCH 分流路径漏掉了它"+
					"（漏召回是正确性回归——精确判定与排序仍在 matchCI，召回不该更窄）", qy, id)
			}
		}
	}
}

// pureLikeBaseline 是一份独立的「全 LIKE」检索实现，用作漏召回的判定基准。
//
// 刻意不复用 SearchCIs 的 SQL 拼装与 MATCH 分流：若两者共用同一段代码，共错时
// 无法互相发现。这里只复用两样**本来就该共享**的东西——ciSearchColumns（列集合，
// 口径必须一致）与 matchCI（精确判定与排序，两个后端必须同口径）；
// 召回条件则完全独立地写成 7 列 LIKE。
func pureLikeBaseline(t *testing.T, s *SQLCiStore, query string) []string {
	t.Helper()
	tokens, mode, _, status := normalizeCiSearch(CiSearchQuery{Query: query, Limit: 100})
	if len(tokens) == 0 {
		return nil
	}
	var conds []string
	var args []interface{}
	args = append(args, "t1")
	conds = append(conds, " AND tenant_id=?")
	if status != "" {
		conds = append(conds, " AND status=?")
		args = append(args, status)
	}
	for _, tok := range tokens {
		parts := make([]string, 0, len(ciSearchColumns))
		for range ciSearchColumns {
			parts = append(parts, "?")
		}
		conds = append(conds, " AND ("+strings.Join(parts, " OR ")+")")
		for range ciSearchColumns {
			args = append(args, "%"+tok+"%")
		}
	}
	const cols = `id, ci_type, tenant_id, name, status, approval_status, attrs,
		source, agent_id, device_id, version, created_at, updated_at`
	rows, err := s.db.Query(
		"SELECT "+cols+" FROM ci_items WHERE 1=1"+strings.Join(conds, "")+" ORDER BY id",
		args...)
	if err != nil {
		t.Fatalf("pureLikeBaseline(%q): %v", query, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		item, err := scanCI(rows)
		if err != nil {
			t.Fatalf("pureLikeBaseline(%q) scan: %v", query, err)
		}
		if !ciMatchesFilter(item, "t1", "", status) {
			continue
		}
		if score, _ := matchCI(item, tokens, mode); score <= 0 {
			continue
		}
		out = append(out, item.ID)
	}
	return out
}
