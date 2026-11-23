package sqlstore

import (
	"fmt"
	"go/token"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"
)

// uniqueKeySpec 描述从迁移 DDL 中解析出的一条唯一性约束。
// Columns 保持 DDL 声明顺序；Primary 区分主键与普通唯一键。
type uniqueKeySpec struct {
	Table   string
	Columns []string
	Primary bool
}

// containsKey 判断 cols 是否逐列（含顺序）出现在 keys 中。
func containsKey(keys [][]string, cols []string) bool {
	for _, k := range keys {
		if equalStrings(k, cols) {
			return true
		}
	}
	return false
}

// keySetsEqual 要求两组键的数量、顺序、每键列序完全一致。
func keySetsEqual(a, b [][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !equalStrings(a[i], b[i]) {
			return false
		}
	}
	return true
}

// keysByTable 把解析出的唯一键按表分组为 map（保持声明顺序）。
func keysByTable(specs []uniqueKeySpec) map[string][][]string {
	out := map[string][][]string{}
	for _, s := range specs {
		out[s.Table] = append(out[s.Table], s.Columns)
	}
	return out
}

// TestUpsertUniqueKeyParsing 解析双轨迁移 DDL 的唯一键/主键集合，
// 钉死四张业务目标表在 MySQL 与 PG 轨的唯一键完全一致。
func TestUpsertUniqueKeyParsing(t *testing.T) {
	mysql := mysqlUniqueKeys(t)
	pg := postgresUniqueKeys(t)

	want := map[string][]string{
		"cloud_resources": {"cloud_account_id", "resource_type", "resource_id"},
		"tenants":         {"name"},
		"configs":         {"tenant_id", "key_name"},
		"abac_policies":   {"tenant_id", "id"},
	}
	for table, cols := range want {
		if !containsKey(mysql[table], cols) || !containsKey(pg[table], cols) {
			t.Fatalf("唯一键缺失: table=%s cols=%v mysql=%v pg=%v", table, cols, mysql[table], pg[table])
		}
	}

	if len(mysql) == 0 {
		t.Fatalf("MySQL 唯一键解析 map 为空（解析器失效？），不得假绿")
	}
	if len(pg) == 0 {
		t.Fatalf("PG 唯一键解析 map 为空（解析器失效？），不得假绿")
	}

	// 全量集合校验：MySQL 轨每张表的主键/唯一键集合必须是 PG 轨的子集
	// （PG 轨多出的键不报错）。
	for table, keys := range mysql {
		for _, key := range keys {
			if !containsKey(pg[table], key) {
				t.Fatalf("MySQL 主键/唯一键集合不是 PG 的子集: table=%s 缺失键 %v（PG 现有 %v）",
					table, key, pg[table])
			}
		}
	}

	// schema_migrations 是迁移框架自身的版本表，不参与业务目标比较。
	if _, ok := mysql["schema_migrations"]; ok {
		t.Errorf("schema_migrations 不应进入 MySQL 业务唯一键集合: %v", mysql["schema_migrations"])
	}
	if _, ok := pg["schema_migrations"]; ok {
		t.Errorf("schema_migrations 不应进入 PG 业务唯一键集合: %v", pg["schema_migrations"])
	}
	t.Logf("MySQL 轨解析出 %d 张表的唯一键，PG 轨解析出 %d 张表", len(mysql), len(pg))
}

// syntheticMySQLDDL 覆盖：列内 PRIMARY KEY、列内 UNIQUE、表级 UNIQUE KEY、
// UNIQUE INDEX、普通 KEY（必须忽略）、反引号标识符、前缀长度 name(191)。
const syntheticMySQLDDL = "CREATE TABLE IF NOT EXISTS `demo` (\n" +
	"  `id` VARCHAR(64) PRIMARY KEY,\n" +
	"  `tenant` VARCHAR(64) NOT NULL,\n" +
	"  `email` VARCHAR(255) NOT NULL,\n" +
	"  `name` VARCHAR(191) NOT NULL,\n" +
	"  KEY idx_tenant (`tenant`),\n" +
	"  UNIQUE KEY uq_demo_email (`email`),\n" +
	"  UNIQUE INDEX uq_demo_name (`name`(191))\n" +
	") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;\n"

// syntheticPGDDL 覆盖：CONSTRAINT ... PRIMARY KEY、CONSTRAINT ... UNIQUE、
// 表级裸 UNIQUE、列内 UNIQUE、双引号标识符、列序保持。
const syntheticPGDDL = `CREATE TABLE IF NOT EXISTS "demo" (
  "id" varchar(64) NOT NULL,
  "tenant" varchar(64) NOT NULL,
  "email" varchar(255) NOT NULL,
  "name" varchar(191) NOT NULL,
  CONSTRAINT "demo_pkey" PRIMARY KEY ("id"),
  CONSTRAINT "uq_demo_email" UNIQUE ("email"),
  UNIQUE ("name")
);

CREATE TABLE IF NOT EXISTS "users" (
  "username" varchar(64) NOT NULL UNIQUE,
  "tenant" varchar(64) NOT NULL,
  PRIMARY KEY ("username", "tenant")
);
`

// TestUpsertUniqueKeyParsingSyntheticDDL 验证解析器的结构化规则：
// 前缀长度剥离、引号剥离、列序保持、普通 KEY/INDEX 忽略、
// 列内与表级约束都收集、空内容解析为空集合。
func TestUpsertUniqueKeyParsingSyntheticDDL(t *testing.T) {
	mysql := parseUniqueKeysDDL(syntheticMySQLDDL)
	if got := keysByTable(mysql)["demo"]; !keySetsEqual(got, [][]string{{"id"}, {"email"}, {"name"}}) {
		t.Errorf("MySQL 合成 DDL 解析结果 = %v，want [[id] [email] [name]]（列序保持 + 前缀剥离 + 忽略普通 KEY）", got)
	}

	pg := keysByTable(parseUniqueKeysDDL(syntheticPGDDL))
	if got := pg["demo"]; !keySetsEqual(got, [][]string{{"id"}, {"email"}, {"name"}}) {
		t.Errorf("PG 合成 DDL 解析结果 = %v，want [[id] [email] [name]]", got)
	}
	if got := pg["users"]; !keySetsEqual(got, [][]string{{"username"}, {"username", "tenant"}}) {
		t.Errorf("PG 合成 DDL users 解析结果 = %v，want [[username] [username tenant]]", got)
	}

	if got := keysByTable(parseUniqueKeysDDL("")); len(got) != 0 {
		t.Errorf("空内容应解析为空集合，实际为 %v", got)
	}
}

// TestUpsertConflictParitySynthetic 用合成 upsertCallSpec 验证行为 parity
// 守卫：合法冲突目标双轨都通过；空冲突、缺失唯一键必须报红；
// schema_migrations 目标不参与业务比较。
func TestUpsertConflictParitySynthetic(t *testing.T) {
	spec := func(table string, conflict []string, line int) upsertCallSpec {
		return upsertCallSpec{
			File:       "internal/store/sqlstore/synthetic_upsert.go",
			Func:       "UpsertSynthetic",
			Table:      table,
			Cols:       []string{"id"},
			Values:     []string{"?"},
			Conflict:   conflict,
			UpdateCols: []string{"id"},
			Pos:        token.Position{Filename: "internal/store/sqlstore/synthetic_upsert.go", Line: line},
		}
	}

	good := []upsertCallSpec{
		spec("cloud_resources", []string{"cloud_account_id", "resource_type", "resource_id"}, 11),
		spec("tenants", []string{"name"}, 22),
		spec("configs", []string{"tenant_id", "key_name"}, 33),
		spec("abac_policies", []string{"tenant_id", "id"}, 44),
		// schema_migrations 不参与业务目标比较，即使键不存在也不得报红。
		spec("schema_migrations", []string{"version"}, 55),
	}
	assertUpsertConflictParity(t, good)

	if errs := upsertConflictParityErrors(mysqlUniqueKeys(t), postgresUniqueKeys(t),
		[]upsertCallSpec{spec("tenants", nil, 1)}); len(errs) == 0 {
		t.Fatalf("空 Conflict 必须被拒绝")
	}

	if errs := upsertConflictParityErrors(mysqlUniqueKeys(t), postgresUniqueKeys(t),
		[]upsertCallSpec{spec("tenants", []string{"nonexistent_col"}, 2)}); len(errs) == 0 {
		t.Fatalf("目标表缺失唯一键必须报红")
	}
}

// ============================================================================
// 结构化 DDL 解析实现（本任务为 test-only：解析器随测试文件交付，不改生产代码）
// ============================================================================

// upsertParityCreateTableRe 匹配 CREATE TABLE [IF NOT EXISTS] <identifier> (
// 标识符可带反引号或双引号。
var upsertParityCreateTableRe = regexp.MustCompile("(?i)CREATE\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?[`\"]?([A-Za-z0-9_]+)[`\"]?\\s*\\(")

// upsertParityPrimaryRe 匹配表级 PRIMARY KEY (...)（可带 CONSTRAINT <name> 前缀）。
var upsertParityPrimaryRe = regexp.MustCompile("(?is)^(?:CONSTRAINT\\s+[`\"]?\\w+[`\"]?\\s+)?PRIMARY\\s+KEY\\s*\\((.*)\\)$")

// upsertParityUniqueRe 匹配表级 UNIQUE (...)、UNIQUE KEY <name> (...)、
// UNIQUE INDEX <name> (...)、CONSTRAINT <name> UNIQUE (...)。
var upsertParityUniqueRe = regexp.MustCompile("(?is)^(?:CONSTRAINT\\s+[`\"]?\\w+[`\"]?\\s+)?UNIQUE(?:\\s+(?:KEY|INDEX)\\s+[`\"]?\\w+[`\"]?)?\\s*\\((.*)\\)$")

// upsertParityPlainIndexRe 匹配普通 KEY/INDEX/FULLTEXT/SPATIAL/FOREIGN KEY/CHECK，
// 一律忽略（不参与唯一键 parity）。要求后随括号，避免把名为 key 的列误判。
var upsertParityPlainIndexRe = regexp.MustCompile("(?i)^(?:KEY|INDEX|FULLTEXT(?:\\s+(?:KEY|INDEX))?|SPATIAL(?:\\s+(?:KEY|INDEX))?|FOREIGN\\s+KEY|CHECK)\\s+(?:[`\"]?\\w+[`\"]?\\s+)?\\(")

// upsertParityInlinePrimaryRe / upsertParityInlineUniqueRe 匹配列内约束
// （如 `id INT PRIMARY KEY DEFAULT 1`、`name VARCHAR(64) NOT NULL UNIQUE`）。
var upsertParityInlinePrimaryRe = regexp.MustCompile(`(?i)\bPRIMARY\s+KEY\b`)

var upsertParityInlineUniqueRe = regexp.MustCompile(`(?i)\bUNIQUE\b`)

// upsertParityStringLitRe 匹配单引号字符串字面量。
var upsertParityStringLitRe = regexp.MustCompile(`'(?:[^']|'')*'`)

// mysqlUniqueKeys 解析 MySQL 轨嵌入迁移 DDL，返回
// 表名 → 主键/唯一键列序列表（含声明顺序）。
// schema_migrations 不参与业务目标比较；结果为空立即 Fatalf。
func mysqlUniqueKeys(t *testing.T) map[string][][]string {
	t.Helper()
	m := parseMigrationUniqueKeys(t, mysqlMigrationsFS, ".")
	if len(m) == 0 {
		t.Fatalf("MySQL 迁移 DDL 未解析出任何主键/唯一键（解析器失效？），不得返回空集合后假绿")
	}
	return m
}

// postgresUniqueKeys 解析 PG 轨嵌入迁移 DDL，返回
// 表名 → 主键/唯一键列序列表（含声明顺序）。
// schema_migrations 不参与业务目标比较；结果为空立即 Fatalf。
func postgresUniqueKeys(t *testing.T) map[string][][]string {
	t.Helper()
	m := parseMigrationUniqueKeys(t, pgMigrationsFS, ".")
	if len(m) == 0 {
		t.Fatalf("PG 迁移 DDL 未解析出任何主键/唯一键（解析器失效？），不得返回空集合后假绿")
	}
	return m
}

// parseMigrationUniqueKeys 读取 embed FS 目录下全部 .sql 并解析唯一键。
func parseMigrationUniqueKeys(t *testing.T, fsys fs.FS, dir string) map[string][][]string {
	t.Helper()
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		t.Fatalf("读 embed FS %s 失败: %v", dir, err)
	}
	out := map[string][][]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		content, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("读 %s/%s 失败: %v", dir, e.Name(), err)
		}
		for _, spec := range parseUniqueKeysDDL(string(content)) {
			if spec.Table == "schema_migrations" {
				continue
			}
			// 同一约束跨迁移文件重复声明（如 003 legacy 表）按集合只计一次。
			if containsKey(out[spec.Table], spec.Columns) {
				continue
			}
			out[spec.Table] = append(out[spec.Table], spec.Columns)
		}
	}
	return out
}

// parseUniqueKeysDDL 按 CREATE TABLE 分块结构化解析 DDL，
// 收集列内与表级的 PRIMARY KEY / UNIQUE 约束。
// 双轨方言（MySQL/PG）语法在此统一处理。
func parseUniqueKeysDDL(content string) []uniqueKeySpec {
	var specs []uniqueKeySpec
	var table string
	var body strings.Builder
	inTable := false
	for _, raw := range strings.Split(content, "\n") {
		line := stripLineComment(raw)
		if !inTable {
			if m := upsertParityCreateTableRe.FindStringSubmatch(line); m != nil {
				table = m[1]
				body.Reset()
				inTable = true
			}
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), ")") {
			inTable = false
			for _, item := range splitTopLevelCommas(body.String()) {
				if spec, ok := classifyConstraint(item); ok {
					spec.Table = table
					specs = append(specs, spec)
				}
			}
			continue
		}
		body.WriteString(line)
		body.WriteString("\n")
	}
	return specs
}

// stripLineComment 删除每行 `--` 之后的注释。
func stripLineComment(line string) string {
	if i := strings.Index(line, "--"); i >= 0 {
		return line[:i]
	}
	return line
}

// stripStringLiterals 把单引号字符串字面量替换为 ”，
// 避免把 DEFAULT 'unique' 之类的字面量误判为列内约束。
func stripStringLiterals(s string) string {
	return upsertParityStringLitRe.ReplaceAllString(s, "''")
}

// classifyConstraint 把 CREATE TABLE 体内一个逗号分隔项分类为
// 唯一性约束；普通 KEY/INDEX 与普通列定义返回 ok=false。
func classifyConstraint(item string) (uniqueKeySpec, bool) {
	s := strings.TrimSpace(item)
	if s == "" {
		return uniqueKeySpec{}, false
	}
	lower := strings.ToLower(s)

	// 剥离 CONSTRAINT <name> 前缀。
	body := s
	if strings.HasPrefix(lower, "constraint ") {
		rest := strings.TrimSpace(s[len("constraint "):])
		end := strings.IndexAny(rest, " \t\n\r")
		if end < 0 {
			return uniqueKeySpec{}, false
		}
		body = strings.TrimSpace(rest[end:])
		lower = strings.ToLower(body)
	}

	var primary bool
	var cols []string
	if m := upsertParityPrimaryRe.FindStringSubmatch(body); m != nil {
		primary = true
		cols = parseColumnList(m[1])
	} else if m := upsertParityUniqueRe.FindStringSubmatch(body); m != nil {
		primary = false
		cols = parseColumnList(m[1])
	} else if upsertParityPlainIndexRe.MatchString(body) {
		return uniqueKeySpec{}, false
	} else if upsertParityInlinePrimaryRe.MatchString(stripStringLiterals(lower)) {
		// 列内 PRIMARY KEY：`id INT PRIMARY KEY DEFAULT 1`
		primary = true
		cols = []string{firstIdentifier(body)}
	} else if upsertParityInlineUniqueRe.MatchString(stripStringLiterals(lower)) {
		// 列内 UNIQUE：`username VARCHAR(64) NOT NULL UNIQUE`
		primary = false
		cols = []string{firstIdentifier(body)}
	} else {
		return uniqueKeySpec{}, false
	}
	if len(cols) == 0 {
		return uniqueKeySpec{}, false
	}
	return uniqueKeySpec{Columns: cols, Primary: primary}, true
}

// parseColumnList 解析括号内的列名列表，保持 DDL 顺序：
// 去前缀长度 name(191)、去引号、去排序方向修饰。
func parseColumnList(s string) []string {
	var cols []string
	for _, part := range splitTopLevelCommas(s) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if i := strings.Index(part, "("); i >= 0 {
			part = part[:i]
		}
		col := unquoteIdentifier(part)
		if j := strings.IndexAny(col, " \t"); j >= 0 {
			col = col[:j]
		}
		if col != "" {
			cols = append(cols, col)
		}
	}
	return cols
}

// splitTopLevelCommas 按括号深度 0 的逗号拆分，忽略字符串字面量内的逗号。
func splitTopLevelCommas(s string) []string {
	var out []string
	var cur strings.Builder
	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			cur.WriteByte(c)
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			quote = c
			cur.WriteByte(c)
		case '(':
			depth++
			cur.WriteByte(c)
		case ')':
			depth--
			cur.WriteByte(c)
		case ',':
			if depth == 0 {
				out = append(out, cur.String())
				cur.Reset()
			} else {
				cur.WriteByte(c)
			}
		default:
			cur.WriteByte(c)
		}
	}
	if strings.TrimSpace(cur.String()) != "" {
		out = append(out, cur.String())
	}
	return out
}

// unquoteIdentifier 去掉标识符两端的反引号/双引号。
func unquoteIdentifier(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '`' || s[0] == '"') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// firstIdentifier 取列定义的首个标识符（列名）。
func firstIdentifier(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 0 && (s[0] == '`' || s[0] == '"') {
		if end := strings.IndexByte(s[1:], s[0]); end >= 0 {
			return s[1 : 1+end]
		}
	}
	if i := strings.IndexAny(s, " \t\n\r("); i >= 0 {
		return s[:i]
	}
	return s
}

// upsertConflictParityErrors 对每个 upsertCallSpec 检查冲突目标
// 在 MySQL 与 PG 轨都存在相同列序的唯一键，且每个目标表的 MySQL
// 主键/唯一键集合是 PG 轨对应集合的子集（PG 多出的键不报错）。
// 返回错误信息列表（file:line、表名、冲突列或缺失的唯一键）。
func upsertConflictParityErrors(mysql, pg map[string][][]string, calls []upsertCallSpec) []string {
	var errs []string

	// schema_migrations 不参与业务目标比较。
	targets := map[string]bool{}
	for _, c := range calls {
		if c.Table == "schema_migrations" {
			continue
		}
		targets[c.Table] = true
	}

	for _, c := range calls {
		if len(c.Conflict) == 0 {
			errs = append(errs, fmt.Sprintf("%s:%d: Upsert 调用 Conflict 为空，不允许动态/缺失冲突目标",
				c.File, c.Pos.Line))
			continue
		}
		if c.Table == "schema_migrations" {
			continue
		}
		if !containsKey(mysql[c.Table], c.Conflict) {
			errs = append(errs, fmt.Sprintf("%s:%d: MySQL 表 %s 缺少冲突目标唯一键 %v（现有 %v）",
				c.File, c.Pos.Line, c.Table, c.Conflict, mysql[c.Table]))
		}
		if !containsKey(pg[c.Table], c.Conflict) {
			errs = append(errs, fmt.Sprintf("%s:%d: PG 表 %s 缺少冲突目标唯一键 %v（现有 %v）",
				c.File, c.Pos.Line, c.Table, c.Conflict, pg[c.Table]))
		}
	}

	for table := range targets {
		for _, mk := range mysql[table] {
			if !containsKey(pg[table], mk) {
				errs = append(errs, fmt.Sprintf("MySQL 主键/唯一键集合不是 PG 的子集: table=%s 缺失键 %v（PG 现有 %v）",
					table, mk, pg[table]))
			}
		}
	}
	return errs
}

// assertUpsertConflictParity 对每个 upsertCallSpec 断言冲突目标
// 在 MySQL 与 PG 轨都存在相同列序的唯一键，且 MySQL 轨每个目标表的
// 主键/唯一键集合是 PG 轨对应集合的子集（PG 多出的键不报错）。
func assertUpsertConflictParity(t *testing.T, calls []upsertCallSpec) {
	t.Helper()
	mysql := mysqlUniqueKeys(t)
	pg := postgresUniqueKeys(t)
	if len(mysql) == 0 || len(pg) == 0 {
		t.Fatalf("唯一键解析 map 为空（mysql=%d pg=%d），不得假绿", len(mysql), len(pg))
	}
	for _, c := range calls {
		if len(c.Conflict) == 0 {
			t.Fatalf("%s:%d: Upsert 调用 Conflict 为空，不允许动态/缺失冲突目标", c.File, c.Pos.Line)
		}
	}
	for _, e := range upsertConflictParityErrors(mysql, pg, calls) {
		t.Errorf("%s", e)
	}
}
