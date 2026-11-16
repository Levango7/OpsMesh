package controlplane

// b32_k8s_injection_test.go — 批次 32 守卫（k8s_provision.go 型式收口·构造即校验）。
//
// k8s_provision.go 的构建函数改为**只接受 validated* 类型**
// (validateK8sProvision / validateK8sLifecycle / validateK8sKubeconfig / validateK8sJoin)。
// 原始请求体 struct (k8sProvisionReq / k8sLifecycleReq / k8sJoinReq) 不再能到达 build* 函数。
// 与 db_ops.go / vector_ops.go 同等「架构级」保障：编译期断言，构造点之外无合法路径。
//
// 守卫三条：
//   ① 所有 build* 函数入参类型必须以 "validated" 开头（或 *strings.Builder / string）；
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

// TestB32BuilderParamsAreValidatedOnly 判据①：build* 入参只能是 validated* 或 *strings.Builder / string。
func TestB32BuilderParamsAreValidatedOnly(t *testing.T) {
	const f = "k8s_provision.go"
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
			ts := exprTypeStringK8S(field.Type)
			if ts == "*strings.Builder" || ts == "string" {
				continue
			}
			for _, raw := range []string{"k8sProvisionReq", "k8sLifecycleReq", "k8sJoinReq"} {
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

// exprTypeStringK8S 取表达式字符串形式（只处理标识符与指针/selector 表达式）。
func exprTypeStringK8S(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.StarExpr:
		return "*" + exprTypeStringK8S(x.X)
	case *ast.SelectorExpr:
		return exprTypeStringK8S(x.X) + "." + x.Sel.Name
	default:
		return ""
	}
}

// TestB32WhitelistStillRejects 判据②：既有白名单不失效。
func TestB32WhitelistStillRejects(t *testing.T) {
	s := newShellQuoteServer()

	// 未识别的 tool 必须被拒（400）。
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleK8sProvision(w, r)
	}, map[string]interface{}{
		"deviceID": "b32-k8s-tool",
		"tool":     "kubeadm;id",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("tool 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// action 注入载荷必须被拒（400）。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleK8sProvisionLifecycle(w, r)
	}, map[string]interface{}{
		"deviceID": "b32-k8s-act",
		"tool":     "k3s",
		"action":   "start;id",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("action 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// token 注入载荷必须被拒（400）。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleK8sJoin(w, r)
	}, map[string]interface{}{
		"deviceID":   "b32-k8s-join",
		"tool":       "kubeadm",
		"role":       "worker",
		"masterAddr": "master.example.com",
		"token":      "bad token;id",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("token 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}
}

// TestB32LegitValuesNotBlocked 判据③：合法值不被误拒。
func TestB32LegitValuesNotBlocked(t *testing.T) {
	s := newShellQuoteServer()

	// 合法 tool=k3s 不应被拒。
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleK8sProvision(w, r)
	}, map[string]interface{}{
		"deviceID": "b32-k8s-ok",
		"tool":     "k3s",
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法请求不应被拒：%d %s", rec.Code, rec.Body.String())
	}
}
