// Package controlplane: middleware_deploy.go 实现中间件部署预置模板与 API。
//
// 提供 10+ 个常见中间件（MySQL/Redis/Kafka/Nginx/Tomcat/Zookeeper/PostgreSQL/
// MongoDB/RabbitMQ/Elasticsearch）的部署模板，每个模板支持 docker 容器化与 systemd
// 裸机两种部署方式，通过：
//   - GET    /api/v1/middleware-templates          列出所有模板（可选 ?category= 过滤）
//   - POST   /api/v1/middleware-templates          创建新模板（CRUD）
//   - GET    /api/v1/middleware-templates/{id}     获取模板详情
//   - PUT    /api/v1/middleware-templates/{id}     更新模板（CRUD）
//   - DELETE /api/v1/middleware-templates/{id}     删除模板（CRUD）
//   - POST   /api/v1/middleware-templates/{id}/deploy 在指定 agent 上部署
//   - GET    /api/v1/middleware-instances          查询已部署实例（从任务历史推导）
//
// 设计要点（模板从内存常量改为 store 持久化，支持在线 CRUD）：
//   - 预置模板仍以内存常量 middlewareTemplates 维护（版本随代码升级），启动时
//     seedPresetMiddlewareTemplates 将其幂等写入 store（按 ID 去重，已存在不覆盖）。
//   - API 从 store 读取模板列表/详情；store 为空时回退到内存常量（向后兼容）。
//   - deploy 将 params 替换脚本占位符（{name}/{port}/...）后作为 shell task 下发，
//     复用 store.CreateTask + Audit + 事件总线 + SSE，与 os_optimize.go 同款逻辑。
//   - 租户隔离与审计复用 handleCreateTask 同款逻辑。
package controlplane

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"opsmesh/internal/controlplane/paginate"
	"strconv"
	"strings"

	"opsmesh/internal/events"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// MiddlewareTemplate 预置中间件部署模板。
// Scripts 按 deployType（"docker"/"systemd"）索引对应部署/验证/卸载脚本。
type MiddlewareTemplate struct {
	ID          string                      `json:"id"`
	Name        string                      `json:"name"`
	Category    string                      `json:"category"` // database/cache/message/web/search
	Version     string                      `json:"version"`
	Description string                      `json:"description"`
	DeployTypes []string                    `json:"deployTypes"` // ["docker","systemd"]
	Params      []MiddlewareParam           `json:"params"`
	Scripts     map[string]MiddlewareScript `json:"scripts"` // key: "docker"/"systemd"
	Risk        string                      `json:"risk"`    // low/medium/high
	Tags        []string                    `json:"tags"`
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

	// ---------------- Phase 1/2 扩展：storage/service/monitor ----------------
	// minio (storage, low) — MinIO 对象存储
	{
		ID:          "minio",
		Name:        "MinIO",
		Category:    "storage",
		Version:     "latest",
		Description: "MinIO 高性能对象存储，兼容 S3 API，适用于非结构化数据",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "minio", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "9000", Required: true, Type: "int"},
			{Name: "user", Description: "管理员用户名", Default: "minioadmin", Required: true, Type: "string"},
			{Name: "password", Description: "管理员密码", Default: "minioadmin", Required: true, Type: "string"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/minio", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:9000 -e MINIO_ROOT_USER={user} -e MINIO_ROOT_PASSWORD={password} -v {datadir}:/data --restart unless-stopped minio/minio server /data",
				Verify:    "curl -fsS http://localhost:{port}/minio/health/live",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "wget -qO /usr/local/bin/minio https://dl.min.io/server/minio/release/linux-amd64/minio && chmod +x /usr/local/bin/minio && mkdir -p /data && cat > /etc/systemd/system/minio.service <<'EOF'\n[Unit]\nDescription=MinIO Object Storage\nAfter=network.target\n[Service]\nType=simple\nExecStart=/usr/local/bin/minio server /data\nEnvironment=MINIO_ROOT_USER={user}\nEnvironment=MINIO_ROOT_PASSWORD={password}\nRestart=always\n[Install]\nWantedBy=multi-user.target\nEOF\nsystemctl daemon-reload && systemctl enable minio && systemctl start minio",
				Verify:    "systemctl is-active minio",
				Uninstall: "systemctl stop minio && systemctl disable minio && rm -f /usr/local/bin/minio /etc/systemd/system/minio.service",
			},
		},
		Risk: "low",
		Tags: []string{"storage", "s3", "object"},
	},
	// consul (service, low) — Consul 服务发现
	{
		ID:          "consul",
		Name:        "Consul",
		Category:    "service",
		Version:     "1.15",
		Description: "Consul 服务发现与配置管理，提供服务注册/健康检查/KV 存储",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "consul", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p 8500:8500 -p 8300:8300 -p 8301:8301 -p 8302:8302 --restart unless-stopped hashicorp/consul agent -dev -client=0.0.0.0",
				Verify:    "curl -fsS http://localhost:8500/v1/status/leader",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y consul && systemctl enable consul && systemctl start consul",
				Verify:    "systemctl is-active consul",
				Uninstall: "systemctl stop consul && yum remove -y consul",
			},
		},
		Risk: "low",
		Tags: []string{"service", "discovery", "consensus"},
	},
	// etcd (storage, medium) — etcd 键值存储
	{
		ID:          "etcd",
		Name:        "etcd",
		Category:    "storage",
		Version:     "3.5",
		Description: "etcd 分布式键值存储，为 Kubernetes 等提供可靠的数据存储",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "etcd", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p 2379:2379 -p 2380:2380 --restart unless-stopped gcr.io/etcd-development/etcd /usr/local/bin/etcd -name etcd0 -data-dir /data -listen-client-urls http://0.0.0.0:2379 -advertise-client-urls http://0.0.0.0:2379",
				Verify:    "docker exec {name} etcdctl endpoint health",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y etcd && systemctl enable etcd && systemctl start etcd",
				Verify:    "systemctl is-active etcd",
				Uninstall: "systemctl stop etcd && yum remove -y etcd",
			},
		},
		Risk: "medium",
		Tags: []string{"storage", "kv", "distributed"},
	},
	// prometheus (monitor, low) — Prometheus 监控
	{
		ID:          "prometheus",
		Name:        "Prometheus",
		Category:    "monitor",
		Version:     "2.45",
		Description: "Prometheus 监控与告警系统，适用于时序指标采集与存储",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "prometheus", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "9090", Required: true, Type: "int"},
			{Name: "configdir", Description: "配置目录（宿主机路径）", Default: "/etc/prometheus", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:9090 -v {configdir}:/etc/prometheus --restart unless-stopped prom/prometheus:v2.45.0",
				Verify:    "curl -fsS http://localhost:{port}/-/healthy",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "wget -qO /tmp/prom.tar.gz https://github.com/prometheus/prometheus/releases/download/v2.45.0/prometheus-2.45.0.linux-amd64.tar.gz && tar xzf /tmp/prom.tar.gz -C /opt/ && ln -sf /opt/prometheus-2.45.0.linux-amd64/prometheus /usr/local/bin/prometheus && mkdir -p {configdir} && cat > /etc/systemd/system/prometheus.service <<'EOF'\n[Unit]\nDescription=Prometheus\nAfter=network.target\n[Service]\nExecStart=/usr/local/bin/prometheus --config.file={configdir}/prometheus.yml\nRestart=always\n[Install]\nWantedBy=multi-user.target\nEOF\nsystemctl daemon-reload && systemctl enable prometheus && systemctl start prometheus",
				Verify:    "systemctl is-active prometheus",
				Uninstall: "systemctl stop prometheus && systemctl disable prometheus && rm -f /usr/local/bin/prometheus /etc/systemd/system/prometheus.service",
			},
		},
		Risk: "low",
		Tags: []string{"monitor", "metrics", "timeseries"},
	},
	// grafana (monitor, low) — Grafana 可视化
	{
		ID:          "grafana",
		Name:        "Grafana",
		Category:    "monitor",
		Version:     "10.2",
		Description: "Grafana 可视化平台，适用于指标/日志/链路面板展示与告警",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "grafana", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "3000", Required: true, Type: "int"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:3000 --restart unless-stopped grafana/grafana:10.2.0",
				Verify:    "curl -fsS http://localhost:{port}/api/health",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "cat > /etc/yum.repos.d/grafana.repo <<'EOF'\n[grafana]\nname=grafana\nbaseurl=https://packages.grafana.com/oss/rpm\nrepo_gpgcheck=1\nenabled=1\ngpgkey=https://packages.grafana.com/gpg.key\ngpgcheck=1\nEOF\nyum install -y grafana && systemctl enable grafana && systemctl start grafana",
				Verify:    "systemctl is-active grafana",
				Uninstall: "systemctl stop grafana && yum remove -y grafana",
			},
		},
		Risk: "low",
		Tags: []string{"monitor", "dashboard", "visualization"},
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

// handleMiddlewareTemplates 处理 /api/v1/middleware-templates：
//   - GET：列出所有模板（从 store 读取，store 为空回退预置；可选 category/risk 过滤）
//   - POST：创建新模板（CRUD，需 middleware:write 权限）
//
// 该路由注册在精确路径 /api/v1/middleware-templates（无尾斜杠）。
func (s *Server) handleMiddlewareTemplates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListMiddlewareTemplatesGet(w, r)
	case http.MethodPost:
		s.handleCreateMiddlewareTemplate(w, r)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListMiddlewareTemplatesGet 处理 GET /api/v1/middleware-templates：列出所有模板（从 store 读取，含回退）。
// 可选查询参数 category 过滤、risk 过滤。
func (s *Server) handleListMiddlewareTemplatesGet(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "middleware:read"); !ok {
		return
	}
	q := r.URL.Query()
	category := q.Get("category")
	risk := q.Get("risk")
	all := s.listMiddlewareTemplatesFromStore(actx.TenantID)
	out := make([]MiddlewareTemplate, 0, len(all))
	for _, t := range all {
		if category != "" && t.Category != category {
			continue
		}
		if risk != "" && t.Risk != risk {
			continue
		}
		out = append(out, t)
	}
	paginate.WriteJSON(w, http.StatusOK, out)
}

// handleCreateMiddlewareTemplate 处理 POST /api/v1/middleware-templates：创建新中间件模板（CRUD）。
// 请求体即 MiddlewareTemplate JSON；ID 为空时由 store 分配随机 ID。
// 需 middleware:write 权限；创建后审计 + 事件总线 + SSE 通知。
func (s *Server) handleCreateMiddlewareTemplate(w http.ResponseWriter, r *http.Request) {
	if err := s.verifyFederationRequest(r); err != nil {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	caller, ok := s.requireProd(w, r, "middleware:write")
	if !ok {
		return
	}
	var tpl MiddlewareTemplate
	if err := decodeJSONBody(w, r, &tpl); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if tpl.Name == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if len(tpl.Scripts) == 0 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "scripts is required"})
		return
	}
	tpl.Risk = normalizeRisk(tpl.Risk)
	st := middlewareTemplateToStore(&tpl, actx.TenantID)
	if err := s.store.SaveMiddlewareTemplate(st); err != nil {
		writeInternalError(r.Context(), w, "middleware.saveTemplate", err)
		return
	}
	saved := middlewareTemplateFromStore(s.store.GetMiddlewareTemplate(st.ID))
	userID := ""
	if caller != nil {
		userID = caller.ID
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: userID, Action: "mw_template_create", Target: st.ID, Detail: sanitizeAuditDetail("name=" + tpl.Name),
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: actx.TenantID, UserID: userID,
			Action: "mw_template_create", Target: st.ID, Detail: sanitizeAuditDetail("name=" + tpl.Name), Level: events.LevelInfo,
		})
	}
	s.publishEvent(r.Context(), "mw_template_changed", actx.TenantID, map[string]string{"templateID": st.ID, "action": "create"})
	paginate.WriteJSON(w, http.StatusCreated, saved)
}

// handleUpdateMiddlewareTemplate 处理 PUT /api/v1/middleware-templates/{id}：更新中间件模板（CRUD）。
// 需 middleware:write 权限；不存在返回 404。
func (s *Server) handleUpdateMiddlewareTemplate(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.verifyFederationRequest(r); err != nil {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	caller, ok := s.requireProd(w, r, "middleware:write")
	if !ok {
		return
	}
	var tpl MiddlewareTemplate
	if err := decodeJSONBody(w, r, &tpl); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if tpl.Name == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if len(tpl.Scripts) == 0 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "scripts is required"})
		return
	}
	existing := s.store.GetMiddlewareTemplate(id)
	if existing == nil {
		// 回退检查：若为预置模板 ID 且尚未 seed，允许 upsert。
		if preset := middlewareTemplateByID(id); preset == nil {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
			return
		}
	}
	tpl.ID = id
	tpl.Risk = normalizeRisk(tpl.Risk)
	st := middlewareTemplateToStore(&tpl, actx.TenantID)
	if err := s.store.SaveMiddlewareTemplate(st); err != nil {
		writeInternalError(r.Context(), w, "middleware.saveTemplate", err)
		return
	}
	saved := middlewareTemplateFromStore(s.store.GetMiddlewareTemplate(id))
	userID := ""
	if caller != nil {
		userID = caller.ID
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: userID, Action: "mw_template_update", Target: id, Detail: sanitizeAuditDetail("name=" + tpl.Name),
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: actx.TenantID, UserID: userID,
			Action: "mw_template_update", Target: id, Detail: sanitizeAuditDetail("name=" + tpl.Name), Level: events.LevelInfo,
		})
	}
	s.publishEvent(r.Context(), "mw_template_changed", actx.TenantID, map[string]string{"templateID": id, "action": "update"})
	paginate.WriteJSON(w, http.StatusOK, saved)
}

// handleDeleteMiddlewareTemplate 处理 DELETE /api/v1/middleware-templates/{id}：删除中间件模板（CRUD）。
// 需 middleware:write 权限；不存在返回 404；删除成功返回 204。
func (s *Server) handleDeleteMiddlewareTemplate(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	caller, ok := s.requireProd(w, r, "middleware:write")
	if !ok {
		return
	}
	existing := s.store.GetMiddlewareTemplate(id)
	if existing == nil {
		if middlewareTemplateByID(id) == nil {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
			return
		}
		// 预置模板未 seed，store 中本就不存在，直接返回 204。
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !s.store.DeleteMiddlewareTemplate(id) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
		return
	}
	userID := ""
	if caller != nil {
		userID = caller.ID
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: userID, Action: "mw_template_delete", Target: id, Detail: sanitizeAuditDetail("name=" + existing.Name),
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: actx.TenantID, UserID: userID,
			Action: "mw_template_delete", Target: id, Detail: sanitizeAuditDetail("name=" + existing.Name), Level: events.LevelInfo,
		})
	}
	s.publishEvent(r.Context(), "mw_template_changed", actx.TenantID, map[string]string{"templateID": id, "action": "delete"})
	w.WriteHeader(http.StatusNoContent)
}

// handleMiddlewareTemplateByID 处理 GET /api/v1/middleware-templates/{id}：返回模板详情（从 store 读取，含回退）。
func (s *Server) handleMiddlewareTemplateByID(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	_, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "middleware:read"); !ok {
		return
	}
	t := s.getMiddlewareTemplateByID(id)
	if t == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, t)
}

// handleDeployMiddlewareTemplate 处理 POST /api/v1/middleware-templates/{id}/deploy：
// 在指定 agent 上部署中间件。
// 请求体: { "agentID": "...", "deployType": "docker|systemd", "params": {"name":"...","port":"..."}, "tenantID": "..." }
// 实现：根据 deployType 取对应脚本，将 params 替换占位符后作为 shell task 下发，复用 store.CreateTask。
// 响应: { "task": ..., "taskID": "...", "templateID": "...", "templateName": "...", "deployType": "..." }
func (s *Server) handleDeployMiddlewareTemplate(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if err := s.verifyFederationRequest(r); err != nil {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "middleware:execute"); !ok {
		return
	}
	tpl := s.getMiddlewareTemplateByID(id)
	if tpl == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
		return
	}
	var body struct {
		AgentID    string            `json:"agentID"`
		DeployType string            `json:"deployType"`
		Params     map[string]string `json:"params"`
		TenantID   string            `json:"tenantID"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.AgentID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "agentID is required"})
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
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported deployType: " + body.DeployType})
		return
	}
	script, ok := tpl.Scripts[body.DeployType]
	if !ok {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "script not found for deployType: " + body.DeployType})
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
				paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "param required: " + p.Name})
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
	// 参数类型与语义验证（端口范围/路径/非空）。
	if err := validateMiddlewareParams(tpl.Params, body.Params); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// shell 元字符校验：占位符替换前拒绝含元字符的值，防命令注入。
	if err := validateShellSafeValues(body.Params); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// 认证防御：强制使用头中的租户 ID，忽略 body 中的 tenantID，防 body 覆盖头租户越权。
	targetTenant := actx.TenantID
	agent := s.lookupAgent(body.AgentID)
	if agent == nil || (targetTenant != "" && agent.TenantID != targetTenant) {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "agent not found or tenant mismatch"})
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
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: targetTenant,
		UserID:   actx.UserID,
		Action:   "deploy_middleware",
		Target:   task.TaskID,
		Detail:   sanitizeAuditDetail("template=" + id + " deployType=" + body.DeployType + " agent=" + body.AgentID),
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: targetTenant, UserID: actx.UserID,
			Action: "deploy_middleware", Target: task.TaskID,
			Detail: sanitizeAuditDetail("template=" + id + " deployType=" + body.DeployType + " agent=" + body.AgentID), Level: events.LevelInfo,
		})
	}
	if s.metrics != nil {
		s.metrics.SetQueueDepth(s.store.PendingDepth())
	}
	// SSE：通知前端新部署任务已创建。
	// 租户隔离：携带 targetTenant，仅同租户订阅者收到。
	// 携带 ctx 的 trace_id，使 SSE 事件与链路追踪关联。
	s.publishEvent(r.Context(), "task_status", targetTenant, map[string]string{
		"taskID":  task.TaskID,
		"status":  task.Status,
		"agentID": body.AgentID,
	})
	paginate.WriteJSON(w, http.StatusCreated, map[string]interface{}{
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
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	_, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "middleware:read"); !ok {
		return
	}
	// MVP：返回空数组。后续可从审计/任务历史推导已部署实例。
	// 保留 query 参数解析以兼容前端调用，避免后续扩展时改签名。
	_ = r.URL.Query().Get("agentID")
	_ = r.URL.Query().Get("category")
	paginate.WriteJSON(w, http.StatusOK, []interface{}{})
}

// handleMiddlewareTemplateDetail 统一分派 /api/v1/middleware-templates/{id}... 子路径：
//   - GET    /api/v1/middleware-templates/{id}：模板详情
//   - PUT    /api/v1/middleware-templates/{id}：更新模板（CRUD）
//   - DELETE /api/v1/middleware-templates/{id}：删除模板（CRUD）
//   - POST   /api/v1/middleware-templates/{id}/deploy：在指定 agent 上部署
//
// 注意：/api/v1/middleware-templates（无尾斜杠）由 handleMiddlewareTemplates 处理；
// /api/v1/middleware-templates/（带尾斜杠但无 id）此处转给 list handler 兜底。
func (s *Server) handleMiddlewareTemplateDetail(w http.ResponseWriter, r *http.Request) {
	idAndRest := strings.TrimPrefix(r.URL.Path, "/api/v1/middleware-templates/")
	if idAndRest == "" {
		// 兜底：/api/v1/middleware-templates/（带尾斜杠）转给 list handler 处理 GET/POST。
		s.handleMiddlewareTemplates(w, r)
		return
	}
	parts := strings.SplitN(idAndRest, "/", 2)
	id := parts[0]
	if id == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "template id required"})
		return
	}
	switch {
	case len(parts) == 1:
		// /api/v1/middleware-templates/{id}
		switch r.Method {
		case http.MethodGet:
			s.handleMiddlewareTemplateByID(w, r, id)
		case http.MethodPut:
			s.handleUpdateMiddlewareTemplate(w, r, id)
		case http.MethodDelete:
			s.handleDeleteMiddlewareTemplate(w, r, id)
		default:
			paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	case len(parts) == 2 && parts[1] == "deploy":
		// POST /api/v1/middleware-templates/{id}/deploy
		s.handleDeployMiddlewareTemplate(w, r, id)
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
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

// validateMiddlewareParams 校验中间件模板参数的类型与语义。
//   - int 类型：必须为整数；若参数名为 port 或以 port 结尾则校验端口范围 1-65535。
//   - string 类型：若参数名为路径类（datadir/configdir/confpath/javahome 或以 dir/path 结尾）则校验以 / 开头。
//
// validatePort/validateNonEmpty/validatePath 定义在 os_optimize.go（同包共享）。
func validateMiddlewareParams(params []MiddlewareParam, values map[string]string) error {
	for _, p := range params {
		val, ok := values[p.Name]
		if !ok || val == "" {
			continue
		}
		switch p.Type {
		case "int":
			n, err := strconv.Atoi(val)
			if err != nil {
				return fmt.Errorf("param %s must be integer, got %s", p.Name, val)
			}
			if p.Name == "port" || strings.HasSuffix(p.Name, "port") {
				if err := validatePort(n); err != nil {
					return err
				}
			}
		case "string":
			if p.Name == "datadir" || p.Name == "configdir" || p.Name == "confpath" || p.Name == "javahome" ||
				strings.HasSuffix(p.Name, "dir") || strings.HasSuffix(p.Name, "path") {
				if err := validatePath(val); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// handleUninstallMiddlewareInstance 处理 POST /api/v1/middleware-instances/{id}/uninstall：
// 卸载已部署的中间件实例。
// 请求体: { "agentID": "...", "templateID": "...", "deployType": "docker|systemd", "params": {...}, "tenantID": "..." }
// 实现：根据 templateID 取对应 deployType 的 Uninstall 脚本，占位符替换后作为 shell task 下发，
// 复用 store.CreateTask + Audit + 事件总线 + SSE，与 deploy 同款逻辑。
// 响应: { "task": ..., "taskID": "...", "instanceID": "...", "templateID": "...", "templateName": "...", "deployType": "..." }
func (s *Server) handleUninstallMiddlewareInstance(w http.ResponseWriter, r *http.Request, instanceID string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if err := s.verifyFederationRequest(r); err != nil {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "middleware:execute"); !ok {
		return
	}
	var body struct {
		AgentID    string            `json:"agentID"`
		TemplateID string            `json:"templateID"`
		DeployType string            `json:"deployType"`
		Params     map[string]string `json:"params"`
		TenantID   string            `json:"tenantID"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.AgentID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "agentID is required"})
		return
	}
	if body.TemplateID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "templateID is required"})
		return
	}
	if body.DeployType == "" {
		body.DeployType = "docker" // 默认 docker 部署
	}
	tpl := s.getMiddlewareTemplateByID(body.TemplateID)
	if tpl == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found: " + body.TemplateID})
		return
	}
	script, ok := tpl.Scripts[body.DeployType]
	if !ok {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "script not found for deployType: " + body.DeployType})
		return
	}
	if script.Uninstall == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "uninstall script not defined for template: " + body.TemplateID})
		return
	}
	// 填充默认参数（与 deploy 一致，确保占位符能被替换）。
	if body.Params == nil {
		body.Params = map[string]string{}
	}
	for _, p := range tpl.Params {
		if _, ok := body.Params[p.Name]; !ok && p.Default != "" {
			body.Params[p.Name] = p.Default
		}
	}
	// 类型语义校验 + shell 元字符校验：与 deploy 同规则，防卸载路径命令注入。
	if err := validateMiddlewareParams(tpl.Params, body.Params); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := validateShellSafeValues(body.Params); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// 认证防御：强制使用头中的租户 ID，忽略 body 中的 tenantID，防 body 覆盖头租户越权。
	targetTenant := actx.TenantID
	agent := s.lookupAgent(body.AgentID)
	if agent == nil || (targetTenant != "" && agent.TenantID != targetTenant) {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "agent not found or tenant mismatch"})
		return
	}
	// 拼接最终 command：将 Uninstall 脚本中占位符替换为 params 实际值。
	command := renderMiddlewareScript(script.Uninstall, body.Params)
	task := s.store.CreateTask(&proto.Task{
		AgentID:    body.AgentID,
		TenantID:   targetTenant,
		Type:       proto.TaskTypeShell,
		Command:    command,
		MaxRetries: s.cfg.TaskMaxRetries,
	})
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: targetTenant,
		UserID:   actx.UserID,
		Action:   "uninstall_middleware",
		Target:   task.TaskID,
		Detail:   sanitizeAuditDetail("instance=" + instanceID + " template=" + body.TemplateID + " deployType=" + body.DeployType + " agent=" + body.AgentID),
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: targetTenant, UserID: actx.UserID,
			Action: "uninstall_middleware", Target: task.TaskID,
			Detail: sanitizeAuditDetail("instance=" + instanceID + " template=" + body.TemplateID + " deployType=" + body.DeployType + " agent=" + body.AgentID), Level: events.LevelInfo,
		})
	}
	if s.metrics != nil {
		s.metrics.SetQueueDepth(s.store.PendingDepth())
	}
	// SSE：通知前端卸载任务已创建。
	// 租户隔离：携带 targetTenant，仅同租户订阅者收到。
	// 携带 ctx 的 trace_id，使 SSE 事件与链路追踪关联。
	s.publishEvent(r.Context(), "task_status", targetTenant, map[string]string{
		"taskID":  task.TaskID,
		"status":  task.Status,
		"agentID": body.AgentID,
	})
	paginate.WriteJSON(w, http.StatusCreated, map[string]interface{}{
		"task":         task,
		"taskID":       task.TaskID,
		"instanceID":   instanceID,
		"templateID":   body.TemplateID,
		"templateName": tpl.Name,
		"deployType":   body.DeployType,
	})
}

// handleMiddlewareInstanceRouting 统一分派 /api/v1/middleware-instances/{id}... 子路径：
//   - POST /api/v1/middleware-instances/{id}/uninstall：卸载实例
//
// 注意：/api/v1/middleware-instances（无尾斜杠）由 handleMiddlewareInstances 处理；
// /api/v1/middleware-instances/（带尾斜杠但无 id）此处转给 list handler 兜底。
func (s *Server) handleMiddlewareInstanceRouting(w http.ResponseWriter, r *http.Request) {
	idAndRest := strings.TrimPrefix(r.URL.Path, "/api/v1/middleware-instances/")
	if idAndRest == "" {
		// 兜底：/api/v1/middleware-instances/（带尾斜杠）转给 list handler 处理 GET。
		s.handleMiddlewareInstances(w, r)
		return
	}
	parts := strings.SplitN(idAndRest, "/", 2)
	id := parts[0]
	if id == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "instance id required"})
		return
	}
	switch {
	case len(parts) == 2 && parts[1] == "uninstall":
		// POST /api/v1/middleware-instances/{id}/uninstall
		s.handleUninstallMiddlewareInstance(w, r, id)
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

// ============================================================================
// 中间件模板 store 持久化适配（转换 + seed + 查询回退）
// ============================================================================

// middlewareTemplateToStore 将 controlplane.MiddlewareTemplate 转换为 store.MiddlewareTemplate。
// 整个 MiddlewareTemplate 序列化为 JSON 存入 Config 字段；
// store.MiddlewareTemplate 的 Name/Type/Version 冗余存储便于 SQL 过滤。
func middlewareTemplateToStore(t *MiddlewareTemplate, tenantID string) *store.MiddlewareTemplate {
	if t == nil {
		return nil
	}
	cfg, _ := json.Marshal(t)
	return &store.MiddlewareTemplate{
		ID:       t.ID,
		TenantID: tenantID,
		Name:     t.Name,
		Type:     t.Category, // Category 映射到 Type（中间件类别）
		Version:  t.Version,
		Config:   string(cfg),
	}
}

// middlewareTemplateFromStore 将 store.MiddlewareTemplate 反转换为 controlplane.MiddlewareTemplate。
// Config 为空或反序列化失败时，用 store 行的 ID/Name/Type/Version 构造最小模板（向后兼容）。
func middlewareTemplateFromStore(st *store.MiddlewareTemplate) *MiddlewareTemplate {
	if st == nil {
		return nil
	}
	if st.Config == "" {
		return &MiddlewareTemplate{ID: st.ID, Name: st.Name, Category: st.Type, Version: st.Version}
	}
	var t MiddlewareTemplate
	if err := json.Unmarshal([]byte(st.Config), &t); err != nil {
		return &MiddlewareTemplate{ID: st.ID, Name: st.Name, Category: st.Type, Version: st.Version}
	}
	// 以 store 行的 ID/Name 为准（防 Config 中过期值）。
	if st.ID != "" {
		t.ID = st.ID
	}
	if st.Name != "" {
		t.Name = st.Name
	}
	return &t
}

// seedPresetMiddlewareTemplates 启动时将预置中间件模板幂等写入 store（按 ID 去重，已存在不覆盖）。
// 保持向后兼容：store 为空时 API 回退到内存常量 middlewareTemplates。
// 预置模板归入 "default" 租户，对所有租户可见。
func (s *Server) seedPresetMiddlewareTemplates() {
	for i := range middlewareTemplates {
		tpl := &middlewareTemplates[i]
		if existing := s.store.GetMiddlewareTemplate(tpl.ID); existing != nil {
			continue // 已存在（用户可能已在线修改），不覆盖
		}
		st := middlewareTemplateToStore(tpl, "default")
		if err := s.store.SaveMiddlewareTemplate(st); err != nil {
			log.Printf("[controlplane] seed 预置中间件模板 %s 失败: %v", tpl.ID, err)
		}
	}
}

// listMiddlewareTemplatesFromStore 从 store 读取中间件模板列表（含回退）。
// 合并当前租户的模板与 default 租户的预置模板（按 ID 去重）；
// store 完全为空时回退到内存常量 middlewareTemplates（向后兼容）。
func (s *Server) listMiddlewareTemplatesFromStore(tenantID string) []MiddlewareTemplate {
	stored := s.store.ListMiddlewareTemplates(tenantID)
	if tenantID != "" && tenantID != "default" {
		stored = append(stored, s.store.ListMiddlewareTemplates("default")...)
	}
	if len(stored) == 0 {
		// 回退到内存常量（store 未初始化或为空）。
		out := make([]MiddlewareTemplate, len(middlewareTemplates))
		copy(out, middlewareTemplates)
		return out
	}
	seen := make(map[string]bool, len(stored))
	out := make([]MiddlewareTemplate, 0, len(stored))
	for _, st := range stored {
		if seen[st.ID] {
			continue
		}
		seen[st.ID] = true
		if t := middlewareTemplateFromStore(st); t != nil {
			out = append(out, *t)
		}
	}
	return out
}

// getMiddlewareTemplateByID 从 store 读取单个中间件模板（含回退）。
// store 中不存在时回退到内存常量 middlewareTemplateByID（向后兼容）。
func (s *Server) getMiddlewareTemplateByID(id string) *MiddlewareTemplate {
	if st := s.store.GetMiddlewareTemplate(id); st != nil {
		return middlewareTemplateFromStore(st)
	}
	// 回退到预置模板（store 未 seed 或为空）。
	return middlewareTemplateByID(id)
}
