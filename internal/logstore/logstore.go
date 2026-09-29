// Package logstore 实现 M6 日志检索：集中采集 agent / 任务 / 系统日志，
// 支持按租户 / 设备 / 时间 / 关键字检索。双后端（Memory 环形缓冲 / SQL）。
package logstore

import (
	"context"
	"database/sql"
	"errors"
)

// LogStore 是 M6 日志检索的后端抽象：Memory 环形缓冲 / SQL（两者可写），
// Elasticsearch / Loki（只读，日志由采集器直推，见 SupportsAppend）。
// 控制面通过 Handler 注入此接口；行级租户隔离由调用方在 Append/Query 时保证。
type LogStore interface {
	// Append 写入一条日志（tenant_id 由调用方强制赋值，禁止客户端自报覆盖）。
	Append(ctx context.Context, e *Entry) error
	// Query 按条件检索日志（TenantID 必填；tenantID 为空时不过滤——仅限无网关开发模式）。
	Query(ctx context.Context, q Query) ([]Entry, error)
	// Close 释放底层资源（Memory 为空实现；SQL 不关闭共享 *sql.DB）。
	Close() error
}

// maxQueryLimit 单次检索硬上限，防止无 limit 时全表返回（私有部署防爆）。
const maxQueryLimit = 1000

// ErrAppendUnsupported 表示该后端按设计不接受 OpsMesh 侧写入（日志由 filebeat / promtail /
// fluent-bit 等采集器直推）。它必须是**显式错误**而不是 return nil：
// 返回 nil 会让调用方以为写入成功，agent 日志与任务输出就变成静默丢失。
var ErrAppendUnsupported = errors.New(
	"该后端不接受 OpsMesh 侧写入：Elasticsearch/Loki 的日志须由 filebeat/promtail/fluent-bit 等采集器直推；" +
		"若需 OpsMesh 写入日志（agent 上报 / 任务输出），请把 log-backend 配成 memory 或 sql")

// appendUnsupported 由"只读"后端实现。
type appendUnsupported interface{ AppendUnsupported() bool }

// SupportsAppend 报告该后端是否接受 OpsMesh 侧写入。
// 未知实现按"支持"处理，避免给 memory/sql 后端凭空加限制。
// 调用方须在循环/落库**之前**问一次：循环里逐条拿到 ErrAppendUnsupported 只会把
// 一次能力不匹配放大成上万条无效调用，却仍然一条都写不进去。
func SupportsAppend(ls LogStore) bool {
	a, ok := ls.(appendUnsupported)
	return !ok || !a.AppendUnsupported()
}

// NewMemory 构造内存环形缓冲后端（默认；无外部依赖即可运行）。
// cap 为最大保留条数（<=0 取默认 5000，超出丢弃最旧）。
func NewMemory(cap int) *MemoryLogStore {
	if cap <= 0 {
		cap = 5000
	}
	return &MemoryLogStore{buf: make([]Entry, 0, cap), cap: cap}
}

// NewMemoryWithIndex 构造启用倒排索引的内存后端。
// Append 同步加入索引；SearchFullText 提供全文本检索（短语/布尔/通配符/TF-IDF）。
// cap 为最大保留条数（<=0 取默认 5000）；环形裁剪同步移除索引中旧文档。
func NewMemoryWithIndex(cap int) *MemoryLogStore {
	if cap <= 0 {
		cap = 5000
	}
	return &MemoryLogStore{
		buf:   make([]Entry, 0, cap),
		cap:   cap,
		index: NewInvertedIndex(),
	}
}

// NewSQL 构造 MySQL 后端（数据本地化，私有部署）。
// db 来自 store.SQLStore.DB()（与控制面共享同一连接池，不在本包内关闭）。
func NewSQL(db *sql.DB) (*SQLLogStore, error) {
	s := &SQLLogStore{db: db}
	if err := s.initSchema(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}
