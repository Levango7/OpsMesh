package controlplane

// shellquote_security_test.go — shellQuote 全量修正（17 文件 / 269 调用点）与
// 三处新收编点（db_cloud_managed_ops / elasticsearch_ops_ext / db_nosql_ext_ops HBase）
// 的注入守卫（2026-11-05，取证见 docs/opsmesh-authz-audit-2026-11-04.md）。
//
// 为什么值得单独立守卫：旧 shellQuote 只转义 `"`——双引号段内 `$`、反引号、`\`
// 仍有特殊语义，`shellQuote("$(id)")` 产出 "$(id)" 会被 shell **真实执行**；
// 结尾反斜杠还会吞掉收尾引号破坏配对。这是一个**静默**失效：脚本照常下发、
// 只在执行时见分晓。本文件把「转义后无可展开结构」钉成构建期红。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/store"
)

func newShellQuoteServer() *Server {
	return &Server{
		store:       store.NewMemoryStore(),
		cfg:         &config.Config{TaskMaxRetries: 1, Demo: false, TrustGatewayHeaders: true},
		requireAuth: false,
	}
}

// sqPost 以 admin（网关注入路径）发一条 JSON POST——被收编的 handler 多数要求
// system:write，故测试走 admin 角色，专注注入面而非鉴权面（鉴权面另有用例覆盖）。
func sqPost(t *testing.T, s *Server, h http.HandlerFunc, body map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/sq-audit", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-Id", "sq-audit")
	req.Header.Set("X-User-Roles", "admin")
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

// hasUnescapedDoubleQuoteSpecial 是 QuoteDouble 语义的判据：去掉外壳两端双引号后，
// 正文里任何 `$`、反引号或 `"` 都必须是转义态（前一字符为 `\`）。
// 旧实现（只转义 "）在这组载荷上全部判红。
func hasUnescapedDoubleQuoteSpecial(s string) bool {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return true // 形状不对同样视为不安全
	}
	body := s[1 : len(s)-1]
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '\\':
			i++ // 被转义的下一个字符，跳过
		case '$', '`', '"':
			return true
		}
	}
	return false
}

// TestShellQuoteNeutralizesExpansion 直接钉 shellQuote 语义。
// 窄变异：把实现改回 `"` + strings.ReplaceAll(s, `"`, `\"`) + `"` ⇒ 本用例判红。
func TestShellQuoteNeutralizesExpansion(t *testing.T) {
	payloads := []string{
		`$(id)`,
		"`id`",
		`$HOME`,
		`${IFS}`,
		`a"; touch /tmp/j; echo "`,
		`back\`,
		`\\`,
		`$`,
		"`",
	}
	for _, p := range payloads {
		q := shellQuote(p)
		if hasUnescapedDoubleQuoteSpecial(q) {
			t.Errorf("shellQuote(%q) = %q：仍含未转义展开结构（可注入）", p, q)
		}
	}
}

// TestShellQuoteEmptyAndPlain 形状回归：常规值与空串的产出不得改变——
// 一批调用点把结果嵌在既有双引号段内（如 `".backup %s"`），形状是兼容面。
func TestShellQuoteEmptyAndPlain(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", `""`},
		{"plain", `"plain"`},
		{"ns:table", `"ns:table"`},
		{"a-b.c_d-1", `"a-b.c_d-1"`},
	}
	for _, c := range cases {
		if got := shellQuote(c.in); got != c.want {
			t.Errorf("shellQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestShellQuoteTrailingBackslashDoesNotBreakPairing 结尾反斜杠回归：
// 旧实现产出 "abc\" —— 反斜杠吞掉收尾引号、引号配对破坏，后续任意文本可被拼接。
func TestShellQuoteTrailingBackslashDoesNotBreakPairing(t *testing.T) {
	got := shellQuote("abc\\")
	if got != `"abc\\"` {
		t.Errorf("shellQuote(abc\\) = %q, want %q", got, `"abc\\"`)
	}
}

// ---------------------------------------------------------------- 新收编点（handler 级）

// TestESNodeDrainRejectsInjectionPayloads：node 会嵌进 `-d '{"...":"%s"}'`
// ——shell 单引号内的 JSON 文本，没有任何合法转义空间（加引号破坏外层，
// JSON 语法还要求转义 " 与 \），因此必须白名单拒绝。
func TestESNodeDrainRejectsInjectionPayloads(t *testing.T) {
	s := newShellQuoteServer()
	for _, bad := range []string{
		`$(id)`,
		"`id`",
		`n'; touch /tmp/x #`,
		`n"x`,
		"n x",
		`ns:n`,
		`a/b`,
	} {
		rec := sqPost(t, s, s.handleElasticsearchNodeDrain,
			map[string]interface{}{"deviceID": "sq-dev", "action": "drain", "node": bad})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("node=%q 应 400，实得 %d：%s", bad, rec.Code, rec.Body.String())
		}
	}
	// 合法名放行（下游 202/任务结果不在本断言射程）
	rec := sqPost(t, s, s.handleElasticsearchNodeDrain,
		map[string]interface{}{"deviceID": "sq-dev", "action": "drain", "node": "es-node-01"})
	if rec.Code == http.StatusBadRequest {
		t.Errorf("合法节点名被误拒：%s", rec.Body.String())
	}
}

// TestESShardRelocateRejectsInjectionPayloads：index/from/to 三值同 drain 的处境。
func TestESShardRelocateRejectsInjectionPayloads(t *testing.T) {
	s := newShellQuoteServer()
	cases := []map[string]interface{}{
		{"deviceID": "sq-dev", "action": "relocate", "index": "$(id)", "shard": 1, "from": "n1", "to": "n2"},
		{"deviceID": "sq-dev", "action": "relocate", "index": "idx", "shard": 1, "from": "`id`", "to": "n2"},
		{"deviceID": "sq-dev", "action": "relocate", "index": "idx", "shard": 1, "from": "n1", "to": `n"; x`},
	}
	for i, body := range cases {
		rec := sqPost(t, s, s.handleElasticsearchShardRelocate, body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("relocate case#%d 应 400，实得 %d：%s", i, rec.Code, rec.Body.String())
		}
	}
	rec := sqPost(t, s, s.handleElasticsearchShardRelocate,
		map[string]interface{}{"deviceID": "sq-dev", "action": "relocate", "index": "idx-1", "shard": 0, "from": "node-1", "to": "node-2"})
	if rec.Code == http.StatusBadRequest {
		t.Errorf("合法参数被误拒：%s", rec.Body.String())
	}
}

// TestCloudManagedParamsQuotedInScript：云 CLI 参数走引号层（不是白名单）——
// 注入载荷必须作为**转义后的字面量**进入下发脚本，不得原样拼接。
func TestCloudManagedParamsQuotedInScript(t *testing.T) {
	s := newShellQuoteServer()
	payload := `$(id)`
	rec := sqPost(t, s, s.handleDBAurora,
		map[string]interface{}{"deviceID": "sq-cli-dev", "action": "status", "id": payload})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status 载荷应被受理（引号层保护）：%d %s", rec.Code, rec.Body.String())
	}
	var cmd string
	for _, tk := range s.store.GetTasks("sq-cli-dev") {
		if tk != nil {
			cmd = tk.Command
			break
		}
	}
	if cmd == "" {
		t.Fatal("未找到派发任务")
	}
	if !strings.Contains(cmd, `"\$(id)"`) {
		t.Errorf("脚本未含转义字面量 %q：%s", `"\$(id)"`, cmd)
	}
	if strings.Contains(cmd, `"$(id)"`) {
		t.Errorf("脚本含未转义形态（可注入）：%s", cmd)
	}
}

// TestCloudManagedAllProvidersQuote 六家云厂商逐一验证转义路径生效：
// 每家取一个 action，断言注入载荷以转义形态进入脚本。
func TestCloudManagedAllProvidersQuote(t *testing.T) {
	s := newShellQuoteServer()
	payload := `$(id)`
	cases := []struct {
		name string
		h    http.HandlerFunc
		body map[string]interface{}
	}{
		{"aurora", s.handleDBAurora, map[string]interface{}{"deviceID": "d1", "action": "delete", "id": payload}},
		{"cloudsql", s.handleDBCloudSQL, map[string]interface{}{"deviceID": "d2", "action": "status", "name": payload}},
		{"azuresql", s.handleDBAzureSQL, map[string]interface{}{"deviceID": "d3", "action": "status", "server": payload}},
		{"polardb", s.handleDBPolarDB, map[string]interface{}{"deviceID": "d4", "action": "delete", "id": payload}},
		{"gaussdb", s.handleDBGaussDBDWS, map[string]interface{}{"deviceID": "d5", "action": "delete", "id": payload}},
		{"tdsqlc", s.handleDBTDSQLC, map[string]interface{}{"deviceID": "d6", "action": "delete", "id": payload}},
	}
	for _, c := range cases {
		rec := sqPost(t, s, c.h, c.body)
		if rec.Code != http.StatusAccepted {
			t.Errorf("%s 应受理：%d %s", c.name, rec.Code, rec.Body.String())
			continue
		}
		dev, _ := c.body["deviceID"].(string)
		var cmd string
		for _, tk := range s.store.GetTasks(dev) {
			if tk != nil {
				cmd = tk.Command
				break
			}
		}
		if cmd == "" {
			t.Errorf("%s 未找到派发任务", c.name)
			continue
		}
		if !strings.Contains(cmd, `"\$(id)"`) {
			t.Errorf("%s 脚本未含转义字面量：%s", c.name, cmd)
		}
	}
}
