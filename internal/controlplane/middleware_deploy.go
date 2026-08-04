// Package controlplane: middleware_deploy.go 实现中间件部署预置模板与 API。
//
// 提供 10+ 个常见中间件（MySQL/Redis/Kafka/Nginx/Tomcat/Zookeeper/PostgreSQL/
// MongoDB/RabbitMQ/Elasticsearch）的部署模板，每个模板支持 docker 容器化与 systemd
// 裸机两种部署方式，通过：
//   - GET  /api/v1/middleware-templates          列出所有模板（可选 ?category= 过滤）
//   - GET  /api/v1/middleware-templates/{id}     获取模板详情
//   - POST /api/v1/middleware-templates/{id}/deploy 在指定 agent 上部署
//   - GET  /api/v1/middleware-instances          查询已部署实例（从任务历史推导）
//
// 设计要点：
//   - 模板为内存常量，不落库（预置最佳实践，版本随代码升级）。
//   - deploy 将 params 替换脚本占位符（{name}/{port}/...）后作为 shell task 下发，
//     复用 store.CreateTask + Audit + 事件总线 + SSE，与 os_optimize.go 同款逻辑。
//   - 租户隔离与审计复用 handleCreateTask 同款逻辑。
package controlplane

import (
	"net/http"
	"strings"

	"opsmesh/internal/authctx"
	"opsmesh/internal/events"
	"opsmesh/internal/proto"
)

// MiddlewareTemplate 预置中间件部署模板。
// Scripts 按 deployType（"docker"/"systemd"）索引对应部署/验证/卸载脚本。
type MiddlewareTemplate struct {
	ID          string                     `json:"id"`
	Name        string                     `json:"name"`
	Category    string                     `json:"category"`    // database/cache/message/web/search
	Version     string                     `json:"version"`
	Description string                     `json:"description"`
	DeployTypes []string                   `json:"deployTypes"` // ["docker","systemd"]
	Params      []MiddlewareParam          `json:"params"`
	Scripts     map[string]MiddlewareScript `json:"scripts"` // key: "docker"/"systemd"
	Risk        string                     `json:"risk"`     // low/medium/high
	Tags        []string                   `json:"tags"`
}

// MiddlewareParam 中间件部署参数定义。
type MiddlewareParam struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Default     string `json:"default"`
	Required    bool   `json:"required"`
	Type        string `json:"type"` // string/int/bool
}

// MiddlewareScript 部署脚本三元组：部署/验证/卸载。
// 脚本内可使用 {name}/{port}/{password}/... 等占位符，deploy 时由 params 替换。
type MiddlewareScript struct {
	Deploy    string `json:"deploy"`    // 部署命令
	Verify    string `json:"verify"`    // 验证/健康检查命令
	Uninstall string `json:"uninstall"` // 卸载命令
}

// middlewareTemplates 预置中间件部署模板集合。
// 每个模板对应一个常见中间件，docker 与 systemd 双部署方式并存。
var middlewareTemplates = []MiddlewareTemplate{
	// ---------------- database ----------------
	{
		ID:          "mysql",
		Name:        "MySQL",
		Category:    "database",
		Version:     "8.0",
		Description: "MySQL 关系型数据库，适用于 OLTP 与高一致性场景",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "mysql", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "3306", Required: true, Type: "int"},
			{Name: "password", Description: "root 密码", Default: "", Required: true, Type: "string"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/mysql", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:3306 -e MYSQL_ROOT_PASSWORD={password} -v {datadir}:/var/lib/mysql --restart unless-stopped mysql:8.0",
				Verify:    "docker exec {name} mysqladmin ping -h localhost -u root -p{password}",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y mysql-server && systemctl enable mysqld && systemctl start mysqld",
				Verify:    "systemctl is-active mysqld",
				Uninstall: "systemctl stop mysqld && yum remove -y mysql-server",
			},
		},
		Risk: "medium",
		Tags: []string{"database", "sql", "oltp"},
	},
	{
		ID:          "postgresql",
		Name:        "PostgreSQL",
		Category:    "database",
		Version:     "16",
		Description: "PostgreSQL 高级关系型数据库，支持丰富 SQL 类型与扩展",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "postgres", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "5432", Required: true, Type: "int"},
			{Name: "password", Description: "postgres 用户密码", Default: "", Required: true, Type: "string"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/postgres", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:5432 -e POSTGRES_PASSWORD={password} -v {datadir}:/var/lib/postgresql/data --restart unless-stopped postgres:16",
				Verify:    "docker exec {name} pg_isready -U postgres",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y postgresql-server && postgresql-setup --initdb && systemctl enable postgresql && systemctl start postgresql",
				Verify:    "systemctl is-active postgresql",
				Uninstall: "systemctl stop postgresql && yum remove -y postgresql-server",
			},
		},
		Risk: "medium",
		Tags: []string{"database", "sql", "acid"},
	},
	{
		ID:          "mongodb",
		Name:        "MongoDB",
		Category:    "database",
		Version:     "7.0",
		Description: "MongoDB 文档型 NoSQL 数据库，适用于灵活 schema 与水平扩展场景",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "mongodb", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "27017", Required: true, Type: "int"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/mongodb", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:27017 -v {datadir}:/data/db --restart unless-stopped mongo:7.0",
				Verify:    "docker exec {name} mongosh --eval 'db.runCommand({ ping: 1 })'",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "cat > /etc/yum.repos.d/mongodb.repo <<'EOF'\n[mongodb-org-7.0]\nname=MongoDB Repository\nbaseurl=https://repo.mongodb.org/yum/redhat/$releasever/mongodb-org/7.0/x86_64/\ngpgcheck=1\nenabled=1\ngpgkey=https://www.mongodb.org/static/pgp/server-7.0.asc\nEOF\nyum install -y mongodb-org && systemctl enable mongod && systemctl start mongod",
				Verify:    "systemctl is-active mongod",
				Uninstall: "systemctl stop mongod && yum remove -y mongodb-org",
			},
		},
		Risk: "medium",
		Tags: []string{"database", "nosql", "document"},
	},

	// ---------------- cache ----------------
	{
		ID:          "redis",
		Name:        "Redis",
		Category:    "cache",
		Version:     "7.2",
		Description: "Redis 内存键值存储，适用于缓存/会话/排行榜/发布订阅",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "redis", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "6379", Required: true, Type: "int"},
			{Name: "password", Description: "访问密码（空=无密码）", Default: "", Required: false, Type: "string"},
			{Name: "maxmemory", Description: "最大内存（如 512mb/1gb）", Default: "512mb", Required: false, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:6379 -e REDIS_PASSWORD={password} -e REDIS_MAXMEMORY={maxmemory} --restart unless-stopped redis:7.2 redis-server --requirepass {password} --maxmemory {maxmemory}",
				Verify:    "docker exec {name} redis-cli -a {password} ping",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y redis && sed -i 's/^# requirepass .*/requirepass {password}/' /etc/redis/redis.conf && systemctl enable redis && systemctl start redis",
				Verify:    "systemctl is-active redis",
				Uninstall: "systemctl stop redis && yum remove -y redis",
			},
		},
		Risk: "low",
		Tags: []string{"cache", "kv", "in-memory"},
	},

	// ---------------- message ----------------
	{
		ID:          "kafka",
		Name:        "Kafka",
		Category:    "message",
		Version:     "3.7",
		Description: "Kafka 分布式消息队列，适用于高吞吐流式数据与事件驱动架构",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "kafka", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "9092", Required: true, Type: "int"},
			{Name: "zookeeper", Description: "Zookeeper 连接地址", Default: "localhost:2181", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:9092 -e KAFKA_ZOOKEEPER_CONNECT={zookeeper} -e KAFKA_ADVERTISED_LISTENERS=PLAINTEXT://localhost:{port} --restart unless-stopped confluentinc/cp-kafka:7.6.0",
				Verify:    "docker exec {name} kafka-topics --bootstrap-server localhost:{port} --list",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "cat > /etc/yum.repos.d/kafka.repo <<'EOF'\n[kafka]\nname=Kafka\nbaseurl=https://packages.confluent.io/rpm/7.6\nenabled=1\ngpgcheck=0\nEOF\nyum install -y confluent-kafka && systemctl enable kafka && systemctl start kafka",
				Verify:    "systemctl is-active kafka",
				Uninstall: "systemctl stop kafka && yum remove -y confluent-kafka",
			},
		},
		Risk: "medium",
		Tags: []string{"message", "queue", "stream"},
	},
	{
		ID:          "zookeeper",
		Name:        "Zookeeper",
		Category:    "message",
		Version:     "3.9",
		Description: "Zookeeper 分布式协调服务，提供配置/命名/同步/组服务",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "zookeeper", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "2181", Required: true, Type: "int"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/zookeeper", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:2181 -v {datadir}:/data --restart unless-stopped zookeeper:3.9",
				Verify:    "docker exec {name} zkServer.sh status",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y zookeeper && systemctl enable zookeeper && systemctl start zookeeper",
				Verify:    "systemctl is-active zookeeper",
				Uninstall: "systemctl stop zookeeper && yum remove -y zookeeper",
			},
		},
		Risk: "low",
		Tags: []string{"message", "coordination", "consensus"},
	},
	{
		ID:          "rabbitmq",
		Name:        "RabbitMQ",
		Category:    "message",
		Version:     "3.13",
		Description: "RabbitMQ AMQP 消息代理，适用于可靠投递与复杂路由场景",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "rabbitmq", Required: true, Type: "string"},
			{Name: "port", Description: "AMQP 监听端口", Default: "5672", Required: true, Type: "int"},
			{Name: "mgmtport", Description: "管理界面端口", Default: "15672", Required: true, Type: "int"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:5672 -p {mgmtport}:15672 --restart unless-stopped rabbitmq:3.13-management",
				Verify:    "docker exec {name} rabbitmqctl status",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y rabbitmq-server && rabbitmq-plugins enable rabbitmq_management && systemctl enable rabbitmq-server && systemctl start rabbitmq-server",
				Verify:    "systemctl is-active rabbitmq-server",
				Uninstall: "systemctl stop rabbitmq-server && yum remove -y rabbitmq-server",
			},
		},
		Risk: "low",
		Tags: []string{"message", "amqp", "broker"},
	},

	// ---------------- web ----------------
	{
		ID:          "nginx",
		Name:        "Nginx",
		Category:    "web",
		Version:     "1.25",
		Description: "Nginx 高性能 Web 服务器/反向代理，适用于静态资源与负载均衡",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "nginx", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "80", Required: true, Type: "int"},
			{Name: "confpath", Description: "配置文件路径（宿主机）", Default: "/etc/nginx/nginx.conf", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:80 -v {confpath}:/etc/nginx/nginx.conf:ro --restart unless-stopped nginx:1.25",
				Verify:    "docker exec {name} nginx -t",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y nginx && systemctl enable nginx && systemctl start nginx",
				Verify:    "systemctl is-active nginx",
				Uninstall: "systemctl stop nginx && yum remove -y nginx",
			},
		},
		Risk: "low",
		Tags: []string{"web", "proxy", "load-balance"},
	},
	{
		ID:          "tomcat",
		Name:        "Tomcat",
		Category:    "web",
		Version:     "10.1",
		Description: "Tomcat Java Servlet 容器，适用于 Java Web 应用部署",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "tomcat", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "8080", Required: true, Type: "int"},
			{Name: "javahome", Description: "JAVA_HOME 路径", Default: "/usr/lib/jvm/java-17", Required: false, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:8080 -e JAVA_HOME={javahome} --restart unless-stopped tomcat:10.1",
				Verify:    "docker exec {name} catalina.sh status",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y tomcat && systemctl enable tomcat && systemctl start tomcat",
				Verify:    "systemctl is-active tomcat",
				Uninstall: "systemctl stop tomcat && yum remove -y tomcat",
			},
		},
		Risk: "low",
		Tags: []string{"web", "servlet", "java"},
	},

	// ---------------- search ----------------
	{
		ID:          "elasticsearch",
		Name:        "Elasticsearch",
		Category:    "search",
		Version:     "8.13",
		Description: "Elasticsearch 分布式搜索与分析引擎，适用于全文检索与日志分析",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "elasticsearch", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "9200", Required: true, Type: "int"},
			{Name: "memlimit", Description: "JVM 堆内存限制（如 512m/1g）", Default: "512m", Required: false, Type: "string"},
			{Name: "cluster", Description: "集群名称", Default: "es-cluster", Required: false, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:9200 -e ES_JAVA_OPTS=\"-Xms{memlimit} -Xmx{memlimit}\" -e cluster.name={cluster} -e discovery.type=single-node --restart unless-stopped elasticsearch:8.13",
				Verify:    "curl -fsS http://localhost:{port}/_cluster/health | grep -q '\"status\"'",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "cat > /etc/yum.repos.d/elasticsearch.repo <<'EOF'\n[elasticsearch]\nname=Elasticsearch\nbaseurl=https://artifacts.elastic.co/packages/8.x/yum\ngpgcheck=1\ngpgkey=https://artifacts.elastic.co/GPG-KEY-elasticsearch\nenabled=1\nEOF\nyum install -y elasticsearch && systemctl enable elasticsearch && systemctl start elasticsearch",
				Verify:    "systemctl is-active elasticsearch",
				Uninstall: "systemctl stop elasticsearch && yum remove -y elasticsearch",
			},
		},
		Risk: "medium",
		Tags: []string{"search", "elk", "fulltext"},
	},
}

// middlewareTemplateByID 按 ID 查找预置中间件模板，未找到返回 nil。
func middlewareTemplateByID(id string) *MiddlewareTemplate {
	for i := range middlewareTemplates {
		if middlewareTemplates[i].ID == id {
			return &middlewareTemplates[i]
		}
	}
	return nil
}

// handleMiddlewareTemplates 处理 GET /api/v1/middleware-templates：列出所有预置中间件模板。
// 可选查询参数 category 过滤、risk 过滤。
// 该路由注册在精确路径 /api/v1/middleware-templates（无尾斜杠）。
func (s *Server) handleMiddlewareTemplates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	if s.requireAuth && actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return
	}
	q := r.URL.Query()
	category := q.Get("category")
	risk := q.Get("risk")
	out := make([]MiddlewareTemplate, 0, len(middlewareTemplates))
	for _, t := range middlewareTemplates {
		if category != "" && t.Category != category {
			continue
		}
		if risk != "" && t.Risk != risk {
			continue
		}
		out = append(out, t)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleMiddlewareTemplateByID 处理 GET /api/v1/middleware-templates/{id}：返回模板详情。
func (s *Server) handleMiddlewareTemplateByID(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	if s.requireAuth && actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return
	}
	t := middlewareTemplateByID(id)
	if t == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// handleDeployMiddlewareTemplate 处理 POST /api/v1/middleware-templates/{id}/deploy：
// 在指定 agent 上部署中间件。
// 请求体: { "agentID": "...", "deployType": "docker|systemd", "params": {"name":"...","port":"..."}, "tenantID": "..." }
// 实现：根据 deployType 取对应脚本，将 params 替换占位符后作为 shell task 下发，复用 store.CreateTask。
// 响应: { "task": ..., "taskID": "...", "templateID": "...", "templateName": "...", "deployType": "..." }
func (s *Server) handleDeployMiddlewareTemplate(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.verifyFederationRequest(r); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	if s.requireAuth && actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return
	}
	tpl := middlewareTemplateByID(id)
	if tpl == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
		return
	}
	var body struct {
		AgentID    string            `json:"agentID"`
		DeployType string            `json:"deployType"`
		Params     map[string]string `json:"params"`
		TenantID   string            `json:"tenantID"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.AgentID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "agentID is required"})
		return
	}
	if body.DeployType == "" {
		body.DeployType = "docker" // 默认 docker 部署
	}
	// 校验 deployType 是否在模板支持列表内。
	supported := false
	for _, dt := range tpl.DeployTypes {
		if dt == body.DeployType {
			supported = true
			break
		}
	}
	if !supported {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported deployType: " + body.DeployType})
		return
	}
	script, ok := tpl.Scripts[body.DeployType]
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "script not found for deployType: " + body.DeployType})
		return
	}
	// 校验必填参数。
	for _, p := range tpl.Params {
		if p.Required {
			val, ok := body.Params[p.Name]
			if !ok || val == "" {
				// 未提供则尝试用默认值。
				if p.Default != "" {
					if body.Params == nil {
						body.Params = map[string]string{}
					}
					body.Params[p.Name] = p.Default
					continue
				}
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "param required: " + p.Name})
				return
			}
		}
	}
	// 未提供的可选参数填充默认值。
	for _, p := range tpl.Params {
		if _, ok := body.Params[p.Name]; !ok && p.Default != "" {
			if body.Params == nil {
				body.Params = map[string]string{}
			}
			body.Params[p.Name] = p.Default
		}
	}
	targetTenant := body.TenantID
	if targetTenant == "" {
		targetTenant = actx.TenantID
	}
	agent := s.lookupAgent(body.AgentID)
	if agent == nil || (targetTenant != "" && agent.TenantID != targetTenant) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "agent not found or tenant mismatch"})
		return
	}
	// 拼接最终 command：将脚本中 {name}/{port}/... 占位符替换为 params 实际值。
	command := renderMiddlewareScript(script.Deploy, body.Params)
	task := s.store.CreateTask(&proto.Task{
		AgentID:    body.AgentID,
		TenantID:   targetTenant,
		Type:       proto.TaskTypeShell,
		Command:    command,
		MaxRetries: s.cfg.TaskMaxRetries,
	})
	s.store.Audit(&proto.AuditEvent{
		TenantID: targetTenant,
		UserID:   actx.UserID,
		Action:   "deploy_middleware",
		Target:   task.TaskID,
		Detail:   "template=" + id + " deployType=" + body.DeployType + " agent=" + body.AgentID,
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: targetTenant, UserID: actx.UserID,
			Action: "deploy_middleware", Target: task.TaskID,
			Detail: "template=" + id + " deployType=" + body.DeployType + " agent=" + body.AgentID, Level: events.LevelInfo,
		})
	}
	if s.metrics != nil {
		s.metrics.SetQueueDepth(s.store.PendingDepth())
	}
	s.publishEvent("task_status", map[string]string{
		"taskID":  task.TaskID,
		"status":  task.Status,
		"agentID": body.AgentID,
	})
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"task":         task,
		"taskID":       task.TaskID,
		"templateID":   id,
		"templateName": tpl.Name,
		"deployType":   body.DeployType,
	})
}

// handleMiddlewareInstances 处理 GET /api/v1/middleware-instances：查询已部署中间件实例。
// MVP 实现：返回空数组（后续可从任务历史按 action=deploy_middleware 推导实例清单）。
// 可选查询参数 agentID 过滤、category 过滤。
func (s *Server) handleMiddlewareInstances(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	if s.requireAuth && actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return
	}
	// MVP：返回空数组。后续可从审计/任务历史推导已部署实例。
	// 保留 query 参数解析以兼容前端调用，避免后续扩展时改签名。
	_ = r.URL.Query().Get("agentID")
	_ = r.URL.Query().Get("category")
	writeJSON(w, http.StatusOK, []interface{}{})
}

// handleMiddlewareTemplateDetail 统一分派 /api/v1/middleware-templates/{id}... 子路径：
//   - GET  /api/v1/middleware-templates/{id}：模板详情
//   - POST /api/v1/middleware-templates/{id}/deploy：在指定 agent 上部署
//
// 注意：/api/v1/middleware-templates（无尾斜杠）由 handleMiddlewareTemplates 处理；
// /api/v1/middleware-templates/（带尾斜杠但无 id）此处转给 list handler 兜底。
func (s *Server) handleMiddlewareTemplateDetail(w http.ResponseWriter, r *http.Request) {
	idAndRest := strings.TrimPrefix(r.URL.Path, "/api/v1/middleware-templates/")
	if idAndRest == "" {
		// 兜底：/api/v1/middleware-templates/（带尾斜杠）转给 list handler 处理 GET。
		s.handleMiddlewareTemplates(w, r)
		return
	}
	parts := strings.SplitN(idAndRest, "/", 2)
	id := parts[0]
	if id == "" {
		http.Error(w, "template id required", http.StatusBadRequest)
		return
	}
	switch {
	case len(parts) == 1:
		// GET /api/v1/middleware-templates/{id}
		s.handleMiddlewareTemplateByID(w, r, id)
	case len(parts) == 2 && parts[1] == "deploy":
		// POST /api/v1/middleware-templates/{id}/deploy
		s.handleDeployMiddlewareTemplate(w, r, id)
	default:
		http.NotFound(w, r)
	}
}

// renderMiddlewareScript 将脚本中的 {name}/{port}/{password}/... 占位符替换为 params 实际值。
// 占位符语法：{key}，未提供 key 时保留原占位符（便于排查）。
func renderMiddlewareScript(script string, params map[string]string) string {
	out := script
	for k, v := range params {
		out = strings.ReplaceAll(out, "{"+k+"}", v)
	}
	return out
}