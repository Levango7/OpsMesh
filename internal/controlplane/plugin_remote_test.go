// plugin_remote_test.go 覆盖独立进程插件的控制面接线（TD-62）。
//
// 分两层，缺一不可：
//   - 清单解析/校验层：配错的形态五花八门，但共同症状是"静默不生效"，所以每种都要判错；
//   - 端到端层：必须走一次真实的 PUT /api/v1/platform/config，
//     证明"NewServer 里接的那条线"真的能通到扩展点，而不是只存在于单元测试的注入里。
//     （这正是 TD-62 原缺陷的成因：Manager 与 handler 都对，但生产进程里没人构造过它。）
package controlplane

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/controlplane/pluginhost"
	"github.com/Levango7/OpsMesh/internal/metrics"
	"github.com/Levango7/OpsMesh/internal/plugin"
)

// writeManifest 写一个临时清单文件并返回路径。
// writeManifest 写一个临时清单文件并返回路径。
func writeManifest(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "plugins.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("写清单失败：%v", err)
	}
	return p
}

const testTokenEnv = "OPSMESH_TEST_PLUGIN_TOKEN"

// manifestFor 生成一个最小合法清单（URL 由调用方给）。
func manifestFor(url string, hooks ...string) string {
	if len(hooks) == 0 {
		hooks = []string{"config.preSet"}
	}
	return `{"plugins":[{"name":"guard","version":"1.0.0","url":"` + url +
		`","hooks":["` + strings.Join(hooks, `","`) +
		`"],"tokenEnv":"` + testTokenEnv + `","timeoutMs":2000}]}`
}

func TestNewServerActuallyWiresPluginManager(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, `{"decision":"allow"}`)
	}))
	defer srv.Close()

	t.Setenv(testTokenEnv, "tok")
	prev := PluginManager()
	t.Cleanup(func() { SetPluginManager(prev) })
	SetPluginManager(nil)

	cfg := &config.Config{
		Mode:               "controlplane",
		Store:              "memory",
		EventBus:           "noop",
		TaskMaxRetries:     3,
		GRPCPort:           0,
		HTTPPort:           0,
		MetricsPort:        0,
		PluginManifest:     writeManifest(t, manifestFor(srv.URL)),
		PluginAllowPrivate: true,
	}
	s, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer 失败：%v", err)
	}

	if PluginManager() == nil {
		t.Fatal("NewServer 之后全局插件管理器仍为 nil —— 接线没进生产路径（TD-62 原缺陷复发）")
	}
	if err := s.firePluginHook(nil, plugin.HookConfigPreSet, plugin.Event{Name: "probe"}); err != nil {
		t.Errorf("钩子应放行，实际 %v", err)
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("经 NewServer 接线的宿主应真的打到插件端点（hits=%d）", got)
	}
}

// TestNewServerProductionFailsFastOnBadManifest 钉住 fail-fast：
// 生产模式下清单配错必须让启动失败，而不是"看起来起来了但没有插件"。
func TestNewServerProductionFailsFastOnBadManifest(t *testing.T) {
	t.Setenv(testTokenEnv, "tok")
	path := writeManifest(t, `{"plugins":[{"name":"guard","url":"http://127.0.0.1:1/hook","hooks":["config.makeUp"],"tokenEnv":"`+testTokenEnv+`"}]}`)
	// Production=true 时存储后端也会失败，所以这里直接断言 pluginhost.InitPluginHost 的判定本身，
	// 并单独验证 NewServer 里那条分支用的是同一个判据（见下）。
	if _, _, err := pluginhost.InitPluginHost(&config.Config{Production: true, PluginManifest: path, PluginAllowPrivate: true}, metrics.New()); err == nil {
		t.Fatal("坏清单必须返回 error")
	}
	_, err := NewServer(&config.Config{
		Production: true, Mode: "controlplane", Store: "memory", EventBus: "noop",
		TaskMaxRetries: 3, GRPCPort: 0, HTTPPort: 0, MetricsPort: 0,
		PluginManifest:     path,
		PluginAllowPrivate: true,
	})
	if err == nil {
		t.Fatal("生产模式 + 坏清单时 NewServer 必须返回 error")
	}
	if !strings.Contains(err.Error(), "插件宿主") {
		t.Errorf("错误应指明是插件宿主失败（否则归因会跑到存储层去），实际：%v", err)
	}
}

// TestRemotePluginDenyBlocksRealConfigWrite 端到端：插件 deny ⇒ HTTP 400 且不落库。
func TestRemotePluginDenyBlocksRealConfigWrite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"decision":"deny","reason":"不允许把 maxTenants 改成 0"}`)
	}))
	defer srv.Close()

	s := newPluginWiredTestServer(t, srv.URL, "config.preSet")
	req := httptest.NewRequest(http.MethodPut, "/api/v1/platform/config", strings.NewReader(`{"maxTenants":0}`))
	req.Header.Set("Authorization", loginAsAdmin(t, s))
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleUpdatePlatformConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("deny 应返回 400，实际 %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "plugin policy") {
		t.Errorf("响应体应说明被插件策略拒绝：%s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "maxTenants") {
		t.Error("插件的 reason 不应原样回吐给客户端（内部策略细节外泄）")
	}
	if got := persistedMaxTenants(t, s); got != nil {
		t.Errorf("被阻断的写入不该落库，实际读到 %v", *got)
	}
	assertPluginMetric(t, s.metrics, "config.preSet", "denied", 1)
}

// TestRemotePluginAllowPersistsRewrittenPayload 端到端：插件改写负载后，落库的必须是改写后的值。
//
// 这条证明的是"远程插件与进程内插件同等能力"里最关键的一半：
// 不只是能不能否决，而是 preSet 的改写会不会被丢掉（丢掉的话宣传里的"准入校验+派生策略"就是空的）。
func TestRemotePluginAllowPersistsRewrittenPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"decision":"allow","payload":{"maxTenants":30}}`)
	}))
	defer srv.Close()

	s := newPluginWiredTestServer(t, srv.URL, "config.preSet")
	req := httptest.NewRequest(http.MethodPut, "/api/v1/platform/config", strings.NewReader(`{"maxTenants":7}`))
	req.Header.Set("Authorization", loginAsAdmin(t, s))
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleUpdatePlatformConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("放行时应 200，实际 %d: %s", w.Code, w.Body.String())
	}
	got := persistedMaxTenants(t, s)
	if got == nil || *got != 30 {
		t.Errorf("落库值应是插件改写后的 30，实际 %v", got)
	}
	assertPluginMetric(t, s.metrics, "config.preSet", "ok", 1)
}

// TestRemotePluginUnreachableFailsClosedAndPostNeverBlocks 钉住两种失败语义：
// pre 不可达 ⇒ 阻断（fail-closed）；post 不可达 ⇒ 仍然 200（已落库的事实不回滚）。
func TestRemotePluginUnreachableFailsClosedAndPostNeverBlocks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	url := srv.URL
	srv.Close() // 端点立刻不可用

	s := newPluginWiredTestServer(t, url, "config.preSet")
	req := httptest.NewRequest(http.MethodPut, "/api/v1/platform/config", strings.NewReader(`{"maxTenants":7}`))
	req.Header.Set("Authorization", loginAsAdmin(t, s))
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleUpdatePlatformConfig(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("pre 钩子不可达时应阻断（拔掉插件不能等于绕过准入），实际 %d: %s", w.Code, w.Body.String())
	}
	assertPluginMetric(t, s.metrics, "config.preSet", "error", 1)

	// 同一端点改绑 postSet：业务已提交，失败只进审计。
	s2 := newPluginWiredTestServer(t, url, "config.postSet")
	req2 := httptest.NewRequest(http.MethodPut, "/api/v1/platform/config", strings.NewReader(`{"maxTenants":7}`))
	req2.Header.Set("Authorization", loginAsAdmin(t, s2))
	req2.Header.Set("X-Tenant-ID", "default")
	w2 := httptest.NewRecorder()
	s2.handleUpdatePlatformConfig(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("post 钩子失败不得改变响应（配置已落库），实际 %d: %s", w2.Code, w2.Body.String())
	}
	assertPluginMetric(t, s2.metrics, "config.postSet", "error", 1)
}

// --- 辅助 ---

// newPluginWiredTestServer 造一个"插件宿主已经接好"的测试服务：
// 与生产同构（走 pluginhost.InitPluginHost + SetPluginManager），而不是手工塞一个 handler。
func newPluginWiredTestServer(t *testing.T, url string, hooks ...string) *Server {
	t.Helper()
	t.Setenv(testTokenEnv, "tok")
	s := newAPIKeyTestServer()
	s.metrics = metrics.New()
	mgr, n, err := pluginhost.InitPluginHost(&config.Config{
		PluginManifest:     writeManifest(t, manifestFor(url, hooks...)),
		PluginAllowPrivate: true,
	}, s.metrics)
	if err != nil {
		t.Fatalf("pluginhost.InitPluginHost 失败：%v", err)
	}
	if n == 0 {
		t.Fatal("应注册到 1 个插件")
	}
	prev := PluginManager()
	t.Cleanup(func() { SetPluginManager(prev) })
	SetPluginManager(mgr)
	return s
}

// persistedMaxTenants 读回落库平台配置里的 maxTenants（nil=没有记录）。
//
// 用 maxTenants 而不是别的名字：它是 PlatformConfig 的真实字段（platform_config.go:31）。
// 先前草稿写的 alertRetentionDays **不在这个结构里**，JSON 解码会静默忽略，
// 于是"改写生效"的断言会在一个不存在的字段上恒真——这正是本仓反复踩过的"探针没自证目标"。
func persistedMaxTenants(t *testing.T, s *Server) *int {
	t.Helper()
	item, ok := s.store.GetConfig("default", platformConfigStoreKey)
	if !ok || item == nil || item.Value == "" {
		return nil
	}
	var v struct {
		MaxTenants *int `json:"maxTenants"`
	}
	if err := json.Unmarshal([]byte(item.Value), &v); err != nil {
		t.Fatalf("解析落库配置失败：%v（raw=%s）", err, item.Value)
	}
	return v.MaxTenants
}

// assertPluginMetric 从渲染出的 exposition 里取某条序列的值。
func assertPluginMetric(t *testing.T, m *metrics.M, hook, outcome string, want uint64) {
	t.Helper()
	line := "opsmesh_plugin_hook_calls_total{hook=\"" + hook + "\",outcome=\"" + outcome + "\"} "
	for _, l := range strings.Split(m.Render(), "\n") {
		if strings.HasPrefix(l, line) {
			var got uint64
			if _, err := fmt.Sscanf(strings.TrimPrefix(l, line), "%d", &got); err != nil {
				t.Fatalf("解析指标值失败：%v（%s）", err, l)
			}
			if got != want {
				t.Errorf("%s/%s 期望 %d，实际 %d", hook, outcome, want, got)
			}
			return
		}
	}
	t.Fatalf("渲染里没有 %s 这条序列", line)
}

func TestInitPluginHostDisabledIsNoOp(t *testing.T) {
	prev := PluginManager()
	m := metrics.New()
	mgr, n, err := pluginhost.InitPluginHost(&config.Config{}, m)
	if err != nil {
		t.Fatalf("未配置清单时不应报错：%v", err)
	}
	if mgr != nil || n != 0 {
		t.Errorf("未配置清单时必须返回空宿主（mgr=%v n=%d）", mgr, n)
	}
	if PluginManager() != prev {
		t.Error("未配置清单时不得改动全局插件管理器（否则会把测试注入的管理器覆盖掉）")
	}
}
