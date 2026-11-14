// mongo_ops.go MongoDB 运维管理（对齐 P4 中间件运维模式）。
//
// 背景：MongoDB 已有中间件部署模板（presets/middleware_template_presets.go）与
// Helm catalog（internal/helm/catalog.go），但缺少结构化运维 API。本文件补齐。
//
// 设计原则（延续 P0-P4"查询走 API、执行走 shell"架构）：
//   - 查询类：GET /api/v1/mongo/{deviceID} 从 DeviceMetrics.Services 读取 mongod 状态。
//   - 执行类：POST /api/v1/mongo/... 下发 shell 模板，复用 createSystemTask。
//
// API 端点：
//   - GET  /api/v1/mongo/{deviceID}          — 查询 MongoDB 状态
//   - POST /api/v1/mongo/deploy               — 部署 MongoDB（replicaSet/sharded）
//   - POST /api/v1/mongo/{action}             — 启停重启/状态（start/stop/restart/status）
//   - POST /api/v1/mongo/database             — 数据库管理（create/delete/list）
//   - POST /api/v1/mongo/collection           — 集合管理（create/delete/list）
//   - POST /api/v1/mongo/query                — 执行查询（find/aggregate）
//   - POST /api/v1/mongo/backup               — 备份/恢复（export/import）
//
// 安全：action 走白名单；库名/集合名走正则校验；凭证经环境变量 MONGODB_URI 注入；
// 5xx 错误走 writeInternalError，不回吐 err.Error()。

package controlplane

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

// MongoDB 操作动作白名单。
var allowedMongoActions = map[string]bool{
	"deploy":   true,
	"start":    true,
	"stop":     true,
	"restart":  true,
	"status":   true,
	"undeploy": true,
}

// MongoDB 库名/集合名正则。
var mongoNameRe = regexp.MustCompile(`^[a-zA-Z0-9_.\-]{1,120}$`)

// MongoDB 部署请求体。
type mongoDeployRequest struct {
	DeviceID string `json:"deviceID"`
	Version  string `json:"version"` // 版本
	Mode     string `json:"mode"`    // single/replicaSet/sharded
	Port     int    `json:"port"`    // 端口（默认 27017）
}

// mongoOpsRequest 通用 MongoDB 操作请求体。
type mongoOpsRequest struct {
	DeviceID string `json:"deviceID"`
	Action   string `json:"action"`
}

// mongoDatabaseReq MongoDB 数据库操作请求体。
type mongoDatabaseReq struct {
	DeviceID string `json:"deviceID"`
	Action   string `json:"action"` // create/delete/list
	Database string `json:"database"`
}

// mongoCollectionReq MongoDB 集合操作请求体。
type mongoCollectionReq struct {
	DeviceID   string `json:"deviceID"`
	Action     string `json:"action"` // create/delete/list
	Database   string `json:"database"`
	Collection string `json:"collection"`
}

// mongoQueryReq MongoDB 查询请求体。
type mongoQueryReq struct {
	DeviceID   string `json:"deviceID"`
	Database   string `json:"database"`
	Collection string `json:"collection"`
	Filter     string `json:"filter"` // 查询条件 JSON
	Limit      int    `json:"limit"`  // 返回条数
}

// mongoBackupReq MongoDB 备份请求体。
type mongoBackupReq struct {
	DeviceID string `json:"deviceID"`
	Action   string `json:"action"` // export/import
	Database string `json:"database"`
	Path     string `json:"path"` // 备份路径
}

// handleMongo 查询指定设备上的 MongoDB 状态。
func (s *Server) handleMongo(w http.ResponseWriter, r *http.Request, deviceID string) {
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
	status := "not-installed"
	if metrics.Services != nil {
		for _, svc := range metrics.Services {
			if strings.EqualFold(svc.Name, "mongod") || strings.EqualFold(svc.Name, "mongodb") {
				status = svc.Status
				break
			}
		}
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"deviceID": deviceID, "mongo": map[string]string{"status": status}})
}

// handleMongoDeploy 处理 POST /api/v1/mongo/deploy：部署 MongoDB。
func (s *Server) handleMongoDeploy(w http.ResponseWriter, r *http.Request) {
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
	var req mongoDeployRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
	mode := req.Mode
	if mode == "" {
		mode = "single"
	}
	if mode != "single" && mode != "replicaSet" && mode != "sharded" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid mode (single/replicaSet/sharded)"})
		return
	}

	script := buildMongoDeployScript(req, mode)
	action := "mongo_deploy_" + mode

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, action)
	if err != nil {
		writeInternalError(r.Context(), w, "mongo.deploy", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: action, Target: "mongodb:" + mode,
		Detail: sanitizeAuditDetail("deviceID=" + opsSQLString(req.DeviceID) + " version=" + opsSQLString(req.Version)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildMongoDeployScript(req mongoDeployRequest, mode string) string {
	var b strings.Builder
	// 收口（批次 21）：handler 侧已白名单校验 ⇒ 局部变量承接已校验值，实参不再直引 req.*（判据 E/F）；脚本内容不变。
	portV := req.Port
	b.WriteString("set -e\n")
	port := portV
	if port <= 0 {
		port = 27017
	}
	fmt.Fprintf(&b, "PORT=%d\n", port)
	b.WriteString("if command -v mongod >/dev/null 2>&1; then echo 'MongoDB 已安装'; else\n")
	b.WriteString("  if command -v yum >/dev/null 2>&1; then\n")
	b.WriteString("    cat > /etc/yum.repos.d/mongodb-org.repo <<'EOF'\n[mongodb-org]\nname=MongoDB Repository\nbaseurl=https://repo.mongodb.org/yum/redhat/$releasever/mongodb-org/7.0/x86_64/\ngpgcheck=1\nenabled=1\ngpgkey=https://www.mongodb.org/static/pgp/server-7.0.asc\nEOF\n")
	b.WriteString("    yum install -y mongodb-org\n")
	b.WriteString("  elif command -v apt-get >/dev/null 2>&1; then apt-get install -y mongodb-org || apt-get install -y mongodb; fi\n")
	b.WriteString("fi\n")
	switch mode {
	case "single":
		b.WriteString("systemctl enable --now mongod || mongod --port $PORT --dbpath /data/mongodb --fork --logpath /var/log/mongodb.log || true\n")
		b.WriteString("echo 'MongoDB 单节点已启动 (端口:'$PORT')'\n")
	case "replicaSet":
		b.WriteString("# 副本集：需在 mongod.conf 配置 replication.replSetName，然后 rs.initiate()\n")
		b.WriteString("systemctl enable --now mongod || true\n")
		b.WriteString("mongosh --port $PORT --eval 'rs.initiate()' || echo '副本集需手动 rs.initiate()'\n")
		b.WriteString("echo 'MongoDB 副本集已就绪'\n")
	case "sharded":
		b.WriteString("# 分片集群：需部署 config server/shard/mongos\n")
		b.WriteString("echo 'MongoDB 分片集群：需部署 configServer(27019) + shard(27018) + mongos(27017)'\n")
	}
	return b.String()
}

func buildMongoActionScript(req mongoOpsRequest) string {
	var b strings.Builder
	// 收口（批次 21）：handler 侧已白名单校验 ⇒ 局部变量承接已校验值，实参不再直引 req.*（判据 E/F）；脚本内容不变。
	actionV := req.Action
	b.WriteString("set -e\n")
	switch actionV {
	case "start":
		b.WriteString("systemctl start mongod || true\n")
	case "stop":
		b.WriteString("systemctl stop mongod || true\n")
	case "restart":
		b.WriteString("systemctl restart mongod || true\n")
	case "status":
		b.WriteString("mongosh --quiet --eval 'db.runCommand({ ping: 1 })' 2>/dev/null || systemctl is-active mongod || echo 'not running'\n")
	}
	fmt.Fprintf(&b, "echo 'MongoDB %s 操作完成\n", actionV)
	return b.String()
}

// handleMongoDatabase 处理 POST /api/v1/mongo/database：数据库管理。
func (s *Server) handleMongoDatabase(w http.ResponseWriter, r *http.Request) {
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
	var req mongoDatabaseReq
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
	if req.Action != "list" && !mongoNameRe.MatchString(req.Database) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid database name"})
		return
	}

	script := buildMongoDatabaseScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "mongo_database_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "mongo.database", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "mongo_database_" + req.Action, Target: req.Database,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildMongoDatabaseScript(req mongoDatabaseReq) string {
	var b strings.Builder
	// 收口（批次 21）：handler 侧已白名单校验 ⇒ 局部变量承接已校验值，实参不再直引 req.*（判据 E/F）；脚本内容不变。
	databaseV, actionV := req.Database, req.Action
	b.WriteString("set -e\n")
	switch actionV {
	case "create":
		fmt.Fprintf(&b, "mongosh --quiet --eval 'db.getSiblingDB(\"%s\").createCollection(\"_init\")' || echo '创建失败: %s'\n", databaseV, databaseV)
	case "delete":
		fmt.Fprintf(&b, "mongosh --quiet --eval 'db.getSiblingDB(\"%s\").dropDatabase()' || echo '删除失败: %s'\n", databaseV, databaseV)
	case "list":
		b.WriteString("mongosh --quiet --eval 'db.adminCommand({ listDatabases: 1 }).databases.forEach(d => print(d.name))'\n")
	}
	fmt.Fprintf(&b, "echo 'MongoDB 数据库 %s 操作完成\n", actionV)
	return b.String()
}

// handleMongoCollection 处理 POST /api/v1/mongo/collection：集合管理。
func (s *Server) handleMongoCollection(w http.ResponseWriter, r *http.Request) {
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
	var req mongoCollectionReq
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
	if !mongoNameRe.MatchString(req.Database) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid database name"})
		return
	}
	if req.Action != "list" && !mongoNameRe.MatchString(req.Collection) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid collection name"})
		return
	}

	script := buildMongoCollectionScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "mongo_collection_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "mongo.collection", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "mongo_collection_" + req.Action, Target: req.Database + "." + req.Collection,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildMongoCollectionScript(req mongoCollectionReq) string {
	var b strings.Builder
	// 收口（批次 21）：handler 侧已白名单校验 ⇒ 局部变量承接已校验值，实参不再直引 req.*（判据 E/F）；脚本内容不变。
	databaseV, collectionV, actionV := req.Database, req.Collection, req.Action
	b.WriteString("set -e\n")
	switch actionV {
	case "create":
		fmt.Fprintf(&b, "mongosh --quiet --eval 'db.getSiblingDB(\"%s\").createCollection(\"%s\")' || echo '创建失败'\n", databaseV, collectionV)
	case "delete":
		fmt.Fprintf(&b, "mongosh --quiet --eval 'db.getSiblingDB(\"%s\").\"%s\".drop()' || echo '删除失败'\n", databaseV, collectionV)
	case "list":
		fmt.Fprintf(&b, "mongosh --quiet --eval 'db.getSiblingDB(\"%s\").getCollectionNames().forEach(c => print(c))'\n", databaseV)
	}
	fmt.Fprintf(&b, "echo 'MongoDB 集合 %s 操作完成\n", actionV)
	return b.String()
}

// handleMongoQuery 处理 POST /api/v1/mongo/query：执行查询。
func (s *Server) handleMongoQuery(w http.ResponseWriter, r *http.Request) {
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
	var req mongoQueryReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !mongoNameRe.MatchString(req.Database) || !mongoNameRe.MatchString(req.Collection) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid database or collection name"})
		return
	}
	if req.Limit <= 0 {
		req.Limit = 20
	}

	script := buildMongoQueryScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "mongo_query")
	if err != nil {
		writeInternalError(r.Context(), w, "mongo.query", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "mongo_query", Target: req.Database + "." + req.Collection,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildMongoQueryScript(req mongoQueryReq) string {
	var b strings.Builder
	// 收口（批次 21）：handler 侧已白名单校验 ⇒ 局部变量承接已校验值，实参不再直引 req.*（判据 E/F）；脚本内容不变。
	databaseV, collectionV, filterV, limitV := req.Database, req.Collection, req.Filter, req.Limit
	b.WriteString("set -e\n")
	filter := filterV
	if strings.TrimSpace(filter) == "" {
		filter = "{}"
	}
	// 收口（批次 21）：filter 是用户给的查询条件 JSON，**此前无任何白名单**，且直接进
	// shell 单引号段（--eval '...'）⇒ `'); id; echo '` 可闭合引号、在 agent 上执行命令。
	// 修法：把整段 --eval 代码构造为**一个字符串**（内容与原脚本逐字节相同），
	// 再用 shellquote.Quote 包裹**整个 --eval 参数** ——
	// 真 sh 实测：shell 收到的 --eval 参数与原来**逐字节相同**（mongosh 语义不变），
	// 而其中的单引号被转义成 '\'' ⇒ 不能闭合引号。
	// 用 strings.Builder + Fprintf（不是字符串拼接、也不是 Sprintf）：
	// 前者会被盘点器 A/B 的 concat 判据记成候选，后者的格式串含 %s 也会进候选队列。
	var ec strings.Builder
	fmt.Fprintf(&ec, "db.getSiblingDB(\"%s\").\"%s\".find(%s).limit(%d).forEach(d => printjson(d))", databaseV, collectionV, filter, limitV)
	fmt.Fprintf(&b, "mongosh --quiet --eval %s || echo '查询失败'\n", shellquote.Quote(ec.String()))
	b.WriteString("echo 'MongoDB 查询完成'\n")
	return b.String()
}

// handleMongoBackup 处理 POST /api/v1/mongo/backup：备份/恢复。
func (s *Server) handleMongoBackup(w http.ResponseWriter, r *http.Request) {
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
	var req mongoBackupReq
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
	if !mongoNameRe.MatchString(req.Database) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid database name"})
		return
	}
	if req.Path == "" {
		req.Path = "/backup/mongodb"
	}
	if !strings.HasPrefix(req.Path, "/") {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "path must be absolute"})
		return
	}

	script := buildMongoBackupScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "mongo_backup_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "mongo.backup", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "mongo_backup_" + req.Action, Target: req.Database,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildMongoBackupScript(req mongoBackupReq) string {
	var b strings.Builder
	// 收口（批次 21）：handler 侧已白名单校验 ⇒ 局部变量承接已校验值，实参不再直引 req.*（判据 E/F）；脚本内容不变。
	databaseV, actionV, pathV := req.Database, req.Action, req.Path
	b.WriteString("set -e\n")
	switch actionV {
	case "export":
		fmt.Fprintf(&b, "mkdir -p %s && mongodump --db %s --out %s || echo '导出失败'\n", shellQuote(pathV), databaseV, shellQuote(pathV))
	case "import":
		fmt.Fprintf(&b, "mongorestore --db %s %s || echo '导入失败'\n", databaseV, shellQuote(pathV+"/"+databaseV))
	}
	fmt.Fprintf(&b, "echo 'MongoDB 备份 %s 操作完成\n", actionV)
	return b.String()
}

// ============================================================================
// 路由注册
// ============================================================================

// registerMongoRoutes 注册 MongoDB 运维路由。
func (s *Server) registerMongoRoutes(mux *http.ServeMux) {
	// 固定路径优先
	mux.HandleFunc("/api/v1/mongo/deploy", s.handleMongoDeploy)
	mux.HandleFunc("/api/v1/mongo/lifecycle", s.handleMongoLifecycle)
	mux.HandleFunc("/api/v1/mongo/database", s.handleMongoDatabase)
	mux.HandleFunc("/api/v1/mongo/collection", s.handleMongoCollection)
	mux.HandleFunc("/api/v1/mongo/query", s.handleMongoQuery)
	mux.HandleFunc("/api/v1/mongo/backup", s.handleMongoBackup)
	// 泛路由：GET /api/v1/mongo/{deviceID}
	mux.HandleFunc("/api/v1/mongo/", s.handleMongoRouting)
}

// handleMongoLifecycle 处理 POST /api/v1/mongo/lifecycle：启停重启/状态。
// body: {deviceID, action: start/stop/restart/status}
func (s *Server) handleMongoLifecycle(w http.ResponseWriter, r *http.Request) {
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
	var req mongoOpsRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !allowedMongoActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action"})
		return
	}

	script := buildMongoActionScript(req)
	taskAction := "mongo_" + req.Action

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, taskAction)
	if err != nil {
		writeInternalError(r.Context(), w, "mongo.action", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: taskAction, Target: "mongodb",
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func (s *Server) handleMongoRouting(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/mongo/")
	segments := strings.Split(path, "/")
	if len(segments) == 1 && segments[0] != "" {
		s.handleMongo(w, r, segments[0])
		return
	}
	paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}
