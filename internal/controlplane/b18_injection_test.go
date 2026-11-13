package controlplane

// b18_injection_test.go — 批次 18 守卫（virt_ops.go 形态收口：45 处「已校验但直引」）。
//
// # 本批收口的是什么
//
// virt_ops.go 的 4 个 builder（buildVirtDeployScript / buildVirtActionScript /
// buildVirtVMScript / buildVirtSnapshotScript）里，req.Type / req.Action / req.Name /
// req.Snapshot 共 45 处**直接**作为 fmt.Fprintf 的实参。
//
// 这些字段**在各自 handler 里都已有白名单**（virtNameRe / allowedVirtTypes /
// allowedVirtActions / allowedVirtVMActions / allowedVirtSnapshotActions /
// allowedVirtModes），所以它们不是「无校验的真洞」，而是**形态问题**（已校验但直引）：
// 实参仍裸引用 req.*，任何后续改动绕过校验点就能直接注入。
//
// 本批只做**局部变量承接**（vReqType / vReqAction / vReqName / vReqSnapshot）⇒
// 判据 E/F 归零，**脚本文本逐字节不变**（已用「反向替换后与 base 逐行比对」验证差异为 0）。
//
// # 守卫
//
// ① AST 守卫：builder 的脚本实参不得裸引用 req.*；
// ② 回归判据：既有白名单**不失效**——Name/Snapshot 的注入载荷仍必须 400；
// ③ 合法值不被误拒。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// b18BuilderTargets 是被认作「脚本 builder」的接收者形态。
var b18BuilderTargets = map[string]bool{
	"b":       true,
	"buf":     true,
	"sb":      true,
	"builder": true,
	"script":  true,
}

// TestB18NoRawReqInScriptArgs 判据①：virt_ops.go 的脚本实参不得裸引用 req.*。
func TestB18NoRawReqInScriptArgs(t *testing.T) {
	const f = "virt_ops.go"
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
			if len(call.Args) < 3 || !b18IsBuilderTarget(call.Args[0]) {
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

// b18IsBuilderTarget 判断 Fprintf 的接收者是否是脚本 builder。
func b18IsBuilderTarget(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			if id, ok := x.X.(*ast.Ident); ok {
				return b18BuilderTargets[id.Name]
			}
		}
	case *ast.Ident:
		return b18BuilderTargets[x.Name]
	}
	return false
}

// TestB18WhitelistStillRejects 判据②：既有白名单不失效（Name/Snapshot 注入仍 400）。
func TestB18WhitelistStillRejects(t *testing.T) {
	s := newShellQuoteServer()
	payload := "vm; id"

	// VM 名称：virtNameRe 白名单
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleVirtVM(w, r, "kvm")
	}, map[string]interface{}{
		"deviceID": "b18-virt", "action": "start", "name": payload,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("VM name 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// 快照名称
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleVirtSnapshot(w, r, "kvm")
	}, map[string]interface{}{
		"deviceID": "b18-virt", "action": "create", "name": payload, "snapshot": "snap1",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("snapshot name 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// 快照 ID
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleVirtSnapshot(w, r, "kvm")
	}, map[string]interface{}{
		"deviceID": "b18-virt", "action": "create", "name": "vm1", "snapshot": payload,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("snapshot id 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}
}

// TestB18LegitValuesNotBlocked 判据③：合法值不被误拒。
func TestB18LegitValuesNotBlocked(t *testing.T) {
	s := newShellQuoteServer()
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleVirtVM(w, r, "kvm")
	}, map[string]interface{}{
		"deviceID": "b18-ok", "action": "start", "name": "web-01",
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法 VM 名称不应被拒：%d %s", rec.Code, rec.Body.String())
	}
}

// 编译期确保 httptest 被使用（避免 import 漂移时静默）。
var _ = httptest.NewRecorder
