// Package notify 提供告警通知能力。当前实现 Webhook 推送（M7）。
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"opsmesh/internal/proto"
)

// postJSON 将任意可 JSON 序列化的值 POST 到 webhook URL（Content-Type: application/json）。
// 10s 超时。仅在 HTTP 响应 status<300 时返回 nil。
func postJSON(url string, v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("notify: marshal: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("notify: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("notify: post %s: %w", url, err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("notify: post %s returned %d", url, resp.StatusCode)
	}
	return nil
}

// PostAlert 通过 HTTP POST 将告警推送到 webhook URL，JSON body 为 Alert 的完整序列化。
// 超时 10s，非阻塞调用方（goroutine 内使用）。
func PostAlert(url string, a *proto.Alert) error {
	if url == "" || a == nil {
		return nil
	}
	return postJSON(url, a)
}
