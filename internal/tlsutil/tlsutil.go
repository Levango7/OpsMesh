// Package tlsutil 提供 gRPC 传输层 TLS / mTLS 凭证的构造助手。
// 内核默认不启用 TLS（仅内网友好网络）；等保三级生产环境建议开启 mTLS：
//   - 控制面：--tls-cert / --tls-key 提供服务端证书；--client-ca 要求客户端持证（RequireAndVerifyClientCert）。
//   - agent：--tls-cert / --tls-key 持证；--client-ca 校验控制面服务端证书。
package tlsutil

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"google.golang.org/grpc/credentials"
)

// ServerCreds 构造服务端传输凭证。clientCA 非空时启用 mTLS（要求客户端证书）。
func ServerCreds(certFile, keyFile, clientCA string) (credentials.TransportCredentials, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	// 强制 TLS 1.2+，禁用 SSLv3/TLSv1.0/TLSv1.1 等弱协议版本。
	// 不显式设置 CipherSuites，保留 Go 默认强套件（Go 1.17+ 默认已排除不安全套件）。
	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
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
	// 客户端同样强制 TLS 1.2+，与服务端策略一致。
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
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

// HTTPClientTLSConfig 构造 HTTP 客户端 mTLS TLS 配置（联邦通道硬化）：
// 若 certFile/keyFile 非空则呈现客户端证书（双向 mTLS）；若 caFile 非空则校验证书链（防 MITM/伪 peer）。
// 三者皆空返回 (nil, nil) 表示不启用 TLS（向后兼容明文联邦）。
func HTTPClientTLSConfig(certFile, keyFile, caFile string) (*tls.Config, error) {
	if certFile == "" && keyFile == "" && caFile == "" {
		return nil, nil
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("load federation client cert: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	if caFile != "" {
		pool := x509.NewCertPool()
		b, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read federation CA: %w", err)
		}
		if !pool.AppendCertsFromPEM(b) {
			return nil, fmt.Errorf("parse federation CA failed")
		}
		cfg.RootCAs = pool
	}
	return cfg, nil
}

// HTTPServerTLSConfig 构造 HTTP 服务端 TLS 配置（联邦 mTLS 监听）：
// 服务端证书必备；clientCA 非空时要求对端持证（RequireAndVerifyClientCert），实现联邦双向认证。
func HTTPServerTLSConfig(certFile, keyFile, clientCA string) (*tls.Config, error) {
	if certFile == "" || keyFile == "" {
		return nil, fmt.Errorf("federation server requires cert and key")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	if clientCA != "" {
		pool := x509.NewCertPool()
		b, err := os.ReadFile(clientCA)
		if err != nil {
			return nil, err
		}
		if !pool.AppendCertsFromPEM(b) {
			return nil, fmt.Errorf("parse federation client CA failed")
		}
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return cfg, nil
}
