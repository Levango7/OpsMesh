// metrics_collect.go 实现 agent 端跨平台系统指标采集（CPU/内存/磁盘/网络/OS/服务/进程）。
//
// 依赖 github.com/shirou/gopsutil/v3（成熟跨平台库，支持 Windows/Linux/macOS）。
// 采集频率：心跳每 10s 一次，但指标采集每 30s 一次（在 agent.go 的 heartbeatLoop 中节流），
// 减少系统开销（cpu.Percent 等需要采样间隔，频繁调用本身也消耗 CPU）。
//
// 设计取舍：
//   - 仅采集常见运维相关服务（sshd/nginx/mysql/docker/redis/opsmesh 等），不列全部服务，
//     避免输出过长且无关服务干扰运维视线。
//   - DeviceMetrics 同时保留最新值与最近 N 小时历史快照（环形缓冲，默认 2h/240 条），
//     控制面缓存供 API 查询（GET /api/v1/devices/{id}/metrics?range=2h）。
package agent

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	gnet "github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"

	"opsmesh/internal/proto"
)

// MetricsHistoryDefaultCap 环形缓冲默认容量：2h * 120 samples/h（30s 采样间隔）= 240 条。
// 每条 DeviceMetrics 约 1KB，总 ~240KB/设备。
const MetricsHistoryDefaultCap = 240

// MetricsHistory 环形缓冲：保存最近 N 小时的设备指标快照。
// 用 slice + head index 实现，O(1) 追加 O(n) 读取。
// 默认保留 2 小时历史（240 条，30s 采样间隔），可经 NewMetricsHistory(capacity) 自定义。
// 线程安全：所有方法内部加锁，可被采集 goroutine 与查询 goroutine 并发访问。
type MetricsHistory struct {
	samples  []proto.DeviceMetrics // 环形缓冲 slice（固定容量，覆写最旧）
	head     int                   // 下一个写入位置（0..capacity-1）
	size     int                   // 当前已写入数量（<= capacity）
	capacity int                   // 缓冲容量
	mu       sync.Mutex            // 保护 samples/head/size 并发读写
}

// NewMetricsHistory 创建环形缓冲。capacity<=0 时用 MetricsHistoryDefaultCap（240）。
func NewMetricsHistory(capacity int) *MetricsHistory {
	if capacity <= 0 {
		capacity = MetricsHistoryDefaultCap
	}
	return &MetricsHistory{
		samples:  make([]proto.DeviceMetrics, capacity),
		capacity: capacity,
	}
}

// Add 追加一条指标快照到环形缓冲（O(1)）。m 为 nil 时直接返回（不写空记录）。
// 深拷贝入参避免外部并发修改污染缓冲。
func (h *MetricsHistory) Add(m *proto.DeviceMetrics) {
	if h == nil || m == nil {
		return
	}
	cp := *m
	h.mu.Lock()
	h.samples[h.head] = cp
	h.head = (h.head + 1) % h.capacity
	if h.size < h.capacity {
		h.size++
	}
	h.mu.Unlock()
}

// Latest 返回最近一条指标快照（无数据时返回 nil）。返回深拷贝避免外部修改污染缓冲。
func (h *MetricsHistory) Latest() *proto.DeviceMetrics {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.size == 0 {
		return nil
	}
	// head 指向下一个写入位置，最新一条在 (head-1+capacity)%capacity。
	idx := (h.head - 1 + h.capacity) % h.capacity
	cp := h.samples[idx]
	return &cp
}

// Since 返回 CollectedAt >= since 的所有快照（按时间升序）。
// since 为零值时返回全部已存储快照。无数据时返回 nil。
// 返回深拷贝避免外部修改污染缓冲。
func (h *MetricsHistory) Since(since time.Time) []proto.DeviceMetrics {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.size == 0 {
		return nil
	}
	// 最早一条的位置：若 size<capacity，从 0 开始；否则从 head 开始（head 指向最旧）。
	start := 0
	if h.size == h.capacity {
		start = h.head
	}
	out := make([]proto.DeviceMetrics, 0, h.size)
	for i := 0; i < h.size; i++ {
		idx := (start + i) % h.capacity
		s := h.samples[idx]
		if !since.IsZero() && s.CollectedAt.Before(since) {
			continue
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Size 返回当前已存储的样本数（<= capacity）。
func (h *MetricsHistory) Size() int {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.size
}

// Cap 返回环形缓冲容量。
func (h *MetricsHistory) Cap() int {
	if h == nil {
		return 0
	}
	return h.capacity
}

// monitoredServices 是关注的服务名白名单（常见运维相关服务）。
// 仅采集这些服务的状态，避免列全部服务导致输出过长且干扰运维视线。
// 名字按平台约定：Linux 用 systemctl 单元名（如 sshd），Windows 用服务名（如 sshd）。
var monitoredServices = []string{
	"sshd", "opensshd", // SSH
	"nginx", "apache2", "httpd", "caddy", // Web 服务器
	"mysql", "mariadb", "postgresql", // 数据库
	"docker", "containerd", "podman", // 容器运行时
	"redis", "memcached", "mongodb", // 缓存/NoSQL
	"etcd", "consul", "zookeeper", // 服务发现/协调
	"prometheus", "grafana", "node_exporter", // 监控
	"opsmesh", "opsmesh-agent", // 本系统
}

// CollectMetrics 采集当前主机的系统指标（CPU/内存/磁盘/网络/OS/服务/进程）。
// 返回 *proto.DeviceMetrics；任一子采集失败不中断整体，仅对应字段为零值（降级而非报错）。
// deviceID 由调用方（agent）填入，此处采集不关心设备身份。
func CollectMetrics(deviceID string) *proto.DeviceMetrics {
	m := &proto.DeviceMetrics{
		DeviceID:    deviceID,
		Arch:        runtime.GOARCH,
		CollectedAt: time.Now(),
	}
	collectHost(m)         // OS/内核/运行时长/主机名
	collectCPU(m)          // CPU 核心数/使用率/型号
	collectMem(m)          // 内存总量/已用/可用/使用率
	collectDisks(&m.Disks) // 各分区容量/使用率
	collectNet(m)          // 各网卡 IP/MAC/收发字节
	collectServices(m)     // 关注的服务状态
	collectProcessCount(m) // 进程数
	return m
}

// collectHost 采集主机信息：操作系统/版本/内核/架构/运行时长/主机名。
func collectHost(m *proto.DeviceMetrics) {
	info, err := host.Info()
	if err != nil {
		return
	}
	m.Hostname = info.Hostname
	m.OS = info.OS // windows / linux / darwin（gopsutil 已标准化）
	m.OSVersion = strings.TrimSpace(info.Platform + " " + info.PlatformVersion)
	m.Kernel = info.KernelVersion
	m.Uptime = int64(info.Uptime)
}

// collectCPU 采集 CPU 指标：逻辑核心数、使用率（0-100）、型号。
// cpu.Percent 需要采样间隔，这里用 1s 阻塞采样（首次调用返回自启动以来平均值，
// 传 interval>0 则返回该间隔内的实时使用率）。
func collectCPU(m *proto.DeviceMetrics) {
	// 核心数。
	if counts, err := cpu.Counts(true); err == nil {
		m.CPU.Cores = counts
	}
	// 使用率：1s 采样间隔（阻塞 1s）。agent 30s 采集一次，1s 阻塞可接受。
	if percents, err := cpu.Percent(time.Second, false); err == nil && len(percents) > 0 {
		m.CPU.Usage = percents[0]
	}
	// 型号。
	if infos, err := cpu.Info(); err == nil && len(infos) > 0 {
		m.CPU.Model = infos[0].ModelName
	}
}

// collectMem 采集内存指标：总量/已用/可用/使用率（单位 MB）。
func collectMem(m *proto.DeviceMetrics) {
	v, err := mem.VirtualMemory()
	if err != nil {
		return
	}
	const mb = 1024 * 1024
	m.Memory.Total = v.Total / mb
	m.Memory.Used = v.Used / mb
	m.Memory.Available = v.Available / mb
	m.Memory.Usage = v.UsedPercent
}

// collectDisks 采集各分区指标：挂载点/总量/已用/可用/使用率/文件系统类型（单位 GB）。
// 仅采集物理分区（disk.Partitions(false) 仅物理挂载点），过滤掉常见的 tmpfs/proc/sysfs 等。
func collectDisks(out *[]proto.DiskMetrics) {
	partitions, err := disk.Partitions(false)
	if err != nil {
		return
	}
	const gb = 1024 * 1024 * 1024
	skipFS := map[string]bool{
		"tmpfs": true, "devtmpfs": true, "proc": true, "sysfs": true,
		"cgroup": true, "none": true, "squashfs": true, "overlay": true,
	}
	for _, p := range partitions {
		if skipFS[p.Fstype] {
			continue
		}
		u, err := disk.Usage(p.Mountpoint)
		if err != nil || u.Total == 0 {
			continue
		}
		*out = append(*out, proto.DiskMetrics{
			Mount: p.Mountpoint,
			Total: u.Total / gb,
			Used:  u.Used / gb,
			Free:  u.Free / gb,
			Usage: u.UsedPercent,
			Type:  p.Fstype,
		})
	}
}

// collectNet 采集各网卡指标：名称/IP/MAC/收发字节/状态。
// 跳过 loopback 网卡，合并 net.Interfaces（IP/MAC）与 net.IOCounters（收发字节）。
func collectNet(m *proto.DeviceMetrics) {
	ifaces, err := gnet.Interfaces()
	if err != nil {
		return
	}
	// IOCounters 按网卡名索引，便于合并；采集失败时仅缺收发字节，不影响网卡列表。
	ioMap := make(map[string]gnet.IOCountersStat)
	if ioCounters, ioErr := gnet.IOCounters(true); ioErr == nil {
		for _, io := range ioCounters {
			ioMap[io.Name] = io
		}
	}
	for _, iface := range ifaces {
		if isLoopback(iface) {
			continue
		}
		ip := firstIPv4(iface)
		status := "down"
		if isFlagUp(iface) {
			status = "up"
		}
		nm := proto.NetMetrics{
			Name:   iface.Name,
			IP:     ip,
			MAC:    iface.HardwareAddr,
			Status: status,
		}
		if io, ok := ioMap[iface.Name]; ok {
			nm.RxBytes = io.BytesRecv
			nm.TxBytes = io.BytesSent
		}
		m.Network = append(m.Network, nm)
	}
}

// isLoopback 判断网卡是否为 loopback（gopsutil v3 中 Flags 为 []string，含 "loopback"）。
func isLoopback(iface gnet.InterfaceStat) bool {
	for _, f := range iface.Flags {
		if strings.EqualFold(f, "loopback") {
			return true
		}
	}
	return false
}

// isFlagUp 判断网卡是否处于 up 状态（Flags 含 "up"）。
func isFlagUp(iface gnet.InterfaceStat) bool {
	for _, f := range iface.Flags {
		if strings.EqualFold(f, "up") {
			return true
		}
	}
	return false
}

// firstIPv4 返回网卡第一个 IPv4 地址（无则返回空串）。
func firstIPv4(iface gnet.InterfaceStat) string {
	for _, addr := range iface.Addrs {
		s := addr.Addr
		// 去掉 CIDR 后缀（如 192.168.1.10/24 -> 192.168.1.10）。
		if i := strings.IndexByte(s, '/'); i >= 0 {
			s = s[:i]
		}
		// 简单 IPv4 判定：含 3 个 '.'。
		if strings.Count(s, ".") == 3 {
			return s
		}
	}
	return ""
}

// collectServices 采集关注的服务状态（仅 monitoredServices 白名单）。
// Windows: sc query <name>；Linux: systemctl is-active <name> + systemctl is-enabled <name>。
// 任一服务查询失败不中断，仅跳过该服务。
func collectServices(m *proto.DeviceMetrics) {
	for _, name := range monitoredServices {
		status, enabled := queryService(name)
		if status == "" {
			continue // 服务不存在或查询失败，跳过
		}
		m.Services = append(m.Services, proto.ServiceInfo{
			Name:    name,
			Status:  status,
			Enabled: enabled,
		})
	}
}

// queryService 查询单个服务状态：返回 status（running/stopped）与 enabled（是否开机自启）。
// 服务不存在或查询失败时返回空 status。
func queryService(name string) (status string, enabled bool) {
	if runtime.GOOS == "windows" {
		return queryServiceWindows(name)
	}
	return queryServiceLinux(name)
}

// queryServiceWindows 用 sc query <name> 查询 Windows 服务状态。
// 输出含 "STATE" 行，如 "STATE              : 4  RUNNING"。
func queryServiceWindows(name string) (status string, enabled bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "sc", "query", name).CombinedOutput()
	if err != nil {
		return "", false
	}
	// 解析 STATE 行。
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "STATE") {
			// 格式：STATE              : 4  RUNNING
			parts := strings.SplitN(line, ":", 2)
			if len(parts) < 2 {
				continue
			}
			val := strings.TrimSpace(parts[1])
			// val 如 "4  RUNNING"，取最后一段。
			fields := strings.Fields(val)
			if len(fields) >= 1 {
				state := strings.ToLower(fields[len(fields)-1])
				if state == "running" {
					status = "running"
				} else {
					status = "stopped"
				}
			}
		}
	}
	if status == "" {
		return "", false
	}
	// 查询启动类型（sc qc 输出含 START_TYPE）。
	qctx, qcancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer qcancel()
	qout, qerr := exec.CommandContext(qctx, "sc", "qc", name).CombinedOutput()
	if qerr != nil {
		// sc qc 失败（如服务不存在）不影响 status，仅 enabled 置 false
		enabled = false
	} else {
		for _, line := range strings.Split(string(qout), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "START_TYPE") {
				// 格式：START_TYPE      : 2  AUTO_START
				if strings.Contains(strings.ToUpper(line), "AUTO_START") {
					enabled = true
				}
				break
			}
		}
	}
	return status, enabled
}

// queryServiceLinux 用 systemctl is-active/is-enabled 查询 Linux 服务状态。
func queryServiceLinux(name string) (status string, enabled bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "is-active", name).Output()
	if err != nil {
		// systemctl is-active 对 inactive 服务返回非 0 退出码 + "inactive" 输出。
		s := strings.TrimSpace(string(out))
		if s == "inactive" || s == "failed" {
			status = "stopped"
		} else {
			return "", false // 服务不存在或 systemctl 不可用
		}
	} else {
		s := strings.TrimSpace(string(out))
		switch s {
		case "active":
			status = "running"
		case "inactive", "failed", "deactivating":
			status = "stopped"
		default:
			return "", false
		}
	}
	// 查询是否开机自启。
	ectx, ecancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer ecancel()
	eout, eerr := exec.CommandContext(ectx, "systemctl", "is-enabled", name).Output()
	if eerr != nil {
		// systemctl is-enabled 失败（如服务不存在）视为非开机自启
		enabled = false
	} else {
		es := strings.TrimSpace(string(eout))
		enabled = es == "enabled" || es == "enabled-runtime" || es == "static"
	}
	return status, enabled
}

// collectProcessCount 采集当前进程总数。
func collectProcessCount(m *proto.DeviceMetrics) {
	pids, err := process.Pids()
	if err != nil {
		return
	}
	m.ProcessCount = len(pids)
}
