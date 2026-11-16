package gates

// ha_failover_doc_impl_gate_test.go —— 「文档宣称 vs 实现」漂移钉子（单端点）。
//
// 缺陷原型（2026-11-16 实测）：`POST /api/v1/ha/failover` 的**实现**在审计整改批次 1
// 已把假成功 200 改成显式 501（`internal/controlplane/ha.go` 的 `handleHAFailover`，
// 注释写明"原实现返回 200 accepted 但不做任何切换，属假成功"），
// 但 `docs/api-reference.md` 仍写「响应 `200 OK`」+ 响应体
// `{"status":"accepted","message":"failover triggered..."}` ——
// 而同一节的前言已经写着「本端点不直接执行切换」，**文档自相矛盾**。
//
// 判据（双向，防两个方向的漂移）：
//   ① 实现必须仍返回 501 —— 不允许回退成"宣称已接受但不干活"的假成功；
//   ② 文档该小节必须写 501，且不得再出现旧的成功文案。
//
// 为什么单列这一条：本仓已登记的最高频主题即「措辞/宣称领先于代码」。路径类漂移有
// doc_paths_gate、能力证据类有 capability_evidence_gate，而「同一能力的状态码在
// 文档与实现之间对不上」此前无门禁覆盖。这里先钉住已发生的这一条。

import (
	"os"
	"strings"
	"testing"
)

func TestHAFailoverDocMatchesImpl(t *testing.T) {
	root := repoRoot(t)

	impl, err := os.ReadFile(root + "/internal/controlplane/ha.go")
	if err != nil {
		t.Fatalf("读 ha.go 失败: %v", err)
	}
	if !strings.Contains(string(impl), "http.StatusNotImplemented") {
		t.Error("ha.go 不再返回 501 —— 若确已接线真实切主，请同步更新本门禁与文档；" +
			"若只是回退成 200 假成功，那是退步，必须拦住")
	}

	doc, err := os.ReadFile(root + "/docs/api-reference.md")
	if err != nil {
		t.Fatalf("读 api-reference.md 失败: %v", err)
	}
	sec := section(string(doc), "### POST /api/v1/ha/failover")
	if sec == "" {
		t.Fatal("api-reference.md 缺少 `### POST /api/v1/ha/failover` 小节")
	}
	// 判据必须钉「响应行」而不是「小节内出现过 501」——后者会被同节的说明文字判满，
	// 实测变异（把响应行改回 200 OK）下仍绿，是真实的逃逸。
	if !strings.Contains(sec, "**响应**：`501 Not Implemented`") {
		t.Error("api-reference.md 的 ha/failover 响应行未写 `501 Not Implemented` —— 与实现的显式 501 对不上，" +
			"调用方会按文档以为切主已触发（这正是批次 1 想消除的假成功）")
	}
	if !strings.Contains(sec, "ERR_NOT_IMPLEMENTED") {
		t.Error("api-reference.md 的 ha/failover 响应示例缺少 ERR_NOT_IMPLEMENTED —— 与实现的响应体对不上")
	}
	for _, stale := range []string{"failover triggered", `"status": "accepted"`} {
		if strings.Contains(sec, stale) {
			t.Errorf("api-reference.md 的 ha/failover 小节仍残留旧成功文案 %q —— 实现并不执行切换", stale)
		}
	}
}

// section 取 title 起、到下一个同级/更高级标题或 `---` 分隔线为止的文本。
func section(doc, title string) string {
	i := strings.Index(doc, title)
	if i < 0 {
		return ""
	}
	rest := doc[i+len(title):]
	if j := strings.Index(rest, "\n## "); j >= 0 {
		rest = rest[:j]
	}
	if j := strings.Index(rest, "\n---"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}
