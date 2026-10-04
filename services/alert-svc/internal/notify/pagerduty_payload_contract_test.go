// pagerduty_payload_contract_test.go — 外发载荷的**线路形状**（Event API v2 契约）。
//
// 为什么按"原始 JSON 里有没有这个键"来断言，而不是反序列化成 struct 再比字段：
// acknowledge/resolve 不填 severity，序列化后就是 `"severity":""`——按 struct 比是
// `Severity == ""`（看起来"没赋值"，无害），按线路看却是**给枚举字段发了一个非法值**。
// 真实 PagerDuty 端点会判 400，而我们这边只留一条 WARN，客户侧的表现是"值班永远叫不醒"。
// 这类差异只有在字节层面断言才拦得住。
package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func capturePayload(t *testing.T, send func(c *PagerDutyClient) error) map[string]any {
	t.Helper()
	var raw map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &raw); err != nil {
			t.Errorf("外发 body 不是合法 JSON: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	if err := send(NewPagerDutyClient("rk-1", srv.URL, 5*time.Second, false)); err != nil {
		t.Fatalf("发送失败: %v", err)
	}
	return raw
}

func TestPagerDutyPayloadWireShape(t *testing.T) {
	cases := []struct {
		name         string
		send         func(c *PagerDutyClient) error
		wantAction   string
		wantPayload  bool
		wantSeverity string // 期望 severity 的确切值；"" = 这个键必须整个不出现
	}{
		{
			name: "trigger", wantAction: "trigger", wantPayload: true, wantSeverity: "critical",
			send: func(c *PagerDutyClient) error {
				return c.TriggerEvent("dev-1", "CPU 高", "critical", "dedup-1", map[string]interface{}{"cpu": 95.5})
			},
		},
		{
			name: "acknowledge", wantAction: "acknowledge", wantPayload: true, wantSeverity: "",
			send: func(c *PagerDutyClient) error {
				return c.AcknowledgeEvent("dev-1", "alert acknowledged", "dedup-1", nil)
			},
		},
		{
			name: "resolve", wantAction: "resolve", wantPayload: true, wantSeverity: "",
			send: func(c *PagerDutyClient) error {
				return c.ResolveEvent("dev-1", "alert resolved", "dedup-1", nil)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := capturePayload(t, tc.send)

			// 三个必填键：缺任何一个，真实端点直接 400。
			if body["routing_key"] != "rk-1" {
				t.Errorf("routing_key 缺失或不等于集成密钥: %#v", body["routing_key"])
			}
			if body["event_action"] != tc.wantAction {
				t.Errorf("event_action = %#v，期望 %q", body["event_action"], tc.wantAction)
			}
			if body["dedup_key"] != "dedup-1" {
				t.Errorf("dedup_key = %#v（三条动作必须共用同一 dedup_key，否则 ack/resolve 关联不到原 incident）", body["dedup_key"])
			}

			payload, ok := body["payload"].(map[string]any)
			if !ok != !tc.wantPayload {
				t.Fatalf("payload 存在性 = %v，期望 %v", ok, tc.wantPayload)
			}
			if !ok {
				return
			}
			_, hasSeverity := payload["severity"]
			if tc.wantSeverity == "" {
				if hasSeverity {
					t.Errorf("ack/resolve 的 payload 里出现了 severity 键（值为 %v）：空枚举是非法值，必须整个不发", payload["severity"])
				}
				return
			}
			if payload["severity"] != tc.wantSeverity {
				t.Errorf("trigger 的 severity = %v，期望 %q", payload["severity"], tc.wantSeverity)
			}
			if payload["source"] != "dev-1" || payload["summary"] == "" {
				t.Errorf("trigger 的 payload 缺少 source/summary: %#v", payload)
			}
		})
	}
}
