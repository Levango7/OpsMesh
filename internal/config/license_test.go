package config

// license_test.go 离线授权校验的单元测试。
//
// 覆盖重点（按"错了会导致什么"排序）：
//  1. 伪造签名必须被拒——否则任何人都能自签一个 enterprise 凭据；
//  2. 过期必须被拒——否则授权是一次性的、永久有效；
//  3. 篡改载荷必须被拒——签名覆盖原始字节，改 edition 就要重新签；
//  4. 非企业版 edition 必须被拒——避免把别的档位凭据当成企业版用；
//  5. 公钥解析的四种输入形式——运维实际会用 PEM 文件与裸 base64 两种。

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// marshalPKIX 把 Ed25519 公钥编码为标准 SPKI DER（openssl 产出的就是它）。
func marshalPKIX(pub ed25519.PublicKey) ([]byte, error) {
	return x509.MarshalPKIXPublicKey(pub)
}

// fixedNow 固定"当前时间"，避免测试随真实时钟漂移。
func fixedNow() time.Time {
	return time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
}

func testKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("生成测试密钥对失败: %v", err)
	}
	return pub, priv
}

func validLicense() License {
	return License{
		Edition:  LicenseEditionEnterprise,
		Customer: "acme-corp",
		Devices:  100,
		Expires:  fixedNow().AddDate(1, 0, 0),
		IssuedAt: fixedNow(),
	}
}

// TestVerifyLicense_Valid 正常凭据应通过并回填全部字段。
func TestVerifyLicense_Valid(t *testing.T) {
	pub, priv := testKeyPair(t)
	want := validLicense()
	tok, err := SignLicense(want, priv)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	got, err := VerifyLicense(tok, pub, fixedNow())
	if err != nil {
		t.Fatalf("合法凭据应通过校验；got err=%v", err)
	}
	if got.Edition != LicenseEditionEnterprise {
		t.Fatalf("edition 应为 enterprise；got=%q", got.Edition)
	}
	if got.Customer != "acme-corp" || got.Devices != 100 {
		t.Fatalf("载荷字段未正确回填；got=%+v", got)
	}
	if !got.Expires.Equal(want.Expires) {
		t.Fatalf("expires 不一致；got=%v want=%v", got.Expires, want.Expires)
	}
}

// TestVerifyLicense_PermanentExpiry 零值 Expires = 永久授权，不应被判过期。
func TestVerifyLicense_PermanentExpiry(t *testing.T) {
	pub, priv := testKeyPair(t)
	lic := validLicense()
	lic.Expires = time.Time{}
	tok, err := SignLicense(lic, priv)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if _, err := VerifyLicense(tok, pub, fixedNow()); err != nil {
		t.Fatalf("零值 Expires 应视为永久授权；got err=%v", err)
	}
	// 永久授权在很久以后仍然有效。
	if _, err := VerifyLicense(tok, pub, fixedNow().AddDate(50, 0, 0)); err != nil {
		t.Fatalf("永久授权不应因时间流逝失效；got err=%v", err)
	}
}

// TestVerifyLicense_ForgedSignature 用**别人的私钥**签发：验签必须失败。
// 这是最关键的一条——若通过，任何人都能自签企业版凭据。
func TestVerifyLicense_ForgedSignature(t *testing.T) {
	attackerPub, attackerPriv := testKeyPair(t)
	_, vendorPriv := testKeyPair(t) // 厂商密钥，与攻击者完全无关
	vendorPub := vendorPriv.Public().(ed25519.PublicKey)

	tok, err := SignLicense(validLicense(), attackerPriv)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	// 用攻击者自己的公钥验签应通过（说明签发本身正常，排除"凭据格式写错"这一变量）。
	if _, err := VerifyLicense(tok, attackerPub, fixedNow()); err != nil {
		t.Fatalf("攻击者公钥验自己签的凭据应通过；got err=%v", err)
	}
	// 厂商公钥验签必须失败。
	if _, err := VerifyLicense(tok, vendorPub, fixedNow()); err == nil {
		t.Fatal("异私钥签发的凭据必须被拒绝（否则授权形同虚设）")
	}
}

// TestVerifyLicense_TamperedPayload 篡改载荷（edition 改 enterprise）但保留原签名 → 必须被拒。
// 若实现改成"验签后重新序列化比对"或"只解码不验签"，这条会挂。
func TestVerifyLicense_TamperedPayload(t *testing.T) {
	pub, priv := testKeyPair(t)
	tok, err := SignLicense(validLicense(), priv)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 2 {
		t.Fatalf("凭据格式应为两段；got=%q", tok)
	}
	// 把载荷里的 edition 改成 enterprise 但不动签名（本题载荷本来就是 enterprise，
	// 故改为篡改 customer 与 devices，模拟"改内容沿用旧签名"）。
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("载荷不是 base64url: %v", err)
	}
	tampered := strings.Replace(string(raw), `"customer":"acme-corp"`, `"customer":"evil-corp"`, 1)
	tampered = strings.Replace(tampered, `"devices":100`, `"devices":999999`, 1)
	bad := base64.RawURLEncoding.EncodeToString([]byte(tampered)) + "." + parts[1]
	if _, err := VerifyLicense(bad, pub, fixedNow()); err == nil {
		t.Fatal("篡改载荷后必须验签失败")
	}
}

// TestVerifyLicense_Expired 过期凭据必须被拒，且错误可被 errors.Is 识别。
func TestVerifyLicense_Expired(t *testing.T) {
	pub, priv := testKeyPair(t)
	lic := validLicense()
	lic.Expires = fixedNow().Add(-time.Hour)
	tok, err := SignLicense(lic, priv)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	_, err = VerifyLicense(tok, pub, fixedNow())
	if err == nil {
		t.Fatal("已过期凭据必须被拒绝")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Fatalf("错误应说明过期原因；got=%v", err)
	}
}

// TestVerifyLicense_WrongEdition 非 enterprise 档位的凭据不得当作企业版。
func TestVerifyLicense_WrongEdition(t *testing.T) {
	pub, priv := testKeyPair(t)
	lic := validLicense()
	lic.Edition = "community-plus"
	tok, err := SignLicense(lic, priv)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if _, err := VerifyLicense(tok, pub, fixedNow()); err == nil {
		t.Fatal("非 enterprise edition 必须被拒绝")
	}
}

// TestVerifyLicense_Malformed 各类格式错误都应被拒，且不 panic。
func TestVerifyLicense_Malformed(t *testing.T) {
	pub, _ := testKeyPair(t)
	cases := []struct {
		name  string
		token string
	}{
		{"空串", ""},
		{"纯空白", "   \n\t "},
		{"无分隔符", "abcdef"},
		{"三段", "aaa.bbb.ccc"},
		{"载荷非 base64url", "!!!.###"},
		{"签名非 base64url", base64.RawURLEncoding.EncodeToString([]byte(`{"edition":"enterprise"}`)) + ".###"},
		{"合法载荷但签名长度不对", base64.RawURLEncoding.EncodeToString([]byte(`{"edition":"enterprise"}`)) + "." + base64.RawURLEncoding.EncodeToString([]byte("short"))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := VerifyLicense(c.token, pub, fixedNow()); err == nil {
				t.Fatalf("%s 应被拒绝为非法格式", c.name)
			}
		})
	}
}

// TestVerifyLicense_BadPublicKeySize 公钥长度不对时直接拒，不进入验签（避免 panic）。
func TestVerifyLicense_BadPublicKeySize(t *testing.T) {
	_, priv := testKeyPair(t)
	tok, err := SignLicense(validLicense(), priv)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if _, err := VerifyLicense(tok, ed25519.PublicKey("too-short"), fixedNow()); err == nil {
		t.Fatal("公钥长度非法时必须被拒绝")
	}
}

// TestSignLicense_BadPrivateKey 私钥长度非法时签发应报错（不产生无效凭据）。
func TestSignLicense_BadPrivateKey(t *testing.T) {
	if _, err := SignLicense(validLicense(), ed25519.PrivateKey("short")); err == nil {
		t.Fatal("私钥长度非法时签发应失败")
	}
}

// TestParsePublicKey_AllForms 公钥解析须支持运维实际会用的四种形式。
func TestParsePublicKey_AllForms(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	// 1) 裸 base64（标准编码，含 padding）
	forms := map[string]string{
		"base64Std": base64.StdEncoding.EncodeToString(pub),
		"base64Raw": base64.RawStdEncoding.EncodeToString(pub),
		"base64url": base64.RawURLEncoding.EncodeToString(pub),
		"hex":       hex.EncodeToString(pub),
	}
	for name, raw := range forms {
		t.Run(name, func(t *testing.T) {
			got, err := ParsePublicKey(raw)
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if !got.Equal(pub) {
				t.Fatalf("解析出的公钥与原公钥不一致")
			}
		})
	}

	// 2) PEM（PKIX/SPKI 标准格式，openssl genpkey 产出的就是它）
	der, err := marshalPKIX(pub)
	if err != nil {
		t.Fatalf("SPKI 编码失败: %v", err)
	}
	pemText := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	t.Run("pemInline", func(t *testing.T) {
		got, err := ParsePublicKey(pemText)
		if err != nil {
			t.Fatalf("PEM 解析失败: %v", err)
		}
		if !got.Equal(pub) {
			t.Fatalf("PEM 解析出的公钥不一致")
		}
	})

	// 3) PEM 文件路径（容器里最常见：k8s Secret 挂载文件）
	path := filepath.Join(t.TempDir(), "license.pub")
	if err := os.WriteFile(path, []byte(pemText), 0o600); err != nil {
		t.Fatalf("写公钥文件失败: %v", err)
	}
	t.Run("pemFile", func(t *testing.T) {
		got, err := ParsePublicKey(path)
		if err != nil {
			t.Fatalf("公钥文件解析失败: %v", err)
		}
		if !got.Equal(pub) {
			t.Fatalf("公钥文件解析结果不一致")
		}
	})

	// 4) 裸 base64 的文件（某些发行版只存裸串）
	barePath := filepath.Join(t.TempDir(), "license.b64")
	if err := os.WriteFile(barePath, []byte(base64.StdEncoding.EncodeToString(pub)+"\n"), 0o600); err != nil {
		t.Fatalf("写裸公钥文件失败: %v", err)
	}
	t.Run("bareFile", func(t *testing.T) {
		got, err := ParsePublicKey(barePath)
		if err != nil {
			t.Fatalf("裸公钥文件解析失败: %v", err)
		}
		if !got.Equal(pub) {
			t.Fatalf("裸公钥文件解析结果不一致")
		}
	})
}

// TestParsePublicKey_Errors 各种非法输入应报错而非 panic 或返回错误长度密钥。
func TestParsePublicKey_Errors(t *testing.T) {
	cases := map[string]string{
		"空串":        "",
		"空白":        "   ",
		"非密钥文本":     "not-a-key",
		"长度不对的hex":  hex.EncodeToString(make([]byte, 16)),
		"不存在的文件":    filepath.Join(t.TempDir(), "missing.pub"),
		"PEM块但内容非法": "-----BEGIN PUBLIC KEY-----\nZm9v\n-----END PUBLIC KEY-----\n",
		"路径存在但无权限":  filepath.Join(os.TempDir(), "\x00invalid"),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ParsePublicKey(raw)
			if err == nil {
				t.Fatalf("%s 应解析失败，实际返回了 %d 字节密钥", name, len(got))
			}
			if got != nil {
				t.Fatalf("失败时必须返回 nil 密钥；got=%v", got)
			}
		})
	}
}

// TestVerifyLicense_EdgePublicKeyWrongBit 换一个**合法长度但不同**的公钥验签必须失败。
// 覆盖"公钥长度对但内容错"这条路径（配置串错一位字符的常见场景）。
func TestVerifyLicense_EdgePublicKeyWrongBit(t *testing.T) {
	_, priv := testKeyPair(t)
	tok, err := SignLicense(validLicense(), priv)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	wrong, _ := testKeyPair(t)
	if _, err := VerifyLicense(tok, wrong, fixedNow()); err == nil {
		t.Fatal("错误公钥验签必须失败")
	}
}
