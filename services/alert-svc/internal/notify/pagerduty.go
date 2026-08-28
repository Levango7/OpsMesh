// Package notify provides alert notification dispatchers.
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Notifier is the interface for sending alert notifications to external systems.
type Notifier interface {
	TriggerEvent(source, summary, severity, dedupKey string, details map[string]interface{}) error
	AcknowledgeEvent(source, summary, dedupKey string, details map[string]interface{}) error
	ResolveEvent(source, summary, dedupKey string, details map[string]interface{}) error
	IsEnabled() bool
}

// Severity levels supported by PagerDuty Event API v2.
const (
	SeverityCritical = "critical"
	SeverityError    = "error"
	SeverityWarning  = "warning"
	SeverityInfo     = "info"
)

// EventAction represents the type of PagerDuty event.
type EventAction string

const (
	ActionTrigger     EventAction = "trigger"
	ActionAcknowledge EventAction = "acknowledge"
	ActionResolve     EventAction = "resolve"
)

// PagerDutyEvent is the payload for the PagerDuty Event API v2.
// Reference: https://developer.pagerduty.com/api-reference/368ae3d83d083-send-an-event-to-pager-duty
type PagerDutyEvent struct {
	RoutingKey  string           `json:"routing_key"`
	EventAction EventAction      `json:"event_action"`
	DedupKey    string           `json:"dedup_key,omitempty"`
	Payload     *EventPayload    `json:"payload"`
	Links       []EventLink      `json:"links,omitempty"`
}

// EventPayload is the payload field of a PagerDuty event.
type EventPayload struct {
	Summary   string            `json:"summary"`
	Source    string            `json:"source"`
	Severity  string            `json:"severity"`
	Component string            `json:"component,omitempty"`
	Group     string            `json:"group,omitempty"`
	Class     string            `json:"class,omitempty"`
	Details   map[string]interface{} `json:"custom_details,omitempty"`
}

// EventLink represents a link in a PagerDuty event.
type EventLink struct {
	Href string `json:"href"`
	Text string `json:"text"`
}

// PagerDutyClient sends events to PagerDuty via the Event API v2.
type PagerDutyClient struct {
	routingKey string
	apiURL     string
	httpClient *http.Client
	disabled   bool
}

// NewPagerDutyClient creates a new PagerDutyEvent API v2 client.
func NewPagerDutyClient(routingKey, apiURL string, timeout time.Duration, disabled bool) *PagerDutyClient {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &PagerDutyClient{
		routingKey: routingKey,
		apiURL:     apiURL,
		httpClient: &http.Client{Timeout: timeout},
		disabled:   disabled,
	}
}

// IsEnabled returns whether the client is enabled.
func (c *PagerDutyClient) IsEnabled() bool {
	return !c.disabled && c.routingKey != ""
}

// TriggerEvent sends a trigger event to PagerDuty, creating a new incident.
func (c *PagerDutyClient) TriggerEvent(source, summary, severity, dedupKey string, details map[string]interface{}) error {
	payload := &EventPayload{
		Summary:  summary,
		Source:   source,
		Severity: mapSeverity(severity),
		Details:  details,
	}
	event := &PagerDutyEvent{
		RoutingKey:  c.routingKey,
		EventAction: ActionTrigger,
		DedupKey:    dedupKey,
		Payload:     payload,
	}
	return c.send(event)
}

// AcknowledgeEvent sends an acknowledge event to PagerDuty.
func (c *PagerDutyClient) AcknowledgeEvent(source, summary, dedupKey string, details map[string]interface{}) error {
	payload := &EventPayload{
		Summary: summary,
		Source:  source,
		Details: details,
	}
	event := &PagerDutyEvent{
		RoutingKey:  c.routingKey,
		EventAction: ActionAcknowledge,
		DedupKey:    dedupKey,
		Payload:     payload,
	}
	return c.send(event)
}

// ResolveEvent sends a resolve event to PagerDuty.
func (c *PagerDutyClient) ResolveEvent(source, summary, dedupKey string, details map[string]interface{}) error {
	payload := &EventPayload{
		Summary: summary,
		Source:  source,
		Details: details,
	}
	event := &PagerDutyEvent{
		RoutingKey:  c.routingKey,
		EventAction: ActionResolve,
		DedupKey:    dedupKey,
		Payload:     payload,
	}
	return c.send(event)
}

func (c *PagerDutyClient) send(event *PagerDutyEvent) error {
	if c.disabled {
		return nil
	}
	if c.routingKey == "" {
		return fmt.Errorf("pagerduty: routing key is required")
	}

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("pagerduty: failed to marshal event: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.apiURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("pagerduty: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("pagerduty: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pagerduty: unexpected status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// mapSeverity maps alert severity strings to PagerDuty severity levels.
func mapSeverity(severity string) string {
	switch severity {
	case "critical":
		return SeverityCritical
	case "error":
		return SeverityError
	case "warning":
		return SeverityWarning
	case "info":
		return SeverityInfo
	default:
		return SeverityWarning
	}
}
