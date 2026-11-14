package controlplane

// b23_injection_test.go — 批次 23 守卫（olap_ops.go 形态收口：~23 处「已校验但直引」）。
//
// # 本批收口的是什么
//
// olap_ops.go 的 5 个 builder：buildOLAPDeployScript（Type/Mode/Version/DeviceID）、
// buildOLAPActionScript（Action/Name/DeviceID）、buildOLAPDatabaseScript（Database/Action/
// DeviceID）、buildOLAPTableScript（Table/Database/Action/DeviceID）、
// buildOLAPQueryScript（Query/Database/DeviceID）。
//
// 这些字段**在 handler 里都有校验**：
//   - `Type`/`Action` → `allowedOLAPTypes` / `allowedOLAPActions` / `allowedOLAPResourceActions`（枚举）；
//   - `Mode`          → `allowedOLAPModes[type][mode]`；
//   - `Name`/`Database`/`Table` → `olapNameRe`（`^[a-zA-Z0-9_]{1,64}$`，无任何分隔符/元字符）；
//   - `Query`         → `shellQuote` 承载（SQL 字符串正确转义）；
//   - `Schema`        → handler 内拒绝 NUL/换行；builder 内用 **shellQuote** 承载；
//   - `DeviceID`      → 不进下发脚本，仅进 `createSystemTask` 任务参数。
//
// 因此 ~23 处全是形态面，不是真洞。本批只做**局部变量承接** ⇒ 判据 E/F 归零；脚本文本逐字节不变。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"strings"
	"testing"
)

// TestB23NoRawReqInScriptArgs 判据①。
func TestB23NoRawReqInScriptArgs(t *testing.T) {
	const f = "olap_ops.go"
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
			if len(call.Args) < 3 || !b23IsBuilderTarget(call.Args[0]) {
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

// b23IsBuilderTarget 判断 Fprintf 的接收者是否是脚本 builder。
func b23IsBuilderTarget(e ast.Expr) bool {
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

// TestB23WhitelistStillRejects 判据②：既有白名单不失效。
func TestB23WhitelistStillRejects(t *testing.T) {
	s := newShellQuoteServer()

	// 未识别的 OLAP 类型必须被拒（400）：候选 Type 字典收敛到 allowedOLAPTypes。
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleOLAPDeploy(w, r)
	}, map[string]interface{}{
		"deviceID": "b23-olap-dep", "type": "rs; id", "version": "1.0", "mode": "single",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("type 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// Database 名必须匹配 olapNameRe：带分号载荷必须被拒（400）。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleOLAPDatabase(w, r, "clickhouse")
	}, map[string]interface{}{
		"deviceID": "b23-olap-db", "action": "create",
		"database": "x'; id", "cluster": "false",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("database 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// Table 名必须匹配 olapNameRe：带 $() 载荷必须被拒（400）。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleOLAPTable(w, r, "clickhouse")
	}, map[string]interface{}{
		"deviceID": "b23-olap-table", "action": "create",
		"database": "testdb", "table": "$(id)", "schema": "",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("table 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// Action 白名单：未识别动作必须被拒（400）。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleOLAPAction(w, r, "clickhouse", "deploy;id")
	}, map[string]interface{}{
		"deviceID": "b23-olap-act", "action": "list", "name": "ok",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("action 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}
}

// TestB23LegitValuesNotBlocked 判据③：合法值不被误拒。
func TestB23LegitValuesNotBlocked(t *testing.T) {
	s := newShellQuoteServer()
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleOLAPDeploy(w, r)
	}, map[string]interface{}{
		"deviceID": "b23-olap-ok", "type": "clickhouse", "version": "23.3", "mode": "single",
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法请求不应被拒：%d %s", rec.Code, rec.Body.String())
	}
}
