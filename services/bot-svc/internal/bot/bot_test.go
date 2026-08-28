package bot

import (
	"testing"
	"time"
)

func TestParseStatus(t *testing.T) {
	b := NewBot(30)
	cmd, err := b.Parse("user1", "/opsmesh status")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Type != CmdStatus {
		t.Errorf("expected CmdStatus, got %v", cmd.Type)
	}
	if cmd.UserID != "user1" {
		t.Errorf("expected user1, got %s", cmd.UserID)
	}
}

func TestParseDevices(t *testing.T) {
	b := NewBot(30)
	cmd, err := b.Parse("user1", "/opsmesh devices")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Type != CmdDevices {
		t.Errorf("expected CmdDevices, got %v", cmd.Type)
	}
}

func TestParseAlerts(t *testing.T) {
	b := NewBot(30)
	cmd, err := b.Parse("user1", "/opsmesh alerts")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Type != CmdAlerts {
		t.Errorf("expected CmdAlerts, got %v", cmd.Type)
	}
}

func TestParseAck(t *testing.T) {
	b := NewBot(30)
	cmd, err := b.Parse("user1", "/opsmesh ack alert-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Type != CmdAck {
		t.Errorf("expected CmdAck, got %v", cmd.Type)
	}
	if len(cmd.Args) != 1 || cmd.Args[0] != "alert-123" {
		t.Errorf("expected [alert-123], got %v", cmd.Args)
	}
}

func TestParseAckMissingArg(t *testing.T) {
	b := NewBot(30)
	_, err := b.Parse("user1", "/opsmesh ack")
	if err == nil {
		t.Fatal("expected error for missing alert_id")
	}
}

func TestParseTask(t *testing.T) {
	b := NewBot(30)
	cmd, err := b.Parse("user1", "/opsmesh task device-1 restart")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Type != CmdTask {
		t.Errorf("expected CmdTask, got %v", cmd.Type)
	}
	if len(cmd.Args) != 2 || cmd.Args[0] != "device-1" || cmd.Args[1] != "restart" {
		t.Errorf("expected [device-1 restart], got %v", cmd.Args)
	}
}

func TestParseTaskMissingArgs(t *testing.T) {
	b := NewBot(30)
	_, err := b.Parse("user1", "/opsmesh task device-1")
	if err == nil {
		t.Fatal("expected error for missing command")
	}
}

func TestParseDeploy(t *testing.T) {
	b := NewBot(30)
	cmd, err := b.Parse("user1", "/opsmesh deploy app-1 v2.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Type != CmdDeploy {
		t.Errorf("expected CmdDeploy, got %v", cmd.Type)
	}
	if len(cmd.Args) != 2 || cmd.Args[0] != "app-1" || cmd.Args[1] != "v2.0" {
		t.Errorf("expected [app-1 v2.0], got %v", cmd.Args)
	}
}

func TestParseDeployMissingArgs(t *testing.T) {
	b := NewBot(30)
	_, err := b.Parse("user1", "/opsmesh deploy app-1")
	if err == nil {
		t.Fatal("expected error for missing version")
	}
}

func TestParseMetrics(t *testing.T) {
	b := NewBot(30)
	cmd, err := b.Parse("user1", "/opsmesh metrics device-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Type != CmdMetrics {
		t.Errorf("expected CmdMetrics, got %v", cmd.Type)
	}
	if len(cmd.Args) != 1 || cmd.Args[0] != "device-1" {
		t.Errorf("expected [device-1], got %v", cmd.Args)
	}
}

func TestParseMetricsMissingArg(t *testing.T) {
	b := NewBot(30)
	_, err := b.Parse("user1", "/opsmesh metrics")
	if err == nil {
		t.Fatal("expected error for missing device_id")
	}
}

func TestParseHelp(t *testing.T) {
	b := NewBot(30)
	cmd, err := b.Parse("user1", "/opsmesh help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Type != CmdHelp {
		t.Errorf("expected CmdHelp, got %v", cmd.Type)
	}
}

func TestParseEmpty(t *testing.T) {
	b := NewBot(30)
	_, err := b.Parse("user1", "")
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestParseUnknownSubcommand(t *testing.T) {
	b := NewBot(30)
	cmd, err := b.Parse("user1", "/opsmesh foobar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Type != CmdUnknown {
		t.Errorf("expected CmdUnknown, got %v", cmd.Type)
	}
}

func TestParseNoPrefix(t *testing.T) {
	b := NewBot(30)
	cmd, err := b.Parse("user1", "hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Type != CmdUnknown {
		t.Errorf("expected CmdUnknown, got %v", cmd.Type)
	}
}

func TestParseOpsmeshOnly(t *testing.T) {
	b := NewBot(30)
	cmd, err := b.Parse("user1", "/opsmesh")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Type != CmdHelp {
		t.Errorf("expected CmdHelp, got %v", cmd.Type)
	}
}

func TestRateLimit(t *testing.T) {
	b := NewBot(3)
	user := "user-rate"

	if !b.CheckRateLimit(user) {
		t.Error("first request should pass")
	}
	if !b.CheckRateLimit(user) {
		t.Error("second request should pass")
	}
	if !b.CheckRateLimit(user) {
		t.Error("third request should pass")
	}
	if b.CheckRateLimit(user) {
		t.Error("fourth request should be rate limited")
	}
}

func TestRateLimitWindow(t *testing.T) {
	b := NewBot(2)
	user := "user-window"

	b.mu.Lock()
	b.rateLimiter[user] = []time.Time{
		time.Now().Add(-2 * time.Minute),
		time.Now().Add(-90 * time.Second),
	}
	b.mu.Unlock()

	if !b.CheckRateLimit(user) {
		t.Error("old timestamps should have expired, request should pass")
	}
}

func TestRateLimitDifferentUsers(t *testing.T) {
	b := NewBot(1)

	if !b.CheckRateLimit("user-a") {
		t.Error("user-a first request should pass")
	}
	if b.CheckRateLimit("user-a") {
		t.Error("user-a second request should be limited")
	}
	if !b.CheckRateLimit("user-b") {
		t.Error("user-b request should pass (different user)")
	}
}

func TestHelpText(t *testing.T) {
	text := HelpText()
	if text == "" {
		t.Error("help text should not be empty")
	}
}
