package logstore

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// mkEntry 测试辅助：构造一条日志。
func mkEntry(level, device, agent, source, task, msg string) Entry {
	return Entry{
		TenantID:  "t1",
		DeviceID:  device,
		AgentID:   agent,
		TaskID:    task,
		Level:     level,
		Source:    source,
		Message:   msg,
		Timestamp: time.Now(),
	}
}

func TestParseQuery_SimpleField(t *testing.T) {
	q, err := ParseQuery("level=error")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := q.(*FieldEq); !ok {
		t.Fatalf("want *FieldEq, got %T", q)
	}
	hit := mkEntry("error", "dev-1", "a1", "task", "", "boom")
	miss := mkEntry("info", "dev-1", "a1", "task", "", "ok")
	if !q.Match(&hit) {
		t.Fatal("应匹配 level=error")
	}
	if q.Match(&miss) {
		t.Fatal("info 不应匹配 level=error")
	}
}

func TestParseQuery_And(t *testing.T) {
	q, err := ParseQuery("level=error AND device=dev-1")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := q.(*AndNode); !ok {
		t.Fatalf("want *AndNode, got %T", q)
	}
	cases := []struct {
		e    Entry
		want bool
	}{
		{mkEntry("error", "dev-1", "a", "task", "", "x"), true},
		{mkEntry("error", "dev-2", "a", "task", "", "x"), false},
		{mkEntry("info", "dev-1", "a", "task", "", "x"), false},
	}
	for i, c := range cases {
		if got := q.Match(&c.e); got != c.want {
			t.Fatalf("case %d: want %v got %v", i, c.want, got)
		}
	}
}

func TestParseQuery_Or(t *testing.T) {
	q, err := ParseQuery("level=warn OR level=error")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := q.(*OrNode); !ok {
		t.Fatalf("want *OrNode, got %T", q)
	}
	cases := []struct {
		e    Entry
		want bool
	}{
		{mkEntry("warn", "", "", "", "", ""), true},
		{mkEntry("error", "", "", "", "", ""), true},
		{mkEntry("info", "", "", "", "", ""), false},
	}
	for i, c := range cases {
		if got := q.Match(&c.e); got != c.want {
			t.Fatalf("case %d: want %v got %v", i, c.want, got)
		}
	}
}

func TestParseQuery_Parens(t *testing.T) {
	q, err := ParseQuery("source=task AND (level=warn OR level=error)")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cases := []struct {
		e    Entry
		want bool
	}{
		{mkEntry("warn", "", "", "task", "", ""), true},
		{mkEntry("error", "", "", "task", "", ""), true},
		{mkEntry("info", "", "", "task", "", ""), false},
		{mkEntry("warn", "", "", "agent", "", ""), false},
	}
	for i, c := range cases {
		if got := q.Match(&c.e); got != c.want {
			t.Fatalf("case %d: want %v got %v", i, c.want, got)
		}
	}
}

func TestParseQuery_Contains(t *testing.T) {
	q, err := ParseQuery(`message~"panic"`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := q.(*FieldContains); !ok {
		t.Fatalf("want *FieldContains, got %T", q)
	}
	hit := mkEntry("error", "", "", "", "", "runtime panic: nil deref")
	miss := mkEntry("info", "", "", "", "", "all good")
	if !q.Match(&hit) {
		t.Fatal("应匹配包含 panic")
	}
	if q.Match(&miss) {
		t.Fatal("不应匹配")
	}
}

func TestParseQuery_NotEqual(t *testing.T) {
	q, err := ParseQuery("level!=info")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := q.(*FieldNotEq); !ok {
		t.Fatalf("want *FieldNotEq, got %T", q)
	}
	cases := []struct {
		e    Entry
		want bool
	}{
		{mkEntry("info", "", "", "", "", ""), false},
		{mkEntry("warn", "", "", "", "", ""), true},
		{mkEntry("error", "", "", "", "", ""), true},
	}
	for i, c := range cases {
		if got := q.Match(&c.e); got != c.want {
			t.Fatalf("case %d: want %v got %v", i, c.want, got)
		}
	}
}

func TestParseQuery_NotContains(t *testing.T) {
	q, err := ParseQuery(`message!~"debug"`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := q.(*FieldNotContains); !ok {
		t.Fatalf("want *FieldNotContains, got %T", q)
	}
	cases := []struct {
		e    Entry
		want bool
	}{
		{mkEntry("info", "", "", "", "", "starting debug logger"), false},
		{mkEntry("info", "", "", "", "", "service ready"), true},
	}
	for i, c := range cases {
		if got := q.Match(&c.e); got != c.want {
			t.Fatalf("case %d: want %v got %v", i, c.want, got)
		}
	}
}

func TestParseQuery_Complex(t *testing.T) {
	q, err := ParseQuery(`level=error AND device=dev-1 AND message~"panic"`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cases := []struct {
		e    Entry
		want bool
	}{
		{mkEntry("error", "dev-1", "a", "task", "", "panic: nil"), true},
		{mkEntry("error", "dev-2", "a", "task", "", "panic: nil"), false},
		{mkEntry("error", "dev-1", "a", "task", "", "ok"), false},
		{mkEntry("warn", "dev-1", "a", "task", "", "panic"), false},
	}
	for i, c := range cases {
		if got := q.Match(&c.e); got != c.want {
			t.Fatalf("case %d: want %v got %v", i, c.want, got)
		}
	}
}

func TestParseQuery_NotOperator(t *testing.T) {
	// NOT 优先级高于 AND：NOT level=info AND source=task
	q, err := ParseQuery("NOT level=info AND source=task")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cases := []struct {
		e    Entry
		want bool
	}{
		{mkEntry("error", "", "", "task", "", ""), true},
		{mkEntry("info", "", "", "task", "", ""), false},
		{mkEntry("error", "", "", "agent", "", ""), false},
	}
	for i, c := range cases {
		if got := q.Match(&c.e); got != c.want {
			t.Fatalf("case %d: want %v got %v", i, c.want, got)
		}
	}
}

func TestParseQuery_InvalidSyntax(t *testing.T) {
	cases := []string{
		"",                       // 空串
		"   ",                    // 仅空白
		"level",                  // 缺操作符
		"level=",                 // 缺值
		"=error",                 // 缺字段
		"level=error AND",        // AND 后无表达式
		"level=error OR",         // OR 后无表达式
		"(level=error",           // 括号未闭合
		"level=error)",           // 多余右括号
		"foo=bar",                // 未知字段
		`message="panic"`,        // message 用 = 不允许
		`message!="panic"`,       // message 用 != 不允许
		"level=error AND AND level=warn", // 双 AND
		"level~error AND",        // 末尾悬空
	}
	for i, s := range cases {
		_, err := ParseQuery(s)
		if err == nil {
			t.Fatalf("case %d (%q): 期望返回错误，实际为 nil", i, s)
		}
	}
}

func TestParseQuery_EmptyQueryError(t *testing.T) {
	_, err := ParseQuery("")
	if !errors.Is(err, ErrEmptyQuery) {
		t.Fatalf("空串应返回 ErrEmptyQuery，got %v", err)
	}
}

func TestParseQuery_QuotedValue(t *testing.T) {
	// 带空格的引号值。
	q, err := ParseQuery(`message~"nil pointer dereference"`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	e := mkEntry("error", "", "", "", "", "fatal: nil pointer dereference at 0x0")
	if !q.Match(&e) {
		t.Fatal("应匹配带空格的引号值")
	}
}

func TestParseQuery_OperatorPrecedence(t *testing.T) {
	// AND 优先于 OR：level=warn OR level=error AND device=dev-1
	// 等价于 level=warn OR (level=error AND device=dev-1)
	q, err := ParseQuery("level=warn OR level=error AND device=dev-1")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cases := []struct {
		e    Entry
		want bool
	}{
		{mkEntry("warn", "dev-99", "", "", "", ""), true},   // warn 命中左分支
		{mkEntry("error", "dev-1", "", "", "", ""), true},   // 右分支全命中
		{mkEntry("error", "dev-2", "", "", "", ""), false},  // device 不匹配
		{mkEntry("info", "dev-1", "", "", "", ""), false},
	}
	for i, c := range cases {
		if got := q.Match(&c.e); got != c.want {
			t.Fatalf("case %d: want %v got %v", i, c.want, got)
		}
	}
}

func TestApplyQuery(t *testing.T) {
	entries := []Entry{
		mkEntry("error", "dev-1", "a1", "task", "T1", "panic: nil"),
		mkEntry("info", "dev-1", "a1", "task", "T2", "started"),
		mkEntry("error", "dev-2", "a1", "task", "T3", "panic: oom"),
		mkEntry("warn", "dev-1", "a1", "agent", "", "cpu high"),
		mkEntry("error", "dev-1", "a2", "task", "T4", "panic: stack"),
	}
	q, err := ParseQuery(`level=error AND device=dev-1 AND message~"panic"`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out := ApplyQuery(entries, q)
	if len(out) != 2 {
		t.Fatalf("want 2 hits, got %d", len(out))
	}
	for _, e := range out {
		if e.Level != "error" || e.DeviceID != "dev-1" || !strings.Contains(e.Message, "panic") {
			t.Fatalf("过滤结果不合规：%#v", e)
		}
	}
	// 顺序应保持原序。
	if out[0].TaskID != "T1" || out[1].TaskID != "T4" {
		t.Fatalf("顺序应保持原序，got %s,%s", out[0].TaskID, out[1].TaskID)
	}
}

func TestApplyQuery_NilQuery(t *testing.T) {
	entries := []Entry{mkEntry("info", "", "", "", "", "")}
	out := ApplyQuery(entries, nil)
	if len(out) != 0 {
		t.Fatalf("nil 查询应返回空切片，got %d", len(out))
	}
}

func TestFieldValues(t *testing.T) {
	e := mkEntry("error", "dev-1", "agent-7", "task", "task-42", "hello")
	cases := []struct {
		field, want string
	}{
		{"level", "error"},
		{"device", "dev-1"},
		{"agent", "agent-7"},
		{"source", "task"},
		{"task", "task-42"},
		{"message", "hello"},
		{"unknown", ""},
	}
	for _, c := range cases {
		if got := fieldValue(&e, c.field); got != c.want {
			t.Fatalf("field %q: want %q got %q", c.field, c.want, got)
		}
	}
}

func TestParseQuery_AllOperators(t *testing.T) {
	// 验证四种操作符都能解析。
	cases := []string{
		"level=error",
		"level!=info",
		`message~"panic"`,
		`message!~"debug"`,
		`source=task AND agent=a1`,
	}
	for i, s := range cases {
		if _, err := ParseQuery(s); err != nil {
			t.Fatalf("case %d (%q): %v", i, s, err)
		}
	}
}

func TestParseQuery_NestedParens(t *testing.T) {
	// 嵌套括号。
	q, err := ParseQuery("(level=warn OR (level=error AND device=dev-1))")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cases := []struct {
		e    Entry
		want bool
	}{
		{mkEntry("warn", "dev-99", "", "", "", ""), true},
		{mkEntry("error", "dev-1", "", "", "", ""), true},
		{mkEntry("error", "dev-2", "", "", "", ""), false},
		{mkEntry("info", "dev-1", "", "", "", ""), false},
	}
	for i, c := range cases {
		if got := q.Match(&c.e); got != c.want {
			t.Fatalf("case %d: want %v got %v", i, c.want, got)
		}
	}
}