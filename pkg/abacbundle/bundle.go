// Package abacbundle 定义 ABAC 策略包的签名/验签与确定性编码（Ed25519，仅标准库）。
//
// # 契约
//
// 本包是策略包格式的**唯一权威实现**：控制面签发侧与服务端验签侧都只依赖这里，
// 避免「边写边改」导致 canonical bytes 两边不一致——签名覆盖的是规范化字节，
// 任何一方序列化差异都等价于验签失败。
//
// # 规范化（CanonicalJSON）
//
// 签名输入不是 encoding/json 的产物，而是本包的 canonical 形态：
//   - 字段顺序固定为结构体声明顺序（schemaVersion → … → policies）；
//   - Conditions 为 map，**按 key 字节序排序**后输出，绝不依赖 Go map 迭代序；
//   - 无空白；时间统一折算到 UTC 并以 RFC3339Nano 输出；
//   - 空 Conditions 省略；空/ nil Policies 输出为 []。
//
// 因此同一个 Bundle（无论经 JSON 往返多少次）规范化后逐字节一致。
//
// # 错误分类
//
// 验签失败一律返回可 errors.Is 判定的哨兵错误（ErrExpired / ErrNotYetValid /
// ErrBadSignature / ErrUnsupportedSchema / ErrTooManyPolicies / ErrBundleTooLarge /
// ErrUnknownKey / ErrInvalidBundle / ErrInvalidKey），不做「失败当通过」的降级。
package abacbundle

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"
)

const (
	// SchemaVersion 本包唯一支持的策略包 schema 版本。
	SchemaVersion = 1
	// MaxPolicies 单个策略包允许的策略条数上限。
	MaxPolicies = 1000
	// MaxBundleBytes 单个策略包（canonical 字节或编码后 JSON）的大小上限。
	MaxBundleBytes = 1 << 20
	// DefaultTTL 策略包默认有效期（签发方参考值，本包不隐式套用）。
	DefaultTTL = 24 * time.Hour
)

// maxClockSkew 允许签发时间超前本机时钟的容差；超过即视为「尚未生效」。
const maxClockSkew = 5 * time.Minute

// 错误分类。所有错误都可用 errors.Is 判定。
var (
	// ErrExpired 策略包已过期（now 晚于 ExpiresAt）。
	ErrExpired = errors.New("abacbundle: bundle expired")
	// ErrNotYetValid 策略包签发时间超前本机时钟超过允许偏移。
	ErrNotYetValid = errors.New("abacbundle: bundle not yet valid")
	// ErrBadSignature 签名缺失、格式错误或验签不通过。
	ErrBadSignature = errors.New("abacbundle: bad signature")
	// ErrUnsupportedSchema schemaVersion 不是受支持的值。
	ErrUnsupportedSchema = errors.New("abacbundle: unsupported schema version")
	// ErrTooManyPolicies 策略条数超过 MaxPolicies。
	ErrTooManyPolicies = errors.New("abacbundle: too many policies")
	// ErrBundleTooLarge 策略包超过 MaxBundleBytes。
	ErrBundleTooLarge = errors.New("abacbundle: bundle too large")
	// ErrUnknownKey KeySet 中不存在 bundle.KeyID 对应的公钥。
	ErrUnknownKey = errors.New("abacbundle: unknown key id")
	// ErrInvalidBundle 结构非法（空 bundleID / keyID、issuedAt 晚于 expiresAt 等）。
	ErrInvalidBundle = errors.New("abacbundle: invalid bundle")
	// ErrInvalidKey 密钥长度非法（Ed25519 私钥 64 字节、公钥 32 字节）。
	ErrInvalidKey = errors.New("abacbundle: invalid key")
)

// Policy 单条策略。Subject 与控制面 abacPolicy 同一 JSON 字符串编码。
type Policy struct {
	ID         string            `json:"id"`
	Subject    string            `json:"subject"`
	Resource   string            `json:"resource"`
	Action     string            `json:"action"`
	Effect     string            `json:"effect"`
	Conditions map[string]string `json:"conditions,omitempty"`
}

// Bundle 待签名的策略包。SchemaVersion 恒为 SchemaVersion。
type Bundle struct {
	SchemaVersion int       `json:"schemaVersion"`
	BundleID      string    `json:"bundleID"`
	TenantScope   string    `json:"tenantScope"`
	KeyID         string    `json:"keyId"`
	IssuedAt      time.Time `json:"issuedAt"`
	ExpiresAt     time.Time `json:"expiresAt"`
	Policies      []Policy  `json:"policies"`
}

// SignedBundle 签名后的策略包。Signature 为 canonical Bundle 字节上的 Ed25519 签名（base64 std）。
type SignedBundle struct {
	Bundle    Bundle `json:"bundle"`
	Signature string `json:"signature"`
}

// validate 做结构校验（不含时间窗口的有效性判断）。
func validate(b Bundle) error {
	if b.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: got %d want %d", ErrUnsupportedSchema, b.SchemaVersion, SchemaVersion)
	}
	if b.BundleID == "" {
		return fmt.Errorf("%w: empty bundleID", ErrInvalidBundle)
	}
	if b.KeyID == "" {
		return fmt.Errorf("%w: empty keyId", ErrInvalidBundle)
	}
	if len(b.Policies) > MaxPolicies {
		return fmt.Errorf("%w: %d > %d", ErrTooManyPolicies, len(b.Policies), MaxPolicies)
	}
	if b.IssuedAt.After(b.ExpiresAt) {
		return fmt.Errorf("%w: issuedAt after expiresAt", ErrInvalidBundle)
	}
	return nil
}

// CanonicalJSON 返回确定性序列化结果：字段顺序固定、Conditions 排序、无空白。
// 支持 Bundle、*Bundle、Policy、*Policy；其他类型返回 ErrInvalidBundle。
func CanonicalJSON(v any) ([]byte, error) {
	switch t := v.(type) {
	case Bundle:
		return canonicalBundle(t)
	case *Bundle:
		if t == nil {
			return nil, fmt.Errorf("%w: nil *Bundle", ErrInvalidBundle)
		}
		return canonicalBundle(*t)
	case Policy:
		var buf bytes.Buffer
		if err := writePolicy(&buf, t); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	case *Policy:
		if t == nil {
			return nil, fmt.Errorf("%w: nil *Policy", ErrInvalidBundle)
		}
		return CanonicalJSON(*t)
	default:
		return nil, fmt.Errorf("%w: cannot canonicalize %T", ErrInvalidBundle, v)
	}
}

func canonicalBundle(b Bundle) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`{"schemaVersion":`)
	buf.WriteString(strconv.Itoa(b.SchemaVersion))
	if err := writeFieldString(&buf, "bundleID", b.BundleID); err != nil {
		return nil, err
	}
	if err := writeFieldString(&buf, "tenantScope", b.TenantScope); err != nil {
		return nil, err
	}
	if err := writeFieldString(&buf, "keyId", b.KeyID); err != nil {
		return nil, err
	}
	if err := writeFieldTime(&buf, "issuedAt", b.IssuedAt); err != nil {
		return nil, err
	}
	if err := writeFieldTime(&buf, "expiresAt", b.ExpiresAt); err != nil {
		return nil, err
	}
	buf.WriteString(`,"policies":[`)
	for i := range b.Policies {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := writePolicy(&buf, b.Policies[i]); err != nil {
			return nil, err
		}
	}
	buf.WriteString(`]}`)
	return buf.Bytes(), nil
}

func writePolicy(buf *bytes.Buffer, p Policy) error {
	buf.WriteString(`{"id":`)
	if err := writeJSONString(buf, p.ID); err != nil {
		return err
	}
	if err := writeFieldString(buf, "subject", p.Subject); err != nil {
		return err
	}
	if err := writeFieldString(buf, "resource", p.Resource); err != nil {
		return err
	}
	if err := writeFieldString(buf, "action", p.Action); err != nil {
		return err
	}
	if err := writeFieldString(buf, "effect", p.Effect); err != nil {
		return err
	}
	if len(p.Conditions) > 0 {
		buf.WriteString(`,"conditions":{`)
		keys := make([]string, 0, len(p.Conditions))
		for k := range p.Conditions {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeJSONString(buf, k); err != nil {
				return err
			}
			buf.WriteByte(':')
			if err := writeJSONString(buf, p.Conditions[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	}
	buf.WriteByte('}')
	return nil
}

func writeFieldString(buf *bytes.Buffer, name, value string) error {
	buf.WriteString(`,"`)
	buf.WriteString(name)
	buf.WriteString(`":`)
	return writeJSONString(buf, value)
}

func writeFieldTime(buf *bytes.Buffer, name string, t time.Time) error {
	return writeFieldString(buf, name, t.UTC().Format(time.RFC3339Nano))
}

// writeJSONString 使用 encoding/json 的字符串转义规则，保证与整体 JSON 编码一致。
func writeJSONString(buf *bytes.Buffer, s string) error {
	enc, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("%w: marshal string: %v", ErrInvalidBundle, err)
	}
	buf.Write(enc)
	return nil
}

// Digest 返回 canonical Bundle 字节的 SHA-256（小写 hex）。
func Digest(b Bundle) (string, error) {
	raw, err := CanonicalJSON(b)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// Sign 校验 b 后对 canonical 字节签名。不做任何隐式补全（schema/bundleID/keyID 必须已填）。
func Sign(b Bundle, priv ed25519.PrivateKey) (SignedBundle, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return SignedBundle{}, fmt.Errorf("%w: private key length %d", ErrInvalidKey, len(priv))
	}
	if err := validate(b); err != nil {
		return SignedBundle{}, err
	}
	raw, err := CanonicalJSON(b)
	if err != nil {
		return SignedBundle{}, err
	}
	if len(raw) > MaxBundleBytes {
		return SignedBundle{}, fmt.Errorf("%w: canonical %d > %d", ErrBundleTooLarge, len(raw), MaxBundleBytes)
	}
	sig := ed25519.Sign(priv, raw)
	return SignedBundle{
		Bundle:    b,
		Signature: base64.StdEncoding.EncodeToString(sig),
	}, nil
}

// Verify 使用当前时钟验签。
func Verify(sb SignedBundle, pub ed25519.PublicKey) error {
	return VerifyAt(sb, pub, time.Now())
}

// VerifyAt 使用指定时钟验签，便于测试与确定性回放。
//
// 校验顺序：schema → 策略数上限 → canonical 体积 → 公钥/签名格式 → 签名 →
// 签名内容（bundleID/keyID/时间先后）→ 过期 → 尚未生效（允许 5m 时钟偏移）。
// 时间判断放在签名之后：未验签的时间字段不可信。
func VerifyAt(sb SignedBundle, pub ed25519.PublicKey, now time.Time) error {
	if sb.Bundle.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: got %d want %d", ErrUnsupportedSchema, sb.Bundle.SchemaVersion, SchemaVersion)
	}
	if len(sb.Bundle.Policies) > MaxPolicies {
		return fmt.Errorf("%w: %d > %d", ErrTooManyPolicies, len(sb.Bundle.Policies), MaxPolicies)
	}
	raw, err := CanonicalJSON(sb.Bundle)
	if err != nil {
		return err
	}
	if len(raw) > MaxBundleBytes {
		return fmt.Errorf("%w: canonical %d > %d", ErrBundleTooLarge, len(raw), MaxBundleBytes)
	}
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: public key length %d", ErrBadSignature, len(pub))
	}
	sig, err := base64.StdEncoding.DecodeString(sb.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("%w: malformed signature", ErrBadSignature)
	}
	if !ed25519.Verify(pub, raw, sig) {
		return ErrBadSignature
	}
	if err := validate(sb.Bundle); err != nil {
		return err
	}
	now = now.UTC()
	if now.After(sb.Bundle.ExpiresAt) {
		return fmt.Errorf("%w: expired at %s", ErrExpired, sb.Bundle.ExpiresAt.UTC().Format(time.RFC3339Nano))
	}
	if sb.Bundle.IssuedAt.After(now.Add(maxClockSkew)) {
		return fmt.Errorf("%w: issued at %s", ErrNotYetValid, sb.Bundle.IssuedAt.UTC().Format(time.RFC3339Nano))
	}
	return nil
}

// Encode 序列化为 JSON 并强制 MaxBundleBytes 上限。
func Encode(sb SignedBundle) ([]byte, error) {
	out, err := json.Marshal(sb)
	if err != nil {
		return nil, fmt.Errorf("%w: encode: %v", ErrInvalidBundle, err)
	}
	if len(out) > MaxBundleBytes {
		return nil, fmt.Errorf("%w: encoded %d > %d", ErrBundleTooLarge, len(out), MaxBundleBytes)
	}
	return out, nil
}

// Decode 反序列化 JSON；不做 DisallowUnknownFields（前向兼容），但拒绝 schemaVersion > SchemaVersion。
func Decode(data []byte) (SignedBundle, error) {
	if len(data) > MaxBundleBytes {
		return SignedBundle{}, fmt.Errorf("%w: input %d > %d", ErrBundleTooLarge, len(data), MaxBundleBytes)
	}
	var sb SignedBundle
	if err := json.Unmarshal(data, &sb); err != nil {
		return SignedBundle{}, fmt.Errorf("%w: decode: %v", ErrInvalidBundle, err)
	}
	if sb.Bundle.SchemaVersion > SchemaVersion {
		return SignedBundle{}, fmt.Errorf("%w: got %d want <= %d", ErrUnsupportedSchema, sb.Bundle.SchemaVersion, SchemaVersion)
	}
	return sb, nil
}

// NewBundleID 生成随机 UUIDv4 形态的 bundleID（crypto/rand，无外部依赖）。
func NewBundleID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("abacbundle: generate bundle id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// KeySet 按 keyId 索引的公钥集合，支持轮换期同时存在多把密钥。
type KeySet struct {
	mu   sync.RWMutex
	keys map[string]ed25519.PublicKey
}

// Add 注册（或覆盖）一把公钥。允许同一 keyId 覆盖以实现平滑换钥。
func (ks *KeySet) Add(keyID string, pub ed25519.PublicKey) error {
	if keyID == "" {
		return fmt.Errorf("%w: empty keyId", ErrInvalidKey)
	}
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: public key length %d", ErrInvalidKey, len(pub))
	}
	cp := make(ed25519.PublicKey, len(pub))
	copy(cp, pub)

	ks.mu.Lock()
	defer ks.mu.Unlock()
	if ks.keys == nil {
		ks.keys = make(map[string]ed25519.PublicKey)
	}
	ks.keys[keyID] = cp
	return nil
}

// Verify 按 sb.Bundle.KeyID 选钥并验签；未知 keyId 返回 ErrUnknownKey。
func (ks *KeySet) Verify(sb SignedBundle, now time.Time) error {
	ks.mu.RLock()
	pub, ok := ks.keys[sb.Bundle.KeyID]
	ks.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownKey, sb.Bundle.KeyID)
	}
	return VerifyAt(sb, pub, now)
}
