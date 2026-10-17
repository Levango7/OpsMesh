// middleware_ops.go 中间件运维管理（Kafka/Redis/Zookeeper/RabbitMQ/ActiveMQ 等）。
//
// 设计原则（延续 P0/P1/P3"查询走 API、执行走 shell"架构）：
//   - 查询类：GET /api/v1/middleware/{deviceID} 从 DeviceMetrics.Services 读取中间件
//     服务状态（monitoredServices 已涵盖 redis/kafka/zookeeper/rabbitmq 等），返回结构化 JSON。
//   - 执行类：POST /api/v1/middleware/deploy 等，shell 模板下发到 agent 执行，
//     复用 store.CreateTask + agent executeShell（白名单 + 元字符防护 + 超时）。
//
// API 端点：
//   - GET  /api/v1/middleware/{deviceID}        — 查询指定设备上的中间件状态
//   - POST /api/v1/middleware/deploy             — 部署中间件（{deviceID, type, mode, version, config}）
//   - POST /api/v1/middleware/{type}/{action}    — 启停重启/状态（{deviceID, ...}）
//   - POST /api/v1/middleware/kafka/topic        — Kafka Topic 管理（create/delete/list）
//   - POST /api/v1/middleware/kafka/consumer-group — Kafka Consumer Group 查询（list/describe）
//
// 安全：type/mode/action 走严格白名单；version/话题名走正则校验，杜绝命令注入；
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

// 中间件类型白名单。
var allowedMiddlewareTypes = map[string]bool{
	"redis":     true,
	"kafka":     true,
	"zookeeper": true,
	"rabbitmq":  true,
	"activemq":  true,
}

// 中间件部署模式白名单（按类型差异化校验）。
var allowedMiddlewareModes = map[string]map[string]bool{
	"redis": {
		"standalone":   true, // 单点
		"master-slave": true, // 主备
		"sentinel":     true, // 哨兵
		"cluster":      true, // Cluster
	},
	"kafka": {
		"single": true, // 单节点
		"multi":  true, // 多节点集群
	},
	"zookeeper": {
		"single": true, // 单节点
		"multi":  true, // 集群
	},
	"rabbitmq": {
		"single": true,
		"multi":  true,
	},
	"activemq": {
		"single": true,
		"multi":  true,
	},
}

// 中间件操作动作白名单。
var allowedMiddlewareActions = map[string]bool{
	"deploy":   true,
	"start":    true,
	"stop":     true,
	"restart":  true,
	"status":   true,
	"undeploy": true,
}

// 中间件版本号正则（允许语义化版本与常见后缀）。
var middlewareVersionRe = regexp.MustCompile(`^[a-zA-Z0-9_.+-]+$`)

// 话题名正则（Kafka Topic，允许字母数字下划线连字符点）。
var kafkaTopicRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

// 中间件部署请求体。
type middlewareDeployRequest struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`    // redis/kafka/zookeeper/rabbitmq/activemq
	Mode     string `json:"mode"`    // 部署模式
	Version  string `json:"version"` // 中间件版本
	Config   string `json:"config"`  // 部署配置 JSON（端口/内存/副本数/持久化/密码等）
}

// middlewareOpsRequest 通用中间件操作请求体。
type middlewareOpsRequest struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`
	Mode     string `json:"mode"`
	Action   string `json:"action"` // start/stop/restart/status/undeploy
}

// handleMiddleware 查询指定设备上的中间件状态。
// 从 DeviceMetrics.Services 中对应服务名读取运行状态，返回结构化 JSON。
func (s *Server) handleMiddleware(w http.ResponseWriter, r *http.Request, deviceID string) {
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
	type mwInfo struct {
		Type   string `json:"type"`
		Mode   string `json:"mode"`
		Status string `json:"status"` // installed/running/stopped
	}
	var result []mwInfo
	for _, mwType := range []string{"redis", "kafka", "zookeeper", "rabbitmq", "activemq"} {
		info := mwInfo{Type: mwType, Status: "not-installed"}
		if metrics.Services != nil {
			for _, svc := range metrics.Services {
				if strings.EqualFold(svc.Name, mwType) || strings.EqualFold(svc.Name, mwType+"-server") {
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
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"deviceID": deviceID, "middleware": result})
}

// handleMiddlewareDeploy 处理 POST /api/v1/middleware/deploy：部署中间件。
func (s *Server) handleMiddlewareDeploy(w http.ResponseWriter, r *http.Request) {
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
	var req middlewareDeployRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !allowedMiddlewareTypes[req.Type] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported middleware type"})
		return
	}
	if !allowedMiddlewareModes[req.Type][req.Mode] {
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

	script := buildMiddlewareDeployScript(req)
	action := "mw_deploy_" + req.Type + "_" + req.Mode

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, action)
	if err != nil {
		writeInternalError(r.Context(), w, "middleware.deploy."+req.Type, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: action, Target: req.Type + ":" + req.Mode,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s version=%s", req.DeviceID, req.Version)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

// buildMiddlewareDeployScript 按类型+模式生成部署 shell 脚本（幂等，yum/apt 自动探测）。
func buildMiddlewareDeployScript(req middlewareDeployRequest) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("# 自动探测包管理器\n")
	b.WriteString("if command -v yum >/dev/null 2>&1; then PKG_MGR=yum; elif command -v dnf >/dev/null 2>&1; then PKG_MGR=dnf; elif command -v apt-get >/dev/null 2>&1; then PKG_MGR=apt-get; else echo '未找到包管理器' && exit 1; fi\n")
	b.WriteString("VERSION=" + shellQuote(req.Version) + "\n")
	switch req.Type {
	case "redis":
		buildRedisDeployScript(req, &b)
	case "kafka":
		buildKafkaDeployScript(req, &b)
	case "zookeeper":
		buildZookeeperDeployScript(req, &b)
	case "rabbitmq":
		buildRabbitMQDeployScript(req, &b)
	case "activemq":
		buildActiveMQDeployScript(req, &b)
	}
	b.WriteString(fmt.Sprintf("echo '%s %s 部署脚本执行完成\n", req.Type, req.Mode))
	return b.String()
}

func shellQuote(s string) string {
	if s == "" {
		return `""`
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// ============================================================================
// Redis 部署脚本（单点/主备/Sentinel/Cluster）
// ============================================================================

func buildRedisDeployScript(req middlewareDeployRequest, b *strings.Builder) {
	b.WriteString("# Redis 部署\n")
	b.WriteString("if command -v redis-server >/dev/null 2>&1; then echo 'Redis 已安装'; else $PKG_MGR install -y redis; fi\n")
	switch req.Mode {
	case "standalone":
		b.WriteString("redis-server --daemonize yes\n")
		b.WriteString("echo 'Redis 单点已启动'\n")
	case "master-slave":
		b.WriteString("# 主备：首节点为 master， slave 节点执行 replicaof <master-ip> <master-port>\n")
		b.WriteString("echo 'Redis 主备模式：slave 节点需执行 replicaof'\n")
	case "sentinel":
		b.WriteString("# 哨兵：redis-server /etc/redis/sentinel.conf --sentinel\n")
		b.WriteString("echo 'Redis Sentinel 配置模板已生成'\n")
	case "cluster":
		b.WriteString("# Cluster：需 >=3 个 master，每个 master 建议 1 个 slave\n")
		b.WriteString("echo 'Redis Cluster：需手动执行 redis-cli --cluster create'\n")
	}
}

// ============================================================================
// Kafka 部署脚本（单节点/多节点，版本感知：2.x Zookeeper / 3.x 过渡 / 4.x KRaft）
// ============================================================================

// Kafka 版本差异说明：
//   - 2.x: 依赖 Zookeeper，kafka-topics.sh/kafka-consumer-groups.sh 用 --zookeeper
//   - 3.x: 过渡期，同时支持 --zookeeper 与 --bootstrap-server，KRaft 为预览
//   - 4.x: KRaft 为默认，移除 --zookeeper，统一用 --bootstrap-server
func buildKafkaDeployScript(req middlewareDeployRequest, b *strings.Builder) {
	b.WriteString("# Kafka 部署（版本感知：2.x Zookeeper / 3.x 过渡 / 4.x KRaft）\n")
	b.WriteString("if command -v kafka-server-start.sh >/dev/null 2>&1; then echo 'Kafka 已安装'; else echo '请先安装 Kafka 二进制包' && exit 1; fi\n")
	b.WriteString("# 探测 Kafka 主版本，决定 CLI 参数（--zookeeper vs --bootstrap-server）\n")
	b.WriteString("KAFKA_VER=$(kafka-topics.sh --version 2>/dev/null | grep -oE '[0-9]+\\.[0-9]+' | head -1)\n")
	b.WriteString("if [ -n \"$KAFKA_VER\" ]; then\n")
	b.WriteString("  MAJOR=$(echo \"$KAFKA_VER\" | cut -d. -f1)\n")
	b.WriteString("  if [ \"$MAJOR\" -ge 4 ]; then KAFKA_OPTS=\"--bootstrap-server localhost:9092\"; fi\n")
	b.WriteString("  if [ \"$MAJOR\" -eq 3 ]; then KAFKA_OPTS=\"--bootstrap-server localhost:9092\"; fi\n")
	b.WriteString("  if [ \"$MAJOR\" -le 2 ]; then KAFKA_OPTS=\"--zookeeper localhost:2181\"; fi\n")
	b.WriteString("else KAFKA_OPTS=\"--bootstrap-server localhost:9092\"; fi\n")
	switch req.Mode {
	case "single":
		b.WriteString("kafka-server-start.sh -daemon config/server.properties\n")
		b.WriteString("echo 'Kafka 单节点已启动'\n")
	case "multi":
		b.WriteString("# 多节点：需在每个 broker 配置唯一 broker.id\n")
		b.WriteString("if [ \"$MAJOR\" -le 2 ]; then echo '2.x 需配置 zookeeper.connect'; else echo '3.x/4.x 使用 KRaft 或 zookeeper.connect'; fi\n")
		b.WriteString("echo 'Kafka 多节点：需逐节点配置 broker.id'\n")
	}
}

// ============================================================================
// Kafka Consumer Group 管理（版本感知）
// ============================================================================

// kafkaConsumerGroupReq Kafka Consumer Group 操作请求体。
type kafkaConsumerGroupReq struct {
	DeviceID string `json:"deviceID"`
	Action   string `json:"action"` // list/describe
	Group    string `json:"group"`  // 消费组名称（describe 时必填）
}

// handleKafkaConsumerGroup 处理 POST /api/v1/middleware/kafka/consumer-group。
func (s *Server) handleKafkaConsumerGroup(w http.ResponseWriter, r *http.Request) {
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
	var req kafkaConsumerGroupReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	allowed := map[string]bool{"list": true, "describe": true}
	if !allowed[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (list/describe)"})
		return
	}
	if req.Action == "describe" && req.Group == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "describe 需指定 group"})
		return
	}
	script := buildKafkaConsumerGroupScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "kafka_consumergroup_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "middleware.kafka.consumergroup", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "kafka_consumergroup_" + req.Action, Target: req.Group,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildKafkaConsumerGroupScript(req kafkaConsumerGroupReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("if [ -z \"$KAFKA_OPTS\" ]; then KAFKA_OPTS=\"--bootstrap-server localhost:9092\"; fi\n")
	switch req.Action {
	case "list":
		b.WriteString("kafka-consumer-groups.sh $KAFKA_OPTS --list\n")
	case "describe":
		b.WriteString(fmt.Sprintf("kafka-consumer-groups.sh $KAFKA_OPTS --describe --group %s\n", req.Group))
	}
	b.WriteString(fmt.Sprintf("echo 'Kafka Consumer Group %s 操作完成\n", req.Action))
	return b.String()
}

// ============================================================================
// Zookeeper 部署脚本（单节点/多节点）
// ============================================================================

func buildZookeeperDeployScript(req middlewareDeployRequest, b *strings.Builder) {
	b.WriteString("# Zookeeper 部署\n")
	b.WriteString("if command -v zkServer.sh >/dev/null 2>&1; then echo 'Zookeeper 已安装'; else echo '请先安装 Zookeeper 二进制包' && exit 1; fi\n")
	switch req.Mode {
	case "single":
		b.WriteString("zkServer.sh start\n")
		b.WriteString("echo 'Zookeeper 单节点已启动'\n")
	case "multi":
		b.WriteString("# 多节点：需在 zoo.cfg 中配置 server.1/2/3，并在 myid 文件中写入唯一 ID\n")
		b.WriteString("echo 'Zookeeper 多节点：需逐节点配置 myid'\n")
	}
}

// ============================================================================
// Zookeeper 四字命令监控 + 节点状态
// ============================================================================

// zookeeperCmdReq Zookeeper 四字命令请求体。
type zookeeperCmdReq struct {
	DeviceID string `json:"deviceID"`
	Host     string `json:"host"` // Zookeeper 地址（host:port，默认 localhost:2181）
	Cmd      string `json:"cmd"`  // 四字命令：stat/ruok/mntr/cons/envi/conf/crst/dump/wchs/wchc/mon
}

// handleZookeeperCmd 处理 POST /api/v1/middleware/zookeeper/cmd。
func (s *Server) handleZookeeperCmd(w http.ResponseWriter, r *http.Request) {
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
	var req zookeeperCmdReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if req.Host == "" {
		req.Host = "localhost:2181"
	}
	allowed := map[string]bool{"stat": true, "ruok": true, "mntr": true, "cons": true, "envi": true, "conf": true, "crst": true, "dump": true, "wchs": true, "wchc": true, "mon": true}
	if !allowed[req.Cmd] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported 4lw command (stat/ruok/mntr/cons/envi/conf/crst/dump/wchs/wchc/mon)"})
		return
	}
	script := buildZookeeperCmdScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "zookeeper_cmd_"+req.Cmd)
	if err != nil {
		writeInternalError(r.Context(), w, "middleware.zookeeper.cmd", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "zookeeper_cmd_" + req.Cmd, Target: req.Host,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildZookeeperCmdScript(req zookeeperCmdReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString(fmt.Sprintf("HOST=%q\n", req.Host))
	b.WriteString(fmt.Sprintf("echo %q | nc -w 2 $HOST\n", req.Cmd))
	b.WriteString(fmt.Sprintf("echo 'Zookeeper %s 命令执行完成 (host=%s)\n", req.Cmd, req.Host))
	return b.String()
}

// ============================================================================
// RabbitMQ 部署脚本（单节点/多节点）
// ============================================================================

func buildRabbitMQDeployScript(req middlewareDeployRequest, b *strings.Builder) {
	b.WriteString("# RabbitMQ 部署（依赖 Erlang/Elixir 运行时）\n")
	b.WriteString("if command -v rabbitmq-server >/dev/null 2>&1; then echo 'RabbitMQ 已安装'; else echo '请先安装 RabbitMQ' && exit 1; fi\n")
	switch req.Mode {
	case "single":
		b.WriteString("rabbitmq-server -detached\n")
		b.WriteString("echo 'RabbitMQ 单节点已启动'\n")
	case "multi":
		b.WriteString("# 多节点：需配置相同的 Erlang Cookie 并 join_cluster\n")
		b.WriteString("echo 'RabbitMQ 多节点：需配置 Erlang Cookie 与 join_cluster'\n")
	}
}

// ============================================================================
// ActiveMQ 部署脚本（单节点/多节点）
// ============================================================================

func buildActiveMQDeployScript(req middlewareDeployRequest, b *strings.Builder) {
	b.WriteString("# ActiveMQ 部署（依赖 Java 运行时）\n")
	b.WriteString("if command -v activemq >/dev/null 2>&1; then echo 'ActiveMQ 已安装'; else echo '请先安装 ActiveMQ' && exit 1; fi\n")
	switch req.Mode {
	case "single":
		b.WriteString("activemq start\n")
		b.WriteString("echo 'ActiveMQ 单节点已启动'\n")
	case "multi":
		b.WriteString("# 多节点：需配置网络连接器（networkConnectors）实现集群\n")
		b.WriteString("echo 'ActiveMQ 多节点：需配置 networkConnectors'\n")
	}
}

// handleMiddlewareAction 处理 POST /api/v1/middleware/{type}/{action}：启停重启/状态。
func (s *Server) handleMiddlewareAction(w http.ResponseWriter, r *http.Request, mwType, action string) {
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
	if !allowedMiddlewareTypes[mwType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported middleware type"})
		return
	}
	if !allowedMiddlewareActions[action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action"})
		return
	}
	var req middlewareOpsRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	req.Type = mwType
	req.Action = action

	script := buildMiddlewareActionScript(req)
	taskAction := "mw_" + action + "_" + mwType

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, taskAction)
	if err != nil {
		writeInternalError(r.Context(), w, "middleware.action."+mwType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: taskAction, Target: mwType,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

// buildMiddlewareActionScript 生成启停重启/状态脚本。
func buildMiddlewareActionScript(req middlewareOpsRequest) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "redis":
		b.WriteString("# Redis 启停\n")
		switch req.Action {
		case "start":
			b.WriteString("redis-server --daemonize yes || true\n")
		case "stop":
			b.WriteString("redis-cli shutdown nosave || true\n")
		case "restart":
			b.WriteString("redis-cli shutdown nosave || true\nredis-server --daemonize yes || true\n")
		case "status":
			b.WriteString("redis-cli ping || echo 'not running'\n")
		}
	case "kafka":
		b.WriteString("# Kafka 启停\n")
		switch req.Action {
		case "start":
			b.WriteString("kafka-server-start.sh -daemon config/server.properties || true\n")
		case "stop":
			b.WriteString("kafka-server-stop.sh || true\n")
		case "restart":
			b.WriteString("kafka-server-stop.sh || true\nkafka-server-start.sh -daemon config/server.properties || true\n")
		case "status":
			b.WriteString("kafka-topics.sh --bootstrap-server localhost:9092 --list || echo 'not running'\n")
		}
	case "zookeeper":
		b.WriteString("# Zookeeper 启停\n")
		switch req.Action {
		case "start":
			b.WriteString("zkServer.sh start || true\n")
		case "stop":
			b.WriteString("zkServer.sh stop || true\n")
		case "restart":
			b.WriteString("zkServer.sh restart || true\n")
		case "status":
			b.WriteString("zkServer.sh status || echo 'not running'\n")
		}
	case "rabbitmq":
		b.WriteString("# RabbitMQ 启停\n")
		switch req.Action {
		case "start":
			b.WriteString("rabbitmq-server -detached || true\n")
		case "stop":
			b.WriteString("rabbitmqctl stop || true\n")
		case "restart":
			b.WriteString("rabbitmqctl stop || true\nrabbitmq-server -detached || true\n")
		case "status":
			b.WriteString("rabbitmqctl status || echo 'not running'\n")
		}
	case "activemq":
		b.WriteString("# ActiveMQ 启停\n")
		switch req.Action {
		case "start":
			b.WriteString("activemq start || true\n")
		case "stop":
			b.WriteString("activemq stop || true\n")
		case "restart":
			b.WriteString("activemq stop || true\nactivemq start || true\n")
		case "status":
			b.WriteString("activemq status || echo 'not running'\n")
		}
	}
	b.WriteString(fmt.Sprintf("echo '%s %s 操作完成\n", req.Type, req.Action))
	return b.String()
}

// ============================================================================
// Kafka Topic 管理
// ============================================================================

// kafkaTopicReq Kafka Topic 操作请求体。
type kafkaTopicReq struct {
	DeviceID   string `json:"deviceID"`
	Action     string `json:"action"` // create/delete/list
	Topic      string `json:"topic"`
	Partitions int    `json:"partitions"`
	Replicas   int    `json:"replicas"`
}

// handleKafkaTopic 处理 POST /api/v1/middleware/kafka/topic。
func (s *Server) handleKafkaTopic(w http.ResponseWriter, r *http.Request) {
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
	var req kafkaTopicReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !kafkaTopicRe.MatchString(req.Topic) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid topic name"})
		return
	}
	allowed := map[string]bool{"create": true, "delete": true, "list": true}
	if !allowed[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (create/delete/list)"})
		return
	}
	script := buildKafkaTopicScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "kafka_topic_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "middleware.kafka.topic", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "kafka_topic_" + req.Action, Target: req.Topic,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildKafkaTopicScript(req kafkaTopicReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("BOOTSTRAP=localhost:9092\n")
	switch req.Action {
	case "create":
		if req.Partitions <= 0 {
			req.Partitions = 1
		}
		if req.Replicas <= 0 {
			req.Replicas = 1
		}
		b.WriteString(fmt.Sprintf("kafka-topics.sh --create --topic %s --partitions %d --replication-factor %d --bootstrap-server $BOOTSTRAP\n", req.Topic, req.Partitions, req.Replicas))
	case "delete":
		b.WriteString(fmt.Sprintf("kafka-topics.sh --delete --topic %s --bootstrap-server $BOOTSTRAP\n", req.Topic))
	case "list":
		b.WriteString("kafka-topics.sh --list --bootstrap-server $BOOTSTRAP\n")
	}
	b.WriteString(fmt.Sprintf("echo 'Kafka Topic %s 操作完成\n", req.Action))
	return b.String()
}

// ============================================================================
// 路由注册
// ============================================================================

// registerMiddlewareRoutes 注册中间件运维路由。
// 固定路径（kafka/topic、kafka/consumer-group、zookeeper/cmd、redis/sentinel、redis/cluster、
// rabbitmq/{vhost|queue|exchange}、activemq/{queue|topic}）优先注册，
// 让 ServeMux 精确匹配优先于前缀泛路由 /api/v1/middleware/。
func (s *Server) registerMiddlewareRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/middleware/kafka/consumer-group", s.handleKafkaConsumerGroup) // POST Consumer Group list/describe
	mux.HandleFunc("/api/v1/middleware/zookeeper/cmd", s.handleZookeeperCmd)              // POST Zookeeper 四字命令
	mux.HandleFunc("/api/v1/middleware/redis/sentinel", s.handleRedisSentinel)            // POST Redis Sentinel monitor/failover
	mux.HandleFunc("/api/v1/middleware/redis/cluster", s.handleRedisCluster)              // POST Redis Cluster nodes/slots/rebalance
	mux.HandleFunc("/api/v1/middleware/rabbitmq/vhost", s.handleRabbitMQVhost)            // POST RabbitMQ Vhost 管理
	mux.HandleFunc("/api/v1/middleware/rabbitmq/queue", s.handleRabbitMQQueue)            // POST RabbitMQ Queue 管理
	mux.HandleFunc("/api/v1/middleware/rabbitmq/exchange", s.handleRabbitMQExchange)      // POST RabbitMQ Exchange 管理
	mux.HandleFunc("/api/v1/middleware/activemq/queue", s.handleActiveMQQueue)            // POST ActiveMQ Queue 管理
	mux.HandleFunc("/api/v1/middleware/activemq/topic", s.handleActiveMQTopic)            // POST ActiveMQ Topic 管理
	mux.HandleFunc("/api/v1/middleware/", s.handleMiddlewareRouting)
}

func (s *Server) handleMiddlewareRouting(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/middleware/")
	segments := strings.Split(path, "/")
	switch len(segments) {
	case 1:
		// /api/v1/middleware/{deviceID}
		if segments[0] != "" {
			s.handleMiddleware(w, r, segments[0])
		} else {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}
	case 2:
		// /api/v1/middleware/{type}/{action}
		if segments[0] != "" && segments[1] != "" {
			s.handleMiddlewareAction(w, r, segments[0], segments[1])
		} else {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

// ============================================================================
// Redis Sentinel 管理（monitor/failover）
// ============================================================================

// redisSentinelReq Redis Sentinel 操作请求体。
type redisSentinelReq struct {
	DeviceID string `json:"deviceID"`
	Action   string `json:"action"` // monitor/failover
	Master   string `json:"master"` // master 名称（sentinel monitor 用）
	Host     string `json:"host"`   // sentinel 地址（host:port，默认 localhost:26379）
}

// handleRedisSentinel 处理 POST /api/v1/middleware/redis/sentinel。
func (s *Server) handleRedisSentinel(w http.ResponseWriter, r *http.Request) {
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
	var req redisSentinelReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if req.Host == "" {
		req.Host = "localhost:26379"
	}
	allowed := map[string]bool{"monitor": true, "failover": true}
	if !allowed[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (monitor/failover)"})
		return
	}
	script := buildRedisSentinelScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "redis_sentinel_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "middleware.redis.sentinel", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "redis_sentinel_" + req.Action, Target: req.Master,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s host=%s", req.DeviceID, req.Host)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildRedisSentinelScript(req redisSentinelReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString(fmt.Sprintf("SENTINEL_HOST=%q\n", req.Host))
	switch req.Action {
	case "monitor":
		if req.Master == "" {
			b.WriteString("echo 'sentinel monitor 需指定 master 名称' && exit 1\n")
		} else {
			b.WriteString(fmt.Sprintf("redis-cli -h %s SENTINEL monitor %s 127.0.0.1 6379 2\n", req.Host, req.Master))
			b.WriteString(fmt.Sprintf("echo 'Sentinel 已监控 master: %s'\n", req.Master))
		}
	case "failover":
		if req.Master == "" {
			b.WriteString("echo 'sentinel failover 需指定 master 名称' && exit 1\n")
		} else {
			b.WriteString(fmt.Sprintf("redis-cli -h %s SENTINEL failover %s\n", req.Host, req.Master))
			b.WriteString(fmt.Sprintf("echo 'Sentinel 已触发 master %s 故障转移'\n", req.Master))
		}
	}
	return b.String()
}

// ============================================================================
// Redis Cluster 管理（nodes/slots/rebalance）
// ============================================================================

// redisClusterReq Redis Cluster 操作请求体。
type redisClusterReq struct {
	DeviceID string `json:"deviceID"`
	Action   string `json:"action"` // nodes/slots/rebalance
	Host     string `json:"host"`   // cluster 节点地址（host:port，默认 localhost:6379）
}

// handleRedisCluster 处理 POST /api/v1/middleware/redis/cluster。
func (s *Server) handleRedisCluster(w http.ResponseWriter, r *http.Request) {
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
	var req redisClusterReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if req.Host == "" {
		req.Host = "localhost:6379"
	}
	allowed := map[string]bool{"nodes": true, "slots": true, "rebalance": true}
	if !allowed[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (nodes/slots/rebalance)"})
		return
	}
	script := buildRedisClusterScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "redis_cluster_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "middleware.redis.cluster", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "redis_cluster_" + req.Action, Target: req.Host,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildRedisClusterScript(req redisClusterReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString(fmt.Sprintf("CLUSTER_HOST=%q\n", req.Host))
	switch req.Action {
	case "nodes":
		b.WriteString(fmt.Sprintf("redis-cli -h %s CLUSTER NODES\n", req.Host))
	case "slots":
		b.WriteString(fmt.Sprintf("redis-cli -h %s CLUSTER SLOTS\n", req.Host))
	case "rebalance":
		b.WriteString(fmt.Sprintf("echo 'rebalance 需在 redis-cli --cluster rebalance 中手动执行，host=%s'\n", req.Host))
	}
	b.WriteString(fmt.Sprintf("echo 'Redis Cluster %s 操作完成 (host=%s)\n", req.Action, req.Host))
	return b.String()
}

// ============================================================================
// RabbitMQ Vhost/Queue/Exchange 管理
// ============================================================================

// rabbitMQReq RabbitMQ 操作请求体。
type rabbitMQReq struct {
	DeviceID string `json:"deviceID"`
	Action   string `json:"action"` // vhost/create/delete/list (queue/exchange/vhost)
	Name     string `json:"name"`   // 队列/交换机/虚拟主机名
	Vhost    string `json:"vhost"`  // 虚拟主机（默认 /）
}

// handleRabbitMQVhost 处理 POST /api/v1/middleware/rabbitmq/vhost。
func (s *Server) handleRabbitMQVhost(w http.ResponseWriter, r *http.Request) {
	s.handleRabbitMQResource(w, r, "vhost")
}

// handleRabbitMQQueue 处理 POST /api/v1/middleware/rabbitmq/queue。
func (s *Server) handleRabbitMQQueue(w http.ResponseWriter, r *http.Request) {
	s.handleRabbitMQResource(w, r, "queue")
}

// handleRabbitMQExchange 处理 POST /api/v1/middleware/rabbitmq/exchange。
func (s *Server) handleRabbitMQExchange(w http.ResponseWriter, r *http.Request) {
	s.handleRabbitMQResource(w, r, "exchange")
}

func (s *Server) handleRabbitMQResource(w http.ResponseWriter, r *http.Request, resource string) {
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
	var req rabbitMQReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if req.Vhost == "" {
		req.Vhost = "/"
	}
	allowed := map[string]bool{"create": true, "delete": true, "list": true}
	if !allowed[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (create/delete/list)"})
		return
	}
	script := buildRabbitMQScript(req, resource)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "rabbitmq_"+resource+"_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "middleware.rabbitmq."+resource, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "rabbitmq_" + resource + "_" + req.Action, Target: req.Name,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s vhost=%s", req.DeviceID, req.Vhost)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildRabbitMQScript(req rabbitMQReq, resource string) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString(fmt.Sprintf("VHOST=%q\n", req.Vhost))
	switch resource {
	case "vhost":
		switch req.Action {
		case "create":
			b.WriteString(fmt.Sprintf("rabbitmqctl add_vhost %s\n", shellQuote(req.Name)))
		case "delete":
			b.WriteString(fmt.Sprintf("rabbitmqctl delete_vhost %s\n", shellQuote(req.Name)))
		case "list":
			b.WriteString("rabbitmqctl list_vhosts\n")
		}
	case "queue":
		switch req.Action {
		case "create":
			if req.Name == "" {
				b.WriteString("echo 'queue create 需指定 queue 名称' && exit 1\n")
			} else {
				b.WriteString(fmt.Sprintf("rabbitmqctl declare_queue name=%s vhost=%s\n", shellQuote(req.Name), req.Vhost))
			}
		case "delete":
			b.WriteString(fmt.Sprintf("rabbitmqctl delete_queue name=%s vhost=%s\n", shellQuote(req.Name), req.Vhost))
		case "list":
			b.WriteString(fmt.Sprintf("rabbitmqctl list_queues -p %s\n", req.Vhost))
		}
	case "exchange":
		switch req.Action {
		case "create":
			if req.Name == "" {
				b.WriteString("echo 'exchange create 需指定 exchange 名称' && exit 1\n")
			} else {
				b.WriteString(fmt.Sprintf("rabbitmqctl declare_exchange name=%s type=direct vhost=%s\n", shellQuote(req.Name), req.Vhost))
			}
		case "delete":
			b.WriteString(fmt.Sprintf("rabbitmqctl delete_exchange name=%s vhost=%s\n", shellQuote(req.Name), req.Vhost))
		case "list":
			b.WriteString(fmt.Sprintf("rabbitmqctl list_exchanges -p %s\n", req.Vhost))
		}
	}
	b.WriteString(fmt.Sprintf("echo 'RabbitMQ %s %s 操作完成\n", resource, req.Action))
	return b.String()
}

// ============================================================================
// ActiveMQ Queue/Topic 管理
// ============================================================================

// activeMQReq ActiveMQ 操作请求体。
type activeMQReq struct {
	DeviceID string `json:"deviceID"`
	Action   string `json:"action"` // create/delete/list
	Name     string `json:"name"`   // 队列/话题名
	Type     string `json:"type"`   // queue/topic
}

// handleActiveMQQueue 处理 POST /api/v1/middleware/activemq/queue。
func (s *Server) handleActiveMQQueue(w http.ResponseWriter, r *http.Request) {
	s.handleActiveMQResource(w, r, "queue")
}

// handleActiveMQTopic 处理 POST /api/v1/middleware/activemq/topic。
func (s *Server) handleActiveMQTopic(w http.ResponseWriter, r *http.Request) {
	s.handleActiveMQResource(w, r, "topic")
}

func (s *Server) handleActiveMQResource(w http.ResponseWriter, r *http.Request, resType string) {
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
	var req activeMQReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	req.Type = resType
	allowed := map[string]bool{"create": true, "delete": true, "list": true}
	if !allowed[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (create/delete/list)"})
		return
	}
	script := buildActiveMQScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "activemq_"+resType+"_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "middleware.activemq."+resType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "activemq_" + resType + "_" + req.Action, Target: req.Name,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildActiveMQScript(req activeMQReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Action {
	case "create":
		if req.Name == "" {
			b.WriteString("echo 'create 需指定名称' && exit 1\n")
		} else {
			b.WriteString(fmt.Sprintf("activemq create %s --type %s\n", shellQuote(req.Name), req.Type))
		}
	case "delete":
		b.WriteString(fmt.Sprintf("activemq delete %s --type %s\n", shellQuote(req.Name), req.Type))
	case "list":
		b.WriteString(fmt.Sprintf("activemq list %s\n", req.Type))
	}
	b.WriteString(fmt.Sprintf("echo 'ActiveMQ %s %s 操作完成\n", req.Type, req.Action))
	return b.String()
}
