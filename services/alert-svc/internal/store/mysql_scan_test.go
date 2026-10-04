// mysql_scan_test.go — 可空列必须能读回来（#64）。
//
// 分三层，刻意不合并：
//  1. 静态对账（任何环境都跑）：SELECT 列清单与 Scan 目标个数一一对应；schema 里没写
//     NOT NULL 的列，读侧目标必须是 sql.Null*；initSchema 内联建表与 schema.sql 列集合一致。
//  2. 真库回归（有 OPSMESH_TEST_MYSQL_DSN 才跑）：写入全 NULL 的行再读回来。
//     只有真驱动会给出 `converting NULL to string is unsupported`——用假 scanner 复现它
//     等于自己重写一遍 database/sql 的转换规则，测的是我的想象而不是驱动。
//     CI 的 integration job 提供 MySQL（与 internal/store 同一实例、独立库名）。
package store

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestAlertColumnsMatchScanTargets 钉住"列清单 ↔ Scan 目标"的对齐关系。
func TestAlertColumnsMatchScanTargets(t *testing.T) {
	cases := []struct {
		name    string
		columns string
		dests   []any
	}{
		{"alerts", alertColumns, (&nullAlert{}).dest()},
		{"alert_rules", alertRuleColumns, (&nullAlertRule{}).dest()},
	}
	for _, c := range cases {
		n := len(strings.Split(c.columns, ","))
		if n != len(c.dests) {
			t.Errorf("%s: SELECT 列数 %d 与 Scan 目标数 %d 不一致（新增列必须同时补目标）", c.name, n, len(c.dests))
		}
	}
}

// TestNullableSchemaColumnsScanIntoNullTypes 把 schema.sql 的可空性与读侧目标类型对账。
// 判据取源码字面量而不是结构体字段类型：Alert.AgentID 本来就是 string，
// 用结构体当"读侧是否安全"的判据会把缺陷本身当成事实。
func TestNullableSchemaColumnsScanIntoNullTypes(t *testing.T) {
	schema := readInto(t, "schema.sql")
	nullable := nullableColumns(schema, "alerts")
	if len(nullable) < 5 {
		t.Fatalf("只解析出 %d 个 alerts 可空列，schema 解析已失效（本测试会空跑）", len(nullable))
	}
	src := readInto(t, "mysql.go")
	block := extractBetween(src, "type nullAlert struct {", "\n}")
	for _, col := range nullable {
		f := goFieldName(col)
		if !regexp.MustCompile(`\b` + f + `\s+sql\.Null`).MatchString(block) {
			t.Errorf("列 %s 在 schema 里可空，但 nullAlert.%s 不是 sql.Null* 目标（NULL 会让整行读不出来）", col, f)
		}
	}
	ruleBlock := extractBetween(src, "type nullAlertRule struct {", "\n}")
	for _, col := range nullableColumns(schema, "alert_rules") {
		f := goFieldName(col)
		if !regexp.MustCompile(`\b` + f + `\s+sql\.Null`).MatchString(ruleBlock) {
			t.Errorf("列 %s 在 schema 里可空，但 nullAlertRule.%s 不是 sql.Null* 目标", col, f)
		}
	}
}

// TestInitSchemaMatchesSchemaSQL 防"两份建表定义漂移"：initSchema 内联的 DDL 与 schema.sql
// 是同一段结构写了两遍，列集合一旦分叉，真机建表与文档/迁移读到的就不是同一张表。
func TestInitSchemaMatchesSchemaSQL(t *testing.T) {
	schema := readInto(t, "schema.sql")
	src := readInto(t, "mysql.go")
	for _, table := range []string{"alerts", "alert_rules"} {
		want := nullableColumns(schema, table)
		got := columnSet(tableColumnsFromGoDDL(src, table))
		if len(want) == 0 || len(got) == 0 {
			t.Fatalf("解析 %s 的列集合为空（schema.sql / initSchema 任一解析失效，本测试会空跑）", table)
		}
		for _, c := range want {
			if !strings.Contains(got, "|"+c+"|") {
				t.Errorf("%s.%s 在 schema.sql 有、initSchema 的 DDL 没有", table, c)
			}
		}
	}
}

// extractBetween 取 start 与 end 之间的正文（用于按结构体声明块逐字段对账）。
func extractBetween(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	rest := s[i+len(start):]
	if j := strings.Index(rest, end); j >= 0 {
		return rest[:j]
	}
	return rest
}

func readInto(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", name, err)
	}
	return string(b)
}

// nullableColumns 返回 CREATE TABLE <table> 里**没有** NOT NULL / AUTO_INCREMENT 的列名。
func nullableColumns(schema, table string) []string {
	re := regexp.MustCompile(`(?s)CREATE TABLE IF NOT EXISTS ` + table + ` \((.*?)\n\)`)
	m := re.FindStringSubmatch(schema)
	if m == nil {
		return nil
	}
	return definedColumns(m[1])
}

// tableColumnsFromGoDDL 从 mysql.go 的 initSchema 字符串字面量里取同一张表的列。
// 收口锚点用"换行+两个 tab+)"而不是第一个 ")"：DDL 里 VARCHAR(64) 这类行内括号会先把
// 非贪婪匹配截断，那样解析出来的是半张表而不是列清单。
func tableColumnsFromGoDDL(src, table string) []string {
	re := regexp.MustCompile(`(?s)CREATE TABLE IF NOT EXISTS ` + table + ` \((.*?)\n\t\t\)`)
	m := re.FindStringSubmatch(src)
	if m == nil {
		return nil
	}
	return definedColumns(m[1])
}

func definedColumns(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimSuffix(line, ",")
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		if regexp.MustCompile(`^(PRIMARY KEY|UNIQUE|INDEX|KEY|CONSTRAINT)\b`).MatchString(line) {
			continue
		}
		// PRIMARY KEY 在 MySQL 里隐含 NOT NULL（alert_rules.id 就是这么写的），
		// 不按"可空"处理，否则门禁会把一张本来健康的表判成缺陷。
		if strings.Contains(line, "AUTO_INCREMENT") || strings.Contains(line, "NOT NULL") ||
			strings.Contains(line, "PRIMARY KEY") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			out = append(out, fields[0])
		}
	}
	return out
}

func columnSet(cols []string) string {
	return "|" + strings.Join(cols, "|") + "|"
}

// goFieldName 把下划线列名映射到结构体字段名（snake → camel，ID 类后缀保持大写）。
func goFieldName(col string) string {
	parts := strings.Split(col, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	name := strings.Join(parts, "")
	name = strings.ReplaceAll(name, "Id", "ID")
	return strings.ToLower(name[:1]) + name[1:]
}

// TestMySQLNullableColumnsRoundTrip 真库回归：全 NULL 的一行必须读得回来。
//
// 本机跑法用一次性容器（出厂栈的 MySQL root 只允许容器内连接，宿主直连必 1045）：
//
//	docker run -d --name opsmesh-alert-migtest-mysql -e MYSQL_ROOT_PASSWORD=sqltest-pw \
//	  -e MYSQL_ROOT_HOST=% -p 13318:3306 mysql:8.0
//	OPSMESH_TEST_MYSQL_DSN='root:sqltest-pw@tcp(127.0.0.1:13318)/opsmesh_alert_test?parseTime=true' \
//	  go test ./internal/store/ -run TestMySQLNullable -count=1
func TestMySQLNullableColumnsRoundTrip(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("未设置 OPSMESH_TEST_MYSQL_DSN：跳过真库回归（缺陷 #64 只有真驱动能复现）")
	}
	st, err := NewMySQLStore(dsn)
	if err != nil {
		t.Fatalf("NewMySQLStore: %v", err)
	}
	defer func() { _ = st.Close() }()

	now := time.Now().UTC().Truncate(time.Second)
	const alertID = "null-col-roundtrip"
	const ruleID = "null-col-rule"

	// 只写必填两列，其余留给驱动落成 NULL——AddAlert 对空值正是这么写的。
	if _, err := st.db.Exec(`DELETE FROM alerts WHERE alert_id=?`, alertID); err != nil {
		t.Fatalf("清理旧行: %v", err)
	}
	if _, err := st.db.Exec(`INSERT INTO alerts (alert_id, tenant_id, created_at) VALUES (?, ?, ?)`,
		alertID, "t-null", now); err != nil {
		t.Fatalf("插入全 NULL 行: %v", err)
	}
	t.Cleanup(func() { _, _ = st.db.Exec(`DELETE FROM alerts WHERE alert_id=?`, alertID) })

	got := st.Alert(alertID)
	if got == nil {
		t.Fatalf("Alert(%s) 返回 nil：可空列把整行吞掉了（就是 #64 的现场）", alertID)
	}
	if got.TenantID != "t-null" || got.AlertID != alertID {
		t.Errorf("读回的键不对: %+v", got)
	}
	if got.AgentID != "" || got.DeviceID != "" || got.Comment != "" {
		t.Errorf("NULL 列应读成空串，got agent=%q device=%q comment=%q", got.AgentID, got.DeviceID, got.Comment)
	}
	if listed := st.Alerts("t-null"); len(listed) == 0 {
		t.Errorf("Alerts(tenant) 没有返回刚插入的全 NULL 行（列表侧同样丢行）")
	}

	// 规则侧同理：threshold / for_duration / message 都是可空列。
	if _, err := st.db.Exec(`DELETE FROM alert_rules WHERE id=?`, ruleID); err != nil {
		t.Fatalf("清理旧规则: %v", err)
	}
	if _, err := st.db.Exec(`INSERT INTO alert_rules (id, tenant_id) VALUES (?, ?)`, ruleID, "t-null"); err != nil {
		t.Fatalf("插入全 NULL 规则: %v", err)
	}
	t.Cleanup(func() { _, _ = st.db.Exec(`DELETE FROM alert_rules WHERE id=?`, ruleID) })

	r := st.GetAlertRule(ruleID)
	if r == nil {
		t.Fatalf("GetAlertRule(%s) 返回 nil：可空列同样吞掉整行", ruleID)
	}
	// enabled 列 DDL 带 DEFAULT 1，未显式写值时读回应为 true；
	// 这条同时钉住"NULL→false 的静默翻转"：可空布尔列若被当 NULL 读，规则会莫名失效。
	if r.ID != ruleID || !r.Enabled {
		t.Errorf("规则读回不对: %+v", r)
	}
	if rules := st.ListAlertRules("t-null"); len(rules) == 0 {
		t.Errorf("ListAlertRules 未返回全 NULL 规则")
	}

	// 写侧读侧必须闭合：AddAlert 走 nullString，紧接着要能按 ID 读回。
	const added = "addalert-roundtrip"
	st.AddAlert(&Alert{AlertID: added, TenantID: "t-null"})
	t.Cleanup(func() { _, _ = st.db.Exec(`DELETE FROM alerts WHERE alert_id=?`, added) })
	a := st.Alert(added)
	if a == nil {
		t.Fatalf("AddAlert 写入的告警读不回来（空列落 NULL 后被 Scan 吞掉）")
	}
	if a.Status != "firing" {
		t.Errorf("AddAlert 的默认状态没有落库: %+v", a)
	}
}

// TestMySQLRuleIDRoundTrip 覆盖 #67：告警必须能回溯到产生它的规则。
//
// 两段都要跑，因为它们是两个不同的缺陷面：
//
//	a) 写读闭合——Evaluate 侧把 ev.RuleID 交下来之后，列、INSERT、Scan 三处都得接住；
//	b) 升级路径——存量库的 alerts 表**没有**这一列，而 CREATE TABLE IF NOT EXISTS 对已存在
//	   的表什么都不做。这里刻意先 DROP COLUMN 模拟老库，再走一遍 initSchema，
//	   证明 ensureColumn 真的把列补回来了（不补的后果是升级后第一次 INSERT 直接炸）。
func TestMySQLRuleIDRoundTrip(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("未设置 OPSMESH_TEST_MYSQL_DSN：跳过真库回归")
	}
	st, err := NewMySQLStore(dsn)
	if err != nil {
		t.Fatalf("NewMySQLStore: %v", err)
	}

	const id = "ruleid-roundtrip"
	_, _ = st.db.Exec(`DELETE FROM alerts WHERE alert_id=?`, id)
	st.AddAlert(&Alert{AlertID: id, TenantID: "t-rule", RuleID: "rule-42", Metric: "cpu", Status: "firing"})
	t.Cleanup(func() { _, _ = st.db.Exec(`DELETE FROM alerts WHERE alert_id=?`, id); _ = st.Close() })

	got := st.Alert(id)
	if got == nil {
		t.Fatalf("Alert(%s) 读不回来", id)
	}
	if got.RuleID != "rule-42" {
		t.Fatalf("rule_id 往返丢失：got %q，期望 rule-42", got.RuleID)
	}

	// —— 模拟"列还不存在"的存量库 ——
	if _, err := st.db.Exec(`ALTER TABLE alerts DROP COLUMN rule_id`); err != nil {
		t.Fatalf("前置失败（无法删列模拟老库）: %v", err)
	}
	_ = st.Close()
	st2, err := NewMySQLStore(dsn)
	if err != nil {
		t.Fatalf("老库升级时 NewMySQLStore: %v", err)
	}
	var n int
	if err := st2.db.QueryRow(`SELECT COUNT(*) FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'alerts' AND COLUMN_NAME = 'rule_id'`).Scan(&n); err != nil {
		t.Fatalf("查 information_schema: %v", err)
	}
	if n != 1 {
		t.Fatalf("存量库升级后 alerts.rule_id 仍然不存在（ensureColumn 没生效）")
	}
	_, _ = st2.db.Exec(`DELETE FROM alerts WHERE alert_id=?`, id)
	st2.AddAlert(&Alert{AlertID: id, TenantID: "t-rule", RuleID: "rule-99"})
	if a := st2.Alert(id); a == nil || a.RuleID != "rule-99" {
		t.Fatalf("升级后的库写读 rule_id 不通: %+v", a)
	}
	_, _ = st2.db.Exec(`DELETE FROM alerts WHERE alert_id=?`, id)
	_ = st2.Close()
}
