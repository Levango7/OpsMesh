// test_helpers_test.go multischema 包测试用的最小替身与工具（TD-61 末批：随多 schema 测试
// 从父包迁入；原父包 parent_test_helpers_test.go / parent_dsn_test.go 的对应定义因无剩余使用者已删除）。
//
// 与 memory 子包内的同名定义刻意重复：Go 的跨包测试不能 import 另一个包的 _test.go，
// 拆包后各测试包自带最小实现是本仓既有风格（见 memory/memory_test.go 与 sqlstore 的同名件）。
package multischema

import (
	"context"
	"database/sql"
	"strings"

	"github.com/Levango7/OpsMesh/internal/events"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store/memory"
)

// newMemoryStore 本包测试用的内存后端构造（父包 store.NewMemoryStore 的测试内别名，
// TD-61 末批随多 schema 测试下沉——委托层 smoke 以内存后端驱动，避免依赖真实 MySQL）。
func newMemoryStore() *memory.MemoryStore { return memory.NewMemoryStore() }

// recordingBus 测试用内存事件总线，记录所有发布的事件。
type recordingBus struct {
	events []events.Event
}

func (b *recordingBus) Publish(_ context.Context, e events.Event) error {
	b.events = append(b.events, e)
	return nil
}

// countDevices 统计 Snapshot 返回的 map 中的设备总数。
func countDevices(m map[string][]proto.DeviceInfo) int {
	n := 0
	for _, ds := range m {
		n += len(ds)
	}
	return n
}

// stripDBName 从 DSN 中去掉 dbname，保留 ?params，用于连 mysql 不指定库。
// user:pass@tcp(host:port)/dbname?params → user:pass@tcp(host:port)/?params
// 注意：go-sql-driver 要求 dbname 分隔符 "/" 必须存在（空库名也要保留），
// 否则报 "missing the slash separating the database name"。
func stripDBName(dsn string) string {
	idx := strings.LastIndex(dsn, "/")
	if idx == -1 {
		return dsn
	}
	head := dsn[:idx]
	tail := dsn[idx+1:]
	qIdx := strings.Index(tail, "?")
	if qIdx == -1 {
		return head + "/"
	}
	return head + "/" + tail[qIdx:]
}

// dropTestDB 删除测试用临时库。
func dropTestDB(adminDSN, dbName string) {
	db, err := sql.Open("mysql", adminDSN)
	if err != nil {
		return
	}
	defer db.Close()
	_, _ = db.Exec("DROP DATABASE IF EXISTS " + dbName)
}
