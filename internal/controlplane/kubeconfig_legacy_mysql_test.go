// kubeconfig_legacy_mysql_test.go kubeconfig 三类存量与机会式迁移的真库验证
// （TD-88 前置：加密原语下沉的规格 §7 第 4 项）。
//
// 为什么必须真库：这三类存量的区分（无前缀旧密文 / 无前缀明文 / 带前缀密文）与迁移后的
// 「重写回库」都要经过 store 的持久化路径（SaveK8sCluster 幂等 upsert + ListK8sClusters 读回）
// 才算验完——纯内存用例（coverage_extra_test.go 的 decryptKubeconfigForLoad 用例）只覆盖了判定逻辑，
// 不覆盖「迁移真的落库」。
//
// 门控：OPSMESH_TEST_MYSQL_DSN 缺失即 skip（CI 的 integration job 注入该变量）。
package controlplane

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/store"
)

func TestKubeconfigLegacyMigration_MySQL(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("skipping MySQL integration smoke: missing OPSMESH_TEST_MYSQL_DSN env; set it to a real MySQL DSN to run")
	}
	st, err := store.NewSQLStore(dsn, "", "")
	if err != nil {
		t.Fatalf("NewSQLStore: %v", err)
	}
	key := []byte("0123456789abcdef0123456789abcdef")
	s := &Server{cfg: &config.Config{}, encryptionKey: key, store: st}

	suffix := time.Now().UnixNano()
	const plaintext = "apiVersion: v1\nkind: Config\nclusters: []\n" // 明文 kubeconfig 形态

	// 造三类存量：legacy 旧密文（无前缀）/ 真明文（无前缀）/ 新格式（带前缀）。
	newlyEnc, err := s.encryptKubeconfig(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !strings.HasPrefix(newlyEnc, "enc:v1:") {
		t.Fatalf("新格式应带 enc:v1: 前缀: %q", newlyEnc)
	}
	rows := []*store.K8sCluster{
		{ID: fmt.Sprintf("itest-kc-legacy-%d", suffix), Name: "legacy", Server: "https://legacy.example", Kubeconfig: strings.TrimPrefix(newlyEnc, "enc:v1:")},
		{ID: fmt.Sprintf("itest-kc-plain-%d", suffix), Name: "plain", Server: "https://plain.example", Kubeconfig: plaintext},
		{ID: fmt.Sprintf("itest-kc-new-%d", suffix), Name: "new", Server: "https://new.example", Kubeconfig: newlyEnc},
	}
	for _, r := range rows {
		if err := st.SaveK8sCluster(r); err != nil {
			t.Fatalf("SaveK8sCluster(%s): %v", r.ID, err)
		}
		defer func(id string) { _ = st.DeleteK8sCluster(id) }(r.ID)
	}

	// 读回（真库往返）+ 三段分流判定 + 机会式迁移（与 server.go 启动恢复环同一动作）。
	wantMigrate := map[string]bool{rows[0].ID: true, rows[1].ID: false, rows[2].ID: false}
	for _, r := range rows {
		got := st.GetK8sCluster(r.ID)
		if got == nil {
			t.Fatalf("GetK8sCluster(%s) 未命中——写入没落库", r.ID)
		}
		plain, migrate, err := s.decryptKubeconfigForLoad(got.Kubeconfig)
		if err != nil {
			t.Fatalf("%s decryptKubeconfigForLoad: %v", r.Name, err)
		}
		if plain != plaintext {
			t.Fatalf("%s 读回 = %q, want 原文（三类存量都应能解出同一明文）", r.Name, plain)
		}
		if migrate != wantMigrate[r.ID] {
			t.Fatalf("%s migrate = %v, want %v（只有无前缀旧密文需要迁移）", r.Name, migrate, wantMigrate[r.ID])
		}
		if migrate {
			// 机会式迁移：立即用新格式重写回库（SaveK8sCluster 幂等 upsert）。
			enc, err := s.encryptKubeconfig(plain)
			if err != nil {
				t.Fatalf("%s 迁移加密: %v", r.Name, err)
			}
			got.Kubeconfig = enc
			if err := st.SaveK8sCluster(got); err != nil {
				t.Fatalf("%s 迁移回写: %v", r.Name, err)
			}
		}
	}

	// 迁移后断言：legacy 行已变为带前缀且仍可解出；明文行与新建行内容未变。
	after := st.GetK8sCluster(rows[0].ID)
	if after == nil {
		t.Fatal("legacy 行迁移后查不到")
	}
	if !strings.HasPrefix(after.Kubeconfig, "enc:v1:") {
		t.Fatalf("legacy 行迁移后应带 enc:v1: 前缀，实际 %q", after.Kubeconfig)
	}
	if plain, _, err := s.decryptKubeconfigForLoad(after.Kubeconfig); err != nil || plain != plaintext {
		t.Fatalf("迁移后的 legacy 行解密 = (%q, %v)", plain, err)
	}
	plainRow := st.GetK8sCluster(rows[1].ID)
	if plainRow == nil {
		t.Fatal("明文行迁移后查不到")
	}
	if plainRow.Kubeconfig != plaintext {
		t.Fatalf("明文存量不应被重写（避免擅自加密运维有意写入的明文）: %q", plainRow.Kubeconfig)
	}
	newRow := st.GetK8sCluster(rows[2].ID)
	if newRow == nil || newRow.Kubeconfig != newlyEnc {
		t.Fatal("带前缀的新格式行不应被改动")
	}
	t.Logf("ROUNDTRIP_OK: 三类存量（旧密文/明文/新格式）真库往返 + 机会式迁移落库均验证通过")
}
