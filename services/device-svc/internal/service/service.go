package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	devicev1 "github.com/Levango7/OpsMesh/services/device-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/device-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/device-svc/internal/store"
	"opsmesh/pkg/discover"
	"opsmesh/pkg/metrics"
	"opsmesh/pkg/provision"
	"opsmesh/pkg/retry"
	"opsmesh/pkg/tenant"
)

// Errors returned by the service.
var (
	ErrDeviceNotFound = errors.New("device not found")
	ErrDeviceInvalid  = errors.New("device invalid")
	ErrAgentNotFound  = errors.New("agent not found")
	ErrAgentInvalid   = errors.New("agent invalid")
	ErrCINotFound     = errors.New("CI not found")
	ErrCIInvalid      = errors.New("CI invalid")
	ErrJobNotFound    = errors.New("discovery job not found")
	ErrJobInvalid     = errors.New("discovery job invalid")
)

// Service implements the device service business logic.
type Service struct {
	deviceStore    store.DeviceStore
	agentStore     store.AgentStore
	ciStore        store.CiStore
	discoveryStore store.DiscoveryStore
	provisionStore store.ProvisionStore
	tenantMgr      *tenant.Manager
	// discoverCfg D2 Discovery 真实化配置（白名单/超时）。nil=默认值（白名单空=不校验，
	// 超时 60s）——测试直构 Service 时可零值，生产由 main 注入 config.Load() 结果。
	discoverCfg *DiscoverConfig
	// autoProvisionCfg D3 自动纳管配置（默认 nil=自动纳管关闭）。
	// 由 main 从 config.Load() 映射注入。
	autoProvisionCfg *AutoProvisionConfig
}

// DiscoverConfig D2 真实发现的运行参数（由 main 从 config.Load() 映射注入）。
type DiscoverConfig struct {
	// CIDRWhitelist 逗号分隔白名单；空=不校验（与 controlplane 同语义）。
	CIDRWhitelist string
	// Timeout 单个发现 job 的整体超时。
	Timeout time.Duration
}

// AutoProvisionConfig D3 自动纳管的运行参数（由 main 从 config.Load() 映射注入）。
type AutoProvisionConfig struct {
	// Enabled 启用自动纳管编排（默认 false，双闸第一闸）。
	Enabled bool
	// FallbackAdvertise advertise 为空时回退的地址（通常 http://127.0.0.1:8081）。
	FallbackAdvertise string
	// SSHKey SSH 私钥路径（空=仅签发 token 不推送，第二闸）。
	SSHKey string
	// SSHUser SSH 登录用户。
	SSHUser string
	// SSHKP SSH 私钥密码（可选）。
	SSHKP string
	// SSHKnownHosts KnownHosts 文件路径（生产必配，防 MITM）。
	SSHKnownHosts string
	// AdvertiseAddr 控制面对外地址（用于拼接 bootstrap URL）。
	AdvertiseAddr string
}

// SetAutoProvisionConfig 注入 D3 自动纳管配置（main 启动时调用；测试可省略走默认值）。
func (s *Service) SetAutoProvisionConfig(cfg *AutoProvisionConfig) {
	s.autoProvisionCfg = cfg
}

// RunAutoProvision 执行自动纳管编排（与 controlplane AutoProvision 行为等价）：
//
//	对每段 CIDR：discover.Sweep 存活扫描 → dev-{ip} 幂等入库 discovered → 签发一次性 install token →
//	（配置 SSHKey 时）通过 SSH 推送 bootstrap 完成 agent 安装。
//
// 双闸默认关闭（Enabled=false 时立即返回错误）；SSH 推送受 SSHKey 配置控制。
// 返回 provision.Summary（含 Scanned/Registered/Provisioned/SSHPushed/Failures）。
func (s *Service) RunAutoProvision(ctx context.Context, cidrs []string, tenantID string) (*provision.Summary, error) {
	if s.autoProvisionCfg == nil || !s.autoProvisionCfg.Enabled {
		return nil, fmt.Errorf("provision: 自动纳管未启用（需配置 DEVICE_SVC_AUTO_PROVISION=true）")
	}
	// SSRF 防护：CIDR 白名单校验（与 controlplane ValidateCIDR 同语义）。
	// validateDiscoveryCIDR 内部读取 s.discoverWhitelist(); 白名单为空时不校验（向后兼容）。
	for _, cidr := range cidrs {
		if err := s.validateDiscoveryCIDR(cidr); err != nil {
			return nil, fmt.Errorf("provision: CIDR %q 不在白名单: %w", cidr, err)
		}
	}
	cfg := provision.Config{
		AdvertiseAddr:          s.autoProvisionCfg.AdvertiseAddr,
		FallbackAdvertise:      s.autoProvisionCfg.FallbackAdvertise,
		ProvisionSSHUser:       s.autoProvisionCfg.SSHUser,
		ProvisionSSHKey:        s.autoProvisionCfg.SSHKey,
		ProvisionSSHKP:         s.autoProvisionCfg.SSHKP,
		ProvisionSSHKnownHosts: s.autoProvisionCfg.SSHKnownHosts,
	}
	deps := provision.DeviceDeps{
		UpsertDevice: func(deviceID, ip, cidr, tntID string) {
			s.deviceStore.RegisterDevice(&models.Device{
				ID:       deviceID,
				IP:       ip,
				TenantID: tntID,
				Status:   "discovered",
			})
		},
		Provision: func(deviceID, host, tntID string) (token string, bootstrap string, err error) {
			token, err = s.provisionStore.IssueToken(deviceID, tntID, 15*time.Minute)
			return
		},
	}
	return provision.AutoProvision(ctx, deps, cfg, cidrs, tenantID)
}

// NewService creates a new Service.
func NewService(ds store.DeviceStore, as store.AgentStore, cs store.CiStore, disc store.DiscoveryStore, ps store.ProvisionStore, tm *tenant.Manager) *Service {
	return &Service{
		deviceStore:    ds,
		agentStore:     as,
		ciStore:        cs,
		discoveryStore: disc,
		provisionStore: ps,
		tenantMgr:      tm,
	}
}

// SetProvisionStore 注入 ProvisionStore（main 启动时调用；测试可省略走默认值）。
func (s *Service) SetProvisionStore(ps store.ProvisionStore) {
	s.provisionStore = ps
}

// SetDiscoverConfig 注入 D2 真实发现配置（main 启动时调用；测试可省略走默认值）。
func (s *Service) SetDiscoverConfig(cfg *DiscoverConfig) {
	s.discoverCfg = cfg
}

// discoverWhitelist 返回生效的白名单（未注入配置时为空=不校验）。
func (s *Service) discoverWhitelist() string {
	if s.discoverCfg == nil {
		return ""
	}
	return s.discoverCfg.CIDRWhitelist
}

// discoverTimeout 返回生效的 job 超时（未配置时 60s）。
func (s *Service) discoverTimeout() time.Duration {
	if s.discoverCfg == nil || s.discoverCfg.Timeout <= 0 {
		return 60 * time.Second
	}
	return s.discoverCfg.Timeout
}

// validateDiscoveryCIDR 目标 CIDR 必须完全落在白名单内（防扫描云元数据网段/内网探测）。
// 白名单为空时不校验（向后兼容）。语义与 controlplane server_netsec.go ValidateCIDR 一致：
// 目标网段起止 IP 都必须落在同一条允许的 CIDR 内。
func (s *Service) validateDiscoveryCIDR(cidr string) error {
	whitelist := s.discoverWhitelist()
	if whitelist == "" {
		return nil
	}
	_, targetNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("invalid target CIDR %q: %w", cidr, err)
	}
	targetStart, targetEnd := cidrBounds(targetNet)
	for _, allowed := range strings.Split(whitelist, ",") {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" {
			continue
		}
		_, n, err := net.ParseCIDR(allowed)
		if err != nil {
			return fmt.Errorf("invalid allowed CIDR %q: %w", allowed, err)
		}
		if n.Contains(targetStart) && n.Contains(targetEnd) {
			return nil
		}
	}
	return fmt.Errorf("target CIDR %q not within any allowed CIDR in whitelist", cidr)
}

// cidrBounds 返回 CIDR 网段的起始 IP 与结束 IP（网络地址与广播地址）。
func cidrBounds(n *net.IPNet) (net.IP, net.IP) {
	start := make(net.IP, len(n.IP))
	copy(start, n.IP)
	end := make(net.IP, len(n.IP))
	copy(end, n.IP)
	for i := range end {
		end[i] = n.IP[i] | ^n.Mask[i]
	}
	return start, end
}

// === DeviceService methods ===

// RegisterDevice registers a new device.
func (s *Service) RegisterDevice(ctx context.Context, req *devicev1.RegisterDeviceRequest) (*devicev1.Device, error) {
	if req.Device == nil {
		return nil, ErrDeviceInvalid
	}

	// Enforce device count quota for the tenant.
	if s.tenantMgr != nil {
		tenantID, err := tenant.TenantIDFromContext(ctx)
		if err != nil {
			tenantID = req.Device.TenantId
		}
		if err := s.tenantMgr.EnforceQuota(ctx, tenantID, tenant.ResourceDevices, 1); err != nil {
			return nil, fmt.Errorf("device quota exceeded: %w", err)
		}
	}

	now := timestamppb.Now()
	d := req.Device
	if d.Id == "" {
		d.Id = "dev-" + uuid.New().String()[:8]
	}
	d.Status = "online"
	d.CreatedAt = now
	d.UpdatedAt = now

	storeDev := protoToDevice(d)
	s.deviceStore.RegisterDevice(storeDev)

	// Track device usage for the tenant.
	if s.tenantMgr != nil {
		tenantID, _ := tenant.TenantIDFromContext(ctx)
		if tenantID == "" {
			tenantID = d.TenantId
		}
		_ = s.tenantMgr.TrackUsage(ctx, tenantID, tenant.ResourceDevices, 1)
	}

	return d, nil
}

// Heartbeat updates device heartbeat.
func (s *Service) HeartbeatDevice(ctx context.Context, req *devicev1.HeartbeatRequest) error {
	var lastErr error
	err := retry.Do(func() error {
		ok := s.deviceStore.Heartbeat(req.DeviceId, req.Status)
		if !ok {
			lastErr = ErrDeviceNotFound
			return retry.Retryable(ErrDeviceNotFound)
		}
		return nil
	}, 3, 50*time.Millisecond)
	if err != nil {
		metrics.RecordBusinessMetric("device_heartbeat_failures", 1, map[string]string{"device_id": req.DeviceId})
		return lastErr
	}
	metrics.RecordBusinessMetric("device_heartbeats_total", 1, map[string]string{"device_id": req.DeviceId})
	return nil
}

// GetDevice retrieves a device by ID.
func (s *Service) GetDevice(ctx context.Context, req *devicev1.GetDeviceRequest) (*devicev1.Device, error) {
	d := s.deviceStore.Device(req.Id)
	if d == nil {
		return nil, ErrDeviceNotFound
	}
	return deviceToProto(d), nil
}

// ListDevices lists devices with filtering.
func (s *Service) ListDevices(ctx context.Context, req *devicev1.ListDevicesRequest) (*devicev1.ListDevicesResponse, error) {
	devices := s.deviceStore.ListDevices(req.TenantId, req.Status, req.Group, int(req.Limit))
	out := make([]*devicev1.Device, 0, len(devices))
	for _, d := range devices {
		out = append(out, deviceToProto(d))
	}
	return &devicev1.ListDevicesResponse{Devices: out}, nil
}

// UpdateDevice updates device information.
func (s *Service) UpdateDevice(ctx context.Context, req *devicev1.UpdateDeviceRequest) (*devicev1.Device, error) {
	if req.Device == nil {
		return nil, ErrDeviceInvalid
	}

	storeDev := protoToDevice(req.Device)
	updated, ok := s.deviceStore.UpdateDevice(storeDev)
	if !ok {
		return nil, ErrDeviceNotFound
	}
	return deviceToProto(updated), nil
}

// DeleteDevice removes a device.
func (s *Service) DeleteDevice(ctx context.Context, req *devicev1.DeleteDeviceRequest) error {
	ok := s.deviceStore.DeleteDevice(req.Id)
	if !ok {
		return ErrDeviceNotFound
	}
	return nil
}

// GetDeviceStatus returns device status.
func (s *Service) GetDeviceStatus(ctx context.Context, req *devicev1.GetDeviceStatusRequest) (*devicev1.DeviceStatus, error) {
	status := s.deviceStore.GetDeviceStatus(req.DeviceId)
	if status == nil {
		return nil, ErrDeviceNotFound
	}
	return &devicev1.DeviceStatus{
		DeviceId:      status.DeviceID,
		Status:        status.Status,
		Reachable:     status.Reachable,
		UptimeSeconds: status.UptimeSeconds,
		LastHeartbeat: timestamppb.New(status.LastHeartbeat),
	}, nil
}

// === AgentService methods ===

// RegisterAgent registers a new agent.
func (s *Service) RegisterAgent(ctx context.Context, req *devicev1.RegisterAgentRequest) (*devicev1.Agent, error) {
	if req.Agent == nil {
		return nil, ErrAgentInvalid
	}

	now := timestamppb.Now()
	a := req.Agent
	if a.Id == "" {
		a.Id = "agent-" + uuid.New().String()[:8]
	}
	a.Status = "online"
	a.CreatedAt = now
	a.UpdatedAt = now

	storeAgent := protoToAgent(a)
	s.agentStore.RegisterAgent(storeAgent)

	return a, nil
}

// GetAgent retrieves an agent by ID.
func (s *Service) GetAgent(ctx context.Context, req *devicev1.GetAgentRequest) (*devicev1.Agent, error) {
	a := s.agentStore.Agent(req.Id)
	if a == nil {
		return nil, ErrAgentNotFound
	}
	return agentToProto(a), nil
}

// ListAgents lists agents with filtering.
func (s *Service) ListAgents(ctx context.Context, req *devicev1.ListAgentsRequest) (*devicev1.ListAgentsResponse, error) {
	agents := s.agentStore.ListAgents(req.TenantId, req.Status, int(req.Limit))
	out := make([]*devicev1.Agent, 0, len(agents))
	for _, a := range agents {
		out = append(out, agentToProto(a))
	}
	return &devicev1.ListAgentsResponse{Agents: out}, nil
}

// UpdateAgentStatus updates agent status.
func (s *Service) UpdateAgentStatus(ctx context.Context, req *devicev1.UpdateAgentStatusRequest) (*devicev1.Agent, error) {
	updated, ok := s.agentStore.UpdateAgentStatus(req.AgentId, req.Status, int(req.Load))
	if !ok {
		return nil, ErrAgentNotFound
	}
	return agentToProto(updated), nil
}

// HeartbeatAgent updates agent heartbeat.
func (s *Service) HeartbeatAgent(ctx context.Context, req *devicev1.AgentHeartbeatRequest) error {
	var lastErr error
	err := retry.Do(func() error {
		ok := s.agentStore.AgentHeartbeat(req.AgentId, req.Status, int(req.Load))
		if !ok {
			lastErr = ErrAgentNotFound
			return retry.Retryable(ErrAgentNotFound)
		}
		return nil
	}, 3, 50*time.Millisecond)
	if err != nil {
		metrics.RecordBusinessMetric("agent_heartbeat_failures", 1, map[string]string{"agent_id": req.AgentId})
		return lastErr
	}
	metrics.RecordBusinessMetric("agent_heartbeats_total", 1, map[string]string{"agent_id": req.AgentId})
	return nil
}

// === CMDBService methods ===

// CreateCI creates a new CI.
func (s *Service) CreateCI(ctx context.Context, req *devicev1.CreateCIRequest) (*devicev1.CI, error) {
	if req.Ci == nil {
		return nil, ErrCIInvalid
	}

	now := timestamppb.Now()
	ci := req.Ci
	if ci.Id == "" {
		ci.Id = "ci-" + uuid.New().String()[:8]
	}
	ci.Status = "active"
	ci.Version = 1
	ci.CreatedAt = now
	ci.UpdatedAt = now

	storeCI := protoToCI(ci)
	s.ciStore.CreateCI(storeCI)

	return ci, nil
}

// GetCI retrieves a CI by ID.
func (s *Service) GetCI(ctx context.Context, req *devicev1.GetCIRequest) (*devicev1.CI, error) {
	ci := s.ciStore.GetCI(req.Id, "")
	if ci == nil {
		return nil, ErrCINotFound
	}
	return ciToProto(ci), nil
}

// UpdateCI updates a CI.
func (s *Service) UpdateCI(ctx context.Context, req *devicev1.UpdateCIRequest) (*devicev1.CI, error) {
	if req.Ci == nil {
		return nil, ErrCIInvalid
	}

	storeCI := protoToCI(req.Ci)
	updated, ok := s.ciStore.UpdateCI(storeCI)
	if !ok {
		return nil, ErrCINotFound
	}
	return ciToProto(updated), nil
}

// DeleteCI removes a CI.
func (s *Service) DeleteCI(ctx context.Context, req *devicev1.DeleteCIRequest) error {
	ok := s.ciStore.DeleteCI(req.Id, "")
	if !ok {
		return ErrCINotFound
	}
	return nil
}

// ListCIs lists CIs with filtering.
func (s *Service) ListCIs(ctx context.Context, req *devicev1.ListCIsRequest) (*devicev1.ListCIsResponse, error) {
	cis := s.ciStore.ListCIs(req.TenantId, req.CiType, req.Status, int(req.Limit))
	out := make([]*devicev1.CI, 0, len(cis))
	for _, ci := range cis {
		out = append(out, ciToProto(ci))
	}
	return &devicev1.ListCIsResponse{Cis: out}, nil
}

// CreateCIRelation creates a CI relationship.
func (s *Service) CreateCIRelation(ctx context.Context, req *devicev1.CreateCIRelationRequest) (*devicev1.CIRelation, error) {
	if req.Relation == nil {
		return nil, errors.New("relation invalid")
	}

	storeRel := protoToRelation(req.Relation)
	s.ciStore.CreateRelation(storeRel)

	return req.Relation, nil
}

// GetCIRelations retrieves CI relations.
func (s *Service) GetCIRelations(ctx context.Context, req *devicev1.GetCIRelationsRequest) (*devicev1.GetCIRelationsResponse, error) {
	rels := s.ciStore.GetCIRelations(req.CiId, "")
	out := make([]*devicev1.CIRelation, 0, len(rels))
	for _, r := range rels {
		out = append(out, relationToProto(r))
	}
	return &devicev1.GetCIRelationsResponse{Relations: out}, nil
}

// === DiscoveryService methods ===

// StartDiscovery initiates network discovery.
//
// D2 真实化（2026-09）：原实现为硬编码 stub（同步写死 FoundDevices=3/ScannedHosts=254），
// 现改为真实 Sweep 扫描 + 候选设备入库：
//  1. CIDR 白名单校验（不通过直接报错，job 不创建）；
//  2. 创建 status=running 的 job，HTTP/gRPC 立即返回（异步语义，与 proto 的
//     pending/running/completed 状态机吻合——大网段扫描不再阻塞调用方）；
//  3. 后台 goroutine 用 pkg/discover.Sweep（TCP-connect 存活扫描，与
//     controlplane provision/auto.go:70 同参数：ports [22,9100]/并发 64/单连 800ms）
//     扫描目标网段；
//  4. 每个存活 IP 以 ID=dev-{ip} 幂等入库（Status=discovered，与 controlplane
//     UpsertDevice 的 State=discovered/Managed=false 候选设备语义对齐）；
//  5. job 以超时 ctx 兜底（默认 60s），完成/失败/超时均回写终态。
func (s *Service) StartDiscovery(ctx context.Context, req *devicev1.StartDiscoveryRequest) (*devicev1.DiscoveryJob, error) {
	if req.Cidr == "" {
		return nil, ErrJobInvalid
	}
	// 白名单校验失败：job 不落库直接报错（与 controlplane autoProvision 的 403 语义一致）。
	if err := s.validateDiscoveryCIDR(req.Cidr); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrJobInvalid, err)
	}

	now := timestamppb.Now()
	job := &devicev1.DiscoveryJob{
		Id:        "job-" + uuid.New().String()[:8],
		TenantId:  req.TenantId,
		Cidr:      req.Cidr,
		Status:    "running",
		StartedAt: now,
	}

	storeJob := &models.DiscoveryJob{
		ID:        job.Id,
		TenantID:  job.TenantId,
		CIDR:      job.Cidr,
		Status:    "running",
		StartedAt: now.AsTime(),
	}
	created := s.discoveryStore.CreateJob(storeJob)
	if created == nil {
		return nil, errors.New("create job failed")
	}

	// 后台扫描：job ctx 超时兜底；完成/失败/超时均回写终态（幂等 UpdateJob）。
	go s.runDiscoveryJob(created)

	job.Status = "running"
	return job, nil
}

// runDiscoveryJob 执行单次真实扫描并回写 job 终态。
// 扫描参数与 controlplane provision/auto.go:70 完全一致（ports [22,9100]/并发 64/
// 单连 800ms）——双轨对照前提：发现行为必须与 controlplane 的网段发现可对照。
func (s *Service) runDiscoveryJob(job *models.DiscoveryJob) {
	ctx, cancel := context.WithTimeout(context.Background(), s.discoverTimeout())
	defer cancel()

	alive, err := discover.Sweep(ctx, job.CIDR, []int{22, 9100}, 64, 800*time.Millisecond)
	if err != nil {
		job.Status = "failed"
		job.Error = err.Error()
		if !job.CompletedAt.IsZero() {
			job.CompletedAt = time.Now()
		} else {
			job.CompletedAt = time.Now()
		}
		s.discoveryStore.UpdateJob(job)
		return
	}

	// 候选设备入库：ID=dev-{ip} 幂等（重复扫描同网段不产生重复设备），
	// Status=discovered 与 controlplane UpsertDevice 的 State=discovered 语义对齐。
	for _, ip := range alive {
		d := &models.Device{
			ID:       "dev-" + ip,
			TenantID: job.TenantID,
			IP:       ip,
			Status:   "discovered",
		}
		s.deviceStore.RegisterDevice(d)
	}

	job.Status = "completed"
	job.ScannedHosts = len(alive) // 语义说明：此处记存活主机数（Sweep 只返回存活 IP，
	// 不返回探测过的全量主机数——原 stub 的 254 是网段理论容量）。真实网段容量
	// 展示留给前端按 CIDR 计算（与 controlplane 的行为差异已在 TD-60 登记）。
	job.FoundDevices = len(alive)
	job.CompletedAt = time.Now()
	s.discoveryStore.UpdateJob(job)
}

// GetDiscoveryStatus returns discovery job status.
func (s *Service) GetDiscoveryStatus(ctx context.Context, req *devicev1.GetDiscoveryStatusRequest) (*devicev1.DiscoveryJob, error) {
	job := s.discoveryStore.GetJob(req.JobId)
	if job == nil {
		return nil, ErrJobNotFound
	}
	return jobToProto(job), nil
}

// ListDiscoveredDevices lists devices from discovery.
func (s *Service) ListDiscoveredDevices(ctx context.Context, req *devicev1.ListDiscoveredDevicesRequest) (*devicev1.ListDiscoveredDevicesResponse, error) {
	devices := s.deviceStore.ListDevices(req.TenantId, "discovered", "", 0)
	out := make([]*devicev1.Device, 0, len(devices))
	for _, d := range devices {
		out = append(out, deviceToProto(d))
	}
	return &devicev1.ListDiscoveredDevicesResponse{Devices: out}, nil
}

// === Mapping functions ===

func protoToDevice(d *devicev1.Device) *models.Device {
	labels := make(map[string]string)
	if d.Labels != nil {
		labels = d.Labels
	}
	return &models.Device{
		ID:        d.Id,
		TenantID:  d.TenantId,
		Name:      d.Name,
		IP:        d.Ip,
		MAC:       d.Mac,
		OS:        d.Os,
		Arch:      d.Arch,
		Status:    d.Status,
		AgentID:   d.AgentId,
		Tags:      d.Tags,
		Labels:    labels,
		Group:     d.Group,
		CreatedAt: d.CreatedAt.AsTime(),
		UpdatedAt: d.UpdatedAt.AsTime(),
	}
}

func deviceToProto(d *models.Device) *devicev1.Device {
	return &devicev1.Device{
		Id:            d.ID,
		TenantId:      d.TenantID,
		Name:          d.Name,
		Ip:            d.IP,
		Mac:           d.MAC,
		Os:            d.OS,
		Arch:          d.Arch,
		Status:        d.Status,
		AgentId:       d.AgentID,
		Tags:          d.Tags,
		Labels:        d.Labels,
		Group:         d.Group,
		LastHeartbeat: timestamppb.New(d.LastHeartbeat),
		CreatedAt:     timestamppb.New(d.CreatedAt),
		UpdatedAt:     timestamppb.New(d.UpdatedAt),
	}
}

func protoToAgent(a *devicev1.Agent) *models.Agent {
	return &models.Agent{
		ID:          a.Id,
		TenantID:    a.TenantId,
		DeviceID:    a.DeviceId,
		Hostname:    a.Hostname,
		Version:     a.Version,
		Status:      a.Status,
		Load:        int(a.Load),
		OS:          a.Os,
		Arch:        a.Arch,
		Addr:        a.Addr,
		GRPCPort:    int(a.GrpcPort),
		MetricsPort: int(a.MetricsPort),
		CreatedAt:   a.CreatedAt.AsTime(),
		UpdatedAt:   a.UpdatedAt.AsTime(),
	}
}

func agentToProto(a *models.Agent) *devicev1.Agent {
	return &devicev1.Agent{
		Id:            a.ID,
		TenantId:      a.TenantID,
		DeviceId:      a.DeviceID,
		Hostname:      a.Hostname,
		Version:       a.Version,
		Status:        a.Status,
		Load:          int32(a.Load),
		Os:            a.OS,
		Arch:          a.Arch,
		Addr:          a.Addr,
		GrpcPort:      int32(a.GRPCPort),
		MetricsPort:   int32(a.MetricsPort),
		LastHeartbeat: timestamppb.New(a.LastHeartbeat),
		CreatedAt:     timestamppb.New(a.CreatedAt),
		UpdatedAt:     timestamppb.New(a.UpdatedAt),
	}
}

func protoToCI(ci *devicev1.CI) *models.CI {
	attrs := make(map[string]string)
	if ci.Attributes != nil {
		attrs = ci.Attributes
	}
	return &models.CI{
		ID:         ci.Id,
		TenantID:   ci.TenantId,
		CiType:     ci.CiType,
		Name:       ci.Name,
		Status:     ci.Status,
		Attributes: attrs,
		Source:     ci.Source,
		AgentID:    ci.AgentId,
		DeviceID:   ci.DeviceId,
		Version:    int(ci.Version),
		CreatedAt:  ci.CreatedAt.AsTime(),
		UpdatedAt:  ci.UpdatedAt.AsTime(),
	}
}

func ciToProto(ci *models.CI) *devicev1.CI {
	return &devicev1.CI{
		Id:         ci.ID,
		TenantId:   ci.TenantID,
		CiType:     ci.CiType,
		Name:       ci.Name,
		Status:     ci.Status,
		Attributes: ci.Attributes,
		Source:     ci.Source,
		AgentId:    ci.AgentID,
		DeviceId:   ci.DeviceID,
		Version:    int32(ci.Version),
		CreatedAt:  timestamppb.New(ci.CreatedAt),
		UpdatedAt:  timestamppb.New(ci.UpdatedAt),
	}
}

func protoToRelation(rel *devicev1.CIRelation) *models.CIRelation {
	attrs := make(map[string]string)
	if rel.Attributes != nil {
		attrs = rel.Attributes
	}
	return &models.CIRelation{
		ID:           rel.Id,
		SourceCIID:   rel.SourceCiId,
		TargetCIID:   rel.TargetCiId,
		RelationType: rel.RelationType,
		TenantID:     rel.TenantId,
		Attributes:   attrs,
		CreatedAt:    rel.CreatedAt.AsTime(),
	}
}

func relationToProto(rel *models.CIRelation) *devicev1.CIRelation {
	return &devicev1.CIRelation{
		Id:           rel.ID,
		SourceCiId:   rel.SourceCIID,
		TargetCiId:   rel.TargetCIID,
		RelationType: rel.RelationType,
		TenantId:     rel.TenantID,
		Attributes:   rel.Attributes,
		CreatedAt:    timestamppb.New(rel.CreatedAt),
	}
}

func jobToProto(job *models.DiscoveryJob) *devicev1.DiscoveryJob {
	return &devicev1.DiscoveryJob{
		Id:           job.ID,
		TenantId:     job.TenantID,
		Cidr:         job.CIDR,
		Status:       job.Status,
		TotalHosts:   int32(job.TotalHosts),
		ScannedHosts: int32(job.ScannedHosts),
		FoundDevices: int32(job.FoundDevices),
		Error:        job.Error,
		StartedAt:    timestamppb.New(job.StartedAt),
		CompletedAt:  timestamppb.New(job.CompletedAt),
	}
}
