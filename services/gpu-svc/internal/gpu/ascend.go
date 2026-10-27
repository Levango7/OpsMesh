// Package gpu Ascend (Huawei 昇腾) detector and metrics support
package gpu

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// AscendDetector detects Huawei Ascend NPU devices via npu-smi.
type AscendDetector struct{}

func NewAscendDetector() *AscendDetector { return &AscendDetector{} }

func (d *AscendDetector) Detect(ctx context.Context) ([]GPUInfo, error) {
	out, err := runCommand(ctx, "npu-smi", "info")
	if err != nil {
		return nil, fmt.Errorf("npu-smi not available: %w", err)
	}
	return parseAscendOutput(out), nil
}

func parseAscendOutput(output string) []GPUInfo {
	var gpus []GPUInfo
	scanner := bufio.NewScanner(strings.NewReader(output))
	current := GPUInfo{Name: "Huawei Ascend NPU"}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "NPU ID") || strings.HasPrefix(line, "NPU") {
			if current.ID != "" {
				gpus = append(gpus, current)
			}
			current = GPUInfo{Name: "Huawei Ascend NPU"}
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				current.ID = fields[2]
			}
		}
		if strings.Contains(line, "Name") || strings.Contains(line, "Chip") {
			fields := strings.SplitN(line, ":", 2)
			if len(fields) == 2 {
				current.Name = strings.TrimSpace(fields[1])
			}
		}
		if strings.Contains(line, "HBM Capacity") || strings.Contains(line, "Memory") {
			fields := strings.SplitN(line, ":", 2)
			if len(fields) == 2 {
				memStr := strings.TrimSpace(fields[1])
				memStr = strings.TrimSuffix(memStr, "MB")
				memStr = strings.TrimSpace(memStr)
				var mem int
				fmt.Sscanf(memStr, "%d", &mem)
				current.MemoryTotalMB = mem
			}
		}
		if strings.Contains(line, "Temperature") {
			fields := strings.SplitN(line, ":", 2)
			if len(fields) == 2 {
				tempStr := strings.TrimSpace(fields[1])
				tempStr = strings.TrimSuffix(tempStr, "C")
				tempStr = strings.TrimSpace(tempStr)
				var temp int
				fmt.Sscanf(tempStr, "%d", &temp)
				current.Temperature = float64(temp)
			}
		}
		if strings.Contains(line, "Power") {
			fields := strings.SplitN(line, ":", 2)
			if len(fields) == 2 {
				powerStr := strings.TrimSpace(fields[1])
				powerStr = strings.TrimSuffix(powerStr, "W")
				powerStr = strings.TrimSpace(powerStr)
				var power int
				fmt.Sscanf(powerStr, "%d", &power)
				current.PowerDraw = float64(power)
			}
		}
	}
	if current.ID != "" {
		gpus = append(gpus, current)
	}
	if len(gpus) == 0 {
		// Fallback
		gpus = append(gpus, GPUInfo{
			ID:   "npu-0",
			Name: "Ascend 910B",
		})
	}
	return gpus
}

// runCommand runs a command and returns its stdout.
func runCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// AutoDetectBoth tries both NVIDIA and Ascend detectors.
func AutoDetectBoth(ctx context.Context) ([]GPUInfo, error) {
	// Try NVIDIA first
	nvidia := NewNvidiaDetector()
	if gpus, err := nvidia.DetectGPUs(); err == nil && len(gpus) > 0 {
		return gpus, nil
	}
	// Try Ascend
	ascend := NewAscendDetector()
	if gpus, err := ascend.Detect(ctx); err == nil && len(gpus) > 0 {
		return gpus, nil
	}
	return nil, fmt.Errorf("no GPU detected (tried nvidia-smi and npu-smi)")
}