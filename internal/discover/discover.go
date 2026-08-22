// Package discover 提供网段存活扫描（真实纳管）。
//
// 不依赖任何外部包：用标准库 net 对 segment CIDR 做并发受限的 TCP-connect 探测，
// 返回存活主机 IP。ICMP 需原始套接字（特权），故默认用 TCP-connect 探测（非特权、可控）。
//
// 这是产品核心差异点“服务部署后整段网络打通、设备自动纳管”的真实兑现路径；
// MVP 默认关闭（--discover=false），此时采用“agent 即设备”的降级纳管（见 store.Register）。
package discover

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"
)

// MaxHosts 单次扫描的主机数上限，避免 /16 等大网段耗尽资源（MVP 期望 /24）。
const MaxHosts = 1024

// Sweep 对 cidr（如 10.30.0.0/24）做存活扫描：对每台主机的 ports 尝试 TCP 连接，
// 任一端口连通即视为存活。concurrency 控制并发度，timeout 控制单连接超时。
// 返回存活 IP 列表（去重，按字符串升序）。仅支持 IPv4。
func Sweep(ctx context.Context, cidr string, ports []int, concurrency int, timeout time.Duration) ([]string, error) {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("parse cidr %q: %w", cidr, err)
	}
	hosts, err := enumerateIPv4(ipnet)
	if err != nil {
		return nil, err
	}
	if len(ports) == 0 {
		ports = []int{22, 80, 443, 9090}
	}
	if concurrency <= 0 {
		concurrency = 64
	}
	if timeout <= 0 {
		timeout = 800 * time.Millisecond
	}

	sem := make(chan struct{}, concurrency)
	var mu sync.Mutex
	seen := make(map[string]struct{})
	var wg sync.WaitGroup
	for _, ip := range hosts {
		select {
		case <-ctx.Done():
			wg.Wait()
			return nil, ctx.Err()
		case sem <- struct{}{}:
		}
		wg.Add(1)
		ipStr := ip.String()
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if alive(ipStr, ports, timeout) {
				mu.Lock()
				seen[ipStr] = struct{}{}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	out := make([]string, 0, len(seen))
	for ip := range seen {
		out = append(out, ip)
	}
	sort.Strings(out)
	return out, nil
}

// alive 对单 IP 的 ports 做 TCP 连接，任一成功即存活。
func alive(ip string, ports []int, timeout time.Duration) bool {
	for _, p := range ports {
		addr := net.JoinHostPort(ip, fmt.Sprintf("%d", p))
		conn, err := net.DialTimeout("tcp", addr, timeout)
		if err == nil {
			_ = conn.Close()
			return true
		}
	}
	return false
}

// enumerateIPv4 返回 CIDR 内（排除网络/广播地址，/31、/32 例外）的主机 IP。
func enumerateIPv4(ipnet *net.IPNet) ([]net.IP, error) {
	ipv4 := ipnet.IP.To4()
	if ipv4 == nil {
		return nil, fmt.Errorf("仅支持 IPv4 网段，收到: %s", ipnet.IP)
	}
	ones, bits := ipnet.Mask.Size()
	if bits != 32 {
		return nil, fmt.Errorf("非 IPv4 /32 掩码: %s", ipnet.Mask)
	}
	base := binary.BigEndian.Uint32(ipv4)
	maskU := binary.BigEndian.Uint32(net.CIDRMask(ones, 32))
	start := base & maskU
	end := start | ^maskU

	var out []net.IP
	skipNB := ones <= 30 // /31、/32 不跳过网络/广播地址
	count := 0
	for h := start; h <= end && count < MaxHosts; h++ {
		if skipNB && (h == start || h == end) {
			count++
			continue
		}
		b := make(net.IP, 4)
		binary.BigEndian.PutUint32(b, h)
		out = append(out, b)
		count++
	}
	return out, nil
}
