// Package service implements the business logic for log-svc.
package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"opsmesh.io/log-svc/api/proto/v1"
	"opsmesh.io/log-svc/pkg/logstore"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Service wraps logstore.Handler and implements business logic.
type Service struct {
	logv1.UnimplementedLogServiceServer
	store logstore.LogStore
}

// NewService creates a new Service instance.
func NewService(store logstore.LogStore) *Service {
	return &Service{store: store}
}

// ensure Service implements the gRPC interface
func (s *Service) mustEmbedUnimplementedLogServiceServer() {}

// SearchLogs searches log entries with the given filters.
func (s *Service) SearchLogs(ctx context.Context, req *logv1.SearchLogsRequest) (*logv1.SearchLogsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request cannot be nil")
	}

	q := logstore.Query{
		TenantID: req.TenantId,
		DeviceID: req.DeviceId,
		AgentID:  req.AgentId,
		Level:    req.Level,
		Source:   req.Source,
		Keyword:  req.Keyword,
		Q:        req.Q,
		Limit:    int(req.Limit),
		Offset:   int(req.Offset),
	}

	if req.From != nil {
		q.From = req.From.AsTime()
	}
	if req.To != nil {
		q.To = req.To.AsTime()
	}

	entries, err := s.store.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	pbEntries := make([]*logv1.LogEntry, 0, len(entries))
	for i := range entries {
		pbEntries = append(pbEntries, entryToProto(&entries[i]))
	}

	return &logv1.SearchLogsResponse{
		Entries: pbEntries,
		Total:   int32(len(pbEntries)),
	}, nil
}

// AppendLog appends a single log entry.
func (s *Service) AppendLog(ctx context.Context, req *logv1.AppendLogRequest) (*logv1.LogEntry, error) {
	if req == nil {
		return nil, fmt.Errorf("request cannot be nil")
	}

	entry := &logstore.Entry{
		TenantID: req.TenantId,
		DeviceID: req.DeviceId,
		AgentID:  req.AgentId,
		TaskID:   req.TaskId,
		Level:    req.Level,
		Source:   req.Source,
		Message:  req.Message,
	}

	if err := s.store.Append(ctx, entry); err != nil {
		return nil, fmt.Errorf("append failed: %w", err)
	}

	return entryToProto(entry), nil
}

// GetLogStats returns log statistics for a tenant within a time range.
func (s *Service) GetLogStats(ctx context.Context, req *logv1.GetLogStatsRequest) (*logv1.GetLogStatsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request cannot be nil")
	}

	q := logstore.Query{
		TenantID: req.TenantId,
		Limit:    1000, // max query limit for stats
	}

	if req.From != nil {
		q.From = req.From.AsTime()
	}
	if req.To != nil {
		q.To = req.To.AsTime()
	}

	entries, err := s.store.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("stats query failed: %w", err)
	}

	var errorCount, warnCount, infoCount int64
	sourceCounts := make(map[string]int64)

	for i := range entries {
		switch strings.ToLower(entries[i].Level) {
		case "error":
			errorCount++
		case "warn", "warning":
			warnCount++
		case "info":
			infoCount++
		}
		if entries[i].Source != "" {
			sourceCounts[entries[i].Source]++
		}
	}

	return &logv1.GetLogStatsResponse{
		TotalEntries: int64(len(entries)),
		ErrorCount:   errorCount,
		WarnCount:    warnCount,
		InfoCount:    infoCount,
		SourceCounts: sourceCounts,
	}, nil
}

// ListLogSources returns distinct log sources for a tenant.
func (s *Service) ListLogSources(ctx context.Context, req *logv1.ListLogSourcesRequest) (*logv1.ListLogSourcesResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request cannot be nil")
	}

	q := logstore.Query{
		TenantID: req.TenantId,
		Limit:    1000,
	}

	entries, err := s.store.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list sources query failed: %w", err)
	}

	seen := make(map[string]struct{})
	var sources []string
	for i := range entries {
		src := entries[i].Source
		if src == "" {
			src = "unknown"
		}
		if _, ok := seen[src]; !ok {
			seen[src] = struct{}{}
			sources = append(sources, src)
		}
	}

	return &logv1.ListLogSourcesResponse{
		Sources: sources,
	}, nil
}

// Close releases the underlying store resources.
func (s *Service) Close() error {
	if s.store != nil {
		return s.store.Close()
	}
	return nil
}

// entryToProto converts a logstore.Entry to a logv1.LogEntry.
func entryToProto(e *logstore.Entry) *logv1.LogEntry {
	if e == nil {
		return nil
	}
	return &logv1.LogEntry{
		Id:        e.ID,
		TenantId:  e.TenantID,
		DeviceId:  e.DeviceID,
		AgentId:   e.AgentID,
		TaskId:    e.TaskID,
		Timestamp: timeToProto(e.Timestamp),
		Level:     e.Level,
		Source:    e.Source,
		Message:   e.Message,
	}
}

// timeToProto converts time.Time to *timestamppb.Timestamp.
func timeToProto(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return &timestamppb.Timestamp{
		Seconds: t.Unix(),
		Nanos:   int32(t.Nanosecond()),
	}
}
