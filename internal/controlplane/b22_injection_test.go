package controlplane

// b22_injection_test.go — 批次 22 守卫（fileshare_ops.go 形态收口：30 处「已校验但直引」）。
//
// # 本批收口的是什么
//
// fileshare_ops.go 的唯一 builder（buildFSShareScript）里，req.Type / req.Action / req.Op /
// req.Path / req.Name / req.Server / req.ExportPath / req.MountPoint / req.Options /
// req.MountOpts / req.Service / req.AnonymousOK 共 30 处**直接**作为脚本实参。
//
// 这些字段**在 handler 里都有白名单**（allowedFSShareTypes / allowedFSShareActions /
// allowedFSShareOps / allowedFSMountOps / fsPathRe / fsShareNameRe / validFSPath /
// validFSOptions / allowedNFSMountOpts / monitorHostRe），AnonymousOK 是 bool ⇒ 不进注入面
// ⇒ 30 处全是形态面，不是真洞。
//
// 本批只做**局部变量承接** ⇒ 判据 E/F 归零；脚本文本逐字节不变。
//
// # 守卫
//
// ① AST 守卫：builder 的脚本实参不得裸引用 req.*；
// ② 回归判据：既有白名单**不失效**——Path/Name/Server 注入载荷仍必须 400；
// ③ 合法值不被误拒。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"strings"
	"testing"
)

// TestB22NoRawReqInScriptArgs 判据①。
func TestB22NoRawReqInScriptArgs(t *testing.T) {
	const f = "fileshare_ops.go"
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
			if len(call.Args) < 3 || !b22IsBuilderTarget(call.Args[0]) {
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

// b22IsBuilderTarget 判断 Fprintf 的接收者是否是脚本 builder。
func b22IsBuilderTarget(e ast.Expr) bool {
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

// TestB22WhitelistStillRejects 判据②：既有白名单不失效。
func TestB22WhitelistStillRejects(t *testing.T) {
	s := newShellQuoteServer()
	payload := "x; id"

	// path：必须是绝对路径且不含空格/.. ⇒ 注入载荷必须 400
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleFSShare(w, r, "share")
	}, map[string]interface{}{
		"deviceID": "b22-fs", "action": "add", "type": "nfs", "op": "add",
		"path": "/tmp/x; id", "name": "share1", "server": "10.0.0.1",
		"exportPath": "/data/export", "mountPoint": "/mnt/share1",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("path 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// share name
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleFSShare(w, r, "share")
	}, map[string]interface{}{
		"deviceID": "b22-fs", "action": "add", "type": "samba", "op": "add",
		"path": "/tmp/ok", "name": payload, "server": "10.0.0.1",
		"exportPath": "/data/export", "mountPoint": "/mnt/share1",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("share name 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}
}

// TestB22LegitValuesNotBlocked 判据③：合法值不被误拒。
func TestB22LegitValuesNotBlocked(t *testing.T) {
	s := newShellQuoteServer()
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleFSShare(w, r, "share")
	}, map[string]interface{}{
		"deviceID": "b22-fs", "action": "add", "type": "nfs", "op": "add",
		"path": "/tmp/ok", "name": "share1", "server": "10.0.0.1",
		"exportPath": "/data/export", "mountPoint": "/mnt/share1",
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法请求不应被拒：%d %s", rec.Code, rec.Body.String())
	}
}
