package controlplane

import (
	"reflect"
	"testing"

	"github.com/Levango7/OpsMesh/internal/store"
)

// fakeSecretStore 是启动扫描所需的最小替身（只实现 plaintextSecretKeys 用到的两个方法）。
type fakeSecretStore struct {
	items map[string]store.SecretItem // "tenant/key" -> item（Value 为存储值）
}

func (f *fakeSecretStore) ListSecrets(string) []*store.SecretMeta {
	var out []*store.SecretMeta
	for k := range f.items {
		tenant, key := splitTenantKey(k)
		out = append(out, &store.SecretMeta{TenantID: tenant, Key: key})
	}
	return out
}

func (f *fakeSecretStore) GetSecret(tenantID, key string) (*store.SecretItem, bool) {
	it, ok := f.items[tenantID+"/"+key]
	if !ok {
		return nil, false
	}
	cp := it
	return &cp, true
}

func splitTenantKey(s string) (string, string) {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}

// TestPlaintextSecretKeys 钉住启动扫描的判定：只有「非引用」才算明文存量；引用与空表不报警。
func TestPlaintextSecretKeys(t *testing.T) {
	cases := []struct {
		name  string
		items map[string]store.SecretItem
		want  []string
	}{
		{
			name:  "空表不报",
			items: nil,
			want:  nil,
		},
		{
			name: "引用形态不报",
			items: map[string]store.SecretItem{
				"t1/ref": {Key: "ref", Value: "${vault:test/ref}"},
			},
			want: nil,
		},
		{
			name: "明文存量点名（按 tenant/key 升序）",
			items: map[string]store.SecretItem{
				"t2/legacy": {Key: "legacy", Value: "plain-password"},
				"t1/old":    {Key: "old", Value: "p@ss"},
				"t1/ref":    {Key: "ref", Value: "${env:TOKEN}"},
			},
			want: []string{"t1/old", "t2/legacy"},
		},
	}
	for _, c := range cases {
		got := plaintextSecretKeys(&fakeSecretStore{items: c.items})
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: plaintextSecretKeys = %v, want %v", c.name, got, c.want)
		}
	}
}
