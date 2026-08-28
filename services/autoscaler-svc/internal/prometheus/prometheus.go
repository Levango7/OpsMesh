package prometheus

import (
	"fmt"
	"io"
	"net/http"
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
func (c *Client) ReadMetric(deployment, namespace, metric string) (float64, error) {
	query := fmt.Sprintf(`%s{deployment="%s",namespace="%s"}`, metric, deployment, namespace)
	url := fmt.Sprintf("%s/api/v1/query?query=%s", c.baseURL, query)

	resp, err := c.httpClient.Get(url)
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

	result, err := parseQueryResult(string(body), deployment, namespace)
	if err != nil {
		return 0, err
	}
	return result, nil
}

func parseQueryResult(body, deployment, namespace string) (float64, error) {
	var result float64
	found := false

	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			val, err := strconv.ParseFloat(parts[1], 64)
			if err == nil {
				result = val
				found = true
			}
		}
	}

	if !found {
		return 0, fmt.Errorf("no metric found for deployment=%s namespace=%s", deployment, namespace)
	}
	return result, nil
}
