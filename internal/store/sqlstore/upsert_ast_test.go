package sqlstore

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// upsertCallSpec 描述一处 s.dialect.Upsert 调用点的元数据。
// Conflict 为空切片表示调用点传了 nil conflict（收集器不丢弃，
// 由最终守卫报红）。
type upsertCallSpec struct {
	File, Func, Table                  string
	Cols, Values, Conflict, UpdateCols []string
	Pos                                token.Position
}

// sqlstoreScanRoot 返回本仓 internal/store/sqlstore 的绝对路径。
// 由本测试文件位置推导，与运行时工作目录无关，保证扫描的是
// sqlstore 包树而非“当前目录”。
func sqlstoreScanRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller 无法定位测试文件")
	}
	// <repo>/internal/store/sqlstore/upsert_ast_test.go → <repo>
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(thisFile))))
	return filepath.Join(repoRoot, "internal", "store", "sqlstore")
}

// sqlstoreProductionFiles 用 filepath.WalkDir 递归扫描
// internal/store/sqlstore，返回全部非 _test.go 的生产 .go 文件。
func sqlstoreProductionFiles(t *testing.T) []string {
	t.Helper()
	root := sqlstoreScanRoot(t)
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return files
}

// collectUpsertCalls 解析 files 中的每个 Go 源文件，收集所有
// s.dialect.Upsert 调用点的元数据。仅收集接收者末段为 dialect、
// 方法名为 Upsert 且恰好 5 个参数的调用；五个参数必须全部是
// 字符串字面量或字符串字面量复合字面量（nil conflict 除外），
// 否则 Fatalf 拒绝——不允许动态表名、列名或冲突目标。
func collectUpsertCalls(t *testing.T, files []string) []upsertCallSpec {
	t.Helper()
	var out []upsertCallSpec
	for _, file := range files {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			decl, ok := n.(*ast.FuncDecl)
			if !ok {
				return true
			}
			ast.Inspect(decl, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Upsert" {
					return true
				}
				recv, ok := sel.X.(*ast.SelectorExpr)
				if !ok || recv.Sel.Name != "dialect" {
					return true
				}
				if len(call.Args) != 5 {
					t.Fatalf("%s: Upsert 必须恰好 5 个参数", fset.Position(call.Pos()))
				}
				out = append(out, upsertCallSpec{
					File:       file,
					Func:       decl.Name.Name,
					Table:      literalString(t, call.Args[0]),
					Cols:       literalStrings(t, call.Args[1]),
					Values:     literalStrings(t, call.Args[2]),
					Conflict:   literalStrings(t, call.Args[3]),
					UpdateCols: literalStrings(t, call.Args[4]),
					Pos:        fset.Position(call.Pos()),
				})
				return true
			})
			return true
		})
	}
	return out
}

// literalString 解析字符串字面量；非字面量直接 Fatalf。
func literalString(t *testing.T, expr ast.Expr) string {
	t.Helper()
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		t.Fatalf("Upsert 参数必须是字符串字面量，实际为 %T", expr)
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		t.Fatalf("unquote %s: %v", lit.Value, err)
	}
	return s
}

// literalStrings 解析 []string 复合字面量（元素均为字符串字面量）；
// nil 字面量解析为空切片。非字面量直接 Fatalf。
func literalStrings(t *testing.T, expr ast.Expr) []string {
	t.Helper()
	if ident, ok := expr.(*ast.Ident); ok && ident.Name == "nil" {
		return nil
	}
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		t.Fatalf("Upsert 参数必须是 []string 字面量或 nil，实际为 %T", expr)
	}
	out := make([]string, 0, len(lit.Elts))
	for _, elt := range lit.Elts {
		out = append(out, literalString(t, elt))
	}
	return out
}

// TestCollectUpsertCalls 用内存源码验证收集器的五种形态：
// 合法的 s.dialect.Upsert 调用、nil conflict 调用（收集但 Conflict 为空）、
// 非 dialect 接收者的 Upsert 调用（忽略）。
func TestCollectUpsertCalls(t *testing.T) {
	src := `package sqlstore
func f(s *SQLStore) {
    s.dialect.Upsert("t", []string{"id", "v"}, []string{"?", "NOW()"}, []string{"id"}, []string{"v"})
    s.dialect.Upsert("bad", []string{"id"}, []string{"?"}, nil, []string{"id"})
    other.Upsert("ignored", []string{"id"}, []string{"?"}, []string{"id"}, []string{"id"})
}`
	dir := t.TempDir()
	file := filepath.Join(dir, "probe.go")
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatalf("write probe source: %v", err)
	}

	got := collectUpsertCalls(t, []string{file})
	if len(got) != 2 {
		t.Fatalf("collectUpsertCalls: got %d specs, want 2 (only s.dialect.Upsert): %+v", len(got), got)
	}

	want := []upsertCallSpec{
		{
			Func:       "f",
			Table:      "t",
			Cols:       []string{"id", "v"},
			Values:     []string{"?", "NOW()"},
			Conflict:   []string{"id"},
			UpdateCols: []string{"v"},
		},
		{
			Func:       "f",
			Table:      "bad",
			Cols:       []string{"id"},
			Values:     []string{"?"},
			Conflict:   nil,
			UpdateCols: []string{"id"},
		},
	}
	for i, w := range want {
		g := got[i]
		if g.Func != w.Func {
			t.Errorf("spec %d Func = %q, want %q", i, g.Func, w.Func)
		}
		if g.Table != w.Table {
			t.Errorf("spec %d Table = %q, want %q", i, g.Table, w.Table)
		}
		if !equalStrings(g.Cols, w.Cols) {
			t.Errorf("spec %d Cols = %v, want %v", i, g.Cols, w.Cols)
		}
		if !equalStrings(g.Values, w.Values) {
			t.Errorf("spec %d Values = %v, want %v", i, g.Values, w.Values)
		}
		if !equalStrings(g.Conflict, w.Conflict) {
			t.Errorf("spec %d Conflict = %v, want %v (nil conflict 必须保留为空，不得丢弃)", i, g.Conflict, w.Conflict)
		}
		if !equalStrings(g.UpdateCols, w.UpdateCols) {
			t.Errorf("spec %d UpdateCols = %v, want %v", i, g.UpdateCols, w.UpdateCols)
		}
		if g.File != file {
			t.Errorf("spec %d File = %q, want %q", i, g.File, file)
		}
		if g.Pos.Filename == "" || g.Pos.Line == 0 {
			t.Errorf("spec %d Pos = %+v, want a real source position", i, g.Pos)
		}
	}
}

// TestSqlstoreProductionFiles 断言扫描器递归覆盖 internal/store/sqlstore
// 整棵目录树、排除 _test.go，且至少返回 40 个生产 .go 文件。
func TestSqlstoreProductionFiles(t *testing.T) {
	files := sqlstoreProductionFiles(t)
	if len(files) < 40 {
		t.Fatalf("sqlstoreProductionFiles: got %d production files, want >= 40", len(files))
	}

	root := sqlstoreScanRoot(t)
	for _, f := range files {
		if filepath.Dir(f) != root {
			// 递归扫描允许子目录文件，但必须仍位于 sqlstore 树下。
			if !isUnderRoot(f, root) {
				t.Fatalf("file %q escapes scan root %q", f, root)
			}
		}
		if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") {
			t.Fatalf("non-production file returned: %q", f)
		}
	}

	// 行为性证明“不得只扫当前目录”：在 sqlstore 树下临时放入一个嵌套
	// 子目录的生产 .go 文件，收集器必须能扫到它（扫描后立即清理）。
	probeDir := filepath.Join(root, "zz_upsert_ast_probe")
	if err := os.MkdirAll(probeDir, 0o755); err != nil {
		t.Fatalf("mkdir probe dir: %v", err)
	}
	probe := filepath.Join(probeDir, "zz_probe.go")
	if err := os.WriteFile(probe, []byte("package sqlstore\n"), 0o644); err != nil {
		t.Fatalf("write probe file: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(probeDir) })

	files = sqlstoreProductionFiles(t)
	found := false
	for _, f := range files {
		if f == probe {
			found = true
		}
	}
	if !found {
		t.Fatalf("sqlstoreProductionFiles did not recurse into %q (nested file %q missing)", probeDir, probe)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func isUnderRoot(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
