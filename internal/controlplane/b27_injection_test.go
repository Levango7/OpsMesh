package controlplane

// b27_injection_test.go — 批次 27 守卫（tsdb_ops.go 形态收口）。
//
// tsdb_ops.go 5 个 builder：buildTSDBDeployScript / buildTSDBActionScript /
// buildTSDBDatabaseScript / buildTSDBWriteScript / buildTSDBQueryScript
// 里 req.Type/req.Action/req.Name/req.Database/req.Query/req.Line 直接作为脚本实参。
//
// 这些字段**在 handler 里都有白名单**：
//   - Type → allowedTSDBTypes（influxdb/tdengine/victoriametrics）
//   - Action → allowedTSDBActions / allowed{create,delete,list}
//   - Name/Database → tsdbDBRe（数据库名标识符）
//   - Query → handler 非空校验 + 进 shellQuote（转义后拼入脚本）
//   - Line → handler 非空校验 + shellQuote（转义后拼入脚本）
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

// TestB27NoRawReqInScriptArgs 判据①。
func TestB27NoRawReqInScriptArgs(t *testing.T) {
	const f = "tsdb_ops.go"
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

// TestB27WhitelistStillRejects 判据②：既有白名单不失效。
func TestB27WhitelistStillRejects(t *testing.T) {
	s := newShellQuoteServer()

	// 未识别的 TSDB type 必须被拒（400）。
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleTSDBAction(w, r, "influxdb;id", "status")
	}, map[string]interface{}{
		"deviceID": "b27-tsdb-type",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("type 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// 未识别的 action 必须被拒（400）。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleTSDBAction(w, r, "influxdb", "start;id")
	}, map[string]interface{}{
		"deviceID": "b27-tsdb-act",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("action 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// 数据库名注入载荷必须被拒（400）。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleTSDBDatabase(w, r, "influxdb")
	}, map[string]interface{}{
		"deviceID": "b27-tsdb-db", "action": "create", "name": "x;id",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("database 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// 查询/写入缺关键字段必须被拒（400）。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleTSDBQuery(w, r, "influxdb")
	}, map[string]interface{}{
		"deviceID": "b27-tsdb-q",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 query 应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleTSDBWrite(w, r, "influxdb")
	}, map[string]interface{}{
		"deviceID": "b27-tsdb-w", "database": "db1",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 line 应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}
}

// TestB27LegitValuesNotBlocked 判据③：合法值不被误拒。
func TestB27LegitValuesNotBlocked(t *testing.T) {
	s := newShellQuoteServer()

	// 合法 database 查询不应被拒。
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleTSDBDatabase(w, r, "influxdb")
	}, map[string]interface{}{
		"deviceID": "b27-tsdb-ok", "action": "list",
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法请求不应被拒：%d %s", rec.Code, rec.Body.String())
	}

	// 合法状态查询不应被拒。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleTSDBAction(w, r, "influxdb", "status")
	}, map[string]interface{}{
		"deviceID": "b27-tsdb-ok2",
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法请求不应被拒：%d %s", rec.Code, rec.Body.String())
	}
}
