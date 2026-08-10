package grpcx

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"opsmesh/internal/proto"
)

// TestJSONCodecGoldenRegister 校验 Register 请求（proto.AgentInfo）的 golden JSON 报文。
// golden 报文是 JSON codec 作为正式契约的版本化基线：任何字段名/顺序/类型变更
// 都会触发本测试失败，强制开发者显式更新 golden，从而让契约变更可被 review 感知。
func TestJSONCodecGoldenRegister(t *testing.T) {
	req := &proto.AgentInfo{
		AgentID:     "agent-001",
		Hostname:    "host-001",
		Segment:     "seg-a",
		TenantID:    "tenant-001",
		Addr:        "10.0.0.1",
		GRPCPort:    9090,
		MetricsPort: 9091,
		Status:      "online",
		Load:        0,
		// LastSeen/InstallToken/OnboardDeviceID/OS/Arch 保持零值，golden 中体现零值序列化。
	}

	out, err := JSONCodec.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// golden：__v 注入在首位，其后字段顺序由 encoding/json 按 struct 字段定义顺序输出。
	// time.Time 零值序列化为 "0001-01-01T00:00:00Z"；空串序列化为 ""。
	want := `{"__v":1,"agentID":"agent-001","hostname":"host-001","segment":"seg-a","tenantID":"tenant-001","addr":"10.0.0.1","grpcPort":9090,"metricsPort":9091,"status":"online","load":0,"lastSeen":"0001-01-01T00:00:00Z","installToken":"","onboardDeviceID":"","os":"","arch":""}`
	if string(out) != want {
		t.Fatalf("golden mismatch:\n  got  = %s\n  want = %s", out, want)
	}
}

// TestJSONCodecGoldenHeartbeat 校验 Heartbeat 请求（grpcx.HeartbeatReq）的 golden JSON 报文。
// CmdbReport/Metrics 为 nil 且带 omitempty，故不出现在报文中。
func TestJSONCodecGoldenHeartbeat(t *testing.T) {
	req := &HeartbeatReq{
		AgentID: "agent-001",
		Status:  "online",
		Load:    42,
	}

	out, err := JSONCodec.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	want := `{"__v":1,"agentID":"agent-001","status":"online","load":42}`
	if string(out) != want {
		t.Fatalf("golden mismatch:\n  got  = %s\n  want = %s", out, want)
	}
}

// TestJSONCodecVersionNegotiation 校验 JSON codec 的版本协商行为：
//   - __v=1 通过；
//   - __v=2 拒绝（ErrCodecVersionMismatch）；
//   - __v 缺失拒绝（ErrCodecVersionMissing）。
func TestJSONCodecVersionNegotiation(t *testing.T) {
	// __v=1 通过。
	var hb HeartbeatReq
	if err := JSONCodec.Unmarshal([]byte(`{"__v":1,"agentID":"a1","status":"online","load":1}`), &hb); err != nil {
		t.Fatalf("__v=1 should pass, got error: %v", err)
	}
	if hb.AgentID != "a1" || hb.Status != "online" || hb.Load != 1 {
		t.Fatalf("unmarshaled body mismatch: %+v", hb)
	}

	// __v=2 拒绝。
	var hb2 HeartbeatReq
	err := JSONCodec.Unmarshal([]byte(`{"__v":2,"agentID":"a1"}`), &hb2)
	if err == nil {
		t.Fatal("__v=2 should be rejected, got nil error")
	}
	if !errors.Is(err, ErrCodecVersionMismatch) {
		t.Fatalf("__v=2 should return ErrCodecVersionMismatch, got: %v", err)
	}

	// __v 缺失拒绝。
	var hb3 HeartbeatReq
	err = JSONCodec.Unmarshal([]byte(`{"agentID":"a1"}`), &hb3)
	if err == nil {
		t.Fatal("missing __v should be rejected, got nil error")
	}
	if !errors.Is(err, ErrCodecVersionMissing) {
		t.Fatalf("missing __v should return ErrCodecVersionMissing, got: %v", err)
	}
}

// TestJSONCodecRoundTrip 校验 Marshal→Unmarshal 往返一致性。
// 对 Register（proto.AgentInfo）和 Heartbeat（grpcx.HeartbeatReq）两类报文做往返，
// 确保注入的 __v 元字段不破坏业务字段还原。
func TestJSONCodecRoundTrip(t *testing.T) {
	t.Run("HeartbeatReq", func(t *testing.T) {
		orig := &HeartbeatReq{
			AgentID: "agent-rt",
			Status:  "online",
			Load:    7,
		}
		data, err := JSONCodec.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var back HeartbeatReq
		if err := JSONCodec.Unmarshal(data, &back); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if back.AgentID != orig.AgentID || back.Status != orig.Status || back.Load != orig.Load {
			t.Fatalf("round-trip mismatch: orig=%+v back=%+v", orig, back)
		}
	})

	t.Run("AgentInfo", func(t *testing.T) {
		orig := &proto.AgentInfo{
			AgentID:  "agent-rt",
			Hostname: "host-rt",
			Segment:  "seg-rt",
			TenantID: "tenant-rt",
			Addr:     "10.1.2.3",
			GRPCPort: 9090,
			Status:   "online",
			Load:     5,
		}
		data, err := JSONCodec.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var back proto.AgentInfo
		if err := JSONCodec.Unmarshal(data, &back); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if back.AgentID != orig.AgentID || back.Hostname != orig.Hostname || back.Segment != orig.Segment ||
			back.TenantID != orig.TenantID || back.Addr != orig.Addr || back.GRPCPort != orig.GRPCPort ||
			back.Status != orig.Status || back.Load != orig.Load {
			t.Fatalf("round-trip mismatch: orig=%+v back=%+v", orig, back)
		}
	})

	t.Run("EmptyObject", func(t *testing.T) {
		// 空对象 {} 经 Marshal 注入 __v 后应能 Unmarshal 回空结构体。
		orig := &Empty{}
		data, err := JSONCodec.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if !bytes.Contains(data, []byte(`"__v":1`)) {
			t.Fatalf("Empty object should contain __v field, got: %s", data)
		}
		var back Empty
		if err := JSONCodec.Unmarshal(data, &back); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
	})

	t.Run("NilPointer", func(t *testing.T) {
		// nil 指针 Marshal 产出 "null" → 注入为 {"__v":1}，Unmarshal 到 Empty 应成功。
		data, err := JSONCodec.Marshal((*HeartbeatReq)(nil))
		if err != nil {
			t.Fatalf("Marshal nil: %v", err)
		}
		if string(data) != `{"__v":1}` {
			t.Fatalf("nil pointer should marshal to {\"__v\":1}, got: %s", data)
		}
	})
}

// TestJSONCodecVersionFieldFirst 确保 __v 是注入后的第一个字段（golden 比对依赖此顺序）。
func TestJSONCodecVersionFieldFirst(t *testing.T) {
	req := &HeartbeatReq{AgentID: "a", Status: "s", Load: 1}
	data, err := JSONCodec.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(data)
	if !strings.HasPrefix(s, `{"__v":1,`) {
		t.Fatalf("__v should be the first field, got: %s", s)
	}
}
