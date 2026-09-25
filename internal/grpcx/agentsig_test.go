package grpcx

import (
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// TestPayloadDigest_RoundTripStable 是本套签名方案的地基：
// agent 对「发送对象」算摘要，控制面对「解码后的对象」算摘要，两者必须逐位相等——
// 否则 v2 签名在所有正常请求上都会验签失败（把好请求全拒了）。
// 逐个报文类型验证 marshal → unmarshal → marshal 的字节稳定性。
func TestPayloadDigest_RoundTripStable(t *testing.T) {
	cases := []struct {
		name string
		msg  any
	}{
		{"AgentInfo", &proto.AgentInfo{
			AgentID: "agent-1", Hostname: "h1", Segment: "seg", TenantID: "t1",
			Addr: "10.0.0.1", GRPCPort: 9090, MetricsPort: 9091, Status: "online",
			Load: 3, LastSeen: time.Now().UTC(), InstallToken: "tok", OnboardDeviceID: "dev-1",
			OS: "linux", Arch: "amd64",
		}},
		{"HeartbeatReq", &HeartbeatReq{
			AgentID: "agent-1", Status: "online", Load: 3,
			CmdbReport: &proto.CmdbReport{CiType: "machine", Seq: 7, Attrs: []proto.CmdbAttr{{Key: "os.version", Value: "22.04", Type: "string"}}},
			Metrics:    &proto.DeviceMetrics{DeviceID: "dev-1", Hostname: "h1", OS: "linux", ProcessCount: 12, CPU: proto.CPUMetrics{Cores: 8, Usage: 12.5, Model: "x"}, CollectedAt: time.Now().UTC()},
		}},
		{"HeartbeatReq_Minimal", &HeartbeatReq{AgentID: "agent-1"}},
		{"PullTasksReq", &PullTasksReq{AgentID: "agent-1"}},
		{"TaskResult", &proto.TaskResult{
			TaskID: "t-1", AgentID: "agent-1", ExitCode: 1,
			Stdout: "out", Stderr: "err", DurationMs: 1234, FinishedAt: time.Now().UTC(), ClaimEpoch: 9,
		}},
		{"CancelTaskReq", &CancelTaskReq{TaskID: "t-1", TenantID: "t1"}},
		{"PollCancelsReq", &PollCancelsReq{AgentID: "agent-1"}},
		{"ReportLogsReq", &ReportLogsReq{Report: proto.LogReport{
			AgentID: "agent-1", TenantID: "t1", LogName: "syslog",
			Lines: []proto.LogLine{{Timestamp: time.Now().UTC(), Level: "info", Message: "hello"}},
		}}},
		{"ConfigureAgentReq", &ConfigureAgentReq{AgentID: "agent-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := jsonCodec{}.Marshal(tc.msg)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			sent := PayloadDigest(tc.msg)
			if sent == "" {
				t.Fatal("PayloadDigest 返回空串（Marshal 失败）")
			}
			// 模拟控制面：把线上字节解码成新对象后重新算摘要。
			decoded := newSameType(tc.msg)
			err = (jsonCodec{}).Unmarshal(raw, decoded)
			if err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			got := PayloadDigest(decoded)
			if got != sent {
				t.Fatalf("跨端摘要不一致：agent=%s controlplane=%s", sent, got)
			}
		})
	}
}

// newSameType 返回与样例同类型的新对象（测试内手写，避免反射）。
func newSameType(msg any) any {
	switch msg.(type) {
	case *proto.AgentInfo:
		return &proto.AgentInfo{}
	case *HeartbeatReq:
		return &HeartbeatReq{}
	case *PullTasksReq:
		return &PullTasksReq{}
	case *proto.TaskResult:
		return &proto.TaskResult{}
	case *CancelTaskReq:
		return &CancelTaskReq{}
	case *PollCancelsReq:
		return &PollCancelsReq{}
	case *ReportLogsReq:
		return &ReportLogsReq{}
	case *ConfigureAgentReq:
		return &ConfigureAgentReq{}
	}
	panic("newSameType: 未覆盖的类型")
}

// TestPayloadDigest_SensitiveToEveryField 摘要必须对业务字段敏感（v2 的核心价值）。
func TestPayloadDigest_SensitiveToEveryField(t *testing.T) {
	base := &proto.TaskResult{TaskID: "t-1", AgentID: "agent-1", ExitCode: 0, Stdout: "ok"}
	baseDigest := PayloadDigest(base)

	mutants := map[string]*proto.TaskResult{
		"改 TaskID":   {TaskID: "t-2", AgentID: "agent-1", Stdout: "ok"},
		"改 AgentID":  {TaskID: "t-1", AgentID: "agent-2", Stdout: "ok"},
		"改 ExitCode": {TaskID: "t-1", AgentID: "agent-1", ExitCode: 1, Stdout: "ok"},
		"改 Stdout":   {TaskID: "t-1", AgentID: "agent-1", Stdout: "forged"},
		"加 Stderr":   {TaskID: "t-1", AgentID: "agent-1", Stdout: "ok", Stderr: "x"},
	}
	for name, m := range mutants {
		if PayloadDigest(m) == baseDigest {
			t.Fatalf("%s 后摘要未变：篡改不可检出", name)
		}
	}
}

// TestPayloadDigest_DeterministicAcrossCalls map 字段（ControlConfig）依赖 json 键排序，
// 多次调用必须得到同一摘要（否则签名随机化 = 验签随机失败）。
func TestPayloadDigest_DeterministicAcrossCalls(t *testing.T) {
	msg := &RegisterResp{AgentID: "a", ControlConfig: map[string]int{"heartbeatInterval": 10, "taskPollInterval": 15}}
	first := PayloadDigest(msg)
	for i := 0; i < 20; i++ {
		if got := PayloadDigest(msg); got != first {
			t.Fatalf("第 %d 次摘要不同：%s != %s", i+2, got, first)
		}
	}
}

// TestComputeAgentSignature_V1V2Differ v2 必须与 v1 不同（否则 v1 请求会被误判为 v2）。
func TestComputeAgentSignature_V1V2Differ(t *testing.T) {
	v1 := ComputeAgentSignatureV1("secret", "1700000000", "agent-1")
	v2 := ComputeAgentSignatureV2("secret", "1700000000", "agent-1", PayloadDigest(&PullTasksReq{AgentID: "agent-1"}))
	if v1 == v2 {
		t.Fatal("v1 与 v2 签名相同：算法域未分离")
	}
	if len(v1) != 64 || len(v2) != 64 {
		t.Fatalf("HMAC-SHA256 hex 长度应为 64，得到 %d/%d", len(v1), len(v2))
	}
}

// TestComputeAgentSignatureV2_FieldAmbiguity v2 消息不得因分段拼接产生歧义：
// 身份与摘要边界不可通过挪动内容互相伪装（"a"+"bc" 与 "ab"+"c" 必须产出不同签名）。
func TestComputeAgentSignatureV2_FieldAmbiguity(t *testing.T) {
	a := ComputeAgentSignatureV2("s", "1", "agent-1", "aa")
	b := ComputeAgentSignatureV2("s", "1", "agent-1a", "a")
	if strings.EqualFold(a, b) {
		t.Fatal("分段边界歧义：不同 (身份,摘要) 组合产出同一签名")
	}
	// \n 分隔后，时间戳与身份不可互换（"1"+"2" vs "12"+""）。
	c := ComputeAgentSignatureV2("s", "1", "2", "d")
	d := ComputeAgentSignatureV2("s", "12", "", "d")
	if strings.EqualFold(c, d) {
		t.Fatal("分段边界歧义：timestamp/identity 可互换")
	}
}

// TestComputeAgentSignature_V1MatchesLegacyFormula 锁死 v1 消息构造，避免误改导致存量 agent 全部验签失败。
func TestComputeAgentSignature_V1MatchesLegacyFormula(t *testing.T) {
	// 与历史实现逐字节一致：HMAC-SHA256(secret, timestamp+agentID)。
	got := ComputeAgentSignatureV1("k", "1700000000", "agent-x")
	if got != hmacHex("k", "1700000000agent-x") {
		t.Fatal("v1 消息构造已偏离历史实现（存量 agent 将验签失败）")
	}
}
