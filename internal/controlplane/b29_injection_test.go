package controlplane

// b29_injection_test.go — 批次 29 守卫（db_ops.go 形态收口）。
//
// db_ops.go 8 个 builder：buildDBDeployScript（主构建器）+
// buildPostgreSQL/Oracle/TiDB/OceanBase/MSSQLDeployScript（子构建器）+
// buildDBActionScript / buildDBInstanceScript
// 里 req.Type/req.Mode/req.Action/req.Name 等直接作为脚本实参。
//
// 这些字段**在 handler 里都有白名单**：
//   - Type → allowedDBTypes（postgresql/oracle/tidb/oceanbase/mssql/teledb）
//   - Mode → allowedDBModes[type]（single/ha/rac/cluster）
//   - Action(启停) → allowedDBActions / Action(实例) → allowedDBInstanceActions
//   - Name → dbNameRe（实例/数据库名标识符）
//   - Version → handler 校验（部署分支）
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

// TestB29NoRawReqInScriptArgs 判据①。
func TestB29NoRawReqInScriptArgs(t *testing.T) {
	const f = "db_ops.go"
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

// TestB29WhitelistStillRejects 判据②：既有白名单不失效。
func TestB29WhitelistStillRejects(t *testing.T) {
	s := newShellQuoteServer()

	// 未识别的 db type 必须被拒（400）。
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleDBAction(w, r, "postgresql;id", "status")
	}, map[string]interface{}{
		"deviceID": "b29-db-type",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("type 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// 未识别的 action 必须被拒（400）。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleDBAction(w, r, "postgresql", "start;id")
	}, map[string]interface{}{
		"deviceID": "b29-db-act",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("action 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// 数据库实例名注入载荷必须被拒（400）。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleDBInstance(w, r, "postgresql")
	}, map[string]interface{}{
		"deviceID": "b29-db-inst", "action": "create", "name": "x;id",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("db 实例注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}
}

// TestB29LegitValuesNotBlocked 判据③：合法值不被误拒。
func TestB29LegitValuesNotBlocked(t *testing.T) {
	s := newShellQuoteServer()

	// 合法 instance 列表请求不应被拒。
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleDBInstance(w, r, "postgresql")
	}, map[string]interface{}{
		"deviceID": "b29-db-ok", "action": "list",
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法请求不应被拒：%d %s", rec.Code, rec.Body.String())
	}

	// 合法状态查询不应被拒。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleDBAction(w, r, "postgresql", "status")
	}, map[string]interface{}{
		"deviceID": "b29-db-ok2",
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法请求不应被拒：%d %s", rec.Code, rec.Body.String())
	}
}
