// bench_m4_test.go M4-5 性能基准测试：关键 API Handler 性能基准。
//
// 覆盖：
//   - BenchmarkHandleListDevices    GET /api/v1/devices 性能
//   - BenchmarkHandleCreateTask     POST /api/v1/tasks 性能
//   - BenchmarkHandleDeviceMetrics  GET /api/v1/devices/{id}/metrics 性能
//   - BenchmarkHandleAlertRules     GET /api/v1/alert-rules 性能
//
// 设计要点：
//   - 用 testing.B + httptest 构造请求，避免真实 TCP 监听开销；
//   - 用 b.ResetTimer() 排除初始化时间；
//   - 用 b.RunParallel 测并发吞吐；
//   - Server 构造与 server_test.go 一致：requireAuth=false + Demo=true 放行 RBAC。
package controlplane

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"opsmesh/internal/config"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// newBenchServer 构造用于基准测试的 Server（requireAuth=false + Demo=true 放行 RBAC）。
// 与 server_test.go 中构造方式一致。
func newBenchServer() (*Server, *store.MemoryStore) {
	st := store.NewMemoryStore()
	s := &Server{store: st, requireAuth: false, cfg: &config.Config{TaskMaxRetries: 3, Demo: true}}
	return s, st
}

// seedDevices 预填充 n 个设备到 store，返回设备 ID 列表。
func seedDevices(st *store.MemoryStore, n int) []string {
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("dev-bench-%d", i)
		st.UpsertDevice(&proto.DeviceInfo{
			DeviceID:  id,
			Segment:   "seg-a",
			TenantID:  "t1",
			IP:        fmt.Sprintf("10.30.0.%d", i%250+1),
			AgentID:   fmt.Sprintf("agent-bench-%d", i),
			State:     "online",
			TaskState: "idle",
			Managed:   true,
			Hostname:  fmt.Sprintf("host-%d", i),
			OS:        "linux",
			Arch:      "amd64",
		})
		ids = append(ids, id)
	}
	return ids
}

// BenchmarkHandleListDevices GET /api/v1/devices 性能。
// 不同设备规模下全量查询的吞吐。
func BenchmarkHandleListDevices(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("devices=%d", n), func(b *testing.B) {
			s, st := newBenchServer()
			seedDevices(st, n)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
				req.Header.Set("X-Tenant-ID", "t1")
				rec := httptest.NewRecorder()
				s.handleDevices(rec, req)
				if rec.Code != http.StatusOK {
					b.Fatalf("status = %d, want 200", rec.Code)
				}
			}
		})
	}
}

// BenchmarkHandleListDevices_Concurrent 并发 GET /api/v1/devices 性能。
// 模拟多前端客户端并发拉取设备列表。
func BenchmarkHandleListDevices_Concurrent(b *testing.B) {
	s, st := newBenchServer()
	seedDevices(st, 10000)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
			req.Header.Set("X-Tenant-ID", "t1")
			rec := httptest.NewRecorder()
			s.handleDevices(rec, req)
		}
	})
}

// BenchmarkHandleCreateTask POST /api/v1/tasks 性能。
// 单线程下创建任务的吞吐（含 JSON 解码 + store.CreateTask + 审计 + SSE 广播）。
func BenchmarkHandleCreateTask(b *testing.B) {
	s, st := newBenchServer()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	body, _ := json.Marshal(map[string]string{
		"agentID":  a.AgentID,
		"command":  "echo bench",
		"tenantID": "t1",
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", bytes.NewReader(body))
		req.Header.Set("X-Tenant-ID", "t1")
		req.Header.Set("X-User-Id", "u1")
		rec := httptest.NewRecorder()
		s.handleCreateTask(rec, req)
		if rec.Code != http.StatusCreated {
			b.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
		}
	}
}

// BenchmarkHandleCreateTask_Concurrent 并发 POST /api/v1/tasks 性能。
// 模拟多控制面副本/多 API 客户端并发下发任务。
func BenchmarkHandleCreateTask_Concurrent(b *testing.B) {
	s, st := newBenchServer()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	body, _ := json.Marshal(map[string]string{
		"agentID":  a.AgentID,
		"command":  "echo bench",
		"tenantID": "t1",
	})
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", bytes.NewReader(body))
			req.Header.Set("X-Tenant-ID", "t1")
			req.Header.Set("X-User-Id", "u1")
			rec := httptest.NewRecorder()
			s.handleCreateTask(rec, req)
		}
	})
}

// BenchmarkHandleDeviceMetrics GET /api/v1/devices/{id}/metrics 性能。
// 单设备最新指标查询吞吐。
func BenchmarkHandleDeviceMetrics(b *testing.B) {
	s, st := newBenchServer()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1", Hostname: "web-01"})
	deviceID := "dev-" + a.AgentID
	// 注入指标
	st.StoreDeviceMetrics(deviceID, &proto.DeviceMetrics{
		DeviceID: deviceID,
		CPU:      proto.CPUMetrics{Cores: 4, Usage: 50.0},
		Memory:   proto.MemMetrics{Total: 8192, Used: 4096},
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/devices/"+deviceID+"/metrics", nil)
		req.Header.Set("X-Tenant-ID", "t1")
		rec := httptest.NewRecorder()
		s.handleDeviceRouting(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
	}
}

// BenchmarkHandleDeviceMetrics_Range GET /api/v1/devices/{id}/metrics?range=2h 性能。
// 历史时序查询吞吐（环形缓冲遍历 + JSON 序列化）。
func BenchmarkHandleDeviceMetrics_Range(b *testing.B) {
	s, st := newBenchServer()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1", Hostname: "web-01"})
	deviceID := "dev-" + a.AgentID
	// 注入 100 条历史指标
	base := time.Now().Add(-2 * time.Hour)
	for i := 0; i < 100; i++ {
		st.StoreDeviceMetrics(deviceID, &proto.DeviceMetrics{
			DeviceID:    deviceID,
			CPU:         proto.CPUMetrics{Cores: 4, Usage: float64(i)},
			CollectedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/devices/"+deviceID+"/metrics?range=2h", nil)
		req.Header.Set("X-Tenant-ID", "t1")
		rec := httptest.NewRecorder()
		s.handleDeviceRouting(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
	}
}

// BenchmarkHandleDeviceMetrics_Concurrent 并发 GET /api/v1/devices/{id}/metrics 性能。
// 模拟多前端面板并发拉取设备指标。
func BenchmarkHandleDeviceMetrics_Concurrent(b *testing.B) {
	s, st := newBenchServer()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1", Hostname: "web-01"})
	deviceID := "dev-" + a.AgentID
	st.StoreDeviceMetrics(deviceID, &proto.DeviceMetrics{
		DeviceID: deviceID,
		CPU:      proto.CPUMetrics{Cores: 4, Usage: 50.0},
	})
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/devices/"+deviceID+"/metrics", nil)
			req.Header.Set("X-Tenant-ID", "t1")
			rec := httptest.NewRecorder()
			s.handleDeviceRouting(rec, req)
		}
	})
}

// BenchmarkHandleAlertRules GET /api/v1/alert-rules 性能。
// 不同规则规模下列表查询吞吐。
func BenchmarkHandleAlertRules(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("rules=%d", n), func(b *testing.B) {
			s, _ := newBenchServer()
			// 通过 POST 预创建 n 条规则
			for i := 0; i < n; i++ {
				body, _ := json.Marshal(map[string]interface{}{
					"metric":    "cpu.usage",
					"op":        ">",
					"threshold": 80.0,
					"severity":  "warning",
					"message":   "cpu high",
					"enabled":   true,
				})
				req := httptest.NewRequest(http.MethodPost, "/api/v1/alert-rules", bytes.NewReader(body))
				req.Header.Set("X-Tenant-ID", "t1")
				req.Header.Set("X-User-Id", "u1")
				rec := httptest.NewRecorder()
				s.handleAlertRules(rec, req)
				if rec.Code != http.StatusCreated {
					b.Fatalf("seed create = %d, want 201; body=%s", rec.Code, rec.Body.String())
				}
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/alert-rules", nil)
				req.Header.Set("X-Tenant-ID", "t1")
				rec := httptest.NewRecorder()
				s.handleAlertRules(rec, req)
				if rec.Code != http.StatusOK {
					b.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
				}
			}
		})
	}
}

// BenchmarkHandleAlertRules_Concurrent 并发 GET /api/v1/alert-rules 性能。
func BenchmarkHandleAlertRules_Concurrent(b *testing.B) {
	s, _ := newBenchServer()
	// 预创建 100 条规则
	for i := 0; i < 100; i++ {
		body, _ := json.Marshal(map[string]interface{}{
			"metric":    "cpu.usage",
			"op":        ">",
			"threshold": 80.0,
			"severity":  "warning",
			"message":   "cpu high",
			"enabled":   true,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/alert-rules", bytes.NewReader(body))
		req.Header.Set("X-Tenant-ID", "t1")
		req.Header.Set("X-User-Id", "u1")
		rec := httptest.NewRecorder()
		s.handleAlertRules(rec, req)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/alert-rules", nil)
			req.Header.Set("X-Tenant-ID", "t1")
			rec := httptest.NewRecorder()
			s.handleAlertRules(rec, req)
		}
	})
}
