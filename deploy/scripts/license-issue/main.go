// license-issue —— 厂商侧**真实**商业授权凭据签发器。
//
// 与 deploy/scripts/license-ci 的分工（两者不可互换，别合并）：
//
//	┌─────────────────┬──────────────────────────┬────────────────────────────┐
//	│                 │ license-ci               │ license-issue（本程序）     │
//	├─────────────────┼──────────────────────────┼────────────────────────────┤
//	│ 用途            │ CI 冒烟（临时密钥）      │ 给客户签发真实授权         │
//	│ 密钥来源        │ 每次运行时新生成         │ 既有厂商私钥文件           │
//	│ 私钥是否留存    │ 否（用完即弃）           │ 是（厂商侧唯一资产）       │
//	│ 有效期          │ 默认 1 小时              │ 默认 1 年 / 可永久         │
//	└─────────────────┴──────────────────────────┴────────────────────────────┘
//
// 为什么必须另开一个而不是扩展 license-ci：license-ci 的核心约束是「仓库零私钥」，
// 它**永远**现生成密钥、绝不读私钥文件。若给它加上读私钥的能力，就等于在 CI 里
// 开了一条接触真实签发私钥的路径——CI 日志与构件都可能把它带出去。
// 两个程序各自单职责，风险面互不叠加。
//
// 本程序复用 config.SignLicense 而非自行拼载荷：授权契约只有一处定义，
// config.License 加字段时不会两边漂移（license-ci 因「不进仓库依赖」的取舍
// 保留了自带 payload，改动 config.License 时需同步改它，见该文件顶部注释）。
//
// 用法：
//
//	# 1) 首次投产：生成厂商密钥对（私钥务必妥善保管，丢失即无法续签）
//	go run ./deploy/scripts/license-issue -gen-key -out-dir ./vendor-keys
//
//	# 2) 给客户签一年期授权（100 台设备）
//	go run ./deploy/scripts/license-issue \
//	    -priv-key ./vendor-keys/private.pem \
//	    -customer "acme-corp" -devices 100 -ttl 8760h
//
//	# 3) 签永久授权（不设到期；用于买断型客户）
//	go run ./deploy/scripts/license-issue -priv-key ./vendor-keys/private.pem \
//	    -customer "acme-corp" -perpetual
//
//	产出（-out-dir，默认当前目录）：
//	  license.key   —— 交给客户的凭据，经 --license-key 传入
//	  public.key    —— 随版本分发给客户，经 --license-public-key 传入
//	  private.pem   —— 仅 -gen-key 时产出；**绝不离开厂商侧**
package main

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
)

func main() {
	privPath := flag.String("priv-key", "", "厂商私钥文件（PKCS#8 PEM 或裸 base64）；-gen-key 时不填")
	genKey := flag.Bool("gen-key", false, "生成新的厂商密钥对（首次投产用）")
	outDir := flag.String("out-dir", ".", "输出目录")
	customer := flag.String("customer", "", "客户标识（写入载荷，仅审计与技术支持定位用）")
	devices := flag.Int("devices", 0, "授权设备数上限；0 = 不限")
	ttl := flag.Duration("ttl", 365*24*time.Hour, "授权有效期")
	perpetual := flag.Bool("perpetual", false, "永久授权（不设 expires）；优先于 -ttl")
	features := flag.String("features", "", "授权功能清单（逗号分隔）；空=该档位下全部功能")
	flag.Parse()

	if err := run(*genKey, *privPath, *outDir, *customer, *devices, *ttl, *perpetual, *features); err != nil {
		fmt.Fprintf(os.Stderr, "license-issue: %v\n", err)
		os.Exit(1)
	}
}

func run(genKey bool, privPath, outDir, customer string, devices int, ttl time.Duration, perpetual bool, features string) error {
	if !genKey && privPath == "" {
		return errors.New("需指定 -priv-key（已有厂商私钥）或 -gen-key（首次生成）；两者都不给则无法签发")
	}
	if !genKey && customer == "" {
		// 空客户标识不会导致验签失败，但会让后续审计与支持定位无从下手——
		// 一张不知道发给谁的授权在续期/纠纷时是废纸，故强制填写。
		return errors.New("-customer 必填：授权凭据要能回溯到客户")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("建输出目录失败: %w", err)
	}

	var (
		priv ed25519.PrivateKey
		pub  ed25519.PublicKey
	)
	if genKey {
		var err error
		pub, priv, err = ed25519.GenerateKey(nil)
		if err != nil {
			return fmt.Errorf("生成密钥对失败: %w", err)
		}
		if err := writePrivateKey(filepath.Join(outDir, "private.pem"), priv); err != nil {
			return err
		}
		fmt.Println("license-issue: 已生成厂商密钥对 → private.pem（**绝不分发**）")
	} else {
		p, err := readPrivateKey(privPath)
		if err != nil {
			return err
		}
		priv = p
		pub = p.Public().(ed25519.PublicKey)
	}

	if err := writePublicKey(filepath.Join(outDir, "public.key"), pub); err != nil {
		return err
	}

	lic := config.License{
		Edition:  config.LicenseEditionEnterprise,
		Customer: customer,
		Devices:  devices,
		IssuedAt: time.Now().UTC(),
	}
	// 永久授权 = 零值 Expires（VerifyLicense 对零值不做过期判定）。
	// 不用"一百年后到期"的写法：那只是把永久伪装成有限期，且 RFC3339 序列化后
	// 会在客户界面上显示一个荒谬的到期年份。
	if !perpetual {
		lic.Expires = lic.IssuedAt.Add(ttl)
	}
	if features != "" {
		lic.Features = splitComma(features)
	}

	token, err := config.SignLicense(lic, priv)
	if err != nil {
		return fmt.Errorf("签发失败: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "license.key"), []byte(token), 0o600); err != nil {
		return fmt.Errorf("写凭据失败: %w", err)
	}

	// 自校验：签完立刻用同一份公钥验一遍。
	// 这是把"签发错误"挡在交付之前——客户拿到一张验不过的凭据，
	// 现场只会看到「授权凭据签名校验失败」，无法区分是自己配错还是厂商签错。
	if _, err := config.VerifyLicense(token, pub, time.Now()); err != nil {
		return fmt.Errorf("自校验失败（凭据未交付，请排查）: %w", err)
	}

	fmt.Printf("license-issue: 已签发 customer=%s devices=%d expires=%s\n",
		customer, devices, expiresText(lic.Expires, perpetual))
	fmt.Printf("license-issue: 产出 %s\n", outDir)
	return nil
}

// writePrivateKey 以 PKCS#8 PEM 落盘私钥（0600）。
// 选 PKCS#8 而非裸 base64：openssl/Vault/KMS 都能直接读，不锁死在自家格式上。
func writePrivateKey(path string, priv ed25519.PrivateKey) error {
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return fmt.Errorf("PKCS#8 编码私钥失败: %w", err)
	}
	block := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if block == nil {
		return errors.New("PEM 编码私钥失败")
	}
	// 0600：私钥只服务进程/管理员自身，绝不可被同主机其他账号读取。
	return os.WriteFile(path, block, 0o600)
}

// readPrivateKey 读私钥，优先 PKCS#8 PEM，回退裸 base64。
func readPrivateKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读私钥文件失败: %w", err)
	}
	s := string(raw)
	if blk, _ := pem.Decode(raw); blk != nil {
		parsed, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
		if err != nil {
			return nil, fmt.Errorf("解析 PKCS#8 私钥失败: %w", err)
		}
		priv, ok := parsed.(ed25519.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("私钥类型 %T，期望 ed25519.PrivateKey", parsed)
		}
		return priv, nil
	}
	// 裸 base64（64 字节）：与 config.ParsePublicKey 的裸串支持对称。
	b, err := base64.RawURLEncoding.DecodeString(trimSpace(s))
	if err != nil {
		b, err = base64.StdEncoding.DecodeString(trimSpace(s))
		if err != nil {
			return nil, errors.New("私钥既非 PEM 也非 base64；请用 -gen-key 生成的 private.pem")
		}
	}
	if len(b) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("私钥长度 %d，期望 %d 字节", len(b), ed25519.PrivateKeySize)
	}
	return ed25519.PrivateKey(b), nil
}

// writePublicKey 写 base64url 公钥（与 --license-public-key 的输入形式一致）。
func writePublicKey(path string, pub ed25519.PublicKey) error {
	s := base64.RawURLEncoding.EncodeToString(pub)
	return os.WriteFile(path, []byte(s), 0o644)
}

func splitComma(s string) []string {
	out := []string{}
	cur := ""
	for _, r := range s {
		if r == ',' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func expiresText(t time.Time, perpetual bool) string {
	if perpetual {
		return "永久"
	}
	return t.Format(time.RFC3339)
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\n' || s[start] == '\r' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\n' || s[end-1] == '\r' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
