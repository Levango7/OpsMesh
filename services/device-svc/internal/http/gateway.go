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
	"net/http"
	"strconv"
	"strings"

	"github.com/Levango7/OpsMesh/services/device-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/device-svc/internal/store"
)

// Gateway 持有 HTTP handler 依赖的各 store 接口。
type Gateway struct {
	devices   store.DeviceStore
	agents    store.AgentStore
	cis       store.CiStore
	discovery store.DiscoveryStore
}

// NewGateway 构造 Gateway 实例。
func NewGateway(ds store.DeviceStore, as store.AgentStore, cs store.CiStore, disc store.DiscoveryStore) *Gateway {
	return &Gateway{devices: ds, agents: as, cis: cs, discovery: disc}
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
		job := &models.DiscoveryJob{
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

// ============ 工具 ============

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
