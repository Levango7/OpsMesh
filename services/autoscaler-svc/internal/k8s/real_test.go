package k8s

// real_test.go 用 httptest 假 APIServer 走**真实 client-go 编解码链路**验证执行器，
// 而不是用 fake clientset：后者只会告诉我"我调了哪个方法"，不会告诉我
// "请求真的发到 /apis/apps/v1/namespaces/<ns>/deployments/<name>、body 里真的是
// 一个只改 spec.replicas 的 merge patch"。这一层正是 #61 那个造假面的判定面——
// 此前这里连一个字节都没发出去过。

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// deploymentJSON 构造最小可用的 Deployment 响应体。
//
// apiVersion/kind 必须写全：client-go 靠它们解码。这里刻意用 Deployment 而不是 Scale
// 类型，原因见 real.go 顶部——apps/v1 的 scheme 没注册 Scale。
func deploymentJSON(name, ns string, replicas int32) string {
	return `{
	  "apiVersion": "apps/v1",
	  "kind": "Deployment",
	  "metadata": {"name": "` + name + `", "namespace": "` + ns + `", "resourceVersion": "9"},
	  "spec": {"replicas": ` + strconv.FormatInt(int64(replicas), 10) + `,
	           "selector": {"matchLabels": {"app": "` + name + `"}},
	           "template": {"metadata": {"labels": {"app": "` + name + `"}},
	                         "spec": {"containers": [{"name": "c", "image": "nginx"}]}}},
	  "status": {"replicas": ` + strconv.FormatInt(int64(replicas), 10) + `}
	}`
}

// fakeAPIServer 返回一个只认 Deployment 读写的假 APIServer，并记录收到的写请求。
type recordedWrite struct {
	path        string
	contentType string
	body        map[string]interface{}
}

func fakeAPIServer(t *testing.T, current int32) (string, *[]recordedWrite) {
	t.Helper()
	var writes []recordedWrite
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		name := "api"
		// 只有 /apis/apps/v1/namespaces/<ns>/deployments/api 这一个对象存在，
		// 其它名字一律 404——"目标不存在必须报错"这条断言靠的就是这里不兜底。
		target := strings.HasPrefix(r.URL.Path, "/apis/apps/v1/namespaces/") &&
			strings.HasSuffix(r.URL.Path, "/deployments/"+name)
		switch {
		case r.Method == http.MethodGet && target:
			_, _ = w.Write([]byte(deploymentJSON(name, nsOf(r.URL.Path), current)))
		case r.Method == http.MethodPatch && target:
			b, _ := io.ReadAll(r.Body)
			var obj map[string]interface{}
			if err := json.Unmarshal(b, &obj); err != nil {
				t.Errorf("假 APIServer 收到无法解码的 patch body: %v (%s)", err, b)
			}
			writes = append(writes, recordedWrite{path: r.URL.Path, contentType: r.Header.Get("Content-Type"), body: obj})
			rep := int32(0)
			if spec, ok := obj["spec"].(map[string]interface{}); ok {
				if f, ok := spec["replicas"].(float64); ok {
					rep = int32(f)
				}
			}
			_, _ = w.Write([]byte(deploymentJSON(name, nsOf(r.URL.Path), rep)))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"Status","status":"Failure","reason":"NotFound","code":404}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &writes
}

// nsOf 从 /apis/apps/v1/namespaces/<ns>/deployments/<name> 里取 <ns>。
func nsOf(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, p := range parts {
		if p == "namespaces" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func newRealScalerForURL(t *testing.T, url string) *RealScaler {
	t.Helper()
	cs, err := kubernetes.NewForConfig(&rest.Config{Host: url})
	if err != nil {
		t.Fatalf("kubernetes.NewForConfig: %v", err)
	}
	return newWithClientset(cs, ExecutorKubeconfig)
}

func TestRealScaler_GetReplicas(t *testing.T) {
	url, _ := fakeAPIServer(t, 2)
	rs := newRealScalerForURL(t, url)

	got, err := rs.GetReplicas("api", "prod")
	if err != nil {
		t.Fatalf("GetReplicas: %v", err)
	}
	if got != 2 {
		t.Fatalf("GetReplicas = %d, want 2（假 APIServer 返回的就是 2）", got)
	}
	if rs.Executor() != ExecutorKubeconfig {
		t.Fatalf("Executor = %q, want %q", rs.Executor(), ExecutorKubeconfig)
	}
	if IsSimulated(rs.Executor()) {
		t.Fatal("真实执行器被判定为模拟")
	}
}

// TestRealScaler_SetReplicas_IsReplicasOnlyMergePatch 钉住两件事：
// 请求确实发到目标 Deployment，且 body 只动 spec.replicas。
// 后者是"扩容不会顺手改坏别的字段"的唯一证据——换成全量 Update 这条就会红。
func TestRealScaler_SetReplicas_IsReplicasOnlyMergePatch(t *testing.T) {
	url, writes := fakeAPIServer(t, 2)
	rs := newRealScalerForURL(t, url)

	if err := rs.SetReplicas("api", "prod", 5); err != nil {
		t.Fatalf("SetReplicas: %v", err)
	}
	if len(*writes) != 1 {
		t.Fatalf("收到 %d 次写请求，期望恰好 1 次", len(*writes))
	}
	w := (*writes)[0]
	if w.path != "/apis/apps/v1/namespaces/prod/deployments/api" {
		t.Fatalf("写请求路径 = %q", w.path)
	}
	if w.contentType != "application/merge-patch+json" {
		t.Fatalf("patch 类型 = %q, want application/merge-patch+json（merge 语义才不会覆盖别的字段）", w.contentType)
	}
	if len(w.body) != 1 {
		t.Fatalf("patch 顶层字段 = %v，期望只有 spec", w.body)
	}
	spec, _ := w.body["spec"].(map[string]interface{})
	if len(spec) != 1 || spec["replicas"] != float64(5) {
		t.Fatalf("patch 的 spec = %v，期望只有 replicas=5", spec)
	}
}

func TestRealScaler_EmptyNamespaceDefaultsToDefault(t *testing.T) {
	url, writes := fakeAPIServer(t, 1)
	rs := newRealScalerForURL(t, url)
	if err := rs.SetReplicas("api", "", 3); err != nil {
		t.Fatalf("SetReplicas: %v", err)
	}
	if len(*writes) != 1 || !strings.Contains((*writes)[0].path, "/namespaces/default/") {
		t.Fatalf("空 namespace 未回落 default：%v", *writes)
	}
}

func TestRealScaler_RejectsBadArgumentsWithoutWriting(t *testing.T) {
	url, writes := fakeAPIServer(t, 1)
	rs := newRealScalerForURL(t, url)
	if _, err := rs.GetReplicas("", "prod"); err == nil {
		t.Fatal("空 deployment 名应报错")
	}
	if err := rs.SetReplicas("", "prod", 2); err == nil {
		t.Fatal("空 deployment 名应报错")
	}
	if err := rs.SetReplicas("api", "prod", -1); err == nil {
		t.Fatal("负副本数应报错")
	}
	if len(*writes) != 0 {
		t.Fatalf("参数校验失败却发出了写请求：%v", *writes)
	}
}

func TestRealScaler_NotFoundSurfacesError(t *testing.T) {
	url, _ := fakeAPIServer(t, 1)
	rs := newRealScalerForURL(t, url)
	if _, err := rs.GetReplicas("ghost", "nowhere"); err == nil {
		t.Fatal("目标不存在时应返回错误而不是 0 副本")
	}
	if err := rs.SetReplicas("ghost", "nowhere", 2); err == nil {
		t.Fatal("目标不存在时写操作应返回错误（否则会被记成「已扩容」）")
	}
}

func TestNewFromKubeconfig_EmptyPathFails(t *testing.T) {
	// 这一条不是细节洁癖：AUTOSCALER_K8S_EXECUTOR=kubeconfig 而凭据没配时，
	// 必须启动失败，绝不能静默退回内存执行器（那正是本轮修掉的缺陷形态）。
	if _, err := NewFromKubeconfig(""); err == nil {
		t.Fatal("kubeconfig 路径为空必须报错")
	}
}

func TestClient_ExecutorIsSimulated(t *testing.T) {
	c := NewClient()
	if c.Executor() != ExecutorSimulated {
		t.Fatalf("内存执行器身份 = %q, want %q", c.Executor(), ExecutorSimulated)
	}
	if !IsSimulated(c.Executor()) {
		t.Fatal("内存执行器必须被判定为模拟")
	}
	// 编译期断言：两种实现都满足对外接口，业务层不需要知道差别。
	var _ Scaler = c
	var _ Scaler = (*RealScaler)(nil)
}
