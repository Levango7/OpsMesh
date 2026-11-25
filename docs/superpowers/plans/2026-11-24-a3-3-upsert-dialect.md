# A3-3 UPSERT 方言化 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 `internal/store/sqlstore` 的 48 条真实 MySQL UPSERT 收口为 45 条方言生成器调用与 3 条专用适配，使 MySQL 行为不回退、PostgreSQL UPSERT 可执行且有真库证据。

**Architecture:** 扩展 `Dialect.Upsert(table, cols, values, conflict, updateCols)`，MySQL 输出 `ON DUPLICATE KEY UPDATE ... VALUES(...)`，PG 输出 `ON CONFLICT (...) DO UPDATE SET ... EXCLUDED...`。业务文件只传字面量元数据；领导租约、ABAC 版本自增、lifecycle 更新时间三个语义特例集中在 `upsert_special.go`。AST 守卫收集调用点并核对迁移 DDL 的唯一约束，真库测试覆盖 PREPARE、幂等与并发语义。

**Tech Stack:** Go `database/sql`、`go-sql-driver/mysql v1.10.0`、`jackc/pgx/v5 v5.13.0`、Go AST、MySQL 真库、PostgreSQL 15 真库。

**Spec:** `docs/superpowers/specs/2026-11-24-a3-3-upsert-dialect-design.md`

## Global Constraints

- 全程中文沟通；代码标识符、命令、check 名保留原文。
- 假成功零容忍：没有命令输出与真实读数，不得写“通过”。
- 只改 `internal/store/sqlstore/`、本计划、设计稿、调查文档、`docs/CLAIMS.md`；不改 `.github/workflows/`。
- 不纳入 `internal/controlplane`、`services/*`、`internal/cmdb` 的 UPSERT。
- MySQL 只承诺语义、参数顺序、写后结果等价，不承诺 SQL 文本逐字节相同。
- PG 只收口 UPSERT 语句；周围 `?`、反引号、分页等 A3-2 读写面不在本批，不得宣称 PG 全业务读写可用。
- PG DSN 只支持现有 URL 形态（`postgres://`、`postgresql://`、`pg://`）；不扩展 keyword/value DSN。
- CI 不新增 PostgreSQL service；PG15 真库在本机用 `OPSMESH_TEST_PG_DSN` 验收，PR 明示环境与命令。
- 48 个 UPSERT 本身均不消费 `RowsAffected`；不得要求 MySQL/PG UPSERT 行数相等。
- 合并前置：required 全绿 + `MERGEABLE` + CLEAN + base == main tip；合并后 main 首验、补 DONE、继续盯 CI。
- 不删除留档分支 `feat/p0-a3-pg-dialect`，不重开 PR #47，不单方删除远端分支。

---

## File Structure

### 生产代码

- Modify `internal/store/sqlstore/dialect.go`
  - `Dialect` 接口新增 `Upsert`。
  - MySQL/PG 生成器、标识符引用 helper、PG URL DSN 的 UTC 覆盖。
- Create `internal/store/sqlstore/upsert_special.go`
  - 仅包含三个特例构建器：`upsertLeaderLease`、`upsertABACPolicy`、`upsertLifecycleJob`。
- Modify 33 个业务文件中的 48 条 UPSERT：
  - `sql.go`、`sql_abac_policy.go`、`sql_alerts.go`、`sql_apikey.go`、`sql_argocd.go`、`sql_automation.go`、`sql_backup.go`、`sql_billing.go`、`sql_budget.go`、`sql_cloud_account.go`、`sql_cloud_resource.go`、`sql_compliance.go`、`sql_config.go`、`sql_devices.go`、`sql_discovery.go`、`sql_gateway_route.go`、`sql_k8s.go`、`sql_m2.go`、`sql_network.go`、`sql_pipeline.go`、`sql_plugin.go`、`sql_quota.go`、`sql_refresh.go`、`sql_script.go`、`sql_slo.go`、`sql_task_record.go`、`sql_tasks.go`、`sql_templates.go`、`sql_tenant.go`、`sql_ticket.go`、`sql_tokens.go`、`sql_traffic.go`、`sql_webhook.go`。

### 测试

- Modify `internal/store/sqlstore/dialect_test.go`
  - 生成器单测、PG DSN UTC 契约。
- Create `internal/store/sqlstore/dialect_upsert_test.go`
  - MySQL/PG SQL 形态、占位符、标识符、常量与 `NOW()`。
- Create `internal/store/sqlstore/upsert_ast_test.go`
  - 生产文件扫描、调用点 AST 收集、特殊函数 allowlist。
- Create `internal/store/sqlstore/upsert_callsites_test.go`
  - 45 个通用调用点的函数/表/冲突目标登记与分组断言。
- Create `internal/store/sqlstore/upsert_pg_parity_test.go`
  - MySQL/PG 主键与唯一约束解析、目标表 parity、空集合保护。
- Create `internal/store/sqlstore/upsert_integration_test.go`
  - 48 条语句在 MySQL/PG 真库 PREPARE；代表行为与时区。
- Create `internal/store/sqlstore/upsert_special_test.go`
  - 领导租约、ABAC 并发、lifecycle 时间戳。
- Modify `internal/store/sqlstore/sql_postgres_test.go`
  - `SHOW TimeZone` 真库断言。

### 文档与迁移

- Modify `internal/store/sqlstore/migrations/pg/001_initial.sql`
  - 只订正 lifecycle 更新时间注释，不改已应用 DDL。
- Modify `docs/pg-dialect-a3-2-a3-3-plan-2026-11-23.md`
  - 最终交付口径与实测结果。
- Modify `docs/superpowers/specs/2026-11-24-a3-3-upsert-dialect-design.md`
  - 已确认的 URL-only UTC 方案。
- Modify `docs/CLAIMS.md`
  - 设计/计划 DONE、实现 CLAIM/DONE。

---

## Task 1: 关闭设计认领并开启实现认领

**Files:**

- Modify: `docs/CLAIMS.md`

**Interfaces:**

- Consumes: 设计提交 `64064c85`。
- Produces: 一个覆盖 `internal/store/sqlstore/` 与相关文档的 qoder 实现 CLAIM。

- [ ] **Step 1: 追加设计阶段 DONE 与计划阶段 DONE**

设计阶段 DONE 的 `scope=` 必须与 2026-11-23T19:26:08Z 的设计 CLAIM 完全一致，并写 `commit=64064c85`；计划阶段 DONE 的 `scope=` 必须与 2026-11-23T19:35:00Z 的计划 CLAIM 完全一致，并写 `commit=$PLAN_COMMIT`。

```bash
PLAN_COMMIT=$(git rev-parse HEAD)
printf '[DONE] %s from=qoder commit=64064c85 scope=docs/CLAIMS.md,docs/superpowers/specs/2026-11-24-a3-3-upsert-dialect-design.md,docs/pg-dialect-a3-2-a3-3-plan-2026-11-23.md note=P0-A3-3 设计阶段收口。\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> docs/CLAIMS.md
printf '[DONE] %s from=qoder commit=%s scope=docs/superpowers/plans/2026-11-24-a3-3-upsert-dialect.md note=P0-A3-3 实现计划完成。\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$PLAN_COMMIT" >> docs/CLAIMS.md
```

- [ ] **Step 2: 追加实现 CLAIM**

```bash
printf '[CLAIM] %s from=qoder ttl=24h scope=internal/store/sqlstore/,docs/CLAIMS.md,docs/superpowers/plans/2026-11-24-a3-3-upsert-dialect.md,docs/superpowers/specs/2026-11-24-a3-3-upsert-dialect-design.md,docs/pg-dialect-a3-2-a3-3-plan-2026-11-23.md note=P0-A3-3 实现：48 条 UPSERT 收口为 45 通用 + 3 特例；不改 .github/workflows，不扩 controlplane/services/cmdb。\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> docs/CLAIMS.md
```

- [ ] **Step 3: 跑协调门**

Run: `python scripts/coord-gate.py`

Expected: `active=1`、`issues=0`、`conflict=0`、`blocked=0`。若脚本回写 `docs/COORDINATION-BOARD.md`，先 `git diff -- docs/COORDINATION-BOARD.md` 确认只有自动状态块，再 `git restore --source=HEAD -- docs/COORDINATION-BOARD.md`。

- [ ] **Step 4: 提交认领交接**

```bash
git add docs/CLAIMS.md
git commit -m "chore: claim A3-3 implementation scope"
```

---

## Task 2: 用 TDD 实现 `Dialect.Upsert`

**Files:**

- Modify: `internal/store/sqlstore/dialect.go:24-63,80-180,263-325`
- Create: `internal/store/sqlstore/dialect_upsert_test.go`

**Interfaces:**

- Consumes: 现有 `Dialect`、`convertPlaceholdersPG`。
- Produces:
  - `Dialect.Upsert(table string, cols, values, conflict, updateCols []string) string`
  - `quoteIdentifier(ident string, quote byte) string`
  - `quotedIdentifiers(idents []string, quote byte) string`
  - `upsertAssignmentsMySQL(cols []string) string`
  - `upsertAssignmentsPG(cols []string) string`

- [ ] **Step 1: 写失败测试**

```go
package sqlstore

import "testing"

func TestDialectUpsertSQL(t *testing.T) {
	cols := []string{"id", "name", "heartbeat_at"}
	values := []string{"?", "?", "NOW()"}
	conflict := []string{"id"}
	updates := []string{"name", "heartbeat_at"}

	mysql := mysqlDialect{}.Upsert("demo", cols, values, conflict, updates)
	wantMySQL := "INSERT INTO `demo` (`id`, `name`, `heartbeat_at`) VALUES (?, ?, NOW()) " +
		"ON DUPLICATE KEY UPDATE `name`=VALUES(`name`), `heartbeat_at`=VALUES(`heartbeat_at`)"
	if mysql != wantMySQL {
		t.Fatalf("mysql upsert:\n got: %s\nwant: %s", mysql, wantMySQL)
	}

	pg := postgresDialect{}.Upsert("demo", cols, values, conflict, updates)
	wantPG := `INSERT INTO "demo" ("id", "name", "heartbeat_at") VALUES ($1, $2, NOW()) ` +
		`ON CONFLICT ("id") DO UPDATE SET "name"=EXCLUDED."name", "heartbeat_at"=EXCLUDED."heartbeat_at"`
	if pg != wantPG {
		t.Fatalf("pg upsert:\n got: %s\nwant: %s", pg, wantPG)
	}
}
```

再补三个子测试：

1. `conflict=[]string{"tenant_id", "key_name"}` 时 PG 输出 `ON CONFLICT ("tenant_id", "key_name")`。
2. `values=[]string{"'?'", "?", "\"?\""}` 时 PG 只转换第二个 `?` 为 `$1`（首尾两个 `?` 分别在单引号字符串与双引号标识符内，字面量感知转换不为它们编号）。
3. 标识符含双引号/反引号时按方言重复转义，例如 MySQL `` `a``b` ``、PG `"a""b"`。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/store/sqlstore -run '^TestDialectUpsertSQL$' -count=1`

Expected: 编译失败，`Dialect` 没有 `Upsert` 方法。

- [ ] **Step 3: 实现最小生成器**

在 `Dialect` 接口加入：

```go
Upsert(table string, cols, values, conflict, updateCols []string) string
```

实现 helper：

```go
func quoteIdentifier(ident string, quote byte) string {
	return string(quote) + strings.ReplaceAll(ident, string(quote), strings.Repeat(string(quote), 2)) + string(quote)
}

func quotedIdentifiers(idents []string, quote byte) string {
	out := make([]string, len(idents))
	for i, ident := range idents {
		out[i] = quoteIdentifier(ident, quote)
	}
	return strings.Join(out, ", ")
}

func upsertAssignmentsMySQL(cols []string) string {
	out := make([]string, len(cols))
	for i, col := range cols {
		q := quoteIdentifier(col, '`')
		out[i] = q + "=VALUES(" + q + ")"
	}
	return strings.Join(out, ", ")
}

func upsertAssignmentsPG(cols []string) string {
	out := make([]string, len(cols))
	for i, col := range cols {
		q := quoteIdentifier(col, '"')
		out[i] = q + "=EXCLUDED." + q
	}
	return strings.Join(out, ", ")
}
```

MySQL：

```go
func (mysqlDialect) Upsert(table string, cols, values, _ []string, updateCols []string) string {
	return "INSERT INTO " + quoteIdentifier(table, '`') + " (" + quotedIdentifiers(cols, '`') + ") VALUES (" +
		strings.Join(values, ", ") + ") ON DUPLICATE KEY UPDATE " + upsertAssignmentsMySQL(updateCols)
}
```

PG：

```go
func (postgresDialect) Upsert(table string, cols, values, conflict, updateCols []string) string {
	query := "INSERT INTO " + quoteIdentifier(table, '"') + " (" + quotedIdentifiers(cols, '"') + ") VALUES (" +
		strings.Join(values, ", ") + ") ON CONFLICT (" + quotedIdentifiers(conflict, '"') +
		") DO UPDATE SET " + upsertAssignmentsPG(updateCols)
	return convertPlaceholdersPG(query)
}
```

注意：`convertPlaceholdersPG` 不识别 SQL 注释；本批 value 片段只允许 `?`、受控常量、`NOW()`，不得传入带注释的片段。

- [ ] **Step 4: 运行单测**

Run: `go test ./internal/store/sqlstore -run '^TestDialectUpsertSQL$' -count=1`

Expected: PASS。

- [ ] **Step 5: 格式化并提交**

```bash
gofmt -w internal/store/sqlstore/dialect.go internal/store/sqlstore/dialect_upsert_test.go
go test ./internal/store/sqlstore -run '^TestDialectUpsertSQL$' -count=1
git add internal/store/sqlstore/dialect.go internal/store/sqlstore/dialect_upsert_test.go
git commit -m "feat: add dialect-aware UPSERT generator"
```

---

## Task 3: PG DSN 强制 UTC

**Files:**

- Modify: `internal/store/sqlstore/dialect.go`
- Modify: `internal/store/sqlstore/dialect_test.go:207-217`
- Modify: `internal/store/sqlstore/sql_postgres_test.go`

**Interfaces:**

- Consumes: `postgresDialect.PrepareDSN`。
- Produces: URL query 中已有或缺失 `TimeZone` 都被规范为 `UTC`；PG 真库夹具断言会话时区。

- [ ] **Step 1: 改失败测试**

把原 PG DSN 恒等断言替换为：

```go
func TestPostgresPrepareDSNForcesUTC(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"postgres://u:p@h/opsmesh?sslmode=disable", "postgres://u:p@h/opsmesh?TimeZone=UTC&sslmode=disable"},
		{"postgres://u:p@h/opsmesh?TimeZone=Asia%2FShanghai&sslmode=disable", "postgres://u:p@h/opsmesh?TimeZone=UTC&sslmode=disable"},
		{"postgresql://u:p@h/opsmesh?sslmode=disable&timezone=UTC", "postgresql://u:p@h/opsmesh?TimeZone=UTC&sslmode=disable"},
	}
	for _, tc := range cases {
		if got := (postgresDialect{}).PrepareDSN(tc.in); got != tc.want {
			t.Errorf("PrepareDSN(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}
```

在 `newPostgresTestStore` 建库后、返回 store 前加入：

```go
var timezone string
if err := s.db.QueryRowContext(context.Background(), "SHOW TimeZone").Scan(&timezone); err != nil {
	t.Fatalf("SHOW TimeZone: %v", err)
}
if timezone != "UTC" {
	t.Fatalf("PG session TimeZone=%q, want UTC", timezone)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/store/sqlstore -run '^TestPostgresPrepareDSNForcesUTC$' -count=1`

Expected: FAIL，当前 `PrepareDSN` 恒等。

- [ ] **Step 3: 实现 URL query 覆盖**

`dialect.go` 引入 `net/url`：

```go
func (postgresDialect) PrepareDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	q := u.Query()
	for key := range q {
		if strings.EqualFold(key, "TimeZone") {
			delete(q, key)
		}
	}
	q.Set("TimeZone", "UTC")
	u.RawQuery = q.Encode()
	return u.String()
}
```

不得复用 `ensureDSNParam`：它遇到已有 `TimeZone` 不会覆盖。

- [ ] **Step 4: 运行单测**

Run: `go test ./internal/store/sqlstore -run '^(TestPostgresPrepareDSNForcesUTC|TestPrepareDSN)$' -count=1`

Expected: PASS。原 `TestPrepareDSN` 中若有 PG 恒等断言，必须同步删除或改写。

- [ ] **Step 5: 本机 PG 真库验证并提交**

确保已设置 `OPSMESH_TEST_PG_DSN`。

Run: `go test ./internal/store/sqlstore -run '^TestPostgres' -count=1 -v`

Expected: 无 skip；`SHOW TimeZone` 断言 PASS。

```bash
gofmt -w internal/store/sqlstore/dialect.go internal/store/sqlstore/dialect_test.go internal/store/sqlstore/sql_postgres_test.go
git add internal/store/sqlstore/dialect.go internal/store/sqlstore/dialect_test.go internal/store/sqlstore/sql_postgres_test.go
git commit -m "fix: force UTC for PostgreSQL sessions"
```

---

## Task 4: 实现 AST 调用点收集器

**Files:**

- Create: `internal/store/sqlstore/upsert_ast_test.go`

**Interfaces:**

- Consumes: `go/parser`、`go/ast`、`go/token`。
- Produces:
  - `type upsertCallSpec struct { File, Func, Table string; Cols, Values, Conflict, UpdateCols []string; Pos token.Position }`
  - `func sqlstoreProductionFiles(t *testing.T) []string`
  - `func collectUpsertCalls(t *testing.T, files []string) []upsertCallSpec`
  - `func literalString(t *testing.T, expr ast.Expr) string`
  - `func literalStrings(t *testing.T, expr ast.Expr) []string`

- [ ] **Step 1: 写收集器测试**

用内存源码验证五种形态：

```go
src := `package sqlstore
func f(s *SQLStore) {
    s.dialect.Upsert("t", []string{"id", "v"}, []string{"?", "NOW()"}, []string{"id"}, []string{"v"})
    s.dialect.Upsert("bad", []string{"id"}, []string{"?"}, nil, []string{"id"})
    other.Upsert("ignored", []string{"id"}, []string{"?"}, []string{"id"}, []string{"id"})
}`
```

断言：

- 只收集 `s.dialect.Upsert`。
- 合法调用解析出 `Table=t`、`Cols=[id v]`、`Values=[? NOW()]`、`Conflict=[id]`、`UpdateCols=[v]`。
- `nil` conflict 调用被收集但 `Conflict` 为空，留给最终守卫报红；收集器本身不静默丢弃。

同时断言 `sqlstoreProductionFiles` 用 `filepath.WalkDir` 递归扫描 `internal/store/sqlstore`，排除 `_test.go`，至少返回 40 个生产 `.go` 文件；不得只扫当前目录。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/store/sqlstore -run '^TestCollectUpsertCalls' -count=1`

Expected: 编译失败，类型与函数未定义。

- [ ] **Step 3: 实现收集器**

核心判定：

```go
sel, ok := call.Fun.(*ast.SelectorExpr)
if !ok || sel.Sel.Name != "Upsert" {
    return
}
recv, ok := sel.X.(*ast.SelectorExpr)
if !ok || recv.Sel.Name != "dialect" {
    return
}
if len(call.Args) != 5 {
    t.Fatalf("%s: Upsert 必须恰好 5 个参数", fset.Position(call.Pos()))
}
```

五个参数分别用 `literalString` / `literalStrings` 解析；非字面量直接 `Fatalf`，不允许动态表名、列名或冲突目标。`Func` 通过 `ast.Inspect` 外层 `*ast.FuncDecl` 获得。

- [ ] **Step 4: 运行测试**

Run: `go test ./internal/store/sqlstore -run '^TestCollectUpsertCalls' -count=1`

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
gofmt -w internal/store/sqlstore/upsert_ast_test.go
git add internal/store/sqlstore/upsert_ast_test.go
git commit -m "test: collect sqlstore UPSERT call metadata"
```

---

## Task 5: 实现唯一约束 parity 解析器

**Files:**

- Create: `internal/store/sqlstore/upsert_pg_parity_test.go`
- Read: `internal/store/sqlstore/dialect_pg_parity_test.go`
- Read: `internal/store/sqlstore/migrations/**/*.sql`
- Read: `internal/store/sqlstore/migrations/pg/001_initial.sql`

**Interfaces:**

- Consumes: MySQL/PG embed migration FS。
- Produces:
  - `type uniqueKeySpec struct { Table string; Columns []string; Primary bool }`
  - `func mysqlUniqueKeys(t *testing.T) map[string][][]string`
  - `func postgresUniqueKeys(t *testing.T) map[string][][]string`
  - `func assertUpsertConflictParity(t *testing.T, calls []upsertCallSpec)`

- [ ] **Step 1: 写解析器测试**

至少断言：

```go
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
```

再断言两个解析 map 均非空；`schema_migrations` 不参与业务目标比较。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/store/sqlstore -run '^TestUpsertUniqueKeyParsing' -count=1`

Expected: 编译失败，解析器未定义。

- [ ] **Step 3: 实现结构化 DDL 解析**

解析规则：

- MySQL：`PRIMARY KEY (...)`、`UNIQUE KEY <name> (...)`、`UNIQUE (...)`；忽略普通 `KEY/INDEX`。
- PG：`PRIMARY KEY (...)`、`UNIQUE (...)`、`CONSTRAINT <name> PRIMARY KEY (...)`、`CONSTRAINT <name> UNIQUE (...)`。
- 先删除每行 `--` 后的注释，再按 `CREATE TABLE [IF NOT EXISTS] <identifier> (` 分表；列内 `PRIMARY KEY` 与表级约束都收集。
- MySQL 唯一列若带前缀长度（如 `name(191)`），只取列名；目标冲突列不使用前缀索引。
- 标识符去掉反引号/双引号；列顺序必须保持 DDL 顺序。
- 每个解析 map 为空立即 `Fatalf`，不得返回空集合后假绿。

`assertUpsertConflictParity` 对每个 `upsertCallSpec`：

1. `Conflict` 为空 → `Fatalf`。
2. MySQL 目标表必须存在该唯一键。
3. PG 目标表必须存在完全相同列顺序的唯一键。
4. 对每个目标表逐项比较 MySQL 主键/唯一键集合，要求 MySQL 集合是 PG 集合的子集；PG 多出的唯一键不报错。
5. 错误信息带 `file:line`、表名、冲突列或缺失的唯一键。

- [ ] **Step 4: 运行测试**

Run: `go test ./internal/store/sqlstore -run '^TestUpsertUniqueKeyParsing$' -count=1`

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
gofmt -w internal/store/sqlstore/upsert_pg_parity_test.go
git add internal/store/sqlstore/upsert_pg_parity_test.go
git commit -m "test: parse UPSERT unique-key parity"
```

---

## Task 6: 迁移低风险 CRUD 组（15 处）

**Files:**

- Modify: `sql_alerts.go`、`sql_apikey.go`、`sql_argocd.go`、`sql_automation.go`、`sql_backup.go`、`sql_billing.go`、`sql_budget.go`、`sql_cloud_account.go`、`sql_compliance.go`、`sql_script.go`、`sql_templates.go`
- Create/Modify: `upsert_callsites_test.go`

**Interfaces:**

- Consumes: `Dialect.Upsert`。
- Produces: 15 个 `s.dialect.Upsert(...)` 调用。

记号：`C` 为 INSERT 列顺序；`?×n` 为 n 个连续占位；`U=C-a,b` 表示 C 中除 a、b 外每列更新；未写 `V` 时 VALUES 与 C 等长且全为 `?`。

| 函数 | 表 / conflict | C | U |
|---|---|---|---|
| `CreateAlertRule` | `alert_rules/id` | `id,tenant_id,metric,op,threshold,for_duration,severity,message,enabled,created_at,created_by` | `C-id,created_at` |
| `CreateAPIKey` | `api_keys/id` | `id,tenant_id,name,key_hash,scopes,rate_limit_per_sec,expires_at,last_used_at,enabled,created_at` | `C-id,tenant_id,created_at` |
| `CreateApp` | `argocd_apps/id` | `id,tenant_id,name,namespace,repo_url,path,target_revision,cluster_url,sync_policy,status,health_status,created_at,updated_at` | `C-id,tenant_id,created_at` |
| `CreateAutomationRule` | `automation_rules/id` | `id,tenant_id,name,description,trigger_type,trigger_params,actions,enabled,created_at,updated_at` | `C-id,tenant_id,created_at` |
| `CreateAutomationExecution` | `automation_executions/id` | `id,tenant_id,rule_id,rule_name,status,detail,started_at,ended_at` | `C-id,tenant_id` |
| `CreateBackup` | `backup_records/id` | `id,tenant_id,type,status,size,path,created_at` | `C-id,tenant_id,created_at` |
| `CreateBillingPlan` | `billing_plans/id` | `id,name,price,interval_spec,features,resource_limits,created_at` | `C-id,created_at` |
| `CreateSubscription` | `subscriptions/id` | `id,tenant_id,plan_id,status,started_at,expires_at,created_at` | `C-id,tenant_id,created_at` |
| `CreateInvoice` | `invoices/id` | `id,tenant_id,subscription_id,amount,period_start,period_end,status,items,created_at` | `C-id,tenant_id,created_at` |
| `UpsertBudget` | `budgets/tenant_id` | `tenant_id,daily_limit,monthly_limit,used_today,used_month,alert_threshold,last_reset,timezone,month_reset,alerted,updated_at` | `C-tenant_id` |
| `CreateCloudAccount` | `cloud_accounts/id` | `id,tenant_id,workspace_id,name,provider,region,auth_type,credentials_ref,status,sync_status,last_sync_at,discovered_count,created_at,updated_at,created_by` | `C-id,tenant_id,created_at` |
| `SaveReport` | `compliance_reports/id` | `id,tenant_id,device_id,results,score,created_at` | `C-id,tenant_id,created_at` |
| `CreateScript` | `scripts/id` | `id,tenant_id,name,language,content,params,timeout_sec,enabled,created_at,updated_at` | `C-id,tenant_id,created_at` |
| `SaveOSTemplate` | `os_templates/id` | `id,tenant_id,name,os,version,arch,install_url,config,created_at,updated_at` | `C-id,tenant_id,created_at` |
| `SaveMiddlewareTemplate` | `middleware_templates/id` | `id,tenant_id,name,type,version,config,created_at,updated_at` | `C-id,tenant_id,created_at` |

- [ ] **Step 1: 写分组失败测试**

在 `upsert_callsites_test.go` 定义：

```go
type expectedUpsert struct {
	Func     string
	Table    string
	Conflict []string
}

func TestUpsertCallsitesLowRiskCRUD(t *testing.T) {
	want := []expectedUpsert{
		{"CreateAlertRule", "alert_rules", []string{"id"}},
		{"CreateAPIKey", "api_keys", []string{"id"}},
		{"CreateApp", "argocd_apps", []string{"id"}},
		{"CreateAutomationRule", "automation_rules", []string{"id"}},
		{"CreateAutomationExecution", "automation_executions", []string{"id"}},
		{"CreateBackup", "backup_records", []string{"id"}},
		{"CreateBillingPlan", "billing_plans", []string{"id"}},
		{"CreateSubscription", "subscriptions", []string{"id"}},
		{"CreateInvoice", "invoices", []string{"id"}},
		{"UpsertBudget", "budgets", []string{"tenant_id"}},
		{"CreateCloudAccount", "cloud_accounts", []string{"id"}},
		{"SaveReport", "compliance_reports", []string{"id"}},
		{"CreateScript", "scripts", []string{"id"}},
		{"SaveOSTemplate", "os_templates", []string{"id"}},
		{"SaveMiddlewareTemplate", "middleware_templates", []string{"id"}},
	}
	assertExpectedUpserts(t, want)
}
```

`assertExpectedUpserts` 从 `collectUpsertCalls` 中按 `Func+Table+Conflict` 精确匹配，缺失或重复都失败。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/store/sqlstore -run '^TestUpsertCallsitesLowRiskCRUD$' -count=1`

Expected: FAIL，15 个调用点均未迁移。

- [ ] **Step 3: 逐文件机械替换**

以 `CreateAlertRule` 为例，原 SQL 替换为：

```go
query := s.dialect.Upsert(
	"alert_rules",
	[]string{"id", "tenant_id", "metric", "op", "threshold", "for_duration", "severity", "message", "enabled", "created_at", "created_by"},
	[]string{"?", "?", "?", "?", "?", "?", "?", "?", "?", "?", "?"},
	[]string{"id"},
	[]string{"tenant_id", "metric", "op", "threshold", "for_duration", "severity", "message", "enabled", "created_by"},
)
if _, err := s.db.ExecContext(ctx, query,
	r.ID, r.TenantID, r.Metric, r.Op, r.Threshold, r.ForDuration, r.Severity, r.Message,
	boolToInt(r.Enabled), r.CreatedAt, nullString(r.CreatedBy)); err != nil {
```

其余 14 处按上表生成完全同形的五个字面量参数；`ExecContext` 的 args 顺序保持不变。不得修改写前查询、错误处理、返回值或 `CreatedAt` 保留逻辑。

- [ ] **Step 4: 运行分组测试与包测试**

Run: `go test ./internal/store/sqlstore -run '^TestUpsertCallsitesLowRiskCRUD$' -count=1`

Expected: PASS。

Run: `go test ./internal/store/sqlstore -count=1`

Expected: PASS；无 DSN 时真库子测试明确 skip。

- [ ] **Step 5: 格式化并提交**

```bash
gofmt -w internal/store/sqlstore/sql_alerts.go internal/store/sqlstore/sql_apikey.go internal/store/sqlstore/sql_argocd.go internal/store/sqlstore/sql_automation.go internal/store/sqlstore/sql_backup.go internal/store/sqlstore/sql_billing.go internal/store/sqlstore/sql_budget.go internal/store/sqlstore/sql_cloud_account.go internal/store/sqlstore/sql_compliance.go internal/store/sqlstore/sql_script.go internal/store/sqlstore/sql_templates.go internal/store/sqlstore/upsert_callsites_test.go
git add internal/store/sqlstore/
git commit -m "refactor: dialectize low-risk CRUD upserts"
```

---

## Task 7: 迁移设备、通知与流水线组（16 处）

**Files:**

- Modify: `sql_devices.go`、`sql_m2.go`、`sql_pipeline.go`、`sql_plugin.go`、`sql_quota.go`、`sql_refresh.go`、`sql_discovery.go`、`sql_network.go`
- Modify: `upsert_callsites_test.go`

**Interfaces:**

- Consumes: `Dialect.Upsert`。
- Produces: 16 个通用调用，含常量 VALUES 与 `Register` 的两条 agents 分流。

| 函数 | 表 / conflict | C | V / U |
|---|---|---|---|
| `Register` agents+secret | `agents/agent_id` | `agent_id,hostname,segment,tenant_id,addr,grpc_port,metrics_port,status,load,last_seen,secret` | `?×11`；`U=C-agent_id,secret` |
| `Register` agents-no-secret | `agents/agent_id` | `agent_id,hostname,segment,tenant_id,addr,grpc_port,metrics_port,status,load,last_seen` | `?×10`；`U=C-agent_id` |
| `Register` onboard | `devices/device_id` | `device_id,segment,tenant_id,ip,agent_id,state,task_state,managed,hostname,os,arch` | `?×5,'online','idle',1,?×3`；`U=segment,tenant_id,ip,agent_id,state,task_state,managed,hostname,os,arch` |
| `Register` placeholder | `devices/device_id` | 同上 | 同上；`U=segment,tenant_id,ip,state,task_state,managed,hostname,os,arch` |
| `Register` demo-task | `tasks/task_id` | `task_id,agent_id,tenant_id,type,command,status,created_at` | `?×5,'pending',?`；`U=C-task_id,agent_id,tenant_id,created_at` |
| `UpsertDevice` | `devices/device_id` | `device_id,segment,tenant_id,ip,agent_id,state,task_state,managed,last_result,last_result_at,retired` | `?×11`；`U=C-device_id` |
| `RegisterService` | `services/service_id` | `service_id,tenant_id,service_name,address,port,metadata,status,last_heartbeat,created_at` | `?×9`；`U=C-service_id,created_at` |
| `CreateSilence` | `alert_silences/id` | `id,tenant_id,match_labels,starts_at,ends_at,created_by,reason,created_at` | `?×8`；`U=C-id,created_at` |
| `CreateNotifyChannel` | `notify_channels/id` | `id,tenant_id,name,type,config,enabled,created_at,updated_at` | `?×8`；`U=C-id,tenant_id,created_at` |
| `CreateNotifyTemplate` | `notify_templates/id` | `id,tenant_id,name,type,title,body,format,created_at,updated_at` | `?×9`；`U=C-id,tenant_id,created_at` |
| `CreateNetworkDevice` | `network_devices/id` | `id,tenant_id,name,type,vendor,model,ip,mask,mac,location,snmp_community,status,config,created_at,updated_at` | `?×15`；`U=C-id,tenant_id,created_at` |
| `CreateTemplate` | `pipeline_templates/id` | `id,tenant_id,name,description,type,yaml,agent_id,parameters,created_at,updated_at` | `?×10`；`U=C-id,tenant_id,created_at` |
| `CreateRun` | `pipeline_runs/id` | `id,tenant_id,template_id,template_name,status,parameters,logs,started_at,finished_at,created_at` | `?×10`；`U=C-id,tenant_id,created_at` |
| `CreatePlugin` | `plugins/id` | `id,name,version,description,author,type,download_url,checksum,installed,enabled,created_at` | `?×11`；`U=C-id,created_at` |
| `SetQuota` | `quota_configs/tenant_id` | `tenant_id,max_devices,max_tasks,max_alerts,updated_at` | `?×5`；`U=C-tenant_id` |
| `SaveRefreshToken` | `refresh_tokens/token_hash` | `token_hash,user_id,tenant_id,device_fp,expires_at,created_at` | `?×6`；`U=C-token_hash` |

- [ ] **Step 1: 写分组失败测试**

`TestUpsertCallsitesDevicesAndOperations` 列出上表 16 项。`Register` 的三条 devices/tasks + 两条 agents 用 `Func+Table+Conflict` 无法区分同表多条，因此测试结构增加 `Ordinal`：按 `fset.Position` 对同 `Func+Table` 调用排序后逐条匹配。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/store/sqlstore -run '^TestUpsertCallsitesDevicesAndOperations$' -count=1`

Expected: FAIL。

- [ ] **Step 3: 替换 SQL，保留常量语义**

onboard 示例：

```go
query := s.dialect.Upsert(
	"devices",
	[]string{"device_id", "segment", "tenant_id", "ip", "agent_id", "state", "task_state", "managed", "hostname", "os", "arch"},
	[]string{"?", "?", "?", "?", "?", "'online'", "'idle'", "1", "?", "?", "?"},
	[]string{"device_id"},
	[]string{"segment", "tenant_id", "ip", "agent_id", "state", "task_state", "managed", "hostname", "os", "arch"},
)
```

placeholder 的 updateCols 为 `[]string{"segment", "tenant_id", "ip", "state", "task_state", "managed", "hostname", "os", "arch"}`；demo-task 的 VALUES 为 `[]string{"?", "?", "?", "?", "?", "'pending'", "?"}`。其余按表生成。

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/store/sqlstore -run '^TestUpsertCallsitesDevicesAndOperations$' -count=1`

Expected: PASS。

Run: `go test ./internal/store/sqlstore -count=1`

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
gofmt -w internal/store/sqlstore/sql_devices.go internal/store/sqlstore/sql_m2.go internal/store/sqlstore/sql_pipeline.go internal/store/sqlstore/sql_plugin.go internal/store/sqlstore/sql_quota.go internal/store/sqlstore/sql_refresh.go internal/store/sqlstore/sql_discovery.go internal/store/sqlstore/sql_network.go internal/store/sqlstore/upsert_callsites_test.go
git add internal/store/sqlstore/
git commit -m "refactor: dialectize device and operations upserts"
```

---

## Task 8: 迁移业务关键组（14 处）

**Files:**

- Modify: `sql.go`、`sql_cloud_resource.go`、`sql_config.go`、`sql_gateway_route.go`、`sql_k8s.go`、`sql_slo.go`、`sql_task_record.go`、`sql_tasks.go`、`sql_tenant.go`、`sql_ticket.go`、`sql_tokens.go`、`sql_traffic.go`、`sql_webhook.go`
- Modify: `upsert_callsites_test.go`

**Interfaces:**

- Consumes: `Dialect.Upsert`。
- Produces: 14 个通用调用；`RenewLeadership` 与 `CreateLifecycleJob` 仍保留到 Task 9。

| 函数 | 表 / conflict | C | V / U |
|---|---|---|---|
| `UpsertControlplaneInstance` | `controlplane_instances/instance_id` | `instance_id,hostname,http_port,grpc_port,role,version,started_at,last_heartbeat` | `?×7,NOW()`；`U=hostname,http_port,grpc_port,role,version,last_heartbeat`，last_heartbeat 的 value 为 `NOW()` |
| `CreateCloudResource` | `cloud_resources/cloud_account_id,resource_type,resource_id` | `id,tenant_id,cloud_account_id,provider,region,resource_type,resource_id,resource_name,status,tags,private_ip,public_ip,created_at,updated_at` | `?×14`；`U=C-id,created_at` |
| `SetConfig` | `configs/tenant_id,key_name` | `tenant_id,key_name,value,format,version,description,updated_by,updated_at` | `?×8`；`U=C-tenant_id,key_name` |
| `SaveGatewayRoute` | `gateway_routes/tenant_id,id` | `id,tenant_id,name,path_prefix,target_backend,methods,rate_limit_per_sec,timeout_ms,enabled,created_at,updated_at` | `?×11`；`U=C-id,tenant_id,created_at` |
| `SaveK8sCluster` | `k8s_clusters/id` | `id,tenant_id,name,server,kubeconfig,status,created_at,updated_at` | `?×8`；`U=C-id,tenant_id,created_at` |
| `CreateSLO` | `slos/id` | `id,tenant_id,name,description,service_name,target,window_spec,slis,created_at,updated_at` | `?×10`；`U=C-id,tenant_id,created_at` |
| `SaveTaskRecord` | `task_records/tenant_id,kind,id` | `kind,id,tenant_id,created_at,payload` | `?×5`；`U=C-kind,id,tenant_id,created_at` |
| `SubmitResult` | `task_results/task_id` | `task_id,agent_id,exit_code,stdout,stderr,finished_at` | `?×6`；`U=exit_code,stdout,stderr,finished_at` |
| `CreateTask` | `tasks/task_id` | `task_id,agent_id,tenant_id,type,command,content,path,status,retry_count,max_retries,dead_letter,schedule,parent_id,created_at,timeout,retry_delay,priority,idempotency_key,approval_required,script_body,depends_on` | `?×21`；`U=type,command,content,path,status,schedule,parent_id,max_retries,timeout,retry_delay,priority,idempotency_key,approval_required,script_body,depends_on` |
| `CreateTenant` | `tenants/id` | `id,name,display_name,status,quota,usage_data,created_at,updated_at` | `?×8`；`U=name,display_name,status,quota,usage_data,updated_at` |
| `CreateTicket` | `tickets/id` | `id,tenant_id,title,description,status,priority,category,assignee_id,creator_id,related_device,related_task,tags,created_at,updated_at,resolved_at` | `?×15`；`U=C-id,tenant_id,created_at` |
| `issueToken` | `install_tokens/token` | `token,device_id,tenant_id,expires_at,consumed` | `?×4,0`；`U=device_id,tenant_id,expires_at,consumed`，consumed value 为 `0` |
| `CreatePolicy` | `traffic_policies/id` | `id,tenant_id,name,service_name,type,canary_weights,mirror_percent,timeout,retries,retry_timeout,max_conns,max_requests,status,created_at,updated_at` | `?×15`；`U=C-id,tenant_id,created_at` |
| `CreateWebhook` | `webhooks/id` | `id,tenant_id,name,url,events,headers,body_template,enabled,retry_count,retry_interval_sec,created_at,updated_at` | `?×12`；`U=C-id,tenant_id,created_at` |

- [ ] **Step 1: 写分组失败测试**

`TestUpsertCallsitesBusinessCritical` 列出上表 14 项。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/store/sqlstore -run '^TestUpsertCallsitesBusinessCritical$' -count=1`

Expected: FAIL。

- [ ] **Step 3: 替换 SQL**

`UpsertControlplaneInstance` 示例：

```go
query := s.dialect.Upsert(
	"controlplane_instances",
	[]string{"instance_id", "hostname", "http_port", "grpc_port", "role", "version", "started_at", "last_heartbeat"},
	[]string{"?", "?", "?", "?", "?", "?", "?", "NOW()"},
	[]string{"instance_id"},
	[]string{"hostname", "http_port", "grpc_port", "role", "version", "last_heartbeat"},
)
```

`CreateTask` 的 updateCols 必须保留上表精确集合：不得误更新 `retry_count`、`dead_letter`、`created_at`。`CreateTenant` PG conflict 固定 `id`；`CreateCloudResource` PG conflict 固定自然键。

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/store/sqlstore -run '^TestUpsertCallsitesBusinessCritical$' -count=1`

Expected: PASS。

Run: `go test ./internal/store/sqlstore -count=1`

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
gofmt -w internal/store/sqlstore/sql.go internal/store/sqlstore/sql_cloud_resource.go internal/store/sqlstore/sql_config.go internal/store/sqlstore/sql_gateway_route.go internal/store/sqlstore/sql_k8s.go internal/store/sqlstore/sql_slo.go internal/store/sqlstore/sql_task_record.go internal/store/sqlstore/sql_tasks.go internal/store/sqlstore/sql_tenant.go internal/store/sqlstore/sql_ticket.go internal/store/sqlstore/sql_tokens.go internal/store/sqlstore/sql_traffic.go internal/store/sqlstore/sql_webhook.go internal/store/sqlstore/upsert_callsites_test.go
git add internal/store/sqlstore/
git commit -m "refactor: dialectize business-critical upserts"
```

---

## Task 9: 实现三个语义特例

**Files:**

- Create: `internal/store/sqlstore/upsert_special.go`
- Create: `internal/store/sqlstore/upsert_special_test.go`
- Modify: `internal/store/sqlstore/sql.go:1013-1058,1090-1100`
- Modify: `internal/store/sqlstore/sql_abac_policy.go:68-90`
- Modify: `internal/store/sqlstore/migrations/pg/001_initial.sql:21`

**Interfaces:**

- Consumes: `Dialect.DriverName`、`ConvertPlaceholders`。
- Produces:
  - `func upsertLeaderLease(d Dialect) string`
  - `func upsertABACPolicy(d Dialect) string`
  - `func upsertLifecycleJob(d Dialect) string`

- [ ] **Step 1: 写 SQL 形态失败测试**

```go
func TestSpecialUpsertSQL(t *testing.T) {
	mysql := upsertLeaderLease(mysqlDialect{})
	if !strings.Contains(mysql, "holder=IF(expires_at < ? OR holder=VALUES(holder)") {
		t.Fatalf("MySQL leader lease 特例丢失: %s", mysql)
	}
	pg := upsertLeaderLease(postgresDialect{})
	for _, token := range []string{
		`ON CONFLICT ("id") DO UPDATE SET`,
		`CASE WHEN "leader_lease"."expires_at" < $4 OR "leader_lease"."holder" = EXCLUDED."holder"`,
		`CASE WHEN "leader_lease"."expires_at" < $5 OR "leader_lease"."holder" = EXCLUDED."holder"`,
		`CASE WHEN "leader_lease"."expires_at" < $6 OR "leader_lease"."holder" = EXCLUDED."holder"`,
		`THEN EXCLUDED."holder" ELSE "leader_lease"."holder" END`,
	} {
		if !strings.Contains(pg, token) {
			t.Fatalf("PG leader lease 缺少 %q: %s", token, pg)
		}
	}
	if !strings.Contains(upsertABACPolicy(postgresDialect{}), `"version" = "abac_policies"."version" + 1`) {
		t.Fatal("PG ABAC 版本自增丢失")
	}
	if !strings.Contains(upsertLifecycleJob(postgresDialect{}), `"updated_at"=now()`) {
		t.Fatal("PG lifecycle 更新时间丢失")
	}
	for _, tc := range []struct {
		name string
		mysql string
		pg string
		want int
	}{
		{"leader", mysql, pg, 6},
		{"abac", upsertABACPolicy(mysqlDialect{}), upsertABACPolicy(postgresDialect{}), 8},
		{"lifecycle", upsertLifecycleJob(mysqlDialect{}), upsertLifecycleJob(postgresDialect{}), 9},
	} {
		if got := strings.Count(tc.mysql, "?"); got != tc.want {
			t.Fatalf("%s MySQL 占位符=%d, want %d", tc.name, got, tc.want)
		}
		if got := strings.Count(tc.pg, "$"); got != tc.want {
			t.Fatalf("%s PG 占位符=%d, want %d", tc.name, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/store/sqlstore -run '^TestSpecialUpsertSQL$' -count=1`

Expected: 编译失败，三个函数未定义。

- [ ] **Step 3: 实现三个构建器**

`upsertLeaderLease`：

- MySQL 原样保留三占位条件与从左到右语义。
- PG 使用 `CASE WHEN`，三个条件都基于旧行：`leader_lease.expires_at < $4/$5/$6 OR leader_lease.holder = EXCLUDED.holder`（三个 CASE 分别绑定第 4/5/6 个 now 实参，不得复用同一参数下标）；ELSE 保留旧列。
- 参数顺序必须保持 `instanceID, exp, now, now, now, now`。

`upsertABACPolicy`：

- MySQL 原样保留 `` `version`=`version`+1 ``。
- PG 输出 `"version" = "abac_policies"."version" + 1`，其余更新列用 `EXCLUDED`。
- conflict 为 `("tenant_id", "id")`。

`upsertLifecycleJob`：

- MySQL 原样保留依赖列定义 `ON UPDATE CURRENT_TIMESTAMP(3)`。
- PG 在原更新列后增加 `"updated_at"=now()`。
- conflict 为 `("id")`；不改 PG 001 DDL。

三个函数用 `switch d.DriverName()` 选择 MySQL/PG；`postgresDialect.DriverName()` 的返回值是 `"pgx"`，因此必须对 `"pgx"` 做特例，其余走 MySQL。PG 分支返回前必须调用 `d.ConvertPlaceholders(query)`，保证 `$n` 连续；MySQL 分支原样返回 `?` SQL。

- [ ] **Step 4: 接线业务调用**

- `RenewLeadership` 的 `ExecContext` 第一个参数改为 `upsertLeaderLease(s.dialect)`，args 不变。
- `UpsertABACPolicyRecord` 的 SQL 改为 `upsertABACPolicy(s.dialect)`，args 不变。
- `CreateLifecycleJob` 的 SQL 改为 `upsertLifecycleJob(s.dialect)`，args 不变。
- PG 001 顶部注释改为：“lifecycle_jobs 的 UPSERT 由专用方言适配显式补 `updated_at`；其余 UPDATE 保持显式写法。”

- [ ] **Step 5: 跑单测并提交**

Run: `go test ./internal/store/sqlstore -run '^TestSpecialUpsertSQL$' -count=1`

Expected: PASS。

```bash
gofmt -w internal/store/sqlstore/upsert_special.go internal/store/sqlstore/upsert_special_test.go internal/store/sqlstore/sql.go internal/store/sqlstore/sql_abac_policy.go
git add internal/store/sqlstore/
git commit -m "feat: add semantic UPSERT exceptions"
```

---

## Task 10: 全调用点真库 PREPARE

**Files:**

- Create/Modify: `internal/store/sqlstore/upsert_integration_test.go`
- Modify: `internal/store/sqlstore/upsert_ast_test.go`

**Interfaces:**

- Consumes: `collectUpsertCalls`、三个特例构建器、MySQL/PG 测试夹具。
- Produces: `TestUpsertPrepareAllCallsites`，覆盖 45 通用 + 3 特例。

- [ ] **Step 1: 写失败测试**

```go
func TestUpsertPrepareAllCallsites(t *testing.T) {
	calls := collectUpsertCalls(t, sqlstoreProductionFiles(t))
	if len(calls) != 45 {
		t.Fatalf("通用 UPSERT 调用数=%d, want 45", len(calls))
	}
	t.Run("mysql", func(t *testing.T) {
		s, cleanup := newTestSQLStore(t)
		defer cleanup()
		prepareAll(t, s, mysqlDialect{}, calls)
	})
	t.Run("postgres", func(t *testing.T) {
		s, _, cleanup := newPostgresTestStore(t)
		defer cleanup()
		prepareAll(t, s, postgresDialect{}, calls)
	})
}
```

`prepareAll` 对每个调用：

```go
query := d.Upsert(c.Table, c.Cols, c.Values, c.Conflict, c.UpdateCols)
stmt, err := s.db.PrepareContext(context.Background(), query)
if err != nil {
	t.Fatalf("%s:%s PREPARE %s: %v", c.File, c.Pos, c.Table, err)
}
stmt.Close()
```

随后对三个特例分别用相同 `PrepareContext` 验证。无 DSN 时子测试 skip；有 DSN 时不得 skip。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/store/sqlstore -run '^TestUpsertPrepareAllCallsites$' -count=1 -v`

Expected: 通用调用数不是 45，或部分 PG 语句 PREPARE 失败。

- [ ] **Step 3: 修正调用点直到 45 条全部 PREPARE**

逐条根据错误修正表名、列名、conflict、VALUES 占位数量；不得通过跳过失败语句让测试变绿。

- [ ] **Step 4: MySQL/PG 真库复跑**

确保 `OPSMESH_TEST_MYSQL_DSN` 与 `OPSMESH_TEST_PG_DSN` 已设置。

Run: `go test ./internal/store/sqlstore -run '^TestUpsertPrepareAllCallsites$' -count=1 -v`

Expected: 两个子测试均执行且 PASS，48 条语句全部 PREPARE 成功。

- [ ] **Step 5: 提交**

```bash
gofmt -w internal/store/sqlstore/upsert_integration_test.go internal/store/sqlstore/upsert_ast_test.go
git add internal/store/sqlstore/
git commit -m "test: prepare every UPSERT on MySQL and PostgreSQL"
```

---

## Task 11: 真库行为与并发回归

**Files:**

- Create/Modify: `internal/store/sqlstore/upsert_integration_test.go`
- Create/Modify: `internal/store/sqlstore/upsert_special_test.go`

**Interfaces:**

- Consumes: MySQL/PG 夹具、`ConvertPlaceholders`、三个特例。
- Produces: 下列行为测试；纯写路径测试在 MySQL/PG 双库执行，涉及周围裸 `?`/反引号读路径的公共方法仅 MySQL 执行并明示 skip 原因。
- Produces: `func waitForDBTimestampTick(t *testing.T, s *SQLStore, d Dialect, after time.Time, tick time.Duration)`

**Scope ruling:** A3-3 只方言化 UPSERT 写路径。若 PG 公共方法因周围 A3-2 读路径失败，不得修改生产读路径；改为直接执行生成的 UPSERT/特例构建器并用 `ConvertPlaceholders` 读回，或将该公共方法测试降级为 MySQL-only。

- [ ] **Step 1: 写代表行为测试**

1. `TestUpsertInsertUpdateReadback`
   - 直接用 `alert_rules` 的生成 SQL 插入固定 ID。
   - 重复执行修改 `metric/message/enabled`。
   - 用 `s.dialect.ConvertPlaceholders("SELECT metric, message FROM alert_rules WHERE id=?")` 读回。
   - 断言第二次值生效，`created_at` 未被覆盖。

2. `TestUpsertCompositeConflict`
   - 用 `configs` 生成 SQL，同一 `(tenant_id,key_name)` 连续写 `version=1`、`version=2`。
   - 断言只有一行且 version=2。

3. `TestUpsertConstantsAndHeartbeat`
   - 用 `devices` 的 onboard 生成 SQL 验证 `state/task_state/managed` 常量覆盖。
   - 用 `controlplane_instances` 生成 SQL 记录首次 `started_at/last_heartbeat`；调用 `waitForDBTimestampTick(t, s, d, firstHeartbeat, time.Second)` 后再写新 heartbeat；断言 `started_at` 不变、`last_heartbeat` 推进。

4. `TestUpsertCrossUniqueKeyIsStrictOnPostgres`
   - MySQL：`CreateTenant` 命中已存在 name 时保持既有行为，不新增断言要求报错。
   - PG：直接对 `tenants` 使用 conflict `(id)`，插入 `id=new,name=existing`，断言唯一键错误。
   - PG：直接对 `cloud_resources` 使用自然键 conflict，插入相同 id、不同自然键，断言唯一键错误。

5. `TestSubmitResultStateTransitions`
   - MySQL-only：`SubmitResult` 周围 tasks SELECT/UPDATE 与 devices writeback 仍是 A3-2 裸 `?`/反引号读路径，本批不方言化。
   - 为 success/retry/dead-letter 各造一条 running task，调 `SubmitResult`，用转换后的 SELECT 验证状态、retry_count、dead_letter。
   - PG 子测试必须显式 `t.Skip("SubmitResult 周围读路径属 A3-2，不在 A3-3 范围")`，不得静默 skip。
   - 不断言 UPSERT 的 `RowsAffected`。

6. `TestUpsertTaskResultsWritePath`
   - 直接用 `task_results` 生成 SQL 在 MySQL/PG 双库执行 success/retry/dead-letter 三种写路径，读回用 `ConvertPlaceholders` 转换后的 SELECT。

- [ ] **Step 2: 写并发特例测试**

1. `TestRenewLeadershipConcurrentClaim`
   - 建多个 `SQLStore` 实例并设置不同 `instanceID`，共享同一库。
   - 子用例覆盖：表为空时插入并持租；本实例未过期续租；其他实例未过期不能抢占；预置过期租约可抢占；`holder/expires_at` 为 NULL 时不误判。
   - 并发子用例预置过期租约，N 个实例同时 `RenewLeadership`；每轮恰好一个返回 true；读回 holder 与获胜者一致；连续 10 轮不双主。

2. `TestABACPolicyConcurrentVersion`
   - 直接执行 `upsertABACPolicy(s.dialect)`（不调用公共 `UpsertABACPolicyRecord`，其 PG 回读仍属 A3-2）。
   - 先插入一条，重复写穿并修改 `subject/resource/action/effect`；断言 version=2、字段覆盖、`created_at` 不变。
   - 同一 `(tenant_id,id)` 起 N 个 goroutine 执行同一生成 SQL。
   - 全部成功后读回 version=N+2；`created_at` 仍不变。

3. `TestLifecycleUpsertTimestamp`
   - 首次 `CreateLifecycleJob` 后读 `updated_at`。
   - 调用 `waitForDBTimestampTick(t, s, s.dialect, firstUpdated, time.Millisecond)` 后同 ID 更新 phase/progress，再读。
   - PG 必须推进；MySQL 保持原隐式更新时间行为。
   - MySQL no-op 子用例在字段完全相同的情况下再写一次，记录并对照更新时间，保持既有行为，不要求两库 no-op 行数或时间相同。
   - 注：当公共 `GetLifecycleJob` 的 PG 读回在 A3-3 write-path-only 裁决下不可用时，本测试改用直接 `upsertLifecycleJob` 构建器加 `ConvertPlaceholders` 读回，不触达公共读路径。

`waitForDBTimestampTick` 按方言查询数据库当前时间（MySQL 用 `SELECT NOW(6)`，PG 用 `SELECT clock_timestamp()`），每 5ms 轮询，最多 3s；只有数据库时间达到 `after+tick` 才返回，否则 `t.Fatal`，不得用宿主固定 sleep 冒充跨库时钟条件。

- [ ] **Step 3: 运行测试确认失败或暴露缺口**

Run: `go test ./internal/store/sqlstore -run '^(TestUpsert|TestRenewLeadershipConcurrentClaim|TestABACPolicyConcurrentVersion|TestLifecycleUpsertTimestamp|TestSubmitResultStateTransitions|TestUpsertTaskResultsWritePath)' -count=1 -v`

Expected: 初次运行暴露实现缺口；修复后全 PASS。PG 公共方法若因周围 A3-2 读路径失败，按上方 Scope ruling 改为直接写路径测试或 MySQL-only，禁止方言化生产读路径。

- [ ] **Step 4: 双库复跑**

Run: `go test ./internal/store/sqlstore -run '^(TestUpsert|TestRenewLeadershipConcurrentClaim|TestABACPolicyConcurrentVersion|TestLifecycleUpsertTimestamp|TestSubmitResultStateTransitions|TestUpsertTaskResultsWritePath)' -count=1 -v`

Expected: 所有写路径测试在 MySQL/PG 均真实执行；MySQL-only 公共方法测试明示 skip 原因。记录测试名、耗时、PASS 数。

- [ ] **Step 5: 提交**

```bash
gofmt -w internal/store/sqlstore/upsert_integration_test.go internal/store/sqlstore/upsert_special_test.go
git add internal/store/sqlstore/
git commit -m "test: verify UPSERT behavior and concurrency"
```

---

## Task 12: 常驻静态守卫与变异射程

**Files:**

- Modify: `internal/store/sqlstore/upsert_ast_test.go`
- Modify: `internal/store/sqlstore/upsert_pg_parity_test.go`
- Modify: `internal/store/sqlstore/upsert_callsites_test.go`

**Interfaces:**

- Consumes: AST 收集器、DDL 解析器、45 个分组测试。
- Produces:
  - `TestNoRawBusinessUpsertSQL`
  - `TestUpsertCallsiteCountAndConflictParity`
  - `TestSpecialUpsertAllowlist`

- [ ] **Step 1: 写最终守卫测试**

```go
func TestNoRawBusinessUpsertSQL(t *testing.T) {
	files := sqlstoreProductionFiles(t)
	for _, file := range files {
		if strings.HasSuffix(file, "dialect.go") || strings.HasSuffix(file, "upsert_special.go") {
			continue
		}
		fset := token.NewFileSet()
		node, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(node, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(value, "ON DUPLICATE KEY UPDATE") || strings.Contains(value, "ON CONFLICT") {
				t.Fatalf("%s: 业务文件禁止裸 UPSERT SQL", fset.Position(lit.Pos()))
			}
			return true
		})
	}
}
```

`TestUpsertCallsiteCountAndConflictParity`：

- `len(calls)==45`。
- 所有 `Conflict` 非空。
- 调 `assertUpsertConflictParity`。

`TestSpecialUpsertAllowlist`：

- 扫描生产 AST，原始 UPSERT token 只允许出现在 `dialect.go` 与 `upsert_special.go`。
- `upsert_special.go` 中含原始 token 的函数集合必须恰为 `upsertLeaderLease`、`upsertABACPolicy`、`upsertLifecycleJob`。

- [ ] **Step 2: 运行测试**

Run: `go test ./internal/store/sqlstore -run '^(TestNoRawBusinessUpsertSQL|TestUpsertCallsiteCountAndConflictParity|TestSpecialUpsertAllowlist)$' -count=1`

Expected: PASS。

- [ ] **Step 3: 做窄变异并记录红绿对照**

依次单独注入、运行、还原：

1. 在 `internal/store/sqlstore/sql_alerts.go` 的任一 SQL 字符串加入 `ON DUPLICATE KEY UPDATE` → `TestNoRawBusinessUpsertSQL` 必须 FAIL。
2. 把 `internal/store/sqlstore/sql_cloud_resource.go` 中 `CreateCloudResource` 的 conflict 从自然键改为 `[]string{"id"}` → `TestUpsertCallsiteCountAndConflictParity` 必须 FAIL。
3. 从 `internal/store/sqlstore/migrations/pg/001_initial.sql` 临时删除 `CONSTRAINT "uk_cloud_resources_account_res" UNIQUE ("cloud_account_id", "resource_type", "resource_id")` → `TestUpsertCallsiteCountAndConflictParity` 必须 FAIL。
4. 在 `internal/store/sqlstore/upsert_pg_parity_test.go` 的 `postgresUniqueKeys` 函数首行临时加入 `return map[string][][]string{}` → `TestUpsertUniqueKeyParsing` 必须 FAIL。

每个变异运行对应单测并保存输出；分别用 `git diff --exit-code -- internal/store/sqlstore/sql_alerts.go`、`git diff --exit-code -- internal/store/sqlstore/sql_cloud_resource.go`、`git diff --exit-code -- internal/store/sqlstore/migrations/pg/001_initial.sql`、`git diff --exit-code -- internal/store/sqlstore/upsert_pg_parity_test.go` 证明归零，再跑三个守卫确认 PASS。

- [ ] **Step 4: 提交守卫**

```bash
gofmt -w internal/store/sqlstore/upsert_ast_test.go internal/store/sqlstore/upsert_pg_parity_test.go internal/store/sqlstore/upsert_callsites_test.go
git add internal/store/sqlstore/
git commit -m "test: guard raw UPSERT SQL and conflict targets"
```

---

## Task 13: 文档收口与全量验证

**Files:**

- Modify: `docs/pg-dialect-a3-2-a3-3-plan-2026-11-23.md`
- Modify: `docs/superpowers/specs/2026-11-24-a3-3-upsert-dialect-design.md`
- Modify: `docs/CLAIMS.md`

**Interfaces:**

- Consumes: 前 12 个任务的实测输出。
- Produces: PR 说明素材、DONE 行、最终干净工作树。

- [ ] **Step 1: 订正旧“三处”口径**

搜索并订正本批相关文档中所有“三处 ON DUPLICATE KEY”表述为“48 处真实 UPSERT（45 通用 + 3 特例）”；不得改无关历史文档。

- [ ] **Step 2: 写实测段落**

文档与 PR 说明必须写清：

- 本机 MySQL 版本、PG15 版本、DSN 形态（不得写密码）。
- 执行的命令与 PASS/skip 数。
- 48 条 PREPARE、并发不双主、ABAC 版本递增、lifecycle 时间戳结果。
- 四种窄变异各自红在哪、还原后绿（含设计要求的三种与额外空集合保护）。
- CI 未加 PG service；PG 结果来自本机真库。
- 本批没有解决 A3-2 其余读写面，不宣称 PG 全业务可用。

- [ ] **Step 3: 全量本地验证**

```bash
gofmt -w internal/store/sqlstore
go build ./...
go vet ./internal/store/sqlstore/...
go test ./internal/store/sqlstore -count=1
python scripts/coord-gate.py
git diff --check
```

再在已设置 MySQL/PG DSN 的环境运行：

```bash
go test ./internal/store/sqlstore -count=1 -v
```

Expected: 构建、vet、格式、协调门通过；无 DSN 的 CI 形态明确 skip，有 DSN 的本机形态真库用例全部 PASS。

- [ ] **Step 4: 追加实现 DONE 并提交**

DONE 的 `scope=` 必须与实现 CLAIM 完全一致；先取 `FINAL_COMMIT=$(git rev-parse HEAD)`，再把该值写在 `note=` 前并追加 DONE：

```bash
FINAL_COMMIT=$(git rev-parse HEAD)
printf '[DONE] %s from=qoder commit=%s scope=internal/store/sqlstore/,docs/CLAIMS.md,docs/superpowers/plans/2026-11-24-a3-3-upsert-dialect.md,docs/superpowers/specs/2026-11-24-a3-3-upsert-dialect-design.md,docs/pg-dialect-a3-2-a3-3-plan-2026-11-23.md note=P0-A3-3 实现完成；48 条 UPSERT=45 通用+3 特例，MySQL/PG 真库证据见调查文档。\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$FINAL_COMMIT" >> docs/CLAIMS.md
git add docs/pg-dialect-a3-2-a3-3-plan-2026-11-23.md docs/superpowers/specs/2026-11-24-a3-3-upsert-dialect-design.md docs/CLAIMS.md
git commit -m "docs: record A3-3 verification results"
```

- [ ] **Step 5: 推送分支并开 PR**

```bash
git push -u origin feat/p0-a3-upsert-dialect
PR_BODY=$(cat <<'EOF'
## Summary
- 将 sqlstore 的 48 条真实 MySQL UPSERT 收口为 45 个通用方言生成器调用与 3 个语义特例。
- PostgreSQL 输出 ON CONFLICT ... DO UPDATE；MySQL 参数顺序与写后结果保持不变。
- 增加 AST 裸 SQL 守卫、冲突目标唯一约束 parity、48 条真库 PREPARE、并发与跨唯一键回归。

## Boundaries
- 不含 controlplane/services/cmdb，不改 .github/workflows，不新增 PostgreSQL service。
- PG 真库证据来自本机 PG15；本批不宣称 A3-2 其余读写面或 PG 全业务读写已完成。
- UPSERT 的 RowsAffected 不参与 MySQL/PG 等价判定。

## Verification
- 完整命令、版本、PASS/skip 数与变异红绿对照见 docs/pg-dialect-a3-2-a3-3-plan-2026-11-23.md 的 A3-3 实测段落。
EOF
)
PR=$(gh pr create --base main --head feat/p0-a3-upsert-dialect --title "feat: dialectize sqlstore UPSERT paths" --body "$PR_BODY" --json number --jq .number)
printf '%s\n' "$PR"
```

PR 说明必须包含 Global Constraints 中的边界、双库实测、CI 无 PG 的说明和 `RowsAffected` 差异。

---

## Task 14: 等 CI、按四条件合并并完成 main 首验

**Files:**

- Modify: `docs/CLAIMS.md`（仅在需要补 main 侧 DONE 时）

- [ ] **Step 1: 盯 required checks**

```bash
PR=$(gh pr view feat/p0-a3-upsert-dialect --json number --jq .number)
gh pr checks "$PR"
gh pr view "$PR" --json mergeable,mergeStateStatus,baseRefName,headRefOid
```

Expected: required 全绿、`MERGEABLE`、CLEAN、base == main tip。任一条件不满足就停手。

- [ ] **Step 2: squash 合并**

仅在四条件同时满足时执行：

```bash
PR=$(gh pr view feat/p0-a3-upsert-dialect --json number --jq .number)
gh pr merge "$PR" --squash
```

- [ ] **Step 3: main 首验**

读取合并提交 SHA，盯 main push run；确认 build-test 与其余 required 全绿。若 base 已移动，先重新核对 PR 四条件，不得沿用旧读数。

- [ ] **Step 4: 补 DONE 并盯销户提交 CI**

如 DONE 未随 PR 进入 main，基于最新 main 追加同 scope DONE，提交推送并盯该提交 CI 全绿。

- [ ] **Step 5: 清理自有分支/worktree**

仅在确认已合并、工作树干净、无独占未推送提交后清理本批 worktree 与本地分支；保留留档分支 `feat/p0-a3-pg-dialect`，远端分支交 owner 处置。

---

## 附录：通用调用点完整登记

下列 45 项是最终守卫的权威清单；任务 6–8 的分组测试合起来必须与之一致。

| # | Func | Table | Conflict |
|---:|---|---|---|
| 1 | `UpsertControlplaneInstance` | `controlplane_instances` | `instance_id` |
| 2 | `CreateAlertRule` | `alert_rules` | `id` |
| 3 | `CreateAPIKey` | `api_keys` | `id` |
| 4 | `CreateApp` | `argocd_apps` | `id` |
| 5 | `CreateAutomationRule` | `automation_rules` | `id` |
| 6 | `CreateAutomationExecution` | `automation_executions` | `id` |
| 7 | `CreateBackup` | `backup_records` | `id` |
| 8 | `CreateBillingPlan` | `billing_plans` | `id` |
| 9 | `CreateSubscription` | `subscriptions` | `id` |
| 10 | `CreateInvoice` | `invoices` | `id` |
| 11 | `UpsertBudget` | `budgets` | `tenant_id` |
| 12 | `CreateCloudAccount` | `cloud_accounts` | `id` |
| 13 | `CreateCloudResource` | `cloud_resources` | `cloud_account_id,resource_type,resource_id` |
| 14 | `SaveReport` | `compliance_reports` | `id` |
| 15 | `SetConfig` | `configs` | `tenant_id,key_name` |
| 16 | `Register` | `agents` | `agent_id` |
| 17 | `Register` | `agents` | `agent_id` |
| 18 | `Register` | `devices` | `device_id` |
| 19 | `Register` | `devices` | `device_id` |
| 20 | `Register` | `tasks` | `task_id` |
| 21 | `UpsertDevice` | `devices` | `device_id` |
| 22 | `RegisterService` | `services` | `service_id` |
| 23 | `SaveGatewayRoute` | `gateway_routes` | `tenant_id,id` |
| 24 | `SaveK8sCluster` | `k8s_clusters` | `id` |
| 25 | `CreateSilence` | `alert_silences` | `id` |
| 26 | `CreateNotifyChannel` | `notify_channels` | `id` |
| 27 | `CreateNotifyTemplate` | `notify_templates` | `id` |
| 28 | `CreateNetworkDevice` | `network_devices` | `id` |
| 29 | `CreateTemplate` | `pipeline_templates` | `id` |
| 30 | `CreateRun` | `pipeline_runs` | `id` |
| 31 | `CreatePlugin` | `plugins` | `id` |
| 32 | `SetQuota` | `quota_configs` | `tenant_id` |
| 33 | `SaveRefreshToken` | `refresh_tokens` | `token_hash` |
| 34 | `CreateScript` | `scripts` | `id` |
| 35 | `CreateSLO` | `slos` | `id` |
| 36 | `SaveTaskRecord` | `task_records` | `tenant_id,kind,id` |
| 37 | `SubmitResult` | `task_results` | `task_id` |
| 38 | `CreateTask` | `tasks` | `task_id` |
| 39 | `SaveOSTemplate` | `os_templates` | `id` |
| 40 | `SaveMiddlewareTemplate` | `middleware_templates` | `id` |
| 41 | `CreateTenant` | `tenants` | `id` |
| 42 | `CreateTicket` | `tickets` | `id` |
| 43 | `issueToken` | `install_tokens` | `token` |
| 44 | `CreatePolicy` | `traffic_policies` | `id` |
| 45 | `CreateWebhook` | `webhooks` | `id` |

三个特例不进入 45 个通用调用计数：

| Func | Table | Conflict | 特例 |
|---|---|---|---|
| `RenewLeadership` | `leader_lease` | `id` | 条件 `CASE WHEN` |
| `UpsertABACPolicyRecord` | `abac_policies` | `tenant_id,id` | `version+1` |
| `CreateLifecycleJob` | `lifecycle_jobs` | `id` | PG `updated_at=now()` |
