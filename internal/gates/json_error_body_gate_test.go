package gates

// json_error_body_gate_test.go 是「HTTP 错误响应必须是 JSON 体」的门禁（BB-03）。
//
// 为什么要有它：`http.Error(w, msg, code)` 写的是 **text/plain** 裸文本，而本仓对外契约
// （docs/api-specification.md，2026-11-07 勘误后）明确错误体是 `{"error":"<msg>"}`。
// 同一条路由族里两套错误形状，客户端就得写两种解析分支——本轮五维排查（BB-03）把
// 56 处这样的裸 text 出口列为存量债，逐批收编。收编完成后，本门禁负责**不许回退**：
// 新增 `http.Error` 会让构建期直接红。
//
// 判据（三档，缺一档就会退化成摆设——对齐 authz_wiring_gate 的做法）：
//  1. 扫描面自证：必须扫到足量 .go 文件（防路径写错导致 0 违规假绿），且探测器对
//     「注释里的 http.Error」判否、对真实调用判是（TestJSONErrorDetector）。
//  2. 白名单外的任何一处 `http.Error(` 出现 ⇒ 红（白名单=尚未收编的存量 + 浏览器资产面）。
//  3. 白名单过期 ⇒ 红：白名单里的文件若已一处都不剩，必须把该行删掉（债务不许藏在豁免里）。
//
// 收编进度（每次只减不增）：
//   - 批 A 已收编（25 处）：internal/cmdb/handler.go、internal/logstore/handler.go、
//     internal/orchestration/handler.go（统一改用各包既有 writeJSON 的 JSON 错误体）。
//   - 待批 B：internal/deploy/{handler,auto_advance}.go、internal/controlplane/{device_metrics,
//     federation,sse,system_ops}.go（收编后从白名单删除）。
//   - 永久白名单：internal/controlplane/enterprise_ui.go —— 浏览器资产面（embed 的静态 UI），
//     非 API 面：这里 405/503 保持 text/plain，浏览器能直接显示，改成 JSON 反而更差。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// jsonErrorExempt 是「允许出现 http.Error 的文件」白名单：文件 → 允许的原因。
// 仅两类可入账：① 明确不该 JSON 化的浏览器资产面；② 尚未收编的存量（收编后必须删行）。
var jsonErrorExempt = map[string]string{
	"internal/controlplane/enterprise_ui.go": "浏览器资产面（embed 静态 UI），405/503 保持 text/plain 便于浏览器直接显示",

	// —— 未收编存量（批 B 收口后逐行删除，删到只剩上面那行）——
	"internal/deploy/handler.go":              "待批 B：改走包内 writeJSON 的 JSON 错误体",
	"internal/deploy/auto_advance.go":         "待批 B：同上",
	"internal/controlplane/device_metrics.go": "待批 B：同上",
	"internal/controlplane/federation.go":     "待批 B：同上",
	"internal/controlplane/sse.go":            "待批 B：同上",
	"internal/controlplane/system_ops.go":     "待批 B：同上",
}

// findHTTPErrorSites 返回 content 中**非注释**部分的 `http.Error(` 出现行号（1-based）。
// 为什么要剥注释：本仓门禁已记过「判据把说明性注释也算成违规」的假红形态。
func findHTTPErrorSites(content string) []int {
	var lines []int
	line := 1
	i := 0
	n := len(content)
	inBlockComment := false
	inLineComment := false
	inString := false // "..." 或 `...`
	inRune := false   // '...'
	escaped := false
	lastMatchLine := 0

	for i < n {
		c := content[i]
		if c == '\n' {
			line++
			inLineComment = false
			escaped = false
			i++
			continue
		}
		if inLineComment {
			i++
			continue
		}
		if inBlockComment {
			if c == '*' && i+1 < n && content[i+1] == '/' {
				inBlockComment = false
				i += 2
				continue
			}
			i++
			continue
		}
		if inString {
			if escaped {
				escaped = false
			} else if c == '\\' && (i == 0 || content[i-1] != '`') {
				escaped = true
			} else if c == '"' || c == '`' {
				inString = false
			}
			i++
			continue
		}
		if inRune {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '\'' {
				inRune = false
			}
			i++
			continue
		}
		// 非字符串/注释态：识别注释与字符串起点、以及目标模式
		if c == '/' && i+1 < n && content[i+1] == '/' {
			inLineComment = true
			i += 2
			continue
		}
		if c == '/' && i+1 < n && content[i+1] == '*' {
			inBlockComment = true
			i += 2
			continue
		}
		if c == '"' {
			inString = true
			i++
			continue
		}
		if c == '`' {
			inString = true
			i++
			continue
		}
		if c == '\'' {
			inRune = true
			i++
			continue
		}
		if strings.HasPrefix(content[i:], "http.Error(") {
			if lastMatchLine != line {
				lines = append(lines, line)
				lastMatchLine = line
			}
			i += len("http.Error(")
			continue
		}
		i++
	}
	return lines
}

// TestJSONErrorDetector 是探测器自证：真实调用判是、注释里判否、字符串里判否。
func TestJSONErrorDetector(t *testing.T) {
	positive := "package x\n\nfunc f() {\n\thttp.Error(w, \"not found\", http.StatusNotFound)\n}\n"
	if got := findHTTPErrorSites(positive); len(got) != 1 || got[0] != 4 {
		t.Fatalf("探测器漏报真实调用：got=%v want=[4]", got)
	}

	negative := "package x\n\n// 说明：以前这里写 http.Error(w, \"x\", 500)，现已改 writeJSON\n/* 块注释里的 http.Error( 不算 */\nvar s = \"http.Error( 在字符串里也不算\"\n"
	if got := findHTTPErrorSites(negative); len(got) != 0 {
		t.Fatalf("探测器把注释/字符串误判成违规：got=%v", got)
	}
}

// TestHTTPErrorOnlyInAllowlist 扫 internal/ 与 services/ 下的非测试 Go 文件：
// 白名单外出现 `http.Error(` ⇒ 红；白名单里的文件一处都不剩 ⇒ 红（豁免过期）。
func TestHTTPErrorOnlyInAllowlist(t *testing.T) {
	root := repoRoot(t)
	scanRoots := []string{filepath.Join(root, "internal"), filepath.Join(root, "services")}

	scanned := 0
	seenIn := map[string]int{}
	var offenders []string

	for _, scanRoot := range scanRoots {
		err := filepath.Walk(scanRoot, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				base := filepath.Base(path)
				if base == "vendor" || base == "node_modules" || base == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			scanned++
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			sites := findHTTPErrorSites(string(raw))
			if len(sites) == 0 {
				return nil
			}
			seenIn[rel] = len(sites)
			if _, ok := jsonErrorExempt[rel]; !ok {
				offenders = append(offenders, rel)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("扫描 %s 失败: %v", scanRoot, err)
		}
	}

	// 判据 1：扫描面自证——文件数低于地板说明路径/过滤器坏了，此时「0 违规」没有意义。
	const minGoFiles = 400
	if scanned < minGoFiles {
		t.Fatalf("扫描面自证失败：只扫到 %d 个非测试 .go 文件（地板 %d）——扫描器或路径有问题，本次结论不可信", scanned, minGoFiles)
	}

	// 判据 1b：白名单里那处「永久豁免」必须真被扫到，否则规则/路径写错了同样会假绿。
	if seenIn["internal/controlplane/enterprise_ui.go"] == 0 {
		t.Fatalf("扫描面自证失败：永久豁免文件 internal/controlplane/enterprise_ui.go 一处都没扫到——路径或探测器有问题")
	}

	// 判据 2：白名单外不得出现。
	if len(offenders) > 0 {
		t.Fatalf("发现白名单外的 http.Error（HTTP 错误响应必须是 JSON 体 {\"error\":\"…\"}）：\n  %s\n"+
			"改法：用本包既有的 writeJSON(w, code, map[string]string{\"error\": msg})，或 controlplane 的 paginate.WriteJSON。",
			strings.Join(offenders, "\n  "))
	}

	// 判据 3：白名单过期 ⇒ 红（债务不许藏在豁免里）。
	var stale []string
	for file, reason := range jsonErrorExempt {
		if seenIn[file] == 0 {
			stale = append(stale, file+"（原因： "+reason+"）")
		}
	}
	if len(stale) > 0 {
		t.Fatalf("白名单过期：下列文件已不含 http.Error，必须从 jsonErrorExempt 删除对应行：\n  %s",
			strings.Join(stale, "\n  "))
	}
}
