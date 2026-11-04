package gates

// ops_route_ratchet_gate_test.go 守的是「ops 路由的 RBAC 覆盖只许涨不许跌」。
//
// 起因（2026-11-04 实测，取证见 docs/decision-materials-2026-11-04.md §3 与
// docs/projects-2026-11-05.md P1）：`bd06090` / `1f9e481` 新增 17 条运维扩展路由
// （云托管 DB 6 + NoSQL 5 + Apache 3 + ES 3），一条权限检查都没有；它们只经
// `dispatchKafkaTask → requireTenantContext`，而后者只保证「有可验证凭证 + 租户一致」。
// 生产 `requireAuth: true` 下等价于**任意已登录用户（含只读 viewer）可把 shell 脚本下发到设备**。
// 同形态在存量里不是孤例：`internal/controlplane/*ops*.go` 94 个文件里 67 个 0 检查。
//
// 为什么是"棘轮 + 台账"而不是"必须全绿"：按域补权限（谁该拿到 db:execute / os:execute）
// 属产品语义，一次性把 300 条写路由全钉成 admin-only 会砍掉值班角色的作业面
// （`system` 组不在 operatorGroups，见 internal/store/model/perm.go 的派生注释）。
// 所以本门禁只要求：**未鉴权写路由数不回增**，且每降一步台账必须跟着下调——
// 「新路由该带闸但忘了带」从静默通过变成立刻判红。
//
// 判据（缺一条就退化成摆设）：
//  1. 扫描面自证：解析到的路由注册数与"已鉴权写路由"数都必须过下限，
//     否则"台账里没有红"只代表正则坏了。
//  2. 逐文件对账：实际值 > 台账值 ⇒ 红（新增了未鉴权写路由）；
//     实际值 < 台账值 ⇒ 红（债务还了，台账必须当场下调，不许把余量留给下一条）。
//  3. 台账里的文件消失 ⇒ 红（改名/删除要同步台账）。
//  4. 变异反证：在内存里把已加固文件的 requireProd 抹掉重算，必须重新计成未鉴权——
//     证的是"闸被摘掉时本门禁会红"，不是"现在恰好没红"。

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	opsDir           = "internal/controlplane"
	opsScanFloor     = 380 // 实测 408 条可解析注册（另有非字面量写法够不着），留 ~7% 余量
	opsGuardedFloor  = 100 // 实测 122 条已鉴权写路由；低于此说明闸识别特征失效
	opsUnresolvedCap = 40  // 解析不到 handler 的路由数上限：超过说明注册写法变了
	opsHardenedFiles = 4   // 2026-11-05 P1-A 止血批的文件数下限（特征对照拍）
)

// opsAuthzGuardRe 认定「这个 handler 过了产品级 RBAC」的特征。
// 刻意不含 requireTenantContext / requireTenantAgent：那两条只证「登录 + 租户」，
// 正是本门禁要抓的形态——17 条新运维路由挂的就是它。
var opsAuthzGuardRe = regexp.MustCompile(`s\.requireProd\(|s\.requirePermission\(|subsystemAuthorize\(`)

var (
	opsRouteRegRe  = regexp.MustCompile(`mux\.Handle(?:Func)?\(\s*"([^"]+)"\s*,\s*([\w.]+)\s*\)`)
	opsFuncDeclRe  = regexp.MustCompile(`^func\s+(?:\([^)]*\)\s*)?([A-Za-z0-9_]+)\s*\(`)
	opsWriteVerbRe = regexp.MustCompile(`http\.Method(Post|Put|Delete|Patch)`)
	// opsCalleeRe 抓同包转手：分派型 handler 自己不鉴权，转给已鉴权的兄弟（如 handleHarborRouting）。
	opsCalleeRe = regexp.MustCompile(`s\.(handle[A-Za-z0-9_]+)\(`)
)

// opsHandlerGuarded 判定 handler 是否（直接或经同包转手）过了产品级 RBAC。
// 已知宽松面：分派型 handler 只要**任一**兄弟带闸就算整条已鉴权，而兄弟里可能还有未鉴权分支。
// 这一格宽松是刻意的——假阳性会把台账写成噪声，棘轮就没人愿意维护；
// 而"闸被摘掉仍然判红"由 TestOpsRatchetJudgeSeesMissingGuard 专门守。
func opsHandlerGuarded(name string, bodies map[string]string, seen map[string]bool) bool {
	if seen[name] {
		return false
	}
	seen[name] = true
	body, ok := bodies[name]
	if !ok {
		return false
	}
	if opsAuthzGuardRe.MatchString(body) {
		return true
	}
	for _, callee := range opsCalleeRe.FindAllStringSubmatch(body, -1) {
		if opsHandlerGuarded(callee[1], bodies, seen) {
			return true
		}
	}
	return false
}

// opsUnguardedWriteRoutes 是未鉴权写路由的债务台账：键 = 文件名，值 = 该文件当前未过 RBAC 的
// 写路由条数。**只许下调，不许上调，也不许在下调后留着空余量**。
//
// 这些条目的共同成因（不在每行重复写一遍）：ops 家族上线时只做了租户校验，没做角色校验；
// 收口方式是 P1-B2（按域补 requireProd，权限矩阵待 owner 裁），见 docs/projects-2026-11-05.md §1。
// 2026-11-05 P1-A 止血批已把 `bd06090`/`1f9e481` 的 17 条移出台账（改判 admin-only），
// 台账值即那之后的现测值（合计 284 条未鉴权写路由，扫描面 408 条注册 / 124 条已鉴权写路由）。
var opsUnguardedWriteRoutes = map[string]int{
	"abac_ops.go":               1,
	"activemq_ext_ops.go":       5,
	"activemq_ops.go":           3,
	"apache_ext_ops.go":         7,
	"apache_ops.go":             5,
	"automation_ops.go":         1,
	"cache_ops.go":              11,
	"capacity_ops.go":           1,
	"cicd_ops.go":               4,
	"container_ops.go":          2,
	"data_ops.go":               3,
	"db_backup_ops.go":          1,
	"db_extended_ops.go":        8,
	"db_misc_ext_ops.go":        7,
	"db_perf_ops.go":            1,
	"db_replication_ops.go":     1,
	"distfs_ops.go":             1,
	"docker_ops.go":             6,
	"domestic_ops.go":           7,
	"elk_ext_ops.go":            11,
	"elk_ops.go":                13,
	"gateway_mgmt_ops.go":       1,
	"git_unix_ops.go":           12,
	"gpu_ext_ops.go":            13,
	"graph_ext_ops.go":          7,
	"ha_ops.go":                 1,
	"hardware_ext_ops.go":       6,
	"hardware_ops.go":           3,
	"iac_ops.go":                1,
	"identity_ops.go":           1,
	"k8s_gateway_ops.go":        1,
	"kafka_advanced_ops.go":     4,
	"kafka_final_ops.go":        6,
	"kafka_ops.go":              6,
	"kafka_ops_ext.go":          6,
	"kafka_scale_ops.go":        6,
	"microservice_ops.go":       1,
	"mq_ext_ops.go":             6,
	"netdiag_ops.go":            1,
	"netsec_ext_ops.go":         6,
	"network_ext_ops.go":        2,
	"network_vendor_ext_ops.go": 5,
	"observability_ops.go":      1,
	"olap_ext_ops.go":           10,
	"oncall_ops.go":             1,
	"oracle_ops.go":             7,
	"os_ext_ops.go":             6,
	"os_tuning_ops.go":          9,
	"pulsar_ops.go":             13,
	"rabbitmq_ext_ops.go":       7,
	"rabbitmq_ops.go":           9,
	"raid_ext_ops.go":           1,
	"redis_cache_ops.go":        1,
	"rocketmq_ext_ops.go":       6,
	"script_compile_ops.go":     4,
	"security_ext_ops.go":       6,
	"servicediscovery_ops.go":   1,
	"servicemesh_ops.go":        1,
	"statuspage_ops.go":         1,
	"synthetic_ops.go":          1,
	"vector_ext_ops.go":         5,
}

type opsRoute struct {
	pattern string
	handler string
}

// opsHandlerBodies 建 handler 名 → 函数体 的索引，供"这条路由是不是写操作"的判定用。
// 覆盖整个 internal/controlplane（非 _test.go）：跨文件注册的 handler 也要能解析。
func opsHandlerBodies(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读 %s: %v", dir, err)
	}
	bodies := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("读 %s: %v", e.Name(), err)
		}
		lines := strings.Split(string(data), "\n")
		for i, l := range lines {
			m := opsFuncDeclRe.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			body := strings.Join(lines[i+1:], "\n")
			if end := strings.Index(body, "\n}\n"); end >= 0 {
				body = body[:end]
			}
			// 同名函数（不同 receiver）取第一次出现：ops 家族里 handler 名唯一，
			// 真出现重名时本门禁会把两份 body 混起来，方向是"更容易判成已鉴权"，
			// 由 opsUnresolvedCap 与台账对账一起兜。
			if _, seen := bodies[m[1]]; !seen {
				bodies[m[1]] = body
			}
		}
	}
	return bodies
}

// opsParseRoutes 从一个文件里抽出全部路由注册。
func opsParseRoutes(body string) []opsRoute {
	var out []opsRoute
	for _, m := range opsRouteRegRe.FindAllStringSubmatch(body, -1) {
		ref := m[2]
		if i := strings.LastIndex(ref, "."); i >= 0 {
			ref = ref[i+1:]
		}
		out = append(out, opsRoute{pattern: m[1], handler: ref})
	}
	return out
}

// opsIsWriteRoute 判定路由是否写操作。
// 带方法前缀的按前缀判；Go 1.22 之前的裸路径注册会接受任意方法，只能看 handler 自己拒什么——
// 解析不到 handler 时**按写操作处理**（宁可多记一笔债，不可漏记）。
func opsIsWriteRoute(r opsRoute, bodies map[string]string, unresolved *int) bool {
	for _, v := range []string{"POST ", "PUT ", "DELETE ", "PATCH "} {
		if strings.HasPrefix(r.pattern, v) {
			return true
		}
	}
	if strings.HasPrefix(r.pattern, "GET ") || strings.HasPrefix(r.pattern, "HEAD ") {
		return false
	}
	body, ok := bodies[r.handler]
	if !ok {
		*unresolved++
		return true
	}
	if opsWriteVerbRe.MatchString(body) {
		return true
	}
	return strings.Contains(body, "MethodPost") || strings.Contains(body, "MethodDelete")
}

// opsUnguardedInBody 计算单份源码里的未鉴权写路由数（不碰文件系统，便于变异反证）。
func opsUnguardedInBody(body string, bodies map[string]string) (routes, unguarded, unresolved int) {
	for _, r := range opsParseRoutes(body) {
		routes++
		if !opsIsWriteRoute(r, bodies, &unresolved) {
			continue
		}
		if _, ok := bodies[r.handler]; !ok || !opsHandlerGuarded(r.handler, bodies, map[string]bool{}) {
			unguarded++
		}
	}
	return routes, unguarded, unresolved
}

func TestOpsRouteRatchetGate(t *testing.T) {
	root := repoRoot(t)
	dir := filepath.Join(root, opsDir)
	bodies := opsHandlerBodies(t, dir)
	if len(bodies) == 0 {
		t.Fatalf("%s 没解析出任何函数体——扫描面为空，本门禁会空转判绿", opsDir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读 %s: %v", dir, err)
	}
	var (
		totalRoutes     int
		totalUnguarded  int
		totalGuarded    int
		totalUnresolved int
		hardenedFiles   int
		scanned         = map[string]bool{}
		actual          = map[string]int{}
	)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.Contains(name, "ops") || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		scanned[name] = true
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("读 %s: %v", name, err)
		}
		body := string(data)
		routes, unguarded, unresolved := opsUnguardedInBody(body, bodies)
		totalRoutes += routes
		totalUnguarded += unguarded
		totalUnresolved += unresolved
		hardened := strings.Contains(body, "s.requireProd(") || strings.Contains(body, "s.requirePermission(")
		if hardened {
			hardenedFiles++
		}
		totalGuarded += routes - unguarded
		actual[name] = unguarded
		if unguarded > opsUnguardedWriteRoutes[name] {
			t.Errorf("%s 有 %d 条未过 RBAC 的写路由，台账记的是 %d 条——新增写路由必须带 s.requireProd/s.requirePermission，"+
				"或按域补权限（docs/projects-2026-11-05.md P1-B2）；确认无误才允许把台账调高并写明原因", name, unguarded, opsUnguardedWriteRoutes[name])
		} else if unguarded < opsUnguardedWriteRoutes[name] {
			t.Errorf("%s 台账 %d 条、实测 %d 条：债已经还了，台账必须当场下调到 %d——留着余量等于给下一条未鉴权路由发通行证",
				name, opsUnguardedWriteRoutes[name], unguarded, unguarded)
		}
	}

	// 判据 1：扫描面自证。
	if totalRoutes < opsScanFloor {
		t.Errorf("只解析到 %d 条 ops 路由注册（下限 %d）——注册写法或正则变了，本门禁正在空转", totalRoutes, opsScanFloor)
	}
	if totalGuarded < opsGuardedFloor {
		t.Errorf("只认出 %d 条已鉴权写路由（下限 %d）——opsAuthzGuardRe 特征失效，'0 违规'不可信", totalGuarded, opsGuardedFloor)
	}
	if hardenedFiles < opsHardenedFiles {
		t.Errorf("只识别到 %d 个含 RBAC 的 ops 文件（下限 %d）——判定特征或文件命名变了", hardenedFiles, opsHardenedFiles)
	}
	if totalUnresolved > opsUnresolvedCap {
		t.Errorf("%d 条路由解析不到 handler 实现（上限 %d）——handler 跨包搬走了或注册写法变了，写操作判定不可信",
			totalUnresolved, opsUnresolvedCap)
	}

	// 判据 2：台账里的文件必须仍在扫描面里。
	for name := range opsUnguardedWriteRoutes {
		if !scanned[name] {
			t.Errorf("台账里的 %s 已不在扫描面（改名/删除/不再含 ops 字样？）——台账要同步清理", name)
		}
	}

	// 台账为空时把现测值打出来：第一次落地/大改后重建台账不用另写脚本。
	if len(opsUnguardedWriteRoutes) == 0 && totalUnguarded > 0 {
		var names []string
		for n, v := range actual {
			if v > 0 {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		for _, n := range names {
			t.Logf("台账待填：\"%s\": %d,", n, actual[n])
		}
	}

	// 判据 3：P1-A 止血批的四份文件必须一条未鉴权写路由都不剩。
	for _, name := range []string{
		"db_cloud_managed_ops.go", "db_nosql_ext_ops.go", "apache_ext_ops2.go", "elasticsearch_ops_ext.go",
	} {
		if n := actual[name]; n != 0 {
			t.Errorf("%s 仍有 %d 条未鉴权写路由；2026-11-05 P1-A 已把这 17 条钉成 system:write，回退就是红", name, n)
		}
	}

	t.Logf("ops 路由扫描面：注册 %d 条 / 已鉴权写路由 %d 条 / 未鉴权写路由 %d 条 / handler 未解析 %d 条 / 含 RBAC 文件 %d 个 / 台账 %d 个文件",
		totalRoutes, totalGuarded, totalUnguarded, totalUnresolved, hardenedFiles, len(opsUnguardedWriteRoutes))
}

// TestOpsRatchetJudgeSeesMissingGuard 是变异反证：把已加固文件的 requireProd 在内存里抹掉重算，
// 台账口径必须立刻把它们重新记成未鉴权。没有这条，"全绿"可能只代表判据看不见闸的存在。
func TestOpsRatchetJudgeSeesMissingGuard(t *testing.T) {
	root := repoRoot(t)
	dir := filepath.Join(root, opsDir)
	bodies := opsHandlerBodies(t, dir)

	cases := []struct {
		file      string
		wantWrite int // 该文件的写路由条数（也是抹掉闸后应回落到的未鉴权数）
	}{
		{"db_cloud_managed_ops.go", 6},
		{"db_nosql_ext_ops.go", 5},
		{"apache_ext_ops2.go", 3},
		{"elasticsearch_ops_ext.go", 3},
	}
	total := 0
	for _, c := range cases {
		data, err := os.ReadFile(filepath.Join(dir, c.file))
		if err != nil {
			t.Fatalf("读 %s: %v", c.file, err)
		}
		body := string(data)
		routes, unguarded, _ := opsUnguardedInBody(body, bodies)
		if routes == 0 {
			t.Errorf("%s 一条路由都没解析到——特征正则失效", c.file)
		}
		if unguarded != 0 {
			t.Errorf("%s 现状就有 %d 条未鉴权写路由（期望 0，P1-A 已止血）", c.file, unguarded)
		}
		// 抹掉闸：把 handler 索引里的鉴权调用换成不会命中的名字，等价于"有人删了鉴权"。
		stripped := map[string]string{}
		for k, v := range bodies {
			v = strings.ReplaceAll(v, "s.requireProd(", "s.disabledRequireProd(")
			v = strings.ReplaceAll(v, "s.requirePermission(", "s.disabledRequirePermission(")
			stripped[k] = v
		}
		_, after, _ := opsUnguardedInBody(body, stripped)
		if after != c.wantWrite {
			t.Errorf("%s 抹掉 requireProd 后未鉴权写路由 = %d, want %d——判据对'闸被摘掉'不敏感，本门禁会自我失明", c.file, after, c.wantWrite)
		}
		total += after
	}
	if total != 17 {
		t.Errorf("变异合计 %d 条，不等于 17 条止血面——写路由判定射程与 P1-A 清单对不上", total)
	}
}
