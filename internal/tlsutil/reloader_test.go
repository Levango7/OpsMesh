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
// 仅用于「断言什么都不发生」的负向场景（Close 后不再 reload）；
// 正向等待一律用下方的轮询 helper——固定 sleep 在事件面延迟大的
// 环境（本机 Windows 实测）会把「事件尚未到达」误判为「reload 未发生」（TD-82）。
const reloadWait = 1 * time.Second

// pollInterval 是轮询 helper 的采样间隔。
const pollInterval = 50 * time.Millisecond

// pollDeadline 是轮询 helper 的截止时间：远大于任何合理的事件面延迟，
// 又远小于 go test 默认超时，失败时能留下可诊断的余量。
const pollDeadline = 10 * time.Second

// waitReloadAttempts 轮询等待 reload 尝试次数达到 want。
// 计数增加是「事件已过防抖、reload 已被调用」的确定性信号。
func waitReloadAttempts(t *testing.T, r *CertificateReloader, want int64) {
	t.Helper()
	deadline := time.Now().Add(pollDeadline)
	for r.ReloadAttempts() < want {
		if time.Now().After(deadline) {
			t.Fatalf("等待 reload 尝试次数超时：期望 ≥%d，实际 %d（%s 内 watcher 未触发）",
				want, r.ReloadAttempts(), pollDeadline)
		}
		time.Sleep(pollInterval)
	}
}

// waitCertSerial 轮询等待当前生效证书的序列号等于 want。
func waitCertSerial(t *testing.T, r *CertificateReloader, want *big.Int) {
	t.Helper()
	deadline := time.Now().Add(pollDeadline)
	for {
		c, err := r.GetCertificate(nil)
		if err != nil {
			t.Fatalf("GetCertificate 失败: %v", err)
		}
		if certSerial(t, c).Cmp(want) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待证书序列号超时：期望 %s（%s 内未生效）", want.String(), pollDeadline)
		}
		time.Sleep(pollInterval)
	}
}

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

	// 等待 watcher 触发 reload（轮询：事件面延迟在不同 OS 上差异极大，
	// 固定 sleep 会把「事件尚未到达」误判为「reload 未发生」）。
	waitReloadAttempts(t, r, 1)
	waitCertSerial(t, r, serialB)
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

	// 等待 watcher 触发 reload（无效证书 ⇒ reload 失败、保持旧证书）。
	// 轮询尝试次数而非 sleep：计数增加证明事件已被处理，此时断言
	// 「仍为旧证书」才是对「失败保持」的真实检验（TD-82 根因）。
	waitReloadAttempts(t, r, 1)

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
	waitReloadAttempts(t, r, 2)
	waitCertSerial(t, r, serialB)
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
