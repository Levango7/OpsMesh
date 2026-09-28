package prometheus

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newQueryServer 启动一个返回固定响应体的 /api/v1/query 桩服务。
func newQueryServer(t *testing.T, statusCode int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestReadMetricVectorSuccess 覆盖真实 Prometheus /api/v1/query 的 JSON 契约：
// 选择器按 {deployment,namespace} 构造并经 URL 编码，向量首条取 value[1]。
func TestReadMetricVectorSuccess(t *testing.T) {
	var gotQuery string
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("query")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"deployment":"web","namespace":"prod"},"value":[1696000000.123,"42.5"]}]}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL + "/")
	v, err := c.ReadMetric("web", "prod", "cpu_usage")
	if err != nil {
		t.Fatalf("ReadMetric 失败: %v", err)
	}
	if v != 42.5 {
		t.Fatalf("value=%v, want 42.5", v)
	}
	if gotPath != "/api/v1/query" {
		t.Fatalf("path=%q, want /api/v1/query", gotPath)
	}
	wantQuery := `cpu_usage{deployment="web",namespace="prod"}`
	if gotQuery != wantQuery {
		t.Fatalf("query=%q, want %q", gotQuery, wantQuery)
	}
}

// TestReadMetricNoData result 为空时保留既有错误语义（评估器据此产出
// failed to read metric 的可诊断原因）。
func TestReadMetricNoData(t *testing.T) {
	srv := newQueryServer(t, http.StatusOK, `{"status":"success","data":{"resultType":"vector","result":[]}}`)
	c := NewClient(srv.URL)
	_, err := c.ReadMetric("web", "prod", "cpu_usage")
	if err == nil || !strings.Contains(err.Error(), "no metric found for deployment=web namespace=prod") {
		t.Fatalf("err=%v, want no metric found", err)
	}
}

// TestReadMetricQueryError status=error（如 PromQL 语法错）应带 errorType/error 报错。
func TestReadMetricQueryError(t *testing.T) {
	srv := newQueryServer(t, http.StatusOK,
		`{"status":"error","errorType":"bad_data","error":"invalid parameter \"query\": parse error"}`)
	c := NewClient(srv.URL)
	_, err := c.ReadMetric("web", "prod", "cpu_usage")
	if err == nil || !strings.Contains(err.Error(), "bad_data") || !strings.Contains(err.Error(), "parse error") {
		t.Fatalf("err=%v, want bad_data/parse error", err)
	}
}

// TestReadMetricInvalidJSON 非 JSON 响应（代理错误页等）必须报解析失败，
// 而不是回落成"无数据"。
func TestReadMetricInvalidJSON(t *testing.T) {
	srv := newQueryServer(t, http.StatusOK, "<html>gateway error</html>")
	c := NewClient(srv.URL)
	_, err := c.ReadMetric("web", "prod", "cpu_usage")
	if err == nil || !strings.Contains(err.Error(), "failed to parse Prometheus response") {
		t.Fatalf("err=%v, want parse failure", err)
	}
}

// TestReadMetricNonNumericValue value[1] 非数字（NaN 以外的异常载荷）报错。
func TestReadMetricNonNumericValue(t *testing.T) {
	srv := newQueryServer(t, http.StatusOK,
		`{"status":"success","data":{"resultType":"vector","result":[{"value":[1696000000,"abc"]}]}}`)
	c := NewClient(srv.URL)
	_, err := c.ReadMetric("web", "prod", "cpu_usage")
	if err == nil || !strings.Contains(err.Error(), `"abc" is not a number`) {
		t.Fatalf("err=%v, want not a number", err)
	}
}

// TestReadMetricHTTPStatus 非 200 状态码保持原错误语义。
func TestReadMetricHTTPStatus(t *testing.T) {
	srv := newQueryServer(t, http.StatusInternalServerError, `{"status":"error"}`)
	c := NewClient(srv.URL)
	_, err := c.ReadMetric("web", "prod", "cpu_usage")
	if err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("err=%v, want status 500", err)
	}
}

// TestReadMetricUnreachable 后端不可达时报查询失败。
func TestReadMetricUnreachable(t *testing.T) {
	srv := newQueryServer(t, http.StatusOK, `{}`)
	url := srv.URL
	srv.Close()
	c := NewClient(url)
	_, err := c.ReadMetric("web", "prod", "cpu_usage")
	if err == nil || !strings.Contains(err.Error(), "failed to query Prometheus") {
		t.Fatalf("err=%v, want query failure", err)
	}
}
