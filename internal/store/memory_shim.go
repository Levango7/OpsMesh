// memory_shim.go 父包对 memory 子包的别名回导层（TD-61 批次 3）。
//
// memory.go 与 25 个 memory_*.go（内存后端方法集）已迁入 internal/store/memory。
// 外部 import 方（129 个文件）与父包内的 sql_* / multi_schema* / stub_guard / 各测试
// 对 `store.MemoryStore` / `store.NewMemoryStore()` 零感知：
//   - 类型走**别名**（与原名同一类型，方法集完整可用）；
//   - 构造函数走**薄包装**（函数无法别名）。
//
// 编译期断言（store.go 尾部的 `var _ Store = (*MemoryStore)(nil)` 等）经别名继续生效：
// 子包实现若漏掉任一契约方法，仍在本包编译期立刻暴露。
package store

import "github.com/Levango7/OpsMesh/internal/store/memory"

// MemoryStore 内存实现（内部为 internal/store/memory.MemoryStore）。
type MemoryStore = memory.MemoryStore

// NewMemoryStore 构造内存后端（默认后端，不依赖任何外部存储）。
func NewMemoryStore() *MemoryStore { return memory.NewMemoryStore() }
