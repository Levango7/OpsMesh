// Web/REST（B/S 端口）TLS 配置测试。
//
// 覆盖 buildHTTPTLS 的三种模式，并用真实 TLS 握手证明：
//   - auto + 证书 → 监听确实走 HTTPS，且明文请求拿不到业务响应；
//   - off → 返回 nil（明文，上游反代终止 TLS 的部署形态）；
//   - on → 缺证书直接报错，不静默退回明文。
package controlplane

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/tlsutil"
)

// writeSelfSignedCert 生成自签服务端证书（SAN 含 localhost/127.0.0.1），
// 返回证书路径、私钥路径与证书 PEM（供客户端 RootCAs 使用）。
func writeSelfSignedCert(t *testing.T) (certFile, keyFile string, certPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成私钥失败: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "opsmesh-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("签发证书失败: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("序列化私钥失败: %v", err)
	}
	dir := t.TempDir()
	certFile = filepath.Join(dir, "server.crt")
	keyFile = filepath.Join(dir, "server.key")
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatalf("写证书失败: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("写私钥失败: %v", err)
	}
	return certFile, keyFile, certPEM
}

func TestBuildHTTPTLS_Auto(t *testing.T) {
	// auto + 无证书：明文（开发/内网），返回 nil 不报错。
	s := &Server{cfg: &config.Config{HTTPTLS: "auto"}}
	cfg, err := s.buildHTTPTLS()
	if err != nil || cfg != nil {
		t.Fatalf("auto 无证书应返回 (nil, nil), got (%v, %v)", cfg, err)
	}

	// auto + 证书齐备：启用 HTTPS。
	certFile, keyFile, _ := writeSelfSignedCert(t)
	s2 := &Server{cfg: &config.Config{HTTPTLS: "auto"}, tlsCert: certFile, tlsKey: keyFile}
	cfg2, err := s2.buildHTTPTLS()
	if err != nil {
		t.Fatalf("auto 有证书应成功: %v", err)
	}
	if cfg2 == nil {
		t.Fatal("auto 有证书应返回 TLS 配置")
	}
	if cfg2.MinVersion < tls.VersionTLS12 {
		t.Fatalf("MinVersion = %x, 应 ≥ TLS1.2", cfg2.MinVersion)
	}
	// 不得要求客户端证书：浏览器不会出示，开启即全员被挡在握手层。
	if cfg2.ClientAuth != tls.NoClientCert {
		t.Fatalf("ClientAuth = %v, Web/REST 不应要求客户端证书", cfg2.ClientAuth)
	}
}

func TestBuildHTTPTLS_Off(t *testing.T) {
	// off：即使证书齐备也保持明文（上游反代终止 TLS 的部署形态）。
	certFile, keyFile, _ := writeSelfSignedCert(t)
	s := &Server{cfg: &config.Config{HTTPTLS: "off"}, tlsCert: certFile, tlsKey: keyFile}
	cfg, err := s.buildHTTPTLS()
	if err != nil || cfg != nil {
		t.Fatalf("off 应返回 (nil, nil), got (%v, %v)", cfg, err)
	}
}

func TestBuildHTTPTLS_OnRequiresCert(t *testing.T) {
	s := &Server{cfg: &config.Config{HTTPTLS: "on"}}
	if _, err := s.buildHTTPTLS(); err == nil {
		t.Fatal("--http-tls=on 缺证书应报错，不得静默退回明文")
	}
	// 只有证书没有私钥同样报错。
	certFile, _, _ := writeSelfSignedCert(t)
	s2 := &Server{cfg: &config.Config{HTTPTLS: "on"}, tlsCert: certFile}
	if _, err := s2.buildHTTPTLS(); err == nil {
		t.Fatal("--http-tls=on 缺私钥应报错")
	}
}

func TestBuildHTTPTLS_IllegalMode(t *testing.T) {
	s := &Server{cfg: &config.Config{HTTPTLS: "maybe"}}
	if _, err := s.buildHTTPTLS(); err == nil {
		t.Fatal("非法 --http-tls 应报错")
	}
}

func TestBuildHTTPTLS_EmptyModeTreatedAsAuto(t *testing.T) {
	// 程序化构造 Config（不设 HTTPTLS）等价 auto。
	certFile, keyFile, _ := writeSelfSignedCert(t)
	s := &Server{cfg: &config.Config{}, tlsCert: certFile, tlsKey: keyFile}
	cfg, err := s.buildHTTPTLS()
	if err != nil || cfg == nil {
		t.Fatalf("空 HTTPTLS + 证书应等价 auto 并返回 TLS 配置, got (%v, %v)", cfg, err)
	}
}

func TestBuildHTTPTLS_ReusesTLSWatchReloader(t *testing.T) {
	certFile, keyFile, certPEM := writeSelfSignedCert(t)
	reloader, err := tlsutil.NewCertificateReloader(certFile, keyFile)
	if err != nil {
		t.Fatalf("构造热重载器失败: %v", err)
	}
	defer reloader.Close()

	s := &Server{cfg: &config.Config{HTTPTLS: "auto", TLSWatch: true}, tlsCert: certFile, tlsKey: keyFile, tlsReloader: reloader}
	cfg, err := s.buildHTTPTLS()
	if err != nil || cfg == nil {
		t.Fatalf("热重载模式应返回 TLS 配置: (%v, %v)", cfg, err)
	}
	if cfg.GetCertificate == nil {
		t.Fatal("--tls-watch 下应复用热重载器（GetCertificate 非空），而非一次性加载证书")
	}
	// GetCertificate 返回的证书应与磁盘上的证书一致（证明 HTTP 与 gRPC 共用同一份凭证）。
	got, err := cfg.GetCertificate(nil)
	if err != nil {
		t.Fatalf("GetCertificate 失败: %v", err)
	}
	wantBlock, _ := pem.Decode(certPEM)
	if got == nil || len(got.Certificate) == 0 || string(got.Certificate[0]) != string(wantBlock.Bytes) {
		t.Fatal("热重载器返回的证书与磁盘证书不一致")
	}
}

// TestBuildHTTPTLS_ServesHTTPS 用真实握手验证：该配置能让监听走 HTTPS，
// 且明文 HTTP 请求拿不到业务响应（P0-2 的核心断言）。
func TestBuildHTTPTLS_ServesHTTPS(t *testing.T) {
	certFile, keyFile, certPEM := writeSelfSignedCert(t)
	s := &Server{cfg: &config.Config{HTTPTLS: "auto"}, tlsCert: certFile, tlsKey: keyFile}
	tlsCfg, err := s.buildHTTPTLS()
	if err != nil || tlsCfg == nil {
		t.Fatalf("buildHTTPTLS: (%v, %v)", tlsCfg, err)
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	srv.TLS = tlsCfg
	srv.StartTLS()
	defer srv.Close()

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certPEM) {
		t.Fatal("客户端 RootCAs 装载失败")
	}
	httpsClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
	resp, err := httpsClient.Get(srv.URL)
	if err != nil {
		t.Fatalf("HTTPS 请求失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HTTPS 状态码 = %d, want 200", resp.StatusCode)
	}

	// 明文 HTTP 打同一端口：TLS 监听会拒绝（400/握手失败），不应返回业务 200。
	plainClient := &http.Client{Timeout: 3 * time.Second}
	presp, perr := plainClient.Get(srv.URL)
	if perr == nil {
		defer presp.Body.Close()
		if presp.StatusCode == http.StatusOK {
			t.Fatal("明文 HTTP 请求竟拿到 200：监听未真正启用 TLS")
		}
	}
}
