// Package logstore 实现 M6 日志检索：集中采集 agent / 任务 / 系统日志，
// 支持按租户 / 设备 / 时间 / 关键字检索。双后端（Memory 环形缓冲 / SQL）。
package logstore

import (
	"context"
	"database/sql"
)

// LogStore 是 M6 日志检索的后端抽象：Memory 环形缓冲 / SQL。
// 控制面通过 Handler 注入此接口；行级租户隔离由调用方在 Append/Query 时保证。
type LogStore interface {
	// Append 写入一条日志（tenant_id 由调用方强制赋值，禁止客户端自报覆盖）。
	Append(ctx context.Context, e *Entry) error
	// Query 按条件检索日志（TenantID 必填；tenantID 为空时不过滤——仅限无网关开发模式）。
	Query(ctx context.Context, q Query) ([]Entry, error)
	// Close 释放底层资源（Memory 为空实现；SQL 不关闭共享 *sql.DB）。
	Close() error
}

// maxQueryLimit 单次检索硬上限，防止无 limit 时全表返回（U-04 私有部署防爆）。
const maxQueryLimit = 1000

// NewMemory 构造内存环形缓冲后端（默认；无外部依赖即可运行）。
// cap 为最大保留条数（<=0 取默认 5000，超出丢弃最旧）。
func NewMemory(cap int) *MemoryLogStore {
	if cap <= 0 {
		cap = 5000
	}
	return &MemoryLogStore{buf: make([]Entry, 0, cap), cap: cap}
}

// NewSQL 构造 MySQL 后端（U-04 数据本地化，私有部署）。
// db 来自 store.SQLStore.DB()（与控制面共享同一连接池，不在本包内关闭）。
func NewSQL(db *sql.DB) (*SQLLogStore, error) {
	s := &SQLLogStore{db: db}
	if err := s.initSchema(nil); err != nil {
		return nil, err
	}
	return s, nil
}
