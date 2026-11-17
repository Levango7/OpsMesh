package abacbundle

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"
)

func testKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(0x40 + i)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	return priv.Public().(ed25519.PublicKey), priv
}

func fixedBundle() Bundle {
	return Bundle{
		SchemaVersion: SchemaVersion,
		BundleID:      "bundle-fixed-001",
		TenantScope:   "tenant-a",
		KeyID:         "key-2026-01",
		IssuedAt:      time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		ExpiresAt:     time.Date(2026, 1, 3, 3, 4, 5, 0, time.UTC),
		Policies: []Policy{
			{
				ID:       "p-allow-read",
				Subject:  `{"role":"admin","team":"sre"}`,
				Resource: "device/*",
				Action:   "read",
				Effect:   "allow",
				Conditions: map[string]string{
					"region": "cn-north",
					"env":    "prod",
				},
			},
			{
				ID:       "p-allow-write",
				Subject:  "user:alice",
				Resource: "task/*",
				Action:   "write",
				Effect:   "allow",
			},
		},
	}
}

func validBundle(now time.Time, keyID string) Bundle {
	return Bundle{
		SchemaVersion: SchemaVersion,
		BundleID:      "bundle-valid-001",
		TenantScope:   "tenant-a",
		KeyID:         keyID,
		IssuedAt:      now.Add(-time.Hour).UTC(),
		ExpiresAt:     now.Add(time.Hour).UTC(),
		Policies: []Policy{
			{
				ID:       "p1",
				Subject:  "user:alice",
				Resource: "device/*",
				Action:   "read",
				Effect:   "allow",
				Conditions: map[string]string{
					"env": "prod",
				},
			},
			{
				ID:       "p2",
				Subject:  "user:bob",
				Resource: "task/*",
				Action:   "write",
				Effect:   "allow",
			},
		},
	}
}

// TestCanonicalJSONExact 锁定 canonical 字节：字段顺序固定、Conditions 按 key 排序、无空白。
func TestCanonicalJSONExact(t *testing.T) {
	want := `{"schemaVersion":1,"bundleID":"bundle-fixed-001","tenantScope":"tenant-a","keyId":"key-2026-01","issuedAt":"2026-01-02T03:04:05Z","expiresAt":"2026-01-03T03:04:05Z","policies":[{"id":"p-allow-read","subject":"{\"role\":\"admin\",\"team\":\"sre\"}","resource":"device/*","action":"read","effect":"allow","conditions":{"env":"prod","region":"cn-north"}},{"id":"p-allow-write","subject":"user:alice","resource":"task/*","action":"write","effect":"allow"}]}`

	got, err := CanonicalJSON(fixedBundle())
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	if string(got) != want {
		t.Fatalf("canonical bytes mismatch:\n got: %s\nwant: %s", got, want)
	}

	// 时间在同一时刻的不同时区表示必须规范化一致。
	shifted := fixedBundle()
	loc := time.FixedZone("UTC+8", 8*3600)
	shifted.IssuedAt = shifted.IssuedAt.In(loc)
	shifted.ExpiresAt = shifted.ExpiresAt.In(loc)
	gotShifted, err := CanonicalJSON(shifted)
	if err != nil {
		t.Fatalf("CanonicalJSON shifted: %v", err)
	}
	if !bytes.Equal(got, gotShifted) {
		t.Fatalf("timezone shift changed canonical bytes:\n%s\n%s", got, gotShifted)
	}

	// 空 / nil policies 与空 Conditions 均归一为 [] / 省略。
	empty := Bundle{
		SchemaVersion: SchemaVersion,
		BundleID:      "b",
		TenantScope:   "t",
		KeyID:         "k",
		IssuedAt:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		ExpiresAt:     time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		Policies:      nil,
	}
	gotEmpty, err := CanonicalJSON(&empty)
	if err != nil {
		t.Fatalf("CanonicalJSON empty: %v", err)
	}
	wantTail := `"policies":[]}`
	if !strings.HasSuffix(string(gotEmpty), wantTail) {
		t.Fatalf("nil policies should canonicalize as []: %s", gotEmpty)
	}
}

func TestCanonicalJSONRejectsUnsupportedType(t *testing.T) {
	if _, err := CanonicalJSON(map[string]string{"a": "b"}); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("want ErrInvalidBundle, got %v", err)
	}
	var nilBundle *Bundle
	if _, err := CanonicalJSON(nilBundle); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("nil bundle: want ErrInvalidBundle, got %v", err)
	}
}

// TestDigestStableAcrossReencode 编码/解码往返后 canonical 与 digest 不变。
func TestDigestStableAcrossReencode(t *testing.T) {
	b := fixedBundle()
	d1, err := Digest(b)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if len(d1) != 64 {
		t.Fatalf("digest length = %d, want 64 hex chars", len(d1))
	}

	_, priv := testKeyPair(t)
	sb, err := Sign(b, priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	raw, err := Encode(sb)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	d2, err := Digest(decoded.Bundle)
	if err != nil {
		t.Fatalf("Digest decoded: %v", err)
	}
	if d1 != d2 {
		t.Fatalf("digest drifted across re-encode: %s != %s", d1, d2)
	}
	again, err := Encode(decoded)
	if err != nil {
		t.Fatalf("Encode decoded: %v", err)
	}
	if !bytes.Equal(raw, again) {
		t.Fatalf("encode/decode/encode is not byte-stable")
	}
}

func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv := testKeyPair(t)
	now := time.Now().UTC().Truncate(time.Second)
	b := validBundle(now, "k1")

	sb, err := Sign(b, priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if sb.Signature == "" {
		t.Fatal("empty signature")
	}
	if err := VerifyAt(sb, pub, now); err != nil {
		t.Fatalf("VerifyAt: %v", err)
	}
	if err := Verify(sb, pub); err != nil {
		t.Fatalf("Verify with wall clock: %v", err)
	}
}

func TestVerifyTamperedBundleFails(t *testing.T) {
	pub, priv := testKeyPair(t)
	now := time.Now().UTC().Truncate(time.Second)
	sb, err := Sign(validBundle(now, "k1"), priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	clone := func() SignedBundle {
		c := sb
		c.Bundle.Policies = append([]Policy(nil), sb.Bundle.Policies...)
		return c
	}

	tests := []struct {
		name   string
		tamper func(c *SignedBundle)
	}{
		{
			name: "policy subject",
			tamper: func(c *SignedBundle) {
				c.Bundle.Policies[0].Subject = `{"role":"root"}`
			},
		},
		{
			name: "policy action",
			tamper: func(c *SignedBundle) {
				c.Bundle.Policies[1].Action = "delete"
			},
		},
		{
			name: "policy condition",
			tamper: func(c *SignedBundle) {
				c.Bundle.Policies[0].Conditions = map[string]string{"env": "dev"}
			},
		},
		{
			name:   "expiry",
			tamper: func(c *SignedBundle) { c.Bundle.ExpiresAt = c.Bundle.ExpiresAt.Add(48 * time.Hour) },
		},
		{
			name:   "issuedAt",
			tamper: func(c *SignedBundle) { c.Bundle.IssuedAt = c.Bundle.IssuedAt.Add(-time.Hour) },
		},
		{
			name:   "removed policy",
			tamper: func(c *SignedBundle) { c.Bundle.Policies = c.Bundle.Policies[:1] },
		},
		{
			name: "signature byte",
			tamper: func(c *SignedBundle) {
				sig, derr := base64.StdEncoding.DecodeString(c.Signature)
				if derr != nil {
					t.Fatalf("decode signature: %v", derr)
				}
				sig[0] ^= 0x01
				c.Signature = base64.StdEncoding.EncodeToString(sig)
			},
		},
		{
			name:   "signature garbage",
			tamper: func(c *SignedBundle) { c.Signature = "not-base64!!" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := clone()
			tt.tamper(&c)
			if err := VerifyAt(c, pub, now); !errors.Is(err, ErrBadSignature) {
				t.Fatalf("want ErrBadSignature, got %v", err)
			}
		})
	}
}

func TestVerifyTimeWindow(t *testing.T) {
	pub, priv := testKeyPair(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	sign := func(b Bundle) SignedBundle {
		t.Helper()
		sb, err := Sign(b, priv)
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		return sb
	}

	expired := validBundle(now, "k1")
	expired.IssuedAt = now.Add(-48 * time.Hour)
	expired.ExpiresAt = now.Add(-24 * time.Hour)
	if err := VerifyAt(sign(expired), pub, now); !errors.Is(err, ErrExpired) {
		t.Fatalf("want ErrExpired, got %v", err)
	}

	// 恰好到 ExpiresAt 的瞬间仍有效（expiresAt 为闭边界）。
	boundary := validBundle(now, "k1")
	boundary.ExpiresAt = now
	if err := VerifyAt(sign(boundary), pub, now); err != nil {
		t.Fatalf("boundary should be valid: %v", err)
	}

	future := validBundle(now, "k1")
	future.IssuedAt = now.Add(10 * time.Minute)
	future.ExpiresAt = now.Add(2 * time.Hour)
	if err := VerifyAt(sign(future), pub, now); !errors.Is(err, ErrNotYetValid) {
		t.Fatalf("want ErrNotYetValid, got %v", err)
	}

	skewed := validBundle(now, "k1")
	skewed.IssuedAt = now.Add(4 * time.Minute)
	skewed.ExpiresAt = now.Add(2 * time.Hour)
	if err := VerifyAt(sign(skewed), pub, now); err != nil {
		t.Fatalf("4m skew should be tolerated: %v", err)
	}
}

func TestSchemaAndCaps(t *testing.T) {
	pub, priv := testKeyPair(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	schema2 := validBundle(now, "k1")
	schema2.SchemaVersion = SchemaVersion + 1
	if _, err := Sign(schema2, priv); !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("Sign schema 2: want ErrUnsupportedSchema, got %v", err)
	}

	// 即使签名本身正确，Verify 也必须先拒绝不支持的 schema。
	raw2, err := CanonicalJSON(schema2)
	if err != nil {
		t.Fatalf("CanonicalJSON schema 2: %v", err)
	}
	forged := SignedBundle{Bundle: schema2, Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, raw2))}
	if err := VerifyAt(forged, pub, now); !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("VerifyAt schema 2: want ErrUnsupportedSchema, got %v", err)
	}
	if _, err := Decode([]byte(`{"bundle":{"schemaVersion":2},"signature":""}`)); !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("Decode schema 2: want ErrUnsupportedSchema, got %v", err)
	}

	tooMany := validBundle(now, "k1")
	tooMany.Policies = make([]Policy, MaxPolicies+1)
	if _, err := Sign(tooMany, priv); !errors.Is(err, ErrTooManyPolicies) {
		t.Fatalf("Sign too many: want ErrTooManyPolicies, got %v", err)
	}
	if err := VerifyAt(SignedBundle{Bundle: tooMany}, pub, now); !errors.Is(err, ErrTooManyPolicies) {
		t.Fatalf("VerifyAt too many: want ErrTooManyPolicies, got %v", err)
	}

	huge := validBundle(now, "k1")
	huge.Policies = []Policy{{
		ID:       "p-huge",
		Subject:  strings.Repeat("a", MaxBundleBytes),
		Resource: "*",
		Action:   "read",
		Effect:   "allow",
	}}
	if _, err := Sign(huge, priv); !errors.Is(err, ErrBundleTooLarge) {
		t.Fatalf("Sign huge: want ErrBundleTooLarge, got %v", err)
	}
	if _, err := Encode(SignedBundle{Bundle: huge}); !errors.Is(err, ErrBundleTooLarge) {
		t.Fatalf("Encode huge: want ErrBundleTooLarge, got %v", err)
	}
	if _, err := Decode(make([]byte, MaxBundleBytes+1)); !errors.Is(err, ErrBundleTooLarge) {
		t.Fatalf("Decode huge: want ErrBundleTooLarge, got %v", err)
	}
}

func TestKeySetRotation(t *testing.T) {
	pubOld, privOld := testKeyPair(t)

	seed := bytes.Repeat([]byte{0xA5}, ed25519.SeedSize)
	privNew := ed25519.NewKeyFromSeed(seed)
	pubNew := privNew.Public().(ed25519.PublicKey)

	now := time.Now().UTC().Truncate(time.Second)
	sbOld, err := Sign(validBundle(now, "old"), privOld)
	if err != nil {
		t.Fatalf("Sign old: %v", err)
	}
	sbNew, err := Sign(validBundle(now, "new"), privNew)
	if err != nil {
		t.Fatalf("Sign new: %v", err)
	}

	var ks KeySet
	if err := ks.Add("old", pubOld); err != nil {
		t.Fatalf("Add old: %v", err)
	}
	if err := ks.Add("new", pubNew); err != nil {
		t.Fatalf("Add new: %v", err)
	}
	if err := ks.Verify(sbOld, now); err != nil {
		t.Fatalf("rotation must accept old key: %v", err)
	}
	if err := ks.Verify(sbNew, now); err != nil {
		t.Fatalf("rotation must accept new key: %v", err)
	}

	unknown := sbOld
	unknown.Bundle.KeyID = "gone"
	if err := ks.Verify(unknown, now); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("want ErrUnknownKey, got %v", err)
	}

	if err := ks.Add("", pubOld); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("empty keyID: want ErrInvalidKey, got %v", err)
	}
	if err := ks.Add("short", ed25519.PublicKey{1, 2, 3}); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("short key: want ErrInvalidKey, got %v", err)
	}
}

// TestDecodeForwardCompatible 未知字段（bundle / policy / 顶层）必须被忽略，签名仍可验证。
func TestDecodeForwardCompatible(t *testing.T) {
	pub, priv := testKeyPair(t)
	now := time.Now().UTC().Truncate(time.Second)
	sb, err := Sign(validBundle(now, "k1"), priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	raw, err := Encode(sb)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal encoded: %v", err)
	}
	doc["futureTopLevel"] = map[string]any{"x": 1}
	bundleMap := doc["bundle"].(map[string]any)
	bundleMap["futureBundleField"] = "ignored"
	firstPolicy := bundleMap["policies"].([]any)[0].(map[string]any)
	firstPolicy["futurePolicyField"] = []any{"ignored"}
	forward, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal forward bundle: %v", err)
	}

	decoded, err := Decode(forward)
	if err != nil {
		t.Fatalf("Decode forward-compatible bundle: %v", err)
	}
	if err := VerifyAt(decoded, pub, now); err != nil {
		t.Fatalf("VerifyAt forward-compatible bundle: %v", err)
	}
}

func TestNewBundleID(t *testing.T) {
	seen := make(map[string]struct{}, 64)
	for i := 0; i < 64; i++ {
		id, err := NewBundleID()
		if err != nil {
			t.Fatalf("NewBundleID: %v", err)
		}
		if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
			t.Fatalf("malformed uuid: %q", id)
		}
		if id[14] != '4' {
			t.Fatalf("uuid version nibble = %q, want 4", id[14])
		}
		if !strings.ContainsRune("89ab", rune(id[19])) {
			t.Fatalf("uuid variant nibble = %q, want 8/9/a/b", id[19])
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate uuid: %q", id)
		}
		seen[id] = struct{}{}
	}
}

// TestRandomBundlesSignVerifyAndTamper 属性测试：随机 bundle 均可验签，
// canonical 字节被单字节篡改后签名不再成立（签名覆盖全部 canonical 字节）。
//
// 额外做一次「解码后」篡改检查：Decode 允许未知字段（前向兼容），因此把某个
// 值为空的字段名改成未知字段会解码出**同一个 Bundle**——这不是有效篡改
// （canonical payload 未变，语义相同）。只要篡改改变了 canonical 内容，Verified 必须失败。
func TestRandomBundlesSignVerifyAndTamper(t *testing.T) {
	pub, priv := testKeyPair(t)
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	rng := rand.New(rand.NewSource(20261117))

	words := []string{"alice", "bob", "admin", "sre", "read", "write", "device/*", "task/*", "prod", "dev", "cn-north", "us-east", `{"role":"ops"}`}
	pick := func() string { return words[rng.Intn(len(words))] }

	for i := 0; i < 200; i++ {
		b := Bundle{
			SchemaVersion: SchemaVersion,
			BundleID:      fmt.Sprintf("bundle-%03d", i),
			TenantScope:   fmt.Sprintf("tenant-%d", rng.Intn(5)),
			KeyID:         fmt.Sprintf("key-%d", rng.Intn(3)),
			IssuedAt:      now.Add(-time.Duration(rng.Intn(3600)) * time.Second),
			ExpiresAt:     now.Add(time.Duration(1+rng.Intn(3600)) * time.Second),
		}
		nPolicies := rng.Intn(6)
		for p := 0; p < nPolicies; p++ {
			pol := Policy{
				ID:       fmt.Sprintf("p-%d-%d", i, p),
				Subject:  pick(),
				Resource: pick(),
				Action:   pick(),
				Effect:   "allow",
			}
			if rng.Intn(2) == 0 {
				pol.Conditions = make(map[string]string, 4)
				for c := 0; c < rng.Intn(4); c++ {
					pol.Conditions[fmt.Sprintf("k%d", rng.Intn(6))] = pick()
				}
			}
			b.Policies = append(b.Policies, pol)
		}

		sb, err := Sign(b, priv)
		if err != nil {
			t.Fatalf("iteration %d: Sign: %v", i, err)
		}
		if err := VerifyAt(sb, pub, now); err != nil {
			t.Fatalf("iteration %d: VerifyAt: %v", i, err)
		}

		canon, err := CanonicalJSON(b)
		if err != nil {
			t.Fatalf("iteration %d: CanonicalJSON: %v", i, err)
		}
		if len(canon) == 0 {
			t.Fatalf("iteration %d: empty canonical bytes", i)
		}
		sig, err := base64.StdEncoding.DecodeString(sb.Signature)
		if err != nil {
			t.Fatalf("iteration %d: decode signature: %v", i, err)
		}

		tampered := bytes.Clone(canon)
		pos := rng.Intn(len(tampered))
		tampered[pos] ^= 0x01

		if ed25519.Verify(pub, tampered, sig) {
			t.Fatalf("iteration %d: signature accepted tampered canonical byte %d", i, pos)
		}

		var tb Bundle
		if err := json.Unmarshal(tampered, &tb); err != nil {
			// 篡改破坏 JSON 结构同样是安全拒绝。
			continue
		}
		recanon, err := CanonicalJSON(tb)
		if err != nil {
			continue
		}
		if bytes.Equal(recanon, canon) {
			continue
		}
		if err := VerifyAt(SignedBundle{Bundle: tb, Signature: sb.Signature}, pub, now); err == nil {
			t.Fatalf("iteration %d: tampered canonical byte %d still verified", i, pos)
		}
	}
}
