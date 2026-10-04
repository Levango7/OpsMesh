// shell_grep_gate_test.go — 交付脚本里不许用 `producer | grep -q` 做判定。
//
// 缺陷形态（2026-10-04 真机定位，代价是四轮"缺哪个指标"各不相同的假红）：
// 这些脚本开头都写了 `set -euo pipefail`。`grep -q` 一命中就退出，生产者（printf/curl/docker logs）
// 还在往管道里写 ⇒ 被 SIGPIPE 打死 ⇒ 管道退出码变成 141，pipefail 把它升格成"整条管道失败"。
// 于是**明明命中了却判成没命中**。是否踩中取决于生产者输出量与样式位置：
// 越过管道缓冲区（约 64KB）才可能中招，所以小输出时一直"正常"，
// 而 /metrics 这类几十 KB 的快照就会偶发失败——正是最难查的那一类假红。
//
// 正确写法是先落到变量再判：`grep -q PAT <<<"$var"`（herestring 由 bash 落成临时文件，
// 没有管道、没有可被杀的生产者，`^` 行锚语义与逐字一致）。
// 刻意不换成 `[[ $s == *PAT* ]]`：那是整串子串匹配，`^anchor` 会失效，
// 而指标名互为前缀时（opsmesh_http_metrics_series vs ..._dropped_total）就会假命中。
package gates

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// pipelineGrepQ 判断一行是否把 grep -q（及 -qv/-qE/-qF/-qx/-qw/-qi）放在管道右侧做判定。
func pipelineGrepQ(line string) bool {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
		return false // 注释里允许出现该写法（本文件头就是这么写的）
	}
	segs := strings.Split(line, "|")
	if len(segs) < 2 {
		return false
	}
	for _, s := range segs[1:] {
		s = strings.TrimSpace(s)
		if strings.HasPrefix(s, "grep -q") { // 覆盖 -q/-qv/-qE/-qF/-qx/-qw/-qi
			return true
		}
	}
	return false
}

// TestPipelineGrepQDetector 是探测器的自检（正例 + 反例都要有）。
// 没有这一段，"扫到 0 处违规"就可能是"扫描器坏了"的另一种说法。
func TestPipelineGrepQDetector(t *testing.T) {
	pos := []string{
		`  if printf '%s' "$mbody" | grep -q '^opsmesh_http_metrics_series_dropped_total [0-9]' \`,
		`        printf '%s\n' "$ports" | grep -qx "$port" && return 0`,
		`            if docker compose logs task-svc 2>&1 | grep -q "MySQL store 已启用"; then`,
		`          echo "$out" | grep -q "image: x" || { echo "::error::坏"; exit 1; }`,
	}
	neg := []string{
		`  if grep -q '^opsmesh_http_metrics_series_dropped_total [0-9]' <<<"$mbody"; then`,
		`  sline="$(grep -E '^x [0-9]+$' <<<"$mbody" | head -1)"`, // 右侧是 head，不是 grep -q
		`# 原先到处写着 printf '%s' "$var" | grep -q PAT`,
		`  mtext="$(curl -sS http://x/metrics)"`,
	}
	for _, l := range pos {
		if !pipelineGrepQ(l) {
			t.Errorf("探测器漏报（这条必须判红）：%s", l)
		}
	}
	for _, l := range neg {
		if pipelineGrepQ(l) {
			t.Errorf("探测器误报（这条是正确写法或注释）：%s", l)
		}
	}
}

// TestNoPipelineGrepQInDeliveryAssets 扫交付脚本与流水线定义。
func TestNoPipelineGrepQInDeliveryAssets(t *testing.T) {
	targets := collectShellAndWorkflowFiles(t, filepath.Join("..", ".."))
	if len(targets) < 10 {
		t.Fatalf("只扫到 %d 个交付脚本/工作流文件（扫描面塌了，本门禁会空转）", len(targets))
	}
	checked := 0
	var bad []string
	for _, f := range targets {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", f, err)
		}
		for n, line := range strings.Split(string(b), "\n") {
			checked++
			if pipelineGrepQ(line) {
				bad = append(bad, f+":"+strconv.Itoa(n+1))
			}
		}
	}
	if checked < 2000 {
		t.Fatalf("只核对到 %d 行（扫描面异常，判红而不是放行）", checked)
	}
	t.Logf("扫描面：%d 个文件 / %d 行，违规 %d", len(targets), checked, len(bad))
	if len(bad) > 0 {
		t.Errorf("发现 %d 处 `producer | grep -q`（pipefail 下命中也可能判成未命中，见本文件头注释）。"+
			"改成 `grep -q PAT <<<\"$var\"`：%v", len(bad), bad)
	}
}

// collectShellAndWorkflowFiles 收 deploy/ 下的 .sh 与 .github/workflows 下的 .yml/.yaml。
// 排除前端产物目录，避免把 node_modules/embed 里的 js 当脚本扫。
func collectShellAndWorkflowFiles(t *testing.T, root string) []string {
	t.Helper()
	pats := []string{
		"deploy/*.sh", "deploy/*/*.sh", "deploy/*/*/*.sh", "deploy/*/*/*/*.sh",
		".github/workflows/*.yml", ".github/workflows/*.yaml",
	}
	var out []string
	for _, pat := range pats {
		ms, err := filepath.Glob(filepath.Join(root, pat))
		if err != nil {
			t.Fatalf("glob %s: %v", pat, err)
		}
		for _, m := range ms {
			if strings.Contains(m, "node_modules") {
				continue
			}
			if st, err := os.Stat(m); err == nil && !st.IsDir() {
				out = append(out, m)
			}
		}
	}
	return out
}
