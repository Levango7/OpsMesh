package v1alpha1

import (
	"encoding/json"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func decodeKeys(t *testing.T, v any) map[string]json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v (raw=%s)", err, raw)
	}
	return out
}

func requireKeys(t *testing.T, got map[string]json.RawMessage, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("键集合 = %v, want %v", keysOf(got), want)
	}
	for _, k := range want {
		if _, ok := got[k]; !ok {
			t.Fatalf("缺键 %q（实际 %v）", k, keysOf(got))
		}
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestGroupVersionAndSchemeRegistration(t *testing.T) {
	if got := GroupVersion.String(); got != "opsmesh.io/v1alpha1" {
		t.Fatalf("GroupVersion = %q, want opsmesh.io/v1alpha1", got)
	}

	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	for _, obj := range []runtime.Object{
		&OpsMeshInstance{}, &OpsMeshInstanceList{},
		&ExternalK8sCluster{}, &ExternalK8sClusterList{},
		&HelmRelease{}, &HelmReleaseList{},
	} {
		kinds, _, err := scheme.ObjectKinds(obj)
		if err != nil {
			t.Fatalf("ObjectKinds(%T): %v", obj, err)
		}
		if len(kinds) != 1 {
			t.Fatalf("ObjectKinds(%T) = %v, want 单个 GVK", obj, kinds)
		}
		want := schema.GroupVersionKind{Group: "opsmesh.io", Version: "v1alpha1", Kind: kinds[0].Kind}
		if kinds[0] != want {
			t.Fatalf("ObjectKinds(%T) = %v, want %v", obj, kinds[0], want)
		}
	}
}

// CRD 的 json tag 就是用户可见的 YAML/JSON 字段名，改名等于破坏已部署的清单。
func TestCRDJSONFieldNames(t *testing.T) {
	instance := OpsMeshInstance{Spec: OpsMeshInstanceSpec{
		Replicas: 1, Image: "img", AgentImage: "agent", Store: "memory", SegmentCIDR: "10.244.0.0/16",
	}}
	top := decodeKeys(t, instance)
	if _, ok := top["spec"]; !ok {
		t.Fatalf("OpsMeshInstance 缺 spec: %v", keysOf(top))
	}
	if _, ok := top["status"]; !ok {
		t.Fatalf("OpsMeshInstance 缺 status: %v", keysOf(top))
	}
	// Production/TLSEnabled 为 false ⇒ 按 omitempty 省略；MySQL/Redis 是值类型结构体，
	// 即便标了 omitempty 也恒被序列化（encoding/json 不省略零值 struct）——这就是
	// CRD 实际暴露的键集合，不是笔误。
	requireKeys(t, decodeKeys(t, instance.Spec),
		"replicas", "image", "agentImage", "store", "segmentCIDR", "mysql", "redis")
	requireKeys(t, decodeKeys(t, MySQLSpec{Enabled: true, Storage: "10Gi"}),
		"enabled", "storage")
	requireKeys(t, decodeKeys(t, RedisSpec{Enabled: true, Storage: "1Gi"}),
		"enabled", "storage")

	full := OpsMeshInstanceSpec{
		Replicas: 2, Image: "img", AgentImage: "agent", Store: "mysql",
		Production: true, TLSEnabled: true, SegmentCIDR: "10.244.0.0/16",
		MySQL: MySQLSpec{Enabled: true, Storage: "10Gi"},
		Redis: RedisSpec{Enabled: true, Storage: "1Gi"},
	}
	requireKeys(t, decodeKeys(t, full),
		"replicas", "image", "agentImage", "store", "production", "tlsEnabled",
		"segmentCIDR", "mysql", "redis")

	helm := HelmReleaseSpec{ChartName: "c", RepoName: "r", Namespace: "ns", Environment: EnvironmentProduction}
	requireKeys(t, decodeKeys(t, helm), "chartName", "repoName", "namespace", "environment")

	ext := ExternalK8sClusterSpec{
		ClusterName: "c", KubeconfigSecretRef: corev1.SecretKeySelector{},
	}
	requireKeys(t, decodeKeys(t, ext), "clusterName", "kubeconfigSecretRef")
	requireKeys(t, decodeKeys(t, APIResourceSummary{Group: "g", Version: "v", Kind: "k"}),
		"group", "version", "kind", "namespaced")
}

func TestStatusConditionHelpers(t *testing.T) {
	fixed := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	restore := now
	now = func() metav1.Time { return metav1.NewTime(fixed) }
	t.Cleanup(func() { now = restore })

	t.Run("ExternalK8sCluster", func(t *testing.T) {
		var s ExternalK8sClusterStatus
		s.SetCondition(metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Synced"})
		got := s.GetCondition("Ready")
		if got == nil || got.Status != metav1.ConditionTrue {
			t.Fatalf("GetCondition = %+v", got)
		}
		if !got.LastTransitionTime.Equal(&metav1.Time{Time: fixed}) {
			t.Fatalf("零值 LastTransitionTime 应被补成注入时钟, got %v", got.LastTransitionTime)
		}

		// 同类型再次设置应覆盖而非追加，且保留调用方给的时间。
		explicit := metav1.NewTime(fixed.Add(time.Hour))
		s.SetCondition(metav1.Condition{
			Type: "Ready", Status: metav1.ConditionFalse, Reason: "Drifted", LastTransitionTime: explicit,
		})
		if len(s.Conditions) != 1 {
			t.Fatalf("Conditions = %d 条, want 1（同类型应合并）", len(s.Conditions))
		}
		if got := s.GetCondition("Ready"); got.Reason != "Drifted" || !got.LastTransitionTime.Equal(&explicit) {
			t.Fatalf("合并后 = %+v", got)
		}

		s.RemoveCondition("Ready")
		if s.GetCondition("Ready") != nil {
			t.Fatal("RemoveCondition 后仍能查到")
		}
		s.RemoveCondition("Ready") // 不存在时必须是 no-op
	})

	t.Run("HelmRelease", func(t *testing.T) {
		var s HelmReleaseStatus
		s.SetCondition(metav1.Condition{Type: "Deployed", Status: metav1.ConditionTrue, Reason: "Install"})
		if got := s.GetCondition("Deployed"); got == nil || got.Reason != "Install" {
			t.Fatalf("GetCondition = %+v", got)
		}
		s.RemoveCondition("Deployed")
		if s.GetCondition("Deployed") != nil {
			t.Fatal("RemoveCondition 后仍能查到")
		}
	})

	t.Run("OpsMeshInstance", func(t *testing.T) {
		var s OpsMeshInstanceStatus
		s.SetCondition(metav1.Condition{Type: ConditionTypeReady, Status: metav1.ConditionTrue, Reason: "Synced"})
		got := s.GetCondition(ConditionTypeReady)
		if got == nil || got.LastTransitionTime.IsZero() {
			t.Fatalf("GetCondition = %+v, 期望自动补 LastTransitionTime", got)
		}
		if ConditionTypeReady != "Ready" || ConditionTypeProgressing != "Progressing" || ConditionTypeError != "Error" {
			t.Fatal("条件类型常量被改动（reconciler 与 CRD 契约依赖字面值）")
		}
		s.RemoveCondition(ConditionTypeReady)
		if s.GetCondition(ConditionTypeReady) != nil {
			t.Fatal("RemoveCondition 后仍能查到")
		}
	})
}

// DeepCopy 必须彻底隔离切片/映射/指针，否则共享 informer 缓存会被就地污染。
func TestDeepCopyIsolation(t *testing.T) {
	instance := &OpsMeshInstance{}
	instance.Name = "mesh"
	instance.Spec.MySQL = MySQLSpec{
		Enabled:           true,
		Storage:           "10Gi",
		PasswordSecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "mysql-secret"}},
	}
	instance.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}}

	instanceCopy := instance.DeepCopy()
	instanceCopy.Spec.MySQL.PasswordSecretRef.Name = "other"
	instanceCopy.Status.Conditions[0].Reason = "changed"
	if instance.Spec.MySQL.PasswordSecretRef.Name != "mysql-secret" {
		t.Fatal("SecretKeySelector 指针未深拷贝")
	}
	if instance.Status.Conditions[0].Reason == "changed" {
		t.Fatal("Conditions 切片未深拷贝")
	}

	objCopy := instance.DeepCopyObject()
	if _, same := objCopy.(*OpsMeshInstance); !same {
		t.Fatalf("DeepCopyObject 类型 = %T", objCopy)
	}
	if objCopy.(*OpsMeshInstance) == instance {
		t.Fatal("DeepCopyObject 返回了同一指针")
	}

	ext := &ExternalK8sCluster{}
	ext.Status.CRDNames = []string{"a.example.com"}
	ext.Status.APIResourceList = []APIResourceSummary{{Group: "g", Version: "v1", Kind: "K"}}
	syncTime := metav1.NewTime(time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC))
	ext.Status.LastSyncTime = &syncTime
	extCopy := ext.DeepCopy()
	extCopy.Status.CRDNames[0] = "b.example.com"
	extCopy.Status.APIResourceList[0].Kind = "Other"
	extCopy.Status.LastSyncTime.Time = time.Time{}
	if ext.Status.CRDNames[0] != "a.example.com" {
		t.Fatal("CRDNames 未深拷贝")
	}
	if ext.Status.APIResourceList[0].Kind != "K" {
		t.Fatal("APIResourceList 未深拷贝")
	}
	if ext.Status.LastSyncTime.IsZero() {
		t.Fatal("LastSyncTime 指针未深拷贝")
	}

	// Values 的取值来自 JSON 反序列化，数值只会是 float64（native int 不是合法
	// JSON 值，Runtime.DeepCopyJSONValue 对其会 panic —— 与 controller-gen 生成物一致）。
	release := &HelmRelease{}
	release.Spec.Values = map[string]interface{}{
		"replicas": float64(1),
		"nested":   map[string]interface{}{"k": "v"},
	}
	release.Spec.ValuesFileRef = &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "values"},
	}
	releaseCopy := release.DeepCopy()
	releaseCopy.Spec.Values["replicas"] = float64(3)
	releaseCopy.Spec.Values["nested"].(map[string]interface{})["k"] = "changed"
	releaseCopy.Spec.ValuesFileRef.Name = "other"
	if release.Spec.Values["replicas"] != float64(1) {
		t.Fatal("Values 顶层未深拷贝")
	}
	if release.Spec.Values["nested"].(map[string]interface{})["k"] != "v" {
		t.Fatal("Values 嵌套 map 未深拷贝（会与 informer 缓存共享底层数据）")
	}
	if release.Spec.ValuesFileRef.Name != "values" {
		t.Fatal("ValuesFileRef 指针未深拷贝")
	}

	list := &OpsMeshInstanceList{Items: []OpsMeshInstance{*instance}}
	listCopy := list.DeepCopy()
	listCopy.Items[0].Name = "renamed"
	if list.Items[0].Name != "mesh" {
		t.Fatal("List.Items 未深拷贝")
	}
	if list.DeepCopyObject() == nil {
		t.Fatal("List.DeepCopyObject 返回 nil")
	}
}
