// arity_guard_test.go — SQL 读路径的「SELECT 列数 ↔ 消费侧 Scan 目标数」对账接入断言（TD-90）。
//
// 机制在 `pkg/sqlguard`（单一实现，各服务/store 共用）：解析查询调用、字面量求值（包级/函数级、
// 跨文件、跨行拼接）、列数计数（词边界感知）、四种消费形态（rows.Scan / 链式 / scanX(row) /
// scanX(row.Scan)），并解析 `x.dest()...` 展开。本文件只保留本包的政策：覆盖率棘轮下限与豁免账。
//
// 接入实测基线（2026-10-10）：judged=13、mismatch=0、wide=0。
// 缺陷形态（根 store 已发生两次、均在真库暴露）：`rows.Scan` 列数不等 ⇒ database/sql 报
// `expected N destination arguments in Scan, not M`，调用方多半只记日志后返回空 ⇒ 读路径静默失效。
package store

import (
	"os"
	"sort"
	"testing"

	"github.com/Levango7/OpsMesh/pkg/sqlguard"
)

// arityJudgedFloor 参与比对的站点数棘轮下限（只许上调；覆盖变差必须有人写下理由）。
const arityJudgedFloor = 13

// arityWideExemptions 是「消费侧 ≥3 而 SQL 不可判」的显式账目（键 file#fn#var），空表也合法。
var arityWideExemptions = map[string]string{}

func TestSelectColumnCountMatchesScanTargets(t *testing.T) {
	res, err := sqlguard.Analyze(".")
	if err != nil {
		t.Fatalf("sqlguard.Analyze: %v", err)
	}
	if res.Judged < arityJudgedFloor {
		t.Errorf("参与比对的站点只有 %d 个（< 下限 %d）——解析或匹配逻辑失效，本门禁在空跑", res.Judged, arityJudgedFloor)
	}
	if len(res.Mismatches) > 0 {
		sort.Strings(res.Mismatches)
		t.Errorf("以下站点的 SELECT 列数 ≠ 消费侧 Scan 目标数——database/sql 会直接报 "+
			"expected N destination arguments in Scan, not M，读路径在真库上静默失效：\n  %s",
			joinLines(res.Mismatches))
	}
	var unregistered []string
	for key, desc := range res.WideUnjudged {
		if _, ok := arityWideExemptions[key]; !ok {
			unregistered = append(unregistered, desc)
		}
	}
	if len(unregistered) > 0 {
		sort.Strings(unregistered)
		t.Errorf("以下宽扫描站点静态判不出 SELECT 列数且未登记豁免：\n  %s\n"+
			"确属运行时拼接的，带理由登记；否则先扩展解析或改写入参形态", joinLines(unregistered))
	}
	for key := range arityWideExemptions {
		if _, ok := res.WideUnjudged[key]; !ok {
			t.Errorf("arityWideExemptions 条目 %q 已不对应未判定站点——请清理，避免永久免检", key)
		}
	}
}

// TestSelectArityReport 诊断模式：OPSMESH_ARITY_REPORT=1 时打印明细（不判红）。
func TestSelectArityReport(t *testing.T) {
	if os.Getenv("OPSMESH_ARITY_REPORT") == "" {
		t.Skip("报告模式：设 OPSMESH_ARITY_REPORT=1 运行")
	}
	res, err := sqlguard.Analyze(".")
	if err != nil {
		t.Fatalf("sqlguard.Analyze: %v", err)
	}
	t.Logf("判定一致 %d（下限 %d）；不可解析 %d；不可判 %d；无消费侧 %d；宽站点 %d",
		res.Judged, arityJudgedFloor, res.Unresolved, res.Uncountable, res.NoConsumer, len(res.WideUnjudged))
	for _, l := range res.Mismatches {
		t.Logf("  MISMATCH %s", l)
	}
	for key, desc := range res.WideUnjudged {
		t.Logf("  WIDE %s → %s", key, desc)
	}
}

func joinLines(xs []string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += "\n  "
		}
		out += x
	}
	return out
}
