// sqlguard_test.go 机制自身的合成用例：用最小 Go 片段钉住判据，
// 不依赖任何真实 store（对真实代码的接入断言在各包的 *_guard_test.go 里）。
package sqlguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writePkg 把若干「文件名 → 内容」写进临时目录并返回目录。
func writePkg(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("写 %s: %v", name, err)
		}
	}
	return dir
}

func analyze(t *testing.T, files map[string]string) Result {
	t.Helper()
	res, err := Analyze(writePkg(t, files))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	return res
}

const pkgHead = "package fake\n\nimport (\n\t\"context\"\n\t\"database/sql\"\n)\n\nvar ctx = context.Background()\n\n"

func TestDirectScanConsistent(t *testing.T) {
	res := analyze(t, map[string]string{"a.go": pkgHead + `
func f(db *sql.DB) {
	rows, err := db.QueryContext(ctx, "SELECT id, name FROM t")
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		rows.Scan(&id, &name)
	}
}
`})
	if res.Judged != 1 || len(res.Mismatches) != 0 {
		t.Fatalf("judged=%d mismatches=%v, want 1/0", res.Judged, res.Mismatches)
	}
}

func TestDirectScanMismatchIsReported(t *testing.T) {
	res := analyze(t, map[string]string{"a.go": pkgHead + `
func f(db *sql.DB) {
	rows, _ := db.QueryContext(ctx, "SELECT id, name FROM t")
	for rows.Next() {
		var id, name, extra string
		rows.Scan(&id, &name, &extra)
	}
}
`})
	if len(res.Mismatches) != 1 {
		t.Fatalf("want 1 mismatch, got %v", res.Mismatches)
	}
	if !strings.Contains(res.Mismatches[0], "SELECT 2 列 vs rows(direct) 扫 3 个目标") {
		t.Errorf("mismatch 文案未含两侧列数: %s", res.Mismatches[0])
	}
}

func TestChainedScan(t *testing.T) {
	res := analyze(t, map[string]string{"a.go": pkgHead + `
func f(db *sql.DB) {
	var a, b, c string
	err := db.QueryRowContext(ctx, "SELECT x, y, z FROM t").Scan(&a, &b, &c)
	_ = err
}
`})
	if res.Judged != 1 || len(res.Mismatches) != 0 {
		t.Fatalf("judged=%d mismatches=%v, want 1/0（链式 Scan 应参与比对）", res.Judged, res.Mismatches)
	}
	if res.Forms["chained"] != 1 {
		t.Errorf("forms=%v，期望 chained=1", res.Forms)
	}
}

func TestHelperArityComesFromHelperBody(t *testing.T) {
	// helper 形态：SELECT 在调用方、Scan 在 helper 体内——两个缺陷实例都是这个形态。
	files := map[string]string{"a.go": pkgHead + `
type T struct{ A, B string }

type rowScanner interface{ Scan(dest ...any) error }

func scanT(row rowScanner) *T {
	var t T
	var a, b sql.NullString
	if err := row.Scan(&a, &b, &t.A); err != nil {
		return nil
	}
	return &t
}

func Get(db *sql.DB) *T {
	row := db.QueryRowContext(ctx, "SELECT a, b FROM t WHERE id=1")
	return scanT(row)
}
`}
	res := analyze(t, files)
	if len(res.Mismatches) != 1 {
		t.Fatalf("want 1 mismatch（SELECT 2 vs helper 3）, got %v", res.Mismatches)
	}
	if !strings.Contains(res.Mismatches[0], "scanT(helper) 扫 3 个目标") {
		t.Errorf("mismatch 未点名 helper: %s", res.Mismatches[0])
	}
	// 修好 SELECT 后应一致。
	files["a.go"] = strings.Replace(files["a.go"], "SELECT a, b FROM t", "SELECT a, b, c FROM t", 1)
	res2 := analyze(t, files)
	if len(res2.Mismatches) != 0 || res2.Judged != 1 {
		t.Fatalf("修正后 want 0 mismatch/1 judged, got %v/%d", res2.Mismatches, res2.Judged)
	}
}

func TestConstConcatAndLocalVarResolve(t *testing.T) {
	res := analyze(t, map[string]string{"a.go": pkgHead + `
const tCols = "id, name, " +
	"created_at"

func f(db *sql.DB) {
	q := "SELECT " + tCols + " FROM t"
	rows, _ := db.QueryContext(ctx, q)
	for rows.Next() {
		var id, name, createdAt string
		rows.Scan(&id, &name, &createdAt)
	}
}
`})
	if res.Judged != 1 || len(res.Mismatches) != 0 {
		t.Fatalf("judged=%d mismatches=%v（跨行常量拼接 + 函数内 q 应可解析）, want 1/0", res.Judged, res.Mismatches)
	}
}

func TestAmbiguousConstIsNotJudged(t *testing.T) {
	// 同名两值（不同函数各写一份 q）⇒ 弃判而不是猜错。
	res := analyze(t, map[string]string{"a.go": pkgHead + `
func f(db *sql.DB) {
	q := "SELECT a FROM t1"
	rows, _ := db.QueryContext(ctx, q)
	rows.Scan(&rows)
}

func g(db *sql.DB) {
	q := "SELECT a, b FROM t2"
	rows, _ := db.QueryContext(ctx, q)
	for rows.Next() {
		var a, b string
		rows.Scan(&a, &b)
	}
}
`})
	// f 的 q 与 g 的 q 同名不同值：两者都不判（弃判），但都不应产生 mismatch。
	if len(res.Mismatches) != 0 {
		t.Fatalf("弃判不应产生 mismatch: %v", res.Mismatches)
	}
}

func TestStarUncountableButAggregateJudged(t *testing.T) {
	// `SELECT *` 数不出列数 ⇒ 弃判（uncountable）；`SELECT COUNT(*)` 是**一个**表达式列
	// ⇒ 照常参与比对（与 1 个 Scan 目标一致），这比"聚合就跳过"更有牙齿。
	res := analyze(t, map[string]string{"a.go": pkgHead + `
func f(db *sql.DB) {
	rows, _ := db.QueryContext(ctx, "SELECT * FROM t")
	for rows.Next() {
		var a, b string
		rows.Scan(&a, &b)
	}
}

func g(db *sql.DB) {
	var n int
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM t").Scan(&n)
}
`})
	if len(res.Mismatches) != 0 {
		t.Fatalf("弃判/一致都不应判红: %v", res.Mismatches)
	}
	if res.Uncountable != 1 {
		t.Errorf("uncountable=%d, want 1（只有 SELECT * 不可判）", res.Uncountable)
	}
	if res.Judged != 1 {
		t.Errorf("judged=%d, want 1（COUNT(*) 与 1 个目标应参与比对）", res.Judged)
	}
}

func TestWideUnjudgedIsKeyedAndListed(t *testing.T) {
	// 消费侧 ≥3 而 SQL 不可判 ⇒ 进 WideUnjudged（键 file#fn#var），调用方据此登记豁免。
	res := analyze(t, map[string]string{"a.go": pkgHead + `
type rowScanner interface{ Scan(dest ...any) error }

func scanBig(row rowScanner) error {
	var a, b, c, d string
	return row.Scan(&a, &b, &c, &d)
}

func f(db *sql.DB, dynamic string) {
	rows, _ := db.QueryContext(ctx, dynamic)
	for rows.Next() {
		scanBig(rows)
	}
}
`})
	if len(res.WideUnjudged) != 1 {
		t.Fatalf("want 1 wide-unjudged, got %v", res.WideUnjudged)
	}
	for key := range res.WideUnjudged {
		if key != "a.go#f#rows" {
			t.Errorf("键应为 file#fn#var，got %q", key)
		}
	}
}

func TestGoFilesSkipsTests(t *testing.T) {
	dir := writePkg(t, map[string]string{"a.go": "package fake\n", "b_test.go": "package fake\n"})
	files, err := GoFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || !strings.HasSuffix(files[0], "a.go") {
		t.Fatalf("GoFiles=%v，应只含非测试文件", files)
	}
}

func TestFromSubstringInColumnNameDoesNotTruncateList(t *testing.T) {
	// `from_replicas` 里的 "from" 曾被当成子句边界 ⇒ 10 列被数成 5 列并误报不一致。
	res := analyze(t, map[string]string{"a.go": pkgHead + `
func f(db *sql.DB) {
	rows, _ := db.QueryContext(ctx, "SELECT id, rule_id, from_replicas, to_replicas, reason FROM t")
	for rows.Next() {
		var a, b, c, d, e string
		rows.Scan(&a, &b, &c, &d, &e)
	}
}
`})
	if res.Judged != 1 || len(res.Mismatches) != 0 {
		t.Fatalf("judged=%d mismatches=%v（from_replicas 不得截断列表）, want 1/0", res.Judged, res.Mismatches)
	}
}

func TestCamelCaseColumnCounts(t *testing.T) {
	// 真实列名不都是 snake_case（device 表的 lastHeartbeat）；按驼峰拒判会让整类站点失明。
	res := analyze(t, map[string]string{"a.go": pkgHead + `
func f(db *sql.DB) {
	rows, _ := db.QueryContext(ctx, "SELECT id, lastHeartbeat, ` + "`group`" + ` FROM t")
	for rows.Next() {
		var a, b, c string
		rows.Scan(&a, &b, &c)
	}
}
`})
	if res.Judged != 1 || res.Uncountable != 0 || len(res.Mismatches) != 0 {
		t.Fatalf("judged=%d uncountable=%d mismatches=%v, want 1/0/0", res.Judged, res.Uncountable, res.Mismatches)
	}
}

func TestSpreadDestResolvesByLocalType(t *testing.T) {
	// alert 的 `nullAlert.dest()` 形态：方法名跨类型重名 ⇒ 靠局部变量类型限定名解析。
	res := analyze(t, map[string]string{"a.go": pkgHead + `
type nullA struct{ a, b, c sql.NullString }
type nullB struct{ x, y sql.NullString }

func (n *nullA) dest() []any {
	return []any{&n.a, &n.b, &n.c}
}

func (n *nullB) dest() []any {
	return []any{&n.x, &n.y}
}

func f(db *sql.DB) {
	rows, _ := db.QueryContext(ctx, "SELECT a, b, c FROM t")
	for rows.Next() {
		var na nullA
		rows.Scan(na.dest()...)
	}
}
`})
	if res.Judged != 1 || len(res.Mismatches) != 0 {
		t.Fatalf("judged=%d mismatches=%v（应经 nullA.dest 解析出 3 个目标）, want 1/0", res.Judged, res.Mismatches)
	}
	// 反过来：类型不同、目标数不同时必须判出不一致（证明解析真的按类型走）。
	res2 := analyze(t, map[string]string{"a.go": pkgHead + `
type nullB struct{ x, y sql.NullString }

func (n *nullB) dest() []any {
	return []any{&n.x, &n.y}
}

func f(db *sql.DB) {
	rows, _ := db.QueryContext(ctx, "SELECT a, b, c FROM t")
	for rows.Next() {
		var nb nullB
		rows.Scan(nb.dest()...)
	}
}
`})
	if len(res2.Mismatches) != 1 {
		t.Fatalf("want 1 mismatch（SELECT 3 vs nullB.dest 2）, got %v", res2.Mismatches)
	}
}

func TestScanFuncValueHelper(t *testing.T) {
	// `scanRunbook(rows.Scan)` 形态：目标数来自 helper 内对 scan 参数的那次调用。
	res := analyze(t, map[string]string{"a.go": pkgHead + `
func scanR(scan func(dest ...any) error) error {
	var a, b, c string
	return scan(&a, &b, &c)
}

func f(db *sql.DB) {
	rows, _ := db.QueryContext(ctx, "SELECT a, b FROM t")
	for rows.Next() {
		scanR(rows.Scan)
	}
}
`})
	if len(res.Mismatches) != 1 {
		t.Fatalf("want 1 mismatch（SELECT 2 vs scanR 3）, got %v", res.Mismatches)
	}
	if !strings.Contains(res.Mismatches[0], "scanR(rows.Scan)") {
		t.Errorf("mismatch 未点名 scan 方法值形态: %s", res.Mismatches[0])
	}
}

func TestCrossFileConstAndHelper(t *testing.T) {
	// 常量与 helper 定义在**另一个文件**：只按文件内解析会让整类站点失明。
	res := analyze(t, map[string]string{
		"consts.go": "package fake\n\nconst tCols = \"id, name, created_at\"\n",
		"scan.go":   "package fake\n\nimport \"database/sql\"\n\ntype rowScanner interface{ Scan(dest ...any) error }\n\nfunc scanX(row rowScanner) error {\n\tvar a, b, c string\n\treturn row.Scan(&a, &b, &c)\n}\n",
		"q.go":      pkgHead + "\nfunc f(db *sql.DB) {\n\trow := db.QueryRowContext(ctx, \"SELECT \"+tCols+\" FROM t\")\n\tscanX(row)\n}\n",
	})
	if res.Judged != 1 || len(res.Mismatches) != 0 {
		t.Fatalf("judged=%d mismatches=%v（跨文件常量与 helper 都应解析）, want 1/0", res.Judged, res.Mismatches)
	}
}
