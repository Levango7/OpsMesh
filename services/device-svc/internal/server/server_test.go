package server

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Levango7/OpsMesh/pkg/tenant"
	devicev1 "github.com/Levango7/OpsMesh/services/device-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/device-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/device-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/device-svc/internal/store"
)

type testEnv struct {
	srv *Server
	svc *service.Service
	mem *store.MemoryStore
}

// newTestEnv 按生产装配方式构造：同一个 MemoryStore 兼作四个 store，
// tenant.Manager 与 main.go 一样以 nil 的 quota/usage store 构造（默认配额来自内置表）。
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	mem := store.NewMemoryStore()
	svc := service.NewService(mem, mem, mem, mem, mem, tenant.NewManager(nil, nil, nil))
	return &testEnv{srv: NewServer(svc), svc: svc, mem: mem}
}

func requireCode(t *testing.T, err error, want codes.Code, what string) {
	t.Helper()
	if got := status.Code(err); got != want {
		t.Fatalf("%s: code = %v, want %v (err=%v)", what, got, want, err)
	}
}

func TestDeviceHandlersMapping(t *testing.T) {
	env := newTestEnv(t)
	srv, ctx := env.srv, context.Background()

	_, err := srv.RegisterDevice(ctx, &devicev1.RegisterDeviceRequest{})
	requireCode(t, err, codes.InvalidArgument, "RegisterDevice(nil device)")

	dev, err := srv.RegisterDevice(ctx, &devicev1.RegisterDeviceRequest{Device: &devicev1.Device{
		TenantId: "t1", Name: "web-1", Ip: "10.0.0.5",
	}})
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	if dev.GetId() == "" || dev.GetStatus() != "online" {
		t.Fatalf("RegisterDevice = id=%q status=%q, want 非空 id 与 online", dev.GetId(), dev.GetStatus())
	}

	// 心跳成功后再查状态：状态随心跳写入。
	if _, err := srv.Heartbeat(ctx, &devicev1.HeartbeatRequest{DeviceId: dev.GetId(), Status: "offline"}); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	st, err := srv.GetDeviceStatus(ctx, &devicev1.GetDeviceStatusRequest{DeviceId: dev.GetId()})
	if err != nil {
		t.Fatalf("GetDeviceStatus: %v", err)
	}
	if st.GetStatus() != "offline" || st.GetReachable() {
		t.Fatalf("GetDeviceStatus = %+v, want offline/不可达", st)
	}

	// 未知设备：心跳与服务层重试 3 次后仍应映射成 NotFound。
	_, err = srv.Heartbeat(ctx, &devicev1.HeartbeatRequest{DeviceId: "missing"})
	requireCode(t, err, codes.NotFound, "Heartbeat(missing)")
	_, err = srv.GetDeviceStatus(ctx, &devicev1.GetDeviceStatusRequest{DeviceId: "missing"})
	requireCode(t, err, codes.NotFound, "GetDeviceStatus(missing)")
	_, err = srv.GetDevice(ctx, &devicev1.GetDeviceRequest{Id: "missing"})
	requireCode(t, err, codes.NotFound, "GetDevice(missing)")

	// 列表：空结果必须是 []（JSON 契约），且租户过滤生效。
	resp, err := srv.ListDevices(ctx, &devicev1.ListDevicesRequest{TenantId: "t2"})
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	if resp.GetDevices() == nil || len(resp.GetDevices()) != 0 {
		t.Fatalf("ListDevices(t2) = %v, want 空非 nil 切片", resp.GetDevices())
	}
	resp, err = srv.ListDevices(ctx, &devicev1.ListDevicesRequest{TenantId: "t1"})
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	if len(resp.GetDevices()) != 1 {
		t.Fatalf("ListDevices(t1) = %d 条, want 1", len(resp.GetDevices()))
	}

	_, err = srv.UpdateDevice(ctx, &devicev1.UpdateDeviceRequest{})
	requireCode(t, err, codes.InvalidArgument, "UpdateDevice(nil device)")
	_, err = srv.UpdateDevice(ctx, &devicev1.UpdateDeviceRequest{Device: &devicev1.Device{Id: "missing"}})
	requireCode(t, err, codes.NotFound, "UpdateDevice(missing)")

	updated, err := srv.UpdateDevice(ctx, &devicev1.UpdateDeviceRequest{Device: &devicev1.Device{
		Id: dev.GetId(), TenantId: "t1", Name: "web-1-renamed", Ip: "10.0.0.5", Status: "online",
	}})
	if err != nil {
		t.Fatalf("UpdateDevice: %v", err)
	}
	if updated.GetName() != "web-1-renamed" {
		t.Fatalf("UpdateDevice name = %q", updated.GetName())
	}

	_, err = srv.DeleteDevice(ctx, &devicev1.DeleteDeviceRequest{Id: "missing"})
	requireCode(t, err, codes.NotFound, "DeleteDevice(missing)")
	if _, err := srv.DeleteDevice(ctx, &devicev1.DeleteDeviceRequest{Id: dev.GetId()}); err != nil {
		t.Fatalf("DeleteDevice: %v", err)
	}
	// MemoryStore 的删除是软删除（Retired=true，与 controlplane RetireDevice 对齐），
	// 可观测契约是"列表里不再出现"，而不是 GetDevice 变 404。
	resp, err = srv.ListDevices(ctx, &devicev1.ListDevicesRequest{TenantId: "t1"})
	if err != nil {
		t.Fatalf("ListDevices(删除后): %v", err)
	}
	if len(resp.GetDevices()) != 0 {
		t.Fatalf("ListDevices(删除后) = %d 条, want 0（软删除应从列表消失）", len(resp.GetDevices()))
	}
}

func TestAgentHandlersMapping(t *testing.T) {
	env := newTestEnv(t)
	srv, ctx := env.srv, context.Background()

	_, err := srv.RegisterAgent(ctx, &devicev1.RegisterAgentRequest{})
	requireCode(t, err, codes.InvalidArgument, "RegisterAgent(nil agent)")

	agent, err := srv.RegisterAgent(ctx, &devicev1.RegisterAgentRequest{Agent: &devicev1.Agent{
		TenantId: "t1", Hostname: "host-1",
	}})
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	if agent.GetId() == "" || agent.GetStatus() != "online" {
		t.Fatalf("RegisterAgent = id=%q status=%q, want 非空 id 与 online", agent.GetId(), agent.GetStatus())
	}

	_, err = srv.GetAgent(ctx, &devicev1.GetAgentRequest{Id: "missing"})
	requireCode(t, err, codes.NotFound, "GetAgent(missing)")
	_, err = srv.UpdateAgentStatus(ctx, &devicev1.UpdateAgentStatusRequest{AgentId: "missing"})
	requireCode(t, err, codes.NotFound, "UpdateAgentStatus(missing)")
	_, err = srv.AgentHeartbeat(ctx, &devicev1.AgentHeartbeatRequest{AgentId: "missing"})
	requireCode(t, err, codes.NotFound, "AgentHeartbeat(missing)")

	got, err := srv.UpdateAgentStatus(ctx, &devicev1.UpdateAgentStatusRequest{
		AgentId: agent.GetId(), Status: "stale", Load: 7,
	})
	if err != nil {
		t.Fatalf("UpdateAgentStatus: %v", err)
	}
	if got.GetStatus() != "stale" || got.GetLoad() != 7 {
		t.Fatalf("UpdateAgentStatus = status=%q load=%d, want stale/7", got.GetStatus(), got.GetLoad())
	}
	if _, err := srv.AgentHeartbeat(ctx, &devicev1.AgentHeartbeatRequest{
		AgentId: agent.GetId(), Status: "online", Load: 2,
	}); err != nil {
		t.Fatalf("AgentHeartbeat: %v", err)
	}

	list, err := srv.ListAgents(ctx, &devicev1.ListAgentsRequest{TenantId: "t2"})
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if list.GetAgents() == nil || len(list.GetAgents()) != 0 {
		t.Fatalf("ListAgents(t2) = %v, want 空非 nil 切片", list.GetAgents())
	}
	list, err = srv.ListAgents(ctx, &devicev1.ListAgentsRequest{TenantId: "t1", Status: "online"})
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(list.GetAgents()) != 1 {
		t.Fatalf("ListAgents(t1,online) = %d 条, want 1", len(list.GetAgents()))
	}
}

func TestCMDBHandlersMapping(t *testing.T) {
	env := newTestEnv(t)
	srv, ctx := env.srv, context.Background()

	_, err := srv.CreateCI(ctx, &devicev1.CreateCIRequest{})
	requireCode(t, err, codes.InvalidArgument, "CreateCI(nil ci)")

	ci, err := srv.CreateCI(ctx, &devicev1.CreateCIRequest{Ci: &devicev1.CI{
		TenantId: "t1", CiType: "server", Name: "web-1",
	}})
	if err != nil {
		t.Fatalf("CreateCI: %v", err)
	}
	if ci.GetId() == "" || ci.GetStatus() != "active" || ci.GetVersion() != 1 {
		t.Fatalf("CreateCI = %+v, want 非空 id/active/version=1", ci)
	}

	_, err = srv.GetCI(ctx, &devicev1.GetCIRequest{Id: "missing"})
	requireCode(t, err, codes.NotFound, "GetCI(missing)")
	_, err = srv.UpdateCI(ctx, &devicev1.UpdateCIRequest{})
	requireCode(t, err, codes.InvalidArgument, "UpdateCI(nil ci)")
	_, err = srv.UpdateCI(ctx, &devicev1.UpdateCIRequest{Ci: &devicev1.CI{Id: "missing"}})
	requireCode(t, err, codes.NotFound, "UpdateCI(missing)")
	_, err = srv.DeleteCI(ctx, &devicev1.DeleteCIRequest{Id: "missing"})
	requireCode(t, err, codes.NotFound, "DeleteCI(missing)")

	updated, err := srv.UpdateCI(ctx, &devicev1.UpdateCIRequest{Ci: &devicev1.CI{
		Id: ci.GetId(), TenantId: "t1", CiType: "server", Name: "web-1-renamed", Status: "active",
	}})
	if err != nil {
		t.Fatalf("UpdateCI: %v", err)
	}
	if updated.GetName() != "web-1-renamed" || updated.GetVersion() != 2 {
		t.Fatalf("UpdateCI = name=%q version=%d, want web-1-renamed/2", updated.GetName(), updated.GetVersion())
	}

	list, err := srv.ListCIs(ctx, &devicev1.ListCIsRequest{TenantId: "t1", CiType: "server"})
	if err != nil {
		t.Fatalf("ListCIs: %v", err)
	}
	if len(list.GetCis()) != 1 {
		t.Fatalf("ListCIs(t1,server) = %d 条, want 1", len(list.GetCis()))
	}

	_, err = srv.CreateCIRelation(ctx, &devicev1.CreateCIRelationRequest{})
	requireCode(t, err, codes.InvalidArgument, "CreateCIRelation(nil relation)")

	rel, err := srv.CreateCIRelation(ctx, &devicev1.CreateCIRelationRequest{Relation: &devicev1.CIRelation{
		SourceCiId: ci.GetId(), TargetCiId: "ci-target", RelationType: "depends_on", TenantId: "t1",
	}})
	if err != nil {
		t.Fatalf("CreateCIRelation: %v", err)
	}
	if rel.GetId() == 0 {
		t.Fatal("CreateCIRelation 响应未回出 store 分配的自增 ID")
	}
	rels, err := srv.GetCIRelations(ctx, &devicev1.GetCIRelationsRequest{CiId: ci.GetId()})
	if err != nil {
		t.Fatalf("GetCIRelations: %v", err)
	}
	if len(rels.GetRelations()) != 1 || rels.GetRelations()[0].GetRelationType() != "depends_on" {
		t.Fatalf("GetCIRelations = %+v, want 单条 depends_on", rels.GetRelations())
	}
	if rels.GetRelations()[0].GetId() == 0 {
		t.Fatal("持久化的关系未分配自增 ID")
	}

	if _, err := srv.DeleteCI(ctx, &devicev1.DeleteCIRequest{Id: ci.GetId()}); err != nil {
		t.Fatalf("DeleteCI: %v", err)
	}
}

func TestDiscoveryHandlersMapping(t *testing.T) {
	env := newTestEnv(t)
	srv, ctx := env.srv, context.Background()

	// 空 CIDR / 白名单越界都在创建 job 前被拒，不会启动后台扫描 goroutine。
	// 白名单拒绝错误经 fmt.Errorf("%w") 包装，适配层必须用 errors.Is 才映射成 InvalidArgument。
	_, err := srv.StartDiscovery(ctx, &devicev1.StartDiscoveryRequest{})
	requireCode(t, err, codes.InvalidArgument, "StartDiscovery(空 cidr)")

	env.svc.SetDiscoverConfig(&service.DiscoverConfig{CIDRWhitelist: "192.0.2.0/24"})
	_, err = srv.StartDiscovery(ctx, &devicev1.StartDiscoveryRequest{Cidr: "203.0.113.0/24", TenantId: "t1"})
	requireCode(t, err, codes.InvalidArgument, "StartDiscovery(白名单外 CIDR)")

	_, err = srv.GetDiscoveryStatus(ctx, &devicev1.GetDiscoveryStatusRequest{JobId: "missing"})
	requireCode(t, err, codes.NotFound, "GetDiscoveryStatus(missing)")

	// 直接向 store 落一条终态 job，验证 job → proto 的字段映射（不触发真实扫描）。
	started := time.Now().Add(-time.Minute)
	env.mem.CreateJob(&models.DiscoveryJob{
		ID: "job-1", TenantID: "t1", CIDR: "192.0.2.0/24", Status: "completed",
		TotalHosts: 256, ScannedHosts: 2, FoundDevices: 2,
		StartedAt: started, CompletedAt: started.Add(time.Second),
	})
	job, err := srv.GetDiscoveryStatus(ctx, &devicev1.GetDiscoveryStatusRequest{JobId: "job-1"})
	if err != nil {
		t.Fatalf("GetDiscoveryStatus: %v", err)
	}
	if job.GetStatus() != "completed" || job.GetFoundDevices() != 2 || job.GetTotalHosts() != 256 {
		t.Fatalf("GetDiscoveryStatus = %+v, want completed/found=2/total=256", job)
	}
	if job.GetStartedAt() == nil || job.GetCompletedAt() == nil {
		t.Fatalf("GetDiscoveryStatus 时间戳缺失: %+v", job)
	}

	// ListDiscoveredDevices 只回 discovered 状态的设备，并按租户过滤。
	env.mem.RegisterDevice(&models.Device{ID: "dev-a", TenantID: "t1", Status: "discovered"})
	env.mem.RegisterDevice(&models.Device{ID: "dev-b", TenantID: "t1", Status: "online"})
	env.mem.RegisterDevice(&models.Device{ID: "dev-c", TenantID: "t2", Status: "discovered"})

	list, err := srv.ListDiscoveredDevices(ctx, &devicev1.ListDiscoveredDevicesRequest{TenantId: "t1"})
	if err != nil {
		t.Fatalf("ListDiscoveredDevices: %v", err)
	}
	if len(list.GetDevices()) != 1 || list.GetDevices()[0].GetId() != "dev-a" {
		t.Fatalf("ListDiscoveredDevices(t1) = %+v, want 仅 dev-a", list.GetDevices())
	}
	empty, err := srv.ListDiscoveredDevices(ctx, &devicev1.ListDiscoveredDevicesRequest{TenantId: "t3"})
	if err != nil {
		t.Fatalf("ListDiscoveredDevices: %v", err)
	}
	if empty.GetDevices() == nil || len(empty.GetDevices()) != 0 {
		t.Fatalf("ListDiscoveredDevices(t3) = %v, want 空非 nil 切片", empty.GetDevices())
	}
}
