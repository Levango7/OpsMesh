package controlplane

// b28_injection_test.go — 批次 28 守卫（graph_ops.go 形态收口）。
//
// graph_ops.go 5 个 builder：buildGraphDeployScript / buildGraphActionScript /
// buildGraphDatabaseScript / buildGraphQueryScript / buildGraphBackupScript
// 里 req.Type/req.Action/req.Name/req.Database/req.Query 直接作为脚本实参。
//
// 这些字段**在 handler 里都有白名单**：
//   - Type → allowedGraphTypes（neo4j/nebulagraph）
//   - Action → allowedGraphActions / allowedGraphDatabaseActions
//   - Name/Database → graphNameRe（图空间/数据库标识符）
//   - Query → handler 非空校验 + 脚本内经 shellQuote 转义后拼入
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

// TestB28NoRawReqInScriptArgs 判据①。
func TestB28NoRawReqInScriptArgs(t *testing.T) {
	const f = "graph_ops.go"
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

// TestB28WhitelistStillRejects 判据②：既有白名单不失效。
func TestB28WhitelistStillRejects(t *testing.T) {
	s := newShellQuoteServer()

	// 未识别的图库 type 必须被拒（400）。
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleGraphAction(w, r, "neo4j;id", "status")
	}, map[string]interface{}{
		"deviceID": "b28-graph-type",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("type 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// 未识别的 action 必须被拒（400）。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleGraphAction(w, r, "neo4j", "start;id")
	}, map[string]interface{}{
		"deviceID": "b28-graph-act",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("action 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// 图空间/数据库名注入载荷必须被拒（400）。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleGraphDatabase(w, r, "neo4j")
	}, map[string]interface{}{
		"deviceID": "b28-graph-db", "action": "create", "name": "x;id",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("database 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// 缺 query 必须被拒（400）。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleGraphQuery(w, r, "neo4j")
	}, map[string]interface{}{
		"deviceID": "b28-graph-q",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 query 应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}
}

// TestB28LegitValuesNotBlocked 判据③：合法值不被误拒。
func TestB28LegitValuesNotBlocked(t *testing.T) {
	s := newShellQuoteServer()

	// 合法 database 列表请求不应被拒。
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleGraphDatabase(w, r, "neo4j")
	}, map[string]interface{}{
		"deviceID": "b28-graph-ok", "action": "list",
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法请求不应被拒：%d %s", rec.Code, rec.Body.String())
	}

	// 合法状态查询不应被拒。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleGraphAction(w, r, "neo4j", "status")
	}, map[string]interface{}{
		"deviceID": "b28-graph-ok2",
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法请求不应被拒：%d %s", rec.Code, rec.Body.String())
	}
}
