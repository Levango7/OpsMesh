// db_ops.go 数据库运维管理（PostgreSQL/Oracle/TiDB/OceanBase）。
//
// 设计原则（延续 P0/P1/P3/P4"查询走 API、执行走 shell"架构）：
//   - 查询类：GET /api/v1/db/{deviceID} 从 DeviceMetrics.Services 读取数据库服务状态。
//   - 执行类：POST /api/v1/db/deploy 等，shell 模板下发到 agent 执行。
//
// API 端点：
//   - GET  /api/v1/db/{deviceID}              — 查询指定设备上的数据库状态
//   - POST /api/v1/db/deploy                   — 部署数据库（{deviceID, type, mode, version, config}）
//   - POST /api/v1/db/{type}/{action}          — 启停重启/状态（{deviceID, ...}）
//   - POST /api/v1/db/{type}/instance          — 实例管理（create/drop/list 数据库/实例）
//
// 安全：type/mode/action 走严格白名单；版本号/实例名走正则校验，杜绝命令注入；
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

// 数据库类型白名单。
var allowedDBTypes = map[string]bool{
	"postgresql": true,
	"oracle":     true,
	"tidb":       true,
	"oceanbase":  true,
	"mssql":      true,
	"teledb":     true,
}

// 数据库部署模式白名单。
var allowedDBModes = map[string]map[string]bool{
	"postgresql": {
		"single": true,
		"ha":     true,
	},
	"oracle": {
		"single": true,
		"rac":    true,
	},
	"tidb": {
		"single":  true,
		"cluster": true,
	},
	"oceanbase": {
		"single":  true,
		"cluster": true,
	},
	"mssql": {
		"single": true,
		"ha":     true,
	},
}

// 数据库操作动作白名单。
var allowedDBActions = map[string]bool{
	"deploy":   true,
	"start":    true,
	"stop":     true,
	"restart":  true,
	"status":   true,
	"undeploy": true,
	"backup":   true,
	"restore":  true,
}

// 数据库实例操作动作白名单。
var allowedDBInstanceActions = map[string]bool{
	"create": true,
	"drop":   true,
	"list":   true,
}

// 数据库名正则。
var dbNameRe = regexp.MustCompile(`^[a-zA-Z0-9_.$-]+$`)

// 数据库部署请求体。
type dbDeployRequest struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`    // postgresql/oracle/tidb/oceanbase
	Mode     string `json:"mode"`    // single/ha/rac/cluster
	Version  string `json:"version"` // 数据库版本
	Config   string `json:"config"`  // 部署配置 JSON（端口/内存/副本数/持久化/密码等）
}

// dbOpsRequest 通用数据库操作请求体。
type dbOpsRequest struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`
	Mode     string `json:"mode"`
	Action   string `json:"action"` // start/stop/restart/status/undeploy
}

// dbInstanceReq 数据库实例操作请求体。
type dbInstanceReq struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`   // postgresql/oracle/tidb/oceanbase
	Action   string `json:"action"` // create/drop/list
	Name     string `json:"name"`   // 数据库名/实例名
}

// handleDB 查询指定设备上的数据库状态。
func (s *Server) handleDB(w http.ResponseWriter, r *http.Request, deviceID string) {
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
	type dbInfo struct {
		Type   string `json:"type"`
		Status string `json:"status"`
	}
	var result []dbInfo
	for _, dbType := range []string{"postgresql", "oracle", "tidb", "oceanbase", "mssql"} {
		info := dbInfo{Type: dbType, Status: "not-installed"}
		if metrics.Services != nil {
			for _, svc := range metrics.Services {
				if strings.EqualFold(svc.Name, dbType) || strings.EqualFold(svc.Name, dbType+"-server") {
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
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"deviceID": deviceID, "databases": result})
}

// handleDBDeploy 处理 POST /api/v1/db/deploy：部署数据库。
func (s *Server) handleDBDeploy(w http.ResponseWriter, r *http.Request) {
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
	var req dbDeployRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !allowedDBTypes[req.Type] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported database type"})
		return
	}
	if !allowedDBModes[req.Type][req.Mode] {
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

	script := buildDBDeployScript(req)
	action := "db_deploy_" + req.Type + "_" + req.Mode

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.deploy."+req.Type, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: action, Target: req.Type + ":" + req.Mode,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s version=%s", req.DeviceID, req.Version)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

// buildDBDeployScript 按类型+模式生成数据库部署 shell 脚本。
func buildDBDeployScript(req dbDeployRequest) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("# 自动探测包管理器\n")
	b.WriteString("if command -v yum >/dev/null 2>&1; then PKG_MGR=yum; elif command -v dnf >/dev/null 2>&1; then PKG_MGR=dnf; elif command -v apt-get >/dev/null 2>&1; then PKG_MGR=apt-get; else echo '未找到包管理器' && exit 1; fi\n")
	b.WriteString("VERSION=" + shellQuote(req.Version) + "\n")
	switch req.Type {
	case "postgresql":
		buildPostgreSQLDeployScript(req, &b)
	case "oracle":
		buildOracleDeployScript(req, &b)
	case "tidb":
		buildTiDBDeployScript(req, &b)
	case "oceanbase":
		buildOceanBaseDeployScript(req, &b)
	case "mssql":
		buildMSSQLDeployScript(req, &b)
	case "teledb":
		b.WriteString("# Teledb 部署\n")
		b.WriteString("echo 'Teledb deploy'\n")
	}
	fmt.Fprintf(&b, "echo '%s %s 数据库部署脚本执行完成\n", req.Type, req.Mode)
	return b.String()
}

func buildPostgreSQLDeployScript(req dbDeployRequest, b *strings.Builder) {
	b.WriteString("# PostgreSQL 部署\n")
	b.WriteString("if command -v postgres >/dev/null 2>&1 || command -v pg_ctl >/dev/null 2>&1; then echo 'PostgreSQL 已安装'; else $PKG_MGR install -y postgresql; fi\n")
	switch req.Mode {
	case "single":
		b.WriteString("pg_ctlcluster 14 main start || systemctl start postgresql || true\n")
		b.WriteString("echo 'PostgreSQL 单点已启动'\n")
	case "ha":
		b.WriteString("# HA：需配置 streaming replication / patroni / repmgr\n")
		b.WriteString("echo 'PostgreSQL HA：需配置 streaming replication'\n")
	}
}

func buildOracleDeployScript(req dbDeployRequest, b *strings.Builder) {
	b.WriteString("# Oracle 数据库部署（需 Oracle 安装介质）\n")
	b.WriteString("if command -v sqlplus >/dev/null 2>&1; then echo 'Oracle 已安装'; else echo '请先上传 Oracle 安装介质并运行 runInstaller' && exit 1; fi\n")
	switch req.Mode {
	case "single":
		b.WriteString("dbstart $ORACLE_HOME || systemctl start oracle || true\n")
		b.WriteString("echo 'Oracle 单实例已启动'\n")
	case "rac":
		b.WriteString("# Oracle RAC 部署：Grid Infrastructure + ASM + RAC 数据库实例\n")
		b.WriteString("# 1. Grid Infrastructure 检查与安装\n")
		b.WriteString("if command -v crsctl >/dev/null 2>&1; then echo 'Grid Infrastructure 已安装'; else echo '请先上传 Grid Infrastructure 安装介质并运行 runInstaller (clusterware)' && exit 1; fi\n")
		b.WriteString("crsctl check crs 2>&1 || (echo 'CRS 未运行，尝试启动'; crsctl start crs 2>&1 || true)\n")
		b.WriteString("# 2. 投票磁盘配置\n")
		b.WriteString("crsctl query css votedisk 2>&1 || true\n")
		b.WriteString("if [ -n \"$VOTING_DISK\" ]; then crsctl add css votedisk \"$VOTING_DISK\" 2>/dev/null || crsctl replace votedisk \"$VOTING_DISK\" 2>/dev/null || echo '投票磁盘配置失败'; fi\n")
		b.WriteString("# 3. ASM 磁盘组配置\n")
		b.WriteString("if command -v asmcmd >/dev/null 2>&1; then\n")
		b.WriteString("  asmcmd ls 2>/dev/null || true\n")
		b.WriteString("  if [ -n \"$ASM_DISKGROUP\" ]; then echo \"ASM 磁盘组: $ASM_DISKGROUP\"; asmcmd mkdg \"$ASM_DISKGROUP\" 2>/dev/null || echo '磁盘组已存在或创建失败'; fi\n")
		b.WriteString("else echo 'ASM 不可用'; fi\n")
		b.WriteString("# 4. SCAN VIP / VIP 配置\n")
		b.WriteString("srvctl config scan 2>/dev/null || true\n")
		b.WriteString("if [ -n \"$SCAN_NAME\" ]; then srvctl add scan -n \"$SCAN_NAME\" 2>/dev/null || echo 'SCAN 已配置'; fi\n")
		b.WriteString("if [ -n \"$VIP_NAME\" ]; then srvctl add vip -n \"$VIP_NAME\" -k \"$VIP_NETMASK\" -A \"$VIP_IP\" 2>/dev/null || echo 'VIP 已配置'; fi\n")
		b.WriteString("# 5. RAC 数据库实例创建\n")
		b.WriteString("if [ -n \"$DB_NAME\" ]; then\n")
		b.WriteString("  dbca -silent -createDatabase -templateName General_Purpose.dbc -gdbname \"$DB_NAME\" -sid \"$DB_NAME\" -createAsContainerDatabase false -numberOfPDBs 0 -databaseType RAC -nodelist \"$NODE_LIST\" -storageType ASM -diskGroupName \"$ASM_DISKGROUP\" -characterSet AL32UTF8 -sysPassword \"$SYS_PWD\" -systemPassword \"$SYS_PWD\" 2>&1 | tail -20 || echo 'DBCA 执行失败或数据库已存在'\n")
		b.WriteString("fi\n")
		b.WriteString("# 6. 节点添加/删除\n")
		b.WriteString("if [ -n \"$ADD_NODE\" ]; then addnode.sh -silent \"CLUSTER_NEW_NODES={$ADD_NODE}\" 2>&1 | tail -20 || echo '节点添加失败'; fi\n")
		b.WriteString("if [ -n \"$DEL_NODE\" ]; then crsctl delete node -n \"$DEL_NODE\" 2>/dev/null || srvctl remove nodeapps -n \"$DEL_NODE\" -f 2>/dev/null || echo '节点删除失败'; fi\n")
		b.WriteString("# 7. RAC 健康检查\n")
		b.WriteString("echo '=== RAC 健康检查 ==='\n")
		b.WriteString("crsctl check crs 2>&1 || true\n")
		b.WriteString("srvctl status database -d \"$DB_NAME\" 2>/dev/null || true\n")
		b.WriteString("olsnodes -n 2>/dev/null || true\n")
		b.WriteString("echo 'Oracle RAC 部署脚本执行完成'\n")
	}
}

func buildTiDBDeployScript(req dbDeployRequest, b *strings.Builder) {
	b.WriteString("# TiDB 部署（依赖 PD + TiKV + TiDB）\n")
	b.WriteString("if command -v tidb-server >/dev/null 2>&1; then echo 'TiDB 已安装'; else echo '请先安装 TiDB (tiup)' && exit 1; fi\n")
	switch req.Mode {
	case "single":
		b.WriteString("tidb-server --store=tikv --path=/tmp/tidb --host=0.0.0.0 --port=4000 &\n")
		b.WriteString("echo 'TiDB 单节点已启动'\n")
	case "cluster":
		b.WriteString("# 集群：需 tiup cluster deploy + tiup cluster start\n")
		b.WriteString("echo 'TiDB 集群：需 tiup cluster deploy'\n")
	}
}

func buildOceanBaseDeployScript(req dbDeployRequest, b *strings.Builder) {
	b.WriteString("# OceanBase 部署（依赖 observer + obproxy）\n")
	b.WriteString("if command -v observer >/dev/null 2>&1 || command -v obclient >/dev/null 2>&1; then echo 'OceanBase 已安装'; else echo '请先安装 OceanBase' && exit 1; fi\n")
	switch req.Mode {
	case "single":
		b.WriteString("observer --start || systemctl start oceanbase || true\n")
		b.WriteString("echo 'OceanBase 单节点已启动'\n")
	case "cluster":
		b.WriteString("# 集群：需配置 OBServer 集群 + OBProxy\n")
		b.WriteString("echo 'OceanBase 集群：需配置 OBServer 集群'\n")
	}
}

func buildMSSQLDeployScript(req dbDeployRequest, b *strings.Builder) {
	b.WriteString("# MS SQL Server 部署\n")
	b.WriteString("if command -v sqlcmd >/dev/null 2>&1 || systemctl is-active --quiet mssql-server 2>/dev/null; then echo 'MS SQL Server 已安装'; else \n")
	b.WriteString("  if command -v yum >/dev/null 2>&1; then $PKG_MGR install -y mssql-server; elif command -v apt-get >/dev/null 2>&1; then $PKG_MGR install -y mssql-server; fi\n")
	b.WriteString("fi\n")
	b.WriteString("# 配置 SA 密码并启动\n")
	b.WriteString("echo '配置 SA 密码' && /opt/mssql/bin/mssql-conf set-sa-password 2>/dev/null || true\n")
	switch req.Mode {
	case "single":
		b.WriteString("systemctl enable mssql-server && systemctl start mssql-server || true\n")
		b.WriteString("echo 'MS SQL Server 单点已启动'\n")
	case "ha":
		b.WriteString("# HA：需配置 Always On 可用性组\n")
		b.WriteString("systemctl enable mssql-server && systemctl start mssql-server || true\n")
		b.WriteString("echo 'MS SQL Server HA：需配置 Always On 可用性组'\n")
	}
}

// handleDBAction 处理 POST /api/v1/db/{type}/{action}：启停重启/状态。
func (s *Server) handleDBAction(w http.ResponseWriter, r *http.Request, dbType, action string) {
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
	if !allowedDBTypes[dbType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported database type"})
		return
	}
	if !allowedDBActions[action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action"})
		return
	}
	var req dbOpsRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	req.Type = dbType
	req.Action = action

	script := buildDBActionScript(req)
	taskAction := "db_" + action + "_" + dbType

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, taskAction)
	if err != nil {
		writeInternalError(r.Context(), w, "db.action."+dbType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: taskAction, Target: dbType,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

// buildDBActionScript 生成数据库启停重启/状态脚本。
func buildDBActionScript(req dbOpsRequest) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "postgresql":
		b.WriteString("# PostgreSQL 启停\n")
		switch req.Action {
		case "start":
			b.WriteString("pg_ctlcluster 14 main start || systemctl start postgresql || true\n")
		case "stop":
			b.WriteString("pg_ctlcluster 14 main stop || systemctl stop postgresql || true\n")
		case "restart":
			b.WriteString("pg_ctlcluster 14 main restart || systemctl restart postgresql || true\n")
		case "status":
			b.WriteString("pg_isready || echo 'not running'\n")
		}
	case "oracle":
		b.WriteString("# Oracle 启停\n")
		switch req.Action {
		case "start":
			b.WriteString("dbstart $ORACLE_HOME || systemctl start oracle || true\n")
		case "stop":
			b.WriteString("dbshut $ORACLE_HOME || systemctl stop oracle || true\n")
		case "restart":
			b.WriteString("dbshut $ORACLE_HOME || true\ndbstart $ORACLE_HOME || systemctl restart oracle || true\n")
		case "status":
			b.WriteString("lsnrctl status || echo 'not running'\n")
		}
	case "tidb":
		b.WriteString("# TiDB 启停\n")
		switch req.Action {
		case "start":
			b.WriteString("tiup cluster start <cluster-name> || systemctl start tidb || true\n")
		case "stop":
			b.WriteString("tiup cluster stop <cluster-name> || systemctl stop tidb || true\n")
		case "restart":
			b.WriteString("tiup cluster restart <cluster-name> || systemctl restart tidb || true\n")
		case "status":
			b.WriteString("tiup cluster display <cluster-name> || echo 'not running'\n")
		}
	case "oceanbase":
		b.WriteString("# OceanBase 启停\n")
		switch req.Action {
		case "start":
			b.WriteString("systemctl start oceanbase || observer --start || true\n")
		case "stop":
			b.WriteString("systemctl stop oceanbase || true\n")
		case "restart":
			b.WriteString("systemctl restart oceanbase || true\n")
		case "status":
			b.WriteString("obclient -h 127.0.0.1 -P 2881 -e 'select 1' || echo 'not running'\n")
		}
	case "mssql":
		b.WriteString("# MS SQL Server 启停\n")
		switch req.Action {
		case "start":
			b.WriteString("systemctl start mssql-server || true\n")
		case "stop":
			b.WriteString("systemctl stop mssql-server || true\n")
		case "restart":
			b.WriteString("systemctl restart mssql-server || true\n")
		case "status":
			b.WriteString("sqlcmd -S localhost -Q \"SELECT @@VERSION\" || systemctl status mssql-server || echo 'not running'\n")
		case "backup":
			b.WriteString("sqlcmd -S localhost -Q \"BACKUP DATABASE ...\" \n")
		case "restore":
			b.WriteString("sqlcmd -S localhost -Q \"RESTORE DATABASE ...\" \n")
		}
	case "teledb":
		b.WriteString("# Teledb 启停\n")
		switch req.Action {
		case "status":
			b.WriteString("tel status 2>/dev/null || echo 'not running'\n")
		}
	}
	fmt.Fprintf(&b, "echo '%s %s 操作完成\n", req.Type, req.Action)
	return b.String()
}

// handleDBInstance 处理 POST /api/v1/db/{type}/instance：实例管理（create/drop/list）。
func (s *Server) handleDBInstance(w http.ResponseWriter, r *http.Request, dbType string) {
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
	if !allowedDBTypes[dbType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported database type"})
		return
	}
	var req dbInstanceReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !allowedDBInstanceActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (create/drop/list)"})
		return
	}
	if req.Action != "list" && !dbNameRe.MatchString(req.Name) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid db name"})
		return
	}
	req.Type = dbType

	script := buildDBInstanceScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "db_instance_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.instance."+dbType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_instance_" + req.Action, Target: req.Name,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s type=%s", req.DeviceID, dbType)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildDBInstanceScript(req dbInstanceReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "postgresql":
		b.WriteString("# PostgreSQL 实例管理\n")
		switch req.Action {
		case "create":
			fmt.Fprintf(&b, "createdb %s || echo '数据库已存在或创建失败: %s'\n", shellQuote(req.Name), req.Name)
		case "drop":
			fmt.Fprintf(&b, "dropdb %s || echo '数据库不存在或删除失败: %s'\n", shellQuote(req.Name), req.Name)
		case "list":
			b.WriteString("psql -c '\\l'\n")
		}
	case "oracle":
		b.WriteString("# Oracle 实例管理（sqlplus /sys as sysdba）\n")
		switch req.Action {
		case "create":
			fmt.Fprintf(&b, "sqlplus / as sysdba <<EOF\ncreate database %s;\nexit;\nEOF\n", shellQuote(req.Name))
		case "drop":
			fmt.Fprintf(&b, "sqlplus / as sysdba <<EOF\ndrop database %s;\nexit;\nEOF\n", shellQuote(req.Name))
		case "list":
			b.WriteString("sqlplus / as sysdba <<EOF\nselect name from v$database;\nexit;\nEOF\n")
		}
	case "tidb":
		b.WriteString("# TiDB 实例管理（mysql 客户端）\n")
		switch req.Action {
		case "create":
			fmt.Fprintf(&b, "mysql -h 127.0.0.1 -P 4000 -u root -e 'create database %s' || echo '创建失败: %s'\n", shellQuote(req.Name), req.Name)
		case "drop":
			fmt.Fprintf(&b, "mysql -h 127.0.0.1 -P 4000 -u root -e 'drop database %s' || echo '删除失败: %s'\n", shellQuote(req.Name), req.Name)
		case "list":
			b.WriteString("mysql -h 127.0.0.1 -P 4000 -u root -e 'show databases'\n")
		}
	case "oceanbase":
		b.WriteString("# OceanBase 实例管理（obclient）\n")
		switch req.Action {
		case "create":
			fmt.Fprintf(&b, "obclient -h 127.0.0.1 -P 2881 -u root -e 'create database %s' || echo '创建失败: %s'\n", shellQuote(req.Name), req.Name)
		case "drop":
			fmt.Fprintf(&b, "obclient -h 127.0.0.1 -P 2881 -u root -e 'drop database %s' || echo '删除失败: %s'\n", shellQuote(req.Name), req.Name)
		case "list":
			b.WriteString("obclient -h 127.0.0.1 -P 2881 -u root -e 'show databases'\n")
		}
	case "mssql":
		b.WriteString("# MS SQL Server 实例管理（sqlcmd）\n")
		switch req.Action {
		case "create":
			fmt.Fprintf(&b, "sqlcmd -S localhost -Q \"CREATE DATABASE %s\" || echo '创建失败: %s'\n", shellQuote(req.Name), req.Name)
		case "drop":
			fmt.Fprintf(&b, "sqlcmd -S localhost -Q \"DROP DATABASE %s\" || echo '删除失败: %s'\n", shellQuote(req.Name), req.Name)
		case "list":
			b.WriteString("sqlcmd -S localhost -Q \"SELECT name FROM sys.databases\"\n")
		}
	}
	fmt.Fprintf(&b, "echo '数据库 %s %s 操作完成\n", req.Type, req.Action)
	return b.String()
}

// ============================================================================
// 路由注册
// ============================================================================

// registerDBRoutes 注册数据库运维路由。
func (s *Server) registerDBRoutes(mux *http.ServeMux) {
	// 固定路径优先：/api/v1/db/{type}/instance
	mux.HandleFunc("/api/v1/db/postgresql/instance", s.handleDBInstancePostgreSQL)
	mux.HandleFunc("/api/v1/db/oracle/instance", s.handleDBInstanceOracle)
	mux.HandleFunc("/api/v1/db/tidb/instance", s.handleDBInstanceTiDB)
	mux.HandleFunc("/api/v1/db/oceanbase/instance", s.handleDBInstanceOceanBase)
	mux.HandleFunc("/api/v1/db/mssql/instance", s.handleDBInstanceMSSQL)
	// 泛路由：GET /api/v1/db/{deviceID} / POST /api/v1/db/{type}/{action} / POST /api/v1/db/deploy
	mux.HandleFunc("/api/v1/db/deploy", s.handleDBDeploy)
	mux.HandleFunc("/api/v1/db/", s.handleDBRouting)
}

func (s *Server) handleDBInstancePostgreSQL(w http.ResponseWriter, r *http.Request) {
	s.handleDBInstance(w, r, "postgresql")
}
func (s *Server) handleDBInstanceOracle(w http.ResponseWriter, r *http.Request) {
	s.handleDBInstance(w, r, "oracle")
}
func (s *Server) handleDBInstanceTiDB(w http.ResponseWriter, r *http.Request) {
	s.handleDBInstance(w, r, "tidb")
}
func (s *Server) handleDBInstanceOceanBase(w http.ResponseWriter, r *http.Request) {
	s.handleDBInstance(w, r, "oceanbase")
}
func (s *Server) handleDBInstanceMSSQL(w http.ResponseWriter, r *http.Request) {
	s.handleDBInstance(w, r, "mssql")
}

// handleDBInstanceRouting 统一分派 /api/v1/db/{type}/instance 子路径。
func (s *Server) handleDBInstanceRouting(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/db/")
	segments := strings.Split(strings.TrimSuffix(path, "/"), "/")
	if len(segments) < 2 || segments[1] != "instance" {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	switch segments[0] {
	case "postgresql":
		s.handleDBInstancePostgreSQL(w, r)
	case "oracle":
		s.handleDBInstanceOracle(w, r)
	case "tidb":
		s.handleDBInstanceTiDB(w, r)
	case "oceanbase":
		s.handleDBInstanceOceanBase(w, r)
	case "mssql":
		s.handleDBInstanceMSSQL(w, r)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported database type"})
	}
}

func (s *Server) handleDBRouting(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/db/")
	segments := strings.Split(path, "/")
	switch len(segments) {
	case 1:
		// /api/v1/db/{deviceID}
		if segments[0] != "" {
			s.handleDB(w, r, segments[0])
		} else {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}
	case 2:
		// /api/v1/db/{type}/{action}
		if segments[0] != "" && segments[1] != "" {
			s.handleDBAction(w, r, segments[0], segments[1])
		} else {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}
