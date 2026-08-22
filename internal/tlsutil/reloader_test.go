// Package tlsutil 的 CertificateReloader 单元测试。
//
// 测试策略：动态生成自签证书写入临时目录，覆盖 BasicLoad / HotReload / ReloadFailureKeepsOld / Close 四个场景。
// 复用 tlsutil_test.go 中的 generateTestCert 风格，但此处需要写入指定路径（覆盖式）以模拟证书替换，
// 故单独定义 writeCertTo helper。
package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeCertTo 生成自签证书与私钥，写入指定路径（覆盖式）。
// 返回证书的 SerialNumber，用于区分不同证书实例。
// 用于模拟证书文件被外部进程替换的场景。
func writeCertTo(t *testing.T, certPath, keyPath string) *big.Int {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成 ECDSA 私钥失败: %v", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("生成序列号失败: %v", err)
	}

	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "test-opsmesh-reloader"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("自签证书失败: %v", err)
	}

	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes}), 0o644); err != nil {
		t.Fatalf("写入证书文件失败: %v", err)
	}

	keyBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("编码私钥失败: %v", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes}), 0o600); err != nil {
		t.Fatalf("写入私钥文件失败: %v", err)
	}

	return serial
}

// certSerial 提取 tls.Certificate 的 SerialNumber，用于区分不同证书实例。
func certSerial(t *testing.T, c *tls.Certificate) *big.Int {
	t.Helper()
	if len(c.Certificate) == 0 {
		t.Fatal("证书 DER 字节为空")
	}
	xc, err := x509.ParseCertificate(c.Certificate[0])
	if err != nil {
		t.Fatalf("解析证书失败: %v", err)
	}
	return xc.SerialNumber
}

// reloadWait 等待 watcher 触发 reload：防抖 100ms + fsnotify 事件传递延迟 + 余量。
// Windows/CI 环境下 fsnotify 事件传递可能较慢，给 1s 余量。
const reloadWait = 1 * time.Second

// ---------------------------------------------------------------------------
// TestCertificateReloader_BasicLoad
// ---------------------------------------------------------------------------

// TestCertificateReloader_BasicLoad 验证初始加载证书成功，GetCertificate 返回有效证书。
func TestCertificateReloader_BasicLoad(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	serial := writeCertTo(t, certPath, keyPath)

	r, err := NewCertificateReloader(certPath, keyPath)
	if err != nil {
		t.Fatalf("NewCertificateReloader 失败: %v", err)
	}
	defer r.Close()

	c, err := r.GetCertificate(nil)
	if err != nil {
		t.Fatalf("GetCertificate 失败: %v", err)
	}
	if c == nil {
		t.Fatal("GetCertificate 返回 nil")
	}
	if certSerial(t, c).Cmp(serial) != 0 {
		t.Fatal("返回的证书 SerialNumber 与写入证书不匹配")
	}
}

// ---------------------------------------------------------------------------
// TestCertificateReloader_HotReload
// ---------------------------------------------------------------------------

// TestCertificateReloader_HotReload 验证证书文件变更后 watcher 触发 reload，GetCertificate 返回新证书。
func TestCertificateReloader_HotReload(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	serialA := writeCertTo(t, certPath, keyPath)

	r, err := NewCertificateReloader(certPath, keyPath)
	if err != nil {
		t.Fatalf("NewCertificateReloader 失败: %v", err)
	}
	defer r.Close()

	// 验证初始证书 A。
	c, err := r.GetCertificate(nil)
	if err != nil {
		t.Fatalf("GetCertificate 初始失败: %v", err)
	}
	if certSerial(t, c).Cmp(serialA) != 0 {
		t.Fatal("初始证书 SerialNumber 不匹配")
	}

	// 写入新证书 B（覆盖同一文件路径，模拟运维替换证书）。
	serialB := writeCertTo(t, certPath, keyPath)
	if serialB.Cmp(serialA) == 0 {
		t.Fatal("新证书 SerialNumber 与旧证书相同（不应发生）")
	}

	// 等待 watcher 触发 reload。
	time.Sleep(reloadWait)

	c2, err := r.GetCertificate(nil)
	if err != nil {
		t.Fatalf("GetCertificate 热重载后失败: %v", err)
	}
	if certSerial(t, c2).Cmp(serialB) != 0 {
		t.Fatalf("热重载后证书未更新为 B：期望 serial=%s，实际=%s", serialB.String(), certSerial(t, c2).String())
	}
}

// ---------------------------------------------------------------------------
// TestCertificateReloader_ReloadFailureKeepsOld
// ---------------------------------------------------------------------------

// TestCertificateReloader_ReloadFailureKeepsOld 验证 reload 失败时（写入无效证书）旧证书保持可用。
func TestCertificateReloader_ReloadFailureKeepsOld(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	serialA := writeCertTo(t, certPath, keyPath)

	r, err := NewCertificateReloader(certPath, keyPath)
	if err != nil {
		t.Fatalf("NewCertificateReloader 失败: %v", err)
	}
	defer r.Close()

	// 写入无效证书内容（模拟半成品文件 / 损坏文件）。
	if err := os.WriteFile(certPath, []byte("-----BEGIN CERTIFICATE-----\nINVALID\n-----END CERTIFICATE-----\n"), 0o644); err != nil {
		t.Fatalf("写入无效内容失败: %v", err)
	}

	// 等待 watcher 触发 reload（reload 会失败但保持旧证书）。
	time.Sleep(reloadWait)

	// 验证旧证书仍可用。
	c, err := r.GetCertificate(nil)
	if err != nil {
		t.Fatalf("GetCertificate 失败: %v", err)
	}
	if certSerial(t, c).Cmp(serialA) != 0 {
		t.Fatal("reload 失败后应保持旧证书，但 SerialNumber 已变化")
	}

	// 恢复有效证书，验证 reload 恢复正常（旧证书仍可用 → 新证书可用）。
	serialB := writeCertTo(t, certPath, keyPath)
	time.Sleep(reloadWait)
	c2, _ := r.GetCertificate(nil)
	if certSerial(t, c2).Cmp(serialB) != 0 {
		t.Fatal("恢复有效证书后应 reload 为新证书")
	}
}

// ---------------------------------------------------------------------------
// TestCertificateReloader_Close
// ---------------------------------------------------------------------------

// TestCertificateReloader_Close 验证 Close 后 watcher 不再监听文件变更。
func TestCertificateReloader_Close(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	serialA := writeCertTo(t, certPath, keyPath)

	r, err := NewCertificateReloader(certPath, keyPath)
	if err != nil {
		t.Fatalf("NewCertificateReloader 失败: %v", err)
	}

	if err := r.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	// Close 后写入新证书，等待，验证未 reload（仍返回旧证书）。
	serialB := writeCertTo(t, certPath, keyPath)
	if serialB.Cmp(serialA) == 0 {
		t.Fatal("新证书 SerialNumber 与旧证书相同（不应发生）")
	}
	time.Sleep(reloadWait)

	c, err := r.GetCertificate(nil)
	if err != nil {
		t.Fatalf("GetCertificate 失败: %v", err)
	}
	if certSerial(t, c).Cmp(serialA) != 0 {
		t.Fatal("Close 后不应再 reload，应保持旧证书")
	}

	// 多次 Close 应安全（不 panic）。
	if err := r.Close(); err != nil {
		t.Fatalf("多次 Close 应安全，实际 err: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestCertificateReloader_BadInitialCert
// ---------------------------------------------------------------------------

// TestCertificateReloader_BadInitialCert 验证初始证书加载失败时返回 error（fail-fast）。
func TestCertificateReloader_BadInitialCert(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "nonexistent-cert.pem")
	keyPath := filepath.Join(dir, "nonexistent-key.pem")

	r, err := NewCertificateReloader(certPath, keyPath)
	if err == nil {
		r.Close()
		t.Fatal("证书文件不存在时应返回 error")
	}
	if r != nil {
		t.Fatal("失败时应返回 nil reloader")
	}
}
