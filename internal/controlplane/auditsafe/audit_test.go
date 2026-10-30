package auditsafe

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// 审计 Detail 会拼进日志/事件记录：换行必须被替换，否则一条用户输入能伪造多行记录。
func TestDetailRemovesLineBreaks(t *testing.T) {
	cases := map[string]string{
		"单一换行":      "a\nb",
		"CRLF":      "a\r\nb",
		"裸 CR 直接删除": "a\rb",
		"多条换行":      "x\n\ny",
	}
	want := map[string]string{
		"单一换行":      "a b",
		"CRLF":      "a b",
		"裸 CR 直接删除": "ab",
		"多条换行":      "x  y",
	}
	for name, in := range cases {
		if got := Detail(in); got != want[name] {
			t.Fatalf("%s: Detail(%q) = %q, want %q", name, in, got, want[name])
		}
	}
}

func TestDetailTruncation(t *testing.T) {
	// 恰好 200 字节不截断。
	if got := Detail(strings.Repeat("x", 200)); len(got) != 200 || strings.HasSuffix(got, "...") {
		t.Fatalf("Detail(200 字节) = len=%d, want 原样", len(got))
	}
	// 超长按 200 字节上限 + 省略号。
	got := Detail(strings.Repeat("x", 250))
	if len(got) != 203 || !strings.HasSuffix(got, "...") {
		t.Fatalf("Detail(250 字节) = len=%d, want 203 且以 ... 结尾", len(got))
	}
}

// 审计 Detail 里常有中文命令/原因：按字节硬切会切断多字节 UTF-8，
// 落库/序列化时得到非法字符。截断必须在字符边界回退。
func TestDetailTruncationKeepsValidUTF8(t *testing.T) {
	in := strings.Repeat("测", 100) // 300 字节，200 不是字符边界
	got := Detail(in)
	if !utf8.ValidString(got) {
		t.Fatalf("Detail(中文) 产生非法 UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("Detail(中文) 未截断: %q", got)
	}
	if len(got) > 203 || len(got) < 190 {
		t.Fatalf("Detail(中文) 截断后 %d 字节，超出 200 字节预算", len(got))
	}
}
