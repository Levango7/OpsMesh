// otelx_endpoint_test.go — OTLP 端点规范化（#65）。
//
// 这里测的是"出厂 12 个微服务实际下发的字符串"能不能变成 gRPC 拨得动的目标，
// 而不是我想象的写法：带 scheme 的 `http://otel-collector:4317` 正是
// docker-compose.prod.yml 里 OTEL_EXPORTER_OTLP_ENDPOINT 的字面值。
package otelx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeEndpoint(t *testing.T) {
	cases := []struct {
		in         string
		wantHost   string
		wantPlain  bool // true = 明文（WithInsecure）
		wantErrSub string
	}{
		// 出厂微服务 env 的形态：scheme 必须被剥掉，且明文。
		{"http://otel-collector:4317", "otel-collector:4317", true, ""},
		{"http://otel-collector:4317/", "otel-collector:4317", true, ""},
		// OTLP/HTTP 端点常带路径，gRPC 侧只认 host:port。
		{"http://otel:4318/v1/traces", "otel:4318", true, ""},
		// 控制面/agent 旗标形态：无 scheme，沿用"443 才是 TLS"的历史口径。
		{"otel-collector:4317", "otel-collector:4317", true, ""},
		{"collector.example.com:443", "collector.example.com:443", false, ""},
		// 显式 https 一律走 TLS，哪怕端口不是 443。
		{"https://collector.example.com:4317", "collector.example.com:4317", false, ""},
		// 只给 host：补 OTel 规范默认端口，避免 gRPC 自己猜出 ":443" 拼成双端口。
		{"http://otel", "otel:4317", true, ""},
		{" HTTP://Otel:4317 ", "Otel:4317", true, ""}, // scheme 归一化，host 原样保留（不做大小写改写）
		// 非法输入必须报错并让调用方 fail-fast，而不是"配上就跑、每 5s 失败一次"。
		{"", "", false, "endpoint 为空"},
		{"ftp://otel:4317", "", false, "scheme"},
		{"http://", "", false, "为空"},
	}

	for _, c := range cases {
		got, plain, err := normalizeEndpoint(c.in)
		if c.wantErrSub != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErrSub) {
				t.Errorf("normalizeEndpoint(%q) err=%v，期望包含 %q", c.in, err, c.wantErrSub)
			}
			continue
		}
		if err != nil {
			t.Errorf("normalizeEndpoint(%q) 意外报错: %v", c.in, err)
			continue
		}
		if got != c.wantHost || plain != c.wantPlain {
			t.Errorf("normalizeEndpoint(%q) = (%q, plain=%v)，期望 (%q, plain=%v)",
				c.in, got, plain, c.wantHost, c.wantPlain)
		}
	}
}

// TestNormalizeEndpointRejectsUnparseableInit 钉住 Init 侧的行为：坏端点必须返回错误，
// 而不是"初始化成功、span 在后台持续导出失败"。后者就是 #65 的形态。
func TestNormalizeEndpointRejectsUnparseableInit(t *testing.T) {
	if _, err := Init(Config{ServiceName: "otelx-test", Endpoint: "not-a-host:"}); err == nil {
		// "not-a-host:" 去掉 scheme 后 host 为空 → 应报错
		t.Errorf("Init 接受了坏端点（应 fail-fast 而不是静默退化）")
	}
}

// TestFactoryComposeEndpointIsDialable 把"交付配置"与"解析能力"钉在一起：
// 直接读 docker-compose.prod.yml 里的 OTEL_EXPORTER_OTLP_ENDPOINT 字面量逐条规范化。
// 改了 compose 而解析跟不上，这条会在本地与 CI 立刻红，而不是等到真机 span 全丢。
func TestFactoryComposeEndpointIsDialable(t *testing.T) {
	path := filepath.Join("..", "..", "deploy", "docker", "docker-compose.prod.yml")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败（交付门禁没有清单可读时必须是红，不是跳过）: %v", path, err)
	}
	lines := []string{}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.Contains(l, "OTEL_EXPORTER_OTLP_ENDPOINT:") {
			lines = append(lines, strings.TrimSpace(l))
		}
	}
	if len(lines) < 10 {
		t.Fatalf("只找到 %d 行 OTEL_EXPORTER_OTLP_ENDPOINT（交付形态已变，本测试失去覆盖面）", len(lines))
	}
	for _, l := range lines {
		val := l[strings.Index(l, ":")+1:]
		val = strings.Trim(strings.TrimSpace(val), `"`)
		host, _, err := normalizeEndpoint(val)
		if err != nil {
			t.Errorf("compose 端点 %q 无法规范化: %v", val, err)
			continue
		}
		if strings.Contains(host, "://") {
			t.Errorf("规范化后仍带 scheme：%q", host)
		}
	}
}
