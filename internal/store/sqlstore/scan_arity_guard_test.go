// scan_arity_guard_test.go — TD-90 的**接入断言**：SELECT 列数 ↔ 消费侧 Scan 目标数 必须一致。
//
// 机制（解析查询调用、字面量求值、列数计数、三种消费形态、helper 目标数）已下沉到
// `pkg/sqlguard`（单一实现，各服务/store 共用）；**本文件只保留本包的政策**：
// 覆盖率棘轮下限、宽站点豁免账、判红文案与诊断模式。
//
// # 缺陷形态（已发生两次，均只在真库/生产暴露；守卫上线即抓住第二例）
//
// `rows.Scan` 列数不等时报 `expected N destination arguments in Scan, not M`，
// 调用方普遍只 storefail.Record 后返回 nil/continue ⇒ 该读路径**静默返回空列表**
// （内存后端正确故单测全绿）。历史实例：
//   - `GetK8sCluster`：SELECT 7 列 / `scanK8sCluster` 8 个目标 ⇒ 按 ID 查/改/删与租户归属校验恒失败；
//   - `GetTasks`：SELECT 8 列（漏 tenant_id）/ `scanTaskListRow` 9 个目标 ⇒ MySQL 后端恒返回空列表
//     （真库日志：`[store] GetTasks 扫描失败: sql: expected 8 destination arguments in Scan, not 9`）。
package sqlstore

import (
	"os"
	"sort"
	"testing"

	"github.com/Levango7/OpsMesh/pkg/sqlguard"
)

// arityJudgedFloor 参与比对的站点数棘轮下限（只许上调）。
// 128 = 2026-10-10 下沉 pkg/sqlguard 并加跨文件常量解析后的实测值（不可解析 5、列数不可判 3，其余参与比对）。
// 覆盖变差必须有人写下理由——防「门禁悄悄失去牙齿」。
const arityJudgedFloor = 128

// arityWideExemptions 是「消费侧 ≥3 而 SQL 不可判」的显式账目（键 file#fn#var）。
// 当前为空：三种消费形态与包级/函数级字面量展开已覆盖全部宽站点。
// 空表也是合法状态；条目必须带理由，且站点消失/被解析成功后必须删除（过期即判红）。
var arityWideExemptions = map[string]string{}

// TestSelectColumnCountMatchesScanTargets 本包的对账门禁（TD-90）。
func TestSelectColumnCountMatchesScanTargets(t *testing.T) {
	res, err := sqlguard.Analyze(".")
	if err != nil {
		t.Fatalf("sqlguard.Analyze: %v", err)
	}
	if res.Judged < arityJudgedFloor {
		t.Errorf("参与比对的站点只有 %d 个（< 下限 %d）——解析或匹配逻辑失效，本门禁在空跑；"+
			"先跑 OPSMESH_ARITY_REPORT=1 看明细", res.Judged, arityJudgedFloor)
	}
	if len(res.Mismatches) > 0 {
		sort.Strings(res.Mismatches)
		t.Errorf("以下站点的 SELECT 列数 ≠ 消费侧 Scan 目标数——database/sql 会直接报 "+
			"expected N destination arguments in Scan, not M，调用方多半只记 storefail 就返回空，"+
			"该读路径在真库上静默失效（GetK8sCluster、GetTasks 两次真实缺陷即此形态）：\n  %s",
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
		t.Errorf("以下宽扫描站点（消费侧 ≥3 个目标）静态判不出 SELECT 列数，且未登记豁免：\n  %s\n"+
			"先尝试扩展解析（字面量/常量拼接的最常见形态已覆盖）；确属运行时拼接的，带理由登记为豁免",
			joinLines(unregistered))
	}
	for key := range arityWideExemptions {
		if _, ok := res.WideUnjudged[key]; !ok {
			t.Errorf("arityWideExemptions 的条目 %q 已不对应任何未判定站点（解析已覆盖或站点已删）——"+
				"请清理，否则「曾经合理」会变成永久免检", key)
		}
	}
}

// TestSelectArityReport 诊断模式（不判红）：设 OPSMESH_ARITY_REPORT=1 打印明细，
// 供扩展解析能力、调整下限、排查失明站点时看真实数据。
func TestSelectArityReport(t *testing.T) {
	if os.Getenv("OPSMESH_ARITY_REPORT") == "" {
		t.Skip("报告模式：设 OPSMESH_ARITY_REPORT=1 运行")
	}
	res, err := sqlguard.Analyze(".")
	if err != nil {
		t.Fatalf("sqlguard.Analyze: %v", err)
	}
	t.Logf("判定一致 %d（下限 %d）；SQL 不可解析 %d；列数不可判 %d；无消费侧 %d；宽站点未判定 %d",
		res.Judged, arityJudgedFloor, res.Unresolved, res.Uncountable, res.NoConsumer, len(res.WideUnjudged))
	forms := make([]string, 0, len(res.Forms))
	for k := range res.Forms {
		forms = append(forms, k)
	}
	sort.Strings(forms)
	for _, k := range forms {
		t.Logf("  消费形态：%-8s %d", k, res.Forms[k])
	}
	for _, l := range res.Mismatches {
		t.Logf("  MISMATCH %s", l)
	}
	for key, desc := range res.WideUnjudged {
		t.Logf("  WIDE-UNJUDGED %s → %s", key, desc)
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
