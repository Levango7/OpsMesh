// parent_test_helpers_test.go 父包测试用的最小替身与工具（TD-61 拆包后按包各自持有）。
//
// 与 memory/memory_test.go 中的同名定义刻意重复：recordingBus / countDevices 是
// **测试替身与测试工具**，Go 的跨包测试不能 import 另一个包的 _test.go，而拆包后
// memory 子包也不能反向 import 父包——测试侧按包各自持有最小实现是本仓既有风格
// （各测试包自带 stub）——改动面最小，也避免为测试往生产包里塞公共依赖。
package store

import (
	"context"

	"github.com/Levango7/OpsMesh/internal/events"
	"github.com/Levango7/OpsMesh/internal/proto"
)

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
