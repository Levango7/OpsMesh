// test_helpers_test.go sqlstore 包的测试替身（按包各自持有，跨包测试不能 import 彼此的 _test.go）。
package sqlstore

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
