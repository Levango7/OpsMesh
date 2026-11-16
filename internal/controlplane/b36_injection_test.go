package controlplane

// b36_injection_test.go — 守卫（ansible_ops.go 型式收口·构造即校验）。
//
// ansible_ops.go 的构建函数改为**只接受 validated* 类型**
// (validateAnsibleAdhoc / validateAnsiblePlaybook / validateAnsibleInventory)。
// 原始请求体 struct (ansibleAdhocReq / ansiblePlaybookReq / ansibleInventoryReq) 不再能到达 build* 函数。
// 与 db_ops.go / vector_ops.go / k8s_provision.go / monitor_ops.go / middleware_ops.go / mongo_ops.go
// 同等「架构级」保障。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"strings"
	"testing"
)

// TestB36BuilderParamsAreValidatedOnly 判据①：build* 入参只能是 validated* 或 *strings.Builder / string。
func TestB36BuilderParamsAreValidatedOnly(t *testing.T) {
	const f = "ansible_ops.go"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, f, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("解析 %s 失败：%v", f, err)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || !strings.HasPrefix(fn.Name.Name, "build") {
			return true
		}
		for i, field := range fn.Type.Params.List {
			ts := exprTypeStringAns(field.Type)
			if ts == "*strings.Builder" || ts == "string" {
				continue
			}
			for _, raw := range []string{"ansibleAdhocReq", "ansiblePlaybookReq", "ansibleInventoryReq"} {
				if strings.Contains(ts, raw) {
					t.Errorf("%s:%d builder %s 入参 %d 仍包含原始 req 类型 %q", f, fset.Position(field.Pos()).Line, fn.Name.Name, i, raw)
				}
			}
			if !strings.HasPrefix(ts, "validated") {
				t.Errorf("%s:%d builder %s 入参 %d 类型 = %q，必须以 validated 开头", f, fset.Position(field.Pos()).Line, fn.Name.Name, i, ts)
			}
		}
		return true
	})
}

// exprTypeStringAns 取表达式字符串形式（只处理标识符与指针/selector 表达式）。
func exprTypeStringAns(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.StarExpr:
		return "*" + exprTypeStringAns(x.X)
	case *ast.SelectorExpr:
		return exprTypeStringAns(x.X) + "." + x.Sel.Name
	default:
		return ""
	}
}

// TestB36WhitelistStillRejects 判据②：既有白名单不失效。
func TestB36WhitelistStillRejects(t *testing.T) {
	s := newShellQuoteServer()

	// 未识别的 module 必须被拒（400）。
	rec := sqPost(t, s, s.handleAnsibleAdhoc, map[string]interface{}{
		"deviceID": "b36-ansible-mod",
		"module":   "shell;id",
		"pattern":  "all",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("module 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// 未识别的 action 必须被拒（400）。
	rec = sqPost(t, s, s.handleAnsiblePlaybook, map[string]interface{}{
		"deviceID": "b36-ansible-act",
		"action":   "run;id",
		"name":     "site",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("action 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}
}

// TestB36LegitValuesNotBlocked 判据③：合法值不被误拒。
func TestB36LegitValuesNotBlocked(t *testing.T) {
	s := newShellQuoteServer()

	// 合法 module（ping）不应被拒。
	rec := sqPost(t, s, s.handleAnsibleAdhoc, map[string]interface{}{
		"deviceID": "b36-ansible-ok",
		"module":   "ping",
		"pattern":  "all",
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法请求不应被拒：%d %s", rec.Code, rec.Body.String())
	}
}
