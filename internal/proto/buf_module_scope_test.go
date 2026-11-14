package proto

// buf_module_scope_test.go — 防复发门禁：proto/buf.yaml 的 modules 不得越出 context 目录。
//
// 为什么需要这条：buf v2 要求 module path 位于 **context 目录内**（context = 含 buf.yaml 的目录
// = proto/）。一旦有人把 `../services/task-svc/api` 这类越界 path 加进来，buf 会**拒绝加载整个配置**：
//
//	buf: Failure: decode buf.yaml: invalid module path: ../services/task-svc/api: is outside the context directory
//
// ⇒ CI 的 proto job（buf lint + buf breaking）**全红**，而且红的不止这一个 PR ——
// main 与所有 open PR 的 build-test 一起被挡（TD-60 的 a704cea / 00791d8 已各触发一次）。
//
// 本门禁把「再加回来」变成**一个说得清楚的失败**，而不是一次全局红灯。
//
// 补充事实（已实测）：即便把 buf workspace 根提到仓库根，也**不能**把 task-svc/api 列为 module ——
// 它的 package `opsmesh.task.v1` 与目录 `proto/v1` 不匹配，且与 canonical 同包撞名
// （TD-63 当初把 canonical 改址到 opsmesh/task/v1 的原因）⇒ buf 报
// "Files with package opsmesh.task.v1 must be within a directory opsmesh/task/v1 relative to root"。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bufConfigRelPath 是 proto/buf.yaml 相对本测试包目录（internal/proto/）的路径。
const bufConfigRelPath = "../../proto/buf.yaml"

// TestBufModulesStayInsideContext 断言 proto/buf.yaml 的每个 module path 都在 proto/ 内。
func TestBufModulesStayInsideContext(t *testing.T) {
	b, err := os.ReadFile(bufConfigRelPath)
	if err != nil {
		t.Fatalf("读 %s 失败：%v", bufConfigRelPath, err)
	}
	contextDir := filepath.Dir(bufConfigRelPath)
	for _, line := range strings.Split(string(b), "\n") {
		l := strings.TrimSpace(line)
		if !strings.HasPrefix(l, "- path:") {
			continue
		}
		p := strings.TrimSpace(strings.TrimPrefix(l, "- path:"))
		if p == "" || p == "." {
			continue
		}
		abs := filepath.Clean(filepath.Join(contextDir, p))
		rel, err := filepath.Rel(contextDir, abs)
		if err != nil {
			t.Fatalf("计算 %s 相对 %s 失败：%v", abs, contextDir, err)
		}
		if strings.HasPrefix(rel, "..") {
			t.Errorf("proto/buf.yaml 的 module path %q 越出 context 目录（%s）：\n"+
				"  buf v2 会直接拒绝加载整个配置 —— \"invalid module path: %s: is outside the context directory\"\n"+
				"  ⇒ CI proto job 全红，main 与所有 open PR 的 build-test 一起被挡。\n"+
				"  注：task-svc/api 也不能列为 module（package opsmesh.task.v1 与目录 proto/v1 不匹配、\n"+
				"  且与 canonical 同包撞名）—— 两份 proto 的一致性由 TD-63 字节锁定用例保证。",
				p, contextDir, p)
		}
	}
}
