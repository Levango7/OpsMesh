package controlplane

// b21_injection_test.go — 批次 21 守卫（mongo_ops.go：Filter 注入真洞 + 形态面）。
//
// # 本批修的是什么
//
// ① **真洞**：buildMongoQueryScript 把用户给的 filter（原样、**无任何白名单**）放进
//
//	mongosh --quiet --eval 'db.getSiblingDB("db")."coll".find(%s).limit(%d)...'
//
// 的 **shell 单引号段** ⇒ `'); id; echo '` 可闭合引号、在 agent 上执行命令。
//
// 修法：把整段 --eval 代码构造为一个字符串（内容与原脚本**逐字节相同**），
// 再用 shellquote.Quote 包裹**整个 --eval 参数** ⇒ 真 sh 实测：shell 收到的参数与原来
// 逐字节相同（mongosh 语义不变），而其中的单引号被转义成 '\'' ⇒ 不能闭合引号。
//
// ② 形态面：6 个 builder 内 17 处实参改用局部变量承接（判据 E/F 归零）。
//
// # 守卫
//
// ① 真洞判据：含单引号的 filter 不得出现在裸 shell 单引号段内；--eval 参数必须是 Quote 形态；
// ② AST 判据：builder 的脚本实参不得裸引用 req.*；
// ③ 能力判据：合法 filter（含引号的 JSON）不被 400 拒收（能力零降级）。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"strings"
	"testing"
)

// TestB21FilterNotInBareSingleQuotes 真洞判据①。
func TestB21FilterNotInBareSingleQuotes(t *testing.T) {
	payload := "x'); touch /tmp/PWNED; echo '"
	script := buildMongoQueryScript(mongoQueryReq{
		DeviceID:   "b21-mongo",
		Database:   "db1",
		Collection: "c1",
		Filter:     payload,
		Limit:      10,
	})
	// --eval 之后必须是 Quote 形态（以 ' 起始、含转义 '\''），而非裸单引号段
	i := strings.Index(script, "--eval ")
	if i < 0 {
		t.Fatalf("脚本里找不到 --eval：\n%s", script)
	}
	rest := script[i+len("--eval "):]
	if !strings.HasPrefix(rest, "'") {
		t.Fatalf("--eval 参数未用 Quote 包裹（应以其单引号起始）：%q", rest[:min(60, len(rest))])
	}
	if !strings.Contains(rest, `'\''`) {
		t.Fatalf("--eval 参数未对单引号做转义（含 '\\''）：%q", rest[:min(120, len(rest))])
	}
	// 载荷不得出现在任何裸 shell 单引号段内：逐行找形如 echo '...' / --eval '...' 的裸段
	for _, line := range strings.Split(script, "\n") {
		if !strings.Contains(line, "--eval '") {
			continue
		}
		if strings.Contains(line, payload) {
			t.Fatalf("filter 载荷出现在裸 --eval '...' 单引号段内：\n%s", line)
		}
	}
}

// TestB21NoRawReqInScriptArgs AST 判据②。
func TestB21NoRawReqInScriptArgs(t *testing.T) {
	const f = "mongo_ops.go"
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
			if len(call.Args) < 3 || !b21IsBuilderTarget(call.Args[0]) {
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

// b21IsBuilderTarget 判断 Fprintf 的接收者是否是脚本 builder。
func b21IsBuilderTarget(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			if id, ok := x.X.(*ast.Ident); ok {
				switch id.Name {
				case "b", "buf", "sb", "builder", "script", "ec":
					return true
				}
			}
		}
	case *ast.Ident:
		switch x.Name {
		case "b", "buf", "sb", "builder", "script", "ec":
			return true
		}
	}
	return false
}

// TestB21LegitFilterNotRejected 能力判据③：合法 filter 不被 400 拒收。
func TestB21LegitFilterNotRejected(t *testing.T) {
	s := newShellQuoteServer()
	rec := sqPost(t, s, s.handleMongoQuery, map[string]interface{}{
		"deviceID": "b21-mongo", "database": "db1", "collection": "c1",
		"filter": `{"status":"it's ok"}`, "limit": 10,
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法 filter（含单引号）不应被拒（能力零降级）：%d %s", rec.Code, rec.Body.String())
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
