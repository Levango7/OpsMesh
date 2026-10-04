// Device Simulator for OpsMesh
// Simulates N devices registering, sending heartbeats, and reporting metrics.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Configuration holds all simulator parameters.
type Configuration struct {
	NumDevices        int
	HeartbeatInterval time.Duration
	BatchSize         int
	ControlPlaneURL   string
	FailureRate       float64
	HighLoadRate      float64
}

// Device represents a simulated device.
type Device struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	OSType    string    `json:"os_type"`
	OSVersion string    `json:"os_version"`
	Arch      string    `json:"arch"`
	IPAddress string    `json:"ip_address"`
	Status    string    `json:"status"`
	LastSeen  time.Time `json:"last_seen"`
	CPU       float64   `json:"cpu_percent"`
	Memory    float64   `json:"memory_percent"`
	Disk      float64   `json:"disk_percent"`
	NetIn     int64     `json:"net_in_bytes"`
	NetOut    int64     `json:"net_out_bytes"`
	GPUs      []GPUInfo `json:"gpus,omitempty"`
	Failures  int64     `json:"failures"`
	TasksRun  int64     `json:"tasks_run"`
}

// GPUInfo represents simulated GPU data.
type GPUInfo struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Utilization float64 `json:"utilization"`
	MemoryUsed  int64   `json:"memory_used_mb"`
	MemoryTotal int64   `json:"memory_total_mb"`
	Temperature float64 `json:"temperature_c"`
}

// HeartbeatPayload is the data sent in each heartbeat.
type HeartbeatPayload struct {
	DeviceID  string    `json:"device_id"`
	Timestamp time.Time `json:"timestamp"`
	Status    string    `json:"status"`
	CPU       float64   `json:"cpu_percent"`
	Memory    float64   `json:"memory_percent"`
	Disk      float64   `json:"disk_percent"`
	NetIn     int64     `json:"net_in_bytes"`
	NetOut    int64     `json:"net_out_bytes"`
	GPUs      []GPUInfo `json:"gpus,omitempty"`
	TasksRun  int64     `json:"tasks_run"`
	Failures  int64     `json:"failures"`
}

// TaskResult reports task execution back to controlplane.
type TaskResult struct {
	DeviceID  string    `json:"device_id"`
	TaskID    string    `json:"task_id"`
	Status    string    `json:"status"`
	Output    string    `json:"output"`
	Duration  int64     `json:"duration_ms"`
	Timestamp time.Time `json:"timestamp"`
}

var (
	osTypes    = []string{"linux", "windows", "macos"}
	osVersions = map[string][]string{
		"linux":   {"Ubuntu 22.04", "Ubuntu 24.04", "Debian 12", "CentOS 9", "RHEL 9", "Alpine 3.20"},
		"windows": {"Windows 11", "Windows Server 2022", "Windows Server 2025"},
		"macos":   {"macOS 14", "macOS 15"},
	}
	archs    = []string{"amd64", "arm64", "arm"}
	gpuNames = []string{
		"NVIDIA RTX 4090", "NVIDIA RTX 4080", "NVIDIA A100",
		"NVIDIA H100", "AMD RX 7900 XTX", "NVIDIA RTX 3090",
	}
)

func main() {
	cfg := parseFlags()

	log.Printf("[simulator] Starting %d devices, interval=%s, batch=%d",
		cfg.NumDevices, cfg.HeartbeatInterval, cfg.BatchSize)
	log.Printf("[simulator] Target: %s", cfg.ControlPlaneURL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("[simulator] Shutting down...")
		cancel()
	}()

	// Create devices
	devices := make([]*Device, cfg.NumDevices)
	for i := 0; i < cfg.NumDevices; i++ {
		devices[i] = createDevice(i)
	}

	// Register devices in batches
	var registered int64
	var wg sync.WaitGroup

	for i := 0; i < cfg.NumDevices; i += cfg.BatchSize {
		end := i + cfg.BatchSize
		if end > cfg.NumDevices {
			end = cfg.NumDevices
		}

		batch := devices[i:end]
		for _, dev := range batch {
			wg.Add(1)
			go func(d *Device) {
				defer wg.Done()
				if err := registerDevice(ctx, cfg, d); err != nil {
					log.Printf("[simulator] Device %s registration failed: %v", d.ID, err)
					return
				}
				atomic.AddInt64(&registered, 1)
			}(dev)
		}

		// Small delay between batches
		time.Sleep(100 * time.Millisecond)
	}

	// Wait for registration with timeout
	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneCh)
	}()

	select {
	case <-doneCh:
		log.Printf("[simulator] %d/%d devices registered", registered, cfg.NumDevices)
	case <-time.After(30 * time.Second):
		log.Printf("[simulator] Registration timeout (%d/%d registered)", atomic.LoadInt64(&registered), cfg.NumDevices)
	}

	// Start heartbeats
	var heartbeatCount, errorCount int64
	var heartbeatWg sync.WaitGroup

	for _, dev := range devices {
		heartbeatWg.Add(1)
		go func(d *Device) {
			defer heartbeatWg.Done()
			ticker := time.NewTicker(cfg.HeartbeatInterval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					err := sendHeartbeat(ctx, cfg, d)
					if err != nil {
						atomic.AddInt64(&errorCount, 1)
					} else {
						atomic.AddInt64(&heartbeatCount, 1)
					}

					// Simulate task execution
					if rand.Float64() < 0.3 {
						result := simulateTask(d)
						// 结果上报失败只影响模拟统计，不中断心跳循环；此处留痕即可。
						if err := reportTaskResult(ctx, cfg, result); err != nil {
							log.Printf("device-sim: reportTaskResult failed: %v", err)
						}
					}
				}
			}
		}(dev)
	}

	// Stats reporter
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				hb := atomic.LoadInt64(&heartbeatCount)
				errs := atomic.LoadInt64(&errorCount)
				log.Printf("[simulator] Stats: heartbeats=%d errors=%d devices=%d",
					hb, errs, registered)
			}
		}
	}()

	<-ctx.Done()
	heartbeatWg.Wait()

	log.Printf("[simulator] Final: heartbeats=%d errors=%d",
		atomic.LoadInt64(&heartbeatCount), atomic.LoadInt64(&errorCount))
	log.Println("[simulator] Shutdown complete")
}

func parseFlags() Configuration {
	numDevices := flag.Int("devices", 10, "Number of devices to simulate")
	interval := flag.String("interval", "10s", "Heartbeat interval")
	batchSize := flag.Int("batch", 5, "Batch size for registration")
	url := flag.String("url", "http://localhost:8080", "Controlplane URL")
	failureRate := flag.Float64("failure-rate", 0.05, "Probability of device failure per heartbeat")
	highLoadRate := flag.Float64("highload-rate", 0.1, "Probability of high load simulation")

	flag.Parse()

	heartbeatInterval, err := time.ParseDuration(*interval)
	if err != nil {
		log.Fatalf("Invalid interval: %v", err)
	}

	return Configuration{
		NumDevices:        *numDevices,
		HeartbeatInterval: heartbeatInterval,
		BatchSize:         *batchSize,
		ControlPlaneURL:   *url,
		FailureRate:       *failureRate,
		HighLoadRate:      *highLoadRate,
	}
}

func createDevice(index int) *Device {
	osType := osTypes[rand.Intn(len(osTypes))]
	versions := osVersions[osType]
	osVersion := versions[rand.Intn(len(versions))]
	arch := archs[rand.Intn(len(archs))]

	// Generate IP
	ip := fmt.Sprintf("10.%d.%d.%d",
		rand.Intn(254), rand.Intn(254), rand.Intn(254)+1)

	dev := &Device{
		ID:        fmt.Sprintf("dev-%s-%04d", randomString(6), index),
		Name:      fmt.Sprintf("sim-%s-%d", osType, index),
		OSType:    osType,
		OSVersion: osVersion,
		Arch:      arch,
		IPAddress: ip,
		Status:    "online",
		LastSeen:  time.Now(),
	}

	// Some devices have GPUs
	if rand.Float64() < 0.4 {
		numGPUs := rand.Intn(3) + 1
		for i := 0; i < numGPUs; i++ {
			memTotal := int64((rand.Intn(16) + 4) * 1024)
			dev.GPUs = append(dev.GPUs, GPUInfo{
				ID:          fmt.Sprintf("gpu-%d", i),
				Name:        gpuNames[rand.Intn(len(gpuNames))],
				MemoryTotal: memTotal,
			})
		}
	}

	return dev
}

// simHTTPClient 是模拟器的出网 client。
//
// 为什么不用 http.DefaultClient（修复记录 2026-10-04）：
// DefaultClient.Timeout 为 0，只在 ctx 带 deadline 时才受保护。模拟器注册/
// 心跳的 ctx 来自主流程，父上下文未必有 deadline；一旦对端 TCP 建连后不回
// 数据（防火墙丢包、连接被劫持后静默），请求会无限期挂着，心跳 goroutine
// 逐个卡死，模拟器"还活着但一台设备都没注册上"，且没有任何日志线索。
//
// 8 秒对模拟器足够宽裕（它打的是自家控制面），又能保证故障可观测。
var simHTTPClient = &http.Client{Timeout: 8 * time.Second}

func registerDevice(ctx context.Context, cfg Configuration, d *Device) error {
	payload, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/devices/register", cfg.ControlPlaneURL)

	// 重试必须**重建** request，不能复用同一个 req：
	// http.Request 的 Body 是 io.ReadCloser，第一次 Do 已被读完（读到 EOF），
	// 第二次 Do 会发出空 body。控制面收到空 body 会解析失败，
	// 于是"重试"变成"发一个更坏的请求"，且现象是注册莫名失败而非重试成功。
	// （此处此前即存在该缺陷——DefaultClient 掩盖了它，因为没人注意到重试路径。）
	send := func() (*http.Response, error) {
		req, rerr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if rerr != nil {
			return nil, fmt.Errorf("request: %w", rerr)
		}
		req.Header.Set("Content-Type", "application/json")
		return simHTTPClient.Do(req)
	}

	resp, err := send()
	if err != nil {
		// Retry once
		time.Sleep(time.Second)
		resp, err = send()
		if err != nil {
			return fmt.Errorf("do: %w", err)
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		d.Status = "online"
		return nil
	}

	// Non-2xx but we continue (controlplane may not have this endpoint yet)
	return nil
}

func sendHeartbeat(ctx context.Context, cfg Configuration, d *Device) error {
	// Simulate metrics
	d.CPU = randomMetric(5, 45, cfg.HighLoadRate)
	d.Memory = randomMetric(20, 60, cfg.HighLoadRate)
	d.Disk = randomMetric(30, 70, 0)
	d.NetIn += int64(rand.Intn(1000000))
	d.NetOut += int64(rand.Intn(500000))

	// Update GPU metrics
	for i := range d.GPUs {
		d.GPUs[i].Utilization = randomMetric(0, 30, cfg.HighLoadRate)
		d.GPUs[i].MemoryUsed = int64(float64(d.GPUs[i].MemoryTotal) * randomMetric(0.1, 0.5, 0))
		d.GPUs[i].Temperature = 40 + rand.Float64()*40
	}

	// Simulate occasional failures
	if rand.Float64() < cfg.FailureRate {
		d.Status = "offline"
		d.Failures++
	} else {
		d.Status = "online"
		d.LastSeen = time.Now()
	}

	payload := HeartbeatPayload{
		DeviceID:  d.ID,
		Timestamp: time.Now(),
		Status:    d.Status,
		CPU:       d.CPU,
		Memory:    d.Memory,
		Disk:      d.Disk,
		NetIn:     d.NetIn,
		NetOut:    d.NetOut,
		GPUs:      d.GPUs,
		TasksRun:  d.TasksRun,
		Failures:  d.Failures,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/devices/%s/heartbeat", cfg.ControlPlaneURL, d.ID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := simHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("do: %w", err)
	}
	defer resp.Body.Close()

	return nil
}

func simulateTask(d *Device) TaskResult {
	taskID := fmt.Sprintf("task-%s", randomString(8))
	statuses := []string{"success", "success", "success", "success", "failed"}
	status := statuses[rand.Intn(len(statuses))]
	duration := int64(rand.Intn(5000) + 100)

	if status == "success" {
		d.TasksRun++
	}

	return TaskResult{
		DeviceID:  d.ID,
		TaskID:    taskID,
		Status:    status,
		Output:    fmt.Sprintf("Task %s completed in %dms", taskID, duration),
		Duration:  duration,
		Timestamp: time.Now(),
	}
}

func reportTaskResult(ctx context.Context, cfg Configuration, result TaskResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/api/v1/tasks/%s/result", cfg.ControlPlaneURL, result.TaskID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := simHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

func randomMetric(min, max float64, highLoadProb float64) float64 {
	if rand.Float64() < highLoadProb {
		// High load: return value in upper range
		return min + (max-min)*0.8 + rand.Float64()*(max-min)*0.2
	}
	return min + rand.Float64()*(max-min)
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
