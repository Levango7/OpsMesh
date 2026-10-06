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
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/metrics"
	"github.com/Levango7/OpsMesh/internal/plugin"
)

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

func TestLoadPluginManifestHappyPath(t *testing.T) {
	t.Setenv(testTokenEnv, "tok")
	specs, err := loadPluginManifest(writeManifest(t, manifestFor("http://127.0.0.1:18123/hook")), true)
	if err != nil {
		t.Fatalf("合法清单被拒：%v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("应解析出 1 个插件，实际 %d", len(specs))
	}
	s := specs[0]
	if s.Name != "guard" || s.Token != "tok" || s.Timeout != 2*time.Second {
		t.Errorf("字段不符：%+v", s)
	}
	if len(s.Hooks) != 1 || s.Hooks[0] != plugin.HookConfigPreSet {
		t.Errorf("扩展点映射不符：%v", s.Hooks)
	}
}

func TestLoadPluginManifestRejects(t *testing.T) {
	good := manifestFor("http://127.0.0.1:18123/hook")
	cases := []struct {
		name    string
		body    string
		token   string
		allowP  bool
		wantSub string
	}{
		{"文件不存在", "__NOFILE__", "tok", true, "读取失败"},
		{"坏 JSON", `{"plugins":[`, "tok", true, "解析失败"},
		{"空 plugins", `{"plugins":[]}`, "tok", true, "plugins 为空"},
		{"未知字段（token 明文）", `{"plugins":[{"name":"a","url":"http://127.0.0.1:1/","hooks":["config.preSet"],"token":"x"}]}`, "tok", true, "解析失败"},
		{"未知字段名拼错", strings.Replace(good, `"timeoutMs"`, `"timeout_ms"`, 1), "tok", true, "解析失败"},
		{"空插件名", strings.Replace(good, `"name":"guard"`, `"name":"  "`, 1), "tok", true, "name 为空"},
		{"插件名重复", `{"plugins":[` + entry("guard", "http://127.0.0.1:1/hook") + `,` +
			entry("guard", "http://127.0.0.1:2/hook") + `]}`, "tok", true, "重复"},
		{"hooks 为空", strings.Replace(good, `"hooks":["config.preSet"]`, `"hooks":[]`, 1), "tok", true, "hooks 为空"},
		{"未冻结的扩展点", strings.Replace(good, "config.preSet", "config.makeUp", 1), "tok", true, "未知扩展点"},
		{"缺 tokenEnv", strings.Replace(good, `"tokenEnv":"`+testTokenEnv+`",`, ``, 1), "tok", true, "tokenEnv"},
		{"tokenEnv 指向未设置变量", good, "", true, "为空或未设置"},
		{"超时越界", strings.Replace(good, `"timeoutMs":2000`, `"timeoutMs":999999`, 1), "tok", true, "越界"},
		{"超时为负", strings.Replace(good, `"timeoutMs":2000`, `"timeoutMs":-1`, 1), "tok", true, "越界"},
		{"环回 URL 未开 allow-private", good, "tok", false, "SSRF"},
		{"非 http 协议", strings.Replace(good, "http://127.0.0.1:18123", "file:///tmp", 1), "tok", true, "SSRF"},
		{"元数据地址即使开开关也拒", strings.Replace(good, "http://127.0.0.1:18123", "http://169.254.169.254", 1), "tok", true, "SSRF"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.token == "" {
				t.Setenv(testTokenEnv, "")
				_ = os.Unsetenv(testTokenEnv)
			} else {
				t.Setenv(testTokenEnv, c.token)
			}
			path := writeManifest(t, c.body)
			if c.body == "__NOFILE__" {
				path = filepath.Join(t.TempDir(), "not-there.json")
			}
			if _, err := loadPluginManifest(path, c.allowP); err == nil {
				t.Fatalf("应判错：%s", c.name)
			} else if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("错误信息应含 %q，实际：%v", c.wantSub, err)
			}
		})
	}
}

func TestInitPluginHostDisabledIsNoOp(t *testing.T) {
	prev := PluginManager()
	m := metrics.New()
	mgr, n, err := initPluginHost(&config.Config{}, m)
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

// entry 生成一个插件对象字面量（重复名用例需要手写两个条目，字符串拼接读不懂）。
func entry(name, url string) string {
	return `{"name":"` + name + `","url":"` + url +
		`","hooks":["config.preSet"],"tokenEnv":"` + testTokenEnv + `"}`
}

func TestInitPluginHostBuildsManager(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"decision":"allow"}`)
	}))
	defer srv.Close()

	t.Setenv(testTokenEnv, "tok")
	m := metrics.New()
	mgr, n, err := initPluginHost(&config.Config{PluginManifest: writeManifest(t, manifestFor(srv.URL, "config.preSet", "config.postSet")), PluginAllowPrivate: true}, m)
	if err != nil {
		t.Fatalf("初始化失败：%v", err)
	}
	if n != 1 || mgr == nil {
		t.Fatalf("应注册 1 个插件，实际 n=%d mgr=%v", n, mgr)
	}
	if got := len(mgr.AllPlugins()); got != 1 {
		t.Errorf("AllPlugins 应为 1，实际 %d", got)
	}
	if err := mgr.FireHook(nil, plugin.HookConfigPreSet, plugin.Event{Name: "x"}); err != nil {
		t.Errorf("放行场景不应报错：%v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("插件端点应被调用 1 次，实际 %d", got)
	}
	// 计数：ok 一次。
	assertPluginMetric(t, m, "config.preSet", "ok", 1)
}

// TestNewServerActuallyWiresPluginManager 是本轮的**核心断言**：
// 生产启动路径（NewServer）必须真的构造过插件宿主。
//
// TD-62 之所以能潜伏那么久，就是因为 SetPluginManager 只有测试调用——
// 只测 initPluginHost 会留下同一个盲区（"函数正确但从没被调用"）。
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
	// Production=true 时存储后端也会失败，所以这里直接断言 initPluginHost 的判定本身，
	// 并单独验证 NewServer 里那条分支用的是同一个判据（见下）。
	if _, _, err := initPluginHost(&config.Config{Production: true, PluginManifest: path, PluginAllowPrivate: true}, metrics.New()); err == nil {
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
// 与生产同构（走 initPluginHost + SetPluginManager），而不是手工塞一个 handler。
func newPluginWiredTestServer(t *testing.T, url string, hooks ...string) *Server {
	t.Helper()
	t.Setenv(testTokenEnv, "tok")
	s := newAPIKeyTestServer()
	s.metrics = metrics.New()
	mgr, n, err := initPluginHost(&config.Config{
		PluginManifest:     writeManifest(t, manifestFor(url, hooks...)),
		PluginAllowPrivate: true,
	}, s.metrics)
	if err != nil {
		t.Fatalf("initPluginHost 失败：%v", err)
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
