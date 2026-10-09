// sql_shim.go 父包对 sqlstore 子包的别名回导层（TD-61 批次 3-sql）。
//
// sql.go 与 32 个 sql_*.go（SQL 后端方法集 + 迁移框架）已迁入 internal/store/sqlstore。
// 外部 import 方（controlplane 的 *store.SQLStore 类型断言 / store.NewSQLStore 构造）
// 与父包内的 multi_schema*（defaultStoreFactory 构造 per-schema SQLStore）对
// `store.SQLStore` / `store.NewSQLStore` 零感知：
//   - 类型走**别名**（与原名同一类型，方法集完整可用）；
//   - 构造函数走**薄包装**（函数无法别名）。
//
// 编译期断言（store.go 尾部的 `var _ Store = (*SQLStore)(nil)` 等）经别名继续生效。
// 生命周期：multi_schema 包装层下沉（末批）后本文件成为最后一块回导层，随父包一并收口。
package store

import "github.com/Levango7/OpsMesh/internal/store/sqlstore"

// SQLStore 基于 MySQL + Redis 的持久化实现（内部为 internal/store/sqlstore.SQLStore）。
type SQLStore = sqlstore.SQLStore

// NewSQLStore 打开 MySQL 连接并建表（幂等）。redisAddr 为空则跳过 Redis。
func NewSQLStore(dsn, redisAddr, redisPassword string) (*SQLStore, error) {
	return sqlstore.NewSQLStore(dsn, redisAddr, redisPassword)
}
