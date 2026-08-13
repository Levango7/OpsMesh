// server_factory.go — 构造器与存储选择：store/session 选择、各域 handler 构造、storeDispatcher
package controlplane

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

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// newDeployHandler 构造 M3 部署处理器：按 store 类型选 SQL/Memory 后端，
// 并用 store 适配 deploy.Dispatcher（防腐接口，避免 deploy 反向依赖 controlplane）。
func newDeployHandler(st store.Store) *deploy.Handler {
	var ds deploy.DeployStore
	if ss, ok := st.(*store.SQLStore); ok {
		s, err := deploy.NewSQL(ss.DB())
		if err != nil {
			logx.Error(context.Background(), "MySQL 部署后端初始化失败，回退 memory", err)
			ds = deploy.NewMemory()
		} else {
			logx.Info(context.Background(), "M3 部署后端=mysql", "reason", "U-04 数据本地化")
			ds = s
		}
	} else {
		ds = deploy.NewMemory()
	}
	return deploy.NewHandler(ds, &storeDispatcher{store: st})
}

// newOrchestrationHandler 构造 M5 作业编排处理器：按 store 类型选 SQL/Memory 后端，
// 并以 store（具备 CreateTask + TasksByParent）直接适配 orchestration.TaskEngine（防腐）。
func newOrchestrationHandler(st store.Store) *orchestration.Handler {
	var ws orchestration.WorkflowStore
	if ss, ok := st.(*store.SQLStore); ok {
		s, err := orchestration.NewSQL(ss.DB())
		if err != nil {
			logx.Error(context.Background(), "MySQL 工作流后端初始化失败，回退 memory", err)
			ws = orchestration.NewMemory()
		} else {
			logx.Info(context.Background(), "M5 工作流后端=mysql", "reason", "U-04 数据本地化")
			ws = s
		}
	} else {
		ws = orchestration.NewMemory()
	}
	return orchestration.NewHandler(ws, st)
}

// storeDispatcher 以 store.Store 适配 deploy.Dispatcher（M3 -> M4 任务引擎派发）。
// M2-1B：原 registryDispatcher 持有 *Registry 薄间接层，现直连 store.Store 小接口。
type storeDispatcher struct {
	store store.Store
}

func (d *storeDispatcher) CreateTask(t *proto.Task) *proto.Task {
	return d.store.CreateTask(t)
}

func (d *storeDispatcher) Device(id string) *proto.DeviceInfo {
	return d.store.Device(id)
}

func (d *storeDispatcher) TaskStates(ids []string, tenantID string) map[string]string {
	out := make(map[string]string)
	if len(ids) == 0 {
		return out
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	for _, t := range d.store.AllTasks(tenantID) {
		if set[t.TaskID] {
			out[t.TaskID] = t.Status
		}
	}
	return out
}

// selectStore 按配置选择后端：--store=mysql 且 DSN 非空时启用 SQLStore，否则 MemoryStore。
// 同时注入事件总线（P1-5），使 store 层状态变更可经 Kafka 等真实消费。
// M4-4C：--multi-schema=true 时使用 MultiSchemaStore（每租户独立 schema），而非单个 SQLStore。
//
// P0-G3 安全加固：静默回退改 fail-fast。
//   - 返回 (store.Store, error)：MySQL/MultiSchema 初始化失败时返回 nil, error（不回退 memory）。
//   - 调用方按 cfg.Production 决策：生产模式 log.Fatal（fail-fast），非生产打 Warning 后回退 memory（保持 demo 兼容）。
//   - 无 cfg.Store == "mysql" 时仍用 MemoryStore（这是正常行为，不是回退，返回 nil error）。
func selectStore(cfg *config.Config, bus events.Bus) (store.Store, error) {
	if cfg.Store == "mysql" && cfg.MySQLDSN != "" {
		if cfg.MultiSchema {
			ms, err := store.NewMultiSchemaStore(cfg.MySQLDSN, cfg.RedisAddr, store.DefaultSchemaNamer(cfg.SchemaPrefix))
			if err != nil {
				return nil, fmt.Errorf("multi-schema store 初始化失败: %w", err)
			}
			logx.Info(context.Background(), "持久化后端=mysql(multi-schema)", "reason", "M4-4C 多租户 schema 隔离")
			return ms.WithBus(bus).WithSecret(cfg.ProvisionSecret).WithDemo(cfg.Demo), nil
		}
		ss, err := store.NewSQLStore(cfg.MySQLDSN, cfg.RedisAddr)
		if err != nil {
			return nil, fmt.Errorf("mysql store 初始化失败: %w", err)
		}
		logx.Info(context.Background(), "持久化后端=mysql", "reason", "U-04 数据本地化")
		return ss.WithBus(bus).WithSecret(cfg.ProvisionSecret).WithDemo(cfg.Demo), nil
	}
	logx.Info(context.Background(), "持久化后端=memory", "reason", "默认，无外部依赖")
	return store.NewMemoryStore().WithSecret(cfg.ProvisionSecret).WithBus(bus).WithDemo(cfg.Demo), nil
}

// selectSessionStore 按 cfg.SessionStore 选择会话状态后端（B-6 多副本共享）。
//
//   - cfg.SessionStore 为空（默认）：InProcessSessionStore（进程内 map，单副本/demo 零依赖）；
//   - cfg.SessionStore="redis://host:port"：RedisSessionStore（多副本 HA 共享 JWT 黑名单/限流/改密令牌）。
//
// Redis 初始化失败时返回 error（不回退进程内），由调用方按 cfg.Production 决策：
// 生产模式 fail-fast，非生产回退进程内（保持本地体验兼容）。
func selectSessionStore(cfg *config.Config) (store.SessionStore, error) {
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
	logx.Info(context.Background(), "会话状态后端=redis", "addr", addr, "reason", "B-6 多副本 HA 共享")
	return rs, nil
}

// newCMDBHandler 按 store 类型创建 CMDB 处理器：MySQL 时使用 SQLCiStore，否则 MemoryCiStore。
func newCMDBHandler(st store.Store) *cmdb.Handler {
	if ss, ok := st.(*store.SQLStore); ok {
		return cmdb.NewHandler(cmdb.NewSQLCiStore(ss.DB()))
	}
	return cmdb.NewHandler(cmdb.NewMemoryCiStore())
}

// newLogHandler 按 cfg.LogStore 选择 M6 日志检索后端（B1 修复 8：Loki/ES 接入）：
//   - memory（默认）：环形缓冲，无外部依赖
//   - sql：MySQL 后端（与控制面共享连接池）
//   - loki：Grafana Loki 后端（仅查询，Append 为 noop，日志由 promtail 直接推送）
//   - es：Elasticsearch 后端（仅查询，Append 为 noop，日志由 filebeat 直接推送）
//
// loki/es 初始化失败时回退 memory（不阻断启动）。
func newLogHandler(st store.Store, cfg *config.Config) *logstore.Handler {
	// B1 修复 8：优先按 cfg.LogStore 选择后端（loki/es 分支）。
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
		logx.Info(context.Background(), "M6 日志后端=mysql", "reason", "U-04 数据本地化")
		return logstore.NewHandler(ls)
	}
	logx.Info(context.Background(), "M6 日志后端=memory", "reason", "默认，无外部依赖")
	return logstore.NewHandler(logstore.NewMemory(0))
}

// securityHeadersMiddleware 为 HTTP 响应注入安全头（H5 安全头中间件）。
// 应用于整个主 mux（仪表盘 + /api/v1/* + 静态资源）；/metrics 在独立 server（buildMetrics）不受影响。
//
// B1 修复 5+6：安全头补全 + CSP nonce 收紧
//   - HSTS：仅 HTTPS 部署（s.tlsCert != ""）时注入 Strict-Transport-Security
//   - Permissions-Policy：禁用 camera/microphone/geolocation
//   - CSP nonce：每请求生成随机 nonce 并注入 CSP；
//     script-src 已移除 'unsafe-inline'（个人版引导页 + 企业版 Vue3 产物均无 inline script），
//     仅保留 'self' + 'nonce-{nonce}'。style-src 仍保留 'unsafe-inline'（Vue :style 绑定需要）。
//
// /healthz 也被包裹但 CSP 对其无副作用（返回 text/plain，无脚本/HTML 解析）。
