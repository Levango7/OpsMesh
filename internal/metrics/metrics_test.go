package metrics

import (
	"strings"
	"testing"
)

func TestMetricsRender(t *testing.T) {
	m := New()
	m.SetAgents(3)
	m.IncTask("done")
	m.IncTask("done")
	m.IncTask("failed")
	m.SetQueueDepth(2)
	m.ObserveDuration(1.5)
	m.ObserveDuration(0.5)

	out := m.Render()
	for _, want := range []string{
		"opsmesh_agents_total 3",
		"opsmesh_tasks_total{status=\"done\"} 2",
		"opsmesh_tasks_total{status=\"failed\"} 1",
		"opsmesh_task_queue_depth 2",
		"opsmesh_task_duration_seconds_count 2",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("Render 缺少 %q\n---%s", want, out)
		}
	}
}
