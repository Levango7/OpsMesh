// Package provision 提供 自动纳管推送能力：通过 SSH 在候选设备上自动安装 OpsMesh agent。
package provision

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// knownHostsCallback 从 known_hosts 文件读取主机公钥，返回一个 HostKeyCallback。
// 只支持非哈希的 known_hosts 格式（"hostname key-type base64-key"），不支持 |1|hash 格式。
// 文件不存在或格式错误时返回错误。
func knownHostsCallback(filePath string) (ssh.HostKeyCallback, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("provision: 读取 KnownHosts %s 失败: %w", filePath, err)
	}
	// 解析 known_hosts：每行提取 [hostnames...] key-type base64-key
	type entry struct {
		hosts []string
		key   ssh.PublicKey
	}
	var entries []entry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// 跳过哈希格式（|1|...）
		if strings.HasPrefix(line, "|") {
			continue
		}
		// 分离主机名部分（第一个空格前）和公钥部分（之后）
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		hostPart := parts[0]
		keyPart := parts[1]
		// 解析公钥
		pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(keyPart))
		if err != nil {
			continue
		}
		hosts := strings.Split(hostPart, ",")
		entries = append(entries, entry{hosts: hosts, key: pub})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("provision: KnownHosts %s 无有效条目", filePath)
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		for _, e := range entries {
			for _, h := range e.hosts {
				// 支持通配符匹配：*.example.com 匹配 sub.example.com
				if strings.TrimPrefix(h, "*.") != h { // 通配符格式 *.domain
					// 提取 remote 的实际域名部分
					actualHost := hostname
					if idx := strings.LastIndex(actualHost, "."); idx != -1 {
						suffix := h[1:] // 去掉 *
						if strings.HasSuffix(actualHost, suffix) {
							if bytes.Equal(e.key.Marshal(), key.Marshal()) {
								return nil
							}
						}
					}
				} else if h == hostname || h == net.JoinHostPort(hostname, "22") || h == remote.String() {
					if bytes.Equal(e.key.Marshal(), key.Marshal()) {
						return nil
					}
				}
			}
		}
		return fmt.Errorf("provision: 主机 %s 不在 KnownHosts 中（host key mismatch）", hostname)
	}, nil
}

// PushAndExec 通过 SSH 连接到目标主机，执行 bootstrap 命令安装 agent。
// addr: host:port 格式；user: SSH 登录用户；keyPath: SSH 私钥路径；
// keyPass: 私钥密码（可选）；knownHostsPath: KnownHosts 文件路径（空=Insecure 回退）；
// cmd: 远程 shell 命令（bootstrap）。
// 返回远程 stdout+stderr 合并输出。10 分钟超时（传输 + agent 启动窗口）。
func PushAndExec(ctx context.Context, addr, user, keyPath, keyPass, knownHostsPath, cmd string) (string, error) {
	if user == "" {
		user = "root"
	}
	// 安全（F16 / M12）：KnownHosts 主机指纹校验——等保生产必须配置 --provision-ssh-known-hosts。
	// 未配置时回退 InsecureIgnoreHostKey 并打印警告（仅开发/内网调试）。
	// 生产环境（--production=true）必须在 autoProvisionLoop 调用处拒绝 SSH 推送（见 auto.go），
	// 此处保留 Insecure 回退仅为向后兼容非 production 调用方，绝不应用于生产。
	// MITM 风险说明：无主机指纹校验时，攻击者可劫持 SSH 连接注入恶意 agent 二进制（供应链 RCE）。
	hostKeyCallback := ssh.InsecureIgnoreHostKey()
	if knownHostsPath != "" {
		cb, err := knownHostsCallback(knownHostsPath)
		if err != nil {
			return "", err
		}
		hostKeyCallback = cb
	} else {
		// M12: 未配置 known_hosts → MITM 风险，打印显眼警告（生产应由 auto.go 提前拦截）。
		fmt.Fprintln(os.Stderr, "[provision] 警告：SSH 未配置 known_hosts（--provision-ssh-known-hosts 为空），回退 InsecureIgnoreHostKey，存在中间人攻击风险；生产环境必须配置 known_hosts")
	}
	config := &ssh.ClientConfig{
		User:            user,
		HostKeyCallback: hostKeyCallback,
		Timeout:         15 * time.Second,
	}

	// 读取 SSH 私钥
	if keyPath != "" {
		keyBytes, err := os.ReadFile(keyPath)
		if err != nil {
			return "", fmt.Errorf("provision: 读取 SSH 私钥 %s 失败: %w", keyPath, err)
		}
		var signer ssh.Signer
		if keyPass != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(keyBytes, []byte(keyPass))
		} else {
			signer, err = ssh.ParsePrivateKey(keyBytes)
		}
		if err != nil {
			return "", fmt.Errorf("provision: 解析 SSH 私钥失败: %w", err)
		}
		config.Auth = []ssh.AuthMethod{ssh.PublicKeys(signer)}
	} else {
		return "", fmt.Errorf("provision: 未配置 SSH 私钥（--provision-ssh-key）")
	}

	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return "", fmt.Errorf("provision: SSH 连接 %s 失败: %w", addr, err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("provision: SSH 会话创建失败: %w", err)
	}
	defer session.Close()

	// 在远程执行 bootstrap 命令（curl install.sh | sh 或直接启动 agent binary）
	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	// 设置执行超时（与外部 ctx 联动 + 硬超时 10 分钟防止悬挂连接）
	type result struct {
		err error
	}
	resCh := make(chan result, 1)
	go func() {
		err := session.Run(cmd)
		resCh <- result{err: err}
	}()

	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGTERM)
		_ = client.Close()
		return stdout.String() + stderr.String(), ctx.Err()
	case r := <-resCh:
		out := stdout.String() + stderr.String()
		if r.err != nil {
			return out, fmt.Errorf("provision: 远程执行失败: %w (stdout+stderr: %s)", r.err, out)
		}
		return out, nil
	case <-time.After(10 * time.Minute):
		_ = session.Signal(ssh.SIGTERM)
		_ = client.Close()
		return stdout.String() + stderr.String(), fmt.Errorf("provision: 远程执行超时（10min）")
	}
}
