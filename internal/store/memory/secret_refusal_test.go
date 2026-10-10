package memory

import "testing"

// TestSetSecretRejectsPlaintext 钉住 memory 后端的「只存引用」契约（TD-88）。
//
// 三个后端各自接线一行（契约实现只有 model.RequireSecretReference 一处）；本用例只覆盖
// memory——sql 侧重合在 sql_p03_test.go 的租户隔离用例里（那里已改用引用形态），
// multischema 走委托、由内层后端把关。
func TestSetSecretRejectsPlaintext(t *testing.T) {
	m := NewMemoryStore()
	if meta := m.SetSecret(&SecretItem{Key: "k", Value: "plain-password"}, "t1"); meta != nil {
		t.Fatalf("明文写入应被拒绝（返回 nil），got %+v", meta)
	}
	if _, ok := m.GetSecret("t1", "k"); ok {
		t.Fatal("被拒绝的写入不应落库")
	}
	// 引用形态必须可用，且轮换同样受同一契约约束。
	meta := m.SetSecret(&SecretItem{Key: "k", Value: "${vault:test/k}"}, "t1")
	if meta == nil {
		t.Fatal("引用形态写入应成功")
	}
	if got := m.RotateSecret("t1", "k", "plain-again"); got != nil {
		t.Fatalf("明文轮换应被拒绝，got %+v", got)
	}
	if item, ok := m.GetSecret("t1", "k"); !ok || item.Value != "${vault:test/k}" {
		t.Fatalf("被拒绝的轮换不应改动原值: (%+v,%v)", item, ok)
	}
}
