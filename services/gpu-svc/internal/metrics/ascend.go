// Package metrics Ascend NPU metrics collector
package metrics

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/models"
)

// AscendCollector collects metrics from Huawei Ascend NPU via npu-smi.
type AscendCollector struct {
	interval time.Duration
}

func NewAscendCollector(interval time.Duration) *AscendCollector {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &AscendCollector{interval: interval}
}

func (c *AscendCollector) Collect(ctx context.Context, nodeID string) (*models.GPUMetrics, error) {
	out, err := runCmd(ctx, "npu-smi", "info")
	if err != nil {
		return nil, fmt.Errorf("npu-smi not available: %w", err)
	}
	return parseAscendMetrics(out, nodeID), nil
}

func parseAscendMetrics(output, nodeID string) *models.GPUMetrics {
	m := &models.GPUMetrics{
		NodeID:    nodeID,
		Timestamp: time.Now(),
		Source:    "npu-smi",
	}
	scanner := bufio.NewScanner(strings.NewReader(output))
	var current models.GPUMetricsPerGPU
	current.Index = -1

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "NPU ID") || strings.HasPrefix(line, "NPU") {
			if current.Index >= 0 {
				m.GPUs = append(m.GPUs, current)
			}
			current = models.GPUMetricsPerGPU{}
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				// parse NPU index
				if idx, err := strconv.Atoi(fields[2]); err == nil {
					current.Index = idx
				}
			}
		}
		if strings.Contains(line, "HBM") || strings.Contains(line, "Memory") {
			fields := strings.SplitN(line, ":", 2)
			if len(fields) == 2 {
				memStr := strings.TrimSpace(fields[1])
				memStr = strings.TrimSuffix(memStr, "MB")
				memStr = strings.TrimSpace(memStr)
				if v, err := strconv.ParseInt(memStr, 10, 64); err == nil {
					current.MemoryUsedMB = int(v)
				}
			}
		}
		if strings.Contains(line, "Temperature") {
			fields := strings.SplitN(line, ":", 2)
			if len(fields) == 2 {
				tempStr := strings.TrimSpace(fields[1])
				tempStr = strings.TrimSuffix(tempStr, "C")
				tempStr = strings.TrimSpace(tempStr)
				if v, err := strconv.ParseFloat(tempStr, 64); err == nil {
					current.TemperatureC = v
				}
			}
		}
		if strings.Contains(line, "Power") {
			fields := strings.SplitN(line, ":", 2)
			if len(fields) == 2 {
				powerStr := strings.TrimSpace(fields[1])
				powerStr = strings.TrimSuffix(powerStr, "W")
				powerStr = strings.TrimSpace(powerStr)
				if v, err := strconv.ParseFloat(powerStr, 64); err == nil {
					current.PowerDrawW = v
				}
			}
		}
		if strings.Contains(line, "Utilization") || strings.Contains(line, "AICore") {
			fields := strings.SplitN(line, ":", 2)
			if len(fields) == 2 {
				utilStr := strings.TrimSpace(fields[1])
				utilStr = strings.TrimSuffix(utilStr, "%")
				utilStr = strings.TrimSpace(utilStr)
				if v, err := strconv.ParseFloat(utilStr, 64); err == nil {
					current.UtilizationPct = v
				}
			}
		}
	}
	if current.Index >= 0 {
		m.GPUs = append(m.GPUs, current)
	}

	// Aggregate
	var totalUtil, totalTemp float64
	for _, g := range m.GPUs {
		m.TotalMemoryUsedMB += g.MemoryUsedMB
		m.TotalMemoryTotalMB += g.MemoryTotalMB
		m.TotalPowerDrawW += g.PowerDrawW
		totalUtil += g.UtilizationPct
		totalTemp += g.TemperatureC
	}
	if len(m.GPUs) > 0 {
		n := float64(len(m.GPUs))
		m.AvgUtilization = totalUtil / n
		m.AvgTemperatureC = totalTemp / n
	}

	if len(m.GPUs) == 0 {
		// Fallback
		m.GPUs = append(m.GPUs, models.GPUMetricsPerGPU{
			Index:           0,
			UtilizationPct:  0,
			MemoryUsedMB:    0,
			MemoryTotalMB:   0,
			TemperatureC:    0,
			PowerDrawW:      0,
		})
	}
	return m
}

func runCmd(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}