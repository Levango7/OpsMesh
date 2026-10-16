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
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	gnet "github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"

	"github.com/Levango7/OpsMesh/internal/proto"
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
	collectHost(m)                 // OS/内核/运行时长/主机名
	collectCPU(m)                  // CPU 核心数/使用率/型号
	collectMem(m)                  // 内存总量/已用/可用/使用率
	collectDisks(&m.Disks)         // 各分区容量/使用率
	collectInodes(&m.Inodes)       // 各分区 inode 使用率
	collectIOPS(&m.IOPS)           // 各分区 IOPS/延迟
	collectNet(m)                  // 各网卡 IP/MAC/收发字节
	collectServices(m)             // 关注的服务状态
	collectProcessCount(m)         // 进程数
	collectProcesses(&m.Processes) // 进程 Top 列表（按 CPU/内存排序）
	collectGPU(m)                  // GPU 指标（NVIDIA/AMD，无 GPU 时为空）
	collectEnvs(m)                 // 环境依赖版本（glibc/openssl/python/java 等）
	collectSystem(m)               // 系统清单（用户/组/防火墙，5 分钟节流）
	return m
}

// systemInventoryInterval 系统清单采集节流间隔（用户/组/防火墙变化不频繁，5 分钟一次足够）。
const systemInventoryInterval = 5 * time.Minute

// systemInventoryLastCol 上次系统清单采集时间（包级，CollectMetrics 间共享）。
var systemInventoryLastCol time.Time

// collectSystem 采集系统清单（用户/组/防火墙规则），5 分钟节流：间隔内复用上次结果。
// 由于 DeviceMetrics 每次新建，节流时从缓存复制上次结果。
func collectSystem(m *proto.DeviceMetrics) {
	if time.Since(systemInventoryLastCol) < systemInventoryInterval && systemInventoryCache != nil {
		cp := *systemInventoryCache
		m.System = &cp
		return
	}
	inv := &proto.SystemInventory{CollectedAt: time.Now()}
	collectUsers(&inv.Users)
	collectGroups(&inv.Groups)
	collectFirewall(inv)
	systemInventoryCache = inv
	systemInventoryLastCol = time.Now()
	m.System = inv
}

// systemInventoryCache 系统清单缓存（节流复用，避免每次采集都执行 getent/iptables）。
var systemInventoryCache *proto.SystemInventory

// collectUsers 采集系统用户（Linux: getent passwd；Windows 跳过）。
func collectUsers(out *[]proto.UserInfo) {
	if runtime.GOOS != "linux" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "getent", "passwd").Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) < 7 {
			continue
		}
		uid := parseInt(fields[2])
		gid := parseInt(fields[3])
		*out = append(*out, proto.UserInfo{
			Name:   fields[0],
			UID:    uid,
			GID:    gid,
			Home:   fields[5],
			Shell:  fields[6],
			System: uid < 1000,
		})
	}
}

// collectGroups 采集系统用户组（Linux: getent group；Windows 跳过）。
func collectGroups(out *[]proto.GroupInfo) {
	if runtime.GOOS != "linux" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "getent", "group").Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) < 4 {
			continue
		}
		var members []string
		if fields[3] != "" {
			members = strings.Split(fields[3], ",")
		}
		*out = append(*out, proto.GroupInfo{
			Name:    fields[0],
			GID:     parseInt(fields[2]),
			Members: members,
		})
	}
}

// collectFirewall 采集防火墙规则（优先 firewalld，回退 iptables）。
func collectFirewall(inv *proto.SystemInventory) {
	if runtime.GOOS != "linux" {
		return
	}
	// 优先检测 firewalld。
	if _, err := exec.LookPath("firewall-cmd"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, "firewall-cmd", "--list-all").Output(); err == nil {
			inv.FirewallBackend = "firewalld"
			inv.Firewall = parseFirewalldOutput(string(out))
			return
		}
	}
	// 回退 iptables。
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "iptables", "-L", "-n", "--line-numbers").Output()
	if err != nil {
		inv.FirewallBackend = "none"
		return
	}
	inv.FirewallBackend = "iptables"
	inv.Firewall = parseIptablesOutput(string(out))
}

// parseIptablesOutput 解析 iptables -L -n 输出为结构化规则。
func parseIptablesOutput(output string) []proto.FirewallRule {
	var rules []proto.FirewallRule
	chain := ""
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// 链头：Chain INPUT (policy ACCEPT)
		if strings.HasPrefix(line, "Chain ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				chain = fields[1]
			}
			continue
		}
		// 跳过表头（num target prot opt source destination）。
		if strings.HasPrefix(line, "num") || strings.HasPrefix(line, "pkts") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		// 格式：num target prot opt source destination [额外匹配]
		rule := proto.FirewallRule{
			Chain:    chain,
			Target:   fields[1],
			Protocol: fields[2],
			Source:   fields[4],
		}
		// 逐行原始文本作为 Rule（便于排查）。
		rule.Rule = line
		rules = append(rules, rule)
	}
	return rules
}

// parseFirewalldOutput 解析 firewall-cmd --list-all 输出为结构化规则。
func parseFirewalldOutput(output string) []proto.FirewallRule {
	var rules []proto.FirewallRule
	zone := ""
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "public") || strings.HasPrefix(line, "trusted") ||
			strings.HasPrefix(line, "block") || strings.HasPrefix(line, "drop") {
			zone = strings.Fields(line)[0]
			continue
		}
		if strings.HasPrefix(line, "ports:") {
			ports := strings.TrimSpace(strings.TrimPrefix(line, "ports:"))
			if ports == "" {
				continue
			}
			for _, p := range strings.Fields(ports) {
				parts := strings.Split(p, "/")
				rule := proto.FirewallRule{Chain: zone, Target: "allow"}
				if len(parts) == 2 {
					rule.Port = parts[0]
					rule.Protocol = parts[1]
				}
				rule.Rule = "port " + p
				rules = append(rules, rule)
			}
		}
		if strings.HasPrefix(line, "services:") {
			svcs := strings.TrimSpace(strings.TrimPrefix(line, "services:"))
			for _, s := range strings.Fields(svcs) {
				rules = append(rules, proto.FirewallRule{
					Chain:  zone,
					Target: "allow",
					Rule:   "service " + s,
				})
			}
		}
	}
	return rules
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

// collectGPU 采集 GPU 指标（优先 nvidia-smi，回退 rocm-smi；无 GPU 时留空）。
// 输出格式（逗号分隔）：index,name,uuid,utilization.gpu,memory.total,memory.used,temperature.gpu,power.draw,power.limit,driver_version
// 单 GPU 时单行输出；多 GPU 多行。
func collectGPU(m *proto.DeviceMetrics) {
	out, err := exec.Command("nvidia-smi", "--query-gpu=index,name,uuid,utilization.gpu,memory.total,memory.used,temperature.gpu,power.draw,power.limit,driver_version", "--format=csv,noheader,nounits").Output()
	if err != nil {
		// 回退 rocm-smi（AMD GPU）。
		out, err = exec.Command("rocm-smi", "--showproductname", "--showuuid", "--showuse", "--showmemuse", "--showtemp", "--showpower", "--showpowerlimit", "--showdriverversion", "--csv").Output()
		if err != nil {
			return // 无 GPU 或工具不可用，留空
		}
		m.GPUs = parseROCmSmiOutput(string(out))
		return
	}
	m.GPUs = parseNvidiaSmiOutput(string(out))
}

// parseNvidiaSmiOutput 解析 nvidia-smi CSV 输出。
func parseNvidiaSmiOutput(output string) []proto.GPUMetrics {
	var gpus []proto.GPUMetrics
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 10 {
			continue
		}
		g := proto.GPUMetrics{}
		g.Index = parseInt(fields[0])
		g.Name = strings.TrimSpace(fields[1])
		g.UUID = strings.TrimSpace(fields[2])
		g.Usage = parseFloat(fields[3])
		g.MemTotal = parseUint(fields[4])
		g.MemUsed = parseUint(fields[5])
		if g.MemTotal > 0 {
			g.MemUsage = float64(g.MemUsed) / float64(g.MemTotal) * 100
		}
		g.Temp = parseInt(fields[6])
		g.PowerDraw = parseFloat(fields[7])
		g.PowerLimit = parseFloat(fields[8])
		g.Driver = strings.TrimSpace(fields[9])
		gpus = append(gpus, g)
	}
	return gpus
}

// parseROCmSmiOutput 解析 rocm-smi CSV 输出（简化版，仅保留关键字段）。
func parseROCmSmiOutput(output string) []proto.GPUMetrics {
	var gpus []proto.GPUMetrics
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		return gpus
	}
	// 跳过表头，逐行解析（格式依 rocm-smi 版本而异，此处尽力解析）。
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 3 {
			continue
		}
		g := proto.GPUMetrics{}
		g.Name = strings.TrimSpace(fields[0])
		g.UUID = strings.TrimSpace(fields[1])
		gpus = append(gpus, g)
	}
	return gpus
}

// collectEnvs 采集环境依赖版本信息（glibc/openssl/python3/java/node等）。
// 任一命令失败不中断，仅跳过该依赖。
func collectEnvs(m *proto.DeviceMetrics) {
	checks := []struct {
		name  string
		cmd   string
		args  []string
		parse func(string) string
	}{
		{"glibc", "ldd", []string{"--version"}, parseFirstLine},
		{"openssl", "openssl", []string{"version"}, parseFirstLine},
		{"python3", "python3", []string{"--version"}, parseFirstLine},
		{"java", "java", []string{"-version"}, parseJavaVersion},
		{"node", "node", []string{"--version"}, parseFirstLine},
		{"git", "git", []string{"--version"}, parseFirstLine},
		{"docker", "docker", []string{"version", "--format", "{{.Server.Version}}"}, parseFirstLine},
		{"kubectl", "kubectl", []string{"version", "--client", "--short"}, parseFirstLine},
		{"opsmesh-agent", "opsmesh-agent", []string{"--version"}, parseFirstLine},
	}
	for _, c := range checks {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		out, err := exec.CommandContext(ctx, c.cmd, c.args...).CombinedOutput()
		cancel()
		if err != nil {
			continue // 命令不存在或执行失败，跳过
		}
		ver := c.parse(string(out))
		if ver == "" {
			continue
		}
		m.Envs = append(m.Envs, proto.EnvDependency{
			Name:    c.name,
			Version: ver,
		})
	}
}

// parseFirstLine 取输出第一行并去空白。
func parseFirstLine(out string) string {
	return strings.TrimSpace(strings.Split(out, "\n")[0])
}

// parseJavaVersion 解析 java -version 输出（版本在双引号内，如 "17.0.5"）。
func parseJavaVersion(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, `"`); i >= 0 {
			rest := line[i+1:]
			if j := strings.Index(rest, `"`); j >= 0 {
				return rest[:j]
			}
		}
	}
	return ""
}

// parseInt 安全解析 int（失败返回 0）。
func parseInt(s string) int {
	var n int
	_, _ = fmt.Sscanf(strings.TrimSpace(s), "%d", &n)
	return n
}

// parseFloat 安全解析 float64（失败返回 0）。
func parseFloat(s string) float64 {
	var n float64
	_, _ = fmt.Sscanf(strings.TrimSpace(s), "%f", &n)
	return n
}

// parseUint 安全解析 uint64（失败返回 0）。
func parseUint(s string) uint64 {
	var n uint64
	_, _ = fmt.Sscanf(strings.TrimSpace(s), "%d", &n)
	return n
}

// collectInodes 采集各分区 inode 使用率（Linux df -i，Windows 跳过）。
func collectInodes(out *[]proto.DiskInodeMetrics) {
	if runtime.GOOS != "linux" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "df", "-i", "-P", "--output=target,iused,ifree,ipcent")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for i, line := range lines {
		if i == 0 {
			continue // skip header
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		dim := proto.DiskInodeMetrics{
			Mount: fields[0],
		}
		dim.Used = parseUint(fields[1])
		dim.Free = parseUint(fields[2])
		dim.Total = dim.Used + dim.Free
		if dim.Total > 0 {
			dim.Usage = float64(dim.Used) / float64(dim.Total) * 100
		}
		*out = append(*out, dim)
	}
}

// collectIOPS 采集各分区 IOPS/延迟（Linux /proc/diskstats，Windows 跳过）。
func collectIOPS(out *[]proto.DiskIOMetrics) {
	if runtime.GOOS != "linux" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "cat", "/proc/diskstats")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 14 {
			continue
		}
		// /proc/diskstats fields: major minor name reads completions merges sectors_ticks read_ticks writes writes_ticks io_ticks weighted_io_ticks discards discard_ticks discard_ticks flush_ticks
		device := fields[2]
		// Skip loop/ram devices
		if strings.HasPrefix(device, "loop") || strings.HasPrefix(device, "ram") {
			continue
		}
		readsCompleted := parseUint(fields[3])
		writesCompleted := parseUint(fields[7])
		ioTicks := parseUint(fields[12]) // io_ticks (ms)

		// Simple IOPS calculation (reads + writes per sample interval)
		// Note: this is a cumulative counter, actual IOPS would need delta over time
		dim := proto.DiskIOMetrics{
			Mount:     device,
			ReadIOPS:  float64(readsCompleted),
			WriteIOPS: float64(writesCompleted),
		}
		if ioTicks > 0 && (readsCompleted+writesCompleted) > 0 {
			dim.ReadLatency = float64(ioTicks) / float64(readsCompleted+writesCompleted)
			dim.WriteLatency = dim.ReadLatency
		}
		*out = append(*out, dim)
	}
}

// collectProcesses 采集进程 Top 列表（按 CPU 使用率降序，最多 20 条）。
func collectProcesses(out *[]proto.ProcessInfo) {
	processes, err := process.Processes()
	if err != nil {
		return
	}
	type procInfo struct {
		p   *process.Process
		cpu float64
		mem float64
	}
	var list []procInfo
	for _, p := range processes {
		cpu, err := p.CPUPercent()
		if err != nil {
			continue
		}
		mem32, err := p.MemoryPercent()
		if err != nil {
			continue
		}
		list = append(list, procInfo{p: p, cpu: cpu, mem: float64(mem32)})
	}
	// Sort by CPU descending
	sort.Slice(list, func(i, j int) bool {
		return list[i].cpu > list[j].cpu
	})
	limit := 20
	if len(list) < limit {
		limit = len(list)
	}
	for i := 0; i < limit; i++ {
		p := list[i].p
		cmdline, _ := p.Cmdline()
		username, _ := p.Username()
		statusList, _ := p.Status()
		status := ""
		if len(statusList) > 0 {
			status = statusList[0]
		}
		memBytes, _ := p.MemoryInfo()
		*out = append(*out, proto.ProcessInfo{
			PID:      int32(p.Pid),
			Name:     func() string { n, _ := p.Name(); return n }(),
			CPU:      list[i].cpu,
			Mem:      list[i].mem,
			MemBytes: memBytes.RSS / 1024 / 1024,
			Status:   status,
			Cmd:      strings.Join(strings.Fields(cmdline), " "),
			User:     username,
		})
	}
}
