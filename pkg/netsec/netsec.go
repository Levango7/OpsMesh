// Package netsec 提供扩展网络安全运维的纯逻辑层（无 HTTP/store 依赖）。
//
// TD-60 device 域下沉：netsec 六个能力（IPS/IDS、SIEM、蜜罐、流量整形、VPN、零信任）
// 原本内联在 controlplane/netsec_ext_ops.go 的 HTTP handler 中，脚本构建逻辑与
// HTTP 分发耦合。本包把「动作 → 脚本」的映射抽成纯函数，使 controlplane 与
// device-svc 两侧可共用同一份权威脚本，避免双份实现漂移。
//
// 约定：调用方负责解析请求体与下发任务；本包只回答「这个动作对应什么脚本」。
package netsec

// ActionRequest 是所有 netsec 端点共用的请求形态。
type ActionRequest struct {
	DeviceID string `json:"deviceID"`
	Action   string `json:"action"`
}

// Capability 标识一个 netsec 子能力（用于任务类型前缀）。
type Capability string

const (
	CapIPSIDS         Capability = "ips_ids"
	CapSIEM           Capability = "siem"
	CapHoneypot       Capability = "honeypot"
	CapTrafficShaping Capability = "traffic_shaping"
	CapVPN            Capability = "vpn"
	CapZeroTrust      Capability = "zerotrust"
)

// Build 返回指定能力与动作对应的 shell 脚本。
// ok=false 表示动作不受支持（调用方应返回 400 unknown action）。
func Build(cap Capability, action string) (script string, ok bool) {
	switch cap {
	case CapIPSIDS:
		return ipsIDS(action)
	case CapSIEM:
		return siem(action)
	case CapHoneypot:
		return honeypot(action)
	case CapTrafficShaping:
		return trafficShaping(action)
	case CapVPN:
		return vpn(action)
	case CapZeroTrust:
		return zeroTrust(action)
	}
	return "", false
}

// TaskAction 返回下发任务时使用的类型标签（与既有 controlplane 行为逐字一致）。
func TaskAction(cap Capability, action string) string {
	return string(cap) + "_" + action
}

// SupportedActions 返回指定能力支持的动作集合（供文档/前端枚举使用）。
func SupportedActions(cap Capability) []string {
	switch cap {
	case CapIPSIDS:
		return []string{"status", "deploy", "enable", "disable", "rules", "update"}
	case CapSIEM:
		return []string{"status", "deploy", "enable", "disable", "rules"}
	case CapHoneypot:
		return []string{"status", "deploy", "enable", "disable", "list"}
	case CapTrafficShaping:
		return []string{"status", "limit", "shape", "priority", "qos"}
	case CapVPN:
		return []string{"status", "deploy", "enable", "disable", "config"}
	case CapZeroTrust:
		return []string{"status", "deploy", "enable", "disable", "policy"}
	}
	return nil
}

func ipsIDS(action string) (string, bool) {
	switch action {
	case "status":
		return `echo "=== IPS/IDS Status ===" && (suricata --version 2>/dev/null || echo "suricata not available") && (snort -V 2>/dev/null || echo "snort not available") && (zeek --version 2>/dev/null || echo "zeek not available")`, true
	case "deploy":
		return `# deploy: apt install suricata/snort/zeek
apt-get install -y suricata snort zeek 2>/dev/null || apt install -y suricata snort zeek`, true
	case "enable":
		return `systemctl start suricata && echo "suricata enabled"`, true
	case "disable":
		return `systemctl stop suricata && echo "suricata disabled"`, true
	case "rules":
		return `suricata-update && echo "rules updated"`, true
	case "update":
		return `suricata-update && echo "updated"`, true
	}
	return "", false
}

func siem(action string) (string, bool) {
	switch action {
	case "status":
		return `echo "=== SIEM Status ===" && (wazuh-agent status 2>/dev/null || echo "wazuh-agent not available") && (ossec status 2>/dev/null || echo "ossec not available")`, true
	case "deploy":
		return `# deploy: apt install wazuh-agent
apt-get install -y wazuh-agent 2>/dev/null || apt install -y wazuh-agent`, true
	case "enable":
		return `systemctl start wazuh-agent && echo "wazuh-agent enabled"`, true
	case "disable":
		return `systemctl stop wazuh-agent && echo "wazuh-agent disabled"`, true
	case "rules":
		return `ossec-control enable && echo "ossec rules enabled"`, true
	}
	return "", false
}

func honeypot(action string) (string, bool) {
	switch action {
	case "status":
		return `echo "=== Honeypot Status ===" && (cowrie status 2>/dev/null || echo "cowrie not available") && (honeypot status 2>/dev/null || echo "honeypot not available")`, true
	case "deploy":
		return `# deploy: apt install cowrie
apt-get install -y cowrie 2>/dev/null || apt install -y cowrie`, true
	case "enable":
		return `systemctl start cowrie && echo "cowrie enabled"`, true
	case "disable":
		return `systemctl stop cowrie && echo "cowrie disabled"`, true
	case "list":
		return `ls /opt/cowrie/var/log/ 2>/dev/null || echo "cowrie log dir not found"`, true
	}
	return "", false
}

func trafficShaping(action string) (string, bool) {
	switch action {
	case "status":
		return `echo "=== Traffic Shaping Status ===" && (tc qdisc show 2>/dev/null || echo "tc not available") && (iptables -L 2>/dev/null || echo "iptables not available")`, true
	case "limit":
		return `# limit: tc qdisc add ... tbf
tc qdisc add dev eth0 root tbf rate 1mbit burst 32kbit latency 400ms && echo "tbf limit applied"`, true
	case "shape":
		return `# shape: tc qdisc add ... htb
tc qdisc add dev eth0 root htb default 12 && echo "htb shaping applied"`, true
	case "priority":
		return `# priority: tc filter add ... prio
tc filter add dev eth0 protocol ip parent 1:0 prio 1 u32 match ip dport 80 0xffff flowid 1:10 && echo "prio filter applied"`, true
	case "qos":
		return `# qos: tc qdisc add ... cake
tc qdisc add dev eth0 root cake bandwidth 10mbit && echo "cake qos applied"`, true
	}
	return "", false
}

func vpn(action string) (string, bool) {
	switch action {
	case "status":
		return `echo "=== VPN Status ===" && (wg show 2>/dev/null || echo "wireguard not available") && (openvpn --version 2>/dev/null | head -3 || echo "openvpn not available") && (ipsec status 2>/dev/null || echo "ipsec not available")`, true
	case "deploy":
		return `# deploy: apt install wireguard/openvpn/strongswan
apt-get install -y wireguard openvpn strongswan 2>/dev/null || apt install -y wireguard openvpn strongswan`, true
	case "enable":
		return `systemctl start wg-quick@wg0 && echo "wg-quick@wg0 enabled"`, true
	case "disable":
		return `systemctl stop wg-quick@wg0 && echo "wg-quick@wg0 disabled"`, true
	case "config":
		return `wg showconf wg0 2>/dev/null || echo "wg0 not found"`, true
	}
	return "", false
}

func zeroTrust(action string) (string, bool) {
	switch action {
	case "status":
		return `echo "=== Zero Trust Status ===" && (spire-agent status 2>/dev/null || echo "spire-agent not available") && (step-ca status 2>/dev/null || echo "step-ca not available")`, true
	case "deploy":
		return `# deploy: apt install spire-agent
apt-get install -y spire-agent 2>/dev/null || apt install -y spire-agent`, true
	case "enable":
		return `systemctl start spire-agent && echo "spire-agent enabled"`, true
	case "disable":
		return `systemctl stop spire-agent && echo "spire-agent disabled"`, true
	case "policy":
		return `spire-agent api fetch jwt 2>/dev/null || echo "spire-agent api not available"`, true
	}
	return "", false
}
