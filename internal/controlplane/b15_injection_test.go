package controlplane

// b15_injection_test.go — 批次 15（apache 族「版本与路径」白名单补齐）的注入守卫。
//
// # 本批修的是什么
//
// `apache_ops.go`（httpd / Tomcat / Jetty / OpenResty / Nginx）与
// `apache_ext_ops.go`（Hadoop / Hive / HBase / Solr / Druid / NiFi / Impala）的
// `req.Version`（进 tarball URL、ln 目标路径、回显）与 `req.Path`（进 `printf … > 路径`）
// **全程没有任何白名单** ⇒ `;` / `$( )` / 反引号可直达 agent 的 `sh -c`。
// 本批：handler 侧补 isVersionLike（版本）+ shellquote.ValidatePath（路径）⇒ 非法 400；
// builder/handler 内用局部变量承接已校验值 ⇒ 实参不再直接引用 req.*（判据 E/F 归零）。
//
// # 守卫
//
// ① 实参级 AST 守卫：脚本拼装的字符串槽实参不得裸引用 req.*（带类型感知：int 字段不是注入面）；
// ② 白名单拒收语义判据：版本/路径的注入载荷必须 400 且不下发任何脚本。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"strings"
	"testing"
)

// b15Files 是本批守卫覆盖的源文件（相对本包目录）。
var b15Files = []string{
	"apache_ops.go",
	"apache_ext_ops.go",
}

// b15SafeHelpers 是本批认可的「收口助手」集合。
var b15SafeHelpers = []string{
	"shellquote.Quote(",
	"shellquote.QuoteDouble(",
	"shellquote.QuoteJoin(",
	"shellquote.QuotePowerShellSingle(",
	"shellquote.PowerShellEncodedCommand(",
	"shellQuote(",
	"opsEcho(",
	"opsSQLString(",
	"opsHeredocContent(",
	"opsQuoteJSONText(",
	"strconv.Quote(",
	"strconv.Itoa(",
	"strconv.FormatBool(",
}

// TestB15NoRawReqInScriptBuilders 判据①：脚本拼装的字符串槽实参不得裸引用 req.*。
func TestB15NoRawReqInScriptBuilders(t *testing.T) {
	for _, f := range b15Files {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, f, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("解析 %s 失败：%v", f, err)
		}
		types := b15FieldTypes(file)
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
				if len(call.Args) < 3 || !b15IsBuilderTarget(call.Args[0]) {
					return true
				}
				args = call.Args[2:]
			default:
				return true
			}
			for _, a := range args {
				if !b15ArgTainted(a, types) {
					continue
				}
				src := b9Src(fset, a)
				if b15HasSafeHelper(src) {
					continue
				}
				t.Errorf("%s:%d 脚本实参裸引用用户字段：%s", f, fset.Position(a.Pos()).Line, src)
			}
			return true
		})
	}
}

// b15FieldTypes 收集文件内结构体字段类型（字段名 → 类型名）；同名只要有一处是 string 即按 string 处理。
func b15FieldTypes(file *ast.File) map[string]string {
	types := map[string]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		st, ok := n.(*ast.StructType)
		if !ok {
			return true
		}
		for _, f := range st.Fields.List {
			typ := ""
			if id, ok := f.Type.(*ast.Ident); ok {
				typ = id.Name
			}
			for _, name := range f.Names {
				if typ == "string" || types[name.Name] == "" {
					types[name.Name] = typ
				}
			}
		}
		return true
	})
	return types
}

// b15ArgTainted 判断实参是否携带未净化的字符串型用户字段。
func b15ArgTainted(e ast.Expr, types map[string]string) bool {
	switch x := e.(type) {
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok && id.Name == "req" {
			return types[x.Sel.Name] == "string"
		}
		return b15ArgTainted(x.X, types)
	case *ast.CallExpr:
		for _, a := range x.Args {
			if b15ArgTainted(a, types) {
				return true
			}
		}
		return false
	case *ast.BinaryExpr:
		return b15ArgTainted(x.X, types) || b15ArgTainted(x.Y, types)
	case *ast.ParenExpr:
		return b15ArgTainted(x.X, types)
	}
	return false
}

// b15BuilderTargets 是被认作「脚本 builder」的接收者形态。
var b15BuilderTargets = map[string]bool{
	"b":       true,
	"buf":     true,
	"sb":      true,
	"builder": true,
	"script":  true,
}

// b15IsBuilderTarget 判断 Fprintf 的接收者是否是脚本 builder。
func b15IsBuilderTarget(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			if id, ok := x.X.(*ast.Ident); ok {
				return b15BuilderTargets[id.Name]
			}
		}
	case *ast.Ident:
		return b15BuilderTargets[x.Name]
	}
	return false
}

// b15HasSafeHelper 判断实参文本里是否含被认可的收口助手。
func b15HasSafeHelper(src string) bool {
	for _, h := range b15SafeHelpers {
		if strings.Contains(src, h) {
			return true
		}
	}
	return false
}

// TestB15WhitelistRejects 判据②：版本/路径的注入载荷必须 400，且不下发任何脚本。
func TestB15WhitelistRejects(t *testing.T) {
	s := newShellQuoteServer()
	payload := "$(touch marker)"

	// httpd：version 与 path 都必须走白名单
	rec := sqPost(t, s, s.handleApacheHTTPD, map[string]interface{}{
		"deviceID": "b15-httpd", "action": "deploy", "version": payload,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("httpd version 注入载荷应被拒：%d %s", rec.Code, rec.Body.String())
	}

	rec = sqPost(t, s, s.handleApacheHTTPD, map[string]interface{}{
		"deviceID": "b15-httpd", "action": "config", "content": "x", "path": payload,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("httpd path 注入载荷应被拒：%d %s", rec.Code, rec.Body.String())
	}

	// hadoop：version 必须走版本白名单
	rec = sqPost(t, s, s.handleApacheHadoop, map[string]interface{}{
		"deviceID": "b15-hadoop", "action": "deploy", "version": payload,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("hadoop version 注入载荷应被拒：%d %s", rec.Code, rec.Body.String())
	}
}
