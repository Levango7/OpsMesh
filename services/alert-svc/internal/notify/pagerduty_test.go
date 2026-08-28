package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPagerDutyClientTriggerEvent(t *testing.T) {
	var receivedAction EventAction
	var receivedRoutingKey string
	var receivedDedupKey string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", ct)
		}
		body, _ := io.ReadAll(r.Body)
		var event PagerDutyEvent
		if err := json.Unmarshal(body, &event); err != nil {
			t.Fatalf("failed to unmarshal event: %v", err)
		}
		receivedAction = event.EventAction
		receivedRoutingKey = event.RoutingKey
		receivedDedupKey = event.DedupKey
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	client := NewPagerDutyClient("test-routing-key", srv.URL, 5*time.Second, false)
	err := client.TriggerEvent("device-1", "High CPU usage", "critical", "dedup-1", map[string]interface{}{
		"cpu_usage": 95.5,
	})
	if err != nil {
		t.Fatalf("TriggerEvent failed: %v", err)
	}
	if receivedAction != ActionTrigger {
		t.Errorf("expected action %s, got %s", ActionTrigger, receivedAction)
	}
	if receivedRoutingKey != "test-routing-key" {
		t.Errorf("expected routing key test-routing-key, got %s", receivedRoutingKey)
	}
	if receivedDedupKey != "dedup-1" {
		t.Errorf("expected dedup key dedup-1, got %s", receivedDedupKey)
	}
}

func TestPagerDutyClientAcknowledgeEvent(t *testing.T) {
	var receivedAction EventAction

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var event PagerDutyEvent
		_ = json.Unmarshal(body, &event)
		receivedAction = event.EventAction
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	client := NewPagerDutyClient("test-routing-key", srv.URL, 5*time.Second, false)
	err := client.AcknowledgeEvent("device-1", "Acknowledged", "dedup-1", nil)
	if err != nil {
		t.Fatalf("AcknowledgeEvent failed: %v", err)
	}
	if receivedAction != ActionAcknowledge {
		t.Errorf("expected action %s, got %s", ActionAcknowledge, receivedAction)
	}
}

func TestPagerDutyClientResolveEvent(t *testing.T) {
	var receivedAction EventAction

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var event PagerDutyEvent
		_ = json.Unmarshal(body, &event)
		receivedAction = event.EventAction
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	client := NewPagerDutyClient("test-routing-key", srv.URL, 5*time.Second, false)
	err := client.ResolveEvent("device-1", "Resolved", "dedup-1", nil)
	if err != nil {
		t.Fatalf("ResolveEvent failed: %v", err)
	}
	if receivedAction != ActionResolve {
		t.Errorf("expected action %s, got %s", ActionResolve, receivedAction)
	}
}

func TestPagerDutyClientServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid routing key"}`))
	}))
	defer srv.Close()

	client := NewPagerDutyClient("bad-key", srv.URL, 5*time.Second, false)
	err := client.TriggerEvent("device-1", "Test", "warning", "", nil)
	if err == nil {
		t.Fatal("expected error for server 400 response, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected error to contain status 400, got: %v", err)
	}
}

func TestPagerDutyClientDisabled(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	client := NewPagerDutyClient("test-key", srv.URL, 5*time.Second, true)
	err := client.TriggerEvent("device-1", "Test", "warning", "", nil)
	if err != nil {
		t.Fatalf("disabled client should not return error, got: %v", err)
	}
	if called {
		t.Error("disabled client should not make HTTP requests")
	}
}

func TestPagerDutyClientEmptyRoutingKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	client := NewPagerDutyClient("", srv.URL, 5*time.Second, false)
	err := client.TriggerEvent("device-1", "Test", "warning", "", nil)
	if err == nil {
		t.Fatal("expected error for empty routing key, got nil")
	}
	if !strings.Contains(err.Error(), "routing key") {
		t.Errorf("expected routing key error, got: %v", err)
	}
}

func TestPagerDutyClientPayloadContents(t *testing.T) {
	var receivedEvent PagerDutyEvent

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedEvent)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	client := NewPagerDutyClient("key-123", srv.URL, 5*time.Second, false)
	details := map[string]interface{}{
		"cpu_usage": 98.2,
		"host":      "server-01",
	}
	err := client.TriggerEvent("server-01", "CPU threshold exceeded", "critical", "alert-42", details)
	if err != nil {
		t.Fatalf("TriggerEvent failed: %v", err)
	}

	if receivedEvent.Payload == nil {
		t.Fatal("expected payload to be non-nil")
	}
	if receivedEvent.Payload.Summary != "CPU threshold exceeded" {
		t.Errorf("expected summary 'CPU threshold exceeded', got %s", receivedEvent.Payload.Summary)
	}
	if receivedEvent.Payload.Source != "server-01" {
		t.Errorf("expected source 'server-01', got %s", receivedEvent.Payload.Source)
	}
	if receivedEvent.Payload.Severity != SeverityCritical {
		t.Errorf("expected severity %s, got %s", SeverityCritical, receivedEvent.Payload.Severity)
	}
	if receivedEvent.Payload.Details["cpu_usage"] != 98.2 {
		t.Errorf("expected detail cpu_usage=98.2, got %v", receivedEvent.Payload.Details["cpu_usage"])
	}
}

func TestMapSeverity(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"critical", SeverityCritical},
		{"error", SeverityError},
		{"warning", SeverityWarning},
		{"info", SeverityInfo},
		{"unknown", SeverityWarning},
		{"", SeverityWarning},
	}
	for _, tt := range tests {
		got := mapSeverity(tt.input)
		if got != tt.expected {
			t.Errorf("mapSeverity(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestIsEnabled(t *testing.T) {
	client := NewPagerDutyClient("key", "https://events.pagerduty.com/v2/enqueue", 5*time.Second, false)
	if !client.IsEnabled() {
		t.Error("expected client to be enabled with routing key and not disabled")
	}

	clientDisabled := NewPagerDutyClient("key", "https://events.pagerduty.com/v2/enqueue", 5*time.Second, true)
	if clientDisabled.IsEnabled() {
		t.Error("expected client to be disabled when disabled=true")
	}

	clientNoKey := NewPagerDutyClient("", "https://events.pagerduty.com/v2/enqueue", 5*time.Second, false)
	if clientNoKey.IsEnabled() {
		t.Error("expected client to be disabled with empty routing key")
	}
}
