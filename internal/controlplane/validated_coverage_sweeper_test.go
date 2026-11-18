package controlplane

// validated_coverage_sweeper_test.go — 构造即校验迁移的**全局汇总护栏**（补 b29-b36 逐文件门禁之上）。
//
// 背景：controlplane 曾是把所有能力挂成 Server 方法的"上帝包"。earlier history 用
// validated* 型式收口了 7 个 ops 文件（db/vector/k8s_provision/monitor/middleware/mongo/ansible），
// 每个都有 bNN 逐文件门禁。本测试作为 sweeper：
//   - 一眼汇总所有已迁移文件的 build* 入参是否仍是 validated*（防回潮）；
//   - 新增迁移文件只需在 migratedFiles 加一行，自动纳入扫描。
//
// 与 b29-b36 互补，不替代它们（那些还钉了 handler 白名单、脚本内容等更细判据）。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// migratedFiles 已完成 validated* 迁移的文件；新增迁移在此登记即自动受本护栏保护。
var migratedFiles = []string{
	"db_ops.go",
	"vector_ops.go",
	"k8s_provision.go",
	"monitor_ops.go",
	"middleware_ops.go",
	"mongo_ops.go",
	"ansible_ops.go",
}

// TestValidatedMigrationSweeper 判据：每个已迁移文件的 build* 函数，除 *strings.Builder /
// string / int / bool 外，入参类型必须以 validated 开头（不接受原始 *Req 类型）。
func TestValidatedMigrationSweeper(t *testing.T) {
	checked := 0
	for _, f := range migratedFiles {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Errorf("解析 %s 失败：%v", f, err)
			continue
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !strings.HasPrefix(fn.Name.Name, "build") {
				continue
			}
			checked++
			for _, field := range fn.Type.Params.List {
				ts := sweeperTypeString(field.Type)
				switch ts {
				case "*strings.Builder", "string", "int", "bool", "*strings.Builder)":
					continue
				}
				ts = strings.TrimPrefix(ts, "*")
				if !strings.HasPrefix(ts, "validated") {
					t.Errorf("%s:%d build %s 入参类型 %q 非 validated*（构造即校验回潮）",
						f, fset.Position(field.Pos()).Line, fn.Name.Name, ts)
				}
			}
		}
	}
	if checked < 40 {
		t.Errorf("sweeper 只检查到 %d 个 build 函数，疑似迁移文件登记不全（期望 >=40）", checked)
	}
}

func sweeperTypeString(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.StarExpr:
		return "*" + sweeperTypeString(x.X)
	case *ast.SelectorExpr:
		return sweeperTypeString(x.X) + "." + x.Sel.Name
	default:
		return ""
	}
}
