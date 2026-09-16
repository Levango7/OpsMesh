// Package controlplane: middleware_template_presets.go 定义中间件部署预置模板数据。
//
// 从 middleware_deploy.go 拆分而来。middlewareTemplates 包含 10+ 个常见中间件
// （MySQL/Redis/Kafka/Nginx/Tomcat/Zookeeper/PostgreSQL/MongoDB/RabbitMQ/Elasticsearch）
// 的部署模板，每个模板支持 docker 容器化与 systemd 裸机两种部署方式。
package controlplane

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
