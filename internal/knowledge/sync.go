// Package knowledge 提供知识库同步、混合检索、重排序、引用溯源能力。
package knowledge

import (
	"context"
	"fmt"
	"sync"
	"time"

	gateway "github.com/Levango7/OpsMesh/internal/ai/gateway"
)

// KnowledgeSource 知识源类型
type KnowledgeSource string

const (
	SourceCMDB         KnowledgeSource = "cmdb"
	SourceAlert        KnowledgeSource = "alert"
	SourceRunbook      KnowledgeSource = "runbook"
	SourceGit          KnowledgeSource = "git"
	SourceConfluence    KnowledgeSource = "confluence"
	SourceManual        KnowledgeSource = "manual"
)

// KnowledgeDoc 知识文档
type KnowledgeDoc struct {
	ID          string                 `json:"id"`
	Source      KnowledgeSource         `json:"source"`
	Title       string                 `json:"title"`
	Content     string                 `json:"content"`
	Embedding   []float64              `json:"embedding,omitempty"`
	Score       float64                `json:"score,omitempty"`
	Metadata    map[string]interface{} `json:"metadata"`
	CreatedAt   time.Time              `json:"createdAt"`
	UpdatedAt   time.Time              `json:"updatedAt"`
	Citation    *Citation              `json:"citation,omitempty"`
}

// Citation 引用溯源
type Citation struct {
	Source      KnowledgeSource `json:"source"`
	SourceID    string         `json:"sourceId"`
	SourceURL   string         `json:"sourceUrl,omitempty"`
	Excerpt     string         `json:"excerpt"`
	Relevance   float64        `json:"relevance"`
}

// SyncConfig 同步配置
type SyncConfig struct {
	Source      KnowledgeSource `json:"source"`
	Enabled     bool           `json:"enabled"`
	Interval    time.Duration  `json:"interval"`
	LastSync    time.Time      `json:"lastSync"`
	BatchSize   int            `json:"batchSize"`
}

// SyncEngine 知识同步引擎
type SyncEngine struct {
	mu          sync.RWMutex
	configs     map[KnowledgeSource]*SyncConfig
	store       KnowledgeStore
	gw     *gateway.Gateway
	syncFuncs   map[KnowledgeSource]SyncFunc
}

// SyncFunc 同步函数类型
type SyncFunc func(ctx context.Context, store KnowledgeStore, gw *gateway.Gateway) (int, error)

// KnowledgeStore 知识存储接口
type KnowledgeStore interface {
	Upsert(ctx context.Context, docs []KnowledgeDoc) error
	Search(ctx context.Context, query string, topK int, filter map[string]interface{}) ([]KnowledgeDoc, error)
	Delete(ctx context.Context, ids []string) error
	Count(ctx context.Context) (int, error)
	ListBySource(ctx context.Context, source KnowledgeSource) ([]KnowledgeDoc, error)
}

// NewSyncEngine 创建同步引擎
func NewSyncEngine(store KnowledgeStore, gw *gateway.Gateway) *SyncEngine {
	engine := &SyncEngine{
		configs:   make(map[KnowledgeSource]*SyncConfig),
		store:     store,
		gw:   gw,
		syncFuncs: make(map[KnowledgeSource]SyncFunc),
	}

	// 注册默认同步函数
	engine.RegisterSyncFunc(SourceCMDB, syncCMDB)
	engine.RegisterSyncFunc(SourceAlert, syncAlerts)
	engine.RegisterSyncFunc(SourceRunbook, syncRunbooks)
	engine.RegisterSyncFunc(SourceGit, syncGit)
	engine.RegisterSyncFunc(SourceConfluence, syncConfluence)

	// 默认配置
	for _, src := range []KnowledgeSource{SourceCMDB, SourceAlert, SourceRunbook, SourceGit, SourceConfluence} {
		engine.configs[src] = &SyncConfig{
			Source:    src,
			Enabled:   true,
			Interval:  1 * time.Hour,
			BatchSize: 100,
		}
	}

	return engine
}

func (e *SyncEngine) RegisterSyncFunc(source KnowledgeSource, fn SyncFunc) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.syncFuncs[source] = fn
}

// SyncNow 立即同步指定源
func (e *SyncEngine) SyncNow(ctx context.Context, source KnowledgeSource) (int, error) {
	e.mu.RLock()
	fn, ok := e.syncFuncs[source]
	cfg, cfgOk := e.configs[source]
	e.mu.RUnlock()

	if !ok {
		return 0, fmt.Errorf("sync function not registered for source %s", source)
	}
	if !cfgOk || !cfg.Enabled {
		return 0, fmt.Errorf("source %s is disabled", source)
	}

	count, err := fn(ctx, e.store, e.gw)
	if err != nil {
		return 0, err
	}

	e.mu.Lock()
	cfg.LastSync = time.Now()
	e.mu.Unlock()

	return count, nil
}

// SyncAll 同步所有启用的源
func (e *SyncEngine) SyncAll(ctx context.Context) (map[KnowledgeSource]int, error) {
	results := make(map[KnowledgeSource]int)
	for source := range e.configs {
		if !e.configs[source].Enabled {
			continue
		}
		count, err := e.SyncNow(ctx, source)
		if err != nil {
			results[source] = -1
			continue
		}
		results[source] = count
	}
	return results, nil
}

// StartScheduler 启动定时同步
func (e *SyncEngine) StartScheduler(ctx context.Context) {
	for source, cfg := range e.configs {
		if !cfg.Enabled {
			continue
		}
		go func(src KnowledgeSource, interval time.Duration) {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					count, err := e.SyncNow(ctx, src)
					if err != nil {
						fmt.Printf("sync %s failed: %v\n", src, err)
					} else {
						fmt.Printf("sync %s completed: %d docs\n", src, count)
					}
				}
			}
		}(source, cfg.Interval)
	}
}

// GetConfigs 获取同步配置
func (e *SyncEngine) GetConfigs() []*SyncConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]*SyncConfig, 0, len(e.configs))
	for _, c := range e.configs {
		cp := *c
		out = append(out, &cp)
	}
	return out
}

// UpdateConfig 更新同步配置
func (e *SyncEngine) UpdateConfig(source KnowledgeSource, enabled bool, interval time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if cfg, ok := e.configs[source]; ok {
		cfg.Enabled = enabled
		if interval > 0 {
			cfg.Interval = interval
		}
	}
}

// ============================================================================
// 默认同步函数 (模拟实现，实际应调用对应服务 API)
// ============================================================================

func syncCMDB(ctx context.Context, store KnowledgeStore, gw *gateway.Gateway) (int, error) {
	// 模拟：从 CMDB 获取 CI 数据并转为知识文档
	docs := []KnowledgeDoc{
		{
			ID:     "cmdb-ci-web-01",
			Source: SourceCMDB,
			Title:  "Web Server web-01",
			Content: "Device: web-01, IP: 192.168.1.10, OS: Ubuntu 22.04, CPU: 8 cores, Memory: 32GB, Status: online, Agent: installed",
			Metadata: map[string]interface{}{
				"ci_type": "server",
				"status":  "online",
				"ip":      "192.168.1.10",
			},
		},
		{
			ID:     "cmdb-ci-db-01",
			Source: SourceCMDB,
			Title:  "Database Server db-01",
			Content: "Device: db-01, IP: 192.168.1.20, OS: CentOS 8, CPU: 16 cores, Memory: 64GB, Status: online, Database: PostgreSQL 15",
			Metadata: map[string]interface{}{
				"ci_type": "database",
				"status":  "online",
				"ip":      "192.168.1.20",
			},
		},
	}
	// 生成 embedding
	for i := range docs {
		emb, err := gw.Embed(ctx, "openai", docs[i].Content)
		if err != nil {
			continue
		}
		docs[i].Embedding = emb
		docs[i].CreatedAt = time.Now()
		docs[i].UpdatedAt = time.Now()
	}
	return len(docs), store.Upsert(ctx, docs)
}

func syncAlerts(ctx context.Context, store KnowledgeStore, gw *gateway.Gateway) (int, error) {
	docs := []KnowledgeDoc{
		{
			ID:     "alert-cpu-high-web01",
			Source: SourceAlert,
			Title:  "CPU 使用率过高 - web-01",
			Content: "告警: web-01 CPU 使用率持续超过 90% (阈值 80%)，持续 15 分钟。可能原因：1. 应用进程 CPU 占用 2. 定时任务执行 3. 资源不足",
			Metadata: map[string]interface{}{
				"severity": "critical",
				"device":   "web-01",
				"metric":   "cpu_usage",
			},
		},
		{
			ID:     "alert-disk-full-db01",
			Source: SourceAlert,
			Title:  "磁盘空间不足 - db-01",
			Content: "告警: db-01 根分区使用率 95% (阈值 90%)。可能原因：1. 日志文件过大 2. 数据库 WAL 日志堆积 3. 临时文件未清理",
			Metadata: map[string]interface{}{
				"severity": "warning",
				"device":   "db-01",
				"metric":   "disk_usage",
			},
		},
	}
	for i := range docs {
		emb, err := gw.Embed(ctx, "openai", docs[i].Content)
		if err != nil {
			continue
		}
		docs[i].Embedding = emb
		docs[i].CreatedAt = time.Now()
		docs[i].UpdatedAt = time.Now()
	}
	return len(docs), store.Upsert(ctx, docs)
}

func syncRunbooks(ctx context.Context, store KnowledgeStore, gw *gateway.Gateway) (int, error) {
	docs := []KnowledgeDoc{
		{
			ID:     "runbook-nginx-502",
			Source: SourceRunbook,
			Title:  "Nginx 502 Bad Gateway 排查指南",
			Content: "1. 检查 upstream 配置是否正确\n2. 检查后端服务是否运行 (systemctl status nginx)\n3. 检查后端端口是否监听 (ss -tlnp)\n4. 查看错误日志 (tail -f /var/log/nginx/error.log)\n5. 检查 SELinux/AppArmor 是否阻止\n6. 重启 Nginx (systemctl restart nginx)",
			Metadata: map[string]interface{}{
				"category": "web",
				"severity": "high",
			},
		},
		{
			ID:     "runbook-cpu-high",
			Source: SourceRunbook,
			Title:  "CPU 使用率过高处理指南",
			Content: "1. top/htop 查看高 CPU 进程\n2. 检查定时任务 (crontab -l)\n3. 检查是否有异常进程 (ps aux --sort=-%cpu)\n4. 查看系统负载 (uptime, vmstat)\n5. 检查 CPU 亲和性设置\n6. 考虑扩容或优化代码",
			Metadata: map[string]interface{}{
				"category": "performance",
				"severity": "medium",
			},
		},
		{
			ID:     "runbook-disk-full",
			Source: SourceRunbook,
			Title:  "磁盘空间不足处理指南",
			Content: "1. df -h 查看磁盘使用\n2. du -sh /* 查找大目录\n3. 清理日志 (journalctl --vacuum-size=100M)\n4. 清理包缓存 (yum clean all / apt clean)\n5. 清理 Docker 镜像 (docker system prune)\n6. 扩容磁盘或迁移数据",
			Metadata: map[string]interface{}{
				"category": "storage",
				"severity": "high",
			},
		},
	}
	for i := range docs {
		emb, err := gw.Embed(ctx, "openai", docs[i].Content)
		if err != nil {
			continue
		}
		docs[i].Embedding = emb
		docs[i].CreatedAt = time.Now()
		docs[i].UpdatedAt = time.Now()
	}
	return len(docs), store.Upsert(ctx, docs)
}

func syncGit(ctx context.Context, store KnowledgeStore, gw *gateway.Gateway) (int, error) {
	docs := []KnowledgeDoc{
		{
			ID:     "git-deploy-script",
			Source: SourceGit,
			Title:  "Deploy Script v2.3",
			Content: "部署脚本：1. 拉取最新代码 (git pull) 2. 构建镜像 (docker build) 3. 推送仓库 (docker push) 4. 滚动更新 (kubectl rollout) 5. 健康检查",
			Metadata: map[string]interface{}{
				"repo":    "ops-scripts",
				"branch":  "main",
				"version": "v2.3",
			},
		},
	}
	for i := range docs {
		emb, err := gw.Embed(ctx, "openai", docs[i].Content)
		if err != nil {
			continue
		}
		docs[i].Embedding = emb
		docs[i].CreatedAt = time.Now()
		docs[i].UpdatedAt = time.Now()
	}
	return len(docs), store.Upsert(ctx, docs)
}

func syncConfluence(ctx context.Context, store KnowledgeStore, gw *gateway.Gateway) (int, error) {
	docs := []KnowledgeDoc{
		{
			ID:     "confluence-arch-overview",
			Source: SourceConfluence,
			Title:  "OpsMesh 架构概览",
			Content: "OpsMesh 是多租户运维编排平台，包含设备管理、任务执行、配置下发、告警、K8s 管理、AI 运维等模块。采用 Go 后端 + Vue 前端 + MySQL 存储。",
			Metadata: map[string]interface{}{
				"space": "OPS",
				"page":  "architecture",
			},
		},
	}
	for i := range docs {
		emb, err := gw.Embed(ctx, "openai", docs[i].Content)
		if err != nil {
			continue
		}
		docs[i].Embedding = emb
		docs[i].CreatedAt = time.Now()
		docs[i].UpdatedAt = time.Now()
	}
	return len(docs), store.Upsert(ctx, docs)
}
