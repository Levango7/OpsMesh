package controlplane

// b38_injection_test.go — 守卫（env_ops.go 型式收口·构造即校验）。
//
// env_ops.go 的构建函数改为**只接受 validated* 类型**
// (validateEnvInstall / validateEnvAction / validateFdLimit / validateIPVS)。
// 原始请求体 struct (envInstallReq / envQueryReq / fdLimitReq / ipvsReq) 不再能到达 build* 函数。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"strings"
	"testing"
)

// TestB38BuilderParamsAreValidatedOnly 判据①：build* 入参只能是 validated* 或 *strings.Builder / string。
func TestB38BuilderParamsAreValidatedOnly(t *testing.T) {
	const f = "env_ops.go"
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
			ts := exprTypeStringEnv(field.Type)
			if ts == "*strings.Builder" || ts == "string" {
				continue
			}
			for _, raw := range []string{"envInstallReq", "envQueryReq", "fdLimitReq", "ipvsReq"} {
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

// exprTypeStringEnv 取表达式字符串形式（只处理标识符与指针/selector 表达式）。
func exprTypeStringEnv(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.StarExpr:
		return "*" + exprTypeStringEnv(x.X)
	case *ast.SelectorExpr:
		return exprTypeStringEnv(x.X) + "." + x.Sel.Name
	default:
		return ""
	}
}

// TestB38WhitelistStillRejects 判据②：既有白名单不失效。
func TestB38WhitelistStillRejects(t *testing.T) {
	s := newShellQuoteServer()

	// 未识别的 envType 必须被拒（400）。
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleEnvInstall(w, r)
	}, map[string]interface{}{
		"deviceID": "b38-env-type",
		"type":     "java;id",
		"version":  "17",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("envType 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// 非法 protocol 必须被拒（400）。
	rec = sqPost(t, s, s.handleIPVS, map[string]interface{}{
		"deviceID": "b38-env-proto",
		"action":   "add",
		"vip":      "10.0.0.1",
		"port":     80,
		"protocol": "http",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("protocol 非法载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}
}

// TestB38LegitValuesNotBlocked 判据③：合法值不被误拒。
func TestB38LegitValuesNotBlocked(t *testing.T) {
	s := newShellQuoteServer()

	// 合法 envType（go）不应被拒。
	rec := sqPost(t, s, s.handleEnvInstall, map[string]interface{}{
		"deviceID": "b38-env-ok",
		"type":     "go",
		"version":  "1.22.0",
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法请求不应被拒：%d %s", rec.Code, rec.Body.String())
	}
}
