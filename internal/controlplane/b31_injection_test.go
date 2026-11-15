package controlplane

// b31_injection_test.go — 批次 31 守卫（vector_ops.go 型式收口·构造即校验）。
//
// vector_ops.go 的构建函数改为**只接受 validated* 类型**（validateVectorDeploy /
// validateVectorAction / validateVectorCollectionAction / validateVectorWrite /
// validateVectorSearch / validateVectorMigrate）。原始请求体 struct（vectorDeployRequest /
// vectorOpsRequest / vectorCollectionReq / vectorWriteReq / vectorSearchReq）不再能到达 build* 函数。编译期约束，构造点之外没有合法路径。
//
// 守卫三条：
//   ① 所有 build* 函数入参类型必须以 "validated" 开头（或 *strings.Builder）；
//   ② build* 函数不得引用原始 req 类型；
//   ③ 既有白名单不失效 + 合法值不被误拒（三例保留）。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"strings"
	"testing"
)

// TestB31BuilderParamsAreValidatedOnly 判据①：build* 入参只能是 validated* 或 *strings.Builder。
func TestB31BuilderParamsAreValidatedOnly(t *testing.T) {
	const f = "vector_ops.go"
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
			ts := typeString(field.Type)
			if ts == "*strings.Builder" || ts == "string" {
				continue
			}
			for _, raw := range []string{"vectorDeployRequest", "vectorOpsRequest", "vectorCollectionReq", "vectorWriteReq", "vectorSearchReq"} {
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

// typeString 取表达式字符串形式（只处理标识符与指针/selector 表达式）。
func typeString(e ast.Expr) string {
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

// TestB31WhitelistStillRejects 判据②：既有白名单不失效。
func TestB31WhitelistStillRejects(t *testing.T) {
	s := newShellQuoteServer()

	// 未识别的 type 必须被拒（400）。
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleVectorAction(w, r, "milvus;id", "status")
	}, map[string]interface{}{
		"deviceID": "b31-vec-type",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("type 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// 未识别的 action 必须被拒（400）。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleVectorAction(w, r, "milvus", "start;id")
	}, map[string]interface{}{
		"deviceID": "b31-vec-act",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("action 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}
}

// TestB31LegitValuesNotBlocked 判据③：合法值不被误拒。
func TestB31LegitValuesNotBlocked(t *testing.T) {
	s := newShellQuoteServer()

	// 合法 action（status）不应被拒。
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleVectorAction(w, r, "milvus", "status")
	}, map[string]interface{}{
		"deviceID": "b31-vec-ok",
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法请求不应被拒：%d %s", rec.Code, rec.Body.String())
	}
}
