package provision

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// generateRSAKey 生成 RSA 私钥并返回 (私钥, ssh.Signer, ssh.PublicKey)。
func generateRSAKey(t *testing.T) (*rsa.PrivateKey, ssh.Signer, ssh.PublicKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	pub, err := ssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("new public key: %v", err)
	}
	return key, signer, pub
}

// writePEMPrivateKey 将 RSA 私钥以 PEM 格式写入临时文件，返回路径。
func writePEMPrivateKey(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "id_rsa")
	buf := bytes.Buffer{}
	if err := pemEncodePrivateKey(&buf, key); err != nil {
		t.Fatalf("encode private key: %v", err)
	}
	if err := os.WriteFile(f, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}
	return f
}

// pemEncodePrivateKey 将 RSA 私钥编码为 OpenSSH PEM 格式。
func pemEncodePrivateKey(buf *bytes.Buffer, key *rsa.PrivateKey) error {
	pemBlock, err := ssh.MarshalPrivateKey(key, "")
	if err != nil {
		return err
	}
	buf.Write(pem.EncodeToMemory(pemBlock))
	return nil
}

// sshTestServer 是一个本地 SSH 测试服务器，用于测试 PushAndExec。
// 它接受指定公钥认证，执行 exec 请求中的命令并通过 stdout 返回固定输出。
type sshTestServer struct {
	listener   net.Listener
	hostSigner ssh.Signer
	allowedPub ssh.PublicKey
	sshConfig  *ssh.ServerConfig
	// execHandler 处理 exec 命令，返回 (stdout, exitCode)。
	execHandler func(cmd string) (string, int)
	// connCount 已接受的连接数（用于验证连接是否建立）。
	connCount int
	done      chan struct{}
}

// startSSHServer 启动本地 SSH 测试服务器，返回地址与清理函数。
func startSSHServer(t *testing.T, hostSigner ssh.Signer, allowedPub ssh.PublicKey, handler func(cmd string) (string, int)) (addr string, cleanup func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	config := &ssh.ServerConfig{
		PublicKeyCallback: func(c ssh.ConnMetadata, offered ssh.PublicKey) (*ssh.Permissions, error) {
			if !bytes.Equal(offered.Marshal(), allowedPub.Marshal()) {
				return nil, fmt.Errorf("unknown public key")
			}
			return nil, nil
		},
	}
	config.AddHostKey(hostSigner)

	srv := &sshTestServer{
		listener:    listener,
		hostSigner:  hostSigner,
		allowedPub:  allowedPub,
		sshConfig:   config,
		execHandler: handler,
		done:        make(chan struct{}),
	}

	go srv.serve()

	return listener.Addr().String(), func() {
		listener.Close()
		<-srv.done
	}
}

func (s *sshTestServer) serve() {
	defer close(s.done)
	for {
		nConn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.connCount++
		go s.handleConn(nConn)
	}
}

func (s *sshTestServer) handleConn(conn net.Conn) {
	defer conn.Close()
	sConn, chans, reqs, err := ssh.NewServerConn(conn, s.sshConfig)
	if err != nil {
		return
	}
	defer sConn.Close()

	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go s.handleSession(channel, requests)
	}
}

func (s *sshTestServer) handleSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()
	for req := range requests {
		switch req.Type {
		case "exec":
			// 解析 exec 请求 payload：4 字节长度 + 命令字符串
			if len(req.Payload) < 4 {
				req.Reply(false, nil)
				continue
			}
			cmdLen := binary.BigEndian.Uint32(req.Payload[:4])
			if uint32(len(req.Payload)) < 4+cmdLen {
				req.Reply(false, nil)
				continue
			}
			cmd := string(req.Payload[4 : 4+cmdLen])
			req.Reply(true, nil)

			out, exitCode := s.execHandler(cmd)
			if out != "" {
				channel.Write([]byte(out))
			}
			// 发送 exit-status
			exitPayload := make([]byte, 4)
			binary.BigEndian.PutUint32(exitPayload, uint32(exitCode))
			channel.SendRequest("exit-status", false, exitPayload)
			return
		case "shell":
			req.Reply(true, nil)
			return
		default:
			req.Reply(false, nil)
		}
	}
}

// ============================================================
// PushAndExec 参数验证与错误处理测试
// ============================================================

// TestPushAndExec_NoKeyPath 验证未配置 SSH 私钥时返回错误。
func TestPushAndExec_NoKeyPath(t *testing.T) {
	_, err := PushAndExec(context.Background(), "127.0.0.1:22", "root", "", "", "", "ls")
	if err == nil {
		t.Fatal("未配置 SSH 私钥应返回错误")
	}
	if !strings.Contains(err.Error(), "未配置 SSH 私钥") {
		t.Fatalf("错误信息应提及未配置 SSH 私钥: %v", err)
	}
}

// TestPushAndExec_KeyFileNotFound 验证私钥文件不存在时返回错误。
func TestPushAndExec_KeyFileNotFound(t *testing.T) {
	_, err := PushAndExec(context.Background(), "127.0.0.1:22", "root", "/nonexistent/id_rsa", "", "", "ls")
	if err == nil {
		t.Fatal("私钥文件不存在应返回错误")
	}
	if !strings.Contains(err.Error(), "读取 SSH 私钥") {
		t.Fatalf("错误信息应提及读取 SSH 私钥失败: %v", err)
	}
}

// TestPushAndExec_InvalidKey 验证私钥文件内容无效时返回解析错误。
func TestPushAndExec_InvalidKey(t *testing.T) {
	f := filepath.Join(t.TempDir(), "bad_key")
	if err := os.WriteFile(f, []byte("not a valid private key"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := PushAndExec(context.Background(), "127.0.0.1:22", "root", f, "", "", "ls")
	if err == nil {
		t.Fatal("无效私钥应返回解析错误")
	}
	if !strings.Contains(err.Error(), "解析 SSH 私钥失败") {
		t.Fatalf("错误信息应提及解析 SSH 私钥失败: %v", err)
	}
}

// TestPushAndExec_KnownHostsFileNotFound 验证 known_hosts 文件不存在时返回错误。
func TestPushAndExec_KnownHostsFileNotFound(t *testing.T) {
	_, _, _ = generateRSAKey(t) // 确保加密库可用
	key, _, _ := generateRSAKey(t)
	keyPath := writePEMPrivateKey(t, key)

	_, err := PushAndExec(context.Background(), "127.0.0.1:22", "root", keyPath, "", "/nonexistent/known_hosts", "ls")
	if err == nil {
		t.Fatal("known_hosts 文件不存在应返回错误")
	}
	if !strings.Contains(err.Error(), "KnownHosts") {
		t.Fatalf("错误信息应提及 KnownHosts: %v", err)
	}
}

// TestPushAndExec_ConnectionFailure 验证 SSH 连接失败时返回错误。
// 连接到一个不存在的主机端口，触发 ssh.Dial 失败。
func TestPushAndExec_ConnectionFailure(t *testing.T) {
	key, _, _ := generateRSAKey(t)
	keyPath := writePEMPrivateKey(t, key)

	// 使用一个保证未监听的端口（RFC 6890 保留端口 1 通常无服务）
	_, err := PushAndExec(context.Background(), "127.0.0.1:1", "root", keyPath, "", "", "ls")
	if err == nil {
		t.Fatal("连接不存在的服务应返回错误")
	}
	if !strings.Contains(err.Error(), "SSH 连接") {
		t.Fatalf("错误信息应提及 SSH 连接失败: %v", err)
	}
}

// TestPushAndExec_InsecureWarningNoKnownHosts 验证未配置 known_hosts 时不报错
// （InsecureIgnoreHostKey 回退），仅打印 stderr 警告。
// 此处验证 knownHostsPath="" 路径不阻断执行（连接失败由后续 Dial 报错）。
func TestPushAndExec_InsecureWarningNoKnownHosts(t *testing.T) {
	key, _, _ := generateRSAKey(t)
	keyPath := writePEMPrivateKey(t, key)

	// knownHostsPath="" → InsecureIgnoreHostKey 回退，连接到不存在端口会失败
	// 但错误应来自 SSH 连接失败，而非 known_hosts 校验
	_, err := PushAndExec(context.Background(), "127.0.0.1:1", "root", keyPath, "", "", "ls")
	if err == nil {
		t.Fatal("应返回连接错误")
	}
	// 错误应来自 SSH 连接失败（而非 KnownHosts）
	if strings.Contains(err.Error(), "KnownHosts") {
		t.Fatalf("未配置 known_hosts 不应返回 KnownHosts 错误: %v", err)
	}
}

// ============================================================
// PushAndExec 成功路径测试（本地 SSH 服务器）
// ============================================================

// TestPushAndExec_Success 验证成功连接并执行命令返回输出。
func TestPushAndExec_Success(t *testing.T) {
	clientKey, _, clientPub := generateRSAKey(t)
	_, hostSigner, _ := generateRSAKey(t)

	keyPath := writePEMPrivateKey(t, clientKey)

	// 启动本地 SSH 服务器
	addr, cleanup := startSSHServer(t, hostSigner, clientPub, func(cmd string) (string, int) {
		return "mock-output: " + cmd, 0
	})
	defer cleanup()

	out, err := PushAndExec(context.Background(), addr, "root", keyPath, "", "", "test-command")
	if err != nil {
		t.Fatalf("成功执行应无错误，got: %v", err)
	}
	if !strings.Contains(out, "mock-output: test-command") {
		t.Fatalf("输出应包含 mock-output: test-command，got: %s", out)
	}
}

// TestPushAndExec_UserDefaultToRoot 验证 user 为空时回退 root 并成功连接。
func TestPushAndExec_UserDefaultToRoot(t *testing.T) {
	clientKey, _, clientPub := generateRSAKey(t)
	_, hostSigner, _ := generateRSAKey(t)

	keyPath := writePEMPrivateKey(t, clientKey)

	addr, cleanup := startSSHServer(t, hostSigner, clientPub, func(cmd string) (string, int) {
		return "ok", 0
	})
	defer cleanup()

	// user 传空字符串，应回退 "root"
	out, err := PushAndExec(context.Background(), addr, "", keyPath, "", "", "id")
	if err != nil {
		t.Fatalf("user 为空回退 root 应成功，got: %v", err)
	}
	if !strings.Contains(out, "ok") {
		t.Fatalf("输出应包含 ok，got: %s", out)
	}
}

// TestPushAndExec_RemoteCommandFailure 验证远程命令执行失败（非零退出码）时返回错误。
func TestPushAndExec_RemoteCommandFailure(t *testing.T) {
	clientKey, _, clientPub := generateRSAKey(t)
	_, hostSigner, _ := generateRSAKey(t)

	keyPath := writePEMPrivateKey(t, clientKey)

	addr, cleanup := startSSHServer(t, hostSigner, clientPub, func(cmd string) (string, int) {
		return "command failed", 1
	})
	defer cleanup()

	out, err := PushAndExec(context.Background(), addr, "root", keyPath, "", "", "false")
	if err == nil {
		t.Fatal("远程命令失败应返回错误")
	}
	if !strings.Contains(err.Error(), "远程执行失败") {
		t.Fatalf("错误信息应提及远程执行失败: %v", err)
	}
	if !strings.Contains(out, "command failed") {
		t.Fatalf("输出应包含 command failed，got: %s", out)
	}
}

// TestPushAndExec_ContextCancel 验证上下文取消时中断执行并返回 ctx.Err()。
func TestPushAndExec_ContextCancel(t *testing.T) {
	clientKey, _, clientPub := generateRSAKey(t)
	_, hostSigner, _ := generateRSAKey(t)

	keyPath := writePEMPrivateKey(t, clientKey)

	// 服务器处理命令时阻塞，等待客户端取消
	addr, cleanup := startSSHServer(t, hostSigner, clientPub, func(cmd string) (string, int) {
		time.Sleep(30 * time.Second)
		return "should-not-reach", 0
	})
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	out, err := PushAndExec(ctx, addr, "root", keyPath, "", "", "sleep 30")
	if err == nil {
		t.Fatal("上下文取消应返回错误")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("错误应为 context.DeadlineExceeded，got: %v", err)
	}
	_ = out // 取消时输出可能为空
}

// TestPushAndExec_SuccessWithKnownHosts 验证配置 known_hosts 时成功连接并校验主机密钥。
func TestPushAndExec_SuccessWithKnownHosts(t *testing.T) {
	clientKey, _, clientPub := generateRSAKey(t)
	_, hostSigner, hostPub := generateRSAKey(t)

	keyPath := writePEMPrivateKey(t, clientKey)

	addr, cleanup := startSSHServer(t, hostSigner, clientPub, func(cmd string) (string, int) {
		return "verified", 0
	})
	defer cleanup()

	// 写入 known_hosts 文件：使用实际连接地址（host:port）匹配 hostname 或 remote.String()
	knownHostsPath := filepath.Join(t.TempDir(), "known_hosts")
	hostLine := keyLine(addr, hostPub)
	if err := os.WriteFile(knownHostsPath, []byte(hostLine+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := PushAndExec(context.Background(), addr, "root", keyPath, "", knownHostsPath, "test")
	if err != nil {
		t.Fatalf("known_hosts 校验通过应成功，got: %v", err)
	}
	if !strings.Contains(out, "verified") {
		t.Fatalf("输出应包含 verified，got: %s", out)
	}
}

// TestPushAndExec_KnownHostsMismatch 验证 known_hosts 主机密钥不匹配时连接被拒绝。
func TestPushAndExec_KnownHostsMismatch(t *testing.T) {
	clientKey, _, clientPub := generateRSAKey(t)
	_, hostSigner, _ := generateRSAKey(t)
	_, _, wrongHostPub := generateRSAKey(t)

	keyPath := writePEMPrivateKey(t, clientKey)

	addr, cleanup := startSSHServer(t, hostSigner, clientPub, func(cmd string) (string, int) {
		return "should-not-reach", 0
	})
	defer cleanup()

	// known_hosts 中写入错误的主机公钥（使用实际地址匹配 hostname）
	knownHostsPath := filepath.Join(t.TempDir(), "known_hosts")
	hostLine := keyLine(addr, wrongHostPub)
	if err := os.WriteFile(knownHostsPath, []byte(hostLine+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := PushAndExec(context.Background(), addr, "root", keyPath, "", knownHostsPath, "test")
	if err == nil {
		t.Fatal("主机密钥不匹配应返回错误")
	}
	if !strings.Contains(err.Error(), "host key mismatch") && !strings.Contains(err.Error(), "不在 KnownHosts") {
		t.Fatalf("错误应提及 host key mismatch 或不在 KnownHosts: %v", err)
	}
}

// TestPushAndExec_KeyWithPassphrase 验证带密码的私钥能正确解析并连接。
func TestPushAndExec_KeyWithPassphrase(t *testing.T) {
	clientKey, _, clientPub := generateRSAKey(t)
	_, hostSigner, _ := generateRSAKey(t)

	// 生成带密码保护的私钥文件
	passphrase := "test-pass-123"
	keyPath := filepath.Join(t.TempDir(), "id_rsa_enc")
	pemBlock, err := ssh.MarshalPrivateKeyWithPassphrase(clientKey, "", []byte(passphrase))
	if err != nil {
		t.Fatalf("marshal encrypted key: %v", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(pemBlock), 0o600); err != nil {
		t.Fatal(err)
	}

	addr, cleanup := startSSHServer(t, hostSigner, clientPub, func(cmd string) (string, int) {
		return "encrypted-ok", 0
	})
	defer cleanup()

	out, err := PushAndExec(context.Background(), addr, "root", keyPath, passphrase, "", "test")
	if err != nil {
		t.Fatalf("带密码私钥应成功连接，got: %v", err)
	}
	if !strings.Contains(out, "encrypted-ok") {
		t.Fatalf("输出应包含 encrypted-ok，got: %s", out)
	}
}

// TestPushAndExec_KeyWithWrongPassphrase 验证私钥密码错误时返回解析失败。
func TestPushAndExec_KeyWithWrongPassphrase(t *testing.T) {
	clientKey, _, _ := generateRSAKey(t)

	passphrase := "correct-pass"
	keyPath := filepath.Join(t.TempDir(), "id_rsa_enc")
	pemBlock, mErr := ssh.MarshalPrivateKeyWithPassphrase(clientKey, "", []byte(passphrase))
	if mErr != nil {
		t.Fatalf("marshal encrypted key: %v", mErr)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(pemBlock), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := PushAndExec(context.Background(), "127.0.0.1:1", "root", keyPath, "wrong-pass", "", "test")
	if err == nil {
		t.Fatal("错误密码应返回解析错误")
	}
	if !strings.Contains(err.Error(), "解析 SSH 私钥失败") {
		t.Fatalf("错误信息应提及解析 SSH 私钥失败: %v", err)
	}
}
