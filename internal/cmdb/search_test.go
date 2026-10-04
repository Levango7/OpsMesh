package cmdb

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Levango7/OpsMesh/internal/fulltext"
)

// ---------------------------------------------------------------------------
// 语料生成器（固定种子，保证测试可复现）
// ---------------------------------------------------------------------------

var searchCorpusNames = []string{
	"web-01", "web-02", "webserver-prod", "web-logic", "db-node-3", "db-standby",
	"redis-cache", "nginx-edge", "k8s-master", "gateway-cn", "订单服务", "支付网关",
}

var searchCorpusTypes = []string{"machine", "os", "service", "app", "cluster"}

var searchCorpusAttrKeys = []string{"ip", "os", "env", "owner", "region", "业务域"}

var searchCorpusAttrVals = []string{
	"10.0.0.1", "10.0.0.2", "192.168.1.7", "CentOS 7", "Ubuntu 22.04",
	"生产环境", "测试环境", "prod", "staging", "cn-north-1", "交易", "payment",
}

var searchCorpusTenants = []string{"t1", "t2"}
var searchCorpusStatus = []string{"active", "active", "active", "archived", "deleted"}

// newSearchCorpus 生成 n 条伪随机 CI（固定种子）。
func newSearchCorpus(n int) []CiItem {
	rng := rand.New(rand.NewSource(20261005))
	out := make([]CiItem, 0, n)
	for i := 0; i < n; i++ {
		attrs := make(map[string]string, 3)
		for j := 0; j < 1+rng.Intn(3); j++ {
			attrs[searchCorpusAttrKeys[rng.Intn(len(searchCorpusAttrKeys))]] =
				searchCorpusAttrVals[rng.Intn(len(searchCorpusAttrVals))]
		}
		out = append(out, CiItem{
			ID:             "ci-" + itoa(i),
			CiType:         searchCorpusTypes[rng.Intn(len(searchCorpusTypes))],
			TenantID:       searchCorpusTenants[rng.Intn(len(searchCorpusTenants))],
			Name:           searchCorpusNames[rng.Intn(len(searchCorpusNames))] + "-" + itoa(i),
			Status:         searchCorpusStatus[rng.Intn(len(searchCorpusStatus))],
			ApprovalStatus: ApprovalApproved,
			Attrs:          attrs,
			Source:         []string{"manual", "agent", "import"}[rng.Intn(3)],
			AgentID:        "agent-" + itoa(rng.Intn(5)),
			DeviceID:       "dev-" + itoa(rng.Intn(5)),
		})
	}
	return out
}

// seedMemoryStore 把语料写入内存 store（走 CreateCI，顺带验证索引维护路径）。
func seedMemoryStore(t *testing.T, items []CiItem) *MemoryCiStore {
	t.Helper()
	s := NewMemoryCiStore()
	for i := range items {
		ci := items[i]
		if err := s.CreateCI(context.Background(), &ci); err != nil {
			t.Fatalf("CreateCI(%s): %v", ci.ID, err)
		}
	}
	return s
}

// ---------------------------------------------------------------------------
// 基础功能
// ---------------------------------------------------------------------------

func TestSearchCIsByNamePrefix(t *testing.T) {
	s := NewMemoryCiStore()
	ctx := context.Background()
	for _, ci := range []CiItem{
		{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "web-01", Status: "active", Attrs: map[string]string{}},
		{ID: "ci-2", CiType: "machine", TenantID: "t1", Name: "webserver-prod", Status: "active", Attrs: map[string]string{}},
		{ID: "ci-3", CiType: "machine", TenantID: "t1", Name: "db-node-3", Status: "active", Attrs: map[string]string{}},
	} {
		c := ci
		if err := s.CreateCI(ctx, &c); err != nil {
			t.Fatalf("CreateCI: %v", err)
		}
	}
	hits, err := s.SearchCIs(ctx, "t1", CiSearchQuery{Query: "web"})
	if err != nil {
		t.Fatalf("SearchCIs: %v", err)
	}
	// 前缀语义：既命中 web-01（切词后是 web），也命中 webserver-prod（整词前缀）。
	if len(hits) != 2 {
		t.Fatalf("SearchCIs(web) = %d hits, want 2: %+v", len(hits), hits)
	}
	if hits[0].ID != "ci-1" && hits[0].ID != "ci-2" {
		t.Errorf("unexpected top hit: %s", hits[0].ID)
	}
	if len(hits[0].Matched) == 0 {
		t.Error("Matched 为空，前端无法做高亮")
	}
}

func TestSearchCIsByAttrAndCJK(t *testing.T) {
	s := NewMemoryCiStore()
	ctx := context.Background()
	for _, ci := range []CiItem{
		{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "host-a", Status: "active",
			Attrs: map[string]string{"ip": "10.0.0.1", "业务域": "交易"}},
		{ID: "ci-2", CiType: "machine", TenantID: "t1", Name: "host-b", Status: "active",
			Attrs: map[string]string{"ip": "192.168.1.7", "业务域": "支付"}},
	} {
		c := ci
		if err := s.CreateCI(ctx, &c); err != nil {
			t.Fatalf("CreateCI: %v", err)
		}
	}
	// 属性值检索（英文切词）
	if hits, _ := s.SearchCIs(ctx, "t1", CiSearchQuery{Query: "192.168"}); len(hits) != 1 || hits[0].ID != "ci-2" {
		t.Errorf("SearchCIs(192.168) = %+v, want [ci-2]", hits)
	}
	// 属性值检索（中文按字）
	if hits, _ := s.SearchCIs(ctx, "t1", CiSearchQuery{Query: "交易"}); len(hits) != 1 || hits[0].ID != "ci-1" {
		t.Errorf("SearchCIs(交易) = %+v, want [ci-1]", hits)
	}
	// 属性键检索
	if hits, _ := s.SearchCIs(ctx, "t1", CiSearchQuery{Query: "ip"}); len(hits) != 2 {
		t.Errorf("SearchCIs(ip) = %d hits, want 2", len(hits))
	}
	// 空检索词返回空结果而非报错
	if hits, err := s.SearchCIs(ctx, "t1", CiSearchQuery{Query: "   "}); err != nil || len(hits) != 0 {
		t.Errorf("SearchCIs(blank) = %+v, err=%v, want empty", hits, err)
	}
}

func TestSearchCIsTenantAndStatusFilter(t *testing.T) {
	ctx := context.Background()
	items := []CiItem{
		{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "web-01", Status: "active", Attrs: map[string]string{}},
		{ID: "ci-2", CiType: "machine", TenantID: "t2", Name: "web-02", Status: "active", Attrs: map[string]string{}},
		{ID: "ci-3", CiType: "machine", TenantID: "t1", Name: "web-03", Status: "deleted", Attrs: map[string]string{}},
	}
	s := seedMemoryStore(t, items)

	// 租户隔离：t1 只能看到自己的，且默认排除 deleted。
	hits, _ := s.SearchCIs(ctx, "t1", CiSearchQuery{Query: "web"})
	if len(hits) != 1 || hits[0].ID != "ci-1" {
		t.Fatalf("租户/状态过滤失效: %+v", hits)
	}
	// 显式指定 status 可搜到已删除项。
	hits, _ = s.SearchCIs(ctx, "t1", CiSearchQuery{Query: "web", Status: "deleted"})
	if len(hits) != 1 || hits[0].ID != "ci-3" {
		t.Fatalf("指定 status=deleted 检索失效: %+v", hits)
	}
	// 类型过滤
	hits, _ = s.SearchCIs(ctx, "t1", CiSearchQuery{Query: "web", CiType: "os"})
	if len(hits) != 0 {
		t.Fatalf("类型过滤失效: %+v", hits)
	}
}

func TestSearchCIsModes(t *testing.T) {
	ctx := context.Background()
	items := []CiItem{
		{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "nginx 生产环境", Status: "active",
			Attrs: map[string]string{"env": "prod"}},
		{ID: "ci-2", CiType: "machine", TenantID: "t1", Name: "nginx 测试环境", Status: "active",
			Attrs: map[string]string{"env": "staging"}},
		{ID: "ci-3", CiType: "machine", TenantID: "t1", Name: "redis", Status: "active",
			Attrs: map[string]string{"env": "prod"}},
	}
	s := seedMemoryStore(t, items)

	// all：两个词都要命中
	if hits, _ := s.SearchCIs(ctx, "t1", CiSearchQuery{Query: "nginx prod", Mode: SearchModeAll}); len(hits) != 1 || hits[0].ID != "ci-1" {
		t.Errorf("mode=all: %+v, want [ci-1]", hits)
	}
	// any：命中任一词，ci-3 只含 prod 也应返回
	if hits, _ := s.SearchCIs(ctx, "t1", CiSearchQuery{Query: "nginx prod", Mode: SearchModeAny}); len(hits) != 3 {
		t.Errorf("mode=any: %d hits, want 3", len(hits))
	}
	// phrase：必须连续出现在同一字段（"nginx 生产" 在 name 里连续）
	if hits, _ := s.SearchCIs(ctx, "t1", CiSearchQuery{Query: "nginx 生产", Mode: SearchModePhrase}); len(hits) != 1 || hits[0].ID != "ci-1" {
		t.Errorf("mode=phrase: %+v, want [ci-1]", hits)
	}
	// 跨字段相邻不构成短语：nginx 在 name、prod 在 attrs
	if hits, _ := s.SearchCIs(ctx, "t1", CiSearchQuery{Query: "nginx prod", Mode: SearchModePhrase}); len(hits) != 0 {
		t.Errorf("跨字段不应构成短语: %+v", hits)
	}
	// 未知 mode 退化为 all（而不是报错）
	if hits, _ := s.SearchCIs(ctx, "t1", CiSearchQuery{Query: "nginx prod", Mode: "bogus"}); len(hits) != 1 {
		t.Errorf("未知 mode 应退化为 all: %+v", hits)
	}
}

func TestSearchCIsLimit(t *testing.T) {
	ctx := context.Background()
	// 语料量必须大于 ciSearchMaxLimit，否则"夹到上限"这条断言永远成立不了。
	items := newSearchCorpus(ciSearchMaxLimit + 100)
	for i := range items {
		items[i].TenantID = "t1"
		items[i].Status = "active"
		items[i].Name = "web-" + itoa(i)
	}
	s := seedMemoryStore(t, items)
	if hits, _ := s.SearchCIs(ctx, "t1", CiSearchQuery{Query: "web"}); len(hits) != ciSearchDefaultLimit {
		t.Errorf("默认 limit = %d, want %d", len(hits), ciSearchDefaultLimit)
	}
	if hits, _ := s.SearchCIs(ctx, "t1", CiSearchQuery{Query: "web", Limit: 10}); len(hits) != 10 {
		t.Errorf("limit=10 → %d hits", len(hits))
	}
	if hits, _ := s.SearchCIs(ctx, "t1", CiSearchQuery{Query: "web", Limit: 99999}); len(hits) != ciSearchMaxLimit {
		t.Errorf("limit 应被夹到 %d, got %d", ciSearchMaxLimit, len(hits))
	}
}

func TestSearchCIsUpdateReflected(t *testing.T) {
	ctx := context.Background()
	s := seedMemoryStore(t, []CiItem{
		{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "oldname", Status: "active", Attrs: map[string]string{}},
	})
	// 改名后：新名可搜到，旧名不应再命中（否则说明索引里残留了旧 posting）。
	updated := CiItem{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "newhost", Status: "active", Attrs: map[string]string{}}
	if err := s.UpdateCI(ctx, &updated); err != nil {
		t.Fatalf("UpdateCI: %v", err)
	}
	if hits, _ := s.SearchCIs(ctx, "t1", CiSearchQuery{Query: "newhost"}); len(hits) != 1 {
		t.Errorf("改名后检索不到新名: %+v", hits)
	}
	if hits, _ := s.SearchCIs(ctx, "t1", CiSearchQuery{Query: "oldname"}); len(hits) != 0 {
		t.Errorf("改名后旧名仍命中，索引未同步: %+v", hits)
	}
}

// ---------------------------------------------------------------------------
// 门禁：倒排索引召回必须等价于暴力扫描
//
// 这条测试锁住"索引维护"这个最容易悄悄腐烂的环节：只要将来有人新增一条
// 写入 CI 的路径却忘了同步索引，召回就会漏项，本测试立即转红。
// ---------------------------------------------------------------------------

func bruteForceSearch(items map[string]CiItem, tokens []string, mode CiSearchMode, limit int, tenantID, ciType, status string) []CiSearchHit {
	var hits []CiSearchHit
	ids := make([]string, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	// 与 candidateIDsLocked 一样的 ID 升序，保证顺序可比
	sort.Strings(ids)
	for _, id := range ids {
		it := items[id]
		if !ciMatchesFilter(&it, tenantID, ciType, status) {
			continue
		}
		score, matched := matchCI(&it, tokens, mode)
		if score <= 0 {
			continue
		}
		hits = append(hits, CiSearchHit{CiItem: it, Score: score, Matched: matched})
	}
	sortCiSearchHits(hits)
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

func TestGateSearchIndexEquivalentToBruteForce(t *testing.T) {
	ctx := context.Background()
	items := newSearchCorpus(200)
	stores := []*MemoryCiStore{seedMemoryStore(t, items)}

	// 再构造一个"部分走过 UpdateCI"的 store，覆盖更新路径的索引维护。
	//
	// 这里必须是**整体替换**而不是追加后缀：若改成 `Name + "-renamed"`，
	// 旧 token 仍然留在名字里，于是"陈旧索引"与"暴力扫描"会得到相同结果，
	// 门禁对漏同步彻底失明（实测过：变异 A 当时只被 TestSearchCIsUpdateReflected 抓到，
	// 本门禁照样绿灯）。替换掉名字与属性后，两侧结果才会真的分叉。
	s2 := seedMemoryStore(t, items)
	for i := 0; i < 40; i++ {
		up := items[i]
		up.Name = "renamed-" + itoa(i)
		up.Attrs = map[string]string{"业务域": " renamed域 " + itoa(i)}
		if err := s2.UpdateCI(context.Background(), &up); err != nil {
			t.Fatalf("UpdateCI: %v", err)
		}
		items[i].Name = up.Name
		items[i].Attrs = up.Attrs
	}
	stores = append(stores, s2)

	queries := []string{
		"web", "db", "redis", "nginx", "prod", "staging", "生产", "交易", "支付",
		"10.0", "192", "cn", "node", "agent", "dev", "k8s", "网关",
		// "renamed" 是专门打更新路径的探针：只有索引跟上了更新才搜得到，
		// 同时被整体替换掉的原名/原属性则应该搜不到。
		"renamed", "renamed 域",
		"web db", "nginx 生产", "redis prod", "订单 支付",
	}
	modes := []CiSearchMode{SearchModeAll, SearchModeAny, SearchModePhrase}
	filters := []struct{ tenant, ciType, status string }{
		{"", "", ""}, {"t1", "", ""}, {"t2", "", ""},
		{"", "machine", ""}, {"", "", "active"}, {"t1", "service", "active"},
	}

	for si, s := range stores {
		for _, query := range queries {
			for _, mode := range modes {
				for _, f := range filters {
					got, err := s.SearchCIs(ctx, f.tenant, CiSearchQuery{
						Query: query, Mode: mode, CiType: f.ciType, Status: f.status, Limit: ciSearchMaxLimit,
					})
					if err != nil {
						t.Fatalf("SearchCIs: %v", err)
					}
					tokens, m, limit, status := normalizeCiSearch(CiSearchQuery{
						Query: query, Mode: mode, CiType: f.ciType, Status: f.status, Limit: ciSearchMaxLimit,
					})
					want := bruteForceSearch(s.items, tokens, m, limit, f.tenant, f.ciType, status)
					if len(got) != len(want) {
						t.Fatalf("store#%d q=%q mode=%s filter=%+v: 索引召回 %d 条，暴力扫描 %d 条（索引漏项）",
							si, query, mode, f, len(got), len(want))
					}
					for i := range got {
						if got[i].ID != want[i].ID {
							t.Fatalf("store#%d q=%q mode=%s filter=%+v: 第 %d 位 %s != %s（顺序不一致）",
								si, query, mode, f, i, got[i].ID, want[i].ID)
						}
						if got[i].Score != want[i].Score {
							t.Fatalf("store#%d q=%q: %s 得分 %f != %f", si, query, got[i].ID, got[i].Score, want[i].Score)
						}
					}
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 门禁：SQL 的 LIKE 召回必须是 matchCI 命中集合的超集
//
// SQL 后端不存索引，召回全靠 LIKE 子串匹配。只要某个 matchCI 命中的 token
// 在原始列文本里不是子串，SQL 后端就会静默漏掉这条 CI——而这个漏洞在
// sqlmock 下永远测不出来（mock 直接返回我们塞进去的行）。下面这条性质测试
// 直接验证超集关系本身。
// ---------------------------------------------------------------------------

func TestGateSQLLikeRecallIsSuperset(t *testing.T) {
	items := newSearchCorpus(200)
	// 召回覆盖的原始列文本（与 ciSearchColumns 一一对应；attrs 用键+值原文，
	// 因为 CAST(attrs AS CHAR) 得到的就是这段 JSON 的文本形态）。
	rawOf := func(it CiItem) []string {
		var attrs []string
		for _, k := range sortedAttrKeys(it.Attrs) {
			attrs = append(attrs, k, it.Attrs[k])
		}
		return []string{it.Name, it.CiType, strings.Join(attrs, " "), it.AgentID, it.DeviceID, it.Source, it.ID}
	}
	tokens := dedupeStrings(nil)
	for _, q := range []string{"web", "db", "redis", "prod", "生产", "交易", "10.0", "cn", "agent", "dev", "node"} {
		tokens = append(tokens, dedupeStrings(fulltext.Tokenize(q))...)
	}
	for _, it := range items {
		fields := ciSearchFields(&it)
		for _, tok := range tokens {
			// matchCI 是否命中？只看"是否有字段含前缀匹配的词"
			hit := false
			for _, f := range fields {
				for _, dt := range f.tokens {
					if strings.HasPrefix(dt, tok) {
						hit = true
					}
				}
			}
			if !hit {
				continue
			}
			// 命中则 LIKE '%tok%' 必须至少匹配一个原始列（大小写不敏感，
			// 与 MySQL 默认 collation 一致）。
			found := false
			lower := strings.ToLower(tok)
			for _, raw := range rawOf(it) {
				if strings.Contains(strings.ToLower(raw), lower) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("CI %s 命中 token %q，但该 token 不是任何列文本的子串 → SQL 后端会漏召回", it.ID, tok)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// SQL 后端
// ---------------------------------------------------------------------------

// ciToSQLRow 把 CI 转成 sqlmock 的一行（列序与 scanCI 一致）。
func ciToSQLRow(t *testing.T, it CiItem) []driver.Value {
	t.Helper()
	attrsJSON, err := json.Marshal(it.Attrs)
	if err != nil {
		t.Fatalf("marshal attrs: %v", err)
	}
	return []driver.Value{it.ID, it.CiType, it.TenantID, it.Name, it.Status, it.ApprovalStatus,
		string(attrsJSON), it.Source, it.AgentID, it.DeviceID, it.Version, it.CreatedAt, it.UpdatedAt}
}

func searchRowsCols() []string {
	return []string{"id", "ci_type", "tenant_id", "name", "status", "approval_status", "attrs",
		"source", "agent_id", "device_id", "version", "created_at", "updated_at"}
}

// TestSQLSearchCIsQueryShape 校验召回 SQL 的形状：租户/状态/类型过滤 + 每 token 一组 LIKE + LIMIT。
func TestSQLSearchCIsQueryShape(t *testing.T) {
	var captured string
	// QueryMatcherFunc 只用于捕获真实 SQL，因此 mock 本身不需要使用。
	// 必须先注册一条期望，sqlmock 才会对着期望调用 matcher（期望为空则查询直接报
	// "was not expected"，matcher 根本不会被触发）。
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(
		sqlmock.QueryMatcherFunc(func(expected, actual string) error {
			captured = actual
			return nil
		})))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows(searchRowsCols()))
	s := &SQLCiStore{db: db}

	_, _ = s.SearchCIs(context.Background(), "t1", CiSearchQuery{
		Query: "web 生产", CiType: "machine", Status: "active", Limit: 5,
	})

	if captured == "" {
		t.Fatal("未捕获到 SQL")
	}
	for _, want := range []string{
		"tenant_id=?", "status=?", "ci_type=?", "LIMIT ?",
	} {
		if !strings.Contains(captured, want) {
			t.Errorf("召回 SQL 缺少 %q:\n%s", want, captured)
		}
	}
	// 每个 token 一组 LIKE。注意 token 数不等于"空格分词数"：中文按字切分，
	// "web 生产" 是 3 个 token（web / 生 / 产），所以这里从分词器反推而不是写死。
	wantTokens := len(dedupeStrings(fulltext.Tokenize("web 生产")))
	if n := strings.Count(captured, "LIKE ?"); n != wantTokens*len(ciSearchColumns) {
		t.Errorf("LIKE 占位符数 = %d, want %d（%d token × %d 列）\n%s",
			n, wantTokens*len(ciSearchColumns), wantTokens, len(ciSearchColumns), captured)
	}
	// 每组 LIKE 都是 AND（token 之间是"与"关系，不能写成 OR）
	if n := strings.Count(captured, ") AND ("); n != wantTokens-1 {
		t.Errorf("token 间应为 AND 连接，组间隔 = %d, want %d\n%s", n, wantTokens-1, captured)
	}
}

// TestSQLSearchCIsScoring SQL 后端必须用与 memory 相同的打分器与过滤逻辑。
func TestSQLSearchCIsScoring(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	s := &SQLCiStore{db: db}
	now := time.Now()

	rows := sqlmock.NewRows(searchRowsCols())
	for _, it := range []CiItem{
		{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "web-01", Status: "active",
			ApprovalStatus: ApprovalApproved, Attrs: map[string]string{"ip": "10.0.0.1"}, Source: "agent", Version: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "ci-2", CiType: "machine", TenantID: "t1", Name: "webserver-prod", Status: "active",
			ApprovalStatus: ApprovalApproved, Attrs: map[string]string{"ip": "10.0.0.2"}, Source: "manual", Version: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "ci-3", CiType: "machine", TenantID: "t1", Name: "db-01", Status: "active",
			ApprovalStatus: ApprovalApproved, Attrs: map[string]string{"ip": "10.0.0.3"}, Source: "manual", Version: 1, CreatedAt: now, UpdatedAt: now},
	} {
		rows.AddRow(ciToSQLRow(t, it)...)
	}
	mock.ExpectQuery("SELECT id, ci_type").WillReturnRows(rows)

	hits, err := s.SearchCIs(context.Background(), "t1", CiSearchQuery{Query: "web"})
	if err != nil {
		t.Fatalf("SearchCIs: %v", err)
	}
	// mock 返回了 3 行（模拟 LIKE 召回超集），但打分器应只留下真正命中的 2 条。
	if len(hits) != 2 {
		t.Fatalf("SQL 检索未剔除无关候选: %d hits %+v", len(hits), hits)
	}
	for _, h := range hits {
		if h.ID == "ci-3" {
			t.Errorf("db-01 不含 web，不应命中: %+v", hits)
		}
		if h.Score <= 0 {
			t.Errorf("命中得分应 > 0: %+v", h)
		}
	}
}

// TestSQLAndMemoryBackendsAgree 两个后端对同一批数据必须给出相同结果。
//
// 这是"打分器只有一份"这条设计约束的可执行证明：一旦有人在某一侧
// 私自改了过滤或排序，这里就会转红。
func TestSQLAndMemoryBackendsAgree(t *testing.T) {
	items := newSearchCorpus(120)
	for i := range items {
		items[i].Status = "active"
	}

	memStore := seedMemoryStore(t, items)
	// 用同一批数据分别喂给两个后端：内存侧走 CreateCI，SQL 侧用 sqlmock 原样返回。
	// 每个租户单独建 mock——sqlmock 的期望是一次性的，复用会报
	// "all expectations were already fulfilled"。
	for _, q := range []string{"web", "生产", "db node", "10.0"} {
		for _, tenant := range []string{"", "t1", "t2"} {
			rows := sqlmock.NewRows(searchRowsCols())
			for _, it := range items {
				rows.AddRow(ciToSQLRow(t, it)...)
			}
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New: %v", err)
			}
			mock.ExpectQuery("SELECT id, ci_type").WillReturnRows(rows)
			sqlStore := &SQLCiStore{db: db}

			memHits, mErr := memStore.SearchCIs(context.Background(), tenant, CiSearchQuery{Query: q, Limit: ciSearchMaxLimit})
			if mErr != nil {
				t.Fatalf("memory SearchCIs: %v", mErr)
			}
			sqlHits, sErr := sqlStore.SearchCIs(context.Background(), tenant, CiSearchQuery{Query: q, Limit: ciSearchMaxLimit})
			if sErr != nil {
				t.Fatalf("sql SearchCIs: %v", sErr)
			}
			if len(memHits) != len(sqlHits) {
				t.Fatalf("q=%q tenant=%q: memory %d 条 vs sql %d 条（两后端口径不一致）",
					q, tenant, len(memHits), len(sqlHits))
			}
			for i := range memHits {
				if memHits[i].ID != sqlHits[i].ID {
					t.Fatalf("q=%q tenant=%q 第 %d 位: memory=%s sql=%s",
						q, tenant, i, memHits[i].ID, sqlHits[i].ID)
				}
				if memHits[i].Score != sqlHits[i].Score {
					t.Fatalf("q=%q 第 %d 位得分不一致: memory=%f sql=%f",
						q, i, memHits[i].Score, sqlHits[i].Score)
				}
			}
			db.Close()
		}
	}
}

// ---------------------------------------------------------------------------
// HTTP 端点
// ---------------------------------------------------------------------------

func newSearchTestStore(t *testing.T) *MemoryCiStore {
	t.Helper()
	return seedMemoryStore(t, []CiItem{
		{ID: "ci-1", CiType: "machine", TenantID: "t1", Name: "web-01", Status: "active",
			Attrs: map[string]string{"ip": "10.0.0.1"}},
		{ID: "ci-2", CiType: "machine", TenantID: "t1", Name: "webserver-prod", Status: "active",
			Attrs: map[string]string{"ip": "10.0.0.2"}},
		{ID: "ci-3", CiType: "machine", TenantID: "t2", Name: "web-02", Status: "active",
			Attrs: map[string]string{"ip": "10.0.0.3"}},
	})
}

func TestHandleCISearch(t *testing.T) {
	s := newSearchTestStore(t)
	mux := http.NewServeMux()
	NewHandler(s).RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/cmdb/ci/search?q=web")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var hits []CiSearchHit
	if err := json.NewDecoder(resp.Body).Decode(&hits); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("检索端点返回空结果")
	}
}

func TestHandleCISearchTenantIsolation(t *testing.T) {
	s := newSearchTestStore(t)
	mux := http.NewServeMux()
	NewHandler(s).RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/cmdb/ci/search?q=web", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	var hits []CiSearchHit
	_ = json.NewDecoder(resp.Body).Decode(&hits)
	if len(hits) != 2 {
		t.Fatalf("t1 应看到 2 条, got %d: %+v", len(hits), hits)
	}
	for _, h := range hits {
		if h.TenantID != "t1" {
			t.Errorf("跨租户泄漏: %+v", h)
		}
	}
}

func TestHandleCISearchBadRequest(t *testing.T) {
	s := newSearchTestStore(t)
	mux := http.NewServeMux()
	NewHandler(s).RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cases := []struct{ name, url string }{
		{"缺少 q", "/api/v1/cmdb/ci/search"},
		{"q 为空", "/api/v1/cmdb/ci/search?q=%20%20"},
		{"limit 非数字", "/api/v1/cmdb/ci/search?q=web&limit=abc"},
		{"limit 为负", "/api/v1/cmdb/ci/search?q=web&limit=-1"},
		{"mode 非法", "/api/v1/cmdb/ci/search?q=web&mode=bogus"},
	}
	for _, c := range cases {
		resp, err := http.Get(srv.URL + c.url)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", c.name, resp.StatusCode)
		}
	}
}

func TestHandleCISearchMethodNotAllowed(t *testing.T) {
	s := newSearchTestStore(t)
	mux := http.NewServeMux()
	NewHandler(s).RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/v1/cmdb/ci/search?q=web", "application/json", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}
