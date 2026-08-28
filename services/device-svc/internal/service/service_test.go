package service

import (
	"context"
	"testing"

	devicev1 "github.com/Levango7/OpsMesh/services/device-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/device-svc/internal/store"
)

func newTestService() *Service {
	st := store.NewMemoryStore()
	return NewService(st, st, st, st, nil)
}

// === DeviceService Tests ===

func TestRegisterDevice(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	req := &devicev1.RegisterDeviceRequest{
		Device: &devicev1.Device{
			Name:     "test-server-01",
			Ip:       "192.168.1.100",
			Os:       "linux",
			Arch:     "amd64",
			TenantId: "tenant-1",
		},
	}

	d, err := svc.RegisterDevice(ctx, req)
	if err != nil {
		t.Fatalf("RegisterDevice failed: %v", err)
	}

	if d.Id == "" {
		t.Error("expected device ID to be set")
	}
	if d.Status != "online" {
		t.Errorf("expected status online, got %s", d.Status)
	}
	if d.CreatedAt == nil {
		t.Error("expected CreatedAt to be set")
	}
}

func TestRegisterDeviceNil(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.RegisterDevice(ctx, &devicev1.RegisterDeviceRequest{})
	if err != ErrDeviceInvalid {
		t.Fatalf("expected ErrDeviceInvalid, got: %v", err)
	}
}

func TestGetDevice(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.RegisterDevice(ctx, &devicev1.RegisterDeviceRequest{
		Device: &devicev1.Device{
			Name:     "test-server-02",
			Ip:       "192.168.1.101",
			TenantId: "tenant-1",
		},
	})
	if err != nil {
		t.Fatalf("RegisterDevice failed: %v", err)
	}

	got, err := svc.GetDevice(ctx, &devicev1.GetDeviceRequest{Id: created.Id})
	if err != nil {
		t.Fatalf("GetDevice failed: %v", err)
	}

	if got.Id != created.Id {
		t.Errorf("expected ID %s, got %s", created.Id, got.Id)
	}
	if got.Name != "test-server-02" {
		t.Errorf("expected name test-server-02, got %s", got.Name)
	}
}

func TestGetDeviceNotFound(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.GetDevice(ctx, &devicev1.GetDeviceRequest{Id: "nonexistent"})
	if err != ErrDeviceNotFound {
		t.Fatalf("expected ErrDeviceNotFound, got: %v", err)
	}
}

func TestListDevices(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := svc.RegisterDevice(ctx, &devicev1.RegisterDeviceRequest{
			Device: &devicev1.Device{
				Name:     "device-list",
				TenantId: "tenant-1",
			},
		})
		if err != nil {
			t.Fatalf("RegisterDevice failed: %v", err)
		}
	}

	resp, err := svc.ListDevices(ctx, &devicev1.ListDevicesRequest{TenantId: "tenant-1"})
	if err != nil {
		t.Fatalf("ListDevices failed: %v", err)
	}

	if len(resp.Devices) != 3 {
		t.Errorf("expected 3 devices, got %d", len(resp.Devices))
	}
}

func TestUpdateDevice(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.RegisterDevice(ctx, &devicev1.RegisterDeviceRequest{
		Device: &devicev1.Device{
			Name:     "original-name",
			TenantId: "tenant-1",
		},
	})
	if err != nil {
		t.Fatalf("RegisterDevice failed: %v", err)
	}

	created.Name = "updated-name"
	created.Ip = "10.0.0.1"

	updated, err := svc.UpdateDevice(ctx, &devicev1.UpdateDeviceRequest{Device: created})
	if err != nil {
		t.Fatalf("UpdateDevice failed: %v", err)
	}

	if updated.Name != "updated-name" {
		t.Errorf("expected name updated-name, got %s", updated.Name)
	}
	if updated.Ip != "10.0.0.1" {
		t.Errorf("expected IP 10.0.0.1, got %s", updated.Ip)
	}
}

func TestDeleteDevice(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.RegisterDevice(ctx, &devicev1.RegisterDeviceRequest{
		Device: &devicev1.Device{
			Name:     "to-delete",
			TenantId: "tenant-1",
		},
	})
	if err != nil {
		t.Fatalf("RegisterDevice failed: %v", err)
	}

	err = svc.DeleteDevice(ctx, &devicev1.DeleteDeviceRequest{Id: created.Id})
	if err != nil {
		t.Fatalf("DeleteDevice failed: %v", err)
	}

	_, err = svc.GetDevice(ctx, &devicev1.GetDeviceRequest{Id: created.Id})
	if err != ErrDeviceNotFound {
		t.Fatalf("expected ErrDeviceNotFound after delete, got: %v", err)
	}
}

func TestDeviceHeartbeat(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.RegisterDevice(ctx, &devicev1.RegisterDeviceRequest{
		Device: &devicev1.Device{
			Name:     "heartbeat-device",
			TenantId: "tenant-1",
		},
	})
	if err != nil {
		t.Fatalf("RegisterDevice failed: %v", err)
	}

	err = svc.HeartbeatDevice(ctx, &devicev1.HeartbeatRequest{
		DeviceId: created.Id,
		Status:   "online",
	})
	if err != nil {
		t.Fatalf("HeartbeatDevice failed: %v", err)
	}
}

// === AgentService Tests ===

func TestRegisterAgent(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	req := &devicev1.RegisterAgentRequest{
		Agent: &devicev1.Agent{
			Hostname: "agent-host-01",
			Version:  "1.0.0",
			Os:       "linux",
			Arch:     "amd64",
			TenantId: "tenant-1",
		},
	}

	a, err := svc.RegisterAgent(ctx, req)
	if err != nil {
		t.Fatalf("RegisterAgent failed: %v", err)
	}

	if a.Id == "" {
		t.Error("expected agent ID to be set")
	}
	if a.Status != "online" {
		t.Errorf("expected status online, got %s", a.Status)
	}
}

func TestGetAgentNotFound(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.GetAgent(ctx, &devicev1.GetAgentRequest{Id: "nonexistent"})
	if err != ErrAgentNotFound {
		t.Fatalf("expected ErrAgentNotFound, got: %v", err)
	}
}

func TestUpdateAgentStatus(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.RegisterAgent(ctx, &devicev1.RegisterAgentRequest{
		Agent: &devicev1.Agent{
			Hostname: "agent-status",
			TenantId: "tenant-1",
		},
	})
	if err != nil {
		t.Fatalf("RegisterAgent failed: %v", err)
	}

	updated, err := svc.UpdateAgentStatus(ctx, &devicev1.UpdateAgentStatusRequest{
		AgentId: created.Id,
		Status:  "stale",
		Load:    42,
	})
	if err != nil {
		t.Fatalf("UpdateAgentStatus failed: %v", err)
	}

	if updated.Status != "stale" {
		t.Errorf("expected status stale, got %s", updated.Status)
	}
	if updated.Load != 42 {
		t.Errorf("expected load 42, got %d", updated.Load)
	}
}

func TestAgentHeartbeat(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.RegisterAgent(ctx, &devicev1.RegisterAgentRequest{
		Agent: &devicev1.Agent{
			Hostname: "agent-heartbeat",
			TenantId: "tenant-1",
		},
	})
	if err != nil {
		t.Fatalf("RegisterAgent failed: %v", err)
	}

	err = svc.HeartbeatAgent(ctx, &devicev1.AgentHeartbeatRequest{
		AgentId: created.Id,
		Status:  "online",
		Load:    10,
	})
	if err != nil {
		t.Fatalf("HeartbeatAgent failed: %v", err)
	}
}

// === CMDBService Tests ===

func TestCreateCI(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	req := &devicev1.CreateCIRequest{
		Ci: &devicev1.CI{
			CiType: "machine",
			Name:   "server-01",
			TenantId: "tenant-1",
		},
	}

	ci, err := svc.CreateCI(ctx, req)
	if err != nil {
		t.Fatalf("CreateCI failed: %v", err)
	}

	if ci.Id == "" {
		t.Error("expected CI ID to be set")
	}
	if ci.Version != 1 {
		t.Errorf("expected version 1, got %d", ci.Version)
	}
}

func TestGetCINotFound(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.GetCI(ctx, &devicev1.GetCIRequest{Id: "nonexistent"})
	if err != ErrCINotFound {
		t.Fatalf("expected ErrCINotFound, got: %v", err)
	}
}

func TestListCIs(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := svc.CreateCI(ctx, &devicev1.CreateCIRequest{
			Ci: &devicev1.CI{
				CiType:   "service",
				Name:     "service-list",
				TenantId: "tenant-1",
			},
		})
		if err != nil {
			t.Fatalf("CreateCI failed: %v", err)
		}
	}

	resp, err := svc.ListCIs(ctx, &devicev1.ListCIsRequest{CiType: "service"})
	if err != nil {
		t.Fatalf("ListCIs failed: %v", err)
	}

	if len(resp.Cis) != 3 {
		t.Errorf("expected 3 CIs, got %d", len(resp.Cis))
	}
}

func TestCreateCIRelation(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	ci1, err := svc.CreateCI(ctx, &devicev1.CreateCIRequest{
		Ci: &devicev1.CI{CiType: "machine", Name: "host-1", TenantId: "tenant-1"},
	})
	if err != nil {
		t.Fatalf("CreateCI failed: %v", err)
	}

	ci2, err := svc.CreateCI(ctx, &devicev1.CreateCIRequest{
		Ci: &devicev1.CI{CiType: "service", Name: "svc-1", TenantId: "tenant-1"},
	})
	if err != nil {
		t.Fatalf("CreateCI failed: %v", err)
	}

	rel, err := svc.CreateCIRelation(ctx, &devicev1.CreateCIRelationRequest{
		Relation: &devicev1.CIRelation{
			SourceCiId:   ci1.Id,
			TargetCiId:   ci2.Id,
			RelationType: "runs_on",
			TenantId:     "tenant-1",
		},
	})
	if err != nil {
		t.Fatalf("CreateCIRelation failed: %v", err)
	}

	if rel.SourceCiId != ci1.Id {
		t.Errorf("expected source CI %s, got %s", ci1.Id, rel.SourceCiId)
	}

	rels, err := svc.GetCIRelations(ctx, &devicev1.GetCIRelationsRequest{CiId: ci1.Id})
	if err != nil {
		t.Fatalf("GetCIRelations failed: %v", err)
	}

	if len(rels.Relations) != 1 {
		t.Errorf("expected 1 relation, got %d", len(rels.Relations))
	}
}

// === DiscoveryService Tests ===

func TestStartDiscovery(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	job, err := svc.StartDiscovery(ctx, &devicev1.StartDiscoveryRequest{
		Cidr:     "192.168.1.0/24",
		TenantId: "tenant-1",
	})
	if err != nil {
		t.Fatalf("StartDiscovery failed: %v", err)
	}

	if job.Id == "" {
		t.Error("expected job ID to be set")
	}
	if job.Status != "completed" {
		t.Errorf("expected status completed, got %s", job.Status)
	}
	if job.FoundDevices != 3 {
		t.Errorf("expected 3 found devices, got %d", job.FoundDevices)
	}
}

func TestGetDiscoveryStatus(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	job, err := svc.StartDiscovery(ctx, &devicev1.StartDiscoveryRequest{
		Cidr:     "10.0.0.0/24",
		TenantId: "tenant-1",
	})
	if err != nil {
		t.Fatalf("StartDiscovery failed: %v", err)
	}

	got, err := svc.GetDiscoveryStatus(ctx, &devicev1.GetDiscoveryStatusRequest{JobId: job.Id})
	if err != nil {
		t.Fatalf("GetDiscoveryStatus failed: %v", err)
	}

	if got.Id != job.Id {
		t.Errorf("expected job ID %s, got %s", job.Id, got.Id)
	}
}

func TestDeleteCINotFound(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	err := svc.DeleteCI(ctx, &devicev1.DeleteCIRequest{Id: "nonexistent"})
	if err != ErrCINotFound {
		t.Fatalf("expected ErrCINotFound, got: %v", err)
	}
}
