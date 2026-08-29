package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client communicates with the OpsMesh REST API.
type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string
}

// NewClient creates a new OpsMesh API client.
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// doRequest performs an HTTP request and decodes the response.
func (c *Client) doRequest(method, path string, body, result interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	url := c.baseURL + path
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}
	}

	return nil
}

// Device represents an OpsMesh device in the API.
type Device struct {
	ID        string            `json:"id,omitempty"`
	Name      string            `json:"name"`
	IP        string            `json:"ip,omitempty"`
	MAC       string            `json:"mac,omitempty"`
	OS        string            `json:"os,omitempty"`
	Arch      string            `json:"arch,omitempty"`
	Status    string            `json:"status,omitempty"`
	AgentID   string            `json:"agentID,omitempty"`
	Tags      []string          `json:"tags,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	Group     string            `json:"group,omitempty"`
	TenantID  string            `json:"tenantID,omitempty"`
	CreatedAt string            `json:"createdAt,omitempty"`
	UpdatedAt string            `json:"updatedAt,omitempty"`
}

// Task represents an OpsMesh task in the API.
type Task struct {
	TaskID    string   `json:"taskID"`
	AgentID   string   `json:"agentID"`
	TenantID  string   `json:"tenantID,omitempty"`
	Type      string   `json:"type"`
	Command   string   `json:"command,omitempty"`
	Content   string   `json:"content,omitempty"`
	Path      string   `json:"path,omitempty"`
	Status    string   `json:"status,omitempty"`
	Timeout   int      `json:"timeout,omitempty"`
	Schedule  string   `json:"schedule,omitempty"`
	ParentID  string   `json:"parentID,omitempty"`
	DependsOn []string `json:"dependsOn,omitempty"`
}

// AlertRule represents an OpsMesh alert rule in the API.
type AlertRule struct {
	ID        string   `json:"id,omitempty"`
	Name      string   `json:"name"`
	Metric    string   `json:"metric"`
	Op        string   `json:"op"`
	Threshold float64  `json:"threshold"`
	Duration  int      `json:"duration,omitempty"`
	Severity  string   `json:"severity,omitempty"`
	Channels  []string `json:"channels,omitempty"`
	Enabled   bool     `json:"enabled,omitempty"`
	TenantID  string   `json:"tenantID,omitempty"`
}

// Alert represents an OpsMesh alert in the API.
type Alert struct {
	ID        string `json:"id"`
	RuleID    string `json:"ruleID,omitempty"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	Status    string `json:"status"`
	FiredAt   string `json:"firedAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// Deployment represents an OpsMesh deployment in the API.
type Deployment struct {
	ID           string   `json:"id,omitempty"`
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	RepoURL      string   `json:"repoURL,omitempty"`
	Content      string   `json:"content,omitempty"`
	Path         string   `json:"path,omitempty"`
	TargetIDs    []string `json:"targetIDs,omitempty"`
	Status       string   `json:"status,omitempty"`
	Strategy     string   `json:"strategy,omitempty"`
	CanaryWeight int      `json:"canaryWeight,omitempty"`
	AutoRollback bool     `json:"autoRollback,omitempty"`
	CreatedBy    string   `json:"createdBy,omitempty"`
	TenantID     string   `json:"tenantID,omitempty"`
	ErrorMessage string   `json:"errorMessage,omitempty"`
}

// Device methods

func (c *Client) CreateDevice(d *Device) (*Device, error) {
	var result Device
	err := c.doRequest(http.MethodPost, "/api/v1/devices", d, &result)
	return &result, err
}

func (c *Client) GetDevice(id string) (*Device, error) {
	var result Device
	err := c.doRequest(http.MethodGet, "/api/v1/devices/"+id, nil, &result)
	return &result, err
}

func (c *Client) UpdateDevice(d *Device) (*Device, error) {
	var result Device
	err := c.doRequest(http.MethodPut, "/api/v1/devices/"+d.ID, d, &result)
	return &result, err
}

func (c *Client) DeleteDevice(id string) error {
	return c.doRequest(http.MethodDelete, "/api/v1/devices/"+id, nil, nil)
}

// Task methods

func (c *Client) CreateTask(t *Task) (*Task, error) {
	var result Task
	err := c.doRequest(http.MethodPost, "/api/v1/tasks", t, &result)
	return &result, err
}

func (c *Client) GetTask(id string) (*Task, error) {
	var result Task
	err := c.doRequest(http.MethodGet, "/api/v1/tasks/"+id, nil, &result)
	return &result, err
}

func (c *Client) UpdateTask(t *Task) (*Task, error) {
	var result Task
	err := c.doRequest(http.MethodPut, "/api/v1/tasks/"+t.TaskID, t, &result)
	return &result, err
}

func (c *Client) DeleteTask(id string) error {
	return c.doRequest(http.MethodDelete, "/api/v1/tasks/"+id, nil, nil)
}

// AlertRule methods

func (c *Client) CreateAlertRule(r *AlertRule) (*AlertRule, error) {
	var result AlertRule
	err := c.doRequest(http.MethodPost, "/api/v1/alert-rules", r, &result)
	return &result, err
}

func (c *Client) GetAlertRule(id string) (*AlertRule, error) {
	var result AlertRule
	err := c.doRequest(http.MethodGet, "/api/v1/alert-rules/"+id, nil, &result)
	return &result, err
}

func (c *Client) UpdateAlertRule(r *AlertRule) (*AlertRule, error) {
	var result AlertRule
	err := c.doRequest(http.MethodPut, "/api/v1/alert-rules/"+r.ID, r, &result)
	return &result, err
}

func (c *Client) DeleteAlertRule(id string) error {
	return c.doRequest(http.MethodDelete, "/api/v1/alert-rules/"+id, nil, nil)
}

// Alert methods

func (c *Client) ListAlerts() ([]Alert, error) {
	var result []Alert
	err := c.doRequest(http.MethodGet, "/api/v1/alerts", nil, &result)
	return result, err
}

// Deployment methods

func (c *Client) CreateDeployment(d *Deployment) (*Deployment, error) {
	var result Deployment
	err := c.doRequest(http.MethodPost, "/api/v1/deployments", d, &result)
	return &result, err
}

func (c *Client) GetDeployment(id string) (*Deployment, error) {
	var result Deployment
	err := c.doRequest(http.MethodGet, "/api/v1/deployments/"+id, nil, &result)
	return &result, err
}

func (c *Client) UpdateDeployment(d *Deployment) (*Deployment, error) {
	var result Deployment
	err := c.doRequest(http.MethodPut, "/api/v1/deployments/"+d.ID, d, &result)
	return &result, err
}

func (c *Client) DeleteDeployment(id string) error {
	return c.doRequest(http.MethodDelete, "/api/v1/deployments/"+id, nil, nil)
}
