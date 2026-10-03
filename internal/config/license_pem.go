package config

// license_pem.go 授权公钥的 PEM 解析。与 license.go 拆开是因为它只被 ParsePublicKey 调用一次，
// 单独成文件让 license.go 保持在"授权语义"这一层，不掺杂编码细节。

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"strings"
)

// parsePEMPublicKey 解析 PKIX/SPKI PEM 公钥块。
func parsePEMPublicKey(s string) (ed25519.PublicKey, error) {
	block, _ := pem.Decode([]byte(s))
	if block == nil {
		return nil, fmt.Errorf("license: not PEM")
	}
	if block.Type != "PUBLIC KEY" {
		return nil, fmt.Errorf("license: PEM block type %q, want PUBLIC KEY", block.Type)
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("license: parse PKIX: %w", err)
	}
	pub, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("license: PEM holds %T, want ed25519.PublicKey", parsed)
	}
	return pub, nil
}

// hexDecode 解析十六进制串（容忍 0x 前缀与大小写）。
func hexDecode(s string) ([]byte, error) {
	s = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(s), "0x"), "0X")
	return hex.DecodeString(s)
}
