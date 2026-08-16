// grpcclient_extra_test.go 补充 grpcclient.go 中未覆盖的函数单元测试。
//
// 覆盖：
//   - SetSecret / signContext 各分支
//   - grpcTarget 边界（URL 解析失败、IPv6、纯 host）
//   - NewGRPCClient TLS 路径 / 多地址
//   - invoke 失败路径 / invokeWithBalancer 失败路径
//   - getConn 复用 / evictConn 淘汰
//   - isConnError 各分支
//   - Register/Heartbeat/PullTasks/ReportResult/CancelTask/PollCancels/ReportLogs
//   - Close
package agent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"opsmesh/internal/discovery"
	"opsmesh/internal/grpcx"
	"opsmesh/internal/proto"
)

// --- SetSecret ---

func TestSetSecret_Extra(t *testing.T) {
	cli, err := newTestGRPCClient()
	if err != nil {
		t.Fatal(err)
	}
	if cli.secret != "" {
		t.Fatal("初始 secret 应为空")
	}
	cli.SetSecret("my-secret")
	if cli.secret != "my-secret" {
		t.Fatalf("SetSecret 后 secret 应为 my-secret，得到 %q", cli.secret)
	}
}

// --- signContext ---

func TestSignContext_NoSecret(t *testing.T) {
	cli, err := newTestGRPCClient()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	out := cli.signContext(ctx, "agent-1")
	if out != ctx {
		t.Fatal("无 secret 时应原样返回 ctx")
	}
}

func TestSignContext_EmptyAgentID(t *testing.T) {
	cli, err := newTestGRPCClient()
	if err != nil {
		t.Fatal(err)
	}
	cli.SetSecret("secret")
	ctx := context.Background()
	out := cli.signContext(ctx, "")
	if out != ctx {
		t.Fatal("空 agentID 时应原样返回 ctx")
	}
}

func TestSignContext_WithSecret(t *testing.T) {
	cli, err := newTestGRPCClient()
	if err != nil {
		t.Fatal(err)
	}
	cli.SetSecret("test-secret")
	ctx := context.Background()
	out := cli.signContext(ctx, "agent-xyz")
	if out == ctx {
		t.Fatal("有 secret + agentID 时应返回新的 ctx（带 metadata）")
	}
	// 多次调用应产生不同的 timestamp（时间推进）
	time.Sleep(10 * time.Millisecond)
	out2 := cli.signContext(ctx, "agent-xyz")
	// 两次签名都应成功（不 panic 即可）
	_ = out
	_ = out2
}

// --- grpcTarget 边界 ---

func TestGrpcTarget_URLParseError(t *testing.T) {
	// 含 scheme 但解析失败的 URL
	_, err := grpcTarget("http://[::1", 9090)
	if err == nil {
		t.Fatal("非法 URL 应返回错误")
	}
}

func TestGrpcTarget_IPv6WithPort(t *testing.T) {
	got, err := grpcTarget("http://[::1]:8080", 9090)
	if err != nil {
		t.Fatalf("IPv6 解析失败: %v", err)
	}
	if got != "[::1]:9090" {
		t.Fatalf("IPv6 + 端口 = %q, want [::1]:9090", got)
	}
}

func TestGrpcTarget_HostOnlyDefaultPort(t *testing.T) {
	got, err := grpcTarget("hostonly", 0)
	if err != nil {
		t.Fatalf("纯 host 解析失败: %v", err)
	}
	if got != "hostonly:9090" {
		t.Fatalf("端口<=0 应用默认 9090，得到 %q", got)
	}
}

func TestGrpcTarget_ExplicitHostPort(t *testing.T) {
	got, err := grpcTarget("cp.example:9091", 9090)
	if err != nil {
		t.Fatal(err)
	}
	if got != "cp.example:9091" {
		t.Fatalf("显式 host:port 应尊重，得到 %q", got)
	}
}

func TestGrpcTarget_HTTPScheme(t *testing.T) {
	got, err := grpcTarget("http://127.0.0.1:8080", 9090)
	if err != nil {
		t.Fatal(err)
	}
	if got != "127.0.0.1:9090" {
		t.Fatalf("HTTP scheme 应剥离端口拼 9090，得到 %q", got)
	}
}

// --- NewGRPCClient TLS ---

func TestNewGRPCClient_TLS(t *testing.T) {
	certPath, keyPath := generateTestCertExtra(t)
	cli, err := NewGRPCClient([]string{"127.0.0.1:9090"}, certPath, keyPath, "", 9090)
	if err != nil {
		t.Fatalf("TLS 构造应成功: %v", err)
	}
	if cli.creds == nil {
		t.Fatal("creds 不应为 nil")
	}
}

func TestNewGRPCClient_BadTLS(t *testing.T) {
	// 不存在的证书文件应失败
	_, err := NewGRPCClient([]string{"127.0.0.1:9090"}, "/nonexistent/cert.pem", "/nonexistent/key.pem", "", 9090)
	if err == nil {
		t.Fatal("不存在的证书应返回错误")
	}
}

func TestNewGRPCClient_MultiAddrs(t *testing.T) {
	cli, err := NewGRPCClient([]string{"127.0.0.1:9090", "127.0.0.2:9090"}, "", "", "", 9090)
	if err != nil {
		t.Fatal(err)
	}
	if len(cli.addrs) != 2 {
		t.Fatalf("应有 2 个地址，得到 %d", len(cli.addrs))
	}
}

// --- isConnError ---

func TestIsConnError_Nil(t *testing.T) {
	if isConnError(nil) {
		t.Fatal("nil 错误应返回 false")
	}
}

func TestIsConnError_Unavailable(t *testing.T) {
	err := status.Error(codes.Unavailable, "conn broken")
	if !isConnError(err) {
		t.Fatal("Unavailable 应为连接错误")
	}
}

func TestIsConnError_Canceled(t *testing.T) {
	err := status.Error(codes.Canceled, "canceled")
	if !isConnError(err) {
		t.Fatal("Canceled 应为连接错误")
	}
}

func TestIsConnError_DeadlineExceeded(t *testing.T) {
	err := status.Error(codes.DeadlineExceeded, "timeout")
	if !isConnError(err) {
		t.Fatal("DeadlineExceeded 应为连接错误")
	}
}

func TestIsConnError_BusinessError(t *testing.T) {
	err := status.Error(codes.InvalidArgument, "bad arg")
	if isConnError(err) {
		t.Fatal("InvalidArgument 不应为连接错误")
	}
}

func TestIsConnError_NonGRPCError(t *testing.T) {
	// 非 gRPC status 错误应保守视为连接错误
	if !isConnError(context.DeadlineExceeded) {
		t.Fatal("非 gRPC status 错误应视为连接错误")
	}
}

// --- invoke 失败路径 ---

func TestInvoke_AllAddrsFail(t *testing.T) {
	// 用不存在的端口，invoke 应失败
	cli, err := NewGRPCClient([]string{"127.0.0.1:1"}, "", "", "", 9090)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = cli.invoke(ctx, "/opsmesh.v1.Registration/Register", &proto.AgentInfo{}, &struct{}{})
	if err == nil {
		t.Fatal("连接不存在的服务器应返回错误")
	}
}

func TestInvoke_MultiAddrsFailover(t *testing.T) {
	// 多个不存在地址，应尝试所有地址后失败
	cli, err := NewGRPCClient([]string{"127.0.0.1:1", "127.0.0.1:2"}, "", "", "", 9090)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = cli.invoke(ctx, "/some/method", &struct{}{}, &struct{}{})
	if err == nil {
		t.Fatal("所有地址都失败应返回错误")
	}
}

// --- invokeWithBalancer ---

func TestInvokeWithBalancer_FailoverFail(t *testing.T) {
	cli, err := newTestGRPCClient()
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	svcs := []discovery.Service{
		{ID: "127.0.0.1:1", Addr: "127.0.0.1", Port: 1, Healthy: true},
	}
	fo := discovery.NewFailover(svcs)
	cli.SetBalancer(fo)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = cli.invoke(ctx, "/some/method", &struct{}{}, &struct{}{})
	if err == nil {
		t.Fatal("连接不存在的服务器应失败")
	}
}

func TestInvokeWithBalancer_RoundRobinFail(t *testing.T) {
	cli, err := newTestGRPCClient()
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	svcs := []discovery.Service{
		{ID: "127.0.0.1:1", Addr: "127.0.0.1", Port: 1, Healthy: true},
		{ID: "127.0.0.1:2", Addr: "127.0.0.1", Port: 2, Healthy: true},
	}
	rr := discovery.NewRoundRobin(svcs)
	cli.SetBalancer(rr)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = cli.invoke(ctx, "/some/method", &struct{}{}, &struct{}{})
	if err == nil {
		t.Fatal("所有实例都失败应返回错误")
	}
}

func TestInvokeWithBalancer_NextError(t *testing.T) {
	cli, err := newTestGRPCClient()
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	// 空实例列表，Next 应返回 ErrNoInstances
	fo := discovery.NewFailover(nil)
	cli.SetBalancer(fo)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = cli.invoke(ctx, "/some/method", &struct{}{}, &struct{}{})
	if err == nil {
		t.Fatal("无实例应返回错误")
	}
}

// --- getConn / evictConn ---

func TestGetConn_Reuse(t *testing.T) {
	cli, err := newTestGRPCClient()
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	c1, err := cli.getConn("127.0.0.1:9090")
	if err != nil {
		t.Fatal(err)
	}
	c2, err := cli.getConn("127.0.0.1:9090")
	if err != nil {
		t.Fatal(err)
	}
	if c1 != c2 {
		t.Fatal("同地址应复用连接")
	}
}

func TestEvictConn_Extra(t *testing.T) {
	cli, err := newTestGRPCClient()
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	_, err = cli.getConn("127.0.0.1:9090")
	if err != nil {
		t.Fatal(err)
	}
	if len(cli.conns) != 1 {
		t.Fatalf("应有 1 个连接，得到 %d", len(cli.conns))
	}
	cli.evictConn("127.0.0.1:9090")
	if len(cli.conns) != 0 {
		t.Fatalf("evictConn 后应为 0 个连接，得到 %d", len(cli.conns))
	}
	if cli.connFailures.Load() != 1 {
		t.Fatalf("connFailures 应为 1，得到 %d", cli.connFailures.Load())
	}
}

func TestEvictConn_NotExist(t *testing.T) {
	cli, err := newTestGRPCClient()
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	// evict 不存在的连接不应 panic
	cli.evictConn("nonexistent:1234")
	if cli.connFailures.Load() != 1 {
		t.Fatalf("即使连接不存在也应计数 failures，得到 %d", cli.connFailures.Load())
	}
}

// --- Register / Heartbeat / PullTasks / ReportResult / CancelTask / PollCancels / ReportLogs ---

func TestRegister_Fail(t *testing.T) {
	cli, err := NewGRPCClient([]string{"127.0.0.1:1"}, "", "", "", 9090)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = cli.Register(ctx, &proto.AgentInfo{AgentID: "a1"})
	if err == nil {
		t.Fatal("连接不存在应失败")
	}
}

func TestHeartbeat_Fail(t *testing.T) {
	cli, err := NewGRPCClient([]string{"127.0.0.1:1"}, "", "", "", 9090)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// 直接构造 HeartbeatReq
	err = cli.Heartbeat(ctx, &grpcx.HeartbeatReq{AgentID: "a1", Status: "online", Load: 1})
	if err == nil {
		t.Fatal("连接不存在应失败")
	}
}

func TestPullTasks_Fail(t *testing.T) {
	cli, err := NewGRPCClient([]string{"127.0.0.1:1"}, "", "", "", 9090)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = cli.PullTasks(ctx, "a1")
	if err == nil {
		t.Fatal("连接不存在应失败")
	}
}

func TestReportResult_Fail(t *testing.T) {
	cli, err := NewGRPCClient([]string{"127.0.0.1:1"}, "", "", "", 9090)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = cli.ReportResult(ctx, &proto.TaskResult{TaskID: "t1", AgentID: "a1"})
	if err == nil {
		t.Fatal("连接不存在应失败")
	}
}

func TestCancelTask_Fail(t *testing.T) {
	cli, err := NewGRPCClient([]string{"127.0.0.1:1"}, "", "", "", 9090)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = cli.CancelTask(ctx, "task-1", "tenant-1")
	if err == nil {
		t.Fatal("连接不存在应失败")
	}
}

func TestPollCancels_Fail(t *testing.T) {
	cli, err := NewGRPCClient([]string{"127.0.0.1:1"}, "", "", "", 9090)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = cli.PollCancels(ctx, "a1")
	if err == nil {
		t.Fatal("连接不存在应失败")
	}
}

func TestReportLogs_Fail(t *testing.T) {
	cli, err := NewGRPCClient([]string{"127.0.0.1:1"}, "", "", "", 9090)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = cli.ReportLogs(ctx, &proto.LogReport{AgentID: "a1"})
	if err == nil {
		t.Fatal("连接不存在应失败")
	}
}

// --- Close ---

func TestClose_Extra(t *testing.T) {
	cli, err := newTestGRPCClient()
	if err != nil {
		t.Fatal(err)
	}
	// 建立一个连接
	_, err = cli.getConn("127.0.0.1:9090")
	if err != nil {
		t.Fatal(err)
	}
	if err := cli.Close(); err != nil {
		t.Fatalf("Close 应成功: %v", err)
	}
	if len(cli.conns) != 0 {
		t.Fatalf("Close 后 conns 应清空，得到 %d", len(cli.conns))
	}
}

func TestClose_Empty(t *testing.T) {
	cli, err := newTestGRPCClient()
	if err != nil {
		t.Fatal(err)
	}
	if err := cli.Close(); err != nil {
		t.Fatalf("Close 空 client 应成功: %v", err)
	}
}

func TestClose_Idempotent(t *testing.T) {
	cli, err := newTestGRPCClient()
	if err != nil {
		t.Fatal(err)
	}
	_ = cli.Close()
	// 二次 Close 不应 panic
	_ = cli.Close()
}

// --- markBalancerFailed 已有测试，补充 RoundRobin 路径 ---

func TestInvoke_WithBalancerSetButAddrsEmpty(t *testing.T) {
	// balancer 非 nil 但实例列表为空 -> Next 返回错误
	cli, err := newTestGRPCClient()
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	fo := discovery.NewFailover(nil)
	cli.SetBalancer(fo)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	err = cli.invoke(ctx, "/x", &struct{}{}, &struct{}{})
	if err == nil {
		t.Fatal("应返回错误")
	}
}

// --- 辅助：生成测试证书 ---

// generateTestCertExtra 生成临时自签证书与私钥 PEM 文件（用于 TLS 测试）。
func generateTestCertExtra(t *testing.T) (certPath, keyPath string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成私钥失败: %v", err)
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
