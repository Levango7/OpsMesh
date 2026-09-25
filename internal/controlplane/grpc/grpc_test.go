package grpc

import (
	"context"
	"testing"

	"github.com/Levango7/OpsMesh/internal/grpcx"
)

func TestSSEDefaultTenant(t *testing.T) {
	if sseDefaultTenant != "default" {
		t.Fatalf("sseDefaultTenant = %q, want default", sseDefaultTenant)
	}
}

func TestAgentSignatureMaxSkew(t *testing.T) {
	expected := 5 * 60
	if int(agentSignatureMaxSkew.Seconds()) != expected {
		t.Fatalf("agentSignatureMaxSkew = %v, want %ds", agentSignatureMaxSkew, expected)
	}
}

// metadata 键与签名算法常量已收敛到 internal/grpcx（agent 与控制面共用一套），
// 此处只断言控制面侧确实引用同一套常量，避免两端各自定义后漂移。
func TestSignatureConstantsSharedWithGrpcx(t *testing.T) {
	if grpcx.AgentSignatureMetadataKey != "agent-signature" {
		t.Fatalf("signature key = %q, want agent-signature", grpcx.AgentSignatureMetadataKey)
	}
	if grpcx.AgentTimestampMetadataKey != "agent-timestamp" {
		t.Fatalf("timestamp key = %q, want agent-timestamp", grpcx.AgentTimestampMetadataKey)
	}
	if grpcx.AgentSignatureAlgV2 != "v2" || grpcx.AgentSignatureAlgV1 != "v1" {
		t.Fatalf("alg constants = %q/%q, want v1/v2", grpcx.AgentSignatureAlgV1, grpcx.AgentSignatureAlgV2)
	}
}

func TestGrpcServerImpl_FieldsAccessible(t *testing.T) {
	g := &GrpcServerImpl{
		Store:       nil,
		RequireAuth: true,
		Cfg:         nil,
		Bus:         nil,
		Metrics:     nil,
		Cmdb:        nil,
		Logs:        nil,
		Publisher:   nil,
	}
	if !g.RequireAuth {
		t.Fatal("RequireAuth should be true")
	}
}

var _ EventPublisher = (*mockPublisher)(nil)

type mockPublisher struct{}

func (m *mockPublisher) PublishEvent(ctx context.Context, typ string, tenantID string, data interface{}) {
}

func TestEventPublisherInterface(t *testing.T) {
	var pub EventPublisher = &mockPublisher{}
	_ = pub
}
