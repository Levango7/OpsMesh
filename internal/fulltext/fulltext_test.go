package fulltext

import (
	"testing"
)

func TestTokenizeMixed(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"Hello World", []string{"hello", "world"}},
		{"你好世界", []string{"你", "好", "世", "界"}},
		{"Error 错误 happened", []string{"error", "错", "误", "happened"}},
		{"hello, world! foo-bar", []string{"hello", "world", "foo", "bar"}},
		{"", nil},
		{"foo_bar baz_qux", []string{"foo_bar", "baz_qux"}}, // 下划线是词内字符
		{"ERROR Error eRrOr", []string{"error", "error", "error"}},
		{"web-01.nginx", []string{"web", "01", "nginx"}},
	}
	for _, c := range cases {
		got := Tokenize(c.in)
		if len(got) != len(c.want) {
			t.Errorf("Tokenize(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("Tokenize(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestIndexAddSearchRemove(t *testing.T) {
	idx := NewIndex[int64]()
	idx.Add(1, "hello world")
	idx.Add(2, "hello foo")
	idx.Add(3, "world bar")
	if got := idx.Search("hello"); len(got) != 2 {
		t.Fatalf("Search(hello) = %v, want 2 hits", got)
	}
	if got := idx.Search("nonexistent"); len(got) != 0 {
		t.Fatalf("Search(nonexistent) = %v, want 0", got)
	}
	if idx.Size() != 3 {
		t.Fatalf("Size = %d, want 3", idx.Size())
	}
	idx.Remove(1)
	if idx.Size() != 2 {
		t.Fatalf("Size after Remove = %d, want 2", idx.Size())
	}
	if got := idx.Search("world"); len(got) != 1 || got[0] != 3 {
		t.Fatalf("Search(world) after Remove = %v, want [3]", got)
	}
	// 重复 Add 同一 docID 不应造成 tf/postings 重复累加。
	idx.Add(2, "hello hello hello")
	idx.Add(2, "hello")
	if idx.Size() != 2 {
		t.Fatalf("Size after re-Add = %d, want 2", idx.Size())
	}
}

// TestIndexSearchPrefix 覆盖前缀检索：这是 CMDB 检索依赖的关键能力
// （"web" 必须命中 "webserver" 这类未切分的整词）。
func TestIndexSearchPrefix(t *testing.T) {
	idx := NewIndex[string]()
	idx.Add("a", "webserver nginx")
	idx.Add("b", "web-01 nginx")
	idx.Add("c", "database mysql")
	idx.Add("d", "weblogic java")

	got := idx.SearchPrefix("web")
	if len(got) != 3 {
		t.Fatalf("SearchPrefix(web) = %v, want 3 hits (a/b/d)", got)
	}
	// 排序需确定性：同分按 docID 升序。
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Errorf("SearchPrefix(web) 顺序不稳定: %v", got)
		}
	}
	if got := idx.SearchPrefix("mysql"); len(got) != 1 || got[0] != "c" {
		t.Fatalf("SearchPrefix(mysql) = %v, want [c]", got)
	}
	if got := idx.SearchPrefix(""); got != nil {
		t.Fatalf("SearchPrefix(\"\") = %v, want nil", got)
	}
	if got := idx.SearchPrefix("zzz"); len(got) != 0 {
		t.Fatalf("SearchPrefix(zzz) = %v, want 0", got)
	}
	// 删除文档后前缀检索不得再返回它（缓存失效路径）。
	idx.Remove("a")
	if got := idx.SearchPrefix("web"); len(got) != 2 {
		t.Fatalf("SearchPrefix(web) after Remove = %v, want 2", got)
	}
}

func TestIndexBooleanAndWildcard(t *testing.T) {
	idx := NewIndex[int64]()
	idx.Add(1, "error timeout db")
	idx.Add(2, "error timeout cache")
	idx.Add(3, "warn slow db")

	if got := idx.SearchAnd([]string{"error", "timeout"}); len(got) != 2 {
		t.Errorf("SearchAnd = %v, want 2", got)
	}
	if got := idx.SearchAnd([]string{"error", "db"}); len(got) != 1 || got[0] != 1 {
		t.Errorf("SearchAnd(error,db) = %v, want [1]", got)
	}
	if got := idx.SearchOr([]string{"warn", "cache"}); len(got) != 2 {
		t.Errorf("SearchOr = %v, want 2", got)
	}
	if got := idx.SearchNot("error"); len(got) != 1 || got[0] != 3 {
		t.Errorf("SearchNot(error) = %v, want [3]", got)
	}
	if got := idx.SearchWildcard("time*"); len(got) != 2 {
		t.Errorf("SearchWildcard(time*) = %v, want 2", got)
	}
	if got := idx.SearchPhrase("error timeout"); len(got) != 2 {
		t.Errorf("SearchPhrase = %v, want 2", got)
	}
}

// TestIndexTermsSorted 保证 Terms 字典序（前缀检索二分定位的前提）。
func TestIndexTermsSorted(t *testing.T) {
	idx := NewIndex[string]()
	idx.Add("a", "zeta alpha mid")
	terms := idx.Terms()
	for i := 1; i < len(terms); i++ {
		if terms[i-1] > terms[i] {
			t.Fatalf("Terms 未排序: %v", terms)
		}
	}
}
