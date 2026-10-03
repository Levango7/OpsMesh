// license-ci —— CI 用的一次性商业授权凭据生成器。
//
// 用途：授权门禁（internal/config/license.go + controlplane/license_gate.go）落地后，
// CI 需要在**已授权**状态下验证企业版前端与 API 的完整交付路径（否则 frontend job
// 的黑盒验收只会拿到未授权说明页）。本程序在运行时生成一对**临时** Ed25519 密钥、
// 签一张 1 小时有效的 enterprise 授权，把公钥与凭据写到指定文件。
//
// 为什么不在仓库里放一对固定测试密钥：任何提交进仓库的私钥都会被 secret 扫描
// 报告，也会被误当成真实签发密钥。运行时生成 = 仓库零私钥、每次 CI 都是新密钥。
// 真实签发私钥永远只存在于签发端（见 docs/license-decision-2026-10-03.md）。
//
// 用法：
//
//	go run ./deploy/scripts/license-ci -out-dir /tmp/opsmesh-license
//	# 产出：/tmp/opsmesh-license/public.key（base64 公钥）
//	#      /tmp/opsmesh-license/license.key（base64url(payload).base64url(sig)）
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// payload 必须与 config.License 的 JSON 契约逐字段对齐：
// expires/issuedAt 为 time.Time（RFC3339 字符串），标签是 issuedAt（非 issued_at）。
// 字段名/类型任一对不上，VerifyLicense 的 json.Unmarshal 即失败并报「格式非法」。
type payload struct {
	Edition  string    `json:"edition"`
	Customer string    `json:"customer"`
	Devices  int       `json:"devices"`
	IssuedAt time.Time `json:"issuedAt"`
	Expires  time.Time `json:"expires"`
}

func main() {
	outDir := flag.String("out-dir", "", "输出目录（必填）")
	customer := flag.String("customer", "ci@opsmesh.local", "授权客户标识（仅审计用）")
	ttl := flag.Duration("ttl", time.Hour, "授权有效期")
	flag.Parse()
	if *outDir == "" {
		fmt.Fprintln(os.Stderr, "license-ci: -out-dir 必填")
		os.Exit(1)
	}

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "license-ci: 生成密钥失败: %v\n", err)
		os.Exit(1)
	}
	now := time.Now()
	body, err := json.Marshal(payload{
		Edition:  "enterprise",
		Customer: *customer,
		Devices:  0, // 0 = 不限
		IssuedAt: now,
		Expires:  now.Add(*ttl),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "license-ci: 序列化载荷失败: %v\n", err)
		os.Exit(1)
	}
	enc := base64.RawURLEncoding
	token := enc.EncodeToString(body) + "." + enc.EncodeToString(ed25519.Sign(priv, body))

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "license-ci: 建目录失败: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(filepath.Join(*outDir, "public.key"), []byte(enc.EncodeToString(pub)), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "license-ci: 写公钥失败: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(filepath.Join(*outDir, "license.key"), []byte(token), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "license-ci: 写凭据失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("LICENSE_CI_READY")
}
