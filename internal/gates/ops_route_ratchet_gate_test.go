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
// 2026-11-06 口径更新（本条是"我自己的门禁算错了"的更正，不是新需求）：
// opencode 的 PR #4/#5 把闸装到了派发口 `dispatchKafkaTask` / `dispatchFastDFSTask`
// （`subsystemAuthorize(task:write)`），284 条存量运维写路由是**经派发口**收口的。
// 本门禁 11-05 版只跟 `s.handle*` 兄弟、不跟派发口，于是把已还的债继续记成欠账。
// 现在：① 授权特征集与 `scripts/authz-route-audit.py` 对齐；② 调用链跟进 `s.dispatch*`；
// ③ 深度上限 3（与审计脚本一致）。台账因此清零——不是"债务消失了"，是"债务改由派发口 +
// 本门禁共同兜住"：任何一条新路由要么自带 requireProd/requirePermission，要么经已鉴权派发口，
// 否则实测值 1 > 台账 0 立刻判红。
//
// 判据（缺一条就退化成摆设）：
//  1. 扫描面自证：解析到的路由注册数与"已鉴权写路由"数都必须过下限，
//     否则"台账里没有红"只代表正则坏了。
//  2. 逐文件对账：实际值 > 台账值 ⇒ 红（新增了未鉴权写路由）；
//     实际值 < 台账值 ⇒ 红（债务还了，台账必须当场下调，不许把余量留给下一条）。
//  3. 台账里的文件消失 ⇒ 红（改名/删除要同步台账）。
//  4. 变异反证：在内存里把所有授权特征抹掉重算，必须重新计成未鉴权——
//     证的是"闸被摘掉时本门禁会红"，不是"现在恰好没红"。含派发口那一跳。

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
	opsGuardedFloor  = 380 // 2026-11-06 派发口收口后实测 408 条写路由全部已鉴权；低于此说明特征失效
	opsUnresolvedCap = 40  // 解析不到 handler 的路由数上限：超过说明注册写法变了
	opsHardenedFiles = 4   // 自带 requireProd/requirePermission 的 ops 文件数下限（2026-11-06 实测 31）
)

// opsAuthzGuardRe 认定「这个 handler 过了产品级 RBAC」的特征。
// 特征集与 opencode 的只读审计脚本 `scripts/authz-route-audit.py` 对齐（2026-11-06 口径同步）：
// 那侧认 requirePermission / requireProd / subsystemAuthorize / authorizeByRoles / authorizeByAPIKey / .Authorize，
// 两边不一致会让"门禁判红"与"审计报告"给出两个数字——本仓同时有这两件，必须同口径。
// 刻意不含 requireTenantContext / requireTenantAgent：那两条只证「登录 + 租户」，
// 正是本门禁要抓的形态——17 条新运维路由当初挂的就是它。
var opsAuthzGuardRe = regexp.MustCompile(
	`s\.requireProd\(|s\.requirePermission\(|subsystemAuthorize\(|s\.authorizeByRoles\(|s\.authorizeByAPIKey\(|\.Authorize\(`)

var (
	opsRouteRegRe  = regexp.MustCompile(`mux\.Handle(?:Func)?\(\s*"([^"]+)"\s*,\s*([\w.]+)\s*\)`)
	opsFuncDeclRe  = regexp.MustCompile(`^func\s+(?:\([^)]*\)\s*)?([A-Za-z0-9_]+)\s*\(`)
	opsWriteVerbRe = regexp.MustCompile(`http\.Method(Post|Put|Delete|Patch)`)
	// opsCalleeRe 抓同包转手。两类都要跟：
	//   - `s.handle*`：分派型 handler 转给已鉴权的兄弟（如 handleHarborRouting）；
	//   - `s.dispatch*` / `s.createSystemTask`：**派发口**。2026-11-05 PR #4 把闸装在
	//     `dispatchKafkaTask`（`subsystemAuthorize(task:write)`）上，415+ 条运维路由都经它，
	//     不跟就等于把已还的债记成欠账（我 11-05 的版本正是这个错，见 docs/CLAIMS.md 03:44 NOTE）。
	opsCalleeRe = regexp.MustCompile(`s\.(handle[A-Za-z0-9_]+|dispatch[A-Za-z0-9_]+|createSystemTask)\(`)
)

// opsGuardDepthCap 与审计脚本的「深链 ≤3」对齐：再深的间接不该被当成闸。
const opsGuardDepthCap = 3

// opsHandlerGuarded 判定 handler 是否（直接或经同包转手/派发口，深度 ≤3）过了产品级 RBAC。
// 已知宽松面：只要**任一**被调方带闸就算整条已鉴权，而被调方里可能还有未鉴权分支。
// 这一格宽松是刻意的——假阳性会把台账写成噪声，棘轮就没人愿意维护；
// 而"闸被摘掉仍然判红"由 TestOpsRatchetJudgeSeesMissingGuard 专门守（含派发口那一跳）。
func opsHandlerGuarded(name string, bodies map[string]string, seen map[string]bool, depth int) bool {
	if depth > opsGuardDepthCap || seen[name] {
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
		if opsHandlerGuarded(callee[1], bodies, seen, depth+1) {
			return true
		}
	}
	return false
}

// opsUnguardedWriteRoutes 是未鉴权写路由的债务台账：键 = 文件名，值 = 该文件当前未过 RBAC 的
// 写路由条数。**只许下调，不许上调，也不许在下调后留着空余量**。
//
// 2026-11-06 清零：opencode 的 PR #4/#5 把 `subsystemAuthorize(task:write)` 装到派发口
// （`dispatchKafkaTask` / `dispatchFastDFSTask`），284 条存量运维写路由经此收口；本门禁同步
// 跟进派发口后实测 408 条注册 / 408 条已鉴权写路由 / 0 条未鉴权。清零即"欠账还完、从此不许再欠"：
// 任何新增写路由若不自带权限点、又不经已鉴权派发口，实测 1 > 台账 0 会立刻判红。
//
// 维护方式：空台账下若出现未鉴权写路由，测试会把待填行打到日志（`台账待填：...`）。
// 只有 owner 明确裁决"这条先不做鉴权"时才允许入账，且必须在本注释里写明原因与期限。
var opsUnguardedWriteRoutes = map[string]int{}

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
		if _, ok := bodies[r.handler]; !ok || !opsHandlerGuarded(r.handler, bodies, map[string]bool{}, 0) {
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

// TestOpsRatchetJudgeSeesMissingGuard 是变异反证：把授权特征在内存里抹掉重算，
// 台账口径必须立刻把相关路由重新记成未鉴权。没有这条，"全绿"可能只代表判据看不见闸的存在。
// 两格分别钉住两条闸：
//   - P1-A 四文件自带 `requireProd`（摘掉后必须回落到 6/5/3/3，合计 17）；
//   - `pulsar_ops.go` 自己没有权限点，全靠派发口的 `subsystemAuthorize`（摘掉后必须全数回落）。
func TestOpsRatchetJudgeSeesMissingGuard(t *testing.T) {
	root := repoRoot(t)
	dir := filepath.Join(root, opsDir)
	bodies := opsHandlerBodies(t, dir)

	// 抹掉所有授权特征：等价于"有人把这些调用删了"。
	// 注意必须连 `subsystemAuthorize` 一起抹——P1-A 的 handler 同时经派发口，
	// 只抹 requireProd 的话派发口仍会把它判成已鉴权，变异就失效了。
	stripped := map[string]string{}
	for k, v := range bodies {
		v = strings.ReplaceAll(v, "s.requireProd(", "s.disabledRequireProd(")
		v = strings.ReplaceAll(v, "s.requirePermission(", "s.disabledRequirePermission(")
		v = strings.ReplaceAll(v, "s.subsystemAuthorize(", "s.disabledSubsystemAuthorize(")
		v = strings.ReplaceAll(v, "s.authorizeByRoles(", "s.disabledAuthorizeByRoles(")
		v = strings.ReplaceAll(v, "s.authorizeByAPIKey(", "s.disabledAuthorizeByAPIKey(")
		stripped[k] = v
	}

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
		_, after, _ := opsUnguardedInBody(body, stripped)
		if after != c.wantWrite {
			t.Errorf("%s 抹掉授权特征后未鉴权写路由 = %d, want %d——判据对'闸被摘掉'不敏感，本门禁会自我失明", c.file, after, c.wantWrite)
		}
		total += after
	}
	if total != 17 {
		t.Errorf("变异合计 %d 条，不等于 17 条止血面——写路由判定射程与 P1-A 清单对不上", total)
	}

	// 派发口那一跳：该文件没有任何自带权限点，现状全绿只能靠 dispatchKafkaTask 的
	// subsystemAuthorize。摘掉派发口的闸后必须整片回落，否则"派发口收口"是假的。
	const dispatchOnly = "pulsar_ops.go"
	data, err := os.ReadFile(filepath.Join(dir, dispatchOnly))
	if err != nil {
		t.Fatalf("读 %s: %v", dispatchOnly, err)
	}
	body := string(data)
	routes, unguarded, _ := opsUnguardedInBody(body, bodies)
	if routes < 10 {
		t.Fatalf("%s 只解析到 %d 条路由——派发口变异样本太小，证不了什么", dispatchOnly, routes)
	}
	if unguarded != 0 {
		t.Errorf("%s 现状有 %d 条未鉴权写路由（期望 0：全部经已鉴权派发口）", dispatchOnly, unguarded)
	}
	if _, after, _ := opsUnguardedInBody(body, stripped); after != routes {
		t.Errorf("%s 摘掉派发口授权后未鉴权写路由 = %d, want %d（全数回落）——派发口的闸不在判据射程内", dispatchOnly, after, routes)
	}
}
