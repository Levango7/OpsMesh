// Package ssh 提供 SSH 客户端池与命令执行能力，用于 K8s 集群自动化部署。
package ssh

import (
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	cryptossh "golang.org/x/crypto/ssh"
)

// Pool SSH 连接池，缓存复用 TCP+SSH 连接。
type Pool struct {
	conns map[string]*cryptossh.Client
	mu    sync.RWMutex
}

func NewPool() *Pool {
	return &Pool{conns: make(map[string]*cryptossh.Client)}
}

// Get 获取或建立到 host:port 的 SSH 连接。
func (p *Pool) Get(host string, port int, user, keyPath, password string) (*cryptossh.Client, error) {
	addr := fmt.Sprintf("%s:%d", host, port)
	p.mu.RLock()
	if c, ok := p.conns[addr]; ok {
		p.mu.RUnlock()
		return c, nil
	}
	p.mu.RUnlock()

	client, err := dial(host, port, user, keyPath, password)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	p.conns[addr] = client
	p.mu.Unlock()
	return client, nil
}

// Close 关闭所有连接。
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.conns {
		c.Close()
	}
	p.conns = make(map[string]*cryptossh.Client)
}

func dial(host string, port int, user, keyPath, password string) (*cryptossh.Client, error) {
	var auth []cryptossh.AuthMethod
	if keyPath != "" {
		key, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("read ssh key: %w", err)
		}
		signer, err := cryptossh.ParsePrivateKey(key)
		if err != nil {
			return nil, fmt.Errorf("parse ssh key: %w", err)
		}
		auth = append(auth, cryptossh.PublicKeys(signer))
	}
	if password != "" {
		auth = append(auth, cryptossh.Password(password))
	}
	if len(auth) == 0 {
		return nil, fmt.Errorf("no auth method provided")
	}

	config := &cryptossh.ClientConfig{
		User: user,
		Auth: auth,
		// #nosec G106 -- 内网运维自动化：连接平台自管/托管的 K8s 节点，主机密钥未预注册，
		// 改为固定 hostkey 校验会断开全部既有连接（语义变更），此处显式豁免待后续引入 known_hosts。
		HostKeyCallback: cryptossh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	client, err := cryptossh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	return client, nil
}

// Exec 在远程主机执行命令，返回 stdout/stderr/exitCode。
func Exec(client *cryptossh.Client, cmd string) (string, string, int, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", "", -1, fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	var stdout, stderr []byte
	session.Stdout = &bytesBuffer{b: &stdout}
	session.Stderr = &bytesBuffer{b: &stderr}

	err = session.Run(cmd)
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*cryptossh.ExitError); ok {
			exitCode = exitErr.ExitStatus()
		} else {
			return string(stdout), string(stderr), -1, fmt.Errorf("run cmd: %w", err)
		}
	}
	return string(stdout), string(stderr), exitCode, nil
}

type bytesBuffer struct {
	b *[]byte
}

func (bb *bytesBuffer) Write(p []byte) (int, error) {
	*bb.b = append(*bb.b, p...)
	return len(p), nil
}

// WriteFile 通过 SFTP 或 cat 写入远程文件。
func WriteFile(client *cryptossh.Client, content []byte, remotePath string, mode os.FileMode) error {
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	cmd := fmt.Sprintf("cat > %s && chmod %o %s", remotePath, mode, remotePath)
	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	go func() {
		defer stdin.Close()
		stdin.Write(content)
	}()
	if err := session.Run(cmd); err != nil {
		return fmt.Errorf("write file %s: %w", remotePath, err)
	}
	return nil
}

// MkdirAll 远程创建目录。
func MkdirAll(client *cryptossh.Client, path string) error {
	_, _, code, err := Exec(client, fmt.Sprintf("mkdir -p %s", path))
	if err != nil || code != 0 {
		return fmt.Errorf("mkdir %s: %w (code %d)", path, err, code)
	}
	return nil
}

// FileExists 检查远程文件是否存在。
// 会话执行异常（非命令退出码）时视为不存在（Exec 对此类错误返回 code=-1，行为与原实现一致）。
func FileExists(client *cryptossh.Client, path string) bool {
	_, _, code, err := Exec(client, fmt.Sprintf("test -f %s", path))
	if err != nil {
		return false
	}
	return code == 0
}

// DirExists 检查远程目录是否存在。
// 会话执行异常（非命令退出码）时视为不存在（Exec 对此类错误返回 code=-1，行为与原实现一致）。
func DirExists(client *cryptossh.Client, path string) bool {
	_, _, code, err := Exec(client, fmt.Sprintf("test -d %s", path))
	if err != nil {
		return false
	}
	return code == 0
}

// ReadFile 读取远程文件内容。
func ReadFile(client *cryptossh.Client, path string) (string, error) {
	out, _, code, err := Exec(client, fmt.Sprintf("cat %s", path))
	if err != nil || code != 0 {
		return "", fmt.Errorf("read file %s: %w (code %d)", path, err, code)
	}
	return out, nil
}

// SyncFile 通过 scp 同步文件（使用 ssh + cat）。
func SyncFile(client *cryptossh.Client, localPath, remotePath string) error {
	content, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("read local file: %w", err)
	}
	mode := os.FileMode(0644)
	if info, statErr := os.Stat(localPath); statErr == nil {
		mode = info.Mode()
	}
	return WriteFile(client, content, remotePath, mode)
}

// CopyDir 远程复制目录（rsync）。
func CopyDir(client *cryptossh.Client, src, dst string) error {
	_, _, code, err := Exec(client, fmt.Sprintf("rsync -a %s %s", src, dst))
	if err != nil || code != 0 {
		return fmt.Errorf("rsync %s -> %s: %w (code %d)", src, dst, err, code)
	}
	return nil
}

// DialTCP 测试 TCP 连通性。
func DialTCP(host string, port int, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), timeout)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// ParseSSHKey 从字符串解析私钥。
func ParseSSHKey(keyContent string) (cryptossh.Signer, error) {
	return cryptossh.ParsePrivateKey([]byte(keyContent))
}

// EnsureDir 确保远程目录存在。
func EnsureDir(client *cryptossh.Client, path string) error {
	return MkdirAll(client, path)
}

// EnsureFile 确保远程文件存在（不存在则创建空文件）。
func EnsureFile(client *cryptossh.Client, path string) error {
	if FileExists(client, path) {
		return nil
	}
	return WriteFile(client, []byte{}, path, 0644)
}

// AppendFile 追加内容到远程文件。
func AppendFile(client *cryptossh.Client, content []byte, remotePath string) error {
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	cmd := fmt.Sprintf("cat >> %s", remotePath)
	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	go func() {
		defer stdin.Close()
		stdin.Write(content)
	}()
	return session.Run(cmd)
}

// RemoveFile 删除远程文件。
func RemoveFile(client *cryptossh.Client, path string) error {
	_, _, code, err := Exec(client, fmt.Sprintf("rm -f %s", path))
	if err != nil || code != 0 {
		return fmt.Errorf("rm %s: %w (code %d)", path, err, code)
	}
	return nil
}

// RemoveDir 删除远程目录。
func RemoveDir(client *cryptossh.Client, path string) error {
	_, _, code, err := Exec(client, fmt.Sprintf("rm -rf %s", path))
	if err != nil || code != 0 {
		return fmt.Errorf("rm -rf %s: %w (code %d)", path, err, code)
	}
	return nil
}

// Chmod 修改远程文件权限。
func Chmod(client *cryptossh.Client, path string, mode os.FileMode) error {
	_, _, code, err := Exec(client, fmt.Sprintf("chmod %o %s", mode, path))
	if err != nil || code != 0 {
		return fmt.Errorf("chmod %s: %w (code %d)", path, err, code)
	}
	return nil
}

// Chown 修改远程文件所有者。
func Chown(client *cryptossh.Client, path, owner string) error {
	_, _, code, err := Exec(client, fmt.Sprintf("chown %s %s", owner, path))
	if err != nil || code != 0 {
		return fmt.Errorf("chown %s: %w (code %d)", path, err, code)
	}
	return nil
}

// Systemctl 控制远程服务。
func Systemctl(client *cryptossh.Client, action, service string) error {
	_, _, code, err := Exec(client, fmt.Sprintf("systemctl %s %s", action, service))
	if err != nil || code != 0 {
		return fmt.Errorf("systemctl %s %s: %w (code %d)", action, service, err, code)
	}
	return nil
}

// WaitForPort 等待远程端口开放。
// 会话执行异常（非命令退出码）视为本次探测未就绪（Exec 对此类错误返回 code=-1，行为与原实现一致）。
func WaitForPort(client *cryptossh.Client, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, _, code, err := Exec(client, fmt.Sprintf("ss -tlnp | grep -q ':%d '", port))
		if err == nil && code == 0 {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("port %d not ready within %v", port, timeout)
}

// GetHostname 获取远程主机名。
func GetHostname(client *cryptossh.Client) (string, error) {
	out, _, code, err := Exec(client, "hostname")
	if err != nil || code != 0 {
		return "", fmt.Errorf("hostname: %w (code %d)", err, code)
	}
	return strings.TrimSpace(out), nil
}

// Client is an alias for golang.org/x/crypto/ssh.Client for external callers.
type Client = cryptossh.Client

func GetOSInfo(client *cryptossh.Client) (string, error) {
	out, _, code, err := Exec(client, "cat /etc/os-release | grep '^ID=' | cut -d= -f2")
	if err != nil || code != 0 {
		return "", fmt.Errorf("os info: %w (code %d)", err, code)
	}
	return strings.TrimSpace(out), nil
}
