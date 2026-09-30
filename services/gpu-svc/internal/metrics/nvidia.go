package metrics

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/models"
)

// 真实数据源（TD-60 §5.9 取证：此前指标全部来自 math/rand 模拟，gpu 域观察期
// 数据不可信）。优先走节点本机的 nvidia-smi；仅当本机无 GPU/无该工具时才
// 回退到模拟，且回退产物以 Source="simulated" 显式标注，与真实数据可区分。

// sourceReal / sourceSimulated 指标来源标注。
const (
	sourceReal      = "nvidia-smi"
	sourceSimulated = "simulated"
)

var (
	nvidiaMu      sync.Mutex
	nvidiaChecked bool
	nvidiaOK      bool
)

// nvidiaSmiCommand 便于测试替换的可执行名。
var nvidiaSmiCommand = "nvidia-smi"

// nvidiaSmiAvailable 探测本机是否有可用的 nvidia-smi（结果缓存，进程内一次）。
func nvidiaSmiAvailable() bool {
	nvidiaMu.Lock()
	defer nvidiaMu.Unlock()
	if !nvidiaChecked {
		nvidiaChecked = true
		nvidiaOK = exec.Command(nvidiaSmiCommand, "--help").Run() == nil
	}
	return nvidiaOK
}

// collectFromNvidiaSmi 经 nvidia-smi 采集本机 GPU 真实指标。
// 查询字段与解析顺序一一对应：index,utilization.gpu,memory.used,memory.total,
// temperature.gpu,power.draw,fan.speed,clocks.current.sm（csv,noheader,nounits）。
func collectFromNvidiaSmi(nodeID string, now time.Time) (*models.GPUMetrics, error) {
	out, err := exec.Command(nvidiaSmiCommand,
		"--query-gpu=index,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw,fan.speed,clocks.current.sm",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return nil, fmt.Errorf("nvidia-smi: %w", err)
	}

	m := &models.GPUMetrics{
		NodeID:    nodeID,
		Timestamp: now,
		Source:    sourceReal,
	}
	var totalUtil, totalTemp float64
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Split(strings.TrimSpace(line), ",")
		if len(fields) < 8 {
			continue
		}
		idx, err := strconv.Atoi(strings.TrimSpace(fields[0]))
		if err != nil {
			continue
		}
		g := models.GPUMetricsPerGPU{Index: idx}
		// 单字段解析失败保持零值（部分驱动对 fan/clock 报 N/A）。
		if v, e := strconv.ParseFloat(strings.TrimSpace(fields[1]), 64); e == nil {
			g.UtilizationPct = v
		}
		if v, e := strconv.Atoi(strings.TrimSpace(fields[2])); e == nil {
			g.MemoryUsedMB = v
		}
		if v, e := strconv.Atoi(strings.TrimSpace(fields[3])); e == nil {
			g.MemoryTotalMB = v
		}
		if v, e := strconv.ParseFloat(strings.TrimSpace(fields[4]), 64); e == nil {
			g.TemperatureC = v
		}
		if v, e := strconv.ParseFloat(strings.TrimSpace(fields[5]), 64); e == nil {
			g.PowerDrawW = v
		}
		if v, e := strconv.ParseFloat(strings.TrimSpace(fields[6]), 64); e == nil {
			g.FanSpeedPct = v
		}
		if v, e := strconv.Atoi(strings.TrimSpace(fields[7])); e == nil {
			g.ClockSpeedMHz = v
		}
		g.ThermalThrottle = g.TemperatureC > 85

		m.GPUs = append(m.GPUs, g)
		m.TotalMemoryUsedMB += g.MemoryUsedMB
		m.TotalMemoryTotalMB += g.MemoryTotalMB
		m.TotalPowerDrawW += g.PowerDrawW
		totalUtil += g.UtilizationPct
		totalTemp += g.TemperatureC
	}
	if len(m.GPUs) == 0 {
		return nil, fmt.Errorf("nvidia-smi 返回 0 台 GPU")
	}
	n := float64(len(m.GPUs))
	m.AvgUtilization = totalUtil / n
	m.AvgTemperatureC = totalTemp / n
	return m, nil
}
