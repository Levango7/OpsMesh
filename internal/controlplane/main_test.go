// main_test.go — controlplane 测试包入口。
//
// 背景：seedRBAC 在每次 NewMemoryStore 时对 3 个预置用户做 bcrypt 哈希
// （成本因子默认 10，单次约 50-100ms）。本包 400+ 测试各自构造 MemoryStore，
// 本地 `go test -count=1` 全量并发跑时 bcrypt 累积开销可将整包推过 120s 超时
// （实测 goroutine 停在 expensiveBlowfishSetup）。CI 已显式设置
// OPSMESH_TEST_BCRYPT_COST=4（见 .github/workflows/ci.yml），此处仅为本地兜底默认值；
// 显式设置的环境变量优先，不被覆盖。仅测试二进制生效（_test.go 不参与生产构建）。
package controlplane

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("OPSMESH_TEST_BCRYPT_COST") == "" {
		_ = os.Setenv("OPSMESH_TEST_BCRYPT_COST", "4")
	}
	os.Exit(m.Run())
}
