package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	devicev1 "github.com/Levango7/OpsMesh/services/device-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/device-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/device-svc/internal/store"
	"opsmesh/pkg/metrics"
	"opsmesh/pkg/retry"
)

// Errors returned by the service.
var (
	ErrDeviceNotFound  = errors.New("device not found")
	ErrDeviceInvalid   = errors.New("device invalid")
	ErrAgentNotFound   = errors.New("agent not found")
	ErrAgentInvalid    = errors.New("agent invalid")
	ErrCINotFound      = errors.New("CI not found")
	ErrCIInvalid       = errors.New("CI invalid")
	ErrJobNotFound     = errors.New("discovery job not found")
	ErrJobInvalid      = errors.New("discovery job invalid")
)

// Service implements the device service business logic.
type Service struct {
	deviceStore   store.DeviceStore
	agentStore    store.AgentStore
	ciStore       store.CiStore
	discoveryStore store.DiscoveryStore
}

// NewService creates a new Service.
func NewService(ds store.DeviceStore, as store.AgentStore, cs store.CiStore, disc store.DiscoveryStore) *Service {
	return &Service{
		deviceStore:   ds,
		agentStore:    as,
		ciStore:       cs,
		discoveryStore: disc,
	}
}

// === DeviceService methods ===

// RegisterDevice registers a new device.
func (s *Service) RegisterDevice(ctx context.Context, req *devicev1.RegisterDeviceRequest) (*devicev1.Device, error) {
	if req.Device == nil {
		return nil, ErrDeviceInvalid
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
func (s *Service) StartDiscovery(ctx context.Context, req *devicev1.StartDiscoveryRequest) (*devicev1.DiscoveryJob, error) {
	if req.Cidr == "" {
		return nil, ErrJobInvalid
	}

	now := timestamppb.Now()
	job := &devicev1.DiscoveryJob{
		Id:       "job-" + uuid.New().String()[:8],
		TenantId: req.TenantId,
		Cidr:     req.Cidr,
		Status:   "running",
		StartedAt: now,
	}

	storeJob := &models.DiscoveryJob{
		ID:        job.Id,
		TenantID:  job.TenantId,
		CIDR:      job.Cidr,
		Status:    "running",
		StartedAt: now.AsTime(),
	}
	s.discoveryStore.CreateJob(storeJob)

	// Simulate discovery completion
	storeJob.Status = "completed"
	storeJob.FoundDevices = 3
	storeJob.ScannedHosts = 254
	storeJob.TotalHosts = 254
	storeJob.CompletedAt = time.Now()
	s.discoveryStore.UpdateJob(storeJob)

	job.Status = "completed"
	job.FoundDevices = 3
	job.ScannedHosts = 254
	job.TotalHosts = 254
	job.CompletedAt = timestamppb.Now()

	return job, nil
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
		Id:        d.ID,
		TenantId:  d.TenantID,
		Name:      d.Name,
		Ip:        d.IP,
		Mac:       d.MAC,
		Os:        d.OS,
		Arch:      d.Arch,
		Status:    d.Status,
		AgentId:   d.AgentID,
		Tags:      d.Tags,
		Labels:    d.Labels,
		Group:     d.Group,
		LastHeartbeat: timestamppb.New(d.LastHeartbeat),
		CreatedAt: timestamppb.New(d.CreatedAt),
		UpdatedAt: timestamppb.New(d.UpdatedAt),
	}
}

func protoToAgent(a *devicev1.Agent) *models.Agent {
	return &models.Agent{
		ID:        a.Id,
		TenantID:  a.TenantId,
		DeviceID:  a.DeviceId,
		Hostname:  a.Hostname,
		Version:   a.Version,
		Status:    a.Status,
		Load:      int(a.Load),
		OS:        a.Os,
		Arch:      a.Arch,
		Addr:      a.Addr,
		GRPCPort:  int(a.GrpcPort),
		MetricsPort: int(a.MetricsPort),
		CreatedAt: a.CreatedAt.AsTime(),
		UpdatedAt: a.UpdatedAt.AsTime(),
	}
}

func agentToProto(a *models.Agent) *devicev1.Agent {
	return &devicev1.Agent{
		Id:        a.ID,
		TenantId:  a.TenantID,
		DeviceId:  a.DeviceID,
		Hostname:  a.Hostname,
		Version:   a.Version,
		Status:    a.Status,
		Load:      int32(a.Load),
		Os:        a.OS,
		Arch:      a.Arch,
		Addr:      a.Addr,
		GrpcPort:  int32(a.GRPCPort),
		MetricsPort: int32(a.MetricsPort),
		LastHeartbeat: timestamppb.New(a.LastHeartbeat),
		CreatedAt: timestamppb.New(a.CreatedAt),
		UpdatedAt: timestamppb.New(a.UpdatedAt),
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
