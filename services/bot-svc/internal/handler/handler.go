package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/Levango7/OpsMesh/services/bot-svc/internal/bot"
	"github.com/Levango7/OpsMesh/services/bot-svc/internal/client"
	"github.com/Levango7/OpsMesh/services/bot-svc/internal/platforms"
	"github.com/Levango7/OpsMesh/services/bot-svc/pkg/config"
)

// Handler handles HTTP webhook requests.
type Handler struct {
	bot    *bot.Bot
	client *client.OpsMeshClient
	config *config.Config
}

// NewHandler creates a new HTTP handler.
func NewHandler(cfg *config.Config, apiClient *client.OpsMeshClient) *Handler {
	return &Handler{
		bot:    bot.NewBot(cfg.RateLimitPerMin),
		client: apiClient,
		config: cfg,
	}
}

// RegisterRoutes registers all HTTP routes.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/webhook/wecom", h.handleWecom)
	mux.HandleFunc("/webhook/feishu", h.handleFeishu)
	mux.HandleFunc("/webhook/slack", h.handleSlack)
	mux.HandleFunc("/webhook/dingtalk", h.handleDingtalk)
	mux.HandleFunc("/health", h.handleHealth)
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (h *Handler) handleWecom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.processWebhook(w, r, platforms.Wecom, h.config.WecomToken)
}

func (h *Handler) handleFeishu(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.processWebhook(w, r, platforms.Feishu, h.config.FeishuToken)
}

func (h *Handler) handleSlack(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.processWebhook(w, r, platforms.Slack, h.config.SlackToken)
}

func (h *Handler) handleDingtalk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.processWebhook(w, r, platforms.Dingtalk, h.config.DingtalkToken)
}

func (h *Handler) processWebhook(w http.ResponseWriter, r *http.Request, p platforms.Platform, expectedToken string) {
	token := r.Header.Get("X-Bot-Token")
	if !platforms.VerifyToken(p, token, expectedToken) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	payload, err := platforms.ParseWebhook(p, body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if !h.bot.CheckRateLimit(payload.UserID) {
		h.writeResponse(w, p, "Rate limit exceeded. Please wait before sending more commands.")
		return
	}

	parsed, err := h.bot.Parse(payload.UserID, payload.Text)
	if err != nil {
		h.writeResponse(w, p, fmt.Sprintf("Error: %v", err))
		return
	}

	result := h.executeCommand(parsed)
	formatted := platforms.FormatResponse(p, result)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(formatted)
}

func (h *Handler) executeCommand(cmd *bot.ParsedCommand) string {
	switch cmd.Type {
	case bot.CmdStatus:
		status, err := h.client.GetStatus()
		if err != nil {
			return fmt.Sprintf("Error fetching status: %v", err)
		}
		return fmt.Sprintf("System Status:\n- Devices Online: %d\n- Active Alerts: %d\n- Healthy: %t",
			status.DevicesOnline, status.ActiveAlerts, status.Healthy)

	case bot.CmdDevices:
		devices, err := h.client.ListDevices()
		if err != nil {
			return fmt.Sprintf("Error listing devices: %v", err)
		}
		if len(devices) == 0 {
			return "No devices found."
		}
		var sb string
		sb = "Devices:\n"
		for _, d := range devices {
			sb += fmt.Sprintf("- %s (%s) [%s] %s\n", d.Name, d.ID, d.Status, d.IP)
		}
		return sb

	case bot.CmdAlerts:
		alerts, err := h.client.ListAlerts()
		if err != nil {
			return fmt.Sprintf("Error listing alerts: %v", err)
		}
		if len(alerts) == 0 {
			return "No active alerts."
		}
		var sb string
		sb = "Active Alerts:\n"
		for _, a := range alerts {
			sb += fmt.Sprintf("- [%s] %s: %s (%s)\n", a.Severity, a.ID, a.Message, a.Status)
		}
		return sb

	case bot.CmdAck:
		alertID := cmd.Args[0]
		if err := h.client.AckAlert(alertID); err != nil {
			return fmt.Sprintf("Error acknowledging alert %s: %v", alertID, err)
		}
		return fmt.Sprintf("Alert %s acknowledged.", alertID)

	case bot.CmdTask:
		deviceID := cmd.Args[0]
		command := cmd.Args[1]
		result, err := h.client.ExecuteTask(deviceID, command)
		if err != nil {
			return fmt.Sprintf("Error executing task on %s: %v", deviceID, err)
		}
		return fmt.Sprintf("Task executed on %s:\n- Task ID: %s\n- Success: %t\n- Output: %s",
			deviceID, result.TaskID, result.Success, result.Output)

	case bot.CmdDeploy:
		appID := cmd.Args[0]
		version := cmd.Args[1]
		result, err := h.client.TriggerDeploy(appID, version)
		if err != nil {
			return fmt.Sprintf("Error deploying %s: %v", appID, err)
		}
		return fmt.Sprintf("Deployment triggered:\n- Deploy ID: %s\n- Success: %t\n- Message: %s",
			result.DeployID, result.Success, result.Message)

	case bot.CmdMetrics:
		deviceID := cmd.Args[0]
		metrics, err := h.client.GetDeviceMetrics(deviceID)
		if err != nil {
			return fmt.Sprintf("Error getting metrics for %s: %v", deviceID, err)
		}
		return fmt.Sprintf("Metrics for %s:\n- CPU: %.1f%%\n- Memory: %.1f%%\n- Disk: %.1f%%",
			metrics.DeviceID, metrics.CPU, metrics.Memory, metrics.Disk)

	case bot.CmdHelp:
		return bot.HelpText()

	case bot.CmdUnknown:
		return fmt.Sprintf("Unknown command. %s", bot.HelpText())

	default:
		return bot.HelpText()
	}
}

func (h *Handler) writeResponse(w http.ResponseWriter, p platforms.Platform, msg string) {
	formatted := platforms.FormatResponse(p, msg)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(formatted)
}

var _ = log.Printf
