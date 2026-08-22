package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadOrCreateAgentID_Persistent 验证 身份持久化：同目录二次加载读取同一文件，ID 稳定。
func TestLoadOrCreateAgentID_Persistent(t *testing.T) {
	dir := t.TempDir()
	id1 := loadOrCreateAgentID(dir, "host-x")
	if id1 == "" {
		t.Fatal("空 ID")
	}
	// 同目录二次加载应读取同一 agent.id（重启沿用）
	id2 := loadOrCreateAgentID(dir, "host-x")
	if id1 != id2 {
		t.Fatalf("ID 不稳定: %q vs %q", id1, id2)
	}
	// 文件确实存在
	if _, err := os.Stat(filepath.Join(dir, "agent.id")); err != nil {
		t.Fatalf("agent.id 未落盘: %v", err)
	}
}

// TestLoadOrCreateAgentID_EmptyDirFallback 验证空目录（不持久化）仍返回非空 ID，不阻塞启动。
func TestLoadOrCreateAgentID_EmptyDirFallback(t *testing.T) {
	id := loadOrCreateAgentID("", "host-y")
	if id == "" {
		t.Fatal("空目录也应产出 ID")
	}
}
