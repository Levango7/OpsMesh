// Package gates 放跨模块的**结构性门禁**（扫描源码形态，而不是运行期行为）。
//
// 为什么需要这一层：本仓是 go.work 多模块，各服务的 cmd/*/main.go 属于不同模块，
// 根模块的测试与 lint 都碰不到它们；而"静默退回内存"这类缺陷恰好长在 main.go 里
// ——它能通过编译、通过 /health、通过所有单测，只在真出事（重启丢数据）时现形。
// 用一条根模块里的源码扫描门禁把它钉住，代价最小、复发时判红最快。
package gates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bannedSilentFallback 是"要求 sql 却没配 DSN 时静默用内存"的源码形态。
//
// 修法是把 DSN 为空变成**启动失败**，而不是把它当成"那就用内存吧"。
const bannedSilentFallback = `StoreType == "sql" && cfg.DSN != ""`

// TestNoSilentSQLFallbackInServiceMains 扫描 services/*/cmd/*/main.go：
//  1. 不许再出现被禁的合并条件形态；
//  2. 任何带 `if cfg.StoreType == "sql" {` 分支的 main，必须同时有 DSN 为空的显式失败
//     ——只查第 1 条的话，"把整个 sql 分支删掉"也能判绿，那是假修。
func TestNoSilentSQLFallbackInServiceMains(t *testing.T) {
	root := repoRoot(t)
	files, err := filepath.Glob(filepath.Join(root, "services", "*", "cmd", "*", "main.go"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) < 7 {
		t.Fatalf("只找到 %d 个服务 main.go（本仓至少有 7 个带 store 装配的服务）——"+
			"匹配面塌了会让本门禁空转", len(files))
	}
	scanned := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读 %s: %v", f, err)
		}
		s := string(b)
		scanned++
		if strings.Contains(s, bannedSilentFallback) {
			t.Errorf("%s 仍是\"要求 sql 却没配 DSN 就静默用内存\"的形态：/health 照样 200、"+
				"重启即丢全部数据。请把 DSN 为空改成启动失败", f)
			continue
		}
		if strings.Contains(s, `if cfg.StoreType == "sql" {`) && !strings.Contains(s, "cfg.DSN == \"\"") {
			t.Errorf("%s 有 sql 分支却没有 DSN 为空的显式失败——分支被删或改成静默回退了", f)
		}
	}
	t.Logf("扫描 %d 个服务 main.go，无静默降级形态", scanned)
}

// repoRoot 从测试工作目录（包目录）向上回溯到含 go.mod 的仓库根。
//
// 必须回溯而不是写死相对路径：go test 的工作目录是被测包目录，`services/...`
// 在那儿根本不存在；而 glob 空结果若不当成错误，门禁就会空转判绿。
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for dir := wd; ; {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("从 %s 向上找不到 go.mod（仓库根判定失败，门禁不许空转）", wd)
		}
		dir = parent
	}
}
