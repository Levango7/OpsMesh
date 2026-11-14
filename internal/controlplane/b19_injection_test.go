package controlplane

// b19_injection_test.go — 批次 19 守卫（dns_ops.go：bind 分支 TXT/MX 记录值注入真洞 + 形态面）。
//
// # 本批修的是什么
//
// ① **真洞**：bind 的 add 分支把记录行放进 shell **单引号段**：
//
//	fmt.Fprintf(&b, "echo '%s %d IN %s %s' >> \"$ZONE_FILE\"\n", req.Name, req.TTL, req.RecordType, req.Value)
//
// 而 TXT 记录值经 regexpTXTValue 校验，其字符类**允许单引号 ' 与分号 ;**
// （TXT 本就该含这些字符）⇒ `x'; id; echo '` 可闭合单引号、在 agent 上执行命令。
//
// 修法：改用 **heredoc 承载整条记录**（内容字面、不展开、不闭合）。
// heredoc 提前闭合不可能：值已被 validDNSRecordValue 拒绝含 \r\n
// ⇒ 不可能出现「单独一行等于分隔符」。
//
// ② 形态面：buildDNSScript 内 11 个字段的实参改用局部变量承接（判据 E/F 归零）。
//
// # 守卫
//
// ① 真洞判据：含单引号/分号的 TXT 值 ⇒ 生成脚本里该值**只能**出现在 heredoc 内，
//    不得出现在任何 shell 单引号段中；
// ② 能力判据：合法 TXT 值（含引号与分号）**不得**被 400 拒收（本批不降 TXT 能力）；
// ③ AST 判据：builder 的 Fprintf 实参不得裸引用 req.*。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"strings"
	"testing"
)

// TestB19TXTValueOnlyInHeredoc 真洞判据①：注入载荷只能落在 heredoc 内。
func TestB19TXTValueOnlyInHeredoc(t *testing.T) {
	payload := "v'; id; echo '"
	script := buildDNSScript(dnsReq{
		DeviceID:   "b19-dns",
		Action:     "record",
		Type:       "bind",
		Op:         "add",
		Zone:       "opsmesh.internal",
		Name:       "txt1",
		Value:      payload,
		RecordType: "TXT",
		TTL:        300,
	})
	if !strings.Contains(script, "<< 'OPSMESH_DNS_EOF'") {
		t.Fatalf("bind 记录行应改由 heredoc 承载，实际脚本片段：\n%s", script)
	}
	if !strings.Contains(script, payload) {
		t.Fatalf("heredoc 内应保留 TXT 值原样（含引号/分号），实际脚本片段：\n%s", script)
	}
	// 载荷**只能**出现在 heredoc 块内：切出 heredoc 块，断言块外无 payload。
	lines := strings.Split(script, "\n")
	inHeredoc, block, outside := false, []string{}, []string{}
	for _, line := range lines {
		switch {
		case strings.Contains(line, "<< 'OPSMESH_DNS_EOF'"):
			inHeredoc = true
			outside = append(outside, line)
		case inHeredoc && strings.TrimSpace(line) == "OPSMESH_DNS_EOF":
			inHeredoc = false
			block = append(block, line)
		case inHeredoc:
			block = append(block, line)
		default:
			outside = append(outside, line)
		}
	}
	for _, line := range outside {
		if strings.Contains(line, payload) {
			t.Fatalf("TXT 载荷出现在 heredoc 之外（可闭合 shell 引号执行命令）：\n%s", line)
		}
	}
	hit := false
	for _, line := range block {
		if strings.Contains(line, payload) {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("heredoc 块内应保留 TXT 值原样，实际块：\n%s", strings.Join(block, "\n"))
	}
	// heredoc 载荷行必须以分隔符闭合（保证内容不会被 shell 解释）
	if !strings.Contains(script, "\nOPSMESH_DNS_EOF\n") {
		t.Fatalf("heredoc 缺少闭合分隔符，实际脚本片段：\n%s", script)
	}
}

// TestB19LegitTXTValueNotRejected 能力判据②：合法 TXT 值不被 400 拒收。
func TestB19LegitTXTValueNotRejected(t *testing.T) {
	s := newShellQuoteServer()
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleDNS(w, r, "record")
	}, map[string]interface{}{
		"deviceID": "b19-dns", "type": "bind", "op": "add",
		"zone": "opsmesh.internal", "name": "txt1",
		"recordType": "TXT", "value": "v'; id; echo '", "ttl": 300,
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法 TXT 值（含引号/分号）不应被拒（不降 TXT 能力）：%d %s", rec.Code, rec.Body.String())
	}
}

// TestB19NoRawReqInScriptArgs AST 判据③：builder 的 Fprintf 实参不得裸引用 req.*。
func TestB19NoRawReqInScriptArgs(t *testing.T) {
	const f = "dns_ops.go"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, f, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("解析 %s 失败：%v", f, err)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var args []ast.Expr
		switch b9CallName(call.Fun) {
		case "fmt.Sprintf":
			if len(call.Args) < 2 {
				return true
			}
			args = call.Args[1:]
		case "fmt.Fprintf":
			if len(call.Args) < 3 || !b19IsBuilderTarget(call.Args[0]) {
				return true
			}
			args = call.Args[2:]
		default:
			return true
		}
		for _, a := range args {
			src := b9Src(fset, a)
			if strings.Contains(src, "req.") {
				t.Errorf("%s:%d 脚本实参裸引用 req.*：%s", f, fset.Position(a.Pos()).Line, src)
			}
		}
		return true
	})
}

// b19IsBuilderTarget 判断 Fprintf 的接收者是否是脚本 builder。
func b19IsBuilderTarget(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			if id, ok := x.X.(*ast.Ident); ok {
				switch id.Name {
				case "b", "buf", "sb", "builder", "script", "rec":
					return true
				}
			}
		}
	case *ast.Ident:
		switch x.Name {
		case "b", "buf", "sb", "builder", "script", "rec":
			return true
		}
	}
	return false
}
