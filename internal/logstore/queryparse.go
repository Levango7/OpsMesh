// Package logstore: queryparse.go 提供 KQL/Lucene 风格的日志查询语法解析器。
//
// 支持的语法示例：
//
//	level=error
//	level=error AND device=dev-1
//	source=task AND (level=warn OR level=error)
//	level=error AND message~"panic"
//	level!=info
//	message!~"debug" OR source=system
//
// 支持的字段：level / device / agent / source / message / task
// 支持的操作符：= != ~ !~
// 组合关键字：AND / OR / NOT（大小写敏感，全大写）
// 括号：() 用于分组
//
// message 字段仅支持 ~ 与 !~ 操作符（按子串匹配语义）。
package logstore

import (
	"errors"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// AST 节点定义
// ---------------------------------------------------------------------------

// QueryNode 查询 AST 节点。Match 判断日志条目是否匹配此查询节点。
type QueryNode interface {
	Match(e *Entry) bool
}

// AndNode AND 组合：左右同时匹配。
type AndNode struct {
	Left, Right QueryNode
}

// Match 实现 QueryNode。
func (n *AndNode) Match(e *Entry) bool {
	if n == nil || n.Left == nil || n.Right == nil {
		return false
	}
	return n.Left.Match(e) && n.Right.Match(e)
}

// OrNode OR 组合：左右任一匹配。
type OrNode struct {
	Left, Right QueryNode
}

// Match 实现 QueryNode。
func (n *OrNode) Match(e *Entry) bool {
	if n == nil || n.Left == nil || n.Right == nil {
		return false
	}
	return n.Left.Match(e) || n.Right.Match(e)
}

// NotNode NOT 取反。
type NotNode struct {
	Child QueryNode
}

// Match 实现 QueryNode。
func (n *NotNode) Match(e *Entry) bool {
	if n == nil || n.Child == nil {
		// nil 子节点视为不匹配，取反后为 true 不合理；保守返回 false。
		return false
	}
	return !n.Child.Match(e)
}

// FieldEq 字段等于值（field=value）。
type FieldEq struct {
	Field, Value string
}

// Match 实现 QueryNode。
func (n *FieldEq) Match(e *Entry) bool {
	if n == nil {
		return false
	}
	return fieldValue(e, n.Field) == n.Value
}

// FieldNotEq 字段不等于值（field!=value）。
type FieldNotEq struct {
	Field, Value string
}

// Match 实现 QueryNode。
func (n *FieldNotEq) Match(e *Entry) bool {
	if n == nil {
		return false
	}
	return fieldValue(e, n.Field) != n.Value
}

// FieldContains 字段包含子串（field~"value"）。
type FieldContains struct {
	Field, Value string
}

// Match 实现 QueryNode。
func (n *FieldContains) Match(e *Entry) bool {
	if n == nil {
		return false
	}
	return strings.Contains(fieldValue(e, n.Field), n.Value)
}

// FieldNotContains 字段不包含子串（field!~"value"）。
type FieldNotContains struct {
	Field, Value string
}

// Match 实现 QueryNode。
func (n *FieldNotContains) Match(e *Entry) bool {
	if n == nil {
		return false
	}
	return !strings.Contains(fieldValue(e, n.Field), n.Value)
}

// ---------------------------------------------------------------------------
// 字段提取与查询应用
// ---------------------------------------------------------------------------

// fieldValue 从 Entry 提取指定字段的值。未知字段返回空串。
// 支持字段：level / device / agent / source / message / task。
func fieldValue(e *Entry, field string) string {
	if e == nil {
		return ""
	}
	switch field {
	case "level":
		return e.Level
	case "device":
		return e.DeviceID
	case "agent":
		return e.AgentID
	case "source":
		return e.Source
	case "message":
		return e.Message
	case "task":
		return e.TaskID
	default:
		return ""
	}
}

// isKnownField 判断字段名是否受支持。
func isKnownField(f string) bool {
	switch f {
	case "level", "device", "agent", "source", "message", "task":
		return true
	default:
		return false
	}
}

// ApplyQuery 对日志列表应用查询过滤，返回匹配的子集（保持原顺序）。
// q 为 nil 时返回空切片（不视为“全部匹配”，避免误用）。
func ApplyQuery(entries []Entry, q QueryNode) []Entry {
	out := make([]Entry, 0, len(entries))
	if q == nil {
		return out
	}
	for i := range entries {
		if q.Match(&entries[i]) {
			out = append(out, entries[i])
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// 词法分析（tokenizer）
// ---------------------------------------------------------------------------

// tokenKind 标识 token 类型。
type tokenKind int

const (
	tokEOF    tokenKind = iota
	tokIdent            // 字段名 / 值（不带引号）
	tokString           // 带引号字符串
	tokOp               // 操作符 = != ~ !~
	tokAnd
	tokOr
	tokNot
	tokLParen
	tokRParen
)

// token 一个词法单元。
type token struct {
	kind  tokenKind
	value string // 对 tokIdent/tokString/tokOp 为字面值；关键字类为对应词
	pos   int    // 起始字节偏移（用于错误定位）
}

// lexer 词法分析器。
type lexer struct {
	s   string
	pos int
}

// newLexer 创建词法分析器。
func newLexer(s string) *lexer { return &lexer{s: s} }

// tokenize 完整扫描输入，返回 token 序列（含尾部 EOF）。
func (l *lexer) tokenize() ([]token, error) {
	var toks []token
	for {
		l.skipSpaces()
		if l.pos >= len(l.s) {
			toks = append(toks, token{kind: tokEOF, pos: l.pos})
			return toks, nil
		}
		c := l.s[l.pos]
		switch {
		case c == '(':
			toks = append(toks, token{kind: tokLParen, value: "(", pos: l.pos})
			l.pos++
		case c == ')':
			toks = append(toks, token{kind: tokRParen, value: ")", pos: l.pos})
			l.pos++
		case c == '=':
			toks = append(toks, token{kind: tokOp, value: "=", pos: l.pos})
			l.pos++
		case c == '~':
			toks = append(toks, token{kind: tokOp, value: "~", pos: l.pos})
			l.pos++
		case c == '!' && l.peek(1) == '=':
			toks = append(toks, token{kind: tokOp, value: "!=", pos: l.pos})
			l.pos += 2
		case c == '!' && l.peek(1) == '~':
			toks = append(toks, token{kind: tokOp, value: "!~", pos: l.pos})
			l.pos += 2
		case c == '!':
			return nil, fmt.Errorf("词法错误：位置 %d 处 '!' 后必须跟 '=' 或 '~'", l.pos)
		case c == '"':
			s, err := l.readString()
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{kind: tokString, value: s, pos: l.pos})
		case isIdentStart(c):
			t, err := l.readIdentOrKeyword()
			if err != nil {
				return nil, err
			}
			toks = append(toks, t)
		default:
			return nil, fmt.Errorf("词法错误：位置 %d 处出现未识别字符 %q", l.pos, c)
		}
	}
}

// skipSpaces 跳过空白与制表符。
func (l *lexer) skipSpaces() {
	for l.pos < len(l.s) && (l.s[l.pos] == ' ' || l.s[l.pos] == '\t' || l.s[l.pos] == '\n' || l.s[l.pos] == '\r') {
		l.pos++
	}
}

// peek 查看相对偏移 n 处的字符（越界返回 0）。
func (l *lexer) peek(n int) byte {
	if l.pos+n >= len(l.s) {
		return 0
	}
	return l.s[l.pos+n]
}

// isIdentStart 判断字节是否可作为标识符起始（字母 / 下划线 / 数字开头按值处理）。
func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
		c == '-' || c == '.'
}

// isIdentPart 判断字节是否可作为标识符后续字符。
func isIdentPart(c byte) bool {
	return isIdentStart(c)
}

// readString 读取双引号字符串（支持 \" 转义）。
func (l *lexer) readString() (string, error) {
	start := l.pos
	l.pos++ // 跳过起始引号
	var b strings.Builder
	for l.pos < len(l.s) {
		c := l.s[l.pos]
		if c == '\\' {
			// 仅支持 \" 与 \\ 转义；其余按字面保留。
			next := l.peek(1)
			if next == '"' || next == '\\' {
				b.WriteByte(next)
				l.pos += 2
				continue
			}
			b.WriteByte(c)
			l.pos++
			continue
		}
		if c == '"' {
			l.pos++ // 跳过结束引号
			_ = start
			return b.String(), nil
		}
		b.WriteByte(c)
		l.pos++
	}
	return "", fmt.Errorf("词法错误：位置 %d 处字符串未闭合", start)
}

// readIdentOrKeyword 读取标识符；若为 AND/OR/NOT 关键字则返回对应 token。
func (l *lexer) readIdentOrKeyword() (token, error) {
	start := l.pos
	for l.pos < len(l.s) && isIdentPart(l.s[l.pos]) {
		l.pos++
	}
	word := l.s[start:l.pos]
	switch word {
	case "AND":
		return token{kind: tokAnd, value: word, pos: start}, nil
	case "OR":
		return token{kind: tokOr, value: word, pos: start}, nil
	case "NOT":
		return token{kind: tokNot, value: word, pos: start}, nil
	}
	return token{kind: tokIdent, value: word, pos: start}, nil
}

// ---------------------------------------------------------------------------
// 递归下降 parser
// ---------------------------------------------------------------------------

// parser 递归下降语法分析器。
type parser struct {
	toks []token
	idx  int
}

// newParser 创建 parser。
func newParser(toks []token) *parser { return &parser{toks: toks} }

// cur 当前 token。
func (p *parser) cur() token {
	if p.idx >= len(p.toks) {
		return token{kind: tokEOF}
	}
	return p.toks[p.idx]
}

// advance 前进一个 token。
func (p *parser) advance() token {
	t := p.cur()
	p.idx++
	return t
}

// expect 期望当前 token 为指定类型，否则返回错误。
func (p *parser) expect(k tokenKind, what string) (token, error) {
	t := p.cur()
	if t.kind != k {
		return token{}, fmt.Errorf("语法错误：位置 %d 处期望 %s，实际为 %q", t.pos, what, t.value)
	}
	p.advance()
	return t, nil
}

// parse 解析整棵 AST（入口，等价于 parseOr）。
func (p *parser) parse() (QueryNode, error) {
	node, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.cur().kind != tokEOF {
		t := p.cur()
		return nil, fmt.Errorf("语法错误：位置 %d 处出现多余 token %q", t.pos, t.value)
	}
	return node, nil
}

// parseOr: parseAnd (OR parseAnd)*
func (p *parser) parseOr() (QueryNode, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.cur().kind == tokOr {
		p.advance()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &OrNode{Left: left, Right: right}
	}
	return left, nil
}

// parseAnd: parseNot (AND parseNot)*
func (p *parser) parseAnd() (QueryNode, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.cur().kind == tokAnd {
		p.advance()
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = &AndNode{Left: left, Right: right}
	}
	return left, nil
}

// parseNot: NOT parseNot | parsePrimary
func (p *parser) parseNot() (QueryNode, error) {
	if p.cur().kind == tokNot {
		p.advance()
		child, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &NotNode{Child: child}, nil
	}
	return p.parsePrimary()
}

// parsePrimary: '(' parseOr ')' | field op value
func (p *parser) parsePrimary() (QueryNode, error) {
	t := p.cur()
	if t.kind == tokLParen {
		p.advance()
		node, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tokRParen, "')'"); err != nil {
			return nil, err
		}
		return node, nil
	}
	// 字段表达式：field op value
	if t.kind != tokIdent {
		return nil, fmt.Errorf("语法错误：位置 %d 处期望字段名或 '('，实际为 %q", t.pos, t.value)
	}
	if !isKnownField(t.value) {
		return nil, fmt.Errorf("语法错误：位置 %d 处未知字段 %q", t.pos, t.value)
	}
	field := p.advance().value

	op := p.cur()
	if op.kind != tokOp {
		return nil, fmt.Errorf("语法错误：位置 %d 处期望操作符（= != ~ !~），实际为 %q", op.pos, op.value)
	}
	p.advance()

	val := p.cur()
	if val.kind != tokIdent && val.kind != tokString {
		return nil, fmt.Errorf("语法错误：位置 %d 处期望值，实际为 %q", val.pos, val.value)
	}
	p.advance()

	// message 字段仅支持 ~ 与 !~ 操作符。
	if field == "message" && op.value != "~" && op.value != "!~" {
		return nil, fmt.Errorf("语法错误：message 字段仅支持 ~ 与 !~ 操作符，得到 %q", op.value)
	}

	switch op.value {
	case "=":
		return &FieldEq{Field: field, Value: val.value}, nil
	case "!=":
		return &FieldNotEq{Field: field, Value: val.value}, nil
	case "~":
		return &FieldContains{Field: field, Value: val.value}, nil
	case "!~":
		return &FieldNotContains{Field: field, Value: val.value}, nil
	default:
		return nil, fmt.Errorf("内部错误：未知操作符 %q", op.value)
	}
}

// ---------------------------------------------------------------------------
// 公共入口
// ---------------------------------------------------------------------------

// ErrEmptyQuery 空查询错误。
var ErrEmptyQuery = errors.New("查询不能为空")

// ParseQuery 解析查询字符串为 AST。
//
// 语法示例：
//
//	level=error
//	level=error AND device=dev-1
//	source=task AND (level=warn OR level=error)
//	level=error AND message~"panic"
//	level!=info
//
// 空串返回 ErrEmptyQuery。
func ParseQuery(s string) (QueryNode, error) {
	if strings.TrimSpace(s) == "" {
		return nil, ErrEmptyQuery
	}
	l := newLexer(s)
	toks, err := l.tokenize()
	if err != nil {
		return nil, err
	}
	p := newParser(toks)
	node, err := p.parse()
	if err != nil {
		return nil, err
	}
	return node, nil
}
