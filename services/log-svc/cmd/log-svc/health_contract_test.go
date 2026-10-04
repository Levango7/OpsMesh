package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	applog "github.com/Levango7/OpsMesh/pkg/log"
)

// TestResolveHealthAddrPortKeyTransition 门禁（TD-77）：健康/指标端口的环境变量
// 从 LOG_SVC_HEALTH_ADDR（地址形式）迁到 LOG_SVC_HTTP_PORT（纯端口），过渡期两者共存。
//
// 断言的是**优先级与语义**，不是单纯取值：
//   - 两个键都设 → 规范键赢（否则新写部署会被旧键反向覆盖）
//   - 只设旧键   → 仍生效，且必须原样返回（ADDR 形式带 ":8105"，不能被加工成 "::8105"）
//   - 只设新键   → 补成 ":PORT"
//   - 都不设     → 回落调用方给的默认值
//   - 新键非法   → 必须回落而不是拼出非法地址让 ListenAndServe 崩掉
func TestResolveHealthAddrPortKeyTransition(t *testing.T) {
	lgr := applog.Init("log-svc-test")
	const fallback = ":8080"

	cases := []struct {
		name    string
		env     map[string]string
		want    string
		comment string
	}{
		{
			name: "都不设时回落默认值",
			env:  map[string]string{},
			want: fallback,
		},
		{
			name: "只设规范键则补冒号",
			env:  map[string]string{"LOG_SVC_HTTP_PORT": "8105"},
			want: ":8105",
		},
		{
			name: "只设历史键则原样生效",
			env:  map[string]string{"LOG_SVC_HEALTH_ADDR": ":9105"},
			want: ":9105",
			// 防回归：曾经有人把 ADDR 形式的值也套一遍 ":"+v 的加工，
			// 结果变成 "::9105"——listen 能成功但探针永远连不上。
		},
		{
			name: "两键并存时规范键优先",
			env: map[string]string{
				"LOG_SVC_HTTP_PORT":   "8105",
				"LOG_SVC_HEALTH_ADDR": ":9999",
			},
			want: ":8105",
		},
		{
			name: "规范键非法时回落",
			env: map[string]string{
				"LOG_SVC_HTTP_PORT":   "not-a-port",
				"LOG_SVC_HEALTH_ADDR": ":9105",
			},
			want: ":9105",
			// 规范键非法不该让服务起不来：仍按历史键生效。
		},
		{
			name: "规范键非法且无历史键时回落默认值",
			env:  map[string]string{"LOG_SVC_HTTP_PORT": "abc"},
			want: fallback,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, k := range []string{"LOG_SVC_HTTP_PORT", "LOG_SVC_HEALTH_ADDR"} {
				t.Setenv(k, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if got := resolveHealthAddr(fallback, lgr); got != tc.want {
				t.Errorf("resolveHealthAddr() = %q, 期望 %q", got, tc.want)
			}
		})
	}
}

// TestHealthPathContract 门禁（TD-77）：规范路径 /health+/ready 与历史别名
// /healthz+/readyz 必须同时可用，且响应完全一致。
//
// 之所以要断言"一致"而非只断言"都 200"：别名是同一个 handler 的第二个注册，
// 不是第二份实现。若有人把别名改成独立实现，两者就会漂移，而外部探针
// （helm values 里 log-svc 的 probe.path 目前仍写 /healthz）会静默拿到旧行为。
func TestHealthPathContract(t *testing.T) {
	srv := newHealthServer(":0", nil)
	mux := srv.Handler

	// 规范路径。
	for _, path := range []string{"/health", "/ready"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("规范路径 %s 期望 200，实得 %d", path, rec.Code)
		}
	}

	// 别名与规范路径逐字节一致。
	for _, pair := range [][2]string{{"/health", "/healthz"}, {"/ready", "/readyz"}} {
		canonical, alias := pair[0], pair[1]
		var canonicalBody, aliasBody string
		for _, spec := range []struct {
			path string
			dst  *string
		}{{canonical, &canonicalBody}, {alias, &aliasBody}} {
			req := httptest.NewRequest(http.MethodGet, spec.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			b, _ := io.ReadAll(rec.Result().Body)
			*spec.dst = string(b)
			if rec.Code != http.StatusOK {
				t.Errorf("%s 期望 200，实得 %d", spec.path, rec.Code)
			}
		}
		if canonicalBody != aliasBody {
			t.Errorf("别名与规范路径响应不一致：%s=%q vs %s=%q",
				canonical, canonicalBody, alias, aliasBody)
		}
	}
}

// TestResolveHealthAddrDoesNotMutateEnv 保证解析过程只读环境变量。
// 这函数在启动路径上，若它顺手 os.Setenv 覆盖了调用方的配置，
// 后续读同一键的代码会拿到被加工过的值——这类污染极难排查。
func TestResolveHealthAddrDoesNotMutateEnv(t *testing.T) {
	lgr := applog.Init("log-svc-test")
	t.Setenv("LOG_SVC_HTTP_PORT", "8105")
	t.Setenv("LOG_SVC_HEALTH_ADDR", ":9105")

	_ = resolveHealthAddr(":8080", lgr)
	_ = resolveHealthAddr(":8080", lgr)

	if got := resolveHealthAddr(":8080", lgr); got != ":8105" {
		t.Errorf("重复调用后结果漂移：得到 %q，期望 :8105（说明环境被改了）", got)
	}
}
