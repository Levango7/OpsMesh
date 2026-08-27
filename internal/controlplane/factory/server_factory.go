// server_factory.go — 构造器与存储选择：store/session 选择、各域 handler 构造、storeDispatcher
package factory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"opsmesh/internal/cmdb"
	"opsmesh/internal/config"
	"opsmesh/internal/deploy"
	"opsmesh/internal/events"
	"opsmesh/internal/logstore"
	"opsmesh/internal/logx"
	"opsmesh/internal/orchestration"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

func FirstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// NewDeployHandler 构造 M3 部署处理器：按 store 类型选 SQL/Memory 后端，
// 并用 store 适配 deploy.Dispatcher（防腐接口，避免 deploy 反向依赖 controlplane）。
func NewDeployHandler(st store.Store) *deploy.Handler {
	var ds deploy.DeployStore
	if ss, ok := st.(*store.SQLStore); ok {
		s, err := deploy.NewSQL(ss.DB())
		if err != nil {
			logx.Error(context.Background(), "MySQL 部署后端初始化失败，回退 memory", err)
			ds = deploy.NewMemory()
		} else {
			logx.Info(context.Background(), "M3 部署后端=mysql", "reason", "数据本地化")
			ds = s
		}
	} else {
		ds = deploy.NewMemory()
	}
	return deploy.NewHandler(ds, &StoreDispatcher{Store: st})
}

// NewOrchestrationHandler 构造 M5 作业编排处理器：按 store 类型选 SQL/Memory 后端，
// 并以 store（具备 CreateTask + TasksByParent）直接适配 orchestration.TaskEngine（防腐）。
func NewOrchestrationHandler(st store.Store) *orchestration.Handler {
	var ws orchestration.WorkflowStore
	if ss, ok := st.(*store.SQLStore); ok {
		s, err := orchestration.NewSQL(ss.DB())
		if err != nil {
			logx.Error(context.Background(), "MySQL 工作流后端初始化失败，回退 memory", err)
			ws = orchestration.NewMemory()
		} else {
			logx.Info(context.Background(), "M5 工作流后端=mysql", "reason", "数据本地化")
			ws = s
		}
	} else {
		ws = orchestration.NewMemory()
	}
	return orchestration.NewHandler(ws, st)
}

// StoreDispatcher 以 store.Store 适配 deploy.Dispatcher（M3 -> M4 任务引擎派发）。
// 原 registryDispatcher 持有 *Registry 薄间接层，现直连 store.Store 小接口。
type StoreDispatcher struct {
	Store store.Store
}

func (d *StoreDispatcher) CreateTask(t *proto.Task) *proto.Task {
	return d.Store.CreateTask(t)
}

func (d *StoreDispatcher) Device(id string) *proto.DeviceInfo {
	return d.Store.Device(id)
}

func (d *StoreDispatcher) TaskStates(ids []string, tenantID string) map[string]string {
	out := make(map[string]string)
	if len(ids) == 0 {
		return out
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	for _, t := range d.Store.AllTasks(tenantID) {
		if set[t.TaskID] {
			out[t.TaskID] = t.Status
		}
	}
	return out
}

// SelectStore 按配置选择后端：--store=mysql 且 DSN 非空时启用 SQLStore，否则 MemoryStore。
// 同时注入事件总线，使 store 层状态变更可经 Kafka 等真实消费。
// ：--multi-schema=true 时使用 MultiSchemaStore（每租户独立 schema），而非单个 SQLStore。
//
// 安全加固：静默回退改 fail-fast。
//   - 返回 (store.Store, error)：MySQL/MultiSchema 初始化失败时返回 nil, error（不回退 memory）。
//   - 调用方按 cfg.Production 决策：生产模式 log.Fatal（fail-fast），非生产打 Warning 后回退 memory（保持 demo 兼容）。
//   - 无 cfg.Store == "mysql" 时仍用 MemoryStore（这是正常行为，不是回退，返回 nil error）。
func SelectStore(cfg *config.Config, bus events.Bus) (store.Store, error) {
	if cfg.Store == "mysql" && cfg.MySQLDSN != "" {
		if cfg.MultiSchema {
			ms, err := store.NewMultiSchemaStore(cfg.MySQLDSN, cfg.RedisAddr, store.DefaultSchemaNamer(cfg.SchemaPrefix))
			if err != nil {
				return nil, fmt.Errorf("multi-schema store 初始化失败: %w", err)
			}
			logx.Info(context.Background(), "持久化后端=mysql(multi-schema)", "reason", "多租户 schema 隔离")
			return ms.WithBus(bus).WithSecret(cfg.ProvisionSecret).WithDemo(cfg.Demo), nil
		}
		ss, err := store.NewSQLStore(cfg.MySQLDSN, cfg.RedisAddr)
		if err != nil {
			return nil, fmt.Errorf("mysql store 初始化失败: %w", err)
		}
		logx.Info(context.Background(), "持久化后端=mysql", "reason", "数据本地化")
		return ss.WithBus(bus).WithSecret(cfg.ProvisionSecret).WithDemo(cfg.Demo), nil
	}
	logx.Info(context.Background(), "持久化后端=memory", "reason", "默认，无外部依赖")
	return store.NewMemoryStore().WithSecret(cfg.ProvisionSecret).WithBus(bus).WithDemo(cfg.Demo), nil
}

// SelectSessionStore 按 cfg.SessionStore 选择会话状态后端（多副本共享）。
//
//   - cfg.SessionStore 为空（默认）：InProcessSessionStore（进程内 map，单副本/demo 零依赖）；
//   - cfg.SessionStore="redis://host:port"：RedisSessionStore（多副本 HA 共享 JWT 黑名单/限流/改密令牌）。
//
// Redis 初始化失败时返回 error（不回退进程内），由调用方按 cfg.Production 决策：
// 生产模式 fail-fast，非生产回退进程内（保持本地体验兼容）。
func SelectSessionStore(cfg *config.Config) (store.SessionStore, error) {
	if cfg.SessionStore == "" {
		logx.Info(context.Background(), "会话状态后端=进程内", "reason", "默认，单副本/demo 零依赖")
		return store.NewInProcessSessionStore(), nil
	}
	// 解析 "redis://host:port" 格式。
	if !strings.HasPrefix(cfg.SessionStore, "redis://") {
		return nil, fmt.Errorf("非法 --session-store=%q（须为 redis://host:port 格式）", cfg.SessionStore)
	}
	addr := strings.TrimPrefix(cfg.SessionStore, "redis://")
	if addr == "" {
		return nil, fmt.Errorf("非法 --session-store=%q（host:port 不可为空）", cfg.SessionStore)
	}
	rs, err := store.NewRedisSessionStore(addr, "opsmesh:", 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("redis session store 初始化失败: %w", err)
	}
	logx.Info(context.Background(), "会话状态后端=redis", "addr", addr, "reason", "多副本 HA 共享")
	return rs, nil
}

// NewCMDBHandler 按 store 类型创建 CMDB 处理器：MySQL 时使用 SQLCiStore，否则 MemoryCiStore。
func NewCMDBHandler(st store.Store) *cmdb.Handler {
	if ss, ok := st.(*store.SQLStore); ok {
		return cmdb.NewHandler(cmdb.NewSQLCiStore(ss.DB()))
	}
	return cmdb.NewHandler(cmdb.NewMemoryCiStore())
}

// NewLogHandler 按 cfg.LogStore 选择 M6 日志检索后端（修复 8：Loki/ES 接入）：
//   - memory（默认）：环形缓冲，无外部依赖
//   - sql：MySQL 后端（与控制面共享连接池）
//   - loki：Grafana Loki 后端（仅查询，Append 为 noop，日志由 promtail 直接推送）
//   - es：Elasticsearch 后端（仅查询，Append 为 noop，日志由 filebeat 直接推送）
//
// loki/es 初始化失败时回退 memory（不阻断启动）。
func NewLogHandler(st store.Store, cfg *config.Config) *logstore.Handler {
	// 修复 8：优先按 cfg.LogStore 选择后端（loki/es 分支）。
	switch cfg.LogStore {
	case "loki":
		if cfg.LokiEndpoint == "" {
			logx.Error(context.Background(), "LogStore=loki 但 LokiEndpoint 为空，回退 memory", nil)
			return logstore.NewHandler(logstore.NewMemory(0))
		}
		logx.Info(context.Background(), "M6 日志后端=loki", "endpoint", cfg.LokiEndpoint)
		return logstore.NewHandler(logstore.NewLokiStore(cfg.LokiEndpoint))
	case "es":
		if cfg.ESEndpoint == "" {
			logx.Error(context.Background(), "LogStore=es 但 ESEndpoint 为空，回退 memory", nil)
			return logstore.NewHandler(logstore.NewMemory(0))
		}
		idx := cfg.ESIndex
		if idx == "" {
			idx = "opsmesh-logs"
		}
		logx.Info(context.Background(), "M6 日志后端=es", "endpoint", cfg.ESEndpoint, "index", idx)
		return logstore.NewHandler(logstore.NewESStore(cfg.ESEndpoint, idx))
	}
	// 默认：按 store 类型选择 memory/sql。
	if ss, ok := st.(*store.SQLStore); ok {
		ls, err := logstore.NewSQL(ss.DB())
		if err != nil {
			logx.Error(context.Background(), "MySQL 日志后端初始化失败，回退 memory", err)
			return logstore.NewHandler(logstore.NewMemory(0))
		}
		logx.Info(context.Background(), "M6 日志后端=mysql", "reason", "数据本地化")
		return logstore.NewHandler(ls)
	}
	logx.Info(context.Background(), "M6 日志后端=memory", "reason", "默认，无外部依赖")
	return logstore.NewHandler(logstore.NewMemory(0))
}
