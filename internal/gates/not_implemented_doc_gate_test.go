package gates

// not_implemented_doc_gate_test.go —— 「刻意未接线（显式 501）」能力的**文档 ↔ 实现**漂移钉。
//
// 缺陷原型（2026-11-16 逐端点对账）：控制面有一批能力被**刻意**改成显式 501 以消除
// 假成功（`ha/failover` 曾回 200 accepted、任务 `retry` 只取消不重建、代理模式
// `dead-letter` 清除回 200），但文档没跟上 —— 有的条目留在旧文案（`ha/failover`，
// 已由 ha_failover_doc_impl_gate_test.go 单钉），**有的端点则完全没写 501**
// （`tasks/{id}/retry`、`tasks/{id}/dead-letter`、`deploys/{id}/auto-advance`），
// 还有一整族端点在本文件里没有任何条目（`/api/v1/ai/agent/*` 六个）。
//
// 判据（双向）：
//   ① 文档「未接线能力（显式 501 清单）」小节必须包含下面每条路由；
//   ② 对应实现文件必须仍含 `StatusNotImplemented`（防"文档写了 501、实现已回退成假成功"）。
//
// 为什么值得单列：本仓已登记的最高频主题即「措辞/宣称领先于代码」。501 是**安全语义**
// —— 把它写成 200 会让运维以为切主/重试/清死信已经发生。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// notImplementedRoutes 是「刻意未接线」的 HTTP 端点及其实现文件。
// 新增一条 501 能力时必须同时进这张表，否则文档清单里就没有它（判据 ① 反向不成立时
// 说明这张表过期了，同样要改）。
var notImplementedRoutes = []struct {
	route string // 文档清单里应出现的形式
	file  string // 仓库相对路径；该文件须含 http.StatusNotImplemented
}{
	{"/api/v1/ha/failover", "internal/controlplane/ha.go"},
	{"/api/v1/audit/verify", "internal/controlplane/audit_query.go"},
	{"/api/v1/tasks/{id}/retry", "internal/controlplane/server_lifecycle.go"},
	{"/api/v1/tasks/{id}/dead-letter", "internal/controlplane/server_lifecycle.go"},
	{"/api/v1/deploys/{id}/auto-advance", "internal/deploy/handler.go"},
	{"/api/v1/logs", "internal/logstore/handler.go"},
	{"/api/v1/ai/agent/execute", "internal/ai/agent/handler.go"},
}

// docSection 取 title 起、到下一个 `\n## ` 或 `\n---` 为止的文本（与 ha 那条门禁各自独立，
// 避免相互依赖；两文件的判据同样不复制实现逻辑）。
func docSection(doc, title string) string {
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

func TestNotImplementedDocCoversImpl(t *testing.T) {
	root := repoRoot(t)

	docBytes, err := os.ReadFile(filepath.Join(root, "docs/api-reference.md"))
	if err != nil {
		t.Fatalf("读 docs/api-reference.md: %v", err)
	}
	sec := docSection(string(docBytes), "## 未接线能力（显式 501 清单）")
	if sec == "" {
		t.Fatal("docs/api-reference.md 缺少「未接线能力（显式 501 清单）」小节")
	}

	for _, e := range notImplementedRoutes {
		if !strings.Contains(sec, e.route) {
			t.Errorf("文档 501 清单缺少端点 %s —— 该能力回 501 但调用方在文档里看不到", e.route)
		}
		raw, err := os.ReadFile(filepath.Join(root, e.file))
		if err != nil {
			t.Fatalf("读实现文件 %s: %v", e.file, err)
		}
		if !strings.Contains(string(raw), "StatusNotImplemented") {
			t.Errorf("%s 里找不到 http.StatusNotImplemented —— 端点 %s 可能已回退成假成功，"+
				"或已真实接线；两者都要同步文档清单与本表", e.file, e.route)
		}
	}
}
