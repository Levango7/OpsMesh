package datatuning

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

func value(t *testing.T, p Profile, component, key string) string {
	t.Helper()
	props, ok := p.Components[component]
	if !ok {
		t.Fatalf("missing component %q", component)
	}
	v, ok := props[key]
	if !ok {
		t.Fatalf("missing key %s.%s", component, key)
	}
	return v
}

func intValue(t *testing.T, p Profile, component, key string) int {
	t.Helper()
	raw := value(t, p, component, key)
	n, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("%s.%s = %q is not an integer: %v", component, key, raw, err)
	}
	return n
}

func floatValue(t *testing.T, p Profile, component, key string) float64 {
	t.Helper()
	raw := value(t, p, component, key)
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		t.Fatalf("%s.%s = %q is not a number: %v", component, key, raw, err)
	}
	return f
}

func TestProfilesWhitelist(t *testing.T) {
	want := []string{"batch", "streaming", "olap", "oltp", "mixed"}
	got := Profiles()
	if len(got) != len(want) {
		t.Fatalf("Profiles() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Profiles()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	got[0] = "mutated"
	if Profiles()[0] != "batch" {
		t.Fatal("Profiles() must return an independent copy")
	}
}

func TestRecommendSmallBatch(t *testing.T) {
	spec := NodeSpec{Count: 3, Cores: 16, RAMGB: 64, DiskGB: 2000, Workload: "batch", DataScaleTB: 2}
	p, err := Recommend(spec)
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if p.Name != "batch-3n" {
		t.Fatalf("Name = %q, want batch-3n", p.Name)
	}
	wantYarnMem := int(math.Floor(float64(62) * 1024 * 0.8))
	if got := intValue(t, p, "yarn", "yarn.nodemanager.resource.memory-mb"); got != wantYarnMem {
		t.Fatalf("yarn memory = %d, want %d", got, wantYarnMem)
	}
	if got := intValue(t, p, "yarn", "yarn.scheduler.maximum-allocation-mb"); got != wantYarnMem {
		t.Fatalf("max alloc = %d, want %d", got, wantYarnMem)
	}
	if got := intValue(t, p, "yarn", "yarn.scheduler.minimum-allocation-mb"); got != 1024 {
		t.Fatalf("min alloc = %d, want 1024", got)
	}
	if got := intValue(t, p, "yarn", "yarn.nodemanager.resource.cpu-vcores"); got != 15 {
		t.Fatalf("vcores = %d, want 15", got)
	}
	if got := value(t, p, "hdfs", "dfs.blocksize"); got != "128m" {
		t.Fatalf("blocksize = %q, want 128m", got)
	}
	if got := intValue(t, p, "hdfs", "dfs.replication"); got != 3 {
		t.Fatalf("replication = %d, want 3", got)
	}
	if got := intValue(t, p, "hdfs", "dfs.namenode.handler.count"); got != 10 {
		t.Fatalf("namenode handlers = %d, want 10", got)
	}
	if got := intValue(t, p, "hdfs", "dfs.datanode.handler.count"); got != 30 {
		t.Fatalf("datanode handlers = %d, want 30", got)
	}
	if got := value(t, p, "hive", "hive.execution.engine"); got != "tez" {
		t.Fatalf("hive engine = %q, want tez", got)
	}
	if got := intValue(t, p, "hive", "hive.tez.container.size"); got != 8192 {
		t.Fatalf("tez container = %d, want 8192 (clamped)", got)
	}
	if got := value(t, p, "hive", "hive.tez.java.opts"); got != "-Xmx6553" {
		t.Fatalf("tez java opts = %q, want -Xmx6553", got)
	}
	if got := intValue(t, p, "spark", "spark.executor.cores"); got != 4 {
		t.Fatalf("executor cores = %d, want 4", got)
	}
	wantExecMem := int(math.Floor(float64(wantYarnMem) / 3 * 0.9))
	if got := intValue(t, p, "spark", "spark.executor.memory"); got != wantExecMem {
		t.Fatalf("executor memory = %d, want %d", got, wantExecMem)
	}
	if got := intValue(t, p, "spark", "spark.driver.memory"); got != 8192 {
		t.Fatalf("driver memory = %d, want 8192 (clamped)", got)
	}
	if got := intValue(t, p, "spark", "spark.sql.shuffle.partitions"); got != 200 {
		t.Fatalf("shuffle partitions = %d, want 200", got)
	}
	if got := floatValue(t, p, "hbase", "hbase.regionserver.global.memstore.size"); got != 0.40 {
		t.Fatalf("memstore = %v, want 0.40", got)
	}
	if got := value(t, p, "hbase", "hbase.hregion.max.filesize"); got != "6G" {
		t.Fatalf("hregion filesize = %q, want 6G", got)
	}
	if got := intValue(t, p, "kafka", "num.partitions"); got < 6 {
		t.Fatalf("kafka partitions = %d, want >= 6", got)
	}
	if got := intValue(t, p, "kafka", "log.retention.hours"); got != 168 {
		t.Fatalf("kafka retention = %d, want 168", got)
	}
	if got := value(t, p, "kafka", "log.segment.bytes"); got != "1G" {
		t.Fatalf("kafka segment bytes = %q, want 1G", got)
	}

	yaml1 := RenderYAML(p)
	yaml2 := RenderYAML(p)
	if yaml1 != yaml2 {
		t.Fatal("RenderYAML is not deterministic for the same profile")
	}
	kafka1 := RenderKafkaProperties(p)
	kafka2 := RenderKafkaProperties(p)
	if kafka1 != kafka2 {
		t.Fatal("RenderKafkaProperties is not deterministic")
	}
}

func TestRecommendLargeOLAP(t *testing.T) {
	p, err := Recommend(NodeSpec{Count: 50, Cores: 64, RAMGB: 256, DiskGB: 200000, Workload: "olap", DataScaleTB: 500})
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if got := value(t, p, "hdfs", "dfs.blocksize"); got != "256m" {
		t.Fatalf("blocksize = %q, want 256m", got)
	}
	if got := intValue(t, p, "spark", "spark.sql.shuffle.partitions"); got < 6400 {
		t.Fatalf("shuffle partitions = %d, want >= 6400", got)
	}
	if got := intValue(t, p, "hdfs", "dfs.namenode.handler.count"); got > 200 {
		t.Fatalf("namenode handlers = %d, want <= 200", got)
	}
	if got := intValue(t, p, "hdfs", "dfs.namenode.handler.count"); got != 34 {
		t.Fatalf("namenode handlers = %d, want 34", got)
	}
	if got := value(t, p, "hbase", "hbase.hregion.max.filesize"); got != "10G" {
		t.Fatalf("hregion filesize = %q, want 10G", got)
	}
	if got := floatValue(t, p, "hbase", "hbase.regionserver.global.memstore.size"); got != 0.40 {
		t.Fatalf("memstore = %v, want 0.40", got)
	}
}

func TestRecommendStreaming(t *testing.T) {
	p, err := Recommend(NodeSpec{Count: 10, Cores: 32, RAMGB: 128, DiskGB: 4000, Workload: "streaming", DataScaleTB: 80, Replication: 2})
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if got := intValue(t, p, "spark", "spark.executor.cores"); got != 2 {
		t.Fatalf("executor cores = %d, want 2", got)
	}
	if got := intValue(t, p, "kafka", "log.retention.hours"); got != 72 {
		t.Fatalf("kafka retention = %d, want 72", got)
	}
	if got := floatValue(t, p, "hbase", "hbase.regionserver.global.memstore.size"); got != 0.35 {
		t.Fatalf("memstore = %v, want 0.35", got)
	}
	if got := intValue(t, p, "hdfs", "dfs.replication"); got != 2 {
		t.Fatalf("replication = %d, want 2", got)
	}
	if got := value(t, p, "hdfs", "dfs.blocksize"); got != "128m" {
		t.Fatalf("blocksize = %q, want 128m", got)
	}
}

func TestRecommendOLTP(t *testing.T) {
	p, err := Recommend(NodeSpec{Count: 5, Cores: 16, RAMGB: 64, DiskGB: 2000, Workload: "oltp", DataScaleTB: 5, Replication: 3})
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if got := value(t, p, "hdfs", "dfs.blocksize"); got != "64m" {
		t.Fatalf("blocksize = %q, want 64m", got)
	}
	if got := intValue(t, p, "kafka", "log.retention.hours"); got != 24 {
		t.Fatalf("kafka retention = %d, want 24", got)
	}
	if got := floatValue(t, p, "hbase", "hbase.regionserver.global.memstore.size"); got != 0.30 {
		t.Fatalf("memstore = %v, want 0.30", got)
	}
	if got := value(t, p, "hbase", "hbase.hregion.max.filesize"); got != "4G" {
		t.Fatalf("hregion filesize = %q, want 4G", got)
	}
}

func TestRecommendInvalid(t *testing.T) {
	base := NodeSpec{Count: 3, Cores: 16, RAMGB: 64, DiskGB: 2000, Workload: "batch", DataScaleTB: 2, Replication: 3}
	cases := []struct {
		name   string
		mutate func(*NodeSpec)
	}{
		{"count zero", func(s *NodeSpec) { s.Count = 0 }},
		{"cores below minimum", func(s *NodeSpec) { s.Cores = 1 }},
		{"ram below minimum", func(s *NodeSpec) { s.RAMGB = 7 }},
		{"disk below minimum", func(s *NodeSpec) { s.DiskGB = 19 }},
		{"negative data scale", func(s *NodeSpec) { s.DataScaleTB = -1 }},
		{"unknown workload", func(s *NodeSpec) { s.Workload = "foo" }},
		{"replication out of range", func(s *NodeSpec) { s.Replication = 9 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := base
			tc.mutate(&spec)
			if _, err := Recommend(spec); err == nil {
				t.Fatalf("Recommend(%+v) = nil error, want error", spec)
			}
		})
	}
}

func TestRecommendBoundsSingleSmallNode(t *testing.T) {
	p, err := Recommend(NodeSpec{Count: 1, Cores: 2, RAMGB: 8, DiskGB: 20, Workload: "batch", DataScaleTB: 0})
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if got := intValue(t, p, "hdfs", "dfs.replication"); got != 3 {
		t.Fatalf("default replication = %d, want 3", got)
	}
	memoryValues := map[string]int{
		"yarn memory":     intValue(t, p, "yarn", "yarn.nodemanager.resource.memory-mb"),
		"max allocation":  intValue(t, p, "yarn", "yarn.scheduler.maximum-allocation-mb"),
		"min allocation":  intValue(t, p, "yarn", "yarn.scheduler.minimum-allocation-mb"),
		"tez container":   intValue(t, p, "hive", "hive.tez.container.size"),
		"executor memory": intValue(t, p, "spark", "spark.executor.memory"),
		"driver memory":   intValue(t, p, "spark", "spark.driver.memory"),
	}
	for name, v := range memoryValues {
		if v < 1024 {
			t.Errorf("%s = %d, want >= 1024", name, v)
		}
	}
	for comp, props := range p.Components {
		for k, v := range props {
			if n, err := strconv.Atoi(v); err == nil && n < 0 {
				t.Errorf("%s.%s = %d, want non-negative", comp, k, n)
			}
			if f, err := strconv.ParseFloat(v, 64); err == nil && f < 0 {
				t.Errorf("%s.%s = %v, want non-negative", comp, k, f)
			}
		}
	}
}

func TestRecommendDeterministic(t *testing.T) {
	spec := NodeSpec{Count: 7, Cores: 24, RAMGB: 96, DiskGB: 8000, Workload: "mixed", DataScaleTB: 80, Replication: 3}
	p1, err := Recommend(spec)
	if err != nil {
		t.Fatalf("first Recommend: %v", err)
	}
	p2, err := Recommend(spec)
	if err != nil {
		t.Fatalf("second Recommend: %v", err)
	}
	if RenderYAML(p1) != RenderYAML(p2) {
		t.Fatal("Recommend twice produced different RenderYAML")
	}
	if RenderKafkaProperties(p1) != RenderKafkaProperties(p2) {
		t.Fatal("Recommend twice produced different Kafka properties")
	}
}

func TestRenderYAMLComponentOrderAndSorting(t *testing.T) {
	p, err := Recommend(NodeSpec{Count: 3, Cores: 16, RAMGB: 64, DiskGB: 2000, Workload: "batch", DataScaleTB: 2})
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	out := RenderYAML(p)
	last := -1
	for _, comp := range componentOrder {
		idx := strings.Index(out, "\n"+comp+":\n")
		if idx < 0 {
			t.Fatalf("component %q not found in YAML:\n%s", comp, out)
		}
		if idx <= last {
			t.Fatalf("component %q is out of fixed order", comp)
		}
		last = idx
	}
	kafka := RenderKafkaProperties(p)
	for _, line := range strings.Split(strings.TrimSpace(kafka), "\n") {
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		if !strings.Contains(line, "=") {
			t.Fatalf("kafka line is not key=value: %q", line)
		}
	}
	if strings.Contains(kafka, "yarn.") || strings.Contains(kafka, "dfs.") {
		t.Fatal("RenderKafkaProperties leaked non-kafka keys")
	}
}
