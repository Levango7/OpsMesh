// sql_devices_agents_scan_test.go — agents 读侧必须扛得住可空列（TD-74，与 devices 读侧同源）。
//
// 分两层，刻意不合并：
//  1. 静态对账（任何环境都跑）：SELECT 列清单与 scanAgentRow 的 Scan 目标个数一一对应；
//     且除 agent_id（主键）外的 agents 可空列在读侧都落到 sql.Null* 目标上——
//     否则任一列 NULL 就让整行读不出来（Agent() 返回 nil，Agents() 把 agent 从清单抹掉）。
//  2. 真库回归（有 OPSMESH_TEST_MYSQL_DSN 才跑）：写入全 NULL 的行再读回来。
//     只有真驱动会给出 `converting NULL to string is unsupported`——用假 scanner 复现它
//     等于自己重写一遍 database/sql 的转换规则，测的是想象而不是驱动。
//
// 列清单从 sql_devices.go 源码里正则抓取，不在本文件另抄一份常量：
// 抄一份就等于给「测试与被测代码各说各话」留了个洞，两者一漂移测试还照样绿。
package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// agentsSelectRe 抓 Agents/Agent 共用的那条 SELECT 的列清单（`load` 带反引号，故字符类放行反引号）。
var agentsSelectRe = regexp.MustCompile("SELECT (agent_id, hostname.*?) FROM agents")

// TestAgentsSelectColumnsMatchScanTargets 钉住「列清单 ↔ Scan 目标」个数对齐。
// 新增列时若忘了补 scanAgentRow 的目标，这里立刻变红，而不是等到某台机器上少一个 agent。
func TestAgentsSelectColumnsMatchScanTargets(t *testing.T) {
	src := readStoreFile(t, "sql_devices.go")

	lists := agentsSelectRe.FindAllStringSubmatch(src, -1)
	if len(lists) == 0 {
		t.Fatalf("未在 sql_devices.go 里抓到 agents 的 SELECT 列清单（正则失效会让本测试空跑）")
	}
	// Agents 与 Agent 各一条 SELECT，两者列清单必须一致——它们共用 scanAgentRow。
	var want string
	for i, m := range lists {
		if i == 0 {
			want = strings.TrimSpace(m[1])
			continue
		}
		if got := strings.TrimSpace(m[1]); got != want {
			t.Errorf("Agents 与 Agent 的 SELECT 列清单不一致：\n  %q\n  %q", want, got)
		}
	}

	cols := splitAgentColumns(want)
	block := extractBetweenGo(src, "func scanAgentRow(", "\n}")
	targets := scanTargetsIn(block)
	if len(targets) == 0 {
		t.Fatalf("未在 scanAgentRow 里抓到 Scan 目标（函数结构变了会让本测试空跑）")
	}
	if len(cols) != len(targets) {
		t.Errorf("agents SELECT 列数 %d 与 scanAgentRow 的 Scan 目标数 %d 不一致（新增列必须同时补目标）：cols=%v targets=%v",
			len(cols), len(targets), cols, targets)
	}
}

// TestAgentsNullableColumnsScanIntoNullTargets 把 schema 的可空性与读侧目标类型对账。
// 判据取 Scan 目标变量的声明（sql.Null*）而不是 proto.AgentInfo 字段类型：
// AgentInfo 本来就是 string/int，用结构体当「读侧是否安全」的判据会把缺陷本身当成事实。
func TestAgentsNullableColumnsScanIntoNullTargets(t *testing.T) {
	nullable := nullableMigrationColumns(t, "agents")
	if len(nullable) < 5 {
		t.Fatalf("只解析出 %d 个 agents 可空列，schema 解析已失效（本测试会空跑）：%v", len(nullable), nullable)
	}
	block := extractBetweenGo(readStoreFile(t, "sql_devices.go"), "func scanAgentRow(", "\n}")
	nullTargets := nullDeclaredVars(block)
	for _, name := range nullable {
		v := goFieldName(name)
		if !nullTargets[v] {
			t.Errorf("agents 列 %s 在 schema 里可空，但 scanAgentRow 的 %s 不是 sql.Null* 目标（NULL 会让整行读不出来）", name, v)
		}
	}
}

// nullDeclaredVars 收集 block 内所有声明为 sql.Null* 的变量名。
// 必须展开 Go 的分组声明（var a, b, c sql.NullString 只在 c 后写了类型），
// 否则 `var hostname, segment sql.NullString` 里的 hostname 会被漏判成标量。
func nullDeclaredVars(block string) map[string]bool {
	out := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\s*var\s+([^\n=]+?)\s+(sql\.Null\w+)\s*$`).FindAllStringSubmatch(block, -1) {
		for _, name := range strings.Split(m[1], ",") {
			if name = strings.TrimSpace(name); name != "" {
				out[name] = true
			}
		}
	}
	return out
}

// TestAgentsRealDBNullableColumnsRoundTrip 真库回归：全 NULL 的一行必须读得回来。
//
// 本机跑法用一次性容器（出厂栈的 MySQL root 只允许容器内连接，宿主直连必 1045）：
//
//	docker run -d --name opsmesh-agents-nulltest-mysql -e MYSQL_ROOT_PASSWORD=sqltest-pw \
//	  -e MYSQL_ROOT_HOST=% -p 13319:3306 mysql:8.0
//	OPSMESH_TEST_MYSQL_DSN='root:sqltest-pw@tcp(127.0.0.1:13319)/opsmesh?parseTime=true' \
//	  go test ./internal/store/ -run TestAgentsRealDB -count=1
func TestAgentsRealDBNullableColumnsRoundTrip(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("未设置 OPSMESH_TEST_MYSQL_DSN：跳过真库回归（可空列吞行只有真驱动能复现）")
	}
	adminDSN := stripDBName(dsn)
	dbName := "test_agentsnull_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	adminDB, err := sql.Open("mysql", adminDSN)
	if err != nil {
		t.Fatalf("open admin db: %v", err)
	}
	if _, err := adminDB.Exec("CREATE DATABASE " + dbName); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create temp db %s: %v", dbName, err)
	}
	_ = adminDB.Close()
	t.Cleanup(func() { dropTestDB(adminDSN, dbName) })

	s, err := NewSQLStore(withDBName(dsn, dbName), "", "")
	if err != nil {
		t.Fatalf("NewSQLStore: %v", err)
	}
	defer func() { _ = s.DB().Close() }()

	const id = "null-agent-roundtrip"
	if _, err := s.db.Exec(`DELETE FROM agents WHERE agent_id=?`, id); err != nil {
		t.Fatalf("清理旧行: %v", err)
	}
	// 只写必填主键，其余留给驱动落成 NULL——真实世界的补列/legacy 行正是这个形态。
	if _, err := s.db.Exec(`INSERT INTO agents (agent_id) VALUES (?)`, id); err != nil {
		t.Fatalf("插入全 NULL 行: %v", err)
	}
	t.Cleanup(func() { _, _ = s.db.Exec(`DELETE FROM agents WHERE agent_id=?`, id) })

	// 单行读：可空列不得让 Agent() 返回 nil（那正是「agent 凭空消失」的现场）。
	got := s.Agent(id)
	if got == nil {
		t.Fatalf("Agent(%s) 返回 nil：agents 可空列把整行吞掉了（TD-74 同源缺陷）", id)
	}
	if got.AgentID != id {
		t.Errorf("读回的 AgentID 不对: %+v", got)
	}
	if got.Hostname != "" || got.Segment != "" || got.Status != "" || got.Addr != "" {
		t.Errorf("NULL 字符串列应读成空串: hostname=%q segment=%q status=%q addr=%q",
			got.Hostname, got.Segment, got.Status, got.Addr)
	}
	if got.GRPCPort != 0 || got.MetricsPort != 0 || got.Load != 0 {
		t.Errorf("NULL 整型列应读成 0: grpc=%d metrics=%d load=%d", got.GRPCPort, got.MetricsPort, got.Load)
	}
	if !got.LastSeen.IsZero() {
		t.Errorf("NULL last_seen 应读成零值时间: %v", got.LastSeen)
	}

	// 列表读：同一行也必须出现在清单里（Agents 侧同样不得丢行）。
	found := false
	for _, a := range s.Agents("") {
		if a.AgentID == id {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Agents() 没有返回刚插入的全 NULL 行（列表侧同样丢行）")
	}

	// 写读闭合：Register 走 upsert 写全列，紧接着要能按 ID 读回。
	const upserted = "upsert-agent-roundtrip"
	s.Register(&proto.AgentInfo{AgentID: upserted, Hostname: "h1", GRPCPort: 9090, MetricsPort: 9091, Status: "online"})
	t.Cleanup(func() { _, _ = s.db.Exec(`DELETE FROM agents WHERE agent_id=?`, upserted) })
	a := s.Agent(upserted)
	if a == nil {
		t.Fatalf("Register 写入的 agent 读不回来（upsert 后仍有列落成 NULL）")
	}
	if a.Hostname != "h1" || a.GRPCPort != 9090 || a.Status != "online" {
		t.Errorf("Register 往返字段不对: %+v", a)
	}
}

// —— 以下为静态对账小工具 ——

// splitAgentColumns 把 SELECT 列清单切成列名（去掉反引号与空白）。
func splitAgentColumns(list string) []string {
	parts := strings.Split(list, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.Trim(strings.TrimSpace(p), "`"))
	}
	return out
}

// scanTargetsIn 抓取 block 内第一个 Scan(...) 的 &ident 目标列表（按顶层逗号切分）。
func scanTargetsIn(block string) []string {
	i := strings.Index(block, ".Scan(")
	if i < 0 {
		return nil
	}
	rest := block[i+len(".Scan("):]
	depth, end := 1, -1
	for j := 0; j < len(rest); j++ {
		switch rest[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				end = j
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return nil
	}
	var out []string
	for _, a := range strings.Split(rest[:end], ",") {
		if a = strings.TrimSpace(a); strings.HasPrefix(a, "&") {
			out = append(out, a)
		}
	}
	return out
}

// extractBetweenGo 取 start 与其后的首个独立 "\n}" 之间的正文（函数体）。
func extractBetweenGo(src, start, end string) string {
	i := strings.Index(src, start)
	if i < 0 {
		return ""
	}
	rest := src[i+len(start):]
	if j := strings.Index(rest, end); j >= 0 {
		return rest[:j]
	}
	return rest
}

// nullableMigrationColumns 从 001_initial.sql 的建表块里取「没写 NOT NULL」的列。
// 单一来源＝迁移文件（与 CI 的 integration job 同源）。
func nullableMigrationColumns(t *testing.T, table string) []string {
	t.Helper()
	src := readStoreFile(t, filepath.Join("migrations", "001_initial.sql"))
	re := regexp.MustCompile(`(?s)CREATE TABLE IF NOT EXISTS ` + table + ` \((.*?)\n\)`)
	m := re.FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("在 001_initial.sql 里找不到 %s 的建表块", table)
	}
	var out []string
	for _, line := range strings.Split(m[1], "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ","))
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		if regexp.MustCompile(`^(PRIMARY KEY|UNIQUE|INDEX|KEY|CONSTRAINT|FOREIGN)\b`).MatchString(line) {
			continue
		}
		// PRIMARY KEY 在 MySQL 里隐含 NOT NULL，不按可空处理，
		// 否则门禁会把一张本来健康的表判成缺陷。
		if strings.Contains(line, "NOT NULL") || strings.Contains(line, "AUTO_INCREMENT") || strings.Contains(line, "PRIMARY KEY") {
			continue
		}
		if fields := strings.Fields(line); len(fields) >= 2 {
			out = append(out, strings.Trim(fields[0], "`"))
		}
	}
	return out
}

// goFieldName 把下划线列名映射到 scanAgentRow 里的局部变量名（snake → 小驼峰，Id→ID）。
func goFieldName(col string) string {
	parts := strings.Split(col, "_")
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	name := strings.ReplaceAll(strings.Join(parts, ""), "Id", "ID")
	return strings.ToLower(name[:1]) + name[1:]
}

// readStoreFile 读取 internal/store 下的文件（测试运行时 cwd 为包目录）。
func readStoreFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", name, err)
	}
	return string(b)
}
