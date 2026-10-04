// Package notify 提供告警通知能力。当前实现 Webhook 推送（M7）。
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/Levango7/OpsMesh/internal/egress"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// webhookTimeout 是单次 webhook 推送的整体超时。
//
// 为什么必须有（本实现此前用的是 http.DefaultClient，Timeout 字段为 0）：
// DefaultClient 的 Transport 是全局共享的默认值，且无整体超时。一旦别处改过
// http.DefaultClient 的字段，本包的通知推送会静默继承那些改动；更实际的风险是
// 连接建立后服务端不回数据时请求会一直挂着，占用 goroutine 与连接池槽位——
// 告警风暴下（正是最需要通知的时刻）会把通知通道拖死。
const webhookTimeout = 10 * time.Second

// defaultEgressClient 是本包默认出网 client：带 SSRF 三层防护
// （建连时逐 IP 复检 / 每跳重定向复检 / 整体超时），且**恒拒私网**。
//
// ⚠️ 修复记录（2026-10-04）：此前此处是裸 http.DefaultClient，**完全绕过**了
// controlplane 的 SSRF 防护。后果不是"少了个校验"，而是配置与实际行为脱节：
//
//   - --webhook-allow-private 这个开关只作用于 controlplane 层的保存前校验，
//     notify 层根本不看它 ⇒ 管理员设了安全基线（false），通知照样能打内网；
//   - DefaultClient 的 CheckRedirect 为 nil ⇒ 默认跟随最多 10 跳重定向，
//     "校验通过的外网 URL"返回一个 302 就能把请求送到 169.254.169.254。
//
// 通知渠道恰恰是最常被配成内网网关（钉钉/飞书/企微内网接入）的地方，
// 也就是最容易被误配、或被利用来绕过策略的地方。
//
// 私网放行能力不是这里写死的：见 SetEgressClient / EgressClient，
// 由 controlplane 按 cfg.WebhookAllowPrivate 注入，使 notify 与 API 层
// **共用同一条策略**——不会出现"保存时放行、投递时拒绝"这种自相矛盾。
var defaultEgressClient = egress.NewClient(webhookTimeout, false)

// egressClient 当前使用的出网 client。启动期一次性注入后只读（见 SetEgressClient）。
var egressClient atomic.Pointer[http.Client]

func init() { egressClient.Store(defaultEgressClient) }

// SetEgressClient 替换本包出网 client（启动期调用，一次即可）。
//
// 用途：让 controlplane 按 --webhook-allow-private 注入与 API 层一致的策略。
// 传入 nil 表示恢复默认（恒拒私网）。**必须在服务开始接收流量前调用**——
// 之后并发读取指针不再变更，故无需加锁。
func SetEgressClient(c *http.Client) {
	if c == nil {
		c = defaultEgressClient
	}
	egressClient.Store(c)
}

// EgressClient 返回当前出网 client（供测试注入替身，避免依赖真实出网）。
func EgressClient() *http.Client { return egressClient.Load() }

// postJSON 将任意可 JSON 序列化的值 POST 到 webhook URL（Content-Type: application/json）。
// 仅在 HTTP 响应 status<300 时返回 nil。
func postJSON(url string, v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("notify: marshal: %w", err)
	}
	client := EgressClient()
	// 出网前再校验一次 URL：client 的 DialContext 已在建连时逐 IP 复检，
	// 这里的前置校验是 defense-in-depth——能在不发请求的前提下给出可读错误，
	// 且覆盖"配置写错"这种非攻击性的误配场景。
	//
	// allowPrivate 与 client 的构造参数保持同一口径：client 放行了私网，这里
	// 再拒一次就成了"配置说行、代码说不行"；反之 client 恒拒私网时这里也拒。
	if err := egress.ValidateURL(url, egressAllowsPrivate(client)); err != nil {
		return fmt.Errorf("notify: webhook URL rejected by egress policy: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), webhookTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("notify: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("notify: post %s: %w", url, err)
	}
	// 显式关闭 Body：不 Close 会让该连接无法回到连接池复用，每条告警都新建
	// 一条 TCP+TLS 连接。无需读取内容——本函数只关心状态码。
	_ = resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("notify: post %s returned %d", url, resp.StatusCode)
	}
	return nil
}

// egressAllowsPrivate 从 client 反推其 allowPrivate 口径，用于让前置 URL 校验
// 与 client 的实际策略保持一致。
//
// 做法：拿 client 的 CheckRedirect 语义无法直接读，故改为在 egress 包登记
// 每个 client 的策略（见 egress.AllowsPrivate），避免这里靠猜。
func egressAllowsPrivate(c *http.Client) bool { return egress.AllowsPrivate(c) }

// PostAlert 通过 HTTP POST 将告警推送到 webhook URL，JSON body 为 Alert 的完整序列化。
// 超时 10s，非阻塞调用方（goroutine 内使用）。
func PostAlert(url string, a *proto.Alert) error {
	if url == "" || a == nil {
		return nil
	}
	return postJSON(url, a)
}
