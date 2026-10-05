package cmdb

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
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
//
// 两列对应探测 SQL 的两个返回值：索引列清单（GROUP_CONCAT）与 @@ngram_token_size。
// idxCols 传空串表示索引不存在——探测子查询此时返回 **NULL** 而不是空串，这里如实模拟 NULL，
// 否则拿空串测会把「索引不存在」与「列集合不一致」两条分支混成一条。
// 只给一列则会让 Scan 失败并落进「探测出错退 LIKE」分支——那等于用错误路径冒充「未就绪」，
// 断言看起来通过，实际测的却是另一件事（同文件 pinFulltextProbe 记的就是这个教训）。
func newProbeMock(t *testing.T, idxCols string, ngramSize int) (*SQLCiStore, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	rows := sqlmock.NewRows([]string{"index_columns", "ngram_token_size"})
	if idxCols == "" {
		rows.AddRow(nil, ngramSize)
	} else {
		rows.AddRow(idxCols, ngramSize)
	}
	mock.ExpectQuery("information_schema.statistics").WillReturnRows(rows)
	return &SQLCiStore{db: db}, mock
}

// TestFulltextProbeDetectsIndex 索引在位、列集合与代码一致、词元长度为 2 时才为就绪。
func TestFulltextProbeDetectsIndex(t *testing.T) {
	s, mock := newProbeMock(t, ciSearchFulltextColumnList, ciSearchNgramTokenSize)
	if !s.isCISearchFulltextReady(context.Background()) {
		t.Error("索引列集合与代码一致且 ngram_token_size=2 时应探测为就绪")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("探测查询未按预期执行: %v", err)
	}
}

// TestFulltextProbeDetectsMissingIndex 索引不存在（探测返回 NULL）时退回 LIKE。
func TestFulltextProbeDetectsMissingIndex(t *testing.T) {
	s, _ := newProbeMock(t, "", ciSearchNgramTokenSize)
	if s.isCISearchFulltextReady(context.Background()) {
		t.Error("索引不存在时应探测为未就绪")
	}
}

// TestFulltextProbeRejectsColumnSetDrift 索引在位但列集合与代码不一致时必须判未就绪。
//
// 这是 021 引入的新中间态：滚动发布时新二进制（MATCH 要 7 列）配还没跑 021 的旧库
// （020 只建了 3 列索引）。若只按索引名判断，这种状态会被判成「就绪」，随后 MATCH 的列数
// 与索引不符 → MySQL 报 1191 → **整条检索失败**：一次索引升级变成检索不可用。
// 第三种形态（集合相同、顺序不同）也要拒，因为 MATCH 的列清单必须与索引定义同序对应。
func TestFulltextProbeRejectsColumnSetDrift(t *testing.T) {
	for _, cols := range []string{
		"name,ci_type,ci_attrs_text",
		"name,ci_type,ci_attrs_text,agent_id,device_id,source",
		"ci_attrs_text,name,ci_type,agent_id,device_id,source,id",
	} {
		t.Run(cols, func(t *testing.T) {
			s, _ := newProbeMock(t, cols, ciSearchNgramTokenSize)
			if s.isCISearchFulltextReady(context.Background()) {
				t.Errorf("索引列集合 %q 与代码清单 %q 不一致时应判未就绪", cols, ciSearchFulltextColumnList)
			}
		})
	}
}

// TestFulltextProbeRejectsOtherNgramTokenSize 索引在位但词元长度不是 2 时必须判未就绪。
//
// 这条守的是分流判据的前提：判据（长度=1 走 LIKE、>=2 走 MATCH）只在 ngram_token_size=2
// 的库上实测过。词元长度是全局可配变量——设成 3 时长度=2 的检索词短于词元，MATCH 返回空，
// 而「索引存在」的探测一切正常，于是表现为**静默漏召回**。size=1 虽大概率只是变宽，
// 但同样没实测过，故一并拒绝：未验证在本模块里按「可能漏召回」处理。
func TestFulltextProbeRejectsOtherNgramTokenSize(t *testing.T) {
	for _, size := range []int{1, 3, 4} {
		t.Run(fmt.Sprintf("size=%d", size), func(t *testing.T) {
			s, _ := newProbeMock(t, ciSearchFulltextColumnList, size) // 列集合正常，只有词元长度不符
			if s.isCISearchFulltextReady(context.Background()) {
				t.Errorf("ngram_token_size=%d 时应判未就绪（退回 LIKE），不能只看索引在不在", size)
			}
		})
	}
}

// TestExtractMatchColumnListIsSingleSourced 期望列清单必须确实是从 MATCH 常量解析出来的。
//
// 探测拿这个清单比对库里的索引，而清单来自 ciSearchFulltextCond 的解析；解析一旦写错
// （例如把 AGAINST 里的内容也吃进来），探测会恒判不一致、全文索引**静默失效**——
// 现象与「没建索引」一模一样，很难往回查。故把解析结果本身钉成断言。
func TestExtractMatchColumnListIsSingleSourced(t *testing.T) {
	if ciSearchFulltextColumnList == "" {
		t.Fatal("列清单解析为空：探测将恒判未就绪，全文索引静默失效")
	}
	if !slices.Equal(strings.Split(ciSearchFulltextColumnList, ","), matchColumnsOf(t, ciSearchFulltextCond)) {
		t.Errorf("规范化清单 %q 与逐列解析结果不一致", ciSearchFulltextColumnList)
	}
	if strings.Contains(ciSearchFulltextColumnList, "AGAINST") || strings.Contains(ciSearchFulltextColumnList, "?") {
		t.Errorf("解析越界，把匹配表达式当成了列名: %q", ciSearchFulltextColumnList)
	}
}

// TestFulltextProbeCachedPerStore 探测每个 store 只打一次。
//
// 需求来源：探测是元数据查询，若每次检索都打一次，等于给每次查询加一次额外往返。
// 这里连打三次 SearchCIs，断言 information_schema 查询只发生一次。
func TestFulltextProbeCachedPerStore(t *testing.T) {
	s, mock := newProbeMock(t, ciSearchFulltextColumnList, ciSearchNgramTokenSize)
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

// TestFulltextProbeConcurrentFirstTouch 并发首次触碰只能打一次探测，且所有 goroutine 结论一致。
//
// 为什么这条不是 TestFulltextProbeCachedPerStore 的重复：那条是**串行**连打三次，
// 串行下把 fulltextMu 删掉照样全绿，-race 也不会报（没有并发访问）。而真实形态是多个请求
// 同时首次触碰探测。这里用 start channel 让 N 个 goroutine 同时冲进去：
// sqlmock 只注册一条期望，任何重复查询都会报 "was not expected" → 那一路判未就绪，
// 于是「全部为 true」这条断言能抓到重复探测，同时 -race 覆盖 fulltextProbed/fulltextReady
// 两个字段。**没有 -race 的全绿不构成并发正确性证据**，这条是本包唯一的并发证据。
func TestFulltextProbeConcurrentFirstTouch(t *testing.T) {
	const n = 32
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.MatchExpectationsInOrder(false)
	mock.ExpectQuery("information_schema.statistics").
		WillReturnRows(sqlmock.NewRows([]string{"index_columns", "ngram_token_size"}).
			AddRow(ciSearchFulltextColumnList, ciSearchNgramTokenSize))

	s := &SQLCiStore{db: db}
	results := make([]bool, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i] = s.isCISearchFulltextReady(context.Background())
		}(i)
	}
	close(start)
	wg.Wait()

	for i, ok := range results {
		if !ok {
			t.Errorf("goroutine %d 判未就绪：并发下探测被打穿（重复查询会被 sqlmock 拒绝）", i)
			break
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("探测查询应恰好一次: %v", err)
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

// newStoreWithProbe 构造一个探测结果为指定就绪状态的 store（探测已在构造时完成）。
// 列集合取代码里的期望值，词元长度取实测过的 2，故 ready=true 就是「三条件全满足」。
func newStoreWithProbe(t *testing.T, ready bool) *SQLCiStore {
	t.Helper()
	if !ready {
		return newStoreWithProbeSize(t, "", ciSearchNgramTokenSize) // 索引不存在
	}
	return newStoreWithProbeSize(t, ciSearchFulltextColumnList, ciSearchNgramTokenSize)
}

// newStoreWithProbeSize 同上，但可分别指定索引列清单与 ngram_token_size，
// 用来验证「列集合一致」和「词元长度为 2」这两道闸。idxCols 为空串表示索引不存在。
func newStoreWithProbeSize(t *testing.T, idxCols string, ngramSize int) *SQLCiStore {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.MatchExpectationsInOrder(false)
	rows := sqlmock.NewRows([]string{"index_columns", "ngram_token_size"})
	if idxCols == "" {
		rows.AddRow(nil, ngramSize)
	} else {
		rows.AddRow(idxCols, ngramSize)
	}
	mock.ExpectQuery("information_schema.statistics").WillReturnRows(rows)
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
	s := newStoreWithProbe(t, true)
	sqlText, args := captureSearchSQL(t, s, "web 生产", "machine")

	// 断言用常量本身而不是硬写列清单：列集合由迁移与代码对账守住
	// （TestCISearchFulltextIndexCoversRecallColumns），这里再抄一份 3 列/7 列字符串
	// 只会变成第三处定义源，改一处就得跟着改这里。
	if !strings.Contains(sqlText, ciSearchFulltextCond) {
		t.Errorf("长度>=2 的 token 应产生 MATCH 片段（应为 %q）:\n%s", ciSearchFulltextCond, sqlText)
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
	s := newStoreWithProbe(t, true)
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
	s := newStoreWithProbe(t, true)
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
	s := newStoreWithProbe(t, false) // 索引不存在
	sqlText, _ := captureSearchSQL(t, s, "web 生产", "machine")

	if strings.Contains(sqlText, "MATCH(") {
		t.Errorf("索引未就绪时不应出现 MATCH 片段:\n%s", sqlText)
	}
	wantTokens := len(dedupeStrings(fulltext.Tokenize("web 生产")))
	if n := strings.Count(sqlText, "LIKE ?"); n != wantTokens*len(ciSearchColumns) {
		t.Errorf("LIKE 占位符数 = %d, want %d\n%s", n, wantTokens*len(ciSearchColumns), sqlText)
	}
}

// TestNgramTokenSizeGateReachesRecallPath 词元长度这道闸必须真的改变发出去的 SQL。
//
// 只断言探测函数的返回值不够：探测与召回之间还隔着一次 useFulltext 传递。若那里读错了
// 值（例如仍只看索引计数），就会出现本闸要防的形态——探测判未就绪、查询照旧走 MATCH，
// 而 MATCH 在非实测的词元长度上可能是静默漏召回。故这条测到端到端形状。
func TestNgramTokenSizeGateReachesRecallPath(t *testing.T) {
	s := newStoreWithProbeSize(t, ciSearchFulltextColumnList, 3) // 索引在位，词元长度不是实测过的 2
	sqlText, args := captureSearchSQL(t, s, "web 生产", "")

	if strings.Contains(sqlText, "MATCH(") {
		t.Errorf("ngram_token_size≠2 时不得走 MATCH:\n%s", sqlText)
	}
	if containsArg(args, "+web") {
		t.Errorf("不应出现 MATCH 的 + 前缀参数（说明闸门没生效）: %v", args)
	}
	// web / 生 / 产 三个 token 全部退回 LIKE
	if n := strings.Count(sqlText, "LIKE ?"); n != 3*len(ciSearchColumns) {
		t.Errorf("LIKE 占位符数 = %d, want %d（3 个 token × %d 列）\n%s",
			n, 3*len(ciSearchColumns), len(ciSearchColumns), sqlText)
	}
}

// TestColumnSetDriftFallsBackToLike 索引列集合与代码不一致时，检索必须整体退回 LIKE。
//
// 这条测的是 021 自己带来的新风险：滚动发布时新二进制（MATCH 要 7 列）配还没跑 021 的旧库
// （只有 020 的 3 列索引）。那种状态下发出 MATCH 会当场报 1191，**整条检索失败**——
// 比慢更糟，也直接推翻「迁移与代码发布顺序不敏感」。断到 SQL 形状而不是只断探测返回值，
// 是因为「判未就绪」和「真的没发 MATCH」之间还隔着一次 useFulltext 传递。
func TestColumnSetDriftFallsBackToLike(t *testing.T) {
	s := newStoreWithProbeSize(t, "name,ci_type,ci_attrs_text", ciSearchNgramTokenSize) // 020 的形态
	sqlText, args := captureSearchSQL(t, s, "web 生产", "")

	if strings.Contains(sqlText, "MATCH(") {
		t.Errorf("索引列集合与代码不一致时不得发出 MATCH（MySQL 会报 1191，检索整体失败）:\n%s", sqlText)
	}
	if containsArg(args, "+web") {
		t.Errorf("不应出现 MATCH 的 + 前缀参数（说明列集合这道闸没生效）: %v", args)
	}
	if n := strings.Count(sqlText, "LIKE ?"); n != 3*len(ciSearchColumns) {
		t.Errorf("LIKE 占位符数 = %d, want %d（web/生/产 三个 token × %d 列）\n%s",
			n, 3*len(ciSearchColumns), len(ciSearchColumns), sqlText)
	}
}

// TestChineseQueryTokensNeverReachFulltext 钉住「收益边界」的根因：查询侧把中文按**单字**切。
//
// 这条补的是分流判据与真实链路之间缺失的一环：ciSearchTokenUseFulltext("生产") 返回 true，
// 但 Tokenize 从不产出长度 >=2 的中文 token，所以 MATCH 对中文恒不可达（中文检索的收益为零，
// 改善面只在 ASCII 词）。哪天把 Tokenize 改成按词切中文，这条会立刻判红——那时必须
// **重新实测** MATCH 的召回口径（现有实测只在单字 token 与 ASCII 词上做过），而不是默认沿用旧结论。
func TestChineseQueryTokensNeverReachFulltext(t *testing.T) {
	for _, q := range []string{"生产机房", "张伟", "订单服务", "核心交易库", "生产 web", "机房-02"} {
		toks := fulltext.Tokenize(q)
		if len(toks) == 0 {
			t.Fatalf("查询 %q 分词为空，用例前提不成立", q)
		}
		for _, tok := range toks {
			if !containsCJK(tok) {
				continue
			}
			if n := len([]rune(tok)); n != 1 {
				t.Errorf("查询 %q 切出长度 %d 的中文 token %q：中文按字切的前提已变，"+
					"分流判据与 MATCH 召回口径都必须重新实测", q, n, tok)
			}
			if ciSearchTokenUseFulltext(tok) {
				t.Errorf("中文 token %q 竟被放行到 MATCH（单字走 MATCH 会漏召回）", tok)
			}
		}
	}
}

// containsCJK 判断 token 里是否含中日韩统一表意文字（测试侧自实现，不依赖被测包的私有判据）。
func containsCJK(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

// TestCISearchFulltextIndexCoversRecallColumns 静态对账「索引列 ↔ MATCH 列 ↔ LIKE 召回列」三方一致。
//
// 为什么这条必须用读迁移文件的方式守，而不是连库或用例覆盖：
//   - 破了①（MATCH 列清单与索引不一致）⇒ MySQL 报 1191，检索当场失败，好发现；
//   - 破了②（索引/ MATCH 少覆盖一个 LIKE 召回列）⇒ **没有任何信号**。分流是独占的，
//     只命中未覆盖列的行在召回阶段就整行丢失，永远到不了 matchCI 的判定层。
//     020 就是这个形态：ciSearchColumns 有 7 列，索引只覆盖 3 列，
//     于是按 agent_id / device_id / source / id 检索会静默返回空。
//
// 这两种破法都不是形状断言（只数片段个数）能发现的，也不是真库用例能发现的——
// 那份语料压根没往这 4 列写值，「不得漏召回」的性质断言因此在空转。
// 结构对账不依赖语料，比样例断言强，且不需要数据库。
func TestCISearchFulltextIndexCoversRecallColumns(t *testing.T) {
	idxName, idxCols := latestFulltextIndexColumns(t)
	matchCols := matchColumnsOf(t, ciSearchFulltextCond)

	// ① MATCH 的列清单必须与索引定义逐列同序一致，否则 1191。
	if !slices.Equal(idxCols, matchCols) {
		t.Errorf("MATCH 列清单 %v 与索引 %s 的定义 %v 不一致（MySQL 会报 1191，检索直接失败）:\n%s",
			matchCols, idxName, idxCols, ciSearchFulltextCond)
	}

	// ② 召回列集合必须被完整覆盖：分流独占，少一列就是漏召回。
	for _, col := range ciSearchColumns {
		indexed := recallColumnToIndexed(col)
		if !slices.Contains(matchCols, indexed) {
			t.Errorf("LIKE 召回列 %q（对应索引列 %q）未被 MATCH 覆盖：只命中该列的行会在召回阶段整行丢失，"+
				"matchCI 看不到它、无从补救", col, indexed)
		}
	}

	// ③ 探测 SQL 里写死的索引名必须与迁移一致，否则探测恒判未就绪、静默退回 LIKE。
	if !strings.Contains(ciSearchFulltextProbeSQL, "'"+idxName+"'") {
		t.Errorf("探测 SQL 引用的索引名与迁移中定义的 %q 不一致:\n%s", idxName, ciSearchFulltextProbeSQL)
	}
}

var (
	// ftIndexAddRe 匹配迁移里的 `ADD FULLTEXT INDEX <name> (<cols>)` 定义（可跨行）。
	ftIndexAddRe = regexp.MustCompile(`(?is)ADD\s+FULLTEXT\s+INDEX\s+(\w+)\s*\(([^)]*)\)`)
	// migrationVersionRe 取迁移文件名的数字前缀，用于挑「版本号最大」的定义。
	migrationVersionRe = regexp.MustCompile(`^(\d+)`)
	matchColsRe        = regexp.MustCompile(`(?i)MATCH\s*\(([^)]*)\)`)
)

// latestFulltextIndexColumns 返回 internal/store/migrations 里**最后一次**为 ci_items
// 定义全文索引的迁移的索引名与列清单（按定义顺序）。
//
// 只看非 .down.sql、且必须同文件提到 ci_items；取版本前缀最大的那份，因为索引可能被
// 后续迁移重建（020 建 3 列、021 补齐到 7 列），代码必须对齐**终态**而不是首个定义。
func latestFulltextIndexColumns(t *testing.T) (string, []string) {
	t.Helper()
	dir := filepath.Join("..", "store", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读迁移目录失败（用例假定的相对路径是否变了？）: %v", err)
	}
	bestVer, bestName, bestCols, bestFile := -1, "", "", ""
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") || strings.HasSuffix(name, ".down.sql") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("读 %s: %v", name, err)
		}
		content := stripSQLLineComments(string(data))
		if !strings.Contains(content, "ci_items") {
			continue
		}
		m := ftIndexAddRe.FindStringSubmatch(content)
		if m == nil {
			continue
		}
		vm := migrationVersionRe.FindStringSubmatch(name)
		if vm == nil {
			continue
		}
		ver, err := strconv.Atoi(vm[1])
		if err != nil {
			continue
		}
		if ver > bestVer {
			bestVer, bestName, bestCols, bestFile = ver, m[1], m[2], name
		}
	}
	if bestFile == "" {
		t.Fatal("在 internal/store/migrations 里找不到定义 ci_items 全文索引的迁移")
	}
	return bestName, splitColumnList(bestCols)
}

// matchColumnsOf 从召回片段常量里取出 MATCH(...) 的列清单。
func matchColumnsOf(t *testing.T, cond string) []string {
	t.Helper()
	m := matchColsRe.FindStringSubmatch(cond)
	if m == nil {
		t.Fatalf("无法从召回片段里取出 MATCH 列清单: %q", cond)
	}
	return splitColumnList(m[1])
}

func splitColumnList(s string) []string {
	var cols []string
	for _, c := range strings.Split(s, ",") {
		if c = strings.TrimSpace(c); c != "" {
			cols = append(cols, c)
		}
	}
	return cols
}

// stripSQLLineComments 去掉 `--` 整行注释，只留可执行语句。
//
// 必须剥：021 的头注释里就写了一句 `ADD FULLTEXT INDEX x (…)`（用来解释解析器把关键字
// 当成列名那个坑），不剥注释的话正则先撞上这句散文，对账会拿「x」当索引名——
// **门禁被自己的说明文字骗过**，比没有门禁更糟，因为它会给出看似精确的错误结论。
// 这与部署资产门禁第 13 节「先剥 `--` 注释再查 CREATE TABLE」是同一条教训的第二次应用。
func stripSQLLineComments(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// recallColumnToIndexed 把 LIKE 侧的列表达式映射到它在全文索引里的列名。
// attrs 是 JSON、不能直接建 FULLTEXT，020 用 STORED 生成列 ci_attrs_text 承载同一份文本。
func recallColumnToIndexed(col string) string {
	if col == "CAST(attrs AS CHAR)" {
		return "ci_attrs_text"
	}
	return col
}

// TestSearchCIsPreservesFilterCondsWhenFulltext 分流不得破坏租户/状态/类型过滤。
//
// 这是安全相关的断言：过滤条件与召回条件的拼接顺序若被改动，最坏情况是
// 跨租户数据泄漏——过滤条件失效，且检索照常返回结果，测试期不易察觉。
func TestSearchCIsPreservesFilterCondsWhenFulltext(t *testing.T) {
	cases := []struct {
		name       string
		indexReady bool
	}{
		{"索引就绪", true},
		{"索引未就绪", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newStoreWithProbe(t, tc.indexReady)
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
		name       string
		indexReady bool
		query      string
	}{
		{"索引就绪-混合 token", true, "web 生产"},
		{"索引未就绪-混合 token", false, "web 生产"},
		{"索引就绪-纯中文", true, "生产机房"},
		{"索引就绪-下划线", true, "server_prod"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newStoreWithProbe(t, tc.indexReady)
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
