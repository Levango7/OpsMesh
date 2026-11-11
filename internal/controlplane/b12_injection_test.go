package controlplane

// b12_injection_test.go — 批次 12（OLAP/备份/硬件/向量/编译/安全/Oracle/OS 调优 8 文件）
// 「req.* 未转义直插」收口的注入守卫。
//
// 覆盖范围：internal/controlplane 下 8 个文件（并集 ~121 findings 的重头部分）：
//
//	olap_ext_ops.go 20 / script_compile_ops.go 20 / security_ext_ops.go 18 /
//	oracle_ops.go 14 / os_tuning_ops.go 14 / db_backup_ops.go 12 /
//	hardware_ops.go 11 / vector_ext_ops.go 11
//
// # 为什么这些 handler 是同一类缺陷面
//
// 全部把用户可控字段裸拼进要下发到 agent 的 shell 脚本（`fmt.Sprintf` 或 `+` 拼接）；
// agent 以 `sh -c` 执行 ⇒ `$(id)` / 反引号 / 分号被真实执行。鉴权面无缺口
// （dispatchKafkaTask → subsystemAuthorize 的 task:write 闸）⇒ 已授权调用者可直接打到 agent 的 RCE。
//
// # 本批的收口手法（与批次 9–11 同源）
//
//   - 资源名/标识符：白名单（opsDBName / opsOracleName / opsModule / opsSched …）
//   - 主机名：opsHost 白名单（字母数字开头 + . _ : -；`:` 放行 IPv6）
//   - 路径：mqPath 绝对路径白名单（含 shellquote.ValidatePath）
//   - 密码/token：shellquote.Quote 整体 Quote
//   - 自由 SQL：Go 层拼 statement，整体 Quote（SQL 层是任意方言——本就是管理面能力的一部分）
//   - 编译/执行端点（script_compile）：content 走 heredoc，先 opsRejectHeredocDelim 拒收
//     "一行恰好等于 SCRIPT_EOF" 的内容（提前闭合 ⇒ 后续内容被当命令执行）
//   - 类型位置（--version / --helper 之类命令名）：枚举白名单（vectorType 等）
//
// # 载荷形态
//
//   - 白名单类字段：载荷会被**拒收**（400）⇒ 判据是「必须 400 且不下发任何脚本」；
//   - 自由值字段：以语法合法的载荷（$(touch marker)）打进受理路径，语义判据用真 sh 验证。

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// b12Files 是本批守卫覆盖的源文件（相对本包目录）。
var b12Files = []string{
	"olap_ext_ops.go",
	"script_compile_ops.go",
	"security_ext_ops.go",
	"oracle_ops.go",
	"os_tuning_ops.go",
	"db_backup_ops.go",
	"hardware_ops.go",
	"vector_ext_ops.go",
}

// b12SafeHelpers 是**批次 12** 认可的「收口助手」集合：返回值已是可安全进 shell 的字面量。
var b12SafeHelpers = map[string]bool{
	"shellquote.Quote":       true,
	"shellquote.QuoteDouble": true,
	"shellquote.QuoteJoin":   true,

	// ops_script.go（域无关，批次 10）
	"opsURL":            true,
	"opsQuoteJSONText":  true,
	"opsJSONPayload":    true,
	"opsEcho":           true,
	"opsPositiveInt":    true,
	"opsNonNegativeInt": true,

	// ops_sql.go（SQL 面通用，批次 12）
	"opsHost":               true,
	"opsHostAllowEmpty":     true,
	"opsDBName":             true,
	"opsDBNameAllowEmpty":   true,
	"opsSQLString":          true,
	"opsRejectHeredocDelim": true,
	"opsName":               true,
	"opsProfile":            true,
	"opsOracleName":         true,
	"opsSQLID":              true,
	"opsFileName":           true,
	"opsTableList":          true,
	"opsPatchID":            true,
	"opsSysctlParam":        true,
	"opsSysctlValue":        true,
	"opsModule":             true,
	"opsDeviceName":         true,
	"opsSched":              true,
	"opsDigits":             true,
	"opsDomain":             true,
	"opsHTTPURL":            true,
	"opsArgList":            true,
	"opsHeredocContent":     true,
	"getInterpreter":        true,
	"getScriptExtension":    true,

	// mq_script.go（MQ 面，批次 11，复用）
	"mqName":                  true,
	"mqNameAllowEmpty":        true,
	"mqVersion":               true,
	"mqNode":                  true,
	"mqNodeList":              true,
	"mqCSVList":               true,
	"mqRocketTopic":           true,
	"mqRocketTopicAllowEmpty": true,
	"mqPath":                  true,

	// vector_ext_ops.go（向量库类型枚举，批次 12）
	"vectorType": true,
}

// b12ValidatingHelpers 是「**校验型**」助手（返回 (值, error)）。使用方必须检查 error。
var b12ValidatingHelpers = map[string]bool{
	"opsHost":             true,
	"opsHostAllowEmpty":   true,
	"opsDBName":           true,
	"opsDBNameAllowEmpty": true,
	"opsName":             true,
	"opsProfile":          true,
	"opsOracleName":       true,
	"opsSQLID":            true,
	"opsFileName":         true,
	"opsTableList":        true,
	"opsPatchID":          true,
	"opsSysctlParam":      true,
	"opsSysctlValue":      true,
	"opsModule":           true,
	"opsDeviceName":       true,
	"opsSched":            true,
	"opsDigits":           true,
	"opsDomain":           true,
	"opsHTTPURL":          true,
	"opsArgList":          true,
	"opsHeredocContent":   true,
	"mqName":              true,
	"mqVersion":           true,
	"mqPath":              true,
	"mqNode":              true,
	"mqNodeList":          true,
	"mqCSVList":           true,
	"mqRocketTopic":       true,
}

// TestB12NoRawReqInterpolation 判据①（AST 级污点传播，**权威口径**）：
// 八个文件里不得再有未经收口的用户字段直插脚本。
func TestB12NoRawReqInterpolation(t *testing.T) {
	for _, f := range b12Files {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, f, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("解析 %s 失败：%v", f, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			st := newB9TaintState(b12SafeHelpers, true)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.DeclStmt:
					if gd, ok := x.Decl.(*ast.GenDecl); ok {
						st.collectReqStruct(gd)
					}
				case *ast.GenDecl:
					st.collectReqStruct(x)
				case *ast.AssignStmt:
					st.checkAssign(fset, f, x, t)
				}
				return true
			})
		}
	}
}

// TestB12ValidatingHelperErrorChecked 判据②：校验型助手的 error 必须被检查。
func TestB12ValidatingHelperErrorChecked(t *testing.T) {
	for _, f := range b12Files {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, f, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("解析 %s 失败：%v", f, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			b10CheckErrorsChecked(fset, f, fn, b12ValidatingHelpers, t)
		}
	}
}

// TestB12InjectionQuoted 判据③（值位置）：受理路径里，载荷只能以字面量形态出现在脚本里。
func TestB12InjectionQuoted(t *testing.T) {
	s := newShellQuoteServer()
	payload := `$(id)`

	// db_backup：密码自由值（shellquote.Quote 整体 Quote）+ host 白名单拒收
	rec := sqPost(t, s, s.handleDBBackup, map[string]interface{}{
		"deviceID": "b12-db-backup", "type": "mysql", "action": "backup",
		"host": "db1.internal", "port": 3306, "user": "backup", "password": payload,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("db_backup 应受理：%d %s", rec.Code, rec.Body.String())
	}
	b11AssertNoInjection(t, "db_backup/password", sqDispatchScript(t, s, "b12-db-backup"), payload)

	// olap_ext：Trino query 自由值（整体 Quote）
	rec = sqPost(t, s, s.handleTrino, map[string]interface{}{
		"deviceID": "b12-trino", "action": "query", "query": "SELECT " + payload,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("trino 应受理：%d %s", rec.Code, rec.Body.String())
	}
	b11AssertNoInjection(t, "olap/trino-query", sqDispatchScript(t, s, "b12-trino"), payload)

	// security_ext：sqlmap 目标（http(s) 白名单 + 整体 Quote）
	rec = sqPost(t, s, s.handleInjectionDetect, map[string]interface{}{
		"deviceID": "b12-sec", "action": "scan-sqli", "target": "https://example.com/" + payload,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("injection 应受理：%d %s", rec.Code, rec.Body.String())
	}
	b11AssertNoInjection(t, "security/scan-sqli", sqDispatchScript(t, s, "b12-sec"), payload)

	// os_tuning：sysctl 值白名单拒收不合法字符
	rec = sqPost(t, s, s.handleKernelTuning, map[string]interface{}{
		"deviceID": "b12-sysctl", "action": "set", "param": "vm.swappiness", "value": payload,
	})
	if rec.Code == http.StatusOK {
		t.Fatalf("sysctl 应拒收非法 value：%d", rec.Code)
	}

	// vector_ext：类型枚举拒收
	rec = sqPost(t, s, s.handleVectorUpgrade, map[string]interface{}{
		"deviceID": "b12-vec", "action": "version", "type": "qdrant" + payload,
	})
	if rec.Code == http.StatusOK {
		t.Fatalf("vector 应拒收非法 type：%d", rec.Code)
	}
}

// TestB12RejectedByWhitelist 判据④：白名单字段载荷必须 400 且不下发任何脚本。
func TestB12RejectedByWhitelist(t *testing.T) {
	s := newShellQuoteServer()
	plan := []struct {
		name    string
		handler http.HandlerFunc
		body    map[string]interface{}
		field   string
		payload string
	}{
		{"db_backup/host", s.handleDBBackup, map[string]interface{}{"type": "mysql", "action": "backup", "deviceID": "b12-rej-1", "user": "u", "password": "p"}, "host", "db1;touch pwn"},
		{"db_backup/db", s.handleDBBackup, map[string]interface{}{"type": "mysql", "action": "restore", "deviceID": "b12-rej-2", "user": "u", "password": "p", "host": "db1"}, "db", "x'; touch pwn; --"},
		{"db_backup/path", s.handleDBBackup, map[string]interface{}{"type": "mysql", "action": "list", "deviceID": "b12-rej-3"}, "path", "rel;touch pwn"},
		{"olap_ext/target", s.handleClickHouse, map[string]interface{}{"action": "materialized-view", "deviceID": "b12-rej-4"}, "target", "v;touch pwn"},
		{"os_tuning/module", s.handleOSKernelModule, map[string]interface{}{"action": "load", "deviceID": "b12-rej-5"}, "module", "modprobe;touch pwn"},
		{"os_tuning/param", s.handleKernelTuning, map[string]interface{}{"action": "get", "deviceID": "b12-rej-6"}, "param", "vm.foo$(id)"},
		{"oracle/node", s.handleOracleRAC, map[string]interface{}{"action": "node-add", "deviceID": "b12-rej-7"}, "node", "n1;touch pwn"},
		{"security/target", s.handleInjectionDetect, map[string]interface{}{"action": "scan-sqli", "deviceID": "b12-rej-8"}, "target", "file:///etc/passwd"},
	}
	for _, c := range plan {
		body := map[string]interface{}{}
		for k, v := range c.body {
			body[k] = v
		}
		body[c.field] = c.payload
		rec := sqPost(t, s, c.handler, body)
		if rec.Code == http.StatusAccepted {
			t.Errorf("%s：应拒收：%d", c.name, rec.Code)
		}
		dev := c.body["deviceID"].(string)
		for _, tk := range s.store.GetTasks(dev) {
			if tk != nil && tk.Command != "" {
				t.Errorf("%s：载荷被拒收但脚本仍被下发：\n%s", c.name, tk.Command)
			}
		}
	}
}

// TestB12SemanticNoSideEffect 语义判据：脚本交真 sh 执行，注入载荷不得落地。
func TestB12SemanticNoSideEffect(t *testing.T) {
	sh := b9Shell()
	if sh == "" {
		t.Skip("本机无 POSIX shell（Windows），语义判据由 CI 的 ubuntu runner 承担")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "pwned")
	if strings.ContainsAny(marker, " \t") {
		t.Skip("临时目录路径含空白，跳过语义判据")
	}
	payload := "$(touch " + filepath.ToSlash(marker) + ")"

	s := newShellQuoteServer()
	plan := []struct {
		name    string
		handler http.HandlerFunc
		body    map[string]interface{}
		field   string
	}{
		{"db_backup/password", s.handleDBBackup, map[string]interface{}{"type": "mysql", "action": "backup", "host": "db1", "port": 3306, "user": "u"}, "password"},
		{"olap/trino", s.handleTrino, map[string]interface{}{"action": "query"}, "query"},
		{"security/scan-sqli", s.handleInjectionDetect, map[string]interface{}{"action": "scan-sqli", "target": "https://example.com/" + payload}, "markerField"},
		{"hardware/cabinet", s.handleCabinet, map[string]interface{}{"action": "devices", "operation": "add", "device": "mkfs" + payload, "position": "U12"}, "device"},
	}
	for _, c := range plan {
		dev := "b12-sem-" + c.name
		body := map[string]interface{}{"deviceID": dev}
		for k, v := range c.body {
			body[k] = v
		}
		body[c.field] = payload
		rec := sqPost(t, s, c.handler, body)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("%s：应受理：%d %s", c.name, rec.Code, rec.Body.String())
		}
		sc := sqDispatchScript(t, s, dev)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_ = exec.CommandContext(ctx, sh, "-c", sc).Run() // 退出码无关紧要
		cancel()
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("注入载荷真实落地：%s", marker)
	}
}

// TestB12WhitelistHelpers 白名单口径单点用例。
func TestB12WhitelistHelpers(t *testing.T) {
	// opsHost
	for _, ok := range []string{"db1", "db-1.internal", "10.0.0.1", "fe80::1"} {
		if _, err := opsHost("host", ok); err != nil {
			t.Errorf("opsHost(%q) 应通过：%v", ok, err)
		}
	}
	for _, bad := range []string{"", "db 1", "db/1", "-h", "db;rm -rf /", "$(id)"} {
		if _, err := opsHost("host", bad); err == nil {
			t.Errorf("opsHost(%q) 应拒收", bad)
		}
	}

	// opsDBName
	for _, ok := range []string{"default", "db_1", "db.schema", "tbl_name"} {
		if _, err := opsDBName("db", ok); err != nil {
			t.Errorf("opsDBName(%q) 应通过：%v", ok, err)
		}
	}
	for _, bad := range []string{"", "db;", "db x", "`id`", "$(id)", "'"} {
		if _, err := opsDBName("db", bad); err == nil {
			t.Errorf("opsDBName(%q) 应拒收", bad)
		}
	}

	// opsSQLString 双写单引号
	if got := opsSQLString(`a'b`); got != `a''b` {
		t.Errorf("opsSQLString(a'b) = %q，want a''b", got)
	}
	if got := opsSQLString("plain"); got != "plain" {
		t.Errorf("opsSQLString(plain) = %q，want plain", got)
	}

	// opsRejectHeredocDelim 拒收含 SCRIPT_EOF 整行的内容
	if err := opsRejectHeredocDelim("ok\nSCRIPT_EOF\nmore", "SCRIPT_EOF"); err == nil {
		t.Error("应拒收含单独一行 SCRIPT_EOF 的内容")
	}
	if err := opsRejectHeredocDelim("ok\nno delimiter\n", "SCRIPT_EOF"); err != nil {
		t.Errorf("不该拒收：%v", err)
	}

	// opsHTTPURL 只放行 http/https
	for _, ok := range []string{"http://a.com/", "https://a.com/x?a=b"} {
		if _, err := opsHTTPURL("target", ok); err != nil {
			t.Errorf("opsHTTPURL(%q) 应通过：%v", ok, err)
		}
	}
	for _, bad := range []string{"file:///etc/passwd", "javascript:alert(1)", "ftp://a/"} {
		if _, err := opsHTTPURL("target", bad); err == nil {
			t.Errorf("opsHTTPURL(%q) 应拒收", bad)
		}
	}

	// opsOracleName
	for _, ok := range []string{"DATA", "TBS_1", "ORCL"} {
		if _, err := opsOracleName("x", ok); err != nil {
			t.Errorf("opsOracleName(%q) 应通过：%v", ok, err)
		}
	}
	for _, bad := range []string{"1DB", "db;x", ""} {
		if _, err := opsOracleName("x", bad); err == nil {
			t.Errorf("opsOracleName(%q) 应拒收", bad)
		}
	}
}
