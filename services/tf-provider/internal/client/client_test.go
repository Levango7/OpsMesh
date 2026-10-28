package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestServer 起一个记录请求并回固定响应的后端，返回 client 与请求记录。
func newTestServer(t *testing.T, status int, respBody string, handler func(w http.ResponseWriter, r *http.Request)) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if handler != nil {
			handler(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if respBody != "" {
			_, _ = io.WriteString(w, respBody)
		}
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, "tok-123"), srv
}

func TestNewClient(t *testing.T) {
	c := NewClient("http://api.example.com", "secret")
	if c.baseURL != "http://api.example.com" || c.token != "secret" {
		t.Errorf("构造结果不对：%+v", c)
	}
	if c.httpClient == nil || c.httpClient.Timeout != 30*time.Second {
		t.Errorf("HTTP 客户端应有 30s 超时：%+v", c.httpClient)
	}
}

// TestDoRequest_HeadersAndPath 钉住 REST 约定：方法/路径拼接、JSON 头、Bearer 鉴权。
func TestDoRequest_HeadersAndPath(t *testing.T) {
	var (
		gotMethod, gotPath, gotAuth, gotCT, gotAccept, gotBody string
	)
	c, _ := newTestServer(t, http.StatusOK, `{"id":"d1"}`, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		gotAccept = r.Header.Get("Accept")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"d1","name":"n"}`)
	})

	dev, err := c.CreateDevice(&Device{Name: "n"})
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/devices" {
		t.Errorf("请求 = %s %s，期望 POST /api/v1/devices", gotMethod, gotPath)
	}
	if gotAuth != "Bearer tok-123" {
		t.Errorf("Authorization = %q，期望 Bearer tok-123", gotAuth)
	}
	if gotCT != "application/json" || gotAccept != "application/json" {
		t.Errorf("Content-Type/Accept = %q/%q", gotCT, gotAccept)
	}
	if !strings.Contains(gotBody, `"name":"n"`) {
		t.Errorf("请求体未序列化正确：%q", gotBody)
	}
	if dev.ID != "d1" || dev.Name != "n" {
		t.Errorf("响应未解码进结果：%+v", dev)
	}
}

func TestDoRequest_NoTokenMeansNoAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	if err := c.DeleteDevice("d1"); err != nil {
		t.Fatalf("DeleteDevice: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("空 token 不应带 Authorization 头，得到 %q", gotAuth)
	}
}

func TestDoRequest_ErrorStatus(t *testing.T) {
	c, _ := newTestServer(t, http.StatusNotFound, `{"error":"device not found"}`, nil)

	dev, err := c.GetDevice("missing")
	if err == nil {
		t.Fatal("4xx 应返回错误")
	}
	msg := err.Error()
	if !strings.Contains(msg, "404") || !strings.Contains(msg, "device not found") {
		t.Errorf("错误信息应含状态码与响应体，得到 %q", msg)
	}
	if dev == nil || dev.ID != "" {
		t.Errorf("出错时应返回零值结果而不是 nil：%+v", dev)
	}
}

func TestDoRequest_MarshalError(t *testing.T) {
	c := NewClient("http://127.0.0.1:1", "")
	err := c.doRequest(http.MethodPost, "/x", make(chan int), nil)
	if err == nil || !strings.Contains(err.Error(), "marshal") {
		t.Fatalf("不可序列化的请求体应报 marshal 错误，得到 %v", err)
	}
}

func TestDoRequest_TransportError(t *testing.T) {
	c := NewClient("http://127.0.0.1:1", "")
	if err := c.doRequest(http.MethodGet, "/x", nil, nil); err == nil {
		t.Fatal("连接失败应返回错误")
	}
}

func TestDoRequest_InvalidJSONResponse(t *testing.T) {
	c, _ := newTestServer(t, http.StatusOK, `not-json`, nil)
	if _, err := c.GetDevice("d1"); err == nil || !strings.Contains(err.Error(), "unmarshal") {
		t.Fatalf("非法响应体应报 unmarshal 错误，得到 %v", err)
	}
}

func TestDoRequest_EmptyBodyWithResult(t *testing.T) {
	c, _ := newTestServer(t, http.StatusOK, "", nil)
	dev, err := c.GetDevice("d1")
	if err != nil {
		t.Fatalf("空响应体不应报错（204 语义）：%v", err)
	}
	if dev == nil || dev.ID != "" {
		t.Errorf("空响应应返回零值结果：%+v", dev)
	}
}

// TestEndpoints 表驱动钉住各资源的 方法+路径，改动会直接破坏 Terraform provider 契约。
func TestEndpoints(t *testing.T) {
	type call struct {
		method, path string
		invoke       func(c *Client) error
	}
	cases := []call{
		{http.MethodPost, "/api/v1/devices", func(c *Client) error { _, err := c.CreateDevice(&Device{Name: "n"}); return err }},
		{http.MethodGet, "/api/v1/devices/d1", func(c *Client) error { _, err := c.GetDevice("d1"); return err }},
		{http.MethodPut, "/api/v1/devices/d1", func(c *Client) error { _, err := c.UpdateDevice(&Device{ID: "d1"}); return err }},
		{http.MethodDelete, "/api/v1/devices/d1", func(c *Client) error { return c.DeleteDevice("d1") }},
		{http.MethodPost, "/api/v1/tasks", func(c *Client) error { _, err := c.CreateTask(&Task{TaskID: "t1"}); return err }},
		{http.MethodGet, "/api/v1/tasks/t1", func(c *Client) error { _, err := c.GetTask("t1"); return err }},
		{http.MethodPut, "/api/v1/tasks/t1", func(c *Client) error { _, err := c.UpdateTask(&Task{TaskID: "t1"}); return err }},
		{http.MethodDelete, "/api/v1/tasks/t1", func(c *Client) error { return c.DeleteTask("t1") }},
		{http.MethodPost, "/api/v1/alert-rules", func(c *Client) error { _, err := c.CreateAlertRule(&AlertRule{Name: "r"}); return err }},
		{http.MethodGet, "/api/v1/alert-rules/r1", func(c *Client) error { _, err := c.GetAlertRule("r1"); return err }},
		{http.MethodPut, "/api/v1/alert-rules/r1", func(c *Client) error { _, err := c.UpdateAlertRule(&AlertRule{ID: "r1"}); return err }},
		{http.MethodDelete, "/api/v1/alert-rules/r1", func(c *Client) error { return c.DeleteAlertRule("r1") }},
		{http.MethodGet, "/api/v1/alerts", func(c *Client) error { _, err := c.ListAlerts(); return err }},
		{http.MethodPost, "/api/v1/deployments", func(c *Client) error { _, err := c.CreateDeployment(&Deployment{Name: "dp"}); return err }},
		{http.MethodGet, "/api/v1/deployments/dp1", func(c *Client) error { _, err := c.GetDeployment("dp1"); return err }},
		{http.MethodPut, "/api/v1/deployments/dp1", func(c *Client) error { _, err := c.UpdateDeployment(&Deployment{ID: "dp1"}); return err }},
		{http.MethodDelete, "/api/v1/deployments/dp1", func(c *Client) error { return c.DeleteDeployment("dp1") }},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var gotMethod, gotPath string
			body := `{}`
			if tc.method == http.MethodGet && tc.path == "/api/v1/alerts" {
				body = `[]` // ListAlerts 解码到切片，回对象会 unmarshal 失败
			}
			c, _ := newTestServer(t, http.StatusOK, body, func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.Path
				_, _ = io.WriteString(w, body)
			})
			if err := tc.invoke(c); err != nil {
				t.Fatalf("调用失败：%v", err)
			}
			if gotMethod != tc.method || gotPath != tc.path {
				t.Errorf("实际请求 = %s %s，期望 %s %s", gotMethod, gotPath, tc.method, tc.path)
			}
		})
	}
}

func TestListAlerts_DecodesArray(t *testing.T) {
	c, _ := newTestServer(t, http.StatusOK, `[{"id":"a1","severity":"high","message":"m","status":"firing"},{"id":"a2","severity":"low","message":"m2","status":"resolved"}]`, nil)
	alerts, err := c.ListAlerts()
	if err != nil {
		t.Fatalf("ListAlerts: %v", err)
	}
	if len(alerts) != 2 || alerts[0].ID != "a1" || alerts[1].Severity != "low" {
		t.Errorf("数组解码不对：%+v", alerts)
	}
}

// TestPayloadRoundTrip 确认请求体是完整结构体 JSON（provider 的字段名/omitempty 依赖它）。
func TestPayloadRoundTrip(t *testing.T) {
	var got map[string]interface{}
	c, _ := newTestServer(t, http.StatusOK, `{}`, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, `{}`)
	})
	_, err := c.CreateDeployment(&Deployment{
		Name: "dp", Type: "k8s", TargetIDs: []string{"a", "b"}, CanaryWeight: 20, AutoRollback: true,
	})
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	if got["name"] != "dp" || got["type"] != "k8s" || got["canaryWeight"] != float64(20) || got["autoRollback"] != true {
		t.Errorf("请求体字段不对：%+v", got)
	}
	if targets, ok := got["targetIDs"].([]interface{}); !ok || len(targets) != 2 {
		t.Errorf("targetIDs 未正确序列化：%+v", got["targetIDs"])
	}
}
