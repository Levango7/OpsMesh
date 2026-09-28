package prometheus

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client reads metrics from Prometheus.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new Prometheus client.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ReadMetric reads a metric value for a deployment from Prometheus.
//
// Prometheus 的 /api/v1/query 返回 JSON（{"status":"success","data":{"resultType":
// "vector","result":[{"metric":{...},"value":[<unix 秒>,"<数值字符串>"]}]}}），
// 此前实现按多行文本（exposition 格式）逐行取第二列解析，导致真实 Prometheus
// 下每次读取都报 "no metric found"——自动伸缩的指标读取链路恒失效（评估器随后
// 只能产出 no_action/failed to read metric）。现按 JSON 契约解析：
//   - status != "success" → 携带 errorType/error 报错（PromQL 语法错等可诊断）；
//   - result 为空（无匹配序列）→ no metric found（保留原错误语义）；
//   - value[1] 非数字 → 报错。
func (c *Client) ReadMetric(deployment, namespace, metric string) (float64, error) {
	// PromQL 序列选择器用 %q 生成 Go 字符串字面量（与 PromQL 的字符串引号规则
	// 一致，内嵌引号/反斜杠正确转义）；整段经 url.Values 编码为查询参数。
	selector := fmt.Sprintf(`%s{deployment=%q,namespace=%q}`, metric, deployment, namespace)
	params := url.Values{}
	params.Set("query", selector)
	reqURL := c.baseURL + "/api/v1/query?" + params.Encode()

	resp, err := c.httpClient.Get(reqURL)
	if err != nil {
		return 0, fmt.Errorf("failed to query Prometheus: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("prometheus returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response: %w", err)
	}

	return parseQueryResult(body, deployment, namespace)
}

// promQueryResponse 是 /api/v1/query 成功响应的最小结构。
type promQueryResponse struct {
	Status    string `json:"status"`
	ErrorType string `json:"errorType"`
	Error     string `json:"error"`
	Data      struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			// value 为 [<unix 时间戳>, "<数值字符串>"]，两元素均为 JSON 值，
			// 时间戳在部分版本可能带小数，故只取下标 1 并放宽为 RawMessage。
			Value []json.RawMessage `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func parseQueryResult(body []byte, deployment, namespace string) (float64, error) {
	var qr promQueryResponse
	if err := json.Unmarshal(body, &qr); err != nil {
		return 0, fmt.Errorf("failed to parse Prometheus response: %w", err)
	}
	if qr.Status != "success" {
		msg := strings.TrimSpace(qr.ErrorType + " " + qr.Error)
		return 0, fmt.Errorf("prometheus query failed: %s", msg)
	}
	if len(qr.Data.Result) == 0 {
		return 0, fmt.Errorf("no metric found for deployment=%s namespace=%s", deployment, namespace)
	}

	value := qr.Data.Result[0].Value
	if len(value) < 2 {
		return 0, fmt.Errorf("prometheus result has no value for deployment=%s namespace=%s", deployment, namespace)
	}
	var raw string
	if err := json.Unmarshal(value[1], &raw); err != nil {
		return 0, fmt.Errorf("prometheus value is not a string: %w", err)
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("prometheus value %q is not a number: %w", raw, err)
	}
	return v, nil
}
