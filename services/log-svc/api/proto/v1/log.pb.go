// Code generated manually to match api/proto/v1/log.proto. DO NOT EDIT.
// This package provides protobuf-compatible message types for the LogService.
// Messages use JSON serialization via a custom gRPC codec.

package logv1

import (
	"fmt"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// LogEntry represents a single log record.
type LogEntry struct {
	Id        int64                  `protobuf:"varint,1,opt,name=id,proto3" json:"id,omitempty"`
	TenantId  string                 `protobuf:"bytes,2,opt,name=tenant_id,json=tenantId,proto3" json:"tenant_id,omitempty"`
	DeviceId  string                 `protobuf:"bytes,3,opt,name=device_id,json=deviceId,proto3" json:"device_id,omitempty"`
	AgentId   string                 `protobuf:"bytes,4,opt,name=agent_id,json=agentId,proto3" json:"agent_id,omitempty"`
	TaskId    string                 `protobuf:"bytes,5,opt,name=task_id,json=taskId,proto3" json:"task_id,omitempty"`
	Timestamp *timestamppb.Timestamp `protobuf:"bytes,6,opt,name=timestamp,proto3" json:"timestamp,omitempty"`
	Level     string                 `protobuf:"bytes,7,opt,name=level,proto3" json:"level,omitempty"`
	Source    string                 `protobuf:"bytes,8,opt,name=source,proto3" json:"source,omitempty"`
	Message   string                 `protobuf:"bytes,9,opt,name=message,proto3" json:"message,omitempty"`
}

func (x *LogEntry) Reset() {
	*x = LogEntry{}
}

func (x *LogEntry) String() string {
	return fmt.Sprintf("LogEntry{Id:%d TenantId:%s Level:%s Source:%s Message:%s}",
		x.Id, x.TenantId, x.Level, x.Source, x.Message)
}

func (x *LogEntry) ProtoMessage() {}

func (x *LogEntry) GetId() int64 {
	if x != nil {
		return x.Id
	}
	return 0
}

func (x *LogEntry) GetTenantId() string {
	if x != nil {
		return x.TenantId
	}
	return ""
}

func (x *LogEntry) GetDeviceId() string {
	if x != nil {
		return x.DeviceId
	}
	return ""
}

func (x *LogEntry) GetAgentId() string {
	if x != nil {
		return x.AgentId
	}
	return ""
}

func (x *LogEntry) GetTaskId() string {
	if x != nil {
		return x.TaskId
	}
	return ""
}

func (x *LogEntry) GetTimestamp() *timestamppb.Timestamp {
	if x != nil {
		return x.Timestamp
	}
	return nil
}

func (x *LogEntry) GetLevel() string {
	if x != nil {
		return x.Level
	}
	return ""
}

func (x *LogEntry) GetSource() string {
	if x != nil {
		return x.Source
	}
	return ""
}

func (x *LogEntry) GetMessage() string {
	if x != nil {
		return x.Message
	}
	return ""
}

// Time returns the timestamp as time.Time.
func (x *LogEntry) Time() time.Time {
	if x == nil || x.Timestamp == nil {
		return time.Time{}
	}
	return x.Timestamp.AsTime()
}

// SearchLogsRequest is the request to search logs.
type SearchLogsRequest struct {
	TenantId string                 `protobuf:"bytes,1,opt,name=tenant_id,json=tenantId,proto3" json:"tenant_id,omitempty"`
	DeviceId string                 `protobuf:"bytes,2,opt,name=device_id,json=deviceId,proto3" json:"device_id,omitempty"`
	AgentId  string                 `protobuf:"bytes,3,opt,name=agent_id,json=agentId,proto3" json:"agent_id,omitempty"`
	Level    string                 `protobuf:"bytes,4,opt,name=level,proto3" json:"level,omitempty"`
	Source   string                 `protobuf:"bytes,5,opt,name=source,proto3" json:"source,omitempty"`
	Keyword  string                 `protobuf:"bytes,6,opt,name=keyword,proto3" json:"keyword,omitempty"`
	Q        string                 `protobuf:"bytes,7,opt,name=q,proto3" json:"q,omitempty"`
	From     *timestamppb.Timestamp `protobuf:"bytes,8,opt,name=from,proto3" json:"from,omitempty"`
	To       *timestamppb.Timestamp `protobuf:"bytes,9,opt,name=to,proto3" json:"to,omitempty"`
	Limit    int32                  `protobuf:"varint,10,opt,name=limit,proto3" json:"limit,omitempty"`
	Offset   int32                  `protobuf:"varint,11,opt,name=offset,proto3" json:"offset,omitempty"`
}

func (x *SearchLogsRequest) Reset() {
	*x = SearchLogsRequest{}
}

func (x *SearchLogsRequest) String() string {
	return fmt.Sprintf("SearchLogsRequest{TenantId:%s Limit:%d}", x.TenantId, x.Limit)
}

func (x *SearchLogsRequest) ProtoMessage() {}

func (x *SearchLogsRequest) GetTenantId() string {
	if x != nil {
		return x.TenantId
	}
	return ""
}

func (x *SearchLogsRequest) GetDeviceId() string {
	if x != nil {
		return x.DeviceId
	}
	return ""
}

func (x *SearchLogsRequest) GetAgentId() string {
	if x != nil {
		return x.AgentId
	}
	return ""
}

func (x *SearchLogsRequest) GetLevel() string {
	if x != nil {
		return x.Level
	}
	return ""
}

func (x *SearchLogsRequest) GetSource() string {
	if x != nil {
		return x.Source
	}
	return ""
}

func (x *SearchLogsRequest) GetKeyword() string {
	if x != nil {
		return x.Keyword
	}
	return ""
}

func (x *SearchLogsRequest) GetQ() string {
	if x != nil {
		return x.Q
	}
	return ""
}

func (x *SearchLogsRequest) GetFrom() *timestamppb.Timestamp {
	if x != nil {
		return x.From
	}
	return nil
}

func (x *SearchLogsRequest) GetTo() *timestamppb.Timestamp {
	if x != nil {
		return x.To
	}
	return nil
}

func (x *SearchLogsRequest) GetLimit() int32 {
	if x != nil {
		return x.Limit
	}
	return 0
}

func (x *SearchLogsRequest) GetOffset() int32 {
	if x != nil {
		return x.Offset
	}
	return 0
}

// FromTime returns the From timestamp as time.Time.
func (x *SearchLogsRequest) FromTime() time.Time {
	if x == nil || x.From == nil {
		return time.Time{}
	}
	return x.From.AsTime()
}

// ToTime returns the To timestamp as time.Time.
func (x *SearchLogsRequest) ToTime() time.Time {
	if x == nil || x.To == nil {
		return time.Time{}
	}
	return x.To.AsTime()
}

// SearchLogsResponse contains search results.
type SearchLogsResponse struct {
	Entries []*LogEntry `protobuf:"bytes,1,rep,name=entries,proto3" json:"entries,omitempty"`
	Total   int32       `protobuf:"varint,2,opt,name=total,proto3" json:"total,omitempty"`
}

func (x *SearchLogsResponse) Reset() {
	*x = SearchLogsResponse{}
}

func (x *SearchLogsResponse) String() string {
	return fmt.Sprintf("SearchLogsResponse{Entries:%d Total:%d}", len(x.Entries), x.Total)
}

func (x *SearchLogsResponse) ProtoMessage() {}

func (x *SearchLogsResponse) GetEntries() []*LogEntry {
	if x != nil {
		return x.Entries
	}
	return nil
}

func (x *SearchLogsResponse) GetTotal() int32 {
	if x != nil {
		return x.Total
	}
	return 0
}

// AppendLogRequest is the request to append a log entry.
type AppendLogRequest struct {
	TenantId string `protobuf:"bytes,1,opt,name=tenant_id,json=tenantId,proto3" json:"tenant_id,omitempty"`
	DeviceId string `protobuf:"bytes,2,opt,name=device_id,json=deviceId,proto3" json:"device_id,omitempty"`
	AgentId  string `protobuf:"bytes,3,opt,name=agent_id,json=agentId,proto3" json:"agent_id,omitempty"`
	TaskId   string `protobuf:"bytes,4,opt,name=task_id,json=taskId,proto3" json:"task_id,omitempty"`
	Level    string `protobuf:"bytes,5,opt,name=level,proto3" json:"level,omitempty"`
	Source   string `protobuf:"bytes,6,opt,name=source,proto3" json:"source,omitempty"`
	Message  string `protobuf:"bytes,7,opt,name=message,proto3" json:"message,omitempty"`
}

func (x *AppendLogRequest) Reset() {
	*x = AppendLogRequest{}
}

func (x *AppendLogRequest) String() string {
	return fmt.Sprintf("AppendLogRequest{TenantId:%s Level:%s Source:%s}", x.TenantId, x.Level, x.Source)
}

func (x *AppendLogRequest) ProtoMessage() {}

func (x *AppendLogRequest) GetTenantId() string {
	if x != nil {
		return x.TenantId
	}
	return ""
}

func (x *AppendLogRequest) GetDeviceId() string {
	if x != nil {
		return x.DeviceId
	}
	return ""
}

func (x *AppendLogRequest) GetAgentId() string {
	if x != nil {
		return x.AgentId
	}
	return ""
}

func (x *AppendLogRequest) GetTaskId() string {
	if x != nil {
		return x.TaskId
	}
	return ""
}

func (x *AppendLogRequest) GetLevel() string {
	if x != nil {
		return x.Level
	}
	return ""
}

func (x *AppendLogRequest) GetSource() string {
	if x != nil {
		return x.Source
	}
	return ""
}

func (x *AppendLogRequest) GetMessage() string {
	if x != nil {
		return x.Message
	}
	return ""
}

// GetLogStatsRequest is the request for log statistics.
type GetLogStatsRequest struct {
	TenantId string                 `protobuf:"bytes,1,opt,name=tenant_id,json=tenantId,proto3" json:"tenant_id,omitempty"`
	From     *timestamppb.Timestamp `protobuf:"bytes,2,opt,name=from,proto3" json:"from,omitempty"`
	To       *timestamppb.Timestamp `protobuf:"bytes,3,opt,name=to,proto3" json:"to,omitempty"`
}

func (x *GetLogStatsRequest) Reset() {
	*x = GetLogStatsRequest{}
}

func (x *GetLogStatsRequest) String() string {
	return fmt.Sprintf("GetLogStatsRequest{TenantId:%s}", x.TenantId)
}

func (x *GetLogStatsRequest) ProtoMessage() {}

func (x *GetLogStatsRequest) GetTenantId() string {
	if x != nil {
		return x.TenantId
	}
	return ""
}

func (x *GetLogStatsRequest) GetFrom() *timestamppb.Timestamp {
	if x != nil {
		return x.From
	}
	return nil
}

func (x *GetLogStatsRequest) GetTo() *timestamppb.Timestamp {
	if x != nil {
		return x.To
	}
	return nil
}

// GetLogStatsResponse contains log statistics.
type GetLogStatsResponse struct {
	TotalEntries int64            `protobuf:"varint,1,opt,name=total_entries,json=totalEntries,proto3" json:"total_entries,omitempty"`
	ErrorCount   int64            `protobuf:"varint,2,opt,name=error_count,json=errorCount,proto3" json:"error_count,omitempty"`
	WarnCount    int64            `protobuf:"varint,3,opt,name=warn_count,json=warnCount,proto3" json:"warn_count,omitempty"`
	InfoCount    int64            `protobuf:"varint,4,opt,name=info_count,json=infoCount,proto3" json:"info_count,omitempty"`
	SourceCounts map[string]int64 `protobuf:"bytes,5,rep,name=source_counts,json=sourceCounts,proto3" json:"source_counts,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"varint,2,opt,name=value,proto3"`
}

func (x *GetLogStatsResponse) Reset() {
	*x = GetLogStatsResponse{}
}

func (x *GetLogStatsResponse) String() string {
	return fmt.Sprintf("GetLogStatsResponse{Total:%d Error:%d Warn:%d Info:%d}",
		x.TotalEntries, x.ErrorCount, x.WarnCount, x.InfoCount)
}

func (x *GetLogStatsResponse) ProtoMessage() {}

func (x *GetLogStatsResponse) GetTotalEntries() int64 {
	if x != nil {
		return x.TotalEntries
	}
	return 0
}

func (x *GetLogStatsResponse) GetErrorCount() int64 {
	if x != nil {
		return x.ErrorCount
	}
	return 0
}

func (x *GetLogStatsResponse) GetWarnCount() int64 {
	if x != nil {
		return x.WarnCount
	}
	return 0
}

func (x *GetLogStatsResponse) GetInfoCount() int64 {
	if x != nil {
		return x.InfoCount
	}
	return 0
}

func (x *GetLogStatsResponse) GetSourceCounts() map[string]int64 {
	if x != nil {
		return x.SourceCounts
	}
	return nil
}

// ListLogSourcesRequest is the request to list log sources.
type ListLogSourcesRequest struct {
	TenantId string `protobuf:"bytes,1,opt,name=tenant_id,json=tenantId,proto3" json:"tenant_id,omitempty"`
}

func (x *ListLogSourcesRequest) Reset() {
	*x = ListLogSourcesRequest{}
}

func (x *ListLogSourcesRequest) String() string {
	return fmt.Sprintf("ListLogSourcesRequest{TenantId:%s}", x.TenantId)
}

func (x *ListLogSourcesRequest) ProtoMessage() {}

func (x *ListLogSourcesRequest) GetTenantId() string {
	if x != nil {
		return x.TenantId
	}
	return ""
}

// ListLogSourcesResponse contains distinct log sources.
type ListLogSourcesResponse struct {
	Sources []string `protobuf:"bytes,1,rep,name=sources,proto3" json:"sources,omitempty"`
}

func (x *ListLogSourcesResponse) Reset() {
	*x = ListLogSourcesResponse{}
}

func (x *ListLogSourcesResponse) String() string {
	return fmt.Sprintf("ListLogSourcesResponse{Sources:%d}", len(x.Sources))
}

func (x *ListLogSourcesResponse) ProtoMessage() {}

func (x *ListLogSourcesResponse) GetSources() []string {
	if x != nil {
		return x.Sources
	}
	return nil
}
