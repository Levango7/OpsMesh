package logstore

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// ===========================================================================
// 辅助函数
// ===========================================================================

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalInt64(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ===========================================================================
// 分词器测试
// ===========================================================================

func TestTokenize_English(t *testing.T) {
	tokens := Tokenize("Hello World")
	want := []string{"hello", "world"}
	if !equalStrings(tokens, want) {
		t.Fatalf("want %v got %v", want, tokens)
	}
}

func TestTokenize_Chinese(t *testing.T) {
	tokens := Tokenize("你好世界")
	want := []string{"你", "好", "世", "界"}
	if !equalStrings(tokens, want) {
		t.Fatalf("want %v got %v", want, tokens)
	}
}

func TestTokenize_Mixed(t *testing.T) {
	tokens := Tokenize("Error 错误 happened 发生")
	want := []string{"error", "错", "误", "happened", "发", "生"}
	if !equalStrings(tokens, want) {
		t.Fatalf("want %v got %v", want, tokens)
	}
}

func TestTokenize_Punctuation(t *testing.T) {
	tokens := Tokenize("hello, world! foo-bar")
	// 标点与连字符作为分隔符。
	want := []string{"hello", "world", "foo", "bar"}
	if !equalStrings(tokens, want) {
		t.Fatalf("want %v got %v", want, tokens)
	}
}

func TestTokenize_Empty(t *testing.T) {
	if tokens := Tokenize(""); tokens != nil {
		t.Fatalf("空串应返回 nil，got %v", tokens)
	}
}

func TestTokenize_Lowercase(t *testing.T) {
	tokens := Tokenize("ERROR Error eRrOr")
	want := []string{"error", "error", "error"}
	if !equalStrings(tokens, want) {
		t.Fatalf("应全部转小写：want %v got %v", want, tokens)
	}
}

func TestTokenize_Underscore(t *testing.T) {
	tokens := Tokenize("foo_bar baz_qux")
	// 下划线作为词内字符。
	want := []string{"foo_bar", "baz_qux"}
	if !equalStrings(tokens, want) {
		t.Fatalf("want %v got %v", want, tokens)
	}
}

func TestTokenize_Digits(t *testing.T) {
	tokens := Tokenize("error 404 timeout 500")
	want := []string{"error", "404", "timeout", "500"}
	if !equalStrings(tokens, want) {
		t.Fatalf("want %v got %v", want, tokens)
	}
}

// ===========================================================================
// Add / Search 测试
// ===========================================================================

func TestInvertedIndex_AddAndSearch(t *testing.T) {
	idx := NewInvertedIndex()
	idx.Add(1, "hello world")
	idx.Add(2, "hello foo")
	idx.Add(3, "world bar")

	hits := idx.Search("hello")
	if !equalInt64(hits, []int64{1, 2}) {
		t.Fatalf("Search hello: want [1 2] got %v", hits)
	}

	hits = idx.Search("world")
	if !equalInt64(hits, []int64{1, 3}) {
		t.Fatalf("Search world: want [1 3] got %v", hits)
	}

	hits = idx.Search("nonexistent")
	if len(hits) != 0 {
		t.Fatalf("不存在的 term 应返回空，got %v", hits)
	}
}

func TestInvertedIndex_SearchCaseInsensitive(t *testing.T) {
	idx := NewInvertedIndex()
	idx.Add(1, "Hello WORLD")
	// 搜索时转小写，应命中。
	hits := idx.Search("HELLO")
	if !equalInt64(hits, []int64{1}) {
		t.Fatalf("大小写不敏感：want [1] got %v", hits)
	}
	hits = idx.Search("world")
	if !equalInt64(hits, []int64{1}) {
		t.Fatalf("大小写不敏感：want [1] got %v", hits)
	}
}

func TestInvertedIndex_Size(t *testing.T) {
	idx := NewInvertedIndex()
	if idx.Size() != 0 {
		t.Fatalf("空索引 Size 应为 0，got %d", idx.Size())
	}
	idx.Add(1, "a")
	idx.Add(2, "b")
	if idx.Size() != 2 {
		t.Fatalf("Size want 2 got %d", idx.Size())
	}
}

func TestInvertedIndex_Terms(t *testing.T) {
	idx := NewInvertedIndex()
	idx.Add(1, "hello world")
	idx.Add(2, "foo bar")
	terms := idx.Terms()
	want := []string{"bar", "foo", "hello", "world"}
	if !equalStrings(terms, want) {
		t.Fatalf("Terms: want %v got %v", want, terms)
	}
}

// ===========================================================================
// 短语查询测试
// ===========================================================================

func TestInvertedIndex_Phrase(t *testing.T) {
	idx := NewInvertedIndex()
	idx.Add(1, "hello world foo")
	idx.Add(2, "hello foo world") // hello world 不连续
	idx.Add(3, "world hello bar")

	hits := idx.SearchPhrase("hello world")
	// 仅 doc 1 的 hello(0) world(1) 连续。
	if !equalInt64(hits, []int64{1}) {
		t.Fatalf("phrase 'hello world': want [1] got %v", hits)
	}

	hits = idx.SearchPhrase("world hello")
	// doc 3 的 world(0) hello(1) 连续。
	if !equalInt64(hits, []int64{3}) {
		t.Fatalf("phrase 'world hello': want [3] got %v", hits)
	}
}

func TestInvertedIndex_PhraseChinese(t *testing.T) {
	idx := NewInvertedIndex()
	idx.Add(1, "系统启动成功")
	idx.Add(2, "启动系统失败")

	hits := idx.SearchPhrase("系统启动")
	// doc 1: 系(0)统(1)启(2)动(3) — 系统启动 连续
	// doc 2: 启(0)动(1)系(2)统(3) — 系统启动 不连续
	if !equalInt64(hits, []int64{1}) {
		t.Fatalf("phrase '系统启动': want [1] got %v", hits)
	}
}

func TestInvertedIndex_PhraseSingleToken(t *testing.T) {
	idx := NewInvertedIndex()
	idx.Add(1, "hello world")
	// 单 token 短语退化为 Search。
	hits := idx.SearchPhrase("hello")
	if !equalInt64(hits, []int64{1}) {
		t.Fatalf("single token phrase: want [1] got %v", hits)
	}
}

func TestInvertedIndex_PhraseNotFound(t *testing.T) {
	idx := NewInvertedIndex()
	idx.Add(1, "hello world")
	hits := idx.SearchPhrase("foo bar")
	if len(hits) != 0 {
		t.Fatalf("不存在的短语应返回空，got %v", hits)
	}
}

func TestInvertedIndex_PhraseRepeated(t *testing.T) {
	idx := NewInvertedIndex()
	// doc 1: "a b a b" — 短语 "a b" 在位置 0-1 和 2-3 都连续。
	idx.Add(1, "a b a b")
	hits := idx.SearchPhrase("a b")
	if !equalInt64(hits, []int64{1}) {
		t.Fatalf("phrase 'a b': want [1] got %v", hits)
	}
}

// ===========================================================================
// 布尔查询测试
// ===========================================================================

func TestInvertedIndex_And(t *testing.T) {
	idx := NewInvertedIndex()
	idx.Add(1, "hello world")
	idx.Add(2, "hello foo")
	idx.Add(3, "world bar")

	hits := idx.SearchAnd([]string{"hello", "world"})
	if !equalInt64(hits, []int64{1}) {
		t.Fatalf("AND hello world: want [1] got %v", hits)
	}

	hits = idx.SearchAnd([]string{"hello", "bar"})
	if len(hits) != 0 {
		t.Fatalf("AND hello bar: want [] got %v", hits)
	}
}

func TestInvertedIndex_AndThreeTerms(t *testing.T) {
	idx := NewInvertedIndex()
	idx.Add(1, "alpha beta gamma")
	idx.Add(2, "alpha beta")
	idx.Add(3, "alpha gamma")
	hits := idx.SearchAnd([]string{"alpha", "beta", "gamma"})
	if !equalInt64(hits, []int64{1}) {
		t.Fatalf("AND three terms: want [1] got %v", hits)
	}
}

func TestInvertedIndex_Or(t *testing.T) {
	idx := NewInvertedIndex()
	idx.Add(1, "hello world")
	idx.Add(2, "foo bar")
	idx.Add(3, "world baz")

	hits := idx.SearchOr([]string{"hello", "foo"})
	if !equalInt64(hits, []int64{1, 2}) {
		t.Fatalf("OR hello foo: want [1 2] got %v", hits)
	}
}

func TestInvertedIndex_OrOverlap(t *testing.T) {
	idx := NewInvertedIndex()
	idx.Add(1, "hello world")
	idx.Add(2, "hello foo")
	// OR hello world：doc 1 含两者（得分叠加），doc 2 仅含 hello。
	hits := idx.SearchOr([]string{"hello", "world"})
	if len(hits) != 2 {
		t.Fatalf("OR overlap: want 2 hits got %d", len(hits))
	}
	// doc 1 含 hello+world，得分应高于 doc 2（仅 hello）。
	if hits[0] != 1 {
		t.Fatalf("OR overlap: doc 1 应排第一，got %v", hits)
	}
}

func TestInvertedIndex_Not(t *testing.T) {
	idx := NewInvertedIndex()
	idx.Add(1, "hello world")
	idx.Add(2, "foo bar")
	idx.Add(3, "world baz")

	hits := idx.SearchNot("hello")
	// doc 2 和 3 不含 hello，按 docID 升序。
	if !equalInt64(hits, []int64{2, 3}) {
		t.Fatalf("NOT hello: want [2 3] got %v", hits)
	}
}

func TestInvertedIndex_NotAllDocs(t *testing.T) {
	idx := NewInvertedIndex()
	idx.Add(1, "hello")
	idx.Add(2, "world")
	// NOT 不存在的 term：返回所有 doc。
	hits := idx.SearchNot("nonexistent")
	if !equalInt64(hits, []int64{1, 2}) {
		t.Fatalf("NOT nonexistent: want [1 2] got %v", hits)
	}
}

// ===========================================================================
// 通配符查询测试
// ===========================================================================

func TestInvertedIndex_WildcardStar(t *testing.T) {
	idx := NewInvertedIndex()
	idx.Add(1, "hello world")
	idx.Add(2, "help yourself")
	idx.Add(3, "world peace")

	hits := idx.SearchWildcard("hel*")
	// hel* 匹配 hello, help
	if !equalInt64(hits, []int64{1, 2}) {
		t.Fatalf("wildcard hel*: want [1 2] got %v", hits)
	}
}

func TestInvertedIndex_WildcardQuestion(t *testing.T) {
	idx := NewInvertedIndex()
	idx.Add(1, "hello world")
	idx.Add(2, "hallo world")
	hits := idx.SearchWildcard("h?llo")
	// h?llo 匹配 hello 和 hallo
	if !equalInt64(hits, []int64{1, 2}) {
		t.Fatalf("wildcard h?llo: want [1 2] got %v", hits)
	}
}

func TestInvertedIndex_WildcardNoSpecial(t *testing.T) {
	idx := NewInvertedIndex()
	idx.Add(1, "hello world")
	// 无通配符退化为普通 Search。
	hits := idx.SearchWildcard("hello")
	if !equalInt64(hits, []int64{1}) {
		t.Fatalf("wildcard no special: want [1] got %v", hits)
	}
}

func TestInvertedIndex_WildcardCombined(t *testing.T) {
	idx := NewInvertedIndex()
	idx.Add(1, "error123")
	idx.Add(2, "error456")
	idx.Add(3, "warn123")
	hits := idx.SearchWildcard("error*")
	// error* 匹配 error123, error456
	if !equalInt64(hits, []int64{1, 2}) {
		t.Fatalf("wildcard error*: want [1 2] got %v", hits)
	}
}

func TestInvertedIndex_WildcardEmpty(t *testing.T) {
	idx := NewInvertedIndex()
	idx.Add(1, "hello")
	hits := idx.SearchWildcard("")
	if len(hits) != 0 {
		t.Fatalf("空 pattern 应返回空，got %v", hits)
	}
}

// ===========================================================================
// TF-IDF 排序测试
// ===========================================================================

func TestInvertedIndex_TFIDF_RareTerm(t *testing.T) {
	idx := NewInvertedIndex()
	// 4 个文档，"rare" 仅在 doc 1（df=1），"common" 在 doc 1,2,3（df=3）。
	idx.Add(1, "rare common")
	idx.Add(2, "common other")
	idx.Add(3, "common extra")
	idx.Add(4, "unrelated stuff")

	// Search "rare"：仅 doc 1，IDF 高。
	hits := idx.Search("rare")
	if !equalInt64(hits, []int64{1}) {
		t.Fatalf("rare: want [1] got %v", hits)
	}
}

func TestInvertedIndex_TFIDF_TFBoost(t *testing.T) {
	idx := NewInvertedIndex()
	idx.Add(1, "rare common")
	idx.Add(2, "common other")
	idx.Add(3, "common extra")
	idx.Add(4, "unrelated stuff")

	// doc 2 更新为 common 出现两次，TF 更高。
	idx.Add(2, "common common double")
	hits := idx.Search("common")
	// N=4, df(common)=3, IDF=log10(4/3)>0
	// doc 2: TF=1+log10(2)≈1.3，得分高于 doc 1,3（TF=1）。
	if hits[0] != 2 {
		t.Fatalf("common: doc 2 (tf=2) 应排第一，got %v", hits)
	}
	if len(hits) != 3 {
		t.Fatalf("common: want 3 hits got %d", len(hits))
	}
}

func TestInvertedIndex_TFIDF_TieBreakByDocID(t *testing.T) {
	idx := NewInvertedIndex()
	idx.Add(3, "term a")
	idx.Add(1, "term b")
	idx.Add(2, "term c")
	// 所有 doc 的 term tf=1, df=3, N=3, IDF=0，得分相同，按 docID 升序。
	hits := idx.Search("term")
	if !equalInt64(hits, []int64{1, 2, 3}) {
		t.Fatalf("tie break: want [1 2 3] got %v", hits)
	}
}

// ===========================================================================
// 删除测试
// ===========================================================================

func TestInvertedIndex_Remove(t *testing.T) {
	idx := NewInvertedIndex()
	idx.Add(1, "hello world")
	idx.Add(2, "hello foo")

	if idx.Size() != 2 {
		t.Fatalf("Size: want 2 got %d", idx.Size())
	}

	idx.Remove(1)
	if idx.Size() != 1 {
		t.Fatalf("Size after remove: want 1 got %d", idx.Size())
	}

	hits := idx.Search("hello")
	if !equalInt64(hits, []int64{2}) {
		t.Fatalf("after remove: Search hello want [2] got %v", hits)
	}

	hits = idx.Search("world")
	if len(hits) != 0 {
		t.Fatalf("after remove: Search world should be empty, got %v", hits)
	}
}

func TestInvertedIndex_RemoveNonexistent(t *testing.T) {
	idx := NewInvertedIndex()
	idx.Add(1, "hello")
	idx.Remove(999) // 不存在的 docID，不应 panic
	if idx.Size() != 1 {
		t.Fatalf("Size: want 1 got %d", idx.Size())
	}
}

func TestInvertedIndex_RemoveAllPostings(t *testing.T) {
	idx := NewInvertedIndex()
	idx.Add(1, "unique_term")
	idx.Remove(1)
	// 删除后 term 应从索引中清除。
	terms := idx.Terms()
	for _, tm := range terms {
		if tm == "unique_term" {
			t.Fatalf("删除后 term 应被清除，但仍存在 unique_term")
		}
	}
}

func TestInvertedIndex_UpdateExisting(t *testing.T) {
	idx := NewInvertedIndex()
	idx.Add(1, "hello world")
	idx.Add(1, "foo bar") // 更新 doc 1
	if idx.Size() != 1 {
		t.Fatalf("Size after update: want 1 got %d", idx.Size())
	}
	// 旧 term 应被清除。
	if hits := idx.Search("hello"); len(hits) != 0 {
		t.Fatalf("更新后旧 term 应被清除，got %v", hits)
	}
	// 新 term 应可搜到。
	if hits := idx.Search("foo"); !equalInt64(hits, []int64{1}) {
		t.Fatalf("更新后新 term 应可搜到：want [1] got %v", hits)
	}
}

// ===========================================================================
// 并发安全测试
// ===========================================================================

func TestInvertedIndex_Concurrent(t *testing.T) {
	idx := NewInvertedIndex()
	const n = 100
	var wg sync.WaitGroup
	// 并发 Add
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			idx.Add(int64(id), fmt.Sprintf("doc %d content %d", id, id%5))
		}(i)
	}
	// 并发 Search
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = idx.Search("doc")
		}()
	}
	// 并发 Remove（移除前一半，后一半保留）
	for i := 0; i < n/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			idx.Remove(int64(id))
		}(i)
	}
	wg.Wait()
	// 最终应剩 n/2 个文档。
	if got := idx.Size(); got != n/2 {
		t.Fatalf("Size after concurrent ops: want %d got %d", n/2, got)
	}
}

func TestInvertedIndex_ConcurrentReadWrite(t *testing.T) {
	idx := NewInvertedIndex()
	const n = 200
	var wg sync.WaitGroup
	// 先填充一半。
	for i := 0; i < n/2; i++ {
		idx.Add(int64(i), fmt.Sprintf("term%d shared", i%10))
	}
	// 并发：继续 Add + 大量 Search。
	for i := n / 2; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			idx.Add(int64(id), fmt.Sprintf("term%d shared", id%10))
		}(i)
	}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = idx.Search("shared")
			_ = idx.SearchPhrase("term0 shared")
			_ = idx.SearchAnd([]string{"shared", "term0"})
		}()
	}
	wg.Wait()
	if got := idx.Size(); got != n {
		t.Fatalf("Size: want %d got %d", n, got)
	}
}

// ===========================================================================
// 集成测试：MemoryLogStore + SearchFullText
// ===========================================================================

func TestMemoryWithIndex_SearchFullText(t *testing.T) {
	ctx := context.Background()
	ls := NewMemoryWithIndex(0)
	_ = ls.Append(ctx, &Entry{TenantID: "t1", Level: "error", Source: "task", Message: "panic nil pointer dereference"})
	_ = ls.Append(ctx, &Entry{TenantID: "t1", Level: "error", Source: "task", Message: "panic oom killed"})
	_ = ls.Append(ctx, &Entry{TenantID: "t1", Level: "info", Source: "agent", Message: "heartbeat ok"})

	// 短语查询
	out, err := ls.SearchFullText(ctx, FullTextQuery{
		Base:   Query{TenantID: "t1"},
		Phrase: "panic nil",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || !strings.Contains(out[0].Message, "nil pointer") {
		t.Fatalf("phrase 'panic nil': want 1 hit with nil pointer, got %#v", out)
	}

	// AND 查询
	out, err = ls.SearchFullText(ctx, FullTextQuery{
		Base: Query{TenantID: "t1"},
		And:  []string{"panic", "oom"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || !strings.Contains(out[0].Message, "oom") {
		t.Fatalf("AND panic oom: want 1 hit, got %#v", out)
	}

	// OR 查询
	out, err = ls.SearchFullText(ctx, FullTextQuery{
		Base: Query{TenantID: "t1"},
		Or:   []string{"heartbeat", "panic"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("OR heartbeat panic: want 3 hits got %d", len(out))
	}

	// NOT 查询
	out, err = ls.SearchFullText(ctx, FullTextQuery{
		Base: Query{TenantID: "t1"},
		Not:  "panic",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || !strings.Contains(out[0].Message, "heartbeat") {
		t.Fatalf("NOT panic: want 1 heartbeat hit, got %#v", out)
	}

	// 通配符
	out, err = ls.SearchFullText(ctx, FullTextQuery{
		Base:     Query{TenantID: "t1"},
		Wildcard: "panic*",
	})
	if err != nil {
		t.Fatal(err)
	}
	// panic* 匹配 panic term，2 条 error 日志含 panic。
	if len(out) != 2 {
		t.Fatalf("wildcard panic*: want 2 hits got %d", len(out))
	}

	// 单 term
	out, err = ls.SearchFullText(ctx, FullTextQuery{
		Base: Query{TenantID: "t1"},
		Term: "heartbeat",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("term heartbeat: want 1 hit got %d", len(out))
	}
}

func TestMemoryWithIndex_Disabled(t *testing.T) {
	ctx := context.Background()
	ls := NewMemory(0) // 未启用索引
	_, err := ls.SearchFullText(ctx, FullTextQuery{Term: "hello"})
	if err != ErrIndexDisabled {
		t.Fatalf("want ErrIndexDisabled, got %v", err)
	}
}

func TestMemoryWithIndex_RingTruncate(t *testing.T) {
	ctx := context.Background()
	ls := NewMemoryWithIndex(2)
	for i := 0; i < 5; i++ {
		if err := ls.Append(ctx, &Entry{TenantID: "t1", Message: fmt.Sprintf("msg %d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	// 环形裁剪后仅保留最新 2 条（ID 4、5）。
	out, err := ls.SearchFullText(ctx, FullTextQuery{
		Base: Query{TenantID: "t1"},
		Term: "msg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("ring truncate: want 2 hits got %d", len(out))
	}
	// 应为 ID 4 和 5。
	ids := map[int64]bool{out[0].ID: true, out[1].ID: true}
	if !ids[4] || !ids[5] {
		t.Fatalf("ring truncate: want ID 4,5 got %d,%d", out[0].ID, out[1].ID)
	}
}

func TestMemoryWithIndex_TenantIsolation(t *testing.T) {
	ctx := context.Background()
	ls := NewMemoryWithIndex(0)
	_ = ls.Append(ctx, &Entry{TenantID: "t1", Message: "secret tenant one"})
	_ = ls.Append(ctx, &Entry{TenantID: "t2", Message: "secret tenant two"})

	// t1 只能搜到自己的日志。
	out, err := ls.SearchFullText(ctx, FullTextQuery{
		Base: Query{TenantID: "t1"},
		Term: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].TenantID != "t1" {
		t.Fatalf("租户隔离失败：got %#v", out)
	}
}

func TestMemoryWithIndex_LevelFilter(t *testing.T) {
	ctx := context.Background()
	ls := NewMemoryWithIndex(0)
	_ = ls.Append(ctx, &Entry{TenantID: "t1", Level: "error", Message: "panic error one"})
	_ = ls.Append(ctx, &Entry{TenantID: "t1", Level: "info", Message: "panic info two"})

	out, err := ls.SearchFullText(ctx, FullTextQuery{
		Base: Query{TenantID: "t1", Level: "error"},
		Term: "panic",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Level != "error" {
		t.Fatalf("level 过滤失败：got %#v", out)
	}
}

func TestMemoryWithIndex_TimeFilter(t *testing.T) {
	ctx := context.Background()
	ls := NewMemoryWithIndex(0)
	base := time.Now()
	e1 := &Entry{TenantID: "t1", Message: "event old", Timestamp: base.Add(-2 * time.Hour)}
	e2 := &Entry{TenantID: "t1", Message: "event new", Timestamp: base}
	_ = ls.Append(ctx, e1)
	_ = ls.Append(ctx, e2)

	from := base.Add(-1 * time.Hour)
	out, err := ls.SearchFullText(ctx, FullTextQuery{
		Base: Query{TenantID: "t1", From: from},
		Term: "event",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || !strings.Contains(out[0].Message, "new") {
		t.Fatalf("时间过滤失败：got %#v", out)
	}
}

func TestMemoryWithIndex_Limit(t *testing.T) {
	ctx := context.Background()
	ls := NewMemoryWithIndex(0)
	for i := 0; i < 10; i++ {
		_ = ls.Append(ctx, &Entry{TenantID: "t1", Message: "common term"})
	}
	out, err := ls.SearchFullText(ctx, FullTextQuery{
		Base:  Query{TenantID: "t1"},
		Term:  "common",
		Limit: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("limit: want 3 got %d", len(out))
	}
}

func TestMemoryWithIndex_EmptyResult(t *testing.T) {
	ctx := context.Background()
	ls := NewMemoryWithIndex(0)
	_ = ls.Append(ctx, &Entry{TenantID: "t1", Message: "hello world"})
	out, err := ls.SearchFullText(ctx, FullTextQuery{
		Base: Query{TenantID: "t1"},
		Term: "nonexistent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("空结果应返回空切片，got %d", len(out))
	}
}

func TestMemoryWithIndex_EmptyQuery(t *testing.T) {
	ctx := context.Background()
	ls := NewMemoryWithIndex(0)
	_ = ls.Append(ctx, &Entry{TenantID: "t1", Message: "hello"})
	_, err := ls.SearchFullText(ctx, FullTextQuery{Base: Query{TenantID: "t1"}})
	if err != ErrEmptyQuery {
		t.Fatalf("空查询应返回 ErrEmptyQuery，got %v", err)
	}
}

func TestMemoryWithIndex_BackwardCompatible(t *testing.T) {
	// 未启用索引的 MemoryLogStore 行为应与原来完全一致。
	ctx := context.Background()
	ls := NewMemory(0)
	_ = ls.Append(ctx, &Entry{TenantID: "t1", Level: "error", Message: "panic nil"})
	_ = ls.Append(ctx, &Entry{TenantID: "t1", Level: "info", Message: "all good"})
	// Query 应正常工作。
	out, err := ls.Query(ctx, Query{TenantID: "t1", Level: "error"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || !strings.Contains(out[0].Message, "panic") {
		t.Fatalf("向后兼容 Query 失败：got %#v", out)
	}
}

func TestMemoryWithIndex_ChineseSearch(t *testing.T) {
	ctx := context.Background()
	ls := NewMemoryWithIndex(0)
	_ = ls.Append(ctx, &Entry{TenantID: "t1", Message: "系统启动成功"})
	_ = ls.Append(ctx, &Entry{TenantID: "t1", Message: "启动失败"})

	// 短语查询：系统启动
	out, err := ls.SearchFullText(ctx, FullTextQuery{
		Base:   Query{TenantID: "t1"},
		Phrase: "系统启动",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || !strings.Contains(out[0].Message, "成功") {
		t.Fatalf("中文短语查询失败：got %#v", out)
	}

	// 单字搜索
	out, err = ls.SearchFullText(ctx, FullTextQuery{
		Base: Query{TenantID: "t1"},
		Term: "启",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("中文单字搜索：want 2 got %d", len(out))
	}
}