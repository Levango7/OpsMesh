package controlplane

// mq_injection_test.go — 批次 11（MQ 运维面「req.* 未转义直插」收口）的注入守卫。
//
// 覆盖范围：internal/controlplane 下 6 个文件、60 处脚本行：
//
//	rabbitmq_ops.go 16 / pulsar_ops.go 14 / mq_ext_ops.go 10 /
//	rabbitmq_ext_ops.go 8 / rocketmq_ext_ops.go 6 / rocketmq_ops.go 6
//
// # 为什么是真缺陷面
//
// 这些 handler 把用户可控字段裸拼进要下发到 agent 的 shell 脚本，agent 以 `sh -c`
// 执行 ⇒ `$(id)` / 反引号 / 分号被**真实执行**。鉴权面无缺口（均经 dispatchKafkaTask
// → subsystemAuthorize 的 task:write 闸）⇒ 已授权调用者可直接打到 agent 的 RCE。
//
// **双引号不是防护**：POSIX 双引号内 `$` 与反引号照样展开。
//
// # 本批的五个易错点（守卫专门盯这五条）
//
//	① **列表不能整体 Quote**：`for node in %s` 的参数要被 shell 词拆分 ⇒ 逐项白名单后
//	   以空格连接（mqNodeList）；整体 Quote 会把列表压成一个词 ⇒ 循环只跑一次。
//	② **heredoc 的证书路径不能 Quote**：它们是**配置文件内容**——Quote 会让 RabbitMQ
//	   读到带引号的路径 ⇒ 启动失败；换行 / 单独成行的 `EOF` 是注入面 ⇒ 绝对路径白名单。
//	③ **版本号落下载 URL 的路径段**：放行 `/` 会改写路径层级、放行 `?` / `#` 会改写 URL
//	   语义 ⇒ 版本号白名单（mqVersion）。
//	④ **选项值位置**：`-t %s` / `--subscription %s` 的值若以 `-` 开头会被 CLI 解析成
//	   另一个选项 ⇒ 资源名白名单必须**字母数字开头**（mqName）。
//	⑤ **自由 JSON**：policy def / connector config 旧形态是 `'%s'`——单引号破段即可执行。
//	   改为 json.Valid 后整体 Quote（非法 JSON 在控制面 400）。
//
// # 载荷形态（沿用批次 7/10 的教训）
//
//   - 白名单类字段：载荷会被**拒收**（400）⇒ 判据是「必须 400 且不下发任何脚本」；
//   - 自由 JSON 字段：载荷必须是**合法 JSON**（否则会被新的校验拦下，测不到注入面）；
//   - 语义判据用**语法合法**的载荷：命令替换 `$(touch marker)` 在旧形态里真的会执行。

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

// b11Files 是本批守卫覆盖的源文件（相对本包目录）。
var b11Files = []string{
	"rabbitmq_ops.go",
	"pulsar_ops.go",
	"mq_ext_ops.go",
	"rabbitmq_ext_ops.go",
	"rocketmq_ext_ops.go",
	"rocketmq_ops.go",
}

// b11SafeHelpers 是**批次 11** 认可的「收口助手」集合：它们的返回值已是可安全进 shell
// 的字面量。
//
// 口径要与 `scripts/shell-injection-audit.py` 的 QUOTE_MARKERS 一致（盘点器那边是
// 文本判据，这里是 AST 级判据 ⇒ 本集合是**类型/语义感知**的权威口径）。
var b11SafeHelpers = map[string]bool{
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

	// mq_script.go（MQ 面，批次 11）
	"mqName":                  true,
	"mqNameAllowEmpty":        true,
	"mqVersion":               true,
	"mqNode":                  true,
	"mqNodeList":              true,
	"mqCSVList":               true,
	"mqRocketTopic":           true,
	"mqRocketTopicAllowEmpty": true,
	"mqPath":                  true,
}

// b11ValidatingHelpers 是「**校验型**」助手：它们返回 (值, error)，校验不过时返回
// **空串 + error**。
//
// 为什么要单独列：这类助手只有在使用方**检查了 error** 时才成立。若调用方把 error
// 丢掉（`x, _ := mqName(...)`），白名单就形同虚设——字段被静默清空。
// 故守卫另有一条断言（TestB11ValidatingHelperErrorChecked）盯这件事。
var b11ValidatingHelpers = map[string]bool{
	"opsQuoteJSONText":        true,
	"opsJSONPayload":          true,
	"mqName":                  true,
	"mqNameAllowEmpty":        true,
	"mqVersion":               true,
	"mqNode":                  true,
	"mqNodeList":              true,
	"mqCSVList":               true,
	"mqRocketTopic":           true,
	"mqRocketTopicAllowEmpty": true,
	"mqPath":                  true,
}

// b11MarkerName 是本批语义判据的副作用标记文件名。
const b11MarkerName = "pwned"

// b11PayloadAny 用于「值位置」的字段。
var b11PayloadAny = []string{
	`$(id)`,
	"`id`",
	`x; touch /tmp/opsmesh-b11-pwned`,
	`x'; touch /tmp/opsmesh-b11-pwned; echo '`,
	`"; touch /tmp/opsmesh-b11-pwned; echo "`,
}

// b11PayloadJSON 用于自由 JSON 字段：载荷必须是**合法 JSON**。
var b11PayloadJSON = []string{
	`{"a":"$(id)"}`,
	`{"a":"x'; touch /tmp/opsmesh-b11-pwned; echo '"}`,
	`{"a":"` + "`id`" + `"}`,
	`{"a":"\"; touch /tmp/opsmesh-b11-pwned; echo \""}`,
}

// b11Payloads 按 kind 取载荷集。
func b11Payloads(kind string) []string {
	if kind == "json" {
		return b11PayloadJSON
	}
	return b11PayloadAny
}

// b11Case 覆盖一个 handler × 注入字段组合。
type b11Case struct {
	name    string
	handler http.HandlerFunc
	body    map[string]interface{}
	field   string
	expect  string // "accept"（载荷只以字面量形态出现在脚本里）| "reject"（白名单拒收 ⇒ 400）
	kind    string // "any" | "json"
}

// b11AcceptCases 是「值位置」字段：载荷**被受理**，但只能以字面量形态出现在脚本里。
func b11AcceptCases(s *Server) []b11Case {
	return []b11Case{
		// ---- rabbitmq_ops.go ----
		{"rabbitmq/user-password", s.handleRabbitMQUser, map[string]interface{}{"action": "add", "user": "u1"}, "password", "accept", "any"},
		{"rabbitmq/set-permissions-conf", s.handleRabbitMQUser, map[string]interface{}{"action": "set-permissions", "user": "u1"}, "conf", "accept", "any"},
		{"rabbitmq/policy-pattern", s.handleRabbitMQPolicy, map[string]interface{}{"action": "set", "name": "p1", "def": `{"a":"b"}`}, "pattern", "accept", "any"},
		{"rabbitmq/shovel-srcUri", s.handleRabbitMQShovel, map[string]interface{}{"action": "add", "name": "sh1", "destUri": "amqp://d", "destQueue": "q1"}, "srcUri", "accept", "any"},
		{"rabbitmq/federation-uri", s.handleRabbitMQFederation, map[string]interface{}{"action": "add-upstream", "name": "up1"}, "uri", "accept", "any"},
		{"rabbitmq/message", s.handleRabbitMQMessage, map[string]interface{}{"action": "publish", "queue": "q1"}, "message", "accept", "any"},

		// ---- pulsar_ops.go ----
		{"pulsar/function-inputs", s.handlePulsarFunction, map[string]interface{}{"action": "create", "name": "fn1", "className": "a.B"}, "inputs", "accept", "any"},
		{"pulsar/subscription-subType", s.handlePulsarSubscription, map[string]interface{}{"action": "create", "topic": "t1", "subName": "s1"}, "subType", "accept", "any"},

		// ---- mq_ext_ops.go ----
		{"mq-ext/connector-config", s.handlePulsarConnector, map[string]interface{}{"action": "create", "name": "conn1", "className": "a.B"}, "config", "accept", "json"},
		{"mq-ext/add-broker", s.handlePulsarScale, map[string]interface{}{"action": "add-broker"}, "broker", "accept", "any"},
		{"mq-ext/tiered-storage-storage", s.handlePulsarTieredStorage, map[string]interface{}{"action": "enable"}, "storage", "accept", "any"},

		// ---- rocketmq_ext_ops.go ----
		{"rocketmq-ext/remove-broker", s.handleRocketMQScale, map[string]interface{}{"action": "remove-broker"}, "broker", "accept", "any"},
	}
}

// b11RejectCase 是「白名单」字段：载荷必须被**拒收**（400），且**不下发任何脚本**。
//
// valid 是该字段的**合法值**——测试先用它证明这条路径本身是通的（否则 400 可能来自
// 别的分支 ⇒ 假通过）。
type b11RejectCase struct {
	name    string
	handler http.HandlerFunc
	body    map[string]interface{}
	field   string
	valid   string
}

// b11RejectCases 列出本批被白名单收口的字段。
func b11RejectCases(s *Server) []b11RejectCase {
	return []b11RejectCase{
		// ---- rabbitmq_ops.go ----
		{"rabbitmq/deploy-version", s.handleRabbitMQDeploy, map[string]interface{}{"mode": "single"}, "version", "3.13"},
		{"rabbitmq/user-add", s.handleRabbitMQUser, map[string]interface{}{"action": "add", "password": "p1"}, "user", "u1"},
		{"rabbitmq/user-delete", s.handleRabbitMQUser, map[string]interface{}{"action": "delete"}, "user", "u1"},
		{"rabbitmq/user-set-permissions", s.handleRabbitMQUser, map[string]interface{}{"action": "set-permissions"}, "user", "u1"},
		{"rabbitmq/cluster-join", s.handleRabbitMQCluster, map[string]interface{}{"action": "join"}, "node", "rabbit@h1"},
		{"rabbitmq/policy-set-name", s.handleRabbitMQPolicy, map[string]interface{}{"action": "set", "pattern": "^p", "def": `{"a":"b"}`}, "name", "p1"},
		{"rabbitmq/policy-clear-name", s.handleRabbitMQPolicy, map[string]interface{}{"action": "clear"}, "name", "p1"},
		{"rabbitmq/shovel-add-name", s.handleRabbitMQShovel, map[string]interface{}{"action": "add", "srcUri": "amqp://s", "destUri": "amqp://d"}, "name", "sh1"},
		{"rabbitmq/shovel-delete-name", s.handleRabbitMQShovel, map[string]interface{}{"action": "delete"}, "name", "sh1"},
		{"rabbitmq/federation-add-name", s.handleRabbitMQFederation, map[string]interface{}{"action": "add-upstream", "uri": "amqp://u"}, "name", "up1"},
		{"rabbitmq/federation-remove-name", s.handleRabbitMQFederation, map[string]interface{}{"action": "remove-upstream"}, "name", "up1"},
		{"rabbitmq/message-publish-queue", s.handleRabbitMQMessage, map[string]interface{}{"action": "publish", "message": "m1"}, "queue", "q1"},
		{"rabbitmq/message-get-queue", s.handleRabbitMQMessage, map[string]interface{}{"action": "get"}, "queue", "q1"},
		{"rabbitmq/message-count-queue", s.handleRabbitMQMessage, map[string]interface{}{"action": "count"}, "queue", "q1"},

		// ---- rabbitmq_ext_ops.go ----
		{"rabbitmq-ext/ha-join", s.handleRabbitMQHA, map[string]interface{}{"action": "join"}, "node", "rabbit@h1"},
		{"rabbitmq-ext/ha-setup", s.handleRabbitMQHA, map[string]interface{}{"action": "setup"}, "nodes", "rabbit@h1,rabbit@h2"},
		{"rabbitmq-ext/scale-add-node", s.handleRabbitMQScale, map[string]interface{}{"action": "add-node"}, "node", "rabbit@h1"},
		{"rabbitmq-ext/scale-remove-node", s.handleRabbitMQScale, map[string]interface{}{"action": "remove-node"}, "node", "rabbit@h1"},
		{"rabbitmq-ext/upgrade", s.handleRabbitMQUpgrade, map[string]interface{}{"action": "upgrade"}, "targetVersion", "3.13.0"},
		{"rabbitmq-ext/backup-path", s.handleRabbitMQBackup, map[string]interface{}{"action": "export"}, "path", "/tmp/b.json"},
		{"rabbitmq-ext/enable-tls-caCert", s.handleRabbitMQSecurity, map[string]interface{}{"action": "enable-tls", "certPath": "/etc/ssl/certs/c.pem", "keyPath": "/etc/ssl/private/k.pem"}, "caCert", "/etc/ssl/certs/ca.pem"},

		// ---- pulsar_ops.go ----
		{"pulsar/deploy-version", s.handlePulsarDeploy, map[string]interface{}{}, "version", "3.2.0"},
		{"pulsar/topic-create", s.handlePulsarTopic, map[string]interface{}{"action": "create"}, "topic", "my.topic_1"},
		{"pulsar/topic-delete", s.handlePulsarTopic, map[string]interface{}{"action": "delete"}, "topic", "my.topic_1"},
		{"pulsar/subscription-create-subName", s.handlePulsarSubscription, map[string]interface{}{"action": "create", "topic": "t1"}, "subName", "sub1"},
		{"pulsar/subscription-create-topic", s.handlePulsarSubscription, map[string]interface{}{"action": "create", "subName": "s1"}, "topic", "t1"},
		{"pulsar/tenant-create-tenant", s.handlePulsarTenant, map[string]interface{}{"action": "create"}, "tenant", "public2"},
		{"pulsar/tenant-create-clusters", s.handlePulsarTenant, map[string]interface{}{"action": "create", "tenant": "public"}, "clusters", "c1,c2"},
		{"pulsar/namespace-create", s.handlePulsarNamespace, map[string]interface{}{"action": "create"}, "namespace", "ns1"},
		{"pulsar/namespace-list-tenant", s.handlePulsarNamespace, map[string]interface{}{"action": "list"}, "tenant", "public"},
		{"pulsar/function-create-name", s.handlePulsarFunction, map[string]interface{}{"action": "create", "className": "a.B"}, "name", "fn1"},
		{"pulsar/function-create-className", s.handlePulsarFunction, map[string]interface{}{"action": "create", "name": "fn1"}, "className", "a.B.C"},
		{"pulsar/function-status-name", s.handlePulsarFunction, map[string]interface{}{"action": "status"}, "name", "fn1"},

		// ---- mq_ext_ops.go ----
		{"mq-ext/upgrade-version", s.handlePulsarUpgrade, map[string]interface{}{"action": "upgrade"}, "targetVersion", "3.2.0"},
		{"mq-ext/ha-setup-nodes", s.handlePulsarHA, map[string]interface{}{"action": "setup-bookkeeper"}, "nodes", "h1,h2"},
		{"mq-ext/schema-list-topic", s.handlePulsarSchemaRegistry, map[string]interface{}{"action": "list"}, "topic", "t1"},
		{"mq-ext/schema-get-topic", s.handlePulsarSchemaRegistry, map[string]interface{}{"action": "get"}, "topic", "t1"},
		{"mq-ext/connector-create-name", s.handlePulsarConnector, map[string]interface{}{"action": "create", "className": "a.B", "config": `{"a":"b"}`}, "name", "conn1"},
		{"mq-ext/connector-create-className", s.handlePulsarConnector, map[string]interface{}{"action": "create", "name": "conn1", "config": `{"a":"b"}`}, "className", "a.B.C"},
		{"mq-ext/connector-delete-name", s.handlePulsarConnector, map[string]interface{}{"action": "delete"}, "name", "conn1"},

		// ---- rocketmq_ops.go ----
		{"rocketmq/deploy-version", s.handleRocketMQDeploy, map[string]interface{}{}, "version", "5.2.0"},
		{"rocketmq/topic-create", s.handleRocketMQTopic, map[string]interface{}{"action": "create"}, "topic", "my_topic-1"},
		{"rocketmq/topic-create-dlq", s.handleRocketMQTopic, map[string]interface{}{"action": "create"}, "topic", "%DLQ%group"},
		{"rocketmq/topic-delete", s.handleRocketMQTopic, map[string]interface{}{"action": "delete"}, "topic", "my_topic-1"},
		{"rocketmq/consumer-group-describe", s.handleRocketMQConsumerGroup, map[string]interface{}{"action": "describe"}, "group", "group1"},
		{"rocketmq/acl-create-subject", s.handleRocketMQACL, map[string]interface{}{"action": "create"}, "subject", "user1"},
		{"rocketmq/acl-create-topic", s.handleRocketMQACL, map[string]interface{}{"action": "create", "subject": "user1"}, "topic", "%DLQ%group"},
		{"rocketmq/acl-create-group", s.handleRocketMQACL, map[string]interface{}{"action": "create", "subject": "user1"}, "group", "group1"},

		// ---- rocketmq_ext_ops.go ----
		{"rocketmq-ext/upgrade", s.handleRocketMQUpgrade, map[string]interface{}{"action": "upgrade"}, "targetVersion", "5.2.0"},
		{"rocketmq-ext/dlq-retry-topic", s.handleRocketMQDLQ, map[string]interface{}{"action": "retry", "group": "g1"}, "topic", "%DLQ%group"},
		{"rocketmq-ext/dlq-retry-group", s.handleRocketMQDLQ, map[string]interface{}{"action": "retry", "topic": "%DLQ%group"}, "group", "g1"},
		{"rocketmq-ext/dlq-purge-topic", s.handleRocketMQDLQ, map[string]interface{}{"action": "purge"}, "topic", "%DLQ%group"},
		{"rocketmq-ext/ha-setup-nodes", s.handleRocketMQHA, map[string]interface{}{"action": "setup-dledger"}, "nodes", "h1,h2"},
		{"rocketmq-ext/scale-add-broker", s.handleRocketMQScale, map[string]interface{}{"action": "add-broker"}, "broker", "broker-1"},
	}
}

// TestB11NoRawReqInterpolation 判据①（AST 级污点传播，**权威口径**）：
// 六个文件里不得再有未经收口的用户字段直插脚本。
func TestB11NoRawReqInterpolation(t *testing.T) {
	for _, f := range b11Files {
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
			st := newB9TaintState(b11SafeHelpers, true)
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

// TestB11ValidatingHelperErrorChecked 判据②：校验型助手的 error 必须被检查。
func TestB11ValidatingHelperErrorChecked(t *testing.T) {
	for _, f := range b11Files {
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
			b10CheckErrorsChecked(fset, f, fn, b11ValidatingHelpers, t)
		}
	}
}

// b11AssertNoInjection 断言脚本里载荷只以字面量形态存在。
func b11AssertNoInjection(t *testing.T, name, script string, payload string) {
	t.Helper()
	out := unquotedText(script)
	if payload != "" && strings.Contains(out, payload) {
		t.Errorf("%s：引号外文本含注入载荷 %q：\n脚本=%s\n引号外=%s", name, payload, script, out)
	}
	if strings.Contains(out, `"`+payload+`"`) {
		t.Errorf("%s：用户字段仍是双引号直插形态（双引号内 $ / 反引号会展开 ⇒ 可执行注入）：\n%s", name, script)
	}
}

// TestB11InjectionQuoted 判据③（值位置）：载荷只以**字面量**形态出现在脚本里。
func TestB11InjectionQuoted(t *testing.T) {
	s := newShellQuoteServer()
	for ci, c := range b11AcceptCases(s) {
		for pi, payload := range b11Payloads(c.kind) {
			dev := fmt.Sprintf("b11-accept-%d-%d", ci, pi)
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
			b11AssertNoInjection(t, c.name+"/"+payload, sc, payload)
		}
	}
}

// TestB11RejectedByWhitelist 判据④（白名单）：载荷必须被拒收（400），且**不下发任何脚本**。
func TestB11RejectedByWhitelist(t *testing.T) {
	s := newShellQuoteServer()
	for _, c := range b11RejectCases(s) {
		// 先用**合法值**证明这条路径本身是通的——否则 400 可能来自别的分支 ⇒ 假通过。
		okBody := map[string]interface{}{"deviceID": "b11-ok-" + c.name}
		for k, v := range c.body {
			okBody[k] = v
		}
		okBody[c.field] = c.valid
		rec := sqPost(t, s, c.handler, okBody)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("%s：合法值 %q 应受理（否则下面的 400 无法归因到本字段）：%d %s",
				c.name, c.valid, rec.Code, rec.Body.String())
		}
		for pi, payload := range b11Payloads("any") {
			body := map[string]interface{}{"deviceID": "b11-rej-" + c.name + "-" + string(rune('a'+pi))}
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

// TestB11FreeFormJSONRejected 判据⑤：用户提供的**自由 JSON** 非法 ⇒ 400 拒收；
// 合法 ⇒ 仍受理（能力不误伤）。
func TestB11FreeFormJSONRejected(t *testing.T) {
	s := newShellQuoteServer()
	bad := []string{
		`{"a":`,
		`not json`,
		`{'a':'b'}`,
		`{"a":"b",}`,
	}
	cases := []b11Case{
		{"rabbitmq/policy-def", s.handleRabbitMQPolicy, map[string]interface{}{"action": "set", "name": "p1", "pattern": "^p"}, "def", "accept", "json"},
		{"mq-ext/connector-config", s.handlePulsarConnector, map[string]interface{}{"action": "create", "name": "conn1", "className": "a.B"}, "config", "accept", "json"},
	}
	for _, c := range cases {
		for i, payload := range bad {
			body := map[string]interface{}{"deviceID": "b11-free-" + c.name + "-" + string(rune('a'+i))}
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
		body := map[string]interface{}{"deviceID": "b11-free-ok-" + c.name}
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

// TestB11SemanticNoSideEffect 判据⑥：把脚本交给**真 sh** 执行，注入载荷不得产生副作用。
func TestB11SemanticNoSideEffect(t *testing.T) {
	sh := b9Shell()
	if sh == "" {
		t.Skip("本机无 POSIX shell（Windows），语义判据由 CI 的 ubuntu runner 承担")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, b11MarkerName)
	if strings.ContainsAny(marker, " \t") {
		t.Skip("临时目录路径含空白，跳过语义判据")
	}
	posixMarker := filepath.ToSlash(marker)
	payload := "$(touch " + posixMarker + ")"

	s := newShellQuoteServer()
	plan := []struct {
		name    string
		handler http.HandlerFunc
		body    map[string]interface{}
		field   string
	}{
		{"rabbitmq/user-password", s.handleRabbitMQUser, map[string]interface{}{"action": "add", "user": "sem-u"}, "password"},
		{"rabbitmq/message", s.handleRabbitMQMessage, map[string]interface{}{"action": "publish", "queue": "sem-q"}, "message"},
		{"rabbitmq/policy-pattern", s.handleRabbitMQPolicy, map[string]interface{}{"action": "set", "name": "sem-p", "def": `{"a":"b"}`}, "pattern"},
		{"pulsar/add-broker", s.handlePulsarScale, map[string]interface{}{"action": "add-broker"}, "broker"},
		{"rocketmq/remove-broker", s.handleRocketMQScale, map[string]interface{}{"action": "remove-broker"}, "broker"},
	}
	for _, c := range plan {
		dev := "b11-sem-" + c.name
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
		if _, err := os.Stat(marker); err == nil {
			t.Fatalf("%s：注入载荷被执行（标记文件已落地）：\n%s", c.name, sc)
		}
	}
}

// TestB11WhitelistHelpers 钉住本批新增白名单的口径（不依赖 handler，直接测助手）。
func TestB11WhitelistHelpers(t *testing.T) {
	// 版本号：字母数字开头，拒绝 / ? # 与空白。
	for _, ok := range []string{"3.13.0", "5.2.0", "3.13.0-rc.1", "1:2.3.4", "3.2.0_lite"} {
		if _, err := mqVersion("version", ok); err != nil {
			t.Errorf("mqVersion(%q) 应通过：%v", ok, err)
		}
	}
	for _, bad := range []string{"", "../1", "3.2.0 --add-config x", "3.2.0?a=b", "3.2.0/evil", "-3.2.0", "3.2.0`id`"} {
		if _, err := mqVersion("version", bad); err == nil && bad != "" {
			t.Errorf("mqVersion(%q) 应拒收", bad)
		}
		if bad == "" {
			if v, err := mqVersion("version", bad); err != nil || v != "" {
				t.Errorf("mqVersion(\"\") 应原样放行（调用方套默认值）")
			}
		}
	}

	// 节点列表：逗号 / 空白都能分隔，逐项白名单。
	for _, c := range []struct{ in, want string }{
		{"rabbit@h1", "rabbit@h1"},
		{"rabbit@h1,rabbit@h2", "rabbit@h1 rabbit@h2"},
		{"rabbit@h1 rabbit@h2", "rabbit@h1 rabbit@h2"},
		{"rabbit@h1, rabbit@h2", "rabbit@h1 rabbit@h2"},
		{"h1,,h2", ""},
		{"h1;id", ""},
		{"h1,$(id)", ""},
		{"", ""},
	} {
		got, err := mqNodeList("nodes", c.in)
		if c.want == "" {
			if err == nil {
				t.Errorf("mqNodeList(%q) 应拒收", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("mqNodeList(%q) 应通过：%v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("mqNodeList(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}

	// CSV 单参数列表：逗号保留，逐项白名单；元素前后空白会被 trim 归一化。
	if got, err := mqCSVList("clusters", "c1, c2"); err != nil || got != "c1,c2" {
		t.Errorf("mqCSVList(\"c1, c2\") = %q, err=%v，期望 \"c1,c2\"", got, err)
	}
	if got, err := mqCSVList("clusters", "c1 "); err != nil || got != "c1" {
		t.Errorf("mqCSVList(\"c1 \") = %q, err=%v，期望 \"c1\"", got, err)
	}
	for _, bad := range []string{"c1,,c2", "c1;id", "c1,$(id)", ",c1", " "} {
		if got, err := mqCSVList("clusters", bad); err == nil {
			t.Errorf("mqCSVList(%q) 应拒收（got=%q）", bad, got)
		}
	}

	// RocketMQ topic：% 与 | 放行，其余口径同 mqName。
	for _, ok := range []string{"my_topic-1", "a.b.c", "%DLQ%group", "a|b"} {
		if _, err := mqRocketTopic("topic", ok); err != nil {
			t.Errorf("mqRocketTopic(%q) 应通过：%v", ok, err)
		}
	}
	for _, bad := range []string{"bad topic", "topic;id", "topic$", "-topic"} {
		if _, err := mqRocketTopic("topic", bad); err == nil {
			t.Errorf("mqRocketTopic(%q) 应拒收", bad)
		}
	}

	// 路径：绝对路径白名单，拒绝相对路径 / .. / 空白 / 元字符。
	for _, ok := range []string{"/tmp/b.json", "/etc/ssl/certs/ca.pem", "/var/backups/mq/"} {
		if _, err := mqPath("path", ok); err != nil {
			t.Errorf("mqPath(%q) 应通过：%v", ok, err)
		}
	}
	for _, bad := range []string{"relative/path", "/tmp/../etc/passwd", "/tmp/a b.json", "/tmp/$(id)", "/tmp/a`id`", ""} {
		if _, err := mqPath("path", bad); err == nil {
			t.Errorf("mqPath(%q) 应拒收", bad)
		}
	}
}
