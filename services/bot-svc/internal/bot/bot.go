package bot

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// CommandType represents a recognized bot command.
type CommandType string

const (
	CmdStatus  CommandType = "status"
	CmdDevices CommandType = "devices"
	CmdAlerts  CommandType = "alerts"
	CmdAck     CommandType = "ack"
	CmdTask    CommandType = "task"
	CmdDeploy  CommandType = "deploy"
	CmdMetrics CommandType = "metrics"
	CmdHelp    CommandType = "help"
	CmdUnknown CommandType = "unknown"
)

// ParsedCommand holds the result of parsing a user message.
type ParsedCommand struct {
	Type   CommandType
	Args   []string
	Raw    string
	UserID string
}

// Result holds the output of executing a command.
type Result struct {
	Success bool
	Message string
	Data    map[string]any
}

// Bot handles command parsing, routing, and rate limiting.
type Bot struct {
	mu              sync.RWMutex
	rateLimiter     map[string][]time.Time
	rateLimitPerMin int
}

// NewBot creates a new Bot instance.
func NewBot(rateLimitPerMin int) *Bot {
	return &Bot{
		rateLimiter:     make(map[string][]time.Time),
		rateLimitPerMin: rateLimitPerMin,
	}
}

// Parse extracts a command from raw message text.
func (b *Bot) Parse(userID, text string) (*ParsedCommand, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("empty command")
	}

	parts := strings.Fields(text)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	cmd := strings.ToLower(parts[0])
	parsed := &ParsedCommand{
		Raw:    text,
		UserID: userID,
	}

	switch cmd {
	case "/opsmesh":
		if len(parts) < 2 {
			parsed.Type = CmdHelp
			return parsed, nil
		}
		sub := strings.ToLower(parts[1])
		switch sub {
		case "status":
			parsed.Type = CmdStatus
		case "devices":
			parsed.Type = CmdDevices
		case "alerts":
			parsed.Type = CmdAlerts
		case "ack":
			parsed.Type = CmdAck
			if len(parts) > 2 {
				parsed.Args = parts[2:]
			} else {
				return nil, fmt.Errorf("usage: /opsmesh ack <alert_id>")
			}
		case "task":
			parsed.Type = CmdTask
			if len(parts) > 3 {
				parsed.Args = parts[2:]
			} else {
				return nil, fmt.Errorf("usage: /opsmesh task <device_id> <command>")
			}
		case "deploy":
			parsed.Type = CmdDeploy
			if len(parts) > 3 {
				parsed.Args = parts[2:]
			} else {
				return nil, fmt.Errorf("usage: /opsmesh deploy <app_id> <version>")
			}
		case "metrics":
			parsed.Type = CmdMetrics
			if len(parts) > 2 {
				parsed.Args = parts[2:]
			} else {
				return nil, fmt.Errorf("usage: /opsmesh metrics <device_id>")
			}
		case "help":
			parsed.Type = CmdHelp
		default:
			parsed.Type = CmdUnknown
		}
	default:
		parsed.Type = CmdUnknown
	}

	return parsed, nil
}

// CheckRateLimit returns true if the user is within rate limits.
func (b *Bot) CheckRateLimit(userID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-time.Minute)

	timestamps := b.rateLimiter[userID]
	valid := make([]time.Time, 0, len(timestamps))
	for _, ts := range timestamps {
		if ts.After(windowStart) {
			valid = append(valid, ts)
		}
	}

	if len(valid) >= b.rateLimitPerMin {
		b.rateLimiter[userID] = valid
		return false
	}

	valid = append(valid, now)
	b.rateLimiter[userID] = valid
	return true
}

// HelpText returns the help message for all supported commands.
func HelpText() string {
	return `Available OpsMesh commands:
/opsmesh status              - Show system status
/opsmesh devices             - List managed devices
/opsmesh alerts              - List active alerts
/opsmesh ack <alert_id>      - Acknowledge an alert
/opsmesh task <device_id> <command> - Execute command on device
/opsmesh deploy <app_id> <version>  - Trigger deployment
/opsmesh metrics <device_id> - Show device metrics
/opsmesh help                - Show this help`
}
