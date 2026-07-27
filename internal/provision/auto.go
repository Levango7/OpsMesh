package provision

import (
	"context"
	"fmt"
	"sync"
	"time"

	"opsmesh/internal/config"
	"opsmesh/internal/discover"
	"opsmesh/internal/proto"
)

// Deps 是 AutoProvision 所需的控制面依赖（以函数注入，避免 provision 反向依赖 controlplane）。
type Deps struct {
	UpsertDevice func(d *proto.DeviceInfo)
	Provision    func(deviceID, host, tenantID string) (token, bootstrap string, err error)
}

// Summary 自动纳管编排结果汇总。
type Summary struct {
	Scanned    int      `json:"scanned"`
	Registered int      `json:"registered"`
	Provisioned int     `json:"provisioned"`
	SSHPushed  int      `json:"sshPushed"`
	Failures   []string `json:"failures,omitempty"`
}

// AutoProvision 执行 B1 自动纳管编排闭环：
//   for 每段 CIDR：discover.Sweep 存活扫描
//     → 存活主机登记为候选设备（State=discovered，Managed=false）
//     → 为设备签发一次性 install token（Provision）
//     → 若配置 SSH 私钥，通过 SSH 自动推送 bootstrap 完成 agent 安装
// tenantID 为空时视为单租户（开发模式）。
//
// 设计要点：
//   - 扫描与推送均受 ctx 控制；SSH 推送在独立 goroutine 中以 Background ctx 运行，
//     避免 HTTP 请求返回后 ctx 取消导致推送中断。
//   - 并发安全：汇总计数与失败列表均加锁。
func AutoProvision(ctx context.Context, deps Deps, cfg *config.Config, cidrs []string, tenantID string) (*Summary, error) {
	sum := &Summary{}
	if len(cidrs) == 0 {
		return nil, fmt.Errorf("provision: 无待扫描网段")
	}
	if deps.UpsertDevice == nil || deps.Provision == nil {
		return nil, fmt.Errorf("provision: 依赖未注入（UpsertDevice/Provision）")
	}
	var mu sync.Mutex
	for _, cidr := range cidrs {
		alive, err := discover.Sweep(ctx, cidr, []int{22, 9100}, 64, 800*time.Millisecond)
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
			deps.UpsertDevice(&proto.DeviceInfo{
				DeviceID: devID,
				Segment:  cidr,
				TenantID: tenantID,
				IP:       ip,
				State:    "discovered",
				Managed:  false,
			})
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
			advertise := cfg.AdvertiseAddr
			if advertise == "" {
				advertise = fmt.Sprintf("http://127.0.0.1:%d", cfg.HTTPPort)
			}
			bootstrap := fmt.Sprintf("curl -sSL %s/install.sh | sh -s -- --token=%s", advertise, token)
			sshAddr := fmt.Sprintf("%s:22", ip)
			go func(addr, cmd, dev string) {
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
	return sum, nil
}
