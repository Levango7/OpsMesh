package grpc

import (
	"context"
	"testing"
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

func TestAgentSignatureMetadataKey(t *testing.T) {
	if agentSignatureMetadataKey != "agent-signature" {
		t.Fatalf("agentSignatureMetadataKey = %q, want agent-signature", agentSignatureMetadataKey)
	}
}

func TestAgentTimestampMetadataKey(t *testing.T) {
	if agentTimestampMetadataKey != "agent-timestamp" {
		t.Fatalf("agentTimestampMetadataKey = %q, want agent-timestamp", agentTimestampMetadataKey)
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
