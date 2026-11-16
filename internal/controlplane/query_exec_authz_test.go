// query_exec_authz_test.go 钉住「查询入口会下发 shell 任务」的鉴权面。
//
// 背景（审计发现）：monitor/vector/olap/mongo/tsdb/graph 八条 query 端点都通过 createSystemTask
// 在设备上执行 shell，却只要求 device:read——viewer 因此拿到了远程执行面。
// 这些入口与 handleCreateTask 同属「下发任务」，口径统一为 task:write
// （server_tasks.go:114，operatorGroups 含 task ⇒ operator 仍可运维，viewer 无 task:write）。
//
// 断言两层：
//  1. viewer（持 device:read、无 task:write）逐条 403，且任务总数不变（没偷偷建任务）；
//  2. operator（持 task:write）逐条 200，且任务总数 +1（真的放行到建任务）。
//
// 身份链复用 g1 的 seeded-role 夹具：真 NewServer + 真 seedRBAC + 真 HS256，
// 让「权限点→role 行→闸」整条链进断言，而不是只测 handler 读权限串。
package controlplane

import (
	"net/http"
	"testing"
)

// queryExecRoutes 是被钉住的八条「查询即下发任务」路由（生产里全部只接受 POST）。
var queryExecRoutes = []struct {
	name string
	path string
	body string
}{
	{
		name: "zabbix-item",
		path: "/api/v1/monitor/zabbix/item",
		body: `{"deviceID":"dev-query-exec","host":"host1"}`,
	},
	{
		name: "nightingale-query",
		path: "/api/v1/monitor/nightingale/query",
		body: `{"deviceID":"dev-query-exec","query":"up","step":60}`,
	},
	{
		name: "skywalking-trace",
		path: "/api/v1/monitor/skywalking/trace",
		body: `{"deviceID":"dev-query-exec","traceID":"trace-1"}`,
	},
	{
		name: "vector-search",
		path: "/api/v1/vector/milvus/search",
		body: `{"deviceID":"dev-query-exec","collection":"coll_1","vector":[0.1,0.2],"topK":3}`,
	},
	{
		name: "olap-query",
		path: "/api/v1/olap/clickhouse/query",
		body: `{"deviceID":"dev-query-exec","query":"SELECT 1"}`,
	},
	{
		name: "mongo-query",
		path: "/api/v1/mongo/query",
		body: `{"deviceID":"dev-query-exec","database":"db1","collection":"coll1","filter":"{}","limit":10}`,
	},
	{
		name: "tsdb-query",
		path: "/api/v1/tsdb/tdengine/query",
		body: `{"deviceID":"dev-query-exec","database":"db1","query":"SELECT 1"}`,
	},
	{
		name: "graph-query",
		path: "/api/v1/graph/neo4j/cypher",
		body: `{"deviceID":"dev-query-exec","database":"graph1","query":"MATCH (n) RETURN n LIMIT 1"}`,
	},
}

// newQueryExecAuthzServer 用 g1 夹具（真 RBAC/真 JWT）并挂上六族路由。
func newQueryExecAuthzServer(t *testing.T) (*Server, *http.ServeMux) {
	t.Helper()
	s, mux := newG1E2EServer(t)
	s.registerMonitorRoutes(mux)
	s.registerVectorRoutes(mux)
	s.registerOLAPRoutes(mux)
	s.registerMongoRoutes(mux)
	s.registerTSDBRoutes(mux)
	s.registerGraphRoutes(mux)
	return s, mux
}

// queryExecTaskTotal 取本租户任务总数，用于证明「403 时没有建任务」。
func queryExecTaskTotal(t *testing.T, s *Server) int {
	t.Helper()
	_, total, err := s.store.ListTasksByPriority(g1Tenant, 0, 1000)
	if err != nil {
		t.Fatalf("ListTasksByPriority: %v", err)
	}
	return total
}

// TestQueryExecHandlersDenyViewer 只读身份不得拿到设备执行面。
func TestQueryExecHandlersDenyViewer(t *testing.T) {
	s, mux := newQueryExecAuthzServer(t)

	u := s.store.GetUser("g1-e2e-viewer")
	if u == nil {
		t.Fatal("viewer 夹具用户不存在")
	}
	perms := s.userPermissions(u)
	hasDeviceRead, hasTaskWrite := false, false
	for _, p := range perms {
		switch p {
		case "device:read":
			hasDeviceRead = true
		case "task:write":
			hasTaskWrite = true
		}
	}
	if !hasDeviceRead || hasTaskWrite {
		t.Fatalf("viewer 权限前置不成立：device:read=%v task:write=%v（应为 true/false）", hasDeviceRead, hasTaskWrite)
	}

	tok := g1Token(t, s, "g1-e2e-viewer")
	for _, rt := range queryExecRoutes {
		before := queryExecTaskTotal(t, s)
		rec := opsAuthzCall(mux, tok, http.MethodPost, rt.path, rt.body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s viewer POST = %d, want 403（task:write 不在 viewer 权限集）；body=%s", rt.name, rec.Code, rec.Body.String())
		}
		if after := queryExecTaskTotal(t, s); after != before {
			t.Errorf("%s viewer 被判 403 后任务数从 %d 变成 %d：鉴权前已建任务", rt.name, before, after)
		}
	}
}

// TestQueryExecHandlersAllowOperator 运维角色持 task:write，必须能继续用这些入口。
func TestQueryExecHandlersAllowOperator(t *testing.T) {
	s, mux := newQueryExecAuthzServer(t)
	tok := g1Token(t, s, "g1-e2e-operator")
	for _, rt := range queryExecRoutes {
		before := queryExecTaskTotal(t, s)
		rec := opsAuthzCall(mux, tok, http.MethodPost, rt.path, rt.body)
		if rec.Code != http.StatusOK {
			t.Errorf("%s operator POST = %d, want 200（task:write 应已在 operator 派生集）；body=%s", rt.name, rec.Code, rec.Body.String())
		}
		if after := queryExecTaskTotal(t, s); after != before+1 {
			t.Errorf("%s operator 放行后任务数 %d -> %d, want +1", rt.name, before, after)
		}
	}
}
