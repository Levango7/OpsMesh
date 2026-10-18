// tsdb_ops.go 时序/流数据存储运维（InfluxDB/TDengine/VictoriaMetrics）。
//
// 设计原则（延续 P0-P4"查询走 API、执行走 shell"架构）：
//   - 查询类：GET /api/v1/tsdb/{deviceID} 查询时序库服务状态。
//   - 执行类：POST /api/v1/tsdb/deploy 等，shell 模板下发到 agent 执行；
//     数据读写走各时序库的 CLI/HTTP API。
//
// 时序库定位（与监控流数据存储相关）：
//   - InfluxDB：推送型时序库，适合高频写入与长周期存储（v2 用 bucket + Flux，v1 用 database + InfluxQL）。
//   - TDengine：国产时序库，超表（STable）+ 子表模型，适合物联网/监控海量测点。
//   - VictoriaMetrics：Prometheus 兼容（PromQL/Prom remote-write），高性能低成本，可替换 Prometheus TSDB。
//
// API 端点：
//   - GET  /api/v1/tsdb/{deviceID}            — 查询时序库状态
//   - POST /api/v1/tsdb/deploy                 — 部署时序库（{deviceID, type, version}）
//   - POST /api/v1/tsdb/{type}/{action}        — 启停重启/状态
//   - POST /api/v1/tsdb/{type}/write           — 写入数据点
//   - POST /api/v1/tsdb/{type}/query           — 查询数据
//   - POST /api/v1/tsdb/{type}/database        — 数据库/桶管理（create/delete/list）
//
// 安全：type/action 走白名单；数据库名/度量名走正则校验；凭证经环境变量注入；
// 5xx 错误走 writeInternalError，不回吐 err.Error()。

package controlplane

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// 时序库类型白名单。
var allowedTSDBTypes = map[string]bool{
	"influxdb":        true,
	"tdengine":        true,
	"victoriametrics": true,
}

// 时序库操作动作白名单。
var allowedTSDBActions = map[string]bool{
	"deploy":   true,
	"start":    true,
	"stop":     true,
	"restart":  true,
	"status":   true,
	"undeploy": true,
}

// TDengine/InfluxDB 数据库名/桶名正则。
var tsdbDBRe = regexp.MustCompile(`^[a-zA-Z0-9_\-]{1,64}$`)

// 时序库部署请求体。
type tsdbDeployRequest struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`    // influxdb/tdengine/victoriametrics
	Version  string `json:"version"` // 版本
}

// tsdbOpsRequest 通用时序库操作请求体。
type tsdbOpsRequest struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`
	Action   string `json:"action"`
}

// tsdbDatabaseReq 数据库/桶管理请求体。
type tsdbDatabaseReq struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`   // influxdb/tdengine/victoriametrics
	Action   string `json:"action"` // create/delete/list
	Name     string `json:"name"`   // 数据库/桶名
}

// tsdbWriteReq 写入数据请求体。
type tsdbWriteReq struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`     // influxdb/tdengine/victoriametrics
	Database string `json:"database"` // 目标库/桶
	Measure  string `json:"measure"`  // 度量/测量名
	Line     string `json:"line"`     // InfluxDB line protocol / TDengine SQL VALUES
}

// tsdbQueryReq 查询数据请求体。
type tsdbQueryReq struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`     // influxdb/tdengine/victoriametrics
	Database string `json:"database"` // 目标库
	Query    string `json:"query"`    // InfluxQL/SQL/PromQL
}

// handleTSDB 查询指定设备上的时序库状态。
func (s *Server) handleTSDB(w http.ResponseWriter, r *http.Request, deviceID string) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "device:read"); !ok {
		return
	}
	dev := s.store.Device(deviceID)
	if dev == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}
	if actx.TenantID != "" && dev.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}
	metrics := s.store.DeviceMetrics(deviceID)
	if metrics == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "no metrics yet"})
		return
	}
	type tsInfo struct {
		Type   string `json:"type"`
		Status string `json:"status"`
	}
	var result []tsInfo
	for _, tsType := range []string{"influxdb", "tdengine", "victoriametrics"} {
		info := tsInfo{Type: tsType, Status: "not-installed"}
		if metrics.Services != nil {
			for _, svc := range metrics.Services {
				if strings.EqualFold(svc.Name, tsType) || strings.EqualFold(svc.Name, tsType+"-server") {
					info.Status = svc.Status
					break
				}
			}
		}
		if info.Status == "not-installed" {
			continue
		}
		result = append(result, info)
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"deviceID": deviceID, "tsdb": result})
}

// handleTSDBDeploy 处理 POST /api/v1/tsdb/deploy：部署时序库。
func (s *Server) handleTSDBDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	var req tsdbDeployRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !allowedTSDBTypes[req.Type] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported tsdb type"})
		return
	}
	if req.Version != "" && !middlewareVersionRe.MatchString(req.Version) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid version"})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}

	script := buildTSDBDeployScript(req)
	action := "tsdb_deploy_" + req.Type

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, action)
	if err != nil {
		writeInternalError(r.Context(), w, "tsdb.deploy."+req.Type, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: action, Target: req.Type,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s version=%s", req.DeviceID, req.Version)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildTSDBDeployScript(req tsdbDeployRequest) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("# 自动探测包管理器\n")
	b.WriteString("if command -v yum >/dev/null 2>&1; then PKG_MGR=yum; elif command -v dnf >/dev/null 2>&1; then PKG_MGR=dnf; elif command -v apt-get >/dev/null 2>&1; then PKG_MGR=apt-get; else echo '未找到包管理器' && exit 1; fi\n")
	switch req.Type {
	case "influxdb":
		b.WriteString("# InfluxDB 部署\n")
		b.WriteString("if command -v influxd >/dev/null 2>&1; then echo 'InfluxDB 已安装'; else $PKG_MGR install -y influxdb2 || echo '请配置 InfluxData 仓库' && exit 1; fi\n")
		b.WriteString("systemctl enable --now influxdb || true\n")
		b.WriteString("echo 'InfluxDB 已启动 (默认端口:8086)'\n")
	case "tdengine":
		b.WriteString("# TDengine 部署\n")
		b.WriteString("if command -v taosd >/dev/null 2>&1; then echo 'TDengine 已安装'; else echo '请下载 TDengine 安装包（taosd/taos）' && exit 1; fi\n")
		b.WriteString("systemctl enable --now taosd taosadapter || true\n")
		b.WriteString("echo 'TDengine 已启动 (taosd:6030, taosadapter:6041)'\n")
	case "victoriametrics":
		b.WriteString("# VictoriaMetrics 部署（单节点，Prometheus 兼容）\n")
		b.WriteString("if command -v victoria-metrics >/dev/null 2>&1; then echo 'VictoriaMetrics 已安装'; else echo '请下载 victoria-metrics 二进制包' && exit 1; fi\n")
		b.WriteString("mkdir -p /data/vmstorage\n")
		b.WriteString("victoria-metrics -storageDataPath=/data/vmstorage -httpListenAddr=:8428 -retentionPeriod=1 &>/var/log/vm.log &\n")
		b.WriteString("echo 'VictoriaMetrics 已启动 (端口:8428)'\n")
	}
	return b.String()
}

// handleTSDBAction 处理 POST /api/v1/tsdb/{type}/{action}：启停重启/状态。
func (s *Server) handleTSDBAction(w http.ResponseWriter, r *http.Request, tsType, action string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	if !allowedTSDBTypes[tsType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported tsdb type"})
		return
	}
	if !allowedTSDBActions[action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action"})
		return
	}
	var req tsdbOpsRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	req.Type = tsType
	req.Action = action

	script := buildTSDBActionScript(req)
	taskAction := "tsdb_" + action + "_" + tsType

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, taskAction)
	if err != nil {
		writeInternalError(r.Context(), w, "tsdb.action."+tsType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: taskAction, Target: tsType,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildTSDBActionScript(req tsdbOpsRequest) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "influxdb":
		b.WriteString("# InfluxDB 启停\n")
		switch req.Action {
		case "start":
			b.WriteString("systemctl start influxdb || true\n")
		case "stop":
			b.WriteString("systemctl stop influxdb || true\n")
		case "restart":
			b.WriteString("systemctl restart influxdb || true\n")
		case "status":
			b.WriteString("systemctl is-active influxdb || echo 'not running'\n")
		}
	case "tdengine":
		b.WriteString("# TDengine 启停\n")
		switch req.Action {
		case "start":
			b.WriteString("systemctl start taosd taosadapter || true\n")
		case "stop":
			b.WriteString("systemctl stop taosd taosadapter || true\n")
		case "restart":
			b.WriteString("systemctl restart taosd taosadapter || true\n")
		case "status":
			b.WriteString("systemctl is-active taosd || echo 'not running'\n")
		}
	case "victoriametrics":
		b.WriteString("# VictoriaMetrics 启停\n")
		switch req.Action {
		case "start":
			b.WriteString("systemctl start victoria-metrics || victoria-metrics -storageDataPath=/data/vmstorage -httpListenAddr=:8428 -retentionPeriod=1 &>/var/log/vm.log & || true\n")
		case "stop":
			b.WriteString("systemctl stop victoria-metrics || pkill -f victoria-metrics || true\n")
		case "restart":
			b.WriteString("systemctl restart victoria-metrics || true\n")
		case "status":
			b.WriteString("systemctl is-active victoria-metrics || (pgrep -f victoria-metrics >/dev/null && echo running || echo 'not running')\n")
		}
	}
	fmt.Fprintf(&b, "echo '时序库 %s %s 操作完成\n", req.Type, req.Action)
	return b.String()
}

// handleTSDBDatabase 处理 POST /api/v1/tsdb/{type}/database：数据库/桶管理。
func (s *Server) handleTSDBDatabase(w http.ResponseWriter, r *http.Request, tsType string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	if !allowedTSDBTypes[tsType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported tsdb type"})
		return
	}
	var req tsdbDatabaseReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	allowed := map[string]bool{"create": true, "delete": true, "list": true}
	if !allowed[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (create/delete/list)"})
		return
	}
	if req.Action != "list" && !tsdbDBRe.MatchString(req.Name) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid database name"})
		return
	}
	req.Type = tsType

	script := buildTSDBDatabaseScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "tsdb_database_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "tsdb.database."+tsType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "tsdb_database_" + req.Action, Target: req.Name,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s type=%s", req.DeviceID, tsType)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildTSDBDatabaseScript(req tsdbDatabaseReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "influxdb":
		b.WriteString("# InfluxDB 桶/数据库管理（influx CLI，凭证从环境变量 INFLUX_TOKEN / INFLUX_ORG 读取）\n")
		switch req.Action {
		case "create":
			fmt.Fprintf(&b, "influx bucket create --name %s --org \"$INFLUX_ORG\" --retention 30d || echo '创建失败: %s'\n", req.Name, req.Name)
		case "delete":
			fmt.Fprintf(&b, "influx bucket delete --name %s --org \"$INFLUX_ORG\" || echo '删除失败: %s'\n", req.Name, req.Name)
		case "list":
			b.WriteString("influx bucket list\n")
		}
	case "tdengine":
		b.WriteString("# TDengine 数据库管理（taos CLI）\n")
		switch req.Action {
		case "create":
			fmt.Fprintf(&b, "taos -s \"CREATE DATABASE IF NOT EXISTS %s\" || echo '创建失败: %s'\n", req.Name, req.Name)
		case "delete":
			fmt.Fprintf(&b, "taos -s \"DROP DATABASE IF EXISTS %s\" || echo '删除失败: %s'\n", req.Name, req.Name)
		case "list":
			b.WriteString("taos -s \"SHOW DATABASES\"\n")
		}
	case "victoriametrics":
		b.WriteString("# VictoriaMetrics 无数据库概念（单一存储），仅列出可用 API\n")
		switch req.Action {
		case "list":
			b.WriteString("curl -s 'http://localhost:8428/api/v1/label/__name__/values' | head -c 2000\n")
		default:
			b.WriteString("echo 'VictoriaMetrics 使用单一存储，无需创建/删除数据库'\n")
		}
	}
	fmt.Fprintf(&b, "echo '时序库 %s 数据库 %s 操作完成\n", req.Type, req.Action)
	return b.String()
}

// handleTSDBWrite 处理 POST /api/v1/tsdb/{type}/write：写入数据点。
func (s *Server) handleTSDBWrite(w http.ResponseWriter, r *http.Request, tsType string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	if !allowedTSDBTypes[tsType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported tsdb type"})
		return
	}
	var req tsdbWriteReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if req.Database != "" && !tsdbDBRe.MatchString(req.Database) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid database name"})
		return
	}
	if req.Line == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "line is required"})
		return
	}
	req.Type = tsType

	script := buildTSDBWriteScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "tsdb_write")
	if err != nil {
		writeInternalError(r.Context(), w, "tsdb.write."+tsType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "tsdb_write", Target: req.Database,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildTSDBWriteScript(req tsdbWriteReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	payload := shellQuote(req.Line)
	switch req.Type {
	case "influxdb":
		b.WriteString("# InfluxDB 写入（line protocol，v1 /write 或 v2 /api/v2/write）\n")
		fmt.Fprintf(&b, "curl -s -X POST 'http://localhost:8086/api/v2/write?org=%s&bucket=%s&precision=s' -H \"Authorization: Token $INFLUX_TOKEN\" --data-binary %s\n", "${INFLUX_ORG:-default}", req.Database, payload)
	case "tdengine":
		b.WriteString("# TDengine 写入（SQL INSERT）\n")
		fmt.Fprintf(&b, "taos -d %s -s %s\n", req.Database, shellQuote("INSERT INTO "+req.Line))
	case "victoriametrics":
		b.WriteString("# VictoriaMetrics 写入（Prometheus remote-write 文本格式）\n")
		fmt.Fprintf(&b, "curl -s -X POST 'http://localhost:8428/api/v1/import/prometheus' --data-binary %s\n", payload)
	}
	b.WriteString("echo '时序数据写入完成'\n")
	return b.String()
}

// handleTSDBQuery 处理 POST /api/v1/tsdb/{type}/query：查询数据。
func (s *Server) handleTSDBQuery(w http.ResponseWriter, r *http.Request, tsType string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "device:read"); !ok {
		return
	}
	if !allowedTSDBTypes[tsType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported tsdb type"})
		return
	}
	var req tsdbQueryReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if req.Database != "" && !tsdbDBRe.MatchString(req.Database) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid database name"})
		return
	}
	if req.Query == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "query is required"})
		return
	}
	req.Type = tsType

	script := buildTSDBQueryScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "tsdb_query")
	if err != nil {
		writeInternalError(r.Context(), w, "tsdb.query."+tsType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "tsdb_query", Target: req.Database,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildTSDBQueryScript(req tsdbQueryReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "influxdb":
		b.WriteString("# InfluxDB 查询（InfluxQL via v1 query API）\n")
		fmt.Fprintf(&b, "curl -s -G 'http://localhost:8086/query?db=%s' --data-urlencode %s\n", req.Database, shellQuote("q="+req.Query))
	case "tdengine":
		b.WriteString("# TDengine 查询（SQL）\n")
		fmt.Fprintf(&b, "taos -d %s -s %s\n", req.Database, shellQuote(req.Query))
	case "victoriametrics":
		b.WriteString("# VictoriaMetrics 查询（PromQL，/api/v1/query）\n")
		fmt.Fprintf(&b, "curl -s -G 'http://localhost:8428/api/v1/query' --data-urlencode %s\n", shellQuote("query="+req.Query))
	}
	b.WriteString("echo '时序数据查询完成'\n")
	return b.String()
}

// ============================================================================
// 路由注册
// ============================================================================

// registerTSDBRoutes 注册时序库运维路由。
func (s *Server) registerTSDBRoutes(mux *http.ServeMux) {
	// 固定路径优先：/api/v1/tsdb/{type}/{database|write|query}
	mux.HandleFunc("/api/v1/tsdb/influxdb/database", s.handleTSDBDatabaseInfluxDB)
	mux.HandleFunc("/api/v1/tsdb/tdengine/database", s.handleTSDBDatabaseTDengine)
	mux.HandleFunc("/api/v1/tsdb/victoriametrics/database", s.handleTSDBDatabaseVM)
	mux.HandleFunc("/api/v1/tsdb/influxdb/write", s.handleTSDBWriteInfluxDB)
	mux.HandleFunc("/api/v1/tsdb/tdengine/write", s.handleTSDBWriteTDengine)
	mux.HandleFunc("/api/v1/tsdb/victoriametrics/write", s.handleTSDBWriteVM)
	mux.HandleFunc("/api/v1/tsdb/influxdb/query", s.handleTSDBQueryInfluxDB)
	mux.HandleFunc("/api/v1/tsdb/tdengine/query", s.handleTSDBQueryTDengine)
	mux.HandleFunc("/api/v1/tsdb/victoriametrics/query", s.handleTSDBQueryVM)
	// 泛路由：GET /api/v1/tsdb/{deviceID} / POST /api/v1/tsdb/{type}/{action} / POST /api/v1/tsdb/deploy
	mux.HandleFunc("/api/v1/tsdb/deploy", s.handleTSDBDeploy)
	mux.HandleFunc("/api/v1/tsdb/", s.handleTSDBRouting)
}

func (s *Server) handleTSDBDatabaseInfluxDB(w http.ResponseWriter, r *http.Request) {
	s.handleTSDBDatabase(w, r, "influxdb")
}
func (s *Server) handleTSDBDatabaseTDengine(w http.ResponseWriter, r *http.Request) {
	s.handleTSDBDatabase(w, r, "tdengine")
}
func (s *Server) handleTSDBDatabaseVM(w http.ResponseWriter, r *http.Request) {
	s.handleTSDBDatabase(w, r, "victoriametrics")
}
func (s *Server) handleTSDBWriteInfluxDB(w http.ResponseWriter, r *http.Request) {
	s.handleTSDBWrite(w, r, "influxdb")
}
func (s *Server) handleTSDBWriteTDengine(w http.ResponseWriter, r *http.Request) {
	s.handleTSDBWrite(w, r, "tdengine")
}
func (s *Server) handleTSDBWriteVM(w http.ResponseWriter, r *http.Request) {
	s.handleTSDBWrite(w, r, "victoriametrics")
}
func (s *Server) handleTSDBQueryInfluxDB(w http.ResponseWriter, r *http.Request) {
	s.handleTSDBQuery(w, r, "influxdb")
}
func (s *Server) handleTSDBQueryTDengine(w http.ResponseWriter, r *http.Request) {
	s.handleTSDBQuery(w, r, "tdengine")
}
func (s *Server) handleTSDBQueryVM(w http.ResponseWriter, r *http.Request) {
	s.handleTSDBQuery(w, r, "victoriametrics")
}

func (s *Server) handleTSDBRouting(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/tsdb/")
	segments := strings.Split(path, "/")
	switch len(segments) {
	case 1:
		// /api/v1/tsdb/{deviceID}
		if segments[0] != "" {
			s.handleTSDB(w, r, segments[0])
		} else {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}
	case 2:
		// /api/v1/tsdb/{type}/{action}
		if segments[0] != "" && segments[1] != "" {
			s.handleTSDBAction(w, r, segments[0], segments[1])
		} else {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}
