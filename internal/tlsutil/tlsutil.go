// Package tlsutil 提供 gRPC 传输层 TLS / mTLS 凭证的构造助手（P1-6）。
// 内核默认不启用 TLS（仅内网友好网络）；等保三级生产环境建议开启 mTLS：
//   - 控制面：--tls-cert / --tls-key 提供服务端证书；--client-ca 要求客户端持证（RequireAndVerifyClientCert）。
//   - agent：--tls-cert / --tls-key 持证；--client-ca 校验控制面服务端证书。
package tlsutil

import (
	"crypto/tls"
	"crypto/x509"
	"os"

	"google.golang.org/grpc/credentials"
)

// ServerCreds 构造服务端传输凭证。clientCA 非空时启用 mTLS（要求客户端证书）。
func ServerCreds(certFile, keyFile, clientCA string) (credentials.TransportCredentials, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}}
	if clientCA != "" {
		pool := x509.NewCertPool()
		b, err := os.ReadFile(clientCA)
		if err != nil {
			return nil, err
		}
		pool.AppendCertsFromPEM(b)
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return credentials.NewTLS(cfg), nil
}

// ClientCreds 构造客户端传输凭证。certFile/keyFile 为空时仅校验服务端（需 caFile）；
// 全部非空时启用双向 mTLS。
func ClientCreds(certFile, keyFile, caFile string) (credentials.TransportCredentials, error) {
	cfg := &tls.Config{}
	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, err
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	if caFile != "" {
		pool := x509.NewCertPool()
		b, err := os.ReadFile(caFile)
		if err != nil {
			return nil, err
		}
		pool.AppendCertsFromPEM(b)
		cfg.RootCAs = pool
	}
	return credentials.NewTLS(cfg), nil
}
