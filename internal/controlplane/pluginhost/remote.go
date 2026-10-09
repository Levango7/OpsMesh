// plugin_remote.go 把"独立进程 + HTTP 契约"这一插件运行时模型接到控制面启动路径上（TD-62）。
//
// # 为什么接线必须在生产里 fail-fast
//
// internal/plugin 的 Manager 早就完整，钩子触发点也早在 2026-10-05 补齐（plugin_host.go），
// 但**从来没有一个进程在启动时构造过 Manager**——SetPluginManager 在生产代码里零调用点，
// 只有测试在调（实测：见报告 §42）。后果就是本文件存在的理由：
// 能力在源码里成立，在交付物里不存在。这类缺口不会因为任何测试变红，
// 因为测试自己注入管理器；把它接进 NewServer 之后，"配错了"必须是启动期错误而不是运行期静默。
//
// # 校验为什么放在这里而不是 internal/plugin
//
// 依赖方向：internal/plugin 要保持零业务依赖的纯框架（与 internal/metrics 同一原则），
// 而 URL 的 SSRF 判定必须复用 internal/egress 这一条策略——
// 通知渠道、webhook、插件端点若各判一套，就是"保存时一套口径、运行期另一套口径"的老缺陷（M7 教训）。
//
// # 清单格式（JSON）
//
//	{
//	  "plugins": [
//	    {
//	      "name": "admission",
//	      "version": "1.0.0",
//	      "url": "http://127.0.0.1:9101/hook",
//	      "hooks": ["config.preSet"],
//	      "tokenEnv": "OPSMESH_PLUGIN_ADMISSION_TOKEN",
//	      "timeoutMs": 2000
//	    }
//	  ]
//	}
//
// 令牌只能经 env 引用（`tokenEnv`），清单文件里写明文令牌会被解析直接判错：
// 清单是要进版本库/配置中心的，令牌不该跟着走。
package pluginhost

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/egress"
	"github.com/Levango7/OpsMesh/internal/metrics"
	"github.com/Levango7/OpsMesh/internal/plugin"
)

// 插件调用的超时边界：默认 2s，上限 30s。
// 上限不是保守癖——扩展点调用是**同步**的（config.preSet 在 PUT 请求路径上），
// 一个允许配成 5 分钟的超时等于把控制面 API 的可用性交给插件进程。
const (
	pluginHookDefaultTimeout = 2 * time.Second
	pluginHookMaxTimeout     = 30 * time.Second
)

// pluginManifestFile 是清单文件的顶层结构。
type pluginManifestFile struct {
	Plugins []pluginManifestEntry `json:"plugins"`
}

// pluginManifestEntry 是单个插件的声明。
type pluginManifestEntry struct {
	Name      string   `json:"name"`
	Version   string   `json:"version"`
	URL       string   `json:"url"`
	Hooks     []string `json:"hooks"`
	TokenEnv  string   `json:"tokenEnv"`
	TimeoutMS int      `json:"timeoutMs"`
}

// pluginHookByName 把清单里的扩展点名字映射为常量。
//
// 刻意不直接 plugin.Hook(name)：那会让"清单里写了个没接线的钩子名"变成运行期静默无操作，
// 而运维会在插件日志里看到"从没被调用过"却查不出原因。未知名必须在这里判错。
func pluginHookByName(name string) (plugin.Hook, error) {
	for _, h := range plugin.AllHooks() {
		if string(h) == strings.TrimSpace(name) {
			return h, nil
		}
	}
	return "", fmt.Errorf("未知扩展点 %q（可用：%s）", name, strings.Join(knownHookNames(), ", "))
}

// knownHookNames 返回冻结清单的名字列表（错误信息与文档共用一处来源，避免口径分叉）。
func knownHookNames() []string {
	all := plugin.AllHooks()
	out := make([]string, 0, len(all))
	for _, h := range all {
		out = append(out, string(h))
	}
	return out
}

// LoadPluginManifest 读取并校验清单文件，返回可直接注册的插件列表。
//
// 全部校验都在注册之前完成（all-or-nothing）：半注册状态意味着"某几个扩展点被插件接管、
// 另一些没有"，而这种状态在日志里和完全没配插件一样安静。
func LoadPluginManifest(path string, allowPrivate bool) ([]plugin.RemoteConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("插件清单读取失败：%w", err)
	}
	var doc pluginManifestFile
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	// DisallowUnknownFields：拼错字段名（timeout 写成 timeout_ms、token 代替 tokenEnv）
	// 若不判错就会静默用默认值——"配了但没生效"正是 TD-62 这一整轮的病根。
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("插件清单解析失败（字段名须与 plugins/remote-example/README.md 一致，且不接受明文 token）：%w", err)
	}
	if len(doc.Plugins) == 0 {
		return nil, fmt.Errorf("插件清单 %s 里 plugins 为空——空清单不是「不启用」而是配置错误，不启用请留空 --plugin-manifest", path)
	}

	seen := map[string]bool{}
	var out []plugin.RemoteConfig
	for i, e := range doc.Plugins {
		where := fmt.Sprintf("plugins[%d]", i)
		name := strings.TrimSpace(e.Name)
		if name == "" {
			return nil, fmt.Errorf("%s：name 为空", where)
		}
		if seen[name] {
			return nil, fmt.Errorf("%s：插件名重复 %q（Manager 会拒绝同名注册，但重复一定是配错了）", where, name)
		}
		seen[name] = true

		if len(e.Hooks) == 0 {
			return nil, fmt.Errorf("插件 %q：hooks 为空（不绑定任何扩展点的插件等于不注册）", name)
		}
		hooks := make([]plugin.Hook, 0, len(e.Hooks))
		for _, h := range e.Hooks {
			hk, err := pluginHookByName(h)
			if err != nil {
				return nil, fmt.Errorf("插件 %q：%w", name, err)
			}
			hooks = append(hooks, hk)
		}

		if strings.TrimSpace(e.TokenEnv) == "" {
			return nil, fmt.Errorf("插件 %q：必须用 tokenEnv 指定令牌所在的环境变量（清单里不能写明文令牌）", name)
		}
		token := os.Getenv(e.TokenEnv)
		if token == "" {
			return nil, fmt.Errorf("插件 %q：环境变量 %s 为空或未设置（没有令牌的控制面调用无法被插件侧鉴权）", name, e.TokenEnv)
		}

		to := time.Duration(e.TimeoutMS) * time.Millisecond
		if e.TimeoutMS == 0 {
			to = pluginHookDefaultTimeout
		}
		if to <= 0 || to > pluginHookMaxTimeout {
			return nil, fmt.Errorf("插件 %q：timeoutMs=%d 越界（有效区间 1..%d，上限存在是因为扩展点调用在请求路径上是同步的）",
				name, e.TimeoutMS, int(pluginHookMaxTimeout/time.Millisecond))
		}

		// SSRF：与通知渠道共用同一条策略（见 internal/egress.ValidateURL）。
		// 拒绝即终止启动——"插件端点指向云元数据"这类配置一旦放行，
		// 插件返回的内容会被宿主 JSON 解析，等于给对方一个内网探测的反射面。
		if err := egress.ValidateURL(e.URL, allowPrivate); err != nil {
			return nil, fmt.Errorf("插件 %q：URL %q 未通过 SSRF 校验（内网端点需 --plugin-allow-private=true）: %w", name, e.URL, err)
		}

		out = append(out, plugin.RemoteConfig{
			Name:    name,
			Version: strings.TrimSpace(e.Version),
			URL:     e.URL,
			Token:   token,
			Hooks:   hooks,
			Timeout: to,
		})
	}
	return out, nil
}

// InitPluginHost 构造插件宿主：注册每个远程插件，并把它的扩展点 handler 接上指标计数。
//
// 返回 (nil, 0, nil) 表示本次进程不启用插件宿主（--plugin-manifest 为空）——
// 这是默认形态，此时三个扩展点无 handler，FireHook 直接放行，行为与启用前完全一致。
func InitPluginHost(cfg *config.Config, m *metrics.M) (*plugin.Manager, int, error) {
	if cfg == nil || strings.TrimSpace(cfg.PluginManifest) == "" {
		return nil, 0, nil
	}
	specs, err := LoadPluginManifest(cfg.PluginManifest, cfg.PluginAllowPrivate)
	if err != nil {
		return nil, 0, err
	}
	mgr := plugin.NewManager()
	for _, spec := range specs {
		rp, err := plugin.NewRemotePlugin(spec)
		if err != nil {
			// Manager 尚未注册任何插件，无需回滚注册；释放已构造插件的空闲连接即可。
			for _, p := range mgr.AllPlugins() {
				_ = p.Close()
			}
			return nil, 0, fmt.Errorf("插件宿主初始化失败：%w", err)
		}
		if err := mgr.Register(rp, nil); err != nil {
			return nil, 0, fmt.Errorf("插件宿主初始化失败：%w", err)
		}
		for _, h := range spec.Hooks {
			if err := mgr.RegisterHook(h, countedRemoteHandler(rp, h, m)); err != nil {
				return nil, 0, fmt.Errorf("插件宿主初始化失败：%w", err)
			}
		}
	}
	return mgr, len(specs), nil
}

// countedRemoteHandler 把远程 handler 包一层指标计数。
//
// 三类结局的区分只在 errors.Is(err, plugin.ErrDenied) 这一条判据上：
// 用错误文本区分会在改文案的那一天静默失真（把策略拒绝记成运维故障，或反过来）。
func countedRemoteHandler(rp *plugin.RemotePlugin, h plugin.Hook, m *metrics.M) plugin.HookHandler {
	inner := rp.Handler(h)
	return func(ev plugin.Event) error {
		err := inner(ev)
		if m != nil {
			switch {
			case err == nil:
				m.IncPluginHook(string(h), "ok")
			case errors.Is(err, plugin.ErrDenied):
				m.IncPluginHook(string(h), "denied")
			default:
				m.IncPluginHook(string(h), "error")
			}
		}
		return err
	}
}
