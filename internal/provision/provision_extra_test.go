package provision

// provision_extra_test.go 补齐 provision 包剩余低覆盖分支：
//   - known_hosts 解析容错分支（无 key 部分 / 公钥解析失败）
//   - known_hosts "host:22" 形式条目（JoinHostPort 与 remote.String() 匹配路径）
//   - 通配符条目 + key 不匹配组合
//   - PushAndExec 的 NewSession 失败分支（服务器拒绝一切 session channel）
//   - AutoProvision SSH 推送成功闭环（SSHPushed++，需本地 22 端口 mock SSH 服务）
//
// 全部使用本地 mock/接口注入，禁止真实外部网络连接；
// 端口 22 被占用时自动 Skip，保证测试在任何环境下可运行。

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"

	"opsmesh/internal/config"
)

// ============================================================
// knownHostsCallback 解析容错分支
// ============================================================

// TestKnownHostsCallback_LineWithoutKeyPart 验证 known_hosts 中仅有主机名、
// 缺少空格分隔公钥部分的畸形行被安全跳过（len(parts)!=2 → continue），
// 且不影响同文件中合法条目的解析。
func TestKnownHostsCallback_LineWithoutKeyPart(t *testing.T) {
	pub, err := generateTestKey()
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "known_hosts")
	content := "malformed-hostname-without-key\n" + keyLine("good-host", pub) + "\n"
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cb, err := knownHostsCallback(f)
	if err != nil {
		t.Fatalf("畸形行应被跳过而非导致整文件解析失败: %v", err)
	}

	// 合法条目仍然生效
	if err := cb("good-host", &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 22}, pub); err != nil {
		t.Fatalf("合法条目应通过校验: %v", err)
	}
	// 畸形行对应的主机不存在任何条目，应被拒绝
	if err := cb("malformed-hostname-without-key", &net.TCPAddr{IP: net.ParseIP("10.0.0.2"), Port: 22}, pub); err == nil {
		t.Fatal("仅含主机名无公钥的畸形行不应产生有效条目")
	}
}

// TestKnownHostsCallback_UnparseableKeySkipped 验证公钥部分无法被
// ssh.ParseAuthorizedKey 解析的行被跳过（err != nil → continue），
// 不影响同文件中合法条目。
func TestKnownHostsCallback_UnparseableKeySkipped(t *testing.T) {
	pub, err := generateTestKey()
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "known_hosts")
	// "@@not-a-valid-key@@" 不是合法 base64/key-type，ParseAuthorizedKey 必然失败
	content := "bad-host @@not-a-valid-key@@\n" + keyLine("good-host", pub) + "\n"
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cb, err := knownHostsCallback(f)
	if err != nil {
		t.Fatalf("无法解析的公钥行应被跳过而非导致整文件解析失败: %v", err)
	}

	if err := cb("good-host", &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 22}, pub); err != nil {
		t.Fatalf("合法条目应通过校验: %v", err)
	}
	if err := cb("bad-host", &net.TCPAddr{IP: net.ParseIP("10.0.0.3"), Port: 22}, pub); err == nil {
		t.Fatal("公钥解析失败的行不应产生有效条目")
	}
}

// TestKnownHostsCallback_HostPortFormEntry 验证 known_hosts 条目以
// "host:22" 形式书写时的两条匹配路径：
//   - h == net.JoinHostPort(hostname, "22")
//   - h == remote.String()
//
// 该形式常见于 ssh-keyscan 对非标准端口的输出。
func TestKnownHostsCallback_HostPortFormEntry(t *testing.T) {
	pub, err := generateTestKey()
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "known_hosts")
	line := "10.0.0.7:22 " + string(ssh.MarshalAuthorizedKey(pub))
	if err := os.WriteFile(f, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cb, err := knownHostsCallback(f)
	if err != nil {
		t.Fatalf("knownHostsCallback err = %v", err)
	}

	remote := &net.TCPAddr{IP: net.ParseIP("10.0.0.7"), Port: 22}
	// 命中 h == net.JoinHostPort(hostname, "22") 分支
	if err := cb("10.0.0.7", remote, pub); err != nil {
		t.Fatalf("host:22 形式条目应匹配 JoinHostPort 路径: %v", err)
	}
	// 命中 h == remote.String() 分支（换一个不在条目主机列表里的 hostname，
	// 仅靠 remote 地址字符串匹配）
	if err := cb("other-name", remote, pub); err != nil {
		t.Fatalf("host:22 形式条目应匹配 remote.String() 路径: %v", err)
	}
	// 不同主机 + 不同 remote 应被拒绝
	wrongRemote := &net.TCPAddr{IP: net.ParseIP("10.0.0.8"), Port: 22}
	if err := cb("10.0.0.8", wrongRemote, pub); err == nil {
		t.Fatal("未登记的主机应被拒绝")
	}
}

// TestKnownHostsCallback_WildcardKeyMismatch 验证通配符域名匹配成立
// 但公钥不一致时仍被拒绝（MITM 场景：攻击者伪造主机密钥）。
func TestKnownHostsCallback_WildcardKeyMismatch(t *testing.T) {
	entryPub, err := generateTestKey()
	if err != nil {
		t.Fatal(err)
	}
	attackerPub, err := generateTestKey()
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "known_hosts")
	line := keyLine("*.example.com", entryPub)
	if err := os.WriteFile(f, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cb, err := knownHostsCallback(f)
	if err != nil {
		t.Fatalf("knownHostsCallback err = %v", err)
	}

	if err := cb("sub.example.com", &net.TCPAddr{IP: net.ParseIP("10.0.0.5"), Port: 22}, attackerPub); err == nil {
		t.Fatal("通配符域名匹配但公钥不一致应被拒绝（host key mismatch）")
	}
}

// ============================================================
// PushAndExec：NewSession 失败分支
// ============================================================

// TestPushAndExec_NewSessionRejected 验证 SSH 握手成功但服务器拒绝一切
// session channel 时，PushAndExec 返回「SSH 会话创建失败」错误。
// 通过本地 mock SSH 服务器对每个 newChannel 调用 Reject 实现，无真实网络依赖。
func TestPushAndExec_NewSessionRejected(t *testing.T) {
	clientKey, _, clientPub := generateRSAKey(t)
	_, hostSigner, _ := generateRSAKey(t)
	keyPath := writePEMPrivateKey(t, clientKey)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})

	serverConfig := &ssh.ServerConfig{
		PublicKeyCallback: func(c ssh.ConnMetadata, offered ssh.PublicKey) (*ssh.Permissions, error) {
			if !bytes.Equal(offered.Marshal(), clientPub.Marshal()) {
				return nil, fmt.Errorf("unknown public key")
			}
			return nil, nil
		},
	}
	serverConfig.AddHostKey(hostSigner)

	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				sConn, chans, reqs, err := ssh.NewServerConn(c, serverConfig)
				if err != nil {
					return
				}
				defer sConn.Close()
				go ssh.DiscardRequests(reqs)
				// 拒绝一切 channel 打开请求 → 客户端 client.NewSession() 失败
				for newChannel := range chans {
					newChannel.Reject(ssh.Prohibited, "sessions not allowed")
				}
			}(conn)
		}
	}()
	defer func() {
		listener.Close()
		<-done
	}()

	_, err = PushAndExec(context.Background(), listener.Addr().String(), "root", keyPath, "", "", "echo hi")
	if err == nil {
		t.Fatal("session channel 被拒绝时应返回错误")
	}
	if !strings.Contains(err.Error(), "SSH 会话创建失败") {
		t.Fatalf("错误信息应提及 SSH 会话创建失败: %v", err)
	}
}

// ============================================================
// AutoProvision：SSH 推送成功闭环
// ============================================================

// startSSHServerOnPort 在指定端口启动本地 SSH mock 服务器（复用 push_test.go
// 的 sshTestServer）。端口被占用（如本机已运行 sshd）时返回错误，由调用方 Skip。
func startSSHServerOnPort(t *testing.T, port int, hostSigner ssh.Signer, allowedPub ssh.PublicKey, handler func(cmd string) (string, int)) (func(), error) {
	t.Helper()
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, err
	}
	serverConfig := &ssh.ServerConfig{
		PublicKeyCallback: func(c ssh.ConnMetadata, offered ssh.PublicKey) (*ssh.Permissions, error) {
			if !bytes.Equal(offered.Marshal(), allowedPub.Marshal()) {
				return nil, fmt.Errorf("unknown public key")
			}
			return nil, nil
		},
	}
	serverConfig.AddHostKey(hostSigner)
	srv := &sshTestServer{
		listener:    listener,
		hostSigner:  hostSigner,
		allowedPub:  allowedPub,
		sshConfig:   serverConfig,
		execHandler: handler,
		done:        make(chan struct{}),
	}
	go srv.serve()
	return func() {
		listener.Close()
		<-srv.done
	}, nil
}

// TestAutoProvision_SSHPushSuccessLoop 验证 AutoProvision 完整纳管闭环的成功路径：
// 扫描存活（22 端口 mock SSH 服务）→ 登记设备 → 签发 token → SSH 推送 bootstrap 成功
// → sum.SSHPushed 递增（覆盖 auto.go 中推送成功分支）。
// 同时校验下发到设备的 bootstrap 命令模板正确拼接（advertise + token）。
//
// 说明：AutoProvision 内部将 SSH 地址硬编码为 "<ip>:22"，因此 mock 服务必须
// 监听 127.0.0.1:22；端口不可用时跳过本测试（环境受限场景不影响其余覆盖）。
// AutoProvision 以 wg.Wait() 同步等待推送 goroutine，断言无需 sleep 轮询。
func TestAutoProvision_SSHPushSuccessLoop(t *testing.T) {
	clientKey, _, clientPub := generateRSAKey(t)
	_, hostSigner, _ := generateRSAKey(t)
	keyPath := writePEMPrivateKey(t, clientKey)

	const (
		wantToken     = "tok" // noopDeps 的 Provision 固定返回 "tok"
		wantAdvertise = "https://opsmesh.example.com:8443"
	)

	var mu sync.Mutex
	var receivedCmd string
	cleanupSrv, lerr := startSSHServerOnPort(t, 22, hostSigner, clientPub, func(cmd string) (string, int) {
		mu.Lock()
		receivedCmd = cmd
		mu.Unlock()
		return "bootstrap-ok", 0
	})
	if lerr != nil {
		t.Skipf("端口 22 不可用（%v），跳过 SSH 推送成功闭环测试", lerr)
	}
	defer cleanupSrv()

	cfg := &config.Config{
		AdvertiseAddr:    wantAdvertise,
		ProvisionSSHKey:  keyPath,
		ProvisionSSHUser: "root",
	}

	sum, err := AutoProvision(context.Background(), noopDeps(), cfg, []string{"127.0.0.1/32"}, "t1")
	if err != nil {
		t.Fatalf("SSH 推送成功闭环不应报错: %v", err)
	}
	if sum.Scanned != 1 {
		t.Fatalf("Scanned 应为 1，got %d", sum.Scanned)
	}
	if sum.Registered != 1 {
		t.Fatalf("Registered 应为 1，got %d", sum.Registered)
	}
	if sum.Provisioned != 1 {
		t.Fatalf("Provisioned 应为 1，got %d", sum.Provisioned)
	}
	if sum.SSHPushed != 1 {
		t.Fatalf("SSH 推送成功后 SSHPushed 应为 1，got %d", sum.SSHPushed)
	}
	if len(sum.Failures) != 0 {
		t.Fatalf("成功闭环不应有 Failures，got %v", sum.Failures)
	}

	// 校验下发 bootstrap 命令模板：advertise 指向 install.sh，token 正确拼接
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(receivedCmd, wantAdvertise+"/install.sh") {
		t.Fatalf("bootstrap 命令应包含 %s/install.sh，got: %s", wantAdvertise, receivedCmd)
	}
	if !strings.Contains(receivedCmd, "--token="+wantToken) {
		t.Fatalf("bootstrap 命令应包含 --token=%s，got: %s", wantToken, receivedCmd)
	}
}