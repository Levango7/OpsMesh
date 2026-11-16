package controlplane

// b39_injection_test.go — 守卫（fileshare_ops.go 型式收口·构造即校验）。
//
// fileshare_ops.go 的 buildFSShareScript 改为**只接受 validatedFSShare**；
// 原始请求体 struct (fsShareReq) 不再能到达 builder。
// 与 db_ops.go / vector_ops.go / k8s_provision.go / monitor_ops.go / middleware_ops.go / mongo_ops.go / ansible_ops.go / dns_ops.go / env_ops.go
// 同等「架构级」保障。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"strings"
	"testing"
)

// TestB39BuilderParamsAreValidatedOnly 判据①：build* 入参只能是 validated* 或 *strings.Builder / string。
func TestB39BuilderParamsAreValidatedOnly(t *testing.T) {
	const f = "fileshare_ops.go"
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
			ts := exprTypeStringFS(field.Type)
			if ts == "*strings.Builder" || ts == "string" {
				continue
			}
			for _, raw := range []string{"fsShareReq"} {
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

// exprTypeStringFS 取表达式字符串形式（只处理标识符与指针/selector 表达式）。
func exprTypeStringFS(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.StarExpr:
		return "*" + exprTypeStringFS(x.X)
	case *ast.SelectorExpr:
		return exprTypeStringFS(x.X) + "." + x.Sel.Name
	default:
		return ""
	}
}

// TestB39WhitelistStillRejects 判据②：既有白名单不失效。
func TestB39WhitelistStillRejects(t *testing.T) {
	s := newShellQuoteServer()

	// 未识别的 type 必须被拒（400）。
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleFSShare(w, r, "deploy")
	}, map[string]interface{}{
		"deviceID": "b39-fs-type",
		"type":     "nfs;id",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("type 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// 非法 exportPath 必须被拒（400）。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleFSShare(w, r, "mount")
	}, map[string]interface{}{
		"deviceID":   "b39-fs-exp",
		"op":         "mount",
		"server":     "nfs.example.com",
		"exportPath": "/exports/data;rm -rf /",
		"mountPoint": "/mnt/data",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("exportPath 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}
}

// TestB39LegitValuesNotBlocked 判据③：合法值不被误拒。
func TestB39LegitValuesNotBlocked(t *testing.T) {
	s := newShellQuoteServer()

	// 合法 type（nfs）不应被拒。
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleFSShare(w, r, "deploy")
	}, map[string]interface{}{
		"deviceID": "b39-fs-ok",
		"type":     "nfs",
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法请求不应被拒：%d %s", rec.Code, rec.Body.String())
	}
}
