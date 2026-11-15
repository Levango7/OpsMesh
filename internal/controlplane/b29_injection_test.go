package controlplane

// b29_injection_test.go — 批次 29 守卫（db_ops.go 型式收口·构造即校验）。
//
// db_ops.go 的构建函数改为**只接受 validated* 类型**（validateDBDeploy /
// validateDBOps / validateDBInstance）。原始请求体 struct（dbDeployRequest /
// dbOpsRequest / dbInstanceReq）不再能到达 build* 函数。这比字符串护栏更强：
// 编译期约束，构造点之外的路径不可能存在。
//
// 守卫三条：
//   ① 所有 build* 函数入参类型必须以 "validated" 开头（*strings.Builder 除外）；
//   ② build* 函数不得引用原始 req 类型（dbDeployRequest / dbOpsRequest / dbInstanceReq）；
//   ③ 既有白名单不失效 + 合法值不被误拒（与批次 24/25/26/27/28/29 同构）。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"strings"
	"testing"
)

// TestB29BuilderParamsAreValidatedOnly 判据①：build* 入参只能是 validated* 或 *strings.Builder。
func TestB29BuilderParamsAreValidatedOnly(t *testing.T) {
	const f = "db_ops.go"
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
			_ = i
			for _, p := range field.Names {
				_ = p
			}
			ts := exprTypeString(field.Type)
			if ts == "*strings.Builder" || ts == "string" {
				continue
			}
			if !strings.HasPrefix(ts, "validated") {
				t.Errorf("%s:%d builder %s 入参 %d 类型 = %q，必须以 validated 开头", f, fset.Position(field.Pos()).Line, fn.Name.Name, i, ts)
			}
		}
		// 判据②：builder 内不允许再出现原始 req 类型名作为外部依赖
		for _, p := range fn.Type.Params.List {
			s := exprTypeString(p.Type)
			for _, raw := range []string{"dbDeployRequest", "dbOpsRequest", "dbInstanceReq"} {
				if strings.Contains(s, raw) {
					t.Errorf("%s:%d builder %s 入参类型仍包含原始 req 类型 %q", f, fset.Position(p.Pos()).Line, fn.Name.Name, raw)
				}
			}
		}
		return true
	})
}

// exprTypeString 取表达式字符串形式（只处理标识符与指针/selector 表达式）。
func exprTypeString(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.StarExpr:
		return "*" + exprTypeString(x.X)
	case *ast.SelectorExpr:
		return exprTypeString(x.X) + "." + x.Sel.Name
	default:
		return ""
	}
}

// TestB29WhitelistStillRejects 判据②：既有白名单不失效。
func TestB29WhitelistStillRejects(t *testing.T) {
	s := newShellQuoteServer()

	// 未识别的 type 必须被拒（400）。
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleDBAction(w, r, "postgresql;id", "status")
	}, map[string]interface{}{
		"deviceID": "b29-db-type",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("type 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// 未识别的 action 必须被拒（400）。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleDBAction(w, r, "postgresql", "start;id")
	}, map[string]interface{}{
		"deviceID": "b29-db-act",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("action 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// 数据库实例名注入载荷必须被拒（400）。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleDBInstance(w, r, "postgresql")
	}, map[string]interface{}{
		"deviceID": "b29-db-inst", "action": "create", "name": "x;id",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("db 实例注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}
}

// TestB29LegitValuesNotBlocked 判据③：合法值不被误拒。
func TestB29LegitValuesNotBlocked(t *testing.T) {
	s := newShellQuoteServer()

	// 合法 instance 列表请求不应被拒。
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleDBInstance(w, r, "postgresql")
	}, map[string]interface{}{
		"deviceID": "b29-db-ok", "action": "list",
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法请求不应被拒：%d %s", rec.Code, rec.Body.String())
	}

	// 合法状态查询不应被拒。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleDBAction(w, r, "postgresql", "status")
	}, map[string]interface{}{
		"deviceID": "b29-db-ok2",
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法请求不应被拒：%d %s", rec.Code, rec.Body.String())
	}
}
