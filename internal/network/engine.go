// Package network 实现网络管理引擎：网络设备模型、监控指标、拓扑结构、网络发现。
//
// 设计要点：
//   - 纯领域模型，无外部依赖，可被 controlplane/store 复用；
//   - 设备类型预置 switch/router/firewall/load_balancer 四类；
//   - 网络发现引擎基于子网扫描（MVP 桩实现，返回示例设备）；
//   - 拓扑结构用邻接表表示节点+链路。
package network

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// subnetMaxHosts 是 IPv4 /24 子网最大可用地址数（保留网络地址和广播地址）。
const subnetMaxHosts = 254

// DeviceType 网络设备类型。
type DeviceType string

const (
	DeviceTypeSwitch       DeviceType = "switch"        // 交换机
	DeviceTypeRouter       DeviceType = "router"        // 路由器
	DeviceTypeFirewall     DeviceType = "firewall"      // 防火墙
	DeviceTypeLoadBalancer DeviceType = "load_balancer" // 负载均衡
)

// AllDeviceTypes 返回全部预置设备类型。
func AllDeviceTypes() []DeviceType {
	return []DeviceType{DeviceTypeSwitch, DeviceTypeRouter, DeviceTypeFirewall, DeviceTypeLoadBalancer}
}

// ValidDeviceType 校验设备类型是否合法。
func ValidDeviceType(t DeviceType) bool {
	switch t {
	case DeviceTypeSwitch, DeviceTypeRouter, DeviceTypeFirewall, DeviceTypeLoadBalancer:
		return true
	}
	return false
}

// DeviceStatus 设备状态。
type DeviceStatus string

const (
	DeviceStatusUp       DeviceStatus = "up"       // 在线
	DeviceStatusDown     DeviceStatus = "down"     // 离线
	DeviceStatusUnknown  DeviceStatus = "unknown"  // 未知
	DeviceStatusMaintain DeviceStatus = "maintain" // 维护中
)

// Device 网络设备模型。
type Device struct {
	ID            string       `json:"id"`
	TenantID      string       `json:"tenantID"`
	Name          string       `json:"name"`
	Type          DeviceType   `json:"type"`
	Vendor        string       `json:"vendor"`        // 厂商：cisco/huawei/juniper/...
	Model         string       `json:"model"`         // 型号
	IP            string       `json:"ip"`            // 管理 IP
	Mask          string       `json:"mask"`          // 子网掩码
	Mac           string       `json:"mac"`           // MAC 地址
	Location      string       `json:"location"`      // 物理位置
	SnmpCommunity string       `json:"snmpCommunity"` // SNMP community（脱敏返回）
	Status        DeviceStatus `json:"status"`
	Interfaces    []Interface  `json:"interfaces"`
	Config        string       `json:"config,omitempty"` // 当前配置（下发/备份）
	CreatedAt     time.Time    `json:"createdAt"`
	UpdatedAt     time.Time    `json:"updatedAt"`
}

// Interface 网络接口。
type Interface struct {
	Name   string `json:"name"` // 如 eth0, GigabitEthernet0/0/1
	IP     string `json:"ip"`
	Mask   string `json:"mask"`
	Mac    string `json:"mac"`
	Speed  int    `json:"speed"`  // Mbps
	Status string `json:"status"` // up/down
	Type   string `json:"type"`   // physical/vlan/loopback
	VlanID int    `json:"vlanID"` // VLAN ID（type=vlan 时有效）
}

// Metrics 网络设备监控指标。
type Metrics struct {
	DeviceID    string            `json:"deviceID"`
	Timestamp   time.Time         `json:"timestamp"`
	CPUUsage    float64           `json:"cpuUsage"`    // %
	MemoryUsage float64           `json:"memoryUsage"` // %
	Temperature float64           `json:"temperature"` // ℃
	Uptime      int64             `json:"uptime"`      // 秒
	Interfaces  []InterfaceMetric `json:"interfaces"`
}

// InterfaceMetric 接口监控指标。
type InterfaceMetric struct {
	Name        string  `json:"name"`
	InBytes     int64   `json:"inBytes"`
	OutBytes    int64   `json:"outBytes"`
	InPackets   int64   `json:"inPackets"`
	OutPackets  int64   `json:"outPackets"`
	InErrors    int64   `json:"inErrors"`
	OutErrors   int64   `json:"outErrors"`
	Bandwidth   float64 `json:"bandwidth"`   // Mbps
	Utilization float64 `json:"utilization"` // %
}

// Topology 网络拓扑结构。
type Topology struct {
	TenantID  string    `json:"tenantID"`
	Nodes     []Node    `json:"nodes"`
	Links     []Link    `json:"links"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Node 拓扑节点。
type Node struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"` // device/segment/cloud
	IP     string `json:"ip"`
	Status string `json:"status"`
}

// Link 拓扑链路。
type Link struct {
	From      string `json:"from"` // 节点 ID
	To        string `json:"to"`   // 节点 ID
	FromIface string `json:"fromIface"`
	ToIface   string `json:"toIface"`
	Bandwidth int    `json:"bandwidth"` // Mbps
	Latency   int    `json:"latency"`   // ms
	Status    string `json:"status"`    // up/down
}

// DiscoverRequest 网络发现请求。
type DiscoverRequest struct {
	Subnet string `json:"subnet"` // CIDR，如 192.168.1.0/24
}

// DiscoverResult 网络发现结果。
type DiscoverResult struct {
	Subnet  string   `json:"subnet"`
	Devices []Device `json:"devices"`
	Scanned int      `json:"scanned"`
	Found   int      `json:"found"`
	Error   string   `json:"error,omitempty"`
}

// ConfigRequest 网络配置下发请求。
type ConfigRequest struct {
	Config string `json:"config"` // 配置文本（CLI/YAML）
}

// ============================================================================
// 网络发现引擎
// ============================================================================

// Engine 网络管理引擎（MVP 桩实现，无外部依赖）。
type Engine struct{}

// NewEngine 构造网络管理引擎。
func NewEngine() *Engine {
	return &Engine{}
}

// Discover 基于子网扫描发现网络设备。
//
// 实现：TCP Connect 扫描（socket 连接常用端口），替换 MVP 桩实现。
// 扫描端口：22(SSH), 80(HTTP), 443(HTTPS), 3306(MySQL), 6379(Redis), 8080(HTTP-Alt), 9090(gRPC)。
// 限制：单次扫描 ≤ 254 地址，超时 500ms/地址。
func (e *Engine) Discover(req DiscoverRequest) DiscoverResult {
	if req.Subnet == "" {
		return DiscoverResult{Error: "subnet is required"}
	}
	ip, ipnet, err := net.ParseCIDR(req.Subnet)
	if err != nil {
		return DiscoverResult{Subnet: req.Subnet, Error: "invalid CIDR: " + err.Error()}
	}
	_ = ip
	// 统计子网内 IP 数（最多 254，避免 /8 之类超大子网爆算）。
	ones, bits := ipnet.Mask.Size()
	if bits == 0 {
		return DiscoverResult{Subnet: req.Subnet, Error: "invalid mask"}
	}
	hostBits := bits - ones
	if hostBits > 8 {
		hostBits = 8
	}

	// 常用端口扫描列表。
	scanPorts := []int{22, 80, 443, 3306, 6379, 8080, 9090}
	timeout := 500 * time.Millisecond

	var devices []Device
	scanned := 0

	// 遍历子网内所有地址（跳过网络地址和广播地址）。
	for ip := ipnet.IP.Mask(ipnet.Mask); ipnet.Contains(ip); incIP(ip) {
		ipStr := ip.String()
		// 跳过网络地址（最后一个字节为 0）。
		if ip[3] == 0 {
			continue
		}
		// 限制扫描数量。
		if scanned >= subnetMaxHosts {
			break
		}
		scanned++

		// 并行扫描常用端口。
		openPorts := scanPortsForHost(ipStr, scanPorts, timeout)
		if len(openPorts) == 0 {
			continue
		}

		// 有端口开放 → 发现设备。
		device := Device{
			ID:       "disc-" + sanitizeCIDR(ipStr),
			Name:     inferDeviceName(ipStr, openPorts),
			Type:     inferDeviceType(openPorts),
			Vendor:   "unknown",
			IP:       ipStr,
			Status:   DeviceStatusUp,
			Location: "discovered",
		}
		devices = append(devices, device)
	}

	return DiscoverResult{
		Subnet:  req.Subnet,
		Devices: devices,
		Scanned: scanned,
		Found:   len(devices),
	}
}

// scanPortsForHost 扫描指定主机的常用端口，返回开放端口列表。
func scanPortsForHost(host string, ports []int, timeout time.Duration) []int {
	var openPorts []int
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, port := range ports {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			addr := fmt.Sprintf("%s:%d", host, p)
			conn, err := net.DialTimeout("tcp", addr, timeout)
			if err != nil {
				return
			}
			_ = conn.Close()
			mu.Lock()
			openPorts = append(openPorts, p)
			mu.Unlock()
		}(port)
	}
	wg.Wait()
	return openPorts
}

// inferDeviceName 根据开放端口推断设备名称。
func inferDeviceName(ip string, ports []int) string {
	hasSSH := false
	hasHTTP := false
	hasMySQL := false
	hasRedis := false
	for _, p := range ports {
		switch p {
		case 22:
			hasSSH = true
		case 80, 8080, 443:
			hasHTTP = true
		case 3306:
			hasMySQL = true
		case 6379:
			hasRedis = true
		}
	}
	switch {
	case hasMySQL:
		return "mysql-server-" + ip
	case hasRedis:
		return "redis-server-" + ip
	case hasSSH && hasHTTP:
		return "web-server-" + ip
	case hasSSH:
		return "linux-host-" + ip
	case hasHTTP:
		return "http-device-" + ip
	default:
		return "device-" + ip
	}
}

// inferDeviceType 根据开放端口推断设备类型。
func inferDeviceType(ports []int) DeviceType {
	for _, p := range ports {
		switch p {
		case 3306, 6379:
			return DeviceTypeSwitch
		}
	}
	return DeviceTypeRouter
}

// incIP 将 IP 地址加 1（用于遍历子网）。
func incIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

// BuildTopology 从设备列表构建拓扑（MVP：设备作为节点，无链路）。
func (e *Engine) BuildTopology(tenantID string, devices []Device) Topology {
	nodes := make([]Node, 0, len(devices))
	for _, d := range devices {
		nodes = append(nodes, Node{
			ID:     d.ID,
			Name:   d.Name,
			Type:   string(d.Type),
			IP:     d.IP,
			Status: string(d.Status),
		})
	}
	return Topology{
		TenantID:  tenantID,
		Nodes:     nodes,
		Links:     []Link{},
		UpdatedAt: time.Now(),
	}
}

// sanitizeCIDR 把 CIDR 字符串转为安全的 ID 片段（替换 / 和 .）。
func sanitizeCIDR(cidr string) string {
	r := strings.NewReplacer("/", "-", ".", "-")
	return r.Replace(cidr)
}

// ValidateConfig 校验网络配置文本（MVP：非空 + 长度限制 64KB）。
func ValidateConfig(cfg string) error {
	if cfg == "" {
		return fmt.Errorf("config is empty")
	}
	if len(cfg) > 64*1024 {
		return fmt.Errorf("config too large: %d bytes (max 64KB)", len(cfg))
	}
	return nil
}
