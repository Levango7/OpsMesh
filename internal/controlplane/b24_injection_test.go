package controlplane

// b24_injection_test.go — 批次 24 守卫（lb_ops.go 形态收口：~21 处「已校验但直引」）。
//
// lb_ops.go 4 个 builder（buildLBDeployScript / buildLBActionScript / buildLBBackendScript /
// buildLBFrontendScript）内 req.Type/req.Action/req.Mode/req.Version/req.DeviceID/req.Backend/
// req.Frontend/req.Weight 等字段直接作为脚本实参。
//
// 这些字段**在 handler 里都有白名单**：
//   - Type/Action → allowedLBTypes / allowedLBActions / allowedLBResourceActions（枚举）
//   - Mode        → allowedLBModes[type][mode]
//   - Version     → middlewareVersionRe
//   - Backend/Frontend → lbIPPortRe（IP/HOST:port）
//   - Weight      → handler 内正整数校验
//   - DeviceID    → 仅进任务参数，不进脚本
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

// b24IsBuilderTarget 判断 Fprintf 的接收者是否是脚本 builder。
func b24IsBuilderTarget(e ast.Expr) bool {
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

// TestB24NoRawReqInScriptArgs 判据①。
func TestB24NoRawReqInScriptArgs(t *testing.T) {
	const f = "lb_ops.go"
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
			if len(call.Args) < 3 || !b24IsBuilderTarget(call.Args[0]) {
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

// TestB24WhitelistStillRejects 判据②：既有白名单不失效。
func TestB24WhitelistStillRejects(t *testing.T) {
	s := newShellQuoteServer()

	// 未识别的 LB 类型必须被拒（400）。
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleLBDeploy(w, r)
	}, map[string]interface{}{
		"deviceID": "b24-lb-dep", "type": "rs; id", "version": "1.0", "mode": "single",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("type 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// Backend 必须匹配 lbIPPortRe。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleLBBackend(w, r, "haproxy")
	}, map[string]interface{}{
		"deviceID": "b24-lb-be", "action": "add", "backend": "10.0.0.1:80; id", "weight": "1",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("backend 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// Action 白名单。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleLBBackend(w, r, "haproxy")
	}, map[string]interface{}{
		"deviceID": "b24-lb-act", "action": "deploy;id", "backend": "10.0.0.1:80", "weight": "1",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("action 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}
}

// TestB24LegitValuesNotBlocked 判据③：合法值不被误拒。
func TestB24LegitValuesNotBlocked(t *testing.T) {
	s := newShellQuoteServer()
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleLBDeploy(w, r)
	}, map[string]interface{}{
		"deviceID": "b24-lb-ok", "type": "haproxy", "version": "2.8", "mode": "single",
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法请求不应被拒：%d %s", rec.Code, rec.Body.String())
	}
}
