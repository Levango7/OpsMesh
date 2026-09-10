// Package http 提供 device-svc 的 HTTP 网关（REST API）。
//
// 设计原则（M13 device-svc 薄客户端化 P0）：
//   - 纯增量：新增 HTTP handler，不改 server.go/service.go/store.go 任何现有代码
//   - 同进程直连 store 层（MemoryStore/MySQLStore 都实现这些接口），
//     不经 gRPC——避免 proto 序列化开销，前端 service_proxy 可直接转发
//   - RESTful 路径：/api/v1/devices, /api/v1/agents, /api/v1/cmdb/cis, /api/v1/discovery/jobs
//   - 错误语义：400 参数错 / 404 不存在 / 500 内部错（与站内其他 API 一致）
//   - JSON 契约：字段名与前端 web/enterprise/src/api/device.js 的消费方式对齐
package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Levango7/OpsMesh/services/device-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/device-svc/internal/store"
	"opsmesh/pkg/provision"
)

// Gateway 持有 HTTP handler 依赖的各 store 接口。
type Gateway struct {
	devices        store.DeviceStore
	agents         store.AgentStore
	cis            store.CiStore
	discovery      store.DiscoveryStore
	provisionStore store.ProvisionStore
	advertiseAddr  string
}

// NewGateway 构造 Gateway 实例。
func NewGateway(ds store.DeviceStore, as store.AgentStore, cs store.CiStore, disc store.DiscoveryStore, ps store.ProvisionStore, advertiseAddr string) *Gateway {
	return &Gateway{devices: ds, agents: as, cis: cs, discovery: disc, provisionStore: ps, advertiseAddr: advertiseAddr}
}

// RegisterRoutes 注册全部 HTTP 路由到给定 mux。
// 鉴权中间件由 main 传入（tenant.Middleware 已有——与 gRPC 拦截器同语义）。
func (g *Gateway) RegisterRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler) {
	// 设备
	mux.Handle("/api/v1/devices", auth(http.HandlerFunc(g.handleDevices)))
	mux.Handle("/api/v1/devices/", auth(http.HandlerFunc(g.handleDeviceDetail)))
	// Agent
	mux.Handle("/api/v1/agents", auth(http.HandlerFunc(g.handleAgents)))
	mux.Handle("/api/v1/agents/", auth(http.HandlerFunc(g.handleAgentDetail)))
	// CMDB
	mux.Handle("/api/v1/cmdb/cis", auth(http.HandlerFunc(g.handleCIs)))
	mux.Handle("/api/v1/cmdb/cis/", auth(http.HandlerFunc(g.handleCIDetail)))
	mux.Handle("/api/v1/cmdb/relations/", auth(http.HandlerFunc(g.handleCIRelations)))
	// 发现
	mux.Handle("/api/v1/discovery/jobs", auth(http.HandlerFunc(g.handleDiscoveryJobs)))
	mux.Handle("/api/v1/discovery/jobs/", auth(http.HandlerFunc(g.handleDiscoveryJobStatus)))
	mux.Handle("/api/v1/discovery/devices", auth(http.HandlerFunc(g.handleDiscoveredDevices)))
	// 自动纳管（D3）
	mux.Handle("/api/v1/provision/auto", auth(http.HandlerFunc(g.handleProvisionAuto)))
	// bootstrap 端点（D3-d）：install.sh 分发 + token 消费注册。
	// 不挂 auth（agent 自举阶段无 JWT；token 本身即凭证，与 controlplane /install.sh 同语义）。
	mux.HandleFunc("/install.sh", g.handleInstallSh)
	mux.HandleFunc("/api/v1/provision/register", g.handleProvisionRegister)
}

// ============ 设备 ============

func (g *Gateway) handleDevices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		devs := g.devices.ListDevices(q.Get("tenantID"), q.Get("status"), q.Get("group"), limit)
		writeJSON(w, http.StatusOK, map[string]any{"devices": devs})
	case http.MethodPost:
		var d models.Device
		if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		created := g.devices.RegisterDevice(&d)
		if created == nil {
			writeError(w, http.StatusBadRequest, "device invalid")
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (g *Gateway) handleDeviceDetail(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/devices/")
	// 子路径：{id}/heartbeat（POST）、{id}/status（GET）
	switch {
	case strings.HasSuffix(rest, "/heartbeat") && r.Method == http.MethodPost:
		id := strings.TrimSuffix(rest, "/heartbeat")
		var body struct {
			Status string `json:"status"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !g.devices.Heartbeat(id, body.Status) {
			writeError(w, http.StatusNotFound, "device not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	case strings.HasSuffix(rest, "/status") && r.Method == http.MethodGet:
		id := strings.TrimSuffix(rest, "/status")
		st := g.devices.GetDeviceStatus(id)
		if st == nil {
			writeError(w, http.StatusNotFound, "device not found")
			return
		}
		writeJSON(w, http.StatusOK, st)
		return
	}
	if rest == "" || strings.Contains(rest, "/") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		d := g.devices.Device(rest)
		if d == nil {
			writeError(w, http.StatusNotFound, "device not found")
			return
		}
		writeJSON(w, http.StatusOK, d)
	case http.MethodPut:
		var d models.Device
		if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		d.ID = rest
		updated, ok := g.devices.UpdateDevice(&d)
		if !ok {
			writeError(w, http.StatusNotFound, "device not found")
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		if !g.devices.DeleteDevice(rest) {
			writeError(w, http.StatusNotFound, "device not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// ============ Agent ============

func (g *Gateway) handleAgents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		agents := g.agents.ListAgents(q.Get("tenantID"), q.Get("status"), limit)
		writeJSON(w, http.StatusOK, map[string]any{"agents": agents})
	case http.MethodPost:
		var a models.Agent
		if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		created := g.agents.RegisterAgent(&a)
		if created == nil {
			writeError(w, http.StatusBadRequest, "agent invalid")
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (g *Gateway) handleAgentDetail(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/agents/")
	// 子路径：{id}/heartbeat（POST）
	if strings.HasSuffix(rest, "/heartbeat") && r.Method == http.MethodPost {
		id := strings.TrimSuffix(rest, "/heartbeat")
		var body struct {
			Status string `json:"status"`
			Load   int    `json:"load"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !g.agents.AgentHeartbeat(id, body.Status, body.Load) {
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if rest == "" || strings.Contains(rest, "/") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		a := g.agents.Agent(rest)
		if a == nil {
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}
		writeJSON(w, http.StatusOK, a)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// ============ CMDB ============

func (g *Gateway) handleCIs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		cis := g.cis.ListCIs(q.Get("tenantID"), q.Get("ciType"), q.Get("status"), limit)
		writeJSON(w, http.StatusOK, map[string]any{"cis": cis})
	case http.MethodPost:
		var ci models.CI
		if err := json.NewDecoder(r.Body).Decode(&ci); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		created := g.cis.CreateCI(&ci)
		if created == nil {
			writeError(w, http.StatusBadRequest, "CI invalid")
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (g *Gateway) handleCIDetail(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/cmdb/cis/")
	if rest == "" || strings.Contains(rest, "/") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		ci := g.cis.GetCI(rest, "")
		if ci == nil {
			writeError(w, http.StatusNotFound, "CI not found")
			return
		}
		writeJSON(w, http.StatusOK, ci)
	case http.MethodPut:
		var ci models.CI
		if err := json.NewDecoder(r.Body).Decode(&ci); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		ci.ID = rest
		updated, ok := g.cis.UpdateCI(&ci)
		if !ok {
			writeError(w, http.StatusNotFound, "CI not found")
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		if !g.cis.DeleteCI(rest, "") {
			writeError(w, http.StatusNotFound, "CI not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (g *Gateway) handleCIRelations(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/cmdb/relations/")
	if r.Method != http.MethodGet || rest == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	rels := g.cis.GetCIRelations(rest, "")
	writeJSON(w, http.StatusOK, map[string]any{"relations": rels})
}

// ============ 发现 ============

func (g *Gateway) handleDiscoveryJobs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var body struct {
			TenantID string `json:"tenantID"`
			CIDR     string `json:"cidr"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.CIDR == "" {
			writeError(w, http.StatusBadRequest, "cidr is required")
			return
		}
		// A-1 阶段说明：StartDiscovery 的 service 层是硬编码 stub（写死 3/254）。
		// HTTP 网关照实透传 store 行为——真实 Sweep 移植属 P1（见 tech-debt TD-60）。
		// ID 生成与 service.go StartDiscovery 同款（job- + uuid 前 8 位）——
		// store.CreateJob 不代填 ID，缺省会以空 ID 入库导致无法回查。
		job := &models.DiscoveryJob{
			ID:       "job-" + uuid.New().String()[:8],
			TenantID: body.TenantID,
			CIDR:     body.CIDR,
			Status:   "running",
		}
		created := g.discovery.CreateJob(job)
		if created == nil {
			writeError(w, http.StatusInternalServerError, "create job failed")
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (g *Gateway) handleDiscoveryJobStatus(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/discovery/jobs/")
	if r.Method != http.MethodGet || rest == "" || strings.Contains(rest, "/") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	job := g.discovery.GetJob(rest)
	if job == nil {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (g *Gateway) handleDiscoveredDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	q := r.URL.Query()
	devs := g.devices.ListDevices(q.Get("tenantID"), "discovered", "", 0)
	writeJSON(w, http.StatusOK, map[string]any{"devices": devs})
}

// handleProvisionAuto 处理 POST /api/v1/provision/auto：手动触发自动纳管编排（D3）。
// body: {"cidrs":["10.30.0.0/24"], "tenantID":"t1"}。
func (g *Gateway) handleProvisionAuto(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		CIDRs    []string `json:"cidrs"`
		TenantID string   `json:"tenantID"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	cidrs := body.CIDRs
	if len(cidrs) == 0 {
		writeError(w, http.StatusBadRequest, "no cidrs provided")
		return
	}
	tenant := body.TenantID
	sum, err := provision.AutoProvision(r.Context(), provision.DeviceDeps{
		UpsertDevice: func(deviceID, ip, cidr, tntID string) {
			g.devices.RegisterDevice(&models.Device{
				ID:       deviceID,
				IP:       ip,
				TenantID: tntID,
				Status:   "discovered",
			})
		},
		Provision: func(deviceID, host, tntID string) (string, string, error) {
			tok, err := g.provisionStore.IssueToken(deviceID, tntID, 15*time.Minute)
			return tok, "", err
		},
	}, provision.Config{
		AdvertiseAddr:     g.advertiseAddr,
		FallbackAdvertise: fmt.Sprintf("http://127.0.0.1:8081"),
	}, cidrs, tenant)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

// ============ 工具 ============

// handleInstallSh 处理 GET /install.sh：下发 agent 自举安装脚本（D3-d）。
// 脚本由 provision.InstallScript 生成（模板与 controlplane 同源）；
// advertise 指向控制面（agent 二进制分发仍在 controlplane，device-svc 只签发 token）。
func (g *Gateway) handleInstallSh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	advertise := g.advertiseAddr
	if advertise == "" {
		advertise = "http://127.0.0.1:8081"
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(provision.InstallScript(advertise, "device-svc")))
}

// handleProvisionRegister 处理 POST /api/v1/provision/register：token 消费注册端点（D3-d）。
// body: {"token":"<install-token>", "agentID":"agent-001", "hostname":"web-1"}。
// 语义（与 controlplane gRPC Register 的 OnboardDeviceID 翻转闭环等价）：
//   - ConsumeToken 一次性消费（MAC→存在→未消费→未过期→置 consumed）
//   - 翻转 dev-{ip} 候选设备为已纳管（Status=online）
//   - token 无效/过期/已用：401
//   - 设备不存在：404
func (g *Gateway) handleProvisionRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Token    string `json:"token"`
		AgentID  string `json:"agentID"`
		Hostname string `json:"hostname"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Token == "" {
		writeError(w, http.StatusBadRequest, "token required")
		return
	}
	deviceID, tenantID, ok := g.provisionStore.ConsumeToken(body.Token)
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid or expired or already-consumed token")
		return
	}
	dev := g.devices.Device(deviceID)
	if dev == nil {
		writeError(w, http.StatusNotFound, "device "+deviceID+" not found")
		return
	}
	// 翻转为已纳管（与 controlplane Register 翻转语义一致：State=online + Managed 标记）。
	dev.Status = "online"
	dev.TenantID = tenantID
	if body.AgentID != "" {
		dev.AgentID = body.AgentID
	}
	if body.Hostname != "" {
		dev.Name = body.Hostname
	}
	if _, updated := g.devices.UpdateDevice(dev); !updated {
		writeError(w, http.StatusInternalServerError, "update device failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "registered",
		"deviceID": deviceID,
		"tenantID": tenantID,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
