package controlplane

// b20_injection_test.go — 批次 20 守卫（storage_ops.go 形态收口：29 处「已校验但直引」）。
//
// # 本批收口的是什么
//
// storage_ops.go 的 4 个 builder（buildStorageDeployScript / buildStorageActionScript /
// buildBucketScript / buildObjectScript）里，req.Type / req.Action / req.Bucket /
// req.Object / req.Local 共 29 处**直接**作为 fmt.Fprintf 的实参。
//
// 这些字段**在各自 handler 里都已有白名单**（allowedStorageTypes / allowedStorageActions /
// allowedBucketActions / allowedObjectActions / storageBucketRe / storageObjectRe），
// 且 storageBucketRe = ^[a-z0-9][a-z0-9.\-]{1,61}[a-z0-9]$|…、
// storageObjectRe = ^[a-zA-Z0-9._/\-]+$ **都不含 shell 元字符** ⇒ 它们不是「无校验的真洞」，
// 而是**形态问题**（已校验但直引）：实参仍裸引用 req.*，后续任何改动绕过校验点即可直接注入。
//
// 本批只做**局部变量承接** ⇒ 判据 E/F 归零，**脚本文本逐字节不变**
// （已用「反向替换后与 base 的 48 行脚本语句逐行比对」验证差异为 0）。
//
// # 守卫
//
// ① AST 守卫：builder 的脚本实参不得裸引用 req.*；
// ② 回归判据：既有白名单**不失效**——Bucket/Object 的注入载荷仍必须 400；
// ③ 合法值不被误拒。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"strings"
	"testing"
)

// TestB20NoRawReqInScriptArgs 判据①：脚本实参不得裸引用 req.*。
func TestB20NoRawReqInScriptArgs(t *testing.T) {
	const f = "storage_ops.go"
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
			if len(call.Args) < 3 || !b20IsBuilderTarget(call.Args[0]) {
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

// b20IsBuilderTarget 判断 Fprintf 的接收者是否是脚本 builder。
func b20IsBuilderTarget(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			if id, ok := x.X.(*ast.Ident); ok {
				switch id.Name {
				case "b", "buf", "sb", "builder", "script":
					return true
				}
			}
		}
	case *ast.Ident:
		switch x.Name {
		case "b", "buf", "sb", "builder", "script":
			return true
		}
	}
	return false
}

// TestB20WhitelistStillRejects 判据②：既有白名单不失效。
func TestB20WhitelistStillRejects(t *testing.T) {
	s := newShellQuoteServer()
	payload := "b; id"

	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleStorageBucket(w, r, "s3")
	}, map[string]interface{}{
		"deviceID": "b20-store", "action": "create", "bucket": payload,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bucket 名注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleStorageObject(w, r, "s3")
	}, map[string]interface{}{
		"deviceID": "b20-store", "action": "put", "bucket": "b1", "object": payload,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("object 名注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}
}

// TestB20LegitValuesNotBlocked 判据③：合法值不被误拒。
func TestB20LegitValuesNotBlocked(t *testing.T) {
	s := newShellQuoteServer()
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleStorageBucket(w, r, "s3")
	}, map[string]interface{}{
		"deviceID": "b20-ok", "action": "create", "bucket": "my-bucket-01",
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法 bucket 名不应被拒：%d %s", rec.Code, rec.Body.String())
	}
}
