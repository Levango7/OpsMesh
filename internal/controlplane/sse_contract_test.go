package controlplane

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// sse_contract_test.go — SSE 实时推送的"代码↔文档"契约守护（TD-24）。
//
// 背景：docs/sse-protocol.md 对外承诺事件名与信封字段，若改代码不改文档，
// 前端将按过期契约解析。本测试强制二者一致：
//   1. 扫描本包源码中所有 publishEvent(..., "name", ...) 的字面量事件名；
//   2. 扫描 docs/sse-protocol.md 事件类型枚举表中的登记项；
//   3. 双向比对：代码事件必须在文档登记；文档事件（除握手帧 hello）必须在代码中存在。

// rePublishEvent 匹配 publishEvent 调用中的字面量事件名（第二参数）。
var rePublishEvent = regexp.MustCompile(`publishEvent\([^\n]*?"(?P<name>[a-z0-9_]+)"`)

// reDocEventRow 匹配文档枚举表行：`| `task_status` | ...` 取首列反引号内容。
var reDocEventRow = regexp.MustCompile("\\|\\s*`(?P<name>[a-z0-9_]+)`")

func TestSSEContract_CodeVsDocAlignment(t *testing.T) {
	// 1. 从本包源码收集 publishEvent 字面量事件名。
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读取包目录失败: %v", err)
	}
	codeEvents := map[string]bool{}
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", name, err)
		}
		scanned++
		for _, m := range rePublishEvent.FindAllStringSubmatch(string(data), -1) {
			codeEvents[m[1]] = true
		}
	}
	if scanned == 0 || len(codeEvents) == 0 {
		t.Fatalf("未扫到任何 publishEvent 字面量事件名（scanned=%d events=%d），契约测试失效", scanned, len(codeEvents))
	}

	// 2. 从 docs/sse-protocol.md 提取事件枚举表。
	docPath := filepath.Join("..", "..", "docs", "sse-protocol.md")
	doc, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", docPath, err)
	}
	docEvents := map[string]bool{}
	for _, m := range reDocEventRow.FindAllStringSubmatch(string(doc), -1) {
		docEvents[m[1]] = true
	}
	if len(docEvents) == 0 {
		t.Fatalf("文档 %s 中未提取到事件枚举表，契约测试失效", docPath)
	}

	// 3. 双向对齐。
	for ev := range codeEvents {
		if !docEvents[ev] {
			t.Errorf("代码事件 %q 未在 %s 枚举表登记（改了 publishEvent 请同步改文档）", ev, docPath)
		}
	}
	for ev := range docEvents {
		if ev == "hello" {
			continue // 握手帧由 handler 直写，不走 publishEvent。
		}
		if !codeEvents[ev] {
			t.Errorf("文档事件 %q 在代码中无对应 publishEvent 调用（删事件请同步删文档）", ev)
		}
	}
}
