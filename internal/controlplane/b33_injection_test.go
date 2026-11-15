package controlplane

// b33_injection_test.go — 守卫（monitor_ops.go 型式收口·构造即校验）。
//
// monitor_ops.go 的构建函数改为**只接受 validated* 类型**
// (validateMonitorDeploy / validateMonitorAction / validateZabbixHost /
// validateZabbixItem / validateNightingaleQuery / validateSkyWalkingTrace)。
// 原始请求体 struct (monitorDeployRequest / monitorOpsRequest / zabbixHostReq /
// zabbixItemReq / nightingaleQueryReq / skywalkingTraceReq) 不再能到达 build* 函数。
// 与 db_ops.go / vector_ops.go / k8s_provision.go 同等「架构级」保障。
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

// TestB33BuilderParamsAreValidatedOnly 判据①：build* 入参只能是 validated* 或 *strings.Builder / string。
func TestB33BuilderParamsAreValidatedOnly(t *testing.T) {
	const f = "monitor_ops.go"
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
			ts := exprTypeStringMon(field.Type)
			if ts == "*strings.Builder" || ts == "string" {
				continue
			}
			for _, raw := range []string{"monitorDeployRequest", "monitorOpsRequest", "zabbixHostReq", "zabbixItemReq", "nightingaleQueryReq", "skywalkingTraceReq"} {
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

// exprTypeStringMon 取表达式字符串形式（只处理标识符与指针/selector 表达式）。
func exprTypeStringMon(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.StarExpr:
		return "*" + exprTypeStringMon(x.X)
	case *ast.SelectorExpr:
		return exprTypeStringMon(x.X) + "." + x.Sel.Name
	default:
		return ""
	}
}

// TestB33WhitelistStillRejects 判据②：既有白名单不失效。
func TestB33WhitelistStillRejects(t *testing.T) {
	s := newShellQuoteServer()

	// 未识别的 monType 必须被拒（400）。
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleMonitorAction(w, r, "zabbix;id", "start")
	}, map[string]interface{}{
		"deviceID": "b33-mon-type",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("monType 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// 未识别的 action 必须被拒（400）。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleMonitorAction(w, r, "zabbix", "start;id")
	}, map[string]interface{}{
		"deviceID": "b33-mon-act",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("action 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}
}

// TestB33LegitValuesNotBlocked 判据③：合法值不被误拒。
func TestB33LegitValuesNotBlocked(t *testing.T) {
	s := newShellQuoteServer()

	// 合法 action（status）不应被拒。
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleMonitorAction(w, r, "zabbix", "status")
	}, map[string]interface{}{
		"deviceID": "b33-mon-ok",
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法请求不应被拒：%d %s", rec.Code, rec.Body.String())
	}
}
