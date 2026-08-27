package controlplane

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
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
	var rePublishEvent = regexp.MustCompile(`[pP]ublishEvent\([^\n]*?"(?P<name>[a-z0-9_]+)"`)

// reDocEventRow 匹配文档枚举表行：`| `task_status` | ...` 取首列反引号内容。
var reDocEventRow = regexp.MustCompile("\\|\\s*`(?P<name>[a-z0-9_]+)`")

func TestSSEContract_CodeVsDocAlignment(t *testing.T) {
	// 1. 从本包源码收集 publishEvent 字面量事件名。
	// 用 runtime.Caller 定位包目录而非依赖 cwd（编译后二进制从任意目录
	// 运行也能工作，避免 os.ReadDir(".") 读到错误目录导致契约测试失效）。
	_, thisFile, _, _ := runtime.Caller(0)
	pkgDir := filepath.Dir(thisFile)
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		t.Fatalf("读取包目录失败: %v", err)
	}
	codeEvents := map[string]bool{}
	scanned := 0
	var filesToScan []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		if e.IsDir() {
			if name == "grpc" {
				grpcDir := filepath.Join(pkgDir, name)
				grpcEntries, err := os.ReadDir(grpcDir)
				if err != nil {
					t.Fatalf("读取 grpc 子目录失败: %v", err)
				}
				for _, ge := range grpcEntries {
					if !ge.IsDir() && strings.HasSuffix(ge.Name(), ".go") {
						filesToScan = append(filesToScan, filepath.Join(grpcDir, ge.Name()))
					}
				}
			}
			continue
		}
		if strings.HasSuffix(name, ".go") {
			filesToScan = append(filesToScan, filepath.Join(pkgDir, name))
		}
	}
	for _, filePath := range filesToScan {
		data, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", filePath, err)
		}
		scanned++
		for _, m := range rePublishEvent.FindAllStringSubmatch(string(data), -1) {
			codeEvents[m[1]] = true
		}
	}
	if scanned == 0 || len(codeEvents) == 0 {
		t.Fatalf("未扫到任何 publishEvent 字面量事件名（scanned=%d events=%d），契约测试失效", scanned, len(codeEvents))
	}

	// 2. 从 docs/sse-protocol.md 提取事件枚举表（仓库根 docs/，由包目录上溯两级）。
	docPath := filepath.Join(filepath.Dir(filepath.Dir(pkgDir)), "docs", "sse-protocol.md")
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

// ssePayloadContract 关键事件的 data 必含字段（与 docs/sse-protocol.md 枚举表
// "data 关键字段"列及前端 web/enterprise/src/api/sse.js EVENT_CONTRACT 对齐）。
// 前端 validateEventData 会丢弃缺字段的载荷，后端字段名漂移 = 事件静默失效，
// 故此处对 publishEvent 的 map 字面量 key 做源码级比对（曾因 id/op 漂移于
// templateID/action 导致模板变更事件全量丢失，见 2026-08-22 审计）。
var ssePayloadContract = map[string][]string{
	"task_status":         {"taskID", "status"},
	"alert_new":           {"alertID", "severity"},
	"device_online":       {"deviceID", "segment"},
	"device_offline":      {"deviceID"},
	"approval_status":     {"requestID", "status"},
	"schedule_status":     {"scheduleID", "status"},
	"os_template_changed": {"templateID", "action"},
	"mw_template_changed": {"templateID", "action"},
	"agent_logs":          {"agentID", "logName", "lines"},
}

// TestSSEContract_PayloadKeys 校验 publishEvent 载荷字段与 ssePayloadContract 一致。
// 扫描策略：定位含 publishEvent + 事件名的行，向后拼接至多 6 行（覆盖跨行 map 字面量），
// 在该语句片段内提取全部 "key": 形式的字面量 key 与契约比对。
func TestSSEContract_PayloadKeys(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	pkgDir := filepath.Dir(thisFile)
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		t.Fatalf("读取包目录失败: %v", err)
	}
	reCall := regexp.MustCompile(`[pP]ublishEvent\(.*?"([a-z0-9_]+)"`)
	reKey := regexp.MustCompile(`"([A-Za-z0-9_]+)"\s*:`)
	seen := map[string]map[string]bool{} // event -> 出现过的 key 集合
	scanned := 0
	var filesToScan []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		if e.IsDir() {
			if name == "grpc" {
				grpcDir := filepath.Join(pkgDir, name)
				grpcEntries, err := os.ReadDir(grpcDir)
				if err != nil {
					t.Fatalf("读取 grpc 子目录失败: %v", err)
				}
				for _, ge := range grpcEntries {
					if !ge.IsDir() && strings.HasSuffix(ge.Name(), ".go") {
						filesToScan = append(filesToScan, filepath.Join(grpcDir, ge.Name()))
					}
				}
			}
			continue
		}
		if strings.HasSuffix(name, ".go") {
			filesToScan = append(filesToScan, filepath.Join(pkgDir, name))
		}
	}
	for _, filePath := range filesToScan {
		data, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", filePath, err)
		}
		scanned++
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			m := reCall.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			ev := m[1]
			if _, guarded := ssePayloadContract[ev]; !guarded {
				continue // 未纳入 payload 契约的事件不校验
			}
			// 拼接语句片段：当前行 + 向后至多 6 行或遇到 "})" 收尾。
			frag := line
			for j := i + 1; j < len(lines) && j <= i+6; j++ {
				frag += lines[j]
				if strings.Contains(lines[j], "})") {
					break
				}
			}
			if seen[ev] == nil {
				seen[ev] = map[string]bool{}
			}
			for _, km := range reKey.FindAllStringSubmatch(frag, -1) {
				seen[ev][km[1]] = true
			}
		}
	}
	if scanned == 0 {
		t.Fatal("未扫描到任何源文件，payload 契约测试失效")
	}
	for ev, required := range ssePayloadContract {
		if len(seen[ev]) == 0 {
			t.Errorf("事件 %q 未扫到任何载荷调用点，请确认调用形式或更新契约", ev)
			continue
		}
		for _, key := range required {
			if !seen[ev][key] {
				t.Errorf("事件 %q 的载荷缺少契约字段 %q（出现过的 key: %v）；前端将丢弃该事件", ev, key, keysOf(seen[ev]))
			}
		}
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
