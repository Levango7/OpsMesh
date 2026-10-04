package gates

// package_count_gate_test.go 把 CI 的 "Verify internal package count (H4 防回归)"
// 在本地也跑一遍。
//
// 为什么必须重复一份：那条门禁只存在于 .github/workflows/ci.yml 里，本地
// `go build` / `go test` / lint 都碰不到它。本仓因此连红两次——每加一个新
// internal 包就要同步 README 与 docs/module-design.md 三处数字，而"我加了包"
// 这个动作发生在本地，"数字没同步"却要等 20 分钟 CI 才说。
// 放进本包（结构性门禁的家）之后，`go test ./internal/gates/` 就能在推送前判红。

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

func TestInternalPackageCountMatchesDocs(t *testing.T) {
	root := repoRoot(t)

	entries, err := os.ReadDir(filepath.Join(root, "internal"))
	if err != nil {
		t.Fatalf("读 internal/: %v", err)
	}
	actual := 0
	for _, e := range entries {
		if e.IsDir() {
			actual++
		}
	}
	if actual < 30 {
		t.Fatalf("只数到 %d 个 internal 包——扫描面塌了，本门禁会空转判绿", actual)
	}

	read := func(file, label string, re *regexp.Regexp) int {
		b, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatalf("读 %s: %v", file, err)
		}
		m := re.FindSubmatch(b)
		if m == nil {
			t.Fatalf("%s 里找不到 %s 的计数（CI 的 grep -oP 同样会失败并判红）", file, label)
		}
		n, err := strconv.Atoi(string(m[1]))
		if err != nil {
			t.Fatalf("解析 %s 的计数失败：%v", file, err)
		}
		return n
	}

	readme := read("README.md", "internal 包职责", regexp.MustCompile(`internal 包职责（(\d+) 个）`))
	design := read("docs/module-design.md", "覆盖包数", regexp.MustCompile(`覆盖包数：(\d+)`))

	t.Logf("internal 目录=%d，README=%d，module-design=%d", actual, readme, design)
	if actual != readme || actual != design {
		t.Errorf("internal 包数三处不一致：实际 %d / README %d / module-design %d。"+
			"新增包时三处都要改（README 职责表 + 目录树、module-design 总览表 + 覆盖包数），"+
			"CI 的同名步骤会红", actual, readme, design)
	}
}
