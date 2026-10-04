package gates

// capability_evidence_gate_test.go 是 #63 的机制部分：能力表里每条"落地文件 / 入口"
// 都必须指向**仓库里真实存在**的路径。
//
// 起因（2026-10-04 实测）：README 的能力表把 `internal/provision/` 当作"设备管理"的
// 证据，而该包早在 D3-a/b（commit 36cc7e1b）就迁到了 `pkg/provision` —— 数字对得上、
// CI 全绿，证据路径却是假的。这类"看起来已核验"的假证据比缺文档更危险：读者会因为
// 它而不去自查。
//
// 判定面只做"路径存在性"这件确定性最强的事。"能力是否真的可用"需要运行期证据，
// 由 verify-runtime.sh、各服务的 bufconn/httptest 用例与活体验证负责，不混进这里。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// backticked 抓表格单元格里的 `code` 片段。
var backticked = regexp.MustCompile("`([^`]+)`")

type evidenceClaim struct {
	doc  string
	path string
	line int
}

func TestCapabilityEvidencePathsExist(t *testing.T) {
	root := repoRoot(t)

	var claims []evidenceClaim
	var violations int

	for _, doc := range []string{"README.md", "docs/product-design.md"} {
		b, err := os.ReadFile(filepath.Join(root, doc))
		if err != nil {
			t.Fatalf("读 %s: %v", doc, err)
		}
		inEvidenceTable := false
		for i, raw := range strings.Split(string(b), "\n") {
			line := strings.TrimSpace(raw)
			if !strings.HasPrefix(line, "|") {
				inEvidenceTable = false
				continue
			}
			if isTableDivider(line) {
				continue
			}
			if isEvidenceTableHeader(line) {
				inEvidenceTable = true
				continue
			}
			if !inEvidenceTable {
				continue
			}
			cells := strings.Split(line, "|")
			if len(cells) < 3 {
				continue
			}
			// 首列是能力名（常含指向源码目录的写法），证据列在其后。
			for _, cell := range cells[2:] {
				for _, m := range backticked.FindAllStringSubmatch(cell, -1) {
					tok := strings.TrimSpace(m[1])
					if !looksLikeRepoPath(tok) {
						continue
					}
					claims = append(claims, evidenceClaim{doc: doc, path: tok, line: i + 1})
					if !pathExists(root, tok) {
						violations++
						t.Errorf("%s:%d 声称的证据路径不存在：%q —— 数字对得上但路径是假的，比没有证据更容易骗人",
							doc, i+1, tok)
					}
				}
			}
		}
	}

	t.Logf("能力表证据路径核对：%d 条声明，%d 条不存在", len(claims), violations)
	if len(claims) < 20 {
		t.Fatalf("只核对到 %d 条证据路径——扫描面塌了（表头文案被改？），本门禁会空转判绿", len(claims))
	}
}

// isTableDivider 认 |---|:--:| 这类分隔行。
func isTableDivider(line string) bool {
	return strings.Trim(line, "|-: ") == ""
}

// isEvidenceTableHeader 只把"带证据列的能力表"纳入核对面。
func isEvidenceTableHeader(line string) bool {
	hasEvidenceColumn := strings.Contains(line, "落地文件") ||
		strings.Contains(line, "证据") || strings.Contains(line, "关键文件")
	if !hasEvidenceColumn {
		return false
	}
	return strings.Contains(line, "功能模块") || strings.Contains(line, "能力") ||
		strings.Contains(line, "成熟度")
}

// looksLikeRepoPath 只留下"明显是仓库内路径"的 token。
//
// 误报的代价比漏报大：门禁一旦开始报 flag 名 / env 名 / HTTP 路径，人们会把它关掉。
func looksLikeRepoPath(tok string) bool {
	if tok == "" || strings.ContainsAny(tok, " \t") {
		return false
	}
	if strings.HasPrefix(tok, "-") || strings.HasPrefix(tok, "/") || strings.Contains(tok, "://") {
		return false // flag / 绝对路径 / URL
	}
	if strings.Contains(tok, ":") && !strings.Contains(tok, "/") {
		return false // key:value 或 文件:行号 之类引用
	}
	if strings.ToUpper(tok) == tok && strings.Contains(tok, "_") {
		return false // 环境变量名（LOG_LEVEL 等）
	}
	if !strings.Contains(tok, "/") && !strings.HasSuffix(tok, ".go") && !strings.HasSuffix(tok, ".md") {
		return false // 既无目录分隔，也不是源码/文档文件名
	}
	switch tok {
	case "README", "DELIVERY", "api-reference.md", "tech-debt.md", "product-roadmap.md":
		// 裸文件名在能力表里常是"引用另一篇文档"，不作为仓库路径断言。
		return false
	}
	return true
}

// pathExists 判定 token 是否指向仓库内真实存在的东西。
//
// 三种宽容写法都是为了不误报（而非放水）：`dir/*` 按目录本体判断；不含 `/` 的
// 裸文件名允许任意位置存在；`dir/` 结尾当目录判断。
func pathExists(root, tok string) bool {
	rel := strings.TrimSuffix(filepath.FromSlash(tok), "/")
	if rel == "" {
		return true
	}
	if _, err := os.Stat(filepath.Join(root, rel)); err == nil {
		return true
	}
	if strings.HasSuffix(rel, "*") {
		base := strings.TrimSuffix(strings.TrimSuffix(rel, "*"), string(filepath.Separator))
		if base != "" {
			if _, err := os.Stat(filepath.Join(root, base)); err == nil {
				return true
			}
		}
	}
	if !strings.Contains(tok, "/") {
		var found bool
		_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil || info == nil || info.IsDir() {
				return nil
			}
			if filepath.Base(p) == tok {
				found = true
			}
			return nil
		})
		return found
	}
	return false
}
