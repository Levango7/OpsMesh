// remote_test.go pluginhost 包的真机用例（TD-87 批 1：自 controlplane/plugin_remote_test.go 拆出）。
//
// 只含「不依赖 Server」的用例（manifest 解析 / 插件宿主装配）；Server 级用例
// （TestNewServerActuallyWiresPluginManager 等）留在父包 plugin_remote_test.go。
package pluginhost

import (
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
	specs, err := LoadPluginManifest(writeManifest(t, manifestFor("http://127.0.0.1:18123/hook")), true)
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
			if _, err := LoadPluginManifest(path, c.allowP); err == nil {
				t.Fatalf("应判错：%s", c.name)
			} else if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("错误信息应含 %q，实际：%v", c.wantSub, err)
			}
		})
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
	mgr, n, err := InitPluginHost(&config.Config{PluginManifest: writeManifest(t, manifestFor(srv.URL, "config.preSet", "config.postSet")), PluginAllowPrivate: true}, m)
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
// 只测 InitPluginHost 会留下同一个盲区（"函数正确但从没被调用"）。

// assertPluginMetric 从渲染出的 exposition 里取某条序列的值（按包各自持有测试替身的既有惯例，
// 父包 plugin_remote_test.go 保留同名一份）。
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
