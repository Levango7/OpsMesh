// support_endpoints_test.go — P1-6 可支撑性端点测试（版本/配置转储/诊断包/pprof）。
//
// 重点覆盖两类主张：
//  1. **脱敏**：敏感字段（口令/密钥/令牌/连接串凭证/URL 凭证）绝不出现在响应里——
//     用哨兵值 + 全量字符串搜索断言，而非逐字段检查（后者会随字段增删而漏）。
//  2. **鉴权**：诊断类端点需要 `diagnostics:dump`（admin 专有），viewer 必须被拒。
package controlplane

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/store"
)

// secretSentinel 是所有哨兵敏感值的公共前缀：响应里出现它即脱敏失败。
const secretSentinel = "SUPERSECRET"

// newSupportTestServer 构造带敏感哨兵的 Server（Demo=false：要求真实身份）。
func newSupportTestServer(t *testing.T) *Server {
	t.Helper()
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store: st,
		cfg: &config.Config{
			TaskMaxRetries: 3,
			HTTPPort:       8080,
			GRPCPort:       9090,
			MetricsPort:    9091,
			Store:          "memory",
			LogLevel:       "debug",
			// 哨兵：任一值出现在响应中即为脱敏缺陷。
			MySQLDSN:         "root:" + secretSentinel + "-DSN@tcp(127.0.0.1:3306)/opsmesh",
			RedisPassword:    secretSentinel + "-REDIS",
			JWTSecret:        secretSentinel + "-JWT",
			ProvisionSecret:  secretSentinel + "-PROVISION",
			EncryptionKey:    secretSentinel + "-ENC",
			GRPCSignatureKey: secretSentinel + "-SIG",
			FederationSecret: secretSentinel + "-FED",
			InstallToken:     secretSentinel + "-INSTALL",
			AdminPassword:    secretSentinel + "-ADMIN",
			VaultToken:       secretSentinel + "-VAULT",
			KmsToken:         secretSentinel + "-KMS",
			KmsKeyID:         secretSentinel + "-KMSKEYID",
			AlertEmailPass:   secretSentinel + "-SMTP",
			ProvisionSSHKey:  secretSentinel + "-SSHKEY",
			ProvisionSSHKP:   secretSentinel + "-SSHPASS",
			TLSKey:           "/etc/opsmesh/tls.key",
			// URL 内嵌凭证：脱敏必须剥掉 userinfo 与 query。
			LokiEndpoint:    "https://loki-user:" + secretSentinel + "-LOKIPASS@loki.internal:3100/loki/api/v1/push?token=" + secretSentinel + "-TOKEN",
			AlertWebhookURL: "https://hooks.example.com/notify?token=" + secretSentinel + "-WEBHOOK",
			LogPushEndpoint: secretSentinel + "-NOT-A-URL",
		},
		jwtSecret:    []byte("test-jwt-secret-for-support-test-32b!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// assertNoSecrets 断言内容中不含任何哨兵值（脱敏的机器可判定形式）。
func assertNoSecrets(t *testing.T, what, body string) {
	t.Helper()
	if strings.Contains(body, secretSentinel) {
		idx := strings.Index(body, secretSentinel)
		start := idx - 60
		if start < 0 {
			start = 0
		}
		end := idx + 60
		if end > len(body) {
			end = len(body)
		}
		t.Fatalf("%s 泄漏敏感值（哨兵 %s 出现在输出中）：…%s…", what, secretSentinel, body[start:end])
	}
}

// TestHandleVersion 验证 /version：无鉴权可取、字段齐备、不含任何配置机密。
func TestHandleVersion(t *testing.T) {
	s := newSupportTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	w := httptest.NewRecorder()
	s.handleVersion(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, key := range []string{`"version"`, `"commit"`, `"goVersion"`, `"uptimeSeconds"`} {
		if !strings.Contains(body, key) {
			t.Errorf("响应缺少字段 %s：%s", key, body)
		}
	}
	assertNoSecrets(t, "/version", body)

	// 非 GET 必须 405（与其余只读端点一致）。
	w2 := httptest.NewRecorder()
	s.handleVersion(w2, httptest.NewRequest(http.MethodPost, "/version", nil))
	if w2.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /version status=%d, want 405", w2.Code)
	}
}

// TestConfigSnapshot_Redaction 直接断言配置快照的脱敏与内容。
func TestConfigSnapshot_Redaction(t *testing.T) {
	s := newSupportTestServer(t)
	snap := s.configSnapshot()

	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	assertNoSecrets(t, "configSnapshot", string(raw))

	// 非敏感项应如实输出（否则「配置转储」无价值）。
	rt, _ := snap["runtime"].(map[string]any)
	// logLevel 刻意报告**生效值**（logx 进程级别）而非 cfg.LogLevel 的请求值：
	// 排障要回答的是「现在实际按什么级别在打日志」。
	if got, _ := rt["logLevel"].(string); got != logxLevelName() {
		t.Errorf("logLevel=%v 应等于生效级别 %q", rt["logLevel"], logxLevelName())
	}
	if rt["httpPort"] != 8080 {
		t.Errorf("runtime 快照内容不符: %v", rt)
	}
	st, _ := snap["store"].(map[string]any)
	if st["mysqlDsnConfigured"] != true {
		t.Errorf("mysqlDsnConfigured 应为 true（只出布尔不出值）: %v", st)
	}
	if _, hasRaw := st["mysqlDsn"]; hasRaw {
		t.Errorf("store 快照不应包含 mysqlDsn 原值: %v", st)
	}
	auth, _ := snap["auth"].(map[string]any)
	for _, k := range []string{"jwtSecretConfigured", "encryptionKeyConfigured", "provisionSecretConfigured"} {
		if auth[k] != true {
			t.Errorf("auth.%s 应为 true: %v", k, auth)
		}
	}
	// URL 字段必须剥掉 userinfo/query，只留 scheme://host/path。
	obs, _ := snap["observability"].(map[string]any)
	if got, _ := obs["lokiEndpoint"].(string); got != "https://loki.internal:3100/loki/api/v1/push" {
		t.Errorf("lokiEndpoint 未正确脱敏: %q", got)
	}
}

// TestAdminConfig_Authorization 验证配置转储的鉴权层次：
// 无身份 → 401；viewer（无 diagnostics:dump）→ 403；admin → 200。
func TestAdminConfig_Authorization(t *testing.T) {
	s := newSupportTestServer(t)

	anon := httptest.NewRecorder()
	s.handleAdminConfig(anon, httptest.NewRequest(http.MethodGet, "/api/v1/admin/config", nil))
	if anon.Code != http.StatusUnauthorized {
		t.Errorf("无身份 status=%d, want 401; body=%s", anon.Code, anon.Body.String())
	}

	viewer := httptest.NewRecorder()
	vr := httptest.NewRequest(http.MethodGet, "/api/v1/admin/config", nil)
	vr.Header.Set("Authorization", loginAsViewer(t, s))
	s.handleAdminConfig(viewer, vr)
	if viewer.Code != http.StatusForbidden {
		t.Errorf("viewer status=%d, want 403（viewer 不得读配置转储）; body=%s", viewer.Code, viewer.Body.String())
	}

	admin := httptest.NewRecorder()
	ar := httptest.NewRequest(http.MethodGet, "/api/v1/admin/config", nil)
	ar.Header.Set("Authorization", loginAsAdmin(t, s))
	s.handleAdminConfig(admin, ar)
	if admin.Code != http.StatusOK {
		t.Fatalf("admin status=%d, want 200; body=%s", admin.Code, admin.Body.String())
	}
	assertNoSecrets(t, "/api/v1/admin/config", admin.Body.String())
}

// TestAdminDiagnostics_Zip 验证诊断包：admin 可取、zip 结构完整、内容同样脱敏。
func TestAdminDiagnostics_Zip(t *testing.T) {
	s := newSupportTestServer(t)

	r := httptest.NewRequest(http.MethodGet, "/api/v1/admin/diagnostics", nil)
	r.Header.Set("Authorization", loginAsAdmin(t, s))
	w := httptest.NewRecorder()
	s.handleAdminDiagnostics(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type=%q, want application/zip", ct)
	}
	body := w.Body.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("诊断包不是合法 zip: %v", err)
	}
	want := map[string]bool{
		"README.txt": false, "version.json": false, "config.json": false,
		"health.json": false, "metrics.txt": false, "goroutines.txt": false,
	}
	var all bytes.Buffer
	for _, f := range zr.File {
		if _, ok := want[f.Name]; ok {
			want[f.Name] = true
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("打开 %s 失败: %v", f.Name, err)
		}
		_, _ = all.ReadFrom(rc)
		rc.Close()
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("诊断包缺少条目 %s", name)
		}
	}
	assertNoSecrets(t, "诊断包", all.String())

	// viewer 必须被拒（与 config 端点同一权限点）。
	vw := httptest.NewRecorder()
	vr := httptest.NewRequest(http.MethodGet, "/api/v1/admin/diagnostics", nil)
	vr.Header.Set("Authorization", loginAsViewer(t, s))
	s.handleAdminDiagnostics(vw, vr)
	if vw.Code != http.StatusForbidden {
		t.Errorf("viewer status=%d, want 403", vw.Code)
	}
}

// TestRegisterPprof_Gating 验证 pprof 的双层门槛：
// 默认关闭（404）；开启后仍受 --metrics-allow-cidr 准入（生产空白名单=拒）。
func TestRegisterPprof_Gating(t *testing.T) {
	// 1. 默认关闭：路由不存在。
	s := newSupportTestServer(t)
	muxOff := http.NewServeMux()
	s.registerPprof(muxOff)
	wOff := httptest.NewRecorder()
	muxOff.ServeHTTP(wOff, httptest.NewRequest(http.MethodGet, "/debug/pprof/goroutine", nil))
	if wOff.Code != http.StatusNotFound {
		t.Errorf("默认关闭时 /debug/pprof 应 404，实际 %d", wOff.Code)
	}

	// 2. 开启 + 生产 + 空 CIDR（fail-closed）→ 403。
	sProd := newSupportTestServer(t)
	sProd.cfg.DebugPprof = true
	sProd.cfg.Production = true
	muxDeny := http.NewServeMux()
	sProd.registerPprof(muxDeny)
	wDeny := httptest.NewRecorder()
	muxDeny.ServeHTTP(wDeny, httptest.NewRequest(http.MethodGet, "/debug/pprof/goroutine", nil))
	if wDeny.Code != http.StatusForbidden {
		t.Errorf("生产+空 CIDR 时 pprof 应 403（fail-closed），实际 %d", wDeny.Code)
	}

	// 3. 开启 + 白名单含来源 → 放行（这里只断言准入通过，即非 403/404）。
	sAllow := newSupportTestServer(t)
	sAllow.cfg.DebugPprof = true
	sAllow.cfg.Production = true
	sAllow.cfg.MetricsAllowCIDR = "127.0.0.0/8,::1/128"
	muxAllow := http.NewServeMux()
	sAllow.registerPprof(muxAllow)
	wAllow := httptest.NewRecorder()
	allowReq := httptest.NewRequest(http.MethodGet, "/debug/pprof/goroutine", nil)
	// httptest 默认 RemoteAddr 是 192.0.2.1:1234，须显式设为白名单内地址。
	allowReq.RemoteAddr = "127.0.0.1:12345"
	muxAllow.ServeHTTP(wAllow, allowReq)
	if wAllow.Code == http.StatusForbidden || wAllow.Code == http.StatusNotFound {
		t.Errorf("白名单命中时 pprof 准入不应拒绝，实际 %d", wAllow.Code)
	}
}

// TestRedactURL 覆盖 URL 脱敏的边界。
func TestRedactURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"http://loki:3100/loki/api/v1/push", "http://loki:3100/loki/api/v1/push"},
		{"https://u:p@loki:3100/push?token=abc", "https://loki:3100/push"},
		{"https://loki:3100/push?tenant=x#frag", "https://loki:3100/push"},
		{"not-a-url", "<已配置（非标准 URL，内容不展示）>"},
		{"  ", ""},
	}
	for _, c := range cases {
		if got := redactURL(c.in); got != c.want {
			t.Errorf("redactURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
