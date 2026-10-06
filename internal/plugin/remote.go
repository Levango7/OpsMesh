// remote.go 实现插件运行时模型（TD-62 决策点 ①）。
//
// # 选的是哪一种：独立进程 + HTTP 契约
//
// 三种候选里只有这一种能同时满足商用交付的硬约束：
//
//	Go plugin（plugin.Open）  要求插件与宿主用**同一 Go 版本、同一依赖图**编译，
//	                          且只能 linux/amd64 这类同平台同架构；客户改一行依赖
//	                          就得重编插件，二进制发布形态下不可运维（全仓 plugin.Open 命中 0）。
//	WASM                    需要引入运行时（wazero/wasmtime），依赖面从"零依赖手写"
//	                          变成"多一个沙箱要跟进 CVE"，而本产品只有 3 个扩展点，收益不成比例。
//	独立进程 + HTTP         与本产品既有形态同构：12 个微服务就是独立进程 + gRPC，
//	                          webhook 外发就是 HTTP + SSRF 校验 + 令牌。插件作者用任何
//	                          语言写一个 HTTP 服务即可接入，不碰宿主工具链。
//
// # 契约
//
// 控制面对每个绑定的扩展点发一次 POST：
//
//	POST <url>
//	Authorization: Bearer <token>
//	Content-Type: application/json
//	{"plugin":"<name>","hook":"config.preSet","name":"<事件名>","payload":<原负载 JSON>}
//
// 插件返回 2xx 且可选地带上：
//
//	{"decision":"allow"|"deny","reason":"<说明>","payload":{...}}
//
//   - decision 缺省为 allow；"deny" ⇒ handler 返回 error（pre 钩子据此阻断）。
//   - 非 2xx、超时、连接失败、JSON 解析失败同样返回 error。
//   - payload 用于改写负载：只有当宿主传进来的负载是**指针**时才生效
//     （config.preSet/postSet 传 *PlatformConfig），因为改写必须落在调用方还持有的那块内存上；
//     非指针负载（task.preClaim 传 agentID 字符串）无法就地改写，带上 payload 会被拒绝而不是静默丢弃。
//
// # 失败即阻断（fail-closed），这是刻意的
//
// 插件进程不可达时，pre 钩子返回 error ⇒ 控制面拒绝该次操作。
// 反面选择（不可达就放行）会让"拔掉插件"变成一条绕过准入策略的通道，
// 而准入类扩展点存在的意义恰恰是不可绕过。代价要说清楚：**插件挂掉会让平台配置写入被拒**，
// 所以插件必须与控制面同等级别运维；序列 opsmesh_plugin_hook_calls_total{outcome="error"} 就是给这个风险配的告警面。

package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"time"
)

// remoteBodyLimit 限制读取插件响应体的上限。
// 插件是半可信外部进程：不设上限的话，一个返回超大响应体的端点就能把控制面吃到 OOM
// （与 P1-4「无界内存缓冲加上限」同一条判据）。
const remoteBodyLimit = 1 << 20 // 1 MiB

// remoteReasonLimit 限制插件 reason 文本进入日志/错误信息前的长度。
const remoteReasonLimit = 200

// ErrDenied 标记"插件在线且明确投了反对票"这一种失败。
//
// 为什么需要一个哨兵而不是靠错误文本区分：控制面要为三种结局分别计数
// （ok / denied / error），而"插件拒绝"与"插件不可达"在业务含义上完全相反——
// 前者说明准入策略正在起作用，后者是要打电话的运维故障。
// 靠 strings.Contains 判区分，改一句错误文案就会把 denied 误记成 error。
var ErrDenied = errors.New("plugin: 远程插件明确拒绝")

// RemoteConfig 一个独立进程插件的接入参数。
//
// 由控制面从清单文件（--plugin-manifest）读出并**校验过**才构造：
// URL 协议白名单与 SSRF 准入在 internal/controlplane/plugin_remote.go 判定
// （本包刻意不依赖 internal/egress，保持插件框架零依赖）。
type RemoteConfig struct {
	Name    string // 插件唯一标识（进入请求体与错误信息）
	Version string // 插件版本，仅用于注册日志
	URL     string // 扩展点接收端（http/https）
	Token   string // 调用令牌，以 Authorization: Bearer 发出；空表示不启用（本包拒绝该形态）
	Hooks   []Hook // 绑定的扩展点集合
	Timeout time.Duration
	Client  *http.Client // nil ⇒ 用内部默认客户端（超时取 Timeout）
}

// RemotePlugin 是独立进程插件在宿主内的表示：实现 Plugin 接口，
// 把每个绑定扩展点的 handler 转成一次 HTTP 调用。
type RemotePlugin struct {
	cfg    RemoteConfig
	client *http.Client
}

var _ Plugin = (*RemotePlugin)(nil)

// hookRequest 发往插件的请求体。
type hookRequest struct {
	Plugin  string `json:"plugin"`
	Hook    string `json:"hook"`
	Name    string `json:"name,omitempty"`
	Payload any    `json:"payload,omitempty"`
}

// hookResponse 插件的响应体（全部字段可选）。
type hookResponse struct {
	Decision string          `json:"decision"`
	Reason   string          `json:"reason"`
	Payload  json.RawMessage `json:"payload"`
}

// NewRemotePlugin 构造并校验独立进程插件。
//
// 校验项都是"配错了必然静默失效"的那几类：空名、空 URL、空令牌、空扩展点集合、
// 非正的超时、扩展点不在冻结清单里。URL 的 SSRF 判定不在这里（见本文件包注释）。
func NewRemotePlugin(cfg RemoteConfig) (*RemotePlugin, error) {
	if strings.TrimSpace(cfg.Name) == "" {
		return nil, fmt.Errorf("plugin: 远程插件名为空")
	}
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, fmt.Errorf("plugin: 远程插件 %q 未配置 URL", cfg.Name)
	}
	if cfg.Token == "" {
		// 允许空令牌 = 控制面对插件的调用无法被插件侧鉴权，
		// 而"谁能触发准入判断"这件事必须可验证；宁可启动即报错，不要静默的无鉴权通道。
		return nil, fmt.Errorf("plugin: 远程插件 %q 未配置令牌（必须经 env 引用提供）", cfg.Name)
	}
	if len(cfg.Hooks) == 0 {
		return nil, fmt.Errorf("plugin: 远程插件 %q 没有绑定任何扩展点（等于不注册，判错而不是静默跳过）", cfg.Name)
	}
	frozen := map[Hook]bool{}
	for _, h := range AllHooks() {
		frozen[h] = true
	}
	for _, h := range cfg.Hooks {
		if !frozen[h] {
			return nil, fmt.Errorf("plugin: 远程插件 %q 绑定了未冻结的扩展点 %q（扩展点清单见 internal/plugin/hooks.go 的 AllHooks）", cfg.Name, h)
		}
	}
	if cfg.Timeout <= 0 {
		return nil, fmt.Errorf("plugin: 远程插件 %q 的超时必须为正（当前 %s）", cfg.Name, cfg.Timeout)
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	return &RemotePlugin{cfg: cfg, client: client}, nil
}

func (p *RemotePlugin) Name() string    { return p.cfg.Name }
func (p *RemotePlugin) Version() string { return p.cfg.Version }

// Init 无本地状态需要初始化：远端可达性留给首次调用（启动期打一次 /health 会把
// "插件此刻没起来"误判成配置错误，而插件晚于控制面启动是编排里的常态）。
func (p *RemotePlugin) Init(_ any) error { return nil }

// Close 释放空闲连接。不返回 error：插件进程由运维管理，宿主无权终止它。
func (p *RemotePlugin) Close() error {
	p.client.CloseIdleConnections()
	return nil
}

// Handler 返回该扩展点的 HookHandler（注册进 Manager 用）。
func (p *RemotePlugin) Handler(h Hook) HookHandler {
	return func(ev Event) error { return p.call(h, ev) }
}

// call 执行一次远程扩展点调用。
func (p *RemotePlugin) call(h Hook, ev Event) error {
	ctx := ev.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	body, err := json.Marshal(hookRequest{Plugin: p.cfg.Name, Hook: string(h), Name: ev.Name, Payload: ev.Payload})
	if err != nil {
		return fmt.Errorf("plugin: 远程插件 %q 请求体序列化失败: %w", p.cfg.Name, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("plugin: 远程插件 %q 请求构造失败: %w", p.cfg.Name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.cfg.Token)
	req.Header.Set("X-OpsMesh-Plugin-Hook", string(h))

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("plugin: 远程插件 %q 调用失败（hook=%s，按不可达处理，pre 钩子会因此阻断）: %w", p.cfg.Name, h, err)
	}
	defer func() { _, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, remoteBodyLimit)); _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, remoteBodyLimit))
	if err != nil {
		return fmt.Errorf("plugin: 远程插件 %q 响应读取失败: %w", p.cfg.Name, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("plugin: 远程插件 %q 返回非 2xx（status=%d）: %s", p.cfg.Name, resp.StatusCode, trimReason(string(raw)))
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil // 空响应 = 放行
	}
	var hr hookResponse
	if err := json.Unmarshal(raw, &hr); err != nil {
		return fmt.Errorf("plugin: 远程插件 %q 响应不是合法 JSON: %w", p.cfg.Name, err)
	}
	switch strings.ToLower(strings.TrimSpace(hr.Decision)) {
	case "", "allow":
	case "deny":
		return fmt.Errorf("%w（插件=%s，hook=%s）: %s", ErrDenied, p.cfg.Name, h, trimReason(hr.Reason))
	default:
		return fmt.Errorf("plugin: 远程插件 %q 返回了未知 decision=%q（只接受 allow/deny）", p.cfg.Name, hr.Decision)
	}
	if len(hr.Payload) == 0 {
		return nil
	}
	// 负载改写只在宿主传进"指针"时成立：改写必须落在调用方仍持有的那块内存上，
	// 否则（例如传 string 的 task.preClaim）插件的"改写"会静默无效——那比报错更糟。
	rv := reflect.ValueOf(ev.Payload)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("plugin: 远程插件 %q 回写了 payload，但扩展点 %s 的负载不是指针（%T），改写无法生效", p.cfg.Name, h, ev.Payload)
	}
	if err := json.Unmarshal(hr.Payload, rv.Interface()); err != nil {
		return fmt.Errorf("plugin: 远程插件 %q 回写的 payload 反序列化失败: %w", p.cfg.Name, err)
	}
	return nil
}

// trimReason 把插件给的自由文本压到可进日志的长度。
func trimReason(s string) string {
	s = strings.NewReplacer("\n", " ", "\r", " ").Replace(strings.TrimSpace(s))
	if len(s) > remoteReasonLimit {
		return s[:remoteReasonLimit] + "…"
	}
	if s == "" {
		return "(无说明)"
	}
	return s
}
