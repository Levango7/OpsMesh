
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
	"strconv"
	"strings"
	"time"
)

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
	ID           string       `json:"id"`
	TenantID     string       `json:"tenantID"`
	Name         string       `json:"name"`
	Type         DeviceType   `json:"type"`
	Vendor       string       `json:"vendor"`       // 厂商：cisco/huawei/juniper/...
	Model        string       `json:"model"`        // 型号
	IP           string       `json:"ip"`           // 管理 IP
	Mask         string       `json:"mask"`         // 子网掩码
	Mac          string       `json:"mac"`          // MAC 地址
	Location     string       `json:"location"`     // 物理位置
	SnmpCommunity string      `json:"snmpCommunity"` // SNMP community（脱敏返回）
	Status       DeviceStatus `json:"status"`
	Interfaces   []Interface  `json:"interfaces"`
	Config       string       `json:"config,omitempty"` // 当前配置（下发/备份）
	CreatedAt    time.Time    `json:"createdAt"`
	UpdatedAt    time.Time    `json:"updatedAt"`
}

// Interface 网络接口。
type Interface struct {
	Name     string `json:"name"`     // 如 eth0, GigabitEthernet0/0/1
	IP       string `json:"ip"`
	Mask     string `json:"mask"`
	Mac      string `json:"mac"`
	Speed    int    `json:"speed"`    // Mbps
	Status   string `json:"status"`   // up/down
	Type     string `json:"type"`     // physical/vlan/loopback
	VlanID   int    `json:"vlanID"`   // VLAN ID（type=vlan 时有效）
}

// Metrics 网络设备监控指标。
type Metrics struct {
	DeviceID    string    `json:"deviceID"`
	Timestamp   time.Time `json:"timestamp"`
	CPUUsage    float64   `json:"cpuUsage"`    // %
	MemoryUsage float64   `json:"memoryUsage"` // %
	Temperature float64   `json:"temperature"` // ℃
	Uptime      int64     `json:"uptime"`      // 秒
	Interfaces  []InterfaceMetric `json:"interfaces"`
}

// InterfaceMetric 接口监控指标。
type InterfaceMetric struct {
	Name       string  `json:"name"`
	InBytes    int64   `json:"inBytes"`
	OutBytes   int64   `json:"outBytes"`
	InPackets  int64   `json:"inPackets"`
	OutPackets int64   `json:"outPackets"`
	InErrors   int64   `json:"inErrors"`
	OutErrors  int64   `json:"outErrors"`
	Bandwidth  float64 `json:"bandwidth"` // Mbps
	Utilization float64 `json:"utilization"` // %
}

// Topology 网络拓扑结构。
type Topology struct {
	TenantID  string  `json:"tenantID"`
	Nodes     []Node  `json:"nodes"`
	Links     []Link  `json:"links"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Node 拓扑节点。
type Node struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`   // device/segment/cloud
	IP     string `json:"ip"`
	Status string `json:"status"`
}

// Link 拓扑链路。
type Link struct {
	From    string `json:"from"`    // 节点 ID
	To      string `json:"to"`      // 节点 ID
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
// MVP 实现：校验 CIDR 合法性，返回示例设备（不实际发起扫描）。
// 生产实现可接入 nmap/SNMP 扫描。
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
	scanned := 0
	if hostBits <= 8 {
		scanned = (1 << hostBits) - 2
	} else {
		scanned = 254
	}
	// 示例：返回 2 个示例设备（网关 + 一台主机）。
	base := strings.TrimSuffix(req.Subnet, "/"+strconv.Itoa(ones))
	gatewayIP := ipnet.IP
	if len(gatewayIP) >= 4 {
		gatewayIP = net.IP(append([]byte(nil), gatewayIP...))
		gatewayIP[3] = gatewayIP[3] + 1
	}
	devices := []Device{
		{
			ID:       "disc-gw-" + sanitizeCIDR(req.Subnet),
			Name:     "gateway-" + base,
			Type:     DeviceTypeRouter,
			Vendor:   "unknown",
			IP:       gatewayIP.String(),
			Status:   DeviceStatusUp,
			Location: "discovered",
		},
		{
			ID:       "disc-sw-" + sanitizeCIDR(req.Subnet),
			Name:     "switch-" + base,
			Type:     DeviceTypeSwitch,
			Vendor:   "unknown",
			IP:       base,
			Status:   DeviceStatusUnknown,
			Location: "discovered",
		},
	}
	return DiscoverResult{
		Subnet:  req.Subnet,
		Devices: devices,
		Scanned: scanned,
		Found:   len(devices),
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