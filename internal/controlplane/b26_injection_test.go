package controlplane

// b26_injection_test.go — 批次 26 守卫（kernel_ops.go 形态收口）。
//
// kernel_ops.go 4 个 builder：buildKernelParamScript / buildKernelModuleScript /
// buildKernelUpgradeScript / buildKernelGrubScript
// 里 req.Action/req.Name/req.Value/req.Version/req.Index 直接作为脚本实参。
//
// 这些字段**在 handler 里都有白名单**：
//   - Action → allowedKernelParamActions / allowedKernelModuleActions /
//     allowedKernelUpgradeActions / allowedKernelGrubActions
//   - Name(Sysctl) → kernelParamRe（sysctl key）
//   - Name(模块) → kernelModuleRe
//   - Value → kernelValueRe
//   - Version → kernelVersionRe
//   - Index → handler 显式校验 >= 0
//   - DeviceID → 仅进 createSystemTask 不进脚本
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

// TestB26NoRawReqInScriptArgs 判据①。
func TestB26NoRawReqInScriptArgs(t *testing.T) {
	const f = "kernel_ops.go"
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
			if len(call.Args) < 3 || !b26IsBuilderTarget(call.Args[0]) {
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

// b26IsBuilderTarget 判断 Fprintf 的接收者是否是脚本 builder。
func b26IsBuilderTarget(e ast.Expr) bool {
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

// TestB26WhitelistStillRejects 判据②：既有白名单不失效。
func TestB26WhitelistStillRejects(t *testing.T) {
	s := newShellQuoteServer()

	// 未识别的 action 必须被拒（400）。
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleKernelParam(w, r)
	}, map[string]interface{}{
		"deviceID": "b26-kernel-param",
		"action":   "query;id",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("action 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// 参数名注入载荷必须被拒（400）。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleKernelParam(w, r)
	}, map[string]interface{}{
		"deviceID": "b26-kernel-param2", "action": "set", "name": "a;id", "value": "1",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("参数名注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// 模块名注入载荷必须被拒（400）。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleKernelModule(w, r)
	}, map[string]interface{}{
		"deviceID": "b26-kernel-mod", "action": "load", "name": "x;id",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("模块注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// 内核版本注入载荷必须被拒（400）。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleKernelUpgrade(w, r)
	}, map[string]interface{}{
		"deviceID": "b26-kernel-ver", "action": "install", "version": "5.10.0;id",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("版本注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}
}

// TestB26LegitValuesNotBlocked 判据③：合法值不被误拒。
func TestB26LegitValuesNotBlocked(t *testing.T) {
	s := newShellQuoteServer()

	// 合法 sysctl 查询不应被拒。
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleKernelParam(w, r)
	}, map[string]interface{}{
		"deviceID": "b26-kernel-ok", "action": "query", "name": "net.ipv4.ip_forward",
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法请求不应被拒：%d %s", rec.Code, rec.Body.String())
	}

	// 合法模块加载不应被拒。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleKernelModule(w, r)
	}, map[string]interface{}{
		"deviceID": "b26-kernel-ok2", "action": "load", "name": "br_netfilter",
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法请求不应被拒：%d %s", rec.Code, rec.Body.String())
	}
}
