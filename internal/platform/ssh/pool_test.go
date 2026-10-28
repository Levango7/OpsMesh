package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	cryptossh "golang.org/x/crypto/ssh"
)

// execHandler 模拟远端命令：返回 stdout/stderr/退出码，需要 stdin 的命令自行读取。
type execHandler func(cmd string, stdin io.Reader) (stdout, stderr string, code int)

type testSSHServer struct {
	port    int
	handler execHandler

	mu       sync.Mutex
	commands []string
}

func (s *testSSHServer) record(cmd string) {
	s.mu.Lock()
	s.commands = append(s.commands, cmd)
	s.mu.Unlock()
}

func (s *testSSHServer) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.commands...)
}

// startTestSSHServer 起一个 loopback 的进程内 SSH 服务端（ed25519 主机键、
// 仅接受 ops/secret），无需外部 sshd，也不执行真实 shell。
func startTestSSHServer(t *testing.T, handler execHandler) *testSSHServer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("生成主机键失败: %v", err)
	}
	signer, err := cryptossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("构造 signer 失败: %v", err)
	}

	cfg := &cryptossh.ServerConfig{
		PasswordCallback: func(meta cryptossh.ConnMetadata, pass []byte) (*cryptossh.Permissions, error) {
			if meta.User() == "ops" && string(pass) == "secret" {
				return nil, nil
			}
			return nil, fmt.Errorf("invalid credentials")
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	s := &testSSHServer{port: ln.Addr().(*net.TCPAddr).Port, handler: handler}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serveConn(conn, cfg)
		}
	}()
	return s
}

func (s *testSSHServer) serveConn(conn net.Conn, cfg *cryptossh.ServerConfig) {
	defer func() { _ = conn.Close() }()
	sshConn, chans, reqs, err := cryptossh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	defer func() { _ = sshConn.Close() }()
	go cryptossh.DiscardRequests(reqs)

	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			_ = newCh.Reject(cryptossh.UnknownChannelType, "unsupported channel")
			continue
		}
		ch, chReqs, err := newCh.Accept()
		if err != nil {
			return
		}
		go s.serveSession(ch, chReqs)
	}
}

func (s *testSSHServer) serveSession(ch cryptossh.Channel, reqs <-chan *cryptossh.Request) {
	defer func() { _ = ch.Close() }()
	handler := s.handler
	if handler == nil {
		handler = func(string, io.Reader) (string, string, int) { return "", "", 0 }
	}
	for req := range reqs {
		if req.Type != "exec" {
			_ = req.Reply(false, nil)
			continue
		}
		var payload struct{ Command string }
		if err := cryptossh.Unmarshal(req.Payload, &payload); err != nil {
			_ = req.Reply(false, nil)
			return
		}
		_ = req.Reply(true, nil)
		s.record(payload.Command)

		stdout, stderr, code := handler(payload.Command, ch)
		_, _ = io.WriteString(ch, stdout)
		_, _ = ch.Stderr().Write([]byte(stderr))
		_, _ = ch.SendRequest("exit-status", false, cryptossh.Marshal(struct{ Status uint32 }{uint32(code)}))
		return
	}
}

func newPoolClient(t *testing.T, srv *testSSHServer) (*Pool, *cryptossh.Client) {
	t.Helper()
	pool := NewPool()
	t.Cleanup(pool.Close)
	client, err := pool.Get("127.0.0.1", srv.port, "ops", "", "secret")
	if err != nil {
		t.Fatalf("建立 SSH 连接失败: %v", err)
	}
	return pool, client
}

func recvWithin(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case got := <-ch:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("服务端未在 2s 内收到 stdin")
		return ""
	}
}

func TestDialErrors(t *testing.T) {
	srv := startTestSSHServer(t, nil)

	t.Run("no auth method", func(t *testing.T) {
		_, err := dial("127.0.0.1", srv.port, "ops", "", "")
		if err == nil || !strings.Contains(err.Error(), "no auth method provided") {
			t.Fatalf("err = %v, want no auth method provided", err)
		}
	})

	t.Run("missing key file", func(t *testing.T) {
		_, err := dial("127.0.0.1", srv.port, "ops", filepath.Join(t.TempDir(), "missing"), "")
		if err == nil || !strings.Contains(err.Error(), "read ssh key") {
			t.Fatalf("err = %v, want read ssh key", err)
		}
	})

	t.Run("invalid key content", func(t *testing.T) {
		keyPath := filepath.Join(t.TempDir(), "id_ed25519")
		if err := os.WriteFile(keyPath, []byte("not-a-private-key"), 0600); err != nil {
			t.Fatalf("写临时私钥: %v", err)
		}
		_, err := dial("127.0.0.1", srv.port, "ops", keyPath, "")
		if err == nil || !strings.Contains(err.Error(), "parse ssh key") {
			t.Fatalf("err = %v, want parse ssh key", err)
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		_, err := dial("127.0.0.1", srv.port, "ops", "", "wrong")
		if err == nil || !strings.Contains(err.Error(), "ssh dial") {
			t.Fatalf("err = %v, want ssh dial 包装", err)
		}
	})

	t.Run("connection refused", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("预留端口失败: %v", err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		if err := ln.Close(); err != nil {
			t.Fatalf("关闭预留监听: %v", err)
		}
		_, err = dial("127.0.0.1", port, "ops", "", "secret")
		if err == nil || !strings.Contains(err.Error(), "ssh dial") {
			t.Fatalf("err = %v, want ssh dial 包装", err)
		}
	})
}

func TestPoolReusesConnectionAndReconnectsAfterClose(t *testing.T) {
	srv := startTestSSHServer(t, func(cmd string, _ io.Reader) (string, string, int) {
		return "node-1\n", "", 0
	})
	pool := NewPool()

	c1, err := pool.Get("127.0.0.1", srv.port, "ops", "", "secret")
	if err != nil {
		t.Fatalf("首次 Get: %v", err)
	}
	c2, err := pool.Get("127.0.0.1", srv.port, "ops", "", "secret")
	if err != nil {
		t.Fatalf("二次 Get: %v", err)
	}
	if c1 != c2 {
		t.Fatal("同一 addr 的第二次 Get 未复用连接")
	}

	pool.Close()
	if _, _, code, err := Exec(c1, "hostname"); err == nil || code != -1 {
		t.Fatalf("Close 后旧句柄仍可用: code=%d err=%v", code, err)
	}

	c3, err := pool.Get("127.0.0.1", srv.port, "ops", "", "secret")
	if err != nil {
		t.Fatalf("Close 后重连失败: %v", err)
	}
	if c3 == c1 {
		t.Fatal("Close 后 Get 返回了已关闭的旧连接")
	}
	out, _, code, err := Exec(c3, "hostname")
	if err != nil || code != 0 || strings.TrimSpace(out) != "node-1" {
		t.Fatalf("重连后 Exec = (%q,%d,%v)", out, code, err)
	}
	pool.Close()
}

func TestExecStreamsAndExitStatus(t *testing.T) {
	srv := startTestSSHServer(t, func(cmd string, _ io.Reader) (string, string, int) {
		if strings.Contains(cmd, "fail") {
			return "partial\n", "boom\n", 7
		}
		return "ok\n", "warn\n", 0
	})
	_, client := newPoolClient(t, srv)

	out, errOut, code, err := Exec(client, "echo ok")
	if err != nil || code != 0 {
		t.Fatalf("成功命令: err=%v code=%d", err, code)
	}
	if out != "ok\n" || errOut != "warn\n" {
		t.Fatalf("输出映射错误: stdout=%q stderr=%q", out, errOut)
	}

	out, errOut, code, err = Exec(client, "fail")
	if err != nil {
		t.Fatalf("非零退出码不应返回 error: %v", err)
	}
	if code != 7 {
		t.Fatalf("退出码 = %d, want 7", code)
	}
	if out != "partial\n" || errOut != "boom\n" {
		t.Fatalf("失败命令输出未透传: stdout=%q stderr=%q", out, errOut)
	}
}

func TestExecOnClosedClient(t *testing.T) {
	srv := startTestSSHServer(t, nil)
	pool, client := newPoolClient(t, srv)
	pool.Close()

	out, errOut, code, err := Exec(client, "hostname")
	if err == nil || !strings.Contains(err.Error(), "new session") {
		t.Fatalf("err = %v, want new session", err)
	}
	if code != -1 || out != "" || errOut != "" {
		t.Fatalf("异常路径应返回 (-1, 空输出): code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestWriteFileSendsContentAndChmod(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		gotStdIn := make(chan string, 1)
		srv := startTestSSHServer(t, func(_ string, stdin io.Reader) (string, string, int) {
			b, _ := io.ReadAll(stdin)
			gotStdIn <- string(b)
			return "", "", 0
		})
		_, client := newPoolClient(t, srv)

		if err := WriteFile(client, []byte("hello\n"), "/etc/ops/app.conf", 0640); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if got := recvWithin(t, gotStdIn); got != "hello\n" {
			t.Fatalf("远端 stdin = %q, want hello\\n", got)
		}
		wantCmd := "cat > /etc/ops/app.conf && chmod 640 /etc/ops/app.conf"
		if cmds := srv.recorded(); len(cmds) != 1 || cmds[0] != wantCmd {
			t.Fatalf("命令串 = %v, want [%s]", cmds, wantCmd)
		}
	})

	t.Run("command failure", func(t *testing.T) {
		srv := startTestSSHServer(t, func(string, io.Reader) (string, string, int) {
			return "", "", 1
		})
		_, client := newPoolClient(t, srv)
		err := WriteFile(client, []byte("x"), "/etc/ops/app.conf", 0644)
		if err == nil || !strings.Contains(err.Error(), "write file /etc/ops/app.conf") {
			t.Fatalf("err = %v, want write file 包装", err)
		}
	})
}

func TestAppendFile(t *testing.T) {
	gotStdIn := make(chan string, 1)
	srv := startTestSSHServer(t, func(_ string, stdin io.Reader) (string, string, int) {
		b, _ := io.ReadAll(stdin)
		gotStdIn <- string(b)
		return "", "", 0
	})
	_, client := newPoolClient(t, srv)

	if err := AppendFile(client, []byte("line\n"), "/var/log/ops.log"); err != nil {
		t.Fatalf("AppendFile: %v", err)
	}
	if got := recvWithin(t, gotStdIn); got != "line\n" {
		t.Fatalf("远端 stdin = %q, want line\\n", got)
	}
	if cmds := srv.recorded(); len(cmds) != 1 || cmds[0] != "cat >> /var/log/ops.log" {
		t.Fatalf("命令串 = %v", cmds)
	}
}

func TestRemoteHelperCommands(t *testing.T) {
	cases := []struct {
		name string
		run  func(*cryptossh.Client) error
		cmd  string
	}{
		{name: "MkdirAll", run: func(c *cryptossh.Client) error { return MkdirAll(c, "/opt/x") }, cmd: "mkdir -p /opt/x"},
		{name: "EnsureDir", run: func(c *cryptossh.Client) error { return EnsureDir(c, "/opt/y") }, cmd: "mkdir -p /opt/y"},
		{name: "RemoveFile", run: func(c *cryptossh.Client) error { return RemoveFile(c, "/tmp/a") }, cmd: "rm -f /tmp/a"},
		{name: "RemoveDir", run: func(c *cryptossh.Client) error { return RemoveDir(c, "/tmp/d") }, cmd: "rm -rf /tmp/d"},
		{name: "Chmod", run: func(c *cryptossh.Client) error { return Chmod(c, "/etc/x", 0755) }, cmd: "chmod 755 /etc/x"},
		{name: "Chown", run: func(c *cryptossh.Client) error { return Chown(c, "/etc/x", "root:root") }, cmd: "chown root:root /etc/x"},
		{name: "Systemctl", run: func(c *cryptossh.Client) error { return Systemctl(c, "restart", "kubelet") }, cmd: "systemctl restart kubelet"},
		{name: "CopyDir", run: func(c *cryptossh.Client) error { return CopyDir(c, "/src", "/dst") }, cmd: "rsync -a /src /dst"},
	}

	for _, tc := range cases {
		t.Run(tc.name+"/success", func(t *testing.T) {
			srv := startTestSSHServer(t, nil)
			_, client := newPoolClient(t, srv)
			if err := tc.run(client); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if cmds := srv.recorded(); len(cmds) != 1 || cmds[0] != tc.cmd {
				t.Fatalf("命令串 = %v, want [%s]", cmds, tc.cmd)
			}
		})

		t.Run(tc.name+"/failure", func(t *testing.T) {
			srv := startTestSSHServer(t, func(string, io.Reader) (string, string, int) {
				return "", "denied\n", 1
			})
			_, client := newPoolClient(t, srv)
			err := tc.run(client)
			if err == nil || !strings.Contains(err.Error(), "(code 1)") {
				t.Fatalf("err = %v, want 含 (code 1)", err)
			}
		})
	}
}

func TestProbeHelpers(t *testing.T) {
	t.Run("exists when exit code is zero", func(t *testing.T) {
		srv := startTestSSHServer(t, nil)
		_, client := newPoolClient(t, srv)
		if !FileExists(client, "/etc/kubernetes/admin.conf") {
			t.Fatal("FileExists = false, want true")
		}
		if !DirExists(client, "/etc/kubernetes") {
			t.Fatal("DirExists = false, want true")
		}
	})

	t.Run("missing when exit code is non-zero", func(t *testing.T) {
		srv := startTestSSHServer(t, func(string, io.Reader) (string, string, int) {
			return "", "", 1
		})
		_, client := newPoolClient(t, srv)
		if FileExists(client, "/nope") {
			t.Fatal("FileExists = true, want false")
		}
		if DirExists(client, "/nope") {
			t.Fatal("DirExists = true, want false")
		}
	})

	t.Run("session error treated as missing", func(t *testing.T) {
		srv := startTestSSHServer(t, nil)
		pool, client := newPoolClient(t, srv)
		pool.Close()
		if FileExists(client, "/etc/hosts") || DirExists(client, "/etc") {
			t.Fatal("会话异常时应视为不存在")
		}
	})

	t.Run("read file", func(t *testing.T) {
		srv := startTestSSHServer(t, func(string, io.Reader) (string, string, int) {
			return "content\n", "", 0
		})
		_, client := newPoolClient(t, srv)
		got, err := ReadFile(client, "/etc/ops.conf")
		if err != nil || got != "content\n" {
			t.Fatalf("ReadFile = (%q,%v)", got, err)
		}
	})

	t.Run("read file failure", func(t *testing.T) {
		srv := startTestSSHServer(t, func(string, io.Reader) (string, string, int) {
			return "", "missing\n", 2
		})
		_, client := newPoolClient(t, srv)
		if _, err := ReadFile(client, "/absent"); err == nil || !strings.Contains(err.Error(), "(code 2)") {
			t.Fatalf("err = %v, want 含 (code 2)", err)
		}
	})

	t.Run("hostname and os info trimmed", func(t *testing.T) {
		srv := startTestSSHServer(t, func(cmd string, _ io.Reader) (string, string, int) {
			if cmd == "hostname" {
				return "  node-1  \n", "", 0
			}
			return "ubuntu\n", "", 0
		})
		_, client := newPoolClient(t, srv)
		host, err := GetHostname(client)
		if err != nil || host != "node-1" {
			t.Fatalf("GetHostname = (%q,%v)", host, err)
		}
		osID, err := GetOSInfo(client)
		if err != nil || osID != "ubuntu" {
			t.Fatalf("GetOSInfo = (%q,%v)", osID, err)
		}
	})

	t.Run("wait for port", func(t *testing.T) {
		srv := startTestSSHServer(t, nil)
		_, client := newPoolClient(t, srv)
		if err := WaitForPort(client, 6443, time.Second); err != nil {
			t.Fatalf("WaitForPort: %v", err)
		}
		if cmds := srv.recorded(); len(cmds) != 1 || !strings.Contains(cmds[0], ":6443 ") {
			t.Fatalf("探测命令 = %v", cmds)
		}
	})

	t.Run("wait for port timeout without polling", func(t *testing.T) {
		srv := startTestSSHServer(t, nil)
		_, client := newPoolClient(t, srv)
		err := WaitForPort(client, 6443, 0)
		if err == nil || !strings.Contains(err.Error(), "not ready within") {
			t.Fatalf("err = %v, want not ready within", err)
		}
	})

	t.Run("dial tcp", func(t *testing.T) {
		srv := startTestSSHServer(t, nil)
		if err := DialTCP("127.0.0.1", srv.port, time.Second); err != nil {
			t.Fatalf("DialTCP 到监听端口: %v", err)
		}
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("预留端口: %v", err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		if err := ln.Close(); err != nil {
			t.Fatalf("关闭预留监听: %v", err)
		}
		if err := DialTCP("127.0.0.1", port, 200*time.Millisecond); err == nil {
			t.Fatal("DialTCP 到已关闭端口应报错")
		}
	})
}

func TestEnsureFileOnlyWritesWhenMissing(t *testing.T) {
	t.Run("already exists", func(t *testing.T) {
		srv := startTestSSHServer(t, nil)
		_, client := newPoolClient(t, srv)
		if err := EnsureFile(client, "/tmp/exists"); err != nil {
			t.Fatalf("EnsureFile: %v", err)
		}
		if cmds := srv.recorded(); len(cmds) != 1 || cmds[0] != "test -f /tmp/exists" {
			t.Fatalf("已存在时不应写文件: %v", cmds)
		}
	})

	t.Run("creates empty file when missing", func(t *testing.T) {
		gotStdIn := make(chan string, 1)
		srv := startTestSSHServer(t, func(cmd string, stdin io.Reader) (string, string, int) {
			if strings.HasPrefix(cmd, "test -f ") {
				return "", "", 1
			}
			b, _ := io.ReadAll(stdin)
			gotStdIn <- string(b)
			return "", "", 0
		})
		_, client := newPoolClient(t, srv)
		if err := EnsureFile(client, "/tmp/missing"); err != nil {
			t.Fatalf("EnsureFile: %v", err)
		}
		if got := recvWithin(t, gotStdIn); got != "" {
			t.Fatalf("建空文件的 stdin = %q, want 空", got)
		}
		cmds := srv.recorded()
		wantWrite := "cat > /tmp/missing && chmod 644 /tmp/missing"
		if len(cmds) != 2 || cmds[0] != "test -f /tmp/missing" || cmds[1] != wantWrite {
			t.Fatalf("命令序列 = %v", cmds)
		}
	})
}

func TestParseSSHKey(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("生成私钥: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("PKCS8 编码: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	signer, err := ParseSSHKey(string(keyPEM))
	if err != nil {
		t.Fatalf("ParseSSHKey: %v", err)
	}
	if got := signer.PublicKey().Type(); got != "ssh-ed25519" {
		t.Fatalf("公钥类型 = %q, want ssh-ed25519", got)
	}

	if _, err := ParseSSHKey("garbage"); err == nil {
		t.Fatal("非法私钥应报错")
	}
}

func TestSyncFilePassesContentAndLocalMode(t *testing.T) {
	gotStdIn := make(chan string, 1)
	srv := startTestSSHServer(t, func(_ string, stdin io.Reader) (string, string, int) {
		b, _ := io.ReadAll(stdin)
		gotStdIn <- string(b)
		return "", "", 0
	})
	_, client := newPoolClient(t, srv)

	local := filepath.Join(t.TempDir(), "kubeadm.yaml")
	if err := os.WriteFile(local, []byte("apiVersion: kubeadm.k8s.io/v1beta3\n"), 0640); err != nil {
		t.Fatalf("写本地文件: %v", err)
	}
	info, err := os.Stat(local)
	if err != nil {
		t.Fatalf("stat 本地文件: %v", err)
	}

	if err := SyncFile(client, local, "/etc/kubernetes/kubeadm.yaml"); err != nil {
		t.Fatalf("SyncFile: %v", err)
	}
	if got := recvWithin(t, gotStdIn); got != "apiVersion: kubeadm.k8s.io/v1beta3\n" {
		t.Fatalf("远端 stdin = %q", got)
	}
	// 远端 chmod 必须采用本地文件的真实权限位（Windows 上 os.Stat 的 Mode 与
	// Unix 不同），因此按 stat 结果而非硬编码 640 断言。
	wantCmd := fmt.Sprintf("cat > /etc/kubernetes/kubeadm.yaml && chmod %o /etc/kubernetes/kubeadm.yaml", info.Mode())
	if cmds := srv.recorded(); len(cmds) != 1 || cmds[0] != wantCmd {
		t.Fatalf("命令串 = %v, want [%s]", cmds, wantCmd)
	}
}

func TestPoolConcurrentGet(t *testing.T) {
	srv := startTestSSHServer(t, nil)
	pool := NewPool()
	defer pool.Close()

	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client, err := pool.Get("127.0.0.1", srv.port, "ops", "", "secret")
			if err != nil {
				errs <- err
				return
			}
			if client == nil {
				errs <- fmt.Errorf("Get 返回 nil client")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("并发 Get: %v", err)
	}
}
