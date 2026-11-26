package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// TestExecFileRejectsSymlinkedParentEscape 钉文件任务的父目录逃逸：
// 允许根目录下的 link 是指向根外的符号链接。词法 Rel 会把 link/escaped.txt
// 误判为根内路径，但真实写入发生在根外；必须按解析后的真实父目录拒绝。
func TestExecFileRejectsSymlinkedParentEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("平台不能创建目录符号链接: %v", err)
	}

	a := newTestAgent(time.Second)
	a.cfg.AgentFileRootWhitelist = root
	var out, errb bytes.Buffer
	err := a.execFile(context.Background(), &out, &errb, proto.Task{
		Type: proto.TaskTypeFile, Path: filepath.Join(link, "escaped.txt"), Content: "x",
	})
	if err == nil {
		t.Fatal("应拒绝经父目录符号链接逃逸到白名单根外，当前却允许写入")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "escaped.txt")); statErr == nil {
		t.Fatal("不应在根外实际写出 escaped.txt")
	}
}

// TestExecFileAllowsSymlinkedParentInsideRoot 验证不把合法的目录链接误杀：
// 根目录下的 link 指向同一根目录内的 real，写入应落到 real/out.txt。
func TestExecFileAllowsSymlinkedParentInsideRoot(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("平台不能创建目录符号链接: %v", err)
	}

	a := newTestAgent(time.Second)
	a.cfg.AgentFileRootWhitelist = root
	var out, errb bytes.Buffer
	target := filepath.Join(link, "out.txt")
	if err := a.execFile(context.Background(), &out, &errb, proto.Task{
		Type: proto.TaskTypeFile, Path: target, Content: "hello",
	}); err != nil {
		t.Fatalf("根目录内的目录链接不应被误拒: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(realDir, "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("文件内容 = %q, want hello", string(got))
	}
}
