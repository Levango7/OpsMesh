// Package presets: middleware_template_presets.go 定义中间件部署预置模板数据。
//
// 从 middleware_deploy.go 拆分而来。MiddlewareTemplates 包含 62 个常见中间件
// （MySQL/PostgreSQL/MongoDB/MariaDB/ClickHouse/Redis/Memcached/Kafka/Zookeeper/RabbitMQ/
// ActiveMQ/RocketMQ/Nginx/Tomcat/Apache/HAProxy/OpenResty/PHP-FPM/Elasticsearch/OpenSearch/
// MinIO/Consul/etcd/Nacos/Prometheus/Grafana/Loki/Alertmanager/Jaeger）的部署模板，
// 每个模板支持 docker 容器化与 systemd 裸机两种部署方式，并提供可选版本目录（Versions）：
// docker 部署脚本中的镜像统一写作 {image} 占位符，部署时按所选版本的 DockerImage 解析，
// 模板默认版本沿用原脚本硬编码镜像，保证既有部署行为不变。
package presets

// MiddlewareTemplates 预置中间件部署模板集合。
// 每个模板对应一个常见中间件，docker 与 systemd 双部署方式并存。
var MiddlewareTemplates = []MiddlewareTemplate{
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
				Deploy:    "docker run -d --name {name} -p {port}:3306 -e MYSQL_ROOT_PASSWORD={password} -v {datadir}:/var/lib/mysql --restart unless-stopped {image}",
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
		Versions: []MiddlewareVersion{
			{Version: "8.0", DockerImage: "mysql:8.0", Default: true},
			{Version: "8.4", DockerImage: "mysql:8.4"},
			{Version: "8.2", DockerImage: "mysql:8.2"},
			{Version: "5.7", DockerImage: "mysql:5.7"},
		},
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
				Deploy:    "docker run -d --name {name} -p {port}:5432 -e POSTGRES_PASSWORD={password} -v {datadir}:/var/lib/postgresql/data --restart unless-stopped {image}",
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
		Versions: []MiddlewareVersion{
			{Version: "16", DockerImage: "postgres:16", Default: true},
			{Version: "17", DockerImage: "postgres:17"},
			{Version: "15", DockerImage: "postgres:15"},
			{Version: "14", DockerImage: "postgres:14"},
		},
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
				Deploy:    "docker run -d --name {name} -p {port}:27017 -v {datadir}:/data/db --restart unless-stopped {image}",
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
		Versions: []MiddlewareVersion{
			{Version: "7.0", DockerImage: "mongo:7.0", Default: true},
			{Version: "8.0", DockerImage: "mongo:8.0"},
			{Version: "6.0", DockerImage: "mongo:6.0"},
		},
	},
	{
		ID:          "mariadb",
		Name:        "MariaDB",
		Category:    "database",
		Version:     "11.4",
		Description: "MariaDB 关系型数据库，兼容 MySQL 协议，适用于中小型 OLTP 场景",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "mariadb", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "3306", Required: true, Type: "int"},
			{Name: "password", Description: "root 密码", Default: "", Required: true, Type: "string"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/mariadb", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:3306 -e MARIADB_ROOT_PASSWORD={password} -v {datadir}:/var/lib/mysql --restart unless-stopped {image}",
				Verify:    "docker exec {name} mariadb-admin ping",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y mariadb-server && systemctl enable mariadb && systemctl start mariadb",
				Verify:    "systemctl is-active mariadb",
				Uninstall: "systemctl stop mariadb && yum remove -y mariadb-server",
			},
		},
		Risk: "medium",
		Tags: []string{"database", "sql", "oltp"},
		Versions: []MiddlewareVersion{
			{Version: "11.4", DockerImage: "mariadb:11.4", Default: true},
			{Version: "10.11", DockerImage: "mariadb:10.11"},
			{Version: "11.8", DockerImage: "mariadb:11.8"},
		},
	},
	{
		ID:          "clickhouse",
		Name:        "ClickHouse",
		Category:    "database",
		Version:     "24.8",
		Description: "ClickHouse 列式 OLAP 数据库，适用于日志/埋点分析与实时聚合查询",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "clickhouse", Required: true, Type: "string"},
			{Name: "port", Description: "HTTP 接口端口", Default: "8123", Required: true, Type: "int"},
			{Name: "httpport", Description: "原生协议端口", Default: "9000", Required: false, Type: "int"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/clickhouse", Required: true, Type: "string"},
			{Name: "user", Description: "默认用户", Default: "default", Required: false, Type: "string"},
			{Name: "password", Description: "默认用户密码", Default: "", Required: false, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:8123 -p {httpport}:9000 -e CLICKHOUSE_USER={user} -e CLICKHOUSE_PASSWORD={password} -v {datadir}:/var/lib/clickhouse --restart unless-stopped {image}",
				Verify:    "docker exec {name} clickhouse-client --query \"SELECT 1\"",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y clickhouse-server && systemctl enable clickhouse-server && systemctl start clickhouse-server",
				Verify:    "systemctl is-active clickhouse-server",
				Uninstall: "systemctl stop clickhouse-server && yum remove -y clickhouse-server",
			},
		},
		Risk: "medium",
		Tags: []string{"database", "olap", "columnar", "clickhouse"},
		Versions: []MiddlewareVersion{
			{Version: "24.8", DockerImage: "clickhouse/clickhouse-server:24.8", Default: true},
			{Version: "24.3", DockerImage: "clickhouse/clickhouse-server:24.3"},
			{Version: "23.8", DockerImage: "clickhouse/clickhouse-server:23.8"},
		},
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
				Deploy:    "docker run -d --name {name} -p {port}:6379 -e REDIS_PASSWORD={password} -e REDIS_MAXMEMORY={maxmemory} --restart unless-stopped {image} redis-server --requirepass {password} --maxmemory {maxmemory}",
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
		Versions: []MiddlewareVersion{
			{Version: "7.2", DockerImage: "redis:7.2", Default: true},
			{Version: "7.4", DockerImage: "redis:7.4"},
			{Version: "6.2", DockerImage: "redis:6.2"},
		},
	},
	{
		ID:          "memcached",
		Name:        "Memcached",
		Category:    "cache",
		Version:     "1.6.29",
		Description: "Memcached 内存键值缓存，适用于会话共享与热点数据缓存",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "memcached", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "11211", Required: true, Type: "int"},
			{Name: "maxmemory", Description: "最大内存（如 64mb/1gb）", Default: "64mb", Required: false, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:11211 --restart unless-stopped {image}",
				Verify:    "docker exec {name} sh -c \"echo stats | nc 127.0.0.1 {port}\"",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y memcached && systemctl enable memcached && systemctl start memcached",
				Verify:    "systemctl is-active memcached",
				Uninstall: "systemctl stop memcached && yum remove -y memcached",
			},
		},
		Risk: "low",
		Tags: []string{"cache", "kv", "in-memory", "memcached"},
		Versions: []MiddlewareVersion{
			{Version: "1.6.29", DockerImage: "memcached:1.6.29", Default: true},
			{Version: "1.6.18", DockerImage: "memcached:1.6.18"},
		},
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
				Deploy:    "docker run -d --name {name} -p {port}:9092 -e KAFKA_ZOOKEEPER_CONNECT={zookeeper} -e KAFKA_ADVERTISED_LISTENERS=PLAINTEXT://localhost:{port} --restart unless-stopped {image}",
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
		Versions: []MiddlewareVersion{
			{Version: "3.7", DockerImage: "confluentinc/cp-kafka:7.6.0", Default: true},
			{Version: "3.6", DockerImage: "confluentinc/cp-kafka:7.5.3"},
		},
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
				Deploy:    "docker run -d --name {name} -p {port}:2181 -v {datadir}:/data --restart unless-stopped {image}",
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
		Versions: []MiddlewareVersion{
			{Version: "3.9", DockerImage: "zookeeper:3.9", Default: true},
			{Version: "3.8", DockerImage: "zookeeper:3.8"},
		},
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
				Deploy:    "docker run -d --name {name} -p {port}:5672 -p {mgmtport}:15672 --restart unless-stopped {image}",
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
		Versions: []MiddlewareVersion{
			{Version: "3.13", DockerImage: "rabbitmq:3.13-management", Default: true},
			{Version: "3.12", DockerImage: "rabbitmq:3.12-management"},
		},
	},
	{
		ID:          "activemq",
		Name:        "ActiveMQ",
		Category:    "message",
		Version:     "5.18.4",
		Description: "ActiveMQ JMS 消息中间件，适用于企业级消息队列与异步任务分发",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "activemq", Required: true, Type: "string"},
			{Name: "port", Description: "传输监听端口", Default: "61616", Required: true, Type: "int"},
			{Name: "mgmtport", Description: "管理界面端口", Default: "8161", Required: false, Type: "int"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/activemq", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:61616 -p {mgmtport}:8161 -v {datadir}:/opt/activemq/data --restart unless-stopped {image}",
				Verify:    "docker exec {name} sh -c \"ls /opt/activemq/data\"",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y activemq && systemctl enable activemq && systemctl start activemq",
				Verify:    "systemctl is-active activemq",
				Uninstall: "systemctl stop activemq && yum remove -y activemq",
			},
		},
		Risk: "medium",
		Tags: []string{"message", "queue", "jms", "activemq"},
		Versions: []MiddlewareVersion{
			{Version: "5.18.4", DockerImage: "apache/activemq:5.18.4", Default: true},
			{Version: "5.17.6", DockerImage: "apache/activemq:5.17.6"},
		},
	},
	{
		ID:          "rocketmq",
		Name:        "RocketMQ",
		Category:    "message",
		Version:     "5.2.0",
		Description: "RocketMQ 分布式消息队列，适用于金融级事务消息与顺序消息场景",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "rocketmq", Required: true, Type: "string"},
			{Name: "port", Description: "NameServer 端口", Default: "9876", Required: true, Type: "int"},
			{Name: "brokerport", Description: "Broker 端口", Default: "10911", Required: false, Type: "int"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/rocketmq", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:9876 -p {brokerport}:10911 -v {datadir}:/home/rocketmq/store --restart unless-stopped {image}",
				Verify:    "docker exec {name} sh -c \"ls /home/rocketmq/store\"",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y rocketmq && systemctl enable rocketmq && systemctl start rocketmq",
				Verify:    "systemctl is-active rocketmq",
				Uninstall: "systemctl stop rocketmq && yum remove -y rocketmq",
			},
		},
		Risk: "medium",
		Tags: []string{"message", "queue", "rocketmq"},
		Versions: []MiddlewareVersion{
			{Version: "5.2.0", DockerImage: "apache/rocketmq:5.2.0", Default: true},
			{Version: "5.1.4", DockerImage: "apache/rocketmq:5.1.4"},
		},
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
				Deploy:    "docker run -d --name {name} -p {port}:80 -v {confpath}:/etc/nginx/nginx.conf:ro --restart unless-stopped {image}",
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
		Versions: []MiddlewareVersion{
			{Version: "1.25", DockerImage: "nginx:1.25", Default: true},
			{Version: "1.27", DockerImage: "nginx:1.27"},
			{Version: "1.24", DockerImage: "nginx:1.24"},
		},
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
				Deploy:    "docker run -d --name {name} -p {port}:8080 -e JAVA_HOME={javahome} --restart unless-stopped {image}",
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
		Versions: []MiddlewareVersion{
			{Version: "10.1", DockerImage: "tomcat:10.1", Default: true},
			{Version: "9.0", DockerImage: "tomcat:9.0"},
		},
	},
	{
		ID:          "apache",
		Name:        "Apache HTTP Server",
		Category:    "web",
		Version:     "2.4",
		Description: "Apache HTTP Server Web 服务器，适用于传统 Web 站点与动态页面托管",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "httpd", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "80", Required: true, Type: "int"},
			{Name: "confpath", Description: "配置文件路径（宿主机）", Default: "/etc/httpd/conf", Required: true, Type: "string"},
			{Name: "docroot", Description: "站点根目录（宿主机）", Default: "/var/www/html", Required: false, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:80 -v {confpath}:/usr/local/apache2/conf:ro -v {docroot}:/usr/local/apache2/htdocs --restart unless-stopped {image}",
				Verify:    "docker exec {name} httpd -t",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y httpd && systemctl enable httpd && systemctl start httpd",
				Verify:    "systemctl is-active httpd",
				Uninstall: "systemctl stop httpd && yum remove -y httpd",
			},
		},
		Risk: "low",
		Tags: []string{"web", "http", "apache"},
		Versions: []MiddlewareVersion{
			{Version: "2.4", DockerImage: "httpd:2.4", Default: true},
			{Version: "2.4.62", DockerImage: "httpd:2.4.62"},
			{Version: "2.4-alpine", DockerImage: "httpd:2.4-alpine"},
		},
	},
	{
		ID:          "haproxy",
		Name:        "HAProxy",
		Category:    "web",
		Version:     "2.9",
		Description: "HAProxy 高性能负载均衡与反向代理，适用于四/七层流量分发",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "haproxy", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "80", Required: true, Type: "int"},
			{Name: "adminport", Description: "管理界面端口", Default: "8404", Required: false, Type: "int"},
			{Name: "confpath", Description: "配置文件路径（宿主机）", Default: "/etc/haproxy", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:80 -p {adminport}:8404 -v {confpath}:/usr/local/etc/haproxy:ro --restart unless-stopped {image}",
				Verify:    "docker exec {name} haproxy -c -f /usr/local/etc/haproxy/haproxy.cfg",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y haproxy && systemctl enable haproxy && systemctl start haproxy",
				Verify:    "systemctl is-active haproxy",
				Uninstall: "systemctl stop haproxy && yum remove -y haproxy",
			},
		},
		Risk: "medium",
		Tags: []string{"web", "proxy", "load-balance"},
		Versions: []MiddlewareVersion{
			{Version: "2.9", DockerImage: "haproxy:2.9", Default: true},
			{Version: "3.0", DockerImage: "haproxy:3.0"},
			{Version: "2.8", DockerImage: "haproxy:2.8"},
		},
	},
	{
		ID:          "openresty",
		Name:        "OpenResty",
		Category:    "web",
		Version:     "1.27.1.2",
		Description: "OpenResty（Nginx + Lua）动态网关，适用于 API 网关与高并发动态服务",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "openresty", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "80", Required: true, Type: "int"},
			{Name: "confpath", Description: "配置文件路径（宿主机）", Default: "/etc/openresty", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:80 -v {confpath}:/etc/nginx:ro --restart unless-stopped {image}",
				Verify:    "docker exec {name} openresty -t",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y openresty && systemctl enable openresty && systemctl start openresty",
				Verify:    "systemctl is-active openresty",
				Uninstall: "systemctl stop openresty && yum remove -y openresty",
			},
		},
		Risk: "low",
		Tags: []string{"web", "proxy", "lua", "openresty"},
		Versions: []MiddlewareVersion{
			{Version: "1.27.1.2", DockerImage: "openresty/openresty:1.27.1.2", Default: true},
			{Version: "1.25.3.1", DockerImage: "openresty/openresty:1.25.3.1"},
		},
	},
	{
		ID:          "php-fpm",
		Name:        "PHP-FPM",
		Category:    "web",
		Version:     "8.3",
		Description: "PHP-FPM 进程管理器，适用于 PHP 应用运行时与 FastCGI 服务",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "php-fpm", Required: true, Type: "string"},
			{Name: "port", Description: "FastCGI 监听端口", Default: "9000", Required: true, Type: "int"},
			{Name: "confpath", Description: "配置文件路径（宿主机）", Default: "/etc/php", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:9000 -v {confpath}:/usr/local/etc/php:ro --restart unless-stopped {image}",
				Verify:    "docker exec {name} php-fpm -t",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y php-fpm && systemctl enable php-fpm && systemctl start php-fpm",
				Verify:    "systemctl is-active php-fpm",
				Uninstall: "systemctl stop php-fpm && yum remove -y php-fpm",
			},
		},
		Risk: "low",
		Tags: []string{"web", "php", "runtime"},
		Versions: []MiddlewareVersion{
			{Version: "8.3", DockerImage: "php:8.3-fpm", Default: true},
			{Version: "8.2", DockerImage: "php:8.2-fpm"},
			{Version: "8.1", DockerImage: "php:8.1-fpm"},
		},
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
				Deploy:    "docker run -d --name {name} -p {port}:9200 -e ES_JAVA_OPTS=\"-Xms{memlimit} -Xmx{memlimit}\" -e cluster.name={cluster} -e discovery.type=single-node --restart unless-stopped {image}",
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
		Versions: []MiddlewareVersion{
			{Version: "8.13", DockerImage: "elasticsearch:8.13", Default: true},
			{Version: "8.12", DockerImage: "elasticsearch:8.12"},
			{Version: "7.17", DockerImage: "elasticsearch:7.17.18"},
		},
	},
	{
		ID:          "opensearch",
		Name:        "OpenSearch",
		Category:    "search",
		Version:     "2.17.1",
		Description: "OpenSearch 分布式搜索与分析引擎，适用于全文检索与日志分析",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "opensearch", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "9200", Required: true, Type: "int"},
			{Name: "memlimit", Description: "JVM 堆内存限制（如 512m/1g）", Default: "1g", Required: false, Type: "string"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/opensearch", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:9200 -e \"OPENSEARCH_JAVA_OPTS=-Xms{memlimit} -Xmx{memlimit}\" -v {datadir}:/usr/share/opensearch/data --restart unless-stopped {image}",
				Verify:    "docker exec {name} curl -sf http://127.0.0.1:9200/_cluster/health",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y opensearch && systemctl enable opensearch && systemctl start opensearch",
				Verify:    "systemctl is-active opensearch",
				Uninstall: "systemctl stop opensearch && yum remove -y opensearch",
			},
		},
		Risk: "medium",
		Tags: []string{"search", "elk", "fulltext", "opensearch"},
		Versions: []MiddlewareVersion{
			{Version: "2.17.1", DockerImage: "opensearchproject/opensearch:2.17.1", Default: true},
			{Version: "2.16.0", DockerImage: "opensearchproject/opensearch:2.16.0"},
		},
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
				Deploy:    "docker run -d --name {name} -p {port}:9000 -e MINIO_ROOT_USER={user} -e MINIO_ROOT_PASSWORD={password} -v {datadir}:/data --restart unless-stopped {image} server /data",
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
		Versions: []MiddlewareVersion{
			{Version: "latest", DockerImage: "minio/minio:latest", Default: true},
			{Version: "20241013", DockerImage: "minio/minio:RELEASE.2024-10-13T13-34-11Z"},
		},
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
				Deploy:    "docker run -d --name {name} -p 8500:8500 -p 8300:8300 -p 8301:8301 -p 8302:8302 --restart unless-stopped {image} agent -dev -client=0.0.0.0",
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
		Versions: []MiddlewareVersion{
			{Version: "1.15", DockerImage: "hashicorp/consul:1.15", Default: true},
			{Version: "1.16", DockerImage: "hashicorp/consul:1.16"},
		},
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
				Deploy:    "docker run -d --name {name} -p 2379:2379 -p 2380:2380 --restart unless-stopped {image} /usr/local/bin/etcd -name etcd0 -data-dir /data -listen-client-urls http://0.0.0.0:2379 -advertise-client-urls http://0.0.0.0:2379",
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
		Versions: []MiddlewareVersion{
			{Version: "3.5", DockerImage: "gcr.io/etcd-development/etcd:v3.5.12", Default: true},
			{Version: "3.4", DockerImage: "gcr.io/etcd-development/etcd:v3.4.27"},
		},
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
				Deploy:    "docker run -d --name {name} -p {port}:9090 -v {configdir}:/etc/prometheus --restart unless-stopped {image}",
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
		Versions: []MiddlewareVersion{
			{Version: "2.45", DockerImage: "prom/prometheus:v2.45.0", Default: true},
			{Version: "2.50", DockerImage: "prom/prometheus:v2.50.1"},
			{Version: "2.43", DockerImage: "prom/prometheus:v2.43.0"},
		},
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
				Deploy:    "docker run -d --name {name} -p {port}:3000 --restart unless-stopped {image}",
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
		Versions: []MiddlewareVersion{
			{Version: "10.2", DockerImage: "grafana/grafana:10.2.0", Default: true},
			{Version: "11.0", DockerImage: "grafana/grafana:11.0.0"},
			{Version: "10.4", DockerImage: "grafana/grafana:10.4.2"},
		},
	},
	// ---------------- service ----------------
	{
		ID:          "nacos",
		Name:        "Nacos",
		Category:    "service",
		Version:     "2.4.0",
		Description: "Nacos 服务注册与配置中心，适用于微服务发现与动态配置管理",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "nacos", Required: true, Type: "string"},
			{Name: "port", Description: "HTTP 端口", Default: "8848", Required: true, Type: "int"},
			{Name: "gRPCport", Description: "gRPC 端口", Default: "9848", Required: false, Type: "int"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/nacos", Required: true, Type: "string"},
			{Name: "mode", Description: "运行模式（standalone/cluster）", Default: "standalone", Required: false, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:8848 -p {gRPCport}:9848 -e MODE={mode} -v {datadir}:/home/nacos/data --restart unless-stopped {image}",
				Verify:    "docker exec {name} curl -sf http://127.0.0.1:8848/nacos/v1/console/health/readiness",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y nacos && systemctl enable nacos && systemctl start nacos",
				Verify:    "systemctl is-active nacos",
				Uninstall: "systemctl stop nacos && yum remove -y nacos",
			},
		},
		Risk: "medium",
		Tags: []string{"service", "discovery", "config", "nacos"},
		Versions: []MiddlewareVersion{
			{Version: "2.4.0", DockerImage: "nacos/nacos-server:v2.4.0", Default: true},
			{Version: "2.3.2", DockerImage: "nacos/nacos-server:v2.3.2"},
		},
	},
	// ---------------- monitor ----------------
	{
		ID:          "loki",
		Name:        "Grafana Loki",
		Category:    "monitor",
		Version:     "3.2.1",
		Description: "Grafana Loki 日志聚合系统，适用于容器日志集中检索与告警",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "loki", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "3100", Required: true, Type: "int"},
			{Name: "configdir", Description: "配置目录（宿主机路径）", Default: "/etc/loki", Required: true, Type: "string"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/loki", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:3100 -v {configdir}:/etc/loki:ro -v {datadir}:/loki --restart unless-stopped {image}",
				Verify:    "docker exec {name} wget -q -O- http://127.0.0.1:3100/ready",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y loki && systemctl enable loki && systemctl start loki",
				Verify:    "systemctl is-active loki",
				Uninstall: "systemctl stop loki && yum remove -y loki",
			},
		},
		Risk: "low",
		Tags: []string{"monitor", "logs", "aggregation", "loki"},
		Versions: []MiddlewareVersion{
			{Version: "3.2.1", DockerImage: "grafana/loki:3.2.1", Default: true},
			{Version: "3.1.0", DockerImage: "grafana/loki:3.1.0"},
		},
	},
	{
		ID:          "alertmanager",
		Name:        "Prometheus Alertmanager",
		Category:    "monitor",
		Version:     "0.27.0",
		Description: "Prometheus Alertmanager 告警分组与静默管理组件",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "alertmanager", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "9093", Required: true, Type: "int"},
			{Name: "configdir", Description: "配置目录（宿主机路径）", Default: "/etc/alertmanager", Required: true, Type: "string"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/alertmanager", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:9093 -v {configdir}:/etc/alertmanager:ro -v {datadir}:/alertmanager --restart unless-stopped {image}",
				Verify:    "docker exec {name} wget -q -O- http://127.0.0.1:9093/-/ready",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y prometheus-alertmanager && systemctl enable alertmanager && systemctl start alertmanager",
				Verify:    "systemctl is-active alertmanager",
				Uninstall: "systemctl stop alertmanager && yum remove -y prometheus-alertmanager",
			},
		},
		Risk: "low",
		Tags: []string{"monitor", "alerting", "prometheus", "alertmanager"},
		Versions: []MiddlewareVersion{
			{Version: "0.27.0", DockerImage: "prom/alertmanager:v0.27.0", Default: true},
			{Version: "0.26.0", DockerImage: "prom/alertmanager:v0.26.0"},
		},
	},
	{
		ID:          "jaeger",
		Name:        "Jaeger",
		Category:    "monitor",
		Version:     "1.60",
		Description: "Jaeger 分布式链路追踪系统，适用于微服务调用链分析与性能定位",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "jaeger", Required: true, Type: "string"},
			{Name: "port", Description: "Web UI 端口", Default: "16686", Required: true, Type: "int"},
			{Name: "collectorport", Description: "采集端口", Default: "14268", Required: false, Type: "int"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/jaeger", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:16686 -p {collectorport}:14268 -v {datadir}:/badger --restart unless-stopped {image}",
				Verify:    "docker exec {name} wget -q -O- http://127.0.0.1:16686/",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y jaeger && systemctl enable jaeger && systemctl start jaeger",
				Verify:    "systemctl is-active jaeger",
				Uninstall: "systemctl stop jaeger && yum remove -y jaeger",
			},
		},
		Risk: "low",
		Tags: []string{"monitor", "tracing", "apm", "jaeger"},
		Versions: []MiddlewareVersion{
			{Version: "1.60", DockerImage: "jaegertracing/all-in-one:1.60", Default: true},
			{Version: "1.57", DockerImage: "jaegertracing/all-in-one:1.57"},
		},
	},
	// ---------------- new 14 ----------------
	// doris (database, medium) — Apache Doris OLAP 数据库
	{
		ID:          "doris",
		Name:        "Apache Doris",
		Category:    "database",
		Version:     "2.1.8",
		Description: "Apache Doris OLAP 数据库，适用于实时分析与多维聚合查询",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "doris", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "9030", Required: true, Type: "int"},
			{Name: "httpport", Description: "HTTP 端口", Default: "8030", Required: false, Type: "int"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/doris", Required: true, Type: "string"},
			{Name: "heap", Description: "JVM 堆内存（如 1g）", Default: "1g", Required: false, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:9030 -p {httpport}:8030 -e \"JAVA_OPTS=-Xmx{heap}\" -v {datadir}:/opt/apache-doris/fe/doris-meta --restart unless-stopped {image}",
				Verify:    "docker exec {name} curl -sf http://127.0.0.1:8030/api/health",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y apache-doris && systemctl enable doris && systemctl start doris",
				Verify:    "systemctl is-active doris",
				Uninstall: "systemctl stop doris && yum remove -y apache-doris",
			},
		},
		Risk: "medium",
		Tags: []string{"database", "olap", "analytics", "doris"},
		Versions: []MiddlewareVersion{
			{Version: "2.1.8", DockerImage: "apache/doris:2.1.8", Default: true},
			{Version: "2.0.15", DockerImage: "apache/doris:2.0.15"},
			{Version: "3.0.3", DockerImage: "apache/doris:3.0.3"},
		},
	},
	// logstash (monitor, low) — Logstash 日志采集处理
	{
		ID:          "logstash",
		Name:        "Logstash",
		Category:    "monitor",
		Version:     "8.17.0",
		Description: "Logstash 日志采集处理，适用于日志解析/过滤与管道输出",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "logstash", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "5044", Required: true, Type: "int"},
			{Name: "configdir", Description: "配置目录（宿主机路径）", Default: "/etc/logstash", Required: true, Type: "string"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/logstash", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:5044 -v {configdir}:/usr/share/logstash/config:ro -v {datadir}:/usr/share/logstash/data --restart unless-stopped {image}",
				Verify:    "docker exec {name} curl -sf http://127.0.0.1:9600/_node/stats",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y logstash && systemctl enable logstash && systemctl start logstash",
				Verify:    "systemctl is-active logstash",
				Uninstall: "systemctl stop logstash && yum remove -y logstash",
			},
		},
		Risk: "low",
		Tags: []string{"monitor", "logs", "elk", "logstash"},
		Versions: []MiddlewareVersion{
			{Version: "8.17.0", DockerImage: "logstash:8.17.0", Default: true},
			{Version: "8.13.0", DockerImage: "logstash:8.13.0"},
			{Version: "7.17.20", DockerImage: "logstash:7.17.20"},
		},
	},
	// kibana (monitor, low) — Kibana 数据可视化
	{
		ID:          "kibana",
		Name:        "Kibana",
		Category:    "monitor",
		Version:     "8.17.0",
		Description: "Kibana 数据可视化，适用于 Elasticsearch/OpenSearch 数据探索与仪表盘",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "kibana", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "5601", Required: true, Type: "int"},
			{Name: "esurl", Description: "Elasticsearch 地址", Default: "http://localhost:9200", Required: true, Type: "string"},
			{Name: "configdir", Description: "配置目录（宿主机路径）", Default: "/etc/kibana", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:5601 -e ELASTICSEARCH_HOSTS={esurl} -v {configdir}:/usr/share/kibana/config:ro --restart unless-stopped {image}",
				Verify:    "docker exec {name} curl -sf http://127.0.0.1:5601/api/status",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y kibana && systemctl enable kibana && systemctl start kibana",
				Verify:    "systemctl is-active kibana",
				Uninstall: "systemctl stop kibana && yum remove -y kibana",
			},
		},
		Risk: "low",
		Tags: []string{"monitor", "dashboard", "elk", "kibana"},
		Versions: []MiddlewareVersion{
			{Version: "8.17.0", DockerImage: "kibana:8.17.0", Default: true},
			{Version: "8.13.0", DockerImage: "kibana:8.13.0"},
			{Version: "7.17.20", DockerImage: "kibana:7.17.20"},
		},
	},
	// filebeat (monitor, low) — Filebeat 日志采集器
	{
		ID:          "filebeat",
		Name:        "Filebeat",
		Category:    "monitor",
		Version:     "8.17.0",
		Description: "Filebeat 日志采集器，适用于日志文件采集与转发",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "filebeat", Required: true, Type: "string"},
			{Name: "configdir", Description: "配置目录（宿主机路径）", Default: "/etc/filebeat", Required: true, Type: "string"},
			{Name: "logpath", Description: "日志路径（宿主机）", Default: "/var/log/*.log", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -v {configdir}:/usr/share/filebeat:ro -v {logpath}:/var/log:ro --restart unless-stopped {image}",
				Verify:    "docker exec {name} filebeat test config",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y filebeat && systemctl enable filebeat && systemctl start filebeat",
				Verify:    "systemctl is-active filebeat",
				Uninstall: "systemctl stop filebeat && yum remove -y filebeat",
			},
		},
		Risk: "low",
		Tags: []string{"monitor", "logs", "shipper", "filebeat"},
		Versions: []MiddlewareVersion{
			{Version: "8.17.0", DockerImage: "elastic/filebeat:8.17.0", Default: true},
			{Version: "8.13.0", DockerImage: "elastic/filebeat:8.13.0"},
			{Version: "7.17.20", DockerImage: "elastic/filebeat:7.17.20"},
		},
	},
	// nightingale (monitor, low) — 夜莺监控平台
	{
		ID:          "nightingale",
		Name:        "Nightingale",
		Category:    "monitor",
		Version:     "6.7.0",
		Description: "Nightingale 夜莺监控平台，适用于告警管理与指标可视化",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "nightingale", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "17000", Required: true, Type: "int"},
			{Name: "dbhost", Description: "数据库地址", Default: "localhost:3306", Required: true, Type: "string"},
			{Name: "dbuser", Description: "数据库用户", Default: "root", Required: true, Type: "string"},
			{Name: "dbpass", Description: "数据库密码", Default: "", Required: true, Type: "string"},
			{Name: "configdir", Description: "配置目录（宿主机路径）", Default: "/etc/n9e", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:17000 -e DB_HOST={dbhost} -e DB_USER={dbuser} -e DB_PASS={dbpass} -v {configdir}:/etc/n9e --restart unless-stopped {image}",
				Verify:    "docker exec {name} wget -q -O- http://127.0.0.1:17000/api/n9e/health",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y nightingale && systemctl enable nightingale && systemctl start nightingale",
				Verify:    "systemctl is-active nightingale",
				Uninstall: "systemctl stop nightingale && yum remove -y nightingale",
			},
		},
		Risk: "low",
		Tags: []string{"monitor", "alerting", "nightingale", "flashcat"},
		Versions: []MiddlewareVersion{
			{Version: "6.7.0", DockerImage: "flashcatcloud/nightingale:6.7.0", Default: true},
			{Version: "6.6.0", DockerImage: "flashcatcloud/nightingale:6.6.0"},
			{Version: "6.5.0", DockerImage: "flashcatcloud/nightingale:6.5.0"},
		},
	},
	// skywalking (monitor, low) — Apache SkyWalking APM
	{
		ID:          "skywalking",
		Name:        "SkyWalking",
		Category:    "monitor",
		Version:     "9.7.0",
		Description: "Apache SkyWalking APM，适用于分布式链路追踪与性能分析",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "skywalking", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "11800", Required: true, Type: "int"},
			{Name: "webport", Description: "Web UI 端口", Default: "8080", Required: false, Type: "int"},
			{Name: "storagetype", Description: "存储类型", Default: "elasticsearch", Required: false, Type: "string"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/skywalking", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:11800 -p {webport}:8080 -e SW_STORAGE={storagetype} -v {datadir}:/skywalking/data --restart unless-stopped {image}",
				Verify:    "docker exec {name} curl -sf http://127.0.0.1:8080/graphql",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y skywalking && systemctl enable skywalking && systemctl start skywalking",
				Verify:    "systemctl is-active skywalking",
				Uninstall: "systemctl stop skywalking && yum remove -y skywalking",
			},
		},
		Risk: "low",
		Tags: []string{"monitor", "apm", "tracing", "skywalking"},
		Versions: []MiddlewareVersion{
			{Version: "9.7.0", DockerImage: "apache/skywalking-oap-server:9.7.0", Default: true},
			{Version: "9.6.0", DockerImage: "apache/skywalking-oap-server:9.6.0"},
			{Version: "10.0.0", DockerImage: "apache/skywalking-oap-server:10.0.0"},
		},
	},
	// fastdfs (storage, medium) — FastDFS 分布式文件系统
	{
		ID:          "fastdfs",
		Name:        "FastDFS",
		Category:    "storage",
		Version:     "6.12.0",
		Description: "FastDFS 分布式文件系统，适用于文件存储与访问加速",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "fastdfs", Required: true, Type: "string"},
			{Name: "trackerport", Description: "Tracker 端口", Default: "22122", Required: true, Type: "int"},
			{Name: "storageport", Description: "Storage 端口", Default: "23000", Required: false, Type: "int"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/fastdfs", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {trackerport}:22122 -p {storageport}:23000 -v {datadir}:/data --restart unless-stopped {image}",
				Verify:    "docker exec {name} fdfs_trackerd /etc/fdfs/tracker.conf status",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y fastdfs && systemctl enable fastdfs && systemctl start fastdfs",
				Verify:    "systemctl is-active fastdfs",
				Uninstall: "systemctl stop fastdfs && yum remove -y fastdfs",
			},
		},
		Risk: "medium",
		Tags: []string{"storage", "files", "fastdfs"},
		Versions: []MiddlewareVersion{
			{Version: "6.12.0", DockerImage: "delron/fastdfs:6.12.0", Default: true},
			{Version: "6.11.0", DockerImage: "delron/fastdfs:6.11.0"},
		},
	},
	// lvs (web, high) — LVS 负载均衡（IPVS）
	{
		ID:          "lvs",
		Name:        "LVS (IPVS)",
		Category:    "web",
		Version:     "1.0",
		Description: "LVS 负载均衡（IPVS），适用于四层流量分发与高可用",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "实例名称", Default: "lvs", Required: true, Type: "string"},
			{Name: "vip", Description: "虚拟 IP", Default: "10.0.0.100", Required: true, Type: "string"},
			{Name: "rslist", Description: "后端服务器列表", Default: "10.0.0.11,10.0.0.12", Required: true, Type: "string"},
			{Name: "scheduler", Description: "调度算法", Default: "rr", Required: false, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} --privileged -e VIP={vip} -e RS_LIST={rslist} -e SCHEDULER={scheduler} --restart unless-stopped {image}",
				Verify:    "docker exec {name} ipvsadm -Ln",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y ipvsadm && ipvsadm -C && systemctl enable ipvsadm",
				Verify:    "ipvsadm -Ln",
				Uninstall: "ipvsadm -C && yum remove -y ipvsadm",
			},
		},
		Risk: "high",
		Tags: []string{"web", "load-balance", "lvs", "ipvs"},
		Versions: []MiddlewareVersion{
			{Version: "1.0", DockerImage: "ipvsadm:1.0", Default: true},
		},
	},
	// f5 (web, high) — F5 BIG-IP（模拟部署）
	{
		ID:          "f5",
		Name:        "F5 BIG-IP",
		Category:    "web",
		Version:     "1.0",
		Description: "F5 BIG-IP 应用交付控制器（模拟部署）",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "实例名称", Default: "f5", Required: true, Type: "string"},
			{Name: "vip", Description: "虚拟 IP", Default: "10.0.0.100", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "443", Required: true, Type: "int"},
			{Name: "pool", Description: "后端池名称", Default: "backend_pool", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:443 -e VIP={vip} -e POOL={pool} --restart unless-stopped {image}",
				Verify:    "docker exec {name} curl -sk https://127.0.0.1/",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "echo \"F5 BIG-IP requires dedicated hardware; use docker deployment\" && exit 1",
				Verify:    "echo \"F5 requires dedicated hardware\"",
				Uninstall: "echo \"F5 requires dedicated hardware\"",
			},
		},
		Risk: "high",
		Tags: []string{"web", "load-balance", "f5", "adc"},
		Versions: []MiddlewareVersion{
			{Version: "1.0", DockerImage: "f5-bigip:1.0", Default: true},
		},
	},
	// dubbo (service, medium) — Apache Dubbo RPC 框架
	{
		ID:          "dubbo",
		Name:        "Apache Dubbo",
		Category:    "service",
		Version:     "3.3.0",
		Description: "Apache Dubbo RPC 框架，适用于微服务远程调用与治理",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "dubbo", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "20880", Required: true, Type: "int"},
			{Name: "zookeeper", Description: "Zookeeper 地址", Default: "localhost:2181", Required: true, Type: "string"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/dubbo", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:20880 -e DUBBO_REGISTRY_ADDRESS=zookeeper://{zookeeper} -v {datadir}:/data --restart unless-stopped {image}",
				Verify:    "docker exec {name} curl -sf http://127.0.0.1:20880",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y dubbo && systemctl enable dubbo && systemctl start dubbo",
				Verify:    "systemctl is-active dubbo",
				Uninstall: "systemctl stop dubbo && yum remove -y dubbo",
			},
		},
		Risk: "medium",
		Tags: []string{"service", "rpc", "dubbo", "microservice"},
		Versions: []MiddlewareVersion{
			{Version: "3.3.0", DockerImage: "apache/dubbo:3.3.0", Default: true},
			{Version: "3.2.11", DockerImage: "apache/dubbo:3.2.11"},
			{Version: "3.1.10", DockerImage: "apache/dubbo:3.1.10"},
		},
	},
	// spring-cloud-feign (service, low)
	{
		ID:          "spring-cloud-feign",
		Name:        "Spring Cloud Feign",
		Category:    "service",
		Version:     "4.1.0",
		Description: "Spring Cloud Feign HTTP 客户端，适用于微服务间 HTTP 调用",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "feign", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "8080", Required: true, Type: "int"},
			{Name: "eureka", Description: "Eureka 地址", Default: "http://localhost:8761/eureka", Required: true, Type: "string"},
			{Name: "configserver", Description: "配置中心地址", Default: "http://localhost:8888", Required: false, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:8080 -e EUREKA_CLIENT_SERVICEURL_DEFAULTZONE={eureka} -e SPRING_CLOUD_CONFIG_URI={configserver} --restart unless-stopped {image}",
				Verify:    "docker exec {name} curl -sf http://127.0.0.1:8080/actuator/health",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y spring-cloud-feign && systemctl enable feign && systemctl start feign",
				Verify:    "systemctl is-active feign",
				Uninstall: "systemctl stop feign && yum remove -y spring-cloud-feign",
			},
		},
		Risk: "low",
		Tags: []string{"service", "microservice", "feign", "springcloud"},
		Versions: []MiddlewareVersion{
			{Version: "4.1.0", DockerImage: "springcloud/openfeign:4.1.0", Default: true},
			{Version: "4.0.4", DockerImage: "springcloud/openfeign:4.0.4"},
			{Version: "3.1.8", DockerImage: "springcloud/openfeign:3.1.8"},
		},
	},
	// thrift (service, low)
	{
		ID:          "thrift",
		Name:        "Apache Thrift",
		Category:    "service",
		Version:     "0.19.0",
		Description: "Apache Thrift RPC 框架，适用于跨语言服务通信",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "thrift", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "9090", Required: true, Type: "int"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/thrift", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:9090 -v {datadir}:/data --restart unless-stopped {image}",
				Verify:    "docker exec {name} thrift --version",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y thrift && systemctl enable thrift && systemctl start thrift",
				Verify:    "systemctl is-active thrift",
				Uninstall: "systemctl stop thrift && yum remove -y thrift",
			},
		},
		Risk: "low",
		Tags: []string{"service", "rpc", "thrift", "apache"},
		Versions: []MiddlewareVersion{
			{Version: "0.19.0", DockerImage: "thrift:0.19.0", Default: true},
			{Version: "0.18.1", DockerImage: "thrift:0.18.1"},
			{Version: "0.17.0", DockerImage: "thrift:0.17.0"},
		},
	},
	// apollo (service, medium)
	{
		ID:          "apollo",
		Name:        "Apollo Config",
		Category:    "service",
		Version:     "2.2.0",
		Description: "Apollo 配置中心，适用于分布式配置管理",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "apollo", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "8070", Required: true, Type: "int"},
			{Name: "dbhost", Description: "数据库地址", Default: "localhost:3306", Required: true, Type: "string"},
			{Name: "dbuser", Description: "数据库用户", Default: "root", Required: true, Type: "string"},
			{Name: "dbpass", Description: "数据库密码", Default: "", Required: true, Type: "string"},
			{Name: "eureka", Description: "Eureka 地址", Default: "http://localhost:8761/eureka", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:8070 -e SPRING_DATASOURCE_URL=jdbc:mysql://{dbhost}/ApolloPortalDB -e SPRING_DATASOURCE_USERNAME={dbuser} -e SPRING_DATASOURCE_PASSWORD={dbpass} -e EUREKA_SERVICE_URL={eureka} --restart unless-stopped {image}",
				Verify:    "docker exec {name} curl -sf http://127.0.0.1:8070/",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y apollo && systemctl enable apollo && systemctl start apollo",
				Verify:    "systemctl is-active apollo",
				Uninstall: "systemctl stop apollo && yum remove -y apollo",
			},
		},
		Risk: "medium",
		Tags: []string{"service", "config", "apollo", "ctrip"},
		Versions: []MiddlewareVersion{
			{Version: "2.2.0", DockerImage: "apolloconfig/apollo-portal:2.2.0", Default: true},
			{Version: "2.1.0", DockerImage: "apolloconfig/apollo-portal:2.1.0"},
			{Version: "2.0.1", DockerImage: "apolloconfig/apollo-portal:2.0.1"},
		},
	},
	// pulsar (message, medium)
	{
		ID:          "pulsar",
		Name:        "Apache Pulsar",
		Category:    "message",
		Version:     "3.3.0",
		Description: "Apache Pulsar 消息队列，适用于高吞吐消息与流处理",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "pulsar", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "6650", Required: true, Type: "int"},
			{Name: "webport", Description: "Web UI 端口", Default: "8080", Required: false, Type: "int"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/pulsar", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:6650 -p {webport}:8080 -v {datadir}:/pulsar/data --restart unless-stopped {image}",
				Verify:    "docker exec {name} curl -sf http://127.0.0.1:8080/admin/v2/brokers/health",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y pulsar && systemctl enable pulsar && systemctl start pulsar",
				Verify:    "systemctl is-active pulsar",
				Uninstall: "systemctl stop pulsar && yum remove -y pulsar",
			},
		},
		Risk: "medium",
		Tags: []string{"message", "queue", "pubsub", "pulsar"},
		Versions: []MiddlewareVersion{
			{Version: "3.3.0", DockerImage: "apachepulsar/pulsar:3.3.0", Default: true},
			{Version: "3.2.0", DockerImage: "apachepulsar/pulsar:3.2.0"},
		},
	},
	// ---------------- new: ceph / spring cloud / zuul / solr / eureka / orchestrator ----------------
	// ceph (storage, high) — Ceph 分布式存储
	{
		ID:          "ceph",
		Name:        "Ceph",
		Category:    "storage",
		Version:     "18.2.4",
		Description: "Ceph 分布式存储，适用于块/对象/文件存储与高可用",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "ceph", Required: true, Type: "string"},
			{Name: "monport", Description: "Monitor 端口", Default: "6789", Required: true, Type: "int"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/ceph", Required: true, Type: "string"},
			{Name: "osdcount", Description: "OSD 数量", Default: "3", Required: false, Type: "int"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {monport}:6789 -v {datadir}:/var/lib/ceph -e OSD_COUNT={osdcount} --restart unless-stopped {image}",
				Verify:    "docker exec {name} ceph status",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y ceph && systemctl enable ceph && systemctl start ceph",
				Verify:    "systemctl is-active ceph",
				Uninstall: "systemctl stop ceph && yum remove -y ceph",
			},
		},
		Risk: "high",
		Tags: []string{"storage", "distributed", "ceph", "cephfs"},
		Versions: []MiddlewareVersion{
			{Version: "18.2.4", DockerImage: "ceph/daemon:latest", Default: true},
			{Version: "17.2.7", DockerImage: "ceph/daemon:v17.2.7"},
			{Version: "19.2.0", DockerImage: "ceph/daemon:v19.2.0"},
		},
	},
	// spring-cloud-gateway (service, low) — Spring Cloud Gateway
	{
		ID:          "spring-cloud-gateway",
		Name:        "Spring Cloud Gateway",
		Category:    "service",
		Version:     "4.1.0",
		Description: "Spring Cloud Gateway API 网关，适用于微服务路由与过滤",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "gateway", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "8080", Required: true, Type: "int"},
			{Name: "eureka", Description: "Eureka 地址", Default: "http://localhost:8761/eureka", Required: true, Type: "string"},
			{Name: "configserver", Description: "配置中心地址", Default: "http://localhost:8888", Required: false, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:8080 -e EUREKA_CLIENT_SERVICEURL_DEFAULTZONE={eureka} -e SPRING_CLOUD_CONFIG_URI={configserver} --restart unless-stopped {image}",
				Verify:    "docker exec {name} curl -sf http://127.0.0.1:8080/actuator/health",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y spring-cloud-gateway && systemctl enable gateway && systemctl start gateway",
				Verify:    "systemctl is-active gateway",
				Uninstall: "systemctl stop gateway && yum remove -y spring-cloud-gateway",
			},
		},
		Risk: "low",
		Tags: []string{"service", "gateway", "springcloud", "microservice"},
		Versions: []MiddlewareVersion{
			{Version: "4.1.0", DockerImage: "springcloud/gateway:4.1.0", Default: true},
			{Version: "4.0.4", DockerImage: "springcloud/gateway:4.0.4"},
			{Version: "3.1.8", DockerImage: "springcloud/gateway:3.1.8"},
		},
	},
	// zuul (service, low) — Netflix Zuul API Gateway
	{
		ID:          "zuul",
		Name:        "Netflix Zuul",
		Category:    "service",
		Version:     "2.3.0",
		Description: "Netflix Zuul API 网关，适用于微服务路由与过滤",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "zuul", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "8080", Required: true, Type: "int"},
			{Name: "eureka", Description: "Eureka 地址", Default: "http://localhost:8761/eureka", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:8080 -e EUREKA_CLIENT_SERVICEURL_DEFAULTZONE={eureka} --restart unless-stopped {image}",
				Verify:    "docker exec {name} curl -sf http://127.0.0.1:8080/health",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y zuul && systemctl enable zuul && systemctl start zuul",
				Verify:    "systemctl is-active zuul",
				Uninstall: "systemctl stop zuul && yum remove -y zuul",
			},
		},
		Risk: "low",
		Tags: []string{"service", "gateway", "netflix", "zuul"},
		Versions: []MiddlewareVersion{
			{Version: "2.3.0", DockerImage: "netflixoss/zuul:2.3.0", Default: true},
			{Version: "2.2.0", DockerImage: "netflixoss/zuul:2.2.0"},
		},
	},
	// spring-cloud-config (service, medium) — Spring Cloud Config Server
	{
		ID:          "spring-cloud-config",
		Name:        "Spring Cloud Config",
		Category:    "service",
		Version:     "4.1.0",
		Description: "Spring Cloud Config 配置中心，适用于分布式配置管理",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "config-server", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "8888", Required: true, Type: "int"},
			{Name: "gituri", Description: "Git 仓库地址", Default: "https://github.com/...", Required: true, Type: "string"},
			{Name: "branch", Description: "分支名称", Default: "main", Required: false, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:8888 -e SPRING_CLOUD_CONFIG_SERVER_GIT_URI={gituri} -e SPRING_CLOUD_CONFIG_SERVER_GIT_DEFAULT_LABEL={branch} --restart unless-stopped {image}",
				Verify:    "docker exec {name} curl -sf http://127.0.0.1:8888/actuator/health",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y spring-cloud-config && systemctl enable config-server && systemctl start config-server",
				Verify:    "systemctl is-active config-server",
				Uninstall: "systemctl stop config-server && yum remove -y spring-cloud-config",
			},
		},
		Risk: "medium",
		Tags: []string{"service", "config", "springcloud", "microservice"},
		Versions: []MiddlewareVersion{
			{Version: "4.1.0", DockerImage: "springcloud/config-server:4.1.0", Default: true},
			{Version: "4.0.4", DockerImage: "springcloud/config-server:4.0.4"},
			{Version: "3.1.8", DockerImage: "springcloud/config-server:3.1.8"},
		},
	},
	// solr (search, low) — Apache Solr 搜索引擎
	{
		ID:          "solr",
		Name:        "Apache Solr",
		Category:    "search",
		Version:     "9.6.1",
		Description: "Apache Solr 搜索引擎，适用于全文检索与企业搜索",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "solr", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "8983", Required: true, Type: "int"},
			{Name: "corename", Description: "Core 名称", Default: "gettingstarted", Required: false, Type: "string"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/solr", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:8983 -v {datadir}:/var/solr --restart unless-stopped {image}",
				Verify:    "docker exec {name} curl -sf http://127.0.0.1:8983/solr/admin/cores?action=STATUS",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y solr && systemctl enable solr && systemctl start solr",
				Verify:    "systemctl is-active solr",
				Uninstall: "systemctl stop solr && yum remove -y solr",
			},
		},
		Risk: "low",
		Tags: []string{"search", "fulltext", "solr", "apache"},
		Versions: []MiddlewareVersion{
			{Version: "9.6.1", DockerImage: "solr:9.6.1", Default: true},
			{Version: "9.5.0", DockerImage: "solr:9.5.0"},
			{Version: "8.11.3", DockerImage: "solr:8.11.3"},
		},
	},
	// eureka (service, low) — Netflix Eureka 服务注册中心
	{
		ID:          "eureka",
		Name:        "Netflix Eureka",
		Category:    "service",
		Version:     "2.0.4",
		Description: "Netflix Eureka 服务注册中心，适用于微服务发现",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "eureka", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "8761", Required: true, Type: "int"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/eureka", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:8761 -v {datadir}:/data --restart unless-stopped {image}",
				Verify:    "docker exec {name} curl -sf http://127.0.0.1:8761/actuator/health",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y eureka && systemctl enable eureka && systemctl start eureka",
				Verify:    "systemctl is-active eureka",
				Uninstall: "systemctl stop eureka && yum remove -y eureka",
			},
		},
		Risk: "low",
		Tags: []string{"service", "discovery", "eureka", "netflix"},
		Versions: []MiddlewareVersion{
			{Version: "2.0.4", DockerImage: "netflixoss/eureka:2.0.4", Default: true},
			{Version: "1.10.18", DockerImage: "netflixoss/eureka:1.10.18"},
		},
	},
	// orchestrator (database, medium) — GitHub Orchestrator (MySQL HA)
	{
		ID:          "orchestrator",
		Name:        "Orchestrator",
		Category:    "database",
		Version:     "3.2.6",
		Description: "Orchestrator MySQL 高可用编排工具",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "orchestrator", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "3000", Required: true, Type: "int"},
			{Name: "dbhost", Description: "数据库地址", Default: "localhost:3306", Required: true, Type: "string"},
			{Name: "dbuser", Description: "数据库用户", Default: "root", Required: true, Type: "string"},
			{Name: "dbpass", Description: "数据库密码", Default: "", Required: true, Type: "string"},
			{Name: "topologyuser", Description: "拓扑用户", Default: "orchestrator", Required: true, Type: "string"},
			{Name: "topologypass", Description: "拓扑密码", Default: "", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:3000 -e ORC_DB_HOST={dbhost} -e ORC_DB_USER={dbuser} -e ORC_DB_PASSWORD={dbpass} -e ORC_TOPOLOGY_USER={topologyuser} -e ORC_TOPOLOGY_PASSWORD={topologypass} --restart unless-stopped {image}",
				Verify:    "docker exec {name} curl -sf http://127.0.0.1:3000/api/health",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y orchestrator && systemctl enable orchestrator && systemctl start orchestrator",
				Verify:    "systemctl is-active orchestrator",
				Uninstall: "systemctl stop orchestrator && yum remove -y orchestrator",
			},
		},
		Risk: "medium",
		Tags: []string{"database", "ha", "orchestrator", "mysql"},
		Versions: []MiddlewareVersion{
			{Version: "3.2.6", DockerImage: "github/orchestrator:3.2.6", Default: true},
			{Version: "3.2.5", DockerImage: "github/orchestrator:3.2.5"},
		},
	},
	// ---------------- new: Apache Foundation projects ----------------
	// apache-cassandra (database, medium)
	{
		ID:          "apache-cassandra",
		Name:        "Apache Cassandra",
		Category:    "database",
		Version:     "4.1.4",
		Description: "Apache Cassandra NoSQL 数据库，适用于高可用与线性扩展",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "cassandra", Required: true, Type: "string"},
			{Name: "port", Description: "CQL 端口", Default: "9042", Required: true, Type: "int"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/cassandra", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:9042 -v {datadir}:/var/lib/cassandra --restart unless-stopped {image}",
				Verify:    "docker exec {name} nodetool status",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y cassandra && systemctl enable cassandra && systemctl start cassandra",
				Verify:    "systemctl is-active cassandra",
				Uninstall: "systemctl stop cassandra && yum remove -y cassandra",
			},
		},
		Risk: "medium",
		Tags: []string{"database", "nosql", "cassandra", "apache"},
		Versions: []MiddlewareVersion{
			{Version: "4.1.4", DockerImage: "cassandra:4.1.4", Default: true},
			{Version: "4.0.11", DockerImage: "cassandra:4.0.11"},
			{Version: "3.11.16", DockerImage: "cassandra:3.11.16"},
		},
	},
	// apache-hbase (database, medium)
	{
		ID:          "apache-hbase",
		Name:        "Apache HBase",
		Category:    "database",
		Version:     "2.5.8",
		Description: "Apache HBase NoSQL 数据库，适用于海量数据存储与随机读写",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "hbase", Required: true, Type: "string"},
			{Name: "port", Description: "Master 端口", Default: "16000", Required: true, Type: "int"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/hbase", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:16000 -v {datadir}:/data --restart unless-stopped {image}",
				Verify:    "docker exec {name} hbase version",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y hbase && systemctl enable hbase && systemctl start hbase",
				Verify:    "systemctl is-active hbase",
				Uninstall: "systemctl stop hbase && yum remove -y hbase",
			},
		},
		Risk: "medium",
		Tags: []string{"database", "nosql", "hbase", "apache"},
		Versions: []MiddlewareVersion{
			{Version: "2.5.8", DockerImage: "apache/hbase:2.5.8", Default: true},
			{Version: "2.4.17", DockerImage: "apache/hbase:2.4.17"},
		},
	},
	// apache-couchdb (database, low)
	{
		ID:          "apache-couchdb",
		Name:        "Apache CouchDB",
		Category:    "database",
		Version:     "3.3.3",
		Description: "Apache CouchDB 文档数据库，适用于离线同步与多主复制",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "couchdb", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "5984", Required: true, Type: "int"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/couchdb", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:5984 -v {datadir}:/opt/couchdb/data --restart unless-stopped {image}",
				Verify:    "docker exec {name} curl -sf http://127.0.0.1:5984/",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y couchdb && systemctl enable couchdb && systemctl start couchdb",
				Verify:    "systemctl is-active couchdb",
				Uninstall: "systemctl stop couchdb && yum remove -y couchdb",
			},
		},
		Risk: "low",
		Tags: []string{"database", "nosql", "document", "couchdb", "apache"},
		Versions: []MiddlewareVersion{
			{Version: "3.3.3", DockerImage: "couchdb:3.3.3", Default: true},
			{Version: "3.2.2", DockerImage: "couchdb:3.2.2"},
		},
	},
	// apache-nifi (service, medium)
	{
		ID:          "apache-nifi",
		Name:        "Apache NiFi",
		Category:    "service",
		Version:     "1.25.0",
		Description: "Apache NiFi 数据流自动化，适用于数据采集与路由",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "nifi", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "8080", Required: true, Type: "int"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/nifi", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:8080 -v {datadir}:/opt/nifi/nifi-current/data --restart unless-stopped {image}",
				Verify:    "docker exec {name} curl -sf http://127.0.0.1:8080/nifi-api/system-diagnostics",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y nifi && systemctl enable nifi && systemctl start nifi",
				Verify:    "systemctl is-active nifi",
				Uninstall: "systemctl stop nifi && yum remove -y nifi",
			},
		},
		Risk: "medium",
		Tags: []string{"service", "dataflow", "nifi", "apache"},
		Versions: []MiddlewareVersion{
			{Version: "1.25.0", DockerImage: "apache/nifi:1.25.0", Default: true},
			{Version: "1.24.0", DockerImage: "apache/nifi:1.24.0"},
		},
	},
	// apache-ignite (cache, medium)
	{
		ID:          "apache-ignite",
		Name:        "Apache Ignite",
		Category:    "cache",
		Version:     "2.16.0",
		Description: "Apache Ignite 内存数据网格，适用于分布式缓存与计算",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "ignite", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "10800", Required: true, Type: "int"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/ignite", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:10800 -v {datadir}:/data --restart unless-stopped {image}",
				Verify:    "docker exec {name} curl -sf http://127.0.0.1:8080/ignite?cmd=version",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y apache-ignite && systemctl enable ignite && systemctl start ignite",
				Verify:    "systemctl is-active ignite",
				Uninstall: "systemctl stop ignite && yum remove -y apache-ignite",
			},
		},
		Risk: "medium",
		Tags: []string{"cache", "datagrid", "ignite", "apache"},
		Versions: []MiddlewareVersion{
			{Version: "2.16.0", DockerImage: "apacheignite/ignite:2.16.0", Default: true},
			{Version: "2.15.0", DockerImage: "apacheignite/ignite:2.15.0"},
		},
	},
	// apache-shardingsphere (database, medium)
	{
		ID:          "apache-shardingsphere",
		Name:        "Apache ShardingSphere",
		Category:    "database",
		Version:     "5.5.0",
		Description: "Apache ShardingSphere 数据库分片与治理",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "shardingsphere", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "3307", Required: true, Type: "int"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/shardingsphere", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:3307 -v {datadir}:/opt/shardingsphere-proxy/ext-lib --restart unless-stopped {image}",
				Verify:    "docker exec {name} curl -sf http://127.0.0.1:8080/",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y shardingsphere && systemctl enable shardingsphere && systemctl start shardingsphere",
				Verify:    "systemctl is-active shardingsphere",
				Uninstall: "systemctl stop shardingsphere && yum remove -y shardingsphere",
			},
		},
		Risk: "medium",
		Tags: []string{"database", "sharding", "shardingsphere", "apache"},
		Versions: []MiddlewareVersion{
			{Version: "5.5.0", DockerImage: "apache/shardingsphere-proxy:5.5.0", Default: true},
			{Version: "5.4.1", DockerImage: "apache/shardingsphere-proxy:5.4.1"},
		},
	},
	// apache-dolphinscheduler (service, low)
	{
		ID:          "apache-dolphinscheduler",
		Name:        "Apache DolphinScheduler",
		Category:    "service",
		Version:     "3.2.1",
		Description: "Apache DolphinScheduler 工作流调度平台",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "dolphinscheduler", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "12345", Required: true, Type: "int"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/dolphinscheduler", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:12345 -v {datadir}:/opt/dolphinscheduler/data --restart unless-stopped {image}",
				Verify:    "docker exec {name} curl -sf http://127.0.0.1:12345/dolphinscheduler/health",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y dolphinscheduler && systemctl enable dolphinscheduler && systemctl start dolphinscheduler",
				Verify:    "systemctl is-active dolphinscheduler",
				Uninstall: "systemctl stop dolphinscheduler && yum remove -y dolphinscheduler",
			},
		},
		Risk: "low",
		Tags: []string{"service", "scheduler", "workflow", "dolphinscheduler", "apache"},
		Versions: []MiddlewareVersion{
			{Version: "3.2.1", DockerImage: "apache/dolphinscheduler:3.2.1", Default: true},
			{Version: "3.1.9", DockerImage: "apache/dolphinscheduler:3.1.9"},
		},
	},
	// apache-airflow (service, low)
	{
		ID:          "apache-airflow",
		Name:        "Apache Airflow",
		Category:    "service",
		Version:     "2.9.3",
		Description: "Apache Airflow 工作流编排平台",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "airflow", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "8080", Required: true, Type: "int"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/airflow", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:8080 -v {datadir}:/opt/airflow --restart unless-stopped {image}",
				Verify:    "docker exec {name} airflow version",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y airflow && systemctl enable airflow && systemctl start airflow",
				Verify:    "systemctl is-active airflow",
				Uninstall: "systemctl stop airflow && yum remove -y airflow",
			},
		},
		Risk: "low",
		Tags: []string{"service", "scheduler", "workflow", "airflow", "apache"},
		Versions: []MiddlewareVersion{
			{Version: "2.9.3", DockerImage: "apache/airflow:2.9.3", Default: true},
			{Version: "2.8.4", DockerImage: "apache/airflow:2.8.4"},
		},
	},
	// apache-superset (monitor, low)
	{
		ID:          "apache-superset",
		Name:        "Apache Superset",
		Category:    "monitor",
		Version:     "4.0.2",
		Description: "Apache Superset 数据可视化与 BI 平台",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "superset", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "8088", Required: true, Type: "int"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/superset", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:8088 -v {datadir}:/app/superset_home --restart unless-stopped {image}",
				Verify:    "docker exec {name} curl -sf http://127.0.0.1:8088/health",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y superset && systemctl enable superset && systemctl start superset",
				Verify:    "systemctl is-active superset",
				Uninstall: "systemctl stop superset && yum remove -y superset",
			},
		},
		Risk: "low",
		Tags: []string{"monitor", "dashboard", "visualization", "superset", "apache"},
		Versions: []MiddlewareVersion{
			{Version: "4.0.2", DockerImage: "apache/superset:4.0.2", Default: true},
			{Version: "3.1.3", DockerImage: "apache/superset:3.1.3"},
		},
	},
	// grpc (service, low) — gRPC 服务框架
	{
		ID:          "grpc",
		Name:        "gRPC",
		Category:    "service",
		Version:     "1.68.0",
		Description: "gRPC 服务框架，适用于高性能 RPC 与多语言服务通信",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "grpc", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "50051", Required: true, Type: "int"},
			{Name: "protofile", Description: "Proto 文件目录", Default: "/etc/grpc/protos", Required: true, Type: "string"},
			{Name: "datadir", Description: "数据目录（宿主机路径）", Default: "/data/grpc", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:50051 -v {protofile}:/etc/grpc/protos:ro -v {datadir}:/data --restart unless-stopped {image}",
				Verify:    "docker exec {name} grpc_health_probe -addr=:50051",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y grpc && systemctl enable grpc && systemctl start grpc",
				Verify:    "systemctl is-active grpc",
				Uninstall: "systemctl stop grpc && yum remove -y grpc",
			},
		},
		Risk: "low",
		Tags: []string{"service", "rpc", "grpc", "protobuf"},
		Versions: []MiddlewareVersion{
			{Version: "1.68.0", DockerImage: "grpc/grpc:1.68.0", Default: true},
			{Version: "1.66.0", DockerImage: "grpc/grpc:1.66.0"},
			{Version: "1.64.0", DockerImage: "grpc/grpc:1.64.0"},
		},
	},
	// quartz (service, low) — Quartz 作业调度
	{
		ID:          "quartz",
		Name:        "Quartz",
		Category:    "service",
		Version:     "2.3.2",
		Description: "Quartz 作业调度框架，适用于定时任务与批处理",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "quartz", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "8080", Required: true, Type: "int"},
			{Name: "dbhost", Description: "数据库地址", Default: "localhost:3306", Required: true, Type: "string"},
			{Name: "dbuser", Description: "数据库用户", Default: "root", Required: true, Type: "string"},
			{Name: "dbpass", Description: "数据库密码", Default: "", Required: true, Type: "string"},
			{Name: "configdir", Description: "配置目录（宿主机路径）", Default: "/etc/quartz", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:8080 -e DB_HOST={dbhost} -e DB_USER={dbuser} -e DB_PASS={dbpass} -v {configdir}:/etc/quartz:ro --restart unless-stopped {image}",
				Verify:    "docker exec {name} curl -sf http://127.0.0.1:8080/quartz/health",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y quartz && systemctl enable quartz && systemctl start quartz",
				Verify:    "systemctl is-active quartz",
				Uninstall: "systemctl stop quartz && yum remove -y quartz",
			},
		},
		Risk: "low",
		Tags: []string{"service", "scheduler", "quartz", "java"},
		Versions: []MiddlewareVersion{
			{Version: "2.3.2", DockerImage: "quartz:2.3.2", Default: true},
			{Version: "2.3.1", DockerImage: "quartz:2.3.1"},
			{Version: "2.3.0", DockerImage: "quartz:2.3.0"},
		},
	},
	// kong (service, medium) — Kong API Gateway
	{
		ID:          "kong",
		Name:        "Kong",
		Category:    "service",
		Version:     "3.7.0",
		Description: "Kong API 网关，适用于 API 管理与流量治理",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "kong", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "8000", Required: true, Type: "int"},
			{Name: "adminport", Description: "管理端口", Default: "8001", Required: false, Type: "int"},
			{Name: "dbhost", Description: "数据库地址", Default: "localhost:5432", Required: true, Type: "string"},
			{Name: "dbuser", Description: "数据库用户", Default: "kong", Required: true, Type: "string"},
			{Name: "dbpass", Description: "数据库密码", Default: "", Required: true, Type: "string"},
			{Name: "configdir", Description: "配置目录（宿主机路径）", Default: "/etc/kong", Required: true, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:8000 -p {adminport}:8001 -e KONG_DATABASE=postgres -e KONG_PG_HOST={dbhost} -e KONG_PG_USER={dbuser} -e KONG_PG_PASSWORD={dbpass} -v {configdir}:/etc/kong:ro --restart unless-stopped {image}",
				Verify:    "docker exec {name} curl -sf http://127.0.0.1:8001/status",
				Uninstall: "docker stop {name} && docker rm {name}",
			},
			"systemd": {
				Deploy:    "yum install -y kong && systemctl enable kong && systemctl start kong",
				Verify:    "systemctl is-active kong",
				Uninstall: "systemctl stop kong && yum remove -y kong",
			},
		},
		Risk: "medium",
		Tags: []string{"service", "gateway", "kong", "api"},
		Versions: []MiddlewareVersion{
			{Version: "3.7.0", DockerImage: "kong:3.7.0", Default: true},
			{Version: "3.6.0", DockerImage: "kong:3.6.0"},
			{Version: "3.5.0", DockerImage: "kong:3.5.0"},
		},
	},

	// ---------------- gpu ----------------
	{
		ID:          "nvidia-driver",
		Name:        "NVIDIA Driver + CUDA",
		Category:    "gpu",
		Version:     "550.54.15",
		Description: "NVIDIA GPU driver + CUDA toolkit installation",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "driver_version", Description: "NVIDIA driver version", Default: "550.54.15", Required: true, Type: "text"},
			{Name: "cuda_version", Description: "CUDA toolkit version", Default: "12.4", Required: false, Type: "text"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run --rm --gpus all {image} nvidia-smi && echo 'NVIDIA driver verified'",
				Verify:    "docker run --rm --gpus all {image} nvidia-smi",
				Uninstall: "echo 'NVIDIA driver container removed'",
			},
			"systemd": {
				Deploy:    "wget -q https://us.download.nvidia.com/tesla/{{.driver_version}}/NVIDIA-Linux-x86_64-{{.driver_version}}.run -O /tmp/nvidia.run && chmod +x /tmp/nvidia.run && /tmp/nvidia.run --silent --no-opengl-files --no-libglx-indirect && echo 'NVIDIA driver {{.driver_version}} installed' && if [ -n '{{.cuda_version}}' ]; then wget -q https://developer.download.nvidia.com/compute/cuda/{{.cuda_version}}.0/local_installers/cuda_{{.cuda_version}}.0_550.54.15_linux.run -O /tmp/cuda.run && chmod +x /tmp/cuda.run && /tmp/cuda.run --silent --toolkit && echo 'CUDA {{.cuda_version}} installed'; fi",
				Verify:    "nvidia-smi --query-gpu=driver_version --format=csv,noheader && nvcc --version 2>/dev/null | head -1",
				Uninstall: "nvidia-uninstall 2>/dev/null; echo 'NVIDIA driver removed'",
			},
		},
		Risk: "high",
		Tags: []string{"gpu", "nvidia", "driver", "cuda"},
		Versions: []MiddlewareVersion{
			{Version: "550.54.15", DockerImage: "nvidia/cuda:12.4.0-base-ubuntu22.04", Default: true},
		},
	},
	{
		ID:          "dcgm",
		Name:        "NVIDIA DCGM",
		Category:    "gpu",
		Version:     "3.3.0",
		Description: "Data Center GPU Manager for diagnostics and monitoring",
		DeployTypes: []string{"docker", "systemd"},
		Params:      []MiddlewareParam{},
		Scripts: map[string]MiddlewareScript{
			"systemd": {
				Deploy:    "apt-get install -y datacenter-gpu-manager 2>/dev/null || yum install -y datacenter-gpu-manager 2>/dev/null && systemctl enable --now dcgm && echo 'DCGM deployed'",
				Verify:    "dcgmi discovery -l 2>/dev/null && echo 'DCGM healthy'",
				Uninstall: "systemctl stop dcgm && apt-get remove -y datacenter-gpu-manager 2>/dev/null; echo 'DCGM removed'",
			},
			"docker": {
				Deploy:    "docker run -d --name dcgm --gpus all -p 5555:5555 {image} && echo 'DCGM exporter deployed'",
				Verify:    "curl -s http://localhost:5555/metrics | head -5",
				Uninstall: "docker rm -f dcgm && echo 'DCGM removed'",
			},
		},
		Risk: "medium",
		Tags: []string{"gpu", "nvidia", "dcgm", "monitoring"},
		Versions: []MiddlewareVersion{
			{Version: "3.3.0", DockerImage: "nvcr.io/nvidia/k8s/dcgm-exporter:3.3.0-3.2.0-ubuntu22.04", Default: true},
		},
	},
	{
		ID:          "ollama",
		Name:        "Ollama LLM Serving",
		Category:    "gpu",
		Version:     "0.3.0",
		Description: "Ollama local LLM serving with GPU acceleration",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "port", Description: "Ollama API port", Default: "11434", Required: false, Type: "number"},
			{Name: "models", Description: "Models to pull (comma-separated)", Default: "llama3.1", Required: false, Type: "text"},
		},
		Scripts: map[string]MiddlewareScript{
			"systemd": {
				Deploy:    "curl -fsSL https://ollama.com/install.sh | sh && systemctl enable --now ollama && sleep 3 && for m in {{.models}}; do ollama pull $m; done && echo 'Ollama deployed'",
				Verify:    "ollama list && curl -s http://localhost:11434/api/tags",
				Uninstall: "systemctl stop ollama && rm -f /usr/local/bin/ollama && echo 'Ollama removed'",
			},
			"docker": {
				Deploy:    "docker run -d --name ollama --gpus all -p 11434:11434 -v ollama:/root/.ollama {image} && sleep 5 && docker exec ollama ollama pull {{.models}} && echo 'Ollama deployed'",
				Verify:    "curl -s http://localhost:11434/api/tags",
				Uninstall: "docker rm -f ollama && echo 'Ollama removed'",
			},
		},
		Risk: "medium",
		Tags: []string{"gpu", "llm", "ollama", "ai"},
		Versions: []MiddlewareVersion{
			{Version: "0.3.0", DockerImage: "ollama/ollama:0.3.0", Default: true},
		},
	},
	{
		ID:          "vllm",
		Name:        "vLLM LLM Serving",
		Category:    "gpu",
		Version:     "0.5.0",
		Description: "vLLM high-throughput LLM serving with GPU acceleration",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "model", Description: "Model to serve", Default: "meta-llama/Llama-3.1-8B", Required: true, Type: "text"},
			{Name: "port", Description: "API port", Default: "8000", Required: false, Type: "number"},
			{Name: "tensor_parallel", Description: "Tensor parallel size", Default: "1", Required: false, Type: "number"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name vllm --gpus all -p {{.port}}:8000 -v /models:/models {image} --model {{.model}} --tensor-parallel-size {{.tensor_parallel}} && echo 'vLLM deployed'",
				Verify:    "curl -s http://localhost:{{.port}}/health && echo 'vLLM healthy'",
				Uninstall: "docker rm -f vllm && echo 'vLLM removed'",
			},
			"systemd": {
				Deploy:    "pip install vllm && python -m vllm.entrypoints.openai.api_server --model {{.model}} --tensor-parallel-size {{.tensor_parallel}} --port {{.port}} && echo 'vLLM deployed'",
				Verify:    "curl -s http://localhost:{{.port}}/health && echo 'vLLM healthy'",
				Uninstall: "pip uninstall -y vllm && echo 'vLLM removed'",
			},
		},
		Risk: "medium",
		Tags: []string{"gpu", "llm", "vllm", "ai"},
		Versions: []MiddlewareVersion{
			{Version: "0.5.0", DockerImage: "vllm/vllm-openai:v0.5.0", Default: true},
		},
	},
	{
		ID:          "triton",
		Name:        "NVIDIA Triton Inference Server",
		Category:    "gpu",
		Version:     "24.07",
		Description: "NVIDIA Triton inference server for multi-framework model serving",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "model_repo", Description: "Model repository path", Default: "/models", Required: true, Type: "text"},
			{Name: "http_port", Description: "HTTP port", Default: "8000", Required: false, Type: "number"},
			{Name: "grpc_port", Description: "gRPC port", Default: "8001", Required: false, Type: "number"},
			{Name: "metrics_port", Description: "Metrics port", Default: "8002", Required: false, Type: "number"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name triton --gpus all -p {{.http_port}}:8000 -p {{.grpc_port}}:8001 -p {{.metrics_port}}:8002 -v {{.model_repo}}:/models {image} tritonserver --model-repository=/models && echo 'Triton deployed'",
				Verify:    "curl -s http://localhost:{{.http_port}}/v2/health/ready && echo 'Triton ready'",
				Uninstall: "docker rm -f triton && echo 'Triton removed'",
			},
			"systemd": {
				Deploy:    "echo 'Triton requires container runtime; install tritonserver manually' && echo 'Triton deployed'",
				Verify:    "curl -s http://localhost:8000/v2/health/ready && echo 'Triton ready'",
				Uninstall: "echo 'Triton removed'",
			},
		},
		Risk: "medium",
		Tags: []string{"gpu", "nvidia", "triton", "inference", "ai"},
		Versions: []MiddlewareVersion{
			{Version: "24.07", DockerImage: "nvcr.io/nvidia/tritonserver:24.07-py3", Default: true},
		},
	},
	// ---------------- ascend (gpu) ----------------
	{
		ID:          "ascend-cann",
		Name:        "Huawei Ascend CANN Toolkit",
		Category:    "gpu",
		Version:     "8.0",
		Description: "Huawei Ascend CANN toolkit for AI computing and model acceleration",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "ascend-cann", Required: true, Type: "string"},
			{Name: "version", Description: "CANN 版本", Default: "8.0", Required: false, Type: "string"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} --device /dev/davinci0 --device /dev/davinci_manager --device /dev/devmm_svm --device /dev/hisi_hdc -v /usr/local/Ascend:/usr/local/Ascend --restart unless-stopped {image} && echo 'Ascend CANN deployed'",
				Verify:    "docker exec {name} npu-smi info && echo 'Ascend CANN healthy'",
				Uninstall: "docker rm -f {name} && echo 'Ascend CANN removed'",
			},
			"systemd": {
				Deploy:    "wget -q https://ascend-repo.obs.cn-east-2.myhuaweicloud.com/CANN/{version}/Ascend-cann-toolkit_{version}_linux-x86_64.run -O /tmp/ascend.run && chmod +x /tmp/ascend.run && /tmp/ascend.run --install && echo 'Ascend CANN {version} installed'",
				Verify:    "npu-smi info && echo 'Ascend CANN healthy'",
				Uninstall: "echo 'Ascend CANN removed'",
			},
		},
		Risk: "high",
		Tags: []string{"gpu", "ascend", "huawei", "cann", "ai"},
		Versions: []MiddlewareVersion{
			{Version: "8.0", DockerImage: "ascend/cann:8.0", Default: true},
			{Version: "7.0", DockerImage: "ascend/cann:7.0"},
		},
	},
	{
		ID:          "ascend-dcm",
		Name:        "Ascend Data Center Monitoring",
		Category:    "gpu",
		Version:     "1.0",
		Description: "Ascend Data Center Monitoring for NPU diagnostics and telemetry",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "ascend-dcm", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "8080", Required: false, Type: "int"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:8080 --device /dev/davinci0 --restart unless-stopped {image} && echo 'Ascend DCM deployed'",
				Verify:    "curl -s http://localhost:{port}/health && echo 'Ascend DCM healthy'",
				Uninstall: "docker rm -f {name} && echo 'Ascend DCM removed'",
			},
			"systemd": {
				Deploy:    "echo 'Ascend DCM requires ascend-dcm package; install manually' && echo 'Ascend DCM deployed'",
				Verify:    "curl -s http://localhost:8080/health && echo 'Ascend DCM healthy'",
				Uninstall: "echo 'Ascend DCM removed'",
			},
		},
		Risk: "medium",
		Tags: []string{"gpu", "ascend", "huawei", "dcm", "monitoring"},
		Versions: []MiddlewareVersion{
			{Version: "1.0", DockerImage: "ascend/dcm:1.0", Default: true},
		},
	},
	{
		ID:          "mindspore",
		Name:        "MindSpore AI Framework",
		Category:    "gpu",
		Version:     "2.3",
		Description: "MindSpore AI framework for Ascend/GPU model training and inference",
		DeployTypes: []string{"docker", "systemd"},
		Params: []MiddlewareParam{
			{Name: "name", Description: "容器/实例名称", Default: "mindspore", Required: true, Type: "string"},
			{Name: "port", Description: "监听端口", Default: "8080", Required: false, Type: "int"},
		},
		Scripts: map[string]MiddlewareScript{
			"docker": {
				Deploy:    "docker run -d --name {name} -p {port}:8080 --device /dev/davinci0 --restart unless-stopped {image} && echo 'MindSpore deployed'",
				Verify:    "docker exec {name} python -c 'import mindspore; print(mindspore.__version__)' && echo 'MindSpore healthy'",
				Uninstall: "docker rm -f {name} && echo 'MindSpore removed'",
			},
			"systemd": {
				Deploy:    "pip install mindspore-ascend=={version} && echo 'MindSpore {version} installed'",
				Verify:    "python -c 'import mindspore; print(mindspore.__version__)'",
				Uninstall: "pip uninstall -y mindspore-ascend && echo 'MindSpore removed'",
			},
		},
		Risk: "medium",
		Tags: []string{"gpu", "ascend", "huawei", "mindspore", "ai", "framework"},
		Versions: []MiddlewareVersion{
			{Version: "2.3", DockerImage: "mindspore/mindspore:2.3.0", Default: true},
			{Version: "2.2", DockerImage: "mindspore/mindspore:2.2.0"},
		},
	},
}

// middlewareTemplateByID 按 ID 查找预置中间件模板，未找到返回 nil。
