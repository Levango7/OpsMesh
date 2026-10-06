// remote_test.go 覆盖独立进程插件的 HTTP 契约（TD-62）。
//
// 这里的断言之所以逐条写死，是因为契约的两种失败形态都很安静：
// 静默放行（该拒的没拒）和静默无效（改写没落到宿主内存上）。
// 两者都不会让调用方看到错误，只会让"扩展点存在"这个宣称继续是假的。
package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// captureHandler 记录收到的请求，并按 given 应答。
func captureHandler(t *testing.T, got *hookRequest, gotAuth *string, resp string, status int) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("读取插件请求体失败：%v", err)
		}
		if got != nil && len(body) > 0 {
			if err := json.Unmarshal(body, got); err != nil {
				t.Errorf("插件请求体不是合法 JSON：%v（body=%s）", err, body)
			}
		}
		if gotAuth != nil {
			*gotAuth = r.Header.Get("Authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, resp)
	}
}

func testRemote(t *testing.T, url string, hooks []Hook) *RemotePlugin {
	t.Helper()
	rp, err := NewRemotePlugin(RemoteConfig{
		Name:    "t",
		Version: "0.0.1",
		URL:     url,
		Token:   "s3cret",
		Hooks:   hooks,
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewRemotePlugin 失败：%v", err)
	}
	t.Cleanup(func() { _ = rp.Close() })
	return rp
}

type samplePayload struct {
	Retention int  `json:"retention"`
	Locked    bool `json:"locked"`
}

func TestRemoteAllowOnEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "")
	}))
	defer srv.Close()

	rp := testRemote(t, srv.URL, []Hook{HookConfigPreSet})
	if err := rp.Handler(HookConfigPreSet)(Event{Name: "platform/config"}); err != nil {
		t.Errorf("空响应体应视为放行，实际 err=%v", err)
	}
}

func TestRemoteDecisionAllowAndDeny(t *testing.T) {
	cases := []struct {
		resp     string
		wantErr  bool
		wantDeny bool
		explain  string
	}{
		{`{"decision":"allow"}`, false, false, "显式 allow"},
		{`{}`, false, false, "缺 decision 即默认放行"},
		{`{"decision":"deny","reason":"禁止关闭审计"}`, true, true, "deny"},
		{`{"decision":"NOPE"}`, true, false, "未知 decision 必须判错而不是当放行"},
	}
	for _, c := range cases {
		t.Run(c.explain, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, c.resp)
			}))
			defer srv.Close()
			rp := testRemote(t, srv.URL, []Hook{HookConfigPreSet})
			err := rp.Handler(HookConfigPreSet)(Event{Name: "x"})
			if (err != nil) != c.wantErr {
				t.Fatalf("wantErr=%v，实际 err=%v", c.wantErr, err)
			}
			if c.wantDeny && !errors.Is(err, ErrDenied) {
				t.Errorf("deny 必须可用 errors.Is(err, ErrDenied) 判定（控制面靠它区分策略拒绝与运维故障），实际 %v", err)
			}
			if !c.wantDeny && err != nil && errors.Is(err, ErrDenied) {
				t.Errorf("非 deny 的失败被记成了 deny：%v", err)
			}
		})
	}
}

func TestRemoteNon2xxIsErrorNotDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	}))
	defer srv.Close()
	rp := testRemote(t, srv.URL, []Hook{HookTaskPreClaim})
	err := rp.Handler(HookTaskPreClaim)(Event{Name: "task/claim"})
	if err == nil {
		t.Fatal("非 2xx 必须返回 error（pre 钩子据此阻断）")
	}
	if errors.Is(err, ErrDenied) {
		t.Errorf("5xx 是运维故障，不能记成「插件明确拒绝」：%v", err)
	}
}

func TestRemoteSendsTokenAndRequestBody(t *testing.T) {
	var got hookRequest
	var auth string
	srv := httptest.NewServer(captureHandler(t, &got, &auth, `{"decision":"allow"}`, http.StatusOK))
	defer srv.Close()

	rp := testRemote(t, srv.URL, []Hook{HookConfigPreSet})
	if err := rp.Handler(HookConfigPreSet)(Event{Name: "platform/config", Payload: &samplePayload{Retention: 7}}); err != nil {
		t.Fatalf("调用失败：%v", err)
	}
	if auth != "Bearer s3cret" {
		t.Errorf("Authorization 头应为 Bearer <token>，实际 %q", auth)
	}
	if got.Plugin != "t" || got.Hook != string(HookConfigPreSet) || got.Name != "platform/config" {
		t.Errorf("请求体元信息不符：%+v", got)
	}
	if got.Payload == nil {
		t.Fatal("payload 必须随请求发出（插件看不到负载就无法判断）")
	}
	m, ok := got.Payload.(map[string]any)
	if !ok || fmt.Sprint(m["retention"]) != "7" {
		t.Errorf("payload 序列化不符：%v", got.Payload)
	}
}

func TestRemotePayloadRewriteLandsOnHostMemory(t *testing.T) {
	// 插件把 retention 改成 30：宿主侧那个 body 必须真的变成 30，
	// 否则"preSet 可改写"是假的——控制面随后落库的还是原值。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"decision":"allow","payload":{"retention":30,"locked":true}}`)
	}))
	defer srv.Close()

	body := &samplePayload{Retention: 7}
	rp := testRemote(t, srv.URL, []Hook{HookConfigPreSet})
	if err := rp.Handler(HookConfigPreSet)(Event{Payload: body}); err != nil {
		t.Fatalf("调用失败：%v", err)
	}
	if body.Retention != 30 || !body.Locked {
		t.Errorf("改写未落到宿主内存：%+v（期望 retention=30 locked=true）", body)
	}
}

func TestRemotePayloadRewriteOnNonPointerIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"payload":{"a":1}}`)
	}))
	defer srv.Close()

	rp := testRemote(t, srv.URL, []Hook{HookTaskPreClaim})
	// task.preClaim 传的是字符串负载：改写无法生效，必须报错而不是静默丢弃。
	err := rp.Handler(HookTaskPreClaim)(Event{Payload: "agent-1"})
	if err == nil {
		t.Fatal("非指针负载收到 payload 回写时应报错（静默无效比报错危险）")
	}
	if !strings.Contains(err.Error(), "不是指针") {
		t.Errorf("错误信息应说明原因，实际：%v", err)
	}
}

func TestRemoteBadJSONResponseIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"decision":`+`oops`)
	}))
	defer srv.Close()
	rp := testRemote(t, srv.URL, []Hook{HookConfigPostSet})
	if err := rp.Handler(HookConfigPostSet)(Event{}); err == nil {
		t.Fatal("坏 JSON 必须判错")
	}
}

func TestRemoteUnreachableIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	url := srv.URL
	srv.Close() // 立刻关掉：模拟"插件进程没起来"

	rp := testRemote(t, url, []Hook{HookConfigPreSet})
	err := rp.Handler(HookConfigPreSet)(Event{})
	if err == nil {
		t.Fatal("不可达必须返回 error —— pre 钩子 fail-closed 的前提就是它")
	}
	if errors.Is(err, ErrDenied) {
		t.Errorf("不可达不是策略拒绝：%v", err)
	}
}

func TestRemoteHonorsTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = io.WriteString(w, `{"decision":"allow"}`)
	}))
	defer srv.Close()

	rp, err := NewRemotePlugin(RemoteConfig{
		Name: "slow", URL: srv.URL, Token: "x",
		Hooks: []Hook{HookConfigPostSet}, Timeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	defer func() { _ = rp.Close() }()

	start := time.Now()
	if err := rp.Handler(HookConfigPostSet)(Event{}); err == nil {
		t.Fatal("超过 timeout 必须返回 error（否则一个卡住的插件会拖死请求路径）")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("超时未生效，耗时 %s", elapsed)
	}
}

func TestRemoteRespectsContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = io.WriteString(w, `{"decision":"allow"}`)
	}))
	defer srv.Close()
	rp := testRemote(t, srv.URL, []Hook{HookConfigPreSet})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ev := Event{Ctx: ctx}
	if err := rp.Handler(HookConfigPreSet)(ev); err == nil {
		t.Fatal("已取消的 context 必须让调用立即失败")
	}
}

func TestRemoteOversizedResponseDoesNotLeakWholeBody(t *testing.T) {
	// 响应体超过 1MiB 上限：只读上限内的字节，随后 JSON 解析失败判错。
	// 这条防的是"插件返回超大 bodies → 宿主 OOM"，与 P1-4 同一条判据。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"decision":"allow","junk":"`)
		_, _ = io.WriteString(w, strings.Repeat("A", 2<<20))
		_, _ = io.WriteString(w, `"}`)
	}))
	defer srv.Close()
	rp := testRemote(t, srv.URL, []Hook{HookConfigPostSet})
	if err := rp.Handler(HookConfigPostSet)(Event{}); err == nil {
		t.Fatal("被截断的响应必须判错，不能当成放行")
	}
}

func TestNewRemotePluginValidation(t *testing.T) {
	base := RemoteConfig{
		Name: "p", URL: "http://127.0.0.1:1/hook", Token: "t",
		Hooks: []Hook{HookConfigPreSet}, Timeout: time.Second,
	}
	cases := []struct {
		name   string
		mutate func(*RemoteConfig)
	}{
		{"空名", func(c *RemoteConfig) { c.Name = "  " }},
		{"空 URL", func(c *RemoteConfig) { c.URL = "" }},
		{"空令牌（无法让插件鉴权调用方）", func(c *RemoteConfig) { c.Token = "" }},
		{"未绑定扩展点", func(c *RemoteConfig) { c.Hooks = nil }},
		{"绑定未冻结的扩展点", func(c *RemoteConfig) { c.Hooks = []Hook{"made.up"} }},
		{"超时为 0", func(c *RemoteConfig) { c.Timeout = 0 }},
		{"超时为负", func(c *RemoteConfig) { c.Timeout = -time.Second }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := base
			c.mutate(&cfg)
			if _, err := NewRemotePlugin(cfg); err == nil {
				t.Errorf("应拒绝：%s", c.name)
			}
		})
	}
	if _, err := NewRemotePlugin(base); err != nil {
		t.Errorf("合法配置被拒：%v", err)
	}
}

func TestRemotePluginLifecycleAndInterface(t *testing.T) {
	rp := testRemote(t, "http://127.0.0.1:1/hook", []Hook{HookConfigPostSet})
	var p Plugin = rp
	if p.Name() != "t" || p.Version() != "0.0.1" {
		t.Errorf("Name/Version 不符：%q %q", p.Name(), p.Version())
	}
	if err := p.Init(nil); err != nil {
		t.Errorf("Init 应无副作用，实际 %v", err)
	}
	if err := p.Close(); err != nil {
		t.Errorf("Close 应幂等无错，实际 %v", err)
	}
	if err := p.Close(); err != nil {
		t.Errorf("二次 Close 仍应无错，实际 %v", err)
	}
}

// TestRemotePluginRegistersIntoManager 证明远程插件能作为普通 Plugin 走完整生命周期，
// 且注册后经 Manager.FireHook 触发（控制面的接线点只认 Manager）。
func TestRemotePluginRegistersIntoManager(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, `{"decision":"deny","reason":"no"}`)
	}))
	defer srv.Close()

	mgr := NewManager()
	defer func() { _ = mgr.Close() }()
	rp := testRemote(t, srv.URL, []Hook{HookConfigPreSet})
	if err := mgr.Register(rp, nil); err != nil {
		t.Fatalf("注册失败：%v", err)
	}
	if err := mgr.RegisterHook(HookConfigPreSet, rp.Handler(HookConfigPreSet)); err != nil {
		t.Fatalf("注册钩子失败：%v", err)
	}
	err := mgr.FireHook(context.Background(), HookConfigPreSet, Event{Name: "x"})
	if err == nil {
		t.Fatal("插件 deny 时 FireHook 必须返回 error")
	}
	if !errors.Is(err, ErrDenied) {
		t.Errorf("Manager 的 %%w 包装丢掉了 ErrDenied 判定（控制面会把它记成运维故障）：%v", err)
	}
	if hits.Load() != 1 {
		t.Errorf("插件端点应被调用 1 次，实际 %d", hits.Load())
	}
}
