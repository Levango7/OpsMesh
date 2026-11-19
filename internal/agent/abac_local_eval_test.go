package agent

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/pkg/abacbundle"
)

// newLocalEvalClient 构造启用策略包、带一把验签公钥的客户端。
func newLocalEvalClient(t *testing.T, pub ed25519.PublicKey, cachePath string) *ABACBundleClient {
	t.Helper()
	cfg := &config.Config{
		ABACBundleEnabled:    true,
		ABACBundlePublicKeys: []string{"k1:" + base64.StdEncoding.EncodeToString(pub)},
		ABACBundleCachePath:  cachePath,
	}
	return NewABACBundleClient(cfg, "agent-1", "secret", "http://127.0.0.1:1", nopABACBundleLogger{})
}

func newLocalEvalKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("生成 Ed25519 密钥: %v", err)
	}
	return pub, priv
}

func signLocalEvalBundle(t *testing.T, priv ed25519.PrivateKey, policies []abacbundle.Policy, issuedAt, expiresAt time.Time) abacbundle.SignedBundle {
	t.Helper()
	sb, err := abacbundle.Sign(abacbundle.Bundle{
		SchemaVersion: abacbundle.SchemaVersion,
		BundleID:      "b-local-eval",
		TenantScope:   "t1",
		KeyID:         "k1",
		IssuedAt:      issuedAt,
		ExpiresAt:     expiresAt,
		Policies:      policies,
	}, priv)
	if err != nil {
		t.Fatalf("签名策略包: %v", err)
	}
	return sb
}

func TestABACLocalEvaluateAllow(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	pub, priv := newLocalEvalKey(t)
	c := newLocalEvalClient(t, pub, filepath.Join(t.TempDir(), "bundle.json"))
	c.now = func() time.Time { return now }

	sb := signLocalEvalBundle(t, priv, []abacbundle.Policy{
		{ID: "p-allow", Subject: "alice", Resource: "vm:*", Action: "read", Effect: "allow"},
	}, now.Add(-time.Minute), now.Add(time.Hour))
	c.bundle = &sb
	c.source = abacBundleSourceNetwork

	got, err := c.EvaluateLocal("alice", "vm:42", "read", "")
	if err != nil {
		t.Fatalf("EvaluateLocal: %v", err)
	}
	if !got.Allowed || got.MatchedPolicyID != "p-allow" || got.DenyOverride {
		t.Fatalf("应命中 p-allow，得到 %+v", got)
	}
	if got.BundleExpired {
		t.Fatal("未过期包不应标记 BundleExpired")
	}
}

func TestABACLocalEvaluateDeny(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	pub, priv := newLocalEvalKey(t)
	c := newLocalEvalClient(t, pub, filepath.Join(t.TempDir(), "bundle.json"))
	c.now = func() time.Time { return now }

	sb := signLocalEvalBundle(t, priv, []abacbundle.Policy{
		{ID: "p-allow-all", Subject: "*", Resource: "*", Action: "*", Effect: "allow"},
		{ID: "p-deny-prod", Subject: "*", Resource: "vm:prod-*", Action: "*", Effect: "deny"},
	}, now.Add(-time.Minute), now.Add(time.Hour))
	c.bundle = &sb
	c.source = abacBundleSourceNetwork

	got, err := c.EvaluateLocal("bob", "vm:prod-1", "delete", "")
	if err != nil {
		t.Fatalf("EvaluateLocal: %v", err)
	}
	if got.Allowed || got.MatchedPolicyID != "p-deny-prod" || !got.DenyOverride {
		t.Fatalf("deny 必须优先，得到 %+v", got)
	}
}

func TestABACLocalEvaluateExpiredMarker(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	pub, priv := newLocalEvalKey(t)
	c := newLocalEvalClient(t, pub, filepath.Join(t.TempDir(), "bundle.json"))
	c.now = func() time.Time { return now }

	sb := signLocalEvalBundle(t, priv, []abacbundle.Policy{
		{ID: "p-allow", Subject: "*", Resource: "*", Action: "*", Effect: "allow"},
	}, now.Add(-2*time.Hour), now.Add(-time.Hour))
	c.bundle = &sb
	c.source = abacBundleSourceCache

	got, err := c.EvaluateLocal("alice", "vm:1", "read", "")
	if err != nil {
		t.Fatalf("过期包仍应可评估（仅标记），得到 error: %v", err)
	}
	if !got.Allowed || !got.BundleExpired {
		t.Fatalf("过期包应保持原决策并标记 BundleExpired，得到 %+v", got)
	}
}

func TestABACLocalEvaluateNoBundle(t *testing.T) {
	pub, _ := newLocalEvalKey(t)
	c := newLocalEvalClient(t, pub, filepath.Join(t.TempDir(), "bundle.json"))

	if _, err := c.EvaluateLocal("alice", "vm:1", "read", ""); !errors.Is(err, ErrNoBundle) {
		t.Fatalf("无包时必须返回 ErrNoBundle，得到 %v", err)
	}

	var nilClient *ABACBundleClient
	if _, err := nilClient.EvaluateLocal("alice", "vm:1", "read", ""); !errors.Is(err, ErrNoBundle) {
		t.Fatalf("nil 客户端必须返回 ErrNoBundle，得到 %v", err)
	}
}

func TestABACLocalEvaluateTamperedCacheRejected(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	pub, priv := newLocalEvalKey(t)
	cachePath := filepath.Join(t.TempDir(), "bundle.json")

	sb := signLocalEvalBundle(t, priv, []abacbundle.Policy{
		{ID: "p-deny", Subject: "*", Resource: "*", Action: "*", Effect: "deny"},
	}, now.Add(-time.Minute), now.Add(time.Hour))

	tampered := sb
	tampered.Bundle.Policies = append([]abacbundle.Policy(nil), sb.Bundle.Policies...)
	tampered.Bundle.Policies[0].Effect = "allow"
	raw, err := abacbundle.Encode(tampered)
	if err != nil {
		t.Fatalf("编码篡改包: %v", err)
	}
	if err := os.WriteFile(cachePath, raw, 0o600); err != nil {
		t.Fatalf("写入篡改缓存: %v", err)
	}

	c := newLocalEvalClient(t, pub, cachePath)
	c.now = func() time.Time { return now }
	c.LoadFromCache()
	if got, _ := c.Current(); got != nil {
		t.Fatal("签名被篡改的缓存不得载入")
	}
	if _, err := c.EvaluateLocal("alice", "vm:1", "read", ""); !errors.Is(err, ErrNoBundle) {
		t.Fatalf("篡改缓存被丢弃后应返回 ErrNoBundle，得到 %v", err)
	}
}

func TestABACLocalEvaluateTamperedMemoryRejected(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	pub, priv := newLocalEvalKey(t)
	c := newLocalEvalClient(t, pub, filepath.Join(t.TempDir(), "bundle.json"))
	c.now = func() time.Time { return now }

	sb := signLocalEvalBundle(t, priv, []abacbundle.Policy{
		{ID: "p-deny", Subject: "*", Resource: "*", Action: "*", Effect: "deny"},
	}, now.Add(-time.Minute), now.Add(time.Hour))

	tampered := sb
	tampered.Bundle.Policies = append([]abacbundle.Policy(nil), sb.Bundle.Policies...)
	tampered.Bundle.Policies[0].Effect = "allow"
	c.bundle = &tampered
	c.source = abacBundleSourceNetwork

	if _, err := c.EvaluateLocal("alice", "vm:1", "read", ""); !errors.Is(err, abacbundle.ErrBadSignature) {
		t.Fatalf("内存包复验失败必须拒绝评估（ErrBadSignature），得到 %v", err)
	}
}
