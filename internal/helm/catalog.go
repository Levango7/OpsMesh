// catalog.go 实现预置应用商店，提供 20+ 常用 Helm 应用的目录定义与查询能力。
//
// 设计要点：
//   - CatalogItem 描述单个应用条目（ID/名称/分类/chart/仓库/版本/图标/默认值）；
//   - DefaultCatalog 是预置目录（24 个应用，覆盖 9 个分类）；
//   - 提供 GetCatalogItem/ListByCategory/ListCategories/SearchCatalog 等查询函数；
//   - 所有数据为纯静态，不依赖 helm CLI，可离线使用。

package helm

import (
	"fmt"
	"strings"
)

// CatalogCategory 表示应用分类。
type CatalogCategory string

const (
	CategoryDatabase CatalogCategory = "database" // 数据库
	CategoryCache    CatalogCategory = "cache"    // 缓存
	CategoryMQ       CatalogCategory = "mq"       // 消息队列
	CategoryWeb      CatalogCategory = "web"      // Web 服务器
	CategoryMonitor  CatalogCategory = "monitor"  // 监控
	CategoryStorage  CatalogCategory = "storage"  // 存储
	CategoryNetwork  CatalogCategory = "network"  // 网络
	CategorySecurity CatalogCategory = "security" // 安全
	CategoryCI       CatalogCategory = "ci"       // CI/CD
)

// CatalogItem 描述应用商店中的一个应用条目。
type CatalogItem struct {
	ID            string                 `json:"id"            yaml:"id"`
	Name          string                 `json:"name"          yaml:"name"`
	Category      CatalogCategory        `json:"category"      yaml:"category"`
	Chart         string                 `json:"chart"         yaml:"chart"`   // chart 名称
	Repo          string                 `json:"repo"          yaml:"repo"`    // 仓库名
	Version       string                 `json:"version"       yaml:"version"` // 推荐版本
	Icon          string                 `json:"icon"          yaml:"icon"`    // 图标 URL
	Description   string                 `json:"description"   yaml:"description"`
	DefaultValues map[string]interface{} `json:"defaultValues,omitempty" yaml:"defaultValues,omitempty"`
	// Maintainer 维护者（仅展示用）。
	Maintainer string `json:"maintainer,omitempty" yaml:"maintainer,omitempty"`
	// HomePage 项目主页。
	HomePage string `json:"homePage,omitempty" yaml:"homePage,omitempty"`
}

// DefaultCatalog 是预置应用商店目录（24 个应用，覆盖 9 个分类）。
//
// 所有 chart 默认来自 bitnami 仓库（https://charts.bitnami.com/bitnami），
// 部分独立项目（prometheus/grafana/loki/fluent-bit/ingress-nginx/cert-manager/
// istio/vault/keycloak/jenkins）使用各自官方仓库。
var DefaultCatalog = []*CatalogItem{
	// ========== 数据库（database） ==========
	{
		ID:          "mysql",
		Name:        "MySQL",
		Category:    CategoryDatabase,
		Chart:       "mysql",
		Repo:        "bitnami",
		Version:     "9.10.0",
		Icon:        "https://bitnami.com/assets/stacks/mysql/img/mysql-stack-220x234.png",
		Description: "MySQL 是快速、可靠、可扩展的开源关系数据库管理系统。",
		Maintainer:  "Bitnami",
		HomePage:    "https://mysql.com",
		DefaultValues: map[string]interface{}{
			"auth": map[string]interface{}{
				"rootPassword": "change-me",
				"database":     "appdb",
			},
			"primary": map[string]interface{}{
				"persistence": map[string]interface{}{
					"size": "8Gi",
				},
			},
		},
	},
	{
		ID:          "postgresql",
		Name:        "PostgreSQL",
		Category:    CategoryDatabase,
		Chart:       "postgresql",
		Repo:        "bitnami",
		Version:     "13.4.0",
		Icon:        "https://bitnami.com/assets/stacks/postgresql/img/postgresql-stack-220x234.png",
		Description: "PostgreSQL 是功能强大的开源对象关系数据库系统。",
		Maintainer:  "Bitnami",
		HomePage:    "https://postgresql.org",
		DefaultValues: map[string]interface{}{
			"auth": map[string]interface{}{
				"postgresPassword": "change-me",
			},
			"primary": map[string]interface{}{
				"persistence": map[string]interface{}{
					"size": "8Gi",
				},
			},
		},
	},
	{
		ID:          "mongodb",
		Name:        "MongoDB",
		Category:    CategoryDatabase,
		Chart:       "mongodb",
		Repo:        "bitnami",
		Version:     "14.0.0",
		Icon:        "https://bitnami.com/assets/stacks/mongodb/img/mongodb-stack-220x234.png",
		Description: "MongoDB 是面向文档的 NoSQL 数据库。",
		Maintainer:  "Bitnami",
		HomePage:    "https://mongodb.com",
	},
	{
		ID:          "mariadb",
		Name:        "MariaDB",
		Category:    CategoryDatabase,
		Chart:       "mariadb",
		Repo:        "bitnami",
		Version:     "13.0.0",
		Icon:        "https://bitnami.com/assets/stacks/mariadb/img/mariadb-stack-220x234.png",
		Description: "MariaDB 是 MySQL 的社区开发分支，高性能且兼容。",
		Maintainer:  "Bitnami",
		HomePage:    "https://mariadb.org",
	},
	{
		ID:          "elasticsearch",
		Name:        "Elasticsearch",
		Category:    CategoryStorage,
		Chart:       "elasticsearch",
		Repo:        "bitnami",
		Version:     "19.13.0",
		Icon:        "https://bitnami.com/assets/stacks/elasticsearch/img/elasticsearch-stack-220x234.png",
		Description: "Elasticsearch 是分布式搜索与分析引擎。",
		Maintainer:  "Bitnami",
		HomePage:    "https://elastic.co",
	},

	// ========== 缓存（cache） ==========
	{
		ID:          "redis",
		Name:        "Redis",
		Category:    CategoryCache,
		Chart:       "redis",
		Repo:        "bitnami",
		Version:     "18.0.0",
		Icon:        "https://bitnami.com/assets/stacks/redis/img/redis-stack-220x234.png",
		Description: "Redis 是高性能内存数据结构存储，用作数据库、缓存与消息代理。",
		Maintainer:  "Bitnami",
		HomePage:    "https://redis.io",
		DefaultValues: map[string]interface{}{
			"auth": map[string]interface{}{
				"password": "change-me",
			},
			"master": map[string]interface{}{
				"persistence": map[string]interface{}{
					"size": "8Gi",
				},
			},
		},
	},
	{
		ID:          "memcached",
		Name:        "Memcached",
		Category:    CategoryCache,
		Chart:       "memcached",
		Repo:        "bitnami",
		Version:     "6.5.0",
		Icon:        "https://bitnami.com/assets/stacks/memcached/img/memcached-stack-220x234.png",
		Description: "Memcached 是高性能分布式内存缓存系统。",
		Maintainer:  "Bitnami",
		HomePage:    "https://memcached.org",
	},

	// ========== 消息队列（mq） ==========
	{
		ID:          "kafka",
		Name:        "Kafka",
		Category:    CategoryMQ,
		Chart:       "kafka",
		Repo:        "bitnami",
		Version:     "25.0.0",
		Icon:        "https://bitnami.com/assets/stacks/kafka/img/kafka-stack-220x234.png",
		Description: "Apache Kafka 是高吞吐量分布式事件流平台。",
		Maintainer:  "Bitnami",
		HomePage:    "https://kafka.apache.org",
	},
	{
		ID:          "rabbitmq",
		Name:        "RabbitMQ",
		Category:    CategoryMQ,
		Chart:       "rabbitmq",
		Repo:        "bitnami",
		Version:     "14.0.0",
		Icon:        "https://bitnami.com/assets/stacks/rabbitmq/img/rabbitmq-stack-220x234.png",
		Description: "RabbitMQ 是可靠的消息代理，支持 AMQP/MQTT/STOMP。",
		Maintainer:  "Bitnami",
		HomePage:    "https://rabbitmq.com",
	},
	{
		ID:          "nats",
		Name:        "NATS",
		Category:    CategoryMQ,
		Chart:       "nats",
		Repo:        "bitnami",
		Version:     "7.7.0",
		Icon:        "https://bitnami.com/assets/stacks/nats/img/nats-stack-220x234.png",
		Description: "NATS 是轻量级高性能云原生消息系统。",
		Maintainer:  "Bitnami",
		HomePage:    "https://nats.io",
	},

	// ========== Web 服务器（web） ==========
	{
		ID:          "nginx",
		Name:        "Nginx",
		Category:    CategoryWeb,
		Chart:       "nginx",
		Repo:        "bitnami",
		Version:     "16.0.0",
		Icon:        "https://bitnami.com/assets/stacks/nginx/img/nginx-stack-220x234.png",
		Description: "Nginx 是高性能 HTTP 服务器与反向代理。",
		Maintainer:  "Bitnami",
		HomePage:    "https://nginx.org",
	},
	{
		ID:          "apache",
		Name:        "Apache HTTPD",
		Category:    CategoryWeb,
		Chart:       "apache",
		Repo:        "bitnami",
		Version:     "9.6.0",
		Icon:        "https://bitnami.com/assets/stacks/apache/img/apache-stack-220x234.png",
		Description: "Apache HTTP Server 是开源 Web 服务器。",
		Maintainer:  "Bitnami",
		HomePage:    "https://httpd.apache.org",
	},
	{
		ID:          "tomcat",
		Name:        "Tomcat",
		Category:    CategoryWeb,
		Chart:       "tomcat",
		Repo:        "bitnami",
		Version:     "11.0.0",
		Icon:        "https://bitnami.com/assets/stacks/tomcat/img/tomcat-stack-220x234.png",
		Description: "Apache Tomcat 是 Java Servlet/JSP 容器。",
		Maintainer:  "Bitnami",
		HomePage:    "https://tomcat.apache.org",
	},

	// ========== 监控（monitor） ==========
	{
		ID:          "prometheus",
		Name:        "Prometheus",
		Category:    CategoryMonitor,
		Chart:       "prometheus",
		Repo:        "prometheus-community",
		Version:     "25.0.0",
		Icon:        "https://prometheus.io/assets/favicons/favicon-192x192.png",
		Description: "Prometheus 是云原生监控系统与时间序列数据库。",
		Maintainer:  "Prometheus Community",
		HomePage:    "https://prometheus.io",
		DefaultValues: map[string]interface{}{
			"server": map[string]interface{}{
				"persistentVolume": map[string]interface{}{
					"size": "8Gi",
				},
			},
		},
	},
	{
		ID:          "grafana",
		Name:        "Grafana",
		Category:    CategoryMonitor,
		Chart:       "grafana",
		Repo:        "grafana",
		Version:     "8.0.0",
		Icon:        "https://grafana.com/static/img/logos/grafana-logo.svg",
		Description: "Grafana 是开源可视化与分析平台。",
		Maintainer:  "Grafana Labs",
		HomePage:    "https://grafana.com",
	},
	{
		ID:          "loki",
		Name:        "Loki",
		Category:    CategoryMonitor,
		Chart:       "loki",
		Repo:        "grafana",
		Version:     "6.0.0",
		Icon:        "https://grafana.com/static/img/logos/loki-logo.svg",
		Description: "Loki 是水平可扩展的日志聚合系统。",
		Maintainer:  "Grafana Labs",
		HomePage:    "https://grafana.com/oss/loki",
	},
	{
		ID:          "fluent-bit",
		Name:        "Fluent Bit",
		Category:    CategoryMonitor,
		Chart:       "fluent-bit",
		Repo:        "fluent",
		Version:     "0.40.0",
		Icon:        "https://fluentbit.io/images/logo_small.png",
		Description: "Fluent Bit 是轻量级日志与数据处理器。",
		Maintainer:  "Fluent",
		HomePage:    "https://fluentbit.io",
	},

	// ========== 存储（storage） ==========
	{
		ID:          "minio",
		Name:        "MinIO",
		Category:    CategoryStorage,
		Chart:       "minio",
		Repo:        "bitnami",
		Version:     "13.0.0",
		Icon:        "https://min.io/resources/img/logo.svg",
		Description: "MinIO 是 S3 兼容的高性能对象存储。",
		Maintainer:  "Bitnami",
		HomePage:    "https://min.io",
	},
	{
		ID:          "ceph",
		Name:        "Ceph",
		Category:    CategoryStorage,
		Chart:       "ceph-csi-rbd",
		Repo:        "ceph-csi",
		Version:     "3.10.0",
		Icon:        "https://ceph.io/assets/ceph-logo-8c8c8c8c.svg",
		Description: "Ceph 是分布式存储系统，提供块/对象/文件存储。",
		Maintainer:  "Ceph Community",
		HomePage:    "https://ceph.io",
	},

	// ========== 网络（network） ==========
	{
		ID:          "ingress-nginx",
		Name:        "Ingress Nginx",
		Category:    CategoryNetwork,
		Chart:       "ingress-nginx",
		Repo:        "ingress-nginx",
		Version:     "4.10.0",
		Icon:        "https://kubernetes.github.io/ingress-nginx/images/ingress-nginx-logo.svg",
		Description: "NGINX Ingress Controller 是 Kubernetes 入口控制器。",
		Maintainer:  "Kubernetes Community",
		HomePage:    "https://kubernetes.github.io/ingress-nginx",
	},
	{
		ID:          "cert-manager",
		Name:        "Cert Manager",
		Category:    CategoryNetwork,
		Chart:       "cert-manager",
		Repo:        "jetstack",
		Version:     "1.14.0",
		Icon:        "https://cert-manager.io/images/logo.svg",
		Description: "cert-manager 是 Kubernetes 证书管理控制器。",
		Maintainer:  "Jetstack",
		HomePage:    "https://cert-manager.io",
	},
	{
		ID:          "istio",
		Name:        "Istio",
		Category:    CategoryNetwork,
		Chart:       "istio-base",
		Repo:        "istio",
		Version:     "1.20.0",
		Icon:        "https://istio.io/latest/favicons/android-192x192.png",
		Description: "Istio 是服务网格，提供流量管理/安全/可观测性。",
		Maintainer:  "Istio Community",
		HomePage:    "https://istio.io",
	},
	{
		ID:          "metallb",
		Name:        "MetalLB",
		Category:    CategoryNetwork,
		Chart:       "metallb",
		Repo:        "metallb",
		Version:     "0.14.0",
		Icon:        "https://metallb.universe.tf/images/logo.png",
		Description: "MetalLB 是裸机 Kubernetes 负载均衡器。",
		Maintainer:  "MetalLB Community",
		HomePage:    "https://metallb.universe.tf",
	},

	// ========== 安全（security） ==========
	{
		ID:          "vault",
		Name:        "HashiCorp Vault",
		Category:    CategorySecurity,
		Chart:       "vault",
		Repo:        "hashicorp",
		Version:     "0.27.0",
		Icon:        "https://vaultproject.io/img/vault-logo.png",
		Description: "Vault 是密钥管理与机密存储工具。",
		Maintainer:  "HashiCorp",
		HomePage:    "https://vaultproject.io",
	},
	{
		ID:          "keycloak",
		Name:        "Keycloak",
		Category:    CategorySecurity,
		Chart:       "keycloak",
		Repo:        "bitnami",
		Version:     "19.0.0",
		Icon:        "https://www.keycloak.org/resources/images/keycloak_icon_512px.svg",
		Description: "Keycloak 是开源身份与访问管理（IAM）系统。",
		Maintainer:  "Bitnami",
		HomePage:    "https://keycloak.org",
	},

	// ========== CI/CD（ci） ==========
	{
		ID:          "jenkins",
		Name:        "Jenkins",
		Category:    CategoryCI,
		Chart:       "jenkins",
		Repo:        "jenkins",
		Version:     "4.12.0",
		Icon:        "https://get.jenkins.io/images/logos/jenkins.svg",
		Description: "Jenkins 是开源自动化服务器，支持 CI/CD。",
		Maintainer:  "Jenkins Community",
		HomePage:    "https://jenkins.io",
	},
	{
		ID:          "argocd",
		Name:        "Argo CD",
		Category:    CategoryCI,
		Chart:       "argo-cd",
		Repo:        "argoproj",
		Version:     "6.0.0",
		Icon:        "https://argo-cd.readthedocs.io/en/stable/assets/logo.png",
		Description: "Argo CD 是 GitOps 持续交付工具。",
		Maintainer:  "Argo Project",
		HomePage:    "https://argo-cd.readthedocs.io",
	},
}

// =============================================================================
// 目录查询函数
// =============================================================================

// GetCatalogItem 按 ID 在 DefaultCatalog 中查找条目。
//
// 未找到返回 nil。
func GetCatalogItem(id string) *CatalogItem {
	for _, item := range DefaultCatalog {
		if item.ID == id {
			return item
		}
	}
	return nil
}

// ListByCategory 按分类列出目录条目。
//
// category 为空时返回全部条目。
func ListByCategory(category CatalogCategory) []*CatalogItem {
	if category == "" {
		out := make([]*CatalogItem, len(DefaultCatalog))
		copy(out, DefaultCatalog)
		return out
	}
	out := make([]*CatalogItem, 0)
	for _, item := range DefaultCatalog {
		if item.Category == category {
			out = append(out, item)
		}
	}
	return out
}

// ListCategories 返回目录中出现的所有分类（去重，按首次出现顺序）。
func ListCategories() []CatalogCategory {
	seen := make(map[CatalogCategory]bool)
	out := make([]CatalogCategory, 0, 8)
	for _, item := range DefaultCatalog {
		if !seen[item.Category] {
			seen[item.Category] = true
			out = append(out, item.Category)
		}
	}
	return out
}

// SearchCatalog 在目录中搜索条目（匹配 ID/Name/Description/Chart）。
//
// keyword 为空时返回全部；匹配大小写不敏感。
func SearchCatalog(keyword string) []*CatalogItem {
	if keyword == "" {
		out := make([]*CatalogItem, len(DefaultCatalog))
		copy(out, DefaultCatalog)
		return out
	}
	kw := strings.ToLower(keyword)
	out := make([]*CatalogItem, 0)
	for _, item := range DefaultCatalog {
		if strings.Contains(strings.ToLower(item.ID), kw) ||
			strings.Contains(strings.ToLower(item.Name), kw) ||
			strings.Contains(strings.ToLower(item.Description), kw) ||
			strings.Contains(strings.ToLower(item.Chart), kw) {
			out = append(out, item)
		}
	}
	return out
}

// CatalogStats 是目录统计信息。
type CatalogStats struct {
	Total      int                     // 总条目数
	ByCategory map[CatalogCategory]int // 按分类统计
}

// CatalogStatistics 计算 DefaultCatalog 的统计信息。
func CatalogStatistics() *CatalogStats {
	st := &CatalogStats{ByCategory: make(map[CatalogCategory]int)}
	for _, item := range DefaultCatalog {
		st.Total++
		st.ByCategory[item.Category]++
	}
	return st
}

// DefaultRepoURLs 返回 DefaultCatalog 引用的所有仓库 URL（去重）。
//
// 用于一次性 helm repo add 所有需要的仓库。
func DefaultRepoURLs() map[string]string {
	return map[string]string{
		"bitnami":              "https://charts.bitnami.com/bitnami",
		"prometheus-community": "https://prometheus-community.github.io/helm-charts",
		"grafana":              "https://grafana.github.io/helm-charts",
		"fluent":               "https://fluent.github.io/helm-charts",
		"ceph-csi":             "https://ceph.github.io/csi-charts",
		"ingress-nginx":        "https://kubernetes.github.io/ingress-nginx",
		"jetstack":             "https://charts.jetstack.io",
		"istio":                "https://istio-release.storage.googleapis.com/charts",
		"metallb":              "https://metallb.github.io/metallb",
		"hashicorp":            "https://helm.releases.hashicorp.com",
		"jenkins":              "https://charts.jenkins.io",
		"argoproj":             "https://argoproj.github.io/argo-helm",
	}
}

// EnsureDefaultRepos 将 DefaultRepoURLs 中的所有仓库添加到 RepoManager。
//
// 已存在的仓库会被跳过（不报错）。返回成功添加的仓库名列表。
func EnsureDefaultRepos(m *RepoManager) ([]string, error) {
	if m == nil {
		return nil, fmt.Errorf("helm/catalog: RepoManager 为 nil")
	}
	urls := DefaultRepoURLs()
	added := make([]string, 0, len(urls))
	for name, url := range urls {
		// 已存在则跳过。
		if _, err := m.GetRepo(name); err == nil {
			continue
		}
		if err := m.AddRepo(&ChartRepo{Name: name, URL: url, Type: RepoTypeHTTP}); err != nil {
			return added, fmt.Errorf("helm/catalog: 添加默认仓库 %q 失败: %w", name, err)
		}
		added = append(added, name)
	}
	return added, nil
}
