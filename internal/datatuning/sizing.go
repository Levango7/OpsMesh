// Package datatuning 实现工作负载感知的大数据集群参数推荐（纯函数，仅依赖标准库）。
//
// 输入为集群节点规格与工作负载画像，输出 YARN/HDFS/Hive/Spark/HBase/Kafka
// 的推荐参数集合；同一输入多次调用产出逐字节一致，便于审计与幂等下发。
package datatuning

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// NodeSpec 描述集群节点规格与工作负载画像。
type NodeSpec struct {
	Count       int     `json:"count"`
	Cores       int     `json:"cores"`
	RAMGB       int     `json:"ramGB"`
	DiskGB      int     `json:"diskGB"`
	Workload    string  `json:"workload"`
	DataScaleTB float64 `json:"dataScaleTB"`
	Replication int     `json:"replication"`
}

// Profile 是一次推荐产出的组件参数集合。
type Profile struct {
	Name       string                       `json:"name"`
	Workload   string                       `json:"workload"`
	Components map[string]map[string]string `json:"components"`
	Notes      []string                     `json:"notes"`
}

// workloadOrder 为支持的工作负载白名单（同时决定 Profiles 返回顺序）。
var workloadOrder = []string{"batch", "streaming", "olap", "oltp", "mixed"}

// componentOrder 决定渲染时组件段的固定顺序。
var componentOrder = []string{"yarn", "hdfs", "hive", "spark", "hbase", "kafka"}

// Profiles 返回支持的工作负载白名单（副本，调用方可安全修改）。
func Profiles() []string {
	return append([]string(nil), workloadOrder...)
}

func validWorkload(wl string) bool {
	for _, w := range workloadOrder {
		if w == wl {
			return true
		}
	}
	return false
}

// memoryRatio 返回 YARN 可用内存占节点可用内存的比例。
func memoryRatio(wl string) float64 {
	switch wl {
	case "batch", "olap":
		return 0.80
	case "streaming", "mixed":
		return 0.70
	case "oltp":
		return 0.75
	}
	return 0.70
}

// clampInt 将 v 限制在 [minV, maxV]，被夹取时追加可读说明。
func clampInt(minV, v, maxV int, name string, notes *[]string) int {
	if v < minV {
		*notes = append(*notes, fmt.Sprintf("%s clamped to minimum %d (computed %d)", name, minV, v))
		return minV
	}
	if v > maxV {
		*notes = append(*notes, fmt.Sprintf("%s clamped to maximum %d (computed %d)", name, maxV, v))
		return maxV
	}
	return v
}

func itoa(v int) string { return strconv.Itoa(v) }

// Recommend 根据节点规格与工作负载画像生成组件参数推荐。
//
// 每节点预留 1 核 + 2 GiB 给操作系统；其余资源按工作负载比例切分给 YARN。
func Recommend(in NodeSpec) (Profile, error) {
	wl := strings.ToLower(strings.TrimSpace(in.Workload))
	if !validWorkload(wl) {
		return Profile{}, fmt.Errorf("unsupported workload %q (allowed: %s)", in.Workload, strings.Join(workloadOrder, ", "))
	}
	if in.Count < 1 {
		return Profile{}, fmt.Errorf("count must be >= 1, got %d", in.Count)
	}
	if in.Cores < 2 {
		return Profile{}, fmt.Errorf("cores must be >= 2, got %d", in.Cores)
	}
	if in.RAMGB < 8 {
		return Profile{}, fmt.Errorf("ramGB must be >= 8, got %d", in.RAMGB)
	}
	if in.DiskGB < 20 {
		return Profile{}, fmt.Errorf("diskGB must be >= 20, got %d", in.DiskGB)
	}
	if in.DataScaleTB < 0 {
		return Profile{}, fmt.Errorf("dataScaleTB must be >= 0, got %v", in.DataScaleTB)
	}

	replicationNote := ""
	replication := in.Replication
	switch replication {
	case 0:
		replication = 3
		replicationNote = "replication not specified; defaulted to 3"
	case 1, 2, 3:
	default:
		return Profile{}, fmt.Errorf("replication must be 1, 2 or 3, got %d", in.Replication)
	}

	usableCores := in.Cores - 1
	usableRAMGB := in.RAMGB - 2
	ratio := memoryRatio(wl)

	var notes []string
	notes = append(notes, fmt.Sprintf("workload %s: YARN memory ratio %.2f applied; reserved 1 core + 2 GiB per node for OS", wl, ratio))
	if replicationNote != "" {
		notes = append(notes, replicationNote)
	}

	yarnMem := int(math.Floor(float64(usableRAMGB) * 1024 * ratio))

	// HDFS 块大小：超大规模优先 256m；OLTP 小文件优先 64m。
	blockSize := "128m"
	switch {
	case in.DataScaleTB > 100:
		blockSize = "256m"
	case wl == "oltp":
		blockSize = "64m"
	}

	namenodeHandlers := clampInt(10, int(math.Round(20*math.Log10(float64(in.Count)))), 200,
		"dfs.namenode.handler.count", &notes)
	datanodeHandlers := clampInt(10, usableCores*2, 256, "dfs.datanode.handler.count", &notes)

	tezContainer := clampInt(2048, yarnMem/4, 8192, "hive.tez.container.size", &notes)

	executorCores := 4
	if wl == "streaming" {
		executorCores = 2
	}
	executorsPerNode := usableCores / executorCores
	if executorsPerNode < 1 {
		executorsPerNode = 1
	}
	executorMem := clampInt(1024, int(math.Floor(float64(yarnMem)/float64(executorsPerNode)*0.9)), 32768,
		"spark.executor.memory", &notes)
	driverMem := clampInt(1024, executorMem, 8192, "spark.driver.memory", &notes)
	shufflePartitions := in.Count * in.Cores * 2
	if shufflePartitions < 200 {
		shufflePartitions = 200
	}

	memstore := "0.40"
	switch wl {
	case "streaming":
		memstore = "0.35"
	case "oltp":
		memstore = "0.30"
	}

	maxFileSize := "8G"
	switch wl {
	case "olap":
		maxFileSize = "10G"
	case "batch":
		maxFileSize = "6G"
	case "oltp":
		maxFileSize = "4G"
	}

	retention := 120
	switch wl {
	case "batch":
		retention = 168
	case "streaming":
		retention = 72
	case "oltp":
		retention = 24
	}

	partitions := in.Count * usableCores / 2
	if partitions < 6 {
		partitions = 6
	}

	networkThreads := usableCores
	if networkThreads < 8 {
		networkThreads = 8
	}
	ioThreads := usableCores * 2
	if ioThreads < 16 {
		ioThreads = 16
	}

	components := map[string]map[string]string{
		"yarn": {
			"yarn.nodemanager.resource.memory-mb":                   itoa(yarnMem),
			"yarn.scheduler.maximum-allocation-mb":                  itoa(yarnMem),
			"yarn.scheduler.minimum-allocation-mb":                  "1024",
			"yarn.nodemanager.resource.cpu-vcores":                  itoa(usableCores),
			"yarn.resourcemanager.scheduler.class":                  "org.apache.hadoop.yarn.server.resourcemanager.scheduler.capacity.CapacityScheduler",
			"yarn.nodemanager.vmem-check-enabled":                   "false",
			"yarn.nodemanager.pmem-check-enabled":                   "true",
			"yarn.log-aggregation-enable":                           "true",
			"yarn.nodemanager.local-dirs":                           "/data/yarn/local",
			"yarn.nodemanager.log-dirs":                             "/data/yarn/log",
			"yarn.nodemanager.aux-services":                         "mapreduce_shuffle",
			"yarn.nodemanager.aux-services.mapreduce_shuffle.class": "org.apache.hadoop.mapred.ShuffleHandler",
		},
		"hdfs": {
			"dfs.blocksize":                       blockSize,
			"dfs.replication":                     itoa(replication),
			"dfs.namenode.handler.count":          itoa(namenodeHandlers),
			"dfs.datanode.handler.count":          itoa(datanodeHandlers),
			"dfs.datanode.max.transfer.threads":   "8192",
			"dfs.namenode.safemode.threshold-pct": "0.999",
			"dfs.client.read.shortcircuit":        "true",
			"dfs.client.socket-timeout":           "60000",
		},
		"hive": {
			"hive.execution.engine":             "tez",
			"hive.tez.container.size":           itoa(tezContainer),
			"hive.tez.java.opts":                fmt.Sprintf("-Xmx%d", int(float64(tezContainer)*0.8)),
			"hive.auto.convert.join":            "true",
			"hive.optimize.bucketmapjoin":       "true",
			"hive.vectorized.execution.enabled": "true",
			"hive.exec.dynamic.partition.mode":  "nonstrict",
			"hive.exec.max.dynamic.partitions":  "5000",
		},
		"spark": {
			"spark.executor.cores":            itoa(executorCores),
			"spark.executor.instances":        itoa(in.Count * executorsPerNode),
			"spark.executor.memory":           itoa(executorMem),
			"spark.driver.memory":             itoa(driverMem),
			"spark.sql.shuffle.partitions":    itoa(shufflePartitions),
			"spark.sql.adaptive.enabled":      "true",
			"spark.serializer":                "org.apache.spark.serializer.KryoSerializer",
			"spark.dynamicAllocation.enabled": "true",
			"spark.shuffle.service.enabled":   "true",
		},
		"hbase": {
			"hbase.regionserver.global.memstore.size": memstore,
			"hbase.hregion.max.filesize":              maxFileSize,
			"hbase.regionserver.handler.count":        itoa(clampInt(30, usableCores*4, 300, "hbase.regionserver.handler.count", &notes)),
			"hbase.hregion.memstore.flush.size":       "134217728",
			"hbase.hstore.blockingStoreFiles":         "16",
			"hbase.regionserver.regionSplitLimit":     "1000",
		},
		"kafka": {
			"num.partitions":             itoa(partitions),
			"log.retention.hours":        itoa(retention),
			"num.network.threads":        itoa(networkThreads),
			"num.io.threads":             itoa(ioThreads),
			"log.segment.bytes":          "1G",
			"default.replication.factor": itoa(replication),
			"min.insync.replicas":        itoa(minInt(replication, 2)),
			"log.cleanup.policy":         "delete",
			"auto.create.topics.enable":  "false",
		},
	}

	// 追加依赖可用核数/节点的说明，便于审计复算。
	notes = append(notes,
		fmt.Sprintf("per-node usable resources: %d vcores, %d GiB RAM (after OS reservation)", usableCores, usableRAMGB),
		fmt.Sprintf("kafka topic partitions per topic: %d (count*usableCores/2)", partitions),
	)

	return Profile{
		Name:       fmt.Sprintf("%s-%dn", wl, in.Count),
		Workload:   wl,
		Components: components,
		Notes:      notes,
	}, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// RenderComponentProperties 渲染单个组件的 properties 文本（键排序，确定性输出）。
func RenderComponentProperties(p Profile, component string) string {
	props, ok := p.Components[component]
	if !ok {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# OpsMesh %s tuning profile %s (workload=%s)\n", component, p.Name, p.Workload)
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, props[k])
	}
	return b.String()
}

// RenderKafkaProperties 渲染 Kafka 的 server.properties 片段。
func RenderKafkaProperties(p Profile) string {
	return RenderComponentProperties(p, "kafka")
}

// RenderYAML 渲染完整推荐为 YAML 文本。
//
// 组件段顺序固定为 yarn,hdfs,hive,spark,hbase,kafka，组件内键按字典序排序，
// 因此同一 Profile 多次渲染逐字节一致。
func RenderYAML(p Profile) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# OpsMesh data tuning profile %s (workload=%s)\n", p.Name, p.Workload)
	for _, comp := range componentOrder {
		props, ok := p.Components[comp]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "%s:\n", comp)
		keys := make([]string, 0, len(props))
		for k := range props {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "  %s: %s\n", k, yamlQuote(props[k]))
		}
	}
	if len(p.Notes) > 0 {
		b.WriteString("notes:\n")
		for _, n := range p.Notes {
			fmt.Fprintf(&b, "  - %s\n", yamlQuote(n))
		}
	}
	return b.String()
}

func yamlQuote(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\n`,
		"\t", `\t`,
	)
	return `"` + r.Replace(s) + `"`
}
