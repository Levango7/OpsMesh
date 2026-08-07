package store

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"opsmesh/internal/cron"
	"opsmesh/internal/dag"
	"opsmesh/internal/events"
	"opsmesh/internal/proto"

	"golang.org/x/crypto/bcrypt"
)

// MemoryStore 内存实现：逻辑与原 controlplane.Registry 一致（单机、并发安全）。
// 作为默认后端，不依赖任何外部存储，保证无 MySQL/Redis 也能 go build 并直接起。
type MemoryStore struct {
	mu       sync.RWMutex
	agents   map[string]*proto.AgentInfo
	segments map[string][]*proto.DeviceInfo // segment -> 设备列表
	tasks    map[string][]*proto.Task       // agentID -> 任务列表
	results  map[string][]*proto.TaskResult // agentID -> 上报结果
	audits   []*proto.AuditEvent            // 审计事件（U-04 留痕）
	alerts   []*proto.Alert                 // 告警事件（M7）
	seq      int                            // 自增序号，用于生成 agentID
	bus      events.Bus                     // 事件总线（P1-5）；可 nil（测试/默认 noop）
	demo     bool                           // 演示模式（P0-5）：开启时注册预置 uname -a
	// B1 自动纳管闭环：install token 的 HMAC 签名密钥与已签发 token 登记表。
	// secret 为空时由 NewMemoryStore 随机生成（单实例 MVP）；多副本须一致（经 WithSecret 注入）。
	secret string
	tokens map[string]*tokenMeta // token -> 元数据（一次性、限时）
	// 用户中心（RBAC）：users/roles/permissions 内存表。
	// permissions 为预定义权限（按组分类），初始化时填充，运行期只读。
	// users/roles 支持 CRUD；密码以 bcrypt 哈希存于 User.PasswordHash。
	users       map[string]*User // id -> user
	usersByName map[string]*User // username -> user（登录 O(1) 直查）
	roles       map[string]*Role // id -> role
	permissions []*Permission    // 预定义权限列表（只读）
	// 设备实时监控指标：deviceID -> 最新指标（agent 心跳上报，仅保留最新值）。
	// 历史时序由 Prometheus 负责，这里只缓存最近一次采集结果供 API 查询。
	deviceMetrics map[string]*proto.DeviceMetrics
	// K8s 集群配置（Phase 3）：clusterID -> 集群配置。
	// 与 deviceMetrics 同样由 m.mu 保护并发安全；Kubeconfig 为敏感内容，API 层负责脱敏。
	k8sClusters map[string]*K8sCluster
	// task 81 gRPC agent 身份绑定：agentID -> HMAC 签名密钥（32 字节 hex）。
	// 由 Register 时为每个 agent 随机生成；agent 拉任务/上报/轮询取消时用此密钥签名，
	// 控制面据此验证 agent 身份，不再纯信任 agent 自报的 AgentID。
	agentSecrets map[string]string
	// task 100 告警规则：ruleID -> 规则。由 m.mu 保护并发安全。
	// 控制面告警引擎周期评估 Enabled 规则，按 Metric/Op/Threshold 比对设备最新指标，
	// 持续 ForDuration 满足则产出 Alert（M7）。
	alertRules map[string]*AlertRule
	// task 100 OS 安装模板：templateID -> 模板。由 m.mu 保护并发安全。
	// 用于 B1 自动纳管闭环的「裸机→OS→agent」全自动安装链路；Config 为敏感内容，API 层负责脱敏。
	osTemplates map[string]*OSTemplate
	// task 100 中间件部署模板：templateID -> 模板。由 m.mu 保护并发安全。
	// 供「应用编排→中间件实例化」复用；Config 为敏感内容，API 层负责脱敏。
	middlewareTemplates map[string]*MiddlewareTemplate
	// task 111 刷新令牌：tokenHash -> refresh token。由 m.mu 保护并发安全。
	// key 为明文 token 的 SHA-256 摘要（P1-F7 明文不落库）；value 为元信息。
	// 用于 access token 过期后的无感续期；登出/吊销时按摘要删除。
	refreshTokens map[string]*RefreshToken
}

// tokenMeta B1 install token 元数据：一次性、限时，消费后标记 consumed。
type tokenMeta struct {
	deviceID  string
	tenantID  string
	expiresAt time.Time
	consumed  bool
}

// WithBus 注入事件总线（store 构造后由控制面注入，避免改动所有构造调用点）。
// 线程安全：必须在 Start/首次并发访问前调用，非并发安全。
func (m *MemoryStore) WithBus(b events.Bus) *MemoryStore {
	m.bus = b
	return m
}

// WithDemo 设置演示模式（P0-5）：开启时 Register 预置 uname -a 示例任务。
// 线程安全：必须在 Start/首次并发访问前调用，非并发安全。
func (m *MemoryStore) WithDemo(b bool) Store {
	m.demo = b
	// 演示模式（P0-5）：仅 --demo 开启时预置示例告警，避免污染生产。
	// 让 M7 监控告警 tab 在本地预览中即可见、可操作（确认/静默）。
	if b {
		now := time.Now()
		m.addAlertLocked(&proto.Alert{
			AlertID:   "alert-demo-1",
			TenantID:  "default",
			DeviceID:  "dev-demo-01",
			AgentID:   "agent-demo",
			Severity:  "critical",
			Message:   "示例告警：核心服务进程异常退出（demo）",
			CreatedAt: now.Add(-35 * time.Minute),
		})
		m.addAlertLocked(&proto.Alert{
			AlertID:   "alert-demo-2",
			TenantID:  "default",
			DeviceID:  "dev-demo-02",
			AgentID:   "agent-demo",
			Severity:  "warning",
			Message:   "示例告警：磁盘使用率超过 85%（demo）",
			CreatedAt: now.Add(-12 * time.Minute),
		})
	}
	return m
}

// SeedDemoTopology 在 --demo 时主动播种若干 agent / 设备 / 任务，
// 让控制台（运维中枢 / 部署中心 / 监控告警 / 配置库上下文）在无真实 agent 进程时也能完整演示。
// 仅由 controlplane 在 demo 模式调用（selectStore 之后）；单测直接构造 NewMemoryStore 不受影响。
// 幂等：若已存在 agent 则跳过，避免重复播种。
func (m *MemoryStore) SeedDemoTopology() {
	if !m.demo {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.agents) > 0 {
		return // 已播种，跳过
	}
	type spec struct {
		seg, host, ip string
	}
	specs := []spec{
		{"seg-a", "web-01", "10.30.1.11"},
		{"seg-a", "web-02", "10.30.1.12"},
		{"seg-b", "db-01", "10.30.2.21"},
	}
	now := time.Now()
	for i, s := range specs {
		aid := fmt.Sprintf("agent-demo-%d", i+1)
		devID := fmt.Sprintf("dev-demo-0%d", i+1)
		a := &proto.AgentInfo{
			AgentID:     aid,
			Hostname:    s.host,
			Segment:     s.seg,
			TenantID:    "default",
			Addr:        s.ip + ":9090",
			GRPCPort:    9090,
			MetricsPort: 9091,
			Status:      "online",
			Load:        (i + 1) * 11,
			LastSeen:    now,
		}
		m.agents[aid] = a
		m.segments[s.seg] = append(m.segments[s.seg], &proto.DeviceInfo{
			DeviceID:  devID,
			Segment:   s.seg,
			TenantID:  "default",
			IP:        s.ip,
			AgentID:   aid,
			State:     "online",
			TaskState: "idle",
			Managed:   true,
		})
		// 每台预置几条不同状态的任务，演示运维中枢 / 部署 fan-out / 告警联动。
		ts := []*proto.Task{
			{TaskID: fmt.Sprintf("task-%s-1", aid), AgentID: aid, TenantID: "default", Type: "shell", Command: "systemctl status nginx", Status: "done", CreatedAt: now.Add(-2 * time.Hour)},
			{TaskID: fmt.Sprintf("task-%s-2", aid), AgentID: aid, TenantID: "default", Type: "shell", Command: "uname -a", Status: "running", CreatedAt: now.Add(-3 * time.Minute)},
		}
		if i == 0 {
			// 第一台造一个失败+死信任务，演示死信 / 告警联动。
			ts = append(ts, &proto.Task{
				TaskID:     fmt.Sprintf("task-%s-3", aid),
				AgentID:    aid,
				TenantID:   "default",
				Type:       "shell",
				Command:    "deploy.sh --rollback",
				Status:     "failed",
				RetryCount: 3,
				MaxRetries: 3,
				DeadLetter: true,
				CreatedAt:  now.Add(-10 * time.Minute),
			})
		}
		m.tasks[aid] = ts
	}
}

// publish 在总线非空时发布领域事件（审计/告警可接 Kafka）。
func (m *MemoryStore) publish(e events.Event) {
	if m.bus != nil {
		if err := m.bus.Publish(context.Background(), e); err != nil {
			log.Printf("store: memory 发布事件 %s 失败: %v", e.Action, err)
		}
	}
}

// NewMemoryStore 构造空内存存储。secret 随机生成（单实例 MVP；多副本经 WithSecret 注入一致密钥）。
// 同时预填充用户中心数据：预定义权限（按组分类）、预定义角色（admin/operator/viewer）、
// 预定义用户（admin/admin123、operator/operator123、viewer/viewer123）。
func NewMemoryStore() *MemoryStore {
	m := &MemoryStore{
		agents:              make(map[string]*proto.AgentInfo),
		segments:            make(map[string][]*proto.DeviceInfo),
		tasks:               make(map[string][]*proto.Task),
		results:             make(map[string][]*proto.TaskResult),
		tokens:              make(map[string]*tokenMeta),
		users:               make(map[string]*User),
		usersByName:         make(map[string]*User),
		roles:               make(map[string]*Role),
		secret:              mustRandHex(32),
		deviceMetrics:       make(map[string]*proto.DeviceMetrics),
		k8sClusters:         make(map[string]*K8sCluster),
		agentSecrets:        make(map[string]string),
		alertRules:          make(map[string]*AlertRule),
		osTemplates:         make(map[string]*OSTemplate),
		middlewareTemplates: make(map[string]*MiddlewareTemplate),
		refreshTokens:       make(map[string]*RefreshToken),
	}
	m.seedRBAC()
	return m
}

// seedRBAC 预填充用户中心数据：权限/角色/用户。
// 在 NewMemoryStore 中调用一次，幂等（重复调用会重置 map，但构造期单线程无并发风险）。
//
// 预定义权限按组分类：device/task/alert/cmdb/deploy/workflow/log/audit/user/role/federation。
// 预定义角色：
//   - admin：所有权限
//   - operator：device/task/alert/cmdb/deploy/workflow/log/audit 的 read+write（不含 delete）
//   - viewer：所有 *:read 权限
//
// 预定义用户（密码 bcrypt 哈希）：
//   - admin/admin123（admin 角色）
//   - operator/operator123（operator 角色）
//   - viewer/viewer123（viewer 角色）
func (m *MemoryStore) seedRBAC() {
	// 1. 预定义权限（按组分类）。
	// 复用 rbacPermSpecs 作为权限单一来源（与 SQLStore 保持一致，杜绝两份定义漂移）。
	m.permissions = make([]*Permission, 0, len(rbacPermSpecs))
	for i, ps := range rbacPermSpecs {
		pid := fmt.Sprintf("perm-%s-%02d", ps.group, i+1)
		m.permissions = append(m.permissions, &Permission{
			ID:          pid,
			Name:        ps.name,
			Description: ps.desc,
			Group:       ps.group,
		})
	}

	// 2. 预定义角色（复用 RolePermissions() 统一角色→权限映射，与 RBAC 闸保持一致）。
	rp := RolePermissions()
	viewerPerms := rp["viewer"]
	operatorPerms := rp["operator"]
	adminPerms := rp["admin"]

	now := time.Now()
	m.roles["role-admin"] = &Role{
		ID:          "role-admin",
		Name:        "admin",
		Description: "超级管理员，拥有所有权限",
		Permissions: adminPerms,
		CreatedAt:   now,
	}
	m.roles["role-operator"] = &Role{
		ID:          "role-operator",
		Name:        "operator",
		Description: "运维人员，可操作设备/任务/告警/部署等，不含删除权限",
		Permissions: operatorPerms,
		CreatedAt:   now,
	}
	m.roles["role-viewer"] = &Role{
		ID:          "role-viewer",
		Name:        "viewer",
		Description: "只读用户，仅可查看各类资源",
		Permissions: viewerPerms,
		CreatedAt:   now,
	}

	// 3. 预定义用户（密码 bcrypt 哈希）。
	// bcrypt 哈希失败时 panic（构造期失败优于运行期诡异出错）。
	// 安全债 85：预置弱口令（admin123/operator123/viewer123）首登强制改密，
	// MustChangePassword=true 标记由 auth.go 登录响应返回前端，弹出改密对话框；
	// 改密成功后由 change-password API 清除标记。避免弱口令被遗忘长期可登。
	type userSpec struct {
		id, name, password, email string
		roleIDs                   []string
	}
	specs := []userSpec{
		{"user-admin", "admin", "admin123", "admin@opsmesh.local", []string{"role-admin"}},
		{"user-operator", "operator", "operator123", "operator@opsmesh.local", []string{"role-operator"}},
		{"user-viewer", "viewer", "viewer123", "viewer@opsmesh.local", []string{"role-viewer"}},
	}
	for _, us := range specs {
		hash, err := bcryptHash(us.password)
		if err != nil {
			// bcrypt 失败属于环境异常（成本因子超限等），构造期 panic 暴露问题。
			log.Panicf("[store] 预填充用户 %q 的 bcrypt 哈希失败: %v", us.name, err)
		}
		u := &User{
			ID:                 us.id,
			Username:           us.name,
			Email:              us.email,
			PasswordHash:       hash,
			Status:             "active",
			RoleIDs:            us.roleIDs,
			CreatedAt:          now,
			MustChangePassword: true, // 预置弱口令强制改密（安全债 85）
		}
		m.users[u.ID] = u
		m.usersByName[u.Username] = u
	}
}

// bcryptHash 包装 bcrypt.GenerateFromPassword，避免 memory.go 直接依赖 bcrypt 包
// （集中在此函数便于后续替换哈希算法或调整成本因子）。
// 成本因子使用 bcrypt.DefaultCost（10），兼顾安全与性能。
func bcryptHash(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// bcryptCompare 包装 bcrypt.CompareHashAndPassword，返回是否匹配。
// 用于登录校验：哈希与明文密码比对。
func bcryptCompare(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// WithSecret 注入 B1 install token 的 HMAC 签名密钥（空则保留构造时随机密钥）。
// 多副本控制面共享同一 MySQL 时须注入一致密钥，否则互不相认。
// 线程安全：必须在 Start/首次并发访问前调用，非并发安全。
func (m *MemoryStore) WithSecret(s string) *MemoryStore {
	if s != "" {
		m.secret = s
	}
	return m
}

// randHex 返回 n 字节的十六进制随机串（crypto/rand，密码学安全）。
// 用于 nonce：熵失败时回退时间戳（降熵但可容忍）并打印告警。
func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		log.Printf("[store] 警告：crypto/rand 熵源不可用（%v），回退时间戳派生 nonce——熵降级", err)
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// mustRandHex 返回 n 字节的十六进制随机串，熵失败时 panic（用于 HMAC secret）。
// 安全（F11）：secret 必须密码学安全，不可静默降级为时间戳。
func mustRandHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		log.Panicf("[store] crypto/rand 不可用，无法生成安全密钥: %v", err)
	}
	return hex.EncodeToString(b)
}

// hashToken 对完整 token 取 SHA-256 摘要（hex）。
// 安全（P1-F7）：库存/内存只存摘要，不存明文 token——DB 只读账号/备份泄露不等于活体 token 泄露。
func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// verifyTokenMAC 校验 token 的 HMAC 签名（F8 真正落地）：从 token 中提取 payload + 签名，
// 用 secret 重算 HMAC-SHA256 并与签名部分比较。防 DB 写权限伪造 token。
func verifyTokenMAC(secret, token string) bool {
	if secret == "" || token == "" {
		return false
	}
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	sigHex, payload := parts[0], parts[1]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sigHex))
}

// issueTokenLocked B1 签发一个一次性 install token（HMAC(deviceID|tenantID|expiry|nonce)），
// 调用方须持 m.mu（写锁）。token 格式：hex(hmac) + "." + payload，payload 明文含设备/租户/过期/随机串。
// 库存键为 token 的 SHA-256 摘要（P1-F7 明文不落库）。
func (m *MemoryStore) issueTokenLocked(deviceID, tenantID string, ttl time.Duration) (string, error) {
	if m.secret == "" {
		m.secret = mustRandHex(32) // 兜底，正常构造时已置随机密钥
	}
	nonce := randHex(16)
	expiresAt := time.Now().Add(ttl)
	payload := strings.Join([]string{tenantID, deviceID, strconv.FormatInt(expiresAt.Unix(), 10), nonce}, "|")
	mac := hmac.New(sha256.New, []byte(m.secret))
	mac.Write([]byte(payload))
	tok := hex.EncodeToString(mac.Sum(nil)) + "." + payload
	m.tokens[hashToken(tok)] = &tokenMeta{deviceID: deviceID, tenantID: tenantID, expiresAt: expiresAt}
	return tok, nil
}

// Register 注册一个 agent：分配 agentID，按 segment 落桶，并预置示例任务。
func (m *MemoryStore) Register(a *proto.AgentInfo) *proto.AgentInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	if a.AgentID == "" {
		m.seq++
		a.AgentID = "agent-" + strconv.Itoa(m.seq)
	}
	if a.Status == "" {
		a.Status = "online"
	}
	a.LastSeen = time.Now()
	m.agents[a.AgentID] = a
	// task 81 gRPC agent 身份绑定：为每个 agent 生成 HMAC 签名密钥（仅首次注册时生成，复用已有 agent 不重置）。
	// agent 拉任务/上报/轮询取消时用此密钥签名，控制面据此验证 agent 身份，不再纯信任 agent 自报的 AgentID。
	if _, ok := m.agentSecrets[a.AgentID]; !ok {
		m.agentSecrets[a.AgentID] = mustRandHex(32)
	}

	// B1 自动纳管闭环：若携带 OnboardDeviceID（由 gRPC Register 校验 install token 后回填），
	// 说明这是「已发现候选设备」经 provision 推送 agent 后回注册——翻转该候选设备为已纳管，
	// 不再新建占位设备。token 校验已在服务侧 ConsumeToken 完成，此处相信已盖章的 OnboardDeviceID。
	// 安全（P0-F1 纵深防御）：翻转前校验候选设备租户与 agent 租户一致（防越权翻转）。
	if a.OnboardDeviceID != "" {
	outer:
		for _, devs := range m.segments {
			for _, d := range devs {
				if d.DeviceID == a.OnboardDeviceID {
					if a.TenantID != "" && d.TenantID != "" && d.TenantID != a.TenantID {
						continue // 租户不一致：拒绝翻转（跨租户设备劫持防护）
					}
					d.Managed = true
					d.State = "online"
					d.AgentID = a.AgentID
					d.TenantID = a.TenantID
					break outer
				}
			}
		}
	} else {
		// U-02：把 agent 落到对应 segment 桶（整段网络纳管，自动生成占位设备）。
		exists := false
		for _, d := range m.segments[a.Segment] {
			if d.AgentID == a.AgentID {
				exists = true
				// 已存在则补全主机元信息（旧版 agent 可能未上报 OS/Arch，注册升级时回填）。
				d.Hostname = a.Hostname
				if a.OS != "" {
					d.OS = a.OS
				}
				if a.Arch != "" {
					d.Arch = a.Arch
				}
				break
			}
		}
		if !exists {
			m.segments[a.Segment] = append(m.segments[a.Segment], &proto.DeviceInfo{
				DeviceID:  "dev-" + a.AgentID,
				Segment:   a.Segment,
				TenantID:  a.TenantID,
				IP:        a.Addr,
				AgentID:   a.AgentID,
				State:     "online",
				TaskState: "idle",
				Managed:   true, // agent 主动注册 = 真正纳管（B1 闭环：discovered 候选才 false）
				Hostname:  a.Hostname,
				OS:        a.OS,
				Arch:      a.Arch,
			})
		}
	}

	// 演示模式（P0-5）：仅 --demo 开启时预置一条 uname -a 示例任务，避免污染生产。
	if m.demo && len(m.tasks[a.AgentID]) == 0 {
		m.tasks[a.AgentID] = []*proto.Task{
			{
				TaskID:    "task-" + a.AgentID + "-1",
				AgentID:   a.AgentID,
				TenantID:  a.TenantID,
				Type:      "shell",
				Command:   "uname -a",
				Status:    "pending",
				CreatedAt: time.Now(),
			},
		}
	}
	// 事件驱动：注册事件经总线发布（审计/告警可接 Kafka，P1-5）。
	m.publish(events.Event{Action: "register", Target: a.AgentID, TenantID: a.TenantID, Level: events.LevelInfo})
	// 审计留痕（U-04 等保三级：注册 100% 入审计轨迹；统一在存储层产出，避免与 handler 重复）。
	// 此处已持 m.mu 写锁，调用 appendAudit 而非 Audit，避免重入死锁。
	m.appendAudit(&proto.AuditEvent{
		TenantID: a.TenantID,
		Action:   "register",
		Target:   a.AgentID,
	})
	return a
}

// Heartbeat 更新 agent 的在线状态/负载/最近心跳时间。返回是否已知该 agent。
func (m *MemoryStore) Heartbeat(agentID, status string, load int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[agentID]
	if !ok {
		return false
	}
	a.LastSeen = time.Now()
	a.Status = status
	a.Load = load
	return true
}

// GetTasks 返回指定 agent 的待执行任务（仅 pending；只读，不改动状态，用于检视/调试）。
func (m *MemoryStore) GetTasks(agentID string) []*proto.Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ts := m.tasks[agentID]
	out := make([]*proto.Task, 0, len(ts))
	for _, t := range ts {
		if t.Status != "" && t.Status != "pending" {
			continue
		}
		c := *t
		out = append(out, &c)
	}
	return out
}

// TasksByParent 返回指定 parent_id 的全部任务（跨状态），用于 M5 工作流运行归组 / F4 模板血缘。
func (m *MemoryStore) TasksByParent(parentID string) []*proto.Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*proto.Task
	for _, ts := range m.tasks {
		for _, t := range ts {
			if t.ParentID == parentID {
				c := *t
				out = append(out, &c)
			}
		}
	}
	return out
}

// ClaimTask 原子领取该 agent 的下一条 pending 任务（pending→running），返回被领取的任务。
// 并发调用时由同一把锁保证同一任务只被领取一次（HA 协调，P1-1）。返回值为锁内拷贝。
func (m *MemoryStore) ClaimTask(agentID string) *proto.Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tasks[agentID] {
		// 仅模板任务（ParentID 空且 Schedule 非空）不可被直接领取；
		// 派生实例（ParentID 指向模板）是正常 pending 任务，可被领取。
		if t.ParentID == "" && t.Schedule != "" {
			continue
		}
		if t.Status == "" || t.Status == "pending" {
			t.Status = "running"
			t.ClaimedAt = time.Now()
			t.ClaimedBy = "controlplane"
			// 返回锁内拷贝：调用方（gRPC 层）在锁外读取/序列化，避免与 SubmitResult 锁内修改构成 data race。
			c := *t
			return &c
		}
	}
	return nil
}

// UpsertDevice 写入/更新一台纳管设备（真实网段发现 P0-2 用；按 deviceID 幂等）。
func (m *MemoryStore) UpsertDevice(d *proto.DeviceInfo) {
	if d == nil || d.DeviceID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.segments[d.Segment]
	for i, existing := range list {
		if existing.DeviceID == d.DeviceID {
			list[i] = d
			m.segments[d.Segment] = list
			return
		}
	}
	m.segments[d.Segment] = append(m.segments[d.Segment], d)
}

// PendingDepth 返回当前 pending 任务总数（观测队列深度 P2-1）。
func (m *MemoryStore) PendingDepth() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, ts := range m.tasks {
		for _, t := range ts {
			if t.Status == "" || t.Status == "pending" {
				n++
			}
		}
	}
	return n
}

// ReclaimStaleTasks 复位超期未完成的 running 任务为 pending（P0-1 任务必达）。
// agent 经 ClaimTask 领取（写 ClaimedAt）后若失联、超过 maxAge 仍未上报结果，
// 该任务将永远卡在 running；此处周期性调用把它复位，重新进入调度队列。
// 在持锁下遍历并就地修改切片元素，未增删 map key，并发安全。
func (m *MemoryStore) ReclaimStaleTasks(maxAge time.Duration) int {
	if maxAge <= 0 {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	cutoff := time.Now().Add(-maxAge)
	for _, ts := range m.tasks {
		for _, t := range ts {
			if t.Status == "running" && !t.ClaimedAt.IsZero() && t.ClaimedAt.Before(cutoff) {
				t.Status = "pending"
				t.ClaimedAt = time.Time{}
				t.ClaimedBy = ""
				n++
			}
		}
	}
	return n
}

// CreateTask 下发一个任务给指定 agent（agentID 必填，TaskID 为空时分配；状态置 pending）。
// FireDueSchedules 评估所有模板任务（ParentID=="" 且 Schedule!=""），
// 对到点（cron 匹配 now 且 LastFiredAt 早于本分钟）的模板派生一个 pending 实例并回写 LastFiredAt。
// 返回本批次派生的实例数（F4 定时/周期调度；控制面 scheduleLoop 周期调用）。
func (m *MemoryStore) FireDueSchedules(now time.Time) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	fired := 0
	minuteStart := now.Truncate(time.Minute)
	for _, tks := range m.tasks {
		for _, t := range tks {
			if t.ParentID != "" || t.Schedule == "" {
				continue // 仅处理模板任务
			}
			ok, err := cron.Match(t.Schedule, now)
			if err != nil || !ok {
				continue
			}
			// 本分钟已触发过则跳过，避免重复派生（调度器周期 < 1 分钟时尤其重要）。
			if !t.LastFiredAt.IsZero() && !t.LastFiredAt.Before(minuteStart) {
				continue
			}
			inst := &proto.Task{
				AgentID:    t.AgentID,
				TenantID:   t.TenantID,
				Type:       t.Type,
				Command:    t.Command,
				Content:    t.Content,
				Path:       t.Path,
				Status:     "pending",
				ParentID:   t.TaskID,
				MaxRetries: t.MaxRetries,
				CreatedAt:  now,
			}
			m.seq++
			inst.TaskID = fmt.Sprintf("task-%d-%d", now.UnixNano(), m.seq)
			m.tasks[t.AgentID] = append(m.tasks[t.AgentID], inst)
			t.LastFiredAt = now
			fired++
			m.publish(events.Event{Action: "schedule_fire", Target: inst.TaskID, TenantID: t.TenantID,
				Detail: "parent=" + t.TaskID + " cron=" + t.Schedule, Level: events.LevelInfo})
		}
	}
	return fired
}

func (m *MemoryStore) CreateTask(t *proto.Task) *proto.Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t.AgentID == "" {
		return t // 调用方需保证 agentID 非空
	}
	if t.TaskID == "" {
		m.seq++
		t.TaskID = fmt.Sprintf("task-%d-%d", time.Now().UnixNano(), m.seq)
	}
	if t.Status == "" {
		t.Status = "pending"
	}
	// M5 作业编排：含前置依赖的任务初始为 blocked，待依赖 done 后由 SubmitResult 释放为 pending。
	if len(t.DependsOn) > 0 {
		t.Status = "blocked"
	}
	// task 100 任务审批：高风险任务（ApprovalRequired=true）初始为 pending_approval，
	// 不进入 ClaimTask 队列（ClaimTask 仅领取 pending）；待 ApproveTask 翻转为 pending 后才可下发。
	if t.ApprovalRequired && t.Status == "pending" {
		t.Status = "pending_approval"
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	m.tasks[t.AgentID] = append(m.tasks[t.AgentID], t)
	// 事件驱动：下发任务事件经总线发布。
	m.publish(events.Event{Action: "create_task", Target: t.TaskID, TenantID: t.TenantID, Detail: t.Command, Level: events.LevelInfo})
	return t
}

// SubmitResult 接收 agent 上报的执行结果，按成功/失败处理任务终态，并同步设备看板（B2）。
// F2 失败重试 / 死信：失败且未达上限 → 复位 pending（RetryCount++）重新入队；
// 已达上限 → 置 failed 且标记 DeadLetter（死信，需人工处置）并产出 critical 告警（M7）。
// 状态守卫（幂等）：仅接受任务处于 running 时的上报；其他状态的迟到上报被忽略（结果记录不受影响）。
func (m *MemoryStore) SubmitResult(res *proto.TaskResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[res.AgentID] = append(m.results[res.AgentID], res)
	success := res.ExitCode == 0

	// 更新任务终态（成功=done；失败=重试或死信）。
	for _, t := range m.tasks[res.AgentID] {
		if t.TaskID != res.TaskID {
			continue
		}
		// 状态守卫（幂等，task 82）：仅接受任务处于 running 时的上报；
		// 其他状态的迟到/重复上报一律忽略，防止已取消任务被翻回 done、
		// 防止重复失败上报反复累计 retry_count 造成假死信。
		if t.Status != "running" {
			log.Printf("store: memory SubmitResult 忽略非 running 任务 %s (status=%s exitCode=%d)", t.TaskID, t.Status, res.ExitCode)
			return
		}
		if success {
			t.Status = "done"
		} else if t.RetryCount < t.MaxRetries {
			t.Status = "pending"
			t.ClaimedAt = time.Time{}
			t.ClaimedBy = ""
			t.RetryCount++
			m.publish(events.Event{Action: "task_retry", Target: t.TaskID, TenantID: t.TenantID,
				Detail: fmt.Sprintf("retry %d/%d", t.RetryCount, t.MaxRetries), Level: events.LevelWarn})
		} else {
			t.Status = "failed"
			t.DeadLetter = true
			m.addAlertLocked(&proto.Alert{
				AlertID:   "alert-" + t.TaskID,
				TenantID:  t.TenantID,
				DeviceID:  "dev-" + res.AgentID,
				AgentID:   res.AgentID,
				Severity:  "critical",
				Message:   fmt.Sprintf("task %s entered dead-letter after %d retries (exitCode=%d)", t.TaskID, t.RetryCount, res.ExitCode),
				CreatedAt: time.Now(),
			})
		}
	}

	// M5 作业编排：本任务 done 后释放依赖它的 blocked 任务（依赖全部 done → pending）。
	m.releaseDeps(res.AgentID, res.TaskID)

	// 找到该 agent 所属网段，回写设备 TaskState + LastResult（B2 失败回写看板）。
	seg := ""
	if a, ok := m.agents[res.AgentID]; ok {
		seg = a.Segment
	}
	if devs, ok := m.segments[seg]; ok {
		for _, d := range devs {
			if d.AgentID == res.AgentID {
				if success {
					d.TaskState = "done"
					d.LastResult = "success"
				} else {
					d.TaskState = "idle"
					d.LastResult = "failed"
				}
				d.LastResultAt = time.Now()
			}
		}
	}
	// 事件驱动：结果上报事件经总线发布（按退出码定级别）。
	lvl := events.LevelInfo
	if !success {
		lvl = events.LevelWarn
	}
	m.publish(events.Event{Action: "report_result", Target: res.TaskID, TenantID: "", Detail: fmt.Sprintf("exitCode=%d", res.ExitCode), Level: lvl})
}

// releaseDeps M5 作业编排：任务 done 后，将其 blocked 的依赖任务中“全部前置依赖现已 done”的
// 释放为 pending，使其进入可下发队列。仅在同 agent 的任务图内生效（任务按 agentID 索引）。
// 调用方需持锁（SubmitResult 已加锁）。
func (m *MemoryStore) releaseDeps(agentID, doneTaskID string) {
	byID := make(map[string]*proto.Task, len(m.tasks[agentID]))
	for _, t := range m.tasks[agentID] {
		byID[t.TaskID] = t
	}
	for _, t := range m.tasks[agentID] {
		if t.Status != "blocked" {
			continue
		}
		if dag.AllDepsDone(t, byID) {
			t.Status = "pending"
			t.ClaimedAt = time.Time{}
			t.ClaimedBy = ""
			m.publish(events.Event{Action: "task_released", Target: t.TaskID, TenantID: t.TenantID,
				Detail: "deps done (trigger=" + doneTaskID + ")", Level: events.LevelInfo})
		}
	}
}

// addAlertLocked 在持锁下追加告警，超 ring 上限（复用 auditCap）截断最旧。
func (m *MemoryStore) addAlertLocked(a *proto.Alert) {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	if a.Status == "" {
		a.Status = proto.AlertStatusFiring
	}
	m.alerts = append(m.alerts, a)
	if len(m.alerts) > auditCap {
		m.alerts = m.alerts[len(m.alerts)-auditCap:]
	}
}

// AddAlert 记录一条告警（M7）。
func (m *MemoryStore) AddAlert(a *proto.Alert) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addAlertLocked(a)
}

// Alerts 返回活跃告警（M7）；tenantID 非空时按租户过滤。
func (m *MemoryStore) Alerts(tenantID string) []*proto.Alert {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*proto.Alert, 0, len(m.alerts))
	for _, a := range m.alerts {
		if tenantID != "" && a.TenantID != tenantID {
			continue
		}
		c := *a
		out = append(out, &c)
	}
	return out
}

// Alert 按 alertID 返回单条告警（M7；供 ack/silence 定位）。
func (m *MemoryStore) Alert(id string) *proto.Alert {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, a := range m.alerts {
		if a.AlertID == id {
			c := *a
			return &c
		}
	}
	return nil
}

// AckAlert 确认告警（M7）：置 acknowledged 并记录确认人；tenantID 非空时校验归属，越权返回 false。
func (m *MemoryStore) AckAlert(id, tenantID, by string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.alerts {
		if a.AlertID == id && (tenantID == "" || a.TenantID == tenantID) {
			a.Status = proto.AlertStatusAcknowledged
			a.AcknowledgedBy = by
			a.UpdatedAt = time.Now()
			return true
		}
	}
	return false
}

// SilenceAlert 静默告警（M7）：置 silenced 并记录静默截止与备注；tenantID 非空时校验归属，越权返回 false。
// until 为零值时默认静默 24h。
func (m *MemoryStore) SilenceAlert(id, tenantID, by string, until time.Time, comment string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if until.IsZero() {
		until = time.Now().Add(24 * time.Hour)
	}
	for _, a := range m.alerts {
		if a.AlertID == id && (tenantID == "" || a.TenantID == tenantID) {
			a.Status = proto.AlertStatusSilenced
			a.AcknowledgedBy = by
			a.SilencedUntil = until
			a.Comment = comment
			a.UpdatedAt = time.Now()
			return true
		}
	}
	return false
}

// TaskResult 按 taskID 返回单条执行结果（A5/F7 结果查询 API）。
func (m *MemoryStore) TaskResult(taskID string) *proto.TaskResult {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, rs := range m.results {
		for _, r := range rs {
			if r.TaskID == taskID {
				c := *r
				return &c
			}
		}
	}
	return nil
}

// CancelTask 取消任务（F3）：pending/running -> cancelled；已 done/failed/cancelled 不可取消。
// 取消 pending 后其不会进入 ClaimTask 领取（原子翻转只看 pending），实现运行前拦截。
func (m *MemoryStore) CancelTask(id, tenantID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ts := range m.tasks {
		for _, t := range ts {
			if t.TaskID != id {
				continue
			}
			if tenantID != "" && t.TenantID != tenantID {
				return false
			}
			if t.Status == "pending" || t.Status == "running" {
				t.Status = "cancelled"
				return true
			}
			return false
		}
	}
	return false
}

// RetireDevice 退役/下线设备（F5）：标记 retired，退出活跃清单（Snapshot 过滤）但仍可查归档。
func (m *MemoryStore) RetireDevice(id, tenantID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, devs := range m.segments {
		for _, d := range devs {
			if d.DeviceID != id {
				continue
			}
			if tenantID != "" && d.TenantID != tenantID {
				return false
			}
			d.Retired = true
			d.State = "offline"
			return true
		}
	}
	return false
}

// Provision B1 自动纳管闭环：为「已发现候选设备」签发一次性、限时的 install token
// （HMAC 签名，密钥来自 store 构造时注入的 ProvisionSecret），标记设备 provisioning，
// 并返回 token 与 bootstrap 提示（curl|sh 经 token 安装 agent）。
// deviceID 不存在时返回错误；device 已 managed 也允许重新签发（幂等重推）。
func (m *MemoryStore) Provision(deviceID, host, tenantID string) (token, bootstrap string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// 安全（F15）：payload 用 | 分隔，deviceID/tenantID 含 | 导致解析歧义，直接拒绝。
	if strings.Contains(deviceID, "|") || strings.Contains(tenantID, "|") {
		return "", "", fmt.Errorf("deviceID 或 tenantID 含非法字符 |")
	}
	found := false
	for _, devs := range m.segments {
		for _, d := range devs {
			if d.DeviceID == deviceID {
				found = true
				d.State = "provisioning"
				d.IP = host // 记录目标主机（可能与发现 IP 一致）
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return "", "", fmt.Errorf("device %s not found", deviceID)
	}
	tok, e := m.issueTokenLocked(deviceID, tenantID, 15*time.Minute)
	if e != nil {
		return "", "", e
	}
	// bootstrap 为占位模板，真实控制面地址由 HTTP handler 按请求 host 重写。
	boot := fmt.Sprintf("curl -sSL http://<control-plane>:8080/install.sh | sh -s -- --token=%s", tok)
	return tok, boot, nil
}

// IssueToken 生成并登记一个一次性 install token（HMAC(deviceID|tenantID|expiry|nonce)，ttl 为有效期）。
func (m *MemoryStore) IssueToken(deviceID, tenantID string, ttl time.Duration) (token string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if deviceID == "" {
		return "", fmt.Errorf("deviceID required")
	}
	return m.issueTokenLocked(deviceID, tenantID, ttl)
}

// ConsumeToken 校验并消费 token：限时、未用过才返回设备与租户并置 consumed；否则返回 ok=false。
// 安全（F8）：先验 HMAC 签名（防 DB 写权限伪造），再按摘要查找，最后检查一次性+过期。
func (m *MemoryStore) ConsumeToken(token string) (deviceID, tenantID string, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !verifyTokenMAC(m.secret, token) {
		return "", "", false
	}
	tm, exists := m.tokens[hashToken(token)]
	if !exists {
		return "", "", false
	}
	if tm.consumed {
		return "", "", false
	}
	if time.Now().After(tm.expiresAt) {
		return "", "", false
	}
	tm.consumed = true
	return tm.deviceID, tm.tenantID, true
}

// auditCap 审计事件环形上限，避免长周期运行无界增长导致 OOM（P2-15）。
const auditCap = 10000

// Audit 记录一条审计事件（U-04 等保三级：操作 100% 留痕）。
func (m *MemoryStore) Audit(e *proto.AuditEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appendAudit(e)
}

// appendAudit 在已持有 m.mu 时追加审计，供 Register 等持锁路径内部调用，避免重入死锁。
func (m *MemoryStore) appendAudit(e *proto.AuditEvent) {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	m.audits = append(m.audits, e)
	if len(m.audits) > auditCap {
		// 保留最近 auditCap 条（丢弃最旧），防止无限增长。
		m.audits = m.audits[len(m.audits)-auditCap:]
	}
}

// Audits 返回已记录审计事件副本。
func (m *MemoryStore) Audits() []*proto.AuditEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*proto.AuditEvent, len(m.audits))
	copy(out, m.audits)
	return out
}

// QueryAudits 按租户/动作/时间窗过滤审计事件（P0-4 审计可查；U-04 等保三级留痕必须可检索）。
// tenant/action 为空表示不限；since/until 为零值表示不限；limit<=0 表示不限制（返回全部匹配）。
// 返回按时间倒序（最新在前）。
func (m *MemoryStore) QueryAudits(tenant, action string, since, until time.Time, limit int) []*proto.AuditEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*proto.AuditEvent, 0, len(m.audits))
	for _, e := range m.audits {
		if tenant != "" && e.TenantID != tenant {
			continue
		}
		if action != "" && e.Action != action {
			continue
		}
		if !since.IsZero() && e.CreatedAt.Before(since) {
			continue
		}
		if !until.IsZero() && e.CreatedAt.After(until) {
			continue
		}
		out = append(out, e)
	}
	// 倒序：最新在前
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// AllTasks 返回全部任务（tenantID 非空时按租户过滤；供任务列表端点）。
func (m *MemoryStore) AllTasks(tenantID string) []*proto.Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*proto.Task, 0)
	for _, ts := range m.tasks {
		for _, t := range ts {
			if tenantID != "" && t.TenantID != tenantID {
				continue
			}
			c := *t
			out = append(out, &c)
		}
	}
	return out
}

// Device 按 deviceID 返回单台设备（供设备详情端点）。
func (m *MemoryStore) Device(id string) *proto.DeviceInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, devs := range m.segments {
		for _, d := range devs {
			if d.DeviceID == id {
				c := *d
				return &c
			}
		}
	}
	return nil
}

// Results 返回某 agent 的上报结果（供设备详情端点）。
func (m *MemoryStore) Results(agentID string) []*proto.TaskResult {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*proto.TaskResult, len(m.results[agentID]))
	copy(out, m.results[agentID])
	return out
}

// Agents 返回已注册 agent（tenantID 非空时按租户过滤）。
func (m *MemoryStore) Agents(tenantID string) []*proto.AgentInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*proto.AgentInfo, 0, len(m.agents))
	for _, a := range m.agents {
		if tenantID != "" && a.TenantID != tenantID {
			continue
		}
		c := *a
		out = append(out, &c)
	}
	return out
}

// Agent 按 agentID 直接返回单台 agent（O(1) 直查；返回深拷贝避免 data race，P2-17）。
func (m *MemoryStore) Agent(id string) *proto.AgentInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.agents[id]
	if !ok {
		return nil
	}
	c := *a
	return &c
}

// RenewLeadership MemoryStore 恒为 leader（单实例；config 已拒绝 memory+replicas>1 的多副本分裂）。
func (m *MemoryStore) RenewLeadership(ttl time.Duration) bool { return true }

// IsLeader MemoryStore 恒为 true。
func (m *MemoryStore) IsLeader() bool { return true }

// CancelledTaskIDs 返回该 agent 当前 cancelled 状态的任务 ID（F3 取消信号下发用）。
func (m *MemoryStore) CancelledTaskIDs(agentID string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []string
	for _, t := range m.tasks[agentID] {
		if t.Status == "cancelled" {
			out = append(out, t.TaskID)
		}
	}
	return out
}

// AgentSecret 返回该 agent 的 HMAC 签名密钥（task 81 gRPC 身份绑定）。
// agent 不存在或未生成密钥时返回空串（调用方据此判断是否需要签名）。
func (m *MemoryStore) AgentSecret(agentID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.agentSecrets[agentID]
}

// RetireStaleDevices F5 离线超龄自动归档：扫描最后心跳早于 maxAge 的 agent 对应设备
// （或已无 agent 的孤儿设备），批量标记 retired。返回归档数。
func (m *MemoryStore) RetireStaleDevices(maxAge time.Duration) int {
	if maxAge <= 0 {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	n := 0
	for _, devs := range m.segments {
		for _, d := range devs {
			if d.Retired {
				continue
			}
			aged := true
			if d.AgentID != "" {
				if a, ok := m.agents[d.AgentID]; ok {
					aged = a.LastSeen.Before(cutoff)
				}
			}
			if aged {
				d.Retired = true
				d.State = "offline"
				n++
			}
		}
	}
	return n
}

// CleanupTokens 清理过期/已消费的 install token（F9 无界增长防护）。
func (m *MemoryStore) CleanupTokens(batch int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	n := 0
	for key, tm := range m.tokens {
		if batch > 0 && n >= batch {
			break
		}
		if tm.consumed || now.After(tm.expiresAt) {
			delete(m.tokens, key)
			n++
		}
	}
	return n
}

// Snapshot 返回 segment -> 设备列表的当前视图（tenantID 非空时按租户过滤）。
func (m *MemoryStore) Snapshot(tenantID string) map[string][]proto.DeviceInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string][]proto.DeviceInfo, len(m.segments))
	for seg, devs := range m.segments {
		list := make([]proto.DeviceInfo, 0, len(devs))
		for _, d := range devs {
			if tenantID != "" && d.TenantID != tenantID {
				continue
			}
			if d.Retired {
				continue // F5 退役设备不出现在活跃清单
			}
			list = append(list, *d)
		}
		out[seg] = list
	}
	return out
}

// StoreDeviceMetrics 存储设备最新监控指标（agent 心跳上报，仅保留最新值）。
// deviceID 为空或 metrics 为 nil 时直接返回。深拷贝入参避免外部并发修改。
func (m *MemoryStore) StoreDeviceMetrics(deviceID string, metrics *proto.DeviceMetrics) {
	if deviceID == "" || metrics == nil {
		return
	}
	cp := *metrics
	m.mu.Lock()
	m.deviceMetrics[deviceID] = &cp
	m.mu.Unlock()
}

// DeviceMetrics 返回设备最新监控指标（无数据时返回 nil）。返回深拷贝避免外部并发修改。
func (m *MemoryStore) DeviceMetrics(deviceID string) *proto.DeviceMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.deviceMetrics[deviceID]
	if !ok || v == nil {
		return nil
	}
	cp := *v
	return &cp
}

// ============================================================================
// task 100 任务审批：ApproveTask / RejectTask
// ============================================================================

// ApproveTask 审批通过任务（task 100）：将 pending_approval 状态翻转回 pending，
// 记录审批人/审批时间。仅 pending_approval 状态可审批；其他状态返回 false。
// tenantID 非空时校验任务归属，越权返回 false。
func (m *MemoryStore) ApproveTask(id, tenantID, approvedBy string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ts := range m.tasks {
		for _, t := range ts {
			if t.TaskID != id {
				continue
			}
			if tenantID != "" && t.TenantID != tenantID {
				return false
			}
			if t.Status != "pending_approval" {
				return false
			}
			t.Status = "pending"
			t.ApprovedBy = approvedBy
			t.ApprovedAt = time.Now()
			m.publish(events.Event{Action: "task_approved", Target: t.TaskID, TenantID: t.TenantID,
				Detail: "by=" + approvedBy, Level: events.LevelInfo})
			return true
		}
	}
	return false
}

// RejectTask 驳回任务（task 100）：将 pending_approval 状态置为 rejected，
// 记录审批人/审批时间。被驳回任务永不进入 ClaimTask 队列。仅 pending_approval 状态可驳回。
// tenantID 非空时校验任务归属，越权返回 false。
func (m *MemoryStore) RejectTask(id, tenantID, approvedBy string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ts := range m.tasks {
		for _, t := range ts {
			if t.TaskID != id {
				continue
			}
			if tenantID != "" && t.TenantID != tenantID {
				return false
			}
			if t.Status != "pending_approval" {
				return false
			}
			t.Status = "rejected"
			t.ApprovedBy = approvedBy
			t.ApprovedAt = time.Now()
			m.publish(events.Event{Action: "task_rejected", Target: t.TaskID, TenantID: t.TenantID,
				Detail: "by=" + approvedBy, Level: events.LevelWarn})
			return true
		}
	}
	return false
}

// ============================================================================
// task 100 告警规则：CreateAlertRule / ListAlertRules / DeleteAlertRule
// ============================================================================

// randAlertRuleID 生成随机告警规则 ID（16 字节十六进制，crypto/rand 密码学安全）。
// 用于 CreateAlertRule 分配 ID（调用方未填 ID 时）。
func randAlertRuleID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 熵源失败回退时间戳（降级但可容忍，唯一性由 alertRules map key 兜底）。
		return fmt.Sprintf("alert-rule-%d", time.Now().UnixNano())
	}
	return "alert-rule-" + hex.EncodeToString(b)
}

// CreateAlertRule 创建告警规则（task 100）：ID 为空时由 store 分配随机 ID；
// TenantID 为空时归一为 default。返回持久化后的规则（含分配的 ID）。
func (m *MemoryStore) CreateAlertRule(r *AlertRule) *AlertRule {
	if r == nil {
		return nil
	}
	if r.TenantID == "" {
		r.TenantID = "default"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.ID == "" {
		r.ID = randAlertRuleID()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}
	// 深拷贝存储，避免调用方继续修改 r 影响 store 内部状态
	// （AlertRule 字段均为值类型，浅拷贝即深拷贝）。
	stored := *r
	m.alertRules[r.ID] = &stored
	ret := *r
	return &ret
}

// ListAlertRules 返回告警规则（task 100）；tenantID 非空时按租户过滤。
// 按创建时间升序返回深拷贝，避免外部修改破坏内部状态。
func (m *MemoryStore) ListAlertRules(tenantID string) []*AlertRule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*AlertRule, 0, len(m.alertRules))
	for _, r := range m.alertRules {
		if tenantID != "" && r.TenantID != tenantID {
			continue
		}
		cp := *r
		out = append(out, &cp)
	}
	// 按创建时间升序（插入排序，与 ListK8sClusters 风格一致）。
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].CreatedAt.Before(out[j-1].CreatedAt) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

// DeleteAlertRule 删除告警规则（task 100），返回是否删除成功（不存在返回 false）。
func (m *MemoryStore) DeleteAlertRule(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.alertRules[id]; !ok {
		return false
	}
	delete(m.alertRules, id)
	return true
}

// ============================================================================
// task 100 OS 安装模板：SaveOSTemplate / ListOSTemplates / GetOSTemplate / DeleteOSTemplate
// ============================================================================

// randOSTemplateID 生成随机 OS 安装模板 ID（16 字节十六进制，crypto/rand 密码学安全）。
func randOSTemplateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("os-tmpl-%d", time.Now().UnixNano())
	}
	return "os-tmpl-" + hex.EncodeToString(b)
}

// SaveOSTemplate 创建或更新 OS 安装模板（按 ID 幂等）。
// ID 为空时分配随机 ID；TenantID 为空时归一为 default；
// CreatedAt 为空时填当前时间；UpdatedAt 始终刷新。
func (m *MemoryStore) SaveOSTemplate(t *OSTemplate) error {
	if t == nil {
		return nil
	}
	if t.TenantID == "" {
		t.TenantID = "default"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if t.ID == "" {
		t.ID = randOSTemplateID()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	// 深拷贝存储，避免调用方继续修改 t 影响 store 内部状态
	// （OSTemplate 字段均为值类型，浅拷贝即深拷贝）。
	stored := *t
	m.osTemplates[t.ID] = &stored
	return nil
}

// ListOSTemplates 返回 OS 安装模板（按创建时间升序；深拷贝）；tenantID 非空时按租户过滤。
func (m *MemoryStore) ListOSTemplates(tenantID string) []*OSTemplate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*OSTemplate, 0, len(m.osTemplates))
	for _, t := range m.osTemplates {
		if tenantID != "" && t.TenantID != tenantID {
			continue
		}
		cp := *t
		out = append(out, &cp)
	}
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].CreatedAt.Before(out[j-1].CreatedAt) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

// GetOSTemplate 按 ID 返回单个 OS 安装模板（深拷贝；不存在返回 nil）。
func (m *MemoryStore) GetOSTemplate(id string) *OSTemplate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.osTemplates[id]
	if !ok {
		return nil
	}
	cp := *t
	return &cp
}

// DeleteOSTemplate 删除 OS 安装模板，返回是否删除成功（不存在返回 false）。
func (m *MemoryStore) DeleteOSTemplate(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.osTemplates[id]; !ok {
		return false
	}
	delete(m.osTemplates, id)
	return true
}

// ============================================================================
// task 100 中间件部署模板：SaveMiddlewareTemplate / ListMiddlewareTemplates / GetMiddlewareTemplate / DeleteMiddlewareTemplate
// ============================================================================

// randMiddlewareTemplateID 生成随机中间件部署模板 ID（16 字节十六进制，crypto/rand 密码学安全）。
func randMiddlewareTemplateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("mw-tmpl-%d", time.Now().UnixNano())
	}
	return "mw-tmpl-" + hex.EncodeToString(b)
}

// SaveMiddlewareTemplate 创建或更新中间件部署模板（按 ID 幂等）。
// ID 为空时分配随机 ID；TenantID 为空时归一为 default；
// CreatedAt 为空时填当前时间；UpdatedAt 始终刷新。
func (m *MemoryStore) SaveMiddlewareTemplate(t *MiddlewareTemplate) error {
	if t == nil {
		return nil
	}
	if t.TenantID == "" {
		t.TenantID = "default"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if t.ID == "" {
		t.ID = randMiddlewareTemplateID()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	// 深拷贝存储，避免调用方继续修改 t 影响 store 内部状态
	// （MiddlewareTemplate 字段均为值类型，浅拷贝即深拷贝）。
	stored := *t
	m.middlewareTemplates[t.ID] = &stored
	return nil
}

// ListMiddlewareTemplates 返回中间件部署模板（按创建时间升序；深拷贝）；tenantID 非空时按租户过滤。
func (m *MemoryStore) ListMiddlewareTemplates(tenantID string) []*MiddlewareTemplate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*MiddlewareTemplate, 0, len(m.middlewareTemplates))
	for _, t := range m.middlewareTemplates {
		if tenantID != "" && t.TenantID != tenantID {
			continue
		}
		cp := *t
		out = append(out, &cp)
	}
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].CreatedAt.Before(out[j-1].CreatedAt) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

// GetMiddlewareTemplate 按 ID 返回单个中间件部署模板（深拷贝；不存在返回 nil）。
func (m *MemoryStore) GetMiddlewareTemplate(id string) *MiddlewareTemplate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.middlewareTemplates[id]
	if !ok {
		return nil
	}
	cp := *t
	return &cp
}

// DeleteMiddlewareTemplate 删除中间件部署模板，返回是否删除成功（不存在返回 false）。
func (m *MemoryStore) DeleteMiddlewareTemplate(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.middlewareTemplates[id]; !ok {
		return false
	}
	delete(m.middlewareTemplates, id)
	return true
}
