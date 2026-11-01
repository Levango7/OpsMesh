// rbac_perm_source_guard_test.go 是权限目录的第二层守护：从**源码调用点**扫权限点，
// 而不是只核对一份手工清单。
//
// 起因（2026-11-02）：internal/aiops/handler.go 用的是自己的包装
// `h.authorize(fn, "aiops:read", "aiops:write")`，不是 2026-09-29 那次盘点所依据的
// requireProd/requirePermission 字面量 ⇒ 手工清单 handlerRequiredPerms 从未看见它，
// 目录里也就一直没有 aiops:* ⇒ 六条端点（含 POST /aiops/execute，会把修复方案下发成
// agent 任务）对含 admin 在内的全部角色恒 403。清单能记住「当时已知的」，记不住
// 「后来新出现的」，本用例把后者也变成红。
package store

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// permLiteral 权限点字面量的形状：group:action（小写、无空格、无斜杠）。
// 刻意收紧到这一个形状，避免把 "aiops: model is required" 这类错误串误判成权限点。
var permLiteral = regexp.MustCompile(`^[a-z][a-z0-9_]*:[a-z][a-z0-9_]*$`)

// authzCallMarkers 「这一行在核对权限点」的调用点特征。
var authzCallMarkers = []string{"authorize(", "requireProd(", "requirePermission(", "requirePerm("}

// findRepoRoot 从测试工作目录向上找本模块的 go.mod（module 行匹配才认，
// 免得哪天在 services/* 子模块里跑测试时把子模块根当仓库根）。
func findRepoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	dir := cwd
	for i := 0; i < 8; i++ {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && strings.Contains(string(data), "module github.com/Levango7/OpsMesh") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("没从 %s 向上找到本模块 go.mod——用例前提（工作区布局）变了", cwd)
	return ""
}

// scanAuthzPerms 扫 internal/ 下全部非测试 Go 文件的鉴权调用点，返回权限点 → 出处列表。
func scanAuthzPerms(t *testing.T, root string) map[string][]string {
	t.Helper()
	found := make(map[string][]string)
	base := filepath.Join(root, "internal")
	err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}
		for i, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue // 注释里的示例/说明不算引用
			}
			matched := false
			for _, m := range authzCallMarkers {
				if strings.Contains(line, m) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			for _, tok := range quotedStrings(line) {
				if !permLiteral.MatchString(tok) {
					continue
				}
				found[tok] = append(found[tok], fmt.Sprintf("%s:%d", filepath.ToSlash(rel), i+1))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描 internal/ 失败: %v", err)
	}
	return found
}

// quotedStrings 取出一行里的双引号字面量。权限点串内不含引号与转义，
// 处理不了的形状会被当成「不是权限点」放过，属可接受的窄化。
func quotedStrings(line string) []string {
	var out []string
	for {
		start := strings.Index(line, `"`)
		if start < 0 {
			return out
		}
		rest := line[start+1:]
		end := strings.Index(rest, `"`)
		if end < 0 {
			return out
		}
		out = append(out, rest[:end])
		line = rest[end+1:]
	}
}

// TestRolePermissions_CoversAuthorizeCallSites 目录覆盖守护（源码版）：
// 每个被鉴权调用点引用的权限点都必须在目录里，否则 admin 也不持有 ⇒ 端点恒 403。
func TestRolePermissions_CoversAuthorizeCallSites(t *testing.T) {
	root := findRepoRoot(t)
	found := scanAuthzPerms(t, root)

	// 对照拍：扫描器必须真的扫到东西，且扫到已知权限点。否则「零红」只代表扫描器坏了。
	if len(found) == 0 {
		t.Fatalf("没扫到任何鉴权调用点（root=%s）——扫描特征已失效，守护等于不存在", root)
	}
	for _, want := range []string{"ai:read", "device:read"} {
		if _, ok := found[want]; !ok {
			t.Errorf("对照失败：源码扫描没找到已知权限点 %q，扫描特征已失效", want)
		}
	}

	catalog := map[string]bool{}
	for _, p := range RolePermissions()["admin"] {
		catalog[p] = true
	}

	var missing []string
	for perm, sites := range found {
		if !catalog[perm] {
			sort.Strings(sites)
			missing = append(missing, fmt.Sprintf("%s（%s）", perm, strings.Join(sites, ", ")))
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("权限目录缺以下被 handler 引用的权限点，对应端点对全部角色（含 admin）恒 403：%s",
			strings.Join(missing, "；"))
	}
}

// TestHandlerRequiredPermsMatchSourceScan 两层守护的一致性：手工清单里的每一项，
// 源码扫描也应能找到——清单不是凭空写的，扫描特征也没漏掉整个族。
func TestHandlerRequiredPermsMatchSourceScan(t *testing.T) {
	found := scanAuthzPerms(t, findRepoRoot(t))
	var absent []string
	for _, p := range handlerRequiredPerms {
		if _, ok := found[p]; !ok {
			absent = append(absent, p)
		}
	}
	if len(absent) > 0 {
		sort.Strings(absent)
		t.Errorf("手工清单里的权限点没在源码鉴权调用点扫到：%s"+
			"（要么引用已随重构消失→清单该清；要么扫描特征漏了这一族）", strings.Join(absent, ", "))
	}
}
