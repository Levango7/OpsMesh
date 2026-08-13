package grpcx

import (
	"testing"

	"google.golang.org/grpc"
)

// TestRegistrationServiceDesc 校验手写 ServiceDesc 的完整性（无 protoc 的契约护栏）。
func TestRegistrationServiceDesc(t *testing.T) {
	sd := Registration_ServiceDesc
	var _ grpc.ServiceDesc = sd // 编译期类型断言

	if sd.ServiceName != "opsmesh.v1.Registration" {
		t.Fatalf("ServiceName = %q, want opsmesh.v1.Registration", sd.ServiceName)
	}
	if sd.HandlerType != (*RegistrationServer)(nil) {
		t.Fatal("HandlerType must be *RegistrationServer")
	}
	if len(sd.Methods) != 7 {
		t.Fatalf("Methods = %d, want 7 (Register/Heartbeat/PullTasks/ReportResult/CancelTask/PollCancels/ReportLogs)", len(sd.Methods))
	}
	if len(sd.Streams) != 0 {
		t.Fatalf("Streams = %d, want 0 (内核只产生数据，不消费流)", len(sd.Streams))
	}
	want := map[string]bool{
		"Register":     true,
		"Heartbeat":    true,
		"PullTasks":    true,
		"ReportResult": true,
		"CancelTask":   true,
		"PollCancels":  true,
		"ReportLogs":   true,
	}
	for _, m := range sd.Methods {
		if !want[m.MethodName] {
			t.Fatalf("unexpected method %q", m.MethodName)
		}
		delete(want, m.MethodName)
	}
	if len(want) != 0 {
		t.Fatalf("missing methods %v", want)
	}
}
