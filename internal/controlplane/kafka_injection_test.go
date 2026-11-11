package controlplane

// kafka_injection_test.go — 批次 10（Kafka/Graph/GPU 运维面「req.* 未转义直插」收口）的注入守卫。
//
// 覆盖范围：internal/controlplane 下 7 个文件、90 处脚本行：
//
//	kafka_ops.go 14 / kafka_ops_ext.go 8 / kafka_scale_ops.go 15 /
//	kafka_advanced_ops.go 7 / kafka_final_ops.go 5 /
//	graph_ext_ops.go 20 / gpu_ext_ops.go 18
//
// # 为什么是真缺陷面
//
// 这些 handler 把用户可控字段裸拼进要下发到 agent 的 shell 脚本，agent 以 `sh -c`
// 执行 ⇒ `$(id)` / 反引号 / 分号被**真实执行**。鉴权面无缺口（均经 dispatchKafkaTask
// → subsystemAuthorize 的 task:write 闸）⇒ 已授权调用者可直接打到 agent 的 RCE。
//
// **双引号不是防护**：POSIX 双引号内 `$` 与反引号照样展开（`echo "…$(id)…"` 一样被执行）。
//
// # 本批的五个易错点（守卫专门盯这五条）
//
//	① **命令名不是值**：Graph 的 `req.Type` 在 `%s --version` 里是要**当二进制执行**的
//	  命令名 ⇒ 这是**命令注入**（载荷可以指向任意二进制/脚本），Quote 挡不住 ⇒ 必须
//	  枚举白名单。
//	② **双层注入**：Cypher / nGQL 语句里既有图库层的字符串字面量、又有 shell 的双引号
//	  段 ⇒ 两层都得收：字段走**与方言无关的白名单**（不用 Cypher 层转义——Cypher 与
//	  nGQL 的 `\'` 规则不一致，转义不可证），整条语句走 shellquote.Quote。
//	③ **选项名不是值**：Kafka 的 `--operation %s` 与 `--%s` 的值会被 Kafka CLI 解析成
//	  **选项** ⇒ Quote 只保证 shell 层安全 ⇒ 必须枚举白名单。
//	④ **URL 必须整体 Quote**：旧形态 URL 在 `"http://…/%s"` 里，双引号内单引号是
//	  **字面量**——Quote 产出的 `'x'` 会变成 URL 里的 %27 ⇒ 请求必然 404。故正确做法
//	  是 Go 层拼完整 URL 再整体 Quote（守卫用真 shell 求值来验「逐字相同」）。
//	⑤ **heredoc 会提前闭合**：`<< 'EOF'` 的内容本应字面，但载荷里出现**一行恰好等于
//	  EOF** 就会让 heredoc 提前闭合 ⇒ 其后内容被 shell 当**命令**执行。
//
// # 载荷形态（沿用批次 7/9 的教训）
//
//   - 白名单类字段（Graph 标识符 / Kafka 资源名 / topic 列表 / GPU 节点名 …）：载荷
//     会被**拒收**（400）⇒ 判据是「必须 400 且不下发任何脚本」。
//   - 自由 JSON 字段（connector config / schema）：载荷必须是**合法 JSON**（否则会被
//     新的 json.Valid 校验拦下，测不到注入面）⇒ 用 `{"a":"…"}` 包裹。
//   - 语义判据用**语法合法**的载荷：命令替换 `$(touch marker)` 在旧形态里真的会执行；
//     带未闭合单引号的载荷会让整段脚本语法错误 ⇒ fail-closed，不构成可执行注入的证明。

import (
	"context"
	"fmt"
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

// b10Files 是本批守卫覆盖的源文件（相对本包目录）。
var b10Files = []string{
	"kafka_ops.go",
	"kafka_ops_ext.go",
	"kafka_scale_ops.go",
	"kafka_advanced_ops.go",
	"kafka_final_ops.go",
	"graph_ext_ops.go",
	"gpu_ext_ops.go",
}

// b10SafeHelpers 是**批次 10** 认可的「收口助手」集合：它们的返回值已是可安全进 shell
// 的字面量。
//
// 口径要与 `scripts/shell-injection-audit.py` 的 QUOTE_MARKERS 一致（盘点器那边是
// 文本判据，这里是 AST 级判据 ⇒ 本集合是**类型/语义感知**的权威口径）。
//
// # 为什么 cypherShell / nebulaConsole **不在**集合里
//
// 它们只把整条语句做 **shell 层** Quote，**挡不住图库层注入**（Cypher / nGQL 自己的
// 字符串字面量仍可被闭合）。把它们排除在集合之外，AST 守卫才能抓到「图库层注入」——
// 即使白名单助手被绕过、用户字段直接进了语句。
var b10SafeHelpers = map[string]bool{
	"shellquote.Quote":       true,
	"shellquote.QuoteDouble": true,
	"shellquote.QuoteJoin":   true,

	// ops_script.go（域无关）
	"opsURL":            true,
	"opsQuoteJSONText":  true,
	"opsJSONPayload":    true,
	"opsEcho":           true,
	"opsPositiveInt":    true,
	"opsNonNegativeInt": true,

	// graph_script.go（图库面）
	"graphCommandType": true,
	"graphIdent":       true,
	"graphPropDefs":    true,
	"graphSecret":      true,
	"graphPath":        true,

	// kafka_script.go（Kafka 面）
	"kafkaACLOperation":    true,
	"kafkaACLResourceFlag": true,
	"kafkaACLResourceName": true,
	"kafkaResource":        true,
	"kafkaTopic":           true,
	"kafkaTopicList":       true,
	"kafkaHeredoc":         true,
	"kafkaHeredocf":        true,

	// gpu_ext_ops.go（GPU 面）
	"gpuNodeArg": true,
}

// b10ValidatingHelpers 是「**校验型**」助手：它们返回 (值, error)，校验不过时返回
// **空串 + error**。
//
// 为什么要单独列：这类助手只有在使用方**检查了 error** 时才成立。若调用方把 error
// 丢掉（`x, _ := kafkaTopic(req.Topic)`），白名单就形同虚设——字段被静默清空。
// 故守卫另有一条断言（TestB10ValidatingHelperErrorChecked）盯这件事。
var b10ValidatingHelpers = map[string]bool{
	"graphCommandType":     true,
	"graphIdent":           true,
	"graphPropDefs":        true,
	"graphSecret":          true,
	"graphPath":            true,
	"kafkaACLOperation":    true,
	"kafkaACLResourceFlag": true,
	"kafkaACLResourceName": true,
	"kafkaResource":        true,
	"kafkaTopic":           true,
	"kafkaTopicList":       true,
	"kafkaHeredoc":         true,
	"kafkaHeredocf":        true,
	"gpuNodeArg":           true,
	"opsQuoteJSONText":     true,
	"opsJSONPayload":       true,
}

// b10MarkerName 是本批语义判据的副作用标记文件名（放在临时目录里）。
const b10MarkerName = "pwned"

// b10PayloadAny 用于「值位置」的字段（URL 段 / 回显 / Quote 的值 / CLI 参数值）。
var b10PayloadAny = []string{
	`$(id)`,
	"`id`",
	`x; touch /tmp/opsmesh-b10-pwned`,
	`x'; touch /tmp/opsmesh-b10-pwned; echo '`,
	`"; touch /tmp/opsmesh-b10-pwned; echo "`,
}

// b10PayloadJSON 用于自由 JSON 字段：载荷必须是**合法 JSON**（否则会被新的
// json.Valid 校验拦下 ⇒ 测不到注入面）。
var b10PayloadJSON = []string{
	`{"a":"$(id)"}`,
	`{"a":"x'; touch /tmp/opsmesh-b10-pwned; echo '"}`,
	`{"a":"` + "`id`" + `"}`,
	`{"a":"\"; touch /tmp/opsmesh-b10-pwned; echo \""}`,
}

// b10Payloads 按 kind 取载荷集。
func b10Payloads(kind string) []string {
	if kind == "json" {
		return b10PayloadJSON
	}
	return b10PayloadAny
}

// b10Case 覆盖一个 handler × 注入字段组合。
type b10Case struct {
	name    string
	handler http.HandlerFunc
	body    map[string]interface{}
	field   string
	expect  string // "accept"（受理：载荷只以字面量形态出现在脚本里）| "reject"（白名单拒收 ⇒ 400）
	kind    string // "any" | "json"
}

// TestB10NoRawReqInterpolation 判据①（AST 级污点传播，**权威口径**）：
// 七个文件里不得再有未经收口的用户字段直插脚本。
//
// 为什么用 **AST 级**判据而不是行级文本判据（批次 7 的做法）：
//   - 污点源：请求结构体里**字符串类型**的字段（`req.Foo`）——int 字段在 JSON 解码
//     阶段就被类型校验挡掉（传字符串 ⇒ 400），不可能携带 shell 元字符；
//   - 传播：局部变量赋值、字符串拼接、`fmt.Sprintf` 实参；
//   - 净化：经收口助手（Quote 族 / 白名单助手 / JSON 构造）返回的值即已安全；
//   - 断言①：`script = <任意表达式>` 的整体不得携带未净化的污点（**拼接形态**也
//     覆盖——批次 10 收口后大量是 `cypherShell(...) + " … " + opsEcho(...)`）；
//   - 断言②：`script = fmt.Sprintf(...)` 的格式串必须是字面量、每个实参都不得携带
//     未净化的污点。
//
// 非字符串字段（`gpu` / `throttle` / `limit` / `partition` / `offset`，int）不构成
// 注入面——但盘点器（判据 C/D）是**行级文本**判据、不区分字段类型 ⇒ 本守卫才是
// 「这些字段不是注入面」的权威证明。
func TestB10NoRawReqInterpolation(t *testing.T) {
	for _, f := range b10Files {
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
			st := newB9TaintState(b10SafeHelpers, true)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.DeclStmt:
					// 函数体内的 `var req struct{...}` 是 DeclStmt 包着 GenDecl。
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

// TestB10ValidatingHelperErrorChecked 判据②：白名单助手的 error 必须被检查。
//
// 为什么要有这条：白名单助手（如 kafkaTopic）在校验失败时返回 **空串 + error**。
// 若调用方把 error 丢掉（`x, _ := kafkaTopic(req.Topic)`），白名单就形同虚设——字段被
// 静默清空（功能错），且在「失败时原样返回」的改写下会直接变成注入面。
//
// 断言：每个校验型助手的返回值所绑定的 err 变量，必须在**其后**被某个 if 的条件检查。
func TestB10ValidatingHelperErrorChecked(t *testing.T) {
	for _, f := range b10Files {
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
			b10CheckErrorsChecked(fset, f, fn, b10ValidatingHelpers, t)
		}
	}
}

// b10Event 是「校验型调用」或「错误检查点」的源码位置事件。
type b10Event struct {
	off  int
	line int
	err  string
	kind string // "call" | "check"
}

// b10CheckErrorsChecked 断言：校验型助手的 err 变量在其后被检查。
//
// helpers：本批次认可的校验型助手集合（批次 10 传 b10ValidatingHelpers、批次 11 传
// b11ValidatingHelpers；判据本身只有一份）。
func b10CheckErrorsChecked(fset *token.FileSet, file string, fn *ast.FuncDecl, helpers map[string]bool, t *testing.T) {
	var events []b10Event
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			// `v, err := helper(...)` / `v, err = helper(...)`
			if len(x.Lhs) < 2 || len(x.Rhs) != 1 {
				return true
			}
			call, ok := x.Rhs[0].(*ast.CallExpr)
			if !ok {
				return true
			}
			if !helpers[b9CallName(call.Fun)] {
				return true
			}
			errID, ok := x.Lhs[1].(*ast.Ident)
			if !ok {
				return true
			}
			events = append(events, b10Event{
				off:  fset.Position(x.Pos()).Offset,
				line: fset.Position(x.Pos()).Line,
				err:  errID.Name,
				kind: "call",
			})
		case *ast.IfStmt:
			// 条件里出现 err 名 ⇒ 该 err 在此处被检查。
			// 用 **Cond 的位置**排序：if-init 形态（`if v, err := helper(...); err != nil`）
			// 里，init 的位置在 cond 之前 ⇒ 排序上「检查」在「调用」之后 ⇒ 判定正确。
			condOff := fset.Position(x.Cond.Pos()).Offset
			condLine := fset.Position(x.Cond.Pos()).Line
			ast.Inspect(x.Cond, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok {
					events = append(events, b10Event{off: condOff, line: condLine, err: id.Name, kind: "check"})
				}
				return true
			})
		}
		return true
	})

	for _, e := range events {
		if e.kind != "call" {
			continue
		}
		checked := false
		for _, c := range events {
			if c.kind == "check" && c.err == e.err && c.off > e.off {
				checked = true
				break
			}
		}
		if !checked {
			t.Errorf("%s:%d 校验型助手的 error（%s）未被检查（白名单失效 ⇒ 字段被静默清空）",
				file, e.line, e.err)
		}
	}
}

// b10AssertNoInjection 断言脚本里载荷只以字面量形态存在。
//
// 用批次 7 的 unquotedText（纯函数，取脚本中「单引号之外」的文本）：注入载荷若出现在
// 那里，就是可执行结构。
//
// 注意：shellquote.Quote 对含单引号的字符串产出 POSIX 标准的 `'\”` 关闭-转义-重开
// 序列 ⇒ unquotedText 的简易单引号扫描会把其中一段当成「引号外文本」⇒ 故这里**只**
// 断言「载荷整体不在引号外文本里」与「载荷不是双引号直插形态」，不做「载荷的任意
// 子串都不许出现」这种更粗的断言（否则会对 `'\”` 形态误报）。
func b10AssertNoInjection(t *testing.T, name, script string, payload string) {
	t.Helper()
	out := unquotedText(script)
	if payload != "" && strings.Contains(out, payload) {
		t.Errorf("%s：引号外文本含注入载荷 %q：\n脚本=%s\n引号外=%s", name, payload, script, out)
	}
	// 旧形态：用户字段直接落在**双引号段**内（双引号内 $ / 反引号仍会展开 ⇒ 不是防护）。
	//
	// 这里必须在 **unquotedText** 上查，不能在原始脚本上查——原始脚本里到处是 JSON 的
	// 双引号，载荷嵌在 JSON 字符串里时两侧恰好也是 `"`，在原始脚本上查会**误判**
	// （JSON 双引号 ≠ shell 双引号：整个 JSON 在 shell 单引号段内，本来就不可执行）。
	if strings.Contains(out, `"`+payload+`"`) {
		t.Errorf("%s：用户字段仍是双引号直插形态（双引号内 $ / 反引号会展开 ⇒ 可执行注入）：\n%s", name, script)
	}
}

// b10AcceptCases 是「值位置」字段：载荷**被受理**，但只能以字面量形态出现在脚本里。
//
// 这些字段的收口手段是「整体 Quote」（值）或「Go 层构造 JSON 后整体 Quote」。
func b10AcceptCases(s *Server) []b10Case {
	return []b10Case{
		// ---- kafka_ops.go ----
		{"kafka/schema-registry-subject", s.handleKafkaSchemaRegistry, map[string]interface{}{"action": "get-schema", "subject": "s1"}, "subject", "accept", "any"},
		{"kafka/connect-config", s.handleKafkaConnect, map[string]interface{}{"action": "create", "connector": "c1"}, "config", "accept", "json"},
		{"kafka/schema-registry-schema", s.handleKafkaSchemaRegistry, map[string]interface{}{"action": "register", "subject": "s1"}, "schema", "accept", "json"},

		// ---- kafka_ops_ext.go ----
		{"kafka-ext/delegation-principal", s.handleKafkaDelegationToken, map[string]interface{}{"action": "create"}, "principal", "accept", "any"},
		{"kafka-ext/delegation-tokenId", s.handleKafkaDelegationToken, map[string]interface{}{"action": "expire"}, "tokenId", "accept", "any"},
		{"kafka-ext/config-value", s.handleKafkaConfig, map[string]interface{}{"action": "alter", "entityType": "topics", "entityName": "t1"}, "config", "accept", "any"},

		// ---- kafka_scale_ops.go ----
		{"kafka-scale/remove-broker", s.handleKafkaScale, map[string]interface{}{"action": "remove-broker"}, "brokerId", "accept", "any"},
		{"kafka-scale/upgrade-version", s.handleKafkaUpgrade, map[string]interface{}{"action": "upgrade"}, "targetVersion", "accept", "any"},
		{"kafka-scale/log-clean-topic", s.handleKafkaLog, map[string]interface{}{"action": "clean"}, "topic", "accept", "any"},
		{"kafka-scale/log-compact-topic", s.handleKafkaLog, map[string]interface{}{"action": "compact"}, "topic", "accept", "any"},
		{"kafka-scale/log-retention-topic", s.handleKafkaLog, map[string]interface{}{"action": "retention"}, "topic", "accept", "any"},
		{"kafka-scale/transaction-txnId", s.handleKafkaTransaction, map[string]interface{}{"action": "describe"}, "txnId", "accept", "any"},
		{"kafka-scale/mm2-source", s.handleMirrorMakerFull, map[string]interface{}{"action": "create-link", "target": "t2"}, "source", "accept", "any"},
		{"kafka-scale/mm2-target", s.handleMirrorMakerFull, map[string]interface{}{"action": "create-link", "source": "s1"}, "target", "accept", "any"},
		{"kafka-scale/mm2-linkName", s.handleMirrorMakerFull, map[string]interface{}{"action": "create-link", "source": "s1", "target": "t2"}, "linkName", "accept", "any"},
		{"kafka-scale/streams-appId", s.handleKafkaStreams, map[string]interface{}{"action": "reset"}, "appId", "accept", "any"},
		{"kafka-scale/streams-inputTopic", s.handleKafkaStreams, map[string]interface{}{"action": "reset", "appId": "a1"}, "inputTopic", "accept", "any"},
		{"kafka-scale/streams-outputTopic", s.handleKafkaStreams, map[string]interface{}{"action": "reset", "appId": "a1"}, "outputTopic", "accept", "any"},

		// ---- kafka_advanced_ops.go ----
		{"kafka-adv/share-group", s.handleKafkaShareGroup, map[string]interface{}{"action": "create", "topic": "t1"}, "group", "accept", "any"},
		{"kafka-adv/share-topic", s.handleKafkaShareGroup, map[string]interface{}{"action": "create", "group": "g1"}, "topic", "accept", "any"},
		{"kafka-adv/oauth-version", s.handleOAuth, map[string]interface{}{"action": "deploy"}, "version", "accept", "any"},

		// ---- kafka_final_ops.go ----
		{"kafka-final/tiered-bucket", s.handleKafkaTieredStorage, map[string]interface{}{"action": "configure"}, "bucket", "accept", "any"},
		{"kafka-final/kraft-zk", s.handleKafkaKRaftMigration, map[string]interface{}{"action": "check"}, "zkConnect", "accept", "any"},
		{"kafka-final/streams-full-appId", s.handleKafkaStreamsFull, map[string]interface{}{"action": "reset"}, "appId", "accept", "any"},
		{"kafka-final/otel-endpoint", s.handleKafkaOTel, map[string]interface{}{"action": "enable"}, "endpoint", "accept", "any"},

		// ---- graph_ext_ops.go ----
		{"graph/scale-node", s.handleGraphScale, map[string]interface{}{"action": "add-node"}, "node", "accept", "any"},
		{"graph/upgrade-version", s.handleGraphUpgrade, map[string]interface{}{"action": "upgrade", "type": "neo4j"}, "targetVersion", "accept", "any"},
		{"graph/backup-list-path", s.handleGraphBackupExt, map[string]interface{}{"action": "list"}, "path", "accept", "any"},

		// ---- gpu_ext_ops.go ----
		{"gpu/driver-version", s.handleGPUDriver, map[string]interface{}{"action": "install"}, "version", "accept", "any"},
		{"gpu/cuda-version", s.handleGPUCuda, map[string]interface{}{"action": "install"}, "version", "accept", "any"},
		{"gpu/mig-profile", s.handleGPUMig, map[string]interface{}{"action": "create"}, "profile", "accept", "any"},
		{"gpu/mig-migId", s.handleGPUMig, map[string]interface{}{"action": "delete"}, "migId", "accept", "any"},
		{"gpu/cost-tenantId", s.handleGPUCost, map[string]interface{}{"action": "billing"}, "tenantId", "accept", "any"},
		{"gpu/anomaly-nodeId", s.handleGPUAnomaly, map[string]interface{}{"action": "health"}, "nodeId", "accept", "any"},
		{"gpu/ascend-driver-version", s.handleAscendDriver, map[string]interface{}{"action": "install"}, "version", "accept", "any"},
		{"gpu/ascend-virt-profile", s.handleAscendVirtualization, map[string]interface{}{"action": "create"}, "profile", "accept", "any"},
	}
}

// b10RejectCase 是「白名单」字段：载荷必须被**拒收**（400）且不下发任何脚本。
//
// valid 是该字段的**合法值**——测试先用它证明这条路径本身是通的（否则 400 可能来自
// 别的分支 ⇒ 假通过）。
type b10RejectCase struct {
	name    string
	handler http.HandlerFunc
	body    map[string]interface{}
	field   string
	valid   string
}

// b10RejectCases 列出本批被白名单收口的字段。
func b10RejectCases(s *Server) []b10RejectCase {
	return []b10RejectCase{
		// ---- kafka_ops.go ----
		{"kafka/acl-principal", s.handleKafkaACL, map[string]interface{}{"action": "create", "resource": "topic", "name": "t1", "operation": "Read", "permission": "Allow"}, "principal", "User:u1"},
		{"kafka/acl-resource", s.handleKafkaACL, map[string]interface{}{"action": "create", "principal": "User:u1", "name": "t1", "operation": "Read", "permission": "Allow"}, "resource", "topic"},
		{"kafka/acl-name", s.handleKafkaACL, map[string]interface{}{"action": "create", "principal": "User:u1", "resource": "topic", "operation": "Read", "permission": "Allow"}, "name", "t1"},
		{"kafka/acl-operation", s.handleKafkaACL, map[string]interface{}{"action": "create", "principal": "User:u1", "resource": "topic", "name": "t1", "permission": "Allow"}, "operation", "Read"},
		{"kafka/connect-connector", s.handleKafkaConnect, map[string]interface{}{"action": "status"}, "connector", "c1"},
		{"kafka/quota-entity", s.handleKafkaQuota, map[string]interface{}{"action": "set", "entityName": "n1", "producerRate": 1024}, "entity", "topics"},
		{"kafka/quota-entityName", s.handleKafkaQuota, map[string]interface{}{"action": "set", "entity": "topics", "producerRate": 1024}, "entityName", "n1"},

		// ---- kafka_ops_ext.go ----
		{"kafka-ext/rebalance-topics", s.handleKafkaRebalance, map[string]interface{}{"action": "preferred-leader-election"}, "topics", "t1,t2"},
		{"kafka-ext/delete-records-topic", s.handleKafkaDeleteRecords, map[string]interface{}{}, "topic", "t1"},
		{"kafka-ext/config-entityType", s.handleKafkaConfig, map[string]interface{}{"action": "describe", "entityName": "t1"}, "entityType", "topics"},
		{"kafka-ext/config-entityName", s.handleKafkaConfig, map[string]interface{}{"action": "describe", "entityType": "topics"}, "entityName", "t1"},

		// ---- kafka_advanced_ops.go ----
		{"kafka-adv/oauth-realm", s.handleOAuth, map[string]interface{}{"action": "create-realm"}, "realm", "r1"},
		{"kafka-adv/oauth-clientId", s.handleOAuth, map[string]interface{}{"action": "create-client", "realm": "r1"}, "clientId", "c1"},

		// ---- kafka_final_ops.go ----
		{"kafka-final/keda-consumerGroup", s.handleKafkaKEDA, map[string]interface{}{"action": "create-scaledobject", "topic": "t1"}, "consumerGroup", "g1"},
		{"kafka-final/keda-topic", s.handleKafkaKEDA, map[string]interface{}{"action": "create-scaledobject", "consumerGroup": "g1"}, "topic", "t1"},

		// ---- graph_ext_ops.go ----
		{"graph/upgrade-type", s.handleGraphUpgrade, map[string]interface{}{"action": "plan"}, "type", "neo4j"},
		{"graph/user-name", s.handleGraphUser, map[string]interface{}{"action": "create", "type": "neo4j", "password": "p1"}, "user", "u1"},
		{"graph/user-password", s.handleGraphUser, map[string]interface{}{"action": "create", "type": "neo4j", "user": "u1"}, "password", "p1"},
		{"graph/index-name", s.handleGraphIndex, map[string]interface{}{"action": "create", "type": "neo4j"}, "index", "i1"},
		{"graph/schema-name", s.handleGraphSchema, map[string]interface{}{"action": "create-tag", "type": "neo4j", "props": "name string"}, "name", "t1"},
		{"graph/schema-props", s.handleGraphSchema, map[string]interface{}{"action": "create-tag", "type": "neo4j", "name": "t1"}, "props", "name string"},
		{"graph/backup-path", s.handleGraphBackupExt, map[string]interface{}{"action": "backup", "type": "neo4j"}, "path", "/var/backups/graph"},

		// ---- gpu_ext_ops.go ----
		{"gpu/node-mgmt-nodeId", s.handleGPUNodeMgmt, map[string]interface{}{"action": "cordon"}, "nodeId", "node1"},
	}
}

// TestB10InjectionQuoted 判据③（值位置）：载荷只以**字面量**形态出现在脚本里。
func TestB10InjectionQuoted(t *testing.T) {
	s := newShellQuoteServer()
	for _, c := range b10AcceptCases(s) {
		for pi, payload := range b10Payloads(c.kind) {
			body := map[string]interface{}{"deviceID": "b10-" + c.name + "-" + string(rune('a'+pi))}
			for k, v := range c.body {
				body[k] = v
			}
			body[c.field] = payload
			dev := body["deviceID"].(string)
			rec := sqPost(t, s, c.handler, body)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("%s：应受理：%d %s", c.name, rec.Code, rec.Body.String())
			}
			sc := sqDispatchScript(t, s, dev)
			b10AssertNoInjection(t, c.name+"/"+payload, sc, payload)
		}
	}
}

// TestB10RejectedByWhitelist 判据④（白名单）：载荷必须被拒收（400），且**不下发任何脚本**。
//
// 「不下发任何脚本」这条比 400 更重要：控制面拒收 ⇒ 没有脚本 ⇒ agent 端无从执行。
func TestB10RejectedByWhitelist(t *testing.T) {
	s := newShellQuoteServer()
	for _, c := range b10RejectCases(s) {
		// 先用**合法值**证明这条路径本身是通的——否则 400 可能来自别的分支 ⇒ 假通过。
		okBody := map[string]interface{}{"deviceID": "b10-ok-" + c.name}
		for k, v := range c.body {
			okBody[k] = v
		}
		okBody[c.field] = c.valid
		rec := sqPost(t, s, c.handler, okBody)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("%s：合法值 %q 应受理（否则下面的 400 无法归因到本字段）：%d %s",
				c.name, c.valid, rec.Code, rec.Body.String())
		}
		for pi, payload := range b10Payloads("any") {
			body := map[string]interface{}{"deviceID": "b10-rej-" + c.name + "-" + string(rune('a'+pi))}
			for k, v := range c.body {
				body[k] = v
			}
			body[c.field] = payload
			dev := body["deviceID"].(string)
			rec := sqPost(t, s, c.handler, body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s：非法值应 400 拒收，实得 %d：%s", c.name, rec.Code, rec.Body.String())
				continue
			}
			if tasks := s.store.GetTasks(dev); len(tasks) != 0 {
				t.Errorf("%s：被拒收的请求不应下发任何脚本，却下发了 %d 个任务", c.name, len(tasks))
			}
		}
	}
}

// TestB10FreeFormJSONRejected 判据⑤：用户提供的**自由 JSON** 非法 ⇒ 400 拒收；
// 合法 ⇒ 仍受理（能力不误伤）。
func TestB10FreeFormJSONRejected(t *testing.T) {
	s := newShellQuoteServer()
	bad := []string{
		`{"a":`,
		`not json`,
		`{'a':'b'}`,
		`{"a":"b",}`,
	}
	cases := []b10Case{
		{"kafka/connect-config", s.handleKafkaConnect, map[string]interface{}{"action": "create", "connector": "c1"}, "config", "accept", "json"},
		{"kafka/schema-registry-schema", s.handleKafkaSchemaRegistry, map[string]interface{}{"action": "register", "subject": "s1"}, "schema", "accept", "json"},
	}
	for _, c := range cases {
		for i, payload := range bad {
			body := map[string]interface{}{"deviceID": "b10-free-" + c.name + "-" + string(rune('a'+i))}
			for k, v := range c.body {
				body[k] = v
			}
			body[c.field] = payload
			rec := sqPost(t, s, c.handler, body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s：非法 JSON 应 400 拒收，实得 %d：%s", c.name, rec.Code, rec.Body.String())
			}
		}
		// 合法 JSON 仍受理（能力不误伤）。
		body := map[string]interface{}{"deviceID": "b10-free-ok-" + c.name}
		for k, v := range c.body {
			body[k] = v
		}
		body[c.field] = `{"properties":{"a":{"type":"string"}}}`
		rec := sqPost(t, s, c.handler, body)
		if rec.Code != http.StatusAccepted {
			t.Errorf("%s：合法 JSON 应受理，实得 %d：%s", c.name, rec.Code, rec.Body.String())
		}
	}
}

// TestB10SemanticNoSideEffect 判据⑥：把脚本交给**真 sh** 执行，注入载荷不得产生副作用。
//
// 载荷用 `$(touch marker)`：旧形态里 URL 段是**裸拼**（无引号）、回显段是**双引号**
// 段 ⇒ 两处都会真的执行命令替换。
func TestB10SemanticNoSideEffect(t *testing.T) {
	sh := b9Shell()
	if sh == "" {
		t.Skip("本机无 POSIX shell（Windows），语义判据由 CI 的 ubuntu runner 承担")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, b10MarkerName)
	if strings.ContainsAny(marker, " \t") {
		t.Skip("临时目录路径含空白，跳过语义判据")
	}
	posixMarker := filepath.ToSlash(marker)

	s := newShellQuoteServer()
	plan := []struct {
		name    string
		handler http.HandlerFunc
		body    map[string]interface{}
		field   string
	}{
		{"kafka/schema-registry-subject", s.handleKafkaSchemaRegistry, map[string]interface{}{"action": "get-schema", "subject": "s1"}, "subject"},
		{"kafka/config-value", s.handleKafkaConfig, map[string]interface{}{"action": "alter", "entityType": "topics", "entityName": "t1"}, "config"},
		{"kafka/mm2-source", s.handleMirrorMakerFull, map[string]interface{}{"action": "create-link", "target": "t2"}, "source"},
		{"kafka/otel-endpoint", s.handleKafkaOTel, map[string]interface{}{"action": "enable"}, "endpoint"},
		{"graph/scale-node", s.handleGraphScale, map[string]interface{}{"action": "add-node"}, "node"},
		{"gpu/cost-tenantId", s.handleGPUCost, map[string]interface{}{"action": "billing"}, "tenantId"},
		{"gpu/driver-version", s.handleGPUDriver, map[string]interface{}{"action": "install"}, "version"},
	}
	for _, c := range plan {
		body := map[string]interface{}{"deviceID": "b10-sem-" + c.name}
		for k, v := range c.body {
			body[k] = v
		}
		body[c.field] = "$(touch " + posixMarker + ")"
		rec := sqPost(t, s, c.handler, body)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("%s：应受理：%d %s", c.name, rec.Code, rec.Body.String())
		}
		sc := sqDispatchScript(t, s, body["deviceID"].(string))
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_ = exec.CommandContext(ctx, sh, "-c", sc).Run() // 退出码无关紧要（Kafka/NVIDIA 多半不存在）
		cancel()
		if _, err := os.Stat(marker); err == nil {
			t.Fatalf("%s：注入载荷被执行（标记文件已落地）：\n%s", c.name, sc)
		}
	}
}

// TestB10URLQuoteSemantics 钉判据⑦：**整体 Quote 后，curl 收到的 URL 与原 URL 逐字相同**。
//
// 这是本批最容易被做错的一步——若把 Quote 结果塞回双引号段（`"http://…/'idx'"`），
// URL 里会多出字面量单引号（%27）⇒ 请求 404，而且**文本断言看不出来**（引号是配对的）。
// 故必须交给真 shell 求值来验。
func TestB10URLQuoteSemantics(t *testing.T) {
	sh := b9Shell()
	if sh == "" {
		t.Skip("本机无 POSIX shell（Windows），语义判据由 CI 的 ubuntu runner 承担")
	}
	paths := []string{
		"s1",
		"s1/v2",
		"subjects/with space",
		"subjects/with'quote",
		"subjects/a?b=c&d=e",
	}
	for _, p := range paths {
		out, err := exec.Command(sh, "-c", "printf '%s' "+opsURL("http://localhost:8081", p)).CombinedOutput()
		if err != nil {
			t.Fatalf("求值 %q 失败：%v（%s）", p, err, out)
		}
		want := "http://localhost:8081/" + p
		if string(out) != want {
			t.Errorf("URL 求值结果与原 URL 不等：\n got=%q\nwant=%q", string(out), want)
		}
	}
}

// TestB10CommandNameRejected 钉判据①：Graph 的 `req.Type` 是**命令名**（要当二进制执行）
// ⇒ 非白名单必须 400，且**不得**下发任何脚本。
//
// 为什么这条单列：Quote 挡不住命令注入——`'sh'` 仍是一个合法参数，但作为命令名毫无
// 意义；真正的风险是载荷指向任意二进制/脚本路径。
func TestB10CommandNameRejected(t *testing.T) {
	s := newShellQuoteServer()
	for i, payload := range []string{
		"sh",
		"bash",
		"/bin/sh",
		"neo4j; id",
		"$(id)",
		"`id`",
		"touch /tmp/opsmesh-b10-pwned",
	} {
		dev := fmt.Sprintf("b10-cmd-%d", i)
		rec := sqPost(t, s, s.handleGraphUpgrade, map[string]interface{}{
			"deviceID": dev, "action": "plan", "type": payload,
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("req.Type=%q 应 400 拒收（命令名白名单），实得 %d：%s", payload, rec.Code, rec.Body.String())
			continue
		}
		if tasks := s.store.GetTasks(dev); len(tasks) != 0 {
			t.Errorf("req.Type=%q 被拒收却下发了 %d 个任务", payload, len(tasks))
		}
	}
	// 白名单内的合法值仍受理（能力不误伤）。
	rec := sqPost(t, s, s.handleGraphUpgrade, map[string]interface{}{
		"deviceID": "b10-cmd-ok", "action": "plan", "type": "neo4j",
	})
	if rec.Code != http.StatusAccepted {
		t.Errorf("req.Type=neo4j 应受理，实得 %d：%s", rec.Code, rec.Body.String())
	}
}

// TestB10HeredocEarlyCloseRejected 钉判据⑧：heredoc 内容里出现**一行恰好等于 EOF**
// ⇒ 必须 400 拒收（否则 heredoc 提前闭合，其后内容被 shell 当**命令**执行）。
func TestB10HeredocEarlyCloseRejected(t *testing.T) {
	s := newShellQuoteServer()
	dev := "b10-heredoc-1"
	rec := sqPost(t, s, s.handleKafkaScale, map[string]interface{}{
		"deviceID": dev, "action": "add-broker", "brokerId": "1",
		"host": "h1\nEOF\ntouch /tmp/opsmesh-b10-pwned", "port": 9092,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("heredoc 内容含一行恰好 EOF 时应 400 拒收，实得 %d：%s", rec.Code, rec.Body.String())
	}
	if tasks := s.store.GetTasks(dev); len(tasks) != 0 {
		t.Errorf("被拒收的请求不应下发任何脚本，却下发了 %d 个任务", len(tasks))
	}
}

// TestB10HeredocEarlyCloseIsRealVector 证明判据⑧的向量**真实可执行**（不是假想）。
//
// 构造**旧形态**（真换行的 heredoc）并交给真 sh：内容里有一行恰好 EOF ⇒ heredoc 提前
// 闭合 ⇒ 其后的 `touch` 被当命令执行 ⇒ 标记文件真的落地。
//
// 本用例不测产品代码，只测「shell 的语义」——它是判据⑧存在的前提。
func TestB10HeredocEarlyCloseIsRealVector(t *testing.T) {
	sh := b9Shell()
	if sh == "" {
		t.Skip("本机无 POSIX shell（Windows），语义判据由 CI 的 ubuntu runner 承担")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, b10MarkerName)
	if strings.ContainsAny(marker, " \t") {
		t.Skip("临时目录路径含空白，跳过语义判据")
	}
	posixMarker := filepath.ToSlash(marker)
	// 旧形态：heredoc 内容里有一行恰好 EOF ⇒ 提前闭合 ⇒ 其后的 touch 被当命令执行。
	content := "broker.id=1\nEOF\ntouch " + posixMarker + "\n"
	oldScript := "cat > server.properties << 'EOF'\n" + content + "\nEOF\necho done\n"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	cmd := exec.CommandContext(ctx, sh, "-c", oldScript)
	cmd.Dir = dir
	_ = cmd.Run()
	cancel()
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("heredoc 提前闭合向量不成立（旧形态未落地标记文件）——判据⑧的前提失效：%v", err)
	}
}

// TestB10MirrorMakerHeredocIsWellFormed 钉「mirror-maker 的 heredoc 首次可用」这条能力变化。
//
// 旧形态写在 Go 的 **raw string** 里，其中的 `\n` 是**字面**反斜杠+n ⇒ 分隔符被拼成
// `EOFnclusters`、heredoc 永不闭合 ⇒ 该功能今天跑不起来。本批改用真换行。
func TestB10MirrorMakerHeredocIsWellFormed(t *testing.T) {
	s := newShellQuoteServer()
	rec := sqPost(t, s, s.handleMirrorMaker, map[string]interface{}{
		"deviceID": "b10-mm2-1", "action": "start",
		"sourceCluster": "c1", "targetCluster": "c2", "topics": "t1",
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("mirror-maker start 应受理：%d %s", rec.Code, rec.Body.String())
	}
	sc := sqDispatchScript(t, s, "b10-mm2-1")
	if !strings.Contains(sc, "<< 'EOF'\n") {
		t.Errorf("mirror-maker 的 heredoc 缺少「真换行」后的定界符行：\n%q", sc)
	}
	if !strings.Contains(sc, "\nEOF\n") {
		t.Errorf("mirror-maker 的 heredoc 缺少闭合分隔符行：\n%q", sc)
	}
	if strings.Contains(sc, `\n`) {
		t.Errorf("mirror-maker 的 heredoc 仍含字面 \\n（旧形态：分隔符被拼成 EOFnclusters、heredoc 永不闭合）：\n%q", sc)
	}
}

// TestB10IntFieldsCannotCarryMetacharacters 钉「int 字段不是注入面」这条判据。
//
// gpu / throttle / limit / partition / offset 都是 int 字段：JSON 解码阶段类型已定，
// 传字符串 ⇒ 400 ⇒ 请求体**无法**把 shell 元字符送进来。
func TestB10IntFieldsCannotCarryMetacharacters(t *testing.T) {
	s := newShellQuoteServer()
	rec := sqPost(t, s, s.handleGPUMig, map[string]interface{}{
		"deviceID": "b10-int-1", "action": "create", "profile": "1g.5gb", "gpu": "$(id)",
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("int 字段传字符串应 400（JSON 类型校验），实得 %d：%s", rec.Code, rec.Body.String())
	}
}
