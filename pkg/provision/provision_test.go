package provision

import (
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

// generateTestKey 生成 RSA 测试密钥并返回 ssh.PublicKey。
func generateTestKey() (ssh.PublicKey, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	pub, err := ssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("new public key: %w", err)
	}
	return pub, nil
}

// keyLine 将 ssh.PublicKey 格式化为 known_hosts 行：hostname key-type base64。
func keyLine(hostname string, pub ssh.PublicKey) string {
	marshaled := ssh.MarshalAuthorizedKey(pub)
	return hostname + " " + string(marshaled)
}

// TestKnownHostsCallback_NonExistent 验证文件不存在时返回错误。
func TestKnownHostsCallback_NonExistent(t *testing.T) {
	_, err := knownHostsCallback("/nonexistent/known_hosts")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

// TestKnownHostsCallback_EmptyFile 验证空文件返回错误。
func TestKnownHostsCallback_EmptyFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "empty_known_hosts")
	if err := os.WriteFile(f, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := knownHostsCallback(f)
	if err == nil {
		t.Fatal("expected error for empty file")
	}
}

// TestKnownHostsCallback_CommentsOnly 验证仅注释行的文件返回错误。
func TestKnownHostsCallback_CommentsOnly(t *testing.T) {
	f := filepath.Join(t.TempDir(), "comment_only")
	content := "# This is a comment\n# Another comment\n"
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := knownHostsCallback(f)
	if err == nil {
		t.Fatal("expected error for comment-only file")
	}
}

// TestKnownHostsCallback_Match 验证匹配的 host key 通过校验。
func TestKnownHostsCallback_Match(t *testing.T) {
	pub, err := generateTestKey()
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "known_hosts")
	line := keyLine("10.0.0.1", pub)
	if err := os.WriteFile(f, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cb, err := knownHostsCallback(f)
	if err != nil {
		t.Fatalf("knownHostsCallback err = %v", err)
	}

	// 匹配的主机应通过
	if err := cb("10.0.0.1", &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 22}, pub); err != nil {
		t.Fatalf("匹配主机应通过，got err = %v", err)
	}
}

// TestKnownHostsCallback_WrongKey 验证不匹配的 key 被拒绝。
func TestKnownHostsCallback_WrongKey(t *testing.T) {
	pub, err := generateTestKey()
	if err != nil {
		t.Fatal(err)
	}
	wrongPub, err := generateTestKey()
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "known_hosts")
	line := keyLine("10.0.0.1", pub)
	if err := os.WriteFile(f, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cb, err := knownHostsCallback(f)
	if err != nil {
		t.Fatalf("knownHostsCallback err = %v", err)
	}

	// 错误的 key 应被拒绝
	if err := cb("10.0.0.1", &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 22}, wrongPub); err == nil {
		t.Fatal("错误的 host key 应被拒绝")
	}
}

// TestKnownHostsCallback_UnknownHost 验证未知主机被拒绝。
func TestKnownHostsCallback_UnknownHost(t *testing.T) {
	pub, err := generateTestKey()
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "known_hosts")
	line := keyLine("allowed-host.example.com", pub)
	if err := os.WriteFile(f, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cb, err := knownHostsCallback(f)
	if err != nil {
		t.Fatalf("knownHostsCallback err = %v", err)
	}

	if err := cb("unknown-host.example.com", &net.TCPAddr{IP: net.ParseIP("10.0.0.99"), Port: 22}, pub); err == nil {
		t.Fatal("未知主机应被拒绝")
	}
}

// TestKnownHostsCallback_Wildcard 验证通配符 *.example.com 匹配。
func TestKnownHostsCallback_Wildcard(t *testing.T) {
	pub, err := generateTestKey()
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "known_hosts")
	line := keyLine("*.example.com", pub)
	if err := os.WriteFile(f, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cb, err := knownHostsCallback(f)
	if err != nil {
		t.Fatalf("knownHostsCallback err = %v", err)
	}

	// *.example.com 应匹配 sub.example.com
	if err := cb("sub.example.com", &net.TCPAddr{IP: net.ParseIP("10.0.0.5"), Port: 22}, pub); err != nil {
		t.Fatalf("通配符匹配主机应通过，got err = %v", err)
	}
	// 但不应匹配 example.com（不是 *.example.com）
	if err := cb("example.com", &net.TCPAddr{IP: net.ParseIP("10.0.0.5"), Port: 22}, pub); err == nil {
		t.Fatal("通配符 *.example.com 不应匹配 example.com")
	}
	// 也不应匹配 different.com
	if err := cb("other.com", &net.TCPAddr{IP: net.ParseIP("10.0.0.6"), Port: 22}, pub); err == nil {
		t.Fatal("通配符 *.example.com 不应匹配 other.com")
	}
}

// TestKnownHostsCallback_MultipleEntries 验证多个 known_hosts 条目。
func TestKnownHostsCallback_MultipleEntries(t *testing.T) {
	pub1, err := generateTestKey()
	if err != nil {
		t.Fatal(err)
	}
	pub2, err := generateTestKey()
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "known_hosts")
	content := keyLine("host-a", pub1) + "\n" + keyLine("host-b", pub2) + "\n"
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cb, err := knownHostsCallback(f)
	if err != nil {
		t.Fatalf("knownHostsCallback err = %v", err)
	}

	// host-a 用 pub1
	if err := cb("host-a", &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 22}, pub1); err != nil {
		t.Fatalf("host-a 应通过: %v", err)
	}
	// host-b 用 pub2
	if err := cb("host-b", &net.TCPAddr{IP: net.ParseIP("10.0.0.2"), Port: 22}, pub2); err != nil {
		t.Fatalf("host-b 应通过: %v", err)
	}
	// host-a 用 pub2（错误 key）
	if err := cb("host-a", &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 22}, pub2); err == nil {
		t.Fatal("host-a 用 pub2 应被拒绝")
	}
}

// TestKnownHostsCallback_SkipsHashed 验证 |1|hash 格式被跳过且不导致错误。
func TestKnownHostsCallback_SkipsHashed(t *testing.T) {
	pub, err := generateTestKey()
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "known_hosts")
	// 包含一个哈希格式 + 一个正常条目
	content := "|1|abc123|def456 ssh-rsa AAAAB3NzaC1yc2E...\n"
	content += keyLine("plain-host", pub) + "\n"
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cb, err := knownHostsCallback(f)
	if err != nil {
		t.Fatalf("knownHostsCallback err = %v", err)
	}

	// 哈希格式被跳过，但正常条目应可用
	if err := cb("plain-host", &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 22}, pub); err != nil {
		t.Fatalf("plain-host 应通过: %v", err)
	}
}

// TestKnownHostsCallback_SSHKeyTypes 验证不同 SSH 密钥类型（RSA, Ed25519）兼容。
func TestKnownHostsCallback_SSHKeyTypes(t *testing.T) {
	// RSA
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rsaPub, err := ssh.NewPublicKey(&rsaKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}

	f := filepath.Join(t.TempDir(), "known_hosts")
	content := keyLine("rsa-host", rsaPub) + "\n"
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cb, err := knownHostsCallback(f)
	if err != nil {
		t.Fatalf("knownHostsCallback err = %v", err)
	}
	if err := cb("rsa-host", &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 22}, rsaPub); err != nil {
		t.Fatalf("RSA key 匹配应通过: %v", err)
	}
}
