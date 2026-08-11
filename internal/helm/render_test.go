package helm

import (
	"strings"
	"testing"
)

// =============================================================================
// ParseMultiDocYAML 测试
// =============================================================================

const sampleManifest = `---
# Source: mysql/templates/primary/statefulset.yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: mysql-primary
  labels:
    app.kubernetes.io/name: mysql
spec:
  replicas: 1
---
# Source: mysql/templates/primary/svc.yaml
apiVersion: v1
kind: Service
metadata:
  name: mysql-primary
spec:
  ports:
  - port: 3306
---
# Source: mysql/templates/primary/svc-headless.yaml
apiVersion: v1
kind: Service
metadata:
  name: mysql-primary-headless
spec:
  clusterIP: None
---
# Source: mysql/templates/primary/configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: mysql-primary
data:
  my.cnf: |
    [mysqld]
    port=3306
`

func TestParseMultiDocYAML_Basic(t *testing.T) {
	templates := ParseMultiDocYAML(sampleManifest)
	if len(templates) != 4 {
		t.Fatalf("templates len = %d, want 4", len(templates))
	}

	// 验证 StatefulSet。
	ss := templates[0]
	if ss.Kind != "StatefulSet" {
		t.Errorf("templates[0].Kind = %q, want StatefulSet", ss.Kind)
	}
	if ss.APIVersion != "apps/v1" {
		t.Errorf("templates[0].APIVersion = %q, want apps/v1", ss.APIVersion)
	}
	if ss.Name != "mysql/templates/primary/statefulset.yaml" {
		t.Errorf("templates[0].Name = %q", ss.Name)
	}
	if ss.ResourceName != "mysql-primary" {
		t.Errorf("templates[0].ResourceName = %q, want mysql-primary", ss.ResourceName)
	}

	// 验证第一个 Service。
	svc1 := templates[1]
	if svc1.Kind != "Service" {
		t.Errorf("templates[1].Kind = %q, want Service", svc1.Kind)
	}
	if svc1.ResourceName != "mysql-primary" {
		t.Errorf("templates[1].ResourceName = %q", svc1.ResourceName)
	}

	// 验证 ConfigMap。
	cm := templates[3]
	if cm.Kind != "ConfigMap" {
		t.Errorf("templates[3].Kind = %q, want ConfigMap", cm.Kind)
	}
}

func TestParseMultiDocYAML_Empty(t *testing.T) {
	if got := ParseMultiDocYAML(""); got != nil {
		t.Errorf("ParseMultiDocYAML(empty) = %v, want nil", got)
	}
	if got := ParseMultiDocYAML("   \n  \n"); got != nil {
		t.Errorf("ParseMultiDocYAML(whitespace) = %v, want nil", got)
	}
}

func TestParseMultiDocYAML_SingleDoc(t *testing.T) {
	const single = `---
# Source: chart/templates/deploy.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
`
	templates := ParseMultiDocYAML(single)
	if len(templates) != 1 {
		t.Fatalf("templates len = %d, want 1", len(templates))
	}
	if templates[0].Kind != "Deployment" {
		t.Errorf("Kind = %q", templates[0].Kind)
	}
}

func TestParseMultiDocYAML_NoSource(t *testing.T) {
	// 无 Source 注释时用 kind/resourceName 兜底。
	const noSource = `---
apiVersion: v1
kind: Secret
metadata:
  name: my-secret
`
	templates := ParseMultiDocYAML(noSource)
	if len(templates) != 1 {
		t.Fatalf("templates len = %d, want 1", len(templates))
	}
	if templates[0].Kind != "Secret" {
		t.Errorf("Kind = %q", templates[0].Kind)
	}
	if templates[0].ResourceName != "my-secret" {
		t.Errorf("ResourceName = %q", templates[0].ResourceName)
	}
	// Name 应兜底为 "secret/my-secret"。
	if templates[0].Name != "secret/my-secret" {
		t.Errorf("Name = %q, want secret/my-secret", templates[0].Name)
	}
}

func TestParseMultiDocYAML_WithCRD(t *testing.T) {
	const crd = `---
# Source: crds/crd.yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: monitors.monitoring.coreos.com
spec:
  group: monitoring.coreos.com
`
	templates := ParseMultiDocYAML(crd)
	if len(templates) != 1 {
		t.Fatalf("templates len = %d, want 1", len(templates))
	}
	if templates[0].Kind != "CustomResourceDefinition" {
		t.Errorf("Kind = %q", templates[0].Kind)
	}
}

func TestParseMultiDocYAML_SkipEmptyDocs(t *testing.T) {
	// 含空文档（连续 ---）。
	const withEmpty = `---
---

---
# Source: chart/templates/deploy.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
`
	templates := ParseMultiDocYAML(withEmpty)
	if len(templates) != 1 {
		t.Fatalf("templates len = %d, want 1 (empty docs skipped)", len(templates))
	}
}

func TestParseMultiDocYAML_QuotedValues(t *testing.T) {
	// 带引号的 name 值。
	const quoted = `---
# Source: chart/templates/svc.yaml
apiVersion: v1
kind: Service
metadata:
  name: "my-service"
`
	templates := ParseMultiDocYAML(quoted)
	if len(templates) != 1 {
		t.Fatalf("templates len = %d, want 1", len(templates))
	}
	if templates[0].ResourceName != "my-service" {
		t.Errorf("ResourceName = %q, want my-service (unquoted)", templates[0].ResourceName)
	}
}

// =============================================================================
// splitYAMLDocuments / parseSingleYAMLDoc 测试
// =============================================================================

func TestSplitYAMLDocuments(t *testing.T) {
	docs := splitYAMLDocuments("---\nkind: A\n---\nkind: B\n")
	if len(docs) != 2 {
		t.Fatalf("docs len = %d, want 2", len(docs))
	}
	if !strings.Contains(docs[0], "kind: A") {
		t.Errorf("docs[0] = %q", docs[0])
	}
	if !strings.Contains(docs[1], "kind: B") {
		t.Errorf("docs[1] = %q", docs[1])
	}
}

func TestIsYAMLDocSeparator(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"---", true},
		{"...", true},
		{"--- ", true},
		{"  ---  ", true},
		{"--", false},
		{"kind: Service", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isYAMLDocSeparator(tt.line); got != tt.want {
			t.Errorf("isYAMLDocSeparator(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestSplitYAMLKeyValue(t *testing.T) {
	tests := []struct {
		input string
		key   string
		val   string
		ok    bool
	}{
		{"kind: Service", "kind", "Service", true},
		{"name: mysql", "name", "mysql", true},
		{"metadata:", "metadata", "", true},
		{"- item", "", "", false},
		{"key with space: value", "", "", false},
		{"no colon", "", "", false},
	}
	for _, tt := range tests {
		key, val, ok := splitYAMLKeyValue(tt.input)
		if key != tt.key || val != tt.val || ok != tt.ok {
			t.Errorf("splitYAMLKeyValue(%q) = (%q,%q,%v), want (%q,%q,%v)",
				tt.input, key, val, ok, tt.key, tt.val, tt.ok)
		}
	}
}

func TestUnquoteYAML(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`"value"`, "value"},
		{`'value'`, "value"},
		{`plain`, "plain"},
		{`""`, ""},
		{`"`, `"`},
	}
	for _, tt := range tests {
		if got := unquoteYAML(tt.input); got != tt.want {
			t.Errorf("unquoteYAML(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// =============================================================================
// RenderStats / FilterByKind 测试
// =============================================================================

func TestStats(t *testing.T) {
	templates := ParseMultiDocYAML(sampleManifest)
	st := Stats(templates)
	if st.Total != 4 {
		t.Errorf("Total = %d, want 4", st.Total)
	}
	if st.ByKind["StatefulSet"] != 1 {
		t.Errorf("ByKind[StatefulSet] = %d, want 1", st.ByKind["StatefulSet"])
	}
	if st.ByKind["Service"] != 2 {
		t.Errorf("ByKind[Service] = %d, want 2", st.ByKind["Service"])
	}
	if st.ByKind["ConfigMap"] != 1 {
		t.Errorf("ByKind[ConfigMap] = %d, want 1", st.ByKind["ConfigMap"])
	}
}

func TestFilterByKind(t *testing.T) {
	templates := ParseMultiDocYAML(sampleManifest)
	services := FilterByKind(templates, "Service")
	if len(services) != 2 {
		t.Errorf("services len = %d, want 2", len(services))
	}
	both := FilterByKind(templates, "Service", "StatefulSet")
	if len(both) != 3 {
		t.Errorf("both len = %d, want 3", len(both))
	}
	all := FilterByKind(templates)
	if len(all) != 4 {
		t.Errorf("all len = %d, want 4", len(all))
	}
}

// =============================================================================
// RenderOptions / RenderChart 测试（用 mock CLI）
// =============================================================================

func TestRenderChart_Validation(t *testing.T) {
	// chartPath 为空。
	_, err := RenderChart("", &RenderOptions{ReleaseName: "rel"})
	if err == nil {
		t.Fatal("RenderChart empty chartPath should fail")
	}

	// ReleaseName 为空。
	_, err = RenderChart("./chart", &RenderOptions{ReleaseName: ""})
	if err == nil {
		t.Fatal("RenderChart empty ReleaseName should fail")
	}

	// opts 为 nil。
	_, err = RenderChart("./chart", nil)
	if err == nil {
		t.Fatal("RenderChart nil opts should fail")
	}
}

func TestRenderChartWithCLI_Success(t *testing.T) {
	mock := newMockReturn(map[string]string{
		"template": sampleManifest,
	})
	templates, err := RenderChartWithCLI(mock, "bitnami/mysql", &RenderOptions{
		Namespace:   "default",
		ReleaseName: "my-release",
	})
	if err != nil {
		t.Fatalf("RenderChartWithCLI failed: %v", err)
	}
	if len(templates) != 4 {
		t.Fatalf("templates len = %d, want 4", len(templates))
	}

	// 验证 template 命令被调用。
	found := false
	for _, call := range mock.calls {
		if len(call) > 0 && call[0] == "template" {
			found = true
			if call[1] != "my-release" || call[2] != "bitnami/mysql" {
				t.Errorf("template args = %v", call)
			}
			break
		}
	}
	if !found {
		t.Error("template command not called")
	}
}

func TestRenderChartWithCLI_WithValues(t *testing.T) {
	mock := newMockReturn(map[string]string{
		"template": sampleManifest,
	})
	_, err := RenderChartWithCLI(mock, "chart", &RenderOptions{
		ReleaseName: "rel",
		Namespace:   "ns",
		Values: map[string]interface{}{
			"key": "value",
		},
	})
	if err != nil {
		t.Fatalf("RenderChartWithCLI with values failed: %v", err)
	}
	// 验证 -f 参数。
	for _, call := range mock.calls {
		if len(call) > 0 && call[0] == "template" {
			hasF := false
			for i, a := range call {
				if a == "-f" && i+1 < len(call) {
					hasF = true
					break
				}
			}
			if !hasF {
				t.Error("template call should include -f for values")
			}
			return
		}
	}
}

func TestRenderChartWithCLI_IncludeCRDs(t *testing.T) {
	mock := newMockReturn(map[string]string{
		"template": sampleManifest,
	})
	_, err := RenderChartWithCLI(mock, "chart", &RenderOptions{
		ReleaseName: "rel",
		IncludeCRDs: true,
	})
	if err != nil {
		t.Fatalf("RenderChartWithCLI failed: %v", err)
	}
	for _, call := range mock.calls {
		if len(call) > 0 && call[0] == "template" {
			hasCRDs := false
			for _, a := range call {
				if a == "--include-crds" {
					hasCRDs = true
					break
				}
			}
			if !hasCRDs {
				t.Error("template call should include --include-crds")
			}
			return
		}
	}
}

func TestRenderChartWithCLI_DefaultNamespace(t *testing.T) {
	mock := newMockReturn(map[string]string{
		"template": sampleManifest,
	})
	_, err := RenderChartWithCLI(mock, "chart", &RenderOptions{
		ReleaseName: "rel",
		// Namespace 为空，应默认 "default"。
	})
	if err != nil {
		t.Fatalf("RenderChartWithCLI failed: %v", err)
	}
	for _, call := range mock.calls {
		if len(call) > 0 && call[0] == "template" {
			hasDefault := false
			for i, a := range call {
				if a == "-n" && i+1 < len(call) && call[i+1] == "default" {
					hasDefault = true
					break
				}
			}
			if !hasDefault {
				t.Error("template call should default to -n default")
			}
			return
		}
	}
}