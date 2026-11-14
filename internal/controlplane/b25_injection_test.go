package controlplane

// b25_injection_test.go — 批次 25 守卫（harbor_ops.go 形态收口）。
//
// harbor_ops.go 5 个 builder：buildHarborDeployScript / buildHarborActionScript /
// buildHarborProjectScript / buildHarborRepositoryScript / buildHarborArtifactScript
// 里 req.Action/req.Project/req.Repository/req.Reference 等直接作为脚本实参。
//
// 这些字段**在 handler 里都有白名单**：
//   - Action → allowedHarborActions / allowedHarborResourceActions
//   - Project/Repository/Reference → harborProjectRe / harborRepositoryRe / harborArtifactRe
//   - DeviceID → 仅进 createSystemTask 不进脚本
//   - Mode/Version → harborDeployRequest 仅在 deploy 分支中静态渲染（HARBOR_VERSION env 块硬编码 v2.8.0）
//
// 全形态面，无真洞。本批只做**局部变量承接** ⇒ 判据 E/F 归零；脚本文本逐字节不变。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"strings"
	"testing"
)

// TestB25NoRawReqInScriptArgs 判据①。
func TestB25NoRawReqInScriptArgs(t *testing.T) {
	const f = "harbor_ops.go"
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
			if len(call.Args) < 3 || !b25IsBuilderTarget(call.Args[0]) {
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

// b25IsBuilderTarget 判断 Fprintf 的接收者是否是脚本 builder。
func b25IsBuilderTarget(e ast.Expr) bool {
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

// TestB25WhitelistStillRejects 判据②：既有白名单不失效。
func TestB25WhitelistStillRejects(t *testing.T) {
	s := newShellQuoteServer()

	// 未识别的 action 必须被拒（400）。
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleHarborAction(w, r, "deploy;id")
	}, map[string]interface{}{
		"deviceID": "b25-harbor-act",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("action 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// Project 注入载荷必须被拒（400）。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleHarborProject(w, r)
	}, map[string]interface{}{
		"deviceID": "b25-harbor-pro", "action": "create", "project": "x;id",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("project 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// Repository 注入载荷必须被拒（400）。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleHarborRepository(w, r)
	}, map[string]interface{}{
		"deviceID": "b25-harbor-repo", "action": "delete", "project": "test",
		"repository": "x;id",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("repository 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}
}

// TestB25LegitValuesNotBlocked 判据③：合法值不被误拒。
func TestB25LegitValuesNotBlocked(t *testing.T) {
	s := newShellQuoteServer()
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleHarborAction(w, r, "status")
	}, map[string]interface{}{
		"deviceID": "b25-harbor-ok",
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法请求不应被拒：%d %s", rec.Code, rec.Body.String())
	}
}
