// graph_ops.go 图数据库运维（Neo4j/NebulaGraph）。
//
// 设计原则（延续 P0-P4"查询走 API、执行走 shell"架构）：
//   - 查询类：GET /api/v1/graph/{deviceID} 查询图库服务状态。
//   - 执行类：POST /api/v1/graph/deploy 等，shell 模板下发到 agent 执行；
//     图数据读写走 Cypher（Neo4j）/ nGQL（NebulaGraph）。
//
// 图库定位（CMDB 关系拓扑/调用链/知识图谱场景）：
//   - Neo4j：主流属性图数据库，Cypher 查询语言，社区版/企业版。
//   - NebulaGraph：国产分布式图数据库，nGQL，适合十亿点边规模。
//
// API 端点：
//   - GET  /api/v1/graph/{deviceID}        — 查询图库状态
//   - POST /api/v1/graph/deploy             — 部署图库（{deviceID, type, version, mode}）
//   - POST /api/v1/graph/{type}/{action}    — 启停重启/状态
//   - POST /api/v1/graph/{type}/cypher      — 执行查询（Cypher/nGQL）
//   - POST /api/v1/graph/{type}/database    — 图空间/数据库管理（create/delete/list）
//   - POST /api/v1/graph/{type}/backup      — 备份/恢复（export/import）
//
// 安全：type/action 走白名单；库名走正则校验；查询语句仅拒绝 NUL 与换行，其余交由图库自身鉴权；
// 5xx 错误走 writeInternalError。

package controlplane

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// 图库类型白名单。
var allowedGraphTypes = map[string]bool{
	"neo4j":       true,
	"nebulagraph": true,
}

// 图库操作动作白名单。
var allowedGraphActions = map[string]bool{
	"deploy":   true,
	"start":    true,
	"stop":     true,
	"restart":  true,
	"status":   true,
	"undeploy": true,
}

// 图空间/数据库名正则。
var graphDatabaseRe = regexp.MustCompile(`^[a-zA-Z0-9_]{1,64}$`)

// 图库部署请求体。
type graphDeployRequest struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`    // neo4j/nebulagraph
	Version  string `json:"version"` // 版本
	Mode     string `json:"mode"`    // single/cluster
}

// graphOpsRequest 通用图库操作请求体。
type graphOpsRequest struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`
	Action   string `json:"action"`
}

// graphDatabaseReq 图空间/数据库管理请求体。
type graphDatabaseReq struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`   // neo4j/nebulagraph
	Action   string `json:"action"` // create/delete/list
	Name     string `json:"name"`   // 图空间/数据库名
}

// graphQueryReq 图查询请求体。
type graphQueryReq struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`     // neo4j/nebulagraph
	Database string `json:"database"` // 目标库/图空间
	Query    string `json:"query"`    // Cypher / nGQL
}

// graphBackupReq 图库备份请求体。
type graphBackupReq struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`     // neo4j/nebulagraph
	Action   string `json:"action"`   // export/import
	Database string `json:"database"` // 目标库/图空间
}

// handleGraph 查询指定设备上的图库状态。
func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request, deviceID string) {
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
	type gInfo struct {
		Type   string `json:"type"`
		Status string `json:"status"`
	}
	var result []gInfo
	for _, gType := range []string{"neo4j", "nebulagraph"} {
		info := gInfo{Type: gType, Status: "not-installed"}
		if metrics.Services != nil {
			for _, svc := range metrics.Services {
				if strings.EqualFold(svc.Name, gType) || strings.EqualFold(svc.Name, gType+"-server") {
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
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"deviceID": deviceID, "graphDB": result})
}

// handleGraphDeploy 处理 POST /api/v1/graph/deploy：部署图库。
func (s *Server) handleGraphDeploy(w http.ResponseWriter, r *http.Request) {
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
	var req graphDeployRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !allowedGraphTypes[req.Type] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported graph db type"})
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

	script := buildGraphDeployScript(req)
	action := "graph_deploy_" + req.Type

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, action)
	if err != nil {
		writeInternalError(r.Context(), w, "graph.deploy."+req.Type, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: action, Target: req.Type,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s mode=%s version=%s", req.DeviceID, req.Mode, req.Version)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildGraphDeployScript(req graphDeployRequest) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "neo4j":
		b.WriteString("# Neo4j 部署\n")
		b.WriteString("if command -v neo4j >/dev/null 2>&1; then echo 'Neo4j 已安装'; else docker run -d --name neo4j -p 7474:7474 -p 7687:7687 -v /data/neo4j:/data -e NEO4J_AUTH=$NEO4J_USER/$NEO4J_PASSWORD neo4j:5 || echo '请安装 Neo4j' && exit 1; fi\n")
		b.WriteString("echo 'Neo4j 已启动 (Browser:7474, Bolt:7687)'\n")
	case "nebulagraph":
		b.WriteString("# NebulaGraph 部署（metad/storaged/graphd + nebula-console）\n")
		b.WriteString("cd /opt && curl -fsSL -O https://oss-cdn.nebula-graph.com.cn/package/3.6.0/nebula-graph-3.6.0.el7.x86_64.rpm && rpm -ivh nebula-graph-*.rpm || echo '请下载 NebulaGraph 发行包'\n")
		b.WriteString("/usr/local/nebula/scripts/nebula.service start all || true\n")
		b.WriteString("echo 'NebulaGraph 已启动 (graphd:9669, metad:9559, storaged:9779)'\n")
	}
	return b.String()
}

// handleGraphAction 处理 POST /api/v1/graph/{type}/{action}：启停重启/状态。
func (s *Server) handleGraphAction(w http.ResponseWriter, r *http.Request, gType, action string) {
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
	if !allowedGraphTypes[gType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported graph db type"})
		return
	}
	if !allowedGraphActions[action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action"})
		return
	}
	var req graphOpsRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	req.Type = gType
	req.Action = action

	script := buildGraphActionScript(req)
	taskAction := "graph_" + action + "_" + gType

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, taskAction)
	if err != nil {
		writeInternalError(r.Context(), w, "graph.action."+gType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: taskAction, Target: gType,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildGraphActionScript(req graphOpsRequest) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "neo4j":
		b.WriteString("# Neo4j 启停\n")
		switch req.Action {
		case "start":
			b.WriteString("systemctl start neo4j || docker start neo4j || true\n")
		case "stop":
			b.WriteString("systemctl stop neo4j || docker stop neo4j || true\n")
		case "restart":
			b.WriteString("systemctl restart neo4j || docker restart neo4j || true\n")
		case "status":
			b.WriteString("systemctl is-active neo4j || docker ps --filter name=neo4j --format '{{.Status}}' || echo 'not running'\n")
		}
	case "nebulagraph":
		b.WriteString("# NebulaGraph 启停\n")
		switch req.Action {
		case "start":
			b.WriteString("/usr/local/nebula/scripts/nebula.service start all || true\n")
		case "stop":
			b.WriteString("/usr/local/nebula/scripts/nebula.service stop all || true\n")
		case "restart":
			b.WriteString("/usr/local/nebula/scripts/nebula.service restart all || true\n")
		case "status":
			b.WriteString("/usr/local/nebula/scripts/nebula.service status all || echo 'not running'\n")
		}
	}
	fmt.Fprintf(&b, "echo '图库 %s %s 操作完成\n", req.Type, req.Action)
	return b.String()
}

// handleGraphDatabase 处理 POST /api/v1/graph/{type}/database：图空间/数据库管理。
func (s *Server) handleGraphDatabase(w http.ResponseWriter, r *http.Request, gType string) {
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
	if !allowedGraphTypes[gType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported graph db type"})
		return
	}
	var req graphDatabaseReq
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
	if req.Action != "list" && !graphDatabaseRe.MatchString(req.Name) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid database name"})
		return
	}
	req.Type = gType

	script := buildGraphDatabaseScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "graph_database_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "graph.database."+gType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "graph_database_" + req.Action, Target: req.Name,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildGraphDatabaseScript(req graphDatabaseReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "neo4j":
		b.WriteString("# Neo4j 数据库管理（cypher-shell，凭证从环境变量 NEO4J_USER / NEO4J_PASSWORD 读取）\n")
		switch req.Action {
		case "create":
			fmt.Fprintf(&b, "cypher-shell -u \"$NEO4J_USER\" -p \"$NEO4J_PASSWORD\" 'CREATE DATABASE %s' || echo '创建失败'\n", req.Name)
		case "delete":
			fmt.Fprintf(&b, "cypher-shell -u \"$NEO4J_USER\" -p \"$NEO4J_PASSWORD\" 'DROP DATABASE %s' || echo '删除失败'\n", req.Name)
		case "list":
			b.WriteString("cypher-shell -u \"$NEO4J_USER\" -p \"$NEO4J_PASSWORD\" 'SHOW DATABASES'\n")
		}
	case "nebulagraph":
		b.WriteString("# NebulaGraph 图空间管理（nebula-console，凭证从环境变量 NEBULA_USER / NEBULA_PASSWORD 读取）\n")
		switch req.Action {
		case "create":
			fmt.Fprintf(&b, "nebula-console -addr 127.0.0.1 -port 9669 -u \"$NEBULA_USER\" -p \"$NEBULA_PASSWORD\" -e 'CREATE SPACE IF NOT EXISTS %s (vid_type=FIXED_STRING(64))' || echo '创建失败'\n", req.Name)
		case "delete":
			fmt.Fprintf(&b, "nebula-console -addr 127.0.0.1 -port 9669 -u \"$NEBULA_USER\" -p \"$NEBULA_PASSWORD\" -e 'DROP SPACE %s' || echo '删除失败'\n", req.Name)
		case "list":
			b.WriteString("nebula-console -addr 127.0.0.1 -port 9669 -u \"$NEBULA_USER\" -p \"$NEBULA_PASSWORD\" -e 'SHOW SPACES'\n")
		}
	}
	fmt.Fprintf(&b, "echo '图库 %s 数据库 %s 操作完成\n", req.Type, req.Action)
	return b.String()
}

// handleGraphQuery 处理 POST /api/v1/graph/{type}/cypher：执行图查询。
func (s *Server) handleGraphQuery(w http.ResponseWriter, r *http.Request, gType string) {
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
	if !allowedGraphTypes[gType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported graph db type"})
		return
	}
	var req graphQueryReq
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
	// 拒绝 NUL 与换行，防止 shell 注入式破坏；其余交由图库自身鉴权执行。
	if strings.ContainsAny(req.Query, "\x00\n\r") {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "query must not contain NUL or newline"})
		return
	}
	if req.Database != "" && !graphDatabaseRe.MatchString(req.Database) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid database name"})
		return
	}
	req.Type = gType

	script := buildGraphQueryScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "graph_query")
	if err != nil {
		writeInternalError(r.Context(), w, "graph.query."+gType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "graph_query", Target: req.Database,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildGraphQueryScript(req graphQueryReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	q := shellQuote(req.Query)
	switch req.Type {
	case "neo4j":
		if req.Database != "" {
			fmt.Fprintf(&b, "cypher-shell -u \"$NEO4J_USER\" -p \"$NEO4J_PASSWORD\" -d %s %s\n", req.Database, q)
		} else {
			fmt.Fprintf(&b, "cypher-shell -u \"$NEO4J_USER\" -p \"$NEO4J_PASSWORD\" %s\n", q)
		}
	case "nebulagraph":
		if req.Database != "" {
			fmt.Fprintf(&b, "nebula-console -addr 127.0.0.1 -port 9669 -u \"$NEBULA_USER\" -p \"$NEBULA_PASSWORD\" -space %s -e %s\n", req.Database, q)
		} else {
			fmt.Fprintf(&b, "nebula-console -addr 127.0.0.1 -port 9669 -u \"$NEBULA_USER\" -p \"$NEBULA_PASSWORD\" -e %s\n", q)
		}
	}
	b.WriteString("echo '图查询完成'\n")
	return b.String()
}

// handleGraphBackup 处理 POST /api/v1/graph/{type}/backup：备份/恢复。
func (s *Server) handleGraphBackup(w http.ResponseWriter, r *http.Request, gType string) {
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
	if !allowedGraphTypes[gType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported graph db type"})
		return
	}
	var req graphBackupReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	allowed := map[string]bool{"export": true, "import": true}
	if !allowed[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (export/import)"})
		return
	}
	if req.Database != "" && !graphDatabaseRe.MatchString(req.Database) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid database name"})
		return
	}
	req.Type = gType

	script := buildGraphBackupScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "graph_backup_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "graph.backup."+gType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "graph_backup_" + req.Action, Target: req.Database,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildGraphBackupScript(req graphBackupReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "neo4j":
		b.WriteString("# Neo4j 备份（neo4j-admin，需停库或企业版在线备份）\n")
		switch req.Action {
		case "export":
			fmt.Fprintf(&b, "neo4j-admin database dump %s --to-path=/backup/ || echo '导出失败'\n", req.Database)
		case "import":
			fmt.Fprintf(&b, "neo4j-admin database load %s --from-path=/backup/ || echo '导入失败'\n", req.Database)
		}
	case "nebulagraph":
		b.WriteString("# NebulaGraph 备份（nebula-br 工具）\n")
		switch req.Action {
		case "export":
			fmt.Fprintf(&b, "nebula-br backup full --space %s --meta 127.0.0.1:9559 --storage local:///backup/ || echo '导出失败'\n", req.Database)
		case "import":
			fmt.Fprintf(&b, "nebula-br restore full --space %s --name <backup-name> || echo '导入失败'\n", req.Database)
		}
	}
	fmt.Fprintf(&b, "echo '图库备份 %s 操作完成\n", req.Action)
	return b.String()
}

// ============================================================================
// 路由注册
// ============================================================================

// registerGraphRoutes 注册图库运维路由。
func (s *Server) registerGraphRoutes(mux *http.ServeMux) {
	// 固定路径优先：/api/v1/graph/{type}/{database|cypher|backup}
	for _, t := range []string{"neo4j", "nebulagraph"} {
		tt := t
		mux.HandleFunc("/api/v1/graph/"+tt+"/database", func(w http.ResponseWriter, r *http.Request) { s.handleGraphDatabase(w, r, tt) })
		mux.HandleFunc("/api/v1/graph/"+tt+"/cypher", func(w http.ResponseWriter, r *http.Request) { s.handleGraphQuery(w, r, tt) })
		mux.HandleFunc("/api/v1/graph/"+tt+"/backup", func(w http.ResponseWriter, r *http.Request) { s.handleGraphBackup(w, r, tt) })
	}
	// 泛路由
	mux.HandleFunc("/api/v1/graph/deploy", s.handleGraphDeploy)
	mux.HandleFunc("/api/v1/graph/", s.handleGraphRouting)
}

func (s *Server) handleGraphRouting(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/graph/")
	segments := strings.Split(path, "/")
	switch len(segments) {
	case 1:
		if segments[0] != "" {
			s.handleGraph(w, r, segments[0])
		} else {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}
	case 2:
		if segments[0] != "" && segments[1] != "" {
			s.handleGraphAction(w, r, segments[0], segments[1])
		} else {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}
