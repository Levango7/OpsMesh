// multi_shim.go 多 schema 包装层的稳定门面（TD-61 末批）。
//
// 实现已下沉 internal/store/multischema/；本文件以「类型别名 + 薄包装」把公共面
// 保留在父包，外部 import 方（controlplane/factory、server_netsec.go 的类型分发、
// config 注释引用的语义面）继续以 store.X 编程，零改动：
//
//   - store.MultiSchemaStore：server_netsec.go:402 的 `case *store.MultiSchemaStore`
//     —— 别名与原名是同一类型，类型分发逐字不变；
//   - store.NewMultiSchemaStore / store.DefaultSchemaNamer：factory 构造路径。
package store

import "github.com/Levango7/OpsMesh/internal/store/multischema"

// SchemaNamer 租户名→MySQL schema 名的映射函数（含 SQL 注入白名单校验）。
type SchemaNamer = multischema.SchemaNamer

// MultiSchemaStore 多租户 schema 隔离实现（每租户独立 schema，逐方法委托到后端 store）。
type MultiSchemaStore = multischema.MultiSchemaStore

// DefaultSchemaNamer 返回默认命名器：prefix + tenant（双方均过 [a-zA-Z0-9_] 白名单）。
func DefaultSchemaNamer(prefix string) SchemaNamer {
	return multischema.DefaultSchemaNamer(prefix)
}

// NewMultiSchemaStore 构造多租户 schema 隔离存储。
func NewMultiSchemaStore(baseDSN, redisAddr, redisPassword string, namer SchemaNamer) (*MultiSchemaStore, error) {
	return multischema.NewMultiSchemaStore(baseDSN, redisAddr, redisPassword, namer)
}
