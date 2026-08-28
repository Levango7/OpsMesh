package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OpsMeshClient is an HTTP client for calling OpsMesh APIs.
type OpsMeshClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewOpsMeshClient creates a new OpsMesh API client.
func NewOpsMeshClient(baseURL string) *OpsMeshClient {
	return &OpsMeshClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// SystemStatus represents the overall system status.
type SystemStatus struct {
	DevicesOnline int    `json:"devices_online"`
	ActiveAlerts  int    `json:"active_alerts"`
	Healthy       bool   `json:"healthy"`
}

// Device represents a managed device.
type Device struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	IP     string `json:"ip"`
}

// AlertInfo represents an active alert.
type AlertInfo struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Status   string `json:"status"`
}

// DeviceMetrics represents metrics for a device.
type DeviceMetrics struct {
	DeviceID string            `json:"device_id"`
	CPU      float64           `json:"cpu"`
	Memory   float64           `json:"memory"`
	Disk     float64           `json:"disk"`
	Custom   map[string]float64 `json:"custom,omitempty"`
}

// TaskResult represents the result of a task execution.
type TaskResult struct {
	TaskID  string `json:"task_id"`
	Success bool   `json:"success"`
	Output  string `json:"output"`
}

// DeployResult represents the result of a deployment.
type DeployResult struct {
	DeployID string `json:"deploy_id"`
	Success  bool   `json:"success"`
	Message  string `json:"message"`
}

// GetStatus retrieves system status.
func (c *OpsMeshClient) GetStatus() (*SystemStatus, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/api/v1/status")
	if err != nil {
		return nil, fmt.Errorf("failed to get status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status API returned %d", resp.StatusCode)
	}

	var status SystemStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to decode status: %w", err)
	}
	return &status, nil
}

// ListDevices retrieves all managed devices.
func (c *OpsMeshClient) ListDevices() ([]Device, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/api/v1/devices")
	if err != nil {
		return nil, fmt.Errorf("failed to list devices: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("devices API returned %d", resp.StatusCode)
	}

	var devices []Device
	if err := json.NewDecoder(resp.Body).Decode(&devices); err != nil {
		return nil, fmt.Errorf("failed to decode devices: %w", err)
	}
	return devices, nil
}

// ListAlerts retrieves active alerts.
func (c *OpsMeshClient) ListAlerts() ([]AlertInfo, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/api/v1/alerts?status=firing")
	if err != nil {
		return nil, fmt.Errorf("failed to list alerts: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("alerts API returned %d", resp.StatusCode)
	}

	var alerts []AlertInfo
	if err := json.NewDecoder(resp.Body).Decode(&alerts); err != nil {
		return nil, fmt.Errorf("failed to decode alerts: %w", err)
	}
	return alerts, nil
}

// AckAlert acknowledges an alert.
func (c *OpsMeshClient) AckAlert(alertID string) error {
	body, _ := json.Marshal(map[string]string{"id": alertID})
	resp, err := c.httpClient.Post(c.baseURL+"/api/v1/alerts/ack", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to ack alert: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ack API returned %d", resp.StatusCode)
	}
	return nil
}

// ExecuteTask executes a command on a device.
func (c *OpsMeshClient) ExecuteTask(deviceID, command string) (*TaskResult, error) {
	body, _ := json.Marshal(map[string]string{"device_id": deviceID, "command": command})
	resp, err := c.httpClient.Post(c.baseURL+"/api/v1/tasks", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to execute task: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("task API returned %d", resp.StatusCode)
	}

	var result TaskResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode task result: %w", err)
	}
	return &result, nil
}

// TriggerDeploy triggers a deployment.
func (c *OpsMeshClient) TriggerDeploy(appID, version string) (*DeployResult, error) {
	body, _ := json.Marshal(map[string]string{"app_id": appID, "version": version})
	resp, err := c.httpClient.Post(c.baseURL+"/api/v1/deploy", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to trigger deploy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("deploy API returned %d", resp.StatusCode)
	}

	var result DeployResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode deploy result: %w", err)
	}
	return &result, nil
}

// GetDeviceMetrics retrieves metrics for a device.
func (c *OpsMeshClient) GetDeviceMetrics(deviceID string) (*DeviceMetrics, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/api/v1/devices/" + deviceID + "/metrics")
	if err != nil {
		return nil, fmt.Errorf("failed to get metrics: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metrics API returned %d", resp.StatusCode)
	}

	var metrics DeviceMetrics
	if err := json.NewDecoder(resp.Body).Decode(&metrics); err != nil {
		return nil, fmt.Errorf("failed to decode metrics: %w", err)
	}
	return &metrics, nil
}

// SetHTTPClient allows replacing the underlying HTTP client (for testing).
func (c *OpsMeshClient) SetHTTPClient(client *http.Client) {
	c.httpClient = client
}

// DrainBody reads and discards the response body.
func DrainBody(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}
