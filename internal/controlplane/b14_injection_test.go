package controlplane

// b14_injection_test.go — 批次 14（脚本 builder 实参级收口）的注入守卫。
//
// # 为什么需要这一批
//
// 批次 9–13 的注入面盘点（scripts/shell-injection-audit.py）有**两个结构性盲区**，
// 本批把守卫补上：
//
//  1. **逐行判定**：同一行只要出现一个被 Quote 过的参数，整行即被跳过 ⇒ 同行其余
//     **裸**实参（req.* 直插）逃逸出盘点。
//  2. **只认 fmt.Sprintf(**：以 `fmt.Fprintf(&builder, ...)` 拼脚本的**整类**从未被计入。
//
// 本守卫因此**按实参粒度**判定：fmt.Sprintf / fmt.Fprintf 的**字符串槽**实参里，
// 不得再裸引用 req.<字段>；要么整体 Quote/转义，要么用「已校验的局部变量」承接。
//
// # 本批覆盖的文件（8 个）
//
//	env_ops.go / zookeeper_middleware_ops.go / ntp_ops.go / security_ops.go /
//	redis_middleware_ops.go / rabbitmq_middleware_ops.go / kafka_middleware_ops.go /
//	monitor_ops.go
//
// # 语义判据
//
//	白名单字段载荷（IPVS 的 VIP、redis sentinel 的 host/master、zookeeper 的 host、
//	rabbitmq 的 vhost、kafka 的 group）必须 **400 且不下发任何脚本**。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"strings"
	"testing"
)

// b14Files 是本批守卫覆盖的源文件（相对本包目录）。
var b14Files = []string{
	"env_ops.go",
	"zookeeper_middleware_ops.go",
	"ntp_ops.go",
	"security_ops.go",
	"redis_middleware_ops.go",
	"rabbitmq_middleware_ops.go",
	"kafka_middleware_ops.go",
	"monitor_ops.go",
}

// b14SafeHelpers 是本批认可的「收口助手」：返回值已是可安全进 shell 的字面量，
// 或其参数本身已被白名单校验。
var b14SafeHelpers = []string{
	"shellquote.Quote(",
	"shellquote.QuoteDouble(",
	"shellquote.QuoteJoin(",
	"shellquote.QuotePowerShellSingle(",
	"shellquote.PowerShellEncodedCommand(",
	"shellQuote(",
	"opsEcho(",
	"opsSQLString(",
	"opsHeredocContent(",
	"opsQuoteJSONText(",
	"esEcho(",
	"esAPIURL(",
	"opsURL(",
	"strconv.Quote(",
	"strconv.Itoa(",
	"strconv.FormatBool(",
	"urlQueryEscape(",
	"regexp.QuoteMeta(",
	"defaultIfEmpty(",
	"nonEmptyPayload(",
	"firstNonEmpty(",
}

// b14BuilderTargets 是被认作「脚本 builder」的接收者形态（变量名集合）。
var b14BuilderTargets = map[string]bool{
	"b":       true,
	"buf":     true,
	"sb":      true,
	"builder": true,
	"script":  true,
}

// TestB14NoRawReqInScriptBuilders 判据①：脚本拼装的**字符串槽**实参不得裸引用 req.*。
//
// 为什么按实参判定（而不是按行）：同一行里既有 Quote 过的兄弟参数、又有裸参数时，
// 逐行判定会整行放过——这正是批次 12/13 之后仍能挖出真洞的原因。
//
// 为什么带类型感知：只有**字符串**字段能携带 shell 元字符（int 字段在 JSON 解码期
// 就被类型挡掉，传字符串 ⇒ 400）⇒ int 字段不是注入面，不该判红。
func TestB14NoRawReqInScriptBuilders(t *testing.T) {
	for _, f := range b14Files {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, f, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("解析 %s 失败：%v", f, err)
		}
		types := b14FieldTypes(file)
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := b9CallName(call.Fun)
			var args []ast.Expr
			switch name {
			case "fmt.Sprintf":
				if len(call.Args) < 2 {
					return true
				}
				args = call.Args[1:]
			case "fmt.Fprintf":
				// 目标必须是脚本 builder（&x / x），否则不是 shell 拼装位。
				if len(call.Args) < 3 {
					return true
				}
				if !b14IsBuilderTarget(call.Args[0]) {
					return true
				}
				args = call.Args[2:]
			default:
				return true
			}
			for _, a := range args {
				if !b14ArgTainted(a, types) {
					continue
				}
				src := b9Src(fset, a)
				if b14HasSafeHelper(src) {
					continue
				}
				t.Errorf("%s:%d 脚本实参裸引用用户字段：%s",
					f, fset.Position(a.Pos()).Line, src)
			}
			return true
		})
	}
}

// b14FieldTypes 收集文件内所有结构体字段的类型（字段名 → 类型名）。
//
// 保守规则：同名字段只要在**任一**结构体里是 string，就按 string 处理（宁可多报）。
func b14FieldTypes(file *ast.File) map[string]string {
	types := map[string]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		st, ok := n.(*ast.StructType)
		if !ok {
			return true
		}
		for _, f := range st.Fields.List {
			typ := ""
			if id, ok := f.Type.(*ast.Ident); ok {
				typ = id.Name
			}
			for _, name := range f.Names {
				if typ == "string" || types[name.Name] == "" {
					types[name.Name] = typ
				}
			}
		}
		return true
	})
	return types
}

// b14ArgTainted 判断实参表达式是否**携带未净化的字符串型用户字段**。
func b14ArgTainted(e ast.Expr, types map[string]string) bool {
	switch x := e.(type) {
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok && id.Name == "req" {
			return types[x.Sel.Name] == "string"
		}
		return b14ArgTainted(x.X, types)
	case *ast.CallExpr:
		for _, a := range x.Args {
			if b14ArgTainted(a, types) {
				return true
			}
		}
		return false
	case *ast.BinaryExpr:
		return b14ArgTainted(x.X, types) || b14ArgTainted(x.Y, types)
	case *ast.ParenExpr:
		return b14ArgTainted(x.X, types)
	}
	return false
}

// b14IsBuilderTarget 判断 Fprintf 的接收者是否是脚本 builder。
func b14IsBuilderTarget(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			if id, ok := x.X.(*ast.Ident); ok {
				return b14BuilderTargets[id.Name]
			}
		}
	case *ast.Ident:
		return b14BuilderTargets[x.Name]
	}
	return false
}

// b14HasSafeHelper 判断实参文本里是否含被认可的收口助手。
func b14HasSafeHelper(src string) bool {
	for _, h := range b14SafeHelpers {
		if strings.Contains(src, h) {
			return true
		}
	}
	return false
}

// TestB14WhitelistRejects 判据②：白名单字段的注入载荷必须 400，且不下发任何脚本。
func TestB14WhitelistRejects(t *testing.T) {
	s := newShellQuoteServer()
	payload := "$(touch marker)"

	// IPVS：VIP 必须是 IP 字面量
	rec := sqPost(t, s, s.handleIPVS, map[string]interface{}{
		"deviceID": "b14-ipvs", "action": "add", "vip": payload, "port": 80, "protocol": "tcp",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("IPVS VIP 注入载荷应被拒：%d %s", rec.Code, rec.Body.String())
	}

	// redis sentinel：host 走主机白名单
	rec = sqPost(t, s, s.handleMiddlewareRedisSentinel, map[string]interface{}{
		"deviceID": "b14-redis", "action": "monitor", "host": payload, "master": "mymaster",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("redis sentinel host 注入载荷应被拒：%d %s", rec.Code, rec.Body.String())
	}

	// zookeeper：host 走主机白名单
	rec = sqPost(t, s, s.handleZookeeperCmd, map[string]interface{}{
		"deviceID": "b14-zk", "cmd": "stat", "host": payload,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("zookeeper host 注入载荷应被拒：%d %s", rec.Code, rec.Body.String())
	}

	// rabbitmq：vhost 走标识符白名单
	rec = sqPost(t, s, s.handleRabbitMQVhost, map[string]interface{}{
		"deviceID": "b14-rmq", "action": "list", "vhost": payload,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("rabbitmq vhost 注入载荷应被拒：%d %s", rec.Code, rec.Body.String())
	}

	// kafka consumer group：group 走标识符白名单
	rec = sqPost(t, s, s.handleKafkaConsumerGroup, map[string]interface{}{
		"deviceID": "b14-kafka", "action": "describe", "group": payload,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("kafka group 注入载荷应被拒：%d %s", rec.Code, rec.Body.String())
	}
}

// TestB14AuditScriptJudgements 判据③：盘点器必须包含判据 E / F（实参级 + Fprintf 级）。
//
// 为什么用「脚本内容断言」而不是「跑脚本」：测试环境不保证有 python3；这里断言的是
// **判据已接入**这一事实（新增判据被误删会让整个批次的口径失效）。
func TestB14AuditScriptJudgements(t *testing.T) {
	raw, err := os.ReadFile("../../scripts/shell-injection-audit.py")
	if err != nil {
		t.Skipf("读不到盘点器脚本（跳过）：%v", err)
	}
	s := string(raw)
	for _, marker := range []string{"raw_arg_findings", "raw_fprintf_findings", "RAW_ARG_SAFE_WRAPPERS"} {
		if !strings.Contains(s, marker) {
			t.Errorf("盘点器缺少判据 E/F 的关键实现：%s", marker)
		}
	}
}
