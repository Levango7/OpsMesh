package controlplane

// b16_injection_test.go — 批次 16（真洞剩余：system_ops useradd 的 %q + k8s CertHash）守卫。
//
// # 本批修的是什么
//
// ① `system_ops.go` 的 `buildUserAddScript` 用 **Go 的 %q** 写 useradd 的实参
//    （`id %q` / `useradd %q` / `-g %q` / `-d %q` / `-s %q`）。%q **不是 shell 转义**：
//    它只转义引号与反斜杠，`$` 与反引号原样保留 ⇒ 在 shell 的双引号段里照样展开
//    ⇒ `$(id)` 可直达 agent 的 `sh -c`。本批在 handler 侧补白名单
//    （username → opsIdentifier；home/shell → shellquote.ValidatePath：绝对路径、无 shell 元字符），
//    合法输入下 %q 与 shellQuote 的输出**逐字节一致** ⇒ 脚本文本不变。
//
// ② `k8s_provision.go` 的 `req.CertHash` 进 `kubeadm join --discovery-token-ca-cert-hash`
//    命令位 ⇒ 补 `sha256:<hex>` 白名单。
//
// # 守卫
//
// ① 实参级 AST 守卫（带类型感知）：脚本拼装的字符串槽实参不得裸引用 req.*；
// ② 白名单拒收语义判据：username / home / shell / certHash 的注入载荷必须 400 且不下发脚本。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"strings"
	"testing"
)

// b16Files 是本批守卫覆盖的源文件（相对本包目录）。
var b16Files = []string{
	"system_ops.go",
}

// b16SafeHelpers 是本批认可的「收口助手」集合。
var b16SafeHelpers = []string{
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

// TestB16NoRawReqInScriptBuilders 判据①：脚本拼装的字符串槽实参不得裸引用 req.*。
func TestB16NoRawReqInScriptBuilders(t *testing.T) {
	for _, f := range b16Files {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, f, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("解析 %s 失败：%v", f, err)
		}
		types := b16FieldTypes(file)
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
				if len(call.Args) < 3 || !b16IsBuilderTarget(call.Args[0]) {
					return true
				}
				args = call.Args[2:]
			default:
				return true
			}
			for _, a := range args {
				if !b16ArgTainted(a, types) {
					continue
				}
				src := b9Src(fset, a)
				if b16HasSafeHelper(src) {
					continue
				}
				t.Errorf("%s:%d 脚本实参裸引用用户字段：%s", f, fset.Position(a.Pos()).Line, src)
			}
			return true
		})
	}
}

// b16FieldTypes 收集文件内结构体字段类型（字段名 → 类型名）；同名只要有一处是 string 即按 string 处理。
func b16FieldTypes(file *ast.File) map[string]string {
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

// b16ArgTainted 判断实参是否携带未净化的字符串型用户字段。
func b16ArgTainted(e ast.Expr, types map[string]string) bool {
	switch x := e.(type) {
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok && id.Name == "req" {
			return types[x.Sel.Name] == "string"
		}
		return b16ArgTainted(x.X, types)
	case *ast.CallExpr:
		for _, a := range x.Args {
			if b16ArgTainted(a, types) {
				return true
			}
		}
		return false
	case *ast.BinaryExpr:
		return b16ArgTainted(x.X, types) || b16ArgTainted(x.Y, types)
	case *ast.ParenExpr:
		return b16ArgTainted(x.X, types)
	}
	return false
}

// b16BuilderTargets 是被认作「脚本 builder」的接收者形态。
var b16BuilderTargets = map[string]bool{
	"b":       true,
	"buf":     true,
	"sb":      true,
	"builder": true,
	"script":  true,
}

// b16IsBuilderTarget 判断 Fprintf 的接收者是否是脚本 builder。
func b16IsBuilderTarget(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			if id, ok := x.X.(*ast.Ident); ok {
				return b16BuilderTargets[id.Name]
			}
		}
	case *ast.Ident:
		return b16BuilderTargets[x.Name]
	}
	return false
}

// b16HasSafeHelper 判断实参文本里是否含被认可的收口助手。
func b16HasSafeHelper(src string) bool {
	for _, h := range b16SafeHelpers {
		if strings.Contains(src, h) {
			return true
		}
	}
	return false
}

// TestB16WhitelistRejects 判据②：useradd 与 join 的注入载荷必须 400，且不下发任何脚本。
func TestB16WhitelistRejects(t *testing.T) {
	s := newShellQuoteServer()
	payload := "$(touch marker)"

	// system users：username 走标识符白名单
	rec := sqPost(t, s, s.handleSystemUsers, map[string]interface{}{
		"agentID": "b16-sys", "username": payload,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("useradd username 注入载荷应被拒：%d %s", rec.Code, rec.Body.String())
	}

	// system users：home 走绝对路径白名单
	rec = sqPost(t, s, s.handleSystemUsers, map[string]interface{}{
		"agentID": "b16-sys", "username": "alice", "home": payload,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("useradd home 注入载荷应被拒：%d %s", rec.Code, rec.Body.String())
	}

	// system users：shell 走绝对路径白名单
	rec = sqPost(t, s, s.handleSystemUsers, map[string]interface{}{
		"agentID": "b16-sys", "username": "alice", "shell": payload,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("useradd shell 注入载荷应被拒：%d %s", rec.Code, rec.Body.String())
	}

	// k8s join：certHash 走 sha256:<hex> 白名单
	rec = sqPost(t, s, s.handleK8sJoin, map[string]interface{}{
		"deviceID": "b16-k8s", "masterAddr": "10.0.0.10", "token": "abcdef.0123456789abcdef",
		"certHash": payload,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("k8s certHash 注入载荷应被拒：%d %s", rec.Code, rec.Body.String())
	}
}
