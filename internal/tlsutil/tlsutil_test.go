// Package tlsutil 的单元测试（测试补全）。
// 测试中动态生成自签证书与 CA，避免提交二进制证书文件；测试结束自动清理临时文件。
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
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/credentials"
)

// generateTestCert 生成临时自签证书与私钥 PEM 文件，返回各自路径。
// 证书包含 127.0.0.1 与 localhost SAN，可用作服务端或客户端证书。
// 测试结束自动清理临时文件。
func generateTestCert(t *testing.T) (certPath, keyPath string) {
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
		Subject:               pkix.Name{CommonName: "test-opsmesh"},
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

	certFile, err := os.CreateTemp("", "opsmesh-test-cert-*.pem")
	if err != nil {
		t.Fatalf("创建临时证书文件失败: %v", err)
	}
	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		certFile.Close()
		t.Fatalf("写入证书 PEM 失败: %v", err)
	}
	if err := certFile.Close(); err != nil {
		t.Fatalf("关闭证书文件失败: %v", err)
	}
	certPath = certFile.Name()

	keyBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("编码私钥失败: %v", err)
	}
	keyFile, err := os.CreateTemp("", "opsmesh-test-key-*.pem")
	if err != nil {
		t.Fatalf("创建临时私钥文件失败: %v", err)
	}
	if err := pem.Encode(keyFile, &pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes}); err != nil {
		keyFile.Close()
		t.Fatalf("写入私钥 PEM 失败: %v", err)
	}
	if err := keyFile.Close(); err != nil {
		t.Fatalf("关闭私钥文件失败: %v", err)
	}
	keyPath = keyFile.Name()

	t.Cleanup(func() {
		_ = os.Remove(certPath)
		_ = os.Remove(keyPath)
	})
	return certPath, keyPath
}

// generateTestCA 生成临时自签 CA 证书 PEM 文件，返回其路径。
// 测试结束自动清理临时文件。
func generateTestCA(t *testing.T) string {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成 CA ECDSA 私钥失败: %v", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("生成 CA 序列号失败: %v", err)
	}

	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "test-opsmesh-ca"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("自签 CA 证书失败: %v", err)
	}

	caFile, err := os.CreateTemp("", "opsmesh-test-ca-*.pem")
	if err != nil {
		t.Fatalf("创建临时 CA 文件失败: %v", err)
	}
	if err := pem.Encode(caFile, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		caFile.Close()
		t.Fatalf("写入 CA PEM 失败: %v", err)
	}
	if err := caFile.Close(); err != nil {
		t.Fatalf("关闭 CA 文件失败: %v", err)
	}
	caPath := caFile.Name()

	t.Cleanup(func() {
		_ = os.Remove(caPath)
	})
	return caPath
}

// writeInvalidPEM 创建一个内容为无效 PEM 的临时文件，返回路径。用于触发
// AppendCertsFromPEM 返回 false 的错误分支。
func writeInvalidPEM(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp("", "opsmesh-invalid-*.pem")
	if err != nil {
		t.Fatalf("创建临时文件失败: %v", err)
	}
	if _, err := f.WriteString("-----BEGIN CERTIFICATE-----\nZ29waGVyIGludmFsaWQgY2VydA==\n-----END CERTIFICATE-----\n"); err != nil {
		f.Close()
		t.Fatalf("写入无效 PEM 失败: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("关闭文件失败: %v", err)
	}
	path := f.Name()
	t.Cleanup(func() { _ = os.Remove(path) })
	return path
}

// ---------------------------------------------------------------------------
// ServerCreds
// ---------------------------------------------------------------------------

func TestServerCreds_TLSOnly(t *testing.T) {
	certPath, keyPath := generateTestCert(t)

	creds, err := ServerCreds(certPath, keyPath, "")
	if err != nil {
		t.Fatalf("ServerCreds TLS-only 返回错误: %v", err)
	}
	if creds == nil {
		t.Fatal("ServerCreds TLS-only 返回 nil credentials")
	}
}

func TestServerCreds_MTLS(t *testing.T) {
	certPath, keyPath := generateTestCert(t)
	caPath := generateTestCA(t)

	creds, err := ServerCreds(certPath, keyPath, caPath)
	if err != nil {
		t.Fatalf("ServerCreds mTLS 返回错误: %v", err)
	}
	if creds == nil {
		t.Fatal("ServerCreds mTLS 返回 nil credentials")
	}
}

func TestServerCreds_BadCertFile(t *testing.T) {
	_, keyPath := generateTestCert(t)

	creds, err := ServerCreds("nonexistent-cert-"+strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")+".pem", keyPath, "")
	if err == nil {
		t.Fatal("期望因 cert 文件不存在返回错误，实际 err == nil")
	}
	if creds != nil {
		t.Fatal("错误路径下应返回 nil credentials")
	}
}

func TestServerCreds_BadKeyFile(t *testing.T) {
	certPath, _ := generateTestCert(t)

	creds, err := ServerCreds(certPath, "nonexistent-key-"+strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")+".pem", "")
	if err == nil {
		t.Fatal("期望因 key 文件不存在返回错误，实际 err == nil")
	}
	if creds != nil {
		t.Fatal("错误路径下应返回 nil credentials")
	}
}

// ---------------------------------------------------------------------------
// ClientCreds
// ---------------------------------------------------------------------------

func TestClientCreds_TLSOnly(t *testing.T) {
	caPath := generateTestCA(t)

	creds, err := ClientCreds("", "", caPath)
	if err != nil {
		t.Fatalf("ClientCreds TLS-only 返回错误: %v", err)
	}
	if creds == nil {
		t.Fatal("ClientCreds TLS-only 返回 nil credentials")
	}
}

func TestClientCreds_MTLS(t *testing.T) {
	certPath, keyPath := generateTestCert(t)
	caPath := generateTestCA(t)

	creds, err := ClientCreds(certPath, keyPath, caPath)
	if err != nil {
		t.Fatalf("ClientCreds mTLS 返回错误: %v", err)
	}
	if creds == nil {
		t.Fatal("ClientCreds mTLS 返回 nil credentials")
	}
}

func TestClientCreds_NoFiles(t *testing.T) {
	// 全部为空 → 明文兼容模式，仍返回非 nil credentials（仅含 MinVersion 配置）。
	creds, err := ClientCreds("", "", "")
	if err != nil {
		t.Fatalf("ClientCreds 全空应返回 (creds, nil)，实际 err: %v", err)
	}
	if creds == nil {
		t.Fatal("ClientCreds 全空应返回非 nil credentials")
	}
}

func TestClientCreds_BadCertFile(t *testing.T) {
	caPath := generateTestCA(t)

	creds, err := ClientCreds("nonexistent-client-cert-"+strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")+".pem", "nonexistent-client-key.pem", caPath)
	if err == nil {
		t.Fatal("期望因 cert 文件不存在返回错误，实际 err == nil")
	}
	if creds != nil {
		t.Fatal("错误路径下应返回 nil credentials")
	}
}

// ---------------------------------------------------------------------------
// HTTPClientTLSConfig
// ---------------------------------------------------------------------------

func TestHTTPClientTLSConfig_AllEmpty(t *testing.T) {
	cfg, err := HTTPClientTLSConfig("", "", "")
	if err != nil {
		t.Fatalf("全空应返回 (nil, nil)，实际 err: %v", err)
	}
	if cfg != nil {
		t.Fatalf("全空应返回 nil config，实际得到 %+v", cfg)
	}
}

func TestHTTPClientTLSConfig_WithCert(t *testing.T) {
	certPath, keyPath := generateTestCert(t)

	cfg, err := HTTPClientTLSConfig(certPath, keyPath, "")
	if err != nil {
		t.Fatalf("HTTPClientTLSConfig WithCert 返回错误: %v", err)
	}
	if cfg == nil {
		t.Fatal("期望非 nil config")
	}
	if len(cfg.Certificates) != 1 {
		t.Fatalf("期望 Certificates 长度 1，实际 %d", len(cfg.Certificates))
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Fatalf("期望 MinVersion=TLS1.2，实际 %x", cfg.MinVersion)
	}
}

func TestHTTPClientTLSConfig_WithCA(t *testing.T) {
	caPath := generateTestCA(t)

	cfg, err := HTTPClientTLSConfig("", "", caPath)
	if err != nil {
		t.Fatalf("HTTPClientTLSConfig WithCA 返回错误: %v", err)
	}
	if cfg == nil {
		t.Fatal("期望非 nil config")
	}
	if cfg.RootCAs == nil {
		t.Fatal("期望 RootCAs 非 nil")
	}
}

func TestHTTPClientTLSConfig_BadCA(t *testing.T) {
	invalidCA := writeInvalidPEM(t)

	cfg, err := HTTPClientTLSConfig("", "", invalidCA)
	if err == nil {
		t.Fatal("期望因无效 CA 返回错误，实际 err == nil")
	}
	if cfg != nil {
		t.Fatal("错误路径下应返回 nil config")
	}
}

// ---------------------------------------------------------------------------
// HTTPServerTLSConfig
// ---------------------------------------------------------------------------

func TestHTTPServerTLSConfig_Success(t *testing.T) {
	certPath, keyPath := generateTestCert(t)

	cfg, err := HTTPServerTLSConfig(certPath, keyPath, "")
	if err != nil {
		t.Fatalf("HTTPServerTLSConfig Success 返回错误: %v", err)
	}
	if cfg == nil {
		t.Fatal("期望非 nil config")
	}
	if len(cfg.Certificates) != 1 {
		t.Fatalf("期望 Certificates 长度 1，实际 %d", len(cfg.Certificates))
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Fatalf("期望 MinVersion=TLS1.2，实际 %x", cfg.MinVersion)
	}
	if cfg.ClientAuth != tls.NoClientCert {
		t.Fatalf("无 clientCA 时期望 ClientAuth=NoClientCert，实际 %v", cfg.ClientAuth)
	}
}

func TestHTTPServerTLSConfig_MTLS(t *testing.T) {
	certPath, keyPath := generateTestCert(t)
	caPath := generateTestCA(t)

	cfg, err := HTTPServerTLSConfig(certPath, keyPath, caPath)
	if err != nil {
		t.Fatalf("HTTPServerTLSConfig mTLS 返回错误: %v", err)
	}
	if cfg == nil {
		t.Fatal("期望非 nil config")
	}
	if cfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatalf("期望 ClientAuth=RequireAndVerifyClientCert，实际 %v", cfg.ClientAuth)
	}
	if cfg.ClientCAs == nil {
		t.Fatal("期望 ClientCAs 非 nil")
	}
}

func TestHTTPServerTLSConfig_NoCert(t *testing.T) {
	_, keyPath := generateTestCert(t)

	cfg, err := HTTPServerTLSConfig("", keyPath, "")
	if err == nil {
		t.Fatal("certFile 为空应返回错误")
	}
	if cfg != nil {
		t.Fatal("错误路径下应返回 nil config")
	}
}

func TestHTTPServerTLSConfig_BadCert(t *testing.T) {
	_, keyPath := generateTestCert(t)

	creds, err := HTTPServerTLSConfig("nonexistent-server-cert-"+strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")+".pem", keyPath, "")
	if err == nil {
		t.Fatal("期望因 cert 文件不存在返回错误，实际 err == nil")
	}
	if creds != nil {
		t.Fatal("错误路径下应返回 nil config")
	}
}

func TestHTTPServerTLSConfig_BadClientCA(t *testing.T) {
	certPath, keyPath := generateTestCert(t)
	invalidCA := writeInvalidPEM(t)

	cfg, err := HTTPServerTLSConfig(certPath, keyPath, invalidCA)
	if err == nil {
		t.Fatal("期望因无效 clientCA 返回错误，实际 err == nil")
	}
	if cfg != nil {
		t.Fatal("错误路径下应返回 nil config")
	}
}

// 编译期保证我们使用了 credentials 包（避免未使用导入在重构时被误删）。
var _ credentials.TransportCredentials
