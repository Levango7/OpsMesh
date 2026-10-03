package config

// license.go 商业授权（Open-Core 企业版闸门）。
//
// 背景：OpsMesh 内核走 Apache-2.0（见 LICENSE），企业版能力走商业授权。
// 若无技术闸门，则「拿到镜像 = 拿到全部企业版功能」——商业模式在技术上不成立。
// 本文件提供**离线**授权校验：不依赖 License Server，私有化客户内网可直接验签。
//
// 设计取舍：
//   - 用 Ed25519 而非 RSA：签名小（64B）、验签快、无参数选择陷阱，私钥仅在签发端存在；
//   - 公钥走配置（--license-public-key）而非编译期内嵌：便于轮换与灰度，
//     且避免"公钥写死在代码里 → 换 key 必须重新编译"的运维负担；
//   - 载荷用 base64url(JSON) + "." + base64url(签名) 的两段式，便于复制粘贴与排障；
//   - **验签失败一律按"未授权"处理**，不区分"签名错"与"格式错"，避免给爆破方反馈信号。
//
// 安全边界（务必知悉）：本机制是**许可控制，不是安全边界**。
// 客户端持有的公钥校验逻辑与二进制都在客户手里，攻击者改二进制即可绕过。
// 它的作用是「让付费能力有明确的授权语义与可审计的凭据」，
// 而不是防破解——防破解需配合服务端校验或硬件信任根，不在当前范围内。

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// LicenseEditionEnterprise 企业版标识。社区版（无 license）为空字符串。
const LicenseEditionEnterprise = "enterprise"

// License 授权载荷。序列化为 JSON 后由厂商私钥签名。
//
// 字段均为可公开信息——授权凭据不含机密，机密在"私钥不离开签发端"这件事上。
type License struct {
	// Edition 版本档位：""（无 license 即社区版）/ "enterprise"。
	Edition string `json:"edition"`
	// Customer 客户标识（用于审计与技术支持定位，不参与验签决策）。
	Customer string `json:"customer"`
	// Devices 授权设备数上限；0 = 不限。
	Devices int `json:"devices"`
	// Expires 到期时间；零值 = 永久授权。
	Expires time.Time `json:"expires"`
	// IssuedAt 签发时间（审计用）。
	IssuedAt time.Time `json:"issuedAt"`
	// Features 授权功能清单；空表示该 edition 下全部功能。
	Features []string `json:"features,omitempty"`
}

// 授权校验的失败原因。用哨兵错误而非字符串，便于调用方 errors.Is 判定，
// 且不会把内部细节透给 HTTP 响应。
var (
	ErrLicenseAbsent    = errors.New("license: not configured")
	ErrLicenseMalformed = errors.New("license: malformed token")
	ErrLicenseSignature = errors.New("license: signature verification failed")
	ErrLicenseExpired   = errors.New("license: expired")
	ErrLicenseEdition   = errors.New("license: edition not granted")
)

// VerifyLicense 校验授权凭据。
//
// token 格式：base64url(payloadJSON) + "." + base64url(ed25519Signature)
// pub 为 Ed25519 公钥；now 传入以便测试可注入。
//
// 返回值语义：
//   - ok=true：授权有效，lic 为解析出的载荷；
//   - ok=false：未授权，err 说明原因。**任何失败都应按"无企业版功能"处理。**
func VerifyLicense(token string, pub ed25519.PublicKey, now time.Time) (*License, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrLicenseAbsent
	}
	if len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: public key size %d, want %d", ErrLicenseMalformed, len(pub), ed25519.PublicKeySize)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, fmt.Errorf("%w: want <payload>.<signature>", ErrLicenseMalformed)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: payload not base64url: %v", ErrLicenseMalformed, err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: signature not base64url: %v", ErrLicenseMalformed, err)
	}
	// 验签覆盖**载荷原始字节**：不重新序列化后比对，避免字段顺序/omitempty 差异导致误判。
	if !ed25519.Verify(pub, payload, sig) {
		return nil, ErrLicenseSignature
	}
	var lic License
	if err := json.Unmarshal(payload, &lic); err != nil {
		return nil, fmt.Errorf("%w: payload not JSON: %v", ErrLicenseMalformed, err)
	}
	if lic.Edition != LicenseEditionEnterprise {
		return nil, fmt.Errorf("%w: edition=%q, want %q", ErrLicenseEdition, lic.Edition, LicenseEditionEnterprise)
	}
	if !lic.Expires.IsZero() && now.After(lic.Expires) {
		return nil, fmt.Errorf("%w: expired at %s", ErrLicenseExpired, lic.Expires.UTC().Format(time.RFC3339))
	}
	return &lic, nil
}

// SignLicense 用私钥签发授权凭据（供厂商侧签发工具调用，不在运行路径上）。
func SignLicense(lic License, priv ed25519.PrivateKey) (string, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("license: private key size %d, want %d", len(priv), ed25519.PrivateKeySize)
	}
	payload, err := json.Marshal(lic)
	if err != nil {
		return "", fmt.Errorf("license: marshal payload: %w", err)
	}
	sig := ed25519.Sign(priv, payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(sig), nil
}

// ParsePublicKey 解析公钥。支持四种来源形式：
//
//  1. PEM 文本（含 "BEGIN PUBLIC KEY" 块，标准 PKIX/SPKI 编码）
//  2. 文件路径（内容为上述 PEM 或裸 base64）
//  3. 裸 base64 或 base64url 的 32 字节原始公钥
//  4. 裸 hex（64 个字符）
//
// 之所以支持裸 base64/hex：Ed25519 公钥就是 32 字节，放进 .env 或 k8s Secret
// 比维护 PEM 块更省事（容器里没有 openssl 也无妨）。
//
// 顺序上**必须先判 PEM 再判路径**：内联 PEM 文本自身就含 "-----BEGIN" 与换行，
// 若先按"看起来像路径"去读文件，内联写法会直接失败（曾如此）。现判据是
// "去掉空白后以 -----BEGIN 开头"才算 PEM 文本，其余含换行/以 .pem 结尾的才当路径。
func ParsePublicKey(raw string) (ed25519.PublicKey, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, errors.New("license: empty public key")
	}
	// 1) 内联 PEM：明确以 -----BEGIN 开头，直接解析，不当路径。
	if strings.HasPrefix(s, "-----BEGIN") {
		return parsePEMPublicKey(s)
	}
	// 2) 文件路径：含换行（多行裸串粘贴）或以 PEM 后缀结尾时才尝试读文件。
	if strings.ContainsAny(s, "\r\n") || strings.HasSuffix(strings.ToLower(s), ".pem") ||
		strings.HasSuffix(strings.ToLower(s), ".pub") {
		b, err := os.ReadFile(s) //nolint:gosec // G703：路径来自运维配置（--license-public-key），与根配置 config 文件读取豁免同款
		if err != nil {
			return nil, fmt.Errorf("license: read public key file: %w", err)
		}
		return ParsePublicKey(strings.TrimSpace(string(b)))
	}
	// 3) 文件路径：像路径（含路径分隔符）且确实存在。
	if strings.ContainsAny(s, `/\`) {
		if b, err := os.ReadFile(s); err == nil { //nolint:gosec // G703 同上：运维配置的路径，非外部用户输入
			return ParsePublicKey(strings.TrimSpace(string(b)))
		}
		// 路径不存在时继续按裸串尝试（slash 可能只是 base64 里的字符）。
	}
	// 4) 裸 base64 / base64url。
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.RawURLEncoding, base64.URLEncoding} {
		if b, err := enc.DecodeString(s); err == nil && len(b) == ed25519.PublicKeySize {
			return ed25519.PublicKey(b), nil
		}
	}
	// 5) 裸 hex（64 个字符）。
	if b, err := hexDecode(s); err == nil && len(b) == ed25519.PublicKeySize {
		return ed25519.PublicKey(b), nil
	}
	return nil, fmt.Errorf("license: cannot parse public key (len=%d); expected PEM, base64(32B) or hex(64)", len(s))
}
