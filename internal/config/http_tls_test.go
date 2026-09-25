// Web/REST 监听协议（--http-tls）的加载与校验测试。
//
// 背景：--tls-cert/--tls-key 原本只作用于 gRPC，Web/REST（B/S 端口）始终明文，
// 而部署文档宣称「--tls-cert 启用 HTTP TLS」。--http-tls 让该声明成为事实：
//
//	auto（默认）— 证书齐备即 HTTPS；
//	on          — 强制 HTTPS，缺证书拒绝启动；
//	off         — 始终明文（仅限上游反代终止 TLS 的部署）。
package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoad_HTTPTLSDefaultsToAuto(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	if got := loadForTest().HTTPTLS; got != "auto" {
		t.Fatalf("HTTPTLS 默认值 = %q, want auto", got)
	}
}

func TestLoad_HTTPTLSFromFlagAndEnv(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()

	if got := loadForTest("--http-tls=on").HTTPTLS; got != "on" {
		t.Fatalf("--http-tls=on → HTTPTLS = %q", got)
	}
	// 环境变量与大小写归一化：OPSMESH_HTTP_TLS=OFF 应被识别为 off。
	if err := os.Setenv("OPSMESH_HTTP_TLS", "OFF"); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	if got := loadForTest().HTTPTLS; got != "off" {
		t.Fatalf("OPSMESH_HTTP_TLS=OFF → HTTPTLS = %q, want off（应小写归一化）", got)
	}
	// 显式 flag 优先于 env。
	if got := loadForTest("--http-tls=on").HTTPTLS; got != "on" {
		t.Fatalf("flag 应优先于 env: HTTPTLS = %q", got)
	}
}

func TestValidate_HTTPTLSIllegalValueRejected(t *testing.T) {
	c := base()
	c.HTTPTLS = "yes-please"
	err := c.Validate()
	if err == nil {
		t.Fatal("非法 --http-tls 应被拒绝")
	}
	if !strings.Contains(err.Error(), "--http-tls") {
		t.Fatalf("错误消息应提示 --http-tls, got %q", err.Error())
	}
}

func TestValidate_HTTPTLSOnRequiresCertAndKey(t *testing.T) {
	// --http-tls=on 但无证书：拒绝（否则会静默退回明文，与声明不符）。
	c := base()
	c.HTTPTLS = "on"
	if err := c.Validate(); err == nil {
		t.Fatal("--http-tls=on 无证书应被拒绝")
	}
	// 只有证书没有私钥：同样拒绝。
	c2 := base()
	c2.HTTPTLS = "on"
	c2.TLSCert = "tls.crt"
	if err := c2.Validate(); err == nil {
		t.Fatal("--http-tls=on 缺少 --tls-key 应被拒绝")
	}
	// 证书 + 私钥齐备：通过。
	c3 := base()
	c3.HTTPTLS = "on"
	c3.TLSCert = "tls.crt"
	c3.TLSKey = "tls.key"
	if err := c3.Validate(); err != nil {
		t.Fatalf("--http-tls=on 配齐证书应通过: %v", err)
	}
}

func TestValidate_HTTPTLSOffAllowedInProduction(t *testing.T) {
	// 上游 Ingress/Nginx 终止 TLS 是合法生产架构：显式 off 放行（启动期输出告警）。
	c := base()
	prodReadyDefaults(c)
	c.HTTPTLS = "off"
	if err := c.Validate(); err != nil {
		t.Fatalf("生产 + --http-tls=off 应放行（由启动告警提示风险）: %v", err)
	}
}

func TestValidate_HTTPTLSEmptyTreatedAsAuto(t *testing.T) {
	// 程序化构造 Config（不设 HTTPTLS）等价 auto：保持向后兼容，
	// 且 auto 在有证书时仍是 HTTPS，不存在静默降级。
	c := base()
	c.HTTPTLS = ""
	if err := c.Validate(); err != nil {
		t.Fatalf("空 HTTPTLS 应按 auto 处理: %v", err)
	}
}

func TestValidate_ProductionCertWithoutKeyRejected(t *testing.T) {
	// 生产模式只有证书没有私钥：gRPC/Web 都无法启用 TLS，
	// 若不拒绝则运维会误以为已加密（比不配证书更危险）。
	c := base()
	c.Production = true
	c.TLSCert = "tls.crt"
	c.TLSKey = ""
	c.JWTSecret = "0123456789abcdef0123456789abcdef"
	c.EncryptionKey = "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA="
	err := c.Validate()
	if err == nil {
		t.Fatal("生产 + 有证书无私钥应被拒绝")
	}
	if !strings.Contains(err.Error(), "--tls-key") {
		t.Fatalf("错误消息应提示 --tls-key, got %q", err.Error())
	}
}
