// olap_ops.go OLAP 分析引擎运维（ClickHouse/Doris/Trino）。
//
// 设计原则（延续 P0-P5"查询走 API、执行走 shell"架构）：
//   - 查询类：GET /api/v1/olap/{deviceID} 从 DeviceMetrics.Services 读取 OLAP 服务状态。
//   - 执行类：POST /api/v1/olap/deploy 等，shell 模板下发到 agent 执行。
//
// OLAP 引擎定位：
//   - ClickHouse：列式 OLAP，单机/集群（MergeTree 家族），适合日志分析/行为分析/大宽表聚合。
//   - Doris：MPP 架构 OLAP（FE+BE），兼容 MySQL 协议，适合实时数仓/报表。
//   - Trino：分布式 SQL 查询引擎（无存储，联邦查询 Hive/Iceberg/MySQL/ES 等）。
//
// API 端点：
//   - GET  /api/v1/olap/{deviceID}          — 查询 OLAP 引擎状态
//   - POST /api/v1/olap/deploy               — 部署 OLAP（{deviceID, type, version, mode}）
//   - POST /api/v1/olap/{type}/{action}      — 启停重启/状态
//   - POST /api/v1/olap/{type}/query         — SQL 查询
//   - POST /api/v1/olap/{type}/database      — 库管理（create/delete/list）
//   - POST /api/v1/olap/{type}/table         — 表管理（create/delete/list）
//
// 安全：type/action 走白名单；库表名走正则校验；查询语句拒绝 NUL/换行；
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

// OLAP 类型白名单。
var allowedOLAPTypes = map[string]bool{
	"clickhouse": true,
	"doris":      true,
	"trino":      true,
}

// OLAP 部署模式白名单。
var allowedOLAPModes = map[string]map[string]bool{
	"clickhouse": {
		"single":  true,
		"cluster": true,
	},
	"doris": {
		"single":  true,
		"cluster": true,
	},
	"trino": {
		"single":  true,
		"cluster": true,
	},
}

// OLAP 操作动作白名单。
var allowedOLAPActions = map[string]bool{
	"deploy":   true,
	"start":    true,
	"stop":     true,
	"restart":  true,
	"status":   true,
	"undeploy": true,
}

// OLAP 资源动作白名单。
var allowedOLAPResourceActions = map[string]bool{
	"create": true,
	"delete": true,
	"list":   true,
}

// OLAP 库表名正则。
var olapNameRe = regexp.MustCompile(`^[a-zA-Z0-9_]{1,64}$`)

// OLAP 部署请求体。
type olapDeployRequest struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`    // clickhouse/doris/trino
	Version  string `json:"version"` // 版本
	Mode     string `json:"mode"`    // single/cluster
}

// olapOpsRequest 通用 OLAP 操作请求体。
type olapOpsRequest struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`
	Action   string `json:"action"`
}

// olapDatabaseReq 库管理请求体。
type olapDatabaseReq struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`   // clickhouse/doris/trino
	Action   string `json:"action"` // create/delete/list
	Name     string `json:"name"`   // 库名
}

// olapTableReq 表管理请求体。
type olapTableReq struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`     // clickhouse/doris/trino
	Action   string `json:"action"`   // create/delete/list
	Database string `json:"database"` // 库名
	Table    string `json:"table"`    // 表名
	Schema   string `json:"schema"`   // 建表语句（create 用）
}

// olapQueryReq SQL 查询请求体。
type olapQueryReq struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`     // clickhouse/doris/trino
	Database string `json:"database"` // 库名
	Query    string `json:"query"`    // SQL
}

// handleOLAP 查询指定设备上的 OLAP 引擎状态。
func (s *Server) handleOLAP(w http.ResponseWriter, r *http.Request, deviceID string) {
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
	type olapInfo struct {
		Type   string `json:"type"`
		Status string `json:"status"`
	}
	var result []olapInfo
	for _, olapType := range []string{"clickhouse", "doris", "trino"} {
		info := olapInfo{Type: olapType, Status: "not-installed"}
		if metrics.Services != nil {
			for _, svc := range metrics.Services {
				// ClickHouse: clickhouse-server；Doris: doris-fe/doris-be；Trino: trino
				if strings.EqualFold(svc.Name, olapType) ||
					strings.EqualFold(svc.Name, olapType+"-server") ||
					strings.EqualFold(svc.Name, olapType+"-fe") {
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
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"deviceID": deviceID, "olap": result})
}

// handleOLAPDeploy 处理 POST /api/v1/olap/deploy：部署 OLAP 引擎。
func (s *Server) handleOLAPDeploy(w http.ResponseWriter, r *http.Request) {
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
	var req olapDeployRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !allowedOLAPTypes[req.Type] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported olap type (clickhouse/doris/trino)"})
		return
	}
	mode := req.Mode
	if mode == "" {
		mode = "single"
	}
	if !allowedOLAPModes[req.Type][mode] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported mode for " + req.Type})
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
	req.Mode = mode

	script := buildOLAPDeployScript(req)
	action := "olap_deploy_" + req.Type + "_" + mode

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, action)
	if err != nil {
		writeInternalError(r.Context(), w, "olap.deploy."+req.Type, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: action, Target: req.Type + ":" + mode,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s version=%s", req.DeviceID, req.Version)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildOLAPDeployScript(req olapDeployRequest) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("# 自动探测包管理器\n")
	b.WriteString("if command -v yum >/dev/null 2>&1; then PKG_MGR=yum; elif command -v dnf >/dev/null 2>&1; then PKG_MGR=dnf; elif command -v apt-get >/dev/null 2>&1; then PKG_MGR=apt-get; else echo '未找到包管理器' && exit 1; fi\n")
	switch req.Type {
	case "clickhouse":
		b.WriteString("# ClickHouse 部署\n")
		b.WriteString("if command -v clickhouse-server >/dev/null 2>&1; then echo 'ClickHouse 已安装'; else\n")
		b.WriteString("  curl -fsSL https://packages.clickhouse.com/rpm/lts/repodata/repomd.xml.key 2>/dev/null | gpg --dearmor -o /usr/share/keyrings/clickhouse-keyring.gpg 2>/dev/null || true\n")
		b.WriteString("  $PKG_MGR install -y clickhouse-server clickhouse-client || echo '请配置 ClickHouse 仓库' \n")
		b.WriteString("fi\n")
		if req.Mode == "cluster" {
			b.WriteString("# 集群：需配置 remote_servers 分片与副本（config.xml）\n")
			b.WriteString("echo 'ClickHouse 集群：需配置 remote_servers 分片副本'\n")
		} else {
			b.WriteString("systemctl enable --now clickhouse-server || true\n")
			b.WriteString("echo 'ClickHouse 单节点已启动 (HTTP:8123, TCP:9000)'\n")
		}
	case "doris":
		b.WriteString("# Doris 部署（FE + BE）\n")
		b.WriteString("if [ -d /opt/doris ] || command -v doris-fe >/dev/null 2>&1; then echo 'Doris 已安装'; else echo '请下载 Doris 发行包（FE/BE）' && exit 1; fi\n")
		b.WriteString("/opt/doris/fe/bin/start_fe.sh --daemon || true\n")
		b.WriteString("/opt/doris/be/bin/start_be.sh --daemon || true\n")
		b.WriteString("echo 'Doris FE+BE 已启动 (FE HTTP:8030, MySQL:9030, BE:8040)'\n")
	case "trino":
		b.WriteString("# Trino 部署（分布式 SQL 查询引擎）\n")
		b.WriteString("if command -v trino >/dev/null 2>&1 || [ -d /opt/trino ]; then echo 'Trino 已安装'; else echo '请下载 Trino 发行包' && exit 1; fi\n")
		b.WriteString("cd /opt/trino && ./bin/launcher start || true\n")
		b.WriteString("echo 'Trino 已启动 (端口:8080)'\n")
	}
	return b.String()
}

// handleOLAPAction 处理 POST /api/v1/olap/{type}/{action}：启停重启/状态。
func (s *Server) handleOLAPAction(w http.ResponseWriter, r *http.Request, olapType, action string) {
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
	if !allowedOLAPTypes[olapType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported olap type"})
		return
	}
	if !allowedOLAPActions[action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action"})
		return
	}
	var req olapOpsRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	req.Type = olapType
	req.Action = action

	script := buildOLAPActionScript(req)
	taskAction := "olap_" + action + "_" + olapType

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, taskAction)
	if err != nil {
		writeInternalError(r.Context(), w, "olap.action."+olapType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: taskAction, Target: olapType,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildOLAPActionScript(req olapOpsRequest) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "clickhouse":
		b.WriteString("# ClickHouse 启停\n")
		switch req.Action {
		case "start":
			b.WriteString("systemctl start clickhouse-server || true\n")
		case "stop":
			b.WriteString("systemctl stop clickhouse-server || true\n")
		case "restart":
			b.WriteString("systemctl restart clickhouse-server || true\n")
		case "status":
			b.WriteString("clickhouse-client --query 'SELECT version()' 2>/dev/null || systemctl is-active clickhouse-server || echo 'not running'\n")
		}
	case "doris":
		b.WriteString("# Doris 启停\n")
		switch req.Action {
		case "start":
			b.WriteString("/opt/doris/fe/bin/start_fe.sh --daemon || true\n/opt/doris/be/bin/start_be.sh --daemon || true\n")
		case "stop":
			b.WriteString("/opt/doris/fe/bin/stop_fe.sh || true\n/opt/doris/be/bin/stop_be.sh || true\n")
		case "restart":
			b.WriteString("/opt/doris/fe/bin/stop_fe.sh || true\n/opt/doris/be/bin/stop_be.sh || true\nsleep 2\n/opt/doris/fe/bin/start_fe.sh --daemon || true\n/opt/doris/be/bin/start_be.sh --daemon || true\n")
		case "status":
			b.WriteString("pgrep -f doris-fe >/dev/null && echo 'FE running' || echo 'FE not running'\npgrep -f doris-be >/dev/null && echo 'BE running' || echo 'BE not running'\n")
		}
	case "trino":
		b.WriteString("# Trino 启停\n")
		switch req.Action {
		case "start":
			b.WriteString("cd /opt/trino && ./bin/launcher start || true\n")
		case "stop":
			b.WriteString("cd /opt/trino && ./bin/launcher stop || true\n")
		case "restart":
			b.WriteString("cd /opt/trino && ./bin/launcher restart || true\n")
		case "status":
			b.WriteString("cd /opt/trino && ./bin/launcher status || echo 'not running'\n")
		}
	}
	fmt.Fprintf(&b, "echo 'OLAP %s %s 操作完成\n", req.Type, req.Action)
	return b.String()
}

// handleOLAPDatabase 处理 POST /api/v1/olap/{type}/database：库管理。
func (s *Server) handleOLAPDatabase(w http.ResponseWriter, r *http.Request, olapType string) {
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
	if !allowedOLAPTypes[olapType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported olap type"})
		return
	}
	var req olapDatabaseReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !allowedOLAPResourceActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (create/delete/list)"})
		return
	}
	if req.Action != "list" && !olapNameRe.MatchString(req.Name) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid database name"})
		return
	}
	req.Type = olapType

	script := buildOLAPDatabaseScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "olap_database_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "olap.database."+olapType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "olap_database_" + req.Action, Target: req.Name,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s type=%s", req.DeviceID, olapType)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildOLAPDatabaseScript(req olapDatabaseReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "clickhouse":
		b.WriteString("# ClickHouse 库管理（clickhouse-client）\n")
		switch req.Action {
		case "create":
			fmt.Fprintf(&b, "clickhouse-client --query 'CREATE DATABASE IF NOT EXISTS %s' || echo '创建失败'\n", req.Name)
		case "delete":
			fmt.Fprintf(&b, "clickhouse-client --query 'DROP DATABASE IF EXISTS %s' || echo '删除失败'\n", req.Name)
		case "list":
			b.WriteString("clickhouse-client --query 'SHOW DATABASES'\n")
		}
	case "doris":
		b.WriteString("# Doris 库管理（MySQL 协议 9030）\n")
		switch req.Action {
		case "create":
			fmt.Fprintf(&b, "mysql -h 127.0.0.1 -P 9030 -u root -e 'CREATE DATABASE IF NOT EXISTS %s' || echo '创建失败'\n", req.Name)
		case "delete":
			fmt.Fprintf(&b, "mysql -h 127.0.0.1 -P 9030 -u root -e 'DROP DATABASE IF EXISTS %s' || echo '删除失败'\n", req.Name)
		case "list":
			b.WriteString("mysql -h 127.0.0.1 -P 9030 -u root -e 'SHOW DATABASES'\n")
		}
	case "trino":
		b.WriteString("# Trino 无库概念（catalog 映射外部存储），列出 catalog\n")
		switch req.Action {
		case "list":
			b.WriteString("cd /opt/trino && ./bin/trino --execute 'SHOW CATALOGS' || echo 'not running'\n")
		default:
			b.WriteString("echo 'Trino 使用 catalog 概念，无需创建/删除数据库'\n")
		}
	}
	fmt.Fprintf(&b, "echo 'OLAP %s 数据库 %s 操作完成\n", req.Type, req.Action)
	return b.String()
}

// handleOLAPTable 处理 POST /api/v1/olap/{type}/table：表管理。
func (s *Server) handleOLAPTable(w http.ResponseWriter, r *http.Request, olapType string) {
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
	if !allowedOLAPTypes[olapType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported olap type"})
		return
	}
	var req olapTableReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !allowedOLAPResourceActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (create/delete/list)"})
		return
	}
	if !olapNameRe.MatchString(req.Database) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid database name"})
		return
	}
	if req.Action != "list" && !olapNameRe.MatchString(req.Table) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid table name"})
		return
	}
	req.Type = olapType

	script := buildOLAPTableScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "olap_table_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "olap.table."+olapType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "olap_table_" + req.Action, Target: req.Database + "." + req.Table,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildOLAPTableScript(req olapTableReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "clickhouse":
		b.WriteString("# ClickHouse 表管理\n")
		switch req.Action {
		case "create":
			schema := req.Schema
			if schema == "" {
				schema = fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s.%s (id UInt64, ts DateTime, data String) ENGINE = MergeTree() ORDER BY id", req.Database, req.Table)
			}
			if strings.ContainsAny(schema, "\x00\n") {
				b.WriteString("echo '建表语句不能包含换行' && exit 1\n")
			} else {
				fmt.Fprintf(&b, "clickhouse-client --query %s || echo '创建失败'\n", shellQuote(schema))
			}
		case "delete":
			fmt.Fprintf(&b, "clickhouse-client --query 'DROP TABLE IF EXISTS %s.%s' || echo '删除失败'\n", req.Database, req.Table)
		case "list":
			fmt.Fprintf(&b, "clickhouse-client --query 'SHOW TABLES FROM %s'\n", req.Database)
		}
	case "doris":
		b.WriteString("# Doris 表管理\n")
		switch req.Action {
		case "create":
			schema := req.Schema
			if schema == "" {
				b.WriteString("echo 'Doris 建表需提供 schema（含分桶 DISTRIBUTED BY）' && exit 1\n")
			} else if strings.ContainsAny(schema, "\x00\n") {
				b.WriteString("echo '建表语句不能包含换行' && exit 1\n")
			} else {
				fmt.Fprintf(&b, "mysql -h 127.0.0.1 -P 9030 -u root %s -e %s || echo '创建失败'\n", req.Database, shellQuote(schema))
			}
		case "delete":
			fmt.Fprintf(&b, "mysql -h 127.0.0.1 -P 9030 -u root %s -e 'DROP TABLE IF EXISTS %s' || echo '删除失败'\n", req.Database, req.Table)
		case "list":
			fmt.Fprintf(&b, "mysql -h 127.0.0.1 -P 9030 -u root %s -e 'SHOW TABLES'\n", req.Database)
		}
	case "trino":
		b.WriteString("# Trino 表管理（SQL 跨越 catalog）\n")
		switch req.Action {
		case "list":
			fmt.Fprintf(&b, "cd /opt/trino && ./bin/trino --execute 'SHOW TABLES FROM %s' || echo 'not running'\n", req.Database)
		case "delete":
			fmt.Fprintf(&b, "cd /opt/trino && ./bin/trino --execute 'DROP TABLE IF EXISTS %s.%s' || echo '删除失败'\n", req.Database, req.Table)
		default:
			b.WriteString("echo 'Trino 建表取决于底层 connector（Hive/Iceberg 等）'\n")
		}
	}
	fmt.Fprintf(&b, "echo 'OLAP %s 表 %s 操作完成\n", req.Type, req.Action)
	return b.String()
}

// handleOLAPQuery 处理 POST /api/v1/olap/{type}/query：SQL 查询。
func (s *Server) handleOLAPQuery(w http.ResponseWriter, r *http.Request, olapType string) {
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
	if !allowedOLAPTypes[olapType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported olap type"})
		return
	}
	var req olapQueryReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if req.Query == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "query is required"})
		return
	}
	if strings.ContainsAny(req.Query, "\x00\n\r") {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "query must not contain NUL or newline"})
		return
	}
	if req.Database != "" && !olapNameRe.MatchString(req.Database) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid database name"})
		return
	}
	req.Type = olapType

	script := buildOLAPQueryScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "olap_query")
	if err != nil {
		writeInternalError(r.Context(), w, "olap.query."+olapType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "olap_query", Target: req.Database,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildOLAPQueryScript(req olapQueryReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	q := shellQuote(req.Query)
	switch req.Type {
	case "clickhouse":
		if req.Database != "" {
			fmt.Fprintf(&b, "clickhouse-client --database %s --query %s\n", req.Database, q)
		} else {
			fmt.Fprintf(&b, "clickhouse-client --query %s\n", q)
		}
	case "doris":
		if req.Database != "" {
			fmt.Fprintf(&b, "mysql -h 127.0.0.1 -P 9030 -u root -D %s -e %s\n", req.Database, q)
		} else {
			fmt.Fprintf(&b, "mysql -h 127.0.0.1 -P 9030 -u root -e %s\n", q)
		}
	case "trino":
		if req.Database != "" {
			fmt.Fprintf(&b, "cd /opt/trino && ./bin/trino --catalog %s --execute %s\n", req.Database, q)
		} else {
			fmt.Fprintf(&b, "cd /opt/trino && ./bin/trino --execute %s\n", q)
		}
	}
	b.WriteString("echo 'OLAP 查询完成'\n")
	return b.String()
}

// ============================================================================
// 路由注册
// ============================================================================

// registerOLAPRoutes 注册 OLAP 运维路由。
func (s *Server) registerOLAPRoutes(mux *http.ServeMux) {
	// 固定路径优先：/api/v1/olap/{type}/{database|table|query}
	for _, t := range []string{"clickhouse", "doris", "trino"} {
		tt := t
		mux.HandleFunc("/api/v1/olap/"+tt+"/database", func(w http.ResponseWriter, r *http.Request) { s.handleOLAPDatabase(w, r, tt) })
		mux.HandleFunc("/api/v1/olap/"+tt+"/table", func(w http.ResponseWriter, r *http.Request) { s.handleOLAPTable(w, r, tt) })
		mux.HandleFunc("/api/v1/olap/"+tt+"/query", func(w http.ResponseWriter, r *http.Request) { s.handleOLAPQuery(w, r, tt) })
	}
	// 泛路由
	mux.HandleFunc("/api/v1/olap/deploy", s.handleOLAPDeploy)
	mux.HandleFunc("/api/v1/olap/", s.handleOLAPRouting)
}

func (s *Server) handleOLAPRouting(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/olap/")
	segments := strings.Split(path, "/")
	switch len(segments) {
	case 1:
		if segments[0] != "" {
			s.handleOLAP(w, r, segments[0])
		} else {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}
	case 2:
		if segments[0] != "" && segments[1] != "" {
			s.handleOLAPAction(w, r, segments[0], segments[1])
		} else {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}
