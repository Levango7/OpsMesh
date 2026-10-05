package cmdb

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/Levango7/OpsMesh/internal/fulltext"
)

// ---------------------------------------------------------------------------
// 分流判据的单元测试（不碰 DB）
// ---------------------------------------------------------------------------

// TestCISearchTokenUseFulltext 锁定"哪些 token 能走全文索引"的判据。
//
// 这组用例直接对应 020 迁移注释里记录的本机实测结论，任何一条被改动都必须同步更新
// 迁移注释并重新实测——判据写错的后果是中文检索静默返回空结果（漏召回），
// 而漏召回在测试期没有任何信号，只会在用户查询时暴露。
func TestCISearchTokenUseFulltext(t *testing.T) {
	cases := []struct {
		tok    string
		want   bool
		reason string
	}{
		{"web", true, "长度 3 的 ASCII 词，实测 MATCH ⊇ LIKE"},
		{"db", true, "长度 2 的 ASCII 词，实测一致"},
		{"cn", true, "长度 2 的 ASCII 词，实测一致"},
		{"north", true, "长度 5，实测一致"},
		{"生产", true, "长度 2 的中文词，ngram 双字词元可命中"},
		{"机房", true, "长度 2 的中文词，实测一致"},
		{"订单服", true, "长度 3 的中文词，实测一致"},
		{"生", false, "单字：Tokenize 按字切中文，ngram 按双字切，实测 35/35 召回为空"},
		{"产", false, "单字：同上，走 MATCH 会漏召回"},
		{"张", false, "单字：同上"},
		{"北", false, "单字：同上"},
		{"w", false, "单字符 ASCII 同样召回为空（实测），不能只按「是否中文」判断"},
		{"a", false, "单字符 ASCII，同上"},
		{"server_prod", false, "含下划线：ngram 按字面量处理，LIKE 里 '_' 是通配符，语义不同"},
		{"db_slave_01", false, "含下划线，同上"},
		{"web01", true, "长度 5 无下划线，可走 MATCH（无命中也只是空结果，不是漏召回）"},
		{"", false, "空串防御：不应走到 MATCH"},
	}
	for _, c := range cases {
		if got := ciSearchTokenUseFulltext(c.tok); got != c.want {
			t.Errorf("ciSearchTokenUseFulltext(%q) = %v, want %v（%s）", c.tok, got, c.want, c.reason)
		}
	}
}

// TestCISearchTokenUseFulltextCountsRunes 确认长度判定按 rune 而非字节。
//
// 中文在 UTF-8 下占 3 字节：若误用 len(tok) >= 2，"生" 会被算成 3 而放行，
// 单字 token 就会走进 MATCH 路径——正是本分流要防的那个漏召回。
// 顺带也钉住反向：双字中文必须放行，不能因为"看起来像中文"就一律排除。
func TestCISearchTokenUseFulltextCountsRunes(t *testing.T) {
	for _, tok := range []string{"生", "产", "机", "房", "订", "北", "w", "a"} {
		if len([]rune(tok)) != 1 {
			t.Fatalf("用例前提不成立：%q 的 rune 数应为 1", tok)
		}
		if ciSearchTokenUseFulltext(tok) {
			t.Errorf("token %q：字节数 len=%d、字符数 len([]rune)=1，单字符不应放行到 MATCH（"+
				"若按字节判定，中文单字占 3 字节会被误放行，导致漏召回）", tok, len(tok))
		}
	}
	if !ciSearchTokenUseFulltext("生产") {
		t.Error("双字中文词应放行到 MATCH（ngram 按双字切，可命中）")
	}
}

// ---------------------------------------------------------------------------
// 索引探测
// ---------------------------------------------------------------------------

// newProbeMock 构造一个只预期"全文索引探测查询"的 sqlmock。
func newProbeMock(t *testing.T, count int) (*SQLCiStore, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectQuery("information_schema.statistics").
		WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(count))
	return &SQLCiStore{db: db}, mock
}

// TestFulltextProbeDetectsIndex 索引存在时探测为就绪。
func TestFulltextProbeDetectsIndex(t *testing.T) {
	s, mock := newProbeMock(t, 3)
	if !s.isCISearchFulltextReady(context.Background()) {
		t.Error("索引存在时应探测为就绪")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("探测查询未按预期执行: %v", err)
	}
}

// TestFulltextProbeDetectsMissingIndex 索引不存在时退回 LIKE。
func TestFulltextProbeDetectsMissingIndex(t *testing.T) {
	s, _ := newProbeMock(t, 0)
	if s.isCISearchFulltextReady(context.Background()) {
		t.Error("索引不存在时应探测为未就绪")
	}
}

// TestFulltextProbeCachedPerStore 探测每个 store 只打一次。
//
// 需求来源：探测是元数据查询，若每次检索都打一次，等于给每次查询加一次额外往返。
// 这里连打三次 SearchCIs，断言 information_schema 查询只发生一次。
func TestFulltextProbeCachedPerStore(t *testing.T) {
	s, mock := newProbeMock(t, 1)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if !s.isCISearchFulltextReady(ctx) {
			t.Fatalf("第 %d 次探测应为就绪（结果应被缓存）", i+1)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("探测查询次数不符，期望恰好 1 次: %v", err)
	}
}

// TestFulltextProbeErrorTreatedAsNotReady 探测失败必须退回 LIKE 而不是让检索报错。
//
// 方向很重要：退回 LIKE 只是慢（并受候选窗口限制），探测失败却让查询直接失败
// 则是把一个"索引优化"变成了"检索不可用"。故任何探测错误都按未就绪处理。
func TestFulltextProbeErrorTreatedAsNotReady(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	mock.ExpectQuery("information_schema.statistics").
		WillReturnError(errors.New("db error"))
	s := &SQLCiStore{db: db}
	if s.isCISearchFulltextReady(context.Background()) {
		t.Error("探测出错时应按未就绪处理（退回 LIKE），不应报就绪")
	}
}

// ---------------------------------------------------------------------------
// 召回路径分流（端到端 SQL 形状）
// ---------------------------------------------------------------------------

// argRecorder 实现 driver.ValueConverter，顺带把每次转换的参数记下来。
//
// 为什么不用 sqlmock 自带的 WithArgs：那条路要求**事先知道**参数值，
// 而这里要验证的恰恰是"运行时生成的参数形态"（MATCH 带 + 前缀、LIKE 带 % 包裹），
// 事先写死就变成了自证。只提供记录、不做校验。
type argRecorder struct {
	args *[]driver.NamedValue
}

func (r argRecorder) ConvertValue(v any) (driver.Value, error) {
	*r.args = append(*r.args, driver.NamedValue{Ordinal: len(*r.args) + 1, Value: v})
	return v, nil
}

// pinFulltextProbe 把 store 的索引探测结果直接钉死，跳过真实探测查询。
//
// 为什么既有测试需要它：SearchCIs 每次首次检索会先打一次 information_schema 探测，
// 而那些测试的 sqlmock 只为召回查询注册了期望（正则如 "SELECT id, ci_type"）。
// 探测查询会被 mock 拒绝 → 探针报错 → 退回 LIKE → 测试**碰巧**通过。
// 这有三个问题：①通过依赖的是错误路径，一旦探测改成"出错即报错"就会连带崩掉；
// ②每次运行刷一条错误日志，掩盖真实失败；③测试意图不明确——它测的是 LIKE 召回形状，
//
//	就该显式声明"索引未就绪"。故统一用本函数显式表达。
func pinFulltextProbe(s *SQLCiStore, ready bool) *SQLCiStore {
	s.fulltextProbed = true
	s.fulltextReady = ready
	return s
}

// captureSearchSQL 跑一次 SearchCIs，返回真实 SQL 与实参列表。
//
// 用 sqlmock.QueryMatcherFunc 捕获 SQL，用 ValueConverterOption 捕获参数——
// QueryMatcher 只在 SQL 文本匹配时触发，因此必须**先注册期望**，
// 否则期望为空时查询直接报 "was not expected"，matcher 根本不会被调用。
func captureSearchSQL(t *testing.T, s *SQLCiStore, query, ciType string) (string, []driver.NamedValue) {
	t.Helper()
	var captured string
	var gotArgs []driver.NamedValue
	db, mock, err := sqlmock.New(
		sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(func(expected, actual string) error {
			captured = actual
			return nil
		})),
		sqlmock.ValueConverterOption(argRecorder{args: &gotArgs}),
	)
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	mock.MatchExpectationsInOrder(false)
	// 探测已在 newStoreWithProbe 里固化缓存，本轮只会发出真正的检索查询。
	mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows(searchRowsCols()))

	old := s.db
	s.db = db
	defer func() { s.db = old }()
	if _, err := s.SearchCIs(context.Background(), "t1", CiSearchQuery{
		Query: query, CiType: ciType, Status: "active", Limit: 5,
	}); err != nil {
		t.Fatalf("SearchCIs: %v", err)
	}
	return captured, gotArgs
}

// newStoreWithProbe 构造一个探测结果为指定值的 store（探测已在构造时完成）。
func newStoreWithProbe(t *testing.T, indexCnt int) *SQLCiStore {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.MatchExpectationsInOrder(false)
	mock.ExpectQuery("information_schema.statistics").
		WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(indexCnt))
	s := &SQLCiStore{db: db}
	// 立刻探测一次，把结果固化进缓存，后续检索不再打探测查询。
	s.isCISearchFulltextReady(context.Background())
	return s
}

// TestSearchCIsMixedTokensDispatchPerToken 混合查询必须逐 token 分流。
//
// 查询 "web 生产" 被分词为 web / 生 / 产：
//   - web 长度 3 无下划线 → MATCH，参数 "+web"
//   - 生 / 产 单字 → LIKE，参数 "%生%" / "%产%"
//
// 若实现图省事把整个查询切成一种路径，这个用例会立刻失败——而它恰恰是最容易
// 被"看起来能跑"掩盖的错误形态。
func TestSearchCIsMixedTokensDispatchPerToken(t *testing.T) {
	s := newStoreWithProbe(t, 1)
	sqlText, args := captureSearchSQL(t, s, "web 生产", "machine")

	if !strings.Contains(sqlText, "MATCH(name, ci_type, ci_attrs_text) AGAINST(? IN BOOLEAN MODE)") {
		t.Errorf("长度>=2 的 token 应产生 MATCH 片段:\n%s", sqlText)
	}
	// "web 生产" → 生、产 两个单字 token，每个 7 个 LIKE 占位符
	if n := strings.Count(sqlText, "LIKE ?"); n != 2*len(ciSearchColumns) {
		t.Errorf("LIKE 占位符数 = %d, want %d（2 个单字 token × %d 列）\n%s",
			n, 2*len(ciSearchColumns), len(ciSearchColumns), sqlText)
	}
	if n := strings.Count(sqlText, "MATCH("); n != 1 {
		t.Errorf("MATCH 片段数 = %d, want 1\n%s", n, sqlText)
	}
	// 参数侧同样要验证：MATCH 的参数必须带 "+" 前缀（关闭 50% 阈值），
	// 否则高频词在长表里会突然搜不到。
	if !containsArg(args, "+web") {
		t.Errorf("MATCH 参数应为 \"+web\"（BOOLEAN 模式加 + 关闭 50%% 阈值），实际参数: %v", args)
	}
	if !containsArg(args, "%生%") || !containsArg(args, "%产%") {
		t.Errorf("单字 token 应以 %%%%tok%% 形式走 LIKE，实际参数: %v", args)
	}
}

// TestSearchCIsAllSingleCharTokensUseLike 纯中文查询全部走 LIKE。
//
// 这是最关键的一条回归防线：单字 token 走 MATCH 会让中文检索返回空。
// 索引就绪也必须走 LIKE。
func TestSearchCIsAllSingleCharTokensUseLike(t *testing.T) {
	s := newStoreWithProbe(t, 1)
	sqlText, _ := captureSearchSQL(t, s, "生产机房", "")

	if strings.Contains(sqlText, "MATCH(") {
		t.Errorf("纯中文（全部单字 token）不应出现 MATCH 片段:\n%s", sqlText)
	}
	if n := strings.Count(sqlText, "LIKE ?"); n != 4*len(ciSearchColumns) {
		t.Errorf("LIKE 占位符数 = %d, want %d（生产机房 → 4 个单字 token × %d 列）\n%s",
			n, 4*len(ciSearchColumns), len(ciSearchColumns), sqlText)
	}
}

// TestSearchCIsUnderscoreTokenUsesLike 含下划线的 token 必须走 LIKE。
func TestSearchCIsUnderscoreTokenUsesLike(t *testing.T) {
	s := newStoreWithProbe(t, 1)
	sqlText, _ := captureSearchSQL(t, s, "server_prod", "")

	if strings.Contains(sqlText, "MATCH(") {
		t.Errorf("含下划线的 token 不应走 MATCH（ngram 按字面量处理，会漏召回）:\n%s", sqlText)
	}
	if n := strings.Count(sqlText, "LIKE ?"); n != len(ciSearchColumns) {
		t.Errorf("LIKE 占位符数 = %d, want %d\n%s", n, len(ciSearchColumns), sqlText)
	}
}

// TestSearchCIsFallsBackToLikeWhenNoIndex 索引不存在时全部走 LIKE，且不影响检索成功。
func TestSearchCIsFallsBackToLikeWhenNoIndex(t *testing.T) {
	s := newStoreWithProbe(t, 0) // 索引不存在
	sqlText, _ := captureSearchSQL(t, s, "web 生产", "machine")

	if strings.Contains(sqlText, "MATCH(") {
		t.Errorf("索引未就绪时不应出现 MATCH 片段:\n%s", sqlText)
	}
	wantTokens := len(dedupeStrings(fulltext.Tokenize("web 生产")))
	if n := strings.Count(sqlText, "LIKE ?"); n != wantTokens*len(ciSearchColumns) {
		t.Errorf("LIKE 占位符数 = %d, want %d\n%s", n, wantTokens*len(ciSearchColumns), sqlText)
	}
}

// TestSearchCIsPreservesFilterCondsWhenFulltext 分流不得破坏租户/状态/类型过滤。
//
// 这是安全相关的断言：过滤条件与召回条件的拼接顺序若被改动，最坏情况是
// 跨租户数据泄漏——过滤条件失效，且检索照常返回结果，测试期不易察觉。
func TestSearchCIsPreservesFilterCondsWhenFulltext(t *testing.T) {
	cases := []struct {
		name     string
		indexCnt int
	}{
		{"索引就绪", 1},
		{"索引未就绪", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newStoreWithProbe(t, tc.indexCnt)
			sqlText, args := captureSearchSQL(t, s, "web 生产", "machine")
			for _, want := range []string{"tenant_id=?", "status=?", "ci_type=?", "LIMIT ?"} {
				if !strings.Contains(sqlText, want) {
					t.Errorf("缺少过滤条件 %q:\n%s", want, sqlText)
				}
			}
			// 参数顺序即绑定顺序：过滤参数在前，召回参数在后。
			// 顺序错乱不会编译失败，只会让 tenant_id 收到召回词，表现为结果恒空。
			wantHead := []string{"t1", "active", "machine"}
			if len(args) < len(wantHead) {
				t.Fatalf("参数数量不足: %v", args)
			}
			for i, w := range wantHead {
				if got := fmt.Sprint(args[i].Value); got != w {
					t.Errorf("第 %d 个参数 = %q, want %q（过滤参数必须在前，顺序错位会导致结果恒空且无报错）: %v",
						i, got, w, args)
				}
			}
		})
	}
}

// TestSearchCIsCondsArgsAligned 分流后 conds 与 args 仍严格一一对应。
func TestSearchCIsCondsArgsAligned(t *testing.T) {
	for _, tc := range []struct {
		name     string
		indexCnt int
		query    string
	}{
		{"索引就绪-混合 token", 1, "web 生产"},
		{"索引未就绪-混合 token", 0, "web 生产"},
		{"索引就绪-纯中文", 1, "生产机房"},
		{"索引就绪-下划线", 1, "server_prod"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newStoreWithProbe(t, tc.indexCnt)
			sqlText, args := captureSearchSQL(t, s, tc.query, "machine")
			placeholders := strings.Count(sqlText, "?")
			if placeholders != len(args) {
				t.Errorf("占位符数 %d != 实参数 %d（conds 与 args 错位会静默返回空结果）\nSQL: %s\nargs: %v",
					placeholders, len(args), sqlText, args)
			}
		})
	}
}

// containsArg 判断实参列表里是否存在指定值。
func containsArg(args []driver.NamedValue, want string) bool {
	for _, a := range args {
		if fmt.Sprint(a.Value) == want {
			return true
		}
	}
	return false
}
