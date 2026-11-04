// ops_route_authz_test.go 钉的是 P1-A 止血面 + P2-A 匿名面。
//
// 背景（取证见 docs/decision-materials-2026-11-04.md §3、docs/projects-2026-11-05.md P1/P2）：
//   - `bd06090` / `1f9e481` 带来的 17 条运维扩展路由（云托管 DB 6 + NoSQL 5 + Apache 3 + ES 3）
//     上线时只有 `requireTenantContext`（还藏在 `dispatchKafkaTask` 里，kafka_ops.go:29-40），
//     等价于「登录 = 可执行」：生产 `requireAuth: true` 下 viewer 也能把 shell 脚本下发到设备；
//   - `GET /api/services`、`GET /api/traces` 四端点此前**无认证**返回 200 空结果。
//
// 本用例把三条腿钉住：无凭证 401、viewer/operator 403、admin 不被鉴权层拒；
// 再钉两件顺序性质——鉴权必须先于参数解析（否则匿名探测能拿到 400，白送路由存在性），
// 以及 HBase 表名/列族白名单（同文件其余分支用 shellQuote，这两个值进的是单引号段，必须校验）。
//
// 身份链复用 g1 的 seeded-role 夹具：真 NewServer + 真 seedRBAC + 真 HS256，
// 目的是让「权限点从目录走到 role 行再走到这道闸」整条链进断言，而不是只测 handler 会读权限串。
package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// opsAuthzRoutes 是被钉住的 17 条路由（生产里全部只接受 POST）。
// 数量断言见 TestOpsAuthzRouteListIsHonest——列表缩水就等于门禁空转。
var opsAuthzRoutes = []string{
	"/api/v1/db/aurora",
	"/api/v1/db/cloudsql",
	"/api/v1/db/azuresql",
	"/api/v1/db/polardb",
	"/api/v1/db/gaussdbdws",
	"/api/v1/db/tdsqlc",
	"/api/v1/db/hbase",
	"/api/v1/db/cassandra",
	"/api/v1/db/couchbase",
	"/api/v1/db/scylladb",
	"/api/v1/db/couchdb",
	"/api/v1/middleware/apache/superset",
	"/api/v1/middleware/apache/dolphinscheduler",
	"/api/v1/middleware/apache/seattunnel",
	"/api/v1/search/elasticsearch/node-drain",
	"/api/v1/search/elasticsearch/shard-relocate",
	"/api/v1/search/elasticsearch/diagnostic",
}

const opsAuthzBody = `{"deviceID":"ops-authz-device","action":"status"}`

// opsAuthzHBodyError 是 HBase 白名单命中时的错误文案（按内容断言，不绑状态码之外的下游结果）。
const opsAuthzHBodyError = "unsupported table or columnFamily name"

func newOpsAuthzServer(t *testing.T) (*Server, *http.ServeMux) {
	t.Helper()
	s, mux := newG1E2EServer(t)
	// 生产由 Start() 调这四个 register；测试直接挂到同一 mux 上，路径与注册方式逐字一致。
	s.registerCloudManagedDBRoutes(mux)
	s.registerNoSQLExtRoutes(mux)
	s.registerApacheExt2Routes(mux)
	s.registerElasticsearchExtRoutes(mux)
	return s, mux
}

// opsAuthzCall 与 g1Call 的差别只有一个：token 为空时**不发 Authorization 头**。
// g1Call 会发 "Bearer "，那走的是「有头但验签失败」的分支，证不了「匿名」。
func opsAuthzCall(mux *http.ServeMux, token, method, path, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-Tenant-ID", g1Tenant)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

func TestOpsAuthzRouteListIsHonest(t *testing.T) {
	if len(opsAuthzRoutes) != 17 {
		t.Fatalf("钉住的路由清单是 %d 条，不是 17 条——要么列表被改小（本门禁空转），要么产品面已变（判据要跟着重写）", len(opsAuthzRoutes))
	}
	s, mux := newOpsAuthzServer(t)
	// 扫描面自证：17 条必须真的挂在 mux 上可达。注册函数改名/漏挂时，
	// 下面各用例会因为 mux 404 而不是 401/403 判红，这条先把它说清楚。
	for _, p := range opsAuthzRoutes {
		if rec := opsAuthzCall(mux, "", http.MethodPost, p, opsAuthzBody); rec.Code == http.StatusNotFound {
			t.Errorf("POST %s = 404：路由没注册到 mux，后续鉴权断言全是空转", p)
		}
	}
	// 夹具自证：三个身份都拿得到 token，否则"403 判定"会退化成 401。
	for _, id := range []string{"g1-e2e-admin", "g1-e2e-operator", "g1-e2e-viewer"} {
		if g1Token(t, s, id) == "" {
			t.Fatalf("测试身份 %s 没拿到 token", id)
		}
	}
}

func TestOpsExtRoutesRejectAnonymous(t *testing.T) {
	_, mux := newOpsAuthzServer(t)
	for _, p := range opsAuthzRoutes {
		if rec := opsAuthzCall(mux, "", http.MethodPost, p, opsAuthzBody); rec.Code != http.StatusUnauthorized {
			t.Errorf("POST %s 无凭证 = %d, want 401；body=%s", p, rec.Code, rec.Body.String())
		}
	}
}

func TestOpsExtRoutesDenyNonAdminRoles(t *testing.T) {
	s, mux := newOpsAuthzServer(t)
	for _, who := range []struct{ id, label string }{
		{"g1-e2e-viewer", "viewer"},
		{"g1-e2e-operator", "operator"},
	} {
		tok := g1Token(t, s, who.id)
		for _, p := range opsAuthzRoutes {
			rec := opsAuthzCall(mux, tok, http.MethodPost, p, opsAuthzBody)
			if rec.Code != http.StatusForbidden {
				t.Errorf("%s POST %s = %d, want 403（system:write 不在该角色权限集）；body=%s", who.label, p, rec.Code, rec.Body.String())
			}
		}
	}
}

func TestOpsExtRoutesAdminPassesAuthzLayer(t *testing.T) {
	s, mux := newOpsAuthzServer(t)
	admin := g1Token(t, s, "g1-e2e-admin")
	for _, p := range opsAuthzRoutes {
		rec := opsAuthzCall(mux, admin, http.MethodPost, p, opsAuthzBody)
		if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
			t.Errorf("admin POST %s = %d：权限点没从目录走到 role 行；body=%s", p, rec.Code, rec.Body.String())
		}
	}
}

// TestOpsExtRoutesAuthzPrecedesBodyParsing 钉顺序：鉴权必须在解码/校验之前。
// 若哪天有人把 requireProd 挪到 decode 之后，匿名探测会拿到 400（暴露参数结构），
// 而不是 401——这类"顺序回归"用状态码才看得见，光看有没有 requireProd 是看不出来的。
func TestOpsExtRoutesAuthzPrecedesBodyParsing(t *testing.T) {
	_, mux := newOpsAuthzServer(t)
	for _, p := range opsAuthzRoutes {
		rec := opsAuthzCall(mux, "", http.MethodPost, p, `{not-json`)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("POST %s 非法 JSON + 无凭证 = %d, want 401（鉴权应先于解析）；body=%s", p, rec.Code, rec.Body.String())
		}
	}
}

// TestHBaseTableNameWhitelist 钉 `db_nosql_ext_ops.go:58-66` 那段拼接：
// table/cf 会进 `hbase shell <<< "create '<table>', '<cf>'"` 的单引号段，
// 一个 `'` 就跳出引号，外层双引号还会继续展开 `$()`/反引号。
func TestHBaseTableNameWhitelist(t *testing.T) {
	s, mux := newOpsAuthzServer(t)
	admin := g1Token(t, s, "g1-e2e-admin")

	hbasePayload := func(table, cf string) string {
		b, err := json.Marshal(map[string]string{
			"deviceID": "ops-authz-device", "action": "table", "table": table, "cf": cf,
		})
		if err != nil {
			t.Fatalf("构造载荷: %v", err)
		}
		return string(b)
	}

	// 合法名不得被校验层挡下（下游 202/500 属设备是否存在，不在本断言射程）。
	for _, good := range []struct{ table, cf string }{
		{"ns:table", "cf"},
		{"user_ops-2026", "Column-Family_1"},
	} {
		rec := opsAuthzCall(mux, admin, http.MethodPost, "/api/v1/db/hbase", hbasePayload(good.table, good.cf))
		if strings.Contains(rec.Body.String(), opsAuthzHBodyError) {
			t.Errorf("合法表名 %q/%q 被白名单误杀：code=%d body=%s", good.table, good.cf, rec.Code, rec.Body.String())
		}
	}

	// 恶意名必须在拼进脚本前判 400——判据用文案，别用"非 202"（202 也可能是别的分支）。
	for _, bad := range []string{
		"t'; touch /tmp/pwned #",
		"t$(id)",
		"t`id`",
		"t|sh",
		"t'\"x",
		"t\nrm -f",
		"" + strings.Repeat("a", 129),
	} {
		rec := opsAuthzCall(mux, admin, http.MethodPost, "/api/v1/db/hbase", hbasePayload(bad, "cf"))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), opsAuthzHBodyError) {
			t.Errorf("恶意表名 %q 未被白名单挡下：code=%d want 400 body=%s", bad, rec.Code, rec.Body.String())
		}
	}

	// 列族同规则：cf 带引号也一样拒。
	rec := opsAuthzCall(mux, admin, http.MethodPost, "/api/v1/db/hbase", hbasePayload("ns:table", "cf'; drop 'x"))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), opsAuthzHBodyError) {
		t.Errorf("恶意列族未被挡下：code=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestTraceEndpointsRequireAuth 钉 P2-A：四个 trace 端点从"匿名 200 空结果"收成"匿名 401"。
// 注册方式与生产一致（server_lifecycle.go:494-497 的四条 mux 模式）。
func TestTraceEndpointsRequireAuth(t *testing.T) {
	s, _ := newG1E2EServer(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/services", s.handleTraceServices)
	mux.HandleFunc("GET /api/services/", s.handleTraceRouting)
	mux.HandleFunc("GET /api/traces", s.handleListTraces)
	mux.HandleFunc("GET /api/traces/", s.handleTraceRouting)

	cases := []string{"/api/services", "/api/services/svc/operations", "/api/traces", "/api/traces/abc123"}
	for _, p := range cases {
		if rec := opsAuthzCall(mux, "", http.MethodGet, p, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s 无凭证 = %d, want 401；body=%s", p, rec.Code, rec.Body.String())
		}
	}
	// 登录（含 viewer）仍拿到原来的空结果 200——本批改的是匿名面，不改可读性语义。
	viewer := g1Token(t, s, "g1-e2e-viewer")
	for _, p := range cases {
		rec := opsAuthzCall(mux, viewer, http.MethodGet, p, "")
		if rec.Code != http.StatusOK {
			t.Errorf("viewer GET %s = %d, want 200（未接 Jaeger 的空态不能变成错误态）；body=%s", p, rec.Code, rec.Body.String())
		}
	}
	// 扫描面自证：若 handler 里的闸被摘掉，匿名会拿到 200——上面第一条就红，这里证"用例有射程"。
	rec := opsAuthzCall(mux, "", http.MethodGet, "/api/traces", "")
	if rec.Code == http.StatusOK {
		t.Errorf("匿名 GET /api/traces 仍是 200：requireTenantContext 不在了")
	}
}
