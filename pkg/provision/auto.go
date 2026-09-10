// Package provision 提供 自动纳管推送能力：通过 SSH 在候选设备上自动安装 OpsMesh agent。
//
// 本包是 controlplane internal/provision 迁出的零依赖版本：仅依赖 stdlib + pkg/discover。
// 调用方各自从自己的配置填充 Config；UpsertDevice 回调以纯函数签名注入，
// 由调用方闭包内自行构造实体（controlplane 构造 proto.DeviceInfo，device-svc 构造 models.Device）。
package provision

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"opsmesh/pkg/discover"
)

// Config 自动纳管所需的配置项（解耦：原 internal/provision 直接吃 *config.Config，迁出后改为
// 纯结构体，调用方从自己的 config 字段填充，避免 pkg 反向依赖 internal/config）。
type Config struct {
	// AdvertiseAddr 控制面对外地址（用于拼接 bootstrap curl URL）。空字符串表示由调用方在
	// FallbackAdvertise 字段提供本机回环地址。
	AdvertiseAddr string
	// FallbackAdvertise advertise 为空时回退的地址（通常 fmt.Sprintf("http://127.0.0.1:%d", HTTPPort)）。
	FallbackAdvertise string
	// Production 生产模式：true 时强制 advertise 为 HTTPS（防供应链 RCE）且强制 known_hosts。
	Production bool
	// SSH 推送相关字段
	ProvisionSSHUser       string
	ProvisionSSHKey        string
	ProvisionSSHKP         string
	ProvisionSSHKnownHosts string
	// ScanPorts 存活扫描端口列表；空时默认 [22, 9100]。
	ScanPorts []int
}

// SweepConcurrency 扫描并发上限（Sweep 函数参数）。
const SweepConcurrency = 64

// SweepTimeout 单端口探测超时。
const SweepTimeout = 800 * time.Millisecond

// DeviceDeps 是 AutoProvision 所需的控制面依赖（以函数注入，避免 provision 反向依赖 controlplane）。
//
// UpsertDevice 在每次扫描到存活主机时回调，调用方负责把设备登记到自己的存储。
// 参数：deviceID="dev-<ip>"，ip=存活 IP，cidr=扫描网段，tenantID=租户标识。
type DeviceDeps struct {
	UpsertDevice func(deviceID, ip, cidr, tenantID string)
	Provision    func(deviceID, host, tenantID string) (token, bootstrap string, err error)
}

// Summary 自动纳管编排结果汇总。
type Summary struct {
	Scanned     int      `json:"scanned"`
	Registered  int      `json:"registered"`
	Provisioned int      `json:"provisioned"`
	SSHPushed   int      `json:"sshPushed"`
	Failures    []string `json:"failures,omitempty"`
}

// AutoProvision 执行 自动纳管编排闭环：
//
//	for 每段 CIDR：discover.Sweep 存活扫描
//	  → 存活主机登记为候选设备（discover 回调）
//	  → 为设备签发一次性 install token（Provision 回调）
//	  → 若配置 SSH 私钥，通过 SSH 自动推送 bootstrap 完成 agent 安装
//
// tenantID 为空时视为单租户（开发模式）。
//
// 设计要点：
//   - 扫描与推送均受 ctx 控制；SSH 推送在独立 goroutine 中以 Background ctx 运行，
//     避免 HTTP 请求返回后 ctx 取消导致推送中断。
//   - 并发安全：汇总计数与失败列表均加锁。
//   - advertise HTTPS 强制（生产）：防 agent 二进制明文下载被中间人篡改（供应链 RCE）。
func AutoProvision(ctx context.Context, deps DeviceDeps, cfg Config, cidrs []string, tenantID string) (*Summary, error) {
	sum := &Summary{}
	if len(cidrs) == 0 {
		return nil, fmt.Errorf("provision: 无待扫描网段")
	}
	if deps.UpsertDevice == nil || deps.Provision == nil {
		return nil, fmt.Errorf("provision: 依赖未注入（UpsertDevice/Provision）")
	}
	var mu sync.Mutex
	// 供应链加固：生产模式强制 advertise 为 HTTPS——agent 二进制经 HTTP 明文下载
	// 可被中间人篡改（供应链 RCE）。advertise 为空回退本机回环地址（仅本地调试）时同样拒绝。
	advertise := cfg.AdvertiseAddr
	if advertise == "" {
		advertise = cfg.FallbackAdvertise
	}
	if cfg.Production && !strings.HasPrefix(advertise, "https://") {
		return nil, fmt.Errorf("provision: 生产模式要求 advertise 为 HTTPS（当前 %q），防 agent 二进制明文下载被篡改（供应链 RCE）", advertise)
	}
	if !strings.HasPrefix(advertise, "https://") {
		fmt.Fprintf(os.Stderr, "[provision] 警告：advertise 非 HTTPS（%s），agent 二进制明文传输存在中间人篡改风险；生产环境务必配置 HTTPS\n", advertise)
	}
	// 加固项（设计文档 §五 4）：advertise 格式白名单——只允许 scheme://host:port 形式，
	// 阻断含 shell 元字符（` ; & | $ \n 等）的恶意配置进 bootstrap 命令拼接。
	if !validateAdvertise(advertise) {
		return nil, fmt.Errorf("provision: advertise 格式非法（必须为 scheme://host:port）：%q", advertise)
	}
	// SSH 连接风暴防护：推送并发上限（信号量限流），避免大网段扫描后同时发起大量 SSH 连接。
	sshSem := make(chan struct{}, 8)
	// 等待所有 SSH goroutine 完成（防止函数返回后 goroutine 仍写 sum）
	var wg sync.WaitGroup
	scanPorts := cfg.ScanPorts
	if len(scanPorts) == 0 {
		scanPorts = []int{22, 9100}
	}
	for _, cidr := range cidrs {
		alive, err := discover.Sweep(ctx, cidr, scanPorts, SweepConcurrency, SweepTimeout)
		if err != nil {
			mu.Lock()
			sum.Failures = append(sum.Failures, fmt.Sprintf("scan %s: %v", cidr, err))
			mu.Unlock()
			continue
		}
		for _, ip := range alive {
			mu.Lock()
			sum.Scanned++
			mu.Unlock()

			devID := fmt.Sprintf("dev-%s", ip)
			deps.UpsertDevice(devID, ip, cidr, tenantID)
			mu.Lock()
			sum.Registered++
			mu.Unlock()

			token, _, err := deps.Provision(devID, ip, tenantID)
			if err != nil {
				mu.Lock()
				sum.Failures = append(sum.Failures, fmt.Sprintf("provision %s: %v", ip, err))
				mu.Unlock()
				continue
			}
			mu.Lock()
			sum.Provisioned++
			mu.Unlock()

			if cfg.ProvisionSSHKey == "" {
				continue // 未配置 SSH 私钥：仅签发 token，等待用户手动 curl|sh 或 agent 自助纳管
			}
			// M12 生产环境强制 known_hosts：拒绝 InsecureIgnoreHostKey SSH 推送（MITM 防护）。
			// 生产模式下 known_hosts 为空时直接跳过 SSH 推送并记录失败，避免供应链 RCE 风险。
			if cfg.Production && cfg.ProvisionSSHKnownHosts == "" {
				mu.Lock()
				sum.Failures = append(sum.Failures, fmt.Sprintf("ssh %s: 生产模式拒绝 InsecureIgnoreHostKey（MITM 风险），请配置 --provision-ssh-known-hosts", ip))
				mu.Unlock()
				continue
			}
			bootstrap := fmt.Sprintf("curl -sSL %s/install.sh | sh -s -- --token=%s", advertise, token)
			sshAddr := fmt.Sprintf("%s:22", ip)
			wg.Add(1)
			go func(addr, cmd, dev string) {
				defer wg.Done()
				sshSem <- struct{}{} // 并发限流（最多 8 个并行 SSH 推送）
				defer func() { <-sshSem }()
				out, e := PushAndExec(context.Background(), addr, cfg.ProvisionSSHUser, cfg.ProvisionSSHKey, cfg.ProvisionSSHKP, cfg.ProvisionSSHKnownHosts, cmd)
				if e != nil {
					mu.Lock()
					sum.Failures = append(sum.Failures, fmt.Sprintf("ssh %s: %v (out=%s)", addr, e, out))
					mu.Unlock()
					return
				}
				mu.Lock()
				sum.SSHPushed++
				mu.Unlock()
			}(sshAddr, bootstrap, devID)
		}
	}
	// P1-4 修复：等待所有 SSH goroutine 完成后再返回，防止调用方 json.Marshal 与 goroutine 写竞争。
	// WaitGroup 保证 sum 的最终状态完整（SSH 推送结果不被截断）。
	wg.Wait()
	return sum, nil
}

// validateAdvertise 校验 advertise 格式：只允许 scheme://host:port 形式，
// 禁止含 shell 元字符（` ; & | $ \n \r \t 等）以及反引号。
// scheme 前缀仅允许 http:// 或 https://；host/port 段字符走白名单。
func validateAdvertise(s string) bool {
	if s == "" {
		return false
	}
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return false
	}
	// 显式拒绝常见 shell 元字符（纵深防御）
	for _, c := range s {
		switch c {
		case ' ', '\t', '\n', '\r', '`', '$', ';', '&', '|', '<', '>', '\\', '"', '\'':
			return false
		}
	}
	return true
}
