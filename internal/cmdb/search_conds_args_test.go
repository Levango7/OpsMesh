// search_conds_args_test.go 检索 SQL 的「条件/实参对齐」契约（自 search_fulltext_test.go 保留；
// TD-91：全文索引召回路径删除后，其余机制用例随机制一并移除，只留与召回路径无关、仍有长期价值的两条）。
//
// 为什么留下它们：`conds` 与 `args` 错位/顺序漂移不会编译失败，只会让结果恒空、或让 tenant_id
// 收到检索词——是静默失败里最难查的一类（与 root store 那次 SELECT/Scan 列数错位同族）。
package cmdb

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// captureSearchSQL 用 sqlmock 捕获 SearchCIs 生成的 SQL 文本与实参（不触碰真实库）。
func captureSearchSQL(t *testing.T, s *SQLCiStore, query, ciType string) (string, []driver.NamedValue) {
	t.Helper()
	var captured string
	var gotArgs []driver.NamedValue
	db, mock, err := sqlmock.New(
		sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(func(expected, actual string) error {
			captured = actual
			return nil
		})),
		sqlmock.ValueConverterOption(argRecorder{args: &gotArgs}),
	)
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	mock.MatchExpectationsInOrder(false)
	mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows(searchRowsCols()))

	old := s.db
	s.db = db
	defer func() { s.db = old }()
	if _, err := s.SearchCIs(context.Background(), "t1", CiSearchQuery{
		Query: query, CiType: ciType, Status: "active", Limit: 5,
	}); err != nil {
		t.Fatalf("SearchCIs: %v", err)
	}
	return captured, gotArgs
}

// argRecorder 是 sqlmock 的 ValueConverter：把每次绑定的实参按序记录下来。
type argRecorder struct {
	args *[]driver.NamedValue
}

func (r argRecorder) ConvertValue(v any) (driver.Value, error) {
	*r.args = append(*r.args, driver.NamedValue{Ordinal: len(*r.args) + 1, Value: v})
	return v, nil
}

// TestSearchCIsCondsArgsAligned conds 与 args 严格一一对应（占位符数 == 实参数个数）。
func TestSearchCIsCondsArgsAligned(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
	}{
		{"混合 token", "web 生产"},
		{"纯中文", "生产机房"},
		{"下划线", "server_prod"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &SQLCiStore{}
			sqlText, args := captureSearchSQL(t, s, tc.query, "machine")
			placeholders := strings.Count(sqlText, "?")
			if placeholders != len(args) {
				t.Errorf("占位符数 %d != 实参数 %d（conds 与 args 错位会静默返回空结果）\nSQL: %s\nargs: %v",
					placeholders, len(args), sqlText, args)
			}
		})
	}
}

// TestSearchCIsPreservesFilterConds 过滤条件（租户/状态/类型）与 LIMIT 必须保留，且过滤参数在前。
func TestSearchCIsPreservesFilterConds(t *testing.T) {
	s := &SQLCiStore{}
	sqlText, args := captureSearchSQL(t, s, "web 生产", "machine")
	for _, want := range []string{"tenant_id=?", "status=?", "ci_type=?", "LIMIT ?"} {
		if !strings.Contains(sqlText, want) {
			t.Errorf("缺少过滤条件 %q:\n%s", want, sqlText)
		}
	}
	// 参数顺序即绑定顺序：过滤参数在前，召回参数在后。
	// 顺序错乱不会编译失败，只会让 tenant_id 收到召回词，表现为结果恒空。
	wantHead := []string{"t1", "active", "machine"}
	if len(args) < len(wantHead) {
		t.Fatalf("参数数量不足: %v", args)
	}
	for i, w := range wantHead {
		if got := fmt.Sprint(args[i].Value); got != w {
			t.Errorf("第 %d 个参数 = %q, want %q（过滤参数必须在前，顺序错位会导致结果恒空且无报错）: %v",
				i, got, w, args)
		}
	}
}
