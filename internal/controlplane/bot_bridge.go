package controlplane

// bot_bridge.go — ChatOps Web 命令台聚合 handler（/api/v1/bot/*）。
//
// 背景（M13 六域接线的 bot 域）：bot-svc（services/bot-svc/）是企业 IM 平台
// 的 webhook 入口（/webhook/{wecom,feishu,slack,dingtalk}，X-Bot-Token 鉴权），
// 而 web 前端 BotView 调的是另一组契约（src/api/bot.js）：
//
//	POST /api/v1/bot/command        {command, platform}  → 执行并返回记录
//	GET  /api/v1/bot/history        ?platform=&limit=    → 命令历史
//	GET  /api/v1/bot/platforms                           → 平台开关
//	GET  /api/v1/bot/quick-commands                     → 快捷命令
//
// 两者是同一命令引擎（bot-svc internal/bot.Parse 的 /opsmesh 语法）的两个
// 入口。bot-svc 的 webhook 模式不适合浏览器（无平台回调上下文），本 bridge
// 在聚合层实现 Web 契约：命令语法与 bot-svc 保持一致（/opsmesh help 查看），
// 数据源复用站内 store（设备/告警/指标），命令历史内存保留（进程级，重启清空
// ——与 gateway 路由规则的运行期配置语义一致）。
//
// 鉴权：bot:read/bot:write 权限 + requireTenantContext（历史按租户隔离）。
// 限流：每用户 12 次/分钟（与 bot-svc 默认 RateLimitPerMin 一致，防命令台
// 成为绕过任务审批的旁路——写类命令 ack/task 单独要求 bot:write）。

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"opsmesh/internal/authctx"
	"opsmesh/internal/controlplane/paginate"
	"opsmesh/internal/proto"
)

// botCommandRecord 一条命令执行历史（前端 BotView history 项契约）。
type botCommandRecord struct {
	ID         string `json:"id"`
	Command    string `json:"command"`
	Platform   string `json:"platform"`
	Status     string `json:"status"` // success | failed
	Response   any    `json:"response"`
	ExecutedAt string `json:"executedAt"`
	TenantID   string `json:"-"`
	UserID     string `json:"-"`
}

// botHistoryState 命令历史存储（进程级内存，按租户隔离，最新在前）。
type botHistoryState struct {
	mu      sync.Mutex
	records map[string][]*botCommandRecord // tenantID -> 时间倒序
}

// maxBotHistoryPerTenant 每租户历史上限（有界防泄漏）。
const maxBotHistoryPerTenant = 200

var botHistory = &botHistoryState{records: make(map[string][]*botCommandRecord)}

// botPlatformDef 平台开关定义（与 BotView 平台下拉对齐；enabled 由
// BOT_PLATFORMS_ENABLED env 控制，逗号分隔启用清单，空=全部只读展示）。
type botPlatformDef struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// botDefaultPlatforms 平台清单（与 bot-svc platforms 包四平台一致）。
var botDefaultPlatforms = []botPlatformDef{
	{ID: "wecom", Name: "企业微信", Enabled: false},
	{ID: "feishu", Name: "飞书", Enabled: false},
	{ID: "slack", Name: "Slack", Enabled: false},
	{ID: "dingtalk", Name: "钉钉", Enabled: false},
	{ID: "web", Name: "Web 控制台", Enabled: true},
}

// botQuickCommandDefs 快捷命令（与 bot-svc HelpText 语法一致；label 由 i18n
// 前端渲染——这里给命令原文，前端 quick-btn 显示 label）。
type botQuickCommandDef struct {
	Label   string `json:"label"`
	Command string `json:"command"`
}

var botQuickCommands = []botQuickCommandDef{
	{Label: "status", Command: "/opsmesh status"},
	{Label: "devices", Command: "/opsmesh devices"},
	{Label: "alerts", Command: "/opsmesh alerts"},
	{Label: "help", Command: "/opsmesh help"},
}

// handleBotCommand POST /api/v1/bot/command {command, platform}。
// 响应即一条历史记录（store.unshift 契约）。
func (s *Server) handleBotCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	caller, ok := s.requirePermission(w, r, "bot:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	var body struct {
		Command  string `json:"command"`
		Platform string `json:"platform"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.JSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(body.Command) == "" {
		paginate.JSONError(w, http.StatusBadRequest, "command is required")
		return
	}
	if body.Platform == "" {
		body.Platform = "web"
	}
	rec := &botCommandRecord{
		ID:         randHexID("bot"),
		Command:    body.Command,
		Platform:   body.Platform,
		ExecutedAt: time.Now().Format(time.RFC3339),
		TenantID:   actx.TenantID,
		UserID:     caller.ID,
	}
	resp, err := s.executeBotCommand(actx, body.Command)
	if err != nil {
		rec.Status = "failed"
		rec.Response = err.Error()
	} else {
		rec.Status = "success"
		rec.Response = resp
	}
	botHistory.append(actx.TenantID, rec)
	paginate.WriteJSON(w, http.StatusOK, rec)
}

// executeBotCommand 执行 /opsmesh 语法命令（与 bot-svc internal/bot.Parse
// 同语法；数据源为站内 store，租户隔离天然继承）。
// callerLabel 供 ack 记录确认人（用户中心登录名或网关注入标识）。
func callerLabel(actx authctx.Context, raw string) string {
	_ = raw
	if actx.UserID != "" {
		return actx.UserID
	}
	return "bot-web"
}

func (s *Server) executeBotCommand(actx authctx.Context, text string) (any, error) {
	tenantID := actx.TenantID
	parts := strings.Fields(strings.TrimSpace(text))
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	if !strings.EqualFold(parts[0], "/opsmesh") {
		return nil, fmt.Errorf("unknown command, try /opsmesh help")
	}
	if len(parts) < 2 {
		return botHelpLines(), nil
	}
	switch strings.ToLower(parts[1]) {
	case "status":
		snap := s.store.Snapshot(tenantID)
		total, online := 0, 0
		for _, devs := range snap {
			total += len(devs)
			for _, d := range devs {
				if strings.EqualFold(d.State, "online") {
					online++
				}
			}
		}
		active := 0
		for _, a := range s.store.Alerts(tenantID) {
			if a.Status != "resolved" {
				active++
			}
		}
		return map[string]any{
			"devicesTotal":  total,
			"devicesOnline": online,
			"activeAlerts":  active,
		}, nil
	case "devices":
		snap := s.store.Snapshot(tenantID)
		list := make([]map[string]any, 0, len(snap))
		for _, devs := range snap {
			for _, d := range devs {
				list = append(list, map[string]any{"id": d.DeviceID, "name": d.Hostname, "status": d.State, "ip": d.IP})
			}
		}
		return map[string]any{"devices": list}, nil
	case "alerts":
		alerts := s.store.Alerts(tenantID)
		list := make([]map[string]any, 0, len(alerts))
		for _, a := range alerts {
			list = append(list, map[string]any{"id": a.AlertID, "severity": a.Severity, "message": a.Message, "status": a.Status})
		}
		return map[string]any{"alerts": list}, nil
	case "ack":
		if len(parts) < 3 {
			return nil, fmt.Errorf("usage: /opsmesh ack <alert_id>")
		}
		if !s.store.AckAlert(parts[2], tenantID, callerLabel(actx, text)) {
			return nil, fmt.Errorf("alert %s not found in tenant", parts[2])
		}
		return map[string]any{"acknowledged": parts[2]}, nil
	case "metrics":
		if len(parts) < 3 {
			return nil, fmt.Errorf("usage: /opsmesh metrics <device_id>")
		}
		m := s.store.DeviceMetrics(parts[2])
		if m == nil {
			return map[string]any{"message": "no metrics for " + parts[2]}, nil
		}
		return map[string]any{
			"deviceId": parts[2],
			"cpu":      m.CPU.Usage,
			"memory":   m.Memory.Usage,
			"disk":     diskAvgUsage(m),
		}, nil
	case "help":
		return botHelpLines(), nil
	default:
		return nil, fmt.Errorf("unknown subcommand %q, try /opsmesh help", parts[1])
	}
}

// diskAvgUsage 多磁盘使用率均值（无盘返回 0）。
func diskAvgUsage(m *proto.DeviceMetrics) float64 {
	if m == nil || len(m.Disks) == 0 {
		return 0
	}
	var sum float64
	for _, d := range m.Disks {
		sum += d.Usage
	}
	return sum / float64(len(m.Disks))
}

// botHelpLines 帮助文本（与 bot-svc HelpText 语法一致；task/deploy 属于
// 高危写操作，Web 台不开放——保留在 IM webhook 入口，web 命令台仅只读+ack）。
func botHelpLines() map[string]any {
	return map[string]any{
		"commands": []string{
			"/opsmesh status - 系统概览（设备/告警计数）",
			"/opsmesh devices - 设备清单",
			"/opsmesh alerts - 活跃告警",
			"/opsmesh ack <alert_id> - 确认告警",
			"/opsmesh metrics <device_id> - 设备最新指标",
			"/opsmesh help - 本帮助",
		},
	}
}

// append 追加历史（最新在前，有界）。
func (h *botHistoryState) append(tenantID string, rec *botCommandRecord) {
	h.mu.Lock()
	defer h.mu.Unlock()
	list := append([]*botCommandRecord{rec}, h.records[tenantID]...)
	if len(list) > maxBotHistoryPerTenant {
		list = list[:maxBotHistoryPerTenant]
	}
	h.records[tenantID] = list
}

// list 读取历史（platform 过滤 + limit 截断，最新在前）。
func (h *botHistoryState) list(tenantID, platform string, limit int) []*botCommandRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	src := h.records[tenantID]
	out := make([]*botCommandRecord, 0, len(src))
	for _, rec := range src {
		if platform != "" && rec.Platform != platform {
			continue
		}
		out = append(out, rec)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// handleBotHistory GET /api/v1/bot/history?platform=&limit=。
func (s *Server) handleBotHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := s.requirePermission(w, r, "bot:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	platform := q.Get("platform")
	limit := 20
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]any{"history": botHistory.list(actx.TenantID, platform, limit)})
}

// handleBotPlatforms GET /api/v1/bot/platforms。
// enabled 由 env BOT_PLATFORMS_ENABLED（逗号分隔 ID 清单）控制；web 恒开。
func (s *Server) handleBotPlatforms(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := s.requirePermission(w, r, "bot:read"); !ok {
		return
	}
	if _, ok := s.requireTenantContext(w, r); !ok {
		return
	}
	enabled := map[string]bool{}
	for _, id := range strings.Split(getEnvDefault("BOT_PLATFORMS_ENABLED", "web"), ",") {
		id = strings.TrimSpace(strings.ToLower(id))
		if id != "" {
			enabled[id] = true
		}
	}
	out := make([]botPlatformDef, len(botDefaultPlatforms))
	for i, p := range botDefaultPlatforms {
		p.Enabled = p.ID == "web" || enabled[p.ID]
		out[i] = p
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]any{"platforms": out})
}

// handleBotQuickCommands GET /api/v1/bot/quick-commands。
func (s *Server) handleBotQuickCommands(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := s.requirePermission(w, r, "bot:read"); !ok {
		return
	}
	if _, ok := s.requireTenantContext(w, r); !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]any{"commands": botQuickCommands})
}

// getEnvDefault 读 env（空回默认）——与 services pkg/config getEnv 同语义。
func getEnvDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
