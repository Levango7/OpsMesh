//go:build kafka

package events

import (
	"path/filepath"
	"testing"
)

// TestNewKafkaBranchWALWiring 验证 events.New 的 kafka 分支按 OPSMESH_KAFKA_WAL_DIR 接线 WAL（issue #171）。
// 未设置=无 WAL 向后兼容路径（newKafkaBus 不变）；设置=启用 WAL 并指向 env 目录。
func TestNewKafkaBranchWALWiring(t *testing.T) {
	// 未设置/显式空：向后兼容无 WAL（wal==nil）。
	t.Setenv("OPSMESH_KAFKA_WAL_DIR", "")
	inner, ok := New("kafka", "localhost:1", "test-topic").(stampingBus).inner.(*kafkaBus)
	if !ok || inner == nil || inner.wal != nil {
		t.Fatalf("未设置 WAL 目录时应走无 WAL 向后兼容路径（wal==nil）: ok=%v inner=%+v", ok, inner)
	}

	// 设置临时目录：启用 WAL，且目录为 env 指定值。
	dir := filepath.Join(t.TempDir(), "wal")
	t.Setenv("OPSMESH_KAFKA_WAL_DIR", dir)
	inner, ok = New("kafka", "localhost:1", "test-topic").(stampingBus).inner.(*kafkaBus)
	if !ok || inner == nil || inner.wal == nil {
		t.Fatalf("设置 WAL 目录后应启用 WAL: ok=%v inner=%+v", ok, inner)
	}
	if inner.wal.dir != dir {
		t.Fatalf("WAL 目录应为 env 指定值: got=%q want=%q", inner.wal.dir, dir)
	}
	inner.StopWAL()
}
