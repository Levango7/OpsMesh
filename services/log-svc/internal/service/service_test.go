package service

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	"opsmesh.io/log-svc/api/proto/v1"
	"opsmesh.io/log-svc/pkg/logstore"
)

// newTestService creates a service with an in-memory store for testing.
func newTestService() *Service {
	store := logstore.NewMemoryWithIndex(1000)
	return NewService(store)
}

func TestNewService(t *testing.T) {
	store := logstore.NewMemory(100)
	svc := NewService(store)
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
	if svc.store != store {
		t.Error("service store not set correctly")
	}
}

func TestSearchLogs_Empty(t *testing.T) {
	svc := newTestService()
	defer svc.Close()

	ctx := context.Background()
	req := &logv1.SearchLogsRequest{
		TenantId: "test-tenant",
	}

	resp, err := svc.SearchLogs(ctx, req)
	if err != nil {
		t.Fatalf("SearchLogs failed: %v", err)
	}
	if resp == nil {
		t.Fatal("SearchLogs returned nil response")
	}
	if len(resp.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(resp.Entries))
	}
}

func TestSearchLogs_WithEntries(t *testing.T) {
	svc := newTestService()
	defer svc.Close()

	ctx := context.Background()

	// Append test entries
	entries := []*logv1.AppendLogRequest{
		{TenantId: "t1", Level: "info", Source: "agent", Message: "hello world"},
		{TenantId: "t1", Level: "error", Source: "agent", Message: "something failed"},
		{TenantId: "t1", Level: "warn", Source: "system", Message: "disk almost full"},
		{TenantId: "t2", Level: "info", Source: "agent", Message: "other tenant"},
	}

	for _, e := range entries {
		_, err := svc.AppendLog(ctx, e)
		if err != nil {
			t.Fatalf("AppendLog failed: %v", err)
		}
	}

	// Search all for tenant t1
	resp, err := svc.SearchLogs(ctx, &logv1.SearchLogsRequest{
		TenantId: "t1",
	})
	if err != nil {
		t.Fatalf("SearchLogs failed: %v", err)
	}
	if len(resp.Entries) != 3 {
		t.Errorf("expected 3 entries for t1, got %d", len(resp.Entries))
	}

	// Search with level filter
	resp, err = svc.SearchLogs(ctx, &logv1.SearchLogsRequest{
		TenantId: "t1",
		Level:    "error",
	})
	if err != nil {
		t.Fatalf("SearchLogs with level filter failed: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Errorf("expected 1 error entry, got %d", len(resp.Entries))
	}
	if len(resp.Entries) > 0 && resp.Entries[0].Message != "something failed" {
		t.Errorf("unexpected message: %s", resp.Entries[0].Message)
	}

	// Search with keyword
	resp, err = svc.SearchLogs(ctx, &logv1.SearchLogsRequest{
		TenantId: "t1",
		Keyword:  "disk",
	})
	if err != nil {
		t.Fatalf("SearchLogs with keyword failed: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Errorf("expected 1 entry with 'disk', got %d", len(resp.Entries))
	}

	// Search tenant t2
	resp, err = svc.SearchLogs(ctx, &logv1.SearchLogsRequest{
		TenantId: "t2",
	})
	if err != nil {
		t.Fatalf("SearchLogs for t2 failed: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Errorf("expected 1 entry for t2, got %d", len(resp.Entries))
	}
}

func TestSearchLogs_WithTimeRange(t *testing.T) {
	svc := newTestService()
	defer svc.Close()

	ctx := context.Background()

	// Append entries
	_, err := svc.AppendLog(ctx, &logv1.AppendLogRequest{
		TenantId: "t1", Level: "info", Source: "agent", Message: "msg1",
	})
	if err != nil {
		t.Fatalf("AppendLog failed: %v", err)
	}

	time.Sleep(10 * time.Millisecond)
	now := time.Now()
	time.Sleep(10 * time.Millisecond)

	_, err = svc.AppendLog(ctx, &logv1.AppendLogRequest{
		TenantId: "t1", Level: "info", Source: "agent", Message: "msg2",
	})
	if err != nil {
		t.Fatalf("AppendLog failed: %v", err)
	}

	// Search with from time after first entry
	resp, err := svc.SearchLogs(ctx, &logv1.SearchLogsRequest{
		TenantId: "t1",
		From:     timestamppb.New(now),
	})
	if err != nil {
		t.Fatalf("SearchLogs with time range failed: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Errorf("expected 1 entry after time filter, got %d", len(resp.Entries))
	}
	if len(resp.Entries) > 0 && resp.Entries[0].Message != "msg2" {
		t.Errorf("expected msg2, got %s", resp.Entries[0].Message)
	}
}

func TestSearchLogs_NilRequest(t *testing.T) {
	svc := newTestService()
	defer svc.Close()

	_, err := svc.SearchLogs(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil request")
	}
}

func TestSearchLogs_Pagination(t *testing.T) {
	svc := newTestService()
	defer svc.Close()

	ctx := context.Background()

	// Append 10 entries
	for i := 0; i < 10; i++ {
		_, err := svc.AppendLog(ctx, &logv1.AppendLogRequest{
			TenantId: "t1", Level: "info", Source: "agent", Message: "entry",
		})
		if err != nil {
			t.Fatalf("AppendLog failed: %v", err)
		}
	}

	// Test limit
	resp, err := svc.SearchLogs(ctx, &logv1.SearchLogsRequest{
		TenantId: "t1",
		Limit:    5,
	})
	if err != nil {
		t.Fatalf("SearchLogs with limit failed: %v", err)
	}
	if len(resp.Entries) != 5 {
		t.Errorf("expected 5 entries with limit, got %d", len(resp.Entries))
	}

	// Test offset
	resp, err = svc.SearchLogs(ctx, &logv1.SearchLogsRequest{
		TenantId: "t1",
		Limit:    5,
		Offset:   5,
	})
	if err != nil {
		t.Fatalf("SearchLogs with offset failed: %v", err)
	}
	if len(resp.Entries) != 5 {
		t.Errorf("expected 5 entries with offset, got %d", len(resp.Entries))
	}
}

func TestAppendLog(t *testing.T) {
	svc := newTestService()
	defer svc.Close()

	ctx := context.Background()

	req := &logv1.AppendLogRequest{
		TenantId: "test-tenant",
		DeviceId: "dev-192.168.1.1",
		AgentId:  "agent-1",
		TaskId:   "task-1",
		Level:    "error",
		Source:   "task",
		Message:  "command failed",
	}

	entry, err := svc.AppendLog(ctx, req)
	if err != nil {
		t.Fatalf("AppendLog failed: %v", err)
	}
	if entry == nil {
		t.Fatal("AppendLog returned nil entry")
	}
	if entry.Id == 0 {
		t.Error("entry ID should be non-zero")
	}
	if entry.TenantId != req.TenantId {
		t.Errorf("tenant ID mismatch: got %s, want %s", entry.TenantId, req.TenantId)
	}
	if entry.Level != req.Level {
		t.Errorf("level mismatch: got %s, want %s", entry.Level, req.Level)
	}
	if entry.Message != req.Message {
		t.Errorf("message mismatch: got %s, want %s", entry.Message, req.Message)
	}
	if entry.Timestamp == nil {
		t.Error("timestamp should not be nil")
	}
}

func TestAppendLog_Defaults(t *testing.T) {
	svc := newTestService()
	defer svc.Close()

	ctx := context.Background()

	// Append without level or source
	req := &logv1.AppendLogRequest{
		TenantId: "test-tenant",
		Message:  "test message",
	}

	entry, err := svc.AppendLog(ctx, req)
	if err != nil {
		t.Fatalf("AppendLog failed: %v", err)
	}

	// Service should pass through to store which applies defaults
	if entry.Message != req.Message {
		t.Errorf("message mismatch: got %s, want %s", entry.Message, req.Message)
	}
}

func TestAppendLog_NilRequest(t *testing.T) {
	svc := newTestService()
	defer svc.Close()

	_, err := svc.AppendLog(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil request")
	}
}

func TestGetLogStats(t *testing.T) {
	svc := newTestService()
	defer svc.Close()

	ctx := context.Background()

	// Append entries with different levels
	entries := []*logv1.AppendLogRequest{
		{TenantId: "t1", Level: "info", Source: "agent", Message: "msg1"},
		{TenantId: "t1", Level: "info", Source: "agent", Message: "msg2"},
		{TenantId: "t1", Level: "warn", Source: "agent", Message: "msg3"},
		{TenantId: "t1", Level: "error", Source: "task", Message: "msg4"},
		{TenantId: "t1", Level: "error", Source: "task", Message: "msg5"},
		{TenantId: "t1", Level: "error", Source: "task", Message: "msg6"},
	}

	for _, e := range entries {
		_, err := svc.AppendLog(ctx, e)
		if err != nil {
			t.Fatalf("AppendLog failed: %v", err)
		}
	}

	req := &logv1.GetLogStatsRequest{
		TenantId: "t1",
	}

	resp, err := svc.GetLogStats(ctx, req)
	if err != nil {
		t.Fatalf("GetLogStats failed: %v", err)
	}
	if resp == nil {
		t.Fatal("GetLogStats returned nil response")
	}
	if resp.TotalEntries != 6 {
		t.Errorf("expected 6 total entries, got %d", resp.TotalEntries)
	}
	if resp.InfoCount != 2 {
		t.Errorf("expected 2 info entries, got %d", resp.InfoCount)
	}
	if resp.WarnCount != 1 {
		t.Errorf("expected 1 warn entry, got %d", resp.WarnCount)
	}
	if resp.ErrorCount != 3 {
		t.Errorf("expected 3 error entries, got %d", resp.ErrorCount)
	}
	if resp.SourceCounts["agent"] != 3 {
		t.Errorf("expected 3 agent entries, got %d", resp.SourceCounts["agent"])
	}
	if resp.SourceCounts["task"] != 3 {
		t.Errorf("expected 3 task entries, got %d", resp.SourceCounts["task"])
	}
}

func TestGetLogStats_NilRequest(t *testing.T) {
	svc := newTestService()
	defer svc.Close()

	_, err := svc.GetLogStats(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil request")
	}
}

func TestGetLogStats_EmptyTenant(t *testing.T) {
	svc := newTestService()
	defer svc.Close()

	ctx := context.Background()

	resp, err := svc.GetLogStats(ctx, &logv1.GetLogStatsRequest{
		TenantId: "nonexistent",
	})
	if err != nil {
		t.Fatalf("GetLogStats for empty tenant failed: %v", err)
	}
	if resp.TotalEntries != 0 {
		t.Errorf("expected 0 entries for nonexistent tenant, got %d", resp.TotalEntries)
	}
}

func TestListLogSources(t *testing.T) {
	svc := newTestService()
	defer svc.Close()

	ctx := context.Background()

	// Append entries with different sources
	entries := []*logv1.AppendLogRequest{
		{TenantId: "t1", Level: "info", Source: "agent", Message: "msg1"},
		{TenantId: "t1", Level: "info", Source: "agent", Message: "msg2"},
		{TenantId: "t1", Level: "warn", Source: "system", Message: "msg3"},
		{TenantId: "t1", Level: "error", Source: "task", Message: "msg4"},
	}

	for _, e := range entries {
		_, err := svc.AppendLog(ctx, e)
		if err != nil {
			t.Fatalf("AppendLog failed: %v", err)
		}
	}

	req := &logv1.ListLogSourcesRequest{
		TenantId: "t1",
	}

	resp, err := svc.ListLogSources(ctx, req)
	if err != nil {
		t.Fatalf("ListLogSources failed: %v", err)
	}
	if resp == nil {
		t.Fatal("ListLogSources returned nil response")
	}
	if len(resp.Sources) != 3 {
		t.Errorf("expected 3 distinct sources, got %d", len(resp.Sources))
	}

	// Check that all expected sources are present
	sourceSet := make(map[string]bool)
	for _, s := range resp.Sources {
		sourceSet[s] = true
	}
	for _, expected := range []string{"agent", "system", "task"} {
		if !sourceSet[expected] {
			t.Errorf("expected source %q not found in response", expected)
		}
	}
}

func TestListLogSources_NilRequest(t *testing.T) {
	svc := newTestService()
	defer svc.Close()

	_, err := svc.ListLogSources(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil request")
	}
}

func TestListLogSources_Empty(t *testing.T) {
	svc := newTestService()
	defer svc.Close()

	ctx := context.Background()

	resp, err := svc.ListLogSources(ctx, &logv1.ListLogSourcesRequest{
		TenantId: "nonexistent",
	})
	if err != nil {
		t.Fatalf("ListLogSources for empty tenant failed: %v", err)
	}
	if len(resp.Sources) != 0 {
		t.Errorf("expected 0 sources for nonexistent tenant, got %d", len(resp.Sources))
	}
}

func TestServiceClose(t *testing.T) {
	svc := newTestService()
	err := svc.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestEntryToProto(t *testing.T) {
	entry := &logstore.Entry{
		ID:        123,
		TenantID:  "t1",
		DeviceID:  "dev-1",
		AgentID:   "agent-1",
		TaskID:    "task-1",
		Timestamp: time.Now().UTC(),
		Level:     "info",
		Source:    "agent",
		Message:   "test message",
	}

	proto := entryToProto(entry)
	if proto == nil {
		t.Fatal("entryToProto returned nil")
	}
	if proto.Id != entry.ID {
		t.Errorf("ID mismatch: got %d, want %d", proto.Id, entry.ID)
	}
	if proto.TenantId != entry.TenantID {
		t.Errorf("TenantId mismatch: got %s, want %s", proto.TenantId, entry.TenantID)
	}
	if proto.DeviceId != entry.DeviceID {
		t.Errorf("DeviceId mismatch: got %s, want %s", proto.DeviceId, entry.DeviceID)
	}
	if proto.AgentId != entry.AgentID {
		t.Errorf("AgentId mismatch: got %s, want %s", proto.AgentId, entry.AgentID)
	}
	if proto.TaskId != entry.TaskID {
		t.Errorf("TaskId mismatch: got %s, want %s", proto.TaskId, entry.TaskID)
	}
	if proto.Level != entry.Level {
		t.Errorf("Level mismatch: got %s, want %s", proto.Level, entry.Level)
	}
	if proto.Source != entry.Source {
		t.Errorf("Source mismatch: got %s, want %s", proto.Source, entry.Source)
	}
	if proto.Message != entry.Message {
		t.Errorf("Message mismatch: got %s, want %s", proto.Message, entry.Message)
	}
	if proto.Timestamp == nil {
		t.Error("Timestamp should not be nil")
	}
}

func TestEntryToProto_Nil(t *testing.T) {
	result := entryToProto(nil)
	if result != nil {
		t.Error("entryToProto(nil) should return nil")
	}
}
