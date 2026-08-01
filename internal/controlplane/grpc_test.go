package controlplane

import (
	"context"
	"net"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"opsmesh/internal/grpcx"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// TestGRPCRegistrationLoop 是内核的端到端护栏：进程内起真实 gRPC server（9090，JSON codec，
// 无需 protoc），用 ForceCodec 客户端跑通 注册→拉任务→上报结果→心跳 全闭环，
// 并断言「服务端按网关注入租户盖章、租户隔离生效」。
func TestGRPCRegistrationLoop(t *testing.T) {
	st := store.NewMemoryStore().WithDemo(true)
	srvImpl := &grpcServerImpl{store: st, requireAuth: false}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	gs := grpc.NewServer()
	gs.RegisterService(&grpcx.Registration_ServiceDesc, srvImpl)
	go func() { _ = gs.Serve(lis) }()
	defer gs.Stop()

	port := lis.Addr().(*net.TCPAddr).Port
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(grpcx.JSONCodec)),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// 注册时携带网关注入的租户 t1（agent 自身不应能伪造租户）
	rctx := metadata.AppendToOutgoingContext(ctx, "x-tenant-id", "t1")
	regResp := &grpcx.RegisterResp{}
	if err := conn.Invoke(rctx, "/opsmesh.v1.Registration/Register",
		&proto.AgentInfo{Segment: "seg-a"}, regResp, grpc.ForceCodec(grpcx.JSONCodec)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if regResp.AgentID == "" {
		t.Fatal("expected non-empty agentID")
	}

	// 服务端按网关租户盖章：t1 看到 1 个，且 TenantID 被强制为 t1；t2 看到 0 个（隔离生效）
	if got := st.Agents("t1"); len(got) != 1 || got[0].TenantID != "t1" {
		t.Fatalf("server-side tenant stamping failed: %+v", got)
	}
	if got := st.Agents("t2"); len(got) != 0 {
		t.Fatalf("tenant isolation broken, t2 sees %d agents", len(got))
	}

	// 拉任务：注册后预置 1 条 shell 任务
	ptResp := &grpcx.PullTasksResp{}
	if err := conn.Invoke(rctx, "/opsmesh.v1.Registration/PullTasks",
		&grpcx.PullTasksReq{AgentID: regResp.AgentID}, ptResp, grpc.ForceCodec(grpcx.JSONCodec)); err != nil {
		t.Fatalf("PullTasks: %v", err)
	}
	if len(ptResp.Tasks) != 1 {
		t.Fatalf("PullTasks returned %d, want 1", len(ptResp.Tasks))
	}

	// 二次拉取应返回 0：同一任务已被原子领取，不会双发（P1-1 HA 协调）。
	ptResp2 := &grpcx.PullTasksResp{}
	if err := conn.Invoke(rctx, "/opsmesh.v1.Registration/PullTasks",
		&grpcx.PullTasksReq{AgentID: regResp.AgentID}, ptResp2, grpc.ForceCodec(grpcx.JSONCodec)); err != nil {
		t.Fatalf("PullTasks#2: %v", err)
	}
	if len(ptResp2.Tasks) != 0 {
		t.Fatalf("PullTasks#2 returned %d, want 0 (no double-claim)", len(ptResp2.Tasks))
	}

	// 上报结果：设备 TaskState 应变为 done
	if err := conn.Invoke(rctx, "/opsmesh.v1.Registration/ReportResult",
		&proto.TaskResult{TaskID: ptResp.Tasks[0].TaskID, AgentID: regResp.AgentID, ExitCode: 0},
		&grpcx.Empty{}, grpc.ForceCodec(grpcx.JSONCodec)); err != nil {
		t.Fatalf("ReportResult: %v", err)
	}
	found := false
	for _, ds := range st.Snapshot("t1") {
		for _, d := range ds {
			if d.AgentID == regResp.AgentID {
				found = true
				if d.TaskState != "done" {
					t.Fatalf("device TaskState = %q, want done", d.TaskState)
				}
			}
		}
	}
	if !found {
		t.Fatal("registered device not found in snapshot")
	}

	// 心跳：负载应更新为 7
	if err := conn.Invoke(rctx, "/opsmesh.v1.Registration/Heartbeat",
		&grpcx.HeartbeatReq{AgentID: regResp.AgentID, Status: "online", Load: 7},
		&grpcx.Empty{}, grpc.ForceCodec(grpcx.JSONCodec)); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if got := st.Agents("t1"); got[0].Load != 7 {
		t.Fatalf("Load = %d, want 7", got[0].Load)
	}
}

// TestGRPCCancelReachesWorker F3 端到端护栏：控制面 CancelTask 经 PollCancels
// 把取消信号真正下达到 agent worker——正在执行的「长命令」须被立即中止，
// 且任务状态保持 cancelled（worker 不回写 store，避免误翻 done/死信）。
func TestGRPCCancelReachesWorker(t *testing.T) {
	st := store.NewMemoryStore()
	srvImpl := &grpcServerImpl{store: st, requireAuth: false}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	gs := grpc.NewServer()
	gs.RegisterService(&grpcx.Registration_ServiceDesc, srvImpl)
	go func() { _ = gs.Serve(lis) }()
	defer gs.Stop()

	port := lis.Addr().(*net.TCPAddr).Port
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(grpcx.JSONCodec)),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// 注册 agent（拿到 agentID）
	regResp := &grpcx.RegisterResp{}
	if err := conn.Invoke(ctx, "/opsmesh.v1.Registration/Register",
		&proto.AgentInfo{Segment: "seg-a"}, regResp, grpc.ForceCodec(grpcx.JSONCodec)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	agentID := regResp.AgentID

	// 注入一个长任务（sleep 5s）：取消信号若真正到达，应在远小于 5s 内中止
	longTask := st.CreateTask(&proto.Task{
		AgentID: agentID, TenantID: "t1", Type: "shell", Command: "sleep 5",
	})

	// 模拟 agent worker：领取 -> 以可被取消的 ctx 执行长命令 -> 轮询 PollCancels
	type result struct {
		aborted bool
		dur     time.Duration
	}
	resCh := make(chan result, 1)
	go func() {
		ptResp := &grpcx.PullTasksResp{}
		if err := conn.Invoke(ctx, "/opsmesh.v1.Registration/PullTasks",
			&grpcx.PullTasksReq{AgentID: agentID}, ptResp, grpc.ForceCodec(grpcx.JSONCodec)); err != nil {
			t.Errorf("PullTasks: %v", err)
			return
		}
		if len(ptResp.Tasks) != 1 {
			t.Errorf("PullTasks=%d want 1", len(ptResp.Tasks))
			return
		}
		taskID := ptResp.Tasks[0].TaskID
		taskCtx, taskCancel := context.WithCancel(ctx)
		defer taskCancel()

		start := time.Now()
		cmd := exec.CommandContext(taskCtx, "sh", "-c", "sleep 5")
		poll := make(chan struct{})
		go func() {
			tk := time.NewTicker(50 * time.Millisecond)
			defer tk.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-poll:
					return
				case <-tk.C:
					pc := &grpcx.PollCancelsResp{}
					if err := conn.Invoke(ctx, "/opsmesh.v1.Registration/PollCancels",
						&grpcx.PollCancelsReq{AgentID: agentID}, pc, grpc.ForceCodec(grpcx.JSONCodec)); err != nil {
						continue
					}
					for _, id := range pc.CancelledTaskIDs {
						if id == taskID {
							taskCancel() // 命中取消：中止本地执行（与真实 worker 行为一致）
						}
					}
				}
			}
		}()
		_ = cmd.Run()
		close(poll)
		dur := time.Since(start)
		resCh <- result{aborted: dur < 4*time.Second, dur: dur}
	}()

	// 等 worker 真正开始执行长命令后，由控制面下发取消
	time.Sleep(300 * time.Millisecond)
	briefCtx, briefCancel := context.WithTimeout(ctx, 3*time.Second)
	defer briefCancel()
	if err := conn.Invoke(briefCtx, "/opsmesh.v1.Registration/CancelTask",
		&grpcx.CancelTaskReq{TaskID: longTask.TaskID}, &grpcx.Empty{}, grpc.ForceCodec(grpcx.JSONCodec)); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}

	select {
	case r := <-resCh:
		if !r.aborted {
			t.Fatalf("长命令未被取消信号中止（耗时 %v，应远小于 5s）", r.dur)
		}
	case <-ctx.Done():
		t.Fatal("等待 worker 结果超时（取消信号未生效？）")
	}

	// 取消后：PollCancels 应返回该任务；且任务状态保持 cancelled（未被误翻）
	pc := &grpcx.PollCancelsResp{}
	if err := conn.Invoke(ctx, "/opsmesh.v1.Registration/PollCancels",
		&grpcx.PollCancelsReq{AgentID: agentID}, pc, grpc.ForceCodec(grpcx.JSONCodec)); err != nil {
		t.Fatalf("PollCancels: %v", err)
	}
	found := false
	for _, id := range pc.CancelledTaskIDs {
		if id == longTask.TaskID {
			found = true
		}
	}
	if !found {
		t.Fatalf("PollCancels 未返回被取消任务 %s（got %v）", longTask.TaskID, pc.CancelledTaskIDs)
	}
}

// TestGRPCRegister_ConsumesToken B1 端到端：带 install token 注册，服务端消费 token 并翻转候选设备。
func TestGRPCRegister_ConsumesToken(t *testing.T) {
	st := store.NewMemoryStore().WithSecret("opsmesh-test-secret")
	srvImpl := &grpcServerImpl{store: st, requireAuth: false}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	gs := grpc.NewServer()
	gs.RegisterService(&grpcx.Registration_ServiceDesc, srvImpl)
	go func() { _ = gs.Serve(lis) }()
	defer gs.Stop()

	port := lis.Addr().(*net.TCPAddr).Port
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(grpcx.JSONCodec)),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// 创建候选设备
	st.UpsertDevice(&proto.DeviceInfo{
		DeviceID: "dev-candidate", Segment: "seg-a", TenantID: "t1",
		IP: "10.0.0.5", State: "discovered", Managed: false,
	})
	// 签发 install token（不走 gRPC，直接调 store）
	tok, _, err := st.Provision("dev-candidate", "10.0.0.5", "t1")
	if err != nil {
		t.Fatalf("Provision err = %v", err)
	}
	// 带 token 注册
	resp := &grpcx.RegisterResp{}
	if err := conn.Invoke(ctx, "/opsmesh.v1.Registration/Register",
		&proto.AgentInfo{
			AgentID: "agent-candidate", Hostname: "h1", Segment: "seg-a",
			Addr: "10.0.0.5", Status: "online",
			InstallToken: tok,
		}, resp, grpc.ForceCodec(grpcx.JSONCodec)); err != nil {
		t.Fatalf("Register with token err = %v", err)
	}
	if resp.AgentID == "" {
		t.Fatal("Register with token returned empty AgentID")
	}
	// 候选设备应已纳管
	dev := st.Device("dev-candidate")
	if dev == nil {
		t.Fatal("dev-candidate not found")
	}
	if !dev.Managed {
		t.Fatal("dev-candidate.Managed = false, want true after onboard")
	}
	if dev.State != "online" {
		t.Fatalf("dev-candidate.State = %q, want online", dev.State)
	}
	if dev.AgentID != resp.AgentID {
		t.Fatalf("dev-candidate.AgentID = %q, want %q", dev.AgentID, resp.AgentID)
	}

	// 无效 token 应被拒绝
	if err := conn.Invoke(ctx, "/opsmesh.v1.Registration/Register",
		&proto.AgentInfo{
			AgentID: "agent-fake", Hostname: "h2", Segment: "seg-a",
			InstallToken: "invalid-token",
		}, &grpcx.RegisterResp{}, grpc.ForceCodec(grpcx.JSONCodec)); err == nil {
		t.Fatal("Register with invalid token should fail")
	}
}
